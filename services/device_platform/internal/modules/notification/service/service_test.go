package service

import (
	"context"
	"errors"
	"sort"
	"testing"
	"time"

	runtimedomain "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/device_runtime/domain"
	"github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/notification/domain"
	"github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/notification/repository"
)

const (
	testParentID = "11111111-1111-4111-8111-111111111111"
	testDeviceID = "sprout_device_abc"
)

type memoryRepository struct {
	notifications map[string]*domain.Notification
	deliveries    []*domain.Delivery
	devices       map[string]string // deviceID -> parentAccountID
	healthErr     error
}

func newMemoryRepository() *memoryRepository {
	return &memoryRepository{
		notifications: map[string]*domain.Notification{},
		devices:       map[string]string{testDeviceID: testParentID},
	}
}

func (m *memoryRepository) Create(
	_ context.Context,
	notification *domain.Notification,
	deliveries []*domain.Delivery,
) error {
	copyNotification := *notification
	m.notifications[notification.ID] = &copyNotification
	m.deliveries = append(m.deliveries, deliveries...)
	return nil
}

func (m *memoryRepository) Get(
	_ context.Context,
	notificationID string,
) (*domain.Notification, error) {
	notification, ok := m.notifications[notificationID]
	if !ok {
		return nil, repository.ErrNotFound
	}
	copyNotification := *notification
	return &copyNotification, nil
}

func (m *memoryRepository) ListInbox(
	_ context.Context,
	parentAccountID string,
	now time.Time,
	limit int,
	_ string,
	category string,
) ([]domain.InboxItem, error) {
	items := make([]domain.InboxItem, 0)
	for _, delivery := range m.deliveries {
		if delivery.ParentAccountID != parentAccountID ||
			delivery.Channel != domain.ChannelInApp ||
			delivery.Status == domain.DeliveryStatusExpired {
			continue
		}
		notification, ok := m.notifications[delivery.NotificationID]
		if !ok || notification.PublishAt.After(now) {
			continue
		}
		if notification.ExpiresAt != nil && !notification.ExpiresAt.After(now) {
			continue
		}
		if category != "" && notification.Category != category {
			continue
		}
		items = append(items, domain.InboxItem{
			ID:       notification.ID,
			Category: notification.Category,
			Title:    notification.Title,
			Body:     notification.Body,
			Read:     delivery.Status == domain.DeliveryStatusRead,
			Channels: notification.Channels,
		})
	}
	sort.Slice(items, func(i, j int) bool { return items[i].ID > items[j].ID })
	if len(items) > limit {
		items = items[:limit]
	}
	return items, nil
}

func (m *memoryRepository) CountUnread(
	_ context.Context,
	parentAccountID string,
	_ time.Time,
) (int, error) {
	count := 0
	for _, delivery := range m.deliveries {
		if delivery.ParentAccountID == parentAccountID &&
			delivery.Channel == domain.ChannelInApp &&
			delivery.Status == domain.DeliveryStatusDelivered {
			count++
		}
	}
	return count, nil
}

func (m *memoryRepository) MarkRead(
	_ context.Context,
	notificationID string,
	parentAccountID string,
	_ time.Time,
) error {
	for _, delivery := range m.deliveries {
		if delivery.NotificationID == notificationID &&
			delivery.ParentAccountID == parentAccountID &&
			delivery.Channel == domain.ChannelInApp {
			delivery.Status = domain.DeliveryStatusRead
			return nil
		}
	}
	return repository.ErrNotFound
}

func (m *memoryRepository) MarkAllRead(
	_ context.Context,
	parentAccountID string,
	_ time.Time,
) error {
	for _, delivery := range m.deliveries {
		if delivery.ParentAccountID == parentAccountID &&
			delivery.Channel == domain.ChannelInApp {
			delivery.Status = domain.DeliveryStatusRead
		}
	}
	return nil
}

