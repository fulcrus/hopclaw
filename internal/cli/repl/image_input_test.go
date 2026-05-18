package repl

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/fulcrus/hopclaw/acp"
	"github.com/fulcrus/hopclaw/contextengine"
)

type recordingGateway struct {
	message       string
	images        []string
	contentBlocks []contextengine.ContentBlock
	structured    *acp.StructuredCommand
}

type richPromptOnce struct {
	results []RichReadResult
}

func (g *recordingGateway) SubmitRunWithOptions(_ context.Context, _ string, message string, images []string, options acp.PromptOptions) (string, <-chan acp.RunEvent, error) {
	g.message = message
	g.images = append([]string(nil), images...)
	g.contentBlocks = append([]contextengine.ContentBlock(nil), options.ContentBlocks...)
	if options.StructuredCommand != nil {
		cloned := *options.StructuredCommand
		g.structured = &cloned
	} else {
		g.structured = nil
	}
	events := make(chan acp.RunEvent, 1)
	events <- acp.RunEvent{Type: "complete", StopReason: acp.StopEndTurn}
	close(events)
	return "run-1", events, nil
}

func (g *recordingGateway) SubmitRun(ctx context.Context, sessionKey, message string, images []string) (string, <-chan acp.RunEvent, error) {
	return g.SubmitRunWithOptions(ctx, sessionKey, message, images, acp.PromptOptions{})
}

func (g *recordingGateway) CancelRun(context.Context, string, string) error { return nil }
func (g *recordingGateway) ListSessions(context.Context, int, int) ([]acp.SessionInfo, error) {
	return nil, nil
}
func (g *recordingGateway) ResolveSession(_ context.Context, key string) (*acp.SessionInfo, error) {
	return &acp.SessionInfo{SessionID: "sess-1", SessionKey: key, Status: acp.SessionIdle}, nil
}
func (g *recordingGateway) ResetSession(context.Context, string) error { return nil }

func (p *richPromptOnce) ReadLine(_ string, _ *CommandRegistry) (string, error) {
	return "", io.EOF
}

func (p *richPromptOnce) ReadRichLine(_ string, _ *CommandRegistry) (RichReadResult, error) {
	if len(p.results) == 0 {
		return RichReadResult{}, io.EOF
	}
	result := p.results[0]
	p.results = p.results[1:]
	return result, nil
}

func (p *richPromptOnce) ReadApproval(_ string) (rune, error) {
	return 0, io.EOF
}

func (p *richPromptOnce) ReadSecret(_ string) (string, error) {
	return "", io.EOF
}

