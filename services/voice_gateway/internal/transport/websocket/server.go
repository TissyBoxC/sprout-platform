// Package websocket contains the authenticated device realtime audio transport.
//
// Control messages are JSON and audio payloads are binary. The server owns one
// reader and one writer goroutine per connection, so session state and write
// deadlines remain unambiguous under concurrent network activity.
package websocket

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/TissyBoxC/sprout-platform/services/voice_gateway/internal/audio/buffer"
	"github.com/TissyBoxC/sprout-platform/services/voice_gateway/internal/audio/frame"
	"github.com/TissyBoxC/sprout-platform/services/voice_gateway/internal/audio/playback"
	"github.com/TissyBoxC/sprout-platform/services/voice_gateway/internal/audio/preprocess"
	"github.com/TissyBoxC/sprout-platform/services/voice_gateway/internal/audio/vad"
	"github.com/TissyBoxC/sprout-platform/services/voice_gateway/internal/session"
	"github.com/gorilla/websocket"
)

const (
	defaultReadLimitBytes    = 64 * 1024
	defaultWriteQueueDepth   = 64
	defaultSegmentQueueDepth = 4
	// The device speaker owns the guardian volume cap and mute state, so the
	// gateway passes reply PCM through unchanged. Applying an extra fixed gain
	// here would attenuate every reply twice and quietly halve the output.
	defaultVolumePercent       = 100
	defaultMaxVolumePercent    = 100
	defaultPongWait            = 60 * time.Second
	defaultPingInterval        = 25 * time.Second
	defaultWriteTimeout        = 10 * time.Second
	defaultHandshakeTimeout    = 10 * time.Second
	defaultSessionTTL          = 15 * time.Minute
	defaultMaxFramesPerSecond  = 80
	defaultMaxBytesPerSecond   = 128 * 1024
	defaultSendQueueDropPolicy = SendQueueDisconnect
)

// ServerConfig bounds one device connection and the sessions it may create.
//
// Limiting both frames and bytes is intentional: a small number of maximum
// sized frames must not consume the same budget as normal 20 ms Opus frames.
type ServerConfig struct {
	ReadLimitBytes     int64
	WriteQueueDepth    int
	SegmentQueueDepth  int
	PongWait           time.Duration
	PingInterval       time.Duration
	WriteTimeout       time.Duration
	MaxFramesPerSecond int
	MaxBytesPerSecond  int
	SessionTTL         time.Duration
	Manager            *session.Manager
	Verifier           TokenVerifier
	DetectorFactory    func() vad.Detector
	// PreprocessFactory builds the per-session echo cancellation, noise
	// suppression, and gain calibration chain. Optional; nil keeps the plain
	// codec and VAD path.
	PreprocessFactory func() (preprocess.Processor, error)
	// ReferenceAudio returns the most recent speaker playback for one device so
	// echo cancellation can subtract it from that device's microphone signal.
	// A nil provider or nil result means speaker output is silent.
	ReferenceAudio  func(deviceID string) []int16
	OnSessionClosed func(sessionID string, cause error)
	OnSegmentReady  func(segment session.AudioSegment)
	OnBackpressure  func(deviceID string, dropped uint64)
	// SegmentHandler, when set, owns the conversation pipeline. It receives each
	// completed utterance plus a sink that streams synthesized reply audio back
	// to the device over this connection. Invocations for one connection are
	// serialised, so the handler may keep per-session state.
	SegmentHandler func(segment session.AudioSegment, sink AudioSink)
	// onPlaybackScheduler observes the per-connection scheduler once it is
	// created. Test-only seam; production leaves it nil.
	onPlaybackScheduler func(*playback.Scheduler)
}

// AudioSink accepts one synthesized PCM frame for the connected device.
//
// The transport owns scheduling, encoding, framing, sequence numbering, and
// backpressure; the conversation pipeline supplies only 16 kHz mono PCM. The
// optional onPlayed callback receives the exact gain-applied frame the speaker
// is about to play, which the echo canceller needs to subtract.
type AudioSink interface {
	EnqueueAudio(
		itemID string,
		priority playback.Priority,
		pcm []int16,
		interruptible bool,
		onPlayed func([]int16),
	) error
}

// turnObserver is the optional transport-internal half of a conversation turn.
//
// The segment consumer brackets each SegmentHandler invocation with these
// callbacks so the session state machine advances thinking -> speaking ->
// listening without changing the public AudioSink signature.
type turnObserver interface {
	beginTurn(segment session.AudioSegment)
	completeTurn(segment session.AudioSegment)
}

// SendQueuePolicy determines what happens when a slow connection fills the
// bounded outbound queue.
type SendQueuePolicy string

