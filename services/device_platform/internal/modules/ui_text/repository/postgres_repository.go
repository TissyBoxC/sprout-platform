package repository

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/ui_text/domain"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// PostgresRepository is the PostgreSQL-backed remote UI text repository.
type PostgresRepository struct {
	pool *pgxpool.Pool
}

// NewPostgresRepository creates a remote UI text repository.
func NewPostgresRepository(pool *pgxpool.Pool) *PostgresRepository {
	return &PostgresRepository{pool: pool}
}

const packageColumns = `
	package_id,
	package_version,
	language,
	platform,
	min_client_version,
	status,
	signature_key_id,
	signature,
	submitted_at,
	approved_at,
	COALESCE(approved_by::text, ''),
	published_at,
	COALESCE(published_by::text, ''),
	withdrawn_at,
	revision,
	COALESCE(created_by::text, ''),
	created_at,
	updated_at
`

// NextVersion allocates the next version number for one logical package.
//
// The read is advisory only: CreateVersion holds the real duplicate guard
// through the primary key, so a concurrent operator gets a version conflict
// rather than two rows sharing one version number.
func (r *PostgresRepository) NextVersion(
	ctx context.Context,
	packageID string,
	language string,
	platform domain.Platform,
) (int, error) {
	var version int
	err := r.pool.QueryRow(ctx, `
		SELECT COALESCE(MAX(package_version), 0) + 1
		FROM ui_text_package_versions
		WHERE package_id = $1 AND language = $2 AND platform = $3
	`, packageID, language, platform).Scan(&version)
	if err != nil {
		return 0, fmt.Errorf("next ui text version: %w", err)
	}
	return version, nil
}

// CreateVersion stores a new draft version and its entries atomically.
func (r *PostgresRepository) CreateVersion(
	ctx context.Context,
	input domain.DraftInput,
	packageVersion int,
	actorID string,
	now time.Time,
) (*domain.PackageDetail, error) {
	transaction, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin ui text create: %w", err)
	}
	defer func() {
		_ = transaction.Rollback(context.Background())
	}()
	_, err = transaction.Exec(ctx, `
		INSERT INTO ui_text_package_versions (
			package_id, package_version, language, platform, min_client_version,
			status, created_by, created_at, updated_at
		) VALUES ($1, $2, $3, $4, $5, 'draft', $6, $7, $7)
	`,
		input.PackageID,
		packageVersion,
		input.Language,
		input.Platform,
		input.MinClientVersion,
		nullableActor(actorID),
		now,
	)
	if err != nil {
		if isUniqueViolation(err) {
			return nil, domain.ErrVersionExists
		}
		return nil, fmt.Errorf("create ui text version: %w", err)
	}
	if err := insertEntries(
		ctx,
		transaction,
		input.PackageID,
		input.Language,
		input.Platform,
		packageVersion,
		input.Entries,
	); err != nil {
		return nil, err
	}
	if err := transaction.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit ui text create: %w", err)
	}
	return r.GetVersion(ctx, input.PackageID, input.Language, input.Platform, packageVersion)
}

// UpdateDraft replaces the entries and metadata of one draft version.
func (r *PostgresRepository) UpdateDraft(
	ctx context.Context,
	input domain.DraftInput,
	packageVersion int,
	now time.Time,
) (*domain.PackageDetail, error) {
	transaction, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin ui text draft update: %w", err)
	}
	defer func() {
		_ = transaction.Rollback(context.Background())
	}()
	tag, err := transaction.Exec(ctx, `
		UPDATE ui_text_package_versions
		SET min_client_version = $5,
		    updated_at = $6
		WHERE package_id = $1
		  AND language = $2
		  AND platform = $3
		  AND package_version = $4
		  AND status = 'draft'
	`,
		input.PackageID,
		input.Language,
		input.Platform,
		packageVersion,
		input.MinClientVersion,
		now,
	)
	if err != nil {
		return nil, fmt.Errorf("update ui text draft: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return nil, domain.ErrInvalidTransition
	}
	if _, err := transaction.Exec(ctx, `
		DELETE FROM ui_text_entries
		WHERE package_id = $1
		  AND language = $2
		  AND platform = $3
		  AND package_version = $4
	`, input.PackageID, input.Language, input.Platform, packageVersion); err != nil {
		return nil, fmt.Errorf("clear ui text draft entries: %w", err)
	}
	if err := insertEntries(
		ctx,
		transaction,
		input.PackageID,
		input.Language,
		input.Platform,
		packageVersion,
		input.Entries,
	); err != nil {
		return nil, err
	}
	if err := transaction.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit ui text draft update: %w", err)
	}
	return r.GetVersion(ctx, input.PackageID, input.Language, input.Platform, packageVersion)
}

