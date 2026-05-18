package toolruntime

import (
	"strings"
	"testing"
)

func TestComputeDiffIdentical(t *testing.T) {
	t.Parallel()
	result := computeDiff("a", "b", "hello\nworld\n", "hello\nworld\n")
	if result != "" {
		t.Fatalf("expected empty diff for identical input, got:\n%s", result)
	}
}

func TestComputeDiffSimpleChange(t *testing.T) {
	t.Parallel()
	old := "line1\nline2\nline3\n"
	new := "line1\nchanged\nline3\n"
	result := computeDiff("a/file.txt", "b/file.txt", old, new)

	if !strings.Contains(result, "--- a/file.txt") {
		t.Fatalf("missing old header:\n%s", result)
	}
	if !strings.Contains(result, "+++ b/file.txt") {
		t.Fatalf("missing new header:\n%s", result)
	}
	if !strings.Contains(result, "-line2") {
		t.Fatalf("missing deleted line:\n%s", result)
	}
	if !strings.Contains(result, "+changed") {
		t.Fatalf("missing added line:\n%s", result)
	}
}

func TestComputeDiffAddLines(t *testing.T) {
	t.Parallel()
	old := "a\nb\n"
	new := "a\nb\nc\nd\n"
	result := computeDiff("old", "new", old, new)

	if !strings.Contains(result, "+c") {
		t.Fatalf("missing added line c:\n%s", result)
	}
	if !strings.Contains(result, "+d") {
		t.Fatalf("missing added line d:\n%s", result)
	}
}

func TestComputeDiffDeleteLines(t *testing.T) {
	t.Parallel()
	old := "a\nb\nc\nd\n"
	new := "a\nd\n"
	result := computeDiff("old", "new", old, new)

	if !strings.Contains(result, "-b") {
		t.Fatalf("missing deleted line b:\n%s", result)
	}
	if !strings.Contains(result, "-c") {
		t.Fatalf("missing deleted line c:\n%s", result)
	}
}

func TestComputeDiffEmptyToContent(t *testing.T) {
	t.Parallel()
	result := computeDiff("old", "new", "", "new content\n")
	if !strings.Contains(result, "+new content") {
		t.Fatalf("missing added content:\n%s", result)
	}
}

func TestComputeDiffContentToEmpty(t *testing.T) {
	t.Parallel()
	result := computeDiff("old", "new", "old content\n", "")
	if !strings.Contains(result, "-old content") {
		t.Fatalf("missing deleted content:\n%s", result)
	}
}

func TestComputeDiffMultipleHunks(t *testing.T) {
	t.Parallel()
	// Create a file with changes far apart to get multiple hunks.
	var oldLines, newLines []string
	for i := 0; i < 20; i++ {
		line := strings.Repeat("x", 1) + string(rune('a'+i))
		oldLines = append(oldLines, line)
		newLines = append(newLines, line)
	}
	// Change line 2 and line 18 (0-indexed) to force two separate hunks.
	newLines[1] = "CHANGED1"
	newLines[17] = "CHANGED2"

	old := strings.Join(oldLines, "\n") + "\n"
	new := strings.Join(newLines, "\n") + "\n"

	result := computeDiff("old", "new", old, new)
	hunkCount := strings.Count(result, "@@ ")
	if hunkCount != 2 {
		t.Fatalf("expected 2 hunks, got %d:\n%s", hunkCount, result)
	}
}

func TestDiffLinesSymmetry(t *testing.T) {
	t.Parallel()
	// The diff of A→B reversed should have opposite operations.
	old := splitLines("alpha\nbeta\ngamma\n")
	new := splitLines("alpha\nBETA\ngamma\n")

	forward := diffLines(old, new)
	backward := diffLines(new, old)

	countKind := func(edits []editEntry, kind byte) int {
		n := 0
		for _, e := range edits {
			if e.kind == kind {
				n++
			}
		}
		return n
	}

	if countKind(forward, '-') != countKind(backward, '+') {
		t.Fatal("forward deletes should equal backward inserts")
	}
	if countKind(forward, '+') != countKind(backward, '-') {
		t.Fatal("forward inserts should equal backward deletes")
	}
}

func TestSplitLines(t *testing.T) {
	t.Parallel()
	tests := []struct {
		input string
		want  int
	}{
		{"", 0},
		{"a\n", 1},
		{"a\nb\n", 2},
		{"a\nb", 2}, // no trailing newline
	}
	for _, tt := range tests {
		got := splitLines(tt.input)
		if len(got) != tt.want {
			t.Errorf("splitLines(%q) = %d lines, want %d", tt.input, len(got), tt.want)
		}
	}
}
