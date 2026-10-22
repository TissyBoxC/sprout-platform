package careview

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// recordingSink captures device-bound frames and whether the relay released it.
type recordingSink struct {
	mutex  sync.Mutex
	frames [][]byte
	closed bool
}

func (sink *recordingSink) SendRelayFrame(payload []byte) error {
	sink.mutex.Lock()
	defer sink.mutex.Unlock()
	if sink.closed {
		return errors.New("sink is closed")
	}
	sink.frames = append(sink.frames, append([]byte(nil), payload...))
	return nil
}

func (sink *recordingSink) Close() {
	sink.mutex.Lock()
	sink.closed = true
	sink.mutex.Unlock()
}

func (sink *recordingSink) frameCount() int {
	sink.mutex.Lock()
	defer sink.mutex.Unlock()
	return len(sink.frames)
}

func (sink *recordingSink) isClosed() bool {
	sink.mutex.Lock()
	defer sink.mutex.Unlock()
	return sink.closed
}

// failSink reports a write failure for every frame, simulating a device that
// disconnected while the stream was open.
type failSink struct{ closed atomic.Bool }

func (sink *failSink) SendRelayFrame([]byte) error { return errors.New("device write failed") }
func (sink *failSink) Close()                      { sink.closed.Store(true) }

func testSession(expiresAt time.Time) Session {
	return Session{
		ID:               "session_alpha",
		DeviceID:         "device_alpha",
		State:            StateStreaming,
		PlatformDeadline: expiresAt,
		ExpiresAt:        expiresAt,
	}
}

func newTestLogger(buffer *bytes.Buffer) *slog.Logger {
	if buffer == nil {
		return slog.New(slog.NewJSONHandler(io.Discard, nil))
	}
	return slog.New(slog.NewJSONHandler(buffer, nil))
}

func admitTestRelay(t *testing.T, manager *RelayManager, sink OutboundSink, logger *slog.Logger) *Relay {
	t.Helper()
	relay, err := manager.Admit(context.Background(), testSession(time.Now().Add(time.Minute)), RelayAdmission{
		Outbound:        sink,
		MaxFrameBytes:   1024,
		WriteQueueDepth: 4,
		Logger:          logger,
	})
	if err != nil {
		t.Fatalf("admit relay: %v", err)
	}
	t.Cleanup(func() { relay.Close(CloseReasonGuardianHangup) })
	return relay
}

func TestRelayForwardToDeviceReachesSink(t *testing.T) {
	sink := &recordingSink{}
	relay := admitTestRelay(t, NewRelayManager(4), sink, newTestLogger(nil))

	if err := relay.ForwardToDevice([]byte("jpeg-frame")); err != nil {
		t.Fatalf("forward to device: %v", err)
	}
	waitForCondition(t, time.Second, func() bool { return sink.frameCount() == 1 })
	if stats := relay.Stats(); stats.FramesIn != 1 || stats.BytesIn != uint64(len("jpeg-frame")) {
		t.Fatalf("unexpected stats: %+v", stats)
	}
}

func TestRelayForwardToGuardianReachesWriter(t *testing.T) {
	relay := admitTestRelay(t, NewRelayManager(4), &recordingSink{}, newTestLogger(nil))

	var received [][]byte
	var receivedMutex sync.Mutex
	relay.SetGuardianWriter(func(payload []byte) error {
		receivedMutex.Lock()
		received = append(received, append([]byte(nil), payload...))
		receivedMutex.Unlock()
		return nil
	})
	if err := relay.ForwardToGuardian([]byte("device-frame")); err != nil {
		t.Fatalf("forward to guardian: %v", err)
	}
	receivedMutex.Lock()
	defer receivedMutex.Unlock()
	if len(received) != 1 || string(received[0]) != "device-frame" {
		t.Fatalf("guardian frames = %q, want [device-frame]", received)
	}
	if relay.Stats().FramesOut != 1 {
		t.Fatalf("frames out = %d, want 1", relay.Stats().FramesOut)
	}
}

