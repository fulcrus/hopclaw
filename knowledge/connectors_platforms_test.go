package knowledge

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
)

func TestFeishuDocsConnectorSync(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/open-apis/auth/v3/tenant_access_token/internal":
			_ = json.NewEncoder(w).Encode(map[string]any{"tenant_access_token": "tenant-token"})
		case r.URL.Path == "/open-apis/docx/v1/documents/doccn123":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"data": map[string]any{
					"document": map[string]any{
						"title": "Feishu Runbook",
						"url":   "https://feishu.example/docx/doccn123",
					},
				},
			})
		case r.URL.Path == "/open-apis/docx/v1/documents/doccn123/raw_content":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"data": map[string]any{
					"content": "故障处理手册\n回滚步骤和升级说明",
				},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	store, err := NewSQLiteStore(filepath.Join(t.TempDir(), "knowledge.db"))
	if err != nil {
		t.Fatalf("NewSQLiteStore() error = %v", err)
	}
	svc, err := NewService(store, nil)
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}
	source, err := svc.UpsertSource(context.Background(), Source{
		Name:    "Feishu Docs",
		Kind:    SourceKindFeishuDocs,
		Enabled: true,
		Config: map[string]any{
			"base_url":     server.URL + "/open-apis",
			"app_id":       "cli_a",
			"app_secret":   "secret",
			"document_ids": []string{"doccn123"},
		},
	})
	if err != nil {
		t.Fatalf("UpsertSource() error = %v", err)
	}
	result, err := svc.SyncSource(context.Background(), source.ID)
	if err != nil {
		t.Fatalf("SyncSource() error = %v", err)
	}
	if result.Stats.Documents != 1 || result.Stats.Chunks == 0 {
		t.Fatalf("result stats = %#v", result.Stats)
	}
	results, err := svc.Search(context.Background(), SearchFilter{Query: "回滚", Limit: 5})
	if err != nil {
		t.Fatalf("Search() error = %v", err)
	}
	if len(results) == 0 || !strings.Contains(results[0].Preview, "回滚") {
		t.Fatalf("results = %#v", results)
	}
}

func TestNotionConnectorSync(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/v1/pages/12345678-1234-1234-1234-123456789abc":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"url":              "https://notion.so/page",
				"last_edited_time": "2026-03-18T10:00:00Z",
				"properties": map[string]any{
					"Title": map[string]any{
						"type": "title",
						"title": []any{
							map[string]any{"plain_text": "Notion SOP"},
						},
					},
				},
			})
		case strings.HasPrefix(r.URL.Path, "/v1/blocks/12345678-1234-1234-1234-123456789abc/children"):
			_ = json.NewEncoder(w).Encode(map[string]any{
				"results": []any{
					map[string]any{
						"id":           "block-1",
						"type":         "paragraph",
						"has_children": false,
						"paragraph": map[string]any{
							"rich_text": []any{
								map[string]any{"plain_text": "Deploy checklist and rollback notes"},
							},
						},
					},
				},
				"has_more": false,
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	store, err := NewSQLiteStore(filepath.Join(t.TempDir(), "knowledge.db"))
	if err != nil {
		t.Fatalf("NewSQLiteStore() error = %v", err)
	}
	svc, err := NewService(store, nil)
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}
	source, err := svc.UpsertSource(context.Background(), Source{
		Name:    "Notion SOP",
		Kind:    SourceKindNotion,
		Enabled: true,
		Config: map[string]any{
			"base_url": server.URL,
			"token":    "secret-token",
			"page_ids": []string{"12345678123412341234123456789abc"},
		},
	})
	if err != nil {
		t.Fatalf("UpsertSource() error = %v", err)
	}
	if _, err := svc.SyncSource(context.Background(), source.ID); err != nil {
		t.Fatalf("SyncSource() error = %v", err)
	}
	results, err := svc.Search(context.Background(), SearchFilter{Query: "rollback", Limit: 5})
	if err != nil {
		t.Fatalf("Search() error = %v", err)
	}
	if len(results) == 0 || results[0].SourceKind != SourceKindNotion {
		t.Fatalf("results = %#v", results)
	}
}

