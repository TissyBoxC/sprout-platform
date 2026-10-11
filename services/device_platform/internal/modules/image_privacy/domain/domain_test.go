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
	if policy.Enabled {
		t.Fatalf("default policy must keep image processing off")
	}
	if policy.UploadAllowed {
		t.Fatalf("default policy must keep image upload off")
	}
	if !policy.LocalProcessingOnly {
		t.Fatalf("default policy must stay local-only")
	}
	if policy.ProcessingMode != ModeMetadata {
		t.Fatalf("default mode = %q", policy.ProcessingMode)
	}
	if policy.UploadScope != ScopeNone {
		t.Fatalf("default scope = %q", policy.UploadScope)
	}
	if policy.RetentionDays != DefaultRetentionDays {
		t.Fatalf("default retention = %d", policy.RetentionDays)
	}
	if policy.RedactionLevel != RedactionStrict {
		t.Fatalf("default redaction = %q", policy.RedactionLevel)
	}
	if !policy.RequireChildConsent {
		t.Fatalf("default policy must require child consent")
	}
}

func TestValidateInputBounds(t *testing.T) {
	valid := Input{
		Enabled:             true,
		UploadAllowed:       true,
		LocalProcessingOnly: false,
		ProcessingMode:      ModeLocalFull,
		UploadScope:         ScopeAnalysis,
		RetentionDays:       MaxRetentionDays,
		RedactionLevel:      RedactionStandard,
		RequireChildConsent: false,
	}
	normalized, err := ValidateInput(valid)
	if err != nil {
		t.Fatalf("valid input rejected: %v", err)
	}
	if normalized.UploadScope != ScopeAnalysis {
		t.Fatalf("scope = %q", normalized.UploadScope)
	}

	cases := map[string]Input{
		"unknown mode": func() Input {
			bad := valid
			bad.ProcessingMode = "cloud"
			return bad
		}(),
		"unknown scope": func() Input {
			bad := valid
			bad.UploadScope = "raw"
			return bad
		}(),
		"unknown redaction": func() Input {
			bad := valid
			bad.RedactionLevel = "loose"
			return bad
		}(),
		"retention too large": func() Input {
			bad := valid
			bad.RetentionDays = MaxRetentionDays + 1
			return bad
		}(),
		"retention negative": func() Input {
			bad := valid
			bad.RetentionDays = -1
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
		Enabled:             false,
		UploadAllowed:       true,
		LocalProcessingOnly: false,
		ProcessingMode:      ModeLocalFull,
		UploadScope:         ScopeAnalysis,
		RetentionDays:       7,
		RedactionLevel:      RedactionNone,
	}
	normalized, err := ValidateInput(input)
	if err != nil {
		t.Fatalf("valid input rejected: %v", err)
	}
	if normalized.UploadAllowed ||
		normalized.UploadScope != ScopeNone ||
		normalized.ProcessingMode != ModeOff {
		t.Fatalf("disabling must clear upload and mode: %+v", normalized)
	}
}

func TestValidateInputLocalOnlyForcesUploadOff(t *testing.T) {
	input := Input{
		Enabled:             true,
		UploadAllowed:       true,
		LocalProcessingOnly: true,
		ProcessingMode:      ModeLocalFull,
		UploadScope:         ScopeAnalysis,
		RetentionDays:       7,
		RedactionLevel:      RedactionStrict,
	}
	normalized, err := ValidateInput(input)
	if err != nil {
		t.Fatalf("valid input rejected: %v", err)
	}
	if normalized.UploadAllowed || normalized.UploadScope != ScopeNone {
		t.Fatalf("local-only must force upload off: %+v", normalized)
	}
}

func TestValidateInputUnauthorisedUploadClearsScope(t *testing.T) {
	input := Input{
		Enabled:             true,
		UploadAllowed:       false,
		LocalProcessingOnly: false,
		ProcessingMode:      ModeLocalFull,
		UploadScope:         ScopeThumbnails,
		RetentionDays:       7,
		RedactionLevel:      RedactionStrict,
	}
	normalized, err := ValidateInput(input)
	if err != nil {
		t.Fatalf("valid input rejected: %v", err)
	}
	if normalized.UploadScope != ScopeNone {
		t.Fatalf("unauthorised upload must clear scope, got %q", normalized.UploadScope)
	}
}

func TestValidateStatusSupported(t *testing.T) {
	valid := StatusInput{
		CameraSupported: true,
		LocalProcessing: true,
		UploadAllowed:   true,
		ProcessingMode:  ModeLocalFull,
		UploadScope:     ScopeThumbnails,
		RedactionLevel:  RedactionStrict,
		ProcessedCount:  20,
		UploadedCount:   5,
		BlockedCount:    2,
		LastBlockReason: BlockScope,
		PolicyRevision:  2,
	}
	normalized, err := ValidateStatus(valid)
	if err != nil {
		t.Fatalf("valid status rejected: %v", err)
	}
	if normalized.UploadScope != ScopeThumbnails {
		t.Fatalf("scope = %q", normalized.UploadScope)
	}

	cases := map[string]StatusInput{
		"bad mode": func() StatusInput {
			bad := valid
			bad.ProcessingMode = "cloud"
			return bad
		}(),
		"bad scope": func() StatusInput {
			bad := valid
			bad.UploadScope = "raw"
			return bad
		}(),
		"bad redaction": func() StatusInput {
			bad := valid
			bad.RedactionLevel = "loose"
			return bad
		}(),
		"bad reason": func() StatusInput {
			bad := valid
			bad.LastBlockReason = "exploded"
			return bad
		}(),
		"too many processed": func() StatusInput {
			bad := valid
			bad.ProcessedCount = MaxImageCount + 1
			return bad
		}(),
		"uploaded over processed": func() StatusInput {
			bad := valid
			bad.ProcessedCount = 1
			bad.UploadedCount = 2
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
		CameraSupported: false,
		LocalProcessing: true,
		UploadAllowed:   true,
		ProcessingMode:  ModeLocalFull,
		UploadScope:     ScopeAnalysis,
		RedactionLevel:  RedactionNone,
		ProcessedCount:  30,
		UploadedCount:   10,
		BlockedCount:    4,
		LastBlockReason: BlockQuota,
		PolicyRevision:  1,
	}
	normalized, err := ValidateStatus(input)
	if err != nil {
		t.Fatalf("unsupported status rejected: %v", err)
	}
	if normalized.LocalProcessing ||
		normalized.UploadAllowed ||
		normalized.ProcessingMode != ModeOff ||
		normalized.UploadScope != ScopeNone ||
		normalized.RedactionLevel != RedactionStrict ||
		normalized.ProcessedCount != 0 ||
		normalized.UploadedCount != 0 ||
		normalized.BlockedCount != 0 ||
		normalized.LastBlockReason != BlockNone {
		t.Fatalf("unsupported device must report a fully off pipeline: %+v", normalized)
	}
}

func TestValidateStatusUploadOffClearsScope(t *testing.T) {
	input := StatusInput{
		CameraSupported: true,
		LocalProcessing: true,
		UploadAllowed:   false,
		ProcessingMode:  ModeLocalFull,
		UploadScope:     ScopeAnalysis,
		RedactionLevel:  RedactionStrict,
		ProcessedCount:  10,
		UploadedCount:   0,
		BlockedCount:    1,
		LastBlockReason: BlockDisabled,
		PolicyRevision:  1,
	}
	normalized, err := ValidateStatus(input)
	if err != nil {
		t.Fatalf("status rejected: %v", err)
	}
	if normalized.UploadScope != ScopeNone {
		t.Fatalf("upload off must clear scope, got %q", normalized.UploadScope)
	}
}

func TestCatalogsAreConsistent(t *testing.T) {
	for _, mode := range ProcessingModes {
		if !IsProcessingMode(mode) {
			t.Fatalf("mode %q not recognised", mode)
		}
	}
	if IsProcessingMode("cloud") {
		t.Fatalf("unexpected mode accepted")
	}
	for _, scope := range UploadScopes {
		if !IsUploadScope(scope) {
			t.Fatalf("scope %q not recognised", scope)
		}
	}
	for _, level := range RedactionLevels {
		if !IsRedactionLevel(level) {
			t.Fatalf("redaction %q not recognised", level)
		}
	}
	for _, reason := range BlockReasons {
		if !IsBlockReason(reason) {
			t.Fatalf("reason %q not recognised", reason)
		}
	}
	if IsBlockReason("exploded") {
		t.Fatalf("unexpected reason accepted")
	}
}
