package toolruntime

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/fulcrus/hopclaw/agent"
	"github.com/fulcrus/hopclaw/channels"
	channelmgr "github.com/fulcrus/hopclaw/channels/manager"
	"github.com/fulcrus/hopclaw/config"
)

type semanticTestAdapter struct {
	sentMessages []channels.OutboundMessage
	history      []channels.HistoryMessage
	actionData   map[string]*channels.ChannelActionResult
}

func (a *semanticTestAdapter) Connect(context.Context) error    { return nil }
func (a *semanticTestAdapter) Disconnect(context.Context) error { return nil }
func (a *semanticTestAdapter) Capabilities() channels.Capabilities {
	return channels.Capabilities{
		SendText:       true,
		SendRichText:   true,
		SendFile:       true,
		ReceiveEvent:   true,
		ReceiveMessage: true,
	}
}
func (a *semanticTestAdapter) Status() channels.Status { return channels.StatusConnected }
func (a *semanticTestAdapter) SubscribeEvents() <-chan channels.InboundMessage {
	ch := make(chan channels.InboundMessage)
	close(ch)
	return ch
}
func (a *semanticTestAdapter) Send(_ context.Context, msg channels.OutboundMessage) error {
	a.sentMessages = append(a.sentMessages, msg)
	return nil
}
func (a *semanticTestAdapter) ReadHistory(_ context.Context, _ string, limit int, _ string) ([]channels.HistoryMessage, error) {
	if limit >= len(a.history) {
		return append([]channels.HistoryMessage(nil), a.history...), nil
	}
	return append([]channels.HistoryMessage(nil), a.history[:limit]...), nil
}
func (a *semanticTestAdapter) ExecuteAction(_ context.Context, _ string, action channels.ChannelAction) (*channels.ChannelActionResult, error) {
	if result, ok := a.actionData[action.Type]; ok {
		return result, nil
	}
	return &channels.ChannelActionResult{Success: false, Error: "unknown action"}, nil
}

type semanticEmbeddingStub struct{}

func (semanticEmbeddingStub) Embed(_ context.Context, texts []string) ([][]float32, error) {
	out := make([][]float32, 0, len(texts))
	for _, text := range texts {
		switch {
		case strings.Contains(strings.ToLower(text), "hello"), strings.Contains(strings.ToLower(text), "greeting"):
			out = append(out, []float32{1, 0})
		case strings.Contains(strings.ToLower(text), "world"), strings.Contains(strings.ToLower(text), "planet"):
			out = append(out, []float32{0, 1})
		default:
			out = append(out, []float32{0.5, 0.5})
		}
	}
	return out, nil
}

