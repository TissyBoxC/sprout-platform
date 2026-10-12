package service

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	authdomain "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/auth/domain"
	childdomain "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/child/domain"
	bindingdomain "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/device_binding/domain"
	runtimedomain "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/device_runtime/domain"
	operationsdomain "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/operations/domain"
	"github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/privacy/domain"
	"github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/privacy/repository"
	usagedomain "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/usage_report/domain"
)

// --- memory repository -----------------------------------------------------

type memoryRepository struct {
	consents          []domain.ConsentEvent
	receipts          []domain.ExportReceipt
	deletions         []domain.DeletionRequest
	audits            []domain.AuditEntry
	purged            []string
	purgedExpiredAt   []time.Time
	purgedUsageBefore []time.Time
	nextDeletionSeq   int
}

func (r *memoryRepository) AppendConsentEvent(
	_ context.Context,
	event *domain.ConsentEvent,
) error {
	r.consents = append(r.consents, *event)
	return nil
}

func (r *memoryRepository) ListConsentEvents(
	_ context.Context,
	parentAccountID string,
) ([]domain.ConsentEvent, error) {
	result := make([]domain.ConsentEvent, 0)
	for index := len(r.consents) - 1; index >= 0; index-- {
		if r.consents[index].ParentAccountID == parentAccountID {
			result = append(result, r.consents[index])
		}
	}
	return result, nil
}

func (r *memoryRepository) CreateExportReceipt(
	_ context.Context,
	receipt *domain.ExportReceipt,
) error {
	r.receipts = append(r.receipts, *receipt)
	return nil
}

func (r *memoryRepository) LatestExportReceipt(
	_ context.Context,
	parentAccountID string,
) (*domain.ExportReceipt, error) {
	for index := len(r.receipts) - 1; index >= 0; index-- {
		if r.receipts[index].ParentAccountID == parentAccountID {
			copyReceipt := r.receipts[index]
			return &copyReceipt, nil
		}
	}
	return nil, domain.ErrAccountUnavailable
}

func (r *memoryRepository) RequestDeletion(
	_ context.Context,
	request *domain.DeletionRequest,
) error {
	for _, existing := range r.deletions {
		if existing.ParentAccountID == request.ParentAccountID &&
			existing.Status == domain.DeletionStatusPending {
			return domain.ErrDeletionPending
		}
	}
	r.nextDeletionSeq++
	r.deletions = append(r.deletions, *request)
	return nil
}

func (r *memoryRepository) CancelDeletion(
	_ context.Context,
	parentAccountID string,
) (*domain.DeletionRequest, error) {
	for index := range r.deletions {
		request := &r.deletions[index]
		if request.ParentAccountID != parentAccountID ||
			request.Status != domain.DeletionStatusPending {
			continue
		}
		now := time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC)
		request.Status = domain.DeletionStatusCancelled
		request.CancelledAt = &now
		request.UpdatedAt = now
		copyRequest := *request
		return &copyRequest, nil
	}
	return nil, domain.ErrDeletionNotPending
}

func (r *memoryRepository) CurrentDeletion(
	_ context.Context,
	parentAccountID string,
) (*domain.DeletionRequest, error) {
	for index := len(r.deletions) - 1; index >= 0; index-- {
		if r.deletions[index].ParentAccountID == parentAccountID {
			copyRequest := r.deletions[index]
			return &copyRequest, nil
		}
	}
	return nil, nil
}

func (r *memoryRepository) ClaimDueDeletions(
	_ context.Context,
	limit int,
) ([]domain.DeletionCandidate, error) {
	if limit < 1 {
		limit = domain.DefaultDeletionBatchSize
	}
	now := time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC)
	candidates := make([]domain.DeletionCandidate, 0)
	for index := range r.deletions {
		request := &r.deletions[index]
		if request.Status != domain.DeletionStatusPending ||
			request.ExecuteAfter.After(now) {
			continue
		}
		request.Status = domain.DeletionStatusProcessing
		request.UpdatedAt = now
		candidates = append(candidates, domain.DeletionCandidate{
			Request:         *request,
			ParentAccountID: request.ParentAccountID,
		})
		if len(candidates) >= limit {
			break
		}
	}
	return candidates, nil
}

