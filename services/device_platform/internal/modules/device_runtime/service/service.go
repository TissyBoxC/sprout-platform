// Package service owns device runtime status and maintenance commands.
package service

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	bindingdomain "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/device_binding/domain"
	bindingservice "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/device_binding/service"
	"github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/device_runtime/domain"
	"github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/device_runtime/repository"
	otaDomain "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/ota/domain"
	"github.com/TissyBoxC/sprout-platform/services/device_platform/internal/platform/clock"
	"github.com/TissyBoxC/sprout-platform/services/device_platform/internal/platform/featurehealth"
	"github.com/google/uuid"
)

var runtimeIdentifierPattern = regexp.MustCompile(`^[a-z][a-z0-9]*(?:_[a-z0-9]+){1,7}$`)

// BindingReader is the narrow device-binding surface required by runtime
// status. Keeping it local prevents the runtime module from reading binding
// tables directly.
type BindingReader interface {
	VerifyDeviceSession(ctx context.Context, token string) (string, error)
	GetByDeviceID(ctx context.Context, deviceID string) (*bindingdomain.Binding, error)
	List(ctx context.Context, parentAccountID string) ([]bindingdomain.Binding, error)
	ListAllForAdmin(ctx context.Context) ([]bindingdomain.Binding, error)
}

// DiagnosticReader is the narrow diagnostic surface required to summarize a
// device's recent stability. It is optional; when it is absent the runtime
// status simply reports no recent crashes.
type DiagnosticReader interface {
	RecentCrashCount(ctx context.Context, deviceID string) (uint64, error)
}

// CommandAcknowledgementSink receives a successful device command
// acknowledgement. It is optional so runtime command delivery remains usable
// when notification delivery is not assembled.
type CommandAcknowledgementSink interface {
	MarkDeliveryDeliveredByCommand(
		ctx context.Context,
		commandID string,
		deviceID string,
	) error
}

// CommandPublisher pushes one freshly created command to a device transport.
//
// It is optional. Command rows are always persisted first, and the device's
// authenticated HTTP poll remains the recovery path, so a publish failure must
// never fail command creation. Implementations are expected to be safe for
// concurrent use.
type CommandPublisher interface {
	PublishCommand(
		ctx context.Context,
		deviceID string,
		command *domain.Command,
	) error
}

// FirmwareUpdateAuthorizer is the narrow OTA validation port used before a
// firmware_update runtime command becomes durable. It deliberately exposes no
// release creation or deployment mutation; the OTA module remains the only
// authority that can approve a firmware rollout.
type FirmwareUpdateAuthorizer interface {
	AuthorizeRuntimeFirmwareUpdate(
		ctx context.Context,
		deviceID string,
		releaseID string,
	) (*FirmwareUpdateAuthorization, error)
}

// FirmwareUpdateAuthorization is the immutable OTA authorization checked
// before a firmware_update command may be persisted. DeploymentKind is not
// duplicated in the command payload: OTA's deployment row remains the single
// source of truth.
type FirmwareUpdateAuthorization = otaDomain.RuntimeFirmwareUpdateAuthorization

// Service validates, stores, and exposes device runtime state.
type Service struct {
	repository          repository.Repository
	bindingReader       BindingReader
	diagnosticReader    DiagnosticReader
	acknowledgementSink CommandAcknowledgementSink
	commandPublisher    CommandPublisher
	firmwareUpdateAuth  FirmwareUpdateAuthorizer
	timeSource          clock.Clock
	offlineThreshold    time.Duration
	commandTTL          time.Duration
}

// Options contains runtime service dependencies and policy.
type Options struct {
	Repository               repository.Repository
	BindingService           BindingReader
	DiagnosticReader         DiagnosticReader
	AcknowledgementSink      CommandAcknowledgementSink
	CommandPublisher         CommandPublisher
	FirmwareUpdateAuthorizer FirmwareUpdateAuthorizer
	Clock                    clock.Clock
	OfflineThreshold         time.Duration
	CommandTTL               time.Duration
}

