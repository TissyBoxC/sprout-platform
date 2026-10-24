// Package service drives a learning assessment from start to finish.
//
// The service owns the lifecycle decisions the domain cannot make alone: which
// course item comes next, whether the family may start another session, and how
// a store failure becomes a stable error the transport can translate. Difficulty
// arithmetic lives in the domain so it stays testable without a database, and
// this package never imports the repository: the composition root translates
// repository sentinels into the StoreSentinel values below.
package service

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"sync/atomic"
	"time"

	"github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/assessment/domain"
)

var (
	// ErrSessionNotFound means the session is unknown to this caller. A session
	// owned by another family answers identically so the endpoint cannot be used
	// to discover which sessions exist.
	ErrSessionNotFound = errors.New("assessment session not found")
	// ErrLessonNotFound means the lesson is unknown, unpublished, or has no
	// practice items to assess.
	ErrLessonNotFound = errors.New("assessment lesson not found")
	// ErrChildNotFound means the child is unknown or belongs to another family.
	ErrChildNotFound = errors.New("assessment child not found")
	// ErrAlreadyActive means the family or child already has a live session.
	ErrAlreadyActive = errors.New("assessment session already active")
	// ErrConflict means the request does not fit the current session state.
	ErrConflict = errors.New("assessment session state conflict")
	// ErrItemNotFound means the reported item is not part of the lesson.
	ErrItemNotFound = errors.New("assessment item not found")
	// ErrExpiryFailed means at least one overdue session could not be closed.
	ErrExpiryFailed = errors.New("assessment expiry incomplete")
)

var (
	// StoreSentinelNotFound marks a missing session so the adapter can translate
	// a repository error without this package importing the repository.
	StoreSentinelNotFound = errors.New("assessment store: not found")
	// StoreSentinelConflict marks a stale write or a taken answer position.
	StoreSentinelConflict = errors.New("assessment store: conflict")
	// StoreSentinelAlreadyActive marks the partial unique index refusing a
	// second live session in the same scope.
	StoreSentinelAlreadyActive = errors.New("assessment store: already active")
)

// Store is the persistence the service needs.
type Store interface {
	Create(ctx context.Context, session domain.Session) error
	Get(ctx context.Context, familyID, sessionID string) (domain.Session, error)
	// GetForDevice answers only for a device currently bound to the session's
	// child.
	GetForDevice(ctx context.Context, deviceID, sessionID string) (domain.Session, error)
	ListExpired(ctx context.Context, now time.Time, limit int) ([]domain.Session, error)
	History(ctx context.Context, familyID, childID string, limit int) ([]domain.Session, error)
	ListAnswers(ctx context.Context, sessionID string) ([]domain.Answer, error)
	// RecordAnswer writes the answer and the session counters atomically, and
	// refuses the write when the stored answer count no longer matches the one
	// the caller read.
	RecordAnswer(
		ctx context.Context,
		session domain.Session,
		answer domain.Answer,
		previousAnswered int,
	) error
	// Transition closes or finishes a session under the same compare-and-set
	// rule as RecordAnswer.
	Transition(ctx context.Context, session domain.Session, previousAnswered int) error
	CountActive(ctx context.Context) (int, error)
	CountSessionsSince(ctx context.Context, familyID string, since time.Time) (int, error)
	// CountStartedSince counts sessions started across every family, which is
	// the platform-wide activity the feature center reports.
	CountStartedSince(ctx context.Context, since time.Time) (int, error)
	// ChildInFamily reports whether a child profile belongs to the family.
	ChildInFamily(ctx context.Context, familyID, childID string) (bool, error)
}

// LessonCatalog is the read-only English course the assessment draws from.
//
// It is deliberately narrower than the English repository contract: an
// assessment only needs the published lessons and their ordered items, and a
// narrow seam keeps this module removable and testable with a small fake.
type LessonCatalog interface {
	// Lessons returns the published lessons, at most limit of them.
	Lessons(ctx context.Context, limit int) ([]Lesson, error)
	// Lesson returns one lesson with its ordered items, or ErrLessonNotFound.
	Lesson(ctx context.Context, lessonID string) (Lesson, error)
}

// Lesson is one course the assessment can draw items from.
type Lesson struct {
	LessonID string
	AgeTier  string
	Items    []Item
}

// Item is one assessable course item in teaching order.
type Item struct {
	ID   string
	Type string
	Text string
}

