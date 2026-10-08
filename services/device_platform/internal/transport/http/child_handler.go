package http

import (
	"context"
	"errors"
	"net/http"
	"strings"

	childdomain "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/child/domain"
	childservice "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/child/service"
	policydomain "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/parent_policy/domain"
	policeservice "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/parent_policy/service"
)

// DefaultGuardianConsentVersion is the consent revision the platform records
// when a guardian confirms the current notice.
const DefaultGuardianConsentVersion = "2026-01"

// childService is the guardian-facing child profile surface.
//
// The HTTP layer depends on this narrow interface so the child module stays
// removable and tests can substitute a small fake.
type childService interface {
	Create(
		ctx context.Context,
		familyID string,
		input childdomain.ProfileInput,
		guardianConsentVersion string,
	) (*childdomain.Child, error)
	List(ctx context.Context, familyID string) ([]childdomain.Child, error)
	Get(ctx context.Context, familyID string, childID string) (*childdomain.Child, error)
	Update(
		ctx context.Context,
		familyID string,
		childID string,
		input childdomain.ProfileInput,
	) (*childdomain.Child, error)
	Delete(ctx context.Context, familyID string, childID string) error
}

// policyService is the guardian-facing parent policy surface.
type policyService interface {
	Get(
		ctx context.Context,
		familyID string,
		childID string,
	) (*policydomain.Policy, error)
	List(ctx context.Context, familyID string) ([]policydomain.Policy, error)
	Update(
		ctx context.Context,
		familyID string,
		childID string,
		input policydomain.PolicyInput,
	) (*policydomain.Policy, error)
	UpdateWithVersion(
		ctx context.Context,
		familyID string,
		childID string,
		input policydomain.PolicyInput,
		expectedVersion int,
	) (*policydomain.Policy, error)
}

type childHandler struct {
	service       childService
	policyService policyService
}

type childProfileRequest struct {
	Nickname          string   `json:"nickname"`
	AgeTier           string   `json:"age_tier"`
	Interests         []string `json:"interests"`
	ContentCategories []string `json:"content_categories"`
	GuardianConsent   bool     `json:"guardian_consent"`
}

type disabledPeriodRequest struct {
	StartTime string `json:"start_time"`
	EndTime   string `json:"end_time"`
}

type parentPolicyRequest struct {
	PolicyVersion     int                     `json:"policy_version"`
	DailyLimitMinutes int                     `json:"daily_limit_minutes"`
	AllowedCategories []string                `json:"allowed_categories"`
	DisabledPeriods   []disabledPeriodRequest `json:"disabled_periods"`
	MaxVolumePercent  int                     `json:"max_volume_percent"`
}

func (handler childHandler) list(
	response http.ResponseWriter,
	request *http.Request,
) {
	familyID, ok := authenticatedAccountID(request)
	if !ok {
		writeError(response, request, http.StatusUnauthorized, "unauthenticated", "请重新登录")
		return
	}
	children, err := handler.service.List(request.Context(), familyID)
	if err != nil {
		writeChildError(response, request, err)
		return
	}
	result := make([]map[string]any, 0, len(children))
	for index := range children {
		result = append(result, childProfileResponse(&children[index]))
	}
	writeSuccess(response, request, http.StatusOK, map[string]any{
		"children": result,
	})
}

func (handler childHandler) create(
	response http.ResponseWriter,
	request *http.Request,
) {
	familyID, ok := authenticatedAccountID(request)
	if !ok {
		writeError(response, request, http.StatusUnauthorized, "unauthenticated", "请重新登录")
		return
	}
	var payload childProfileRequest
	if err := decodeJSON(request, &payload); err != nil {
		writeError(response, request, http.StatusBadRequest, "invalid_request", "请检查填写的内容")
		return
	}
	if !payload.GuardianConsent {
		writeError(response, request, http.StatusUnprocessableEntity, "guardian_consent_required", "请确认已获得监护人同意")
		return
	}
	child, err := handler.service.Create(
		request.Context(),
		familyID,
		childdomain.ProfileInput{
			Nickname:          payload.Nickname,
			AgeTier:           payload.AgeTier,
			Interests:         payload.Interests,
			ContentCategories: payload.ContentCategories,
		},
		DefaultGuardianConsentVersion,
	)
	if err != nil {
		writeChildError(response, request, err)
		return
	}
	writeSuccess(response, request, http.StatusCreated, map[string]any{
		"child": childProfileResponse(child),
	})
}

