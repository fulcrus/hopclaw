package watch

import (
	"bufio"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/fulcrus/hopclaw/agent"
	"github.com/fulcrus/hopclaw/automation"
	browserclient "github.com/fulcrus/hopclaw/browserapi/client"
	browsertypes "github.com/fulcrus/hopclaw/browserapi/types"
	"github.com/fulcrus/hopclaw/contextengine"
	runtimesvc "github.com/fulcrus/hopclaw/runtime"
	verifyrt "github.com/fulcrus/hopclaw/runtime/verify"
)

type mockSubmitter struct {
	mu           sync.Mutex
	requests     []automation.SubmitRequest
	result       *runtimesvc.RunResult
	getResults   []*runtimesvc.RunResult
	verification *verifyrt.RunVerification
	verifyErr    error
	err          error
	getErr       error
}

type mockChannelDeliverer struct {
	mu    sync.Mutex
	calls []deliverCall
	err   error
}

type deliverCall struct {
	Channel string
	Target  string
	Content string
}

func (m *mockSubmitter) Submit(_ context.Context, req automation.SubmitRequest) (*runtimesvc.RunResult, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.requests = append(m.requests, req)
	return m.result, m.err
}

func (m *mockSubmitter) GetRunResult(_ context.Context, runID string) (*runtimesvc.RunResult, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.getErr != nil {
		return nil, m.getErr
	}
	if len(m.getResults) > 0 {
		result := m.getResults[0]
		if len(m.getResults) > 1 {
			m.getResults = m.getResults[1:]
		}
		if result != nil && result.RunID == "" {
			result.RunID = runID
		}
		return result, nil
	}
	result := m.result
	if result != nil && result.RunID == "" {
		result.RunID = runID
	}
	return result, nil
}

func (m *mockSubmitter) Requests() []automation.SubmitRequest {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]automation.SubmitRequest, len(m.requests))
	copy(out, m.requests)
	return out
}

func (m *mockSubmitter) GetRunVerification(_ context.Context, runID string) (*verifyrt.RunVerification, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.verifyErr != nil {
		return nil, m.verifyErr
	}
	if m.verification == nil {
		return nil, nil
	}
	copyVerification := *m.verification
	if copyVerification.RunID == "" {
		copyVerification.RunID = runID
	}
	return &copyVerification, nil
}

func (m *mockChannelDeliverer) DeliverMessage(_ context.Context, target automation.DeliveryTarget, content string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.calls = append(m.calls, deliverCall{
		Channel: target.Channel,
		Target:  target.Target,
		Content: content,
	})
	return m.err
}

func (m *mockChannelDeliverer) Calls() []deliverCall {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]deliverCall, len(m.calls))
	copy(out, m.calls)
	return out
}

func testStore(t *testing.T) (*Store, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "watch.json")
	if err := os.WriteFile(path, []byte(`{"version":1,"watches":[]}`), 0o644); err != nil {
		t.Fatalf("write watch file: %v", err)
	}
	store, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	return store, path
}

func TestValidateRejectsInvalidSource(t *testing.T) {
	t.Parallel()
	err := Validate(Watch{Interval: "1m", Source: Source{Kind: "unknown"}})
	if !errors.Is(err, ErrInvalidSource) {
		t.Fatalf("Validate() error = %v", err)
	}
}

func TestValidateAcceptsCalendarSource(t *testing.T) {
	t.Parallel()
	err := Validate(Watch{Interval: "1m", Source: Source{Kind: SourceKindCalendar, Calendar: &CalendarSource{Query: "standup", Limit: 5}}})
	if err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
}

func TestValidateAcceptsStructuredInboxSource(t *testing.T) {
	t.Parallel()
	err := Validate(Watch{Interval: "1m", Source: Source{Kind: SourceKindStructuredInbox, StructuredInbox: &StructuredInboxSource{SessionKey: "slack:C123", Limit: 10}}})
	if err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
}

func TestValidateAcceptsWebhookSource(t *testing.T) {
	t.Parallel()
	err := Validate(Watch{Interval: "1m", Source: Source{Kind: SourceKindWebhook, Webhook: &WebhookSource{WebhookID: "demo", SenderID: "user-1", Limit: 10}}})
	if err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
}

