package service

import (
	"context"
	"errors"
	"sort"
	"testing"
	"time"

	"github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/memory/domain"
	memoryRepository "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/memory/repository"
)

// stubRepository is a scripted store so the service tests exercise ownership,
// limits, tombstones and the guardian/device precedence rule without a database.
type stubRepository struct {
	memories     map[string]*domain.ChildMemory
	tombstones   map[string]int64
	revision     int64
	limitErr     error
	upsertErr    error
	purgeCutoff  time.Time
	readiness    domain.Readiness
	readinessErr error
}

func newStubRepository() *stubRepository {
	return &stubRepository{
		memories:   map[string]*domain.ChildMemory{},
		tombstones: map[string]int64{},
	}
}

func memoryStoreKey(childID string, memoryKey string) string {
	return childID + "/" + memoryKey
}

func (r *stubRepository) Upsert(
	_ context.Context,
	familyID string,
	childID string,
	records []domain.MemoryRecord,
	limit int,
	expectedRevision *int64,
	now time.Time,
) ([]domain.ChildMemory, error) {
	if r.upsertErr != nil {
		return nil, r.upsertErr
	}
	if expectedRevision != nil && *expectedRevision != r.revision {
		return nil, domain.ErrMemoryRevisionConflict
	}
	stored := make([]domain.ChildMemory, 0, len(records))
	for _, record := range records {
		r.revision++
		memory := &domain.ChildMemory{
			FamilyID:         familyID,
			ChildID:          childID,
			MemoryKey:        record.MemoryKey,
			MemoryValue:      record.MemoryValue,
			Confidence:       record.Confidence,
			Source:           record.Source,
			Revision:         r.revision,
			CreatedAt:        now,
			UpdatedAt:        now,
			LastReinforcedAt: now,
		}
		r.memories[memoryStoreKey(childID, record.MemoryKey)] = memory
		stored = append(stored, *memory)
	}
	if len(stored) > limit {
		return nil, r.limitErr
	}
	return stored, nil
}

func (r *stubRepository) ListByChild(
	_ context.Context,
	familyID string,
	childID string,
) ([]domain.ChildMemory, error) {
	result := make([]domain.ChildMemory, 0, len(r.memories))
	for _, memory := range r.memories {
		if memory.ChildID == childID && memory.FamilyID == familyID {
			result = append(result, *memory)
		}
	}
	sort.Slice(result, func(first, second int) bool {
		return result[first].MemoryKey < result[second].MemoryKey
	})
	return result, nil
}

func (r *stubRepository) ListSummariesByFamily(
	_ context.Context,
	familyID string,
) ([]memoryRepository.FamilyMemorySummary, error) {
	byChild := map[string][]domain.MemorySummary{}
	for _, memory := range r.memories {
		if memory.FamilyID != familyID {
			continue
		}
		byChild[memory.ChildID] = append(byChild[memory.ChildID], domain.MemorySummary{
			MemoryKey:        memory.MemoryKey,
			Source:           memory.Source,
			Confidence:       memory.Confidence,
			Revision:         memory.Revision,
			UpdatedAt:        memory.UpdatedAt,
			LastReinforcedAt: memory.LastReinforcedAt,
		})
	}
	result := make([]memoryRepository.FamilyMemorySummary, 0, len(byChild))
	for childID, summaries := range byChild {
		result = append(result, memoryRepository.FamilyMemorySummary{
			ChildID:  childID,
			Memories: summaries,
		})
	}
	return result, nil
}

func (r *stubRepository) PageSince(
	_ context.Context,
	familyID string,
	childID string,
	sinceRevision int64,
) (domain.MemoryPage, error) {
	page := domain.MemoryPage{ChildID: childID, SinceRevision: sinceRevision}
	for _, memory := range r.memories {
		if memory.ChildID != childID || memory.FamilyID != familyID {
			continue
		}
		if memory.Revision > page.NextRevision {
			page.NextRevision = memory.Revision
		}
		if memory.Revision > sinceRevision {
			page.Memories = append(page.Memories, *memory)
		}
	}
	for key, revision := range r.tombstones {
		if revision > page.NextRevision {
			page.NextRevision = revision
		}
		if revision > sinceRevision {
			page.DeletedKeys = append(page.DeletedKeys, key)
		}
	}
	return page, nil
}

func (r *stubRepository) CurrentRevision(
	_ context.Context,
	_ string,
	_ string,
) (int64, error) {
	return r.revision, nil
}

