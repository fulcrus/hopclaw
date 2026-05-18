package acp

import (
	"fmt"
	"testing"
)

// ---------------------------------------------------------------------------
// SessionStore
// ---------------------------------------------------------------------------

func TestSessionStoreGetOrCreate(t *testing.T) {
	t.Parallel()

	store := &SessionStore{
		sessions: make(map[string]*session),
		done:     make(chan struct{}),
	}

	sess := store.GetOrCreate("s1", "gw-1", "/tmp")
	if sess.ID != "s1" {
		t.Fatalf("ID = %q, want %q", sess.ID, "s1")
	}
	if sess.GatewayKey != "gw-1" {
		t.Fatalf("GatewayKey = %q, want %q", sess.GatewayKey, "gw-1")
	}
	if sess.CWD != "/tmp" {
		t.Fatalf("CWD = %q, want %q", sess.CWD, "/tmp")
	}
	if sess.CreatedAt.IsZero() {
		t.Fatal("CreatedAt is zero")
	}

	// Getting the same ID returns the same session.
	sess2 := store.GetOrCreate("s1", "gw-other", "/other")
	if sess2.GatewayKey != "gw-1" {
		t.Fatalf("expected existing session, GatewayKey = %q", sess2.GatewayKey)
	}
}

func TestSessionStoreGet(t *testing.T) {
	t.Parallel()

	store := &SessionStore{
		sessions: make(map[string]*session),
		done:     make(chan struct{}),
	}

	_, ok := store.Get("missing")
	if ok {
		t.Fatal("expected not found for missing session")
	}

	store.GetOrCreate("s1", "gw-1", "")
	sess, ok := store.Get("s1")
	if !ok {
		t.Fatal("expected session to be found")
	}
	if sess.ID != "s1" {
		t.Fatalf("ID = %q", sess.ID)
	}
}

func TestSessionStoreTouch(t *testing.T) {
	t.Parallel()

	store := &SessionStore{
		sessions: make(map[string]*session),
		done:     make(chan struct{}),
	}

	sess := store.GetOrCreate("s1", "gw-1", "")
	originalTime := sess.LastTouchedAt

	store.Touch("s1")
	if sess.LastTouchedAt.Before(originalTime) {
		t.Fatal("LastTouchedAt was not updated")
	}

	// Touch non-existent session does not panic.
	store.Touch("nonexistent")
}

func TestSessionStoreSetAndClearActiveRun(t *testing.T) {
	t.Parallel()

	store := &SessionStore{
		sessions: make(map[string]*session),
		done:     make(chan struct{}),
	}

	store.GetOrCreate("s1", "gw-1", "")

	cancelled := false
	store.SetActiveRun("s1", "run-1", func() { cancelled = true })

	sess, _ := store.Get("s1")
	if sess.ActiveRunID != "run-1" {
		t.Fatalf("ActiveRunID = %q, want %q", sess.ActiveRunID, "run-1")
	}

	store.ClearActiveRun("s1")
	if sess.ActiveRunID != "" {
		t.Fatalf("ActiveRunID = %q after clear, want empty", sess.ActiveRunID)
	}
	if sess.Cancel != nil {
		t.Fatal("Cancel should be nil after clear")
	}
	// The cancel function should NOT have been called by ClearActiveRun.
	if cancelled {
		t.Fatal("cancel should not be invoked by ClearActiveRun")
	}
}

func TestSessionStoreClearActiveRunIfMatchPreservesNewerRun(t *testing.T) {
	t.Parallel()

	store := &SessionStore{
		sessions: make(map[string]*session),
		done:     make(chan struct{}),
	}

	store.GetOrCreate("s1", "gw-1", "")
	store.SetActiveRun("s1", "run-1", nil)
	store.SetActiveRun("s1", "run-2", nil)

	store.ClearActiveRunIfMatch("s1", "run-1")

	sess, _ := store.Get("s1")
	if sess.ActiveRunID != "run-2" {
		t.Fatalf("ActiveRunID = %q, want %q", sess.ActiveRunID, "run-2")
	}

	store.ClearActiveRunIfMatch("s1", "run-2")
	if sess.ActiveRunID != "" {
		t.Fatalf("ActiveRunID = %q after matched clear, want empty", sess.ActiveRunID)
	}
}

func TestSessionStoreSetConfigOption(t *testing.T) {
	t.Parallel()

	store := &SessionStore{
		sessions: make(map[string]*session),
		done:     make(chan struct{}),
	}

	store.GetOrCreate("s1", "gw-1", "")
	store.SetConfigOption("s1", ConfigThoughtLevel, "high")
	store.SetConfigOption("s1", ConfigVerboseLevel, "debug")

	opts := store.ConfigOptions("s1")
	if opts[ConfigThoughtLevel] != "high" {
		t.Fatalf("ConfigThoughtLevel = %q, want %q", opts[ConfigThoughtLevel], "high")
	}
	if opts[ConfigVerboseLevel] != "debug" {
		t.Fatalf("ConfigVerboseLevel = %q, want %q", opts[ConfigVerboseLevel], "debug")
	}

	// Config options for missing session returns nil.
	if store.ConfigOptions("missing") != nil {
		t.Fatal("expected nil for missing session")
	}
}

