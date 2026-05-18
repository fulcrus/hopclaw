package toolruntime

import (
	"fmt"
	"math"
	"strings"
)

// ---------------------------------------------------------------------------
// Canvas template system
// ---------------------------------------------------------------------------

// canvasTemplate defines a named, self-contained HTML template that can be
// rendered from structured parameters.
type canvasTemplate struct {
	Name        string
	Description string
	Required    []string
	Render      func(params map[string]any) (string, error)
}

// SVG chart dimension defaults.
const (
	chartDefaultWidth  = 600
	chartDefaultHeight = 400
	chartPadding       = 60
	chartBarGap        = 8
	chartLabelFontSize = 12
	chartTitleFontSize = 16
	chartStrokeWidth   = 2
	chartPointRadius   = 4
	pieDefaultRadius   = 150
	pieCenterX         = 300
	pieCenterY         = 220
)

// canvasTemplates is the global registry of built-in canvas templates.
var canvasTemplates = map[string]canvasTemplate{
	"chart.bar":      chartBarTemplate(),
	"chart.line":     chartLineTemplate(),
	"chart.pie":      chartPieTemplate(),
	"table.data":     tableDataTemplate(),
	"markdown":       markdownTemplate(),
	"code.highlight": codeHighlightTemplate(),
	"dashboard":      dashboardTemplate(),
	"form.input":     formInputTemplate(),
}

// renderCanvasTemplate looks up the named template and renders it with params.
func renderCanvasTemplate(name string, params map[string]any) (string, error) {
	tmpl, ok := canvasTemplates[name]
	if !ok {
		return "", fmt.Errorf("unknown canvas template %q", name)
	}
	for _, key := range tmpl.Required {
		if _, exists := params[key]; !exists {
			return "", fmt.Errorf("template %q requires parameter %q", name, key)
		}
	}
	return tmpl.Render(params)
}

// ---------------------------------------------------------------------------
// chart.bar
// ---------------------------------------------------------------------------

func chartBarTemplate() canvasTemplate {
	return canvasTemplate{
		Name:        "chart.bar",
		Description: "SVG bar chart with labeled axes.",
		Required:    []string{"labels", "values"},
		Render: func(params map[string]any) (string, error) {
			title, _ := paramString(params, "title")
			labels, err := paramStringSlice(params, "labels")
			if err != nil {
				return "", fmt.Errorf("chart.bar: %w", err)
			}
			values, err := paramFloat64Slice(params, "values")
			if err != nil {
				return "", fmt.Errorf("chart.bar: %w", err)
			}
			if len(labels) != len(values) {
				return "", fmt.Errorf("chart.bar: labels and values must have the same length")
			}
			colors := defaultColors(params, len(labels))

			maxVal := maxFloat(values)
			if maxVal == 0 {
				maxVal = 1
			}
			plotW := chartDefaultWidth - 2*chartPadding
			plotH := chartDefaultHeight - 2*chartPadding
			barW := (plotW - chartBarGap*(len(labels)-1)) / len(labels)

			var sb strings.Builder
			sb.WriteString(svgHeader(chartDefaultWidth, chartDefaultHeight))
			if title != "" {
				sb.WriteString(fmt.Sprintf(`<text x="%d" y="30" text-anchor="middle" font-size="%d" font-weight="bold">%s</text>`,
					chartDefaultWidth/2, chartTitleFontSize, escapeXML(title)))
			}
			for i, v := range values {
				h := int(float64(plotH) * v / maxVal)
				x := chartPadding + i*(barW+chartBarGap)
				y := chartPadding + plotH - h
				color := colors[i%len(colors)]
				sb.WriteString(fmt.Sprintf(`<rect x="%d" y="%d" width="%d" height="%d" fill="%s"/>`,
					x, y, barW, h, color))
				sb.WriteString(fmt.Sprintf(`<text x="%d" y="%d" text-anchor="middle" font-size="%d">%s</text>`,
					x+barW/2, chartPadding+plotH+chartLabelFontSize+4, chartLabelFontSize, escapeXML(labels[i])))
			}
			sb.WriteString("</svg>")
			return wrapHTML(title, sb.String()), nil
		},
	}
}

