package http

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/ota/domain"
	otaservice "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/ota/service"
)

// DeviceSessionVerifier resolves the owning device id for a device session.
type DeviceSessionVerifier interface {
	VerifyDeviceSession(ctx context.Context, deviceSessionToken string) (string, error)
}

// GuardianDeviceVerifier resolves a guardian-owned device by session account.
type GuardianDeviceVerifier interface {
	DeviceBelongsToParent(
		ctx context.Context,
		parentAccountID string,
		deviceID string,
	) (bool, error)
}

type otaHandler struct {
	service         *otaservice.Service
	deviceSessions  DeviceSessionVerifier
	guardianDevices GuardianDeviceVerifier
}

func newOTAHandler(
	service *otaservice.Service,
	deviceSessions DeviceSessionVerifier,
	guardianDevices GuardianDeviceVerifier,
) otaHandler {
	return otaHandler{
		service:         service,
		deviceSessions:  deviceSessions,
		guardianDevices: guardianDevices,
	}
}

type otaReleaseMutation struct {
	FirmwareVersion    string `json:"firmware_version"`
	HardwareRevision   string `json:"hardware_revision"`
	Channel            string `json:"channel"`
	ArtifactKey        string `json:"artifact_key"`
	ArtifactURL        string `json:"artifact_url"`
	SHA256             string `json:"sha256"`
	SizeBytes          int64  `json:"size_bytes"`
	SignatureKeyID     string `json:"signature_key_id"`
	SignatureAlgorithm string `json:"signature_algorithm"`
	Signature          string `json:"signature"`
	RollbackAllowed    bool   `json:"rollback_allowed"`
	Mandatory          bool   `json:"mandatory"`
	MinSourceVersion   string `json:"min_source_version"`
	ReleaseNotes       string `json:"release_notes"`
	TargetType         string `json:"target_type"`
	TargetID           string `json:"target_id"`
	CanaryPercent      int    `json:"canary_percent"`
	ExpectedVersion    int64  `json:"expected_version"`
}

type otaDeviceEventRequest struct {
	EventID         string         `json:"event_id"`
	DeploymentID    string         `json:"deployment_id"`
	ReleaseID       string         `json:"release_id"`
	EventType       string         `json:"event_type"`
	Sequence        int64          `json:"sequence"`
	ProgressPercent int            `json:"progress_percent"`
	BytesReceived   int64          `json:"bytes_received"`
	BytesTotal      int64          `json:"bytes_total"`
	ErrorCode       string         `json:"error_code"`
	Message         string         `json:"message"`
	Detail          map[string]any `json:"detail"`
	OccurredAt      string         `json:"occurred_at"`
}

type otaInstallRequest struct {
	DeploymentID string `json:"deployment_id"`
}

func (handler otaHandler) registerRoutes(
	mux *http.ServeMux,
	requireAuthentication func(http.HandlerFunc) http.HandlerFunc,
	requireAdmin func(http.HandlerFunc) http.HandlerFunc,
) {
	if handler.service == nil {
		return
	}
	mux.HandleFunc("GET /api/v1/devices/{device_id}/ota", handler.deviceUpdate)
	mux.HandleFunc("POST /api/v1/devices/{device_id}/ota/events", handler.recordDeviceEvent)
	if requireAuthentication != nil {
		mux.HandleFunc(
			"POST /api/v1/devices/{device_id}/ota/install",
			requireAuthentication(handler.installForGuardian),
		)
		mux.HandleFunc(
			"GET /api/v1/devices/{device_id}/ota/status",
			requireAuthentication(handler.statusForGuardian),
		)
		mux.HandleFunc(
			"POST /api/v1/devices/{device_id}/ota/retry",
			requireAuthentication(handler.retryForGuardian),
		)
		mux.HandleFunc(
			"POST /api/v1/devices/{device_id}/ota/rollback",
			requireAuthentication(handler.rollbackForGuardian),
		)
	}
	if requireAdmin == nil {
		return
	}
	mux.HandleFunc(
		"GET /api/v1/admin/ota/releases",
		requireAdmin(handler.listAdminReleases),
	)
	mux.HandleFunc(
		"POST /api/v1/admin/ota/releases",
		requireAdmin(handler.createAdminRelease),
	)
	mux.HandleFunc(
		"GET /api/v1/admin/ota/releases/{release_id}",
		requireAdmin(handler.getAdminRelease),
	)
	mux.HandleFunc(
		"PUT /api/v1/admin/ota/releases/{release_id}",
		requireAdmin(handler.updateAdminRelease),
	)
	mux.HandleFunc(
		"POST /api/v1/admin/ota/releases/{release_id}/publish",
		requireAdmin(handler.publishAdminRelease),
	)
	mux.HandleFunc(
		"POST /api/v1/admin/ota/releases/{release_id}/pause",
		requireAdmin(handler.pauseAdminRelease),
	)
	mux.HandleFunc(
		"POST /api/v1/admin/ota/releases/{release_id}/withdraw",
		requireAdmin(handler.withdrawAdminRelease),
	)
	mux.HandleFunc(
		"POST /api/v1/admin/ota/releases/{release_id}/rollback",
		requireAdmin(handler.rollbackAdminRelease),
	)
	mux.HandleFunc(
		"GET /api/v1/admin/ota/releases/{release_id}/deployments",
		requireAdmin(handler.listAdminDeployments),
	)
	mux.HandleFunc(
		"POST /api/v1/admin/ota/deployments/{deployment_id}/retry",
		requireAdmin(handler.retryAdminDeployment),
	)
	mux.HandleFunc(
		"GET /api/v1/admin/ota/statistics",
		requireAdmin(handler.adminStatistics),
	)
}

