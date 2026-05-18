package gateway

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/fulcrus/hopclaw/config"
	"github.com/fulcrus/hopclaw/durablefact"
)

type durableFactsView string

const (
	durableFactsViewOperator durableFactsView = "operator"
	durableFactsViewContext  durableFactsView = "context"
	durableFactsViewConfig   durableFactsView = "config"
)

type durableFactsResponseMeta struct {
	View                string `json:"view"`
	Count               int    `json:"count"`
	ContextCount        int    `json:"context_count"`
	ConfigCount         int    `json:"config_count"`
	ReviewRequiredCount int    `json:"review_required_count"`
}

type durableFactsOperatorResponse struct {
	durableFactsResponseMeta
	Items []durablefact.OperatorView `json:"items"`
}

type durableFactsContextResponse struct {
	durableFactsResponseMeta
	Items []durablefact.ContextView `json:"items"`
}

type durableFactsConfigResponse struct {
	durableFactsResponseMeta
	Items []durablefact.ConfigView `json:"items"`
}

type durableFactProviderPayload struct {
	API          string   `json:"api,omitempty"`
	BaseURL      string   `json:"base_url,omitempty"`
	Region       string   `json:"region,omitempty"`
	APIKey       string   `json:"api_key,omitempty"`
	APIKeys      []string `json:"api_keys,omitempty"`
	AccessKeyID  string   `json:"access_key_id,omitempty"`
	SecretKey    string   `json:"secret_key,omitempty"`
	SessionToken string   `json:"session_token,omitempty"`
	DefaultModel string   `json:"default_model,omitempty"`
	TimeoutSec   int      `json:"timeout_sec,omitempty"`
	Headers      string   `json:"headers,omitempty"`
	Enabled      *bool    `json:"enabled,omitempty"`
	YAMLHash     string   `json:"yaml_hash,omitempty"`
}

type durableFactChannelPayload struct {
	Config   string `json:"config"`
	Enabled  *bool  `json:"enabled,omitempty"`
	YAMLHash string `json:"yaml_hash,omitempty"`
}

type durableFactSettingPayload struct {
	Value    string `json:"value"`
	YAMLHash string `json:"yaml_hash,omitempty"`
}

// handleDurableFactsList returns the current DurableFact projections exposed by
// the configured memory and config stores.
//
//	GET /operator/durable-facts
func (g *Gateway) handleDurableFactsList(w http.ResponseWriter, r *http.Request) {
	view, err := parseDurableFactsView(r.URL.Query().Get("view"))
	if err != nil {
		gwError(w, http.StatusBadRequest, err.Error())
		return
	}
	filter, err := durableFactsFilterFromQuery(r)
	if err != nil {
		gwError(w, http.StatusBadRequest, err.Error())
		return
	}

	switch view {
	case durableFactsViewContext:
		items, supported, err := g.listContextDurableFacts(r.Context(), filter)
		if err != nil {
			gwError(w, http.StatusInternalServerError, err.Error())
			return
		}
		if !supported {
			gwError(w, http.StatusServiceUnavailable, "memory durable facts not available")
			return
		}
		gwJSON(w, http.StatusOK, durableFactsContextResponse{
			durableFactsResponseMeta: durableFactsResponseMeta{
				View:                string(view),
				Count:               len(items),
				ContextCount:        len(items),
				ConfigCount:         0,
				ReviewRequiredCount: countContextReviewRequired(items),
			},
			Items: items,
		})
	case durableFactsViewConfig:
		items, supported, err := g.listConfigDurableFacts(r.Context(), filter)
		if err != nil {
			gwError(w, http.StatusInternalServerError, err.Error())
			return
		}
		if !supported {
			gwError(w, http.StatusServiceUnavailable, "config durable facts not available")
			return
		}
		gwJSON(w, http.StatusOK, durableFactsConfigResponse{
			durableFactsResponseMeta: durableFactsResponseMeta{
				View:                string(view),
				Count:               len(items),
				ContextCount:        0,
				ConfigCount:         len(items),
				ReviewRequiredCount: countConfigReviewRequired(items),
			},
			Items: items,
		})
	default:
		items, contextCount, configCount, supported, err := g.listOperatorDurableFacts(r.Context(), filter)
		if err != nil {
			gwError(w, http.StatusInternalServerError, err.Error())
			return
		}
		if !supported {
			gwError(w, http.StatusServiceUnavailable, "durable facts not available")
			return
		}
		gwJSON(w, http.StatusOK, durableFactsOperatorResponse{
			durableFactsResponseMeta: durableFactsResponseMeta{
				View:                string(view),
				Count:               len(items),
				ContextCount:        contextCount,
				ConfigCount:         configCount,
				ReviewRequiredCount: countOperatorReviewRequired(items),
			},
			Items: items,
		})
	}
}

