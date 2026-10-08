// Package domain defines the management-facing feature registry contract.
package domain

import (
	"errors"
	"fmt"
	"time"
)

var (
	// ErrInvalidFeatureID reports an id that is not lower_snake_case.
	ErrInvalidFeatureID = errors.New("invalid feature id")
	// ErrFeatureNotFound reports an id that is not present in the registry.
	ErrFeatureNotFound = errors.New("feature not found")
	// ErrFeatureReadOnly reports a write to a feature without editable config.
	ErrFeatureReadOnly = errors.New("feature is read only")
	// ErrInvalidConfig reports malformed or invalid feature configuration.
	ErrInvalidConfig = errors.New("invalid feature configuration")
	// ErrUnknownConfigField reports a field absent from the feature schema.
	ErrUnknownConfigField = errors.New("unknown feature configuration field")
	// ErrImmutableConfigField reports an attempt to change a system field.
	ErrImmutableConfigField = errors.New("immutable feature configuration field")
	// ErrConfigVersionConflict reports a stale optimistic-concurrency version.
	ErrConfigVersionConflict = errors.New("feature configuration version conflict")
	// ErrFeatureConfigNotFound reports a missing persisted override.
	ErrFeatureConfigNotFound = errors.New("feature configuration not found")
	// ErrHealthCheckFailed reports a health probe that did not return a status.
	ErrHealthCheckFailed = errors.New("feature health check failed")
	// ErrHealthCheckUnavailable reports a feature without a wired probe.
	ErrHealthCheckUnavailable = errors.New("feature health check is not available")
)

// HealthStatus is the stable health projection for one registered feature.
type HealthStatus string

const (
	HealthHealthy     HealthStatus = "healthy"
	HealthDegraded    HealthStatus = "degraded"
	HealthUnavailable HealthStatus = "unavailable"
	HealthUnknown     HealthStatus = "unknown"
)

// Valid reports whether the health value is part of the public contract.
func (status HealthStatus) Valid() bool {
	switch status {
	case HealthHealthy, HealthDegraded, HealthUnavailable, HealthUnknown:
		return true
	default:
		return false
	}
}

// FeatureStatus is the operational lifecycle shown in the feature center.
type FeatureStatus string

const (
	FeatureStatusActive   FeatureStatus = "active"
	FeatureStatusPlanned  FeatureStatus = "planned"
	FeatureStatusDisabled FeatureStatus = "disabled"
)

// ConfigFieldType identifies the supported recursive schema value types.
type ConfigFieldType string

const (
	ConfigFieldBoolean     ConfigFieldType = "boolean"
	ConfigFieldInteger     ConfigFieldType = "integer"
	ConfigFieldNumber      ConfigFieldType = "number"
	ConfigFieldString      ConfigFieldType = "string"
	ConfigFieldSelect      ConfigFieldType = "select"
	ConfigFieldMultiselect ConfigFieldType = "multiselect"
	ConfigFieldSecret      ConfigFieldType = "secret"
	ConfigFieldDuration    ConfigFieldType = "duration"
	ConfigFieldURL         ConfigFieldType = "url"
	ConfigFieldObject      ConfigFieldType = "object"
)

// ConfigValueSource describes where the effective field value came from.
type ConfigValueSource string

const (
	ConfigSourceRegistryDefault ConfigValueSource = "registry_default"
	ConfigSourceSystem          ConfigValueSource = "system"
	ConfigSourceDatabase        ConfigValueSource = "database"
)

// HealthSource describes how a feature health projection is produced so the
// console can distinguish a live probe from a static or unavailable one.
type HealthSource string

const (
	// HealthSourceRuntime means the status came from a live service probe.
	HealthSourceRuntime HealthSource = "service_runtime"
	// HealthSourceDatabase means the status came from a persisted state read.
	HealthSourceDatabase HealthSource = "database_state"
	// HealthSourceNotConfigured means the capability has no probe wired yet.
	HealthSourceNotConfigured HealthSource = "not_configured"
)

// ConfigOption is one selectable value in a select or multiselect field.
type ConfigOption struct {
	Value string `json:"value"`
	Label string `json:"label"`
}

