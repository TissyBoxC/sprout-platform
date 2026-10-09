// Package service owns notification authoring, fan-out, inbox reads, and
// remote device message delivery.
package service

import (
	"context"
	"errors"
	"strings"

	runtimedomain "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/device_runtime/domain"
	"github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/notification/domain"
	"github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/notification/repository"
	"github.com/TissyBoxC/sprout-platform/services/device_platform/internal/platform/clock"
	"github.com/google/uuid"
)

// DeviceCommander is the narrow device-command surface used to deliver a
// guardian message to a bound device. It keeps the notification module from
// depending on device runtime persistence internals.
type DeviceCommander interface {
	CreateCommand(
		ctx context.Context,
		deviceID string,
		commandType runtimedomain.CommandType,
		requestedBy string,
		payload map[string]any,
	) (*runtimedomain.Command, error)
}

// auditRecorder is the narrow platform audit write surface.
type auditRecorder interface {
	RecordAudit(
		ctx context.Context,
		actorAccountID string,
		targetAccountID string,
		action string,
		detail map[string]any,
	) error
}

// Service owns notification lifecycle and delivery state.
type Service struct {
	repository repository.Repository
	commander  DeviceCommander
	audit      auditRecorder
	timeSource clock.Clock
}

// Options contains notification service dependencies.
type Options struct {
	Repository repository.Repository
	Commander  DeviceCommander
	Audit      auditRecorder
	Clock      clock.Clock
}

// New creates the notification service.
func New(options Options) (*Service, error) {
	if options.Repository == nil {
		return nil, errors.New("notification repository is required")
	}
	timeSource := options.Clock
	if timeSource == nil {
		timeSource = clock.SystemClock{}
	}
	return &Service{
		repository: options.Repository,
		commander:  options.Commander,
		audit:      options.Audit,
		timeSource: timeSource,
	}, nil
}

// Inbox returns one bounded page of the guardian's visible notifications.
func (s *Service) Inbox(
	ctx context.Context,
	parentAccountID string,
	limit int,
	cursor string,
	category string,
) (*domain.InboxPage, error) {
	now := s.timeSource.Now().UTC()
	if limit <= 0 {
		limit = domain.DefaultPageSize
	}
	if limit > domain.MaxPageSize {
		limit = domain.MaxPageSize
	}
	if category != "" && !domain.CategoryIsValid(category) {
		return nil, domain.ErrInvalidNotification
	}
	// Fetch one extra row to detect a following page without a second query.
	items, err := s.repository.ListInbox(
		ctx,
		parentAccountID,
		now,
		limit+1,
		strings.TrimSpace(cursor),
		strings.TrimSpace(category),
	)
	if err != nil {
		return nil, err
	}
	hasMore := len(items) > limit
	if hasMore {
		items = items[:limit]
	}
	unread, err := s.repository.CountUnread(ctx, parentAccountID, now)
	if err != nil {
		return nil, err
	}
	page := &domain.InboxPage{
		Items:       items,
		UnreadCount: unread,
		HasMore:     hasMore,
	}
	if hasMore && len(items) > 0 {
		page.NextCursor = items[len(items)-1].ID
	}
	return page, nil
}

// UnreadCount returns the bounded unread badge value.
func (s *Service) UnreadCount(
	ctx context.Context,
	parentAccountID string,
) (int, error) {
	count, err := s.repository.CountUnread(ctx, parentAccountID, s.timeSource.Now().UTC())
	if err != nil {
		return 0, err
	}
	return count, nil
}

// MarkRead marks one guardian-owned notification as read.
func (s *Service) MarkRead(
	ctx context.Context,
	parentAccountID string,
	notificationID string,
) error {
	notificationID = strings.TrimSpace(notificationID)
	if notificationID == "" {
		return domain.ErrInvalidNotification
	}
	err := s.repository.MarkRead(
		ctx,
		notificationID,
		parentAccountID,
		s.timeSource.Now().UTC(),
	)
	if errors.Is(err, repository.ErrNotFound) {
		return domain.ErrNotificationForbidden
	}
	return err
}