func (m *memoryRepository) Delete(_ context.Context, notificationID string) error {
	if _, ok := m.notifications[notificationID]; !ok {
		return repository.ErrNotFound
	}
	delete(m.notifications, notificationID)
	kept := m.deliveries[:0]
	for _, delivery := range m.deliveries {
		if delivery.NotificationID != notificationID {
			kept = append(kept, delivery)
		}
	}
	m.deliveries = kept
	return nil
}

func (m *memoryRepository) ListAdmin(
	_ context.Context,
	filter domain.AdminFilter,
) ([]domain.AdminItem, int, error) {
	items := make([]domain.AdminItem, 0, len(m.notifications))
	for _, notification := range m.notifications {
		if filter.Category != "" && notification.Category != filter.Category {
			continue
		}
		item := domain.AdminItem{Notification: *notification}
		for _, delivery := range m.deliveries {
			if delivery.NotificationID != notification.ID {
				continue
			}
			item.DeliveryTotal++
			switch delivery.Status {
			case domain.DeliveryStatusPending:
				item.DeliveryPending++
			case domain.DeliveryStatusDelivered:
				item.DeliveryDelivered++
			case domain.DeliveryStatusRead:
				item.DeliveryRead++
			case domain.DeliveryStatusFailed:
				item.DeliveryFailed++
			}
		}
		items = append(items, item)
	}
	sort.Slice(items, func(i, j int) bool { return items[i].ID < items[j].ID })
	return items, len(items), nil
}

func (m *memoryRepository) ListDeviceMessages(
	_ context.Context,
	parentAccountID string,
	deviceID string,
	now time.Time,
	limit int,
) ([]domain.DeviceMessage, error) {
	messages := make([]domain.DeviceMessage, 0)
	for _, delivery := range m.deliveries {
		if delivery.ParentAccountID != parentAccountID ||
			delivery.Channel != domain.ChannelDevice {
			continue
		}
		notification, ok := m.notifications[delivery.NotificationID]
		if !ok || notification.DeviceID != deviceID ||
			notification.PublishAt.After(now) {
			continue
		}
		if notification.ExpiresAt != nil && !notification.ExpiresAt.After(now) {
			continue
		}
		duration := notification.DisplayDurationSeconds
		if duration <= 0 {
			duration = domain.DefaultDeviceMessageSeconds
		}
		messages = append(messages, domain.DeviceMessage{
			ID:              notification.ID,
			Title:           notification.Title,
			Body:            notification.Body,
			Severity:        notification.Severity,
			DurationSeconds: duration,
			Category:        notification.Category,
		})
	}
	sort.Slice(messages, func(i, j int) bool {
		return messages[i].ID > messages[j].ID
	})
	if len(messages) > limit {
		messages = messages[:limit]
	}
	return messages, nil
}

func (m *memoryRepository) MarkDeliveryDeliveredByCommand(
	_ context.Context,
	commandID string,
	deviceID string,
	now time.Time,
) error {
	for _, delivery := range m.deliveries {
		notification, ok := m.notifications[delivery.NotificationID]
		if ok &&
			delivery.CommandID == commandID &&
			notification.DeviceID == deviceID &&
			delivery.Channel == domain.ChannelDevice &&
			(delivery.Status == domain.DeliveryStatusPending ||
				delivery.Status == domain.DeliveryStatusFailed) {
			delivery.Status = domain.DeliveryStatusDelivered
			if delivery.DeliveredAt == nil {
				delivery.DeliveredAt = &now
			}
			delivery.UpdatedAt = now
			return nil
		}
	}
	return repository.ErrNotFound
}

func (m *memoryRepository) ActiveParentIDs(_ context.Context) ([]string, error) {
	return []string{testParentID}, nil
}

func (m *memoryRepository) ParentOwnsDevice(
	_ context.Context,
	parentAccountID string,
	deviceID string,
) (bool, error) {
	return m.devices[deviceID] == parentAccountID, nil
}

func (m *memoryRepository) RecordDeliveryCommand(
	_ context.Context,
	notificationID string,
	parentAccountID string,
	commandID string,
) error {
	for _, delivery := range m.deliveries {
		if delivery.NotificationID == notificationID &&
			delivery.ParentAccountID == parentAccountID &&
			delivery.Channel == domain.ChannelDevice {
			delivery.CommandID = commandID
			return nil
		}
	}
	return repository.ErrNotFound
}

