package service

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"testing"
	"time"

	"github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/english/domain"
	englishrepository "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/english/repository"
)

type fixedClock struct {
	now time.Time
}

func (c fixedClock) Now() time.Time {
	return c.now
}

// memoryRepository is a small in-memory double for the service tests.
type memoryRepository struct {
	lessons          map[string]*domain.Lesson
	attempts         []domain.PracticeAttempt
	listCatalogCalls int
	getLessonCalls   int
	listAttemptCalls int
	createAttemptErr error
}

func newMemoryRepository() *memoryRepository {
	return &memoryRepository{lessons: map[string]*domain.Lesson{}}
}

func (r *memoryRepository) ListLessons(
	_ context.Context,
	ageTier string,
) ([]domain.Lesson, error) {
	return r.listLessons(ageTier, false, 0)
}

func (r *memoryRepository) ListLessonCatalog(
	_ context.Context,
	ageTier string,
	limit int,
) ([]domain.Lesson, error) {
	r.listCatalogCalls++
	return r.listLessons(ageTier, true, limit)
}

func (r *memoryRepository) listLessons(
	ageTier string,
	full bool,
	limit int,
) ([]domain.Lesson, error) {
	result := make([]domain.Lesson, 0, len(r.lessons))
	for _, lesson := range r.lessons {
		if lesson.Status != domain.LessonStatusPublished {
			continue
		}
		if ageTier != "" && lesson.AgeTier != ageTier {
			continue
		}
		lessonCopy := *lesson
		if !full {
			lessonCopy.Words = nil
			lessonCopy.Sentence = nil
		}
		result = append(result, lessonCopy)
	}
	sort.SliceStable(result, func(left, right int) bool {
		if result[left].AgeTier != result[right].AgeTier {
			return result[left].AgeTier < result[right].AgeTier
		}
		if result[left].Sequence != result[right].Sequence {
			return result[left].Sequence < result[right].Sequence
		}
		return result[left].LessonID < result[right].LessonID
	})
	if limit > 0 && len(result) > limit {
		result = result[:limit]
	}
	return result, nil
}

func (r *memoryRepository) GetLesson(
	_ context.Context,
	lessonID string,
) (*domain.Lesson, error) {
	r.getLessonCalls++
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
	if r.createAttemptErr != nil {
		return r.createAttemptErr
	}
	r.attempts = append(r.attempts, *attempt)
	return nil
}