func TestServiceSupportsFileSource(t *testing.T) {
	t.Parallel()

	store, _ := testStore(t)
	filePath := filepath.Join(t.TempDir(), "watched.txt")
	if err := os.WriteFile(filePath, []byte("first version"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	now := time.Now().UTC()
	item := Watch{
		ID:          "watch-file",
		Name:        "file",
		Enabled:     true,
		Interval:    "1m",
		FireOnStart: true,
		Source:      Source{Kind: SourceKindFile, File: &FileSource{Path: filePath}},
		NextCheckAt: now.Add(-time.Second),
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	if err := store.Add(item); err != nil {
		t.Fatalf("Add() error = %v", err)
	}
	submitter := &mockSubmitter{result: &runtimesvc.RunResult{RunID: "run-file", Status: "completed", Summary: "file changed"}}
	svc := NewService(store, submitter)

	svc.fireDue(context.Background())

	got, err := store.Get("watch-file")
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if got.LastStatus != RunStatusTriggered {
		t.Fatalf("LastStatus = %q, want %q", got.LastStatus, RunStatusTriggered)
	}
	if got.LastRunID != "run-file" {
		t.Fatalf("LastRunID = %q", got.LastRunID)
	}
	if got.LastResult == nil {
		t.Fatal("expected LastResult to be populated")
	}
	if got.LastResult.Status != "triggered" || got.LastResult.RunID != "run-file" {
		t.Fatalf("LastResult = %#v", got.LastResult)
	}
}

func TestServiceSupportsFeedSource(t *testing.T) {
	t.Parallel()

	store, _ := testStore(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`<?xml version="1.0"?><rss><channel><item><guid>1</guid><title>Hello</title><pubDate>Mon, 01 Jan 2024 00:00:00 GMT</pubDate></item></channel></rss>`))
	}))
	defer server.Close()

	now := time.Now().UTC()
	item := Watch{
		ID:          "watch-feed",
		Name:        "feed",
		Enabled:     true,
		Interval:    "1m",
		FireOnStart: true,
		Source:      Source{Kind: SourceKindFeed, Feed: &FeedSource{URL: server.URL}},
		NextCheckAt: now.Add(-time.Second),
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	if err := store.Add(item); err != nil {
		t.Fatalf("Add() error = %v", err)
	}
	submitter := &mockSubmitter{result: &runtimesvc.RunResult{RunID: "run-feed", Status: "completed", Summary: "feed changed"}}
	svc := NewService(store, submitter)
	svc.fireDue(context.Background())
	got, err := store.Get("watch-feed")
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if got.LastStatus != RunStatusTriggered {
		t.Fatalf("LastStatus = %q, want %q", got.LastStatus, RunStatusTriggered)
	}
}