func (m *memoryRepository) UpdateDeliveryStatus(
	_ context.Context,
	notificationID string,
	parentAccountID string,
	channel string,
	status string,
	failureCode string,
	_ time.Time,
) error {
	for _, delivery := range m.deliveries {
		if delivery.NotificationID == notificationID &&
			delivery.ParentAccountID == parentAccountID &&
			delivery.Channel == channel {
			delivery.Status = status
			delivery.FailureCode = failureCode
			if status == domain.DeliveryStatusFailed {
				delivery.RetryCount++
			}
			return nil
		}
	}
	return nil
}

func (m *memoryRepository) RetireDelivery(
	_ context.Context,
	notificationID string,
	parentAccountID string,
	channel string,
	status string,
	failureCode string,
	_ time.Time,
) error {
	for _, delivery := range m.deliveries {
		if delivery.NotificationID == notificationID &&
			delivery.ParentAccountID == parentAccountID &&
			delivery.Channel == channel {
			delivery.Status = status
			delivery.FailureCode = failureCode
			return nil
		}
	}
	return nil
}

func (m *memoryRepository) PendingDeliveries(
	_ context.Context,
	channel string,
	limit int,
) ([]*domain.Delivery, error) {
	result := make([]*domain.Delivery, 0)
	for _, delivery := range m.deliveries {
		if delivery.Channel == channel &&
			(delivery.Status == domain.DeliveryStatusPending ||
				(delivery.Status == domain.DeliveryStatusFailed &&
					delivery.FailureCode == domain.DeliveryFailureProviderError)) &&
			delivery.RetryCount < 20 {
			result = append(result, delivery)
		}
	}
	if len(result) > limit {
		result = result[:limit]
	}
	return result, nil
}

func (m *memoryRepository) DeliveryHealth(
	_ context.Context,
) (domain.DeliveryHealth, error) {
	if m.healthErr != nil {
		return domain.DeliveryHealth{}, m.healthErr
	}
	var health domain.DeliveryHealth
	for _, delivery := range m.deliveries {
		switch delivery.Status {
		case domain.DeliveryStatusPending:
			health.PendingCount++
		case domain.DeliveryStatusFailed:
			health.FailedCount++
		}
	}
	return health, nil
}

type fakeDeliverySender struct {
	sent int
	err  error
}

func (f *fakeDeliverySender) Send(
	_ context.Context,
	_ *domain.Notification,
	_ *domain.Delivery,
) error {
	if f.err != nil {
		return f.err
	}
	f.sent++
	return nil
}

type fakeCommander struct {
	commands []*runtimedomain.Command
	err      error
}

func (f *fakeCommander) CreateCommand(
	_ context.Context,
	deviceID string,
	commandType runtimedomain.CommandType,
	requestedBy string,
	payload map[string]any,
) (*runtimedomain.Command, error) {
	if f.err != nil {
		return nil, f.err
	}
	command := &runtimedomain.Command{
		ID:          "command-1",
		DeviceID:    deviceID,
		Type:        commandType,
		Payload:     payload,
		RequestedBy: requestedBy,
	}
	f.commands = append(f.commands, command)
	return command, nil
}

func newTestService(t *testing.T, repo *memoryRepository, commander *fakeCommander) *Service {
	t.Helper()
	service, err := New(Options{
		Repository: repo,
		Commander:  commander,
		Clock:      fixedClock{now: time.Date(2026, 10, 9, 12, 0, 0, 0, time.UTC)},
	})
	if err != nil {
		t.Fatalf("create service: %v", err)
	}
	return service
}

type fixedClock struct{ now time.Time }

func (c fixedClock) Now() time.Time { return c.now }

