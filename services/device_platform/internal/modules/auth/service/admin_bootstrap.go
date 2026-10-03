package service

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/auth/domain"
	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"
)

// CreateParent creates one guardian account through a trusted maintenance
// path.
//
// This exists for support and acceptance testing when SMS delivery is not yet
// configured. It still enforces the normal phone, password, consent, and
// profile rules, but intentionally does not issue a session because the
// maintenance command is not a user login flow.
func (s *Service) CreateParent(
	ctx context.Context,
	input domain.RegisterInput,
) (*domain.ParentAccount, *domain.AIAccountSummary, error) {
	input.Phone = strings.TrimSpace(input.Phone)
	input.GuardianFamilyName = strings.TrimSpace(input.GuardianFamilyName)
	input.ChildNickname = strings.TrimSpace(input.ChildNickname)
	input.ChildBirthday = strings.TrimSpace(input.ChildBirthday)
	input.GuardianConsentVersion = strings.TrimSpace(input.GuardianConsentVersion)
	if input.GuardianConsentVersion == "" {
		input.GuardianConsentVersion = defaultConsent
	}
	if err := validateRegistration(input); err != nil {
		return nil, nil, err
	}

	passwordHash, err := bcrypt.GenerateFromPassword(
		[]byte(input.Password),
		bcrypt.DefaultCost,
	)
	if err != nil {
		return nil, nil, fmt.Errorf("hash password: %w", err)
	}

	now := s.timeSource.Now().UTC()
	account := &domain.ParentAccount{
		ID:                     uuid.NewString(),
		Phone:                  normalizePhone(input.Phone),
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
		return nil, nil, err
	}
	if s.childProvisioner != nil && account.ChildNickname != "" {
		if _, err := s.childProvisioner.SyncRegistrationProfile(
			ctx,
			account.ID,
			account.ChildNickname,
			account.ChildBirthday,
			account.GuardianConsentVersion,
		); err != nil {
			_ = s.repository.DeleteParentAccount(ctx, account.ID)
			return nil, nil, err
		}
	}

	var summary *domain.AIAccountSummary
	if s.aiProvisioner != nil {
		// Keep the account usable when the AI gateway is temporarily down; a
		// later login repairs the provider projection.
		summary, _ = s.aiProvisioner.EnsureForParent(ctx, account.ID, account.Email)
	}
	return account, summary, nil
}

// BootstrapAdmin creates one initial administrator and enrolls TOTP.
//
// This operation is intended for a trusted local maintenance command. It
// returns the one-time otpauth URI so the operator can scan it into an
// authenticator; the secret is never persisted in plaintext or logged here.
func (s *Service) BootstrapAdmin(
	ctx context.Context,
	email string,
	password string,
	displayName string,
) (string, error) {
	email = normalizeEmail(email)
	displayName = strings.TrimSpace(displayName)
	if email == "" || !strings.Contains(email, "@") ||
		strings.HasPrefix(email, "@") || strings.HasSuffix(email, "@") {
		return "", domain.ErrInvalidEmail
	}
	if len(password) < 8 || len(password) > 128 ||
		!containsLetterAndNumber(password) {
		return "", domain.ErrWeakPassword
	}
	if displayName == "" || len([]rune(displayName)) > 40 {
		return "", domain.ErrInvalidDisplayName
	}

	existing, err := s.repository.GetParentAccountByEmail(ctx, email)
	if err == nil {
		if existing.Role != domain.RoleAdmin {
			return "", domain.ErrEmailExists
		}
		// A previous bootstrap may have created the account before TOTP
		// enrollment failed. Re-running only completes the missing enrollment.
		return s.EnrollAdminTOTP(ctx, existing.ID)
	}
	if !errors.Is(err, domain.ErrAccountNotFound) {
		return "", err
	}

	passwordHash, err := bcrypt.GenerateFromPassword(
		[]byte(password),
		bcrypt.DefaultCost,
	)
	if err != nil {
		return "", fmt.Errorf("hash administrator password: %w", err)
	}
	now := s.timeSource.Now().UTC()
	accountID := uuid.NewString()
	account := &domain.ParentAccount{
		ID:           accountID,
		Email:        email,
		Phone:        "admin:" + accountID,
		PasswordHash: string(passwordHash),
		DisplayName:  displayName,
		Status:       accountStatusActive,
		Role:         domain.RoleAdmin,
		CreatedAt:    now,
		UpdatedAt:    now,
	}
	if err := s.repository.CreateParentAccount(ctx, account); err != nil {
		return "", err
	}
	uri, err := s.EnrollAdminTOTP(ctx, account.ID)
	if err != nil {
		return "", err
	}
	return uri, nil
}

// ResetAdminPassword replaces the password for one existing administrator.
//
// This operation is intended for a trusted local maintenance command. It
// deliberately refuses non-admin accounts and does not touch TOTP enrollment.
func (s *Service) ResetAdminPassword(
	ctx context.Context,
	email string,
	password string,
) error {
	email = normalizeEmail(email)
	if email == "" || !strings.Contains(email, "@") ||
		strings.HasPrefix(email, "@") || strings.HasSuffix(email, "@") {
		return domain.ErrInvalidEmail
	}
	if !isStrongPassword(password) {
		return domain.ErrWeakPassword
	}

	account, err := s.repository.GetParentAccountByEmail(ctx, email)
	if err != nil {
		return err
	}
	if account.Role != domain.RoleAdmin {
		return domain.ErrInsufficientPrivilege
	}

	passwordHash, err := bcrypt.GenerateFromPassword(
		[]byte(password),
		bcrypt.DefaultCost,
	)
	if err != nil {
		return fmt.Errorf("hash administrator password: %w", err)
	}
	return s.repository.UpdatePasswordHash(ctx, account.ID, string(passwordHash))
}

// ResetParentPassword replaces the password for one existing parent account.
//
// It intentionally refuses administrator accounts and revokes every active
// session through the repository so the previous password cannot be reused.
func (s *Service) ResetParentPassword(
	ctx context.Context,
	accountID string,
	password string,
) error {
	if !isStrongPassword(password) {
		return domain.ErrWeakPassword
	}

	account, err := s.repository.GetParentAccountByID(ctx, accountID)
	if err != nil {
		return err
	}
	if account.Role != domain.RoleParent {
		return domain.ErrInsufficientPrivilege
	}

	passwordHash, err := bcrypt.GenerateFromPassword(
		[]byte(password),
		bcrypt.DefaultCost,
	)
	if err != nil {
		// bcrypt's only input error is an overlong password; other failures
		// indicate invalid cost configuration and must not reach the caller.
		return domain.ErrWeakPassword
	}
	return s.repository.UpdatePasswordHash(ctx, account.ID, string(passwordHash))
}

func isStrongPassword(password string) bool {
	return len(password) >= 8 &&
		len(password) <= 128 &&
		containsLetterAndNumber(password)
}
