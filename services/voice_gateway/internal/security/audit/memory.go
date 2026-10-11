package audit

import (
	"strings"
	"sync"
)

// maxMemoryEvents bounds the in-memory sink so a long-running process or a
// misbehaving caller cannot grow memory without limit. It is intended for
// tests and single-process diagnostics, not as durable storage.
const maxMemoryEvents = 1024

// MemorySink keeps the most recent events in a bounded ring for tests and
// diagnostics. It stores only normalized Event values, which are already
// content-free by construction.
type MemorySink struct {
	mutex  sync.Mutex
	events []Event
}

// NewMemorySink creates an empty bounded sink.
func NewMemorySink() *MemorySink {
	return &MemorySink{events: make([]Event, 0, maxMemoryEvents)}
}

// Record stores one event, evicting the oldest entry when the bound is hit.
func (sink *MemorySink) Record(event Event) {
	if sink == nil {
		return
	}
	event = normalize(event)
	sink.mutex.Lock()
	defer sink.mutex.Unlock()
	if len(sink.events) >= maxMemoryEvents {
		sink.events = sink.events[1:]
	}
	sink.events = append(sink.events, event)
}

// Events returns a copy of the captured events in insertion order.
func (sink *MemorySink) Events() []Event {
	if sink == nil {
		return nil
	}
	sink.mutex.Lock()
	defer sink.mutex.Unlock()
	return append([]Event(nil), sink.events...)
}

// FilterByReason returns captured events with the given reason code.
func (sink *MemorySink) FilterByReason(reason string) []Event {
	reason = strings.TrimSpace(reason)
	events := sink.Events()
	filtered := make([]Event, 0, len(events))
	for _, event := range events {
		if event.Reason == reason {
			filtered = append(filtered, event)
		}
	}
	return filtered
}
