package http

import (
	"testing"
	"time"

	"github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/ota/domain"
)

func TestValidateOTAReleaseMutationRequiresCurrentSignaturePayloadVersion(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		version   string
		releaseID string
		wantErr   bool
	}{
		{name: "current", version: domain.OTAReleaseSignaturePayloadVersion, releaseID: "00000000-0000-4000-8000-000000000001"},
		{name: "missing", version: "", wantErr: true},
		{name: "legacy digest-only", version: "1", wantErr: true},
		{name: "arbitrary", version: "3", wantErr: true},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			err := validateOTAReleaseMutation(otaReleaseMutation{
				ReleaseID:               test.releaseID,
				SignaturePayloadVersion: test.version,
			})
			if (err != nil) != test.wantErr {
				t.Fatalf("validateOTAReleaseMutation() error = %v, wantErr %v", err, test.wantErr)
			}
		})
	}
}

func TestDeviceReleaseResponseIncludesPublishedAt(t *testing.T) {
	t.Parallel()
	publishedAt := time.Date(2026, 10, 18, 8, 9, 10, 0, time.FixedZone("SGT", 8*60*60))
	response := otaDeviceReleaseResponse(domain.DeviceRelease{
		ReleaseID:   "00000000-0000-4000-8000-000000000001",
		Version:     "0.13.0",
		PublishedAt: &publishedAt,
	})
	if response["published_at"] != "2026-10-18T00:09:10Z" {
		t.Fatalf("published_at = %v", response["published_at"])
	}
}

func TestOTAReleaseMutationRequiresCanonicalReleaseUUID(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		releaseID string
		wantErr   bool
	}{
		{name: "empty draft id", wantErr: true},
		{name: "canonical", releaseID: "00000000-0000-4000-8000-000000000001"},
		{name: "uppercase", releaseID: "00000000-0000-4000-8000-00000000000A", wantErr: true},
		{name: "not uuid", releaseID: "release_1", wantErr: true},
		{name: "nil uuid", releaseID: "00000000-0000-0000-0000-000000000000", wantErr: true},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			err := validateOTAReleaseMutation(otaReleaseMutation{
				ReleaseID:               test.releaseID,
				SignaturePayloadVersion: domain.OTAReleaseSignaturePayloadVersion,
			})
			if (err != nil) != test.wantErr {
				t.Fatalf("validateOTAReleaseMutation() error = %v, wantErr %v", err, test.wantErr)
			}
		})
	}
}
