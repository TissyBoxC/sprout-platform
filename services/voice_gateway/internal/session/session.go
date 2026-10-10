// Package session owns realtime voice session lifecycle.
package session

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/TissyBoxC/sprout-platform/services/voice_gateway/internal/audio/buffer"
	"github.com/TissyBoxC/sprout-platform/services/voice_gateway/internal/audio/codec"
	"github.com/TissyBoxC/sprout-platform/services/voice_gateway/internal/audio/frame"
	"github.com/TissyBoxC/sprout-platform/services/voice_gateway/internal/audio/preprocess"
	"github.com/TissyBoxC/sprout-platform/services/voice_gateway/internal/audio/vad"
)

const (
	defaultIdleTimeout       = 60 * time.Second
	defaultMaxDuration       = 15 * time.Minute
	defaultMaxSegmentLength  = 30 * time.Second
	defaultPreRollFrames     = 10
	defaultSegmentQueueDepth = 1
	defaultMinimumSpeechMS   = 60
)

// Errors returned by one realtime session.
var (
	ErrSessionClosed        = errors.New("voice session is closed")
	ErrSessionIdleTimeout   = errors.New("voice session idle timeout")
	ErrSessionDurationLimit = errors.New("voice session duration limit reached")
	ErrSessionWindowExpired = errors.New("voice session continuity window expired")
	ErrSessionIdentity      = errors.New("voice session identity does not match the frame")
	ErrOutOfOrderFrame      = errors.New("audio frame sequence is older than the accepted stream")
	ErrSegmentQueueFull     = errors.New("voice segment queue is full")
	ErrSegmentTooLong       = errors.New("voice segment exceeds the maximum duration")
)

// SessionConfig describes one device conversation and its resource bounds.
type SessionConfig struct {
	ID            string
	DeviceID      string
	SchemaVersion string
	StreamID      string
	// ContentCategory is the category the device declared for this session.
	// It is empty for older clients and is passed to the conversation policy
	// without being logged or persisted here.
	ContentCategory      string
	BufferCapacityFrames int
	SegmentQueueDepth    int
	PreRollFrames        int
	SegmentHandler       func(AudioSegment)
	IdleTimeout          time.Duration
	MaxDuration          time.Duration
	MaxSegmentLength     time.Duration
	MinimumSpeechMS      int
	CodecFactory         func() (codec.Codec, error)
	DetectorFactory      func() vad.Detector
	// PreprocessFactory builds the per-session echo cancellation, noise
	// suppression, and gain calibration chain. It is optional so a profile
	// without extra CPU still gets the codec and VAD path.
	PreprocessFactory func() (preprocess.Processor, error)
	// ReferenceAudio supplies the most recent speaker playback so echo
	// cancellation can subtract it from the microphone signal. The session
	// copies the returned slice before use; nil means the speaker is silent.
	ReferenceAudio func() []int16
}

// AudioSegment is one bounded PCM utterance handed to ASR or moderation.
//
// The segment owns a copy of its samples; callers may retain it after the
// session advances. Empty segments are never emitted.
type AudioSegment struct {
	ID              string
	SessionID       string
	DeviceID        string
	StreamID        string
	ContentCategory string
	SequenceStart   uint32
	SequenceEnd     uint32
	CapturedAt      time.Time
	PCM             []int16
}

// SessionStats is a point-in-time diagnostic view of one conversation.
type SessionStats struct {
	State            State
	AcceptedFrames   uint64
	DuplicateFrames  uint64
	OutOfOrderFrames uint64
	Buffer           buffer.Stats
	SegmentsEmitted  uint64
	LastSequence     uint32
	HasSequence      bool
	LastActivity     time.Time
}

// QualitySnapshot is a read-only preprocessing quality view for one session.
//
// The snapshot intentionally contains only scalar quality metrics. It never
// exposes PCM, filtered audio, provider payloads, or conversation content.
type QualitySnapshot struct {
	SessionID             string    `json:"session_id"`
	DeviceID              string    `json:"device_id"`
	State                 State     `json:"state"`
	EchoCancellationReady bool      `json:"echo_cancellation_ready"`
	EchoConvergence       int       `json:"echo_convergence"`
	NoiseFloor            float64   `json:"noise_floor"`
	AgcGain               float64   `json:"agc_gain"`
	VADConfidence         int       `json:"vad_confidence"`
	VADNoiseFloor         float64   `json:"vad_noise_floor"`
	LastActivity          time.Time `json:"last_activity"`
	MetricsAt             time.Time `json:"metrics_at"`
}

