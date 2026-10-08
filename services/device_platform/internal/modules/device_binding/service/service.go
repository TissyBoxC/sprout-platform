// Package service owns device provisioning token and binding workflows.
package service

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"regexp"
	"strings"
	"time"

	"github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/device_binding/domain"
	"github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/device_binding/repository"
	"github.com/TissyBoxC/sprout-platform/services/device_platform/internal/platform/clock"
	"github.com/google/uuid"
)

var deviceIDPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]{7,127}$`)

// Keep this set in lockstep with common.schema.json::device_capability and the
// firmware device_capabilities component. Unknown capabilities are rejected
// at registration so clients never render an unreviewed feature or hardware
// claim.
var deviceCapabilities = map[string]struct{}{
	"audio_input":     {},
	"audio_output":    {},
	"wifi":            {},
	"camera":          {},
	"display":         {},
	"touch":           {},
	"led":             {},
	"battery":         {},
	"cellular_4g":     {},
	"motion":          {},
	"bluetooth_audio": {},
	"video_call":      {},
	"location":        {},
	"geofence":        {},
	"sos":             {},
	"multi_device":    {},
}

// DeviceProofVerifier validates a device signature over a platform nonce.
// Implementations must reject malformed keys and never trust device metadata.
type DeviceProofVerifier interface {
	ValidatePublicKey(publicKey string) error
	Verify(publicKey string, message []byte, signature []byte) error
}

// Service creates, consumes, and revokes device provisioning state.
type Service struct {
	repository           repository.Repository
	proofVerifier        DeviceProofVerifier
	timeSource           clock.Clock
	tokenTTL             time.Duration
	challengeTTL         time.Duration
	registrationTokenTTL time.Duration
	sessionTTL           time.Duration
}

// Options contains device binding service dependencies and policy.
type Options struct {
	Repository           repository.Repository
	ProofVerifier        DeviceProofVerifier
	Clock                clock.Clock
	TokenTTL             time.Duration
	ChallengeTTL         time.Duration
	RegistrationTokenTTL time.Duration
	SessionTTL           time.Duration
}

// New creates the device binding service.
func New(options Options) (*Service, error) {
	if options.Repository == nil {
		return nil, errors.New("device binding repository is required")
	}
	if options.TokenTTL <= 0 {
		return nil, errors.New("device binding token TTL must be positive")
	}
	if options.ProofVerifier == nil {
		return nil, errors.New("device proof verifier is required")
	}
	challengeTTL := options.ChallengeTTL
	if challengeTTL <= 0 {
		challengeTTL = 2 * time.Minute
	}
	registrationTokenTTL := options.RegistrationTokenTTL
	if registrationTokenTTL <= 0 {
		registrationTokenTTL = 15 * time.Minute
	}
	sessionTTL := options.SessionTTL
	if sessionTTL <= 0 {
		sessionTTL = 30 * time.Minute
	}
	timeSource := options.Clock
	if timeSource == nil {
		timeSource = clock.SystemClock{}
	}
	return &Service{
		repository:           options.Repository,
		proofVerifier:        options.ProofVerifier,
		timeSource:           timeSource,
		tokenTTL:             options.TokenTTL,
		challengeTTL:         challengeTTL,
		registrationTokenTTL: registrationTokenTTL,
		sessionTTL:           sessionTTL,
	}, nil
}

// CreateToken creates a short-lived, single-use token for one device.
// Only the token hash is persisted; the plaintext is returned once.
func (s *Service) CreateToken(
	ctx context.Context,
	deviceID string,
) (string, *domain.BindingToken, error) {
	deviceID = strings.TrimSpace(deviceID)
	if !deviceIDPattern.MatchString(deviceID) {
		return "", nil, domain.ErrInvalidDeviceID
	}
	plainToken, err := randomToken()
	if err != nil {
		return "", nil, err
	}
	now := s.timeSource.Now().UTC()
	token := &domain.BindingToken{
		ID:        uuid.NewString(),
		DeviceID:  deviceID,
		TokenHash: hashToken(plainToken),
		ExpiresAt: now.Add(s.tokenTTL),
		CreatedAt: now,
	}
	if err := s.repository.CreateToken(ctx, token); err != nil {
		return "", nil, err
	}
	return plainToken, token, nil
}

// CreateRegistrationToken creates a short-lived registration grant during a
// controlled manufacturing or support flow. The device consumes it once.
func (s *Service) CreateRegistrationToken(
	ctx context.Context,
	deviceID string,
) (string, *domain.RegistrationToken, error) {
	deviceID = strings.TrimSpace(deviceID)
	if !deviceIDPattern.MatchString(deviceID) {
		return "", nil, domain.ErrInvalidDeviceID
	}
	plainToken, err := randomToken()
	if err != nil {
		return "", nil, err
	}
	now := s.timeSource.Now().UTC()
	token := &domain.RegistrationToken{
		ID:        uuid.NewString(),
		DeviceID:  deviceID,
		TokenHash: hashToken(plainToken),
		ExpiresAt: now.Add(s.registrationTokenTTL),
		CreatedAt: now,
	}
	if err := s.repository.CreateRegistrationToken(ctx, token); err != nil {
		return "", nil, err
	}
	return plainToken, token, nil
}

// RegisterDevice consumes a registration grant and stores the device public
// identity. Metadata is copied from the device but never trusted for auth.
func (s *Service) RegisterDevice(
	ctx context.Context,
	registrationToken string,
	input domain.DeviceRegistrationInput,
) (*domain.DeviceCredential, error) {
	registrationToken = strings.TrimSpace(registrationToken)
	if registrationToken == "" {
		return nil, domain.ErrRegistrationTokenNotFound
	}
	token, err := s.repository.GetRegistrationTokenByHash(
		ctx,
		hashToken(registrationToken),
	)
	if err != nil {
		return nil, err
	}
	now := s.timeSource.Now().UTC()
	if token.ConsumedAt != nil {
		return nil, domain.ErrRegistrationTokenConsumed
	}
	if !token.ExpiresAt.After(now) {
		return nil, domain.ErrRegistrationTokenExpired
	}
	if !deviceIDPattern.MatchString(strings.TrimSpace(input.DeviceID)) ||
		strings.TrimSpace(input.PublicKey) == "" {
		return nil, domain.ErrInvalidDeviceID
	}
	if token.DeviceID != strings.TrimSpace(input.DeviceID) {
		return nil, domain.ErrInvalidDeviceProof
	}
	credential := &domain.DeviceCredential{
		DeviceID:        token.DeviceID,
		HardwareModel:   strings.TrimSpace(input.HardwareModel),
		FirmwareVersion: strings.TrimSpace(input.FirmwareVersion),
		CapabilitySet:   normalizeCapabilities(input.Capabilities),
		PublicKey:       strings.TrimSpace(input.PublicKey),
		Status:          "active",
		RegisteredAt:    now,
		UpdatedAt:       now,
	}
	if len([]rune(credential.HardwareModel)) > 64 ||
		len([]rune(credential.FirmwareVersion)) > 64 ||
		len(credential.PublicKey) > 4096 {
		return nil, domain.ErrInvalidDeviceID
	}
	if len(credential.CapabilitySet) == 0 {
		return nil, domain.ErrInvalidDeviceID
	}
	if err := s.proofVerifier.ValidatePublicKey(credential.PublicKey); err != nil {
		return nil, domain.ErrInvalidDeviceProof
	}
	if err := s.repository.ConsumeRegistrationTokenAndUpsertCredential(
		ctx,
		token.ID,
		credential,
	); err != nil {
		return nil, err
	}
	return credential, nil
}

// StartDeviceAuthentication creates a short-lived nonce for one device.
func (s *Service) StartDeviceAuthentication(
	ctx context.Context,
	deviceID string,
) (string, *domain.DeviceChallenge, error) {
	deviceID = strings.TrimSpace(deviceID)
	credential, err := s.repository.GetDeviceCredential(ctx, deviceID)
	if err != nil {
		return "", nil, err
	}
	if credential.Status != "active" {
		return "", nil, domain.ErrDeviceDisabled
	}
	nonce, err := randomToken()
	if err != nil {
		return "", nil, err
	}
	now := s.timeSource.Now().UTC()
	challenge := &domain.DeviceChallenge{
		ID:        uuid.NewString(),
		DeviceID:  deviceID,
		NonceHash: hashToken(nonce),
		ExpiresAt: now.Add(s.challengeTTL),
		CreatedAt: now,
	}
	if err := s.repository.CreateDeviceChallenge(ctx, challenge); err != nil {
		return "", nil, err
	}
	return nonce, challenge, nil
}

// CompleteDeviceAuthentication verifies the device signature and consumes the
// challenge. The returned token is a short-lived opaque device session token.
func (s *Service) CompleteDeviceAuthentication(
	ctx context.Context,
	deviceID string,
	nonce string,
	signature []byte,
) (string, error) {
	deviceID = strings.TrimSpace(deviceID)
	nonce = strings.TrimSpace(nonce)
	if deviceID == "" || nonce == "" || len(signature) == 0 {
		return "", domain.ErrInvalidDeviceProof
	}
	credential, err := s.repository.GetDeviceCredential(ctx, deviceID)
	if err != nil {
		return "", err
	}
	if credential.Status != "active" {
		return "", domain.ErrDeviceDisabled
	}
	challenge, err := s.repository.GetDeviceChallengeByNonceHash(
		ctx,
		hashToken(nonce),
	)
	if err != nil {
		return "", err
	}
	if challenge.DeviceID != deviceID {
		return "", domain.ErrInvalidDeviceProof
	}
	now := s.timeSource.Now().UTC()
	if challenge.ConsumedAt != nil {
		return "", domain.ErrDeviceChallengeConsumed
	}
	if !challenge.ExpiresAt.After(now) {
		return "", domain.ErrDeviceChallengeExpired
	}
	if err := s.proofVerifier.Verify(
		credential.PublicKey,
		[]byte(deviceID+"."+nonce),
		signature,
	); err != nil {
		return "", domain.ErrInvalidDeviceProof
	}
	sessionToken, err := randomToken()
	if err != nil {
		return "", err
	}
	now = s.timeSource.Now().UTC()
	if err := s.repository.CompleteDeviceAuthentication(
		ctx,
		challenge.ID,
		deviceID,
		&domain.DeviceSession{
			ID:         uuid.NewString(),
			DeviceID:   deviceID,
			TokenHash:  hashToken(sessionToken),
			ExpiresAt:  now.Add(s.sessionTTL),
			CreatedAt:  now,
			LastUsedAt: now,
		},
	); err != nil {
		return "", err
	}
	return sessionToken, nil
}

// CreateDeviceProvisioningToken creates a single-use binding token after a
// device has authenticated. The token is the only value placed in QR or BLE
// payloads; it is not the device identity or an AI credential.
func (s *Service) CreateDeviceProvisioningToken(
	ctx context.Context,
	deviceID string,
	deviceSessionToken string,
) (string, *domain.BindingToken, error) {
	deviceID = strings.TrimSpace(deviceID)
	deviceSessionToken = strings.TrimSpace(deviceSessionToken)
	if deviceID == "" || deviceSessionToken == "" {
		return "", nil, domain.ErrDeviceSessionNotFound
	}
	session, err := s.repository.GetDeviceSessionByTokenHash(
		ctx,
		hashToken(deviceSessionToken),
	)
	if err != nil {
		return "", nil, err
	}
	if session.DeviceID != deviceID ||
		!session.ExpiresAt.After(s.timeSource.Now().UTC()) {
		return "", nil, domain.ErrDeviceSessionExpired
	}
	if _, err := s.repository.GetDeviceCredential(ctx, deviceID); err != nil {
		return "", nil, err
	}
	return s.CreateToken(ctx, deviceID)
}

// Bind consumes a provisioning token and binds the device to the parent.
func (s *Service) Bind(
	ctx context.Context,
	parentAccountID string,
	plainToken string,
	deviceName string,
) (*domain.Binding, error) {
	plainToken = strings.TrimSpace(plainToken)
	if plainToken == "" {
		return nil, domain.ErrTokenNotFound
	}
	token, err := s.repository.GetTokenByHash(ctx, hashToken(plainToken))
	if err != nil {
		return nil, err
	}
	now := s.timeSource.Now().UTC()
	if token.ConsumedAt != nil {
		return nil, domain.ErrTokenConsumed
	}
	if !token.ExpiresAt.After(now) {
		return nil, domain.ErrTokenExpired
	}
	credential, err := s.repository.GetDeviceCredential(ctx, token.DeviceID)
	if err != nil {
		return nil, err
	}
	if credential.Status != "active" {
		return nil, domain.ErrDeviceDisabled
	}
	deviceName = strings.TrimSpace(deviceName)
	if deviceName == "" {
		deviceName = "初芽"
	}
	if len([]rune(deviceName)) > 40 {
		return nil, domain.ErrInvalidDeviceID
	}

	binding := &domain.Binding{
		ID:              uuid.NewString(),
		ParentAccountID: parentAccountID,
		DeviceID:        token.DeviceID,
		DeviceName:      deviceName,
		HardwareModel:   credential.HardwareModel,
		FirmwareVersion: credential.FirmwareVersion,
		Capabilities:    append([]string(nil), credential.CapabilitySet...),
		BoundAt:         now,
		UpdatedAt:       now,
	}
	if err := s.repository.ConsumeTokenAndUpsertBinding(
		ctx,
		token.ID,
		binding,
	); err != nil {
		return nil, err
	}
	return binding, nil
}

// List returns the devices owned by one parent account.
func (s *Service) List(
	ctx context.Context,
	parentAccountID string,
) ([]domain.Binding, error) {
	return s.repository.ListByParentAccountID(ctx, parentAccountID)
}

// ListAllForAdmin returns every binding for the operations console.
//
// Callers must enforce administrator authorization before invoking this
// method; the method deliberately does not expose a parent-scoped substitute.
func (s *Service) ListAllForAdmin(ctx context.Context) ([]domain.Binding, error) {
	return s.repository.ListAllBindings(ctx)
}

// GetByDeviceID returns one durable binding for internal relay lookups.
//
// Unlike List, this is not scoped to a parent account. Callers must run on the
// internal service surface, never on a guardian-facing request.
func (s *Service) GetByDeviceID(
	ctx context.Context,
	deviceID string,
) (*domain.Binding, error) {
	deviceID = strings.TrimSpace(deviceID)
	if !deviceIDPattern.MatchString(deviceID) {
		return nil, domain.ErrInvalidDeviceID
	}
	return s.repository.GetByDeviceID(ctx, deviceID)
}

// VerifyDeviceSession resolves the device id for a live device session token.
//
// Device-scoped endpoints use this instead of a parent access token because the
// firmware never receives a guardian credential.
func (s *Service) VerifyDeviceSession(
	ctx context.Context,
	deviceSessionToken string,
) (string, error) {
	deviceSessionToken = strings.TrimSpace(deviceSessionToken)
	if deviceSessionToken == "" {
		return "", domain.ErrDeviceSessionNotFound
	}
	session, err := s.repository.GetDeviceSessionByTokenHash(
		ctx,
		hashToken(deviceSessionToken),
	)
	if err != nil {
		return "", err
	}
	if !session.ExpiresAt.After(s.timeSource.Now().UTC()) {
		return "", domain.ErrDeviceSessionExpired
	}
	return session.DeviceID, nil
}

// BindingStatus reports whether a device session belongs to a bound device.
//
// A device polls this after showing a QR code so it can leave the provisioning
// screen without ever learning the guardian account behind the binding.
func (s *Service) BindingStatus(
	ctx context.Context,
	deviceID string,
	deviceSessionToken string,
) (bool, error) {
	sessionDeviceID, err := s.VerifyDeviceSession(ctx, deviceSessionToken)
	if err != nil {
		return false, err
	}
	if sessionDeviceID != strings.TrimSpace(deviceID) {
		return false, domain.ErrInvalidDeviceProof
	}
	if _, err := s.repository.GetByDeviceID(ctx, sessionDeviceID); err != nil {
		if errors.Is(err, domain.ErrDeviceNotFound) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

// Delete removes a device binding owned by the parent account.
func (s *Service) Delete(
	ctx context.Context,
	parentAccountID string,
	deviceID string,
) error {
	deviceID = strings.TrimSpace(deviceID)
	if !deviceIDPattern.MatchString(deviceID) {
		return domain.ErrInvalidDeviceID
	}
	return s.repository.RevokeDeviceSessionsAndDeleteBinding(
		ctx,
		parentAccountID,
		deviceID,
	)
}

// RevokeSessions revokes every live device session and optionally disables the
// device credential. The binding is intentionally preserved: an operator
// revoking sessions for a security response must not silently unbind the
// family device.
func (s *Service) RevokeSessions(
	ctx context.Context,
	deviceID string,
	disableDevice bool,
) error {
	deviceID = strings.TrimSpace(deviceID)
	if !deviceIDPattern.MatchString(deviceID) {
		return domain.ErrInvalidDeviceID
	}
	if _, err := s.repository.GetByDeviceID(ctx, deviceID); err != nil {
		return err
	}
	return s.repository.RevokeDeviceSessions(ctx, deviceID, disableDevice)
}

func normalizeCapabilities(capabilities []string) []string {
	normalized := make([]string, 0, len(capabilities))
	seen := make(map[string]struct{}, len(capabilities))
	for _, capability := range capabilities {
		capability = strings.TrimSpace(capability)
		if _, supported := deviceCapabilities[capability]; !supported {
			continue
		}
		if _, exists := seen[capability]; exists {
			continue
		}
		seen[capability] = struct{}{}
		normalized = append(normalized, capability)
	}
	return normalized
}

func randomToken() (string, error) {
	bytes := make([]byte, 32)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	return hex.EncodeToString(bytes), nil
}

func hashToken(value string) string {
	hash := sha256.Sum256([]byte(value))
	return hex.EncodeToString(hash[:])
}
