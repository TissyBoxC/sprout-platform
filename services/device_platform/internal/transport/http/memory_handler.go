package http

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	memorydomain "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/memory/domain"
	memoryrepository "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/memory/repository"
	memoryservice "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/memory/service"
)

// memoryService is the guardian-facing child memory surface.
//
// The HTTP layer depends on this narrow interface so the module stays removable
// and route tests can substitute a small fake. The service owns every
// ownership check; this file only shapes the wire format.
type memoryService interface {
	ListForGuardian(
		ctx context.Context,
		familyID string,
		childID string,
	) ([]memorydomain.ChildMemory, error)
	SummariesForFamily(
		ctx context.Context,
		familyID string,
	) ([]memoryrepository.FamilyMemorySummary, error)
	SetForGuardian(
		ctx context.Context,
		familyID string,
		childID string,
		input memorydomain.MemoryInput,
	) (*memorydomain.ChildMemory, error)
	DeleteForGuardian(
		ctx context.Context,
		familyID string,
		childID string,
		memoryKey string,
	) (int64, error)
	DeleteAllForGuardian(
		ctx context.Context,
		familyID string,
		childID string,
	) (int64, error)
	SyncForDevice(
		ctx context.Context,
		familyID string,
		childID string,
		sinceRevision int64,
	) (memorydomain.MemoryPage, error)
	UpsertForDevice(
		ctx context.Context,
		familyID string,
		childID string,
		inputs []memorydomain.MemoryInput,
	) ([]memorydomain.ChildMemory, error)
	CurrentRevisionForDevice(
		ctx context.Context,
		familyID string,
		childID string,
	) (int64, error)
}

// memoryHandler exposes the guardian-visible memory store.
//
// Boundary: every response contains memory values, so the routes must stay
// behind guardian authentication plus the child read/manage family actions.
// Nothing here logs a request or response body.
type memoryHandler struct {
	service memoryService
}

// deviceMemoryHandler serves the device sync protocol behind a device session
// token. The family comes from the session binding and the child from the
// family's profile, never from the request, so a device cannot pull or write
// another family's or another child's memories.
type deviceMemoryHandler struct {
	handler memoryService
	// familyResolver maps a device session to its owning family.
	familyResolver englishRuntimeResolver
	// childResolver maps a family to its child profile.
	childResolver memoryDeviceChildResolver
}

// memoryDeviceChildResolver returns the child profile a device is bound to.
//
// It returns an empty id when the family has no child profile yet. It must
// return memoryrepository.ErrDeviceChildAmbiguous when the family has more than
// one profile and the binding cannot disambiguate.
type memoryDeviceChildResolver interface {
	ResolveFamilyChild(
		ctx context.Context,
		familyID string,
		deviceID string,
	) (string, error)
}

type memoryUpsertRequest struct {
	MemoryKey   string   `json:"memory_key"`
	MemoryValue string   `json:"memory_value"`
	Confidence  *float64 `json:"confidence"`
}

type memoryBatchRequest struct {
	Memories []memoryUpsertRequest `json:"memories"`
}

func (handler memoryHandler) list(
	response http.ResponseWriter,
	request *http.Request,
	familyID string,
) {
	if handler.service == nil {
		writeError(response, request, http.StatusServiceUnavailable, "service_unavailable", "长期记忆暂时无法读取，请稍后重试")
		return
	}
	if !memoryFamilyMatches(response, request, familyID) {
		return
	}
	childID := strings.TrimSpace(request.PathValue("child_id"))
	memories, err := handler.service.ListForGuardian(request.Context(), familyID, childID)
	if err != nil {
		writeMemoryError(response, request, err)
		return
	}
	items := make([]map[string]any, 0, len(memories))
	for index := range memories {
		items = append(items, memoryResponse(&memories[index]))
	}
	writeSuccess(response, request, http.StatusOK, map[string]any{
		"child_id": childID,
		"memories": items,
	})
}

