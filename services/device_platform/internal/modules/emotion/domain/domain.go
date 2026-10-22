// Package domain defines mood sensing: a coarse, aggregated picture of how a
// child has been feeling lately, so a guardian can adjust how they spend time
// together.
//
// This is not emotion surveillance. The platform stores one label, one
// confidence, and one observation instant per sample, and nothing else: no
// image, no audio, no transcript, not even the sentence a child was saying. A
// guardian reads a distribution over a period, never a moment, because "at
// 19:42 the child was frustrated" is a record of a child's inner state that
// nobody needs to keep.
//
// Four rules shape everything here. Sensing is off until a guardian turns it
// on, a device may only report about the child it is bound to, a sample must be
// recent but not from the future, and the summary is aggregate-only by
// construction: it exposes counts and shares, never a timestamped event.
package domain

import (
	"errors"
	"strings"
	"time"
)

var (
	// ErrInvalidRequest means the request cannot address a real child or family.
	ErrInvalidRequest = errors.New("invalid emotion request")
	// ErrInvalidLabel means the label is not one of the supported emotions.
	ErrInvalidLabel = errors.New("invalid emotion label")
	// ErrInvalidConfidence means the confidence falls outside 0-100.
	ErrInvalidConfidence = errors.New("invalid emotion confidence")
	// ErrObservationFuture means the observation instant is meaningfully ahead
	// of the platform clock.
	ErrObservationFuture = errors.New("emotion observation is in the future")
	// ErrObservationTooOld means the observation instant is older than the
	// accepted window, so it belongs to data the feature no longer collects.
	ErrObservationTooOld = errors.New("emotion observation is too old")
	// ErrDeviceNotFound means the device is unknown or belongs to another
	// family. Both cases answer identically so the endpoint cannot be used to
	// discover which devices exist.
	ErrDeviceNotFound = errors.New("emotion device not found")
	// ErrChildNotBound means the device is not currently bound to the reported
	// child. A device may only speak about the child it serves.
	ErrChildNotBound = errors.New("emotion child is not bound to this device")
	// ErrInvalidDays means the summary window is outside the accepted range.
	ErrInvalidDays = errors.New("invalid emotion summary window")
	// ErrInvalidRetention means the retention period is outside 1..90 days.
	ErrInvalidRetention = errors.New("invalid emotion retention days")
)

// Label is one coarse emotion from the fixed vocabulary.
//
// The vocabulary is deliberately small and neutral. Every value describes a
// feeling a young child can be in, so a guardian reading a distribution is not
// shown a diagnosis.
type Label string

const (
	LabelCalm       Label = "calm"
	LabelHappy      Label = "happy"
	LabelCurious    Label = "curious"
	LabelTired      Label = "tired"
	LabelFrustrated Label = "frustrated"
	LabelSad        Label = "sad"
	LabelUpset      Label = "upset"
)

// AllLabels returns the complete vocabulary in a stable order.
//
// The order is the one a guardian reads: settled and positive first, then
// tiring, then difficult. Callers must not invent their own order, otherwise
// the same distribution renders differently in two places.
func AllLabels() []Label {
	return []Label{
		LabelCalm,
		LabelHappy,
		LabelCurious,
		LabelTired,
		LabelFrustrated,
		LabelSad,
		LabelUpset,
	}
}

