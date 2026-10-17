package service

import (
	"context"
	"errors"
	"strings"
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

func TestStaleHeartbeatDoesNotReportOnlineOrReplaceNewerSnapshot(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, time.October, 3, 12, 0, 0, 0, time.UTC)
	repository := newMemoryRepository()
	service, err := New(Options{
		Repository:       repository,
		BindingService:   &memoryBindingReader{deviceID: "device_test_001"},
		Clock:            fixedClock{now: now},
		OfflineThreshold: 30 * time.Second,
	})
	if err != nil {
		t.Fatalf("create runtime service: %v", err)
	}
	newer := validHeartbeatInput(now)
	newer.HeartbeatID = "heartbeat_newer"
	if _, err := service.RecordHeartbeat(context.Background(), "session", newer); err != nil {
		t.Fatalf("record newer heartbeat: %v", err)
	}

	stale := validHeartbeatInput(now.Add(-5 * time.Minute))
	stale.HeartbeatID = "heartbeat_stale"
	stale.ConnectionState = domain.ConnectionStateOffline
	stale.OfflineState = domain.OfflineStateOffline
	stale.OfflineReason = domain.OfflineReasonNetworkUnavailable
	stale.FallbackActive = true
	status, err := service.RecordHeartbeat(context.Background(), "session", stale)
	if err != nil {
		t.Fatalf("send stale heartbeat: %v", err)
	}
	if status.HeartbeatID != "heartbeat_newer" || !status.IsOnline {
		t.Fatalf("expected newer online snapshot to remain current, got %#v", status)
	}
}

func TestHeartbeatCapabilitySnapshotRequiresRevisionAndRejectsUnknownValues(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, time.October, 3, 12, 0, 0, 0, time.UTC)
	service, err := New(Options{
		Repository:     newMemoryRepository(),
		BindingService: &memoryBindingReader{deviceID: "device_test_001"},
		Clock:          fixedClock{now: now},
	})
	if err != nil {
		t.Fatalf("create runtime service: %v", err)
	}

	withoutRevision := validHeartbeatInput(now)
	withoutRevision.Capabilities = []string{"wifi"}
	if _, err := service.RecordHeartbeat(
		context.Background(),
		"session",
		withoutRevision,
	); err != domain.ErrInvalidHeartbeat {
		t.Fatalf("expected a list without revision to be rejected, got %v", err)
	}

	withoutCapabilities := validHeartbeatInput(now)
	withoutCapabilities.CapabilityRevision = 2
	if _, err := service.RecordHeartbeat(
		context.Background(),
		"session",
		withoutCapabilities,
	); err != domain.ErrInvalidHeartbeat {
		t.Fatalf("expected a revision without a list to be rejected, got %v", err)
	}

	unknown := validHeartbeatInput(now)
	unknown.Capabilities = []string{"wifi", "teleportation"}
	unknown.CapabilityRevision = 2
	if _, err := service.RecordHeartbeat(
		context.Background(),
		"session",
		unknown,
	); err != domain.ErrInvalidHeartbeat {
		t.Fatalf("expected an unknown capability to be rejected, got %v", err)
	}

	empty := validHeartbeatInput(now)
	empty.Capabilities = []string{}
	empty.CapabilityRevision = 2
	if _, err := service.RecordHeartbeat(
		context.Background(),
		"session",
		empty,
	); err != domain.ErrInvalidHeartbeat {
		t.Fatalf("expected an empty capability snapshot to be rejected, got %v", err)
	}

	legacy := validHeartbeatInput(now)
	legacy.Capabilities = nil
	legacy.CapabilityRevision = 0
	if _, err := service.RecordHeartbeat(
		context.Background(),
		"session",
		legacy,
	); err != nil {
		t.Fatalf("expected legacy firmware without snapshots to stay accepted, got %v", err)
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

func TestRecordHeartbeatRejectsContradictoryOfflineState(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, time.October, 3, 12, 0, 0, 0, time.UTC)
	repository := newMemoryRepository()
	service, err := New(Options{
		Repository:     repository,
		BindingService: &memoryBindingReader{deviceID: "device_test_001"},
		Clock:          fixedClock{now: now},
	})
	if err != nil {
		t.Fatalf("create runtime service: %v", err)
	}
	cases := map[string]func(*domain.HeartbeatInput){
		"online fallback": func(input *domain.HeartbeatInput) {
			input.FallbackActive = true
		},
		"online reason": func(input *domain.HeartbeatInput) {
			input.OfflineReason = domain.OfflineReasonServiceUnavailable
		},
		"offline without reason": func(input *domain.HeartbeatInput) {
			input.ConnectionState = domain.ConnectionStateOffline
			input.OfflineState = domain.OfflineStateOffline
			input.FallbackActive = true
		},
		"grace fallback": func(input *domain.HeartbeatInput) {
			input.ConnectionState = domain.ConnectionStateConnecting
			input.OfflineState = domain.OfflineStateGrace
			input.OfflineReason = domain.OfflineReasonNetworkUnavailable
			input.FallbackActive = true
		},
	}
	for name, mutate := range cases {
		input := validHeartbeatInput(now)
		input.HeartbeatID = "heartbeat_" + strings.ReplaceAll(name, " ", "_")
		mutate(&input)
		if _, err := service.RecordHeartbeat(context.Background(), "session", input); err != domain.ErrInvalidHeartbeat {
			t.Errorf("%s: expected invalid heartbeat, got %v", name, err)
		}
	}
}

