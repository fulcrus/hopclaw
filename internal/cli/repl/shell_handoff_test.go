package repl

import (
	"context"
	"encoding/base64"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/fulcrus/hopclaw/acp"
)

func TestShellHandoffCommand(t *testing.T) {
	command, ok := shellHandoffCommand("  !git status --short  ")
	if !ok {
		t.Fatal("shellHandoffCommand() should detect !cmd input")
	}
	if command != "git status --short" {
		t.Fatalf("shellHandoffCommand() = %q, want %q", command, "git status --short")
	}

	if _, ok := shellHandoffCommand("!"); ok {
		t.Fatal("shellHandoffCommand() should reject empty commands")
	}
	if _, ok := shellHandoffCommand("git status"); ok {
		t.Fatal("shellHandoffCommand() should ignore normal prompts")
	}
}

func TestREPLSubmitRewritesShellHandoffWithoutImageExtraction(t *testing.T) {
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
	session, err := client.NewSession(ctx, acp.NewSessionParams{SessionKey: "repl-shell"})
	if err != nil {
		t.Fatalf("NewSession() error = %v", err)
	}

	imagePath := writeShellTestPNG(t)
	repl := &REPL{
		client:     client,
		service:    &fakeService{},
		renderer:   NewRenderer(io.Discard, false),
		streamer:   NewStreamer(client.Notifications()),
		sessionID:  session.SessionID,
		sessionKey: session.SessionKey,
	}

	if err := repl.submit(ctx, "!cat "+imagePath); err != nil {
		t.Fatalf("submit() error = %v", err)
	}
	if len(gateway.images) != 0 {
		t.Fatalf("gateway.images = %#v, want no image attachments for shell handoff", gateway.images)
	}
	if !strings.Contains(gateway.message, "Use the `exec.shell` tool") {
		t.Fatalf("gateway.message = %q, want shell handoff instruction", gateway.message)
	}
	if !strings.Contains(gateway.message, "Command:\ncat "+imagePath) {
		t.Fatalf("gateway.message = %q, want exact command payload", gateway.message)
	}
}

func TestApprovalCardFieldsUseStructuredSignalsInsteadOfKeywords(t *testing.T) {
	card := approvalCardFields(acp.PermissionRequest{
		RequestID:                  "approval-shell",
		ToolName:                   "outil.exec",
		Description:                "需要审批",
		Input:                      `{"commande":"git status --short"}`,
		DefaultGrantScope:          "once",
		MaxGrantScope:              "session",
		RequiresExternalSideEffect: true,
	})

	if card.Action != "outil.exec" {
		t.Fatalf("Action = %q, want %q", card.Action, "outil.exec")
	}
	if card.Reason != "需要审批" {
		t.Fatalf("Reason = %q, want %q", card.Reason, "需要审批")
	}
	if card.Impact != "destructive" {
		t.Fatalf("Impact = %q, want %q", card.Impact, "destructive")
	}
	if !strings.Contains(card.Input, `"commande":"git status --short"`) {
		t.Fatalf("Input = %q, want exact command payload", card.Input)
	}
	if card.Scope != "once | conversation" {
		t.Fatalf("Scope = %q, want %q", card.Scope, "once | conversation")
	}
	if !card.AllowSession {
		t.Fatal("AllowSession = false, want true")
	}
}

func TestApprovalDecisionScopeHonorsStructuredPolicy(t *testing.T) {
	if got := approvalDecisionScope('a', acp.PermissionRequest{MaxGrantScope: "session"}); got != "session" {
		t.Fatalf("approvalDecisionScope(session) = %q, want %q", got, "session")
	}
	if got := approvalDecisionScope('a', acp.PermissionRequest{MaxGrantScope: "once"}); got != "once" {
		t.Fatalf("approvalDecisionScope(once) = %q, want %q", got, "once")
	}
	if approvalAllowsSessionGrant(acp.PermissionRequest{MaxGrantScope: "once"}) {
		t.Fatal("approvalAllowsSessionGrant(once) = true, want false")
	}
}

func writeShellTestPNG(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "sample.png")
	data, err := base64.StdEncoding.DecodeString("iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO+tmIoAAAAASUVORK5CYII=")
	if err != nil {
		t.Fatalf("DecodeString() error = %v", err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("WriteFile(%q) error = %v", path, err)
	}
	return path
}