func (handler otaHandler) deviceUpdate(
	response http.ResponseWriter,
	request *http.Request,
) {
	deviceID, ok := handler.verifyDeviceSession(response, request)
	if !ok {
		return
	}
	channel := domain.Channel(strings.TrimSpace(request.URL.Query().Get("channel")))
	current, err := handler.service.CurrentDeviceUpdate(request.Context(), deviceID)
	if err != nil && !errors.Is(err, domain.ErrNoUpdateAvailable) {
		handler.writeError(response, request, err)
		return
	}
	if current != nil {
		handler.writeDeviceUpdate(
			response,
			request,
			deviceID,
			current.Release.DeviceManifest(),
			current.CurrentVersion,
			&current.Deployment,
			string(current.Deployment.Status),
			current.UpdateAvailable,
			true,
		)
		return
	}
	release, err := handler.service.DeviceUpdate(request.Context(), deviceID, channel)
	if errors.Is(err, domain.ErrNoUpdateAvailable) {
		currentVersion, versionErr := handler.service.DeviceCurrentVersion(
			request.Context(),
			deviceID,
		)
		if versionErr != nil {
			currentVersion = ""
		}
		writeSuccess(response, request, http.StatusOK, map[string]any{
			"device_id":          deviceID,
			"status":             "up_to_date",
			"current_version":    currentVersion,
			"update_available":   false,
			"rollback_available": false,
			"checked_at":         time.Now().UTC(),
		})
		return
	}
	if err != nil {
		handler.writeError(response, request, err)
		return
	}
	writeSuccess(response, request, http.StatusOK, map[string]any{
		"device_id":          deviceID,
		"status":             "available",
		"update_available":   true,
		"rollback_available": false,
		"target_version":     release.Version,
		"checked_at":         time.Now().UTC(),
		"release":            otaDeviceReleaseResponse(*release),
	})
}

func (handler otaHandler) recordDeviceEvent(
	response http.ResponseWriter,
	request *http.Request,
) {
	deviceID, ok := handler.verifyDeviceSession(response, request)
	if !ok {
		return
	}
	var payload otaDeviceEventRequest
	if err := decodeJSON(request, &payload); err != nil {
		writeError(
			response,
			request,
			http.StatusBadRequest,
			"invalid_request",
			"设备更新信息不完整",
		)
		return
	}
	var reportedAt time.Time
	if strings.TrimSpace(payload.OccurredAt) != "" {
		parsed, err := time.Parse(time.RFC3339, payload.OccurredAt)
		if err != nil {
			writeError(
				response,
				request,
				http.StatusUnprocessableEntity,
				"invalid_reported_at",
				"设备时间需要重新校准",
			)
			return
		}
		reportedAt = parsed
	}
	detail := payload.Detail
	if detail == nil {
		detail = map[string]any{}
	}
	if strings.TrimSpace(payload.ReleaseID) != "" {
		detail["release_id"] = strings.TrimSpace(payload.ReleaseID)
	}
	_, inserted, err := handler.service.RecordEvent(
		request.Context(),
		domain.EventInput{
			DeploymentID:    payload.DeploymentID,
			DeviceID:        deviceID,
			EventID:         payload.EventID,
			Type:            domain.EventType(payload.EventType),
			Sequence:        payload.Sequence,
			ProgressPercent: payload.ProgressPercent,
			BytesReceived:   payload.BytesReceived,
			BytesTotal:      payload.BytesTotal,
			ErrorCode:       payload.ErrorCode,
			Message:         payload.Message,
			Detail:          detail,
			ReportedAt:      reportedAt,
		},
	)
	if err != nil {
		handler.writeError(response, request, err)
		return
	}
	writeSuccess(response, request, http.StatusOK, map[string]any{
		"accepted":  true,
		"duplicate": !inserted,
	})
}

