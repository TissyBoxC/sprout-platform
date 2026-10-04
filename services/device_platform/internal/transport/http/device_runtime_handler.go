package http

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	auditdomain "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/audit/domain"
	bindingdomain "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/device_binding/domain"
	"github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/device_runtime/domain"
	runtimeservice "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/device_runtime/service"
	policydomain "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/parent_policy/domain"
)

type deviceRuntimeHandler struct {
	service           *runtimeservice.Service
	policyService     deviceRuntimePolicyService
	diagnosticService deviceRuntimeDiagnosticService
}

type deviceRuntimeDiagnosticService interface {
	Validate(
		deviceID string,
		diagnostics *auditdomain.Diagnostics,
	) (*auditdomain.Diagnostics, error)
	Record(
		ctx context.Context,
		deviceID string,
		reportedAt time.Time,
		diagnostics *auditdomain.Diagnostics,
	) error
}

type deviceRuntimePolicyService interface {
	GetEffective(
		ctx context.Context,
		familyID string,
	) (*policydomain.EffectivePolicy, error)
}

type recordHeartbeatRequest struct {
	HeartbeatID     string `json:"heartbeat_id"`
	ReportedAt      string `json:"reported_at"`
	FirmwareVersion string `json:"firmware_version"`
	Connection      struct {
		State     string `json:"state"`
		Transport string `json:"transport"`
	} `json:"connection"`
	NetworkQuality struct {
		Level             string `json:"level"`
		RSSIDBM           int    `json:"rssi_dbm"`
		LatencyMS         int    `json:"latency_ms"`
		PacketLossPercent int    `json:"packet_loss_percent"`
	} `json:"network_quality"`
	TimeSync struct {
		State        string  `json:"state"`
		Source       string  `json:"source"`
		LastSyncedAt *string `json:"last_synced_at"`
		OffsetMS     int     `json:"offset_ms"`
	} `json:"time_sync"`
	Offline struct {
		State            string `json:"state"`
		Reason           string `json:"reason"`
		FallbackActive   bool   `json:"fallback_active"`
		PendingTelemetry int    `json:"pending_telemetry"`
	} `json:"offline"`
	Diagnostics *auditdomain.Diagnostics `json:"diagnostics"`
}

type createRuntimeCommandRequest struct {
	CommandType string `json:"command_type"`
}

type acknowledgeRuntimeCommandRequest struct {
	Status     string `json:"status"`
	ResultCode string `json:"result_code"`
}

// recordHeartbeat accepts an authenticated device report. The path device id
// must match the session owner, so one device cannot spoof another's state.
func (handler deviceRuntimeHandler) recordHeartbeat(
	response http.ResponseWriter,
	request *http.Request,
) {
	deviceSessionToken, ok := bearerToken(request)
	if !ok {
		writeError(response, request, http.StatusUnauthorized, "device_session_expired", "设备登录已过期，请重新连接")
		return
	}
	var payload recordHeartbeatRequest
	if err := decodeJSON(request, &payload); err != nil {
		writeError(response, request, http.StatusBadRequest, "invalid_request", "设备状态格式不正确")
		return
	}
	reportedAt, err := time.Parse(time.RFC3339, strings.TrimSpace(payload.ReportedAt))
	if err != nil {
		writeError(response, request, http.StatusUnprocessableEntity, "invalid_reported_at", "设备时间需要重新校准")
		return
	}
	var timeSyncedAt *time.Time
	if payload.TimeSync.LastSyncedAt != nil {
		parsed, parseErr := time.Parse(time.RFC3339, strings.TrimSpace(*payload.TimeSync.LastSyncedAt))
		if parseErr != nil {
			writeError(response, request, http.StatusUnprocessableEntity, "invalid_time_sync", "设备时间需要重新校准")
			return
		}
		timeSyncedAt = &parsed
	}
	deviceID := strings.TrimSpace(request.PathValue("device_id"))
	var normalizedDiagnostics *auditdomain.Diagnostics
	if payload.Diagnostics != nil {
		if handler.diagnosticService == nil {
			writeError(response, request, http.StatusServiceUnavailable, "service_unavailable", "设备诊断暂时无法接收")
			return
		}
		var validationErr error
		normalizedDiagnostics, validationErr = handler.diagnosticService.Validate(
			deviceID,
			payload.Diagnostics,
		)
		if validationErr != nil {
			writeError(response, request, http.StatusUnprocessableEntity, "invalid_device_diagnostics", "设备诊断需要重新同步")
			return
		}
	}
	status, err := handler.service.RecordHeartbeat(
		request.Context(),
		deviceSessionToken,
		domain.HeartbeatInput{
			DeviceID:          deviceID,
			HeartbeatID:       payload.HeartbeatID,
			ReportedAt:        reportedAt,
			FirmwareVersion:   payload.FirmwareVersion,
			ConnectionState:   domain.ConnectionState(payload.Connection.State),
			Transport:         domain.Transport(payload.Connection.Transport),
			NetworkQuality:    domain.NetworkQualityLevel(payload.NetworkQuality.Level),
			RSSIDBM:           payload.NetworkQuality.RSSIDBM,
			LatencyMS:         payload.NetworkQuality.LatencyMS,
			PacketLossPercent: payload.NetworkQuality.PacketLossPercent,
			TimeSyncState:     domain.TimeSyncState(payload.TimeSync.State),
			TimeSyncSource:    domain.TimeSyncSource(payload.TimeSync.Source),
			TimeSyncedAt:      timeSyncedAt,
			TimeOffsetMS:      payload.TimeSync.OffsetMS,
			OfflineState:      domain.OfflineState(payload.Offline.State),
			OfflineReason:     domain.OfflineReason(payload.Offline.Reason),
			FallbackActive:    payload.Offline.FallbackActive,
			PendingTelemetry:  payload.Offline.PendingTelemetry,
		},
	)
	if err != nil {
		writeDeviceRuntimeError(response, request, err)
		return
	}
	if normalizedDiagnostics != nil {
		if recordErr := handler.diagnosticService.Record(
			request.Context(),
			status.DeviceID,
			reportedAt,
			normalizedDiagnostics,
		); recordErr != nil {
			writeError(response, request, http.StatusInternalServerError, "service_error", "设备诊断暂时无法保存，请稍后重试")
			return
		}
	}
	writeSuccess(response, request, http.StatusOK, runtimeStatusResponse(status))
}