func (handler childHandler) update(
	response http.ResponseWriter,
	request *http.Request,
) {
	familyID, ok := authenticatedAccountID(request)
	if !ok {
		writeError(response, request, http.StatusUnauthorized, "unauthenticated", "请重新登录")
		return
	}
	var payload childProfileRequest
	if err := decodeJSON(request, &payload); err != nil {
		writeError(response, request, http.StatusBadRequest, "invalid_request", "请检查填写的内容")
		return
	}
	child, err := handler.service.Update(
		request.Context(),
		familyID,
		strings.TrimSpace(request.PathValue("child_id")),
		childdomain.ProfileInput{
			Nickname:          payload.Nickname,
			AgeTier:           payload.AgeTier,
			Interests:         payload.Interests,
			ContentCategories: payload.ContentCategories,
		},
	)
	if err != nil {
		writeChildError(response, request, err)
		return
	}
	writeSuccess(response, request, http.StatusOK, map[string]any{
		"child": childProfileResponse(child),
	})
}

func (handler childHandler) delete(
	response http.ResponseWriter,
	request *http.Request,
) {
	familyID, ok := authenticatedAccountID(request)
	if !ok {
		writeError(response, request, http.StatusUnauthorized, "unauthenticated", "请重新登录")
		return
	}
	if err := handler.service.Delete(
		request.Context(),
		familyID,
		strings.TrimSpace(request.PathValue("child_id")),
	); err != nil {
		writeChildError(response, request, err)
		return
	}
	writeSuccess(response, request, http.StatusOK, map[string]any{"deleted": true})
}

func (handler childHandler) getPolicy(
	response http.ResponseWriter,
	request *http.Request,
) {
	familyID, ok := authenticatedAccountID(request)
	if !ok {
		writeError(response, request, http.StatusUnauthorized, "unauthenticated", "请重新登录")
		return
	}
	policy, err := handler.policyService.Get(
		request.Context(),
		familyID,
		strings.TrimSpace(request.PathValue("child_id")),
	)
	if err != nil {
		writeChildError(response, request, err)
		return
	}
	writeSuccess(response, request, http.StatusOK, map[string]any{
		"policy": parentPolicyResponse(policy),
	})
}

func (handler childHandler) updatePolicy(
	response http.ResponseWriter,
	request *http.Request,
) {
	familyID, ok := authenticatedAccountID(request)
	if !ok {
		writeError(response, request, http.StatusUnauthorized, "unauthenticated", "请重新登录")
		return
	}
	var payload parentPolicyRequest
	if err := decodeJSON(request, &payload); err != nil {
		writeError(response, request, http.StatusBadRequest, "invalid_request", "请检查填写的内容")
		return
	}
	periods := make([]policydomain.DisabledPeriod, 0, len(payload.DisabledPeriods))
	for _, period := range payload.DisabledPeriods {
		periods = append(periods, policydomain.DisabledPeriod{
			StartTime: period.StartTime,
			EndTime:   period.EndTime,
		})
	}
	policy, err := handler.policyService.UpdateWithVersion(
		request.Context(),
		familyID,
		strings.TrimSpace(request.PathValue("child_id")),
		policydomain.PolicyInput{
			DailyLimitMinutes: payload.DailyLimitMinutes,
			AllowedCategories: payload.AllowedCategories,
			DisabledPeriods:   periods,
			MaxVolumePercent:  payload.MaxVolumePercent,
		},
		payload.PolicyVersion,
	)
	if err != nil {
		writeChildError(response, request, err)
		return
	}
	writeSuccess(response, request, http.StatusOK, map[string]any{
		"policy": parentPolicyResponse(policy),
	})
}