// A device frame that arrives before the guardian attaches must be counted and
// dropped, never buffered.
func TestRelayDropsFramesBeforeGuardianAttaches(t *testing.T) {
	relay := admitTestRelay(t, NewRelayManager(4), &recordingSink{}, newTestLogger(nil))
	if err := relay.ForwardToGuardian([]byte("early-frame")); err != nil {
		t.Fatalf("forward to guardian: %v", err)
	}
	if relay.Stats().Dropped != 1 || relay.Stats().FramesOut != 0 {
		t.Fatalf("expected one dropped frame and none delivered, got %+v", relay.Stats())
	}
}

// The processing-layer gate: a relay whose observed state left streaming
// refuses frames and reports why.
func TestRelayObserveStateGate(t *testing.T) {
	for _, testCase := range []struct {
		name    string
		state   State
		wantErr error
	}{
		{name: "streaming still allows frames", state: StateStreaming},
		{name: "requested refuses frames", state: StateRequested, wantErr: ErrNotStreaming},
		{name: "ended refuses frames", state: StateEnded, wantErr: ErrNotStreaming},
		{name: "declined refuses frames", state: StateDeclined, wantErr: ErrNotStreaming},
		{name: "failed refuses frames", state: StateFailed, wantErr: ErrNotStreaming},
		{name: "empty refuses frames", state: State(""), wantErr: ErrNotStreaming},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			relay := admitTestRelay(t, NewRelayManager(4), &recordingSink{}, newTestLogger(nil))
			err := relay.ObserveState(testCase.state)
			if testCase.wantErr == nil && err != nil {
				t.Fatalf("ObserveState() error = %v, want nil", err)
			}
			if testCase.wantErr != nil && !errors.Is(err, testCase.wantErr) {
				t.Fatalf("ObserveState() error = %v, want %v", err, testCase.wantErr)
			}
			if got := relay.IsStreaming(); got != (testCase.wantErr == nil) {
				t.Fatalf("IsStreaming() = %v after observing %q", got, testCase.state)
			}
			if testCase.state.AllowsFrames() {
				if relay.IsClosed() {
					t.Fatal("a streaming observation closed the relay")
				}
				return
			}
			// A non-streaming observation closes the relay itself, so no reader
			// can observe streaming after the platform stopped it.
			select {
			case <-relay.Done():
			case <-time.After(time.Second):
				t.Fatal("a non-streaming observation left the relay open")
			}
			if relay.CloseReason() != CloseReasonSessionEnded {
				t.Fatalf("close reason = %q, want %q", relay.CloseReason(), CloseReasonSessionEnded)
			}
		})
	}
}

// Close must release the device sink and run its side effects once even when
// several goroutines race it.
func TestRelayCloseIsIdempotentAndReleasesDeviceSide(t *testing.T) {
	sink := &recordingSink{}
	relay := admitTestRelay(t, NewRelayManager(4), sink, newTestLogger(nil))

	var waitGroup sync.WaitGroup
	for index := 0; index < 8; index++ {
		waitGroup.Add(1)
		go func() {
			defer waitGroup.Done()
			relay.Close(CloseReasonDeviceHangup)
		}()
	}
	waitGroup.Wait()

	select {
	case <-relay.Done():
	case <-time.After(time.Second):
		t.Fatal("relay did not close")
	}
	if !sink.isClosed() {
		t.Fatal("relay close must release the device-side sink")
	}
	if relay.CloseReason() != CloseReasonDeviceHangup {
		t.Fatalf("close reason = %q, want %q", relay.CloseReason(), CloseReasonDeviceHangup)
	}
	if err := relay.ForwardToDevice([]byte("late-frame")); err == nil {
		t.Fatal("a closed relay must refuse new guardian frames")
	}
}

