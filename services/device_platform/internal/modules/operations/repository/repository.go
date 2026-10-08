// Package repository persists operations settings, releases, and dashboards.
package repository

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	authdomain "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/auth/domain"
	"github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/operations/domain"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const settingsKey = "operations"

// Repository defines operations persistence used by the management console.
type Repository interface {
	GetSettings(ctx context.Context) (*domain.Settings, int64, error)
	RuntimePolicy(ctx context.Context) (*domain.RuntimePolicy, error)
	SaveSettings(
		ctx context.Context,
		settings *domain.Settings,
		actorAccountID string,
		expectedVersion int64,
	) (int64, error)
	Overview(
		ctx context.Context,
		onlineThreshold time.Duration,
	) (*domain.Overview, error)
	ListFamilyAccounts(ctx context.Context) ([]domain.FamilyAccount, error)
	ParentOverview(
		ctx context.Context,
		parentAccountID string,
		onlineThreshold time.Duration,
	) (*authdomain.ParentOverview, error)
	ListReleases(ctx context.Context) ([]domain.Release, error)
	CreateRelease(ctx context.Context, release *domain.Release) error
	PublishRelease(ctx context.Context, version string, actorAccountID string) error
	DeleteRelease(ctx context.Context, version string) error
	FindReleaseArtifact(
		ctx context.Context,
		version string,
		kind string,
		platform string,
	) (*domain.ReleaseArtifact, error)
	LatestPublishedUpdate(
		ctx context.Context,
		platform string,
		channel string,
	) (*domain.Release, error)
}

// FindReleaseArtifact returns the newest matching registration for a single
// version. Draft records are included because the console resolves the file
// before publishing the release.
func (r *PostgresRepository) FindReleaseArtifact(
	ctx context.Context,
	version string,
	kind string,
	platform string,
) (*domain.ReleaseArtifact, error) {
	var artifact domain.ReleaseArtifact
	err := r.pool.QueryRow(ctx, `
		SELECT version, kind, platform, download_url, sha256
		FROM platform_releases
		WHERE version = $1
		  AND kind = $2
		  AND platform = $3
		ORDER BY created_at DESC
		LIMIT 1
	`, version, kind, platform).Scan(
		&artifact.Version,
		&artifact.Kind,
		&artifact.Platform,
		&artifact.DownloadURL,
		&artifact.SHA256,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, domain.ErrReleaseNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("find release artifact: %w", err)
	}
	return &artifact, nil
}

// RuntimePolicy loads the narrow policy used by registration and AI account
// provisioning. Missing settings fail closed so a database problem cannot
// silently enable registration or increase default spending.
func (r *PostgresRepository) RuntimePolicy(
	ctx context.Context,
) (*domain.RuntimePolicy, error) {
	settings, _, err := r.GetSettings(ctx)
	if err != nil {
		return nil, err
	}
	return &domain.RuntimePolicy{
		RegistrationEnabled:       settings.Account.RegistrationEnabled,
		PhoneVerificationRequired: settings.Account.PhoneVerificationRequired,
		EmailLoginEnabled:         settings.Account.EmailLoginEnabled,
		DefaultBalanceUSD:         settings.AI.DefaultBalanceUSD,
		DefaultConcurrency:        settings.AI.DefaultConcurrency,
		DefaultModels:             append([]string(nil), settings.AI.DefaultModels...),
		MinorModeDefault:          settings.Safety.MinorModeDefault,
		OutputModerationEnabled:   settings.Safety.OutputModerationEnabled,
		CrisisInterventionEnabled: settings.Safety.CrisisInterventionEnabled,
	}, nil
}

