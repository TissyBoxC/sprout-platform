package http

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"

	contentdomain "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/content/domain"
	contentservice "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/content/service"
)

type contentHandler struct {
	service *contentservice.Service
}

// deviceContentHandler wraps the content read surface with device-session
// verification. Firmware devices authenticate with a short-lived session
// token rather than a guardian credential, and the path device id must match
// the session owner so one device cannot read another device's manifest.
type deviceContentHandler struct {
	contentHandler contentHandler
	bindingService deviceContentBindingService
}

type deviceContentBindingService interface {
	VerifyDeviceSession(ctx context.Context, deviceSessionToken string) (string, error)
}

type createContentPackageRequest struct {
	PackageID string   `json:"package_id"`
	Title     string   `json:"title"`
	Category  string   `json:"category"`
	AgeTiers  []string `json:"age_tiers"`
	AssetKey  string   `json:"asset_key"`
	SHA256    string   `json:"sha256"`
	SizeBytes int64    `json:"size_bytes"`
}

type updateContentPackageRequest struct {
	Title     string   `json:"title"`
	Category  string   `json:"category"`
	AgeTiers  []string `json:"age_tiers"`
	AssetKey  string   `json:"asset_key"`
	SHA256    string   `json:"sha256"`
	SizeBytes int64    `json:"size_bytes"`
}

type contentDecisionRequest struct {
	Reason string `json:"reason"`
}

func (handler contentHandler) listPackages(
	response http.ResponseWriter,
	request *http.Request,
) {
	page, pageSize, err := contentPageParams(request)
	if err != nil {
		writeError(response, request, http.StatusUnprocessableEntity, "invalid_page", "请检查分页参数")
		return
	}
	result, err := handler.service.List(request.Context(), contentdomain.PackageListFilter{
		Category: strings.TrimSpace(request.URL.Query().Get("category")),
		AgeTier:  strings.TrimSpace(request.URL.Query().Get("age_tier")),
		Status:   strings.TrimSpace(request.URL.Query().Get("status")),
		Keyword:  strings.TrimSpace(request.URL.Query().Get("keyword")),
		Page:     page,
		PageSize: pageSize,
	})
	if err != nil {
		writeContentError(response, request, err)
		return
	}
	writeSuccess(response, request, http.StatusOK, result)
}

func (handler contentHandler) createPackage(
	response http.ResponseWriter,
	request *http.Request,
) {
	var payload createContentPackageRequest
	if err := decodeJSON(request, &payload); err != nil {
		writeError(response, request, http.StatusBadRequest, "invalid_request", "请检查内容包信息")
		return
	}
	created, err := handler.service.CreateDraft(
		request.Context(),
		contentdomain.MetadataInput{
			PackageID: payload.PackageID,
			Title:     payload.Title,
			Category:  contentdomain.Category(payload.Category),
			AgeTiers:  contentAgeTiers(payload.AgeTiers),
			AssetKey:  payload.AssetKey,
			SHA256:    payload.SHA256,
			SizeBytes: payload.SizeBytes,
		},
	)
	if err != nil {
		writeContentError(response, request, err)
		return
	}
	writeSuccess(response, request, http.StatusCreated, map[string]any{
		"package": created,
	})
}

func (handler contentHandler) createPackageVersion(
	response http.ResponseWriter,
	request *http.Request,
) {
	var payload updateContentPackageRequest
	if err := decodeJSON(request, &payload); err != nil {
		writeError(response, request, http.StatusBadRequest, "invalid_request", "请检查内容包信息")
		return
	}
	packageID := request.PathValue("package_id")
	created, err := handler.service.CreateVersion(
		request.Context(),
		packageID,
		contentdomain.MetadataInput{
			PackageID: packageID,
			Title:     payload.Title,
			Category:  contentdomain.Category(payload.Category),
			AgeTiers:  contentAgeTiers(payload.AgeTiers),
			AssetKey:  payload.AssetKey,
			SHA256:    payload.SHA256,
			SizeBytes: payload.SizeBytes,
		},
	)
	if err != nil {
		writeContentError(response, request, err)
		return
	}
	writeSuccess(response, request, http.StatusCreated, map[string]any{
		"package": created,
	})
}

func (handler contentHandler) getPackage(
	response http.ResponseWriter,
	request *http.Request,
) {
	detail, err := handler.service.GetDetail(
		request.Context(),
		request.PathValue("package_id"),
	)
	if err != nil {
		writeContentError(response, request, err)
		return
	}
	writeSuccess(response, request, http.StatusOK, detail)
}

