package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/auth/domain"
	authrepository "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/auth/repository"
	platformclock "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/platform/clock"
	"github.com/pquerna/otp/totp"
	"golang.org/x/crypto/bcrypt"
)

func TestCompleteAdminLoginLocksAfterRepeatedWrongCodes(t *testing.T) {
	fixture := newMFAFixture(t)

	for attempt := 1; attempt < defaultMFAMaxAttempts; attempt++ {
		_, _, err := fixture.service.CompleteAdminLogin(
			context.Background(),
			domain.AdminMFALoginInput{
				ChallengeToken: fixture.challengeToken,
				Code:           "000000",
			},
		)
		if !errors.Is(err, domain.ErrInvalidMFACode) {
			t.Fatalf("attempt %d: expected invalid code, got %v", attempt, err)
		}
	}
	_, _, err := fixture.service.CompleteAdminLogin(
		context.Background(),
		domain.AdminMFALoginInput{
			ChallengeToken: fixture.challengeToken,
			Code:           "000000",
		},
	)
	if !errors.Is(err, domain.ErrMFAAccountLocked) {
		t.Fatalf("expected account lock, got %v", err)
	}
	challenge := fixture.challenge()
	if challenge.FailedAttempts != defaultMFAMaxAttempts || challenge.LockedAt == nil {
		t.Fatalf("persisted challenge state = %+v", challenge)
	}

	_, _, err = fixture.service.CompleteAdminLogin(
		context.Background(),
		domain.AdminMFALoginInput{
			ChallengeToken: fixture.challengeToken,
			Code:           mustMFAcode(t, fixture.secret, time.Now().UTC()),
		},
	)
	if !errors.Is(err, domain.ErrMFAAccountLocked) {
		t.Fatalf("a correct code must not unlock a locked challenge: %v", err)
	}
	if !fixture.auditor.hasAction("admin.mfa.locked") {
		t.Fatal("lock transition must be audited")
	}
}

func TestCompleteAdminLoginFailureBudgetSpansChallenges(t *testing.T) {
	fixture := newMFAFixture(t)

	for attempt := 0; attempt < defaultMFAMaxAttempts-1; attempt++ {
		token := fixture.startChallenge(t)
		_, _, err := fixture.service.CompleteAdminLogin(
			context.Background(),
			domain.AdminMFALoginInput{ChallengeToken: token, Code: "000000"},
		)
		if !errors.Is(err, domain.ErrInvalidMFACode) {
			t.Fatalf("attempt %d: expected invalid code, got %v", attempt+1, err)
		}
	}

	token := fixture.startChallenge(t)
	_, _, err := fixture.service.CompleteAdminLogin(
		context.Background(),
		domain.AdminMFALoginInput{ChallengeToken: token, Code: "000000"},
	)
	if !errors.Is(err, domain.ErrMFAAccountLocked) {
		t.Fatalf("expected account-wide lock from a new challenge, got %v", err)
	}

	otherToken, _, startErr := fixture.service.StartAdminLogin(
		context.Background(),
		domain.LoginInput{
			Identifier: fixture.accountIdentifier(),
			Password:   "valid-password",
		},
	)
	if !errors.Is(startErr, domain.ErrMFAAccountLocked) {
		t.Fatalf("starting a challenge for a locked account must fail: %v", startErr)
	}
	if otherToken != "" {
		t.Fatal("locked account received a MFA challenge token")
	}
	otherToken = fixture.addChallenge(t, "locked-account-probe")
	_, _, err = fixture.service.CompleteAdminLogin(
		context.Background(),
		domain.AdminMFALoginInput{
			ChallengeToken: otherToken,
			Code:           mustMFAcode(t, fixture.secret, fixture.clock.now),
		},
	)
	if !errors.Is(err, domain.ErrMFAAccountLocked) {
		t.Fatalf("a correct code must not bypass an account lock: %v", err)
	}
	state := fixture.accountFailureState()
	if state.FailedAttempts != defaultMFAMaxAttempts ||
		state.LockedUntil == nil ||
		!state.LockedUntil.After(fixture.clock.now) {
		t.Fatalf("account failure state = %+v", state)
	}
	if fixture.auditor.containsSecretMaterial("000000") ||
		fixture.auditor.containsSecretMaterial(token) ||
		fixture.auditor.containsSecretMaterial(fixture.secret) {
		t.Fatal("account lock audit recorded TOTP or challenge material")
	}
}