func TestServiceSupportsMailboxSource(t *testing.T) {
	t.Parallel()

	store, _ := testStore(t)
	imap := newFakeWatchIMAPServer(t, []fakeWatchIMAPMessage{{
		UID: "101", Subject: "Alert", From: "ops@example.com", Date: "Mon, 01 Jan 2024 00:00:00 GMT",
	}})
	defer imap.Close()

	now := time.Now().UTC()
	item := Watch{
		ID:          "watch-mailbox",
		Name:        "mailbox",
		Enabled:     true,
		Interval:    "1m",
		FireOnStart: true,
		Source:      Source{Kind: SourceKindMailbox, Mailbox: &MailboxSource{Folder: "INBOX", Query: "Alert"}},
		NextCheckAt: now.Add(-time.Second),
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	if err := store.Add(item); err != nil {
		t.Fatalf("Add() error = %v", err)
	}
	submitter := &mockSubmitter{result: &runtimesvc.RunResult{RunID: "run-mailbox", Status: "completed", Summary: "mailbox changed"}}
	svc := NewService(store, submitter, WithEmailConfig(EmailConfig{
		IMAPHost: imap.Host,
		IMAPPort: imap.Port,
		Username: "test@example.com",
		Password: "secret",
	}))
	svc.fireDue(context.Background())
	got, err := store.Get("watch-mailbox")
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if got.LastStatus != RunStatusTriggered {
		t.Fatalf("LastStatus = %q, want %q", got.LastStatus, RunStatusTriggered)
	}
}

func TestServiceCalendarSourceRequiresConfig(t *testing.T) {
	t.Parallel()

	store, _ := testStore(t)
	now := time.Now().UTC()
	item := Watch{
		ID:          "watch-calendar",
		Name:        "calendar",
		Enabled:     true,
		Interval:    "1m",
		FireOnStart: true,
		Source:      Source{Kind: SourceKindCalendar, Calendar: &CalendarSource{Query: "standup", Limit: 5}},
		NextCheckAt: now.Add(-time.Second),
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	if err := store.Add(item); err != nil {
		t.Fatalf("Add() error = %v", err)
	}
	svc := NewService(store, &mockSubmitter{})
	svc.fireDue(context.Background())
	got, err := store.Get("watch-calendar")
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if got.LastStatus != RunStatusError {
		t.Fatalf("LastStatus = %q, want %q", got.LastStatus, RunStatusError)
	}
	if !strings.Contains(got.LastError, "CalDAV") && !strings.Contains(strings.ToLower(got.LastError), "calendar") {
		t.Fatalf("LastError = %q", got.LastError)
	}
}

func TestServiceSupportsStructuredInboxSource(t *testing.T) {
	t.Parallel()

	store, _ := testStore(t)
	sessions := agent.NewInMemorySessionStore()
	session, err := sessions.GetOrCreate(context.Background(), "slack:C123", "test-model")
	if err != nil {
		t.Fatalf("GetOrCreate() error = %v", err)
	}
	lock, loaded, err := mustLoadSessionForTest(context.Background(), sessions, session.ID)
	if err != nil {
		t.Fatalf("LoadForExecution() error = %v", err)
	}
	loaded.Messages = append(loaded.Messages, contextengine.Message{
		Role:      contextengine.RoleUser,
		Content:   "first inbound",
		CreatedAt: time.Date(2026, 3, 16, 10, 0, 0, 0, time.UTC),
	})
	if saveErr := sessions.Save(context.Background(), loaded); saveErr != nil {
		lock()
		t.Fatalf("Save() error = %v", saveErr)
	}
	lock()

	now := time.Now().UTC()
	item := Watch{
		ID:          "watch-structured-inbox",
		Name:        "structured inbox",
		Enabled:     true,
		Interval:    "1m",
		FireOnStart: true,
		Source:      Source{Kind: SourceKindStructuredInbox, StructuredInbox: &StructuredInboxSource{SessionKey: "slack:C123", Limit: 10}},
		NextCheckAt: now.Add(-time.Second),
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	if err := store.Add(item); err != nil {
		t.Fatalf("Add() error = %v", err)
	}
	submitter := &mockSubmitter{result: &runtimesvc.RunResult{RunID: "run-structured-inbox", Status: "completed", Summary: "inbox changed"}}
	svc := NewService(store, submitter, WithSessionInboxReader(sessions))

	svc.fireDue(context.Background())

	got, err := store.Get("watch-structured-inbox")
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if got.LastStatus != RunStatusTriggered {
		t.Fatalf("LastStatus = %q, want %q", got.LastStatus, RunStatusTriggered)
	}
	requests := submitter.Requests()
	if len(requests) != 1 || !strings.Contains(requests[0].Content, "first inbound") {
		t.Fatalf("watch prompt did not include inbox content: %+v", requests)
	}
}

func TestServiceSupportsWebhookInboxSource(t *testing.T) {
	t.Parallel()

	store, _ := testStore(t)
	sessions := agent.NewInMemorySessionStore()
	session, err := sessions.GetOrCreate(context.Background(), "webhook:demo:user-1", "test-model")
	if err != nil {
		t.Fatalf("GetOrCreate() error = %v", err)
	}
	lock, loaded, err := mustLoadSessionForTest(context.Background(), sessions, session.ID)
	if err != nil {
		t.Fatalf("LoadForExecution() error = %v", err)
	}
	loaded.Messages = append(loaded.Messages, contextengine.Message{
		Role:      contextengine.RoleUser,
		Content:   "payload changed",
		CreatedAt: time.Date(2026, 3, 16, 11, 0, 0, 0, time.UTC),
	})
	if saveErr := sessions.Save(context.Background(), loaded); saveErr != nil {
		lock()
		t.Fatalf("Save() error = %v", saveErr)
	}
	lock()

	now := time.Now().UTC()
	item := Watch{
		ID:          "watch-webhook-inbox",
		Name:        "webhook inbox",
		Enabled:     true,
		Interval:    "1m",
		FireOnStart: true,
		Source:      Source{Kind: SourceKindWebhook, Webhook: &WebhookSource{WebhookID: "demo", SenderID: "user-1", Limit: 10}},
		NextCheckAt: now.Add(-time.Second),
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	if err := store.Add(item); err != nil {
		t.Fatalf("Add() error = %v", err)
	}
	submitter := &mockSubmitter{result: &runtimesvc.RunResult{RunID: "run-webhook-inbox", Status: "completed", Summary: "webhook changed"}}
	svc := NewService(store, submitter, WithSessionInboxReader(sessions))

	svc.fireDue(context.Background())

	got, err := store.Get("watch-webhook-inbox")
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if got.LastStatus != RunStatusTriggered {
		t.Fatalf("LastStatus = %q, want %q", got.LastStatus, RunStatusTriggered)
	}
	requests := submitter.Requests()
	if len(requests) != 1 || !strings.Contains(requests[0].Content, "payload changed") {
		t.Fatalf("watch prompt did not include webhook inbox content: %+v", requests)
	}
}

func mustLoadSessionForTest(ctx context.Context, sessions *agent.InMemorySessionStore, sessionID string) (func(), *agent.Session, error) {
	session, unlock, err := sessions.LoadForExecution(ctx, sessionID)
	if err != nil {
		return nil, nil, err
	}
	return unlock, session, nil
}

func TestServiceSupportsBrowserSnapshotSource(t *testing.T) {
	t.Parallel()

	store, _ := testStore(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/browser/v1":
			var req browsertypes.Request
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				t.Fatalf("Decode() error = %v", err)
			}
			switch req.Action {
			case browsertypes.ActionCreateSession:
				_ = json.NewEncoder(w).Encode(browsertypes.Response{OK: true, SessionID: "sess-1"})
			case browsertypes.ActionSnapshot:
				_ = json.NewEncoder(w).Encode(browsertypes.Response{OK: true, Data: map[string]any{"html": "<html><body>Hello</body></html>"}})
			case browsertypes.ActionCloseSession:
				_ = json.NewEncoder(w).Encode(browsertypes.Response{OK: true})
			default:
				http.Error(w, "bad action", http.StatusBadRequest)
			}
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	now := time.Now().UTC()
	item := Watch{
		ID:          "watch-browser",
		Name:        "browser",
		Enabled:     true,
		Interval:    "1m",
		FireOnStart: true,
		Source:      Source{Kind: SourceKindBrowserSnapshot, BrowserSnapshot: &BrowserSnapshotSource{URL: "https://example.com/page"}},
		NextCheckAt: now.Add(-time.Second),
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	if err := store.Add(item); err != nil {
		t.Fatalf("Add() error = %v", err)
	}
	submitter := &mockSubmitter{result: &runtimesvc.RunResult{RunID: "run-browser", Status: "completed", Summary: "browser changed"}}
	svc := NewService(store, submitter, WithBrowserClient(browserclient.New(server.URL)))
	svc.fireDue(context.Background())
	got, err := store.Get("watch-browser")
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if got.LastStatus != RunStatusTriggered {
		t.Fatalf("LastStatus = %q, want %q", got.LastStatus, RunStatusTriggered)
	}
}

func TestServicePrimesFingerprintWithoutTrigger(t *testing.T) {
	t.Parallel()

	store, _ := testStore(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("hello world"))
	}))
	defer server.Close()

	item := Watch{
		ID:          "watch-1",
		Name:        "page",
		Enabled:     true,
		Interval:    "1m",
		Source:      Source{Kind: SourceKindHTTP, HTTP: &HTTPSource{URL: server.URL}},
		NextCheckAt: time.Now().UTC().Add(-time.Second),
		CreatedAt:   time.Now().UTC(),
		UpdatedAt:   time.Now().UTC(),
	}
	if err := store.Add(item); err != nil {
		t.Fatalf("Add() error = %v", err)
	}
	submitter := &mockSubmitter{}
	svc := NewService(store, submitter)

	svc.fireDue(context.Background())

	reqs := submitter.Requests()
	if len(reqs) != 0 {
		t.Fatalf("expected 0 submit, got %d", len(reqs))
	}
	got, err := store.Get("watch-1")
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if got.LastStatus != RunStatusPrimed {
		t.Fatalf("LastStatus = %q, want %q", got.LastStatus, RunStatusPrimed)
	}
	if got.LastFingerprint == "" {
		t.Fatal("expected LastFingerprint to be populated")
	}
}

func TestServiceTriggersRunOnChange(t *testing.T) {
	t.Parallel()

	store, _ := testStore(t)
	var mu sync.Mutex
	body := "version one"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		_, _ = w.Write([]byte(body))
	}))
	defer server.Close()

	now := time.Now().UTC()
	item := Watch{
		ID:          "watch-2",
		Name:        "page",
		Enabled:     true,
		Interval:    "1m",
		Source:      Source{Kind: SourceKindHTTP, HTTP: &HTTPSource{URL: server.URL}},
		NextCheckAt: now.Add(-time.Second),
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	if err := store.Add(item); err != nil {
		t.Fatalf("Add() error = %v", err)
	}
	submitter := &mockSubmitter{result: &runtimesvc.RunResult{RunID: "run-1", Status: "completed", Summary: "change detected"}}
	svc := NewService(store, submitter)

	svc.fireDue(context.Background())

	mu.Lock()
	body = "version two"
	mu.Unlock()
	if err := store.Update("watch-2", func(w *Watch) {
		w.NextCheckAt = time.Now().UTC().Add(-time.Second)
	}); err != nil {
		t.Fatalf("Update() error = %v", err)
	}
	svc.fireDue(context.Background())

	reqs := submitter.Requests()
	if len(reqs) != 1 {
		t.Fatalf("expected 1 submit, got %d", len(reqs))
	}
	if reqs[0].SessionKey != "watch:watch-2" {
		t.Fatalf("SessionKey = %q, want %q", reqs[0].SessionKey, "watch:watch-2")
	}
	got, err := store.Get("watch-2")
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if got.LastStatus != RunStatusTriggered {
		t.Fatalf("LastStatus = %q, want %q", got.LastStatus, RunStatusTriggered)
	}
	if got.LastRunID != "run-1" {
		t.Fatalf("LastRunID = %q, want %q", got.LastRunID, "run-1")
	}
	if got.LastSummary != "change detected" {
		t.Fatalf("LastSummary = %q", got.LastSummary)
	}
}

