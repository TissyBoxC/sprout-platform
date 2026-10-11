package http

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	ui_textdomain "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/ui_text/domain"
	ui_textservice "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/ui_text/service"
)

type uiTextHandler struct {
	service *ui_textservice.Service
}

// deviceUITextHandler exposes published copy to firmware devices that hold a
// device session token rather than a guardian credential. The path device id
// must match the session owner so one device cannot read another's manifest.
type deviceUITextHandler struct {
	handler        uiTextHandler
	bindingService englishSessionResolver
}

type uiTextEntryRequest struct {
	UITextKey string   `json:"ui_text_key"`
	Value     string   `json:"value"`
	MaxLength int      `json:"max_length"`
	Variables []string `json:"variables"`
}

type uiTextDraftRequest struct {
	PackageID        string               `json:"package_id"`
	Language         string               `json:"language"`
	Platform         string               `json:"platform"`
	MinClientVersion string               `json:"min_client_version"`
	Entries          []uiTextEntryRequest `json:"entries"`
}

type uiTextReviewRequest struct {
	Reason string `json:"reason"`
}

type uiTextPublishRequest struct {
	SignatureKeyID string `json:"signature_key_id"`
	Signature      string `json:"signature"`
}

func (handler uiTextHandler) listVersions(
	response http.ResponseWriter,
	request *http.Request,
) {
	page, pageSize, err := uiTextPageParams(request)
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
	versions, total, err := handler.service.ListVersions(
		request.Context(),
		ui_textdomain.VersionFilter{
			PackageID: strings.TrimSpace(request.URL.Query().Get("package_id")),
			Language:  strings.TrimSpace(request.URL.Query().Get("language")),
			Platform:  ui_textdomain.Platform(strings.TrimSpace(request.URL.Query().Get("platform"))),
			Status:    ui_textdomain.Status(strings.TrimSpace(request.URL.Query().Get("status"))),
			Page:      page,
			PageSize:  pageSize,
		},
	)
	if err != nil {
		writeUITextError(response, request, err)
		return
	}
	writeSuccess(response, request, http.StatusOK, map[string]any{
		"versions":  versions,
		"page":      page,
		"page_size": pageSize,
		"total":     total,
	})
}

func (handler uiTextHandler) getVersion(
	response http.ResponseWriter,
	request *http.Request,
) {
	packageVersion, err := uiTextVersionParam(request)
	if err != nil {
		writeError(
			response,
			request,
			http.StatusUnprocessableEntity,
			"invalid_version",
			"请检查文案版本号",
		)
		return
	}
	detail, err := handler.service.GetVersion(
		request.Context(),
		request.PathValue("package_id"),
		request.PathValue("language"),
		ui_textdomain.Platform(request.PathValue("platform")),
		packageVersion,
	)
	if err != nil {
		writeUITextError(response, request, err)
		return
	}
	writeSuccess(response, request, http.StatusOK, map[string]any{
		"package": detail.Package,
		"entries": detail.Entries,
	})
}

func (handler uiTextHandler) createDraft(
	response http.ResponseWriter,
	request *http.Request,
) {
	accountID, ok := authenticatedAccountID(request)
	if !ok {
		writeError(response, request, http.StatusUnauthorized, "unauthenticated", "请重新登录")
		return
	}
	var payload uiTextDraftRequest
	if err := decodeJSON(request, &payload); err != nil {
		writeError(response, request, http.StatusBadRequest, "invalid_request", "请检查文案内容")
		return
	}
	detail, err := handler.service.CreateDraft(
		request.Context(),
		uiTextDraftInput(payload),
		accountID,
	)
	if err != nil {
		writeUITextError(response, request, err)
		return
	}
	writeSuccess(response, request, http.StatusCreated, map[string]any{
		"package": detail.Package,
		"entries": detail.Entries,
	})
}

func (handler uiTextHandler) updateDraft(
	response http.ResponseWriter,
	request *http.Request,
) {
	accountID, ok := authenticatedAccountID(request)
	if !ok {
		writeError(response, request, http.StatusUnauthorized, "unauthenticated", "请重新登录")
		return
	}
	packageVersion, err := uiTextVersionParam(request)
	if err != nil {
		writeError(
			response,
			request,
			http.StatusUnprocessableEntity,
			"invalid_version",
			"请检查文案版本号",
		)
		return
	}
	var payload uiTextDraftRequest
	if err := decodeJSON(request, &payload); err != nil {
		writeError(response, request, http.StatusBadRequest, "invalid_request", "请检查文案内容")
		return
	}
	// Path identity is authoritative: a path that disagrees with the body is a
	// client bug, so the body identity is replaced rather than trusted.
	payload.PackageID = request.PathValue("package_id")
	payload.Language = request.PathValue("language")
	payload.Platform = request.PathValue("platform")
	detail, err := handler.service.UpdateDraft(
		request.Context(),
		uiTextDraftInput(payload),
		packageVersion,
		accountID,
	)
	if err != nil {
		writeUITextError(response, request, err)
		return
	}
	writeSuccess(response, request, http.StatusOK, map[string]any{
		"package": detail.Package,
		"entries": detail.Entries,
	})
}

