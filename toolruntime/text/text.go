// Package text implements text processing tool handlers (text.json, text.yaml,
// text.csv, text.xml, text.toml, text.ini, text.dotenv, text.jsonl, text.html,
// text.markdown, text.regex, text.base64, text.hex, text.url, text.uuid,
// text.template, text.count) for the toolruntime registry.
package text

import (
	"context"
	"encoding/base64"
	"encoding/csv"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"
	"text/template"
	"unicode/utf8"

	"github.com/fulcrus/hopclaw/agent"
	"github.com/fulcrus/hopclaw/contextengine"
	"github.com/fulcrus/hopclaw/skill"

	"gopkg.in/yaml.v2"
)

// Runtime is the narrow interface that text handlers need from *Builtins.
type Runtime interface {
	JSONResult(call agent.ToolCall, payload map[string]any) (contextengine.ToolResult, error)
	ResolvePath(input string) (string, error)
	// MaxReadBytes returns the configured maximum file read size (0 = unlimited).
	MaxReadBytes() int
	// ReadArtifact reads an artifact by URI, returning data, content type, and error.
	// Returns nil, "", nil when the artifact store is not configured.
	ReadArtifact(ctx context.Context, uri string) ([]byte, string, error)
}

// Handler is the tool handler signature for text tools.
type Handler func(ctx context.Context, rt Runtime, call agent.ToolCall) (contextengine.ToolResult, error)

// ToolDef pairs a tool manifest with a text handler.
type ToolDef struct {
	Manifest skill.ToolManifest
	Handler  Handler
}