const (
	// DefaultRetentionDays is how long samples are kept when the guardian has
	// not chosen otherwise. It matches the schema default.
	DefaultRetentionDays = 30
	// MinRetentionDays and MaxRetentionDays bound the stored retention period.
	// A longer window would keep more of a child's inner life than the feature
	// needs, and a shorter one would make the weekly trend meaningless.
	MinRetentionDays = 1
	MaxRetentionDays = 90

	// DefaultSummaryDays is the window a guardian sees by default.
	DefaultSummaryDays = 7
	// MinSummaryDays and MaxSummaryDays bound the requested window.
	MinSummaryDays = 1
	MaxSummaryDays = 90

	// ObservationFutureTolerance absorbs clock skew between the device and the
	// platform. A sample stamped further ahead is refused rather than clamped,
	// because a wrong device clock would otherwise silently backdate nothing
	// and forward-date everything.
	ObservationFutureTolerance = 5 * time.Minute
	// ObservationMaxAge is how old a sample may be when it arrives. Beyond it
	// the device is replaying a backlog the retention window already covers.
	ObservationMaxAge = 90 * 24 * time.Hour

	// MinConfidence and MaxConfidence bound the reported confidence interval.
	MinConfidence = 0
	MaxConfidence = 100

	// MinSamplesForTrend is the number of samples each period needs before a
	// trend is reported. Below it the comparison is noise: two samples of a
	// tired afternoon would read as a fifteen-minute slump.
	MinSamplesForTrend = 20
)

// IsLabel reports whether value is one of the supported emotions.
func IsLabel(value string) bool {
	switch Label(value) {
	case LabelCalm, LabelHappy, LabelCurious, LabelTired,
		LabelFrustrated, LabelSad, LabelUpset:
		return true
	default:
		return false
	}
}

// ConfidenceValid reports whether a confidence is inside 0..100. An
// out-of-range value is rejected rather than clamped: a client that reports 150
// is broken, and quietly accepting it would store a value the database forbids.
func ConfidenceValid(confidence int) bool {
	return confidence >= MinConfidence && confidence <= MaxConfidence
}

// RetentionValid reports whether a retention period is inside 1..90 days.
func RetentionValid(days int) bool {
	return days >= MinRetentionDays && days <= MaxRetentionDays
}

// NormaliseRetention turns a requested retention into a usable value.
//
// Zero means "the guardian did not choose", so it becomes the default; any
// other out-of-range value is an error the caller must report rather than
// silently replace, because the guardian asked for something specific.
func NormaliseRetention(days int) (int, error) {
	if days == 0 {
		return DefaultRetentionDays, nil
	}
	if !RetentionValid(days) {
		return 0, ErrInvalidRetention
	}
	return days, nil
}

// Settings is one family's mood sensing configuration.
//
// A family without a stored row is DISABLED. That is expressed by Enabled
// being false on a zero Settings value, so callers never have to special-case a
// missing row into an "on" default.
type Settings struct {
	FamilyID string
	Enabled  bool
	// RetentionDays is how long samples are kept before the retention worker
	// removes them.
	RetentionDays int
	UpdatedAt     time.Time
}

// DefaultSettings returns the configuration of a family that never changed it.
func DefaultSettings(familyID string, now time.Time) Settings {
	return Settings{
		FamilyID:      familyID,
		Enabled:       false,
		RetentionDays: DefaultRetentionDays,
		UpdatedAt:     now.UTC(),
	}
}

// Apply validates and stores a requested configuration change.
func (settings *Settings) Apply(enabled bool, retentionDays int, now time.Time) error {
	normalised, err := NormaliseRetention(retentionDays)
	if err != nil {
		return err
	}
	settings.Enabled = enabled
	settings.RetentionDays = normalised
	settings.UpdatedAt = now.UTC()
	return nil
}

// Cutoff returns the oldest instant the retention period keeps. A sample
// observed before it is due for removal.
func (settings Settings) Cutoff(now time.Time) time.Time {
	days := settings.RetentionDays
	if days <= 0 {
		days = DefaultRetentionDays
	}
	return now.UTC().AddDate(0, 0, -days)
}

// Observation is one accepted sample.
//
// It holds a label, a confidence, and an instant. There is deliberately no
// field for media, text, or a reason: anything that could reconstruct what the
// child was doing at that moment belongs outside this table.
type Observation struct {
	ID       string
	FamilyID string
	// ChildID may be empty when the device is not bound to a child, in which
	// case the sample still counts toward the family distribution.
	ChildID    string
	DeviceID   string
	Label      Label
	Confidence int
	ObservedAt time.Time
	CreatedAt  time.Time
}

