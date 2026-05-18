// Package e2e runs end-to-end integration tests against a real HopClaw server
// backed by DeepSeek. Each test submits a task via the HTTP API, waits for the
// run to finish, and asserts on the result.
//
// Run:
//
//	DEEPSEEK_API_KEY=sk-... go test ./e2e/ -v -timeout 60m -count=1
//
// Or source the env file first:
//
//	set -a; source .env.test; set +a
//	go test ./e2e/ -v -timeout 60m -count=1
package e2e

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// Shared test server — started once, reused across all tests
// ---------------------------------------------------------------------------

var (
	setupOnce  sync.Once
	serverAddr string
	serverProc *os.Process
	setupErr   error
	workspace  string
)

func setup(t *testing.T) string {
	t.Helper()
	key := os.Getenv("DEEPSEEK_API_KEY")
	if key == "" {
		t.Skip("DEEPSEEK_API_KEY not set; skipping e2e tests")
	}
	setupOnce.Do(func() {
		workspace = filepath.Join(os.TempDir(), fmt.Sprintf("hopclaw-e2e-%d", os.Getpid()))
		os.MkdirAll(workspace, 0o755)
		os.MkdirAll(filepath.Join(workspace, "artifacts"), 0o755)
		os.MkdirAll(filepath.Join(workspace, "skills"), 0o755)

		repoRoot := findRepoRoot()

		// Find free port.
		ln, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			setupErr = err
			return
		}
		addr := ln.Addr().String()
		ln.Close()

		// Write config.
		cfg := fmt.Sprintf(`
server:
  address: %s
store:
  backend: memory
agent:
  system_prompt: |
    You are HopClaw, a versatile AI assistant. Complete tasks end-to-end.
    Produce concrete, verifiable output. Use the tools available to you.
    Work directory: %s
    When writing files, write them under the work directory.
    When running commands, set the working directory to the work directory.
    Always verify your work before reporting success.
  default_model: deepseek-chat
  max_tool_rounds: 12
  max_tool_recovery_attempts: 3
  queue_mode: enqueue
  dedupe_window: 2s
runtime:
  profile: trusted_desktop
  status_reminder_delay: 2s
  artifacts:
    enabled: true
    path: %s/artifacts
    inline_threshold: 8192
    retention: 1h
skills:
  include_catalog: true
  auto_detect: false
  install_policy: deny
  dirs:
    - %s/skills
models:
  openai_compat:
    base_url: https://api.deepseek.com
    api_key: %s
    model: deepseek-chat
    timeout: 120s
tools:
  builtins:
    enabled: true
    root: %s
    default_exec_timeout: 30s
    max_read_bytes: 262144
  local_exec:
    enabled: true
    default_timeout: 30s
  capabilities:
    exec:
      mode: allow
      timeout: 30s
      max_output: 1048576
    net:
      allow_private: false
      allow_local: true
      max_download: 10485760
    fs:
      skip_dirs: [.git, node_modules]
    layer2:
      git: true
      media: false
hosts:
  desktop:
    base_url: http://127.0.0.1:9224
`, addr, workspace, workspace, workspace, key, workspace)

		cfgPath := filepath.Join(workspace, "config.yaml")
		os.WriteFile(cfgPath, []byte(cfg), 0o600)

		// Start server via go run.
		srv := exec.Command("go", "run", "./cmd/hopclaw", "serve", "--config", cfgPath)
		srv.Dir = repoRoot
		srv.Stdout = os.Stdout
		srv.Stderr = os.Stderr
		if err := srv.Start(); err != nil {
			setupErr = fmt.Errorf("start: %w", err)
			return
		}
		serverProc = srv.Process

		// Wait for health.
		deadline := time.Now().Add(20 * time.Second)
		for time.Now().Before(deadline) {
			resp, err := http.Get("http://" + addr + "/healthz")
			if err == nil {
				resp.Body.Close()
				if resp.StatusCode == 200 {
					serverAddr = addr
					return
				}
			}
			time.Sleep(300 * time.Millisecond)
		}
		srv.Process.Kill()
		setupErr = fmt.Errorf("server at %s did not become ready", addr)
	})
	if setupErr != nil {
		t.Fatalf("setup: %v", setupErr)
	}
	return serverAddr
}

func findRepoRoot() string {
	dir, _ := os.Getwd()
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "."
		}
		dir = parent
	}
}

// ---------------------------------------------------------------------------
// HTTP helpers
// ---------------------------------------------------------------------------

type submitReq struct {
	SessionKey string         `json:"session_key"`
	Content    string         `json:"content"`
	Metadata   map[string]any `json:"metadata,omitempty"`
}

type runInfo struct {
	ID            string `json:"id"`
	SessionID     string `json:"session_id"`
	Status        string `json:"status"`
	Phase         string `json:"phase"`
	ExecutionMode string `json:"execution_mode"`
	Error         string `json:"error"`
	ToolRounds    int    `json:"tool_rounds"`
}

type runResult struct {
	RunID   string `json:"run_id"`
	Output  string `json:"output"`
	Summary string `json:"summary"`
	Outcome string `json:"outcome"`
}

