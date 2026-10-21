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
	deviceBindingService  adminDeviceManagementService
	childService          adminChildService
	policyService         adminPolicyService
	auditService          auditRecorder
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
		expectedVersion int64,
	) (*operationsdomain.Settings, int64, error)
	Overview(ctx context.Context) (*operationsdomain.Overview, error)
	ListFamilyAccounts(
		ctx context.Context,
		page int,
		pageSize int,
	) (operationsdomain.FamilyAccountPage, error)
	ListReleases(
		ctx context.Context,
		page int,
		pageSize int,
	) (operationsdomain.ReleasePage, error)
	CreateRelease(
		ctx context.Context,
		input operationsdomain.ReleaseInput,
	) (*operationsdomain.Release, error)
	PublishRelease(
		ctx context.Context,
		version string,
		actorAccountID string,
	) error
	DeleteRelease(ctx context.Context, releaseID string) error
	FindReleaseArtifact(
		ctx context.Context,
		version string,
		kind string,
		platform string,
		channel string,
	) (*operationsdomain.ReleaseArtifact, error)
	AppUpdate(
		ctx context.Context,
		platform string,
		channel string,
		currentVersion string,
		currentResourceVersion string,
	) (*operationsdomain.AppUpdate, error)
}

// serviceVersionAdminService is the management-facing surface for the brand
// service inventory and its container upgrades.
type serviceVersionAdminService interface {
	Snapshot(
		ctx context.Context,
	) ([]serviceversiondomain.Service, time.Time, error)
	AllCurrent(services []serviceversiondomain.Service) bool
	UpgradeExecutionEnabled() bool
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
	PromoteStagedFile(
		ctx context.Context,
		input releaseStoredomain.PromoteStagedInput,
	) (releaseStoredomain.File, error)
	DeleteFile(ctx context.Context, relativePath string) error
	// DigestForPath hashes one stored artifact on demand. Inventory stopped
	// hashing every file because that read 6.1 GB, took 96s and timed the
	// operator page out; registering a pre-existing file asks for it here.
	DigestForPath(ctx context.Context, relativePath string) (string, error)
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

// adminDeviceManagementService is the destructive support surface for device
// lifecycle operations. Every method must verify the parent-device relation.
type adminDeviceManagementService interface {
	Delete(ctx context.Context, parentAccountID string, deviceID string) error
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
	ID            string `json:"id"`
	LatencyMS     *int   `json:"latency_ms"`
	IsRecommended bool   `json:"is_recommended"`
}

type createParentRequest struct {
	Phone                  string `json:"phone"`
	Password               string `json:"password"`
	GuardianFamilyName     string `json:"guardian_family_name"`
	ChildNickname          string `json:"child_nickname"`
	ChildBirthday          string `json:"child_birthday"`
	GuardianConsentVersion string `json:"guardian_consent_version"`
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
			GuardianConsentVersion: strings.TrimSpace(payload.GuardianConsentVersion),
		},
	)
	if err != nil {
		writeAuthServiceError(response, request, err)
		return
	}
	payloadResponse := accountResponse(account, summary)
	if err := handler.recordAdminAudit(
		request,
		account.ID,
		"admin.family.created",
		map[string]any{"scope": "family"},
	); err != nil {
		writeAuditNotice(response, request, payloadResponse)
		return
	}
	writeSuccess(response, request, http.StatusCreated, payloadResponse)
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
	payloadResponse := map[string]any{
		"ai_account": map[string]any{
			"status":            summary.Status,
			"balance_usd":       summary.BalanceUSD,
			"concurrency_limit": summary.ConcurrencyLimit,
			"available_models":  summary.AvailableModels,
			"selected_models":   summary.SelectedModels,
			"allowed_models":    summary.AllowedModels,
			"provider_ready":    summary.ProviderReady,
		},
	}
	if err := handler.recordAdminAudit(
		request,
		account.ID,
		"admin.family.ai_account.created",
		map[string]any{
			"status":            summary.Status,
			"balance_usd":       summary.BalanceUSD,
			"concurrency_limit": summary.ConcurrencyLimit,
		},
	); err != nil {
		writeAuditNotice(response, request, payloadResponse)
		return
	}
	writeSuccess(response, request, http.StatusOK, payloadResponse)
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
	payloadResponse := map[string]any{
		"account": map[string]any{
			"parent_account_id":    account.ID,
			"display_name":         account.DisplayName,
			"guardian_family_name": account.GuardianFamilyName,
			"child_nickname":       account.ChildNickname,
			"child_birthday":       account.ChildBirthday,
		},
	}
	if err := handler.recordAdminAudit(
		request,
		account.ID,
		"admin.family.profile.updated",
		map[string]any{"scope": "profile"},
	); err != nil {
		writeAuditNotice(response, request, payloadResponse)
		return
	}
	writeSuccess(response, request, http.StatusOK, payloadResponse)
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
	payloadResponse := map[string]any{"password_reset": true}
	if err := handler.recordAdminAudit(
		request,
		parentAccountID,
		"admin.family.password.reset",
		map[string]any{"scope": "credential"},
	); err != nil {
		writeAuditNotice(response, request, payloadResponse)
		return
	}
	writeSuccess(response, request, http.StatusOK, payloadResponse)
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

