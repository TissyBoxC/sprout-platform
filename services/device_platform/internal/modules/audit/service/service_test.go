package service

import (
	"context"
	"testing"
	"time"

	"github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/audit/domain"
	"github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/audit/repository"
)

type stubRepository struct {
	saved             *domain.Diagnostics
	get               *domain.Snapshot
	savedProvisioning *domain.Provisioning
	getProvisioning   *domain.ProvisioningSnapshot
	err               error
}

func (stub *stubRepository) SaveDiagnostics(
	_ context.Context,
	_ string,
	_ time.Time,
	diagnostics *domain.Diagnostics,
) error {
	stub.saved = diagnostics
	return stub.err
}

func (stub *stubRepository) GetDiagnostics(
	_ context.Context,
	_ string,
	_ int,
) (*domain.Snapshot, error) {
	return stub.get, stub.err
}

func (stub *stubRepository) SaveProvisioning(
	_ context.Context,
	_ string,
	_ time.Time,
	provisioning *domain.Provisioning,
) error {
	stub.savedProvisioning = provisioning
	return stub.err
}

func (stub *stubRepository) GetProvisioning(
	_ context.Context,
	_ string,
	_ int,
) (*domain.ProvisioningSnapshot, error) {
	return stub.getProvisioning, stub.err
}

var _ repository.Repository = (*stubRepository)(nil)

func TestValidateAcceptsCanonicalDiagnostics(t *testing.T) {
	service := &Service{}
	diagnostics := &domain.Diagnostics{
		SchemaVersion:     "1.0.0",
		NewestSequence:    13,
		DroppedBootEvents: 0,
		BootEvents: []domain.BootEvent{
			{
				EventType:       domain.EventTypeBoot,
				EventID:         "boot_00000001",
				Sequence:        10,
				UptimeMS:        1200,
				BootCount:       3,
				ResetReason:     "power_on",
				FirmwareVersion: "0.4.0",
			},
		},
		LatestFailure: &domain.ModuleFailure{
			EventType:       domain.EventTypeFailure,
			EventID:         "failure_00000011",
			Sequence:        11,
			ModuleName:      "network_manager",
			ErrorCode:       "ESP_ERR_TIMEOUT",
			FailureCount:    1,
			FirmwareVersion: "0.4.0",
		},
		RecoveryEvents: []domain.RecoveryEvent{
			{
				EventType:       domain.EventTypeRecovered,
				EventID:         "recovery_00000012",
				Sequence:        12,
				ModuleName:      "network_manager",
				FirmwareVersion: "0.4.0",
			},
		},
		InteractionEvents: []domain.InteractionEvent{
			{
				EventType:       domain.InteractionEventButtonGesture,
				EventID:         "interaction_00000013",
				Sequence:        13,
				DetailCode:      "long_press",
				DurationMS:      1800,
				FirmwareVersion: "0.4.0",
			},
		},
	}

	normalized, err := service.Validate("sprout_device_001", diagnostics)
	if err != nil {
		t.Fatalf("expected validation to succeed, got %v", err)
	}
	if normalized == nil || normalized.NewestSequence != 13 {
		t.Fatalf("unexpected normalized diagnostics: %+v", normalized)
	}
	if normalized.SchemaVersion != "1.0.0" {
		t.Fatalf("expected schema version to be preserved, got %q", normalized.SchemaVersion)
	}
}

func TestValidateAcceptsOmittedEventTypeFromOlderPayloads(t *testing.T) {
	service := &Service{}
	diagnostics := &domain.Diagnostics{
		SchemaVersion:  "1.0.0",
		NewestSequence: 1,
		BootEvents: []domain.BootEvent{
			{
				EventID:         "boot_00000001",
				Sequence:        1,
				UptimeMS:        12,
				BootCount:       1,
				ResetReason:     "power_on",
				FirmwareVersion: "0.4.0",
			},
		},
	}
	normalized, err := service.Validate("sprout_device_001", diagnostics)
	if err != nil {
		t.Fatalf("expected omitted event type to be accepted, got %v", err)
	}
	if normalized.BootEvents[0].EventType != domain.EventTypeBoot {
		t.Fatalf("expected the canonical event type, got %q", normalized.BootEvents[0].EventType)
	}
}

