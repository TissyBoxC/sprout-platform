package websocket

import (
	"net/http"
	"strings"
)

const (
	bearerPrefix            = "Bearer "
	webSocketProtocolPrefix = "sprout.device."
)

// DeviceIdentity is the authenticated device extracted from a short-lived
// token before the WebSocket upgrade is accepted.
type DeviceIdentity struct {
	DeviceID string
	TokenID  string
}

// TokenVerifier validates one short-lived device token.
//
// Implementations must cryptographically verify the token, reject expired or
// revoked credentials, and never return an empty device identifier. The
// verifier receives the raw token only at the transport boundary; callers must
// not log it.
type TokenVerifier interface {
	VerifyDeviceToken(token string) (DeviceIdentity, error)
}

// AuthError is a stable authentication failure that never contains the token.
type AuthError struct {
	Reason string
}

func (authError *AuthError) Error() string {
	if authError == nil || authError.Reason == "" {
		return "device authentication failed"
	}
	return authError.Reason
}

// authenticateDevice extracts a token from the Authorization header or the
// WebSocket subprotocol, verifies it, and returns the authenticated identity.
//
// The subprotocol fallback exists for embedded clients that cannot set custom
// headers. A token is never accepted from a query string because query strings
// leak into proxy logs.
func authenticateDevice(request *http.Request, verifier TokenVerifier) (DeviceIdentity, error) {
	if verifier == nil {
		return DeviceIdentity{}, &AuthError{Reason: "token verifier is unavailable"}
	}
	token, _ := deviceToken(request)
	if token == "" {
		return DeviceIdentity{}, &AuthError{Reason: "device token is missing"}
	}
	identity, err := verifier.VerifyDeviceToken(token)
	if err != nil {
		return DeviceIdentity{}, &AuthError{Reason: "device token is invalid"}
	}
	if !validIdentifier(identity.DeviceID) {
		return DeviceIdentity{}, &AuthError{Reason: "device token identity is invalid"}
	}
	return identity, nil
}

func deviceToken(request *http.Request) (string, string) {
	if request == nil {
		return "", ""
	}
	authorization := strings.TrimSpace(request.Header.Get("Authorization"))
	if strings.HasPrefix(authorization, bearerPrefix) {
		token := strings.TrimSpace(strings.TrimPrefix(authorization, bearerPrefix))
		if token != "" {
			return token, ""
		}
	}
	for _, protocol := range request.Header.Values("Sec-WebSocket-Protocol") {
		for _, candidate := range strings.Split(protocol, ",") {
			candidate = strings.TrimSpace(candidate)
			if strings.HasPrefix(candidate, webSocketProtocolPrefix) {
				token := strings.TrimPrefix(candidate, webSocketProtocolPrefix)
				if token != "" {
					return token, candidate
				}
			}
		}
	}
	return "", ""
}
