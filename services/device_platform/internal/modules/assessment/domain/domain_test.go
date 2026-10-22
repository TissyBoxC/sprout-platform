package domain_test

import (
	"errors"
	"testing"
	"time"

	"github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/assessment/domain"
)

func newSession() domain.Session {
	started := time.Date(2026, 10, 22, 9, 0, 0, 0, time.UTC)
	return domain.Session{
		ID:         "session-1",
		FamilyID:   "family-1",
		ChildID:    "child-1",
		LessonID:   "greetings_basic",
		State:      domain.StateActive,
		Difficulty: domain.DefaultDifficulty,
		StartedAt:  started,
		ExpiresAt:  started.Add(domain.MaxSessionDuration),
	}
}

// answerAt builds a valid answer for the position the session expects next.
func answerAt(session domain.Session, score int) domain.AnswerInput {
	return domain.AnswerInput{
		Position:   session.AnsweredCount,
		ItemID:     "item-1",
		ItemType:   domain.ItemTypeWord,
		TargetText: "hello",
		Score:      score,
	}
}

func TestAdvanceDifficulty(t *testing.T) {
	// The adaptive curve is the whole feature, so every boundary is checked
	// directly rather than only through a session.
	cases := []struct {
		name     string
		current  int
		correct  bool
		streak   int
		expected int
	}{
		{"wrong answer lowers by one", 3, false, 0, 2},
		{"wrong answer at the floor stays", 1, false, 0, 1},
		{"first correct answer holds", 2, true, 1, 2},
		{"second correct answer raises", 2, true, 2, 3},
		{"long streak only raises by one", 2, true, 7, 3},
		{"correct answer at the ceiling stays", 5, true, 2, 5},
		{"wrong answer at the ceiling drops", 5, false, 0, 4},
		{"stored value below the range is clamped", 0, true, 1, 1},
		{"stored value above the range is clamped", 9, false, 0, 4},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			got := domain.AdvanceDifficulty(testCase.current, testCase.correct, testCase.streak)
			if got != testCase.expected {
				t.Fatalf("AdvanceDifficulty(%d, %v, %d) = %d, want %d",
					testCase.current, testCase.correct, testCase.streak,
					got, testCase.expected)
			}
			if got < domain.MinDifficulty || got > domain.MaxDifficulty {
				t.Fatalf("difficulty %d escaped the supported range", got)
			}
		})
	}
}

func TestAnswerRaisesDifficultyAfterTwoCorrectAnswers(t *testing.T) {
	session := newSession()
	now := session.StartedAt

	for index := 0; index < 2; index++ {
		if _, err := session.Answer(answerAt(session, 90), now); err != nil {
			t.Fatalf("answer %d: %v", index, err)
		}
	}
	// One correct answer is not evidence of mastery; the second one is.
	if session.Difficulty != domain.DefaultDifficulty+1 {
		t.Fatalf("difficulty = %d, want %d", session.Difficulty, domain.DefaultDifficulty+1)
	}
	if session.CorrectCount != 2 || session.AnsweredCount != 2 || session.ItemIndex != 2 {
		t.Fatalf("counters = correct %d answered %d index %d, want 2/2/2",
			session.CorrectCount, session.AnsweredCount, session.ItemIndex)
	}
}

func TestAnswerLowersDifficultyOnOneWrongAnswer(t *testing.T) {
	session := newSession()
	now := session.StartedAt
	session.Difficulty = 4
	if _, err := session.Answer(answerAt(session, 10), now); err != nil {
		t.Fatalf("answer: %v", err)
	}
	if session.Difficulty != 3 {
		t.Fatalf("difficulty = %d, want 3", session.Difficulty)
	}
	if session.CorrectCount != 0 || session.AnsweredCount != 1 {
		t.Fatalf("counters = %d/%d, want 0/1", session.CorrectCount, session.AnsweredCount)
	}
	if session.Streak != 0 {
		t.Fatalf("streak = %d, want 0", session.Streak)
	}
}

