// Package domain defines a learning assessment: one bounded run of English
// practice items where the difficulty follows the child's answers.
//
// An assessment is not a test the child can fail. It exists so the platform can
// pick the next item at the right level: a child who answers two in a row moves
// up, a child who misses one moves down, and the session ends after a fixed
// number of items or a fixed wall-clock window.
//
// Three rules shape every transition. Only an active session accepts answers,
// a session past its deadline is refused even before the expiry worker reaches
// it, and every answer position is consumed exactly once. All of the arithmetic
// lives here so the difficulty curve is testable without a database.
package domain

import (
	"errors"
	"strings"
	"time"
	"unicode/utf8"
)

var (
	// ErrInvalidRequest means the request cannot address a real assessment.
	ErrInvalidRequest = errors.New("invalid assessment request")
	// ErrInvalidAnswer means the reported answer cannot be recorded.
	ErrInvalidAnswer = errors.New("invalid assessment answer")
	// ErrInvalidLessonID means the lesson identifier is malformed.
	ErrInvalidLessonID = errors.New("invalid assessment lesson id")
	// ErrInvalidItemType means the item is neither a word nor a sentence.
	ErrInvalidItemType = errors.New("invalid assessment item type")
	// ErrInvalidScore means the score falls outside 0-100.
	ErrInvalidScore = errors.New("invalid assessment score")
	// ErrLessonNotFound means the lesson is unknown or not published.
	ErrLessonNotFound = errors.New("assessment lesson not found")
	// ErrChildNotFound means the child profile is unknown or owned by another
	// family.
	ErrChildNotFound = errors.New("assessment child not found")
	// ErrSessionFinished means the session already reached a terminal state.
	ErrSessionFinished = errors.New("assessment session already finished")
	// ErrSessionExpired means the session ran past its deadline.
	ErrSessionExpired = errors.New("assessment session expired")
	// ErrUnexpectedPosition means the answer did not continue the sequence.
	ErrUnexpectedPosition = errors.New("assessment answer position out of order")
	// ErrAlreadyActive means the family or child already has an open session.
	ErrAlreadyActive = errors.New("assessment session already active")
	// ErrRateLimited means the family started assessments too often.
	ErrRateLimited = errors.New("assessment rate limit reached")
)

// SessionState is the lifecycle position of one assessment.
type SessionState string

const (
	// StateActive is the only state that accepts answers.
	StateActive SessionState = "active"
	// StateCompleted means the child reached the item limit or the guardian
	// finished the session on purpose.
	StateCompleted SessionState = "completed"
	// StateAbandoned means the session was closed without being finished, for
	// example by the deadline during a pause in the practice.
	StateAbandoned SessionState = "abandoned"
)

// Item types mirror the English practice item vocabulary the device already
// reports, so an assessment answer can be stored beside its practice attempt.
const (
	ItemTypeWord     = "word"
	ItemTypeSentence = "sentence"
)

// Finish reasons are short stable tokens, never free text from a client.
const (
	FinishReasonCompleted = "completed"
	FinishReasonExpired   = "expired"
	FinishReasonAbandoned = "abandoned"
	FinishReasonItemLimit = "item_limit"
)

const (
	// MinDifficulty and MaxDifficulty bound the adaptive level. Level 1 is the
	// easiest lesson content; level 5 is the hardest.
	MinDifficulty = 1
	MaxDifficulty = 5
	// DefaultDifficulty is where every session starts, so two children on the
	// same lesson are treated the same until they answer differently.
	DefaultDifficulty = 2
	// StreakToAdvance is how many consecutive correct answers raise the level.
	// Two is deliberate: one lucky answer is not evidence of mastery, and a
	// longer streak keeps a slow learner at the same level for too long.
	StreakToAdvance = 2
	// PassingScore is the threshold that counts an answer as correct. It is the
	// same threshold English practice uses, so a word cannot be "passed" in one
	// feature and "wrong" in the other.
	PassingScore = 60

	// MaxItemsPerSession bounds one assessment. The device shows one item at a
	// time, and ten is a short enough run that a child does not tire.
	MaxItemsPerSession = 10
	// MaxSessionDuration bounds one assessment in wall-clock time. A session
	// left open on a paused device must not stay active forever.
	MaxSessionDuration = 10 * time.Minute
	// MaxSessionsPerWindow caps how often one family may start an assessment.
	MaxSessionsPerWindow = 20
	// SessionRateWindow is the window for MaxSessionsPerWindow.
	SessionRateWindow = time.Hour

	// DefaultHistoryLimit is how many past sessions a family sees by default.
	DefaultHistoryLimit = 20
	// MaxHistoryLimit is the largest history page accepted.
	MaxHistoryLimit = 100

	// ItemIDMaxLength and TargetTextMaxLength bound the stored item text. The
	// text is the lesson content the child is asked to say; it is bounded here
	// so a client cannot turn an answer row into free-form storage.
	ItemIDMaxLength     = 128
	TargetTextMaxLength = 160
)

