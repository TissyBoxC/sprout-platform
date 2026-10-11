// Package domain defines the device offline download policy for content.
//
// The policy tells a device how much content it may cache for offline use and
// when that cache must be refreshed or evicted. It is platform-wide: one
// version is published by an operator, every bound device reads the same
// policy, and a device reports back the revision it currently applies so the
// console can see which devices are behind.
package domain

import "errors"

var (
	// ErrInvalidPolicy reports a policy value outside the approved bounds.
	ErrInvalidPolicy = errors.New("invalid content offline policy")
	// ErrInvalidAcceptedRevision reports a negative reported revision.
	ErrInvalidAcceptedRevision = errors.New("invalid accepted revision")
)

// Approved bounds. They are deliberately conservative: a device has limited
// flash, so the cache cap and the per-item size ceiling keep one bad policy
// from filling local storage.
const (
	MinCachedPackages = 1
	MaxCachedPackages = 200

	// MinCacheBytes is 16 MiB and MaxCacheBytes is 8 GiB.
	MinCacheBytes = 16 * 1024 * 1024
	MaxCacheBytes = 8 * 1024 * 1024 * 1024

	// MinCacheTTLHours is one hour and MaxCacheTTLHours is one year.
	MinCacheTTLHours = 1
	MaxCacheTTLHours = 24 * 365

	// DefaultCachedPackages and friends are the fallback policy served before
	// an operator publishes one, so a fresh device still has a sane contract.
	DefaultCachedPackages = 20
	DefaultCacheBytes     = 512 * 1024 * 1024
	DefaultCacheTTLHours  = 24 * 30
)

// Policy is the platform-wide device offline download policy.
type Policy struct {
	// OfflineEnabled lets a device keep playing cached content with no network.
	OfflineEnabled bool
	// MaxCachedPackages caps how many package versions a device may cache.
	MaxCachedPackages int
	// MaxCacheBytes caps the total cached bytes on the device.
	MaxCacheBytes int64
	// CacheTTLHours is how long a cached package stays valid before refresh.
	CacheTTLHours int
	// DownloadOverWifiOnly keeps large downloads off metered links.
	DownloadOverWifiOnly bool
	// Revision is bumped on every publish so a device can sync incrementally.
	Revision int
}

// Input is the editable policy payload accepted from an operator.
type Input struct {
	OfflineEnabled       bool
	MaxCachedPackages    int
	MaxCacheBytes        int64
	CacheTTLHours        int
	DownloadOverWifiOnly bool
}

// Default returns the fallback policy used before an operator publishes one.
func Default() Policy {
	return Policy{
		OfflineEnabled:       true,
		MaxCachedPackages:    DefaultCachedPackages,
		MaxCacheBytes:        DefaultCacheBytes,
		CacheTTLHours:        DefaultCacheTTLHours,
		DownloadOverWifiOnly: true,
		Revision:             0,
	}
}

// ValidateInput normalizes and validates one policy payload.
func ValidateInput(input Input) (Input, error) {
	normalized := Input{
		OfflineEnabled:       input.OfflineEnabled,
		MaxCachedPackages:    input.MaxCachedPackages,
		MaxCacheBytes:        input.MaxCacheBytes,
		CacheTTLHours:        input.CacheTTLHours,
		DownloadOverWifiOnly: input.DownloadOverWifiOnly,
	}
	if normalized.MaxCachedPackages < MinCachedPackages ||
		normalized.MaxCachedPackages > MaxCachedPackages {
		return Input{}, ErrInvalidPolicy
	}
	if normalized.MaxCacheBytes < MinCacheBytes ||
		normalized.MaxCacheBytes > MaxCacheBytes {
		return Input{}, ErrInvalidPolicy
	}
	if normalized.CacheTTLHours < MinCacheTTLHours ||
		normalized.CacheTTLHours > MaxCacheTTLHours {
		return Input{}, ErrInvalidPolicy
	}
	return normalized, nil
}

// ValidateAcceptedRevision guards a device-reported revision.
func ValidateAcceptedRevision(revision int) error {
	if revision < 0 {
		return ErrInvalidAcceptedRevision
	}
	return nil
}

// Acknowledgement records the policy revision one device currently applies.
type Acknowledgement struct {
	DeviceID string
	FamilyID string
	Revision int
	// UpToDate reports whether the device has the newest published revision.
	UpToDate bool
}
