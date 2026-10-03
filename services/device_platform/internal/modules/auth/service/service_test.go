package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/auth/domain"
	"github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/auth/repository"
	childdomain "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/child/domain"
	operationsdomain "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/operations/domain"
	"github.com/TissyBoxC/sprout-platform/services/device_platform/internal/platform/security"
	"golang.org/x/crypto/bcrypt"
)

func TestRegisterRejectsPhoneVerificationWhenProviderIsNotConfigured(t *testing.T) {
	service, err := New(Options{
		Repository:  &memoryRepository{},
		TokenIssuer: mustTokenIssuer(t),
		AccessTTL:   time.Minute,
		RefreshTTL:  time.Hour,
	})
	if err != nil {
		t.Fatalf("create authentication service: %v", err)
	}

	_, _, _, err = service.Register(context.Background(), domain.RegisterInput{
		Phone:                  "13800138000",
		PhoneVerificationCode:  "000000",
		Password:               "sprout123",
		GuardianConsentVersion: defaultConsent,
	})

	if !errors.Is(err, domain.ErrPhoneVerification) {
		t.Fatalf("expected phone verification to be unavailable, got %v", err)
	}
}

func TestRegisterAllowsOptionalGuardianAndChildFields(t *testing.T) {
	repository := &memoryRepository{}
	service, err := New(Options{
		Repository:    repository,
		TokenIssuer:   mustTokenIssuer(t),
		PhoneVerifier: noOpPhoneVerifier{},
		AccessTTL:     time.Minute,
		RefreshTTL:    time.Hour,
	})
	if err != nil {
		t.Fatalf("create authentication service: %v", err)
	}

	account, pair, _, err := service.Register(context.Background(), domain.RegisterInput{
		Phone:                  "13800138000",
		PhoneVerificationCode:  "000000",
		Password:               "sprout123",
		GuardianConsentVersion: defaultConsent,
	})
	if err != nil {
		t.Fatalf("register optional profile: %v", err)
	}
	if account.DisplayName != "家长" {
		t.Fatalf("expected a generic display name, got %q", account.DisplayName)
	}
	if pair == nil || pair.AccessToken == "" || pair.RefreshToken == "" {
		t.Fatal("expected a complete session token pair")
	}
}

func TestRegisterCreatesChildProfileWhenNicknameIsProvided(t *testing.T) {
	repository := &memoryRepository{}
	childProvisioner := &memoryChildProvisioner{}
	service, err := New(Options{
		Repository:       repository,
		TokenIssuer:      mustTokenIssuer(t),
		PhoneVerifier:    noOpPhoneVerifier{},
		ChildProvisioner: childProvisioner,
		AccessTTL:        time.Minute,
		RefreshTTL:       time.Hour,
	})
	if err != nil {
		t.Fatalf("create authentication service: %v", err)
	}

	_, _, _, err = service.Register(context.Background(), domain.RegisterInput{
		Phone:                  "13800138000",
		PhoneVerificationCode:  "000000",
		Password:               "sprout123",
		ChildNickname:          "小芽",
		ChildBirthday:          "2022-08-01",
		GuardianConsentVersion: defaultConsent,
	})
	if err != nil {
		t.Fatalf("register with child profile: %v", err)
	}
	if childProvisioner.createdProfiles != 1 {
		t.Fatalf("expected one child profile, got %d", childProvisioner.createdProfiles)
	}
}

func TestRegisterRollsBackParentWhenChildProfileFails(t *testing.T) {
	repository := &memoryRepository{}
	service, err := New(Options{
		Repository:       repository,
		TokenIssuer:      mustTokenIssuer(t),
		PhoneVerifier:    noOpPhoneVerifier{},
		ChildProvisioner: &memoryChildProvisioner{fail: true},
		AccessTTL:        time.Minute,
		RefreshTTL:       time.Hour,
	})
	if err != nil {
		t.Fatalf("create authentication service: %v", err)
	}

	_, _, _, err = service.Register(context.Background(), domain.RegisterInput{
		Phone:                  "13800138000",
		PhoneVerificationCode:  "000000",
		Password:               "sprout123",
		ChildNickname:          "小芽",
		GuardianConsentVersion: defaultConsent,
	})
	if err == nil {
		t.Fatal("expected child profile failure")
	}
	if repository.account != nil {
		t.Fatal("expected the parent account to be rolled back")
	}
}

