// Package service coordinates care-view sessions across the device, the relay,
// and the guardian.
//
// A session moves requested -> streaming only after the device confirms it is
// showing its own indication, and every path that ends it also tells the device
// to stop capturing. The service never touches pixels: it owns permission,
// lifecycle, and deadline, and the relay owns bytes.
package service

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/care_view/domain"
)

var (
	// ErrDeviceNotFound means the device is unknown or owned by another family.
	// Both cases answer identically so the endpoint cannot be used to discover
	// which devices exist.
	ErrDeviceNotFound = errors.New("care view device not found")
	// ErrSessionNotFound means the session is unknown to this caller.
	ErrSessionNotFound = errors.New("care view session not found")
	// ErrConflict means the request does not fit the current state.
	ErrConflict = errors.New("care view session state conflict")
	// ErrBusy means the device already serves an open session.
	ErrBusy = errors.New("care view device busy")
)

var (
	// StoreSentinelNotFound marks a missing session so the adapter can translate
	// a repository error without this package importing the repository.
	StoreSentinelNotFound = errors.New("care view store: not found")
	// StoreSentinelBusy marks a concurrent transition conflict.
	StoreSentinelBusy = errors.New("care view store: conflict")
)

// Store is the persistence the service needs.
type Store interface {
	Create(ctx context.Context, session domain.Session) error
	Get(ctx context.Context, familyID, sessionID string) (domain.Session, error)
	GetForDevice(ctx context.Context, deviceID, sessionID string) (domain.Session, error)
	Transition(ctx context.Context, session domain.Session, expectedState domain.ViewState) error
	ListOpen(ctx context.Context) ([]domain.Session, error)
	History(ctx context.Context, familyID string, limit int) ([]domain.Session, error)
	CountOpen(ctx context.Context) (int, error)
	CountSessionsSince(ctx context.Context, familyID string, since time.Time) (int, error)
}

// DeviceLookup resolves device ownership.
type DeviceLookup interface {
	// DeviceFamily returns the owning family, or an empty string when the device
	// is unknown.
	DeviceFamily(ctx context.Context, deviceID string) (string, error)
	// DeviceChild returns the child currently bound to the device, if any.
	DeviceChild(ctx context.Context, deviceID string) (string, error)
}

// ViewSwitch reports whether a family may use care view at all.
type ViewSwitch interface {
	// ViewEnabled reports the family's setting. Unlike intercom, a missing row
	// means DISABLED: looking into a child's room requires an explicit decision.
	ViewEnabled(ctx context.Context, familyID string) (bool, error)
}

// ConsentReader reports the guardian's image consent for the family.
type ConsentReader interface {
	ImageConsentGranted(ctx context.Context, familyID string) (bool, error)
}

// DeviceNotifier pushes session control to the device.
type DeviceNotifier interface {
	NotifyCareViewStart(ctx context.Context, session domain.Session) error
	NotifyCareViewStop(ctx context.Context, session domain.Session) error
}

// Options are the collaborators the service needs.
type Options struct {
	Store    Store
	Devices  DeviceLookup
	Switches ViewSwitch
	Consent  ConsentReader
	Notifier DeviceNotifier
	Logger   *slog.Logger
}

// Service owns care-view lifecycle.
type Service struct {
	store    Store
	devices  DeviceLookup
	switches ViewSwitch
	consent  ConsentReader
	notifier DeviceNotifier
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
		devices:  options.Devices,
		switches: options.Switches,
		consent:  options.Consent,
		notifier: options.Notifier,
		logger:   logger,
		now:      func() time.Time { return time.Now().UTC() },
		newID:    newSessionID,
	}
}

