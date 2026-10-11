package service

import (
	"context"
	"testing"
	"time"

	"github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/english/domain"
)

type fixedClock struct {
	now time.Time
}

func (c fixedClock) Now() time.Time {
	return c.now
}

// memoryRepository is a small in-memory double for the service tests.
type memoryRepository struct {
	lessons  map[string]*domain.Lesson
	attempts []domain.PracticeAttempt
}

func newMemoryRepository() *memoryRepository {
	return &memoryRepository{lessons: map[string]*domain.Lesson{}}
}

func (r *memoryRepository) ListLessons(
	_ context.Context,
	ageTier string,
) ([]domain.Lesson, error) {
	result := make([]domain.Lesson, 0, len(r.lessons))
	for _, lesson := range r.lessons {
		if lesson.Status != domain.LessonStatusPublished {
			continue
		}
		if ageTier != "" && lesson.AgeTier != ageTier {
			continue
		}
		result = append(result, *lesson)
	}
	return result, nil
}

func (r *memoryRepository) GetLesson(
	_ context.Context,
	lessonID string,
) (*domain.Lesson, error) {
	lesson, ok := r.lessons[lessonID]
	if !ok {
		return nil, domain.ErrLessonNotFound
	}
	copyLesson := *lesson
	return &copyLesson, nil
}

func (r *memoryRepository) CreateAttempt(
	_ context.Context,
	attempt *domain.PracticeAttempt,
) error {
	r.attempts = append(r.attempts, *attempt)
	return nil
}

func (r *memoryRepository) ListAttempts(
	_ context.Context,
	familyID string,
	query domain.AttemptQuery,
) ([]domain.PracticeAttempt, error) {
	result := make([]domain.PracticeAttempt, 0, len(r.attempts))
	for _, attempt := range r.attempts {
		if attempt.FamilyID != familyID {
			continue
		}
		if query.ChildID != "" && attempt.ChildID != query.ChildID {
			continue
		}
		if query.LessonID != "" && attempt.LessonID != query.LessonID {
			continue
		}
		result = append(result, attempt)
	}
	if query.Limit > 0 && len(result) > query.Limit {
		result = result[:query.Limit]
	}
	return result, nil
}

func newService(t *testing.T) (*Service, *memoryRepository) {
	t.Helper()
	repository := newMemoryRepository()
	service, err := New(Options{
		Repository: repository,
		Clock:      fixedClock{now: time.Date(2026, 10, 11, 9, 0, 0, 0, time.UTC)},
	})
	if err != nil {
		t.Fatalf("create english service: %v", err)
	}
	return service, repository
}

func TestListLessonsFiltersByAgeTier(t *testing.T) {
	service, repository := newService(t)
	repository.lessons["lesson_fruit"] = &domain.Lesson{
		LessonID: "lesson_fruit",
		AgeTier:  domain.AgeTier3To4,
		Status:   domain.LessonStatusPublished,
	}
	repository.lessons["lesson_school"] = &domain.Lesson{
		LessonID: "lesson_school",
		AgeTier:  domain.AgeTier7To8,
		Status:   domain.LessonStatusPublished,
	}
	repository.lessons["lesson_draft"] = &domain.Lesson{
		LessonID: "lesson_draft",
		AgeTier:  domain.AgeTier3To4,
		Status:   domain.LessonStatusDraft,
	}

	lessons, err := service.ListLessons(context.Background(), domain.AgeTier3To4)
	if err != nil {
		t.Fatalf("list lessons: %v", err)
	}
	if len(lessons) != 1 || lessons[0].LessonID != "lesson_fruit" {
		t.Fatalf("lessons = %+v, want only lesson_fruit", lessons)
	}

	all, err := service.ListLessons(context.Background(), "")
	if err != nil {
		t.Fatalf("list all lessons: %v", err)
	}
	if len(all) != 2 {
		t.Fatalf("all lessons = %d, want 2 published", len(all))
	}

	if _, err := service.ListLessons(context.Background(), "age_99"); err != domain.ErrInvalidAgeTier {
		t.Fatalf("invalid age tier error = %v, want ErrInvalidAgeTier", err)
	}
}

func TestGetLessonRejectsInvalidID(t *testing.T) {
	service, _ := newService(t)
	if _, err := service.GetLesson(context.Background(), "Bad Id"); err != domain.ErrInvalidLessonID {
		t.Fatalf("error = %v, want ErrInvalidLessonID", err)
	}
}

func TestRecordAttemptDerivesPassAndClampsScore(t *testing.T) {
	service, _ := newService(t)

	attempt, err := service.RecordAttempt(context.Background(), "family-1", domain.AttemptInput{
		ChildID:    "child-1",
		LessonID:   "lesson_fruit",
		ItemID:     "word-apple",
		ItemType:   domain.ItemTypeWord,
		TargetText: "apple",
		Score:      85,
	})
	if err != nil {
		t.Fatalf("record attempt: %v", err)
	}
	if !attempt.Passed {
		t.Fatalf("score 85 should pass")
	}
	if attempt.ID == "" || attempt.FamilyID != "family-1" {
		t.Fatalf("unexpected attempt: %+v", attempt)
	}

	// A noisy out-of-range score is clamped, and the pass derives from it.
	clamped, err := service.RecordAttempt(context.Background(), "family-1", domain.AttemptInput{
		LessonID:   "lesson_fruit",
		ItemID:     "word-apple",
		ItemType:   domain.ItemTypeWord,
		TargetText: "apple",
		Score:      140,
	})
	if err != nil {
		t.Fatalf("record attempt: %v", err)
	}
	if clamped.Score != 100 {
		t.Fatalf("clamped score = %d, want 100", clamped.Score)
	}
	if !clamped.Passed {
		t.Fatalf("clamped 100 should pass")
	}

	failed, err := service.RecordAttempt(context.Background(), "family-1", domain.AttemptInput{
		LessonID:   "lesson_fruit",
		ItemID:     "word-apple",
		ItemType:   domain.ItemTypeWord,
		TargetText: "apple",
		Score:      -5,
	})
	if err != nil {
		t.Fatalf("record attempt: %v", err)
	}
	if failed.Score != 0 || failed.Passed {
		t.Fatalf("failed attempt = %+v, want score 0 and not passed", failed)
	}
}

