// Package service contains the wearable location use cases.
//
// Every operation is family-scoped: the caller passes the authenticated family
// id and the service never trusts a client-provided owner. The service stores
// bounded metadata only. Revoking the location permission stops sampling,
// upload and display and clears the retained positions, and an SOS is latched
// until it is cancelled so an emergency never clears silently.
package service

import (
	"context"
	"errors"
	"math"
	"strings"

	"github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/wearable/domain"
	"github.com/TissyBoxC/sprout-platform/services/device_platform/internal/platform/clock"
	"github.com/google/uuid"
)

// Repository is the persistence surface the service depends on.
type Repository interface {
	Policy(ctx context.Context, familyID string) (domain.LocationPolicy, bool, error)
	SavePolicy(ctx context.Context, policy domain.LocationPolicy) error
	ClearFamilyLocations(ctx context.Context, familyID string) error

	CreateContact(ctx context.Context, contact domain.EmergencyContact) error
	UpdateContact(ctx context.Context, contact domain.EmergencyContact) error
	DeleteContact(ctx context.Context, familyID string, contactID string) error
	Contacts(ctx context.Context, familyID string) ([]domain.EmergencyContact, error)
	Contact(ctx context.Context, familyID string, contactID string) (domain.EmergencyContact, bool, error)

	SaveGeofence(ctx context.Context, geofence domain.Geofence) error
	Geofence(ctx context.Context, familyID string) (domain.Geofence, bool, error)

	RecordStatus(ctx context.Context, status domain.DeviceStatus) error
	Statuses(ctx context.Context, familyID string) ([]domain.DeviceStatus, error)

	CreateSOS(ctx context.Context, event domain.SOSEvent) error
	CancelSOS(ctx context.Context, familyID string, eventID string) (domain.SOSEvent, bool, error)
	SOSEvents(ctx context.Context, familyID string, limit int) ([]domain.SOSEvent, error)
	ActiveSOS(ctx context.Context, familyID string) ([]domain.SOSEvent, error)
	ActiveSOSByDevice(ctx context.Context, familyID string, deviceID string) (domain.SOSEvent, bool, error)

	AdminStats(ctx context.Context, windowDays int) (domain.AdminStats, error)
}

// Service owns wearable location policy, positions, geofence, contacts and SOS.
type Service struct {
	repository Repository
	clock      clock.Clock
}

// Options contains wearable service dependencies.
type Options struct {
	Repository Repository
	Clock      clock.Clock
}

// New creates the wearable service.
func New(options Options) (*Service, error) {
	if options.Repository == nil {
		return nil, errors.New("wearable repository is required")
	}
	timeSource := options.Clock
	if timeSource == nil {
		timeSource = clock.SystemClock{}
	}
	return &Service{repository: options.Repository, clock: timeSource}, nil
}

// Policy returns the stored location policy, or the conservative default when a
// family has never set one.
func (s *Service) Policy(
	ctx context.Context,
	familyID string,
) (domain.LocationPolicy, error) {
	familyID = strings.TrimSpace(familyID)
	if familyID == "" {
		return domain.LocationPolicy{}, domain.ErrInvalidPolicy
	}
	policy, ok, err := s.repository.Policy(ctx, familyID)
	if err != nil {
		return domain.LocationPolicy{}, err
	}
	if !ok {
		return defaultPolicy(familyID), nil
	}
	return policy, nil
}

// UpdatePolicy validates and stores the family location policy. Revoking the
// permission also clears the retained positions so a revoked permission never
// keeps reporting a position.
func (s *Service) UpdatePolicy(
	ctx context.Context,
	familyID string,
	input domain.LocationPolicy,
) (domain.LocationPolicy, error) {
	familyID = strings.TrimSpace(familyID)
	if familyID == "" {
		return domain.LocationPolicy{}, domain.ErrInvalidPolicy
	}
	normalized, err := domain.ValidatePolicy(input)
	if err != nil {
		return domain.LocationPolicy{}, err
	}
	normalized.FamilyID = familyID
	normalized.UpdatedAt = s.clock.Now().UTC()
	if err := s.repository.SavePolicy(ctx, normalized); err != nil {
		return domain.LocationPolicy{}, err
	}
	if !normalized.PermissionGranted {
		if err := s.repository.ClearFamilyLocations(ctx, familyID); err != nil {
			return domain.LocationPolicy{}, err
		}
	}
	return normalized, nil
}