// MarkAllRead marks every unread notification for one guardian.
func (s *Service) MarkAllRead(
	ctx context.Context,
	parentAccountID string,
) error {
	return s.repository.MarkAllRead(ctx, parentAccountID, s.timeSource.Now().UTC())
}

// CreateAdministratorNotice validates, stores, and fans out an operator
// notification.
func (s *Service) CreateAdministratorNotice(
	ctx context.Context,
	actorAccountID string,
	input domain.AdministratorInput,
) (*domain.AdminItem, error) {
	notification, recipients, err := s.buildNotification(
		ctx,
		input,
		actorAccountID,
		domain.SourceAdministrator,
	)
	if err != nil {
		return nil, err
	}
	if err := s.persist(ctx, notification, recipients); err != nil {
		return nil, err
	}
	s.recordAudit(ctx, actorAccountID, notification, "notification.create")
	return s.adminItem(ctx, notification)
}

// SendFamilyMessage lets a guardian send a bounded message to a bound device.
//
// The message always lands in the guardian's own inbox; when SendToDevice is
// set and the device is online-capable, a display_message command is queued on
// the device command channel.
func (s *Service) SendFamilyMessage(
	ctx context.Context,
	parentAccountID string,
	input domain.GuardianMessageInput,
) (*domain.InboxItem, error) {
	deviceID := strings.TrimSpace(input.DeviceID)
	if !identifierPattern(deviceID) {
		return nil, domain.ErrInvalidNotification
	}
	owned, err := s.repository.ParentOwnsDevice(ctx, parentAccountID, deviceID)
	if err != nil {
		return nil, err
	}
	if !owned {
		return nil, domain.ErrNotificationForbidden
	}
	body := strings.TrimSpace(input.Body)
	if body == "" || domain.RuneCount(body) > domain.MaxBodyRunes {
		return nil, domain.ErrInvalidNotification
	}
	duration := input.DisplayDurationSeconds
	if duration <= 0 {
		duration = domain.DefaultDeviceMessageSeconds
	}
	if duration > domain.MaxDeviceMessageSeconds {
		return nil, domain.ErrInvalidNotification
	}
	channels := []string{domain.ChannelInApp}
	if input.SendToDevice {
		channels = append(channels, domain.ChannelDevice)
	}
	title, err := defaultFamilyMessageTitle(body)
	if err != nil {
		return nil, err
	}
	notification, recipients, err := s.buildNotification(ctx, domain.AdministratorInput{
		Category:               domain.CategoryFamilyMessage,
		Severity:               domain.SeverityInfo,
		Title:                  title,
		Body:                   body,
		Audience:               domain.AudienceFamilyDevices,
		Channels:               channels,
		ParentAccountID:        parentAccountID,
		DeviceID:               deviceID,
		DisplayDurationSeconds: duration,
	}, parentAccountID, domain.SourceGuardian)
	if err != nil {
		return nil, err
	}
	if err := s.persist(ctx, notification, recipients); err != nil {
		return nil, err
	}

	if input.SendToDevice && s.commander != nil {
		command, commandErr := s.commander.CreateCommand(
			ctx,
			deviceID,
			runtimedomain.CommandDisplayMessage,
			parentAccountID,
			map[string]any{
				"notification_id":  notification.ID,
				"title":            notification.Title,
				"body":             notification.Body,
				"severity":         notification.Severity,
				"duration_seconds": duration,
				"category":         notification.Category,
			},
		)
		if commandErr == nil && command != nil {
			if err := s.repository.RecordDeliveryCommand(
				ctx,
				notification.ID,
				parentAccountID,
				command.ID,
			); err != nil {
				return nil, err
			}
		} else if commandErr != nil {
			// The inbox entry is still valid; a device that is offline or has
			// no command channel simply reads it in the application instead.
			_ = s.repository.UpdateDeliveryStatus(
				ctx,
				notification.ID,
				parentAccountID,
				domain.ChannelDevice,
				domain.DeliveryStatusFailed,
				"device_unavailable",
				s.timeSource.Now().UTC(),
			)
		}
	}

	s.recordAudit(ctx, parentAccountID, notification, "notification.family_message")
	return &domain.InboxItem{
		ID:          notification.ID,
		Category:    notification.Category,
		Severity:    notification.Severity,
		Title:       notification.Title,
		Body:        notification.Body,
		DeviceID:    notification.DeviceID,
		Channels:    notification.Channels,
		PublishAt:   notification.PublishAt,
		ExpiresAt:   notification.ExpiresAt,
		Read:        false,
		ActionPath:  notification.ActionPath,
		ActionLabel: notification.ActionLabel,
	}, nil
}

