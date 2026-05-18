package toolruntime

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/fulcrus/hopclaw/agent"
)

func newTestLayer2(root string) *Layer2Registry {
	return NewLayer2Registry(Layer2Config{Root: root})
}

func TestLayer2GitStatus(t *testing.T) {
	t.Parallel()

	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}

	root := t.TempDir()
	cmd := exec.Command("git", "init")
	cmd.Dir = root
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git init error = %v, output = %s", err, string(output))
	}
	if err := os.WriteFile(filepath.Join(root, "tracked.txt"), []byte("pending"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	reg := newTestLayer2(root)
	results, err := reg.ExecuteBatch(context.Background(), &agent.Run{ID: "run-1"}, &agent.Session{ID: "sess-1"}, []agent.ToolCall{{
		ID:   "call-git",
		Name: "git.status",
	}})
	if err != nil {
		t.Fatalf("ExecuteBatch(git.status) error = %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("len(results) = %d", len(results))
	}
	var payload struct {
		RepoAvailable bool `json:"repo_available"`
		IsClean       bool `json:"is_clean"`
		Entries       []struct {
			Path      string `json:"path"`
			Untracked bool   `json:"untracked"`
		} `json:"entries"`
	}
	if err := json.Unmarshal([]byte(results[0].Content), &payload); err != nil {
		t.Fatalf("json.Unmarshal(git.status) error = %v", err)
	}
	if !payload.RepoAvailable || payload.IsClean || len(payload.Entries) != 1 || payload.Entries[0].Path != "tracked.txt" || !payload.Entries[0].Untracked {
		t.Fatalf("git.status payload = %#v", payload)
	}
}

func TestLayer2GitStatusGracefullyHandlesPlainWorkspace(t *testing.T) {
	t.Parallel()

	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}

	root := t.TempDir()
	reg := newTestLayer2(root)
	results, err := reg.ExecuteBatch(context.Background(), &agent.Run{ID: "run-plain"}, &agent.Session{ID: "sess-plain"}, []agent.ToolCall{{
		ID:   "call-git-plain",
		Name: "git.status",
	}})
	if err != nil {
		t.Fatalf("ExecuteBatch(git.status plain) error = %v", err)
	}
	var payload struct {
		RepoAvailable bool   `json:"repo_available"`
		Status        string `json:"status"`
	}
	if err := json.Unmarshal([]byte(results[0].Content), &payload); err != nil {
		t.Fatalf("json.Unmarshal(git.status plain) error = %v", err)
	}
	if payload.RepoAvailable || payload.Status != "not_a_git_repository" {
		t.Fatalf("git.status plain payload = %#v", payload)
	}
}

func TestLayer2GitDiff(t *testing.T) {
	t.Parallel()

	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}

	root := t.TempDir()
	runGitCmd := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = root
		if output, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v error = %v, output = %s", args, err, string(output))
		}
	}

	runGitCmd("init")
	runGitCmd("config", "user.email", "openclaw@example.com")
	runGitCmd("config", "user.name", "OpenClaw")
	if err := os.WriteFile(filepath.Join(root, "tracked.txt"), []byte("before\n"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	runGitCmd("add", "tracked.txt")
	runGitCmd("commit", "-m", "initial")
	if err := os.WriteFile(filepath.Join(root, "tracked.txt"), []byte("after\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(updated) error = %v", err)
	}

	reg := newTestLayer2(root)
	results, err := reg.ExecuteBatch(context.Background(), &agent.Run{ID: "run-1"}, &agent.Session{ID: "sess-1"}, []agent.ToolCall{{
		ID:   "call-diff",
		Name: "git.diff",
		Input: map[string]any{
			"pathspecs": []any{"tracked.txt"},
		},
	}})
	if err != nil {
		t.Fatalf("ExecuteBatch(git.diff) error = %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("len(results) = %d", len(results))
	}
	var payload struct {
		RepoAvailable bool     `json:"repo_available"`
		IsClean       bool     `json:"is_clean"`
		ChangedFiles  []string `json:"changed_files"`
		Diff          string   `json:"diff"`
	}
	if err := json.Unmarshal([]byte(results[0].Content), &payload); err != nil {
		t.Fatalf("json.Unmarshal(git.diff) error = %v", err)
	}
	if !payload.RepoAvailable || payload.IsClean || len(payload.ChangedFiles) != 1 || payload.ChangedFiles[0] != "tracked.txt" {
		t.Fatalf("git.diff payload = %#v", payload)
	}
	if !strings.Contains(payload.Diff, "-before") || !strings.Contains(payload.Diff, "+after") {
		t.Fatalf("git.diff payload diff = %q", payload.Diff)
	}
}

func TestLayer2GitLogAndShow(t *testing.T) {
	t.Parallel()

	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}

	root := t.TempDir()
	runGitCmd := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = root
		if output, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v error = %v, output = %s", args, err, string(output))
		}
	}

	runGitCmd("init")
	runGitCmd("config", "user.email", "openclaw@example.com")
	runGitCmd("config", "user.name", "OpenClaw")
	if err := os.WriteFile(filepath.Join(root, "tracked.txt"), []byte("v1\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(v1) error = %v", err)
	}
	runGitCmd("add", "tracked.txt")
	runGitCmd("commit", "-m", "initial")
	if err := os.WriteFile(filepath.Join(root, "tracked.txt"), []byte("v2\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(v2) error = %v", err)
	}
	runGitCmd("commit", "-am", "second")

	reg := newTestLayer2(root)
	logResults, err := reg.ExecuteBatch(context.Background(), &agent.Run{ID: "run-1"}, &agent.Session{ID: "sess-1"}, []agent.ToolCall{{
		ID:   "call-log",
		Name: "git.log",
		Input: map[string]any{
			"limit": 1,
		},
	}})
	if err != nil {
		t.Fatalf("ExecuteBatch(git.log) error = %v", err)
	}
	var logPayload struct {
		RepoAvailable bool `json:"repo_available"`
		Count         int  `json:"count"`
		Limit         int  `json:"limit"`
		Commits       []struct {
			Hash    string `json:"hash"`
			Subject string `json:"subject"`
		} `json:"commits"`
	}
	if err := json.Unmarshal([]byte(logResults[0].Content), &logPayload); err != nil {
		t.Fatalf("json.Unmarshal(git.log) error = %v", err)
	}
	if !logPayload.RepoAvailable || logPayload.Limit != 1 || logPayload.Count != 1 || len(logPayload.Commits) != 1 || logPayload.Commits[0].Subject != "second" || logPayload.Commits[0].Hash == "" {
		t.Fatalf("git.log payload = %#v", logPayload)
	}

	showResults, err := reg.ExecuteBatch(context.Background(), &agent.Run{ID: "run-1"}, &agent.Session{ID: "sess-1"}, []agent.ToolCall{{
		ID:   "call-show",
		Name: "git.show",
		Input: map[string]any{
			"rev":  "HEAD",
			"stat": true,
		},
	}})
	if err != nil {
		t.Fatalf("ExecuteBatch(git.show) error = %v", err)
	}
	if len(showResults) != 1 {
		t.Fatalf("len(results) = %d", len(showResults))
	}
	var showPayload struct {
		RepoAvailable bool   `json:"repo_available"`
		IsEmpty       bool   `json:"is_empty"`
		Content       string `json:"content"`
	}
	if err := json.Unmarshal([]byte(showResults[0].Content), &showPayload); err != nil {
		t.Fatalf("json.Unmarshal(git.show) error = %v", err)
	}
	if !showPayload.RepoAvailable || showPayload.IsEmpty {
		t.Fatalf("git.show payload = %#v", showPayload)
	}
	if !strings.Contains(showPayload.Content, "second") || !strings.Contains(showPayload.Content, "+v2") {
		t.Fatalf("git.show payload content = %q", showPayload.Content)
	}
}