func TestServiceDeliversTriggeredWatchResult(t *testing.T) {
	t.Parallel()

	store, _ := testStore(t)
	filePath := filepath.Join(t.TempDir(), "watch-delivery.txt")
	if err := os.WriteFile(filePath, []byte("changed body"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	now := time.Now().UTC()
	item := Watch{
		ID:          "watch-delivery",
		Name:        "delivery watch",
		Enabled:     true,
		Interval:    "1m",
		FireOnStart: true,
		Source:      Source{Kind: SourceKindFile, File: &FileSource{Path: filePath}},
		Delivery:    &DeliveryTarget{Channel: "feishu", Target: "oc_alerts"},
		NextCheckAt: now.Add(-time.Second),
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	if err := store.Add(item); err != nil {
		t.Fatalf("Add() error = %v", err)
	}
	submitter := &mockSubmitter{result: &runtimesvc.RunResult{
		RunID:   "run-watch-delivery",
		Status:  "completed",
		Summary: "international update detected",
		Output:  "Oil moved higher after the latest announcement.",
	}}
	deliverer := &mockChannelDeliverer{}
	svc := NewService(store, submitter, WithChannelDeliverer(deliverer))

	svc.fireDue(context.Background())

	calls := deliverer.Calls()
	if len(calls) != 1 {
		t.Fatalf("delivery calls = %d, want 1", len(calls))
	}
	if calls[0].Channel != "feishu" || calls[0].Target != "oc_alerts" {
		t.Fatalf("delivery call = %+v", calls[0])
	}
	if !strings.Contains(calls[0].Content, "international update detected") {
		t.Fatalf("delivery content = %q", calls[0].Content)
	}
	if !strings.Contains(calls[0].Content, "Oil moved higher") {
		t.Fatalf("delivery content = %q", calls[0].Content)
	}
	got, err := store.Get("watch-delivery")
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if got.Notifications.TotalCount != 1 || got.Notifications.TodayCount != 1 {
		t.Fatalf("Notifications = %+v", got.Notifications)
	}
	if got.Notifications.LastStatus != "delivered" {
		t.Fatalf("LastStatus = %q", got.Notifications.LastStatus)
	}
}

func TestServicePropagatesAutomationScope(t *testing.T) {
	t.Parallel()

	store, _ := testStore(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("watch body"))
	}))
	defer server.Close()

	now := time.Now().UTC()
	item := Watch{
		ID:           "watch-scope",
		Name:         "scope watch",
		Enabled:      true,
		Interval:     "1m",
		FireOnStart:  true,
		SessionKey:   "watch:custom",
		Model:        "gpt-4.1",
		AutomationID: "auto-watch-1",
		Source:       Source{Kind: SourceKindHTTP, HTTP: &HTTPSource{URL: server.URL}},
		NextCheckAt:  now.Add(-time.Second),
		CreatedAt:    now,
		UpdatedAt:    now,
	}
	if err := store.Add(item); err != nil {
		t.Fatalf("Add() error = %v", err)
	}
	submitter := &mockSubmitter{result: &runtimesvc.RunResult{RunID: "run-watch-scope", Status: "completed", Summary: "watch completed"}}
	svc := NewService(store, submitter)

	svc.fireDue(context.Background())

	reqs := submitter.Requests()
	if len(reqs) != 1 {
		t.Fatalf("expected 1 submit, got %d", len(reqs))
	}
	req := reqs[0]
	if req.SessionKey != "watch:custom" || req.Model != "gpt-4.1" {
		t.Fatalf("request = %+v", req)
	}
	if req.AutomationID != "auto-watch-1" {
		t.Fatalf("AutomationID = %q", req.AutomationID)
	}
	if req.Metadata["automation_kind"] != "watch" || req.Metadata["automation_id"] != "watch-scope" || req.Metadata["automation_name"] != "scope watch" || req.Metadata["watch_source"] != string(SourceKindHTTP) {
		t.Fatalf("metadata = %+v", req.Metadata)
	}
}

