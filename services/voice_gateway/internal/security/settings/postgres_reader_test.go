package settings

import "testing"

func TestSafetyPolicyAlwaysEnablesCrisisIntervention(t *testing.T) {
	policy, err := decodeSafetyPolicy([]byte(`{
		"safety": {
			"output_moderation_enabled": false,
			"crisis_intervention_enabled": false,
			"minor_mode_default": true
		}
	}`))
	if err != nil {
		t.Fatalf("decodeSafetyPolicy() error = %v", err)
	}
	if policy.OutputModerationEnabled {
		t.Fatal("output moderation = true, want the stored false value")
	}
	if !policy.CrisisInterventionEnabled {
		t.Fatal("crisis intervention = false, want the mandatory true value")
	}
	if !policy.MinorModeDefault {
		t.Fatal("minor mode default = false, want the enforced true value")
	}
}

func TestSafetyPolicyAlwaysEnablesMinorMode(t *testing.T) {
	policy, err := decodeSafetyPolicy([]byte(`{
		"safety": {
			"minor_mode_default": false,
			"output_moderation_enabled": true,
			"crisis_intervention_enabled": false
		}
	}`))
	if err != nil {
		t.Fatalf("decodeSafetyPolicy() error = %v", err)
	}
	if !policy.MinorModeDefault {
		t.Fatal("minor mode default = false, want the enforced true value")
	}
}

func TestSafetyPolicyRejectsMalformedDocument(t *testing.T) {
	if _, err := decodeSafetyPolicy([]byte(`not-json`)); err == nil {
		t.Fatal("decodeSafetyPolicy() error = nil, want a decode error")
	}
}