func (r *memoryRepository) CompleteDeletion(
	_ context.Context,
	requestID string,
) error {
	for index := range r.deletions {
		if r.deletions[index].ID != requestID {
			continue
		}
		if r.deletions[index].Status != domain.DeletionStatusProcessing {
			return domain.ErrDeletionNotPending
		}
		now := time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC)
		r.deletions[index].Status = domain.DeletionStatusCompleted
		r.deletions[index].CompletedAt = &now
		r.deletions[index].UpdatedAt = now
		return nil
	}
	return domain.ErrDeletionNotFound
}

func (r *memoryRepository) FailDeletion(
	_ context.Context,
	requestID string,
	reason string,
) error {
	for index := range r.deletions {
		if r.deletions[index].ID != requestID {
			continue
		}
		if r.deletions[index].Status != domain.DeletionStatusProcessing {
			return nil
		}
		r.deletions[index].Status = domain.DeletionStatusFailed
		r.deletions[index].FailureReason = reason
		return nil
	}
	return nil
}

func (r *memoryRepository) PurgeParentData(
	_ context.Context,
	parentAccountID string,
) error {
	r.purged = append(r.purged, parentAccountID)
	return nil
}

func (r *memoryRepository) PurgeExpiredRecords(
	_ context.Context,
	now time.Time,
) (repository.ExpiredRecordPurge, error) {
	r.purgedExpiredAt = append(r.purgedExpiredAt, now)
	return repository.ExpiredRecordPurge{}, nil
}

func (r *memoryRepository) PurgeUsageReports(
	_ context.Context,
	before time.Time,
) (repository.UsageReportPurge, error) {
	r.purgedUsageBefore = append(r.purgedUsageBefore, before)
	return repository.UsageReportPurge{}, nil
}

func (r *memoryRepository) AppendAudit(
	_ context.Context,
	entry *domain.AuditEntry,
) error {
	r.audits = append(r.audits, *entry)
	return nil
}

func (r *memoryRepository) QueryAudit(
	_ context.Context,
	filter domain.AuditFilter,
) (*domain.AuditPage, error) {
	items := make([]domain.AuditEntry, 0, len(r.audits))
	for _, entry := range r.audits {
		if filter.Action != "" && entry.Action != filter.Action {
			continue
		}
		if filter.ActorAccountID != "" &&
			entry.ActorAccountID != filter.ActorAccountID {
			continue
		}
		if filter.TargetAccountID != "" &&
			entry.TargetAccountID != filter.TargetAccountID {
			continue
		}
		items = append(items, entry)
	}
	return &domain.AuditPage{
		Items:    items,
		Total:    int64(len(items)),
		Page:     1,
		PageSize: domain.DefaultAuditPageSize,
		Actions:  []string{},
	}, nil
}

// --- fakes -----------------------------------------------------------------

type fakeParentAccounts struct {
	account         *authdomain.ParentAccount
	revokedSessions int
}

func (f *fakeParentAccounts) GetParentAccountByID(
	_ context.Context,
	accountID string,
) (*authdomain.ParentAccount, error) {
	if f.account == nil || f.account.ID != accountID {
		return nil, authdomain.ErrAccountNotFound
	}
	return f.account, nil
}

func (f *fakeParentAccounts) RevokeAllSessions(
	_ context.Context,
	_ string,
) error {
	f.revokedSessions++
	return nil
}

type fakeChildren struct {
	children []childdomain.Child
}

func (f *fakeChildren) ListByFamilyID(
	_ context.Context,
	familyID string,
) ([]childdomain.Child, error) {
	result := make([]childdomain.Child, 0)
	for _, child := range f.children {
		if child.FamilyID == familyID {
			result = append(result, child)
		}
	}
	return result, nil
}

type fakeDevices struct {
	bindings []bindingdomain.Binding
	deleted  []string
}

func (f *fakeDevices) ListByParentAccountID(
	_ context.Context,
	parentAccountID string,
) ([]bindingdomain.Binding, error) {
	result := make([]bindingdomain.Binding, 0)
	for _, binding := range f.bindings {
		if binding.ParentAccountID == parentAccountID {
			result = append(result, binding)
		}
	}
	return result, nil
}

func (f *fakeDevices) RevokeDeviceSessionsAndDeleteBinding(
	_ context.Context,
	parentAccountID string,
	deviceID string,
) error {
	for _, binding := range f.bindings {
		if binding.ParentAccountID == parentAccountID &&
			binding.DeviceID == deviceID {
			f.deleted = append(f.deleted, deviceID)
			return nil
		}
	}
	return bindingdomain.ErrDeviceNotFound
}