// DeviceMessages returns the pending device display messages for one
// authenticated device so the firmware can render them locally.
func (s *Service) DeviceMessages(
	ctx context.Context,
	parentAccountID string,
	deviceID string,
	limit int,
) ([]domain.DeviceMessage, error) {
	if limit <= 0 || limit > 20 {
		limit = 20
	}
	// Reuse the admin list, filtered to this device's family messages.
	items, _, err := s.repository.ListAdmin(ctx, domain.AdminFilter{
		Category: domain.CategoryFamilyMessage,
		Page:     1,
		PageSize: limit,
	})
	if err != nil {
		return nil, err
	}
	now := s.timeSource.Now().UTC()
	messages := make([]domain.DeviceMessage, 0, len(items))
	for _, item := range items {
		if item.DeviceID != deviceID {
			continue
		}
		if item.ParentAccountID != "" && item.ParentAccountID != parentAccountID {
			continue
		}
		if item.ExpiresAt != nil && !item.ExpiresAt.After(now) {
			continue
		}
		duration := item.DisplayDurationSeconds
		if duration <= 0 {
			duration = domain.DefaultDeviceMessageSeconds
		}
		messages = append(messages, domain.DeviceMessage{
			ID:              item.ID,
			Title:           item.Title,
			Body:            item.Body,
			Severity:        item.Severity,
			DurationSeconds: duration,
			Category:        item.Category,
		})
	}
	return messages, nil
}

// AdminList returns a filtered, paged operator projection.
func (s *Service) AdminList(
	ctx context.Context,
	filter domain.AdminFilter,
) (*domain.AdminPage, error) {
	if filter.Category != "" && !domain.CategoryIsValid(filter.Category) {
		return nil, domain.ErrInvalidNotification
	}
	if filter.Audience != "" && !domain.AudienceIsValid(filter.Audience) {
		return nil, domain.ErrInvalidNotification
	}
	if filter.Query != "" && domain.RuneCount(filter.Query) > domain.MaxBodyRunes {
		return nil, domain.ErrInvalidNotification
	}
	page, pageSize := domain.ClampPage(filter.Page, filter.PageSize)
	filter.Page = page
	filter.PageSize = pageSize
	filter.Query = strings.TrimSpace(filter.Query)

	items, total, err := s.repository.ListAdmin(ctx, filter)
	if err != nil {
		return nil, err
	}
	totalPages := 0
	if total > 0 {
		totalPages = (total + pageSize - 1) / pageSize
	}
	return &domain.AdminPage{
		Items:      items,
		Total:      total,
		Page:       page,
		PageSize:   pageSize,
		TotalPages: totalPages,
	}, nil
}

// AdminGet returns one operator projection by id.
func (s *Service) AdminGet(
	ctx context.Context,
	notificationID string,
) (*domain.AdminItem, error) {
	notification, err := s.repository.Get(ctx, strings.TrimSpace(notificationID))
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, domain.ErrNotificationNotFound
		}
		return nil, err
	}
	return s.adminItem(ctx, notification)
}

// AdminDelete removes one notification and its delivery rows.
func (s *Service) AdminDelete(
	ctx context.Context,
	actorAccountID string,
	notificationID string,
) error {
	notificationID = strings.TrimSpace(notificationID)
	if notificationID == "" {
		return domain.ErrInvalidNotification
	}
	if err := s.repository.Delete(ctx, notificationID); err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return domain.ErrNotificationNotFound
		}
		return err
	}
	s.auditAction(ctx, actorAccountID, notificationID, "notification.delete", nil)
	return nil
}

