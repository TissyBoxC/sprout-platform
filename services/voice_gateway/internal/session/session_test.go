package session

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/TissyBoxC/sprout-platform/services/voice_gateway/internal/audio/buffer"
	"github.com/TissyBoxC/sprout-platform/services/voice_gateway/internal/audio/codec"
	"github.com/TissyBoxC/sprout-platform/services/voice_gateway/internal/audio/frame"
	"github.com/TissyBoxC/sprout-platform/services/voice_gateway/internal/audio/vad"
)

func TestManagerCreatesAndRemovesSession(t *testing.T) {
	manager := NewManager(ManagerConfig{})
	defer manager.CloseAll()

	voiceSession, err := manager.Create(testSessionConfig("session-1", "device-1", "stream-1"))
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	if manager.Count() != 1 || manager.CountForDevice("device-1") != 1 {
		t.Fatalf("unexpected counts: %d/%d", manager.Count(), manager.CountForDevice("device-1"))
	}
	if _, ok := manager.Get(voiceSession.ID()); !ok {
		t.Fatal("created session was not retrievable")
	}
	if !manager.Remove(voiceSession.ID()) {
		t.Fatal("remove reported a missing session")
	}
	if manager.Count() != 0 {
		t.Fatalf("expected zero sessions, got %d", manager.Count())
	}
	select {
	case <-voiceSession.Done():
	case <-time.After(time.Second):
		t.Fatal("removed session did not close")
	}
}

func TestManagerRejectsDuplicateStreamAndLimits(t *testing.T) {
	manager := NewManager(ManagerConfig{MaxSessions: 2, MaxSessionsPerDevice: 1})
	defer manager.CloseAll()

	if _, err := manager.Create(testSessionConfig("session-1", "device-1", "stream-1")); err != nil {
		t.Fatalf("create first session: %v", err)
	}
	if _, err := manager.Create(testSessionConfig("session-2", "device-1", "stream-1")); !errors.Is(err, ErrDeviceSessionLimit) {
		t.Fatalf("expected per-device limit, got %v", err)
	}
	if _, err := manager.Create(testSessionConfig("session-3", "device-2", "stream-1")); err != nil {
		t.Fatalf("create second device session: %v", err)
	}
	if _, err := manager.Create(testSessionConfig("session-4", "device-3", "stream-1")); !errors.Is(err, ErrSessionLimitReached) {
		t.Fatalf("expected global limit, got %v", err)
	}
}

func TestManagerRejectsDuplicateIdentifierAndStream(t *testing.T) {
	manager := NewManager(ManagerConfig{MaxSessions: 4, MaxSessionsPerDevice: 2})
	defer manager.CloseAll()

	if _, err := manager.Create(testSessionConfig("session-1", "device-1", "stream-1")); err != nil {
		t.Fatalf("create first session: %v", err)
	}
	if _, err := manager.Create(testSessionConfig("session-1", "device-2", "stream-2")); !errors.Is(err, ErrSessionAlreadyExists) {
		t.Fatalf("expected duplicate id rejection, got %v", err)
	}
	if _, err := manager.Create(testSessionConfig("session-2", "device-1", "stream-1")); !errors.Is(err, ErrDuplicateStream) {
		t.Fatalf("expected duplicate stream rejection, got %v", err)
	}
}

func TestManagerCloseAllWaitsForEverySession(t *testing.T) {
	manager := NewManager(ManagerConfig{MaxSessions: 4, MaxSessionsPerDevice: 2})
	first, err := manager.Create(testSessionConfig("session-close-1", "device-1", "stream-1"))
	if err != nil {
		t.Fatalf("create first session: %v", err)
	}
	second, err := manager.Create(testSessionConfig("session-close-2", "device-2", "stream-2"))
	if err != nil {
		t.Fatalf("create second session: %v", err)
	}

	manager.CloseAll()
	select {
	case <-first.Done():
	default:
		t.Fatal("first session was not closed")
	}
	select {
	case <-second.Done():
	default:
		t.Fatal("second session was not closed")
	}
	if manager.Count() != 0 {
		t.Fatalf("close-all left %d sessions", manager.Count())
	}
}

func TestSessionSequenceHandlingAndVADSegment(t *testing.T) {
	voiceSession, err := NewSession(testSessionConfig("session-1", "device-1", "stream-1"))
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	defer voiceSession.Close()

	first := testAudioFrame(1)
	if err := voiceSession.AcceptFrame(context.Background(), first); err != nil {
		t.Fatalf("accept first frame: %v", err)
	}
	if err := voiceSession.AcceptFrame(context.Background(), first); !errors.Is(err, buffer.ErrDuplicateFrame) {
		t.Fatalf("expected duplicate rejection, got %v", err)
	}
	if err := voiceSession.AcceptFrame(context.Background(), testAudioFrame(0)); !errors.Is(err, ErrOutOfOrderFrame) {
		t.Fatalf("expected out-of-order rejection, got %v", err)
	}

	stats := voiceSession.Stats()
	if stats.AcceptedFrames != 1 || stats.DuplicateFrames != 1 || stats.OutOfOrderFrames != 1 {
		t.Fatalf("unexpected sequence stats: %+v", stats)
	}
}

