// Package domain defines the wearable location, geofence, emergency-contact and
// SOS contracts for the wearable module.
//
// A family owns one location privacy policy, the emergency contacts a child can
// reach, one bounded home geofence and the SOS history. The platform stores only
// the bounded metadata it needs: a rounded position, the precision band, the
// geofence result and the SOS lifecycle. It never stores a raw continuous track
// and clearing the policy deletes the retained positions, so revoking the
// location permission stops sampling, upload and display.
package domain

import (
	"errors"
	"time"
)

var (
	// ErrInvalidPolicy reports a malformed location policy.
	ErrInvalidPolicy = errors.New("invalid wearable location policy")
	// ErrInvalidLocation reports a malformed location report.
	ErrInvalidLocation = errors.New("invalid wearable location report")
	// ErrLocationNotFound reports a missing device location status.
	ErrLocationNotFound = errors.New("wearable location not found")
	// ErrInvalidContact reports a malformed emergency contact.
	ErrInvalidContact = errors.New("invalid wearable emergency contact")
	// ErrContactNotFound reports a missing emergency contact.
	ErrContactNotFound = errors.New("wearable emergency contact not found")
	// ErrInvalidGeofence reports a malformed geofence.
	ErrInvalidGeofence = errors.New("invalid wearable geofence")
	// ErrGeofenceNotFound reports a missing home geofence.
	ErrGeofenceNotFound = errors.New("wearable geofence not found")
	// ErrInvalidSOS reports a malformed SOS request or transition.
	ErrInvalidSOS = errors.New("invalid wearable sos")
	// ErrSOSNotFound reports a missing SOS event.
	ErrSOSNotFound = errors.New("wearable sos not found")
)

// SchemaVersion is the location contract version shared with the firmware and
// both front ends.
const SchemaVersion = 1

// Precision bands the platform recognises. A coarser band is a stricter privacy
// choice, so the default is the coarsest one.
const (
	PrecisionCoarse   = "coarse"
	PrecisionBalanced = "balanced"
	PrecisionPrecise  = "precise"
)

// Geofence states the platform recognises.
const (
	GeofenceInside  = "inside"
	GeofenceOutside = "outside"
	GeofenceUnknown = "unknown"
)

// SOS states the platform recognises. A triggered SOS is latched until it is
// cancelled so a momentary button bounce never clears an emergency.
const (
	SOSStateIdle      = "idle"
	SOSStateArmed     = "armed"
	SOSStateTriggered = "triggered"
	SOSStateCancelled = "cancelled"
)

// Contact roles the platform recognises.
const (
	RoleGuardian = "guardian"
	RoleFamily   = "family"
	RoleService  = "service"
)

// Bounds shared with the firmware so a policy never leaves the documented range.
const (
	MinUploadIntervalSeconds = 30
	MaxUploadIntervalSeconds = 86400
	MinRetentionDays         = 1
	MaxRetentionDays         = 90
	MinGeofenceRadiusMeters  = 50
	MaxGeofenceRadiusMeters  = 10000
)

// ContactNameMaxLength and ContactPhoneMaxLength bound an emergency contact.
const (
	ContactNameMaxLength  = 40
	ContactPhoneMaxLength = 32
)

// SOSHistoryDefaultLimit and SOSHistoryMaxLimit bound a history read.
const (
	SOSHistoryDefaultLimit = 50
	SOSHistoryMaxLimit     = 200
)

// SupportedPrecision reports whether a precision band is accepted.
func SupportedPrecision(precision string) bool {
	switch precision {
	case PrecisionCoarse, PrecisionBalanced, PrecisionPrecise:
		return true
	default:
		return false
	}
}

// SupportedGeofenceState reports whether a geofence state is accepted.
func SupportedGeofenceState(state string) bool {
	switch state {
	case GeofenceInside, GeofenceOutside, GeofenceUnknown:
		return true
	default:
		return false
	}
}

// SupportedSOSState reports whether an SOS state is accepted.
func SupportedSOSState(state string) bool {
	switch state {
	case SOSStateIdle, SOSStateArmed, SOSStateTriggered, SOSStateCancelled:
		return true
	default:
		return false
	}
}

