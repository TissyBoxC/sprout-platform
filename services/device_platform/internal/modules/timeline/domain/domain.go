// Package domain defines the growth timeline: a read-only story of what a child
// already did, assembled from records the platform stores elsewhere.
//
// The timeline is an aggregate view, not a new record. It owns no table and no
// event feed: every entry is derived from a row another module already wrote
// (an assessment, a practice attempt, a generated report, a guardian's growth
// record). That is deliberate, because a second event table would drift from
// the records it claims to summarise the first time either side changed.
//
// Two rules shape what may appear here. Only finished work is a milestone: a
// started-but-abandoned assessment or an interrupted playback is not something
// a child achieved, and counting it would flatter the count. And an event
// carries a stable kind plus a Chinese sentence, never raw material: no score
// breakdown, no recognised speech, no audio, no lesson text. A guardian reads
// the line; nothing in it can be replayed or quoted back at the child.
package domain

import (
	"errors"
	"sort"
	"strings"
	"time"
)

var (
	// ErrInvalidCursor means the pagination cursor is malformed or belongs to a
	// different child.
	ErrInvalidCursor = errors.New("invalid timeline cursor")
	// ErrInvalidLimit means the requested page size is outside 1..100.
	ErrInvalidLimit = errors.New("invalid timeline limit")
	// ErrInvalidScope means the request does not name a family or child.
	ErrInvalidScope = errors.New("invalid timeline scope")
	// ErrChildNotFound means the child is unknown or belongs to another family.
	// Both cases answer identically so the endpoint cannot be used to discover
	// which child profiles exist.
	ErrChildNotFound = errors.New("timeline child not found")
)

// Kind is the stable event type a client switches on.
//
// The values are part of the contract: a parent app renders an icon and a card
// per kind, so renaming one is a breaking change and the Chinese sentence must
// stay a description of the same fact.
type Kind string

const (
	// KindAssessmentCompleted is an assessment the child finished, whether by
	// answering every item, reaching the item limit, or a guardian ending it.
	KindAssessmentCompleted Kind = "assessment_completed"
	// KindEnglishPractice is a completed English practice round (word or
	// sentence) the child passed.
	KindEnglishPractice Kind = "english_practice"
	// KindReportPublished is a weekly or monthly report the platform generated.
	KindReportPublished Kind = "report_published"
	// KindMilestone is a growth record a guardian marked as a milestone.
	KindMilestone Kind = "milestone"
	// KindContentCompleted is a content item the child played to the end.
	KindContentCompleted Kind = "content_completed"
)

// CountsAsMilestone reports whether an event of this kind feeds the milestone
// counters.
//
// Every kind defined today counts, because the query layer already filters out
// unfinished work. The method exists so a future kind (a reminder, a device
// pairing) can be added without silently inflating a child's achievements.
func (kind Kind) CountsAsMilestone() bool {
	switch kind {
	case KindAssessmentCompleted, KindEnglishPractice, KindReportPublished,
		KindMilestone, KindContentCompleted:
		return true
	default:
		return false
	}
}

// IsSupported reports whether the value is a kind this module produces.
func (kind Kind) IsSupported() bool {
	return kind.CountsAsMilestone()
}

const (
	// DefaultLimit is the page size when the guardian does not ask for one.
	DefaultLimit = 30
	// MinLimit and MaxLimit bound one page. A timeline is a scrolling list, so a
	// small page keeps the first paint fast on a phone over a slow network.
	MinLimit = 1
	MaxLimit = 100

	// MaxSubjectRunes bounds the short subject a caller may attach to a
	// practice or report event. Longer text is cut rather than refused: the
	// subject is descriptive, not the record itself.
	MaxSubjectRunes = 40
)

// Event is one entry in a child's growth timeline.
//
// ID is stable and unique within the timeline: it is derived from the source
// table and the source row, so re-reading the same page returns the same
// identifiers and a client can de-duplicate without guessing.
type Event struct {
	// ID is "<kind>:<source row id>".
	ID          string
	Kind        Kind
	OccurredAt  time.Time
	Title       string
	Description string
	// Subject is a short non-sensitive noun the title refers to, such as a
	// practice item type. It is empty when the kind needs none.
	Subject string
}

// SortEvents orders events oldest first and then by identity.
//
// The secondary key matters for paging: several sources are merged in memory,
// and two events can share a timestamp when a device reports a finished item
// and a policy gate in the same second. Without a deterministic tie-break the
// cursor would skip or repeat an entry between pages.
func SortEvents(events []Event) {
	sort.SliceStable(events, func(left, right int) bool {
		if !events[left].OccurredAt.Equal(events[right].OccurredAt) {
			return events[left].OccurredAt.Before(events[right].OccurredAt)
		}
		return events[left].ID < events[right].ID
	})
}

