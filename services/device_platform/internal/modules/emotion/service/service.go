// Package service coordinates mood sensing between the device that samples, the
// guardian who decides whether it runs, and the retention worker that removes
// what is no longer needed.
//
// The service owns the decisions the domain cannot make alone: whether the
// family turned sensing on, whether the reporting device actually serves the
// child it names, and how a storage failure becomes a stable error the
// transport can translate. It deliberately never imports the repository: the
// composition root translates repository sentinels into the StoreSentinel values
// below.
//
// Two products rules are enforced here rather than in a handler. Recording is
// refused when sensing is off, with a stable error rather than a silent drop, so
// a device can back off instead of retrying forever. And Summary is the only
// read a guardian has: it returns counts, shares, and trends, and its type has
// no place to put an observation instant.
package service

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/emotion/domain"
)

var (
	// ErrDisabled means the family has not turned sensing on. It is a stable
	// error, not a silent success: a device that believes its sample was stored
	// would keep sampling a child the guardian never opted in for.
	ErrDisabled = errors.New("emotion sensing is not enabled for this family")
	// ErrDeviceNotFound means the device is unknown or owned by another family.
	// Both cases answer identically so the endpoint cannot be used to discover
	// which devices exist.
	ErrDeviceNotFound = errors.New("emotion device not found")
	// ErrChildNotBound means the device is not bound to the reported child.
	ErrChildNotBound = errors.New("emotion child is not bound to this device")
	// ErrSummariseFailed means the aggregate could not be read.
	ErrSummariseFailed = errors.New("emotion summary unavailable")
	// ErrPurgeIncomplete means at least one family's expired samples remain, so
	// the sweep must not report success while a retention window is violated.
	ErrPurgeIncomplete = errors.New("emotion retention sweep incomplete")
)

var (
	// StoreSentinelNotFound marks a missing setting so the adapter can translate
	// a repository error without this package importing the repository.
	StoreSentinelNotFound = errors.New("emotion store: not found")
	// StoreSentinelNotBound marks a write refused because the device no longer
	// serves the child.
	StoreSentinelNotBound = errors.New("emotion store: child not bound")
)

// Store is the persistence the service needs.
type Store interface {
	GetSettings(ctx context.Context, familyID string) (domain.Settings, error)
	SaveSettings(ctx context.Context, settings domain.Settings) error
	// DeviceFamily returns the owning family, or an empty string when the
	// device is unknown.
	DeviceFamily(ctx context.Context, deviceID string) (string, error)
	DeviceChildBinding(ctx context.Context, familyID, deviceID, childID string) (bool, error)
	// InsertObservation stores one sample and refuses it when the binding
	// disappeared between the check and the write.
	InsertObservation(ctx context.Context, observation domain.Observation) error
	// CountLabels aggregates per label within [from, to). It returns no
	// instant and no identifier.
	CountLabels(
		ctx context.Context,
		familyID, childID string,
		from, to time.Time,
	) (map[domain.Label]domain.LabelCount, error)
	DeleteBefore(ctx context.Context, familyID string, cutoff time.Time) (int, error)
	ListEnabledFamilies(ctx context.Context, limit int) ([]domain.Settings, error)
	CountEnabledFamilies(ctx context.Context) (int, error)
	CountObservationsSince(ctx context.Context, since time.Time) (int, error)
}

// Options are the collaborators the service needs.
type Options struct {
	Store Store
	// Switches reads the family's setting inside the record path, so a caller
	// that already knows the setting still cannot record while it is off.
	Switches SettingsReader
	// Devices resolves the device's family and child binding. It is separate
	// from Store so the device seam can be faked without a database.
	Devices DeviceLookup
	Logger  *slog.Logger
}

// SettingsReader reports whether a family may sense at all.
type SettingsReader interface {
	SettingsEnabled(ctx context.Context, familyID string) (bool, error)
}

// DeviceLookup answers which family owns a device and which child it serves.
type DeviceLookup interface {
	DeviceFamily(ctx context.Context, deviceID string) (string, error)
	DeviceChild(ctx context.Context, deviceID string) (string, error)
}