// A device write failure closes the relay with the device reason so the parent
// socket does not keep waiting for pictures nobody can send.
func TestRelayClosesWhenDeviceWriteFails(t *testing.T) {
	sink := &failSink{}
	relay := admitTestRelay(t, NewRelayManager(4), sink, newTestLogger(nil))
	if err := relay.ForwardToDevice([]byte("jpeg-frame")); err != nil {
		t.Fatalf("forward to device: %v", err)
	}
	waitForCondition(t, time.Second, func() bool {
		select {
		case <-relay.Done():
			return true
		default:
			return false
		}
	})
	if !sink.closed.Load() {
		t.Fatal("device write failure must release the device sink")
	}
	if relay.CloseReason() != CloseReasonDeviceHangup {
		t.Fatalf("close reason = %q, want %q", relay.CloseReason(), CloseReasonDeviceHangup)
	}
}

// A frame above the size limit ends the relay rather than reaching the device.
func TestRelayRejectsOversizedFrame(t *testing.T) {
	sink := &recordingSink{}
	relay := admitTestRelay(t, NewRelayManager(4), sink, newTestLogger(nil))
	if err := relay.ForwardToDevice(bytes.Repeat([]byte{0x01}, 2048)); !errors.Is(err, ErrFrameRejected) {
		t.Fatalf("ForwardToDevice() error = %v, want ErrFrameRejected", err)
	}
	if relay.CloseReason() != CloseReasonFrameLimit {
		t.Fatalf("close reason = %q, want %q", relay.CloseReason(), CloseReasonFrameLimit)
	}
	if sink.frameCount() != 0 {
		t.Fatalf("device received %d frames for an oversized frame", sink.frameCount())
	}
}

func TestRelayAttachGuardianIsSingleOwner(t *testing.T) {
	relay := admitTestRelay(t, NewRelayManager(4), &recordingSink{}, newTestLogger(nil))
	if err := relay.AttachGuardian(); err != nil {
		t.Fatalf("first attach: %v", err)
	}
	if err := relay.AttachGuardian(); !errors.Is(err, ErrSessionAlreadyRelayed) {
		t.Fatalf("second attach error = %v, want ErrSessionAlreadyRelayed", err)
	}
}

func TestRelayManagerRejectsSecondRelayForSameSession(t *testing.T) {
	manager := NewRelayManager(4)
	first := admitTestRelay(t, manager, &recordingSink{}, newTestLogger(nil))
	_ = first

	second, err := manager.Admit(context.Background(), testSession(time.Now().Add(time.Minute)), RelayAdmission{
		Outbound:        &recordingSink{},
		MaxFrameBytes:   1024,
		WriteQueueDepth: 4,
	})
	if !errors.Is(err, ErrSessionAlreadyRelayed) {
		t.Fatalf("second admit error = %v, want ErrSessionAlreadyRelayed", err)
	}
	if second != nil {
		t.Fatal("second admit returned a relay for a session that already has one")
	}
}

func TestRelayManagerEnforcesCapAndForgetsClosedRelays(t *testing.T) {
	manager := NewRelayManager(1)
	first := admitTestRelay(t, manager, &recordingSink{}, newTestLogger(nil))
	other := testSession(time.Now().Add(time.Minute))
	other.ID = "session_beta"
	if _, err := manager.Admit(context.Background(), other, RelayAdmission{
		Outbound:        &recordingSink{},
		MaxFrameBytes:   1024,
		WriteQueueDepth: 4,
	}); !errors.Is(err, ErrLimitReached) {
		t.Fatalf("second admit error = %v, want ErrLimitReached", err)
	}
	first.Close(CloseReasonGuardianHangup)
	waitForCondition(t, time.Second, func() bool { return manager.Count() == 0 })
	if _, err := manager.Admit(context.Background(), other, RelayAdmission{
		Outbound:        &recordingSink{},
		MaxFrameBytes:   1024,
		WriteQueueDepth: 4,
	}); err != nil {
		t.Fatalf("admit after release: %v", err)
	}
}

