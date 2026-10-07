package playback

import (
	"errors"
	"sync"

	"github.com/TissyBoxC/sprout-platform/services/voice_gateway/internal/audio/codec"
	"github.com/TissyBoxC/sprout-platform/services/voice_gateway/internal/audio/frame"
)

// ErrSchedulerClosed means playback scheduling has stopped permanently.
var ErrSchedulerClosed = errors.New("playback scheduler is closed")

// ErrInvalidPCMFrame means an item did not contain exactly one 20 ms PCM frame.
var ErrInvalidPCMFrame = errors.New("playback item is not one PCM frame")

// FrameSink receives one encoded Opus frame for the connected device.
type FrameSink interface {
	// SendAudio writes one encoded Opus frame. turnEpoch is the reply turn the
	// frame belongs to; the sink MUST drop the frame when a barge-in has since
	// advanced past that turn, so a canceled reply never reaches the speaker.
	SendAudio(payload []byte, turnEpoch uint64) error
}

type frameEncoder interface {
	EncodePCM(pcm []int16, destination []byte) (int, error)
}

// Scheduler drains one connection's playback queue in the background.
//
// Queue items carry little-endian PCM. The scheduler applies mute and volume
// before Opus encoding so gain changes take effect immediately without
// re-encoding queued audio.
type Scheduler struct {
	queue    *Queue
	sink     FrameSink
	encoder  frameEncoder
	wake     chan struct{}
	stop     chan struct{}
	done     chan struct{}
	stopOnce sync.Once
	mutex    sync.Mutex
	closed   bool
}

// NewScheduler creates a per-connection scheduler with a dedicated Opus
// encoder. The returned scheduler starts draining immediately.
func NewScheduler(sink FrameSink, volumePercent int, maxVolumePercent int) (*Scheduler, error) {
	if sink == nil {
		return nil, errors.New("playback scheduler requires a frame sink")
	}
	return newScheduler(sink, func() (frameEncoder, error) {
		return codec.NewOpusCodec()
	}, volumePercent, maxVolumePercent)
}

func newScheduler(
	sink FrameSink,
	encoderFactory func() (frameEncoder, error),
	volumePercent int,
	maxVolumePercent int,
) (*Scheduler, error) {
	if sink == nil {
		return nil, errors.New("playback scheduler requires a frame sink")
	}
	queue, err := NewQueue(volumePercent, maxVolumePercent)
	if err != nil {
		return nil, err
	}
	audioEncoder, err := encoderFactory()
	if err != nil {
		return nil, err
	}
	scheduler := &Scheduler{
		queue:   queue,
		sink:    sink,
		encoder: audioEncoder,
		wake:    make(chan struct{}, 1),
		stop:    make(chan struct{}),
		done:    make(chan struct{}),
	}
	go scheduler.run()
	return scheduler, nil
}

// Enqueue adds one PCM frame to the connection playback queue.
func (s *Scheduler) Enqueue(item Item) error {
	if s == nil {
		return ErrSchedulerClosed
	}
	if len(item.Payload) != frame.SamplesPerFrame*2 {
		return ErrInvalidPCMFrame
	}

	s.mutex.Lock()
	if s.closed {
		s.mutex.Unlock()
		return ErrSchedulerClosed
	}
	err := s.queue.Enqueue(item)
	if err == nil {
		s.signalLocked()
	}
	s.mutex.Unlock()
	if err != nil {
		return err
	}
	return nil
}

// Pause stops the scheduler from dispatching more audio.
func (s *Scheduler) Pause() {
	if s == nil {
		return
	}
	s.mutex.Lock()
	if s.closed {
		s.mutex.Unlock()
		return
	}
	s.queue.Pause()
	s.mutex.Unlock()
}

// Resume allows dispatching to continue.
func (s *Scheduler) Resume() {
	if s == nil {
		return
	}
	s.mutex.Lock()
	if s.closed {
		s.mutex.Unlock()
		return
	}
	s.queue.Resume()
	s.signalLocked()
	s.mutex.Unlock()
}