func submit(t *testing.T, addr, session, content string) *runInfo {
	t.Helper()
	body, _ := json.Marshal(submitReq{SessionKey: session, Content: content})
	resp, err := http.Post("http://"+addr+"/runtime/runs", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("submit: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 202 && resp.StatusCode != 201 {
		data, _ := io.ReadAll(resp.Body)
		t.Fatalf("submit status=%d body=%s", resp.StatusCode, data)
	}
	var r runInfo
	json.NewDecoder(resp.Body).Decode(&r)
	return &r
}

func wait(t *testing.T, addr, runID string, timeout time.Duration) *runInfo {
	t.Helper()
	deadline := time.Now().Add(timeout)
	lastStatus := ""
	for time.Now().Before(deadline) {
		resp, err := http.Get("http://" + addr + "/runtime/runs/" + runID)
		if err != nil {
			time.Sleep(1 * time.Second)
			continue
		}
		var r runInfo
		json.NewDecoder(resp.Body).Decode(&r)
		resp.Body.Close()
		if r.Status == "completed" || r.Status == "failed" || r.Status == "cancelled" {
			t.Logf("run %s finished: status=%s phase=%s tool_rounds=%d", runID, r.Status, r.Phase, r.ToolRounds)
			return &r
		}
		status := fmt.Sprintf("%s/%s", r.Status, r.Phase)
		if status != lastStatus {
			t.Logf("run %s: %s (tool_rounds=%d)", runID, status, r.ToolRounds)
			lastStatus = status
		}
		time.Sleep(2 * time.Second)
	}
	t.Fatalf("run %s did not finish within %v (last=%s)", runID, timeout, lastStatus)
	return nil
}

func result(t *testing.T, addr, runID string) *runResult {
	t.Helper()
	resp, err := http.Get("http://" + addr + "/runtime/runs/" + runID + "/result")
	if err != nil {
		return nil
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil
	}
	var r runResult
	json.NewDecoder(resp.Body).Decode(&r)
	return &r
}

func submitWait(t *testing.T, addr, session, content string, timeout time.Duration) (*runInfo, *runResult) {
	t.Helper()
	run := submit(t, addr, session, content)
	final := wait(t, addr, run.ID, timeout)
	res := result(t, addr, run.ID)
	return final, res
}

func health(t *testing.T, addr string) bool {
	t.Helper()
	resp, err := http.Get("http://" + addr + "/healthz")
	if err != nil {
		return false
	}
	resp.Body.Close()
	return resp.StatusCode == 200
}

// ---------------------------------------------------------------------------
// Assertions
// ---------------------------------------------------------------------------

const defaultTimeout = 180 * time.Second

func assertCompleted(t *testing.T, r *runInfo) {
	t.Helper()
	if r.Status != "completed" {
		t.Errorf("status=%s error=%q, want completed", r.Status, r.Error)
	}
}

func assertOutputContains(t *testing.T, res *runResult, substr string) {
	t.Helper()
	if res == nil {
		t.Error("result is nil")
		return
	}
	combined := strings.ToLower(res.Output + " " + res.Summary)
	if !strings.Contains(combined, strings.ToLower(substr)) {
		t.Errorf("output does not contain %q\noutput: %s", substr, trunc(res.Output, 500))
	}
}

func assertFileExists(t *testing.T, rel string) {
	t.Helper()
	p := filepath.Join(workspace, rel)
	if _, err := os.Stat(p); os.IsNotExist(err) {
		t.Errorf("expected file %s", p)
	}
}

func assertFileContains(t *testing.T, rel, substr string) {
	t.Helper()
	p := filepath.Join(workspace, rel)
	data, err := os.ReadFile(p)
	if err != nil {
		t.Errorf("read %s: %v", p, err)
		return
	}
	if !strings.Contains(strings.ToLower(string(data)), strings.ToLower(substr)) {
		t.Errorf("file %s does not contain %q", rel, substr)
	}
}

func trunc(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}

// ---------------------------------------------------------------------------
// Cleanup
// ---------------------------------------------------------------------------

func TestMain(m *testing.M) {
	code := m.Run()
	if serverProc != nil {
		serverProc.Kill()
	}
	// Leave workspace for inspection on failure.
	if code == 0 && workspace != "" {
		os.RemoveAll(workspace)
	}
	os.Exit(code)
}

// ===================================================================
// TEST CASES — organized by category
// ===================================================================

// ---------------------------------------------------------------------------
// Category 1: Server & API infrastructure (10 tests)
// ---------------------------------------------------------------------------

func TestInfra_Health(t *testing.T) {
	addr := setup(t)
	if !health(t, addr) {
		t.Fatal("health check failed")
	}
}

func TestInfra_ListTools(t *testing.T) {
	addr := setup(t)
	resp, err := http.Get("http://" + addr + "/runtime/tools")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var wrapper struct {
		Items []map[string]any `json:"items"`
	}
	json.NewDecoder(resp.Body).Decode(&wrapper)
	if len(wrapper.Items) < 20 {
		t.Fatalf("expected 20+ tools, got %d", len(wrapper.Items))
	}
}

func TestInfra_ListRunsEmpty(t *testing.T) {
	addr := setup(t)
	resp, err := http.Get("http://" + addr + "/runtime/runs")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("status=%d", resp.StatusCode)
	}
}

func TestInfra_ListSessionsEmpty(t *testing.T) {
	addr := setup(t)
	resp, err := http.Get("http://" + addr + "/runtime/sessions")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("status=%d", resp.StatusCode)
	}
}

func TestInfra_SubmitEmptyContent(t *testing.T) {
	addr := setup(t)
	body, _ := json.Marshal(submitReq{SessionKey: "empty-test", Content: ""})
	resp, err := http.Post("http://"+addr+"/runtime/runs", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 400 {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
}

func TestInfra_GetNonexistentRun(t *testing.T) {
	addr := setup(t)
	resp, err := http.Get("http://" + addr + "/runtime/runs/nonexistent")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode == 200 {
		t.Fatal("expected error for nonexistent run")
	}
}

func TestInfra_ListApprovals(t *testing.T) {
	addr := setup(t)
	resp, err := http.Get("http://" + addr + "/runtime/approvals")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("status=%d", resp.StatusCode)
	}
}

func TestInfra_EventStream(t *testing.T) {
	addr := setup(t)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, "GET", "http://"+addr+"/runtime/events/stream", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil && !strings.Contains(err.Error(), "context deadline") {
		t.Fatal(err)
	}
	if resp != nil {
		resp.Body.Close()
		if resp.StatusCode != 200 {
			t.Fatalf("status=%d", resp.StatusCode)
		}
	}
}

func TestInfra_ListArtifacts(t *testing.T) {
	addr := setup(t)
	resp, err := http.Get("http://" + addr + "/runtime/artifacts")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("status=%d", resp.StatusCode)
	}
}