func (handler otaHandler) installForGuardian(
	response http.ResponseWriter,
	request *http.Request,
) {
	deviceID, parentID, ok := handler.verifyGuardianDevice(response, request)
	if !ok {
		return
	}
	var payload otaInstallRequest
	if request.Body != nil && request.ContentLength != 0 {
		if err := decodeJSON(request, &payload); err != nil {
			writeError(
				response,
				request,
				http.StatusBadRequest,
				"invalid_request",
				"请检查更新请求",
			)
			return
		}
	}
	release, err := handler.service.DeviceUpdate(request.Context(), deviceID, "")
	if err != nil {
		handler.writeError(response, request, err)
		return
	}
	requestID := strings.TrimSpace(payload.DeploymentID)
	if requestID == "" {
		requestID = "guardian:" + parentID + ":" + deviceID + ":" + release.ReleaseID
	}
	deployment, err := handler.service.AssignRelease(
		request.Context(),
		release.ReleaseID,
		deviceID,
		requestID,
		parentID,
	)
	if err != nil {
		handler.writeError(response, request, err)
		return
	}
	handler.writeDeviceUpdate(
		response,
		request,
		deviceID,
		*release,
		handler.currentVersion(request, deviceID),
		deployment,
		string(deployment.Status),
		true,
		true,
	)
}

func (handler otaHandler) statusForGuardian(
	response http.ResponseWriter,
	request *http.Request,
) {
	deviceID, _, ok := handler.verifyGuardianDevice(response, request)
	if !ok {
		return
	}
	current, err := handler.service.CurrentDeviceUpdate(request.Context(), deviceID)
	if err != nil && !errors.Is(err, domain.ErrNoUpdateAvailable) {
		handler.writeError(response, request, err)
		return
	}
	if current == nil {
		release, updateErr := handler.service.DeviceUpdate(
			request.Context(),
			deviceID,
			"",
		)
		if errors.Is(updateErr, domain.ErrNoUpdateAvailable) {
			handler.writeCurrentDeviceUpdate(response, request, deviceID)
			return
		}
		if updateErr != nil {
			handler.writeError(response, request, updateErr)
			return
		}
		handler.writeDeviceUpdate(
			response,
			request,
			deviceID,
			*release,
			handler.currentVersion(request, deviceID),
			nil,
			"available",
			true,
			true,
		)
		return
	}
	handler.writeDeviceUpdate(
		response,
		request,
		deviceID,
		current.Release.DeviceManifest(),
		current.CurrentVersion,
		&current.Deployment,
		string(current.Deployment.Status),
		current.UpdateAvailable,
		true,
	)
}

func (handler otaHandler) retryForGuardian(
	response http.ResponseWriter,
	request *http.Request,
) {
	deviceID, parentID, ok := handler.verifyGuardianDevice(response, request)
	if !ok {
		return
	}
	deployments, err := handler.service.ListDeployments(
		request.Context(),
		domain.DeploymentFilter{
			DeviceID: deviceID,
			Page:     1,
			PageSize: 1,
		},
	)
	if err != nil || len(deployments.Items) == 0 {
		if err == nil {
			err = domain.ErrDeploymentNotFound
		}
		handler.writeError(response, request, err)
		return
	}
	deployment := deployments.Items[0]
	if deployment.Status != domain.DeploymentStatusFailed {
		handler.writeError(response, request, domain.ErrDeploymentStateConflict)
		return
	}
	updated, err := handler.service.RetryDeployment(
		request.Context(),
		deployment.ID,
		parentID,
		deployment.RecordVersion,
	)
	if err != nil {
		handler.writeError(response, request, err)
		return
	}
	release, err := handler.service.GetRelease(request.Context(), updated.ReleaseID)
	if err != nil {
		handler.writeError(response, request, err)
		return
	}
	handler.writeDeviceUpdate(
		response,
		request,
		deviceID,
		release.DeviceManifest(),
		handler.currentVersion(request, deviceID),
		updated,
		string(updated.Status),
		true,
		true,
	)
}

