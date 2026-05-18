package gateway

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/fulcrus/hopclaw/keychain"
	"github.com/fulcrus/hopclaw/knowledge"
)

type testKeychainStore struct {
	mu      sync.Mutex
	secrets map[string]map[string]string
	setErr  error
	delErr  error
}

func newTestKeychainStore() *testKeychainStore {
	return &testKeychainStore{secrets: map[string]map[string]string{}}
}

func (s *testKeychainStore) Get(service, key string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	group := s.secrets[service]
	if group == nil {
		return "", keychain.ErrNotFound
	}
	value, ok := group[key]
	if !ok {
		return "", keychain.ErrNotFound
	}
	return value, nil
}

func (s *testKeychainStore) Set(service, key, value string) error {
	if s.setErr != nil {
		return s.setErr
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.secrets[service] == nil {
		s.secrets[service] = map[string]string{}
	}
	s.secrets[service][key] = value
	return nil
}

func (s *testKeychainStore) Delete(service, key string) error {
	if s.delErr != nil {
		return s.delErr
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	group := s.secrets[service]
	if group == nil {
		return keychain.ErrNotFound
	}
	if _, ok := group[key]; !ok {
		return keychain.ErrNotFound
	}
	delete(group, key)
	return nil
}

func (s *testKeychainStore) count(service string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.secrets[service])
}

type failingKnowledgeStore struct {
	base             knowledge.Store
	failOnUpsertCall int
	upsertCalls      int
	upsertErr        error
}

func (s *failingKnowledgeStore) ListSources(ctx context.Context) ([]knowledge.Source, error) {
	return s.base.ListSources(ctx)
}

func (s *failingKnowledgeStore) GetSource(ctx context.Context, id string) (*knowledge.Source, error) {
	return s.base.GetSource(ctx, id)
}

func (s *failingKnowledgeStore) UpsertSource(ctx context.Context, source knowledge.Source) (knowledge.Source, error) {
	s.upsertCalls++
	if s.failOnUpsertCall > 0 && s.upsertCalls == s.failOnUpsertCall {
		if s.upsertErr == nil {
			s.upsertErr = errors.New("forced upsert failure")
		}
		return knowledge.Source{}, s.upsertErr
	}
	return s.base.UpsertSource(ctx, source)
}

func (s *failingKnowledgeStore) DeleteSource(ctx context.Context, id string) error {
	return s.base.DeleteSource(ctx, id)
}

func (s *failingKnowledgeStore) ListDocuments(ctx context.Context, sourceID string) ([]knowledge.Document, error) {
	return s.base.ListDocuments(ctx, sourceID)
}

func (s *failingKnowledgeStore) UpsertDocument(ctx context.Context, document knowledge.Document, chunks []knowledge.Chunk) error {
	return s.base.UpsertDocument(ctx, document, chunks)
}

func (s *failingKnowledgeStore) DeleteDocuments(ctx context.Context, sourceID string, documentIDs []string) error {
	return s.base.DeleteDocuments(ctx, sourceID, documentIDs)
}

func (s *failingKnowledgeStore) ComputeSourceStats(ctx context.Context, sourceID string) (knowledge.SourceStats, error) {
	return s.base.ComputeSourceStats(ctx, sourceID)
}

func (s *failingKnowledgeStore) ListChunks(ctx context.Context, sourceID string) ([]knowledge.Chunk, error) {
	return s.base.ListChunks(ctx, sourceID)
}

func (s *failingKnowledgeStore) ListAllChunks(ctx context.Context) ([]knowledge.Chunk, error) {
	return s.base.ListAllChunks(ctx)
}

func (s *failingKnowledgeStore) SearchText(ctx context.Context, filter knowledge.SearchFilter, queryLocale string) ([]knowledge.SearchResult, error) {
	return s.base.SearchText(ctx, filter, queryLocale)
}

func (s *failingKnowledgeStore) ListChunkVectors(ctx context.Context, sourceID string) ([]knowledge.ChunkVector, error) {
	return s.base.ListChunkVectors(ctx, sourceID)
}

func (s *failingKnowledgeStore) UpsertChunkVectors(ctx context.Context, vectors []knowledge.ChunkVector) error {
	return s.base.UpsertChunkVectors(ctx, vectors)
}

func (s *failingKnowledgeStore) DeleteChunkVectors(ctx context.Context, chunkIDs []string) error {
	return s.base.DeleteChunkVectors(ctx, chunkIDs)
}

func TestKnowledgeSourceCRUDAndSearch(t *testing.T) {
	docRoot := t.TempDir()
	if err := os.WriteFile(filepath.Join(docRoot, "faq.md"), []byte("Refund policy and invoice export guide."), 0o644); err != nil {
		t.Fatalf("write doc: %v", err)
	}
	store, err := knowledge.NewSQLiteStore(filepath.Join(t.TempDir(), "knowledge.db"))
	if err != nil {
		t.Fatalf("NewSQLiteStore() error = %v", err)
	}
	svc, err := knowledge.NewService(store, nil)
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}
	gw := newTestGatewayFull(t)
	gw.SetKnowledgeService(svc)

	rec := doRequest(t, gw.Handler(), "POST", "/operator/knowledge/sources", `{"name":"FAQ Docs","kind":"local_dir","path":"`+docRoot+`","enabled":true}`)
	if rec.Code != 201 {
		t.Fatalf("POST /operator/knowledge/sources status = %d body=%s", rec.Code, rec.Body.String())
	}
	var created struct {
		Item knowledge.Source `json:"item"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode create response: %v", err)
	}
	if created.Item.ID == "" {
		t.Fatal("expected created source id")
	}

	rec = doRequest(t, gw.Handler(), "POST", "/operator/knowledge/sources/"+created.Item.ID+"/sync", "")
	if rec.Code != 200 {
		t.Fatalf("POST /operator/knowledge/sources/{id}/sync status = %d body=%s", rec.Code, rec.Body.String())
	}

	rec = doRequest(t, gw.Handler(), "GET", "/operator/knowledge/search?q=invoice&source_id="+created.Item.ID, "")
	if rec.Code != 200 {
		t.Fatalf("GET /operator/knowledge/search status = %d body=%s", rec.Code, rec.Body.String())
	}
	var search knowledgeSearchResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &search); err != nil {
		t.Fatalf("decode search response: %v", err)
	}
	if search.Count == 0 {
		t.Fatalf("search count = 0 body=%s", rec.Body.String())
	}

	rec = doRequest(t, gw.Handler(), "PATCH", "/operator/knowledge/sources/"+created.Item.ID, `{"enabled":false}`)
	if rec.Code != 200 {
		t.Fatalf("PATCH /operator/knowledge/sources/{id} status = %d body=%s", rec.Code, rec.Body.String())
	}

	rec = doRequest(t, gw.Handler(), "DELETE", "/operator/knowledge/sources/"+created.Item.ID, "")
	if rec.Code != 200 {
		t.Fatalf("DELETE /operator/knowledge/sources/{id} status = %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestKnowledgeSourcesListReturnsCatalogDrivenFieldMetadata(t *testing.T) {
	fileStore, err := knowledge.NewSQLiteStore(filepath.Join(t.TempDir(), "knowledge.db"))
	if err != nil {
		t.Fatalf("NewSQLiteStore() error = %v", err)
	}
	svc, err := knowledge.NewService(fileStore, nil)
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}
	gw := newTestGatewayFull(t)
	gw.SetKnowledgeService(svc)

	rec := doRequest(t, gw.Handler(), "GET", "/operator/knowledge/sources", "")
	if rec.Code != 200 {
		t.Fatalf("GET /operator/knowledge/sources status = %d body=%s", rec.Code, rec.Body.String())
	}

	var payload knowledgeSourcesListResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	if len(payload.SupportedKinds) == 0 {
		t.Fatal("expected supported kinds metadata")
	}

	var confluence knowledge.SourceKindDescriptor
	found := false
	for _, item := range payload.SupportedKinds {
		if item.Kind != knowledge.SourceKindConfluence {
			continue
		}
		confluence = item
		found = true
		break
	}
	if !found {
		t.Fatal("expected confluence metadata")
	}
	if len(confluence.Fields) == 0 {
		t.Fatal("expected confluence field descriptors")
	}
	foundLocaleField := false
	for _, field := range confluence.Fields {
		if field.Scope == knowledge.SourceFieldScopeRoot && field.Key == "locale" {
			foundLocaleField = true
			break
		}
	}
	if !foundLocaleField {
		t.Fatalf("expected common locale field in confluence descriptor, got %#v", confluence.Fields)
	}
	if len(confluence.Requirements) == 0 || len(confluence.Requirements[0].AnyOf) == 0 {
		t.Fatalf("expected confluence auth requirements, got %#v", confluence.Requirements)
	}
}

func TestKnowledgeSourceCreateAndUpdateLocale(t *testing.T) {
	docRoot := t.TempDir()
	store, err := knowledge.NewSQLiteStore(filepath.Join(t.TempDir(), "knowledge.db"))
	if err != nil {
		t.Fatalf("NewSQLiteStore() error = %v", err)
	}
	svc, err := knowledge.NewService(store, nil)
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}
	gw := newTestGatewayFull(t)
	gw.SetKnowledgeService(svc)

	rec := doRequest(t, gw.Handler(), "POST", "/operator/knowledge/sources", `{"name":"Locale Docs","kind":"local_dir","path":"`+docRoot+`","enabled":true,"locale":"zh-CN"}`)
	if rec.Code != 201 {
		t.Fatalf("POST /operator/knowledge/sources status = %d body=%s", rec.Code, rec.Body.String())
	}
	var created struct {
		Item knowledgeSourceView `json:"item"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode create response: %v", err)
	}
	if created.Item.Locale != "zh-CN" {
		t.Fatalf("created locale = %q, want zh-CN", created.Item.Locale)
	}

	rec = doRequest(t, gw.Handler(), "PATCH", "/operator/knowledge/sources/"+created.Item.ID, `{"locale":"en"}`)
	if rec.Code != 200 {
		t.Fatalf("PATCH /operator/knowledge/sources/{id} status = %d body=%s", rec.Code, rec.Body.String())
	}
	var updated struct {
		Item knowledgeSourceView `json:"item"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &updated); err != nil {
		t.Fatalf("decode patch response: %v", err)
	}
	if updated.Item.Locale != "en" {
		t.Fatalf("updated locale = %q, want en", updated.Item.Locale)
	}

	rec = doRequest(t, gw.Handler(), "GET", "/operator/knowledge/sources/"+created.Item.ID, "")
	if rec.Code != 200 {
		t.Fatalf("GET /operator/knowledge/sources/{id} status = %d body=%s", rec.Code, rec.Body.String())
	}
	var fetched struct {
		Item knowledgeSourceView `json:"item"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &fetched); err != nil {
		t.Fatalf("decode get response: %v", err)
	}
	if fetched.Item.Locale != "en" {
		t.Fatalf("fetched locale = %q, want en", fetched.Item.Locale)
	}
}

