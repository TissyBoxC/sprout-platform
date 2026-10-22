// Package service assembles a child's growth timeline from records other
// modules already store, and turns them into sentences a guardian reads.
//
// The service owns three decisions the repository cannot make. It decides which
// source rows are milestones (finished work only), it owns the Chinese copy so
// the wording lives in one place instead of three handlers, and it merges the
// per-source reads into one ordered story with a stable cursor.
//
// It never imports the repository: the composition root adapts it to the Source
// interface below and translates the repository's sentinels into the ones this
// package declares.
package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/timeline/domain"
)

var (
	// ErrChildNotFound means the child is unknown or belongs to another family.
	// Both cases answer identically so the endpoint cannot be used to discover
	// which child profiles exist.
	ErrChildNotFound = errors.New("timeline child not found")
	// ErrSourceUnavailable means a source table could not be read, so the story
	// would be incomplete. Serving a partial timeline as if it were whole is
	// worse than failing: a parent would conclude nothing happened.
	ErrSourceUnavailable = errors.New("timeline source unavailable")
)

// Source reads the records a timeline is built from.
//
// It is a read-only seam on purpose. The timeline owns no table, and a write
// method here would be the first step towards the duplicate event table this
// design exists to avoid.
type Source interface {
	// ChildInFamily reports whether the child profile belongs to the family.
	ChildInFamily(ctx context.Context, familyID, childID string) (bool, error)
	CompletedAssessments(ctx context.Context, familyID, childID string) ([]Assessment, error)
	EnglishPractices(ctx context.Context, familyID, childID string) ([]Practice, error)
	PublishedReports(ctx context.Context, familyID, childID string) ([]Report, error)
	CompletedContent(ctx context.Context, familyID, childID string) ([]ContentPlay, error)
	GrowthRecords(ctx context.Context, familyID, childID string) ([]GrowthRecord, error)
	CountEventsSince(ctx context.Context, since time.Time) (int64, error)
	CountActiveFamilies(ctx context.Context, since time.Time) (int64, error)
}

// Assessment is one finished assessment as the timeline may see it.
type Assessment struct {
	ID         string
	LessonID   string
	Answered   int
	Mastery    int
	FinishedAt time.Time
}

// Practice is one completed English practice attempt.
//
// It carries no score and no recognised text: a passed attempt is already the
// fact, and the raw result belongs to the practice screen, not to a keepsake.
type Practice struct {
	ID        string
	ItemType  string
	Practiced time.Time
}

// Report is one generated growth report.
type Report struct {
	ID          string
	PeriodType  string
	GeneratedAt time.Time
}

// ContentPlay is one content item played to the end.
type ContentPlay struct {
	ID        string
	PackageID string
	PlayedAt  time.Time
}

// GrowthRecord is one guardian-written record.
type GrowthRecord struct {
	ID         string
	Category   string
	Title      string
	RecordedAt time.Time
}

// Options are the collaborators the service needs.
type Options struct {
	Source Source
	Logger *slog.Logger
}

// Service assembles timelines.
type Service struct {
	source Source
	logger *slog.Logger
	now    func() time.Time
	// location is the zone a "day" is counted in for streaks. The family has no
	// stored time zone of its own, and the usage ledger only upgrades a zone
	// when a device reports one, so streak days are counted here durably rather
	// than switching zone whenever the newest device report says so.
	location *time.Location
}

// NewService builds the service.
func NewService(options Options) *Service {
	logger := options.Logger
	if logger == nil {
		logger = slog.Default()
	}
	return &Service{
		source:   options.Source,
		logger:   logger,
		now:      func() time.Time { return time.Now().UTC() },
		location: time.UTC,
	}
}

// List returns one page of the child's timeline, oldest first.
//
// before is the opaque cursor from the previous page; empty means the first
// page. limit is clamped into 1..100 with 30 as the default. A family that has
// just been created has no records at all, and that is an ordinary empty page,
// not an error.
func (service *Service) List(
	ctx context.Context,
	familyID, childID, before string,
	limit int,
) ([]domain.Event, string, error) {
	if err := domain.ValidateLimit(limit); err != nil {
		return nil, "", err
	}
	// The cursor is parsed before any read so a malformed one costs nothing.
	cursor, err := domain.ParseCursor(before)
	if err != nil {
		return nil, "", err
	}
	events, err := service.collect(ctx, familyID, childID)
	if err != nil {
		return nil, "", err
	}
	page := domain.Paginate(events, cursor, limit)

	service.logger.Info("timeline page served",
		"family_id", familyID,
		"event_count", len(page.Events),
		"kind_count", distinctKinds(page.Events))
	return page.Events, page.NextCursor, nil
}

