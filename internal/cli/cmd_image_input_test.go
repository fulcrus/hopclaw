package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/fulcrus/hopclaw/acp"
	"github.com/fulcrus/hopclaw/agent"
	"github.com/fulcrus/hopclaw/bootstrap"
	"github.com/fulcrus/hopclaw/contextengine"
	"github.com/fulcrus/hopclaw/eventbus"
	"github.com/fulcrus/hopclaw/internal/meta"
	runtimesvc "github.com/fulcrus/hopclaw/runtime"
)

type cliImageModelClient struct{}

func (cliImageModelClient) Chat(_ context.Context, _ agent.ChatRequest) (*agent.ModelResponse, error) {
	return &agent.ModelResponse{
		Message: contextengine.Message{Role: contextengine.RoleAssistant, Content: "ok"},
	}, nil
}

type cliStreamingChatModelClient struct {
	firstDelta chan struct{}
	release    chan struct{}
}

func (m *cliStreamingChatModelClient) Chat(_ context.Context, _ agent.ChatRequest) (*agent.ModelResponse, error) {
	return &agent.ModelResponse{
		Message: contextengine.Message{Role: contextengine.RoleAssistant, Content: "I am here.\n"},
	}, nil
}

func (m *cliStreamingChatModelClient) ChatStream(ctx context.Context, req agent.ChatRequest, cb agent.StreamCallback) (*agent.ModelResponse, error) {
	if cb != nil {
		cb.OnTextDelta(ctx, "I am ")
	}
	if m.firstDelta != nil {
		select {
		case <-m.firstDelta:
		default:
			close(m.firstDelta)
		}
	}
	if m.release != nil && strings.TrimSpace(req.RunID) == "" {
		<-m.release
	}
	if cb != nil {
		cb.OnTextDelta(ctx, "here.\n")
		cb.OnComplete(ctx)
	}
	return &agent.ModelResponse{
		Message: contextengine.Message{Role: contextengine.RoleAssistant, Content: "I am here.\n"},
	}, nil
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func jsonResponse(body any) (*http.Response, error) {
	data, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Header:     make(http.Header),
		Body:       io.NopCloser(bytes.NewReader(data)),
	}
	resp.Header.Set("Content-Type", "application/json")
	return resp, nil
}

func TestExternalInteractiveGatewaySubmitRunWithOptionsForwardsImages(t *testing.T) {

	var runRequest struct {
		SessionKey    string                       `json:"session_key"`
		Content       string                       `json:"content"`
		ContentBlocks []contextengine.ContentBlock `json:"content_blocks"`
		Images        []string                     `json:"images"`
		Model         string                       `json:"model"`
		Metadata      map[string]any               `json:"metadata"`
	}

	client := &GatewayClient{
		BaseURL: "http://cli.test",
		HTTP: &http.Client{
			Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
				switch r.URL.Path {
				case "/runtime/events":
					return jsonResponse(map[string]any{"items": []any{}, "count": 0})
				case "/runtime/interact":
					if err := json.NewDecoder(r.Body).Decode(&runRequest); err != nil {
						t.Fatalf("decode /runtime/interact body: %v", err)
					}
					return jsonResponse(map[string]any{
						"decision": map[string]any{"reply_act": "task_accept"},
						"run": map[string]any{
							"id":         "run-1",
							"session_id": "sess-1",
							"status":     "running",
							"model":      "gpt-4o",
						},
						"submit_request": map[string]any{
							"session_key": runRequest.SessionKey,
							"content":     runRequest.Content,
						},
					})
				case "/runtime/events/stream":
					return &http.Response{
						StatusCode: http.StatusOK,
						Header: http.Header{
							"Content-Type": []string{"text/event-stream"},
						},
						Body: io.NopCloser(strings.NewReader("")),
					}, nil
				default:
					return &http.Response{
						StatusCode: http.StatusNotFound,
						Body:       io.NopCloser(strings.NewReader("not found")),
						Header:     make(http.Header),
					}, nil
				}
			}),
		},
	}

	gateway := &externalInteractiveGateway{client: client}

	runID, _, err := gateway.SubmitRunWithOptions(context.Background(), "cli-images", "describe", []string{"data:image/png;base64,ZmFrZQ=="}, acp.PromptOptions{
		Model: "gpt-4o",
	})
	if err != nil {
		t.Fatalf("SubmitRunWithOptions() error = %v", err)
	}
	if runID != "run-1" {
		t.Fatalf("runID = %q, want run-1", runID)
	}
	if len(runRequest.Images) != 1 || runRequest.Images[0] != "data:image/png;base64,ZmFrZQ==" {
		t.Fatalf("runRequest.Images = %#v", runRequest.Images)
	}
	if len(runRequest.ContentBlocks) != 0 {
		t.Fatalf("runRequest.ContentBlocks = %#v, want empty when only images were supplied", runRequest.ContentBlocks)
	}
	if runRequest.Model != "gpt-4o" {
		t.Fatalf("runRequest.Model = %q, want gpt-4o", runRequest.Model)
	}
}

