package service

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"testing"
	"time"

	"github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/device_binding/domain"
	"github.com/TissyBoxC/sprout-platform/services/device_platform/internal/platform/security"
)

func TestDeviceRegistrationAndAuthenticationFlow(t *testing.T) {
	t.Parallel()
	repository := newMemoryRepository()
	service, err := New(Options{
		Repository:    repository,
		ProofVerifier: security.ECDSAProofVerifier{},
		TokenTTL:      time.Minute,
	})
	if err != nil {
		t.Fatalf("create service: %v", err)
	}

	privateKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate device key: %v", err)
	}
	publicKey := elliptic.Marshal(
		privateKey.Curve,
		privateKey.X,
		privateKey.Y,
	)
	registrationToken, _, err := service.CreateRegistrationToken(
		context.Background(),
		"device_test_001",
	)
	if err != nil {
		t.Fatalf("create registration token: %v", err)
	}
	if _, err := service.RegisterDevice(
		context.Background(),
		registrationToken,
		domain.DeviceRegistrationInput{
			DeviceID:        "device_test_001",
			HardwareModel:   "esp32-s3-n16r8",
			FirmwareVersion: "0.1.0",
			Capabilities:    []string{"wifi", "display"},
			PublicKey:       base64.StdEncoding.EncodeToString(publicKey),
		},
	); err != nil {
		t.Fatalf("register device: %v", err)
	}
	if _, err := service.RegisterDevice(
		context.Background(),
		registrationToken,
		domain.DeviceRegistrationInput{
			DeviceID:  "device_test_001",
			PublicKey: base64.StdEncoding.EncodeToString(publicKey),
		},
	); !errors.Is(err, domain.ErrRegistrationTokenConsumed) {
		t.Fatalf("expected consumed registration token, got %v", err)
	}

	nonce, _, err := service.StartDeviceAuthentication(
		context.Background(),
		"device_test_001",
	)
	if err != nil {
		t.Fatalf("start device authentication: %v", err)
	}
	message := []byte("device_test_001." + nonce)
	digest := sha256.Sum256(message)
	signatureR, signatureS, err := ecdsa.Sign(rand.Reader, privateKey, digest[:])
	if err != nil {
		t.Fatalf("sign device challenge: %v", err)
	}
	signature := make([]byte, 64)
	signatureR.FillBytes(signature[:32])
	signatureS.FillBytes(signature[32:])
	sessionToken, err := service.CompleteDeviceAuthentication(
		context.Background(),
		"device_test_001",
		nonce,
		signature,
	)
	if err != nil {
		t.Fatalf("complete device authentication: %v", err)
	}
	provisioningToken, _, err := service.CreateDeviceProvisioningToken(
		context.Background(),
		"device_test_001",
		sessionToken,
	)
	if err != nil {
		t.Fatalf("create provisioning token: %v", err)
	}
	if provisioningToken == "" {
		t.Fatal("expected a provisioning token")
	}
}

func TestDeleteRevokesDeviceSessions(t *testing.T) {
	t.Parallel()
	repository := newMemoryRepository()
	service, err := New(Options{
		Repository:    repository,
		ProofVerifier: security.ECDSAProofVerifier{},
		TokenTTL:      time.Minute,
	})
	if err != nil {
		t.Fatalf("create service: %v", err)
	}

	repository.bindings["device_test_001"] = &domain.Binding{
		ID:              "binding-001",
		ParentAccountID: "parent-001",
		DeviceID:        "device_test_001",
	}
	repository.sessions["session-hash"] = &domain.DeviceSession{
		ID:        "session-001",
		DeviceID:  "device_test_001",
		ExpiresAt: time.Now().UTC().Add(time.Hour),
	}

	if err := service.Delete(
		context.Background(),
		"parent-001",
		"device_test_001",
	); err != nil {
		t.Fatalf("delete binding: %v", err)
	}

	session := repository.sessions["session-hash"]
	if session == nil || session.RevokedAt == nil {
		t.Fatal("expected the device session to be revoked")
	}
	if _, exists := repository.bindings["device_test_001"]; exists {
		t.Fatal("expected the device binding to be removed")
	}
}

type memoryRepository struct {
	registrationTokens map[string]*domain.RegistrationToken
	credentials        map[string]*domain.DeviceCredential
	challenges         map[string]*domain.DeviceChallenge
	sessions           map[string]*domain.DeviceSession
	tokens             map[string]*domain.BindingToken
	bindings           map[string]*domain.Binding
}

func newMemoryRepository() *memoryRepository {
	return &memoryRepository{
		registrationTokens: make(map[string]*domain.RegistrationToken),
		credentials:        make(map[string]*domain.DeviceCredential),
		challenges:         make(map[string]*domain.DeviceChallenge),
		sessions:           make(map[string]*domain.DeviceSession),
		tokens:             make(map[string]*domain.BindingToken),
		bindings:           make(map[string]*domain.Binding),
	}
}

func (r *memoryRepository) CreateToken(_ context.Context, token *domain.BindingToken) error {
	r.tokens[token.TokenHash] = token
	return nil
}

