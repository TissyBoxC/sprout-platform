package service_test

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/emotion/domain"
	"github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/emotion/service"
)

var referenceNow = time.Date(2026, 10, 22, 9, 0, 0, 0, time.UTC)

// fakeStore keeps settings and samples in memory and honours the same sentinels
// as the repository adapter, so the service is tested against its real contract
// without a database.
type fakeStore struct {
	settings map[string]domain.Settings
	samples  []domain.Observation
	// familyOf maps a device to the owning family, mirroring device_bindings.
	familyOf map[string]string
	// bindings holds "device|child" pairs that are active, mirroring
	// device_child_bindings.
	bindings map[string]bool
	// insertErr forces a write failure for the retention tests.
	insertErr error
	// deleteErr forces a retention failure for the incomplete-sweep test.
	deleteErr error
}

func newFakeStore() *fakeStore {
	return &fakeStore{
		settings: map[string]domain.Settings{},
		familyOf: map[string]string{
			"device-1": "family-1",
			"device-2": "family-2",
		},
		bindings: map[string]bool{
			"device-1|child-1": true,
			"device-2|child-2": true,
		},
	}
}

func (store *fakeStore) GetSettings(_ context.Context, familyID string) (domain.Settings, error) {
	settings, ok := store.settings[familyID]
	if !ok {
		return domain.Settings{}, service.StoreSentinelNotFound
	}
	return settings, nil
}

func (store *fakeStore) SaveSettings(_ context.Context, settings domain.Settings) error {
	store.settings[settings.FamilyID] = settings
	return nil
}

func (store *fakeStore) DeviceFamily(_ context.Context, deviceID string) (string, error) {
	return store.familyOf[deviceID], nil
}

func (store *fakeStore) DeviceChildBinding(
	_ context.Context,
	_, deviceID, childID string,
) (bool, error) {
	return store.bindings[deviceID+"|"+childID], nil
}

func (store *fakeStore) InsertObservation(
	_ context.Context,
	observation domain.Observation,
) error {
	if store.insertErr != nil {
		return store.insertErr
	}
	// The repository re-checks the binding inside the insert, so the fake must
	// too: a test that skipped it would pass while production refused the write.
	if observation.ChildID != "" &&
		!store.bindings[observation.DeviceID+"|"+observation.ChildID] {
		return service.StoreSentinelNotBound
	}
	store.samples = append(store.samples, observation)
	return nil
}

func (store *fakeStore) CountLabels(
	_ context.Context,
	familyID, childID string,
	from, to time.Time,
) (map[domain.Label]domain.LabelCount, error) {
	counts := map[domain.Label]domain.LabelCount{}
	for _, sample := range store.samples {
		if sample.FamilyID != familyID {
			continue
		}
		if childID != "" && sample.ChildID != childID {
			continue
		}
		if sample.ObservedAt.Before(from) || !sample.ObservedAt.Before(to) {
			continue
		}
		current := counts[sample.Label]
		current.Label = sample.Label
		current.Count++
		current.ConfidenceSum += sample.Confidence
		counts[sample.Label] = current
	}
	return counts, nil
}

func (store *fakeStore) DeleteBefore(
	_ context.Context,
	familyID string,
	cutoff time.Time,
) (int, error) {
	if store.deleteErr != nil {
		return 0, store.deleteErr
	}
	kept := make([]domain.Observation, 0, len(store.samples))
	removed := 0
	for _, sample := range store.samples {
		if sample.FamilyID == familyID && sample.ObservedAt.Before(cutoff) {
			removed++
			continue
		}
		kept = append(kept, sample)
	}
	store.samples = kept
	return removed, nil
}

func (store *fakeStore) ListEnabledFamilies(
	_ context.Context,
	limit int,
) ([]domain.Settings, error) {
	enabled := make([]domain.Settings, 0, len(store.settings))
	for _, settings := range store.settings {
		if settings.Enabled && len(enabled) < limit {
			enabled = append(enabled, settings)
		}
	}
	return enabled, nil
}

func (store *fakeStore) CountEnabledFamilies(_ context.Context) (int, error) {
	count := 0
	for _, settings := range store.settings {
		if settings.Enabled {
			count++
		}
	}
	return count, nil
}

