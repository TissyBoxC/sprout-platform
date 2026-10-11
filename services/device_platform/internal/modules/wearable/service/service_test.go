package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/wearable/domain"
)

// fixedClock returns a stable instant so the stored stamps are deterministic.
type fixedClock struct{}

func (fixedClock) Now() time.Time {
	return time.Date(2026, time.October, 11, 9, 0, 0, 0, time.UTC)
}

type fakeRepository struct {
	policies  map[string]domain.LocationPolicy
	contacts  map[string]domain.EmergencyContact
	geofences map[string]domain.Geofence
	statuses  map[string]domain.DeviceStatus
	sos       map[string]domain.SOSEvent
	stats     domain.AdminStats
	cleared   []string
}

func newFakeRepository() *fakeRepository {
	return &fakeRepository{
		policies:  map[string]domain.LocationPolicy{},
		contacts:  map[string]domain.EmergencyContact{},
		geofences: map[string]domain.Geofence{},
		statuses:  map[string]domain.DeviceStatus{},
		sos:       map[string]domain.SOSEvent{},
	}
}

func (r *fakeRepository) Policy(
	_ context.Context,
	familyID string,
) (domain.LocationPolicy, bool, error) {
	policy, ok := r.policies[familyID]
	return policy, ok, nil
}

func (r *fakeRepository) SavePolicy(
	_ context.Context,
	policy domain.LocationPolicy,
) error {
	r.policies[policy.FamilyID] = policy
	return nil
}

func (r *fakeRepository) ClearFamilyLocations(
	_ context.Context,
	familyID string,
) error {
	r.cleared = append(r.cleared, familyID)
	for id, status := range r.statuses {
		if status.FamilyID == familyID {
			delete(r.statuses, id)
		}
	}
	return nil
}

func (r *fakeRepository) CreateContact(
	_ context.Context,
	contact domain.EmergencyContact,
) error {
	r.contacts[contact.ID] = contact
	return nil
}

func (r *fakeRepository) UpdateContact(
	_ context.Context,
	contact domain.EmergencyContact,
) error {
	if _, ok := r.contacts[contact.ID]; !ok {
		return domain.ErrContactNotFound
	}
	r.contacts[contact.ID] = contact
	return nil
}

func (r *fakeRepository) DeleteContact(
	_ context.Context,
	_ string,
	contactID string,
) error {
	if _, ok := r.contacts[contactID]; !ok {
		return domain.ErrContactNotFound
	}
	delete(r.contacts, contactID)
	return nil
}

func (r *fakeRepository) Contacts(
	_ context.Context,
	_ string,
) ([]domain.EmergencyContact, error) {
	contacts := make([]domain.EmergencyContact, 0, len(r.contacts))
	for _, contact := range r.contacts {
		contacts = append(contacts, contact)
	}
	return contacts, nil
}

func (r *fakeRepository) Contact(
	_ context.Context,
	_ string,
	contactID string,
) (domain.EmergencyContact, bool, error) {
	contact, ok := r.contacts[contactID]
	return contact, ok, nil
}

func (r *fakeRepository) SaveGeofence(
	_ context.Context,
	geofence domain.Geofence,
) error {
	r.geofences[geofence.FamilyID] = geofence
	return nil
}

func (r *fakeRepository) Geofence(
	_ context.Context,
	familyID string,
) (domain.Geofence, bool, error) {
	geofence, ok := r.geofences[familyID]
	return geofence, ok, nil
}

func (r *fakeRepository) RecordStatus(
	_ context.Context,
	status domain.DeviceStatus,
) error {
	r.statuses[status.DeviceID] = status
	return nil
}

func (r *fakeRepository) Statuses(
	_ context.Context,
	_ string,
) ([]domain.DeviceStatus, error) {
	statuses := make([]domain.DeviceStatus, 0, len(r.statuses))
	for _, status := range r.statuses {
		statuses = append(statuses, status)
	}
	return statuses, nil
}

func (r *fakeRepository) CreateSOS(
	_ context.Context,
	event domain.SOSEvent,
) error {
	r.sos[event.ID] = event
	return nil
}

func (r *fakeRepository) CancelSOS(
	_ context.Context,
	_ string,
	eventID string,
) (domain.SOSEvent, bool, error) {
	event, ok := r.sos[eventID]
	if !ok || event.State != domain.SOSStateTriggered {
		return domain.SOSEvent{}, false, nil
	}
	event.State = domain.SOSStateCancelled
	r.sos[eventID] = event
	return event, true, nil
}

