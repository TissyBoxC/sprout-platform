package http

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	meshdomain "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/device_mesh/domain"
	meshservice "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/device_mesh/service"
)

// meshTestService is an in-memory double so the handler tests exercise the real
// validation and lifecycle without a database.
type meshTestService struct {
	groups   map[string]meshdomain.DeviceGroup
	members  map[string][]meshdomain.Member
	messages map[string]meshdomain.RoutedMessage
	content  map[string]meshdomain.ContentState
}

func newMeshTestService() *meshTestService {
	return &meshTestService{
		groups:   map[string]meshdomain.DeviceGroup{},
		members:  map[string][]meshdomain.Member{},
		messages: map[string]meshdomain.RoutedMessage{},
		content:  map[string]meshdomain.ContentState{},
	}
}

func (s *meshTestService) CreateGroup(
	_ context.Context,
	familyID string,
	name string,
	routeScope string,
) (meshdomain.DeviceGroup, error) {
	normalizedName, normalizedScope, err := meshdomain.ValidateGroupInput(name, routeScope)
	if err != nil {
		return meshdomain.DeviceGroup{}, err
	}
	group := meshdomain.DeviceGroup{
		ID:         "group-1",
		FamilyID:   familyID,
		Name:       normalizedName,
		RouteScope: normalizedScope,
	}
	s.groups[group.ID] = group
	return group, nil
}

func (s *meshTestService) DeleteGroup(
	_ context.Context,
	_ string,
	groupID string,
) error {
	if _, ok := s.groups[groupID]; !ok {
		return meshdomain.ErrGroupNotFound
	}
	delete(s.groups, groupID)
	return nil
}

func (s *meshTestService) Groups(
	_ context.Context,
	_ string,
) ([]meshdomain.DeviceGroup, error) {
	groups := make([]meshdomain.DeviceGroup, 0, len(s.groups))
	for _, group := range s.groups {
		groups = append(groups, group)
	}
	return groups, nil
}

func (s *meshTestService) AddMember(
	_ context.Context,
	familyID string,
	groupID string,
	deviceID string,
) (meshdomain.Member, error) {
	if _, ok := s.groups[groupID]; !ok {
		return meshdomain.Member{}, meshdomain.ErrGroupNotFound
	}
	member := meshdomain.Member{
		GroupID:  groupID,
		DeviceID: deviceID,
		FamilyID: familyID,
		AddedAt:  time.Date(2026, time.October, 11, 9, 0, 0, 0, time.UTC),
	}
	s.members[groupID] = append(s.members[groupID], member)
	return member, nil
}

func (s *meshTestService) RemoveMember(
	_ context.Context,
	_ string,
	groupID string,
	deviceID string,
) error {
	members := s.members[groupID]
	for index, member := range members {
		if member.DeviceID == deviceID {
			s.members[groupID] = append(members[:index], members[index+1:]...)
			return nil
		}
	}
	return meshdomain.ErrMemberNotFound
}

func (s *meshTestService) Members(
	_ context.Context,
	_ string,
	groupID string,
) ([]meshdomain.Member, error) {
	return s.members[groupID], nil
}

func (s *meshTestService) RouteMessage(
	_ context.Context,
	familyID string,
	message meshdomain.RoutedMessage,
) (meshdomain.RoutedMessage, error) {
	normalized, err := meshdomain.ValidateMessage(message)
	if err != nil {
		return meshdomain.RoutedMessage{}, err
	}
	if normalized.Scope == meshdomain.ScopeGroup {
		if _, ok := s.groups[normalized.Target]; !ok {
			return meshdomain.RoutedMessage{}, meshdomain.ErrGroupNotFound
		}
	}
	routed := meshdomain.RoutedMessage{
		ID:           "message-1",
		FamilyID:     familyID,
		SourceDevice: normalized.SourceDevice,
		Scope:        normalized.Scope,
		Target:       normalized.Target,
		Body:         normalized.Body,
	}
	s.messages[routed.ID] = routed
	return routed, nil
}

func (s *meshTestService) MessageHistory(
	_ context.Context,
	_ string,
	_ int,
) ([]meshdomain.RoutedMessage, error) {
	messages := make([]meshdomain.RoutedMessage, 0, len(s.messages))
	for _, message := range s.messages {
		messages = append(messages, message)
	}
	return messages, nil
}

