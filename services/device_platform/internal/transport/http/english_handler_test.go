package http

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	englishdomain "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/english/domain"
	englishservice "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/english/service"
)

// englishTestRepository is an in-memory double so the handler tests exercise
// the real service validation without a database.
type englishTestRepository struct {
	lessons                map[string]*englishdomain.Lesson
	attempts               []englishdomain.PracticeAttempt
	omitContentOnListQuery bool
}

func newEnglishTestRepository() *englishTestRepository {
	return &englishTestRepository{lessons: map[string]*englishdomain.Lesson{}}
}

func (r *englishTestRepository) ListLessons(
	_ context.Context,
	ageTier string,
) ([]englishdomain.Lesson, error) {
	result := make([]englishdomain.Lesson, 0, len(r.lessons))
	for _, lesson := range r.lessons {
		if lesson.Status != englishdomain.LessonStatusPublished {
			continue
		}
		if ageTier != "" && lesson.AgeTier != ageTier {
			continue
		}
		lessonCopy := *lesson
		if r.omitContentOnListQuery {
			lessonCopy.Words = nil
			lessonCopy.Sentence = nil
		}
		result = append(result, lessonCopy)
	}
	return result, nil
}

func (r *englishTestRepository) GetLesson(
	_ context.Context,
	lessonID string,
) (*englishdomain.Lesson, error) {
	lesson, ok := r.lessons[lessonID]
	if !ok {
		return nil, englishdomain.ErrLessonNotFound
	}
	copyLesson := *lesson
	return &copyLesson, nil
}

func (r *englishTestRepository) CreateAttempt(
	_ context.Context,
	attempt *englishdomain.PracticeAttempt,
) error {
	r.attempts = append(r.attempts, *attempt)
	return nil
}

func (r *englishTestRepository) ListAttempts(
	_ context.Context,
	familyID string,
	query englishdomain.AttemptQuery,
) ([]englishdomain.PracticeAttempt, error) {
	result := make([]englishdomain.PracticeAttempt, 0, len(r.attempts))
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

func newEnglishHandlerForTest(t *testing.T) (englishHandler, *englishTestRepository) {
	t.Helper()
	repository := newEnglishTestRepository()
	service, err := englishservice.New(englishservice.Options{
		Repository: repository,
		Clock:      englishTestClock{},
	})
	if err != nil {
		t.Fatalf("create english service: %v", err)
	}
	return englishHandler{service: service}, repository
}

type englishTestBindingService struct {
	sessionDeviceID string
	err             error
}

func (service englishTestBindingService) VerifyDeviceSession(
	_ context.Context,
	_ string,
) (string, error) {
	if service.err != nil {
		return "", service.err
	}
	return service.sessionDeviceID, nil
}

type englishTestRuntimeService struct {
	familyID string
	err      error
}

func (service englishTestRuntimeService) ResolveDeviceFamily(
	_ context.Context,
	_ string,
	_ string,
) (string, error) {
	if service.err != nil {
		return "", service.err
	}
	return service.familyID, nil
}

type englishTestClock struct{}

func (englishTestClock) Now() time.Time {
	return time.Date(2026, 10, 11, 9, 0, 0, 0, time.UTC)
}

func englishRequest(method string, target string, body string, accountID string) *http.Request {
	var request *http.Request
	if body == "" {
		request = httptest.NewRequest(method, target, nil)
	} else {
		request = httptest.NewRequest(method, target, strings.NewReader(body))
	}
	if accountID == "" {
		return request
	}
	return withOwnerFamilyContext(request, accountID)
}

func TestEnglishListLessonsRequiresAuthentication(t *testing.T) {
	handler, _ := newEnglishHandlerForTest(t)
	request := englishRequest(http.MethodGet, "/api/v1/english/lessons", "", "")
	recorder := httptest.NewRecorder()
	handler.listLessons(recorder, request)
	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusUnauthorized)
	}
}

