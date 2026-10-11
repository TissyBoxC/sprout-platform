package service

import (
	"crypto/ed25519"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"

	"github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/ui_text/domain"
)

// Ed25519KeyringVerifier verifies detached UI text signatures against a
// configured set of trusted public keys. The signed payload is the lowercase
// SHA-256 hex digest produced by PackageDigest, matching the publishing
// tooling contract.
type Ed25519KeyringVerifier struct {
	keys map[string]ed25519.PublicKey
}

// NewEd25519KeyringVerifier parses a JSON object of key id to base64 public
// key. An empty keyring is allowed so a deployment can start without UI text
// signing, but every publish attempt then fails with an unknown-key error.
func NewEd25519KeyringVerifier(encoded string) (*Ed25519KeyringVerifier, error) {
	verifier := &Ed25519KeyringVerifier{keys: map[string]ed25519.PublicKey{}}
	encoded = strings.TrimSpace(encoded)
	if encoded == "" {
		return verifier, nil
	}
	decoded := map[string]string{}
	if err := json.Unmarshal([]byte(encoded), &decoded); err != nil {
		return nil, errors.New("ui text signature public keys must be a JSON object")
	}
	for keyID, value := range decoded {
		keyID = strings.TrimSpace(keyID)
		if !domain.ValidKeyID(keyID) {
			return nil, errors.New("ui text signature key id is invalid")
		}
		raw, err := base64.StdEncoding.DecodeString(strings.TrimSpace(value))
		if err != nil {
			return nil, errors.New("ui text signature public key must be base64")
		}
		if len(raw) != ed25519.PublicKeySize {
			return nil, errors.New("ui text signature public key must be 32 bytes")
		}
		verifier.keys[keyID] = ed25519.PublicKey(raw)
	}
	return verifier, nil
}

// Verify checks an Ed25519 detached signature.
func (verifier *Ed25519KeyringVerifier) Verify(
	algorithm string,
	keyID string,
	digest string,
	signature string,
) error {
	if verifier == nil {
		return domain.ErrSignatureUnknownKey
	}
	if !domain.ValidSignatureAlgorithm(algorithm) {
		return domain.ErrSignatureInvalid
	}
	publicKey, ok := verifier.keys[strings.TrimSpace(keyID)]
	if !ok || len(publicKey) == 0 {
		return domain.ErrSignatureUnknownKey
	}
	digest = strings.ToLower(strings.TrimSpace(digest))
	if _, err := hex.DecodeString(digest); err != nil {
		return domain.ErrSignatureInvalid
	}
	rawSignature, err := base64.StdEncoding.DecodeString(strings.TrimSpace(signature))
	if err != nil {
		return domain.ErrSignatureInvalid
	}
	if !ed25519.Verify(publicKey, []byte(digest), rawSignature) {
		return domain.ErrSignatureInvalid
	}
	return nil
}

var _ SignatureVerifier = (*Ed25519KeyringVerifier)(nil)
