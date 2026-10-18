// Package service contains English learning use cases.
//
// Catalog reads are guardian-visible and read-only; progress writes are
// family-scoped so the service never trusts a client-provided owner. The
// progress rollup is derived from the bounded attempt list so the console can
// show best scores and lesson completion without a second aggregation query.
package service

import (
	"context"
	"errors"
	"strings"

	"github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/english/domain"
	"github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/english/repository"
	"github.com/TissyBoxC/sprout-platform/services/device_platform/internal/platform/clock"
	"github.com/google/uuid"
)

// DefaultAttemptLimit and MaxAttemptLimit bound the attempt browsing API.
// Progress uses a full aggregate and intentionally does not apply these
// limits.
const (
	DefaultAttemptLimit = 100
	MaxAttemptLimit     = 500
)

// Service owns English learning catalog reads and progress writes.
type Service struct {
	repository repository.Repository
	timeSource clock.Clock
}

// Options contains English learning dependencies.
type Options struct {
	Repository repository.Repository
	Clock      clock.Clock
}

// New creates the English learning service.
func New(options Options) (*Service, error) {
	if options.Repository == nil {
		return nil, errors.New("english repository is required")
	}
	timeSource := options.Clock
	if timeSource == nil {
		timeSource = clock.SystemClock{}
	}
	return &Service{repository: options.Repository, timeSource: timeSource}, nil
}

// ListLessons returns the published catalog for one age tier. An empty age
// tier returns every tier so an admin read can see the whole catalog.
func (s *Service) ListLessons(
	ctx context.Context,
	ageTier string,
) ([]domain.Lesson, error) {
	normalized := strings.TrimSpace(ageTier)
	if normalized != "" && !domain.IsAgeTier(normalized) {
		return nil, domain.ErrInvalidAgeTier
	}
	return s.repository.ListLessons(ctx, normalized)
}

// GetLesson returns one lesson with its ordered words and sentences.
func (s *Service) GetLesson(
	ctx context.Context,
	lessonID string,
) (*domain.Lesson, error) {
	normalized := strings.TrimSpace(lessonID)
	if !domain.LessonIDValid(normalized) {
		return nil, domain.ErrInvalidLessonID
	}
	return s.repository.GetLesson(ctx, normalized)
}

// LessonCatalog returns the published catalogue with each lesson's ordered
// words and sentences so a device can cache the full teaching content in one
// read. It hydrates from the summary list plus the per-lesson loader rather
// than adding a second repository query shape.
func (s *Service) LessonCatalog(
	ctx context.Context,
	ageTier string,
) ([]domain.Lesson, error) {
	normalized := strings.TrimSpace(ageTier)
	if normalized != "" && !domain.IsAgeTier(normalized) {
		return nil, domain.ErrInvalidAgeTier
	}
	if reader, ok := s.repository.(repository.CatalogReader); ok {
		return reader.ListLessonCatalog(ctx, normalized, repository.CatalogMaxLessons)
	}
	summaries, err := s.repository.ListLessons(ctx, normalized)
	if err != nil {
		return nil, err
	}
	catalog := make([]domain.Lesson, 0, len(summaries))
	for index := range summaries {
		if len(catalog) >= repository.CatalogMaxLessons {
			break
		}
		lesson, err := s.repository.GetLesson(ctx, summaries[index].LessonID)
		if err != nil {
			return nil, err
		}
		catalog = append(catalog, *lesson)
	}
	return catalog, nil
}

// RecordAttempt validates and stores one practice attempt, deriving Passed
// from the reported score so a client cannot claim an unearned pass.
func (s *Service) RecordAttempt(
	ctx context.Context,
	familyID string,
	input domain.AttemptInput,
) (*domain.PracticeAttempt, error) {
	familyID = strings.TrimSpace(familyID)
	if familyID == "" {
		return nil, domain.ErrInvalidAttempt
	}
	normalized, err := domain.ValidateAttemptInput(input)
	if err != nil {
		return nil, err
	}
	now := s.timeSource.Now().UTC()
	attempt := &domain.PracticeAttempt{
		ID:             uuid.NewString(),
		FamilyID:       familyID,
		ChildID:        normalized.ChildID,
		LessonID:       normalized.LessonID,
		ItemID:         normalized.ItemID,
		ItemType:       normalized.ItemType,
		TargetText:     normalized.TargetText,
		RecognizedText: normalized.RecognizedText,
		Score:          normalized.Score,
		Passed:         domain.AttemptPassed(normalized.Score),
		PracticedAt:    now,
		CreatedAt:      now,
	}
	if err := s.repository.CreateAttempt(ctx, attempt); err != nil {
		return nil, err
	}
	return attempt, nil
}

