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
		if delivery.Channel == channel && delivery.Status == domain.DeliveryStatusPending {
			result = append(result, delivery)
		}
	}
	if len(result) > limit {
		result = result[:limit]
	}
	return result, nil
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