func TestInfra_CancelNonexistent(t *testing.T) {
	addr := setup(t)
	resp, err := http.Post("http://"+addr+"/runtime/runs/nonexistent/cancel", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode == 200 {
		t.Fatal("expected error")
	}
}

// ---------------------------------------------------------------------------
// Category 2: Pure knowledge / chat (10 tests)
// ---------------------------------------------------------------------------

func TestKnowledge_SimpleQuestion(t *testing.T) {
	addr := setup(t)
	r, res := submitWait(t, addr, "know-1", "What is the capital of France? Answer in one word.", defaultTimeout)
	assertCompleted(t, r)
	assertOutputContains(t, res, "Paris")
}

func TestKnowledge_MathCalculation(t *testing.T) {
	addr := setup(t)
	r, res := submitWait(t, addr, "know-2", "What is 17 * 23? Just give the number.", defaultTimeout)
	assertCompleted(t, r)
	assertOutputContains(t, res, "391")
}

func TestKnowledge_CodeExplanation(t *testing.T) {
	addr := setup(t)
	r, _ := submitWait(t, addr, "know-3", "Explain what a goroutine is in Go in 2 sentences.", defaultTimeout)
	assertCompleted(t, r)
}

func TestKnowledge_Translation(t *testing.T) {
	addr := setup(t)
	r, res := submitWait(t, addr, "know-4", "Translate 'Hello World' to Japanese. Give only the translation.", defaultTimeout)
	assertCompleted(t, r)
	if res != nil && res.Output == "" {
		t.Error("expected non-empty output")
	}
}

func TestKnowledge_ListGeneration(t *testing.T) {
	addr := setup(t)
	r, res := submitWait(t, addr, "know-5", "List 5 programming languages that start with the letter P.", defaultTimeout)
	assertCompleted(t, r)
	assertOutputContains(t, res, "Python")
}

func TestKnowledge_ChineseQuestion(t *testing.T) {
	addr := setup(t)
	r, res := submitWait(t, addr, "know-6", "中国的首都是哪里？用一个词回答。", defaultTimeout)
	assertCompleted(t, r)
	assertOutputContains(t, res, "北京")
}

func TestKnowledge_JSONGeneration(t *testing.T) {
	addr := setup(t)
	r, res := submitWait(t, addr, "know-7", "Generate a valid JSON object with keys: name, age, city. Use any values.", defaultTimeout)
	assertCompleted(t, r)
	if res != nil && !strings.Contains(res.Output, "{") {
		t.Error("expected JSON in output")
	}
}

func TestKnowledge_Summarization(t *testing.T) {
	addr := setup(t)
	r, _ := submitWait(t, addr, "know-8", "Summarize the concept of 'microservices architecture' in 3 bullet points.", defaultTimeout)
	assertCompleted(t, r)
}

func TestKnowledge_CompareContrast(t *testing.T) {
	addr := setup(t)
	r, _ := submitWait(t, addr, "know-9", "Compare REST and GraphQL APIs. Give 3 differences.", defaultTimeout)
	assertCompleted(t, r)
}

func TestKnowledge_CreativeWriting(t *testing.T) {
	addr := setup(t)
	r, _ := submitWait(t, addr, "know-10", "Write a haiku about programming.", defaultTimeout)
	assertCompleted(t, r)
}

// ---------------------------------------------------------------------------
// Category 3: File system operations (15 tests)
// ---------------------------------------------------------------------------

func TestFS_WriteAndRead(t *testing.T) {
	addr := setup(t)
	r, _ := submitWait(t, addr, "fs-1", "Create a file called hello.txt with the content 'Hello from HopClaw!' in the work directory.", defaultTimeout)
	assertCompleted(t, r)
	assertFileExists(t, "hello.txt")
	assertFileContains(t, "hello.txt", "Hello from HopClaw")
}

func TestFS_WriteJSON(t *testing.T) {
	addr := setup(t)
	r, _ := submitWait(t, addr, "fs-2", `Create a file called data.json with this content: {"name":"test","version":"1.0","items":[1,2,3]}`, defaultTimeout)
	assertCompleted(t, r)
	assertFileExists(t, "data.json")
	assertFileContains(t, "data.json", `"name"`)
}

func TestFS_WriteCSV(t *testing.T) {
	addr := setup(t)
	r, _ := submitWait(t, addr, "fs-3", "Create a CSV file called people.csv with headers: name,age,city and 3 rows of sample data.", defaultTimeout)
	assertCompleted(t, r)
	assertFileExists(t, "people.csv")
	assertFileContains(t, "people.csv", "name")
}

func TestFS_ListDirectory(t *testing.T) {
	addr := setup(t)
	r, res := submitWait(t, addr, "fs-4", "List all files in the work directory.", defaultTimeout)
	assertCompleted(t, r)
	if res != nil && !strings.Contains(res.Output, "hello") && !strings.Contains(res.Output, ".txt") {
		// If previous tests created files, they should be listed
		t.Log("note: output may not contain expected files if fs-1 hasn't run yet")
	}
}

func TestFS_ReadFile(t *testing.T) {
	addr := setup(t)
	// First create a file, then read it.
	os.WriteFile(filepath.Join(workspace, "readme.md"), []byte("# Test\nThis is a test file."), 0o644)
	r, res := submitWait(t, addr, "fs-5", "Read the file readme.md from the work directory and tell me what it says.", defaultTimeout)
	assertCompleted(t, r)
	assertOutputContains(t, res, "test")
}

func TestFS_CreateDirectory(t *testing.T) {
	addr := setup(t)
	r, _ := submitWait(t, addr, "fs-6", "Create a directory called 'subdir' in the work directory, then create a file 'subdir/nested.txt' with content 'nested file'.", defaultTimeout)
	assertCompleted(t, r)
	assertFileExists(t, "subdir/nested.txt")
}

func TestFS_WriteMarkdown(t *testing.T) {
	addr := setup(t)
	r, _ := submitWait(t, addr, "fs-7", "Write a Markdown file called report.md with a title '# Weekly Report', two sections '## Summary' and '## Action Items', and some placeholder content.", defaultTimeout)
	assertCompleted(t, r)
	assertFileExists(t, "report.md")
	assertFileContains(t, "report.md", "Weekly Report")
}

func TestFS_WriteYAML(t *testing.T) {
	addr := setup(t)
	r, _ := submitWait(t, addr, "fs-8", "Create a YAML config file called app.yaml with keys: app_name, version, database (with sub-keys host, port, name).", defaultTimeout)
	assertCompleted(t, r)
	assertFileExists(t, "app.yaml")
	assertFileContains(t, "app.yaml", "app_name")
}

func TestFS_WriteHTML(t *testing.T) {
	addr := setup(t)
	r, _ := submitWait(t, addr, "fs-9", "Create an HTML file called page.html with a basic page: doctype, head with title 'Test Page', body with an h1 and a paragraph.", defaultTimeout)
	assertCompleted(t, r)
	assertFileExists(t, "page.html")
	assertFileContains(t, "page.html", "<html")
}

func TestFS_WritePython(t *testing.T) {
	addr := setup(t)
	r, _ := submitWait(t, addr, "fs-10", "Write a Python script called fib.py that prints the first 10 Fibonacci numbers.", defaultTimeout)
	assertCompleted(t, r)
	assertFileExists(t, "fib.py")
	assertFileContains(t, "fib.py", "def")
}

func TestFS_WriteShellScript(t *testing.T) {
	addr := setup(t)
	r, _ := submitWait(t, addr, "fs-11", "Write a shell script called info.sh that prints the current date, hostname, and working directory.", defaultTimeout)
	assertCompleted(t, r)
	assertFileExists(t, "info.sh")
	assertFileContains(t, "info.sh", "date")
}

func TestFS_WriteMultipleFiles(t *testing.T) {
	addr := setup(t)
	r, _ := submitWait(t, addr, "fs-12", "Create 3 files in the work directory: colors.txt (list 5 colors), animals.txt (list 5 animals), cities.txt (list 5 cities).", defaultTimeout)
	assertCompleted(t, r)
	assertFileExists(t, "colors.txt")
	assertFileExists(t, "animals.txt")
	assertFileExists(t, "cities.txt")
}

func TestFS_WriteLargeFile(t *testing.T) {
	addr := setup(t)
	r, _ := submitWait(t, addr, "fs-13", "Create a file called numbers.txt containing the numbers 1 through 100, one per line.", defaultTimeout)
	assertCompleted(t, r)
	assertFileExists(t, "numbers.txt")
	assertFileContains(t, "numbers.txt", "50")
}

func TestFS_CopyFile(t *testing.T) {
	addr := setup(t)
	os.WriteFile(filepath.Join(workspace, "original.txt"), []byte("original content"), 0o644)
	r, _ := submitWait(t, addr, "fs-14", "Read the file original.txt and create a copy called copy.txt with the same content.", defaultTimeout)
	assertCompleted(t, r)
	assertFileExists(t, "copy.txt")
	assertFileContains(t, "copy.txt", "original content")
}

func TestFS_ModifyFile(t *testing.T) {
	addr := setup(t)
	os.WriteFile(filepath.Join(workspace, "modify_me.txt"), []byte("line1\nline2\nline3\n"), 0o644)
	r, _ := submitWait(t, addr, "fs-15", "Read modify_me.txt, add a line 'line4' at the end, and write it back.", defaultTimeout)
	assertCompleted(t, r)
	assertFileContains(t, "modify_me.txt", "line4")
}

// ---------------------------------------------------------------------------
// Category 4: Command execution (15 tests)
// ---------------------------------------------------------------------------

func TestExec_Echo(t *testing.T) {
	addr := setup(t)
	r, res := submitWait(t, addr, "exec-1", "Run the command: echo 'Hello from exec'", defaultTimeout)
	assertCompleted(t, r)
	assertOutputContains(t, res, "Hello from exec")
}

func TestExec_Date(t *testing.T) {
	addr := setup(t)
	r, res := submitWait(t, addr, "exec-2", "Run the 'date' command and tell me what day of the week it is today.", defaultTimeout)
	assertCompleted(t, r)
	if res != nil && res.Output == "" {
		t.Error("expected date output")
	}
}

func TestExec_PWD(t *testing.T) {
	addr := setup(t)
	r, _ := submitWait(t, addr, "exec-3", "Run 'pwd' and tell me the current directory.", defaultTimeout)
	assertCompleted(t, r)
}

func TestExec_Uname(t *testing.T) {
	addr := setup(t)
	r, res := submitWait(t, addr, "exec-4", "Run 'uname -s' and tell me what operating system this is.", defaultTimeout)
	assertCompleted(t, r)
	assertOutputContains(t, res, "Darwin")
}

func TestExec_WC(t *testing.T) {
	addr := setup(t)
	os.WriteFile(filepath.Join(workspace, "count_me.txt"), []byte("one\ntwo\nthree\nfour\nfive\n"), 0o644)
	r, res := submitWait(t, addr, "exec-5", "Count the number of lines in count_me.txt using 'wc -l'.", defaultTimeout)
	assertCompleted(t, r)
	assertOutputContains(t, res, "5")
}

func TestExec_Grep(t *testing.T) {
	addr := setup(t)
	os.WriteFile(filepath.Join(workspace, "search.txt"), []byte("apple\nbanana\napricot\ncherry\navocado\n"), 0o644)
	r, res := submitWait(t, addr, "exec-6", "Use grep to find all lines starting with 'a' in search.txt.", defaultTimeout)
	assertCompleted(t, r)
	assertOutputContains(t, res, "apple")
}

func TestExec_Sort(t *testing.T) {
	addr := setup(t)
	os.WriteFile(filepath.Join(workspace, "unsorted.txt"), []byte("banana\napple\ncherry\n"), 0o644)
	r, res := submitWait(t, addr, "exec-7", "Sort the file unsorted.txt alphabetically and show the result.", defaultTimeout)
	assertCompleted(t, r)
	assertOutputContains(t, res, "apple")
}

func TestExec_PipeCommands(t *testing.T) {
	addr := setup(t)
	r, res := submitWait(t, addr, "exec-8", "Run: echo -e 'c\\na\\nb' | sort", defaultTimeout)
	assertCompleted(t, r)
	if res == nil || res.Output == "" {
		t.Error("expected pipe output")
	}
}

func TestExec_RunPython(t *testing.T) {
	addr := setup(t)
	r, res := submitWait(t, addr, "exec-9", "Run: python3 -c 'print(2**10)'", defaultTimeout)
	assertCompleted(t, r)
	assertOutputContains(t, res, "1024")
}

func TestExec_CreateAndRun(t *testing.T) {
	addr := setup(t)
	r, res := submitWait(t, addr, "exec-10", "Create a Python script calc.py that prints 7*8, then run it and tell me the result.", defaultTimeout)
	assertCompleted(t, r)
	assertOutputContains(t, res, "56")
}

func TestExec_EnvironmentVariable(t *testing.T) {
	addr := setup(t)
	r, res := submitWait(t, addr, "exec-11", "Run: echo $HOME", defaultTimeout)
	assertCompleted(t, r)
	if res != nil && !strings.Contains(res.Output, "/") {
		t.Error("expected home path in output")
	}
}

func TestExec_DiskUsage(t *testing.T) {
	addr := setup(t)
	r, _ := submitWait(t, addr, "exec-12", "Run 'df -h /' and tell me how much disk space is available.", defaultTimeout)
	assertCompleted(t, r)
}

func TestExec_ProcessList(t *testing.T) {
	addr := setup(t)
	r, _ := submitWait(t, addr, "exec-13", "Run 'ps aux | head -5' and tell me what processes are running.", defaultTimeout)
	assertCompleted(t, r)
}

func TestExec_NetworkCheck(t *testing.T) {
	addr := setup(t)
	r, _ := submitWait(t, addr, "exec-14", "Run 'curl -s -o /dev/null -w \"%{http_code}\" https://httpbin.org/get' and tell me the HTTP status code.", defaultTimeout)
	assertCompleted(t, r)
}

func TestExec_GoVersion(t *testing.T) {
	addr := setup(t)
	r, res := submitWait(t, addr, "exec-15", "Run 'go version' and tell me the Go version.", defaultTimeout)
	assertCompleted(t, r)
	assertOutputContains(t, res, "go")
}

// ---------------------------------------------------------------------------
// Category 5: Multi-step tasks / automation (15 tests)
// ---------------------------------------------------------------------------

func TestAuto_CreateProjectStructure(t *testing.T) {
	addr := setup(t)
	r, _ := submitWait(t, addr, "auto-1", "Create a project structure: make directories src/, tests/, docs/ in the work directory. In src/ create main.py with a hello world function. In tests/ create test_main.py with a basic test. In docs/ create README.md.", 180*time.Second)
	assertCompleted(t, r)
	assertFileExists(t, "src/main.py")
	assertFileExists(t, "tests/test_main.py")
	assertFileExists(t, "docs/README.md")
}

func TestAuto_DataPipeline(t *testing.T) {
	addr := setup(t)
	r, _ := submitWait(t, addr, "auto-2", "Create a CSV file called sales.csv with columns: date,product,quantity,price and 10 rows of sample data. Then write a Python script analyze.py that reads the CSV and prints the total revenue (quantity*price summed). Then run the script.", 180*time.Second)
	assertCompleted(t, r)
	assertFileExists(t, "sales.csv")
	assertFileExists(t, "analyze.py")
}

func TestAuto_GenerateAndTest(t *testing.T) {
	addr := setup(t)
	r, res := submitWait(t, addr, "auto-3", "Write a Python function in utils.py that checks if a string is a palindrome. Then write a test file test_utils.py that tests it with 3 cases. Then run the tests with: python3 -m pytest test_utils.py -v (install pytest first if needed with pip3 install pytest).", 180*time.Second)
	assertCompleted(t, r)
	assertFileExists(t, "utils.py")
	if res != nil {
		t.Logf("output: %s", trunc(res.Output, 500))
	}
}

func TestAuto_WebScrapeAndSave(t *testing.T) {
	addr := setup(t)
	r, _ := submitWait(t, addr, "auto-4", "Fetch the content of https://httpbin.org/json using a net tool or curl, then save the response body to a file called httpbin_response.json in the work directory.", defaultTimeout)
	assertCompleted(t, r)
	assertFileExists(t, "httpbin_response.json")
}

func TestAuto_GitInit(t *testing.T) {
	addr := setup(t)
	gitDir := filepath.Join(workspace, "myrepo")
	os.MkdirAll(gitDir, 0o755)
	r, _ := submitWait(t, addr, "auto-5", fmt.Sprintf("Initialize a git repository in %s, create a file README.md with content '# My Project', and make an initial commit.", gitDir), 180*time.Second)
	assertCompleted(t, r)
	assertFileExists(t, "myrepo/.git/HEAD")
}

func TestAuto_TemplateGeneration(t *testing.T) {
	addr := setup(t)
	r, _ := submitWait(t, addr, "auto-6", "Create a Dockerfile for a Python 3.11 application that copies requirements.txt, installs deps, copies app code, and runs 'python main.py'. Save it as Dockerfile in the work directory.", defaultTimeout)
	assertCompleted(t, r)
	assertFileExists(t, "Dockerfile")
	assertFileContains(t, "Dockerfile", "FROM")
}

func TestAuto_ConfigGeneration(t *testing.T) {
	addr := setup(t)
	r, _ := submitWait(t, addr, "auto-7", "Create a docker-compose.yml with 3 services: a postgres database, a redis cache, and a web app that depends on both.", defaultTimeout)
	assertCompleted(t, r)
	assertFileExists(t, "docker-compose.yml")
	assertFileContains(t, "docker-compose.yml", "postgres")
}

func TestAuto_ShellScriptPipeline(t *testing.T) {
	addr := setup(t)
	r, _ := submitWait(t, addr, "auto-8", "Write a shell script called backup.sh that: 1) creates a backup directory with today's date, 2) copies all .txt files from the work directory into it, 3) creates a manifest.txt listing all copied files. Then run it.", 180*time.Second)
	assertCompleted(t, r)
	assertFileExists(t, "backup.sh")
}

func TestAuto_APIClient(t *testing.T) {
	addr := setup(t)
	r, _ := submitWait(t, addr, "auto-9", "Write a Python script called api_client.py that makes a GET request to https://httpbin.org/get with a custom header X-Test: HopClaw, and saves the response to api_response.json. Then run it.", 180*time.Second)
	assertCompleted(t, r)
	assertFileExists(t, "api_client.py")
}

func TestAuto_CodeRefactor(t *testing.T) {
	addr := setup(t)
	os.WriteFile(filepath.Join(workspace, "messy.py"), []byte(`
def f(x,y):
    r=x+y
    return r
def g(a,b,c):
    r=a*b+c
    return r
print(f(1,2))
print(g(3,4,5))
`), 0o644)
	r, _ := submitWait(t, addr, "auto-10", "Read messy.py, refactor it with descriptive function/variable names and add docstrings, then write the result to clean.py.", defaultTimeout)
	assertCompleted(t, r)
	assertFileExists(t, "clean.py")
	assertFileContains(t, "clean.py", "def")
}

func TestAuto_MakefileGeneration(t *testing.T) {
	addr := setup(t)
	r, _ := submitWait(t, addr, "auto-11", "Create a Makefile with targets: build (echo building), test (echo testing), clean (echo cleaning), and all (depends on build and test).", defaultTimeout)
	assertCompleted(t, r)
	assertFileExists(t, "Makefile")
	assertFileContains(t, "Makefile", "build")
}

func TestAuto_SQLGeneration(t *testing.T) {
	addr := setup(t)
	r, _ := submitWait(t, addr, "auto-12", "Create a file schema.sql with SQL to create tables: users (id, name, email, created_at), posts (id, user_id, title, body, created_at), comments (id, post_id, user_id, body, created_at). Include foreign keys.", defaultTimeout)
	assertCompleted(t, r)
	assertFileExists(t, "schema.sql")
	assertFileContains(t, "schema.sql", "CREATE TABLE")
}

func TestAuto_EnvFileGeneration(t *testing.T) {
	addr := setup(t)
	r, _ := submitWait(t, addr, "auto-13", "Create a .env.example file with typical environment variables for a web app: DATABASE_URL, REDIS_URL, SECRET_KEY, API_KEY, PORT, DEBUG.", defaultTimeout)
	assertCompleted(t, r)
	assertFileExists(t, ".env.example")
	assertFileContains(t, ".env.example", "DATABASE_URL")
}

func TestAuto_NginxConfig(t *testing.T) {
	addr := setup(t)
	r, _ := submitWait(t, addr, "auto-14", "Create an nginx.conf that proxies requests from port 80 to a backend on localhost:8080, with proper location blocks.", defaultTimeout)
	assertCompleted(t, r)
	assertFileExists(t, "nginx.conf")
	assertFileContains(t, "nginx.conf", "proxy_pass")
}

func TestAuto_CIConfig(t *testing.T) {
	addr := setup(t)
	r, _ := submitWait(t, addr, "auto-15", "Create a GitHub Actions workflow file .github/workflows/ci.yml that runs Go tests on push to main.", 180*time.Second)
	assertCompleted(t, r)
	assertFileExists(t, ".github/workflows/ci.yml")
	assertFileContains(t, ".github/workflows/ci.yml", "go test")
}

// ---------------------------------------------------------------------------
// Category 6: Network / HTTP (10 tests)
// ---------------------------------------------------------------------------

func TestNet_FetchURL(t *testing.T) {
	addr := setup(t)
	r, res := submitWait(t, addr, "net-1", "Fetch https://httpbin.org/get and show me the response.", defaultTimeout)
	assertCompleted(t, r)
	assertOutputContains(t, res, "httpbin")
}

func TestNet_FetchJSON(t *testing.T) {
	addr := setup(t)
	r, res := submitWait(t, addr, "net-2", "Fetch https://httpbin.org/json and extract the 'slideshow' title from the JSON.", defaultTimeout)
	assertCompleted(t, r)
	assertOutputContains(t, res, "Sample")
}

func TestNet_POSTRequest(t *testing.T) {
	addr := setup(t)
	r, _ := submitWait(t, addr, "net-3", "Send a POST request to https://httpbin.org/post with JSON body {\"test\": \"hopclaw\"} and show the response.", defaultTimeout)
	assertCompleted(t, r)
}

func TestNet_CheckHeaders(t *testing.T) {
	addr := setup(t)
	r, res := submitWait(t, addr, "net-4", "Fetch https://httpbin.org/headers and tell me the User-Agent header value.", defaultTimeout)
	assertCompleted(t, r)
	if res != nil && res.Output == "" {
		t.Error("expected header output")
	}
}

func TestNet_DownloadAndSave(t *testing.T) {
	addr := setup(t)
	r, _ := submitWait(t, addr, "net-5", "Download the content from https://httpbin.org/robots.txt and save it to robots.txt in the work directory.", defaultTimeout)
	assertCompleted(t, r)
	assertFileExists(t, "robots.txt")
}

func TestNet_StatusCode(t *testing.T) {
	addr := setup(t)
	r, res := submitWait(t, addr, "net-6", "Fetch https://httpbin.org/status/418 and tell me the HTTP status code.", defaultTimeout)
	assertCompleted(t, r)
	assertOutputContains(t, res, "418")
}

func TestNet_FetchIP(t *testing.T) {
	addr := setup(t)
	r, _ := submitWait(t, addr, "net-7", "Fetch https://httpbin.org/ip and tell me the origin IP address.", defaultTimeout)
	assertCompleted(t, r)
}

func TestNet_RedirectFollow(t *testing.T) {
	addr := setup(t)
	r, _ := submitWait(t, addr, "net-8", "Fetch https://httpbin.org/redirect/2 (which redirects twice) and show the final response.", defaultTimeout)
	assertCompleted(t, r)
}

func TestNet_FetchXML(t *testing.T) {
	addr := setup(t)
	r, _ := submitWait(t, addr, "net-9", "Fetch https://httpbin.org/xml and tell me the root element name.", defaultTimeout)
	assertCompleted(t, r)
}

func TestNet_MultipleRequests(t *testing.T) {
	addr := setup(t)
	r, _ := submitWait(t, addr, "net-10", "Fetch https://httpbin.org/get and https://httpbin.org/ip, and summarize both responses.", defaultTimeout)
	assertCompleted(t, r)
}

// ---------------------------------------------------------------------------
// Category 7: Data transformation / text processing (10 tests)
// ---------------------------------------------------------------------------

func TestData_CSVToJSON(t *testing.T) {
	addr := setup(t)
	os.WriteFile(filepath.Join(workspace, "input.csv"), []byte("name,age\nAlice,30\nBob,25\n"), 0o644)
	r, _ := submitWait(t, addr, "data-1", "Read input.csv and convert it to JSON format. Save the result as output.json.", defaultTimeout)
	assertCompleted(t, r)
	assertFileExists(t, "output.json")
}

func TestData_JSONToYAML(t *testing.T) {
	addr := setup(t)
	os.WriteFile(filepath.Join(workspace, "config.json"), []byte(`{"server":{"host":"localhost","port":8080},"debug":true}`), 0o644)
	r, _ := submitWait(t, addr, "data-2", "Read config.json and convert it to YAML format. Save the result as config.yaml.", defaultTimeout)
	assertCompleted(t, r)
	assertFileExists(t, "config.yaml")
}

func TestData_TextAnalysis(t *testing.T) {
	addr := setup(t)
	os.WriteFile(filepath.Join(workspace, "text.txt"), []byte("The quick brown fox jumps over the lazy dog. The dog slept peacefully. The fox ran away quickly."), 0o644)
	r, res := submitWait(t, addr, "data-3", "Read text.txt and count how many times the word 'the' appears (case insensitive).", defaultTimeout)
	assertCompleted(t, r)
	assertOutputContains(t, res, "3")
}

func TestData_ExtractEmails(t *testing.T) {
	addr := setup(t)
	os.WriteFile(filepath.Join(workspace, "contacts.txt"), []byte("John: john@example.com\nJane: jane@test.org\nBob: bob@mail.com\nNo email here"), 0o644)
	r, res := submitWait(t, addr, "data-4", "Read contacts.txt and extract all email addresses.", defaultTimeout)
	assertCompleted(t, r)
	assertOutputContains(t, res, "john@example.com")
}

func TestData_MarkdownTable(t *testing.T) {
	addr := setup(t)
	r, _ := submitWait(t, addr, "data-5", "Create a Markdown file table.md with a table of the top 5 most populous countries with columns: Rank, Country, Population (approximate).", defaultTimeout)
	assertCompleted(t, r)
	assertFileExists(t, "table.md")
	assertFileContains(t, "table.md", "|")
}

func TestData_RegexProcessing(t *testing.T) {
	addr := setup(t)
	os.WriteFile(filepath.Join(workspace, "logs.txt"), []byte("2024-01-15 ERROR: disk full\n2024-01-16 INFO: started\n2024-01-17 ERROR: timeout\n2024-01-18 INFO: ready\n"), 0o644)
	r, res := submitWait(t, addr, "data-6", "Read logs.txt and extract only the ERROR lines.", defaultTimeout)
	assertCompleted(t, r)
	assertOutputContains(t, res, "disk full")
}

func TestData_SortAndDeduplicate(t *testing.T) {
	addr := setup(t)
	os.WriteFile(filepath.Join(workspace, "dupes.txt"), []byte("banana\napple\ncherry\napple\nbanana\ndate\n"), 0o644)
	r, res := submitWait(t, addr, "data-7", "Read dupes.txt, remove duplicates, sort alphabetically, and save to unique.txt.", defaultTimeout)
	assertCompleted(t, r)
	assertFileExists(t, "unique.txt")
	if res != nil {
		t.Logf("output: %s", trunc(res.Output, 300))
	}
}

func TestData_WordFrequency(t *testing.T) {
	addr := setup(t)
	os.WriteFile(filepath.Join(workspace, "essay.txt"), []byte("go is a programming language. go is fast. go is simple. python is popular. python is easy."), 0o644)
	r, res := submitWait(t, addr, "data-8", "Read essay.txt and count the frequency of each word. Which word appears most often?", defaultTimeout)
	assertCompleted(t, r)
	assertOutputContains(t, res, "is")
}

func TestData_Base64EncodeDecode(t *testing.T) {
	addr := setup(t)
	r, res := submitWait(t, addr, "data-9", "Base64 encode the string 'HopClaw Test' and then decode it back. Show both the encoded and decoded values.", defaultTimeout)
	assertCompleted(t, r)
	assertOutputContains(t, res, "HopClaw Test")
}

func TestData_HashCalculation(t *testing.T) {
	addr := setup(t)
	r, _ := submitWait(t, addr, "data-10", "Calculate the SHA256 hash of the string 'hello world' using a command like: echo -n 'hello world' | shasum -a 256", defaultTimeout)
	assertCompleted(t, r)
}

// ---------------------------------------------------------------------------
// Category 8: Code generation & quality (10 tests)
// ---------------------------------------------------------------------------

func TestCode_GoProgram(t *testing.T) {
	addr := setup(t)
	r, _ := submitWait(t, addr, "code-1", "Write a Go program in main.go that implements a simple HTTP server on port 9999 that responds with 'pong' to GET /ping. Don't run it, just write the file.", defaultTimeout)
	assertCompleted(t, r)
	assertFileExists(t, "main.go")
	assertFileContains(t, "main.go", "pong")
}

func TestCode_PythonClass(t *testing.T) {
	addr := setup(t)
	r, _ := submitWait(t, addr, "code-2", "Write a Python file called todo.py with a TodoList class that supports add(item), remove(item), list(), and done(item) methods.", defaultTimeout)
	assertCompleted(t, r)
	assertFileExists(t, "todo.py")
	assertFileContains(t, "todo.py", "class TodoList")
}

func TestCode_JavaScriptModule(t *testing.T) {
	addr := setup(t)
	r, _ := submitWait(t, addr, "code-3", "Write a JavaScript module called validator.js with functions: isEmail(str), isURL(str), isPhone(str) using regex. Export them.", defaultTimeout)
	assertCompleted(t, r)
	assertFileExists(t, "validator.js")
	assertFileContains(t, "validator.js", "isEmail")
}

func TestCode_BashUtils(t *testing.T) {
	addr := setup(t)
	r, _ := submitWait(t, addr, "code-4", "Write a bash library file utils.sh with functions: log_info(), log_error(), check_command_exists(), require_root().", defaultTimeout)
	assertCompleted(t, r)
	assertFileExists(t, "utils.sh")
	assertFileContains(t, "utils.sh", "log_info")
}

func TestCode_SQLQueries(t *testing.T) {
	addr := setup(t)
	r, _ := submitWait(t, addr, "code-5", "Write a file queries.sql with 5 useful SQL queries for an e-commerce database: most popular products, revenue by month, inactive users, recent orders, average order value.", defaultTimeout)
	assertCompleted(t, r)
	assertFileExists(t, "queries.sql")
	assertFileContains(t, "queries.sql", "SELECT")
}

func TestCode_TypeScriptInterface(t *testing.T) {
	addr := setup(t)
	r, _ := submitWait(t, addr, "code-6", "Write a TypeScript file called types.ts with interfaces for a blog: User, Post, Comment, Tag. Include proper types for all fields.", defaultTimeout)
	assertCompleted(t, r)
	assertFileExists(t, "types.ts")
	assertFileContains(t, "types.ts", "interface")
}

func TestCode_PythonCLI(t *testing.T) {
	addr := setup(t)
	r, _ := submitWait(t, addr, "code-7", "Write a Python CLI tool called greet.py using argparse that takes --name and --greeting arguments and prints a personalized greeting. Then run it with: python3 greet.py --name HopClaw --greeting Hello", 180*time.Second)
	assertCompleted(t, r)
	assertFileExists(t, "greet.py")
}

func TestCode_CSSLayout(t *testing.T) {
	addr := setup(t)
	r, _ := submitWait(t, addr, "code-8", "Write a CSS file called layout.css with a responsive 3-column grid layout that collapses to 1 column on mobile (under 768px).", defaultTimeout)
	assertCompleted(t, r)
	assertFileExists(t, "layout.css")
	assertFileContains(t, "layout.css", "grid")
}

func TestCode_TerraformConfig(t *testing.T) {
	addr := setup(t)
	r, _ := submitWait(t, addr, "code-9", "Write a Terraform file called main.tf that defines an AWS EC2 instance with a security group allowing SSH and HTTP.", defaultTimeout)
	assertCompleted(t, r)
	assertFileExists(t, "main.tf")
	assertFileContains(t, "main.tf", "aws_instance")
}

func TestCode_ProtoDefinition(t *testing.T) {
	addr := setup(t)
	r, _ := submitWait(t, addr, "code-10", "Write a Protocol Buffers file called service.proto with a UserService that has CreateUser, GetUser, ListUsers RPCs.", defaultTimeout)
	assertCompleted(t, r)
	assertFileExists(t, "service.proto")
	assertFileContains(t, "service.proto", "service UserService")
}

// ---------------------------------------------------------------------------
// Category 9: Concurrent sessions (5 tests)
// ---------------------------------------------------------------------------

func TestConcurrent_TwoSessions(t *testing.T) {
	addr := setup(t)
	var wg sync.WaitGroup
	wg.Add(2)
	var r1, r2 *runInfo
	go func() {
		defer wg.Done()
		r1, _ = submitWait(t, addr, "conc-1a", "What is 2+2? Answer with just the number.", defaultTimeout)
	}()
	go func() {
		defer wg.Done()
		r2, _ = submitWait(t, addr, "conc-1b", "What is 3+3? Answer with just the number.", defaultTimeout)
	}()
	wg.Wait()
	assertCompleted(t, r1)
	assertCompleted(t, r2)
}

func TestConcurrent_ThreeParallel(t *testing.T) {
	addr := setup(t)
	var wg sync.WaitGroup
	results := make([]*runInfo, 3)
	for i := 0; i < 3; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			results[idx], _ = submitWait(t, addr, fmt.Sprintf("conc-2-%d", idx), fmt.Sprintf("What is %d * %d? Answer with just the number.", idx+2, idx+3), defaultTimeout)
		}(i)
	}
	wg.Wait()
	for i, r := range results {
		if r.Status != "completed" {
			t.Errorf("concurrent run %d: status=%s error=%s", i, r.Status, r.Error)
		}
	}
}

