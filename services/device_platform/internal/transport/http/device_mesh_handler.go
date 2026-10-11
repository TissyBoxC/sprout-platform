package http

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"

	meshdomain "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/device_mesh/domain"
	meshservice "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/device_mesh/service"
)

// deviceMeshService is the guardian-facing multi-device surface. The HTTP layer
// depends on this narrow interface so the module stays removable.
type deviceMeshService interface {
	CreateGroup(
		ctx context.Context,
		familyID string,
		name string,
		routeScope string,
	) (meshdomain.DeviceGroup, error)
	DeleteGroup(ctx context.Context, familyID string, groupID string) error
	Groups(ctx context.Context, familyID string) ([]meshdomain.DeviceGroup, error)
	AddMember(
		ctx context.Context,
		familyID string,
		groupID string,
		deviceID string,
	) (meshdomain.Member, error)
	RemoveMember(ctx context.Context, familyID string, groupID string, deviceID string) error
	Members(ctx context.Context, familyID string, groupID string) ([]meshdomain.Member, error)
	RouteMessage(
		ctx context.Context,
		familyID string,
		message meshdomain.RoutedMessage,
	) (meshdomain.RoutedMessage, error)
	MessageHistory(ctx context.Context, familyID string, limit int) ([]meshdomain.RoutedMessage, error)
	ReportSync(
		ctx context.Context,
		familyID string,
		input meshservice.SyncInput,
	) (meshdomain.ContentState, error)
	ContentStates(ctx context.Context, familyID string) ([]meshdomain.ContentState, error)
	AdminStats(ctx context.Context, windowDays int) (meshdomain.AdminStats, error)
}

type deviceMeshHandler struct {
	service deviceMeshService
}

type meshGroupPayload struct {
	Name       string `json:"name"`
	RouteScope string `json:"route_scope"`
}

type meshMemberPayload struct {
	DeviceID string `json:"device_id"`
}

type meshMessagePayload struct {
	SourceDevice string `json:"source_device"`
	Scope        string `json:"scope"`
	Target       string `json:"target"`
	Body         string `json:"body"`
}

type meshSyncPayload struct {
	ContentKey    string `json:"content_key"`
	LocalVersion  uint64 `json:"local_version"`
	RemoteVersion uint64 `json:"remote_version"`
	Strategy      string `json:"strategy"`
}

// listGroups returns every device group in the authenticated family.
func (handler deviceMeshHandler) listGroups(
	response http.ResponseWriter,
	request *http.Request,
) {
	familyID, ok := authenticatedAccountID(request)
	if !ok {
		writeError(response, request, http.StatusUnauthorized, "unauthenticated", "请重新登录")
		return
	}
	groups, err := handler.service.Groups(request.Context(), familyID)
	if err != nil {
		writeDeviceMeshError(response, request, err)
		return
	}
	if groups == nil {
		groups = []meshdomain.DeviceGroup{}
	}
	writeSuccess(response, request, http.StatusOK, map[string]any{
		"groups": meshGroupResponses(groups),
	})
}

// createGroup stores a new device group.
func (handler deviceMeshHandler) createGroup(
	response http.ResponseWriter,
	request *http.Request,
) {
	familyID, ok := authenticatedAccountID(request)
	if !ok {
		writeError(response, request, http.StatusUnauthorized, "unauthenticated", "请重新登录")
		return
	}
	var payload meshGroupPayload
	if err := decodeJSON(request, &payload); err != nil {
		writeError(response, request, http.StatusBadRequest, "invalid_request", "请检查设备组信息")
		return
	}
	group, err := handler.service.CreateGroup(
		request.Context(),
		familyID,
		payload.Name,
		payload.RouteScope,
	)
	if err != nil {
		writeDeviceMeshError(response, request, err)
		return
	}
	writeSuccess(response, request, http.StatusCreated, meshGroupResponse(group))
}

// deleteGroup removes one device group.
func (handler deviceMeshHandler) deleteGroup(
	response http.ResponseWriter,
	request *http.Request,
) {
	familyID, ok := authenticatedAccountID(request)
	if !ok {
		writeError(response, request, http.StatusUnauthorized, "unauthenticated", "请重新登录")
		return
	}
	groupID := strings.TrimSpace(request.PathValue("group_id"))
	if err := handler.service.DeleteGroup(request.Context(), familyID, groupID); err != nil {
		writeDeviceMeshError(response, request, err)
		return
	}
	writeSuccess(response, request, http.StatusOK, map[string]any{"deleted": true})
}

