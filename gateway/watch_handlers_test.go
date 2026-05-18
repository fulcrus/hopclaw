package gateway

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/fulcrus/hopclaw/watch"
)

func newTestWatchGateway(t *testing.T) (*Gateway, *watch.Service) {
	t.Helper()
	tmp := filepath.Join(t.TempDir(), "watch.json")
	if err := os.WriteFile(tmp, []byte(`{"version":1,"watches":[]}`), 0o644); err != nil {
		t.Fatalf("write watch file: %v", err)
	}
	store, err := watch.Load(tmp)
	if err != nil {
		t.Fatalf("watch.Load() error = %v", err)
	}
	svc := watch.NewService(store, nil)
	gw := newTestGatewayFull(t)
	gw.SetWatch(svc)
	return gw, svc
}

func TestWatchListNilService(t *testing.T) {
	t.Parallel()

	gw := newTestGatewayFull(t)
	rec := doRequest(t, gw.Handler(), http.MethodGet, "/operator/watch/items", "")
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d", rec.Code)
	}
}

func TestWatchCreateAndGet(t *testing.T) {
	t.Parallel()

	gw, _ := newTestWatchGateway(t)
	handler := gw.Handler()

	body := `{"name":"page-watch","interval":"5m","source":{"kind":"http","http":{"url":"https://example.com"}},"delivery":{"channel":"feishu","target":"oc_alerts"},"prompt":"Summarize changes"}`
	rec := doRequest(t, handler, http.MethodPost, "/operator/watch/items", body)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create: status = %d body=%s", rec.Code, rec.Body.String())
	}

	var created watchResponse
	if err := json.NewDecoder(rec.Body).Decode(&created); err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	if created.Item.ID == "" {
		t.Fatal("expected watch id")
	}
	if created.Item.Source.Kind != watch.SourceKindHTTP {
		t.Fatalf("Source.Kind = %q", created.Item.Source.Kind)
	}
	if created.Item.Delivery == nil || created.Item.Delivery.Channel != "feishu" || created.Item.Delivery.Target != "oc_alerts" {
		t.Fatalf("Delivery = %+v", created.Item.Delivery)
	}

	getRec := doRequest(t, handler, http.MethodGet, "/operator/watch/items/"+created.Item.ID, "")
	if getRec.Code != http.StatusOK {
		t.Fatalf("get: status = %d body=%s", getRec.Code, getRec.Body.String())
	}
}

