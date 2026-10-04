// Package playback schedules outbound audio for one device stream.
//
// The device has a single speaker, so replies, prompts, reminders, and tones
// compete for the same output. The queue applies an explicit priority order,
// supports interruption and resume, enforces mute, and never exceeds the
// guardian's maximum volume.
package playback

import (
	"errors"
	"fmt"
	"sync"
	"time"
)

// Priority orders outbound audio. Higher priority interrupts lower priority.
type Priority int

const (
	// PriorityAmbient is background content that always yields.
	PriorityAmbient Priority = iota

	// PriorityConversation is the AI reply to the child.
	PriorityConversation

	// PriorityPrompt is a system message such as "network restored".
	PriorityPrompt

	// PrioritySafety is a crisis or guardian-safety announcement. It is never
	// preempted or suppressed by mute.
	PrioritySafety
)

// Errors returned by queue operations.
var (
	ErrEmptyQueue        = errors.New("playback queue is empty")
	ErrItemNotFound      = errors.New("playback item not found")
	ErrNoInterruption    = errors.New("playback item cannot be interrupted")
	ErrInvalidVolume     = errors.New("volume percent is outside 0-100")
	ErrDuplicateItemID   = errors.New("playback item id already exists")
	ErrUnsupportedFormat = errors.New("playback item payload is empty")
)

// Item is one queued audio segment.
type Item struct {
	ItemID        string
	Priority      Priority
	Payload       []byte
	EnqueuedAt    time.Time
	Interruptible bool
	// OnPlayed receives the gain-applied PCM immediately before Opus encoding.
	// It is optional and must not block playback.
	OnPlayed func([]int16)
}

// Snapshot is the observable queue state for telemetry and the device UI.
type Snapshot struct {
	NowPlaying *Item
	Pending    int
	IsPaused   bool
	IsMuted    bool
	Volume     int
	MaxVolume  int
}

// Queue holds pending playback for one stream.
//
// Methods are safe for concurrent use: the session loop enqueues replies while
// the transport loop drains and reports playback progress.
type Queue struct {
	mutex      sync.Mutex
	pending    []*Item
	nowPlaying *Item
	isPaused   bool
	isMuted    bool
	volume     int
	maxVolume  int
}

// NewQueue returns a queue with mute off and a conservative default volume.
//
// maxVolumePercent comes from the guardian policy. A value outside 0-100 is
// rejected so a bad policy cannot raise the hardware above its allowed level.
func NewQueue(volumePercent int, maxVolumePercent int) (*Queue, error) {
	if !isValidVolume(volumePercent) || !isValidVolume(maxVolumePercent) {
		return nil, ErrInvalidVolume
	}
	effectiveVolume := volumePercent
	if effectiveVolume > maxVolumePercent {
		effectiveVolume = maxVolumePercent
	}
	return &Queue{
		volume:    effectiveVolume,
		maxVolume: maxVolumePercent,
	}, nil
}

// Enqueue adds one item, interrupting the current item when the new priority is
// strictly higher and the current item is interruptible.
func (q *Queue) Enqueue(item Item) error {
	if q == nil {
		return ErrEmptyQueue
	}
	if len(item.Payload) == 0 {
		return ErrUnsupportedFormat
	}
	if item.ItemID == "" {
		return ErrItemNotFound
	}
	if item.EnqueuedAt.IsZero() {
		item.EnqueuedAt = time.Now().UTC()
	}

	q.mutex.Lock()
	defer q.mutex.Unlock()

	if q.containsItemIDLocked(item.ItemID) {
		return ErrDuplicateItemID
	}

	if q.nowPlaying != nil && item.Priority > q.nowPlaying.Priority {
		if !q.nowPlaying.Interruptible {
			return ErrNoInterruption
		}
		// The interrupted item goes back into the pending set. Priority order
		// then resumes it after the interrupting item but before any lower
		// priority work that was already waiting.
		interrupted := q.nowPlaying
		q.nowPlaying = nil
		q.insertLocked(interrupted)
	}

	queued := item
	q.insertLocked(&queued)
	return nil
}

// Next finishes the current item and returns the next one to play.
//
// A paused queue returns false without changing state. A resumed item is
// returned before newly queued work so an interrupted reply finishes.
func (q *Queue) Next() (*Item, bool) {
	if q == nil {
		return nil, false
	}

	q.mutex.Lock()
	defer q.mutex.Unlock()

	if q.isPaused {
		return nil, false
	}
	if len(q.pending) == 0 {
		q.nowPlaying = nil
		return nil, false
	}
	next := q.pending[0]
	q.pending = q.pending[1:]
	q.nowPlaying = next
	return next, true
}

// TakeCurrent removes one item from the active slot if it is still current.
//
// A preemption, clear, or mute may have requeued or dropped the item while it
// was being encoded. Only the caller that still owns the active slot may send
// its frame, which prevents both stale output and duplicate playback.
func (q *Queue) TakeCurrent(itemID string) bool {
	if q == nil || itemID == "" {
		return false
	}
	q.mutex.Lock()
	defer q.mutex.Unlock()
	if q.nowPlaying != nil && q.nowPlaying.ItemID == itemID {
		q.nowPlaying = nil
		return true
	}
	return false
}

// Pause stops returning items until Resume is called.
func (q *Queue) Pause() {
	if q == nil {
		return
	}
	q.mutex.Lock()
	defer q.mutex.Unlock()
	q.isPaused = true
}

