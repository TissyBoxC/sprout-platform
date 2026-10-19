// Package service contains parent authentication use cases.
package service

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/auth/domain"
	"github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/auth/repository"
	childdomain "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/child/domain"
	familydomain "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/family/domain"
	operationsdomain "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/operations/domain"
	"github.com/TissyBoxC/sprout-platform/services/device_platform/internal/platform/clock"
	"github.com/TissyBoxC/sprout-platform/services/device_platform/internal/platform/security"
	"github.com/google/uuid"
	"github.com/pquerna/otp/totp"
	"golang.org/x/crypto/bcrypt"
)

const (
	accountStatusActive              = "active"
	defaultConsent                   = "2026-01"
	defaultMFAChallengeTTL           = 5 * time.Minute
	defaultMFAAccountLockDuration    = 15 * time.Minute
	defaultMFAMaxAttempts            = 5
	defaultTOTPKeyVersion            = 1
	defaultTOTPPeriodSeconds         = 30
	phoneVerificationPurposeRegister = "register"
	phoneVerificationPurposeLogin    = "login"
	phoneVerificationCodeTTL         = 5 * time.Minute
)

// AIAccountProvisioner creates or repairs the parent's AI execution account.
// It must be idempotent for one parent account id.
type AIAccountProvisioner interface {
	EnsureForParent(
		ctx context.Context,
		parentAccountID string,
		parentEmail string,
	) (*domain.AIAccountSummary, error)
	GetForParent(
		ctx context.Context,
		parentAccountID string,
	) (*domain.AIAccountSummary, error)
}

// Service owns parent registration, login, token rotation, and account reads.
type Service struct {
	repository       repository.Repository
	tokenIssuer      security.TokenIssuer
	aiProvisioner    AIAccountProvisioner
	childProvisioner ChildAccountProvisioner
	ownerProvisioner FamilyOwnerProvisioner
	mfaCipher        MFACipher
	phoneVerifier    PhoneVerifier
	overviewReader   ParentOverviewReader
	policyReader     RegistrationPolicyReader
	mfaAuditor       MFAAuditor
	readinessReader  ReadinessReader
	timeSource       clock.Clock
	accessTTL        time.Duration
	refreshTTL       time.Duration
	mfaChallengeTTL  time.Duration
	mfaMaxAttempts   int
	mfaLockDuration  time.Duration
}

// MFAAuditor writes the mandatory administrator MFA security trail. A nil
// auditor means the deployment is missing a required security dependency and
// MFA fails closed.
type MFAAuditor interface {
	RecordAudit(
		ctx context.Context,
		actorAccountID string,
		targetAccountID string,
		action string,
		detail map[string]any,
	) error
}

// ParentOverviewReader supplies the guardian dashboard projection.
//
// The auth module owns the endpoint but not device or usage persistence, so
// this dependency stays narrow and read-only.
type ParentOverviewReader interface {
	ParentOverview(
		ctx context.Context,
		parentAccountID string,
	) (*domain.ParentOverview, error)
}

// RegistrationPolicyReader exposes the registration and sign-in switches owned
// by the operations module. Keeping it narrow avoids a package cycle while
// still making the administrative settings authoritative.
type RegistrationPolicyReader interface {
	RuntimePolicy(ctx context.Context) (*operationsdomain.RuntimePolicy, error)
}

// ChildAccountProvisioner stores the optional child details entered during
// guardian registration. It is deliberately narrow so authentication does not
// depend on the child repository or its policy implementation.
type ChildAccountProvisioner interface {
	SyncRegistrationProfile(
		ctx context.Context,
		familyID string,
		nickname string,
		birthday string,
		guardianConsentVersion string,
	) (*childdomain.Child, error)
}

// PhoneVerifier sends and validates guardian mobile verification codes.
//
// The local verifier is enabled only through an explicit development setting.
// It accepts the fixed 000000 development code while SMS delivery is not
// configured; an empty code never bypasses verification.
type PhoneVerifier interface {
	SendVerificationCode(ctx context.Context, phone string, purpose string) error
	VerifyCode(ctx context.Context, phone string, purpose string, code string) error
}

// MFACipher encrypts administrator TOTP secrets before persistence.
type MFACipher interface {
	Encrypt(plaintext []byte) (ciphertext []byte, nonce []byte, err error)
	Decrypt(ciphertext []byte, nonce []byte) ([]byte, error)
}

// Options contains authentication service dependencies and policies.
type Options struct {
	Repository             repository.Repository
	TokenIssuer            security.TokenIssuer
	AIProvisioner          AIAccountProvisioner
	ChildProvisioner       ChildAccountProvisioner
	MFACipher              MFACipher
	PhoneVerifier          PhoneVerifier
	OverviewReader         ParentOverviewReader
	PolicyReader           RegistrationPolicyReader
	MFAAuditor             MFAAuditor
	ReadinessReader        ReadinessReader
	MFAChallengeTTL        time.Duration
	MFAMaxAttempts         int
	MFAAccountLockDuration time.Duration
	Clock                  clock.Clock
	AccessTTL              time.Duration
	RefreshTTL             time.Duration
}