func TestAnswerStreakResetsAfterAWrongAnswer(t *testing.T) {
	session := newSession()
	now := session.StartedAt
	scores := []int{90, 10, 90}
	for index, score := range scores {
		if _, err := session.Answer(answerAt(session, score), now); err != nil {
			t.Fatalf("answer %d: %v", index, err)
		}
	}
	// The miss lowered the level to 1, and the third answer only reaches a
	// streak of one, so it cannot raise the level back yet.
	if session.Difficulty != 1 {
		t.Fatalf("difficulty = %d, want 1", session.Difficulty)
	}
	if session.Streak != 1 {
		t.Fatalf("streak = %d, want 1", session.Streak)
	}
}

func TestAnswerRejectsFinishedSession(t *testing.T) {
	cases := []struct {
		name   string
		state  domain.SessionState
		finish func(*domain.Session) error
	}{
		{"completed", domain.StateCompleted, func(s *domain.Session) error {
			return s.Finish(s.StartedAt, domain.FinishReasonCompleted)
		}},
		{"abandoned", domain.StateAbandoned, func(s *domain.Session) error {
			return s.Abandon(s.StartedAt, domain.FinishReasonAbandoned)
		}},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			session := newSession()
			if err := testCase.finish(&session); err != nil {
				t.Fatalf("finish: %v", err)
			}
			if session.State != testCase.state {
				t.Fatalf("state = %s, want %s", session.State, testCase.state)
			}
			if _, err := session.Answer(answerAt(session, 90), session.StartedAt); !errors.Is(
				err, domain.ErrSessionFinished,
			) {
				t.Fatalf("err = %v, want ErrSessionFinished", err)
			}
		})
	}
}

func TestAnswerRejectsExpiredSessionBeforeTheWorkerRuns(t *testing.T) {
	session := newSession()
	// The expiry worker runs on an interval; an answer arriving between sweeps
	// must still be refused, otherwise the deadline is only advisory.
	if _, err := session.Answer(answerAt(session, 90), session.ExpiresAt); !errors.Is(
		err, domain.ErrSessionExpired,
	) {
		t.Fatalf("err = %v, want ErrSessionExpired", err)
	}
	if session.AnsweredCount != 0 {
		t.Fatalf("answered = %d, want 0", session.AnsweredCount)
	}
}

func TestAnswerRejectsUnexpectedPosition(t *testing.T) {
	session := newSession()
	if _, err := session.Answer(answerAt(session, 90), session.StartedAt); err != nil {
		t.Fatalf("first answer: %v", err)
	}
	// Replaying the position the device already reported must not count twice.
	duplicate := domain.AnswerInput{
		Position: 0, ItemID: "item-1", ItemType: domain.ItemTypeWord,
		TargetText: "hello", Score: 90,
	}
	if _, err := session.Answer(duplicate, session.StartedAt); !errors.Is(
		err, domain.ErrUnexpectedPosition,
	) {
		t.Fatalf("duplicate err = %v, want ErrUnexpectedPosition", err)
	}
	// Skipping ahead is equally refused: the sequence has no holes.
	skipped := domain.AnswerInput{
		Position: 5, ItemID: "item-2", ItemType: domain.ItemTypeWord,
		TargetText: "hello", Score: 90,
	}
	if _, err := session.Answer(skipped, session.StartedAt); !errors.Is(
		err, domain.ErrUnexpectedPosition,
	) {
		t.Fatalf("skipped err = %v, want ErrUnexpectedPosition", err)
	}
	if session.AnsweredCount != 1 {
		t.Fatalf("answered = %d, want 1", session.AnsweredCount)
	}
}

func TestAnswerRejectsMalformedInput(t *testing.T) {
	valid := domain.AnswerInput{
		Position: 0, ItemID: "item-1", ItemType: domain.ItemTypeWord,
		TargetText: "hello", Score: 80,
	}
	cases := []struct {
		name   string
		mutate func(*domain.AnswerInput)
		want   error
	}{
		{"negative score", func(input *domain.AnswerInput) { input.Score = -1 }, domain.ErrInvalidScore},
		{"score above one hundred", func(input *domain.AnswerInput) { input.Score = 101 }, domain.ErrInvalidScore},
		{"unknown item type", func(input *domain.AnswerInput) { input.ItemType = "phrase" }, domain.ErrInvalidItemType},
		{"blank item type", func(input *domain.AnswerInput) { input.ItemType = " " }, domain.ErrInvalidItemType},
		{"blank item id", func(input *domain.AnswerInput) { input.ItemID = "   " }, domain.ErrInvalidAnswer},
		{"blank target text", func(input *domain.AnswerInput) { input.TargetText = "" }, domain.ErrInvalidAnswer},
		{"negative position", func(input *domain.AnswerInput) { input.Position = -1 }, domain.ErrInvalidAnswer},
		{"item id too long", func(input *domain.AnswerInput) {
			input.ItemID = repeat("a", domain.ItemIDMaxLength+1)
		}, domain.ErrInvalidAnswer},
		{"target text too long", func(input *domain.AnswerInput) {
			input.TargetText = repeat("a", domain.TargetTextMaxLength+1)
		}, domain.ErrInvalidAnswer},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			input := valid
			testCase.mutate(&input)
			if _, err := input.Validate(); !errors.Is(err, testCase.want) {
				t.Fatalf("err = %v, want %v", err, testCase.want)
			}
		})
	}
}