// CreateContact validates and stores one emergency contact.
func (s *Service) CreateContact(
	ctx context.Context,
	familyID string,
	input domain.ContactInput,
) (domain.EmergencyContact, error) {
	familyID = strings.TrimSpace(familyID)
	if familyID == "" {
		return domain.EmergencyContact{}, domain.ErrInvalidContact
	}
	normalized, err := domain.ValidateContactInput(input)
	if err != nil {
		return domain.EmergencyContact{}, err
	}
	contact := domain.EmergencyContact{
		ID:          uuid.NewString(),
		FamilyID:    familyID,
		ChildID:     strings.TrimSpace(normalized.ChildID),
		DisplayName: strings.TrimSpace(normalized.DisplayName),
		Phone:       strings.TrimSpace(normalized.Phone),
		Role:        normalized.Role,
		Priority:    normalized.Priority,
		Enabled:     normalized.Enabled,
	}
	if err := s.repository.CreateContact(ctx, contact); err != nil {
		return domain.EmergencyContact{}, err
	}
	contact.CreatedAt = s.clock.Now().UTC()
	contact.UpdatedAt = contact.CreatedAt
	return contact, nil
}

// UpdateContact validates and stores one emergency contact edit.
func (s *Service) UpdateContact(
	ctx context.Context,
	familyID string,
	contactID string,
	input domain.ContactInput,
) (domain.EmergencyContact, error) {
	familyID = strings.TrimSpace(familyID)
	contactID = strings.TrimSpace(contactID)
	if familyID == "" || contactID == "" {
		return domain.EmergencyContact{}, domain.ErrInvalidContact
	}
	normalized, err := domain.ValidateContactInput(input)
	if err != nil {
		return domain.EmergencyContact{}, err
	}
	contact := domain.EmergencyContact{
		ID:          contactID,
		FamilyID:    familyID,
		ChildID:     strings.TrimSpace(normalized.ChildID),
		DisplayName: strings.TrimSpace(normalized.DisplayName),
		Phone:       strings.TrimSpace(normalized.Phone),
		Role:        normalized.Role,
		Priority:    normalized.Priority,
		Enabled:     normalized.Enabled,
	}
	if err := s.repository.UpdateContact(ctx, contact); err != nil {
		return domain.EmergencyContact{}, err
	}
	return contact, nil
}

// DeleteContact removes one emergency contact.
func (s *Service) DeleteContact(
	ctx context.Context,
	familyID string,
	contactID string,
) error {
	familyID = strings.TrimSpace(familyID)
	contactID = strings.TrimSpace(contactID)
	if familyID == "" || contactID == "" {
		return domain.ErrInvalidContact
	}
	return s.repository.DeleteContact(ctx, familyID, contactID)
}

// Contacts returns every emergency contact in a family.
func (s *Service) Contacts(
	ctx context.Context,
	familyID string,
) ([]domain.EmergencyContact, error) {
	familyID = strings.TrimSpace(familyID)
	if familyID == "" {
		return nil, domain.ErrInvalidContact
	}
	return s.repository.Contacts(ctx, familyID)
}

// Geofence returns the stored home geofence, if a family set one.
func (s *Service) Geofence(
	ctx context.Context,
	familyID string,
) (domain.Geofence, bool, error) {
	familyID = strings.TrimSpace(familyID)
	if familyID == "" {
		return domain.Geofence{}, false, domain.ErrInvalidGeofence
	}
	return s.repository.Geofence(ctx, familyID)
}

// UpdateGeofence validates and stores the single home geofence for a family.
func (s *Service) UpdateGeofence(
	ctx context.Context,
	familyID string,
	input domain.GeofenceInput,
) (domain.Geofence, error) {
	familyID = strings.TrimSpace(familyID)
	if familyID == "" {
		return domain.Geofence{}, domain.ErrInvalidGeofence
	}
	normalized, err := domain.ValidateGeofenceInput(input)
	if err != nil {
		return domain.Geofence{}, err
	}
	geofence := domain.Geofence{
		ID:           uuid.NewString(),
		FamilyID:     familyID,
		ChildID:      strings.TrimSpace(normalized.ChildID),
		Latitude:     normalized.Latitude,
		Longitude:    normalized.Longitude,
		RadiusMeters: normalized.RadiusMeters,
		Enabled:      normalized.Enabled,
	}
	// Keep the stable id when a geofence already exists so an edit is an update.
	if existing, ok, err := s.repository.Geofence(ctx, familyID); err != nil {
		return domain.Geofence{}, err
	} else if ok {
		geofence.ID = existing.ID
		geofence.CreatedAt = existing.CreatedAt
	}
	if err := s.repository.SaveGeofence(ctx, geofence); err != nil {
		return domain.Geofence{}, err
	}
	return geofence, nil
}