// ConfigField is one node in a recursively validated configuration schema.
//
// Object fields use Fields as their child schema. Secret fields never return
// their plaintext value; Value contains only configuration metadata and a mask.
type ConfigField struct {
	Key      string            `json:"key"`
	Label    string            `json:"label"`
	Type     ConfigFieldType   `json:"type"`
	Required bool              `json:"required"`
	Default  any               `json:"default,omitempty"`
	Value    any               `json:"value,omitempty"`
	Options  []ConfigOption    `json:"options,omitempty"`
	Min      *float64          `json:"min,omitempty"`
	Max      *float64          `json:"max,omitempty"`
	Unit     string            `json:"unit,omitempty"`
	Help     string            `json:"help,omitempty"`
	Secret   bool              `json:"secret"`
	Source   ConfigValueSource `json:"source"`
	Editable bool              `json:"editable"`
	// AllowCustom permits a multiselect field to hold values that are not part
	// of Options. It is used where the selectable set is dynamic, such as the
	// AI model catalogue published by the gateway.
	AllowCustom bool          `json:"allow_custom"`
	Fields      []ConfigField `json:"fields,omitempty"`
}

// Metric is a read-only operational measure owned by one feature.
type Metric struct {
	Key         string `json:"key"`
	Label       string `json:"label"`
	Description string `json:"description,omitempty"`
	Unit        string `json:"unit,omitempty"`
	Value       any    `json:"value,omitempty"`
}

// Feature is the management projection of one registered capability.
type Feature struct {
	ID              string         `json:"id"`
	Name            string         `json:"name"`
	Description     string         `json:"description"`
	Category        string         `json:"category"`
	Owner           string         `json:"owner"`
	Status          FeatureStatus  `json:"status"`
	Health          HealthStatus   `json:"health"`
	HealthDetail    *HealthDetail  `json:"health_detail,omitempty"`
	HealthSource    string         `json:"health_source"`
	ConfigVersion   int64          `json:"config_version"`
	ConfigSchema    []ConfigField  `json:"config_schema"`
	Values          map[string]any `json:"values"`
	ReadOnlyMetrics []Metric       `json:"read_only_metrics"`
	ReadOnlyReason  string         `json:"read_only_reason,omitempty"`
	UpdatedAt       *time.Time     `json:"updated_at,omitempty"`
}

// HealthDetail is the management UI projection for a health probe.
type HealthDetail struct {
	Status    HealthStatus `json:"status"`
	CheckedAt time.Time    `json:"checked_at"`
	Message   string       `json:"message,omitempty"`
	LatencyMS *int         `json:"latency_ms,omitempty"`
}

// FeatureConfig is the persisted overlay for one feature registry entry.
type FeatureConfig struct {
	FeatureID string
	Values    map[string]any
	Version   int64
	UpdatedBy string
	UpdatedAt time.Time
}

// VersionConflictError reports the versions involved in a rejected write.
//
// It is deliberately value-free beyond version numbers so callers can map it
// to a stable API error without exposing stored feature configuration.
type VersionConflictError struct {
	ExpectedVersion int64
	CurrentVersion  int64
}

// Error implements error.
func (e *VersionConflictError) Error() string {
	if e == nil {
		return ErrConfigVersionConflict.Error()
	}
	return fmt.Sprintf(
		"feature configuration version conflict: expected %d, current %d",
		e.ExpectedVersion,
		e.CurrentVersion,
	)
}

// Is allows errors.Is(err, ErrConfigVersionConflict).
func (e *VersionConflictError) Is(target error) bool {
	return target == ErrConfigVersionConflict
}

// ConfigValidationError identifies a safe field path and a stable reason.
//
// The validation message never includes the submitted value, which prevents
// accidental secret or personal-data leakage into errors and logs.
type ConfigValidationError struct {
	Field string
	Err   error
}

// Error implements error.
func (e *ConfigValidationError) Error() string {
	if e == nil || e.Err == nil {
		return ErrInvalidConfig.Error()
	}
	if e.Field == "" {
		return e.Err.Error()
	}
	return fmt.Sprintf("%s: %s", e.Field, e.Err)
}

// Unwrap allows errors.Is to inspect the stable validation cause.
func (e *ConfigValidationError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}
