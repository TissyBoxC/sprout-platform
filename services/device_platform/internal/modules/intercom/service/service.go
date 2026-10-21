// Package service orchestrates intercom call lifecycle.
//
// The service owns the rules that a single state transition cannot express on
// its own: whether the guardian may call this device at all, whether the family
// has intercom enabled, how often a guardian may ring, and closing calls that
// nobody ended.
package service

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/intercom/domain"
)

var (
	// ErrDeviceNotFound is returned when the device is not bound to this family.
	ErrDeviceNotFound = errors.New("device not found")
	// ErrCallNotFound is returned when the call does not belong to the caller.
	ErrCallNotFound = errors.New("call not found")
	// ErrConflict is returned when the call already moved to another state.
	ErrConflict = errors.New("call already moved")
	// ErrBusy is returned when the device is already in another call.
	ErrBusy = errors.New("device is busy")
)

// StoreSentinel* are the errors a Store must return so the service can map them.
var (
	// StoreSentinelNotFound means the requested call does not exist for that scope.
	StoreSentinelNotFound = errors.New("intercom call not found")
	// StoreSentinelBusy means the device already holds an unfinished call.
	StoreSentinelBusy = errors.New("device is already in a call")
)

// Store is the persistence the service needs.
//
// Implementations must translate their own storage errors into the sentinels
// below, so the service never depends on a particular database driver.
type Store interface {
	CreateRinging(ctx context.Context, callID, familyID, deviceID, childID string, startedAt time.Time) (domain.Call, error)
	Get(ctx context.Context, familyID, callID string) (domain.Call, error)
	GetForDevice(ctx context.Context, deviceID, callID string) (domain.Call, error)
	Transition(ctx context.Context, call domain.Call, expected domain.CallState) (domain.Call, error)
	CountStartedSince(ctx context.Context, familyID string, since time.Time) (int, error)
	ListOpen(ctx context.Context) ([]domain.Call, error)
	History(ctx context.Context, familyID string, limit int) ([]domain.Call, error)
	CountOpen(ctx context.Context) (int, error)
	CountCallsSince(ctx context.Context, since time.Time) (int, error)
}

// DeviceLookup resolves which family a device belongs to and its current child.
//
// It is a narrow interface so the service does not depend on the whole device
// binding module.
type DeviceLookup interface {
	// DeviceFamily returns the family that owns the device, or an empty string
	// when the device is unknown or unbound.
	DeviceFamily(ctx context.Context, deviceID string) (string, error)
	// DeviceChild returns the device's current child, or an empty string.
	DeviceChild(ctx context.Context, deviceID string) (string, error)
}

// IntercomSwitch reports whether a family may use intercom at all.
type IntercomSwitch interface {
	// IntercomEnabled reports the family's current intercom setting. A family
	// without an explicit setting is treated as enabled, because the guardian
	// still initiates every call and the child still accepts it.
	IntercomEnabled(ctx context.Context, familyID string) (bool, error)
}

// Service implements intercom call lifecycle.
type Service struct {
	store      Store
	devices    DeviceLookup
	switches   IntercomSwitch
	logger     *slog.Logger
	now        func() time.Time
	newID      func() (string, error)
}

// NewService builds the service.
//
// A nil switch means intercom is allowed for every family; production wiring
// always supplies the real one.
func NewService(store Store, devices DeviceLookup, switches IntercomSwitch, logger *slog.Logger) *Service {
	if logger == nil {
		logger = slog.Default()
	}
	return &Service{
		store:    store,
		devices:  devices,
		switches: switches,
		logger:   logger,
		now:      func() time.Time { return time.Now().UTC() },
		newID:    newCallID,
	}
}

