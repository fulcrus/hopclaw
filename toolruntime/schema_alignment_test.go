package toolruntime

import (
	"context"
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/fulcrus/hopclaw/agent"
	"github.com/fulcrus/hopclaw/channels"
	channelmgr "github.com/fulcrus/hopclaw/channels/manager"
	"github.com/fulcrus/hopclaw/cron"
	"github.com/fulcrus/hopclaw/toolruntime/document"
	spreadsheetpkg "github.com/fulcrus/hopclaw/toolruntime/spreadsheet"
	"github.com/fulcrus/hopclaw/wakeup"
	"github.com/fulcrus/hopclaw/watch"
	"github.com/xuri/excelize/v2"
)

func TestWatchOutputPayloadMatchesSchemas(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	builtins := NewBuiltins(BuiltinsConfig{Root: root, MaxReadBytes: 64 * 1024})
	storePath := filepath.Join(t.TempDir(), "watch.json")
	if err := os.WriteFile(storePath, []byte(`{"version":1,"watches":[]}`), 0o644); err != nil {
		t.Fatalf("write watch store: %v", err)
	}
	store, err := watch.Load(storePath)
	if err != nil {
		t.Fatalf("watch.Load() error = %v", err)
	}
	builtins.ApplyBindings(BuiltinsBindings{WatchService: watch.NewService(store, nil)})

	run := &agent.Run{ID: "run-watch-schema"}
	sess := &agent.Session{ID: "sess-watch-schema"}

	addPayload := execBuiltinPayload(t, builtins, run, sess, "watch.add", map[string]any{
		"name":        "schema-watch",
		"interval":    "5m",
		"source_kind": "http",
		"source_url":  "https://example.com/news",
		"prompt":      "Summarize changes",
	})
	assertPayloadMatchesSchema(t, "watch.add", addPayload, watchAddOutputSchema())

	watchID, ok := addPayload["id"].(string)
	if !ok || watchID == "" {
		t.Fatalf("watch.add id = %#v", addPayload["id"])
	}

	listPayload := execBuiltinPayload(t, builtins, run, sess, "watch.list", map[string]any{})
	assertPayloadMatchesSchema(t, "watch.list", listPayload, watchListOutputSchema())

	statusPayload := execBuiltinPayload(t, builtins, run, sess, "watch.status", map[string]any{"id": watchID})
	assertPayloadMatchesSchema(t, "watch.status", statusPayload, watchStatusOutputSchema())

	updatePayload := execBuiltinPayload(t, builtins, run, sess, "watch.update", map[string]any{
		"id":       watchID,
		"interval": "10m",
		"enabled":  false,
	})
	assertPayloadMatchesSchema(t, "watch.update", updatePayload, watchUpdateOutputSchema())

	runPayload := execBuiltinPayload(t, builtins, run, sess, "watch.run", map[string]any{"id": watchID})
	assertPayloadMatchesSchema(t, "watch.run", runPayload, watchRunOutputSchema())

	removePayload := execBuiltinPayload(t, builtins, run, sess, "watch.remove", map[string]any{"id": watchID})
	assertPayloadMatchesSchema(t, "watch.remove", removePayload, watchRemoveOutputSchema())
}

func TestWakeupOutputPayloadMatchesSchemas(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	builtins := NewBuiltins(BuiltinsConfig{Root: root, MaxReadBytes: 64 * 1024})
	storePath := filepath.Join(t.TempDir(), "wakeup.json")
	if err := os.WriteFile(storePath, []byte(`{"version":1,"triggers":[]}`), 0o644); err != nil {
		t.Fatalf("write wakeup store: %v", err)
	}
	store, err := wakeup.Load(storePath)
	if err != nil {
		t.Fatalf("wakeup.Load() error = %v", err)
	}
	builtins.ApplyBindings(BuiltinsBindings{WakeupService: wakeup.NewService(store, nil)})

	run := &agent.Run{ID: "run-wakeup-schema"}
	sess := &agent.Session{ID: "sess-wakeup-schema"}

	addPayload := execBuiltinPayload(t, builtins, run, sess, "wakeup.add", map[string]any{
		"name":        "schema-wakeup",
		"schedule":    "0 8 * * *",
		"message":     "Send summary",
		"channel":     "feishu",
		"session_key": "feishu:chat-1",
	})
	assertPayloadMatchesSchema(t, "wakeup.add", addPayload, wakeupAddOutputSchema())

	triggerID, ok := addPayload["id"].(string)
	if !ok || triggerID == "" {
		t.Fatalf("wakeup.add id = %#v", addPayload["id"])
	}

	listPayload := execBuiltinPayload(t, builtins, run, sess, "wakeup.list", map[string]any{})
	assertPayloadMatchesSchema(t, "wakeup.list", listPayload, wakeupListOutputSchema())

	statusPayload := execBuiltinPayload(t, builtins, run, sess, "wakeup.status", map[string]any{"id": triggerID})
	assertPayloadMatchesSchema(t, "wakeup.status", statusPayload, wakeupStatusOutputSchema())

	updatePayload := execBuiltinPayload(t, builtins, run, sess, "wakeup.update", map[string]any{
		"id":      triggerID,
		"enabled": false,
	})
	assertPayloadMatchesSchema(t, "wakeup.update", updatePayload, wakeupUpdateOutputSchema())

	removePayload := execBuiltinPayload(t, builtins, run, sess, "wakeup.remove", map[string]any{"id": triggerID})
	assertPayloadMatchesSchema(t, "wakeup.remove", removePayload, wakeupRemoveOutputSchema())
}

