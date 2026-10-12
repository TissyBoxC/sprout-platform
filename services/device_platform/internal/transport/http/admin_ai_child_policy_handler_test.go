package http

import (
	"testing"

	gatewaydomain "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/ai_gateway/domain"
)

func TestAIGatewayChildPolicyResponseOmitsSafetyBoundaryText(t *testing.T) {
	alignment := &gatewaydomain.ChildPolicyAlignment{
		Enabled:              true,
		RequirePolicyVersion: true,
		InjectSafetyBoundary: true,
		Aligned:              true,
		AgeBands: []gatewaydomain.ProviderAgeBandPolicy{
			{
				Name:            "6-8",
				AllowedModels:   []string{"gpt-5-mini"},
				MaxOutputTokens: 1536,
				ContentRating:   "G",
			},
		},
		Tiers: []gatewaydomain.ChildPolicyTierAlignment{
			{
				AgeTier:         "age_5_6",
				AgeBand:         "6-8",
				Bound:           true,
				AllowedModels:   []string{"gpt-5-mini"},
				MaxOutputTokens: 1536,
				ContentRating:   "G",
			},
		},
	}

	payload := childPolicyAlignmentResponse(alignment)
	if payload["enabled"] != true || payload["aligned"] != true {
		t.Fatalf("unexpected flags: %#v", payload)
	}
	if _, exists := payload["safety_boundary"]; exists {
		t.Fatal("response must never include the safety-boundary text")
	}
	tiers, ok := payload["tiers"].([]map[string]any)
	if !ok || len(tiers) != 1 {
		t.Fatalf("unexpected tiers payload: %#v", payload["tiers"])
	}
	if tiers[0]["age_tier"] != "age_5_6" || tiers[0]["bound"] != true {
		t.Fatalf("unexpected tier payload: %#v", tiers[0])
	}
	bands, ok := payload["bands"].([]map[string]any)
	if !ok || len(bands) != 1 || bands[0]["name"] != "6-8" {
		t.Fatalf("unexpected bands payload: %#v", payload["bands"])
	}
}

func TestAIGatewayChildPolicyResponseHandlesNilAlignment(t *testing.T) {
	payload := childPolicyAlignmentResponse(nil)
	if payload["enabled"] != false || payload["aligned"] != false {
		t.Fatalf("expected a disabled payload for a nil policy: %#v", payload)
	}
	if tiers, ok := payload["tiers"].([]map[string]any); !ok || len(tiers) != 0 {
		t.Fatalf("expected an empty tier list: %#v", payload["tiers"])
	}
}