func TestKnowledgeSearchLocaleAwareAndSourceViewExposesSyncCursor(t *testing.T) {
	docRoot := t.TempDir()
	if err := os.WriteFile(filepath.Join(docRoot, "en.md"), []byte("Rollback guide for production incidents."), 0o644); err != nil {
		t.Fatalf("write en.md: %v", err)
	}
	if err := os.WriteFile(filepath.Join(docRoot, "zh.md"), []byte("回滚指南与生产故障处理流程。"), 0o644); err != nil {
		t.Fatalf("write zh.md: %v", err)
	}

	store, err := knowledge.NewSQLiteStore(filepath.Join(t.TempDir(), "knowledge.db"))
	if err != nil {
		t.Fatalf("NewSQLiteStore() error = %v", err)
	}
	svc, err := knowledge.NewService(store, gatewayKnowledgeEmbeddingClient{})
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}
	gw := newTestGatewayFull(t)
	gw.SetKnowledgeService(svc)

	rec := doRequest(t, gw.Handler(), "POST", "/operator/knowledge/sources", `{"name":"Hybrid Docs","kind":"local_dir","path":"`+docRoot+`","enabled":true}`)
	if rec.Code != 201 {
		t.Fatalf("POST /operator/knowledge/sources status = %d body=%s", rec.Code, rec.Body.String())
	}
	var created struct {
		Item knowledgeSourceView `json:"item"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode create response: %v", err)
	}

	rec = doRequest(t, gw.Handler(), "POST", "/operator/knowledge/sources/"+created.Item.ID+"/sync", "")
	if rec.Code != 200 {
		t.Fatalf("POST /operator/knowledge/sources/{id}/sync status = %d body=%s", rec.Code, rec.Body.String())
	}

	rec = doRequest(t, gw.Handler(), "GET", "/operator/knowledge/sources/"+created.Item.ID, "")
	if rec.Code != 200 {
		t.Fatalf("GET /operator/knowledge/sources/{id} status = %d body=%s", rec.Code, rec.Body.String())
	}
	var fetched struct {
		Item knowledgeSourceView `json:"item"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &fetched); err != nil {
		t.Fatalf("decode get response: %v", err)
	}
	if strings.TrimSpace(fetched.Item.SyncCursor) == "" {
		t.Fatalf("expected sync cursor in source view, got %#v", fetched.Item)
	}

	rec = doRequest(t, gw.Handler(), "GET", "/operator/knowledge/search?q=回滚&source_id="+created.Item.ID, "")
	if rec.Code != 200 {
		t.Fatalf("GET /operator/knowledge/search status = %d body=%s", rec.Code, rec.Body.String())
	}
	var search knowledgeSearchResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &search); err != nil {
		t.Fatalf("decode search response: %v", err)
	}
	if len(search.Items) < 2 {
		t.Fatalf("expected cross-locale results, got %#v", search.Items)
	}
	if search.Items[0].Locale != "zh-CN" {
		t.Fatalf("expected locale-aware search to prefer zh-CN, got %#v", search.Items[0])
	}
	foundEnglish := false
	for _, item := range search.Items {
		if item.Path == "en.md" {
			foundEnglish = true
			break
		}
	}
	if !foundEnglish {
		t.Fatalf("expected cross-language retrieval to include en.md, got %#v", search.Items)
	}
}

