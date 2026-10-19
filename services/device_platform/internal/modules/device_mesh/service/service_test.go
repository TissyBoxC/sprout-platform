package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/device_mesh/domain"
	"github.com/TissyBoxC/sprout-platform/services/device_platform/internal/platform/featurehealth"
)

// fixedClock returns a stable instant so the stored stamps are deterministic.
type fixedClock struct{}

func (fixedClock) Now() time.Time {
	return time.Date(2026, time.October, 11, 9, 0, 0, 0, time.UTC)
}

type fakeRepository struct {
	groups   map[string]domain.DeviceGroup
	members  map[string][]domain.Member
	messages map[string]domain.RoutedMessage
	content  map[string]domain.ContentState
	stats    domain.AdminStats
}

type readinessRepository struct {
	*fakeRepository
	freshness time.Duration
	readiness featurehealth.DeviceCapabilityReadiness
}

func (r *readinessRepository) Readiness(
	_ context.Context,
	freshness time.Duration,
) (featurehealth.DeviceCapabilityReadiness, error) {
	r.freshness = freshness
	return r.readiness, nil
}

func newFakeRepository() *fakeRepository {
	return &fakeRepository{
		groups:   map[string]domain.DeviceGroup{},
		members:  map[string][]domain.Member{},
		messages: map[string]domain.RoutedMessage{},
		content:  map[string]domain.ContentState{},
	}
}

func contentKey(familyID string, key string) string {
	return familyID + "|" + key
}

func (r *fakeRepository) CreateGroup(
	_ context.Context,
	group domain.DeviceGroup,
) error {
	r.groups[group.ID] = group
	return nil
}

func (r *fakeRepository) DeleteGroup(
	_ context.Context,
	_ string,
	groupID string,
) error {
	if _, ok := r.groups[groupID]; !ok {
		return domain.ErrGroupNotFound
	}
	delete(r.groups, groupID)
	delete(r.members, groupID)
	return nil
}

func (r *fakeRepository) Group(
	_ context.Context,
	_ string,
	groupID string,
) (domain.DeviceGroup, bool, error) {
	group, ok := r.groups[groupID]
	if ok {
		group.MemberCount = len(r.members[groupID])
	}
	return group, ok, nil
}

func (r *fakeRepository) Groups(
	_ context.Context,
	_ string,
) ([]domain.DeviceGroup, error) {
	groups := make([]domain.DeviceGroup, 0, len(r.groups))
	for _, group := range r.groups {
		group.MemberCount = len(r.members[group.ID])
		groups = append(groups, group)
	}
	return groups, nil
}

func (r *fakeRepository) AddMember(
	_ context.Context,
	member domain.Member,
) error {
	r.members[member.GroupID] = append(r.members[member.GroupID], member)
	return nil
}

func (r *fakeRepository) RemoveMember(
	_ context.Context,
	_ string,
	groupID string,
	deviceID string,
) error {
	members := r.members[groupID]
	for index, member := range members {
		if member.DeviceID == deviceID {
			r.members[groupID] = append(members[:index], members[index+1:]...)
			return nil
		}
	}
	return domain.ErrMemberNotFound
}

func (r *fakeRepository) Members(
	_ context.Context,
	_ string,
	groupID string,
) ([]domain.Member, error) {
	return r.members[groupID], nil
}

func (r *fakeRepository) MemberGroups(
	_ context.Context,
	_ string,
	deviceID string,
) ([]string, error) {
	groups := make([]string, 0, 4)
	for groupID, members := range r.members {
		for _, member := range members {
			if member.DeviceID == deviceID {
				groups = append(groups, groupID)
			}
		}
	}
	return groups, nil
}

func (r *fakeRepository) CreateMessage(
	_ context.Context,
	message domain.RoutedMessage,
) error {
	r.messages[message.ID] = message
	return nil
}

func (r *fakeRepository) Messages(
	_ context.Context,
	_ string,
	_ int,
) ([]domain.RoutedMessage, error) {
	messages := make([]domain.RoutedMessage, 0, len(r.messages))
	for _, message := range r.messages {
		messages = append(messages, message)
	}
	return messages, nil
}

func (r *fakeRepository) SaveContentState(
	_ context.Context,
	state domain.ContentState,
) error {
	r.content[contentKey(state.FamilyID, state.ContentKey)] = state
	return nil
}

func (r *fakeRepository) ContentState(
	_ context.Context,
	familyID string,
	key string,
) (domain.ContentState, bool, error) {
	state, ok := r.content[contentKey(familyID, key)]
	return state, ok, nil
}

func (r *fakeRepository) ContentStates(
	_ context.Context,
	_ string,
) ([]domain.ContentState, error) {
	states := make([]domain.ContentState, 0, len(r.content))
	for _, state := range r.content {
		states = append(states, state)
	}
	return states, nil
}

func (r *fakeRepository) AdminStats(
	_ context.Context,
	windowDays int,
) (domain.AdminStats, error) {
	stats := r.stats
	stats.WindowDays = windowDays
	return stats, nil
}