// SessionNotifier hands one freshly opened session to the device the child will
// answer on.
//
// It is optional. The session row is always persisted first, and the device can
// still read it through its own authenticated session id, so a notification
// failure must never fail session creation. Implementations are expected to be
// safe for concurrent use.
type SessionNotifier interface {
	NotifyAssessmentStarted(
		ctx context.Context,
		deviceID string,
		session domain.Session,
	) error
}

// Options are the collaborators the service needs.
type Options struct {
	Store    Store
	Lessons  LessonCatalog
	Logger   *slog.Logger
	Notifier SessionNotifier
}

// Service owns assessment lifecycle.
type Service struct {
	store    Store
	lessons  LessonCatalog
	logger   *slog.Logger
	notifier SessionNotifier
	now      func() time.Time
	newID    func() (string, error)
	// expiryFailures counts consecutive failed sweeps. A session that never
	// closes keeps its slot and blocks the next assessment, so the health probe
	// reports degraded instead of inspecting an empty table forever.
	expiryFailures atomic.Int32
}

// NewService builds the service.
func NewService(options Options) *Service {
	logger := options.Logger
	if logger == nil {
		logger = slog.Default()
	}
	return &Service{
		store:    options.Store,
		lessons:  options.Lessons,
		logger:   logger,
		notifier: options.Notifier,
		now:      func() time.Time { return time.Now().UTC() },
		newID:    newID,
	}
}

// SetSessionNotifier wires the device notification port after both services have
// been constructed.
//
// The assessment service is built before the runtime command service it notifies
// through, so the seam is filled in once both exist instead of forcing a
// constructor cycle.
func (service *Service) SetSessionNotifier(notifier SessionNotifier) {
	service.notifier = notifier
}

// Start opens an assessment for a lesson the family may practise.
//
// The session is created active because the child starts answering as soon as
// the first item appears; unlike a care view there is no second party to confirm
// anything. The deadline is fixed here so the device and the expiry worker
// enforce the same instant.
func (service *Service) Start(
	ctx context.Context,
	request domain.StartRequest,
) (StartResult, error) {
	if err := request.Validate(); err != nil {
		return StartResult{}, err
	}
	if request.ChildID != "" {
		// A child from another family must look exactly like an unknown one.
		known, err := service.store.ChildInFamily(ctx, request.FamilyID, request.ChildID)
		if err != nil {
			return StartResult{}, err
		}
		if !known {
			return StartResult{}, ErrChildNotFound
		}
	}

	lesson, err := service.lessons.Lesson(ctx, request.LessonID)
	if err != nil {
		if errors.Is(err, ErrLessonNotFound) {
			return StartResult{}, ErrLessonNotFound
		}
		return StartResult{}, fmt.Errorf("read assessment lesson: %w", err)
	}
	if len(lesson.Items) == 0 {
		// A lesson without items is not assessable. Reporting it as not found
		// keeps the answer stable and stops a guardian retrying it.
		return StartResult{}, ErrLessonNotFound
	}

	now := service.now()
	count, err := service.store.CountSessionsSince(
		ctx, request.FamilyID, now.Add(-domain.SessionRateWindow),
	)
	if err != nil {
		return StartResult{}, err
	}
	if count >= domain.MaxSessionsPerWindow {
		return StartResult{}, domain.ErrRateLimited
	}

	sessionID, err := service.newID()
	if err != nil {
		return StartResult{}, err
	}
	session := domain.Session{
		ID:         sessionID,
		FamilyID:   request.FamilyID,
		ChildID:    request.ChildID,
		LessonID:   lesson.LessonID,
		State:      domain.StateActive,
		Difficulty: domain.DefaultDifficulty,
		StartedAt:  now,
		ExpiresAt:  now.Add(domain.MaxSessionDuration),
	}
	if err := service.store.Create(ctx, session); err != nil {
		if errors.Is(err, StoreSentinelAlreadyActive) {
			return StartResult{}, ErrAlreadyActive
		}
		return StartResult{}, err
	}
	// The store persists the default level; an expired session still starts
	// with no answers, so the first item is derived from that level only.
	item, err := service.pickItem(session, lesson, 0, "")
	if err != nil {
		return StartResult{}, err
	}

	// Only identifiers, the level and counters are logged. Item text is lesson
	// content and the answer belongs to a child: neither is log material.
	service.logger.Info("assessment started",
		"session_id", session.ID, "family_id", session.FamilyID,
		"child_id", session.ChildID, "lesson_id", session.LessonID,
		"difficulty", session.Difficulty)

	// A guardian who named a device wants the assessment to open on that device.
	// The session is already durable, so a failed notification only means the
	// child starts by tapping the entry point instead of being taken there.
	if service.notifier != nil && request.DeviceID != "" {
		if notifyErr := service.notifier.NotifyAssessmentStarted(
			ctx, request.DeviceID, session,
		); notifyErr != nil {
			service.logger.Warn("assessment device notification failed",
				"session_id", session.ID, "device_id", request.DeviceID,
				"error", notifyErr)
		}
	}
	return StartResult{Session: session, NextItem: item}, nil
}