func (r *memoryRepository) GetTokenByHash(
	_ context.Context,
	tokenHash string,
) (*domain.BindingToken, error) {
	token := r.tokens[tokenHash]
	if token == nil {
		return nil, domain.ErrTokenNotFound
	}
	return token, nil
}

func (r *memoryRepository) ConsumeTokenAndUpsertBinding(
	_ context.Context,
	tokenID string,
	binding *domain.Binding,
) error {
	for _, token := range r.tokens {
		if token.ID == tokenID {
			if token.ConsumedAt != nil {
				return domain.ErrTokenConsumed
			}
			now := time.Now().UTC()
			token.ConsumedAt = &now
			r.bindings[binding.DeviceID] = binding
			return nil
		}
	}
	return domain.ErrTokenNotFound
}

func (r *memoryRepository) UpsertBinding(context.Context, *domain.Binding) error {
	return nil
}

func (r *memoryRepository) ListByParentAccountID(
	context.Context,
	string,
) ([]domain.Binding, error) {
	return nil, nil
}

func (r *memoryRepository) ListAllBindings(
	context.Context,
) ([]domain.Binding, error) {
	return nil, nil
}

func (r *memoryRepository) GetByDeviceID(
	context.Context,
	string,
) (*domain.Binding, error) {
	return nil, domain.ErrDeviceNotFound
}

func (r *memoryRepository) Delete(context.Context, string, string) error {
	return nil
}

func (r *memoryRepository) RevokeDeviceSessionsAndDeleteBinding(
	_ context.Context,
	parentAccountID string,
	deviceID string,
) error {
	for tokenHash, session := range r.sessions {
		if session.DeviceID == deviceID {
			now := time.Now().UTC()
			session.RevokedAt = &now
			r.sessions[tokenHash] = session
		}
	}
	binding := r.bindings[deviceID]
	if binding == nil || binding.ParentAccountID != parentAccountID {
		return domain.ErrDeviceNotFound
	}
	delete(r.bindings, deviceID)
	return nil
}

func (r *memoryRepository) CreateRegistrationToken(
	_ context.Context,
	token *domain.RegistrationToken,
) error {
	r.registrationTokens[token.TokenHash] = token
	return nil
}

func (r *memoryRepository) GetRegistrationTokenByHash(
	_ context.Context,
	tokenHash string,
) (*domain.RegistrationToken, error) {
	token := r.registrationTokens[tokenHash]
	if token == nil {
		return nil, domain.ErrRegistrationTokenNotFound
	}
	return token, nil
}

func (r *memoryRepository) ConsumeRegistrationTokenAndUpsertCredential(
	_ context.Context,
	tokenID string,
	credential *domain.DeviceCredential,
) error {
	consumed := false
	for _, token := range r.registrationTokens {
		if token.ID == tokenID {
			if token.ConsumedAt != nil {
				return domain.ErrRegistrationTokenConsumed
			}
			now := time.Now().UTC()
			token.ConsumedAt = &now
			consumed = true
			break
		}
	}
	if !consumed {
		return domain.ErrRegistrationTokenNotFound
	}
	r.credentials[credential.DeviceID] = credential
	return nil
}

func (r *memoryRepository) GetDeviceCredential(
	_ context.Context,
	deviceID string,
) (*domain.DeviceCredential, error) {
	credential := r.credentials[deviceID]
	if credential == nil {
		return nil, domain.ErrDeviceNotFound
	}
	return credential, nil
}

func (r *memoryRepository) CreateDeviceChallenge(
	_ context.Context,
	challenge *domain.DeviceChallenge,
) error {
	r.challenges[challenge.NonceHash] = challenge
	return nil
}

func (r *memoryRepository) GetDeviceChallengeByNonceHash(
	_ context.Context,
	nonceHash string,
) (*domain.DeviceChallenge, error) {
	challenge := r.challenges[nonceHash]
	if challenge == nil {
		return nil, domain.ErrDeviceChallengeNotFound
	}
	return challenge, nil
}

func (r *memoryRepository) CompleteDeviceAuthentication(
	_ context.Context,
	challengeID string,
	deviceID string,
	session *domain.DeviceSession,
) error {
	consumed := false
	for _, challenge := range r.challenges {
		if challenge.ID == challengeID {
			if challenge.ConsumedAt != nil {
				return domain.ErrDeviceChallengeConsumed
			}
			now := time.Now().UTC()
			challenge.ConsumedAt = &now
			consumed = true
			break
		}
	}
	if !consumed {
		return domain.ErrDeviceChallengeNotFound
	}
	credential := r.credentials[deviceID]
	if credential == nil {
		return domain.ErrDeviceNotFound
	}
	now := time.Now().UTC()
	credential.LastAuthenticatedAt = &now
	r.sessions[session.TokenHash] = session
	return nil
}

func (r *memoryRepository) GetDeviceSessionByTokenHash(
	_ context.Context,
	tokenHash string,
) (*domain.DeviceSession, error) {
	session := r.sessions[tokenHash]
	if session == nil {
		return nil, domain.ErrDeviceSessionNotFound
	}
	return session, nil
}