const (
	// SendQueueDisconnect closes the connection once the queue is full. Voice
	// transport prioritises bounded memory and predictable latency over
	// silently delaying control messages.
	SendQueueDisconnect SendQueuePolicy = "disconnect"
)

// Server accepts authenticated device audio sessions.
//
// One Server may be shared by all handlers. It never stores raw audio or
// tokens, and it logs only identifiers, error codes, and counters.
type Server struct {
	config   ServerConfig
	upgrader websocket.Upgrader
	logger   *slog.Logger
	stats    serverStats
}

type serverStats struct {
	acceptedConnections atomic.Uint64
	rejectedConnections atomic.Uint64
	backpressureDrops   atomic.Uint64
	malformedFrames     atomic.Uint64
}

// NewServer creates a realtime transport with secure production defaults.
//
// A missing manager or token verifier is a configuration error rather than an
// insecure fallback, because an unauthenticated audio endpoint is not
// acceptable for a child-facing product.
func NewServer(config ServerConfig, logger *slog.Logger) (*Server, error) {
	if config.Manager == nil {
		return nil, errors.New("websocket server requires a session manager")
	}
	if config.Verifier == nil {
		return nil, errors.New("websocket server requires a token verifier")
	}
	if logger == nil {
		logger = slog.Default()
	}
	config = normalizeServerConfig(config)

	return &Server{
		config: config,
		logger: logger,
		upgrader: websocket.Upgrader{
			HandshakeTimeout: config.WriteTimeout,
			ReadBufferSize:   defaultReadLimitBytes,
			WriteBufferSize:  defaultWriteQueueDepth * frame.MaxPayloadBytes,
			CheckOrigin:      func(*http.Request) bool { return true },
		},
	}, nil
}

// Handler returns the HTTP handler for the realtime endpoint.
//
// Authentication happens before the upgrade, so an invalid or missing token
// receives a normal HTTP 401 and never reaches the WebSocket state machine.
func (s *Server) Handler() http.Handler {
	return http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if s == nil {
			http.Error(response, "service unavailable", http.StatusServiceUnavailable)
			return
		}
		identity, err := authenticateDevice(request, s.config.Verifier)
		if err != nil {
			s.stats.rejectedConnections.Add(1)
			response.Header().Set("Content-Type", "application/json; charset=utf-8")
			response.WriteHeader(http.StatusUnauthorized)
			_ = writeJSON(response, NewErrorFrame("", "", errorCodeUnauthenticated, "设备认证失败，请重新登录", false))
			return
		}
		upgradeHeaders := http.Header{}
		_, selectedProtocol := deviceToken(request)
		if selectedProtocol != "" {
			upgradeHeaders.Set("Sec-WebSocket-Protocol", selectedProtocol)
		}
		connection, err := s.upgrader.Upgrade(response, request, upgradeHeaders)
		if err != nil {
			s.stats.rejectedConnections.Add(1)
			s.logger.Warn("device websocket upgrade failed", "device_id", identity.DeviceID)
			return
		}
		s.stats.acceptedConnections.Add(1)
		s.serveConnection(connection, identity)
	})
}

// Stats returns process-local transport counters for diagnostics.
func (s *Server) Stats() ServerStats {
	if s == nil {
		return ServerStats{}
	}
	return ServerStats{
		AcceptedConnections: s.stats.acceptedConnections.Load(),
		RejectedConnections: s.stats.rejectedConnections.Load(),
		BackpressureDrops:   s.stats.backpressureDrops.Load(),
		MalformedFrames:     s.stats.malformedFrames.Load(),
	}
}

// ServerStats is a point-in-time transport counter snapshot.
type ServerStats struct {
	AcceptedConnections uint64
	RejectedConnections uint64
	BackpressureDrops   uint64
	MalformedFrames     uint64
}