// Answer records one reported answer and returns the next item.
//
// It re-derives the level from the answers already stored before applying the new
// one, so a client that lost the previous response cannot answer the same
// position twice and still move the level twice.
func (service *Service) Answer(
	ctx context.Context,
	familyID, sessionID string,
	input domain.AnswerInput,
) (AnswerResult, error) {
	session, answers, err := service.loadFamilySession(ctx, familyID, sessionID)
	if err != nil {
		return AnswerResult{}, err
	}
	return service.answer(ctx, session, answers, input)
}

// AnswerForDevice records one answer reported by the device serving the child.
func (service *Service) AnswerForDevice(
	ctx context.Context,
	deviceID, sessionID string,
	input domain.AnswerInput,
) (AnswerResult, error) {
	session, err := service.GetForDevice(ctx, deviceID, sessionID)
	if err != nil {
		return AnswerResult{}, err
	}
	answers, err := service.store.ListAnswers(ctx, session.ID)
	if err != nil {
		return AnswerResult{}, err
	}
	return service.answer(ctx, session, answers, input)
}

// Finish completes an active session on purpose.
func (service *Service) Finish(
	ctx context.Context,
	familyID, sessionID string,
) (domain.Session, error) {
	session, answers, err := service.loadFamilySession(ctx, familyID, sessionID)
	if err != nil {
		return domain.Session{}, err
	}
	return service.close(ctx, session, answers, func(candidate *domain.Session, now time.Time) error {
		return candidate.Finish(now, domain.FinishReasonCompleted)
	})
}

// Get returns one session to its own family.
func (service *Service) Get(
	ctx context.Context,
	familyID, sessionID string,
) (domain.Session, error) {
	session, _, err := service.loadFamilySession(ctx, familyID, sessionID)
	return session, err
}

// GetForDevice returns one session to the device serving its child.
func (service *Service) GetForDevice(
	ctx context.Context,
	deviceID, sessionID string,
) (domain.Session, error) {
	session, err := service.store.GetForDevice(ctx, deviceID, sessionID)
	if err != nil {
		if errors.Is(err, StoreSentinelNotFound) {
			return domain.Session{}, ErrSessionNotFound
		}
		return domain.Session{}, err
	}
	answers, err := service.store.ListAnswers(ctx, session.ID)
	if err != nil {
		return domain.Session{}, err
	}
	return service.applyState(session, answers), nil
}

// Detail returns a session with its answers, so a guardian can review the run.
func (service *Service) Detail(
	ctx context.Context,
	familyID, sessionID string,
) (domain.Session, []domain.Answer, error) {
	return service.loadFamilySession(ctx, familyID, sessionID)
}

// CurrentItem returns the item the device should show for a session.
//
// It re-derives the item from the answers already given, so a device that
// restarted mid-session resumes at the right item and level instead of showing
// the first one again. A completed session returns an empty item.
func (service *Service) CurrentItem(
	ctx context.Context,
	familyID, sessionID string,
) (domain.Session, NextItem, error) {
	session, answers, err := service.loadFamilySession(ctx, familyID, sessionID)
	if err != nil {
		return domain.Session{}, NextItem{}, err
	}
	if session.State.IsTerminal() {
		return session, NextItem{}, nil
	}
	item, err := service.deriveNext(ctx, session, answers)
	if err != nil {
		return domain.Session{}, NextItem{}, err
	}
	return session, item, nil
}

// History returns a family's past sessions, newest first.
func (service *Service) History(
	ctx context.Context,
	familyID, childID string,
	limit int,
) ([]domain.Session, error) {
	if limit <= 0 {
		limit = domain.DefaultHistoryLimit
	}
	if limit > domain.MaxHistoryLimit {
		limit = domain.MaxHistoryLimit
	}
	return service.store.History(ctx, familyID, childID, limit)
}

