package toolruntime

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/fulcrus/hopclaw/agent"
)

func TestSpreadsheetToolsLifecycle(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	source := filepath.Join(root, "sheet.csv")
	if err := os.WriteFile(source, []byte("name,qty,price\napple,2,3\norange,5,4\n"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	builtins := NewBuiltins(BuiltinsConfig{Root: root, MaxReadBytes: 64 * 1024})
	ctx := context.Background()
	run := &agent.Run{ID: "run-sheet"}
	sess := &agent.Session{ID: "sess-sheet"}

	exec := func(name string, input map[string]any) string {
		t.Helper()
		results, err := builtins.ExecuteBatch(ctx, run, sess, []agent.ToolCall{{
			ID: "call-" + name, Name: name, Input: input,
		}})
		if err != nil {
			t.Fatalf("%s error: %v", name, err)
		}
		return results[0].Content
	}

	var readOut struct {
		Range   string              `json:"range"`
		Headers []string            `json:"headers"`
		Objects []map[string]string `json:"objects"`
	}
	if err := json.Unmarshal([]byte(exec("spreadsheet.read_range", map[string]any{
		"path":   "sheet.csv",
		"range":  "A1:C3",
		"header": true,
	})), &readOut); err != nil {
		t.Fatalf("spreadsheet.read_range unmarshal: %v", err)
	}
	if readOut.Range != "A1:C3" {
		t.Fatalf("range = %q", readOut.Range)
	}
	if len(readOut.Headers) != 3 || readOut.Headers[0] != "name" {
		t.Fatalf("headers = %v", readOut.Headers)
	}
	if len(readOut.Objects) != 2 || readOut.Objects[1]["qty"] != "5" {
		t.Fatalf("objects = %+v", readOut.Objects)
	}

	var writeOut struct {
		Range   string `json:"range"`
		Created bool   `json:"created"`
	}
	if err := json.Unmarshal([]byte(exec("spreadsheet.write_range", map[string]any{
		"path":  "sheet.csv",
		"range": "B2:C3",
		"values": []any{
			[]any{"7", "9"},
			[]any{"8", "10"},
		},
	})), &writeOut); err != nil {
		t.Fatalf("spreadsheet.write_range unmarshal: %v", err)
	}
	if writeOut.Range != "B2:C3" || writeOut.Created {
		t.Fatalf("writeOut = %+v", writeOut)
	}

	updated, err := os.ReadFile(source)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if !strings.Contains(string(updated), "apple,7,9") || !strings.Contains(string(updated), "orange,8,10") {
		t.Fatalf("updated csv = %q", string(updated))
	}

	exportPath := filepath.Join(root, "sheet.md")
	var exportOut struct {
		Format string `json:"format"`
		Output string `json:"output"`
	}
	if err := json.Unmarshal([]byte(exec("spreadsheet.export", map[string]any{
		"path":   "sheet.csv",
		"output": "sheet.md",
		"format": "markdown",
	})), &exportOut); err != nil {
		t.Fatalf("spreadsheet.export unmarshal: %v", err)
	}
	if exportOut.Format != "markdown" || exportOut.Output != "sheet.md" {
		t.Fatalf("exportOut = %+v", exportOut)
	}
	exported, err := os.ReadFile(exportPath)
	if err != nil {
		t.Fatalf("ReadFile(export) error = %v", err)
	}
	if !strings.Contains(string(exported), "| name | qty | price |") {
		t.Fatalf("markdown export = %q", string(exported))
	}
}