func TestConcurrent_SessionIsolation(t *testing.T) {
	addr := setup(t)
	// Two sessions should not see each other's context.
	r1, _ := submitWait(t, addr, "iso-1", "Remember: the secret word is 'elephant'. Confirm you remember it.", defaultTimeout)
	r2, res2 := submitWait(t, addr, "iso-2", "What is the secret word? (There isn't one, just say 'no secret word')", defaultTimeout)
	assertCompleted(t, r1)
	assertCompleted(t, r2)
	if res2 != nil && strings.Contains(strings.ToLower(res2.Output), "elephant") {
		t.Error("session isolation breach: iso-2 should not know about iso-1's secret")
	}
}

func TestConcurrent_SameSessionSequential(t *testing.T) {
	addr := setup(t)
	r1, _ := submitWait(t, addr, "seq-1", "My name is TestBot. Remember it.", defaultTimeout)
	assertCompleted(t, r1)
	r2, res2 := submitWait(t, addr, "seq-1", "What is my name?", defaultTimeout)
	assertCompleted(t, r2)
	assertOutputContains(t, res2, "TestBot")
}

func TestConcurrent_QueueBehavior(t *testing.T) {
	addr := setup(t)
	// Submit 3 runs fast to the same session.
	run1 := submit(t, addr, "queue-1", "Write a file q1.txt with content 'first'")
	run2 := submit(t, addr, "queue-1", "Write a file q2.txt with content 'second'")
	run3 := submit(t, addr, "queue-1", "Write a file q3.txt with content 'third'")
	// All should eventually complete.
	wait(t, addr, run1.ID, 180*time.Second)
	wait(t, addr, run2.ID, 180*time.Second)
	wait(t, addr, run3.ID, 180*time.Second)
}

