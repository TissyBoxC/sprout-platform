package service

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/service_version/domain"
	"github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/service_version/repository"
)

type fakeReleaseSource struct {
	releases       []domain.Release
	latest         *domain.Release
	hasMore        bool
	latestErr      error
	findErr        error
	listErr        error
	requests       []string
	latestRequests int
}

func newTestReleaseSource(
	t *testing.T,
	snapshotJSON string,
	releases map[string][]domain.Release,
) (*Service, string) {
	t.Helper()
	stateDir := t.TempDir()
	if snapshotJSON != "" {
		if err := os.WriteFile(
			filepath.Join(stateDir, "status.json"),
			[]byte(snapshotJSON),
			0o600,
		); err != nil {
			t.Fatalf("write status snapshot: %v", err)
		}
	}
	if releases != nil {
		payload, err := json.Marshal(domain.ReleaseCatalog{
			GeneratedAt: time.Unix(0, 0).UTC(),
			Services:    releases,
		})
		if err != nil {
			t.Fatalf("encode release catalog: %v", err)
		}
		if err := os.WriteFile(
			filepath.Join(stateDir, "releases.json"),
			payload,
			0o600,
		); err != nil {
			t.Fatalf("write release catalog: %v", err)
		}
	}
	service, err := New(Options{
		Repository: repository.NewFileRepository(stateDir),
		Clock:      func() time.Time { return time.Unix(0, 0).UTC() },
	})
	if err != nil {
		t.Fatalf("create service: %v", err)
	}
	return service, stateDir
}

func (source *fakeReleaseSource) ListReleases(
	_ context.Context,
	repository string,
	_ int,
	_ int,
) ([]domain.Release, bool, error) {
	source.requests = append(source.requests, "list:"+repository)
	if source.listErr != nil {
		return nil, false, source.listErr
	}
	return append([]domain.Release(nil), source.releases...), source.hasMore, nil
}

func (source *fakeReleaseSource) LatestRelease(
	_ context.Context,
	repository string,
) (domain.Release, error) {
	source.requests = append(source.requests, "latest:"+repository)
	source.latestRequests++
	if source.latestErr != nil {
		return domain.Release{}, source.latestErr
	}
	if source.latest != nil {
		return *source.latest, nil
	}
	if len(source.releases) == 0 {
		return domain.Release{}, domain.ErrReleaseNotFound
	}
	return source.releases[0], nil
}

func (source *fakeReleaseSource) FindRelease(
	_ context.Context,
	repository string,
	version string,
) (domain.Release, error) {
	source.requests = append(source.requests, "find:"+repository)
	if source.findErr != nil {
		return domain.Release{}, source.findErr
	}
	for _, release := range source.releases {
		if release.Version == version {
			return release, nil
		}
	}
	return domain.Release{}, domain.ErrReleaseNotFound
}

func newTestService(t *testing.T, snapshotJSON string) (*Service, string) {
	return newTestServiceWithReleases(t, snapshotJSON, &fakeReleaseSource{})
}

func newTestServiceWithReleases(
	t *testing.T,
	snapshotJSON string,
	releases ReleaseSource,
) (*Service, string) {
	t.Helper()
	stateDir := t.TempDir()
	if snapshotJSON != "" {
		if err := os.WriteFile(
			filepath.Join(stateDir, "status.json"),
			[]byte(snapshotJSON),
			0o600,
		); err != nil {
			t.Fatalf("write snapshot: %v", err)
		}
	}
	service, err := New(Options{
		Repository: repository.NewFileRepository(stateDir),
		Releases:   releases,
		Clock:      func() time.Time { return time.Unix(0, 0).UTC() },
	})
	if err != nil {
		t.Fatalf("New() returned unexpected error: %v", err)
	}
	return service, stateDir
}

func TestSnapshotMergesCatalogAndMarksUpgradable(t *testing.T) {
	service, _ := newTestService(t, `{
		"generated_at": "2026-10-03T00:00:00Z",
		"services": [
			{"id":"device_platform","current_version":"0.9.0","latest_version":"0.10.0","status":"outdated","release_url":"https://example.test/v0.10.0"}
		]
	}`)

	services, checkedAt, err := service.Snapshot(context.Background())
	if err != nil {
		t.Fatalf("Snapshot() returned unexpected error: %v", err)
	}
	if checkedAt.IsZero() {
		t.Fatal("expected a non-zero checked timestamp")
	}
	var platform *domain.Service
	for index := range services {
		if services[index].ID == "device_platform" {
			platform = &services[index]
		}
	}
	if platform == nil {
		t.Fatal("expected device_platform in the snapshot")
	}
	if !platform.CanUpgrade {
		t.Fatal("expected an outdated platform to be upgradable")
	}
	if !platform.UpgradeSupported {
		t.Fatal("expected the platform to support version selection")
	}
	if platform.DisplayName == "" {
		t.Fatal("expected the catalogue to supply a display name")
	}
}

