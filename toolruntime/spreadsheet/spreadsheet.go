// Package spreadsheet implements spreadsheet tool handlers (spreadsheet.read_range,
// spreadsheet.write_range, spreadsheet.export, spreadsheet.list_sheets,
// spreadsheet.create, spreadsheet.set_style) for the toolruntime registry.
package spreadsheet

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/fulcrus/hopclaw/agent"
	"github.com/fulcrus/hopclaw/contextengine"
	"github.com/fulcrus/hopclaw/skill"
)

// Runtime is the narrow interface that spreadsheet handlers need from *Builtins.
type Runtime interface {
	JSONResult(call agent.ToolCall, payload map[string]any) (contextengine.ToolResult, error)
	ResolvePath(input string) (string, error)
	DisplayPath(absPath string) string
}

// Handler is the tool handler signature, parameterized on our narrow Runtime interface.
type Handler func(ctx context.Context, rt Runtime, call agent.ToolCall) (contextengine.ToolResult, error)

// ToolDef pairs a tool manifest with a spreadsheet handler.
type ToolDef struct {
	Manifest skill.ToolManifest
	Handler  Handler
}

// ToolDefs returns all spreadsheet domain tool definitions (CSV/TSV core).
func ToolDefs() []ToolDef {
	return []ToolDef{
		{
			Manifest: skill.ToolManifest{
				Name:            "spreadsheet.read_range",
				Description:     "Read a cell range from a CSV, TSV, or XLSX spreadsheet file.",
				InputSchema:     spreadsheetReadSchema(),
				OutputSchema:    spreadsheetReadOutputSchema(),
				SideEffectClass: "read",
				Idempotent:      true,
				ExecutionKey:    "spreadsheet:{path}",
			},
			Handler: handleSpreadsheetReadRange,
		},
		{
			Manifest: skill.ToolManifest{
				Name:             "spreadsheet.write_range",
				Description:      "Write values into a cell range in a CSV, TSV, or XLSX spreadsheet file.",
				InputSchema:      spreadsheetWriteSchema(),
				OutputSchema:     spreadsheetWriteOutputSchema(),
				SideEffectClass:  "local_write",
				RequiresApproval: true,
				ExecutionKey:     "spreadsheet:{path}",
			},
			Handler: handleSpreadsheetWriteRange,
		},
		{
			Manifest: skill.ToolManifest{
				Name:             "spreadsheet.export",
				Description:      "Export a CSV, TSV, or XLSX spreadsheet file to CSV, TSV, JSON, or Markdown.",
				InputSchema:      spreadsheetExportSchema(),
				OutputSchema:     spreadsheetExportOutputSchema(),
				SideEffectClass:  "local_write",
				RequiresApproval: true,
				ExecutionKey:     "spreadsheet:{path}",
			},
			Handler: handleSpreadsheetExport,
		},
	}
}

// XLSXToolDefs returns XLSX-specific tool definitions.
func XLSXToolDefs() []ToolDef {
	return []ToolDef{
		{
			Manifest: skill.ToolManifest{
				Name:            "spreadsheet.list_sheets",
				Description:     "List all sheet names in an XLSX workbook.",
				InputSchema:     xlsxListSheetsSchema(),
				OutputSchema:    xlsxListSheetsOutputSchema(),
				SideEffectClass: "read",
				Idempotent:      true,
				ExecutionKey:    "spreadsheet:{path}",
			},
			Handler: handleXLSXListSheets,
		},
		{
			Manifest: skill.ToolManifest{
				Name:             "spreadsheet.create",
				Description:      "Create a new XLSX workbook with optional sheet names and data.",
				InputSchema:      xlsxCreateSchema(),
				OutputSchema:     xlsxCreateOutputSchema(),
				SideEffectClass:  "local_write",
				RequiresApproval: true,
				ExecutionKey:     "spreadsheet:{path}",
			},
			Handler: handleXLSXCreate,
		},
		{
			Manifest: skill.ToolManifest{
				Name:             "spreadsheet.set_style",
				Description:      "Set cell style (font, color, border, number format) on a range in an XLSX workbook.",
				InputSchema:      xlsxSetStyleSchema(),
				OutputSchema:     xlsxSetStyleOutputSchema(),
				SideEffectClass:  "local_write",
				RequiresApproval: true,
				ExecutionKey:     "spreadsheet:{path}",
			},
			Handler: handleXLSXSetStyle,
		},
	}
}

// ---------------------------------------------------------------------------
// Input schemas
// ---------------------------------------------------------------------------