// New creates the parent authentication service.
func New(options Options) (*Service, error) {
	if options.Repository == nil {
		return nil, errors.New("authentication repository is required")
	}
	if options.TokenIssuer == nil {
		return nil, errors.New("token issuer is required")
	}
	if options.AccessTTL <= 0 || options.RefreshTTL <= 0 {
		return nil, errors.New("authentication token TTLs must be positive")
	}
	mfaChallengeTTL := options.MFAChallengeTTL
	if mfaChallengeTTL <= 0 {
		mfaChallengeTTL = defaultMFAChallengeTTL
	}
	mfaMaxAttempts := options.MFAMaxAttempts
	if mfaMaxAttempts <= 0 {
		mfaMaxAttempts = defaultMFAMaxAttempts
	}
	mfaLockDuration := options.MFAAccountLockDuration
	if mfaLockDuration <= 0 {
		mfaLockDuration = defaultMFAAccountLockDuration
	}
	timeSource := options.Clock
	if timeSource == nil {
		timeSource = clock.SystemClock{}
	}
	readinessReader := options.ReadinessReader
	if readinessReader == nil {
		if reader, ok := options.Repository.(ReadinessReader); ok {
			readinessReader = reader
		}
	}
	return &Service{
		repository:       options.Repository,
		tokenIssuer:      options.TokenIssuer,
		aiProvisioner:    options.AIProvisioner,
		childProvisioner: options.ChildProvisioner,
		mfaCipher:        options.MFACipher,
		phoneVerifier:    options.PhoneVerifier,
		overviewReader:   options.OverviewReader,
		policyReader:     options.PolicyReader,
		mfaAuditor:       options.MFAAuditor,
		readinessReader:  readinessReader,
		timeSource:       timeSource,
		accessTTL:        options.AccessTTL,
		refreshTTL:       options.RefreshTTL,
		mfaChallengeTTL:  mfaChallengeTTL,
		mfaMaxAttempts:   mfaMaxAttempts,
		mfaLockDuration:  mfaLockDuration,
	}, nil
}

// SetMFAAuditor wires the administrator security audit after both the auth
// and privacy services exist. MFA remains unavailable until this is set.
func (s *Service) SetMFAAuditor(auditor MFAAuditor) {
	s.mfaAuditor = auditor
}

// SetOverviewReader wires the optional dashboard projection after both
// modules are constructed. Keeping this setter separate avoids a package cycle
// between authentication and operations.
func (s *Service) SetOverviewReader(reader ParentOverviewReader) {
	s.overviewReader = reader
}

// SetPolicyReader wires the operations policy after both modules are built.
func (s *Service) SetPolicyReader(reader RegistrationPolicyReader) {
	s.policyReader = reader
}

// SetChildProvisioner wires the child profile module after construction so
// authentication can stay independently removable and testable.
func (s *Service) SetChildProvisioner(provisioner ChildAccountProvisioner) {
	s.childProvisioner = provisioner
}

// FamilyOwnerProvisioner makes a newly registered guardian the owner of its
// family. It is deliberately narrow so authentication stays removable and does
// not depend on the family repository.
type FamilyOwnerProvisioner interface {
	BootstrapOwner(
		ctx context.Context,
		familyID string,
		userID string,
	) (familydomain.Member, error)
}

// SetFamilyOwnerProvisioner wires the family module after construction. A
// failure to create the owner is treated as a registration failure so a
// guardian never ends up with a family that has no owner.
func (s *Service) SetFamilyOwnerProvisioner(provisioner FamilyOwnerProvisioner) {
	s.ownerProvisioner = provisioner
}

// ParentOverview returns the authenticated guardian's dashboard counters.
func (s *Service) ParentOverview(
	ctx context.Context,
	parentAccountID string,
) (*domain.ParentOverview, error) {
	if _, err := s.GetIdentity(ctx, parentAccountID); err != nil {
		return nil, err
	}
	if s.overviewReader == nil {
		return nil, domain.ErrAIAccountUnavailable
	}
	return s.overviewReader.ParentOverview(ctx, parentAccountID)
}

// Register creates a guardian account and best-effort creates its AI account.
//
// A temporary AI gateway outage must not discard a valid parent registration,
// so a failed provisioning attempt returns a nil summary instead of an error.
// The AI projection is repaired by a later login or account read.
func (s *Service) Register(
	ctx context.Context,
	input domain.RegisterInput,
) (*domain.ParentAccount, *domain.TokenPair, *domain.AIAccountSummary, error) {
	policy, err := s.registrationPolicy(ctx)
	if err != nil {
		return nil, nil, nil, err
	}
	if !policy.RegistrationEnabled {
		return nil, nil, nil, domain.ErrRegistrationDisabled
	}
	input.Phone = strings.TrimSpace(input.Phone)
	input.GuardianFamilyName = strings.TrimSpace(input.GuardianFamilyName)
	input.ChildNickname = strings.TrimSpace(input.ChildNickname)
	input.ChildBirthday = strings.TrimSpace(input.ChildBirthday)
	input.GuardianConsentVersion = strings.TrimSpace(input.GuardianConsentVersion)
	if input.GuardianConsentVersion == "" {
		input.GuardianConsentVersion = defaultConsent
	}
	if err := validateRegistration(input); err != nil {
		return nil, nil, nil, err
	}
	input.Phone = normalizePhone(input.Phone)
	if policy.PhoneVerificationRequired {
		if err := s.verifyPhoneCode(
			ctx,
			input.Phone,
			phoneVerificationPurposeRegister,
			input.PhoneVerificationCode,
		); err != nil {
			return nil, nil, nil, err
		}
	}

	passwordHash, err := bcrypt.GenerateFromPassword(
		[]byte(input.Password),
		bcrypt.DefaultCost,
	)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("hash password: %w", err)
	}

	now := s.timeSource.Now().UTC()
	account := &domain.ParentAccount{
		ID:                     uuid.NewString(),
		Phone:                  input.Phone,
		PasswordHash:           string(passwordHash),
		DisplayName:            guardianDisplayName(input),
		GuardianFamilyName:     input.GuardianFamilyName,
		ChildNickname:          input.ChildNickname,
		ChildBirthday:          input.ChildBirthday,
		Status:                 accountStatusActive,
		Role:                   domain.RoleParent,
		GuardianConsentVersion: input.GuardianConsentVersion,
		GuardianConsentedAt:    now,
		PhoneVerifiedAt:        &now,
		CreatedAt:              now,
		UpdatedAt:              now,
	}
	if err := s.repository.CreateParentAccount(ctx, account); err != nil {
		return nil, nil, nil, err
	}
	if s.ownerProvisioner != nil {
		if _, err := s.ownerProvisioner.BootstrapOwner(
			ctx,
			account.ID,
			account.ID,
		); err != nil {
			// A family must always have an owner, so a bootstrap failure fails
			// the registration instead of leaving an ownerless family behind.
			_ = s.repository.DeleteParentAccount(ctx, account.ID)
			return nil, nil, nil, err
		}
	}
	if s.childProvisioner != nil && input.ChildNickname != "" {
		if _, err := s.childProvisioner.SyncRegistrationProfile(
			ctx,
			account.ID,
			input.ChildNickname,
			input.ChildBirthday,
			input.GuardianConsentVersion,
		); err != nil {
			// The account and child profile are one user-visible registration.
			// Do not leave a guardian without the requested child profile.
			_ = s.repository.DeleteParentAccount(ctx, account.ID)
			return nil, nil, nil, err
		}
	}

	tokenPair, err := s.issueSession(ctx, account.ID)
	if err != nil {
		return nil, nil, nil, err
	}
	var summary *domain.AIAccountSummary
	if s.aiProvisioner != nil {
		// AI provisioning is deliberately after account and session creation:
		// the parent account remains recoverable when the AI gateway is down.
		summary, _ = s.aiProvisioner.EnsureForParent(ctx, account.ID, account.Email)
	}
	return account, tokenPair, summary, nil
}