// ToolDefs returns all text domain tool definitions.
func ToolDefs() []ToolDef {
	return []ToolDef{
		// --- Format Parsers ---
		{
			Manifest: skill.ToolManifest{
				Name:            "text.json",
				Description:     "Parse JSON from a file or inline string and optionally query a dot-path.",
				InputSchema:     textJSONInputSchema(),
				OutputSchema:    textJSONOutputSchema(),
				SideEffectClass: "read",
				Idempotent:      true,
			},
			Handler: handleTextJSON,
		},
		{
			Manifest: skill.ToolManifest{
				Name:            "text.yaml",
				Description:     "Parse YAML from a file or inline string and convert to JSON.",
				InputSchema:     textYAMLInputSchema(),
				OutputSchema:    textYAMLOutputSchema(),
				SideEffectClass: "read",
				Idempotent:      true,
			},
			Handler: handleTextYAML,
		},
		{
			Manifest: skill.ToolManifest{
				Name:            "text.csv",
				Description:     "Parse CSV/TSV from a file or inline string with optional column selection.",
				InputSchema:     textCSVInputSchema(),
				OutputSchema:    textCSVOutputSchema(),
				SideEffectClass: "read",
				Idempotent:      true,
			},
			Handler: handleTextCSV,
		},
		{
			Manifest: skill.ToolManifest{
				Name:            "text.xml",
				Description:     "Parse XML from a file or inline string and convert to a JSON representation.",
				InputSchema:     textXMLInputSchema(),
				OutputSchema:    textXMLOutputSchema(),
				SideEffectClass: "read",
				Idempotent:      true,
			},
			Handler: handleTextXML,
		},
		{
			Manifest: skill.ToolManifest{
				Name:            "text.toml",
				Description:     "Parse basic TOML from a file or inline string and convert to JSON.",
				InputSchema:     textTOMLInputSchema(),
				OutputSchema:    textTOMLOutputSchema(),
				SideEffectClass: "read",
				Idempotent:      true,
			},
			Handler: handleTextTOML,
		},
		{
			Manifest: skill.ToolManifest{
				Name:            "text.ini",
				Description:     "Parse INI/properties from a file or inline string and convert to JSON.",
				InputSchema:     textINIInputSchema(),
				OutputSchema:    textINIOutputSchema(),
				SideEffectClass: "read",
				Idempotent:      true,
			},
			Handler: handleTextINI,
		},
		{
			Manifest: skill.ToolManifest{
				Name:            "text.dotenv",
				Description:     "Parse a .env file or inline string into key-value pairs.",
				InputSchema:     textDotenvInputSchema(),
				OutputSchema:    textDotenvOutputSchema(),
				SideEffectClass: "read",
				Idempotent:      true,
			},
			Handler: handleTextDotenv,
		},
		{
			Manifest: skill.ToolManifest{
				Name:            "text.jsonl",
				Description:     "Parse JSONL (newline-delimited JSON) from a file or inline string.",
				InputSchema:     textJSONLInputSchema(),
				OutputSchema:    textJSONLOutputSchema(),
				SideEffectClass: "read",
				Idempotent:      true,
			},
			Handler: handleTextJSONL,
		},
		{
			Manifest: skill.ToolManifest{
				Name:            "text.html",
				Description:     "Extract text, links, or specific tags from HTML content.",
				InputSchema:     textHTMLInputSchema(),
				OutputSchema:    textHTMLOutputSchema(),
				SideEffectClass: "read",
				Idempotent:      true,
			},
			Handler: handleTextHTML,
		},
		{
			Manifest: skill.ToolManifest{
				Name:            "text.markdown",
				Description:     "Process Markdown: extract frontmatter, build table of contents, or extract a section.",
				InputSchema:     textMarkdownInputSchema(),
				OutputSchema:    textMarkdownOutputSchema(),
				SideEffectClass: "read",
				Idempotent:      true,
			},
			Handler: handleTextMarkdown,
		},
		// --- Text Processing ---
		{
			Manifest: skill.ToolManifest{
				Name:            "text.regex",
				Description:     "Match, extract, replace, or split text using a regular expression.",
				InputSchema:     textRegexInputSchema(),
				OutputSchema:    textRegexOutputSchema(),
				SideEffectClass: "read",
				Idempotent:      true,
			},
			Handler: handleTextRegex,
		},
		{
			Manifest: skill.ToolManifest{
				Name:            "text.base64",
				Description:     "Encode or decode a Base64 string.",
				InputSchema:     textBase64InputSchema(),
				OutputSchema:    textBase64OutputSchema(),
				SideEffectClass: "read",
				Idempotent:      true,
			},
			Handler: handleTextBase64,
		},
		{
			Manifest: skill.ToolManifest{
				Name:            "text.hex",
				Description:     "Encode or decode a hexadecimal string.",
				InputSchema:     textHexInputSchema(),
				OutputSchema:    textHexOutputSchema(),
				SideEffectClass: "read",
				Idempotent:      true,
			},
			Handler: handleTextHex,
		},
		{
			Manifest: skill.ToolManifest{
				Name:            "text.url",
				Description:     "Parse, encode, or decode a URL.",
				InputSchema:     textURLInputSchema(),
				OutputSchema:    textURLOutputSchema(),
				SideEffectClass: "read",
				Idempotent:      true,
			},
			Handler: handleTextURL,
		},
		// text.hash is deprecated — use crypto.hash (which has "text.hash" alias)
		{
			Manifest: skill.ToolManifest{
				Name:            "text.uuid",
				Description:     "Generate one or more UUID v4 values.",
				InputSchema:     textUUIDInputSchema(),
				OutputSchema:    textUUIDOutputSchema(),
				SideEffectClass: "read",
				Idempotent:      true,
			},
			Handler: handleTextUUID,
		},
		{
			Manifest: skill.ToolManifest{
				Name:            "text.template",
				Description:     "Render a Go text/template with the supplied data.",
				InputSchema:     textTemplateInputSchema(),
				OutputSchema:    textTemplateOutputSchema(),
				SideEffectClass: "read",
				Idempotent:      true,
			},
			Handler: handleTextTemplate,
		},
		{
			Manifest: skill.ToolManifest{
				Name:            "text.count",
				Description:     "Count lines, words, characters, and bytes of text.",
				InputSchema:     textCountInputSchema(),
				OutputSchema:    textCountOutputSchema(),
				SideEffectClass: "read",
				Idempotent:      true,
			},
			Handler: handleTextCount,
		},
	}
}

// ---------------------------------------------------------------------------
// Handlers
// ---------------------------------------------------------------------------