// ExpireDue closes every session whose deadline has passed.
//
// This is the backstop for a device that went to sleep mid-assessment: a session
// must not stay active overnight and block the next one. The answer path already
// refuses late answers through the domain, so this sweep closes the record
// rather than enforcing the deadline.
func (service *Service) ExpireDue(ctx context.Context) (int, error) {
	now := service.now()
	sessions, err := service.store.ListExpired(ctx, now, expiryBatchSize)
	if err != nil {
		return 0, err
	}
	closed := 0
	failures := 0
	for _, session := range sessions {
		answers, answerErr := service.store.ListAnswers(ctx, session.ID)
		previousAnswered := session.AnsweredCount
		if answerErr == nil {
			session = service.applyState(session, answers)
			previousAnswered = session.AnsweredCount
		}
		if err := session.Expire(now); err != nil {
			continue
		}
		if err := service.store.Transition(ctx, session, previousAnswered); err != nil {
			if errors.Is(err, StoreSentinelConflict) {
				// Another actor finished it first; that is the desired end state.
				continue
			}
			failures++
			service.logger.Warn("assessment expiry transition failed",
				"session_id", session.ID, "error", err)
			continue
		}
		closed++
		service.logger.Info("assessment closed by deadline",
			"session_id", session.ID, "family_id", session.FamilyID,
			"answered", session.AnsweredCount, "reason", session.FinishReason)
	}
	if failures > 0 {
		// A partial sweep must be visible: the health probe reads this counter,
		// and a silently successful sweep would hide sessions that never close
		// while still holding their single-active slot.
		service.expiryFailures.Add(1)
		return closed, fmt.Errorf("%w: %d sessions", ErrExpiryFailed, failures)
	}
	service.expiryFailures.Store(0)
	return closed, nil
}

// Metrics reports operational counters for the feature center.
func (service *Service) Metrics(ctx context.Context) (Metrics, error) {
	active, err := service.store.CountActive(ctx)
	if err != nil {
		return Metrics{}, err
	}
	recent, err := service.store.CountStartedSince(ctx, service.now().Add(-24*time.Hour))
	if err != nil {
		return Metrics{}, err
	}
	return Metrics{ActiveSessions: active, SessionsLast24: recent}, nil
}

// Readiness reports whether the assessment can run at all.
//
// It reads the course catalog rather than trusting process state: without a
// published lesson there is nothing to assess, and the console must say so
// instead of showing a green row over an empty catalog.
func (service *Service) Readiness(ctx context.Context) (Readiness, error) {
	lessons, err := service.lessons.Lessons(ctx, 1)
	if err != nil {
		return Readiness{}, fmt.Errorf("read assessment catalog: %w", err)
	}
	if len(lessons) == 0 {
		return Readiness{State: ReadinessDegraded}, nil
	}
	if _, err := service.store.CountActive(ctx); err != nil {
		return Readiness{}, err
	}
	if service.expiryFailures.Load() > 0 {
		// Overdue sessions are not closing. The feature still serves new
		// assessments, but a family whose stale session is stuck would see a
		// conflict it cannot resolve, so the console must show degraded.
		return Readiness{State: ReadinessDegraded}, nil
	}
	return Readiness{State: ReadinessHealthy}, nil
}

// Metrics are read-only counters exposed to operators.
type Metrics struct {
	ActiveSessions int
	SessionsLast24 int
}

// ReadinessState is the capability health shown in the feature center.
type ReadinessState string

const (
	ReadinessHealthy     ReadinessState = "healthy"
	ReadinessDegraded    ReadinessState = "degraded"
	ReadinessUnavailable ReadinessState = "unavailable"
)

// Readiness is the non-sensitive assessment health snapshot. It carries no
// lesson text, child identifier, or score.
type Readiness struct {
	State ReadinessState
}

// StartResult is a new session and the first item to show.
type StartResult struct {
	Session  domain.Session
	NextItem NextItem
}

// AnswerResult is the updated session and the item to show next. NextItem is
// empty when the session finished.
type AnswerResult struct {
	Session  domain.Session
	NextItem NextItem
}

// NextItem is the course item a device shows next.
type NextItem struct {
	// Position is the index this item will be answered at.
	Position int
	// Difficulty is the level the item was chosen at, so the device can space
	// the presentation and the guardian can see the curve.
	Difficulty int
	Item       Item
}

// Empty reports whether there is no item to show.
func (item NextItem) Empty() bool {
	return item.Item.ID == ""
}