// unbindParentDevice removes one device from a family and revokes its
// sessions. The parent account is part of the lookup, so an operator cannot
// unbind a device by supplying an unrelated device id.
func (handler adminHandler) unbindParentDevice(
	response http.ResponseWriter,
	request *http.Request,
) {
	parentAccountID := strings.TrimSpace(
		request.PathValue("parent_account_id"),
	)
	deviceID := strings.TrimSpace(request.PathValue("device_id"))
	if parentAccountID == "" || deviceID == "" {
		writeError(response, request, http.StatusBadRequest, "invalid_request", "请检查要解绑的设备")
		return
	}
	if handler.deviceBindingService == nil {
		writeError(response, request, http.StatusServiceUnavailable, "service_unavailable", "设备信息暂时无法处理，请稍后重试")
		return
	}
	if err := handler.deviceBindingService.Delete(
		request.Context(),
		parentAccountID,
		deviceID,
	); err != nil {
		writeDeviceBindingError(response, request, err)
		return
	}
	payloadResponse := map[string]any{"unbound": true}
	if err := handler.recordAdminAudit(
		request,
		parentAccountID,
		"admin.family.device_binding.revoked",
		map[string]any{"device_id": deviceID},
	); err != nil {
		writeAuditNotice(response, request, payloadResponse)
		return
	}
	writeSuccess(response, request, http.StatusOK, payloadResponse)
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
		// Operators must be able to tell a genuinely missing policy from a
		// failed read, so the read outcome is reported explicitly instead of
		// being dropped into an absent field.
		if handler.policyService == nil {
			record["policy"] = nil
			record["policy_state"] = "unavailable"
			record["policy_error"] = "策略服务暂时不可用"
		} else if policy, policyErr := handler.policyService.Get(
			request.Context(),
			parentAccountID,
			child.ID,
		); policyErr == nil {
			record["policy"] = parentPolicyResponse(policy)
			record["policy_state"] = "available"
		} else if errors.Is(policyErr, policydomain.ErrPolicyNotFound) {
			record["policy"] = nil
			record["policy_state"] = "not_set"
		} else {
			record["policy"] = nil
			record["policy_state"] = "unavailable"
			record["policy_error"] = "策略暂时无法读取"
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
		request.URL.Query().Get("channel"),
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
			IsRecommended: strings.EqualFold(
				strings.TrimSpace(model.Model),
				strings.TrimSpace(config.RecommendedModel),
			),
		})
	}
	writeSuccess(response, request, http.StatusOK, map[string]any{
		"models":            models,
		"recommended_model": strings.TrimSpace(config.RecommendedModel),
	})
}

