package careview

import (
	"errors"
	"sync"
	"testing"
	"time"
)

// deviceSink records frames written through the transport-level sink.
type deviceSink struct {
	mutex  sync.Mutex
	frames [][]byte
}

func (sink *deviceSink) SendDeviceFrame(payload []byte) error {
	sink.mutex.Lock()
	defer sink.mutex.Unlock()
	sink.frames = append(sink.frames, append([]byte(nil), payload...))
	return nil
}

func (sink *deviceSink) frameCount() int {
	sink.mutex.Lock()
	defer sink.mutex.Unlock()
	return len(sink.frames)
}

func TestDeviceConnectionRegistryRegisterAndRelease(t *testing.T) {
	registry := NewDeviceConnectionRegistry()
	first := &deviceSink{}
	release := registry.Register("device_alpha", first)
	if _, err := registry.DeviceConnection("device_alpha"); err != nil {
		t.Fatalf("DeviceConnection() error = %v, want a live connection", err)
	}

	// A fast reconnect replaces the entry; the previous socket's release must
	// not unregister the new one.
	second := &deviceSink{}
	_ = registry.Register("device_alpha", second)
	release()
	connection, err := registry.DeviceConnection("device_alpha")
	if err != nil {
		t.Fatalf("DeviceConnection() error = %v after stale release", err)
	}
	if connection != second {
		t.Fatal("a stale release removed the replacement connection")
	}

	// Releasing the current connection removes it.
	registry.Register("device_alpha", second)()
	if _, err := registry.DeviceConnection("device_alpha"); !errors.Is(err, ErrDeviceNotConnected) {
		t.Fatalf("DeviceConnection() error = %v, want ErrDeviceNotConnected", err)
	}
}

func TestDeviceConnectionRegistryUnknownDevice(t *testing.T) {
	registry := NewDeviceConnectionRegistry()
	if _, err := registry.DeviceConnection("device_missing"); !errors.Is(err, ErrDeviceNotConnected) {
		t.Fatalf("DeviceConnection() error = %v, want ErrDeviceNotConnected", err)
	}
	var nilRegistry *DeviceConnectionRegistry
	if _, err := nilRegistry.DeviceConnection("device_alpha"); !errors.Is(err, ErrDeviceNotConnected) {
		t.Fatalf("nil registry error = %v, want ErrDeviceNotConnected", err)
	}
}

func TestRealtimeDeviceConnectorRequiresDeviceTokenAndConnection(t *testing.T) {
	registry := NewDeviceConnectionRegistry()
	connector := NewRealtimeDeviceConnector(RealtimeConnectorConfig{
		Source:        registry,
		MaxFrameBytes: 1024,
	})
	session := testSession(time.Now().Add(time.Minute))

	if _, err := connector.OpenDeviceRelay(session, "  "); err == nil {
		t.Fatal("an empty device token must not open a device side")
	}
	if _, err := connector.OpenDeviceRelay(session, "device-token"); !errors.Is(err, ErrDeviceNotConnected) {
		t.Fatalf("OpenDeviceRelay() error = %v, want ErrDeviceNotConnected", err)
	}

	sink := &deviceSink{}
	registry.Register(session.DeviceID, sink)
	outbound, err := connector.OpenDeviceRelay(session, "device-token")
	if err != nil {
		t.Fatalf("OpenDeviceRelay() error = %v", err)
	}
	if err := outbound.SendRelayFrame([]byte("jpeg-frame")); err != nil {
		t.Fatalf("SendRelayFrame() error = %v", err)
	}
	if sink.frameCount() != 1 {
		t.Fatalf("device frames = %d, want 1", sink.frameCount())
	}
	// The connector's own size limit refuses an oversized frame before it can
	// reach the device writer.
	if err := outbound.SendRelayFrame(make([]byte, 2048)); !errors.Is(err, ErrFrameRejected) {
		t.Fatalf("oversized SendRelayFrame() error = %v, want ErrFrameRejected", err)
	}
	outbound.Close()
	if err := outbound.SendRelayFrame([]byte("jpeg-frame")); err == nil {
		t.Fatal("a closed sink accepted a frame")
	}
}

func TestRealtimeDeviceConnectorWithoutSource(t *testing.T) {
	connector := NewRealtimeDeviceConnector(RealtimeConnectorConfig{})
	if _, err := connector.OpenDeviceRelay(testSession(time.Now().Add(time.Minute)), "device-token"); !errors.Is(err, ErrRelayUnavailable) {
		t.Fatalf("OpenDeviceRelay() error = %v, want ErrRelayUnavailable", err)
	}
}