func TestExternalInteractiveGatewaySubmitRunWithOptionsForwardsContentBlocks(t *testing.T) {

	var runRequest struct {
		SessionKey    string                       `json:"session_key"`
		Content       string                       `json:"content"`
		ContentBlocks []contextengine.ContentBlock `json:"content_blocks"`
	}

	client := &GatewayClient{
		BaseURL: "http://cli.test",
		HTTP: &http.Client{
			Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
				switch r.URL.Path {
				case "/runtime/events":
					return jsonResponse(map[string]any{"items": []any{}, "count": 0})
				case "/runtime/interact":
					if err := json.NewDecoder(r.Body).Decode(&runRequest); err != nil {
						t.Fatalf("decode /runtime/interact body: %v", err)
					}
					return jsonResponse(map[string]any{
						"decision": map[string]any{"reply_act": "task_accept"},
						"run": map[string]any{
							"id":         "run-structured-1",
							"session_id": "sess-1",
							"status":     "running",
						},
						"submit_request": map[string]any{
							"session_key":    runRequest.SessionKey,
							"content":        runRequest.Content,
							"content_blocks": runRequest.ContentBlocks,
						},
					})
				case "/runtime/events/stream":
					return &http.Response{
						StatusCode: http.StatusOK,
						Header: http.Header{
							"Content-Type": []string{"text/event-stream"},
						},
						Body: io.NopCloser(strings.NewReader("")),
					}, nil
				default:
					return &http.Response{
						StatusCode: http.StatusNotFound,
						Body:       io.NopCloser(strings.NewReader("not found")),
						Header:     make(http.Header),
					}, nil
				}
			}),
		},
	}

	gateway := &externalInteractiveGateway{client: client}
	blocks := []contextengine.ContentBlock{
		{Type: contextengine.ContentBlockText, Text: "inspect this"},
		{Type: contextengine.ContentBlockFile, Label: "spec.md", Path: "/tmp/spec.md"},
	}

	runID, _, err := gateway.SubmitRunWithOptions(context.Background(), "cli-blocks", "inspect this", nil, acp.PromptOptions{
		ContentBlocks: blocks,
	})
	if err != nil {
		t.Fatalf("SubmitRunWithOptions() error = %v", err)
	}
	if runID != "run-structured-1" {
		t.Fatalf("runID = %q, want run-structured-1", runID)
	}
	if !reflect.DeepEqual(runRequest.ContentBlocks, blocks) {
		t.Fatalf("runRequest.ContentBlocks = %#v, want %#v", runRequest.ContentBlocks, blocks)
	}
}