func TestServiceStoresVerificationWarningOnTriggeredRun(t *testing.T) {
	t.Parallel()

	store, _ := testStore(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("warning body"))
	}))
	defer server.Close()

	now := time.Now().UTC()
	item := Watch{
		ID:          "watch-warning",
		Name:        "warning",
		Enabled:     true,
		Interval:    "1m",
		Source:      Source{Kind: SourceKindHTTP, HTTP: &HTTPSource{URL: server.URL}},
		FireOnStart: true,
		NextCheckAt: now.Add(-time.Second),
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	if err := store.Add(item); err != nil {
		t.Fatalf("Add() error = %v", err)
	}
	submitter := &mockSubmitter{
		result: &runtimesvc.RunResult{RunID: "run-watch-warning", Status: "completed", Summary: "change detected"},
		verification: &verifyrt.RunVerification{
			Status:  verifyrt.StatusWarning,
			Summary: "verification finished with 1 advisory warning",
		},
	}
	svc := NewService(store, submitter)

	svc.fireDue(context.Background())

	got, err := store.Get("watch-warning")
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if got.LastStatus != RunStatusTriggered {
		t.Fatalf("LastStatus = %q, want %q", got.LastStatus, RunStatusTriggered)
	}
	if got.LastVerificationStatus != string(verifyrt.StatusWarning) {
		t.Fatalf("LastVerificationStatus = %q", got.LastVerificationStatus)
	}
	if got.LastVerificationSummary != "verification finished with 1 advisory warning" {
		t.Fatalf("LastVerificationSummary = %q", got.LastVerificationSummary)
	}
	if got.LastResult == nil || got.LastResult.Verification == nil {
		t.Fatalf("LastResult = %#v", got.LastResult)
	}
	if string(got.LastResult.Verification.Status) != string(verifyrt.StatusWarning) {
		t.Fatalf("LastResult.Verification = %#v", got.LastResult.Verification)
	}
}