func (r *stubRepository) Get(
	_ context.Context,
	familyID string,
	childID string,
	memoryKey string,
) (*domain.ChildMemory, error) {
	memory, ok := r.memories[memoryStoreKey(childID, memoryKey)]
	if !ok || memory.FamilyID != familyID {
		return nil, domain.ErrMemoryNotFound
	}
	copyMemory := *memory
	return &copyMemory, nil
}

func (r *stubRepository) Delete(
	_ context.Context,
	familyID string,
	childID string,
	memoryKey string,
) (int64, error) {
	storeKey := memoryStoreKey(childID, memoryKey)
	memory, ok := r.memories[storeKey]
	if !ok || memory.FamilyID != familyID {
		return 0, domain.ErrMemoryNotFound
	}
	delete(r.memories, storeKey)
	r.revision++
	r.tombstones[memoryKey] = r.revision
	return r.revision, nil
}

func (r *stubRepository) DeleteForChild(
	_ context.Context,
	familyID string,
	childID string,
) (int64, error) {
	var removed int64
	for storeKey, memory := range r.memories {
		if memory.ChildID != childID {
			continue
		}
		if familyID != "" && memory.FamilyID != familyID {
			continue
		}
		delete(r.memories, storeKey)
		removed++
	}
	for key := range r.tombstones {
		delete(r.tombstones, key)
	}
	return removed, nil
}

func (r *stubRepository) DeleteForFamily(
	_ context.Context,
	familyID string,
) (int64, error) {
	var removed int64
	for storeKey, memory := range r.memories {
		if memory.FamilyID != familyID {
			continue
		}
		delete(r.memories, storeKey)
		removed++
	}
	return removed, nil
}

func (r *stubRepository) PurgeExpired(
	_ context.Context,
	cutoff time.Time,
) (int64, error) {
	r.purgeCutoff = cutoff
	var removed int64
	for storeKey, memory := range r.memories {
		if memory.LastReinforcedAt.Before(cutoff) {
			delete(r.memories, storeKey)
			removed++
		}
	}
	return removed, nil
}

func (r *stubRepository) Readiness(
	_ context.Context,
	_ int,
) (domain.Readiness, error) {
	return r.readiness, r.readinessErr
}

type fixedClock struct{ now time.Time }

func (c fixedClock) Now() time.Time { return c.now }

func newTestService(t *testing.T, repository *stubRepository, options Options) *Service {
	t.Helper()
	options.Repository = repository
	if options.Clock == nil {
		options.Clock = fixedClock{now: time.Date(2026, 10, 21, 9, 0, 0, 0, time.UTC)}
	}
	service, err := New(options)
	if err != nil {
		t.Fatalf("create memory service: %v", err)
	}
	return service
}

func TestNewRequiresARepository(t *testing.T) {
	if _, err := New(Options{}); err == nil {
		t.Fatal("expected a memory service without a repository to be refused")
	}
}

func TestGuardianSetStoresAuthoritativeFact(t *testing.T) {
	repository := newStubRepository()
	service := newTestService(t, repository, Options{})

	memory, err := service.SetForGuardian(
		context.Background(),
		"family-1",
		"child-1",
		domain.MemoryInput{MemoryKey: "favorite_color", MemoryValue: "蓝色"},
	)
	if err != nil {
		t.Fatalf("set guardian memory: %v", err)
	}
	if memory.Source != domain.SourceGuardian || memory.Confidence != 1 {
		t.Fatalf("source/confidence = %s/%v, want guardian/1", memory.Source, memory.Confidence)
	}
	if memory.Revision != 1 {
		t.Fatalf("revision = %d, want 1", memory.Revision)
	}
}

func TestGuardianSetRejectsEmptyChild(t *testing.T) {
	service := newTestService(t, newStubRepository(), Options{})
	_, err := service.SetForGuardian(
		context.Background(),
		"family-1",
		" ",
		domain.MemoryInput{MemoryKey: "pet_name", MemoryValue: "咪咪"},
	)
	if !errors.Is(err, domain.ErrChildNotFound) {
		t.Fatalf("error = %v, want ErrChildNotFound", err)
	}
}

func TestGuardianSetRejectsEmptyFamily(t *testing.T) {
	service := newTestService(t, newStubRepository(), Options{})
	_, err := service.SetForGuardian(
		context.Background(),
		"",
		"child-1",
		domain.MemoryInput{MemoryKey: "pet_name", MemoryValue: "咪咪"},
	)
	if !errors.Is(err, domain.ErrChildNotFound) {
		t.Fatalf("error = %v, want ErrChildNotFound", err)
	}
}

