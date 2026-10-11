package service

import (
	"context"
	"errors"
	"testing"

	"github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/content_offline_policy/domain"
)

type fakeRepository struct {
	policy domain.Policy
	saved  []domain.Input
	acks   map[string]int
}

func (r *fakeRepository) Current(context.Context) (domain.Policy, error) {
	return r.policy, nil
}

func (r *fakeRepository) Save(
	_ context.Context,
	input domain.Input,
) (domain.Policy, error) {
	r.saved = append(r.saved, input)
	r.policy = domain.Policy{
		OfflineEnabled:       input.OfflineEnabled,
		MaxCachedPackages:    input.MaxCachedPackages,
		MaxCacheBytes:        input.MaxCacheBytes,
		CacheTTLHours:        input.CacheTTLHours,
		DownloadOverWifiOnly: input.DownloadOverWifiOnly,
		Revision:             len(r.saved),
	}
	return r.policy, nil
}

func (r *fakeRepository) RecordAcknowledgement(
	_ context.Context,
	_ string,
	deviceID string,
	revision int,
) error {
	if r.acks == nil {
		r.acks = map[string]int{}
	}
	r.acks[deviceID] = revision
	return nil
}

func (r *fakeRepository) Acknowledgements(
	_ context.Context,
	_ string,
) ([]domain.Acknowledgement, error) {
	acks := make([]domain.Acknowledgement, 0, len(r.acks))
	for deviceID, revision := range r.acks {
		acks = append(acks, domain.Acknowledgement{
			DeviceID: deviceID,
			Revision: revision,
		})
	}
	return acks, nil
}

func TestCurrentReturnsRepositoryPolicy(t *testing.T) {
	repository := &fakeRepository{policy: domain.Default()}
	service := New(Options{Repository: repository})

	policy, err := service.Current(context.Background())
	if err != nil {
		t.Fatalf("Current() returned unexpected error: %v", err)
	}
	if policy.MaxCachedPackages != domain.DefaultCachedPackages {
		t.Fatalf("expected default package cap, got %d", policy.MaxCachedPackages)
	}
}

func TestCurrentWithoutRepositoryReturnsDefault(t *testing.T) {
	service := New(Options{})

	policy, err := service.Current(context.Background())
	if err != nil {
		t.Fatalf("Current() returned unexpected error: %v", err)
	}
	if policy.Revision != 0 || !policy.OfflineEnabled {
		t.Fatalf("unexpected fallback policy: %+v", policy)
	}
}

func TestPublishRejectsOutOfRangePolicy(t *testing.T) {
	service := New(Options{Repository: &fakeRepository{}})

	cases := []domain.Input{
		{
			MaxCachedPackages: 0,
			MaxCacheBytes:     domain.DefaultCacheBytes,
			CacheTTLHours:     domain.DefaultCacheTTLHours,
		},
		{
			MaxCachedPackages: domain.MaxCachedPackages + 1,
			MaxCacheBytes:     domain.DefaultCacheBytes,
			CacheTTLHours:     domain.DefaultCacheTTLHours,
		},
		{
			MaxCachedPackages: domain.DefaultCachedPackages,
			MaxCacheBytes:     domain.MinCacheBytes - 1,
			CacheTTLHours:     domain.DefaultCacheTTLHours,
		},
		{
			MaxCachedPackages: domain.DefaultCachedPackages,
			MaxCacheBytes:     domain.MaxCacheBytes + 1,
			CacheTTLHours:     domain.DefaultCacheTTLHours,
		},
		{
			MaxCachedPackages: domain.DefaultCachedPackages,
			MaxCacheBytes:     domain.DefaultCacheBytes,
			CacheTTLHours:     0,
		},
		{
			MaxCachedPackages: domain.DefaultCachedPackages,
			MaxCacheBytes:     domain.DefaultCacheBytes,
			CacheTTLHours:     domain.MaxCacheTTLHours + 1,
		},
	}
	for _, input := range cases {
		if _, err := service.Publish(context.Background(), input); !errors.Is(
			err,
			domain.ErrInvalidPolicy,
		) {
			t.Fatalf("expected ErrInvalidPolicy for %+v, got %v", input, err)
		}
	}
}