func TestServiceMarksVerificationFailureAsError(t *testing.T) {
	t.Parallel()

	store, _ := testStore(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("failure body"))
	}))
	defer server.Close()

	now := time.Now().UTC()
	item := Watch{
		ID:          "watch-verify-fail",
		Name:        "verify fail",
		Enabled:     true,
		Interval:    "1m",
		Source:      Source{Kind: SourceKindHTTP, HTTP: &HTTPSource{URL: server.URL}},
		FireOnStart: true,
		NextCheckAt: now.Add(-time.Second),
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	if err := store.Add(item); err != nil {
		t.Fatalf("Add() error = %v", err)
	}
	submitter := &mockSubmitter{
		result: &runtimesvc.RunResult{RunID: "run-watch-fail", Status: "completed", Summary: "change detected"},
		verification: &verifyrt.RunVerification{
			Status:  verifyrt.StatusFailed,
			Summary: "artifact chart is missing",
		},
	}
	svc := NewService(store, submitter)

	svc.fireDue(context.Background())

	got, err := store.Get("watch-verify-fail")
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if got.LastStatus != RunStatusError {
		t.Fatalf("LastStatus = %q, want %q", got.LastStatus, RunStatusError)
	}
	if got.LastError != "artifact chart is missing" {
		t.Fatalf("LastError = %q", got.LastError)
	}
	if got.LastVerificationStatus != string(verifyrt.StatusFailed) {
		t.Fatalf("LastVerificationStatus = %q", got.LastVerificationStatus)
	}
}

