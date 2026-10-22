package app

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/care_view/domain"
)

// careViewStreamAuthorizer asks the voice gateway to open the frame relay for a
// confirmed session.
//
// The gateway is the only component that talks to devices for media, so the
// platform does not stream frames itself. The gateway re-reads the session state
// from this service before forwarding anything, which is why the call carries no
// authority of its own: it is a request, not a grant.
type careViewStreamAuthorizer struct {
	endpoint     string
	serviceToken string
	client       *http.Client
}

// NewCareViewStreamAuthorizer builds an authorizer against the gateway's internal
// API. An empty endpoint returns nil so a deployment without a relay simply
// reports streaming as unavailable instead of failing every start.
func NewCareViewStreamAuthorizer(endpoint, serviceToken string) *careViewStreamAuthorizer {
	trimmed := strings.TrimRight(strings.TrimSpace(endpoint), "/")
	if trimmed == "" || serviceToken == "" {
		return nil
	}
	return &careViewStreamAuthorizer{
		endpoint:     trimmed,
		serviceToken: serviceToken,
		client:       &http.Client{Timeout: 10 * time.Second},
	}
}

// AuthorizeStream opens the relay and returns the relative path the parent app
// connects to. The path carries no token: the app authenticates itself.
func (authorizer *careViewStreamAuthorizer) AuthorizeStream(
	ctx context.Context,
	session domain.Session,
	deviceToken string,
) (string, error) {
	body, err := json.Marshal(map[string]string{
		"device_id":    session.DeviceID,
		"device_token": deviceToken,
	})
	if err != nil {
		return "", err
	}
	request, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		fmt.Sprintf("%s/internal/v1/care-view/sessions/%s/stream", authorizer.endpoint, session.ID),
		bytes.NewReader(body),
	)
	if err != nil {
		return "", err
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Authorization", "Bearer "+authorizer.serviceToken)

	response, err := authorizer.client.Do(request)
	if err != nil {
		return "", err
	}
	defer response.Body.Close()

	// The body is bounded: a misconfigured upstream must not be able to exhaust
	// memory here.
	payload, err := io.ReadAll(io.LimitReader(response.Body, 1<<16))
	if err != nil {
		return "", err
	}
	if response.StatusCode != http.StatusOK {
		// The gateway's detail is deliberately not propagated: it may name internal
		// identifiers, and the guardian only needs to know the view is unavailable.
		return "", fmt.Errorf("care view relay refused the stream: %d", response.StatusCode)
	}

	var envelope struct {
		Data struct {
			StreamPath string `json:"stream_path"`
		} `json:"data"`
	}
	if err := json.Unmarshal(payload, &envelope); err != nil {
		return "", err
	}
	if envelope.Data.StreamPath == "" {
		return "", fmt.Errorf("care view relay returned no stream path")
	}
	return envelope.Data.StreamPath, nil
}
