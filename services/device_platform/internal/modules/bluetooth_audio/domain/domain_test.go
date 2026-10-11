package domain

import (
	"errors"
	"strings"
	"testing"
)

func TestDefaultPolicyIsConservative(t *testing.T) {
	policy := Default()
	if policy.SchemaVersion != SchemaVersion {
		t.Fatalf("schema version = %d", policy.SchemaVersion)
	}
	if policy.Enabled {
		t.Fatalf("default policy must keep bluetooth audio off")
	}
	if policy.PairingAllowed {
		t.Fatalf("default policy must keep pairing off")
	}
	if !policy.SpeakerModeAllowed {
		t.Fatalf("default policy should allow speaker mode once enabled")
	}
	if !policy.AutoReconnect {
		t.Fatalf("default policy should auto reconnect")
	}
	if policy.PreferredCodec != CodecAuto {
		t.Fatalf("preferred codec = %q", policy.PreferredCodec)
	}
	if policy.MaxVolumePercent != DefaultMaxVolumePercent {
		t.Fatalf("max volume = %d", policy.MaxVolumePercent)
	}
}

func TestValidateInputBounds(t *testing.T) {
	valid := Input{
		Enabled:            true,
		SpeakerModeAllowed: true,
		PairingAllowed:     true,
		AutoReconnect:      true,
		PreferredCodec:     CodecLDAC,
		MaxVolumePercent:   100,
		DisplayName:        "  客厅音箱  ",
	}
	normalized, err := ValidateInput(valid)
	if err != nil {
		t.Fatalf("valid input rejected: %v", err)
	}
	if normalized.DisplayName != "客厅音箱" {
		t.Fatalf("display name not trimmed: %q", normalized.DisplayName)
	}

	cases := map[string]Input{
		"unknown codec": func() Input {
			bad := valid
			bad.PreferredCodec = "aptx"
			return bad
		}(),
		"volume too small": func() Input {
			bad := valid
			bad.MaxVolumePercent = -1
			return bad
		}(),
		"volume too large": func() Input {
			bad := valid
			bad.MaxVolumePercent = 101
			return bad
		}(),
		"name too long": func() Input {
			bad := valid
			bad.DisplayName = strings.Repeat("a", DisplayNameMaxLength+1)
			return bad
		}(),
	}
	for name, input := range cases {
		if _, err := ValidateInput(input); !errors.Is(err, ErrInvalidPolicy) {
			t.Fatalf("%s: err = %v, want ErrInvalidPolicy", name, err)
		}
	}
}

func TestValidateInputDisablingFeatureClearsDerivedFlags(t *testing.T) {
	input := Input{
		Enabled:            false,
		SpeakerModeAllowed: true,
		PairingAllowed:     true,
		AutoReconnect:      true,
		PreferredCodec:     CodecAuto,
		MaxVolumePercent:   70,
	}
	normalized, err := ValidateInput(input)
	if err != nil {
		t.Fatalf("valid input rejected: %v", err)
	}
	if normalized.SpeakerModeAllowed || normalized.PairingAllowed {
		t.Fatalf("disabling the feature must clear speaker and pairing flags")
	}
}

func TestValidateStatusSupported(t *testing.T) {
	valid := StatusInput{
		BluetoothSupported:   true,
		SpeakerMode:          true,
		AdapterState:         AdapterConnected,
		PairedDeviceCount:    3,
		ConnectedDeviceCount: 1,
		ActiveCodec:          ActiveCodecAAC,
		VolumePercent:        55,
		LastDisconnectReason: DisconnectRange,
		PolicyRevision:       2,
	}
	normalized, err := ValidateStatus(valid)
	if err != nil {
		t.Fatalf("valid status rejected: %v", err)
	}
	if !normalized.SpeakerMode {
		t.Fatalf("speaker mode should survive when connected")
	}

	cases := map[string]StatusInput{
		"bad adapter": func() StatusInput {
			bad := valid
			bad.AdapterState = "sleeping"
			return bad
		}(),
		"bad codec": func() StatusInput {
			bad := valid
			bad.ActiveCodec = "aptx"
			return bad
		}(),
		"bad reason": func() StatusInput {
			bad := valid
			bad.LastDisconnectReason = "exploded"
			return bad
		}(),
		"too many paired": func() StatusInput {
			bad := valid
			bad.PairedDeviceCount = MaxPairedDevices + 1
			return bad
		}(),
		"connected over paired": func() StatusInput {
			bad := valid
			bad.PairedDeviceCount = 1
			bad.ConnectedDeviceCount = 2
			return bad
		}(),
		"volume out of range": func() StatusInput {
			bad := valid
			bad.VolumePercent = 101
			return bad
		}(),
		"negative revision": func() StatusInput {
			bad := valid
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

func TestValidateStatusUnsupportedIsFullyOff(t *testing.T) {
	input := StatusInput{
		BluetoothSupported:   false,
		SpeakerMode:          true,
		AdapterState:         AdapterConnected,
		PairedDeviceCount:    4,
		ConnectedDeviceCount: 2,
		ActiveCodec:          ActiveCodecSBC,
		VolumePercent:        80,
		LastDisconnectReason: DisconnectUser,
		PolicyRevision:       1,
	}
	normalized, err := ValidateStatus(input)
	if err != nil {
		t.Fatalf("unsupported status rejected: %v", err)
	}
	if normalized.SpeakerMode ||
		normalized.AdapterState != AdapterOff ||
		normalized.PairedDeviceCount != 0 ||
		normalized.ConnectedDeviceCount != 0 ||
		normalized.ActiveCodec != ActiveCodecUnknown ||
		normalized.VolumePercent != 0 ||
		normalized.LastDisconnectReason != DisconnectNone {
		t.Fatalf("unsupported device must report a fully off adapter: %+v", normalized)
	}
}

func TestValidateStatusSpeakerModeRequiresConnection(t *testing.T) {
	input := StatusInput{
		BluetoothSupported:   true,
		SpeakerMode:          true,
		AdapterState:         AdapterAdvertising,
		PairedDeviceCount:    0,
		ConnectedDeviceCount: 0,
		ActiveCodec:          ActiveCodecUnknown,
		VolumePercent:        0,
		LastDisconnectReason: DisconnectNone,
		PolicyRevision:       1,
	}
	normalized, err := ValidateStatus(input)
	if err != nil {
		t.Fatalf("status rejected: %v", err)
	}
	if normalized.SpeakerMode {
		t.Fatalf("speaker mode must clear while not connected")
	}
}

func TestCatalogsAreConsistent(t *testing.T) {
	for _, codec := range PreferredCodecs {
		if !IsPreferredCodec(codec) {
			t.Fatalf("preferred codec %q not recognised", codec)
		}
	}
	if IsPreferredCodec("aptx") {
		t.Fatalf("unexpected codec accepted")
	}
	for _, state := range AdapterStates {
		if !IsAdapterState(state) {
			t.Fatalf("adapter state %q not recognised", state)
		}
	}
	for _, reason := range DisconnectReasons {
		if !IsDisconnectReason(reason) {
			t.Fatalf("disconnect reason %q not recognised", reason)
		}
	}
	if IsDisconnectReason("unknown") {
		t.Fatalf("unexpected reason accepted")
	}
}