// Cursor is the position a page continues from.
//
// It is the identity of the last event the previous page returned. Identity
// rather than a timestamp keeps the boundary exact: the whole timeline is read
// and cut here, so an event stored later with an earlier occurrence time still
// lands after the cursor instead of being skipped.
type Cursor struct {
	// OccurredAt and EventID identify the last returned event. They travel
	// together so a malformed cursor that names only half of a position is
	// rejected instead of being interpreted against the wrong event.
	OccurredAt time.Time
	EventID    string
}

// IsZero reports whether the cursor points at the start of the timeline.
func (cursor Cursor) IsZero() bool {
	return cursor.EventID == ""
}

// String encodes the cursor as an opaque token.
//
// The format is deliberately readable during operations (RFC3339 plus the
// event identity): a support engineer can tell where a stuck page stopped.
// Clients must treat it as opaque and pass it back unchanged.
func (cursor Cursor) String() string {
	if cursor.IsZero() {
		return ""
	}
	return cursor.OccurredAt.UTC().Format(time.RFC3339Nano) + "|" + cursor.EventID
}

// ParseCursor decodes the token a client sent back.
//
// An empty token is the first page, not an error. Anything that does not decode
// cleanly is rejected: quietly starting from the beginning would make a broken
// client loop over the same first page forever.
func ParseCursor(raw string) (Cursor, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return Cursor{}, nil
	}
	separator := strings.LastIndex(trimmed, "|")
	if separator <= 0 || separator == len(trimmed)-1 {
		return Cursor{}, ErrInvalidCursor
	}
	occurredAt, err := time.Parse(time.RFC3339Nano, trimmed[:separator])
	if err != nil {
		return Cursor{}, ErrInvalidCursor
	}
	eventID := trimmed[separator+1:]
	if strings.ContainsRune(eventID, '|') || len(eventID) > 256 {
		return Cursor{}, ErrInvalidCursor
	}
	return Cursor{OccurredAt: occurredAt.UTC(), EventID: eventID}, nil
}

// NormaliseLimit clamps a requested page size into the supported range.
func NormaliseLimit(limit int) int {
	if limit <= 0 {
		return DefaultLimit
	}
	if limit > MaxLimit {
		return MaxLimit
	}
	return limit
}

// ValidateLimit rejects a page size the contract does not allow.
//
// An out-of-range limit is refused rather than clamped, unlike a missing one:
// a client asking for 500 is confused about the contract, and answering with
// 100 hides that until the next page silently ends early.
func ValidateLimit(limit int) error {
	if limit == 0 {
		return nil
	}
	if limit < MinLimit || limit > MaxLimit {
		return ErrInvalidLimit
	}
	return nil
}

// ValidateScope checks the request names a family and, optionally, a child.
func ValidateScope(familyID, childID string) error {
	if strings.TrimSpace(familyID) == "" {
		return ErrInvalidScope
	}
	if len(childID) > 128 {
		return ErrInvalidScope
	}
	return nil
}

// Page is one window of the merged timeline.
type Page struct {
	Events []Event
	// NextCursor is empty when the page reached the newest event.
	NextCursor string
}

// Paginate cuts one page out of an already merged, already sorted timeline.
//
// The whole story is materialised before it is cut because the sources live in
// different tables: there is no single column to page on across them, and
// ordering a union by timestamp would still not give a stable boundary when
// timestamps collide. A child's history is bounded by their age, so reading it
// all costs little; the alternative is a cursor per source and a merge that
// silently drops entries.
func Paginate(events []Event, cursor Cursor, limit int) Page {
	limit = NormaliseLimit(limit)
	start := 0
	if !cursor.IsZero() {
		start = sort.Search(len(events), func(index int) bool {
			return eventAfterCursor(events[index], cursor)
		})
	}
	page := Page{Events: make([]Event, 0, limit)}
	for index := start; index < len(events) && len(page.Events) < limit; index++ {
		page.Events = append(page.Events, events[index])
	}
	if start+len(page.Events) < len(events) && len(page.Events) > 0 {
		last := page.Events[len(page.Events)-1]
		page.NextCursor = Cursor{OccurredAt: last.OccurredAt, EventID: last.ID}.String()
	}
	return page
}

// eventAfterCursor reports whether an event sorts strictly after the cursor.
func eventAfterCursor(event Event, cursor Cursor) bool {
	if !event.OccurredAt.Equal(cursor.OccurredAt) {
		return event.OccurredAt.After(cursor.OccurredAt)
	}
	return event.ID > cursor.EventID
}

