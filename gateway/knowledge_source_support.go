package gateway

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/fulcrus/hopclaw/keychain"
	"github.com/fulcrus/hopclaw/knowledge"
)

func (g *Gateway) requireKnowledgeService(w http.ResponseWriter) *knowledge.Service {
	if g == nil || g.knowledge == nil {
		gwError(w, http.StatusServiceUnavailable, "knowledge service not available")
		return nil
	}
	return g.knowledge
}

func knowledgeRequestContext(parent context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(parent, knowledgeRequestTimeout)
}

func knowledgeSourceIDFromPath(w http.ResponseWriter, r *http.Request) (string, bool) {
	id := strings.TrimSpace(r.PathValue("id"))
	if id == "" {
		gwError(w, http.StatusBadRequest, "missing source id")
		return "", false
	}
	return id, true
}

func knowledgeSourcesListPayload(items []knowledge.Source) knowledgeSourcesListResponse {
	views := make([]knowledgeSourceView, 0, len(items))
	for _, item := range items {
		views = append(views, sourceToView(item))
	}
	return knowledgeSourcesListResponse{
		Items:          views,
		Count:          len(views),
		SupportedKinds: knowledge.SupportedSourceKinds(),
	}
}

func loadKnowledgeSource(ctx context.Context, svc *knowledge.Service, id string) (*knowledge.Source, int, error) {
	source, err := svc.GetSource(ctx, strings.TrimSpace(id))
	if err != nil {
		return nil, http.StatusInternalServerError, err
	}
	if source == nil {
		return nil, http.StatusNotFound, errors.New("knowledge source not found")
	}
	return source, 0, nil
}

type knowledgeSecretMutationPlan struct {
	source      knowledge.Source
	stagedKeys  []string
	cleanupKeys []string
}

func createKnowledgeSource(ctx context.Context, svc *knowledge.Service, req knowledgeSourceRequest) (knowledge.Source, int, error) {
	source, err := buildKnowledgeSourceFromRequest(req)
	if err != nil {
		return knowledge.Source{}, http.StatusBadRequest, err
	}
	source = ensureKnowledgeSourceID(source)
	plan, err := prepareKnowledgeSourceSecretMutation(nil, source)
	if err != nil {
		return knowledge.Source{}, http.StatusBadRequest, err
	}
	created, err := svc.UpsertSource(ctx, plan.source)
	if err != nil {
		if cleanupErr := plan.rollback(); cleanupErr != nil {
			log.Warn("knowledge source create secret rollback failed", "source_id", plan.source.ID, "error", cleanupErr)
		}
		return knowledge.Source{}, http.StatusBadRequest, err
	}
	return created, 0, nil
}

func applyKnowledgeSourceUpdate(source knowledge.Source, req knowledgeSourceUpdateRequest) (knowledgeSecretMutationPlan, error) {
	updated := source
	if req.Name != nil {
		updated.Name = strings.TrimSpace(*req.Name)
	}
	if req.Enabled != nil {
		updated.Enabled = *req.Enabled
	}
	if req.Locale != nil {
		updated.Locale = strings.TrimSpace(*req.Locale)
	}
	if req.Path != nil {
		updated.Path = strings.TrimSpace(*req.Path)
	}
	if req.URLs != nil {
		updated.URLs = append([]string(nil), (*req.URLs)...)
	}
	if len(req.Config) > 0 {
		updated.Config = mergeKnowledgeConfig(updated.Kind, updated.Config, req.Config)
	}
	if req.IncludeGlobs != nil {
		updated.IncludeGlobs = append([]string(nil), (*req.IncludeGlobs)...)
	}
	if req.ExcludeGlobs != nil {
		updated.ExcludeGlobs = append([]string(nil), (*req.ExcludeGlobs)...)
	}
	updated, err := knowledge.NormalizeSource(updated)
	if err != nil {
		return knowledgeSecretMutationPlan{}, err
	}
	return prepareKnowledgeSourceSecretMutation(&source, updated)
}