func TestWatchCreateSupportedSources(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		body      string
		wantKind  string
		assertion func(t *testing.T, item watch.Watch)
	}{
		{
			name:     "file",
			body:     `{"name":"file-watch","interval":"5m","source":{"kind":"file","file":{"path":"/tmp/report.csv"}}}`,
			wantKind: watch.SourceKindFile,
			assertion: func(t *testing.T, item watch.Watch) {
				if item.Source.File == nil || item.Source.File.Path != "/tmp/report.csv" {
					t.Fatalf("file source = %+v", item.Source.File)
				}
			},
		},
		{
			name:     "feed",
			body:     `{"name":"feed-watch","interval":"5m","source":{"kind":"feed","feed":{"url":"https://example.com/feed.xml"}}}`,
			wantKind: watch.SourceKindFeed,
			assertion: func(t *testing.T, item watch.Watch) {
				if item.Source.Feed == nil || item.Source.Feed.URL != "https://example.com/feed.xml" {
					t.Fatalf("feed source = %+v", item.Source.Feed)
				}
			},
		},
		{
			name:     "mailbox",
			body:     `{"name":"mail-watch","interval":"5m","source":{"kind":"mailbox","mailbox":{"folder":"INBOX","query":"Alert","limit":10}}}`,
			wantKind: watch.SourceKindMailbox,
			assertion: func(t *testing.T, item watch.Watch) {
				if item.Source.Mailbox == nil || item.Source.Mailbox.Folder != "INBOX" || item.Source.Mailbox.Query != "Alert" || item.Source.Mailbox.Limit != 10 {
					t.Fatalf("mailbox source = %+v", item.Source.Mailbox)
				}
			},
		},
		{
			name:     "browser-snapshot",
			body:     `{"name":"browser-watch","interval":"5m","source":{"kind":"browser_snapshot","browser_snapshot":{"url":"https://example.com/page"}}}`,
			wantKind: watch.SourceKindBrowserSnapshot,
			assertion: func(t *testing.T, item watch.Watch) {
				if item.Source.BrowserSnapshot == nil || item.Source.BrowserSnapshot.URL != "https://example.com/page" {
					t.Fatalf("browser snapshot source = %+v", item.Source.BrowserSnapshot)
				}
			},
		},
		{
			name:     "calendar",
			body:     `{"name":"calendar-watch","interval":"5m","source":{"kind":"calendar","calendar":{"query":"standup","limit":8}}}`,
			wantKind: watch.SourceKindCalendar,
			assertion: func(t *testing.T, item watch.Watch) {
				if item.Source.Calendar == nil || item.Source.Calendar.Query != "standup" || item.Source.Calendar.Limit != 8 {
					t.Fatalf("calendar source = %+v", item.Source.Calendar)
				}
			},
		},
		{
			name:     "webhook",
			body:     `{"name":"webhook-watch","interval":"5m","source":{"kind":"webhook","webhook":{"webhook_id":"demo","sender_id":"user-1","limit":12}}}`,
			wantKind: watch.SourceKindWebhook,
			assertion: func(t *testing.T, item watch.Watch) {
				if item.Source.Webhook == nil || item.Source.Webhook.WebhookID != "demo" || item.Source.Webhook.SenderID != "user-1" || item.Source.Webhook.Limit != 12 {
					t.Fatalf("webhook source = %+v", item.Source.Webhook)
				}
			},
		},
		{
			name:     "structured-app-inbox",
			body:     `{"name":"inbox-watch","interval":"5m","source":{"kind":"structured_app_inbox","structured_app_inbox":{"session_key":"slack:C123","limit":15}}}`,
			wantKind: watch.SourceKindStructuredInbox,
			assertion: func(t *testing.T, item watch.Watch) {
				if item.Source.StructuredInbox == nil || item.Source.StructuredInbox.SessionKey != "slack:C123" || item.Source.StructuredInbox.Limit != 15 {
					t.Fatalf("structured inbox source = %+v", item.Source.StructuredInbox)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gw, _ := newTestWatchGateway(t)
			rec := doRequest(t, gw.Handler(), http.MethodPost, "/operator/watch/items", tt.body)
			if rec.Code != http.StatusCreated {
				t.Fatalf("create: status = %d body=%s", rec.Code, rec.Body.String())
			}
			var created watchResponse
			if err := json.NewDecoder(rec.Body).Decode(&created); err != nil {
				t.Fatalf("Decode() error = %v", err)
			}
			if created.Item.Source.Kind != tt.wantKind {
				t.Fatalf("Source.Kind = %q, want %q", created.Item.Source.Kind, tt.wantKind)
			}
			tt.assertion(t, created.Item)
		})
	}
}

func TestWatchCreateRejectsTrailingJSON(t *testing.T) {
	t.Parallel()

	gw, svc := newTestWatchGateway(t)
	rec := doRequest(t, gw.Handler(), http.MethodPost, "/operator/watch/items", `{"name":"page-watch","interval":"5m","source":{"kind":"http","http":{"url":"https://example.com"}}} {"extra":true}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("create trailing json: status = %d body=%s", rec.Code, rec.Body.String())
	}

	items := svc.Store().List()
	if len(items) != 0 {
		t.Fatalf("items = %#v, want empty", items)
	}
}

func TestWatchStatus(t *testing.T) {
	t.Parallel()

	gw, _ := newTestWatchGateway(t)
	rec := doRequest(t, gw.Handler(), http.MethodGet, "/operator/watch/status", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	var payload watchStatusResponse
	if err := json.NewDecoder(rec.Body).Decode(&payload); err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	if payload.WatchCount != 0 {
		t.Fatalf("WatchCount = %d, want 0", payload.WatchCount)
	}
}

func TestWatchUpdateRejectsInvalidMutation(t *testing.T) {
	t.Parallel()

	gw, svc := newTestWatchGateway(t)
	handler := gw.Handler()
	store := svc.Store()
	if err := store.Add(watch.Watch{
		ID:       "watch-1",
		Name:     "page",
		Enabled:  true,
		Interval: "5m",
		Source: watch.Source{
			Kind: watch.SourceKindHTTP,
			HTTP: &watch.HTTPSource{URL: "https://example.com"},
		},
	}); err != nil {
		t.Fatalf("Add() error = %v", err)
	}

	rec := doRequest(t, handler, http.MethodPatch, "/operator/watch/items/watch-1", `{"source":{"kind":"http","http":{"url":""}}}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}

	item, err := store.Get("watch-1")
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if item.Source.HTTP == nil || item.Source.HTTP.URL != "https://example.com" {
		t.Fatalf("watch source mutated on invalid update: %+v", item.Source.HTTP)
	}
}

