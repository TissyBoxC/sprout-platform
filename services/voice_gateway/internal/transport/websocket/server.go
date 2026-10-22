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
	"strings"
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
	defaultVolumePercent    = 100
	defaultMaxVolumePercent = 100
	defaultPongWait         = 60 * time.Second
	defaultPingInterval     = 25 * time.Second
	// defaultAuthRecheckInterval matches the platform settings cache TTL so a
	// guardian's withdrawal is observed within one short window on an open
	// connection.
	defaultAuthRecheckInterval = 30 * time.Second
	defaultWriteTimeout        = 10 * time.Second
	defaultHandshakeTimeout    = 10 * time.Second
	defaultSessionTTL          = 15 * time.Minute
	defaultContinuityWindow    = 60 * time.Second
	defaultMaxFramesPerSecond  = 80
	defaultMaxBytesPerSecond   = 128 * 1024
	defaultSendQueueDropPolicy = SendQueueDisconnect
)

// ServerConfig bounds one device connection and the sessions it may create.
//
// Limiting both frames and bytes is intentional: a small number of maximum
// sized frames must not consume the same budget as normal 20 ms Opus frames.
type ServerConfig struct {
	ReadLimitBytes    int64
	WriteQueueDepth   int
	SegmentQueueDepth int
	PongWait          time.Duration
	PingInterval      time.Duration
	// AuthRecheckInterval re-checks the guardian's live consent during a
	// long-lived connection so a withdrawal mid-session ends the audio path.
	// Zero uses defaultAuthRecheckInterval.
	AuthRecheckInterval time.Duration
	WriteTimeout        time.Duration
	MaxFramesPerSecond  int
	MaxBytesPerSecond   int
	SessionTTL          time.Duration
	// ContinuityWindow keeps a finished session recoverable for follow-up turns
	// without a new wake word. The session is released when the window expires,
	// the connection closes, or a new session replaces it.
	ContinuityWindow time.Duration
	Manager          *session.Manager
	Verifier         TokenVerifier
	// Authorizer re-checks the guardian's platform state before the upgrade so
	// a withdrawn consent or disabled account blocks new voice sessions. It is
	// optional for tests and legacy deployments; production enables it whenever
	// the platform database is configured.
	Authorizer      DeviceAuthorizer
	DetectorFactory func() vad.Detector
	// PreprocessFactory builds the per-session echo cancellation, noise
	// suppression, and gain calibration chain. Optional; nil keeps the plain
	// codec and VAD path.
	PreprocessFactory func() (preprocess.Processor, error)
	// ReferenceAudio returns the most recent speaker playback for one device so
	// echo cancellation can subtract it from that device's microphone signal.
	// A nil provider or nil result means speaker output is silent.
	ReferenceAudio func(deviceID string) []int16
	// OnDeviceDisconnected releases per-device resources that must not outlive
	// the connection, such as the retained speaker reference audio. It is
	// called once per accepted connection after close, before the token is
	// revoked. A nil value releases nothing.
	OnDeviceDisconnected func(deviceID string)
	OnSessionClosed      func(sessionID string, cause error)
	// OnContinuityExpired reports the session identifier when the idle grace
	// window elapses and the session resources are released.
	OnContinuityExpired func(sessionID string)
	OnSegmentReady      func(segment session.AudioSegment)
	OnBackpressure      func(deviceID string, dropped uint64)
	// AllowedOrigins is the exact Origin allowlist for browser WebSocket
	// clients. Native clients without an Origin header remain supported.
	AllowedOrigins []string
	// TokenRevoker extends the authenticated token's shared revocation record
	// when this connection closes. Production wires the HMAC verifier backed by
	// Redis, so the record is visible to every gateway replica. The verification
	// path also consumes the token atomically before upgrade, making reconnect
	// replay fail even while another replica owns the live connection.
	TokenRevoker func(identity DeviceIdentity)
	// SegmentHandler, when set, owns the conversation pipeline. It receives each
	// completed utterance plus a sink that streams synthesized reply audio back
	// to the device over this connection. Invocations for one connection are
	// serialised, so the handler may keep per-session state.
	SegmentHandler func(ctx context.Context, segment session.AudioSegment, sink AudioSink)
	// onPlaybackScheduler observes the per-connection scheduler once it is
	// created. Test-only seam; production leaves it nil.
	onPlaybackScheduler func(*playback.Scheduler)
	// OnDeviceConnected registers the live connection of an authenticated
	// device so another transport (the guardian intercom relay) can write
	// frames to it. It is called once after the upgrade and its returned
	// release function is called once when the connection ends. A nil value
	// keeps other transports from writing to devices.
	OnDeviceConnected func(deviceID string, connection DeviceFrameSink) (release func())
}