// listMembers returns every member of a group.
func (handler deviceMeshHandler) listMembers(
	response http.ResponseWriter,
	request *http.Request,
) {
	familyID, ok := authenticatedAccountID(request)
	if !ok {
		writeError(response, request, http.StatusUnauthorized, "unauthenticated", "请重新登录")
		return
	}
	groupID := strings.TrimSpace(request.PathValue("group_id"))
	members, err := handler.service.Members(request.Context(), familyID, groupID)
	if err != nil {
		writeDeviceMeshError(response, request, err)
		return
	}
	if members == nil {
		members = []meshdomain.Member{}
	}
	writeSuccess(response, request, http.StatusOK, map[string]any{
		"members": meshMemberResponses(members),
	})
}

// addMember adds one device to a group.
func (handler deviceMeshHandler) addMember(
	response http.ResponseWriter,
	request *http.Request,
) {
	familyID, ok := authenticatedAccountID(request)
	if !ok {
		writeError(response, request, http.StatusUnauthorized, "unauthenticated", "请重新登录")
		return
	}
	groupID := strings.TrimSpace(request.PathValue("group_id"))
	var payload meshMemberPayload
	if err := decodeJSON(request, &payload); err != nil {
		writeError(response, request, http.StatusBadRequest, "invalid_request", "请检查设备信息")
		return
	}
	member, err := handler.service.AddMember(
		request.Context(),
		familyID,
		groupID,
		payload.DeviceID,
	)
	if err != nil {
		writeDeviceMeshError(response, request, err)
		return
	}
	writeSuccess(response, request, http.StatusCreated, meshMemberResponse(member))
}

// removeMember removes one device from a group.
func (handler deviceMeshHandler) removeMember(
	response http.ResponseWriter,
	request *http.Request,
) {
	familyID, ok := authenticatedAccountID(request)
	if !ok {
		writeError(response, request, http.StatusUnauthorized, "unauthenticated", "请重新登录")
		return
	}
	groupID := strings.TrimSpace(request.PathValue("group_id"))
	deviceID := strings.TrimSpace(request.PathValue("device_id"))
	if err := handler.service.RemoveMember(request.Context(), familyID, groupID, deviceID); err != nil {
		writeDeviceMeshError(response, request, err)
		return
	}
	writeSuccess(response, request, http.StatusOK, map[string]any{"deleted": true})
}

// listMessages returns the recent routed messages in the family.
func (handler deviceMeshHandler) listMessages(
	response http.ResponseWriter,
	request *http.Request,
) {
	familyID, ok := authenticatedAccountID(request)
	if !ok {
		writeError(response, request, http.StatusUnauthorized, "unauthenticated", "请重新登录")
		return
	}
	limit := parseMeshLimit(request.URL.Query().Get("limit"))
	messages, err := handler.service.MessageHistory(request.Context(), familyID, limit)
	if err != nil {
		writeDeviceMeshError(response, request, err)
		return
	}
	if messages == nil {
		messages = []meshdomain.RoutedMessage{}
	}
	writeSuccess(response, request, http.StatusOK, map[string]any{
		"messages": meshMessageResponses(messages),
	})
}

// routeMessage stores one routed message.
func (handler deviceMeshHandler) routeMessage(
	response http.ResponseWriter,
	request *http.Request,
) {
	familyID, ok := authenticatedAccountID(request)
	if !ok {
		writeError(response, request, http.StatusUnauthorized, "unauthenticated", "请重新登录")
		return
	}
	var payload meshMessagePayload
	if err := decodeJSON(request, &payload); err != nil {
		writeError(response, request, http.StatusBadRequest, "invalid_request", "请检查消息内容")
		return
	}
	message, err := handler.service.RouteMessage(request.Context(), familyID, meshdomain.RoutedMessage{
		SourceDevice: payload.SourceDevice,
		Scope:        payload.Scope,
		Target:       payload.Target,
		Body:         payload.Body,
	})
	if err != nil {
		writeDeviceMeshError(response, request, err)
		return
	}
	writeSuccess(response, request, http.StatusCreated, meshMessageResponse(message))
}

// listContentStates returns every content-sync state in the family.
func (handler deviceMeshHandler) listContentStates(
	response http.ResponseWriter,
	request *http.Request,
) {
	familyID, ok := authenticatedAccountID(request)
	if !ok {
		writeError(response, request, http.StatusUnauthorized, "unauthenticated", "请重新登录")
		return
	}
	states, err := handler.service.ContentStates(request.Context(), familyID)
	if err != nil {
		writeDeviceMeshError(response, request, err)
		return
	}
	if states == nil {
		states = []meshdomain.ContentState{}
	}
	writeSuccess(response, request, http.StatusOK, map[string]any{
		"states": meshContentResponses(states),
	})
}