func (handler deviceRuntimeHandler) listForParent(
	response http.ResponseWriter,
	request *http.Request,
) {
	accountID, ok := authenticatedAccountID(request)
	if !ok {
		writeError(response, request, http.StatusUnauthorized, "unauthenticated", "请重新登录")
		return
	}
	statuses, err := handler.service.ListDeviceStatuses(request.Context(), accountID)
	if err != nil {
		writeDeviceRuntimeError(response, request, err)
		return
	}
	result := make([]map[string]any, 0, len(statuses))
	for index := range statuses {
		result = append(result, deviceStatusResponse(&statuses[index]))
	}
	writeSuccess(response, request, http.StatusOK, map[string]any{"devices": result})
}

func (handler deviceRuntimeHandler) getForParent(
	response http.ResponseWriter,
	request *http.Request,
) {
	accountID, ok := authenticatedAccountID(request)
	if !ok {
		writeError(response, request, http.StatusUnauthorized, "unauthenticated", "请重新登录")
		return
	}
	status, err := handler.service.GetDeviceStatus(
		request.Context(),
		accountID,
		strings.TrimSpace(request.PathValue("device_id")),
	)
	if err != nil {
		writeDeviceRuntimeError(response, request, err)
		return
	}
	writeSuccess(response, request, http.StatusOK, deviceStatusResponse(status))
}

func (handler deviceRuntimeHandler) listForAdmin(
	response http.ResponseWriter,
	request *http.Request,
) {
	statuses, err := handler.service.ListAllDeviceStatuses(request.Context())
	if err != nil {
		writeDeviceRuntimeError(response, request, err)
		return
	}
	result := make([]map[string]any, 0, len(statuses))
	for index := range statuses {
		result = append(result, deviceStatusResponse(&statuses[index]))
	}
	writeSuccess(response, request, http.StatusOK, map[string]any{"devices": result})
}

func (handler deviceRuntimeHandler) createCommand(
	response http.ResponseWriter,
	request *http.Request,
) {
	accountID, _ := authenticatedAccountID(request)
	var payload createRuntimeCommandRequest
	if err := decodeJSON(request, &payload); err != nil {
		writeError(response, request, http.StatusBadRequest, "invalid_request", "请选择要执行的操作")
		return
	}
	command, err := handler.service.CreateCommand(
		request.Context(),
		strings.TrimSpace(request.PathValue("device_id")),
		domain.CommandType(strings.TrimSpace(payload.CommandType)),
		accountID,
		map[string]any{},
	)
	if err != nil {
		writeDeviceRuntimeError(response, request, err)
		return
	}
	writeSuccess(response, request, http.StatusCreated, commandResponse(command))
}