// Start begins a call from a guardian to their device.
//
// It rejects devices outside the family, families with intercom switched off,
// and guardians who ring too often. The returned call is ringing: no audio is
// carried until the child accepts on the device.
func (service *Service) Start(ctx context.Context, request domain.Request) (domain.Call, error) {
	if err := request.Validate(); err != nil {
		return domain.Call{}, err
	}
	owner, err := service.devices.DeviceFamily(ctx, request.DeviceID)
	if err != nil {
		return domain.Call{}, fmt.Errorf("resolve device family: %w", err)
	}
	// An unknown device and another family's device must be indistinguishable.
	if owner == "" || owner != request.FamilyID {
		return domain.Call{}, ErrDeviceNotFound
	}
	if service.switches != nil {
		enabled, err := service.switches.IntercomEnabled(ctx, request.FamilyID)
		if err != nil {
			return domain.Call{}, fmt.Errorf("read intercom setting: %w", err)
		}
		if !enabled {
			return domain.Call{}, domain.ErrIntercomDisabled
		}
	}

	now := service.now()
	recent, err := service.store.CountStartedSince(ctx, request.FamilyID, now.Add(-domain.CallRateWindow))
	if err != nil {
		return domain.Call{}, fmt.Errorf("count recent calls: %w", err)
	}
	if recent >= domain.MaxCallsPerWindow {
		return domain.Call{}, domain.ErrRateLimited
	}

	childID := request.ChildID
	if childID == "" {
		childID, err = service.devices.DeviceChild(ctx, request.DeviceID)
		if err != nil {
			return domain.Call{}, fmt.Errorf("resolve device child: %w", err)
		}
	}

	callID, err := service.newID()
	if err != nil {
		return domain.Call{}, fmt.Errorf("generate call id: %w", err)
	}
	call, err := service.store.CreateRinging(ctx, callID, request.FamilyID, request.DeviceID, childID, now)
	if err != nil {
		if errors.Is(err, StoreSentinelBusy) {
			return domain.Call{}, ErrBusy
		}
		return domain.Call{}, fmt.Errorf("create call: %w", err)
	}
	// Log the lifecycle only: never audio, never the child's speech.
	service.logger.Info("intercom call ringing",
		"call_id", call.ID, "family_id", call.FamilyID, "device_id", call.DeviceID)
	return call, nil
}

// Accept lets the device report that the child pressed accept.
func (service *Service) Accept(ctx context.Context, deviceID, callID string) (domain.Call, error) {
	call, err := service.store.GetForDevice(ctx, deviceID, callID)
	if err != nil {
		return domain.Call{}, mapStoreError(err)
	}
	previous := call.State
	if err := call.Accept(service.now()); err != nil {
		return domain.Call{}, mapDomainError(err)
	}
	updated, err := service.store.Transition(ctx, call, previous)
	if err != nil {
		return domain.Call{}, mapStoreError(err)
	}
	service.logger.Info("intercom call accepted",
		"call_id", updated.ID, "device_id", updated.DeviceID)
	return updated, nil
}

// Decline lets the device report that the child refused the call.
func (service *Service) Decline(ctx context.Context, deviceID, callID string) (domain.Call, error) {
	return service.transitionForDevice(ctx, deviceID, callID, func(call *domain.Call, now time.Time) error {
		return call.Decline(now, "child_declined")
	})
}

// HangUp ends an active call from either side.
func (service *Service) HangUp(ctx context.Context, familyID, callID, reason string) (domain.Call, error) {
	if reason == "" {
		reason = "hangup"
	}
	return service.transitionForGuardian(ctx, familyID, callID, func(call *domain.Call, now time.Time) error {
		return call.HangUp(now, reason)
	})
}

// HangUpForDevice ends an active call reported by the device itself.
//
// The device authenticates with its own session, so the call is located by
// device rather than family: a device can only end a call it takes part in.
func (service *Service) HangUpForDevice(ctx context.Context, deviceID, callID, reason string) (domain.Call, error) {
	if reason == "" {
		reason = "hangup"
	}
	return service.transitionForDevice(ctx, deviceID, callID, func(call *domain.Call, now time.Time) error {
		return call.HangUp(now, reason)
	})
}

// Cancel stops a ring nobody answered yet.
func (service *Service) Cancel(ctx context.Context, familyID, callID string) (domain.Call, error) {
	return service.transitionForGuardian(ctx, familyID, callID, func(call *domain.Call, now time.Time) error {
		return call.Cancel(now)
	})
}

// Get returns one call owned by the family.
func (service *Service) Get(ctx context.Context, familyID, callID string) (domain.Call, error) {
	call, err := service.store.Get(ctx, familyID, callID)
	if err != nil {
		return domain.Call{}, mapStoreError(err)
	}
	return call, nil
}

// GetForDevice returns one call that involves the given device.
func (service *Service) GetForDevice(ctx context.Context, deviceID, callID string) (domain.Call, error) {
	call, err := service.store.GetForDevice(ctx, deviceID, callID)
	if err != nil {
		return domain.Call{}, mapStoreError(err)
	}
	return call, nil
}

