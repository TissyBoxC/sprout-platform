package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/screen/domain"
	"github.com/TissyBoxC/sprout-platform/services/device_platform/internal/platform/featurehealth"
)

// fakeRepository is an in-memory double so the service tests exercise the real
// validation and lifecycle without a database.
type fakeRepository struct {
	policy   domain.Policy
	statuses map[string]domain.Status
	entries  map[string]domain.DictionaryEntry
	playback map[string]domain.PlaybackPlan
	stats    domain.AdminStats
}

func newFakeRepository() *fakeRepository {
	return &fakeRepository{
		policy:   domain.Default(),
		statuses: map[string]domain.Status{},
		entries:  map[string]domain.DictionaryEntry{},
		playback: map[string]domain.PlaybackPlan{},
	}
}

func entryKey(familyID string, term string) string {
	return familyID + "|" + term
}

func (r *fakeRepository) Current(
	_ context.Context,
	_ string,
) (domain.Policy, error) {
	return r.policy, nil
}

func (r *fakeRepository) Save(
	_ context.Context,
	_ string,
	input domain.Input,
) (domain.Policy, error) {
	r.policy = domain.Policy{
		SchemaVersion:      domain.SchemaVersion,
		DefaultExpression:  input.DefaultExpression,
		AnimationEnabled:   input.AnimationEnabled,
		PointReadEnabled:   input.PointReadEnabled,
		VideoEnabled:       input.VideoEnabled,
		VideoMaxFPS:        input.VideoMaxFPS,
		VideoMaxBitrateKBS: input.VideoMaxBitrateKBS,
		AnimationFrameMS:   input.AnimationFrameMS,
		Revision:           r.policy.Revision + 1,
	}
	return r.policy, nil
}

func (r *fakeRepository) RecordStatus(
	_ context.Context,
	status domain.Status,
) error {
	r.statuses[status.DeviceID] = status
	return nil
}

func (r *fakeRepository) Statuses(
	_ context.Context,
	_ string,
) ([]domain.Status, error) {
	statuses := make([]domain.Status, 0, len(r.statuses))
	for _, status := range r.statuses {
		statuses = append(statuses, status)
	}
	return statuses, nil
}

func (r *fakeRepository) Entries(
	_ context.Context,
	familyID string,
) ([]domain.DictionaryEntry, error) {
	entries := make([]domain.DictionaryEntry, 0, len(r.entries))
	for _, entry := range r.entries {
		if entry.FamilyID == familyID {
			entries = append(entries, entry)
		}
	}
	return entries, nil
}

func (r *fakeRepository) EntryByTerm(
	_ context.Context,
	familyID string,
	term string,
) (domain.DictionaryEntry, bool, error) {
	entry, ok := r.entries[entryKey(familyID, term)]
	return entry, ok, nil
}

func (r *fakeRepository) SaveEntry(
	_ context.Context,
	entry domain.DictionaryEntry,
) error {
	if existing, ok := r.entries[entryKey(entry.FamilyID, entry.Term)]; ok {
		entry.ID = existing.ID
	}
	r.entries[entryKey(entry.FamilyID, entry.Term)] = entry
	return nil
}

func (r *fakeRepository) DeleteEntry(
	_ context.Context,
	familyID string,
	entryID string,
) error {
	for key, entry := range r.entries {
		if entry.FamilyID == familyID && entry.ID == entryID {
			delete(r.entries, key)
			return nil
		}
	}
	return domain.ErrDictionaryNotFound
}

func (r *fakeRepository) EntryCount(
	_ context.Context,
	familyID string,
) (int, error) {
	count := 0
	for _, entry := range r.entries {
		if entry.FamilyID == familyID {
			count++
		}
	}
	return count, nil
}

func (r *fakeRepository) Playback(
	_ context.Context,
	familyID string,
	deviceID string,
) (domain.PlaybackPlan, bool, error) {
	plan, ok := r.playback[familyID+"|"+deviceID]
	return plan, ok, nil
}

func (r *fakeRepository) SavePlayback(
	_ context.Context,
	familyID string,
	deviceID string,
	plan domain.PlaybackPlan,
) error {
	r.playback[familyID+"|"+deviceID] = plan
	return nil
}

func (r *fakeRepository) AdminStats(
	_ context.Context,
	windowDays int,
) (domain.AdminStats, error) {
	stats := r.stats
	stats.WindowDays = windowDays
	return stats, nil
}

func newService(t *testing.T, repository Repository) *Service {
	t.Helper()
	service, err := New(Options{Repository: repository})
	if err != nil {
		t.Fatalf("new service: %v", err)
	}
	return service
}