func parseDurableFactsView(raw string) (durableFactsView, error) {
	switch durableFactsView(strings.TrimSpace(raw)) {
	case "", durableFactsViewOperator:
		return durableFactsViewOperator, nil
	case durableFactsViewContext:
		return durableFactsViewContext, nil
	case durableFactsViewConfig:
		return durableFactsViewConfig, nil
	default:
		return "", fmt.Errorf("invalid durable facts view %q", strings.TrimSpace(raw))
	}
}

func durableFactsFilterFromQuery(r *http.Request) (durablefact.Filter, error) {
	filter := durablefact.Filter{
		Namespace: strings.TrimSpace(r.URL.Query().Get("namespace")),
		ScopeKey:  strings.TrimSpace(r.URL.Query().Get("scope_key")),
		Query:     strings.TrimSpace(r.URL.Query().Get("q")),
	}
	if raw := strings.TrimSpace(r.URL.Query().Get("fact_class")); raw != "" {
		class, err := parseDurableFactClass(raw)
		if err != nil {
			return durablefact.Filter{}, err
		}
		filter.FactClass = class
	}
	if raw := strings.TrimSpace(r.URL.Query().Get("review_required")); raw != "" {
		reviewRequired, err := strconv.ParseBool(raw)
		if err != nil {
			return durablefact.Filter{}, fmt.Errorf("invalid review_required %q", raw)
		}
		filter.ReviewRequired = &reviewRequired
	}
	return filter, nil
}

func parseDurableFactClass(raw string) (durablefact.FactClass, error) {
	switch durablefact.FactClass(strings.TrimSpace(raw)) {
	case durablefact.FactClassPreference:
		return durablefact.FactClassPreference, nil
	case durablefact.FactClassAgreement:
		return durablefact.FactClassAgreement, nil
	case durablefact.FactClassBusinessRule:
		return durablefact.FactClassBusinessRule, nil
	case durablefact.FactClassSystemConfig:
		return durablefact.FactClassSystemConfig, nil
	case durablefact.FactClassImportedNote:
		return durablefact.FactClassImportedNote, nil
	default:
		return "", fmt.Errorf("invalid fact_class %q", strings.TrimSpace(raw))
	}
}

func (g *Gateway) listContextDurableFacts(ctx context.Context, filter durablefact.Filter) ([]durablefact.ContextView, bool, error) {
	if g == nil || g.runtime == nil {
		return nil, false, nil
	}
	items, supported, err := g.runtime.ListMemoryContextViews(ctx, filter)
	if err != nil || !supported {
		return items, supported, err
	}
	sort.Slice(items, func(i, j int) bool {
		return durableContextViewLess(items[i], items[j])
	})
	return items, true, nil
}

func (g *Gateway) listConfigDurableFacts(ctx context.Context, filter durablefact.Filter) ([]durablefact.ConfigView, bool, error) {
	if g == nil || g.configStore == nil {
		return nil, false, nil
	}
	items, err := g.configStore.ListConfigViews(ctx, filter)
	if err != nil {
		return nil, true, err
	}
	items, err = sanitizeConfigDurableFactViews(items)
	if err != nil {
		return nil, true, err
	}
	sort.Slice(items, func(i, j int) bool {
		return durableConfigViewLess(items[i], items[j])
	})
	return items, true, nil
}

func (g *Gateway) listOperatorDurableFacts(ctx context.Context, filter durablefact.Filter) ([]durablefact.OperatorView, int, int, bool, error) {
	items := make([]durablefact.OperatorView, 0)
	contextCount := 0
	configCount := 0
	supported := false

	if g != nil && g.runtime != nil {
		contextItems, memorySupported, err := g.runtime.ListMemoryOperatorViews(ctx, filter)
		if err != nil {
			return nil, 0, 0, false, err
		}
		if memorySupported {
			supported = true
			contextCount = len(contextItems)
			items = append(items, contextItems...)
		}
	}

	if g != nil && g.configStore != nil {
		configItems, err := g.configStore.ListOperatorViews(ctx, filter)
		if err != nil {
			return nil, 0, 0, false, err
		}
		configItems, err = sanitizeOperatorDurableFactViews(configItems)
		if err != nil {
			return nil, 0, 0, false, err
		}
		supported = true
		configCount = len(configItems)
		items = append(items, configItems...)
	}

	sort.Slice(items, func(i, j int) bool {
		return durableOperatorViewLess(items[i], items[j])
	})
	return items, contextCount, configCount, supported, nil
}

