package repository

import (
	"testing"

	"github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/operations/domain"
)

func TestPreferredAppUpdateReleaseKeepsSameVersionArtifactsDistinct(t *testing.T) {
	apk := &domain.Release{
		Version:  "0.29.0",
		Kind:     domain.ReleaseKindClient,
		Platform: "android",
	}
	adminWeb := &domain.Release{
		Version:  "0.29.0",
		Kind:     domain.ReleaseKindClient,
		Platform: "all",
	}

	selected := preferredAppUpdateRelease(apk, adminWeb)
	if selected != apk {
		t.Fatal("an all-platform package must not replace an exact platform artifact")
	}

	selected = preferredAppUpdateRelease(adminWeb, apk)
	if selected != apk {
		t.Fatal("an exact platform artifact should win regardless of row order")
	}
}

func TestPreferredAppUpdateReleaseUsesHighestVersion(t *testing.T) {
	current := &domain.Release{
		Version:  "0.28.0",
		Kind:     domain.ReleaseKindClient,
		Platform: "android",
	}
	candidate := &domain.Release{
		Version:  "0.29.0",
		Kind:     domain.ReleaseKindClient,
		Platform: "all",
	}

	if selected := preferredAppUpdateRelease(current, candidate); selected != candidate {
		t.Fatal("expected the newer artifact")
	}
}
