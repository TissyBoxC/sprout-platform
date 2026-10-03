package service

import (
	"context"
	"testing"
	"time"

	bindingdomain "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/device_binding/domain"
	"github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/device_runtime/domain"
)

func TestRecordHeartbeatIsIdempotentAndComputesOnlineState(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, time.October, 3, 12, 0, 0, 0, time.UTC)
	repository := newMemoryRepository()
	service, err := New(Options{
		Repository: repository,
		BindingService: &memoryBindingReader{
			deviceID: "device_test_001",
			bindings: []bindingdomain.Binding{
				{
					DeviceID:   "device_test_001",
					DeviceName: "初芽",
				},
			},
		},
		Clock:            fixedClock{now: now},
		OfflineThreshold: time.Minute,
	})
	if err != nil {
		t.Fatalf("create runtime service: %v", err)
	}
	input := validHeartbeatInput(now)
	first, err := service.RecordHeartbeat(context.Background(), "session", input)
	if err != nil {
		t.Fatalf("record heartbeat: %v", err)
	}
	if !first.IsOnline {
		t.Fatal("expected a fresh online heartbeat")
	}
	if repository.saveCount != 1 {
		t.Fatalf("expected one persisted heartbeat, got %d", repository.saveCount)
	}

	second, err := service.RecordHeartbeat(context.Background(), "session", input)
	if err != nil {
		t.Fatalf("repeat heartbeat: %v", err)
	}
	if !second.IsOnline {
		t.Fatal("expected repeated heartbeat to retain online state")
	}
	if repository.saveCount != 2 {
		t.Fatalf("expected repository to see both attempts, got %d", repository.saveCount)
	}
	if repository.heartbeatCount != 1 {
		t.Fatalf("expected one logical heartbeat, got %d", repository.heartbeatCount)
	}
}

func TestRecordHeartbeatRejectsUnknownDeviceAndBadMetrics(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, time.October, 3, 12, 0, 0, 0, time.UTC)
	service, err := New(Options{
		Repository: newMemoryRepository(),
		BindingService: &memoryBindingReader{
			deviceID: "device_test_001",
		},
		Clock: fixedClock{now: now},
	})
	if err != nil {
		t.Fatalf("create runtime service: %v", err)
	}
	input := validHeartbeatInput(now)
	input.DeviceID = "device_other_001"
	if _, err := service.RecordHeartbeat(context.Background(), "session", input); err != domain.ErrInvalidHeartbeat {
		t.Fatalf("expected invalid heartbeat for a mismatched device, got %v", err)
	}

	input = validHeartbeatInput(now)
	input.PacketLossPercent = 101
	if _, err := service.RecordHeartbeat(context.Background(), "session", input); err != domain.ErrInvalidHeartbeat {
		t.Fatalf("expected invalid heartbeat for packet loss, got %v", err)
	}
}

func TestCommandLifecycleIsAcknowledgedOnce(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, time.October, 3, 12, 0, 0, 0, time.UTC)
	repository := newMemoryRepository()
	service, err := New(Options{
		Repository: repository,
		BindingService: &memoryBindingReader{
			deviceID: "device_test_001",
		},
		Clock: fixedClock{now: now},
	})
	if err != nil {
		t.Fatalf("create runtime service: %v", err)
	}
	command, err := service.CreateCommand(
		context.Background(),
		"device_test_001",
		domain.CommandReconnectNetwork,
		"admin_test_001",
		map[string]any{},
	)
	if err != nil {
		t.Fatalf("create command: %v", err)
	}
	if err := service.AcknowledgeCommand(
		context.Background(),
		"session",
		"device_test_001",
		command.ID,
		domain.CommandStatusAcknowledged,
		"ok",
	); err != nil {
		t.Fatalf("acknowledge command: %v", err)
	}
	if err := service.AcknowledgeCommand(
		context.Background(),
		"session",
		"device_test_001",
		command.ID,
		domain.CommandStatusAcknowledged,
		"ok",
	); err != domain.ErrCommandNotFound {
		t.Fatalf("expected duplicate acknowledgement to be rejected, got %v", err)
	}
}