// RecordInput is a device's validated report of one sample.
type RecordInput struct {
	DeviceID   string
	ChildID    string
	Label      string
	Confidence int
	ObservedAt time.Time
}

// Validate checks the report's own fields against the vocabulary and clock.
//
// The reported instant is not clamped: a device whose clock is five minutes
// ahead is tolerated, while one that is an hour ahead or replaying last
// quarter's backlog is refused with a stable error.
func (input RecordInput) Validate(now time.Time) (RecordInput, error) {
	normalised := RecordInput{
		DeviceID:   strings.TrimSpace(input.DeviceID),
		ChildID:    strings.TrimSpace(input.ChildID),
		Label:      strings.TrimSpace(input.Label),
		Confidence: input.Confidence,
		ObservedAt: input.ObservedAt.UTC(),
	}
	if normalised.DeviceID == "" {
		return RecordInput{}, ErrInvalidRequest
	}
	if normalised.ObservedAt.IsZero() {
		return RecordInput{}, ErrInvalidRequest
	}
	if !IsLabel(normalised.Label) {
		return RecordInput{}, ErrInvalidLabel
	}
	if !ConfidenceValid(normalised.Confidence) {
		return RecordInput{}, ErrInvalidConfidence
	}
	now = now.UTC()
	if normalised.ObservedAt.After(now.Add(ObservationFutureTolerance)) {
		return RecordInput{}, ErrObservationFuture
	}
	if normalised.ObservedAt.Before(now.Add(-ObservationMaxAge)) {
		return RecordInput{}, ErrObservationTooOld
	}
	return normalised, nil
}

// LabelCount is how often one label was observed in a window.
type LabelCount struct {
	Label Label
	Count int
	// ConfidenceSum is the sum of the per-sample confidences behind Count.
	// Only the mean is ever reported to a guardian; the sum is kept so the
	// aggregate can weight each label by how sure the device was.
	ConfidenceSum int
}

// Trend values are relative movements between the current and previous period.
type Trend string

const (
	// TrendUp means the label is clearly more frequent than before.
	TrendUp Trend = "up"
	// TrendDown means it is clearly less frequent than before.
	TrendDown Trend = "down"
	// TrendFlat means it moved less than the reporting margin.
	TrendFlat Trend = "flat"
	// TrendUnknown means there is not enough data on one side to compare.
	TrendUnknown Trend = "unknown"
)

// trendMargin is the relative change below which a movement is reported as
// flat. One in ten keeps a small daily wobble from being presented as a change.
const trendMargin = 0.1

// CompareLabels returns the trend for a label between two periods.
//
// It refuses to answer on thin data: a period with fewer than
// MinSamplesForTrend samples cannot support a claim about a child's mood, and
// reporting "rising frustration" from three samples would invite a guardian to
// respond to noise.
func CompareLabels(current, previous LabelCount, currentTotal, previousTotal int) Trend {
	if currentTotal < MinSamplesForTrend || previousTotal < MinSamplesForTrend {
		return TrendUnknown
	}
	currentShare := shareOf(current.Count, currentTotal)
	previousShare := shareOf(previous.Count, previousTotal)
	delta := currentShare - previousShare
	switch {
	case delta > trendMargin:
		return TrendUp
	case delta < -trendMargin:
		return TrendDown
	default:
		return TrendFlat
	}
}

// shareOf returns count/total as a fraction, or zero when there is no data.
func shareOf(count, total int) float64 {
	if total <= 0 {
		return 0
	}
	return float64(count) / float64(total)
}

// Percent returns the rounded percentage count occupies of total.
//
// It is the only number a guardian sees as a share, so it lives here rather
// than in a transport layer, and it is rounded once: two clients computing
// percentages from raw counts could disagree by a point.
func Percent(count, total int) int {
	if total <= 0 || count <= 0 {
		return 0
	}
	return int((int64(count)*100 + int64(total)/2) / int64(total))
}