// overview lists which memories exist for every child of the family without
// returning the stored values. The guardian drills into one child to read them.
func (handler memoryHandler) overview(
	response http.ResponseWriter,
	request *http.Request,
	familyID string,
) {
	if handler.service == nil {
		writeError(response, request, http.StatusServiceUnavailable, "service_unavailable", "长期记忆暂时无法读取，请稍后重试")
		return
	}
	if !memoryFamilyMatches(response, request, familyID) {
		return
	}
	summaries, err := handler.service.SummariesForFamily(request.Context(), familyID)
	if err != nil {
		writeMemoryError(response, request, err)
		return
	}
	children := make([]map[string]any, 0, len(summaries))
	for _, summary := range summaries {
		keys := make([]map[string]any, 0, len(summary.Memories))
		for _, memory := range summary.Memories {
			keys = append(keys, map[string]any{
				"memory_key":         memory.MemoryKey,
				"source":             memory.Source,
				"confidence":         memory.Confidence,
				"revision":           memory.Revision,
				"updated_at":         memory.UpdatedAt.UTC().Format(time.RFC3339),
				"last_reinforced_at": memory.LastReinforcedAt.UTC().Format(time.RFC3339),
			})
		}
		children = append(children, map[string]any{
			"child_id": summary.ChildID,
			"memories": keys,
		})
	}
	writeSuccess(response, request, http.StatusOK, map[string]any{
		"children": children,
	})
}

// set creates or replaces one guardian-authored memory.
func (handler memoryHandler) set(
	response http.ResponseWriter,
	request *http.Request,
	familyID string,
) {
	if handler.service == nil {
		writeError(response, request, http.StatusServiceUnavailable, "service_unavailable", "长期记忆暂时无法保存，请稍后重试")
		return
	}
	if !memoryFamilyMatches(response, request, familyID) {
		return
	}
	childID := strings.TrimSpace(request.PathValue("child_id"))
	var payload memoryUpsertRequest
	if err := decodeJSON(request, &payload); err != nil {
		writeError(response, request, http.StatusBadRequest, "invalid_request", "请检查要保存的记忆内容")
		return
	}
	memory, err := handler.service.SetForGuardian(
		request.Context(),
		familyID,
		childID,
		memorydomain.MemoryInput{
			MemoryKey:   payload.MemoryKey,
			MemoryValue: payload.MemoryValue,
			Confidence:  payload.Confidence,
		},
	)
	if err != nil {
		writeMemoryError(response, request, err)
		return
	}
	writeSuccess(response, request, http.StatusOK, map[string]any{
		"memory": memoryResponse(memory),
	})
}

// remove deletes one memory. A missing key is reported as not found so a
// double-tap in the guardian UI does not surface as a server error.
func (handler memoryHandler) remove(
	response http.ResponseWriter,
	request *http.Request,
	familyID string,
) {
	if handler.service == nil {
		writeError(response, request, http.StatusServiceUnavailable, "service_unavailable", "长期记忆暂时无法删除，请稍后重试")
		return
	}
	if !memoryFamilyMatches(response, request, familyID) {
		return
	}
	childID := strings.TrimSpace(request.PathValue("child_id"))
	memoryKey := strings.TrimSpace(request.PathValue("memory_key"))
	revision, err := handler.service.DeleteForGuardian(
		request.Context(),
		familyID,
		childID,
		memoryKey,
	)
	if err != nil {
		writeMemoryError(response, request, err)
		return
	}
	writeSuccess(response, request, http.StatusOK, map[string]any{
		"child_id":   childID,
		"memory_key": memoryKey,
		"deleted":    true,
		"revision":   revision,
	})
}

// removeAll is the guardian's "forget everything about my child" action.
func (handler memoryHandler) removeAll(
	response http.ResponseWriter,
	request *http.Request,
	familyID string,
) {
	if handler.service == nil {
		writeError(response, request, http.StatusServiceUnavailable, "service_unavailable", "长期记忆暂时无法删除，请稍后重试")
		return
	}
	if !memoryFamilyMatches(response, request, familyID) {
		return
	}
	childID := strings.TrimSpace(request.PathValue("child_id"))
	deleted, err := handler.service.DeleteAllForGuardian(
		request.Context(),
		familyID,
		childID,
	)
	if err != nil {
		writeMemoryError(response, request, err)
		return
	}
	writeSuccess(response, request, http.StatusOK, map[string]any{
		"child_id": childID,
		"deleted":  true,
		"count":    deleted,
	})
}