func TestEnglishListLessonsReturnsSummaries(t *testing.T) {
	handler, repository := newEnglishHandlerForTest(t)
	repository.lessons["lesson_fruit"] = &englishdomain.Lesson{
		LessonID: "lesson_fruit",
		AgeTier:  englishdomain.AgeTier3To4,
		Title:    "水果",
		Status:   englishdomain.LessonStatusPublished,
		Words: []englishdomain.Word{
			{ID: "word-apple", Term: "apple", Translation: "苹果"},
		},
	}

	request := englishRequest(
		http.MethodGet,
		"/api/v1/english/lessons?age_tier=age_3_4",
		"",
		"family-1",
	)
	recorder := httptest.NewRecorder()
	handler.listLessons(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d: %s",
			recorder.Code, http.StatusOK, recorder.Body.String())
	}
	var envelope struct {
		Data struct {
			Lessons []struct {
				LessonID  string `json:"lesson_id"`
				WordCount int    `json:"word_count"`
			} `json:"lessons"`
		} `json:"data"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(envelope.Data.Lessons) != 1 {
		t.Fatalf("lessons = %d, want 1", len(envelope.Data.Lessons))
	}
	if envelope.Data.Lessons[0].LessonID != "lesson_fruit" ||
		envelope.Data.Lessons[0].WordCount != 1 {
		t.Fatalf("unexpected lesson: %+v", envelope.Data.Lessons[0])
	}
}

func TestEnglishGetLessonReturnsDetail(t *testing.T) {
	handler, repository := newEnglishHandlerForTest(t)
	repository.lessons["lesson_fruit"] = &englishdomain.Lesson{
		LessonID: "lesson_fruit",
		AgeTier:  englishdomain.AgeTier3To4,
		Title:    "水果",
		Status:   englishdomain.LessonStatusPublished,
		Words: []englishdomain.Word{
			{ID: "word-apple", Position: 1, Term: "apple", Translation: "苹果"},
		},
		Sentence: []englishdomain.Sentence{
			{
				ID:          "sent-like",
				Position:    1,
				Pattern:     "I like ___.",
				Prompt:      "说一句你喜欢的水果",
				ModelAnswer: "I like apples.",
			},
		},
	}
	request := englishRequest(http.MethodGet, "/api/v1/english/lessons/lesson_fruit", "", "family-1")
	request.SetPathValue("lesson_id", "lesson_fruit")
	recorder := httptest.NewRecorder()
	handler.getLesson(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d: %s",
			recorder.Code, http.StatusOK, recorder.Body.String())
	}
	var envelope struct {
		Data struct {
			Lesson struct {
				Words     []map[string]any `json:"words"`
				Sentences []map[string]any `json:"sentences"`
			} `json:"lesson"`
		} `json:"data"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(envelope.Data.Lesson.Words) != 1 || len(envelope.Data.Lesson.Sentences) != 1 {
		t.Fatalf("unexpected lesson detail: %s", recorder.Body.String())
	}
}

func TestEnglishGetLessonNotFound(t *testing.T) {
	handler, _ := newEnglishHandlerForTest(t)
	request := englishRequest(http.MethodGet, "/api/v1/english/lessons/lesson_missing", "", "family-1")
	request.SetPathValue("lesson_id", "lesson_missing")
	recorder := httptest.NewRecorder()
	handler.getLesson(recorder, request)
	if recorder.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusNotFound)
	}
}