func TestRecordHeartbeatPersistsTemperatureBandAndRejectsOutOfRange(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, time.October, 3, 12, 0, 0, 0, time.UTC)
	repository := newMemoryRepository()
	service, err := New(Options{
		Repository: repository,
		BindingService: &memoryBindingReader{
			deviceID: "device_test_001",
			bindings: []bindingdomain.Binding{{DeviceID: "device_test_001"}},
		},
		Clock:            fixedClock{now: now},
		OfflineThreshold: time.Minute,
	})
	if err != nil {
		t.Fatalf("create runtime service: %v", err)
	}

	tenths := 425
	input := validHeartbeatInput(now)
	input.Temperature = domain.TemperatureStateWarm
	input.TemperatureTenths = &tenths
	status, err := service.RecordHeartbeat(context.Background(), "session", input)
	if err != nil {
		t.Fatalf("record heartbeat with temperature: %v", err)
	}
	if status.Temperature != domain.TemperatureStateWarm {
		t.Fatalf("expected warm band, got %q", status.Temperature)
	}
	if status.TemperatureTenths == nil || *status.TemperatureTenths != tenths {
		t.Fatalf("expected the temperature reading to persist, got %v", status.TemperatureTenths)
	}

	outOfRange := validHeartbeatInput(now)
	outOfRange.HeartbeatID = "heartbeat_test_002"
	outOfRange.Temperature = domain.TemperatureStateCritical
	tooHot := 2000
	outOfRange.TemperatureTenths = &tooHot
	if _, err := service.RecordHeartbeat(context.Background(), "session", outOfRange); err != domain.ErrInvalidHeartbeat {
		t.Fatalf("expected invalid heartbeat for an out-of-range temperature, got %v", err)
	}

	omitted := validHeartbeatInput(now)
	omitted.HeartbeatID = "heartbeat_test_003"
	omitted.Temperature = ""
	status, err = service.RecordHeartbeat(context.Background(), "session", omitted)
	if err != nil {
		t.Fatalf("record heartbeat without temperature: %v", err)
	}
	if status.Temperature != domain.TemperatureStateUnknown {
		t.Fatalf("expected omitted temperature to normalize to unknown, got %q", status.Temperature)
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

func TestListPendingCommandsMarksFirstDeliveryOnce(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, time.October, 3, 12, 0, 0, 0, time.UTC)
	later := now.Add(time.Minute)
	repository := newMemoryRepository()
	repository.now = now
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

	first, err := service.ListPendingCommands(context.Background(), "session")
	if err != nil {
		t.Fatalf("list pending commands: %v", err)
	}
	if len(first) != 1 || first[0].Status != domain.CommandStatusDelivered {
		t.Fatalf("expected one delivered command, got %#v", first)
	}
	if first[0].DeliveredAt == nil || !first[0].DeliveredAt.Equal(now) {
		t.Fatalf("expected delivered_at %v, got %v", now, first[0].DeliveredAt)
	}
	stored := repository.commands[command.ID]
	if stored.Status != domain.CommandStatusDelivered ||
		stored.DeliveredAt == nil ||
		!stored.DeliveredAt.Equal(now) {
		t.Fatalf("expected persisted delivery at %v, got %#v", now, stored)
	}

	repository.now = later
	second, err := service.ListPendingCommands(context.Background(), "session")
	if err != nil {
		t.Fatalf("second list pending commands: %v", err)
	}
	if len(second) != 1 || second[0].Status != domain.CommandStatusDelivered {
		t.Fatalf("expected the same delivered command, got %#v", second)
	}
	if second[0].DeliveredAt == nil || !second[0].DeliveredAt.Equal(now) {
		t.Fatalf("expected delivery time to remain %v, got %v", now, second[0].DeliveredAt)
	}
}

