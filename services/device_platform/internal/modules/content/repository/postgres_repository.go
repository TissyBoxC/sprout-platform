package repository

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/content/domain"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// PostgresRepository persists content packages in device_platform's database.
type PostgresRepository struct {
	pool *pgxpool.Pool
}

// NewPostgresRepository creates the durable content repository.
func NewPostgresRepository(pool *pgxpool.Pool) *PostgresRepository {
	return &PostgresRepository{pool: pool}
}

// List returns the filtered package list.
func (r *PostgresRepository) List(
	ctx context.Context,
	filter domain.PackageListFilter,
) (domain.PackagePage, error) {
	conditions := []string{"1 = 1"}
	args := make([]any, 0, 6)
	add := func(condition string, value any) {
		args = append(args, value)
		conditions = append(conditions, fmt.Sprintf(condition, len(args)))
	}
	if filter.Category != "" {
		add("category = $%d", filter.Category)
	}
	if filter.AgeTier != "" {
		add("$%d = ANY(age_tiers)", filter.AgeTier)
	}
	if filter.Status != "" {
		add("status = $%d", filter.Status)
	}
	if filter.Keyword != "" {
		keywordPattern := "%" + filter.Keyword + "%"
		args = append(args, keywordPattern, keywordPattern)
		conditions = append(conditions, fmt.Sprintf(
			"(package_id ILIKE $%d OR title ILIKE $%d)",
			len(args)-1,
			len(args),
		))
	}
	where := strings.Join(conditions, " AND ")
	var total int
	if err := r.pool.QueryRow(
		ctx,
		"SELECT COUNT(*) FROM content_package_versions WHERE "+where,
		args...,
	).Scan(&total); err != nil {
		return domain.PackagePage{}, fmt.Errorf("count content packages: %w", err)
	}
	offset := (filter.Page - 1) * filter.PageSize
	pageArgs := append(append([]any{}, args...), filter.PageSize, offset)
	query := `SELECT ` + packageVersionColumns + `
		FROM content_package_versions
		WHERE ` + where + `
		ORDER BY updated_at DESC, package_id ASC, package_version DESC
		LIMIT $` + fmt.Sprint(len(pageArgs)-1) + ` OFFSET $` + fmt.Sprint(len(pageArgs))
	rows, err := r.pool.Query(ctx, query, pageArgs...)
	if err != nil {
		return domain.PackagePage{}, fmt.Errorf("list content packages: %w", err)
	}
	defer rows.Close()
	packages := make([]domain.PackageVersion, 0)
	for rows.Next() {
		item, scanErr := scanPackageVersion(rows)
		if scanErr != nil {
			return domain.PackagePage{}, scanErr
		}
		packages = append(packages, *item)
	}
	if err := rows.Err(); err != nil {
		return domain.PackagePage{}, fmt.Errorf("iterate content packages: %w", err)
	}
	return domain.PackagePage{
		Packages: packages,
		Page:     filter.Page,
		PageSize: filter.PageSize,
		Total:    total,
	}, nil
}

