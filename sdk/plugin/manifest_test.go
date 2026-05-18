package plugin

import "testing"

func TestNewManifestTrimsFields(t *testing.T) {
	t.Parallel()

	manifest := NewManifest(" demo ", " 1.0.0 ", " Example plugin ")
	if manifest.Name != "demo" {
		t.Fatalf("Name = %q, want demo", manifest.Name)
	}
	if manifest.Version != "1.0.0" {
		t.Fatalf("Version = %q, want 1.0.0", manifest.Version)
	}
	if manifest.Description != "Example plugin" {
		t.Fatalf("Description = %q, want Example plugin", manifest.Description)
	}
}

func TestManifestCloneIsolated(t *testing.T) {
	t.Parallel()

	original := Manifest{
		Name: "demo",
		Channels: map[string]ChannelDecl{
			"demo": {
				Config: map[string]any{"mode": "test"},
			},
		},
	}

	cloned := original.Clone()
	channel := cloned.Channels["demo"]
	channel.Config["mode"] = "changed"
	cloned.Channels["demo"] = channel

	if got := original.Channels["demo"].Config["mode"]; got != "test" {
		t.Fatalf("original mutated, mode = %#v", got)
	}
}

func TestManifestSkillRootsDedupesAndTrims(t *testing.T) {
	t.Parallel()

	manifest := Manifest{
		SkillsDir:  " skills ",
		SkillsDirs: []string{"skills", "skills/custom", " ", "skills/custom"},
	}

	roots := manifest.SkillRoots()
	if len(roots) != 2 {
		t.Fatalf("len(SkillRoots()) = %d, want 2 (%#v)", len(roots), roots)
	}
	if roots[0] != "skills" || roots[1] != "skills/custom" {
		t.Fatalf("SkillRoots() = %#v", roots)
	}
	if manifest.PrimarySkillDir() != "skills" {
		t.Fatalf("PrimarySkillDir() = %q, want skills", manifest.PrimarySkillDir())
	}
}
