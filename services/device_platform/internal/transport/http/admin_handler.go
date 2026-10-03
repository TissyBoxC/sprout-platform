package http

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	gatewaydomain "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/ai_gateway/domain"
	gatewayservice "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/ai_gateway/service"
	authdomain "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/auth/domain"
	authservice "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/auth/service"
	childdomain "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/child/domain"
	deviceruntimedomain "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/device_runtime/domain"
	operationsdomain "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/operations/domain"
	policydomain "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/parent_policy/domain"
	releaseStoredomain "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/release_store/domain"
	serviceversiondomain "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/service_version/domain"
	serviceversionservice "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/service_version/service"
)

type adminHandler struct {
	service               *gatewayservice.Service
	parentService         parentAccountService
	operationsService     operationsAdminService
	serviceVersionService serviceVersionAdminService
	releaseStoreService   releaseStoreAdminService
	deviceStatusService   adminDeviceStatusService
	childService          adminChildService
	policyService         adminPolicyService
}

type parentAccountService interface {
	CreateParent(
		ctx context.Context,
		input authdomain.RegisterInput,
	) (*authdomain.ParentAccount, *authdomain.AIAccountSummary, error)
	GetIdentity(
		ctx context.Context,
		accountID string,
	) (*authdomain.ParentAccount, error)
	UpdateParentProfile(
		ctx context.Context,
		accountID string,
		update authservice.ProfileUpdate,
	) (*authdomain.ParentAccount, error)
	ResetParentPassword(
		ctx context.Context,
		accountID string,
		password string,
	) error
}

// operationsAdminService is the management-facing operations surface needed by
// the HTTP layer. Keeping it narrow avoids coupling transport to persistence.
type operationsAdminService interface {
	Settings(ctx context.Context) (*operationsdomain.Settings, int64, error)
	UpdateSettings(
		ctx context.Context,
		settings *operationsdomain.Settings,
		actorAccountID string,
	) (*operationsdomain.Settings, int64, error)
	Overview(ctx context.Context) (*operationsdomain.Overview, error)
	ListFamilyAccounts(ctx context.Context) ([]operationsdomain.FamilyAccount, error)
	ListReleases(ctx context.Context) ([]operationsdomain.Release, error)
	CreateRelease(
		ctx context.Context,
		input operationsdomain.ReleaseInput,
	) (*operationsdomain.Release, error)
	PublishRelease(
		ctx context.Context,
		version string,
		actorAccountID string,
	) error
	DeleteRelease(ctx context.Context, version string) error
	FindReleaseArtifact(
		ctx context.Context,
		version string,
		kind string,
		platform string,
	) (*operationsdomain.ReleaseArtifact, error)
	AppUpdate(
		ctx context.Context,
		platform string,
		channel string,
		currentVersion string,
	) (*operationsdomain.AppUpdate, error)
}

// serviceVersionAdminService is the management-facing surface for the brand
// service inventory and its container upgrades.
type serviceVersionAdminService interface {
	Snapshot(
		ctx context.Context,
	) ([]serviceversiondomain.Service, time.Time, error)
	AllCurrent(services []serviceversiondomain.Service) bool
	RequestCheck(ctx context.Context) error
	PlanUpgrade(
		ctx context.Context,
		serviceID string,
	) (serviceversionservice.UpgradePlan, error)
	PlanUpgradeTo(
		ctx context.Context,
		serviceID string,
		targetVersion string,
	) (serviceversionservice.UpgradePlan, error)
	Confirmation(
		ctx context.Context,
		plan serviceversionservice.UpgradePlan,
	) (serviceversionservice.UpgradeConfirmation, error)
	ListReleases(
		ctx context.Context,
		serviceID string,
		page int,
		pageSize int,
	) (serviceversiondomain.ReleasePage, error)
	PlanUpgradeAll(
		ctx context.Context,
	) ([]serviceversionservice.UpgradePlan, error)
	Enqueue(
		ctx context.Context,
		plan serviceversionservice.UpgradePlan,
		actorAccountID string,
	) (*serviceversiondomain.Operation, error)
	EnqueueAll(
		ctx context.Context,
		plans []serviceversionservice.UpgradePlan,
		actorAccountID string,
	) ([]serviceversiondomain.Operation, error)
	ListOperations(ctx context.Context) ([]serviceversiondomain.Operation, error)
	FindOperation(
		ctx context.Context,
		operationID string,
	) (*serviceversiondomain.Operation, error)
}