func TestCompleteAdminLoginExpiredLockStartsFreshBudget(t *testing.T) {
	fixture := newMFAFixture(t)
	fixture.lockAccount(t)

	fixture.clock.now = fixture.accountFailureState().LockedUntil.Add(time.Second)
	token := fixture.startChallenge(t)
	_, _, err := fixture.service.CompleteAdminLogin(
		context.Background(),
		domain.AdminMFALoginInput{ChallengeToken: token, Code: "000000"},
	)
	if !errors.Is(err, domain.ErrInvalidMFACode) {
		t.Fatalf("expected a fresh wrong-code result after lock expiry, got %v", err)
	}
	state := fixture.accountFailureState()
	if state.FailedAttempts != 1 || state.LockedUntil != nil {
		t.Fatalf("expired lock did not reset budget: %+v", state)
	}
	if !fixture.auditor.hasAction("admin.mfa.account_recovered") {
		t.Fatal("expired account lock recovery must be audited")
	}
}

func TestCompleteAdminLoginSuccessClearsAccountFailureBudget(t *testing.T) {
	fixture := newMFAFixture(t)
	token := fixture.startChallenge(t)
	for attempt := 0; attempt < defaultMFAMaxAttempts-1; attempt++ {
		_, _, err := fixture.service.CompleteAdminLogin(
			context.Background(),
			domain.AdminMFALoginInput{ChallengeToken: token, Code: "000000"},
		)
		if !errors.Is(err, domain.ErrInvalidMFACode) {
			t.Fatalf("attempt %d: expected invalid code, got %v", attempt+1, err)
		}
	}
	if fixture.accountFailureState().FailedAttempts != defaultMFAMaxAttempts-1 {
		t.Fatalf("pre-success failure budget = %+v", fixture.accountFailureState())
	}

	_, pair, err := fixture.service.CompleteAdminLogin(
		context.Background(),
		domain.AdminMFALoginInput{
			ChallengeToken: token,
			Code:           mustMFAcode(t, fixture.secret, fixture.clock.now),
		},
	)
	if err != nil || pair == nil {
		t.Fatalf("successful MFA failed: pair=%+v err=%v", pair, err)
	}
	state := fixture.accountFailureState()
	if state.FailedAttempts != 0 || state.LockedUntil != nil {
		t.Fatalf("successful MFA did not clear account failures: %+v", state)
	}
}

func TestCompleteAdminLoginAccountLockKeepsStableErrorContract(t *testing.T) {
	fixture := newMFAFixture(t)
	fixture.lockAccount(t)

	_, _, err := fixture.service.CompleteAdminLogin(
		context.Background(),
		domain.AdminMFALoginInput{
			ChallengeToken: fixture.challengeToken,
			Code:           mustMFAcode(t, fixture.secret, fixture.clock.now),
		},
	)
	if !errors.Is(err, domain.ErrMFAAccountLocked) {
		t.Fatalf("expected account lock, got %v", err)
	}
	if !errors.Is(err, domain.ErrMFAChallengeLocked) {
		t.Fatalf("account lock lost the established HTTP error contract: %v", err)
	}
	if errors.Is(domain.ErrMFAChallengeLocked, domain.ErrMFAAccountLocked) {
		t.Fatal("challenge lock must not be reported as an account lock")
	}
}

func TestCompleteAdminLoginAuditDetailsContainOnlyNonSensitiveState(t *testing.T) {
	fixture := newMFAFixture(t)
	fixture.lockAccount(t)
	_, _, err := fixture.service.CompleteAdminLogin(
		context.Background(),
		domain.AdminMFALoginInput{
			ChallengeToken: fixture.challengeToken,
			Code:           mustMFAcode(t, fixture.secret, fixture.clock.now),
		},
	)
	if !errors.Is(err, domain.ErrMFAAccountLocked) {
		t.Fatalf("expected account lock, got %v", err)
	}

	details := fixture.auditor.auditDetails()
	for _, detail := range details {
		for key, value := range detail {
			switch key {
			case "outcome", "failed_attempts", "remaining_attempts":
			default:
				t.Fatalf("audit detail contains unexpected key %q", key)
			}
			switch typed := value.(type) {
			case string:
				if typed == "000000" ||
					typed == fixture.secret ||
					typed == fixture.challengeToken {
					t.Fatalf("audit detail leaked secret material for %q", key)
				}
			}
		}
	}
	lockedDetails := fixture.auditor.detailsForAction("admin.mfa.locked")
	if len(lockedDetails) != 1 {
		t.Fatalf("lock audit count = %d, want 1", len(lockedDetails))
	}
	if lockedDetails[0]["outcome"] != "account_locked" {
		t.Fatalf(
			"lock audit outcome = %v, want account_locked",
			lockedDetails[0]["outcome"],
		)
	}
	if lockedDetails[0]["failed_attempts"] != defaultMFAMaxAttempts {
		t.Fatalf(
			"lock audit failed_attempts = %v, want %d",
			lockedDetails[0]["failed_attempts"],
			defaultMFAMaxAttempts,
		)
	}
}

