package playback

import (
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/TissyBoxC/sprout-platform/services/voice_gateway/internal/audio/frame"
)

type recordingFrameSink struct {
	mutex   sync.Mutex
	payload [][]byte
	signal  chan struct{}
}

func newRecordingFrameSink() *recordingFrameSink {
	return &recordingFrameSink{signal: make(chan struct{}, 64)}
}

func (s *recordingFrameSink) SendAudio(payload []byte, _ uint64) error {
	s.mutex.Lock()
	s.payload = append(s.payload, append([]byte(nil), payload...))
	s.mutex.Unlock()
	select {
	case s.signal <- struct{}{}:
	default:
	}
	return nil
}

func (s *recordingFrameSink) frames() [][]byte {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	frames := make([][]byte, len(s.payload))
	for index, payload := range s.payload {
		frames[index] = append([]byte(nil), payload...)
	}
	return frames
}

func (s *recordingFrameSink) waitForFrames(t *testing.T, count int) {
	t.Helper()
	deadline := time.After(time.Second)
	for {
		if len(s.frames()) >= count {
			return
		}
		select {
		case <-s.signal:
		case <-deadline:
			t.Fatalf("expected %d frames, got %d", count, len(s.frames()))
		}
	}
}

type blockingFrameEncoder struct {
	started chan struct{}
	release chan struct{}
	once    sync.Once
	calls   atomic.Int32
}

func newBlockingFrameEncoder() *blockingFrameEncoder {
	return &blockingFrameEncoder{
		started: make(chan struct{}),
		release: make(chan struct{}),
	}
}

func (encoder *blockingFrameEncoder) EncodePCM(pcm []int16, destination []byte) (int, error) {
	if encoder.calls.Add(1) == 1 {
		encoder.once.Do(func() {
			close(encoder.started)
		})
		<-encoder.release
	}
	if len(pcm) != frame.SamplesPerFrame {
		return 0, ErrInvalidPCMFrame
	}
	copy(destination, []byte{0x01})
	return 1, nil
}

type capturingFrameEncoder struct {
	mutex sync.Mutex
	pcm   []int16
}

func (encoder *capturingFrameEncoder) EncodePCM(pcm []int16, destination []byte) (int, error) {
	encoder.mutex.Lock()
	encoder.pcm = append([]int16(nil), pcm...)
	encoder.mutex.Unlock()
	copy(destination, []byte{0x01})
	return 1, nil
}

func (encoder *capturingFrameEncoder) capturedPCM() []int16 {
	encoder.mutex.Lock()
	defer encoder.mutex.Unlock()
	return append([]int16(nil), encoder.pcm...)
}

type countingEncoder struct {
	calls atomic.Int32
}

func (encoder *countingEncoder) EncodePCM(_ []int16, destination []byte) (int, error) {
	encoder.calls.Add(1)
	copy(destination, []byte{0x01})
	return 1, nil
}

type failingSink struct{}

func (failingSink) SendAudio([]byte, uint64) error {
	return errors.New("sink failed")
}

func testPCMBytes(value int16) []byte {
	data := make([]byte, frame.SamplesPerFrame*2)
	for index := 0; index < frame.SamplesPerFrame; index++ {
		sample := uint16(value)
		data[index*2] = byte(sample)
		data[index*2+1] = byte(sample >> 8)
	}
	return data
}

func newSchedulerTestItem(itemID string, priority Priority, interruptible bool) Item {
	return Item{
		ItemID:        itemID,
		Priority:      priority,
		Payload:       testPCMBytes(int16(len(itemID))),
		Interruptible: interruptible,
	}
}

func TestSchedulerDrainsInPriorityOrder(t *testing.T) {
	sink := newRecordingFrameSink()
	encoder := &countingEncoder{}
	scheduler, err := newScheduler(sink, func() (frameEncoder, error) {
		return encoder, nil
	}, 100, 100)
	if err != nil {
		t.Fatalf("NewScheduler() error = %v", err)
	}
	defer scheduler.Close()

	scheduler.Pause()
	if err := scheduler.Enqueue(newSchedulerTestItem("item_ambient", PriorityAmbient, true)); err != nil {
		t.Fatalf("enqueue ambient: %v", err)
	}
	if err := scheduler.Enqueue(newSchedulerTestItem("item_safety", PrioritySafety, true)); err != nil {
		t.Fatalf("enqueue safety: %v", err)
	}
	if err := scheduler.Enqueue(newSchedulerTestItem("item_prompt", PriorityPrompt, true)); err != nil {
		t.Fatalf("enqueue prompt: %v", err)
	}
	scheduler.Resume()
	sink.waitForFrames(t, 3)
}

