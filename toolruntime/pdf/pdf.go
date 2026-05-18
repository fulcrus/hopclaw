// Package pdf implements PDF tool handlers (pdf.info, pdf.extract_text,
// pdf.search, pdf.page_text, pdf.merge, pdf.watermark, pdf.create) for the
// toolruntime registry.
package pdf

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"

	"github.com/fulcrus/hopclaw/agent"
	"github.com/fulcrus/hopclaw/contextengine"
	"github.com/fulcrus/hopclaw/skill"

	pdflib "github.com/ledongthuc/pdf"
	pdfcpuapi "github.com/pdfcpu/pdfcpu/pkg/api"
	pdfcpumodel "github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"
	pdfcputypes "github.com/pdfcpu/pdfcpu/pkg/pdfcpu/types"
)

// Runtime is the narrow interface that pdf handlers need from *Builtins.
type Runtime interface {
	JSONResult(call agent.ToolCall, payload map[string]any) (contextengine.ToolResult, error)
	ResolvePath(input string) (string, error)
	DisplayPath(absPath string) string
}

// Handler is the tool handler signature for pdf tools.
type Handler func(ctx context.Context, rt Runtime, call agent.ToolCall) (contextengine.ToolResult, error)

// ToolDef pairs a tool manifest with a pdf handler.
type ToolDef struct {
	Manifest skill.ToolManifest
	Handler  Handler
}

// ---------------------------------------------------------------------------
// Constants
// ---------------------------------------------------------------------------

const (
	pdfDefaultPages          = "all"
	pdfSearchContextRadius   = 50
	pdfWatermarkDefaultOp    = 0.3
	pdfWatermarkDefaultRot   = 45
	pdfWatermarkDefaultScale = 1.0
)

// ToolDefs returns all pdf domain tool definitions.
func ToolDefs() []ToolDef {
	return []ToolDef{
		{
			Manifest: skill.ToolManifest{
				Name:            "pdf.info",
				Description:     "Return metadata about a PDF file: page count, file size, and whether it is encrypted.",
				InputSchema:     pdfInfoInputSchema(),
				OutputSchema:    pdfInfoOutputSchema(),
				SideEffectClass: "read",
				Idempotent:      true,
				ExecutionKey:    "pdf:info:{path}",
			},
			Handler: handlePDFInfo,
		},
		{
			Manifest: skill.ToolManifest{
				Name:            "pdf.extract_text",
				Description:     "Extract plain text from a PDF file. Optionally specify page ranges such as \"1-3,5\" or \"all\".",
				InputSchema:     pdfExtractTextInputSchema(),
				OutputSchema:    pdfExtractTextOutputSchema(),
				SideEffectClass: "read",
				Idempotent:      true,
				ExecutionKey:    "pdf:extract_text:{path}",
			},
			Handler: handlePDFExtractText,
		},
		{
			Manifest: skill.ToolManifest{
				Name:            "pdf.search",
				Description:     "Search for a text query in a PDF file, returning matching pages, line numbers, and surrounding context.",
				InputSchema:     pdfSearchInputSchema(),
				OutputSchema:    pdfSearchOutputSchema(),
				SideEffectClass: "read",
				Idempotent:      true,
				ExecutionKey:    "pdf:search:{path}",
			},
			Handler: handlePDFSearch,
		},
		{
			Manifest: skill.ToolManifest{
				Name:            "pdf.page_text",
				Description:     "Extract text from a single specific page of a PDF file (1-based page number).",
				InputSchema:     pdfPageTextInputSchema(),
				OutputSchema:    pdfPageTextOutputSchema(),
				SideEffectClass: "read",
				Idempotent:      true,
				ExecutionKey:    "pdf:page_text:{path}",
			},
			Handler: handlePDFPageText,
		},
		{
			Manifest: skill.ToolManifest{
				Name:             "pdf.merge",
				Description:      "Merge multiple PDF files into a single output PDF.",
				InputSchema:      pdfMergeInputSchema(),
				OutputSchema:     pdfMergeOutputSchema(),
				SideEffectClass:  "local_write",
				RequiresApproval: true,
			},
			Handler: handlePDFMerge,
		},
		{
			Manifest: skill.ToolManifest{
				Name:             "pdf.watermark",
				Description:      "Add a text watermark to all pages of a PDF file.",
				InputSchema:      pdfWatermarkInputSchema(),
				OutputSchema:     pdfWatermarkOutputSchema(),
				SideEffectClass:  "local_write",
				RequiresApproval: true,
			},
			Handler: handlePDFWatermark,
		},
		{
			Manifest: skill.ToolManifest{
				Name:             "pdf.create",
				Description:      "Create a PDF from text content. For HTML/rich content, use canvas.present + canvas.pdf instead.",
				InputSchema:      pdfCreateInputSchema(),
				OutputSchema:     pdfCreateOutputSchema(),
				SideEffectClass:  "local_write",
				RequiresApproval: true,
			},
			Handler: handlePDFCreate,
		},
	}
}