func TestRegisterRejectsInvalidChildBirthday(t *testing.T) {
	service, err := New(Options{
		Repository:    &memoryRepository{},
		TokenIssuer:   mustTokenIssuer(t),
		PhoneVerifier: noOpPhoneVerifier{},
		AccessTTL:     time.Minute,
		RefreshTTL:    time.Hour,
	})
	if err != nil {
		t.Fatalf("create authentication service: %v", err)
	}

	_, _, _, err = service.Register(context.Background(), domain.RegisterInput{
		Phone:                  "13800138000",
		PhoneVerificationCode:  "000000",
		Password:               "sprout123",
		ChildBirthday:          "2030-01-01",
		GuardianConsentVersion: defaultConsent,
	})

	if !errors.Is(err, domain.ErrInvalidChildBirthday) {
		t.Fatalf("expected invalid child birthday, got %v", err)
	}
}

func TestRegisterHonoursOperationsRegistrationPolicy(t *testing.T) {
	service, err := New(Options{
		Repository:    &memoryRepository{},
		TokenIssuer:   mustTokenIssuer(t),
		PhoneVerifier: noOpPhoneVerifier{},
		PolicyReader: staticPolicyReader{policy: operationsdomain.RuntimePolicy{
			RegistrationEnabled:       false,
			PhoneVerificationRequired: false,
			EmailLoginEnabled:         false,
		}},
		AccessTTL:  time.Minute,
		RefreshTTL: time.Hour,
	})
	if err != nil {
		t.Fatalf("create authentication service: %v", err)
	}

	_, _, _, err = service.Register(context.Background(), domain.RegisterInput{
		Phone:                  "13800138000",
		Password:               "sprout123",
		GuardianConsentVersion: defaultConsent,
	})
	if !errors.Is(err, domain.ErrRegistrationDisabled) {
		t.Fatalf("expected registration disabled, got %v", err)
	}
}

func TestRegisterCanSkipPhoneVerificationWhenPolicyAllowsIt(t *testing.T) {
	service, err := New(Options{
		Repository:  &memoryRepository{},
		TokenIssuer: mustTokenIssuer(t),
		PolicyReader: staticPolicyReader{policy: operationsdomain.RuntimePolicy{
			RegistrationEnabled:       true,
			PhoneVerificationRequired: false,
			EmailLoginEnabled:         true,
		}},
		AccessTTL:  time.Minute,
		RefreshTTL: time.Hour,
	})
	if err != nil {
		t.Fatalf("create authentication service: %v", err)
	}

	_, _, _, err = service.Register(context.Background(), domain.RegisterInput{
		Phone:                  "13800138000",
		Password:               "sprout123",
		GuardianConsentVersion: defaultConsent,
	})
	if err != nil {
		t.Fatalf("register without phone verification: %v", err)
	}
}

func TestCreateParentSkipsSMSButKeepsGuardianRoleAndPasswordRules(t *testing.T) {
	repository := &memoryRepository{}
	service, err := New(Options{
		Repository:  repository,
		TokenIssuer: mustTokenIssuer(t),
		AccessTTL:   time.Minute,
		RefreshTTL:  time.Hour,
	})
	if err != nil {
		t.Fatalf("create authentication service: %v", err)
	}

	account, _, err := service.CreateParent(context.Background(), domain.RegisterInput{
		Phone:                  "13800138000",
		Password:               "sprout123",
		GuardianFamilyName:     "林",
		ChildNickname:          "小芽",
		GuardianConsentVersion: defaultConsent,
	})
	if err != nil {
		t.Fatalf("create parent through maintenance flow: %v", err)
	}
	if account.Role != domain.RoleParent {
		t.Fatalf("expected parent role, got %q", account.Role)
	}
	if account.PhoneVerifiedAt == nil || account.GuardianConsentedAt.IsZero() {
		t.Fatal("maintenance-created parent must record verification and consent")
	}

	_, _, err = service.CreateParent(context.Background(), domain.RegisterInput{
		Phone:    "13800138001",
		Password: "weak",
	})
	if !errors.Is(err, domain.ErrWeakPassword) {
		t.Fatalf("expected weak password rejection, got %v", err)
	}
}