func TestDeviceBatchDoesNotOverwriteGuardianFact(t *testing.T) {
	repository := newStubRepository()
	service := newTestService(t, repository, Options{})
	if _, err := service.SetForGuardian(
		context.Background(),
		"family-1",
		"child-1",
		domain.MemoryInput{MemoryKey: "pet_name", MemoryValue: "咪咪"},
	); err != nil {
		t.Fatalf("seed guardian memory: %v", err)
	}

	stored, err := service.UpsertForDevice(
		context.Background(),
		"family-1",
		"child-1",
		[]domain.MemoryInput{
			{MemoryKey: "pet_name", MemoryValue: "旺财"},
			{MemoryKey: "favorite_food", MemoryValue: "饺子"},
		},
	)
	if err != nil {
		t.Fatalf("device upsert: %v", err)
	}
	if len(stored) != 1 || stored[0].MemoryKey != "favorite_food" {
		t.Fatalf("stored = %+v, want only the new device fact", stored)
	}
	guardianFact, err := repository.Get(context.Background(), "family-1", "child-1", "pet_name")
	if err != nil {
		t.Fatalf("read guardian memory: %v", err)
	}
	if guardianFact.MemoryValue != "咪咪" {
		t.Fatalf("guardian fact = %q, want the guardian statement preserved", guardianFact.MemoryValue)
	}
}

func TestDeviceBatchRejectsWholeBatchOnOneInvalidEntry(t *testing.T) {
	repository := newStubRepository()
	service := newTestService(t, repository, Options{})
	_, err := service.UpsertForDevice(
		context.Background(),
		"family-1",
		"child-1",
		[]domain.MemoryInput{
			{MemoryKey: "pet_name", MemoryValue: "咪咪"},
			{MemoryKey: "bad key", MemoryValue: "x"},
		},
	)
	if !errors.Is(err, domain.ErrInvalidMemoryKey) {
		t.Fatalf("error = %v, want ErrInvalidMemoryKey", err)
	}
	if len(repository.memories) != 0 {
		t.Fatal("an invalid batch must not write any memory")
	}
}

func TestDeviceBatchRejectsOversizedBatch(t *testing.T) {
	service := newTestService(t, newStubRepository(), Options{})
	inputs := make([]domain.MemoryInput, domain.MaxBatchSize+1)
	for index := range inputs {
		inputs[index] = domain.MemoryInput{
			MemoryKey:   "fact_number_" + string(rune('a'+index%26)) + string(rune('a'+index/26)),
			MemoryValue: "value",
		}
	}
	if _, err := service.UpsertForDevice(
		context.Background(),
		"family-1",
		"child-1",
		inputs,
	); !errors.Is(err, domain.ErrInvalidMemoryInput) {
		t.Fatalf("error = %v, want ErrInvalidMemoryInput", err)
	}
}

func TestDeviceBatchPropagatesLimitExceeded(t *testing.T) {
	repository := newStubRepository()
	repository.limitErr = domain.ErrMemoryLimitExceeded
	service := newTestService(t, repository, Options{ChildLimit: 1})
	_, err := service.UpsertForDevice(
		context.Background(),
		"family-1",
		"child-1",
		[]domain.MemoryInput{
			{MemoryKey: "pet_name", MemoryValue: "咪咪"},
			{MemoryKey: "favorite_food", MemoryValue: "饺子"},
		},
	)
	if !errors.Is(err, domain.ErrMemoryLimitExceeded) {
		t.Fatalf("error = %v, want ErrMemoryLimitExceeded", err)
	}
}

func TestDeletePublishesTombstoneThroughSync(t *testing.T) {
	repository := newStubRepository()
	service := newTestService(t, repository, Options{})
	if _, err := service.SetForGuardian(
		context.Background(),
		"family-1",
		"child-1",
		domain.MemoryInput{MemoryKey: "pet_name", MemoryValue: "咪咪"},
	); err != nil {
		t.Fatalf("seed memory: %v", err)
	}
	revision, err := service.DeleteForGuardian(
		context.Background(),
		"family-1",
		"child-1",
		"pet_name",
	)
	if err != nil {
		t.Fatalf("delete memory: %v", err)
	}
	page, err := service.SyncForDevice(context.Background(), "family-1", "child-1", 1)
	if err != nil {
		t.Fatalf("sync: %v", err)
	}
	// The delete response already answers with the new cursor, and a pull from
	// the pre-delete revision must report both the same cursor and the tombstone
	// so a device that missed the response still learns about the deletion.
	if page.NextRevision != revision {
		t.Fatalf("next revision = %d, want %d", page.NextRevision, revision)
	}
	if len(page.DeletedKeys) != 1 || page.DeletedKeys[0] != "pet_name" {
		t.Fatalf("deleted keys = %v, want [pet_name]", page.DeletedKeys)
	}
}