// ListAttempts returns a bounded, family-scoped attempt list.
func (s *Service) ListAttempts(
	ctx context.Context,
	familyID string,
	query domain.AttemptQuery,
) ([]domain.PracticeAttempt, error) {
	query.ChildID = strings.TrimSpace(query.ChildID)
	query.LessonID = strings.TrimSpace(query.LessonID)
	switch {
	case query.Limit <= 0:
		query.Limit = DefaultAttemptLimit
	case query.Limit > MaxAttemptLimit:
		query.Limit = MaxAttemptLimit
	}
	return s.repository.ListAttempts(ctx, familyID, query)
}

// Progress summarizes every attempt owned by one family into per-item
// progress and a per-lesson rollup, newest activity first.
func (s *Service) Progress(
	ctx context.Context,
	familyID string,
	query domain.AttemptQuery,
) ([]domain.LessonProgress, []domain.ProgressItem, error) {
	familyID = strings.TrimSpace(familyID)
	if familyID == "" {
		return nil, nil, domain.ErrInvalidAttempt
	}
	query.ChildID = strings.TrimSpace(query.ChildID)
	query.LessonID = strings.TrimSpace(query.LessonID)
	if reader, ok := s.repository.(repository.ProgressReader); ok {
		return reader.ListProgress(ctx, familyID, query)
	}
	// The legacy contract has a LIMIT parameter. Force it to zero so a caller
	// passing the browsing limit cannot silently truncate a progress rollup.
	query.Limit = 0
	attempts, err := s.repository.ListAttempts(ctx, familyID, query)
	if err != nil {
		return nil, nil, err
	}
	lessons, items := summarize(attempts)
	return lessons, items, nil
}

// itemKey identifies one practiced item across attempts.
type itemKey struct {
	lessonID string
	itemID   string
}

// summarize folds a newest-first attempt list into per-item best scores and a
// per-lesson rollup. It preserves the first-seen (newest) order so the console
// leads with the most recent activity.
func summarize(
	attempts []domain.PracticeAttempt,
) ([]domain.LessonProgress, []domain.ProgressItem) {
	itemOrder := make([]itemKey, 0, len(attempts))
	items := make(map[itemKey]*domain.ProgressItem)
	lessons := make(map[string]*domain.LessonProgress)
	lessonOrder := make([]string, 0, 8)

	for index := range attempts {
		attempt := attempts[index]
		key := itemKey{lessonID: attempt.LessonID, itemID: attempt.ItemID}
		item, seen := items[key]
		if !seen {
			item = &domain.ProgressItem{
				LessonID:   attempt.LessonID,
				ItemID:     attempt.ItemID,
				ItemType:   attempt.ItemType,
				TargetText: attempt.TargetText,
				LastAt:     attempt.PracticedAt,
			}
			items[key] = item
			itemOrder = append(itemOrder, key)
		}
		item.Attempts++
		if attempt.Score > item.BestScore {
			item.BestScore = attempt.Score
		}
		if attempt.Passed {
			item.Passed = true
		}

		lesson, seenLesson := lessons[attempt.LessonID]
		if !seenLesson {
			lesson = &domain.LessonProgress{LessonID: attempt.LessonID}
			lessons[attempt.LessonID] = lesson
			lessonOrder = append(lessonOrder, attempt.LessonID)
		}
		lesson.Attempts++
		lesson.TotalItems = countItems(itemOrder, attempt.LessonID)
		if attempt.Score > lesson.BestScore {
			lesson.BestScore = attempt.Score
		}
		lesson.AverageScore += attempt.Score
		if attempt.PracticedAt.After(lesson.LastPracticed) {
			lesson.LastPracticed = attempt.PracticedAt
		}
	}

	itemResults := make([]domain.ProgressItem, 0, len(itemOrder))
	for _, key := range itemOrder {
		itemResults = append(itemResults, *items[key])
	}

	for _, lessonID := range lessonOrder {
		lesson := lessons[lessonID]
		if lesson.Attempts > 0 {
			lesson.AverageScore = lesson.AverageScore / lesson.Attempts
		}
		lesson.PassedItems = countPassedItems(items, itemOrder, lessonID)
	}

	lessonResults := make([]domain.LessonProgress, 0, len(lessonOrder))
	for _, lessonID := range lessonOrder {
		lessonResults = append(lessonResults, *lessons[lessonID])
	}
	return lessonResults, itemResults
}

func countItems(order []itemKey, lessonID string) int {
	count := 0
	for _, key := range order {
		if key.lessonID == lessonID {
			count++
		}
	}
	return count
}

func countPassedItems(
	items map[itemKey]*domain.ProgressItem,
	order []itemKey,
	lessonID string,
) int {
	count := 0
	for _, key := range order {
		item := items[key]
		if key.lessonID == lessonID && item != nil && item.Passed {
			count++
		}
	}
	return count
}