// ---------------------------------------------------------------------------
// Param helpers — duplicated locally to avoid importing toolruntime.
// ---------------------------------------------------------------------------

func stringFrom(value any) (string, error) {
	switch typed := value.(type) {
	case nil:
		return "", nil
	case string:
		return typed, nil
	default:
		return "", fmt.Errorf("expected string, got %T", value)
	}
}

func requiredString(input map[string]any, key string) (string, error) {
	value, err := stringFrom(input[key])
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(value) == "" {
		return "", fmt.Errorf("%s is required", key)
	}
	return value, nil
}

func stringFromDefault(value any, fallback string) string {
	s, err := stringFrom(value)
	if err != nil || strings.TrimSpace(s) == "" {
		return fallback
	}
	return s
}

func stringSliceFrom(value any) ([]string, error) {
	if value == nil {
		return nil, nil
	}
	switch typed := value.(type) {
	case []string:
		return typed, nil
	case []any:
		out := make([]string, 0, len(typed))
		for _, v := range typed {
			s, err := stringFrom(v)
			if err != nil {
				return nil, err
			}
			out = append(out, s)
		}
		return out, nil
	default:
		return nil, fmt.Errorf("expected string array, got %T", value)
	}
}

func intFrom(value any, fallback int) (int, error) {
	if value == nil {
		return fallback, nil
	}
	switch typed := value.(type) {
	case int:
		return typed, nil
	case int64:
		return int(typed), nil
	case float64:
		return int(typed), nil
	case string:
		if strings.TrimSpace(typed) == "" {
			return fallback, nil
		}
		var v int64
		_, err := fmt.Sscanf(typed, "%d", &v)
		if err != nil {
			return 0, err
		}
		return int(v), nil
	default:
		return 0, fmt.Errorf("expected integer, got %T", value)
	}
}

func floatFrom(value any, fallback float64) (float64, error) {
	switch typed := value.(type) {
	case nil:
		return fallback, nil
	case float64:
		return typed, nil
	case int:
		return float64(typed), nil
	case int64:
		return float64(typed), nil
	case string:
		if strings.TrimSpace(typed) == "" {
			return fallback, nil
		}
		return strconv.ParseFloat(typed, 64)
	default:
		return 0, fmt.Errorf("expected number, got %T", value)
	}
}

func boolFromDefault(value any, fallback bool) (bool, error) {
	if value == nil {
		return fallback, nil
	}
	switch typed := value.(type) {
	case bool:
		return typed, nil
	default:
		return false, fmt.Errorf("expected boolean, got %T", value)
	}
}

// ---------------------------------------------------------------------------
// Schema helpers
// ---------------------------------------------------------------------------

func stringSchema(description string) map[string]any {
	schema := map[string]any{"type": "string"}
	if description != "" {
		schema["description"] = description
	}
	return schema
}

func integerSchema(description string) map[string]any {
	schema := map[string]any{"type": "integer"}
	if description != "" {
		schema["description"] = description
	}
	return schema
}