// getAIGatewayChildPolicy exposes the child-safety policy the AI gateway is
// enforcing, projected onto the platform's canonical age tiers. Operators use
// it to confirm every age actually maps to an enforced band. The response never
// includes the injected safety-boundary text or the internal service token.
func (handler adminHandler) getAIGatewayChildPolicy(
	response http.ResponseWriter,
	request *http.Request,
) {
	alignment, err := handler.service.ChildPolicyAlignmentForAdmin(request.Context())
	if err != nil {
		writeError(response, request, http.StatusBadGateway, "provider_error", "暂时无法读取儿童安全策略")
		return
	}
	writeSuccess(response, request, http.StatusOK, childPolicyAlignmentResponse(alignment))
}

// childPolicyAlignmentResponse renders the aligned policy for the console. It
// mirrors the domain shape and deliberately omits the gateway's safety-boundary
// text and service token.
func childPolicyAlignmentResponse(
	alignment *gatewaydomain.ChildPolicyAlignment,
) map[string]any {
	if alignment == nil {
		return map[string]any{
			"enabled": false,
			"tiers":   []map[string]any{},
			"bands":   []map[string]any{},
			"aligned": false,
		}
	}
	bands := make([]map[string]any, 0, len(alignment.AgeBands))
	for _, band := range alignment.AgeBands {
		bands = append(bands, map[string]any{
			"name":              band.Name,
			"allowed_models":    band.AllowedModels,
			"max_output_tokens": band.MaxOutputTokens,
			"content_rating":    band.ContentRating,
		})
	}
	tiers := make([]map[string]any, 0, len(alignment.Tiers))
	for _, tier := range alignment.Tiers {
		tiers = append(tiers, map[string]any{
			"age_tier":          tier.AgeTier,
			"age_band":          tier.AgeBand,
			"bound":             tier.Bound,
			"age_band_missing":  tier.AgeBandMissing,
			"allowed_models":    tier.AllowedModels,
			"max_output_tokens": tier.MaxOutputTokens,
			"content_rating":    tier.ContentRating,
		})
	}
	return map[string]any{
		"enabled":                alignment.Enabled,
		"require_policy_version": alignment.RequirePolicyVersion,
		"default_age_band":       alignment.DefaultAgeBand,
		"inject_safety_boundary": alignment.InjectSafetyBoundary,
		"aligned":                alignment.Aligned,
		"bands":                  bands,
		"tiers":                  tiers,
	}
}

// getFamilyAIUsage reconciles one guardian's AI spend against the gateway's
// own aggregated usage. It never returns provider credentials or any prompt or
// reply content; the window defaults to the last 24 hours and is capped so a
// single console request cannot force an unbounded gateway scan.
func (handler adminHandler) getFamilyAIUsage(
	response http.ResponseWriter,
	request *http.Request,
) {
	parentAccountID := strings.TrimSpace(request.PathValue("parent_account_id"))
	if parentAccountID == "" {
		writeError(response, request, http.StatusBadRequest, "invalid_request", "请检查填写的内容")
		return
	}
	startTime, endTime, ok := aiUsageReconciliationWindow(request)
	if !ok {
		writeError(response, request, http.StatusUnprocessableEntity, "invalid_window", "查询时间范围不正确")
		return
	}
	usage, err := handler.service.UsageReconciliationForAdmin(
		request.Context(),
		parentAccountID,
		startTime,
		endTime,
	)
	if err != nil {
		handler.writeAIUsageError(response, request, err)
		return
	}
	models := make([]map[string]any, 0, len(usage.Models))
	for _, model := range usage.Models {
		models = append(models, map[string]any{
			"model":                 model.Model,
			"requests":              model.Requests,
			"input_tokens":          model.InputTokens,
			"output_tokens":         model.OutputTokens,
			"cache_creation_tokens": model.CacheCreationTokens,
			"cache_read_tokens":     model.CacheReadTokens,
			"total_tokens":          model.TotalTokens,
			"cost_usd":              model.CostUSD,
			"actual_cost_usd":       model.ActualCostUSD,
		})
	}
	writeSuccess(response, request, http.StatusOK, map[string]any{
		"provider_account_id": usage.ProviderAccountID,
		"start_time":          usage.StartTime,
		"end_time":            usage.EndTime,
		"totals": map[string]any{
			"requests":              usage.Totals.Requests,
			"input_tokens":          usage.Totals.InputTokens,
			"output_tokens":         usage.Totals.OutputTokens,
			"cache_creation_tokens": usage.Totals.CacheCreationTokens,
			"cache_read_tokens":     usage.Totals.CacheReadTokens,
			"total_tokens":          usage.Totals.TotalTokens,
			"cost_usd":              usage.Totals.CostUSD,
			"actual_cost_usd":       usage.Totals.ActualCostUSD,
			"average_duration_ms":   usage.Totals.AverageDurationMs,
		},
		"models": models,
	})
}