// ---------------------------------------------------------------------------
// chart.line
// ---------------------------------------------------------------------------

func chartLineTemplate() canvasTemplate {
	return canvasTemplate{
		Name:        "chart.line",
		Description: "SVG line chart with optional data points.",
		Required:    []string{"labels", "values"},
		Render: func(params map[string]any) (string, error) {
			title, _ := paramString(params, "title")
			labels, err := paramStringSlice(params, "labels")
			if err != nil {
				return "", fmt.Errorf("chart.line: %w", err)
			}
			values, err := paramFloat64Slice(params, "values")
			if err != nil {
				return "", fmt.Errorf("chart.line: %w", err)
			}
			if len(labels) != len(values) {
				return "", fmt.Errorf("chart.line: labels and values must have the same length")
			}
			color := "#4A90D9"
			if c, ok := paramString(params, "color"); ok {
				color = c
			}

			maxVal := maxFloat(values)
			if maxVal == 0 {
				maxVal = 1
			}
			plotW := chartDefaultWidth - 2*chartPadding
			plotH := chartDefaultHeight - 2*chartPadding
			step := 0
			if len(labels) > 1 {
				step = plotW / (len(labels) - 1)
			}

			var sb strings.Builder
			sb.WriteString(svgHeader(chartDefaultWidth, chartDefaultHeight))
			if title != "" {
				sb.WriteString(fmt.Sprintf(`<text x="%d" y="30" text-anchor="middle" font-size="%d" font-weight="bold">%s</text>`,
					chartDefaultWidth/2, chartTitleFontSize, escapeXML(title)))
			}

			// Build polyline points.
			points := make([]string, len(values))
			for i, v := range values {
				x := chartPadding + i*step
				y := chartPadding + plotH - int(float64(plotH)*v/maxVal)
				points[i] = fmt.Sprintf("%d,%d", x, y)
			}
			sb.WriteString(fmt.Sprintf(`<polyline points="%s" fill="none" stroke="%s" stroke-width="%d"/>`,
				strings.Join(points, " "), color, chartStrokeWidth))

			// Data point circles.
			for i, v := range values {
				x := chartPadding + i*step
				y := chartPadding + plotH - int(float64(plotH)*v/maxVal)
				sb.WriteString(fmt.Sprintf(`<circle cx="%d" cy="%d" r="%d" fill="%s"/>`,
					x, y, chartPointRadius, color))
			}

			// X-axis labels.
			for i, lbl := range labels {
				x := chartPadding + i*step
				sb.WriteString(fmt.Sprintf(`<text x="%d" y="%d" text-anchor="middle" font-size="%d">%s</text>`,
					x, chartPadding+plotH+chartLabelFontSize+4, chartLabelFontSize, escapeXML(lbl)))
			}
			sb.WriteString("</svg>")
			return wrapHTML(title, sb.String()), nil
		},
	}
}

// ---------------------------------------------------------------------------
// chart.pie
// ---------------------------------------------------------------------------