// GetVersion returns one stored version with its ordered entries.
func (r *PostgresRepository) GetVersion(
	ctx context.Context,
	packageID string,
	language string,
	platform domain.Platform,
	packageVersion int,
) (*domain.PackageDetail, error) {
	detail := &domain.PackageDetail{}
	err := r.pool.QueryRow(ctx, `
		SELECT `+packageColumns+`
		FROM ui_text_package_versions
		WHERE package_id = $1 AND language = $2 AND platform = $3 AND package_version = $4
	`, packageID, language, platform, packageVersion).Scan(packageScanTargets(&detail.Package)...)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, domain.ErrPackageNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("read ui text version: %w", err)
	}
	entries, err := r.entriesFor(
		ctx,
		packageID,
		language,
		platform,
		packageVersion,
	)
	if err != nil {
		return nil, err
	}
	detail.Entries = entries
	return detail, nil
}

// ListVersions returns one operator page of version summaries plus the total.
func (r *PostgresRepository) ListVersions(
	ctx context.Context,
	filter domain.VersionFilter,
) ([]domain.PackageVersion, int, error) {
	page := filter.Page
	if page < 1 {
		page = 1
	}
	pageSize := filter.PageSize
	if pageSize < 1 {
		pageSize = domain.DefaultPageSize
	}
	if pageSize > domain.MaxPageSize {
		pageSize = domain.MaxPageSize
	}
	clauses := make([]string, 0, 4)
	arguments := make([]any, 0, 6)
	if filter.PackageID != "" {
		arguments = append(arguments, filter.PackageID)
		clauses = append(clauses, fmt.Sprintf("package_id = $%d", len(arguments)))
	}
	if filter.Language != "" {
		arguments = append(arguments, filter.Language)
		clauses = append(clauses, fmt.Sprintf("language = $%d", len(arguments)))
	}
	if filter.Platform != "" {
		arguments = append(arguments, filter.Platform)
		clauses = append(clauses, fmt.Sprintf("platform = $%d", len(arguments)))
	}
	if filter.Status != "" {
		arguments = append(arguments, filter.Status)
		clauses = append(clauses, fmt.Sprintf("status = $%d", len(arguments)))
	}
	where := ""
	if len(clauses) > 0 {
		where = " WHERE " + joinClauses(clauses)
	}
	var total int
	if err := r.pool.QueryRow(
		ctx,
		"SELECT COUNT(*) FROM ui_text_package_versions"+where,
		arguments...,
	).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count ui text versions: %w", err)
	}
	listArguments := append(append([]any(nil), arguments...), pageSize, (page-1)*pageSize)
	rows, err := r.pool.Query(ctx, `
		SELECT `+packageColumns+`,
		       (SELECT COUNT(*) FROM ui_text_entries AS entry
		        WHERE entry.package_id = ui_text_package_versions.package_id
		          AND entry.language = ui_text_package_versions.language
		          AND entry.platform = ui_text_package_versions.platform
		          AND entry.package_version = ui_text_package_versions.package_version)
		FROM ui_text_package_versions`+where+`
		ORDER BY updated_at DESC, package_version DESC
		LIMIT $`+fmt.Sprint(len(listArguments)-1)+` OFFSET $`+fmt.Sprint(len(listArguments)),
		listArguments...,
	)
	if err != nil {
		return nil, 0, fmt.Errorf("list ui text versions: %w", err)
	}
	defer rows.Close()
	items := make([]domain.PackageVersion, 0, pageSize)
	for rows.Next() {
		var item domain.PackageVersion
		targets := append(packageScanTargets(&item.Package), &item.EntryCount)
		if err := rows.Scan(targets...); err != nil {
			return nil, 0, fmt.Errorf("scan ui text version: %w", err)
		}
		item.IsPublished = item.Package.Status == domain.StatusPublished
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("iterate ui text versions: %w", err)
	}
	return items, total, nil
}

