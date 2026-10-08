// Package service owns guardian consent, export, deletion, and audit use cases.
package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	authdomain "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/auth/domain"
	childdomain "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/child/domain"
	bindingdomain "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/device_binding/domain"
	runtimedomain "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/device_runtime/domain"
	operationsdomain "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/operations/domain"
	"github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/privacy/domain"
	"github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/privacy/repository"
	usagedomain "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/usage_report/domain"
	"github.com/TissyBoxC/sprout-platform/services/device_platform/internal/platform/clock"
	"github.com/google/uuid"
)

const (
	exportSchemaVersion = "1.0.0"
	exportTTL           = 24 * time.Hour
	maxExportUsageDays  = 90
	defaultConsent      = "2026-01"
)

// ParentAccountReader is the narrow identity read surface used by the privacy
// service. It must not expose password hashes to the caller.
type ParentAccountReader interface {
	GetParentAccountByID(
		ctx context.Context,
		accountID string,
	) (*authdomain.ParentAccount, error)
	RevokeAllSessions(ctx context.Context, parentAccountID string) error
}

// ChildReader returns guardian-owned child profiles for exports and deletion.
type ChildReader interface {
	ListByFamilyID(
		ctx context.Context,
		familyID string,
	) ([]childdomain.Child, error)
}

// DeviceReader returns guardian-owned devices for exports and deletion.
type DeviceReader interface {
	ListByParentAccountID(
		ctx context.Context,
		parentAccountID string,
	) ([]bindingdomain.Binding, error)
	RevokeDeviceSessionsAndDeleteBinding(
		ctx context.Context,
		parentAccountID string,
		deviceID string,
	) error
}

// RuntimeReader returns the current online state for each bound device.
type RuntimeReader interface {
	ListDeviceStatuses(
		ctx context.Context,
		parentAccountID string,
	) ([]runtimedomain.DeviceStatus, error)
}

// UsageReader returns aggregate usage for the guardian export.
type UsageReader interface {
	ListByFamily(
		ctx context.Context,
		parentAccountID string,
		fromDate time.Time,
		days int,
	) ([]usagedomain.DailyUsage, error)
}

// AIDeletion is the narrow provider-side account deletion contract.
type AIDeletion interface {
	DeleteForParent(ctx context.Context, parentAccountID string) error
}

// RetentionReader supplies the operator retention disclosure.
type RetentionReader interface {
	RuntimePolicy(ctx context.Context) (*operationsdomain.RuntimePolicy, error)
	Settings(ctx context.Context) (*operationsdomain.Settings, int64, error)
}

// Options contains privacy service dependencies.
type Options struct {
	Repository      repository.Repository
	ParentAccounts  ParentAccountReader
	Children        ChildReader
	Devices         DeviceReader
	Runtime         RuntimeReader
	Usage           UsageReader
	AIAccounts      AIDeletion
	Retention       RetentionReader
	Clock           clock.Clock
	DeletionGrace   time.Duration
	RetentionPolicy domain.RetentionPolicy
}

// Service implements the complete guardian privacy lifecycle.
type Service struct {
	repository      repository.Repository
	parentAccounts  ParentAccountReader
	children        ChildReader
	devices         DeviceReader
	runtime         RuntimeReader
	usage           UsageReader
	aiAccounts      AIDeletion
	retention       RetentionReader
	clock           clock.Clock
	deletionGrace   time.Duration
	retentionPolicy domain.RetentionPolicy
}

