package spreadsheet

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/fulcrus/hopclaw/agent"
	"github.com/fulcrus/hopclaw/contextengine"
	"github.com/xuri/excelize/v2"
)

// ---------------------------------------------------------------------------
// XLSX helpers
// ---------------------------------------------------------------------------

// isXLSXFile returns true when the file extension is .xlsx (case-insensitive).
func isXLSXFile(path string) bool {
	return strings.EqualFold(filepath.Ext(path), ".xlsx")
}

// readXLSXFile reads a sheet from an XLSX workbook into a string grid.
// When sheet is empty the first (active) sheet is used.
func readXLSXFile(path string, sheet string) ([][]string, error) {
	f, err := excelize.OpenFile(path)
	if err != nil {
		return nil, fmt.Errorf("open xlsx: %w", err)
	}
	defer f.Close()

	if sheet == "" {
		sheet = f.GetSheetName(f.GetActiveSheetIndex())
	}
	rows, err := f.GetRows(sheet)
	if err != nil {
		return nil, fmt.Errorf("read xlsx sheet %q: %w", sheet, err)
	}
	return rows, nil
}

// ReadXLSXFile is the exported version of readXLSXFile for use by tests in the
// root toolruntime package.
func ReadXLSXFile(path string, sheet string) ([][]string, error) {
	return readXLSXFile(path, sheet)
}

func xlsxSheetExists(path string, sheet string) (bool, error) {
	f, err := excelize.OpenFile(path)
	if err != nil {
		return false, fmt.Errorf("open xlsx: %w", err)
	}
	defer f.Close()
	idx, err := f.GetSheetIndex(sheet)
	if err != nil {
		return false, err
	}
	return idx >= 0, nil
}

// writeXLSXFile writes a string grid to a sheet in an XLSX workbook.
// If the file does not exist a new workbook is created.
// When sheet is empty, "Sheet1" is used.
func writeXLSXFile(path string, sheet string, grid [][]string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	if sheet == "" {
		sheet = "Sheet1"
	}

	var f *excelize.File
	if _, statErr := os.Stat(path); statErr == nil {
		var openErr error
		f, openErr = excelize.OpenFile(path)
		if openErr != nil {
			return fmt.Errorf("open xlsx: %w", openErr)
		}
	} else {
		f = excelize.NewFile()
		// Rename the default sheet to the requested name.
		defaultSheet := f.GetSheetName(0)
		if defaultSheet != sheet {
			if _, err := f.NewSheet(sheet); err != nil {
				return fmt.Errorf("create xlsx sheet %q: %w", sheet, err)
			}
			f.DeleteSheet(defaultSheet) //nolint:errcheck
		}
	}
	defer f.Close()

	// Ensure the target sheet exists.
	if idx, _ := f.GetSheetIndex(sheet); idx < 0 {
		if _, err := f.NewSheet(sheet); err != nil {
			return fmt.Errorf("create xlsx sheet %q: %w", sheet, err)
		}
	}

	for r, row := range grid {
		for c, val := range row {
			cell, _ := excelize.CoordinatesToCellName(c+1, r+1)
			if err := f.SetCellValue(sheet, cell, val); err != nil {
				return fmt.Errorf("set cell %s: %w", cell, err)
			}
		}
	}
	return f.SaveAs(path)
}

// ---------------------------------------------------------------------------
// XLSX-specific schemas
// ---------------------------------------------------------------------------

func xlsxListSheetsSchema() map[string]any {
	return objectSchema(map[string]any{
		"path": stringSchema("Path to the XLSX workbook file."),
	}, "path")
}

func xlsxListSheetsOutputSchema() map[string]any {
	return objectSchema(map[string]any{
		"path":   stringSchema("Workbook path."),
		"sheets": stringArraySchema("Sheet names."),
		"count":  integerSchema("Number of sheets."),
	}, "path", "sheets", "count")
}

func xlsxCreateSchema() map[string]any {
	sheetDef := objectSchema(map[string]any{
		"name": stringSchema("Sheet name."),
		"data": arraySchema(map[string]any{"type": "array", "items": map[string]any{}}, "2D array of cell values for the sheet."),
	}, "name")
	return objectSchema(map[string]any{
		"path":   stringSchema("Path for the new XLSX workbook file."),
		"sheets": arraySchema(sheetDef, "Optional list of sheets with names and data."),
	}, "path")
}