// Service owns mood sensing.
type Service struct {
	store    Store
	switches SettingsReader
	devices  DeviceLookup
	logger   *slog.Logger
	now      func() time.Time
	newID    func() (string, error)
}

// NewService builds the service.
func NewService(options Options) *Service {
	logger := options.Logger
	if logger == nil {
		logger = slog.Default()
	}
	return &Service{
		store:    options.Store,
		switches: options.Switches,
		devices:  options.Devices,
		logger:   logger,
		now:      func() time.Time { return time.Now().UTC() },
		newID:    newID,
	}
}

// GetSettings returns the family's configuration.
//
// A family that never changed it reads as the disabled default, with the
// default retention. Returning an error for "no row" would force every caller to
// special-case a state that is in fact the norm.
func (service *Service) GetSettings(
	ctx context.Context,
	familyID string,
) (domain.Settings, error) {
	if familyID == "" {
		return domain.Settings{}, domain.ErrInvalidRequest
	}
	settings, err := service.store.GetSettings(ctx, familyID)
	if err != nil {
		if errors.Is(err, StoreSentinelNotFound) {
			return domain.DefaultSettings(familyID, service.now()), nil
		}
		return domain.Settings{}, err
	}
	if settings.RetentionDays <= 0 {
		// A row written before the retention rule existed still answers with a
		// bound inside the documented range.
		settings.RetentionDays = domain.DefaultRetentionDays
	}
	return settings, nil
}

// Enable turns sensing on for the family.
//
// Enabling is an explicit guardian decision recorded in its own call rather than
// a side effect of a PUT, so the operation is unambiguous in the audit trail and
// a retried request cannot flip the setting by accident.
func (service *Service) Enable(
	ctx context.Context,
	familyID string,
	retentionDays int,
) (domain.Settings, error) {
	return service.setEnabled(ctx, familyID, true, retentionDays)
}

// Disable turns sensing off for the family.
//
// Turning it off stops new samples immediately and keeps the retention choice.
// Stored samples are not deleted here: a guardian who turns the feature off for
// a week and back on again still has their trend, and deletion is an explicit
// privacy action rather than an accidental consequence of an off switch.
func (service *Service) Disable(
	ctx context.Context,
	familyID string,
	retentionDays int,
) (domain.Settings, error) {
	return service.setEnabled(ctx, familyID, false, retentionDays)
}

// UpdateSettings applies a full configuration change from the guardian.
//
// retentionDays of zero means "keep what is stored", because the settings screen
// saves the switch without resending an unchanged number.
func (service *Service) UpdateSettings(
	ctx context.Context,
	familyID string,
	enabled bool,
	retentionDays int,
) (domain.Settings, error) {
	if familyID == "" {
		return domain.Settings{}, domain.ErrInvalidRequest
	}
	current, err := service.GetSettings(ctx, familyID)
	if err != nil {
		return domain.Settings{}, err
	}
	if retentionDays == 0 {
		retentionDays = current.RetentionDays
	}
	return service.setEnabled(ctx, familyID, enabled, retentionDays)
}

// setEnabled validates and stores one configuration change.
func (service *Service) setEnabled(
	ctx context.Context,
	familyID string,
	enabled bool,
	retentionDays int,
) (domain.Settings, error) {
	if familyID == "" {
		return domain.Settings{}, domain.ErrInvalidRequest
	}
	now := service.now()
	settings := domain.DefaultSettings(familyID, now)
	if enabled || retentionDays != 0 {
		// Turning the feature on or naming a window must start from the stored
		// configuration, so the other field is preserved. A bare "off" keeps the
		// stored retention too, because Apply only replaces it when the request
		// names one.
		stored, err := service.GetSettings(ctx, familyID)
		if err != nil {
			return domain.Settings{}, err
		}
		settings = stored
	}
	if err := settings.Apply(enabled, retentionDays, now); err != nil {
		return domain.Settings{}, err
	}
	if err := service.store.SaveSettings(ctx, settings); err != nil {
		return domain.Settings{}, err
	}
	service.logger.Info("emotion settings updated",
		"family_id", settings.FamilyID,
		"enabled", settings.Enabled,
		"retention_days", settings.RetentionDays)
	return settings, nil
}