func TestExternalInteractiveGatewaySubmitRunWithOptionsForwardsStructuredRetry(t *testing.T) {

	var runRequest struct {
		SessionKey        string `json:"session_key"`
		Content           string `json:"content"`
		StructuredCommand struct {
			Kind  string `json:"kind"`
			RunID string `json:"run_id"`
		} `json:"structured_command"`
	}

	client := &GatewayClient{
		BaseURL: "http://cli.test",
		HTTP: &http.Client{
			Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
				switch r.URL.Path {
				case "/runtime/events":
					return jsonResponse(map[string]any{"items": []any{}, "count": 0})
				case "/runtime/interact":
					if err := json.NewDecoder(r.Body).Decode(&runRequest); err != nil {
						t.Fatalf("decode /runtime/interact body: %v", err)
					}
					return jsonResponse(map[string]any{
						"decision": map[string]any{"reply_act": "resume_ack"},
						"run": map[string]any{
							"id":         "run-retry-1",
							"session_id": "sess-1",
							"status":     "running",
						},
						"submit_request": map[string]any{
							"session_key": runRequest.SessionKey,
							"content":     "retry source",
						},
					})
				case "/runtime/events/stream":
					return &http.Response{
						StatusCode: http.StatusOK,
						Header: http.Header{
							"Content-Type": []string{"text/event-stream"},
						},
						Body: io.NopCloser(strings.NewReader("")),
					}, nil
				default:
					return &http.Response{
						StatusCode: http.StatusNotFound,
						Body:       io.NopCloser(strings.NewReader("not found")),
						Header:     make(http.Header),
					}, nil
				}
			}),
		},
	}

	gateway := &externalInteractiveGateway{client: client}
	runID, _, err := gateway.SubmitRunWithOptions(context.Background(), "cli-retry", "", nil, acp.PromptOptions{
		StructuredCommand: &acp.StructuredCommand{Kind: "retry", RunID: "run-source-1"},
	})
	if err != nil {
		t.Fatalf("SubmitRunWithOptions() error = %v", err)
	}
	if runID != "run-retry-1" {
		t.Fatalf("runID = %q, want run-retry-1", runID)
	}
	if runRequest.SessionKey != "cli-retry" {
		t.Fatalf("runRequest.SessionKey = %q, want cli-retry", runRequest.SessionKey)
	}
	if runRequest.StructuredCommand.Kind != "retry" || runRequest.StructuredCommand.RunID != "run-source-1" {
		t.Fatalf("runRequest.StructuredCommand = %#v", runRequest.StructuredCommand)
	}
}

type cliInteractionClassifier struct {
	decision runtimesvc.InteractionDecision
	err      error
}

func (c cliInteractionClassifier) Classify(context.Context, runtimesvc.InteractionClassifyRequest) (runtimesvc.InteractionDecision, error) {
	if c.err != nil {
		return runtimesvc.InteractionDecision{}, c.err
	}
	return c.decision, nil
}