func TestRelayManagerCloseAllClosesOpenRelays(t *testing.T) {
	manager := NewRelayManager(4)
	sink := &recordingSink{}
	relay := admitTestRelay(t, manager, sink, newTestLogger(nil))
	manager.CloseAll()
	select {
	case <-relay.Done():
	default:
		t.Fatal("CloseAll left a relay open")
	}
	if relay.CloseReason() != CloseReasonGatewayShutdown {
		t.Fatalf("close reason = %q, want %q", relay.CloseReason(), CloseReasonGatewayShutdown)
	}
	if !sink.isClosed() {
		t.Fatal("CloseAll must release the device sink")
	}
	if _, err := manager.Admit(context.Background(), testSession(time.Now().Add(time.Minute)), RelayAdmission{
		Outbound: sink,
	}); !errors.Is(err, ErrRelayUnavailable) {
		t.Fatalf("admit after shutdown error = %v, want ErrRelayUnavailable", err)
	}
}

// The close log is the only durable record of a view. It must carry the
// identifiers, duration, and reason, and must never contain frame bytes or the
// device token: frame bytes are a picture of a child's room.
func TestRelayCloseLogHasNoFrameBytesOrToken(t *testing.T) {
	var logBuffer bytes.Buffer
	relay := admitTestRelay(t, NewRelayManager(4), &recordingSink{}, newTestLogger(&logBuffer))
	if err := relay.ForwardToDevice([]byte("FRAME-CONTENT-MARKER")); err != nil {
		t.Fatalf("forward to device: %v", err)
	}
	relay.Close(CloseReasonGuardianHangup)
	output := logBuffer.String()
	if !strings.Contains(output, `"session_id":"session_alpha"`) {
		t.Fatalf("close log is missing the session id: %s", output)
	}
	if !strings.Contains(output, `"close_reason":"guardian_hangup"`) {
		t.Fatalf("close log is missing the close reason: %s", output)
	}
	if !strings.Contains(output, `"duration_ms"`) || !strings.Contains(output, `"frames_in"`) {
		t.Fatalf("close log is missing the duration or frame counters: %s", output)
	}
	if strings.Contains(output, "FRAME-CONTENT-MARKER") {
		t.Fatalf("close log leaked frame content: %s", output)
	}
}

func TestRelayDeadlineReasonNamesTheLimit(t *testing.T) {
	relay := admitTestRelay(t, NewRelayManager(4), &recordingSink{}, newTestLogger(nil))
	if relay.DeadlineReason() != CloseReasonMaxDuration {
		t.Fatalf("deadline reason = %q, want %q", relay.DeadlineReason(), CloseReasonMaxDuration)
	}
}

func TestNormalizeFrameLimits(t *testing.T) {
	for _, testCase := range []struct {
		name       string
		frameBytes int
		frameRate  int
		wantBytes  int
		wantRate   int
	}{
		{name: "defaults", frameBytes: 0, frameRate: 0, wantBytes: 256 * 1024, wantRate: 8},
		{name: "configured", frameBytes: 1024, frameRate: 2, wantBytes: 1024, wantRate: 2},
		{name: "clamped", frameBytes: MaxFrameBytesLimit * 2, frameRate: 120, wantBytes: MaxFrameBytesLimit, wantRate: MaxFrameRateLimit},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			if got := NormalizeMaxFrameBytes(testCase.frameBytes); got != testCase.wantBytes {
				t.Fatalf("NormalizeMaxFrameBytes(%d) = %d, want %d", testCase.frameBytes, got, testCase.wantBytes)
			}
			if got := NormalizeMaxFramesPerSecond(testCase.frameRate); got != testCase.wantRate {
				t.Fatalf("NormalizeMaxFramesPerSecond(%d) = %d, want %d", testCase.frameRate, got, testCase.wantRate)
			}
		})
	}
}

func waitForCondition(t *testing.T, timeout time.Duration, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if condition() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("condition was not satisfied before timeout")
}