// Start opens a viewing session for a device the family owns.
//
// The session starts in requested, not streaming: the device must confirm that
// it is showing the child an indication before a single frame may flow.
func (service *Service) Start(ctx context.Context, request domain.Request) (domain.Session, error) {
	if err := request.Validate(); err != nil {
		return domain.Session{}, err
	}
	// A device that belongs to someone else must look exactly like a missing one.
	familyID, err := service.devices.DeviceFamily(ctx, request.DeviceID)
	if err != nil {
		return domain.Session{}, fmt.Errorf("resolve device family: %w", err)
	}
	if familyID == "" || familyID != request.FamilyID {
		return domain.Session{}, ErrDeviceNotFound
	}
	if service.switches != nil {
		enabled, switchErr := service.switches.ViewEnabled(ctx, request.FamilyID)
		if switchErr != nil {
			return domain.Session{}, fmt.Errorf("read care view switch: %w", switchErr)
		}
		if !enabled {
			return domain.Session{}, domain.ErrDisabled
		}
	}
	// Consent is re-read on every start so withdrawing it takes effect at once.
	if service.consent != nil {
		granted, consentErr := service.consent.ImageConsentGranted(ctx, request.FamilyID)
		if consentErr != nil {
			return domain.Session{}, fmt.Errorf("read image consent: %w", consentErr)
		}
		if !granted {
			return domain.Session{}, domain.ErrNoConsent
		}
	}

	now := service.now()
	count, err := service.store.CountSessionsSince(ctx, request.FamilyID, now.Add(-domain.SessionRateWindow))
	if err != nil {
		return domain.Session{}, err
	}
	if count >= domain.MaxSessionsPerWindow {
		return domain.Session{}, domain.ErrRateLimited
	}

	childID := request.ChildID
	if childID == "" {
		childID, err = service.devices.DeviceChild(ctx, request.DeviceID)
		if err != nil {
			return domain.Session{}, fmt.Errorf("resolve device child: %w", err)
		}
	}

	sessionID, err := service.newID()
	if err != nil {
		return domain.Session{}, err
	}
	session := domain.Session{
		ID:         sessionID,
		FamilyID:   request.FamilyID,
		DeviceID:   request.DeviceID,
		ChildID:    childID,
		GuardianID: request.GuardianID,
		State:      domain.StateRequested,
		StartedAt:  now,
		// The deadline is fixed at creation so the device and the relay enforce
		// the same instant without asking the platform again.
		ExpiresAt: now.Add(domain.MaxViewDuration),
	}
	if err := service.store.Create(ctx, session); err != nil {
		if errors.Is(err, StoreSentinelBusy) {
			return domain.Session{}, ErrBusy
		}
		return domain.Session{}, err
	}

	service.logger.Info("care view requested",
		"session_id", session.ID, "family_id", session.FamilyID,
		"device_id", session.DeviceID, "guardian_id", session.GuardianID)
	if service.notifier != nil {
		if notifyErr := service.notifier.NotifyCareViewStart(ctx, session); notifyErr != nil {
			// The device never learned about the view, so nothing can stream.
			// Close the session instead of leaving the device holding a slot.
			closed, closeErr := service.transitionForGuardian(ctx, session.FamilyID, session.ID,
				func(candidate *domain.Session, at time.Time) error {
					return candidate.Fail(at, "notify_failed")
				})
			if closeErr != nil {
				service.logger.Warn("care view cleanup failed",
					"session_id", session.ID, "error", closeErr)
				return domain.Session{}, notifyErr
			}
			service.logger.Warn("care view start notification failed",
				"session_id", session.ID, "error", notifyErr)
			return closed, notifyErr
		}
	}
	return session, nil
}

// Accept moves a requested session into streaming after the device confirms its
// indication is on.
func (service *Service) Accept(ctx context.Context, deviceID, sessionID string) (domain.Session, error) {
	return service.transitionForDevice(ctx, deviceID, sessionID,
		func(session *domain.Session, now time.Time) error {
			return session.Accept(now)
		})
}