func TestExternalInteractiveGatewaySubmitRunWithOptionsUsesInteractForChatReply(t *testing.T) {

	turnIDCh := make(chan string, 1)
	firstDeltaWritten := make(chan struct{})
	release := make(chan struct{})
	streamOpened := make(chan struct{}, 1)
	pipeReader, pipeWriter := io.Pipe()
	client := &GatewayClient{
		BaseURL: "http://cli.test",
		HTTP: &http.Client{
			Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
				switch r.URL.Path {
				case "/runtime/events":
					return jsonResponse(map[string]any{"items": []any{}, "count": 0})
				case "/runtime/interact":
					var req map[string]any
					if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
						t.Fatalf("decode /runtime/interact body: %v", err)
					}
					if got := req["content"]; got != "hi" {
						t.Fatalf("content = %#v, want %q", got, "hi")
					}
					metadata, _ := req["metadata"].(map[string]any)
					turnID, _ := metadata["interaction_turn_id"].(string)
					if strings.TrimSpace(turnID) == "" {
						t.Fatalf("metadata.interaction_turn_id = %#v, want non-empty", metadata["interaction_turn_id"])
					}
					turnIDCh <- turnID
					<-release
					return jsonResponse(map[string]any{
						"decision": map[string]any{
							"reply_act": "chat_reply",
							"reason":    "chit_chat",
						},
						"message": "I am here.",
					})
				case "/runtime/events/stream":
					select {
					case streamOpened <- struct{}{}:
					default:
					}
					go func() {
						turnID := <-turnIDCh
						_, _ = pipeWriter.Write([]byte(fmt.Sprintf("data: {\"id\":\"evt-1\",\"type\":\"model.text_delta\",\"session_id\":\"sess-1\",\"attrs\":{\"delta\":\"I am \",\"interaction_turn_id\":%q}}\n\n", turnID)))
						close(firstDeltaWritten)
						<-release
						_, _ = pipeWriter.Write([]byte(fmt.Sprintf("data: {\"id\":\"evt-2\",\"type\":\"model.text_delta\",\"session_id\":\"sess-1\",\"attrs\":{\"delta\":\"here.\\n\",\"interaction_turn_id\":%q}}\n\n", turnID)))
						_, _ = pipeWriter.Write([]byte(fmt.Sprintf("data: {\"id\":\"evt-3\",\"type\":\"model.stream_complete\",\"session_id\":\"sess-1\",\"attrs\":{\"interaction_turn_id\":%q}}\n\n", turnID)))
						_ = pipeWriter.Close()
					}()
					return &http.Response{
						StatusCode: http.StatusOK,
						Header: http.Header{
							"Content-Type": []string{"text/event-stream"},
						},
						Body: pipeReader,
					}, nil
				default:
					return &http.Response{
						StatusCode: http.StatusNotFound,
						Body:       io.NopCloser(strings.NewReader("not found")),
						Header:     make(http.Header),
					}, nil
				}
			}),
		},
	}

	gateway := &externalInteractiveGateway{client: client}
	type submitResult struct {
		runID  string
		events <-chan acp.RunEvent
		err    error
	}
	submitted := make(chan submitResult, 1)
	go func() {
		runID, events, err := gateway.SubmitRunWithOptions(context.Background(), "cli-chat", "hi", nil, acp.PromptOptions{})
		submitted <- submitResult{runID: runID, events: events, err: err}
	}()
	select {
	case <-streamOpened:
	case <-time.After(2 * time.Second):
		t.Fatal("event stream was not opened")
	}
	select {
	case <-firstDeltaWritten:
	case <-time.After(2 * time.Second):
		t.Fatal("first runless delta was not written")
	}
	var result submitResult
	select {
	case result = <-submitted:
	case <-time.After(2 * time.Second):
		t.Fatal("SubmitRunWithOptions did not return after first runless delta")
	}
	close(release)
	if result.err != nil {
		t.Fatalf("SubmitRunWithOptions() error = %v", result.err)
	}
	if result.runID != "" {
		t.Fatalf("runID = %q, want empty for chat reply", result.runID)
	}
	var got []acp.RunEvent
	for event := range result.events {
		got = append(got, event)
	}
	if len(got) != 3 {
		t.Fatalf("events = %#v, want 3 streamed events", got)
	}
	if got[0].Type != "text_delta" || got[0].Text != "I am " {
		t.Fatalf("first event = %#v, want first streamed delta", got[0])
	}
	if got[2].Type != "complete" || !got[2].Runless {
		t.Fatalf("last event = %#v, want runless complete", got[2])
	}
}

func TestMessageSendCommandDefinesImageFlag(t *testing.T) {

	cmd := newMessageSendCmd()
	if flag := cmd.Flags().Lookup("image"); flag == nil {
		t.Fatal("expected --image flag to be registered")
	}
}

func TestRunMessageSendWithClientEncodesDataURI(t *testing.T) {

	imagePath := t.TempDir() + "/sample.png"
	if err := os.WriteFile(imagePath, []byte("fake-png"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	var runRequest messageSendRequest
	client := &GatewayClient{
		BaseURL: "http://cli.test",
		HTTP: &http.Client{
			Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
				switch r.URL.Path {
				case "/runtime/runs":
					if err := json.NewDecoder(r.Body).Decode(&runRequest); err != nil {
						t.Fatalf("decode /runtime/runs body: %v", err)
					}
					return jsonResponse(map[string]any{
						"id":         "run-1",
						"session_id": "sess-1",
						"status":     "completed",
					})
				case "/runtime/sessions/sess-1":
					return jsonResponse(map[string]any{
						"id":  "sess-1",
						"key": "cli:chat-images",
						"messages": []map[string]any{{
							"role":    "assistant",
							"content": "done",
						}},
					})
				default:
					return &http.Response{
						StatusCode: http.StatusNotFound,
						Body:       io.NopCloser(strings.NewReader("not found")),
						Header:     make(http.Header),
					}, nil
				}
			}),
		},
	}

	oldJSON, oldVerbose := flagJSON, flagVerbose
	flagJSON, flagVerbose = false, false
	defer func() {
		flagJSON, flagVerbose = oldJSON, oldVerbose
	}()

	if err := runMessageSendWithClient(context.Background(), client, "chat-images", "cli", "hello", []string{imagePath}); err != nil {
		t.Fatalf("runMessageSendWithClient() error = %v", err)
	}

	if len(runRequest.Images) != 1 || !strings.HasPrefix(runRequest.Images[0], "data:image/png;base64,") {
		t.Fatalf("runRequest.Images = %#v", runRequest.Images)
	}
	if got := runRequest.Metadata[meta.KeyChannel]; got != "cli" {
		t.Fatalf("runRequest.Metadata[channel] = %#v, want cli", got)
	}
	if got := runRequest.Metadata[meta.KeyChatType]; got != meta.ChatTypeDirect.String() {
		t.Fatalf("runRequest.Metadata[chat_type] = %#v, want direct", got)
	}
	rawCaps, ok := runRequest.Metadata[meta.KeyChannelCapabilities].(map[string]any)
	if !ok {
		t.Fatalf("runRequest.Metadata[channel_capabilities] = %#v, want map", runRequest.Metadata[meta.KeyChannelCapabilities])
	}
	if got := rawCaps["interactive"]; got != true {
		t.Fatalf("channel_capabilities.interactive = %#v, want true", got)
	}
	if got := rawCaps["inline_delivery"]; got != false {
		t.Fatalf("channel_capabilities.inline_delivery = %#v, want false", got)
	}
}