func TestPublishStoresValidPolicyAndBumpsRevision(t *testing.T) {
	repository := &fakeRepository{}
	service := New(Options{Repository: repository})
	input := domain.Input{
		OfflineEnabled:       true,
		MaxCachedPackages:    40,
		MaxCacheBytes:        1024 * 1024 * 1024,
		CacheTTLHours:        72,
		DownloadOverWifiOnly: true,
	}

	policy, err := service.Publish(context.Background(), input)
	if err != nil {
		t.Fatalf("Publish() returned unexpected error: %v", err)
	}
	if policy.Revision != 1 {
		t.Fatalf("expected revision 1, got %d", policy.Revision)
	}
	if policy.MaxCachedPackages != 40 || policy.CacheTTLHours != 72 {
		t.Fatalf("policy not stored verbatim: %+v", policy)
	}

	policy, err = service.Publish(context.Background(), input)
	if err != nil {
		t.Fatalf("second Publish() returned unexpected error: %v", err)
	}
	if policy.Revision != 2 {
		t.Fatalf("expected revision to bump to 2, got %d", policy.Revision)
	}
}

func TestPublishWithoutRepositoryReturnsDefault(t *testing.T) {
	service := New(Options{})

	policy, err := service.Publish(context.Background(), domain.Input{
		MaxCachedPackages: domain.DefaultCachedPackages,
		MaxCacheBytes:     domain.DefaultCacheBytes,
		CacheTTLHours:     domain.DefaultCacheTTLHours,
	})
	if err != nil {
		t.Fatalf("Publish() returned unexpected error: %v", err)
	}
	if policy.Revision != 0 {
		t.Fatalf("expected fallback revision 0, got %d", policy.Revision)
	}
}

func TestAcknowledgeRejectsNegativeRevision(t *testing.T) {
	service := New(Options{Repository: &fakeRepository{}})

	if err := service.Acknowledge(
		context.Background(),
		"family-1",
		"device-1",
		-1,
	); !errors.Is(err, domain.ErrInvalidAcceptedRevision) {
		t.Fatalf("expected ErrInvalidAcceptedRevision, got %v", err)
	}
}

func TestAcknowledgementsMarkDevicesBehindThePolicy(t *testing.T) {
	repository := &fakeRepository{
		policy: domain.Policy{Revision: 5},
	}
	service := New(Options{Repository: repository})
	if err := service.Acknowledge(
		context.Background(),
		"family-1",
		"device-current",
		5,
	); err != nil {
		t.Fatalf("Acknowledge() returned unexpected error: %v", err)
	}
	if err := service.Acknowledge(
		context.Background(),
		"family-1",
		"device-behind",
		2,
	); err != nil {
		t.Fatalf("Acknowledge() returned unexpected error: %v", err)
	}

	acks, err := service.Acknowledgements(context.Background(), "family-1")
	if err != nil {
		t.Fatalf("Acknowledgements() returned unexpected error: %v", err)
	}
	if len(acks) != 2 {
		t.Fatalf("expected two acknowledgements, got %d", len(acks))
	}
	upToDate := map[string]bool{}
	for _, ack := range acks {
		upToDate[ack.DeviceID] = ack.UpToDate
	}
	if !upToDate["device-current"] {
		t.Fatal("device on the published revision should be up to date")
	}
	if upToDate["device-behind"] {
		t.Fatal("device behind the published revision must not be up to date")
	}
}

func TestAcknowledgementsWithoutRepositoryIsEmpty(t *testing.T) {
	service := New(Options{})

	acks, err := service.Acknowledgements(context.Background(), "family-1")
	if err != nil {
		t.Fatalf("Acknowledgements() returned unexpected error: %v", err)
	}
	if len(acks) != 0 {
		t.Fatalf("expected no acknowledgements, got %d", len(acks))
	}
}