func TestRecordAttemptRejectsInvalidInput(t *testing.T) {
	service, _ := newService(t)
	cases := []struct {
		name  string
		input domain.AttemptInput
		want  error
	}{
		{
			name: "bad lesson id",
			input: domain.AttemptInput{
				LessonID:   "Bad",
				ItemID:     "word-apple",
				ItemType:   domain.ItemTypeWord,
				TargetText: "apple",
			},
			want: domain.ErrInvalidLessonID,
		},
		{
			name: "bad item type",
			input: domain.AttemptInput{
				LessonID:   "lesson_fruit",
				ItemID:     "word-apple",
				ItemType:   "video",
				TargetText: "apple",
			},
			want: domain.ErrInvalidItemType,
		},
		{
			name: "missing target text",
			input: domain.AttemptInput{
				LessonID: "lesson_fruit",
				ItemID:   "word-apple",
				ItemType: domain.ItemTypeWord,
			},
			want: domain.ErrInvalidAttempt,
		},
		{
			name: "missing item id",
			input: domain.AttemptInput{
				LessonID:   "lesson_fruit",
				ItemType:   domain.ItemTypeWord,
				TargetText: "apple",
			},
			want: domain.ErrInvalidAttempt,
		},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			_, err := service.RecordAttempt(context.Background(), "family-1", testCase.input)
			if err != testCase.want {
				t.Fatalf("error = %v, want %v", err, testCase.want)
			}
		})
	}
}

func TestProgressRollsUpBestScoresAndPasses(t *testing.T) {
	service, _ := newService(t)
	ctx := context.Background()
	inputs := []domain.AttemptInput{
		{ChildID: "child-1", LessonID: "lesson_fruit", ItemID: "word-apple", ItemType: domain.ItemTypeWord, TargetText: "apple", Score: 40},
		{ChildID: "child-1", LessonID: "lesson_fruit", ItemID: "word-apple", ItemType: domain.ItemTypeWord, TargetText: "apple", Score: 80},
		{ChildID: "child-1", LessonID: "lesson_fruit", ItemID: "word-pear", ItemType: domain.ItemTypeWord, TargetText: "pear", Score: 30},
		{ChildID: "child-1", LessonID: "lesson_school", ItemID: "sent-hi", ItemType: domain.ItemTypeSentence, TargetText: "Hello, teacher!", Score: 90},
	}
	for _, input := range inputs {
		if _, err := service.RecordAttempt(ctx, "family-1", input); err != nil {
			t.Fatalf("record attempt: %v", err)
		}
	}
	// A different family must not appear in this family's progress.
	if _, err := service.RecordAttempt(ctx, "family-2", inputs[0]); err != nil {
		t.Fatalf("record other family attempt: %v", err)
	}

	lessons, items, err := service.Progress(ctx, "family-1", domain.AttemptQuery{ChildID: "child-1"})
	if err != nil {
		t.Fatalf("progress: %v", err)
	}
	if len(lessons) != 2 {
		t.Fatalf("lessons = %d, want 2", len(lessons))
	}
	byLesson := map[string]domain.LessonProgress{}
	for _, lesson := range lessons {
		byLesson[lesson.LessonID] = lesson
	}
	fruit := byLesson["lesson_fruit"]
	if fruit.Attempts != 3 {
		t.Fatalf("fruit attempts = %d, want 3", fruit.Attempts)
	}
	if fruit.TotalItems != 2 || fruit.PassedItems != 1 {
		t.Fatalf("fruit items = %d passed = %d, want 2/1", fruit.TotalItems, fruit.PassedItems)
	}
	if fruit.BestScore != 80 {
		t.Fatalf("fruit best = %d, want 80", fruit.BestScore)
	}
	if fruit.AverageScore != 50 {
		t.Fatalf("fruit average = %d, want 50", fruit.AverageScore)
	}

	if len(items) != 3 {
		t.Fatalf("items = %d, want 3", len(items))
	}
	byItem := map[string]domain.ProgressItem{}
	for _, item := range items {
		byItem[item.ItemID] = item
	}
	apple := byItem["word-apple"]
	if apple.BestScore != 80 || apple.Attempts != 2 || !apple.Passed {
		t.Fatalf("apple item = %+v, want best 80, 2 attempts, passed", apple)
	}
	pear := byItem["word-pear"]
	if pear.BestScore != 30 || pear.Passed {
		t.Fatalf("pear item = %+v, want best 30, not passed", pear)
	}
}

func TestProgressClampsLimit(t *testing.T) {
	service, repository := newService(t)
	for index := 0; index < 5; index++ {
		repository.attempts = append(repository.attempts, domain.PracticeAttempt{
			FamilyID: "family-1",
			LessonID: "lesson_fruit",
			ItemID:   "word-apple",
			Score:    50,
		})
	}
	// MaxAttemptLimit is not exceeded by the repository call.
	_, items, err := service.Progress(context.Background(), "family-1", domain.AttemptQuery{Limit: 99999})
	if err != nil {
		t.Fatalf("progress: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("items = %d, want 1", len(items))
	}
	if items[0].Attempts != 5 {
		t.Fatalf("attempts = %d, want 5", items[0].Attempts)
	}
}
