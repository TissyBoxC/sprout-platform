package careview

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"
)

// Relay is one open care-view stream between a guardian and a device.
//
// Ownership: the manager creates it, the guardian WebSocket handler drives its
// read loop, and Close may be called from any goroutine (the deadline timer,
// the platform state watcher, the device path, or shutdown). Close runs its
// side effects exactly once, records the first reason, and releases the
// device-bound queue.
//
// The relay never stores what it forwards. It keeps byte counts only.
type Relay struct {
	session Session
	reason  CloseReason
	opened  time.Time
	logger  *slog.Logger
	now     func() time.Time

	// maxFrameBytes is the hard per-frame ceiling the relay re-checks even
	// after the connector validated the frame.
	maxFrameBytes int

	outbound OutboundSink

	// stateLock guards the last platform state this relay observed. The
	// guardian handler refreshes it after each re-read; the state watcher
	// closes the relay when the platform stops reporting streaming.
	stateLock sync.RWMutex
	state     State

	guardianLock   sync.Mutex
	guardianActive bool
	guardian       func(payload []byte) error

	// deadlineReason names the limit that ExpiresAt came from, so the close log
	// distinguishes the gateway cap from an earlier platform deadline.
	deadlineReason CloseReason

	cancel context.CancelCauseFunc
	// doneContext is cancelled by Close; it is the only context the guardian
	// handler waits on, so a close unblocks readers and timers at once.
	doneContext context.Context
	done        chan struct{}
	closeOnce   sync.Once
	closeReason atomic.Value
	// outboundQueue is bounded so a device that stops reading applies
	// backpressure to the guardian instead of growing gateway memory.
	outboundQueue chan []byte
	writerDone    chan struct{}

	framesIn  atomic.Uint64
	framesOut atomic.Uint64
	dropped   atomic.Uint64
	bytesIn   atomic.Uint64
}

// newRelay builds the relay and starts its device writer.
//
// ctx is already cancelled by the caller when it wants the relay gone; the
// derived context inherits that, so a relay never outlives its parent.
func newRelay(
	ctx context.Context,
	session Session,
	outbound OutboundSink,
	maxFrameBytes int,
	writeQueueDepth int,
	logger *slog.Logger,
	now func() time.Time,
) *Relay {
	if writeQueueDepth <= 0 {
		writeQueueDepth = defaultWriteQueueDepth
	}
	if logger == nil {
		logger = slog.Default()
	}
	if now == nil {
		now = time.Now
	}
	relayContext, cancel := context.WithCancelCause(ctx)
	relay := &Relay{
		session:        session,
		reason:         CloseReasonTransportClosed,
		logger:         logger,
		now:            now,
		maxFrameBytes:  NormalizeMaxFrameBytes(maxFrameBytes),
		outbound:       outbound,
		state:          session.State,
		deadlineReason: CloseReasonMaxDuration,
		cancel:         cancel,
		doneContext:    relayContext,
		done:           make(chan struct{}),
		outboundQueue:  make(chan []byte, writeQueueDepth),
		writerDone:     make(chan struct{}),
	}
	go relay.writeLoop(relayContext)
	return relay
}

// Session returns the authorized view this relay carries.
func (relay *Relay) Session() Session {
	if relay == nil {
		return Session{}
	}
	return relay.session
}

// Context is cancelled when the relay closes for any reason.
func (relay *Relay) Context() context.Context {
	if relay == nil {
		return context.Background()
	}
	return relay.doneContext
}

// Done is closed once the relay has closed.
func (relay *Relay) Done() <-chan struct{} {
	if relay == nil {
		closed := make(chan struct{})
		close(closed)
		return closed
	}
	return relay.done
}

// IsClosed reports whether the relay has ended.
func (relay *Relay) IsClosed() bool {
	if relay == nil {
		return true
	}
	select {
	case <-relay.done:
		return true
	default:
		return false
	}
}

