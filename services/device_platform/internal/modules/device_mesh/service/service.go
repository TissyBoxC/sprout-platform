// Package service contains the multi-device, family-linkage and content-sync use
// cases.
//
// Every operation is family-scoped: the caller passes the authenticated family
// id and the service never trusts a client-provided owner. Content converges by
// version so a family reaches eventual consistency, and a message routes to the
// device, a group, or the whole family. Deleting a device only removes its group
// membership: it never touches another device's content, so a family keeps its
// data when one device is removed.
package service

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/device_mesh/domain"
	"github.com/TissyBoxC/sprout-platform/services/device_platform/internal/platform/clock"
	"github.com/TissyBoxC/sprout-platform/services/device_platform/internal/platform/featurehealth"
	"github.com/google/uuid"
)

// Repository is the persistence surface the service depends on.
type Repository interface {
	CreateGroup(ctx context.Context, group domain.DeviceGroup) error
	DeleteGroup(ctx context.Context, familyID string, groupID string) error
	Group(ctx context.Context, familyID string, groupID string) (domain.DeviceGroup, bool, error)
	Groups(ctx context.Context, familyID string) ([]domain.DeviceGroup, error)

	AddMember(ctx context.Context, member domain.Member) error
	RemoveMember(ctx context.Context, familyID string, groupID string, deviceID string) error
	Members(ctx context.Context, familyID string, groupID string) ([]domain.Member, error)
	MemberGroups(ctx context.Context, familyID string, deviceID string) ([]string, error)

	CreateMessage(ctx context.Context, message domain.RoutedMessage) error
	Messages(ctx context.Context, familyID string, limit int) ([]domain.RoutedMessage, error)

	SaveContentState(ctx context.Context, state domain.ContentState) error
	ContentState(ctx context.Context, familyID string, contentKey string) (domain.ContentState, bool, error)
	ContentStates(ctx context.Context, familyID string) ([]domain.ContentState, error)

	AdminStats(ctx context.Context, windowDays int) (domain.AdminStats, error)
}

// Service owns device groups, routing and content sync.
type Service struct {
	repository Repository
	clock      clock.Clock
}

const capabilityFreshness = 24 * time.Hour

// Options contains device-mesh service dependencies.
type Options struct {
	Repository Repository
	Clock      clock.Clock
}

// MessageHistoryDefaultLimit and MessageHistoryMaxLimit bound a history read.
const (
	MessageHistoryDefaultLimit = 50
	MessageHistoryMaxLimit     = 200
)

// SyncInput is one content-sync report from a device.
type SyncInput struct {
	ContentKey    string
	LocalVersion  uint64
	RemoteVersion uint64
	Strategy      string
}

// New creates the device-mesh service.
func New(options Options) (*Service, error) {
	if options.Repository == nil {
		return nil, errors.New("device mesh repository is required")
	}
	timeSource := options.Clock
	if timeSource == nil {
		timeSource = clock.SystemClock{}
	}
	return &Service{repository: options.Repository, clock: timeSource}, nil
}

// CreateGroup validates and stores one device group.
func (s *Service) CreateGroup(
	ctx context.Context,
	familyID string,
	name string,
	routeScope string,
) (domain.DeviceGroup, error) {
	familyID = strings.TrimSpace(familyID)
	if familyID == "" {
		return domain.DeviceGroup{}, domain.ErrInvalidGroup
	}
	normalizedName, normalizedScope, err := domain.ValidateGroupInput(
		strings.TrimSpace(name),
		routeScope,
	)
	if err != nil {
		return domain.DeviceGroup{}, err
	}
	group := domain.DeviceGroup{
		ID:         uuid.NewString(),
		FamilyID:   familyID,
		Name:       normalizedName,
		RouteScope: normalizedScope,
	}
	if err := s.repository.CreateGroup(ctx, group); err != nil {
		return domain.DeviceGroup{}, err
	}
	group.CreatedAt = s.clock.Now().UTC()
	group.UpdatedAt = group.CreatedAt
	return group, nil
}