func TestInfrastructureNeverUpgradable(t *testing.T) {
	service, _ := newTestService(t, `{
		"generated_at": "2026-10-03T00:00:00Z",
		"services": [
			{"id":"postgres","current_version":"15","latest_version":"16","status":"outdated"},
			{"id":"download_http","current_version":"1.27","latest_version":"1.28","status":"outdated"}
		]
	}`)

	services, _, err := service.Snapshot(context.Background())
	if err != nil {
		t.Fatalf("Snapshot() returned unexpected error: %v", err)
	}
	for _, item := range services {
		if item.ID == "postgres" || item.ID == "download_http" {
			if item.UpgradeSupported {
				t.Fatalf("infrastructure image %s must not support upgrades", item.ID)
			}
			if item.CanUpgrade {
				t.Fatalf("infrastructure image %s must not be auto-upgraded", item.ID)
			}
		}
	}
}

func TestSnapshotWithoutWorkerStateIsUnknown(t *testing.T) {
	service, _ := newTestService(t, "")

	result, err := service.SnapshotResult(context.Background())
	if err != nil {
		t.Fatalf("SnapshotResult() returned unexpected error: %v", err)
	}
	if len(result.Services) == 0 {
		t.Fatal("expected the catalogue to be returned even without a snapshot")
	}
	for _, item := range result.Services {
		if item.Status != domain.ServiceStatusUnknown {
			t.Fatalf("expected unknown status, got %q for %s", item.Status, item.ID)
		}
		if item.CanUpgrade {
			t.Fatalf("an unknown service must not be upgradable: %s", item.ID)
		}
	}
	if result.CheckedAt.IsZero() {
		t.Fatal("expected the platform read time when the worker snapshot is absent")
	}
	if result.StateSource != "platform-read" {
		t.Fatalf("expected platform-read state source, got %q", result.StateSource)
	}
}

func TestSnapshotResultUsesWorkerTimestampWhenPresent(t *testing.T) {
	service, _ := newTestService(t, `{
		"generated_at":"2026-10-03T00:00:00Z",
		"services":[]
	}`)

	result, err := service.SnapshotResult(context.Background())
	if err != nil {
		t.Fatalf("SnapshotResult() returned unexpected error: %v", err)
	}
	want := time.Date(2026, time.October, 3, 0, 0, 0, 0, time.UTC)
	if !result.CheckedAt.Equal(want) {
		t.Fatalf("expected worker timestamp %s, got %s", want, result.CheckedAt)
	}
	if result.StateSource != "worker" {
		t.Fatalf("expected worker state source, got %q", result.StateSource)
	}
}

func TestPlanUpgradeRejectsUnknownService(t *testing.T) {
	service, _ := newTestService(t, `{"generated_at":"2026-10-03T00:00:00Z","services":[]}`)

	_, err := service.PlanUpgrade(context.Background(), "does_not_exist")
	if !errors.Is(err, domain.ErrServiceNotFound) {
		t.Fatalf("expected ErrServiceNotFound, got %v", err)
	}
}

func TestPlanUpgradeRejectsCurrentService(t *testing.T) {
	service, _ := newTestService(t, `{
		"generated_at":"2026-10-03T00:00:00Z",
		"services":[{"id":"device_platform","current_version":"0.10.0","latest_version":"0.10.0","status":"current"}]
	}`)

	_, err := service.PlanUpgrade(context.Background(), "device_platform")
	if !errors.Is(err, domain.ErrServiceNotUpgradable) {
		t.Fatalf("expected ErrServiceNotUpgradable, got %v", err)
	}
}

func TestEnqueueAndListOperations(t *testing.T) {
	service, stateDir := newTestService(t, `{
		"generated_at":"2026-10-03T00:00:00Z",
		"services":[{"id":"device_platform","current_version":"0.9.0","latest_version":"0.10.0","status":"outdated"}]
	}`)

	plan, err := service.PlanUpgrade(context.Background(), "device_platform")
	if err != nil {
		t.Fatalf("PlanUpgrade() returned unexpected error: %v", err)
	}
	operation, err := service.Enqueue(context.Background(), plan, "admin-1")
	if err != nil {
		t.Fatalf("Enqueue() returned unexpected error: %v", err)
	}
	if operation.Status != domain.OperationStatusQueued {
		t.Fatalf("expected queued operation, got %q", operation.Status)
	}
	if _, err := os.Stat(
		filepath.Join(stateDir, "queue", operation.ID+".json"),
	); err != nil {
		t.Fatalf("expected queue file: %v", err)
	}

	// A second enqueue while the worker result is absent still counts the
	// first as inactive, so a fresh upgrade is allowed. The lock is enforced
	// again once the worker writes an active result file.
	if _, err := service.Enqueue(context.Background(), plan, "admin-1"); err != nil {
		t.Fatalf("Enqueue() returned unexpected error: %v", err)
	}
}