// GetDetail returns all versions and review history for one package.
func (r *PostgresRepository) GetDetail(
	ctx context.Context,
	packageID string,
) (*domain.PackageDetail, error) {
	if strings.TrimSpace(packageID) == "" {
		return nil, domain.ErrPackageNotFound
	}
	rows, err := r.pool.Query(
		ctx,
		`SELECT `+packageVersionColumns+`
		 FROM content_package_versions
		 WHERE package_id = $1
		 ORDER BY package_version DESC`,
		packageID,
	)
	if err != nil {
		return nil, fmt.Errorf("get content package versions: %w", err)
	}
	defer rows.Close()
	versions := make([]domain.PackageVersion, 0)
	for rows.Next() {
		item, scanErr := scanPackageVersion(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		versions = append(versions, *item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate content package versions: %w", err)
	}
	if len(versions) == 0 {
		return nil, domain.ErrPackageNotFound
	}
	logRows, err := r.pool.Query(
		ctx,
		`SELECT id::text, package_id, package_version, action, from_status,
		        to_status, reason, actor_account_id, created_at
		 FROM content_review_logs
		 WHERE package_id = $1
		 ORDER BY created_at DESC, id DESC`,
		packageID,
	)
	if err != nil {
		return nil, fmt.Errorf("get content review history: %w", err)
	}
	defer logRows.Close()
	history := make([]domain.ReviewLog, 0)
	for logRows.Next() {
		var item domain.ReviewLog
		if err := logRows.Scan(
			&item.ID,
			&item.PackageID,
			&item.PackageVersion,
			&item.Action,
			&item.FromStatus,
			&item.ToStatus,
			&item.Reason,
			&item.ActorAccountID,
			&item.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan content review history: %w", err)
		}
		item.CreatedAt = item.CreatedAt.UTC()
		history = append(history, item)
	}
	if err := logRows.Err(); err != nil {
		return nil, fmt.Errorf("iterate content review history: %w", err)
	}
	return &domain.PackageDetail{
		PackageID: packageID,
		Versions:  versions,
		History:   history,
	}, nil
}

// GetVersion loads one package version or reports ErrPackageNotFound.
func (r *PostgresRepository) GetVersion(
	ctx context.Context,
	packageID string,
	packageVersion int,
) (*domain.PackageVersion, error) {
	row := r.pool.QueryRow(
		ctx,
		`SELECT `+packageVersionColumns+`
		 FROM content_package_versions
		 WHERE package_id = $1 AND package_version = $2`,
		packageID,
		packageVersion,
	)
	item, err := scanPackageVersion(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, domain.ErrPackageNotFound
	}
	return item, err
}

// CreateDraft inserts version 1 for a new package.
func (r *PostgresRepository) CreateDraft(
	ctx context.Context,
	input domain.MetadataInput,
	now time.Time,
) (*domain.PackageVersion, error) {
	item := &domain.PackageVersion{
		PackageID:      input.PackageID,
		PackageVersion: 1,
		Title:          input.Title,
		Category:       input.Category,
		AgeTiers:       input.AgeTiers,
		AssetKey:       input.AssetKey,
		SHA256:         input.SHA256,
		SizeBytes:      input.SizeBytes,
		Status:         domain.StatusDraft,
		CreatedAt:      now,
		UpdatedAt:      now,
	}
	_, err := r.pool.Exec(
		ctx,
		`INSERT INTO content_package_versions (
			package_id, package_version, title, category, age_tiers, asset_key,
			sha256, size_bytes, status, created_at, updated_at
		 ) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)`,
		item.PackageID,
		item.PackageVersion,
		item.Title,
		item.Category,
		ageTierValues(input.AgeTiers),
		item.AssetKey,
		item.SHA256,
		item.SizeBytes,
		item.Status,
		item.CreatedAt,
		item.UpdatedAt,
	)
	if err != nil {
		if isUniqueViolation(err) {
			return nil, domain.ErrVersionExists
		}
		return nil, fmt.Errorf("create content draft: %w", err)
	}
	return item, nil
}

// CreateVersion inserts the next draft version for an existing package.
//
// The caller computes the candidate number, but the primary key remains the
// final race guard. A concurrent operator receives ErrVersionExists and can
// reload the package before retrying.
func (r *PostgresRepository) CreateVersion(
	ctx context.Context,
	input domain.MetadataInput,
	packageVersion int,
	now time.Time,
) (*domain.PackageVersion, error) {
	item := &domain.PackageVersion{
		PackageID:      input.PackageID,
		PackageVersion: packageVersion,
		Title:          input.Title,
		Category:       input.Category,
		AgeTiers:       input.AgeTiers,
		AssetKey:       input.AssetKey,
		SHA256:         input.SHA256,
		SizeBytes:      input.SizeBytes,
		Status:         domain.StatusDraft,
		CreatedAt:      now,
		UpdatedAt:      now,
	}
	_, err := r.pool.Exec(
		ctx,
		`INSERT INTO content_package_versions (
			package_id, package_version, title, category, age_tiers, asset_key,
			sha256, size_bytes, status, created_at, updated_at
		 ) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)`,
		item.PackageID,
		item.PackageVersion,
		item.Title,
		item.Category,
		ageTierValues(item.AgeTiers),
		item.AssetKey,
		item.SHA256,
		item.SizeBytes,
		item.Status,
		item.CreatedAt,
		item.UpdatedAt,
	)
	if err != nil {
		if isUniqueViolation(err) {
			return nil, domain.ErrVersionExists
		}
		return nil, fmt.Errorf("create content version: %w", err)
	}
	return item, nil
}

// UpdateDraft updates mutable draft metadata.
func (r *PostgresRepository) UpdateDraft(
	ctx context.Context,
	packageID string,
	packageVersion int,
	input domain.MetadataInput,
	now time.Time,
) (*domain.PackageVersion, error) {
	row := r.pool.QueryRow(
		ctx,
		`UPDATE content_package_versions
		 SET title = $3, category = $4, age_tiers = $5, asset_key = $6,
		     sha256 = $7, size_bytes = $8, updated_at = $9
		 WHERE package_id = $1 AND package_version = $2 AND status = 'draft'
		 RETURNING `+packageVersionColumns,
		packageID,
		packageVersion,
		input.Title,
		input.Category,
		ageTierValues(input.AgeTiers),
		input.AssetKey,
		input.SHA256,
		input.SizeBytes,
		now,
	)
	item, err := scanPackageVersion(row)
	if errors.Is(err, pgx.ErrNoRows) {
		current, currentErr := r.GetVersion(ctx, packageID, packageVersion)
		if currentErr != nil {
			return nil, currentErr
		}
		return nil, &domain.TransitionError{
			Current: current,
			Err:     domain.ErrInvalidTransition,
		}
	}
	return item, err
}

// ApplyTransition commits a lifecycle move, its audit log, and revision bump.
func (r *PostgresRepository) ApplyTransition(
	ctx context.Context,
	transition domain.Transition,
) (*domain.PackageVersion, error) {
	transaction, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin content transition: %w", err)
	}
	defer func() {
		_ = transaction.Rollback(context.Background())
	}()
	revision, err := nextCatalogRevision(ctx, transaction)
	if err != nil {
		return nil, err
	}
	if transition.Action == domain.ActionPublish {
		archivedVersions, err := archiveOtherPublished(
			ctx,
			transaction,
			revision,
			transition,
		)
		if err != nil {
			return nil, err
		}
		for _, archivedVersion := range archivedVersions {
			if _, err := transaction.Exec(
				ctx,
				`INSERT INTO content_review_logs (
					id, package_id, package_version, action, from_status,
					to_status, reason, actor_account_id, created_at
				 ) VALUES ($1,$2,$3,'archive','published','archived',$4,$5,$6)`,
				uuid.NewString(),
				archivedVersion.PackageID,
				archivedVersion.PackageVersion,
				"",
				transition.ActorAccountID,
				transition.Now,
			); err != nil {
				return nil, fmt.Errorf("record superseded content review log: %w", err)
			}
		}
	}
	row := transaction.QueryRow(
		ctx,
		`UPDATE content_package_versions
		 SET status = $3,
		     submitted_at = CASE WHEN $4 = 'submit' THEN $5 ELSE submitted_at END,
		     approved_at = CASE WHEN $4 = 'approve' THEN $5 ELSE approved_at END,
		     approved_by = CASE WHEN $4 = 'approve' THEN $6 ELSE approved_by END,
		     published_at = CASE WHEN $4 = 'publish' THEN $5 ELSE published_at END,
		     catalog_revision = CASE
		         WHEN $4 IN ('publish', 'withdraw', 'archive') THEN $7
		         ELSE catalog_revision
		     END,
		     updated_at = $5
		 WHERE package_id = $1 AND package_version = $2
		   AND status = $8
		 RETURNING `+packageVersionColumns,
		transition.PackageID,
		transition.PackageVersion,
		transition.ToStatus,
		transition.Action,
		transition.Now,
		transition.ActorAccountID,
		revision,
		transition.FromStatus,
	)
	item, err := scanPackageVersion(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, &domain.TransitionError{
			Err: domain.ErrInvalidTransition,
		}
	}
	if err != nil {
		return nil, err
	}
	if _, err := transaction.Exec(
		ctx,
		`INSERT INTO content_review_logs (
			id, package_id, package_version, action, from_status, to_status,
			reason, actor_account_id, created_at
		 ) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)`,
		uuid.NewString(),
		transition.PackageID,
		transition.PackageVersion,
		transition.Action,
		transition.FromStatus,
		transition.ToStatus,
		transition.Reason,
		transition.ActorAccountID,
		transition.Now,
	); err != nil {
		return nil, fmt.Errorf("record content review log: %w", err)
	}
	if err := transaction.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit content transition: %w", err)
	}
	return item, nil
}

