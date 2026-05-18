package version

import "testing"

func TestFull_DevDefault(t *testing.T) {
	// Reset to defaults.
	Version = "dev"
	GitCommit = ""
	BuildDate = ""

	got := Full()
	if got != "dev" {
		t.Errorf("Full() = %q, want %q", got, "dev")
	}
}

func TestFull_WithCommit(t *testing.T) {
	Version = "v1.2.3"
	GitCommit = "abc1234"
	BuildDate = ""
	defer func() {
		Version = "dev"
		GitCommit = ""
	}()

	got := Full()
	want := "v1.2.3 (abc1234)"
	if got != want {
		t.Errorf("Full() = %q, want %q", got, want)
	}
}

func TestFull_WithCommitAndDate(t *testing.T) {
	Version = "v1.0.0"
	GitCommit = "def5678"
	BuildDate = "2026-01-15T12:00:00Z"
	defer func() {
		Version = "dev"
		GitCommit = ""
		BuildDate = ""
	}()

	got := Full()
	want := "v1.0.0 (def5678) built 2026-01-15T12:00:00Z"
	if got != want {
		t.Errorf("Full() = %q, want %q", got, want)
	}
}