func TestSyncRejectsNegativeRevision(t *testing.T) {
	service := newTestService(t, newStubRepository(), Options{})
	_, err := service.SyncForDevice(context.Background(), "family-1", "child-1", -1)
	if !errors.Is(err, domain.ErrInvalidMemoryInput) {
		t.Fatalf("error = %v, want ErrInvalidMemoryInput", err)
	}
}

func TestPurgeExpiredUsesReinforcementWindow(t *testing.T) {
	now := time.Date(2026, 10, 21, 9, 0, 0, 0, time.UTC)
	repository := newStubRepository()
	repository.memories[memoryStoreKey("child-1", "pet_name")] = &domain.ChildMemory{
		FamilyID:         "family-1",
		ChildID:          "child-1",
		MemoryKey:        "pet_name",
		MemoryValue:      "咪咪",
		LastReinforcedAt: now.AddDate(0, 0, -400),
	}
	repository.memories[memoryStoreKey("child-1", "favorite_color")] = &domain.ChildMemory{
		FamilyID:         "family-1",
		ChildID:          "child-1",
		MemoryKey:        "favorite_color",
		MemoryValue:      "蓝色",
		LastReinforcedAt: now.AddDate(0, 0, -10),
	}
	service := newTestService(t, repository, Options{
		Clock:         fixedClock{now: now},
		RetentionDays: 365,
	})
	removed, err := service.PurgeExpired(context.Background())
	if err != nil {
		t.Fatalf("purge: %v", err)
	}
	if removed != 1 {
		t.Fatalf("removed = %d, want 1", removed)
	}
	wantCutoff := now.AddDate(0, 0, -365)
	if !repository.purgeCutoff.Equal(wantCutoff) {
		t.Fatalf("cutoff = %s, want %s", repository.purgeCutoff, wantCutoff)
	}
	if _, err := repository.Get(
		context.Background(),
		"family-1",
		"child-1",
		"favorite_color",
	); err != nil {
		t.Fatalf("recently reinforced memory must survive: %v", err)
	}
}

func TestRetentionDaysFallsBackToTheDisclosedDefault(t *testing.T) {
	service := newTestService(t, newStubRepository(), Options{})
	if service.RetentionDays() != DefaultRetentionDays {
		t.Fatalf("retention = %d, want %d", service.RetentionDays(), DefaultRetentionDays)
	}
	if service.ChildLimit() != domain.DefaultChildMemoryLimit {
		t.Fatalf("limit = %d, want %d", service.ChildLimit(), domain.DefaultChildMemoryLimit)
	}
}

func TestDeleteForChildIsIdempotent(t *testing.T) {
	repository := newStubRepository()
	service := newTestService(t, repository, Options{})
	if err := service.DeleteForChild(context.Background(), "child-1"); err != nil {
		t.Fatalf("delete for a child without memory: %v", err)
	}
	if err := service.DeleteForChild(context.Background(), " "); !errors.Is(
		err,
		domain.ErrInvalidMemoryInput,
	) {
		t.Fatalf("blank child error = %v, want ErrInvalidMemoryInput", err)
	}
}

func TestReadinessHealthAndDegradedStates(t *testing.T) {
	repository := newStubRepository()
	repository.readiness = domain.Readiness{MemoryCount: 3, ChildCount: 2}
	service := newTestService(t, repository, Options{})
	readiness, err := service.Readiness(context.Background())
	if err != nil {
		t.Fatalf("readiness: %v", err)
	}
	if readiness.State != domain.ReadinessHealthy || readiness.MemoryCount != 3 {
		t.Fatalf("readiness = %+v, want healthy with 3 memories", readiness)
	}

	repository.readiness = domain.Readiness{
		MemoryCount:         210,
		ChildCount:          2,
		OverLimitChildCount: 1,
	}
	readiness, err = service.Readiness(context.Background())
	if err != nil {
		t.Fatalf("readiness: %v", err)
	}
	if readiness.State != domain.ReadinessDegraded {
		t.Fatalf("state = %q, want degraded when a child saturated the cap", readiness.State)
	}
}

func TestReadinessSurfacesReadFailure(t *testing.T) {
	repository := newStubRepository()
	repository.readinessErr = errors.New("database unavailable")
	service := newTestService(t, repository, Options{})
	if _, err := service.Readiness(context.Background()); err == nil {
		t.Fatal("expected the readiness read failure to surface")
	}
}