func (r *fakeRepository) SOSEvents(
	_ context.Context,
	_ string,
	_ int,
) ([]domain.SOSEvent, error) {
	events := make([]domain.SOSEvent, 0, len(r.sos))
	for _, event := range r.sos {
		events = append(events, event)
	}
	return events, nil
}

func (r *fakeRepository) ActiveSOS(
	_ context.Context,
	familyID string,
) ([]domain.SOSEvent, error) {
	events := make([]domain.SOSEvent, 0, 4)
	for _, event := range r.sos {
		if event.FamilyID == familyID && event.State == domain.SOSStateTriggered {
			events = append(events, event)
		}
	}
	return events, nil
}

func (r *fakeRepository) ActiveSOSByDevice(
	_ context.Context,
	familyID string,
	deviceID string,
) (domain.SOSEvent, bool, error) {
	for _, event := range r.sos {
		if event.FamilyID == familyID && event.DeviceID == deviceID &&
			event.State == domain.SOSStateTriggered {
			return event, true, nil
		}
	}
	return domain.SOSEvent{}, false, nil
}

func (r *fakeRepository) AdminStats(
	_ context.Context,
	windowDays int,
) (domain.AdminStats, error) {
	stats := r.stats
	stats.WindowDays = windowDays
	return stats, nil
}

func newService(t *testing.T, repository *fakeRepository) *Service {
	t.Helper()
	service, err := New(Options{Repository: repository, Clock: fixedClock{}})
	if err != nil {
		t.Fatalf("new service: %v", err)
	}
	return service
}

func TestPolicyDefaultsWithoutStoredPolicy(t *testing.T) {
	service := newService(t, newFakeRepository())
	policy, err := service.Policy(context.Background(), "family-1")
	if err != nil {
		t.Fatalf("policy: %v", err)
	}
	if policy.Enabled || policy.PermissionGranted {
		t.Fatalf("expected a disabled default policy, got %+v", policy)
	}
	if policy.Precision != domain.PrecisionCoarse {
		t.Fatalf("expected coarse precision, got %q", policy.Precision)
	}
}

func TestUpdatePolicyClampsAndClearsOnRevoke(t *testing.T) {
	repository := newFakeRepository()
	service := newService(t, repository)

	granted, err := service.UpdatePolicy(context.Background(), "family-1", domain.LocationPolicy{
		Enabled:               true,
		PermissionGranted:     true,
		Precision:             domain.PrecisionBalanced,
		UploadIntervalSeconds: 60,
		RetentionDays:         30,
		GeofenceRadiusMeters:  200,
	})
	if err != nil {
		t.Fatalf("update policy: %v", err)
	}
	if !granted.PermissionGranted {
		t.Fatalf("expected the permission to be granted")
	}

	// Out-of-range values are clamped into the documented bounds.
	clamped, err := service.UpdatePolicy(context.Background(), "family-1", domain.LocationPolicy{
		Enabled:               true,
		PermissionGranted:     true,
		Precision:             domain.PrecisionPrecise,
		UploadIntervalSeconds: 999999,
		RetentionDays:         999999,
		GeofenceRadiusMeters:  99999999,
	})
	if err != nil {
		t.Fatalf("update policy: %v", err)
	}
	if clamped.UploadIntervalSeconds != domain.MaxUploadIntervalSeconds {
		t.Fatalf("expected the interval to clamp up, got %d", clamped.UploadIntervalSeconds)
	}
	if clamped.RetentionDays != domain.MaxRetentionDays {
		t.Fatalf("expected the retention to clamp up, got %d", clamped.RetentionDays)
	}
	if clamped.GeofenceRadiusMeters != domain.MaxGeofenceRadiusMeters {
		t.Fatalf("expected the radius to clamp up, got %d", clamped.GeofenceRadiusMeters)
	}

	// An invalid precision is refused.
	if _, err := service.UpdatePolicy(context.Background(), "family-1", domain.LocationPolicy{
		Precision: "wild",
	}); !errors.Is(err, domain.ErrInvalidPolicy) {
		t.Fatalf("expected ErrInvalidPolicy, got %v", err)
	}

	// Revoking the permission clears the retained positions.
	if _, err := service.UpdatePolicy(context.Background(), "family-1", domain.LocationPolicy{
		Enabled:               true,
		PermissionGranted:     false,
		Precision:             domain.PrecisionCoarse,
		UploadIntervalSeconds: 300,
		RetentionDays:         7,
		GeofenceRadiusMeters:  500,
	}); err != nil {
		t.Fatalf("revoke policy: %v", err)
	}
	if len(repository.cleared) == 0 {
		t.Fatalf("expected revoked policy to clear stored locations")
	}
}