func TestValidateAcceptsPlatformDeviceIdentifier(t *testing.T) {
	service := &Service{}
	normalized, err := service.Validate(
		"sprout_device_001",
		&domain.Diagnostics{
			SchemaVersion:  "1.0.0",
			NewestSequence: 1,
			BootEvents: []domain.BootEvent{
				{
					EventID:         "boot_00000001",
					Sequence:        1,
					BootCount:       1,
					ResetReason:     "power_on",
					FirmwareVersion: "0.4.0",
				},
			},
		},
	)
	if err != nil {
		t.Fatalf("expected platform device identifier to be accepted, got %v", err)
	}
	if normalized == nil {
		t.Fatal("expected normalized diagnostics")
	}
}

func TestValidateRejectsUnknownSchemaVersion(t *testing.T) {
	service := &Service{}
	_, err := service.Validate("sprout_device_001", &domain.Diagnostics{
		SchemaVersion:  "2.0.0",
		NewestSequence: 1,
	})
	if err != domain.ErrInvalidDiagnostics {
		t.Fatalf("expected invalid diagnostics, got %v", err)
	}
}

func TestValidateRejectsUnknownInteractionEventType(t *testing.T) {
	service := &Service{}
	_, err := service.Validate("sprout_device_001", &domain.Diagnostics{
		SchemaVersion:  "1.0.0",
		NewestSequence: 1,
		InteractionEvents: []domain.InteractionEvent{
			{
				EventType:       "unknown_interaction",
				EventID:         "interaction_00000001",
				Sequence:        1,
				DetailCode:      "wake_word",
				DurationMS:      0,
				FirmwareVersion: "0.4.0",
			},
		},
	})
	if err != domain.ErrInvalidDiagnostics {
		t.Fatalf("expected invalid diagnostics for an unknown interaction type, got %v", err)
	}
}

func TestValidateRejectsOversizedInteractionDuration(t *testing.T) {
	service := &Service{}
	_, err := service.Validate("sprout_device_001", &domain.Diagnostics{
		SchemaVersion:  "1.0.0",
		NewestSequence: 1,
		InteractionEvents: []domain.InteractionEvent{
			{
				EventType:       domain.InteractionEventFactoryResetCompleted,
				EventID:         "interaction_00000001",
				Sequence:        1,
				DetailCode:      "guardian_request",
				DurationMS:      3_600_001,
				FirmwareVersion: "0.4.0",
			},
		},
	})
	if err != domain.ErrInvalidDiagnostics {
		t.Fatalf("expected invalid diagnostics for an oversized duration, got %v", err)
	}
}

func TestRecordDelegatesNormalizedDiagnostics(t *testing.T) {
	repositoryStub := &stubRepository{}
	service := &Service{repository: repositoryStub}
	err := service.Record(
		context.Background(),
		"sprout_device_001",
		time.Date(2026, 10, 4, 0, 0, 0, 0, time.UTC),
		&domain.Diagnostics{
			SchemaVersion:  "1.0.0",
			NewestSequence: 1,
			BootEvents: []domain.BootEvent{
				{
					EventID:         "boot_00000001",
					Sequence:        1,
					BootCount:       1,
					ResetReason:     "power_on",
					FirmwareVersion: "0.4.0",
				},
			},
		},
	)
	if err != nil {
		t.Fatalf("expected record to succeed, got %v", err)
	}
	if repositoryStub.saved == nil ||
		repositoryStub.saved.BootEvents[0].EventType != domain.EventTypeBoot {
		t.Fatalf("expected normalized diagnostics to reach the repository")
	}
}

