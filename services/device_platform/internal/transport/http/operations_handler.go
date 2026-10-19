package http

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"

	operationsdomain "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/operations/domain"
)

func (handler adminHandler) appUpdate(
	response http.ResponseWriter,
	request *http.Request,
) {
	platform := strings.TrimSpace(request.URL.Query().Get("platform"))
	channel := strings.TrimSpace(request.URL.Query().Get("channel"))
	currentVersion := strings.TrimSpace(request.URL.Query().Get("current_version"))
	currentResourceVersion := strings.TrimSpace(
		request.URL.Query().Get("current_resource_version"),
	)
	update, err := handler.operationsService.AppUpdate(
		request.Context(),
		platform,
		channel,
		currentVersion,
		currentResourceVersion,
	)
	if err != nil {
		if errors.Is(err, operationsdomain.ErrUpdateNotAvailable) ||
			errors.Is(err, operationsdomain.ErrReleaseNotFound) {
			response.WriteHeader(http.StatusNoContent)
			return
		}
		writeError(response, request, http.StatusInternalServerError, "service_error", "暂时无法检查更新")
		return
	}
	writeSuccess(response, request, http.StatusOK, update)
}

type settingsResponse struct {
	Settings adminSettingsResponse `json:"settings"`
	Version  int64                 `json:"version"`
}

// adminSettingsResponse separates read-only safety invariants from editable
// settings. The nested safety object is the update DTO and therefore omits
// minor mode, making it impossible for an older control surface to mistake
// the protection for an operator switch.
type adminSettingsResponse struct {
	AI        operationsdomain.AISettings        `json:"ai"`
	Account   operationsdomain.AccountSettings   `json:"account"`
	Safety    adminSafetySettingsResponse        `json:"safety"`
	Voice     operationsdomain.VoiceSettings     `json:"voice"`
	Update    operationsdomain.UpdateSettings    `json:"update"`
	Retention operationsdomain.RetentionSettings `json:"retention"`
}

type adminSafetySettingsResponse struct {
	MinorModeDefault          bool `json:"minor_mode_default"`
	OutputModerationEnabled   bool `json:"output_moderation_enabled"`
	CrisisInterventionEnabled bool `json:"crisis_intervention_enabled"`
	AllowAudioUpload          bool `json:"allow_audio_upload"`
	AllowImageUpload          bool `json:"allow_image_upload"`
}

func newAdminSettingsResponse(
	settings *operationsdomain.Settings,
) adminSettingsResponse {
	safety := settings.Safety
	safety.MinorModeDefault = true
	return adminSettingsResponse{
		AI:      settings.AI,
		Account: settings.Account,
		Safety: adminSafetySettingsResponse{
			MinorModeDefault:          true,
			OutputModerationEnabled:   safety.OutputModerationEnabled,
			CrisisInterventionEnabled: safety.CrisisInterventionEnabled,
			AllowAudioUpload:          safety.AllowAudioUpload,
			AllowImageUpload:          safety.AllowImageUpload,
		},
		Voice:     settings.Voice,
		Update:    settings.Update,
		Retention: settings.Retention,
	}
}

type createReleaseRequest struct {
	Version             string `json:"version"`
	Channel             string `json:"channel"`
	Kind                string `json:"kind"`
	Platform            string `json:"platform"`
	DownloadURL         string `json:"download_url"`
	SHA256              string `json:"sha256"`
	ReleaseNotes        string `json:"release_notes"`
	IsMandatory         bool   `json:"is_mandatory"`
	MinSupportedVersion string `json:"min_supported_version"`
}

func (handler adminHandler) getSettings(
	response http.ResponseWriter,
	request *http.Request,
) {
	settings, version, err := handler.operationsService.Settings(request.Context())
	if err != nil {
		if errors.Is(err, operationsdomain.ErrSettingsNotFound) {
			writeError(response, request, http.StatusNotFound, "settings_not_found", "还没有可用的系统设置")
			return
		}
		writeError(response, request, http.StatusInternalServerError, "service_error", "暂时无法读取系统设置")
		return
	}
	if settings.Account.GuardianConsentVersion == "" {
		settings.Account.GuardianConsentVersion =
			operationsdomain.DefaultGuardianConsentVersion
	}
	writeSuccess(response, request, http.StatusOK, settingsResponse{
		Settings: newAdminSettingsResponse(settings),
		Version:  version,
	})
}