type updateAIAccountRequest struct {
	Status           string   `json:"status"`
	BalanceUSD       float64  `json:"balance_usd"`
	ConcurrencyLimit int      `json:"concurrency_limit"`
	AvailableModels  []string `json:"available_models"`
	Reason           string   `json:"reason"`
}

// releaseStoreAdminService is the management-facing download-store surface.
// It is defined here so transport tests can substitute a small fake.
type releaseStoreAdminService interface {
	Inventory(ctx context.Context) (releaseStoredomain.Inventory, error)
	IndexStatus(ctx context.Context) (releaseStoredomain.IndexStatus, error)
	RefreshAllIndexes(
		ctx context.Context,
	) (releaseStoredomain.IndexRefreshSummary, error)
	UploadFile(
		ctx context.Context,
		input releaseStoredomain.UploadInput,
		reader io.Reader,
	) (releaseStoredomain.File, error)
	DeleteFile(ctx context.Context, relativePath string) error
	RefreshIndex(
		ctx context.Context,
		input releaseStoredomain.IndexRefreshRequest,
	) (releaseStoredomain.IndexRefreshResult, error)
}

type updateAIModelsRequest struct {
	SelectedModels []string `json:"selected_models"`
}

type updateParentProfileRequest struct {
	DisplayName        string `json:"display_name"`
	GuardianFamilyName string `json:"guardian_family_name"`
	ChildNickname      string `json:"child_nickname"`
	ChildBirthday      string `json:"child_birthday"`
}

type resetParentPasswordRequest struct {
	Password string `json:"password"`
}

// adminDeviceStatusService is the narrow device surface the management console
// needs. It is scoped to one parent so an operator can never read another
// family's devices by guessing a device id.
type adminDeviceStatusService interface {
	ListDeviceStatuses(
		ctx context.Context,
		parentAccountID string,
	) ([]deviceruntimedomain.DeviceStatus, error)
}

// adminChildService is the management-facing child profile read surface.
type adminChildService interface {
	List(ctx context.Context, familyID string) ([]childdomain.Child, error)
	Get(ctx context.Context, familyID string, childID string) (*childdomain.Child, error)
}

// adminPolicyService is the management-facing parent policy read surface.
type adminPolicyService interface {
	Get(ctx context.Context, familyID string, childID string) (*policydomain.Policy, error)
}

type aiAccountDefaultsResponse struct {
	DefaultBalanceUSD  float64 `json:"default_balance_usd"`
	DefaultConcurrency int     `json:"default_concurrency"`
	Source             string  `json:"source"`
}

type aiModelResponse struct {
	ID        string `json:"id"`
	LatencyMS *int   `json:"latency_ms"`
}

type createParentRequest struct {
	Phone              string `json:"phone"`
	Password           string `json:"password"`
	GuardianFamilyName string `json:"guardian_family_name"`
	ChildNickname      string `json:"child_nickname"`
	ChildBirthday      string `json:"child_birthday"`
}

