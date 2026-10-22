package intercom

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

// recordingSink captures device frames and whether it was closed.
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

// failSink reports a write failure for every frame, simulating a device that
// disconnected while the relay was open.
type failSink struct{ closed atomic.Bool }

func (sink *failSink) SendRelayFrame([]byte) error {
	return errors.New("device write failed")
}

func (sink *failSink) Close() { sink.closed.Store(true) }

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

func testSession(expiresAt time.Time) Session {
	return Session{
		ID:        "call_alpha",
		DeviceID:  "device_alpha",
		State:     StateActive,
		ExpiresAt: expiresAt,
	}
}

func newTestLogger(buffer *bytes.Buffer) *slog.Logger {
	if buffer == nil {
		return slog.New(slog.NewJSONHandler(io.Discard, nil))
	}
	return slog.New(slog.NewJSONHandler(buffer, nil))
}

func TestStateAllowsAudio(t *testing.T) {
	for _, testCase := range []struct {
		name  string
		state State
		want  bool
	}{
		{name: "ringing never carries audio", state: StateRinging, want: false},
		{name: "active carries audio", state: StateActive, want: true},
		{name: "ended does not carry audio", state: StateEnded, want: false},
		{name: "missed does not carry audio", state: StateMissed, want: false},
		{name: "declined does not carry audio", state: StateDeclined, want: false},
		{name: "cancelled does not carry audio", state: StateCancelled, want: false},
		{name: "unknown state does not carry audio", state: State("bogus"), want: false},
		{name: "empty state does not carry audio", state: State(""), want: false},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			if got := testCase.state.AllowsAudio(); got != testCase.want {
				t.Fatalf("AllowsAudio(%q) = %v, want %v", testCase.state, got, testCase.want)
			}
		})
	}
}

func TestRelayForwardToDeviceReachesSink(t *testing.T) {
	sink := &recordingSink{}
	relay, err := NewRelayManager(4).Admit(
		context.Background(),
		testSession(time.Now().Add(time.Minute)),
		sink,
		"device-token-value",
		4,
		newTestLogger(nil),
	)
	if err != nil {
		t.Fatalf("admit relay: %v", err)
	}
	defer relay.Close(CloseReasonGuardianHangup)

	if err := relay.ForwardToDevice([]byte("guardian-audio")); err != nil {
		t.Fatalf("forward to device: %v", err)
	}
	waitForCondition(t, time.Second, func() bool { return sink.frameCount() == 1 })
	if string(sink.frames[0]) != "guardian-audio" {
		t.Fatalf("device frame = %q, want guardian-audio", sink.frames[0])
	}
}

func TestRelayForwardToGuardianReachesWriter(t *testing.T) {
	sink := &recordingSink{}
	relay, err := NewRelayManager(4).Admit(
		context.Background(),
		testSession(time.Now().Add(time.Minute)),
		sink,
		"device-token-value",
		4,
		newTestLogger(nil),
	)
	if err != nil {
		t.Fatalf("admit relay: %v", err)
	}
	defer relay.Close(CloseReasonGuardianHangup)

	var received [][]byte
	var receivedMutex sync.Mutex
	relay.AttachGuardianWriter(func(payload []byte) error {
		receivedMutex.Lock()
		received = append(received, append([]byte(nil), payload...))
		receivedMutex.Unlock()
		return nil
	})
	if err := relay.ForwardToGuardian([]byte("device-audio")); err != nil {
		t.Fatalf("forward to guardian: %v", err)
	}
	receivedMutex.Lock()
	defer receivedMutex.Unlock()
	if len(received) != 1 || string(received[0]) != "device-audio" {
		t.Fatalf("guardian frames = %q, want [device-audio]", received)
	}
	if relay.Stats().FramesOut != 1 {
		t.Fatalf("frames out = %d, want 1", relay.Stats().FramesOut)
	}
}