func (handler deviceRuntimeHandler) listCommands(
	response http.ResponseWriter,
	request *http.Request,
) {
	commands, err := handler.service.ListCommands(
		request.Context(),
		strings.TrimSpace(request.PathValue("device_id")),
	)
	if err != nil {
		writeDeviceRuntimeError(response, request, err)
		return
	}
	result := make([]map[string]any, 0, len(commands))
	for index := range commands {
		result = append(result, commandResponse(&commands[index]))
	}
	writeSuccess(response, request, http.StatusOK, map[string]any{"commands": result})
}

func (handler deviceRuntimeHandler) listDeviceCommands(
	response http.ResponseWriter,
	request *http.Request,
) {
	deviceSessionToken, ok := bearerToken(request)
	if !ok {
		writeError(response, request, http.StatusUnauthorized, "device_session_expired", "设备登录已过期，请重新连接")
		return
	}
	commands, err := handler.service.ListPendingCommands(
		request.Context(),
		deviceSessionToken,
	)
	if err != nil {
		writeDeviceRuntimeError(response, request, err)
		return
	}
	result := make([]map[string]any, 0, len(commands))
	for index := range commands {
		result = append(result, commandResponse(&commands[index]))
	}
	writeSuccess(response, request, http.StatusOK, map[string]any{"commands": result})
}

func (handler deviceRuntimeHandler) acknowledgeCommand(
	response http.ResponseWriter,
	request *http.Request,
) {
	deviceSessionToken, ok := bearerToken(request)
	if !ok {
		writeError(response, request, http.StatusUnauthorized, "device_session_expired", "设备登录已过期，请重新连接")
		return
	}
	var payload acknowledgeRuntimeCommandRequest
	if err := decodeJSON(request, &payload); err != nil {
		writeError(response, request, http.StatusBadRequest, "invalid_request", "设备操作结果格式不正确")
		return
	}
	if err := handler.service.AcknowledgeCommand(
		request.Context(),
		deviceSessionToken,
		strings.TrimSpace(request.PathValue("device_id")),
		strings.TrimSpace(request.PathValue("command_id")),
		domain.CommandStatus(strings.TrimSpace(payload.Status)),
		payload.ResultCode,
	); err != nil {
		writeDeviceRuntimeError(response, request, err)
		return
	}
	writeSuccess(response, request, http.StatusOK, map[string]any{"acknowledged": true})
}

// getParentPolicy returns the current guardian policy for the authenticated
// device's family. The path device id is checked against the session owner so
// one device cannot read another family's policy by changing the path.
func (handler deviceRuntimeHandler) getParentPolicy(
	response http.ResponseWriter,
	request *http.Request,
) {
	if handler.policyService == nil {
		writeError(response, request, http.StatusServiceUnavailable, "service_unavailable", "家长策略暂时无法读取")
		return
	}
	deviceSessionToken, ok := bearerToken(request)
	if !ok {
		writeError(response, request, http.StatusUnauthorized, "device_session_expired", "设备登录已过期，请重新连接")
		return
	}
	deviceID, err := handler.service.ResolveDeviceFamily(
		request.Context(),
		deviceSessionToken,
		strings.TrimSpace(request.PathValue("device_id")),
	)
	if err != nil {
		writeDeviceRuntimeError(response, request, err)
		return
	}
	effective, err := handler.policyService.GetEffective(
		request.Context(),
		deviceID,
	)
	if err != nil {
		writeDeviceRuntimeError(response, request, err)
		return
	}
	writeSuccess(response, request, http.StatusOK, effectivePolicyResponse(effective))
}

func effectivePolicyResponse(policy *policydomain.EffectivePolicy) map[string]any {
	if policy == nil {
		return nil
	}
	disabledPeriods := make([]map[string]string, 0, len(policy.DisabledPeriods))
	for _, period := range policy.DisabledPeriods {
		disabledPeriods = append(disabledPeriods, map[string]string{
			"start_time": period.StartTime,
			"end_time":   period.EndTime,
		})
	}
	return map[string]any{
		"schema_version":      "1.0.0",
		"policy_version":      policy.PolicyVersion,
		"daily_limit_minutes": policy.DailyLimitMinutes,
		"allowed_categories":  policy.AllowedCategories,
		"disabled_periods":    disabledPeriods,
		"max_volume_percent":  policy.MaxVolumePercent,
		"source_child_count":  policy.SourceChildCount,
		"aggregation_mode":    "most_restrictive",
		"updated_at":          policy.UpdatedAt.UTC(),
	}
}