func (handler adminHandler) createParent(
	response http.ResponseWriter,
	request *http.Request,
) {
	var payload createParentRequest
	if err := decodeJSON(request, &payload); err != nil {
		writeError(response, request, http.StatusBadRequest, "invalid_request", "请检查填写的内容")
		return
	}
	account, summary, err := handler.service.CreateParentWithAIAccount(
		request.Context(),
		handler.parentService,
		authdomain.RegisterInput{
			Phone:                  payload.Phone,
			Password:               payload.Password,
			GuardianFamilyName:     payload.GuardianFamilyName,
			ChildNickname:          payload.ChildNickname,
			ChildBirthday:          payload.ChildBirthday,
			GuardianConsentVersion: "2026-01",
		},
	)
	if err != nil {
		writeAuthServiceError(response, request, err)
		return
	}
	writeSuccess(response, request, http.StatusCreated, accountResponse(account, summary))
}

// retryParentAIAccount repairs the dependent AI account for an existing
// guardian. It is safe to call repeatedly and never creates another parent.
func (handler adminHandler) retryParentAIAccount(
	response http.ResponseWriter,
	request *http.Request,
) {
	parentAccountID := strings.TrimSpace(
		request.PathValue("parent_account_id"),
	)
	if parentAccountID == "" {
		writeError(response, request, http.StatusBadRequest, "invalid_request", "请检查要开通的家长账号")
		return
	}
	account, err := handler.parentService.GetIdentity(
		request.Context(),
		parentAccountID,
	)
	if err != nil {
		if errors.Is(err, authdomain.ErrAccountNotFound) {
			writeError(response, request, http.StatusNotFound, "account_not_found", "没有找到这个家长账号")
			return
		}
		writeError(response, request, http.StatusInternalServerError, "service_error", "暂时无法读取家长账号")
		return
	}
	if account.Role != authdomain.RoleParent {
		writeError(response, request, http.StatusUnprocessableEntity, "invalid_account", "这个账号不是家长账号")
		return
	}
	summary, err := handler.service.RepairParentAIAccount(
		request.Context(),
		account.ID,
		account.Email,
	)
	if err != nil {
		if errors.Is(err, gatewaydomain.ErrProviderUnavailable) ||
			errors.Is(err, gatewaydomain.ErrProviderRejected) {
			writeError(response, request, http.StatusBadGateway, "provider_error", "AI 服务暂时不可用，请稍后重试")
			return
		}
		writeError(response, request, http.StatusInternalServerError, "service_error", "家长 AI 账号没有开通，请稍后重试")
		return
	}
	writeSuccess(response, request, http.StatusOK, map[string]any{
		"ai_account": map[string]any{
			"status":            summary.Status,
			"balance_usd":       summary.BalanceUSD,
			"concurrency_limit": summary.ConcurrencyLimit,
			"available_models":  summary.AvailableModels,
			"selected_models":   summary.SelectedModels,
			"allowed_models":    summary.AllowedModels,
			"provider_ready":    summary.ProviderReady,
		},
	})
}

// updateParentProfile lets an operator correct guardian-editable profile
// fields for one family. Identity fields (phone, email, role) stay immutable
// here because changing them would move the account between families.
func (handler adminHandler) updateParentProfile(
	response http.ResponseWriter,
	request *http.Request,
) {
	parentAccountID := strings.TrimSpace(
		request.PathValue("parent_account_id"),
	)
	if parentAccountID == "" {
		writeError(response, request, http.StatusBadRequest, "invalid_request", "请检查要修改的家长账号")
		return
	}
	var payload updateParentProfileRequest
	if err := decodeJSON(request, &payload); err != nil {
		writeError(response, request, http.StatusBadRequest, "invalid_request", "请检查填写的内容")
		return
	}
	account, err := handler.parentService.UpdateParentProfile(
		request.Context(),
		parentAccountID,
		authservice.ProfileUpdate{
			DisplayName:        payload.DisplayName,
			GuardianFamilyName: payload.GuardianFamilyName,
			ChildNickname:      payload.ChildNickname,
			ChildBirthday:      payload.ChildBirthday,
		},
	)
	if err != nil {
		writeAuthServiceError(response, request, err)
		return
	}
	writeSuccess(response, request, http.StatusOK, map[string]any{
		"account": map[string]any{
			"parent_account_id":    account.ID,
			"display_name":         account.DisplayName,
			"guardian_family_name": account.GuardianFamilyName,
			"child_nickname":       account.ChildNickname,
			"child_birthday":       account.ChildBirthday,
		},
	})
}