// Clear drops queued audio while optionally retaining safety announcements.
func (s *Scheduler) Clear(includeSafety bool) {
	if s == nil {
		return
	}
	s.mutex.Lock()
	if s.closed {
		s.mutex.Unlock()
		return
	}
	s.queue.Clear(includeSafety)
	s.signalLocked()
	s.mutex.Unlock()
}

// SetMuted toggles normal playback suppression without suppressing safety.
func (s *Scheduler) SetMuted(isMuted bool) {
	if s == nil {
		return
	}
	s.mutex.Lock()
	if s.closed {
		s.mutex.Unlock()
		return
	}
	s.queue.SetMuted(isMuted)
	s.signalLocked()
	s.mutex.Unlock()
}

// SetVolume sets the current volume within the guardian maximum.
func (s *Scheduler) SetVolume(volumePercent int) error {
	if s == nil {
		return ErrSchedulerClosed
	}
	s.mutex.Lock()
	defer s.mutex.Unlock()
	if s.closed {
		return ErrSchedulerClosed
	}
	return s.queue.SetVolume(volumePercent)
}

// SetMaxVolume applies an updated guardian policy limit.
func (s *Scheduler) SetMaxVolume(maxVolumePercent int) error {
	if s == nil {
		return ErrSchedulerClosed
	}
	s.mutex.Lock()
	defer s.mutex.Unlock()
	if s.closed {
		return ErrSchedulerClosed
	}
	return s.queue.SetMaxVolume(maxVolumePercent)
}

// Snapshot returns the current scheduler state for diagnostics.
func (s *Scheduler) Snapshot() Snapshot {
	if s == nil {
		return Snapshot{}
	}
	return s.queue.Snapshot()
}

// Close stops the scheduler permanently and is safe to call concurrently.
func (s *Scheduler) Close() error {
	if s == nil {
		return nil
	}
	s.mutex.Lock()
	if !s.closed {
		s.closed = true
		s.queue.Clear(true)
		s.stopOnce.Do(func() {
			close(s.stop)
		})
	}
	s.mutex.Unlock()
	<-s.done
	return nil
}

func (s *Scheduler) run() {
	defer close(s.done)
	for {
		select {
		case <-s.stop:
			return
		case <-s.wake:
		}
		for {
			select {
			case <-s.stop:
				return
			default:
			}
			item, ok := s.queue.Next()
			if !ok {
				break
			}
			if err := s.play(item); err != nil {
				s.fail(err)
				return
			}
		}
	}
}

func (s *Scheduler) play(item *Item) error {
	if item == nil {
		return nil
	}
	volume := s.queue.PlaybackVolume(item.Priority)
	if volume == 0 && item.Priority != PrioritySafety {
		s.queue.TakeCurrent(item.ItemID)
		return nil
	}

	pcm := bytesToPCM(item.Payload)
	if volume != 100 {
		applyVolume(pcm, volume)
	}

	encoded := make([]byte, frame.MaxPayloadBytes)
	written, err := s.encoder.EncodePCM(pcm, encoded)
	if err != nil {
		return err
	}
	s.mutex.Lock()
	if s.closed || !s.queue.TakeCurrent(item.ItemID) {
		s.mutex.Unlock()
		return nil
	}
	if item.OnPlayed != nil {
		item.OnPlayed(append([]int16(nil), pcm...))
	}
	s.mutex.Unlock()
	return s.sink.SendAudio(encoded[:written], item.TurnEpoch)
}

func (s *Scheduler) fail(err error) {
	s.mutex.Lock()
	if !s.closed {
		s.closed = true
		s.queue.Clear(true)
		s.stopOnce.Do(func() {
			close(s.stop)
		})
	}
	s.mutex.Unlock()
}

func (s *Scheduler) signalLocked() {
	select {
	case s.wake <- struct{}{}:
	default:
	}
}

func bytesToPCM(data []byte) []int16 {
	pcm := make([]int16, len(data)/2)
	for index := range pcm {
		pcm[index] = int16(uint16(data[index*2]) | uint16(data[index*2+1])<<8)
	}
	return pcm
}

func applyVolume(pcm []int16, volumePercent int) {
	for index, sample := range pcm {
		pcm[index] = int16(int32(sample) * int32(volumePercent) / 100)
	}
}
