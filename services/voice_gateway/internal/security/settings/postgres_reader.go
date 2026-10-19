// Package settings exposes the operator-controlled child-safety switches
// stored by the platform console.
//
// The platform persists these switches in the single versioned
// platform_settings document. Reusing that source of truth keeps the console
// toggles from drifting away from the voice gateway's actual runtime policy.
package settings

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// DefaultCacheTTL bounds how long a console change may take to reach the
// gateway. It is short enough for operators to observe edits quickly and long
// enough that per-turn checks never query PostgreSQL.
const DefaultCacheTTL = 10 * time.Second

// SafetyPolicy is the effective child-safety switch projection.
//
// CrisisInterventionEnabled and MinorModeDefault are retained for
// observability, but callers must treat both as mandatory: the platform
// contract never lets an operator disable either protection.
type SafetyPolicy struct {
	OutputModerationEnabled   bool `json:"output_moderation_enabled"`
	CrisisInterventionEnabled bool `json:"crisis_intervention_enabled"`
	MinorModeDefault          bool `json:"minor_mode_default"`
}

// Reader resolves the current safety policy.
type Reader interface {
	SafetyPolicy(ctx context.Context) (SafetyPolicy, error)
}

// PostgresReader reads platform_settings with a short-lived cache so a
// database hiccup cannot stall an active voice turn.
type PostgresReader struct {
	pool     *pgxpool.Pool
	ttl      time.Duration
	now      func() time.Time
	mutex    sync.Mutex
	cached   SafetyPolicy
	cachedAt time.Time
	valid    bool
}

// NewPostgresReader creates a cached safety-policy reader.
func NewPostgresReader(pool *pgxpool.Pool) *PostgresReader {
	return &PostgresReader{
		pool: pool,
		ttl:  DefaultCacheTTL,
		now:  time.Now,
	}
}

// SafetyPolicy returns the cached policy when fresh, otherwise reloads it.
//
// A read failure returns the last known value when one exists so a transient
// database error does not silently re-enable moderation that an operator
// deliberately disabled, or disable moderation that was enabled.
func (r *PostgresReader) SafetyPolicy(ctx context.Context) (SafetyPolicy, error) {
	if r == nil {
		return SafetyPolicy{}, fmt.Errorf("safety settings reader is unavailable")
	}
	r.mutex.Lock()
	defer r.mutex.Unlock()
	if r.valid && r.now().Sub(r.cachedAt) < r.ttl {
		return r.cached, nil
	}
	policy, err := r.load(ctx)
	if err != nil {
		if r.valid {
			return r.cached, nil
		}
		return SafetyPolicy{}, err
	}
	r.cached = policy
	r.cachedAt = r.now()
	r.valid = true
	return policy, nil
}

func (r *PostgresReader) load(ctx context.Context) (SafetyPolicy, error) {
	if r.pool == nil {
		return SafetyPolicy{}, fmt.Errorf("safety settings database is unavailable")
	}
	var raw []byte
	err := r.pool.QueryRow(ctx, `
		SELECT value
		FROM platform_settings
		WHERE settings_key = 'operations'
	`).Scan(&raw)
	if err != nil {
		if err == pgx.ErrNoRows {
			return SafetyPolicy{}, fmt.Errorf("platform operations settings are missing")
		}
		return SafetyPolicy{}, fmt.Errorf("read platform safety settings: %w", err)
	}
	return decodeSafetyPolicy(raw)
}

func decodeSafetyPolicy(raw []byte) (SafetyPolicy, error) {
	var document struct {
		Safety SafetyPolicy `json:"safety"`
	}
	if err := json.Unmarshal(raw, &document); err != nil {
		return SafetyPolicy{}, fmt.Errorf("decode platform safety settings: %w", err)
	}
	// Crisis handling is a non-negotiable child-safety guarantee. The stored
	// value is reported for visibility, but the effective policy is always on.
	document.Safety.CrisisInterventionEnabled = true
	// Minor mode is the same kind of platform invariant: every account starts
	// on the protected path, so a stale or tampered settings document must not
	// be able to turn the protection off in the gateway.
	document.Safety.MinorModeDefault = true
	return document.Safety, nil
}