// Login validates credentials, creates a renewable session, and repairs the
// parent's AI account projection when the provider is reachable.
func (s *Service) Login(
	ctx context.Context,
	input domain.LoginInput,
) (*domain.ParentAccount, *domain.TokenPair, *domain.AIAccountSummary, error) {
	identifier := strings.TrimSpace(input.Identifier)
	if identifier == "" || input.Password == "" {
		return nil, nil, nil, domain.ErrInvalidCredentials
	}
	account, err := s.resolveParentAccountByIdentifier(ctx, identifier)
	if err != nil {
		if errors.Is(err, domain.ErrAccountNotFound) {
			return nil, nil, nil, domain.ErrInvalidCredentials
		}
		return nil, nil, nil, err
	}
	if account.Status != accountStatusActive {
		return nil, nil, nil, domain.ErrAccountDisabled
	}
	if err := bcrypt.CompareHashAndPassword(
		[]byte(account.PasswordHash),
		[]byte(input.Password),
	); err != nil {
		return nil, nil, nil, domain.ErrInvalidCredentials
	}
	// Administrators must use the dedicated password plus TOTP flow. Falling
	// through here would issue a normal session and bypass the second factor.
	if account.Role == domain.RoleAdmin {
		return nil, nil, nil, domain.ErrMFARequired
	}

	if err := s.repository.UpdateLastLogin(ctx, account.ID); err != nil {
		return nil, nil, nil, err
	}
	tokenPair, err := s.issueSession(ctx, account.ID)
	if err != nil {
		return nil, nil, nil, err
	}
	var summary *domain.AIAccountSummary
	if s.aiProvisioner != nil {
		summary, _ = s.aiProvisioner.EnsureForParent(ctx, account.ID, account.Email)
	}
	return account, tokenPair, summary, nil
}

// SendPhoneVerificationCode requests an SMS verification code for registration
// or future phone-based sign-in. The verifier may be a real provider or the
// development bypass; neither exposes the code to callers.
func (s *Service) SendPhoneVerificationCode(
	ctx context.Context,
	phone string,
	purpose string,
) error {
	policy, err := s.registrationPolicy(ctx)
	if err != nil {
		return err
	}
	if purpose == phoneVerificationPurposeRegister && !policy.RegistrationEnabled {
		return domain.ErrRegistrationDisabled
	}
	if !isValidPhone(phone) {
		return domain.ErrInvalidPhone
	}
	if purpose == "" {
		purpose = phoneVerificationPurposeRegister
	}
	if purpose != phoneVerificationPurposeRegister &&
		purpose != phoneVerificationPurposeLogin {
		return domain.ErrInvalidVerification
	}
	if s.phoneVerifier == nil {
		return domain.ErrPhoneVerification
	}
	return s.phoneVerifier.SendVerificationCode(
		ctx,
		normalizePhone(phone),
		purpose,
	)
}

// BindEmail adds an optional email sign-in identifier to one authenticated
// guardian. The email is unique across all account roles so it cannot be
// claimed by two accounts.
func (s *Service) BindEmail(
	ctx context.Context,
	accountID string,
	input domain.BindEmailInput,
) (*domain.ParentAccount, error) {
	email := normalizeEmail(input.Email)
	if !isValidEmail(email) {
		return nil, domain.ErrInvalidEmail
	}
	if input.Password == "" {
		return nil, domain.ErrInvalidCredentials
	}
	account, err := s.repository.GetParentAccountByID(ctx, accountID)
	if err != nil {
		return nil, err
	}
	if account.Status != accountStatusActive {
		return nil, domain.ErrAccountDisabled
	}
	if err := bcrypt.CompareHashAndPassword(
		[]byte(account.PasswordHash),
		[]byte(input.Password),
	); err != nil {
		return nil, domain.ErrInvalidCredentials
	}
	if err := s.repository.UpdateEmail(ctx, account.ID, email); err != nil {
		return nil, err
	}
	account.Email = email
	now := s.timeSource.Now().UTC()
	account.EmailVerifiedAt = &now
	account.UpdatedAt = now
	return account, nil
}