func runtimeStatusResponse(status *domain.RuntimeStatus) map[string]any {
	if status == nil {
		return nil
	}
	return map[string]any{
		"device_id":        status.DeviceID,
		"heartbeat_id":     status.HeartbeatID,
		"reported_at":      status.ReportedAt,
		"received_at":      status.ReceivedAt,
		"firmware_version": status.FirmwareVersion,
		"is_online":        status.IsOnline,
		"connection": map[string]any{
			"state":     status.ConnectionState,
			"transport": status.Transport,
		},
		"network_quality": map[string]any{
			"level":               status.NetworkQuality,
			"rssi_dbm":            status.RSSIDBM,
			"latency_ms":          status.LatencyMS,
			"packet_loss_percent": status.PacketLossPercent,
		},
		"time_sync": map[string]any{
			"state":          status.TimeSyncState,
			"source":         status.TimeSyncSource,
			"last_synced_at": status.TimeSyncedAt,
			"offset_ms":      status.TimeOffsetMS,
		},
		"offline": map[string]any{
			"state":             status.OfflineState,
			"reason":            status.OfflineReason,
			"fallback_active":   status.FallbackActive,
			"pending_telemetry": status.PendingTelemetry,
		},
	}
}

func deviceStatusResponse(status *domain.DeviceStatus) map[string]any {
	if status == nil {
		return nil
	}
	return map[string]any{
		"parent_account_id": status.ParentAccountID,
		"device_id":        status.DeviceID,
		"device_name":      status.DeviceName,
		"hardware_model":   status.HardwareModel,
		"firmware_version": status.FirmwareVersion,
		"capabilities":     status.Capabilities,
		"lifecycle_status": status.LifecycleStatus,
		"bound_at":         status.BoundAt,
		"updated_at":       status.UpdatedAt,
		"runtime":          runtimeStatusResponse(status.Runtime),
	}
}

func commandResponse(command *domain.Command) map[string]any {
	if command == nil {
		return nil
	}
	return map[string]any{
		"command_id":      command.ID,
		"device_id":       command.DeviceID,
		"command_type":    command.Type,
		"status":          command.Status,
		"request_id":      command.RequestID,
		"created_at":      command.CreatedAt,
		"delivered_at":    command.DeliveredAt,
		"acknowledged_at": command.AcknowledgedAt,
		"result_code":     command.ResultCode,
	}
}

func writeDeviceRuntimeError(
	response http.ResponseWriter,
	request *http.Request,
	err error,
) {
	switch {
	case errors.Is(err, domain.ErrInvalidHeartbeat):
		writeError(response, request, http.StatusUnprocessableEntity, "invalid_device_status", "设备状态需要重新同步")
	case errors.Is(err, domain.ErrInvalidCommand):
		writeError(response, request, http.StatusUnprocessableEntity, "invalid_device_command", "请选择要执行的操作")
	case errors.Is(err, domain.ErrDeviceNotFound),
		errors.Is(err, bindingdomain.ErrDeviceNotFound):
		writeError(response, request, http.StatusNotFound, "device_not_found", "没有找到这台设备")
	case errors.Is(err, domain.ErrCommandNotFound):
		writeError(response, request, http.StatusConflict, "command_already_handled", "这个操作已经处理过")
	case errors.Is(err, policydomain.ErrPolicyNotFound):
		writeError(response, request, http.StatusNotFound, "parent_policy_not_found", "家长策略不存在")
	case errors.Is(err, policydomain.ErrInvalidCategories):
		writeError(response, request, http.StatusConflict, "parent_policy_conflict", "家长策略冲突，请检查儿童内容设置")
	case errors.Is(err, bindingdomain.ErrDeviceSessionExpired),
		errors.Is(err, bindingdomain.ErrDeviceSessionNotFound):
		writeError(response, request, http.StatusUnauthorized, "device_session_expired", "设备登录已过期，请重新连接")
	case errors.Is(err, bindingdomain.ErrDeviceDisabled):
		writeError(response, request, http.StatusForbidden, "device_disabled", "这台设备当前无法连接")
	case errors.Is(err, bindingdomain.ErrInvalidDeviceProof),
		errors.Is(err, bindingdomain.ErrInvalidDeviceID):
		writeError(response, request, http.StatusUnauthorized, "device_auth_failed", "设备验证没有通过")
	default:
		writeError(response, request, http.StatusInternalServerError, "service_error", "操作没有完成，请稍后重试")
	}
}
