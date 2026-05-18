package toolruntime

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/fulcrus/hopclaw/agent"
	"github.com/fulcrus/hopclaw/config"
	"github.com/fulcrus/hopclaw/policy"
)

func execBuiltinCall(t *testing.T, builtins *Builtins, name string, input map[string]any) (contextengineJSON map[string]any, raw string, err error) {
	t.Helper()
	results, execErr := builtins.ExecuteBatch(context.Background(), &agent.Run{ID: "run-sec"}, &agent.Session{ID: "sess-sec"}, []agent.ToolCall{{
		ID:    "call-" + name,
		Name:  name,
		Input: input,
	}})
	if execErr != nil {
		return nil, "", execErr
	}
	raw = results[0].Content
	if unmarshalErr := json.Unmarshal([]byte(raw), &contextengineJSON); unmarshalErr != nil {
		t.Fatalf("json.Unmarshal(%s): %v", name, unmarshalErr)
	}
	return contextengineJSON, raw, nil
}

func TestBuiltinsRejectSymlinkEscapeReadAndWrite(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	outside := t.TempDir()
	target := filepath.Join(outside, "secret.txt")
	if err := os.WriteFile(target, []byte("secret"), 0o644); err != nil {
		t.Fatalf("WriteFile(target): %v", err)
	}
	linkPath := filepath.Join(root, "secret-link.txt")
	if err := os.Symlink(target, linkPath); err != nil {
		t.Fatalf("Symlink(): %v", err)
	}

	builtins := NewBuiltins(BuiltinsConfig{Root: root})
	_, _, err := execBuiltinCall(t, builtins, "fs.read", map[string]any{"path": "secret-link.txt"})
	if err == nil || !strings.Contains(err.Error(), "escapes builtin root") {
		t.Fatalf("fs.read symlink escape err = %v, want sandbox rejection", err)
	}

	_, _, err = execBuiltinCall(t, builtins, "fs.write", map[string]any{"path": "secret-link.txt", "content": "changed"})
	if err == nil || !strings.Contains(err.Error(), "escapes builtin root") {
		t.Fatalf("fs.write symlink escape err = %v, want sandbox rejection", err)
	}

	got, readErr := os.ReadFile(target)
	if readErr != nil {
		t.Fatalf("ReadFile(target): %v", readErr)
	}
	if string(got) != "secret" {
		t.Fatalf("outside target = %q, want unchanged", string(got))
	}
}

