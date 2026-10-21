// Package domain contains the child long-term memory types shared by the
// platform modules.
//
// 术语表（唯一规范名，其他写法禁止）:
//   - child memory    -> ChildMemory（一条记忆：key/value/confidence/source）
//   - memory key      -> MemoryKey（如 favorite_color、pet_name）
//   - memory value    -> MemoryValue（记忆内容，可能包含儿童个人信息）
//   - memory revision -> Revision（单儿童单调递增的同步版本号）
//
// 记忆按儿童隔离：唯一约束是 (child_id, memory_key)，family_id 只用于越权
// 校验与级联清理。记忆值属于儿童个人信息，本包的类型都不实现 String()，
// 错误信息也不携带记忆值，因此记忆内容不会经由错误或日志外泄。
package domain

import (
	"errors"
	"regexp"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

var (
	ErrMemoryNotFound          = errors.New("child memory not found")
	ErrInvalidMemoryKey        = errors.New("invalid memory key")
	ErrInvalidMemoryValue      = errors.New("invalid memory value")
	ErrInvalidMemorySource     = errors.New("invalid memory source")
	ErrInvalidMemoryConfidence = errors.New("invalid memory confidence")
	ErrMemoryLimitExceeded     = errors.New("child memory limit exceeded")
	ErrInvalidMemoryInput      = errors.New("invalid memory input")
	ErrMemoryRevisionConflict  = errors.New("child memory revision conflict")
	ErrChildNotFound           = errors.New("child not found")
	ErrReadinessUnavailable    = errors.New("memory readiness is unavailable")
)

// Memory sources. The set is small and stable so the guardian UI can explain
// where a memory came from without parsing free-form client strings.
const (
	SourceConversation = "conversation"
	SourceGuardian     = "guardian"
	SourceDevice       = "device"
)

// Sources is the canonical ordered list shared by validation and the console.
var Sources = []string{SourceConversation, SourceGuardian, SourceDevice}

// Limits bound one child's memory store.
//
// DefaultChildMemoryLimit caps the stored key count per child. The limit is
// enforced by the server, never by the device, because a device has no
// visibility into what other devices already wrote for the same child.
const (
	DefaultChildMemoryLimit = 200
	MaxMemoryKeyRunes       = 64
	MaxMemoryValueRunes     = 280
	DefaultBatchSize        = 50
	MaxBatchSize            = DefaultChildMemoryLimit
)

// memoryKeyPattern keeps keys machine-addressable so the conversation service
// can look a fact up by name instead of fuzzy-matching free text.
var memoryKeyPattern = regexp.MustCompile(`^[a-z][a-z0-9_]{1,63}$`)

// ChildMemory is one long-term fact about a child.
//
// Revision is per child: every accepted write bumps the child's counter, so a
// device pulling with since_revision never misses a concurrent guardian edit.
// LastReinforcedAt records the last time the fact was confirmed again, which is
// what the retention window uses instead of the creation timestamp.
type ChildMemory struct {
	ID               string
	FamilyID         string
	ChildID          string
	MemoryKey        string
	MemoryValue      string
	Confidence       float64
	Source           string
	Revision         int64
	CreatedAt        time.Time
	UpdatedAt        time.Time
	LastReinforcedAt time.Time
}

// MemoryInput contains the fields accepted for one handwritten memory.
// Confidence is optional: a writer that cannot estimate it omits the pointer.
type MemoryInput struct {
	MemoryKey   string
	MemoryValue string
	Confidence  *float64
}

// MemoryRecord is one normalized, validated memory entry.
type MemoryRecord struct {
	MemoryKey   string
	MemoryValue string
	Confidence  float64
	Source      string
}

// MemoryPage is one consistent revision snapshot of a child's memory store.
//
// NextRevision is what the device persists as its sync cursor: it is the
// child's highest assigned revision at read time, so rows written during the
// response are not skipped by the next pull.
type MemoryPage struct {
	ChildID       string
	SinceRevision int64
	NextRevision  int64
	Memories      []ChildMemory
	DeletedKeys   []string
}

// MemorySummary is the non-sensitive portion of one stored memory, used where
// the value must not travel (for example bulk listings of a whole family).
type MemorySummary struct {
	MemoryKey        string
	Source           string
	Confidence       float64
	Revision         int64
	UpdatedAt        time.Time
	LastReinforcedAt time.Time
}

// ValidateMemoryKey normalizes and validates one memory key.
//
// Only lower_snake_case keys are accepted so the same fact has exactly one
// canonical name across device, gateway, and console.
func ValidateMemoryKey(key string) (string, error) {
	normalized := strings.ToLower(trimSpace(key))
	if normalized == "" ||
		utf8.RuneCountInString(normalized) > MaxMemoryKeyRunes ||
		!memoryKeyPattern.MatchString(normalized) {
		return "", ErrInvalidMemoryKey
	}
	return normalized, nil
}

// ValidateMemoryValue normalizes and validates one memory value.
//
// The value is trimmed and length-bounded but is never echoed in an error and
// never used as a log field or map key, so a hostile value cannot leak through
// an error path. Control characters are rejected because a device would
// otherwise render them as a broken utterance.
func ValidateMemoryValue(value string) (string, error) {
	normalized := trimSpace(value)
	if normalized == "" || utf8.RuneCountInString(normalized) > MaxMemoryValueRunes {
		return "", ErrInvalidMemoryValue
	}
	for _, character := range normalized {
		if unicode.IsControl(character) {
			return "", ErrInvalidMemoryValue
		}
	}
	return normalized, nil
}

// ValidateSource validates one memory source.
func ValidateSource(source string) (string, error) {
	normalized := trimSpace(source)
	for _, candidate := range Sources {
		if candidate == normalized {
			return normalized, nil
		}
	}
	return "", ErrInvalidMemorySource
}

// NormalizeConfidence validates an optional confidence and fills the default.
//
// A writer that cannot estimate confidence omits it; treating the absence as
// full confidence would let an unverified guess outrank a guardian edit.
func NormalizeConfidence(confidence *float64, source string) (float64, error) {
	if confidence == nil {
		return DefaultConfidenceFor(source), nil
	}
	value := *confidence
	if value != value || value < 0 || value > 1 { // NaN comparison is false
		return 0, ErrInvalidMemoryConfidence
	}
	return value, nil
}

// DefaultConfidenceFor is the confidence assigned when the writer omits it.
//
// A guardian statement is authoritative; a device-reported fact is a heuristic
// parsed from conversation, so it starts lower.
func DefaultConfidenceFor(source string) float64 {
	if source == SourceGuardian {
		return 1
	}
	return 0.6
}

// ValidateMemoryInput normalizes one handwritten memory for the given source.
func ValidateMemoryInput(input MemoryInput, source string) (MemoryRecord, error) {
	normalizedSource, err := ValidateSource(source)
	if err != nil {
		return MemoryRecord{}, err
	}
	key, err := ValidateMemoryKey(input.MemoryKey)
	if err != nil {
		return MemoryRecord{}, err
	}
	value, err := ValidateMemoryValue(input.MemoryValue)
	if err != nil {
		return MemoryRecord{}, err
	}
	confidence, err := NormalizeConfidence(input.Confidence, normalizedSource)
	if err != nil {
		return MemoryRecord{}, err
	}
	return MemoryRecord{
		MemoryKey:   key,
		MemoryValue: value,
		Confidence:  confidence,
		Source:      normalizedSource,
	}, nil
}

// ValidateMemoryRecord normalizes one batch entry whose source is already
// resolved. An invalid or missing entry rejects the whole batch.
func ValidateMemoryRecord(record MemoryRecord) (MemoryRecord, error) {
	source, err := ValidateSource(record.Source)
	if err != nil {
		return MemoryRecord{}, err
	}
	key, err := ValidateMemoryKey(record.MemoryKey)
	if err != nil {
		return MemoryRecord{}, err
	}
	value, err := ValidateMemoryValue(record.MemoryValue)
	if err != nil {
		return MemoryRecord{}, err
	}
	if err := validateConfidence(record.Confidence); err != nil {
		return MemoryRecord{}, err
	}
	return MemoryRecord{
		MemoryKey:   key,
		MemoryValue: value,
		Confidence:  record.Confidence,
		Source:      source,
	}, nil
}

func validateConfidence(value float64) error {
	if value != value || value < 0 || value > 1 { // NaN comparison is false
		return ErrInvalidMemoryConfidence
	}
	return nil
}

func trimSpace(value string) string {
	return strings.TrimSpace(value)
}
