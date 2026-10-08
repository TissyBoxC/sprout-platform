package http

import (
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/TissyBoxC/sprout-platform/packages/go/httpapi"
	"github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/service_version/domain"
	serviceversionservice "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/service_version/service"
)

// versionSnapshotResponse is the console-facing service inventory.
type versionSnapshotResponse struct {
	Services    []domain.Service `json:"services"`
	CheckedAt   *time.Time       `json:"checked_at"`
	AllUpToDate bool             `json:"all_up_to_date"`
}

type upgradeServiceRequest struct {
	TargetVersion string `json:"target_version"`
}

func (handler adminHandler) listServiceVersions(
	response http.ResponseWriter,
	request *http.Request,
) {
	services, checkedAt, err := handler.serviceVersionService.Snapshot(request.Context())
	if err != nil {
		writeError(response, request, http.StatusInternalServerError, "service_error", "暂时无法读取服务版本")
		return
	}
	writeSuccess(response, request, http.StatusOK, versionSnapshotResponse{
		Services:    services,
		CheckedAt:   timePointer(checkedAt),
		AllUpToDate: handler.serviceVersionService.AllCurrent(services),
	})
}

func (handler adminHandler) checkServiceVersions(
	response http.ResponseWriter,
	request *http.Request,
) {
	if err := handler.serviceVersionService.RequestCheck(request.Context()); err != nil {
		writeError(response, request, http.StatusInternalServerError, "service_error", "检查更新没有开始，请稍后重试")
		return
	}
	// The worker refreshes asynchronously; return the current known snapshot
	// immediately so the console can render without waiting on the network.
	services, checkedAt, err := handler.serviceVersionService.Snapshot(request.Context())
	if err != nil {
		writeError(response, request, http.StatusInternalServerError, "service_error", "暂时无法读取服务版本")
		return
	}
	writeSuccess(response, request, http.StatusOK, versionSnapshotResponse{
		Services:    services,
		CheckedAt:   timePointer(checkedAt),
		AllUpToDate: handler.serviceVersionService.AllCurrent(services),
	})
}

func (handler adminHandler) upgradeService(
	response http.ResponseWriter,
	request *http.Request,
) {
	accountID, ok := authenticatedAccountID(request)
	if !ok {
		writeError(response, request, http.StatusUnauthorized, "unauthenticated", "请重新登录")
		return
	}
	serviceID := strings.TrimSpace(request.PathValue("service"))
	var payload upgradeServiceRequest
	if err := decodeOptionalJSON(request, &payload); err != nil {
		writeError(response, request, http.StatusBadRequest, "invalid_request", "请选择要升级到的版本")
		return
	}
	plan, err := handler.serviceVersionService.PlanUpgradeTo(
		request.Context(),
		serviceID,
		payload.TargetVersion,
	)
	if err != nil {
		writeServiceVersionError(response, request, err)
		return
	}
	confirmation, err := handler.serviceVersionService.Confirmation(
		request.Context(),
		plan,
	)
	if err != nil {
		writeServiceVersionError(response, request, err)
		return
	}
	operation, err := handler.serviceVersionService.Enqueue(
		request.Context(),
		plan,
		accountID,
	)
	if err != nil {
		writeServiceVersionError(response, request, err)
		return
	}
	writeSuccess(response, request, http.StatusAccepted, map[string]any{
		"operation":    operation,
		"confirmation": confirmation,
	})
}

func (handler adminHandler) listServiceReleases(
	response http.ResponseWriter,
	request *http.Request,
) {
	serviceID := strings.TrimSpace(request.PathValue("service"))
	page := parsePositiveQueryInt(request.URL.Query().Get("page"), 1)
	pageSize := parsePositiveQueryInt(
		request.URL.Query().Get("page_size"),
		serviceversionservice.ReleaseCatalogLimit,
	)
	releasePage, err := handler.serviceVersionService.ListReleases(
		request.Context(),
		serviceID,
		page,
		pageSize,
	)
	if err != nil {
		writeServiceVersionError(response, request, err)
		return
	}
	writeSuccess(response, request, http.StatusOK, releasePage)
}