// ListFamilyAccounts returns guardian accounts joined with their dependent AI
// account projection. The left join is deliberate: an account whose provider
// provisioning failed must remain visible to operators.
func (r *PostgresRepository) ListFamilyAccounts(
	ctx context.Context,
) ([]domain.FamilyAccount, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT
			parent_accounts.id,
			COALESCE(parent_accounts.phone, ''),
			COALESCE(parent_accounts.email, ''),
			parent_accounts.display_name,
			parent_accounts.guardian_family_name,
			COALESCE(parent_accounts.child_nickname, ''),
			COALESCE(parent_accounts.child_birthday::text, ''),
			parent_accounts.status,
			parent_accounts.created_at,
			parent_accounts.last_login_at,
			ai_accounts.provider_account_id,
			ai_accounts.status,
			ai_accounts.balance_usd,
			ai_accounts.concurrency_limit,
			ai_accounts.available_models,
			ai_accounts.selected_models,
			ai_accounts.allowed_models,
			ai_accounts.credential_ciphertext IS NOT NULL,
			ai_accounts.updated_at
		FROM parent_accounts
		LEFT JOIN ai_accounts ON ai_accounts.parent_account_id = parent_accounts.id
		WHERE parent_accounts.role = 'parent'
		ORDER BY parent_accounts.created_at DESC
	`)
	if err != nil {
		return nil, fmt.Errorf("list family accounts: %w", err)
	}
	defer rows.Close()
	accounts := make([]domain.FamilyAccount, 0)
	for rows.Next() {
		var account domain.FamilyAccount
		var providerAccountID *string
		var aiStatus *string
		var balanceUSD *float64
		var concurrencyLimit *int
		var availableModels []string
		var selectedModels []string
		var allowedModels []string
		var credentialReady *bool
		var aiUpdatedAt *time.Time
		if err := rows.Scan(
			&account.ParentAccountID,
			&account.Phone,
			&account.Email,
			&account.DisplayName,
			&account.GuardianFamilyName,
			&account.ChildNickname,
			&account.ChildBirthday,
			&account.Status,
			&account.CreatedAt,
			&account.LastLoginAt,
			&providerAccountID,
			&aiStatus,
			&balanceUSD,
			&concurrencyLimit,
			&availableModels,
			&selectedModels,
			&allowedModels,
			&credentialReady,
			&aiUpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan family account: %w", err)
		}
		if providerAccountID != nil {
			account.AIAccount = &domain.FamilyAIAccount{
				ProviderAccountID: *providerAccountID,
				Status:            stringValue(aiStatus),
				BalanceUSD:        floatValue(balanceUSD),
				ConcurrencyLimit:  intValue(concurrencyLimit),
				AvailableModels:   availableModels,
				SelectedModels:    selectedModels,
				AllowedModels:     allowedModels,
				CredentialReady:   boolValue(credentialReady),
				UpdatedAt:         timeValue(aiUpdatedAt),
			}
		}
		accounts = append(accounts, account)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate family accounts: %w", err)
	}
	return accounts, nil
}

// PostgresRepository is the PostgreSQL-backed operations repository.
type PostgresRepository struct {
	pool *pgxpool.Pool
}

// NewPostgresRepository creates an operations repository.
func NewPostgresRepository(pool *pgxpool.Pool) *PostgresRepository {
	return &PostgresRepository{pool: pool}
}

// GetSettings loads the single versioned operations document.
func (r *PostgresRepository) GetSettings(
	ctx context.Context,
) (*domain.Settings, int64, error) {
	var raw []byte
	var version int64
	err := r.pool.QueryRow(ctx, `
		SELECT value, version
		FROM platform_settings
		WHERE settings_key = $1
	`, settingsKey).Scan(&raw, &version)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, 0, domain.ErrSettingsNotFound
	}
	if err != nil {
		return nil, 0, fmt.Errorf("load platform settings: %w", err)
	}
	var settings domain.Settings
	if err := json.Unmarshal(raw, &settings); err != nil {
		return nil, 0, fmt.Errorf("decode platform settings: %w", err)
	}
	return &settings, version, nil
}

// SaveSettings performs an optimistic update and increments the document
// version. A missing row is initialized with the same conservative defaults
// used by the migration.
func (r *PostgresRepository) SaveSettings(
	ctx context.Context,
	settings *domain.Settings,
	actorAccountID string,
	expectedVersion int64,
) (int64, error) {
	encoded, err := json.Marshal(settings)
	if err != nil {
		return 0, fmt.Errorf("encode platform settings: %w", err)
	}
	if expectedVersion == 0 {
		var version int64
		err = r.pool.QueryRow(ctx, `
			INSERT INTO platform_settings (
				settings_key,
				value,
				version,
				updated_by,
				created_at,
				updated_at
			)
			VALUES ($1, $2::jsonb, 1, NULLIF($3, '')::uuid, NOW(), NOW())
			ON CONFLICT (settings_key) DO NOTHING
			RETURNING version
		`, settingsKey, encoded, actorAccountID).Scan(&version)
		if err == nil {
			return version, nil
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return 0, fmt.Errorf("insert platform settings: %w", err)
		}
		return 0, r.settingsVersionConflict(ctx, expectedVersion)
	}

	var version int64
	err = r.pool.QueryRow(ctx, `
		UPDATE platform_settings
		SET value = $2::jsonb,
		    version = platform_settings.version + 1,
		    updated_by = NULLIF($3, '')::uuid,
		    updated_at = NOW()
		WHERE platform_settings.settings_key = $1
		  AND platform_settings.version = $4
		RETURNING version
	`, settingsKey, encoded, actorAccountID, expectedVersion).Scan(&version)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return 0, r.settingsVersionConflict(ctx, expectedVersion)
		}
		return 0, fmt.Errorf("save platform settings: %w", err)
	}
	return version, nil
}

func (r *PostgresRepository) settingsVersionConflict(
	ctx context.Context,
	expectedVersion int64,
) error {
	var currentVersion int64
	err := r.pool.QueryRow(ctx, `
		SELECT version
		FROM platform_settings
		WHERE settings_key = $1
	`, settingsKey).Scan(&currentVersion)
	if errors.Is(err, pgx.ErrNoRows) {
		return &domain.SettingsVersionConflictError{
			ExpectedVersion: expectedVersion,
			CurrentVersion:  0,
		}
	}
	if err != nil {
		return fmt.Errorf("load current platform settings version: %w", err)
	}
	return &domain.SettingsVersionConflictError{
		ExpectedVersion: expectedVersion,
		CurrentVersion:  currentVersion,
	}
}

// Overview returns dashboard counters from the owning tables.
func (r *PostgresRepository) Overview(
	ctx context.Context,
	onlineThreshold time.Duration,
) (*domain.Overview, error) {
	overview := &domain.Overview{RecentReleases: make([]domain.Release, 0)}
	if err := r.pool.QueryRow(ctx, `
		SELECT
			(SELECT COUNT(*) FROM parent_accounts WHERE role = 'parent' AND status = 'active'),
			(SELECT COUNT(*) FROM device_bindings),
			(
				SELECT COUNT(*)
				FROM device_runtime_status
				WHERE connection_state = 'online'
				  AND received_at >= NOW() - $1::interval
			),
			COALESCE((
				SELECT SUM(conversation_count)
				FROM platform_usage_daily
				WHERE usage_day = CURRENT_DATE
			), 0),
			COALESCE((
				SELECT SUM(spent_usd)
				FROM platform_usage_daily
				WHERE usage_day = CURRENT_DATE
			), 0),
			COALESCE((
				SELECT SUM(balance_usd)
				FROM ai_accounts
				WHERE status = 'active'
			), 0),
			(SELECT COUNT(*) FROM platform_releases WHERE status = 'draft')
	`,
		fmt.Sprintf("%d milliseconds", onlineThreshold.Milliseconds()),
	).Scan(
		&overview.ParentAccountCount,
		&overview.ActiveDeviceCount,
		&overview.OnlineDeviceCount,
		&overview.TodayConversationCount,
		&overview.TodaySpentUSD,
		&overview.TotalBalanceUSD,
		&overview.PendingReleaseCount,
	); err != nil {
		return nil, fmt.Errorf("load operations overview: %w", err)
	}
	releases, err := r.ListReleases(ctx)
	if err != nil {
		return nil, err
	}
	const recentReleaseLimit = 5
	if len(releases) > recentReleaseLimit {
		releases = releases[:recentReleaseLimit]
	}
	overview.RecentReleases = releases
	return overview, nil
}

// ParentOverview returns one guardian's dashboard counters.
func (r *PostgresRepository) ParentOverview(
	ctx context.Context,
	parentAccountID string,
	onlineThreshold time.Duration,
) (*authdomain.ParentOverview, error) {
	overview := &authdomain.ParentOverview{}
	err := r.pool.QueryRow(ctx, `
		SELECT
			COALESCE((
				SELECT SUM(conversation_count)
				FROM platform_usage_daily
				WHERE parent_account_id = $1
				  AND usage_day = CURRENT_DATE
			), 0),
			COALESCE((
				SELECT SUM(spent_usd)
				FROM platform_usage_daily
				WHERE parent_account_id = $1
				  AND usage_day = CURRENT_DATE
			), 0),
			COALESCE((
				SELECT balance_usd
				FROM ai_accounts
				WHERE parent_account_id = $1
				  AND status = 'active'
			), 0),
			(SELECT COUNT(*) FROM device_bindings WHERE parent_account_id = $1),
			(
				SELECT COUNT(*)
				FROM device_bindings AS bindings
				JOIN device_runtime_status AS runtime
				  ON runtime.device_id = bindings.device_id
				WHERE bindings.parent_account_id = $1
				  AND runtime.connection_state = 'online'
				  AND runtime.received_at >= NOW() - $2::interval
			)
	`,
		parentAccountID,
		fmt.Sprintf("%d milliseconds", onlineThreshold.Milliseconds()),
	).Scan(
		&overview.TodayConversationCount,
		&overview.TodaySpentUSD,
		&overview.BalanceUSD,
		&overview.DeviceCount,
		&overview.OnlineDeviceCount,
	)
	if err != nil {
		return nil, fmt.Errorf("load parent overview: %w", err)
	}
	overview.RemainingBalanceUSD = overview.BalanceUSD
	return overview, nil
}

// ListReleases returns the newest release records for operators.
func (r *PostgresRepository) ListReleases(
	ctx context.Context,
) ([]domain.Release, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT
			id,
			version,
			channel,
			kind,
			platform,
			download_url,
			sha256,
			release_notes,
			is_mandatory,
			min_supported_version,
			status,
			published_at,
			created_at,
			updated_at
		FROM platform_releases
		ORDER BY created_at DESC
		LIMIT 100
	`)
	if err != nil {
		return nil, fmt.Errorf("list platform releases: %w", err)
	}
	defer rows.Close()
	releases := make([]domain.Release, 0)
	for rows.Next() {
		release, err := scanRelease(rows)
		if err != nil {
			return nil, err
		}
		releases = append(releases, *release)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate platform releases: %w", err)
	}
	return releases, nil
}