func TestDeviceEnglishLessonCatalogReturnsFullContent(t *testing.T) {
	handler, repository := newEnglishHandlerForTest(t)
	repository.omitContentOnListQuery = true
	repository.lessons["lesson_fruit"] = &englishdomain.Lesson{
		LessonID: "lesson_fruit",
		AgeTier:  englishdomain.AgeTier3To4,
		Title:    "水果",
		Status:   englishdomain.LessonStatusPublished,
		Words: []englishdomain.Word{
			{ID: "word-apple", Position: 1, Term: "apple", Translation: "苹果"},
		},
		Sentence: []englishdomain.Sentence{
			{
				ID:          "sent-like",
				Position:    1,
				Pattern:     "I like ___.  ",
				Prompt:      "说一句你喜欢的水果",
				ModelAnswer: "I like apples.",
			},
		},
	}
	deviceHandler := deviceEnglishHandler{
		englishHandler: &handler,
		bindingService: englishTestBindingService{
			sessionDeviceID: "sprout_device_001",
		},
		runtimeService: englishTestRuntimeService{familyID: "family-001"},
	}
	request := httptest.NewRequest(
		http.MethodGet,
		"/api/v1/devices/sprout_device_001/english/lessons?age_tier=age_3_4",
		nil,
	)
	request.SetPathValue("device_id", "sprout_device_001")
	request.Header.Set("Authorization", "Bearer device-session-token")
	recorder := httptest.NewRecorder()
	deviceHandler.listLessons(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d: %s",
			recorder.Code, http.StatusOK, recorder.Body.String())
	}
	var envelope struct {
		Data struct {
			Lessons []struct {
				LessonID  string           `json:"lesson_id"`
				Words     []map[string]any `json:"words"`
				Sentences []map[string]any `json:"sentences"`
			} `json:"lessons"`
		} `json:"data"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(envelope.Data.Lessons) != 1 ||
		envelope.Data.Lessons[0].LessonID != "lesson_fruit" {
		t.Fatalf("unexpected lesson catalog: %s", recorder.Body.String())
	}
	if len(envelope.Data.Lessons[0].Words) != 1 ||
		len(envelope.Data.Lessons[0].Sentences) != 1 {
		t.Fatalf("catalog did not include full lesson content: %s", recorder.Body.String())
	}
}

func TestDeviceEnglishLessonCatalogRejectsSessionDeviceMismatch(t *testing.T) {
	handler, _ := newEnglishHandlerForTest(t)
	deviceHandler := deviceEnglishHandler{
		englishHandler: &handler,
		bindingService: englishTestBindingService{
			sessionDeviceID: "sprout_device_other",
		},
		runtimeService: englishTestRuntimeService{familyID: "family-001"},
	}
	request := httptest.NewRequest(
		http.MethodGet,
		"/api/v1/devices/sprout_device_001/english/lessons",
		nil,
	)
	request.SetPathValue("device_id", "sprout_device_001")
	request.Header.Set("Authorization", "Bearer device-session-token")
	recorder := httptest.NewRecorder()
	deviceHandler.listLessons(recorder, request)
	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d: %s",
			recorder.Code, http.StatusUnauthorized, recorder.Body.String())
	}
}

func TestDeviceEnglishRecordAttemptUsesVerifiedFamily(t *testing.T) {
	handler, repository := newEnglishHandlerForTest(t)
	deviceHandler := deviceEnglishHandler{
		englishHandler: &handler,
		bindingService: englishTestBindingService{
			sessionDeviceID: "sprout_device_001",
		},
		runtimeService: englishTestRuntimeService{familyID: "family-001"},
	}
	request := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/devices/sprout_device_001/english/attempts",
		strings.NewReader(`{
			"child_id": "child_001",
			"lesson_id": "lesson_fruit",
			"item_id": "word-apple",
			"item_type": "word",
			"target_text": "apple",
			"recognized_text": "apple",
			"score": 140
		}`),
	)
	request.SetPathValue("device_id", "sprout_device_001")
	request.Header.Set("Authorization", "Bearer device-session-token")
	recorder := httptest.NewRecorder()
	deviceHandler.recordAttempt(recorder, request)
	if recorder.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d: %s",
			recorder.Code, http.StatusCreated, recorder.Body.String())
	}
	if len(repository.attempts) != 1 {
		t.Fatalf("attempts = %d, want 1", len(repository.attempts))
	}
	attempt := repository.attempts[0]
	if attempt.FamilyID != "family-001" ||
		attempt.Score != 100 ||
		!attempt.Passed {
		t.Fatalf("unexpected attempt: %+v", attempt)
	}
}

func TestDeviceEnglishRecordAttemptRejectsSessionDeviceMismatch(t *testing.T) {
	handler, repository := newEnglishHandlerForTest(t)
	deviceHandler := deviceEnglishHandler{
		englishHandler: &handler,
		bindingService: englishTestBindingService{
			sessionDeviceID: "sprout_device_other",
		},
		runtimeService: englishTestRuntimeService{familyID: "family-001"},
	}
	request := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/devices/sprout_device_001/english/attempts",
		strings.NewReader(`{
			"lesson_id": "lesson_fruit",
			"item_id": "word-apple",
			"item_type": "word",
			"target_text": "apple",
			"score": 90
		}`),
	)
	request.SetPathValue("device_id", "sprout_device_001")
	request.Header.Set("Authorization", "Bearer device-session-token")
	recorder := httptest.NewRecorder()
	deviceHandler.recordAttempt(recorder, request)
	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d: %s",
			recorder.Code, http.StatusUnauthorized, recorder.Body.String())
	}
	if len(repository.attempts) != 0 {
		t.Fatalf("attempts = %d, want none", len(repository.attempts))
	}
}

func TestEnglishListAttemptsIsFamilyScoped(t *testing.T) {
	handler, repository := newEnglishHandlerForTest(t)
	repository.attempts = []englishdomain.PracticeAttempt{
		{
			ID:             "attempt-1",
			FamilyID:       "family-1",
			ChildID:        "child-1",
			LessonID:       "lesson_fruit",
			ItemID:         "word-apple",
			ItemType:       englishdomain.ItemTypeWord,
			TargetText:     "apple",
			RecognizedText: "apple",
			Score:          90,
			Passed:         true,
			PracticedAt:    time.Date(2026, 10, 11, 9, 0, 0, 0, time.UTC),
		},
		{
			ID:          "attempt-2",
			FamilyID:    "family-2",
			ChildID:     "child-9",
			LessonID:    "lesson_fruit",
			ItemID:      "word-apple",
			ItemType:    englishdomain.ItemTypeWord,
			TargetText:  "apple",
			Score:       40,
			PracticedAt: time.Date(2026, 10, 11, 9, 0, 0, 0, time.UTC),
		},
	}
	request := englishRequest(http.MethodGet, "/api/v1/english/attempts", "", "family-1")
	recorder := httptest.NewRecorder()
	handler.listAttempts(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusOK)
	}
	var envelope struct {
		Data struct {
			Attempts []map[string]any `json:"attempts"`
		} `json:"data"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(envelope.Data.Attempts) != 1 {
		t.Fatalf("attempts = %d, want family-1 only", len(envelope.Data.Attempts))
	}
}