func TestAutomationOutputPayloadMatchesSchemas(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	builtins := NewBuiltins(BuiltinsConfig{Root: root, MaxReadBytes: 64 * 1024})

	cronStorePath := filepath.Join(t.TempDir(), "cron.json")
	if err := os.WriteFile(cronStorePath, []byte(`{"version":1,"jobs":[]}`), 0o644); err != nil {
		t.Fatalf("write cron store: %v", err)
	}
	cronStore, err := cron.Load(cronStorePath)
	if err != nil {
		t.Fatalf("cron.Load() error = %v", err)
	}
	if err := cronStore.Add(cron.Job{
		ID:      "cron-schema",
		Name:    "Schema cron",
		Enabled: true,
		Schedule: cron.Schedule{
			Kind:       cron.ScheduleKindCron,
			Expression: "0 8 * * *",
		},
		Payload: cron.Payload{Content: "schema"},
	}); err != nil {
		t.Fatalf("cronStore.Add() error = %v", err)
	}
	builtins.ApplyBindings(BuiltinsBindings{CronService: cron.NewService(cronStore, nil, nil)})

	run := &agent.Run{ID: "run-automation-schema"}
	sess := &agent.Session{ID: "sess-automation-schema"}

	searchPayload := execBuiltinPayload(t, builtins, run, sess, "automation.search", map[string]any{
		"query": "schema",
	})
	assertPayloadMatchesSchema(t, "automation.search", searchPayload, automationSearchOutputSchema())

	statsPayload := execBuiltinPayload(t, builtins, run, sess, "automation.stats", map[string]any{})
	assertPayloadMatchesSchema(t, "automation.stats", statsPayload, automationStatsOutputSchema())
}

func TestDocumentOutputPayloadMatchesSchemas(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	builtins := NewBuiltins(BuiltinsConfig{Root: root, MaxReadBytes: 64 * 1024})
	run := &agent.Run{ID: "run-doc-schema"}
	sess := &agent.Session{ID: "sess-doc-schema"}

	createPayload := execBuiltinPayload(t, builtins, run, sess, "document.create", map[string]any{
		"path":   "schema.docx",
		"title":  "Schema Title",
		"author": "Schema Author",
		"content": []any{
			map[string]any{"text": "Main Title", "style": "heading1"},
			map[string]any{"text": "First paragraph."},
			map[string]any{"text": "Second paragraph."},
		},
	})
	assertPayloadMatchesSchema(t, "document.create", createPayload, documentOutputSchema(t, "document.create"))

	readPayload := execBuiltinPayload(t, builtins, run, sess, "document.read", map[string]any{
		"path": "schema.docx",
	})
	assertPayloadMatchesSchema(t, "document.read", readPayload, documentOutputSchema(t, "document.read"))

	infoPayload := execBuiltinPayload(t, builtins, run, sess, "document.info", map[string]any{
		"path": "schema.docx",
	})
	assertPayloadMatchesSchema(t, "document.info", infoPayload, documentOutputSchema(t, "document.info"))

	searchPayload := execBuiltinPayload(t, builtins, run, sess, "document.search", map[string]any{
		"path":  "schema.docx",
		"query": "paragraph",
	})
	assertPayloadMatchesSchema(t, "document.search", searchPayload, documentOutputSchema(t, "document.search"))
}