// StartAdminLogin validates an administrator password and creates a
// short-lived MFA challenge. No access token is issued before TOTP succeeds.
// The challenge is persisted before its mandatory audit event; if the audit
// store is unavailable, the token is withheld and the challenge is removed so
// an operator never sees a started event for an unusable challenge.
func (s *Service) StartAdminLogin(
	ctx context.Context,
	input domain.LoginInput,
) (string, *domain.ParentAccount, error) {
	identifier := strings.TrimSpace(input.Identifier)
	if identifier == "" || input.Password == "" {
		return "", nil, domain.ErrInvalidCredentials
	}
	account, err := s.resolveParentAccountByIdentifier(ctx, identifier)
	if err != nil {
		if errors.Is(err, domain.ErrAccountNotFound) {
			return "", nil, domain.ErrInvalidCredentials
		}
		return "", nil, err
	}
	if account.Role != domain.RoleAdmin {
		return "", nil, domain.ErrInsufficientPrivilege
	}
	if account.Status != accountStatusActive {
		return "", nil, domain.ErrAccountDisabled
	}
	if err := bcrypt.CompareHashAndPassword(
		[]byte(account.PasswordHash),
		[]byte(input.Password),
	); err != nil {
		return "", nil, domain.ErrInvalidCredentials
	}
	if _, err := s.repository.GetTOTPCredential(ctx, account.ID); err != nil {
		return "", nil, domain.ErrMFANotConfigured
	}

	now := s.timeSource.Now().UTC()
	accountFailureState, err := s.repository.ClearExpiredMFAAccountLock(
		ctx,
		account.ID,
		now,
	)
	if err != nil {
		return "", nil, err
	}
	if accountFailureState.LockedUntil != nil &&
		accountFailureState.LockedUntil.After(now) {
		if err := s.recordMFAEvent(
			ctx,
			account.ID,
			account.ID,
			"admin.mfa.account_locked",
			map[string]any{
				"outcome":         "account_locked",
				"failed_attempts": accountFailureState.FailedAttempts,
			},
		); err != nil {
			return "", nil, err
		}
		return "", nil, domain.ErrMFAAccountLocked
	}
	if accountFailureState.Recovered {
		if err := s.recordMFAEvent(
			ctx,
			account.ID,
			account.ID,
			"admin.mfa.account_recovered",
			map[string]any{
				"outcome":         "lock_expired",
				"failed_attempts": 0,
			},
		); err != nil {
			return "", nil, err
		}
	}

	challengeToken, err := randomToken()
	if err != nil {
		return "", nil, fmt.Errorf("generate MFA challenge: %w", err)
	}
	if s.mfaAuditor == nil {
		return "", nil, domain.ErrMFAAuditUnavailable
	}
	challenge := &domain.MFAChallenge{
		ID:              uuid.NewString(),
		ParentAccountID: account.ID,
		ChallengeHash:   hashRefreshToken(challengeToken),
		ExpiresAt:       now.Add(s.mfaChallengeTTL),
		FailedAttempts:  accountFailureState.FailedAttempts,
		CreatedAt:       now,
	}
	if err := s.repository.CreateMFAChallenge(ctx, challenge); err != nil {
		return "", nil, err
	}
	if err := s.recordMFAEvent(
		ctx,
		account.ID,
		account.ID,
		"admin.mfa.challenge.started",
		map[string]any{"outcome": "challenge_started"},
	); err != nil {
		if deleteErr := s.repository.DeleteMFAChallenge(ctx, challenge.ID); deleteErr != nil {
			return "", nil, errors.Join(err, fmt.Errorf(
				"remove unaudited MFA challenge: %w",
				deleteErr,
			))
		}
		return "", nil, err
	}
	return challengeToken, account, nil
}

