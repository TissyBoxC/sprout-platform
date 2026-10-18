package http

import (
	"context"
	"errors"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	gatewaydomain "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/ai_gateway/domain"
	aiexecution "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/ai_gateway/execution"
	bindingdomain "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/device_binding/domain"
	bindingservice "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/device_binding/service"
	policydomain "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/parent_policy/domain"
	releaseStoredomain "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/release_store/domain"
)

var (
	aiExecutionProjectPattern = regexp.MustCompile(`^[a-z][a-z0-9_]{0,63}$`)
	aiExecutionSessionPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$`)
)

// internalHandler exposes service-to-service endpoints for the AI relay.
// Every route must be mounted behind the internal service token.
type internalHandler struct {
	bindingService       *bindingservice.Service
	aiService            aiExecutionAuthorizer
	executionStore       aiExecutionStore
	executionTokenTTL    time.Duration
	executionLeaseTTL    time.Duration
	aiUpstreamURL        string
	aiHTTPClient         *http.Client
	requireHTTPSUpstream bool
	policyService        internalPolicyService
	releaseStoreService  releaseStoreAdminService
}

// aiExecutionStore issues and consumes one-time relay tickets. Redis is the
// production implementation so duplicate execution is rejected across replicas.
type aiExecutionStore interface {
	Issue(
		ctx context.Context,
		ticket aiexecution.Ticket,
		ttl time.Duration,
	) (string, error)
	Consume(ctx context.Context, token string) (aiexecution.Ticket, error)
	AcquireDeviceLease(
		ctx context.Context,
		deviceID string,
		limit int,
		ttl time.Duration,
	) (string, func(), error)
}

// internalPolicyService resolves the effective parent policy for the device
// and relay path. It is separate from the guardian-facing edit surface.
type internalPolicyService interface {
	GetEffective(
		ctx context.Context,
		familyID string,
	) (*policydomain.EffectivePolicy, error)
}

// uploadReleaseFile lets the release pipeline publish one artifact through
// the same store implementation used by the console. The caller must already
// be authenticated by the internal service token.
func (handler internalHandler) uploadReleaseFile(
	response http.ResponseWriter,
	request *http.Request,
) {
	setLargeUploadDeadline(response)
	request.Body = http.MaxBytesReader(
		response,
		request.Body,
		releaseStoredomain.MaxUploadBytes+1<<20,
	)
	if err := request.ParseMultipartForm(32 << 20); err != nil {
		var maxBytesError *http.MaxBytesError
		if errors.As(err, &maxBytesError) {
			writeError(response, request, http.StatusRequestEntityTooLarge, "file_too_large", "上传文件过大")
			return
		}
		writeError(response, request, http.StatusBadRequest, "invalid_upload", "请重新提交发布文件")
		return
	}
	relativePath := strings.TrimSpace(request.FormValue("relative_path"))
	if relativePath == "" {
		relativePath = strings.TrimSpace(request.FormValue("path"))
	}
	overwrite, _ := strconv.ParseBool(strings.TrimSpace(request.FormValue("overwrite")))
	file, _, err := request.FormFile("file")
	if err != nil {
		writeError(response, request, http.StatusBadRequest, "missing_file", "请提交发布文件")
		return
	}
	defer file.Close()
	uploaded, err := handler.releaseStoreService.UploadFile(
		request.Context(),
		releaseStoredomain.UploadInput{
			RelativePath: relativePath,
			Overwrite:    overwrite,
			Version:      strings.TrimSpace(request.FormValue("version")),
			Channel:      strings.TrimSpace(request.FormValue("channel")),
			Platform:     strings.TrimSpace(request.FormValue("platform")),
			Kind:         strings.TrimSpace(request.FormValue("kind")),
			FileName:     strings.TrimSpace(request.FormValue("filename")),
		},
		file,
	)
	if err != nil {
		writeReleaseStoreError(response, request, err)
		return
	}
	writeSuccess(response, request, http.StatusCreated, map[string]any{"file": uploaded})
}

// refreshReleaseIndex regenerates the public manifest and global index for
// one release. The endpoint is intentionally non-destructive when the version
// directory is absent, matching the console's refresh contract.
func (handler internalHandler) refreshReleaseIndex(
	response http.ResponseWriter,
	request *http.Request,
) {
	var payload refreshReleaseIndexRequest
	if err := decodeOptionalJSON(request, &payload); err != nil {
		writeError(response, request, http.StatusBadRequest, "invalid_request", "请检查版本信息")
		return
	}
	if strings.TrimSpace(payload.Version) == "" {
		writeError(response, request, http.StatusBadRequest, "invalid_request", "请指定要刷新的发布版本")
		return
	}
	result, err := handler.releaseStoreService.RefreshIndex(
		request.Context(),
		releaseStoredomain.IndexRefreshRequest{
			Version:     payload.Version,
			Channel:     payload.Channel,
			Title:       payload.Title,
			PublishedAt: payload.PublishedAt,
		},
	)
	if err != nil {
		writeReleaseStoreError(response, request, err)
		return
	}
	writeSuccess(response, request, http.StatusOK, map[string]any{"release": result})
}

// aiExecutionTicket authorizes one bound-device model execution without
// returning the provider credential. The relay presents the one-time ticket
// to the platform-owned execution proxy, which alone decrypts the key.
func (handler internalHandler) aiExecutionTicket(
	response http.ResponseWriter,
	request *http.Request,
) {
	deviceID := strings.TrimSpace(request.PathValue("device_id"))
	binding, err := handler.bindingService.GetByDeviceID(request.Context(), deviceID)
	if err != nil {
		writeInternalExecutionError(response, request, err)
		return
	}
	model := strings.TrimSpace(request.URL.Query().Get("model"))
	if model == "" {
		writeError(response, request, http.StatusBadRequest, "invalid_model", "请选择一个可用模型")
		return
	}
	project := strings.TrimSpace(request.URL.Query().Get("project"))
	if !aiExecutionProjectPattern.MatchString(project) {
		writeError(response, request, http.StatusBadRequest, "invalid_project", "无法识别这次对话的使用场景")
		return
	}
	sessionID := strings.TrimSpace(request.URL.Query().Get("session_id"))
	if !aiExecutionSessionPattern.MatchString(sessionID) {
		writeError(response, request, http.StatusBadRequest, "invalid_session", "无法识别这次对话")
		return
	}
	if handler.aiService == nil {
		writeError(response, request, http.StatusServiceUnavailable, "execution_unavailable", "AI 服务暂时不可用，请稍后重试")
		return
	}
	credential, err := handler.aiService.AuthorizeExecution(
		request.Context(),
		binding.ParentAccountID,
		model,
	)
	if err != nil {
		writeInternalExecutionError(response, request, err)
		return
	}
	if handler.executionStore == nil {
		writeError(response, request, http.StatusServiceUnavailable, "execution_unavailable", "AI 服务暂时不可用，请稍后重试")
		return
	}
	if handler.executionTokenTTL <= 0 {
		writeError(response, request, http.StatusServiceUnavailable, "execution_unavailable", "AI 服务暂时不可用，请稍后重试")
		return
	}
	if credential.ConcurrencyLimit < 1 {
		writeError(response, request, http.StatusServiceUnavailable, "execution_unavailable", "AI 服务暂时不可用，请稍后重试")
		return
	}
	issued, err := handler.executionStore.Issue(
		request.Context(),
		aiexecution.Ticket{
			DeviceID:        binding.DeviceID,
			ParentAccountID: binding.ParentAccountID,
			SessionID:       sessionID,
			Project:         project,
			Model:           model,
			Concurrency:     credential.ConcurrencyLimit,
		},
		handler.executionTokenTTL,
	)
	if err != nil {
		writeError(response, request, http.StatusServiceUnavailable, "execution_unavailable", "AI 服务暂时不可用，请稍后重试")
		return
	}
	writeSuccess(response, request, http.StatusOK, map[string]any{
		"ticket":     issued,
		"expires_in": int(handler.executionTokenTTL.Seconds()),
		"model":      model,
	})
}

// effectivePolicies returns the single effective policy a family-bound
// device or relay must execute. It never exposes child or guardian identities.
func (handler internalHandler) effectivePolicies(
	response http.ResponseWriter,
	request *http.Request,
) {
	if handler.policyService == nil {
		writeError(response, request, http.StatusServiceUnavailable, "service_unavailable", "家长策略暂时无法读取")
		return
	}
	familyID := strings.TrimSpace(request.URL.Query().Get("family_id"))
	if familyID == "" {
		writeError(response, request, http.StatusBadRequest, "invalid_request", "请指定家长账号")
		return
	}
	effective, err := handler.policyService.GetEffective(
		request.Context(),
		familyID,
	)
	if err != nil {
		writeDeviceRuntimeError(response, request, err)
		return
	}
	writeSuccess(response, request, http.StatusOK, map[string]any{
		"policy": effectivePolicyResponse(effective),
	})
}

func writeInternalExecutionError(
	response http.ResponseWriter,
	request *http.Request,
	err error,
) {
	switch {
	case errors.Is(err, bindingdomain.ErrInvalidDeviceID),
		errors.Is(err, bindingdomain.ErrDeviceNotFound):
		writeError(response, request, http.StatusNotFound, "device_not_found", "没有找到这台设备")
	case errors.Is(err, gatewaydomain.ErrAccountNotFound),
		errors.Is(err, gatewaydomain.ErrCredentialInvalid):
		writeError(response, request, http.StatusNotFound, "credential_not_found", "这台设备还没有可用的 AI 服务")
	case errors.Is(err, gatewaydomain.ErrModelNotAllowed):
		writeError(response, request, http.StatusForbidden, "model_not_allowed", "这个模型当前不可用")
	case errors.Is(err, gatewaydomain.ErrProviderUnavailable):
		writeError(response, request, http.StatusBadGateway, "ai_service_unavailable", "AI 服务暂时不可用，请稍后重试")
	default:
		writeError(response, request, http.StatusInternalServerError, "service_error", "操作没有完成，请稍后重试")
	}
}