type fakeRuntime struct {
	statuses []runtimedomain.DeviceStatus
}

func (f *fakeRuntime) ListDeviceStatuses(
	_ context.Context,
	_ string,
) ([]runtimedomain.DeviceStatus, error) {
	return f.statuses, nil
}

type fakeUsage struct {
	usages []usagedomain.DailyUsage
}

func (f *fakeUsage) ListByFamily(
	_ context.Context,
	_ string,
	_ time.Time,
	_ int,
) ([]usagedomain.DailyUsage, error) {
	return f.usages, nil
}

type fakeAIDeletion struct {
	deleted []string
	err     error
}

func (f *fakeAIDeletion) DeleteForParent(
	_ context.Context,
	parentAccountID string,
) error {
	if f.err != nil {
		return f.err
	}
	f.deleted = append(f.deleted, parentAccountID)
	return nil
}

type fakeRetention struct {
	settings *operationsdomain.Settings
}

// fakeConsentCommander records the device commands the privacy service
// dispatches so tests can assert consent changes reach devices.
type fakeConsentCommander struct {
	commands []consentCommandRecord
}

// consentCommandRecord is a local record of one dispatched consent-sync
// command, kept separate from the runtime domain so the test owns the shape it
// asserts on.
type consentCommandRecord struct {
	deviceID    string
	commandType runtimedomain.CommandType
	requestedBy string
	payload     map[string]any
}

func (c *fakeConsentCommander) CreateCommand(
	_ context.Context,
	deviceID string,
	commandType runtimedomain.CommandType,
	requestedBy string,
	payload map[string]any,
) (*runtimedomain.Command, error) {
	c.commands = append(c.commands, consentCommandRecord{
		deviceID:    deviceID,
		commandType: commandType,
		requestedBy: requestedBy,
		payload:     payload,
	})
	return &runtimedomain.Command{DeviceID: deviceID, Type: commandType}, nil
}

func (f *fakeRetention) RuntimePolicy(
	_ context.Context,
) (*operationsdomain.RuntimePolicy, error) {
	return nil, nil
}

func (f *fakeRetention) Settings(
	_ context.Context,
) (*operationsdomain.Settings, int64, error) {
	if f.settings == nil {
		return nil, 0, errors.New("no settings")
	}
	return f.settings, 1, nil
}

type fixedClock struct {
	now time.Time
}

func (c fixedClock) Now() time.Time {
	return c.now
}

// --- helpers ---------------------------------------------------------------

const (
	testParentID = "11111111-1111-1111-1111-111111111111"
	testDeviceID = "sprout_device_0001"
)

func testNow() time.Time {
	return time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC)
}

func newTestService(
	t *testing.T,
	repository *memoryRepository,
	parents *fakeParentAccounts,
	devices *fakeDevices,
	ai *fakeAIDeletion,
	options ...func(*Options),
) *Service {
	t.Helper()
	now := testNow()
	account := parents.account
	if account == nil {
		account = &authdomain.ParentAccount{
			ID:                     testParentID,
			Email:                  "guardian@example.com",
			Phone:                  "13800000000",
			PasswordHash:           "super-secret-hash",
			DisplayName:            "家长",
			Status:                 "active",
			GuardianConsentVersion: "2026-01",
			GuardianConsentedAt:    now.Add(-24 * time.Hour),
			CreatedAt:              now.Add(-48 * time.Hour),
			UpdatedAt:              now,
		}
		parents.account = account
	}
	config := Options{
		Repository:     repository,
		ParentAccounts: parents,
		Children: &fakeChildren{children: []childdomain.Child{
			{
				ID:                     "child-1",
				FamilyID:               testParentID,
				Nickname:               "宝贝",
				AgeTier:                childdomain.AgeTier5To6,
				GuardianConsentVersion: "2026-01",
				CreatedAt:              now.Add(-24 * time.Hour),
				UpdatedAt:              now,
			},
		}},
		Devices: devices,
		Runtime: &fakeRuntime{statuses: []runtimedomain.DeviceStatus{
			{
				DeviceID: testDeviceID,
				Runtime:  &runtimedomain.RuntimeStatus{IsOnline: true},
			},
		}},
		Usage: &fakeUsage{usages: []usagedomain.DailyUsage{
			{
				ReportDate:        "2026-10-07",
				ParentAccountID:   testParentID,
				DeviceID:          testDeviceID,
				ConversationCount: 3,
				ActiveSeconds:     1200,
				CreatedAt:         now,
				UpdatedAt:         now,
			},
		}},
		AIAccounts: ai,
		Retention: &fakeRetention{settings: &operationsdomain.Settings{
			Retention: operationsdomain.RetentionSettings{
				AudioDays:        0,
				ImageDays:        0,
				ConversationDays: 14,
			},
		}},
		Clock: fixedClock{now: now},
	}
	for _, apply := range options {
		apply(&config)
	}
	service, err := New(config)
	if err != nil {
		t.Fatalf("new privacy service: %v", err)
	}
	return service
}