func TestServiceAppliesBackoffAfterWatchError(t *testing.T) {
	t.Parallel()

	store, _ := testStore(t)
	now := time.Now().UTC()
	item := Watch{
		ID:          "watch-backoff",
		Name:        "backoff",
		Enabled:     true,
		Interval:    "1m",
		FireOnStart: true,
		Source:      Source{Kind: SourceKindCalendar, Calendar: &CalendarSource{Query: "standup"}},
		NextCheckAt: now.Add(-time.Second),
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	if err := store.Add(item); err != nil {
		t.Fatalf("Add() error = %v", err)
	}
	svc := NewService(store, &mockSubmitter{})

	svc.fireDue(context.Background())

	got, err := store.Get("watch-backoff")
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if got.ConsecutiveErrors != 1 {
		t.Fatalf("ConsecutiveErrors = %d, want 1", got.ConsecutiveErrors)
	}
	if got.BackoffUntil.IsZero() || !got.BackoffUntil.After(got.LastCheckedAt) {
		t.Fatalf("BackoffUntil = %v, want future time after LastCheckedAt=%v", got.BackoffUntil, got.LastCheckedAt)
	}

	lastCheckedAt := got.LastCheckedAt
	if err := store.Update("watch-backoff", func(w *Watch) {
		w.NextCheckAt = time.Now().UTC().Add(-time.Second)
	}); err != nil {
		t.Fatalf("Update() error = %v", err)
	}
	svc.fireDue(context.Background())

	got, err = store.Get("watch-backoff")
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if !got.LastCheckedAt.Equal(lastCheckedAt) {
		t.Fatalf("LastCheckedAt changed during backoff: got %v, want %v", got.LastCheckedAt, lastCheckedAt)
	}
	if got.ConsecutiveErrors != 1 {
		t.Fatalf("ConsecutiveErrors = %d, want 1 after backoff skip", got.ConsecutiveErrors)
	}
}

func TestServiceDisablesWatchAfterTenConsecutiveErrors(t *testing.T) {
	t.Parallel()

	store, _ := testStore(t)
	now := time.Now().UTC()
	item := Watch{
		ID:                "watch-disable-after-errors",
		Name:              "disable after errors",
		Enabled:           true,
		Interval:          "1m",
		FireOnStart:       true,
		Source:            Source{Kind: SourceKindCalendar, Calendar: &CalendarSource{Query: "standup"}},
		ConsecutiveErrors: 9,
		NextCheckAt:       now.Add(-time.Second),
		CreatedAt:         now,
		UpdatedAt:         now,
	}
	if err := store.Add(item); err != nil {
		t.Fatalf("Add() error = %v", err)
	}
	svc := NewService(store, &mockSubmitter{})

	svc.fireDue(context.Background())

	got, err := store.Get("watch-disable-after-errors")
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if got.Enabled {
		t.Fatal("expected watch to be disabled after 10 consecutive errors")
	}
	if got.ConsecutiveErrors != 10 {
		t.Fatalf("ConsecutiveErrors = %d, want 10", got.ConsecutiveErrors)
	}
	if !got.NextCheckAt.IsZero() {
		t.Fatalf("NextCheckAt = %v, want zero after auto-disable", got.NextCheckAt)
	}
}

func TestStoreSaveRoundTrip(t *testing.T) {
	t.Parallel()

	store, path := testStore(t)
	if err := store.Add(Watch{ID: "watch-save", Interval: "1m", Source: Source{Kind: SourceKindHTTP, HTTP: &HTTPSource{URL: "https://example.com"}}}); err != nil {
		t.Fatalf("Add() error = %v", err)
	}
	if err := store.Save(); err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	var sf StoreFile
	if err := json.Unmarshal(data, &sf); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}
	if len(sf.Watches) != 1 {
		t.Fatalf("len(sf.Watches) = %d, want 1", len(sf.Watches))
	}
}

type fakeWatchIMAPMessage struct {
	UID     string
	Subject string
	From    string
	Date    string
}

type fakeWatchIMAPServer struct {
	ln   net.Listener
	Host string
	Port int
}

func (s *fakeWatchIMAPServer) Close() {
	if s != nil && s.ln != nil {
		_ = s.ln.Close()
	}
}