// AdminStats returns the operator counters.
func (s *Service) AdminStats(ctx context.Context) (*domain.AdminStats, error) {
	// Reuse the admin list to compute counters from a bounded projection rather
	// than adding another aggregate query to the repository surface.
	items, total, err := s.repository.ListAdmin(ctx, domain.AdminFilter{
		Page:     1,
		PageSize: domain.MaxListLimit,
	})
	if err != nil {
		return nil, err
	}
	stats := &domain.AdminStats{Total: total}
	for _, item := range items {
		if item.Audience == domain.AudienceAllParents {
			stats.BroadcastCount++
		}
		stats.PendingDeliveryCount += item.DeliveryPending
		stats.FailedDeliveryCount += item.DeliveryFailed
	}
	return stats, nil
}

// buildNotification validates an input and resolves its recipient set.
func (s *Service) buildNotification(
	ctx context.Context,
	input domain.AdministratorInput,
	actorAccountID string,
	source string,
) (*domain.Notification, []string, error) {
	category := strings.TrimSpace(input.Category)
	if !domain.CategoryIsValid(category) {
		return nil, nil, domain.ErrInvalidNotification
	}
	severity := strings.TrimSpace(input.Severity)
	if severity == "" {
		severity = domain.SeverityInfo
	}
	if !domain.SeverityIsValid(severity) {
		return nil, nil, domain.ErrInvalidNotification
	}
	title := strings.TrimSpace(input.Title)
	if title == "" || domain.RuneCount(title) > domain.MaxTitleRunes {
		return nil, nil, domain.ErrInvalidNotification
	}
	body := strings.TrimSpace(input.Body)
	if body == "" || domain.RuneCount(body) > domain.MaxBodyRunes {
		return nil, nil, domain.ErrInvalidNotification
	}
	actionLabel := strings.TrimSpace(input.ActionLabel)
	if domain.RuneCount(actionLabel) > domain.MaxActionLabelRunes {
		return nil, nil, domain.ErrInvalidNotification
	}
	actionPath := strings.TrimSpace(input.ActionPath)
	if domain.RuneCount(actionPath) > domain.MaxActionPathRunes ||
		strings.Contains(actionPath, "://") {
		return nil, nil, domain.ErrInvalidNotification
	}
	audience := strings.TrimSpace(input.Audience)
	if audience == "" {
		audience = domain.AudienceParent
	}
	if !domain.AudienceIsValid(audience) {
		return nil, nil, domain.ErrInvalidNotification
	}
	channels, err := domain.NormalizeChannels(input.Channels)
	if err != nil {
		return nil, nil, err
	}
	duration := input.DisplayDurationSeconds
	if duration < 0 || duration > domain.MaxDeviceMessageSeconds {
		return nil, nil, domain.ErrInvalidNotification
	}
	if domain.HasChannel(channels, domain.ChannelDevice) && duration == 0 {
		duration = domain.DefaultDeviceMessageSeconds
	}

	now := s.timeSource.Now().UTC()
	expiresAt, err := domain.NormalizeExpiry(input.ExpiresAt, now)
	if err != nil {
		return nil, nil, err
	}

	notification := &domain.Notification{
		ID:                     uuid.NewString(),
		Category:               category,
		Severity:               severity,
		Title:                  title,
		Body:                   body,
		ActionPath:             actionPath,
		ActionLabel:            actionLabel,
		Audience:               audience,
		DeviceID:               strings.TrimSpace(input.DeviceID),
		DisplayDurationSeconds: duration,
		Channels:               channels,
		Source:                 source,
		PublishAt:              now,
		ExpiresAt:              expiresAt,
		CreatedAt:              now,
		UpdatedAt:              now,
	}
	if actorAccountID != "" && source == domain.SourceAdministrator {
		notification.CreatedBy = actorAccountID
	}

	var recipients []string
	switch audience {
	case domain.AudienceParent:
		parentAccountID := strings.TrimSpace(input.ParentAccountID)
		if !isUUID(parentAccountID) {
			return nil, nil, domain.ErrInvalidNotification
		}
		notification.ParentAccountID = parentAccountID
		recipients = []string{parentAccountID}
	case domain.AudienceAllParents:
		ids, err := s.repository.ActiveParentIDs(ctx)
		if err != nil {
			return nil, nil, err
		}
		recipients = ids
	case domain.AudienceFamilyDevices:
		parentAccountID := strings.TrimSpace(input.ParentAccountID)
		if !isUUID(parentAccountID) {
			return nil, nil, domain.ErrInvalidNotification
		}
		if notification.DeviceID != "" && !identifierPattern(notification.DeviceID) {
			return nil, nil, domain.ErrInvalidNotification
		}
		notification.ParentAccountID = parentAccountID
		recipients = []string{parentAccountID}
	}
	if len(recipients) == 0 {
		return nil, nil, domain.ErrInvalidNotification
	}
	return notification, recipients, nil
}