// CompleteAdminLogin validates a TOTP code and consumes the challenge exactly
// once before issuing a normal administrator session.
func (s *Service) CompleteAdminLogin(
	ctx context.Context,
	input domain.AdminMFALoginInput,
) (*domain.ParentAccount, *domain.TokenPair, error) {
	input.ChallengeToken = strings.TrimSpace(input.ChallengeToken)
	input.Code = strings.TrimSpace(input.Code)
	if input.ChallengeToken == "" || len(input.Code) != 6 {
		return nil, nil, domain.ErrInvalidMFACode
	}
	if s.mfaAuditor == nil {
		return nil, nil, domain.ErrMFAAuditUnavailable
	}
	challenge, err := s.repository.GetMFAChallengeByHash(
		ctx,
		hashRefreshToken(input.ChallengeToken),
	)
	if err != nil {
		return nil, nil, err
	}
	now := s.timeSource.Now().UTC()
	if challenge.ConsumedAt != nil {
		if auditErr := s.recordMFAEvent(
			ctx,
			challenge.ParentAccountID,
			challenge.ParentAccountID,
			"admin.mfa.replayed",
			map[string]any{"outcome": "challenge_replayed"},
		); auditErr != nil {
			return nil, nil, auditErr
		}
		return nil, nil, domain.ErrMFAChallengeConsumed
	}
	if !challenge.ExpiresAt.After(now) {
		if auditErr := s.recordMFAEvent(
			ctx,
			challenge.ParentAccountID,
			challenge.ParentAccountID,
			"admin.mfa.expired",
			map[string]any{"outcome": "challenge_expired"},
		); auditErr != nil {
			return nil, nil, auditErr
		}
		return nil, nil, domain.ErrMFAChallengeExpired
	}
	if challenge.LockedAt != nil {
		if auditErr := s.recordMFAChallengeLockedObservation(
			ctx,
			challenge.ParentAccountID,
			challenge.FailedAttempts,
		); auditErr != nil {
			return nil, nil, auditErr
		}
		return nil, nil, domain.ErrMFAAccountLocked
	}
	account, err := s.repository.GetParentAccountByID(
		ctx,
		challenge.ParentAccountID,
	)
	if err != nil {
		return nil, nil, err
	}
	if account.Role != domain.RoleAdmin || account.Status != accountStatusActive {
		if auditErr := s.recordMFAEvent(
			ctx,
			account.ID,
			account.ID,
			"admin.mfa.denied",
			map[string]any{"outcome": "insufficient_privilege"},
		); auditErr != nil {
			return nil, nil, auditErr
		}
		return nil, nil, domain.ErrInsufficientPrivilege
	}
	if s.mfaCipher == nil {
		return nil, nil, domain.ErrMFANotConfigured
	}
	credential, err := s.repository.GetTOTPCredential(ctx, account.ID)
	if err != nil {
		return nil, nil, err
	}
	accountFailureState, err := s.repository.GetMFAAccountFailureState(
		ctx,
		account.ID,
	)
	if err != nil {
		return nil, nil, err
	}
	if accountFailureState.LockedUntil != nil &&
		accountFailureState.LockedUntil.After(now) {
		if auditErr := s.recordMFAAccountLockedObservation(
			ctx,
			account.ID,
			accountFailureState.FailedAttempts,
		); auditErr != nil {
			return nil, nil, auditErr
		}
		return nil, nil, domain.ErrMFAAccountLocked
	}
	secret, err := s.mfaCipher.Decrypt(
		credential.EncryptedSecret,
		credential.SecretNonce,
	)
	if err != nil {
		return nil, nil, domain.ErrInvalidMFACode
	}
	if !totp.Validate(input.Code, string(secret)) {
		updated, accountState, failureOutcome, failureErr :=
			s.repository.RecordMFAChallengeFailure(
				ctx,
				challenge.ID,
				s.mfaMaxAttempts,
				s.mfaLockDuration,
				now,
			)
		if failureErr != nil {
			observed := updated
			if observed == nil {
				observed = challenge
			}
			if auditErr := s.auditMFAFailureRejection(
				ctx,
				observed,
				failureErr,
			); auditErr != nil {
				return nil, nil, auditErr
			}
			return nil, nil, failureErr
		}
		if accountState != nil && accountState.LockedUntil != nil {
			if failureOutcome == repository.MFAFailureLockedNow {
				if err := s.recordMFAEvent(
					ctx,
					account.ID,
					account.ID,
					"admin.mfa.locked",
					map[string]any{
						"outcome":         "account_locked",
						"failed_attempts": accountState.FailedAttempts,
					},
				); err != nil {
					return nil, nil, err
				}
			} else {
				if err := s.recordMFAAccountLockedObservation(
					ctx,
					account.ID,
					accountState.FailedAttempts,
				); err != nil {
					return nil, nil, err
				}
			}
			return nil, nil, domain.ErrMFAAccountLocked
		}
		if err := s.recordMFAEvent(
			ctx,
			account.ID,
			account.ID,
			"admin.mfa.failed",
			map[string]any{
				"outcome":            "invalid_code",
				"failed_attempts":    accountState.FailedAttempts,
				"remaining_attempts": max(0, s.mfaMaxAttempts-accountState.FailedAttempts),
			},
		); err != nil {
			return nil, nil, err
		}
		return nil, nil, domain.ErrInvalidMFACode
	}
	if err := s.repository.ConsumeMFAChallenge(ctx, challenge.ID, now); err != nil {
		if errors.Is(err, domain.ErrMFAAccountLocked) {
			currentState, stateErr := s.repository.GetMFAAccountFailureState(
				ctx,
				account.ID,
			)
			if stateErr != nil {
				return nil, nil, stateErr
			}
			if auditErr := s.recordMFAAccountLockedObservation(
				ctx,
				account.ID,
				currentState.FailedAttempts,
			); auditErr != nil {
				return nil, nil, auditErr
			}
			return nil, nil, domain.ErrMFAAccountLocked
		}
		if errors.Is(err, domain.ErrMFAChallengeLocked) &&
			!errors.Is(err, domain.ErrMFAAccountLocked) {
			current, readErr := s.repository.GetMFAChallengeByHash(
				ctx,
				challenge.ChallengeHash,
			)
			if readErr != nil {
				return nil, nil, readErr
			}
			if auditErr := s.recordMFAChallengeLockedObservation(
				ctx,
				current.ParentAccountID,
				current.FailedAttempts,
			); auditErr != nil {
				return nil, nil, auditErr
			}
			return nil, nil, domain.ErrMFAAccountLocked
		} else if errors.Is(err, domain.ErrMFAChallengeExpired) ||
			errors.Is(err, domain.ErrMFAChallengeConsumed) {
			if auditErr := s.recordMFAEvent(
				ctx,
				account.ID,
				account.ID,
				"admin.mfa."+mfaChallengeResultAction(err),
				map[string]any{"outcome": mfaChallengeResultAction(err)},
			); auditErr != nil {
				return nil, nil, auditErr
			}
		}
		return nil, nil, err
	}
	if err := s.recordMFAEvent(
		ctx,
		account.ID,
		account.ID,
		"admin.mfa.succeeded",
		map[string]any{"outcome": "succeeded"},
	); err != nil {
		return nil, nil, err
	}
	if err := s.repository.UpdateLastLogin(ctx, account.ID); err != nil {
		return nil, nil, err
	}
	tokenPair, err := s.issueSession(ctx, account.ID)
	if err != nil {
		return nil, nil, err
	}
	return account, tokenPair, nil
}

func (s *Service) recordMFAAccountLockedObservation(
	ctx context.Context,
	accountID string,
	failedAttempts int,
) error {
	return s.recordMFAEvent(
		ctx,
		accountID,
		accountID,
		"admin.mfa.locked_observed",
		map[string]any{
			"outcome":         "account_locked",
			"failed_attempts": failedAttempts,
		},
	)
}

// recordMFAChallengeLockedObservation emits one audit event for each request
// that observes the terminal lock created by another concurrent attempt.
// The single lock transition emits admin.mfa.locked; every request that loses
// the race emits this observation event with no challenge token or TOTP.
func (s *Service) recordMFAChallengeLockedObservation(
	ctx context.Context,
	accountID string,
	failedAttempts int,
) error {
	if err := s.recordMFAEvent(
		ctx,
		accountID,
		accountID,
		"admin.mfa.locked_observed",
		map[string]any{
			"outcome":         "account_locked",
			"failed_attempts": failedAttempts,
		},
	); err != nil {
		return err
	}
	return nil
}