func TestCompleteAdminLoginConcurrentFailuresAcrossChallengesDoNotLoseCounts(t *testing.T) {
	fixture := newMFAFixture(t)
	const requests = 12
	tokens := make([]string, 0, requests)
	for index := 0; index < requests; index++ {
		tokens = append(tokens, fixture.startChallenge(t))
	}

	var invalid, locked, failures atomic.Int32
	start := make(chan struct{})
	var wait sync.WaitGroup
	for _, token := range tokens {
		wait.Add(1)
		go func(challengeToken string) {
			defer wait.Done()
			<-start
			_, _, err := fixture.service.CompleteAdminLogin(
				context.Background(),
				domain.AdminMFALoginInput{
					ChallengeToken: challengeToken,
					Code:           "000000",
				},
			)
			switch {
			case errors.Is(err, domain.ErrInvalidMFACode):
				invalid.Add(1)
			case errors.Is(err, domain.ErrMFAAccountLocked):
				locked.Add(1)
			default:
				failures.Add(1)
			}
		}(token)
	}
	close(start)
	wait.Wait()

	if invalid.Load() != defaultMFAMaxAttempts-1 ||
		locked.Load() != requests-defaultMFAMaxAttempts+1 ||
		failures.Load() != 0 {
		t.Fatalf(
			"invalid=%d locked=%d failures=%d",
			invalid.Load(),
			locked.Load(),
			failures.Load(),
		)
	}
	state := fixture.accountFailureState()
	if state.FailedAttempts != defaultMFAMaxAttempts || state.LockedUntil == nil {
		t.Fatalf("concurrent failures lost counts: %+v", state)
	}
	if fixture.auditor.countAction("admin.mfa.locked") != 1 {
		t.Fatalf(
			"account lock transitions = %d, want 1",
			fixture.auditor.countAction("admin.mfa.locked"),
		)
	}
	if fixture.auditor.containsSecretMaterial("000000") {
		t.Fatal("concurrent failure audit recorded the submitted TOTP")
	}
}

func TestCompleteAdminLoginConsumesChallengeAndRejectsReplay(t *testing.T) {
	fixture := newMFAFixture(t)
	code := mustMFAcode(t, fixture.secret, time.Now().UTC())

	if _, pair, err := fixture.service.CompleteAdminLogin(
		context.Background(),
		domain.AdminMFALoginInput{
			ChallengeToken: fixture.challengeToken,
			Code:           code,
		},
	); err != nil {
		t.Fatalf("complete administrator login: %v", err)
	} else if pair == nil || pair.AccessToken == "" || pair.RefreshToken == "" {
		t.Fatal("successful MFA must issue a complete session")
	}

	_, _, err := fixture.service.CompleteAdminLogin(
		context.Background(),
		domain.AdminMFALoginInput{
			ChallengeToken: fixture.challengeToken,
			Code:           code,
		},
	)
	if !errors.Is(err, domain.ErrMFAChallengeConsumed) {
		t.Fatalf("expected replay rejection, got %v", err)
	}
	if !fixture.auditor.hasAction("admin.mfa.replayed") {
		t.Fatal("replay must be audited")
	}
}