func booleanSchema(description string) map[string]any {
	schema := map[string]any{"type": "boolean"}
	if description != "" {
		schema["description"] = description
	}
	return schema
}

func objectSchema(properties map[string]any, required ...string) map[string]any {
	schema := map[string]any{
		"type":                 "object",
		"properties":           properties,
		"additionalProperties": false,
	}
	if len(required) > 0 {
		schema["required"] = required
	}
	return schema
}

func arraySchema(items map[string]any, description string) map[string]any {
	schema := map[string]any{
		"type":  "array",
		"items": items,
	}
	if description != "" {
		schema["description"] = description
	}
	return schema
}

// ---------------------------------------------------------------------------
// Input schemas
// ---------------------------------------------------------------------------

func pdfInfoInputSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"path": map[string]any{"type": "string", "description": "Path to the PDF file."},
		},
		"required":             []string{"path"},
		"additionalProperties": false,
	}
}

func pdfExtractTextInputSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"path":  map[string]any{"type": "string", "description": "Path to the PDF file."},
			"pages": map[string]any{"type": "string", "description": "Page range to extract, e.g. \"1-3,5\" or \"all\". Defaults to \"all\"."},
		},
		"required":             []string{"path"},
		"additionalProperties": false,
	}
}

func pdfSearchInputSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"path":           map[string]any{"type": "string", "description": "Path to the PDF file."},
			"query":          map[string]any{"type": "string", "description": "Text to search for in the PDF."},
			"case_sensitive": map[string]any{"type": "boolean", "description": "Whether the search is case-sensitive. Defaults to false."},
		},
		"required":             []string{"path", "query"},
		"additionalProperties": false,
	}
}

func pdfPageTextInputSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"path": map[string]any{"type": "string", "description": "Path to the PDF file."},
			"page": map[string]any{"type": "integer", "description": "1-based page number to extract text from."},
		},
		"required":             []string{"path", "page"},
		"additionalProperties": false,
	}
}

func pdfMergeInputSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"paths":  map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "Array of PDF file paths to merge."},
			"output": map[string]any{"type": "string", "description": "Output file path for the merged PDF."},
		},
		"required":             []string{"paths", "output"},
		"additionalProperties": false,
	}
}

func pdfWatermarkInputSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"path":         map[string]any{"type": "string", "description": "Path to the PDF file."},
			"output":       map[string]any{"type": "string", "description": "Output file path for the watermarked PDF."},
			"text":         map[string]any{"type": "string", "description": "Watermark text to add."},
			"opacity":      map[string]any{"type": "number", "description": "Watermark opacity from 0.0 to 1.0. Defaults to 0.3."},
			"rotation":     map[string]any{"type": "integer", "description": "Watermark rotation in degrees. Defaults to 45."},
			"scale_factor": map[string]any{"type": "number", "description": "Watermark scale factor. Defaults to 1.0."},
		},
		"required":             []string{"path", "output", "text"},
		"additionalProperties": false,
	}
}

func pdfCreateInputSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"content": map[string]any{"type": "string", "description": "Text content to write into the PDF."},
			"output":  map[string]any{"type": "string", "description": "Output file path for the created PDF."},
			"format":  map[string]any{"type": "string", "description": "Content format: \"text\", \"html\", or \"markdown\". Only \"text\" is natively supported; html/markdown require the browser service (use canvas.present + canvas.pdf instead). Defaults to \"text\"."},
		},
		"required":             []string{"content", "output"},
		"additionalProperties": false,
	}
}

// ---------------------------------------------------------------------------
// Output schemas
// ---------------------------------------------------------------------------

func pdfInfoOutputSchema() map[string]any {
	return objectSchema(map[string]any{
		"path":       stringSchema("Path to the PDF file."),
		"page_count": integerSchema("Total number of pages in the PDF."),
		"encrypted":  booleanSchema("Whether the PDF is encrypted."),
		"file_size":  integerSchema("File size in bytes."),
	}, "path", "page_count", "encrypted", "file_size")
}