// New creates the device runtime service.
func New(options Options) (*Service, error) {
	if options.Repository == nil {
		return nil, errors.New("device runtime repository is required")
	}
	if options.BindingService == nil {
		return nil, errors.New("device binding service is required")
	}
	timeSource := options.Clock
	if timeSource == nil {
		timeSource = clock.SystemClock{}
	}
	offlineThreshold := options.OfflineThreshold
	if offlineThreshold <= 0 {
		offlineThreshold = 90 * time.Second
	}
	commandTTL := options.CommandTTL
	if commandTTL <= 0 {
		commandTTL = 15 * time.Minute
	}
	return &Service{
		repository:          options.Repository,
		bindingReader:       options.BindingService,
		diagnosticReader:    options.DiagnosticReader,
		acknowledgementSink: options.AcknowledgementSink,
		commandPublisher:    options.CommandPublisher,
		firmwareUpdateAuth:  options.FirmwareUpdateAuthorizer,
		timeSource:          timeSource,
		offlineThreshold:    offlineThreshold,
		commandTTL:          commandTTL,
	}, nil
}

// SetCommandAcknowledgementSink wires the optional notification-delivery
// acknowledgement consumer after both services have been constructed.
func (s *Service) SetCommandAcknowledgementSink(
	sink CommandAcknowledgementSink,
) {
	s.acknowledgementSink = sink
}

// SetCommandPublisher wires the optional real-time command transport after the
// service graph is constructed. A nil publisher keeps command creation working
// with the durable row plus HTTP-poll recovery path only.
func (s *Service) SetCommandPublisher(publisher CommandPublisher) {
	s.commandPublisher = publisher
}

// SetFirmwareUpdateAuthorizer wires the OTA validation port after both
// services have been constructed.
func (s *Service) SetFirmwareUpdateAuthorizer(
	authorizer FirmwareUpdateAuthorizer,
) {
	s.firmwareUpdateAuth = authorizer
}

// RecordHeartbeat validates and persists one authenticated runtime snapshot.
func (s *Service) RecordHeartbeat(
	ctx context.Context,
	deviceSessionToken string,
	input domain.HeartbeatInput,
) (*domain.RuntimeStatus, error) {
	deviceID, err := s.bindingReader.VerifyDeviceSession(
		ctx,
		deviceSessionToken,
	)
	if err != nil {
		return nil, err
	}
	if !runtimeIdentifierPattern.MatchString(strings.TrimSpace(input.DeviceID)) ||
		input.DeviceID != deviceID {
		return nil, domain.ErrInvalidHeartbeat
	}
	if !runtimeIdentifierPattern.MatchString(strings.TrimSpace(input.HeartbeatID)) ||
		len([]rune(input.FirmwareVersion)) > 64 ||
		input.FirmwareVersion == "" {
		return nil, domain.ErrInvalidHeartbeat
	}
	// Firmware that predates thermal telemetry omits the temperature band.
	// Treat that as 'unknown' rather than rejecting the heartbeat.
	if input.Temperature == "" {
		input.Temperature = domain.TemperatureStateUnknown
	}
	if input.CapabilityRevision != 0 {
		if input.Capabilities == nil {
			return nil, domain.ErrInvalidHeartbeat
		}
		capabilities, capabilityErr := bindingdomain.NormalizeCapabilities(
			input.Capabilities,
		)
		if capabilityErr != nil {
			return nil, domain.ErrInvalidHeartbeat
		}
		input.Capabilities = capabilities
	}
	// Capabilities are optional so a firmware built before the runtime snapshot
	// contract can still report status. A capability list without a revision is
	// ambiguous and must never mutate the durable snapshot.
	if input.Capabilities != nil && input.CapabilityRevision == 0 {
		return nil, domain.ErrInvalidHeartbeat
	}
	if err := validateHeartbeatMetrics(input); err != nil {
		return nil, err
	}

	now := s.timeSource.Now().UTC()
	reportedAt := input.ReportedAt.UTC()
	if reportedAt.IsZero() || reportedAt.Before(now.Add(-24*time.Hour)) ||
		reportedAt.After(now.Add(5*time.Minute)) {
		// A device with a bad clock is still allowed to recover only after the
		// platform has accepted a plausible timestamp. Otherwise stale replay
		// data could overwrite the current status.
		return nil, domain.ErrInvalidHeartbeat
	}
	heartbeat := &domain.Heartbeat{
		DeviceID:           deviceID,
		HeartbeatID:        strings.TrimSpace(input.HeartbeatID),
		ReportedAt:         reportedAt,
		FirmwareVersion:    strings.TrimSpace(input.FirmwareVersion),
		Capabilities:       append([]string(nil), input.Capabilities...),
		CapabilityRevision: input.CapabilityRevision,
		ConnectionState:    input.ConnectionState,
		Transport:          input.Transport,
		NetworkQuality:     input.NetworkQuality,
		RSSIDBM:            input.RSSIDBM,
		LatencyMS:          input.LatencyMS,
		PacketLossPercent:  input.PacketLossPercent,
		Temperature:        input.Temperature,
		TemperatureTenths:  input.TemperatureTenths,
		TimeSyncState:      input.TimeSyncState,
		TimeSyncSource:     input.TimeSyncSource,
		TimeSyncedAt:       input.TimeSyncedAt,
		TimeOffsetMS:       input.TimeOffsetMS,
		OfflineState:       input.OfflineState,
		OfflineReason:      input.OfflineReason,
		FallbackActive:     input.FallbackActive,
		PendingTelemetry:   input.PendingTelemetry,
		ReceivedAt:         now,
		UpdatedAt:          now,
	}
	inserted, err := s.repository.SaveHeartbeat(ctx, heartbeat)
	if err != nil {
		return nil, err
	}
	if !inserted {
		// QoS 1 may redeliver the same heartbeat. Return the current state so
		// the device receives a successful acknowledgement without a second
		// status mutation.
		return s.GetStatus(ctx, deviceID)
	}
	status, err := s.GetStatus(ctx, deviceID)
	if err != nil {
		return nil, err
	}
	return status, nil
}