// Decline records that the device or child refused the view.
func (service *Service) Decline(ctx context.Context, deviceID, sessionID, reason string) (domain.Session, error) {
	return service.transitionForDevice(ctx, deviceID, sessionID,
		func(session *domain.Session, now time.Time) error {
			return session.Decline(now, normaliseDeviceReason(reason, "declined"))
		})
}

// End closes a session from the guardian side.
func (service *Service) End(ctx context.Context, familyID, sessionID, reason string) (domain.Session, error) {
	return service.transitionForGuardian(ctx, familyID, sessionID,
		func(session *domain.Session, now time.Time) error {
			return session.End(now, reason)
		})
}

// EndForDevice closes a session reported by the device, for example when the
// child pressed the button to stop being watched.
func (service *Service) EndForDevice(ctx context.Context, deviceID, sessionID, reason string) (domain.Session, error) {
	return service.transitionForDevice(ctx, deviceID, sessionID,
		func(session *domain.Session, now time.Time) error {
			reason = normaliseDeviceReason(reason, "child_closed")
			if reason == "declined" {
				return session.Decline(now, reason)
			}
			return session.End(now, reason)
		})
}

// Get returns one session to its own family.
func (service *Service) Get(ctx context.Context, familyID, sessionID string) (domain.Session, error) {
	session, err := service.store.Get(ctx, familyID, sessionID)
	if err != nil {
		if errors.Is(err, StoreSentinelNotFound) {
			return domain.Session{}, ErrSessionNotFound
		}
		return domain.Session{}, err
	}
	return session, nil
}

// GetForDevice returns one session to the device it addresses.
func (service *Service) GetForDevice(ctx context.Context, deviceID, sessionID string) (domain.Session, error) {
	session, err := service.store.GetForDevice(ctx, deviceID, sessionID)
	if err != nil {
		if errors.Is(err, StoreSentinelNotFound) {
			return domain.Session{}, ErrSessionNotFound
		}
		return domain.Session{}, err
	}
	return session, nil
}

// History returns a family's past sessions.
func (service *Service) History(ctx context.Context, familyID string, limit int) ([]domain.Session, error) {
	if limit <= 0 {
		limit = domain.DefaultHistoryLimit
	}
	if limit > domain.MaxHistoryLimit {
		limit = domain.MaxHistoryLimit
	}
	return service.store.History(ctx, familyID, limit)
}

// ExpireDue closes every session past its deadline and tells the device to stop.
//
// This is the backstop for a guardian who closed the app without stopping: the
// device is instructed to stop capturing even though no request arrives.
func (service *Service) ExpireDue(ctx context.Context) (int, error) {
	now := service.now()
	sessions, err := service.store.ListOpen(ctx)
	if err != nil {
		return 0, err
	}
	closed := 0
	for _, session := range sessions {
		// A requested session never started a camera, so it is abandoned after the
		// short request timeout rather than waiting for the full view deadline.
		deadline := session.ExpiresAt
		if session.State == domain.StateRequested {
			deadline = session.StartedAt.Add(domain.RequestTimeout)
		}
		if now.Before(deadline) {
			continue
		}
		previous := session
		if err := session.Expire(now); err != nil {
			continue
		}
		if err := service.store.Transition(ctx, session, previous.State); err != nil {
			service.logger.Warn("care view expiry transition failed",
				"session_id", session.ID, "error", err)
			continue
		}
		closed++
		service.logger.Info("care view closed by deadline",
			"session_id", session.ID, "state", session.State, "reason", session.FinishReason)
		service.notifyStop(ctx, session)
	}
	return closed, nil
}

// Metrics reports operational counters for the feature center.
func (service *Service) Metrics(ctx context.Context) (Metrics, error) {
	open, err := service.store.CountOpen(ctx)
	if err != nil {
		return Metrics{}, err
	}
	return Metrics{OpenSessions: open}, nil
}

// Metrics are read-only counters exposed to operators.
type Metrics struct {
	OpenSessions int
}