func TestSessionEmitsCompletedSpeechSegment(t *testing.T) {
	config := testSessionConfig("session-1", "device-1", "stream-1")
	config.DetectorFactory = func() vad.Detector {
		return &scriptedDetector{values: []bool{false, true, true, false}}
	}
	config.PreRollFrames = 1
	voiceSession, err := NewSession(config)
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	defer voiceSession.Close()

	for sequence := uint32(0); sequence < 4; sequence++ {
		if err := voiceSession.AcceptFrame(context.Background(), testAudioFrame(sequence)); err != nil {
			t.Fatalf("accept frame %d: %v", sequence, err)
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	segment, err := voiceSession.NextSegment(ctx)
	if err != nil {
		t.Fatalf("next segment: %v", err)
	}
	if segment.SessionID != "session-1" || segment.SequenceStart != 0 || segment.SequenceEnd != 2 {
		t.Fatalf("unexpected segment metadata: %+v", segment)
	}
	if len(segment.PCM) == 0 {
		t.Fatal("segment did not contain PCM samples")
	}
}

func TestSessionPropagatesContentCategoryToSegment(t *testing.T) {
	config := testSessionConfig("session-category", "device-1", "stream-1")
	config.ContentCategory = "story"
	config.DetectorFactory = func() vad.Detector {
		return &scriptedDetector{values: []bool{true, true, false}}
	}
	voiceSession, err := NewSession(config)
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	defer voiceSession.Close()

	if voiceSession.ContentCategory() != "story" {
		t.Fatalf("session category = %q, want story", voiceSession.ContentCategory())
	}
	for sequence := uint32(0); sequence < 3; sequence++ {
		if err := voiceSession.AcceptFrame(
			context.Background(),
			testAudioFrame(sequence),
		); err != nil {
			t.Fatalf("accept frame %d: %v", sequence, err)
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	segment, err := voiceSession.NextSegment(ctx)
	if err != nil {
		t.Fatalf("next segment: %v", err)
	}
	if segment.ContentCategory != "story" {
		t.Fatalf("segment category = %q, want story", segment.ContentCategory)
	}
}

func TestSessionIdleTimeoutCancels(t *testing.T) {
	config := testSessionConfig("session-1", "device-1", "stream-1")
	config.IdleTimeout = 40 * time.Millisecond
	config.MaxDuration = time.Second
	voiceSession, err := NewSession(config)
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	defer voiceSession.Close()

	select {
	case <-voiceSession.Done():
	case <-time.After(time.Second):
		t.Fatal("idle session did not time out")
	}
	if !errors.Is(voiceSession.Err(), ErrSessionIdleTimeout) {
		t.Fatalf("expected idle timeout cause, got %v", voiceSession.Err())
	}
}

func TestSessionConcurrentAcceptIsSerialized(t *testing.T) {
	voiceSession, err := NewSession(testSessionConfig("session-1", "device-1", "stream-1"))
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	defer voiceSession.Close()

	var waitGroup sync.WaitGroup
	for index := 0; index < 16; index++ {
		waitGroup.Add(1)
		go func(sequence uint32) {
			defer waitGroup.Done()
			_ = voiceSession.AcceptFrame(context.Background(), testAudioFrame(sequence))
		}(uint32(index))
	}
	waitGroup.Wait()

	stats := voiceSession.Stats()
	if stats.AcceptedFrames == 0 {
		t.Fatalf("no concurrent frame was accepted: %+v", stats)
	}
}

func TestSessionContextCancellationStopsSegmentWait(t *testing.T) {
	voiceSession, err := NewSession(testSessionConfig("session-1", "device-1", "stream-1"))
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	defer voiceSession.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := voiceSession.NextSegment(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("expected caller cancellation, got %v", err)
	}
}

func testSessionConfig(id string, deviceID string, streamID string) SessionConfig {
	return SessionConfig{
		ID:                   id,
		DeviceID:             deviceID,
		SchemaVersion:        frame.SchemaVersion,
		StreamID:             streamID,
		BufferCapacityFrames: 4,
		SegmentQueueDepth:    2,
		PreRollFrames:        2,
		IdleTimeout:          time.Second,
		MaxDuration:          10 * time.Second,
		MaxSegmentLength:     time.Second,
		MinimumSpeechMS:      frame.DurationMS,
		CodecFactory: func() (codec.Codec, error) {
			return codec.NewOpusCodec()
		},
		DetectorFactory: func() vad.Detector {
			return &scriptedDetector{values: []bool{true}}
		},
	}
}

func testAudioFrame(sequence uint32) frame.Frame {
	opusCodec, err := codec.NewOpusCodec()
	if err != nil {
		panic(err)
	}
	pcm := make([]int16, frame.SamplesPerFrame)
	for index := range pcm {
		pcm[index] = int16((index % 32) * 400)
	}
	payload := make([]byte, frame.MaxPayloadBytes)
	encoded, err := opusCodec.EncodePCM(pcm, payload)
	if err != nil {
		panic(err)
	}
	return frame.New(
		"frame-"+time.Now().UTC().Format("150405.000000000"),
		"device-1",
		"stream-1",
		sequence,
		time.Now().UTC(),
		payload[:encoded],
	)
}

type scriptedDetector struct {
	mutex  sync.Mutex
	index  int
	values []bool
}

func (d *scriptedDetector) IsSpeech(_ []byte) bool {
	d.mutex.Lock()
	defer d.mutex.Unlock()
	if len(d.values) == 0 {
		return true
	}
	value := d.values[d.index%len(d.values)]
	d.index++
	return value
}