func TestKnowledgeSourceGetRedactsSecrets(t *testing.T) {
	store, err := knowledge.NewSQLiteStore(filepath.Join(t.TempDir(), "knowledge.db"))
	if err != nil {
		t.Fatalf("NewSQLiteStore() error = %v", err)
	}
	svc, err := knowledge.NewService(store, nil)
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}
	source, err := svc.UpsertSource(context.Background(), knowledge.Source{
		Name:    "Notion Private",
		Kind:    knowledge.SourceKindNotion,
		Enabled: true,
		Config: map[string]any{
			"base_url": "https://api.notion.com",
			"token":    "super-secret",
			"page_ids": []string{"12345678123412341234123456789abc"},
		},
	})
	if err != nil {
		t.Fatalf("UpsertSource() error = %v", err)
	}
	gw := newTestGatewayFull(t)
	gw.SetKnowledgeService(svc)

	rec := doRequest(t, gw.Handler(), "GET", "/operator/knowledge/sources/"+source.ID, "")
	if rec.Code != 200 {
		t.Fatalf("GET /operator/knowledge/sources/{id} status = %d body=%s", rec.Code, rec.Body.String())
	}
	var payload map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	item, _ := payload["item"].(map[string]any)
	config, _ := item["config"].(map[string]any)
	if _, ok := config["token"]; ok {
		t.Fatalf("expected token to be redacted, payload=%s", rec.Body.String())
	}
	secrets, _ := item["configured_secrets"].([]any)
	if len(secrets) == 0 {
		t.Fatalf("expected configured secrets, payload=%s", rec.Body.String())
	}
}