// reportSync applies one content-sync report.
func (handler deviceMeshHandler) reportSync(
	response http.ResponseWriter,
	request *http.Request,
) {
	familyID, ok := authenticatedAccountID(request)
	if !ok {
		writeError(response, request, http.StatusUnauthorized, "unauthenticated", "请重新登录")
		return
	}
	var payload meshSyncPayload
	if err := decodeJSON(request, &payload); err != nil {
		writeError(response, request, http.StatusBadRequest, "invalid_request", "请检查同步内容")
		return
	}
	state, err := handler.service.ReportSync(request.Context(), familyID, meshservice.SyncInput{
		ContentKey:    payload.ContentKey,
		LocalVersion:  payload.LocalVersion,
		RemoteVersion: payload.RemoteVersion,
		Strategy:      payload.Strategy,
	})
	if err != nil {
		writeDeviceMeshError(response, request, err)
		return
	}
	writeSuccess(response, request, http.StatusOK, meshContentResponse(state))
}

// adminStats returns the bounded fleet multi-device summary.
func (handler deviceMeshHandler) adminStats(
	response http.ResponseWriter,
	request *http.Request,
) {
	windowDays := parseVideoWindowDays(request.URL.Query().Get("window_days"))
	stats, err := handler.service.AdminStats(request.Context(), windowDays)
	if err != nil {
		writeDeviceMeshError(response, request, err)
		return
	}
	writeSuccess(response, request, http.StatusOK, stats)
}

func meshGroupResponse(group meshdomain.DeviceGroup) map[string]any {
	return map[string]any{
		"id":           group.ID,
		"name":         group.Name,
		"route_scope":  group.RouteScope,
		"member_count": group.MemberCount,
		"created_at":   group.CreatedAt,
	}
}

func meshGroupResponses(groups []meshdomain.DeviceGroup) []map[string]any {
	results := make([]map[string]any, 0, len(groups))
	for _, group := range groups {
		results = append(results, meshGroupResponse(group))
	}
	return results
}

func meshMemberResponse(member meshdomain.Member) map[string]any {
	return map[string]any{
		"group_id":  member.GroupID,
		"device_id": member.DeviceID,
		"added_at":  member.AddedAt,
	}
}

func meshMemberResponses(members []meshdomain.Member) []map[string]any {
	results := make([]map[string]any, 0, len(members))
	for _, member := range members {
		results = append(results, meshMemberResponse(member))
	}
	return results
}

func meshMessageResponse(message meshdomain.RoutedMessage) map[string]any {
	return map[string]any{
		"id":            message.ID,
		"source_device": message.SourceDevice,
		"scope":         message.Scope,
		"target":        message.Target,
		"body":          message.Body,
		"created_at":    message.CreatedAt,
	}
}

func meshMessageResponses(messages []meshdomain.RoutedMessage) []map[string]any {
	results := make([]map[string]any, 0, len(messages))
	for _, message := range messages {
		results = append(results, meshMessageResponse(message))
	}
	return results
}

func meshContentResponse(state meshdomain.ContentState) map[string]any {
	return map[string]any{
		"content_key":    state.ContentKey,
		"version":        state.Version,
		"conflict_count": state.ConflictCount,
		"sync_state":     state.SyncState,
		"updated_at":     state.UpdatedAt,
	}
}

func meshContentResponses(states []meshdomain.ContentState) []map[string]any {
	results := make([]map[string]any, 0, len(states))
	for _, state := range states {
		results = append(results, meshContentResponse(state))
	}
	return results
}

func parseMeshLimit(raw string) int {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0
	}
	value, err := strconv.Atoi(raw)
	if err != nil || value <= 0 ||
		value > meshservice.MessageHistoryMaxLimit {
		return 0
	}
	return value
}

func writeDeviceMeshError(
	response http.ResponseWriter,
	request *http.Request,
	err error,
) {
	switch {
	case errors.Is(err, meshdomain.ErrInvalidGroup):
		writeError(response, request, http.StatusUnprocessableEntity, "invalid_group", "请检查设备组信息")
	case errors.Is(err, meshdomain.ErrGroupNotFound):
		writeError(response, request, http.StatusNotFound, "group_not_found", "设备组不存在")
	case errors.Is(err, meshdomain.ErrInvalidMember):
		writeError(response, request, http.StatusUnprocessableEntity, "invalid_member", "请检查设备信息")
	case errors.Is(err, meshdomain.ErrMemberNotFound):
		writeError(response, request, http.StatusNotFound, "member_not_found", "设备不在该组")
	case errors.Is(err, meshdomain.ErrInvalidMessage):
		writeError(response, request, http.StatusUnprocessableEntity, "invalid_message", "请检查消息内容")
	case errors.Is(err, meshdomain.ErrInvalidSync):
		writeError(response, request, http.StatusUnprocessableEntity, "invalid_sync", "请检查同步内容")
	default:
		writeError(response, request, http.StatusInternalServerError, "service_error", "操作没有完成，请稍后重试")
	}
}