// ---------------------------------------------------------------------------
// Category 10: Chinese language tasks (10 tests)
// ---------------------------------------------------------------------------

func TestChinese_SimpleTask(t *testing.T) {
	addr := setup(t)
	r, res := submitWait(t, addr, "zh-1", "用中文回答：1+1等于几？", defaultTimeout)
	assertCompleted(t, r)
	assertOutputContains(t, res, "2")
}

func TestChinese_WriteFile(t *testing.T) {
	addr := setup(t)
	r, _ := submitWait(t, addr, "zh-2", "创建一个文件 greeting.txt，内容是'你好世界'。", defaultTimeout)
	assertCompleted(t, r)
	assertFileExists(t, "greeting.txt")
}

func TestChinese_CodeGeneration(t *testing.T) {
	addr := setup(t)
	r, _ := submitWait(t, addr, "zh-3", "写一个Python脚本 hello_cn.py，打印 Hello World 和 你好世界。", defaultTimeout)
	assertCompleted(t, r)
	assertFileExists(t, "hello_cn.py")
}

func TestChinese_DataAnalysis(t *testing.T) {
	addr := setup(t)
	os.WriteFile(filepath.Join(workspace, "students.csv"), []byte("姓名,年龄,分数\n张三,20,85\n李四,21,90\n王五,19,78\n"), 0o644)
	r, res := submitWait(t, addr, "zh-4", "读取students.csv文件，告诉我平均分数是多少。", defaultTimeout)
	assertCompleted(t, r)
	assertOutputContains(t, res, "84")
}