func TestCompleteAdminLoginRejectsExpiredChallenge(t *testing.T) {
	fixture := newMFAFixture(t)
	fixture.clock.now = fixture.clock.now.Add(5 * time.Minute)

	_, _, err := fixture.service.CompleteAdminLogin(
		context.Background(),
		domain.AdminMFALoginInput{
			ChallengeToken: fixture.challengeToken,
			Code:           mustMFAcode(t, fixture.secret, time.Now().UTC()),
		},
	)
	if !errors.Is(err, domain.ErrMFAChallengeExpired) {
		t.Fatalf("expected expired challenge, got %v", err)
	}
	if !fixture.auditor.hasAction("admin.mfa.expired") {
		t.Fatal("expiry rejection must be audited")
	}
}

func TestCompleteAdminLoginAuditFailureDeniesSession(t *testing.T) {
	fixture := newMFAFixture(t)
	fixture.auditor.fail.Store(true)

	_, pair, err := fixture.service.CompleteAdminLogin(
		context.Background(),
		domain.AdminMFALoginInput{
			ChallengeToken: fixture.challengeToken,
			Code:           mustMFAcode(t, fixture.secret, time.Now().UTC()),
		},
	)
	if !errors.Is(err, domain.ErrMFAAuditUnavailable) {
		t.Fatalf("expected audit failure, got %v", err)
	}
	if pair != nil {
		t.Fatal("audit failure must never return a session")
	}
}

func TestCompleteAdminLoginFailedCodeAuditFailureReturnsSafeError(t *testing.T) {
	fixture := newMFAFixture(t)
	fixture.auditor.fail.Store(true)

	_, pair, err := fixture.service.CompleteAdminLogin(
		context.Background(),
		domain.AdminMFALoginInput{
			ChallengeToken: fixture.challengeToken,
			Code:           "000000",
		},
	)
	if !errors.Is(err, domain.ErrMFAAuditUnavailable) {
		t.Fatalf("expected audit failure, got %v", err)
	}
	if pair != nil {
		t.Fatal("audit failure must never return a session")
	}
}

func TestCompleteAdminLoginWrongCodeOnConsumedChallengeAuditsReplay(t *testing.T) {
	fixture := newMFAFixture(t)
	code := mustMFAcode(t, fixture.secret, time.Now().UTC())
	if _, _, err := fixture.service.CompleteAdminLogin(
		context.Background(),
		domain.AdminMFALoginInput{
			ChallengeToken: fixture.challengeToken,
			Code:           code,
		},
	); err != nil {
		t.Fatalf("complete administrator login: %v", err)
	}

	_, _, err := fixture.service.CompleteAdminLogin(
		context.Background(),
		domain.AdminMFALoginInput{
			ChallengeToken: fixture.challengeToken,
			Code:           "000000",
		},
	)
	if !errors.Is(err, domain.ErrMFAChallengeConsumed) {
		t.Fatalf("expected consumed challenge, got %v", err)
	}
	if fixture.auditor.countAction("admin.mfa.replayed") != 1 {
		t.Fatalf(
			"replay audit count = %d, want 1",
			fixture.auditor.countAction("admin.mfa.replayed"),
		)
	}
	if fixture.auditor.containsSecretMaterial("000000") {
		t.Fatal("MFA audit recorded the submitted TOTP")
	}
}

func TestCompleteAdminLoginWrongCodeOnExpiredChallengeAuditsExpiry(t *testing.T) {
	fixture := newMFAFixture(t)
	fixture.clock.now = fixture.clock.now.Add(5 * time.Minute)

	_, _, err := fixture.service.CompleteAdminLogin(
		context.Background(),
		domain.AdminMFALoginInput{
			ChallengeToken: fixture.challengeToken,
			Code:           "000000",
		},
	)
	if !errors.Is(err, domain.ErrMFAChallengeExpired) {
		t.Fatalf("expected expired challenge, got %v", err)
	}
	if fixture.auditor.countAction("admin.mfa.expired") != 1 {
		t.Fatalf(
			"expiry audit count = %d, want 1",
			fixture.auditor.countAction("admin.mfa.expired"),
		)
	}
}