func TestExpiredCommandIsNotDeliveredOrAcknowledged(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, time.October, 3, 12, 0, 0, 0, time.UTC)
	repository := newMemoryRepository()
	repository.now = now
	service, err := New(Options{
		Repository: repository,
		BindingService: &memoryBindingReader{
			deviceID: "device_test_001",
		},
		Clock:      fixedClock{now: now},
		CommandTTL: time.Minute,
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
	if !command.ExpiresAt.Equal(now.Add(time.Minute)) {
		t.Fatalf("expected expiry %v, got %v", now.Add(time.Minute), command.ExpiresAt)
	}
	if !repository.commands[command.ID].ExpiresAt.Equal(command.ExpiresAt) {
		t.Fatal("expected expiry to be persisted")
	}

	later := now.Add(time.Minute)
	service.timeSource = fixedClock{now: later}
	repository.now = later
	commands, err := service.ListPendingCommands(context.Background(), "session")
	if err != nil {
		t.Fatalf("list expired commands: %v", err)
	}
	if len(commands) != 0 {
		t.Fatalf("expected expired command to be withheld, got %#v", commands)
	}
	if err := service.AcknowledgeCommand(
		context.Background(),
		"session",
		"device_test_001",
		command.ID,
		domain.CommandStatusAcknowledged,
		"late",
	); err != domain.ErrCommandNotFound {
		t.Fatalf("expected expired acknowledgement to be rejected, got %v", err)
	}
	if stored := repository.commands[command.ID]; stored.Status != domain.CommandStatusExpired {
		t.Fatalf("expected durable expired status, got %#v", stored)
	}
}

func TestCommandAcknowledgementNotifiesDeliverySink(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, time.October, 3, 12, 0, 0, 0, time.UTC)
	repository := newMemoryRepository()
	sink := &recordingAcknowledgementSink{}
	service, err := New(Options{
		Repository: repository,
		BindingService: &memoryBindingReader{
			deviceID: "device_test_001",
		},
		Clock:               fixedClock{now: now},
		AcknowledgementSink: sink,
	})
	if err != nil {
		t.Fatalf("create runtime service: %v", err)
	}
	command, err := service.CreateCommand(
		context.Background(),
		"device_test_001",
		domain.CommandDisplayMessage,
		"parent_test_001",
		map[string]any{"notification_id": "notification_test_001"},
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
	if sink.commandID != command.ID || sink.deviceID != "device_test_001" {
		t.Fatalf(
			"expected sink call for command %q and device %q, got %q and %q",
			command.ID,
			"device_test_001",
			sink.commandID,
			sink.deviceID,
		)
	}
}

func TestCommandAcknowledgementIgnoresSecondarySinkFailure(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, time.October, 3, 12, 0, 0, 0, time.UTC)
	repository := newMemoryRepository()
	service, err := New(Options{
		Repository: repository,
		BindingService: &memoryBindingReader{
			deviceID: "device_test_001",
		},
		Clock: fixedClock{now: now},
		AcknowledgementSink: &failingAcknowledgementSink{
			err: errors.New("notification store unavailable"),
		},
	})
	if err != nil {
		t.Fatalf("create runtime service: %v", err)
	}
	command, err := service.CreateCommand(
		context.Background(),
		"device_test_001",
		domain.CommandDisplayMessage,
		"parent_test_001",
		map[string]any{"notification_id": "notification_test_001"},
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
		t.Fatalf("acknowledge command should ignore sink failure, got %v", err)
	}
	if repository.commands[command.ID].Status != domain.CommandStatusAcknowledged {
		t.Fatal("expected the command to remain acknowledged")
	}
}

