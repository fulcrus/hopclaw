package approval

import "testing"

func TestResourceScopeFromToolCallCapturesPathsHostsCommandsAndParams(t *testing.T) {
	t.Parallel()

	scope := ResourceScopeFromToolCall("exec.run", map[string]any{
		"command": "git",
		"args":    []any{"status", "-sb"},
		"dir":     "./repo",
		"url":     "https://api.example.com/v1/status",
		"method":  "POST",
	})
	if len(scope.PathPrefixes) != 1 || scope.PathPrefixes[0] != "repo" {
		t.Fatalf("scope.PathPrefixes = %#v", scope.PathPrefixes)
	}
	if len(scope.Hosts) != 1 || scope.Hosts[0] != "api.example.com" {
		t.Fatalf("scope.Hosts = %#v", scope.Hosts)
	}
	if len(scope.CommandPrefixes) != 1 || scope.CommandPrefixes[0] != "git status -sb" {
		t.Fatalf("scope.CommandPrefixes = %#v", scope.CommandPrefixes)
	}
	if got := scope.Parameters["method"]; len(got) != 1 || got[0] != "POST" {
		t.Fatalf("scope.Parameters = %#v", scope.Parameters)
	}
	if scope.Summary == "" {
		t.Fatal("expected summary to be populated")
	}
}

func TestResourceScopeMatchesNestedPathsAndHosts(t *testing.T) {
	t.Parallel()

	granted := ResourceScope{
		PathPrefixes: []string{"reports"},
		Hosts:        []string{"api.example.com"},
		Parameters:   map[string][]string{"method": {"POST"}},
	}
	request := ResourceScopeFromToolCall("net.http", map[string]any{
		"url":    "https://api.example.com/v1/deploy",
		"method": "POST",
		"path":   "reports/daily/out.txt",
	})
	if !granted.Matches(request) {
		t.Fatalf("granted scope should match request: granted=%+v request=%+v", granted, request)
	}
	if granted.Matches(ResourceScopeFromToolCall("net.http", map[string]any{
		"url":    "https://other.example.com/v1/deploy",
		"method": "POST",
		"path":   "reports/daily/out.txt",
	})) {
		t.Fatal("host mismatch should not match")
	}
	if granted.Matches(ResourceScopeFromToolCall("net.http", map[string]any{
		"url":    "https://api.example.com/v1/deploy",
		"method": "GET",
		"path":   "reports/daily/out.txt",
	})) {
		t.Fatal("parameter mismatch should not match")
	}
}

func TestResourceScopeNormalizesWindowsStylePathsAndMatchesCaseInsensitively(t *testing.T) {
	t.Parallel()

	scope := ResourceScopeFromToolCall("fs.write", map[string]any{
		"path": `C:\Work\Reports\Daily.txt`,
	})
	if len(scope.PathPrefixes) != 1 || scope.PathPrefixes[0] != "c:/work/reports/daily.txt" {
		t.Fatalf("scope.PathPrefixes = %#v", scope.PathPrefixes)
	}

	granted := ResourceScope{
		PathPrefixes: []string{`C:\Work\Reports`},
	}
	if !granted.MatchesCall("fs.write", map[string]any{"path": `c:/work/reports/daily.txt`}) {
		t.Fatal("expected windows path scope to match normalized child path")
	}
	if granted.MatchesCall("fs.write", map[string]any{"path": `c:/work/reporting/daily.txt`}) {
		t.Fatal("unexpected match for out-of-scope windows path")
	}

	relative := ResourceScopeFromToolCall("fs.write", map[string]any{
		"path": `Reports\Daily.txt`,
	})
	if len(relative.PathPrefixes) != 1 || relative.PathPrefixes[0] != "reports/daily.txt" {
		t.Fatalf("relative.PathPrefixes = %#v", relative.PathPrefixes)
	}
}

func TestResourceScopeExecScriptRequiresMatchingScriptDigest(t *testing.T) {
	t.Parallel()

	scope := ResourceScopeFromToolCall("exec.script", map[string]any{
		"interpreter": "/bin/sh",
		"script":      "echo hello\n",
	})
	if len(scope.CommandPrefixes) != 1 || scope.CommandPrefixes[0] != "/bin/sh" {
		t.Fatalf("scope.CommandPrefixes = %#v", scope.CommandPrefixes)
	}
	if got := scope.Parameters["script_sha256"]; len(got) != 1 || got[0] == "" {
		t.Fatalf("scope.Parameters = %#v", scope.Parameters)
	}
	if !scope.MatchesCall("exec.script", map[string]any{
		"interpreter": "/bin/sh",
		"script":      "echo hello\n",
	}) {
		t.Fatal("expected identical script to match granted scope")
	}
	if scope.MatchesCall("exec.script", map[string]any{
		"interpreter": "/bin/sh",
		"script":      "echo goodbye\n",
	}) {
		t.Fatal("changed script content should not match granted scope")
	}
	if scope.MatchesCall("exec.script", map[string]any{
		"interpreter": "python3",
		"script":      "echo hello\n",
	}) {
		t.Fatal("different interpreter should not match granted scope")
	}
}

func TestResourceScopeProcStartCapturesCommandPrefix(t *testing.T) {
	t.Parallel()

	scope := ResourceScopeFromToolCall("proc.start", map[string]any{
		"command": "python3",
		"args":    []any{"worker.py", "--port", "8080"},
	})
	if len(scope.CommandPrefixes) != 1 || scope.CommandPrefixes[0] != "python3 worker.py --port 8080" {
		t.Fatalf("scope.CommandPrefixes = %#v", scope.CommandPrefixes)
	}
	if !scope.MatchesCall("proc.start", map[string]any{
		"command": "python3",
		"args":    []any{"worker.py", "--port", "8080", "--verbose"},
	}) {
		t.Fatal("expected proc.start grant to match the approved command prefix")
	}
	if scope.MatchesCall("proc.start", map[string]any{
		"command": "python3",
		"args":    []any{"other.py"},
	}) {
		t.Fatal("different proc.start command should not match granted scope")
	}
}

func TestResourceScopeProcStopMatchesOnlyApprovedProcessID(t *testing.T) {
	t.Parallel()

	scope := ResourceScopeFromToolCall("proc.stop", map[string]any{
		"id": "proc-123",
	})
	if got := scope.Parameters["process_id"]; len(got) != 1 || got[0] != "proc-123" {
		t.Fatalf("scope.Parameters = %#v", scope.Parameters)
	}
	if !scope.MatchesCall("proc.stop", map[string]any{"id": "proc-123"}) {
		t.Fatal("expected proc.stop grant to match approved process id")
	}
	if scope.MatchesCall("proc.stop", map[string]any{"id": "proc-999"}) {
		t.Fatal("different process id should not match granted proc.stop scope")
	}
}