func TestStartAdminLoginAuditFailureWithholdsAndRemovesChallenge(t *testing.T) {
	fixture := newMFAFixture(t)
	fixture.repository.challenges = map[string]*domain.MFAChallenge{}
	fixture.auditor.fail.Store(true)

	token, account, err := fixture.service.StartAdminLogin(
		context.Background(),
		domain.LoginInput{
			Identifier: fixture.accountIdentifier(),
			Password:   "valid-password",
		},
	)
	if !errors.Is(err, domain.ErrMFAAuditUnavailable) {
		t.Fatalf("expected audit failure, got %v", err)
	}
	if token != "" || account != nil {
		t.Fatalf("audit failure must withhold account and token: %q %+v", token, account)
	}
	fixture.repository.mu.Lock()
	challengeCount := len(fixture.repository.challenges)
	fixture.repository.mu.Unlock()
	if challengeCount != 0 {
		t.Fatalf("unaudited challenge was left behind: %d", challengeCount)
	}
}

func TestCompleteAdminLoginConcurrentWrongCodesLockExactlyOnce(t *testing.T) {
	fixture := newMFAFixture(t)
	const requests = 20
	var invalid, locked, failures atomic.Int32
	start := make(chan struct{})
	var wait sync.WaitGroup
	for index := 0; index < requests; index++ {
		wait.Add(1)
		go func() {
			defer wait.Done()
			<-start
			_, _, err := fixture.service.CompleteAdminLogin(
				context.Background(),
				domain.AdminMFALoginInput{
					ChallengeToken: fixture.challengeToken,
					Code:           "000000",
				},
			)
			switch {
			case errors.Is(err, domain.ErrInvalidMFACode):
				invalid.Add(1)
			case errors.Is(err, domain.ErrMFAAccountLocked):
				locked.Add(1)
			default:
				failures.Add(1)
			}
		}()
	}
	close(start)
	wait.Wait()

	if invalid.Load() != defaultMFAMaxAttempts-1 ||
		locked.Load() != requests-defaultMFAMaxAttempts+1 ||
		failures.Load() != 0 {
		t.Fatalf(
			"invalid=%d locked=%d failures=%d",
			invalid.Load(),
			locked.Load(),
			failures.Load(),
		)
	}
	challenge := fixture.challenge()
	if challenge.FailedAttempts != defaultMFAMaxAttempts || challenge.LockedAt == nil {
		t.Fatalf("persisted challenge state = %+v", challenge)
	}
	if fixture.auditor.countAction("admin.mfa.locked") != 1 ||
		fixture.auditor.countAction("admin.mfa.locked_observed") !=
			requests-defaultMFAMaxAttempts {
		t.Fatalf(
			"lock=%d observed=%d",
			fixture.auditor.countAction("admin.mfa.locked"),
			fixture.auditor.countAction("admin.mfa.locked_observed"),
		)
	}
	for _, failedAttempts := range fixture.auditor.observedFailedAttempts() {
		if failedAttempts != defaultMFAMaxAttempts {
			t.Fatalf(
				"locked_observed failed_attempts = %d, want %d",
				failedAttempts,
				defaultMFAMaxAttempts,
			)
		}
	}
}

func TestCompleteAdminLoginConcurrentReplayIssuesOneSession(t *testing.T) {
	fixture := newMFAFixture(t)
	code := mustMFAcode(t, fixture.secret, time.Now().UTC())
	const requests = 12
	var succeeded, consumed, failures atomic.Int32
	start := make(chan struct{})
	var wait sync.WaitGroup
	for index := 0; index < requests; index++ {
		wait.Add(1)
		go func() {
			defer wait.Done()
			<-start
			_, pair, err := fixture.service.CompleteAdminLogin(
				context.Background(),
				domain.AdminMFALoginInput{
					ChallengeToken: fixture.challengeToken,
					Code:           code,
				},
			)
			switch {
			case err == nil && pair != nil:
				succeeded.Add(1)
			case errors.Is(err, domain.ErrMFAChallengeConsumed):
				consumed.Add(1)
			default:
				failures.Add(1)
			}
		}()
	}
	close(start)
	wait.Wait()

	if succeeded.Load() != 1 || consumed.Load() != requests-1 || failures.Load() != 0 {
		t.Fatalf(
			"success=%d consumed=%d failures=%d",
			succeeded.Load(),
			consumed.Load(),
			failures.Load(),
		)
	}
}

type mfaFixture struct {
	service        *Service
	repository     *memoryRepository
	auditor        *recordingMFAAuditor
	clock          *fixedMFAClock
	challengeToken string
	secret         string
	challengeSeq   int
}

