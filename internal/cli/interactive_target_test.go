package cli

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/fulcrus/hopclaw/config"
)

func TestEnsureUniqueTargetNamesUsesFallbackBaseName(t *testing.T) {
	items := []interactiveTarget{
		{BaseURL: "http://127.0.0.1:16280"},
		{BaseURL: "http://127.0.0.1:16281"},
		{Name: "dev", BaseURL: "http://127.0.0.1:16282"},
		{Name: "dev", BaseURL: "http://127.0.0.1:16283"},
	}

	ensureUniqueTargetNames(items)

	got := []string{items[0].Name, items[1].Name, items[2].Name, items[3].Name}
	want := []string{"local-serve", "local-serve-16281", "dev", "dev-16283"}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("names[%d] = %q, want %q (all=%v)", i, got[i], want[i], got)
		}
	}
}

func TestRegisterServeInstanceUsesRequestedNameAndCleansUp(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	cfg := config.Config{
		Server: config.ServerConfig{Address: "127.0.0.1:16280"},
		Runtime: config.RuntimeConfig{
			Profile: config.RuntimeProfileDesktop,
		},
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	lease, err := registerServeInstance(ctx, cfg, "/tmp/config.yaml", "local-dev")
	if err != nil {
		t.Fatalf("registerServeInstance() error = %v", err)
	}

	entries, err := os.ReadDir(serveInstanceDir())
	if err != nil {
		t.Fatalf("ReadDir() error = %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("len(entries) = %d, want 1", len(entries))
	}
	record, ok := loadLocalServeInstanceRecord(filepath.Join(serveInstanceDir(), entries[0].Name()))
	if !ok {
		t.Fatal("expected valid instance record")
	}
	if record.Name != "local-dev" {
		t.Fatalf("record.Name = %q, want %q", record.Name, "local-dev")
	}
	if record.BaseURL != "http://127.0.0.1:16280" {
		t.Fatalf("record.BaseURL = %q, want %q", record.BaseURL, "http://127.0.0.1:16280")
	}

	lease.Close()

	entries, err = os.ReadDir(serveInstanceDir())
	if err != nil {
		t.Fatalf("ReadDir() after Close error = %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("len(entries) after Close = %d, want 0", len(entries))
	}
}

func TestResolveInitialInteractiveTargetPromptsForDetectedLocalServe(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	oldRemote := flagRemote
	oldLocal := flagLocal
	flagRemote = ""
	flagLocal = false
	t.Cleanup(func() {
		flagRemote = oldRemote
		flagLocal = oldLocal
	})

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/healthz":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"ok":true}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	configPath := writeInteractiveConfig(t, server.URL)
	t.Setenv("HOPCLAW_CONFIG", configPath)

	cfg, err := config.Load(configPath)
	if err != nil {
		t.Fatalf("config.Load() error = %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	lease, err := registerServeInstance(ctx, cfg, configPath, "local-dev")
	if err != nil {
		t.Fatalf("registerServeInstance() error = %v", err)
	}
	defer lease.Close()

	var output bytes.Buffer
	target, err := resolveInitialInteractiveTarget(context.Background(), strings.NewReader("local-dev\n"), &output, true)
	if err != nil {
		t.Fatalf("resolveInitialInteractiveTarget() error = %v", err)
	}
	if target.Kind != interactiveTargetLocal {
		t.Fatalf("target.Kind = %q, want %q", target.Kind, interactiveTargetLocal)
	}
	if target.Name != "local-dev" {
		t.Fatalf("target.Name = %q, want %q", target.Name, "local-dev")
	}
	text := output.String()
	if !strings.Contains(text, "Detected a local HopClaw runtime.") {
		t.Fatalf("prompt output = %q, want detection header", text)
	}
	if !strings.Contains(text, "local") {
		t.Fatalf("prompt output = %q, want local option", text)
	}
}

func TestResolveInitialInteractiveTargetLocalChoice(t *testing.T) {
	target, err := promptForInteractiveTarget(strings.NewReader("2\n"), ioDiscardBuffer{}, []interactiveTarget{{
		Kind:        interactiveTargetLocal,
		Name:        "local-dev",
		Description: "http://127.0.0.1:16280",
	}})
	if err != nil {
		t.Fatalf("promptForInteractiveTarget() error = %v", err)
	}
	if target.Kind != interactiveTargetLocal {
		t.Fatalf("target.Kind = %q, want %q", target.Kind, interactiveTargetLocal)
	}
	if target.Name != localTargetName {
		t.Fatalf("target.Name = %q, want %q", target.Name, localTargetName)
	}
}

func TestPromptForInteractiveTargetUsesRuntimeRetryHint(t *testing.T) {
	var output bytes.Buffer
	target, err := promptForInteractiveTarget(strings.NewReader("oops\nlocal\n"), &output, []interactiveTarget{{
		Kind:        interactiveTargetLocal,
		Name:        "local-dev",
		Description: "http://127.0.0.1:16280",
	}})
	if err != nil {
		t.Fatalf("promptForInteractiveTarget() error = %v", err)
	}
	if target.Name != localTargetName {
		t.Fatalf("target.Name = %q, want %q", target.Name, localTargetName)
	}
	if !strings.Contains(output.String(), "Please choose a listed number or remote name.") {
		t.Fatalf("prompt output = %q", output.String())
	}
}

func TestResolveNamedInteractiveTargetUsesRuntimeErrors(t *testing.T) {
	if _, err := resolveNamedInteractiveTarget(context.Background(), "", interactiveTarget{}); err == nil || !strings.Contains(err.Error(), "remote name is required") {
		t.Fatalf("resolveNamedInteractiveTarget(empty) error = %v", err)
	}
	if _, err := resolveNamedInteractiveTarget(context.Background(), "missing-runtime", interactiveTarget{}); err == nil || !strings.Contains(err.Error(), `remote "missing-runtime" not found`) {
		t.Fatalf("resolveNamedInteractiveTarget(missing) error = %v", err)
	}
}

func TestResolveNamedInteractiveTargetDoesNotTreatStandaloneAsLocalAlias(t *testing.T) {
	if _, err := resolveNamedInteractiveTarget(context.Background(), "standalone", interactiveTarget{}); err == nil || !strings.Contains(err.Error(), `remote "standalone" not found`) {
		t.Fatalf("resolveNamedInteractiveTarget(standalone) error = %v", err)
	}
}

func TestResolveNamedInteractiveTargetTreatsExplicitURLAsRemote(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/healthz":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"ok":true}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	target, err := resolveNamedInteractiveTarget(context.Background(), server.URL, interactiveTarget{})
	if err != nil {
		t.Fatalf("resolveNamedInteractiveTarget(url) error = %v", err)
	}
	if target.Kind != interactiveTargetRemote {
		t.Fatalf("target.Kind = %q, want %q", target.Kind, interactiveTargetRemote)
	}
	if !strings.Contains(target.Name, "127.0.0.1:") {
		t.Fatalf("target.Name = %q, want explicit host:port display", target.Name)
	}
	if target.Description != server.URL {
		t.Fatalf("target.Description = %q, want %q", target.Description, server.URL)
	}
}

type ioDiscardBuffer struct{}

func (ioDiscardBuffer) Write(p []byte) (int, error) { return len(p), nil }
