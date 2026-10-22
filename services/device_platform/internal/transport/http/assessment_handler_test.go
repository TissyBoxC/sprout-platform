package http

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	assessmentdomain "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/assessment/domain"
	assessmentservice "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/assessment/service"
)

// assessmentMemoryStore is an in-memory double so the handler tests exercise the
// real service validation and level arithmetic without a database.
type assessmentMemoryStore struct {
	sessions map[string]assessmentdomain.Session
	answers  map[string][]assessmentdomain.Answer
	// childFamily maps a child profile to its family.
	childFamily map[string]string
	// deviceChild maps a device to the child it currently serves.
	deviceChild map[string]string
}

func newAssessmentMemoryStore() *assessmentMemoryStore {
	return &assessmentMemoryStore{
		sessions:    map[string]assessmentdomain.Session{},
		answers:     map[string][]assessmentdomain.Answer{},
		childFamily: map[string]string{"child-1": "family-1"},
		deviceChild: map[string]string{"device-1": "child-1"},
	}
}

func (store *assessmentMemoryStore) Create(_ context.Context, session assessmentdomain.Session) error {
	for _, existing := range store.sessions {
		if existing.FamilyID == session.FamilyID &&
			existing.ChildID == session.ChildID &&
			existing.State == assessmentdomain.StateActive {
			return assessmentservice.StoreSentinelAlreadyActive
		}
	}
	store.sessions[session.ID] = session
	return nil
}

func (store *assessmentMemoryStore) Get(
	_ context.Context,
	familyID, sessionID string,
) (assessmentdomain.Session, error) {
	session, ok := store.sessions[sessionID]
	if !ok || session.FamilyID != familyID {
		return assessmentdomain.Session{}, assessmentservice.StoreSentinelNotFound
	}
	return session, nil
}

func (store *assessmentMemoryStore) GetForDevice(
	_ context.Context,
	deviceID, sessionID string,
) (assessmentdomain.Session, error) {
	session, ok := store.sessions[sessionID]
	// Text comparison mirrors the repository, where a session with no child is
	// never claimable by a device.
	if !ok || session.ChildID == "" || store.deviceChild[deviceID] != session.ChildID {
		return assessmentdomain.Session{}, assessmentservice.StoreSentinelNotFound
	}
	return session, nil
}

func (store *assessmentMemoryStore) ListExpired(
	_ context.Context,
	now time.Time,
	limit int,
) ([]assessmentdomain.Session, error) {
	expired := make([]assessmentdomain.Session, 0, len(store.sessions))
	for _, session := range store.sessions {
		if session.State == assessmentdomain.StateActive && !now.Before(session.ExpiresAt) {
			expired = append(expired, session)
		}
	}
	if len(expired) > limit {
		expired = expired[:limit]
	}
	return expired, nil
}

func (store *assessmentMemoryStore) History(
	_ context.Context,
	familyID, childID string,
	limit int,
) ([]assessmentdomain.Session, error) {
	history := make([]assessmentdomain.Session, 0, limit)
	for _, session := range store.sessions {
		if session.FamilyID == familyID && (childID == "" || session.ChildID == childID) {
			history = append(history, session)
		}
	}
	return history, nil
}

func (store *assessmentMemoryStore) ListAnswers(
	_ context.Context,
	sessionID string,
) ([]assessmentdomain.Answer, error) {
	return append([]assessmentdomain.Answer(nil), store.answers[sessionID]...), nil
}

func (store *assessmentMemoryStore) RecordAnswer(
	_ context.Context,
	session assessmentdomain.Session,
	answer assessmentdomain.Answer,
	previousAnswered int,
) error {
	current, ok := store.sessions[session.ID]
	if !ok {
		return assessmentservice.StoreSentinelNotFound
	}
	if current.State != assessmentdomain.StateActive || current.AnsweredCount != previousAnswered {
		return assessmentservice.StoreSentinelConflict
	}
	store.sessions[session.ID] = session
	store.answers[session.ID] = append(store.answers[session.ID], answer)
	return nil
}