func TestReportLocationRequiresGrantedPermission(t *testing.T) {
	repository := newFakeRepository()
	service := newService(t, repository)

	report := domain.LocationReport{
		Latitude:       1.3521,
		Longitude:      103.8198,
		AccuracyMeters: 8,
		Precision:      domain.PrecisionBalanced,
	}
	// Without a granted permission the report is refused.
	if _, err := service.ReportLocation(
		context.Background(), "family-1", "device-1", report,
	); !errors.Is(err, domain.ErrInvalidLocation) {
		t.Fatalf("expected ErrInvalidLocation before grant, got %v", err)
	}

	if _, err := service.UpdatePolicy(context.Background(), "family-1", domain.LocationPolicy{
		Enabled:               true,
		PermissionGranted:     true,
		Precision:             domain.PrecisionBalanced,
		UploadIntervalSeconds: 300,
		RetentionDays:         7,
		GeofenceRadiusMeters:  500,
	}); err != nil {
		t.Fatalf("grant policy: %v", err)
	}

	status, err := service.ReportLocation(
		context.Background(), "family-1", "device-1", report,
	)
	if err != nil {
		t.Fatalf("report location: %v", err)
	}
	if !status.PermissionGranted || !status.LocationEnabled {
		t.Fatalf("expected the status to carry the granted permission, got %+v", status)
	}
	if status.GeofenceState != domain.GeofenceUnknown {
		t.Fatalf("expected unknown geofence without a home point, got %q", status.GeofenceState)
	}

	// An out-of-range coordinate is refused.
	if _, err := service.ReportLocation(context.Background(), "family-1", "device-1",
		domain.LocationReport{
			Latitude:  120,
			Longitude: 103.8,
			Precision: domain.PrecisionCoarse,
		}); !errors.Is(err, domain.ErrInvalidLocation) {
		t.Fatalf("expected ErrInvalidLocation for a bad latitude, got %v", err)
	}
}

func TestGeofenceDerivation(t *testing.T) {
	repository := newFakeRepository()
	service := newService(t, repository)
	if _, err := service.UpdatePolicy(context.Background(), "family-1", domain.LocationPolicy{
		Enabled:               true,
		PermissionGranted:     true,
		Precision:             domain.PrecisionPrecise,
		UploadIntervalSeconds: 300,
		RetentionDays:         7,
		GeofenceRadiusMeters:  500,
	}); err != nil {
		t.Fatalf("grant policy: %v", err)
	}
	// A home point at 0,0 with a 500 m radius: a nearby point is inside, a far
	// point is outside.
	if _, err := service.UpdateGeofence(context.Background(), "family-1", domain.GeofenceInput{
		Latitude:     0,
		Longitude:    0,
		RadiusMeters: 500,
		Enabled:      true,
	}); err != nil {
		t.Fatalf("update geofence: %v", err)
	}

	inside, err := service.ReportLocation(context.Background(), "family-1", "device-1",
		domain.LocationReport{Latitude: 0.001, Longitude: 0.001, Precision: domain.PrecisionPrecise})
	if err != nil {
		t.Fatalf("report inside: %v", err)
	}
	if inside.GeofenceState != domain.GeofenceInside {
		t.Fatalf("expected inside, got %q", inside.GeofenceState)
	}

	outside, err := service.ReportLocation(context.Background(), "family-1", "device-1",
		domain.LocationReport{Latitude: 0.1, Longitude: 0.1, Precision: domain.PrecisionPrecise})
	if err != nil {
		t.Fatalf("report outside: %v", err)
	}
	if outside.GeofenceState != domain.GeofenceOutside {
		t.Fatalf("expected outside, got %q", outside.GeofenceState)
	}
}