// ApplyTransition commits one lifecycle move. Publishing also withdraws the
// superseded publication and advances the catalog revision in the same
// transaction so a client never observes two published rows.
func (r *PostgresRepository) ApplyTransition(
	ctx context.Context,
	transition domain.TransitionInput,
	now time.Time,
) (*domain.Package, error) {
	transaction, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin ui text transition: %w", err)
	}
	defer func() {
		_ = transaction.Rollback(context.Background())
	}()
	var revision int64
	switch transition.Action {
	case domain.ActionSubmit:
		_, err = transaction.Exec(ctx, `
			UPDATE ui_text_package_versions
			SET status = 'in_review', submitted_at = $5, updated_at = $5
			WHERE package_id = $1 AND language = $2 AND platform = $3
			  AND package_version = $4 AND status = 'draft'
		`, transition.PackageID, transition.Language, transition.Platform, transition.PackageVersion, now)
	case domain.ActionApprove:
		_, err = transaction.Exec(ctx, `
			UPDATE ui_text_package_versions
			SET approved_at = $5, approved_by = $6, updated_at = $5
			WHERE package_id = $1 AND language = $2 AND platform = $3
			  AND package_version = $4 AND status = 'in_review'
		`, transition.PackageID, transition.Language, transition.Platform,
			transition.PackageVersion, now, nullableActor(transition.ActorID))
	case domain.ActionReject:
		_, err = transaction.Exec(ctx, `
			UPDATE ui_text_package_versions
			SET status = 'draft', submitted_at = NULL, updated_at = $5
			WHERE package_id = $1 AND language = $2 AND platform = $3
			  AND package_version = $4 AND status = 'in_review'
		`, transition.PackageID, transition.Language, transition.Platform, transition.PackageVersion, now)
	case domain.ActionWithdraw:
		_, err = transaction.Exec(ctx, `
			UPDATE ui_text_package_versions
			SET status = 'withdrawn', withdrawn_at = $5, updated_at = $5
			WHERE package_id = $1 AND language = $2 AND platform = $3
			  AND package_version = $4 AND status = 'published'
		`, transition.PackageID, transition.Language, transition.Platform, transition.PackageVersion, now)
	case domain.ActionPublish:
		revision, err = nextRevision(ctx, transaction)
		if err == nil {
			_, err = transaction.Exec(ctx, `
				UPDATE ui_text_package_versions
				SET status = 'withdrawn', withdrawn_at = $5, updated_at = $5
				WHERE package_id = $1 AND language = $2 AND platform = $3
				  AND status = 'published'
			`, transition.PackageID, transition.Language, transition.Platform, transition.PackageVersion, now)
		}
		if err == nil {
			_, err = transaction.Exec(ctx, `
				UPDATE ui_text_package_versions
				SET status = 'published',
				    published_at = $5,
				    published_by = $6,
				    signature_key_id = $7,
				    signature = $8,
				    revision = $9,
				    updated_at = $5
				WHERE package_id = $1 AND language = $2 AND platform = $3
				  AND package_version = $4 AND status = 'in_review'
			`, transition.PackageID, transition.Language, transition.Platform,
				transition.PackageVersion, now, nullableActor(transition.ActorID),
				transition.SignatureKeyID, transition.Signature, revision)
		}
	default:
		return nil, domain.ErrInvalidTransition
	}
	if err != nil {
		return nil, fmt.Errorf("apply ui text transition: %w", err)
	}
	detail, err := getVersionTx(
		ctx,
		transaction,
		transition.PackageID,
		transition.Language,
		transition.Platform,
		transition.PackageVersion,
	)
	if err != nil {
		return nil, err
	}
	if err := transaction.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit ui text transition: %w", err)
	}
	return &detail.Package, nil
}