func chartPieTemplate() canvasTemplate {
	return canvasTemplate{
		Name:        "chart.pie",
		Description: "SVG pie chart with legend.",
		Required:    []string{"labels", "values"},
		Render: func(params map[string]any) (string, error) {
			title, _ := paramString(params, "title")
			labels, err := paramStringSlice(params, "labels")
			if err != nil {
				return "", fmt.Errorf("chart.pie: %w", err)
			}
			values, err := paramFloat64Slice(params, "values")
			if err != nil {
				return "", fmt.Errorf("chart.pie: %w", err)
			}
			if len(labels) != len(values) {
				return "", fmt.Errorf("chart.pie: labels and values must have the same length")
			}
			colors := defaultColors(params, len(labels))

			total := 0.0
			for _, v := range values {
				total += v
			}
			if total == 0 {
				total = 1
			}

			var sb strings.Builder
			sb.WriteString(svgHeader(chartDefaultWidth, chartDefaultHeight))
			if title != "" {
				sb.WriteString(fmt.Sprintf(`<text x="%d" y="30" text-anchor="middle" font-size="%d" font-weight="bold">%s</text>`,
					chartDefaultWidth/2, chartTitleFontSize, escapeXML(title)))
			}

			startAngle := -math.Pi / 2
			for i, v := range values {
				frac := v / total
				angle := frac * 2 * math.Pi
				endAngle := startAngle + angle

				x1 := pieCenterX + int(float64(pieDefaultRadius)*math.Cos(startAngle))
				y1 := pieCenterY + int(float64(pieDefaultRadius)*math.Sin(startAngle))
				x2 := pieCenterX + int(float64(pieDefaultRadius)*math.Cos(endAngle))
				y2 := pieCenterY + int(float64(pieDefaultRadius)*math.Sin(endAngle))
				largeArc := 0
				if angle > math.Pi {
					largeArc = 1
				}
				color := colors[i%len(colors)]
				sb.WriteString(fmt.Sprintf(`<path d="M%d,%d L%d,%d A%d,%d 0 %d,1 %d,%d Z" fill="%s"/>`,
					pieCenterX, pieCenterY, x1, y1,
					pieDefaultRadius, pieDefaultRadius, largeArc, x2, y2, color))
				startAngle = endAngle
			}

			// Legend.
			legendX := chartDefaultWidth - chartPadding - 100
			for i, lbl := range labels {
				ly := chartPadding + i*20
				color := colors[i%len(colors)]
				sb.WriteString(fmt.Sprintf(`<rect x="%d" y="%d" width="12" height="12" fill="%s"/>`, legendX, ly, color))
				sb.WriteString(fmt.Sprintf(`<text x="%d" y="%d" font-size="%d">%s</text>`,
					legendX+16, ly+11, chartLabelFontSize, escapeXML(lbl)))
			}

			sb.WriteString("</svg>")
			return wrapHTML(title, sb.String()), nil
		},
	}
}

// ---------------------------------------------------------------------------
// table.data
// ---------------------------------------------------------------------------

func tableDataTemplate() canvasTemplate {
	return canvasTemplate{
		Name:        "table.data",
		Description: "HTML data table with sortable column headers.",
		Required:    []string{"headers", "rows"},
		Render: func(params map[string]any) (string, error) {
			headers, err := paramStringSlice(params, "headers")
			if err != nil {
				return "", fmt.Errorf("table.data: %w", err)
			}
			rows, err := paramStringSlice2D(params, "rows")
			if err != nil {
				return "", fmt.Errorf("table.data: %w", err)
			}

			var sb strings.Builder
			sb.WriteString(`<table style="border-collapse:collapse;width:100%;font-family:sans-serif;">`)
			sb.WriteString("<thead><tr>")
			for _, h := range headers {
				sb.WriteString(fmt.Sprintf(`<th style="border:1px solid #ccc;padding:8px;background:#f5f5f5;cursor:pointer;" onclick="sortTable(this)">%s</th>`, escapeHTML(h)))
			}
			sb.WriteString("</tr></thead><tbody>")
			for _, row := range rows {
				sb.WriteString("<tr>")
				for _, cell := range row {
					sb.WriteString(fmt.Sprintf(`<td style="border:1px solid #ccc;padding:8px;">%s</td>`, escapeHTML(cell)))
				}
				sb.WriteString("</tr>")
			}
			sb.WriteString("</tbody></table>")

			sortScript := `<script>
function sortTable(th){
  var table=th.closest('table'),idx=[].indexOf.call(th.parentNode.children,th);
  var rows=[].slice.call(table.tBodies[0].rows);
  var asc=th.dataset.asc!=='true';th.dataset.asc=asc;
  rows.sort(function(a,b){
    var A=a.cells[idx].textContent,B=b.cells[idx].textContent;
    var nA=parseFloat(A),nB=parseFloat(B);
    if(!isNaN(nA)&&!isNaN(nB))return asc?nA-nB:nB-nA;
    return asc?A.localeCompare(B):B.localeCompare(A);
  });
  rows.forEach(function(r){table.tBodies[0].appendChild(r);});
}
</script>`
			return wrapHTML("Data Table", sb.String()+sortScript), nil
		},
	}
}

