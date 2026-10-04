// Package reference stores the most recent speaker playback per device so
// gateway-side echo cancellation can subtract it from the microphone signal.
//
// The device echoes what the speaker plays. Without a reference signal the
// adaptive filter has nothing to cancel, so the playback path publishes here
// and the capture path reads here. The bus keeps at most one frame per device
// and never logs or persists audio.
package reference

import "sync"

// Bus is a concurrency-safe registry of per-device speaker reference frames.
//
// A nil *Bus is valid and always reports silence, so callers can leave echo
// cancellation unconfigured without nil checks. Publish copies the frame so
// the playback buffer may be reused immediately.
type Bus struct {
	mutex  sync.RWMutex
	frames map[string][]int16
}

// NewBus creates an empty reference registry.
func NewBus() *Bus {
	return &Bus{frames: make(map[string][]int16)}
}

// Publish records the speaker frame being played for one device.
//
// An empty device identifier or empty frame clears the entry, which tells the
// echo canceller the speaker is silent for the next capture window.
func (b *Bus) Publish(deviceID string, frame []int16) {
	if b == nil || deviceID == "" {
		return
	}
	copied := append([]int16(nil), frame...)
	b.mutex.Lock()
	if len(copied) == 0 {
		delete(b.frames, deviceID)
	} else {
		b.frames[deviceID] = copied
	}
	b.mutex.Unlock()
}

// Latest returns a copy of the last published frame, or nil when the speaker
// is silent or the device has never played audio.
func (b *Bus) Latest(deviceID string) []int16 {
	if b == nil || deviceID == "" {
		return nil
	}
	b.mutex.RLock()
	frame := b.frames[deviceID]
	b.mutex.RUnlock()
	if len(frame) == 0 {
		return nil
	}
	return append([]int16(nil), frame...)
}

// Clear removes the retained frame for one device, used when a conversation or
// transport closes so audio does not outlive the session.
func (b *Bus) Clear(deviceID string) {
	if b == nil || deviceID == "" {
		return
	}
	b.mutex.Lock()
	delete(b.frames, deviceID)
	b.mutex.Unlock()
}