func handleTextJSON(_ context.Context, rt Runtime, call agent.ToolCall) (contextengine.ToolResult, error) {
	content, err := textContent(rt, call.Input)
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("text.json: %w", err)
	}
	var parsed any
	if err := json.Unmarshal([]byte(content), &parsed); err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("text.json: invalid JSON: %w", err)
	}
	query, _ := stringFrom(call.Input["query"])
	if strings.TrimSpace(query) != "" {
		parsed = queryJSON(parsed, query)
	}
	resultBytes, err := json.Marshal(parsed)
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("text.json: %w", err)
	}
	return rt.JSONResult(call, map[string]any{
		"result": json.RawMessage(resultBytes),
	})
}

func handleTextYAML(_ context.Context, rt Runtime, call agent.ToolCall) (contextengine.ToolResult, error) {
	content, err := textContent(rt, call.Input)
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("text.yaml: %w", err)
	}
	var parsed any
	if err := yaml.Unmarshal([]byte(content), &parsed); err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("text.yaml: invalid YAML: %w", err)
	}
	parsed = normalizeYAML(parsed)
	resultBytes, err := json.Marshal(parsed)
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("text.yaml: %w", err)
	}
	return rt.JSONResult(call, map[string]any{
		"result": json.RawMessage(resultBytes),
	})
}

func handleTextCSV(_ context.Context, rt Runtime, call agent.ToolCall) (contextengine.ToolResult, error) {
	content, err := textContent(rt, call.Input)
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("text.csv: %w", err)
	}
	delimiter, _ := stringFrom(call.Input["delimiter"])
	if delimiter == "" {
		delimiter = ","
	}
	hasHeader, err := boolFromDefault(call.Input["header"], true)
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("text.csv: invalid header flag: %w", err)
	}
	limit, err := intFrom(call.Input["limit"], 0)
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("text.csv: invalid limit: %w", err)
	}
	columns, _ := stringSliceFrom(call.Input["columns"])

	reader := csv.NewReader(strings.NewReader(content))
	reader.Comma = rune(delimiter[0])
	reader.LazyQuotes = true
	reader.FieldsPerRecord = -1

	allRecords, err := reader.ReadAll()
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("text.csv: %w", err)
	}
	if len(allRecords) == 0 {
		return rt.JSONResult(call, map[string]any{
			"headers":   []string{},
			"rows":      [][]string{},
			"row_count": 0,
		})
	}

	var headers []string
	var dataRows [][]string
	if hasHeader {
		headers = allRecords[0]
		dataRows = allRecords[1:]
	} else {
		dataRows = allRecords
	}

	var colIndices []int
	if len(columns) > 0 {
		for _, col := range columns {
			found := false
			if hasHeader {
				for i, h := range headers {
					if strings.EqualFold(h, col) {
						colIndices = append(colIndices, i)
						found = true
						break
					}
				}
			}
			if !found {
				if idx, err := strconv.Atoi(col); err == nil && idx >= 0 {
					colIndices = append(colIndices, idx)
				}
			}
		}
	}

	filterRow := func(row []string) []string {
		if len(colIndices) == 0 {
			return row
		}
		out := make([]string, 0, len(colIndices))
		for _, i := range colIndices {
			if i < len(row) {
				out = append(out, row[i])
			} else {
				out = append(out, "")
			}
		}
		return out
	}

	if len(colIndices) > 0 && hasHeader {
		headers = filterRow(headers)
	}

	rows := make([][]string, 0, len(dataRows))
	for i, row := range dataRows {
		if limit > 0 && i >= limit {
			break
		}
		rows = append(rows, filterRow(row))
	}

	return rt.JSONResult(call, map[string]any{
		"headers":   headers,
		"rows":      rows,
		"row_count": len(rows),
	})
}

func handleTextXML(_ context.Context, rt Runtime, call agent.ToolCall) (contextengine.ToolResult, error) {
	content, err := textContent(rt, call.Input)
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("text.xml: %w", err)
	}
	tagFilter, _ := stringFrom(call.Input["tag"])

	result, err := parseXMLToMap(content, tagFilter)
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("text.xml: %w", err)
	}
	return rt.JSONResult(call, map[string]any{
		"result": result,
	})
}