func TestListDeviceStatusesReturnsOnlineAndMissingRuntimeStates(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, time.October, 3, 12, 0, 0, 0, time.UTC)
	repository := newMemoryRepository()
	repository.statuses["device_test_001"] = domain.RuntimeStatus{
		Heartbeat: domain.Heartbeat{
			DeviceID:        "device_test_001",
			ConnectionState: domain.ConnectionStateOnline,
			ReceivedAt:      now.Add(-30 * time.Second),
		},
	}
	repository.statuses["device_test_002"] = domain.RuntimeStatus{
		Heartbeat: domain.Heartbeat{
			DeviceID:        "device_test_002",
			ConnectionState: domain.ConnectionStateOnline,
			ReceivedAt:      now.Add(-2 * time.Minute),
		},
	}
	service, err := New(Options{
		Repository: repository,
		BindingService: &memoryBindingReader{
			bindings: []bindingdomain.Binding{
				{
					DeviceID:   "device_test_001",
					DeviceName: "初芽",
				},
				{
					DeviceID:   "device_test_002",
					DeviceName: "备用设备",
				},
				{
					DeviceID:   "device_test_003",
					DeviceName: "尚未连接设备",
				},
			},
		},
		Clock:            fixedClock{now: now},
		OfflineThreshold: time.Minute,
	})
	if err != nil {
		t.Fatalf("create runtime service: %v", err)
	}

	statuses, err := service.ListDeviceStatuses(context.Background(), "parent-001")
	if err != nil {
		t.Fatalf("list device statuses: %v", err)
	}
	if len(statuses) != 3 {
		t.Fatalf("expected three bound devices, got %d", len(statuses))
	}
	if statuses[0].Runtime == nil || !statuses[0].Runtime.IsOnline {
		t.Fatal("expected the fresh heartbeat to be online")
	}
	if statuses[1].Runtime == nil || statuses[1].Runtime.IsOnline {
		t.Fatal("expected the stale heartbeat to be offline")
	}
	if statuses[2].Runtime != nil {
		t.Fatal("expected a device without runtime data to remain safely unknown")
	}
}

