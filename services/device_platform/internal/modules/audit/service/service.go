// Package service validates device diagnostics and exposes bounded queries.
package service

import (
	"context"
	"errors"
	"regexp"
	"strings"
	"time"

	"github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/audit/domain"
	"github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/audit/repository"
)

var (
	diagnosticDevicePattern = regexp.MustCompile(
		`^[A-Za-z0-9][A-Za-z0-9._:-]{7,127}$`,
	)
	eventIDPattern         = regexp.MustCompile(`^[a-z][a-z0-9_]{3,63}$`)
	symbolicTextPattern    = regexp.MustCompile(`^[A-Za-z0-9_.:-]{1,64}$`)
	firmwareVersionPattern = regexp.MustCompile(`^[0-9A-Za-z][0-9A-Za-z.+-]{0,63}$`)
)

// Service validates diagnostic payloads and serves bounded query results.
type Service struct {
	repository repository.Repository
}

// Options contains audit service dependencies.
type Options struct {
	Repository repository.Repository
}

// New creates the diagnostic service.
func New(options Options) (*Service, error) {
	if options.Repository == nil {
		return nil, errors.New("audit repository is required")
	}
	return &Service{repository: options.Repository}, nil
}

// Record validates the optional diagnostics carried by one heartbeat. A nil
// or empty payload is a successful no-op so older firmware stays compatible.
func (s *Service) Record(
	ctx context.Context,
	deviceID string,
	reportedAt time.Time,
	diagnostics *domain.Diagnostics,
) error {
	normalized, err := s.Validate(deviceID, diagnostics)
	if err != nil || normalized == nil {
		return err
	}
	return s.repository.SaveDiagnostics(
		ctx,
		strings.TrimSpace(deviceID),
		reportedAt.UTC(),
		normalized,
	)
}

// Validate normalizes one optional diagnostic payload without persisting it.
// Transport calls this before accepting the runtime heartbeat so an invalid
// diagnostic extension cannot leave a partially processed heartbeat behind.
func (s *Service) Validate(
	deviceID string,
	diagnostics *domain.Diagnostics,
) (*domain.Diagnostics, error) {
	if diagnostics == nil {
		return nil, nil
	}
	deviceID = strings.TrimSpace(deviceID)
	if !diagnosticDevicePattern.MatchString(deviceID) {
		return nil, domain.ErrInvalidDiagnostics
	}
	if diagnostics.SchemaVersion != "1.0.0" ||
		len(diagnostics.BootEvents) > 8 ||
		len(diagnostics.RecoveryEvents) > 16 ||
		diagnostics.DroppedBootEvents > 1_000_000 {
		return nil, domain.ErrInvalidDiagnostics
	}

	normalized := &domain.Diagnostics{
		SchemaVersion:     diagnostics.SchemaVersion,
		NewestSequence:    diagnostics.NewestSequence,
		DroppedBootEvents: diagnostics.DroppedBootEvents,
		BootEvents:        make([]domain.BootEvent, 0, len(diagnostics.BootEvents)),
		RecoveryEvents:    make([]domain.RecoveryEvent, 0, len(diagnostics.RecoveryEvents)),
	}

	for index := range diagnostics.BootEvents {
		event := diagnostics.BootEvents[index]
		if !validEvent(
			event.EventID,
			event.Sequence,
			diagnostics.NewestSequence,
		) || (event.EventType != "" && event.EventType != domain.EventTypeBoot) ||
			event.UptimeMS > 4_294_967_295 ||
			event.BootCount == 0 ||
			!symbolicTextPattern.MatchString(event.ResetReason) ||
			!firmwareVersionPattern.MatchString(event.FirmwareVersion) {
			return nil, domain.ErrInvalidDiagnostics
		}
		event.EventType = domain.EventTypeBoot
		event.FirmwareVersion = strings.TrimSpace(event.FirmwareVersion)
		normalized.BootEvents = append(normalized.BootEvents, event)
	}

	for index := range diagnostics.RecoveryEvents {
		event := diagnostics.RecoveryEvents[index]
		if !validEvent(
			event.EventID,
			event.Sequence,
			diagnostics.NewestSequence,
		) || (event.EventType != "" && event.EventType != domain.EventTypeRecovered) ||
			!symbolicTextPattern.MatchString(event.ModuleName) ||
			!firmwareVersionPattern.MatchString(event.FirmwareVersion) {
			return nil, domain.ErrInvalidDiagnostics
		}
		event.EventType = domain.EventTypeRecovered
		event.ModuleName = strings.TrimSpace(event.ModuleName)
		event.FirmwareVersion = strings.TrimSpace(event.FirmwareVersion)
		normalized.RecoveryEvents = append(normalized.RecoveryEvents, event)
	}

	if diagnostics.LatestFailure != nil {
		failure := *diagnostics.LatestFailure
		if !validEvent(
			failure.EventID,
			failure.Sequence,
			diagnostics.NewestSequence,
		) || (failure.EventType != "" && failure.EventType != domain.EventTypeFailure) ||
			!symbolicTextPattern.MatchString(failure.ModuleName) ||
			!symbolicTextPattern.MatchString(failure.ErrorCode) ||
			failure.FailureCount > 4_294_967_295 ||
			!firmwareVersionPattern.MatchString(failure.FirmwareVersion) {
			return nil, domain.ErrInvalidDiagnostics
		}
		failure.EventType = domain.EventTypeFailure
		failure.ModuleName = strings.TrimSpace(failure.ModuleName)
		failure.ErrorCode = strings.TrimSpace(failure.ErrorCode)
		failure.FirmwareVersion = strings.TrimSpace(failure.FirmwareVersion)
		normalized.LatestFailure = &failure
	}

	if normalized.NewestSequence == 0 &&
		len(normalized.BootEvents) == 0 &&
		len(normalized.RecoveryEvents) == 0 &&
		normalized.LatestFailure == nil {
		return nil, nil
	}
	return normalized, nil
}

// Get returns the newest diagnostic history for one device.
func (s *Service) Get(
	ctx context.Context,
	deviceID string,
	limit int,
) (*domain.Snapshot, error) {
	deviceID = strings.TrimSpace(deviceID)
	if !diagnosticDevicePattern.MatchString(deviceID) {
		return nil, domain.ErrEventNotFound
	}
	return s.repository.GetDiagnostics(ctx, deviceID, limit)
}

func validEvent(eventID string, sequence uint64, newestSequence uint64) bool {
	return eventIDPattern.MatchString(strings.TrimSpace(eventID)) &&
		sequence > 0 &&
		sequence <= newestSequence
}