// GetStatus returns the latest runtime status for one bound device.
func (s *Service) GetStatus(
	ctx context.Context,
	deviceID string,
) (*domain.RuntimeStatus, error) {
	status, err := s.repository.GetStatus(ctx, deviceID)
	if err != nil {
		return nil, err
	}
	status.IsOnline = s.isOnline(status.Heartbeat)
	s.applyDiagnosticHint(ctx, status, deviceID)
	return status, nil
}

// applyDiagnosticHint attaches a bounded recent-crash count when a diagnostic
// reader is configured. A reader failure is non-fatal: the runtime status is
// still returned, just without the hint, so a diagnostic outage never blocks
// the guardian device list.
func (s *Service) applyDiagnosticHint(
	ctx context.Context,
	status *domain.RuntimeStatus,
	deviceID string,
) {
	if s.diagnosticReader == nil || status == nil {
		return
	}
	count, err := s.diagnosticReader.RecentCrashCount(ctx, deviceID)
	if err != nil {
		return
	}
	status.Diagnostic.RecentCrashCount = count
}

// GetDeviceStatus returns binding metadata plus the latest runtime snapshot.
func (s *Service) GetDeviceStatus(
	ctx context.Context,
	parentAccountID string,
	deviceID string,
) (*domain.DeviceStatus, error) {
	bindings, err := s.bindingReader.List(ctx, parentAccountID)
	if err != nil {
		return nil, err
	}
	for index := range bindings {
		if bindings[index].DeviceID != deviceID {
			continue
		}
		status := &domain.DeviceStatus{
			ParentAccountID: bindings[index].ParentAccountID,
			DeviceID:        bindings[index].DeviceID,
			DeviceName:      bindings[index].DeviceName,
			HardwareModel:   bindings[index].HardwareModel,
			FirmwareVersion: bindings[index].FirmwareVersion,
			Capabilities:    append([]string(nil), bindings[index].Capabilities...),
			LifecycleStatus: "active",
			BoundAt:         bindings[index].BoundAt,
			UpdatedAt:       bindings[index].UpdatedAt,
		}
		runtime, runtimeErr := s.GetStatus(ctx, deviceID)
		if runtimeErr != nil && !errors.Is(runtimeErr, domain.ErrDeviceNotFound) {
			return nil, runtimeErr
		}
		applyRuntimeSnapshot(status, runtime)
		status.Runtime = runtime
		return status, nil
	}
	return nil, bindingdomain.ErrDeviceNotFound
}