// syncForDevice returns the child's memory delta since since_revision.
func (handler deviceMemoryHandler) syncForDevice(
	response http.ResponseWriter,
	request *http.Request,
) {
	familyID, childID, ok := handler.resolveDevice(response, request)
	if !ok {
		return
	}
	sinceRevision, valid := parseSinceRevision(
		response,
		request,
		request.URL.Query().Get("since_revision"),
	)
	if !valid {
		return
	}
	page, err := handler.handler.SyncForDevice(
		request.Context(),
		familyID,
		childID,
		sinceRevision,
	)
	if err != nil {
		writeMemoryError(response, request, err)
		return
	}
	items := make([]map[string]any, 0, len(page.Memories))
	for index := range page.Memories {
		items = append(items, memoryResponse(&page.Memories[index]))
	}
	writeSuccess(response, request, http.StatusOK, map[string]any{
		"child_id":       page.ChildID,
		"since_revision": page.SinceRevision,
		"revision":       page.NextRevision,
		"memories":       items,
		"deleted_keys":   page.DeletedKeys,
	})
}

// upsertForDevice stores a device-reported batch and answers with the new
// revision the device must use as its next cursor.
func (handler deviceMemoryHandler) upsertForDevice(
	response http.ResponseWriter,
	request *http.Request,
) {
	familyID, childID, ok := handler.resolveDevice(response, request)
	if !ok {
		return
	}
	var payload memoryBatchRequest
	if err := decodeJSON(request, &payload); err != nil {
		writeError(response, request, http.StatusBadRequest, "invalid_request", "请检查上报的记忆内容")
		return
	}
	if len(payload.Memories) == 0 {
		writeError(response, request, http.StatusUnprocessableEntity, "invalid_memory_batch", "没有可保存的记忆")
		return
	}
	if len(payload.Memories) > memorydomain.MaxBatchSize {
		writeError(response, request, http.StatusUnprocessableEntity, "memory_limit_exceeded", "一次上报的记忆条数超过上限")
		return
	}
	inputs := make([]memorydomain.MemoryInput, 0, len(payload.Memories))
	for _, item := range payload.Memories {
		inputs = append(inputs, memorydomain.MemoryInput{
			MemoryKey:   item.MemoryKey,
			MemoryValue: item.MemoryValue,
			Confidence:  item.Confidence,
		})
	}
	stored, err := handler.handler.UpsertForDevice(
		request.Context(),
		familyID,
		childID,
		inputs,
	)
	if err != nil {
		writeMemoryError(response, request, err)
		return
	}
	revision, err := handler.handler.CurrentRevisionForDevice(
		request.Context(),
		familyID,
		childID,
	)
	if err != nil {
		writeMemoryError(response, request, err)
		return
	}
	items := make([]map[string]any, 0, len(stored))
	for index := range stored {
		items = append(items, memoryResponse(&stored[index]))
	}
	writeSuccess(response, request, http.StatusOK, map[string]any{
		"child_id": childID,
		"revision": revision,
		"memories": items,
	})
}

// resolveDevice authenticates the device session and resolves the family and
// child the device is allowed to sync.
func (handler deviceMemoryHandler) resolveDevice(
	response http.ResponseWriter,
	request *http.Request,
) (string, string, bool) {
	if handler.handler == nil || handler.familyResolver == nil || handler.childResolver == nil {
		writeError(response, request, http.StatusServiceUnavailable, "service_unavailable", "长期记忆同步暂时不可用，请稍后重试")
		return "", "", false
	}
	deviceSessionToken, ok := bearerToken(request)
	if !ok {
		writeError(response, request, http.StatusUnauthorized, "device_session_expired", "设备登录已过期，请重新连接")
		return "", "", false
	}
	deviceID := strings.TrimSpace(request.PathValue("device_id"))
	familyID, err := handler.familyResolver.ResolveDeviceFamily(
		request.Context(),
		deviceSessionToken,
		deviceID,
	)
	if err != nil {
		writeDeviceBindingError(response, request, err)
		return "", "", false
	}
	childID, err := handler.childResolver.ResolveFamilyChild(
		request.Context(),
		familyID,
		deviceID,
	)
	if errors.Is(err, memoryrepository.ErrDeviceChildAmbiguous) {
		writeError(response, request, http.StatusConflict, "child_binding_ambiguous", "这个家庭有多个儿童档案，请先在家长端确认设备对应的儿童")
		return "", "", false
	}
	if err != nil || strings.TrimSpace(childID) == "" {
		writeError(response, request, http.StatusNotFound, "child_not_bound", "这台设备还没有绑定儿童档案")
		return "", "", false
	}
	return familyID, childID, true
}

