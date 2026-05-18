package update

import (
	"testing"
	"time"
)

func TestGitHubReleasesAPIURLHonorsEnvOverride(t *testing.T) {
	t.Setenv("HOPCLAW_UPDATE_API_URL", "http://127.0.0.1:18080/releases")

	got := githubReleasesAPIURL()
	if got != "http://127.0.0.1:18080/releases" {
		t.Fatalf("githubReleasesAPIURL() = %q, want env override", got)
	}
}

func TestNormalizePolicyDisableManifestClearsManifestURL(t *testing.T) {
	got := normalizePolicy(Policy{DisableManifest: true})
	if !got.DisableManifest {
		t.Fatal("DisableManifest = false, want true")
	}
	if got.ManifestURL != "" {
		t.Fatalf("ManifestURL = %q, want empty when manifest is disabled", got.ManifestURL)
	}
}

func TestNormalizeVersion(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"v1.2.3", "0001.0002.0003"},
		{"1.2.3", "0001.0002.0003"},
		{"0.10.5", "0000.0010.0005"},
		{"v2.0.0-rc1", "0002.0000.0000"},
		{"v1.0.0+build123", "0001.0000.0000"},
		{"invalid", ""},
		{"1.2", ""},
		{"dev", ""},
	}

	for _, tt := range tests {
		got := normalizeVersion(tt.input)
		if got != tt.want {
			t.Errorf("normalizeVersion(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestIsNewer(t *testing.T) {
	tests := []struct {
		current string
		latest  string
		want    bool
	}{
		{"v1.0.0", "v1.0.1", true},
		{"v1.0.0", "v1.1.0", true},
		{"v1.0.0", "v2.0.0", true},
		{"v1.0.1", "v1.0.0", false},
		{"v1.0.0", "v1.0.0", false},
		{"v0.9.0", "v0.10.0", true},
		{"2026.3.18-beta.1", "2026.3.18-beta.2", true},
		{"2026.3.18-nightly.20260318.1", "2026.3.18-nightly.20260318.2", true},
		{"2026.3.18-beta.2", "2026.3.18", true},
		{"dev", "v1.0.0", false},       // dev build — skip
		{"v1.0.0", "dev", false},       // bad latest — skip
		{"v1.0.0-rc1", "v1.0.0", true}, // stable is newer than rc on the same base
	}

	for _, tt := range tests {
		got := isNewer(tt.current, tt.latest)
		if got != tt.want {
			t.Errorf("isNewer(%q, %q) = %v, want %v", tt.current, tt.latest, got, tt.want)
		}
	}
}

func TestSameVersionRespectsPrereleaseSuffix(t *testing.T) {
	if sameVersion("2026.3.18-beta.1", "2026.3.18-beta.2") {
		t.Fatal("beta.1 and beta.2 should not be treated as the same version")
	}
	if !sameVersion("v2026.3.18-beta.2", "2026.3.18-beta.2") {
		t.Fatal("version prefix normalization should preserve exact prerelease match")
	}
}

func TestCheckState_RoundTrip(t *testing.T) {
	// Use a temp dir as HOME to avoid polluting real state.
	t.Setenv("HOME", t.TempDir())

	state := checkState{
		LatestVersion: "v1.2.3",
		UpdateURL:     "https://example.com/release",
	}

	if err := saveCheckState(state); err != nil {
		t.Fatalf("saveCheckState() error: %v", err)
	}

	loaded, err := loadCheckState()
	if err != nil {
		t.Fatalf("loadCheckState() error: %v", err)
	}

	if loaded.LatestVersion != state.LatestVersion {
		t.Errorf("LatestVersion = %q, want %q", loaded.LatestVersion, state.LatestVersion)
	}
	if loaded.UpdateURL != state.UpdateURL {
		t.Errorf("UpdateURL = %q, want %q", loaded.UpdateURL, state.UpdateURL)
	}
}

func TestSelectManifestRelease(t *testing.T) {
	manifest := releaseManifestFile{
		Channels: map[string]manifestChannel{
			"stable": {
				Latest: "2026.3.17",
				Releases: []releaseManifest{
					{Version: "2026.3.16", Channel: "stable", PublishedAt: "2026-03-16T09:00:00Z"},
					{Version: "2026.3.17", Channel: "stable", PublishedAt: "2026-03-17T09:00:00Z"},
				},
			},
			"beta": {
				Latest: "2026.3.18-beta.1",
				Releases: []releaseManifest{
					{Version: "2026.3.18-beta.1", Channel: "beta", PublishedAt: "2026-03-18T09:00:00Z"},
				},
			},
		},
	}

	stable, err := selectManifestRelease(manifest, "stable", "")
	if err != nil {
		t.Fatalf("selectManifestRelease(stable) error = %v", err)
	}
	if stable.Version != "2026.3.17" {
		t.Fatalf("stable.Version = %q, want 2026.3.17", stable.Version)
	}

	beta, err := selectManifestRelease(manifest, "beta", "")
	if err != nil {
		t.Fatalf("selectManifestRelease(beta) error = %v", err)
	}
	if beta.Version != "2026.3.18-beta.1" {
		t.Fatalf("beta.Version = %q, want 2026.3.18-beta.1", beta.Version)
	}

	pinned, err := selectManifestRelease(manifest, "stable", "2026.3.16")
	if err != nil {
		t.Fatalf("selectManifestRelease(version) error = %v", err)
	}
	if pinned.Version != "2026.3.16" {
		t.Fatalf("pinned.Version = %q, want 2026.3.16", pinned.Version)
	}
}

func TestCompareReleaseOrderUsesVersionThenPublishedAt(t *testing.T) {
	if got := compareReleaseOrder("2026.3.17", "2026-03-17T09:00:00Z", "2026.3.16", "2026-03-18T09:00:00Z"); got <= 0 {
		t.Fatalf("compareReleaseOrder newer version = %d, want > 0", got)
	}
	if got := compareReleaseOrder("2026.3.17", "2026-03-17T09:00:00Z", "2026.3.17", "2026-03-16T09:00:00Z"); got <= 0 {
		t.Fatalf("compareReleaseOrder newer published_at = %d, want > 0", got)
	}
	if got := compareReleaseOrder("2026.3.18", "2026-03-18T09:00:00Z", "2026.3.18-beta.2", "2026-03-18T10:00:00Z"); got <= 0 {
		t.Fatalf("compareReleaseOrder stable over beta = %d, want > 0", got)
	}
	if got := compareReleaseOrder("2026.3.18-beta.2", "2026-03-18T09:00:00Z", "2026.3.18-beta.1", "2026-03-18T10:00:00Z"); got <= 0 {
		t.Fatalf("compareReleaseOrder beta sequence = %d, want > 0", got)
	}
	if got := compareReleaseOrder("2026.3.18-nightly.20260318.2", "2026-03-18T09:00:00Z", "2026.3.18-nightly.20260318.1", "2026-03-18T10:00:00Z"); got <= 0 {
		t.Fatalf("compareReleaseOrder nightly sequence = %d, want > 0", got)
	}
}

func TestCheckStateFromResult(t *testing.T) {
	now := time.Now().UTC()
	state := checkStateFromResult(&CheckResult{
		CurrentVersion: "2026.3.16",
		LatestVersion:  "2026.3.17",
		LatestChannel:  "stable",
		UpdateURL:      "https://example.com/release",
		Notes:          "summary",
		CheckedAt:      now,
		PublishedAt:    now,
	}, Policy{Channel: "stable", SkipVersion: "2026.3.15"})
	if state.Channel != "stable" {
		t.Fatalf("state.Channel = %q, want stable", state.Channel)
	}
	if state.SkipVersion != "2026.3.15" {
		t.Fatalf("state.SkipVersion = %q", state.SkipVersion)
	}
}