// resetParentPassword replaces one family's password. The repository revokes
// every active session as part of the same operation, so a stolen refresh
// token cannot survive an operator reset.
func (handler adminHandler) resetParentPassword(
	response http.ResponseWriter,
	request *http.Request,
) {
	parentAccountID := strings.TrimSpace(
		request.PathValue("parent_account_id"),
	)
	if parentAccountID == "" {
		writeError(response, request, http.StatusBadRequest, "invalid_request", "请检查要重置的家长账号")
		return
	}
	var payload resetParentPasswordRequest
	if err := decodeJSON(request, &payload); err != nil {
		writeError(response, request, http.StatusBadRequest, "invalid_request", "请检查填写的内容")
		return
	}
	if err := handler.parentService.ResetParentPassword(
		request.Context(),
		parentAccountID,
		payload.Password,
	); err != nil {
		writeAuthServiceError(response, request, err)
		return
	}
	writeSuccess(response, request, http.StatusOK, map[string]any{
		"password_reset": true,
	})
}

// listParentDevices returns the bound devices of one family together with
// their most recent runtime state so an operator can see who is online.
func (handler adminHandler) listParentDevices(
	response http.ResponseWriter,
	request *http.Request,
) {
	parentAccountID := strings.TrimSpace(
		request.PathValue("parent_account_id"),
	)
	if parentAccountID == "" {
		writeError(response, request, http.StatusBadRequest, "invalid_request", "请检查要查看的家长账号")
		return
	}
	if handler.deviceStatusService == nil {
		writeError(response, request, http.StatusServiceUnavailable, "service_unavailable", "设备信息暂时无法读取，请稍后重试")
		return
	}
	statuses, err := handler.deviceStatusService.ListDeviceStatuses(
		request.Context(),
		parentAccountID,
	)
	if err != nil {
		writeDeviceRuntimeError(response, request, err)
		return
	}
	devices := make([]map[string]any, 0, len(statuses))
	for index := range statuses {
		devices = append(devices, deviceStatusResponse(&statuses[index]))
	}
	writeSuccess(response, request, http.StatusOK, map[string]any{
		"devices": devices,
	})
}

// listFamilyChildren returns the child profiles of one guardian, including
// the current parent policy, so operators can answer support questions. The
// endpoint stays read-only because only the guardian may change consent or
// limits through the parent application.
func (handler adminHandler) listFamilyChildren(
	response http.ResponseWriter,
	request *http.Request,
) {
	parentAccountID := strings.TrimSpace(request.PathValue("parent_account_id"))
	if parentAccountID == "" {
		writeError(response, request, http.StatusBadRequest, "invalid_request", "请选择要查看的家长账号")
		return
	}
	if handler.childService == nil {
		writeError(response, request, http.StatusServiceUnavailable, "service_unavailable", "儿童档案暂时无法读取，请稍后重试")
		return
	}
	children, err := handler.childService.List(request.Context(), parentAccountID)
	if err != nil {
		writeError(response, request, http.StatusInternalServerError, "service_error", "暂时无法读取儿童档案")
		return
	}
	result := make([]map[string]any, 0, len(children))
	for index := range children {
		child := &children[index]
		record := childProfileResponse(child)
		if handler.policyService != nil {
			policy, policyErr := handler.policyService.Get(
				request.Context(),
				parentAccountID,
				child.ID,
			)
			if policyErr == nil {
				record["policy"] = parentPolicyResponse(policy)
			}
		}
		result = append(result, record)
	}
	writeSuccess(response, request, http.StatusOK, map[string]any{
		"children": result,
	})
}