// ListDeviceStatuses returns runtime state for every device owned by a parent.
// Authenticated administrator transport reuses this method after resolving the
// target parent; authorization remains a transport concern.
func (s *Service) ListDeviceStatuses(
	ctx context.Context,
	parentAccountID string,
) ([]domain.DeviceStatus, error) {
	bindings, err := s.bindingReader.List(ctx, parentAccountID)
	if err != nil {
		return nil, err
	}
	deviceIDs := make([]string, 0, len(bindings))
	for index := range bindings {
		deviceIDs = append(deviceIDs, bindings[index].DeviceID)
	}
	statuses, err := s.repository.ListStatuses(ctx, deviceIDs)
	if err != nil {
		return nil, err
	}
	runtimeByDeviceID := make(map[string]domain.RuntimeStatus, len(statuses))
	for index := range statuses {
		statuses[index].IsOnline = s.isOnline(statuses[index].Heartbeat)
		s.applyDiagnosticHint(ctx, &statuses[index], statuses[index].DeviceID)
		runtimeByDeviceID[statuses[index].DeviceID] = statuses[index]
	}

	result := make([]domain.DeviceStatus, 0, len(bindings))
	for index := range bindings {
		status := domain.DeviceStatus{
			ParentAccountID: bindings[index].ParentAccountID,
			DeviceID:        bindings[index].DeviceID,
			DeviceName:      bindings[index].DeviceName,
			HardwareModel:   bindings[index].HardwareModel,
			FirmwareVersion: bindings[index].FirmwareVersion,
			Capabilities:    append([]string(nil), bindings[index].Capabilities...),
			LifecycleStatus: "active",
			BoundAt:         bindings[index].BoundAt,
			UpdatedAt:       bindings[index].UpdatedAt,
		}
		if runtime, exists := runtimeByDeviceID[bindings[index].DeviceID]; exists {
			applyRuntimeSnapshot(&status, &runtime)
			status.Runtime = &runtime
		}
		result = append(result, status)
	}
	return result, nil
}

// ListAllDeviceStatuses returns runtime state for the operations console.
func (s *Service) ListAllDeviceStatuses(
	ctx context.Context,
) ([]domain.DeviceStatus, error) {
	bindings, err := s.bindingReader.ListAllForAdmin(ctx)
	if err != nil {
		return nil, err
	}
	statuses, err := s.repository.ListAllStatuses(ctx)
	if err != nil {
		return nil, err
	}
	runtimeByDeviceID := make(map[string]domain.RuntimeStatus, len(statuses))
	for index := range statuses {
		statuses[index].IsOnline = s.isOnline(statuses[index].Heartbeat)
		s.applyDiagnosticHint(ctx, &statuses[index], statuses[index].DeviceID)
		runtimeByDeviceID[statuses[index].DeviceID] = statuses[index]
	}

	result := make([]domain.DeviceStatus, 0, len(bindings))
	for index := range bindings {
		status := domain.DeviceStatus{
			ParentAccountID: bindings[index].ParentAccountID,
			DeviceID:        bindings[index].DeviceID,
			DeviceName:      bindings[index].DeviceName,
			HardwareModel:   bindings[index].HardwareModel,
			FirmwareVersion: bindings[index].FirmwareVersion,
			Capabilities:    append([]string(nil), bindings[index].Capabilities...),
			LifecycleStatus: "active",
			BoundAt:         bindings[index].BoundAt,
			UpdatedAt:       bindings[index].UpdatedAt,
		}
		if runtime, exists := runtimeByDeviceID[bindings[index].DeviceID]; exists {
			applyRuntimeSnapshot(&status, &runtime)
			status.Runtime = &runtime
		}
		result = append(result, status)
	}
	return result, nil
}