func knowledgeSearchFilterFromRequest(r *http.Request) (knowledge.SearchFilter, error) {
	query := strings.TrimSpace(r.URL.Query().Get("q"))
	if query == "" {
		return knowledge.SearchFilter{}, errors.New("query is required")
	}
	limit := 8
	if raw := strings.TrimSpace(r.URL.Query().Get("limit")); raw != "" {
		if value, err := strconv.Atoi(raw); err == nil && value > 0 {
			limit = value
		}
	}
	return knowledge.SearchFilter{
		Query:    query,
		SourceID: strings.TrimSpace(r.URL.Query().Get("source_id")),
		Locale:   strings.TrimSpace(r.URL.Query().Get("locale")),
		Limit:    limit,
	}, nil
}

func buildKnowledgeSourceFromRequest(req knowledgeSourceRequest) (knowledge.Source, error) {
	source := knowledge.Source{
		ID:           strings.TrimSpace(req.ID),
		Name:         strings.TrimSpace(req.Name),
		Kind:         knowledge.SourceKind(strings.TrimSpace(req.Kind)),
		Enabled:      true,
		Locale:       strings.TrimSpace(req.Locale),
		Path:         strings.TrimSpace(req.Path),
		URLs:         append([]string(nil), req.URLs...),
		Config:       cloneKnowledgeConfig(req.Config),
		IncludeGlobs: append([]string(nil), req.IncludeGlobs...),
		ExcludeGlobs: append([]string(nil), req.ExcludeGlobs...),
	}
	if req.Enabled != nil {
		source.Enabled = *req.Enabled
	}
	return knowledge.NormalizeSource(source)
}

func knowledgeConfigString(config map[string]any, key string) string {
	if len(config) == 0 {
		return ""
	}
	raw, ok := config[key]
	if !ok || raw == nil {
		return ""
	}
	switch typed := raw.(type) {
	case string:
		return strings.TrimSpace(typed)
	default:
		return strings.TrimSpace(fmt.Sprintf("%v", typed))
	}
}

func sourceToView(source knowledge.Source) knowledgeSourceView {
	return knowledgeSourceView{
		ID:                source.ID,
		Name:              source.Name,
		Kind:              string(source.Kind),
		Enabled:           source.Enabled,
		Locale:            source.Locale,
		Path:              source.Path,
		URLs:              append([]string(nil), source.URLs...),
		Config:            sanitizeKnowledgeConfig(source.Kind, source.Config),
		ConfiguredSecrets: configuredKnowledgeSecrets(source.Kind, source.Config),
		IncludeGlobs:      append([]string(nil), source.IncludeGlobs...),
		ExcludeGlobs:      append([]string(nil), source.ExcludeGlobs...),
		Status:            string(source.Status),
		LastSyncAt:        source.LastSyncAt,
		SyncCursor:        source.SyncCursor,
		LastError:         source.LastError,
		Stats:             source.Stats,
		CreatedAt:         source.CreatedAt,
		UpdatedAt:         source.UpdatedAt,
		ConnectorNote:     source.ConnectorNote,
	}
}

func cloneKnowledgeConfig(config map[string]any) map[string]any {
	if len(config) == 0 {
		return nil
	}
	out := make(map[string]any, len(config))
	for key, value := range config {
		out[key] = cloneKnowledgeConfigValue(value)
	}
	return out
}

func cloneKnowledgeConfigValue(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		return cloneKnowledgeConfig(typed)
	case []any:
		out := make([]any, 0, len(typed))
		for _, item := range typed {
			out = append(out, cloneKnowledgeConfigValue(item))
		}
		return out
	case []string:
		return append([]string(nil), typed...)
	default:
		return typed
	}
}

