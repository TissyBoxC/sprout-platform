package http

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/TissyBoxC/sprout-platform/packages/go/httpapi"
	gatewaydomain "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/ai_gateway/domain"
	authdomain "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/auth/domain"
	authservice "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/auth/service"
)

type authHandler struct {
	service *authservice.Service
	audit   auditRecorder
}

type registerRequest struct {
	Phone                  string `json:"phone"`
	PhoneVerificationCode  string `json:"phone_verification_code"`
	Password               string `json:"password"`
	GuardianFamilyName     string `json:"guardian_family_name"`
	ChildNickname          string `json:"child_nickname"`
	ChildBirthday          string `json:"child_birthday"`
	GuardianConsentVersion string `json:"guardian_consent_version"`
}

type loginRequest struct {
	Identifier string `json:"identifier"`
	Password   string `json:"password"`
}

type adminLoginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

type bindEmailRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

type updateProfileRequest struct {
	DisplayName        string `json:"display_name"`
	GuardianFamilyName string `json:"guardian_family_name"`
	ChildNickname      string `json:"child_nickname"`
	ChildBirthday      string `json:"child_birthday"`
}

type sendPhoneVerificationRequest struct {
	Phone   string `json:"phone"`
	Purpose string `json:"purpose"`
}

type adminMFALoginRequest struct {
	ChallengeToken string `json:"challenge_token"`
	Code           string `json:"code"`
}

type refreshRequest struct {
	RefreshToken string `json:"refresh_token"`
}

func (handler authHandler) register(response http.ResponseWriter, request *http.Request) {
	var payload registerRequest
	if err := decodeJSON(request, &payload); err != nil {
		writeError(response, request, http.StatusBadRequest, "invalid_request", "请检查填写的内容")
		return
	}
	account, pair, summary, err := handler.service.Register(request.Context(), authdomain.RegisterInput{
		Phone:                  payload.Phone,
		PhoneVerificationCode:  payload.PhoneVerificationCode,
		Password:               payload.Password,
		GuardianFamilyName:     payload.GuardianFamilyName,
		ChildNickname:          payload.ChildNickname,
		ChildBirthday:          payload.ChildBirthday,
		GuardianConsentVersion: payload.GuardianConsentVersion,
	})
	if err != nil {
		writeAuthServiceError(response, request, err)
		return
	}
	writeSuccess(response, request, http.StatusCreated, authResponse(account, pair, summary))
}

func (handler authHandler) login(response http.ResponseWriter, request *http.Request) {
	var payload loginRequest
	if err := decodeJSON(request, &payload); err != nil {
		writeError(response, request, http.StatusBadRequest, "invalid_request", "请检查填写的内容")
		return
	}
	account, pair, summary, err := handler.service.Login(request.Context(), authdomain.LoginInput{
		Identifier: payload.Identifier,
		Password:   payload.Password,
	})
	if err != nil {
		writeAuthServiceError(response, request, err)
		return
	}
	writeSuccess(response, request, http.StatusOK, authResponse(account, pair, summary))
}

// sendPhoneVerification creates a short-lived code request. The local
// development verifier accepts 000000 until an SMS provider is configured.
func (handler authHandler) sendPhoneVerification(
	response http.ResponseWriter,
	request *http.Request,
) {
	var payload sendPhoneVerificationRequest
	if err := decodeJSON(request, &payload); err != nil {
		writeError(response, request, http.StatusBadRequest, "invalid_request", "请检查填写的内容")
		return
	}
	if err := handler.service.SendPhoneVerificationCode(
		request.Context(),
		payload.Phone,
		payload.Purpose,
	); err != nil {
		writeAuthServiceError(response, request, err)
		return
	}
	writeSuccess(response, request, http.StatusOK, map[string]any{
		"sent": true,
	})
}