// CheckReadiness reports aggregate runtime health for every active device
// credential. A device without a heartbeat is degraded because runtime status
// is required for the operator and guardian device lists; no registered device
// is unknown rather than a fabricated healthy state.
func (s *Service) CheckReadiness(
	ctx context.Context,
) (featurehealth.DeviceCapabilityReadiness, error) {
	reader, ok := s.repository.(interface {
		Readiness(
			context.Context,
			time.Duration,
		) (featurehealth.DeviceCapabilityReadiness, error)
	})
	if !ok {
		return featurehealth.DeviceCapabilityReadiness{
			Status: featurehealth.StatusUnavailable,
		}, nil
	}
	return reader.Readiness(ctx, s.offlineThreshold)
}

// applyRuntimeSnapshot uses the accepted runtime projection for guardian and
// operator views. Capabilities only become authoritative with a positive
// revision so legacy heartbeats cannot erase the registered feature set.
func applyRuntimeSnapshot(status *domain.DeviceStatus, runtime *domain.RuntimeStatus) {
	if status == nil || runtime == nil {
		return
	}
	status.FirmwareVersion = runtime.FirmwareVersion
	if runtime.CapabilityRevision > 0 {
		status.Capabilities = append([]string(nil), runtime.Capabilities...)
		status.CapabilityRevision = runtime.CapabilityRevision
	}
}

// CreateCommand validates and stores one operator maintenance action.
func (s *Service) CreateCommand(
	ctx context.Context,
	deviceID string,
	commandType domain.CommandType,
	requestedBy string,
	payload map[string]any,
) (*domain.Command, error) {
	if !runtimeIdentifierPattern.MatchString(strings.TrimSpace(deviceID)) {
		return nil, domain.ErrInvalidCommand
	}
	if !domain.CommandTypeIsValid(commandType) {
		return nil, domain.ErrInvalidCommand
	}
	if commandType == domain.CommandFirmwareUpdate {
		authorization, err := s.authorizeFirmwareUpdate(
			ctx,
			deviceID,
			payload,
		)
		if err != nil {
			return nil, err
		}
		payload = map[string]any{
			"release_id": authorization.ReleaseID,
		}
	} else if err := validateNonFirmwareCommandPayload(commandType, payload); err != nil {
		return nil, err
	}
	requestID, err := randomIdentifier("request")
	if err != nil {
		return nil, err
	}
	now := s.timeSource.Now().UTC()
	command := &domain.Command{
		ID:          uuid.NewString(),
		DeviceID:    deviceID,
		Type:        commandType,
		Payload:     payload,
		Status:      domain.CommandStatusPending,
		RequestedBy: strings.TrimSpace(requestedBy),
		RequestID:   requestID,
		CreatedAt:   now,
		ExpiresAt:   now.Add(s.commandTTL),
	}
	if err := s.repository.CreateCommand(ctx, command); err != nil {
		return nil, err
	}
	// Best-effort real-time push. The command row is already durable and the
	// device's authenticated poll remains the recovery path, so a transport
	// failure must not fail or roll back the operator's action.
	if s.commandPublisher != nil {
		_ = s.commandPublisher.PublishCommand(ctx, command.DeviceID, command)
	}
	return command, nil
}

func validateNonFirmwareCommandPayload(
	commandType domain.CommandType,
	payload map[string]any,
) error {
	if commandType == domain.CommandDisplayMessage {
		if len(payload) == 0 {
			return nil
		}
		allowed := map[string]bool{
			"notification_id":  true,
			"title":            true,
			"body":             true,
			"severity":         true,
			"duration_seconds": true,
			"category":         true,
		}
		for key := range payload {
			if !allowed[key] {
				return domain.ErrInvalidCommand
			}
		}
		notificationID, ok := payload["notification_id"].(string)
		if !ok || strings.TrimSpace(notificationID) == "" {
			return domain.ErrInvalidCommand
		}
		return nil
	}
	if len(payload) != 0 {
		return domain.ErrInvalidCommand
	}
	return nil
}