func newService(t *testing.T, repository Repository) *Service {
	t.Helper()
	service, err := New(Options{Repository: repository, Clock: fixedClock{}})
	if err != nil {
		t.Fatalf("new service: %v", err)
	}
	return service
}

func TestCreateGroupValidates(t *testing.T) {
	service := newService(t, newFakeRepository())
	group, err := service.CreateGroup(context.Background(), "family-1", "客厅组", domain.ScopeGroup)
	if err != nil {
		t.Fatalf("create group: %v", err)
	}
	if group.ID == "" || group.RouteScope != domain.ScopeGroup {
		t.Fatalf("unexpected group: %+v", group)
	}

	// An empty name or a device scope is refused for a group.
	if _, err := service.CreateGroup(context.Background(), "family-1", "", domain.ScopeGroup); !errors.Is(err, domain.ErrInvalidGroup) {
		t.Fatalf("expected ErrInvalidGroup, got %v", err)
	}
	if _, err := service.CreateGroup(context.Background(), "family-1", "客厅组", domain.ScopeDevice); !errors.Is(err, domain.ErrInvalidGroup) {
		t.Fatalf("expected ErrInvalidGroup for a device scope, got %v", err)
	}
}

func TestMembershipLifecycle(t *testing.T) {
	repository := newFakeRepository()
	service := newService(t, repository)
	group, err := service.CreateGroup(context.Background(), "family-1", "客厅组", domain.ScopeGroup)
	if err != nil {
		t.Fatalf("create group: %v", err)
	}

	member, err := service.AddMember(context.Background(), "family-1", group.ID, "device-1")
	if err != nil {
		t.Fatalf("add member: %v", err)
	}
	if member.DeviceID != "device-1" {
		t.Fatalf("unexpected member: %+v", member)
	}

	// Adding a member twice is idempotent.
	if _, err := service.AddMember(context.Background(), "family-1", group.ID, "device-1"); err != nil {
		t.Fatalf("re-add member: %v", err)
	}
	members, err := service.Members(context.Background(), "family-1", group.ID)
	if err != nil {
		t.Fatalf("members: %v", err)
	}
	if len(members) != 1 {
		t.Fatalf("expected one member, got %d", len(members))
	}

	// Adding to a missing group is a not-found.
	if _, err := service.AddMember(context.Background(), "family-1", "missing", "device-2"); !errors.Is(err, domain.ErrGroupNotFound) {
		t.Fatalf("expected ErrGroupNotFound, got %v", err)
	}

	if err := service.RemoveMember(context.Background(), "family-1", group.ID, "device-1"); err != nil {
		t.Fatalf("remove member: %v", err)
	}
	members, err = service.Members(context.Background(), "family-1", group.ID)
	if err != nil {
		t.Fatalf("members: %v", err)
	}
	if len(members) != 0 {
		t.Fatalf("expected no members, got %d", len(members))
	}
	if err := service.RemoveMember(context.Background(), "family-1", group.ID, "device-1"); !errors.Is(err, domain.ErrMemberNotFound) {
		t.Fatalf("expected ErrMemberNotFound, got %v", err)
	}
}

func TestRemoveMemberKeepsOtherData(t *testing.T) {
	repository := newFakeRepository()
	service := newService(t, repository)
	group, err := service.CreateGroup(context.Background(), "family-1", "客厅组", domain.ScopeGroup)
	if err != nil {
		t.Fatalf("create group: %v", err)
	}
	if _, err := service.AddMember(context.Background(), "family-1", group.ID, "device-1"); err != nil {
		t.Fatalf("add device-1: %v", err)
	}
	if _, err := service.AddMember(context.Background(), "family-1", group.ID, "device-2"); err != nil {
		t.Fatalf("add device-2: %v", err)
	}
	// Removing one device leaves the other member intact.
	if err := service.RemoveMember(context.Background(), "family-1", group.ID, "device-1"); err != nil {
		t.Fatalf("remove device-1: %v", err)
	}
	members, err := service.Members(context.Background(), "family-1", group.ID)
	if err != nil {
		t.Fatalf("members: %v", err)
	}
	if len(members) != 1 || members[0].DeviceID != "device-2" {
		t.Fatalf("expected device-2 to remain, got %+v", members)
	}
}

func TestAddMemberRespectsBound(t *testing.T) {
	repository := newFakeRepository()
	service := newService(t, repository)
	group, err := service.CreateGroup(context.Background(), "family-1", "客厅组", domain.ScopeGroup)
	if err != nil {
		t.Fatalf("create group: %v", err)
	}
	for index := 0; index < domain.GroupMemberMax; index++ {
		deviceID := "device-" + string(rune('a'+index))
		if _, err := service.AddMember(context.Background(), "family-1", group.ID, deviceID); err != nil {
			t.Fatalf("add %s: %v", deviceID, err)
		}
	}
	if _, err := service.AddMember(context.Background(), "family-1", group.ID, "device-overflow"); !errors.Is(err, domain.ErrInvalidMember) {
		t.Fatalf("expected ErrInvalidMember at the bound, got %v", err)
	}
}