// IsTerminal reports whether the session can no longer change state.
func (state SessionState) IsTerminal() bool {
	return state == StateCompleted || state == StateAbandoned
}

// AllowsAnswers reports whether answers may be recorded in this state.
func (state SessionState) AllowsAnswers() bool {
	return state == StateActive
}

// IsItemType reports whether value is a supported assessment item type.
func IsItemType(value string) bool {
	return value == ItemTypeWord || value == ItemTypeSentence
}

// ClampDifficulty keeps a level inside the supported range. It exists so a
// stored value that predates a rule change cannot push the curve out of range.
func ClampDifficulty(difficulty int) int {
	if difficulty < MinDifficulty {
		return MinDifficulty
	}
	if difficulty > MaxDifficulty {
		return MaxDifficulty
	}
	return difficulty
}

// IsCorrect reports whether a score clears the passing threshold. The threshold
// is inclusive: a child who exactly reaches it advanced.
func IsCorrect(score int) bool {
	return score >= PassingScore
}

// ScoreValid reports whether a score is inside the 0-100 range. An out-of-range
// score is rejected rather than clamped: a client that reports 150 is broken,
// and quietly accepting it would store a value the database forbids.
func ScoreValid(score int) bool {
	return score >= 0 && score <= 100
}

// AnswerInput is a validated, single reported answer.
type AnswerInput struct {
	// Position is the zero-based index the client is answering. It must equal
	// the session's AnsweredCount so a reply that was lost in transit is
	// retried with the same position, and a duplicate is rejected instead of
	// counted twice.
	Position   int
	ItemID     string
	ItemType   string
	TargetText string
	Score      int
}

// Validate normalises and checks one reported answer.
func (input AnswerInput) Validate() (AnswerInput, error) {
	normalized := AnswerInput{
		Position:   input.Position,
		ItemID:     strings.TrimSpace(input.ItemID),
		ItemType:   strings.TrimSpace(input.ItemType),
		TargetText: strings.TrimSpace(input.TargetText),
		Score:      input.Score,
	}
	if normalized.Position < 0 || normalized.ItemID == "" ||
		utf8.RuneCountInString(normalized.ItemID) > ItemIDMaxLength {
		return AnswerInput{}, ErrInvalidAnswer
	}
	if !IsItemType(normalized.ItemType) {
		return AnswerInput{}, ErrInvalidItemType
	}
	if normalized.TargetText == "" ||
		utf8.RuneCountInString(normalized.TargetText) > TargetTextMaxLength {
		return AnswerInput{}, ErrInvalidAnswer
	}
	if !ScoreValid(normalized.Score) {
		return AnswerInput{}, ErrInvalidScore
	}
	return normalized, nil
}

// Session is one bounded assessment run.
//
// Streak is derived from the stored answer history rather than persisted on the
// row: a replayed or corrected answer sequence must produce the same level, and
// a denormalised counter would drift the first time a write was retried.
type Session struct {
	ID       string
	FamilyID string
	// ChildID is empty for a family-level session. The single-session rule then
	// applies to the family as a whole.
	ChildID    string
	LessonID   string
	State      SessionState
	Difficulty int
	// ItemIndex is how many items the assessment has served; it equals the next
	// answer position.
	ItemIndex     int
	CorrectCount  int
	AnsweredCount int
	StartedAt     time.Time
	// ExpiresAt is the hard deadline. It is fixed at creation so the device can
	// enforce the same instant without asking the platform again.
	ExpiresAt    time.Time
	FinishedAt   *time.Time
	FinishReason string
	// Streak is the number of consecutive correct answers up to the last
	// answered position. It is filled from the answer history on read.
	Streak int
}

// IsExpired reports whether the session ran past its deadline. A session with
// no expiry instant is treated as live so a corrupt row cannot lock a family
// out of the feature.
func (session Session) IsExpired(now time.Time) bool {
	if session.ExpiresAt.IsZero() {
		return false
	}
	return !now.Before(session.ExpiresAt)
}

// RemainingItems returns how many items the session may still serve.
func (session Session) RemainingItems() int {
	remaining := MaxItemsPerSession - session.ItemIndex
	if remaining < 0 {
		return 0
	}
	return remaining
}

// Mastery returns the rounded percentage of correct answers. It returns 0 for
// a session without answers rather than dividing by zero, which also keeps the
// number a parent sees meaningful: nothing practised, nothing mastered.
func (session Session) Mastery() int {
	if session.AnsweredCount <= 0 {
		return 0
	}
	return int((int64(session.CorrectCount)*100 + int64(session.AnsweredCount)/2) /
		int64(session.AnsweredCount))
}

