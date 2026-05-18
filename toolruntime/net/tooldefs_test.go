package net

import "testing"

func TestToolDefsSmoke(t *testing.T) {
	t.Parallel()

	defs := ToolDefs()
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
