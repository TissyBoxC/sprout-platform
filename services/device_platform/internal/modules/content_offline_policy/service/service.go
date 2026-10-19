// Package service contains the content offline policy use cases.
//
// The policy is platform-wide and singleton: an operator reads and publishes
// one version, and every bound device reads the same policy so its cache
// budget is consistent across the fleet. The service validates the payload
// before it touches the store, so a bad revision never replaces a good one.
package service

import (
	"context"

	"github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/content_offline_policy/domain"
)

// Repository is the persistence surface the service depends on.
type Repository interface {
	Current(ctx context.Context) (domain.Policy, error)
	Save(ctx context.Context, input domain.Input) (domain.Policy, error)
	RecordAcknowledgement(
		ctx context.Context,
		familyID string,
		deviceID string,
		revision int,
	) error
	Acknowledgements(ctx context.Context, familyID string) ([]domain.Acknowledgement, error)
}

// Service owns offline policy reads and publishes.
type Service struct {
	repository      Repository
	readinessReader ReadinessReader
}

// Options contains offline policy service dependencies.
type Options struct {
	Repository      Repository
	ReadinessReader ReadinessReader
}

// New creates the offline policy service.
func New(options Options) *Service {
	readinessReader := options.ReadinessReader
	if readinessReader == nil {
		if reader, ok := options.Repository.(ReadinessReader); ok {
			readinessReader = reader
		}
	}
	return &Service{
		repository:      options.Repository,
		readinessReader: readinessReader,
	}
}

// Current returns the published policy or the default when none is set.
func (s *Service) Current(ctx context.Context) (domain.Policy, error) {
	if s.repository == nil {
		return domain.Default(), nil
	}
	return s.repository.Current(ctx)
}

// Publish validates and stores a new policy revision.
func (s *Service) Publish(
	ctx context.Context,
	input domain.Input,
) (domain.Policy, error) {
	if s.repository == nil {
		return domain.Default(), nil
	}
	normalized, err := domain.ValidateInput(input)
	if err != nil {
		return domain.Policy{}, err
	}
	return s.repository.Save(ctx, normalized)
}

// Acknowledge records the policy revision one device currently applies.
func (s *Service) Acknowledge(
	ctx context.Context,
	familyID string,
	deviceID string,
	revision int,
) error {
	if s.repository == nil {
		return nil
	}
	if err := domain.ValidateAcceptedRevision(revision); err != nil {
		return err
	}
	return s.repository.RecordAcknowledgement(ctx, familyID, deviceID, revision)
}

// Acknowledgements returns each device's applied revision, annotated with
// whether it is already on the newest published policy.
func (s *Service) Acknowledgements(
	ctx context.Context,
	familyID string,
) ([]domain.Acknowledgement, error) {
	if s.repository == nil {
		return []domain.Acknowledgement{}, nil
	}
	policy, err := s.repository.Current(ctx)
	if err != nil {
		return nil, err
	}
	acks, err := s.repository.Acknowledgements(ctx, familyID)
	if err != nil {
		return nil, err
	}
	for index := range acks {
		acks[index].UpToDate = acks[index].Revision >= policy.Revision
	}
	return acks, nil
}