// transitionForGuardian applies a transition owned by the family.
func (service *Service) transitionForGuardian(
	ctx context.Context,
	familyID, sessionID string,
	apply func(*domain.Session, time.Time) error,
) (domain.Session, error) {
	session, err := service.store.Get(ctx, familyID, sessionID)
	if err != nil {
		if errors.Is(err, StoreSentinelNotFound) {
			return domain.Session{}, ErrSessionNotFound
		}
		return domain.Session{}, err
	}
	return service.apply(ctx, session, apply)
}

// transitionForDevice applies a transition addressed to the device.
func (service *Service) transitionForDevice(
	ctx context.Context,
	deviceID, sessionID string,
	apply func(*domain.Session, time.Time) error,
) (domain.Session, error) {
	session, err := service.store.GetForDevice(ctx, deviceID, sessionID)
	if err != nil {
		if errors.Is(err, StoreSentinelNotFound) {
			return domain.Session{}, ErrSessionNotFound
		}
		return domain.Session{}, err
	}
	return service.apply(ctx, session, apply)
}

// apply runs one transition and persists it against the state it read.
func (service *Service) apply(
	ctx context.Context,
	session domain.Session,
	apply func(*domain.Session, time.Time) error,
) (domain.Session, error) {
	previous := session
	now := service.now()
	if err := apply(&session, now); err != nil {
		return domain.Session{}, mapTransitionError(err)
	}
	if err := service.store.Transition(ctx, session, previous.State); err != nil {
		if errors.Is(err, StoreSentinelBusy) {
			return domain.Session{}, ErrConflict
		}
		return domain.Session{}, err
	}
	service.logger.Info("care view session changed",
		"session_id", session.ID, "state", session.State, "reason", session.FinishReason)
	// Any terminal state must also stop the device, otherwise a camera can keep
	// capturing after the guardian's window closed.
	if session.State.IsTerminal() && previous.State == domain.StateStreaming {
		service.notifyStop(ctx, session)
	}
	return session, nil
}

// notifyStop tells the device to stop capturing.
func (service *Service) notifyStop(ctx context.Context, session domain.Session) {
	if service.notifier == nil {
		return
	}
	if err := service.notifier.NotifyCareViewStop(ctx, session); err != nil {
		// The device also enforces its own deadline, so a failed stop is not a
		// runaway camera; it is worth an operator's attention, not a failure.
		service.logger.Warn("care view stop notification failed",
			"session_id", session.ID, "device_id", session.DeviceID, "error", err)
	}
}

// mapTransitionError translates a domain error into a service error.
func mapTransitionError(err error) error {
	switch {
	case errors.Is(err, domain.ErrFinished):
		return ErrConflict
	case errors.Is(err, domain.ErrNotRequested), errors.Is(err, domain.ErrNotStreaming):
		return ErrConflict
	default:
		return err
	}
}

// normaliseDeviceReason keeps a device-supplied reason to a safe token.
func normaliseDeviceReason(reason, fallback string) string {
	switch reason {
	case "child_closed", "camera_unavailable", "expired", "busy", "transport_closed", "declined":
		return reason
	case "":
		return fallback
	default:
		return fallback
	}
}

// newSessionID returns a random session identifier.
func newSessionID() (string, error) {
	buffer := make([]byte, 16)
	if _, err := rand.Read(buffer); err != nil {
		return "", fmt.Errorf("generate care view session id: %w", err)
	}
	// Shape the bytes as a UUIDv4 so the column type and the client agree.
	buffer[6] = (buffer[6] & 0x0f) | 0x40
	buffer[8] = (buffer[8] & 0x3f) | 0x80
	encoded := hex.EncodeToString(buffer)
	return encoded[0:8] + "-" + encoded[8:12] + "-" + encoded[12:16] + "-" +
		encoded[16:20] + "-" + encoded[20:32], nil
}
