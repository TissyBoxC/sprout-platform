package service_test

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/assessment/domain"
	"github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/assessment/service"
)

// fakeStore keeps sessions in memory and honours the same sentinels as the
// repository adapter, so the service is tested against its real contract.
type fakeStore struct {
	sessions map[string]domain.Session
	answers  map[string][]domain.Answer
	children map[string]string
	// conflictNext makes the next compare-and-set write lose the race.
	conflictNext bool
	// failTransition makes Transitions fail for identifiers not listed here.
	failTransitionFor map[string]bool
}

func newFakeStore() *fakeStore {
	return &fakeStore{
		sessions:          map[string]domain.Session{},
		answers:           map[string][]domain.Answer{},
		children:          map[string]string{"child-1": "family-1", "child-2": "family-2"},
		failTransitionFor: map[string]bool{},
	}
}

func (store *fakeStore) Create(_ context.Context, session domain.Session) error {
	for _, existing := range store.sessions {
		if existing.FamilyID != session.FamilyID || existing.State.IsTerminal() {
			continue
		}
		// The partial unique index keys on (family_id, child_id), so a
		// family-level session only conflicts with another family-level one.
		if existing.ChildID == session.ChildID {
			return service.StoreSentinelAlreadyActive
		}
	}
	store.sessions[session.ID] = session
	return nil
}

func (store *fakeStore) Get(_ context.Context, familyID, sessionID string) (domain.Session, error) {
	session, ok := store.sessions[sessionID]
	if !ok || session.FamilyID != familyID {
		return domain.Session{}, service.StoreSentinelNotFound
	}
	return session, nil
}

func (store *fakeStore) GetForDevice(_ context.Context, deviceID, sessionID string) (domain.Session, error) {
	session, ok := store.sessions[sessionID]
	if !ok || deviceID != "device-1" || session.ChildID != "child-1" {
		return domain.Session{}, service.StoreSentinelNotFound
	}
	return session, nil
}

func (store *fakeStore) ListExpired(_ context.Context, now time.Time, limit int) ([]domain.Session, error) {
	expired := make([]domain.Session, 0, len(store.sessions))
	for _, session := range store.sessions {
		if session.State == domain.StateActive && !now.Before(session.ExpiresAt) {
			expired = append(expired, session)
		}
	}
	if len(expired) > limit {
		expired = expired[:limit]
	}
	return expired, nil
}

func (store *fakeStore) History(
	_ context.Context,
	familyID, childID string,
	limit int,
) ([]domain.Session, error) {
	history := make([]domain.Session, 0, limit)
	for _, session := range store.sessions {
		if session.FamilyID != familyID {
			continue
		}
		if childID != "" && session.ChildID != childID {
			continue
		}
		history = append(history, session)
	}
	return history, nil
}

func (store *fakeStore) ListAnswers(_ context.Context, sessionID string) ([]domain.Answer, error) {
	return append([]domain.Answer(nil), store.answers[sessionID]...), nil
}

func (store *fakeStore) RecordAnswer(
	_ context.Context,
	session domain.Session,
	answer domain.Answer,
	previousAnswered int,
) error {
	current, ok := store.sessions[session.ID]
	if !ok {
		return service.StoreSentinelNotFound
	}
	if store.conflictNext {
		store.conflictNext = false
		return service.StoreSentinelConflict
	}
	if !current.State.AllowsAnswers() || current.AnsweredCount != previousAnswered {
		return service.StoreSentinelConflict
	}
	for _, existing := range store.answers[session.ID] {
		if existing.Position == answer.Position {
			return service.StoreSentinelConflict
		}
	}
	store.sessions[session.ID] = session
	store.answers[session.ID] = append(store.answers[session.ID], answer)
	return nil
}

func (store *fakeStore) Transition(
	_ context.Context,
	session domain.Session,
	previousAnswered int,
) error {
	current, ok := store.sessions[session.ID]
	if !ok {
		return service.StoreSentinelNotFound
	}
	if store.conflictNext {
		store.conflictNext = false
		return service.StoreSentinelConflict
	}
	if store.failTransitionFor[session.ID] {
		return errors.New("database is on fire")
	}
	if current.State.IsTerminal() || current.AnsweredCount != previousAnswered {
		return service.StoreSentinelConflict
	}
	store.sessions[session.ID] = session
	return nil
}

