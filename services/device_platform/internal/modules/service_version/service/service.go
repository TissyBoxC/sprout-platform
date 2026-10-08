// Package service projects the upgrade worker state for the management
// console and validates every operator request before it reaches the worker.
package service

import (
	"context"
	"errors"
	"sort"
	"strings"
	"time"

	operationsdomain "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/operations/domain"
	"github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/service_version/domain"
	"github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/service_version/repository"
	"github.com/google/uuid"
)

// Service owns the read-mostly service inventory and its upgrade commands.
type Service struct {
	repository repository.Repository
	releases   ReleaseSource
	catalog    []domain.CatalogEntry
	clock      func() time.Time
}

// Options contains service version dependencies.
type Options struct {
	Repository repository.Repository
	Releases   ReleaseSource
	Clock      func() time.Time
}

// SnapshotResult is one console-facing view of the worker snapshot.
//
// CheckedAt is always populated for a healthy response. When the worker has
// not written status.json yet, it is the time the platform read the shared
// state directory so the console can distinguish "not reported yet" from a
// permanently missing page value.
type SnapshotResult struct {
	Services    []domain.Service
	CheckedAt   time.Time
	StateSource string
}

// UpgradePlan is a validated batch of service upgrades.
type UpgradePlan struct {
	TargetService string
	TargetVersion string
	UpgradesSelf  bool
}

// UpgradeConfirmation is the operator-facing summary of a queued change.
//
// CurrentVersion is read from the worker snapshot; TargetVersion is the exact
// release the worker will install. Keeping both values on the response lets the
// console render a confirmation without a second round trip.
type UpgradeConfirmation struct {
	ServiceID      string `json:"service_id"`
	DisplayName    string `json:"display_name"`
	CurrentVersion string `json:"current_version"`
	TargetVersion  string `json:"target_version"`
	IsSelf         bool   `json:"is_self"`
}

// New creates the service version service.
func New(options Options) (*Service, error) {
	if options.Repository == nil {
		return nil, errors.New("service version repository is required")
	}
	releaseSource := options.Releases
	if releaseSource == nil {
		releaseSource = NewRepositoryReleaseSource(options.Repository)
	}
	timeSource := options.Clock
	if timeSource == nil {
		timeSource = func() time.Time { return time.Now().UTC() }
	}
	return &Service{
		repository: options.Repository,
		releases:   releaseSource,
		catalog:    defaultCatalog(),
		clock:      timeSource,
	}, nil
}

// Snapshot merges the worker snapshot with the catalogue so every managed
// service is visible even before the worker has reported on it.
func (s *Service) Snapshot(ctx context.Context) ([]domain.Service, time.Time, error) {
	result, err := s.SnapshotResult(ctx)
	if err != nil {
		return nil, time.Time{}, err
	}
	return result.Services, result.CheckedAt, nil
}

// SnapshotResult returns the inventory plus the timestamp the console should
// show as the latest check. A worker-generated timestamp is preferred; the
// read timestamp is the explicit fallback for a missing or zero timestamp.
func (s *Service) SnapshotResult(ctx context.Context) (SnapshotResult, error) {
	snapshot, err := s.repository.Status(ctx)
	if err != nil && !errors.Is(err, domain.ErrStateUnavailable) {
		return SnapshotResult{}, err
	}
	reported := make(map[string]domain.Service, len(snapshotServices(snapshot)))
	for _, service := range snapshotServices(snapshot) {
		reported[service.ID] = service
	}
	services := make([]domain.Service, 0, len(s.catalog))
	for _, entry := range s.catalog {
		service := reported[entry.ID]
		service.ID = entry.ID
		service.DisplayName = entry.DisplayName
		service.Role = entry.Role
		service.Image = entry.Image
		service.IsSelf = entry.IsSelf
		service.UpgradeSupported = entry.AutoUpgrade
		if service.Status == "" {
			service.Status = domain.ServiceStatusUnknown
		}
		// An infrastructure image is detected but never auto-upgraded, so the
		// console must not offer an action the worker would refuse.
		service.CanUpgrade = entry.AutoUpgrade &&
			service.Status == domain.ServiceStatusOutdated &&
			service.LatestVersion != "" &&
			CompareVersions(service.LatestVersion, service.CurrentVersion) > 0
		services = append(services, service)
	}
	sort.SliceStable(services, func(left int, right int) bool {
		return catalogRank(s.catalog, services[left].ID) <
			catalogRank(s.catalog, services[right].ID)
	})
	var checkedAt time.Time
	stateSource := "worker"
	if snapshot != nil {
		checkedAt = snapshot.GeneratedAt
	}
	if checkedAt.IsZero() {
		checkedAt = s.clock().UTC()
		stateSource = "platform-read"
	}
	return SnapshotResult{
		Services:    services,
		CheckedAt:   checkedAt,
		StateSource: stateSource,
	}, nil
}

