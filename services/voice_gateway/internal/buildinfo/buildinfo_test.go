package buildinfo

import "testing"

func TestCurrentFallsBackToDevelopmentVersion(t *testing.T) {
	previous := Version
	defer func() { Version = previous }()

	Version = ""
	if got := Current(); got != "dev" {
		t.Fatalf("Current() = %q, want dev", got)
	}

	Version = "0.32.0"
	if got := Current(); got != "0.32.0" {
		t.Fatalf("Current() = %q, want 0.32.0", got)
	}
}