// Session represents one active device conversation.
//
// AcceptFrame is safe for concurrent callers; it serialises codec, sequence,
// VAD, and segment state so one stream cannot race two readers. The session
// owns its codec and detector and releases both when Close or a timeout fires.
type Session struct {
	id                  string
	deviceID            string
	schemaVersion       string
	streamID            string
	contentCategory     string
	createdAt           time.Time
	buffer              *buffer.RingBuffer
	segmentQueueDepth   int
	preRollFrames       int
	segmentHandler      func(AudioSegment)
	idleTimeout         time.Duration
	maxDuration         time.Duration
	maxSegmentLength    time.Duration
	minimumSpeechFrames int

	ctx           context.Context
	cancel        context.CancelCauseFunc
	activity      chan struct{}
	segments      chan AudioSegment
	done          chan struct{}
	closedOnce    sync.Once
	segmentMu     sync.RWMutex
	segmentClosed bool

	mu                   sync.Mutex
	codec                codec.Codec
	detector             vad.Detector
	processor            preprocess.Processor
	referenceAudio       func() []int16
	state                State
	lastSequence         uint32
	hasSequence          bool
	acceptedFrames       uint64
	duplicateFrames      uint64
	outOfOrderFrames     uint64
	segmentsEmitted      uint64
	lastActivity         time.Time
	metricsAt            time.Time
	quality              QualitySnapshot
	segmentActive        bool
	segmentSequenceStart uint32
	segmentSequenceEnd   uint32
	segmentCapturedAt    time.Time
	segmentSamples       []int16
	preRoll              []frameSamples
}

type frameSamples struct {
	sequence   uint32
	capturedAt time.Time
	samples    []int16
}

// NewSession creates a bounded conversation.
//
// Identity fields are required; missing timing and capacity values receive
// secure production defaults. The returned session starts an idle/maximum
// duration watchdog that exits when the session is closed.
func NewSession(config SessionConfig) (*Session, error) {
	config = normalizeSessionConfig(config)
	if config.ID == "" || config.DeviceID == "" || config.StreamID == "" {
		return nil, fmt.Errorf("%w: id, device_id and stream_id are required", ErrSessionIdentity)
	}

	sessionContext, cancel := context.WithCancelCause(context.Background())
	audioCodec, err := config.CodecFactory()
	if err != nil {
		cancel(err)
		return nil, fmt.Errorf("create session codec: %w", err)
	}
	detector := config.DetectorFactory()
	if audioCodec == nil || detector == nil {
		cancel(errors.New("session codec and detector must not be nil"))
		return nil, errors.New("create session: codec and detector must not be nil")
	}
	var processor preprocess.Processor
	if config.PreprocessFactory != nil {
		builtProcessor, processorErr := config.PreprocessFactory()
		if processorErr != nil {
			cancel(processorErr)
			return nil, fmt.Errorf("create session preprocessor: %w", processorErr)
		}
		processor = builtProcessor
	}

	now := time.Now().UTC()
	created := &Session{
		id:                  config.ID,
		deviceID:            config.DeviceID,
		schemaVersion:       config.SchemaVersion,
		streamID:            config.StreamID,
		contentCategory:     strings.TrimSpace(config.ContentCategory),
		createdAt:           now,
		buffer:              buffer.NewRingBuffer(config.BufferCapacityFrames),
		segmentQueueDepth:   config.SegmentQueueDepth,
		preRollFrames:       config.PreRollFrames,
		segmentHandler:      config.SegmentHandler,
		idleTimeout:         config.IdleTimeout,
		maxDuration:         config.MaxDuration,
		maxSegmentLength:    config.MaxSegmentLength,
		minimumSpeechFrames: millisecondsToFrames(config.MinimumSpeechMS),
		ctx:                 sessionContext,
		cancel:              cancel,
		activity:            make(chan struct{}, 1),
		segments:            make(chan AudioSegment, config.SegmentQueueDepth),
		done:                make(chan struct{}),
		codec:               audioCodec,
		detector:            detector,
		processor:           processor,
		referenceAudio:      config.ReferenceAudio,
		state:               StateIdle,
		lastActivity:        now,
	}
	go created.watchTimeouts()
	go func() {
		<-created.ctx.Done()
		close(created.done)
		created.closeSegments()
	}()
	return created, nil
}