func pdfExtractTextOutputSchema() map[string]any {
	return objectSchema(map[string]any{
		"text":            stringSchema("Extracted plain text."),
		"page_count":      integerSchema("Total number of pages in the PDF."),
		"pages_extracted": integerSchema("Number of pages from which text was extracted."),
	}, "text", "page_count", "pages_extracted")
}

func pdfSearchOutputSchema() map[string]any {
	match := objectSchema(map[string]any{
		"page":    integerSchema("1-based page number where the match was found."),
		"line":    integerSchema("1-based line number within the page."),
		"context": stringSchema("Surrounding text context for the match."),
	}, "page", "line", "context")
	return objectSchema(map[string]any{
		"matches":       arraySchema(match, "List of search matches."),
		"total_matches": integerSchema("Total number of matches found."),
	}, "matches", "total_matches")
}

func pdfPageTextOutputSchema() map[string]any {
	return objectSchema(map[string]any{
		"text": stringSchema("Extracted plain text from the requested page."),
		"page": integerSchema("1-based page number that was extracted."),
	}, "text", "page")
}

func pdfMergeOutputSchema() map[string]any {
	return objectSchema(map[string]any{
		"path":        stringSchema("Output file path."),
		"page_count":  integerSchema("Total page count in the merged PDF."),
		"input_count": integerSchema("Number of input files merged."),
		"bytes":       integerSchema("Output file size in bytes."),
	}, "path", "page_count", "input_count", "bytes")
}

func pdfWatermarkOutputSchema() map[string]any {
	return objectSchema(map[string]any{
		"path":  stringSchema("Output file path."),
		"bytes": integerSchema("Output file size in bytes."),
	}, "path", "bytes")
}

func pdfCreateOutputSchema() map[string]any {
	return objectSchema(map[string]any{
		"path":  stringSchema("Output file path."),
		"bytes": integerSchema("Output file size in bytes."),
	}, "path", "bytes")
}

// ---------------------------------------------------------------------------
// Handlers
// ---------------------------------------------------------------------------

func handlePDFInfo(_ context.Context, rt Runtime, call agent.ToolCall) (contextengine.ToolResult, error) {
	path, err := requiredString(call.Input, "path")
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("pdf.info: %w", err)
	}
	resolvedPath, err := rt.ResolvePath(path)
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("pdf.info: %w", err)
	}
	fi, err := os.Stat(resolvedPath)
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("pdf.info: %w", err)
	}
	f, reader, err := pdflib.Open(resolvedPath)
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("pdf.info: %w", err)
	}
	defer f.Close()
	encrypted := !reader.Trailer().Key("Encrypt").IsNull()
	return rt.JSONResult(call, map[string]any{
		"path":       rt.DisplayPath(resolvedPath),
		"page_count": reader.NumPage(),
		"encrypted":  encrypted,
		"file_size":  fi.Size(),
	})
}

func handlePDFExtractText(_ context.Context, rt Runtime, call agent.ToolCall) (contextengine.ToolResult, error) {
	path, err := requiredString(call.Input, "path")
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("pdf.extract_text: %w", err)
	}
	pagesSpec, _ := stringFrom(call.Input["pages"])
	if strings.TrimSpace(pagesSpec) == "" {
		pagesSpec = pdfDefaultPages
	}
	resolvedPath, err := rt.ResolvePath(path)
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("pdf.extract_text: %w", err)
	}
	f, reader, err := pdflib.Open(resolvedPath)
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("pdf.extract_text: %w", err)
	}
	defer f.Close()
	totalPages := reader.NumPage()
	pageNums, err := parsePageRange(pagesSpec, totalPages)
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("pdf.extract_text: %w", err)
	}
	var sb strings.Builder
	pagesExtracted := 0
	for _, pageNum := range pageNums {
		text, extractErr := extractPageText(reader, pageNum)
		if extractErr != nil {
			return contextengine.ToolResult{}, fmt.Errorf("pdf.extract_text: page %d: %w", pageNum, extractErr)
		}
		if sb.Len() > 0 && len(text) > 0 {
			sb.WriteString("\n")
		}
		sb.WriteString(text)
		pagesExtracted++
	}
	return rt.JSONResult(call, map[string]any{
		"text":            sb.String(),
		"page_count":      totalPages,
		"pages_extracted": pagesExtracted,
	})
}