func (handler otaHandler) rollbackForGuardian(
	response http.ResponseWriter,
	request *http.Request,
) {
	deviceID, parentID, ok := handler.verifyGuardianDevice(response, request)
	if !ok {
		return
	}
	current, err := handler.service.CurrentDeviceUpdate(request.Context(), deviceID)
	if err != nil {
		handler.writeError(response, request, err)
		return
	}
	deployment, err := handler.service.RollbackDeployment(
		request.Context(),
		current.Deployment.ID,
		parentID,
		current.Deployment.RecordVersion,
	)
	if err != nil {
		handler.writeError(response, request, err)
		return
	}
	release, err := handler.service.GetRelease(request.Context(), deployment.ReleaseID)
	if err != nil {
		handler.writeError(response, request, err)
		return
	}
	handler.writeDeviceUpdate(
		response,
		request,
		deviceID,
		release.DeviceManifest(),
		handler.currentVersion(request, deviceID),
		deployment,
		string(deployment.Status),
		false,
		true,
	)
}

func (handler otaHandler) listAdminReleases(
	response http.ResponseWriter,
	request *http.Request,
) {
	filter, err := adminReleaseFilter(request)
	if err != nil {
		writeError(
			response,
			request,
			http.StatusUnprocessableEntity,
			"invalid_page",
			"请检查分页参数",
		)
		return
	}
	page, err := handler.service.ListReleases(request.Context(), filter)
	if err != nil {
		handler.writeError(response, request, err)
		return
	}
	writeSuccess(response, request, http.StatusOK, map[string]any{
		"releases":  otaReleaseResponses(page.Items),
		"total":     page.Total,
		"page":      page.Page,
		"page_size": page.PageSize,
	})
}

func (handler otaHandler) createAdminRelease(
	response http.ResponseWriter,
	request *http.Request,
) {
	var payload otaReleaseMutation
	if err := decodeJSON(request, &payload); err != nil {
		writeError(
			response,
			request,
			http.StatusBadRequest,
			"invalid_request",
			"请检查更新内容",
		)
		return
	}
	accountID, ok := authenticatedAccountID(request)
	if !ok {
		writeError(
			response,
			request,
			http.StatusUnauthorized,
			"unauthenticated",
			"请重新登录",
		)
		return
	}
	release, err := handler.service.CreateReleaseDraft(
		request.Context(),
		releaseInputFromMutation(payload, accountID, 0),
	)
	if err != nil {
		handler.writeError(response, request, err)
		return
	}
	writeSuccess(response, request, http.StatusCreated, otaReleaseResponse(*release))
}

func (handler otaHandler) getAdminRelease(
	response http.ResponseWriter,
	request *http.Request,
) {
	release, err := handler.service.GetRelease(
		request.Context(),
		request.PathValue("release_id"),
	)
	if err != nil {
		handler.writeError(response, request, err)
		return
	}
	versions, err := handler.service.ListReleases(
		request.Context(),
		domain.ReleaseFilter{
			Version:  release.Version,
			Page:     1,
			PageSize: 100,
		},
	)
	if err != nil {
		handler.writeError(response, request, err)
		return
	}
	writeSuccess(response, request, http.StatusOK, map[string]any{
		"release":  otaReleaseResponse(*release),
		"versions": otaReleaseResponses(versions.Items),
		"rollback": map[string]any{
			"allowed":        release.RollbackAllowed,
			"target_version": release.RollbackReleaseID,
		},
	})
}

func (handler otaHandler) updateAdminRelease(
	response http.ResponseWriter,
	request *http.Request,
) {
	var payload otaReleaseMutation
	if err := decodeJSON(request, &payload); err != nil {
		writeError(
			response,
			request,
			http.StatusBadRequest,
			"invalid_request",
			"请检查更新内容",
		)
		return
	}
	accountID, ok := authenticatedAccountID(request)
	if !ok {
		writeError(
			response,
			request,
			http.StatusUnauthorized,
			"unauthenticated",
			"请重新登录",
		)
		return
	}
	release, err := handler.service.UpdateReleaseDraft(
		request.Context(),
		request.PathValue("release_id"),
		releaseInputFromMutation(payload, accountID, payload.ExpectedVersion),
	)
	if err != nil {
		handler.writeError(response, request, err)
		return
	}
	writeSuccess(response, request, http.StatusOK, otaReleaseResponse(*release))
}

func (handler otaHandler) publishAdminRelease(
	response http.ResponseWriter,
	request *http.Request,
) {
	handler.transitionAdminRelease(
		response,
		request,
		func(actorID string, expectedVersion int64) (*domain.Release, error) {
			return handler.service.PublishRelease(
				request.Context(),
				request.PathValue("release_id"),
				actorID,
				expectedVersion,
			)
		},
	)
}