func TestBindEmailAcceptsAnEmailAsAnAlternateLoginIdentifier(t *testing.T) {
	account := &domain.ParentAccount{
		ID:           "parent-001",
		Phone:        "13800138000",
		PasswordHash: mustPasswordHash(t, "sprout123"),
		Status:       accountStatusActive,
	}
	repository := &memoryRepository{
		account: account,
	}
	service, err := New(Options{
		Repository:  repository,
		TokenIssuer: mustTokenIssuer(t),
		AccessTTL:   time.Minute,
		RefreshTTL:  time.Hour,
	})
	if err != nil {
		t.Fatalf("create authentication service: %v", err)
	}

	updated, err := service.BindEmail(context.Background(), account.ID, domain.BindEmailInput{
		Email:    "Guardian@Example.com",
		Password: "sprout123",
	})
	if err != nil {
		t.Fatalf("bind recovery email: %v", err)
	}
	if updated.Email != "guardian@example.com" {
		t.Fatalf("expected a normalized email, got %q", updated.Email)
	}
	if updated.EmailVerifiedAt == nil {
		t.Fatal("expected the bound email to be marked verified")
	}

	_, _, _, err = service.Login(context.Background(), domain.LoginInput{
		Identifier: "guardian@example.com",
		Password:   "sprout123",
	})
	if err != nil {
		t.Fatalf("log in with bound email: %v", err)
	}
}

type memoryRepository struct {
	account             *domain.ParentAccount
	passwordHashUpdates int
}

func (r *memoryRepository) CreateParentAccount(
	_ context.Context,
	account *domain.ParentAccount,
) error {
	r.account = account
	return nil
}

func (r *memoryRepository) DeleteParentAccount(
	_ context.Context,
	accountID string,
) error {
	if r.account != nil && r.account.ID == accountID {
		r.account = nil
	}
	return nil
}

type memoryChildProvisioner struct {
	createdProfiles int
	fail            bool
}

func (p *memoryChildProvisioner) SyncRegistrationProfile(
	_ context.Context,
	_ string,
	_ string,
	_ string,
	_ string,
) (*childdomain.Child, error) {
	if p.fail {
		return nil, errors.New("child profile failed")
	}
	p.createdProfiles++
	return &childdomain.Child{}, nil
}

func (r *memoryRepository) GetParentAccountByEmail(
	_ context.Context,
	email string,
) (*domain.ParentAccount, error) {
	if r.account != nil && r.account.Email == email {
		return r.account, nil
	}
	return nil, domain.ErrAccountNotFound
}

func (r *memoryRepository) GetParentAccountByPhone(
	_ context.Context,
	phone string,
) (*domain.ParentAccount, error) {
	if r.account != nil && r.account.Phone == phone {
		return r.account, nil
	}
	return nil, domain.ErrAccountNotFound
}

func (r *memoryRepository) GetParentAccountByID(
	_ context.Context,
	accountID string,
) (*domain.ParentAccount, error) {
	if r.account != nil && r.account.ID == accountID {
		return r.account, nil
	}
	return nil, domain.ErrAccountNotFound
}

func (r *memoryRepository) UpdatePasswordHash(
	_ context.Context,
	accountID string,
	passwordHash string,
) error {
	if r.account == nil || r.account.ID != accountID {
		return domain.ErrAccountNotFound
	}
	r.passwordHashUpdates++
	r.account.PasswordHash = passwordHash
	return nil
}

