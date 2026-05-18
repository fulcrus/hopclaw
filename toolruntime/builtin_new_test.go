package toolruntime

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/fulcrus/hopclaw/agent"
	"github.com/fulcrus/hopclaw/internal/modules"
	"github.com/fulcrus/hopclaw/skill"
)

// ---------------------------------------------------------------------------
// 1. TestFSExtraTools
// ---------------------------------------------------------------------------

func TestFSExtraTools(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	builtins := NewBuiltins(BuiltinsConfig{Root: root, MaxReadBytes: 64 * 1024})
	ctx := context.Background()
	run := &agent.Run{ID: "run-1"}
	sess := &agent.Session{ID: "sess-1"}

	exec := func(name string, input map[string]any) string {
		t.Helper()
		results, err := builtins.ExecuteBatch(ctx, run, sess, []agent.ToolCall{{
			ID: "call-" + name, Name: name, Input: input,
		}})
		if err != nil {
			t.Fatalf("%s error: %v", name, err)
		}
		return results[0].Content
	}

	// --- fs.chmod ---
	if err := os.WriteFile(filepath.Join(root, "chmod_target.txt"), []byte("chmod test"), 0o644); err != nil {
		t.Fatalf("WriteFile error: %v", err)
	}
	content := exec("fs.chmod", map[string]any{"path": "chmod_target.txt", "mode": "0755"})
	var chmodResult struct {
		Path    string `json:"path"`
		Mode    string `json:"mode"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal([]byte(content), &chmodResult); err != nil {
		t.Fatalf("fs.chmod unmarshal: %v", err)
	}
	if chmodResult.Mode != "0755" {
		t.Fatalf("fs.chmod mode = %q, want 0755", chmodResult.Mode)
	}
	info, err := os.Stat(filepath.Join(root, "chmod_target.txt"))
	if err != nil {
		t.Fatalf("stat after chmod: %v", err)
	}
	if info.Mode().Perm() != 0o755 {
		t.Fatalf("fs.chmod file perm = %v, want 0755", info.Mode().Perm())
	}

	// --- fs.link ---
	if err := os.WriteFile(filepath.Join(root, "link_target.txt"), []byte("link data"), 0o644); err != nil {
		t.Fatalf("WriteFile error: %v", err)
	}
	content = exec("fs.link", map[string]any{"target": "link_target.txt", "link_path": "my_link.txt"})
	var linkResult struct {
		Target   string `json:"target"`
		LinkPath string `json:"link_path"`
		Message  string `json:"message"`
	}
	if err := json.Unmarshal([]byte(content), &linkResult); err != nil {
		t.Fatalf("fs.link unmarshal: %v", err)
	}
	if linkResult.LinkPath == "" {
		t.Fatal("fs.link link_path is empty")
	}
	resolved, err := os.Readlink(filepath.Join(root, "my_link.txt"))
	if err != nil {
		t.Fatalf("Readlink error: %v", err)
	}
	if !strings.HasSuffix(resolved, "link_target.txt") {
		t.Fatalf("symlink target = %q, want suffix link_target.txt", resolved)
	}

	// --- fs.tmp (file) ---
	content = exec("fs.tmp", map[string]any{"prefix": "test-"})
	var tmpResult struct {
		Path  string `json:"path"`
		IsDir bool   `json:"is_dir"`
	}
	if err := json.Unmarshal([]byte(content), &tmpResult); err != nil {
		t.Fatalf("fs.tmp unmarshal: %v", err)
	}
	if tmpResult.IsDir {
		t.Fatal("fs.tmp (file) is_dir should be false")
	}
	absPath := filepath.Join(root, tmpResult.Path)
	if _, err := os.Stat(absPath); err != nil {
		t.Fatalf("fs.tmp file does not exist: %v", err)
	}

	// --- fs.tmp (dir) ---
	content = exec("fs.tmp", map[string]any{"prefix": "testdir-", "is_dir": true})
	if err := json.Unmarshal([]byte(content), &tmpResult); err != nil {
		t.Fatalf("fs.tmp dir unmarshal: %v", err)
	}
	if !tmpResult.IsDir {
		t.Fatal("fs.tmp (dir) is_dir should be true")
	}
	absPath = filepath.Join(root, tmpResult.Path)
	fi, err := os.Stat(absPath)
	if err != nil {
		t.Fatalf("fs.tmp dir does not exist: %v", err)
	}
	if !fi.IsDir() {
		t.Fatal("fs.tmp dir is not a directory")
	}

	// --- fs.disk ---
	content = exec("fs.disk", map[string]any{})
	var diskResult struct {
		TotalBytes uint64 `json:"total_bytes"`
		FreeBytes  uint64 `json:"free_bytes"`
	}
	if err := json.Unmarshal([]byte(content), &diskResult); err != nil {
		t.Fatalf("fs.disk unmarshal: %v", err)
	}
	if diskResult.TotalBytes == 0 {
		t.Fatal("fs.disk total_bytes is 0")
	}
	if diskResult.FreeBytes == 0 {
		t.Fatal("fs.disk free_bytes is 0")
	}
}

// ---------------------------------------------------------------------------
// 2. TestExecScript
// ---------------------------------------------------------------------------

func TestExecScript(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	builtins := NewBuiltins(BuiltinsConfig{Root: root, MaxReadBytes: 64 * 1024})
	ctx := context.Background()
	run := &agent.Run{ID: "run-1"}
	sess := &agent.Session{ID: "sess-1"}

	exec := func(name string, input map[string]any) string {
		t.Helper()
		results, err := builtins.ExecuteBatch(ctx, run, sess, []agent.ToolCall{{
			ID: "call-" + name, Name: name, Input: input,
		}})
		if err != nil {
			t.Fatalf("%s error: %v", name, err)
		}
		return results[0].Content
	}

	// Normal script
	content := exec("exec.script", map[string]any{"script": "echo hello"})
	var scriptResult struct {
		Stdout   string `json:"stdout"`
		Stderr   string `json:"stderr"`
		ExitCode int    `json:"exit_code"`
	}
	if err := json.Unmarshal([]byte(content), &scriptResult); err != nil {
		t.Fatalf("exec.script unmarshal: %v", err)
	}
	if scriptResult.Stdout != "hello" {
		t.Fatalf("exec.script stdout = %q, want hello", scriptResult.Stdout)
	}
	if scriptResult.ExitCode != 0 {
		t.Fatalf("exec.script exit_code = %d, want 0", scriptResult.ExitCode)
	}

	// Non-zero exit
	content = exec("exec.script", map[string]any{"script": "exit 1"})
	if err := json.Unmarshal([]byte(content), &scriptResult); err != nil {
		t.Fatalf("exec.script unmarshal: %v", err)
	}
	if scriptResult.ExitCode != 1 {
		t.Fatalf("exec.script exit_code = %d, want 1", scriptResult.ExitCode)
	}
}

// ---------------------------------------------------------------------------
// 3. TestEnvTools
// ---------------------------------------------------------------------------

func TestEnvTools(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	builtins := NewBuiltins(BuiltinsConfig{Root: root, MaxReadBytes: 64 * 1024})
	ctx := context.Background()
	run := &agent.Run{ID: "run-1"}
	sess := &agent.Session{ID: "sess-1"}

	exec := func(name string, input map[string]any) string {
		t.Helper()
		results, err := builtins.ExecuteBatch(ctx, run, sess, []agent.ToolCall{{
			ID: "call-" + name, Name: name, Input: input,
		}})
		if err != nil {
			t.Fatalf("%s error: %v", name, err)
		}
		return results[0].Content
	}

	// --- env.info ---
	content := exec("env.info", map[string]any{})
	var infoResult struct {
		OS       string `json:"os"`
		Arch     string `json:"arch"`
		Hostname string `json:"hostname"`
		CPUs     int    `json:"cpus"`
	}
	if err := json.Unmarshal([]byte(content), &infoResult); err != nil {
		t.Fatalf("env.info unmarshal: %v", err)
	}
	if infoResult.OS == "" {
		t.Fatal("env.info os is empty")
	}
	if infoResult.Arch == "" {
		t.Fatal("env.info arch is empty")
	}
	if infoResult.Hostname == "" {
		t.Fatal("env.info hostname is empty")
	}
	if infoResult.CPUs <= 0 {
		t.Fatalf("env.info cpus = %d, want > 0", infoResult.CPUs)
	}

	// --- env.probe ---
	content = exec("env.probe", map[string]any{})
	var probeResult struct {
		OS            string            `json:"os"`
		Arch          string            `json:"arch"`
		AvailableBins map[string]string `json:"available_bins"`
	}
	if err := json.Unmarshal([]byte(content), &probeResult); err != nil {
		t.Fatalf("env.probe unmarshal: %v", err)
	}
	if probeResult.OS == "" {
		t.Fatal("env.probe os is empty")
	}
	if probeResult.Arch == "" {
		t.Fatal("env.probe arch is empty")
	}
	if probeResult.AvailableBins == nil {
		t.Fatal("env.probe available_bins is nil")
	}

	// --- env.set then env.get ---
	envKey := fmt.Sprintf("OPENCLAW_TEST_%d", time.Now().UnixNano())
	exec("env.set", map[string]any{"name": envKey, "value": "test_value_42"})
	content = exec("env.get", map[string]any{"name": envKey})
	var getResult struct {
		Name     string `json:"name"`
		Exists   bool   `json:"exists"`
		Source   string `json:"source"`
		Managed  bool   `json:"managed"`
		Redacted bool   `json:"redacted"`
	}
	if err := json.Unmarshal([]byte(content), &getResult); err != nil {
		t.Fatalf("env.get unmarshal: %v", err)
	}
	if !getResult.Exists || getResult.Source != "overlay" || !getResult.Redacted || getResult.Managed {
		t.Fatalf("env.get result = %+v, want run overlay visibility only", getResult)
	}
	if got := os.Getenv(envKey); got != "" {
		t.Fatalf("os.Getenv(%s) = %q after env.set, want host process unchanged", envKey, got)
	}

	// --- env.refresh ---
	content = exec("env.refresh", map[string]any{})
	var refreshResult struct {
		Summary      map[string]any `json:"summary"`
		NewlyActive  []string       `json:"newly_active"`
		NewlyDormant []string       `json:"newly_dormant"`
	}
	if err := json.Unmarshal([]byte(content), &refreshResult); err != nil {
		t.Fatalf("env.refresh unmarshal: %v", err)
	}
	if refreshResult.Summary == nil {
		t.Fatal("env.refresh summary is nil")
	}
}

// ---------------------------------------------------------------------------
// 4. TestNetTools
// ---------------------------------------------------------------------------

func TestNetTools(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	builtins := NewBuiltins(BuiltinsConfig{Root: root, MaxReadBytes: 64 * 1024})
	ctx := context.Background()
	run := &agent.Run{ID: "run-1"}
	sess := &agent.Session{ID: "sess-1"}

	exec := func(name string, input map[string]any) string {
		t.Helper()
		results, err := builtins.ExecuteBatch(ctx, run, sess, []agent.ToolCall{{
			ID: "call-" + name, Name: name, Input: input,
		}})
		if err != nil {
			t.Fatalf("%s error: %v", name, err)
		}
		return results[0].Content
	}

	// --- net.dns ---
	content := exec("net.dns", map[string]any{"host": "localhost"})
	var dnsResult struct {
		Host    string   `json:"host"`
		Type    string   `json:"type"`
		Records []string `json:"records"`
	}
	if err := json.Unmarshal([]byte(content), &dnsResult); err != nil {
		t.Fatalf("net.dns unmarshal: %v", err)
	}
	if dnsResult.Host != "localhost" {
		t.Fatalf("net.dns host = %q, want localhost", dnsResult.Host)
	}
	if len(dnsResult.Records) == 0 {
		t.Fatal("net.dns records is empty for localhost")
	}

	// --- net.ip ---
	content = exec("net.ip", map[string]any{})
	var ipResult struct {
		Interfaces []map[string]any `json:"interfaces"`
	}
	if err := json.Unmarshal([]byte(content), &ipResult); err != nil {
		t.Fatalf("net.ip unmarshal: %v", err)
	}
	if len(ipResult.Interfaces) == 0 {
		t.Fatal("net.ip interfaces is empty")
	}

	// --- net.ping ---
	// Start a local TCP listener so we have a known-open port.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
	}))
	defer srv.Close()
	// Extract port from srv.URL (e.g., "http://127.0.0.1:12345")
	srvURL := srv.URL
	colonIdx := strings.LastIndex(srvURL, ":")
	srvPort := srvURL[colonIdx+1:]

	content = exec("net.ping", map[string]any{"host": "127.0.0.1", "port": mustParseInt(srvPort)})
	var pingResult struct {
		Host      string `json:"host"`
		Port      int    `json:"port"`
		Reachable bool   `json:"reachable"`
	}
	if err := json.Unmarshal([]byte(content), &pingResult); err != nil {
		t.Fatalf("net.ping unmarshal: %v", err)
	}
	if pingResult.Host != "127.0.0.1" {
		t.Fatalf("net.ping host = %q", pingResult.Host)
	}
	if !pingResult.Reachable {
		t.Fatal("net.ping reachable should be true for local test server")
	}

	// --- net.http ---
	httpSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"status":"ok"}`))
	}))
	defer httpSrv.Close()

	content = exec("net.http", map[string]any{"url": httpSrv.URL, "method": "GET"})
	var httpResult struct {
		StatusCode int    `json:"status_code"`
		Body       string `json:"body"`
	}
	if err := json.Unmarshal([]byte(content), &httpResult); err != nil {
		t.Fatalf("net.http unmarshal: %v", err)
	}
	if httpResult.StatusCode != 200 {
		t.Fatalf("net.http status_code = %d, want 200", httpResult.StatusCode)
	}
	if !strings.Contains(httpResult.Body, `"status":"ok"`) {
		t.Fatalf("net.http body = %q", httpResult.Body)
	}

	// --- net.fetch ---
	fetchSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.Write([]byte("<html><body><p>Hello World</p></body></html>"))
	}))
	defer fetchSrv.Close()

	content = exec("net.fetch", map[string]any{"url": fetchSrv.URL})
	var fetchResult struct {
		URL         string `json:"url"`
		ContentType string `json:"content_type"`
		Content     string `json:"content"`
		Truncated   bool   `json:"truncated"`
	}
	if err := json.Unmarshal([]byte(content), &fetchResult); err != nil {
		t.Fatalf("net.fetch unmarshal: %v", err)
	}
	if !strings.Contains(fetchResult.Content, "Hello World") {
		t.Fatalf("net.fetch content = %q, want to contain 'Hello World'", fetchResult.Content)
	}
	if fetchResult.Truncated {
		t.Fatal("net.fetch truncated should be false for small content")
	}

	// --- web.fetch ---
	content = exec("web.fetch", map[string]any{"url": fetchSrv.URL})
	var webFetchResult struct {
		URL         string `json:"url"`
		FinalURL    string `json:"final_url"`
		Domain      string `json:"domain"`
		Title       string `json:"title"`
		ContentType string `json:"content_type"`
		Content     string `json:"content"`
		StatusCode  int    `json:"status_code"`
		Truncated   bool   `json:"truncated"`
	}
	if err := json.Unmarshal([]byte(content), &webFetchResult); err != nil {
		t.Fatalf("web.fetch unmarshal: %v", err)
	}
	if webFetchResult.StatusCode != 200 {
		t.Fatalf("web.fetch status_code = %d, want 200", webFetchResult.StatusCode)
	}
	if webFetchResult.Domain == "" {
		t.Fatal("web.fetch domain should not be empty")
	}
	if !strings.Contains(webFetchResult.Content, "Hello World") {
		t.Fatalf("web.fetch content = %q, want to contain 'Hello World'", webFetchResult.Content)
	}
}