func (handler otaHandler) pauseAdminRelease(
	response http.ResponseWriter,
	request *http.Request,
) {
	handler.transitionAdminRelease(
		response,
		request,
		func(actorID string, expectedVersion int64) (*domain.Release, error) {
			return handler.service.PauseRelease(
				request.Context(),
				request.PathValue("release_id"),
				actorID,
				expectedVersion,
			)
		},
	)
}

func (handler otaHandler) withdrawAdminRelease(
	response http.ResponseWriter,
	request *http.Request,
) {
	handler.transitionAdminRelease(
		response,
		request,
		func(actorID string, expectedVersion int64) (*domain.Release, error) {
			return handler.service.WithdrawRelease(
				request.Context(),
				request.PathValue("release_id"),
				actorID,
				expectedVersion,
			)
		},
	)
}

func (handler otaHandler) rollbackAdminRelease(
	response http.ResponseWriter,
	request *http.Request,
) {
	handler.transitionAdminRelease(
		response,
		request,
		func(actorID string, expectedVersion int64) (*domain.Release, error) {
			return handler.service.RollbackRelease(
				request.Context(),
				request.PathValue("release_id"),
				actorID,
				expectedVersion,
			)
		},
	)
}

func (handler otaHandler) listAdminDeployments(
	response http.ResponseWriter,
	request *http.Request,
) {
	page, pageSize, err := pageParams(request)
	if err != nil {
		writeError(
			response,
			request,
			http.StatusUnprocessableEntity,
			"invalid_page",
			"请检查分页参数",
		)
		return
	}
	result, err := handler.service.ListDeployments(
		request.Context(),
		domain.DeploymentFilter{
			ReleaseID: request.PathValue("release_id"),
			Status: domain.DeploymentStatus(
				strings.TrimSpace(request.URL.Query().Get("status")),
			),
			Page:     page,
			PageSize: pageSize,
		},
	)
	if err != nil {
		handler.writeError(response, request, err)
		return
	}
	writeSuccess(response, request, http.StatusOK, map[string]any{
		"deployments": result.Items,
		"total":       result.Total,
		"page":        result.Page,
		"page_size":   result.PageSize,
	})
}

func (handler otaHandler) retryAdminDeployment(
	response http.ResponseWriter,
	request *http.Request,
) {
	accountID, ok := authenticatedAccountID(request)
	if !ok {
		writeError(
			response,
			request,
			http.StatusUnauthorized,
			"unauthenticated",
			"请重新登录",
		)
		return
	}
	deployment, err := handler.service.GetDeployment(
		request.Context(),
		request.PathValue("deployment_id"),
	)
	if err != nil {
		handler.writeError(response, request, err)
		return
	}
	updated, err := handler.service.RetryDeployment(
		request.Context(),
		deployment.ID,
		accountID,
		deployment.RecordVersion,
	)
	if err != nil {
		handler.writeError(response, request, err)
		return
	}
	writeSuccess(response, request, http.StatusAccepted, updated)
}

func (handler otaHandler) adminStatistics(
	response http.ResponseWriter,
	request *http.Request,
) {
	statistics, err := handler.service.Statistics(
		request.Context(),
		domain.DeploymentFilter{},
	)
	if err != nil {
		handler.writeError(response, request, err)
		return
	}
	writeSuccess(response, request, http.StatusOK, map[string]any{
		"published_release_count":    statistics.ByStatus["published"],
		"canary_release_count":       statistics.ByChannel["canary"],
		"active_deployment_count":    activeDeploymentCount(statistics.ByStatus),
		"succeeded_deployment_count": statistics.ByStatus["succeeded"],
		"failed_deployment_count":    statistics.Failed,
		"rollback_count":             statistics.RolledBack,
		"failure_rate":               statistics.FailureRate,
		"rollback_rate":              rollbackRate(statistics),
	})
}

func activeDeploymentCount(byStatus map[string]int64) int64 {
	var total int64
	for _, status := range []string{
		"queued",
		"offered",
		"downloading",
		"validating",
		"installing",
		"pending_verify",
	} {
		total += byStatus[status]
	}
	return total
}

func rollbackRate(statistics *domain.Statistics) float64 {
	if statistics == nil || statistics.Total == 0 {
		return 0
	}
	return float64(statistics.RolledBack) / float64(statistics.Total)
}