func TestKnowledgeSourceCreateStoresSecretsInKeychain(t *testing.T) {
	store := newTestKeychainStore()
	original := keychain.CurrentStore()
	keychain.SetStore(store)
	t.Cleanup(func() { keychain.SetStore(original) })

	fileStore, err := knowledge.NewSQLiteStore(filepath.Join(t.TempDir(), "knowledge.db"))
	if err != nil {
		t.Fatalf("NewSQLiteStore() error = %v", err)
	}
	svc, err := knowledge.NewService(fileStore, nil)
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}
	gw := newTestGatewayFull(t)
	gw.SetKnowledgeService(svc)

	rec := doRequest(t, gw.Handler(), "POST", "/operator/knowledge/sources", `{
		"name":"Google Workspace Docs",
		"kind":"google_drive",
		"enabled":true,
		"config":{
			"base_url":"https://www.googleapis.com",
			"token":"super-secret-token",
			"file_ids":["file123"]
		}
	}`)
	if rec.Code != 201 {
		t.Fatalf("POST /operator/knowledge/sources status = %d body=%s", rec.Code, rec.Body.String())
	}
	var created struct {
		Item knowledgeSourceView `json:"item"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode create response: %v", err)
	}
	if len(created.Item.ConfiguredSecrets) != 1 || created.Item.ConfiguredSecrets[0] != "token" {
		t.Fatalf("configured secrets = %#v", created.Item.ConfiguredSecrets)
	}

	source, err := svc.GetSource(context.Background(), created.Item.ID)
	if err != nil {
		t.Fatalf("GetSource() error = %v", err)
	}
	if source == nil {
		t.Fatal("expected stored source")
	}
	tokenRef := source.Config["token"]
	if tokenRef == nil {
		t.Fatalf("expected token ref in stored config: %#v", source.Config)
	}
	tokenRefString := tokenRef.(string)
	if tokenRefString == "super-secret-token" || tokenRefString == "" {
		t.Fatalf("expected keychain ref, got %q", tokenRefString)
	}
	if !strings.HasPrefix(tokenRefString, "keychain:") {
		t.Fatalf("expected keychain ref, got %q", tokenRefString)
	}
	secretKey := strings.TrimPrefix(tokenRefString, "keychain:")
	resolved, err := store.Get(keychain.DefaultService(), secretKey)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if resolved != "super-secret-token" {
		t.Fatalf("stored secret = %q", resolved)
	}

	rec = doRequest(t, gw.Handler(), "DELETE", "/operator/knowledge/sources/"+created.Item.ID, "")
	if rec.Code != 200 {
		t.Fatalf("DELETE /operator/knowledge/sources/{id} status = %d body=%s", rec.Code, rec.Body.String())
	}
	if _, err := store.Get(keychain.DefaultService(), secretKey); err == nil {
		t.Fatal("expected secret to be removed after delete")
	}
}

func TestKnowledgeSourceCreateRollsBackStagedSecretsWhenStoreUpsertFails(t *testing.T) {
	store := newTestKeychainStore()
	original := keychain.CurrentStore()
	keychain.SetStore(store)
	t.Cleanup(func() { keychain.SetStore(original) })

	baseStore, err := knowledge.NewSQLiteStore(filepath.Join(t.TempDir(), "knowledge.db"))
	if err != nil {
		t.Fatalf("NewSQLiteStore() error = %v", err)
	}
	svc, err := knowledge.NewService(&failingKnowledgeStore{
		base:             baseStore,
		failOnUpsertCall: 1,
		upsertErr:        errors.New("boom"),
	}, nil)
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}
	gw := newTestGatewayFull(t)
	gw.SetKnowledgeService(svc)

	rec := doRequest(t, gw.Handler(), "POST", "/operator/knowledge/sources", `{
		"name":"Failing Google Docs",
		"kind":"google_drive",
		"enabled":true,
		"config":{
			"token":"super-secret-token",
			"file_ids":["file123"]
		}
	}`)
	if rec.Code != 400 {
		t.Fatalf("POST /operator/knowledge/sources status = %d body=%s", rec.Code, rec.Body.String())
	}
	if got := store.count(keychain.DefaultService()); got != 0 {
		t.Fatalf("staged secrets not rolled back, count = %d", got)
	}
}

func TestKnowledgeSourceDeleteKeepsUserManagedKeychainRefs(t *testing.T) {
	store := newTestKeychainStore()
	original := keychain.CurrentStore()
	keychain.SetStore(store)
	t.Cleanup(func() { keychain.SetStore(original) })

	const sharedKey = "corp-shared-google-drive-token"
	if err := store.Set(keychain.DefaultService(), sharedKey, "shared-secret-token"); err != nil {
		t.Fatalf("Set() error = %v", err)
	}

	fileStore, err := knowledge.NewSQLiteStore(filepath.Join(t.TempDir(), "knowledge.db"))
	if err != nil {
		t.Fatalf("NewSQLiteStore() error = %v", err)
	}
	svc, err := knowledge.NewService(fileStore, nil)
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}
	gw := newTestGatewayFull(t)
	gw.SetKnowledgeService(svc)

	rec := doRequest(t, gw.Handler(), "POST", "/operator/knowledge/sources", `{
		"name":"Shared Google Docs",
		"kind":"google_drive",
		"enabled":true,
		"config":{
			"base_url":"https://www.googleapis.com",
			"token":"keychain:`+sharedKey+`",
			"file_ids":["file123"]
		}
	}`)
	if rec.Code != 201 {
		t.Fatalf("POST /operator/knowledge/sources status = %d body=%s", rec.Code, rec.Body.String())
	}
	var created struct {
		Item knowledgeSourceView `json:"item"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode create response: %v", err)
	}

	rec = doRequest(t, gw.Handler(), "DELETE", "/operator/knowledge/sources/"+created.Item.ID, "")
	if rec.Code != 200 {
		t.Fatalf("DELETE /operator/knowledge/sources/{id} status = %d body=%s", rec.Code, rec.Body.String())
	}
	resolved, err := store.Get(keychain.DefaultService(), sharedKey)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if resolved != "shared-secret-token" {
		t.Fatalf("stored secret = %q", resolved)
	}
}

