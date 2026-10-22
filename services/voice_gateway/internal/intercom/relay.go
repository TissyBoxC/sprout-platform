package intercom

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"
)

// maxCallDuration is the platform domain limit. The relay cap is the smaller
// of the configured value and the platform deadline, so the relay never
// outlives the call record it relies on.
const (
	defaultWriteQueueDepth = 64
	defaultMaxRelays       = 256
)

// Relay is one open intercom relay between a guardian and a device.
//
// Ownership: the registry creates it, the guardian WebSocket handler drives
// its read loop, and Close may be called from any goroutine. Close runs its
// side effects exactly once and releases the outbound queue.
type Relay struct {
	session Session
	reason  CloseReason
	opened  time.Time
	logger  *slog.Logger
	now     func() time.Time

	outbound    OutboundSink
	deviceToken string

	guardianLock   sync.Mutex
	guardianActive bool
	guardian       func(payload []byte) error

	cancel context.CancelCauseFunc
	// doneContext is cancelled by close; it is the only context the guardian
	// handler waits on, so a close unblocks reads and timers at once.
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
}

// newRelay builds the relay and starts its device writer.
//
// ctx is already cancelled by the caller when it wants the relay gone; the
// derived context inherits that, so a relay never outlives its parent.
func newRelay(
	ctx context.Context,
	session Session,
	outbound OutboundSink,
	deviceToken string,
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
		session:       session,
		reason:        CloseReasonTransportClosed,
		logger:        logger,
		now:           now,
		outbound:      outbound,
		deviceToken:   deviceToken,
		cancel:        cancel,
		doneContext:   relayContext,
		done:          make(chan struct{}),
		outboundQueue: make(chan []byte, writeQueueDepth),
		writerDone:    make(chan struct{}),
	}
	go relay.writeLoop(relayContext)
	return relay
}

// Session returns the authorized call this relay carries.
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

// ForwardToDevice queues one guardian frame for the device side.
//
// A full queue closes the relay instead of buffering without bound: intercom
// audio is only useful in real time, and dropping a frame silently would
// deliver chopped audio to a child.
func (relay *Relay) ForwardToDevice(payload []byte) error {
	if relay == nil {
		return ErrRelayUnavailable
	}
	if len(payload) == 0 {
		return nil
	}
	frame := append([]byte(nil), payload...)
	// Check the closed state first: a select with both a ready send and a ready
	// done case picks randomly, which would let a closed relay deliver one
	// last guardian frame after the call ended.
	select {
	case <-relay.done:
		return errors.New("intercom relay is closed")
	default:
	}
	select {
	case <-relay.done:
		return errors.New("intercom relay is closed")
	case relay.outboundQueue <- frame:
		relay.framesIn.Add(1)
		return nil
	default:
		relay.dropped.Add(1)
		relay.Close(CloseReasonTransportClosed)
		return errors.New("intercom relay outbound queue is full")
	}
}

// AttachGuardian claims the single guardian side of this relay.
//
// Exactly one guardian socket may carry a call; a second attach returns
// ErrSessionAlreadyRelayed so a reconnect cannot duplicate audio to the device.
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

// AttachGuardianWriter installs the guardian frame writer used for device
// audio. It is called by the guardian handler once its socket is upgraded.
func (relay *Relay) AttachGuardianWriter(writer func(payload []byte) error) {
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
// and dropped rather than buffered: late intercom audio is useless, and
// buffering it would grow memory for a call nobody is listening to yet.
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

// Done is closed once the relay has closed.
func (relay *Relay) Done() <-chan struct{} {
	if relay == nil {
		closed := make(chan struct{})
		close(closed)
		return closed
	}
	return relay.done
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

// Stats is a bounded, audio-free view of one relay for diagnostics.
type Stats struct {
	SessionID   string
	DeviceID    string
	CloseReason CloseReason
	Duration    time.Duration
	FramesIn    uint64
	FramesOut   uint64
	Dropped     uint64
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
	}
}

// logClosed records the lifecycle only: the call identifier, the measured
// duration, and the fixed close reason. Audio, device tokens, guardian
// identifiers, and transcripts never reach this line.
func (relay *Relay) logClosed() {
	duration := relay.now().Sub(relay.opened)
	relay.logger.Info("intercom relay closed",
		"session_id", relay.session.ID,
		"device_id", relay.session.DeviceID,
		"duration_ms", duration.Milliseconds(),
		"close_reason", string(relay.CloseReason()),
		"frames_in", relay.framesIn.Load(),
		"frames_out", relay.framesOut.Load(),
		"frames_dropped", relay.dropped.Load(),
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
				relay.Close(CloseReasonTransportClosed)
				return
			}
		}
	}
}

// RelayManager admits relays, enforces the concurrency cap, and releases
// every relay it created on shutdown.
//
// Admission is serialized so two guardians (or a reconnect) cannot both hold
// the same call: a second relay for a live session is refused instead of
// duplicating frames to the device.
type RelayManager struct {
	mutex     sync.Mutex
	closed    bool
	maxRelays int
	relays    map[string]*Relay
	now       func() time.Time
}

// NewRelayManager creates a relay admission manager.
func NewRelayManager(maxRelays int) *RelayManager {
	if maxRelays <= 0 {
		maxRelays = defaultMaxRelays
	}
	return &RelayManager{
		maxRelays: maxRelays,
		relays:    make(map[string]*Relay),
		now:       time.Now,
	}
}

// Admit registers a new relay for an already-authorized session.
//
// The manager never inspects the session state itself; authorization happens
// before this call, and the state rule lives in one place.
func (manager *RelayManager) Admit(
	ctx context.Context,
	session Session,
	outbound OutboundSink,
	deviceToken string,
	writeQueueDepth int,
	logger *slog.Logger,
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
	relay := newRelay(ctx, session, outbound, deviceToken, writeQueueDepth, logger, manager.now)
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

// Relay returns the open relay for one call on this replica.
func (manager *RelayManager) Relay(sessionID string) (*Relay, bool) {
	if manager == nil {
		return nil, false
	}
	manager.mutex.Lock()
	defer manager.mutex.Unlock()
	relay, exists := manager.relays[sessionID]
	return relay, exists
}

// ActiveSession reports whether this gateway replica already carries a relay
// for the call.
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