func (store *fakeStore) CountActive(_ context.Context) (int, error) {
	count := 0
	for _, session := range store.sessions {
		if session.State == domain.StateActive {
			count++
		}
	}
	return count, nil
}

func (store *fakeStore) CountSessionsSince(
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

func (store *fakeStore) CountStartedSince(_ context.Context, since time.Time) (int, error) {
	count := 0
	for _, session := range store.sessions {
		if !session.StartedAt.Before(since) {
			count++
		}
	}
	return count, nil
}

func (store *fakeStore) ChildInFamily(_ context.Context, familyID, childID string) (bool, error) {
	return store.children[childID] == familyID && childID != "", nil
}

// fakeCatalog serves one lesson with ordered word items.
type fakeCatalog struct {
	lessons map[string]service.Lesson
	err     error
}

func newFakeCatalog(items int) *fakeCatalog {
	greetings := service.Lesson{LessonID: "greetings_basic", AgeTier: "age_3_4"}
	for index := 0; index < items; index++ {
		greetings.Items = append(greetings.Items, service.Item{
			ID:   "word-" + string(rune('a'+index)),
			Type: domain.ItemTypeWord,
			Text: "word",
		})
	}
	return &fakeCatalog{lessons: map[string]service.Lesson{greetings.LessonID: greetings}}
}

func (catalog *fakeCatalog) Lessons(_ context.Context, limit int) ([]service.Lesson, error) {
	if catalog.err != nil {
		return nil, catalog.err
	}
	lessons := make([]service.Lesson, 0, len(catalog.lessons))
	for _, lesson := range catalog.lessons {
		lessons = append(lessons, lesson)
		if limit > 0 && len(lessons) >= limit {
			break
		}
	}
	return lessons, nil
}

func (catalog *fakeCatalog) Lesson(_ context.Context, lessonID string) (service.Lesson, error) {
	lesson, ok := catalog.lessons[lessonID]
	if !ok {
		return service.Lesson{}, service.ErrLessonNotFound
	}
	return lesson, nil
}

func newService(store *fakeStore, catalog *fakeCatalog) *service.Service {
	return service.NewService(service.Options{
		Store:   store,
		Lessons: catalog,
		Logger:  slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
}

func startRequest() domain.StartRequest {
	return domain.StartRequest{
		FamilyID: "family-1",
		ChildID:  "child-1",
		LessonID: "greetings_basic",
	}
}

// answerFor builds an answer for the position a session is discussing now.
func answerFor(item service.NextItem, score int) domain.AnswerInput {
	return domain.AnswerInput{
		Position:   item.Position,
		ItemID:     item.Item.ID,
		ItemType:   item.Item.Type,
		TargetText: item.Item.Text,
		Score:      score,
	}
}

func TestStartCreatesActiveSession(t *testing.T) {
	store := newFakeStore()
	svc := newService(store, newFakeCatalog(10))

	result, err := svc.Start(context.Background(), startRequest())
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	if result.Session.State != domain.StateActive {
		t.Fatalf("state = %s, want active", result.Session.State)
	}
	if result.Session.Difficulty != domain.DefaultDifficulty {
		t.Fatalf("difficulty = %d, want %d", result.Session.Difficulty, domain.DefaultDifficulty)
	}
	if result.Session.ExpiresAt.Sub(result.Session.StartedAt) != domain.MaxSessionDuration {
		t.Fatalf("window = %v, want %v",
			result.Session.ExpiresAt.Sub(result.Session.StartedAt), domain.MaxSessionDuration)
	}
	if len(result.Session.ID) != 36 {
		t.Fatalf("id = %q, want a 36-character UUID", result.Session.ID)
	}
	if result.NextItem.Empty() {
		t.Fatal("a new session must offer its first item")
	}
	if result.NextItem.Difficulty != domain.DefaultDifficulty {
		t.Fatalf("item difficulty = %d, want %d",
			result.NextItem.Difficulty, domain.DefaultDifficulty)
	}
}

func TestStartRejectsUnknownLesson(t *testing.T) {
	svc := newService(newFakeStore(), newFakeCatalog(5))
	request := startRequest()
	request.LessonID = "missing_lesson"
	if _, err := svc.Start(context.Background(), request); !errors.Is(err, service.ErrLessonNotFound) {
		t.Fatalf("err = %v, want ErrLessonNotFound", err)
	}
}

func TestStartRejectsEmptyLesson(t *testing.T) {
	store := newFakeStore()
	catalog := &fakeCatalog{lessons: map[string]service.Lesson{
		"greetings_basic": {LessonID: "greetings_basic"},
	}}
	svc := newService(store, catalog)
	// A lesson with no items cannot be assessed; it must not start an empty run.
	if _, err := svc.Start(context.Background(), startRequest()); !errors.Is(err, service.ErrLessonNotFound) {
		t.Fatalf("err = %v, want ErrLessonNotFound", err)
	}
}

func TestStartRejectsForeignChild(t *testing.T) {
	svc := newService(newFakeStore(), newFakeCatalog(5))
	request := startRequest()
	// child-2 belongs to family-2 and must look exactly like an unknown child.
	request.ChildID = "child-2"
	if _, err := svc.Start(context.Background(), request); !errors.Is(err, service.ErrChildNotFound) {
		t.Fatalf("err = %v, want ErrChildNotFound", err)
	}
	request.ChildID = "nobody"
	if _, err := svc.Start(context.Background(), request); !errors.Is(err, service.ErrChildNotFound) {
		t.Fatalf("err = %v, want ErrChildNotFound", err)
	}
}

func TestStartRejectsSecondActiveSession(t *testing.T) {
	svc := newService(newFakeStore(), newFakeCatalog(5))
	if _, err := svc.Start(context.Background(), startRequest()); err != nil {
		t.Fatalf("first start: %v", err)
	}
	// A child practises one assessment at a time.
	if _, err := svc.Start(context.Background(), startRequest()); !errors.Is(err, service.ErrAlreadyActive) {
		t.Fatalf("second start err = %v, want ErrAlreadyActive", err)
	}
}

func TestStartAllowsAnotherChildInTheSameFamily(t *testing.T) {
	store := newFakeStore()
	store.children["child-3"] = "family-1"
	svc := newService(store, newFakeCatalog(5))
	if _, err := svc.Start(context.Background(), startRequest()); err != nil {
		t.Fatalf("first start: %v", err)
	}
	request := startRequest()
	request.ChildID = "child-3"
	// The single-active rule is per child, not per household.
	if _, err := svc.Start(context.Background(), request); err != nil {
		t.Fatalf("second child start: %v", err)
	}
}

func TestStartIsRateLimited(t *testing.T) {
	store := newFakeStore()
	svc := newService(store, newFakeCatalog(5))
	for index := 0; index < domain.MaxSessionsPerWindow; index++ {
		request := startRequest()
		// A distinct child per run keeps the single-active rule out of the way
		// while still counting toward the family's hourly limit.
		request.ChildID = ""
		created, err := svc.Start(context.Background(), request)
		if err != nil {
			t.Fatalf("start %d: %v", index, err)
		}
		stored := store.sessions[created.Session.ID]
		stored.State = domain.StateAbandoned
		finished := stored.StartedAt
		stored.FinishedAt = &finished
		store.sessions[created.Session.ID] = stored
	}
	if _, err := svc.Start(context.Background(), startRequest()); !errors.Is(err, domain.ErrRateLimited) {
		t.Fatalf("err = %v, want ErrRateLimited", err)
	}
}

func TestAnswerRaisesDifficultyAndServesNextItem(t *testing.T) {
	store := newFakeStore()
	svc := newService(store, newFakeCatalog(10))
	started, err := svc.Start(context.Background(), startRequest())
	if err != nil {
		t.Fatalf("start: %v", err)
	}

	result, err := svc.Answer(
		context.Background(),
		started.Session.FamilyID,
		started.Session.ID,
		answerFor(started.NextItem, 95),
	)
	if err != nil {
		t.Fatalf("first answer: %v", err)
	}
	if result.Session.Difficulty != domain.DefaultDifficulty {
		t.Fatalf("difficulty after one correct = %d, want %d",
			result.Session.Difficulty, domain.DefaultDifficulty)
	}
	if result.NextItem.Empty() {
		t.Fatal("an active session must offer the next item")
	}

	result, err = svc.Answer(
		context.Background(),
		started.Session.FamilyID,
		started.Session.ID,
		answerFor(result.NextItem, 95),
	)
	if err != nil {
		t.Fatalf("second answer: %v", err)
	}
	if result.Session.Difficulty != domain.DefaultDifficulty+1 {
		t.Fatalf("difficulty after two correct = %d, want %d",
			result.Session.Difficulty, domain.DefaultDifficulty+1)
	}
	if result.Session.CorrectCount != 2 || result.Session.AnsweredCount != 2 {
		t.Fatalf("counters = %d/%d, want 2/2",
			result.Session.CorrectCount, result.Session.AnsweredCount)
	}
}

func TestAnswerLowersDifficultyAfterAWrongAnswer(t *testing.T) {
	store := newFakeStore()
	svc := newService(store, newFakeCatalog(10))
	started, _ := svc.Start(context.Background(), startRequest())

	// The level is derived from the answers, so it has to be earned: the second
	// and third correct answers take the child from level 2 to level 4.
	item := started.NextItem
	for index := 0; index < 3; index++ {
		result, err := svc.Answer(context.Background(), "family-1", started.Session.ID, answerFor(item, 90))
		if err != nil {
			t.Fatalf("answer %d: %v", index, err)
		}
		item = result.NextItem
	}
	if item.Difficulty != 4 {
		t.Fatalf("difficulty before the miss = %d, want 4", item.Difficulty)
	}

	result, err := svc.Answer(context.Background(), "family-1", started.Session.ID, answerFor(item, 10))
	if err != nil {
		t.Fatalf("answer: %v", err)
	}
	if result.Session.Difficulty != 3 {
		t.Fatalf("difficulty = %d, want 3", result.Session.Difficulty)
	}
	if result.Session.CorrectCount != 3 {
		t.Fatalf("correct = %d, want 3", result.Session.CorrectCount)
	}
}

func TestAnswerRejectsReplayedPosition(t *testing.T) {
	store := newFakeStore()
	svc := newService(store, newFakeCatalog(10))
	started, _ := svc.Start(context.Background(), startRequest())
	first := answerFor(started.NextItem, 90)
	if _, err := svc.Answer(context.Background(), "family-1", started.Session.ID, first); err != nil {
		t.Fatalf("first answer: %v", err)
	}
	if _, err := svc.Answer(context.Background(), "family-1", started.Session.ID, first); !errors.Is(
		err, service.ErrConflict,
	) {
		t.Fatalf("replay err = %v, want ErrConflict", err)
	}
	if len(store.answers[started.Session.ID]) != 1 {
		t.Fatalf("stored answers = %d, want 1", len(store.answers[started.Session.ID]))
	}
}

func TestAnswerRejectsExpiredSession(t *testing.T) {
	store := newFakeStore()
	svc := newService(store, newFakeCatalog(10))
	started, _ := svc.Start(context.Background(), startRequest())
	stored := store.sessions[started.Session.ID]
	stored.ExpiresAt = time.Now().UTC().Add(-time.Second)
	store.sessions[started.Session.ID] = stored

	// The sweep has not run yet; the answer path must still refuse the answer.
	if _, err := svc.Answer(
		context.Background(), "family-1", started.Session.ID, answerFor(started.NextItem, 90),
	); !errors.Is(err, service.ErrConflict) {
		t.Fatalf("err = %v, want ErrConflict", err)
	}
}

func TestAnswerRejectsFinishedSession(t *testing.T) {
	store := newFakeStore()
	svc := newService(store, newFakeCatalog(10))
	started, _ := svc.Start(context.Background(), startRequest())
	if _, err := svc.Finish(context.Background(), "family-1", started.Session.ID); err != nil {
		t.Fatalf("finish: %v", err)
	}
	if _, err := svc.Answer(
		context.Background(), "family-1", started.Session.ID, answerFor(started.NextItem, 90),
	); !errors.Is(err, service.ErrConflict) {
		t.Fatalf("err = %v, want ErrConflict", err)
	}
}

func TestAnswerRejectsItemOutsideTheLesson(t *testing.T) {
	store := newFakeStore()
	svc := newService(store, newFakeCatalog(10))
	started, _ := svc.Start(context.Background(), startRequest())

	foreign := answerFor(started.NextItem, 90)
	foreign.ItemID = "not-in-this-lesson"
	if _, err := svc.Answer(context.Background(), "family-1", started.Session.ID, foreign); !errors.Is(
		err, service.ErrItemNotFound,
	) {
		t.Fatalf("err = %v, want ErrItemNotFound", err)
	}

	wrongType := answerFor(started.NextItem, 90)
	wrongType.ItemType = domain.ItemTypeSentence
	if _, err := svc.Answer(context.Background(), "family-1", started.Session.ID, wrongType); !errors.Is(
		err, service.ErrItemNotFound,
	) {
		t.Fatalf("err = %v, want ErrItemNotFound", err)
	}
}

func TestAnswerRejectsInvalidScore(t *testing.T) {
	store := newFakeStore()
	svc := newService(store, newFakeCatalog(10))
	started, _ := svc.Start(context.Background(), startRequest())
	for _, score := range []int{-1, 101} {
		if _, err := svc.Answer(
			context.Background(), "family-1", started.Session.ID, answerFor(started.NextItem, score),
		); !errors.Is(err, domain.ErrInvalidScore) {
			t.Fatalf("score %d err = %v, want ErrInvalidScore", score, err)
		}
	}
}

func TestAnswerOnAnotherFamilysSessionIsNotFound(t *testing.T) {
	store := newFakeStore()
	svc := newService(store, newFakeCatalog(10))
	started, _ := svc.Start(context.Background(), startRequest())
	// A cross-family access must not reveal that the session exists.
	if _, err := svc.Get(context.Background(), "family-2", started.Session.ID); !errors.Is(
		err, service.ErrSessionNotFound,
	) {
		t.Fatalf("err = %v, want ErrSessionNotFound", err)
	}
	if _, err := svc.Answer(
		context.Background(), "family-2", started.Session.ID, answerFor(started.NextItem, 90),
	); !errors.Is(err, service.ErrSessionNotFound) {
		t.Fatalf("answer err = %v, want ErrSessionNotFound", err)
	}
	if _, err := svc.Finish(context.Background(), "family-2", started.Session.ID); !errors.Is(
		err, service.ErrSessionNotFound,
	) {
		t.Fatalf("finish err = %v, want ErrSessionNotFound", err)
	}
}

func TestAnswerFinishesAtTheItemLimit(t *testing.T) {
	store := newFakeStore()
	svc := newService(store, newFakeCatalog(10))
	started, _ := svc.Start(context.Background(), startRequest())
	item := started.NextItem
	var result service.AnswerResult
	var err error
	for index := 0; index < domain.MaxItemsPerSession; index++ {
		result, err = svc.Answer(context.Background(), "family-1", started.Session.ID, answerFor(item, 80))
		if err != nil {
			t.Fatalf("answer %d: %v", index, err)
		}
		item = result.NextItem
	}
	if result.Session.State != domain.StateCompleted {
		t.Fatalf("state = %s, want completed", result.Session.State)
	}
	if result.Session.FinishReason != domain.FinishReasonItemLimit {
		t.Fatalf("reason = %q, want %q",
			result.Session.FinishReason, domain.FinishReasonItemLimit)
	}
	if !result.NextItem.Empty() {
		t.Fatal("a completed session must not offer another item")
	}
	if result.Session.Mastery() != 100 {
		t.Fatalf("mastery = %d, want 100", result.Session.Mastery())
	}
}

func TestConsecutiveItemsAreNotRepeated(t *testing.T) {
	store := newFakeStore()
	svc := newService(store, newFakeCatalog(10))
	started, _ := svc.Start(context.Background(), startRequest())
	previous := started.NextItem.Item.ID
	item := started.NextItem
	for index := 0; index < 6; index++ {
		result, err := svc.Answer(context.Background(), "family-1", started.Session.ID, answerFor(item, 90))
		if err != nil {
			t.Fatalf("answer %d: %v", index, err)
		}
		item = result.NextItem
		if item.Item.ID == previous {
			t.Fatalf("item %q served twice in a row", item.Item.ID)
		}
		previous = item.Item.ID
	}
}

func TestFinishCompletesAndFlipsToTerminal(t *testing.T) {
	store := newFakeStore()
	svc := newService(store, newFakeCatalog(10))
	started, _ := svc.Start(context.Background(), startRequest())
	if _, err := svc.Answer(
		context.Background(), "family-1", started.Session.ID, answerFor(started.NextItem, 70),
	); err != nil {
		t.Fatalf("answer: %v", err)
	}

	finished, err := svc.Finish(context.Background(), "family-1", started.Session.ID)
	if err != nil {
		t.Fatalf("finish: %v", err)
	}
	if finished.State != domain.StateCompleted || finished.FinishReason != domain.FinishReasonCompleted {
		t.Fatalf("finished = %s/%s", finished.State, finished.FinishReason)
	}
	if finished.FinishedAt == nil {
		t.Fatal("a finished session must record when it ended")
	}
	if finished.Mastery() != 100 {
		t.Fatalf("mastery = %d, want 100", finished.Mastery())
	}
	if _, err := svc.Finish(context.Background(), "family-1", started.Session.ID); !errors.Is(
		err, service.ErrConflict,
	) {
		t.Fatalf("second finish err = %v, want ErrConflict", err)
	}
}

func TestExpireDueClosesOverdueSessions(t *testing.T) {
	store := newFakeStore()
	svc := newService(store, newFakeCatalog(10))
	fresh, _ := svc.Start(context.Background(), startRequest())
	request := startRequest()
	request.ChildID = ""
	overdue, _ := svc.Start(context.Background(), request)

	stored := store.sessions[overdue.Session.ID]
	stored.ExpiresAt = time.Now().UTC().Add(-time.Minute)
	store.sessions[overdue.Session.ID] = stored

	closed, err := svc.ExpireDue(context.Background())
	if err != nil {
		t.Fatalf("expire due: %v", err)
	}
	if closed != 1 {
		t.Fatalf("closed = %d, want 1", closed)
	}
	expired := store.sessions[overdue.Session.ID]
	if expired.State != domain.StateAbandoned || expired.FinishReason != domain.FinishReasonExpired {
		t.Fatalf("expired = %s/%s, want abandoned/expired",
			expired.State, expired.FinishReason)
	}
	// A live session must not be touched by the sweep.
	if store.sessions[fresh.Session.ID].State != domain.StateActive {
		t.Fatalf("fresh state = %s, want active", store.sessions[fresh.Session.ID].State)
	}
}

func TestExpireDueReportsFailures(t *testing.T) {
	store := newFakeStore()
	svc := newService(store, newFakeCatalog(10))
	started, _ := svc.Start(context.Background(), startRequest())
	stored := store.sessions[started.Session.ID]
	stored.ExpiresAt = time.Now().UTC().Add(-time.Minute)
	store.sessions[started.Session.ID] = stored
	store.failTransitionFor[started.Session.ID] = true

	// A sweep that cannot close an overdue session must not report success.
	if _, err := svc.ExpireDue(context.Background()); !errors.Is(err, service.ErrExpiryFailed) {
		t.Fatalf("err = %v, want ErrExpiryFailed", err)
	}
	// A stuck session keeps its slot, so the capability is degraded even though
	// the catalog itself is fine.
	readiness, err := svc.Readiness(context.Background())
	if err != nil {
		t.Fatalf("readiness: %v", err)
	}
	if readiness.State != service.ReadinessDegraded {
		t.Fatalf("state = %s, want degraded", readiness.State)
	}
}

func TestGetForDeviceOnlyAnswersForTheBoundChild(t *testing.T) {
	store := newFakeStore()
	svc := newService(store, newFakeCatalog(10))
	started, _ := svc.Start(context.Background(), startRequest())

	session, err := svc.GetForDevice(context.Background(), "device-1", started.Session.ID)
	if err != nil {
		t.Fatalf("device read: %v", err)
	}
	if session.ID != started.Session.ID {
		t.Fatalf("session = %q, want %q", session.ID, started.Session.ID)
	}
	// Another device must not learn that this session exists.
	if _, err := svc.GetForDevice(context.Background(), "device-2", started.Session.ID); !errors.Is(
		err, service.ErrSessionNotFound,
	) {
		t.Fatalf("err = %v, want ErrSessionNotFound", err)
	}
}

func TestAnswerForDeviceRecordsForTheOwningDevice(t *testing.T) {
	store := newFakeStore()
	svc := newService(store, newFakeCatalog(10))
	started, _ := svc.Start(context.Background(), startRequest())

	result, err := svc.AnswerForDevice(
		context.Background(), "device-1", started.Session.ID, answerFor(started.NextItem, 90),
	)
	if err != nil {
		t.Fatalf("device answer: %v", err)
	}
	if result.Session.AnsweredCount != 1 {
		t.Fatalf("answered = %d, want 1", result.Session.AnsweredCount)
	}
	if _, err := svc.AnswerForDevice(
		context.Background(), "device-2", started.Session.ID, answerFor(result.NextItem, 90),
	); !errors.Is(err, service.ErrSessionNotFound) {
		t.Fatalf("err = %v, want ErrSessionNotFound", err)
	}
}

func TestCurrentItemResumesAtTheNextPosition(t *testing.T) {
	store := newFakeStore()
	svc := newService(store, newFakeCatalog(10))
	started, _ := svc.Start(context.Background(), startRequest())
	result, err := svc.Answer(
		context.Background(), "family-1", started.Session.ID, answerFor(started.NextItem, 90),
	)
	if err != nil {
		t.Fatalf("answer: %v", err)
	}

	session, item, err := svc.CurrentItem(context.Background(), "family-1", started.Session.ID)
	if err != nil {
		t.Fatalf("current item: %v", err)
	}
	if item.Position != 1 {
		t.Fatalf("position = %d, want 1", item.Position)
	}
	if item.Item.ID != result.NextItem.Item.ID {
		t.Fatalf("item = %q, want %q", item.Item.ID, result.NextItem.Item.ID)
	}
	if session.AnsweredCount != 1 {
		t.Fatalf("answered = %d, want 1", session.AnsweredCount)
	}
}

func TestHistoryClampsLimitAndScopesFamily(t *testing.T) {
	store := newFakeStore()
	svc := newService(store, newFakeCatalog(10))
	started, _ := svc.Start(context.Background(), startRequest())

	history, err := svc.History(context.Background(), "family-1", "", -5)
	if err != nil {
		t.Fatalf("history: %v", err)
	}
	if len(history) != 1 || history[0].ID != started.Session.ID {
		t.Fatalf("history = %+v", history)
	}
	other, err := svc.History(context.Background(), "family-2", "", 100000)
	if err != nil {
		t.Fatalf("history: %v", err)
	}
	if len(other) != 0 {
		t.Fatalf("family-2 history = %d, want 0", len(other))
	}
}

func TestMetricsReportsCounts(t *testing.T) {
	svc := newService(newFakeStore(), newFakeCatalog(10))
	if _, err := svc.Start(context.Background(), startRequest()); err != nil {
		t.Fatalf("start: %v", err)
	}
	metrics, err := svc.Metrics(context.Background())
	if err != nil {
		t.Fatalf("metrics: %v", err)
	}
	if metrics.ActiveSessions != 1 || metrics.SessionsLast24 != 1 {
		t.Fatalf("metrics = %+v, want 1 active and 1 recent", metrics)
	}
}

func TestReadiness(t *testing.T) {
	empty := newService(newFakeStore(), &fakeCatalog{lessons: map[string]service.Lesson{}})
	readiness, err := empty.Readiness(context.Background())
	if err != nil {
		t.Fatalf("readiness: %v", err)
	}
	// Without a published lesson there is nothing to assess, so the console must
	// not show the capability as healthy.
	if readiness.State != service.ReadinessDegraded {
		t.Fatalf("state = %s, want degraded", readiness.State)
	}

	ready := newService(newFakeStore(), newFakeCatalog(3))
	readiness, err = ready.Readiness(context.Background())
	if err != nil {
		t.Fatalf("readiness: %v", err)
	}
	if readiness.State != service.ReadinessHealthy {
		t.Fatalf("state = %s, want healthy", readiness.State)
	}

	broken := newService(newFakeStore(), &fakeCatalog{err: errors.New("catalog down")})
	if _, err := broken.Readiness(context.Background()); err == nil {
		t.Fatal("an unreadable catalog must not report a health state")
	}
}

func TestAnswerRetriesAfterAConcurrentWrite(t *testing.T) {
	store := newFakeStore()
	svc := newService(store, newFakeCatalog(10))
	started, _ := svc.Start(context.Background(), startRequest())
	store.conflictNext = true

	// A lost race is reported as a conflict, never silently dropped.
	if _, err := svc.Answer(
		context.Background(), "family-1", started.Session.ID, answerFor(started.NextItem, 90),
	); !errors.Is(err, service.ErrConflict) {
		t.Fatalf("err = %v, want ErrConflict", err)
	}
	if len(store.answers[started.Session.ID]) != 0 {
		t.Fatalf("answers = %d, want 0", len(store.answers[started.Session.ID]))
	}
}