func TestResolveDeviceFamilyRequiresSessionOwnedPathDevice(t *testing.T) {
	t.Parallel()
	service, err := New(Options{
		Repository: newMemoryRepository(),
		BindingService: &memoryBindingReader{
			deviceID: "device_test_001",
			bindings: []bindingdomain.Binding{
				{
					ParentAccountID: "parent-001",
					DeviceID:        "device_test_001",
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("create runtime service: %v", err)
	}

	familyID, err := service.ResolveDeviceFamily(
		context.Background(),
		"session",
		"device_test_001",
	)
	if err != nil {
		t.Fatalf("resolve device family: %v", err)
	}
	if familyID != "parent-001" {
		t.Fatalf("expected parent-001, got %q", familyID)
	}
	if _, err := service.ResolveDeviceFamily(
		context.Background(),
		"session",
		"device_other_001",
	); err != bindingdomain.ErrInvalidDeviceProof {
		t.Fatalf("expected a mismatched path device to be rejected, got %v", err)
	}
}

type fixedClock struct {
	now time.Time
}

func (clock fixedClock) Now() time.Time {
	return clock.now
}

type memoryBindingReader struct {
	deviceID string
	bindings []bindingdomain.Binding
}

func (reader *memoryBindingReader) VerifyDeviceSession(
	context.Context,
	string,
) (string, error) {
	return reader.deviceID, nil
}

func (reader *memoryBindingReader) GetByDeviceID(
	_ context.Context,
	deviceID string,
) (*bindingdomain.Binding, error) {
	for index := range reader.bindings {
		if reader.bindings[index].DeviceID == deviceID {
			return &reader.bindings[index], nil
		}
	}
	return nil, bindingdomain.ErrDeviceNotFound
}

func (reader *memoryBindingReader) List(
	context.Context,
	string,
) ([]bindingdomain.Binding, error) {
	return reader.bindings, nil
}

func (reader *memoryBindingReader) ListAllForAdmin(
	context.Context,
) ([]bindingdomain.Binding, error) {
	return reader.bindings, nil
}

type memoryRepository struct {
	heartbeats     map[string]domain.Heartbeat
	statuses       map[string]domain.RuntimeStatus
	commands       map[string]domain.Command
	saveCount      int
	heartbeatCount int
}

func newMemoryRepository() *memoryRepository {
	return &memoryRepository{
		heartbeats: make(map[string]domain.Heartbeat),
		statuses:   make(map[string]domain.RuntimeStatus),
		commands:   make(map[string]domain.Command),
	}
}

func (repository *memoryRepository) SaveHeartbeat(
	_ context.Context,
	heartbeat *domain.Heartbeat,
) (bool, error) {
	repository.saveCount++
	key := heartbeat.DeviceID + ":" + heartbeat.HeartbeatID
	if _, exists := repository.heartbeats[key]; exists {
		return false, nil
	}
	repository.heartbeats[key] = *heartbeat
	repository.heartbeatCount++
	repository.statuses[heartbeat.DeviceID] = domain.RuntimeStatus{
		Heartbeat: *heartbeat,
	}
	return true, nil
}

func (repository *memoryRepository) GetStatus(
	_ context.Context,
	deviceID string,
) (*domain.RuntimeStatus, error) {
	status, exists := repository.statuses[deviceID]
	if !exists {
		return nil, domain.ErrDeviceNotFound
	}
	return &status, nil
}

func (repository *memoryRepository) ListStatuses(
	_ context.Context,
	deviceIDs []string,
) ([]domain.RuntimeStatus, error) {
	result := make([]domain.RuntimeStatus, 0, len(deviceIDs))
	for _, deviceID := range deviceIDs {
		if status, exists := repository.statuses[deviceID]; exists {
			result = append(result, status)
		}
	}
	return result, nil
}

func (repository *memoryRepository) ListAllStatuses(
	context.Context,
) ([]domain.RuntimeStatus, error) {
	result := make([]domain.RuntimeStatus, 0, len(repository.statuses))
	for _, status := range repository.statuses {
		result = append(result, status)
	}
	return result, nil
}

func (repository *memoryRepository) CreateCommand(
	_ context.Context,
	command *domain.Command,
) error {
	if _, exists := repository.commands[command.ID]; exists {
		return nil
	}
	repository.commands[command.ID] = *command
	return nil
}

func (repository *memoryRepository) GetCommand(
	_ context.Context,
	commandID string,
) (*domain.Command, error) {
	command, exists := repository.commands[commandID]
	if !exists {
		return nil, domain.ErrCommandNotFound
	}
	return &command, nil
}

func (repository *memoryRepository) ListCommands(
	_ context.Context,
	deviceID string,
) ([]domain.Command, error) {
	result := make([]domain.Command, 0)
	for _, command := range repository.commands {
		if command.DeviceID == deviceID {
			result = append(result, command)
		}
	}
	return result, nil
}

func (repository *memoryRepository) ListPendingCommands(
	_ context.Context,
	deviceID string,
) ([]domain.Command, error) {
	result := make([]domain.Command, 0)
	for _, command := range repository.commands {
		if command.DeviceID == deviceID &&
			(command.Status == domain.CommandStatusPending ||
				command.Status == domain.CommandStatusDelivered) {
			result = append(result, command)
		}
	}
	return result, nil
}

func (repository *memoryRepository) AcknowledgeCommand(
	_ context.Context,
	commandID string,
	deviceID string,
	status domain.CommandStatus,
	resultCode string,
) error {
	command, exists := repository.commands[commandID]
	if !exists || command.DeviceID != deviceID ||
		(command.Status != domain.CommandStatusPending &&
			command.Status != domain.CommandStatusDelivered) {
		return domain.ErrCommandNotFound
	}
	command.Status = status
	command.ResultCode = resultCode
	repository.commands[commandID] = command
	return nil
}

func validHeartbeatInput(now time.Time) domain.HeartbeatInput {
	return domain.HeartbeatInput{
		DeviceID:          "device_test_001",
		HeartbeatID:       "heartbeat_test_001",
		ReportedAt:        now,
		FirmwareVersion:   "0.3.0",
		ConnectionState:   domain.ConnectionStateOnline,
		Transport:         domain.TransportWiFi,
		NetworkQuality:    domain.NetworkQualityGood,
		RSSIDBM:           -58,
		LatencyMS:         42,
		PacketLossPercent: 1,
		TimeSyncState:     domain.TimeSyncSynchronized,
		TimeSyncSource:    domain.TimeSyncSourceSNTP,
		TimeSyncedAt:      &now,
		TimeOffsetMS:      12,
		OfflineState:      domain.OfflineStateOnline,
		OfflineReason:     domain.OfflineReasonNone,
		FallbackActive:    false,
		PendingTelemetry:  0,
	}
}