func (s *Service) authorizeFirmwareUpdate(
	ctx context.Context,
	deviceID string,
	payload map[string]any,
) (*FirmwareUpdateAuthorization, error) {
	releaseID, ok := domain.FirmwareUpdateReleaseID(payload)
	if !ok || uuid.Validate(strings.TrimSpace(releaseID)) != nil {
		return nil, domain.ErrInvalidCommand
	}
	if s.firmwareUpdateAuth == nil {
		return nil, domain.ErrFirmwareUpdateUnavailable
	}
	authorization, err := s.firmwareUpdateAuth.AuthorizeRuntimeFirmwareUpdate(
		ctx,
		deviceID,
		strings.TrimSpace(releaseID),
	)
	if err != nil {
		return nil, err
	}
	if authorization == nil ||
		strings.TrimSpace(authorization.ReleaseID) != strings.TrimSpace(releaseID) ||
		!validReleaseID(authorization.ReleaseID) ||
		!validReleaseID(authorization.DeploymentID) ||
		authorization.FirmwareVersion == "" ||
		(authorization.DeploymentKind != "install" &&
			authorization.DeploymentKind != "rollback") {
		return nil, domain.ErrInvalidCommand
	}
	return authorization, nil
}

func validReleaseID(value string) bool {
	parsed, err := uuid.Parse(strings.TrimSpace(value))
	return err == nil &&
		parsed != uuid.Nil &&
		parsed.String() == strings.TrimSpace(value)
}

// ListCommands returns recent maintenance actions for one device.
func (s *Service) ListCommands(
	ctx context.Context,
	deviceID string,
) ([]domain.Command, error) {
	if !runtimeIdentifierPattern.MatchString(strings.TrimSpace(deviceID)) {
		return nil, domain.ErrInvalidCommand
	}
	return s.repository.ListCommands(ctx, deviceID)
}

// ListPendingCommands resolves the authenticated device and returns commands
// that still need an acknowledgement.
func (s *Service) ListPendingCommands(
	ctx context.Context,
	deviceSessionToken string,
) ([]domain.Command, error) {
	deviceID, err := s.bindingReader.VerifyDeviceSession(ctx, deviceSessionToken)
	if err != nil {
		return nil, err
	}
	commands, err := s.repository.ListPendingCommands(ctx, deviceID)
	if err != nil {
		return nil, err
	}
	now := s.timeSource.Now().UTC()
	activeCommands := make([]domain.Command, 0, len(commands))
	for _, command := range commands {
		if !command.ExpiresAt.IsZero() && !command.ExpiresAt.After(now) {
			continue
		}
		activeCommands = append(activeCommands, command)
	}
	commands = activeCommands
	commandIDs := make([]string, 0, len(commands))
	for index := range commands {
		if commands[index].Status != domain.CommandStatusPending {
			continue
		}
		commandIDs = append(commandIDs, commands[index].ID)
		commands[index].Status = domain.CommandStatusDelivered
		commands[index].DeliveredAt = &now
	}
	if err := s.repository.MarkCommandsDelivered(ctx, commandIDs, deviceID); err != nil {
		return nil, err
	}
	return commands, nil
}