func (handler contentHandler) updatePackage(
	response http.ResponseWriter,
	request *http.Request,
) {
	packageVersion, err := contentVersionParam(request)
	if err != nil {
		writeError(response, request, http.StatusUnprocessableEntity, "invalid_version", "请检查内容包版本")
		return
	}
	var payload updateContentPackageRequest
	if err := decodeJSON(request, &payload); err != nil {
		writeError(response, request, http.StatusBadRequest, "invalid_request", "请检查内容包信息")
		return
	}
	updated, err := handler.service.UpdateDraft(
		request.Context(),
		request.PathValue("package_id"),
		packageVersion,
		contentdomain.MetadataInput{
			PackageID: request.PathValue("package_id"),
			Title:     payload.Title,
			Category:  contentdomain.Category(payload.Category),
			AgeTiers:  contentAgeTiers(payload.AgeTiers),
			AssetKey:  payload.AssetKey,
			SHA256:    payload.SHA256,
			SizeBytes: payload.SizeBytes,
		},
	)
	if err != nil {
		writeContentError(response, request, err)
		return
	}
	writeSuccess(response, request, http.StatusOK, map[string]any{
		"package": updated,
	})
}

func (handler contentHandler) submitPackage(
	response http.ResponseWriter,
	request *http.Request,
) {
	handler.applyAction(response, request, contentdomain.ActionSubmit)
}

func (handler contentHandler) approvePackage(
	response http.ResponseWriter,
	request *http.Request,
) {
	handler.applyAction(response, request, contentdomain.ActionApprove)
}

func (handler contentHandler) rejectPackage(
	response http.ResponseWriter,
	request *http.Request,
) {
	handler.applyAction(response, request, contentdomain.ActionReject)
}

func (handler contentHandler) publishPackage(
	response http.ResponseWriter,
	request *http.Request,
) {
	handler.applyAction(response, request, contentdomain.ActionPublish)
}

func (handler contentHandler) withdrawPackage(
	response http.ResponseWriter,
	request *http.Request,
) {
	handler.applyAction(response, request, contentdomain.ActionWithdraw)
}

func (handler contentHandler) archivePackage(
	response http.ResponseWriter,
	request *http.Request,
) {
	handler.applyAction(response, request, contentdomain.ActionArchive)
}

func (handler contentHandler) catalog(
	response http.ResponseWriter,
	request *http.Request,
) {
	sinceRevision, err := contentRevisionParam(request)
	if err != nil {
		writeError(response, request, http.StatusUnprocessableEntity, "invalid_revision", "请检查内容版本号")
		return
	}
	catalog, err := handler.service.Catalog(
		request.Context(),
		contentdomain.CatalogQuery{
			SinceRevision: sinceRevision,
			AgeTier:       strings.TrimSpace(request.URL.Query().Get("age_tier")),
			Category:      strings.TrimSpace(request.URL.Query().Get("category")),
		},
	)
	if err != nil {
		writeContentError(response, request, err)
		return
	}
	writeSuccess(response, request, http.StatusOK, catalog)
}

func (handler contentHandler) download(
	response http.ResponseWriter,
	request *http.Request,
) {
	info, err := handler.service.Download(
		request.Context(),
		request.PathValue("package_id"),
	)
	if err != nil {
		writeContentError(response, request, err)
		return
	}
	writeSuccess(response, request, http.StatusOK, map[string]any{
		"download": info,
	})
}

func (handler deviceContentHandler) catalog(
	response http.ResponseWriter,
	request *http.Request,
) {
	if !handler.verifyDeviceSession(response, request) {
		return
	}
	handler.contentHandler.catalog(response, request)
}

func (handler deviceContentHandler) download(
	response http.ResponseWriter,
	request *http.Request,
) {
	if !handler.verifyDeviceSession(response, request) {
		return
	}
	handler.contentHandler.download(response, request)
}

func (handler deviceContentHandler) verifyDeviceSession(
	response http.ResponseWriter,
	request *http.Request,
) bool {
	if handler.bindingService == nil {
		writeError(response, request, http.StatusUnauthorized, "device_session_expired", "设备登录已过期，请重新连接")
		return false
	}
	deviceSessionToken, ok := bearerToken(request)
	if !ok {
		writeError(response, request, http.StatusUnauthorized, "device_session_expired", "设备登录已过期，请重新连接")
		return false
	}
	pathDeviceID := strings.TrimSpace(request.PathValue("device_id"))
	sessionDeviceID, err := handler.bindingService.VerifyDeviceSession(
		request.Context(),
		deviceSessionToken,
	)
	if err != nil || sessionDeviceID != pathDeviceID {
		writeError(response, request, http.StatusUnauthorized, "device_session_expired", "设备登录已过期，请重新连接")
		return false
	}
	return true
}