// New creates the privacy service.
func New(options Options) (*Service, error) {
	if options.Repository == nil {
		return nil, errors.New("privacy repository is required")
	}
	if options.ParentAccounts == nil {
		return nil, errors.New("privacy parent account reader is required")
	}
	if options.Children == nil {
		return nil, errors.New("privacy child reader is required")
	}
	if options.Devices == nil {
		return nil, errors.New("privacy device reader is required")
	}
	if options.Runtime == nil {
		return nil, errors.New("privacy runtime reader is required")
	}
	if options.Usage == nil {
		return nil, errors.New("privacy usage reader is required")
	}
	if options.AIAccounts == nil {
		return nil, errors.New("privacy AI deletion dependency is required")
	}
	timeSource := options.Clock
	if timeSource == nil {
		timeSource = clock.SystemClock{}
	}
	grace := options.DeletionGrace
	if grace <= 0 {
		grace = domain.DefaultDeletionGracePeriod
	}
	retentionPolicy := options.RetentionPolicy
	if retentionPolicy == (domain.RetentionPolicy{}) {
		retentionPolicy = domain.RetentionPolicy{
			AudioRetentionDays:        0,
			ImageRetentionDays:        0,
			ConversationRetentionDays: 30,
		}
	}
	return &Service{
		repository:      options.Repository,
		parentAccounts:  options.ParentAccounts,
		children:        options.Children,
		devices:         options.Devices,
		runtime:         options.Runtime,
		usage:           options.Usage,
		aiAccounts:      options.AIAccounts,
		retention:       options.Retention,
		clock:           timeSource,
		deletionGrace:   grace,
		retentionPolicy: retentionPolicy,
	}, nil
}

// Status returns the guardian-facing privacy control state.
func (s *Service) Status(
	ctx context.Context,
	parentAccountID string,
) (*domain.Status, error) {
	account, err := s.parentAccounts.GetParentAccountByID(ctx, parentAccountID)
	if err != nil {
		return nil, err
	}
	deletion, err := s.currentDeletion(ctx, parentAccountID)
	if err != nil {
		return nil, err
	}
	exportReceipt, err := s.latestExport(ctx, parentAccountID)
	if err != nil {
		return nil, err
	}
	consents, err := s.repository.ListConsentEvents(ctx, parentAccountID)
	if err != nil {
		return nil, err
	}
	consent := consentStatus(account, consents)
	status := &domain.Status{
		Consent:  consent,
		Deletion: deletion,
		Export: domain.ExportStatus{
			Available: true,
			Last:      exportReceipt,
		},
		Policy: s.effectiveRetention(ctx),
	}
	return status, nil
}