// AcknowledgeCommand records a device result exactly once.
func (s *Service) AcknowledgeCommand(
	ctx context.Context,
	deviceSessionToken string,
	pathDeviceID string,
	commandID string,
	status domain.CommandStatus,
	resultCode string,
) error {
	if !runtimeIdentifierPattern.MatchString(strings.TrimSpace(commandID)) &&
		!isUUID(commandID) {
		return domain.ErrInvalidCommand
	}
	if status != domain.CommandStatusAcknowledged &&
		status != domain.CommandStatusFailed {
		return domain.ErrInvalidCommand
	}
	deviceID, err := s.bindingReader.VerifyDeviceSession(ctx, deviceSessionToken)
	if err != nil {
		return err
	}
	if strings.TrimSpace(pathDeviceID) != deviceID {
		return bindingdomain.ErrInvalidDeviceProof
	}
	if err := s.repository.AcknowledgeCommand(
		ctx,
		commandID,
		deviceID,
		status,
		strings.TrimSpace(resultCode),
	); err != nil {
		return err
	}
	if s.acknowledgementSink != nil {
		if err := s.acknowledgementSink.MarkDeliveryDeliveredByCommand(
			ctx,
			commandID,
			deviceID,
		); err != nil {
			// The command result is already durable. Surface the bookkeeping
			// failure so the MQTT worker records it, while a device retry stays
			// idempotent and cannot roll back the command.
			return err
		}
	}
	return nil
}

// AcknowledgeCommandFromDevice records a device result that arrived on the
// authenticated MQTT channel.
//
// The MQTT broker authenticates the device by mutual TLS and maps the
// certificate identity to the device id, so there is no HTTP session token
// here. The device id and the command id must still match the durable command
// row, and the repository update is the same single-writer idempotent path the
// HTTP acknowledgement uses, so a retransmitted ack never double-applies.
func (s *Service) AcknowledgeCommandFromDevice(
	ctx context.Context,
	deviceID string,
	commandID string,
	status domain.CommandStatus,
	resultCode string,
) error {
	deviceID = strings.TrimSpace(deviceID)
	commandID = strings.TrimSpace(commandID)
	if !runtimeIdentifierPattern.MatchString(deviceID) {
		return domain.ErrInvalidCommand
	}
	if !runtimeIdentifierPattern.MatchString(commandID) && !isUUID(commandID) {
		return domain.ErrInvalidCommand
	}
	if status != domain.CommandStatusAcknowledged &&
		status != domain.CommandStatusFailed {
		return domain.ErrInvalidCommand
	}
	if err := s.repository.AcknowledgeCommand(
		ctx,
		commandID,
		deviceID,
		status,
		strings.TrimSpace(resultCode),
	); err != nil {
		if errors.Is(err, domain.ErrCommandNotFound) {
			existing, lookupErr := s.repository.GetCommand(ctx, commandID)
			if lookupErr == nil &&
				existing.DeviceID == deviceID &&
				(existing.Status == domain.CommandStatusAcknowledged ||
					existing.Status == domain.CommandStatusFailed) {
				// QoS 1 can redeliver an ack after the first write committed.
				// The MQTT path is already authenticated by device identity,
				// so a terminal row is an idempotent success, not an unknown
				// command. The HTTP path keeps its explicit 409 contract.
				return nil
			}
		}
		return err
	}
	if s.acknowledgementSink != nil {
		if err := s.acknowledgementSink.MarkDeliveryDeliveredByCommand(
			ctx,
			commandID,
			deviceID,
		); err != nil {
			return err
		}
	}
	return nil
}

// ResolveDeviceFamily verifies that the path device id belongs to the
// authenticated device session and returns the owning parent account id. The
// binding lookup is server-side; a client cannot choose a family id.
func (s *Service) ResolveDeviceFamily(
	ctx context.Context,
	deviceSessionToken string,
	pathDeviceID string,
) (string, error) {
	deviceID, err := s.bindingReader.VerifyDeviceSession(ctx, deviceSessionToken)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(pathDeviceID) != deviceID {
		return "", bindingdomain.ErrInvalidDeviceProof
	}
	binding, err := s.bindingReader.GetByDeviceID(ctx, deviceID)
	if err != nil {
		return "", err
	}
	return binding.ParentAccountID, nil
}

// OfflineThreshold returns the server-side online threshold for contracts.
func (s *Service) OfflineThreshold() time.Duration {
	return s.offlineThreshold
}

func (s *Service) isOnline(heartbeat domain.Heartbeat) bool {
	freshnessSource := heartbeat.ReportedAt
	if freshnessSource.IsZero() {
		// Status rows written before reported_at was available still have a
		// trustworthy server receive time. New heartbeats always set both.
		freshnessSource = heartbeat.ReceivedAt
	}
	return heartbeat.ConnectionState == domain.ConnectionStateOnline &&
		s.timeSource.Now().UTC().Sub(freshnessSource) <= s.offlineThreshold
}