func (handler otaHandler) transitionAdminRelease(
	response http.ResponseWriter,
	request *http.Request,
	transition func(actorID string, expectedVersion int64) (*domain.Release, error),
) {
	accountID, ok := authenticatedAccountID(request)
	if !ok {
		writeError(
			response,
			request,
			http.StatusUnauthorized,
			"unauthenticated",
			"请重新登录",
		)
		return
	}
	releaseID := request.PathValue("release_id")
	current, err := handler.service.GetRelease(request.Context(), releaseID)
	if err != nil {
		handler.writeError(response, request, err)
		return
	}
	expectedVersion := current.RecordVersion
	if raw := strings.TrimSpace(request.URL.Query().Get("expected_version")); raw != "" {
		parsed, parseErr := strconv.ParseInt(raw, 10, 64)
		if parseErr != nil || parsed <= 0 {
			writeError(
				response,
				request,
				http.StatusUnprocessableEntity,
				"invalid_version",
				"更新版本已变化，请重新加载",
			)
			return
		}
		expectedVersion = parsed
	}
	release, err := transition(accountID, expectedVersion)
	if err != nil {
		handler.writeError(response, request, err)
		return
	}
	writeSuccess(response, request, http.StatusOK, otaReleaseResponse(*release))
}

func (handler otaHandler) verifyDeviceSession(
	response http.ResponseWriter,
	request *http.Request,
) (string, bool) {
	if handler.deviceSessions == nil {
		writeError(
			response,
			request,
			http.StatusUnauthorized,
			"device_session_expired",
			"设备登录已过期，请重新连接",
		)
		return "", false
	}
	token, ok := bearerToken(request)
	if !ok {
		writeError(
			response,
			request,
			http.StatusUnauthorized,
			"device_session_expired",
			"设备登录已过期，请重新连接",
		)
		return "", false
	}
	deviceID, err := handler.deviceSessions.VerifyDeviceSession(
		request.Context(),
		token,
	)
	if err != nil || deviceID != strings.TrimSpace(request.PathValue("device_id")) {
		writeError(
			response,
			request,
			http.StatusUnauthorized,
			"device_session_expired",
			"设备登录已过期，请重新连接",
		)
		return "", false
	}
	return deviceID, true
}

func (handler otaHandler) verifyGuardianDevice(
	response http.ResponseWriter,
	request *http.Request,
) (string, string, bool) {
	parentID, ok := authenticatedAccountID(request)
	if !ok {
		writeError(
			response,
			request,
			http.StatusUnauthorized,
			"unauthenticated",
			"请重新登录",
		)
		return "", "", false
	}
	deviceID := strings.TrimSpace(request.PathValue("device_id"))
	if handler.guardianDevices == nil {
		writeError(
			response,
			request,
			http.StatusNotFound,
			"device_not_found",
			"没有找到这台设备",
		)
		return "", "", false
	}
	owned, err := handler.guardianDevices.DeviceBelongsToParent(
		request.Context(),
		parentID,
		deviceID,
	)
	if err != nil || !owned {
		writeError(
			response,
			request,
			http.StatusNotFound,
			"device_not_found",
			"没有找到这台设备",
		)
		return "", "", false
	}
	return deviceID, parentID, true
}

func (handler otaHandler) writeCurrentDeviceUpdate(
	response http.ResponseWriter,
	request *http.Request,
	deviceID string,
) {
	currentVersion := handler.currentVersion(request, deviceID)
	writeSuccess(response, request, http.StatusOK, map[string]any{
		"device_id":          deviceID,
		"status":             "up_to_date",
		"current_version":    currentVersion,
		"update_available":   false,
		"rollback_available": false,
		"checked_at":         time.Now().UTC(),
	})
}

func (handler otaHandler) currentVersion(
	request *http.Request,
	deviceID string,
) string {
	currentVersion, err := handler.service.DeviceCurrentVersion(
		request.Context(),
		deviceID,
	)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(currentVersion)
}

func (handler otaHandler) writeDeviceUpdate(
	response http.ResponseWriter,
	request *http.Request,
	deviceID string,
	release domain.DeviceRelease,
	currentVersion string,
	deployment *domain.Deployment,
	status string,
	updateAvailable bool,
	rollbackAvailable bool,
) {
	currentVersion = strings.TrimSpace(currentVersion)
	payload := map[string]any{
		"device_id":          deviceID,
		"status":             status,
		"current_version":    currentVersion,
		"target_version":     release.Version,
		"update_available":   updateAvailable,
		"rollback_available": rollbackAvailable,
		"checked_at":         time.Now().UTC(),
		"release":            otaDeviceReleaseResponse(release),
	}
	if deployment != nil {
		payload["deployment_id"] = deployment.ID
		if deployment.RollbackReleaseID != "" {
			payload["rollback_release_id"] = deployment.RollbackReleaseID
			payload["rollback_of_deployment_id"] = deployment.RollbackOfDeploymentID
		}
		payload["progress_percent"] = deployment.ProgressPercent
		payload["record_version"] = deployment.RecordVersion
		if deployment.FailureCode != "" {
			payload["error_code"] = deployment.FailureCode
		}
		if deployment.FailureMessage != "" {
			payload["error_message"] = deployment.FailureMessage
		}
	}
	writeSuccess(response, request, http.StatusOK, payload)
}