// mustParseInt converts a numeric string to int for test inputs.
func mustParseInt(s string) int {
	var n int
	fmt.Sscanf(s, "%d", &n)
	return n
}

// ---------------------------------------------------------------------------
// 5. TestTextTools
// ---------------------------------------------------------------------------

func TestTextTools(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	builtins := NewBuiltins(BuiltinsConfig{Root: root, MaxReadBytes: 64 * 1024})
	ctx := context.Background()
	run := &agent.Run{ID: "run-1"}
	sess := &agent.Session{ID: "sess-1"}

	exec := func(name string, input map[string]any) string {
		t.Helper()
		results, err := builtins.ExecuteBatch(ctx, run, sess, []agent.ToolCall{{
			ID: "call-" + name, Name: name, Input: input,
		}})
		if err != nil {
			t.Fatalf("%s error: %v", name, err)
		}
		return results[0].Content
	}

	// --- text.json ---
	content := exec("text.json", map[string]any{
		"input": `{"name":"test","age":30}`,
		"query": ".name",
	})
	var jsonResult struct {
		Result json.RawMessage `json:"result"`
	}
	if err := json.Unmarshal([]byte(content), &jsonResult); err != nil {
		t.Fatalf("text.json unmarshal: %v", err)
	}
	var nameVal string
	if err := json.Unmarshal(jsonResult.Result, &nameVal); err != nil {
		t.Fatalf("text.json result unmarshal: %v", err)
	}
	if nameVal != "test" {
		t.Fatalf("text.json result = %q, want test", nameVal)
	}

	// --- text.csv ---
	csvInput := "name,age,city\nAlice,30,NYC\nBob,25,LA"
	content = exec("text.csv", map[string]any{"input": csvInput})
	var csvResult struct {
		Headers  []string   `json:"headers"`
		Rows     [][]string `json:"rows"`
		RowCount int        `json:"row_count"`
	}
	if err := json.Unmarshal([]byte(content), &csvResult); err != nil {
		t.Fatalf("text.csv unmarshal: %v", err)
	}
	if csvResult.RowCount != 2 {
		t.Fatalf("text.csv row_count = %d, want 2", csvResult.RowCount)
	}
	if len(csvResult.Headers) != 3 || csvResult.Headers[0] != "name" {
		t.Fatalf("text.csv headers = %v", csvResult.Headers)
	}

	// --- text.base64 encode ---
	content = exec("text.base64", map[string]any{"input": "hello"})
	var b64EncResult struct {
		Result string `json:"result"`
	}
	if err := json.Unmarshal([]byte(content), &b64EncResult); err != nil {
		t.Fatalf("text.base64 encode unmarshal: %v", err)
	}
	if b64EncResult.Result == "" {
		t.Fatal("text.base64 encode result is empty")
	}
	// Decode back
	content = exec("text.base64", map[string]any{"input": b64EncResult.Result, "decode": true})
	var b64DecResult struct {
		Result string `json:"result"`
	}
	if err := json.Unmarshal([]byte(content), &b64DecResult); err != nil {
		t.Fatalf("text.base64 decode unmarshal: %v", err)
	}
	if b64DecResult.Result != "hello" {
		t.Fatalf("text.base64 roundtrip = %q, want hello", b64DecResult.Result)
	}

	// --- text.hex ---
	content = exec("text.hex", map[string]any{"input": "hello"})
	var hexResult struct {
		Result string `json:"result"`
	}
	if err := json.Unmarshal([]byte(content), &hexResult); err != nil {
		t.Fatalf("text.hex unmarshal: %v", err)
	}
	if hexResult.Result != "68656c6c6f" {
		t.Fatalf("text.hex result = %q, want 68656c6c6f", hexResult.Result)
	}

	// --- text.regex ---
	content = exec("text.regex", map[string]any{
		"pattern": `\d+`,
		"input":   "abc123def456",
	})
	var regexResult struct {
		Matches []string `json:"matches"`
		Count   int      `json:"count"`
	}
	if err := json.Unmarshal([]byte(content), &regexResult); err != nil {
		t.Fatalf("text.regex unmarshal: %v", err)
	}
	if regexResult.Count != 2 {
		t.Fatalf("text.regex count = %d, want 2", regexResult.Count)
	}
	if regexResult.Matches[0] != "123" || regexResult.Matches[1] != "456" {
		t.Fatalf("text.regex matches = %v", regexResult.Matches)
	}

	// --- text.count ---
	content = exec("text.count", map[string]any{"input": "hello world\nfoo bar"})
	var countResult struct {
		Lines      int `json:"lines"`
		Words      int `json:"words"`
		Characters int `json:"characters"`
		Bytes      int `json:"bytes"`
	}
	if err := json.Unmarshal([]byte(content), &countResult); err != nil {
		t.Fatalf("text.count unmarshal: %v", err)
	}
	if countResult.Lines != 2 {
		t.Fatalf("text.count lines = %d, want 2", countResult.Lines)
	}
	if countResult.Words != 4 {
		t.Fatalf("text.count words = %d, want 4", countResult.Words)
	}
	if countResult.Characters != 19 {
		t.Fatalf("text.count characters = %d, want 19", countResult.Characters)
	}

	// --- text.uuid ---
	content = exec("text.uuid", map[string]any{"count": 1})
	var uuidResult struct {
		UUID string `json:"uuid"`
	}
	if err := json.Unmarshal([]byte(content), &uuidResult); err != nil {
		t.Fatalf("text.uuid unmarshal: %v", err)
	}
	uuidRe := regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)
	if !uuidRe.MatchString(uuidResult.UUID) {
		t.Fatalf("text.uuid format invalid: %q", uuidResult.UUID)
	}

	// --- text.hash ---
	content = exec("text.hash", map[string]any{"input": "hello", "algorithm": "sha256"})
	var hashResult struct {
		Hash      string `json:"hash"`
		Algorithm string `json:"algorithm"`
	}
	if err := json.Unmarshal([]byte(content), &hashResult); err != nil {
		t.Fatalf("text.hash unmarshal: %v", err)
	}
	if hashResult.Hash == "" {
		t.Fatal("text.hash hash is empty")
	}
	if hashResult.Algorithm != "sha256" {
		t.Fatalf("text.hash algorithm = %q, want sha256", hashResult.Algorithm)
	}

	// --- text.url ---
	content = exec("text.url", map[string]any{"input": "https://example.com/path?q=1"})
	var urlResult struct {
		Scheme   string         `json:"scheme"`
		Host     string         `json:"host"`
		Path     string         `json:"path"`
		Query    map[string]any `json:"query"`
		Fragment string         `json:"fragment"`
	}
	if err := json.Unmarshal([]byte(content), &urlResult); err != nil {
		t.Fatalf("text.url unmarshal: %v", err)
	}
	if urlResult.Scheme != "https" {
		t.Fatalf("text.url scheme = %q, want https", urlResult.Scheme)
	}
	if urlResult.Host != "example.com" {
		t.Fatalf("text.url host = %q, want example.com", urlResult.Host)
	}
	if urlResult.Path != "/path" {
		t.Fatalf("text.url path = %q, want /path", urlResult.Path)
	}
	if urlResult.Query["q"] != "1" {
		t.Fatalf("text.url query = %v", urlResult.Query)
	}

	// --- text.dotenv ---
	content = exec("text.dotenv", map[string]any{"input": "KEY=value\n# comment\nFOO=bar"})
	var dotenvResult struct {
		Vars  map[string]string `json:"vars"`
		Count int               `json:"count"`
	}
	if err := json.Unmarshal([]byte(content), &dotenvResult); err != nil {
		t.Fatalf("text.dotenv unmarshal: %v", err)
	}
	if dotenvResult.Vars["KEY"] != "value" || dotenvResult.Vars["FOO"] != "bar" {
		t.Fatalf("text.dotenv vars = %v", dotenvResult.Vars)
	}
	if dotenvResult.Count != 2 {
		t.Fatalf("text.dotenv count = %d, want 2", dotenvResult.Count)
	}

	// --- text.ini ---
	content = exec("text.ini", map[string]any{"input": "[section]\nkey=value"})
	var iniResult struct {
		Result map[string]any `json:"result"`
	}
	if err := json.Unmarshal([]byte(content), &iniResult); err != nil {
		t.Fatalf("text.ini unmarshal: %v", err)
	}
	sectionMap, ok := iniResult.Result["section"].(map[string]any)
	if !ok {
		t.Fatalf("text.ini result missing section: %v", iniResult.Result)
	}
	if sectionMap["key"] != "value" {
		t.Fatalf("text.ini section.key = %v, want value", sectionMap["key"])
	}

	// --- text.jsonl ---
	jsonlInput := `{"id":1,"name":"alice"}
{"id":2,"name":"bob"}
{"id":3,"name":"charlie"}`
	content = exec("text.jsonl", map[string]any{"input": jsonlInput})
	var jsonlResult struct {
		Records []any `json:"records"`
		Count   int   `json:"count"`
	}
	if err := json.Unmarshal([]byte(content), &jsonlResult); err != nil {
		t.Fatalf("text.jsonl unmarshal: %v", err)
	}
	if jsonlResult.Count != 3 {
		t.Fatalf("text.jsonl count = %d, want 3", jsonlResult.Count)
	}
}