// DeviceFrameSink accepts a binary frame write on behalf of another transport.
//
// The realtime server owns the single writer goroutine per connection, so the
// sink serializes with control frames and reply audio instead of writing to the
// socket directly. Implementations must be safe for concurrent use.
type DeviceFrameSink interface {
	SendDeviceFrame(payload []byte) error
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
			CheckOrigin:      originChecker(config.AllowedOrigins),
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
		if err := authorizeDevice(
			request.Context(),
			s.config.Authorizer,
			identity.DeviceID,
		); err != nil {
			s.stats.rejectedConnections.Add(1)
			response.Header().Set("Content-Type", "application/json; charset=utf-8")
			response.WriteHeader(http.StatusForbidden)
			_ = writeJSON(response, NewErrorFrame("", "", errorCodeUnauthenticated, "语音服务当前不可用，请让家长重新确认授权", false))
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
	connectionContext, cancelContext := context.WithCancelCause(context.Background())
	var cancelOnce sync.Once
	cancel := func(cause error) {
		cancelOnce.Do(func() {
			cancelContext(cause)
		})
	}
	defer cancel(errors.New("websocket connection closed"))
	defer connection.Close()
	defer func() {
		if s.config.OnDeviceDisconnected != nil {
			s.config.OnDeviceDisconnected(identity.DeviceID)
		}
		if s.config.TokenRevoker != nil {
			s.config.TokenRevoker(identity)
		}
	}()
	connection.SetReadLimit(s.config.ReadLimitBytes)
	_ = connection.SetReadDeadline(time.Now().Add(s.config.PongWait))
	connection.SetPongHandler(func(string) error {
		return connection.SetReadDeadline(time.Now().Add(s.config.PongWait))
	})

	writer := newConnectionWriter(connection, s.config.WriteQueueDepth, s.config.WriteTimeout)
	defer writer.Close()
	if s.config.OnDeviceConnected != nil {
		// Register before the reader loop so an intercom relay that starts at
		// the same instant as the device connection still finds the sink.
		// Release runs last so no relay frame can be queued to a closed
		// writer after the device is gone.
		if release := s.config.OnDeviceConnected(identity.DeviceID, writer); release != nil {
			defer release()
		}
	}

	var currentSession *session.Session
	var continuityGeneration atomic.Uint64
	var continuityTimer *time.Timer
	var continuityKeepAliveStop chan struct{}
	var sessionMu sync.Mutex
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
	stopContinuityTimer := func() {
		sessionMu.Lock()
		continuityGeneration.Add(1)
		if continuityTimer != nil {
			continuityTimer.Stop()
			continuityTimer = nil
		}
		if continuityKeepAliveStop != nil {
			close(continuityKeepAliveStop)
			continuityKeepAliveStop = nil
		}
		sessionMu.Unlock()
	}
	startContinuityTimer := func(voiceSession *session.Session) {
		if voiceSession == nil || s.config.ContinuityWindow <= 0 {
			return
		}
		generation := continuityGeneration.Add(1)
		sessionMu.Lock()
		if continuityTimer != nil {
			continuityTimer.Stop()
		}
		if continuityKeepAliveStop != nil {
			close(continuityKeepAliveStop)
		}
		keepAliveStop := make(chan struct{})
		continuityKeepAliveStop = keepAliveStop
		go func() {
			ticker := time.NewTicker(time.Second)
			defer ticker.Stop()
			for {
				select {
				case <-keepAliveStop:
					return
				case <-ticker.C:
					voiceSession.KeepAlive()
				}
			}
		}()
		continuityTimer = time.AfterFunc(s.config.ContinuityWindow, func() {
			if continuityGeneration.Load() != generation {
				return
			}
			active := getSession()
			if active == nil || active.ID() != voiceSession.ID() {
				return
			}
			sessionMu.Lock()
			if continuityGeneration.Load() != generation {
				sessionMu.Unlock()
				return
			}
			continuityTimer = nil
			sessionMu.Unlock()
			stopContinuityTimer()
			setSession(nil)
			s.config.Manager.Remove(voiceSession.ID())
			if s.config.OnContinuityExpired != nil {
				s.config.OnContinuityExpired(voiceSession.ID())
			}
		})
		sessionMu.Unlock()
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
			ContentCategory:   control.ContentCategory,
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
			FrameObserver: func(observation session.FrameObservation) {
				sink.observeAcceptedFrame(getSession(), observation)
			},
		})
	}
	attachSession := func(voiceSession *session.Session) {
		stopContinuityTimer()
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
	sink.onAcceptedFrame = func(voiceSession *session.Session, observation session.FrameObservation) {
		if !observation.Speech {
			return
		}
		if getSession() != voiceSession || voiceSession.State() != session.StateSpeaking {
			return
		}
		scheduleBargeIn(voiceSession)
	}
	go s.consumeSegments(connectionContext, segmentQueue, segmentDone, sink)
	heartbeatDone := make(chan struct{})
	go s.sendHeartbeats(connectionContext, writer, heartbeatDone)

	// A withdrawal of the guardian's consent must end an already-open
	// connection, not only gate new upgrades. The watcher re-checks the
	// authoritative platform state on an interval; on failure it sends a safe
	// close reason and closes the socket, which unblocks the reader below.
	var consentRevoked atomic.Bool
	revokeForConsent := func() {
		if consentRevoked.Swap(true) {
			return
		}
		// WriteControl is safe to call concurrently with the writer goroutine,
		// so the safe close reason is flushed synchronously and cannot race a
		// queued error frame. Bumping the read deadline unblocks the reader.
		_ = connection.WriteControl(
			websocket.CloseMessage,
			websocket.FormatCloseMessage(websocket.ClosePolicyViolation, "consent_revoked"),
			time.Now().Add(s.config.WriteTimeout),
		)
		_ = connection.SetReadDeadline(time.Now())
	}
	if s.config.Authorizer != nil {
		go s.watchAuthorization(connectionContext, identity.DeviceID, revokeForConsent)
	}
	// Starting or resuming an utterance re-checks consent synchronously so a
	// withdrawal that landed between interval ticks is observed before any new
	// audio is accepted.
	reauthorize := func() bool {
		if s.config.Authorizer == nil {
			return true
		}
		if err := authorizeDevice(connectionContext, s.config.Authorizer, identity.DeviceID); err != nil {
			revokeForConsent()
			return false
		}
		return true
	}

	limiter := newRateLimiter(s.config.MaxFramesPerSecond, s.config.MaxBytesPerSecond)
	var disconnectReason string

	for {
		messageType, payload, err := connection.ReadMessage()
		if err != nil {
			switch {
			case consentRevoked.Load():
				disconnectReason = "consent_revoked"
			case websocket.IsCloseError(err, websocket.CloseNormalClosure, websocket.CloseGoingAway):
				disconnectReason = "client_closed"
			default:
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
				if !reauthorize() {
					disconnectReason = "consent_revoked"
					goto closed
				}
				stopContinuityTimer()
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
				if !reauthorize() {
					disconnectReason = "consent_revoked"
					goto closed
				}
				stopContinuityTimer()
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
			case controlTypeBargeIn:
				voiceSession := getSession()
				if voiceSession == nil {
					s.sendError(writer, nil, errorCodeSessionNotFound, "当前没有进行中的收听", false)
					continue
				}
				if voiceSession.State() != session.StateSpeaking {
					// Repeated or out-of-order barge-in is idempotent: the
					// current state is already safe, so acknowledge nothing and
					// keep the connection usable.
					continue
				}
				scheduleBargeIn(voiceSession)
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
				if control.Type == controlTypeSessionEnd {
					// Keep the conversation alive for the configured continuity
					// window. The next audio or wake frame reuses the same
					// session; the timer releases it if no follow-up arrives.
					if voiceSession.State() != session.StateIdle {
						if transitionErr := voiceSession.Transition(session.EventSuspend); transitionErr != nil {
							s.sendError(writer, voiceSession, errorCodeInternal, "当前无法结束监听，请稍后重试", true)
							continue
						}
						sink.notifyState(voiceSession, string(session.StateIdle))
					}
					sink.turnMu.Lock()
					sink.turnEpoch.Add(1)
					scheduler.Clear(false)
					sink.turnMu.Unlock()
					startContinuityTimer(voiceSession)
					continue
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
			case controlTypeQualityMetrics:
				s.stats.malformedFrames.Add(1)
				s.sendError(writer, getSession(), errorCodeInvalidControl, "控制信息方向不正确", false)
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
			stopContinuityTimer()
			if voiceSession.State() == session.StateIdle {
				if transitionErr := voiceSession.Transition(session.EventStartListening); transitionErr != nil {
					s.sendError(writer, voiceSession, errorCodeInternal, "当前无法开始监听，请稍后重试", true)
					continue
				}
				sink.notifyState(voiceSession, string(session.StateListening))
			}
			_, acceptErr := voiceSession.AcceptFrameObserved(connectionContext, frame.New(
				fmt.Sprintf("%s_%d", voiceSession.ID(), envelope.Sequence),
				identity.DeviceID,
				voiceSession.StreamID(),
				envelope.Sequence,
				envelope.CapturedAt,
				envelope.Payload,
			))
			if acceptErr != nil {
				errorCode := mapSessionError(acceptErr)
				if errors.Is(acceptErr, buffer.ErrDuplicateFrame) {
					// A reconnect can replay the last packet. It is already in
					// the stream window, so acknowledge nothing and keep the
					// session alive instead of disconnecting healthy audio.
					continue
				}
				s.stats.malformedFrames.Add(1)
				s.sendError(writer, voiceSession, errorCode, "音频数据未受理", false)
				if errors.Is(acceptErr, session.ErrSessionClosed) ||
					errors.Is(acceptErr, session.ErrSessionIdleTimeout) ||
					errors.Is(acceptErr, session.ErrSessionDurationLimit) {
					// The device is still connected, so tell it the session fell
					// idle before the reader tears the connection down.
					sink.notifyState(voiceSession, string(session.StateIdle))
					disconnectReason = "session_ended"
					goto closed
				}
				continue
			}
			// Acoustic barge-in is handled once by the session FrameObserver
			// installed above, after decode, preprocessing, and VAD. Keeping a
			// second direct call here would schedule the same interruption
			// twice when a speech frame arrives while the reply is speaking.
		default:
			s.stats.malformedFrames.Add(1)
			s.sendError(writer, getSession(), errorCodeInvalidControl, "不支持的数据类型", false)
		}
	}

closed:
	_ = writer.Close()
	close(heartbeatDone)
	close(segmentDone)
	// Stop provider work and the segment consumer before releasing the
	// connection-scoped session and playback resources.
	cancel(errors.New("websocket connection closed"))
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
		case segment, ok := <-segments:
			if !ok {
				return
			}
			if s.config.SegmentHandler != nil {
				observer, _ := sink.(turnObserver)
				if observer != nil {
					observer.beginTurn(segment)
				}
				handlerDone := make(chan struct{})
				go func(segment session.AudioSegment) {
					defer close(handlerDone)
					s.config.SegmentHandler(ctx, segment, sink)
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

// watchAuthorization re-checks the guardian's live consent on an interval and
// reports the first withdrawal so an open connection stops receiving audio.
//
// The check runs against the platform database, not cached token state, so a
// consent revoked in the parent application ends the session without waiting
// for the signed token to expire. A transient read failure also fails closed:
// the connection closes and the device can reconnect once the guardian's state
// is confirmed again.
func (s *Server) watchAuthorization(
	ctx context.Context,
	deviceID string,
	onRevoked func(),
) {
	interval := s.config.AuthRecheckInterval
	if interval <= 0 {
		interval = defaultAuthRecheckInterval
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			checkCtx, cancel := context.WithTimeout(ctx, s.config.WriteTimeout)
			err := authorizeDevice(checkCtx, s.config.Authorizer, deviceID)
			cancel()
			if err != nil {
				s.logger.Info("device authorization revoked mid-session", "device_id", deviceID)
				onRevoked()
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
	writer          *connectionWriter
	scheduler       *playback.Scheduler
	sessionID       func() string
	session         func() *session.Session
	onAcceptedFrame func(*session.Session, session.FrameObservation)
	notifyState     func(*session.Session, string)
	onTurnStart     func(session.AudioSegment)
	onTurnEnd       func(session.AudioSegment)
	sequence        atomic.Uint32
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

func (sink *connectionAudioSink) observeAcceptedFrame(
	voiceSession *session.Session,
	observation session.FrameObservation,
) {
	if sink == nil || sink.onAcceptedFrame == nil {
		return
	}
	sink.onAcceptedFrame(voiceSession, observation)
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

// CurrentTurnEpoch exposes the barge-in generation to the playback scheduler.
func (sink *connectionAudioSink) CurrentTurnEpoch() uint64 {
	if sink == nil {
		return 0
	}
	return sink.turnEpoch.Load()
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

// SendDeviceFrame queues one binary frame produced by another transport.
//
// The payload is queued through the same bounded channel as reply audio, so a
// relay cannot outrun the device's read loop and the send-queue policy stays
// identical for every writer.
func (writer *connectionWriter) SendDeviceFrame(payload []byte) error {
	if len(payload) == 0 {
		return errors.New("device frame is empty")
	}
	frame := append([]byte(nil), payload...)
	return writer.enqueue(outboundMessage{messageType: websocket.BinaryMessage, payload: frame})
}

var _ DeviceFrameSink = (*connectionWriter)(nil)

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

// rateLimiter enforces independent token buckets over request count and bytes.
// It is owned by one connection read loop and therefore needs no lock.
type rateLimiter struct {
	maxFramesPerSecond int
	maxBytesPerSecond  int
	now                func() time.Time
	lastRefill         time.Time
	frameTokens        float64
	byteTokens         float64
}

func newRateLimiter(maxFramesPerSecond int, maxBytesPerSecond int) *rateLimiter {
	return newRateLimiterWithClock(maxFramesPerSecond, maxBytesPerSecond, time.Now)
}

func newRateLimiterWithClock(
	maxFramesPerSecond int,
	maxBytesPerSecond int,
	now func() time.Time,
) *rateLimiter {
	if now == nil {
		now = time.Now
	}
	current := now()
	return &rateLimiter{
		maxFramesPerSecond: maxFramesPerSecond,
		maxBytesPerSecond:  maxBytesPerSecond,
		now:                now,
		lastRefill:         current,
		frameTokens:        float64(maxFramesPerSecond),
		byteTokens:         float64(maxBytesPerSecond),
	}
}

func (limiter *rateLimiter) allowControl() bool {
	return limiter.allow(0)
}

func (limiter *rateLimiter) allowAudio(bytes int) bool {
	return limiter.allow(bytes)
}

func (limiter *rateLimiter) allow(bytes int) bool {
	if limiter == nil || limiter.maxFramesPerSecond <= 0 || limiter.maxBytesPerSecond <= 0 {
		return false
	}
	now := limiter.now()
	if now.Before(limiter.lastRefill) {
		// A wall-clock rollback must not mint an unbounded new burst.
		limiter.lastRefill = now
	}
	elapsed := now.Sub(limiter.lastRefill).Seconds()
	limiter.lastRefill = now
	limiter.frameTokens = minFloat(
		float64(limiter.maxFramesPerSecond),
		limiter.frameTokens+elapsed*float64(limiter.maxFramesPerSecond),
	)
	limiter.byteTokens = minFloat(
		float64(limiter.maxBytesPerSecond),
		limiter.byteTokens+elapsed*float64(limiter.maxBytesPerSecond),
	)
	if limiter.frameTokens < 1 || limiter.byteTokens < float64(bytes) {
		return false
	}
	limiter.frameTokens--
	limiter.byteTokens -= float64(bytes)
	return true
}

func minFloat(left float64, right float64) float64 {
	if left < right {
		return left
	}
	return right
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
	if config.AuthRecheckInterval <= 0 {
		config.AuthRecheckInterval = defaultAuthRecheckInterval
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
	if config.ContinuityWindow <= 0 {
		config.ContinuityWindow = defaultContinuityWindow
	}
	return config
}

func originChecker(allowedOrigins []string) func(*http.Request) bool {
	allowed := make(map[string]struct{}, len(allowedOrigins))
	for _, origin := range allowedOrigins {
		trimmed := strings.TrimSuffix(strings.TrimSpace(origin), "/")
		if trimmed == "" {
			continue
		}
		allowed[trimmed] = struct{}{}
	}
	return func(request *http.Request) bool {
		// Device firmware and native applications do not send Origin. They are
		// authenticated with a bearer device token before the upgrade.
		origin := strings.TrimSuffix(strings.TrimSpace(request.Header.Get("Origin")), "/")
		if origin == "" {
			return true
		}
		if len(allowed) == 0 {
			return false
		}
		_, ok := allowed[origin]
		return ok
	}
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
		errors.Is(err, session.ErrSessionDurationLimit),
		errors.Is(err, session.ErrSessionWindowExpired):
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