func TestPublishValidates(t *testing.T) {
	service := newService(t, newFakeRepository())
	policy, err := service.Publish(context.Background(), "family-1", domain.Input{
		DefaultExpression:  domain.ExpressionCurious,
		AnimationEnabled:   true,
		PointReadEnabled:   true,
		VideoEnabled:       true,
		VideoMaxFPS:        24,
		VideoMaxBitrateKBS: 2000,
		AnimationFrameMS:   120,
	})
	if err != nil {
		t.Fatalf("publish: %v", err)
	}
	if policy.Revision != 1 || policy.DefaultExpression != domain.ExpressionCurious {
		t.Fatalf("unexpected policy: %+v", policy)
	}

	// A name outside the fixed catalog and an out-of-range frame rate are
	// refused.
	if _, err := service.Publish(context.Background(), "family-1", domain.Input{
		DefaultExpression:  "grumpy",
		VideoMaxFPS:        24,
		VideoMaxBitrateKBS: 2000,
		AnimationFrameMS:   120,
	}); !errors.Is(err, domain.ErrInvalidPolicy) {
		t.Fatalf("expected ErrInvalidPolicy, got %v", err)
	}
	if _, err := service.Publish(context.Background(), "family-1", domain.Input{
		DefaultExpression:  domain.ExpressionHappy,
		VideoMaxFPS:        240,
		VideoMaxBitrateKBS: 2000,
		AnimationFrameMS:   120,
	}); !errors.Is(err, domain.ErrInvalidPolicy) {
		t.Fatalf("expected ErrInvalidPolicy for fps, got %v", err)
	}
}

func TestCapabilityAndStatus(t *testing.T) {
	repository := newFakeRepository()
	service := newService(t, repository)

	// A device that never reported is unsupported.
	capability, err := service.Capability(context.Background(), "family-1", "device-1")
	if err != nil {
		t.Fatalf("capability: %v", err)
	}
	if capability.Supported {
		t.Fatalf("expected unsupported, got %+v", capability)
	}

	_, err = service.ReportStatus(context.Background(), "family-1", "device-1", domain.StatusInput{
		ScreenSupported:      true,
		ExpressionsSupported: true,
		AnimationSupported:   true,
		PointReadSupported:   true,
		VideoSupported:       true,
		Expression:           domain.ExpressionHappy,
		Animation:            domain.AnimationWave,
		VideoState:           domain.VideoPlaying,
		PolicyRevision:       1,
	})
	if err != nil {
		t.Fatalf("report status: %v", err)
	}
	capability, err = service.Capability(context.Background(), "family-1", "device-1")
	if err != nil {
		t.Fatalf("capability after report: %v", err)
	}
	if !capability.Supported || !capability.UpToDate || !capability.VideoSupported {
		t.Fatalf("unexpected capability: %+v", capability)
	}

	// An unknown expression is refused.
	if _, err := service.ReportStatus(context.Background(), "family-1", "device-1", domain.StatusInput{
		ScreenSupported: true,
		Expression:      "grumpy",
		Animation:       domain.AnimationNone,
		VideoState:      domain.VideoIdle,
	}); !errors.Is(err, domain.ErrInvalidInput) {
		t.Fatalf("expected ErrInvalidInput, got %v", err)
	}
}

func TestUnsupportedDeviceReportsAnIdleSurface(t *testing.T) {
	repository := newFakeRepository()
	service := newService(t, repository)
	status, err := service.ReportStatus(context.Background(), "family-1", "device-1", domain.StatusInput{
		ScreenSupported: false,
		Expression:      domain.ExpressionHappy,
		Animation:       domain.AnimationWave,
		VideoState:      domain.VideoPlaying,
	})
	if err != nil {
		t.Fatalf("report status: %v", err)
	}
	if status.Expression != domain.ExpressionNeutral ||
		status.Animation != domain.AnimationNone ||
		status.VideoState != domain.VideoIdle {
		t.Fatalf("expected an idle surface, got %+v", status)
	}
}

func TestDictionaryLifecycleAndBound(t *testing.T) {
	repository := newFakeRepository()
	service := newService(t, repository)

	entry, err := service.PutEntry(context.Background(), "family-1", domain.DictionaryInput{
		Term: "  apple  ",
		Text: "苹果",
	})
	if err != nil {
		t.Fatalf("put entry: %v", err)
	}
	if entry.Term != "apple" || entry.Text != "苹果" {
		t.Fatalf("unexpected entry: %+v", entry)
	}

	// A duplicate term replaces the text and keeps the stable id.
	replaced, err := service.PutEntry(context.Background(), "family-1", domain.DictionaryInput{
		Term: "apple",
		Text: "苹果果",
	})
	if err != nil {
		t.Fatalf("replace entry: %v", err)
	}
	if replaced.ID != entry.ID || replaced.Text != "苹果果" {
		t.Fatalf("expected a stable-id replacement, got %+v", replaced)
	}
	entries, err := service.Dictionary(context.Background(), "family-1")
	if err != nil {
		t.Fatalf("dictionary: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected one entry, got %d", len(entries))
	}

	// An empty term or text is refused.
	if _, err := service.PutEntry(context.Background(), "family-1", domain.DictionaryInput{
		Term: "",
		Text: "苹果",
	}); !errors.Is(err, domain.ErrInvalidDictionaryEntry) {
		t.Fatalf("expected ErrInvalidDictionaryEntry, got %v", err)
	}

	// Fill to the bound and confirm one more is refused.
	for index := 2; index <= domain.PointReadCapacity; index++ {
		term := "term-" + string(rune('a'+index))
		if _, err := service.PutEntry(context.Background(), "family-1", domain.DictionaryInput{
			Term: term,
			Text: "value",
		}); err != nil {
			t.Fatalf("fill entry %d: %v", index, err)
		}
	}
	if _, err := service.PutEntry(context.Background(), "family-1", domain.DictionaryInput{
		Term: "overflow",
		Text: "value",
	}); !errors.Is(err, domain.ErrDictionaryFull) {
		t.Fatalf("expected ErrDictionaryFull, got %v", err)
	}

	if err := service.DeleteEntry(context.Background(), "family-1", entry.ID); err != nil {
		t.Fatalf("delete entry: %v", err)
	}
	if err := service.DeleteEntry(context.Background(), "family-1", "missing"); !errors.Is(err, domain.ErrDictionaryNotFound) {
		t.Fatalf("expected ErrDictionaryNotFound, got %v", err)
	}
}

