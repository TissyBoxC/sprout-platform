// Package domain contains parent authentication domain types.
package domain

import (
	"errors"
	"time"
)

var (
	ErrEmailExists            = errors.New("email already exists")
	ErrPhoneExists            = errors.New("phone already exists")
	ErrAccountNotFound        = errors.New("parent account not found")
	ErrInvalidCredentials     = errors.New("invalid credentials")
	ErrAccountDisabled        = errors.New("parent account is disabled")
	ErrSessionNotFound        = errors.New("session not found")
	ErrSessionExpired         = errors.New("session expired")
	ErrMFARequired            = errors.New("administrator MFA is required")
	ErrMFAChallengeNotFound   = errors.New("MFA challenge not found")
	ErrMFAChallengeExpired    = errors.New("MFA challenge expired")
	ErrMFAChallengeConsumed   = errors.New("MFA challenge already used")
	ErrInvalidMFACode         = errors.New("invalid MFA code")
	ErrMFANotConfigured       = errors.New("administrator MFA is not configured")
	ErrTOTPAlreadyConfigured  = errors.New("administrator TOTP is already configured")
	ErrGuardianConsent        = errors.New("guardian consent is required")
	ErrInvalidEmail           = errors.New("invalid email")
	ErrInvalidPhone           = errors.New("invalid phone")
	ErrPhoneNotBound          = errors.New("phone is not bound")
	ErrPhoneVerification      = errors.New("phone verification is unavailable")
	ErrInvalidVerification    = errors.New("verification code is invalid")
	ErrWeakPassword           = errors.New("password does not meet requirements")
	ErrInvalidDisplayName     = errors.New("display name is required")
	ErrInvalidGuardianName    = errors.New("guardian family name is invalid")
	ErrInvalidChildNickname   = errors.New("child nickname is invalid")
	ErrInvalidChildBirthday   = errors.New("child birthday is invalid")
	ErrAIAccountUnavailable   = errors.New("AI account is unavailable")
	ErrInsufficientPrivilege  = errors.New("insufficient privilege")
	ErrUnknownAdminPermission = errors.New("unknown administrator permission")
	ErrRegistrationDisabled   = errors.New("registration is disabled")
	ErrEmailLoginDisabled     = errors.New("email login is disabled")
)

const (
	RoleParent = "parent"
	RoleAdmin  = "admin"
)

// ParentAccount is the guardian identity that owns family, device, and AI data.
type ParentAccount struct {
	ID                     string
	Email                  string
	Phone                  string
	PasswordHash           string
	DisplayName            string
	GuardianFamilyName     string
	ChildNickname          string
	ChildBirthday          string
	Status                 string
	GuardianConsentVersion string
	GuardianConsentedAt    time.Time
	PhoneVerifiedAt        *time.Time
	EmailVerifiedAt        *time.Time
	Role                   string
	// AdminPermissions is the operation-level grant for an administrator. It is
	// empty for guardians and for an administrator whose capabilities were
	// revoked. See AdminPermissionCatalog for the assignable values.
	AdminPermissions []string
	LastLoginAt      *time.Time
	CreatedAt        time.Time
	UpdatedAt        time.Time
}

// Session is one renewable parent login session.
type Session struct {
	ID               string
	ParentAccountID  string
	RefreshTokenHash string
	ExpiresAt        time.Time
	RevokedAt        *time.Time
	CreatedAt        time.Time
	LastUsedAt       time.Time
}

// TokenPair is the transport result for login and refresh operations.
type TokenPair struct {
	AccessToken  string
	RefreshToken string
	ExpiresIn    time.Duration
}

// RegisterInput contains the minimum data allowed during parent registration.
type RegisterInput struct {
	Phone                  string
	PhoneVerificationCode  string
	Password               string
	GuardianFamilyName     string
	ChildNickname          string
	ChildBirthday          string
	GuardianConsentVersion string
}

// LoginInput accepts either a phone number or a bound email as identifier.
type LoginInput struct {
	Identifier string
	Password   string
}

// BindEmailInput binds an optional email to an already authenticated guardian.
// Re-entering the password prevents a stolen session from silently adding a
// recovery address that can later be used for sign-in.
type BindEmailInput struct {
	Email    string
	Password string
}

// AdminMFALoginInput completes an administrator login after password
// verification by validating a short-lived challenge and TOTP code.
type AdminMFALoginInput struct {
	ChallengeToken string
	Code           string
}

// MFAChallenge is one short-lived administrator second-factor challenge.
// Only the hash of the challenge token is persisted.
type MFAChallenge struct {
	ID              string
	ParentAccountID string
	ChallengeHash   string
	ExpiresAt       time.Time
	ConsumedAt      *time.Time
	CreatedAt       time.Time
}

// PhoneVerificationCode stores only a hash of a short-lived SMS code.
type PhoneVerificationCode struct {
	ID        string
	Phone     string
	Purpose   string
	CodeHash  string
	ExpiresAt time.Time
	CreatedAt time.Time
}

// TOTPCredential is the encrypted TOTP secret enrolled for one administrator.
type TOTPCredential struct {
	ParentAccountID string
	EncryptedSecret []byte
	SecretNonce     []byte
	KeyVersion      int
	EnabledAt       *time.Time
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

// AuthenticatedParent is returned to authorized platform modules.
type AuthenticatedParent struct {
	AccountID   string
	Email       string
	DisplayName string
	Role        string
}

// AIAccountSummary is the parent-safe projection of the linked AI account.
// It never contains a provider API key or an internal service identifier.
type AIAccountSummary struct {
	Status           string
	BalanceUSD       float64
	ConcurrencyLimit int
	// AvailableModels is the platform-approved pool guardians can choose from.
	AvailableModels []string
	// SelectedModels is the guardian's explicit choice; empty means all.
	SelectedModels []string
	// AllowedModels is the effective model allowlist pushed to the provider.
	AllowedModels []string
	ProviderReady bool
}