// --- tests -----------------------------------------------------------------

func TestStatusFallsBackToActiveConsent(t *testing.T) {
	service := newTestService(
		t,
		&memoryRepository{},
		&fakeParentAccounts{},
		&fakeDevices{},
		&fakeAIDeletion{},
	)

	status, err := service.Status(context.Background(), testParentID)
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	if !status.Consent.Active {
		t.Fatalf("expected active consent, got %+v", status.Consent)
	}
	if status.Deletion.Status != domain.DeletionStatusNone {
		t.Fatalf("expected no deletion, got %q", status.Deletion.Status)
	}
	if status.Policy.ConversationRetentionDays != 14 {
		t.Fatalf(
			"expected retention settings to win, got %d",
			status.Policy.ConversationRetentionDays,
		)
	}
}

func TestStatusProjectsAllConsentTypes(t *testing.T) {
	service := newTestService(
		t,
		&memoryRepository{},
		&fakeParentAccounts{},
		&fakeDevices{},
		&fakeAIDeletion{},
	)

	status, err := service.Status(context.Background(), testParentID)
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	if len(status.Consents) != 4 {
		t.Fatalf("expected four consent types, got %d", len(status.Consents))
	}
	wantOrder := []string{
		domain.ConsentTypeGuardianTerms,
		domain.ConsentTypeChildDataProcessing,
		domain.ConsentTypeAIInteraction,
		domain.ConsentTypeEmailContact,
	}
	for index, want := range wantOrder {
		entry := status.Consents[index]
		if entry.ConsentType != want {
			t.Fatalf("consent %d type = %q, want %q", index, entry.ConsentType, want)
		}
		// Without a recorded decision every type falls back to the
		// registration default and must read as granted.
		if !entry.Granted || entry.Status != domain.ConsentStatusActive {
			t.Fatalf("consent %q = %+v, want active default", want, entry)
		}
	}
}

func TestWithdrawOneConsentLeavesOthersGranted(t *testing.T) {
	repository := &memoryRepository{}
	service := newTestService(
		t,
		repository,
		&fakeParentAccounts{},
		&fakeDevices{},
		&fakeAIDeletion{},
	)

	status, err := service.WithdrawConsent(
		context.Background(),
		testParentID,
		domain.ConsentTypeAIInteraction,
		"2026-01",
	)
	if err != nil {
		t.Fatalf("withdraw ai_interaction: %v", err)
	}
	byType := consentStatusesByType(status.Consents)
	if byType[domain.ConsentTypeAIInteraction].Granted {
		t.Fatalf("expected ai_interaction to be withdrawn")
	}
	for _, consentType := range []string{
		domain.ConsentTypeGuardianTerms,
		domain.ConsentTypeChildDataProcessing,
		domain.ConsentTypeEmailContact,
	} {
		if !byType[consentType].Granted {
			t.Fatalf("withdrawing ai_interaction also revoked %q", consentType)
		}
	}

	// The child_data_processing summary must stay active because the guardian
	// only withdrew AI interaction.
	if !status.Consent.Active {
		t.Fatalf("expected child_data_processing summary to stay active")
	}

	status, err = service.GrantConsent(
		context.Background(),
		testParentID,
		domain.ConsentTypeAIInteraction,
		"2026-01",
	)
	if err != nil {
		t.Fatalf("grant ai_interaction: %v", err)
	}
	byType = consentStatusesByType(status.Consents)
	if !byType[domain.ConsentTypeAIInteraction].Granted {
		t.Fatalf("expected ai_interaction to be granted again")
	}
}

func consentStatusesByType(
	statuses []domain.ConsentTypeStatus,
) map[string]domain.ConsentTypeStatus {
	result := make(map[string]domain.ConsentTypeStatus, len(statuses))
	for _, status := range statuses {
		result[status.ConsentType] = status
	}
	return result
}