func TestAnswerTrimsWhitespace(t *testing.T) {
	input := domain.AnswerInput{
		Position: 0, ItemID: "  item-1  ", ItemType: " word ",
		TargetText: "  hello  ", Score: 80,
	}
	normalized, err := input.Validate()
	if err != nil {
		t.Fatalf("validate: %v", err)
	}
	if normalized.ItemID != "item-1" || normalized.ItemType != "word" ||
		normalized.TargetText != "hello" {
		t.Fatalf("normalized = %+v", normalized)
	}
}

func TestAnswerAtPassingScoreCountsAsCorrect(t *testing.T) {
	cases := []struct {
		score   int
		correct bool
	}{
		{59, false},
		{domain.PassingScore, true},
		{100, true},
	}
	for _, testCase := range cases {
		session := newSession()
		answer, err := session.Answer(answerAt(session, testCase.score), session.StartedAt)
		if err != nil {
			t.Fatalf("answer: %v", err)
		}
		if answer.IsCorrect != testCase.correct {
			t.Fatalf("score %d correct = %v, want %v",
				testCase.score, answer.IsCorrect, testCase.correct)
		}
	}
}

func TestItemLimitCompletesTheSession(t *testing.T) {
	session := newSession()
	for index := 0; index < domain.MaxItemsPerSession; index++ {
		if _, err := session.Answer(answerAt(session, 90), session.StartedAt); err != nil {
			t.Fatalf("answer %d: %v", index, err)
		}
	}
	if session.State != domain.StateCompleted {
		t.Fatalf("state = %s, want completed", session.State)
	}
	if session.FinishReason != domain.FinishReasonItemLimit {
		t.Fatalf("reason = %q, want %q", session.FinishReason, domain.FinishReasonItemLimit)
	}
	if session.AnsweredCount != domain.MaxItemsPerSession || session.CorrectCount != domain.MaxItemsPerSession {
		t.Fatalf("counters = %d/%d, want %d/%d",
			session.CorrectCount, session.AnsweredCount,
			domain.MaxItemsPerSession, domain.MaxItemsPerSession)
	}
	if session.RemainingItems() != 0 {
		t.Fatalf("remaining = %d, want 0", session.RemainingItems())
	}
	// A finish reason of item_limit still reads as a normal mastery result.
	if session.Mastery() != 100 {
		t.Fatalf("mastery = %d, want 100", session.Mastery())
	}
}

func TestFinishAndAbandonAreTerminal(t *testing.T) {
	finished := newSession()
	if err := finished.Finish(finished.StartedAt, domain.FinishReasonCompleted); err != nil {
		t.Fatalf("finish: %v", err)
	}
	if !finished.State.IsTerminal() || finished.State.AllowsAnswers() {
		t.Fatalf("finished state = %s, want terminal", finished.State)
	}
	if finished.FinishedAt == nil {
		t.Fatal("finished session must record when it ended")
	}
	if err := finished.Finish(finished.StartedAt, domain.FinishReasonCompleted); !errors.Is(
		err, domain.ErrSessionFinished,
	) {
		t.Fatalf("second finish err = %v, want ErrSessionFinished", err)
	}

	abandoned := newSession()
	if err := abandoned.Abandon(abandoned.StartedAt, ""); err != nil {
		t.Fatalf("abandon: %v", err)
	}
	if abandoned.FinishReason != domain.FinishReasonAbandoned {
		t.Fatalf("reason = %q, want %q", abandoned.FinishReason, domain.FinishReasonAbandoned)
	}
}

