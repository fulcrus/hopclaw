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

// TestOfficeFullLifecycle exercises Word, Excel, and PowerPoint operations
// in a single test to verify the end-to-end office document capability.
func TestOfficeFullLifecycle(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	builtins := NewBuiltins(BuiltinsConfig{Root: root, MaxReadBytes: 256 * 1024})
	ctx := context.Background()
	run := &agent.Run{ID: "run-office"}
	sess := &agent.Session{ID: "sess-office"}

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

	// =======================================================================
	// Part 1: Word Document — create → read → info → search
	// =======================================================================
	t.Run("word_lifecycle", func(t *testing.T) {
		// Create a Word document with multiple styles.
		raw := exec("document.create", map[string]any{
			"path":   "report.docx",
			"title":  "年度报告 2026",
			"author": "HopClaw Agent",
			"content": []any{
				map[string]any{"text": "年度报告", "style": "heading1"},
				map[string]any{"text": "第一章 项目概述", "style": "heading2"},
				map[string]any{"text": "HopClaw 是一个 AI Agent 框架，支持多模型路由和工具调用。"},
				map[string]any{"text": "第二章 技术架构", "style": "heading2"},
				map[string]any{"text": "系统采用 Go 语言编写，支持插件化的技能发现和加载。"},
				map[string]any{"text": "结论", "style": "heading3"},
				map[string]any{"text": "项目进展顺利，所有里程碑已完成。"},
			},
		})

		var createOut struct {
			Path           string `json:"path"`
			ParagraphCount int    `json:"paragraph_count"`
			Bytes          int    `json:"bytes"`
		}
		if err := json.Unmarshal([]byte(raw), &createOut); err != nil {
			t.Fatalf("unmarshal create: %v", err)
		}
		if createOut.ParagraphCount != 7 {
			t.Errorf("paragraph_count = %d, want 7", createOut.ParagraphCount)
		}
		if createOut.Bytes == 0 {
			t.Error("bytes should be > 0")
		}

		// Read back the document.
		raw = exec("document.read", map[string]any{"path": "report.docx"})
		var readOut struct {
			Paragraphs     []string `json:"paragraphs"`
			ParagraphCount int      `json:"paragraph_count"`
			WordCount      int      `json:"word_count"`
			Text           string   `json:"text"`
		}
		if err := json.Unmarshal([]byte(raw), &readOut); err != nil {
			t.Fatalf("unmarshal read: %v", err)
		}
		if readOut.ParagraphCount != 7 {
			t.Errorf("read paragraph_count = %d, want 7", readOut.ParagraphCount)
		}
		if !strings.Contains(readOut.Text, "年度报告") {
			t.Error("text should contain 年度报告")
		}
		if !strings.Contains(readOut.Text, "HopClaw") {
			t.Error("text should contain HopClaw")
		}

		// Get document info.
		raw = exec("document.info", map[string]any{"path": "report.docx"})
		var infoOut struct {
			Title  string `json:"title"`
			Author string `json:"author"`
		}
		if err := json.Unmarshal([]byte(raw), &infoOut); err != nil {
			t.Fatalf("unmarshal info: %v", err)
		}
		if infoOut.Title != "年度报告 2026" {
			t.Errorf("title = %q", infoOut.Title)
		}
		if infoOut.Author != "HopClaw Agent" {
			t.Errorf("author = %q", infoOut.Author)
		}

		// Search for content.
		raw = exec("document.search", map[string]any{
			"path":  "report.docx",
			"query": "技术架构",
		})
		var searchOut struct {
			TotalMatches int `json:"total_matches"`
		}
		if err := json.Unmarshal([]byte(raw), &searchOut); err != nil {
			t.Fatalf("unmarshal search: %v", err)
		}
		if searchOut.TotalMatches != 1 {
			t.Errorf("search matches = %d, want 1", searchOut.TotalMatches)
		}

		t.Log("Word lifecycle: create → read → info → search PASS")
	})

	// =======================================================================
	// Part 2: Excel XLSX — create → list_sheets → write_range → read_range → export
	// =======================================================================
	t.Run("excel_lifecycle", func(t *testing.T) {
		// Create an XLSX workbook.
		raw := exec("spreadsheet.create", map[string]any{
			"path": "sales.xlsx",
			"sheets": []any{
				map[string]any{
					"name": "Q1",
					"data": []any{
						[]any{"Product", "Units", "Revenue"},
						[]any{"Widget A", "100", "5000"},
						[]any{"Widget B", "200", "8000"},
					},
				},
				map[string]any{
					"name": "Q2",
					"data": []any{
						[]any{"Product", "Units", "Revenue"},
						[]any{"Widget A", "150", "7500"},
					},
				},
			},
		})
		var createOut struct {
			Path   string   `json:"path"`
			Sheets []string `json:"sheets"`
		}
		if err := json.Unmarshal([]byte(raw), &createOut); err != nil {
			t.Fatalf("unmarshal create: %v", err)
		}
		if len(createOut.Sheets) != 2 {
			t.Errorf("sheets = %v, want 2 sheets", createOut.Sheets)
		}

		// List sheets.
		raw = exec("spreadsheet.list_sheets", map[string]any{"path": "sales.xlsx"})
		var listOut struct {
			Sheets []string `json:"sheets"`
		}
		if err := json.Unmarshal([]byte(raw), &listOut); err != nil {
			t.Fatalf("unmarshal list_sheets: %v", err)
		}
		if len(listOut.Sheets) != 2 || listOut.Sheets[0] != "Q1" {
			t.Errorf("sheets = %v", listOut.Sheets)
		}

		// Write additional data to Q1.
		raw = exec("spreadsheet.write_range", map[string]any{
			"path":  "sales.xlsx",
			"sheet": "Q1",
			"range": "A4:C4",
			"values": []any{
				[]any{"Widget C", "300", "12000"},
			},
		})
		var writeOut struct {
			Range string `json:"range"`
		}
		if err := json.Unmarshal([]byte(raw), &writeOut); err != nil {
			t.Fatalf("unmarshal write_range: %v", err)
		}
		if writeOut.Range != "A4:C4" {
			t.Errorf("write range = %q", writeOut.Range)
		}

		// Read back the range including new row.
		raw = exec("spreadsheet.read_range", map[string]any{
			"path":   "sales.xlsx",
			"sheet":  "Q1",
			"range":  "A1:C4",
			"header": true,
		})
		var readOut struct {
			Objects []map[string]string `json:"objects"`
			Headers []string            `json:"headers"`
		}
		if err := json.Unmarshal([]byte(raw), &readOut); err != nil {
			t.Fatalf("unmarshal read_range: %v", err)
		}
		if len(readOut.Objects) != 3 {
			t.Errorf("read objects = %d, want 3 (including new row)", len(readOut.Objects))
		}
		if readOut.Objects[2]["Product"] != "Widget C" {
			t.Errorf("row 3 Product = %q, want Widget C", readOut.Objects[2]["Product"])
		}

		// Export Q1 to markdown.
		_ = exec("spreadsheet.export", map[string]any{
			"path":   "sales.xlsx",
			"sheet":  "Q1",
			"output": "sales_q1.md",
			"format": "markdown",
		})
		exported, err := os.ReadFile(filepath.Join(root, "sales_q1.md"))
		if err != nil {
			t.Fatalf("read exported: %v", err)
		}
		if !strings.Contains(string(exported), "Widget C") {
			t.Error("markdown export should contain Widget C")
		}
		if !strings.Contains(string(exported), "| Product |") {
			t.Error("markdown export should have header row")
		}

		// Export to JSON.
		_ = exec("spreadsheet.export", map[string]any{
			"path":   "sales.xlsx",
			"sheet":  "Q1",
			"output": "sales_q1.json",
			"format": "json",
			"header": true,
		})
		jsonExport, err := os.ReadFile(filepath.Join(root, "sales_q1.json"))
		if err != nil {
			t.Fatalf("read json export: %v", err)
		}
		if !strings.Contains(string(jsonExport), "Widget C") {
			t.Error("JSON export should contain Widget C")
		}

		t.Log("Excel lifecycle: create → list_sheets → write_range → read_range → export PASS")
	})

	// =======================================================================
	// Part 3: PowerPoint — create → read → info
	// =======================================================================
	t.Run("pptx_lifecycle", func(t *testing.T) {
		// Create presentation with Chinese content.
		raw := exec("presentation.create", map[string]any{
			"path":   "demo.pptx",
			"title":  "产品演示",
			"author": "HopClaw",
			"slides": []any{
				map[string]any{
					"title":   "封面",
					"content": "HopClaw AI Agent 框架",
					"notes":   "欢迎各位参加本次产品演示",
				},
				map[string]any{
					"title":   "核心功能",
					"content": []any{"多模型路由", "技能发现", "工具调用", "桌面自动化"},
				},
				map[string]any{
					"title":   "架构图",
					"content": "Agent → Model Router → Tool Runtime → Skills",
				},
				map[string]any{
					"title":   "谢谢",
					"content": "Q&A",
					"notes":   "演示结束，欢迎提问",
				},
			},
		})

		var createOut struct {
			SlideCount int `json:"slide_count"`
			Bytes      int `json:"bytes"`
		}
		if err := json.Unmarshal([]byte(raw), &createOut); err != nil {
			t.Fatalf("unmarshal create: %v", err)
		}
		if createOut.SlideCount != 4 {
			t.Errorf("slide_count = %d, want 4", createOut.SlideCount)
		}

		// Read back.
		raw = exec("presentation.read", map[string]any{"path": "demo.pptx"})
		var readOut struct {
			SlideCount int `json:"slide_count"`
			Slides     []struct {
				Title   string `json:"title"`
				Content string `json:"content"`
				Notes   string `json:"notes"`
			} `json:"slides"`
		}
		if err := json.Unmarshal([]byte(raw), &readOut); err != nil {
			t.Fatalf("unmarshal read: %v", err)
		}
		if readOut.SlideCount != 4 {
			t.Errorf("read slide_count = %d", readOut.SlideCount)
		}
		if readOut.Slides[0].Title != "封面" {
			t.Errorf("slide 0 title = %q", readOut.Slides[0].Title)
		}
		if !strings.Contains(readOut.Slides[1].Content, "多模型路由") {
			t.Errorf("slide 1 content = %q, should contain 多模型路由", readOut.Slides[1].Content)
		}

		// Get info.
		raw = exec("presentation.info", map[string]any{"path": "demo.pptx"})
		var infoOut struct {
			Title      string `json:"title"`
			Author     string `json:"author"`
			SlideCount int    `json:"slide_count"`
		}
		if err := json.Unmarshal([]byte(raw), &infoOut); err != nil {
			t.Fatalf("unmarshal info: %v", err)
		}
		if infoOut.Title != "产品演示" {
			t.Errorf("info title = %q", infoOut.Title)
		}
		if infoOut.Author != "HopClaw" {
			t.Errorf("info author = %q", infoOut.Author)
		}

		t.Log("PPTX lifecycle: create → read → info PASS")
	})

	// =======================================================================
	// Part 4: CSV spreadsheet — create from scratch via write_range → read → export
	// =======================================================================
	t.Run("csv_lifecycle", func(t *testing.T) {
		// Write creates the file.
		raw := exec("spreadsheet.write_range", map[string]any{
			"path":  "data.csv",
			"range": "A1:D4",
			"values": []any{
				[]any{"Name", "Age", "City", "Score"},
				[]any{"Alice", "30", "Beijing", "95"},
				[]any{"Bob", "25", "Shanghai", "88"},
				[]any{"Charlie", "35", "Shenzhen", "92"},
			},
		})
		var writeOut struct {
			Created bool `json:"created"`
		}
		if err := json.Unmarshal([]byte(raw), &writeOut); err != nil {
			t.Fatalf("unmarshal write: %v", err)
		}
		if !writeOut.Created {
			t.Error("expected created=true for new file")
		}

		// Read it back with headers.
		raw = exec("spreadsheet.read_range", map[string]any{
			"path":   "data.csv",
			"range":  "A1:D4",
			"header": true,
		})
		var readOut struct {
			Headers []string            `json:"headers"`
			Objects []map[string]string `json:"objects"`
		}
		if err := json.Unmarshal([]byte(raw), &readOut); err != nil {
			t.Fatalf("unmarshal read: %v", err)
		}
		if len(readOut.Headers) != 4 {
			t.Errorf("headers count = %d", len(readOut.Headers))
		}
		if len(readOut.Objects) != 3 {
			t.Errorf("objects count = %d, want 3", len(readOut.Objects))
		}
		if readOut.Objects[0]["Name"] != "Alice" {
			t.Errorf("first row Name = %q", readOut.Objects[0]["Name"])
		}

		// Export to TSV.
		exec("spreadsheet.export", map[string]any{
			"path":   "data.csv",
			"output": "data.tsv",
			"format": "tsv",
		})
		tsv, err := os.ReadFile(filepath.Join(root, "data.tsv"))
		if err != nil {
			t.Fatalf("read tsv: %v", err)
		}
		if !strings.Contains(string(tsv), "Alice\t30\tBeijing\t95") {
			t.Errorf("TSV content unexpected: %q", string(tsv))
		}

		t.Log("CSV lifecycle: write_range (create) → read_range → export PASS")
	})
}