func xlsxCreateOutputSchema() map[string]any {
	return objectSchema(map[string]any{
		"path":    stringSchema("Created workbook path."),
		"sheets":  stringArraySchema("Sheet names in the created workbook."),
		"created": booleanSchema("Whether the file was created."),
	}, "path", "sheets", "created")
}

func xlsxSetStyleSchema() map[string]any {
	styleObj := objectSchema(map[string]any{
		"font_bold":     booleanSchema("Whether to make text bold."),
		"font_color":    stringSchema("Font color as hex, e.g. #FF0000."),
		"bg_color":      stringSchema("Background fill color as hex, e.g. #FFFF00."),
		"number_format": stringSchema("Number format string, e.g. #,##0.00."),
		"border":        booleanSchema("Whether to add thin borders around cells."),
		"alignment":     stringSchema("Horizontal alignment: left, center, right."),
	})
	return objectSchema(map[string]any{
		"path":  stringSchema("Path to the XLSX workbook file."),
		"range": stringSchema("A1-style cell range to style, e.g. A1:C3."),
		"sheet": stringSchema("Sheet name. Defaults to the active sheet."),
		"style": styleObj,
	}, "path", "range", "style")
}

func xlsxSetStyleOutputSchema() map[string]any {
	return objectSchema(map[string]any{
		"path":  stringSchema("Workbook path."),
		"range": stringSchema("Styled range."),
		"sheet": stringSchema("Sheet name."),
	}, "path", "range", "sheet")
}

// ---------------------------------------------------------------------------
// XLSX-specific handlers
// ---------------------------------------------------------------------------

func handleXLSXListSheets(_ context.Context, rt Runtime, call agent.ToolCall) (contextengine.ToolResult, error) {
	path, err := requiredString(call.Input, "path")
	if err != nil {
		return contextengine.ToolResult{}, err
	}
	resolved, err := rt.ResolvePath(path)
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("spreadsheet.list_sheets: %w", err)
	}
	f, err := excelize.OpenFile(resolved)
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("spreadsheet.list_sheets: %w", err)
	}
	defer f.Close()

	sheets := f.GetSheetList()
	return rt.JSONResult(call, map[string]any{
		"path":   rt.DisplayPath(resolved),
		"sheets": sheets,
		"count":  len(sheets),
	})
}

func handleXLSXCreate(_ context.Context, rt Runtime, call agent.ToolCall) (contextengine.ToolResult, error) {
	path, err := requiredString(call.Input, "path")
	if err != nil {
		return contextengine.ToolResult{}, err
	}
	resolved, err := rt.ResolvePath(path)
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("spreadsheet.create: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(resolved), 0o755); err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("spreadsheet.create: %w", err)
	}

	f := excelize.NewFile()
	defer f.Close()

	sheetNames := []string{}
	rawSheets, _ := call.Input["sheets"].([]any)

	if len(rawSheets) == 0 {
		// Default: one sheet named "Sheet1".
		sheetNames = append(sheetNames, "Sheet1")
	} else {
		defaultSheet := f.GetSheetName(0)
		for i, raw := range rawSheets {
			entry, ok := raw.(map[string]any)
			if !ok {
				return contextengine.ToolResult{}, fmt.Errorf("spreadsheet.create: sheets[%d] must be an object", i)
			}
			name, _ := stringFrom(entry["name"])
			name = strings.TrimSpace(name)
			if name == "" {
				name = fmt.Sprintf("Sheet%d", i+1)
			}
			sheetNames = append(sheetNames, name)

			if i == 0 {
				// Rename the default sheet.
				if err := f.SetSheetName(defaultSheet, name); err != nil {
					return contextengine.ToolResult{}, fmt.Errorf("spreadsheet.create: rename default sheet: %w", err)
				}
			} else {
				if _, err := f.NewSheet(name); err != nil {
					return contextengine.ToolResult{}, fmt.Errorf("spreadsheet.create: new sheet %q: %w", name, err)
				}
			}

			// Write data if provided.
			if rawData, ok := entry["data"]; ok && rawData != nil {
				data, dataErr := spreadsheetValues(rawData)
				if dataErr != nil {
					return contextengine.ToolResult{}, fmt.Errorf("spreadsheet.create: sheets[%d].data: %w", i, dataErr)
				}
				for r, row := range data {
					for c, val := range row {
						cell, _ := excelize.CoordinatesToCellName(c+1, r+1)
						if err := f.SetCellValue(name, cell, val); err != nil {
							return contextengine.ToolResult{}, fmt.Errorf("spreadsheet.create: set cell %s: %w", cell, err)
						}
					}
				}
			}
		}
	}

	if err := f.SaveAs(resolved); err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("spreadsheet.create: %w", err)
	}
	return rt.JSONResult(call, map[string]any{
		"path":    rt.DisplayPath(resolved),
		"sheets":  sheetNames,
		"created": true,
	})
}

