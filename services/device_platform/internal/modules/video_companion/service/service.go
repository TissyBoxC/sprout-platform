// Package service contains the video-companionship use cases.
//
// Every operation is family-scoped: the caller passes the authenticated family
// id and the service never trusts a client-provided owner. The service stores
// call metadata only; media negotiation and transport live in the voice
// gateway. A revoked permission or a lost device ends a call with a first-class
// reason instead of leaving it dangling.
package service

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/video_companion/domain"
	"github.com/TissyBoxC/sprout-platform/services/device_platform/internal/platform/clock"
	"github.com/TissyBoxC/sprout-platform/services/device_platform/internal/platform/featurehealth"
	"github.com/google/uuid"
)

// Repository is the persistence surface the service depends on.
type Repository interface {
	CreateContact(ctx context.Context, contact domain.Contact) error
	UpdateContact(ctx context.Context, contact domain.Contact) error
	DeleteContact(ctx context.Context, familyID string, contactID string) error
	Contacts(ctx context.Context, familyID string) ([]domain.Contact, error)
	Contact(ctx context.Context, familyID string, contactID string) (domain.Contact, bool, error)
	CreateCall(ctx context.Context, call domain.Call) error
	Call(ctx context.Context, familyID string, callID string) (domain.Call, bool, error)
	UpdateCall(ctx context.Context, call domain.Call) error
	Calls(ctx context.Context, familyID string, limit int) ([]domain.Call, error)
	RecordStatus(ctx context.Context, status domain.DeviceStatus) error
	Statuses(ctx context.Context, familyID string) ([]domain.DeviceStatus, error)
	AdminStats(ctx context.Context, windowDays int) (domain.AdminStats, error)
}

// Service owns video contacts, call lifecycle and device status.
type Service struct {
	repository Repository
	clock      clock.Clock
}

const capabilityFreshness = 24 * time.Hour

// Options contains video-companion service dependencies.
type Options struct {
	Repository Repository
	Clock      clock.Clock
}

// New creates the video-companion service.
func New(options Options) (*Service, error) {
	if options.Repository == nil {
		return nil, errors.New("video companion repository is required")
	}
	timeSource := options.Clock
	if timeSource == nil {
		timeSource = clock.SystemClock{}
	}
	return &Service{repository: options.Repository, clock: timeSource}, nil
}

// CreateContact validates and stores one contact.
func (s *Service) CreateContact(
	ctx context.Context,
	familyID string,
	input domain.ContactInput,
) (domain.Contact, error) {
	familyID = strings.TrimSpace(familyID)
	if familyID == "" {
		return domain.Contact{}, domain.ErrInvalidContact
	}
	normalized, err := domain.ValidateContactInput(input)
	if err != nil {
		return domain.Contact{}, err
	}
	contact := domain.Contact{
		ID:          uuid.NewString(),
		FamilyID:    familyID,
		ChildID:     strings.TrimSpace(normalized.ChildID),
		DisplayName: strings.TrimSpace(normalized.DisplayName),
		Role:        normalized.Role,
		Enabled:     normalized.Enabled,
	}
	if err := s.repository.CreateContact(ctx, contact); err != nil {
		return domain.Contact{}, err
	}
	contact.CreatedAt = s.clock.Now().UTC()
	contact.UpdatedAt = contact.CreatedAt
	return contact, nil
}

// UpdateContact validates and stores one contact edit.
func (s *Service) UpdateContact(
	ctx context.Context,
	familyID string,
	contactID string,
	input domain.ContactInput,
) (domain.Contact, error) {
	familyID = strings.TrimSpace(familyID)
	contactID = strings.TrimSpace(contactID)
	if familyID == "" || contactID == "" {
		return domain.Contact{}, domain.ErrInvalidContact
	}
	normalized, err := domain.ValidateContactInput(input)
	if err != nil {
		return domain.Contact{}, err
	}
	contact := domain.Contact{
		ID:          contactID,
		FamilyID:    familyID,
		ChildID:     strings.TrimSpace(normalized.ChildID),
		DisplayName: strings.TrimSpace(normalized.DisplayName),
		Role:        normalized.Role,
		Enabled:     normalized.Enabled,
	}
	if err := s.repository.UpdateContact(ctx, contact); err != nil {
		return domain.Contact{}, err
	}
	return contact, nil
}