func (r *memoryRepository) ListAttempts(
	_ context.Context,
	familyID string,
	query domain.AttemptQuery,
) ([]domain.PracticeAttempt, error) {
	r.listAttemptCalls++
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

func (r *memoryRepository) ListProgress(
	_ context.Context,
	familyID string,
	query domain.AttemptQuery,
) ([]domain.LessonProgress, []domain.ProgressItem, error) {
	lessons, items := summarize(r.filteredAttempts(familyID, query))
	return lessons, items, nil
}

func (r *memoryRepository) filteredAttempts(
	familyID string,
	query domain.AttemptQuery,
) []domain.PracticeAttempt {
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
	return result
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

func TestLessonCatalogUsesSingleBatchRead(t *testing.T) {
	service, repository := newService(t)
	for index, lessonID := range []string{
		"lesson_fruit",
		"lesson_school",
		"lesson_animals",
	} {
		repository.lessons[lessonID] = &domain.Lesson{
			LessonID: lessonID,
			AgeTier:  domain.AgeTier3To4,
			Status:   domain.LessonStatusPublished,
			Words: []domain.Word{
				{ID: "word-" + lessonID, LessonID: lessonID, Position: 1, Term: "apple"},
			},
			Sentence: []domain.Sentence{
				{ID: "sentence-" + lessonID, LessonID: lessonID, Position: 1, Pattern: "I like ___."},
			},
			Sequence: index + 1,
		}
	}

	lessons, err := service.LessonCatalog(context.Background(), domain.AgeTier3To4)
	if err != nil {
		t.Fatalf("lesson catalog: %v", err)
	}
	if len(lessons) != 3 {
		t.Fatalf("catalog lessons = %d, want 3", len(lessons))
	}
	if repository.listCatalogCalls != 1 {
		t.Fatalf("batch catalog calls = %d, want 1", repository.listCatalogCalls)
	}
	if repository.getLessonCalls != 0 {
		t.Fatalf("per-lesson detail calls = %d, want 0", repository.getLessonCalls)
	}
	for _, lesson := range lessons {
		if len(lesson.Words) != 1 || len(lesson.Sentence) != 1 {
			t.Fatalf("lesson %s missing batch content: %+v", lesson.LessonID, lesson)
		}
	}
}

// legacyLessonRepository intentionally hides the optional batch catalogue
// method, proving the original Repository consumers still compile and work.
type legacyLessonRepository struct {
	englishrepository.Repository
}

func TestLessonCatalogFallsBackToLegacyRepository(t *testing.T) {
	store := newMemoryRepository()
	store.lessons["lesson_fruit"] = &domain.Lesson{
		LessonID: "lesson_fruit",
		AgeTier:  domain.AgeTier3To4,
		Status:   domain.LessonStatusPublished,
		Words: []domain.Word{
			{ID: "word-apple", LessonID: "lesson_fruit", Position: 1, Term: "apple"},
		},
		Sentence: []domain.Sentence{
			{
				ID:          "sentence-apple",
				LessonID:    "lesson_fruit",
				Position:    1,
				Pattern:     "I like ___.",
				Prompt:      "说出你喜欢苹果",
				ModelAnswer: "I like apples.",
			},
		},
	}
	service, err := New(Options{
		Repository: legacyLessonRepository{Repository: store},
		Clock: fixedClock{
			now: time.Date(2026, 10, 18, 8, 0, 0, 0, time.UTC),
		},
	})
	if err != nil {
		t.Fatalf("create english service: %v", err)
	}

	lessons, err := service.LessonCatalog(context.Background(), domain.AgeTier3To4)
	if err != nil {
		t.Fatalf("legacy lesson catalog: %v", err)
	}
	if len(lessons) != 1 {
		t.Fatalf("legacy catalog lessons = %d, want 1", len(lessons))
	}
	if store.getLessonCalls != 1 {
		t.Fatalf("legacy detail calls = %d, want 1", store.getLessonCalls)
	}
	if len(lessons[0].Words) != 1 ||
		len(lessons[0].Sentence) != 1 ||
		lessons[0].Sentence[0].ModelAnswer != "I like apples." {
		t.Fatalf("legacy catalog lost lesson content: %+v", lessons[0])
	}
}

func TestLessonCatalogCapsResultsAndKeepsStableOrder(t *testing.T) {
	service, repository := newService(t)
	base := time.Date(2026, 10, 18, 8, 0, 0, 0, time.UTC)
	for index := 0; index < 105; index++ {
		lessonID := fmt.Sprintf("lesson_%03d", index)
		repository.lessons[lessonID] = &domain.Lesson{
			LessonID:  lessonID,
			AgeTier:   domain.AgeTier3To4,
			Sequence:  105 - index,
			Status:    domain.LessonStatusPublished,
			CreatedAt: base,
			UpdatedAt: base,
		}
	}

	lessons, err := service.LessonCatalog(context.Background(), domain.AgeTier3To4)
	if err != nil {
		t.Fatalf("lesson catalog: %v", err)
	}
	if len(lessons) != 100 {
		t.Fatalf("lesson catalog length = %d, want 100", len(lessons))
	}
	if lessons[0].LessonID != "lesson_104" || lessons[99].LessonID != "lesson_005" {
		t.Fatalf("unexpected stable order: %s..%s", lessons[0].LessonID, lessons[99].LessonID)
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

func TestRecordAttemptReturnsStableDataIntegrityErrors(t *testing.T) {
	service, repository := newService(t)
	repository.createAttemptErr = domain.ErrChildFamilyMismatch
	_, err := service.RecordAttempt(
		context.Background(),
		"family-1",
		domain.AttemptInput{
			ChildID:    "child-2",
			LessonID:   "lesson_fruit",
			ItemID:     "word-apple",
			ItemType:   domain.ItemTypeWord,
			TargetText: "apple",
		},
	)
	if !errors.Is(err, domain.ErrChildFamilyMismatch) {
		t.Fatalf("error = %v, want ErrChildFamilyMismatch", err)
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
	// Progress is a full aggregate, not a bounded browse. The explicit limit
	// must not be forwarded to the repository.
	lessons, items, err := service.Progress(context.Background(), "family-1", domain.AttemptQuery{Limit: 99999})
	if err != nil {
		t.Fatalf("progress: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("items = %d, want 1", len(items))
	}
	if items[0].Attempts != 5 {
		t.Fatalf("attempts = %d, want 5", items[0].Attempts)
	}
	if len(lessons) != 1 || lessons[0].Attempts != 5 {
		t.Fatalf("lessons = %+v, want 5 attempts", lessons)
	}
}

func TestProgressUsesFullAggregateWithoutBrowsingLimit(t *testing.T) {
	service, repository := newService(t)
	base := time.Date(2026, 10, 11, 9, 0, 0, 0, time.UTC)
	for index := 0; index < MaxAttemptLimit+257; index++ {
		repository.attempts = append(repository.attempts, domain.PracticeAttempt{
			FamilyID:    "family-1",
			ChildID:     "child-1",
			LessonID:    "lesson_fruit",
			ItemID:      "word-apple",
			ItemType:    domain.ItemTypeWord,
			TargetText:  "apple",
			Score:       index % 101,
			Passed:      index%101 >= domain.PassingScore,
			PracticedAt: base.Add(time.Duration(index) * time.Second),
		})
	}

	lessons, items, err := service.Progress(
		context.Background(),
		"family-1",
		domain.AttemptQuery{Limit: 1},
	)
	if err != nil {
		t.Fatalf("progress: %v", err)
	}
	if repository.listAttemptCalls != 0 {
		t.Fatalf("progress fell back to bounded attempts: %d calls", repository.listAttemptCalls)
	}
	if len(lessons) != 1 || lessons[0].Attempts != MaxAttemptLimit+257 {
		t.Fatalf("lessons = %+v, want full attempt count", lessons)
	}
	if len(items) != 1 || items[0].Attempts != MaxAttemptLimit+257 {
		t.Fatalf("items = %+v, want full attempt count", items)
	}
}

func TestProgressKeepsFamilyAndChildFilters(t *testing.T) {
	service, repository := newService(t)
	repository.attempts = []domain.PracticeAttempt{
		{FamilyID: "family-1", ChildID: "child-1", LessonID: "lesson_fruit", ItemID: "word-apple", Score: 80, Passed: true},
		{FamilyID: "family-1", ChildID: "child-2", LessonID: "lesson_fruit", ItemID: "word-pear", Score: 30},
		{FamilyID: "family-2", ChildID: "child-1", LessonID: "lesson_fruit", ItemID: "word-apple", Score: 100, Passed: true},
	}

	_, items, err := service.Progress(
		context.Background(),
		"family-1",
		domain.AttemptQuery{ChildID: "child-1"},
	)
	if err != nil {
		t.Fatalf("progress: %v", err)
	}
	if len(items) != 1 || items[0].ItemID != "word-apple" || items[0].Attempts != 1 {
		t.Fatalf("items = %+v, want only family-1 child-1", items)
	}
}

func TestRecordAttemptRejectsBlankFamily(t *testing.T) {
	service, _ := newService(t)
	_, err := service.RecordAttempt(
		context.Background(),
		"  ",
		domain.AttemptInput{
			LessonID:   "lesson_fruit",
			ItemID:     "word-apple",
			ItemType:   domain.ItemTypeWord,
			TargetText: "apple",
		},
	)
	if !errors.Is(err, domain.ErrInvalidAttempt) {
		t.Fatalf("error = %v, want ErrInvalidAttempt", err)
	}
}

// legacyProgressRepository hides ListProgress so the service falls back to the
// original bounded attempt reader for older adapters.
type legacyProgressRepository struct {
	englishrepository.Repository
}

func TestProgressKeepsLegacyRepositoryCompatible(t *testing.T) {
	store := newMemoryRepository()
	store.attempts = []domain.PracticeAttempt{{
		FamilyID:   "family-1",
		ChildID:    "child-1",
		LessonID:   "lesson_fruit",
		ItemID:     "word-apple",
		ItemType:   domain.ItemTypeWord,
		Score:      75,
		Passed:     true,
		TargetText: "apple",
	}}
	service, err := New(Options{
		Repository: legacyProgressRepository{Repository: store},
		Clock:      fixedClock{now: time.Date(2026, 10, 18, 8, 0, 0, 0, time.UTC)},
	})
	if err != nil {
		t.Fatalf("create english service: %v", err)
	}

	lessons, items, err := service.Progress(context.Background(), "family-1", domain.AttemptQuery{})
	if err != nil {
		t.Fatalf("legacy progress: %v", err)
	}
	if len(lessons) != 1 || len(items) != 1 {
		t.Fatalf("legacy progress = %+v / %+v", lessons, items)
	}
}

func TestRecordAttemptAcceptsMissingLessonWhenRepositoryRejects(t *testing.T) {
	service, repository := newService(t)
	repository.createAttemptErr = domain.ErrLessonNotFound
	_, err := service.RecordAttempt(
		context.Background(),
		"family-1",
		domain.AttemptInput{
			LessonID:   "lesson_missing",
			ItemID:     "word-apple",
			ItemType:   domain.ItemTypeWord,
			TargetText: "apple",
		},
	)
	if !errors.Is(err, domain.ErrLessonNotFound) {
		t.Fatalf("error = %v, want ErrLessonNotFound", err)
	}
}

func TestProgressReadsAllAttemptsWhenBrowsingLimitIsExplicit(t *testing.T) {
	service, repository := newService(t)
	for index := 0; index < MaxAttemptLimit+1; index++ {
		repository.attempts = append(repository.attempts, domain.PracticeAttempt{
			FamilyID: "family-1",
			LessonID: "lesson_fruit",
			ItemID:   "word-apple",
			ItemType: domain.ItemTypeWord,
			Score:    60,
			Passed:   true,
		})
	}

	lessons, items, err := service.Progress(
		context.Background(),
		"family-1",
		domain.AttemptQuery{Limit: 1},
	)
	if err != nil {
		t.Fatalf("progress: %v", err)
	}
	if len(lessons) != 1 || lessons[0].Attempts != MaxAttemptLimit+1 {
		t.Fatalf("lessons = %+v, want all attempts", lessons)
	}
	if len(items) != 1 || items[0].Attempts != MaxAttemptLimit+1 {
		t.Fatalf("items = %+v, want all attempts", items)
	}
}

func TestLegacyProgressFallbackCannotHonorBrowsingLimit(t *testing.T) {
	store := newMemoryRepository()
	for index := 0; index < MaxAttemptLimit+1; index++ {
		store.attempts = append(store.attempts, domain.PracticeAttempt{
			FamilyID:   "family-1",
			LessonID:   "lesson_fruit",
			ItemID:     "word-apple",
			ItemType:   domain.ItemTypeWord,
			TargetText: "apple",
			Score:      60,
			Passed:     true,
		})
	}
	service, err := New(Options{
		Repository: legacyProgressRepository{Repository: store},
		Clock:      fixedClock{now: time.Date(2026, 10, 18, 8, 0, 0, 0, time.UTC)},
	})
	if err != nil {
		t.Fatalf("create english service: %v", err)
	}

	lessons, _, err := service.Progress(
		context.Background(),
		"family-1",
		domain.AttemptQuery{Limit: 1},
	)
	if err != nil {
		t.Fatalf("legacy progress: %v", err)
	}
	if len(lessons) != 1 || lessons[0].Attempts != MaxAttemptLimit+1 {
		t.Fatalf("legacy aggregate = %+v, want full attempt count", lessons)
	}
}