func handleTextTOML(_ context.Context, rt Runtime, call agent.ToolCall) (contextengine.ToolResult, error) {
	content, err := textContent(rt, call.Input)
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("text.toml: %w", err)
	}
	parsed, err := parseBasicTOML(content)
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("text.toml: %w", err)
	}
	return rt.JSONResult(call, map[string]any{
		"result": parsed,
	})
}

func handleTextINI(_ context.Context, rt Runtime, call agent.ToolCall) (contextengine.ToolResult, error) {
	content, err := textContent(rt, call.Input)
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("text.ini: %w", err)
	}
	parsed := parseINI(content)
	return rt.JSONResult(call, map[string]any{
		"result": parsed,
	})
}

func handleTextDotenv(_ context.Context, rt Runtime, call agent.ToolCall) (contextengine.ToolResult, error) {
	content, err := textContent(rt, call.Input)
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("text.dotenv: %w", err)
	}
	vars := parseDotenv(content)
	return rt.JSONResult(call, map[string]any{
		"vars":  vars,
		"count": len(vars),
	})
}

func handleTextJSONL(_ context.Context, rt Runtime, call agent.ToolCall) (contextengine.ToolResult, error) {
	content, err := textContent(rt, call.Input)
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("text.jsonl: %w", err)
	}
	query, _ := stringFrom(call.Input["query"])
	limit, err := intFrom(call.Input["limit"], 100)
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("text.jsonl: invalid limit: %w", err)
	}
	if limit <= 0 {
		limit = 100
	}

	var records []any
	lines := strings.Split(content, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var parsed any
		if err := json.Unmarshal([]byte(line), &parsed); err != nil {
			continue
		}
		if strings.TrimSpace(query) != "" {
			parsed = queryJSON(parsed, query)
		}
		records = append(records, parsed)
		if len(records) >= limit {
			break
		}
	}
	if records == nil {
		records = []any{}
	}
	return rt.JSONResult(call, map[string]any{
		"records": records,
		"count":   len(records),
	})
}

func handleTextHTML(_ context.Context, rt Runtime, call agent.ToolCall) (contextengine.ToolResult, error) {
	content, err := textContent(rt, call.Input)
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("text.html: %w", err)
	}
	mode, _ := stringFrom(call.Input["mode"])
	if mode == "" {
		mode = "text"
	}
	tagFilter, _ := stringFrom(call.Input["tag"])

	switch mode {
	case "links":
		links := extractHTMLLinks(content)
		return rt.JSONResult(call, map[string]any{
			"links": links,
			"count": len(links),
		})
	case "tags":
		if tagFilter == "" {
			return contextengine.ToolResult{}, fmt.Errorf("text.html: tag parameter is required in tags mode")
		}
		contents := extractHTMLTagContent(content, tagFilter)
		return rt.JSONResult(call, map[string]any{
			"content": strings.Join(contents, "\n"),
			"count":   len(contents),
		})
	default: // "text"
		if tagFilter != "" {
			contents := extractHTMLTagContent(content, tagFilter)
			text := strings.Join(contents, "\n")
			return rt.JSONResult(call, map[string]any{
				"content": stripHTMLTags(text),
			})
		}
		return rt.JSONResult(call, map[string]any{
			"content": stripHTMLTags(content),
		})
	}
}

func handleTextMarkdown(_ context.Context, rt Runtime, call agent.ToolCall) (contextengine.ToolResult, error) {
	content, err := textContent(rt, call.Input)
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("text.markdown: %w", err)
	}
	mode, _ := stringFrom(call.Input["mode"])
	if mode == "" {
		mode = "frontmatter"
	}
	section, _ := stringFrom(call.Input["section"])

	switch mode {
	case "frontmatter":
		fm := extractMarkdownFrontmatter(content)
		return rt.JSONResult(call, map[string]any{
			"frontmatter": fm,
		})
	case "toc":
		toc := buildMarkdownTOC(content)
		return rt.JSONResult(call, map[string]any{
			"toc": toc,
		})
	case "section":
		if strings.TrimSpace(section) == "" {
			return contextengine.ToolResult{}, fmt.Errorf("text.markdown: section parameter is required in section mode")
		}
		extracted := extractMarkdownSection(content, section)
		return rt.JSONResult(call, map[string]any{
			"content": extracted,
		})
	default:
		return contextengine.ToolResult{}, fmt.Errorf("text.markdown: unsupported mode %q (use frontmatter, toc, or section)", mode)
	}
}