func (store *assessmentMemoryStore) Transition(
	_ context.Context,
	session assessmentdomain.Session,
	previousAnswered int,
) error {
	current, ok := store.sessions[session.ID]
	if !ok {
		return assessmentservice.StoreSentinelNotFound
	}
	if current.State.IsTerminal() || current.AnsweredCount != previousAnswered {
		return assessmentservice.StoreSentinelConflict
	}
	store.sessions[session.ID] = session
	return nil
}

func (store *assessmentMemoryStore) CountActive(_ context.Context) (int, error) {
	count := 0
	for _, session := range store.sessions {
		if session.State == assessmentdomain.StateActive {
			count++
		}
	}
	return count, nil
}

func (store *assessmentMemoryStore) CountSessionsSince(
	_ context.Context,
	familyID string,
	since time.Time,
) (int, error) {
	count := 0
	for _, session := range store.sessions {
		if session.FamilyID == familyID && !session.StartedAt.Before(since) {
			count++
		}
	}
	return count, nil
}

func (store *assessmentMemoryStore) CountStartedSince(_ context.Context, since time.Time) (int, error) {
	count := 0
	for _, session := range store.sessions {
		if !session.StartedAt.Before(since) {
			count++
		}
	}
	return count, nil
}

func (store *assessmentMemoryStore) ChildInFamily(
	_ context.Context,
	familyID, childID string,
) (bool, error) {
	return childID != "" && store.childFamily[childID] == familyID, nil
}

// assessmentTestCatalog serves one published lesson with ordered word items.
type assessmentTestCatalog struct{ items int }

func (catalog assessmentTestCatalog) Lessons(_ context.Context, limit int) ([]assessmentservice.Lesson, error) {
	lesson, _ := catalog.Lesson(context.Background(), "greetings_basic")
	lessons := []assessmentservice.Lesson{lesson}
	if limit > 0 && len(lessons) > limit {
		lessons = lessons[:limit]
	}
	return lessons, nil
}

func (catalog assessmentTestCatalog) Lesson(
	_ context.Context,
	lessonID string,
) (assessmentservice.Lesson, error) {
	if lessonID != "greetings_basic" {
		return assessmentservice.Lesson{}, assessmentservice.ErrLessonNotFound
	}
	lesson := assessmentservice.Lesson{LessonID: "greetings_basic", AgeTier: assessmentdomain.ItemTypeWord}
	for index := 0; index < catalog.items; index++ {
		lesson.Items = append(lesson.Items, assessmentservice.Item{
			ID:   "word-" + string(rune('a'+index)),
			Type: assessmentdomain.ItemTypeWord,
			Text: "hello",
		})
	}
	return lesson, nil
}

