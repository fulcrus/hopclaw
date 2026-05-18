package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/fulcrus/hopclaw/contextengine"
)

func TestRunSessionsExportWithClientText(t *testing.T) {

	client := newOperatorStatusTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/runtime/sessions/sess-1/messages" {
			t.Fatalf("unexpected path %q", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode([]contextengine.Message{
			{
				Role:      contextengine.RoleUser,
				Content:   "Hello, can you help me?",
				CreatedAt: time.Date(2026, 4, 2, 10, 30, 0, 0, time.UTC),
			},
			{
				Role:      contextengine.RoleAssistant,
				Content:   "Of course! What do you need?",
				CreatedAt: time.Date(2026, 4, 2, 10, 30, 5, 0, time.UTC),
			},
		})
	}, "")

	var out bytes.Buffer
	if err := runSessionsExportWithClient(context.Background(), client, "sess-1", "", "text", &out); err != nil {
		t.Fatalf("runSessionsExportWithClient() error = %v", err)
	}

	got := out.String()
	for _, want := range []string{
		"[2026-04-02 10:30:00] user:",
		"Hello, can you help me?",
		"[2026-04-02 10:30:05] assistant:",
		"Of course! What do you need?",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("output missing %q: %q", want, got)
		}
	}
}

func TestRunSessionsExportWithClientJSONToFile(t *testing.T) {

	client := newOperatorStatusTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/runtime/sessions/sess-json/messages" {
			t.Fatalf("unexpected path %q", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[{"role":"assistant","content":"done"}]`))
	}, "")

	outputPath := t.TempDir() + "/session.json"
	if err := runSessionsExportWithClient(context.Background(), client, "sess-json", outputPath, "json", nil); err != nil {
		t.Fatalf("runSessionsExportWithClient() error = %v", err)
	}

	data, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if strings.TrimSpace(string(data)) != `[{"role":"assistant","content":"done"}]` {
		t.Fatalf("file content = %q", string(data))
	}
}

func TestRunSessionsPruneWithClientDryRun(t *testing.T) {

	var deleteCalls int
	client := newOperatorStatusTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/runtime/sessions":
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(sessionsListResponse{
				Items: []sessionSummary{
					{
						ID:        "sess-old",
						Key:       "alpha",
						UpdatedAt: time.Date(2026, 2, 1, 10, 0, 0, 0, time.UTC),
					},
					{
						ID:        "sess-new",
						Key:       "beta",
						UpdatedAt: time.Date(2026, 4, 1, 12, 0, 0, 0, time.UTC),
					},
				},
				Count: 2,
			})
		case r.Method == http.MethodDelete:
			deleteCalls++
			t.Fatalf("unexpected delete %s", r.URL.Path)
		default:
			t.Fatalf("unexpected %s %s", r.Method, r.URL.Path)
		}
	}, "")

	var out bytes.Buffer
	now := time.Date(2026, 4, 2, 12, 0, 0, 0, time.UTC)
	if err := runSessionsPruneWithClient(context.Background(), client, "30d", true, &out, now); err != nil {
		t.Fatalf("runSessionsPruneWithClient() error = %v", err)
	}

	if deleteCalls != 0 {
		t.Fatalf("deleteCalls = %d, want 0", deleteCalls)
	}

	output := out.String()
	if !strings.Contains(output, "sess-old") {
		t.Fatalf("expected old session in output: %q", output)
	}
	if strings.Contains(output, "sess-new") {
		t.Fatalf("did not expect recent session in output: %q", output)
	}
	if !strings.Contains(output, "Would prune 1 conversations") {
		t.Fatalf("expected summary in output: %q", output)
	}
}

func TestRunSessionsPruneWithClientDeletesMatchingSessions(t *testing.T) {

	var deleted []string
	client := newOperatorStatusTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/runtime/sessions":
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(sessionsListResponse{
				Items: []sessionSummary{
					{
						ID:        "sess-a",
						Key:       "alpha",
						UpdatedAt: time.Date(2026, 1, 31, 10, 0, 0, 0, time.UTC),
					},
					{
						ID:        "sess-b",
						Key:       "beta",
						UpdatedAt: time.Date(2026, 4, 1, 12, 0, 0, 0, time.UTC),
					},
					{
						ID:        "sess-c",
						Key:       "gamma",
						UpdatedAt: time.Date(2026, 1, 15, 8, 0, 0, 0, time.UTC),
					},
				},
				Count: 3,
			})
		case r.Method == http.MethodDelete:
			deleted = append(deleted, strings.TrimPrefix(r.URL.Path, "/runtime/sessions/"))
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
		default:
			t.Fatalf("unexpected %s %s", r.Method, r.URL.Path)
		}
	}, "")

	var out bytes.Buffer
	now := time.Date(2026, 4, 2, 12, 0, 0, 0, time.UTC)
	if err := runSessionsPruneWithClient(context.Background(), client, "30d", false, &out, now); err != nil {
		t.Fatalf("runSessionsPruneWithClient() error = %v", err)
	}

	slices.Sort(deleted)
	want := []string{"sess-a", "sess-c"}
	if !slices.Equal(deleted, want) {
		t.Fatalf("deleted = %v, want %v", deleted, want)
	}
	if strings.TrimSpace(out.String()) != "Pruned 2 conversations" {
		t.Fatalf("output = %q", out.String())
	}
}