// Resume allows playback to continue.
func (q *Queue) Resume() {
	if q == nil {
		return
	}
	q.mutex.Lock()
	defer q.mutex.Unlock()
	q.isPaused = false
}

// SetMuted toggles mute. Mute suppresses normal and prompt audio but never
// safety announcements, because guardians rely on those reaching the child.
func (q *Queue) SetMuted(isMuted bool) {
	if q == nil {
		return
	}
	q.mutex.Lock()
	defer q.mutex.Unlock()
	q.isMuted = isMuted
	if isMuted {
		q.dropSuppressibleLocked()
	}
}

// SetVolume sets the current volume within the guardian maximum.
func (q *Queue) SetVolume(volumePercent int) error {
	if q == nil {
		return ErrEmptyQueue
	}
	if !isValidVolume(volumePercent) {
		return ErrInvalidVolume
	}
	q.mutex.Lock()
	defer q.mutex.Unlock()
	if volumePercent > q.maxVolume {
		q.volume = q.maxVolume
		return nil
	}
	q.volume = volumePercent
	return nil
}

// SetMaxVolume applies an updated guardian policy limit.
//
// Lowering the limit immediately reduces the active volume so a policy change
// takes effect without waiting for the next conversation.
func (q *Queue) SetMaxVolume(maxVolumePercent int) error {
	if q == nil {
		return ErrEmptyQueue
	}
	if !isValidVolume(maxVolumePercent) {
		return ErrInvalidVolume
	}
	q.mutex.Lock()
	defer q.mutex.Unlock()
	q.maxVolume = maxVolumePercent
	if q.volume > q.maxVolume {
		q.volume = q.maxVolume
	}
	return nil
}

// Clear drops pending audio.
//
// Safety announcements are retained so a crisis prompt cannot be cancelled by
// a generic clear request. Pass includeSafety to force a full clear during a
// session teardown.
func (q *Queue) Clear(includeSafety bool) {
	if q == nil {
		return
	}
	q.mutex.Lock()
	defer q.mutex.Unlock()
	if includeSafety {
		q.pending = nil
		q.nowPlaying = nil
		return
	}
	if q.nowPlaying != nil && q.nowPlaying.Priority != PrioritySafety {
		q.nowPlaying = nil
	}
	// A generic clear cancels conversational audio but keeps safety
	// announcements, which must still reach the child.
	retained := make([]*Item, 0, len(q.pending))
	for _, queued := range q.pending {
		if queued.Priority == PrioritySafety {
			retained = append(retained, queued)
		}
	}
	q.pending = retained
}

// PlaybackVolume returns the volume to apply to one priority.
//
// Mute silences suppressible audio but never a safety announcement, so safety
// playback keeps the guardian-approved volume while mute is active.
func (q *Queue) PlaybackVolume(priority Priority) int {
	if q == nil {
		return 0
	}
	q.mutex.Lock()
	defer q.mutex.Unlock()
	if q.isMuted && priority != PrioritySafety {
		return 0
	}
	return q.volume
}

// Snapshot returns the current queue state with the effective volume.
func (q *Queue) Snapshot() Snapshot {
	if q == nil {
		return Snapshot{}
	}
	q.mutex.Lock()
	defer q.mutex.Unlock()
	effectiveVolume := q.volume
	if q.isMuted && effectiveVolume > 0 {
		effectiveVolume = 0
	}
	return Snapshot{
		NowPlaying: q.nowPlaying,
		Pending:    len(q.pending),
		IsPaused:   q.isPaused,
		IsMuted:    q.isMuted,
		Volume:     effectiveVolume,
		MaxVolume:  q.maxVolume,
	}
}

func (q *Queue) containsItemIDLocked(itemID string) bool {
	for _, queued := range q.pending {
		if queued.ItemID == itemID {
			return true
		}
	}
	if q.nowPlaying != nil && q.nowPlaying.ItemID == itemID {
		return true
	}
	return false
}

// insertLocked adds one item in descending priority order. Equal priorities
// keep enqueue order, so audio of the same class plays in the order it was
// produced.
func (q *Queue) insertLocked(item *Item) {
	insertAt := len(q.pending)
	for index, queued := range q.pending {
		if item.Priority > queued.Priority {
			insertAt = index
			break
		}
	}
	q.pending = append(q.pending, nil)
	copy(q.pending[insertAt+1:], q.pending[insertAt:])
	q.pending[insertAt] = item
}

// dropSuppressibleLocked removes everything except safety announcements. The
// current item is dropped too because mute must silence the speaker now.
func (q *Queue) dropSuppressibleLocked() {
	retained := make([]*Item, 0, len(q.pending))
	for _, queued := range q.pending {
		if queued.Priority == PrioritySafety {
			retained = append(retained, queued)
		}
	}
	q.pending = retained
	if q.nowPlaying != nil && q.nowPlaying.Priority != PrioritySafety {
		q.nowPlaying = nil
	}
}

func isValidVolume(volumePercent int) bool {
	return volumePercent >= 0 && volumePercent <= 100
}

// String provides a stable diagnostic label for one priority.
func (p Priority) String() string {
	switch p {
	case PrioritySafety:
		return "safety"
	case PriorityPrompt:
		return "prompt"
	case PriorityConversation:
		return "conversation"
	case PriorityAmbient:
		return "ambient"
	default:
		return fmt.Sprintf("priority_%d", int(p))
	}
}