// AllCurrent reports whether every service is at its newest version. Unknown
// services count as not-current so the console never claims success blindly.
func (s *Service) AllCurrent(services []domain.Service) bool {
	for _, service := range services {
		if service.Status != domain.ServiceStatusCurrent {
			return false
		}
	}
	return len(services) > 0
}

// RequestCheck asks the worker to refresh its snapshot immediately.
func (s *Service) RequestCheck(ctx context.Context) error {
	return s.repository.RequestCheck(ctx)
}

// PlanUpgrade validates a single-service upgrade request against the latest
// snapshot and returns the concrete target version to enqueue.
func (s *Service) PlanUpgrade(
	ctx context.Context,
	serviceID string,
) (UpgradePlan, error) {
	return s.PlanUpgradeTo(ctx, serviceID, "")
}

// PlanUpgradeTo validates a single-service upgrade request. An empty
// targetVersion keeps the upgrade-to-latest behavior; an explicit version must
// exist in the service release list before it can be queued.
func (s *Service) PlanUpgradeTo(
	ctx context.Context,
	serviceID string,
	targetVersion string,
) (UpgradePlan, error) {
	services, _, err := s.Snapshot(ctx)
	if err != nil {
		return UpgradePlan{}, err
	}
	serviceID = strings.TrimSpace(serviceID)
	for _, service := range services {
		if service.ID != serviceID {
			continue
		}
		// An empty target keeps the legacy latest-only contract. An explicit
		// target is an operator-directed version change, so it remains valid
		// for reinstall/downgrade even when the snapshot says current.
		targetVersion = normalizeReleaseVersion(targetVersion)
		if targetVersion == "" {
			if !service.CanUpgrade {
				return UpgradePlan{}, domain.ErrServiceNotUpgradable
			}
			// Empty target_version preserves the legacy behavior: use the
			// snapshot's latest value without an extra repository request.
			return UpgradePlan{
				TargetService: service.ID,
				TargetVersion: service.LatestVersion,
				UpgradesSelf:  service.IsSelf,
			}, nil
		}
		// Explicit operator choice must be verified against the repository,
		// never trusted from the request body or the periodic status cache.
		entry, exists := s.catalogEntry(service.ID)
		if !exists || !entry.AutoUpgrade {
			return UpgradePlan{}, domain.ErrServiceNotUpgradable
		}
		release, err := s.releases.FindRelease(
			ctx,
			entry.ID,
			targetVersion,
		)
		if err != nil {
			return UpgradePlan{}, err
		}
		if release.Version == normalizeReleaseVersion(service.CurrentVersion) {
			return UpgradePlan{}, domain.ErrServiceNotUpgradable
		}
		return UpgradePlan{
			TargetService: service.ID,
			TargetVersion: release.Version,
			UpgradesSelf:  service.IsSelf,
		}, nil
	}
	return UpgradePlan{}, domain.ErrServiceNotFound
}

// Confirmation describes the queued change without exposing repository or
// credential material.
func (s *Service) Confirmation(
	ctx context.Context,
	plan UpgradePlan,
) (UpgradeConfirmation, error) {
	services, _, err := s.Snapshot(ctx)
	if err != nil {
		return UpgradeConfirmation{}, err
	}
	for _, service := range services {
		if service.ID != plan.TargetService {
			continue
		}
		return UpgradeConfirmation{
			ServiceID:      service.ID,
			DisplayName:    service.DisplayName,
			CurrentVersion: service.CurrentVersion,
			TargetVersion:  plan.TargetVersion,
			IsSelf:         plan.UpgradesSelf,
		}, nil
	}
	return UpgradeConfirmation{}, domain.ErrServiceNotFound
}