func TestKnowledgeSourceUpdateRemovesReplacedManagedSecrets(t *testing.T) {
	tests := []struct {
		name         string
		patchBody    string
		wantTokenRef string
		setupStore   func(*testing.T, *testKeychainStore)
		verifyStore  func(*testing.T, *testKeychainStore)
	}{
		{
			name:         "switch to env ref",
			patchBody:    `{"config":{"token":"env:GOOGLE_DRIVE_TOKEN"}}`,
			wantTokenRef: "env:GOOGLE_DRIVE_TOKEN",
		},
		{
			name:         "switch to external keychain ref",
			patchBody:    `{"config":{"token":"keychain:corp-shared-google-drive-token"}}`,
			wantTokenRef: "keychain:corp-shared-google-drive-token",
			setupStore: func(t *testing.T, store *testKeychainStore) {
				t.Helper()
				if err := store.Set(keychain.DefaultService(), "corp-shared-google-drive-token", "shared-secret-token"); err != nil {
					t.Fatalf("Set() error = %v", err)
				}
			},
			verifyStore: func(t *testing.T, store *testKeychainStore) {
				t.Helper()
				resolved, err := store.Get(keychain.DefaultService(), "corp-shared-google-drive-token")
				if err != nil {
					t.Fatalf("Get() error = %v", err)
				}
				if resolved != "shared-secret-token" {
					t.Fatalf("stored secret = %q", resolved)
				}
			},
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			store := newTestKeychainStore()
			original := keychain.CurrentStore()
			keychain.SetStore(store)
			t.Cleanup(func() { keychain.SetStore(original) })

			if tt.setupStore != nil {
				tt.setupStore(t, store)
			}

			fileStore, err := knowledge.NewSQLiteStore(filepath.Join(t.TempDir(), "knowledge.db"))
			if err != nil {
				t.Fatalf("NewSQLiteStore() error = %v", err)
			}
			svc, err := knowledge.NewService(fileStore, nil)
			if err != nil {
				t.Fatalf("NewService() error = %v", err)
			}
			gw := newTestGatewayFull(t)
			gw.SetKnowledgeService(svc)

			rec := doRequest(t, gw.Handler(), "POST", "/operator/knowledge/sources", `{
				"name":"Google Workspace Docs",
				"kind":"google_drive",
				"enabled":true,
				"config":{
					"base_url":"https://www.googleapis.com",
					"token":"super-secret-token",
					"file_ids":["file123"]
				}
			}`)
			if rec.Code != 201 {
				t.Fatalf("POST /operator/knowledge/sources status = %d body=%s", rec.Code, rec.Body.String())
			}
			var created struct {
				Item knowledgeSourceView `json:"item"`
			}
			if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
				t.Fatalf("decode create response: %v", err)
			}

			source, err := svc.GetSource(context.Background(), created.Item.ID)
			if err != nil {
				t.Fatalf("GetSource() error = %v", err)
			}
			if source == nil {
				t.Fatal("expected stored source")
			}
			tokenRef := knowledgeConfigString(source.Config, "token")
			if !strings.HasPrefix(tokenRef, "keychain:") {
				t.Fatalf("expected managed keychain ref, got %q", tokenRef)
			}
			managedKey := strings.TrimPrefix(tokenRef, "keychain:")

			rec = doRequest(t, gw.Handler(), "PATCH", "/operator/knowledge/sources/"+created.Item.ID, tt.patchBody)
			if rec.Code != 200 {
				t.Fatalf("PATCH /operator/knowledge/sources/{id} status = %d body=%s", rec.Code, rec.Body.String())
			}

			updated, err := svc.GetSource(context.Background(), created.Item.ID)
			if err != nil {
				t.Fatalf("GetSource(updated) error = %v", err)
			}
			if updated == nil {
				t.Fatal("expected updated source")
			}
			if got := knowledgeConfigString(updated.Config, "token"); got != tt.wantTokenRef {
				t.Fatalf("updated token ref = %q, want %q", got, tt.wantTokenRef)
			}

			if _, err := store.Get(keychain.DefaultService(), managedKey); !errors.Is(err, keychain.ErrNotFound) {
				t.Fatalf("managed key still present after update: err=%v", err)
			}
			if tt.verifyStore != nil {
				tt.verifyStore(t, store)
			}
		})
	}
}