func TestCreateAdministratorNoticeFansOutPerChannel(t *testing.T) {
	repo := newMemoryRepository()
	service := newTestService(t, repo, &fakeCommander{})
	item, err := service.CreateAdministratorNotice(context.Background(), "", domain.AdministratorInput{
		Category:        domain.CategorySystemAnnouncement,
		Severity:        domain.SeverityInfo,
		Title:           "服务升级",
		Body:            "平台将于今晚升级。",
		Audience:        domain.AudienceParent,
		Channels:        []string{domain.ChannelInApp, domain.ChannelPush},
		ParentAccountID: testParentID,
	})
	if err != nil {
		t.Fatalf("create notice: %v", err)
	}
	if item == nil || item.ID == "" {
		t.Fatalf("expected a notification id")
	}
	if item.DeliveryTotal != 2 {
		t.Fatalf("expected 2 deliveries, got %d", item.DeliveryTotal)
	}
}

func TestCreateAdministratorNoticeRejectsOversizedTitle(t *testing.T) {
	repo := newMemoryRepository()
	service := newTestService(t, repo, &fakeCommander{})
	longTitle := make([]rune, domain.MaxTitleRunes+1)
	for index := range longTitle {
		longTitle[index] = '萌'
	}
	_, err := service.CreateAdministratorNotice(context.Background(), "", domain.AdministratorInput{
		Category:        domain.CategorySystemAnnouncement,
		Title:           string(longTitle),
		Body:            "正文",
		Audience:        domain.AudienceParent,
		Channels:        []string{domain.ChannelInApp},
		ParentAccountID: testParentID,
	})
	if !errors.Is(err, domain.ErrInvalidNotification) {
		t.Fatalf("expected ErrInvalidNotification, got %v", err)
	}
}

func TestInboxReportsUnreadAndCursor(t *testing.T) {
	repo := newMemoryRepository()
	service := newTestService(t, repo, &fakeCommander{})
	for index := 0; index < 3; index++ {
		if _, err := service.CreateAdministratorNotice(context.Background(), "", domain.AdministratorInput{
			Category:        domain.CategorySystemAnnouncement,
			Title:           "通知",
			Body:            "内容",
			Audience:        domain.AudienceParent,
			Channels:        []string{domain.ChannelInApp},
			ParentAccountID: testParentID,
		}); err != nil {
			t.Fatalf("seed notice: %v", err)
		}
	}
	page, err := service.Inbox(context.Background(), testParentID, 2, "", "")
	if err != nil {
		t.Fatalf("inbox: %v", err)
	}
	if len(page.Items) != 2 || !page.HasMore {
		t.Fatalf("expected 2 items with more pages, got %d hasMore=%v", len(page.Items), page.HasMore)
	}
	if page.UnreadCount != 3 {
		t.Fatalf("expected 3 unread, got %d", page.UnreadCount)
	}
	if page.NextCursor == "" {
		t.Fatalf("expected a next cursor")
	}
}

func TestMarkReadRejectsForeignNotification(t *testing.T) {
	repo := newMemoryRepository()
	service := newTestService(t, repo, &fakeCommander{})
	if _, err := service.CreateAdministratorNotice(context.Background(), "", domain.AdministratorInput{
		Category:        domain.CategorySystemAnnouncement,
		Title:           "通知",
		Body:            "内容",
		Audience:        domain.AudienceParent,
		Channels:        []string{domain.ChannelInApp},
		ParentAccountID: testParentID,
	}); err != nil {
		t.Fatalf("seed notice: %v", err)
	}
	err := service.MarkRead(context.Background(), "22222222-2222-4222-8222-222222222222", "unknown-id")
	if !errors.Is(err, domain.ErrNotificationForbidden) {
		t.Fatalf("expected ErrNotificationForbidden, got %v", err)
	}
}

func TestSendFamilyMessageRequiresDeviceOwnership(t *testing.T) {
	repo := newMemoryRepository()
	service := newTestService(t, repo, &fakeCommander{})
	_, err := service.SendFamilyMessage(context.Background(), testParentID, domain.GuardianMessageInput{
		DeviceID:     "sprout_device_other",
		Body:         "吃饭啦",
		SendToDevice: true,
	})
	if !errors.Is(err, domain.ErrNotificationForbidden) {
		t.Fatalf("expected ErrNotificationForbidden, got %v", err)
	}
}

