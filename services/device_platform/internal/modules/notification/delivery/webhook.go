// Package delivery contains outbound notification providers that implement the
// notification service's DeliverySender port.
//
// Providers only receive the authored copy plus routing identifiers. They never
// receive child conversation text, audio, images, tokens, or credentials, and
// upstream error bodies are never surfaced to callers or logs.
package delivery

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	notificationdomain "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/notification/domain"
)

const (
	// maxResponseBytes bounds how much of a provider response is read so a
	// misbehaving provider cannot exhaust memory.
	maxResponseBytes = 8 << 10
	// requestTimeout bounds one delivery attempt.
	requestTimeout = 10 * time.Second
)

// ErrProviderRejected reports a non-2xx provider response. The caller keeps
// the delivery retryable and never receives the provider body.
var ErrProviderRejected = errors.New("notification provider rejected delivery")

// WebhookProvider posts one JSON delivery attempt to a channel-specific
// endpoint.
//
// The payload is intentionally minimal and stable: routing identifiers plus
// the authored title/body. Action paths are sent as a deep link hint so a push
// provider can open the in-app destination, but they carry no credentials.
type WebhookProvider struct {
	channel    string
	endpoint   *url.URL
	authToken  string
	httpClient *http.Client
}

// NewWebhookProvider validates configuration and builds one channel sender.
//
// HTTPS is required. A plain HTTP endpoint is rejected so delivery cannot leak
// guardian contact details or message copy over cleartext transport.
func NewWebhookProvider(
	channel string,
	endpoint string,
	authToken string,
	httpClient *http.Client,
) (*WebhookProvider, error) {
	channel = strings.ToLower(strings.TrimSpace(channel))
	switch channel {
	case notificationdomain.ChannelPush,
		notificationdomain.ChannelSMS,
		notificationdomain.ChannelEmail:
	default:
		return nil, fmt.Errorf("unsupported delivery channel %q", channel)
	}

	parsed, err := url.Parse(strings.TrimSpace(endpoint))
	if err != nil {
		return nil, fmt.Errorf("parse %s delivery endpoint: %w", channel, err)
	}
	if parsed.Scheme != "https" || parsed.Host == "" {
		return nil, fmt.Errorf(
			"%s delivery endpoint must be an absolute https URL",
			channel,
		)
	}
	if parsed.User != nil || parsed.Fragment != "" {
		return nil, fmt.Errorf(
			"%s delivery endpoint must not contain credentials or fragments",
			channel,
		)
	}

	if httpClient == nil {
		httpClient = &http.Client{Timeout: requestTimeout}
	}

	return &WebhookProvider{
		channel:    channel,
		endpoint:   parsed,
		authToken:  strings.TrimSpace(authToken),
		httpClient: httpClient,
	}, nil
}

// Channel returns the notification channel this provider serves.
func (p *WebhookProvider) Channel() string {
	return p.channel
}

type webhookPayload struct {
	SchemaVersion   int    `json:"schema_version"`
	Channel         string `json:"channel"`
	NotificationID  string `json:"notification_id"`
	ParentAccountID string `json:"parent_account_id"`
	Category        string `json:"category"`
	Severity        string `json:"severity"`
	Title           string `json:"title"`
	Body            string `json:"body"`
	ActionPath      string `json:"action_path,omitempty"`
}

// Send posts one delivery attempt. It returns ErrProviderRejected for a
// non-2xx response so the service keeps the row retryable.
func (p *WebhookProvider) Send(
	ctx context.Context,
	notification *notificationdomain.Notification,
	delivery *notificationdomain.Delivery,
) error {
	if p == nil || p.endpoint == nil {
		return errors.New("notification provider is not configured")
	}
	if notification == nil || delivery == nil {
		return errors.New("notification delivery payload is required")
	}
	if delivery.Channel != p.channel {
		return fmt.Errorf(
			"notification provider %q cannot send channel %q",
			p.channel,
			delivery.Channel,
		)
	}

	payload := webhookPayload{
		SchemaVersion:   1,
		Channel:         p.channel,
		NotificationID:  notification.ID,
		ParentAccountID: delivery.ParentAccountID,
		Category:        notification.Category,
		Severity:        notification.Severity,
		Title:           notification.Title,
		Body:            notification.Body,
		ActionPath:      notification.ActionPath,
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("encode %s delivery payload: %w", p.channel, err)
	}

	request, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		p.endpoint.String(),
		bytes.NewReader(encoded),
	)
	if err != nil {
		return fmt.Errorf("create %s delivery request: %w", p.channel, err)
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/json")
	if p.authToken != "" {
		request.Header.Set("Authorization", "Bearer "+p.authToken)
	}

	response, err := p.httpClient.Do(request)
	if err != nil {
		return fmt.Errorf("%s delivery request failed: %w", p.channel, err)
	}
	defer response.Body.Close()
	// Drain a bounded amount so the connection can be reused without trusting
	// or logging provider-supplied content.
	_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, maxResponseBytes))

	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return fmt.Errorf("%w: status %d", ErrProviderRejected, response.StatusCode)
	}
	return nil
}