func newMFAFixture(t *testing.T) *mfaFixture {
	t.Helper()
	account := &domain.ParentAccount{
		ID:          "admin-mfa-1",
		Email:       "admin@example.com",
		DisplayName: "安全管理员",
		Role:        domain.RoleAdmin,
		Status:      accountStatusActive,
	}
	repository := &memoryRepository{account: account}
	auditor := &recordingMFAAuditor{}
	clock := &fixedMFAClock{now: time.Now().UTC().Truncate(time.Second)}
	service, err := New(Options{
		Repository:      repository,
		TokenIssuer:     mustTokenIssuer(t),
		MFACipher:       plaintextMFACipher{},
		MFAAuditor:      auditor,
		Clock:           clock,
		AccessTTL:       time.Minute,
		RefreshTTL:      time.Hour,
		MFAChallengeTTL: 5 * time.Minute,
		MFAMaxAttempts:  defaultMFAMaxAttempts,
	})
	if err != nil {
		t.Fatalf("create MFA service: %v", err)
	}
	key, err := totp.Generate(totp.GenerateOpts{
		Issuer:      "sprout-test",
		AccountName: account.Email,
	})
	if err != nil {
		t.Fatalf("generate TOTP secret: %v", err)
	}
	repository.totpCredential = &domain.TOTPCredential{
		ParentAccountID: account.ID,
		EncryptedSecret: []byte(key.Secret()),
		SecretNonce:     []byte("test-nonce"),
	}
	passwordHash, err := bcrypt.GenerateFromPassword(
		[]byte("valid-password"),
		bcrypt.MinCost,
	)
	if err != nil {
		t.Fatalf("hash test password: %v", err)
	}
	account.PasswordHash = string(passwordHash)
	challengeToken := "mfa-test-challenge-token"
	repository.challenges = map[string]*domain.MFAChallenge{
		hashRefreshToken(challengeToken): {
			ID:              "mfa-challenge-1",
			ParentAccountID: account.ID,
			ChallengeHash:   hashRefreshToken(challengeToken),
			ExpiresAt:       clock.now.Add(5 * time.Minute),
			CreatedAt:       clock.now,
		},
	}
	return &mfaFixture{
		service:        service,
		repository:     repository,
		auditor:        auditor,
		clock:          clock,
		challengeToken: challengeToken,
		secret:         key.Secret(),
		challengeSeq:   1,
	}
}

func (fixture *mfaFixture) startChallenge(t *testing.T) string {
	t.Helper()
	token, account, err := fixture.service.StartAdminLogin(
		context.Background(),
		domain.LoginInput{
			Identifier: fixture.accountIdentifier(),
			Password:   "valid-password",
		},
	)
	if err != nil || token == "" || account == nil {
		t.Fatalf("start administrator MFA challenge: token=%q account=%+v err=%v",
			token, account, err)
	}
	return token
}

func (fixture *mfaFixture) addChallenge(t *testing.T, token string) string {
	t.Helper()
	fixture.repository.mu.Lock()
	defer fixture.repository.mu.Unlock()
	fixture.challengeSeq++
	if fixture.repository.challenges == nil {
		fixture.repository.challenges = make(map[string]*domain.MFAChallenge)
	}
	fixture.repository.challenges[hashRefreshToken(token)] = &domain.MFAChallenge{
		ID:              fmt.Sprintf("mfa-challenge-%d", fixture.challengeSeq),
		ParentAccountID: fixture.repository.account.ID,
		ChallengeHash:   hashRefreshToken(token),
		ExpiresAt:       fixture.clock.now.Add(5 * time.Minute),
		CreatedAt:       fixture.clock.now,
	}
	return token
}

func (fixture *mfaFixture) lockAccount(t *testing.T) {
	t.Helper()
	token := fixture.startChallenge(t)
	for attempt := 0; attempt < defaultMFAMaxAttempts; attempt++ {
		_, _, err := fixture.service.CompleteAdminLogin(
			context.Background(),
			domain.AdminMFALoginInput{ChallengeToken: token, Code: "000000"},
		)
		if attempt+1 == defaultMFAMaxAttempts {
			if !errors.Is(err, domain.ErrMFAAccountLocked) {
				t.Fatalf("expected account lock, got %v", err)
			}
			break
		}
		if !errors.Is(err, domain.ErrInvalidMFACode) {
			t.Fatalf("attempt %d: expected invalid code, got %v", attempt+1, err)
		}
	}
}