// Record stores one sample reported by a device.
//
// The order of the checks is the safety order: the device must exist and belong
// to the family, sensing must be on, the report itself must be well formed, and
// only then is the child binding confirmed. A refused report never reaches the
// table, so a disabled family has no rows even if a device keeps sending.
func (service *Service) Record(
	ctx context.Context,
	input domain.RecordInput,
) (domain.Observation, error) {
	now := service.now()
	normalised, err := input.Validate(now)
	if err != nil {
		return domain.Observation{}, err
	}
	familyID, err := service.devices.DeviceFamily(ctx, normalised.DeviceID)
	if err != nil {
		return domain.Observation{}, fmt.Errorf("resolve emotion device family: %w", err)
	}
	if familyID == "" {
		return domain.Observation{}, ErrDeviceNotFound
	}
	if err := service.requireEnabled(ctx, familyID); err != nil {
		return domain.Observation{}, err
	}

	childID := normalised.ChildID
	if childID == "" {
		// A device that does not name a child reports about the child it
		// currently serves. Without a binding there is no child, so the sample
		// would be an unattributed impression and is refused.
		childID, err = service.devices.DeviceChild(ctx, normalised.DeviceID)
		if err != nil {
			return domain.Observation{}, fmt.Errorf("resolve emotion device child: %w", err)
		}
	}
	if childID == "" {
		return domain.Observation{}, ErrChildNotBound
	}
	bound, err := service.store.DeviceChildBinding(
		ctx, familyID, normalised.DeviceID, childID,
	)
	if err != nil {
		return domain.Observation{}, err
	}
	if !bound {
		return domain.Observation{}, ErrChildNotBound
	}

	observationID, err := service.newID()
	if err != nil {
		return domain.Observation{}, err
	}
	observation := domain.Observation{
		ID:         observationID,
		FamilyID:   familyID,
		ChildID:    childID,
		DeviceID:   normalised.DeviceID,
		Label:      domain.Label(normalised.Label),
		Confidence: normalised.Confidence,
		ObservedAt: normalised.ObservedAt,
		CreatedAt:  now,
	}
	if err := service.store.InsertObservation(ctx, observation); err != nil {
		if errors.Is(err, StoreSentinelNotBound) {
			return domain.Observation{}, ErrChildNotBound
		}
		return domain.Observation{}, err
	}
	// Only the label, the confidence and the family are logged. The observation
	// instant is deliberately absent: pairing it with a family in a log line is
	// exactly the time-locatable record this feature must not keep.
	service.logger.Info("emotion observation recorded",
		"family_id", observation.FamilyID,
		"label", observation.Label,
		"confidence", observation.Confidence)
	return observation, nil
}

// Summary returns the guardian-visible aggregate for a window.
//
// The result carries counts, shares and trends and nothing else. The previous
// window of the same length is read too, so a trend is a real comparison rather
// than an invented arrow, and a window with too few samples reports "unknown"
// instead of a confident movement built on noise.
func (service *Service) Summary(
	ctx context.Context,
	familyID, childID string,
	days int,
) (domain.Summary, error) {
	if familyID == "" {
		return domain.Summary{}, domain.ErrInvalidRequest
	}
	windowDays, err := domain.NormaliseDays(days)
	if err != nil {
		return domain.Summary{}, err
	}
	settings, err := service.GetSettings(ctx, familyID)
	if err != nil {
		return domain.Summary{}, err
	}
	now := service.now()
	// The window ends at the start of tomorrow, so a sample taken this morning
	// is inside "the last seven days". Ending at the current instant would drop
	// the newest samples for up to a second and make the total move while the
	// guardian is looking at it.
	periodEnd := startOfDay(now).AddDate(0, 0, 1)
	periodStart := periodEnd.AddDate(0, 0, -windowDays)
	previousStart := periodStart.AddDate(0, 0, -windowDays)

	current, err := service.store.CountLabels(ctx, familyID, childID, periodStart, periodEnd)
	if err != nil {
		return domain.Summary{}, fmt.Errorf("%w: %v", ErrSummariseFailed, err)
	}
	previous, err := service.store.CountLabels(ctx, familyID, childID, previousStart, periodStart)
	if err != nil {
		return domain.Summary{}, fmt.Errorf("%w: %v", ErrSummariseFailed, err)
	}
	return domain.BuildSummary(
		familyID, childID, windowDays, periodStart, periodEnd,
		current, previous, settings,
	), nil
}

