package http

import (
	"context"
	"errors"
	"net/http"
	"strings"

	runtimeservice "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/device_runtime/service"
	localcommanddomain "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/local_command/domain"
	localcommandservice "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/local_command/service"
)

// localCommandService is the guardian-facing local command surface.
//
// The HTTP layer depends on this narrow interface so the module stays
// removable and tests can substitute a small fake.
type localCommandService interface {
	List(ctx context.Context, familyID string) (localcommandservice.CommandSet, error)
	Replace(
		ctx context.Context,
		familyID string,
		inputs []localcommanddomain.CommandInput,
	) (localcommandservice.CommandSet, error)
	Delete(ctx context.Context, familyID string, commandID string) (int, error)
}

type localCommandHandler struct {
	service localCommandService
}

// deviceLocalCommandHandler hands the family's command set to a bound device
// behind a device session token rather than a guardian credential.
type deviceLocalCommandHandler struct {
	handler        *localCommandHandler
	bindingService englishSessionResolver
	runtimeService *runtimeservice.Service
}

type localCommandPayload struct {
	CommandID string `json:"command_id"`
	Phrase    string `json:"phrase"`
	Action    string `json:"action"`
	Argument  string `json:"argument"`
	Enabled   bool   `json:"enabled"`
}

type replaceLocalCommandsRequest struct {
	Commands []localCommandPayload `json:"commands"`
}

type deleteLocalCommandRequest struct {
	CommandID string `json:"command_id"`
}

func (handler localCommandHandler) list(
	response http.ResponseWriter,
	request *http.Request,
) {
	familyID, ok := authenticatedFamilyOwnerID(request)
	if !ok {
		writeError(response, request, http.StatusUnauthorized, "unauthenticated", "请重新登录")
		return
	}
	set, err := handler.service.List(request.Context(), familyID)
	if err != nil {
		writeLocalCommandError(response, request, err)
		return
	}
	writeSuccess(response, request, http.StatusOK, localCommandSetResponse(set))
}

func (handler localCommandHandler) replace(
	response http.ResponseWriter,
	request *http.Request,
) {
	familyID, ok := authenticatedFamilyOwnerID(request)
	if !ok {
		writeError(response, request, http.StatusUnauthorized, "unauthenticated", "请重新登录")
		return
	}
	var payload replaceLocalCommandsRequest
	if err := decodeJSON(request, &payload); err != nil {
		writeError(response, request, http.StatusBadRequest, "invalid_request", "请检查本地指令")
		return
	}
	inputs := make([]localcommanddomain.CommandInput, 0, len(payload.Commands))
	for _, command := range payload.Commands {
		inputs = append(inputs, localcommanddomain.CommandInput{
			CommandID: command.CommandID,
			Phrase:    command.Phrase,
			Action:    command.Action,
			Argument:  command.Argument,
			Enabled:   command.Enabled,
		})
	}
	set, err := handler.service.Replace(request.Context(), familyID, inputs)
	if err != nil {
		writeLocalCommandError(response, request, err)
		return
	}
	writeSuccess(response, request, http.StatusOK, localCommandSetResponse(set))
}

func (handler localCommandHandler) delete(
	response http.ResponseWriter,
	request *http.Request,
) {
	familyID, ok := authenticatedFamilyOwnerID(request)
	if !ok {
		writeError(response, request, http.StatusUnauthorized, "unauthenticated", "请重新登录")
		return
	}
	var payload deleteLocalCommandRequest
	commandID := strings.TrimSpace(request.PathValue("command_id"))
	if commandID == "" {
		if err := decodeOptionalJSON(request, &payload); err == nil {
			commandID = strings.TrimSpace(payload.CommandID)
		}
	}
	if _, err := handler.service.Delete(
		request.Context(),
		familyID,
		commandID,
	); err != nil {
		writeLocalCommandError(response, request, err)
		return
	}
	writeSuccess(response, request, http.StatusOK, map[string]any{
		"command_id": commandID,
	})
}

// listForDevice returns the family's command set to a bound device.
func (handler deviceLocalCommandHandler) listForDevice(
	response http.ResponseWriter,
	request *http.Request,
) {
	familyID, ok := handler.verifyDeviceFamily(response, request)
	if !ok {
		return
	}
	set, err := handler.handler.service.List(request.Context(), familyID)
	if err != nil {
		writeLocalCommandError(response, request, err)
		return
	}
	writeSuccess(response, request, http.StatusOK, localCommandSetResponse(set))
}

func (handler deviceLocalCommandHandler) verifyDeviceFamily(
	response http.ResponseWriter,
	request *http.Request,
) (string, bool) {
	if handler.bindingService == nil || handler.runtimeService == nil {
		writeError(response, request, http.StatusUnauthorized, "device_session_expired", "设备登录已过期，请重新连接")
		return "", false
	}
	deviceSessionToken, ok := bearerToken(request)
	if !ok {
		writeError(response, request, http.StatusUnauthorized, "device_session_expired", "设备登录已过期，请重新连接")
		return "", false
	}
	pathDeviceID := strings.TrimSpace(request.PathValue("device_id"))
	sessionDeviceID, err := handler.bindingService.VerifyDeviceSession(
		request.Context(),
		deviceSessionToken,
	)
	if err != nil || sessionDeviceID != pathDeviceID {
		writeError(response, request, http.StatusUnauthorized, "device_session_expired", "设备登录已过期，请重新连接")
		return "", false
	}
	familyID, err := handler.runtimeService.ResolveDeviceFamily(
		request.Context(),
		deviceSessionToken,
		pathDeviceID,
	)
	if err != nil {
		writeLocalCommandError(response, request, err)
		return "", false
	}
	return familyID, true
}

func localCommandSetResponse(set localcommandservice.CommandSet) map[string]any {
	commands := make([]map[string]any, 0, len(set.Commands))
	for index := range set.Commands {
		command := set.Commands[index]
		commands = append(commands, map[string]any{
			"command_id": command.CommandID,
			"phrase":     command.Phrase,
			"action":     command.Action,
			"argument":   command.Argument,
			"enabled":    command.Enabled,
			"revision":   command.Revision,
		})
	}
	return map[string]any{
		"revision": set.Revision,
		"commands": commands,
	}
}

func writeLocalCommandError(
	response http.ResponseWriter,
	request *http.Request,
	err error,
) {
	switch {
	case errors.Is(err, localcommanddomain.ErrCommandNotFound):
		writeError(response, request, http.StatusNotFound, "command_not_found", "没有找到这条本地指令")
	case errors.Is(err, localcommanddomain.ErrInvalidPhrase):
		writeError(response, request, http.StatusUnprocessableEntity, "invalid_phrase", "口令不能为空，也不能太长")
	case errors.Is(err, localcommanddomain.ErrInvalidAction):
		writeError(response, request, http.StatusUnprocessableEntity, "invalid_action", "请选择当地支持的指令动作")
	case errors.Is(err, localcommanddomain.ErrInvalidArgument):
		writeError(response, request, http.StatusUnprocessableEntity, "invalid_argument", "指令参数和动作不匹配")
	case errors.Is(err, localcommanddomain.ErrDuplicatePhrase):
		writeError(response, request, http.StatusConflict, "duplicate_phrase", "有两条指令用了同一个口令")
	case errors.Is(err, localcommanddomain.ErrInvalidCommand):
		writeError(response, request, http.StatusUnprocessableEntity, "invalid_command", "请检查本地指令")
	default:
		writeError(response, request, http.StatusInternalServerError, "service_error", "操作没有完成，请稍后重试")
	}
}