func TestEnqueueBlockedByActiveOperation(t *testing.T) {
	service, stateDir := newTestService(t, `{
		"generated_at":"2026-10-03T00:00:00Z",
		"services":[{"id":"device_platform","current_version":"0.9.0","latest_version":"0.10.0","status":"outdated"}]
	}`)
	resultsDir := filepath.Join(stateDir, "results")
	if err := os.MkdirAll(resultsDir, 0o750); err != nil {
		t.Fatalf("create results dir: %v", err)
	}
	if err := os.WriteFile(
		filepath.Join(resultsDir, "active.json"),
		[]byte(`{"id":"active","target_service":"voice_gateway","status":"running"}`),
		0o600,
	); err != nil {
		t.Fatalf("write active result: %v", err)
	}

	plan, err := service.PlanUpgrade(context.Background(), "device_platform")
	if err != nil {
		t.Fatalf("PlanUpgrade() returned unexpected error: %v", err)
	}
	if _, err := service.Enqueue(context.Background(), plan, "admin-1"); !errors.Is(
		err,
		domain.ErrUpgradeInProgress,
	) {
		t.Fatalf("expected ErrUpgradeInProgress, got %v", err)
	}
}

func TestPlanUpgradeAllOrdersPlatformBeforeSelf(t *testing.T) {
	service, _ := newTestService(t, `{
		"generated_at":"2026-10-03T00:00:00Z",
		"services":[
			{"id":"admin_web","current_version":"0.9.0","latest_version":"0.10.0","status":"outdated"},
			{"id":"device_platform","current_version":"0.9.0","latest_version":"0.10.0","status":"outdated"},
			{"id":"sub2api","current_version":"0.2.15","latest_version":"0.2.16","status":"outdated"}
		]
	}`)

	plans, err := service.PlanUpgradeAll(context.Background())
	if err != nil {
		t.Fatalf("PlanUpgradeAll() returned unexpected error: %v", err)
	}
	if len(plans) != 3 {
		t.Fatalf("expected 3 plans, got %d", len(plans))
	}
	order := []string{plans[0].TargetService, plans[1].TargetService, plans[2].TargetService}
	want := []string{"sub2api", "device_platform", "admin_web"}
	for index := range want {
		if order[index] != want[index] {
			t.Fatalf("unexpected upgrade order: %v", order)
		}
	}
}

func TestPlanUpgradeToUsesExplicitPublishedVersion(t *testing.T) {
	releases := &fakeReleaseSource{
		releases: []domain.Release{
			{Version: "0.10.0", ReleaseURL: "https://example.test/v0.10.0"},
			{Version: "0.9.0", ReleaseURL: "https://example.test/v0.9.0"},
		},
	}
	service, _ := newTestServiceWithReleases(t, `{
		"generated_at":"2026-10-03T00:00:00Z",
		"services":[{"id":"device_platform","current_version":"0.10.0","latest_version":"0.10.0","status":"current"}]
	}`, releases)

	plan, err := service.PlanUpgradeTo(
		context.Background(),
		"device_platform",
		"v0.9.0",
	)
	if err != nil {
		t.Fatalf("PlanUpgradeTo() returned unexpected error: %v", err)
	}
	if plan.TargetVersion != "0.9.0" {
		t.Fatalf("expected normalized target 0.9.0, got %q", plan.TargetVersion)
	}
	if len(releases.requests) != 1 || releases.requests[0] != "find:device_platform" {
		t.Fatalf("unexpected release source calls: %v", releases.requests)
	}
}

func TestPlanUpgradeToRejectsUnpublishedVersion(t *testing.T) {
	releases := &fakeReleaseSource{
		releases: []domain.Release{{Version: "0.10.0"}},
	}
	service, _ := newTestServiceWithReleases(t, `{
		"generated_at":"2026-10-03T00:00:00Z",
		"services":[{"id":"device_platform","current_version":"0.9.0","latest_version":"0.10.0","status":"outdated"}]
	}`, releases)

	_, err := service.PlanUpgradeTo(
		context.Background(),
		"device_platform",
		"9.9.9",
	)
	if !errors.Is(err, domain.ErrReleaseNotFound) {
		t.Fatalf("expected ErrReleaseNotFound, got %v", err)
	}
}