func (handler adminHandler) listAIAccounts(
	response http.ResponseWriter,
	request *http.Request,
) {
	accounts, err := handler.service.ListForAdmin(request.Context())
	if err != nil {
		writeError(response, request, http.StatusInternalServerError, "service_error", "暂时无法读取家长账号")
		return
	}
	result := make([]map[string]any, 0, len(accounts))
	for index := range accounts {
		result = append(result, aiAccountAdminResponse(&accounts[index]))
	}
	writeSuccess(response, request, http.StatusOK, map[string]any{"accounts": result})
}

func (handler adminHandler) getReleaseArtifact(
	response http.ResponseWriter,
	request *http.Request,
) {
	artifact, err := handler.operationsService.FindReleaseArtifact(
		request.Context(),
		request.PathValue("version"),
		request.URL.Query().Get("kind"),
		request.URL.Query().Get("platform"),
	)
	if err != nil {
		if errors.Is(err, operationsdomain.ErrReleaseNotFound) {
			writeError(response, request, http.StatusNotFound, "release_artifact_not_found", "没有找到对应的更新文件")
			return
		}
		writeError(response, request, http.StatusInternalServerError, "service_error", "暂时无法读取更新文件")
		return
	}
	writeSuccess(response, request, http.StatusOK, map[string]any{"artifact": artifact})
}

// getAIAccountDefaults exposes the gateway's current defaults instead of the
// management console's saved copy, so the two systems cannot drift.
func (handler adminHandler) getAIAccountDefaults(
	response http.ResponseWriter,
	request *http.Request,
) {
	config, err := handler.service.RuntimeConfigForAdmin(request.Context())
	if err != nil {
		writeError(response, request, http.StatusBadGateway, "provider_error", "暂时无法读取 AI 服务默认设置")
		return
	}
	writeSuccess(response, request, http.StatusOK, aiAccountDefaultsResponse{
		DefaultBalanceUSD:  config.DefaultBalanceUSD,
		DefaultConcurrency: config.DefaultConcurrency,
		Source:             "sub2api",
	})
}

// listAIModels returns the gateway's measured models. Unknown latency remains
// null so an operator can distinguish "not measured" from "zero latency".
func (handler adminHandler) listAIModels(
	response http.ResponseWriter,
	request *http.Request,
) {
	config, err := handler.service.RuntimeConfigForAdmin(request.Context())
	if err != nil {
		writeError(response, request, http.StatusBadGateway, "provider_error", "暂时无法读取可用模型")
		return
	}
	models := make([]aiModelResponse, 0, len(config.Models))
	for _, model := range config.Models {
		if strings.TrimSpace(model.Model) == "" {
			continue
		}
		models = append(models, aiModelResponse{
			ID:        model.Model,
			LatencyMS: model.PrimaryLatencyMs,
		})
	}
	writeSuccess(response, request, http.StatusOK, map[string]any{"models": models})
}

func (handler adminHandler) updateAIAccount(
	response http.ResponseWriter,
	request *http.Request,
) {
	providerAccountID := strings.TrimSpace(request.PathValue("provider_account_id"))
	if providerAccountID == "" {
		writeError(response, request, http.StatusBadRequest, "invalid_request", "请检查填写的内容")
		return
	}
	var payload updateAIAccountRequest
	if err := decodeJSON(request, &payload); err != nil {
		writeError(response, request, http.StatusBadRequest, "invalid_request", "请检查填写的内容")
		return
	}
	if payload.Status != "active" && payload.Status != "suspended" {
		writeError(response, request, http.StatusUnprocessableEntity, "invalid_status", "请选择有效的账号状态")
		return
	}
	if payload.BalanceUSD < 0 || payload.ConcurrencyLimit < 1 {
		writeError(response, request, http.StatusUnprocessableEntity, "invalid_limits", "余额和并发额度必须大于等于 0")
		return
	}
	account, err := handler.service.UpdateForAdmin(
		request.Context(),
		providerAccountID,
		payload.Status,
		payload.BalanceUSD,
		payload.ConcurrencyLimit,
		normalizeModels(payload.AvailableModels),
		payload.Reason,
	)
	if err != nil {
		if errors.Is(err, gatewaydomain.ErrAccountNotFound) {
			writeError(response, request, http.StatusNotFound, "not_found", "没有找到这个账号")
			return
		}
		writeError(response, request, http.StatusBadGateway, "provider_error", "暂时无法更新 AI 服务，请稍后重试")
		return
	}
	writeSuccess(response, request, http.StatusOK, aiAccountAdminResponse(account))
}

