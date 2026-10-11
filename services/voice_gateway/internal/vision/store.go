package vision

import (
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/TissyBoxC/sprout-platform/services/voice_gateway/internal/platform/clock"
)

var (
	taskIDPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]{7,127}$`)
	devicePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]{7,127}$`)
	labelPattern  = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9 _.:/-]{0,63}$`)
)

// Store holds short-lived vision results in memory.
//
// The gateway keeps results only for ResultRetention so a long-running process
// cannot accumulate unbounded child-derived metadata. Image bytes are never
// stored here; entries hold bounded labels, confidences, and a short summary.
// Entries are keyed by device and task so one device can never read another
// device's result even if it learns the task identifier.
type Store struct {
	mutex   sync.Mutex
	entries map[string]storedResult
	clock   clock.Clock
}

type storedResult struct {
	result    Result
	expiresAt time.Time
}

// NewStore creates an empty bounded result store.
func NewStore(timeSource clock.Clock) *Store {
	if timeSource == nil {
		timeSource = clock.SystemClock{}
	}
	return &Store{
		entries: make(map[string]storedResult),
		clock:   timeSource,
	}
}

// Save stores one bounded result, overwriting any prior entry for the task.
func (s *Store) Save(result Result) {
	if s == nil {
		return
	}
	now := s.clock.Now().UTC()
	result.CreatedAt = now
	if result.ExpiresAt.IsZero() || result.ExpiresAt.After(now.Add(ResultRetention)) {
		result.ExpiresAt = now.Add(ResultRetention)
	}
	s.mutex.Lock()
	defer s.mutex.Unlock()
	s.pruneLocked(now)
	s.entries[storeKey(result.DeviceID, result.TaskID)] = storedResult{
		result:    cloneResult(result),
		expiresAt: result.ExpiresAt,
	}
}

// Get returns one non-expired result scoped to the requesting device.
//
// The device identifier is part of the lookup key, so a caller holding a task
// identifier issued to a different device receives no result.
func (s *Store) Get(deviceID string, taskID string) (Result, bool) {
	if s == nil {
		return Result{}, false
	}
	deviceID = strings.TrimSpace(deviceID)
	taskID = strings.TrimSpace(taskID)
	now := s.clock.Now().UTC()
	s.mutex.Lock()
	defer s.mutex.Unlock()
	s.pruneLocked(now)
	entry, ok := s.entries[storeKey(deviceID, taskID)]
	if !ok || !entry.expiresAt.After(now) {
		return Result{}, false
	}
	return cloneResult(entry.result), true
}

// Prune removes expired entries and returns how many were evicted.
func (s *Store) Prune() int {
	if s == nil {
		return 0
	}
	now := s.clock.Now().UTC()
	s.mutex.Lock()
	defer s.mutex.Unlock()
	return s.pruneLocked(now)
}

// Len returns the number of live entries, pruning expired ones first.
func (s *Store) Len() int {
	if s == nil {
		return 0
	}
	s.Prune()
	s.mutex.Lock()
	defer s.mutex.Unlock()
	return len(s.entries)
}

func (s *Store) pruneLocked(now time.Time) int {
	removed := 0
	for key, entry := range s.entries {
		if !entry.expiresAt.After(now) {
			delete(s.entries, key)
			removed++
		}
	}
	return removed
}

// storeKey namespaces a task identifier by device so results from different
// devices remain isolated. A zero-length separator cannot appear in either
// component because both are validated against identifier patterns first.
func storeKey(deviceID string, taskID string) string {
	return deviceID + "\x00" + taskID
}

func cloneResult(result Result) Result {
	cloned := result
	cloned.Labels = append([]Label(nil), result.Labels...)
	return cloned
}