// History returns the family's recent calls.
func (service *Service) History(ctx context.Context, familyID string, limit int) ([]domain.Call, error) {
	if limit <= 0 || limit > maxHistoryLimit {
		limit = defaultHistoryLimit
	}
	return service.store.History(ctx, familyID, limit)
}

// ExpireDue closes every call that ran past its deadline and returns how many
// were closed. It is safe to call repeatedly.
func (service *Service) ExpireDue(ctx context.Context) (int, error) {
	calls, err := service.store.ListOpen(ctx)
	if err != nil {
		return 0, err
	}
	now := service.now()
	closed := 0
	for _, call := range calls {
		if now.Before(call.Deadline()) {
			continue
		}
		previous := call.State
		if err := call.Expire(now); err != nil {
			continue
		}
		if _, err := service.store.Transition(ctx, call, previous); err != nil {
			if errors.Is(err, StoreSentinelBusy) {
				// Another worker closed it first; that is the outcome we wanted.
				continue
			}
			return closed, fmt.Errorf("expire call %s: %w", call.ID, err)
		}
		closed++
		service.logger.Info("intercom call closed by deadline",
			"call_id", call.ID, "state", call.State, "reason", call.DeclineReason)
	}
	return closed, nil
}

// Metrics is the read-only operational view used by the Feature Center.
type Metrics struct {
	OpenCalls   int
	CallsLast24 int
}

// ReadMetrics returns current call counts.
func (service *Service) ReadMetrics(ctx context.Context) (Metrics, error) {
	open, err := service.store.CountOpen(ctx)
	if err != nil {
		return Metrics{}, err
	}
	recent, err := service.store.CountCallsSince(ctx, service.now().Add(-24*time.Hour))
	if err != nil {
		return Metrics{}, err
	}
	return Metrics{OpenCalls: open, CallsLast24: recent}, nil
}

func (service *Service) transitionForDevice(
	ctx context.Context,
	deviceID string,
	callID string,
	apply func(call *domain.Call, now time.Time) error,
) (domain.Call, error) {
	call, err := service.store.GetForDevice(ctx, deviceID, callID)
	if err != nil {
		return domain.Call{}, mapStoreError(err)
	}
	return service.applyTransition(ctx, call, apply)
}

func (service *Service) transitionForGuardian(
	ctx context.Context,
	familyID string,
	callID string,
	apply func(call *domain.Call, now time.Time) error,
) (domain.Call, error) {
	call, err := service.store.Get(ctx, familyID, callID)
	if err != nil {
		return domain.Call{}, mapStoreError(err)
	}
	return service.applyTransition(ctx, call, apply)
}

func (service *Service) applyTransition(
	ctx context.Context,
	call domain.Call,
	apply func(call *domain.Call, now time.Time) error,
) (domain.Call, error) {
	previous := call.State
	if err := apply(&call, service.now()); err != nil {
		return domain.Call{}, mapDomainError(err)
	}
	updated, err := service.store.Transition(ctx, call, previous)
	if err != nil {
		return domain.Call{}, mapStoreError(err)
	}
	service.logger.Info("intercom call finished",
		"call_id", updated.ID, "state", updated.State, "reason", updated.DeclineReason)
	return updated, nil
}

const (
	defaultHistoryLimit = 20
	maxHistoryLimit     = 100
)

func mapStoreError(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, StoreSentinelNotFound):
		return ErrCallNotFound
	case errors.Is(err, StoreSentinelBusy):
		return ErrBusy
	default:
		return err
	}
}

func mapDomainError(err error) error {
	// A call that already moved, or a state that does not permit the requested
	// action, both mean the caller raced another actor. The transport answers
	// with one stable conflict code so the client reloads instead of retrying
	// blindly.
	switch {
	case errors.Is(err, domain.ErrCallFinished),
		errors.Is(err, domain.ErrNotRinging),
		errors.Is(err, domain.ErrNotActive):
		return ErrConflict
	default:
		return err
	}
}

// newCallID returns a random 128-bit identifier formatted as a UUIDv4 string.
func newCallID() (string, error) {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", err
	}
	raw[6] = (raw[6] & 0x0f) | 0x40
	raw[8] = (raw[8] & 0x3f) | 0x80
	encoded := hex.EncodeToString(raw[:])
	return encoded[0:8] + "-" + encoded[8:12] + "-" + encoded[12:16] + "-" +
		encoded[16:20] + "-" + encoded[20:32], nil
}
