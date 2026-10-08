package http

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
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
	update, err := handler.operationsService.AppUpdate(
		request.Context(),
		platform,
		channel,
		currentVersion,
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
	Settings operationsdomain.Settings `json:"settings"`
	Version  int64                     `json:"version"`
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
	writeSuccess(response, request, http.StatusOK, settingsResponse{
		Settings: *settings,
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
	settings, version, err := handler.operationsService.UpdateSettings(
		request.Context(),
		body.Settings,
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
		Settings: *settings,
		Version:  version,
	})
}

type settingsUpdateBody struct {
	Settings        *operationsdomain.Settings
	ExpectedVersion int64
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
	accounts, err := handler.operationsService.ListFamilyAccounts(
		request.Context(),
	)
	if err != nil {
		writeError(response, request, http.StatusInternalServerError, "service_error", "暂时无法读取家长账号")
		return
	}
	writeSuccess(response, request, http.StatusOK, map[string]any{
		"accounts": accounts,
	})
}

func (handler adminHandler) listReleases(
	response http.ResponseWriter,
	request *http.Request,
) {
	releases, err := handler.operationsService.ListReleases(request.Context())
	if err != nil {
		writeError(response, request, http.StatusInternalServerError, "service_error", "暂时无法读取更新记录")
		return
	}
	writeSuccess(response, request, http.StatusOK, map[string]any{
		"releases": releases,
	})
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
			writeError(response, request, http.StatusConflict, "release_exists", "这个版本已经存在")
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
	version := strings.TrimSpace(request.PathValue("version"))
	if err := handler.operationsService.PublishRelease(
		request.Context(),
		version,
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
	version := strings.TrimSpace(request.PathValue("version"))
	if err := handler.operationsService.DeleteRelease(
		request.Context(),
		version,
	); err != nil {
		if errors.Is(err, operationsdomain.ErrReleaseNotFound) {
			writeError(response, request, http.StatusNotFound, "release_not_found", "没有找到这个更新版本")
			return
		}
		writeError(response, request, http.StatusInternalServerError, "service_error", "更新记录没有删除，请稍后重试")
		return
	}
	writeSuccess(response, request, http.StatusOK, map[string]any{"deleted": true})
}
