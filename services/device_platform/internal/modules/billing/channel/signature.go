package channel

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"strings"
)

// SignatureVerifier authenticates a provider callback. header carries the
// callback HTTP headers in canonical MIME form; body is the untouched raw body.
//
// Contract: a verifier returns nil ONLY when the request is provably authentic.
// Any doubt, missing input or internal failure must return an error, and that
// error must satisfy errors.Is(err, ErrSignatureInvalid) because the handler
// uses that sentinel to record result='signature_invalid' and refuse credit.
type SignatureVerifier interface {
	Verify(header http.Header, body []byte) error
}

// HMACHeaderName is the header HMACVerifier reads. It matches the project's
// device-request signing header so operators see one signing convention.
const HMACHeaderName = "X-Signature"

// RejectAllVerifier is the fail-closed default for channels with no secret.
// It rejects every callback, wrapping both ErrSignatureInvalid and
// ErrChannelNotConfigured: the signature is invalid because there is no secret
// to validate it against. Callers can therefore report "payment not open yet"
// to operators while still recording signature_invalid in the ledger.
// It has no fields, so it is a safe zero value.
type RejectAllVerifier struct{}

// Verify always fails; there is deliberately no way to configure it to pass.
func (RejectAllVerifier) Verify(header http.Header, body []byte) error {
	_ = header
	_ = body
	return fmt.Errorf("%w: %w", ErrSignatureInvalid, ErrChannelNotConfigured)
}

// HMACVerifier authenticates a callback with HMAC-SHA256(secret, body).
// The expected value is read from HMACHeaderName, either as lowercase hex or as
// standard base64. Comparison is constant-time.
//
// The secret is never returned, logged or embedded in error messages. Create it
// with NewHMACVerifier so an empty secret cannot silently disable verification.
type HMACVerifier struct {
	secret []byte
}

// NewHMACVerifier builds a verifier from the raw shared secret. An empty or
// whitespace-only secret is rejected: fail closed rather than accept anything.
func NewHMACVerifier(secret string) (*HMACVerifier, error) {
	if strings.TrimSpace(secret) == "" {
		return nil, fmt.Errorf("%w: %w: hmac secret must not be empty", ErrChannelNotConfigured, ErrInvalidRequest)
	}
	return &HMACVerifier{secret: []byte(secret)}, nil
}

// Verify recomputes the HMAC over body and compares it in constant time.
// Missing, malformed or mismatched signatures all return ErrSignatureInvalid
// with the same shape, so a caller cannot tell a guessed header from a wrong one.
func (verifier *HMACVerifier) Verify(header http.Header, body []byte) error {
	if len(verifier.secret) == 0 {
		// A zero-value HMACVerifier is unconfigured, not "accept everything".
		return fmt.Errorf("%w: %w: hmac secret not set", ErrSignatureInvalid, ErrChannelNotConfigured)
	}
	presented := strings.TrimSpace(readSignatureHeader(header))
	if presented == "" {
		return fmt.Errorf("%w: missing %s header", ErrSignatureInvalid, HMACHeaderName)
	}
	expectedBytes, err := verifier.Sign(body)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrSignatureInvalid, err)
	}
	presentedBytes, decodeErr := decodeSignature(presented, len(expectedBytes))
	if decodeErr != nil {
		return fmt.Errorf("%w: %v", ErrSignatureInvalid, decodeErr)
	}
	// subtle.ConstantTimeCompare returns 1 only on equal length and content;
	// it avoids leaking how many leading bytes a guess got right.
	if subtle.ConstantTimeCompare(presentedBytes, expectedBytes) != 1 {
		return fmt.Errorf("%w: signature mismatch", ErrSignatureInvalid)
	}
	return nil
}

// Sign returns the lowercase-hex HMAC-SHA256 of body. It is the exact value the
// provider side must send in HMACHeaderName; it exposes no secret material.
func (verifier *HMACVerifier) Sign(body []byte) ([]byte, error) {
	if len(verifier.secret) == 0 {
		return nil, errors.New("hmac secret not set")
	}
	mac := hmac.New(sha256.New, verifier.secret)
	if _, err := mac.Write(body); err != nil {
		return nil, fmt.Errorf("compute hmac: %w", err)
	}
	return mac.Sum(nil), nil
}

// SignHex is the hex form of Sign, for use in tests and in provider simulators.
func (verifier *HMACVerifier) SignHex(body []byte) (string, error) {
	sum, err := verifier.Sign(body)
	if err != nil {
		return "", err
	}
	return hex.EncodeToString(sum), nil
}

// readSignatureHeader returns the first X-Signature value, tolerating a
// non-canonical key. http.Header.Get misses "x-signature" when the map was built
// in code rather than by the HTTP parser, which would silently fail closed on a
// valid callback; matching case-insensitively keeps that mistake harmless.
func readSignatureHeader(header http.Header) string {
	if header == nil {
		return ""
	}
	if values := header.Values(HMACHeaderName); len(values) > 0 {
		return values[0]
	}
	for key, values := range header {
		if len(values) == 0 || !strings.EqualFold(key, HMACHeaderName) {
			continue
		}
		return values[0]
	}
	return ""
}

// decodeSignature accepts hex or base64 because providers differ; expectedLen
// makes hex-vs-hex selection deterministic instead of guessing.
func decodeSignature(presented string, expectedLen int) ([]byte, error) {
	hexAttempt, hexErr := hex.DecodeString(presented)
	if hexErr == nil && len(hexAttempt) == expectedLen {
		return hexAttempt, nil
	}
	base64Attempt, base64Err := base64.StdEncoding.DecodeString(presented)
	if base64Err == nil && len(base64Attempt) == expectedLen {
		return base64Attempt, nil
	}
	return nil, errors.New("signature is neither hex nor base64 of the expected length")
}