// startAdminLogin validates the password and returns a short-lived MFA
// challenge. It never returns a session token before TOTP succeeds.
func (handler authHandler) startAdminLogin(
	response http.ResponseWriter,
	request *http.Request,
) {
	var payload adminLoginRequest
	if err := decodeJSON(request, &payload); err != nil {
		writeError(response, request, http.StatusBadRequest, "invalid_request", "请检查填写的内容")
		return
	}
	challengeToken, account, err := handler.service.StartAdminLogin(
		request.Context(),
		authdomain.LoginInput{
			Identifier: payload.Email,
			Password:   payload.Password,
		},
	)
	if err != nil {
		writeAuthServiceError(response, request, err)
		return
	}
	writeSuccess(response, request, http.StatusOK, map[string]any{
		"status":          "mfa_required",
		"challenge_token": challengeToken,
		"account": map[string]any{
			"id":                   account.ID,
			"email":                account.Email,
			"phone":                account.Phone,
			"display_name":         account.DisplayName,
			"guardian_family_name": account.GuardianFamilyName,
			"child_nickname":       account.ChildNickname,
			"child_birthday":       account.ChildBirthday,
			"role":                 account.Role,
			"status":               account.Status,
			"admin_permissions":    account.AdminPermissions,
		},
	})
}

// completeAdminLogin consumes the challenge and returns a regular session.
func (handler authHandler) completeAdminLogin(
	response http.ResponseWriter,
	request *http.Request,
) {
	var payload adminMFALoginRequest
	if err := decodeJSON(request, &payload); err != nil {
		writeError(response, request, http.StatusBadRequest, "invalid_request", "请检查填写的内容")
		return
	}
	account, pair, err := handler.service.CompleteAdminLogin(
		request.Context(),
		authdomain.AdminMFALoginInput{
			ChallengeToken: payload.ChallengeToken,
			Code:           payload.Code,
		},
	)
	if err != nil {
		writeAuthServiceError(response, request, err)
		return
	}
	writeSuccess(response, request, http.StatusOK, authResponse(account, pair, nil))
}

func (handler authHandler) bindEmail(response http.ResponseWriter, request *http.Request) {
	accountID, ok := authenticatedAccountID(request)
	if !ok {
		writeError(response, request, http.StatusUnauthorized, "unauthenticated", "请重新登录")
		return
	}
	var payload bindEmailRequest
	if err := decodeJSON(request, &payload); err != nil {
		writeError(response, request, http.StatusBadRequest, "invalid_request", "请检查填写的内容")
		return
	}
	account, err := handler.service.BindEmail(
		request.Context(),
		accountID,
		authdomain.BindEmailInput{
			Email:    payload.Email,
			Password: payload.Password,
		},
	)
	if err != nil {
		writeAuthServiceError(response, request, err)
		return
	}
	writeSuccess(response, request, http.StatusOK, accountResponse(account, nil))
}