// ---------------------------------------------------------------------------
// markdown
// ---------------------------------------------------------------------------

func markdownTemplate() canvasTemplate {
	return canvasTemplate{
		Name:        "markdown",
		Description: "Simple Markdown to HTML converter.",
		Required:    []string{"content"},
		Render: func(params map[string]any) (string, error) {
			content, ok := paramString(params, "content")
			if !ok {
				return "", fmt.Errorf("markdown: content must be a string")
			}
			html := simpleMarkdownToHTML(content)
			body := fmt.Sprintf(`<div style="font-family:sans-serif;max-width:800px;margin:0 auto;padding:20px;line-height:1.6;">%s</div>`, html)
			return wrapHTML("Markdown", body), nil
		},
	}
}

// ---------------------------------------------------------------------------
// code.highlight
// ---------------------------------------------------------------------------

func codeHighlightTemplate() canvasTemplate {
	return canvasTemplate{
		Name:        "code.highlight",
		Description: "Syntax-highlighted code block.",
		Required:    []string{"code"},
		Render: func(params map[string]any) (string, error) {
			code, ok := paramString(params, "code")
			if !ok {
				return "", fmt.Errorf("code.highlight: code must be a string")
			}
			lang, _ := paramString(params, "language")
			if lang == "" {
				lang = "text"
			}
			body := fmt.Sprintf(`<pre style="background:#1e1e1e;color:#d4d4d4;padding:16px;border-radius:6px;overflow-x:auto;font-size:14px;line-height:1.5;font-family:'Consolas','Monaco','Courier New',monospace;"><code class="language-%s">%s</code></pre>`,
				escapeHTML(lang), escapeHTML(code))
			return wrapHTML("Code: "+lang, body), nil
		},
	}
}

// ---------------------------------------------------------------------------
// dashboard
// ---------------------------------------------------------------------------

func dashboardTemplate() canvasTemplate {
	return canvasTemplate{
		Name:        "dashboard",
		Description: "Multi-panel dashboard layout.",
		Required:    []string{"panels"},
		Render: func(params map[string]any) (string, error) {
			title, _ := paramString(params, "title")
			if title == "" {
				title = "Dashboard"
			}
			panels, err := paramPanels(params)
			if err != nil {
				return "", fmt.Errorf("dashboard: %w", err)
			}

			var sb strings.Builder
			sb.WriteString(fmt.Sprintf(`<h1 style="font-family:sans-serif;text-align:center;">%s</h1>`, escapeHTML(title)))
			sb.WriteString(`<div style="display:grid;grid-template-columns:repeat(auto-fit,minmax(300px,1fr));gap:16px;padding:16px;font-family:sans-serif;">`)
			for _, p := range panels {
				sb.WriteString(`<div style="border:1px solid #ddd;border-radius:8px;padding:16px;background:#fafafa;">`)
				sb.WriteString(fmt.Sprintf(`<h3 style="margin:0 0 8px 0;">%s</h3>`, escapeHTML(p.title)))
				sb.WriteString(fmt.Sprintf(`<div>%s</div>`, p.content))
				sb.WriteString("</div>")
			}
			sb.WriteString("</div>")
			return wrapHTML(title, sb.String()), nil
		},
	}
}

// ---------------------------------------------------------------------------
// form.input
// ---------------------------------------------------------------------------

const (
	formFieldText     = "text"
	formFieldEmail    = "email"
	formFieldPassword = "password"
	formFieldNumber   = "number"
	formFieldTextarea = "textarea"
)