func otaDeviceReleaseResponse(release domain.DeviceRelease) map[string]any {
	response := map[string]any{
		"schema_version":      "1.0.0",
		"release_id":          release.ReleaseID,
		"firmware_version":    release.Version,
		"channel":             release.Channel,
		"hardware_revision":   release.HardwareRevision,
		"min_source_version":  release.MinSourceVersion,
		"artifact_url":        release.ArtifactURL,
		"artifact_key":        release.ArtifactKey,
		"sha256":              release.SHA256,
		"size_bytes":          release.SizeBytes,
		"signature_key_id":    release.SignatureKeyID,
		"signature_algorithm": release.SignatureAlgorithm,
		"signature":           release.Signature,
		"rollback_allowed":    release.RollbackAllowed,
		"mandatory":           release.Mandatory,
		"release_notes":       release.ReleaseNotes,
		"status":              release.Status,
	}
	if release.PublishedAt != nil {
		response["published_at"] = *release.PublishedAt
	}
	return response
}

func otaReleaseResponse(release domain.Release) map[string]any {
	response := map[string]any{
		"schema_version":      "1.0.0",
		"release_id":          release.ID,
		"id":                  release.ID,
		"firmware_version":    release.Version,
		"channel":             release.Channel,
		"hardware_revision":   release.HardwareRevision,
		"min_source_version":  release.MinSourceVersion,
		"artifact_url":        release.ArtifactURL,
		"artifact_key":        release.ArtifactKey,
		"sha256":              release.SHA256,
		"size_bytes":          release.SizeBytes,
		"signature_key_id":    release.SignatureKeyID,
		"signature_algorithm": release.SignatureAlgorithm,
		"signature":           release.Signature,
		"signature_status":    release.SignatureStatus,
		"rollback_allowed":    release.RollbackAllowed,
		"mandatory":           release.Mandatory,
		"release_notes":       release.ReleaseNotes,
		"status":              release.Status,
		"target_type":         release.Target.Scope,
		"target_id":           release.Target.DeviceID,
		"canary_percent":      release.Target.CanaryPercent,
		"record_version":      release.RecordVersion,
		"created_at":          release.CreatedAt,
		"updated_at":          release.UpdatedAt,
	}
	if release.Target.GroupID != "" {
		response["target_id"] = release.Target.GroupID
	}
	if release.SignatureVerifiedAt != nil {
		response["signature_verified_at"] = *release.SignatureVerifiedAt
	}
	if release.PublishedAt != nil {
		response["published_at"] = *release.PublishedAt
	}
	if release.RollbackReleaseID != "" {
		response["rollback_release_id"] = release.RollbackReleaseID
	}
	return response
}

func otaReleaseResponses(releases []domain.Release) []map[string]any {
	responses := make([]map[string]any, 0, len(releases))
	for _, release := range releases {
		responses = append(responses, otaReleaseResponse(release))
	}
	return responses
}