// DeleteGroup removes one group and its membership.
func (s *Service) DeleteGroup(
	ctx context.Context,
	familyID string,
	groupID string,
) error {
	familyID = strings.TrimSpace(familyID)
	groupID = strings.TrimSpace(groupID)
	if familyID == "" || groupID == "" {
		return domain.ErrInvalidGroup
	}
	return s.repository.DeleteGroup(ctx, familyID, groupID)
}

// Groups returns every group in a family.
func (s *Service) Groups(
	ctx context.Context,
	familyID string,
) ([]domain.DeviceGroup, error) {
	familyID = strings.TrimSpace(familyID)
	if familyID == "" {
		return nil, domain.ErrInvalidGroup
	}
	return s.repository.Groups(ctx, familyID)
}

// AddMember adds one device to a group, refusing a group that is already at its
// membership bound.
func (s *Service) AddMember(
	ctx context.Context,
	familyID string,
	groupID string,
	deviceID string,
) (domain.Member, error) {
	familyID = strings.TrimSpace(familyID)
	groupID = strings.TrimSpace(groupID)
	deviceID = strings.TrimSpace(deviceID)
	if familyID == "" || groupID == "" || deviceID == "" {
		return domain.Member{}, domain.ErrInvalidMember
	}
	_, ok, err := s.repository.Group(ctx, familyID, groupID)
	if err != nil {
		return domain.Member{}, err
	}
	if !ok {
		return domain.Member{}, domain.ErrGroupNotFound
	}
	members, err := s.repository.Members(ctx, familyID, groupID)
	if err != nil {
		return domain.Member{}, err
	}
	for _, member := range members {
		if member.DeviceID == deviceID {
			// An idempotent add keeps the existing membership.
			return member, nil
		}
	}
	if len(members) >= domain.GroupMemberMax {
		return domain.Member{}, domain.ErrInvalidMember
	}
	member := domain.Member{
		GroupID:  groupID,
		DeviceID: deviceID,
		FamilyID: familyID,
	}
	if err := s.repository.AddMember(ctx, member); err != nil {
		return domain.Member{}, err
	}
	member.AddedAt = s.clock.Now().UTC()
	return member, nil
}

// RemoveMember removes one device from a group. It only removes the membership;
// the device's other group assignments and data are untouched.
func (s *Service) RemoveMember(
	ctx context.Context,
	familyID string,
	groupID string,
	deviceID string,
) error {
	familyID = strings.TrimSpace(familyID)
	groupID = strings.TrimSpace(groupID)
	deviceID = strings.TrimSpace(deviceID)
	if familyID == "" || groupID == "" || deviceID == "" {
		return domain.ErrInvalidMember
	}
	return s.repository.RemoveMember(ctx, familyID, groupID, deviceID)
}

// Members returns every member of a group.
func (s *Service) Members(
	ctx context.Context,
	familyID string,
	groupID string,
) ([]domain.Member, error) {
	familyID = strings.TrimSpace(familyID)
	groupID = strings.TrimSpace(groupID)
	if familyID == "" || groupID == "" {
		return nil, domain.ErrInvalidMember
	}
	return s.repository.Members(ctx, familyID, groupID)
}

// RouteMessage validates and stores one routed message. A group route is only
// allowed when the target group exists in the family, so a message can never be
// routed to another family's group.
func (s *Service) RouteMessage(
	ctx context.Context,
	familyID string,
	message domain.RoutedMessage,
) (domain.RoutedMessage, error) {
	familyID = strings.TrimSpace(familyID)
	if familyID == "" {
		return domain.RoutedMessage{}, domain.ErrInvalidMessage
	}
	normalized, err := domain.ValidateMessage(message)
	if err != nil {
		return domain.RoutedMessage{}, err
	}
	if normalized.Scope == domain.ScopeGroup {
		if _, ok, err := s.repository.Group(ctx, familyID, normalized.Target); err != nil {
			return domain.RoutedMessage{}, err
		} else if !ok {
			return domain.RoutedMessage{}, domain.ErrGroupNotFound
		}
	}
	routed := domain.RoutedMessage{
		ID:           uuid.NewString(),
		FamilyID:     familyID,
		SourceDevice: strings.TrimSpace(normalized.SourceDevice),
		Scope:        normalized.Scope,
		Target:       strings.TrimSpace(normalized.Target),
		Body:         normalized.Body,
	}
	if err := s.repository.CreateMessage(ctx, routed); err != nil {
		return domain.RoutedMessage{}, err
	}
	routed.CreatedAt = s.clock.Now().UTC()
	return routed, nil
}

