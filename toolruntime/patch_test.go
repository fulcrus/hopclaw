package toolruntime

import (
	"testing"
)

func TestParseUnifiedPatchSimple(t *testing.T) {
	t.Parallel()

	input := `--- a/file.go
+++ b/file.go
@@ -1,3 +1,3 @@
 line one
-line two
+line TWO
 line three
`
	patch, err := parseUnifiedPatch(input)
	if err != nil {
		t.Fatalf("parseUnifiedPatch error: %v", err)
	}
	if len(patch.Files) != 1 {
		t.Fatalf("len(Files) = %d, want 1", len(patch.Files))
	}
	if patch.Files[0].OldPath != "a/file.go" {
		t.Fatalf("OldPath = %q", patch.Files[0].OldPath)
	}
	if patch.Files[0].NewPath != "b/file.go" {
		t.Fatalf("NewPath = %q", patch.Files[0].NewPath)
	}
	if len(patch.Files[0].Hunks) != 1 {
		t.Fatalf("len(Hunks) = %d, want 1", len(patch.Files[0].Hunks))
	}
}

func TestParseUnifiedPatchNoDiff(t *testing.T) {
	t.Parallel()

	_, err := parseUnifiedPatch("no diff content here")
	if err == nil {
		t.Fatal("expected error for input with no diff")
	}
}

func TestParsePatchPathDevNull(t *testing.T) {
	t.Parallel()

	path, err := parsePatchPath("--- /dev/null", "--- ")
	if err != nil {
		t.Fatalf("parsePatchPath error: %v", err)
	}
	if path != "" {
		t.Fatalf("path = %q, want empty for /dev/null", path)
	}
}

func TestParsePatchPathNormal(t *testing.T) {
	t.Parallel()

	path, err := parsePatchPath("+++ b/src/main.go", "+++ ")
	if err != nil {
		t.Fatalf("parsePatchPath error: %v", err)
	}
	if path != "b/src/main.go" {
		t.Fatalf("path = %q, want %q", path, "b/src/main.go")
	}
}

func TestParsePatchPathInvalidPrefix(t *testing.T) {
	t.Parallel()

	_, err := parsePatchPath("invalid line", "--- ")
	if err == nil {
		t.Fatal("expected error for invalid prefix")
	}
}

func TestStripPatchPath(t *testing.T) {
	t.Parallel()

	tests := []struct {
		path  string
		strip int
		want  string
		err   bool
	}{
		{"a/src/main.go", 1, "src/main.go", false},
		{"b/src/main.go", 1, "src/main.go", false},
		{"src/main.go", 0, "src/main.go", false},
		{"", 0, "", false},
		{"a/b/c", 5, "", true},
	}

	for _, tt := range tests {
		got, err := stripPatchPath(tt.path, tt.strip)
		if tt.err && err == nil {
			t.Fatalf("stripPatchPath(%q, %d) expected error", tt.path, tt.strip)
		}
		if !tt.err && err != nil {
			t.Fatalf("stripPatchPath(%q, %d) error: %v", tt.path, tt.strip, err)
		}
		if !tt.err && got != tt.want {
			t.Fatalf("stripPatchPath(%q, %d) = %q, want %q", tt.path, tt.strip, got, tt.want)
		}
	}
}

func TestStripPatchPathNegativeStrip(t *testing.T) {
	t.Parallel()

	_, err := stripPatchPath("a/file.go", -1)
	if err == nil {
		t.Fatal("expected error for negative strip")
	}
}

func TestSplitTextLines(t *testing.T) {
	t.Parallel()

	lines := splitTextLines("line1\nline2\nline3\n")
	if len(lines) != 3 {
		t.Fatalf("len(lines) = %d, want 3", len(lines))
	}
	if lines[0].Text != "line1" || !lines[0].HasNewline {
		t.Fatalf("lines[0] = %+v", lines[0])
	}
	if lines[2].Text != "line3" || !lines[2].HasNewline {
		t.Fatalf("lines[2] = %+v", lines[2])
	}
}

func TestSplitTextLinesNoTrailingNewline(t *testing.T) {
	t.Parallel()

	lines := splitTextLines("line1\nline2")
	if len(lines) != 2 {
		t.Fatalf("len(lines) = %d, want 2", len(lines))
	}
	if lines[1].HasNewline {
		t.Fatal("last line should not have newline")
	}
}

func TestSplitTextLinesEmpty(t *testing.T) {
	t.Parallel()

	lines := splitTextLines("")
	if lines != nil {
		t.Fatalf("expected nil for empty input, got %v", lines)
	}
}

func TestJoinTextLines(t *testing.T) {
	t.Parallel()

	lines := []textLine{
		{Text: "hello", HasNewline: true},
		{Text: "world", HasNewline: false},
	}
	result := string(joinTextLines(lines))
	if result != "hello\nworld" {
		t.Fatalf("joinTextLines = %q, want %q", result, "hello\nworld")
	}
}

func TestDisplayPatchPath(t *testing.T) {
	t.Parallel()

	if displayPatchPath("source.go", "target.go") != "target.go" {
		t.Fatal("should prefer target path")
	}
	if displayPatchPath("source.go", "") != "source.go" {
		t.Fatal("should fall back to source path")
	}
}

func TestContainsString(t *testing.T) {
	t.Parallel()

	if !containsString([]string{"a", "b", "c"}, "b") {
		t.Fatal("expected true for existing element")
	}
	if containsString([]string{"a", "b", "c"}, "d") {
		t.Fatal("expected false for missing element")
	}
	if containsString(nil, "a") {
		t.Fatal("expected false for nil slice")
	}
}