func newFakeWatchIMAPServer(t *testing.T, messages []fakeWatchIMAPMessage) *fakeWatchIMAPServer {
	t.Helper()
	cert := mustWatchSelfSignedCert(t)
	ln, err := tls.Listen("tcp", "127.0.0.1:0", &tls.Config{
		Certificates: []tls.Certificate{cert},
		MinVersion:   tls.VersionTLS12,
	})
	if err != nil {
		t.Fatalf("tls.Listen() error = %v", err)
	}
	server := &fakeWatchIMAPServer{ln: ln}
	host, portStr, err := net.SplitHostPort(ln.Addr().String())
	if err != nil {
		t.Fatalf("SplitHostPort() error = %v", err)
	}
	server.Host = host
	server.Port, _ = strconv.Atoi(portStr)
	go func() {
		for {
			conn, acceptErr := ln.Accept()
			if acceptErr != nil {
				return
			}
			go serveFakeWatchIMAPConn(conn, messages)
		}
	}()
	return server
}

func serveFakeWatchIMAPConn(conn net.Conn, messages []fakeWatchIMAPMessage) {
	defer conn.Close()
	r := bufio.NewReader(conn)
	w := bufio.NewWriter(conn)
	write := func(format string, args ...any) {
		_, _ = fmt.Fprintf(w, format, args...)
		_ = w.Flush()
	}
	write("* OK fake imap ready\r\n")
	for {
		line, err := r.ReadString('\n')
		if err != nil {
			return
		}
		line = strings.TrimRight(line, "\r\n")
		parts := strings.SplitN(line, " ", 3)
		if len(parts) < 2 {
			continue
		}
		tag := parts[0]
		cmd := strings.ToUpper(parts[1])
		arg := ""
		if len(parts) > 2 {
			arg = parts[2]
		}
		switch cmd {
		case "LOGIN":
			write("%s OK LOGIN completed\r\n", tag)
		case "SELECT":
			write("* %d EXISTS\r\n%s OK [READ-WRITE] SELECT completed\r\n", len(messages), tag)
		case "UID":
			handleFakeWatchIMAPUID(write, tag, arg, messages)
		case "LOGOUT":
			write("* BYE logging out\r\n%s OK LOGOUT completed\r\n", tag)
			return
		default:
			write("%s OK %s completed\r\n", tag, cmd)
		}
	}
}

func handleFakeWatchIMAPUID(write func(string, ...any), tag string, arg string, messages []fakeWatchIMAPMessage) {
	argUpper := strings.ToUpper(arg)
	switch {
	case strings.HasPrefix(argUpper, "SEARCH "):
		query := ""
		if idx := strings.Index(argUpper, "TEXT "); idx >= 0 {
			query = strings.Trim(strings.TrimSpace(arg[idx+5:]), `"`)
		}
		ids := make([]string, 0, len(messages))
		for _, msg := range messages {
			if query == "" || strings.Contains(strings.ToLower(msg.Subject+" "+msg.From), strings.ToLower(query)) {
				ids = append(ids, msg.UID)
			}
		}
		write("* SEARCH %s\r\n%s OK SEARCH completed\r\n", strings.Join(ids, " "), tag)
	case strings.HasPrefix(argUpper, "FETCH "):
		fields := strings.Fields(arg)
		if len(fields) < 2 {
			write("%s BAD invalid FETCH\r\n", tag)
			return
		}
		uid := fields[1]
		msg, ok := fakeWatchIMAPFind(messages, uid)
		if !ok {
			write("%s NO no such message\r\n", tag)
			return
		}
		headers := fmt.Sprintf("Subject: %s\r\nFrom: %s\r\nDate: %s\r\n\r\n", msg.Subject, msg.From, msg.Date)
		write("* 1 FETCH (UID %s BODY[HEADER.FIELDS (SUBJECT FROM DATE)] {%d}\r\n%s\r\n)\r\n%s OK FETCH completed\r\n",
			msg.UID, len(headers), headers, tag)
	default:
		write("%s BAD unsupported UID command\r\n", tag)
	}
}

func fakeWatchIMAPFind(messages []fakeWatchIMAPMessage, uid string) (fakeWatchIMAPMessage, bool) {
	for _, msg := range messages {
		if msg.UID == uid {
			return msg, true
		}
	}
	return fakeWatchIMAPMessage{}, false
}

func mustWatchSelfSignedCert(t *testing.T) tls.Certificate {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("GenerateKey() error = %v", err)
	}
	tpl := &x509.Certificate{
		SerialNumber: big.NewInt(time.Now().UnixNano()),
		Subject:      pkix.Name{CommonName: "127.0.0.1"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		IPAddresses:  []net.IP{net.ParseIP("127.0.0.1")},
	}
	der, err := x509.CreateCertificate(rand.Reader, tpl, tpl, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("CreateCertificate() error = %v", err)
	}
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)})
	cert, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		t.Fatalf("X509KeyPair() error = %v", err)
	}
	return cert
}