// CurrentRevision returns the published-catalog cursor without listing.
func (r *PostgresRepository) CurrentRevision(ctx context.Context) (int64, error) {
	return currentRevision(ctx, r.pool)
}

// ListPublished returns published packages newer than one revision.
func (r *PostgresRepository) ListPublished(
	ctx context.Context,
	sinceRevision int64,
	limit int,
) (domain.RevisionPage, error) {
	if limit < 1 {
		limit = domain.DefaultPageSize
	}
	if limit > domain.MaxPageSize {
		limit = domain.MaxPageSize
	}
	revision, err := currentRevision(ctx, r.pool)
	if err != nil {
		return domain.RevisionPage{}, err
	}
	rows, err := r.pool.Query(ctx, `
		SELECT `+packageColumns+`
		FROM ui_text_package_versions
		WHERE status = 'published' AND revision > $1
		ORDER BY revision
		LIMIT $2
	`, sinceRevision, limit+1)
	if err != nil {
		return domain.RevisionPage{}, fmt.Errorf("list published ui text: %w", err)
	}
	defer rows.Close()
	packages := make([]domain.Package, 0, limit)
	for rows.Next() {
		var item domain.Package
		if err := rows.Scan(packageScanTargets(&item)...); err != nil {
			return domain.RevisionPage{}, fmt.Errorf("scan published ui text: %w", err)
		}
		packages = append(packages, item)
	}
	if err := rows.Err(); err != nil {
		return domain.RevisionPage{}, fmt.Errorf("iterate published ui text: %w", err)
	}
	hasMore := len(packages) > limit
	if hasMore {
		packages = packages[:limit]
	}
	return domain.RevisionPage{
		Revision: revision,
		Packages: packages,
		HasMore:  hasMore,
	}, nil
}

// PublishedPackage returns the currently published version of one package.
func (r *PostgresRepository) PublishedPackage(
	ctx context.Context,
	packageID string,
	language string,
	platform domain.Platform,
) (*domain.PublishedPackage, error) {
	detail := &domain.PackageDetail{}
	err := r.pool.QueryRow(ctx, `
		SELECT `+packageColumns+`
		FROM ui_text_package_versions
		WHERE package_id = $1 AND language = $2 AND platform = $3
		  AND status = 'published'
	`, packageID, language, platform).Scan(packageScanTargets(&detail.Package)...)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, domain.ErrPackageNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("read published ui text package: %w", err)
	}
	entries, err := r.entriesFor(
		ctx,
		packageID,
		language,
		platform,
		detail.Package.PackageVersion,
	)
	if err != nil {
		return nil, err
	}
	return &domain.PublishedPackage{Package: detail.Package, Entries: entries}, nil
}

func (r *PostgresRepository) entriesFor(
	ctx context.Context,
	packageID string,
	language string,
	platform domain.Platform,
	packageVersion int,
) ([]domain.Entry, error) {
	return queryEntries(
		ctx,
		r.pool,
		packageID,
		language,
		platform,
		packageVersion,
	)
}

func getVersionTx(
	ctx context.Context,
	transaction pgx.Tx,
	packageID string,
	language string,
	platform domain.Platform,
	packageVersion int,
) (*domain.PackageDetail, error) {
	detail := &domain.PackageDetail{}
	err := transaction.QueryRow(ctx, `
		SELECT `+packageColumns+`
		FROM ui_text_package_versions
		WHERE package_id = $1 AND language = $2 AND platform = $3 AND package_version = $4
		FOR UPDATE
	`, packageID, language, platform, packageVersion).Scan(packageScanTargets(&detail.Package)...)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, domain.ErrPackageNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("read ui text version in transaction: %w", err)
	}
	entries, err := queryEntries(
		ctx,
		transaction,
		packageID,
		language,
		platform,
		packageVersion,
	)
	if err != nil {
		return nil, err
	}
	detail.Entries = entries
	return detail, nil
}