func formInputTemplate() canvasTemplate {
	return canvasTemplate{
		Name:        "form.input",
		Description: "Input form with configurable fields.",
		Required:    []string{"fields"},
		Render: func(params map[string]any) (string, error) {
			title, _ := paramString(params, "title")
			if title == "" {
				title = "Form"
			}
			fields, err := paramFields(params)
			if err != nil {
				return "", fmt.Errorf("form.input: %w", err)
			}

			var sb strings.Builder
			sb.WriteString(fmt.Sprintf(`<form style="font-family:sans-serif;max-width:500px;margin:0 auto;padding:20px;">
<h2>%s</h2>`, escapeHTML(title)))
			for _, f := range fields {
				label := f.label
				if label == "" {
					label = f.name
				}
				sb.WriteString(fmt.Sprintf(`<div style="margin-bottom:16px;">
<label style="display:block;margin-bottom:4px;font-weight:bold;">%s</label>`, escapeHTML(label)))
				if f.fieldType == formFieldTextarea {
					sb.WriteString(fmt.Sprintf(`<textarea name="%s" placeholder="%s" style="width:100%%;padding:8px;border:1px solid #ccc;border-radius:4px;min-height:80px;box-sizing:border-box;"></textarea>`,
						escapeHTML(f.name), escapeHTML(f.placeholder)))
				} else {
					inputType := f.fieldType
					if inputType == "" {
						inputType = formFieldText
					}
					sb.WriteString(fmt.Sprintf(`<input type="%s" name="%s" placeholder="%s" style="width:100%%;padding:8px;border:1px solid #ccc;border-radius:4px;box-sizing:border-box;"/>`,
						escapeHTML(inputType), escapeHTML(f.name), escapeHTML(f.placeholder)))
				}
				sb.WriteString("</div>")
			}
			sb.WriteString(`<button type="submit" style="background:#4A90D9;color:#fff;border:none;padding:10px 24px;border-radius:4px;cursor:pointer;font-size:14px;">Submit</button>
</form>`)
			return wrapHTML(title, sb.String()), nil
		},
	}
}

// ---------------------------------------------------------------------------
// Helper types
// ---------------------------------------------------------------------------

type dashboardPanel struct {
	title   string
	content string
}

type formField struct {
	name        string
	fieldType   string
	label       string
	placeholder string
}

// ---------------------------------------------------------------------------
// Parameter extraction helpers
// ---------------------------------------------------------------------------

func paramString(params map[string]any, key string) (string, bool) {
	v, ok := params[key]
	if !ok {
		return "", false
	}
	s, ok := v.(string)
	return s, ok
}

func paramStringSlice(params map[string]any, key string) ([]string, error) {
	v, ok := params[key]
	if !ok {
		return nil, fmt.Errorf("%s is required", key)
	}
	raw, ok := v.([]any)
	if !ok {
		return nil, fmt.Errorf("%s must be an array", key)
	}
	out := make([]string, len(raw))
	for i, item := range raw {
		s, ok := item.(string)
		if !ok {
			return nil, fmt.Errorf("%s[%d] must be a string", key, i)
		}
		out[i] = s
	}
	return out, nil
}

func paramFloat64Slice(params map[string]any, key string) ([]float64, error) {
	v, ok := params[key]
	if !ok {
		return nil, fmt.Errorf("%s is required", key)
	}
	raw, ok := v.([]any)
	if !ok {
		return nil, fmt.Errorf("%s must be an array", key)
	}
	out := make([]float64, len(raw))
	for i, item := range raw {
		switch n := item.(type) {
		case float64:
			out[i] = n
		case int:
			out[i] = float64(n)
		case int64:
			out[i] = float64(n)
		default:
			return nil, fmt.Errorf("%s[%d] must be a number", key, i)
		}
	}
	return out, nil
}

func paramStringSlice2D(params map[string]any, key string) ([][]string, error) {
	v, ok := params[key]
	if !ok {
		return nil, fmt.Errorf("%s is required", key)
	}
	rawRows, ok := v.([]any)
	if !ok {
		return nil, fmt.Errorf("%s must be an array of arrays", key)
	}
	out := make([][]string, len(rawRows))
	for i, rawRow := range rawRows {
		cells, ok := rawRow.([]any)
		if !ok {
			return nil, fmt.Errorf("%s[%d] must be an array", key, i)
		}
		row := make([]string, len(cells))
		for j, cell := range cells {
			s, ok := cell.(string)
			if !ok {
				return nil, fmt.Errorf("%s[%d][%d] must be a string", key, i, j)
			}
			row[j] = s
		}
		out[i] = row
	}
	return out, nil
}