func TestKnowledgeSourceUpdateRollsBackStagedSecretWhenStoreUpsertFails(t *testing.T) {
	store := newTestKeychainStore()
	original := keychain.CurrentStore()
	keychain.SetStore(store)
	t.Cleanup(func() { keychain.SetStore(original) })

	baseStore, err := knowledge.NewSQLiteStore(filepath.Join(t.TempDir(), "knowledge.db"))
	if err != nil {
		t.Fatalf("NewSQLiteStore() error = %v", err)
	}
	wrappedStore := &failingKnowledgeStore{
		base:             baseStore,
		failOnUpsertCall: 2,
		upsertErr:        errors.New("boom"),
	}
	svc, err := knowledge.NewService(wrappedStore, nil)
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}
	gw := newTestGatewayFull(t)
	gw.SetKnowledgeService(svc)

	createRec := doRequest(t, gw.Handler(), "POST", "/operator/knowledge/sources", `{
		"name":"Google Workspace Docs",
		"kind":"google_drive",
		"enabled":true,
		"config":{
			"token":"super-secret-token",
			"file_ids":["file123"]
		}
	}`)
	if createRec.Code != 201 {
		t.Fatalf("POST /operator/knowledge/sources status = %d body=%s", createRec.Code, createRec.Body.String())
	}
	var created struct {
		Item knowledgeSourceView `json:"item"`
	}
	if err := json.Unmarshal(createRec.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode create response: %v", err)
	}

	source, err := svc.GetSource(context.Background(), created.Item.ID)
	if err != nil {
		t.Fatalf("GetSource() error = %v", err)
	}
	if source == nil {
		t.Fatal("expected stored source")
	}
	oldRef := knowledgeConfigString(source.Config, "token")
	oldKey := strings.TrimPrefix(oldRef, "keychain:")
	oldValue, err := store.Get(keychain.DefaultService(), oldKey)
	if err != nil {
		t.Fatalf("Get(old key) error = %v", err)
	}

	updateRec := doRequest(t, gw.Handler(), "PATCH", "/operator/knowledge/sources/"+created.Item.ID, `{"config":{"token":"rotated-secret-token"}}`)
	if updateRec.Code != 400 {
		t.Fatalf("PATCH /operator/knowledge/sources/{id} status = %d body=%s", updateRec.Code, updateRec.Body.String())
	}

	reloaded, err := svc.GetSource(context.Background(), created.Item.ID)
	if err != nil {
		t.Fatalf("GetSource(reloaded) error = %v", err)
	}
	if reloaded == nil {
		t.Fatal("expected source to remain after failed update")
	}
	if got := knowledgeConfigString(reloaded.Config, "token"); got != oldRef {
		t.Fatalf("token ref after failed update = %q, want %q", got, oldRef)
	}
	currentValue, err := store.Get(keychain.DefaultService(), oldKey)
	if err != nil {
		t.Fatalf("Get(old key after failure) error = %v", err)
	}
	if currentValue != oldValue {
		t.Fatalf("old managed secret mutated to %q, want %q", currentValue, oldValue)
	}
	if got := store.count(keychain.DefaultService()); got != 1 {
		t.Fatalf("unexpected keychain entry count after rollback = %d", got)
	}
}