func handleXLSXSetStyle(_ context.Context, rt Runtime, call agent.ToolCall) (contextengine.ToolResult, error) {
	path, err := requiredString(call.Input, "path")
	if err != nil {
		return contextengine.ToolResult{}, err
	}
	rangeRef, err := requiredString(call.Input, "range")
	if err != nil {
		return contextengine.ToolResult{}, err
	}
	resolved, err := rt.ResolvePath(path)
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("spreadsheet.set_style: %w", err)
	}

	f, err := excelize.OpenFile(resolved)
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("spreadsheet.set_style: %w", err)
	}
	defer f.Close()

	sheet := optionalString(call.Input, "sheet")
	if sheet == "" {
		sheet = f.GetSheetName(f.GetActiveSheetIndex())
	}

	// Parse style object.
	styleInput, _ := call.Input["style"].(map[string]any)
	if styleInput == nil {
		return contextengine.ToolResult{}, fmt.Errorf("spreadsheet.set_style: style is required")
	}

	excelStyle := &excelize.Style{}

	// Font settings.
	fontBold, _ := boolFrom(styleInput["font_bold"])
	fontColor, _ := stringFrom(styleInput["font_color"])
	if fontBold || fontColor != "" {
		excelStyle.Font = &excelize.Font{
			Bold:  fontBold,
			Color: strings.TrimPrefix(fontColor, "#"),
		}
	}

	// Background fill.
	bgColor, _ := stringFrom(styleInput["bg_color"])
	if bgColor != "" {
		excelStyle.Fill = excelize.Fill{
			Type:    "pattern",
			Color:   []string{strings.TrimPrefix(bgColor, "#")},
			Pattern: 1,
		}
	}

	// Number format.
	numFmt, _ := stringFrom(styleInput["number_format"])
	if numFmt != "" {
		excelStyle.CustomNumFmt = &numFmt
	}

	// Border.
	addBorder, _ := boolFrom(styleInput["border"])
	if addBorder {
		thinBorder := excelize.Border{Type: "thin", Color: "000000", Style: 1}
		excelStyle.Border = []excelize.Border{
			{Type: "left", Color: thinBorder.Color, Style: thinBorder.Style},
			{Type: "right", Color: thinBorder.Color, Style: thinBorder.Style},
			{Type: "top", Color: thinBorder.Color, Style: thinBorder.Style},
			{Type: "bottom", Color: thinBorder.Color, Style: thinBorder.Style},
		}
	}

	// Alignment.
	alignment, _ := stringFrom(styleInput["alignment"])
	if alignment != "" {
		excelStyle.Alignment = &excelize.Alignment{
			Horizontal: strings.ToLower(strings.TrimSpace(alignment)),
		}
	}

	styleID, err := f.NewStyle(excelStyle)
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("spreadsheet.set_style: create style: %w", err)
	}

	// Parse the range and apply style to each cell.
	rng, err := parseSpreadsheetRange(rangeRef)
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("spreadsheet.set_style: %w", err)
	}
	startCell, _ := excelize.CoordinatesToCellName(rng.startCol+1, rng.startRow+1)
	if rng.hasEnd {
		endCell, _ := excelize.CoordinatesToCellName(rng.endCol+1, rng.endRow+1)
		if err := f.SetCellStyle(sheet, startCell, endCell, styleID); err != nil {
			return contextengine.ToolResult{}, fmt.Errorf("spreadsheet.set_style: apply style: %w", err)
		}
	} else {
		if err := f.SetCellStyle(sheet, startCell, startCell, styleID); err != nil {
			return contextengine.ToolResult{}, fmt.Errorf("spreadsheet.set_style: apply style: %w", err)
		}
	}

	if err := f.Save(); err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("spreadsheet.set_style: save: %w", err)
	}

	return rt.JSONResult(call, map[string]any{
		"path":  rt.DisplayPath(resolved),
		"range": rangeRef,
		"sheet": sheet,
	})
}
