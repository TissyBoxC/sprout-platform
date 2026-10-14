package app

import (
	"context"
	"testing"
	"time"

	"github.com/TissyBoxC/sprout-platform/services/device_platform/internal/config"
	notificationDomain "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/notification/domain"
	notificationRepository "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/notification/repository"
	notificationService "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/notification/service"
)

// emptyNotificationRepository satisfies the persistence port with no rows. The
// registration path never touches the repository, so the stubs only need to
// exist for construction.
type emptyNotificationRepository struct{}

func (emptyNotificationRepository) Create(
	context.Context,
	*notificationDomain.Notification,
	[]*notificationDomain.Delivery,
) error {
	return nil
}

func (emptyNotificationRepository) Get(
	context.Context,
	string,
) (*notificationDomain.Notification, error) {
	return nil, notificationRepository.ErrNotFound
}

func (emptyNotificationRepository) ListInbox(
	context.Context,
	string,
	time.Time,
	int,
	string,
	string,
) ([]notificationDomain.InboxItem, error) {
	return nil, nil
}

func (emptyNotificationRepository) CountUnread(
	context.Context,
	string,
	time.Time,
) (int, error) {
	return 0, nil
}

func (emptyNotificationRepository) MarkRead(
	context.Context,
	string,
	string,
	time.Time,
) error {
	return nil
}

func (emptyNotificationRepository) MarkAllRead(
	context.Context,
	string,
	time.Time,
) error {
	return nil
}

func (emptyNotificationRepository) Delete(context.Context, string) error {
	return nil
}

func (emptyNotificationRepository) ListAdmin(
	context.Context,
	notificationDomain.AdminFilter,
) ([]notificationDomain.AdminItem, int, error) {
	return nil, 0, nil
}

func (emptyNotificationRepository) ListDeviceMessages(
	context.Context,
	string,
	string,
	time.Time,
	int,
) ([]notificationDomain.DeviceMessage, error) {
	return nil, nil
}

func (emptyNotificationRepository) MarkDeliveryDeliveredByCommand(
	context.Context,
	string,
	string,
	time.Time,
) error {
	return nil
}

func (emptyNotificationRepository) ActiveParentIDs(context.Context) ([]string, error) {
	return nil, nil
}

func (emptyNotificationRepository) ParentOwnsDevice(
	context.Context,
	string,
	string,
) (bool, error) {
	return false, nil
}

func (emptyNotificationRepository) RecordDeliveryCommand(
	context.Context,
	string,
	string,
	string,
) error {
	return nil
}

func (emptyNotificationRepository) UpdateDeliveryStatus(
	context.Context,
	string,
	string,
	string,
	string,
	string,
	time.Time,
) error {
	return nil
}

func (emptyNotificationRepository) RetireDelivery(
	context.Context,
	string,
	string,
	string,
	string,
	string,
	time.Time,
) error {
	return nil
}

func (emptyNotificationRepository) PendingDeliveries(
	context.Context,
	string,
	int,
) ([]*notificationDomain.Delivery, error) {
	return nil, nil
}

func (emptyNotificationRepository) DeliveryHealth(
	context.Context,
) (notificationDomain.DeliveryHealth, error) {
	return notificationDomain.DeliveryHealth{}, nil
}

// Compile-time guard so repository interface drift fails this test build.
var _ notificationRepository.Repository = emptyNotificationRepository{}

func newNotificationServiceForTest(t *testing.T) *notificationService.Service {
	t.Helper()
	service, err := notificationService.New(notificationService.Options{
		Repository: emptyNotificationRepository{},
	})
	if err != nil {
		t.Fatalf("notification service: %v", err)
	}
	return service
}

func TestRegisterNotificationDeliveryProvidersSkipsUnconfiguredChannels(t *testing.T) {
	service := newNotificationServiceForTest(t)

	if err := registerNotificationDeliveryProviders(service, config.Config{}); err != nil {
		t.Fatalf("register providers: %v", err)
	}
	if channels := service.DeliveryChannels(); len(channels) != 0 {
		t.Fatalf("expected no external senders, got %v", channels)
	}
}

func TestRegisterNotificationDeliveryProvidersWiresConfiguredChannels(t *testing.T) {
	service := newNotificationServiceForTest(t)
	cfg := config.Config{
		Notification: config.NotificationConfig{
			Push: config.NotificationChannelConfig{
				Endpoint: "https://notify.example.test/push",
				Token:    "push-token",
			},
			Email: config.NotificationChannelConfig{
				Endpoint: "https://notify.example.test/email",
			},
		},
	}

	if err := registerNotificationDeliveryProviders(service, cfg); err != nil {
		t.Fatalf("register providers: %v", err)
	}
	channels := service.DeliveryChannels()
	if len(channels) != 2 {
		t.Fatalf("expected two external senders, got %v", channels)
	}
	if !service.DeliverySenderConfigured(notificationDomain.ChannelPush) {
		t.Fatal("expected push sender to be configured")
	}
	if !service.DeliverySenderConfigured(notificationDomain.ChannelEmail) {
		t.Fatal("expected email sender to be configured")
	}
	if service.DeliverySenderConfigured(notificationDomain.ChannelSMS) {
		t.Fatal("expected SMS sender to remain unconfigured")
	}
}

func TestRegisterNotificationDeliveryProvidersRejectsInvalidEndpoint(t *testing.T) {
	service := newNotificationServiceForTest(t)
	cfg := config.Config{
		Notification: config.NotificationConfig{
			SMS: config.NotificationChannelConfig{
				Endpoint: "http://notify.example.test/sms",
			},
		},
	}

	if err := registerNotificationDeliveryProviders(service, cfg); err == nil {
		t.Fatal("expected insecure SMS endpoint to be rejected")
	}
}

func TestRegisterNotificationDeliveryProvidersRequiresService(t *testing.T) {
	if err := registerNotificationDeliveryProviders(nil, config.Config{}); err == nil {
		t.Fatal("expected nil service to be rejected")
	}
}