// ReportSync applies one content-sync report and returns the resulting state.
// A higher version always wins; a same-version tie under a defer strategy is
// recorded as a conflict and increments the conflict counter instead of
// silently applying, so a family reaches eventual consistency without losing a
// concurrent edit.
func (s *Service) ReportSync(
	ctx context.Context,
	familyID string,
	input SyncInput,
) (domain.ContentState, error) {
	familyID = strings.TrimSpace(familyID)
	contentKey := strings.TrimSpace(input.ContentKey)
	if familyID == "" || contentKey == "" {
		return domain.ContentState{}, domain.ErrInvalidSync
	}
	strategy := input.Strategy
	if strategy == "" {
		strategy = domain.ConflictRemoteWins
	}
	if !domain.SupportedConflict(strategy) {
		return domain.ContentState{}, domain.ErrInvalidSync
	}

	existing, ok, err := s.repository.ContentState(ctx, familyID, contentKey)
	if err != nil {
		return domain.ContentState{}, err
	}

	localVersion := input.LocalVersion
	if ok {
		localVersion = existing.Version
	}
	outcome := domain.SyncOutcome(strategy, localVersion, input.RemoteVersion)

	state := domain.ContentState{
		FamilyID:      familyID,
		ContentKey:    contentKey,
		Version:       localVersion,
		ConflictCount: 0,
		SyncState:     outcome,
	}
	if ok {
		state.ConflictCount = existing.ConflictCount
	}
	switch outcome {
	case domain.SyncApplied:
		// The winning version becomes the stored version so the next report
		// converges against it.
		if input.RemoteVersion > localVersion {
			state.Version = input.RemoteVersion
		} else {
			state.Version = input.LocalVersion
		}
	case domain.SyncConflict:
		state.ConflictCount++
	case domain.SyncFailed:
		// A failure keeps the stored version so a later retry can converge.
	default:
	}
	if err := s.repository.SaveContentState(ctx, state); err != nil {
		return domain.ContentState{}, err
	}
	state.UpdatedAt = s.clock.Now().UTC()
	return state, nil
}

// ContentStates returns every content-sync state in a family.
func (s *Service) ContentStates(
	ctx context.Context,
	familyID string,
) ([]domain.ContentState, error) {
	familyID = strings.TrimSpace(familyID)
	if familyID == "" {
		return nil, domain.ErrInvalidSync
	}
	return s.repository.ContentStates(ctx, familyID)
}

// MessageHistory returns the most recent routed messages in a family.
func (s *Service) MessageHistory(
	ctx context.Context,
	familyID string,
	limit int,
) ([]domain.RoutedMessage, error) {
	familyID = strings.TrimSpace(familyID)
	if familyID == "" {
		return nil, domain.ErrInvalidMessage
	}
	if limit <= 0 || limit > MessageHistoryMaxLimit {
		limit = MessageHistoryDefaultLimit
	}
	return s.repository.Messages(ctx, familyID, limit)
}

// AdminStats returns a bounded, windowed fleet summary.
func (s *Service) AdminStats(
	ctx context.Context,
	windowDays int,
) (domain.AdminStats, error) {
	if windowDays <= 0 || windowDays > 90 {
		windowDays = 7
	}
	return s.repository.AdminStats(ctx, windowDays)
}

// CheckReadiness returns aggregate multi-device health for active devices that
// advertise multi_device.
func (s *Service) CheckReadiness(
	ctx context.Context,
) (featurehealth.DeviceCapabilityReadiness, error) {
	reader, ok := s.repository.(interface {
		Readiness(
			context.Context,
			time.Duration,
		) (featurehealth.DeviceCapabilityReadiness, error)
	})
	if !ok {
		return featurehealth.DeviceCapabilityReadiness{
			Status: featurehealth.StatusUnavailable,
		}, nil
	}
	return reader.Readiness(ctx, capabilityFreshness)
}
