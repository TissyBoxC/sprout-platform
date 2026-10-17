// Package domain contains device provisioning and binding types.
package domain

import (
	"errors"
	"time"
)

var (
	ErrTokenNotFound             = errors.New("binding token not found")
	ErrTokenExpired              = errors.New("binding token expired")
	ErrTokenConsumed             = errors.New("binding token already used")
	ErrDeviceNotFound            = errors.New("device not found")
	ErrDeviceBound               = errors.New("device already bound")
	ErrInvalidDeviceID           = errors.New("invalid device id")
	ErrDeviceDisabled            = errors.New("device is disabled")
	ErrRegistrationTokenNotFound = errors.New("device registration token not found")
	ErrRegistrationTokenExpired  = errors.New("device registration token expired")
	ErrRegistrationTokenConsumed = errors.New("device registration token already used")
	ErrDeviceChallengeNotFound   = errors.New("device challenge not found")
	ErrDeviceChallengeExpired    = errors.New("device challenge expired")
	ErrDeviceChallengeConsumed   = errors.New("device challenge already used")
	ErrInvalidDeviceProof        = errors.New("device proof is invalid")
	ErrDeviceSessionNotFound     = errors.New("device session not found")
	ErrDeviceSessionExpired      = errors.New("device session expired")
)

// BindingToken is a short-lived, single-use device provisioning token.
type BindingToken struct {
	ID         string
	DeviceID   string
	TokenHash  string
	ExpiresAt  time.Time
	ConsumedAt *time.Time
	CreatedAt  time.Time
}

// RegistrationToken authorizes one device to register its public key.
type RegistrationToken struct {
	ID         string
	DeviceID   string
	TokenHash  string
	ExpiresAt  time.Time
	ConsumedAt *time.Time
	CreatedAt  time.Time
}

// Binding is the durable relationship between a parent and a device.
type Binding struct {
	ID                 string
	ParentAccountID    string
	DeviceID           string
	DeviceName         string
	HardwareModel      string
	FirmwareVersion    string
	Capabilities       []string
	CapabilityRevision uint64
	BoundAt            time.Time
	UpdatedAt          time.Time
}

// DeviceCredential stores the public device identity used for challenge
// authentication. The platform never receives the device's private key.
type DeviceCredential struct {
	DeviceID            string
	HardwareModel       string
	FirmwareVersion     string
	CapabilitySet       []string
	CapabilityRevision  uint64
	PublicKey           string
	Status              string
	RegisteredAt        time.Time
	LastAuthenticatedAt *time.Time
	UpdatedAt           time.Time
}

// DeviceChallenge is one short-lived nonce issued to a registered device.
type DeviceChallenge struct {
	ID         string
	DeviceID   string
	NonceHash  string
	ExpiresAt  time.Time
	ConsumedAt *time.Time
	CreatedAt  time.Time
}

// DeviceSession is a short-lived opaque token issued after device proof
// verification. Only its hash is persisted.
type DeviceSession struct {
	ID         string
	DeviceID   string
	TokenHash  string
	ExpiresAt  time.Time
	RevokedAt  *time.Time
	CreatedAt  time.Time
	LastUsedAt time.Time
}

// DeviceRegistrationInput contains the identity and capability data reported
// by a device during its first authenticated registration.
type DeviceRegistrationInput struct {
	DeviceID        string
	HardwareModel   string
	FirmwareVersion string
	Capabilities    []string
	PublicKey       string
}