// SupportedRole reports whether an emergency contact role is accepted.
func SupportedRole(role string) bool {
	switch role {
	case RoleGuardian, RoleFamily, RoleService:
		return true
	default:
		return false
	}
}

// LocationPolicy is the family-scoped privacy policy for wearable location.
type LocationPolicy struct {
	FamilyID              string
	Enabled               bool
	PermissionGranted     bool
	Precision             string
	UploadIntervalSeconds int
	RetentionDays         int
	GeofenceRadiusMeters  int
	UpdatedAt             time.Time
}

// LocationReport is one bounded device position report. The latitude and
// longitude are already rounded to the precision band by the reporter.
type LocationReport struct {
	DeviceID       string
	Latitude       float64
	Longitude      float64
	AccuracyMeters float64
	Precision      string
	GeofenceState  string
	SampledAt      *time.Time
}

// DeviceStatus is the stored latest wearable snapshot for one device.
type DeviceStatus struct {
	DeviceID          string
	FamilyID          string
	LocationEnabled   bool
	PermissionGranted bool
	Precision         string
	SOSState          string
	Latitude          float64
	Longitude         float64
	AccuracyMeters    float64
	GeofenceState     string
	LastReportedAt    time.Time
}

// DeviceStatusView is one device row in the guardian overview.
type DeviceStatusView struct {
	DeviceID          string    `json:"device_id"`
	FamilyID          string    `json:"family_id"`
	LocationEnabled   bool      `json:"location_enabled"`
	PermissionGranted bool      `json:"permission_granted"`
	Precision         string    `json:"precision"`
	SOSState          string    `json:"sos_state"`
	Latitude          float64   `json:"latitude"`
	Longitude         float64   `json:"longitude"`
	AccuracyMeters    float64   `json:"accuracy_meters"`
	GeofenceState     string    `json:"geofence_state"`
	LastReportedAt    time.Time `json:"last_reported_at"`
}

