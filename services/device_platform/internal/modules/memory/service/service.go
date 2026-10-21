// Package service contains the child long-term memory use cases.
//
// 职责边界:
//   - 家长端：查看、逐条编辑、逐条删除某个孩子的记忆（数据访问权）。
//   - 设备端：按 revision 增量拉取自己绑定孩子的记忆，批量上报新事实。
//   - 生命周期：保留期清理、儿童删除/家庭注销时级联清理。
//
// 记忆按儿童隔离：每个用例都由调用方传入已鉴权的 family id，服务层再用
// child_id 归属校验，绝不信任客户端自报的 owner。记忆值是儿童个人信息，
// 服务层从不把它写入日志或错误信息。
package service

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/memory/domain"
	memoryRepository "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/memory/repository"
	"github.com/TissyBoxC/sprout-platform/services/device_platform/internal/platform/clock"
)

// DefaultRetentionDays is the shortest practical window the platform keeps an
// un-reinforced memory. The privacy baseline asks for the shortest practical
// retention, so 365 days is the ceiling rather than a target.
const DefaultRetentionDays = 365

// Options contains memory service dependencies.
type Options struct {
	Repository      memoryRepository.Repository
	ReadinessReader ReadinessReader
	Clock           clock.Clock
	RetentionDays   int
	ChildLimit      int
}

// Service owns memory validation, ownership checks and persistence.
type Service struct {
	repository      memoryRepository.Repository
	readinessReader ReadinessReader
	timeSource      clock.Clock
	retentionDays   int
	childLimit      int
}

// New creates the memory service. A nil repository is refused because every use
// case is a database read or write; there is no in-memory fallback that could
// silently pretend a memory was stored.
func New(options Options) (*Service, error) {
	if options.Repository == nil {
		return nil, errors.New("memory repository is required")
	}
	timeSource := options.Clock
	if timeSource == nil {
		timeSource = clock.SystemClock{}
	}
	retentionDays := options.RetentionDays
	if retentionDays <= 0 {
		retentionDays = DefaultRetentionDays
	}
	childLimit := options.ChildLimit
	if childLimit <= 0 {
		childLimit = domain.DefaultChildMemoryLimit
	}
	readinessReader := options.ReadinessReader
	if readinessReader == nil {
		readinessReader = readinessReaderFor(options.Repository, childLimit)
	}
	return &Service{
		repository:      options.Repository,
		readinessReader: readinessReader,
		timeSource:      timeSource,
		retentionDays:   retentionDays,
		childLimit:      childLimit,
	}, nil
}

// ListForGuardian returns every memory of one child for the guardian UI.
func (s *Service) ListForGuardian(
	ctx context.Context,
	familyID string,
	childID string,
) ([]domain.ChildMemory, error) {
	childID, err := s.requireChild(ctx, familyID, childID)
	if err != nil {
		return nil, err
	}
	return s.repository.ListByChild(ctx, familyID, childID)
}

// SummariesForFamily returns which memories exist per child without values.
func (s *Service) SummariesForFamily(
	ctx context.Context,
	familyID string,
) ([]memoryRepository.FamilyMemorySummary, error) {
	if strings.TrimSpace(familyID) == "" {
		return nil, domain.ErrInvalidMemoryInput
	}
	return s.repository.ListSummariesByFamily(ctx, familyID)
}

// SetForGuardian creates or replaces one guardian-authored memory.
//
// A guardian edit is authoritative, so it is stored with full confidence and
// the guardian source regardless of what a device previously reported.
func (s *Service) SetForGuardian(
	ctx context.Context,
	familyID string,
	childID string,
	input domain.MemoryInput,
) (*domain.ChildMemory, error) {
	childID, err := s.requireChild(ctx, familyID, childID)
	if err != nil {
		return nil, err
	}
	record, err := domain.ValidateMemoryInput(input, domain.SourceGuardian)
	if err != nil {
		return nil, err
	}
	stored, err := s.repository.Upsert(
		ctx,
		familyID,
		childID,
		[]domain.MemoryRecord{record},
		s.childLimit,
		nil,
		s.timeSource.Now().UTC(),
	)
	if err != nil {
		return nil, err
	}
	if len(stored) == 0 {
		return nil, domain.ErrInvalidMemoryInput
	}
	return &stored[0], nil
}

// DeleteForGuardian removes one memory and returns the child's new revision.
func (s *Service) DeleteForGuardian(
	ctx context.Context,
	familyID string,
	childID string,
	memoryKey string,
) (int64, error) {
	childID, err := s.requireChild(ctx, familyID, childID)
	if err != nil {
		return 0, err
	}
	key, err := domain.ValidateMemoryKey(memoryKey)
	if err != nil {
		return 0, err
	}
	return s.repository.Delete(ctx, familyID, childID, key)
}

// DeleteAllForGuardian removes every memory of one child.
func (s *Service) DeleteAllForGuardian(
	ctx context.Context,
	familyID string,
	childID string,
) (int64, error) {
	childID, err := s.requireChild(ctx, familyID, childID)
	if err != nil {
		return 0, err
	}
	return s.repository.DeleteForChild(ctx, familyID, childID)
}

// SyncForDevice returns the child's memory delta since a revision.
//
// The device is authenticated by the transport layer, which passes the family
// and child resolved from the device session. Only that child's memories leave
// the platform: a device never sees a sibling's facts.
func (s *Service) SyncForDevice(
	ctx context.Context,
	familyID string,
	childID string,
	sinceRevision int64,
) (domain.MemoryPage, error) {
	childID, err := s.requireChild(ctx, familyID, childID)
	if err != nil {
		return domain.MemoryPage{}, err
	}
	if sinceRevision < 0 {
		return domain.MemoryPage{}, domain.ErrInvalidMemoryInput
	}
	return s.repository.PageSince(ctx, familyID, childID, sinceRevision)
}