// ---------------------------------------------------------------------------
// 6. TestArchiveTools
// ---------------------------------------------------------------------------

func TestArchiveTools(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	builtins := NewBuiltins(BuiltinsConfig{Root: root, MaxReadBytes: 64 * 1024})
	ctx := context.Background()
	run := &agent.Run{ID: "run-1"}
	sess := &agent.Session{ID: "sess-1"}

	exec := func(name string, input map[string]any) string {
		t.Helper()
		results, err := builtins.ExecuteBatch(ctx, run, sess, []agent.ToolCall{{
			ID: "call-" + name, Name: name, Input: input,
		}})
		if err != nil {
			t.Fatalf("%s error: %v", name, err)
		}
		return results[0].Content
	}

	// Create test files
	if err := os.MkdirAll(filepath.Join(root, "src"), 0o755); err != nil {
		t.Fatalf("MkdirAll error: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "src", "hello.txt"), []byte("hello from archive"), 0o644); err != nil {
		t.Fatalf("WriteFile error: %v", err)
	}

	// --- archive.zip ---
	content := exec("archive.zip", map[string]any{
		"output": "test.zip",
		"paths":  []any{"src/hello.txt"},
	})
	var zipResult struct {
		Path      string `json:"path"`
		FileCount int    `json:"file_count"`
	}
	if err := json.Unmarshal([]byte(content), &zipResult); err != nil {
		t.Fatalf("archive.zip unmarshal: %v", err)
	}
	if zipResult.FileCount != 1 {
		t.Fatalf("archive.zip file_count = %d, want 1", zipResult.FileCount)
	}

	// --- archive.list (zip) ---
	content = exec("archive.list", map[string]any{"path": "test.zip"})
	var listResult struct {
		Format  string           `json:"format"`
		Count   int              `json:"count"`
		Entries []map[string]any `json:"entries"`
	}
	if err := json.Unmarshal([]byte(content), &listResult); err != nil {
		t.Fatalf("archive.list unmarshal: %v", err)
	}
	if listResult.Format != "zip" {
		t.Fatalf("archive.list format = %q, want zip", listResult.Format)
	}
	if listResult.Count < 1 {
		t.Fatalf("archive.list count = %d, want >= 1", listResult.Count)
	}

	// --- archive.unzip ---
	if err := os.MkdirAll(filepath.Join(root, "unzipped"), 0o755); err != nil {
		t.Fatalf("MkdirAll error: %v", err)
	}
	content = exec("archive.unzip", map[string]any{
		"path":   "test.zip",
		"output": "unzipped",
	})
	var unzipResult struct {
		FileCount int `json:"file_count"`
	}
	if err := json.Unmarshal([]byte(content), &unzipResult); err != nil {
		t.Fatalf("archive.unzip unmarshal: %v", err)
	}
	if unzipResult.FileCount != 1 {
		t.Fatalf("archive.unzip file_count = %d, want 1", unzipResult.FileCount)
	}
	// Verify extracted file content
	data, err := os.ReadFile(filepath.Join(root, "unzipped", "hello.txt"))
	if err != nil {
		t.Fatalf("read unzipped file: %v", err)
	}
	if string(data) != "hello from archive" {
		t.Fatalf("unzipped content = %q", string(data))
	}

	// --- archive.tar ---
	content = exec("archive.tar", map[string]any{
		"output":   "test.tar.gz",
		"paths":    []any{"src/hello.txt"},
		"compress": true,
	})
	var tarResult struct {
		Path       string `json:"path"`
		FileCount  int    `json:"file_count"`
		Compressed bool   `json:"compressed"`
	}
	if err := json.Unmarshal([]byte(content), &tarResult); err != nil {
		t.Fatalf("archive.tar unmarshal: %v", err)
	}
	if tarResult.FileCount != 1 {
		t.Fatalf("archive.tar file_count = %d, want 1", tarResult.FileCount)
	}
	if !tarResult.Compressed {
		t.Fatal("archive.tar compressed should be true")
	}

	// --- archive.untar ---
	if err := os.MkdirAll(filepath.Join(root, "untarred"), 0o755); err != nil {
		t.Fatalf("MkdirAll error: %v", err)
	}
	content = exec("archive.untar", map[string]any{
		"path":   "test.tar.gz",
		"output": "untarred",
	})
	var untarResult struct {
		FileCount int `json:"file_count"`
	}
	if err := json.Unmarshal([]byte(content), &untarResult); err != nil {
		t.Fatalf("archive.untar unmarshal: %v", err)
	}
	if untarResult.FileCount != 1 {
		t.Fatalf("archive.untar file_count = %d, want 1", untarResult.FileCount)
	}
	data, err = os.ReadFile(filepath.Join(root, "untarred", "hello.txt"))
	if err != nil {
		t.Fatalf("read untarred file: %v", err)
	}
	if string(data) != "hello from archive" {
		t.Fatalf("untarred content = %q", string(data))
	}
}

// ---------------------------------------------------------------------------
// 7. TestCryptoTools
// ---------------------------------------------------------------------------

func TestCryptoTools(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	builtins := NewBuiltins(BuiltinsConfig{Root: root, MaxReadBytes: 64 * 1024})
	ctx := context.Background()
	run := &agent.Run{ID: "run-1"}
	sess := &agent.Session{ID: "sess-1"}

	exec := func(name string, input map[string]any) string {
		t.Helper()
		results, err := builtins.ExecuteBatch(ctx, run, sess, []agent.ToolCall{{
			ID: "call-" + name, Name: name, Input: input,
		}})
		if err != nil {
			t.Fatalf("%s error: %v", name, err)
		}
		return results[0].Content
	}

	// --- crypto.hash ---
	content := exec("crypto.hash", map[string]any{"input": "hello", "algorithm": "sha256"})
	var hashResult struct {
		Hash      string `json:"hash"`
		Algorithm string `json:"algorithm"`
		InputSize int64  `json:"input_size"`
	}
	if err := json.Unmarshal([]byte(content), &hashResult); err != nil {
		t.Fatalf("crypto.hash unmarshal: %v", err)
	}
	// Known SHA256 of "hello": 2cf24dba5fb0a30e26e83b2ac5b9e29e1b161e5c1fa7425e73043362938b9824
	expectedHash := "2cf24dba5fb0a30e26e83b2ac5b9e29e1b161e5c1fa7425e73043362938b9824"
	if hashResult.Hash != expectedHash {
		t.Fatalf("crypto.hash hash = %q, want %q", hashResult.Hash, expectedHash)
	}
	if hashResult.Algorithm != "sha256" {
		t.Fatalf("crypto.hash algorithm = %q, want sha256", hashResult.Algorithm)
	}
	if hashResult.InputSize != 5 {
		t.Fatalf("crypto.hash input_size = %d, want 5", hashResult.InputSize)
	}

	// --- crypto.hmac ---
	content = exec("crypto.hmac", map[string]any{"input": "hello", "key": "secret"})
	var hmacResult struct {
		HMAC      string `json:"hmac"`
		Algorithm string `json:"algorithm"`
	}
	if err := json.Unmarshal([]byte(content), &hmacResult); err != nil {
		t.Fatalf("crypto.hmac unmarshal: %v", err)
	}
	if hmacResult.HMAC == "" {
		t.Fatal("crypto.hmac hmac is empty")
	}
	// Sign again and verify deterministic
	content2 := exec("crypto.hmac", map[string]any{"input": "hello", "key": "secret"})
	var hmacResult2 struct {
		HMAC string `json:"hmac"`
	}
	json.Unmarshal([]byte(content2), &hmacResult2)
	if hmacResult.HMAC != hmacResult2.HMAC {
		t.Fatalf("crypto.hmac not deterministic: %q vs %q", hmacResult.HMAC, hmacResult2.HMAC)
	}

	// --- crypto.random ---
	content = exec("crypto.random", map[string]any{"length": 16, "encoding": "hex"})
	var randomResult struct {
		Value    string `json:"value"`
		Length   int    `json:"length"`
		Encoding string `json:"encoding"`
	}
	if err := json.Unmarshal([]byte(content), &randomResult); err != nil {
		t.Fatalf("crypto.random unmarshal: %v", err)
	}
	// 16 bytes in hex = 32 hex chars
	if len(randomResult.Value) != 32 {
		t.Fatalf("crypto.random hex value length = %d, want 32", len(randomResult.Value))
	}
	if randomResult.Encoding != "hex" {
		t.Fatalf("crypto.random encoding = %q, want hex", randomResult.Encoding)
	}

	// --- crypto.aes encrypt + decrypt roundtrip ---
	key := "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	plaintext := "hello world"

	content = exec("crypto.aes", map[string]any{
		"input": plaintext,
		"key":   key,
		"mode":  "encrypt",
	})
	var encryptResult struct {
		Result string `json:"result"`
		Mode   string `json:"mode"`
	}
	if err := json.Unmarshal([]byte(content), &encryptResult); err != nil {
		t.Fatalf("crypto.aes encrypt unmarshal: %v", err)
	}
	if encryptResult.Mode != "encrypt" {
		t.Fatalf("crypto.aes encrypt mode = %q", encryptResult.Mode)
	}
	if encryptResult.Result == "" {
		t.Fatal("crypto.aes encrypt result is empty")
	}

	// Decrypt back
	content = exec("crypto.aes", map[string]any{
		"input": encryptResult.Result,
		"key":   key,
		"mode":  "decrypt",
	})
	var decryptResult struct {
		Result string `json:"result"`
		Mode   string `json:"mode"`
	}
	if err := json.Unmarshal([]byte(content), &decryptResult); err != nil {
		t.Fatalf("crypto.aes decrypt unmarshal: %v", err)
	}
	if decryptResult.Result != plaintext {
		t.Fatalf("crypto.aes decrypt result = %q, want %q", decryptResult.Result, plaintext)
	}
	if decryptResult.Mode != "decrypt" {
		t.Fatalf("crypto.aes decrypt mode = %q", decryptResult.Mode)
	}
}

// ---------------------------------------------------------------------------
// 8. TestDBKVTools
// ---------------------------------------------------------------------------

func TestDBKVTools(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	builtins := NewBuiltins(BuiltinsConfig{Root: root, MaxReadBytes: 64 * 1024})
	ctx := context.Background()
	run := &agent.Run{ID: "run-1"}
	sess := &agent.Session{ID: "sess-1"}

	exec := func(name string, input map[string]any) string {
		t.Helper()
		results, err := builtins.ExecuteBatch(ctx, run, sess, []agent.ToolCall{{
			ID: "call-" + name, Name: name, Input: input,
		}})
		if err != nil {
			t.Fatalf("%s error: %v", name, err)
		}
		return results[0].Content
	}

	// Use a unique key prefix to avoid interference from parallel tests sharing the kvStore.
	keyPrefix := fmt.Sprintf("test_%d_", time.Now().UnixNano())
	testKey := keyPrefix + "greeting"

	// --- db.kv.set ---
	content := exec("db.kv.set", map[string]any{"key": testKey, "value": "hello world"})
	var setResult struct {
		Key     string `json:"key"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal([]byte(content), &setResult); err != nil {
		t.Fatalf("db.kv.set unmarshal: %v", err)
	}
	if setResult.Key != testKey {
		t.Fatalf("db.kv.set key = %q, want %q", setResult.Key, testKey)
	}

	// --- db.kv.get ---
	content = exec("db.kv.get", map[string]any{"key": testKey})
	var getResult struct {
		Key    string `json:"key"`
		Value  string `json:"value"`
		Exists bool   `json:"exists"`
	}
	if err := json.Unmarshal([]byte(content), &getResult); err != nil {
		t.Fatalf("db.kv.get unmarshal: %v", err)
	}
	if !getResult.Exists {
		t.Fatal("db.kv.get exists = false, want true")
	}
	if getResult.Value != "hello world" {
		t.Fatalf("db.kv.get value = %q, want hello world", getResult.Value)
	}

	// --- db.kv.list ---
	content = exec("db.kv.list", map[string]any{"prefix": keyPrefix})
	var listResult struct {
		Keys  []string `json:"keys"`
		Count int      `json:"count"`
	}
	if err := json.Unmarshal([]byte(content), &listResult); err != nil {
		t.Fatalf("db.kv.list unmarshal: %v", err)
	}
	if listResult.Count < 1 {
		t.Fatalf("db.kv.list count = %d, want >= 1", listResult.Count)
	}
	found := false
	for _, k := range listResult.Keys {
		if k == testKey {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("db.kv.list keys = %v, missing %q", listResult.Keys, testKey)
	}

	// --- db.kv.delete ---
	content = exec("db.kv.delete", map[string]any{"key": testKey})
	var deleteResult struct {
		Key     string `json:"key"`
		Deleted bool   `json:"deleted"`
	}
	if err := json.Unmarshal([]byte(content), &deleteResult); err != nil {
		t.Fatalf("db.kv.delete unmarshal: %v", err)
	}
	if !deleteResult.Deleted {
		t.Fatal("db.kv.delete deleted = false, want true")
	}

	// Verify it is gone
	content = exec("db.kv.get", map[string]any{"key": testKey})
	if err := json.Unmarshal([]byte(content), &getResult); err != nil {
		t.Fatalf("db.kv.get after delete unmarshal: %v", err)
	}
	if getResult.Exists {
		t.Fatal("db.kv.get after delete: exists = true, want false")
	}
}

// ---------------------------------------------------------------------------
// 9. TestProcTools
// ---------------------------------------------------------------------------

func TestProcTools(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	builtins := NewBuiltins(BuiltinsConfig{Root: root, MaxReadBytes: 64 * 1024})
	ctx := context.Background()
	run := &agent.Run{ID: "run-1"}
	sess := &agent.Session{ID: "sess-1"}

	exec := func(name string, input map[string]any) string {
		t.Helper()
		results, err := builtins.ExecuteBatch(ctx, run, sess, []agent.ToolCall{{
			ID: "call-" + name, Name: name, Input: input,
		}})
		if err != nil {
			t.Fatalf("%s error: %v", name, err)
		}
		return results[0].Content
	}

	// Start a process that prints output and sleeps
	content := exec("proc.start", map[string]any{
		"command": "sh",
		"args":    []any{"-c", "echo hello && sleep 10"},
	})
	var startResult struct {
		ID      string `json:"id"`
		Command string `json:"command"`
		PID     int    `json:"pid"`
	}
	if err := json.Unmarshal([]byte(content), &startResult); err != nil {
		t.Fatalf("proc.start unmarshal: %v", err)
	}
	if startResult.ID == "" {
		t.Fatal("proc.start id is empty")
	}
	if startResult.Command != "sh" {
		t.Fatalf("proc.start command = %q, want sh", startResult.Command)
	}

	// Wait briefly for output to appear
	time.Sleep(500 * time.Millisecond)

	// --- proc.list ---
	content = exec("proc.list", map[string]any{})
	var listResult struct {
		Processes []map[string]any `json:"processes"`
		Count     int              `json:"count"`
	}
	if err := json.Unmarshal([]byte(content), &listResult); err != nil {
		t.Fatalf("proc.list unmarshal: %v", err)
	}
	if listResult.Count < 1 {
		t.Fatalf("proc.list count = %d, want >= 1", listResult.Count)
	}

	// --- proc.logs ---
	content = exec("proc.logs", map[string]any{
		"id":     startResult.ID,
		"stream": "stdout",
	})
	var logsResult struct {
		ID     string `json:"id"`
		Stdout string `json:"stdout"`
	}
	if err := json.Unmarshal([]byte(content), &logsResult); err != nil {
		t.Fatalf("proc.logs unmarshal: %v", err)
	}
	if !strings.Contains(logsResult.Stdout, "hello") {
		t.Fatalf("proc.logs stdout = %q, want to contain hello", logsResult.Stdout)
	}

	// --- proc.stop ---
	content = exec("proc.stop", map[string]any{"id": startResult.ID})
	var stopResult struct {
		ID      string `json:"id"`
		Stopped bool   `json:"stopped"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal([]byte(content), &stopResult); err != nil {
		t.Fatalf("proc.stop unmarshal: %v", err)
	}
	if !stopResult.Stopped {
		t.Fatalf("proc.stop stopped = false, message = %q", stopResult.Message)
	}
}

// ---------------------------------------------------------------------------
// 10. TestSkillStubs
// ---------------------------------------------------------------------------

func TestSkillStubs(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	builtins := NewBuiltins(BuiltinsConfig{Root: root, MaxReadBytes: 64 * 1024})
	ctx := context.Background()
	run := &agent.Run{ID: "run-1"}
	sess := &agent.Session{ID: "sess-1"}

	exec := func(name string, input map[string]any) string {
		t.Helper()
		results, err := builtins.ExecuteBatch(ctx, run, sess, []agent.ToolCall{{
			ID: "call-" + name, Name: name, Input: input,
		}})
		if err != nil {
			t.Fatalf("%s error: %v", name, err)
		}
		return results[0].Content
	}

	// --- skill.list (no skill service / no ClawHub) ---
	content := exec("skill.list", map[string]any{})
	var listResult map[string]any
	if err := json.Unmarshal([]byte(content), &listResult); err != nil {
		t.Fatalf("skill.list unmarshal: %v", err)
	}
	// Should have installed, loaded keys (all empty/nil when not configured).
	if _, ok := listResult["installed"]; !ok {
		t.Fatal("skill.list should have 'installed' key")
	}
	if _, ok := listResult["loaded"]; !ok {
		t.Fatal("skill.list should have 'loaded' key")
	}

	// --- skill.install (no ClawHub configured) ---
	content = exec("skill.install", map[string]any{"name": "test"})
	var installResult struct {
		Name    string `json:"name"`
		Success bool   `json:"success"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal([]byte(content), &installResult); err != nil {
		t.Fatalf("skill.install unmarshal: %v", err)
	}
	if installResult.Name != "test" {
		t.Fatalf("skill.install name = %q, want test", installResult.Name)
	}
	if installResult.Success {
		t.Fatal("skill.install should not succeed without ClawHub")
	}
	if !strings.Contains(installResult.Message, "not configured") {
		t.Fatalf("skill.install message = %q, want to contain 'not configured'", installResult.Message)
	}
}

func TestSkillListUsesModuleCatalogProjectionWhenAvailable(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	builtins := NewBuiltins(BuiltinsConfig{Root: root, MaxReadBytes: 64 * 1024})
	builtins.ApplyBindings(BuiltinsBindings{
		ModuleCatalog: modules.NewStore(modules.BuildCatalog(modules.SkillModules(skill.RegistrySnapshot{
			Ordered: []*skill.SkillPackage{{
				ID:     "pkg-writer",
				Kind:   skill.SkillKindExecutable,
				Status: skill.StatusReady,
				Trust:  skill.TrustInternal,
				Prompt: skill.PromptSkill{
					Name:        "writer",
					Description: "Write files",
				},
				Source: skill.SkillSource{
					Kind: skill.SourceWorkspace,
					Dir:  "/workspace/skills/writer",
				},
				OpenClaw: skill.OpenClawMetadata{
					SkillKey: "dev.writer",
				},
				ToolManifests: []skill.ToolManifest{{
					Name: "writer.run",
				}},
			}},
		}))),
	})

	results, err := builtins.ExecuteBatch(context.Background(), &agent.Run{ID: "run-1"}, &agent.Session{ID: "sess-1"}, []agent.ToolCall{{
		ID:    "call-skill-list",
		Name:  "skill.list",
		Input: map[string]any{},
	}})
	if err != nil {
		t.Fatalf("skill.list error: %v", err)
	}

	var payload struct {
		Loaded []map[string]any `json:"loaded"`
	}
	if err := json.Unmarshal([]byte(results[0].Content), &payload); err != nil {
		t.Fatalf("skill.list unmarshal: %v", err)
	}
	if len(payload.Loaded) != 1 {
		t.Fatalf("len(loaded) = %d, want 1", len(payload.Loaded))
	}
	if got := strings.TrimSpace(fmt.Sprint(payload.Loaded[0]["name"])); got != "writer" {
		t.Fatalf("loaded[0].name = %q, want writer", got)
	}
	if got := strings.TrimSpace(fmt.Sprint(payload.Loaded[0]["config_key"])); got != "dev.writer" {
		t.Fatalf("loaded[0].config_key = %q, want dev.writer", got)
	}
}