func TestKnowledgeSourceDeleteDeletesSourceBeforeBestEffortSecretCleanup(t *testing.T) {
	store := newTestKeychainStore()
	store.delErr = errors.New("delete failed")
	original := keychain.CurrentStore()
	keychain.SetStore(store)
	t.Cleanup(func() { keychain.SetStore(original) })

	fileStore, err := knowledge.NewSQLiteStore(filepath.Join(t.TempDir(), "knowledge.db"))
	if err != nil {
		t.Fatalf("NewSQLiteStore() error = %v", err)
	}
	svc, err := knowledge.NewService(fileStore, nil)
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}
	gw := newTestGatewayFull(t)
	gw.SetKnowledgeService(svc)

	rec := doRequest(t, gw.Handler(), "POST", "/operator/knowledge/sources", `{
		"name":"Google Workspace Docs",
		"kind":"google_drive",
		"enabled":true,
		"config":{
			"token":"super-secret-token",
			"file_ids":["file123"]
		}
	}`)
	if rec.Code != 201 {
		t.Fatalf("POST /operator/knowledge/sources status = %d body=%s", rec.Code, rec.Body.String())
	}
	var created struct {
		Item knowledgeSourceView `json:"item"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode create response: %v", err)
	}

	rec = doRequest(t, gw.Handler(), "DELETE", "/operator/knowledge/sources/"+created.Item.ID, "")
	if rec.Code != 200 {
		t.Fatalf("DELETE /operator/knowledge/sources/{id} status = %d body=%s", rec.Code, rec.Body.String())
	}
	reloaded, err := svc.GetSource(context.Background(), created.Item.ID)
	if err != nil {
		t.Fatalf("GetSource(after delete) error = %v", err)
	}
	if reloaded != nil {
		t.Fatalf("expected source to be deleted, got %#v", reloaded)
	}
	if got := store.count(keychain.DefaultService()); got != 1 {
		t.Fatalf("expected failed cleanup to leave orphaned secret for later reconciliation, count = %d", got)
	}
}

func TestKnowledgeSourceCreateRejectsTrailingJSON(t *testing.T) {
	fileStore, err := knowledge.NewSQLiteStore(filepath.Join(t.TempDir(), "knowledge.db"))
	if err != nil {
		t.Fatalf("NewSQLiteStore() error = %v", err)
	}
	svc, err := knowledge.NewService(fileStore, nil)
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}
	gw := newTestGatewayFull(t)
	gw.SetKnowledgeService(svc)

	rec := doRequest(t, gw.Handler(), "POST", "/operator/knowledge/sources", `{"name":"Docs","kind":"local_dir","path":"`+t.TempDir()+`"} {"extra":true}`)
	if rec.Code != 400 {
		t.Fatalf("POST /operator/knowledge/sources trailing json status = %d body=%s", rec.Code, rec.Body.String())
	}

	items, err := svc.ListSources(context.Background())
	if err != nil {
		t.Fatalf("ListSources() error = %v", err)
	}
	if len(items) != 0 {
		t.Fatalf("expected no stored sources, got %d", len(items))
	}
}

func TestKnowledgeSourceUpdateRejectsTrailingJSON(t *testing.T) {
	fileStore, err := knowledge.NewSQLiteStore(filepath.Join(t.TempDir(), "knowledge.db"))
	if err != nil {
		t.Fatalf("NewSQLiteStore() error = %v", err)
	}
	svc, err := knowledge.NewService(fileStore, nil)
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}
	source, err := svc.UpsertSource(context.Background(), knowledge.Source{
		Name:    "FAQ",
		Kind:    knowledge.SourceKindLocalDir,
		Path:    t.TempDir(),
		Enabled: true,
	})
	if err != nil {
		t.Fatalf("UpsertSource() error = %v", err)
	}
	gw := newTestGatewayFull(t)
	gw.SetKnowledgeService(svc)

	rec := doRequest(t, gw.Handler(), "PATCH", "/operator/knowledge/sources/"+source.ID, `{"enabled":false} {"extra":true}`)
	if rec.Code != 400 {
		t.Fatalf("PATCH /operator/knowledge/sources/{id} trailing json status = %d body=%s", rec.Code, rec.Body.String())
	}

	reloaded, err := svc.GetSource(context.Background(), source.ID)
	if err != nil {
		t.Fatalf("GetSource() error = %v", err)
	}
	if reloaded == nil || !reloaded.Enabled {
		t.Fatalf("expected source to remain enabled, got %#v", reloaded)
	}
}

type gatewayKnowledgeEmbeddingClient struct{}

func (gatewayKnowledgeEmbeddingClient) Embed(_ context.Context, texts []string) ([][]float32, error) {
	out := make([][]float32, 0, len(texts))
	for _, text := range texts {
		lower := strings.ToLower(strings.TrimSpace(text))
		switch {
		case strings.Contains(lower, "rollback"), strings.Contains(text, "回滚"):
			out = append(out, []float32{1, 0, 0})
		case strings.Contains(lower, "invoice"), strings.Contains(text, "发票"):
			out = append(out, []float32{0, 1, 0})
		default:
			out = append(out, []float32{0, 0, 1})
		}
	}
	return out, nil
}