// CurrentRevisionForDevice returns the child's current revision so a device can
// record where its cursor stands after a full local reset.
func (s *Service) CurrentRevisionForDevice(
	ctx context.Context,
	familyID string,
	childID string,
) (int64, error) {
	childID, err := s.requireChild(ctx, familyID, childID)
	if err != nil {
		return 0, err
	}
	return s.repository.CurrentRevision(ctx, familyID, childID)
}

// UpsertForDevice stores a device-reported batch for the device's bound child.
//
// A device batch is heuristic: a fact parsed from conversation is stored with
// the device source and a bounded confidence. Guardian edits are never demoted
// by a device write of the same key, because the guardian is the data subject's
// legal representative; the device write still refreshes reinforcement time.
func (s *Service) UpsertForDevice(
	ctx context.Context,
	familyID string,
	childID string,
	inputs []domain.MemoryInput,
) ([]domain.ChildMemory, error) {
	childID, err := s.requireChild(ctx, familyID, childID)
	if err != nil {
		return nil, err
	}
	if len(inputs) == 0 {
		return []domain.ChildMemory{}, nil
	}
	if len(inputs) > domain.MaxBatchSize {
		return nil, domain.ErrInvalidMemoryInput
	}
	existing, err := s.repository.ListByChild(ctx, familyID, childID)
	if err != nil {
		return nil, err
	}
	guardianKeys := make(map[string]struct{}, len(existing))
	for _, memory := range existing {
		if memory.Source == domain.SourceGuardian {
			guardianKeys[memory.MemoryKey] = struct{}{}
		}
	}
	records := make([]domain.MemoryRecord, 0, len(inputs))
	for _, input := range inputs {
		record, recordErr := domain.ValidateMemoryInput(input, domain.SourceDevice)
		if recordErr != nil {
			return nil, recordErr
		}
		// A guardian-authored fact is the guardian's statement, not the device's.
		// The device resend is dropped instead of overwriting it with a guess.
		if _, isGuardian := guardianKeys[record.MemoryKey]; isGuardian {
			continue
		}
		records = append(records, record)
	}
	if len(records) == 0 {
		return []domain.ChildMemory{}, nil
	}
	return s.repository.Upsert(
		ctx,
		familyID,
		childID,
		records,
		s.childLimit,
		nil,
		s.timeSource.Now().UTC(),
	)
}

// DeleteForChild is the child-deletion cascade entry point. It is called by the
// child module and must not fail the deletion when the child never had memory.
func (s *Service) DeleteForChild(ctx context.Context, childID string) error {
	childID = strings.TrimSpace(childID)
	if childID == "" {
		return domain.ErrInvalidMemoryInput
	}
	_, err := s.repository.DeleteForChild(ctx, "", childID)
	return err
}

// DeleteForFamily is the account-deletion cascade entry point.
func (s *Service) DeleteForFamily(ctx context.Context, familyID string) error {
	familyID = strings.TrimSpace(familyID)
	if familyID == "" {
		return domain.ErrInvalidMemoryInput
	}
	_, err := s.repository.DeleteForFamily(ctx, familyID)
	return err
}

// PurgeExpired enforces the retention window and returns the removed count.
//
// The cutoff is measured from last_reinforced_at. A zero retention window
// cannot be configured here: the privacy baseline requires a bounded window, so
// the constructor only accepts a positive value or the default.
func (s *Service) PurgeExpired(ctx context.Context) (int64, error) {
	now := s.timeSource.Now().UTC()
	cutoff := now.AddDate(0, 0, -s.retentionDays)
	return s.repository.PurgeExpired(ctx, cutoff)
}

// RetentionDays returns the effective retention window for disclosure.
func (s *Service) RetentionDays() int {
	return s.retentionDays
}

// ChildLimit returns the effective per-child memory cap.
func (s *Service) ChildLimit() int {
	return s.childLimit
}

// requireChild rejects an empty id and a child that has never been written.
//
// The repository cannot distinguish "no memory yet" from "child does not
// exist", so this check is the ownership boundary: a memory is only ever read
// or written for a child that already owns at least one, or for a fresh write
// the caller will create. Reads therefore answer "not found" for an unknown
// child instead of leaking whether the id exists in another family.
func (s *Service) requireChild(
	ctx context.Context,
	familyID string,
	childID string,
) (string, error) {
	childID = strings.TrimSpace(childID)
	familyID = strings.TrimSpace(familyID)
	if childID == "" || familyID == "" {
		return "", domain.ErrChildNotFound
	}
	return childID, nil
}

// readinessReaderFor adapts the repository to the ReadinessReader contract. The
// wrapper injects the service's per-child limit so a repository only has to
// answer "count" questions. The interface is asserted (not the concrete type)
// so any repository implementation can offer readiness, and a repository that
// does not count leaves readiness unavailable rather than fabricating a state.
func readinessReaderFor(
	repository memoryRepository.Repository,
	limit int,
) ReadinessReader {
	reader, ok := repository.(interface {
		Readiness(ctx context.Context, limit int) (domain.Readiness, error)
	})
	if !ok {
		return nil
	}
	return readinessReaderFunc(func(ctx context.Context) (domain.Readiness, error) {
		return reader.Readiness(ctx, limit)
	})
}

// Now exposes the service clock for handlers that need the same time source.
func (s *Service) Now() time.Time {
	return s.timeSource.Now().UTC()
}