func (fixture *mfaFixture) accountFailureState() domain.MFAAccountFailureState {
	state, err := fixture.repository.GetMFAAccountFailureState(
		context.Background(),
		fixture.repository.account.ID,
	)
	if err != nil {
		panic(err)
	}
	return *state
}

func (fixture *mfaFixture) challenge() domain.MFAChallenge {
	fixture.repository.mu.Lock()
	defer fixture.repository.mu.Unlock()
	for _, challenge := range fixture.repository.challenges {
		return *challenge
	}
	return domain.MFAChallenge{}
}

func (fixture *mfaFixture) accountIdentifier() string {
	fixture.repository.mu.Lock()
	defer fixture.repository.mu.Unlock()
	return fixture.repository.account.Email
}

type fixedMFAClock struct {
	now time.Time
}

func (clock *fixedMFAClock) Now() time.Time {
	return clock.now
}

type plaintextMFACipher struct{}

func (plaintextMFACipher) Encrypt(plaintext []byte) ([]byte, []byte, error) {
	return append([]byte(nil), plaintext...), []byte("test-nonce"), nil
}

func (plaintextMFACipher) Decrypt(ciphertext []byte, _ []byte) ([]byte, error) {
	return append([]byte(nil), ciphertext...), nil
}

type recordingMFAAuditor struct {
	mu       sync.Mutex
	events   []string
	details  []map[string]any
	failedAt []int
	fail     atomic.Bool
}

func (auditor *recordingMFAAuditor) RecordAudit(
	_ context.Context,
	_ string,
	_ string,
	action string,
	detail map[string]any,
) error {
	if auditor.fail.Load() {
		return errors.New("audit unavailable")
	}
	auditor.mu.Lock()
	defer auditor.mu.Unlock()
	auditor.events = append(auditor.events, action)
	auditor.details = append(auditor.details, detail)
	if action == "admin.mfa.locked_observed" {
		failedAttempts, _ := detail["failed_attempts"].(int)
		auditor.failedAt = append(auditor.failedAt, failedAttempts)
	}
	return nil
}

func (auditor *recordingMFAAuditor) hasAction(action string) bool {
	auditor.mu.Lock()
	defer auditor.mu.Unlock()
	for _, event := range auditor.events {
		if event == action {
			return true
		}
	}
	return false
}

func (auditor *recordingMFAAuditor) countAction(action string) int {
	auditor.mu.Lock()
	defer auditor.mu.Unlock()
	count := 0
	for _, event := range auditor.events {
		if event == action {
			count++
		}
	}
	return count
}

func (auditor *recordingMFAAuditor) containsSecretMaterial(value string) bool {
	auditor.mu.Lock()
	defer auditor.mu.Unlock()
	for _, event := range auditor.events {
		if strings.Contains(event, value) {
			return true
		}
	}
	return false
}

func (auditor *recordingMFAAuditor) observedFailedAttempts() []int {
	auditor.mu.Lock()
	defer auditor.mu.Unlock()
	return append([]int(nil), auditor.failedAt...)
}

func (auditor *recordingMFAAuditor) auditDetails() []map[string]any {
	auditor.mu.Lock()
	defer auditor.mu.Unlock()
	details := make([]map[string]any, 0, len(auditor.details))
	for _, detail := range auditor.details {
		copy := make(map[string]any, len(detail))
		for key, value := range detail {
			copy[key] = value
		}
		details = append(details, copy)
	}
	return details
}

func (auditor *recordingMFAAuditor) detailsForAction(action string) []map[string]any {
	auditor.mu.Lock()
	defer auditor.mu.Unlock()
	details := make([]map[string]any, 0)
	for index, event := range auditor.events {
		if event != action {
			continue
		}
		copy := make(map[string]any, len(auditor.details[index]))
		for key, value := range auditor.details[index] {
			copy[key] = value
		}
		details = append(details, copy)
	}
	return details
}

func mustMFAcode(t *testing.T, secret string, now time.Time) string {
	t.Helper()
	code, err := totp.GenerateCode(secret, now)
	if err != nil {
		t.Fatalf("generate TOTP code: %v", err)
	}
	return code
}

var _ platformclock.Clock = (*fixedMFAClock)(nil)

var _ authrepository.Repository = (*memoryRepository)(nil)
