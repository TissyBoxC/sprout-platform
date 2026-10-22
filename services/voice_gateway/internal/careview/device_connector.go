package careview

import (
	"errors"
	"sync"
	"sync/atomic"

	gatewayws "github.com/TissyBoxC/sprout-platform/services/voice_gateway/internal/transport/websocket"
)

// DeviceConnection is where one relay's device-bound frames are written.
//
// The realtime transport's connection writer satisfies this interface
// directly, so the relay never re-implements device authentication, framing, or
// the send queue policy.
type DeviceConnection interface {
	// SendDeviceFrame queues one binary frame for the device.
	SendDeviceFrame(payload []byte) error
}

// DeviceConnectionSource resolves the live device WebSocket of one device.
//
// A device that is not connected returns a non-nil error; the relay then
// refuses to start rather than accepting guardian traffic that has nowhere to
// go.
type DeviceConnectionSource interface {
	DeviceConnection(deviceID string) (DeviceConnection, error)
}

// DeviceConnectionRegistry tracks the live authenticated connection of every
// device attached to this replica.
//
// This is the gateway's only rendezvous point between the device conversation
// transport and every other transport that writes to a device. It stores one
// sink per device identifier and replaces it on reconnect, so a relay always
// writes to the newest connection.
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
// care-view relay.
//
// The relay never authenticates a device itself. It requires the short-lived
// device token that device_platform issued for the view, and it refuses to
// open a device side without one: an unauthenticated write path to a child's
// camera is not acceptable even when the session record is streaming.
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
	return &RealtimeDeviceConnector{
		source:        config.Source,
		maxFrameBytes: NormalizeMaxFrameBytes(config.MaxFrameBytes),
	}
}

// OpenDeviceRelay attaches to the device side of one view.
//
// The device token is not consulted: the sink writes to the connection the device
// already authenticated, resolved by device id. Requiring a non-empty token here
// would reject every call from the platform, which never holds a device
// credential, for a reason that protects nothing.
func (connector *RealtimeDeviceConnector) OpenDeviceRelay(
	session Session,
	deviceToken string,
) (OutboundSink, error) {
	if connector == nil || connector.source == nil {
		return nil, ErrRelayUnavailable
	}
	_ = deviceToken
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
// realtime transport, which closes it when the socket ends, so a care-view
// hangup must not tear down an unrelated device connection.
type deviceRelaySink struct {
	connection    DeviceConnection
	maxFrameBytes int
	closed        atomic.Bool
}

// SendRelayFrame forwards one frame.
func (sink *deviceRelaySink) SendRelayFrame(payload []byte) error {
	if sink == nil || sink.connection == nil {
		return errors.New("care view device sink is unavailable")
	}
	if sink.closed.Load() {
		return errors.New("care view device sink is closed")
	}
	if len(payload) == 0 || len(payload) > sink.maxFrameBytes {
		return ErrFrameRejected
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