// A device frame that arrives before the guardian attaches must be counted and
// dropped, never buffered.
func TestRelayDropsFramesBeforeGuardianAttaches(t *testing.T) {
	relay, err := NewRelayManager(4).Admit(
		context.Background(),
		testSession(time.Now().Add(time.Minute)),
		&recordingSink{},
		"device-token-value",
		4,
		newTestLogger(nil),
	)
	if err != nil {
		t.Fatalf("admit relay: %v", err)
	}
	defer relay.Close(CloseReasonGuardianHangup)

	if err := relay.ForwardToGuardian([]byte("early-audio")); err != nil {
		t.Fatalf("forward to guardian: %v", err)
	}
	if relay.Stats().Dropped != 1 || relay.Stats().FramesOut != 0 {
		t.Fatalf("expected one dropped frame and none delivered, got %+v", relay.Stats())
	}
}

// Close must release the device sink and run the close side effects once even
// when several goroutines race it.
func TestRelayCloseIsIdempotentAndReleasesDeviceSide(t *testing.T) {
	sink := &recordingSink{}
	relay, err := NewRelayManager(4).Admit(
		context.Background(),
		testSession(time.Now().Add(time.Minute)),
		sink,
		"device-token-value",
		4,
		newTestLogger(nil),
	)
	if err != nil {
		t.Fatalf("admit relay: %v", err)
	}

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
	if err := relay.ForwardToDevice([]byte("late-audio")); err == nil {
		t.Fatal("a closed relay must refuse new guardian audio")
	}
}

// A hung-up call must not leave a device write in flight: the writer stops once
// the relay closes.
func TestRelayCloseStopsDeviceWriter(t *testing.T) {
	sink := &recordingSink{}
	relay, err := NewRelayManager(4).Admit(
		context.Background(),
		testSession(time.Now().Add(time.Minute)),
		sink,
		"device-token-value",
		4,
		newTestLogger(nil),
	)
	if err != nil {
		t.Fatalf("admit relay: %v", err)
	}
	if err := relay.ForwardToDevice([]byte("frame-before-close")); err != nil {
		t.Fatalf("forward to device: %v", err)
	}
	waitForCondition(t, time.Second, func() bool { return sink.frameCount() == 1 })
	relay.Close(CloseReasonGuardianHangup)
	framesAfterClose := sink.frameCount()
	if err := relay.ForwardToDevice([]byte("frame-after-close")); err == nil {
		t.Fatal("closed relay accepted a frame")
	}
	time.Sleep(50 * time.Millisecond)
	if sink.frameCount() != framesAfterClose {
		t.Fatalf("device received %d frames after close, want %d", sink.frameCount(), framesAfterClose)
	}
}