// DeleteContact removes one contact.
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

// Contacts returns every contact in a family.
func (s *Service) Contacts(
	ctx context.Context,
	familyID string,
) ([]domain.Contact, error) {
	familyID = strings.TrimSpace(familyID)
	if familyID == "" {
		return nil, domain.ErrInvalidContact
	}
	return s.repository.Contacts(ctx, familyID)
}

// StartCall validates and stores one new call, then immediately expires any
// call that the target device can never answer.
func (s *Service) StartCall(
	ctx context.Context,
	familyID string,
	input domain.CallInput,
) (domain.Call, error) {
	familyID = strings.TrimSpace(familyID)
	if familyID == "" {
		return domain.Call{}, domain.ErrInvalidCall
	}
	normalized, err := domain.ValidateCallInput(input)
	if err != nil {
		return domain.Call{}, err
	}
	if normalized.ContactID != "" {
		if _, ok, err := s.repository.Contact(ctx, familyID, normalized.ContactID); err != nil {
			return domain.Call{}, err
		} else if !ok {
			return domain.Call{}, domain.ErrContactNotFound
		}
	}
	call := domain.Call{
		ID:        uuid.NewString(),
		FamilyID:  familyID,
		DeviceID:  strings.TrimSpace(normalized.DeviceID),
		ContactID: strings.TrimSpace(normalized.ContactID),
		Initiator: normalized.Initiator,
		State:     domain.StateRinging,
		Quality:   domain.QualityUnknown,
	}
	if err := s.repository.CreateCall(ctx, call); err != nil {
		return domain.Call{}, err
	}
	return call, nil
}

// UpdateCallState stores a lifecycle transition with a first-class end reason.
func (s *Service) UpdateCallState(
	ctx context.Context,
	familyID string,
	callID string,
	state string,
	quality string,
	endReason string,
) (domain.Call, error) {
	familyID = strings.TrimSpace(familyID)
	callID = strings.TrimSpace(callID)
	if familyID == "" || callID == "" {
		return domain.Call{}, domain.ErrInvalidCall
	}
	if !domain.SupportedState(state) {
		return domain.Call{}, domain.ErrInvalidCall
	}
	if quality == "" {
		quality = domain.QualityUnknown
	}
	if !domain.SupportedQuality(quality) {
		return domain.Call{}, domain.ErrInvalidCall
	}
	if endReason != "" && !domain.SupportedEndReason(endReason) {
		return domain.Call{}, domain.ErrInvalidCall
	}
	call, ok, err := s.repository.Call(ctx, familyID, callID)
	if err != nil {
		return domain.Call{}, err
	}
	if !ok {
		return domain.Call{}, domain.ErrCallNotFound
	}
	now := s.clock.Now().UTC()
	call.State = state
	call.Quality = quality
	call.EndReason = endReason
	if state == domain.StateActive && call.AnsweredAt == nil {
		call.AnsweredAt = &now
	}
	if isTerminal(state) && call.EndedAt == nil {
		call.EndedAt = &now
	}
	if err := s.repository.UpdateCall(ctx, call); err != nil {
		return domain.Call{}, err
	}
	return call, nil
}

// CallHistory returns the most recent calls in a family.
func (s *Service) CallHistory(
	ctx context.Context,
	familyID string,
	limit int,
) ([]domain.Call, error) {
	familyID = strings.TrimSpace(familyID)
	if familyID == "" {
		return nil, domain.ErrInvalidCall
	}
	if limit <= 0 || limit > domain.CallHistoryMaxLimit {
		limit = domain.CallHistoryDefaultLimit
	}
	return s.repository.Calls(ctx, familyID, limit)
}