func durableContextViewLess(left, right durablefact.ContextView) bool {
	if left.Namespace != right.Namespace {
		return left.Namespace < right.Namespace
	}
	if left.ScopeKey != right.ScopeKey {
		return left.ScopeKey < right.ScopeKey
	}
	if left.Field != right.Field {
		return left.Field < right.Field
	}
	return left.Key < right.Key
}

func durableConfigViewLess(left, right durablefact.ConfigView) bool {
	if left.Kind != right.Kind {
		return left.Kind < right.Kind
	}
	if left.Name != right.Name {
		return left.Name < right.Name
	}
	return left.Key < right.Key
}

func durableOperatorViewLess(left, right durablefact.OperatorView) bool {
	if left.ViewType != right.ViewType {
		return left.ViewType < right.ViewType
	}
	if left.Namespace != right.Namespace {
		return left.Namespace < right.Namespace
	}
	if left.ScopeKey != right.ScopeKey {
		return left.ScopeKey < right.ScopeKey
	}
	if left.Name != right.Name {
		return left.Name < right.Name
	}
	return left.Key < right.Key
}

func countContextReviewRequired(items []durablefact.ContextView) int {
	count := 0
	for _, item := range items {
		if item.ReviewRequired {
			count++
		}
	}
	return count
}

func countConfigReviewRequired(items []durablefact.ConfigView) int {
	count := 0
	for _, item := range items {
		if item.ReviewRequired {
			count++
		}
	}
	return count
}

func countOperatorReviewRequired(items []durablefact.OperatorView) int {
	count := 0
	for _, item := range items {
		if item.ReviewRequired {
			count++
		}
	}
	return count
}

func sanitizeConfigDurableFactViews(items []durablefact.ConfigView) ([]durablefact.ConfigView, error) {
	sanitized := make([]durablefact.ConfigView, 0, len(items))
	for _, item := range items {
		next, err := sanitizeConfigDurableFactView(item)
		if err != nil {
			return nil, err
		}
		sanitized = append(sanitized, next)
	}
	return sanitized, nil
}

func sanitizeConfigDurableFactView(item durablefact.ConfigView) (durablefact.ConfigView, error) {
	payload, err := sanitizeDurableConfigPayload(item.Kind, item.Name, item.Payload)
	if err != nil {
		return durablefact.ConfigView{}, err
	}
	item.Payload = payload
	return item, nil
}

func sanitizeOperatorDurableFactViews(items []durablefact.OperatorView) ([]durablefact.OperatorView, error) {
	sanitized := make([]durablefact.OperatorView, 0, len(items))
	for _, item := range items {
		next, err := sanitizeOperatorDurableFactView(item)
		if err != nil {
			return nil, err
		}
		sanitized = append(sanitized, next)
	}
	return sanitized, nil
}

func sanitizeOperatorDurableFactView(item durablefact.OperatorView) (durablefact.OperatorView, error) {
	var kind durablefact.ConfigViewKind
	switch item.ViewType {
	case durablefact.ViewTypeConfigProvider:
		kind = durablefact.ConfigViewKindProvider
	case durablefact.ViewTypeConfigChannel:
		kind = durablefact.ConfigViewKindChannel
	case durablefact.ViewTypeConfigSetting:
		kind = durablefact.ConfigViewKindSetting
	default:
		return item, nil
	}

	value, err := sanitizeDurableConfigPayload(kind, item.Name, item.Value)
	if err != nil {
		return durablefact.OperatorView{}, err
	}
	item.Value = value
	return item, nil
}

func sanitizeDurableConfigPayload(kind durablefact.ConfigViewKind, name, raw string) (string, error) {
	switch kind {
	case durablefact.ConfigViewKindProvider:
		return sanitizeDurableProviderPayload(name, raw)
	case durablefact.ConfigViewKindChannel:
		return sanitizeDurableChannelPayload(name, raw)
	case durablefact.ConfigViewKindSetting:
		return sanitizeDurableSettingPayload(name, raw)
	default:
		return raw, nil
	}
}