func handleTextRegex(_ context.Context, rt Runtime, call agent.ToolCall) (contextengine.ToolResult, error) {
	pattern, err := requiredString(call.Input, "pattern")
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("text.regex: %w", err)
	}
	re, err := regexp.Compile(pattern)
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("text.regex: invalid pattern: %w", err)
	}

	content, err := textContent(rt, call.Input)
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("text.regex: %w", err)
	}

	mode, _ := stringFrom(call.Input["mode"])
	if mode == "" {
		mode = "match"
	}

	switch mode {
	case "match":
		matches := re.FindAllString(content, -1)
		if matches == nil {
			matches = []string{}
		}
		return rt.JSONResult(call, map[string]any{
			"matches": matches,
			"count":   len(matches),
		})
	case "extract":
		submatches := re.FindAllStringSubmatch(content, -1)
		var groups [][]string
		groups = append(groups, submatches...)
		if groups == nil {
			groups = [][]string{}
		}
		return rt.JSONResult(call, map[string]any{
			"groups": groups,
			"count":  len(groups),
		})
	case "replace":
		replacement, _ := stringFrom(call.Input["replace"])
		result := re.ReplaceAllString(content, replacement)
		return rt.JSONResult(call, map[string]any{
			"result": result,
		})
	case "split":
		parts := re.Split(content, -1)
		return rt.JSONResult(call, map[string]any{
			"parts": parts,
			"count": len(parts),
		})
	default:
		return contextengine.ToolResult{}, fmt.Errorf("text.regex: unsupported mode %q (use match, extract, replace, or split)", mode)
	}
}

func handleTextBase64(_ context.Context, rt Runtime, call agent.ToolCall) (contextengine.ToolResult, error) {
	input, err := requiredString(call.Input, "input")
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("text.base64: %w", err)
	}
	decode, _ := boolFrom(call.Input["decode"])
	if decode {
		decoded, err := base64.StdEncoding.DecodeString(input)
		if err != nil {
			decoded, err = base64.URLEncoding.DecodeString(input)
			if err != nil {
				decoded, err = base64.RawStdEncoding.DecodeString(input)
				if err != nil {
					return contextengine.ToolResult{}, fmt.Errorf("text.base64: decode failed: %w", err)
				}
			}
		}
		return rt.JSONResult(call, map[string]any{
			"result": string(decoded),
		})
	}
	encoded := base64.StdEncoding.EncodeToString([]byte(input))
	return rt.JSONResult(call, map[string]any{
		"result": encoded,
	})
}

func handleTextHex(_ context.Context, rt Runtime, call agent.ToolCall) (contextengine.ToolResult, error) {
	input, err := requiredString(call.Input, "input")
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("text.hex: %w", err)
	}
	decode, _ := boolFrom(call.Input["decode"])
	if decode {
		decoded, err := hex.DecodeString(input)
		if err != nil {
			return contextengine.ToolResult{}, fmt.Errorf("text.hex: decode failed: %w", err)
		}
		return rt.JSONResult(call, map[string]any{
			"result": string(decoded),
		})
	}
	encoded := hex.EncodeToString([]byte(input))
	return rt.JSONResult(call, map[string]any{
		"result": encoded,
	})
}