func TestSessionStoreList(t *testing.T) {
	t.Parallel()

	store := &SessionStore{
		sessions: make(map[string]*session),
		done:     make(chan struct{}),
	}

	store.GetOrCreate("s1", "gw-1", "")
	store.GetOrCreate("s2", "gw-2", "")
	store.SetActiveRun("s2", "run-x", nil)

	all := store.List(0, 0)
	if len(all) != 2 {
		t.Fatalf("List() returned %d items, want 2", len(all))
	}

	// Pagination: limit.
	limited := store.List(1, 0)
	if len(limited) != 1 {
		t.Fatalf("List(limit=1) returned %d items, want 1", len(limited))
	}

	// Pagination: offset beyond range.
	empty := store.List(0, 100)
	if len(empty) != 0 {
		t.Fatalf("List(offset=100) returned %d items, want 0", len(empty))
	}
}

func TestSessionStoreRemove(t *testing.T) {
	t.Parallel()

	store := &SessionStore{
		sessions: make(map[string]*session),
		done:     make(chan struct{}),
	}

	cancelled := false
	store.GetOrCreate("s1", "gw-1", "")
	store.SetActiveRun("s1", "run-1", func() { cancelled = true })

	store.Remove("s1")
	if !cancelled {
		t.Fatal("expected cancel to be invoked on Remove")
	}

	_, ok := store.Get("s1")
	if ok {
		t.Fatal("session should be removed")
	}

	// Remove non-existent does not panic.
	store.Remove("nonexistent")
}

// ---------------------------------------------------------------------------
// Transport
// ---------------------------------------------------------------------------

func TestTransportClosedSend(t *testing.T) {
	t.Parallel()

	tr := NewTransport(nil, nil)
	tr.Close()

	err := tr.Send(&JSONRPCMessage{JSONRPC: jsonrpcVersion})
	if err == nil {
		t.Fatal("expected error on closed transport")
	}
}

func TestTransportClosedReceive(t *testing.T) {
	t.Parallel()

	tr := NewTransport(nil, nil)
	tr.Close()

	_, err := tr.Receive()
	if err == nil {
		t.Fatal("expected error on closed transport")
	}
}

// ---------------------------------------------------------------------------
// toInt64
// ---------------------------------------------------------------------------

func TestToInt64(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		input   any
		wantVal int64
		wantOK  bool
	}{
		{"float64", float64(42), 42, true},
		{"int64", int64(99), 99, true},
		{"int", int(7), 7, true},
		{"string", "abc", 0, false},
		{"nil", nil, 0, false},
		{"bool", true, 0, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			val, ok := toInt64(tt.input)
			if ok != tt.wantOK {
				t.Fatalf("ok = %v, want %v", ok, tt.wantOK)
			}
			if ok && val != tt.wantVal {
				t.Fatalf("val = %d, want %d", val, tt.wantVal)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// isEOF
// ---------------------------------------------------------------------------

func TestIsEOF(t *testing.T) {
	t.Parallel()

	if isEOF(nil) {
		t.Fatal("nil should not be EOF")
	}
	if !isEOF(fmt.Errorf("acp: failed to read message: EOF")) {
		t.Fatal("expected true for matching EOF string")
	}
}

// ---------------------------------------------------------------------------
// defaultCommands
// ---------------------------------------------------------------------------

func TestDefaultCommandsNonEmpty(t *testing.T) {
	t.Parallel()

	cmds := defaultCommands()
	if len(cmds) == 0 {
		t.Fatal("expected non-empty default commands")
	}
	// Verify each has a name and description.
	for _, cmd := range cmds {
		if cmd.Name == "" {
			t.Fatal("command with empty name")
		}
		if cmd.Description == "" {
			t.Fatalf("command %q has empty description", cmd.Name)
		}
	}
}

// ---------------------------------------------------------------------------
// NeedsPermission
// ---------------------------------------------------------------------------

func TestNeedsPermissionSafeTools(t *testing.T) {
	t.Parallel()

	safe := []string{
		"fs.read", "fs.list", "fs.tree", "fs.find", "fs.grep",
		"fs.stat", "fs.hash", "env.probe", "env.info", "env.get",
		"text.count", "text.extract", "skill.list", "net.dns",
	}
	for _, tool := range safe {
		if NeedsPermission(tool) {
			t.Fatalf("%q should not need permission", tool)
		}
	}
}

func TestNeedsPermissionDangerousTools(t *testing.T) {
	t.Parallel()

	dangerous := []string{"exec.shell", "fs.write", "fs.delete", "db.execute", "unknown.tool"}
	for _, tool := range dangerous {
		if !NeedsPermission(tool) {
			t.Fatalf("%q should need permission", tool)
		}
	}
}