// ID returns the immutable session identifier.
func (s *Session) ID() string {
	if s == nil {
		return ""
	}
	return s.id
}

// DeviceID returns the authenticated device that owns the session.
func (s *Session) DeviceID() string {
	if s == nil {
		return ""
	}
	return s.deviceID
}

// StreamID returns the device-allocated stream identifier.
func (s *Session) StreamID() string {
	if s == nil {
		return ""
	}
	return s.streamID
}

// ContentCategory returns the category the device declared for this session,
// or an empty string for older clients.
func (s *Session) ContentCategory() string {
	if s == nil {
		return ""
	}
	return s.contentCategory
}

// Done closes when the session ends because of close, cancellation, idle
// timeout, or the maximum conversation duration.
func (s *Session) Done() <-chan struct{} {
	if s == nil {
		closed := make(chan struct{})
		close(closed)
		return closed
	}
	return s.done
}

// Err returns the reason the session ended, or nil while it is active.
func (s *Session) Err() error {
	if s == nil {
		return ErrSessionClosed
	}
	return context.Cause(s.ctx)
}

// Close ends the session and releases the watchdog and segment queue.
//
// Close is idempotent and safe to call from the WebSocket cleanup path.
func (s *Session) Close() error {
	if s == nil {
		return nil
	}
	s.closedOnce.Do(func() {
		s.cancel(ErrSessionClosed)
	})
	return nil
}