func TestREPLSubmitExtractsImagePathsIntoPromptImages(t *testing.T) {
	gateway := &recordingGateway{}
	server := acp.NewServer(gateway, acp.ServerConfig{DefaultSessionKey: "default"})
	client, err := acp.NewInProcessClient(context.Background(), server)
	if err != nil {
		t.Fatalf("NewInProcessClient() error = %v", err)
	}
	defer client.Close()

	ctx := context.Background()
	if _, err := client.Initialize(ctx, acp.InitializeParams{
		ProtocolVersion: "2024-11-05",
		ClientInfo:      acp.Implementation{Name: "repl-test", Version: "1.0.0"},
	}); err != nil {
		t.Fatalf("Initialize() error = %v", err)
	}
	session, err := client.NewSession(ctx, acp.NewSessionParams{SessionKey: "repl-images"})
	if err != nil {
		t.Fatalf("NewSession() error = %v", err)
	}

	imagePath := t.TempDir() + "/sample.png"
	if err := os.WriteFile(imagePath, []byte("fake-png"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	repl := &REPL{
		client:     client,
		service:    &fakeService{},
		renderer:   NewRenderer(io.Discard, false),
		streamer:   NewStreamer(client.Notifications()),
		sessionID:  session.SessionID,
		sessionKey: session.SessionKey,
	}

	if err := repl.submit(ctx, "inspect "+imagePath); err != nil {
		t.Fatalf("submit() error = %v", err)
	}
	if gateway.message != "inspect" {
		t.Fatalf("gateway.message = %q, want %q", gateway.message, "inspect")
	}
	if len(gateway.images) != 1 || !strings.HasPrefix(gateway.images[0], "data:image/png;base64,") {
		t.Fatalf("gateway.images = %#v", gateway.images)
	}
	if len(gateway.contentBlocks) != 2 || gateway.contentBlocks[1].Type != contextengine.ContentBlockImage {
		t.Fatalf("gateway.contentBlocks = %#v, want text+image content blocks", gateway.contentBlocks)
	}
}

func TestREPLSubmitUsesRichEditorImagesWhenAvailable(t *testing.T) {
	gateway := &recordingGateway{}
	server := acp.NewServer(gateway, acp.ServerConfig{DefaultSessionKey: "default"})
	client, err := acp.NewInProcessClient(context.Background(), server)
	if err != nil {
		t.Fatalf("NewInProcessClient() error = %v", err)
	}
	defer client.Close()

	ctx := context.Background()
	if _, err := client.Initialize(ctx, acp.InitializeParams{
		ProtocolVersion: "2024-11-05",
		ClientInfo:      acp.Implementation{Name: "repl-test", Version: "1.0.0"},
	}); err != nil {
		t.Fatalf("Initialize() error = %v", err)
	}
	session, err := client.NewSession(ctx, acp.NewSessionParams{SessionKey: "repl-images"})
	if err != nil {
		t.Fatalf("NewSession() error = %v", err)
	}

	repl := &REPL{
		client:     client,
		service:    &fakeService{},
		renderer:   NewRenderer(io.Discard, false),
		streamer:   NewStreamer(client.Notifications()),
		sessionID:  session.SessionID,
		sessionKey: session.SessionKey,
		lastImages: []string{"data:image/png;base64,ZmFrZS1wbmc="},
	}

	if err := repl.submit(ctx, "inspect"); err != nil {
		t.Fatalf("submit() error = %v", err)
	}
	if gateway.message != "inspect" {
		t.Fatalf("gateway.message = %q, want %q", gateway.message, "inspect")
	}
	if len(gateway.images) != 1 || gateway.images[0] != "data:image/png;base64,ZmFrZS1wbmc=" {
		t.Fatalf("gateway.images = %#v", gateway.images)
	}
	if len(gateway.contentBlocks) != 1 || gateway.contentBlocks[0].Type != contextengine.ContentBlockImage {
		t.Fatalf("gateway.contentBlocks = %#v, want image content block", gateway.contentBlocks)
	}
	if repl.lastImages != nil {
		t.Fatalf("repl.lastImages = %#v, want cleared after submit", repl.lastImages)
	}
}

func TestREPLRunSubmitsPureImagePrompt(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	gateway := &recordingGateway{}
	server := acp.NewServer(gateway, acp.ServerConfig{DefaultSessionKey: "default"})
	client, err := acp.NewInProcessClient(context.Background(), server)
	if err != nil {
		t.Fatalf("NewInProcessClient() error = %v", err)
	}
	defer client.Close()

	repl, err := New(Config{
		Client:   client,
		Service:  &fakeService{detail: &SessionDetail{Summary: SessionSummary{ID: "sess-1", Key: "repl-images", Model: "test-model"}}},
		Prompter: &richPromptOnce{results: []RichReadResult{{Images: []string{"data:image/png;base64,ZmFrZS1wbmc="}}}},
		Renderer: NewRenderer(io.Discard, false),
		History:  NewHistory("", 10),
		Version:  "test",
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	if err := repl.Run(context.Background()); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if gateway.message != "" {
		t.Fatalf("gateway.message = %q, want empty prompt text for pure image submission", gateway.message)
	}
	if len(gateway.images) != 1 || gateway.images[0] != "data:image/png;base64,ZmFrZS1wbmc=" {
		t.Fatalf("gateway.images = %#v", gateway.images)
	}
	if len(gateway.contentBlocks) != 1 || gateway.contentBlocks[0].Type != contextengine.ContentBlockImage {
		t.Fatalf("gateway.contentBlocks = %#v, want image content block", gateway.contentBlocks)
	}
}

func TestREPLRunClearsInfoPanelBeforeSubmittingRichPromptInput(t *testing.T) {

	tests := []struct {
		name   string
		result RichReadResult
	}{
		{
			name:   "text",
			result: RichReadResult{Text: "inspect this"},
		},
		{
			name:   "image-only",
			result: RichReadResult{Images: []string{"data:image/png;base64,ZmFrZS1wbmc="}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gateway := &recordingGateway{}
			server := acp.NewServer(gateway, acp.ServerConfig{DefaultSessionKey: "default"})
			client, err := acp.NewInProcessClient(context.Background(), server)
			if err != nil {
				t.Fatalf("NewInProcessClient() error = %v", err)
			}
			defer client.Close()

			repl, err := New(Config{
				Client:   client,
				Service:  &fakeService{detail: &SessionDetail{Summary: SessionSummary{ID: "sess-1", Key: "panel-clear", Model: "test-model"}}},
				Prompter: &richPromptOnce{results: []RichReadResult{tt.result}},
				Renderer: NewRenderer(io.Discard, false),
				History:  NewHistory("", 10),
				Version:  "test",
			})
			if err != nil {
				t.Fatalf("New() error = %v", err)
			}

			repl.panelController = &infoPanel{repl: repl, title: "Badge"}
			repl.activePanel = "Badge"

			if err := repl.Run(context.Background()); err != nil {
				t.Fatalf("Run() error = %v", err)
			}
			if repl.panelController != nil || repl.activePanel != "" {
				t.Fatalf("panel not cleared after submit: controller=%#v activePanel=%q", repl.panelController, repl.activePanel)
			}
		})
	}
}

func TestAttachCommandStagesAttachmentForNextMessage(t *testing.T) {
	gateway := &recordingGateway{}
	server := acp.NewServer(gateway, acp.ServerConfig{DefaultSessionKey: "default"})
	client, err := acp.NewInProcessClient(context.Background(), server)
	if err != nil {
		t.Fatalf("NewInProcessClient() error = %v", err)
	}
	defer client.Close()

	ctx := context.Background()
	if _, err := client.Initialize(ctx, acp.InitializeParams{
		ProtocolVersion: "2024-11-05",
		ClientInfo:      acp.Implementation{Name: "repl-test", Version: "1.0.0"},
	}); err != nil {
		t.Fatalf("Initialize() error = %v", err)
	}
	session, err := client.NewSession(ctx, acp.NewSessionParams{SessionKey: "attach-stage"})
	if err != nil {
		t.Fatalf("NewSession() error = %v", err)
	}

	filePath := filepath.Join(t.TempDir(), "brief.md")
	if err := os.WriteFile(filePath, []byte("# brief\n"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	repl := &REPL{
		client:     client,
		service:    &fakeService{},
		renderer:   NewRenderer(io.Discard, false),
		streamer:   NewStreamer(client.Notifications()),
		sessionID:  session.SessionID,
		sessionKey: session.SessionKey,
		commands:   NewCommandRegistry(),
	}

	if _, err := repl.commands.Execute(ctx, repl, "/attach file "+filePath); err != nil {
		t.Fatalf("Execute(/attach) error = %v", err)
	}
	if gateway.message != "" || len(gateway.contentBlocks) != 0 {
		t.Fatalf("attach command should stage without submitting, message=%q blocks=%#v", gateway.message, gateway.contentBlocks)
	}
	if len(repl.lastContentBlocks) != 1 || repl.lastContentBlocks[0].Type != contextengine.ContentBlockFile {
		t.Fatalf("lastContentBlocks = %#v, want one staged file block", repl.lastContentBlocks)
	}

	if err := repl.submit(ctx, "review this attachment"); err != nil {
		t.Fatalf("submit() error = %v", err)
	}
	if gateway.message != "review this attachment" {
		t.Fatalf("gateway.message = %q, want %q", gateway.message, "review this attachment")
	}
	if len(gateway.contentBlocks) != 1 || gateway.contentBlocks[0].Type != contextengine.ContentBlockFile || gateway.contentBlocks[0].Path != filePath {
		t.Fatalf("gateway.contentBlocks = %#v, want staged file block", gateway.contentBlocks)
	}
}

func TestAttachCommandOneShotSubmitsAttachment(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	gateway := &recordingGateway{}
	server := acp.NewServer(gateway, acp.ServerConfig{DefaultSessionKey: "default"})
	client, err := acp.NewInProcessClient(context.Background(), server)
	if err != nil {
		t.Fatalf("NewInProcessClient() error = %v", err)
	}
	defer client.Close()

	filePath := filepath.Join(t.TempDir(), "brief.md")
	if err := os.WriteFile(filePath, []byte("# brief\n"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	repl, err := New(Config{
		Client:         client,
		Service:        &fakeService{detail: &SessionDetail{Summary: SessionSummary{ID: "sess-1", Key: "attach-one-shot", Model: "test-model"}}},
		Prompter:       &richPromptOnce{},
		Renderer:       NewRenderer(io.Discard, false),
		History:        NewHistory("", 10),
		Version:        "test",
		InitialMessage: "/attach file " + filePath,
		OneShot:        true,
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	if err := repl.Run(context.Background()); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if gateway.message != "" {
		t.Fatalf("gateway.message = %q, want empty attachment-only prompt", gateway.message)
	}
	if len(gateway.contentBlocks) != 1 || gateway.contentBlocks[0].Type != contextengine.ContentBlockFile || gateway.contentBlocks[0].Path != filePath {
		t.Fatalf("gateway.contentBlocks = %#v, want one file attachment block", gateway.contentBlocks)
	}
}

func TestREPLSubmitPureImagePathKeepsEmptyPromptText(t *testing.T) {
	gateway := &recordingGateway{}
	server := acp.NewServer(gateway, acp.ServerConfig{DefaultSessionKey: "default"})
	client, err := acp.NewInProcessClient(context.Background(), server)
	if err != nil {
		t.Fatalf("NewInProcessClient() error = %v", err)
	}
	defer client.Close()

	ctx := context.Background()
	if _, err := client.Initialize(ctx, acp.InitializeParams{
		ProtocolVersion: "2024-11-05",
		ClientInfo:      acp.Implementation{Name: "repl-test", Version: "1.0.0"},
	}); err != nil {
		t.Fatalf("Initialize() error = %v", err)
	}
	session, err := client.NewSession(ctx, acp.NewSessionParams{SessionKey: "repl-images"})
	if err != nil {
		t.Fatalf("NewSession() error = %v", err)
	}

	imagePath := t.TempDir() + "/sample.png"
	if err := os.WriteFile(imagePath, []byte("fake-png"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	repl := &REPL{
		client:     client,
		service:    &fakeService{},
		renderer:   NewRenderer(io.Discard, false),
		streamer:   NewStreamer(client.Notifications()),
		sessionID:  session.SessionID,
		sessionKey: session.SessionKey,
	}

	if err := repl.submit(ctx, imagePath); err != nil {
		t.Fatalf("submit() error = %v", err)
	}
	if gateway.message != "" {
		t.Fatalf("gateway.message = %q, want empty prompt text for pure image path", gateway.message)
	}
	if len(gateway.images) != 1 {
		t.Fatalf("gateway.images = %#v, want one image", gateway.images)
	}
}

func TestREPLRunForwardsStructuredContentBlocksFromRichPrompt(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	gateway := &recordingGateway{}
	server := acp.NewServer(gateway, acp.ServerConfig{DefaultSessionKey: "default"})
	client, err := acp.NewInProcessClient(context.Background(), server)
	if err != nil {
		t.Fatalf("NewInProcessClient() error = %v", err)
	}
	defer client.Close()

	blocks := []contextengine.ContentBlock{
		{Type: contextengine.ContentBlockText, Text: "inspect this"},
		{Type: contextengine.ContentBlockFile, Label: "spec.md", Path: "/tmp/spec.md"},
	}
	repl, err := New(Config{
		Client:   client,
		Service:  &fakeService{detail: &SessionDetail{Summary: SessionSummary{ID: "sess-1", Key: "repl-structured", Model: "test-model"}}},
		Prompter: &richPromptOnce{results: []RichReadResult{{Text: "inspect this", ContentBlocks: blocks}}},
		Renderer: NewRenderer(io.Discard, false),
		History:  NewHistory("", 10),
		Version:  "test",
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	if err := repl.Run(context.Background()); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if gateway.message != "inspect this" {
		t.Fatalf("gateway.message = %q, want %q", gateway.message, "inspect this")
	}
	if !reflect.DeepEqual(gateway.contentBlocks, blocks) {
		t.Fatalf("gateway.contentBlocks = %#v, want %#v", gateway.contentBlocks, blocks)
	}
}

func TestREPLResumePausedPrefersStructuredRetryWhenRunIDAvailable(t *testing.T) {
	gateway := &recordingGateway{}
	server := acp.NewServer(gateway, acp.ServerConfig{DefaultSessionKey: "default"})
	client, err := acp.NewInProcessClient(context.Background(), server)
	if err != nil {
		t.Fatalf("NewInProcessClient() error = %v", err)
	}
	defer client.Close()

	ctx := context.Background()
	if _, err := client.Initialize(ctx, acp.InitializeParams{
		ProtocolVersion: "2024-11-05",
		ClientInfo:      acp.Implementation{Name: "repl-test", Version: "1.0.0"},
	}); err != nil {
		t.Fatalf("Initialize() error = %v", err)
	}
	session, err := client.NewSession(ctx, acp.NewSessionParams{SessionKey: "repl-retry"})
	if err != nil {
		t.Fatalf("NewSession() error = %v", err)
	}

	repl := &REPL{
		client:     client,
		service:    &fakeService{},
		renderer:   NewRenderer(io.Discard, false),
		streamer:   NewStreamer(client.Notifications()),
		sessionID:  session.SessionID,
		sessionKey: session.SessionKey,
		pausedRun: &pausedRunState{
			Message: "resume me",
			RunID:   "run-source-1",
		},
	}

	if err := repl.resumePaused(ctx, true); err != nil {
		t.Fatalf("resumePaused() error = %v", err)
	}
	if gateway.structured == nil {
		t.Fatal("gateway.structured = nil, want structured retry")
	}
	if gateway.structured.Kind != "retry" || gateway.structured.RunID != "run-source-1" {
		t.Fatalf("gateway.structured = %#v", gateway.structured)
	}
}
