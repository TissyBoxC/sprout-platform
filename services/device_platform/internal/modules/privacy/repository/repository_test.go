package repository

import (
	"bytes"
	"os"
	"testing"
)

// TestPurgeParentDataCoversRecentFeatureTables guards the account-deletion
// contract for modules whose policy and device-status tables were added after
// the original purge implementation. A missing DELETE here leaves child-linked
// policy or device state behind after the guardian account is anonymized.
func TestPurgeParentDataCoversRecentFeatureTables(t *testing.T) {
	source, err := os.ReadFile("repository.go")
	if err != nil {
		t.Fatalf("read repository.go: %v", err)
	}
	required := []string{
		"DELETE FROM video_session_policies",
		"DELETE FROM device_video_session_status",
		"DELETE FROM bluetooth_audio_policies",
		"DELETE FROM device_bluetooth_audio_status",
		"DELETE FROM learning_visualization_policies",
		"DELETE FROM device_learning_visualization_status",
		"DELETE FROM image_privacy_policies",
		"DELETE FROM device_image_privacy_status",
	}
	for _, fragment := range required {
		if !bytes.Contains(source, []byte(fragment)) {
			t.Fatalf("PurgeParentData is missing %q", fragment)
		}
	}
}
