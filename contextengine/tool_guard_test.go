package contextengine

import (
	"fmt"
	"strings"
	"testing"
)

func TestCompactToolOutput_JSON(t *testing.T) {
	t.Parallel()

	input := `{"alpha":"` + strings.Repeat("x", 600) + `","beta":[1,2,3,4,5],"gamma":{"ok":true},"delta":"tail"}`
	got := CompactToolOutput(input, 10)

	if got == input {
		t.Fatal("expected JSON output to be compacted")
	}
	if !strings.Contains(got, "[tool output compacted: json]") {
		t.Fatalf("compacted JSON missing marker: %q", got)
	}
	if !strings.Contains(got, "Top-level keys: alpha, beta, delta, gamma") {
		t.Fatalf("compacted JSON missing key summary: %q", got)
	}
	if !strings.Contains(got, "... (4 items total") {
		t.Fatalf("compacted JSON missing total item count: %q", got)
	}
}

func TestCompactToolOutput_HTML(t *testing.T) {
	t.Parallel()

	input := `<!doctype html><html><head><title>Example Page</title><meta name="description" content="Useful summary"><style>.x{display:none}</style><script>window.big="` +
		strings.Repeat("z", 500) +
		`"</script></head><body><h1>Main Heading</h1><h2>Details</h2><p>` +
		strings.Repeat("content ", 120) +
		`</p></body></html>`

	got := CompactToolOutput(input, 10)

	if got == input {
		t.Fatal("expected HTML output to be compacted")
	}
	if !strings.Contains(got, "[tool output compacted: html]") {
		t.Fatalf("compacted HTML missing marker: %q", got)
	}
	if !strings.Contains(got, "Title: Example Page") {
		t.Fatalf("compacted HTML missing title: %q", got)
	}
	if !strings.Contains(got, "Description: Useful summary") {
		t.Fatalf("compacted HTML missing description: %q", got)
	}
	if !strings.Contains(got, "- Main Heading") || !strings.Contains(got, "- Details") {
		t.Fatalf("compacted HTML missing headings: %q", got)
	}
	if strings.Contains(got, "window.big") {
		t.Fatalf("compacted HTML should strip script payloads: %q", got)
	}
}

func TestCompactToolOutput_Logs(t *testing.T) {
	t.Parallel()

	lines := make([]string, 0, 45)
	for i := 1; i <= 45; i++ {
		lines = append(lines, fmt.Sprintf("2026-04-04T12:00:%02dZ INFO line %02d", i%60, i))
	}
	input := strings.Join(lines, "\n")

	got := CompactToolOutput(input, 10)

	if got == input {
		t.Fatal("expected log output to be compacted")
	}
	if !strings.Contains(got, "[tool output compacted: log]") {
		t.Fatalf("compacted log missing marker: %q", got)
	}
	if !strings.Contains(got, "line 01") || !strings.Contains(got, "line 45") {
		t.Fatalf("compacted log should preserve head and tail: %q", got)
	}
	if !strings.Contains(got, "... (15 lines omitted") {
		t.Fatalf("compacted log missing omitted-line marker: %q", got)
	}
	if strings.Contains(got, "line 25") {
		t.Fatalf("compacted log should drop middle lines: %q", got)
	}
}

func TestCompactToolOutput_Short(t *testing.T) {
	t.Parallel()

	input := "short tool output"
	got := CompactToolOutput(input, 0)
	if got != input {
		t.Fatalf("short content should remain unchanged: %q", got)
	}
}

func TestCompactToolOutput_BelowThreshold(t *testing.T) {
	t.Parallel()

	input := strings.Repeat("useful output ", 80)
	got := CompactToolOutput(input, 1000)
	if got != input {
		t.Fatalf("content below threshold should remain unchanged: %q", got)
	}
}