func TestEmbeddedInteractiveGatewaySubmitRunWithOptionsForwardsImages(t *testing.T) {

	sessions := agent.NewInMemorySessionStore()
	runs := agent.NewInMemoryRunStore()
	queue := agent.NewInMemoryCoordinator()
	bus := eventbus.NewInMemoryBus()
	component := agent.NewComponent(agent.AgentConfig{
		DefaultModel: "test-model",
		QueueMode:    agent.QueueEnqueue,
	}, sessions, runs, queue, contextengine.NewSlidingWindowEngine(contextengine.Config{
		BaseSystemPrompt:     "test",
		DefaultContextWindow: 1024,
		DefaultOutputTokens:  128,
	}, nil), cliImageModelClient{}, nil, nil).WithEventBus(bus)
	runtimeSvc := runtimesvc.NewService(component, sessions, runs, nil, bus, nil).
		WithClassifier(cliInteractionClassifier{
			decision: runtimesvc.InteractionDecision{
				SpeechAct:   runtimesvc.SpeechActNewTask,
				TargetScope: runtimesvc.TargetScopeNewRun,
				ReplyAct:    runtimesvc.ReplyActTaskAccept,
				Reason:      "content_blocks",
				Confidence:  0.99,
			},
		})

	gateway := &embeddedInteractiveGateway{
		app: &bootstrap.App{
			AppRuntimeState: bootstrap.AppRuntimeState{Runtime: runtimeSvc},
			AppStoreState:   bootstrap.AppStoreState{Sessions: sessions},
		},
	}

	runID, events, err := gateway.SubmitRunWithOptions(context.Background(), "embedded-images", "describe", []string{"data:image/png;base64,ZmFrZQ=="}, acp.PromptOptions{})
	if err != nil {
		t.Fatalf("SubmitRunWithOptions() error = %v", err)
	}
	if runID == "" {
		t.Fatal("expected non-empty run ID")
	}
	for range events {
	}

	session, err := agent.LoadSessionByKey(context.Background(), sessions, "embedded-images", agent.ScopeFilter{})
	if err != nil {
		t.Fatalf("LoadSessionByKey() error = %v", err)
	}
	if len(session.Messages) == 0 || len(session.Messages[0].ContentBlocks) != 2 {
		t.Fatalf("session.Messages = %#v", session.Messages)
	}
	if session.Messages[0].ContentBlocks[1].Type != contextengine.ContentBlockImage {
		t.Fatalf("image block = %#v", session.Messages[0].ContentBlocks[1])
	}
}