func TestSchedulerPreemptsAndRequeues(t *testing.T) {
	encoder := newBlockingFrameEncoder()
	sink := newRecordingFrameSink()
	scheduler, err := newScheduler(sink, func() (frameEncoder, error) {
		return encoder, nil
	}, 100, 100)
	if err != nil {
		t.Fatalf("newScheduler() error = %v", err)
	}
	defer scheduler.Close()

	if err := scheduler.Enqueue(newSchedulerTestItem("item_conversation", PriorityConversation, true)); err != nil {
		t.Fatalf("enqueue conversation: %v", err)
	}
	select {
	case <-encoder.started:
	case <-time.After(time.Second):
		t.Fatal("conversation frame did not start encoding")
	}
	if err := scheduler.Enqueue(newSchedulerTestItem("item_prompt", PriorityPrompt, true)); err != nil {
		t.Fatalf("enqueue prompt: %v", err)
	}
	close(encoder.release)
	sink.waitForFrames(t, 2)
	if snapshot := scheduler.Snapshot(); snapshot.NowPlaying != nil {
		t.Fatalf("expected preempted frame to be dropped, got %+v", snapshot.NowPlaying)
	}
}

func TestSchedulerClearRetainsSafetyOnly(t *testing.T) {
	sink := newRecordingFrameSink()
	scheduler, err := NewScheduler(sink, 100, 100)
	if err != nil {
		t.Fatalf("NewScheduler() error = %v", err)
	}
	defer scheduler.Close()

	scheduler.Pause()
	if err := scheduler.Enqueue(newSchedulerTestItem("item_conversation", PriorityConversation, true)); err != nil {
		t.Fatalf("enqueue conversation: %v", err)
	}
	if err := scheduler.Enqueue(newSchedulerTestItem("item_safety", PrioritySafety, true)); err != nil {
		t.Fatalf("enqueue safety: %v", err)
	}
	scheduler.Clear(false)
	if snapshot := scheduler.Snapshot(); snapshot.Pending != 1 {
		t.Fatalf("expected only safety pending, got %d", snapshot.Pending)
	}
	scheduler.Clear(true)
	if snapshot := scheduler.Snapshot(); snapshot.Pending != 0 {
		t.Fatalf("expected full clear, got %d pending", snapshot.Pending)
	}
	scheduler.Resume()
}

func TestSchedulerMuteDropsNormalAndKeepsSafety(t *testing.T) {
	encoder := &capturingFrameEncoder{}
	sink := newRecordingFrameSink()
	scheduler, err := newScheduler(sink, func() (frameEncoder, error) {
		return encoder, nil
	}, 100, 100)
	if err != nil {
		t.Fatalf("newScheduler() error = %v", err)
	}
	defer scheduler.Close()

	scheduler.SetMuted(true)
	if err := scheduler.Enqueue(newSchedulerTestItem("item_conversation", PriorityConversation, true)); err != nil {
		t.Fatalf("enqueue conversation: %v", err)
	}
	time.Sleep(20 * time.Millisecond)
	if frames := sink.frames(); len(frames) != 0 {
		t.Fatalf("muted conversation emitted %d frames", len(frames))
	}
	if err := scheduler.Enqueue(newSchedulerTestItem("item_safety", PrioritySafety, true)); err != nil {
		t.Fatalf("enqueue safety: %v", err)
	}
	sink.waitForFrames(t, 1)
	if len(encoder.capturedPCM()) != frame.SamplesPerFrame {
		t.Fatal("safety frame was not encoded")
	}
}

func TestSchedulerVolumeCapClampBeforeEncoding(t *testing.T) {
	encoder := &capturingFrameEncoder{}
	sink := newRecordingFrameSink()
	scheduler, err := newScheduler(sink, func() (frameEncoder, error) {
		return encoder, nil
	}, 80, 60)
	if err != nil {
		t.Fatalf("newScheduler() error = %v", err)
	}
	defer scheduler.Close()

	if err := scheduler.SetVolume(100); err != nil {
		t.Fatalf("SetVolume() error = %v", err)
	}
	item := newSchedulerTestItem("item_volume", PriorityConversation, true)
	item.Payload = testPCMBytes(1000)
	if err := scheduler.Enqueue(item); err != nil {
		t.Fatalf("enqueue volume item: %v", err)
	}
	sink.waitForFrames(t, 1)
	pcm := encoder.capturedPCM()
	if len(pcm) != frame.SamplesPerFrame {
		t.Fatalf("encoded PCM length = %d", len(pcm))
	}
	if pcm[0] != 600 {
		t.Fatalf("expected capped volume sample 600, got %d", pcm[0])
	}
}

func TestSchedulerCloseStopsAndIsIdempotent(t *testing.T) {
	sink := newRecordingFrameSink()
	scheduler, err := NewScheduler(sink, 100, 100)
	if err != nil {
		t.Fatalf("NewScheduler() error = %v", err)
	}
	if err := scheduler.Close(); err != nil {
		t.Fatalf("first Close() error = %v", err)
	}
	if err := scheduler.Close(); err != nil {
		t.Fatalf("second Close() error = %v", err)
	}
	if err := scheduler.Enqueue(newSchedulerTestItem("item_closed", PriorityConversation, true)); !errors.Is(err, ErrSchedulerClosed) {
		t.Fatalf("Enqueue() after Close error = %v, want ErrSchedulerClosed", err)
	}
}