// ReportLocation stores one bounded device position and derives the geofence
// state from the stored home geofence.
func (s *Service) ReportLocation(
	ctx context.Context,
	familyID string,
	deviceID string,
	report domain.LocationReport,
) (domain.DeviceStatus, error) {
	familyID = strings.TrimSpace(familyID)
	deviceID = strings.TrimSpace(deviceID)
	if familyID == "" || deviceID == "" {
		return domain.DeviceStatus{}, domain.ErrInvalidLocation
	}
	normalized, err := domain.ValidateLocationReport(report)
	if err != nil {
		return domain.DeviceStatus{}, err
	}

	policy, ok, err := s.repository.Policy(ctx, familyID)
	if err != nil {
		return domain.DeviceStatus{}, err
	}
	// A report for a family without a granted permission is refused so a device
	// can never report against a revoked permission.
	if !ok || !policy.Enabled || !policy.PermissionGranted {
		return domain.DeviceStatus{}, domain.ErrInvalidLocation
	}

	geofenceState := domain.GeofenceUnknown
	geofence, hasGeofence, err := s.repository.Geofence(ctx, familyID)
	if err != nil {
		return domain.DeviceStatus{}, err
	}
	if hasGeofence && geofence.Enabled {
		distance := haversineMeters(
			geofence.Latitude,
			geofence.Longitude,
			normalized.Latitude,
			normalized.Longitude,
		)
		geofenceState = domain.GeofenceStateForDistance(
			geofence.RadiusMeters,
			distance,
		)
	}

	activeSOS, hasSOS, err := s.repository.ActiveSOSByDevice(ctx, familyID, deviceID)
	if err != nil {
		return domain.DeviceStatus{}, err
	}
	sosState := domain.SOSStateIdle
	if hasSOS {
		sosState = activeSOS.State
	}

	status := domain.DeviceStatus{
		DeviceID:          deviceID,
		FamilyID:          familyID,
		LocationEnabled:   policy.Enabled,
		PermissionGranted: policy.PermissionGranted,
		Precision:         normalized.Precision,
		SOSState:          sosState,
		Latitude:          normalized.Latitude,
		Longitude:         normalized.Longitude,
		AccuracyMeters:    normalized.AccuracyMeters,
		GeofenceState:     geofenceState,
	}
	if err := s.repository.RecordStatus(ctx, status); err != nil {
		return domain.DeviceStatus{}, err
	}
	return status, nil
}

// DeviceStatuses returns each device wearable snapshot in a family.
func (s *Service) DeviceStatuses(
	ctx context.Context,
	familyID string,
) ([]domain.DeviceStatusView, error) {
	familyID = strings.TrimSpace(familyID)
	if familyID == "" {
		return nil, domain.ErrInvalidLocation
	}
	statuses, err := s.repository.Statuses(ctx, familyID)
	if err != nil {
		return nil, err
	}
	devices := make([]domain.DeviceStatusView, 0, len(statuses))
	for _, status := range statuses {
		devices = append(devices, domain.DeviceStatusView{
			DeviceID:          status.DeviceID,
			FamilyID:          status.FamilyID,
			LocationEnabled:   status.LocationEnabled,
			PermissionGranted: status.PermissionGranted,
			Precision:         status.Precision,
			SOSState:          status.SOSState,
			Latitude:          roundCoordinate(status.Latitude, status.Precision),
			Longitude:         roundCoordinate(status.Longitude, status.Precision),
			AccuracyMeters:    status.AccuracyMeters,
			GeofenceState:     status.GeofenceState,
			LastReportedAt:    status.LastReportedAt,
		})
	}
	return devices, nil
}

// TriggerSOS stores a latched SOS event from a device. A second trigger while an
// SOS is already active returns the existing event so a button bounce never
// creates a duplicate.
func (s *Service) TriggerSOS(
	ctx context.Context,
	familyID string,
	input domain.SOSInput,
) (domain.SOSEvent, error) {
	familyID = strings.TrimSpace(familyID)
	if familyID == "" {
		return domain.SOSEvent{}, domain.ErrInvalidSOS
	}
	normalized, err := domain.ValidateSOSInput(input)
	if err != nil {
		return domain.SOSEvent{}, err
	}
	deviceID := strings.TrimSpace(normalized.DeviceID)
	if existing, ok, err := s.repository.ActiveSOSByDevice(ctx, familyID, deviceID); err != nil {
		return domain.SOSEvent{}, err
	} else if ok {
		return existing, nil
	}
	event := domain.SOSEvent{
		ID:        uuid.NewString(),
		FamilyID:  familyID,
		DeviceID:  deviceID,
		Latitude:  normalized.Latitude,
		Longitude: normalized.Longitude,
		State:     domain.SOSStateTriggered,
	}
	if err := s.repository.CreateSOS(ctx, event); err != nil {
		return domain.SOSEvent{}, err
	}
	event.TriggeredAt = s.clock.Now().UTC()
	return event, nil
}