// aiUsageReconciliationWindowMaxDays bounds a single reconciliation query to
// match the gateway's own limit; the default window is the last day.
const aiUsageReconciliationWindowMaxDays = 92

func aiUsageReconciliationWindow(
	request *http.Request,
) (time.Time, time.Time, bool) {
	if request == nil || request.URL == nil {
		return time.Time{}, time.Time{}, false
	}
	query := request.URL.Query()
	endTime := time.Now().UTC()
	if raw := strings.TrimSpace(query.Get("end_time")); raw != "" {
		parsed, err := time.Parse(time.RFC3339, raw)
		if err != nil {
			return time.Time{}, time.Time{}, false
		}
		endTime = parsed.UTC()
	}
	startTime := endTime.AddDate(0, 0, -1)
	if raw := strings.TrimSpace(query.Get("start_time")); raw != "" {
		parsed, err := time.Parse(time.RFC3339, raw)
		if err != nil {
			return time.Time{}, time.Time{}, false
		}
		startTime = parsed.UTC()
	}
	if startTime.After(endTime) {
		return time.Time{}, time.Time{}, false
	}
	if endTime.Sub(startTime) > aiUsageReconciliationWindowMaxDays*24*time.Hour {
		return time.Time{}, time.Time{}, false
	}
	return startTime, endTime, true
}

func (handler adminHandler) writeAIUsageError(
	response http.ResponseWriter,
	request *http.Request,
	err error,
) {
	switch {
	case errors.Is(err, gatewaydomain.ErrAccountNotFound):
		writeError(response, request, http.StatusNotFound, "ai_account_not_found", "这个账号还没有 AI 服务")
	case errors.Is(err, gatewaydomain.ErrProviderUnavailable),
		errors.Is(err, gatewaydomain.ErrProviderRejected):
		writeError(response, request, http.StatusBadGateway, "ai_service_unavailable", "AI 服务暂时不可用，请稍后重试")
	default:
		writeError(response, request, http.StatusInternalServerError, "service_error", "操作没有完成，请稍后重试")
	}
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
	payloadResponse := aiAccountAdminResponse(account)
	if err := handler.recordAdminAudit(
		request,
		account.ParentAccountID,
		"admin.family.ai_account.updated",
		map[string]any{
			"new_status":        account.Status,
			"balance_usd":       account.BalanceUSD,
			"concurrency_limit": account.ConcurrencyLimit,
		},
	); err != nil {
		writeAuditNotice(response, request, payloadResponse)
		return
	}
	writeSuccess(response, request, http.StatusOK, payloadResponse)
}

func (handler adminHandler) recordAdminAudit(
	request *http.Request,
	targetAccountID string,
	action string,
	detail map[string]any,
) error {
	if handler.auditService == nil {
		return errors.New(auditActionUnavailable)
	}
	if request == nil {
		return errors.New("audit request unavailable")
	}
	actorAccountID, ok := authenticatedAccountID(request)
	if !ok {
		return errors.New(auditActionUnauthenticated)
	}
	return handler.auditService.RecordAudit(
		request.Context(),
		actorAccountID,
		targetAccountID,
		action,
		detail,
	)
}

func writeAuditNotice(
	response http.ResponseWriter,
	request *http.Request,
	data any,
) {
	writeSuccessWithNotice(
		response,
		request,
		http.StatusAccepted,
		data,
		"audit_write_failed",
		"操作已经保存，但安全记录没有完成。请联系管理员核对后再继续。",
	)
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