func TestConfluenceConnectorSync(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/wiki/rest/api/content/42":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id":    "42",
				"title": "Ops Runbook",
				"body": map[string]any{
					"storage": map[string]any{
						"value": "<p>Primary runbook and escalation policy</p>",
					},
				},
				"version": map[string]any{"when": "2026-03-18T10:00:00Z"},
				"_links":  map[string]any{"webui": "/spaces/OPS/pages/42"},
			})
		case r.URL.Path == "/wiki/rest/api/content/42/descendant/page":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"results": []any{
					map[string]any{
						"id":    "43",
						"title": "Rollback Guide",
						"body": map[string]any{
							"storage": map[string]any{
								"value": "<p>Rollback checklist and approval path</p>",
							},
						},
						"version": map[string]any{"when": "2026-03-18T10:05:00Z"},
						"_links":  map[string]any{"webui": "/spaces/OPS/pages/43"},
					},
				},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	store, err := NewSQLiteStore(filepath.Join(t.TempDir(), "knowledge.db"))
	if err != nil {
		t.Fatalf("NewSQLiteStore() error = %v", err)
	}
	svc, err := NewService(store, nil)
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}
	source, err := svc.UpsertSource(context.Background(), Source{
		Name:    "Confluence Ops",
		Kind:    SourceKindConfluence,
		Enabled: true,
		Config: map[string]any{
			"base_url":            server.URL + "/wiki",
			"email":               "ops@example.com",
			"api_token":           "api-token",
			"page_ids":            []string{"42"},
			"include_descendants": true,
		},
	})
	if err != nil {
		t.Fatalf("UpsertSource() error = %v", err)
	}
	result, err := svc.SyncSource(context.Background(), source.ID)
	if err != nil {
		t.Fatalf("SyncSource() error = %v", err)
	}
	if result.Stats.Documents != 2 {
		t.Fatalf("Documents = %d, want 2", result.Stats.Documents)
	}
	results, err := svc.Search(context.Background(), SearchFilter{Query: "approval", Limit: 5})
	if err != nil {
		t.Fatalf("Search() error = %v", err)
	}
	if len(results) == 0 || results[0].SourceKind != SourceKindConfluence {
		t.Fatalf("results = %#v", results)
	}
}

func TestGoogleDriveConnectorSync(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/drive/v3/files/file123":
			if r.URL.Query().Get("alt") == "media" {
				_, _ = w.Write([]byte("plain drive content with release checklist"))
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id":           "file123",
				"name":         "Team Runbook",
				"mimeType":     "application/vnd.google-apps.document",
				"modifiedTime": "2026-03-18T10:00:00Z",
				"webViewLink":  "https://drive.google.com/file/d/file123/view",
			})
		case r.URL.Path == "/drive/v3/files/file123/export":
			_, _ = w.Write([]byte("workspace export content with release checklist"))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	store, err := NewSQLiteStore(filepath.Join(t.TempDir(), "knowledge.db"))
	if err != nil {
		t.Fatalf("NewSQLiteStore() error = %v", err)
	}
	svc, err := NewService(store, nil)
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}
	source, err := svc.UpsertSource(context.Background(), Source{
		Name:    "Google Docs",
		Kind:    SourceKindGoogleDrive,
		Enabled: true,
		Config: map[string]any{
			"base_url": server.URL,
			"token":    "secret-token",
			"file_ids": []string{"file123"},
		},
	})
	if err != nil {
		t.Fatalf("UpsertSource() error = %v", err)
	}
	if _, err := svc.SyncSource(context.Background(), source.ID); err != nil {
		t.Fatalf("SyncSource() error = %v", err)
	}
	results, err := svc.Search(context.Background(), SearchFilter{Query: "release checklist", Limit: 5})
	if err != nil {
		t.Fatalf("Search() error = %v", err)
	}
	if len(results) == 0 || results[0].SourceKind != SourceKindGoogleDrive {
		t.Fatalf("results = %#v", results)
	}
}

