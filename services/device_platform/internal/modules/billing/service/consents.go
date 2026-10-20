package service

import (
	"context"
	"strings"

	"github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/billing/domain"
	"github.com/google/uuid"
)

// SpendingConsentService records and withdraws a guardian's agreement to spend.
//
// A child may not trigger a purchase on their own: the guardian grants consent
// in the app, and every checkout re-checks that a live consent exists. Withdrawal
// is immediate and never deletes the evidence that consent once existed.
type SpendingConsentService struct {
	base
}

// NewSpendingConsentService creates the spending consent service.
func NewSpendingConsentService(options Options) (*SpendingConsentService, error) {
	shared, err := newBase(options)
	if err != nil {
		return nil, err
	}
	return &SpendingConsentService{base: shared}, nil
}

// GrantConsent records the guardian's agreement to the current terms version.
//
// An existing live grant is returned as is: re-tapping the consent screen must
// not manufacture a second record of the same agreement, and the first grant
// time is the evidence that matters.
func (s *SpendingConsentService) GrantConsent(
	ctx context.Context,
	parentAccountID string,
	consentVersion string,
	clientIP string,
) (*domain.SpendingConsent, error) {
	accountID := strings.TrimSpace(parentAccountID)
	if accountID == "" {
		return nil, invalidRequest("parent account is required")
	}
	version := strings.TrimSpace(consentVersion)
	if version == "" {
		version = SpendingConsentVersion
	}
	now := s.now()
	consent, err := s.repository.GrantSpendingConsent(ctx, domain.SpendingConsent{
		ID:              uuid.NewString(),
		ParentAccountID: accountID,
		ConsentVersion:  version,
		GrantedAt:       now,
		IPAddress:       strings.TrimSpace(clientIP),
		Method:          ConsentMethodAppConfirm,
		CreatedAt:       now,
		UpdatedAt:       now,
	})
	if err != nil {
		return nil, err
	}
	s.recordAudit(ctx, BillingAuditEvent{
		Action:          "billing.consent.granted",
		ParentAccountID: accountID,
		Detail:          map[string]any{"consent_version": version},
		CreatedAt:       now,
	})
	return consent, nil
}

// RevokeConsent withdraws the guardian's agreement and reports whether a live
// grant existed. A second revoke is not an error: the end state is the same.
func (s *SpendingConsentService) RevokeConsent(
	ctx context.Context,
	parentAccountID string,
	consentVersion string,
) (bool, error) {
	version := strings.TrimSpace(consentVersion)
	if version == "" {
		version = SpendingConsentVersion
	}
	now := s.now()
	revoked, err := s.repository.RevokeSpendingConsent(ctx, parentAccountID, version, now)
	if err != nil {
		return false, err
	}
	if revoked {
		s.recordAudit(ctx, BillingAuditEvent{
			Action:          "billing.consent.revoked",
			ParentAccountID: parentAccountID,
			Detail:          map[string]any{"consent_version": version},
			CreatedAt:       now,
		})
	}
	return revoked, nil
}

// IsActive reports whether the family holds a live consent for the version.
//
// It answers a plain boolean rather than an error for "no consent", because a
// family that never granted consent is a normal state, not a failure.
func (s *SpendingConsentService) IsActive(
	ctx context.Context,
	parentAccountID string,
	consentVersion string,
) (bool, error) {
	version := strings.TrimSpace(consentVersion)
	if version == "" {
		version = SpendingConsentVersion
	}
	consent, err := s.repository.GetActiveSpendingConsent(ctx, parentAccountID, version)
	if err != nil {
		return false, err
	}
	return consent != nil && consent.IsActive(), nil
}

// GetActive returns the live consent, or nil when the family has none.
func (s *SpendingConsentService) GetActive(
	ctx context.Context,
	parentAccountID string,
	consentVersion string,
) (*domain.SpendingConsent, error) {
	version := strings.TrimSpace(consentVersion)
	if version == "" {
		version = SpendingConsentVersion
	}
	return s.repository.GetActiveSpendingConsent(ctx, parentAccountID, version)
}
