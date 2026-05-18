package toolruntime

import (
	"testing"

	"github.com/fulcrus/hopclaw/config"
)

func TestDisabledGroupsFromConfigUsesRegisteredGroupToggles(t *testing.T) {
	t.Parallel()

	disabled := false
	enabled := true
	got := DisabledGroupsFromConfig(config.Layer2Config{
		Git:      &disabled,
		Media:    &disabled,
		Calendar: &enabled,
	})

	for _, name := range []string{"git", "git-write", "media", "media-go"} {
		if !got[name] {
			t.Fatalf("DisabledGroupsFromConfig() missing %q in %#v", name, got)
		}
	}
	if got["calendar"] {
		t.Fatalf("DisabledGroupsFromConfig() unexpectedly disabled calendar: %#v", got)
	}
}