func TestEnglishProgressReturnsRollup(t *testing.T) {
	handler, repository := newEnglishHandlerForTest(t)
	repository.attempts = []englishdomain.PracticeAttempt{
		{FamilyID: "family-1", ChildID: "child-1", LessonID: "lesson_fruit", ItemID: "word-apple", ItemType: englishdomain.ItemTypeWord, TargetText: "apple", Score: 40, PracticedAt: time.Date(2026, 10, 11, 8, 0, 0, 0, time.UTC)},
		{FamilyID: "family-1", ChildID: "child-1", LessonID: "lesson_fruit", ItemID: "word-apple", ItemType: englishdomain.ItemTypeWord, TargetText: "apple", Score: 80, Passed: true, PracticedAt: time.Date(2026, 10, 11, 9, 0, 0, 0, time.UTC)},
	}
	request := englishRequest(http.MethodGet, "/api/v1/english/progress?child_id=child-1", "", "family-1")
	recorder := httptest.NewRecorder()
	handler.listProgress(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d: %s",
			recorder.Code, http.StatusOK, recorder.Body.String())
	}
	var envelope struct {
		Data struct {
			LessonProgress []struct {
				LessonID   string `json:"lesson_id"`
				BestScore  int    `json:"best_score"`
				TotalItems int    `json:"total_items"`
			} `json:"lesson_progress"`
			ItemProgress []struct {
				ItemID    string `json:"item_id"`
				BestScore int    `json:"best_score"`
				Passed    bool   `json:"passed"`
			} `json:"item_progress"`
		} `json:"data"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(envelope.Data.LessonProgress) != 1 ||
		envelope.Data.LessonProgress[0].BestScore != 80 {
		t.Fatalf("unexpected lesson progress: %s", recorder.Body.String())
	}
	if len(envelope.Data.ItemProgress) != 1 ||
		!envelope.Data.ItemProgress[0].Passed {
		t.Fatalf("unexpected item progress: %s", recorder.Body.String())
	}
}
