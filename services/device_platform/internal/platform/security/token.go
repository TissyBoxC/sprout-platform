// Package security owns authentication and authorization primitives.
package security

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

var (
	ErrInvalidToken = errors.New("invalid access token")
	ErrExpiredToken = errors.New("access token expired")
)

// TokenClaims contains the signed identity and lifetime of an access token.
type TokenClaims struct {
	Subject   string
	SessionID string
	TokenID   string
	IssuedAt  time.Time
	ExpiresAt time.Time
}

// TokenIssuer issues and verifies short-lived bearer tokens.
type TokenIssuer interface {
	Issue(claims TokenClaims) (string, error)
	Verify(token string) (TokenClaims, error)
}

// HMACTokenIssuer implements compact, dependency-free HS256 JWTs.
//
// Access tokens bind to a server-side session through sid. The session row is
// the revocation authority, so logout and password reset invalidate access
// tokens immediately instead of waiting for their short expiry.
type HMACTokenIssuer struct {
	secret []byte
}

// NewHMACTokenIssuer creates an issuer backed by a secret of at least 32 bytes.
func NewHMACTokenIssuer(secret string) (*HMACTokenIssuer, error) {
	if len([]byte(strings.TrimSpace(secret))) < 32 {
		return nil, fmt.Errorf("token secret must contain at least 32 bytes")
	}
	return &HMACTokenIssuer{secret: []byte(secret)}, nil
}

// Issue signs one access token.
func (i *HMACTokenIssuer) Issue(claims TokenClaims) (string, error) {
	if i == nil || len(i.secret) == 0 {
		return "", errors.New("token issuer is not configured")
	}
	if strings.TrimSpace(claims.Subject) == "" || strings.TrimSpace(claims.TokenID) == "" {
		return "", errors.New("token subject and id are required")
	}
	if strings.TrimSpace(claims.SessionID) == "" {
		return "", errors.New("token session id is required")
	}
	if claims.IssuedAt.IsZero() {
		claims.IssuedAt = time.Now().UTC()
	}
	if claims.ExpiresAt.IsZero() || !claims.ExpiresAt.After(claims.IssuedAt) {
		return "", errors.New("token expiry must be after issue time")
	}

	header, err := json.Marshal(map[string]string{
		"alg": "HS256",
		"typ": "JWT",
	})
	if err != nil {
		return "", fmt.Errorf("encode token header: %w", err)
	}
	payload, err := json.Marshal(map[string]any{
		"sub": claims.Subject,
		"sid": claims.SessionID,
		"jti": claims.TokenID,
		"iat": claims.IssuedAt.Unix(),
		"exp": claims.ExpiresAt.Unix(),
	})
	if err != nil {
		return "", fmt.Errorf("encode token payload: %w", err)
	}

	unsigned := encodeSegment(header) + "." + encodeSegment(payload)
	signature := hmac.New(sha256.New, i.secret)
	_, _ = signature.Write([]byte(unsigned))
	return unsigned + "." + encodeSegment(signature.Sum(nil)), nil
}

// Verify validates the signature and expiry, then returns the claims.
func (i *HMACTokenIssuer) Verify(token string) (TokenClaims, error) {
	if i == nil || len(i.secret) == 0 {
		return TokenClaims{}, errors.New("token issuer is not configured")
	}
	parts := strings.Split(strings.TrimSpace(token), ".")
	if len(parts) != 3 {
		return TokenClaims{}, ErrInvalidToken
	}
	unsigned := parts[0] + "." + parts[1]
	expected := hmac.New(sha256.New, i.secret)
	_, _ = expected.Write([]byte(unsigned))
	provided, err := decodeSegment(parts[2])
	if err != nil || !hmac.Equal(provided, expected.Sum(nil)) {
		return TokenClaims{}, ErrInvalidToken
	}

	payload, err := decodeSegment(parts[1])
	if err != nil {
		return TokenClaims{}, ErrInvalidToken
	}
	var rawClaims struct {
		Subject   string `json:"sub"`
		SessionID string `json:"sid"`
		TokenID   string `json:"jti"`
		IssuedAt  int64  `json:"iat"`
		ExpiresAt int64  `json:"exp"`
	}
	if err := json.Unmarshal(payload, &rawClaims); err != nil {
		return TokenClaims{}, ErrInvalidToken
	}
	if rawClaims.Subject == "" || rawClaims.SessionID == "" ||
		rawClaims.TokenID == "" || rawClaims.IssuedAt <= 0 ||
		rawClaims.ExpiresAt <= 0 {
		return TokenClaims{}, ErrInvalidToken
	}
	issuedAt := time.Unix(rawClaims.IssuedAt, 0).UTC()
	expiresAt := time.Unix(rawClaims.ExpiresAt, 0).UTC()
	if !expiresAt.After(time.Now().UTC()) {
		return TokenClaims{}, ErrExpiredToken
	}
	return TokenClaims{
		Subject:   rawClaims.Subject,
		SessionID: rawClaims.SessionID,
		TokenID:   rawClaims.TokenID,
		IssuedAt:  issuedAt,
		ExpiresAt: expiresAt,
	}, nil
}

func encodeSegment(value []byte) string {
	return base64.RawURLEncoding.EncodeToString(value)
}

func decodeSegment(value string) ([]byte, error) {
	return base64.RawURLEncoding.DecodeString(value)
}
