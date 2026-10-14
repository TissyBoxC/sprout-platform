package delivery

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	notificationdomain "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/notification/domain"
)

// newTLSServerProvider builds a provider pointed at an httptest TLS server.
//
// NewWebhookProvider requires https, so the test rewrites the httptest scheme
// and injects the server's client, which already trusts its own certificate.
func newTLSServerProvider(
	t *testing.T,
	channel string,
	server *httptest.Server,
	token string,
) *WebhookProvider {
	t.Helper()

	parsed, err := url.Parse(server.URL)
	if err != nil {
		t.Fatalf("parse server url: %v", err)
	}
	parsed.Scheme = "https"
	client := server.Client()
	client.Timeout = 5 * time.Second

	provider, err := NewWebhookProvider(channel, parsed.String(), token, client)
	if err != nil {
		t.Fatalf("NewWebhookProvider() error = %v", err)
	}
	return provider
}

func testNotification() *notificationdomain.Notification {
	return &notificationdomain.Notification{
		ID:         "notification-1",
		Category:   notificationdomain.CategorySystemAnnouncement,
		Severity:   notificationdomain.SeverityInfo,
		Title:      "课程更新",
		Body:       "新增了一组英文儿歌。",
		ActionPath: "/content/library",
	}
}

func testDelivery(channel string) *notificationdomain.Delivery {
	return &notificationdomain.Delivery{
		NotificationID:  "notification-1",
		ParentAccountID: "parent-1",
		Channel:         channel,
	}
}

func TestNewWebhookProviderRejectsUnsupportedChannel(t *testing.T) {
	for _, channel := range []string{
		notificationdomain.ChannelInApp,
		notificationdomain.ChannelDevice,
		"unknown",
	} {
		if _, err := NewWebhookProvider(
			channel,
			"https://notify.example.com/send",
			"",
			nil,
		); err == nil {
			t.Fatalf("expected channel %q to be rejected", channel)
		}
	}
}

func TestNewWebhookProviderRejectsInsecureEndpoint(t *testing.T) {
	for _, endpoint := range []string{
		"http://notify.example.com/send",
		"notify.example.com/send",
		"https://user:pass@notify.example.com/send",
		"https://notify.example.com/send#fragment",
	} {
		if _, err := NewWebhookProvider(
			notificationdomain.ChannelEmail,
			endpoint,
			"",
			nil,
		); err == nil {
			t.Fatalf("expected endpoint %q to be rejected", endpoint)
		}
	}
}

func TestNewWebhookProviderAcceptsSupportedChannel(t *testing.T) {
	for _, channel := range []string{
		notificationdomain.ChannelPush,
		notificationdomain.ChannelSMS,
		notificationdomain.ChannelEmail,
	} {
		provider, err := NewWebhookProvider(
			channel,
			"https://notify.example.com/send",
			"",
			nil,
		)
		if err != nil {
			t.Fatalf("NewWebhookProvider(%q) error = %v", channel, err)
		}
		if provider.Channel() != channel {
			t.Fatalf("unexpected channel: %q", provider.Channel())
		}
	}
}

func TestWebhookProviderSendPostsMinimalPayload(t *testing.T) {
	var (
		gotAuth        string
		gotContentType string
		gotPayload     webhookPayload
	)
	server := httptest.NewTLSServer(http.HandlerFunc(
		func(writer http.ResponseWriter, request *http.Request) {
			gotAuth = request.Header.Get("Authorization")
			gotContentType = request.Header.Get("Content-Type")
			if err := json.NewDecoder(request.Body).Decode(&gotPayload); err != nil {
				t.Errorf("decode payload: %v", err)
			}
			writer.WriteHeader(http.StatusAccepted)
		},
	))
	defer server.Close()

	provider := newTLSServerProvider(
		t,
		notificationdomain.ChannelEmail,
		server,
		"provider-token",
	)
	if err := provider.Send(
		context.Background(),
		testNotification(),
		testDelivery(notificationdomain.ChannelEmail),
	); err != nil {
		t.Fatalf("Send() error = %v", err)
	}

	if gotAuth != "Bearer provider-token" {
		t.Fatalf("unexpected Authorization header: %q", gotAuth)
	}
	if gotContentType != "application/json" {
		t.Fatalf("unexpected Content-Type: %q", gotContentType)
	}
	if gotPayload.SchemaVersion != 1 {
		t.Fatalf("unexpected schema version: %d", gotPayload.SchemaVersion)
	}
	if gotPayload.NotificationID != "notification-1" {
		t.Fatalf("unexpected notification id: %q", gotPayload.NotificationID)
	}
	if gotPayload.ParentAccountID != "parent-1" {
		t.Fatalf("unexpected parent account: %q", gotPayload.ParentAccountID)
	}
	if gotPayload.Channel != notificationdomain.ChannelEmail {
		t.Fatalf("unexpected channel: %q", gotPayload.Channel)
	}
	if gotPayload.Title != "课程更新" || gotPayload.Body == "" {
		t.Fatalf("unexpected copy: %#v", gotPayload)
	}
	if gotPayload.ActionPath != "/content/library" {
		t.Fatalf("unexpected action path: %q", gotPayload.ActionPath)
	}
}

func TestWebhookProviderSendReturnsRetryableErrorOnRejection(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(
		func(writer http.ResponseWriter, _ *http.Request) {
			writer.WriteHeader(http.StatusBadGateway)
		},
	))
	defer server.Close()

	provider := newTLSServerProvider(
		t,
		notificationdomain.ChannelPush,
		server,
		"",
	)
	err := provider.Send(
		context.Background(),
		testNotification(),
		testDelivery(notificationdomain.ChannelPush),
	)
	if !errors.Is(err, ErrProviderRejected) {
		t.Fatalf("expected ErrProviderRejected, got %v", err)
	}
}

func TestWebhookProviderSendRejectsMismatchedChannel(t *testing.T) {
	provider, err := NewWebhookProvider(
		notificationdomain.ChannelSMS,
		"https://notify.example.com/send",
		"",
		nil,
	)
	if err != nil {
		t.Fatalf("NewWebhookProvider() error = %v", err)
	}
	err = provider.Send(
		context.Background(),
		testNotification(),
		testDelivery(notificationdomain.ChannelEmail),
	)
	if err == nil {
		t.Fatal("expected channel mismatch to be rejected")
	}
}

func TestWebhookProviderSendRejectsMissingPayload(t *testing.T) {
	provider, err := NewWebhookProvider(
		notificationdomain.ChannelSMS,
		"https://notify.example.com/send",
		"",
		nil,
	)
	if err != nil {
		t.Fatalf("NewWebhookProvider() error = %v", err)
	}
	if err := provider.Send(
		context.Background(),
		nil,
		testDelivery(notificationdomain.ChannelSMS),
	); err == nil {
		t.Fatal("expected nil notification to be rejected")
	}
	if err := provider.Send(
		context.Background(),
		testNotification(),
		nil,
	); err == nil {
		t.Fatal("expected nil delivery to be rejected")
	}
}
