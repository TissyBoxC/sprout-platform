package domain

import (
	"errors"
	"testing"
)

func TestDefaultPolicyIsConservative(t *testing.T) {
	policy := Default()
	if policy.SchemaVersion != SchemaVersion {
		t.Fatalf("schema version = %d", policy.SchemaVersion)
	}
	if !policy.Enabled {
		t.Fatalf("default policy should be enabled")
	}
	if policy.CameraAllowed || policy.MicrophoneAllowed {
		t.Fatalf("default policy must deny camera and microphone")
	}
	if !policy.RequireChildConsent {
		t.Fatalf("default policy must require child consent")
	}
	if policy.RingTimeoutSeconds != DefaultRingTimeoutSeconds {
		t.Fatalf("ring timeout = %d", policy.RingTimeoutSeconds)
	}
	if policy.MaxSessionSeconds != DefaultMaxSessionSeconds {
		t.Fatalf("max session = %d", policy.MaxSessionSeconds)
	}
}

func TestValidateInputBounds(t *testing.T) {
	valid := Input{
		Enabled:             true,
		CameraAllowed:       true,
		MicrophoneAllowed:   true,
		RequireChildConsent: true,
		QualityFloor:        QualityFair,
		RingTimeoutSeconds:  MinRingTimeoutSeconds,
		MaxSessionSeconds:   MaxMaxSessionSeconds,
	}
	if _, err := ValidateInput(valid); err != nil {
		t.Fatalf("valid input rejected: %v", err)
	}

	cases := map[string]Input{
		"unknown quality": func() Input {
			bad := valid
			bad.QualityFloor = "ultra"
			return bad
		}(),
		"ring too small": func() Input {
			bad := valid
			bad.RingTimeoutSeconds = MinRingTimeoutSeconds - 1
			return bad
		}(),
		"ring too large": func() Input {
			bad := valid
			bad.RingTimeoutSeconds = MaxRingTimeoutSeconds + 1
			return bad
		}(),
		"session too small": func() Input {
			bad := valid
			bad.MaxSessionSeconds = MinMaxSessionSeconds - 1
			return bad
		}(),
		"session too large": func() Input {
			bad := valid
			bad.MaxSessionSeconds = MaxMaxSessionSeconds + 1
			return bad
		}(),
	}
	for name, input := range cases {
		if _, err := ValidateInput(input); !errors.Is(err, ErrInvalidPolicy) {
			t.Fatalf("%s: err = %v, want ErrInvalidPolicy", name, err)
		}
	}
}

func TestValidateStatusCoercesUnsupportedDevice(t *testing.T) {
	// A device that does not support the session cannot report a live state,
	// active capture or a stale end reason.
	input := StatusInput{
		SessionSupported:  false,
		CameraAllowed:     true,
		MicrophoneAllowed: true,
		CallState:         StateActive,
		Quality:           QualityGood,
		LastEndReason:     EndHangup,
		PolicyRevision:    3,
	}
	normalized, err := ValidateStatus(input)
	if err != nil {
		t.Fatalf("unsupported status rejected: %v", err)
	}
	if normalized.CallState != StateIdle {
		t.Fatalf("call state = %q, want idle", normalized.CallState)
	}
	if normalized.Quality != QualityUnknown {
		t.Fatalf("quality = %q, want unknown", normalized.Quality)
	}
	if normalized.LastEndReason != EndNone {
		t.Fatalf("end reason = %q, want none", normalized.LastEndReason)
	}
	if normalized.CameraAllowed || normalized.MicrophoneAllowed {
		t.Fatalf("unsupported device must not report capture permission")
	}
}

func TestValidateStatusRejectsUnknownValues(t *testing.T) {
	base := StatusInput{
		SessionSupported:  true,
		CameraAllowed:     true,
		MicrophoneAllowed: true,
		CallState:         StateRinging,
		Quality:           QualityGood,
		LastEndReason:     EndNone,
		PolicyRevision:    1,
	}
	if _, err := ValidateStatus(base); err != nil {
		t.Fatalf("valid status rejected: %v", err)
	}

	cases := map[string]StatusInput{
		"bad state": func() StatusInput {
			bad := base
			bad.CallState = "dialing"
			return bad
		}(),
		"bad quality": func() StatusInput {
			bad := base
			bad.Quality = "great"
			return bad
		}(),
		"bad reason": func() StatusInput {
			bad := base
			bad.LastEndReason = "exploded"
			return bad
		}(),
		"negative revision": func() StatusInput {
			bad := base
			bad.PolicyRevision = -1
			return bad
		}(),
	}
	for name, input := range cases {
		if _, err := ValidateStatus(input); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("%s: err = %v, want ErrInvalidInput", name, err)
		}
	}
}

func TestCatalogChecks(t *testing.T) {
	if !IsQuality(QualityPoor) || IsQuality("") {
		t.Fatalf("quality catalog check failed")
	}
	if !IsState(StateRejected) || IsState("unknown") {
		t.Fatalf("state catalog check failed")
	}
	if !IsEndReason(EndPermissionRevoked) || IsEndReason("dropped") {
		t.Fatalf("end reason catalog check failed")
	}
}
