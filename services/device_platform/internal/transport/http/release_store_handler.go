package http

import (
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/release_store/domain"
)

type refreshReleaseIndexRequest struct {
	Version     string `json:"version"`
	Channel     string `json:"channel"`
	Title       string `json:"title"`
	PublishedAt string `json:"published_at"`
}

func (handler adminHandler) listReleaseFiles(
	response http.ResponseWriter,
	request *http.Request,
) {
	inventory, err := handler.releaseStoreService.Inventory(request.Context())
	if err != nil {
		writeReleaseStoreError(response, request, err)
		return
	}
	writeSuccess(response, request, http.StatusOK, inventory)
}

func (handler adminHandler) getReleaseIndexStatus(
	response http.ResponseWriter,
	request *http.Request,
) {
	status, err := handler.releaseStoreService.IndexStatus(request.Context())
	if err != nil {
		writeReleaseStoreError(response, request, err)
		return
	}
	writeSuccess(response, request, http.StatusOK, status)
}

// uploadReleaseFile accepts multipart/form-data with fields "path" and
// "file". The request body is bounded before any file content is read.
func (handler adminHandler) uploadReleaseFile(
	response http.ResponseWriter,
	request *http.Request,
) {
	response = wrapStreamingResponseWriter(response)
	setLargeUploadDeadline(response)
	request.Body = http.MaxBytesReader(
		response,
		request.Body,
		domain.MaxUploadBytes+1<<20,
	)
	if err := request.ParseMultipartForm(32 << 20); err != nil {
		var maxBytesError *http.MaxBytesError
		if errors.As(err, &maxBytesError) {
			writeError(response, request, http.StatusRequestEntityTooLarge, "file_too_large", "上传文件过大")
			return
		}
		writeError(response, request, http.StatusBadRequest, "invalid_upload", "请重新选择要上传的文件")
		return
	}
	relativePath := strings.TrimSpace(request.FormValue("relative_path"))
	if relativePath == "" {
		relativePath = strings.TrimSpace(request.FormValue("path"))
	}
	overwrite, _ := strconv.ParseBool(strings.TrimSpace(request.FormValue("overwrite")))
	fileName := strings.TrimSpace(request.FormValue("filename"))
	if fileName == "" {
		fileName = strings.TrimSpace(request.FormValue("file_name"))
	}
	file, _, err := request.FormFile("file")
	if err != nil {
		writeError(response, request, http.StatusBadRequest, "missing_file", "请选择要上传的文件")
		return
	}
	defer file.Close()
	uploaded, err := handler.releaseStoreService.UploadFile(
		request.Context(),
		domain.UploadInput{
			RelativePath: relativePath,
			Overwrite:    overwrite,
			Version:      strings.TrimSpace(request.FormValue("version")),
			Channel:      strings.TrimSpace(request.FormValue("channel")),
			Platform:     strings.TrimSpace(request.FormValue("platform")),
			Kind:         strings.TrimSpace(request.FormValue("kind")),
			FileName:     fileName,
		},
		file,
	)
	if err != nil {
		var maxBytesError *http.MaxBytesError
		if errors.As(err, &maxBytesError) {
			writeError(response, request, http.StatusRequestEntityTooLarge, "file_too_large", "上传文件过大")
			return
		}
		writeReleaseStoreError(response, request, err)
		return
	}
	writeSuccess(response, request, http.StatusCreated, map[string]any{
		"file": uploaded,
	})
}

const largeReleaseUploadTimeout = 30 * time.Minute

// setLargeUploadDeadline widens only the release-upload connection deadline.
// The process-wide ReadTimeout stays short for ordinary API traffic.
func setLargeUploadDeadline(response http.ResponseWriter) {
	controller := http.NewResponseController(response)
	_ = controller.SetReadDeadline(time.Now().Add(largeReleaseUploadTimeout))
	_ = controller.SetWriteDeadline(time.Now().Add(largeReleaseUploadTimeout))
}

func (handler adminHandler) deleteReleaseFile(
	response http.ResponseWriter,
	request *http.Request,
) {
	relativePath := strings.TrimSpace(request.PathValue("path"))
	if relativePath == "" {
		relativePath = strings.TrimSpace(request.URL.Query().Get("path"))
	}
	if err := handler.releaseStoreService.DeleteFile(request.Context(), relativePath); err != nil {
		writeReleaseStoreError(response, request, err)
		return
	}
	writeSuccess(response, request, http.StatusOK, map[string]any{"deleted": true})
}

func (handler adminHandler) refreshReleaseIndex(
	response http.ResponseWriter,
	request *http.Request,
) {
	var payload refreshReleaseIndexRequest
	if err := decodeOptionalJSON(request, &payload); err != nil {
		writeError(response, request, http.StatusBadRequest, "invalid_request", "请检查版本信息")
		return
	}
	if strings.TrimSpace(payload.Version) == "" {
		// The UI refreshes every release it can discover. A full scan keeps
		// manual uploads and CI-published artifacts in one index.
		summary, err := handler.releaseStoreService.RefreshAllIndexes(request.Context())
		if err != nil {
			writeReleaseStoreError(response, request, err)
			return
		}
		writeSuccess(response, request, http.StatusOK, summary)
		return
	}
	result, err := handler.releaseStoreService.RefreshIndex(
		request.Context(),
		domain.IndexRefreshRequest{
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
	writeSuccess(response, request, http.StatusOK, map[string]any{
		"release": result,
	})
}

func writeReleaseStoreError(
	response http.ResponseWriter,
	request *http.Request,
	err error,
) {
	switch {
	case errors.Is(err, domain.ErrPathInvalid):
		writeError(response, request, http.StatusUnprocessableEntity, "invalid_path", "文件路径不正确")
	case errors.Is(err, domain.ErrFileNotFound):
		writeError(response, request, http.StatusNotFound, "file_not_found", "没有找到这个文件")
	case errors.Is(err, domain.ErrFileAlreadyExists):
		writeError(response, request, http.StatusConflict, "file_exists", "文件已经存在，请确认是否替换")
	case errors.Is(err, domain.ErrFileTooLarge):
		writeError(response, request, http.StatusRequestEntityTooLarge, "file_too_large", "上传文件过大")
	case errors.Is(err, domain.ErrReleaseNotCreated):
		writeError(response, request, http.StatusNotFound, "release_not_created", "还没有找到这个版本目录")
	case errors.Is(err, domain.ErrReleaseInvalid):
		writeError(response, request, http.StatusUnprocessableEntity, "invalid_release", "请检查版本和文件目录")
	default:
		writeError(response, request, http.StatusInternalServerError, "service_error", "下载文件操作没有完成，请稍后重试")
	}
}
