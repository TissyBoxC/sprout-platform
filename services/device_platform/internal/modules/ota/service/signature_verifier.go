package service

import (
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"

	"github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/ota/domain"
)

// Ed25519KeyringVerifier verifies detached firmware signatures against a
// configured set of trusted public keys. The signed payload is the complete
// canonical release manifest, so strategy fields cannot be changed after
// signing. Firmware intentionally retains no legacy digest-only verification.
type Ed25519KeyringVerifier struct {
	keys map[string]ed25519.PublicKey
}

// NewEd25519KeyringVerifier parses a JSON object of key id to base64 public
// key. An empty keyring is allowed so a deployment can start without firmware
// signing, but every publish attempt then fails with an unknown-key error.
func NewEd25519KeyringVerifier(encoded string) (*Ed25519KeyringVerifier, error) {
	verifier := &Ed25519KeyringVerifier{keys: map[string]ed25519.PublicKey{}}
	encoded = strings.TrimSpace(encoded)
	if encoded == "" {
		return verifier, nil
	}
	decoded := map[string]string{}
	if err := json.Unmarshal([]byte(encoded), &decoded); err != nil {
		return nil, errors.New("firmware signature public keys must be a JSON object")
	}
	for keyID, value := range decoded {
		if strings.TrimSpace(keyID) != keyID ||
			!domain.ValidSignatureKeyID(keyID) {
			return nil, errors.New(
				"firmware signature key id must be 1-64 characters from [A-Za-z0-9._:-]",
			)
		}
		value = strings.TrimSpace(value)
		raw, err := base64.StdEncoding.Strict().DecodeString(value)
		if err != nil {
			return nil, errors.New("firmware signature public key must be base64")
		}
		if len(raw) != ed25519.PublicKeySize ||
			base64.StdEncoding.EncodeToString(raw) != value {
			return nil, errors.New("firmware signature public key must be 32 bytes")
		}
		verifier.keys[keyID] = ed25519.PublicKey(raw)
	}
	return verifier, nil
}

// Verify checks an Ed25519 detached signature.
func (verifier *Ed25519KeyringVerifier) Verify(
	algorithm string,
	keyID string,
	manifest []byte,
	signature string,
) error {
	if verifier == nil {
		return domain.ErrSignatureKeyUnknown
	}
	if !domain.ValidSignatureAlgorithm(algorithm) {
		return domain.ErrSignatureInvalid
	}
	if strings.TrimSpace(keyID) != keyID {
		return domain.ErrSignatureKeyUnknown
	}
	publicKey, ok := verifier.keys[keyID]
	if !ok || len(publicKey) == 0 {
		return domain.ErrSignatureKeyUnknown
	}
	if len(manifest) == 0 || len(manifest) > 16384 {
		return domain.ErrSignatureInvalid
	}
	signature = strings.TrimSpace(signature)
	rawSignature, err := base64.StdEncoding.Strict().DecodeString(signature)
	if err != nil {
		return domain.ErrSignatureInvalid
	}
	if len(rawSignature) != ed25519.SignatureSize ||
		base64.StdEncoding.EncodeToString(rawSignature) != signature {
		return domain.ErrSignatureInvalid
	}
	if !ed25519.Verify(publicKey, manifest, rawSignature) {
		return domain.ErrSignatureInvalid
	}
	return nil
}

var _ SignatureVerifier = (*Ed25519KeyringVerifier)(nil)