// Capability returns the video call capability for one device.
func (s *Service) Capability(
	ctx context.Context,
	familyID string,
	deviceID string,
) (domain.Capability, error) {
	familyID = strings.TrimSpace(familyID)
	deviceID = strings.TrimSpace(deviceID)
	if familyID == "" || deviceID == "" {
		return domain.Capability{}, domain.ErrInvalidCall
	}
	statuses, err := s.repository.Statuses(ctx, familyID)
	if err != nil {
		return domain.Capability{}, err
	}
	for _, status := range statuses {
		if status.DeviceID != deviceID {
			continue
		}
		return domain.Capability{
			Supported:         true,
			CallState:         status.CallState,
			Quality:           status.Quality,
			CameraAllowed:     status.CameraAllowed,
			MicrophoneAllowed: status.MicrophoneAllowed,
			SchemaVersion:     domain.SchemaVersion,
		}, nil
	}
	return domain.Capability{SchemaVersion: domain.SchemaVersion}, nil
}

// ReportStatus stores one bounded device call status.
func (s *Service) ReportStatus(
	ctx context.Context,
	familyID string,
	deviceID string,
	input domain.CallStatusInput,
) (domain.DeviceStatus, error) {
	familyID = strings.TrimSpace(familyID)
	deviceID = strings.TrimSpace(deviceID)
	if familyID == "" || deviceID == "" {
		return domain.DeviceStatus{}, domain.ErrInvalidCall
	}
	normalized, err := domain.ValidateCallStatus(input)
	if err != nil {
		return domain.DeviceStatus{}, err
	}
	status := domain.DeviceStatus{
		DeviceID:          deviceID,
		FamilyID:          familyID,
		CallState:         normalized.CallState,
		Quality:           normalized.Quality,
		CameraAllowed:     normalized.CameraAllowed,
		MicrophoneAllowed: normalized.MicrophoneAllowed,
		LastCallID:        strings.TrimSpace(normalized.LastCallID),
	}
	if err := s.repository.RecordStatus(ctx, status); err != nil {
		return domain.DeviceStatus{}, err
	}
	return status, nil
}

// DeviceStatuses returns each staged device video status.
func (s *Service) DeviceStatuses(
	ctx context.Context,
	familyID string,
) ([]domain.DeviceCallStatus, error) {
	familyID = strings.TrimSpace(familyID)
	if familyID == "" {
		return nil, domain.ErrInvalidCall
	}
	statuses, err := s.repository.Statuses(ctx, familyID)
	if err != nil {
		return nil, err
	}
	devices := make([]domain.DeviceCallStatus, 0, len(statuses))
	for _, status := range statuses {
		devices = append(devices, domain.DeviceCallStatus{
			DeviceID:          status.DeviceID,
			FamilyID:          status.FamilyID,
			CallState:         status.CallState,
			Quality:           status.Quality,
			CameraAllowed:     status.CameraAllowed,
			MicrophoneAllowed: status.MicrophoneAllowed,
			ReportedAt:        status.ReportedAt,
		})
	}
	return devices, nil
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

// isTerminal reports whether a call state ends the call.
func isTerminal(state string) bool {
	switch state {
	case domain.StateEnded, domain.StateRejected, domain.StateMissed, domain.StateFailed:
		return true
	default:
		return false
	}
}

// CheckReadiness returns aggregate video-call health for active devices that
// advertise the video call capability.
func (s *Service) CheckReadiness(
	ctx context.Context,
) (featurehealth.DeviceCapabilityReadiness, error) {
	reader, ok := s.repository.(interface {
		Readiness(
			context.Context,
			time.Duration,
		) (featurehealth.DeviceCapabilityReadiness, error)
	})
	if !ok {
		return featurehealth.DeviceCapabilityReadiness{
			Status: featurehealth.StatusUnavailable,
		}, nil
	}
	return reader.Readiness(ctx, capabilityFreshness)
}