func (s *Server) serveConnection(connection *websocket.Conn, identity DeviceIdentity) {
	connectionContext, cancel := context.WithCancel(context.Background())
	defer cancel()
	defer connection.Close()
	connection.SetReadLimit(s.config.ReadLimitBytes)
	_ = connection.SetReadDeadline(time.Now().Add(s.config.PongWait))
	connection.SetPongHandler(func(string) error {
		return connection.SetReadDeadline(time.Now().Add(s.config.PongWait))
	})

	writer := newConnectionWriter(connection, s.config.WriteQueueDepth, s.config.WriteTimeout)
	defer writer.Close()

	var currentSession *session.Session
	var playbackGeneration atomic.Uint64
	var sessionMutex sync.RWMutex
	getSession := func() *session.Session {
		sessionMutex.RLock()
		defer sessionMutex.RUnlock()
		return currentSession
	}
	setSession := func(voiceSession *session.Session) {
		sessionMutex.Lock()
		currentSession = voiceSession
		sessionMutex.Unlock()
	}

	writer.start()
	segmentQueue := make(chan session.AudioSegment, s.config.SegmentQueueDepth)
	segmentDone := make(chan struct{})
	sink := &connectionAudioSink{
		writer: writer,
	}
	// Start reply turns at epoch 1 so zero unambiguously means "untagged"
	// (safety announcements) and every conversation frame is checked.
	sink.turnEpoch.Store(1)
	scheduler, err := playback.NewScheduler(sink, defaultVolumePercent, defaultMaxVolumePercent)
	if err != nil {
		s.logger.Warn("device playback scheduler unavailable", "device_id", identity.DeviceID)
		return
	}
	defer scheduler.Close()
	sink.scheduler = scheduler
	if s.config.onPlaybackScheduler != nil {
		s.config.onPlaybackScheduler(scheduler)
	}
	sink.session = func() *session.Session {
		voiceSession := getSession()
		if voiceSession == nil {
			return nil
		}
		return voiceSession
	}
	sink.sessionID = func() string {
		voiceSession := sink.currentSession()
		if voiceSession == nil {
			return ""
		}
		return voiceSession.ID()
	}
	sink.notifyState = func(voiceSession *session.Session, state string) {
		if voiceSession == nil {
			return
		}
		if getSession() != voiceSession {
			return
		}
		notification := NewControlFrame(controlTypeSessionState, voiceSession.ID(), identity.DeviceID)
		notification.State = state
		_ = writer.enqueueText(notification)
	}
	createSession := func(control ControlFrame) (*session.Session, error) {
		return s.config.Manager.Create(session.SessionConfig{
			ID:                control.SessionID,
			DeviceID:          identity.DeviceID,
			SchemaVersion:     frame.SchemaVersion,
			StreamID:          control.StreamID,
			MinimumSpeechMS:   frame.DurationMS,
			DetectorFactory:   s.config.DetectorFactory,
			PreprocessFactory: s.config.PreprocessFactory,
			ReferenceAudio: func() []int16 {
				if s.config.ReferenceAudio == nil {
					return nil
				}
				return s.config.ReferenceAudio(identity.DeviceID)
			},
			SegmentHandler: func(segment session.AudioSegment) {
				select {
				case segmentQueue <- segment:
				case <-connectionContext.Done():
				default:
					s.stats.backpressureDrops.Add(1)
					if s.config.OnBackpressure != nil {
						s.config.OnBackpressure(identity.DeviceID, s.stats.backpressureDrops.Load())
					}
				}
			},
		})
	}
	attachSession := func(voiceSession *session.Session) {
		setSession(voiceSession)
		generation := playbackGeneration.Add(1)
		go s.clearPlaybackOnSessionEnd(
			connectionContext,
			voiceSession,
			scheduler,
			generation,
			&playbackGeneration,
			segmentDone,
		)
	}
	sink.onTurnStart = func(segment session.AudioSegment) {
		voiceSession := getSession()
		if voiceSession == nil || voiceSession.ID() != segment.SessionID {
			return
		}
		if transitionErr := voiceSession.Transition(session.EventStartThinking); transitionErr != nil {
			return
		}
		sink.notifyState(voiceSession, string(session.StateThinking))
	}
	sink.onTurnEnd = func(segment session.AudioSegment) {
		voiceSession := getSession()
		if voiceSession == nil || voiceSession.ID() != segment.SessionID {
			return
		}
		// A turn with no reply stays thinking and finishes directly; a turn that
		// spoke returns to listening via the speaking edge. Both release the
		// session so the device can re-arm for the next utterance.
		var transitionErr error
		switch voiceSession.State() {
		case session.StateSpeaking:
			transitionErr = voiceSession.Transition(session.EventStartListening)
		case session.StateThinking:
			transitionErr = voiceSession.Transition(session.EventFinishTurn)
		default:
			return
		}
		if transitionErr != nil {
			return
		}
		sink.notifyState(voiceSession, string(session.StateListening))
	}
	scheduleBargeIn := func(voiceSession *session.Session) {
		if voiceSession == nil || voiceSession.State() != session.StateSpeaking {
			return
		}
		// Serialize the clear against reply enqueues: any frame ordered before
		// this point is dropped by Clear, and any frame ordered after it sees
		// the session already back in listening and is refused.
		sink.turnMu.Lock()
		// Advance the epoch before clearing so a frame the scheduler already
		// dequeued for the canceled turn fails the SendAudio check below.
		sink.turnEpoch.Add(1)
		scheduler.Clear(false)
		if transitionErr := voiceSession.Transition(session.EventStartListening); transitionErr == nil {
			sink.notifyState(voiceSession, string(session.StateListening))
		}
		sink.turnMu.Unlock()
	}
	go s.consumeSegments(connectionContext, segmentQueue, segmentDone, sink)
	heartbeatDone := make(chan struct{})
	go s.sendHeartbeats(connectionContext, writer, heartbeatDone)

	limiter := newRateLimiter(s.config.MaxFramesPerSecond, s.config.MaxBytesPerSecond)
	var disconnectReason string

	for {
		messageType, payload, err := connection.ReadMessage()
		if err != nil {
			if websocket.IsCloseError(err, websocket.CloseNormalClosure, websocket.CloseGoingAway) {
				disconnectReason = "client_closed"
			} else {
				disconnectReason = "transport_closed"
			}
			break
		}

		switch messageType {
		case websocket.TextMessage:
			if !limiter.allowControl() {
				s.stats.malformedFrames.Add(1)
				s.sendError(writer, getSession(), errorCodeRateLimited, "操作过于频繁，请稍后重试", true)
				continue
			}
			control, decodeErr := DecodeControlFrame(payload)
			if decodeErr != nil {
				s.stats.malformedFrames.Add(1)
				s.sendError(writer, getSession(), errorCodeInvalidControl, "控制信息格式不正确", false)
				continue
			}
			if control.DeviceID != identity.DeviceID {
				s.stats.malformedFrames.Add(1)
				s.sendError(writer, getSession(), errorCodeUnauthenticated, "设备身份不一致", false)
				continue
			}
			if control.SessionID != "" && getSession() != nil && control.SessionID != getSession().ID() {
				s.sendError(writer, getSession(), errorCodeSessionNotFound, "会话已结束，请重新开始", false)
				continue
			}

			switch control.Type {
			case controlTypeWakeDetected:
				voiceSession := getSession()
				if voiceSession == nil {
					if control.SessionID == "" || control.StreamID == "" {
						s.sendError(writer, nil, errorCodeInvalidControl, "控制信息格式不正确", false)
						continue
					}
					created, createErr := createSession(control)
					if createErr != nil {
						s.sendError(writer, nil, mapSessionError(createErr), "当前无法开始监听，请稍后重试", true)
						continue
					}
					if transitionErr := created.Transition(session.EventStartListening); transitionErr != nil {
						s.config.Manager.Remove(created.ID())
						s.sendError(writer, created, errorCodeInternal, "当前无法开始监听，请稍后重试", true)
						continue
					}
					attachSession(created)
					started := NewControlFrame(controlTypeSessionStarted, created.ID(), identity.DeviceID)
					started.ExpiresAt = time.Now().UTC().Add(s.config.SessionTTL).Format(time.RFC3339Nano)
					if err := writer.enqueueText(started); err != nil {
						disconnectReason = "send_failed"
						goto closed
					}
					sink.notifyState(created, string(session.StateListening))
					continue
				}
				if control.StreamID != voiceSession.StreamID() {
					s.stats.malformedFrames.Add(1)
					s.sendError(
						writer,
						voiceSession,
						errorCodeInvalidControl,
						"唤醒信息与当前监听不一致",
						false,
					)
					continue
				}
				if voiceSession.State() != session.StateListening {
					if transitionErr := voiceSession.Transition(
						session.EventStartListening,
					); transitionErr != nil &&
						!errors.Is(transitionErr, session.ErrSessionClosed) {
						s.stats.malformedFrames.Add(1)
						s.sendError(
							writer,
							voiceSession,
							errorCodeInvalidControl,
							"当前无法开始监听",
							false,
						)
						continue
					}
					sink.notifyState(voiceSession, string(session.StateListening))
				}
			case controlTypeSessionStart:
				if getSession() != nil {
					s.sendError(writer, getSession(), errorCodeInvalidControl, "当前已有进行中的监听", false)
					continue
				}
				voiceSession, createErr := createSession(control)
				if createErr != nil {
					s.sendError(writer, nil, mapSessionError(createErr), "当前无法开始监听，请稍后重试", true)
					continue
				}
				if transitionErr := voiceSession.Transition(session.EventStartListening); transitionErr != nil {
					s.config.Manager.Remove(voiceSession.ID())
					s.sendError(writer, voiceSession, errorCodeInternal, "当前无法开始监听，请稍后重试", true)
					continue
				}
				attachSession(voiceSession)
				started := NewControlFrame(controlTypeSessionStarted, voiceSession.ID(), identity.DeviceID)
				started.ExpiresAt = time.Now().UTC().Add(s.config.SessionTTL).Format(time.RFC3339Nano)
				if err := writer.enqueueText(started); err != nil {
					disconnectReason = "send_failed"
					goto closed
				}
				sink.notifyState(voiceSession, string(session.StateListening))
			case controlTypeSessionEnd, controlTypeCancel:
				voiceSession := getSession()
				if voiceSession == nil {
					s.sendError(writer, nil, errorCodeSessionNotFound, "当前没有进行中的监听", false)
					continue
				}
				reason := control.Reason
				if reason == "" {
					reason = "client_request"
				}
				if contErr := voiceSession.Transition(session.EventReset); contErr != nil {
					s.sendError(writer, voiceSession, errorCodeInternal, "当前无法结束监听，请稍后重试", true)
					continue
				}
				s.config.Manager.Remove(voiceSession.ID())
				sink.notifyState(voiceSession, string(session.StateIdle))
				setSession(nil)
				closedFrame := NewControlFrame(controlTypeSessionClosed, voiceSession.ID(), identity.DeviceID)
				closedFrame.Reason = reason
				if err := writer.enqueueText(closedFrame); err != nil {
					disconnectReason = "send_failed"
					goto closed
				}
				if control.Type == controlTypeCancel {
					disconnectReason = "cancelled"
				}
			case controlTypePing:
				pong := NewControlFrame(controlTypePong, control.SessionID, identity.DeviceID)
				if err := writer.enqueueText(pong); err != nil {
					disconnectReason = "send_failed"
					goto closed
				}
			case controlTypePong:
				continue
			case controlTypeSessionStarted, controlTypeSessionState, controlTypeSessionClosed, controlTypeError:
				s.stats.malformedFrames.Add(1)
				s.sendError(writer, getSession(), errorCodeInvalidControl, "控制信息方向不正确", false)
			default:
				s.stats.malformedFrames.Add(1)
				s.sendError(writer, getSession(), errorCodeInvalidControl, "控制信息格式不正确", false)
			}
		case websocket.BinaryMessage:
			voiceSession := getSession()
			if voiceSession == nil {
				s.stats.malformedFrames.Add(1)
				s.sendError(writer, nil, errorCodeSessionNotFound, "请先开始监听", false)
				continue
			}
			if !limiter.allowAudio(len(payload)) {
				s.stats.malformedFrames.Add(1)
				s.sendError(writer, voiceSession, errorCodeRateLimited, "音频发送过快，请稍后重试", true)
				continue
			}
			envelope, envelopeErr := DecodeAudioEnvelope(payload)
			if envelopeErr != nil {
				s.stats.malformedFrames.Add(1)
				s.sendError(writer, voiceSession, mapAudioEnvelopeError(envelopeErr), "音频数据格式不正确", false)
				continue
			}
			if envelope.SessionID != voiceSession.ID() {
				s.stats.malformedFrames.Add(1)
				s.sendError(writer, voiceSession, errorCodeSessionNotFound, "会话已结束，请重新开始", false)
				continue
			}
			if voiceSession.State() == session.StateSpeaking {
				scheduleBargeIn(voiceSession)
			}
			if err := voiceSession.AcceptFrame(connectionContext, frame.New(
				fmt.Sprintf("%s_%d", voiceSession.ID(), envelope.Sequence),
				identity.DeviceID,
				voiceSession.StreamID(),
				envelope.Sequence,
				envelope.CapturedAt,
				envelope.Payload,
			)); err != nil {
				errorCode := mapSessionError(err)
				if errors.Is(err, buffer.ErrDuplicateFrame) {
					// A reconnect can replay the last packet. It is already in
					// the stream window, so acknowledge nothing and keep the
					// session alive instead of disconnecting healthy audio.
					continue
				}
				s.stats.malformedFrames.Add(1)
				s.sendError(writer, voiceSession, errorCode, "音频数据未受理", false)
				if errors.Is(err, session.ErrSessionClosed) ||
					errors.Is(err, session.ErrSessionIdleTimeout) ||
					errors.Is(err, session.ErrSessionDurationLimit) {
					// The device is still connected, so tell it the session fell
					// idle before the reader tears the connection down.
					sink.notifyState(voiceSession, string(session.StateIdle))
					disconnectReason = "session_ended"
					goto closed
				}
				continue
			}
		default:
			s.stats.malformedFrames.Add(1)
			s.sendError(writer, getSession(), errorCodeInvalidControl, "不支持的数据类型", false)
		}
	}

closed:
	_ = writer.Close()
	close(heartbeatDone)
	close(segmentDone)
	if voiceSession := getSession(); voiceSession != nil {
		cause := voiceSession.Err()
		s.config.Manager.Remove(voiceSession.ID())
		setSession(nil)
		if s.config.OnSessionClosed != nil {
			s.config.OnSessionClosed(voiceSession.ID(), cause)
		}
	}
	if disconnectReason == "rate_limited" {
		_ = connection.WriteControl(
			websocket.CloseMessage,
			websocket.FormatCloseMessage(websocket.ClosePolicyViolation, "rate limited"),
			time.Now().Add(s.config.WriteTimeout),
		)
	}
}