func spreadsheetReadSchema() map[string]any {
	return objectSchema(map[string]any{
		"path":      stringSchema("Spreadsheet file path (.csv, .tsv, or .xlsx)."),
		"range":     stringSchema("Optional A1-style cell range like A1:C10. Defaults to the full sheet."),
		"header":    booleanSchema("Whether to treat the first row of the selected range as headers."),
		"delimiter": stringSchema("Optional delimiter override. Supported: comma, tab, semicolon. Ignored for XLSX."),
		"sheet":     stringSchema("Sheet name for XLSX workbooks. Defaults to the active sheet. Ignored for CSV/TSV."),
	}, "path")
}

func spreadsheetWriteSchema() map[string]any {
	return objectSchema(map[string]any{
		"path":              stringSchema("Spreadsheet file path (.csv, .tsv, or .xlsx)."),
		"range":             stringSchema("Top-left cell or exact A1-style range to write into."),
		"values":            arraySchema(map[string]any{"type": "array", "items": map[string]any{}}, "2D array of cell values."),
		"delimiter":         stringSchema("Optional delimiter override. Supported: comma, tab, semicolon. Ignored for XLSX."),
		"create_if_missing": booleanSchema("Whether to create the file if it does not exist. Defaults to true."),
		"sheet":             stringSchema("Sheet name for XLSX workbooks. Defaults to the active sheet. Ignored for CSV/TSV."),
	}, "path", "range", "values")
}

func spreadsheetExportSchema() map[string]any {
	return objectSchema(map[string]any{
		"path":      stringSchema("Spreadsheet file path (.csv, .tsv, or .xlsx)."),
		"output":    stringSchema("Output file path."),
		"format":    stringSchema("Export format: csv, tsv, json, markdown."),
		"range":     stringSchema("Optional A1-style cell range to export."),
		"header":    booleanSchema("Whether to treat the first row of the selected range as headers for JSON export."),
		"delimiter": stringSchema("Optional delimiter override for reading the source. Ignored for XLSX."),
		"sheet":     stringSchema("Sheet name for XLSX workbooks. Defaults to the active sheet. Ignored for CSV/TSV."),
	}, "path", "output", "format")
}

// ---------------------------------------------------------------------------
// Output schemas
// ---------------------------------------------------------------------------

func spreadsheetReadOutputSchema() map[string]any {
	return objectSchema(map[string]any{
		"path":         stringSchema("Spreadsheet path."),
		"range":        stringSchema("Resolved range."),
		"headers":      arraySchema(stringSchema("Column header."), "Headers from the first row when header=true."),
		"rows":         arraySchema(map[string]any{"type": "array", "items": stringSchema("Cell value.")}, "Selected rows."),
		"objects":      arraySchema(map[string]any{"type": "object"}, "Row objects when header=true."),
		"row_count":    integerSchema("Number of returned rows."),
		"column_count": integerSchema("Number of returned columns."),
	}, "path", "range", "rows", "row_count", "column_count")
}

func spreadsheetWriteOutputSchema() map[string]any {
	return objectSchema(map[string]any{
		"path":         stringSchema("Spreadsheet path."),
		"range":        stringSchema("Resolved range written."),
		"row_count":    integerSchema("Number of rows written."),
		"column_count": integerSchema("Number of columns written."),
		"created":      booleanSchema("Whether the file was newly created."),
	}, "path", "range", "row_count", "column_count", "created")
}

func spreadsheetExportOutputSchema() map[string]any {
	return objectSchema(map[string]any{
		"path":      stringSchema("Source spreadsheet path."),
		"output":    stringSchema("Exported file path."),
		"format":    stringSchema("Export format."),
		"range":     stringSchema("Resolved exported range."),
		"bytes":     integerSchema("Bytes written."),
		"row_count": integerSchema("Rows exported."),
	}, "path", "output", "format", "range", "bytes", "row_count")
}

// ---------------------------------------------------------------------------
// Schema helpers — duplicated locally to avoid importing toolruntime.
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