func validateHeartbeatMetrics(input domain.HeartbeatInput) error {
	switch input.ConnectionState {
	case domain.ConnectionStateOffline,
		domain.ConnectionStateConnecting,
		domain.ConnectionStateOnline:
	default:
		return domain.ErrInvalidHeartbeat
	}
	switch input.Transport {
	case domain.TransportNone, domain.TransportWiFi, domain.TransportCellular4G:
	default:
		return domain.ErrInvalidHeartbeat
	}
	switch input.NetworkQuality {
	case domain.NetworkQualityUnknown,
		domain.NetworkQualityPoor,
		domain.NetworkQualityFair,
		domain.NetworkQualityGood,
		domain.NetworkQualityExcellent:
	default:
		return domain.ErrInvalidHeartbeat
	}
	if input.RSSIDBM < -127 || input.RSSIDBM > 0 ||
		input.LatencyMS < 0 || input.LatencyMS > 60000 ||
		input.PacketLossPercent < 0 || input.PacketLossPercent > 100 {
		return domain.ErrInvalidHeartbeat
	}
	switch input.TimeSyncState {
	case domain.TimeSyncUnsynchronized,
		domain.TimeSyncSynchronizing,
		domain.TimeSyncSynchronized:
	default:
		return domain.ErrInvalidHeartbeat
	}
	switch input.TimeSyncSource {
	case domain.TimeSyncSourceNone,
		domain.TimeSyncSourceSNTP,
		domain.TimeSyncSourcePlatform:
	default:
		return domain.ErrInvalidHeartbeat
	}
	if input.TimeOffsetMS < -60000 || input.TimeOffsetMS > 60000 ||
		input.PendingTelemetry < 0 || input.PendingTelemetry > 10000 {
		return domain.ErrInvalidHeartbeat
	}
	switch input.OfflineState {
	case domain.OfflineStateOnline,
		domain.OfflineStateGrace,
		domain.OfflineStateOffline:
	default:
		return domain.ErrInvalidHeartbeat
	}
	switch input.OfflineReason {
	case domain.OfflineReasonNone,
		domain.OfflineReasonNetworkUnavailable,
		domain.OfflineReasonAuthenticationFailed,
		domain.OfflineReasonTimeNotSynchronized,
		domain.OfflineReasonServiceUnavailable:
	default:
		return domain.ErrInvalidHeartbeat
	}
	if input.ConnectionState == domain.ConnectionStateOnline {
		if input.OfflineState != domain.OfflineStateOnline ||
			input.OfflineReason != domain.OfflineReasonNone ||
			input.FallbackActive {
			return domain.ErrInvalidHeartbeat
		}
	} else {
		if input.OfflineState == domain.OfflineStateOnline ||
			input.OfflineReason == domain.OfflineReasonNone ||
			(input.OfflineState == domain.OfflineStateGrace && input.FallbackActive) {
			return domain.ErrInvalidHeartbeat
		}
	}
	switch input.Temperature {
	case domain.TemperatureStateUnknown,
		domain.TemperatureStateNormal,
		domain.TemperatureStateWarm,
		domain.TemperatureStateHot,
		domain.TemperatureStateCritical:
	default:
		return domain.ErrInvalidHeartbeat
	}
	if input.TemperatureTenths != nil {
		if *input.TemperatureTenths < -400 || *input.TemperatureTenths > 1250 {
			return domain.ErrInvalidHeartbeat
		}
	}
	return nil
}

func randomIdentifier(prefix string) (string, error) {
	bytes := make([]byte, 12)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	return fmt.Sprintf("%s_%s", prefix, hex.EncodeToString(bytes)), nil
}

func isUUID(value string) bool {
	_, err := uuid.Parse(value)
	return err == nil
}

var _ BindingReader = (*bindingservice.Service)(nil)