// Summary is the aggregate a guardian is allowed to see.
//
// There is no field for an individual observation: no instant, no device, no
// per-sample confidence, and no identifier. The types in this struct are the
// enforcement, not a handler's discipline, so a future handler cannot leak a
// moment by accident.
type Summary struct {
	FamilyID string
	// ChildID is empty when the summary covers the whole family.
	ChildID string
	Days    int
	// PeriodStart and PeriodEnd bound the window. They are the only instants in
	// the payload, and they describe the window, not any sample in it.
	PeriodStart time.Time
	PeriodEnd   time.Time
	// TotalSamples is how many samples the window contains.
	TotalSamples int
	// Labels is ordered by AllLabels(), always including every label with a zero
	// count, so a distribution renders the same shape even on a quiet week.
	Labels []LabelCount
	// AverageConfidence is the mean confidence of the window, or 0 when empty.
	AverageConfidence int
	// Trends maps each label to its movement versus the previous window of the
	// same length.
	Trends map[Label]Trend
	// PreviousTotalSamples is how many samples the comparison window holds. It
	// is reported so a client can explain an "unknown" trend instead of
	// inventing one.
	PreviousTotalSamples int
	// Enabled reports whether sensing is currently on, so the guardian knows
	// that an empty window may simply mean it is off.
	Enabled bool
	// RetentionDays is the window in effect, so the guardian can see that data
	// older than it is being removed.
	RetentionDays int
}

// HasData reports whether the window holds at least one sample.
func (summary Summary) HasData() bool {
	return summary.TotalSamples > 0
}

// BuildSummary assembles a summary from aggregated counts.
//
// current and previous are per-label totals for the two windows. The result
// always carries every label, because a missing label in a chart is
// indistinguishable from a label with no samples.
func BuildSummary(
	familyID, childID string,
	days int,
	periodStart, periodEnd time.Time,
	current map[Label]LabelCount,
	previous map[Label]LabelCount,
	settings Settings,
) Summary {
	summary := Summary{
		FamilyID:      familyID,
		ChildID:       childID,
		Days:          days,
		PeriodStart:   periodStart.UTC(),
		PeriodEnd:     periodEnd.UTC(),
		Labels:        make([]LabelCount, 0, len(AllLabels())),
		Trends:        make(map[Label]Trend, len(AllLabels())),
		Enabled:       settings.Enabled,
		RetentionDays: settings.RetentionDays,
	}
	confidenceSum := int64(0)
	for _, label := range AllLabels() {
		count := current[label]
		count.Label = label
		summary.Labels = append(summary.Labels, count)
		summary.TotalSamples += count.Count
		confidenceSum += int64(count.ConfidenceSum)
		previousCount := previous[label]
		previousCount.Label = label
		summary.Trends[label] = TrendUnknown
		summary.PreviousTotalSamples += previousCount.Count
	}
	if summary.TotalSamples > 0 {
		summary.AverageConfidence = int(
			(confidenceSum + int64(summary.TotalSamples)/2) / int64(summary.TotalSamples),
		)
		for _, label := range AllLabels() {
			currentCount := current[label]
			currentCount.Label = label
			previousCount := previous[label]
			previousCount.Label = label
			summary.Trends[label] = CompareLabels(
				currentCount,
				previousCount,
				summary.TotalSamples,
				summary.PreviousTotalSamples,
			)
		}
	}
	return summary
}

// NormaliseDays turns a requested window into a usable one.
//
// Zero means "the caller did not ask", which becomes the default. A negative or
// oversized window is an error: silently clamping 400 days to 90 would answer a
// question the guardian did not ask.
func NormaliseDays(days int) (int, error) {
	if days == 0 {
		return DefaultSummaryDays, nil
	}
	if days < MinSummaryDays || days > MaxSummaryDays {
		return 0, ErrInvalidDays
	}
	return days, nil
}