func TestPlanUpgradeToRejectsCurrentVersion(t *testing.T) {
	releases := &fakeReleaseSource{
		releases: []domain.Release{{Version: "0.10.0"}},
	}
	service, _ := newTestServiceWithReleases(t, `{
		"generated_at":"2026-10-03T00:00:00Z",
		"services":[{"id":"device_platform","current_version":"0.10.0","latest_version":"0.10.0","status":"current"}]
	}`, releases)

	_, err := service.PlanUpgradeTo(
		context.Background(),
		"device_platform",
		"0.10.0",
	)
	if !errors.Is(err, domain.ErrServiceNotUpgradable) {
		t.Fatalf("expected ErrServiceNotUpgradable, got %v", err)
	}
}

func TestPlanUpgradeToMapsReleaseSourceFailure(t *testing.T) {
	releases := &fakeReleaseSource{findErr: domain.ErrReleaseSourceFailed}
	service, _ := newTestServiceWithReleases(t, `{
		"generated_at":"2026-10-03T00:00:00Z",
		"services":[{"id":"device_platform","current_version":"0.9.0","latest_version":"0.10.0","status":"outdated"}]
	}`, releases)

	_, err := service.PlanUpgradeTo(
		context.Background(),
		"device_platform",
		"0.10.0",
	)
	if !errors.Is(err, domain.ErrReleaseSourceFailed) {
		t.Fatalf("expected ErrReleaseSourceFailed, got %v", err)
	}
}

func TestListReleasesMarksCurrentAndLatestAcrossPages(t *testing.T) {
	releases := &fakeReleaseSource{
		releases: []domain.Release{
			{Version: "0.9.0", ReleaseURL: "https://example.test/v0.9.0"},
		},
		latest:  &domain.Release{Version: "0.10.0"},
		hasMore: true,
	}
	service, _ := newTestServiceWithReleases(t, `{
		"generated_at":"2026-10-03T00:00:00Z",
		"services":[{"id":"device_platform","current_version":"0.9.0","latest_version":"0.10.0","status":"outdated"}]
	}`, releases)

	page, err := service.ListReleases(context.Background(), "device_platform", 2, 20)
	if err != nil {
		t.Fatalf("ListReleases() returned unexpected error: %v", err)
	}
	if page.Service != "device_platform" || page.Page != 2 || len(page.Releases) != 1 {
		t.Fatalf("unexpected release page: %+v", page)
	}
	if !page.Releases[0].IsCurrent {
		t.Fatal("expected the running version to be marked current")
	}
	if page.Releases[0].IsLatest {
		t.Fatal("an older release must not be marked latest")
	}
	if !page.HasMore {
		t.Fatal("expected has_more to be preserved")
	}
}

func TestListReleasesRejectsInfrastructureService(t *testing.T) {
	service, _ := newTestService(t, `{"generated_at":"2026-10-03T00:00:00Z","services":[]}`)

	_, err := service.ListReleases(context.Background(), "postgres", 1, 20)
	if !errors.Is(err, domain.ErrServiceNotFound) {
		t.Fatalf("expected ErrServiceNotFound, got %v", err)
	}

	_, err = service.ListReleases(context.Background(), "download_http", 1, 20)
	if !errors.Is(err, domain.ErrServiceNotFound) {
		t.Fatalf("expected download_http to be rejected, got %v", err)
	}
}

func TestPlanUpgradeRejectsInfrastructureService(t *testing.T) {
	service, _ := newTestService(t, `{
		"generated_at":"2026-10-03T00:00:00Z",
		"services":[{"id":"download_http","current_version":"1.27","latest_version":"1.28","status":"outdated"}]
	}`)

	_, err := service.PlanUpgradeTo(
		context.Background(),
		"download_http",
		"1.28",
	)
	if !errors.Is(err, domain.ErrServiceNotUpgradable) {
		t.Fatalf("expected ErrServiceNotUpgradable, got %v", err)
	}
}

func TestConfirmationUsesRunningAndPlannedVersions(t *testing.T) {
	service, _ := newTestService(t, `{
		"generated_at":"2026-10-03T00:00:00Z",
		"services":[{"id":"device_platform","current_version":"0.9.0","latest_version":"0.10.0","status":"outdated","display_name":"设备平台"}]
	}`)

	confirmation, err := service.Confirmation(
		context.Background(),
		UpgradePlan{
			TargetService: "device_platform",
			TargetVersion: "0.8.0",
			UpgradesSelf:  false,
		},
	)
	if err != nil {
		t.Fatalf("Confirmation() returned unexpected error: %v", err)
	}
	if confirmation.CurrentVersion != "0.9.0" ||
		confirmation.TargetVersion != "0.8.0" {
		t.Fatalf("unexpected confirmation: %+v", confirmation)
	}
}