func TestSemanticDeliverChannelAndInspect(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "report.txt"), []byte("all systems go"), 0o644); err != nil {
		t.Fatalf("write attachment: %v", err)
	}

	builtins := NewBuiltins(BuiltinsConfig{Root: root, MaxReadBytes: 64 * 1024})
	manager := channelmgr.New()
	adapter := &semanticTestAdapter{
		history: []channels.HistoryMessage{
			{ID: "m1", ChannelID: "thread-1", SenderID: "u1", Content: "hello", Timestamp: "2026-03-18T10:00:00Z"},
			{ID: "m2", ChannelID: "thread-1", SenderID: "u2", Content: "world", Timestamp: "2026-03-18T10:01:00Z"},
		},
		actionData: map[string]*channels.ChannelActionResult{
			semanticInspectParticipants: {
				Success: true,
				Data: map[string]any{
					"participants": []any{"u1", "u2"},
				},
			},
		},
	}
	if err := manager.Register("ops", adapter); err != nil {
		t.Fatalf("register adapter: %v", err)
	}
	builtins.ApplyBindings(BuiltinsBindings{ChannelManager: manager})

	run := &agent.Run{ID: "run-semantic-channel"}
	sess := &agent.Session{ID: "sess-semantic-channel"}

	payload := execBuiltinPayload(t, builtins, run, sess, "semantic.deliver", map[string]any{
		"action": semanticActionSendCard,
		"target": map[string]any{
			"kind":       semanticTargetChannel,
			"channel":    "ops",
			"target_id":  "team-room",
			"channel_id": "thread-1",
		},
		"content": "Daily report",
		"blocks": []any{
			map[string]any{"kind": "section", "title": "Summary", "content": "Everything green"},
		},
		"attachments": []any{
			map[string]any{"path": "report.txt", "label": "report.txt", "content_type": "text/plain"},
		},
	})

	if payload["status"] != semanticStatusOK {
		t.Fatalf("deliver status = %v", payload["status"])
	}
	if len(adapter.sentMessages) != 1 {
		t.Fatalf("sent messages = %d, want 1", len(adapter.sentMessages))
	}
	if got := adapter.sentMessages[0].Format; got != "rich" {
		t.Fatalf("message format = %q, want rich", got)
	}
	if got := len(adapter.sentMessages[0].Blocks); got != 1 {
		t.Fatalf("block count = %d, want 1", got)
	}
	if got := len(adapter.sentMessages[0].Attachments); got != 1 {
		t.Fatalf("attachment count = %d, want 1", got)
	}

	threadPayload := execBuiltinPayload(t, builtins, run, sess, "semantic.inspect_context", map[string]any{
		"kind": semanticInspectThread,
		"target": map[string]any{
			"kind":       semanticTargetChannel,
			"channel":    "ops",
			"channel_id": "thread-1",
		},
		"limit": 2,
	})
	if threadPayload["status"] != semanticStatusOK {
		t.Fatalf("inspect thread status = %v", threadPayload["status"])
	}
	result, ok := threadPayload["result"].(map[string]any)
	if !ok {
		t.Fatalf("thread result type = %T", threadPayload["result"])
	}
	messages, ok := result["messages"].([]any)
	if !ok || len(messages) != 2 {
		t.Fatalf("thread messages = %#v", result["messages"])
	}

	participantsPayload := execBuiltinPayload(t, builtins, run, sess, "semantic.inspect_context", map[string]any{
		"kind": semanticInspectParticipants,
		"target": map[string]any{
			"kind":       semanticTargetChannel,
			"channel":    "ops",
			"channel_id": "thread-1",
		},
	})
	if participantsPayload["status"] != semanticStatusOK {
		t.Fatalf("inspect participants status = %v", participantsPayload["status"])
	}
}

func TestSemanticDeliverDocumentScheduleAndUpload(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "report.txt"), []byte("upload me"), 0o644); err != nil {
		t.Fatalf("write upload file: %v", err)
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("unexpected method %s", r.Method)
		}
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer server.Close()

	parsedURL, err := url.Parse(server.URL)
	if err != nil {
		t.Fatalf("parse server url: %v", err)
	}

	builtins := NewBuiltins(BuiltinsConfig{
		Root: root,
		NetConstraints: config.NetConstraints{
			AllowHosts: []string{parsedURL.Hostname()},
		},
	})
	run := &agent.Run{ID: "run-semantic-office"}
	sess := &agent.Session{ID: "sess-semantic-office"}

	docPayload := execBuiltinPayload(t, builtins, run, sess, "semantic.deliver", map[string]any{
		"action": semanticActionCreateDocument,
		"target": map[string]any{"kind": semanticTargetDocument},
		"document": map[string]any{
			"path":   "brief.docx",
			"title":  "Brief",
			"author": "HopClaw",
			"content": []any{
				map[string]any{"text": "Brief Title", "style": "heading1"},
				map[string]any{"text": "Body paragraph"},
			},
		},
	})
	if docPayload["status"] != semanticStatusOK {
		t.Fatalf("document status = %v", docPayload["status"])
	}
	if _, err := os.Stat(filepath.Join(root, "brief.docx")); err != nil {
		t.Fatalf("stat brief.docx: %v", err)
	}

	schedulePayload := execBuiltinPayload(t, builtins, run, sess, "semantic.deliver", map[string]any{
		"action": semanticActionCreateSchedule,
		"target": map[string]any{"kind": semanticTargetCalendar},
		"schedule": map[string]any{
			"path":    "meeting.ics",
			"summary": "Design Review",
			"start":   "2026-03-20T10:00:00Z",
			"end":     "2026-03-20T11:00:00Z",
		},
	})
	if schedulePayload["status"] != semanticStatusOK {
		t.Fatalf("schedule status = %v", schedulePayload["status"])
	}
	if _, err := os.Stat(filepath.Join(root, "meeting.ics")); err != nil {
		t.Fatalf("stat meeting.ics: %v", err)
	}

	uploadPayload := execBuiltinPayload(t, builtins, run, sess, "semantic.deliver", map[string]any{
		"action": semanticActionUploadAttachment,
		"target": map[string]any{
			"kind":   semanticTargetHTTPUpload,
			"url":    server.URL,
			"field":  "upload",
			"method": "POST",
		},
		"attachments": []any{
			map[string]any{"path": "report.txt", "label": "report.txt"},
		},
	})
	if uploadPayload["status"] != semanticStatusOK {
		t.Fatalf("upload status = %v", uploadPayload["status"])
	}
}