func TestWatchUpdateSupportsChangingSourceKind(t *testing.T) {
	t.Parallel()

	gw, svc := newTestWatchGateway(t)
	handler := gw.Handler()
	store := svc.Store()
	if err := store.Add(watch.Watch{
		ID:       "watch-2",
		Name:     "page",
		Enabled:  true,
		Interval: "5m",
		Source: watch.Source{
			Kind: watch.SourceKindHTTP,
			HTTP: &watch.HTTPSource{URL: "https://example.com"},
		},
	}); err != nil {
		t.Fatalf("Add() error = %v", err)
	}

	rec := doRequest(t, handler, http.MethodPatch, "/operator/watch/items/watch-2", `{"name":"mail-check","source":{"kind":"structured_app_inbox","structured_app_inbox":{"session_key":"slack:C123","limit":5}}}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}

	item, err := store.Get("watch-2")
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if item.Name != "mail-check" {
		t.Fatalf("Name = %q", item.Name)
	}
	if item.Source.Kind != watch.SourceKindStructuredInbox {
		t.Fatalf("Source.Kind = %q", item.Source.Kind)
	}
	if item.Source.StructuredInbox == nil || item.Source.StructuredInbox.SessionKey != "slack:C123" || item.Source.StructuredInbox.Limit != 5 {
		t.Fatalf("Structured inbox source = %+v", item.Source.StructuredInbox)
	}
}

func TestWatchUpdateSupportsDeliveryTarget(t *testing.T) {
	t.Parallel()

	gw, svc := newTestWatchGateway(t)
	handler := gw.Handler()
	store := svc.Store()
	if err := store.Add(watch.Watch{
		ID:       "watch-delivery",
		Name:     "page",
		Enabled:  true,
		Interval: "5m",
		Source: watch.Source{
			Kind: watch.SourceKindHTTP,
			HTTP: &watch.HTTPSource{URL: "https://example.com"},
		},
	}); err != nil {
		t.Fatalf("Add() error = %v", err)
	}

	rec := doRequest(t, handler, http.MethodPatch, "/operator/watch/items/watch-delivery", `{"delivery":{"channel":"telegram","target":"chat-42"}}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}

	item, err := store.Get("watch-delivery")
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if item.Delivery == nil || item.Delivery.Channel != "telegram" || item.Delivery.Target != "chat-42" {
		t.Fatalf("Delivery = %+v", item.Delivery)
	}
}

func TestWatchUpdateRejectsTrailingJSON(t *testing.T) {
	t.Parallel()

	gw, svc := newTestWatchGateway(t)
	handler := gw.Handler()
	store := svc.Store()
	if err := store.Add(watch.Watch{
		ID:       "watch-trailing",
		Name:     "page",
		Enabled:  true,
		Interval: "5m",
		Source: watch.Source{
			Kind: watch.SourceKindHTTP,
			HTTP: &watch.HTTPSource{URL: "https://example.com"},
		},
	}); err != nil {
		t.Fatalf("Add() error = %v", err)
	}

	rec := doRequest(t, handler, http.MethodPatch, "/operator/watch/items/watch-trailing", `{"name":"updated"} {"extra":true}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("update trailing json: status = %d body=%s", rec.Code, rec.Body.String())
	}

	item, err := store.Get("watch-trailing")
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if item.Name != "page" {
		t.Fatalf("Name = %q, want page", item.Name)
	}
}
