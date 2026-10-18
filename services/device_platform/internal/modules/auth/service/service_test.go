package service

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/auth/domain"
	"github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/auth/repository"
	childdomain "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/child/domain"
	familydomain "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/family/domain"
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

func TestRegisterBootstrapsFamilyOwner(t *testing.T) {
	repository := &memoryRepository{}
	ownerProvisioner := &memoryOwnerProvisioner{}
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
	service.SetFamilyOwnerProvisioner(ownerProvisioner)
	if _, _, _, err := service.Register(context.Background(), domain.RegisterInput{
		Phone:                  "13800138000",
		PhoneVerificationCode:  "000000",
		Password:               "sprout123",
		GuardianConsentVersion: defaultConsent,
	}); err != nil {
		t.Fatalf("register: %v", err)
	}
	if ownerProvisioner.bootstrapped != 1 {
		t.Fatalf("expected one owner bootstrap, got %d", ownerProvisioner.bootstrapped)
	}
}

func TestRegisterRollsBackParentWhenOwnerBootstrapFails(t *testing.T) {
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
	service.SetFamilyOwnerProvisioner(&memoryOwnerProvisioner{fail: true})
	if _, _, _, err := service.Register(context.Background(), domain.RegisterInput{
		Phone:                  "13800138000",
		PhoneVerificationCode:  "000000",
		Password:               "sprout123",
		GuardianConsentVersion: defaultConsent,
	}); err == nil {
		t.Fatal("registration must fail when the owner cannot be bootstrapped")
	}
	if repository.account != nil {
		t.Fatal("a failed owner bootstrap must delete the parent account")
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

func TestLocalPhoneVerifierRejectsEmptyCode(t *testing.T) {
	verifier := NewLocalPhoneVerifier(&memoryRepository{}, nil)
	err := verifier.VerifyCode(
		context.Background(),
		"13800000000",
		"register",
		" ",
	)
	if !errors.Is(err, domain.ErrInvalidVerification) {
		t.Fatalf("empty code error = %v, want ErrInvalidVerification", err)
	}
}

func TestVerifyAccessTokenRejectsRevokedSession(t *testing.T) {
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
	account, pair, _, err := service.Register(
		context.Background(),
		domain.RegisterInput{
			Phone:                  "13800138000",
			PhoneVerificationCode:  "000000",
			Password:               "sprout123",
			GuardianConsentVersion: defaultConsent,
		},
	)
	if err != nil {
		t.Fatalf("register: %v", err)
	}
	if accountID, verifyErr := service.VerifyAccessToken(
		context.Background(),
		pair.AccessToken,
	); verifyErr != nil || accountID != account.ID {
		t.Fatalf("active token verification = %q, %v", accountID, verifyErr)
	}

	var sessionID string
	for id := range repository.sessions {
		sessionID = id
		break
	}
	if sessionID == "" {
		t.Fatal("registration did not persist a session")
	}
	if err := repository.RevokeSession(context.Background(), sessionID); err != nil {
		t.Fatalf("revoke session: %v", err)
	}
	if _, err := service.VerifyAccessToken(
		context.Background(),
		pair.AccessToken,
	); !errors.Is(err, domain.ErrSessionNotFound) {
		t.Fatalf("revoked access token error = %v, want ErrSessionNotFound", err)
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

func TestLoginRejectsAdministratorBeforeIssuingSession(t *testing.T) {
	repository := &memoryRepository{
		account: &domain.ParentAccount{
			ID:           "admin-001",
			Email:        "admin@sprout.local",
			Role:         domain.RoleAdmin,
			Status:       accountStatusActive,
			PasswordHash: mustPasswordHash(t, "sprout123"),
		},
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

	account, pair, summary, err := service.Login(context.Background(), domain.LoginInput{
		Identifier: "admin@sprout.local",
		Password:   "sprout123",
	})
	if !errors.Is(err, domain.ErrMFARequired) {
		t.Fatalf("expected administrator MFA requirement, got %v", err)
	}
	if account != nil || pair != nil || summary != nil {
		t.Fatal("public login must not return an administrator account or session")
	}
	if len(repository.sessions) != 0 {
		t.Fatalf("administrator bypass issued %d sessions", len(repository.sessions))
	}
}

func TestLoginRejectsAdministratorWithoutIssuingSession(t *testing.T) {
	lockedUntil := time.Now().UTC().Add(10 * time.Minute)
	repository := &memoryRepository{
		account: &domain.ParentAccount{
			ID:           "admin-002",
			Email:        "admin@sprout.local",
			Role:         domain.RoleAdmin,
			Status:       accountStatusActive,
			PasswordHash: mustPasswordHash(t, "sprout123"),
		},
		mfaAccountFailure: &domain.MFAAccountFailureState{
			ParentAccountID: "admin-002",
			FailedAttempts:  3,
			LockedUntil:     &lockedUntil,
		},
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

	_, _, _, err = service.Login(context.Background(), domain.LoginInput{
		Identifier: "admin@sprout.local",
		Password:   "sprout123",
	})
	if !errors.Is(err, domain.ErrMFARequired) {
		t.Fatalf("expected administrator MFA requirement, got %v", err)
	}
	if repository.passwordHashUpdates != 0 {
		t.Fatal("login must not rewrite the password hash")
	}
	if len(repository.sessions) != 0 {
		t.Fatalf("administrator bypass issued %d sessions", len(repository.sessions))
	}
	if repository.mfaAccountFailure == nil ||
		repository.mfaAccountFailure.FailedAttempts != 3 ||
		repository.mfaAccountFailure.LockedUntil == nil {
		t.Fatalf("administrator bypass changed MFA failure state: %+v",
			repository.mfaAccountFailure)
	}
}

type memoryRepository struct {
	account             *domain.ParentAccount
	passwordHashUpdates int
	sessions            map[string]*domain.Session
	revokedSessions     map[string]bool
	challenges          map[string]*domain.MFAChallenge
	totpCredential      *domain.TOTPCredential
	mfaAccountFailure   *domain.MFAAccountFailureState
	mu                  sync.Mutex
}

func (r *memoryRepository) CreateParentAccount(
	_ context.Context,
	account *domain.ParentAccount,
) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.account = account
	return nil
}

func (r *memoryRepository) DeleteParentAccount(
	_ context.Context,
	accountID string,
) error {
	r.mu.Lock()
	defer r.mu.Unlock()
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

// memoryOwnerProvisioner records owner bootstrapping during registration.
type memoryOwnerProvisioner struct {
	bootstrapped int
	fail         bool
}

func (p *memoryOwnerProvisioner) BootstrapOwner(
	_ context.Context,
	familyID string,
	userID string,
) (familydomain.Member, error) {
	if p.fail {
		return familydomain.Member{}, errors.New("owner bootstrap failed")
	}
	if familyID != userID {
		return familydomain.Member{}, errors.New("family must equal owner account")
	}
	p.bootstrapped++
	return familydomain.Member{FamilyID: familyID, UserID: userID, Role: familydomain.RoleOwner}, nil
}

func (r *memoryRepository) GetParentAccountByEmail(
	_ context.Context,
	email string,
) (*domain.ParentAccount, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.account != nil && r.account.Email == email {
		copy := *r.account
		return &copy, nil
	}
	return nil, domain.ErrAccountNotFound
}

func (r *memoryRepository) GetParentAccountByPhone(
	_ context.Context,
	phone string,
) (*domain.ParentAccount, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.account != nil && r.account.Phone == phone {
		copy := *r.account
		return &copy, nil
	}
	return nil, domain.ErrAccountNotFound
}

func (r *memoryRepository) GetParentAccountByID(
	_ context.Context,
	accountID string,
) (*domain.ParentAccount, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.account != nil && r.account.ID == accountID {
		copy := *r.account
		return &copy, nil
	}
	return nil, domain.ErrAccountNotFound
}

func (r *memoryRepository) UpdatePasswordHash(
	_ context.Context,
	accountID string,
	passwordHash string,
) error {
	r.mu.Lock()
	defer r.mu.Unlock()
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
	r.mu.Lock()
	defer r.mu.Unlock()
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
	r.mu.Lock()
	defer r.mu.Unlock()
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
	r.mu.Lock()
	defer r.mu.Unlock()
	return nil
}

func (r *memoryRepository) ListAdminAccounts(
	_ context.Context,
) ([]domain.ParentAccount, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.account == nil || r.account.Role != domain.RoleAdmin {
		return []domain.ParentAccount{}, nil
	}
	return []domain.ParentAccount{*r.account}, nil
}

func (r *memoryRepository) UpdateAdminPermissions(
	_ context.Context,
	accountID string,
	permissions []string,
) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.account == nil || r.account.ID != accountID {
		return domain.ErrAccountNotFound
	}
	r.account.AdminPermissions = permissions
	return nil
}

func (r *memoryRepository) CreateSession(
	_ context.Context,
	session *domain.Session,
) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.sessions == nil {
		r.sessions = make(map[string]*domain.Session)
	}
	copy := *session
	r.sessions[session.ID] = &copy
	return nil
}

func (r *memoryRepository) GetActiveSessionByID(
	_ context.Context,
	sessionID string,
	parentAccountID string,
) (*domain.Session, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	session, ok := r.sessions[sessionID]
	if !ok || r.revokedSessions[sessionID] ||
		session.ParentAccountID != parentAccountID ||
		!session.ExpiresAt.After(time.Now().UTC()) {
		return nil, domain.ErrSessionNotFound
	}
	copy := *session
	return &copy, nil
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
	_ string,
	_ time.Time,
) error {
	return nil
}

func (r *memoryRepository) RevokeSession(_ context.Context, sessionID string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.revokedSessions == nil {
		r.revokedSessions = make(map[string]bool)
	}
	r.revokedSessions[sessionID] = true
	return nil
}

func (r *memoryRepository) RevokeAllSessions(_ context.Context, _ string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	return nil
}

func (r *memoryRepository) CreateMFAChallenge(
	_ context.Context,
	challenge *domain.MFAChallenge,
) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.challenges == nil {
		r.challenges = make(map[string]*domain.MFAChallenge)
	}
	copy := *challenge
	r.challenges[challenge.ChallengeHash] = &copy
	return nil
}

func (r *memoryRepository) DeleteMFAChallenge(
	_ context.Context,
	challengeID string,
) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	for hash, challenge := range r.challenges {
		if challenge.ID == challengeID &&
			challenge.ConsumedAt == nil &&
			challenge.LockedAt == nil {
			delete(r.challenges, hash)
		}
	}
	return nil
}

func (r *memoryRepository) GetMFAChallengeByHash(
	_ context.Context,
	challengeHash string,
) (*domain.MFAChallenge, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	challenge, ok := r.challenges[challengeHash]
	if !ok {
		return nil, domain.ErrMFAChallengeNotFound
	}
	copy := *challenge
	return &copy, nil
}

func (r *memoryRepository) GetMFAAccountFailureState(
	_ context.Context,
	parentAccountID string,
) (*domain.MFAAccountFailureState, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.mfaAccountFailure == nil {
		return &domain.MFAAccountFailureState{
			ParentAccountID: parentAccountID,
		}, nil
	}
	copy := *r.mfaAccountFailure
	return &copy, nil
}

func (r *memoryRepository) ClearExpiredMFAAccountLock(
	_ context.Context,
	parentAccountID string,
	now time.Time,
) (*domain.MFAAccountFailureState, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.mfaAccountFailure == nil {
		return &domain.MFAAccountFailureState{
			ParentAccountID: parentAccountID,
		}, nil
	}
	if r.mfaAccountFailure.LockedUntil != nil &&
		!r.mfaAccountFailure.LockedUntil.After(now) {
		recovered := true
		r.mfaAccountFailure.FailedAttempts = 0
		r.mfaAccountFailure.LockedUntil = nil
		r.mfaAccountFailure.LastFailedAt = nil
		r.mfaAccountFailure.UpdatedAt = now
		copy := *r.mfaAccountFailure
		copy.Recovered = recovered
		return &copy, nil
	}
	copy := *r.mfaAccountFailure
	return &copy, nil
}

func (r *memoryRepository) ConsumeMFAChallenge(
	_ context.Context,
	challengeID string,
	now time.Time,
) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.mfaAccountFailure != nil &&
		r.mfaAccountFailure.LockedUntil != nil &&
		r.mfaAccountFailure.LockedUntil.After(now) {
		return domain.ErrMFAAccountLocked
	}
	for _, challenge := range r.challenges {
		if challenge.ID != challengeID {
			continue
		}
		if challenge.ConsumedAt != nil {
			return domain.ErrMFAChallengeConsumed
		}
		if !challenge.ExpiresAt.After(now) {
			return domain.ErrMFAChallengeExpired
		}
		if challenge.LockedAt != nil {
			return domain.ErrMFAChallengeLocked
		}
		challenge.ConsumedAt = &now
		r.mfaAccountFailure = nil
		return nil
	}
	return domain.ErrMFAChallengeNotFound
}

func (r *memoryRepository) RecordMFAChallengeFailure(
	_ context.Context,
	challengeID string,
	maxAttempts int,
	lockDuration time.Duration,
	now time.Time,
) (
	*domain.MFAChallenge,
	*domain.MFAAccountFailureState,
	repository.MFAFailureOutcome,
	error,
) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, challenge := range r.challenges {
		if challenge.ID != challengeID {
			continue
		}
		if challenge.ConsumedAt != nil {
			copy := *challenge
			return &copy, r.copyMFAAccountFailureLocked(challenge.ParentAccountID),
				"", domain.ErrMFAChallengeConsumed
		}
		if !challenge.ExpiresAt.After(now) {
			copy := *challenge
			return &copy, r.copyMFAAccountFailureLocked(challenge.ParentAccountID),
				"", domain.ErrMFAChallengeExpired
		}
		if r.mfaAccountFailure == nil {
			r.mfaAccountFailure = &domain.MFAAccountFailureState{
				ParentAccountID: challenge.ParentAccountID,
				UpdatedAt:       now,
			}
		}
		if r.mfaAccountFailure.LockedUntil != nil &&
			r.mfaAccountFailure.LockedUntil.After(now) {
			copy := challengeCopy(challenge)
			return &copy,
				r.copyMFAAccountFailureLocked(challenge.ParentAccountID),
				repository.MFAFailureAlreadyLocked,
				domain.ErrMFAAccountLocked
		}
		if challenge.LockedAt != nil {
			copy := *challenge
			return &copy, r.copyMFAAccountFailureLocked(challenge.ParentAccountID),
				repository.MFAFailureAlreadyLocked, domain.ErrMFAChallengeLocked
		}
		outcome := repository.MFAFailureIncremented
		if r.mfaAccountFailure.LockedUntil != nil {
			r.mfaAccountFailure.FailedAttempts = 0
			r.mfaAccountFailure.LockedUntil = nil
			r.mfaAccountFailure.LastFailedAt = nil
			outcome = repository.MFAFailureLockExpired
		}
		r.mfaAccountFailure.FailedAttempts++
		r.mfaAccountFailure.LastFailedAt = &now
		r.mfaAccountFailure.UpdatedAt = now
		if r.mfaAccountFailure.FailedAttempts >= maxAttempts {
			lockedAt := now
			challenge.LockedAt = &lockedAt
			lockedUntil := now.Add(lockDuration)
			r.mfaAccountFailure.LockedUntil = &lockedUntil
			outcome = repository.MFAFailureLockedNow
		}
		challenge.FailedAttempts = r.mfaAccountFailure.FailedAttempts
		copy := *challenge
		return &copy,
			r.copyMFAAccountFailureLocked(challenge.ParentAccountID),
			outcome,
			nil
	}
	return nil, nil, "", domain.ErrMFAChallengeNotFound
}

func (r *memoryRepository) copyMFAAccountFailureLocked(
	parentAccountID string,
) *domain.MFAAccountFailureState {
	if r.mfaAccountFailure == nil {
		return &domain.MFAAccountFailureState{
			ParentAccountID: parentAccountID,
		}
	}
	copy := *r.mfaAccountFailure
	return &copy
}

func challengeCopy(challenge *domain.MFAChallenge) domain.MFAChallenge {
	return *challenge
}

func (r *memoryRepository) GetTOTPCredential(
	_ context.Context,
	_ string,
) (*domain.TOTPCredential, error) {
	if r.totpCredential == nil {
		return nil, domain.ErrMFANotConfigured
	}
	copy := *r.totpCredential
	return &copy, nil
}

func (r *memoryRepository) UpsertTOTPCredential(
	_ context.Context,
	credential *domain.TOTPCredential,
) error {
	copy := *credential
	r.totpCredential = &copy
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
