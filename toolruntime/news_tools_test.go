package toolruntime

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/fulcrus/hopclaw/agent"
)

func TestSearchNewsNormalizesGenericResults(t *testing.T) {
	t.Parallel()

	var receivedBody map[string]any
	searchSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&receivedBody); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"results": []map[string]any{
				{
					"title":          "Top Story",
					"url":            "https://news.example.com/story",
					"description":    "Breaking update from the day.",
					"source":         "Example News",
					"published_date": "2026-03-13T09:00:00Z",
				},
			},
		})
	}))
	defer searchSrv.Close()

	reg := NewLayer2Registry(Layer2Config{
		Root: t.TempDir(),
		Services: ServicesConfig{
			Search: SearchServiceConfig{
				Provider: "generic",
				BaseURL:  searchSrv.URL,
			},
		},
	})

	results, err := reg.ExecuteBatch(context.Background(), &agent.Run{ID: "r"}, &agent.Session{ID: "s"}, []agent.ToolCall{{
		ID:   "c1",
		Name: "search.news",
		Input: map[string]any{
			"query":           "top stories",
			"count":           3,
			"freshness":       "day",
			"domains":         []any{"example.com"},
			"exclude_domains": []any{"spam.example"},
		},
	}})
	if err != nil {
		t.Fatal(err)
	}

	var out struct {
		Kind        string `json:"kind"`
		Query       string `json:"query"`
		ResultCount int    `json:"result_count"`
		Results     []struct {
			Title       string `json:"title"`
			URL         string `json:"url"`
			Snippet     string `json:"snippet"`
			Source      string `json:"source"`
			PublishedAt string `json:"published_at"`
			Domain      string `json:"domain"`
		} `json:"results"`
	}
	if err := json.Unmarshal([]byte(results[0].Content), &out); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}

	if out.Kind != "news" {
		t.Fatalf("kind = %q, want news", out.Kind)
	}
	if out.Query != "top stories" {
		t.Fatalf("query = %q", out.Query)
	}
	if out.ResultCount != 1 {
		t.Fatalf("result_count = %d, want 1", out.ResultCount)
	}
	if len(out.Results) != 1 {
		t.Fatalf("results length = %d, want 1", len(out.Results))
	}
	if out.Results[0].Domain != "news.example.com" {
		t.Fatalf("domain = %q", out.Results[0].Domain)
	}
	if out.Results[0].Source != "Example News" {
		t.Fatalf("source = %q", out.Results[0].Source)
	}
	if receivedBody["kind"] != "news" {
		t.Fatalf("kind body = %v, want news", receivedBody["kind"])
	}
	if receivedBody["count"].(float64) != 3 {
		t.Fatalf("count body = %v, want 3", receivedBody["count"])
	}
	bodyQuery, _ := receivedBody["query"].(string)
	if !strings.Contains(bodyQuery, "site:example.com") {
		t.Fatalf("query body = %q, want site filter", bodyQuery)
	}
	if !strings.Contains(bodyQuery, "-site:spam.example") {
		t.Fatalf("query body = %q, want exclude filter", bodyQuery)
	}
}

func TestNewsDigestProducesMarkdownAndCSV(t *testing.T) {
	t.Parallel()

	articleSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte(`<html><head><title>Top Story</title></head><body><article><p>Markets rallied after a major policy announcement.</p><p>Analysts said the move could reshape the sector.</p></article></body></html>`))
	}))
	defer articleSrv.Close()

	searchSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"results": []map[string]any{
				{
					"title":          "Markets rally on policy shift",
					"url":            articleSrv.URL,
					"description":    "A strong market reaction followed today's policy shift.",
					"source":         "Finance Wire",
					"published_date": "2026-03-13",
				},
			},
		})
	}))
	defer searchSrv.Close()

	reg := NewLayer2Registry(Layer2Config{
		Root: t.TempDir(),
		Services: ServicesConfig{
			Search: SearchServiceConfig{
				Provider: "generic",
				BaseURL:  searchSrv.URL,
			},
		},
	})

	results, err := reg.ExecuteBatch(context.Background(), &agent.Run{ID: "r"}, &agent.Session{ID: "s"}, []agent.ToolCall{{
		ID:   "c1",
		Name: "news.digest",
		Input: map[string]any{
			"query":       "today's market headlines",
			"count":       1,
			"fetch_top_n": 1,
		},
	}})
	if err != nil {
		t.Fatal(err)
	}

	var out struct {
		ResultCount   int    `json:"result_count"`
		FetchedCount  int    `json:"fetched_count"`
		MarkdownTable string `json:"markdown_table"`
		CSV           string `json:"csv"`
		Items         []struct {
			Title   string `json:"title"`
			Summary string `json:"summary"`
		} `json:"items"`
	}
	if err := json.Unmarshal([]byte(results[0].Content), &out); err != nil {
		t.Fatalf("unmarshal digest: %v", err)
	}

	if out.ResultCount != 1 {
		t.Fatalf("result_count = %d, want 1", out.ResultCount)
	}
	if out.FetchedCount != 1 {
		t.Fatalf("fetched_count = %d, want 1", out.FetchedCount)
	}
	if !strings.Contains(out.MarkdownTable, "Markets rally on policy shift") {
		t.Fatalf("markdown_table = %q", out.MarkdownTable)
	}
	if !strings.Contains(out.MarkdownTable, articleSrv.URL) {
		t.Fatalf("markdown_table missing article URL: %q", out.MarkdownTable)
	}
	if !strings.Contains(out.CSV, "rank,title,source,published_at,url,summary") {
		t.Fatalf("csv missing header: %q", out.CSV)
	}
	if len(out.Items) != 1 {
		t.Fatalf("items length = %d, want 1", len(out.Items))
	}
	if !strings.Contains(out.Items[0].Summary, "Markets rallied after a major policy announcement.") {
		t.Fatalf("summary = %q", out.Items[0].Summary)
	}
}