// updateProfile changes only guardian-editable profile fields. Phone and role
// remain immutable to prevent account takeover through profile mutation.
func (handler authHandler) updateProfile(
	response http.ResponseWriter,
	request *http.Request,
) {
	accountID, ok := authenticatedAccountID(request)
	if !ok {
		writeError(response, request, http.StatusUnauthorized, "unauthenticated", "请重新登录")
		return
	}
	var payload updateProfileRequest
	if err := decodeJSON(request, &payload); err != nil {
		writeError(response, request, http.StatusBadRequest, "invalid_request", "请检查填写的内容")
		return
	}
	account, err := handler.service.UpdateProfile(
		request.Context(),
		accountID,
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
	writeSuccess(response, request, http.StatusOK, accountResponse(account, nil))
}

func (handler authHandler) refresh(response http.ResponseWriter, request *http.Request) {
	var payload refreshRequest
	if err := decodeJSON(request, &payload); err != nil {
		writeError(response, request, http.StatusBadRequest, "invalid_request", "请检查填写的内容")
		return
	}
	pair, err := handler.service.Refresh(request.Context(), payload.RefreshToken)
	if err != nil {
		writeAuthServiceError(response, request, err)
		return
	}
	writeSuccess(response, request, http.StatusOK, map[string]any{
		"access_token":  pair.AccessToken,
		"refresh_token": pair.RefreshToken,
		"expires_in":    int(pair.ExpiresIn.Seconds()),
	})
}

func (handler authHandler) logout(response http.ResponseWriter, request *http.Request) {
	var payload refreshRequest
	if err := decodeJSON(request, &payload); err != nil {
		writeError(response, request, http.StatusBadRequest, "invalid_request", "请检查填写的内容")
		return
	}
	if err := handler.service.Logout(request.Context(), payload.RefreshToken); err != nil {
		writeAuthServiceError(response, request, err)
		return
	}
	writeSuccess(response, request, http.StatusOK, map[string]any{"logged_out": true})
}

func (handler authHandler) me(response http.ResponseWriter, request *http.Request) {
	accountID, ok := authenticatedAccountID(request)
	if !ok {
		writeError(response, request, http.StatusUnauthorized, "unauthenticated", "请重新登录")
		return
	}
	account, summary, err := handler.service.GetAccount(request.Context(), accountID)
	if err != nil {
		writeAuthServiceError(response, request, err)
		return
	}
	writeSuccess(response, request, http.StatusOK, accountResponse(account, summary))
}

// parentOverview returns the guardian dashboard counters for the authenticated
// account. It is intentionally separate from admin statistics and never
// exposes another family's data.
func (handler authHandler) parentOverview(
	response http.ResponseWriter,
	request *http.Request,
) {
	accountID, ok := authenticatedAccountID(request)
	if !ok {
		writeError(response, request, http.StatusUnauthorized, "unauthenticated", "请重新登录")
		return
	}
	overview, err := handler.service.ParentOverview(request.Context(), accountID)
	if err != nil {
		writeError(response, request, http.StatusInternalServerError, "service_error", "暂时无法读取首页数据")
		return
	}
	writeSuccess(response, request, http.StatusOK, overview)
}

// updateAIModels lets a guardian select from the provider-approved models.
// The provider remains authoritative; the client never receives a service key.
func (handler authHandler) requireAuthentication(
	next http.HandlerFunc,
) http.HandlerFunc {
	return func(response http.ResponseWriter, request *http.Request) {
		token, ok := bearerToken(request)
		if !ok {
			handler.recordAuthDenied(request, "", "auth.denied", "missing_token")
			writeError(response, request, http.StatusUnauthorized, "unauthenticated", "请重新登录")
			return
		}
		accountID, err := handler.service.VerifyAccessToken(token)
		if err != nil {
			handler.recordAuthDenied(request, "", "auth.denied", "invalid_token")
			writeError(response, request, http.StatusUnauthorized, "unauthenticated", "请重新登录")
			return
		}
		next(response, request.WithContext(
			context.WithValue(request.Context(), authenticatedAccountKey{}, accountID),
		))
	}
}

func (handler authHandler) requireAdmin(next http.HandlerFunc) http.HandlerFunc {
	return handler.requireAuthentication(func(
		response http.ResponseWriter,
		request *http.Request,
	) {
		if _, ok := handler.authorizeAdmin(request, response, ""); !ok {
			return
		}
		next(response, request)
	})
}

// requireAdminPermission wraps a management handler with the administrator
// role check plus one operation-level permission. A denial records the
// required permission so an operator can tell a role rejection apart from a
// capability rejection without reading request bodies.
func (handler authHandler) requireAdminPermission(
	permission string,
	next http.HandlerFunc,
) http.HandlerFunc {
	return handler.requireAdminPermissionFunc(
		func(*http.Request) string { return permission },
		next,
	)
}

// requireAdminPermissionFunc is requireAdminPermission with the required
// permission resolved per request. Route groups whose read and write endpoints
// share one registration (OTA, feature center) use it to demand only the read
// capability from a GET and the manage capability from a mutation.
func (handler authHandler) requireAdminPermissionFunc(
	permissionOf func(*http.Request) string,
	next http.HandlerFunc,
) http.HandlerFunc {
	return handler.requireAuthentication(func(
		response http.ResponseWriter,
		request *http.Request,
	) {
		if _, ok := handler.authorizeAdmin(request, response, permissionOf(request)); !ok {
			return
		}
		next(response, request)
	})
}

// requireAdminPermissionAction is requireAdminPermission plus the success audit
// label used by sensitive mutations.
func (handler authHandler) requireAdminPermissionAction(
	permission string,
	action string,
	next http.HandlerFunc,
) http.HandlerFunc {
	return handler.requireAdminPermission(permission, handler.auditAfterSuccess(action, next))
}

// requireAdminPermissionAudited returns a route-group middleware that demands
// the operation permission from permissionOf and records a success audit for
// mutations labelled by labeler. It keeps OTA and feature-center registration
// concise while applying the same read/manage split as the single-route form.
func (handler authHandler) requireAdminPermissionAudited(
	permissionOf func(*http.Request) string,
	labeler func(*http.Request) string,
) func(http.HandlerFunc) http.HandlerFunc {
	return func(next http.HandlerFunc) http.HandlerFunc {
		return handler.requireAdminPermissionFunc(permissionOf, func(
			response http.ResponseWriter,
			request *http.Request,
		) {
			if handler.audit == nil || !isAdminMutation(request.Method) {
				next(response, request)
				return
			}
			action := labeler(request)
			if action == "" {
				next(response, request)
				return
			}
			handler.auditAfterSuccess(action, next)(response, request)
		})
	}
}

// authorizeAdmin enforces the administrator role and an optional operation
// permission. On denial it writes the standard forbidden response and a narrow
// audit event, returning the account id only when the request may proceed.
func (handler authHandler) authorizeAdmin(
	request *http.Request,
	response http.ResponseWriter,
	requiredPermission string,
) (string, bool) {
	accountID, ok := authenticatedAccountID(request)
	if !ok {
		handler.recordAuthDenied(request, "", "admin.permission.denied", "unauthenticated")
		writeError(response, request, http.StatusForbidden, "insufficient_permission", "你没有权限访问此页面")
		return "", false
	}
	account, err := handler.service.GetIdentity(request.Context(), accountID)
	if err != nil || account.Role != authdomain.RoleAdmin {
		handler.recordAuthDenied(request, accountID, "admin.permission.denied", "insufficient_role")
		writeError(response, request, http.StatusForbidden, "insufficient_permission", "你没有权限访问此页面")
		return "", false
	}
	if requiredPermission != "" &&
		!authdomain.HasAdminPermission(account.AdminPermissions, requiredPermission) {
		handler.recordAuthDenied(
			request,
			accountID,
			"admin.permission.denied",
			"insufficient_permission:"+requiredPermission,
		)
		writeError(response, request, http.StatusForbidden, "insufficient_permission", "你没有权限访问此页面")
		return "", false
	}
	return accountID, true
}

// auditAfterSuccess records one fixed action label after a handler responds
// successfully. It keeps the audit behaviour identical across the role-only,
// permission, and route-group middleware.
func (handler authHandler) auditAfterSuccess(
	action string,
	next http.HandlerFunc,
) http.HandlerFunc {
	return func(response http.ResponseWriter, request *http.Request) {
		if handler.audit == nil {
			next(response, request)
			return
		}
		recorder := &statusRecorder{ResponseWriter: response, status: http.StatusOK}
		next(recorder, request)
		if recorder.status < http.StatusOK || recorder.status >= http.StatusMultipleChoices {
			return
		}
		actorAccountID, _ := authenticatedAccountID(request)
		_ = handler.audit.RecordAudit(
			request.Context(),
			actorAccountID,
			"",
			action,
			map[string]any{
				"route": request.Method + " " + request.URL.Path,
			},
		)
	}
}

// isAdminMutation reports whether the HTTP method changes state and therefore
// warrants a success audit. Read routes never write an audit entry.
func isAdminMutation(method string) bool {
	switch method {
	case http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
		return true
	default:
		return false
	}
}

// recordAuthDenied writes a narrow denial audit event. It records only the
// route and a stable reason code, never the token, credential, or request body,
// so a security investigation keeps a trail without leaking secrets.
func (handler authHandler) recordAuthDenied(
	request *http.Request,
	actorAccountID string,
	action string,
	reason string,
) {
	if handler.audit == nil || request == nil {
		return
	}
	_ = handler.audit.RecordAudit(
		request.Context(),
		actorAccountID,
		"",
		action,
		map[string]any{
			"route":  request.Method + " " + request.URL.Path,
			"reason": reason,
		},
	)
}

type authenticatedAccountKey struct{}

func authenticatedAccountID(request *http.Request) (string, bool) {
	accountID, ok := request.Context().Value(authenticatedAccountKey{}).(string)
	return accountID, ok && strings.TrimSpace(accountID) != ""
}

// AccountEmail resolves the normalized login email for an authenticated
// account. The family module uses it to match an invitation to the accepting
// login without depending on the auth repository.
func (handler authHandler) AccountEmail(
	ctx context.Context,
	accountID string,
) (string, error) {
	account, err := handler.service.GetIdentity(ctx, accountID)
	if err != nil {
		return "", err
	}
	return account.Email, nil
}

func authResponse(
	account *authdomain.ParentAccount,
	pair *authdomain.TokenPair,
	summary *authdomain.AIAccountSummary,
) map[string]any {
	response := accountResponse(account, summary)
	response["access_token"] = pair.AccessToken
	response["refresh_token"] = pair.RefreshToken
	response["expires_in"] = int(pair.ExpiresIn.Seconds())
	return response
}

func accountResponse(
	account *authdomain.ParentAccount,
	summary *authdomain.AIAccountSummary,
) map[string]any {
	response := map[string]any{
		"account": map[string]any{
			"id":                   account.ID,
			"email":                account.Email,
			"phone":                account.Phone,
			"display_name":         account.DisplayName,
			"guardian_family_name": account.GuardianFamilyName,
			"child_nickname":       account.ChildNickname,
			"child_birthday":       account.ChildBirthday,
			"role":                 account.Role,
			"status":               account.Status,
			"admin_permissions":    account.AdminPermissions,
		},
		"ai_account": nil,
	}
	if summary != nil {
		response["ai_account"] = map[string]any{
			"status":            summary.Status,
			"balance_usd":       summary.BalanceUSD,
			"concurrency_limit": summary.ConcurrencyLimit,
			"available_models":  summary.AvailableModels,
			"selected_models":   summary.SelectedModels,
			"allowed_models":    summary.AllowedModels,
			"provider_ready":    summary.ProviderReady,
		}
	}
	return response
}

func writeAuthServiceError(
	response http.ResponseWriter,
	request *http.Request,
	err error,
) {
	switch {
	case errors.Is(err, authdomain.ErrEmailExists):
		writeError(response, request, http.StatusConflict, "email_exists", "这个邮箱已经注册过")
	case errors.Is(err, authdomain.ErrPhoneExists):
		writeError(response, request, http.StatusConflict, "phone_exists", "这个手机号已经注册过")
	case errors.Is(err, authdomain.ErrInvalidEmail):
		writeError(response, request, http.StatusUnprocessableEntity, "invalid_email", "请输入有效的邮箱")
	case errors.Is(err, authdomain.ErrInvalidPhone):
		writeError(response, request, http.StatusUnprocessableEntity, "invalid_phone", "请输入有效的手机号")
	case errors.Is(err, authdomain.ErrPhoneVerification):
		writeError(response, request, http.StatusServiceUnavailable, "phone_verification_unavailable", "短信验证暂时不可用，请稍后重试")
	case errors.Is(err, authdomain.ErrWeakPassword):
		writeError(response, request, http.StatusUnprocessableEntity, "weak_password", "密码至少 8 位，并同时包含字母和数字")
	case errors.Is(err, authdomain.ErrRegistrationDisabled):
		writeError(response, request, http.StatusForbidden, "registration_disabled", "新账号注册暂时关闭")
	case errors.Is(err, authdomain.ErrEmailLoginDisabled):
		writeError(response, request, http.StatusForbidden, "email_login_disabled", "邮箱登录暂时关闭，请使用手机号登录")
	case errors.Is(err, authdomain.ErrInvalidDisplayName):
		writeError(response, request, http.StatusUnprocessableEntity, "invalid_display_name", "请输入家长称呼")
	case errors.Is(err, authdomain.ErrInvalidGuardianName):
		writeError(response, request, http.StatusUnprocessableEntity, "invalid_guardian_name", "家长姓氏不能超过 40 个字")
	case errors.Is(err, authdomain.ErrInvalidChildNickname):
		writeError(response, request, http.StatusUnprocessableEntity, "invalid_child_nickname", "宝贝姓名不能超过 40 个字")
	case errors.Is(err, authdomain.ErrInvalidChildBirthday):
		writeError(response, request, http.StatusUnprocessableEntity, "invalid_child_birthday", "请输入有效日期，例如 2021-06-01")
	case errors.Is(err, authdomain.ErrInvalidCredentials):
		writeError(response, request, http.StatusUnauthorized, "invalid_credentials", "手机号、邮箱或密码不正确")
	case errors.Is(err, authdomain.ErrInvalidVerification):
		writeError(response, request, http.StatusUnauthorized, "invalid_verification_code", "验证码不正确，请重新输入")
	case errors.Is(err, authdomain.ErrAccountDisabled):
		writeError(response, request, http.StatusForbidden, "account_disabled", "这个账号当前无法登录")
	case errors.Is(err, authdomain.ErrMFANotConfigured):
		writeError(response, request, http.StatusServiceUnavailable, "mfa_not_configured", "管理员验证尚未设置，请联系维护人员")
	case errors.Is(err, authdomain.ErrInvalidMFACode):
		writeError(response, request, http.StatusUnauthorized, "invalid_mfa_code", "验证码不正确，请重新输入")
	case errors.Is(err, authdomain.ErrMFAChallengeNotFound),
		errors.Is(err, authdomain.ErrMFAChallengeExpired),
		errors.Is(err, authdomain.ErrMFAChallengeConsumed):
		writeError(response, request, http.StatusUnauthorized, "mfa_challenge_expired", "验证已过期，请重新登录")
	case errors.Is(err, authdomain.ErrInsufficientPrivilege):
		writeError(response, request, http.StatusForbidden, "insufficient_permission", "你没有权限访问此页面")
	case errors.Is(err, authdomain.ErrSessionNotFound),
		errors.Is(err, authdomain.ErrSessionExpired):
		writeError(response, request, http.StatusUnauthorized, "session_expired", "登录已过期，请重新登录")
	case errors.Is(err, authdomain.ErrAccountNotFound):
		writeError(response, request, http.StatusNotFound, "account_not_found", "没有找到这个账号")
	case errors.Is(err, gatewaydomain.ErrProviderUnavailable),
		errors.Is(err, gatewaydomain.ErrProviderRejected):
		writeError(response, request, http.StatusBadGateway, "ai_service_unavailable", "AI 服务暂时不可用，请稍后重试")
	default:
		writeError(response, request, http.StatusInternalServerError, "service_error", "操作没有完成，请稍后重试")
	}
}

func decodeJSON(request *http.Request, destination any) error {
	decoder := json.NewDecoder(request.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		return err
	}
	return nil
}

func bearerToken(request *http.Request) (string, bool) {
	header := strings.TrimSpace(request.Header.Get("Authorization"))
	const prefix = "Bearer "
	if len(header) <= len(prefix) || !strings.EqualFold(header[:len(prefix)], prefix) {
		return "", false
	}
	token := strings.TrimSpace(header[len(prefix):])
	return token, token != ""
}

func writeSuccess(response http.ResponseWriter, request *http.Request, status int, data any) {
	httpapi.WriteSuccess(response, request, status, data)
}

func writeError(response http.ResponseWriter, request *http.Request, status int, code string, message string) {
	httpapi.WriteError(response, request, status, code, message, false)
}