// ObserveState records a freshly read platform state and refuses it when it no
// longer allows frames. It returns ErrNotStreaming so the caller can close the
// relay with a specific reason instead of guessing.
func (relay *Relay) ObserveState(state State) error {
	if relay == nil {
		return ErrRelayUnavailable
	}
	relay.stateLock.Lock()
	relay.state = state
	relay.stateLock.Unlock()
	if !state.AllowsFrames() {
		// Record the reason immediately: the close may be observed by the
		// handler's socket teardown before the caller reaches Close, and the
		// processing gate (IsStreaming) must already read false.
		relay.Close(CloseReasonSessionEnded)
		return ErrNotStreaming
	}
	return nil
}

// CurrentState returns the last platform state this relay observed.
func (relay *Relay) CurrentState() State {
	if relay == nil {
		return State("")
	}
	relay.stateLock.RLock()
	defer relay.stateLock.RUnlock()
	return relay.state
}

// IsStreaming reports whether this relay may still carry frames.
//
// It is the processing-layer gate: the resolver already refused a non-streaming
// session, and every frame path re-checks the same rule here before forwarding,
// so an implementation bug above cannot turn into a covert camera.
func (relay *Relay) IsStreaming() bool {
	return relay != nil && relay.CurrentState().AllowsFrames()
}

// AttachGuardian claims the single guardian side of this relay.
//
// Exactly one guardian socket may carry a view; a second attach returns
// ErrSessionAlreadyRelayed so a second parent can never observe the same
// camera feed, and a reconnect cannot duplicate it.
func (relay *Relay) AttachGuardian() error {
	if relay == nil {
		return ErrRelayUnavailable
	}
	relay.guardianLock.Lock()
	defer relay.guardianLock.Unlock()
	if relay.guardianActive {
		return ErrSessionAlreadyRelayed
	}
	relay.guardianActive = true
	return nil
}

// SetGuardianWriter installs the guardian frame writer used for device frames.
// It is called by the guardian handler once its socket is upgraded.
func (relay *Relay) SetGuardianWriter(writer func(payload []byte) error) {
	if relay == nil {
		return
	}
	relay.guardianLock.Lock()
	relay.guardian = writer
	relay.guardianLock.Unlock()
}

// ForwardToGuardian hands one device frame to the guardian side.
//
// No guardian is attached during the short window between the device side
// lighting up and the guardian upgrade, so a frame in that window is counted
// and dropped rather than buffered: a late picture is useless, and buffering it
// would grow memory for a view nobody is watching yet.
func (relay *Relay) ForwardToGuardian(payload []byte) error {
	if relay == nil || len(payload) == 0 {
		return nil
	}
	relay.guardianLock.Lock()
	writer := relay.guardian
	relay.guardianLock.Unlock()
	if writer == nil {
		relay.dropped.Add(1)
		return nil
	}
	if err := writer(payload); err != nil {
		relay.Close(CloseReasonTransportClosed)
		return err
	}
	relay.framesOut.Add(1)
	return nil
}

// DeadlineReason names the deadline that is about to close this relay.
//
// The gateway reports which limit fired so operations can tell a five-minute
// policy cap from a platform deadline without guessing from the duration.
func (relay *Relay) DeadlineReason() CloseReason {
	if relay == nil {
		return CloseReasonMaxDuration
	}
	return relay.deadlineReason
}

// ForwardToDevice queues one guardian frame for the device side.
//
// A full queue closes the relay instead of buffering without bound: live view
// frames are only useful in real time, and a silently dropped frame would show
// a guardian a stale picture without telling them.
func (relay *Relay) ForwardToDevice(payload []byte) error {
	if relay == nil {
		return ErrRelayUnavailable
	}
	if len(payload) == 0 {
		return nil
	}
	if relay.outbound == nil {
		return ErrRelayUnavailable
	}
	if len(payload) > relay.maxFrameBytes {
		relay.dropped.Add(1)
		relay.Close(CloseReasonFrameLimit)
		return ErrFrameRejected
	}
	frame := append([]byte(nil), payload...)
	// Check the closed state first: a select with both a ready send and a ready
	// done case picks randomly, which would let a closed relay deliver one last
	// guardian frame after the view ended.
	select {
	case <-relay.done:
		return ErrRelayClosed
	default:
	}
	select {
	case <-relay.done:
		return ErrRelayClosed
	case relay.outboundQueue <- frame:
		relay.framesIn.Add(1)
		relay.bytesIn.Add(uint64(len(frame)))
		return nil
	default:
		relay.dropped.Add(1)
		relay.Close(CloseReasonTransportClosed)
		return errors.New("care view outbound queue is full")
	}
}