// persist writes the notification plus one delivery row per recipient and
// channel.
func (s *Service) persist(
	ctx context.Context,
	notification *domain.Notification,
	recipients []string,
) error {
	deliveries := make([]*domain.Delivery, 0, len(recipients)*len(notification.Channels))
	now := s.timeSource.Now().UTC()
	for _, parentAccountID := range recipients {
		for _, channel := range notification.Channels {
			status := domain.DeliveryStatusPending
			// The in-app inbox is readable as soon as the row exists.
			if channel == domain.ChannelInApp {
				status = domain.DeliveryStatusDelivered
			}
			deliveries = append(deliveries, &domain.Delivery{
				ID:              uuid.NewString(),
				NotificationID:  notification.ID,
				ParentAccountID: parentAccountID,
				Channel:         channel,
				Status:          status,
				CreatedAt:       now,
				UpdatedAt:       now,
			})
		}
	}
	return s.repository.Create(ctx, notification, deliveries)
}

// adminItem loads aggregate delivery counters for one notification.
func (s *Service) adminItem(
	ctx context.Context,
	notification *domain.Notification,
) (*domain.AdminItem, error) {
	items, _, err := s.repository.ListAdmin(ctx, domain.AdminFilter{
		Page:     1,
		PageSize: domain.MaxListLimit,
		Query:    "",
	})
	if err != nil {
		return nil, err
	}
	for index := range items {
		if items[index].ID == notification.ID {
			return &items[index], nil
		}
	}
	// Fall back to the authored copy with zeroed counters so a newly created
	// row is still returned even before any aggregate is visible.
	return &domain.AdminItem{Notification: *notification}, nil
}

func (s *Service) recordAudit(
	ctx context.Context,
	actorAccountID string,
	notification *domain.Notification,
	action string,
) {
	s.auditAction(ctx, actorAccountID, notification.ID, action, map[string]any{
		"category": notification.Category,
		"audience": notification.Audience,
		"channels": notification.Channels,
	})
}

func (s *Service) auditAction(
	ctx context.Context,
	actorAccountID string,
	targetID string,
	action string,
	detail map[string]any,
) {
	if s.audit == nil {
		return
	}
	_ = s.audit.RecordAudit(ctx, actorAccountID, targetID, action, detail)
}

func defaultFamilyMessageTitle(body string) (string, error) {
	runes := []rune(strings.TrimSpace(body))
	if len(runes) == 0 {
		return "", domain.ErrInvalidNotification
	}
	const prefixRunes = 16
	if len(runes) <= prefixRunes {
		return string(runes), nil
	}
	return string(runes[:prefixRunes]) + "…", nil
}

var identifierPattern = func(value string) bool {
	if len(value) < 3 || len(value) > 64 {
		return false
	}
	for index, char := range value {
		switch {
		case char >= 'a' && char <= 'z':
		case char >= '0' && char <= '9':
		case char == '_' || char == '-':
		default:
			return false
		}
		if index == 0 && !(char >= 'a' && char <= 'z') {
			return false
		}
	}
	return true
}

func isUUID(value string) bool {
	if len(value) != 36 {
		return false
	}
	_, err := uuid.Parse(value)
	return err == nil
}
