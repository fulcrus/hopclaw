package toolruntime

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/fulcrus/hopclaw/agent"
	spreadsheetpkg "github.com/fulcrus/hopclaw/toolruntime/spreadsheet"
	"github.com/xuri/excelize/v2"
)

func xlsxExec(t *testing.T, builtins *Builtins, name string, input map[string]any) string {
	t.Helper()
	results, err := builtins.ExecuteBatch(context.Background(), &agent.Run{ID: "run-xlsx"}, &agent.Session{ID: "sess-xlsx"}, []agent.ToolCall{{
		ID: "call-" + name, Name: name, Input: input,
	}})
	if err != nil {
		t.Fatalf("%s error: %v", name, err)
	}
	return results[0].Content
}

func TestXLSXListSheets(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	builtins := NewBuiltins(BuiltinsConfig{Root: root, MaxReadBytes: 64 * 1024})

	// Create a workbook with multiple sheets.
	f := excelize.NewFile()
	f.SetSheetName("Sheet1", "Sales")
	f.NewSheet("Inventory") //nolint:errcheck
	f.NewSheet("Summary")   //nolint:errcheck
	f.SaveAs(filepath.Join(root, "multi.xlsx"))
	f.Close()

	var out struct {
		Sheets []string `json:"sheets"`
		Count  int      `json:"count"`
	}
	raw := xlsxExec(t, builtins, "spreadsheet.list_sheets", map[string]any{"path": "multi.xlsx"})
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if out.Count != 3 {
		t.Fatalf("count = %d, want 3", out.Count)
	}
	if out.Sheets[0] != "Sales" || out.Sheets[1] != "Inventory" || out.Sheets[2] != "Summary" {
		t.Fatalf("sheets = %v", out.Sheets)
	}
}

func TestXLSXCreate(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	builtins := NewBuiltins(BuiltinsConfig{Root: root, MaxReadBytes: 64 * 1024})

	var createOut struct {
		Path    string   `json:"path"`
		Sheets  []string `json:"sheets"`
		Created bool     `json:"created"`
	}
	raw := xlsxExec(t, builtins, "spreadsheet.create", map[string]any{
		"path": "report.xlsx",
		"sheets": []any{
			map[string]any{
				"name": "Data",
				"data": []any{
					[]any{"Name", "Value"},
					[]any{"Alpha", "100"},
					[]any{"Beta", "200"},
				},
			},
			map[string]any{
				"name": "Notes",
			},
		},
	})
	if err := json.Unmarshal([]byte(raw), &createOut); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !createOut.Created {
		t.Fatal("expected created=true")
	}
	if len(createOut.Sheets) != 2 || createOut.Sheets[0] != "Data" || createOut.Sheets[1] != "Notes" {
		t.Fatalf("sheets = %v", createOut.Sheets)
	}

	// Read back the data using readXLSXFile.
	grid, err := spreadsheetpkg.ReadXLSXFile(filepath.Join(root, "report.xlsx"), "Data")
	if err != nil {
		t.Fatalf("readXLSXFile: %v", err)
	}
	if len(grid) != 3 || grid[0][0] != "Name" || grid[2][1] != "200" {
		t.Fatalf("grid = %v", grid)
	}
}