func mergeKnowledgeConfig(kind knowledge.SourceKind, existing map[string]any, updates map[string]any) map[string]any {
	merged := cloneKnowledgeConfig(existing)
	if merged == nil {
		merged = map[string]any{}
	}
	for key, value := range updates {
		if value == nil {
			continue
		}
		if knowledge.IsSecretField(kind, key) {
			trimmed := strings.TrimSpace(fmt.Sprintf("%v", value))
			if trimmed == "" || trimmed == "***" {
				continue
			}
		}
		merged[key] = cloneKnowledgeConfigValue(value)
	}
	return knowledge.NormalizeSourceConfig(kind, merged)
}

func sanitizeKnowledgeConfig(kind knowledge.SourceKind, config map[string]any) map[string]any {
	cfg := cloneKnowledgeConfig(config)
	if len(cfg) == 0 {
		return nil
	}
	for key := range cfg {
		if knowledge.IsSecretField(kind, key) {
			delete(cfg, key)
		}
	}
	return cfg
}

func configuredKnowledgeSecrets(kind knowledge.SourceKind, config map[string]any) []string {
	fields := knowledge.SecretFieldsForKind(kind)
	out := make([]string, 0, len(fields))
	for _, field := range fields {
		if strings.TrimSpace(knowledgeConfigString(config, field)) != "" {
			out = append(out, field)
		}
	}
	return out
}

func ensureKnowledgeSourceID(source knowledge.Source) knowledge.Source {
	if strings.TrimSpace(source.ID) != "" {
		return source
	}
	base := strings.ToLower(strings.TrimSpace(source.Name))
	base = strings.ReplaceAll(base, " ", "-")
	base = strings.ReplaceAll(base, "_", "-")
	if base == "" {
		base = "source"
	}
	source.ID = string(source.Kind) + "-" + base
	return source
}

func prepareKnowledgeSourceSecretMutation(previous *knowledge.Source, source knowledge.Source) (knowledgeSecretMutationPlan, error) {
	plan := knowledgeSecretMutationPlan{source: source}
	if len(source.Config) == 0 {
		if previous != nil {
			plan.cleanupKeys = collectReplacedKnowledgeSourceSecretKeys(*previous, source)
		}
		return plan, nil
	}
	cfg := cloneKnowledgeConfig(source.Config)
	for _, field := range knowledge.SecretFieldsForKind(source.Kind) {
		value := knowledgeConfigString(cfg, field)
		if value == "" {
			continue
		}
		ref, stagedKey, err := normalizeKnowledgeSecretValue(source.ID, field, value)
		if err != nil {
			_ = deleteManagedKnowledgeSecretKeys(plan.stagedKeys)
			return knowledgeSecretMutationPlan{}, err
		}
		cfg[field] = ref
		if strings.TrimSpace(stagedKey) != "" {
			plan.stagedKeys = append(plan.stagedKeys, stagedKey)
		}
	}
	source.Config = cfg
	plan.source = source
	if previous != nil {
		plan.cleanupKeys = collectReplacedKnowledgeSourceSecretKeys(*previous, source)
	}
	return plan, nil
}

func normalizeKnowledgeSecretValue(sourceID string, field string, value string) (string, string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", "", nil
	}
	if strings.HasPrefix(value, "keychain:") || strings.HasPrefix(value, "env:") {
		return value, "", nil
	}
	key := knowledgeSecretKey(sourceID, field)
	if err := keychain.SaveSecret(key, value); err != nil {
		return "", "", fmt.Errorf("store knowledge source secret %s/%s: %w", sourceID, field, err)
	}
	return "keychain:" + key, key, nil
}

func (p knowledgeSecretMutationPlan) rollback() error {
	return deleteManagedKnowledgeSecretKeys(p.stagedKeys)
}

func (p knowledgeSecretMutationPlan) reconcile() error {
	return deleteManagedKnowledgeSecretKeys(p.cleanupKeys)
}