// updateParentAIModels lets a guardian choose from provider-approved models.
// The response is a parent-safe summary and never contains a provider key.
func (handler adminHandler) updateParentAIModels(
	response http.ResponseWriter,
	request *http.Request,
) {
	accountID, ok := authenticatedAccountID(request)
	if !ok {
		writeError(response, request, http.StatusUnauthorized, "unauthenticated", "请重新登录")
		return
	}
	var payload updateAIModelsRequest
	if err := decodeJSON(request, &payload); err != nil {
		writeError(response, request, http.StatusBadRequest, "invalid_request", "请检查选择的模型")
		return
	}
	summary, err := handler.service.UpdateModelsForParent(
		request.Context(),
		accountID,
		payload.SelectedModels,
	)
	if err != nil {
		switch {
		case errors.Is(err, gatewaydomain.ErrModelNotAllowed):
			writeError(response, request, http.StatusUnprocessableEntity, "model_not_available", "请选择当前可用的模型")
		case errors.Is(err, gatewaydomain.ErrAccountNotFound):
			writeError(response, request, http.StatusNotFound, "ai_account_not_found", "还没有可用的 AI 服务")
		case errors.Is(err, gatewaydomain.ErrProviderUnavailable),
			errors.Is(err, gatewaydomain.ErrProviderRejected):
			writeError(response, request, http.StatusBadGateway, "ai_service_unavailable", "AI 服务暂时不可用，请稍后重试")
		default:
			writeError(response, request, http.StatusInternalServerError, "service_error", "操作没有完成，请稍后重试")
		}
		return
	}
	writeSuccess(response, request, http.StatusOK, map[string]any{
		"ai_account": map[string]any{
			"status":            summary.Status,
			"balance_usd":       summary.BalanceUSD,
			"concurrency_limit": summary.ConcurrencyLimit,
			"available_models":  summary.AvailableModels,
			"selected_models":   summary.SelectedModels,
			"allowed_models":    summary.AllowedModels,
			"provider_ready":    summary.ProviderReady,
		},
	})
}

func aiAccountAdminResponse(account *gatewaydomain.Account) map[string]any {
	return map[string]any{
		"parent_account_id":   account.ParentAccountID,
		"parent_email":        account.ParentEmail,
		"parent_display_name": account.ParentDisplayName,
		"provider_account_id": account.ProviderAccountID,
		"status":              account.Status,
		"balance_usd":         account.BalanceUSD,
		"concurrency_limit":   account.ConcurrencyLimit,
		"available_models":    account.AvailableModels,
		"selected_models":     account.SelectedModels,
		"allowed_models":      account.AllowedModels,
		"credential_ready":    len(account.APIKeyCiphertext) > 0,
		"updated_at":          account.UpdatedAt,
	}
}

func normalizeModels(models []string) []string {
	normalized := make([]string, 0, len(models))
	seen := make(map[string]struct{}, len(models))
	for _, model := range models {
		model = strings.TrimSpace(model)
		if model == "" || len(model) > 128 {
			continue
		}
		if _, exists := seen[model]; exists {
			continue
		}
		seen[model] = struct{}{}
		normalized = append(normalized, model)
	}
	return normalized
}
