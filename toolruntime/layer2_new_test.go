package toolruntime

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/fulcrus/hopclaw/agent"
)

func TestLayer2GitWriteTools(t *testing.T) {
	t.Parallel()

	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}

	root := t.TempDir()

	// Init git repo.
	cmd := exec.Command("git", "init")
	cmd.Dir = root
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git init error = %v, output = %s", err, string(output))
	}

	// Configure git user for commits.
	exec.Command("git", "-C", root, "config", "user.email", "test@test.com").Run()
	exec.Command("git", "-C", root, "config", "user.name", "Test").Run()

	reg := NewLayer2Registry(Layer2Config{Root: root})
	ctx := context.Background()
	run := &agent.Run{ID: "run-1"}
	sess := &agent.Session{ID: "sess-1"}

	execTool := func(name string, input map[string]any) string {
		t.Helper()
		results, err := reg.ExecuteBatch(ctx, run, sess, []agent.ToolCall{{
			ID: "call-" + name, Name: name, Input: input,
		}})
		if err != nil {
			t.Fatalf("%s error: %v", name, err)
		}
		if len(results) != 1 {
			t.Fatalf("%s: expected 1 result, got %d", name, len(results))
		}
		return results[0].Content
	}

	// --- git.commit: create a file, add all, commit ---
	if err := os.WriteFile(filepath.Join(root, "test.txt"), []byte("hello"), 0o644); err != nil {
		t.Fatalf("WriteFile(test.txt) error = %v", err)
	}

	commitOut := execTool("git.commit", map[string]any{
		"add_all": true,
		"message": "test commit",
	})

	var commitPayload struct {
		RepoAvailable bool   `json:"repo_available"`
		Hash          string `json:"hash"`
		Message       string `json:"message"`
	}
	if err := json.Unmarshal([]byte(commitOut), &commitPayload); err != nil {
		t.Fatalf("json.Unmarshal(git.commit) error = %v\nraw: %s", err, commitOut)
	}
	if !commitPayload.RepoAvailable {
		t.Fatalf("git.commit: repo not available: %s", commitOut)
	}
	if commitPayload.Hash == "" {
		t.Fatalf("git.commit: expected a commit hash, got empty. payload = %s", commitOut)
	}

	// --- git.branch: list branches after commit ---
	branchOut := execTool("git.branch", map[string]any{
		"list": true,
	})

	var branchPayload struct {
		RepoAvailable bool     `json:"repo_available"`
		Branches      []string `json:"branches"`
	}
	if err := json.Unmarshal([]byte(branchOut), &branchPayload); err != nil {
		t.Fatalf("json.Unmarshal(git.branch) error = %v\nraw: %s", err, branchOut)
	}
	if !branchPayload.RepoAvailable {
		t.Fatalf("git.branch: repo not available: %s", branchOut)
	}
	found := false
	for _, b := range branchPayload.Branches {
		if b == "main" || b == "master" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("git.branch: expected 'main' or 'master' in branches, got %v", branchPayload.Branches)
	}

	// --- git.stash: modify tracked file, stash it ---
	if err := os.WriteFile(filepath.Join(root, "test.txt"), []byte("modified"), 0o644); err != nil {
		t.Fatalf("WriteFile(test.txt modified) error = %v", err)
	}

	stashOut := execTool("git.stash", map[string]any{
		"action": "push",
	})

	var stashPayload struct {
		RepoAvailable bool   `json:"repo_available"`
		Action        string `json:"action"`
		Output        string `json:"output"`
	}
	if err := json.Unmarshal([]byte(stashOut), &stashPayload); err != nil {
		t.Fatalf("json.Unmarshal(git.stash) error = %v\nraw: %s", err, stashOut)
	}
	if !stashPayload.RepoAvailable {
		t.Fatalf("git.stash: repo not available: %s", stashOut)
	}
	if stashPayload.Action != "push" {
		t.Fatalf("git.stash: expected action 'push', got %q", stashPayload.Action)
	}

	// --- git.tag: create tag "v1.0" ---
	execTool("git.tag", map[string]any{
		"name": "v1.0",
	})

	// --- git.tag: list tags, verify "v1.0" is present ---
	tagListOut := execTool("git.tag", map[string]any{
		"list": true,
	})

	var tagPayload struct {
		RepoAvailable bool     `json:"repo_available"`
		Tags          []string `json:"tags"`
	}
	if err := json.Unmarshal([]byte(tagListOut), &tagPayload); err != nil {
		t.Fatalf("json.Unmarshal(git.tag list) error = %v\nraw: %s", err, tagListOut)
	}
	if !tagPayload.RepoAvailable {
		t.Fatalf("git.tag list: repo not available: %s", tagListOut)
	}
	tagFound := false
	for _, tag := range tagPayload.Tags {
		if tag == "v1.0" {
			tagFound = true
			break
		}
	}
	if !tagFound {
		t.Fatalf("git.tag list: expected 'v1.0' in tags, got %v", tagPayload.Tags)
	}
}

