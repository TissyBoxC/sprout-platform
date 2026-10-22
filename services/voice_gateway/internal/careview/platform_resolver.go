package careview

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// PlatformSessionResolver reads the authoritative view state from
// device_platform before a relay may carry frames.
//
// The gateway deliberately does not trust a state carried in the authorize
// request body, and it does not trust the earlier authorize response either.
// A caller that could declare "streaming" would make the device acknowledgement
// gate optional, so the state is re-read from the service that owns the record.
//
// This is the parsing layer of the two-layer gate: every value it returns has
// already been reduced to a streaming session with a future deadline.
type PlatformSessionResolver struct {
	client       *http.Client
	baseURL      *url.URL
	serviceToken string
	now          func() time.Time
}

// PlatformResolverConfig configures the device_platform client.
type PlatformResolverConfig struct {
	BaseURL      string
	ServiceToken string
	// Timeout bounds one state read. Zero uses five seconds.
	Timeout time.Duration
	// HTTPClient lets tests inject a client. TLS verification must stay
	// enabled; this struct never accepts a skip-verify switch.
	HTTPClient *http.Client
}

// NewPlatformSessionResolver validates the platform connection.
//
// A missing base URL or service token is a configuration error: the relay must
// fail closed instead of authorizing views it cannot verify.
func NewPlatformSessionResolver(config PlatformResolverConfig) (*PlatformSessionResolver, error) {
	baseURL := strings.TrimSpace(config.BaseURL)
	if baseURL == "" {
		return nil, fmt.Errorf("care view session resolver requires a device platform base URL")
	}
	parsedURL, err := url.Parse(baseURL)
	if err != nil ||
		(parsedURL.Scheme != "http" && parsedURL.Scheme != "https") ||
		parsedURL.Host == "" ||
		parsedURL.User != nil ||
		parsedURL.RawQuery != "" ||
		parsedURL.Fragment != "" {
		return nil, fmt.Errorf("care view session resolver base URL must be an absolute HTTP(S) URL without credentials, query, or fragment")
	}
	if len(strings.TrimSpace(config.ServiceToken)) < 32 {
		return nil, fmt.Errorf("care view session resolver requires a service token of at least 32 characters")
	}
	client := config.HTTPClient
	if client == nil {
		timeout := config.Timeout
		if timeout <= 0 {
			timeout = 5 * time.Second
		}
		client = &http.Client{Timeout: timeout}
	}
	return &PlatformSessionResolver{
		client:       client,
		baseURL:      parsedURL,
		serviceToken: strings.TrimSpace(config.ServiceToken),
		now:          time.Now,
	}, nil
}

// platformSession is the subset of the platform record the relay enforces.
type platformSession struct {
	SessionID string `json:"session_id"`
	DeviceID  string `json:"device_id"`
	State     string `json:"state"`
	// ExpiresAt is the platform deadline in RFC 3339 form.
	ExpiresAt string `json:"expires_at"`
}

type platformEnvelope struct {
	Data  *platformSession `json:"data"`
	Error *struct {
		Code string `json:"code"`
	} `json:"error"`
}

// ResolveSession reads one view and verifies it may carry frames.
//
// The returned session is already reduced to the allowed state: a requested,
// declined, ended, failed, empty, or unknown state returns ErrNotStreaming, and
// a view past its deadline returns ErrSessionExpired even if the platform has
// not yet closed it. Any network or response failure returns
// ErrPlatformUnavailable, which the handler maps to a retryable refusal rather
// than opening an unverified channel.
func (resolver *PlatformSessionResolver) ResolveSession(
	ctx context.Context,
	sessionID string,
	deviceID string,
) (Session, error) {
	if resolver == nil || resolver.client == nil || resolver.baseURL == nil {
		return Session{}, ErrPlatformUnavailable
	}
	sessionID = strings.TrimSpace(sessionID)
	deviceID = strings.TrimSpace(deviceID)
	if sessionID == "" || deviceID == "" {
		return Session{}, ErrInvalidRequest
	}

	endpoint := resolver.baseURL.JoinPath(
		"internal",
		"v1",
		"devices",
		url.PathEscape(deviceID),
		"care-view-sessions",
		url.PathEscape(sessionID),
	)
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint.String(), nil)
	if err != nil {
		return Session{}, fmt.Errorf("%w: build status request", ErrPlatformUnavailable)
	}
	request.Header.Set("Authorization", "Bearer "+resolver.serviceToken)
	request.Header.Set("Accept", "application/json")

	response, err := resolver.client.Do(request)
	if err != nil {
		// The wrapped error may contain the endpoint but never the token,
		// because the token is only in a request header.
		return Session{}, fmt.Errorf("%w: %s", ErrPlatformUnavailable, redactTransportError(err))
	}
	defer func() {
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 4096))
		_ = response.Body.Close()
	}()

	switch response.StatusCode {
	case http.StatusOK:
	case http.StatusNotFound, http.StatusForbidden, http.StatusGone:
		// An unknown view and another family's view are indistinguishable, so
		// a caller cannot probe which identifiers exist.
		return Session{}, ErrSessionNotFound
	default:
		return Session{}, fmt.Errorf("%w: status %d", ErrPlatformUnavailable, response.StatusCode)
	}

	var envelope platformEnvelope
	decoder := json.NewDecoder(io.LimitReader(response.Body, 64*1024))
	if err := decoder.Decode(&envelope); err != nil {
		return Session{}, fmt.Errorf("%w: decode status response", ErrPlatformUnavailable)
	}
	if envelope.Data == nil || strings.TrimSpace(envelope.Data.SessionID) == "" {
		return Session{}, ErrSessionNotFound
	}
	if envelope.Data.SessionID != sessionID {
		// A mismatched identifier means the platform answered about a
		// different view; refusing is safer than relaying an unchecked one.
		return Session{}, ErrSessionNotFound
	}
	if envelope.Data.DeviceID != "" && envelope.Data.DeviceID != deviceID {
		return Session{}, ErrSessionNotFound
	}

	state := State(strings.TrimSpace(envelope.Data.State))
	if !state.AllowsFrames() {
		return Session{}, ErrNotStreaming
	}
	expiresAt, err := time.Parse(time.RFC3339Nano, strings.TrimSpace(envelope.Data.ExpiresAt))
	if err != nil {
		return Session{}, fmt.Errorf("%w: invalid session deadline", ErrPlatformUnavailable)
	}
	if !resolver.now().Before(expiresAt) {
		return Session{}, ErrSessionExpired
	}
	return Session{
		ID:               sessionID,
		DeviceID:         deviceID,
		State:            state,
		PlatformDeadline: expiresAt,
		ExpiresAt:        expiresAt,
	}, nil
}

// redactTransportError keeps a bounded, non-sensitive description of a failed
// platform read without echoing response bodies.
func redactTransportError(err error) string {
	if err == nil {
		return ""
	}
	var urlError *url.Error
	if errors.As(err, &urlError) {
		return urlError.Op + " " + urlError.URL
	}
	return "transport error"
}

// SessionResolver is the resolver surface the HTTP transport depends on.
//
// It is an interface so tests can inject fixed platform states without a
// network, and so the transport never depends on the concrete client.
type SessionResolver interface {
	ResolveSession(ctx context.Context, sessionID string, deviceID string) (Session, error)
}
