package exec

import (
	"testing"
	"time"
)

func TestToolDefsSmoke(t *testing.T) {
	t.Parallel()

	defs := ToolDefs(5 * time.Second)
	if len(defs) == 0 {
		t.Fatal("ToolDefs() returned no tool definitions")
	}
	for _, def := range defs {
		if def.Manifest.Name == "" {
			t.Fatal("ToolDefs() returned a tool definition with an empty manifest name")
		}
		if def.Handler == nil {
			t.Fatalf("ToolDefs() returned %q without a handler", def.Manifest.Name)
		}
	}
}

func TestExtraToolDefsSmoke(t *testing.T) {
	t.Parallel()

	defs := ExtraToolDefs(5 * time.Second)
	if len(defs) == 0 {
		t.Fatal("ExtraToolDefs() returned no tool definitions")
	}
	for _, def := range defs {
		if def.Manifest.Name == "" {
			t.Fatal("ExtraToolDefs() returned a tool definition with an empty manifest name")
		}
		if def.Handler == nil {
			t.Fatalf("ExtraToolDefs() returned %q without a handler", def.Manifest.Name)
		}
	}
}