func TestSendFamilyMessageQueuesDisplayCommand(t *testing.T) {
	repo := newMemoryRepository()
	commander := &fakeCommander{}
	service := newTestService(t, repo, commander)
	item, err := service.SendFamilyMessage(context.Background(), testParentID, domain.GuardianMessageInput{
		DeviceID:               testDeviceID,
		Body:                   "记得喝水哦",
		DisplayDurationSeconds: 20,
		SendToDevice:           true,
	})
	if err != nil {
		t.Fatalf("send family message: %v", err)
	}
	if item == nil || item.Category != domain.CategoryFamilyMessage {
		t.Fatalf("expected a family message item, got %#v", item)
	}
	if len(commander.commands) != 1 {
		t.Fatalf("expected 1 display command, got %d", len(commander.commands))
	}
	command := commander.commands[0]
	if command.Type != runtimedomain.CommandDisplayMessage {
		t.Fatalf("expected display_message, got %q", command.Type)
	}
	if command.Payload["duration_seconds"] != 20 {
		t.Fatalf("expected duration 20, got %v", command.Payload["duration_seconds"])
	}
}

func TestSendFamilyMessageDegradesWhenDeviceUnavailable(t *testing.T) {
	repo := newMemoryRepository()
	commander := &fakeCommander{err: errors.New("device offline")}
	service := newTestService(t, repo, commander)
	item, err := service.SendFamilyMessage(context.Background(), testParentID, domain.GuardianMessageInput{
		DeviceID:     testDeviceID,
		Body:         "晚安",
		SendToDevice: true,
	})
	if err != nil {
		t.Fatalf("expected graceful degradation, got %v", err)
	}
	if item == nil {
		t.Fatalf("expected an inbox item even when the device is offline")
	}
	found := false
	for _, delivery := range repo.deliveries {
		if delivery.Channel == domain.ChannelDevice &&
			delivery.Status == domain.DeliveryStatusFailed {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected the device delivery to be marked failed")
	}
}

func TestDeviceMessagesOnlyReturnsTargetDevice(t *testing.T) {
	repo := newMemoryRepository()
	repo.devices["sprout_device_other"] = testParentID
	service := newTestService(t, repo, &fakeCommander{})

	for _, deviceID := range []string{testDeviceID, "sprout_device_other"} {
		if _, err := service.SendFamilyMessage(
			context.Background(),
			testParentID,
			domain.GuardianMessageInput{
				DeviceID:     deviceID,
				Body:         "给" + deviceID + "的留言",
				SendToDevice: true,
			},
		); err != nil {
			t.Fatalf("send message to %s: %v", deviceID, err)
		}
	}

	messages, err := service.DeviceMessages(
		context.Background(),
		testParentID,
		testDeviceID,
		20,
	)
	if err != nil {
		t.Fatalf("device messages: %v", err)
	}
	if len(messages) != 1 {
		t.Fatalf("expected exactly one target-device message, got %d", len(messages))
	}
	if messages[0].Body != "给"+testDeviceID+"的留言" {
		t.Fatalf("unexpected message body %q", messages[0].Body)
	}
}

func TestDeviceMessagesFiltersExpiredNotifications(t *testing.T) {
	repo := newMemoryRepository()
	service := newTestService(t, repo, &fakeCommander{})
	fresh, err := service.CreateAdministratorNotice(
		context.Background(),
		"",
		domain.AdministratorInput{
			Category:               domain.CategoryFamilyMessage,
			Title:                  "有效留言",
			Body:                   "还在有效期内",
			Audience:               domain.AudienceFamilyDevices,
			Channels:               []string{domain.ChannelDevice},
			ParentAccountID:        testParentID,
			DeviceID:               testDeviceID,
			DisplayDurationSeconds: 30,
		},
	)
	if err != nil {
		t.Fatalf("create fresh message: %v", err)
	}
	expiredAt := time.Date(2026, 10, 9, 11, 59, 0, 0, time.UTC)
	repo.notifications["expired-message"] = &domain.Notification{
		ID:              "expired-message",
		Category:        domain.CategoryFamilyMessage,
		Severity:        domain.SeverityInfo,
		Title:           "过期留言",
		Body:            "已经过期",
		Audience:        domain.AudienceFamilyDevices,
		ParentAccountID: testParentID,
		DeviceID:        testDeviceID,
		PublishAt:       time.Date(2026, 10, 9, 11, 0, 0, 0, time.UTC),
		ExpiresAt:       &expiredAt,
	}
	repo.deliveries = append(repo.deliveries, &domain.Delivery{
		ID:              "expired-delivery",
		NotificationID:  "expired-message",
		ParentAccountID: testParentID,
		Channel:         domain.ChannelDevice,
		Status:          domain.DeliveryStatusPending,
	})

	messages, err := service.DeviceMessages(
		context.Background(),
		testParentID,
		testDeviceID,
		20,
	)
	if err != nil {
		t.Fatalf("device messages: %v", err)
	}
	if len(messages) != 1 || messages[0].ID != fresh.ID {
		t.Fatalf("expected only the fresh message, got %#v", messages)
	}
}

func TestDeviceMessagesAppliesLimit(t *testing.T) {
	repo := newMemoryRepository()
	service := newTestService(t, repo, &fakeCommander{})
	for index := 0; index < 3; index++ {
		if _, err := service.SendFamilyMessage(
			context.Background(),
			testParentID,
			domain.GuardianMessageInput{
				DeviceID:     testDeviceID,
				Body:         "留言",
				SendToDevice: true,
			},
		); err != nil {
			t.Fatalf("send message %d: %v", index, err)
		}
	}

	messages, err := service.DeviceMessages(
		context.Background(),
		testParentID,
		testDeviceID,
		2,
	)
	if err != nil {
		t.Fatalf("device messages: %v", err)
	}
	if len(messages) != 2 {
		t.Fatalf("expected the limit to cap messages at 2, got %d", len(messages))
	}
}

func TestMarkDeliveryDeliveredByCommandOnlyUpdatesTargetDevice(t *testing.T) {
	repo := newMemoryRepository()
	now := time.Date(2026, time.October, 14, 8, 0, 0, 0, time.UTC)
	repo.notifications["notification-1"] = &domain.Notification{
		ID:              "notification-1",
		Category:        domain.CategoryFamilyMessage,
		Severity:        domain.SeverityInfo,
		Title:           "留言",
		Body:            "测试",
		Audience:        domain.AudienceFamilyDevices,
		ParentAccountID: testParentID,
		DeviceID:        testDeviceID,
		PublishAt:       now,
		CreatedAt:       now,
		UpdatedAt:       now,
	}
	repo.deliveries = append(repo.deliveries, &domain.Delivery{
		ID:              "delivery-1",
		NotificationID:  "notification-1",
		ParentAccountID: testParentID,
		Channel:         domain.ChannelDevice,
		Status:          domain.DeliveryStatusPending,
		CommandID:       "command-1",
	})
	repo.deliveries = append(repo.deliveries, &domain.Delivery{
		ID:              "delivery-2",
		NotificationID:  "notification-1",
		ParentAccountID: testParentID,
		Channel:         domain.ChannelInApp,
		Status:          domain.DeliveryStatusDelivered,
	})
	service := newTestService(t, repo, &fakeCommander{})

	if err := service.MarkDeliveryDeliveredByCommand(
		context.Background(),
		"command-1",
		testDeviceID,
	); err != nil {
		t.Fatalf("mark delivery delivered: %v", err)
	}
	if repo.deliveries[0].Status != domain.DeliveryStatusDelivered {
		t.Fatalf("expected device delivery delivered, got %q", repo.deliveries[0].Status)
	}
	if repo.deliveries[0].DeliveredAt == nil {
		t.Fatal("expected a delivered timestamp")
	}
	if repo.deliveries[0].ReadAt != nil {
		t.Fatal("delivery must not be marked read")
	}
	if repo.deliveries[1].Status != domain.DeliveryStatusDelivered {
		t.Fatal("in-app delivery must remain unchanged")
	}
}

func TestMarkDeliveryDeliveredByCommandIgnoresUnknownCommand(t *testing.T) {
	repo := newMemoryRepository()
	service := newTestService(t, repo, &fakeCommander{})
	if err := service.MarkDeliveryDeliveredByCommand(
		context.Background(),
		"command-unknown",
		testDeviceID,
	); err != nil {
		t.Fatalf("unknown command should be ignored, got %v", err)
	}
}

func TestAdminStatsCountsBroadcastAndFailures(t *testing.T) {
	repo := newMemoryRepository()
	service := newTestService(t, repo, &fakeCommander{})
	if _, err := service.CreateAdministratorNotice(context.Background(), "", domain.AdministratorInput{
		Category: domain.CategorySystemAnnouncement,
		Title:    "广播",
		Body:     "内容",
		Audience: domain.AudienceAllParents,
		Channels: []string{domain.ChannelInApp, domain.ChannelPush},
	}); err != nil {
		t.Fatalf("seed broadcast: %v", err)
	}
	stats, err := service.AdminStats(context.Background())
	if err != nil {
		t.Fatalf("admin stats: %v", err)
	}
	if stats.Total != 1 || stats.BroadcastCount != 1 {
		t.Fatalf("unexpected stats %#v", stats)
	}
	if stats.PendingDeliveryCount != 1 {
		t.Fatalf("expected 1 pending push delivery, got %d", stats.PendingDeliveryCount)
	}
}

func TestDeliveryHealthIgnoresUnconfiguredExternalChannels(t *testing.T) {
	repo := newMemoryRepository()
	service := newTestService(t, repo, &fakeCommander{})
	if _, err := service.CreateAdministratorNotice(context.Background(), "", domain.AdministratorInput{
		Category:        domain.CategorySystemAnnouncement,
		Title:           "站内通知",
		Body:            "内容",
		Audience:        domain.AudienceParent,
		Channels:        []string{domain.ChannelInApp},
		ParentAccountID: testParentID,
	}); err != nil {
		t.Fatalf("seed notice: %v", err)
	}

	health, err := service.DeliveryHealth(context.Background())
	if err != nil {
		t.Fatalf("delivery health: %v", err)
	}
	if health.PendingCount != 0 || health.FailedCount != 0 {
		t.Fatalf("unexpected delivery health %#v", health)
	}
	if len(service.DeliveryChannels()) != 0 {
		t.Fatalf("expected no optional external senders")
	}
}

func TestDeliveryHealthReportsFailedDeliveries(t *testing.T) {
	repo := newMemoryRepository()
	service := newTestService(t, repo, &fakeCommander{})
	repo.deliveries = append(repo.deliveries, &domain.Delivery{
		ID:              "delivery-1",
		NotificationID:  "notification-1",
		ParentAccountID: testParentID,
		Channel:         domain.ChannelPush,
		Status:          domain.DeliveryStatusFailed,
		FailureCode:     domain.DeliveryFailureProviderError,
	})

	health, err := service.DeliveryHealth(context.Background())
	if err != nil {
		t.Fatalf("delivery health: %v", err)
	}
	if health.FailedCount != 1 {
		t.Fatalf("failed count = %d, want 1", health.FailedCount)
	}
}

func TestDeliveryHealthMapsRepositoryFailure(t *testing.T) {
	repo := newMemoryRepository()
	repo.healthErr = errors.New("database unavailable")
	service := newTestService(t, repo, &fakeCommander{})

	if _, err := service.DeliveryHealth(context.Background()); err == nil {
		t.Fatalf("expected repository failure")
	}
}

func TestProcessPendingDeliveriesRetiresUnconfiguredChannel(t *testing.T) {
	repo := newMemoryRepository()
	service := newTestService(t, repo, &fakeCommander{})
	if _, err := service.CreateAdministratorNotice(context.Background(), "", domain.AdministratorInput{
		Category:        domain.CategorySystemAnnouncement,
		Title:           "未配置通道",
		Body:            "内容",
		Audience:        domain.AudienceParent,
		Channels:        []string{domain.ChannelPush},
		ParentAccountID: testParentID,
	}); err != nil {
		t.Fatalf("seed notice: %v", err)
	}

	batch, err := service.ProcessPendingDeliveries(
		context.Background(),
		domain.ChannelPush,
		10,
	)
	if err != nil {
		t.Fatalf("process pending deliveries: %v", err)
	}
	if batch.Retired != 1 || batch.Delivered != 0 || batch.Retried != 0 {
		t.Fatalf("unexpected batch %#v", batch)
	}
	for _, delivery := range repo.deliveries {
		if delivery.Channel != domain.ChannelPush {
			continue
		}
		if delivery.Status != domain.DeliveryStatusExpired ||
			delivery.FailureCode != domain.DeliveryFailureChannelNotConfigured {
			t.Fatalf("unexpected retired delivery %#v", delivery)
		}
	}
}

func TestProcessPendingDeliveriesMarksProviderSuccess(t *testing.T) {
	repo := newMemoryRepository()
	service := newTestService(t, repo, &fakeCommander{})
	sender := &fakeDeliverySender{}
	service.SetDeliverySender(domain.ChannelEmail, sender)
	if _, err := service.CreateAdministratorNotice(context.Background(), "", domain.AdministratorInput{
		Category:        domain.CategorySystemAnnouncement,
		Title:           "邮件",
		Body:            "内容",
		Audience:        domain.AudienceParent,
		Channels:        []string{domain.ChannelEmail},
		ParentAccountID: testParentID,
	}); err != nil {
		t.Fatalf("seed notice: %v", err)
	}

	batch, err := service.ProcessPendingDeliveries(
		context.Background(),
		domain.ChannelEmail,
		10,
	)
	if err != nil {
		t.Fatalf("process pending deliveries: %v", err)
	}
	if sender.sent != 1 || batch.Delivered != 1 {
		t.Fatalf("expected one delivery, sender=%d batch=%#v", sender.sent, batch)
	}
	if !service.DeliverySenderConfigured(domain.ChannelEmail) {
		t.Fatalf("expected email sender to be reported as configured")
	}
}

func TestProcessPendingDeliveriesRetriesProviderFailure(t *testing.T) {
	repo := newMemoryRepository()
	service := newTestService(t, repo, &fakeCommander{})
	service.SetDeliverySender(domain.ChannelSMS, &fakeDeliverySender{err: errors.New("provider down")})
	if _, err := service.CreateAdministratorNotice(context.Background(), "", domain.AdministratorInput{
		Category:        domain.CategorySystemAnnouncement,
		Title:           "短信",
		Body:            "内容",
		Audience:        domain.AudienceParent,
		Channels:        []string{domain.ChannelSMS},
		ParentAccountID: testParentID,
	}); err != nil {
		t.Fatalf("seed notice: %v", err)
	}

	batch, err := service.ProcessPendingDeliveries(
		context.Background(),
		domain.ChannelSMS,
		10,
	)
	if err != nil {
		t.Fatalf("process pending deliveries: %v", err)
	}
	if batch.Retried != 1 {
		t.Fatalf("expected one retry, got %#v", batch)
	}
	for _, delivery := range repo.deliveries {
		if delivery.Channel != domain.ChannelSMS {
			continue
		}
		if delivery.Status != domain.DeliveryStatusFailed ||
			delivery.FailureCode != domain.DeliveryFailureProviderError ||
			delivery.RetryCount != 1 {
			t.Fatalf("unexpected failed delivery %#v", delivery)
		}
	}
}

func TestProcessPendingDeliveriesRejectsWorkerOwnedChannel(t *testing.T) {
	service := newTestService(t, newMemoryRepository(), &fakeCommander{})
	if _, err := service.ProcessPendingDeliveries(
		context.Background(),
		domain.ChannelDevice,
		10,
	); !errors.Is(err, domain.ErrInvalidNotification) {
		t.Fatalf("expected ErrInvalidNotification, got %v", err)
	}
}