// Milestones returns the counters shown above the timeline.
func (service *Service) Milestones(
	ctx context.Context,
	familyID, childID string,
) (domain.Milestones, error) {
	events, err := service.collect(ctx, familyID, childID)
	if err != nil {
		return domain.Milestones{}, err
	}
	milestones := domain.BuildMilestones(events, service.location)
	service.logger.Info("timeline milestones counted",
		"family_id", familyID,
		"event_count", milestones.Total,
		"kind_count", distinctKinds(events))
	return milestones, nil
}

// Metrics reports operational counters for the feature center.
func (service *Service) Metrics(ctx context.Context) (Metrics, error) {
	if service.source == nil {
		return Metrics{}, ErrSourceUnavailable
	}
	since := service.now().Add(-24 * time.Hour)
	events, err := service.source.CountEventsSince(ctx, since)
	if err != nil {
		return Metrics{}, fmt.Errorf("count timeline events: %w", err)
	}
	families, err := service.source.CountActiveFamilies(ctx, since)
	if err != nil {
		return Metrics{}, fmt.Errorf("count timeline families: %w", err)
	}
	return Metrics{EventsLast24h: events, ActiveFamilies: families}, nil
}

// Metrics are read-only counters exposed to operators. They carry no child
// identifier and no event content.
type Metrics struct {
	EventsLast24h  int64
	ActiveFamilies int64
}

// Readiness reports whether a timeline can be produced at all.
//
// The probe reads the real sources rather than trusting process state: a
// missing source table means every guardian sees an empty story, which an
// operator must be able to tell apart from a quiet week.
func (service *Service) Readiness(ctx context.Context) (domain.Readiness, error) {
	if service.source == nil {
		return domain.Readiness{State: domain.ReadinessUnavailable}, nil
	}
	if _, err := service.source.CountEventsSince(ctx, service.now().Add(-24*time.Hour)); err != nil {
		return domain.Readiness{}, fmt.Errorf("read timeline source: %w", err)
	}
	return domain.Readiness{State: domain.ReadinessHealthy}, nil
}

// collect reads every source for one child and merges them into a sorted story.
//
// A read failure is reported rather than skipped. The alternative — degrading to
// whatever loaded — would show a parent a gap in their child's history and call
// it complete.
func (service *Service) collect(
	ctx context.Context,
	familyID, childID string,
) ([]domain.Event, error) {
	if err := domain.ValidateScope(familyID, childID); err != nil {
		return nil, err
	}
	if service.source == nil {
		return nil, ErrSourceUnavailable
	}
	if childID == "" {
		// The timeline is per child by definition. Without a child there is no
		// story to tell, and guessing the family's "main" child would mix two
		// siblings' histories together.
		return nil, domain.ErrInvalidScope
	}
	known, err := service.source.ChildInFamily(ctx, familyID, childID)
	if err != nil {
		return nil, fmt.Errorf("check timeline child: %w", err)
	}
	if !known {
		return nil, ErrChildNotFound
	}

	assessments, err := service.source.CompletedAssessments(ctx, familyID, childID)
	if err != nil {
		return nil, fmt.Errorf("read timeline assessments: %w", err)
	}
	practices, err := service.source.EnglishPractices(ctx, familyID, childID)
	if err != nil {
		return nil, fmt.Errorf("read timeline practice: %w", err)
	}
	reports, err := service.source.PublishedReports(ctx, familyID, childID)
	if err != nil {
		return nil, fmt.Errorf("read timeline reports: %w", err)
	}
	plays, err := service.source.CompletedContent(ctx, familyID, childID)
	if err != nil {
		return nil, fmt.Errorf("read timeline content: %w", err)
	}
	records, err := service.source.GrowthRecords(ctx, familyID, childID)
	if err != nil {
		return nil, fmt.Errorf("read timeline records: %w", err)
	}

	events := make([]domain.Event, 0,
		len(assessments)+len(practices)+len(reports)+len(plays)+len(records))
	for _, assessment := range assessments {
		events = append(events, assessmentEvent(assessment))
	}
	for _, practice := range practices {
		events = append(events, practiceEvent(practice))
	}
	for _, report := range reports {
		events = append(events, reportEvent(report))
	}
	for _, play := range plays {
		events = append(events, contentEvent(play))
	}
	for _, record := range records {
		if event, ok := recordEvent(record); ok {
			events = append(events, event)
		}
	}
	domain.SortEvents(events)
	return events, nil
}

// distinctKinds reports how many different kinds a page contained. It is a
// count for the log line, never a list of what happened.
func distinctKinds(events []domain.Event) int {
	seen := make(map[domain.Kind]bool, len(events))
	for _, event := range events {
		seen[event.Kind] = true
	}
	return len(seen)
}