func handlePDFSearch(_ context.Context, rt Runtime, call agent.ToolCall) (contextengine.ToolResult, error) {
	path, err := requiredString(call.Input, "path")
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("pdf.search: %w", err)
	}
	query, err := requiredString(call.Input, "query")
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("pdf.search: %w", err)
	}
	caseSensitive, err := boolFromDefault(call.Input["case_sensitive"], false)
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("pdf.search: %w", err)
	}
	resolvedPath, err := rt.ResolvePath(path)
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("pdf.search: %w", err)
	}
	f, reader, err := pdflib.Open(resolvedPath)
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("pdf.search: %w", err)
	}
	defer f.Close()
	totalPages := reader.NumPage()
	var matches []map[string]any
	for pageNum := 1; pageNum <= totalPages; pageNum++ {
		text, extractErr := extractPageText(reader, pageNum)
		if extractErr != nil {
			continue
		}
		lines := strings.Split(text, "\n")
		for lineIdx, line := range lines {
			haystack := line
			needle := query
			if !caseSensitive {
				haystack = strings.ToLower(haystack)
				needle = strings.ToLower(needle)
			}
			searchPos := 0
			for {
				idx := strings.Index(haystack[searchPos:], needle)
				if idx < 0 {
					break
				}
				absIdx := searchPos + idx
				ctxText := pdfSearchContext(line, absIdx, len(query))
				matches = append(matches, map[string]any{
					"page":    pageNum,
					"line":    lineIdx + 1,
					"context": ctxText,
				})
				searchPos = absIdx + len(needle)
			}
		}
	}
	return rt.JSONResult(call, map[string]any{
		"matches":       matches,
		"total_matches": len(matches),
	})
}

func handlePDFPageText(_ context.Context, rt Runtime, call agent.ToolCall) (contextengine.ToolResult, error) {
	path, err := requiredString(call.Input, "path")
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("pdf.page_text: %w", err)
	}
	pageNum, err := intFrom(call.Input["page"], 0)
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("pdf.page_text: %w", err)
	}
	if pageNum <= 0 {
		return contextengine.ToolResult{}, fmt.Errorf("pdf.page_text: page must be a positive integer")
	}
	resolvedPath, err := rt.ResolvePath(path)
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("pdf.page_text: %w", err)
	}
	f, reader, err := pdflib.Open(resolvedPath)
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("pdf.page_text: %w", err)
	}
	defer f.Close()
	totalPages := reader.NumPage()
	if pageNum > totalPages {
		return contextengine.ToolResult{}, fmt.Errorf("pdf.page_text: page %d out of range (document has %d pages)", pageNum, totalPages)
	}
	text, err := extractPageText(reader, pageNum)
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("pdf.page_text: page %d: %w", pageNum, err)
	}
	return rt.JSONResult(call, map[string]any{
		"text": text,
		"page": pageNum,
	})
}