func (handler adminHandler) updateSettings(
	response http.ResponseWriter,
	request *http.Request,
) {
	accountID, ok := authenticatedAccountID(request)
	if !ok {
		writeError(response, request, http.StatusUnauthorized, "unauthenticated", "请重新登录")
		return
	}
	body, err := decodeSettingsUpdateRequest(request)
	if err != nil {
		if errors.Is(err, errMissingSettingsEnvelope) {
			writeError(
				response,
				request,
				http.StatusBadRequest,
				"invalid_request",
				"请升级管理界面后重新提交设置",
			)
			return
		}
		writeError(response, request, http.StatusBadRequest, "invalid_request", "请检查填写的内容")
		return
	}
	settingsInput, err := body.Settings.settings()
	if err != nil {
		writeError(response, request, http.StatusUnprocessableEntity, "invalid_settings", "请检查设置内容")
		return
	}
	settings, version, err := handler.operationsService.UpdateSettings(
		request.Context(),
		settingsInput,
		accountID,
		body.ExpectedVersion,
	)
	if err != nil {
		if errors.Is(err, operationsdomain.ErrInvalidSettings) {
			writeError(response, request, http.StatusUnprocessableEntity, "invalid_settings", "请检查设置内容")
			return
		}
		if errors.Is(err, operationsdomain.ErrSettingsVersionConflict) {
			writeError(
				response,
				request,
				http.StatusConflict,
				"settings_version_conflict",
				"系统设置已被其他管理员更新，请刷新后重试",
			)
			return
		}
		writeError(response, request, http.StatusInternalServerError, "service_error", "设置没有保存，请稍后重试")
		return
	}
	writeSuccess(response, request, http.StatusOK, settingsResponse{
		Settings: newAdminSettingsResponse(settings),
		Version:  version,
	})
}

type settingsUpdateBody struct {
	Settings        *adminSettingsUpdateRequest
	ExpectedVersion int64
}

// adminSettingsUpdateRequest accepts the writable settings document. Minor
// mode is deliberately absent; the decoder also accepts its legacy true value
// below so a rolling deployment with an old console stays compatible.
type adminSettingsUpdateRequest struct {
	AI        operationsdomain.AISettings        `json:"ai"`
	Account   operationsdomain.AccountSettings   `json:"account"`
	Safety    adminSafetySettingsUpdateRequest   `json:"safety"`
	Voice     operationsdomain.VoiceSettings     `json:"voice"`
	Update    operationsdomain.UpdateSettings    `json:"update"`
	Retention operationsdomain.RetentionSettings `json:"retention"`
}

type adminSafetySettingsUpdateRequest struct {
	MinorModeDefault          *bool `json:"minor_mode_default"`
	OutputModerationEnabled   bool  `json:"output_moderation_enabled"`
	CrisisInterventionEnabled bool  `json:"crisis_intervention_enabled"`
	AllowAudioUpload          bool  `json:"allow_audio_upload"`
	AllowImageUpload          bool  `json:"allow_image_upload"`
}

func (request adminSettingsUpdateRequest) settings() (*operationsdomain.Settings, error) {
	if request.Safety.MinorModeDefault != nil &&
		!*request.Safety.MinorModeDefault {
		return nil, operationsdomain.ErrInvalidSettings
	}
	if request.Retention.AudioDays != 0 {
		return nil, operationsdomain.ErrInvalidSettings
	}
	if request.Safety.AllowAudioUpload || request.Safety.AllowImageUpload {
		return nil, operationsdomain.ErrInvalidSettings
	}
	return &operationsdomain.Settings{
		AI:      request.AI,
		Account: request.Account,
		Safety: operationsdomain.SafetySettings{
			MinorModeDefault:          true,
			OutputModerationEnabled:   request.Safety.OutputModerationEnabled,
			CrisisInterventionEnabled: request.Safety.CrisisInterventionEnabled,
			AllowAudioUpload:          request.Safety.AllowAudioUpload,
			AllowImageUpload:          request.Safety.AllowImageUpload,
		},
		Voice:     request.Voice,
		Update:    request.Update,
		Retention: request.Retention,
	}, nil
}

var errMissingSettingsEnvelope = errors.New("missing settings envelope")

func decodeSettingsUpdateRequest(request *http.Request) (*settingsUpdateBody, error) {
	raw := make(map[string]json.RawMessage)
	if err := json.NewDecoder(request.Body).Decode(&raw); err != nil {
		return nil, err
	}
	settingsRaw, hasSettings := raw["settings"]
	expectedVersionRaw, hasExpectedVersion := raw["expected_version"]
	if !hasSettings || !hasExpectedVersion ||
		bytes.Equal(bytes.TrimSpace(settingsRaw), []byte("null")) ||
		bytes.Equal(bytes.TrimSpace(expectedVersionRaw), []byte("null")) {
		return nil, errMissingSettingsEnvelope
	}
	if len(raw) != 2 {
		return nil, errors.New("unexpected settings update field")
	}
	var payload settingsUpdateBody
	settingsDecoder := json.NewDecoder(bytes.NewReader(settingsRaw))
	settingsDecoder.DisallowUnknownFields()
	if err := settingsDecoder.Decode(&payload.Settings); err != nil {
		return nil, err
	}
	if err := json.Unmarshal(expectedVersionRaw, &payload.ExpectedVersion); err != nil {
		return nil, err
	}
	return &payload, nil
}

func (handler adminHandler) getOverview(
	response http.ResponseWriter,
	request *http.Request,
) {
	overview, err := handler.operationsService.Overview(request.Context())
	if err != nil {
		writeError(response, request, http.StatusInternalServerError, "service_error", "暂时无法读取运营数据")
		return
	}
	writeSuccess(response, request, http.StatusOK, overview)
}