// Export returns a redacted, guardian-owned data copy. Raw audio, images,
// conversations, credentials, provider identifiers, tokens, and password
// hashes are never included.
func (s *Service) Export(
	ctx context.Context,
	parentAccountID string,
) (*domain.DataExport, error) {
	account, err := s.parentAccounts.GetParentAccountByID(ctx, parentAccountID)
	if err != nil {
		return nil, err
	}
	children, err := s.children.ListByFamilyID(ctx, parentAccountID)
	if err != nil {
		return nil, err
	}
	bindings, err := s.devices.ListByParentAccountID(ctx, parentAccountID)
	if err != nil {
		return nil, err
	}
	onlineByDeviceID := map[string]bool{}
	if s.runtime != nil {
		statuses, statusErr := s.runtime.ListDeviceStatuses(ctx, parentAccountID)
		if statusErr != nil {
			return nil, statusErr
		}
		for _, status := range statuses {
			onlineByDeviceID[status.DeviceID] =
				status.Runtime != nil && status.Runtime.IsOnline
		}
	}
	usage, err := s.exportUsage(ctx, parentAccountID)
	if err != nil {
		return nil, err
	}
	consents, err := s.repository.ListConsentEvents(ctx, parentAccountID)
	if err != nil {
		return nil, err
	}

	now := s.clock.Now().UTC()
	result := &domain.DataExport{
		SchemaVersion: exportSchemaVersion,
		ExportedAt:    now,
		Account: domain.ExportAccount{
			ID:                 account.ID,
			Phone:              account.Phone,
			Email:              account.Email,
			DisplayName:        account.DisplayName,
			GuardianFamilyName: account.GuardianFamilyName,
			ChildNickname:      account.ChildNickname,
			ChildBirthday:      account.ChildBirthday,
			Status:             account.Status,
			CreatedAt:          account.CreatedAt,
			LastLoginAt:        account.LastLoginAt,
		},
		Children: make([]domain.ExportChild, 0, len(children)),
		Devices:  make([]domain.ExportDevice, 0, len(bindings)),
		Consents: consents,
		Policy:   s.effectiveRetention(ctx),
	}
	for _, child := range children {
		result.Children = append(result.Children, domain.ExportChild{
			ID:                     child.ID,
			Nickname:               child.Nickname,
			AgeTier:                child.AgeTier,
			Interests:              append([]string(nil), child.Interests...),
			ContentCategories:      append([]string(nil), child.ContentCategories...),
			GuardianConsentVersion: child.GuardianConsentVersion,
			CreatedAt:              child.CreatedAt,
			UpdatedAt:              child.UpdatedAt,
		})
	}
	for _, binding := range bindings {
		result.Devices = append(result.Devices, domain.ExportDevice{
			DeviceID:        binding.DeviceID,
			DeviceName:      binding.DeviceName,
			HardwareModel:   binding.HardwareModel,
			FirmwareVersion: binding.FirmwareVersion,
			CapabilitySet:   append([]string(nil), binding.Capabilities...),
			BoundAt:         binding.BoundAt,
			UpdatedAt:       binding.UpdatedAt,
			Online:          onlineByDeviceID[binding.DeviceID],
		})
	}
	result.Usage = usage

	receipt := &domain.ExportReceipt{
		ID:              uuid.NewString(),
		ParentAccountID: parentAccountID,
		Format:          "json",
		Status:          domain.ExportStatusCompleted,
		ItemCounts: map[string]int{
			"children": len(result.Children),
			"devices":  len(result.Devices),
			"consents": len(result.Consents),
		},
		CreatedAt: now,
		ExpiresAt: now.Add(exportTTL),
	}
	if err := s.repository.CreateExportReceipt(ctx, receipt); err != nil {
		return nil, err
	}
	if err := s.recordAudit(
		ctx,
		parentAccountID,
		"",
		"privacy.export.requested",
		map[string]any{"format": "json"},
	); err != nil {
		return nil, err
	}
	return result, nil
}

// WithdrawConsent records a withdrawal, pauses AI processing, and revokes
// every guardian session so an existing access token cannot keep using the
// withdrawn authorization.
func (s *Service) WithdrawConsent(
	ctx context.Context,
	parentAccountID string,
	consentType string,
	version string,
) (*domain.Status, error) {
	consentType = strings.TrimSpace(consentType)
	version = strings.TrimSpace(version)
	if consentType == "" {
		consentType = domain.ConsentTypeChildDataProcessing
	}
	if version == "" {
		version = defaultConsent
	}
	if !validConsentType(consentType) || version == "" {
		return nil, domain.ErrInvalidConsentVersion
	}
	if _, err := s.parentAccounts.GetParentAccountByID(ctx, parentAccountID); err != nil {
		return nil, err
	}
	if err := s.ensureConsentEvent(
		ctx,
		parentAccountID,
		consentType,
		version,
		false,
		domain.ConsentSourceGuardian,
		map[string]any{"reason": "guardian_withdrawal"},
	); err != nil {
		return nil, err
	}
	if err := s.parentAccounts.RevokeAllSessions(ctx, parentAccountID); err != nil {
		return nil, err
	}
	if err := s.recordAudit(
		ctx,
		parentAccountID,
		"",
		"privacy.consent.withdrawn",
		map[string]any{"consent_type": consentType},
	); err != nil {
		return nil, err
	}
	return s.Status(ctx, parentAccountID)
}