func handleTextURL(_ context.Context, rt Runtime, call agent.ToolCall) (contextengine.ToolResult, error) {
	input, err := requiredString(call.Input, "input")
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("text.url: %w", err)
	}
	mode, _ := stringFrom(call.Input["mode"])
	if mode == "" {
		mode = "parse"
	}

	switch mode {
	case "parse":
		u, err := url.Parse(input)
		if err != nil {
			return contextengine.ToolResult{}, fmt.Errorf("text.url: %w", err)
		}
		queryMap := make(map[string]any)
		for k, v := range u.Query() {
			if len(v) == 1 {
				queryMap[k] = v[0]
			} else {
				queryMap[k] = v
			}
		}
		return rt.JSONResult(call, map[string]any{
			"scheme":   u.Scheme,
			"host":     u.Host,
			"path":     u.Path,
			"query":    queryMap,
			"fragment": u.Fragment,
		})
	case "encode":
		return rt.JSONResult(call, map[string]any{
			"result": url.QueryEscape(input),
		})
	case "decode":
		decoded, err := url.QueryUnescape(input)
		if err != nil {
			return contextengine.ToolResult{}, fmt.Errorf("text.url: decode failed: %w", err)
		}
		return rt.JSONResult(call, map[string]any{
			"result": decoded,
		})
	default:
		return contextengine.ToolResult{}, fmt.Errorf("text.url: unsupported mode %q (use parse, encode, or decode)", mode)
	}
}

func handleTextUUID(_ context.Context, rt Runtime, call agent.ToolCall) (contextengine.ToolResult, error) {
	count, err := intFrom(call.Input["count"], 1)
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("text.uuid: invalid count: %w", err)
	}
	if count <= 0 {
		count = 1
	}
	if count > 1000 {
		count = 1000
	}

	if count == 1 {
		id, err := generateUUIDv4()
		if err != nil {
			return contextengine.ToolResult{}, fmt.Errorf("text.uuid: %w", err)
		}
		return rt.JSONResult(call, map[string]any{
			"uuid": id,
		})
	}

	uuids := make([]string, 0, count)
	for i := 0; i < count; i++ {
		id, err := generateUUIDv4()
		if err != nil {
			return contextengine.ToolResult{}, fmt.Errorf("text.uuid: %w", err)
		}
		uuids = append(uuids, id)
	}
	return rt.JSONResult(call, map[string]any{
		"uuids": uuids,
	})
}

func handleTextTemplate(_ context.Context, rt Runtime, call agent.ToolCall) (contextengine.ToolResult, error) {
	tmplStr, _ := stringFrom(call.Input["template"])
	fileValue, _ := stringFrom(call.Input["file"])

	if strings.TrimSpace(tmplStr) == "" && strings.TrimSpace(fileValue) != "" {
		resolved, err := rt.ResolvePath(fileValue)
		if err != nil {
			return contextengine.ToolResult{}, fmt.Errorf("text.template: %w", err)
		}
		data, err := os.ReadFile(resolved)
		if err != nil {
			return contextengine.ToolResult{}, fmt.Errorf("text.template: %w", err)
		}
		tmplStr = string(data)
	}
	if strings.TrimSpace(tmplStr) == "" {
		return contextengine.ToolResult{}, fmt.Errorf("text.template: template or file is required")
	}

	templateData, err := mapFrom(call.Input["data"])
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("text.template: invalid data: %w", err)
	}
	if templateData == nil {
		templateData = make(map[string]any)
	}

	tmpl, err := template.New("text.template").Option("missingkey=zero").Parse(tmplStr)
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("text.template: parse error: %w", err)
	}
	var buf strings.Builder
	if err := tmpl.Execute(&buf, templateData); err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("text.template: execute error: %w", err)
	}
	return rt.JSONResult(call, map[string]any{
		"result": buf.String(),
	})
}

func handleTextCount(_ context.Context, rt Runtime, call agent.ToolCall) (contextengine.ToolResult, error) {
	content, err := textContent(rt, call.Input)
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("text.count: %w", err)
	}

	lineCount := 0
	if len(content) > 0 {
		lineCount = strings.Count(content, "\n")
		if content[len(content)-1] != '\n' {
			lineCount++
		}
	}

	wordCount := len(strings.Fields(content))
	charCount := utf8.RuneCountInString(content)
	byteCount := len(content)

	return rt.JSONResult(call, map[string]any{
		"lines":      lineCount,
		"words":      wordCount,
		"characters": charCount,
		"bytes":      byteCount,
	})
}