// CancelSOS transitions one SOS event to cancelled within a family.
func (s *Service) CancelSOS(
	ctx context.Context,
	familyID string,
	eventID string,
) (domain.SOSEvent, error) {
	familyID = strings.TrimSpace(familyID)
	eventID = strings.TrimSpace(eventID)
	if familyID == "" || eventID == "" {
		return domain.SOSEvent{}, domain.ErrInvalidSOS
	}
	event, ok, err := s.repository.CancelSOS(ctx, familyID, eventID)
	if err != nil {
		return domain.SOSEvent{}, err
	}
	if !ok {
		return domain.SOSEvent{}, domain.ErrSOSNotFound
	}
	return event, nil
}

// SOSHistory returns the most recent SOS events in a family.
func (s *Service) SOSHistory(
	ctx context.Context,
	familyID string,
	limit int,
) ([]domain.SOSEvent, error) {
	familyID = strings.TrimSpace(familyID)
	if familyID == "" {
		return nil, domain.ErrInvalidSOS
	}
	if limit <= 0 || limit > domain.SOSHistoryMaxLimit {
		limit = domain.SOSHistoryDefaultLimit
	}
	return s.repository.SOSEvents(ctx, familyID, limit)
}

// ActiveSOS returns every active SOS in a family.
func (s *Service) ActiveSOS(
	ctx context.Context,
	familyID string,
) ([]domain.SOSEvent, error) {
	familyID = strings.TrimSpace(familyID)
	if familyID == "" {
		return nil, domain.ErrInvalidSOS
	}
	return s.repository.ActiveSOS(ctx, familyID)
}

// Capability summarizes whether a device can report wearable location.
func (s *Service) Capability(
	ctx context.Context,
	familyID string,
	deviceID string,
) (domain.DeviceStatusView, error) {
	familyID = strings.TrimSpace(familyID)
	deviceID = strings.TrimSpace(deviceID)
	if familyID == "" || deviceID == "" {
		return domain.DeviceStatusView{}, domain.ErrInvalidLocation
	}
	statuses, err := s.repository.Statuses(ctx, familyID)
	if err != nil {
		return domain.DeviceStatusView{}, err
	}
	for _, status := range statuses {
		if status.DeviceID != deviceID {
			continue
		}
		return domain.DeviceStatusView{
			DeviceID:          status.DeviceID,
			FamilyID:          status.FamilyID,
			LocationEnabled:   status.LocationEnabled,
			PermissionGranted: status.PermissionGranted,
			Precision:         status.Precision,
			SOSState:          status.SOSState,
			Latitude:          roundCoordinate(status.Latitude, status.Precision),
			Longitude:         roundCoordinate(status.Longitude, status.Precision),
			AccuracyMeters:    status.AccuracyMeters,
			GeofenceState:     status.GeofenceState,
			LastReportedAt:    status.LastReportedAt,
		}, nil
	}
	return domain.DeviceStatusView{
		DeviceID:      deviceID,
		FamilyID:      familyID,
		Precision:     domain.PrecisionCoarse,
		SOSState:      domain.SOSStateIdle,
		GeofenceState: domain.GeofenceUnknown,
	}, nil
}

// AdminStats returns a bounded, windowed fleet summary.
func (s *Service) AdminStats(
	ctx context.Context,
	windowDays int,
) (domain.AdminStats, error) {
	if windowDays <= 0 || windowDays > 90 {
		windowDays = 7
	}
	return s.repository.AdminStats(ctx, windowDays)
}

func defaultPolicy(familyID string) domain.LocationPolicy {
	return domain.LocationPolicy{
		FamilyID:              familyID,
		Enabled:               false,
		PermissionGranted:     false,
		Precision:             domain.PrecisionCoarse,
		UploadIntervalSeconds: 900,
		RetentionDays:         7,
		GeofenceRadiusMeters:  500,
	}
}

// roundCoordinate rounds a coordinate to the precision band so a coarse policy
// stores and returns a coarser value than a precise one.
func roundCoordinate(value float64, precision string) float64 {
	digits := 2
	switch precision {
	case domain.PrecisionPrecise:
		digits = 5
	case domain.PrecisionBalanced:
		digits = 3
	default:
		digits = 2
	}
	factor := math.Pow(10, float64(digits))
	return math.Round(value*factor) / factor
}

// haversineMeters returns the great-circle distance between two coordinates.
func haversineMeters(lat1 float64, lon1 float64, lat2 float64, lon2 float64) float64 {
	const earthRadiusMeters = 6371000.0
	toRadians := math.Pi / 180
	dLat := (lat2 - lat1) * toRadians
	dLon := (lon2 - lon1) * toRadians
	a := math.Sin(dLat/2)*math.Sin(dLat/2) +
		math.Cos(lat1*toRadians)*math.Cos(lat2*toRadians)*
			math.Sin(dLon/2)*math.Sin(dLon/2)
	c := 2 * math.Atan2(math.Sqrt(a), math.Sqrt(1-a))
	return earthRadiusMeters * c
}