func TestXLSXSetStyle(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	builtins := NewBuiltins(BuiltinsConfig{Root: root, MaxReadBytes: 64 * 1024})

	// Create a workbook first.
	xlsxExec(t, builtins, "spreadsheet.create", map[string]any{
		"path": "styled.xlsx",
		"sheets": []any{
			map[string]any{
				"name": "Sheet1",
				"data": []any{
					[]any{"Header1", "Header2"},
					[]any{"val1", "val2"},
				},
			},
		},
	})

	var styleOut struct {
		Path  string `json:"path"`
		Range string `json:"range"`
		Sheet string `json:"sheet"`
	}
	raw := xlsxExec(t, builtins, "spreadsheet.set_style", map[string]any{
		"path":  "styled.xlsx",
		"range": "A1:B1",
		"style": map[string]any{
			"font_bold":  true,
			"font_color": "#FF0000",
			"bg_color":   "#FFFF00",
			"border":     true,
			"alignment":  "center",
		},
	})
	if err := json.Unmarshal([]byte(raw), &styleOut); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if styleOut.Range != "A1:B1" {
		t.Fatalf("range = %q", styleOut.Range)
	}
	if styleOut.Sheet != "Sheet1" {
		t.Fatalf("sheet = %q", styleOut.Sheet)
	}

	// Verify the style was applied by opening the file.
	f, err := excelize.OpenFile(filepath.Join(root, "styled.xlsx"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer f.Close()
	styleID, err := f.GetCellStyle("Sheet1", "A1")
	if err != nil {
		t.Fatalf("GetCellStyle: %v", err)
	}
	if styleID == 0 {
		t.Fatal("expected non-zero style ID on A1")
	}
}

func TestSpreadsheetReadXLSX(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	builtins := NewBuiltins(BuiltinsConfig{Root: root, MaxReadBytes: 64 * 1024})

	// Create an XLSX file with excelize directly.
	f := excelize.NewFile()
	f.SetCellValue("Sheet1", "A1", "name")
	f.SetCellValue("Sheet1", "B1", "qty")
	f.SetCellValue("Sheet1", "A2", "apple")
	f.SetCellValue("Sheet1", "B2", "5")
	f.SaveAs(filepath.Join(root, "data.xlsx"))
	f.Close()

	var readOut struct {
		Range    string     `json:"range"`
		Rows     [][]string `json:"rows"`
		RowCount int        `json:"row_count"`
	}
	raw := xlsxExec(t, builtins, "spreadsheet.read_range", map[string]any{
		"path": "data.xlsx",
	})
	if err := json.Unmarshal([]byte(raw), &readOut); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if readOut.RowCount != 2 {
		t.Fatalf("row_count = %d, want 2", readOut.RowCount)
	}
	if readOut.Rows[0][0] != "name" || readOut.Rows[1][1] != "5" {
		t.Fatalf("rows = %v", readOut.Rows)
	}
}

func TestSpreadsheetWriteXLSX(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	builtins := NewBuiltins(BuiltinsConfig{Root: root, MaxReadBytes: 64 * 1024})

	// Write to a new XLSX file.
	var writeOut struct {
		Range   string `json:"range"`
		Created bool   `json:"created"`
	}
	raw := xlsxExec(t, builtins, "spreadsheet.write_range", map[string]any{
		"path":  "new.xlsx",
		"range": "A1",
		"values": []any{
			[]any{"x", "y"},
			[]any{"1", "2"},
		},
	})
	if err := json.Unmarshal([]byte(raw), &writeOut); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !writeOut.Created {
		t.Fatal("expected created=true")
	}
	if writeOut.Range != "A1:B2" {
		t.Fatalf("range = %q", writeOut.Range)
	}

	// Read back to verify.
	grid, err := spreadsheetpkg.ReadXLSXFile(filepath.Join(root, "new.xlsx"), "")
	if err != nil {
		t.Fatalf("readXLSXFile: %v", err)
	}
	if len(grid) != 2 || grid[0][0] != "x" || grid[1][1] != "2" {
		t.Fatalf("grid = %v", grid)
	}

	// Overwrite a cell in the existing file.
	raw = xlsxExec(t, builtins, "spreadsheet.write_range", map[string]any{
		"path":  "new.xlsx",
		"range": "B2",
		"values": []any{
			[]any{"99"},
		},
	})
	if err := json.Unmarshal([]byte(raw), &writeOut); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if writeOut.Created {
		t.Fatal("expected created=false for existing file")
	}

	grid, err = spreadsheetpkg.ReadXLSXFile(filepath.Join(root, "new.xlsx"), "")
	if err != nil {
		t.Fatalf("readXLSXFile: %v", err)
	}
	if grid[1][1] != "99" {
		t.Fatalf("expected cell B2=99, got %q", grid[1][1])
	}
}

func TestSpreadsheetWriteXLSXCreatesNewSheetInExistingWorkbook(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	builtins := NewBuiltins(BuiltinsConfig{Root: root, MaxReadBytes: 64 * 1024})

	xlsxExec(t, builtins, "spreadsheet.create", map[string]any{
		"path": "book.xlsx",
		"sheets": []any{
			map[string]any{
				"name": "Sheet1",
				"data": []any{
					[]any{"existing"},
				},
			},
		},
	})

	xlsxExec(t, builtins, "spreadsheet.write_range", map[string]any{
		"path":  "book.xlsx",
		"sheet": "Report",
		"range": "A1",
		"values": []any{
			[]any{"name", "value"},
			[]any{"alpha", "42"},
		},
	})

	grid, err := spreadsheetpkg.ReadXLSXFile(filepath.Join(root, "book.xlsx"), "Report")
	if err != nil {
		t.Fatalf("readXLSXFile Report: %v", err)
	}
	if len(grid) != 2 || grid[0][0] != "name" || grid[1][1] != "42" {
		t.Fatalf("Report grid = %v", grid)
	}
}