func TestChinese_Translation(t *testing.T) {
	addr := setup(t)
	r, res := submitWait(t, addr, "zh-5", "把以下句子翻译成英文：'今天天气很好，适合出去散步。'", defaultTimeout)
	assertCompleted(t, r)
	assertOutputContains(t, res, "weather")
}

func TestChinese_Summarization(t *testing.T) {
	addr := setup(t)
	r, _ := submitWait(t, addr, "zh-6", "用中文简要介绍什么是微服务架构，3个要点。", defaultTimeout)
	assertCompleted(t, r)
}

func TestChinese_ShellTask(t *testing.T) {
	addr := setup(t)
	r, res := submitWait(t, addr, "zh-7", "执行命令 echo '测试成功' 并告诉我输出结果。", defaultTimeout)
	assertCompleted(t, r)
	assertOutputContains(t, res, "测试成功")
}

func TestChinese_MultiStep(t *testing.T) {
	addr := setup(t)
	r, _ := submitWait(t, addr, "zh-8", "创建一个目录叫 chinese_project，在里面创建 README.md（内容用中文写项目简介），和 main.py（打印'项目启动'）。", 180*time.Second)
	assertCompleted(t, r)
	assertFileExists(t, "chinese_project/README.md")
	assertFileExists(t, "chinese_project/main.py")
}