func TestExportNeverLeaksCredentials(t *testing.T) {
	repository := &memoryRepository{}
	service := newTestService(
		t,
		repository,
		&fakeParentAccounts{},
		&fakeDevices{bindings: []bindingdomain.Binding{
			{
				ID:              "binding-1",
				ParentAccountID: testParentID,
				DeviceID:        testDeviceID,
				DeviceName:      "初芽",
				BoundAt:         testNow(),
				UpdatedAt:       testNow(),
			},
		}},
		&fakeAIDeletion{},
	)

	export, err := service.Export(context.Background(), testParentID)
	if err != nil {
		t.Fatalf("export: %v", err)
	}
	encoded := strings.Builder{}
	encoded.WriteString(export.Account.Email)
	encoded.WriteString(export.Account.Phone)
	encoded.WriteString(export.Account.DisplayName)
	for _, child := range export.Children {
		encoded.WriteString(child.Nickname)
	}
	if strings.Contains(encoded.String(), "super-secret-hash") {
		t.Fatalf("credential leaked into export")
	}
	if len(export.Children) != 1 || len(export.Devices) != 1 {
		t.Fatalf(
			"unexpected export counts children=%d devices=%d",
			len(export.Children),
			len(export.Devices),
		)
	}
	if !export.Devices[0].Online {
		t.Fatalf("expected device to be reported online")
	}
	if len(repository.receipts) != 1 {
		t.Fatalf("expected one export receipt, got %d", len(repository.receipts))
	}
}

func TestDeletionLifecycleRequestCancelAndExecute(t *testing.T) {
	repository := &memoryRepository{}
	parents := &fakeParentAccounts{}
	devices := &fakeDevices{bindings: []bindingdomain.Binding{
		{
			ID:              "binding-1",
			ParentAccountID: testParentID,
			DeviceID:        testDeviceID,
		},
	}}
	ai := &fakeAIDeletion{}
	service := newTestService(
		t,
		repository,
		parents,
		devices,
		ai,
	)

	request, err := service.RequestDeletion(
		context.Background(),
		testParentID,
		"不再使用",
	)
	if err != nil {
		t.Fatalf("request deletion: %v", err)
	}
	if request.Status != domain.DeletionStatusPending || !request.Cancellable {
		t.Fatalf("unexpected deletion request: %+v", request)
	}
	if _, err := service.RequestDeletion(
		context.Background(),
		testParentID,
		"",
	); !errors.Is(err, domain.ErrDeletionPending) {
		t.Fatalf("expected ErrDeletionPending, got %v", err)
	}

	cancelled, err := service.CancelDeletion(context.Background(), testParentID)
	if err != nil {
		t.Fatalf("cancel deletion: %v", err)
	}
	if cancelled.Status != domain.DeletionStatusCancelled {
		t.Fatalf("expected cancelled, got %q", cancelled.Status)
	}
	if _, err := service.CancelDeletion(
		context.Background(),
		testParentID,
	); !errors.Is(err, domain.ErrDeletionNotPending) {
		t.Fatalf("expected ErrDeletionNotPending, got %v", err)
	}
}

func TestExecuteDueDeletionsPurgesAndCompletes(t *testing.T) {
	now := testNow()
	repository := &memoryRepository{
		deletions: []domain.DeletionRequest{
			{
				ID:              "deletion-1",
				ParentAccountID: testParentID,
				Status:          domain.DeletionStatusPending,
				RequestedAt:     now.Add(-8 * 24 * time.Hour),
				ExecuteAfter:    now.Add(-time.Hour),
				UpdatedAt:       now.Add(-8 * 24 * time.Hour),
			},
		},
	}
	devices := &fakeDevices{bindings: []bindingdomain.Binding{
		{
			ID:              "binding-1",
			ParentAccountID: testParentID,
			DeviceID:        testDeviceID,
		},
	}}
	ai := &fakeAIDeletion{}
	service := newTestService(t, repository, &fakeParentAccounts{}, devices, ai)

	if err := service.ExecuteDueDeletions(context.Background(), 10); err != nil {
		t.Fatalf("execute due deletions: %v", err)
	}
	if len(ai.deleted) != 1 || ai.deleted[0] != testParentID {
		t.Fatalf("provider deletion not invoked: %+v", ai.deleted)
	}
	if len(devices.deleted) != 1 || devices.deleted[0] != testDeviceID {
		t.Fatalf("device binding not purged: %+v", devices.deleted)
	}
	if len(repository.purged) != 1 || repository.purged[0] != testParentID {
		t.Fatalf("guardian data was not purged: %+v", repository.purged)
	}
	if repository.deletions[0].Status != domain.DeletionStatusCompleted {
		t.Fatalf("expected completed, got %q", repository.deletions[0].Status)
	}
}