func TestYuqueConnectorSync(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v2/repos/team/runbook/docs":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"data": []any{
					map[string]any{"slug": "rollback-guide"},
				},
			})
		case "/api/v2/repos/team/runbook/docs/rollback-guide":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"data": map[string]any{
					"id":         "doc-1",
					"slug":       "rollback-guide",
					"title":      "Rollback Guide",
					"url":        "https://www.yuque.com/team/runbook/rollback-guide",
					"body":       "Rollback procedure and escalation tree",
					"updated_at": "2026-03-18T11:00:00Z",
				},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	store, err := NewSQLiteStore(filepath.Join(t.TempDir(), "knowledge.db"))
	if err != nil {
		t.Fatalf("NewSQLiteStore() error = %v", err)
	}
	svc, err := NewService(store, nil)
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}
	source, err := svc.UpsertSource(context.Background(), Source{
		Name:    "Yuque Runbook",
		Kind:    SourceKindYuque,
		Enabled: true,
		Config: map[string]any{
			"base_url":   server.URL + "/api/v2",
			"token":      "secret-token",
			"repo_paths": []string{"team/runbook"},
		},
	})
	if err != nil {
		t.Fatalf("UpsertSource() error = %v", err)
	}
	if _, err := svc.SyncSource(context.Background(), source.ID); err != nil {
		t.Fatalf("SyncSource() error = %v", err)
	}
	results, err := svc.Search(context.Background(), SearchFilter{Query: "escalation", Limit: 5})
	if err != nil {
		t.Fatalf("Search() error = %v", err)
	}
	if len(results) == 0 || results[0].SourceKind != SourceKindYuque {
		t.Fatalf("results = %#v", results)
	}
}

func TestTencentDocsConnectorSync(t *testing.T) {
	t.Parallel()

	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/openapi/drive/v2/files/DWU123/metadata":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"data": map[string]any{
					"title":      "腾讯文档日报",
					"url":        "https://docs.qq.com/doc/DWU123",
					"updated_at": "2026-03-18T12:00:00Z",
				},
			})
		case "/openapi/export/v2/files/DWU123":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"data": map[string]any{
					"task_id": "task-1",
				},
			})
		case "/openapi/export/v2/files/DWU123/tasks/task-1":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"data": map[string]any{
					"status":       "done",
					"download_url": server.URL + "/downloads/DWU123.txt",
				},
			})
		case "/downloads/DWU123.txt":
			_, _ = w.Write([]byte("日报复盘与风险升级总结"))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	store, err := NewSQLiteStore(filepath.Join(t.TempDir(), "knowledge.db"))
	if err != nil {
		t.Fatalf("NewSQLiteStore() error = %v", err)
	}
	svc, err := NewService(store, nil)
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}
	source, err := svc.UpsertSource(context.Background(), Source{
		Name:    "Tencent Docs Daily",
		Kind:    SourceKindTencentDocs,
		Enabled: true,
		Config: map[string]any{
			"base_url": server.URL,
			"token":    "secret-token",
			"file_ids": []string{"DWU123"},
		},
	})
	if err != nil {
		t.Fatalf("UpsertSource() error = %v", err)
	}
	if _, err := svc.SyncSource(context.Background(), source.ID); err != nil {
		t.Fatalf("SyncSource() error = %v", err)
	}
	results, err := svc.Search(context.Background(), SearchFilter{Query: "风险升级", Limit: 5})
	if err != nil {
		t.Fatalf("Search() error = %v", err)
	}
	if len(results) == 0 || results[0].SourceKind != SourceKindTencentDocs {
		t.Fatalf("results = %#v", results)
	}
}