func paramPanels(params map[string]any) ([]dashboardPanel, error) {
	v, ok := params["panels"]
	if !ok {
		return nil, fmt.Errorf("panels is required")
	}
	raw, ok := v.([]any)
	if !ok {
		return nil, fmt.Errorf("panels must be an array")
	}
	out := make([]dashboardPanel, len(raw))
	for i, item := range raw {
		m, ok := item.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("panels[%d] must be an object", i)
		}
		t, _ := m["title"].(string)
		c, _ := m["content"].(string)
		out[i] = dashboardPanel{title: t, content: c}
	}
	return out, nil
}

func paramFields(params map[string]any) ([]formField, error) {
	v, ok := params["fields"]
	if !ok {
		return nil, fmt.Errorf("fields is required")
	}
	raw, ok := v.([]any)
	if !ok {
		return nil, fmt.Errorf("fields must be an array")
	}
	out := make([]formField, len(raw))
	for i, item := range raw {
		m, ok := item.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("fields[%d] must be an object", i)
		}
		name, _ := m["name"].(string)
		if name == "" {
			return nil, fmt.Errorf("fields[%d].name is required", i)
		}
		ft, _ := m["type"].(string)
		label, _ := m["label"].(string)
		ph, _ := m["placeholder"].(string)
		out[i] = formField{name: name, fieldType: ft, label: label, placeholder: ph}
	}
	return out, nil
}

// ---------------------------------------------------------------------------
// Rendering helpers
// ---------------------------------------------------------------------------

var defaultColorPalette = []string{
	"#4A90D9", "#E6553A", "#3CB371", "#F5A623",
	"#9B59B6", "#1ABC9C", "#E67E22", "#3498DB",
}

func defaultColors(params map[string]any, n int) []string {
	if raw, ok := params["colors"]; ok {
		if arr, ok := raw.([]any); ok {
			out := make([]string, 0, len(arr))
			for _, item := range arr {
				if s, ok := item.(string); ok {
					out = append(out, s)
				}
			}
			if len(out) > 0 {
				return out
			}
		}
	}
	if n <= len(defaultColorPalette) {
		return defaultColorPalette[:n]
	}
	out := make([]string, n)
	for i := range out {
		out[i] = defaultColorPalette[i%len(defaultColorPalette)]
	}
	return out
}

func maxFloat(values []float64) float64 {
	m := 0.0
	for _, v := range values {
		if v > m {
			m = v
		}
	}
	return m
}

func svgHeader(width, height int) string {
	return fmt.Sprintf(`<svg xmlns="http://www.w3.org/2000/svg" width="%d" height="%d" viewBox="0 0 %d %d">`,
		width, height, width, height)
}

func wrapHTML(title, body string) string {
	if title == "" {
		title = "Canvas"
	}
	return fmt.Sprintf(`<!DOCTYPE html>
<html><head><meta charset="utf-8"><title>%s</title></head>
<body style="margin:0;padding:16px;">%s</body></html>`, escapeHTML(title), body)
}

func escapeHTML(s string) string {
	s = strings.ReplaceAll(s, "&", "&amp;")
	s = strings.ReplaceAll(s, "<", "&lt;")
	s = strings.ReplaceAll(s, ">", "&gt;")
	s = strings.ReplaceAll(s, `"`, "&quot;")
	return s
}

func escapeXML(s string) string {
	return escapeHTML(s)
}