func sanitizeDurableProviderPayload(name, raw string) (string, error) {
	var payload durableFactProviderPayload
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		return "", fmt.Errorf("decode provider durable fact %q: %w", strings.TrimSpace(name), err)
	}

	var headers map[string]string
	if trimmed := strings.TrimSpace(payload.Headers); trimmed != "" && trimmed != "{}" && trimmed != "null" {
		if err := json.Unmarshal([]byte(trimmed), &headers); err != nil {
			return "", fmt.Errorf("decode provider headers for %q: %w", strings.TrimSpace(name), err)
		}
	}

	sanitized := config.SanitizeProviderConfigForOperator(strings.TrimSpace(name), config.ProviderConfig{
		API:          payload.API,
		BaseURL:      payload.BaseURL,
		Region:       payload.Region,
		APIKey:       payload.APIKey,
		APIKeys:      append([]string(nil), payload.APIKeys...),
		AccessKeyID:  payload.AccessKeyID,
		SecretKey:    payload.SecretKey,
		SessionToken: payload.SessionToken,
		DefaultModel: payload.DefaultModel,
		Timeout:      time.Duration(payload.TimeoutSec) * time.Second,
		Headers:      headers,
	})

	headersJSON := strings.TrimSpace(payload.Headers)
	switch {
	case len(sanitized.Headers) > 0:
		data, err := json.Marshal(sanitized.Headers)
		if err != nil {
			return "", fmt.Errorf("encode provider headers for %q: %w", strings.TrimSpace(name), err)
		}
		headersJSON = string(data)
	case headersJSON == "", headersJSON == "null":
		headersJSON = ""
	default:
		headersJSON = "{}"
	}

	payload.API = sanitized.API
	payload.BaseURL = sanitized.BaseURL
	payload.Region = sanitized.Region
	payload.APIKey = sanitized.APIKey
	payload.APIKeys = append([]string(nil), sanitized.APIKeys...)
	payload.AccessKeyID = sanitized.AccessKeyID
	payload.SecretKey = sanitized.SecretKey
	payload.SessionToken = sanitized.SessionToken
	payload.DefaultModel = sanitized.DefaultModel
	payload.TimeoutSec = int(sanitized.Timeout / time.Second)
	payload.Headers = headersJSON

	data, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("encode provider durable fact %q: %w", strings.TrimSpace(name), err)
	}
	return string(data), nil
}

func sanitizeDurableChannelPayload(name, raw string) (string, error) {
	var payload durableFactChannelPayload
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		return "", fmt.Errorf("decode channel durable fact %q: %w", strings.TrimSpace(name), err)
	}

	sanitized, err := config.SanitizeStoredChannelConfigForOperator(strings.TrimSpace(name), json.RawMessage(payload.Config))
	if err != nil {
		return "", fmt.Errorf("sanitize channel durable fact %q: %w", strings.TrimSpace(name), err)
	}
	payload.Config = strings.TrimSpace(string(sanitized))
	if payload.Config == "" {
		payload.Config = "{}"
	}

	data, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("encode channel durable fact %q: %w", strings.TrimSpace(name), err)
	}
	return string(data), nil
}

func sanitizeDurableSettingPayload(name, raw string) (string, error) {
	section, ok := config.SectionOverlayName(name)
	if !ok {
		return raw, nil
	}

	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return raw, nil
	}

	var envelope map[string]json.RawMessage
	if err := json.Unmarshal([]byte(trimmed), &envelope); err != nil {
		return "", fmt.Errorf("decode setting durable fact %q: %w", strings.TrimSpace(name), err)
	}

	if rawValue, ok := envelope["value"]; ok && len(envelope) <= 2 {
		var payload durableFactSettingPayload
		if err := json.Unmarshal([]byte(trimmed), &payload); err != nil {
			return "", fmt.Errorf("decode setting durable fact %q: %w", strings.TrimSpace(name), err)
		}
		var value any
		if err := json.Unmarshal(rawValue, &payload.Value); err != nil {
			return "", fmt.Errorf("decode setting envelope for %q: %w", strings.TrimSpace(name), err)
		}
		if err := json.Unmarshal([]byte(payload.Value), &value); err != nil {
			return "", fmt.Errorf("decode setting payload for %q: %w", strings.TrimSpace(name), err)
		}

		sanitized, err := config.SanitizeSectionValueForOperator(section, value)
		if err != nil {
			return "", fmt.Errorf("sanitize setting durable fact %q: %w", strings.TrimSpace(name), err)
		}
		data, err := json.Marshal(sanitized)
		if err != nil {
			return "", fmt.Errorf("encode setting payload for %q: %w", strings.TrimSpace(name), err)
		}
		payload.Value = string(data)

		body, err := json.Marshal(payload)
		if err != nil {
			return "", fmt.Errorf("encode setting durable fact %q: %w", strings.TrimSpace(name), err)
		}
		return string(body), nil
	}

	var value any
	if err := json.Unmarshal([]byte(trimmed), &value); err != nil {
		return "", fmt.Errorf("decode setting payload for %q: %w", strings.TrimSpace(name), err)
	}

	sanitized, err := config.SanitizeSectionValueForOperator(section, value)
	if err != nil {
		return "", fmt.Errorf("sanitize setting durable fact %q: %w", strings.TrimSpace(name), err)
	}
	body, err := json.Marshal(sanitized)
	if err != nil {
		return "", fmt.Errorf("encode setting durable fact %q: %w", strings.TrimSpace(name), err)
	}
	return string(body), nil
}
