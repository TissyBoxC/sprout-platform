// Package domain contains English learning types shared by the platform
// modules.
//
// The platform owns the lesson catalog (lessons, words, sentence patterns) and
// the guardian-visible practice progress. Everything is family-scoped: a
// guardian only ever reads or writes progress owned by the authenticated
// family, and the catalog is read-only to guardians.
package domain

import (
	"errors"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"
)

var (
	ErrLessonNotFound       = errors.New("english lesson not found")
	ErrAttemptNotFound      = errors.New("english practice attempt not found")
	ErrInvalidLesson        = errors.New("invalid english lesson")
	ErrInvalidWord          = errors.New("invalid english word")
	ErrInvalidSentence      = errors.New("invalid english sentence")
	ErrInvalidAttempt       = errors.New("invalid english practice attempt")
	ErrInvalidAgeTier       = errors.New("invalid english age tier")
	ErrInvalidLessonID      = errors.New("invalid english lesson id")
	ErrInvalidItemType      = errors.New("invalid english practice item type")
	ErrPracticeItemNotFound = errors.New("english practice item not found")
	ErrChildNotFound        = errors.New("english child profile not found")
	ErrChildFamilyMismatch  = errors.New("english child profile does not belong to family")
	ErrReadinessUnavailable = errors.New("english learning readiness is unavailable")
)

// Age tiers reused from the child profile so the catalog matches the child's
// age band without a second mapping.
const (
	AgeTier3To4 = "age_3_4"
	AgeTier5To6 = "age_5_6"
	AgeTier7To8 = "age_7_8"
)

// AgeTiers is the canonical ordered list shared by validation and the console.
var AgeTiers = []string{AgeTier3To4, AgeTier5To6, AgeTier7To8}

// Lesson lifecycle. A draft lesson is not delivered to the device; a withdrawn
// lesson stays in the catalog so the device can clean up its cache.
const (
	LessonStatusDraft     = "draft"
	LessonStatusPublished = "published"
	LessonStatusWithdrawn = "withdrawn"
)

// Practice item types: a single word or a full sentence pattern.
const (
	ItemTypeWord     = "word"
	ItemTypeSentence = "sentence"
)

// Bounded field limits, mirrored by the migration CHECK constraints.
const (
	LessonTitleMaxLength   = 80
	LessonSummaryMaxLength = 200
	WordTermMaxLength      = 40
	WordPhoneticMaxLength  = 40
	WordGlossMaxLength     = 60
	WordExampleMaxLength   = 160
	SentencePatternMax     = 120
	SentencePromptMax      = 120
	SentenceAnswerMax      = 160
	RecognizedTextMax      = 200
	PassingScore           = 60
)

// lessonIDPattern matches a stable, human-readable lesson identifier.
var lessonIDPattern = regexp.MustCompile(`^[a-z][a-z0-9]*(?:_[a-z0-9]+){1,7}$`)

// Word is one vocabulary item under a lesson.
type Word struct {
	ID          string
	LessonID    string
	Position    int
	Term        string
	Phonetic    string
	Translation string
	Example     string
}

// Sentence is one reusable spoken pattern under a lesson.
type Sentence struct {
	ID          string
	LessonID    string
	Position    int
	Pattern     string
	Prompt      string
	ModelAnswer string
}

// Lesson is one English lesson with its ordered words and sentences.
type Lesson struct {
	LessonID  string
	AgeTier   string
	Title     string
	Summary   string
	Sequence  int
	Status    string
	Words     []Word
	Sentence  []Sentence
	CreatedAt time.Time
	UpdatedAt time.Time
}

// PracticeAttempt is one pronunciation-feedback record.
type PracticeAttempt struct {
	ID             string
	FamilyID       string
	ChildID        string
	LessonID       string
	ItemID         string
	ItemType       string
	TargetText     string
	RecognizedText string
	Score          int
	Passed         bool
	PracticedAt    time.Time
	CreatedAt      time.Time
}

// AttemptInput contains the reported fields of one practice attempt.
type AttemptInput struct {
	ChildID        string
	LessonID       string
	ItemID         string
	ItemType       string
	TargetText     string
	RecognizedText string
	Score          int
}

// AttemptQuery bounds one progress read.
type AttemptQuery struct {
	ChildID  string
	LessonID string
	Limit    int
}

// ProgressItem summarizes one practiced item.
type ProgressItem struct {
	LessonID   string
	ItemID     string
	ItemType   string
	TargetText string
	BestScore  int
	Attempts   int
	Passed     bool
	LastAt     time.Time
}

// LessonProgress is the guardian-visible rollup for one lesson.
type LessonProgress struct {
	LessonID      string
	Attempts      int
	PassedItems   int
	TotalItems    int
	BestScore     int
	AverageScore  int
	LastPracticed time.Time
}

// IsAgeTier reports whether value is a platform-approved age band.
func IsAgeTier(value string) bool {
	for _, tier := range AgeTiers {
		if tier == value {
			return true
		}
	}
	return false
}

// IsItemType reports whether value is a supported practice item type.
func IsItemType(value string) bool {
	return value == ItemTypeWord || value == ItemTypeSentence
}

// LessonIDValid reports whether value is a well-formed lesson identifier.
func LessonIDValid(value string) bool {
	return lessonIDPattern.MatchString(value)
}

// ValidateAttemptInput normalizes and validates one reported attempt.
//
// The caller supplies the target text and the recognized text; the score is
// clamped to 0-100 because a device may report a slightly out-of-range value
// from a noisy microphone. Passed is derived from the score so a client cannot
// claim a pass it did not earn.
func ValidateAttemptInput(input AttemptInput) (AttemptInput, error) {
	normalized := AttemptInput{
		ChildID:        strings.TrimSpace(input.ChildID),
		LessonID:       strings.TrimSpace(input.LessonID),
		ItemID:         strings.TrimSpace(input.ItemID),
		ItemType:       strings.TrimSpace(input.ItemType),
		TargetText:     strings.TrimSpace(input.TargetText),
		RecognizedText: strings.TrimSpace(input.RecognizedText),
		Score:          input.Score,
	}
	if !LessonIDValid(normalized.LessonID) {
		return AttemptInput{}, ErrInvalidLessonID
	}
	if normalized.ItemID == "" {
		return AttemptInput{}, ErrInvalidAttempt
	}
	if !IsItemType(normalized.ItemType) {
		return AttemptInput{}, ErrInvalidItemType
	}
	if lengthInRunes(normalized.TargetText) < 1 ||
		lengthInRunes(normalized.TargetText) > SentenceAnswerMax {
		return AttemptInput{}, ErrInvalidAttempt
	}
	if lengthInRunes(normalized.RecognizedText) > RecognizedTextMax {
		return AttemptInput{}, ErrInvalidAttempt
	}
	if normalized.Score < 0 {
		normalized.Score = 0
	}
	if normalized.Score > 100 {
		normalized.Score = 100
	}
	return normalized, nil
}

// AttemptPassed reports whether a score clears the passing threshold.
func AttemptPassed(score int) bool {
	return score >= PassingScore
}

// lengthInRunes returns the number of runes, so a limit is measured in
// characters rather than bytes.
func lengthInRunes(value string) int {
	return utf8.RuneCountInString(value)
}