// simpleMarkdownToHTML converts a subset of Markdown to HTML.
// Supported: headings (###), bold (**), italic (*), inline code (`),
// unordered lists (- ), ordered lists (1. ), links [text](url), paragraphs.
func simpleMarkdownToHTML(md string) string {
	lines := strings.Split(md, "\n")
	var sb strings.Builder
	inUL := false
	inOL := false

	closeList := func() {
		if inUL {
			sb.WriteString("</ul>")
			inUL = false
		}
		if inOL {
			sb.WriteString("</ol>")
			inOL = false
		}
	}

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)

		// Blank line: close any open list.
		if trimmed == "" {
			closeList()
			continue
		}

		// Headings.
		if strings.HasPrefix(trimmed, "### ") {
			closeList()
			sb.WriteString("<h3>" + mdInline(strings.TrimPrefix(trimmed, "### ")) + "</h3>")
			continue
		}
		if strings.HasPrefix(trimmed, "## ") {
			closeList()
			sb.WriteString("<h2>" + mdInline(strings.TrimPrefix(trimmed, "## ")) + "</h2>")
			continue
		}
		if strings.HasPrefix(trimmed, "# ") {
			closeList()
			sb.WriteString("<h1>" + mdInline(strings.TrimPrefix(trimmed, "# ")) + "</h1>")
			continue
		}

		// Unordered list.
		if strings.HasPrefix(trimmed, "- ") {
			if inOL {
				sb.WriteString("</ol>")
				inOL = false
			}
			if !inUL {
				sb.WriteString("<ul>")
				inUL = true
			}
			sb.WriteString("<li>" + mdInline(strings.TrimPrefix(trimmed, "- ")) + "</li>")
			continue
		}

		// Ordered list (digits followed by ". ").
		if len(trimmed) > 2 && trimmed[0] >= '0' && trimmed[0] <= '9' {
			if idx := strings.Index(trimmed, ". "); idx > 0 {
				allDigits := true
				for _, c := range trimmed[:idx] {
					if c < '0' || c > '9' {
						allDigits = false
						break
					}
				}
				if allDigits {
					if inUL {
						sb.WriteString("</ul>")
						inUL = false
					}
					if !inOL {
						sb.WriteString("<ol>")
						inOL = true
					}
					sb.WriteString("<li>" + mdInline(trimmed[idx+2:]) + "</li>")
					continue
				}
			}
		}

		// Regular paragraph.
		closeList()
		sb.WriteString("<p>" + mdInline(trimmed) + "</p>")
	}
	closeList()
	return sb.String()
}

// mdInline applies inline Markdown formatting: bold, italic, code, links.
func mdInline(s string) string {
	// Inline code: `code`
	s = replaceInlinePattern(s, "`", "<code>", "</code>")
	// Bold: **bold**
	s = replaceInlinePattern(s, "**", "<strong>", "</strong>")
	// Italic: *italic* (avoid matching already-converted bold)
	s = replaceInlinePattern(s, "*", "<em>", "</em>")
	// Links: [text](url)
	s = convertLinks(s)
	return s
}

func replaceInlinePattern(s, delimiter, openTag, closeTag string) string {
	var sb strings.Builder
	rest := s
	for {
		start := strings.Index(rest, delimiter)
		if start < 0 {
			sb.WriteString(rest)
			break
		}
		end := strings.Index(rest[start+len(delimiter):], delimiter)
		if end < 0 {
			sb.WriteString(rest)
			break
		}
		end += start + len(delimiter)
		sb.WriteString(rest[:start])
		sb.WriteString(openTag)
		sb.WriteString(rest[start+len(delimiter) : end])
		sb.WriteString(closeTag)
		rest = rest[end+len(delimiter):]
	}
	return sb.String()
}

func convertLinks(s string) string {
	var sb strings.Builder
	rest := s
	for {
		start := strings.Index(rest, "[")
		if start < 0 {
			sb.WriteString(rest)
			break
		}
		mid := strings.Index(rest[start:], "](")
		if mid < 0 {
			sb.WriteString(rest)
			break
		}
		mid += start
		end := strings.Index(rest[mid+2:], ")")
		if end < 0 {
			sb.WriteString(rest)
			break
		}
		end += mid + 2
		text := rest[start+1 : mid]
		url := rest[mid+2 : end]
		sb.WriteString(rest[:start])
		sb.WriteString(fmt.Sprintf(`<a href="%s">%s</a>`, escapeHTML(url), escapeHTML(text)))
		rest = rest[end+1:]
	}
	return sb.String()
}