// expiryBatchSize bounds one sweep so a long backlog cannot hold a worker
// transaction open indefinitely.
const expiryBatchSize = 100

// loadFamilySession loads a session, its answers, and the state derived from
// them. The family predicate lives in the store query, so another family's
// session is indistinguishable from a missing one.
func (service *Service) loadFamilySession(
	ctx context.Context,
	familyID, sessionID string,
) (domain.Session, []domain.Answer, error) {
	session, err := service.store.Get(ctx, familyID, sessionID)
	if err != nil {
		if errors.Is(err, StoreSentinelNotFound) {
			return domain.Session{}, nil, ErrSessionNotFound
		}
		return domain.Session{}, nil, err
	}
	answers, err := service.store.ListAnswers(ctx, session.ID)
	if err != nil {
		return domain.Session{}, nil, err
	}
	return service.applyState(session, answers), answers, nil
}

// answer runs one answer against a session already loaded for this caller.
func (service *Service) answer(
	ctx context.Context,
	session domain.Session,
	answers []domain.Answer,
	input domain.AnswerInput,
) (AnswerResult, error) {
	session = service.applyState(session, answers)
	lesson, err := service.lessons.Lesson(ctx, session.LessonID)
	if err != nil {
		if errors.Is(err, ErrLessonNotFound) {
			return AnswerResult{}, ErrLessonNotFound
		}
		return AnswerResult{}, fmt.Errorf("read assessment lesson: %w", err)
	}
	// The item must belong to the lesson and match the type the client claims,
	// so a session cannot be answered with arbitrary course text.
	if !itemInLesson(lesson, input.ItemID, input.ItemType) {
		return AnswerResult{}, ErrItemNotFound
	}

	previousAnswered := session.AnsweredCount
	recorded, err := session.Answer(input, service.now())
	if err != nil {
		return AnswerResult{}, mapDomainError(err)
	}
	recorded.ID, err = service.newID()
	if err != nil {
		return AnswerResult{}, err
	}
	if err := service.store.RecordAnswer(ctx, session, recorded, previousAnswered); err != nil {
		if errors.Is(err, StoreSentinelConflict) {
			return AnswerResult{}, ErrConflict
		}
		return AnswerResult{}, err
	}

	service.logger.Info("assessment answer recorded",
		"session_id", session.ID, "position", recorded.Position,
		"score", recorded.Score, "correct", recorded.IsCorrect,
		"difficulty", session.Difficulty, "state", session.State)
	if session.State.IsTerminal() {
		return AnswerResult{Session: session}, nil
	}
	next, err := service.pickItem(session, lesson, recorded.Position+1, recorded.ItemID)
	if err != nil {
		return AnswerResult{}, err
	}
	return AnswerResult{Session: session, NextItem: next}, nil
}

// close applies a terminal transition with one compare-and-set retry.
func (service *Service) close(
	ctx context.Context,
	session domain.Session,
	answers []domain.Answer,
	apply func(*domain.Session, time.Time) error,
) (domain.Session, error) {
	previousAnswered := session.AnsweredCount
	candidate := session
	if err := apply(&candidate, service.now()); err != nil {
		return domain.Session{}, mapDomainError(err)
	}
	err := service.store.Transition(ctx, candidate, previousAnswered)
	if err == nil {
		service.logger.Info("assessment session closed",
			"session_id", candidate.ID, "state", candidate.State,
			"reason", candidate.FinishReason)
		return candidate, nil
	}
	if !errors.Is(err, StoreSentinelConflict) {
		return domain.Session{}, err
	}
	// The counters moved under us: reload once and retry against what is stored.
	reloaded, _, reloadErr := service.loadFamilySession(ctx, session.FamilyID, session.ID)
	if reloadErr != nil {
		return domain.Session{}, reloadErr
	}
	previousAnswered = reloaded.AnsweredCount
	candidate = reloaded
	if err := apply(&candidate, service.now()); err != nil {
		return domain.Session{}, mapDomainError(err)
	}
	if err := service.store.Transition(ctx, candidate, previousAnswered); err != nil {
		if errors.Is(err, StoreSentinelConflict) {
			return domain.Session{}, ErrConflict
		}
		return domain.Session{}, err
	}
	service.logger.Info("assessment session closed",
		"session_id", candidate.ID, "state", candidate.State,
		"reason", candidate.FinishReason)
	return candidate, nil
}