func TestLayer2GitRevParseAndBlame(t *testing.T) {
	t.Parallel()

	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}

	root := t.TempDir()
	runGitCmd := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = root
		if output, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v error = %v, output = %s", args, err, string(output))
		}
	}

	runGitCmd("init")
	runGitCmd("config", "user.email", "openclaw@example.com")
	runGitCmd("config", "user.name", "OpenClaw")
	if err := os.WriteFile(filepath.Join(root, "tracked.txt"), []byte("line1\nline2\n"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	runGitCmd("add", "tracked.txt")
	runGitCmd("commit", "-m", "initial")

	reg := newTestLayer2(root)
	revResults, err := reg.ExecuteBatch(context.Background(), &agent.Run{ID: "run-1"}, &agent.Session{ID: "sess-1"}, []agent.ToolCall{{
		ID:   "call-rev",
		Name: "git.rev_parse",
	}})
	if err != nil {
		t.Fatalf("ExecuteBatch(git.rev_parse) error = %v", err)
	}
	var revPayload struct {
		RepoAvailable    bool   `json:"repo_available"`
		Full             string `json:"full"`
		Short            string `json:"short"`
		IsInsideWorkTree bool   `json:"is_inside_work_tree"`
	}
	if err := json.Unmarshal([]byte(revResults[0].Content), &revPayload); err != nil {
		t.Fatalf("json.Unmarshal(git.rev_parse) error = %v", err)
	}
	if !revPayload.RepoAvailable || revPayload.Full == "" || revPayload.Short == "" || !revPayload.IsInsideWorkTree {
		t.Fatalf("git.rev_parse payload = %#v", revPayload)
	}

	blameResults, err := reg.ExecuteBatch(context.Background(), &agent.Run{ID: "run-1"}, &agent.Session{ID: "sess-1"}, []agent.ToolCall{{
		ID:   "call-blame",
		Name: "git.blame",
		Input: map[string]any{
			"path":       "tracked.txt",
			"start_line": 1,
			"end_line":   2,
		},
	}})
	if err != nil {
		t.Fatalf("ExecuteBatch(git.blame) error = %v", err)
	}
	var blamePayload struct {
		RepoAvailable bool `json:"repo_available"`
		Count         int  `json:"count"`
		StartLine     int  `json:"start_line"`
		EndLine       int  `json:"end_line"`
		Entries       []struct {
			Commit  string `json:"commit"`
			Author  string `json:"author"`
			Content string `json:"content"`
			Line    int    `json:"line"`
		} `json:"entries"`
	}
	if err := json.Unmarshal([]byte(blameResults[0].Content), &blamePayload); err != nil {
		t.Fatalf("json.Unmarshal(git.blame) error = %v", err)
	}
	if !blamePayload.RepoAvailable || blamePayload.StartLine != 1 || blamePayload.EndLine != 2 || blamePayload.Count != 2 || len(blamePayload.Entries) != 2 || blamePayload.Entries[0].Author != "OpenClaw" || blamePayload.Entries[1].Content != "line2" {
		t.Fatalf("git.blame payload = %#v", blamePayload)
	}
}

func TestLayer2GitRevParseGracefullyHandlesPlainWorkspace(t *testing.T) {
	t.Parallel()

	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}

	root := t.TempDir()
	reg := newTestLayer2(root)
	results, err := reg.ExecuteBatch(context.Background(), &agent.Run{ID: "run-plain-rev"}, &agent.Session{ID: "sess-plain-rev"}, []agent.ToolCall{{
		ID:   "call-rev-plain",
		Name: "git.rev_parse",
	}})
	if err != nil {
		t.Fatalf("ExecuteBatch(git.rev_parse plain) error = %v", err)
	}
	var payload struct {
		RepoAvailable    bool   `json:"repo_available"`
		Status           string `json:"status"`
		IsInsideWorkTree bool   `json:"is_inside_work_tree"`
	}
	if err := json.Unmarshal([]byte(results[0].Content), &payload); err != nil {
		t.Fatalf("json.Unmarshal(git.rev_parse plain) error = %v", err)
	}
	if payload.RepoAvailable || payload.Status != "not_a_git_repository" || payload.IsInsideWorkTree {
		t.Fatalf("git.rev_parse plain payload = %#v", payload)
	}
}