func (s *Server) consumeSegments(
	ctx context.Context,
	segments <-chan session.AudioSegment,
	done <-chan struct{},
	sink AudioSink,
) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-done:
			return
		case segment := <-segments:
			if s.config.SegmentHandler != nil {
				observer, _ := sink.(turnObserver)
				if observer != nil {
					observer.beginTurn(segment)
				}
				handlerDone := make(chan struct{})
				go func(segment session.AudioSegment) {
					defer close(handlerDone)
					s.config.SegmentHandler(segment, sink)
				}(segment)
				select {
				case <-handlerDone:
				case <-ctx.Done():
					return
				case <-done:
					return
				}
				if observer != nil {
					// The handler enqueues the full reply before returning, so the
					// turn is re-armed for the next utterance at this point.
					observer.completeTurn(segment)
				}
				continue
			}
			if s.config.OnSegmentReady != nil {
				callbackDone := make(chan struct{})
				go func() {
					defer close(callbackDone)
					s.config.OnSegmentReady(segment)
				}()
				select {
				case <-callbackDone:
				case <-ctx.Done():
					return
				case <-done:
					return
				}
			}
		}
	}
}

func (s *Server) clearPlaybackOnSessionEnd(
	ctx context.Context,
	voiceSession *session.Session,
	scheduler *playback.Scheduler,
	generation uint64,
	currentGeneration *atomic.Uint64,
	done <-chan struct{},
) {
	if voiceSession == nil || scheduler == nil || currentGeneration == nil {
		return
	}
	select {
	case <-voiceSession.Done():
		if currentGeneration.Load() == generation {
			scheduler.Clear(false)
		}
	case <-ctx.Done():
	case <-done:
	}
}