func TestFactoryResetCommandIsAccepted(t *testing.T) {
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
		domain.CommandFactoryReset,
		"admin_test_001",
		map[string]any{},
	)
	if err != nil {
		t.Fatalf("create factory reset command: %v", err)
	}
	if command.Type != domain.CommandFactoryReset {
		t.Fatalf("expected factory_reset command, got %q", command.Type)
	}
	if _, err := service.CreateCommand(
		context.Background(),
		"device_test_001",
		domain.CommandType("wipe_everything"),
		"admin_test_001",
		map[string]any{},
	); err != domain.ErrInvalidCommand {
		t.Fatalf("expected unknown command type to be rejected, got %v", err)
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

func TestDeviceStatusUsesRevisionedRuntimeSnapshotForSingleAndListViews(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, time.October, 3, 12, 0, 0, 0, time.UTC)
	binding := bindingdomain.Binding{
		ParentAccountID:    "parent-001",
		DeviceID:           "device_test_001",
		DeviceName:         "初芽",
		FirmwareVersion:    "1.0.0",
		Capabilities:       []string{"wifi", "display"},
		CapabilityRevision: 1,
	}
	repository := newMemoryRepository()
	repository.statuses[binding.DeviceID] = domain.RuntimeStatus{
		Heartbeat: domain.Heartbeat{
			DeviceID:           binding.DeviceID,
			FirmwareVersion:    "1.1.0",
			Capabilities:       []string{"wifi", "display", "camera"},
			CapabilityRevision: 2,
			ConnectionState:    domain.ConnectionStateOnline,
			ReceivedAt:         now,
		},
	}
	service, err := New(Options{
		Repository: repository,
		BindingService: &memoryBindingReader{
			deviceID: binding.DeviceID,
			bindings: []bindingdomain.Binding{binding},
		},
		Clock: fixedClock{now: now},
	})
	if err != nil {
		t.Fatalf("create runtime service: %v", err)
	}

	single, err := service.GetDeviceStatus(
		context.Background(),
		binding.ParentAccountID,
		binding.DeviceID,
	)
	if err != nil {
		t.Fatalf("get device status: %v", err)
	}
	list, err := service.ListDeviceStatuses(context.Background(), binding.ParentAccountID)
	if err != nil {
		t.Fatalf("list device statuses: %v", err)
	}
	for name, status := range map[string]*domain.DeviceStatus{
		"single": single,
		"list":   &list[0],
	} {
		if status.FirmwareVersion != "1.1.0" ||
			status.CapabilityRevision != 2 ||
			len(status.Capabilities) != 3 {
			t.Fatalf("%s view did not project the runtime snapshot: %#v", name, status)
		}
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

func TestCreateCommandPublishesToCommandTransport(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, time.October, 14, 9, 0, 0, 0, time.UTC)
	publisher := &recordingCommandPublisher{}
	service, err := New(Options{
		Repository: newMemoryRepository(),
		BindingService: &memoryBindingReader{
			deviceID: "device_test_001",
		},
		Clock:            fixedClock{now: now},
		CommandPublisher: publisher,
	})
	if err != nil {
		t.Fatalf("create runtime service: %v", err)
	}

	command, err := service.CreateCommand(
		context.Background(),
		"device_test_001",
		domain.CommandRefreshConfiguration,
		"admin_test_001",
		map[string]any{"reason": "support"},
	)
	if err != nil {
		t.Fatalf("create command: %v", err)
	}
	if publisher.deviceID != "device_test_001" {
		t.Fatalf("unexpected published device id: %q", publisher.deviceID)
	}
	if publisher.command == nil || publisher.command.ID != command.ID {
		t.Fatalf("expected the created command to be published, got %#v", publisher.command)
	}
}

func TestCreateCommandSurvivesPublishFailure(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, time.October, 14, 9, 0, 0, 0, time.UTC)
	repository := newMemoryRepository()
	service, err := New(Options{
		Repository: repository,
		BindingService: &memoryBindingReader{
			deviceID: "device_test_001",
		},
		Clock: fixedClock{now: now},
		CommandPublisher: &recordingCommandPublisher{
			err: errors.New("broker unavailable"),
		},
	})
	if err != nil {
		t.Fatalf("create runtime service: %v", err)
	}

	command, err := service.CreateCommand(
		context.Background(),
		"device_test_001",
		domain.CommandReconnectNetwork,
		"admin_test_001",
		nil,
	)
	if err != nil {
		t.Fatalf("expected a publish failure to keep the command durable: %v", err)
	}
	if repository.commands[command.ID].ID != command.ID {
		t.Fatal("expected the command row to be persisted despite the publish failure")
	}
}