func stringArraySchema(description string) map[string]any {
	return arraySchema(stringSchema(""), description)
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

func optionalString(input map[string]any, key string) string {
	value, _ := stringFrom(input[key])
	return strings.TrimSpace(value)
}

func boolFrom(value any) (bool, error) {
	if value == nil {
		return false, nil
	}
	switch typed := value.(type) {
	case bool:
		return typed, nil
	default:
		return false, fmt.Errorf("expected boolean, got %T", value)
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

// ---------------------------------------------------------------------------
// Handlers
// ---------------------------------------------------------------------------

func handleSpreadsheetReadRange(_ context.Context, rt Runtime, call agent.ToolCall) (contextengine.ToolResult, error) {
	path, err := requiredString(call.Input, "path")
	if err != nil {
		return contextengine.ToolResult{}, err
	}
	resolved, err := rt.ResolvePath(path)
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("spreadsheet.read_range: %w", err)
	}
	var grid [][]string
	if isXLSXFile(resolved) {
		grid, err = readXLSXFile(resolved, optionalString(call.Input, "sheet"))
	} else {
		grid, err = readSpreadsheetFile(resolved, spreadsheetDelimiter(call.Input, resolved))
	}
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("spreadsheet.read_range: %w", err)
	}
	selected, resolvedRange, err := spreadsheetSlice(grid, optionalString(call.Input, "range"))
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("spreadsheet.read_range: %w", err)
	}
	header, err := boolFrom(call.Input["header"])
	if err != nil && call.Input["header"] != nil {
		return contextengine.ToolResult{}, fmt.Errorf("spreadsheet.read_range: header: %w", err)
	}
	payload := spreadsheetReadPayload(rt.DisplayPath(resolved), resolvedRange, selected, header)
	return rt.JSONResult(call, payload)
}

func handleSpreadsheetWriteRange(_ context.Context, rt Runtime, call agent.ToolCall) (contextengine.ToolResult, error) {
	path, err := requiredString(call.Input, "path")
	if err != nil {
		return contextengine.ToolResult{}, err
	}
	rangeRef, err := requiredString(call.Input, "range")
	if err != nil {
		return contextengine.ToolResult{}, err
	}
	values, err := spreadsheetValues(call.Input["values"])
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("spreadsheet.write_range: %w", err)
	}
	if len(values) == 0 {
		return contextengine.ToolResult{}, fmt.Errorf("spreadsheet.write_range: values must not be empty")
	}
	resolved, err := rt.ResolvePath(path)
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("spreadsheet.write_range: %w", err)
	}
	xlsx := isXLSXFile(resolved)
	delimiter := ','
	if !xlsx {
		delimiter = spreadsheetDelimiter(call.Input, resolved)
	}
	createIfMissing := true
	if call.Input["create_if_missing"] != nil {
		createIfMissing, err = boolFrom(call.Input["create_if_missing"])
		if err != nil {
			return contextengine.ToolResult{}, fmt.Errorf("spreadsheet.write_range: create_if_missing: %w", err)
		}
	}

	created := false
	grid := [][]string{}
	sheetName := optionalString(call.Input, "sheet")
	if _, statErr := os.Stat(resolved); statErr == nil {
		if xlsx {
			if sheetName == "" {
				grid, err = readXLSXFile(resolved, "")
				if err != nil {
					return contextengine.ToolResult{}, fmt.Errorf("spreadsheet.write_range: %w", err)
				}
			} else {
				exists, sheetErr := xlsxSheetExists(resolved, sheetName)
				if sheetErr != nil {
					return contextengine.ToolResult{}, fmt.Errorf("spreadsheet.write_range: %w", sheetErr)
				}
				if exists {
					grid, err = readXLSXFile(resolved, sheetName)
					if err != nil {
						return contextengine.ToolResult{}, fmt.Errorf("spreadsheet.write_range: %w", err)
					}
				}
			}
		} else {
			grid, err = readSpreadsheetFile(resolved, delimiter)
			if err != nil {
				return contextengine.ToolResult{}, fmt.Errorf("spreadsheet.write_range: %w", err)
			}
		}
	} else if !os.IsNotExist(statErr) {
		return contextengine.ToolResult{}, fmt.Errorf("spreadsheet.write_range: %w", statErr)
	} else if createIfMissing {
		created = true
	} else {
		return contextengine.ToolResult{}, fmt.Errorf("spreadsheet.write_range: file does not exist")
	}

	target, err := parseSpreadsheetRange(rangeRef)
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("spreadsheet.write_range: %w", err)
	}
	if target.hasEnd {
		expectedRows := target.endRow - target.startRow + 1
		expectedCols := target.endCol - target.startCol + 1
		if len(values) != expectedRows || spreadsheetMaxCols(values) != expectedCols {
			return contextengine.ToolResult{}, fmt.Errorf("spreadsheet.write_range: values shape %dx%d does not match range %s", len(values), spreadsheetMaxCols(values), rangeRef)
		}
	} else {
		target.endRow = target.startRow + len(values) - 1
		target.endCol = target.startCol + spreadsheetMaxCols(values) - 1
	}
	grid = spreadsheetEnsureSize(grid, target.endRow+1, target.endCol+1)
	for r := range values {
		for c := range values[r] {
			grid[target.startRow+r][target.startCol+c] = values[r][c]
		}
	}
	if xlsx {
		if err := writeXLSXFile(resolved, sheetName, grid); err != nil {
			return contextengine.ToolResult{}, fmt.Errorf("spreadsheet.write_range: %w", err)
		}
	} else {
		if err := writeSpreadsheetFile(resolved, delimiter, grid); err != nil {
			return contextengine.ToolResult{}, fmt.Errorf("spreadsheet.write_range: %w", err)
		}
	}
	return rt.JSONResult(call, map[string]any{
		"path":         rt.DisplayPath(resolved),
		"range":        spreadsheetRangeString(target),
		"row_count":    len(values),
		"column_count": spreadsheetMaxCols(values),
		"created":      created,
	})
}

