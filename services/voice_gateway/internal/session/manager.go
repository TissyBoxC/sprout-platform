package session

import (
	"errors"
	"sync"
)

const (
	// DefaultMaxSessions bounds aggregate gateway memory and upstream cost.
	DefaultMaxSessions = 128

	// DefaultMaxSessionsPerDevice prevents one compromised device from
	// consuming every available realtime slot.
	DefaultMaxSessionsPerDevice = 3
)

// Errors returned by the session manager.
var (
	ErrManagerClosed        = errors.New("voice session manager is closed")
	ErrSessionLimitReached  = errors.New("voice session limit reached")
	ErrDeviceSessionLimit   = errors.New("device voice session limit reached")
	ErrDuplicateStream      = errors.New("voice stream is already active for the device")
	ErrSessionAlreadyExists = errors.New("voice session id already exists")
	ErrSessionNotFound      = errors.New("voice session not found")
)

// ManagerConfig bounds aggregate and per-device concurrency.
type ManagerConfig struct {
	MaxSessions          int
	MaxSessionsPerDevice int
}

// Manager creates and tracks active voice sessions.
//
// It serialises admission and removal so concurrent WebSocket handshakes
// cannot exceed the configured limits. CloseAll is the shutdown boundary and
// must be called before the process exits.
type Manager struct {
	mutex          sync.RWMutex
	closed         bool
	maxSessions    int
	maxPerDevice   int
	sessions       map[string]*Session
	deviceSessions map[string]map[string]struct{}
	streams        map[string]string
}

// NewManager creates a manager with the supplied limits.
//
// Non-positive limits receive production defaults and are never unbounded.
func NewManager(config ManagerConfig) *Manager {
	if config.MaxSessions <= 0 {
		config.MaxSessions = DefaultMaxSessions
	}
	if config.MaxSessionsPerDevice <= 0 {
		config.MaxSessionsPerDevice = DefaultMaxSessionsPerDevice
	}
	return &Manager{
		maxSessions:    config.MaxSessions,
		maxPerDevice:   config.MaxSessionsPerDevice,
		sessions:       make(map[string]*Session, config.MaxSessions),
		deviceSessions: make(map[string]map[string]struct{}),
		streams:        make(map[string]string),
	}
}

// Create admits a new session or returns a stable admission error.
//
// The manager owns the returned session and removes it automatically when it
// closes. No partial state remains when an admission limit is exceeded.
func (m *Manager) Create(config SessionConfig) (*Session, error) {
	if m == nil {
		return nil, ErrManagerClosed
	}
	m.mutex.Lock()
	defer m.mutex.Unlock()

	if m.closed {
		return nil, ErrManagerClosed
	}
	if len(m.sessions) >= m.maxSessions {
		return nil, ErrSessionLimitReached
	}
	if len(m.deviceSessions[config.DeviceID]) >= m.maxPerDevice {
		return nil, ErrDeviceSessionLimit
	}
	streamKey := streamKey(config.DeviceID, config.StreamID)
	if _, exists := m.streams[streamKey]; exists {
		return nil, ErrDuplicateStream
	}
	if _, exists := m.sessions[config.ID]; exists {
		return nil, ErrSessionAlreadyExists
	}

	created, err := NewSession(config)
	if err != nil {
		return nil, err
	}
	m.sessions[created.ID()] = created
	if m.deviceSessions[created.DeviceID()] == nil {
		m.deviceSessions[created.DeviceID()] = make(map[string]struct{}, m.maxPerDevice)
	}
	m.deviceSessions[created.DeviceID()][created.ID()] = struct{}{}
	m.streams[streamKey] = created.ID()

	go func() {
		<-created.Done()
		m.Remove(created.ID())
	}()
	return created, nil
}

// Get returns the active session by ID.
func (m *Manager) Get(sessionID string) (*Session, bool) {
	if m == nil {
		return nil, false
	}
	m.mutex.RLock()
	defer m.mutex.RUnlock()
	active, ok := m.sessions[sessionID]
	return active, ok
}

// FindByStream returns the active session for one device and stream.
func (m *Manager) FindByStream(deviceID string, streamID string) (*Session, bool) {
	if m == nil {
		return nil, false
	}
	m.mutex.RLock()
	defer m.mutex.RUnlock()
	sessionID, ok := m.streams[streamKey(deviceID, streamID)]
	if !ok {
		return nil, false
	}
	active, ok := m.sessions[sessionID]
	return active, ok
}

// Remove closes and forgets one session. It is idempotent.
func (m *Manager) Remove(sessionID string) bool {
	if m == nil {
		return false
	}
	m.mutex.Lock()
	active, ok := m.sessions[sessionID]
	if ok {
		delete(m.sessions, sessionID)
		deviceID := active.DeviceID()
		delete(m.deviceSessions[deviceID], sessionID)
		if len(m.deviceSessions[deviceID]) == 0 {
			delete(m.deviceSessions, deviceID)
		}
		delete(m.streams, streamKey(active.DeviceID(), active.StreamID()))
	}
	m.mutex.Unlock()
	if ok {
		_ = active.Close()
	}
	return ok
}

// Count returns the number of active sessions.
func (m *Manager) Count() int {
	if m == nil {
		return 0
	}
	m.mutex.RLock()
	defer m.mutex.RUnlock()
	return len(m.sessions)
}

// CountForDevice returns the number of active sessions for one device.
func (m *Manager) CountForDevice(deviceID string) int {
	if m == nil {
		return 0
	}
	m.mutex.RLock()
	defer m.mutex.RUnlock()
	return len(m.deviceSessions[deviceID])
}

// Snapshot returns the active session identifiers without exposing audio.
func (m *Manager) Snapshot() []string {
	if m == nil {
		return nil
	}
	m.mutex.RLock()
	defer m.mutex.RUnlock()
	identifiers := make([]string, 0, len(m.sessions))
	for identifier := range m.sessions {
		identifiers = append(identifiers, identifier)
	}
	return identifiers
}

// QualitySnapshots returns read-only preprocessing metrics for every active
// session. The returned values contain only bounded scalars.
func (m *Manager) QualitySnapshots() []QualitySnapshot {
	if m == nil {
		return nil
	}
	m.mutex.RLock()
	active := make([]*Session, 0, len(m.sessions))
	for _, voiceSession := range m.sessions {
		active = append(active, voiceSession)
	}
	m.mutex.RUnlock()

	snapshots := make([]QualitySnapshot, 0, len(active))
	for _, voiceSession := range active {
		snapshots = append(snapshots, voiceSession.Quality())
	}
	return snapshots
}

// CloseAll closes and forgets every session.
//
// It blocks until all session watchdogs have observed cancellation, which
// makes it safe to use as part of graceful process shutdown.
func (m *Manager) CloseAll() {
	if m == nil {
		return
	}
	m.mutex.Lock()
	if m.closed {
		m.mutex.Unlock()
		return
	}
	m.closed = true
	active := make([]*Session, 0, len(m.sessions))
	for _, voiceSession := range m.sessions {
		active = append(active, voiceSession)
	}
	m.sessions = make(map[string]*Session)
	m.deviceSessions = make(map[string]map[string]struct{})
	m.streams = make(map[string]string)
	m.mutex.Unlock()

	for _, voiceSession := range active {
		_ = voiceSession.Close()
	}
	for _, voiceSession := range active {
		<-voiceSession.Done()
	}
}

func streamKey(deviceID string, streamID string) string {
	return deviceID + "\x00" + streamID
}