func TestLayer2PkgTools(t *testing.T) {
	t.Parallel()

	// Skip if no supported package manager is available.
	hasPkgManager := false
	for _, bin := range []string{"brew", "apt-get", "apk", "yum", "dnf", "pacman", "pip3", "pip", "npm", "cargo"} {
		if _, err := exec.LookPath(bin); err == nil {
			hasPkgManager = true
			break
		}
	}
	if !hasPkgManager {
		t.Skip("no supported package manager available")
	}

	root := t.TempDir()
	reg := NewLayer2Registry(Layer2Config{Root: root})
	ctx := context.Background()
	run := &agent.Run{ID: "run-pkg"}
	sess := &agent.Session{ID: "sess-pkg"}

	results, err := reg.ExecuteBatch(ctx, run, sess, []agent.ToolCall{{
		ID:   "call-pkg.list",
		Name: "pkg.list",
	}})
	if err != nil {
		t.Fatalf("pkg.list error: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("pkg.list: expected 1 result, got %d", len(results))
	}

	var payload struct {
		Manager  string   `json:"manager"`
		Action   string   `json:"action"`
		Packages []string `json:"packages"`
		Count    int      `json:"count"`
	}
	if err := json.Unmarshal([]byte(results[0].Content), &payload); err != nil {
		t.Fatalf("json.Unmarshal(pkg.list) error = %v\nraw: %s", err, results[0].Content)
	}
	if payload.Manager == "" {
		t.Fatalf("pkg.list: expected a manager name, got empty")
	}
	if payload.Action != "list" {
		t.Fatalf("pkg.list: expected action 'list', got %q", payload.Action)
	}
}

func TestLayer2ContainerTools(t *testing.T) {
	t.Parallel()

	if _, err := exec.LookPath("docker"); err != nil {
		t.Skip("docker not installed")
	}

	root := t.TempDir()
	reg := NewLayer2Registry(Layer2Config{Root: root})
	ctx := context.Background()
	run := &agent.Run{ID: "run-container"}
	sess := &agent.Session{ID: "sess-container"}

	results, err := reg.ExecuteBatch(ctx, run, sess, []agent.ToolCall{{
		ID:   "call-container.list",
		Name: "container.list",
	}})
	if err != nil {
		t.Fatalf("container.list error: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("container.list: expected 1 result, got %d", len(results))
	}

	var payload struct {
		Containers []any `json:"containers"`
		Count      int   `json:"count"`
		All        bool  `json:"all"`
		ExitCode   int   `json:"exit_code"`
	}
	if err := json.Unmarshal([]byte(results[0].Content), &payload); err != nil {
		t.Fatalf("json.Unmarshal(container.list) error = %v\nraw: %s", err, results[0].Content)
	}
	// Containers should be a non-nil slice (possibly empty).
	if payload.Containers == nil {
		t.Fatalf("container.list: containers field is nil")
	}
}

func TestLayer2CanExcludeServiceBackedTools(t *testing.T) {
	t.Parallel()

	includeServiceTools := false
	reg := NewLayer2Registry(Layer2Config{
		Root:                t.TempDir(),
		IncludeServiceTools: &includeServiceTools,
	})
	if _, ok := reg.ResolveTool(nil, "search.web"); ok {
		t.Fatal("search.web should not be registered when service-backed tools are excluded")
	}
	if _, ok := reg.ResolveTool(nil, "speech.tts"); ok {
		t.Fatal("speech.tts should not be registered when service-backed tools are excluded")
	}
	if _, ok := reg.ResolveTool(nil, "email.send"); ok {
		t.Fatal("email.send should not be registered when service-backed tools are excluded")
	}
	if _, ok := reg.ResolveTool(nil, "calendar.list_events"); ok {
		t.Fatal("calendar.list_events should not be registered when service-backed tools are excluded")
	}
	if _, ok := reg.ResolveTool(nil, "git.status"); !ok {
		t.Fatal("git.status should remain registered when service-backed tools are excluded")
	}
}

func TestLayer2StubTools(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	reg := NewLayer2Registry(Layer2Config{Root: root})
	ctx := context.Background()
	run := &agent.Run{ID: "run-stub"}
	sess := &agent.Session{ID: "sess-stub"}

	execTool := func(name string, input map[string]any) string {
		t.Helper()
		results, err := reg.ExecuteBatch(ctx, run, sess, []agent.ToolCall{{
			ID: "call-" + name, Name: name, Input: input,
		}})
		if err != nil {
			t.Fatalf("%s error: %v", name, err)
		}
		if len(results) != 1 {
			t.Fatalf("%s: expected 1 result, got %d", name, len(results))
		}
		return results[0].Content
	}

	assertNotConfigured := func(name, content string) {
		t.Helper()
		var payload struct {
			Status  string `json:"status"`
			Message string `json:"message"`
		}
		if err := json.Unmarshal([]byte(content), &payload); err != nil {
			t.Fatalf("json.Unmarshal(%s) error = %v\nraw: %s", name, err, content)
		}
		if payload.Status != "not_configured" {
			t.Fatalf("%s: expected status 'not_configured', got %q", name, payload.Status)
		}
		if payload.Message == "" {
			t.Fatalf("%s: expected a non-empty message", name)
		}
		if !strings.Contains(payload.Message, name) {
			t.Fatalf("%s: expected message to contain tool name, got %q", name, payload.Message)
		}
	}

	// search.web (no config → not_configured)
	searchOut := execTool("search.web", map[string]any{
		"query": "test query",
	})
	assertNotConfigured("search.web", searchOut)

	// speech.tts (no config → not_configured)
	speechOut := execTool("speech.tts", map[string]any{
		"text":   "hello world",
		"output": "out.wav",
	})
	assertNotConfigured("speech.tts", speechOut)

	// email.send (no config → not_configured)
	emailOut := execTool("email.send", map[string]any{
		"to":      "test@example.com",
		"subject": "Test",
		"body":    "Test body",
	})
	assertNotConfigured("email.send", emailOut)
	// session/memory tools are now L1 builtins — tested in builtin_new_test.go.
}

func TestLayer2ToolCount(t *testing.T) {
	t.Parallel()

	reg := NewLayer2Registry(Layer2Config{Root: t.TempDir()})
	statuses := reg.GroupStatuses()

	// We expect 10 groups in the current release.
	if len(statuses) != 10 {
		names := make([]string, 0, len(statuses))
		for _, s := range statuses {
			names = append(names, s.Name)
		}
		t.Fatalf("expected 10 groups, got %d: %v", len(statuses), names)
	}

	totalTools := 0
	for _, s := range statuses {
		totalTools += s.Tools
	}

	// Total across all groups:
	//   git(6) + git-write(7) + pkg(5) + container(6) +
	//   media(9) + media-go(5) + search(3) + speech(2) + email(5) + calendar(4) = 52
	if totalTools != 52 {
		t.Fatalf("expected 52 total tools, got %d", totalTools)
	}

	// Verify per-group counts.
	expectedCounts := map[string]int{
		"git":       6,
		"git-write": 7,
		"pkg":       5,
		"container": 6,
		"media":     9,
		"media-go":  5,
		"search":    3,
		"speech":    2,
		"email":     5,
		"calendar":  4,
	}
	for _, s := range statuses {
		expected, ok := expectedCounts[s.Name]
		if !ok {
			t.Fatalf("unexpected group %q in statuses", s.Name)
		}
		if s.Tools != expected {
			t.Fatalf("group %q: expected %d tools, got %d (tools: %v)", s.Name, expected, s.Tools, s.ToolNames)
		}
	}
}
