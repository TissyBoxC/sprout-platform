// Package featurehealth defines shared, non-sensitive readiness projections for
// hardware capability features registered in the Feature Center.
package featurehealth

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Status is the capability health exposed to the Feature Center.
type Status string

const (
	// StatusHealthy means the capability service is queryable and every
	// registered device that advertises the capability has usable state.
	StatusHealthy Status = "healthy"
	// StatusDegraded means at least one expected device is missing state or
	// reports a fault, so the capability cannot be trusted for that device.
	StatusDegraded Status = "degraded"
	// StatusUnknown means no device advertises the capability yet. An empty
	// deployment is not evidence that hardware works.
	StatusUnknown Status = "unknown"
	// StatusUnavailable means the authoritative state could not be queried.
	StatusUnavailable Status = "unavailable"
)

// Valid reports whether a status is part of the Feature Center contract.
func (status Status) Valid() bool {
	switch status {
	case StatusHealthy, StatusDegraded, StatusUnknown, StatusUnavailable:
		return true
	default:
		return false
	}
}

// DeviceCapabilityFacts contains bounded aggregate facts only. It must never
// contain a device identifier, a family identifier, or a rule payload.
type DeviceCapabilityFacts struct {
	// ExpectedDevices is the number of active registrations whose capability
	// snapshot advertises the feature.
	ExpectedDevices uint64
	// ReportedDevices is the subset of expected devices with a latest state row.
	ReportedDevices uint64
	// FaultedDevices is the subset of expected devices that explicitly reports a
	// hardware or pipeline fault.
	FaultedDevices uint64
	// StaleDevices is the subset whose latest state is older than the feature's
	// freshness window.
	StaleDevices uint64
}

// DeviceCapabilityReadiness is the safe aggregate result returned by a module.
type DeviceCapabilityReadiness struct {
	Status Status
	Facts  DeviceCapabilityFacts
}

// DeviceCapabilityStatus derives health from aggregate facts.
//
// Missing and stale state for a device that advertises the capability is a
// degraded state, not healthy and not unavailable. No expected devices is
// unknown because the platform cannot prove hardware health from an empty set.
// Faulted devices are degraded even when every expected device has reported.
func DeviceCapabilityStatus(
	facts DeviceCapabilityFacts,
	required bool,
) (Status, error) {
	if facts.ReportedDevices > facts.ExpectedDevices ||
		facts.FaultedDevices > facts.ExpectedDevices ||
		facts.StaleDevices > facts.ExpectedDevices {
		return StatusUnavailable, errors.New("inconsistent device capability facts")
	}
	if facts.ExpectedDevices == 0 {
		if required {
			return StatusDegraded, nil
		}
		return StatusUnknown, nil
	}
	if facts.ReportedDevices < facts.ExpectedDevices ||
		facts.FaultedDevices > 0 ||
		facts.StaleDevices > 0 {
		return StatusDegraded, nil
	}
	return StatusHealthy, nil
}

// ValidateFreshness rejects an unbounded or non-positive freshness window at
// service construction time.
func ValidateFreshness(window time.Duration) error {
	if window <= 0 {
		return errors.New("capability freshness window must be positive")
	}
	if window > 90*24*time.Hour {
		return fmt.Errorf("capability freshness window %s exceeds 90 days", window)
	}
	return nil
}

var sqlIdentifierPattern = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)

// DeviceStateQuery describes one capability's authoritative state join. All
// fields are module-authored constants; they are never accepted from a request.
type DeviceStateQuery struct {
	Pool            *pgxpool.Pool
	Capability      string
	StateTable      string
	TimestampColumn string
	FaultCondition  string
	Required        bool
}

// ReadDeviceStateReadiness runs one bounded aggregate query over active device
// registrations and their latest capability state.
func ReadDeviceStateReadiness(
	ctx context.Context,
	query DeviceStateQuery,
	freshness time.Duration,
) (DeviceCapabilityReadiness, error) {
	if query.Pool == nil {
		return DeviceCapabilityReadiness{Status: StatusUnavailable},
			errors.New("capability state pool is required")
	}
	if err := ValidateFreshness(freshness); err != nil {
		return DeviceCapabilityReadiness{},
			fmt.Errorf("validate capability freshness: %w", err)
	}
	for name, value := range map[string]string{
		"state table":      query.StateTable,
		"timestamp column": query.TimestampColumn,
	} {
		if !sqlIdentifierPattern.MatchString(value) {
			return DeviceCapabilityReadiness{},
				fmt.Errorf("invalid %s %q", name, value)
		}
	}
	faultCondition := "FALSE"
	if query.FaultCondition != "" {
		lower := strings.ToLower(query.FaultCondition)
		if len(query.FaultCondition) > 512 ||
			strings.Contains(lower, ";") ||
			strings.Contains(lower, "--") ||
			strings.Contains(lower, "/*") ||
			strings.Contains(lower, "*/") ||
			regexp.MustCompile(`[^a-zA-Z0-9_\s().=<>!'+-]`).
				MatchString(query.FaultCondition) {
			return DeviceCapabilityReadiness{},
				errors.New("invalid capability fault condition")
		}
		faultCondition = query.FaultCondition
	}
	var capability any
	if query.Capability != "" {
		capability = query.Capability
	}
	statement := fmt.Sprintf(`
		SELECT
			COUNT(credential.device_id),
			COUNT(status.device_id),
			COUNT(*) FILTER (
				WHERE status.device_id IS NOT NULL
				  AND (%s)
			),
			COUNT(*) FILTER (
				WHERE status.device_id IS NOT NULL
				  AND status.%s < NOW() - ($1::int * INTERVAL '1 millisecond')
			)
		FROM device_credentials AS credential
		LEFT JOIN %s AS status
		  ON status.device_id = credential.device_id
		WHERE credential.status = 'active'
		  AND ($2::text IS NULL OR $2::text = ANY(credential.capability_set))
	`, faultCondition, query.TimestampColumn, query.StateTable)
	var facts DeviceCapabilityFacts
	if err := query.Pool.QueryRow(
		ctx,
		statement,
		freshness.Milliseconds(),
		capability,
	).Scan(
		&facts.ExpectedDevices,
		&facts.ReportedDevices,
		&facts.FaultedDevices,
		&facts.StaleDevices,
	); err != nil {
		return DeviceCapabilityReadiness{},
			fmt.Errorf("read %s readiness: %w", query.StateTable, err)
	}
	status, err := DeviceCapabilityStatus(facts, query.Required)
	if err != nil {
		return DeviceCapabilityReadiness{},
			fmt.Errorf("derive %s readiness: %w", query.StateTable, err)
	}
	return DeviceCapabilityReadiness{Status: status, Facts: facts}, nil
}