func (s *meshTestService) ReportSync(
	_ context.Context,
	familyID string,
	input meshservice.SyncInput,
) (meshdomain.ContentState, error) {
	if input.ContentKey == "" {
		return meshdomain.ContentState{}, meshdomain.ErrInvalidSync
	}
	if input.Strategy != "" && !meshdomain.SupportedConflict(input.Strategy) {
		return meshdomain.ContentState{}, meshdomain.ErrInvalidSync
	}
	outcome := meshdomain.SyncOutcome(input.Strategy, input.LocalVersion, input.RemoteVersion)
	state := meshdomain.ContentState{
		FamilyID:   familyID,
		ContentKey: input.ContentKey,
		Version:    input.RemoteVersion,
		SyncState:  outcome,
	}
	s.content[input.ContentKey] = state
	return state, nil
}

func (s *meshTestService) ContentStates(
	_ context.Context,
	_ string,
) ([]meshdomain.ContentState, error) {
	states := make([]meshdomain.ContentState, 0, len(s.content))
	for _, state := range s.content {
		states = append(states, state)
	}
	return states, nil
}

func (s *meshTestService) AdminStats(
	_ context.Context,
	windowDays int,
) (meshdomain.AdminStats, error) {
	return meshdomain.AdminStats{
		WindowDays: windowDays,
		GroupCount: len(s.groups),
	}, nil
}

func TestDeviceMeshGroupsRequireAuthentication(t *testing.T) {
	handler := deviceMeshHandler{service: newMeshTestService()}
	request := englishRequest(http.MethodGet, "/api/v1/device-mesh/groups", "", "")
	recorder := httptest.NewRecorder()
	handler.listGroups(recorder, request)
	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusUnauthorized)
	}
}

func TestDeviceMeshCreateGroup(t *testing.T) {
	handler := deviceMeshHandler{service: newMeshTestService()}
	body := `{"name":"客厅组","route_scope":"group"}`
	request := englishRequest(http.MethodPost, "/api/v1/device-mesh/groups", body, "family-1")
	recorder := httptest.NewRecorder()
	handler.createGroup(recorder, request)
	if recorder.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d: %s",
			recorder.Code, http.StatusCreated, recorder.Body.String())
	}
	var envelope struct {
		Data struct {
			ID         string `json:"id"`
			Name       string `json:"name"`
			RouteScope string `json:"route_scope"`
		} `json:"data"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if envelope.Data.Name != "客厅组" || envelope.Data.RouteScope != "group" {
		t.Fatalf("unexpected group: %+v", envelope.Data)
	}
}

func TestDeviceMeshCreateGroupRejectsDeviceScope(t *testing.T) {
	handler := deviceMeshHandler{service: newMeshTestService()}
	body := `{"name":"客厅组","route_scope":"device"}`
	request := englishRequest(http.MethodPost, "/api/v1/device-mesh/groups", body, "family-1")
	recorder := httptest.NewRecorder()
	handler.createGroup(recorder, request)
	if recorder.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want %d",
			recorder.Code, http.StatusUnprocessableEntity)
	}
}

func TestDeviceMeshAddAndRemoveMember(t *testing.T) {
	service := newMeshTestService()
	handler := deviceMeshHandler{service: service}
	service.groups["group-1"] = meshdomain.DeviceGroup{ID: "group-1", Name: "客厅组"}

	body := `{"device_id":"device-1"}`
	request := englishRequest(
		http.MethodPost,
		"/api/v1/device-mesh/groups/group-1/members",
		body,
		"family-1",
	)
	request.SetPathValue("group_id", "group-1")
	recorder := httptest.NewRecorder()
	handler.addMember(recorder, request)
	if recorder.Code != http.StatusCreated {
		t.Fatalf("add status = %d, want %d: %s",
			recorder.Code, http.StatusCreated, recorder.Body.String())
	}

	members := englishRequest(
		http.MethodGet,
		"/api/v1/device-mesh/groups/group-1/members",
		"",
		"family-1",
	)
	members.SetPathValue("group_id", "group-1")
	recorder = httptest.NewRecorder()
	handler.listMembers(recorder, members)
	if recorder.Code != http.StatusOK {
		t.Fatalf("list status = %d, want %d", recorder.Code, http.StatusOK)
	}
	var envelope struct {
		Data struct {
			Members []map[string]any `json:"members"`
		} `json:"data"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(envelope.Data.Members) != 1 {
		t.Fatalf("expected one member, got %d", len(envelope.Data.Members))
	}

	remove := englishRequest(
		http.MethodDelete,
		"/api/v1/device-mesh/groups/group-1/members/device-1",
		"",
		"family-1",
	)
	remove.SetPathValue("group_id", "group-1")
	remove.SetPathValue("device_id", "device-1")
	recorder = httptest.NewRecorder()
	handler.removeMember(recorder, remove)
	if recorder.Code != http.StatusOK {
		t.Fatalf("remove status = %d, want %d", recorder.Code, http.StatusOK)
	}
}