func handleSpreadsheetExport(_ context.Context, rt Runtime, call agent.ToolCall) (contextengine.ToolResult, error) {
	path, err := requiredString(call.Input, "path")
	if err != nil {
		return contextengine.ToolResult{}, err
	}
	output, err := requiredString(call.Input, "output")
	if err != nil {
		return contextengine.ToolResult{}, err
	}
	format, err := requiredString(call.Input, "format")
	if err != nil {
		return contextengine.ToolResult{}, err
	}
	resolvedInput, err := rt.ResolvePath(path)
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("spreadsheet.export: %w", err)
	}
	resolvedOutput, err := rt.ResolvePath(output)
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("spreadsheet.export: %w", err)
	}
	var grid [][]string
	if isXLSXFile(resolvedInput) {
		grid, err = readXLSXFile(resolvedInput, optionalString(call.Input, "sheet"))
	} else {
		grid, err = readSpreadsheetFile(resolvedInput, spreadsheetDelimiter(call.Input, resolvedInput))
	}
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("spreadsheet.export: %w", err)
	}
	selected, resolvedRange, err := spreadsheetSlice(grid, optionalString(call.Input, "range"))
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("spreadsheet.export: %w", err)
	}
	header, err := boolFrom(call.Input["header"])
	if err != nil && call.Input["header"] != nil {
		return contextengine.ToolResult{}, fmt.Errorf("spreadsheet.export: header: %w", err)
	}
	content, err := spreadsheetExportContent(strings.ToLower(strings.TrimSpace(format)), selected, header)
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("spreadsheet.export: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(resolvedOutput), 0o755); err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("spreadsheet.export: %w", err)
	}
	if err := os.WriteFile(resolvedOutput, []byte(content), 0o644); err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("spreadsheet.export: %w", err)
	}
	return rt.JSONResult(call, map[string]any{
		"path":      rt.DisplayPath(resolvedInput),
		"output":    rt.DisplayPath(resolvedOutput),
		"format":    strings.ToLower(strings.TrimSpace(format)),
		"range":     resolvedRange,
		"bytes":     len(content),
		"row_count": len(selected),
	})
}

// ---------------------------------------------------------------------------
// Spreadsheet helpers
// ---------------------------------------------------------------------------

type spreadsheetRange struct {
	startRow int
	startCol int
	endRow   int
	endCol   int
	hasEnd   bool
}

func spreadsheetReadPayload(path string, resolvedRange string, rows [][]string, header bool) map[string]any {
	columnCount := spreadsheetMaxCols(rows)
	payload := map[string]any{
		"path":         path,
		"range":        resolvedRange,
		"rows":         rows,
		"row_count":    len(rows),
		"column_count": columnCount,
	}
	if header && len(rows) > 0 {
		headers := append([]string(nil), rows[0]...)
		objects := make([]map[string]string, 0, max(len(rows)-1, 0))
		for _, row := range rows[1:] {
			entry := make(map[string]string, len(headers))
			for idx, key := range headers {
				if idx < len(row) {
					entry[key] = row[idx]
				} else {
					entry[key] = ""
				}
			}
			objects = append(objects, entry)
		}
		payload["headers"] = headers
		payload["objects"] = objects
	}
	return payload
}