// ListReleases returns one newest-first page of selectable versions for a
// managed service, annotated against the current and latest worker snapshot.
func (s *Service) ListReleases(
	ctx context.Context,
	serviceID string,
	page int,
	pageSize int,
) (domain.ReleasePage, error) {
	serviceID = strings.TrimSpace(serviceID)
	entry, exists := s.catalogEntry(serviceID)
	if !exists || !entry.AutoUpgrade {
		return domain.ReleasePage{}, domain.ErrServiceNotFound
	}
	services, _, err := s.Snapshot(ctx)
	if err != nil {
		return domain.ReleasePage{}, err
	}
	currentVersion := ""
	for _, service := range services {
		if service.ID == serviceID {
			currentVersion = normalizeReleaseVersion(service.CurrentVersion)
			break
		}
	}
	page, pageSize = normalizeReleasePage(page, pageSize)
	releases, hasMore, err := s.releases.ListReleases(
		ctx,
		entry.ID,
		page,
		pageSize,
	)
	if err != nil {
		return domain.ReleasePage{}, err
	}
	latest, err := s.releases.LatestRelease(ctx, entry.ID)
	if err != nil {
		return domain.ReleasePage{}, err
	}
	for index := range releases {
		releases[index].IsCurrent = releases[index].Version == currentVersion
		releases[index].IsLatest = releases[index].Version == latest.Version
	}
	return domain.ReleasePage{
		Service:  serviceID,
		Releases: releases,
		Page:     page,
		PageSize: pageSize,
		HasMore:  hasMore,
	}, nil
}

// PlanUpgradeAll validates the batch upgrade in the worker's required order.
func (s *Service) PlanUpgradeAll(
	ctx context.Context,
) ([]UpgradePlan, error) {
	services, _, err := s.Snapshot(ctx)
	if err != nil {
		return nil, err
	}
	byID := make(map[string]domain.Service, len(services))
	for _, service := range services {
		byID[service.ID] = service
	}
	plans := make([]UpgradePlan, 0, len(services))
	targets, _, err := s.releases.ListReleases(
		ctx,
		"device_platform",
		1,
		releasePageSizeMax,
	)
	if err != nil {
		return nil, err
	}
	platformLatest := ""
	if len(targets) > 0 {
		platformLatest = targets[0].Version
	}
	gatewayTargets, _, err := s.releases.ListReleases(
		ctx,
		"sub2api",
		1,
		releasePageSizeMax,
	)
	if err != nil {
		return nil, err
	}
	gatewayLatest := ""
	if len(gatewayTargets) > 0 {
		gatewayLatest = gatewayTargets[0].Version
	}
	for _, entry := range s.catalog {
		if !entry.AutoUpgrade {
			continue
		}
		service, exists := byID[entry.ID]
		if !exists || !service.CanUpgrade {
			continue
		}
		targetVersion := platformLatest
		if entry.ID == "sub2api" {
			targetVersion = gatewayLatest
		}
		if targetVersion == "" {
			targetVersion = service.LatestVersion
		}
		plans = append(plans, UpgradePlan{
			TargetService: service.ID,
			TargetVersion: targetVersion,
			UpgradesSelf:  service.IsSelf,
		})
	}
	if len(plans) == 0 {
		return nil, domain.ErrNoUpgradeAvailable
	}
	return plans, nil
}

// Enqueue persists one upgrade command for the worker.
func (s *Service) Enqueue(
	ctx context.Context,
	plan UpgradePlan,
	actorAccountID string,
) (*domain.Operation, error) {
	if err := s.ensureIdle(ctx); err != nil {
		return nil, err
	}
	operationID := uuid.NewString()
	now := s.clock().UTC()
	request := &domain.UpgradeRequest{
		ID:            operationID,
		TargetService: plan.TargetService,
		TargetVersion: plan.TargetVersion,
		RequestedAt:   now,
		RequestedBy:   strings.TrimSpace(actorAccountID),
	}
	if err := s.repository.Enqueue(ctx, request); err != nil {
		return nil, err
	}
	return &domain.Operation{
		ID:            operationID,
		TargetService: plan.TargetService,
		TargetVersion: plan.TargetVersion,
		Status:        domain.OperationStatusQueued,
		Message:       "升级任务已排队，正在准备更新",
		RequestedAt:   &now,
	}, nil
}

// ListOperations returns the upgrade history newest first.
func (s *Service) ListOperations(ctx context.Context) ([]domain.Operation, error) {
	return s.repository.ListOperations(ctx)
}