func TestBuiltinsFSConstraintsHideDeniedPathsAndSkipDirs(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, ".env"), []byte("TOKEN=1"), 0o644); err != nil {
		t.Fatalf("WriteFile(.env): %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "keep.txt"), []byte("hello"), 0o644); err != nil {
		t.Fatalf("WriteFile(keep): %v", err)
	}
	if err := os.MkdirAll(filepath.Join(root, "node_modules"), 0o755); err != nil {
		t.Fatalf("MkdirAll(node_modules): %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "node_modules", "pkg.txt"), []byte("ignored"), 0o644); err != nil {
		t.Fatalf("WriteFile(pkg): %v", err)
	}

	builtins := NewBuiltins(BuiltinsConfig{
		Root: root,
		FSConstraints: config.FSConstraints{
			DenyPatterns: []string{"*.env"},
			SkipDirs:     []string{"node_modules"},
		},
	})
	payload, _, err := execBuiltinCall(t, builtins, "fs.list", map[string]any{"path": ".", "recursive": true})
	if err != nil {
		t.Fatalf("fs.list error: %v", err)
	}

	entries, _ := payload["entries"].([]any)
	joined := rawListStrings(entries)
	if strings.Contains(joined, ".env") {
		t.Fatalf("fs.list leaked denied path: %s", joined)
	}
	if strings.Contains(joined, "node_modules") {
		t.Fatalf("fs.list leaked skipped dir: %s", joined)
	}
	if !strings.Contains(joined, "keep.txt") {
		t.Fatalf("fs.list missing allowed file: %s", joined)
	}
}

func TestExecConstraintsEnforced(t *testing.T) {
	t.Parallel()

	builtins := NewBuiltins(BuiltinsConfig{
		Root: t.TempDir(),
		ExecConstraints: config.ExecConstraints{
			Mode:      "allowlist",
			Allowlist: []string{"echo*", "/bin/sh"},
			Denylist:  []string{"*forbidden*"},
			MaxOutput: 5,
		},
	})

	_, raw, err := execBuiltinCall(t, builtins, "exec.run", map[string]any{
		"command": "echo",
		"args":    []any{"hello-world"},
	})
	if err != nil {
		t.Fatalf("exec.run allowlisted error = %v", err)
	}
	if !strings.Contains(raw, "...[truncated]") {
		t.Fatalf("exec.run output should be truncated, got %s", raw)
	}

	_, _, err = execBuiltinCall(t, builtins, "exec.run", map[string]any{
		"command": "printf",
		"args":    []any{"forbidden"},
	})
	if err == nil || !strings.Contains(err.Error(), "denylist") {
		t.Fatalf("exec.run denylist err = %v, want denylist rejection", err)
	}

	_, _, err = execBuiltinCall(t, builtins, "exec.shell", map[string]any{
		"command": "uname -a",
	})
	if err == nil || !strings.Contains(err.Error(), "allowlist") {
		t.Fatalf("exec.shell err = %v, want allowlist rejection", err)
	}
}

func TestRedirectProtectionRejectsLoopbackTargets(t *testing.T) {
	t.Parallel()

	client := newSSRFProtectedHTTPClient(config.NetConstraints{AllowLocal: boolPtr(false)})
	nextReq := httptest.NewRequest(http.MethodGet, "http://127.0.0.1/private", nil)
	redirectReq := &http.Request{URL: nextReq.URL}
	viaURL, _ := url.Parse("https://public.example/path")
	via := []*http.Request{{URL: viaURL}}

	err := client.CheckRedirect(redirectReq, via)
	if err == nil || !strings.Contains(err.Error(), "localhost access is not allowed") {
		t.Fatalf("CheckRedirect err = %v, want loopback rejection", err)
	}
}

func TestNetDownloadRespectsMaxDownload(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("0123456789"))
	}))
	defer server.Close()

	root := t.TempDir()
	builtins := NewBuiltins(BuiltinsConfig{
		Root: root,
		NetConstraints: config.NetConstraints{
			AllowLocal:  boolPtr(true),
			MaxDownload: 4,
		},
	})
	_, _, err := execBuiltinCall(t, builtins, "net.download", map[string]any{
		"url":  server.URL,
		"path": "download.bin",
	})
	if err == nil || !strings.Contains(err.Error(), "max_download") {
		t.Fatalf("net.download err = %v, want max_download rejection", err)
	}
	if _, statErr := os.Stat(filepath.Join(root, "download.bin")); !os.IsNotExist(statErr) {
		t.Fatalf("download.bin should be removed on limit breach, stat err = %v", statErr)
	}
}

func TestExternalToolsCarryApprovalMetadata(t *testing.T) {
	t.Parallel()

	executor := NewExternalToolExecutor([]ExternalToolConfig{{
		Name:        "ext.call",
		Description: "external tool",
		Endpoint:    "https://example.com/tool",
	}})
	bound, ok := executor.ResolveTool(nil, "ext.call")
	if !ok {
		t.Fatal("ResolveTool(ext.call) = false")
	}
	if !bound.Manifest.RequiresApproval {
		t.Fatal("external tool should require approval")
	}
	if bound.Manifest.SideEffectClass != externalToolDefaultSideEffect {
		t.Fatalf("side_effect_class = %q", bound.Manifest.SideEffectClass)
	}
	if bound.Package == nil || bound.Package.Trust != "community" {
		t.Fatalf("trust = %#v, want community", bound.Package)
	}

	engine := policy.NewDefaultEngine(policy.Config{
		RequireApprovalForWrite:  true,
		RequireApprovalCommunity: true,
	})
	decision, err := engine.EvaluateTool(context.Background(), policy.ToolContext{
		ToolName: "ext.call",
		Tool:     bound,
	})
	if err != nil {
		t.Fatalf("EvaluateTool() error = %v", err)
	}
	if decision.Action != policy.ActionRequireApproval {
		t.Fatalf("decision.Action = %q, want require_approval", decision.Action)
	}
}

func rawListStrings(entries []any) string {
	parts := make([]string, 0, len(entries))
	for _, entry := range entries {
		item, _ := entry.(map[string]any)
		if path, _ := item["path"].(string); path != "" {
			parts = append(parts, path)
		}
	}
	return strings.Join(parts, ",")
}

func boolPtr(v bool) *bool { return &v }
