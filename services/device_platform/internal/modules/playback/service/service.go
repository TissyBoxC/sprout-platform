// Package service contains on-demand playback request use cases.
//
// Every guardian operation is family-scoped: the caller passes the
// authenticated family id and the service never trusts a client-provided
// owner. Before a request is stored the service confirms the target device is
// bound to the family and the package is a published catalog item, so a
// guardian cannot queue playback on someone else's device or an unpublished
// package.
package service

import (
	"context"
	"errors"
	"strings"

	"github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/playback/domain"
	"github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/playback/repository"
	"github.com/TissyBoxC/sprout-platform/services/device_platform/internal/platform/clock"
	"github.com/google/uuid"
)

// DefaultRequestLimit and MaxRequestLimit bound a request read so a long-lived
// family cannot force an unbounded response.
const (
	DefaultRequestLimit = 50
	MaxRequestLimit     = 200
)

// DeviceOwnerReader confirms which devices belong to one family.
type DeviceOwnerReader interface {
	// FamilyDeviceIDs returns the device ids bound to the family.
	FamilyDeviceIDs(ctx context.Context, familyID string) ([]string, error)
}

// PackageReader confirms which content packages are deliverable.
type PackageReader interface {
	// IsPublished reports whether the package has a published version.
	IsPublished(ctx context.Context, packageID string) (bool, error)
}

// Service owns playback request validation and persistence.
type Service struct {
	repository      repository.Repository
	deviceReader    DeviceOwnerReader
	packageReader   PackageReader
	readinessReader ReadinessReader
	timeSource      clock.Clock
}

// Options contains playback-service dependencies.
type Options struct {
	Repository      repository.Repository
	DeviceReader    DeviceOwnerReader
	PackageReader   PackageReader
	ReadinessReader ReadinessReader
	Clock           clock.Clock
}

// New creates the playback service.
func New(options Options) (*Service, error) {
	if options.Repository == nil {
		return nil, errors.New("playback repository is required")
	}
	timeSource := options.Clock
	if timeSource == nil {
		timeSource = clock.SystemClock{}
	}
	readinessReader := options.ReadinessReader
	if readinessReader == nil {
		if reader, ok := options.Repository.(ReadinessReader); ok {
			readinessReader = reader
		}
	}
	return &Service{
		repository:      options.Repository,
		deviceReader:    options.DeviceReader,
		packageReader:   options.PackageReader,
		readinessReader: readinessReader,
		timeSource:      timeSource,
	}, nil
}

// Create validates and stores one guardian on-demand playback request.
func (s *Service) Create(
	ctx context.Context,
	familyID string,
	input domain.RequestInput,
) (*domain.Request, error) {
	normalized, err := domain.ValidateRequestInput(input)
	if err != nil {
		return nil, err
	}
	if err := s.assertDeviceOwned(ctx, familyID, normalized.DeviceID); err != nil {
		return nil, err
	}
	if err := s.assertPackagePublished(ctx, normalized.PackageID); err != nil {
		return nil, err
	}
	pending, err := s.repository.HasPendingRequest(
		ctx,
		familyID,
		normalized.DeviceID,
		normalized.PackageID,
	)
	if err != nil {
		return nil, err
	}
	if pending {
		return nil, domain.ErrDuplicateRequest
	}
	now := s.timeSource.Now().UTC()
	request := &domain.Request{
		ID:          uuid.NewString(),
		FamilyID:    familyID,
		DeviceID:    normalized.DeviceID,
		PackageID:   normalized.PackageID,
		Source:      domain.SourceGuardian,
		Status:      domain.StatusPending,
		Note:        normalized.Note,
		RequestedAt: now,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	if err := s.repository.CreateRequest(ctx, request); err != nil {
		return nil, err
	}
	return request, nil
}

// List returns a bounded, family-scoped page of requests.
func (s *Service) List(
	ctx context.Context,
	familyID string,
	query domain.RequestQuery,
) ([]domain.Request, error) {
	query.DeviceID = strings.TrimSpace(query.DeviceID)
	query.Status = strings.TrimSpace(query.Status)
	if query.Status != "" && !domain.IsStatus(query.Status) {
		return nil, domain.ErrInvalidRequestState
	}
	switch {
	case query.Limit <= 0:
		query.Limit = DefaultRequestLimit
	case query.Limit > MaxRequestLimit:
		query.Limit = MaxRequestLimit
	}
	return s.repository.ListRequests(ctx, familyID, query)
}

// Get returns one family-owned request.
func (s *Service) Get(
	ctx context.Context,
	familyID string,
	requestID string,
) (*domain.Request, error) {
	if strings.TrimSpace(requestID) == "" {
		return nil, domain.ErrInvalidRequest
	}
	return s.repository.GetRequest(ctx, familyID, requestID)
}

// Cancel moves a still-pending family request to cancelled.
func (s *Service) Cancel(
	ctx context.Context,
	familyID string,
	requestID string,
) (*domain.Request, error) {
	if strings.TrimSpace(requestID) == "" {
		return nil, domain.ErrInvalidRequest
	}
	return s.repository.CancelRequest(ctx, familyID, requestID)
}

// ListPendingForDevice returns the requests a bound device should act on,
// oldest first.
func (s *Service) ListPendingForDevice(
	ctx context.Context,
	deviceID string,
) ([]domain.Request, error) {
	if strings.TrimSpace(deviceID) == "" {
		return nil, domain.ErrInvalidDeviceID
	}
	return s.repository.ListPendingForDevice(ctx, deviceID)
}

// Acknowledge marks one device-owned request acknowledged.
func (s *Service) Acknowledge(
	ctx context.Context,
	deviceID string,
	requestID string,
) (*domain.Request, error) {
	if strings.TrimSpace(requestID) == "" {
		return nil, domain.ErrInvalidRequest
	}
	return s.repository.AcknowledgeRequest(ctx, deviceID, requestID)
}

// Complete records the terminal outcome for one device-owned request.
func (s *Service) Complete(
	ctx context.Context,
	deviceID string,
	requestID string,
	outcome string,
) (*domain.Request, error) {
	if strings.TrimSpace(requestID) == "" {
		return nil, domain.ErrInvalidRequest
	}
	normalized := strings.TrimSpace(outcome)
	if normalized == "" {
		normalized = domain.OutcomePlayed
	}
	if !domain.IsOutcome(normalized) {
		return nil, domain.ErrInvalidOutcome
	}
	return s.repository.CompleteRequest(ctx, deviceID, requestID, normalized)
}

func (s *Service) assertDeviceOwned(
	ctx context.Context,
	familyID string,
	deviceID string,
) error {
	if s.deviceReader == nil {
		return errors.New("playback device ownership reader is unavailable")
	}
	deviceIDs, err := s.deviceReader.FamilyDeviceIDs(ctx, familyID)
	if err != nil {
		return err
	}
	for _, candidate := range deviceIDs {
		if candidate == deviceID {
			return nil
		}
	}
	return domain.ErrInvalidDeviceID
}

func (s *Service) assertPackagePublished(
	ctx context.Context,
	packageID string,
) error {
	if s.packageReader == nil {
		return errors.New("playback package reader is unavailable")
	}
	published, err := s.packageReader.IsPublished(ctx, packageID)
	if err != nil {
		return err
	}
	if !published {
		return domain.ErrInvalidPackageID
	}
	return nil
}