func TestSemanticUnsupportedAction(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	builtins := NewBuiltins(BuiltinsConfig{Root: root})
	run := &agent.Run{ID: "run-semantic-unsupported"}
	sess := &agent.Session{ID: "sess-semantic-unsupported"}

	payload := execBuiltinPayload(t, builtins, run, sess, "semantic.deliver", map[string]any{
		"action": "edit_document",
		"target": map[string]any{"kind": semanticTargetDocument},
	})
	if payload["status"] != semanticStatusUnsupported {
		t.Fatalf("unsupported status = %v", payload["status"])
	}
}

func TestSemanticCatalogSchema(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	builtins := NewBuiltins(BuiltinsConfig{Root: root})
	run := &agent.Run{ID: "run-semantic-catalog"}
	sess := &agent.Session{ID: "sess-semantic-catalog"}
	payload := execBuiltinPayload(t, builtins, run, sess, "semantic.catalog", map[string]any{})
	assertPayloadMatchesSchema(t, "semantic.catalog", payload, semanticCatalogOutputSchema())
}

func TestSemanticSearchSimilarityAndEmbeddingInspect(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	store := agent.NewInMemoryKVStore()
	store.SetEmbedding(semanticEmbeddingStub{})
	if err := store.Set(context.Background(), "greeting", "hello team"); err != nil {
		t.Fatalf("Set(greeting) error = %v", err)
	}
	if err := store.Set(context.Background(), "planet", "world news"); err != nil {
		t.Fatalf("Set(planet) error = %v", err)
	}

	builtins := NewBuiltins(BuiltinsConfig{Root: root})
	builtins.ApplyBindings(BuiltinsBindings{MemoryStore: store})
	run := &agent.Run{ID: "run-semantic-memory"}
	sess := &agent.Session{ID: "sess-semantic-memory"}

	searchPayload := execBuiltinPayload(t, builtins, run, sess, "semantic.deliver", map[string]any{
		"action": semanticActionSearch,
		"query":  "hello",
		"mode":   memorySearchModeSemantic,
	})
	if searchPayload["status"] != semanticStatusOK {
		t.Fatalf("search status = %v", searchPayload["status"])
	}
	searchResult, ok := searchPayload["result"].(map[string]any)
	if !ok {
		t.Fatalf("search result type = %T", searchPayload["result"])
	}
	entries, ok := searchResult["entries"].([]any)
	if !ok || len(entries) == 0 {
		t.Fatalf("search entries = %#v", searchResult["entries"])
	}
	first, ok := entries[0].(map[string]any)
	if !ok || first["key"] != "greeting" {
		t.Fatalf("first semantic search result = %#v", entries[0])
	}

	similarityPayload := execBuiltinPayload(t, builtins, run, sess, "semantic.deliver", map[string]any{
		"action":    semanticActionSimilarity,
		"query":     "hello",
		"candidate": "greeting from the team",
	})
	if similarityPayload["status"] != semanticStatusOK {
		t.Fatalf("similarity status = %v", similarityPayload["status"])
	}
	similarityResult, ok := similarityPayload["result"].(map[string]any)
	if !ok {
		t.Fatalf("similarity result type = %T", similarityPayload["result"])
	}
	score, ok := similarityResult["score"].(float64)
	if !ok || score < 0.99 {
		t.Fatalf("similarity score = %#v", similarityResult["score"])
	}

	inspectPayload := execBuiltinPayload(t, builtins, run, sess, "semantic.inspect_context", map[string]any{
		"kind": semanticInspectEmbedding,
	})
	if inspectPayload["status"] != semanticStatusOK {
		t.Fatalf("inspect embedding status = %v", inspectPayload["status"])
	}
	inspectResult, ok := inspectPayload["result"].(map[string]any)
	if !ok {
		t.Fatalf("inspect result type = %T", inspectPayload["result"])
	}
	if configured, ok := inspectResult["embedding_configured"].(bool); !ok || !configured {
		t.Fatalf("embedding_configured = %#v", inspectResult["embedding_configured"])
	}
	if vectorCount, ok := inspectResult["vector_count"].(float64); !ok || vectorCount < 2 {
		t.Fatalf("vector_count = %#v", inspectResult["vector_count"])
	}
}