type entryQuerier interface {
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
}

func queryEntries(
	ctx context.Context,
	querier entryQuerier,
	packageID string,
	language string,
	platform domain.Platform,
	packageVersion int,
) ([]domain.Entry, error) {
	rows, err := querier.Query(ctx, `
		SELECT ui_text_key, value, max_length, variables
		FROM ui_text_entries
		WHERE package_id = $1 AND language = $2 AND platform = $3 AND package_version = $4
		ORDER BY ui_text_key
	`, packageID, language, platform, packageVersion)
	if err != nil {
		return nil, fmt.Errorf("list ui text entries: %w", err)
	}
	defer rows.Close()
	entries := make([]domain.Entry, 0, 16)
	for rows.Next() {
		var entry domain.Entry
		if err := rows.Scan(
			&entry.UITextKey,
			&entry.Value,
			&entry.MaxLength,
			&entry.Variables,
		); err != nil {
			return nil, fmt.Errorf("scan ui text entry: %w", err)
		}
		if entry.Variables == nil {
			entry.Variables = []string{}
		}
		entries = append(entries, entry)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate ui text entries: %w", err)
	}
	return entries, nil
}

func insertEntries(
	ctx context.Context,
	transaction pgx.Tx,
	packageID string,
	language string,
	platform domain.Platform,
	packageVersion int,
	entries []domain.Entry,
) error {
	for _, entry := range entries {
		if _, err := transaction.Exec(ctx, `
			INSERT INTO ui_text_entries (
				package_id, language, platform, package_version,
				ui_text_key, value, max_length, variables
			) VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		`,
			packageID,
			language,
			platform,
			packageVersion,
			entry.UITextKey,
			entry.Value,
			entry.MaxLength,
			entry.Variables,
		); err != nil {
			if isUniqueViolation(err) {
				return domain.ErrVersionExists
			}
			return fmt.Errorf("insert ui text entry: %w", err)
		}
	}
	return nil
}

func packageScanTargets(item *domain.Package) []any {
	return []any{
		&item.PackageID,
		&item.PackageVersion,
		&item.Language,
		&item.Platform,
		&item.MinClientVersion,
		&item.Status,
		&item.SignatureKeyID,
		&item.Signature,
		&item.SubmittedAt,
		&item.ApprovedAt,
		&item.ApprovedBy,
		&item.PublishedAt,
		&item.PublishedBy,
		&item.WithdrawnAt,
		&item.Revision,
		&item.CreatedBy,
		&item.CreatedAt,
		&item.UpdatedAt,
	}
}

func nextRevision(ctx context.Context, transaction pgx.Tx) (int64, error) {
	var revision int64
	if err := transaction.QueryRow(ctx, `
		INSERT INTO ui_text_catalog_clock (singleton, revision, updated_at)
		VALUES (TRUE, 1, NOW())
		ON CONFLICT (singleton)
		DO UPDATE SET revision = ui_text_catalog_clock.revision + 1, updated_at = NOW()
		RETURNING revision
	`).Scan(&revision); err != nil {
		return 0, fmt.Errorf("increment ui text revision: %w", err)
	}
	return revision, nil
}

func currentRevision(ctx context.Context, pool *pgxpool.Pool) (int64, error) {
	var revision int64
	if err := pool.QueryRow(ctx, `
		SELECT COALESCE((SELECT revision FROM ui_text_catalog_clock WHERE singleton), 0)
	`).Scan(&revision); err != nil {
		return 0, fmt.Errorf("read ui text revision: %w", err)
	}
	return revision, nil
}

func nullableActor(actorID string) any {
	if actorID == "" {
		return nil
	}
	return actorID
}

func joinClauses(clauses []string) string {
	combined := ""
	for index, clause := range clauses {
		if index > 0 {
			combined += " AND "
		}
		combined += clause
	}
	return combined
}

func isUniqueViolation(err error) bool {
	type sqlStateError interface {
		SQLState() string
	}
	var stateError sqlStateError
	return errors.As(err, &stateError) && stateError.SQLState() == "23505"
}
