package cli

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	"github.com/fulcrus/hopclaw/internal/update"
)

func TestRunUpdateReportsAvailableReleaseWithoutDownloading(t *testing.T) {
	original := updateCheckWithPolicy
	updateCheckWithPolicy = func(_ context.Context, policy update.Policy) (*update.CheckResult, error) {
		if !policy.DisableManifest {
			t.Fatalf("policy.DisableManifest = false, want true")
		}
		return &update.CheckResult{
			CurrentVersion: "v1.0.0",
			CurrentChannel: "stable",
			LatestVersion:  "v1.1.0",
			LatestChannel:  "stable",
			UpdateURL:      "https://github.com/fulcrus/hopclaw/releases/tag/v1.1.0",
			Notes:          "Bug fixes and improvements.",
			PublishedAt:    time.Date(2026, 4, 1, 8, 0, 0, 0, time.UTC),
		}, nil
	}
	defer func() { updateCheckWithPolicy = original }()

	cmd := newUpdateCmd()
	var stdout bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&stdout)

	if err := runUpdate(cmd, nil); err != nil {
		t.Fatalf("runUpdate() error = %v", err)
	}

	output := stdout.String()
	if !strings.Contains(output, "Source: GitHub releases API") {
		t.Fatalf("output missing source: %q", output)
	}
	if !strings.Contains(output, "Latest version: v1.1.0") {
		t.Fatalf("output missing latest version: %q", output)
	}
	if !strings.Contains(output, "Download: https://github.com/fulcrus/hopclaw/releases/tag/v1.1.0") {
		t.Fatalf("output missing download URL: %q", output)
	}
	if !strings.Contains(output, "A newer release is available.") {
		t.Fatalf("output missing advisory guidance: %q", output)
	}
}

func TestRunUpdateCheckFlagSuppressesAdvisoryLine(t *testing.T) {
	original := updateCheckWithPolicy
	updateCheckWithPolicy = func(context.Context, update.Policy) (*update.CheckResult, error) {
		return &update.CheckResult{
			CurrentVersion: "v1.0.0",
			CurrentChannel: "stable",
			LatestVersion:  "v1.1.0",
			LatestChannel:  "stable",
			UpdateURL:      "https://github.com/fulcrus/hopclaw/releases/tag/v1.1.0",
		}, nil
	}
	defer func() { updateCheckWithPolicy = original }()

	cmd := newUpdateCmd()
	var stdout bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&stdout)
	cmd.SetArgs([]string{"--check"})
	if err := cmd.Flags().Set("check", "true"); err != nil {
		t.Fatalf("Flags().Set(check) error = %v", err)
	}

	if err := runUpdate(cmd, nil); err != nil {
		t.Fatalf("runUpdate() error = %v", err)
	}

	output := stdout.String()
	if strings.Contains(output, "A newer release is available.") {
		t.Fatalf("unexpected advisory guidance in --check output: %q", output)
	}
}

func TestRunUpdateRejectsUnsupportedCompatibilityFlags(t *testing.T) {
	original := updateCheckWithPolicy
	updateCheckWithPolicy = func(context.Context, update.Policy) (*update.CheckResult, error) {
		t.Fatal("updateCheckWithPolicy should not run when unsupported flags are set")
		return nil, nil
	}
	defer func() { updateCheckWithPolicy = original }()

	cmd := newUpdateCmd()
	var stdout bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&stdout)
	if err := cmd.Flags().Set("yes", "true"); err != nil {
		t.Fatalf("Flags().Set(yes) error = %v", err)
	}
	if err := cmd.Flags().Set("no-restart", "true"); err != nil {
		t.Fatalf("Flags().Set(no-restart) error = %v", err)
	}
	if err := cmd.Flags().Set("version", "v9.9.9"); err != nil {
		t.Fatalf("Flags().Set(version) error = %v", err)
	}

	err := runUpdate(cmd, nil)
	if err == nil {
		t.Fatal("runUpdate() error = nil, want unsupported-flag guidance")
	}
	for _, want := range []string{"--yes", "--no-restart", "--version", "advisory-only mode"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("error = %q, want %q", err, want)
		}
	}
}