func cleanupKnowledgeSourceSecrets(source knowledge.Source) error {
	for _, field := range knowledge.SecretFieldsForKind(source.Kind) {
		key, ok := managedKnowledgeSecretKeyFromSource(source, field)
		if !ok {
			continue
		}
		if err := keychain.DeleteSecret(key); err != nil && !errors.Is(err, keychain.ErrNotFound) {
			return fmt.Errorf("delete knowledge source secret %s/%s: %w", source.ID, field, err)
		}
	}
	return nil
}

func deleteManagedKnowledgeSecretKeys(keys []string) error {
	for _, key := range keys {
		trimmed := strings.TrimSpace(key)
		if trimmed == "" {
			continue
		}
		if err := keychain.DeleteSecret(trimmed); err != nil && !errors.Is(err, keychain.ErrNotFound) {
			return fmt.Errorf("delete knowledge source secret %s: %w", trimmed, err)
		}
	}
	return nil
}

func cleanupReplacedKnowledgeSourceSecrets(previous, updated knowledge.Source) error {
	return deleteManagedKnowledgeSecretKeys(collectReplacedKnowledgeSourceSecretKeys(previous, updated))
}

func collectReplacedKnowledgeSourceSecretKeys(previous, updated knowledge.Source) []string {
	keys := make([]string, 0)
	for _, field := range knowledge.SecretFieldsForKind(previous.Kind) {
		oldKey, ok := managedKnowledgeSecretKeyFromSource(previous, field)
		if !ok {
			continue
		}
		newKey, newManaged := managedKnowledgeSecretKeyFromSource(updated, field)
		if newManaged && strings.TrimSpace(newKey) == strings.TrimSpace(oldKey) {
			continue
		}
		keys = append(keys, oldKey)
	}
	return keys
}

func knowledgeSecretKey(sourceID string, field string) string {
	return knowledgeManagedSecretKeyPrefix(sourceID, field) + "." + strconv.FormatInt(time.Now().UTC().UnixNano(), 36)
}

func knowledgeManagedSecretKeyPrefix(sourceID string, field string) string {
	return knowledgeSecretKeyNamespace + ".managed." + sanitizeKnowledgeSecretKeyPart(sourceID) + "." + sanitizeKnowledgeSecretKeyPart(field)
}

func legacyKnowledgeSecretKey(sourceID string, field string) string {
	return knowledgeSecretKeyNamespace + "." + sanitizeKnowledgeSecretKeyPart(sourceID) + "." + sanitizeKnowledgeSecretKeyPart(field)
}

func managedKnowledgeSecretKeyFromSource(source knowledge.Source, field string) (string, bool) {
	value := knowledgeConfigString(source.Config, field)
	if !strings.HasPrefix(value, "keychain:") {
		return "", false
	}
	key := strings.TrimSpace(strings.TrimPrefix(value, "keychain:"))
	if key == "" || !isManagedKnowledgeSecretKey(source.ID, field, key) {
		return "", false
	}
	return key, true
}

func isManagedKnowledgeSecretKey(sourceID string, field string, key string) bool {
	trimmed := strings.TrimSpace(key)
	if trimmed == "" {
		return false
	}
	currentPrefix := knowledgeManagedSecretKeyPrefix(sourceID, field)
	legacyKey := legacyKnowledgeSecretKey(sourceID, field)
	return trimmed == legacyKey || trimmed == currentPrefix || strings.HasPrefix(trimmed, currentPrefix+".")
}

func sanitizeKnowledgeSecretKeyPart(value string) string {
	value = strings.TrimSpace(strings.ToLower(value))
	if value == "" {
		return "value"
	}
	var builder strings.Builder
	for _, r := range value {
		switch {
		case r >= 'a' && r <= 'z':
			builder.WriteRune(r)
		case r >= '0' && r <= '9':
			builder.WriteRune(r)
		default:
			builder.WriteByte('-')
		}
	}
	normalized := strings.Trim(builder.String(), "-")
	if normalized == "" {
		return "value"
	}
	return normalized
}