func (handler otaHandler) writeError(
	response http.ResponseWriter,
	request *http.Request,
	err error,
) {
	switch {
	case errors.Is(err, domain.ErrInvalidRelease),
		errors.Is(err, domain.ErrInvalidTarget),
		errors.Is(err, domain.ErrSignatureInvalid),
		errors.Is(err, domain.ErrSignatureRequired):
		writeError(
			response,
			request,
			http.StatusUnprocessableEntity,
			"invalid_release",
			"请检查更新内容和签名",
		)
	case errors.Is(err, domain.ErrSignatureKeyUnknown):
		writeError(
			response,
			request,
			http.StatusUnprocessableEntity,
			"signature_key_unknown",
			"签名密钥未配置或不可信",
		)
	case errors.Is(err, domain.ErrReleaseNotFound),
		errors.Is(err, domain.ErrDeploymentNotFound),
		errors.Is(err, domain.ErrDeviceNotFound),
		errors.Is(err, domain.ErrGroupNotFound):
		writeError(
			response,
			request,
			http.StatusNotFound,
			"ota_not_found",
			"没有找到对应的更新信息",
		)
	case errors.Is(err, domain.ErrReleaseAlreadyExists),
		errors.Is(err, domain.ErrDeploymentAlreadyExists),
		errors.Is(err, domain.ErrGroupAlreadyExists):
		writeError(
			response,
			request,
			http.StatusConflict,
			"ota_exists",
			"该更新已经存在",
		)
	case errors.Is(err, domain.ErrReleaseVersionConflict),
		errors.Is(err, domain.ErrDeploymentVersionConflict),
		errors.Is(err, domain.ErrGroupVersionConflict):
		writeError(
			response,
			request,
			http.StatusConflict,
			"version_conflict",
			"更新状态已变化，请重新加载后再操作",
		)
	case errors.Is(err, domain.ErrReleaseStateConflict),
		errors.Is(err, domain.ErrDeploymentStateConflict),
		errors.Is(err, domain.ErrEventConflict):
		writeError(
			response,
			request,
			http.StatusConflict,
			"invalid_ota_state",
			"当前更新状态不能执行这个操作",
		)
	case errors.Is(err, domain.ErrInvalidEvent):
		writeError(
			response,
			request,
			http.StatusUnprocessableEntity,
			"invalid_ota_event",
			"设备更新状态需要重新同步",
		)
	case errors.Is(err, domain.ErrNoUpdateAvailable):
		writeError(
			response,
			request,
			http.StatusConflict,
			"no_update_available",
			"当前已经是最新版本",
		)
	case errors.Is(err, domain.ErrRollbackNotAllowed):
		writeError(
			response,
			request,
			http.StatusUnprocessableEntity,
			"rollback_not_allowed",
			"这个版本不支持回滚",
		)
	case errors.Is(err, domain.ErrDeviceNotEligible):
		writeError(
			response,
			request,
			http.StatusUnprocessableEntity,
			"device_not_eligible",
			"这台设备暂不适用该更新",
		)
	default:
		writeError(
			response,
			request,
			http.StatusInternalServerError,
			"service_error",
			"操作没有完成，请稍后重试",
		)
	}
}

func releaseInputFromMutation(
	payload otaReleaseMutation,
	actorID string,
	expectedVersion int64,
) domain.ReleaseInput {
	target := domain.Target{
		Scope:         domain.TargetScope(strings.TrimSpace(payload.TargetType)),
		GroupID:       "",
		DeviceID:      "",
		CanaryPercent: payload.CanaryPercent,
	}
	switch target.Scope {
	case domain.TargetScopeGroup:
		target.GroupID = strings.TrimSpace(payload.TargetID)
	case domain.TargetScopeDevice:
		target.DeviceID = strings.TrimSpace(payload.TargetID)
	case domain.TargetScopeAll, domain.TargetScopeCanary:
	default:
		target.Scope = domain.TargetScopeAll
		target.CanaryPercent = 0
	}
	return domain.ReleaseInput{
		Version:            payload.FirmwareVersion,
		Channel:            domain.Channel(payload.Channel),
		HardwareRevision:   payload.HardwareRevision,
		MinSourceVersion:   payload.MinSourceVersion,
		ArtifactURL:        payload.ArtifactURL,
		ArtifactKey:        payload.ArtifactKey,
		SHA256:             payload.SHA256,
		SizeBytes:          payload.SizeBytes,
		SignatureKeyID:     payload.SignatureKeyID,
		SignatureAlgorithm: payload.SignatureAlgorithm,
		Signature:          payload.Signature,
		RollbackAllowed:    payload.RollbackAllowed,
		Mandatory:          payload.Mandatory,
		ReleaseNotes:       payload.ReleaseNotes,
		Target:             target,
		ActorID:            actorID,
		ExpectedVersion:    expectedVersion,
	}
}

func adminReleaseFilter(request *http.Request) (domain.ReleaseFilter, error) {
	page, pageSize, err := pageParams(request)
	if err != nil {
		return domain.ReleaseFilter{}, err
	}
	return domain.ReleaseFilter{
		Status:   domain.ReleaseStatus(strings.TrimSpace(request.URL.Query().Get("status"))),
		Channel:  domain.Channel(strings.TrimSpace(request.URL.Query().Get("channel"))),
		Version:  strings.TrimSpace(request.URL.Query().Get("version")),
		Page:     page,
		PageSize: pageSize,
	}, nil
}

func pageParams(request *http.Request) (int, int, error) {
	page, err := positiveInt(request.URL.Query().Get("page"), 1)
	if err != nil {
		return 0, 0, err
	}
	pageSize, err := positiveInt(request.URL.Query().Get("page_size"), 20)
	if err != nil {
		return 0, 0, err
	}
	if pageSize > 100 {
		pageSize = 100
	}
	return page, pageSize, nil
}

func positiveInt(value string, fallback int) (int, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return fallback, nil
	}
	parsed, err := strconv.Atoi(value)
	if err != nil || parsed < 1 {
		return 0, errors.New("invalid positive integer")
	}
	return parsed, nil
}
