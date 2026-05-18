package toolruntime

import (
	"fmt"
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// chart.bar
// ---------------------------------------------------------------------------

func TestTemplateChartBar(t *testing.T) {
	t.Parallel()
	html, err := renderCanvasTemplate("chart.bar", map[string]any{
		"title":  "Revenue",
		"labels": []any{"Q1", "Q2", "Q3"},
		"values": []any{100.0, 200.0, 150.0},
	})
	if err != nil {
		t.Fatalf("chart.bar render: %v", err)
	}
	assertContains(t, html, "<svg")
	assertContains(t, html, "</svg>")
	assertContains(t, html, "Revenue")
	assertContains(t, html, "<rect")
}

func TestTemplateChartBarWithColors(t *testing.T) {
	t.Parallel()
	html, err := renderCanvasTemplate("chart.bar", map[string]any{
		"labels": []any{"A", "B"},
		"values": []any{10.0, 20.0},
		"colors": []any{"#ff0000", "#00ff00"},
	})
	if err != nil {
		t.Fatalf("chart.bar render: %v", err)
	}
	assertContains(t, html, "#ff0000")
	assertContains(t, html, "#00ff00")
}

func TestTemplateChartBarMissingLabels(t *testing.T) {
	t.Parallel()
	_, err := renderCanvasTemplate("chart.bar", map[string]any{
		"values": []any{10.0},
	})
	if err == nil {
		t.Fatal("expected error for missing labels")
	}
}

func TestTemplateChartBarMismatchedLengths(t *testing.T) {
	t.Parallel()
	_, err := renderCanvasTemplate("chart.bar", map[string]any{
		"labels": []any{"A", "B", "C"},
		"values": []any{10.0, 20.0},
	})
	if err == nil {
		t.Fatal("expected error for mismatched labels/values lengths")
	}
}

func TestTemplateChartBarNoTitle(t *testing.T) {
	t.Parallel()
	html, err := renderCanvasTemplate("chart.bar", map[string]any{
		"labels": []any{"A"},
		"values": []any{10.0},
	})
	if err != nil {
		t.Fatalf("chart.bar render: %v", err)
	}
	assertContains(t, html, "<svg")
	assertContains(t, html, "<rect")
}

func TestTemplateChartBarZeroValues(t *testing.T) {
	t.Parallel()
	html, err := renderCanvasTemplate("chart.bar", map[string]any{
		"labels": []any{"A", "B"},
		"values": []any{0.0, 0.0},
	})
	if err != nil {
		t.Fatalf("chart.bar render with zero values: %v", err)
	}
	assertContains(t, html, "<svg")
}

func TestTemplateChartBarMissingValues(t *testing.T) {
	t.Parallel()
	_, err := renderCanvasTemplate("chart.bar", map[string]any{
		"labels": []any{"A"},
	})
	if err == nil {
		t.Fatal("expected error for missing values")
	}
}

// ---------------------------------------------------------------------------
// chart.line
// ---------------------------------------------------------------------------

func TestTemplateChartLine(t *testing.T) {
	t.Parallel()
	html, err := renderCanvasTemplate("chart.line", map[string]any{
		"title":  "Trend",
		"labels": []any{"Jan", "Feb", "Mar", "Apr"},
		"values": []any{10.0, 30.0, 20.0, 40.0},
		"color":  "#336699",
	})
	if err != nil {
		t.Fatalf("chart.line render: %v", err)
	}
	assertContains(t, html, "<svg")
	assertContains(t, html, "polyline")
	assertContains(t, html, "circle")
	assertContains(t, html, "#336699")
	assertContains(t, html, "Trend")
}

func TestTemplateChartLineDefaultColor(t *testing.T) {
	t.Parallel()
	html, err := renderCanvasTemplate("chart.line", map[string]any{
		"labels": []any{"A", "B"},
		"values": []any{5.0, 10.0},
	})
	if err != nil {
		t.Fatalf("chart.line render: %v", err)
	}
	// Should use default color #4A90D9.
	assertContains(t, html, "#4A90D9")
}

func TestTemplateChartLineMissingValues(t *testing.T) {
	t.Parallel()
	_, err := renderCanvasTemplate("chart.line", map[string]any{
		"labels": []any{"A"},
	})
	if err == nil {
		t.Fatal("expected error for missing values")
	}
}

func TestTemplateChartLineMismatchedLengths(t *testing.T) {
	t.Parallel()
	_, err := renderCanvasTemplate("chart.line", map[string]any{
		"labels": []any{"A", "B", "C"},
		"values": []any{10.0},
	})
	if err == nil {
		t.Fatal("expected error for mismatched labels/values lengths")
	}
}

func TestTemplateChartLineSinglePoint(t *testing.T) {
	t.Parallel()
	html, err := renderCanvasTemplate("chart.line", map[string]any{
		"labels": []any{"A"},
		"values": []any{10.0},
	})
	if err != nil {
		t.Fatalf("chart.line render: %v", err)
	}
	assertContains(t, html, "<svg")
	assertContains(t, html, "polyline")
}

// ---------------------------------------------------------------------------
// chart.pie
// ---------------------------------------------------------------------------

func TestTemplateChartPie(t *testing.T) {
	t.Parallel()
	html, err := renderCanvasTemplate("chart.pie", map[string]any{
		"title":  "Market Share",
		"labels": []any{"Chrome", "Firefox", "Safari"},
		"values": []any{65.0, 20.0, 15.0},
	})
	if err != nil {
		t.Fatalf("chart.pie render: %v", err)
	}
	assertContains(t, html, "<svg")
	assertContains(t, html, "<path")
	assertContains(t, html, "Market Share")
	// Legend should have label text.
	assertContains(t, html, "Chrome")
	assertContains(t, html, "Firefox")
	assertContains(t, html, "Safari")
}

func TestTemplateChartPieMissingLabels(t *testing.T) {
	t.Parallel()
	_, err := renderCanvasTemplate("chart.pie", map[string]any{
		"values": []any{10.0, 20.0},
	})
	if err == nil {
		t.Fatal("expected error for missing labels")
	}
}

func TestTemplateChartPieMissingValues(t *testing.T) {
	t.Parallel()
	_, err := renderCanvasTemplate("chart.pie", map[string]any{
		"labels": []any{"A", "B"},
	})
	if err == nil {
		t.Fatal("expected error for missing values")
	}
}

func TestTemplateChartPieMismatchedLengths(t *testing.T) {
	t.Parallel()
	_, err := renderCanvasTemplate("chart.pie", map[string]any{
		"labels": []any{"A"},
		"values": []any{10.0, 20.0},
	})
	if err == nil {
		t.Fatal("expected error for mismatched labels/values lengths")
	}
}

func TestTemplateChartPieZeroTotal(t *testing.T) {
	t.Parallel()
	html, err := renderCanvasTemplate("chart.pie", map[string]any{
		"labels": []any{"A", "B"},
		"values": []any{0.0, 0.0},
	})
	if err != nil {
		t.Fatalf("chart.pie render with zero total: %v", err)
	}
	assertContains(t, html, "<svg")
}

func TestTemplateChartPieCustomColors(t *testing.T) {
	t.Parallel()
	html, err := renderCanvasTemplate("chart.pie", map[string]any{
		"labels": []any{"A", "B"},
		"values": []any{60.0, 40.0},
		"colors": []any{"#111111", "#222222"},
	})
	if err != nil {
		t.Fatalf("chart.pie render: %v", err)
	}
	assertContains(t, html, "#111111")
	assertContains(t, html, "#222222")
}

// ---------------------------------------------------------------------------
// table.data
// ---------------------------------------------------------------------------

func TestTemplateTableData(t *testing.T) {
	t.Parallel()
	html, err := renderCanvasTemplate("table.data", map[string]any{
		"headers": []any{"Name", "Age", "City"},
		"rows": []any{
			[]any{"Alice", "30", "NYC"},
			[]any{"Bob", "25", "LA"},
		},
	})
	if err != nil {
		t.Fatalf("table.data render: %v", err)
	}
	assertContains(t, html, "<table")
	assertContains(t, html, "<th")
	assertContains(t, html, "Name")
	assertContains(t, html, "Alice")
	assertContains(t, html, "Bob")
	assertContains(t, html, "sortTable")
}

func TestTemplateTableDataMissingHeaders(t *testing.T) {
	t.Parallel()
	_, err := renderCanvasTemplate("table.data", map[string]any{
		"rows": []any{[]any{"a"}},
	})
	if err == nil {
		t.Fatal("expected error for missing headers")
	}
}

func TestTemplateTableDataMissingRows(t *testing.T) {
	t.Parallel()
	_, err := renderCanvasTemplate("table.data", map[string]any{
		"headers": []any{"H1"},
	})
	if err == nil {
		t.Fatal("expected error for missing rows")
	}
}

// ---------------------------------------------------------------------------
// markdown
// ---------------------------------------------------------------------------

func TestTemplateMarkdownHeadings(t *testing.T) {
	t.Parallel()
	html, err := renderCanvasTemplate("markdown", map[string]any{
		"content": "# Title\n## Subtitle\n### Section",
	})
	if err != nil {
		t.Fatalf("markdown render: %v", err)
	}
	assertContains(t, html, "<h1>Title</h1>")
	assertContains(t, html, "<h2>Subtitle</h2>")
	assertContains(t, html, "<h3>Section</h3>")
}

func TestTemplateMarkdownBoldAndCode(t *testing.T) {
	t.Parallel()
	html, err := renderCanvasTemplate("markdown", map[string]any{
		"content": "This is **bold** and `code`.",
	})
	if err != nil {
		t.Fatalf("markdown render: %v", err)
	}
	assertContains(t, html, "<strong>bold</strong>")
	assertContains(t, html, "<code>code</code>")
}

func TestTemplateMarkdownItalic(t *testing.T) {
	t.Parallel()
	html, err := renderCanvasTemplate("markdown", map[string]any{
		"content": "This is *italic* text.",
	})
	if err != nil {
		t.Fatalf("markdown render: %v", err)
	}
	assertContains(t, html, "<em>italic</em>")
}

func TestTemplateMarkdownLists(t *testing.T) {
	t.Parallel()
	html, err := renderCanvasTemplate("markdown", map[string]any{
		"content": "- Item A\n- Item B\n\n1. First\n2. Second",
	})
	if err != nil {
		t.Fatalf("markdown render: %v", err)
	}
	assertContains(t, html, "<ul>")
	assertContains(t, html, "<li>Item A</li>")
	assertContains(t, html, "<ol>")
	assertContains(t, html, "<li>First</li>")
}

func TestTemplateMarkdownLinks(t *testing.T) {
	t.Parallel()
	html, err := renderCanvasTemplate("markdown", map[string]any{
		"content": "Visit [Go](https://go.dev) now.",
	})
	if err != nil {
		t.Fatalf("markdown render: %v", err)
	}
	assertContains(t, html, `<a href="https://go.dev">Go</a>`)
}

func TestTemplateMarkdownMissingContent(t *testing.T) {
	t.Parallel()
	_, err := renderCanvasTemplate("markdown", map[string]any{})
	if err == nil {
		t.Fatal("expected error for missing content")
	}
}

func TestTemplateMarkdownParagraphs(t *testing.T) {
	t.Parallel()
	html, err := renderCanvasTemplate("markdown", map[string]any{
		"content": "First paragraph.\n\nSecond paragraph.",
	})
	if err != nil {
		t.Fatalf("markdown render: %v", err)
	}
	assertContains(t, html, "<p>First paragraph.</p>")
	assertContains(t, html, "<p>Second paragraph.</p>")
}

// ---------------------------------------------------------------------------
// code.highlight
// ---------------------------------------------------------------------------

func TestTemplateCodeHighlight(t *testing.T) {
	t.Parallel()
	html, err := renderCanvasTemplate("code.highlight", map[string]any{
		"code":     "func main() {}",
		"language": "go",
	})
	if err != nil {
		t.Fatalf("code.highlight render: %v", err)
	}
	assertContains(t, html, "<pre")
	assertContains(t, html, "<code")
	assertContains(t, html, "language-go")
	assertContains(t, html, "func main()")
}

func TestTemplateCodeHighlightDefaultLanguage(t *testing.T) {
	t.Parallel()
	html, err := renderCanvasTemplate("code.highlight", map[string]any{
		"code": "hello world",
	})
	if err != nil {
		t.Fatalf("code.highlight render: %v", err)
	}
	assertContains(t, html, "language-text")
}

func TestTemplateCodeHighlightEscaping(t *testing.T) {
	t.Parallel()
	html, err := renderCanvasTemplate("code.highlight", map[string]any{
		"code": `<script>alert("xss")</script>`,
	})
	if err != nil {
		t.Fatalf("code.highlight render: %v", err)
	}
	assertContains(t, html, "&lt;script&gt;")
	assertNotContains(t, html, "<script>alert")
}

func TestTemplateCodeHighlightMissingCode(t *testing.T) {
	t.Parallel()
	_, err := renderCanvasTemplate("code.highlight", map[string]any{})
	if err == nil {
		t.Fatal("expected error for missing code")
	}
}

// ---------------------------------------------------------------------------
// dashboard
// ---------------------------------------------------------------------------

func TestTemplateDashboard(t *testing.T) {
	t.Parallel()
	html, err := renderCanvasTemplate("dashboard", map[string]any{
		"title": "Ops Dashboard",
		"panels": []any{
			map[string]any{"title": "CPU", "content": "42%"},
			map[string]any{"title": "Memory", "content": "8GB / 16GB"},
		},
	})
	if err != nil {
		t.Fatalf("dashboard render: %v", err)
	}
	assertContains(t, html, "Ops Dashboard")
	assertContains(t, html, "CPU")
	assertContains(t, html, "42%")
	assertContains(t, html, "Memory")
	assertContains(t, html, "8GB / 16GB")
	assertContains(t, html, "grid")
}

func TestTemplateDashboardDefaultTitle(t *testing.T) {
	t.Parallel()
	html, err := renderCanvasTemplate("dashboard", map[string]any{
		"panels": []any{
			map[string]any{"title": "P1", "content": "data"},
		},
	})
	if err != nil {
		t.Fatalf("dashboard render: %v", err)
	}
	assertContains(t, html, "Dashboard")
}

func TestTemplateDashboardMissingPanels(t *testing.T) {
	t.Parallel()
	_, err := renderCanvasTemplate("dashboard", map[string]any{
		"title": "Empty",
	})
	if err == nil {
		t.Fatal("expected error for missing panels")
	}
}

// ---------------------------------------------------------------------------
// form.input
// ---------------------------------------------------------------------------

func TestTemplateFormInput(t *testing.T) {
	t.Parallel()
	html, err := renderCanvasTemplate("form.input", map[string]any{
		"title": "Register",
		"fields": []any{
			map[string]any{"name": "username", "type": "text", "label": "Username", "placeholder": "Enter username"},
			map[string]any{"name": "email", "type": "email", "label": "Email"},
			map[string]any{"name": "bio", "type": "textarea", "label": "Bio"},
		},
	})
	if err != nil {
		t.Fatalf("form.input render: %v", err)
	}
	assertContains(t, html, "<form")
	assertContains(t, html, "Register")
	assertContains(t, html, `type="text"`)
	assertContains(t, html, `type="email"`)
	assertContains(t, html, "<textarea")
	assertContains(t, html, "Username")
	assertContains(t, html, "Enter username")
}

func TestTemplateFormInputFieldDefaultType(t *testing.T) {
	t.Parallel()
	html, err := renderCanvasTemplate("form.input", map[string]any{
		"fields": []any{
			map[string]any{"name": "city"},
		},
	})
	if err != nil {
		t.Fatalf("form.input render: %v", err)
	}
	// Default type should be "text".
	assertContains(t, html, `type="text"`)
}

func TestTemplateFormInputMissingFields(t *testing.T) {
	t.Parallel()
	_, err := renderCanvasTemplate("form.input", map[string]any{
		"title": "No Fields",
	})
	if err == nil {
		t.Fatal("expected error for missing fields")
	}
}

func TestTemplateFormInputFieldMissingName(t *testing.T) {
	t.Parallel()
	_, err := renderCanvasTemplate("form.input", map[string]any{
		"fields": []any{
			map[string]any{"type": "text", "label": "No Name"},
		},
	})
	if err == nil {
		t.Fatal("expected error for field without name")
	}
}

func TestTemplateFormInputDefaultTitle(t *testing.T) {
	t.Parallel()
	html, err := renderCanvasTemplate("form.input", map[string]any{
		"fields": []any{
			map[string]any{"name": "email", "type": "email"},
		},
	})
	if err != nil {
		t.Fatalf("form.input render: %v", err)
	}
	// Default title should be "Form".
	assertContains(t, html, "Form")
	assertContains(t, html, `type="email"`)
}

func TestTemplateFormInputPasswordField(t *testing.T) {
	t.Parallel()
	html, err := renderCanvasTemplate("form.input", map[string]any{
		"fields": []any{
			map[string]any{"name": "secret", "type": "password", "label": "Password"},
		},
	})
	if err != nil {
		t.Fatalf("form.input render: %v", err)
	}
	assertContains(t, html, `type="password"`)
	assertContains(t, html, "Password")
}

func TestTemplateFormInputLabelFallbackToName(t *testing.T) {
	t.Parallel()
	html, err := renderCanvasTemplate("form.input", map[string]any{
		"fields": []any{
			map[string]any{"name": "my_field"},
		},
	})
	if err != nil {
		t.Fatalf("form.input render: %v", err)
	}
	// When label is empty, it should use the name as label.
	assertContains(t, html, "my_field")
}

// ---------------------------------------------------------------------------
// Unknown template
// ---------------------------------------------------------------------------

func TestRenderUnknownTemplate(t *testing.T) {
	t.Parallel()
	_, err := renderCanvasTemplate("nonexistent.template", map[string]any{})
	if err == nil {
		t.Fatal("expected error for unknown template")
	}
	if !strings.Contains(err.Error(), "unknown canvas template") {
		t.Fatalf("expected 'unknown canvas template' in error, got %q", err.Error())
	}
}

// ---------------------------------------------------------------------------
// Template registry completeness
// ---------------------------------------------------------------------------

func TestTemplateRegistryCompleteness(t *testing.T) {
	t.Parallel()
	const expectedCount = 8
	if len(canvasTemplates) != expectedCount {
		t.Fatalf("canvasTemplates has %d entries, want %d", len(canvasTemplates), expectedCount)
	}
	expectedNames := []string{
		"chart.bar", "chart.line", "chart.pie",
		"table.data", "markdown", "code.highlight",
		"dashboard", "form.input",
	}
	for _, name := range expectedNames {
		if _, ok := canvasTemplates[name]; !ok {
			t.Fatalf("missing template %q in registry", name)
		}
	}
}

// ---------------------------------------------------------------------------
// Parameter extraction helpers
// ---------------------------------------------------------------------------

func TestParamStringSliceInvalidType(t *testing.T) {
	t.Parallel()
	_, err := paramStringSlice(map[string]any{"x": "not-an-array"}, "x")
	if err == nil {
		t.Fatal("expected error for non-array")
	}
}

func TestParamStringSliceNonStringElement(t *testing.T) {
	t.Parallel()
	_, err := paramStringSlice(map[string]any{"x": []any{"ok", 42}}, "x")
	if err == nil {
		t.Fatal("expected error for non-string element")
	}
}

func TestParamFloat64SliceNonNumber(t *testing.T) {
	t.Parallel()
	_, err := paramFloat64Slice(map[string]any{"x": []any{1.0, "bad"}}, "x")
	if err == nil {
		t.Fatal("expected error for non-number element")
	}
}

func TestParamFloat64SliceIntTypes(t *testing.T) {
	t.Parallel()
	vals, err := paramFloat64Slice(map[string]any{"x": []any{1.0, 2.0}}, "x")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(vals) != 2 || vals[0] != 1.0 || vals[1] != 2.0 {
		t.Fatalf("unexpected values: %v", vals)
	}
}

func TestParamStringSlice2DInvalidRow(t *testing.T) {
	t.Parallel()
	_, err := paramStringSlice2D(map[string]any{"x": []any{"not-array"}}, "x")
	if err == nil {
		t.Fatal("expected error for non-array row")
	}
}

func TestParamStringSlice2DInvalidCell(t *testing.T) {
	t.Parallel()
	_, err := paramStringSlice2D(map[string]any{"x": []any{[]any{42}}}, "x")
	if err == nil {
		t.Fatal("expected error for non-string cell")
	}
}

func TestParamPanelsInvalidItem(t *testing.T) {
	t.Parallel()
	_, err := paramPanels(map[string]any{"panels": []any{"not-object"}})
	if err == nil {
		t.Fatal("expected error for non-object panel")
	}
}

func TestParamFieldsInvalidItem(t *testing.T) {
	t.Parallel()
	_, err := paramFields(map[string]any{"fields": []any{"not-object"}})
	if err == nil {
		t.Fatal("expected error for non-object field")
	}
}

func TestParamFieldsMissing(t *testing.T) {
	t.Parallel()
	_, err := paramFields(map[string]any{})
	if err == nil {
		t.Fatal("expected error for missing fields key")
	}
}

func TestParamPanelsMissing(t *testing.T) {
	t.Parallel()
	_, err := paramPanels(map[string]any{})
	if err == nil {
		t.Fatal("expected error for missing panels key")
	}
}

func TestParamPanelsNotArray(t *testing.T) {
	t.Parallel()
	_, err := paramPanels(map[string]any{"panels": "not-array"})
	if err == nil {
		t.Fatal("expected error for non-array panels")
	}
}

func TestParamFieldsNotArray(t *testing.T) {
	t.Parallel()
	_, err := paramFields(map[string]any{"fields": "not-array"})
	if err == nil {
		t.Fatal("expected error for non-array fields")
	}
}

func TestParamStringSliceMissing(t *testing.T) {
	t.Parallel()
	_, err := paramStringSlice(map[string]any{}, "missing")
	if err == nil {
		t.Fatal("expected error for missing key")
	}
}

func TestParamFloat64SliceMissing(t *testing.T) {
	t.Parallel()
	_, err := paramFloat64Slice(map[string]any{}, "missing")
	if err == nil {
		t.Fatal("expected error for missing key")
	}
}

func TestParamFloat64SliceNotArray(t *testing.T) {
	t.Parallel()
	_, err := paramFloat64Slice(map[string]any{"x": "not-array"}, "x")
	if err == nil {
		t.Fatal("expected error for non-array value")
	}
}

func TestParamStringSlice2DMissing(t *testing.T) {
	t.Parallel()
	_, err := paramStringSlice2D(map[string]any{}, "missing")
	if err == nil {
		t.Fatal("expected error for missing key")
	}
}

func TestParamStringSlice2DNotArray(t *testing.T) {
	t.Parallel()
	_, err := paramStringSlice2D(map[string]any{"x": "not-array"}, "x")
	if err == nil {
		t.Fatal("expected error for non-array value")
	}
}

// ---------------------------------------------------------------------------
// simpleMarkdownToHTML edge cases
// ---------------------------------------------------------------------------

func TestSimpleMarkdownEmptyInput(t *testing.T) {
	t.Parallel()
	result := simpleMarkdownToHTML("")
	if result != "" {
		t.Fatalf("expected empty string, got %q", result)
	}
}

func TestSimpleMarkdownPlainParagraph(t *testing.T) {
	t.Parallel()
	result := simpleMarkdownToHTML("Just some text.")
	assertContains(t, result, "<p>Just some text.</p>")
}

// ---------------------------------------------------------------------------
// escapeHTML
// ---------------------------------------------------------------------------

func TestEscapeHTML(t *testing.T) {
	t.Parallel()
	cases := []struct {
		input string
		want  string
	}{
		{`<script>`, `&lt;script&gt;`},
		{`a & b`, `a &amp; b`},
		{`"quoted"`, `&quot;quoted&quot;`},
		{`normal`, `normal`},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(fmt.Sprintf("input=%q", tc.input), func(t *testing.T) {
			t.Parallel()
			got := escapeHTML(tc.input)
			if got != tc.want {
				t.Fatalf("escapeHTML(%q) = %q, want %q", tc.input, got, tc.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// defaultColors
// ---------------------------------------------------------------------------

func TestDefaultColorsFromParams(t *testing.T) {
	t.Parallel()
	colors := defaultColors(map[string]any{
		"colors": []any{"red", "green"},
	}, 2)
	if len(colors) != 2 || colors[0] != "red" || colors[1] != "green" {
		t.Fatalf("expected [red green], got %v", colors)
	}
}

func TestDefaultColorsFallback(t *testing.T) {
	t.Parallel()
	colors := defaultColors(map[string]any{}, 3)
	if len(colors) != 3 {
		t.Fatalf("expected 3 colors, got %d", len(colors))
	}
	if colors[0] != defaultColorPalette[0] {
		t.Fatalf("expected first default color %q, got %q", defaultColorPalette[0], colors[0])
	}
}

func TestDefaultColorsWrapAround(t *testing.T) {
	t.Parallel()
	n := len(defaultColorPalette) + 2
	colors := defaultColors(map[string]any{}, n)
	if len(colors) != n {
		t.Fatalf("expected %d colors, got %d", n, len(colors))
	}
	// Last color should wrap.
	if colors[len(defaultColorPalette)] != defaultColorPalette[0] {
		t.Fatalf("expected wrap-around color %q, got %q", defaultColorPalette[0], colors[len(defaultColorPalette)])
	}
}

// ---------------------------------------------------------------------------
// wrapHTML
// ---------------------------------------------------------------------------

func TestWrapHTMLDefaultTitle(t *testing.T) {
	t.Parallel()
	html := wrapHTML("", "<p>body</p>")
	assertContains(t, html, "<title>Canvas</title>")
	assertContains(t, html, "<p>body</p>")
}

func TestWrapHTMLCustomTitle(t *testing.T) {
	t.Parallel()
	html := wrapHTML("My Page", "<p>content</p>")
	assertContains(t, html, "<title>My Page</title>")
}

// ---------------------------------------------------------------------------
// Test helpers
// ---------------------------------------------------------------------------

func assertContains(t *testing.T, s, substr string) {
	t.Helper()
	if !strings.Contains(s, substr) {
		t.Fatalf("expected output to contain %q, got:\n%s", substr, truncate(s, 500))
	}
}

func assertNotContains(t *testing.T, s, substr string) {
	t.Helper()
	if strings.Contains(s, substr) {
		t.Fatalf("expected output NOT to contain %q, got:\n%s", substr, truncate(s, 500))
	}
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