// GrantConsent records a new guardian authorization after a withdrawal.
func (s *Service) GrantConsent(
	ctx context.Context,
	parentAccountID string,
	consentType string,
	version string,
) (*domain.Status, error) {
	consentType = strings.TrimSpace(consentType)
	version = strings.TrimSpace(version)
	if consentType == "" {
		consentType = domain.ConsentTypeChildDataProcessing
	}
	if version == "" {
		version = defaultConsent
	}
	if !validConsentType(consentType) || version == "" {
		return nil, domain.ErrInvalidConsentVersion
	}
	if _, err := s.parentAccounts.GetParentAccountByID(ctx, parentAccountID); err != nil {
		return nil, err
	}
	if err := s.ensureConsentEvent(
		ctx,
		parentAccountID,
		consentType,
		version,
		true,
		domain.ConsentSourceGuardian,
		map[string]any{"reason": "guardian_regrant"},
	); err != nil {
		return nil, err
	}
	if err := s.recordAudit(
		ctx,
		parentAccountID,
		"",
		"privacy.consent.granted",
		map[string]any{"consent_type": consentType},
	); err != nil {
		return nil, err
	}
	return s.Status(ctx, parentAccountID)
}

// RequestDeletion starts the seven-day grace period. The account remains
// usable during the period so the guardian can export data or cancel.
func (s *Service) RequestDeletion(
	ctx context.Context,
	parentAccountID string,
	reason string,
) (*domain.DeletionRequest, error) {
	reason = strings.TrimSpace(reason)
	if len([]rune(reason)) > domain.DefaultDeletionReasonMaxRune {
		return nil, domain.ErrInvalidDeletionReason
	}
	if _, err := s.parentAccounts.GetParentAccountByID(ctx, parentAccountID); err != nil {
		return nil, err
	}
	now := s.clock.Now().UTC()
	request := &domain.DeletionRequest{
		ID:              uuid.NewString(),
		ParentAccountID: parentAccountID,
		Status:          domain.DeletionStatusPending,
		Reason:          reason,
		RequestedAt:     now,
		ExecuteAfter:    now.Add(s.deletionGrace),
		UpdatedAt:       now,
	}
	if err := s.repository.RequestDeletion(ctx, request); err != nil {
		return nil, err
	}
	if err := s.recordAudit(
		ctx,
		parentAccountID,
		"",
		"privacy.deletion.requested",
		map[string]any{"execute_after": request.ExecuteAfter},
	); err != nil {
		return nil, err
	}
	request.Cancellable = true
	request.ScheduledFor = &request.ExecuteAfter
	return request, nil
}

// CancelDeletion cancels the open request and returns the resulting state.
func (s *Service) CancelDeletion(
	ctx context.Context,
	parentAccountID string,
) (*domain.DeletionRequest, error) {
	request, err := s.repository.CancelDeletion(ctx, parentAccountID)
	if err != nil {
		return nil, err
	}
	if err := s.recordAudit(
		ctx,
		parentAccountID,
		"",
		"privacy.deletion.cancelled",
		map[string]any{"request_id": request.ID},
	); err != nil {
		return nil, err
	}
	return request, nil
}

// ExecuteDueDeletions runs one bounded batch of due deletion requests.
func (s *Service) ExecuteDueDeletions(
	ctx context.Context,
	limit int,
) error {
	candidates, err := s.repository.ClaimDueDeletions(ctx, limit)
	if err != nil {
		return err
	}
	for _, candidate := range candidates {
		if err := s.executeDeletion(ctx, candidate); err != nil {
			_ = s.repository.FailDeletion(
				context.Background(),
				candidate.Request.ID,
				safeFailureReason(err),
			)
			continue
		}
	}
	return nil
}

func (s *Service) executeDeletion(
	ctx context.Context,
	candidate domain.DeletionCandidate,
) error {
	parentAccountID := candidate.ParentAccountID
	// Provider deletion happens first. A failure keeps the request in pending
	// so an operator can retry without losing the local audit trail.
	if err := s.aiAccounts.DeleteForParent(ctx, parentAccountID); err != nil {
		return fmt.Errorf("delete provider AI account: %w", err)
	}
	if err := s.purgeLocalData(ctx, parentAccountID); err != nil {
		return err
	}
	if err := s.repository.CompleteDeletion(ctx, candidate.Request.ID); err != nil {
		return err
	}
	return s.recordAuditWithActor(
		ctx,
		"",
		parentAccountID,
		"privacy.deletion.completed",
		map[string]any{"request_id": candidate.Request.ID},
	)
}