func (s *Service) recordMFAEvent(
	ctx context.Context,
	actorAccountID string,
	targetAccountID string,
	action string,
	detail map[string]any,
) error {
	if s.mfaAuditor == nil {
		return domain.ErrMFAAuditUnavailable
	}
	if err := s.mfaAuditor.RecordAudit(
		ctx,
		actorAccountID,
		targetAccountID,
		action,
		detail,
	); err != nil {
		return domain.ErrMFAAuditUnavailable
	}
	return nil
}

func mfaChallengeResultAction(err error) string {
	switch {
	case errors.Is(err, domain.ErrMFAChallengeConsumed):
		return "replayed"
	case errors.Is(err, domain.ErrMFAAccountLocked):
		return "locked"
	default:
		return "expired"
	}
}

// EnrollAdminTOTP creates and encrypts a TOTP secret for one administrator.
// The plaintext secret and otpauth URI are returned only to the controlled CLI
// bootstrap flow and are never stored in plaintext.
func (s *Service) EnrollAdminTOTP(
	ctx context.Context,
	accountID string,
) (string, error) {
	account, err := s.repository.GetParentAccountByID(ctx, accountID)
	if err != nil {
		return "", err
	}
	if account.Role != domain.RoleAdmin {
		return "", domain.ErrInsufficientPrivilege
	}
	if _, err := s.repository.GetTOTPCredential(ctx, account.ID); err == nil {
		return "", domain.ErrTOTPAlreadyConfigured
	} else if !errors.Is(err, domain.ErrMFANotConfigured) {
		return "", err
	}
	if s.mfaCipher == nil {
		return "", domain.ErrMFANotConfigured
	}
	key, err := totp.Generate(totp.GenerateOpts{
		Issuer:      "如此萌屋",
		AccountName: account.Email,
		Period:      defaultTOTPPeriodSeconds,
	})
	if err != nil {
		return "", fmt.Errorf("generate administrator TOTP secret: %w", err)
	}
	encryptedSecret, nonce, err := s.mfaCipher.Encrypt([]byte(key.Secret()))
	if err != nil {
		return "", fmt.Errorf("encrypt administrator TOTP secret: %w", err)
	}
	now := s.timeSource.Now().UTC()
	if err := s.repository.UpsertTOTPCredential(ctx, &domain.TOTPCredential{
		ParentAccountID: account.ID,
		EncryptedSecret: encryptedSecret,
		SecretNonce:     nonce,
		KeyVersion:      defaultTOTPKeyVersion,
		EnabledAt:       &now,
		CreatedAt:       now,
		UpdatedAt:       now,
	}); err != nil {
		return "", err
	}
	return key.URL(), nil
}

// GetIdentity returns the authenticated account without provisioning AI state.
// Authorization checks must use this method to avoid side effects.
func (s *Service) GetIdentity(
	ctx context.Context,
	accountID string,
) (*domain.ParentAccount, error) {
	account, err := s.repository.GetParentAccountByID(ctx, accountID)
	if err != nil {
		return nil, err
	}
	if account.Status != accountStatusActive {
		return nil, domain.ErrAccountDisabled
	}
	return account, nil
}

// Refresh rotates a refresh token and returns a fresh access token.
func (s *Service) Refresh(
	ctx context.Context,
	refreshToken string,
) (*domain.TokenPair, error) {
	refreshToken = strings.TrimSpace(refreshToken)
	if refreshToken == "" {
		return nil, domain.ErrSessionNotFound
	}
	session, err := s.repository.GetSessionByRefreshTokenHash(
		ctx,
		hashRefreshToken(refreshToken),
	)
	if err != nil {
		return nil, err
	}
	if !session.ExpiresAt.After(s.timeSource.Now().UTC()) {
		_ = s.repository.RevokeSession(ctx, session.ID)
		return nil, domain.ErrSessionExpired
	}
	account, err := s.repository.GetParentAccountByID(ctx, session.ParentAccountID)
	if err != nil {
		return nil, err
	}
	if account.Status != accountStatusActive {
		return nil, domain.ErrAccountDisabled
	}

	newRefreshToken, err := randomToken()
	if err != nil {
		return nil, fmt.Errorf("generate refresh token: %w", err)
	}
	expiresAt := s.timeSource.Now().UTC().Add(s.refreshTTL)
	if err := s.repository.RotateSession(
		ctx,
		session.ID,
		hashRefreshToken(refreshToken),
		hashRefreshToken(newRefreshToken),
		expiresAt,
	); err != nil {
		return nil, err
	}
	accessToken, err := s.issueAccessToken(account.ID, session.ID)
	if err != nil {
		return nil, err
	}
	return &domain.TokenPair{
		AccessToken:  accessToken,
		RefreshToken: newRefreshToken,
		ExpiresIn:    s.accessTTL,
	}, nil
}