// State returns the current conversation phase.
func (s *Session) State() State {
	if s == nil {
		return StateIdle
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.state
}

// Transition applies a validated state-machine event.
//
// The caller owns conversation sequencing; invalid transitions are rejected
// rather than silently repaired.
func (s *Session) Transition(event Event) error {
	if s == nil {
		return ErrSessionClosed
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	next, err := s.state.Transition(event)
	if err != nil {
		return err
	}
	s.state = next
	return nil
}

// KeepAlive refreshes the idle watchdog without accepting audio.
//
// The transport calls it when a continuity window starts so the configured
// window, rather than the ordinary idle timeout, decides when a parked
// conversation is released.
func (s *Session) KeepAlive() {
	if s == nil {
		return
	}
	s.mu.Lock()
	s.touchLocked()
	s.mu.Unlock()
}

// AcceptFrame validates, decodes, buffers, and segments one inbound frame.
//
// Duplicate and stale sequences are counted and rejected. The method blocks
// only when the bounded segment queue is full, applying backpressure instead
// of dropping child speech. It returns a context error when the session ends
// while waiting for the consumer.
func (s *Session) AcceptFrame(ctx context.Context, audioFrame frame.Frame) error {
	if s == nil {
		return ErrSessionClosed
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := audioFrame.Validate(); err != nil {
		return fmt.Errorf("validate audio frame: %w", err)
	}
	if audioFrame.SchemaVersion != s.schemaVersion ||
		audioFrame.DeviceID != s.deviceID ||
		audioFrame.StreamID != s.streamID {
		return ErrSessionIdentity
	}
	select {
	case <-s.ctx.Done():
		return s.Err()
	default:
	}

	s.mu.Lock()

	if s.hasSequence {
		switch {
		case audioFrame.Sequence == s.lastSequence:
			s.duplicateFrames++
			s.mu.Unlock()
			return buffer.ErrDuplicateFrame
		case !isNewerSequence(audioFrame.Sequence, s.lastSequence):
			s.outOfOrderFrames++
			s.mu.Unlock()
			return ErrOutOfOrderFrame
		}
	}

	pcm := make([]int16, frame.SamplesPerFrame)
	decodedSamples, err := s.codec.DecodePacket(audioFrame.Payload, pcm)
	if err != nil {
		s.mu.Unlock()
		return fmt.Errorf("decode audio frame: %w", err)
	}
	if decodedSamples <= 0 || decodedSamples > len(pcm) {
		s.mu.Unlock()
		return fmt.Errorf("decode audio frame: invalid sample count %d", decodedSamples)
	}
	pcm = pcm[:decodedSamples]

	if s.processor != nil {
		var reference []int16
		if s.referenceAudio != nil {
			// Copy because the playback path may overwrite its buffer while the
			// adaptive echo filter is still converging on this frame.
			reference = append([]int16(nil), s.referenceAudio()...)
		}
		if _, processErr := s.processor.Process(pcm, reference); processErr != nil {
			s.mu.Unlock()
			return fmt.Errorf("preprocess audio frame: %w", processErr)
		}
		s.updatePreprocessQualityLocked()
	}

	if err := s.buffer.Push(audioFrame); err != nil {
		s.mu.Unlock()
		return fmt.Errorf("buffer audio frame: %w", err)
	}
	s.hasSequence = true
	s.lastSequence = audioFrame.Sequence
	s.acceptedFrames++
	s.touchLocked()
	s.advanceListeningLocked()

	isSpeech := s.detector.IsSpeech(int16Bytes(pcm))
	s.updateDetectorQualityLocked()
	s.metricsAt = time.Now().UTC()
	segment, hasSegment, err := s.consumeSpeechLocked(audioFrame, pcm, isSpeech)
	if err != nil {
		s.mu.Unlock()
		return err
	}
	s.mu.Unlock()
	if hasSegment {
		return s.emitSegment(ctx, segment)
	}
	return nil
}

// NextSegment returns the next completed utterance.
//
// The returned segment contains a private PCM copy. A closed session returns
// its terminal cause; a caller context cancellation is returned unchanged.
func (s *Session) NextSegment(ctx context.Context) (AudioSegment, error) {
	if s == nil {
		return AudioSegment{}, ErrSessionClosed
	}
	if ctx == nil {
		ctx = context.Background()
	}
	select {
	case segment, ok := <-s.segments:
		if !ok {
			return AudioSegment{}, s.Err()
		}
		return segment, nil
	case <-ctx.Done():
		return AudioSegment{}, ctx.Err()
	case <-s.ctx.Done():
		return AudioSegment{}, s.Err()
	}
}

// Stats returns a bounded diagnostic snapshot without exposing audio data.
func (s *Session) Stats() SessionStats {
	if s == nil {
		return SessionStats{}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return SessionStats{
		State:            s.state,
		AcceptedFrames:   s.acceptedFrames,
		DuplicateFrames:  s.duplicateFrames,
		OutOfOrderFrames: s.outOfOrderFrames,
		Buffer:           s.buffer.Stats(),
		SegmentsEmitted:  s.segmentsEmitted,
		LastSequence:     s.lastSequence,
		HasSequence:      s.hasSequence,
		LastActivity:     s.lastActivity,
	}
}

// Quality returns the latest bounded preprocessing quality snapshot.
func (s *Session) Quality() QualitySnapshot {
	if s == nil {
		return QualitySnapshot{}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	snapshot := s.quality
	snapshot.SessionID = s.id
	snapshot.DeviceID = s.deviceID
	snapshot.State = s.state
	snapshot.LastActivity = s.lastActivity
	if snapshot.MetricsAt.IsZero() {
		snapshot.MetricsAt = s.lastActivity
	}
	return snapshot
}

func (s *Session) watchTimeouts() {
	idleTimer := time.NewTimer(s.idleTimeout)
	defer idleTimer.Stop()
	maxTimer := time.NewTimer(s.maxDuration)
	defer maxTimer.Stop()

	for {
		select {
		case <-s.ctx.Done():
			return
		case <-s.activity:
			if !idleTimer.Stop() {
				select {
				case <-idleTimer.C:
				default:
				}
			}
			idleTimer.Reset(s.idleTimeout)
		case <-idleTimer.C:
			s.mu.Lock()
			remaining := s.idleTimeout - time.Since(s.lastActivity)
			s.mu.Unlock()
			if remaining <= 0 {
				s.cancel(ErrSessionIdleTimeout)
				return
			}
			idleTimer.Reset(remaining)
		case <-maxTimer.C:
			s.cancel(ErrSessionDurationLimit)
			return
		}
	}
}

func (s *Session) touchLocked() {
	s.lastActivity = time.Now().UTC()
	select {
	case s.activity <- struct{}{}:
	default:
	}
}

func (s *Session) updatePreprocessQualityLocked() {
	stats := s.processor.Stats()
	s.quality.EchoCancellationReady = stats.EchoCancelled > 0
	s.quality.EchoConvergence = stats.EchoConvergence
	s.quality.NoiseFloor = stats.NoiseFloor
	s.quality.AgcGain = stats.CurrentGain
}

func (s *Session) updateDetectorQualityLocked() {
	observer, ok := s.detector.(vad.ObservingDetector)
	if !ok {
		return
	}
	stats := observer.Stats()
	s.quality.VADConfidence = stats.Confidence
	s.quality.VADNoiseFloor = stats.NoiseFloor
	if s.quality.NoiseFloor == 0 && stats.NoiseFloor > 0 {
		s.quality.NoiseFloor = stats.NoiseFloor
	}
}

func (s *Session) advanceListeningLocked() {
	if s.state != StateIdle {
		return
	}
	next, err := s.state.Transition(EventStartListening)
	if err == nil {
		s.state = next
	}
}

func (s *Session) consumeSpeechLocked(
	audioFrame frame.Frame,
	pcm []int16,
	isSpeech bool,
) (AudioSegment, bool, error) {
	if !s.segmentActive {
		if !isSpeech {
			s.rememberPreRollLocked(audioFrame, pcm)
			return AudioSegment{}, false, nil
		}
		s.beginSegmentLocked(audioFrame)
		for _, preRoll := range s.preRoll {
			if preRoll.sequence >= audioFrame.Sequence {
				continue
			}
			if len(s.segmentSamples) == 0 {
				s.segmentSequenceStart = preRoll.sequence
				s.segmentCapturedAt = preRoll.capturedAt
			}
			s.appendSegmentSamplesLocked(preRoll.samples)
		}
		s.preRoll = s.preRoll[:0]
	}

	if isSpeech {
		s.segmentSequenceEnd = audioFrame.Sequence
		s.appendSegmentSamplesLocked(pcm)
	}

	if s.segmentSampleLimit() > 0 && len(s.segmentSamples) >= s.segmentSampleLimit() {
		// A continuous utterance must still reach ASR in bounded chunks.
		segment, err := s.finishSegmentLocked(true)
		if err != nil {
			return AudioSegment{}, false, err
		}
		return segment, true, nil
	}
	if isSpeech {
		return AudioSegment{}, false, nil
	}
	segment, err := s.finishSegmentLocked(false)
	if err != nil {
		return AudioSegment{}, false, err
	}
	return segment, len(segment.PCM) > 0, nil
}

func (s *Session) beginSegmentLocked(audioFrame frame.Frame) {
	s.segmentActive = true
	s.segmentSequenceStart = audioFrame.Sequence
	s.segmentSequenceEnd = audioFrame.Sequence
	s.segmentCapturedAt = audioFrame.CapturedAt
	s.segmentSamples = s.segmentSamples[:0]
}

func (s *Session) appendSegmentSamplesLocked(samples []int16) {
	s.segmentSamples = append(s.segmentSamples, samples...)
}

func (s *Session) rememberPreRollLocked(audioFrame frame.Frame, pcm []int16) {
	if s.preRollFrames <= 0 {
		return
	}
	copied := append([]int16(nil), pcm...)
	entry := frameSamples{
		sequence:   audioFrame.Sequence,
		capturedAt: audioFrame.CapturedAt,
		samples:    copied,
	}
	if len(s.preRoll) < s.preRollFrames {
		s.preRoll = append(s.preRoll, entry)
		return
	}
	copy(s.preRoll, s.preRoll[1:])
	s.preRoll[len(s.preRoll)-1] = entry
}

func (s *Session) finishSegmentLocked(force bool) (AudioSegment, error) {
	samples := append([]int16(nil), s.segmentSamples...)
	segment := AudioSegment{
		ID:              fmt.Sprintf("%s:%d", s.id, s.segmentsEmitted+1),
		SessionID:       s.id,
		DeviceID:        s.deviceID,
		StreamID:        s.streamID,
		ContentCategory: s.contentCategory,
		SequenceStart:   s.segmentSequenceStart,
		SequenceEnd:     s.segmentSequenceEnd,
		CapturedAt:      s.segmentCapturedAt,
		PCM:             samples,
	}
	s.segmentActive = false
	s.segmentSamples = s.segmentSamples[:0]
	if !force && len(samples)/frame.SamplesPerFrame < s.minimumSpeechFrames {
		return AudioSegment{}, nil
	}
	if len(samples) == 0 {
		return AudioSegment{}, nil
	}
	s.segmentsEmitted++
	return segment, nil
}

func (s *Session) emitSegment(ctx context.Context, segment AudioSegment) error {
	if len(segment.PCM) == 0 {
		return nil
	}
	if s.segmentHandler != nil {
		s.segmentHandler(segment)
		return nil
	}
	s.segmentMu.RLock()
	defer s.segmentMu.RUnlock()
	if s.segmentClosed {
		return s.Err()
	}
	select {
	case s.segments <- segment:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	case <-s.ctx.Done():
		return s.Err()
	}
}

func (s *Session) closeSegments() {
	s.segmentMu.Lock()
	defer s.segmentMu.Unlock()
	if s.segmentClosed {
		return
	}
	s.segmentClosed = true
	close(s.segments)
}

func (s *Session) segmentSampleLimit() int {
	if s.maxSegmentLength <= 0 {
		return 0
	}
	frames := int(s.maxSegmentLength / (frame.DurationMS * time.Millisecond))
	if frames < 1 {
		frames = 1
	}
	return frames * frame.SamplesPerFrame
}

func normalizeSessionConfig(config SessionConfig) SessionConfig {
	if config.SchemaVersion == "" {
		config.SchemaVersion = frame.SchemaVersion
	}
	if config.BufferCapacityFrames <= 0 {
		config.BufferCapacityFrames = buffer.DefaultCapacityFrames
	}
	if config.SegmentQueueDepth <= 0 {
		config.SegmentQueueDepth = defaultSegmentQueueDepth
	}
	if config.PreRollFrames < 0 {
		config.PreRollFrames = 0
	}
	if config.PreRollFrames == 0 {
		config.PreRollFrames = defaultPreRollFrames
	}
	if config.IdleTimeout <= 0 {
		config.IdleTimeout = defaultIdleTimeout
	}
	if config.MaxDuration <= 0 {
		config.MaxDuration = defaultMaxDuration
	}
	if config.MaxSegmentLength <= 0 {
		config.MaxSegmentLength = defaultMaxSegmentLength
	}
	if config.MinimumSpeechMS <= 0 {
		config.MinimumSpeechMS = defaultMinimumSpeechMS
	}
	if config.CodecFactory == nil {
		config.CodecFactory = func() (codec.Codec, error) {
			return codec.NewOpusCodec()
		}
	}
	if config.DetectorFactory == nil {
		config.DetectorFactory = func() vad.Detector {
			return vad.NewEnergyDetector(vad.Config{})
		}
	}
	return config
}

func millisecondsToFrames(durationMS int) int {
	frames := (durationMS + frame.DurationMS - 1) / frame.DurationMS
	if frames < 1 {
		return 1
	}
	return frames
}

func int16Bytes(samples []int16) []byte {
	data := make([]byte, len(samples)*2)
	for index, sample := range samples {
		value := uint16(sample)
		data[index*2] = byte(value)
		data[index*2+1] = byte(value >> 8)
	}
	return data
}

func isNewerSequence(candidate uint32, current uint32) bool {
	return int32(candidate-current) > 0
}