func spreadsheetDelimiter(input map[string]any, path string) rune {
	value := strings.ToLower(strings.TrimSpace(optionalString(input, "delimiter")))
	switch value {
	case "tab", "tsv":
		return '\t'
	case "semicolon", ";":
		return ';'
	case "comma", ",", "":
	default:
		return ','
	}
	ext := strings.ToLower(filepath.Ext(path))
	if ext == ".tsv" {
		return '\t'
	}
	return ','
}

func readSpreadsheetFile(path string, delimiter rune) ([][]string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	reader := csv.NewReader(strings.NewReader(string(data)))
	reader.Comma = delimiter
	reader.FieldsPerRecord = -1
	rows, err := reader.ReadAll()
	if err != nil {
		return nil, err
	}
	return rows, nil
}

func writeSpreadsheetFile(path string, delimiter rune, rows [][]string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	file, err := os.Create(path)
	if err != nil {
		return err
	}
	defer file.Close()
	writer := csv.NewWriter(file)
	writer.Comma = delimiter
	if err := writer.WriteAll(rows); err != nil {
		return err
	}
	writer.Flush()
	return writer.Error()
}

func spreadsheetSlice(grid [][]string, rangeRef string) ([][]string, string, error) {
	if len(grid) == 0 {
		if strings.TrimSpace(rangeRef) == "" {
			return [][]string{}, "A1", nil
		}
		rng, err := parseSpreadsheetRange(rangeRef)
		if err != nil {
			return nil, "", err
		}
		return [][]string{}, spreadsheetRangeString(rng), nil
	}
	if strings.TrimSpace(rangeRef) == "" {
		maxCols := spreadsheetMaxCols(grid)
		return copyGrid(grid), spreadsheetRangeString(spreadsheetRange{startRow: 0, startCol: 0, endRow: len(grid) - 1, endCol: maxCols - 1, hasEnd: true}), nil
	}
	rng, err := parseSpreadsheetRange(rangeRef)
	if err != nil {
		return nil, "", err
	}
	if !rng.hasEnd {
		rng.endRow = len(grid) - 1
		rng.endCol = spreadsheetMaxCols(grid) - 1
	}
	out := make([][]string, 0, max(0, rng.endRow-rng.startRow+1))
	for row := rng.startRow; row <= rng.endRow; row++ {
		if row < 0 || row >= len(grid) {
			out = append(out, make([]string, rng.endCol-rng.startCol+1))
			continue
		}
		line := make([]string, 0, rng.endCol-rng.startCol+1)
		for col := rng.startCol; col <= rng.endCol; col++ {
			if col >= 0 && col < len(grid[row]) {
				line = append(line, grid[row][col])
			} else {
				line = append(line, "")
			}
		}
		out = append(out, line)
	}
	return out, spreadsheetRangeString(rng), nil
}

func spreadsheetValues(value any) ([][]string, error) {
	rows, ok := value.([]any)
	if !ok {
		return nil, fmt.Errorf("values must be a 2D array")
	}
	out := make([][]string, 0, len(rows))
	for _, rawRow := range rows {
		cells, ok := rawRow.([]any)
		if !ok {
			return nil, fmt.Errorf("values must be a 2D array")
		}
		row := make([]string, len(cells))
		for idx, cell := range cells {
			if cell == nil {
				row[idx] = ""
				continue
			}
			row[idx] = fmt.Sprint(cell)
		}
		out = append(out, row)
	}
	return out, nil
}

func spreadsheetEnsureSize(grid [][]string, rows int, cols int) [][]string {
	for len(grid) < rows {
		grid = append(grid, make([]string, cols))
	}
	for idx := range grid {
		if len(grid[idx]) < cols {
			grid[idx] = append(grid[idx], make([]string, cols-len(grid[idx]))...)
		}
	}
	return grid
}

func parseSpreadsheetRange(value string) (spreadsheetRange, error) {
	value = strings.TrimSpace(strings.ToUpper(value))
	if value == "" {
		return spreadsheetRange{}, fmt.Errorf("range is required")
	}
	parts := strings.Split(value, ":")
	startCol, startRow, err := parseSpreadsheetCell(parts[0])
	if err != nil {
		return spreadsheetRange{}, err
	}
	rng := spreadsheetRange{startRow: startRow, startCol: startCol, endRow: startRow, endCol: startCol}
	if len(parts) == 2 {
		endCol, endRow, endErr := parseSpreadsheetCell(parts[1])
		if endErr != nil {
			return spreadsheetRange{}, endErr
		}
		rng.endRow = endRow
		rng.endCol = endCol
		rng.hasEnd = true
	}
	if len(parts) > 2 {
		return spreadsheetRange{}, fmt.Errorf("invalid range %q", value)
	}
	if rng.endRow < rng.startRow || rng.endCol < rng.startCol {
		return spreadsheetRange{}, fmt.Errorf("invalid range %q", value)
	}
	return rng, nil
}