// A device write failure closes the relay so the guardian cannot keep talking
// into a channel nobody receives.
func TestRelayClosesWhenDeviceWriteFails(t *testing.T) {
	sink := &failSink{}
	relay, err := NewRelayManager(4).Admit(
		context.Background(),
		testSession(time.Now().Add(time.Minute)),
		sink,
		"device-token-value",
		4,
		newTestLogger(nil),
	)
	if err != nil {
		t.Fatalf("admit relay: %v", err)
	}
	if err := relay.ForwardToDevice([]byte("audio")); err != nil {
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
}

func TestRelayManagerRejectsSecondRelayForSameCall(t *testing.T) {
	manager := NewRelayManager(4)
	session := testSession(time.Now().Add(time.Minute))
	first, err := manager.Admit(context.Background(), session, &recordingSink{}, "token", 4, newTestLogger(nil))
	if err != nil {
		t.Fatalf("first admit: %v", err)
	}
	defer first.Close(CloseReasonGuardianHangup)

	second, err := manager.Admit(context.Background(), session, &recordingSink{}, "token", 4, newTestLogger(nil))
	if !errors.Is(err, ErrSessionAlreadyRelayed) {
		t.Fatalf("second admit error = %v, want ErrSessionAlreadyRelayed", err)
	}
	if second != nil {
		t.Fatal("second admit returned a relay for a call that already has one")
	}
}

func TestRelayManagerEnforcesCapAndForgetsClosedRelays(t *testing.T) {
	manager := NewRelayManager(1)
	first, err := manager.Admit(
		context.Background(),
		testSession(time.Now().Add(time.Minute)),
		&recordingSink{},
		"token",
		4,
		newTestLogger(nil),
	)
	if err != nil {
		t.Fatalf("first admit: %v", err)
	}
	other := testSession(time.Now().Add(time.Minute))
	other.ID = "call_beta"
	if _, err := manager.Admit(context.Background(), other, &recordingSink{}, "token", 4, newTestLogger(nil)); !errors.Is(err, ErrLimitReached) {
		t.Fatalf("second admit error = %v, want ErrLimitReached", err)
	}
	first.Close(CloseReasonGuardianHangup)
	waitForCondition(t, time.Second, func() bool { return manager.Count() == 0 })
	if _, err := manager.Admit(context.Background(), other, &recordingSink{}, "token", 4, newTestLogger(nil)); err != nil {
		t.Fatalf("admit after release: %v", err)
	}
}

func TestRelayManagerCloseAllClosesOpenRelays(t *testing.T) {
	manager := NewRelayManager(4)
	sink := &recordingSink{}
	relay, err := manager.Admit(
		context.Background(),
		testSession(time.Now().Add(time.Minute)),
		sink,
		"token",
		4,
		newTestLogger(nil),
	)
	if err != nil {
		t.Fatalf("admit relay: %v", err)
	}
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
	if _, err := manager.Admit(context.Background(), testSession(time.Now()), sink, "token", 4, newTestLogger(nil)); !errors.Is(err, ErrRelayUnavailable) {
		t.Fatalf("admit after shutdown error = %v, want ErrRelayUnavailable", err)
	}
}

// The close log is the only record of a call. It must carry the identifier,
// duration, and reason and must never contain audio bytes or the device token.
func TestRelayCloseLogHasNoAudioOrToken(t *testing.T) {
	var logBuffer bytes.Buffer
	sink := &recordingSink{}
	relay, err := NewRelayManager(4).Admit(
		context.Background(),
		testSession(time.Now().Add(time.Minute)),
		sink,
		"super-secret-device-token",
		4,
		newTestLogger(&logBuffer),
	)
	if err != nil {
		t.Fatalf("admit relay: %v", err)
	}
	if err := relay.ForwardToDevice([]byte("AUDIO-CONTENT-MARKER")); err != nil {
		t.Fatalf("forward to device: %v", err)
	}
	relay.Close(CloseReasonGuardianHangup)
	output := logBuffer.String()
	if !strings.Contains(output, `"session_id":"call_alpha"`) {
		t.Fatalf("close log is missing the session id: %s", output)
	}
	if !strings.Contains(output, `"close_reason":"guardian_hangup"`) {
		t.Fatalf("close log is missing the close reason: %s", output)
	}
	if !strings.Contains(output, `"duration_ms"`) {
		t.Fatalf("close log is missing the duration: %s", output)
	}
	if strings.Contains(output, "AUDIO-CONTENT-MARKER") {
		t.Fatalf("close log leaked audio content: %s", output)
	}
	if strings.Contains(output, "super-secret-device-token") {
		t.Fatalf("close log leaked the device token: %s", output)
	}
}

func TestRelayStatsReportDurationAndReason(t *testing.T) {
	base := time.Now()
	manager := NewRelayManager(4)
	manager.now = func() time.Time { return base }
	relay, err := manager.Admit(
		context.Background(),
		testSession(base.Add(time.Minute)),
		&recordingSink{},
		"token",
		4,
		newTestLogger(nil),
	)
	if err != nil {
		t.Fatalf("admit relay: %v", err)
	}
	manager.now = func() time.Time { return base.Add(90 * time.Second) }
	relay.now = manager.now
	relay.Close(CloseReasonMaxDuration)
	stats := relay.Stats()
	if stats.CloseReason != CloseReasonMaxDuration {
		t.Fatalf("close reason = %q, want %q", stats.CloseReason, CloseReasonMaxDuration)
	}
	if stats.Duration != 90*time.Second {
		t.Fatalf("duration = %v, want 90s", stats.Duration)
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