// MarkFramesFromDevice counts one device frame after it reached the guardian.
func (relay *Relay) MarkFramesFromDevice() {
	if relay == nil {
		return
	}
	relay.framesOut.Add(1)
}

// Close ends both sides and releases the relay buffers.
//
// The first reason wins; later calls are no-ops. It never blocks on the device
// writer, so a stuck device cannot hold the guardian handler open.
func (relay *Relay) Close(reason CloseReason) {
	if relay == nil {
		return
	}
	relay.closeOnce.Do(func() {
		if reason == "" {
			reason = CloseReasonTransportClosed
		}
		relay.closeReason.Store(reason)
		relay.cancel(errors.New(string(reason)))
		if relay.outbound != nil {
			relay.outbound.Close()
		}
		close(relay.done)
		relay.logClosed()
	})
}

// CloseReason returns the reason recorded for this relay.
func (relay *Relay) CloseReason() CloseReason {
	if relay == nil {
		return CloseReasonTransportClosed
	}
	reason, _ := relay.closeReason.Load().(CloseReason)
	if reason == "" {
		return relay.reason
	}
	return reason
}

// Stats is a bounded, content-free view of one relay for diagnostics.
//
// It deliberately has no field for frame bytes: a care-view frame is a picture
// of a child's room, so the only durable facts are counts, sizes, and time.
type Stats struct {
	SessionID   string
	DeviceID    string
	CloseReason CloseReason
	Duration    time.Duration
	FramesIn    uint64
	FramesOut   uint64
	Dropped     uint64
	BytesIn     uint64
}

// Stats returns this relay's counters.
func (relay *Relay) Stats() Stats {
	if relay == nil {
		return Stats{}
	}
	return Stats{
		SessionID:   relay.session.ID,
		DeviceID:    relay.session.DeviceID,
		CloseReason: relay.CloseReason(),
		Duration:    relay.now().Sub(relay.opened),
		FramesIn:    relay.framesIn.Load(),
		FramesOut:   relay.framesOut.Load(),
		Dropped:     relay.dropped.Load(),
		BytesIn:     relay.bytesIn.Load(),
	}
}

// logClosed records the lifecycle only: identifiers, measured duration, frame
// counters, and the fixed close reason. Frame bytes, device tokens, and
// guardian identifiers never reach this line.
func (relay *Relay) logClosed() {
	duration := relay.now().Sub(relay.opened)
	relay.logger.Info("care view relay closed",
		"session_id", relay.session.ID,
		"device_id", relay.session.DeviceID,
		"duration_ms", duration.Milliseconds(),
		"close_reason", string(relay.CloseReason()),
		"frames_in", relay.framesIn.Load(),
		"frames_out", relay.framesOut.Load(),
		"frames_dropped", relay.dropped.Load(),
		"bytes_in", relay.bytesIn.Load(),
	)
}

func (relay *Relay) writeLoop(ctx context.Context) {
	defer close(relay.writerDone)
	for {
		select {
		case <-ctx.Done():
			return
		case payload := <-relay.outboundQueue:
			if relay.outbound == nil {
				return
			}
			if err := relay.outbound.SendRelayFrame(payload); err != nil {
				// The device side is gone; closing here unblocks the guardian
				// handler through the relay context.
				relay.Close(CloseReasonDeviceHangup)
				return
			}
		}
	}
}

// RelayManager admits relays, enforces the capacity cap, and releases every
// relay it created on shutdown.
//
// Admission is serialized so two guardians (or a reconnect) cannot both hold
// the same view: a second relay for a live session is refused instead of
// duplicating frames, and the per-session guardian attach refuses the second
// socket as well.
type RelayManager struct {
	mutex     sync.Mutex
	closed    bool
	maxRelays int
	relays    map[string]*Relay
	now       func() time.Time
	// logger and writeQueueDepth are recorded at creation so Admit keeps a
	// short signature while every relay still receives the same bounds.
	logger          *slog.Logger
	writeQueueDepth int
}