// memoryFamilyMatches refuses a path family that is not the caller's own
// family. A mismatch answers 404 so the route cannot be used to probe which
// family ids exist.
func memoryFamilyMatches(
	response http.ResponseWriter,
	request *http.Request,
	familyID string,
) bool {
	access, ok := authenticatedFamilyAccess(request)
	if !ok {
		writeError(response, request, http.StatusUnauthorized, "unauthenticated", "请重新登录")
		return false
	}
	if strings.TrimSpace(familyID) != access.FamilyID {
		writeError(response, request, http.StatusNotFound, "family_not_found", "没有找到这个家庭")
		return false
	}
	return true
}

// parseSinceRevision parses the sync cursor. An absent value means a full pull;
// a malformed value is rejected instead of defaulting to 0, because a silent
// reset would make the device drop tombstone information.
func parseSinceRevision(
	response http.ResponseWriter,
	request *http.Request,
	raw string,
) (int64, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0, true
	}
	value, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || value < 0 {
		writeError(response, request, http.StatusUnprocessableEntity, "invalid_since_revision", "请检查同步版本号")
		return 0, false
	}
	return value, true
}

func memoryResponse(memory *memorydomain.ChildMemory) map[string]any {
	return map[string]any{
		"id":                 memory.ID,
		"child_id":           memory.ChildID,
		"memory_key":         memory.MemoryKey,
		"memory_value":       memory.MemoryValue,
		"confidence":         memory.Confidence,
		"source":             memory.Source,
		"revision":           memory.Revision,
		"created_at":         memory.CreatedAt.UTC().Format(time.RFC3339),
		"updated_at":         memory.UpdatedAt.UTC().Format(time.RFC3339),
		"last_reinforced_at": memory.LastReinforcedAt.UTC().Format(time.RFC3339),
	}
}

func writeMemoryError(
	response http.ResponseWriter,
	request *http.Request,
	err error,
) {
	switch {
	case errors.Is(err, memorydomain.ErrMemoryLimitExceeded):
		writeError(response, request, http.StatusConflict, "memory_limit_exceeded", "这个孩子的记忆条数已达上限，请先删除不再需要的记忆")
	case errors.Is(err, memorydomain.ErrMemoryNotFound):
		writeError(response, request, http.StatusNotFound, "memory_not_found", "没有找到这条记忆")
	case errors.Is(err, memorydomain.ErrChildNotFound):
		writeError(response, request, http.StatusNotFound, "child_not_found", "没有找到这个儿童")
	case errors.Is(err, memorydomain.ErrMemoryRevisionConflict):
		writeError(response, request, http.StatusConflict, "memory_revision_conflict", "记忆已在其他设备更新，请重新同步后再试")
	case errors.Is(err, memorydomain.ErrInvalidMemoryKey):
		writeError(response, request, http.StatusUnprocessableEntity, "invalid_memory_key", "请使用小写字母、数字和下划线作为记忆标识")
	case errors.Is(err, memorydomain.ErrInvalidMemoryValue):
		writeError(response, request, http.StatusUnprocessableEntity, "invalid_memory_value", "记忆内容不能为空，且请在 280 字以内")
	case errors.Is(err, memorydomain.ErrInvalidMemorySource):
		writeError(response, request, http.StatusUnprocessableEntity, "invalid_memory_source", "请选择有效的记忆来源")
	case errors.Is(err, memorydomain.ErrInvalidMemoryConfidence):
		writeError(response, request, http.StatusUnprocessableEntity, "invalid_memory_confidence", "置信度必须在 0 到 1 之间")
	case errors.Is(err, memorydomain.ErrInvalidMemoryInput):
		writeError(response, request, http.StatusUnprocessableEntity, "invalid_request", "请检查记忆内容")
	default:
		writeError(response, request, http.StatusInternalServerError, "service_error", "操作没有完成，请稍后重试")
	}
}

var _ memoryService = (*memoryservice.Service)(nil)