func (clock fixedClock) Now() time.Time {
	return clock.now
}

type recordingCommandPublisher struct {
	deviceID string
	command  *domain.Command
	err      error
}

func TestAcknowledgeCommandFromDeviceIsIdempotent(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, time.October, 14, 9, 0, 0, 0, time.UTC)
	repository := newMemoryRepository()
	sink := &recordingAcknowledgementSink{}
	service, err := New(Options{
		Repository: repository,
		BindingService: &memoryBindingReader{
			deviceID: "device_test_001",
		},
		AcknowledgementSink: sink,
		Clock:               fixedClock{now: now},
	})
	if err != nil {
		t.Fatalf("create runtime service: %v", err)
	}
	command, err := service.CreateCommand(
		context.Background(),
		"device_test_001",
		domain.CommandRefreshConfiguration,
		"admin_test_001",
		nil,
	)
	if err != nil {
		t.Fatalf("create command: %v", err)
	}

	if err := service.AcknowledgeCommandFromDevice(
		context.Background(),
		"device_test_001",
		command.ID,
		domain.CommandStatusAcknowledged,
		"ok",
	); err != nil {
		t.Fatalf("acknowledge command: %v", err)
	}
	if repository.commands[command.ID].Status != domain.CommandStatusAcknowledged {
		t.Fatalf(
			"expected acknowledged status, got %q",
			repository.commands[command.ID].Status,
		)
	}
	if sink.commandID != command.ID || sink.deviceID != "device_test_001" {
		t.Fatalf("unexpected sink notification: %#v", sink)
	}

	// A retransmitted QoS 1 ack must not apply the terminal state twice, but
	// the broker may legitimately redeliver it before the SUBACK is processed.
	if err := service.AcknowledgeCommandFromDevice(
		context.Background(),
		"device_test_001",
		command.ID,
		domain.CommandStatusAcknowledged,
		"ok",
	); err != nil {
		t.Fatalf("expected repeated ack to be idempotent, got %v", err)
	}
}

