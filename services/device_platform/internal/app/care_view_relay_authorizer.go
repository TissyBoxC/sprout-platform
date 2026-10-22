package app

import (
	"context"

	"github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/care_view/domain"
	careviewservice "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/care_view/service"
)

// careViewRelayAuthorizer adapts the relay client to the service contract and
// keeps a missing client safe to call.
//
// A deployment without a relay endpoint still serves the whole view lifecycle;
// only the frames are unavailable, which the service reports instead of turning
// every request into an error.
type careViewRelayAuthorizer struct {
	client *careViewStreamAuthorizer
}

func newCareViewRelayAuthorizer(client *careViewStreamAuthorizer) careViewRelayAuthorizer {
	return careViewRelayAuthorizer{client: client}
}

// AuthorizeStream opens the relay, or reports that it is not available.
func (authorizer careViewRelayAuthorizer) AuthorizeStream(
	ctx context.Context,
	session domain.Session,
	deviceToken string,
) (string, error) {
	if authorizer.client == nil {
		return "", careviewservice.ErrRelayUnavailable
	}
	path, err := authorizer.client.AuthorizeStream(ctx, session, deviceToken)
	if err != nil {
		return "", careviewservice.ErrRelayUnavailable
	}
	return path, nil
}

var _ careviewservice.StreamAuthorizer = careViewRelayAuthorizer{}