func (handler adminHandler) listFamilies(
	response http.ResponseWriter,
	request *http.Request,
) {
	page, pageSize, ok := managementPageQuery(response, request)
	if !ok {
		return
	}
	result, err := handler.operationsService.ListFamilyAccounts(
		request.Context(),
		page,
		pageSize,
	)
	if err != nil {
		writeError(response, request, http.StatusInternalServerError, "service_error", "暂时无法读取家长账号")
		return
	}
	writeSuccess(response, request, http.StatusOK, result)
}

func (handler adminHandler) listReleases(
	response http.ResponseWriter,
	request *http.Request,
) {
	page, pageSize, ok := managementPageQuery(response, request)
	if !ok {
		return
	}
	result, err := handler.operationsService.ListReleases(
		request.Context(),
		page,
		pageSize,
	)
	if err != nil {
		writeError(response, request, http.StatusInternalServerError, "service_error", "暂时无法读取更新记录")
		return
	}
	writeSuccess(response, request, http.StatusOK, result)
}

// managementPageQuery validates both pagination fields together so a typo
// never silently falls back to page one. Omitted values use the shared
// management defaults; the service still clamps for non-HTTP callers.
func managementPageQuery(
	response http.ResponseWriter,
	request *http.Request,
) (int, int, bool) {
	page, err := queryPositiveInt(
		request.URL.Query().Get("page"),
		1,
	)
	if err != nil {
		writeError(response, request, http.StatusBadRequest, "invalid_pagination", "请检查页码")
		return 0, 0, false
	}
	pageSize, err := queryPositiveInt(
		request.URL.Query().Get("page_size"),
		operationsdomain.DefaultListPageSize,
	)
	if err != nil || pageSize > operationsdomain.MaxListPageSize {
		writeError(response, request, http.StatusBadRequest, "invalid_pagination", "每页数量需要在 1 到 100 之间")
		return 0, 0, false
	}
	return page, pageSize, true
}

func queryPositiveInt(value string, fallback int) (int, error) {
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

func (handler adminHandler) createRelease(
	response http.ResponseWriter,
	request *http.Request,
) {
	var payload createReleaseRequest
	if err := decodeJSON(request, &payload); err != nil {
		writeError(response, request, http.StatusBadRequest, "invalid_request", "请检查更新信息")
		return
	}
	release, err := handler.operationsService.CreateRelease(
		request.Context(),
		operationsdomain.ReleaseInput{
			Version:             payload.Version,
			Channel:             payload.Channel,
			Kind:                payload.Kind,
			Platform:            payload.Platform,
			DownloadURL:         payload.DownloadURL,
			SHA256:              payload.SHA256,
			ReleaseNotes:        payload.ReleaseNotes,
			IsMandatory:         payload.IsMandatory,
			MinSupportedVersion: payload.MinSupportedVersion,
		},
	)
	if err != nil {
		if errors.Is(err, operationsdomain.ErrReleaseAlreadyExists) {
			writeError(response, request, http.StatusConflict, "release_exists", "同版本、渠道、类型和平台的更新已经登记")
			return
		}
		if errors.Is(err, operationsdomain.ErrInvalidSettings) {
			writeError(response, request, http.StatusUnprocessableEntity, "invalid_release", "请检查版本号、下载地址和校验值")
			return
		}
		writeError(response, request, http.StatusInternalServerError, "service_error", "更新记录没有保存，请稍后重试")
		return
	}
	writeSuccess(response, request, http.StatusCreated, release)
}

func (handler adminHandler) publishRelease(
	response http.ResponseWriter,
	request *http.Request,
) {
	accountID, ok := authenticatedAccountID(request)
	if !ok {
		writeError(response, request, http.StatusUnauthorized, "unauthenticated", "请重新登录")
		return
	}
	releaseID := strings.TrimSpace(request.PathValue("release_id"))
	if err := handler.operationsService.PublishRelease(
		request.Context(),
		releaseID,
		accountID,
	); err != nil {
		if errors.Is(err, operationsdomain.ErrReleaseNotPublishable) {
			writeError(response, request, http.StatusConflict, "release_not_publishable", "这个版本已经发布或无法发布")
			return
		}
		writeError(response, request, http.StatusInternalServerError, "service_error", "更新没有发布，请稍后重试")
		return
	}
	writeSuccess(response, request, http.StatusOK, map[string]any{"published": true})
}

func (handler adminHandler) deleteRelease(
	response http.ResponseWriter,
	request *http.Request,
) {
	releaseID := strings.TrimSpace(request.PathValue("release_id"))
	if err := handler.operationsService.DeleteRelease(
		request.Context(),
		releaseID,
	); err != nil {
		if errors.Is(err, operationsdomain.ErrReleaseNotFound) {
			writeError(response, request, http.StatusNotFound, "release_not_found", "没有找到这条更新记录")
			return
		}
		writeError(response, request, http.StatusInternalServerError, "service_error", "更新记录没有删除，请稍后重试")
		return
	}
	writeSuccess(response, request, http.StatusOK, map[string]any{"deleted": true})
}