func TestLayer2GitBlameAutodiscoversNestedRepo(t *testing.T) {
	t.Parallel()

	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}

	workspace := t.TempDir()
	repo := filepath.Join(workspace, "repos", "sample")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatalf("MkdirAll(repo) error = %v", err)
	}
	runGitCmd := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = repo
		if output, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v error = %v, output = %s", args, err, string(output))
		}
	}
	runGitCmd("init")
	runGitCmd("config", "user.email", "openclaw@example.com")
	runGitCmd("config", "user.name", "OpenClaw")
	if err := os.WriteFile(filepath.Join(repo, "tracked.txt"), []byte("nested\nrepo\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(tracked.txt) error = %v", err)
	}
	runGitCmd("add", "tracked.txt")
	runGitCmd("commit", "-m", "initial")

	reg := newTestLayer2(workspace)
	results, err := reg.ExecuteBatch(context.Background(), &agent.Run{ID: "run-nested"}, &agent.Session{ID: "sess-nested"}, []agent.ToolCall{{
		ID:   "call-blame-nested",
		Name: "git.blame",
		Input: map[string]any{
			"path":       "repos/sample/tracked.txt",
			"start_line": 1,
			"end_line":   2,
		},
	}})
	if err != nil {
		t.Fatalf("ExecuteBatch(git.blame nested) error = %v", err)
	}
	var payload struct {
		RepoAvailable bool `json:"repo_available"`
		Count         int  `json:"count"`
		Entries       []struct {
			Author string `json:"author"`
			Line   int    `json:"line"`
		} `json:"entries"`
	}
	if err := json.Unmarshal([]byte(results[0].Content), &payload); err != nil {
		t.Fatalf("json.Unmarshal(git.blame nested) error = %v", err)
	}
	if !payload.RepoAvailable || payload.Count != 2 || len(payload.Entries) != 2 || payload.Entries[0].Author != "OpenClaw" {
		t.Fatalf("git.blame nested payload = %#v", payload)
	}
}