// PurgeExpired removes samples older than each family's retention window.
//
// It returns how many rows were deleted across every family, which is the only
// figure logged: a per-family retention log would itself describe a family's
// sensing history.
func (service *Service) PurgeExpired(ctx context.Context, now time.Time) (int, error) {
	now = now.UTC()
	families, err := service.store.ListEnabledFamilies(ctx, retentionFamilyBatch)
	if err != nil {
		return 0, err
	}
	removed := 0
	failures := 0
	for _, settings := range families {
		deleted, deleteErr := service.store.DeleteBefore(ctx, settings.FamilyID, settings.Cutoff(now))
		if deleteErr != nil {
			failures++
			service.logger.Warn("emotion retention delete failed",
				"family_id", settings.FamilyID, "error", deleteErr)
			continue
		}
		removed += deleted
	}
	if failures > 0 {
		return removed, fmt.Errorf("%w: %d families", ErrPurgeIncomplete, failures)
	}
	return removed, nil
}

// Metrics reports operational counters for the feature center.
//
// Both numbers are counts. No label breakdown is reported to an operator: a
// platform-wide mood histogram would describe children as a population, which is
// a profiling use the feature does not permit.
func (service *Service) Metrics(ctx context.Context) (Metrics, error) {
	enabled, err := service.store.CountEnabledFamilies(ctx)
	if err != nil {
		return Metrics{}, err
	}
	recent, err := service.store.CountObservationsSince(ctx, service.now().Add(-24*time.Hour))
	if err != nil {
		return Metrics{}, err
	}
	return Metrics{EnabledFamilies: enabled, ObservationsLast24: recent}, nil
}

// Metrics are read-only counters exposed to operators.
type Metrics struct {
	EnabledFamilies    int
	ObservationsLast24 int
}

// retentionFamilyBatch bounds one sweep so a long backlog cannot hold a worker
// loop open indefinitely.
const retentionFamilyBatch = 500

// requireEnabled refuses work for a family that has not opted in.
//
// The reading goes through the switch seam and falls back to the stored setting,
// so a caller that wires only one of them still cannot record for a disabled
// family.
func (service *Service) requireEnabled(ctx context.Context, familyID string) error {
	if service.switches != nil {
		enabled, err := service.switches.SettingsEnabled(ctx, familyID)
		if err != nil {
			return fmt.Errorf("read emotion switch: %w", err)
		}
		if !enabled {
			return ErrDisabled
		}
		return nil
	}
	settings, err := service.GetSettings(ctx, familyID)
	if err != nil {
		return err
	}
	if !settings.Enabled {
		return ErrDisabled
	}
	return nil
}

// startOfDay returns midnight UTC on the day of instant.
func startOfDay(instant time.Time) time.Time {
	utc := instant.UTC()
	return time.Date(utc.Year(), utc.Month(), utc.Day(), 0, 0, 0, 0, time.UTC)
}

// newID returns a random identifier shaped as a UUIDv4, because every identifier
// this module stores is a uuid column.
func newID() (string, error) {
	buffer := make([]byte, 16)
	if _, err := rand.Read(buffer); err != nil {
		return "", fmt.Errorf("generate emotion id: %w", err)
	}
	buffer[6] = (buffer[6] & 0x0f) | 0x40
	buffer[8] = (buffer[8] & 0x3f) | 0x80
	encoded := hex.EncodeToString(buffer)
	return encoded[0:8] + "-" + encoded[8:12] + "-" + encoded[12:16] + "-" +
		encoded[16:20] + "-" + encoded[20:32], nil
}