func TestSemanticOutputPayloadMatchesSchemas(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "report.txt"), []byte("artifact"), 0o644); err != nil {
		t.Fatalf("write artifact: %v", err)
	}
	builtins := NewBuiltins(BuiltinsConfig{Root: root, MaxReadBytes: 64 * 1024})
	manager := channelmgr.New()
	adapter := &semanticTestAdapter{
		history: []channels.HistoryMessage{
			{ID: "m1", ChannelID: "thread-1", SenderID: "u1", Content: "hello", Timestamp: "2026-03-18T10:00:00Z"},
		},
		actionData: map[string]*channels.ChannelActionResult{
			semanticInspectParticipants: {Success: true, Data: map[string]any{"participants": []any{"u1"}}},
		},
	}
	if err := manager.Register("ops", adapter); err != nil {
		t.Fatalf("register adapter: %v", err)
	}
	builtins.ApplyBindings(BuiltinsBindings{ChannelManager: manager})

	run := &agent.Run{ID: "run-semantic-schema"}
	sess := &agent.Session{ID: "sess-semantic-schema"}

	catalogPayload := execBuiltinPayload(t, builtins, run, sess, "semantic.catalog", map[string]any{})
	assertPayloadMatchesSchema(t, "semantic.catalog", catalogPayload, semanticCatalogOutputSchema())

	deliverPayload := execBuiltinPayload(t, builtins, run, sess, "semantic.deliver", map[string]any{
		"action": semanticActionSendMessage,
		"target": map[string]any{
			"kind":      semanticTargetChannel,
			"channel":   "ops",
			"target_id": "room-1",
		},
		"content": "hello",
	})
	assertPayloadMatchesSchema(t, "semantic.deliver", deliverPayload, semanticDeliverOutputSchema())

	inspectPayload := execBuiltinPayload(t, builtins, run, sess, "semantic.inspect_context", map[string]any{
		"kind": semanticInspectThread,
		"target": map[string]any{
			"kind":       semanticTargetChannel,
			"channel":    "ops",
			"channel_id": "thread-1",
		},
	})
	assertPayloadMatchesSchema(t, "semantic.inspect_context", inspectPayload, semanticInspectOutputSchema())
}