func TestSchedulerConcurrentEnqueueAndClose(t *testing.T) {
	encoder := &countingEncoder{}
	sink := newRecordingFrameSink()
	scheduler, err := newScheduler(sink, func() (frameEncoder, error) {
		return encoder, nil
	}, 100, 100)
	if err != nil {
		t.Fatalf("newScheduler() error = %v", err)
	}

	var waitGroup sync.WaitGroup
	for worker := 0; worker < 16; worker++ {
		waitGroup.Add(1)
		go func(worker int) {
			defer waitGroup.Done()
			for index := 0; index < 32; index++ {
				item := newSchedulerTestItem(
					workerItemID(worker, index),
					PriorityConversation,
					true,
				)
				if err := scheduler.Enqueue(item); err != nil && !errors.Is(err, ErrSchedulerClosed) {
					t.Errorf("Enqueue() error = %v", err)
					return
				}
			}
		}(worker)
	}
	waitGroup.Wait()
	if err := scheduler.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	if err := scheduler.Close(); err != nil {
		t.Fatalf("second Close() error = %v", err)
	}
}

func TestSchedulerSinkFailureStopsWithoutSpinning(t *testing.T) {
	encoder := &countingEncoder{}
	scheduler, err := newScheduler(failingSink{}, func() (frameEncoder, error) {
		return encoder, nil
	}, 100, 100)
	if err != nil {
		t.Fatalf("newScheduler() error = %v", err)
	}
	if err := scheduler.Enqueue(newSchedulerTestItem("item_failure", PriorityConversation, true)); err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	deadline := time.After(time.Second)
	for encoder.calls.Load() == 0 {
		select {
		case <-deadline:
			t.Fatal("scheduler did not attempt playback")
		default:
			time.Sleep(time.Millisecond)
		}
	}
	if err := scheduler.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	if calls := encoder.calls.Load(); calls != 1 {
		t.Fatalf("expected one encode attempt, got %d", calls)
	}
}

func TestSchedulerRejectsInvalidFrame(t *testing.T) {
	scheduler, err := NewScheduler(newRecordingFrameSink(), 100, 100)
	if err != nil {
		t.Fatalf("NewScheduler() error = %v", err)
	}
	defer scheduler.Close()
	item := newSchedulerTestItem("item_invalid", PriorityConversation, true)
	item.Payload = []byte{0x01}
	if err := scheduler.Enqueue(item); !errors.Is(err, ErrInvalidPCMFrame) {
		t.Fatalf("Enqueue() error = %v, want ErrInvalidPCMFrame", err)
	}
}

func TestSchedulerReportsPlayedFrameWithAppliedVolume(t *testing.T) {
	sink := newRecordingFrameSink()
	scheduler, err := newScheduler(sink, func() (frameEncoder, error) {
		return &countingEncoder{}, nil
	}, 50, 100)
	if err != nil {
		t.Fatalf("newScheduler() error = %v", err)
	}
	defer scheduler.Close()

	played := make(chan []int16, 1)
	item := newSchedulerTestItem("item_reference", PriorityConversation, true)
	item.Payload = testPCMBytes(1000)
	item.OnPlayed = func(pcm []int16) {
		select {
		case played <- append([]int16(nil), pcm...):
		default:
		}
	}
	if err := scheduler.Enqueue(item); err != nil {
		t.Fatalf("enqueue: %v", err)
	}

	select {
	case pcm := <-played:
		if len(pcm) != frame.SamplesPerFrame {
			t.Fatalf("played frame length = %d", len(pcm))
		}
		if pcm[0] != 500 {
			t.Fatalf("expected volume-applied sample 500, got %d", pcm[0])
		}
	case <-time.After(time.Second):
		t.Fatal("OnPlayed was not invoked for a played frame")
	}
}

func TestSchedulerDoesNotReportSuppressedFrame(t *testing.T) {
	scheduler, err := newScheduler(newRecordingFrameSink(), func() (frameEncoder, error) {
		return &countingEncoder{}, nil
	}, 100, 100)
	if err != nil {
		t.Fatalf("newScheduler() error = %v", err)
	}
	defer scheduler.Close()

	scheduler.SetMuted(true)
	reported := make(chan struct{}, 1)
	item := newSchedulerTestItem("item_muted", PriorityConversation, true)
	item.OnPlayed = func([]int16) {
		select {
		case reported <- struct{}{}:
		default:
		}
	}
	if err := scheduler.Enqueue(item); err != nil {
		t.Fatalf("enqueue: %v", err)
	}

	select {
	case <-reported:
		t.Fatal("OnPlayed fired for audio suppressed by mute")
	case <-time.After(50 * time.Millisecond):
	}
}

func workerItemID(worker int, index int) string {
	return "item_" + string(rune('a'+worker%26)) + "_" + string(rune('a'+index/26)) + string(rune('a'+index%26))
}