func TestRouteMessage(t *testing.T) {
	repository := newFakeRepository()
	service := newService(t, repository)
	group, err := service.CreateGroup(context.Background(), "family-1", "客厅组", domain.ScopeGroup)
	if err != nil {
		t.Fatalf("create group: %v", err)
	}

	// A group-scoped message needs an existing group.
	message, err := service.RouteMessage(context.Background(), "family-1", domain.RoutedMessage{
		SourceDevice: "device-1",
		Scope:        domain.ScopeGroup,
		Target:       group.ID,
		Body:         "晚上一起听故事",
	})
	if err != nil {
		t.Fatalf("route message: %v", err)
	}
	if message.ID == "" || message.Scope != domain.ScopeGroup {
		t.Fatalf("unexpected message: %+v", message)
	}
	if _, err := service.RouteMessage(context.Background(), "family-1", domain.RoutedMessage{
		Scope:  domain.ScopeGroup,
		Target: "missing",
		Body:   "hello",
	}); !errors.Is(err, domain.ErrGroupNotFound) {
		t.Fatalf("expected ErrGroupNotFound, got %v", err)
	}

	// An empty body is refused.
	if _, err := service.RouteMessage(context.Background(), "family-1", domain.RoutedMessage{
		Scope:  domain.ScopeFamily,
		Target: "family-1",
		Body:   "",
	}); !errors.Is(err, domain.ErrInvalidMessage) {
		t.Fatalf("expected ErrInvalidMessage, got %v", err)
	}
}

func TestReportSyncConverges(t *testing.T) {
	repository := newFakeRepository()
	service := newService(t, repository)

	// A newer remote version applies and becomes the stored version.
	state, err := service.ReportSync(context.Background(), "family-1", SyncInput{
		ContentKey:    "theme-pack",
		LocalVersion:  2,
		RemoteVersion: 5,
		Strategy:      domain.ConflictRemoteWins,
	})
	if err != nil {
		t.Fatalf("report sync: %v", err)
	}
	if state.SyncState != domain.SyncApplied || state.Version != 5 {
		t.Fatalf("unexpected state: %+v", state)
	}

	// A same-version tie under defer is a conflict and increments the counter,
	// not a silent apply.
	conflict, err := service.ReportSync(context.Background(), "family-1", SyncInput{
		ContentKey:    "theme-pack",
		LocalVersion:  5,
		RemoteVersion: 5,
		Strategy:      domain.ConflictDefer,
	})
	if err != nil {
		t.Fatalf("report conflict: %v", err)
	}
	if conflict.SyncState != domain.SyncConflict || conflict.ConflictCount != 1 {
		t.Fatalf("expected a conflict with count 1, got %+v", conflict)
	}

	// A later higher remote version resolves the conflict and applies.
	resolved, err := service.ReportSync(context.Background(), "family-1", SyncInput{
		ContentKey:    "theme-pack",
		LocalVersion:  5,
		RemoteVersion: 6,
		Strategy:      domain.ConflictDefer,
	})
	if err != nil {
		t.Fatalf("report resolve: %v", err)
	}
	if resolved.SyncState != domain.SyncApplied || resolved.Version != 6 {
		t.Fatalf("expected the conflict to resolve, got %+v", resolved)
	}

	// An invalid strategy is refused.
	if _, err := service.ReportSync(context.Background(), "family-1", SyncInput{
		ContentKey: "theme-pack",
		Strategy:   "wild",
	}); !errors.Is(err, domain.ErrInvalidSync) {
		t.Fatalf("expected ErrInvalidSync, got %v", err)
	}
}

func TestAdminStatsWindowClamp(t *testing.T) {
	repository := newFakeRepository()
	repository.stats = domain.AdminStats{GroupCount: 2}
	service := newService(t, repository)
	stats, err := service.AdminStats(context.Background(), 0)
	if err != nil {
		t.Fatalf("admin stats: %v", err)
	}
	if stats.WindowDays != 7 || stats.GroupCount != 2 {
		t.Fatalf("unexpected stats: %+v", stats)
	}
}

func TestCheckReadinessReturnsRepositoryHealth(t *testing.T) {
	repository := &readinessRepository{
		fakeRepository: newFakeRepository(),
		readiness: featurehealth.DeviceCapabilityReadiness{
			Status: featurehealth.StatusDegraded,
			Facts: featurehealth.DeviceCapabilityFacts{
				ExpectedDevices: 2,
				ReportedDevices: 1,
			},
		},
	}
	service := newService(t, repository)
	readiness, err := service.CheckReadiness(context.Background())
	if err != nil {
		t.Fatalf("check readiness: %v", err)
	}
	if readiness.Status != featurehealth.StatusDegraded ||
		repository.freshness != capabilityFreshness {
		t.Fatalf("readiness = %+v, freshness = %s", readiness, repository.freshness)
	}
}

func TestCheckReadinessFailsClosedWithoutCapabilityReader(t *testing.T) {
	service := newService(t, newFakeRepository())
	readiness, err := service.CheckReadiness(context.Background())
	if err != nil {
		t.Fatalf("check readiness: %v", err)
	}
	if readiness.Status != featurehealth.StatusUnavailable {
		t.Fatalf("status = %q, want unavailable", readiness.Status)
	}
}