func TestSpreadsheetOutputPayloadMatchesSchemas(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	source := filepath.Join(root, "sheet.csv")
	if err := os.WriteFile(source, []byte("name,qty,price\napple,2,3\norange,5,4\n"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	builtins := NewBuiltins(BuiltinsConfig{Root: root, MaxReadBytes: 64 * 1024})
	run := &agent.Run{ID: "run-sheet-schema"}
	sess := &agent.Session{ID: "sess-sheet-schema"}

	readPayload := execBuiltinPayload(t, builtins, run, sess, "spreadsheet.read_range", map[string]any{
		"path":   "sheet.csv",
		"range":  "A1:C3",
		"header": true,
	})
	assertPayloadMatchesSchema(t, "spreadsheet.read_range", readPayload, spreadsheetOutputSchema(t, "spreadsheet.read_range"))

	writePayload := execBuiltinPayload(t, builtins, run, sess, "spreadsheet.write_range", map[string]any{
		"path":  "sheet.csv",
		"range": "B2:C3",
		"values": []any{
			[]any{"7", "9"},
			[]any{"8", "10"},
		},
	})
	assertPayloadMatchesSchema(t, "spreadsheet.write_range", writePayload, spreadsheetOutputSchema(t, "spreadsheet.write_range"))

	exportPayload := execBuiltinPayload(t, builtins, run, sess, "spreadsheet.export", map[string]any{
		"path":   "sheet.csv",
		"output": "sheet.md",
		"format": "markdown",
	})
	assertPayloadMatchesSchema(t, "spreadsheet.export", exportPayload, spreadsheetOutputSchema(t, "spreadsheet.export"))
}

func TestSpreadsheetXLSXOutputPayloadMatchesSchemas(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	builtins := NewBuiltins(BuiltinsConfig{Root: root, MaxReadBytes: 64 * 1024})
	run := &agent.Run{ID: "run-sheet-xlsx-schema"}
	sess := &agent.Session{ID: "sess-sheet-xlsx-schema"}

	createPayload := execBuiltinPayload(t, builtins, run, sess, "spreadsheet.create", map[string]any{
		"path": "report.xlsx",
		"sheets": []any{
			map[string]any{
				"name": "Data",
				"data": []any{
					[]any{"Name", "Value"},
					[]any{"Alpha", "100"},
				},
			},
			map[string]any{"name": "Notes"},
		},
	})
	assertPayloadMatchesSchema(t, "spreadsheet.create", createPayload, spreadsheetOutputSchema(t, "spreadsheet.create"))

	listPayload := execBuiltinPayload(t, builtins, run, sess, "spreadsheet.list_sheets", map[string]any{
		"path": "report.xlsx",
	})
	assertPayloadMatchesSchema(t, "spreadsheet.list_sheets", listPayload, spreadsheetOutputSchema(t, "spreadsheet.list_sheets"))

	stylePayload := execBuiltinPayload(t, builtins, run, sess, "spreadsheet.set_style", map[string]any{
		"path":  "report.xlsx",
		"sheet": "Data",
		"range": "A1:B1",
		"style": map[string]any{
			"font_bold": true,
			"alignment": "center",
		},
	})
	assertPayloadMatchesSchema(t, "spreadsheet.set_style", stylePayload, spreadsheetOutputSchema(t, "spreadsheet.set_style"))

	f, err := excelize.OpenFile(filepath.Join(root, "report.xlsx"))
	if err != nil {
		t.Fatalf("open xlsx: %v", err)
	}
	_ = f.Close()
}

// spreadsheetOutputSchema looks up a spreadsheet tool's OutputSchema by name from
// the spreadsheet sub-package's ToolDefs and XLSXToolDefs.
func spreadsheetOutputSchema(t *testing.T, name string) map[string]any {
	t.Helper()
	for _, d := range spreadsheetpkg.ToolDefs() {
		if d.Manifest.Name == name {
			return d.Manifest.OutputSchema
		}
	}
	for _, d := range spreadsheetpkg.XLSXToolDefs() {
		if d.Manifest.Name == name {
			return d.Manifest.OutputSchema
		}
	}
	t.Fatalf("spreadsheet tool %q not found", name)
	return nil
}

// documentOutputSchema looks up a document tool's OutputSchema by name from
// the document sub-package's ToolDefs.
func documentOutputSchema(t *testing.T, name string) map[string]any {
	t.Helper()
	for _, d := range document.ToolDefs() {
		if d.Manifest.Name == name {
			return d.Manifest.OutputSchema
		}
	}
	t.Fatalf("document tool %q not found", name)
	return nil
}

func execBuiltinPayload(t *testing.T, builtins *Builtins, run *agent.Run, sess *agent.Session, name string, input map[string]any) map[string]any {
	t.Helper()
	results, err := builtins.ExecuteBatch(context.Background(), run, sess, []agent.ToolCall{{
		ID:    "call-" + name,
		Name:  name,
		Input: input,
	}})
	if err != nil {
		t.Fatalf("%s error: %v", name, err)
	}
	if len(results) != 1 {
		t.Fatalf("%s results = %d, want 1", name, len(results))
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(results[0].Content), &payload); err != nil {
		t.Fatalf("%s unmarshal: %v", name, err)
	}
	return payload
}

func assertPayloadMatchesSchema(t *testing.T, toolName string, payload map[string]any, schema map[string]any) {
	t.Helper()
	assertValueMatchesSchema(t, toolName, payload, schema)
}

func assertValueMatchesSchema(t *testing.T, path string, value any, schema map[string]any) {
	t.Helper()

	schemaType, _ := schema["type"].(string)
	switch schemaType {
	case "object":
		obj, ok := value.(map[string]any)
		if !ok {
			t.Fatalf("%s payload type = %T, want object", path, value)
		}
		props, _ := schema["properties"].(map[string]any)
		for _, key := range schemaRequiredKeys(schema) {
			if _, ok := obj[key]; !ok {
				t.Fatalf("%s missing required key %q", path, key)
			}
		}
		if len(props) == 0 {
			return
		}
		for key, fieldValue := range obj {
			fieldSchema, ok := props[key]
			if !ok {
				t.Fatalf("%s has unexpected key %q not declared in schema", path, key)
			}
			childSchema, ok := fieldSchema.(map[string]any)
			if !ok {
				continue
			}
			assertValueMatchesSchema(t, path+"."+key, fieldValue, childSchema)
		}
	case "array":
		items, ok := value.([]any)
		if !ok {
			t.Fatalf("%s payload type = %T, want array", path, value)
		}
		itemSchema, _ := schema["items"].(map[string]any)
		if len(itemSchema) == 0 {
			return
		}
		for idx, item := range items {
			assertValueMatchesSchema(t, path+"["+strconv.Itoa(idx)+"]", item, itemSchema)
		}
	case "string":
		if _, ok := value.(string); !ok {
			t.Fatalf("%s payload type = %T, want string", path, value)
		}
	case "boolean":
		if _, ok := value.(bool); !ok {
			t.Fatalf("%s payload type = %T, want boolean", path, value)
		}
	case "integer":
		num, ok := value.(float64)
		if !ok || math.Trunc(num) != num {
			t.Fatalf("%s payload type/value = %#v, want integer", path, value)
		}
	case "number":
		if _, ok := value.(float64); !ok {
			t.Fatalf("%s payload type = %T, want number", path, value)
		}
	}
}

func schemaRequiredKeys(schema map[string]any) []string {
	required, ok := schema["required"]
	if !ok {
		return nil
	}
	switch items := required.(type) {
	case []string:
		return append([]string(nil), items...)
	case []any:
		out := make([]string, 0, len(items))
		for _, item := range items {
			if text, ok := item.(string); ok {
				out = append(out, text)
			}
		}
		return out
	default:
		return nil
	}
}