func TestDeviceMeshRouteMessage(t *testing.T) {
	service := newMeshTestService()
	handler := deviceMeshHandler{service: service}
	service.groups["group-1"] = meshdomain.DeviceGroup{ID: "group-1", Name: "客厅组"}

	body := `{"source_device":"device-1","scope":"group","target":"group-1","body":"晚上一起听故事"}`
	request := englishRequest(http.MethodPost, "/api/v1/device-mesh/messages", body, "family-1")
	recorder := httptest.NewRecorder()
	handler.routeMessage(recorder, request)
	if recorder.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d: %s",
			recorder.Code, http.StatusCreated, recorder.Body.String())
	}

	// A group route to a missing group is a not-found.
	missing := englishRequest(
		http.MethodPost,
		"/api/v1/device-mesh/messages",
		`{"scope":"group","target":"missing","body":"hello"}`,
		"family-1",
	)
	recorder = httptest.NewRecorder()
	handler.routeMessage(recorder, missing)
	if recorder.Code != http.StatusNotFound {
		t.Fatalf("missing status = %d, want %d", recorder.Code, http.StatusNotFound)
	}
}

func TestDeviceMeshReportSync(t *testing.T) {
	handler := deviceMeshHandler{service: newMeshTestService()}
	body := `{"content_key":"theme-pack","local_version":2,"remote_version":5,"strategy":"remote_wins"}`
	request := englishRequest(http.MethodPut, "/api/v1/device-mesh/content", body, "family-1")
	recorder := httptest.NewRecorder()
	handler.reportSync(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d: %s",
			recorder.Code, http.StatusOK, recorder.Body.String())
	}
	var envelope struct {
		Data struct {
			Version   uint64 `json:"version"`
			SyncState string `json:"sync_state"`
		} `json:"data"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if envelope.Data.SyncState != "applied" || envelope.Data.Version != 5 {
		t.Fatalf("unexpected sync state: %+v", envelope.Data)
	}
}

func TestDeviceMeshReportSyncRejectsBadStrategy(t *testing.T) {
	handler := deviceMeshHandler{service: newMeshTestService()}
	body := `{"content_key":"theme-pack","strategy":"wild"}`
	request := englishRequest(http.MethodPut, "/api/v1/device-mesh/content", body, "family-1")
	recorder := httptest.NewRecorder()
	handler.reportSync(recorder, request)
	if recorder.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want %d",
			recorder.Code, http.StatusUnprocessableEntity)
	}
}

func TestDeviceMeshAdminStats(t *testing.T) {
	service := newMeshTestService()
	service.groups["group-1"] = meshdomain.DeviceGroup{ID: "group-1"}
	handler := deviceMeshHandler{service: service}
	request := englishRequest(
		http.MethodGet,
		"/api/v1/admin/device-mesh/stats?window_days=30",
		"",
		"",
	)
	recorder := httptest.NewRecorder()
	handler.adminStats(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d: %s",
			recorder.Code, http.StatusOK, recorder.Body.String())
	}
	var envelope struct {
		Data struct {
			WindowDays int `json:"window_days"`
			GroupCount int `json:"group_count"`
		} `json:"data"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if envelope.Data.WindowDays != 30 || envelope.Data.GroupCount != 1 {
		t.Fatalf("unexpected stats: %+v", envelope.Data)
	}
}