func TestSOSLatchAndCancel(t *testing.T) {
	repository := newFakeRepository()
	service := newService(t, repository)

	event, err := service.TriggerSOS(context.Background(), "family-1", domain.SOSInput{
		DeviceID:  "device-1",
		Latitude:  1.35,
		Longitude: 103.81,
	})
	if err != nil {
		t.Fatalf("trigger sos: %v", err)
	}
	if event.State != domain.SOSStateTriggered {
		t.Fatalf("expected a triggered sos, got %q", event.State)
	}

	// A second trigger returns the same latched event.
	again, err := service.TriggerSOS(context.Background(), "family-1", domain.SOSInput{
		DeviceID:  "device-1",
		Latitude:  1.36,
		Longitude: 103.82,
	})
	if err != nil {
		t.Fatalf("re-trigger sos: %v", err)
	}
	if again.ID != event.ID {
		t.Fatalf("expected the latched event to be reused, got a new id")
	}

	active, err := service.ActiveSOS(context.Background(), "family-1")
	if err != nil {
		t.Fatalf("active sos: %v", err)
	}
	if len(active) != 1 {
		t.Fatalf("expected one active sos, got %d", len(active))
	}

	cancelled, err := service.CancelSOS(context.Background(), "family-1", event.ID)
	if err != nil {
		t.Fatalf("cancel sos: %v", err)
	}
	if cancelled.State != domain.SOSStateCancelled {
		t.Fatalf("expected cancelled, got %q", cancelled.State)
	}

	// Cancelling a non-active event is a not-found.
	if _, err := service.CancelSOS(context.Background(), "family-1", "missing"); !errors.Is(err, domain.ErrSOSNotFound) {
		t.Fatalf("expected ErrSOSNotFound, got %v", err)
	}

	// An invalid trigger is refused.
	if _, err := service.TriggerSOS(context.Background(), "family-1", domain.SOSInput{
		DeviceID: "",
	}); !errors.Is(err, domain.ErrInvalidSOS) {
		t.Fatalf("expected ErrInvalidSOS, got %v", err)
	}
}

func TestContactLifecycle(t *testing.T) {
	service := newService(t, newFakeRepository())
	contact, err := service.CreateContact(context.Background(), "family-1", domain.ContactInput{
		DisplayName: "奶奶",
		Phone:       "13800000000",
		Role:        domain.RoleGuardian,
		Priority:    1,
		Enabled:     true,
	})
	if err != nil {
		t.Fatalf("create contact: %v", err)
	}
	if contact.ID == "" {
		t.Fatalf("expected a generated contact id")
	}

	// An empty name or an unknown role is refused.
	if _, err := service.CreateContact(context.Background(), "family-1", domain.ContactInput{
		DisplayName: "",
		Phone:       "13800000000",
		Role:        domain.RoleGuardian,
	}); !errors.Is(err, domain.ErrInvalidContact) {
		t.Fatalf("expected ErrInvalidContact, got %v", err)
	}
	if _, err := service.CreateContact(context.Background(), "family-1", domain.ContactInput{
		DisplayName: "奶奶",
		Phone:       "13800000000",
		Role:        "stranger",
	}); !errors.Is(err, domain.ErrInvalidContact) {
		t.Fatalf("expected ErrInvalidContact, got %v", err)
	}

	if err := service.DeleteContact(context.Background(), "family-1", contact.ID); err != nil {
		t.Fatalf("delete contact: %v", err)
	}
	contacts, err := service.Contacts(context.Background(), "family-1")
	if err != nil {
		t.Fatalf("contacts: %v", err)
	}
	if len(contacts) != 0 {
		t.Fatalf("expected no contacts, got %d", len(contacts))
	}
}

func TestAdminStatsWindowClamp(t *testing.T) {
	repository := newFakeRepository()
	repository.stats = domain.AdminStats{DeviceCount: 3}
	service := newService(t, repository)
	stats, err := service.AdminStats(context.Background(), 0)
	if err != nil {
		t.Fatalf("admin stats: %v", err)
	}
	if stats.WindowDays != 7 {
		t.Fatalf("expected a default 7-day window, got %d", stats.WindowDays)
	}
	if stats.DeviceCount != 3 {
		t.Fatalf("expected the repository stats to pass through, got %d", stats.DeviceCount)
	}
}

func TestCapabilityWithoutStatus(t *testing.T) {
	service := newService(t, newFakeRepository())
	view, err := service.Capability(context.Background(), "family-1", "device-9")
	if err != nil {
		t.Fatalf("capability: %v", err)
	}
	if view.SOSState != domain.SOSStateIdle {
		t.Fatalf("expected idle sos, got %q", view.SOSState)
	}
	if view.Precision != domain.PrecisionCoarse {
		t.Fatalf("expected coarse precision, got %q", view.Precision)
	}
}