func (handler contentHandler) applyAction(
	response http.ResponseWriter,
	request *http.Request,
	action contentdomain.Action,
) {
	packageVersion, err := contentVersionParam(request)
	if err != nil {
		writeError(response, request, http.StatusUnprocessableEntity, "invalid_version", "请检查内容包版本")
		return
	}
	accountID, ok := authenticatedAccountID(request)
	if !ok {
		writeError(response, request, http.StatusUnauthorized, "unauthenticated", "请重新登录")
		return
	}
	var payload contentDecisionRequest
	if action == contentdomain.ActionReject {
		if err := decodeJSON(request, &payload); err != nil {
			writeError(response, request, http.StatusBadRequest, "invalid_request", "请填写审核不通过原因")
			return
		}
	}
	var result *contentdomain.PackageVersion
	switch action {
	case contentdomain.ActionSubmit:
		result, err = handler.service.Submit(
			request.Context(),
			request.PathValue("package_id"),
			packageVersion,
			accountID,
		)
	case contentdomain.ActionApprove:
		result, err = handler.service.Approve(
			request.Context(),
			request.PathValue("package_id"),
			packageVersion,
			accountID,
			payload.Reason,
		)
	case contentdomain.ActionReject:
		result, err = handler.service.Reject(
			request.Context(),
			request.PathValue("package_id"),
			packageVersion,
			accountID,
			payload.Reason,
		)
	case contentdomain.ActionPublish:
		result, err = handler.service.Publish(
			request.Context(),
			request.PathValue("package_id"),
			packageVersion,
			accountID,
		)
	case contentdomain.ActionWithdraw:
		result, err = handler.service.Withdraw(
			request.Context(),
			request.PathValue("package_id"),
			packageVersion,
			accountID,
			payload.Reason,
		)
	case contentdomain.ActionArchive:
		result, err = handler.service.Archive(
			request.Context(),
			request.PathValue("package_id"),
			packageVersion,
			accountID,
		)
	default:
		err = contentdomain.ErrInvalidTransition
	}
	if err != nil {
		writeContentError(response, request, err)
		return
	}
	writeSuccess(response, request, http.StatusOK, map[string]any{
		"package": result,
	})
}

func contentPageParams(request *http.Request) (int, int, error) {
	page, err := contentPositiveInt(request.URL.Query().Get("page"), 1)
	if err != nil {
		return 0, 0, err
	}
	pageSize, err := contentPositiveInt(request.URL.Query().Get("page_size"), 20)
	if err != nil {
		return 0, 0, err
	}
	if pageSize > 100 {
		pageSize = 100
	}
	return page, pageSize, nil
}

func contentRevisionParam(request *http.Request) (int64, error) {
	value := strings.TrimSpace(request.URL.Query().Get("since_revision"))
	if value == "" {
		return 0, nil
	}
	revision, err := strconv.ParseInt(value, 10, 64)
	if err != nil || revision < 0 {
		return 0, contentdomain.ErrInvalidMetadata
	}
	return revision, nil
}

func contentVersionParam(request *http.Request) (int, error) {
	value := strings.TrimSpace(request.PathValue("package_version"))
	version, err := strconv.Atoi(value)
	if err != nil || version < 1 {
		return 0, contentdomain.ErrInvalidMetadata
	}
	return version, nil
}

func contentPositiveInt(value string, fallback int) (int, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return fallback, nil
	}
	parsed, err := strconv.Atoi(value)
	if err != nil || parsed < 1 {
		return 0, contentdomain.ErrInvalidMetadata
	}
	return parsed, nil
}

func contentAgeTiers(values []string) []contentdomain.AgeTier {
	ageTiers := make([]contentdomain.AgeTier, 0, len(values))
	for _, value := range values {
		ageTiers = append(ageTiers, contentdomain.AgeTier(strings.TrimSpace(value)))
	}
	return ageTiers
}

func writeContentError(
	response http.ResponseWriter,
	request *http.Request,
	err error,
) {
	switch {
	case errors.Is(err, contentdomain.ErrInvalidMetadata):
		writeError(response, request, http.StatusUnprocessableEntity, "invalid_content", "请检查内容包信息")
	case errors.Is(err, contentdomain.ErrPackageNotFound):
		writeError(response, request, http.StatusNotFound, "content_not_found", "没有找到这个内容包")
	case errors.Is(err, contentdomain.ErrVersionExists):
		writeError(response, request, http.StatusConflict, "content_exists", "这个内容包已经存在")
	case errors.Is(err, contentdomain.ErrInvalidTransition):
		writeError(response, request, http.StatusConflict, "invalid_content_state", "当前状态不能执行这个操作")
	case errors.Is(err, contentdomain.ErrReviewRequired):
		writeError(response, request, http.StatusConflict, "review_required", "内容还没有通过审核")
	case errors.Is(err, contentdomain.ErrReasonRequired):
		writeError(response, request, http.StatusUnprocessableEntity, "reason_required", "请填写审核不通过原因")
	case errors.Is(err, contentdomain.ErrRevisionAhead):
		writeError(response, request, http.StatusBadRequest, "revision_ahead", "请重新获取内容清单")
	case errors.Is(err, contentdomain.ErrAssetNotFound):
		writeError(response, request, http.StatusNotFound, "asset_not_found", "没有找到内容文件")
	case errors.Is(err, contentdomain.ErrAssetChecksumMismatch):
		writeError(response, request, http.StatusConflict, "asset_changed", "内容文件已经变化，请重新审核")
	default:
		writeError(response, request, http.StatusInternalServerError, "service_error", "操作没有完成，请稍后重试")
	}
}