func TestChinese_MathProblem(t *testing.T) {
	addr := setup(t)
	r, res := submitWait(t, addr, "zh-9", "一个长方形的长是12厘米，宽是8厘米。请计算它的面积和周长。", defaultTimeout)
	assertCompleted(t, r)
	assertOutputContains(t, res, "96")
}

func TestChinese_CodeReview(t *testing.T) {
	addr := setup(t)
	os.WriteFile(filepath.Join(workspace, "review.py"), []byte(`
def add(a, b):
    return a + b

def divide(a, b):
    return a / b
`), 0o644)
	r, _ := submitWait(t, addr, "zh-10", "用中文审查 review.py 文件，指出潜在问题并给出改进建议。", defaultTimeout)
	assertCompleted(t, r)
}

// ---------------------------------------------------------------------------
// Category 11: Error handling & edge cases (5 tests)
// ---------------------------------------------------------------------------

func TestEdge_VeryLongInput(t *testing.T) {
	addr := setup(t)
	long := strings.Repeat("This is a test sentence. ", 100)
	r, _ := submitWait(t, addr, "edge-1", "Summarize this text in one sentence: "+long, defaultTimeout)
	assertCompleted(t, r)
}

func TestEdge_SpecialCharacters(t *testing.T) {
	addr := setup(t)
	r, _ := submitWait(t, addr, "edge-2", "Create a file called special.txt with content: Hello! @#$%^&*() 你好 こんにちは 🎉", defaultTimeout)
	assertCompleted(t, r)
	assertFileExists(t, "special.txt")
}

