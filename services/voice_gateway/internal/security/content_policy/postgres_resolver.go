package content_policy

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// PostgresResolver resolves the effective family policy for a device.
//
// The voice gateway already holds a read connection to the platform database
// for usage recording. Reusing that connection keeps policy enforcement on the
// same source of truth as the device runtime endpoint instead of introducing a
// second network hop or a parallel policy service.
type PostgresResolver struct {
	pool *pgxpool.Pool
}

// NewPostgresResolver creates a policy resolver backed by PostgreSQL.
func NewPostgresResolver(pool *pgxpool.Pool) *PostgresResolver {
	return &PostgresResolver{pool: pool}
}

// Resolve loads the most restrictive policy shared by every child in the
// device's family.
//
// A device is bound to one guardian account, not to an individual child, so
// the aggregate semantics match the platform's device runtime contract. The
// youngest child age tier is used so a shared device never receives content
// rated above the most restrictive child.
func (r *PostgresResolver) Resolve(
	ctx context.Context,
	deviceID string,
) (Profile, error) {
	if r == nil || r.pool == nil {
		return Profile{}, ErrPolicyUnavailable
	}
	deviceID = strings.TrimSpace(deviceID)
	if deviceID == "" {
		return Profile{}, ErrPolicyNotFound
	}

	transaction, err := r.pool.BeginTx(ctx, pgx.TxOptions{
		IsoLevel:   pgx.RepeatableRead,
		AccessMode: pgx.ReadOnly,
	})
	if err != nil {
		return Profile{}, fmt.Errorf("%w: begin policy snapshot", ErrPolicyUnavailable)
	}
	defer func() {
		_ = transaction.Rollback(ctx)
	}()

	var familyID string
	if err := transaction.QueryRow(ctx, `
		SELECT parent_account_id::text
		FROM device_bindings
		WHERE device_id = $1
	`, deviceID).Scan(&familyID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Profile{}, ErrPolicyNotFound
		}
		return Profile{}, fmt.Errorf("%w: resolve device family", ErrPolicyUnavailable)
	}

	rows, err := transaction.Query(ctx, `
		SELECT
			p.policy_version,
			p.daily_limit_minutes,
			p.allowed_categories,
			p.disabled_periods,
			p.max_volume_percent,
			p.updated_at,
			c.age_tier
		FROM parent_policies AS p
		JOIN child_profiles AS c ON c.id = p.child_id
		WHERE p.family_id = $1
		ORDER BY p.created_at ASC
	`, familyID)
	if err != nil {
		return Profile{}, fmt.Errorf("%w: list family policies", ErrPolicyUnavailable)
	}
	defer rows.Close()

	policies := make([]resolvedPolicy, 0)
	var revision int64
	for rows.Next() {
		var policy resolvedPolicy
		if err := rows.Scan(
			&policy.policyVersion,
			&policy.dailyLimitMinutes,
			&policy.allowedCategories,
			&policy.disabledPeriodsJSON,
			&policy.maxVolumePercent,
			&policy.updatedAt,
			&policy.ageTier,
		); err != nil {
			return Profile{}, fmt.Errorf("%w: read family policy", ErrPolicyUnavailable)
		}
		policies = append(policies, policy)
	}
	if err := rows.Err(); err != nil {
		return Profile{}, fmt.Errorf("%w: iterate family policies", ErrPolicyUnavailable)
	}
	if len(policies) == 0 {
		return Profile{}, ErrPolicyNotFound
	}

	if err := transaction.QueryRow(ctx, `
		SELECT revision
		FROM parent_policy_revisions
		WHERE family_id = $1
	`, familyID).Scan(&revision); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Profile{}, ErrPolicyNotFound
		}
		return Profile{}, fmt.Errorf("%w: read policy revision", ErrPolicyUnavailable)
	}
	if err := transaction.Commit(ctx); err != nil {
		return Profile{}, fmt.Errorf("%w: commit policy snapshot", ErrPolicyUnavailable)
	}

	profile := aggregateProfiles(policies)
	profile.PolicyVersion = revision
	return profile, nil
}

type resolvedPolicy struct {
	policyVersion       int64
	dailyLimitMinutes   int
	allowedCategories   []string
	disabledPeriodsJSON []byte
	maxVolumePercent    int
	updatedAt           time.Time
	ageTier             string
}

func aggregateProfiles(policies []resolvedPolicy) Profile {
	allowed := make(map[string]struct{}, len(policies[0].allowedCategories))
	for _, category := range policies[0].allowedCategories {
		allowed[category] = struct{}{}
	}
	periods := make(map[string]DisabledPeriod)
	ageTier := policies[0].ageTier
	updatedAt := policies[0].updatedAt
	for _, policy := range policies {
		for category := range allowed {
			if !containsCategory(policy.allowedCategories, category) {
				delete(allowed, category)
			}
		}
		for _, period := range decodeDisabledPeriods(policy.disabledPeriodsJSON) {
			periods[period.StartTime+"-"+period.EndTime] = period
		}
		if ageTierRank(policy.ageTier) < ageTierRank(ageTier) {
			ageTier = policy.ageTier
		}
		if policy.updatedAt.After(updatedAt) {
			updatedAt = policy.updatedAt
		}
	}

	categories := make([]string, 0, len(allowed))
	for _, category := range AllowedCategories {
		if _, ok := allowed[category]; ok {
			categories = append(categories, category)
		}
	}
	disabledPeriods := make([]DisabledPeriod, 0, len(periods))
	for _, period := range periods {
		disabledPeriods = append(disabledPeriods, period)
	}
	sort.Slice(disabledPeriods, func(left int, right int) bool {
		if disabledPeriods[left].StartTime != disabledPeriods[right].StartTime {
			return disabledPeriods[left].StartTime < disabledPeriods[right].StartTime
		}
		return disabledPeriods[left].EndTime < disabledPeriods[right].EndTime
	})

	profile := Profile{
		AgeTier:           ageTier,
		AllowedCategories: categories,
		DisabledPeriods:   disabledPeriods,
		SourceChildCount:  len(policies),
		UpdatedAt:         updatedAt,
		MaxVolumePercent:  policies[0].maxVolumePercent,
	}
	for _, policy := range policies {
		if policy.maxVolumePercent < profile.MaxVolumePercent {
			profile.MaxVolumePercent = policy.maxVolumePercent
		}
	}
	return profile
}

// decodeDisabledPeriods parses the JSONB array persisted by the platform.
//
// A malformed value is treated as an empty list here because the platform
// validates writes; the category allowlist still fails closed if it is empty.
func decodeDisabledPeriods(encoded []byte) []DisabledPeriod {
	if len(encoded) == 0 {
		return nil
	}
	var values []struct {
		StartTime string `json:"start_time"`
		EndTime   string `json:"end_time"`
	}
	if err := json.Unmarshal(encoded, &values); err != nil {
		return nil
	}
	periods := make([]DisabledPeriod, 0, len(values))
	for _, value := range values {
		periods = append(periods, DisabledPeriod{
			StartTime: value.StartTime,
			EndTime:   value.EndTime,
		})
	}
	return periods
}

func containsCategory(values []string, expected string) bool {
	for _, value := range values {
		if value == expected {
			return true
		}
	}
	return false
}

func ageTierRank(ageTier string) int {
	switch ageTier {
	case "age_3_4":
		return 0
	case "age_5_6":
		return 1
	case "age_7_8":
		return 2
	default:
		// An unknown tier is treated as the youngest and most restrictive
		// bucket rather than letting a malformed value widen content access.
		return 0
	}
}