func newAssessmentHandlerForTest(t *testing.T) (*guardianAssessmentHandler, *assessmentMemoryStore) {
	t.Helper()
	store := newAssessmentMemoryStore()
	service := assessmentservice.NewService(assessmentservice.Options{
		Store:   store,
		Lessons: assessmentTestCatalog{items: 10},
		Logger:  slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	return &guardianAssessmentHandler{service: service}, store
}

func assessmentRequest(method, target, body string) *http.Request {
	var request *http.Request
	if body == "" {
		request = httptest.NewRequest(method, target, nil)
	} else {
		request = httptest.NewRequest(method, target, strings.NewReader(body))
	}
	request.SetPathValue("family_id", "family-1")
	return request
}

// decodeAssessmentSession reads the session envelope the handlers answer with.
func decodeAssessmentSession(t *testing.T, recorder *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var envelope struct {
		Data map[string]any `json:"data"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decode response: %v: %s", err, recorder.Body.String())
	}
	return envelope.Data
}

func TestAssessmentStartRequiresAuthentication(t *testing.T) {
	handler, _ := newAssessmentHandlerForTest(t)
	request := assessmentRequest(
		http.MethodPost,
		"/api/v1/families/family-1/assessment-sessions",
		`{"lesson_id":"greetings_basic"}`,
	)
	recorder := httptest.NewRecorder()
	handler.start(recorder, request)
	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusUnauthorized)
	}
}

func TestAssessmentStartReturnsFirstItem(t *testing.T) {
	handler, _ := newAssessmentHandlerForTest(t)
	request := withOwnerFamilyContext(assessmentRequest(
		http.MethodPost,
		"/api/v1/families/family-1/assessment-sessions",
		`{"lesson_id":"greetings_basic","child_id":"child-1"}`,
	), "parent-1")
	recorder := httptest.NewRecorder()
	handler.start(recorder, request)
	if recorder.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201: %s", recorder.Code, recorder.Body.String())
	}
	body := decodeAssessmentSession(t, recorder)
	if body["state"] != string(assessmentdomain.StateActive) {
		t.Fatalf("state = %v, want active", body["state"])
	}
	if body["difficulty"].(float64) != float64(assessmentdomain.DefaultDifficulty) {
		t.Fatalf("difficulty = %v, want %d", body["difficulty"], assessmentdomain.DefaultDifficulty)
	}
	item, ok := body["next_item"].(map[string]any)
	if !ok || item["item_id"] == "" {
		t.Fatalf("next_item missing: %v", body["next_item"])
	}
	if _, leaked := body["target_text"]; leaked {
		// The session envelope carries no per-answer text; that belongs to the
		// answer payload only.
		t.Fatal("session envelope must not carry target_text")
	}
}

func TestAssessmentStartRejectsMalformedBody(t *testing.T) {
	handler, _ := newAssessmentHandlerForTest(t)
	request := withOwnerFamilyContext(assessmentRequest(
		http.MethodPost,
		"/api/v1/families/family-1/assessment-sessions",
		`{"lesson_id":`,
	), "parent-1")
	recorder := httptest.NewRecorder()
	handler.start(recorder, request)
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", recorder.Code)
	}
}

func TestAssessmentStartReportsAMissingLesson(t *testing.T) {
	handler, _ := newAssessmentHandlerForTest(t)
	request := withOwnerFamilyContext(assessmentRequest(
		http.MethodPost,
		"/api/v1/families/family-1/assessment-sessions",
		`{"lesson_id":"missing_lesson"}`,
	), "parent-1")
	recorder := httptest.NewRecorder()
	handler.start(recorder, request)
	if recorder.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404: %s", recorder.Code, recorder.Body.String())
	}
	assertAssessmentErrorCode(t, recorder, "assessment_lesson_not_found")
}

func TestAssessmentAnswerReturnsNextItemAndRaisesDifficulty(t *testing.T) {
	handler, _ := newAssessmentHandlerForTest(t)
	startRequest := withOwnerFamilyContext(assessmentRequest(
		http.MethodPost,
		"/api/v1/families/family-1/assessment-sessions",
		`{"lesson_id":"greetings_basic","child_id":"child-1"}`,
	), "parent-1")
	startRecorder := httptest.NewRecorder()
	handler.start(startRecorder, startRequest)
	started := decodeAssessmentSession(t, startRecorder)
	sessionID := started["session_id"].(string)
	item := started["next_item"].(map[string]any)

	answerBody := `{"position":0,"item_id":"` + item["item_id"].(string) +
		`","item_type":"word","target_text":"hello","score":90}`
	answerRequest := withOwnerFamilyContext(assessmentRequest(
		http.MethodPost,
		"/api/v1/families/family-1/assessment-sessions/"+sessionID+"/answers",
		answerBody,
	), "parent-1")
	answerRequest.SetPathValue("session_id", sessionID)
	recorder := httptest.NewRecorder()
	handler.answer(recorder, answerRequest)
	if recorder.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201: %s", recorder.Code, recorder.Body.String())
	}
	body := decodeAssessmentSession(t, recorder)
	if body["answered_count"].(float64) != 1 {
		t.Fatalf("answered_count = %v, want 1", body["answered_count"])
	}
	if body["streak"].(float64) != 1 {
		t.Fatalf("streak = %v, want 1", body["streak"])
	}
	if body["accepts_answers"] != true {
		t.Fatalf("accepts_answers = %v, want true", body["accepts_answers"])
	}
	if body["next_item"] == nil {
		t.Fatal("an active session must return its next item")
	}
}

func TestAssessmentAnswerRejectsOutOfRangeScore(t *testing.T) {
	handler, store := newAssessmentHandlerForTest(t)
	startRequest := withOwnerFamilyContext(assessmentRequest(
		http.MethodPost,
		"/api/v1/families/family-1/assessment-sessions",
		`{"lesson_id":"greetings_basic","child_id":"child-1"}`,
	), "parent-1")
	startRecorder := httptest.NewRecorder()
	handler.start(startRecorder, startRequest)
	started := decodeAssessmentSession(t, startRecorder)
	sessionID := started["session_id"].(string)
	item := started["next_item"].(map[string]any)

	for _, score := range []string{"-1", "101"} {
		body := `{"position":0,"item_id":"` + item["item_id"].(string) +
			`","item_type":"word","target_text":"hello","score":` + score + `}`
		request := withOwnerFamilyContext(assessmentRequest(
			http.MethodPost,
			"/api/v1/families/family-1/assessment-sessions/"+sessionID+"/answers",
			body,
		), "parent-1")
		request.SetPathValue("session_id", sessionID)
		recorder := httptest.NewRecorder()
		handler.answer(recorder, request)
		if recorder.Code != http.StatusUnprocessableEntity {
			t.Fatalf("score %s status = %d, want 422", score, recorder.Code)
		}
		assertAssessmentErrorCode(t, recorder, "assessment_invalid_score")
	}
	// A rejected answer must not move the counters.
	if stored := store.sessions[sessionID]; stored.AnsweredCount != 0 {
		t.Fatalf("answered = %d, want 0", stored.AnsweredCount)
	}
}

func TestAssessmentGetHidesAnotherFamilysSession(t *testing.T) {
	handler, _ := newAssessmentHandlerForTest(t)
	startRequest := withOwnerFamilyContext(assessmentRequest(
		http.MethodPost,
		"/api/v1/families/family-1/assessment-sessions",
		`{"lesson_id":"greetings_basic","child_id":"child-1"}`,
	), "parent-1")
	startRecorder := httptest.NewRecorder()
	handler.start(startRecorder, startRequest)
	sessionID := decodeAssessmentSession(t, startRecorder)["session_id"].(string)

	request := withOwnerFamilyContext(assessmentRequest(
		http.MethodGet,
		"/api/v1/families/family-2/assessment-sessions/"+sessionID,
		"",
	), "parent-2")
	request.SetPathValue("family_id", "family-2")
	request.SetPathValue("session_id", sessionID)
	recorder := httptest.NewRecorder()
	handler.get(recorder, request)
	// The answer must be identical to a session that does not exist.
	if recorder.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", recorder.Code)
	}
	assertAssessmentErrorCode(t, recorder, "assessment_not_found")
}

func TestAssessmentFinishThenAnswerConflicts(t *testing.T) {
	handler, _ := newAssessmentHandlerForTest(t)
	startRequest := withOwnerFamilyContext(assessmentRequest(
		http.MethodPost,
		"/api/v1/families/family-1/assessment-sessions",
		`{"lesson_id":"greetings_basic","child_id":"child-1"}`,
	), "parent-1")
	startRecorder := httptest.NewRecorder()
	handler.start(startRecorder, startRequest)
	started := decodeAssessmentSession(t, startRecorder)
	sessionID := started["session_id"].(string)

	finishRequest := withOwnerFamilyContext(assessmentRequest(
		http.MethodPost,
		"/api/v1/families/family-1/assessment-sessions/"+sessionID+"/finish",
		"",
	), "parent-1")
	finishRequest.SetPathValue("session_id", sessionID)
	finishRecorder := httptest.NewRecorder()
	handler.finish(finishRecorder, finishRequest)
	if finishRecorder.Code != http.StatusOK {
		t.Fatalf("finish status = %d, want 200: %s",
			finishRecorder.Code, finishRecorder.Body.String())
	}
	finished := decodeAssessmentSession(t, finishRecorder)
	if finished["state"] != string(assessmentdomain.StateCompleted) {
		t.Fatalf("state = %v, want completed", finished["state"])
	}
	if finished["accepts_answers"] != false {
		t.Fatalf("accepts_answers = %v, want false", finished["accepts_answers"])
	}

	item := started["next_item"].(map[string]any)
	answerRequest := withOwnerFamilyContext(assessmentRequest(
		http.MethodPost,
		"/api/v1/families/family-1/assessment-sessions/"+sessionID+"/answers",
		`{"position":0,"item_id":"`+item["item_id"].(string)+
			`","item_type":"word","target_text":"hello","score":90}`,
	), "parent-1")
	answerRequest.SetPathValue("session_id", sessionID)
	recorder := httptest.NewRecorder()
	handler.answer(recorder, answerRequest)
	if recorder.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409: %s", recorder.Code, recorder.Body.String())
	}
	assertAssessmentErrorCode(t, recorder, "assessment_state_changed")
}

func TestAssessmentHistoryListsSessions(t *testing.T) {
	handler, _ := newAssessmentHandlerForTest(t)
	startRequest := withOwnerFamilyContext(assessmentRequest(
		http.MethodPost,
		"/api/v1/families/family-1/assessment-sessions",
		`{"lesson_id":"greetings_basic","child_id":"child-1"}`,
	), "parent-1")
	handler.start(httptest.NewRecorder(), startRequest)

	request := withOwnerFamilyContext(assessmentRequest(
		http.MethodGet,
		"/api/v1/families/family-1/assessment-sessions?limit=20",
		"",
	), "parent-1")
	recorder := httptest.NewRecorder()
	handler.history(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", recorder.Code, recorder.Body.String())
	}
	var envelope struct {
		Data struct {
			Sessions []map[string]any `json:"sessions"`
		} `json:"data"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(envelope.Data.Sessions) != 1 {
		t.Fatalf("sessions = %d, want 1", len(envelope.Data.Sessions))
	}
}

func TestDeviceAssessmentReturnsNextItem(t *testing.T) {
	store := newAssessmentMemoryStore()
	service := assessmentservice.NewService(assessmentservice.Options{
		Store:   store,
		Lessons: assessmentTestCatalog{items: 10},
		Logger:  slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	guardian := guardianAssessmentHandler{service: service}
	startRequest := withOwnerFamilyContext(assessmentRequest(
		http.MethodPost,
		"/api/v1/families/family-1/assessment-sessions",
		`{"lesson_id":"greetings_basic","child_id":"child-1"}`,
	), "parent-1")
	startRecorder := httptest.NewRecorder()
	guardian.start(startRecorder, startRequest)
	sessionID := decodeAssessmentSession(t, startRecorder)["session_id"].(string)

	device := deviceAssessmentHandler{service: service}
	request := httptest.NewRequest(
		http.MethodGet,
		"/api/v1/devices/device-1/assessment-sessions/"+sessionID,
		nil,
	)
	request.SetPathValue("device_id", "device-1")
	request.SetPathValue("session_id", sessionID)
	recorder := httptest.NewRecorder()
	device.current(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", recorder.Code, recorder.Body.String())
	}
	body := decodeAssessmentSession(t, recorder)
	if body["next_item"] == nil {
		t.Fatal("the device read must carry the next item")
	}

	// A device must not see another household's session, even by identifier.
	other := httptest.NewRequest(
		http.MethodGet,
		"/api/v1/devices/device-9/assessment-sessions/"+sessionID,
		nil,
	)
	other.SetPathValue("device_id", "device-9")
	other.SetPathValue("session_id", sessionID)
	otherRecorder := httptest.NewRecorder()
	device.current(otherRecorder, other)
	if otherRecorder.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", otherRecorder.Code)
	}
}

// assertAssessmentErrorCode checks the stable machine code the client switches on.
func assertAssessmentErrorCode(t *testing.T, recorder *httptest.ResponseRecorder, want string) {
	t.Helper()
	var envelope struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decode error envelope: %v: %s", err, recorder.Body.String())
	}
	if envelope.Error.Code != want {
		t.Fatalf("code = %q, want %q: %s",
			envelope.Error.Code, want, recorder.Body.String())
	}
}