func TestValidateProvisioningAcceptsCanonicalPayload(t *testing.T) {
	service := &Service{}
	normalized, err := service.ValidateProvisioning(
		"sprout_device_001",
		&domain.Provisioning{
			State:          domain.ProvisioningStateProvisioned,
			WiFiConfigured: true,
			SessionState:   domain.SessionStateReady,
			Events: []domain.ProvisioningEvent{
				{
					EventID:         "provisioning_00000002",
					EventType:       domain.ProvisioningEventWiFiConfigured,
					Sequence:        2,
					DetailCode:      "ble_provisioning",
					DurationMS:      4200,
					FirmwareVersion: "0.7.0",
				},
			},
		},
	)
	if err != nil {
		t.Fatalf("expected provisioning to validate, got %v", err)
	}
	if normalized == nil ||
		normalized.State != domain.ProvisioningStateProvisioned ||
		len(normalized.Events) != 1 {
		t.Fatalf("unexpected normalized provisioning: %+v", normalized)
	}
}

func TestValidateProvisioningRejectsUnknownEventType(t *testing.T) {
	service := &Service{}
	_, err := service.ValidateProvisioning(
		"sprout_device_001",
		&domain.Provisioning{
			State:        domain.ProvisioningStateProvisioning,
			SessionState: domain.SessionStateReady,
			Events: []domain.ProvisioningEvent{
				{
					EventID:         "provisioning_00000001",
					EventType:       "unknown_provisioning_step",
					Sequence:        1,
					DetailCode:      "first_run",
					FirmwareVersion: "0.7.0",
				},
			},
		},
	)
	if err != domain.ErrInvalidDiagnostics {
		t.Fatalf("expected invalid provisioning for unknown event type, got %v", err)
	}
}

func TestValidateProvisioningRejectsUnknownSessionState(t *testing.T) {
	service := &Service{}
	_, err := service.ValidateProvisioning(
		"sprout_device_001",
		&domain.Provisioning{
			State:        domain.ProvisioningStateProvisioned,
			SessionState: "expired",
			Events:       []domain.ProvisioningEvent{},
		},
	)
	if err != domain.ErrInvalidDiagnostics {
		t.Fatalf("expected invalid provisioning for unknown session state, got %v", err)
	}
}

func TestRecordProvisioningDelegatesNormalizedPayload(t *testing.T) {
	repositoryStub := &stubRepository{}
	service := &Service{repository: repositoryStub}
	err := service.RecordProvisioning(
		context.Background(),
		"sprout_device_001",
		time.Date(2026, 10, 4, 0, 0, 0, 0, time.UTC),
		&domain.Provisioning{
			State:        domain.ProvisioningStateProvisioned,
			SessionState: domain.SessionStateReady,
			Events: []domain.ProvisioningEvent{
				{
					EventID:         "provisioning_00000005",
					EventType:       domain.ProvisioningEventBindingConfirmed,
					Sequence:        5,
					DetailCode:      "guardian_binding",
					FirmwareVersion: "0.7.0",
				},
			},
		},
	)
	if err != nil {
		t.Fatalf("expected record provisioning to succeed, got %v", err)
	}
	if repositoryStub.savedProvisioning == nil ||
		repositoryStub.savedProvisioning.Events[0].EventType !=
			domain.ProvisioningEventBindingConfirmed {
		t.Fatalf("expected normalized provisioning to reach the repository")
	}
}

func TestRecordProvisioningIgnoresNilPayload(t *testing.T) {
	repositoryStub := &stubRepository{}
	service := &Service{repository: repositoryStub}
	if err := service.RecordProvisioning(
		context.Background(),
		"sprout_device_001",
		time.Now().UTC(),
		nil,
	); err != nil {
		t.Fatalf("expected nil provisioning to be a no-op, got %v", err)
	}
	if repositoryStub.savedProvisioning != nil {
		t.Fatal("expected no repository write for a nil provisioning payload")
	}
}