func TestExpireAbandonsWithTheExpiredReason(t *testing.T) {
	session := newSession()
	if err := session.Expire(session.ExpiresAt); err != nil {
		t.Fatalf("expire: %v", err)
	}
	// A session that ran out of time was not finished by the child, so it must
	// not be reported as completed.
	if session.State != domain.StateAbandoned {
		t.Fatalf("state = %s, want abandoned", session.State)
	}
	if session.FinishReason != domain.FinishReasonExpired {
		t.Fatalf("reason = %q, want %q", session.FinishReason, domain.FinishReasonExpired)
	}
	if err := session.Expire(session.ExpiresAt); !errors.Is(err, domain.ErrSessionFinished) {
		t.Fatalf("second expire err = %v, want ErrSessionFinished", err)
	}
}

func TestFinishReasonIsBounded(t *testing.T) {
	session := newSession()
	long := repeat("a", 64)
	if err := session.Finish(session.StartedAt, long); err != nil {
		t.Fatalf("finish: %v", err)
	}
	if len(session.FinishReason) > 32 {
		t.Fatalf("reason length = %d, want <= 32", len(session.FinishReason))
	}
}

func TestMasteryRoundsInsteadOfTruncating(t *testing.T) {
	cases := []struct {
		name     string
		correct  int
		answered int
		expected int
	}{
		{"no answers", 0, 0, 0},
		{"all correct", 10, 10, 100},
		{"none correct", 0, 3, 0},
		{"rounds up from a third", 1, 3, 33},
		{"rounds a repeating fraction", 2, 3, 67},
		{"rounds a half up", 1, 8, 13},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			session := domain.Session{
				CorrectCount:  testCase.correct,
				AnsweredCount: testCase.answered,
			}
			if got := session.Mastery(); got != testCase.expected {
				t.Fatalf("mastery = %d, want %d", got, testCase.expected)
			}
		})
	}
}

func TestProgressTracksTheItemLimit(t *testing.T) {
	session := newSession()
	if session.Progress() != 0 {
		t.Fatalf("progress before answers = %d, want 0", session.Progress())
	}
	for index := 0; index < 5; index++ {
		if _, err := session.Answer(answerAt(session, 90), session.StartedAt); err != nil {
			t.Fatalf("answer %d: %v", index, err)
		}
	}
	if session.Progress() != 50 {
		t.Fatalf("progress = %d, want 50", session.Progress())
	}
}

func TestIsExpiredHandlesAZeroDeadline(t *testing.T) {
	session := newSession()
	session.ExpiresAt = time.Time{}
	// A row without a deadline must stay usable rather than lock the family out.
	if session.IsExpired(session.StartedAt.Add(time.Hour)) {
		t.Fatal("a zero deadline must not count as expired")
	}
}

func TestValidateStartRequest(t *testing.T) {
	cases := []struct {
		name    string
		request domain.StartRequest
		want    error
	}{
		{"valid", domain.StartRequest{FamilyID: "family-1", LessonID: "greetings_basic"}, nil},
		{"missing family", domain.StartRequest{LessonID: "greetings_basic"}, domain.ErrInvalidRequest},
		{"missing lesson", domain.StartRequest{FamilyID: "family-1"}, domain.ErrInvalidRequest},
		{"blank lesson", domain.StartRequest{FamilyID: "family-1", LessonID: "  "}, domain.ErrInvalidRequest},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			err := testCase.request.Validate()
			if !errors.Is(err, testCase.want) {
				t.Fatalf("err = %v, want %v", err, testCase.want)
			}
		})
	}
}

func TestSessionStateGatesAnswers(t *testing.T) {
	cases := map[domain.SessionState]bool{
		domain.StateActive:    true,
		domain.StateCompleted: false,
		domain.StateAbandoned: false,
	}
	for state, want := range cases {
		if got := state.AllowsAnswers(); got != want {
			t.Fatalf("AllowsAnswers(%s) = %v, want %v", state, got, want)
		}
	}
}

// repeat builds a string of count ASCII characters without importing strings in
// every subtest.
func repeat(value string, count int) string {
	built := make([]byte, 0, count)
	for index := 0; index < count; index++ {
		built = append(built, value[0])
	}
	return string(built)
}