// NewRelayManager creates a relay admission manager.
func NewRelayManager(maxRelays int) *RelayManager {
	if maxRelays <= 0 {
		maxRelays = defaultMaxRelays
	}
	return &RelayManager{
		maxRelays:       maxRelays,
		relays:          make(map[string]*Relay),
		now:             time.Now,
		logger:          slog.Default(),
		writeQueueDepth: defaultWriteQueueDepth,
	}
}

// RelayAdmission is everything one admitted relay needs beyond its session.
//
// There is no frame-rate field here: the relay never reads from a socket, so
// the rate limit belongs to the guardian read loop, not to the relay.
type RelayAdmission struct {
	Outbound        OutboundSink
	MaxFrameBytes   int
	WriteQueueDepth int
	Logger          *slog.Logger
}

// Admit registers a new relay for an already-authorized session.
//
// The manager never inspects the session state itself; authorization happens
// before this call, and the state rule lives in one place (State.AllowsFrames).
func (manager *RelayManager) Admit(
	ctx context.Context,
	session Session,
	admission RelayAdmission,
) (*Relay, error) {
	if manager == nil {
		return nil, ErrRelayUnavailable
	}
	manager.mutex.Lock()
	defer manager.mutex.Unlock()
	if manager.closed {
		return nil, ErrRelayUnavailable
	}
	if _, exists := manager.relays[session.ID]; exists {
		return nil, ErrSessionAlreadyRelayed
	}
	if len(manager.relays) >= manager.maxRelays {
		return nil, ErrLimitReached
	}
	if ctx == nil {
		ctx = context.Background()
	}
	logger := admission.Logger
	if logger == nil {
		logger = manager.logger
	}
	writeQueueDepth := admission.WriteQueueDepth
	if writeQueueDepth <= 0 {
		writeQueueDepth = manager.writeQueueDepth
	}
	relay := newRelay(
		ctx,
		session,
		admission.Outbound,
		admission.MaxFrameBytes,
		writeQueueDepth,
		logger,
		manager.now,
	)
	relay.opened = manager.now()
	manager.relays[session.ID] = relay
	// The removal watcher must not hold the admission lock, so it runs after
	// the relay is registered but before Admit returns.
	go func() {
		<-relay.Done()
		manager.forget(session.ID)
	}()
	return relay, nil
}

func (manager *RelayManager) forget(sessionID string) {
	if manager == nil {
		return
	}
	manager.mutex.Lock()
	delete(manager.relays, sessionID)
	manager.mutex.Unlock()
}

// Count returns the number of open relays.
func (manager *RelayManager) Count() int {
	if manager == nil {
		return 0
	}
	manager.mutex.Lock()
	defer manager.mutex.Unlock()
	return len(manager.relays)
}

// Relay returns the open relay for one view on this replica.
func (manager *RelayManager) Relay(sessionID string) (*Relay, bool) {
	if manager == nil {
		return nil, false
	}
	manager.mutex.Lock()
	defer manager.mutex.Unlock()
	relay, exists := manager.relays[sessionID]
	return relay, exists
}

// ActiveSession reports whether this replica already carries a relay for the
// view.
func (manager *RelayManager) ActiveSession(sessionID string) bool {
	if manager == nil {
		return false
	}
	manager.mutex.Lock()
	defer manager.mutex.Unlock()
	_, exists := manager.relays[sessionID]
	return exists
}

// CloseAll closes every relay. It is the shutdown boundary.
func (manager *RelayManager) CloseAll() {
	if manager == nil {
		return
	}
	manager.mutex.Lock()
	if manager.closed {
		manager.mutex.Unlock()
		return
	}
	manager.closed = true
	open := make([]*Relay, 0, len(manager.relays))
	for _, relay := range manager.relays {
		open = append(open, relay)
	}
	manager.mutex.Unlock()

	for _, relay := range open {
		relay.Close(CloseReasonGatewayShutdown)
	}
	for _, relay := range open {
		<-relay.Done()
	}
}