func (s *Service) purgeLocalData(
	ctx context.Context,
	parentAccountID string,
) error {
	bindings, err := s.devices.ListByParentAccountID(ctx, parentAccountID)
	if err != nil {
		return err
	}
	for _, binding := range bindings {
		if err := s.devices.RevokeDeviceSessionsAndDeleteBinding(
			ctx,
			parentAccountID,
			binding.DeviceID,
		); err != nil && !errors.Is(err, bindingdomain.ErrDeviceNotFound) {
			return err
		}
	}
	if err := s.repository.PurgeParentData(ctx, parentAccountID); err != nil {
		return err
	}
	return nil
}

// QueryAudit returns a validated administrator audit page.
func (s *Service) QueryAudit(
	ctx context.Context,
	filter domain.AuditFilter,
) (*domain.AuditPage, error) {
	if len([]rune(filter.ActorAccountID)) > domain.MaxAuditActorFilterRunes ||
		len([]rune(filter.TargetAccountID)) > domain.MaxAuditTargetFilterRunes {
		return nil, domain.ErrAuditQueryInvalid
	}
	if filter.From != nil && filter.To != nil && filter.To.Before(*filter.From) {
		return nil, domain.ErrAuditQueryInvalid
	}
	return s.repository.QueryAudit(ctx, filter)
}

// RecordAudit writes one safe administrator or system operation event.
func (s *Service) RecordAudit(
	ctx context.Context,
	actorAccountID string,
	targetAccountID string,
	action string,
	detail map[string]any,
) error {
	return s.recordAuditWithActor(
		ctx,
		actorAccountID,
		targetAccountID,
		action,
		detail,
	)
}

func (s *Service) currentDeletion(
	ctx context.Context,
	parentAccountID string,
) (domain.DeletionRequest, error) {
	current, err := s.repository.CurrentDeletion(ctx, parentAccountID)
	if err != nil {
		return domain.DeletionRequest{}, err
	}
	if current == nil {
		return domain.DeletionRequest{Status: domain.DeletionStatusNone}, nil
	}
	return *current, nil
}

func (s *Service) latestExport(
	ctx context.Context,
	parentAccountID string,
) (*domain.ExportReceipt, error) {
	receipt, err := s.repository.LatestExportReceipt(ctx, parentAccountID)
	if errors.Is(err, domain.ErrAccountUnavailable) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return receipt, nil
}

func (s *Service) exportUsage(
	ctx context.Context,
	parentAccountID string,
) (domain.ExportUsageSummary, error) {
	now := s.clock.Now().UTC()
	from := now.AddDate(0, 0, -maxExportUsageDays)
	usages, err := s.usage.ListByFamily(
		ctx,
		parentAccountID,
		from,
		maxExportUsageDays,
	)
	if err != nil {
		return domain.ExportUsageSummary{}, err
	}
	summary := domain.ExportUsageSummary{Days: len(usages)}
	for _, usage := range usages {
		summary.ConversationCount += int64(usage.ConversationCount)
		summary.ActiveSeconds += int64(usage.ActiveSeconds)
		summary.ConversationSeconds += int64(usage.ConversationSeconds)
		summary.ContentPlayCount += int64(usage.ContentPlayCount)
		summary.ContentSeconds += int64(usage.ContentSeconds)
	}
	return summary, nil
}

func (s *Service) effectiveRetention(
	ctx context.Context,
) domain.RetentionPolicy {
	policy := s.retentionPolicy
	if s.retention == nil {
		return policy
	}
	settings, _, err := s.retention.Settings(ctx)
	if err != nil || settings == nil {
		return policy
	}
	return domain.RetentionPolicy{
		AudioRetentionDays:        settings.Retention.AudioDays,
		ImageRetentionDays:        settings.Retention.ImageDays,
		ConversationRetentionDays: settings.Retention.ConversationDays,
	}
}