func handlePDFMerge(_ context.Context, rt Runtime, call agent.ToolCall) (contextengine.ToolResult, error) {
	paths, err := stringSliceFrom(call.Input["paths"])
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("pdf.merge: %w", err)
	}
	if len(paths) < 2 {
		return contextengine.ToolResult{}, fmt.Errorf("pdf.merge: at least 2 input paths are required")
	}
	output, err := requiredString(call.Input, "output")
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("pdf.merge: %w", err)
	}
	resolvedPaths := make([]string, len(paths))
	for i, p := range paths {
		resolved, resolveErr := rt.ResolvePath(p)
		if resolveErr != nil {
			return contextengine.ToolResult{}, fmt.Errorf("pdf.merge: input %d: %w", i, resolveErr)
		}
		resolvedPaths[i] = resolved
	}
	resolvedOutput, err := rt.ResolvePath(output)
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("pdf.merge: output: %w", err)
	}
	if mergeErr := pdfcpuapi.MergeCreateFile(resolvedPaths, resolvedOutput, false, nil); mergeErr != nil {
		return contextengine.ToolResult{}, fmt.Errorf("pdf.merge: %w", mergeErr)
	}
	fi, err := os.Stat(resolvedOutput)
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("pdf.merge: stat output: %w", err)
	}
	pageCount := 0
	f, reader, openErr := pdflib.Open(resolvedOutput)
	if openErr == nil {
		pageCount = reader.NumPage()
		f.Close()
	}
	return rt.JSONResult(call, map[string]any{
		"path":        rt.DisplayPath(resolvedOutput),
		"page_count":  pageCount,
		"input_count": len(paths),
		"bytes":       fi.Size(),
	})
}

func handlePDFWatermark(_ context.Context, rt Runtime, call agent.ToolCall) (contextengine.ToolResult, error) {
	path, err := requiredString(call.Input, "path")
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("pdf.watermark: %w", err)
	}
	output, err := requiredString(call.Input, "output")
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("pdf.watermark: %w", err)
	}
	text, err := requiredString(call.Input, "text")
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("pdf.watermark: %w", err)
	}
	opacity, err := floatFrom(call.Input["opacity"], pdfWatermarkDefaultOp)
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("pdf.watermark: %w", err)
	}
	rotation, err := intFrom(call.Input["rotation"], pdfWatermarkDefaultRot)
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("pdf.watermark: %w", err)
	}
	scaleFactor, err := floatFrom(call.Input["scale_factor"], pdfWatermarkDefaultScale)
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("pdf.watermark: %w", err)
	}
	resolvedPath, err := rt.ResolvePath(path)
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("pdf.watermark: %w", err)
	}
	resolvedOutput, err := rt.ResolvePath(output)
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("pdf.watermark: %w", err)
	}
	desc := fmt.Sprintf("op:%.2f, rot:%d, scale:%.1f abs", opacity, rotation, scaleFactor)
	if wmErr := pdfcpuapi.AddTextWatermarksFile(resolvedPath, resolvedOutput, nil, false, text, desc, nil); wmErr != nil {
		return contextengine.ToolResult{}, fmt.Errorf("pdf.watermark: %w", wmErr)
	}
	fi, err := os.Stat(resolvedOutput)
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("pdf.watermark: stat output: %w", err)
	}
	return rt.JSONResult(call, map[string]any{
		"path":  rt.DisplayPath(resolvedOutput),
		"bytes": fi.Size(),
	})
}

func handlePDFCreate(_ context.Context, rt Runtime, call agent.ToolCall) (contextengine.ToolResult, error) {
	content, err := requiredString(call.Input, "content")
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("pdf.create: %w", err)
	}
	output, err := requiredString(call.Input, "output")
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("pdf.create: %w", err)
	}
	format := stringFromDefault(call.Input["format"], "text")
	if format == "html" || format == "markdown" {
		return contextengine.ToolResult{}, fmt.Errorf("pdf.create: %s format requires the browser service; use canvas.present + canvas.pdf directly", format)
	}
	resolvedOutput, err := rt.ResolvePath(output)
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("pdf.create: output: %w", err)
	}
	jsonPayload := pdfCreateTextJSON(content)
	jsonReader := strings.NewReader(string(jsonPayload))
	outFile, err := os.Create(resolvedOutput)
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("pdf.create: %w", err)
	}
	defer outFile.Close()
	if createErr := pdfcpuapi.Create(nil, jsonReader, outFile, nil); createErr != nil {
		outFile.Close()
		os.Remove(resolvedOutput)
		return contextengine.ToolResult{}, fmt.Errorf("pdf.create: %w", createErr)
	}
	fi, statErr := os.Stat(resolvedOutput)
	if statErr != nil {
		return contextengine.ToolResult{}, fmt.Errorf("pdf.create: stat output: %w", statErr)
	}
	return rt.JSONResult(call, map[string]any{
		"path":  rt.DisplayPath(resolvedOutput),
		"bytes": fi.Size(),
	})
}