func (r *memoryRepository) UpdateEmail(
	_ context.Context,
	accountID string,
	email string,
) error {
	if r.account == nil || r.account.ID != accountID {
		return domain.ErrAccountNotFound
	}
	r.account.Email = email
	now := time.Now().UTC()
	r.account.EmailVerifiedAt = &now
	return nil
}

func (r *memoryRepository) UpdateProfile(
	_ context.Context,
	account *domain.ParentAccount,
) error {
	if r.account == nil || r.account.ID != account.ID {
		return domain.ErrAccountNotFound
	}
	r.account.DisplayName = account.DisplayName
	r.account.GuardianFamilyName = account.GuardianFamilyName
	r.account.ChildNickname = account.ChildNickname
	r.account.ChildBirthday = account.ChildBirthday
	r.account.UpdatedAt = account.UpdatedAt
	return nil
}

func (r *memoryRepository) UpdateLastLogin(
	_ context.Context,
	_ string,
) error {
	return nil
}

func (r *memoryRepository) CreateSession(
	_ context.Context,
	_ *domain.Session,
) error {
	return nil
}

func (r *memoryRepository) GetSessionByRefreshTokenHash(
	_ context.Context,
	_ string,
) (*domain.Session, error) {
	return nil, domain.ErrSessionNotFound
}

func (r *memoryRepository) RotateSession(
	_ context.Context,
	_ string,
	_ string,
	_ time.Time,
) error {
	return nil
}

func (r *memoryRepository) RevokeSession(_ context.Context, _ string) error {
	return nil
}

func (r *memoryRepository) RevokeAllSessions(_ context.Context, _ string) error {
	return nil
}

func (r *memoryRepository) CreateMFAChallenge(
	_ context.Context,
	_ *domain.MFAChallenge,
) error {
	return nil
}

func (r *memoryRepository) GetMFAChallengeByHash(
	_ context.Context,
	_ string,
) (*domain.MFAChallenge, error) {
	return nil, domain.ErrMFAChallengeNotFound
}

func (r *memoryRepository) ConsumeMFAChallenge(_ context.Context, _ string) error {
	return nil
}

func (r *memoryRepository) GetTOTPCredential(
	_ context.Context,
	_ string,
) (*domain.TOTPCredential, error) {
	return nil, domain.ErrMFANotConfigured
}

func (r *memoryRepository) UpsertTOTPCredential(
	_ context.Context,
	_ *domain.TOTPCredential,
) error {
	return nil
}

func (r *memoryRepository) CreatePhoneVerificationCode(
	_ context.Context,
	_ *domain.PhoneVerificationCode,
) error {
	return nil
}

func (r *memoryRepository) ConsumePhoneVerificationCode(
	_ context.Context,
	_ string,
	_ string,
	_ string,
) error {
	return nil
}

var _ repository.Repository = (*memoryRepository)(nil)
var _ ChildAccountProvisioner = (*memoryChildProvisioner)(nil)

type noOpPhoneVerifier struct{}

type staticPolicyReader struct {
	policy operationsdomain.RuntimePolicy
}

func (r staticPolicyReader) RuntimePolicy(
	_ context.Context,
) (*operationsdomain.RuntimePolicy, error) {
	copy := r.policy
	copy.DefaultModels = append([]string(nil), r.policy.DefaultModels...)
	return &copy, nil
}

func (noOpPhoneVerifier) SendVerificationCode(
	_ context.Context,
	_ string,
	_ string,
) error {
	return nil
}

func (noOpPhoneVerifier) VerifyCode(
	_ context.Context,
	_ string,
	_ string,
	_ string,
) error {
	return nil
}

func mustTokenIssuer(t *testing.T) security.TokenIssuer {
	t.Helper()
	issuer, err := security.NewHMACTokenIssuer(
		"test-access-token-secret-at-least-32-characters",
	)
	if err != nil {
		t.Fatalf("create token issuer: %v", err)
	}
	return issuer
}

func mustPasswordHash(t *testing.T, password string) string {
	t.Helper()
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.MinCost)
	if err != nil {
		t.Fatalf("hash test password: %v", err)
	}
	return string(hash)
}