func TestResolvePlaybackClampsToPolicy(t *testing.T) {
	repository := newFakeRepository()
	service := newService(t, repository)
	if _, err := service.Publish(context.Background(), "family-1", domain.Input{
		DefaultExpression:  domain.ExpressionHappy,
		AnimationEnabled:   true,
		PointReadEnabled:   true,
		VideoEnabled:       true,
		VideoMaxFPS:        24,
		VideoMaxBitrateKBS: 2000,
		AnimationFrameMS:   120,
	}); err != nil {
		t.Fatalf("publish: %v", err)
	}

	plan, err := service.ResolvePlayback(
		context.Background(), "family-1", "device-1", 9000, 60,
	)
	if err != nil {
		t.Fatalf("resolve playback: %v", err)
	}
	if plan.BitrateKBS != 2000 || plan.FPS != 24 {
		t.Fatalf("expected the policy ceiling, got %+v", plan)
	}

	stored, err := service.Playback(context.Background(), "family-1", "device-1")
	if err != nil {
		t.Fatalf("playback: %v", err)
	}
	if stored != plan {
		t.Fatalf("expected the stored plan, got %+v", stored)
	}

	// A device with no stored plan falls back to the policy maximum.
	defaultPlan, err := service.Playback(context.Background(), "family-1", "device-2")
	if err != nil {
		t.Fatalf("default playback: %v", err)
	}
	if defaultPlan.FPS != 24 || defaultPlan.BitrateKBS != 2000 {
		t.Fatalf("unexpected default plan: %+v", defaultPlan)
	}
}

func TestResolvePlaybackRefusedWhenVideoDisabled(t *testing.T) {
	repository := newFakeRepository()
	service := newService(t, repository)
	if _, err := service.Publish(context.Background(), "family-1", domain.Input{
		DefaultExpression:  domain.ExpressionHappy,
		AnimationEnabled:   true,
		PointReadEnabled:   true,
		VideoEnabled:       false,
		VideoMaxFPS:        30,
		VideoMaxBitrateKBS: 1500,
		AnimationFrameMS:   200,
	}); err != nil {
		t.Fatalf("publish: %v", err)
	}
	if _, err := service.ResolvePlayback(
		context.Background(), "family-1", "device-1", 1200, 24,
	); !errors.Is(err, domain.ErrInvalidPlayback) {
		t.Fatalf("expected ErrInvalidPlayback, got %v", err)
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
	if stats.WindowDays != 7 || stats.DeviceCount != 3 {
		t.Fatalf("unexpected stats: %+v", stats)
	}
}

type readinessRepository struct {
	*fakeRepository
	freshness time.Duration
	readiness featurehealth.DeviceCapabilityReadiness
}

func (r *readinessRepository) Readiness(
	_ context.Context,
	freshness time.Duration,
) (featurehealth.DeviceCapabilityReadiness, error) {
	r.freshness = freshness
	return r.readiness, nil
}

func TestCheckReadinessReturnsRepositoryHealth(t *testing.T) {
	repository := &readinessRepository{
		fakeRepository: newFakeRepository(),
		readiness: featurehealth.DeviceCapabilityReadiness{
			Status: featurehealth.StatusDegraded,
			Facts: featurehealth.DeviceCapabilityFacts{
				ExpectedDevices: 2,
				ReportedDevices: 1,
			},
		},
	}
	service := newService(t, repository)
	readiness, err := service.CheckReadiness(context.Background())
	if err != nil {
		t.Fatalf("check readiness: %v", err)
	}
	if readiness.Status != featurehealth.StatusDegraded ||
		repository.freshness != capabilityFreshness {
		t.Fatalf("readiness = %+v, freshness = %s", readiness, repository.freshness)
	}
}

func TestCheckReadinessFailsClosedWithoutCapabilityReader(t *testing.T) {
	service := newService(t, newFakeRepository())
	readiness, err := service.CheckReadiness(context.Background())
	if err != nil {
		t.Fatalf("check readiness: %v", err)
	}
	if readiness.Status != featurehealth.StatusUnavailable {
		t.Fatalf("status = %q, want unavailable", readiness.Status)
	}
}