func TestLayer2ProbeAndGroupStatus(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	reg := newTestLayer2(root)

	statuses := reg.GroupStatuses()
	if len(statuses) < 9 {
		t.Fatalf("len(GroupStatuses) = %d, want >= 9", len(statuses))
	}

	// Find the git read group.
	var gitGroup *GroupStatus
	for i := range statuses {
		if statuses[i].Name == "git" {
			gitGroup = &statuses[i]
			break
		}
	}
	if gitGroup == nil {
		t.Fatal("git group not found in statuses")
	}
	if gitGroup.Tools != 6 {
		t.Fatalf("git group tools = %d, want 6", gitGroup.Tools)
	}

	if _, err := exec.LookPath("git"); err != nil {
		if gitGroup.Active {
			t.Fatal("git group should be inactive when git is not installed")
		}
		return
	}

	if !gitGroup.Active {
		t.Fatal("git group should be active when git is installed")
	}

	defs := reg.ToolDefinitions(nil)
	// At least git (6) + git-write (7) + groups with no required bins
	if len(defs) < 13 {
		t.Fatalf("len(ToolDefinitions) = %d, want >= 13", len(defs))
	}
}

func TestLayer2ExposeOutputSchemas(t *testing.T) {
	t.Parallel()

	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}

	reg := newTestLayer2(t.TempDir())
	defs := reg.ToolDefinitions(nil)
	for _, d := range defs {
		bound, ok := reg.ResolveTool(nil, d.Name)
		if !ok {
			t.Fatalf("ResolveTool(%q) failed", d.Name)
		}
		if len(bound.Manifest.InputSchema) == 0 {
			t.Fatalf("tool %q missing input schema", d.Name)
		}
		if len(bound.Manifest.OutputSchema) == 0 {
			t.Fatalf("tool %q missing output schema", d.Name)
		}
	}

	statusTool, ok := reg.ResolveTool(nil, "git.status")
	if !ok {
		t.Fatal("ResolveTool(git.status) failed")
	}
	properties, _ := statusTool.Manifest.OutputSchema["properties"].(map[string]any)
	if _, ok := properties["repo_available"]; !ok {
		t.Fatalf("git.status output schema = %#v", statusTool.Manifest.OutputSchema)
	}
	if _, ok := properties["entries"]; !ok {
		t.Fatalf("git.status output schema = %#v", statusTool.Manifest.OutputSchema)
	}
}

func TestLayer2StatusesAndDefinitionsAreSorted(t *testing.T) {
	t.Parallel()

	reg := newTestLayer2(t.TempDir())
	statuses := reg.GroupStatuses()
	statusNames := make([]string, 0, len(statuses))
	for _, status := range statuses {
		statusNames = append(statusNames, status.Name)
		if !sort.StringsAreSorted(status.ToolNames) {
			t.Fatalf("tool names for group %q are not sorted: %v", status.Name, status.ToolNames)
		}
		if !sort.StringsAreSorted(status.Required) {
			t.Fatalf("required bins for group %q are not sorted: %v", status.Name, status.Required)
		}
	}
	if !sort.StringsAreSorted(statusNames) {
		t.Fatalf("group statuses are not sorted: %v", statusNames)
	}

	defs := reg.ToolDefinitions(nil)
	defNames := make([]string, 0, len(defs))
	for _, def := range defs {
		defNames = append(defNames, def.Name)
	}
	if !sort.StringsAreSorted(defNames) {
		t.Fatalf("tool definitions are not sorted: %v", defNames)
	}
}
