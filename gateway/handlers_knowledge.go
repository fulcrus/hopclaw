package gateway

import (
	"net/http"
	"time"

	"github.com/fulcrus/hopclaw/knowledge"
)

const knowledgeRequestTimeout = 20 * time.Second
const knowledgeSecretKeyNamespace = "knowledge.sources"

type knowledgeSourceRequest struct {
	ID           string         `json:"id,omitempty"`
	Name         string         `json:"name,omitempty"`
	Kind         string         `json:"kind,omitempty"`
	Enabled      *bool          `json:"enabled,omitempty"`
	Locale       string         `json:"locale,omitempty"`
	Path         string         `json:"path,omitempty"`
	URLs         []string       `json:"urls,omitempty"`
	Config       map[string]any `json:"config,omitempty"`
	IncludeGlobs []string       `json:"include_globs,omitempty"`
	ExcludeGlobs []string       `json:"exclude_globs,omitempty"`
}

type knowledgeSourceUpdateRequest struct {
	Name         *string        `json:"name,omitempty"`
	Enabled      *bool          `json:"enabled,omitempty"`
	Locale       *string        `json:"locale,omitempty"`
	Path         *string        `json:"path,omitempty"`
	URLs         *[]string      `json:"urls,omitempty"`
	Config       map[string]any `json:"config,omitempty"`
	IncludeGlobs *[]string      `json:"include_globs,omitempty"`
	ExcludeGlobs *[]string      `json:"exclude_globs,omitempty"`
}

type knowledgeSourceView struct {
	ID                string                `json:"id"`
	Name              string                `json:"name"`
	Kind              string                `json:"kind"`
	Enabled           bool                  `json:"enabled"`
	Locale            string                `json:"locale,omitempty"`
	Path              string                `json:"path,omitempty"`
	URLs              []string              `json:"urls,omitempty"`
	Config            map[string]any        `json:"config,omitempty"`
	ConfiguredSecrets []string              `json:"configured_secrets,omitempty"`
	IncludeGlobs      []string              `json:"include_globs,omitempty"`
	ExcludeGlobs      []string              `json:"exclude_globs,omitempty"`
	Status            string                `json:"status,omitempty"`
	LastSyncAt        time.Time             `json:"last_sync_at,omitempty"`
	SyncCursor        string                `json:"sync_cursor,omitempty"`
	LastError         string                `json:"last_error,omitempty"`
	Stats             knowledge.SourceStats `json:"stats,omitempty"`
	CreatedAt         time.Time             `json:"created_at,omitempty"`
	UpdatedAt         time.Time             `json:"updated_at,omitempty"`
	ConnectorNote     string                `json:"connector_note,omitempty"`
}

type knowledgeSourcesListResponse struct {
	Items          []knowledgeSourceView            `json:"items"`
	Count          int                              `json:"count"`
	SupportedKinds []knowledge.SourceKindDescriptor `json:"supported_kinds,omitempty"`
}

type knowledgeSearchResponse struct {
	Items []knowledge.SearchResult `json:"items"`
	Count int                      `json:"count"`
	Query string                   `json:"query"`
}

type knowledgeSourceEnvelope struct {
	Item knowledgeSourceView `json:"item"`
}

type knowledgeActionResponse struct {
	OK   bool                `json:"ok"`
	Item knowledgeSourceView `json:"item,omitempty"`
	ID   string              `json:"id,omitempty"`
}

// handleKnowledgeSourcesList returns configured external knowledge sources.
//
//	GET /operator/knowledge/sources
func (g *Gateway) handleKnowledgeSourcesList(w http.ResponseWriter, r *http.Request) {
	svc := g.requireKnowledgeService(w)
	if svc == nil {
		return
	}
	ctx, cancel := knowledgeRequestContext(r.Context())
	defer cancel()

	items, err := svc.ListSources(ctx)
	if err != nil {
		gwError(w, http.StatusInternalServerError, err.Error())
		return
	}
	gwJSON(w, http.StatusOK, knowledgeSourcesListResponse{
		Items:          knowledgeSourcesListPayload(items).Items,
		Count:          len(items),
		SupportedKinds: knowledge.SupportedSourceKinds(),
	})
}

// handleKnowledgeSourcesGet returns one configured source.
//
//	GET /operator/knowledge/sources/{id}
func (g *Gateway) handleKnowledgeSourcesGet(w http.ResponseWriter, r *http.Request) {
	svc := g.requireKnowledgeService(w)
	if svc == nil {
		return
	}
	id, ok := knowledgeSourceIDFromPath(w, r)
	if !ok {
		return
	}
	ctx, cancel := knowledgeRequestContext(r.Context())
	defer cancel()

	source, status, err := loadKnowledgeSource(ctx, svc, id)
	if err != nil {
		gwError(w, status, err.Error())
		return
	}
	gwJSON(w, http.StatusOK, knowledgeSourceEnvelope{Item: sourceToView(*source)})
}