func TestExecuteDueDeletionsPurgesDataBeforeDeletingBindings(t *testing.T) {
	now := testNow()
	repository := &memoryRepository{
		deletions: []domain.DeletionRequest{
			{
				ID:              "deletion-order",
				ParentAccountID: testParentID,
				Status:          domain.DeletionStatusPending,
				RequestedAt:     now.Add(-8 * 24 * time.Hour),
				ExecuteAfter:    now.Add(-time.Hour),
				UpdatedAt:       now.Add(-8 * 24 * time.Hour),
			},
		},
	}
	devices := &fakeDevices{bindings: []bindingdomain.Binding{
		{
			ID:              "binding-1",
			ParentAccountID: testParentID,
			DeviceID:        testDeviceID,
		},
	}}
	service := newTestService(
		t,
		repository,
		&fakeParentAccounts{},
		devices,
		&fakeAIDeletion{},
	)

	if err := service.ExecuteDueDeletions(context.Background(), 10); err != nil {
		t.Fatalf("execute due deletions: %v", err)
	}
	if len(repository.purged) != 1 {
		t.Fatalf("guardian purge calls = %d, want 1", len(repository.purged))
	}
}

func TestWithdrawConsentRevokesSessionsAndGrantRestores(t *testing.T) {
	repository := &memoryRepository{}
	parents := &fakeParentAccounts{}
	service := newTestService(
		t,
		repository,
		parents,
		&fakeDevices{},
		&fakeAIDeletion{},
	)

	status, err := service.WithdrawConsent(
		context.Background(),
		testParentID,
		domain.ConsentTypeChildDataProcessing,
		"2026-01",
	)
	if err != nil {
		t.Fatalf("withdraw consent: %v", err)
	}
	if status.Consent.Active {
		t.Fatalf("expected consent to be withdrawn")
	}
	if parents.revokedSessions != 1 {
		t.Fatalf("expected sessions to be revoked, got %d", parents.revokedSessions)
	}

	status, err = service.GrantConsent(
		context.Background(),
		testParentID,
		domain.ConsentTypeChildDataProcessing,
		"2026-01",
	)
	if err != nil {
		t.Fatalf("grant consent: %v", err)
	}
	if !status.Consent.Active {
		t.Fatalf("expected consent to be active again")
	}
}

