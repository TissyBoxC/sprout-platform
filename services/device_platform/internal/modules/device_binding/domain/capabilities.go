package domain

import "strings"

type capabilitySet map[string]struct{}

// The vocabulary is intentionally an explicit slice rather than a map so the
// shared contract validator can compare it directly with common.schema.json.
var deviceCapabilities = []string{
	"audio_input",
	"audio_output",
	"wifi",
	"camera",
	"display",
	"touch",
	"led",
	"battery",
	"cellular_4g",
	"motion",
	"bluetooth_audio",
	"video_call",
	"location",
	"geofence",
	"sos",
	"multi_device",
}

// DeviceCapabilities returns a copy of the stable capability vocabulary shared
// by registration and runtime capability snapshots.
func DeviceCapabilities() []string {
	return append([]string(nil), deviceCapabilities...)
}

// NormalizeCapabilities returns a trimmed, de-duplicated snapshot in input
// order. Any unknown capability is rejected as a whole so a partial set never
// reaches a guardian-facing device view.
func NormalizeCapabilities(capabilities []string) ([]string, error) {
	allowed := make(capabilitySet, len(DeviceCapabilities()))
	for _, capability := range DeviceCapabilities() {
		allowed[capability] = struct{}{}
	}
	normalized := make([]string, 0, len(capabilities))
	seen := make(map[string]struct{}, len(capabilities))
	for _, capability := range capabilities {
		capability = strings.TrimSpace(capability)
		if _, supported := allowed[capability]; !supported {
			return nil, ErrInvalidDeviceID
		}
		if _, exists := seen[capability]; exists {
			continue
		}
		seen[capability] = struct{}{}
		normalized = append(normalized, capability)
	}
	if len(normalized) == 0 {
		return nil, ErrInvalidDeviceID
	}
	return normalized, nil
}