func (handler uiTextHandler) submit(
	response http.ResponseWriter,
	request *http.Request,
) {
	handler.applyAction(response, request, ui_textdomain.ActionSubmit)
}

func (handler uiTextHandler) approve(
	response http.ResponseWriter,
	request *http.Request,
) {
	handler.applyAction(response, request, ui_textdomain.ActionApprove)
}

func (handler uiTextHandler) reject(
	response http.ResponseWriter,
	request *http.Request,
) {
	handler.applyAction(response, request, ui_textdomain.ActionReject)
}

func (handler uiTextHandler) publish(
	response http.ResponseWriter,
	request *http.Request,
) {
	handler.applyAction(response, request, ui_textdomain.ActionPublish)
}

func (handler uiTextHandler) withdraw(
	response http.ResponseWriter,
	request *http.Request,
) {
	handler.applyAction(response, request, ui_textdomain.ActionWithdraw)
}

func (handler uiTextHandler) applyAction(
	response http.ResponseWriter,
	request *http.Request,
	action ui_textdomain.Action,
) {
	accountID, ok := authenticatedAccountID(request)
	if !ok {
		writeError(response, request, http.StatusUnauthorized, "unauthenticated", "请重新登录")
		return
	}
	packageVersion, err := uiTextVersionParam(request)
	if err != nil {
		writeError(
			response,
			request,
			http.StatusUnprocessableEntity,
			"invalid_version",
			"请检查文案版本号",
		)
		return
	}
	transition := ui_textdomain.TransitionInput{
		PackageID:      request.PathValue("package_id"),
		Language:       request.PathValue("language"),
		Platform:       ui_textdomain.Platform(request.PathValue("platform")),
		PackageVersion: packageVersion,
		ActorID:        accountID,
	}
	var result *ui_textdomain.Package
	switch action {
	case ui_textdomain.ActionSubmit:
		result, err = handler.service.Submit(request.Context(), transition)
	case ui_textdomain.ActionApprove:
		result, err = handler.service.Approve(request.Context(), transition)
	case ui_textdomain.ActionReject:
		var payload uiTextReviewRequest
		if decodeErr := decodeJSON(request, &payload); decodeErr != nil {
			writeError(
				response,
				request,
				http.StatusBadRequest,
				"invalid_request",
				"请填写审核不通过原因",
			)
			return
		}
		result, err = handler.service.Reject(request.Context(), transition, payload.Reason)
	case ui_textdomain.ActionPublish:
		var payload uiTextPublishRequest
		if decodeErr := decodeJSON(request, &payload); decodeErr != nil {
			writeError(
				response,
				request,
				http.StatusBadRequest,
				"invalid_request",
				"请提供有效的签名信息",
			)
			return
		}
		result, err = handler.service.Publish(
			request.Context(),
			transition,
			ui_textdomain.PublicationInput{
				SignatureKeyID: payload.SignatureKeyID,
				Signature:      payload.Signature,
			},
		)
	case ui_textdomain.ActionWithdraw:
		result, err = handler.service.Withdraw(request.Context(), transition)
	default:
		err = ui_textdomain.ErrInvalidTransition
	}
	if err != nil {
		writeUITextError(response, request, err)
		return
	}
	writeSuccess(response, request, http.StatusOK, map[string]any{
		"package": result,
	})
}

// published serves the incremental client pull. A client sends the revision it
// already holds and receives both new publications and withdrawals newer than
// that revision, so additions and removals converge through one cursor.
func (handler uiTextHandler) published(
	response http.ResponseWriter,
	request *http.Request,
) {
	sinceRevision, err := uiTextRevisionParam(request)
	if err != nil {
		writeError(
			response,
			request,
			http.StatusUnprocessableEntity,
			"invalid_revision",
			"请检查文案版本号",
		)
		return
	}
	limit, err := uiTextLimitParam(request)
	if err != nil {
		writeError(
			response,
			request,
			http.StatusUnprocessableEntity,
			"invalid_limit",
			"请检查分批数量",
		)
		return
	}
	page, err := handler.service.Published(request.Context(), sinceRevision, limit)
	if err != nil {
		writeUITextError(response, request, err)
		return
	}
	writeSuccess(response, request, http.StatusOK, page)
}

func (handler uiTextHandler) publishedPackage(
	response http.ResponseWriter,
	request *http.Request,
) {
	detail, err := handler.service.PublishedPackage(
		request.Context(),
		request.PathValue("package_id"),
		request.PathValue("language"),
		ui_textdomain.Platform(request.PathValue("platform")),
	)
	if err != nil {
		writeUITextError(response, request, err)
		return
	}
	writeSuccess(response, request, http.StatusOK, map[string]any{
		"package": detail.Package,
		"entries": detail.Entries,
	})
}

func (handler deviceUITextHandler) published(
	response http.ResponseWriter,
	request *http.Request,
) {
	if _, ok := handler.verifyDevice(response, request); !ok {
		return
	}
	handler.handler.published(response, request)
}

func (handler deviceUITextHandler) publishedPackage(
	response http.ResponseWriter,
	request *http.Request,
) {
	if _, ok := handler.verifyDevice(response, request); !ok {
		return
	}
	handler.handler.publishedPackage(response, request)
}