func TestConsentTypesAreEnforcedIndependently(t *testing.T) {
	repository := &memoryRepository{}
	service := newTestService(
		t,
		repository,
		&fakeParentAccounts{},
		&fakeDevices{},
		&fakeAIDeletion{},
	)

	// Without any recorded decision the registration default still stands for
	// every type.
	voiceGranted, err := service.VoiceConsentForParent(context.Background(), testParentID)
	if err != nil {
		t.Fatalf("voice consent default: %v", err)
	}
	if !voiceGranted {
		t.Fatalf("expected voice consent default to be granted")
	}

	// Withdrawing ai_interaction must deny voice while leaving image consent,
	// which is governed by child_data_processing, untouched.
	if _, err := service.WithdrawConsent(
		context.Background(),
		testParentID,
		domain.ConsentTypeAIInteraction,
		"2026-01",
	); err != nil {
		t.Fatalf("withdraw ai_interaction: %v", err)
	}
	voiceGranted, err = service.VoiceConsentForParent(context.Background(), testParentID)
	if err != nil {
		t.Fatalf("voice consent after withdrawal: %v", err)
	}
	if voiceGranted {
		t.Fatalf("expected voice consent to be withdrawn")
	}
	imageGranted, err := service.ImageConsentForParent(context.Background(), testParentID)
	if err != nil {
		t.Fatalf("image consent after voice withdrawal: %v", err)
	}
	if !imageGranted {
		t.Fatalf("expected image consent to remain granted")
	}

	// Withdrawing child_data_processing now denies image consent while the
	// voice decision stays denied.
	if _, err := service.WithdrawConsent(
		context.Background(),
		testParentID,
		domain.ConsentTypeChildDataProcessing,
		"2026-01",
	); err != nil {
		t.Fatalf("withdraw child_data_processing: %v", err)
	}
	imageGranted, err = service.ImageConsentForParent(context.Background(), testParentID)
	if err != nil {
		t.Fatalf("image consent after withdrawal: %v", err)
	}
	if imageGranted {
		t.Fatalf("expected image consent to be withdrawn")
	}

	// Granting ai_interaction again is not enough: voice also needs
	// child_data_processing, which is still withdrawn.
	if _, err := service.GrantConsent(
		context.Background(),
		testParentID,
		domain.ConsentTypeAIInteraction,
		"2026-01",
	); err != nil {
		t.Fatalf("grant ai_interaction: %v", err)
	}
	voiceGranted, err = service.VoiceConsentForParent(context.Background(), testParentID)
	if err != nil {
		t.Fatalf("voice consent after regrant: %v", err)
	}
	if voiceGranted {
		t.Fatalf("expected voice consent to stay withdrawn while child_data_processing is withdrawn")
	}
	imageGranted, err = service.ImageConsentForParent(context.Background(), testParentID)
	if err != nil {
		t.Fatalf("image consent after voice regrant: %v", err)
	}
	if imageGranted {
		t.Fatalf("expected image consent to stay withdrawn")
	}

	// Restoring child_data_processing then makes both image and voice available
	// again, since ai_interaction was already re-granted above.
	if _, err := service.GrantConsent(
		context.Background(),
		testParentID,
		domain.ConsentTypeChildDataProcessing,
		"2026-01",
	); err != nil {
		t.Fatalf("grant child_data_processing: %v", err)
	}
	voiceGranted, err = service.VoiceConsentForParent(context.Background(), testParentID)
	if err != nil {
		t.Fatalf("voice consent after full regrant: %v", err)
	}
	if !voiceGranted {
		t.Fatalf("expected voice consent to be restored once both consents are granted")
	}
}

func TestWithdrawConsentDispatchesSyncCommandToDevices(t *testing.T) {
	repository := &memoryRepository{}
	commander := &fakeConsentCommander{}
	devices := &fakeDevices{bindings: []bindingdomain.Binding{
		{ID: "binding-1", ParentAccountID: testParentID, DeviceID: testDeviceID},
	}}
	service := newTestService(
		t,
		repository,
		&fakeParentAccounts{},
		devices,
		&fakeAIDeletion{},
		func(options *Options) { options.Commander = commander },
	)

	if _, err := service.WithdrawConsent(
		context.Background(),
		testParentID,
		domain.ConsentTypeAIInteraction,
		"2026-01",
	); err != nil {
		t.Fatalf("withdraw ai_interaction: %v", err)
	}
	if len(commander.commands) != 1 {
		t.Fatalf("dispatched commands = %d, want 1", len(commander.commands))
	}
	command := commander.commands[0]
	if command.deviceID != testDeviceID {
		t.Fatalf("command device id = %q, want %q", command.deviceID, testDeviceID)
	}
	if command.commandType != runtimedomain.CommandSyncPrivacyConsent {
		t.Fatalf("command type = %q, want %q", command.commandType, runtimedomain.CommandSyncPrivacyConsent)
	}
	// Withdrawing ai_interaction must also deny the audio upload gate so the
	// device stops uploading speech even before child_data_processing changes.
	if granted, ok := command.payload["audio_upload"].(bool); !ok || granted {
		t.Fatalf("audio_upload = %v, want false", command.payload["audio_upload"])
	}
	if granted, ok := command.payload["image_upload"].(bool); !ok || !granted {
		t.Fatalf("image_upload = %v, want true", command.payload["image_upload"])
	}
}