// deriveNext re-derives the next item from the answers already stored.
func (service *Service) deriveNext(
	ctx context.Context,
	session domain.Session,
	answers []domain.Answer,
) (NextItem, error) {
	lesson, err := service.lessons.Lesson(ctx, session.LessonID)
	if err != nil {
		if errors.Is(err, ErrLessonNotFound) {
			return NextItem{}, ErrLessonNotFound
		}
		return NextItem{}, fmt.Errorf("read assessment lesson: %w", err)
	}
	var lastItemID string
	if len(answers) > 0 {
		lastItemID = answers[len(answers)-1].ItemID
	}
	return service.pickItem(session, lesson, session.ItemIndex, lastItemID)
}

// applyState derives the difficulty, counters and streak from the stored answers.
//
// The level is recomputed answer by answer rather than read from the row, so a
// retried write or a corrected sequence cannot leave the stored level and the
// answers disagreeing about what the child has earned.
func (service *Service) applyState(
	session domain.Session,
	answers []domain.Answer,
) domain.Session {
	level := domain.DefaultDifficulty
	streak := 0
	correct := 0
	for _, answer := range answers {
		if answer.IsCorrect {
			streak++
			correct++
		} else {
			streak = 0
		}
		level = domain.AdvanceDifficulty(level, answer.IsCorrect, streak)
	}
	session.Difficulty = level
	session.Streak = streak
	session.CorrectCount = correct
	session.AnsweredCount = len(answers)
	session.ItemIndex = len(answers)
	return session
}

// pickItem chooses the item for the given position at the session's level.
//
// Level drives the age tier (levels 1-2 the youngest band, 3-4 the middle, 5 the
// oldest) and then the position inside that band, so a child who keeps
// succeeding sees progressively harder course content. The same item is skipped
// while other items at the level are untried, and a lesson shorter than the
// level range is centred on its own content rather than refused.
func (service *Service) pickItem(
	session domain.Session,
	lesson Lesson,
	position int,
	lastItemID string,
) (NextItem, error) {
	if len(lesson.Items) == 0 {
		return NextItem{}, ErrLessonNotFound
	}
	level := domain.ClampDifficulty(session.Difficulty)
	start, width := levelBucket(level, len(lesson.Items))
	offset := position
	if offset < 0 {
		offset = 0
	}
	index := start + offset%width
	// A band of one (a very short lesson, or the hardest level) has no other
	// item to offer, and serving the same item twice in a row looks like the
	// device froze. Borrow from the neighbouring band instead.
	if width == 1 && len(lesson.Items) > 1 && lesson.Items[index].ID == lastItemID {
		index = (index + 1) % len(lesson.Items)
	}
	return NextItem{
		Position:   position,
		Difficulty: level,
		Item:       lesson.Items[index],
	}, nil
}

// levelBucket maps a level and a lesson size onto a start index and a width.
//
// The bands always cover the whole lesson, so a level never points at nothing
// even when a lesson has fewer items than there are levels.
func levelBucket(level, count int) (int, int) {
	if count <= 1 {
		return 0, 1
	}
	start := 0
	switch {
	case level >= domain.MaxDifficulty:
		start = count - 1
	case level >= 4:
		start = count / 2
	}
	if start >= count {
		start = count - 1
	}
	return start, count - start
}

// itemInLesson reports whether the reported item exists with the claimed type.
func itemInLesson(lesson Lesson, itemID, itemType string) bool {
	for _, item := range lesson.Items {
		if item.ID == itemID {
			return item.Type == itemType
		}
	}
	return false
}

// mapDomainError translates a domain error into a service error.
func mapDomainError(err error) error {
	switch {
	case errors.Is(err, domain.ErrSessionFinished),
		errors.Is(err, domain.ErrSessionExpired),
		errors.Is(err, domain.ErrUnexpectedPosition):
		return ErrConflict
	default:
		return err
	}
}

// newID returns a random identifier shaped as a UUIDv4, because every identifier
// this module stores is a uuid column.
func newID() (string, error) {
	buffer := make([]byte, 16)
	if _, err := rand.Read(buffer); err != nil {
		return "", fmt.Errorf("generate assessment id: %w", err)
	}
	buffer[6] = (buffer[6] & 0x0f) | 0x40
	buffer[8] = (buffer[8] & 0x3f) | 0x80
	encoded := hex.EncodeToString(buffer)
	return encoded[0:8] + "-" + encoded[8:12] + "-" + encoded[12:16] + "-" +
		encoded[16:20] + "-" + encoded[20:32], nil
}
