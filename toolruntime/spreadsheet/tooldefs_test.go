package spreadsheet

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

func TestXLSXToolDefsSmoke(t *testing.T) {
	t.Parallel()

	defs := XLSXToolDefs()
	if len(defs) == 0 {
		t.Fatal("XLSXToolDefs() returned no tool definitions")
	}
	for _, def := range defs {
		if def.Manifest.Name == "" {
			t.Fatal("XLSXToolDefs() returned a tool definition with an empty manifest name")
		}
		if def.Handler == nil {
			t.Fatalf("XLSXToolDefs() returned %q without a handler", def.Manifest.Name)
		}
	}
}