// EmergencyContact is one guardian-managed emergency contact.
type EmergencyContact struct {
	ID          string
	FamilyID    string
	ChildID     string
	DisplayName string
	Phone       string
	Role        string
	Priority    int
	Enabled     bool
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// ContactInput is one editable emergency contact payload.
type ContactInput struct {
	ChildID     string
	DisplayName string
	Phone       string
	Role        string
	Priority    int
	Enabled     bool
}

// Geofence is the single bounded home geofence for a family. A family address
// is minimized to one center point plus a radius so the platform never stores a
// street address.
type Geofence struct {
	ID           string
	FamilyID     string
	ChildID      string
	Latitude     float64
	Longitude    float64
	RadiusMeters int
	Enabled      bool
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

// GeofenceInput is one editable geofence payload.
type GeofenceInput struct {
	ChildID      string
	Latitude     float64
	Longitude    float64
	RadiusMeters int
	Enabled      bool
}

// SOSEvent is one stored SOS lifecycle row.
type SOSEvent struct {
	ID          string
	FamilyID    string
	DeviceID    string
	Latitude    float64
	Longitude   float64
	State       string
	TriggeredAt time.Time
	CancelledAt *time.Time
}

// SOSInput is one bounded SOS trigger from a device.
type SOSInput struct {
	DeviceID  string
	Latitude  float64
	Longitude float64
}

// AdminStats is the fleet-wide wearable summary for the admin console.
type AdminStats struct {
	WindowDays           int       `json:"window_days"`
	DeviceCount          int       `json:"device_count"`
	LocationEnabledCount int       `json:"location_enabled_count"`
	PermissionCount      int       `json:"permission_count"`
	SOSTriggeredCount    int       `json:"sos_triggered_count"`
	SOSCancelledCount    int       `json:"sos_cancelled_count"`
	GeofenceOutsideCount int       `json:"geofence_outside_count"`
	PrecisionBuckets     []Count   `json:"precision_buckets"`
	StateBuckets         []Count   `json:"state_buckets"`
	GeneratedAt          time.Time `json:"generated_at"`
}

// Count is one histogram row for a labelled bucket.
type Count struct {
	Label string `json:"label"`
	Count uint64 `json:"count"`
}

// ValidatePolicy normalizes and clamps one location policy into the bounds.
func ValidatePolicy(policy LocationPolicy) (LocationPolicy, error) {
	normalized := policy
	if !SupportedPrecision(normalized.Precision) {
		return LocationPolicy{}, ErrInvalidPolicy
	}
	normalized.UploadIntervalSeconds = clampInt(
		normalized.UploadIntervalSeconds,
		MinUploadIntervalSeconds,
		MaxUploadIntervalSeconds,
	)
	normalized.RetentionDays = clampInt(
		normalized.RetentionDays,
		MinRetentionDays,
		MaxRetentionDays,
	)
	normalized.GeofenceRadiusMeters = clampInt(
		normalized.GeofenceRadiusMeters,
		MinGeofenceRadiusMeters,
		MaxGeofenceRadiusMeters,
	)
	return normalized, nil
}

// ValidateLocationReport normalizes and validates one device location report.
func ValidateLocationReport(report LocationReport) (LocationReport, error) {
	normalized := report
	if !SupportedPrecision(normalized.Precision) {
		return LocationReport{}, ErrInvalidLocation
	}
	if normalized.GeofenceState == "" {
		normalized.GeofenceState = GeofenceUnknown
	}
	if !SupportedGeofenceState(normalized.GeofenceState) {
		return LocationReport{}, ErrInvalidLocation
	}
	if !validLatitude(normalized.Latitude) || !validLongitude(normalized.Longitude) {
		return LocationReport{}, ErrInvalidLocation
	}
	if normalized.AccuracyMeters < 0 {
		return LocationReport{}, ErrInvalidLocation
	}
	return normalized, nil
}

// ValidateContactInput normalizes and validates one emergency contact payload.
func ValidateContactInput(input ContactInput) (ContactInput, error) {
	normalized := input
	nameLength := len([]rune(normalized.DisplayName))
	if nameLength == 0 || nameLength > ContactNameMaxLength {
		return ContactInput{}, ErrInvalidContact
	}
	phoneLength := len([]rune(normalized.Phone))
	if phoneLength == 0 || phoneLength > ContactPhoneMaxLength {
		return ContactInput{}, ErrInvalidContact
	}
	if !SupportedRole(normalized.Role) {
		return ContactInput{}, ErrInvalidContact
	}
	if normalized.Priority < 0 {
		normalized.Priority = 0
	}
	return normalized, nil
}

// ValidateGeofenceInput normalizes and validates one geofence payload.
func ValidateGeofenceInput(input GeofenceInput) (GeofenceInput, error) {
	normalized := input
	if !validLatitude(normalized.Latitude) || !validLongitude(normalized.Longitude) {
		return GeofenceInput{}, ErrInvalidGeofence
	}
	if normalized.RadiusMeters < MinGeofenceRadiusMeters ||
		normalized.RadiusMeters > MaxGeofenceRadiusMeters {
		return GeofenceInput{}, ErrInvalidGeofence
	}
	return normalized, nil
}

// ValidateSOSInput normalizes and validates one SOS trigger.
func ValidateSOSInput(input SOSInput) (SOSInput, error) {
	normalized := input
	if normalized.DeviceID == "" {
		return SOSInput{}, ErrInvalidSOS
	}
	if !validLatitude(normalized.Latitude) || !validLongitude(normalized.Longitude) {
		return SOSInput{}, ErrInvalidSOS
	}
	return normalized, nil
}

// GeofenceStateForDistance returns the geofence state for a measured distance
// and a bounded radius. The comparison is inclusive at the boundary so a point
// exactly on the radius stays inside.
func GeofenceStateForDistance(radiusMeters int, distanceMeters float64) string {
	bounded := clampInt(radiusMeters, MinGeofenceRadiusMeters, MaxGeofenceRadiusMeters)
	if distanceMeters <= float64(bounded) {
		return GeofenceInside
	}
	return GeofenceOutside
}

func validLatitude(value float64) bool {
	return value >= -90 && value <= 90
}

func validLongitude(value float64) bool {
	return value >= -180 && value <= 180
}

func clampInt(value int, minimum int, maximum int) int {
	if value < minimum {
		return minimum
	}
	if value > maximum {
		return maximum
	}
	return value
}