func TestEmbeddedInteractiveGatewaySubmitRunWithOptionsForwardsContentBlocks(t *testing.T) {

	sessions := agent.NewInMemorySessionStore()
	runs := agent.NewInMemoryRunStore()
	queue := agent.NewInMemoryCoordinator()
	bus := eventbus.NewInMemoryBus()
	component := agent.NewComponent(agent.AgentConfig{
		DefaultModel: "test-model",
		QueueMode:    agent.QueueEnqueue,
	}, sessions, runs, queue, contextengine.NewSlidingWindowEngine(contextengine.Config{
		BaseSystemPrompt:     "test",
		DefaultContextWindow: 1024,
		DefaultOutputTokens:  128,
	}, nil), cliImageModelClient{}, nil, nil).WithEventBus(bus)
	runtimeSvc := runtimesvc.NewService(component, sessions, runs, nil, bus, nil).
		WithClassifier(cliInteractionClassifier{
			decision: runtimesvc.InteractionDecision{
				SpeechAct:   runtimesvc.SpeechActNewTask,
				TargetScope: runtimesvc.TargetScopeNewRun,
				ReplyAct:    runtimesvc.ReplyActTaskAccept,
				Reason:      "content_blocks",
				Confidence:  0.99,
			},
		})

	gateway := &embeddedInteractiveGateway{
		app: &bootstrap.App{
			AppRuntimeState: bootstrap.AppRuntimeState{Runtime: runtimeSvc},
			AppStoreState:   bootstrap.AppStoreState{Sessions: sessions},
		},
	}
	blocks := []contextengine.ContentBlock{
		{Type: contextengine.ContentBlockText, Text: "inspect this"},
		{Type: contextengine.ContentBlockFile, Label: "spec.md", Path: "/tmp/spec.md"},
	}

	runID, events, err := gateway.SubmitRunWithOptions(context.Background(), "embedded-blocks", "inspect this", nil, acp.PromptOptions{
		ContentBlocks: blocks,
	})
	if err != nil {
		t.Fatalf("SubmitRunWithOptions() error = %v", err)
	}
	if runID == "" {
		t.Fatal("expected non-empty run ID")
	}
	for range events {
	}

	session, err := agent.LoadSessionByKey(context.Background(), sessions, "embedded-blocks", agent.ScopeFilter{})
	if err != nil {
		t.Fatalf("LoadSessionByKey() error = %v", err)
	}
	if len(session.Messages) == 0 {
		t.Fatalf("session.Messages = %#v", session.Messages)
	}
	if !reflect.DeepEqual(session.Messages[0].ContentBlocks, blocks) {
		t.Fatalf("session.Messages[0].ContentBlocks = %#v, want %#v", session.Messages[0].ContentBlocks, blocks)
	}
}