func parseSpreadsheetCell(value string) (col int, row int, err error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0, 0, fmt.Errorf("cell reference is required")
	}
	letters := ""
	digits := ""
	for _, ch := range value {
		switch {
		case ch >= 'A' && ch <= 'Z':
			if digits != "" {
				return 0, 0, fmt.Errorf("invalid cell reference %q", value)
			}
			letters += string(ch)
		case ch >= '0' && ch <= '9':
			digits += string(ch)
		default:
			return 0, 0, fmt.Errorf("invalid cell reference %q", value)
		}
	}
	if letters == "" || digits == "" {
		return 0, 0, fmt.Errorf("invalid cell reference %q", value)
	}
	col = spreadsheetColumnIndex(letters)
	rowNum, convErr := intFrom(digits, 0)
	if convErr != nil || rowNum <= 0 {
		return 0, 0, fmt.Errorf("invalid cell reference %q", value)
	}
	return col, rowNum - 1, nil
}

func spreadsheetColumnIndex(letters string) int {
	total := 0
	for _, ch := range letters {
		total = total*26 + int(ch-'A'+1)
	}
	return total - 1
}

func spreadsheetColumnLabel(index int) string {
	index++
	label := ""
	for index > 0 {
		index--
		label = string(rune('A'+(index%26))) + label
		index /= 26
	}
	return label
}

func spreadsheetRangeString(rng spreadsheetRange) string {
	start := fmt.Sprintf("%s%d", spreadsheetColumnLabel(rng.startCol), rng.startRow+1)
	end := fmt.Sprintf("%s%d", spreadsheetColumnLabel(rng.endCol), rng.endRow+1)
	if start == end {
		return start
	}
	return start + ":" + end
}

func spreadsheetMaxCols(rows [][]string) int {
	maxCols := 0
	for _, row := range rows {
		if len(row) > maxCols {
			maxCols = len(row)
		}
	}
	return maxCols
}

func spreadsheetExportContent(format string, rows [][]string, header bool) (string, error) {
	switch format {
	case "csv":
		return spreadsheetRenderDelimited(rows, ',')
	case "tsv":
		return spreadsheetRenderDelimited(rows, '\t')
	case "json":
		if header && len(rows) > 0 {
			headers := rows[0]
			objects := make([]map[string]string, 0, max(len(rows)-1, 0))
			for _, row := range rows[1:] {
				entry := make(map[string]string, len(headers))
				for idx, key := range headers {
					if idx < len(row) {
						entry[key] = row[idx]
					} else {
						entry[key] = ""
					}
				}
				objects = append(objects, entry)
			}
			data, err := json.MarshalIndent(objects, "", "  ")
			return string(data), err
		}
		data, err := json.MarshalIndent(rows, "", "  ")
		return string(data), err
	case "markdown", "md":
		return spreadsheetRenderMarkdown(rows), nil
	default:
		return "", fmt.Errorf("unsupported export format %q", format)
	}
}

func spreadsheetRenderDelimited(rows [][]string, delimiter rune) (string, error) {
	var builder strings.Builder
	writer := csv.NewWriter(&builder)
	writer.Comma = delimiter
	if err := writer.WriteAll(rows); err != nil {
		return "", err
	}
	writer.Flush()
	return builder.String(), writer.Error()
}

func spreadsheetRenderMarkdown(rows [][]string) string {
	if len(rows) == 0 {
		return ""
	}
	width := spreadsheetMaxCols(rows)
	normalized := spreadsheetEnsureSize(copyGrid(rows), len(rows), width)
	var builder strings.Builder
	for idx, row := range normalized {
		builder.WriteString("| ")
		builder.WriteString(strings.Join(row, " | "))
		builder.WriteString(" |\n")
		if idx == 0 {
			builder.WriteString("| ")
			builder.WriteString(strings.TrimSpace(strings.Repeat("--- | ", width)))
			builder.WriteString("\n")
		}
	}
	return builder.String()
}

func copyGrid(rows [][]string) [][]string {
	out := make([][]string, len(rows))
	for idx, row := range rows {
		out[idx] = append([]string(nil), row...)
	}
	return out
}
