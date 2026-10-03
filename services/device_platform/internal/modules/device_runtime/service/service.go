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
	"github.com/TissyBoxC/sprout-platform/services/device_platform/internal/platform/clock"
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

// Service validates, stores, and exposes device runtime state.
type Service struct {
	repository       repository.Repository
	bindingReader    BindingReader
	timeSource       clock.Clock
	offlineThreshold time.Duration
	commandTTL       time.Duration
}

// Options contains runtime service dependencies and policy.
type Options struct {
	Repository       repository.Repository
	BindingService   BindingReader
	Clock            clock.Clock
	OfflineThreshold time.Duration
	CommandTTL       time.Duration
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
		repository:       options.Repository,
		bindingReader:    options.BindingService,
		timeSource:       timeSource,
		offlineThreshold: offlineThreshold,
		commandTTL:       commandTTL,
	}, nil
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
		DeviceID:          deviceID,
		HeartbeatID:       strings.TrimSpace(input.HeartbeatID),
		ReportedAt:        reportedAt,
		FirmwareVersion:   strings.TrimSpace(input.FirmwareVersion),
		ConnectionState:   input.ConnectionState,
		Transport:         input.Transport,
		NetworkQuality:    input.NetworkQuality,
		RSSIDBM:           input.RSSIDBM,
		LatencyMS:         input.LatencyMS,
		PacketLossPercent: input.PacketLossPercent,
		TimeSyncState:     input.TimeSyncState,
		TimeSyncSource:    input.TimeSyncSource,
		TimeSyncedAt:      input.TimeSyncedAt,
		TimeOffsetMS:      input.TimeOffsetMS,
		OfflineState:      input.OfflineState,
		OfflineReason:     input.OfflineReason,
		FallbackActive:    input.FallbackActive,
		PendingTelemetry:  input.PendingTelemetry,
		ReceivedAt:        now,
		UpdatedAt:         now,
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
	return status, nil
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
			runtimeCopy := runtime
			status.Runtime = &runtimeCopy
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
			runtimeCopy := runtime
			status.Runtime = &runtimeCopy
		}
		result = append(result, status)
	}
	return result, nil
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
	if commandType != domain.CommandRefreshConfiguration &&
		commandType != domain.CommandReconnectNetwork &&
		commandType != domain.CommandResyncTime {
		return nil, domain.ErrInvalidCommand
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
	}
	if err := s.repository.CreateCommand(ctx, command); err != nil {
		return nil, err
	}
	return command, nil
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
	return s.repository.ListPendingCommands(ctx, deviceID)
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
	return s.repository.AcknowledgeCommand(
		ctx,
		commandID,
		deviceID,
		status,
		strings.TrimSpace(resultCode),
	)
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
	return heartbeat.ConnectionState == domain.ConnectionStateOnline &&
		s.timeSource.Now().UTC().Sub(heartbeat.ReceivedAt) <= s.offlineThreshold
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