func TestAcknowledgeCommandFromDeviceValidatesInput(t *testing.T) {
	t.Parallel()
	service, err := New(Options{
		Repository: newMemoryRepository(),
		BindingService: &memoryBindingReader{
			deviceID: "device_test_001",
		},
	})
	if err != nil {
		t.Fatalf("create runtime service: %v", err)
	}

	cases := []struct {
		name      string
		deviceID  string
		commandID string
		status    domain.CommandStatus
	}{
		{"bad device id", "bad device id", "command_test_001", domain.CommandStatusAcknowledged},
		{"bad command id", "device_test_001", "bad command id", domain.CommandStatusAcknowledged},
		{"bad status", "device_test_001", "command_test_001", domain.CommandStatusPending},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			err := service.AcknowledgeCommandFromDevice(
				context.Background(),
				test.deviceID,
				test.commandID,
				test.status,
				"",
			)
			if err != domain.ErrInvalidCommand {
				t.Fatalf("expected ErrInvalidCommand, got %v", err)
			}
		})
	}
}

func (publisher *recordingCommandPublisher) PublishCommand(
	_ context.Context,
	deviceID string,
	command *domain.Command,
) error {
	publisher.deviceID = deviceID
	publisher.command = command
	return publisher.err
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
	now            time.Time
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
	if current, exists := repository.statuses[heartbeat.DeviceID]; exists {
		if current.ReportedAt.After(heartbeat.ReportedAt) ||
			(current.ReportedAt.Equal(heartbeat.ReportedAt) &&
				current.ReceivedAt.After(heartbeat.ReceivedAt)) {
			return true, nil
		}
	}
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
	for commandID, command := range repository.commands {
		if command.DeviceID == deviceID &&
			(command.Status == domain.CommandStatusPending ||
				command.Status == domain.CommandStatusDelivered) &&
			!command.ExpiresAt.IsZero() &&
			!command.ExpiresAt.After(repository.now) {
			command.Status = domain.CommandStatusExpired
			repository.commands[commandID] = command
			continue
		}
		if command.DeviceID == deviceID &&
			(command.Status == domain.CommandStatusPending ||
				command.Status == domain.CommandStatusDelivered) &&
			(command.ExpiresAt.IsZero() || command.ExpiresAt.After(repository.now)) {
			result = append(result, command)
		}
	}
	return result, nil
}

func (repository *memoryRepository) MarkCommandsDelivered(
	_ context.Context,
	commandIDs []string,
	deviceID string,
) error {
	for _, commandID := range commandIDs {
		command, exists := repository.commands[commandID]
		if !exists || command.DeviceID != deviceID ||
			command.Status != domain.CommandStatusPending ||
			(!command.ExpiresAt.IsZero() && !command.ExpiresAt.After(repository.now)) {
			continue
		}
		deliveredAt := repository.now
		if deliveredAt.IsZero() {
			deliveredAt = time.Now().UTC()
		}
		command.Status = domain.CommandStatusDelivered
		command.DeliveredAt = &deliveredAt
		repository.commands[commandID] = command
	}
	return nil
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
			command.Status != domain.CommandStatusDelivered) ||
		(!command.ExpiresAt.IsZero() && !command.ExpiresAt.After(repository.now)) {
		return domain.ErrCommandNotFound
	}
	command.Status = status
	command.ResultCode = resultCode
	repository.commands[commandID] = command
	return nil
}

type recordingAcknowledgementSink struct {
	commandID string
	deviceID  string
}

func (sink *recordingAcknowledgementSink) MarkDeliveryDeliveredByCommand(
	_ context.Context,
	commandID string,
	deviceID string,
) error {
	sink.commandID = commandID
	sink.deviceID = deviceID
	return nil
}

type failingAcknowledgementSink struct {
	err error
}

func (sink *failingAcknowledgementSink) MarkDeliveryDeliveredByCommand(
	_ context.Context,
	_ string,
	_ string,
) error {
	return sink.err
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