func (store *fakeStore) CountObservationsSince(_ context.Context, since time.Time) (int, error) {
	count := 0
	for _, sample := range store.samples {
		if !sample.ObservedAt.Before(since) {
			count++
		}
	}
	return count, nil
}

// fakeSwitch reads the family setting through the same seam the wiring uses.
type fakeSwitch struct{ store *fakeStore }

func (switcher fakeSwitch) SettingsEnabled(ctx context.Context, familyID string) (bool, error) {
	settings, err := switcher.store.GetSettings(ctx, familyID)
	if err != nil {
		if errors.Is(err, service.StoreSentinelNotFound) {
			return false, nil
		}
		return false, err
	}
	return settings.Enabled, nil
}

// fakeDevices resolves the child a device currently serves.
type fakeDevices struct {
	child map[string]string
	// familyOf holds the owning family per device so the service's existence
	// check is exercised rather than bypassed.
	familyOf map[string]string
}

func (devices fakeDevices) DeviceFamily(_ context.Context, deviceID string) (string, error) {
	return devices.familyOf[deviceID], nil
}

func (devices fakeDevices) DeviceChild(_ context.Context, deviceID string) (string, error) {
	return devices.child[deviceID], nil
}

func newService(store *fakeStore) *service.Service {
	return service.NewService(service.Options{
		Store:    store,
		Switches: fakeSwitch{store: store},
		Devices: fakeDevices{
			child: map[string]string{
				"device-1": "child-1",
				"device-2": "child-2",
			},
			familyOf: map[string]string{
				"device-1": "family-1",
				"device-2": "family-2",
			},
		},
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
}

func validInput() domain.RecordInput {
	return domain.RecordInput{
		DeviceID:   "device-1",
		ChildID:    "child-1",
		Label:      "calm",
		Confidence: 70,
		ObservedAt: time.Now().UTC().Add(-time.Minute),
	}
}

// TestSettingsDefaultToDisabled is the first non-negotiable rule: a family that
// never opted in must read as off and must not have stored samples.
func TestSettingsDefaultToDisabled(t *testing.T) {
	store := newFakeStore()
	svc := newService(store)

	settings, err := svc.GetSettings(context.Background(), "family-1")
	if err != nil {
		t.Fatalf("get settings: %v", err)
	}
	if settings.Enabled {
		t.Fatal("a family without a row must be disabled")
	}
	if settings.RetentionDays != domain.DefaultRetentionDays {
		t.Fatalf("retention = %d, want %d",
			settings.RetentionDays, domain.DefaultRetentionDays)
	}
}

func TestEnableThenRecordStoresSample(t *testing.T) {
	store := newFakeStore()
	svc := newService(store)

	settings, err := svc.Enable(context.Background(), "family-1", 14)
	if err != nil {
		t.Fatalf("enable: %v", err)
	}
	if !settings.Enabled || settings.RetentionDays != 14 {
		t.Fatalf("enable result = %+v", settings)
	}
	observation, err := svc.Record(context.Background(), validInput())
	if err != nil {
		t.Fatalf("record: %v", err)
	}
	if observation.FamilyID != "family-1" || observation.ChildID != "child-1" {
		t.Fatalf("observation = %+v", observation)
	}
	if len(store.samples) != 1 {
		t.Fatalf("samples = %d, want 1", len(store.samples))
	}
}

// TestRecordRefusesDisabledFamily proves a report is refused with a stable error
// rather than silently dropped, so the device can tell the difference.
func TestRecordRefusesDisabledFamily(t *testing.T) {
	store := newFakeStore()
	svc := newService(store)

	_, err := svc.Record(context.Background(), validInput())
	if !errors.Is(err, service.ErrDisabled) {
		t.Fatalf("record while disabled err = %v, want ErrDisabled", err)
	}
	if len(store.samples) != 0 {
		t.Fatalf("a disabled family must store nothing, got %d samples", len(store.samples))
	}
}

// TestDisableStopsNewSamples proves an off switch takes effect on the next report
// without deleting what is already stored.
func TestDisableStopsNewSamples(t *testing.T) {
	store := newFakeStore()
	svc := newService(store)
	if _, err := svc.Enable(context.Background(), "family-1", 30); err != nil {
		t.Fatalf("enable: %v", err)
	}
	if _, err := svc.Record(context.Background(), validInput()); err != nil {
		t.Fatalf("record: %v", err)
	}
	settings, err := svc.Disable(context.Background(), "family-1", 0)
	if err != nil {
		t.Fatalf("disable: %v", err)
	}
	if settings.Enabled {
		t.Fatal("disable must turn sensing off")
	}
	if settings.RetentionDays != 30 {
		// A bare off switch keeps the guardian's retention choice.
		t.Fatalf("retention = %d, want the stored 30", settings.RetentionDays)
	}
	if _, err := svc.Record(context.Background(), validInput()); !errors.Is(err, service.ErrDisabled) {
		t.Fatalf("record after disable err = %v, want ErrDisabled", err)
	}
	if len(store.samples) != 1 {
		t.Fatalf("samples = %d, want the first sample kept", len(store.samples))
	}
}

func TestRecordRefusesUnknownDevice(t *testing.T) {
	store := newFakeStore()
	svc := newService(store)
	if _, err := svc.Enable(context.Background(), "family-1", 30); err != nil {
		t.Fatalf("enable: %v", err)
	}
	input := validInput()
	input.DeviceID = "device-unknown"
	if _, err := svc.Record(context.Background(), input); !errors.Is(err, service.ErrDeviceNotFound) {
		t.Fatalf("unknown device err = %v, want ErrDeviceNotFound", err)
	}
}

// TestRecordRefusesChildFromAnotherFamily proves a device cannot report about a
// child it does not serve, which also covers a cross-family child.
func TestRecordRefusesChildFromAnotherFamily(t *testing.T) {
	store := newFakeStore()
	svc := newService(store)
	if _, err := svc.Enable(context.Background(), "family-1", 30); err != nil {
		t.Fatalf("enable: %v", err)
	}
	input := validInput()
	input.ChildID = "child-2"
	if _, err := svc.Record(context.Background(), input); !errors.Is(err, service.ErrChildNotBound) {
		t.Fatalf("unbound child err = %v, want ErrChildNotBound", err)
	}
	if len(store.samples) != 0 {
		t.Fatal("an unbound child report must not be stored")
	}
}

func TestRecordUsesBoundChildWhenNoneIsNamed(t *testing.T) {
	store := newFakeStore()
	svc := newService(store)
	if _, err := svc.Enable(context.Background(), "family-1", 30); err != nil {
		t.Fatalf("enable: %v", err)
	}
	input := validInput()
	input.ChildID = ""
	observation, err := svc.Record(context.Background(), input)
	if err != nil {
		t.Fatalf("record: %v", err)
	}
	if observation.ChildID != "child-1" {
		t.Fatalf("child = %q, want the bound child", observation.ChildID)
	}
}

func TestRecordRefusesWhenDeviceServesNoChild(t *testing.T) {
	store := newFakeStore()
	svc := service.NewService(service.Options{
		Store:    store,
		Switches: fakeSwitch{store: store},
		Devices: fakeDevices{
			child:    map[string]string{},
			familyOf: map[string]string{"device-1": "family-1"},
		},
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	if _, err := svc.Enable(context.Background(), "family-1", 30); err != nil {
		t.Fatalf("enable: %v", err)
	}
	input := validInput()
	input.ChildID = ""
	if _, err := svc.Record(context.Background(), input); !errors.Is(err, service.ErrChildNotBound) {
		t.Fatalf("no binding err = %v, want ErrChildNotBound", err)
	}
}

// TestRecordRejectsInvalidInput proves the domain rules hold through the service,
// not only in the domain package.
func TestRecordRejectsInvalidInput(t *testing.T) {
	store := newFakeStore()
	svc := newService(store)
	if _, err := svc.Enable(context.Background(), "family-1", 30); err != nil {
		t.Fatalf("enable: %v", err)
	}

	cases := []struct {
		name   string
		mutate func(*domain.RecordInput)
		want   error
	}{
		// "angry" is not used here because it is a translated device bucket;
		// this case needs a name the platform has never heard of.
		{"unknown label", func(input *domain.RecordInput) { input.Label = "elated" },
			domain.ErrInvalidLabel},
		{"confidence high", func(input *domain.RecordInput) { input.Confidence = 101 },
			domain.ErrInvalidConfidence},
		{"confidence low", func(input *domain.RecordInput) { input.Confidence = -1 },
			domain.ErrInvalidConfidence},
		{"future", func(input *domain.RecordInput) {
			input.ObservedAt = time.Now().UTC().Add(time.Hour)
		}, domain.ErrObservationFuture},
		{"too old", func(input *domain.RecordInput) {
			input.ObservedAt = time.Now().UTC().Add(-domain.ObservationMaxAge - time.Hour)
		}, domain.ErrObservationTooOld},
	}
	for _, testCase := range cases {
		input := validInput()
		testCase.mutate(&input)
		if _, err := svc.Record(context.Background(), input); !errors.Is(err, testCase.want) {
			t.Errorf("%s err = %v, want %v", testCase.name, err, testCase.want)
		}
	}
	if len(store.samples) != 0 {
		t.Fatalf("rejected reports stored %d samples", len(store.samples))
	}
}

// TestSummaryReturnsAggregatesOnly proves the guardian payload carries counts and
// shares, and no instant that could locate a sample in time.
func TestSummaryReturnsAggregatesOnly(t *testing.T) {
	store := newFakeStore()
	svc := newService(store)
	if _, err := svc.Enable(context.Background(), "family-1", 30); err != nil {
		t.Fatalf("enable: %v", err)
	}
	now := time.Now().UTC()
	for index := 0; index < 30; index++ {
		label := domain.LabelCalm
		if index%3 == 0 {
			label = domain.LabelHappy
		}
		store.samples = append(store.samples, domain.Observation{
			FamilyID:   "family-1",
			ChildID:    "child-1",
			DeviceID:   "device-1",
			Label:      label,
			Confidence: 80,
			ObservedAt: now.Add(-time.Duration(index) * time.Hour),
		})
	}

	summary, err := svc.Summary(context.Background(), "family-1", "child-1", 7)
	if err != nil {
		t.Fatalf("summary: %v", err)
	}
	if summary.TotalSamples != 30 {
		t.Fatalf("total = %d, want 30", summary.TotalSamples)
	}
	if !summary.Enabled || summary.Days != 7 {
		t.Fatalf("summary metadata = %+v", summary)
	}
	if len(summary.Labels) != len(domain.AllLabels()) {
		t.Fatalf("labels = %d, want every label", len(summary.Labels))
	}
	calm := labelCount(summary, domain.LabelCalm)
	if calm != 20 {
		t.Fatalf("calm = %d, want 20", calm)
	}
	if got := domain.Percent(calm, summary.TotalSamples); got != 67 {
		t.Fatalf("calm share = %d, want 67", got)
	}
	// A first week has no previous window, so no trend may be claimed.
	for label, trend := range summary.Trends {
		if trend != domain.TrendUnknown {
			t.Fatalf("trend for %s = %s, want unknown", label, trend)
		}
	}
}

func TestSummaryComparesWithPreviousPeriod(t *testing.T) {
	store := newFakeStore()
	svc := newService(store)
	if _, err := svc.Enable(context.Background(), "family-1", 30); err != nil {
		t.Fatalf("enable: %v", err)
	}
	now := time.Now().UTC()
	// 40 samples in the current week, half of them frustrated, and 40 in the
	// previous week with only five frustrated: frustration is clearly rising.
	for index := 0; index < 40; index++ {
		label := domain.LabelCalm
		if index < 20 {
			label = domain.LabelFrustrated
		}
		store.samples = append(store.samples, domain.Observation{
			FamilyID: "family-1", ChildID: "child-1", Label: label,
			Confidence: 60, ObservedAt: now.Add(-time.Duration(index) * time.Hour),
		})
		previousLabel := domain.LabelCalm
		if index < 5 {
			previousLabel = domain.LabelFrustrated
		}
		store.samples = append(store.samples, domain.Observation{
			FamilyID: "family-1", ChildID: "child-1", Label: previousLabel,
			Confidence: 60,
			ObservedAt: now.AddDate(0, 0, -7).Add(-time.Duration(index) * time.Hour),
		})
	}

	summary, err := svc.Summary(context.Background(), "family-1", "child-1", 7)
	if err != nil {
		t.Fatalf("summary: %v", err)
	}
	if summary.Trends[domain.LabelFrustrated] != domain.TrendUp {
		t.Fatalf("frustrated trend = %s, want up",
			summary.Trends[domain.LabelFrustrated])
	}
	if summary.Trends[domain.LabelCalm] != domain.TrendDown {
		t.Fatalf("calm trend = %s, want down", summary.Trends[domain.LabelCalm])
	}
}

func TestSummaryExcludesOtherFamiliesAndChildren(t *testing.T) {
	store := newFakeStore()
	svc := newService(store)
	if _, err := svc.Enable(context.Background(), "family-1", 30); err != nil {
		t.Fatalf("enable: %v", err)
	}
	now := time.Now().UTC()
	store.samples = append(store.samples,
		domain.Observation{FamilyID: "family-1", ChildID: "child-1",
			Label: domain.LabelCalm, Confidence: 90, ObservedAt: now.Add(-time.Hour)},
		domain.Observation{FamilyID: "family-2", ChildID: "child-1",
			Label: domain.LabelSad, Confidence: 90, ObservedAt: now.Add(-time.Hour)},
		domain.Observation{FamilyID: "family-1", ChildID: "child-2",
			Label: domain.LabelSad, Confidence: 90, ObservedAt: now.Add(-time.Hour)},
	)

	summary, err := svc.Summary(context.Background(), "family-1", "child-1", 7)
	if err != nil {
		t.Fatalf("summary: %v", err)
	}
	if summary.TotalSamples != 1 {
		t.Fatalf("total = %d, want 1 (other family and sibling excluded)",
			summary.TotalSamples)
	}
	if labelCount(summary, domain.LabelSad) != 0 {
		t.Fatal("a sibling's or another family's sample must not appear")
	}
}

func TestSummaryRejectsWindowOutsideRange(t *testing.T) {
	store := newFakeStore()
	svc := newService(store)
	for _, days := range []int{-1, domain.MaxSummaryDays + 1} {
		if _, err := svc.Summary(context.Background(), "family-1", "", days); !errors.Is(
			err, domain.ErrInvalidDays,
		) {
			t.Errorf("days %d err = %v, want ErrInvalidDays", days, err)
		}
	}
}

// TestPurgeExpiredHonoursEachFamilyWindow proves retention is per family: the
// seven-day family loses its old samples while the thirty-day family keeps them.
func TestPurgeExpiredHonoursEachFamilyWindow(t *testing.T) {
	store := newFakeStore()
	svc := newService(store)
	if _, err := svc.Enable(context.Background(), "family-1", 7); err != nil {
		t.Fatalf("enable family-1: %v", err)
	}
	if _, err := svc.Enable(context.Background(), "family-2", 45); err != nil {
		t.Fatalf("enable family-2: %v", err)
	}
	now := time.Now().UTC()
	store.samples = append(store.samples,
		// ten days old: past family-1's window, inside family-2's.
		domain.Observation{FamilyID: "family-1", ChildID: "child-1",
			Label: domain.LabelCalm, ObservedAt: now.AddDate(0, 0, -10)},
		domain.Observation{FamilyID: "family-2", ChildID: "child-2",
			Label: domain.LabelCalm, ObservedAt: now.AddDate(0, 0, -10)},
		// three days old: inside both.
		domain.Observation{FamilyID: "family-1", ChildID: "child-1",
			Label: domain.LabelHappy, ObservedAt: now.AddDate(0, 0, -3)},
	)

	removed, err := svc.PurgeExpired(context.Background(), now)
	if err != nil {
		t.Fatalf("purge: %v", err)
	}
	if removed != 1 {
		t.Fatalf("removed = %d, want 1", removed)
	}
	if len(store.samples) != 2 {
		t.Fatalf("remaining = %d, want 2", len(store.samples))
	}
	for _, sample := range store.samples {
		if sample.FamilyID == "family-1" && sample.ObservedAt.Before(now.AddDate(0, 0, -7)) {
			t.Fatal("family-1 must not keep samples past its seven-day window")
		}
	}
}

func TestPurgeExpiredSkipsDisabledFamilies(t *testing.T) {
	store := newFakeStore()
	svc := newService(store)
	if _, err := svc.Enable(context.Background(), "family-1", 7); err != nil {
		t.Fatalf("enable: %v", err)
	}
	if _, err := svc.Disable(context.Background(), "family-1", 0); err != nil {
		t.Fatalf("disable: %v", err)
	}
	now := time.Now().UTC()
	store.samples = append(store.samples, domain.Observation{
		FamilyID: "family-1", ChildID: "child-1", Label: domain.LabelCalm,
		ObservedAt: now.AddDate(0, 0, -20),
	})
	removed, err := svc.PurgeExpired(context.Background(), now)
	if err != nil {
		t.Fatalf("purge: %v", err)
	}
	// A disabled family keeps its data: turning the switch off is not a delete,
	// and the guardian asked for deletion through the privacy flow instead.
	if removed != 0 || len(store.samples) != 1 {
		t.Fatalf("removed = %d, remaining = %d, want 0/1", removed, len(store.samples))
	}
}

func TestPurgeExpiredReportsFailures(t *testing.T) {
	store := newFakeStore()
	svc := newService(store)
	if _, err := svc.Enable(context.Background(), "family-1", 7); err != nil {
		t.Fatalf("enable: %v", err)
	}
	store.deleteErr = errors.New("database is down")
	if _, err := svc.PurgeExpired(context.Background(), time.Now().UTC()); !errors.Is(
		err, service.ErrPurgeIncomplete,
	) {
		t.Fatalf("purge err = %v, want ErrPurgeIncomplete", err)
	}
}

func TestMetricsReportCountsOnly(t *testing.T) {
	store := newFakeStore()
	svc := newService(store)
	if _, err := svc.Enable(context.Background(), "family-1", 30); err != nil {
		t.Fatalf("enable: %v", err)
	}
	now := time.Now().UTC()
	store.samples = append(store.samples,
		domain.Observation{FamilyID: "family-1", ChildID: "child-1",
			Label: domain.LabelCalm, ObservedAt: now.Add(-time.Hour)},
		domain.Observation{FamilyID: "family-1", ChildID: "child-1",
			Label: domain.LabelSad, ObservedAt: now.AddDate(0, 0, -3)},
	)
	metrics, err := svc.Metrics(context.Background())
	if err != nil {
		t.Fatalf("metrics: %v", err)
	}
	if metrics.EnabledFamilies != 1 {
		t.Fatalf("enabled families = %d, want 1", metrics.EnabledFamilies)
	}
	if metrics.ObservationsLast24 != 1 {
		t.Fatalf("24h observations = %d, want 1", metrics.ObservationsLast24)
	}
}

func TestSettingRetentionOutsideRangeIsRejected(t *testing.T) {
	store := newFakeStore()
	svc := newService(store)
	if _, err := svc.Enable(context.Background(), "family-1", domain.MaxRetentionDays+1); !errors.Is(
		err, domain.ErrInvalidRetention,
	) {
		t.Fatalf("enable err = %v, want ErrInvalidRetention", err)
	}
	if _, ok := store.settings["family-1"]; ok {
		t.Fatal("a refused setting must not be stored")
	}
}

// TestDisableOnFreshFamilyKeepsRetentionInRange proves a bare off switch on a
// family that never enabled the feature still stores a retention inside the
// documented range, rather than a zero that the database CHECK would reject.
func TestDisableOnFreshFamilyKeepsRetentionInRange(t *testing.T) {
	store := newFakeStore()
	svc := newService(store)
	settings, err := svc.Disable(context.Background(), "family-1", 0)
	if err != nil {
		t.Fatalf("disable: %v", err)
	}
	if settings.Enabled {
		t.Fatal("disable must leave sensing off")
	}
	if settings.RetentionDays != domain.DefaultRetentionDays {
		t.Fatalf("retention = %d, want the default %d",
			settings.RetentionDays, domain.DefaultRetentionDays)
	}
}

// TestSummaryFallsBackToStoredRetentionWhenSwitchIsAbsent proves the record path
// still enforces the switch when only the store is wired, so a composition
// mistake cannot turn the feature on by accident.
func TestRecordEnforcesSwitchWithoutSettingsReader(t *testing.T) {
	store := newFakeStore()
	svc := service.NewService(service.Options{
		Store: store,
		Devices: fakeDevices{
			child:    map[string]string{"device-1": "child-1"},
			familyOf: map[string]string{"device-1": "family-1"},
		},
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	if _, err := svc.Record(context.Background(), validInput()); !errors.Is(err, service.ErrDisabled) {
		t.Fatalf("record err = %v, want ErrDisabled", err)
	}
}

// labelCount returns the count for one label in a summary.
func labelCount(summary domain.Summary, label domain.Label) int {
	for _, entry := range summary.Labels {
		if entry.Label == label {
			return entry.Count
		}
	}
	return 0
}