func (handler adminHandler) upgradeAllServices(
	response http.ResponseWriter,
	request *http.Request,
) {
	accountID, ok := authenticatedAccountID(request)
	if !ok {
		writeError(response, request, http.StatusUnauthorized, "unauthenticated", "请重新登录")
		return
	}
	plans, err := handler.serviceVersionService.PlanUpgradeAll(request.Context())
	if err != nil {
		writeServiceVersionError(response, request, err)
		return
	}
	operations, err := handler.serviceVersionService.EnqueueAll(
		request.Context(),
		plans,
		accountID,
	)
	if err != nil {
		writeServiceVersionError(response, request, err)
		return
	}
	// The frontend tracks a single batch through the first operation id and
	// reads the rest from the operation list.
	var first any
	if len(operations) > 0 {
		first = operations[0]
	}
	writeSuccess(response, request, http.StatusAccepted, map[string]any{
		"operation":  first,
		"operations": operations,
	})
}

func (handler adminHandler) listServiceVersionOperations(
	response http.ResponseWriter,
	request *http.Request,
) {
	operations, err := handler.serviceVersionService.ListOperations(request.Context())
	if err != nil {
		writeError(response, request, http.StatusInternalServerError, "service_error", "暂时无法读取升级记录")
		return
	}
	writeSuccess(response, request, http.StatusOK, map[string]any{
		"operations": operations,
	})
}

func (handler adminHandler) getServiceVersionOperation(
	response http.ResponseWriter,
	request *http.Request,
) {
	operationID := strings.TrimSpace(request.PathValue("operation_id"))
	operation, err := handler.serviceVersionService.FindOperation(
		request.Context(),
		operationID,
	)
	if err != nil {
		if errors.Is(err, domain.ErrOperationNotFound) {
			writeError(response, request, http.StatusNotFound, "operation_not_found", "没有找到这个升级任务")
			return
		}
		writeError(response, request, http.StatusInternalServerError, "service_error", "暂时无法读取升级进度")
		return
	}
	writeSuccess(response, request, http.StatusOK, map[string]any{
		"operation": operation,
	})
}

func writeServiceVersionError(
	response http.ResponseWriter,
	request *http.Request,
	err error,
) {
	switch {
	case errors.Is(err, domain.ErrServiceNotFound):
		writeError(response, request, http.StatusNotFound, "service_not_found", "没有找到这个服务")
	case errors.Is(err, domain.ErrServiceNotUpgradable):
		writeError(response, request, http.StatusUnprocessableEntity, "service_not_upgradable", "这个服务当前不需要升级")
	case errors.Is(err, domain.ErrReleaseNotFound):
		writeError(response, request, http.StatusNotFound, "release_not_found", "没有找到这个发布版本")
	case errors.Is(err, domain.ErrReleaseRepositoryNotFound):
		writeError(response, request, http.StatusNotFound, "release_repository_not_found", "没有找到这个服务的发布仓库")
	case errors.Is(err, domain.ErrReleaseCatalogNotReady):
		writeRetryableError(
			response,
			request,
			http.StatusServiceUnavailable,
			"release_catalog_not_ready",
			"版本目录正在准备，稍后刷新即可",
		)
	case errors.Is(err, domain.ErrReleaseSourceFailed):
		writeError(response, request, http.StatusBadGateway, "release_source_unavailable", "暂时无法读取可选版本，请稍后重试")
	case errors.Is(err, domain.ErrNoUpgradeAvailable):
		writeError(response, request, http.StatusConflict, "already_current", "所有服务都已是最新版本")
	case errors.Is(err, domain.ErrUpgradeInProgress):
		writeError(response, request, http.StatusConflict, "upgrade_in_progress", "已有升级任务正在进行，请等待完成")
	default:
		writeError(response, request, http.StatusInternalServerError, "service_error", "升级任务没有开始，请稍后重试")
	}
}

func writeRetryableError(
	response http.ResponseWriter,
	request *http.Request,
	status int,
	code string,
	message string,
) {
	httpapi.WriteError(response, request, status, code, message, true)
}

func parsePositiveQueryInt(value string, fallback int) int {
	parsed, err := strconv.Atoi(strings.TrimSpace(value))
	if err != nil || parsed < 1 {
		return fallback
	}
	return parsed
}

// decodeOptionalJSON accepts an absent body while still rejecting malformed
// JSON, which keeps the existing no-body upgrade request compatible.
func decodeOptionalJSON(request *http.Request, destination any) error {
	if request.Body == nil {
		return nil
	}
	err := decodeJSON(request, destination)
	if errors.Is(err, io.EOF) {
		return nil
	}
	return err
}

func timePointer(value time.Time) *time.Time {
	if value.IsZero() {
		return nil
	}
	return &value
}