// pdfCreateTextJSON builds a pdfcpu JSON page description for simple text content.
func pdfCreateTextJSON(content string) []byte {
	_ = pdfcputypes.PaperSize
	_ = (*pdfcpumodel.Configuration)(nil)

	payload := map[string]any{
		"paper": "A4",
		"pages": []map[string]any{
			{
				"content": map[string]any{
					"text": []map[string]any{
						{
							"value":    content,
							"position": [2]int{50, 750},
							"font": map[string]any{
								"name": "Helvetica",
								"size": 12,
							},
						},
					},
				},
			},
		},
	}
	data, _ := json.Marshal(payload)
	return data
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func parsePageRange(spec string, totalPages int) ([]int, error) {
	spec = strings.TrimSpace(spec)
	if strings.EqualFold(spec, pdfDefaultPages) || spec == "" {
		pages := make([]int, totalPages)
		for i := range pages {
			pages[i] = i + 1
		}
		return pages, nil
	}
	seen := make(map[int]bool)
	parts := strings.Split(spec, ",")
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		if dashIdx := strings.Index(part, "-"); dashIdx >= 0 {
			startStr := strings.TrimSpace(part[:dashIdx])
			endStr := strings.TrimSpace(part[dashIdx+1:])
			start, err := strconv.Atoi(startStr)
			if err != nil {
				return nil, fmt.Errorf("invalid page number %q", startStr)
			}
			end, err := strconv.Atoi(endStr)
			if err != nil {
				return nil, fmt.Errorf("invalid page number %q", endStr)
			}
			if start > end {
				return nil, fmt.Errorf("invalid page range %q: start exceeds end", part)
			}
			if start < 1 {
				return nil, fmt.Errorf("page number must be >= 1, got %d", start)
			}
			if end > totalPages {
				return nil, fmt.Errorf("page %d out of range (document has %d pages)", end, totalPages)
			}
			for p := start; p <= end; p++ {
				seen[p] = true
			}
		} else {
			n, err := strconv.Atoi(part)
			if err != nil {
				return nil, fmt.Errorf("invalid page number %q", part)
			}
			if n < 1 {
				return nil, fmt.Errorf("page number must be >= 1, got %d", n)
			}
			if n > totalPages {
				return nil, fmt.Errorf("page %d out of range (document has %d pages)", n, totalPages)
			}
			seen[n] = true
		}
	}
	pages := make([]int, 0, len(seen))
	for p := range seen {
		pages = append(pages, p)
	}
	sort.Ints(pages)
	return pages, nil
}

func extractPageText(reader *pdflib.Reader, pageNum int) (text string, err error) {
	defer func() {
		if r := recover(); r != nil {
			text = ""
			err = fmt.Errorf("failed to extract text: %v", r)
		}
	}()
	page := reader.Page(pageNum)
	if page.V.IsNull() {
		return "", nil
	}
	fonts := make(map[string]*pdflib.Font)
	for _, name := range page.Fonts() {
		f := page.Font(name)
		fonts[name] = &f
	}
	result, err := page.GetPlainText(fonts)
	if err != nil {
		return "", err
	}
	return result, nil
}

func pdfSearchContext(line string, matchStart, matchLen int) string {
	start := matchStart - pdfSearchContextRadius
	if start < 0 {
		start = 0
	}
	end := matchStart + matchLen + pdfSearchContextRadius
	if end > len(line) {
		end = len(line)
	}
	ctx := line[start:end]
	if start > 0 {
		ctx = "..." + ctx
	}
	if end < len(line) {
		ctx = ctx + "..."
	}
	return ctx
}