// EnqueueAll queues one command per outdated service, in the worker's fixed
// order. The worker still runs them one at a time; queueing them together only
// removes the need for an operator to click each service.
func (s *Service) EnqueueAll(
	ctx context.Context,
	plans []UpgradePlan,
	actorAccountID string,
) ([]domain.Operation, error) {
	if len(plans) == 0 {
		return nil, domain.ErrNoUpgradeAvailable
	}
	if err := s.ensureIdle(ctx); err != nil {
		return nil, err
	}
	actorAccountID = strings.TrimSpace(actorAccountID)
	operations := make([]domain.Operation, 0, len(plans))
	for _, plan := range plans {
		operationID := uuid.NewString()
		now := s.clock().UTC()
		request := &domain.UpgradeRequest{
			ID:            operationID,
			TargetService: plan.TargetService,
			TargetVersion: plan.TargetVersion,
			RequestedAt:   now,
			RequestedBy:   actorAccountID,
		}
		if err := s.repository.Enqueue(ctx, request); err != nil {
			return operations, err
		}
		operations = append(operations, domain.Operation{
			ID:            operationID,
			TargetService: plan.TargetService,
			TargetVersion: plan.TargetVersion,
			Status:        domain.OperationStatusQueued,
			Message:       "升级任务已排队，正在准备更新",
			RequestedAt:   &now,
		})
	}
	return operations, nil
}

// FindOperation returns one upgrade task by id.
func (s *Service) FindOperation(
	ctx context.Context,
	operationID string,
) (*domain.Operation, error) {
	return s.repository.FindOperation(ctx, operationID)
}

// ensureIdle refuses a new upgrade while another one is still active so the
// worker's single-upgrade lock is never overloaded.
func (s *Service) ensureIdle(ctx context.Context) error {
	operations, err := s.repository.ListOperations(ctx)
	if err != nil {
		return err
	}
	for _, operation := range operations {
		if domain.IsActiveOperation(operation.Status) {
			return domain.ErrUpgradeInProgress
		}
	}
	return nil
}

func (s *Service) catalogEntry(serviceID string) (domain.CatalogEntry, bool) {
	for _, entry := range s.catalog {
		if entry.ID == serviceID {
			return entry, true
		}
	}
	return domain.CatalogEntry{}, false
}

func snapshotServices(snapshot *domain.StatusSnapshot) []domain.Service {
	if snapshot == nil {
		return nil
	}
	return snapshot.Services
}

// catalogRank preserves the operator-facing order from defaultCatalog.
func catalogRank(catalog []domain.CatalogEntry, id string) int {
	for index, entry := range catalog {
		if entry.ID == id {
			return index
		}
	}
	return len(catalog)
}

// defaultCatalog is the single source of truth for managed image identity and
// upgrade eligibility. Infrastructure images are observation-only.
func defaultCatalog() []domain.CatalogEntry {
	return []domain.CatalogEntry{
		{
			ID:             "sub2api",
			DisplayName:    "AI 网关",
			Role:           "AI 网关",
			Image:          "ghcr.io/tissyboxc/sub2api",
			AutoUpgrade:    true,
			PlatformScoped: false,
		},
		{
			ID:             "device_platform",
			DisplayName:    "设备平台",
			Role:           "平台服务",
			Image:          "ghcr.io/tissyboxc/sprout-device-platform",
			AutoUpgrade:    true,
			PlatformScoped: true,
		},
		{
			ID:             "voice_gateway",
			DisplayName:    "语音网关",
			Role:           "平台服务",
			Image:          "ghcr.io/tissyboxc/sprout-voice-gateway",
			AutoUpgrade:    true,
			PlatformScoped: true,
		},
		{
			ID:             "admin_web",
			DisplayName:    "品牌管理端",
			Role:           "平台服务",
			Image:          "ghcr.io/tissyboxc/sprout-admin-web",
			AutoUpgrade:    true,
			PlatformScoped: true,
			IsSelf:         true,
		},
		{
			ID:          "postgres",
			DisplayName: "数据库",
			Role:        "基础设施",
			Image:       "postgres",
		},
		{
			ID:          "redis",
			DisplayName: "缓存服务",
			Role:        "基础设施",
			Image:       "redis",
		},
		{
			ID:          "mqtt",
			DisplayName: "消息服务",
			Role:        "基础设施",
			Image:       "eclipse-mosquitto",
		},
		{
			ID:          "download_init",
			DisplayName: "下载服务初始化",
			Role:        "发布服务",
			Image:       "alpine",
		},
		{
			ID:          "download_ftp",
			DisplayName: "发布文件传输",
			Role:        "发布服务",
			Image:       "atmoz/sftp",
		},
		{
			ID:          "download_http",
			DisplayName: "发布文件下载",
			Role:        "发布服务",
			Image:       "nginx",
		},
	}
}

// CompareVersions compares dotted semantic versions numerically.
func CompareVersions(left string, right string) int {
	return operationsdomain.CompareVersions(left, right)
}