// handleKnowledgeSourcesCreate creates a knowledge source.
//
//	POST /operator/knowledge/sources
func (g *Gateway) handleKnowledgeSourcesCreate(w http.ResponseWriter, r *http.Request) {
	svc := g.requireKnowledgeService(w)
	if svc == nil {
		return
	}
	var req knowledgeSourceRequest
	if !decodeGatewayJSONBody(w, r, &req) {
		return
	}
	ctx, cancel := knowledgeRequestContext(r.Context())
	defer cancel()

	created, status, err := createKnowledgeSource(ctx, svc, req)
	if err != nil {
		gwError(w, status, err.Error())
		return
	}
	gwJSON(w, http.StatusCreated, knowledgeActionResponse{OK: true, Item: sourceToView(created)})
}

// handleKnowledgeSourcesUpdate patches a knowledge source.
//
//	PATCH /operator/knowledge/sources/{id}
func (g *Gateway) handleKnowledgeSourcesUpdate(w http.ResponseWriter, r *http.Request) {
	svc := g.requireKnowledgeService(w)
	if svc == nil {
		return
	}
	id, ok := knowledgeSourceIDFromPath(w, r)
	if !ok {
		return
	}
	var req knowledgeSourceUpdateRequest
	if !decodeGatewayJSONBody(w, r, &req) {
		return
	}
	ctx, cancel := knowledgeRequestContext(r.Context())
	defer cancel()

	source, status, err := loadKnowledgeSource(ctx, svc, id)
	if err != nil {
		gwError(w, status, err.Error())
		return
	}
	plan, err := applyKnowledgeSourceUpdate(*source, req)
	if err != nil {
		gwError(w, http.StatusBadRequest, err.Error())
		return
	}
	out, err := svc.UpsertSource(ctx, plan.source)
	if err != nil {
		if cleanupErr := plan.rollback(); cleanupErr != nil {
			log.Warn("knowledge source update secret rollback failed", "source_id", plan.source.ID, "error", cleanupErr)
		}
		gwError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := plan.reconcile(); err != nil {
		log.Warn("knowledge source replaced secret cleanup failed", "source_id", out.ID, "error", err)
	}
	gwJSON(w, http.StatusOK, knowledgeActionResponse{OK: true, Item: sourceToView(out)})
}

// handleKnowledgeSourcesDelete removes a knowledge source.
//
//	DELETE /operator/knowledge/sources/{id}
func (g *Gateway) handleKnowledgeSourcesDelete(w http.ResponseWriter, r *http.Request) {
	svc := g.requireKnowledgeService(w)
	if svc == nil {
		return
	}
	id, ok := knowledgeSourceIDFromPath(w, r)
	if !ok {
		return
	}
	ctx, cancel := knowledgeRequestContext(r.Context())
	defer cancel()
	source, status, err := loadKnowledgeSource(ctx, svc, id)
	if err != nil && status != http.StatusNotFound {
		gwError(w, status, err.Error())
		return
	}
	if err := svc.DeleteSource(ctx, id); err != nil {
		gwError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if source != nil {
		if err := cleanupKnowledgeSourceSecrets(*source); err != nil {
			log.Warn("knowledge source delete secret cleanup failed", "source_id", source.ID, "error", err)
		}
	}
	gwJSON(w, http.StatusOK, knowledgeActionResponse{OK: true, ID: id})
}

// handleKnowledgeSourcesSync refreshes one source into the knowledge index.
//
//	POST /operator/knowledge/sources/{id}/sync
func (g *Gateway) handleKnowledgeSourcesSync(w http.ResponseWriter, r *http.Request) {
	svc := g.requireKnowledgeService(w)
	if svc == nil {
		return
	}
	id, ok := knowledgeSourceIDFromPath(w, r)
	if !ok {
		return
	}
	ctx, cancel := knowledgeRequestContext(r.Context())
	defer cancel()

	result, err := svc.SyncSource(ctx, id)
	if err != nil {
		gwError(w, http.StatusBadRequest, err.Error())
		return
	}
	gwJSON(w, http.StatusOK, okResultResponse{OK: true, Result: result})
}

// handleKnowledgeSearch searches indexed chunks from configured sources.
//
//	GET /operator/knowledge/search?q=...
func (g *Gateway) handleKnowledgeSearch(w http.ResponseWriter, r *http.Request) {
	svc := g.requireKnowledgeService(w)
	if svc == nil {
		return
	}
	filter, err := knowledgeSearchFilterFromRequest(r)
	if err != nil {
		gwError(w, http.StatusBadRequest, err.Error())
		return
	}
	ctx, cancel := knowledgeRequestContext(r.Context())
	defer cancel()

	items, err := svc.Search(ctx, filter)
	if err != nil {
		gwError(w, http.StatusBadRequest, err.Error())
		return
	}
	gwJSON(w, http.StatusOK, knowledgeSearchResponse{
		Items: items,
		Count: len(items),
		Query: filter.Query,
	})
}