// CatalogRevision returns the current delivery cursor. It lets the device
// path preserve a valid revision when policy filtering yields no categories.
func (r *PostgresRepository) CatalogRevision(
	ctx context.Context,
) (int64, error) {
	return currentCatalogRevision(ctx, r.pool)
}

// Catalog returns published packages changed after the supplied revision.
func (r *PostgresRepository) Catalog(
	ctx context.Context,
	query domain.CatalogQuery,
) (*domain.Catalog, error) {
	revision, err := currentCatalogRevision(ctx, r.pool)
	if err != nil {
		return nil, err
	}
	if query.SinceRevision > revision {
		return nil, domain.ErrRevisionAhead
	}
	conditions := []string{"status = 'published'", "catalog_revision > $1"}
	args := []any{query.SinceRevision}
	if query.AgeTier != "" {
		args = append(args, query.AgeTier)
		conditions = append(
			conditions,
			fmt.Sprintf("$%d = ANY(age_tiers)", len(args)),
		)
	}
	if query.Category != "" {
		args = append(args, query.Category)
		conditions = append(conditions, fmt.Sprintf("category = $%d", len(args)))
	}
	if len(query.AllowedCategories) > 0 {
		args = append(args, query.AllowedCategories)
		conditions = append(
			conditions,
			fmt.Sprintf("category = ANY($%d)", len(args)),
		)
	}
	rows, err := r.pool.Query(
		ctx,
		`SELECT `+packageVersionColumns+`
		 FROM content_package_versions
		 WHERE `+strings.Join(conditions, " AND ")+`
		 ORDER BY package_id ASC`,
		args...,
	)
	if err != nil {
		return nil, fmt.Errorf("read content catalog: %w", err)
	}
	defer rows.Close()
	packages := make([]domain.PackageVersion, 0)
	for rows.Next() {
		item, scanErr := scanPackageVersion(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		packages = append(packages, *item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate content catalog: %w", err)
	}
	withdrawnRows, err := r.pool.Query(
		ctx,
		`SELECT DISTINCT package_id
		 FROM content_package_versions
		 WHERE status IN ('withdrawn', 'archived')
		   AND catalog_revision > $1
		   AND NOT EXISTS (
		       SELECT 1
		       FROM content_package_versions AS current_version
		       WHERE current_version.package_id =
		             content_package_versions.package_id
		         AND current_version.status = 'published'
		   )
		 ORDER BY package_id ASC`,
		query.SinceRevision,
	)
	if err != nil {
		return nil, fmt.Errorf("read withdrawn content: %w", err)
	}
	defer withdrawnRows.Close()
	withdrawn := make([]string, 0)
	for withdrawnRows.Next() {
		var packageID string
		if err := withdrawnRows.Scan(&packageID); err != nil {
			return nil, fmt.Errorf("scan withdrawn content: %w", err)
		}
		withdrawn = append(withdrawn, packageID)
	}
	if err := withdrawnRows.Err(); err != nil {
		return nil, fmt.Errorf("iterate withdrawn content: %w", err)
	}
	return &domain.Catalog{
		Revision:            revision,
		Packages:            packages,
		WithdrawnPackageIDs: withdrawn,
	}, nil
}

// PublishedDownload resolves the single published version of a package.
func (r *PostgresRepository) PublishedDownload(
	ctx context.Context,
	packageID string,
) (*domain.PackageVersion, error) {
	row := r.pool.QueryRow(
		ctx,
		`SELECT `+packageVersionColumns+`
		 FROM content_package_versions
		 WHERE package_id = $1 AND status = 'published'
		 ORDER BY package_version DESC
		 LIMIT 1`,
		packageID,
	)
	item, err := scanPackageVersion(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, domain.ErrPackageNotFound
	}
	return item, err
}

const packageVersionColumns = `
	package_id,
	package_version,
	title,
	category,
	age_tiers,
	asset_key,
	sha256,
	size_bytes,
	status,
	submitted_at,
	approved_at,
	approved_by::text,
	published_at,
	catalog_revision,
	created_at,
	updated_at
`

type rowScanner interface {
	Scan(dest ...any) error
}

// ageTierValues converts the domain age-tier slice into the plain string
// slice pgx binds to a TEXT[] column.
func ageTierValues(values []domain.AgeTier) []string {
	converted := make([]string, 0, len(values))
	for _, value := range values {
		converted = append(converted, string(value))
	}
	return converted
}

func scanPackageVersion(row rowScanner) (*domain.PackageVersion, error) {
	var item domain.PackageVersion
	var approvedBy *string
	if err := row.Scan(
		&item.PackageID,
		&item.PackageVersion,
		&item.Title,
		&item.Category,
		&item.AgeTiers,
		&item.AssetKey,
		&item.SHA256,
		&item.SizeBytes,
		&item.Status,
		&item.SubmittedAt,
		&item.ApprovedAt,
		&approvedBy,
		&item.PublishedAt,
		&item.CatalogRevision,
		&item.CreatedAt,
		&item.UpdatedAt,
	); err != nil {
		return nil, err
	}
	if approvedBy != nil {
		item.ApprovedBy = strings.TrimSpace(*approvedBy)
	}
	item.CreatedAt = item.CreatedAt.UTC()
	item.UpdatedAt = item.UpdatedAt.UTC()
	if item.SubmittedAt != nil {
		value := item.SubmittedAt.UTC()
		item.SubmittedAt = &value
	}
	if item.ApprovedAt != nil {
		value := item.ApprovedAt.UTC()
		item.ApprovedAt = &value
	}
	if item.PublishedAt != nil {
		value := item.PublishedAt.UTC()
		item.PublishedAt = &value
	}
	return &item, nil
}

func archiveOtherPublished(
	ctx context.Context,
	transaction pgx.Tx,
	revision int64,
	transition domain.Transition,
) ([]domain.PackageVersion, error) {
	rows, err := transaction.Query(
		ctx,
		`SELECT `+packageVersionColumns+`
		 FROM content_package_versions
		 WHERE package_id = $1
		   AND package_version <> $2
		   AND status = 'published'`,
		transition.PackageID,
		transition.PackageVersion,
	)
	if err != nil {
		return nil, fmt.Errorf("read superseded content: %w", err)
	}
	defer rows.Close()
	archived := make([]domain.PackageVersion, 0)
	for rows.Next() {
		version, scanErr := scanPackageVersion(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		archived = append(archived, *version)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate superseded content: %w", err)
	}
	if len(archived) > 0 {
		if _, err := transaction.Exec(
			ctx,
			`UPDATE content_package_versions
			 SET status = 'archived', catalog_revision = $3, updated_at = $4
			 WHERE package_id = $1
			   AND package_version <> $2
			   AND status = 'published'`,
			transition.PackageID,
			transition.PackageVersion,
			revision,
			transition.Now,
		); err != nil {
			return nil, fmt.Errorf("archive superseded content: %w", err)
		}
	}
	return archived, nil
}

func nextCatalogRevision(
	ctx context.Context,
	transaction pgx.Tx,
) (int64, error) {
	var revision int64
	if err := transaction.QueryRow(
		ctx,
		`INSERT INTO content_catalog_revision (singleton, revision, updated_at)
		 VALUES (TRUE, 1, NOW())
		 ON CONFLICT (singleton)
		 DO UPDATE SET revision = content_catalog_revision.revision + 1,
		               updated_at = NOW()
		 RETURNING revision`,
	).Scan(&revision); err != nil {
		return 0, fmt.Errorf("increment content catalog revision: %w", err)
	}
	return revision, nil
}

func currentCatalogRevision(
	ctx context.Context,
	pool *pgxpool.Pool,
) (int64, error) {
	var revision int64
	if err := pool.QueryRow(
		ctx,
		`SELECT COALESCE(
			(SELECT revision FROM content_catalog_revision WHERE singleton),
			0
		 )`,
	).Scan(&revision); err != nil {
		return 0, fmt.Errorf("read content catalog revision: %w", err)
	}
	return revision, nil
}

func isUniqueViolation(err error) bool {
	type sqlStateError interface {
		SQLState() string
	}
	var stateError sqlStateError
	return errors.As(err, &stateError) && stateError.SQLState() == "23505"
}

// AddFavorite records one guardian bookmark. A repeated add is reported as
// ErrFavoriteExists so the HTTP layer can answer idempotently.
func (r *PostgresRepository) AddFavorite(
	ctx context.Context,
	parentAccountID string,
	packageID string,
	now time.Time,
) (domain.Favorite, error) {
	var createdAt time.Time
	err := r.pool.QueryRow(
		ctx,
		`INSERT INTO content_favorites (parent_account_id, package_id, created_at)
		 VALUES ($1, $2, $3)
		 RETURNING created_at`,
		parentAccountID,
		packageID,
		now,
	).Scan(&createdAt)
	if err != nil {
		if isUniqueViolation(err) {
			return domain.Favorite{}, domain.ErrFavoriteExists
		}
		return domain.Favorite{}, fmt.Errorf("add content favorite: %w", err)
	}
	return domain.Favorite{PackageID: packageID, CreatedAt: createdAt}, nil
}

// RemoveFavorite deletes one bookmark and reports whether a row was removed.
func (r *PostgresRepository) RemoveFavorite(
	ctx context.Context,
	parentAccountID string,
	packageID string,
) (bool, error) {
	tag, err := r.pool.Exec(
		ctx,
		`DELETE FROM content_favorites
		 WHERE parent_account_id = $1 AND package_id = $2`,
		parentAccountID,
		packageID,
	)
	if err != nil {
		return false, fmt.Errorf("remove content favorite: %w", err)
	}
	return tag.RowsAffected() > 0, nil
}

// ListFavorites returns the guardian's bookmarks joined with the currently
// published version so a withdrawn package still appears with Available false.
func (r *PostgresRepository) ListFavorites(
	ctx context.Context,
	parentAccountID string,
) ([]domain.FavoriteView, error) {
	rows, err := r.pool.Query(
		ctx,
		`SELECT f.package_id,
		        COALESCE(v.title, ''),
		        COALESCE(v.category, ''),
		        COALESCE(v.age_tiers, ARRAY[]::TEXT[]),
		        COALESCE(v.package_version, 0),
		        (v.package_id IS NOT NULL) AS available,
		        f.created_at
		 FROM content_favorites f
		 LEFT JOIN content_package_versions v
		   ON v.package_id = f.package_id
		  AND v.status = 'published'
		 WHERE f.parent_account_id = $1
		 ORDER BY f.created_at DESC, f.package_id ASC`,
		parentAccountID,
	)
	if err != nil {
		return nil, fmt.Errorf("list content favorites: %w", err)
	}
	defer rows.Close()
	views := make([]domain.FavoriteView, 0)
	for rows.Next() {
		var (
			view     domain.FavoriteView
			category string
			ageTiers []string
		)
		if err := rows.Scan(
			&view.PackageID,
			&view.Title,
			&category,
			&ageTiers,
			&view.PackageVersion,
			&view.Available,
			&view.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan content favorite: %w", err)
		}
		view.Category = domain.Category(category)
		view.AgeTiers = make([]domain.AgeTier, 0, len(ageTiers))
		for _, tier := range ageTiers {
			view.AgeTiers = append(view.AgeTiers, domain.AgeTier(tier))
		}
		views = append(views, view)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate content favorites: %w", err)
	}
	return views, nil
}

// RecordPlayback appends one bounded history row and prunes older rows for the
// same device so a long-lived device cannot grow the table without limit. The
// insert and prune run in one transaction so a crash cannot leave the table
// unbounded between the two writes.
func (r *PostgresRepository) RecordPlayback(
	ctx context.Context,
	input domain.PlayRecordInput,
	limit int,
	now time.Time,
) (domain.PlayRecord, error) {
	if limit < 1 {
		limit = 50
	}
	recordID := uuid.NewString()
	transaction, err := r.pool.Begin(ctx)
	if err != nil {
		return domain.PlayRecord{}, fmt.Errorf("begin playback record: %w", err)
	}
	defer func() { _ = transaction.Rollback(ctx) }()
	var stored domain.PlayRecord
	if err := transaction.QueryRow(
		ctx,
		`INSERT INTO content_play_history (
		     id, family_id, device_id, package_id, package_version, outcome,
		     played_seconds, played_at
		 )
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		 RETURNING id, family_id, device_id, package_id, package_version, outcome,
		           played_seconds, played_at`,
		recordID,
		input.FamilyID,
		input.DeviceID,
		input.PackageID,
		input.PackageVersion,
		string(input.Outcome),
		input.PlayedSeconds,
		now,
	).Scan(
		&stored.ID,
		&stored.FamilyID,
		&stored.DeviceID,
		&stored.PackageID,
		&stored.PackageVersion,
		&stored.Outcome,
		&stored.PlayedSeconds,
		&stored.PlayedAt,
	); err != nil {
		return domain.PlayRecord{}, fmt.Errorf("insert content play history: %w", err)
	}
	if _, err := transaction.Exec(
		ctx,
		`DELETE FROM content_play_history
		 WHERE family_id = $1
		   AND device_id = $2
		   AND id NOT IN (
		       SELECT id FROM content_play_history
		       WHERE family_id = $1
		         AND device_id = $2
		       ORDER BY played_at DESC, id DESC
		       LIMIT $3
		   )`,
		input.FamilyID,
		input.DeviceID,
		limit,
	); err != nil {
		return domain.PlayRecord{}, fmt.Errorf("prune content play history: %w", err)
	}
	if err := transaction.Commit(ctx); err != nil {
		return domain.PlayRecord{}, fmt.Errorf("commit playback record: %w", err)
	}
	return stored, nil
}

// ListPlayHistory returns bounded history for one family-owned device.
func (r *PostgresRepository) ListPlayHistory(
	ctx context.Context,
	query domain.PlayHistoryQuery,
) ([]domain.PlayRecord, error) {
	limit := query.Limit
	if limit < 1 || limit > 200 {
		limit = 50
	}
	rows, err := r.pool.Query(
		ctx,
		`SELECT id, family_id, device_id, package_id, package_version, outcome,
		        played_seconds, played_at
		 FROM content_play_history
		 WHERE family_id = $1
		   AND device_id = $2
		 ORDER BY played_at DESC, id DESC
		 LIMIT $3`,
		query.FamilyID,
		query.DeviceID,
		limit,
	)
	if err != nil {
		return nil, fmt.Errorf("list content play history: %w", err)
	}
	defer rows.Close()
	records := make([]domain.PlayRecord, 0)
	for rows.Next() {
		var record domain.PlayRecord
		if err := rows.Scan(
			&record.ID,
			&record.FamilyID,
			&record.DeviceID,
			&record.PackageID,
			&record.PackageVersion,
			&record.Outcome,
			&record.PlayedSeconds,
			&record.PlayedAt,
		); err != nil {
			return nil, fmt.Errorf("scan content play history: %w", err)
		}
		records = append(records, record)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate content play history: %w", err)
	}
	return records, nil
}

const themePackColumns = `
	theme_id,
	title,
	summary,
	age_tiers,
	package_ids,
	status,
	catalog_revision,
	published_at,
	created_at,
	updated_at
`

// ListThemePacks returns the filtered theme packs, newest change first.
func (r *PostgresRepository) ListThemePacks(
	ctx context.Context,
	query domain.ThemePackQuery,
) ([]domain.ThemePack, error) {
	conditions := []string{"1 = 1"}
	args := make([]any, 0, 3)
	if query.Status != "" {
		args = append(args, query.Status)
		conditions = append(conditions, fmt.Sprintf("status = $%d", len(args)))
	}
	if query.Keyword != "" {
		pattern := "%" + query.Keyword + "%"
		args = append(args, pattern, pattern)
		conditions = append(conditions, fmt.Sprintf(
			"(theme_id ILIKE $%d OR title ILIKE $%d)",
			len(args)-1,
			len(args),
		))
	}
	rows, err := r.pool.Query(
		ctx,
		`SELECT `+themePackColumns+`
		 FROM content_theme_packs
		 WHERE `+strings.Join(conditions, " AND ")+`
		 ORDER BY updated_at DESC, theme_id ASC`,
		args...,
	)
	if err != nil {
		return nil, fmt.Errorf("list content theme packs: %w", err)
	}
	defer rows.Close()
	packs := make([]domain.ThemePack, 0)
	for rows.Next() {
		pack, scanErr := scanThemePack(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		packs = append(packs, *pack)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate content theme packs: %w", err)
	}
	return packs, nil
}

// GetThemePack loads one theme pack or reports ErrThemePackNotFound.
func (r *PostgresRepository) GetThemePack(
	ctx context.Context,
	themeID string,
) (*domain.ThemePack, error) {
	row := r.pool.QueryRow(
		ctx,
		`SELECT `+themePackColumns+`
		 FROM content_theme_packs
		 WHERE theme_id = $1`,
		themeID,
	)
	pack, err := scanThemePack(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, domain.ErrThemePackNotFound
	}
	return pack, err
}

// UpsertThemePack creates or replaces a draft theme pack. The insert is
// rejected when the id is already published or withdrawn so a live pack cannot
// be silently overwritten; the service surfaces the conflict.
func (r *PostgresRepository) UpsertThemePack(
	ctx context.Context,
	input domain.ThemePackInput,
	now time.Time,
) (*domain.ThemePack, error) {
	row := r.pool.QueryRow(
		ctx,
		`INSERT INTO content_theme_packs (
		     theme_id, title, summary, age_tiers, package_ids, status,
		     created_at, updated_at
		 )
		 VALUES ($1, $2, $3, $4, $5, 'draft', $6, $6)
		 ON CONFLICT (theme_id) DO UPDATE
		     SET title = EXCLUDED.title,
		         summary = EXCLUDED.summary,
		         age_tiers = EXCLUDED.age_tiers,
		         package_ids = EXCLUDED.package_ids,
		         updated_at = EXCLUDED.updated_at
		     WHERE content_theme_packs.status = 'draft'
		 RETURNING `+themePackColumns,
		input.ThemeID,
		input.Title,
		input.Summary,
		ageTierValues(input.AgeTiers),
		input.PackageIDs,
		now,
	)
	pack, err := scanThemePack(row)
	if errors.Is(err, pgx.ErrNoRows) {
		// The id exists but is not a draft: the conflict guard matched nothing.
		return nil, domain.ErrThemeTransitionInvalid
	}
	if err != nil {
		return nil, fmt.Errorf("upsert content theme pack: %w", err)
	}
	return pack, nil
}

// TransitionThemePack publishes or withdraws a pack. Publishing stamps the
// current catalog revision; withdrawing clears the stamp so a client cannot
// treat a withdrawn pack as current.
func (r *PostgresRepository) TransitionThemePack(
	ctx context.Context,
	themeID string,
	action domain.Action,
	now time.Time,
) (*domain.ThemePack, error) {
	var target domain.ThemePackStatus
	switch action {
	case domain.ActionPublish:
		target = domain.ThemePackPublished
	case domain.ActionWithdraw:
		target = domain.ThemePackWithdrawn
	default:
		return nil, domain.ErrThemeTransitionInvalid
	}
	row := r.pool.QueryRow(
		ctx,
		`UPDATE content_theme_packs
		 SET status = $2,
		     catalog_revision = CASE WHEN $2 = 'published'
		         THEN COALESCE(
		             (SELECT revision FROM content_catalog_revision WHERE singleton),
		             0)
		         ELSE 0 END,
		     published_at = CASE WHEN $2 = 'published' THEN $3 ELSE published_at END,
		     updated_at = $3
		 WHERE theme_id = $1
		 RETURNING `+themePackColumns,
		themeID,
		string(target),
		now,
	)
	pack, err := scanThemePack(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, domain.ErrThemePackNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("transition content theme pack: %w", err)
	}
	if pack.Status != target {
		return nil, domain.ErrThemeTransitionInvalid
	}
	return pack, nil
}

// DeleteThemePack removes one pack. A published pack is never deleted so an
// operator must withdraw it first; the caller surfaces the conflict.
func (r *PostgresRepository) DeleteThemePack(
	ctx context.Context,
	themeID string,
) error {
	tag, err := r.pool.Exec(
		ctx,
		`DELETE FROM content_theme_packs
		 WHERE theme_id = $1 AND status <> 'published'`,
		themeID,
	)
	if err != nil {
		return fmt.Errorf("delete content theme pack: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrThemePackNotFound
	}
	return nil
}

func scanThemePack(row rowScanner) (*domain.ThemePack, error) {
	var pack domain.ThemePack
	var ageTiers []string
	if err := row.Scan(
		&pack.ThemeID,
		&pack.Title,
		&pack.Summary,
		&ageTiers,
		&pack.PackageIDs,
		&pack.Status,
		&pack.CatalogRevision,
		&pack.PublishedAt,
		&pack.CreatedAt,
		&pack.UpdatedAt,
	); err != nil {
		return nil, err
	}
	pack.AgeTiers = make([]domain.AgeTier, 0, len(ageTiers))
	for _, tier := range ageTiers {
		pack.AgeTiers = append(pack.AgeTiers, domain.AgeTier(tier))
	}
	if pack.PackageIDs == nil {
		pack.PackageIDs = []string{}
	}
	pack.CreatedAt = pack.CreatedAt.UTC()
	pack.UpdatedAt = pack.UpdatedAt.UTC()
	if pack.PublishedAt != nil {
		publishedAt := pack.PublishedAt.UTC()
		pack.PublishedAt = &publishedAt
	}
	return &pack, nil
}