// Milestones are the counters a guardian sees above the timeline.
//
// Every number counts finished work only. Streaks are computed over the days a
// child practised rather than over records, so practising ten times in one
// afternoon does not read as a ten-day streak.
type Milestones struct {
	// CompletedAssessments counts assessments the child finished.
	CompletedAssessments int
	// PracticeDays counts distinct calendar days with a passed practice.
	PracticeDays int
	// CurrentStreakDays is the run of consecutive practice days ending on the
	// most recent practice day.
	CurrentStreakDays int
	// LongestStreakDays is the longest such run in the whole history.
	LongestStreakDays int
	// PublishedReports counts reports generated for the child.
	PublishedReports int
	// MarkedMilestones counts growth records a guardian marked as a milestone.
	MarkedMilestones int
	// CompletedContent counts content items played to the end.
	CompletedContent int
	// Total counts every timeline event that qualifies as a milestone.
	Total int
}

// BuildMilestones counts a whole (already filtered) timeline.
//
// It takes the merged events rather than a second set of queries so the
// counters and the list can never disagree: a parent who counts the cards in
// the timeline gets the numbers above the list.
//
// Practice days are counted in the location given, because a "day" for a
// family is their calendar day. The caller passes the family's zone; an empty
// zone means UTC.
func BuildMilestones(events []Event, location *time.Location) Milestones {
	if location == nil {
		location = time.UTC
	}
	var milestones Milestones
	practiceDays := make([]time.Time, 0, len(events))
	for _, event := range events {
		if !event.Kind.CountsAsMilestone() {
			continue
		}
		milestones.Total++
		switch event.Kind {
		case KindAssessmentCompleted:
			milestones.CompletedAssessments++
		case KindEnglishPractice:
			milestones.PracticeDays++
			practiceDays = append(practiceDays, startOfDay(event.OccurredAt, location))
		case KindReportPublished:
			milestones.PublishedReports++
		case KindMilestone:
			milestones.MarkedMilestones++
		case KindContentCompleted:
			milestones.CompletedContent++
		}
	}
	days := uniqueDays(practiceDays)
	milestones.PracticeDays = len(days)
	milestones.CurrentStreakDays, milestones.LongestStreakDays = streakDays(days)
	return milestones
}

// startOfDay returns midnight of the event's calendar day in location.
func startOfDay(instant time.Time, location *time.Location) time.Time {
	local := instant.In(location)
	return time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, location)
}

// uniqueDays sorts days and removes duplicates.
func uniqueDays(days []time.Time) []time.Time {
	if len(days) == 0 {
		return nil
	}
	sort.Slice(days, func(left, right int) bool { return days[left].Before(days[right]) })
	unique := days[:1]
	for _, day := range days[1:] {
		if day.Equal(unique[len(unique)-1]) {
			continue
		}
		unique = append(unique, day)
	}
	return unique
}

// streakDays returns the run ending on the newest day and the longest run.
//
// The current streak deliberately ends on the newest practice day rather than
// on today: a child who practised all last week and not yet today has a streak,
// and resetting it at midnight would punish them for a normal morning.
func streakDays(days []time.Time) (int, int) {
	if len(days) == 0 {
		return 0, 0
	}
	longest := 1
	run := 1
	for index := 1; index < len(days); index++ {
		if days[index].Sub(days[index-1]) == 24*time.Hour {
			run++
		} else {
			run = 1
		}
		if run > longest {
			longest = run
		}
	}
	return run, longest
}

// eventIdentity builds the stable "<kind>:<source row id>" identity.
//
// The source identifier is kept verbatim rather than re-encoded: it is a UUID
// for every row-backed event and an identifier the client already knows for the
// rest, and it only has to be unique inside one child's timeline.
func eventIdentity(kind Kind, sourceID string) string {
	trimmed := strings.TrimSpace(sourceID)
	return string(kind) + ":" + trimmed
}

// NewEvent builds one timeline event with a stable identity.
func NewEvent(kind Kind, sourceID string, occurredAt time.Time, title, description, subject string) Event {
	return Event{
		ID:          eventIdentity(kind, sourceID),
		Kind:        kind,
		OccurredAt:  occurredAt.UTC(),
		Title:       title,
		Description: description,
		Subject:     truncateRunes(strings.TrimSpace(subject), MaxSubjectRunes),
	}
}

// truncateRunes cuts text to the limit in runes so a multi-byte character is
// never split into an invalid sequence.
func truncateRunes(text string, limit int) string {
	runes := []rune(text)
	if len(runes) <= limit {
		return text
	}
	return string(runes[:limit])
}