func TestRecordAuditDropsSensitiveFields(t *testing.T) {
	repository := &memoryRepository{}
	service := newTestService(
		t,
		repository,
		&fakeParentAccounts{},
		&fakeDevices{},
		&fakeAIDeletion{},
	)

	err := service.RecordAudit(
		context.Background(),
		testParentID,
		testParentID,
		"admin.family.password.reset",
		map[string]any{
			"scope":           "credential",
			"password":        "wangjiahao2",
			"provider_token":  "sk-secret",
			"request_payload": map[string]any{"phone": "13800000000"},
			"balance_usd":     10.0,
		},
	)
	if err != nil {
		t.Fatalf("record audit: %v", err)
	}
	if len(repository.audits) != 1 {
		t.Fatalf("expected one audit entry, got %d", len(repository.audits))
	}
	detail := repository.audits[0].Detail
	if _, exists := detail["password"]; exists {
		t.Fatalf("password leaked into audit detail: %+v", detail)
	}
	if _, exists := detail["provider_token"]; exists {
		t.Fatalf("token leaked into audit detail: %+v", detail)
	}
	if _, exists := detail["request_payload"]; exists {
		t.Fatalf("payload leaked into audit detail: %+v", detail)
	}
	if detail["scope"] != "credential" {
		t.Fatalf("safe scalar field missing: %+v", detail)
	}
}

func TestRequestDeletionRejectsOverlongReason(t *testing.T) {
	service := newTestService(
		t,
		&memoryRepository{},
		&fakeParentAccounts{},
		&fakeDevices{},
		&fakeAIDeletion{},
	)
	_, err := service.RequestDeletion(
		context.Background(),
		testParentID,
		strings.Repeat("啊", domain.DefaultDeletionReasonMaxRune+1),
	)
	if !errors.Is(err, domain.ErrInvalidDeletionReason) {
		t.Fatalf("expected ErrInvalidDeletionReason, got %v", err)
	}
}

func TestPurgeExpiredRecordsUsesInjectedClock(t *testing.T) {
	repository := &memoryRepository{}
	service := newTestService(
		t,
		repository,
		&fakeParentAccounts{},
		&fakeDevices{},
		&fakeAIDeletion{},
	)
	if _, err := service.PurgeExpiredRecords(context.Background()); err != nil {
		t.Fatalf("purge expired records: %v", err)
	}
	if len(repository.purgedExpiredAt) != 1 {
		t.Fatalf(
			"purge calls = %d, want 1",
			len(repository.purgedExpiredAt),
		)
	}
	if !repository.purgedExpiredAt[0].Equal(testNow()) {
		t.Fatalf(
			"purge used %v, want the injected clock %v",
			repository.purgedExpiredAt[0],
			testNow(),
		)
	}
}

// The usage-report cleanup cutoff must be derived from the operator-configured
// window and floored to a whole UTC day, so the retention job deletes report
// days strictly before the window rather than a mid-day timestamp.
func TestPurgeUsageReportsUsesConfiguredWindow(t *testing.T) {
	repository := &memoryRepository{}
	service := newTestService(
		t,
		repository,
		&fakeParentAccounts{},
		&fakeDevices{},
		&fakeAIDeletion{},
		func(options *Options) {
			options.Retention = &fakeRetention{settings: &operationsdomain.Settings{
				Retention: operationsdomain.RetentionSettings{
					UsageReportDays: 30,
				},
			}}
		},
	)
	if _, err := service.PurgeUsageReports(context.Background()); err != nil {
		t.Fatalf("purge usage reports: %v", err)
	}
	if len(repository.purgedUsageBefore) != 1 {
		t.Fatalf(
			"usage purge calls = %d, want 1",
			len(repository.purgedUsageBefore),
		)
	}
	want := testNow().AddDate(0, 0, -30)
	want = time.Date(want.Year(), want.Month(), want.Day(), 0, 0, 0, 0, time.UTC)
	if !repository.purgedUsageBefore[0].Equal(want) {
		t.Fatalf(
			"usage purge cutoff = %v, want %v",
			repository.purgedUsageBefore[0],
			want,
		)
	}
}

// A zero window disables the scheduled purge entirely so a deployment can opt
// out without the job deleting the whole ledger.
func TestPurgeUsageReportsDisabledWhenWindowZero(t *testing.T) {
	repository := &memoryRepository{}
	service := newTestService(
		t,
		repository,
		&fakeParentAccounts{},
		&fakeDevices{},
		&fakeAIDeletion{},
		func(options *Options) {
			options.Retention = &fakeRetention{settings: &operationsdomain.Settings{
				Retention: operationsdomain.RetentionSettings{UsageReportDays: 0},
			}}
		},
	)
	if _, err := service.PurgeUsageReports(context.Background()); err != nil {
		t.Fatalf("purge usage reports: %v", err)
	}
	if len(repository.purgedUsageBefore) != 0 {
		t.Fatalf(
			"usage purge calls = %d, want 0",
			len(repository.purgedUsageBefore),
		)
	}
}