// Logout revokes one session. Repeated logout is intentionally successful.
func (s *Service) Logout(ctx context.Context, refreshToken string) error {
	if strings.TrimSpace(refreshToken) == "" {
		return nil
	}
	session, err := s.repository.GetSessionByRefreshTokenHash(
		ctx,
		hashRefreshToken(refreshToken),
	)
	if errors.Is(err, domain.ErrSessionNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	return s.repository.RevokeSession(ctx, session.ID)
}

// GetAccount returns the authenticated parent and parent-safe AI summary.
func (s *Service) GetAccount(
	ctx context.Context,
	accountID string,
) (*domain.ParentAccount, *domain.AIAccountSummary, error) {
	account, err := s.GetIdentity(ctx, accountID)
	if err != nil {
		return nil, nil, err
	}
	if s.aiProvisioner == nil {
		return account, nil, nil
	}
	summary, err := s.aiProvisioner.EnsureForParent(ctx, account.ID, account.Email)
	if err != nil {
		// A provider outage is reported as an unavailable AI summary rather
		// than turning a valid parent login into an authentication failure.
		return account, nil, nil
	}
	return account, summary, nil
}

// VerifyAccessToken returns the subject for a valid bearer access token.
func (s *Service) VerifyAccessToken(ctx context.Context, accessToken string) (string, error) {
	claims, err := s.tokenIssuer.Verify(accessToken)
	if err != nil {
		return "", err
	}
	if _, err := s.repository.GetActiveSessionByID(
		ctx,
		claims.SessionID,
		claims.Subject,
	); err != nil {
		return "", domain.ErrSessionNotFound
	}
	account, err := s.repository.GetParentAccountByID(ctx, claims.Subject)
	if err != nil {
		return "", err
	}
	if account.Status != accountStatusActive {
		return "", domain.ErrAccountDisabled
	}
	return claims.Subject, nil
}

func (s *Service) issueSession(
	ctx context.Context,
	parentAccountID string,
) (*domain.TokenPair, error) {
	refreshToken, err := randomToken()
	if err != nil {
		return nil, fmt.Errorf("generate refresh token: %w", err)
	}
	now := s.timeSource.Now().UTC()
	sessionID := uuid.NewString()
	accessToken, err := s.issueAccessToken(parentAccountID, sessionID)
	if err != nil {
		return nil, err
	}
	session := &domain.Session{
		ID:               sessionID,
		ParentAccountID:  parentAccountID,
		RefreshTokenHash: hashRefreshToken(refreshToken),
		ExpiresAt:        now.Add(s.refreshTTL),
		CreatedAt:        now,
		LastUsedAt:       now,
	}
	if err := s.repository.CreateSession(ctx, session); err != nil {
		return nil, err
	}
	return &domain.TokenPair{
		AccessToken:  accessToken,
		RefreshToken: refreshToken,
		ExpiresIn:    s.accessTTL,
	}, nil
}

// auditMFAFailureRejection preserves one audit event for a wrong code that
// cannot advance the counter because another request already changed the
// challenge. The repository returns the observed row together with the domain
// error, so replay, expiry, and lock races remain distinguishable without a
// second read that could observe yet another state.
func (s *Service) auditMFAFailureRejection(
	ctx context.Context,
	challenge *domain.MFAChallenge,
	failureErr error,
) error {
	if challenge == nil {
		return nil
	}
	switch {
	case errors.Is(failureErr, domain.ErrMFAAccountLocked):
		return s.recordMFAChallengeLockedObservation(
			ctx,
			challenge.ParentAccountID,
			challenge.FailedAttempts,
		)
	case errors.Is(failureErr, domain.ErrMFAChallengeConsumed):
		return s.recordMFAEvent(
			ctx,
			challenge.ParentAccountID,
			challenge.ParentAccountID,
			"admin.mfa.replayed",
			map[string]any{"outcome": "challenge_replayed"},
		)
	case errors.Is(failureErr, domain.ErrMFAChallengeExpired):
		return s.recordMFAEvent(
			ctx,
			challenge.ParentAccountID,
			challenge.ParentAccountID,
			"admin.mfa.expired",
			map[string]any{"outcome": "challenge_expired"},
		)
	default:
		return nil
	}
}

func (s *Service) issueAccessToken(
	parentAccountID string,
	sessionID string,
) (string, error) {
	now := s.timeSource.Now().UTC()
	return s.tokenIssuer.Issue(security.TokenClaims{
		Subject:   parentAccountID,
		SessionID: sessionID,
		TokenID:   uuid.NewString(),
		IssuedAt:  now,
		ExpiresAt: now.Add(s.accessTTL),
	})
}

func validateRegistration(input domain.RegisterInput) error {
	if !isValidPhone(input.Phone) {
		return domain.ErrInvalidPhone
	}
	if len(input.Password) < 8 || len(input.Password) > 128 ||
		!containsLetterAndNumber(input.Password) {
		return domain.ErrWeakPassword
	}
	if len([]rune(input.GuardianFamilyName)) > 40 {
		return domain.ErrInvalidGuardianName
	}
	if len([]rune(input.ChildNickname)) > 40 {
		return domain.ErrInvalidChildNickname
	}
	if input.ChildBirthday != "" {
		birthday, err := time.Parse("2006-01-02", input.ChildBirthday)
		if err != nil || birthday.After(time.Now().UTC()) {
			return domain.ErrInvalidChildBirthday
		}
	}
	if input.GuardianConsentVersion == "" {
		return domain.ErrGuardianConsent
	}
	return nil
}

func guardianDisplayName(input domain.RegisterInput) string {
	familyName := strings.TrimSpace(input.GuardianFamilyName)
	if familyName != "" {
		return familyName + "家长"
	}
	if strings.TrimSpace(input.ChildNickname) != "" {
		return strings.TrimSpace(input.ChildNickname) + "家长"
	}
	return "家长"
}

func (s *Service) resolveParentAccountByIdentifier(
	ctx context.Context,
	identifier string,
) (*domain.ParentAccount, error) {
	if isValidPhone(identifier) {
		return s.repository.GetParentAccountByPhone(ctx, normalizePhone(identifier))
	}
	if isValidEmail(identifier) {
		policy, err := s.registrationPolicy(ctx)
		if err != nil {
			return nil, err
		}
		if !policy.EmailLoginEnabled {
			return nil, domain.ErrEmailLoginDisabled
		}
		return s.repository.GetParentAccountByEmail(ctx, normalizeEmail(identifier))
	}
	return nil, domain.ErrInvalidCredentials
}

// registrationPolicy fails closed when the policy document cannot be read.
// Registration and email sign-in are security-sensitive switches, so a
// database outage must not silently re-enable either path.
func (s *Service) registrationPolicy(
	ctx context.Context,
) (*operationsdomain.RuntimePolicy, error) {
	if s.policyReader == nil {
		return &operationsdomain.RuntimePolicy{
			RegistrationEnabled:       true,
			PhoneVerificationRequired: true,
			EmailLoginEnabled:         true,
		}, nil
	}
	policy, err := s.policyReader.RuntimePolicy(ctx)
	if err != nil {
		return nil, err
	}
	if policy == nil {
		return nil, domain.ErrRegistrationDisabled
	}
	return policy, nil
}

func (s *Service) verifyPhoneCode(
	ctx context.Context,
	phone string,
	purpose string,
	code string,
) error {
	if s.phoneVerifier == nil {
		return domain.ErrPhoneVerification
	}
	return s.phoneVerifier.VerifyCode(ctx, normalizePhone(phone), purpose, strings.TrimSpace(code))
}

func isValidPhone(value string) bool {
	normalized := normalizePhone(value)
	if len(normalized) != 11 || normalized[0] != '1' {
		return false
	}
	for _, character := range normalized {
		if character < '0' || character > '9' {
			return false
		}
	}
	return true
}

func normalizePhone(value string) string {
	replacer := strings.NewReplacer(" ", "", "-", "", "(", "", ")", "")
	return replacer.Replace(strings.TrimSpace(value))
}

func isValidEmail(value string) bool {
	value = strings.TrimSpace(value)
	return value != "" && strings.Contains(value, "@") &&
		!strings.HasPrefix(value, "@") && !strings.HasSuffix(value, "@")
}

// LocalPhoneVerifier keeps the SMS flow usable before a provider is configured.
// It stores code hashes and accepts the development bypass code `000000`.
type LocalPhoneVerifier struct {
	repository repository.Repository
	timeSource clock.Clock
}

// NewLocalPhoneVerifier creates the development SMS verifier.
func NewLocalPhoneVerifier(
	repository repository.Repository,
	timeSource clock.Clock,
) *LocalPhoneVerifier {
	if timeSource == nil {
		timeSource = clock.SystemClock{}
	}
	return &LocalPhoneVerifier{
		repository: repository,
		timeSource: timeSource,
	}
}

// SendVerificationCode records a code request for the enabled local bypass.
func (v *LocalPhoneVerifier) SendVerificationCode(
	ctx context.Context,
	phone string,
	purpose string,
) error {
	if !isValidPhone(phone) {
		return domain.ErrInvalidPhone
	}
	now := v.timeSource.Now().UTC()
	return v.repository.CreatePhoneVerificationCode(ctx, &domain.PhoneVerificationCode{
		ID:        uuid.NewString(),
		Phone:     normalizePhone(phone),
		Purpose:   purpose,
		CodeHash:  hashRefreshToken("000000"),
		ExpiresAt: now.Add(phoneVerificationCodeTTL),
		CreatedAt: now,
	})
}

// VerifyCode accepts only the explicit development code. An empty value is an
// invalid verification attempt and must never bypass the SMS gate.
func (v *LocalPhoneVerifier) VerifyCode(
	ctx context.Context,
	phone string,
	purpose string,
	code string,
) error {
	if strings.TrimSpace(code) != "000000" {
		return domain.ErrInvalidVerification
	}
	return nil
}

func containsLetterAndNumber(value string) bool {
	hasLetter := false
	hasNumber := false
	for _, character := range value {
		switch {
		case character >= 'a' && character <= 'z':
			hasLetter = true
		case character >= 'A' && character <= 'Z':
			hasLetter = true
		case character >= '0' && character <= '9':
			hasNumber = true
		}
	}
	return hasLetter && hasNumber
}

func normalizeEmail(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}

func randomToken() (string, error) {
	bytes := make([]byte, 48)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	return hex.EncodeToString(bytes), nil
}

func hashRefreshToken(value string) string {
	hash := sha256.Sum256([]byte(value))
	return hex.EncodeToString(hash[:])
}

// AESGCMTOTPCipher encrypts administrator TOTP secrets with an authenticated
// key derived from the configured credential material.
type AESGCMTOTPCipher struct {
	aead cipher.AEAD
}

// NewAESGCMTOTPCipher derives a 256-bit key from secret material.
func NewAESGCMTOTPCipher(secret string) (*AESGCMTOTPCipher, error) {
	if len(strings.TrimSpace(secret)) < 32 {
		return nil, errors.New("MFA credential key must contain at least 32 characters")
	}
	derivedKey := sha256.Sum256([]byte(secret))
	block, err := aes.NewCipher(derivedKey[:])
	if err != nil {
		return nil, fmt.Errorf("create MFA cipher: %w", err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("create MFA AEAD: %w", err)
	}
	return &AESGCMTOTPCipher{aead: aead}, nil
}

// Encrypt returns ciphertext and a unique nonce.
func (c *AESGCMTOTPCipher) Encrypt(plaintext []byte) ([]byte, []byte, error) {
	if c == nil || c.aead == nil {
		return nil, nil, errors.New("MFA cipher is not configured")
	}
	nonce := make([]byte, c.aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, nil, fmt.Errorf("generate MFA nonce: %w", err)
	}
	return c.aead.Seal(nil, nonce, plaintext, nil), nonce, nil
}

// Decrypt authenticates and decrypts stored TOTP secret material.
func (c *AESGCMTOTPCipher) Decrypt(ciphertext []byte, nonce []byte) ([]byte, error) {
	if c == nil || c.aead == nil {
		return nil, errors.New("MFA cipher is not configured")
	}
	if len(nonce) != c.aead.NonceSize() {
		return nil, errors.New("MFA nonce has an invalid length")
	}
	plaintext, err := c.aead.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return nil, errors.New("MFA credential authentication failed")
	}
	return plaintext, nil
}