func (handler deviceUITextHandler) verifyDevice(
	response http.ResponseWriter,
	request *http.Request,
) (string, bool) {
	if handler.bindingService == nil {
		writeError(
			response,
			request,
			http.StatusUnauthorized,
			"device_session_expired",
			"设备登录已过期，请重新连接",
		)
		return "", false
	}
	deviceSessionToken, ok := bearerToken(request)
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
	pathDeviceID := strings.TrimSpace(request.PathValue("device_id"))
	sessionDeviceID, err := handler.bindingService.VerifyDeviceSession(
		request.Context(),
		deviceSessionToken,
	)
	if err != nil || sessionDeviceID != pathDeviceID {
		writeError(
			response,
			request,
			http.StatusUnauthorized,
			"device_session_expired",
			"设备登录已过期，请重新连接",
		)
		return "", false
	}
	return pathDeviceID, true
}

func uiTextDraftInput(payload uiTextDraftRequest) ui_textdomain.DraftInput {
	entries := make([]ui_textdomain.Entry, 0, len(payload.Entries))
	for _, entry := range payload.Entries {
		entries = append(entries, ui_textdomain.Entry{
			UITextKey: entry.UITextKey,
			Value:     entry.Value,
			MaxLength: entry.MaxLength,
			Variables: entry.Variables,
		})
	}
	return ui_textdomain.DraftInput{
		PackageID:        payload.PackageID,
		Language:         payload.Language,
		Platform:         ui_textdomain.Platform(payload.Platform),
		MinClientVersion: payload.MinClientVersion,
		Entries:          entries,
	}
}

func uiTextVersionParam(request *http.Request) (int, error) {
	value := strings.TrimSpace(request.PathValue("package_version"))
	version, err := strconv.Atoi(value)
	if err != nil || version < 1 {
		return 0, ui_textdomain.ErrInvalidPackage
	}
	return version, nil
}

func uiTextRevisionParam(request *http.Request) (int64, error) {
	value := strings.TrimSpace(request.URL.Query().Get("since_revision"))
	if value == "" {
		return 0, nil
	}
	revision, err := strconv.ParseInt(value, 10, 64)
	if err != nil || revision < 0 {
		return 0, ui_textdomain.ErrInvalidPackage
	}
	return revision, nil
}

func uiTextLimitParam(request *http.Request) (int, error) {
	value := strings.TrimSpace(request.URL.Query().Get("limit"))
	if value == "" {
		return 0, nil
	}
	limit, err := strconv.Atoi(value)
	if err != nil || limit < 1 {
		return 0, ui_textdomain.ErrInvalidPackage
	}
	return limit, nil
}

func uiTextPageParams(request *http.Request) (int, int, error) {
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

func writeUITextError(
	response http.ResponseWriter,
	request *http.Request,
	err error,
) {
	switch {
	case errors.Is(err, ui_textdomain.ErrInvalidPackage),
		errors.Is(err, ui_textdomain.ErrInvalidEntry):
		writeError(
			response,
			request,
			http.StatusUnprocessableEntity,
			"invalid_ui_text",
			"请检查文案内容和格式",
		)
	case errors.Is(err, ui_textdomain.ErrSignatureRequired):
		writeError(
			response,
			request,
			http.StatusUnprocessableEntity,
			"signature_required",
			"发布文案需要有效的签名",
		)
	case errors.Is(err, ui_textdomain.ErrSignatureUnknownKey):
		writeError(
			response,
			request,
			http.StatusUnprocessableEntity,
			"signature_key_unknown",
			"签名密钥未配置或不可信",
		)
	case errors.Is(err, ui_textdomain.ErrSignatureInvalid):
		writeError(
			response,
			request,
			http.StatusUnprocessableEntity,
			"signature_invalid",
			"文案签名校验失败",
		)
	case errors.Is(err, ui_textdomain.ErrPackageNotFound):
		writeError(
			response,
			request,
			http.StatusNotFound,
			"ui_text_not_found",
			"没有找到这个文案包",
		)
	case errors.Is(err, ui_textdomain.ErrVersionExists):
		writeError(
			response,
			request,
			http.StatusConflict,
			"ui_text_exists",
			"这个文案版本已经存在",
		)
	case errors.Is(err, ui_textdomain.ErrInvalidTransition):
		writeError(
			response,
			request,
			http.StatusConflict,
			"invalid_ui_text_state",
			"当前状态不能执行这个操作",
		)
	case errors.Is(err, ui_textdomain.ErrReviewRequired):
		writeError(
			response,
			request,
			http.StatusConflict,
			"review_required",
			"文案还没有通过审核",
		)
	case errors.Is(err, ui_textdomain.ErrReasonRequired):
		writeError(
			response,
			request,
			http.StatusUnprocessableEntity,
			"reason_required",
			"请填写审核不通过原因",
		)
	case errors.Is(err, ui_textdomain.ErrRevisionAhead):
		writeError(
			response,
			request,
			http.StatusBadRequest,
			"revision_ahead",
			"请重新获取文案清单",
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
