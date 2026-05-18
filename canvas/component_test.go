package canvas

import (
	"sync"
	"testing"
	"time"
)

func TestComponentRegistry_PushAndGet(t *testing.T) {
	r := NewComponentRegistry()

	components := []Component{
		{ID: "c1", Type: "chart", Props: map[string]any{"title": "Sales"}},
		{ID: "c2", Type: "table", Props: map[string]any{"rows": 10}},
	}

	version := r.Push("session-1", components, false)
	if version != 1 {
		t.Errorf("expected version 1, got %d", version)
	}

	got := r.Get("session-1")
	if len(got) != 2 {
		t.Fatalf("expected 2 components, got %d", len(got))
	}
	if got[0].ID != "c1" {
		t.Errorf("expected component ID c1, got %s", got[0].ID)
	}
}

func TestComponentRegistry_PushAppend(t *testing.T) {
	r := NewComponentRegistry()

	r.Push("s1", []Component{{ID: "c1", Type: "chart"}}, false)
	version := r.Push("s1", []Component{{ID: "c2", Type: "table"}}, false)

	if version != 2 {
		t.Errorf("expected version 2, got %d", version)
	}

	got := r.Get("s1")
	if len(got) != 2 {
		t.Fatalf("expected 2 components, got %d", len(got))
	}
}

func TestComponentRegistry_PushReplace(t *testing.T) {
	r := NewComponentRegistry()

	r.Push("s1", []Component{{ID: "c1"}, {ID: "c2"}}, false)
	version := r.Push("s1", []Component{{ID: "c3"}}, true)

	if version != 2 {
		t.Errorf("expected version 2, got %d", version)
	}

	got := r.Get("s1")
	if len(got) != 1 {
		t.Fatalf("expected 1 component, got %d", len(got))
	}
	if got[0].ID != "c3" {
		t.Errorf("expected component ID c3, got %s", got[0].ID)
	}
}

func TestComponentRegistry_Reset(t *testing.T) {
	r := NewComponentRegistry()

	r.Push("s1", []Component{{ID: "c1"}}, false)
	r.Reset("s1")

	got := r.Get("s1")
	if len(got) != 0 {
		t.Errorf("expected 0 components after reset, got %d", len(got))
	}
}

func TestComponentRegistry_GetEmpty(t *testing.T) {
	r := NewComponentRegistry()

	got := r.Get("nonexistent")
	if got != nil {
		t.Errorf("expected nil for nonexistent session, got %v", got)
	}
}

func TestComponentRegistry_Version(t *testing.T) {
	r := NewComponentRegistry()

	if v := r.Version("s1"); v != 0 {
		t.Errorf("expected version 0 for new session, got %d", v)
	}

	r.Push("s1", []Component{{ID: "c1"}}, false)
	if v := r.Version("s1"); v != 1 {
		t.Errorf("expected version 1, got %d", v)
	}

	r.Reset("s1")
	if v := r.Version("s1"); v != 2 {
		t.Errorf("expected version 2 after reset, got %d", v)
	}
}

func TestComponentRegistry_ConcurrentSafety(t *testing.T) {
	r := NewComponentRegistry()
	var wg sync.WaitGroup

	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			sid := "s1"
			r.Push(sid, []Component{{ID: "c", Type: "chart"}}, false)
			_ = r.Get(sid)
			_ = r.Version(sid)
		}(i)
	}

	wg.Wait()
}

func TestTokenStore_IssueAndValidate(t *testing.T) {
	ts := NewTokenStore(time.Minute)
	defer ts.Stop()

	token, err := ts.Issue("session-1")
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}
	if token.Token == "" {
		t.Fatal("expected non-empty token")
	}

	sessionID, err := ts.Validate(token.Token)
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if sessionID != "session-1" {
		t.Errorf("expected session-1, got %s", sessionID)
	}
}

func TestTokenStore_InvalidToken(t *testing.T) {
	ts := NewTokenStore(time.Minute)
	defer ts.Stop()

	_, err := ts.Validate("nonexistent")
	if err != ErrTokenInvalid {
		t.Errorf("expected ErrTokenInvalid, got %v", err)
	}
}

func TestTokenStore_ExpiredToken(t *testing.T) {
	ts := NewTokenStore(time.Millisecond)
	defer ts.Stop()

	token, err := ts.Issue("session-1")
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}

	time.Sleep(5 * time.Millisecond)

	_, err = ts.Validate(token.Token)
	if err != ErrTokenExpired {
		t.Errorf("expected ErrTokenExpired, got %v", err)
	}
}

func TestTokenStore_EmptySession(t *testing.T) {
	ts := NewTokenStore(time.Minute)
	defer ts.Stop()

	_, err := ts.Issue("")
	if err != ErrSessionEmpty {
		t.Errorf("expected ErrSessionEmpty, got %v", err)
	}
}

func TestTokenStore_ConcurrentSafety(t *testing.T) {
	ts := NewTokenStore(time.Minute)
	defer ts.Stop()

	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			token, _ := ts.Issue("s1")
			if token.Token != "" {
				_, _ = ts.Validate(token.Token)
			}
		}()
	}
	wg.Wait()
}