func (s *Server) sendHeartbeats(
	ctx context.Context,
	writer *connectionWriter,
	done <-chan struct{},
) {
	ticker := time.NewTicker(s.config.PingInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-done:
			return
		case <-ticker.C:
			if err := writer.enqueueControl(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}

// connectionAudioSink frames synthesized reply audio for one device connection.
//
// The gateway numbers outbound frames itself so the device can drop duplicates
// after a resume. Frames are queued through the bounded writer, so a slow
// device applies backpressure to the conversation handler instead of growing
// unbounded memory.
type connectionAudioSink struct {
	writer      *connectionWriter
	scheduler   *playback.Scheduler
	sessionID   func() string
	session     func() *session.Session
	notifyState func(*session.Session, string)
	onTurnStart func(session.AudioSegment)
	onTurnEnd   func(session.AudioSegment)
	sequence    atomic.Uint32
	// turnMu orders reply enqueues against a barge-in clear so a canceled turn
	// cannot slip a frame in just after playback was dropped.
	turnMu sync.Mutex
	// turnEpoch increments on every barge-in. The scheduler dequeues a frame and
	// sends it after releasing its own lock, so a barge-in can land in that gap;
	// SendAudio re-checks this epoch under turnMu to drop frames produced by a
	// turn that a barge-in has already canceled.
	//
	// Epoch zero is reserved for untagged audio (for example safety
	// announcements), so a tagged reply turn starts at one.
	turnEpoch atomic.Uint64
}

// EnqueueAudio queues one PCM frame on this connection's playback scheduler.
func (sink *connectionAudioSink) EnqueueAudio(
	itemID string,
	priority playback.Priority,
	pcm []int16,
	interruptible bool,
	onPlayed func([]int16),
) error {
	if sink == nil || sink.scheduler == nil {
		return errors.New("audio sink is unavailable")
	}
	if sink.sessionID() == "" {
		return errors.New("audio sink has no active session")
	}
	voiceSession := sink.currentSession()
	if voiceSession == nil {
		return errors.New("audio sink has no active session")
	}
	sink.turnMu.Lock()
	defer sink.turnMu.Unlock()
	// A barge-in drops the session back to listening while a canceled turn may
	// still be producing frames. Refusing non-listening conversation audio keeps
	// that stale reply from resuming behind the child's new speech.
	if priority == playback.PriorityConversation {
		switch voiceSession.State() {
		case session.StateThinking, session.StateSpeaking:
		default:
			return errors.New("audio sink session is not producing a reply")
		}
	}

	item := playback.Item{
		ItemID:        itemID,
		Priority:      priority,
		Payload:       pcmBytes(pcm),
		Interruptible: interruptible,
		OnPlayed:      onPlayed,
	}
	if priority == playback.PriorityConversation {
		// Emit speaking before the first reply frame is schedulable so the wire
		// order is always session_state=speaking followed by reply audio.
		sink.markSpeaking(voiceSession)
	}
	// Pin the reply frame to the current turn. A barge-in bumps the epoch while
	// holding turnMu, so SendAudio can drop any frame whose turn was canceled in
	// the window between scheduler dequeue and socket write.
	item.TurnEpoch = sink.turnEpoch.Load()
	if err := sink.scheduler.Enqueue(item); err != nil {
		return err
	}
	return nil
}

// SendAudio frames one encoded Opus payload for the device.
//
// turnEpoch is the reply turn that produced the frame. The barge-in path
// advances sink.turnEpoch while holding turnMu, so a frame that was dequeued
// just before the barge-in is dropped here instead of being written after the
// child has already started a new utterance.
func (sink *connectionAudioSink) SendAudio(payload []byte, turnEpoch uint64) error {
	if sink == nil || sink.writer == nil {
		return errors.New("audio sink is unavailable")
	}
	sink.turnMu.Lock()
	stale := turnEpoch != 0 && turnEpoch != sink.turnEpoch.Load()
	sink.turnMu.Unlock()
	if stale {
		return nil
	}
	sessionID := sink.sessionID()
	if sessionID == "" {
		return errors.New("audio sink has no active session")
	}
	encoded, err := EncodeServerAudioEnvelope(sessionID, sink.sequence.Add(1)-1, payload)
	if err != nil {
		return err
	}
	return sink.writer.enqueue(outboundMessage{
		messageType: websocket.BinaryMessage,
		payload:     encoded,
	})
}

func (sink *connectionAudioSink) currentSession() *session.Session {
	if sink == nil || sink.session == nil {
		return nil
	}
	return sink.session()
}

func (sink *connectionAudioSink) markSpeaking(voiceSession *session.Session) {
	if sink == nil || voiceSession == nil {
		return
	}
	if voiceSession.State() != session.StateThinking {
		return
	}
	if transitionErr := voiceSession.Transition(session.EventStartSpeaking); transitionErr != nil {
		return
	}
	sink.notifyState(voiceSession, string(session.StateSpeaking))
}

// beginTurn marks the session thinking before the conversation handler runs.
func (sink *connectionAudioSink) beginTurn(segment session.AudioSegment) {
	if sink == nil || sink.onTurnStart == nil {
		return
	}
	sink.onTurnStart(segment)
}

// completeTurn re-arms the session for the next utterance once the handler has
// enqueued the whole reply. A barge-in or session end already moved the state,
// so the callback is a no-op in those cases.
func (sink *connectionAudioSink) completeTurn(segment session.AudioSegment) {
	if sink == nil || sink.onTurnEnd == nil {
		return
	}
	sink.onTurnEnd(segment)
}

func (s *Server) sendError(
	writer *connectionWriter,
	voiceSession *session.Session,
	code string,
	message string,
	retryable bool,
) {
	sessionID := ""
	deviceID := ""
	if voiceSession != nil {
		sessionID = voiceSession.ID()
		deviceID = voiceSession.DeviceID()
	}
	if err := writer.enqueueText(NewErrorFrame(sessionID, deviceID, code, message, retryable)); err != nil {
		s.stats.backpressureDrops.Add(1)
		if s.config.OnBackpressure != nil && deviceID != "" {
			s.config.OnBackpressure(deviceID, s.stats.backpressureDrops.Load())
		}
	}
}

type connectionWriter struct {
	connection   *websocket.Conn
	sendQueue    chan outboundMessage
	writeTimeout time.Duration
	closeOnce    sync.Once
	closed       chan struct{}
}

type outboundMessage struct {
	messageType int
	payload     []byte
}

func newConnectionWriter(connection *websocket.Conn, queueDepth int, writeTimeout time.Duration) *connectionWriter {
	if queueDepth < 1 {
		queueDepth = defaultWriteQueueDepth
	}
	return &connectionWriter{
		connection:   connection,
		sendQueue:    make(chan outboundMessage, queueDepth),
		writeTimeout: writeTimeout,
		closed:       make(chan struct{}),
	}
}

func (writer *connectionWriter) start() {
	go writer.writeLoop()
}

func (writer *connectionWriter) enqueueText(control ControlFrame) error {
	encoded, err := control.Encode()
	if err != nil {
		return err
	}
	return writer.enqueue(outboundMessage{messageType: websocket.TextMessage, payload: encoded})
}

func (writer *connectionWriter) enqueueControl(messageType int, payload []byte) error {
	return writer.enqueue(outboundMessage{messageType: messageType, payload: payload})
}

func (writer *connectionWriter) enqueue(message outboundMessage) error {
	select {
	case <-writer.closed:
		return errors.New("websocket writer is closed")
	case writer.sendQueue <- message:
		return nil
	default:
		return errors.New("websocket send queue is full")
	}
}

func (writer *connectionWriter) Close() error {
	var closeErr error
	writer.closeOnce.Do(func() {
		close(writer.closed)
		// Closing the socket first releases a write goroutine that may be
		// blocked in WriteMessage; the final close control is best-effort.
		closeErr = writer.connection.Close()
	})
	return closeErr
}

func (writer *connectionWriter) writeLoop() {
	for {
		select {
		case <-writer.closed:
			return
		case message := <-writer.sendQueue:
			if err := writer.write(message); err != nil {
				return
			}
		}
	}
}

func (writer *connectionWriter) write(message outboundMessage) error {
	if message.messageType == websocket.PingMessage {
		return writer.connection.WriteControl(
			websocket.PingMessage,
			message.payload,
			time.Now().Add(writer.writeTimeout),
		)
	}
	if err := writer.connection.SetWriteDeadline(time.Now().Add(writer.writeTimeout)); err != nil {
		return err
	}
	return writer.connection.WriteMessage(message.messageType, message.payload)
}

// rateLimiter enforces a token bucket over control frames, audio frames, and
// bytes. It is owned by one connection read loop and therefore needs no lock.
type rateLimiter struct {
	maxFramesPerSecond int
	maxBytesPerSecond  int
	windowStartedAt    time.Time
	frames             int
	bytes              int
}

func newRateLimiter(maxFramesPerSecond int, maxBytesPerSecond int) *rateLimiter {
	return &rateLimiter{
		maxFramesPerSecond: maxFramesPerSecond,
		maxBytesPerSecond:  maxBytesPerSecond,
		windowStartedAt:    time.Now(),
	}
}

func (limiter *rateLimiter) allowControl() bool {
	return limiter.allow(0)
}

func (limiter *rateLimiter) allowAudio(bytes int) bool {
	return limiter.allow(bytes)
}

func (limiter *rateLimiter) allow(bytes int) bool {
	now := time.Now()
	if now.Sub(limiter.windowStartedAt) >= time.Second {
		limiter.windowStartedAt = now
		limiter.frames = 0
		limiter.bytes = 0
	}
	if limiter.frames >= limiter.maxFramesPerSecond {
		return false
	}
	if limiter.bytes+bytes > limiter.maxBytesPerSecond {
		return false
	}
	limiter.frames++
	limiter.bytes += bytes
	return true
}

func normalizeServerConfig(config ServerConfig) ServerConfig {
	if config.ReadLimitBytes <= 0 {
		config.ReadLimitBytes = defaultReadLimitBytes
	}
	if config.WriteQueueDepth <= 0 {
		config.WriteQueueDepth = defaultWriteQueueDepth
	}
	if config.SegmentQueueDepth <= 0 {
		config.SegmentQueueDepth = defaultSegmentQueueDepth
	}
	if config.PongWait <= 0 {
		config.PongWait = defaultPongWait
	}
	if config.PingInterval <= 0 || config.PingInterval >= config.PongWait {
		config.PingInterval = defaultPingInterval
	}
	if config.WriteTimeout <= 0 {
		config.WriteTimeout = defaultWriteTimeout
	}
	if config.MaxFramesPerSecond <= 0 {
		config.MaxFramesPerSecond = defaultMaxFramesPerSecond
	}
	if config.MaxBytesPerSecond <= 0 {
		config.MaxBytesPerSecond = defaultMaxBytesPerSecond
	}
	if config.SessionTTL <= 0 {
		config.SessionTTL = defaultSessionTTL
	}
	return config
}

func mapSessionError(err error) string {
	switch {
	case errors.Is(err, session.ErrSessionLimitReached),
		errors.Is(err, session.ErrDeviceSessionLimit):
		return errorCodeSessionLimit
	case errors.Is(err, session.ErrSessionNotFound):
		return errorCodeSessionNotFound
	case errors.Is(err, session.ErrSessionClosed),
		errors.Is(err, session.ErrSessionIdleTimeout),
		errors.Is(err, session.ErrSessionDurationLimit):
		return errorCodeSessionExpired
	case errors.Is(err, session.ErrOutOfOrderFrame),
		errors.Is(err, session.ErrSessionIdentity),
		errors.Is(err, session.ErrSegmentQueueFull):
		return errorCodeInvalidAudio
	default:
		return errorCodeInternal
	}
}

func mapAudioEnvelopeError(err error) string {
	if errors.Is(err, ErrAudioEnvelopePayload) || errors.Is(err, frame.ErrPayloadTooLarge) {
		return errorCodeFrameTooLarge
	}
	return errorCodeInvalidAudio
}

func pcmBytes(pcm []int16) []byte {
	data := make([]byte, len(pcm)*2)
	for index, sample := range pcm {
		value := uint16(sample)
		data[index*2] = byte(value)
		data[index*2+1] = byte(value >> 8)
	}
	return data
}

func writeJSON(response http.ResponseWriter, control ControlFrame) error {
	encoded, err := control.Encode()
	if err != nil {
		return err
	}
	_, err = response.Write(encoded)
	return err
}