func childProfileResponse(child *childdomain.Child) map[string]any {
	if child == nil {
		return nil
	}
	return map[string]any{
		"child_id":                 child.ID,
		"family_id":                child.FamilyID,
		"nickname":                 child.Nickname,
		"age_tier":                 child.AgeTier,
		"interests":                child.Interests,
		"content_categories":       child.ContentCategories,
		"guardian_consent":         true,
		"guardian_consent_version": child.GuardianConsentVersion,
		"created_at":               child.CreatedAt,
		"updated_at":               child.UpdatedAt,
	}
}

func parentPolicyResponse(policy *policydomain.Policy) map[string]any {
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
		"policy_id":           policy.ID,
		"family_id":           policy.FamilyID,
		"child_id":            policy.ChildID,
		"policy_version":      policy.PolicyVersion,
		"daily_limit_minutes": policy.DailyLimitMinutes,
		"allowed_categories":  policy.AllowedCategories,
		"disabled_periods":    disabledPeriods,
		"max_volume_percent":  policy.MaxVolumePercent,
		"updated_at":          policy.UpdatedAt,
	}
}

func writeChildError(
	response http.ResponseWriter,
	request *http.Request,
	err error,
) {
	switch {
	case errors.Is(err, childdomain.ErrChildNotFound),
		errors.Is(err, policydomain.ErrPolicyNotFound):
		writeError(response, request, http.StatusNotFound, "child_not_found", "没有找到这个宝贝的档案")
	case errors.Is(err, childdomain.ErrInvalidChildNickname):
		writeError(response, request, http.StatusUnprocessableEntity, "invalid_child_nickname", "请输入 1 到 32 个字的宝贝称呼")
	case errors.Is(err, childdomain.ErrInvalidAgeTier):
		writeError(response, request, http.StatusUnprocessableEntity, "invalid_age_tier", "请选择宝贝的年龄段")
	case errors.Is(err, childdomain.ErrInvalidInterests):
		writeError(response, request, http.StatusUnprocessableEntity, "invalid_interests", "请选择有效的兴趣标签")
	case errors.Is(err, childdomain.ErrInvalidCategories),
		errors.Is(err, policydomain.ErrInvalidCategories):
		writeError(response, request, http.StatusUnprocessableEntity, "invalid_content_categories", "请至少选择一个内容分类")
	case errors.Is(err, childdomain.ErrGuardianConsent):
		writeError(response, request, http.StatusUnprocessableEntity, "guardian_consent_required", "请确认已获得监护人同意")
	case errors.Is(err, policydomain.ErrInvalidDailyLimit):
		writeError(response, request, http.StatusUnprocessableEntity, "invalid_daily_limit", "每日时长需要在 0 到 720 分钟之间")
	case errors.Is(err, policydomain.ErrInvalidDisabledHours):
		writeError(response, request, http.StatusUnprocessableEntity, "invalid_disabled_periods", "请检查免打扰时段")
	case errors.Is(err, policydomain.ErrInvalidVolume):
		writeError(response, request, http.StatusUnprocessableEntity, "invalid_volume", "最大音量需要在 0 到 100 之间")
	case errors.Is(err, policydomain.ErrPolicyVersionConflict):
		writeError(response, request, http.StatusConflict, "version_conflict", "家长策略已更新，请重新加载后再保存")
	default:
		writeError(response, request, http.StatusInternalServerError, "service_error", "操作没有完成，请稍后重试")
	}
}

var (
	_ childService  = (*childservice.Service)(nil)
	_ policyService = (*policeservice.Service)(nil)
)