func TestEmbeddedInteractiveGatewaySubmitRunWithOptionsForwardsStructuredRetry(t *testing.T) {

	sessions := agent.NewInMemorySessionStore()
	runs := agent.NewInMemoryRunStore()
	queue := agent.NewInMemoryCoordinator()
	bus := eventbus.NewInMemoryBus()
	component := agent.NewComponent(agent.AgentConfig{
		DefaultModel: "test-model",
		QueueMode:    agent.QueueEnqueue,
	}, sessions, runs, queue, contextengine.NewSlidingWindowEngine(contextengine.Config{
		BaseSystemPrompt:     "test",
		DefaultContextWindow: 1024,
		DefaultOutputTokens:  128,
	}, nil), cliImageModelClient{}, nil, nil).WithEventBus(bus)
	runtimeSvc := runtimesvc.NewService(component, sessions, runs, nil, bus, nil)

	original, err := runtimeSvc.Submit(context.Background(), runtimesvc.SubmitRequest{
		SessionKey: "embedded-retry",
		Content:    "retry this",
		Model:      "test-model",
	})
	if err != nil {
		t.Fatalf("Submit() error = %v", err)
	}
	if original == nil {
		t.Fatal("expected original run")
	}
	deadline := time.Now().Add(time.Second)
	completed := false
	for time.Now().Before(deadline) {
		current, getErr := runs.Get(context.Background(), original.ID)
		if getErr == nil && current != nil && current.Status == agent.RunCompleted {
			completed = true
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if !completed {
		t.Fatalf("original run %q did not complete before retry", original.ID)
	}

	gateway := &embeddedInteractiveGateway{
		app: &bootstrap.App{
			AppRuntimeState: bootstrap.AppRuntimeState{Runtime: runtimeSvc},
			AppStoreState:   bootstrap.AppStoreState{Sessions: sessions},
		},
	}

	runID, events, err := gateway.SubmitRunWithOptions(context.Background(), "embedded-retry", "", nil, acp.PromptOptions{
		StructuredCommand: &acp.StructuredCommand{Kind: "retry", RunID: original.ID},
	})
	if err != nil {
		t.Fatalf("SubmitRunWithOptions() error = %v", err)
	}
	if runID == "" || runID == original.ID {
		t.Fatalf("runID = %q, want a new retry run ID", runID)
	}
	for range events {
	}

	session, err := agent.LoadSessionByKey(context.Background(), sessions, "embedded-retry", agent.ScopeFilter{})
	if err != nil {
		t.Fatalf("LoadSessionByKey() error = %v", err)
	}
	foundRetryInput := false
	for i := len(session.Messages) - 1; i >= 0; i-- {
		msg := session.Messages[i]
		if msg.Role != contextengine.RoleUser {
			continue
		}
		if strings.TrimSpace(msg.Content) == "retry this" {
			foundRetryInput = true
			break
		}
	}
	if !foundRetryInput {
		t.Fatalf("session.Messages = %#v, want retry run to append retry input", session.Messages)
	}
}

func TestEmbeddedInteractiveGatewaySubmitRunWithOptionsPromotesCLIChatReplyToRun(t *testing.T) {

	sessions := agent.NewInMemorySessionStore()
	runs := agent.NewInMemoryRunStore()
	queue := agent.NewInMemoryCoordinator()
	bus := eventbus.NewInMemoryBus()
	model := &cliStreamingChatModelClient{
		firstDelta: make(chan struct{}),
		release:    make(chan struct{}),
	}
	component := agent.NewComponent(agent.AgentConfig{
		DefaultModel: "test-model",
		QueueMode:    agent.QueueEnqueue,
	}, sessions, runs, queue, contextengine.NewSlidingWindowEngine(contextengine.Config{
		BaseSystemPrompt:     "test",
		DefaultContextWindow: 1024,
		DefaultOutputTokens:  128,
	}, nil), model, nil, nil).WithEventBus(bus)
	runtimeSvc := runtimesvc.NewService(component, sessions, runs, nil, bus, nil).
		WithClassifier(cliInteractionClassifier{
			decision: runtimesvc.InteractionDecision{
				SpeechAct:   runtimesvc.SpeechActCasualChat,
				TargetScope: runtimesvc.TargetScopeNone,
				ReplyAct:    runtimesvc.ReplyActChatReply,
				Reason:      "chit_chat",
				Confidence:  0.98,
			},
		})

	gateway := &embeddedInteractiveGateway{
		app: &bootstrap.App{
			AppRuntimeState: bootstrap.AppRuntimeState{Runtime: runtimeSvc},
			AppStoreState:   bootstrap.AppStoreState{Sessions: sessions},
		},
	}

	type submitResult struct {
		runID  string
		events <-chan acp.RunEvent
		err    error
	}
	submitted := make(chan submitResult, 1)
	go func() {
		runID, events, err := gateway.SubmitRunWithOptions(context.Background(), "embedded-chat", "hi", nil, acp.PromptOptions{})
		submitted <- submitResult{runID: runID, events: events, err: err}
	}()
	select {
	case <-model.firstDelta:
	case <-time.After(2 * time.Second):
		t.Fatal("first embedded runless delta was not emitted")
	}
	var result submitResult
	select {
	case result = <-submitted:
	case <-time.After(2 * time.Second):
		t.Fatal("SubmitRunWithOptions did not return after first embedded runless delta")
	}
	close(model.release)
	if result.err != nil {
		t.Fatalf("SubmitRunWithOptions() error = %v", result.err)
	}
	if result.runID != "" {
		t.Fatalf("runID = %q, want empty for chat reply", result.runID)
	}
	var got []acp.RunEvent
	for event := range result.events {
		got = append(got, event)
	}
	if len(got) != 3 {
		t.Fatalf("events = %#v, want 3 streamed events", got)
	}
	if got[0].Type != "text_delta" || got[0].Text != "I am " {
		t.Fatalf("first event = %#v, want first streamed delta", got[0])
	}
	if got[2].Type != "complete" || !got[2].Runless {
		t.Fatalf("last event = %#v, want runless complete", got[2])
	}
	session, err := agent.LoadSessionByKey(context.Background(), sessions, "embedded-chat", agent.ScopeFilter{})
	if err != nil {
		t.Fatalf("LoadSessionByKey() error = %v", err)
	}
	if len(session.Messages) != 2 {
		t.Fatalf("session.Messages = %#v", session.Messages)
	}
}