func (s *Service) recordAudit(
	ctx context.Context,
	actorAccountID string,
	targetAccountID string,
	action string,
	detail map[string]any,
) error {
	return s.recordAuditWithActor(
		ctx,
		actorAccountID,
		targetAccountID,
		action,
		detail,
	)
}

func (s *Service) recordAuditWithActor(
	ctx context.Context,
	actorAccountID string,
	targetAccountID string,
	action string,
	detail map[string]any,
) error {
	if strings.TrimSpace(action) == "" {
		return domain.ErrAuditQueryInvalid
	}
	safeDetail := make(map[string]any, len(detail))
	for key, value := range detail {
		if safeAuditKey(key) && safeAuditValue(value) {
			safeDetail[key] = value
		}
	}
	return s.repository.AppendAudit(ctx, &domain.AuditEntry{
		ID:              uuid.NewString(),
		ActorAccountID:  actorAccountID,
		TargetAccountID: targetAccountID,
		Action:          action,
		Detail:          safeDetail,
		CreatedAt:       s.clock.Now().UTC(),
	})
}

func (s *Service) ensureConsentEvent(
	ctx context.Context,
	parentAccountID string,
	consentType string,
	version string,
	granted bool,
	source string,
	detail map[string]any,
) error {
	return s.repository.AppendConsentEvent(ctx, &domain.ConsentEvent{
		ID:              uuid.NewString(),
		ParentAccountID: parentAccountID,
		ConsentType:     consentType,
		ConsentVersion:  version,
		Granted:         granted,
		Source:          source,
		Detail:          detail,
		CreatedAt:       s.clock.Now().UTC(),
	})
}

func consentStatus(
	account *authdomain.ParentAccount,
	events []domain.ConsentEvent,
) domain.ConsentStatus {
	status := domain.ConsentStatus{
		Version:       account.GuardianConsentVersion,
		ConsentedAt:   &account.GuardianConsentedAt,
		Status:        domain.ConsentStatusActive,
		Active:        true,
		ReconsentPath: "/settings/privacy",
	}
	for _, event := range events {
		if event.ConsentType != domain.ConsentTypeChildDataProcessing {
			continue
		}
		if event.Granted {
			status.Version = event.ConsentVersion
			status.ConsentedAt = &event.CreatedAt
			status.WithdrawnAt = nil
			status.Status = domain.ConsentStatusActive
			status.Active = true
		} else {
			status.Version = event.ConsentVersion
			status.WithdrawnAt = &event.CreatedAt
			status.Status = domain.ConsentStatusWithdrawn
			status.Active = false
		}
		break
	}
	return status
}

func validConsentType(value string) bool {
	switch value {
	case domain.ConsentTypeGuardianTerms,
		domain.ConsentTypeChildDataProcessing,
		domain.ConsentTypeAIInteraction,
		domain.ConsentTypeEmailContact:
		return true
	default:
		return false
	}
}

func safeAuditKey(key string) bool {
	normalized := strings.ToLower(strings.NewReplacer(
		"-", "",
		"_", "",
		" ", "",
	).Replace(key))
	for _, forbidden := range []string{
		"password",
		"token",
		"secret",
		"apikey",
		"authorization",
		"credential",
		"cookie",
		"body",
		"payload",
		"chat",
		"conversation",
		"transcript",
		"audio",
		"image",
		"video",
		"media",
		"recording",
		"attachment",
		"content",
		"prompt",
	} {
		if strings.Contains(normalized, forbidden) {
			return false
		}
	}
	return true
}

func safeAuditValue(value any) bool {
	switch typed := value.(type) {
	case nil, bool, int, int32, int64, uint, uint32, uint64, float32, float64:
		return true
	case string:
		return len([]rune(typed)) <= 240
	case time.Time:
		return true
	default:
		return false
	}
}

func safeFailureReason(err error) string {
	if err == nil {
		return "unknown failure"
	}
	reason := strings.TrimSpace(err.Error())
	if len([]rune(reason)) > 240 {
		reason = string([]rune(reason)[:240])
	}
	return reason
}