// CreateRelease stores a draft release. Publishing is a separate action so a
// mistaken upload cannot become visible to clients.
func (r *PostgresRepository) CreateRelease(
	ctx context.Context,
	release *domain.Release,
) error {
	_, err := r.pool.Exec(ctx, `
		INSERT INTO platform_releases (
			id,
			version,
			channel,
			kind,
			platform,
			download_url,
			sha256,
			release_notes,
			is_mandatory,
			min_supported_version,
			status,
			created_at,
			updated_at
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)
	`,
		release.ID,
		release.Version,
		release.Channel,
		release.Kind,
		release.Platform,
		release.DownloadURL,
		release.SHA256,
		release.ReleaseNotes,
		release.IsMandatory,
		release.MinSupportedVersion,
		release.Status,
		release.CreatedAt,
		release.UpdatedAt,
	)
	if err != nil {
		return mapReleaseWriteError(err)
	}
	return nil
}

// PublishRelease makes a draft visible to clients.
func (r *PostgresRepository) PublishRelease(
	ctx context.Context,
	version string,
	actorAccountID string,
) error {
	tag, err := r.pool.Exec(ctx, `
		UPDATE platform_releases
		SET status = 'published',
		    published_at = COALESCE(published_at, NOW()),
		    published_by = NULLIF($2, '')::uuid,
		    updated_at = NOW()
		WHERE version = $1
		  AND status = 'draft'
	`, version, actorAccountID)
	if err != nil {
		return fmt.Errorf("publish platform release: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrReleaseNotPublishable
	}
	return nil
}

// DeleteRelease removes a draft or retired release only.
func (r *PostgresRepository) DeleteRelease(
	ctx context.Context,
	version string,
) error {
	tag, err := r.pool.Exec(ctx, `
		DELETE FROM platform_releases
		WHERE version = $1
		  AND status IN ('draft', 'retired')
	`, version)
	if err != nil {
		return fmt.Errorf("delete platform release: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrReleaseNotFound
	}
	return nil
}

// LatestPublishedUpdate returns the highest published client or resource
// release for a platform. Client packages take priority over resource packages
// so a complete installation can supersede a resource-only change.
func (r *PostgresRepository) LatestPublishedUpdate(
	ctx context.Context,
	platform string,
	channel string,
) (*domain.Release, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT
			id,
			version,
			channel,
			kind,
			platform,
			download_url,
			sha256,
			release_notes,
			is_mandatory,
			min_supported_version,
			status,
			published_at,
			created_at,
			updated_at
		FROM platform_releases
		WHERE status = 'published'
		  AND channel = $2
		  AND platform IN ($1, 'all')
		  AND kind IN ('client', 'resource')
	`, platform, channel)
	if err != nil {
		return nil, fmt.Errorf("list published updates: %w", err)
	}
	defer rows.Close()

	var bestClient *domain.Release
	var bestResource *domain.Release
	for rows.Next() {
		release, err := scanRelease(rows)
		if err != nil {
			return nil, fmt.Errorf("scan published update: %w", err)
		}
		if release.Kind == domain.ReleaseKindClient {
			if bestClient == nil ||
				domain.CompareVersions(release.Version, bestClient.Version) > 0 {
				bestClient = release
			}
			continue
		}
		if bestResource == nil ||
			domain.CompareVersions(release.Version, bestResource.Version) > 0 {
			bestResource = release
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate published updates: %w", err)
	}
	if bestClient != nil {
		return bestClient, nil
	}
	if bestResource != nil {
		return bestResource, nil
	}
	return nil, domain.ErrReleaseNotFound
}

type rowScanner interface {
	Scan(dest ...any) error
}

func scanRelease(row rowScanner) (*domain.Release, error) {
	var release domain.Release
	err := row.Scan(
		&release.ID,
		&release.Version,
		&release.Channel,
		&release.Kind,
		&release.Platform,
		&release.DownloadURL,
		&release.SHA256,
		&release.ReleaseNotes,
		&release.IsMandatory,
		&release.MinSupportedVersion,
		&release.Status,
		&release.PublishedAt,
		&release.CreatedAt,
		&release.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	return &release, nil
}

func mapReleaseWriteError(err error) error {
	if err == nil {
		return nil
	}
	if strings.Contains(err.Error(), "platform_releases_version_unique") {
		return domain.ErrReleaseAlreadyExists
	}
	return fmt.Errorf("insert platform release: %w", err)
}

func stringValue(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func floatValue(value *float64) float64 {
	if value == nil {
		return 0
	}
	return *value
}

func intValue(value *int) int {
	if value == nil {
		return 0
	}
	return *value
}

func boolValue(value *bool) bool {
	return value != nil && *value
}

func timeValue(value *time.Time) time.Time {
	if value == nil {
		return time.Time{}
	}
	return *value
}

var _ Repository = (*PostgresRepository)(nil)
