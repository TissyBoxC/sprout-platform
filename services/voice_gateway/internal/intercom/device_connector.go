package intercom

import (
	"errors"
	"strings"
	"sync"
	"sync/atomic"

	gatewayws "github.com/TissyBoxC/sprout-platform/services/voice_gateway/internal/transport/websocket"
)

// ErrDeviceNotConnected is returned when the device has no live realtime
// connection on this gateway replica.
var ErrDeviceNotConnected = errors.New("device is not connected")

// DeviceConnection is where one relay's device frames are written.
//
// The realtime transport's connection writer satisfies this interface
// directly, so the relay never re-implements device authentication or frame
// queues.
type DeviceConnection interface {
	// SendDeviceFrame queues one binary frame for the device.
	SendDeviceFrame(payload []byte) error
}

// DeviceConnectionSource resolves the live device WebSocket of one device.
//
// A device that is not connected returns a non-nil error; the relay then
// refuses to start rather than accepting guardian audio that has nowhere to
// go.
type DeviceConnectionSource interface {
	DeviceConnection(deviceID string) (DeviceConnection, error)
}

// DeviceConnectionRegistry tracks the live authenticated realtime connection of
// every device attached to this replica.
//
// The registry is the only rendezvous point between the device conversation
// transport and the guardian intercom relay. It stores one sink per device
// identifier and replaces it on reconnect, so a relay always writes to the
// newest connection.
type DeviceConnectionRegistry struct {
	mutex       sync.RWMutex
	connections map[string]gatewayws.DeviceFrameSink
}

// NewDeviceConnectionRegistry creates an empty registry.
func NewDeviceConnectionRegistry() *DeviceConnectionRegistry {
	return &DeviceConnectionRegistry{connections: make(map[string]gatewayws.DeviceFrameSink)}
}

// Register records one live device connection and returns the release
// function the transport must call when the connection ends.
//
// Release is idempotent and only removes the entry when it still refers to
// this exact connection, so a fast reconnect cannot be unregistered by the
// previous socket's teardown.
func (registry *DeviceConnectionRegistry) Register(
	deviceID string,
	connection gatewayws.DeviceFrameSink,
) func() {
	if registry == nil || deviceID == "" || connection == nil {
		return func() {}
	}
	registry.mutex.Lock()
	registry.connections[deviceID] = connection
	registry.mutex.Unlock()

	var once sync.Once
	return func() {
		once.Do(func() {
			registry.mutex.Lock()
			if current, exists := registry.connections[deviceID]; exists && current == connection {
				delete(registry.connections, deviceID)
			}
			registry.mutex.Unlock()
		})
	}
}

// DeviceConnection returns the live connection of one device.
func (registry *DeviceConnectionRegistry) DeviceConnection(
	deviceID string,
) (DeviceConnection, error) {
	if registry == nil {
		return nil, ErrDeviceNotConnected
	}
	registry.mutex.RLock()
	connection, exists := registry.connections[deviceID]
	registry.mutex.RUnlock()
	if !exists || connection == nil {
		return nil, ErrDeviceNotConnected
	}
	return connection, nil
}

// RealtimeDeviceConnector bridges the realtime device transport into the
// intercom relay.
//
// The relay never authenticates a device itself. It requires the short-lived
// device token that device_platform holds for the call, and it refuses to open
// a device side without one: an unauthenticated write path to child hardware
// is not acceptable even when the call record is active.
type RealtimeDeviceConnector struct {
	source DeviceConnectionSource
	// maxFrameBytes bounds one frame handed to the device queue.
	maxFrameBytes int
}

// RealtimeConnectorConfig configures the realtime device connector.
type RealtimeConnectorConfig struct {
	Source        DeviceConnectionSource
	MaxFrameBytes int
}

// NewRealtimeDeviceConnector creates the connector.
func NewRealtimeDeviceConnector(config RealtimeConnectorConfig) *RealtimeDeviceConnector {
	maxFrameBytes := config.MaxFrameBytes
	if maxFrameBytes <= 0 {
		maxFrameBytes = 4096
	}
	return &RealtimeDeviceConnector{
		source:        config.Source,
		maxFrameBytes: maxFrameBytes,
	}
}

// OpenDeviceRelay attaches to the device side of one call.
func (connector *RealtimeDeviceConnector) OpenDeviceRelay(
	session Session,
	deviceToken string,
) (OutboundSink, error) {
	if connector == nil || connector.source == nil {
		return nil, ErrRelayUnavailable
	}
	if strings.TrimSpace(deviceToken) == "" {
		return nil, errors.New("intercom relay requires a device token")
	}
	connection, err := connector.source.DeviceConnection(session.DeviceID)
	if err != nil {
		return nil, err
	}
	if connection == nil {
		return nil, ErrDeviceNotConnected
	}
	return &deviceRelaySink{
		connection:    connection,
		maxFrameBytes: connector.maxFrameBytes,
	}, nil
}

// deviceRelaySink writes relay frames onto an existing device connection.
//
// Closing the relay releases the sink only. The device WebSocket belongs to the
// realtime transport, which closes it when the socket ends, so an intercom
// hangup must not tear down an unrelated conversation connection.
type deviceRelaySink struct {
	connection    DeviceConnection
	maxFrameBytes int
	closed        atomic.Bool
}

// SendRelayFrame forwards one frame.
func (sink *deviceRelaySink) SendRelayFrame(payload []byte) error {
	if sink == nil || sink.connection == nil {
		return errors.New("intercom device sink is unavailable")
	}
	if sink.closed.Load() {
		return errors.New("intercom device sink is closed")
	}
	if len(payload) == 0 || len(payload) > sink.maxFrameBytes {
		return errors.New("intercom device frame is outside the allowed size")
	}
	return sink.connection.SendDeviceFrame(payload)
}

// Close stops further writes and releases the sink.
func (sink *deviceRelaySink) Close() {
	if sink == nil {
		return
	}
	sink.closed.Store(true)
}