func TestEdge_EmptyFileHandling(t *testing.T) {
	addr := setup(t)
	os.WriteFile(filepath.Join(workspace, "empty.txt"), []byte(""), 0o644)
	r, _ := submitWait(t, addr, "edge-3", "Read empty.txt and tell me if it's empty or not.", defaultTimeout)
	assertCompleted(t, r)
}

func TestEdge_BinaryLikeContent(t *testing.T) {
	addr := setup(t)
	r, _ := submitWait(t, addr, "edge-4", "Create a file called binary_safe.txt with the text: \\x00\\x01\\xff (literally these escape sequences as text, not actual binary).", defaultTimeout)
	assertCompleted(t, r)
	assertFileExists(t, "binary_safe.txt")
}

func TestEdge_DeepDirectoryNesting(t *testing.T) {
	addr := setup(t)
	r, _ := submitWait(t, addr, "edge-5", "Create directory structure: a/b/c/d/ and write a file a/b/c/d/deep.txt with content 'found me'.", defaultTimeout)
	assertCompleted(t, r)
	assertFileExists(t, "a/b/c/d/deep.txt")
}

// ---------------------------------------------------------------------------
// Category 12: Desktop application automation (15 tests)
// ---------------------------------------------------------------------------

func TestDesktop_ListApps(t *testing.T) {
	addr := setup(t)
	r, res := submitWait(t, addr, "desk-1", "List the currently running desktop applications on this machine.", defaultTimeout)
	assertCompleted(t, r)
	if res != nil && res.Output == "" {
		t.Error("expected list of running apps")
	}
}

func TestDesktop_Screenshot(t *testing.T) {
	addr := setup(t)
	r, _ := submitWait(t, addr, "desk-2", "Take a screenshot of the current desktop.", defaultTimeout)
	assertCompleted(t, r)
}

func TestDesktop_OpenAndScreenshot(t *testing.T) {
	addr := setup(t)
	r, _ := submitWait(t, addr, "desk-3", "Open the Calculator app, wait a moment, then take a screenshot to confirm it opened.", 180*time.Second)
	assertCompleted(t, r)
}

func TestDesktop_OpenSafariAndNavigate(t *testing.T) {
	addr := setup(t)
	r, _ := submitWait(t, addr, "desk-4", "Open Safari, then use hotkey command+l to focus the address bar, type 'https://httpbin.org/get' and press Return. Wait 3 seconds then take a screenshot.", 180*time.Second)
	assertCompleted(t, r)
}

func TestDesktop_ClipboardRoundTrip(t *testing.T) {
	addr := setup(t)
	r, res := submitWait(t, addr, "desk-5", "Write the text 'HopClaw clipboard test 12345' to the system clipboard, then read it back and confirm the content matches.", defaultTimeout)
	assertCompleted(t, r)
	assertOutputContains(t, res, "12345")
}

func TestDesktop_CaptureTree(t *testing.T) {
	addr := setup(t)
	r, res := submitWait(t, addr, "desk-6", "Capture the desktop UI structure tree and tell me the name of the frontmost application.", defaultTimeout)
	assertCompleted(t, r)
	if res != nil && res.Output == "" {
		t.Error("expected frontmost app in output")
	}
}

func TestDesktop_TypeText(t *testing.T) {
	addr := setup(t)
	r, res := submitWait(t, addr, "desk-7", "Open TextEdit (or any text editor), create a new document, then use desktop.find_element / desktop.set_element_value / desktop.get_element_value to enter the exact text 'Hello from HopClaw desktop automation!' and read it back from the same editor element. Report the exact observed value after verification.", 180*time.Second)
	assertCompleted(t, r)
	assertOutputContains(t, res, "Hello from HopClaw desktop automation!")
}

func TestDesktop_HotkeyTest(t *testing.T) {
	addr := setup(t)
	r, _ := submitWait(t, addr, "desk-8", "Open Finder, then use the hotkey command+shift+g to open the 'Go to Folder' dialog, then take a screenshot.", 180*time.Second)
	assertCompleted(t, r)
}

func TestDesktop_FocusWindow(t *testing.T) {
	addr := setup(t)
	r, _ := submitWait(t, addr, "desk-9", "List the running applications, then focus the Finder window and take a screenshot.", 180*time.Second)
	assertCompleted(t, r)
}

func TestDesktop_ListWindows(t *testing.T) {
	addr := setup(t)
	r, res := submitWait(t, addr, "desk-10", "List all visible windows for the frontmost application.", defaultTimeout)
	assertCompleted(t, r)
	if res != nil && res.Output == "" {
		t.Error("expected window list in output")
	}
}

func TestDesktop_OpenTerminalRunCommand(t *testing.T) {
	addr := setup(t)
	r, res := submitWait(t, addr, "desk-11", "Open the Terminal app, wait for it to be ready, then type 'echo HopClaw-Desktop-Test' and press Return. Verify the visible terminal output contains 'HopClaw-Desktop-Test' using desktop.assert_element, OCR, or another direct read-back tool, and report the exact observed output.", 180*time.Second)
	assertCompleted(t, r)
	assertOutputContains(t, res, "HopClaw-Desktop-Test")
}

func TestDesktop_MultiAppWorkflow(t *testing.T) {
	addr := setup(t)
	r, _ := submitWait(t, addr, "desk-12", "Open Calculator, take a screenshot. Then open TextEdit, type 'Desktop workflow complete', take another screenshot. Report which apps were opened.", 180*time.Second)
	assertCompleted(t, r)
}

func TestDesktop_ScreenshotAndAnalyze(t *testing.T) {
	addr := setup(t)
	r, _ := submitWait(t, addr, "desk-13", "Take a screenshot of the current desktop. Then describe what you see in the screenshot, including the menu bar and any visible windows.", defaultTimeout)
	assertCompleted(t, r)
}

func TestDesktop_OpenNotesApp(t *testing.T) {
	addr := setup(t)
	r, res := submitWait(t, addr, "desk-14", "Open the Notes app (or Stickies if Notes is not available), create a new note, then use desktop.find_element / desktop.set_element_value / desktop.get_element_value to enter 'Meeting notes: discuss Q1 roadmap' and confirm the same element now contains that exact value. Report the verified value.", 180*time.Second)
	assertCompleted(t, r)
	assertOutputContains(t, res, "Meeting notes: discuss Q1 roadmap")
}

func TestDesktop_ClipboardWithChineseText(t *testing.T) {
	addr := setup(t)
	r, res := submitWait(t, addr, "desk-15", "Write the Chinese text '桌面自动化测试成功' to the clipboard, then read it back to confirm.", defaultTimeout)
	assertCompleted(t, r)
	assertOutputContains(t, res, "桌面自动化")
}