// Progress returns how far the session got, as a percentage of the item limit.
func (session Session) Progress() int {
	if session.ItemIndex <= 0 {
		return 0
	}
	asked := session.ItemIndex
	if asked > MaxItemsPerSession {
		asked = MaxItemsPerSession
	}
	return asked * 100 / MaxItemsPerSession
}

// Answer records one answer and moves the difficulty.
//
// Every precondition that could let a finished, expired or out-of-order answer
// through is checked here rather than in the service, so the same rules hold for
// the guardian route, the device retry, and any future caller.
func (session *Session) Answer(input AnswerInput, now time.Time) (Answer, error) {
	if session.State.IsTerminal() {
		return Answer{}, ErrSessionFinished
	}
	if !session.State.AllowsAnswers() {
		return Answer{}, ErrSessionFinished
	}
	if session.IsExpired(now) {
		return Answer{}, ErrSessionExpired
	}
	normalized, err := input.Validate()
	if err != nil {
		return Answer{}, err
	}
	if normalized.Position != session.AnsweredCount {
		return Answer{}, ErrUnexpectedPosition
	}

	correct := IsCorrect(normalized.Score)
	streak := session.Streak
	if correct {
		streak++
	} else {
		streak = 0
	}
	// The level moves first and the counters second, so a refused answer never
	// leaves the row half-updated.
	session.Difficulty = AdvanceDifficulty(session.Difficulty, correct, streak)
	session.Streak = streak
	session.AnsweredCount++
	session.ItemIndex = session.AnsweredCount
	if correct {
		session.CorrectCount++
	}

	answer := Answer{
		SessionID:  session.ID,
		Position:   normalized.Position,
		ItemID:     normalized.ItemID,
		ItemType:   normalized.ItemType,
		TargetText: normalized.TargetText,
		Score:      normalized.Score,
		IsCorrect:  correct,
		AnsweredAt: now.UTC(),
	}
	if session.RemainingItems() == 0 {
		// The item limit is checked after the counters move so the finished
		// session reports the full ten answers, not nine.
		_ = session.Finish(now, FinishReasonItemLimit)
	}
	return answer, nil
}

// Finish completes the session on purpose, for example when the guardian ends
// the practice early.
func (session *Session) Finish(now time.Time, reason string) error {
	if session.State.IsTerminal() {
		return ErrSessionFinished
	}
	finishedAt := now.UTC()
	session.State = StateCompleted
	session.FinishedAt = &finishedAt
	session.FinishReason = normaliseReason(reason, FinishReasonCompleted)
	return nil
}

// Abandon closes the session without it being finished. It is used by the
// expiry sweep: the child never completed the run, so calling it completed would
// overstate what happened and depress the reported mastery on purpose.
func (session *Session) Abandon(now time.Time, reason string) error {
	if session.State.IsTerminal() {
		return ErrSessionFinished
	}
	finishedAt := now.UTC()
	session.State = StateAbandoned
	session.FinishedAt = &finishedAt
	session.FinishReason = normaliseReason(reason, FinishReasonAbandoned)
	return nil
}

// Expire closes a session that ran out of time.
func (session *Session) Expire(now time.Time) error {
	return session.Abandon(now, FinishReasonExpired)
}

// AdvanceDifficulty applies the adaptive rule: two consecutive correct answers
// raise the level by one, one wrong answer lowers it by one, and everything else
// holds. The result is always inside 1..5.
func AdvanceDifficulty(current int, correct bool, streak int) int {
	level := ClampDifficulty(current)
	switch {
	case !correct:
		level--
	case streak >= StreakToAdvance:
		level++
	}
	return ClampDifficulty(level)
}

// Answer is one stored answer.
type Answer struct {
	ID         string
	SessionID  string
	Position   int
	ItemType   string
	ItemID     string
	TargetText string
	Score      int
	IsCorrect  bool
	AnsweredAt time.Time
}

// StartRequest is a guardian's validated intent to begin an assessment.
type StartRequest struct {
	FamilyID string
	ChildID  string
	LessonID string
	// DeviceID names the device the child will answer on. It is optional: a
	// guardian who does not pick one still gets a session, and the device then
	// reaches it through its own authenticated session id.
	DeviceID string
}

// Validate checks the request names a family and a well-formed lesson.
//
// Lesson identifier shape is checked by the service against the English catalog,
// not with a regular expression here, so this package never duplicates the
// catalog's identifier rule.
func (request StartRequest) Validate() error {
	if strings.TrimSpace(request.FamilyID) == "" {
		return ErrInvalidRequest
	}
	if strings.TrimSpace(request.LessonID) == "" {
		return ErrInvalidRequest
	}
	return nil
}

// normaliseReason keeps every reason a short stable token.
func normaliseReason(reason, fallback string) string {
	if reason == "" {
		return fallback
	}
	if len(reason) > 32 {
		return reason[:32]
	}
	return reason
}
