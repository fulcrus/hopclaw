package config

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"sort"
	"strconv"
	"strings"

	"github.com/fulcrus/hopclaw/keychain"
	"github.com/fulcrus/hopclaw/store"
	"gopkg.in/yaml.v3"
)

const (
	managedProviderSecretPrefix = "config.providers."
	managedChannelSecretPrefix  = "config.channels."
	managedSectionSecretPrefix  = "config.sections."
)

// NormalizeProviderConfigForStore rewrites literal provider secrets to managed
// keychain references and returns any superseded managed refs that should be
// deleted after the store update succeeds.
func NormalizeProviderConfigForStore(name string, current ProviderConfig, next ProviderConfig) (ProviderConfig, []string, error) {
	current = NormalizeProviderConfig(name, current)
	next = NormalizeProviderConfig(name, next)

	if IsSecretPlaceholder(next.APIKey) {
		next.APIKey = current.APIKey
	}
	for index := range next.APIKeys {
		if IsSecretPlaceholder(next.APIKeys[index]) && index < len(current.APIKeys) {
			next.APIKeys[index] = current.APIKeys[index]
		}
	}
	if IsSecretPlaceholder(next.SecretKey) {
		next.SecretKey = current.SecretKey
	}
	if IsSecretPlaceholder(next.SessionToken) {
		next.SessionToken = current.SessionToken
	}
	if len(next.Headers) > 0 {
		headers := cloneProviderStringMap(next.Headers)
		for key, value := range headers {
			if IsSecretPlaceholder(value) {
				headers[key] = current.Headers[key]
			}
		}
		next.Headers = headers
	}

	var err error
	if next.APIKey, err = normalizeManagedSecretValue("providers", name, "api_key", next.APIKey); err != nil {
		return ProviderConfig{}, nil, err
	}
	for index := range next.APIKeys {
		next.APIKeys[index], err = normalizeManagedSecretValue("providers", name, fmt.Sprintf("api_keys.%d", index), next.APIKeys[index])
		if err != nil {
			return ProviderConfig{}, nil, err
		}
	}
	if next.SecretKey, err = normalizeManagedSecretValue("providers", name, "secret_key", next.SecretKey); err != nil {
		return ProviderConfig{}, nil, err
	}
	if next.SessionToken, err = normalizeManagedSecretValue("providers", name, "session_token", next.SessionToken); err != nil {
		return ProviderConfig{}, nil, err
	}
	if len(next.Headers) > 0 {
		headers := cloneProviderStringMap(next.Headers)
		for _, key := range sensitiveHeaderKeys(headers) {
			headers[key], err = normalizeManagedSecretValue("providers", name, "headers."+key, headers[key])
			if err != nil {
				return ProviderConfig{}, nil, err
			}
		}
		next.Headers = headers
	}

	next = NormalizeProviderConfig(name, next)
	cleanup := managedSecretCleanupRefs(providerSecretValues(current), providerSecretValues(next))
	return next, cleanup, nil
}

// ProviderSecretCleanupRefs returns managed refs owned by the provider config.
// Callers typically use it before deleting a provider overlay row.
func ProviderSecretCleanupRefs(current ProviderConfig) []string {
	return managedSecretCleanupRefs(providerSecretValues(current), nil)
}

// NormalizeSectionForStore applies a section update to the current config,
// preserves operator placeholders, rewrites literal secrets in that section to
// managed keychain refs, and returns the normalized config plus the extracted
// section payload for persistence.
func NormalizeSectionForStore(current Config, section string, sectionValue any) (Config, any, []string, error) {
	next, err := ApplySection(current, section, sectionValue)
	if err != nil {
		return Config{}, nil, nil, err
	}
	normalized, cleanup, err := normalizeSectionSecrets(current, next, section)
	if err != nil {
		return Config{}, nil, nil, err
	}
	value, err := ExtractSection(normalized, section)
	if err != nil {
		return Config{}, nil, nil, err
	}
	payload, err := canonicalSectionPayload(value)
	if err != nil {
		return Config{}, nil, nil, err
	}
	return normalized, payload, cleanup, nil
}

// NormalizeSectionValueForStore rewrites literal secrets inside a stored
// section payload without requiring a full valid root config. It is used by the
// startup migrator for existing dynamic_settings rows.
func NormalizeSectionValueForStore(section string, value any) (any, []string, error) {
	section = strings.TrimSpace(section)
	if section == "" {
		return value, nil, nil
	}
	next, err := decodeSectionValueIntoConfig(section, value)
	if err != nil {
		return nil, nil, err
	}
	normalized, cleanup, err := normalizeSectionSecrets(Config{}, next, section)
	if err != nil {
		return nil, nil, err
	}
	extracted, err := ExtractSection(normalized, section)
	if err != nil {
		return nil, nil, err
	}
	payload, err := canonicalSectionPayload(extracted)
	if err != nil {
		return nil, nil, err
	}
	return payload, cleanup, nil
}

// MergeChannelConfig merges an operator channel patch with the current raw
// channel config, preserving masked secrets. The returned payload stays raw;
// callers that persist into SQLite must pass it through
// NormalizeChannelConfigForStore.
func MergeChannelConfig(name string, currentRaw, incomingRaw json.RawMessage) (json.RawMessage, bool, error) {
	canonical, ok := canonicalChannelName(name)
	if ok {
		payload, err := mergeKnownChannelConfig(canonical, currentRaw, incomingRaw)
		return payload, true, err
	}
	payload, err := mergeUnknownChannelConfig(currentRaw, incomingRaw)
	return payload, false, err
}

// NormalizeChannelConfigForStore rewrites literal channel secrets to managed
// keychain refs and returns any superseded managed refs that should be deleted
// after persistence succeeds.
func NormalizeChannelConfigForStore(name string, currentRaw, nextRaw json.RawMessage) (json.RawMessage, bool, []string, error) {
	canonical, ok := canonicalChannelName(name)
	if ok {
		payload, cleanup, err := normalizeKnownChannelConfigForStore(canonical, currentRaw, nextRaw)
		return payload, true, cleanup, err
	}
	payload, cleanup, err := normalizeUnknownChannelConfigForStore(strings.TrimSpace(name), currentRaw, nextRaw)
	return payload, false, cleanup, err
}

// SanitizeChannelConfigForOperator returns an operator-safe channel config
// payload. Recognized built-in channels use exact config secret metadata;
// unknown blobs fall back to heuristic masking.
func SanitizeChannelConfigForOperator(name string, current Config, raw json.RawMessage, recognized bool) (json.RawMessage, error) {
	if recognized {
		canonical, ok := canonicalChannelName(name)
		if ok {
			return marshalKnownChannelConfig(current.SanitizeForOperator(), canonical)
		}
	}
	return sanitizeUnknownChannelConfigForOperator(raw)
}

// CleanupManagedSecretRefs deletes managed keychain refs that are no longer
// referenced after a successful store update.
func CleanupManagedSecretRefs(refs []string) error {
	var firstErr error
	for _, ref := range refs {
		key := managedSecretRefKey(ref)
		if key == "" {
			continue
		}
		if err := keychain.DeleteSecret(key); err != nil && err != keychain.ErrNotFound && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

// MigrateConfigStoreSecrets rewrites existing plaintext secrets in SQLite-backed
// config rows to managed keychain refs. It is safe to run multiple times.
func MigrateConfigStoreSecrets(ctx context.Context, configStore *store.ConfigStore) error {
	if configStore == nil {
		return nil
	}

	providerRows, err := configStore.ListProviders(ctx)
	if err != nil {
		return err
	}
	for _, row := range providerRows {
		current := RowToProviderConfig(&row)
		normalized, cleanup, err := NormalizeProviderConfigForStore(row.Name, current, current)
		if err != nil {
			return err
		}
		if !providerSecretMapsEqual(providerSecretValues(current), providerSecretValues(normalized)) {
			row.APIKey = normalized.APIKey
			row.APIKeys = append([]string(nil), normalized.APIKeys...)
			row.SecretKey = normalized.SecretKey
			row.SessionToken = normalized.SessionToken
			headersJSON := "{}"
			if len(normalized.Headers) > 0 {
				if data, marshalErr := json.Marshal(normalized.Headers); marshalErr == nil {
					headersJSON = string(data)
				}
			}
			row.Headers = headersJSON
			if err := configStore.UpsertProvider(ctx, &row); err != nil {
				return err
			}
			_ = CleanupManagedSecretRefs(cleanup)
		}
	}

	channelRows, err := configStore.ListChannels(ctx)
	if err != nil {
		return err
	}
	for _, row := range channelRows {
		normalized, _, cleanup, err := NormalizeChannelConfigForStore(row.Name, json.RawMessage(row.Config), json.RawMessage(row.Config))
		if err != nil {
			return err
		}
		nextConfig := strings.TrimSpace(string(normalized))
		if nextConfig == "" {
			nextConfig = "{}"
		}
		if nextConfig != strings.TrimSpace(row.Config) {
			row.Config = nextConfig
			if err := configStore.UpsertChannel(ctx, &row); err != nil {
				return err
			}
			_ = CleanupManagedSecretRefs(cleanup)
		}
	}

	settingRows, err := configStore.ListSettings(ctx)
	if err != nil {
		return err
	}
	for _, row := range settingRows {
		section, ok := overlaySectionName(row.Key)
		if !ok {
			continue
		}
		var value any
		if err := json.Unmarshal([]byte(row.Value), &value); err != nil {
			return fmt.Errorf("decode setting overlay %q: %w", row.Key, err)
		}
		normalizedValue, cleanup, err := NormalizeSectionValueForStore(section, value)
		if err != nil {
			return err
		}
		data, err := json.Marshal(normalizedValue)
		if err != nil {
			return fmt.Errorf("encode setting overlay %q: %w", row.Key, err)
		}
		if string(data) != strings.TrimSpace(row.Value) {
			row.Value = string(data)
			if err := configStore.UpsertSetting(ctx, &row); err != nil {
				return err
			}
			_ = CleanupManagedSecretRefs(cleanup)
		}
	}
	return nil
}

func normalizeSectionSecrets(current Config, next Config, section string) (Config, []string, error) {
	section = strings.TrimSpace(section)
	if section == "" {
		return next, nil, nil
	}
	normalized := next
	var firstErr error
	normalized.walkSecretFields(func(path string, value *string) {
		if firstErr != nil {
			return
		}
		relative, ok := relativeSectionSecretPath(path, section)
		if !ok {
			return
		}
		normalizedValue, err := normalizeManagedSecretValue("sections", section, relative, *value)
		if err != nil {
			firstErr = err
			return
		}
		*value = normalizedValue
	})
	if firstErr != nil {
		return Config{}, nil, firstErr
	}
	cleanup := managedSecretCleanupRefs(filterSectionSecretValues(current, section), filterSectionSecretValues(normalized, section))
	return normalized, cleanup, nil
}

func providerSecretValues(cfg ProviderConfig) map[string]string {
	cfg = NormalizeProviderConfig("", cfg)
	values := make(map[string]string, 8)
	if value := canonicalSecretStorageValue(cfg.APIKey); value != "" {
		values["api_key"] = value
	}
	for index, value := range cfg.APIKeys {
		if trimmed := canonicalSecretStorageValue(value); trimmed != "" {
			values[fmt.Sprintf("api_keys.%d", index)] = trimmed
		}
	}
	if value := canonicalSecretStorageValue(cfg.SecretKey); value != "" {
		values["secret_key"] = value
	}
	if value := canonicalSecretStorageValue(cfg.SessionToken); value != "" {
		values["session_token"] = value
	}
	for _, key := range sensitiveHeaderKeys(cfg.Headers) {
		if value := canonicalSecretStorageValue(cfg.Headers[key]); value != "" {
			values["headers."+key] = value
		}
	}
	return values
}

func providerSecretMapsEqual(left, right map[string]string) bool {
	if len(left) != len(right) {
		return false
	}
	for key, value := range left {
		if right[key] != value {
			return false
		}
	}
	return true
}

func managedSecretCleanupRefs(current, next map[string]string) []string {
	if len(current) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(current))
	refs := make([]string, 0, len(current))
	for path, currentValue := range current {
		if currentValue == next[path] {
			continue
		}
		ref := managedSecretRef(currentValue)
		if ref == "" {
			continue
		}
		if _, ok := seen[ref]; ok {
			continue
		}
		seen[ref] = struct{}{}
		refs = append(refs, ref)
	}
	sort.Strings(refs)
	return refs
}

func normalizeManagedSecretValue(domain, name, path, value string) (string, error) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return "", nil
	}
	if IsSecretPlaceholder(trimmed) {
		return trimmed, nil
	}
	kind, locator, ok := classifySecretRef(trimmed)
	if ok && kind != SecretRefKindLiteral {
		return locator, nil
	}
	key := managedConfigSecretKey(domain, name, path)
	if key == "" {
		return trimmed, nil
	}
	if err := keychain.SaveSecret(key, trimmed); err != nil {
		return "", fmt.Errorf("save managed secret %q: %w", key, err)
	}
	return "keychain:" + key, nil
}

func canonicalSecretStorageValue(value string) string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return ""
	}
	kind, locator, ok := classifySecretRef(trimmed)
	if !ok {
		return ""
	}
	if kind == SecretRefKindLiteral {
		return trimmed
	}
	return locator
}

func managedSecretRef(value string) string {
	value = strings.TrimSpace(value)
	if !strings.HasPrefix(value, "keychain:") {
		return ""
	}
	key := strings.TrimSpace(strings.TrimPrefix(value, "keychain:"))
	switch {
	case strings.HasPrefix(key, managedProviderSecretPrefix):
		return "keychain:" + key
	case strings.HasPrefix(key, managedChannelSecretPrefix):
		return "keychain:" + key
	case strings.HasPrefix(key, managedSectionSecretPrefix):
		return "keychain:" + key
	default:
		return ""
	}
}

func managedSecretRefKey(value string) string {
	ref := managedSecretRef(value)
	if ref == "" {
		return ""
	}
	return strings.TrimSpace(strings.TrimPrefix(ref, "keychain:"))
}

func managedConfigSecretKey(domain, name, path string) string {
	domain = strings.TrimSpace(domain)
	name = strings.TrimSpace(name)
	path = normalizeManagedSecretPath(path)
	switch {
	case domain == "" || name == "" || path == "":
		return ""
	default:
		return "config." + domain + "." + name + "." + path
	}
}

func normalizeManagedSecretPath(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return ""
	}
	replacer := strings.NewReplacer("[", ".", "]", "", " ", "")
	path = replacer.Replace(path)
	for strings.Contains(path, "..") {
		path = strings.ReplaceAll(path, "..", ".")
	}
	return strings.Trim(path, ".")
}

func filterSectionSecretValues(cfg Config, section string) map[string]string {
	values := make(map[string]string, 8)
	for path, value := range cfg.secretFieldValues() {
		relative, ok := relativeSectionSecretPath(path, section)
		if !ok {
			continue
		}
		values[relative] = canonicalSecretStorageValue(value)
	}
	return values
}

func relativeSectionSecretPath(path, section string) (string, bool) {
	section = strings.TrimSpace(section)
	prefix := section + "."
	if !strings.HasPrefix(path, prefix) {
		return "", false
	}
	return strings.TrimPrefix(path, prefix), true
}

func decodeSectionValueIntoConfig(section string, value any) (Config, error) {
	root := map[string]any{
		strings.TrimSpace(section): value,
	}
	data, err := yaml.Marshal(root)
	if err != nil {
		return Config{}, fmt.Errorf("encode section %q: %w", section, err)
	}
	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return Config{}, fmt.Errorf("decode section %q: %w", section, err)
	}
	return cfg, nil
}

func canonicalSectionPayload(value any) (any, error) {
	data, err := yaml.Marshal(value)
	if err != nil {
		return nil, fmt.Errorf("encode section payload: %w", err)
	}
	var payload any
	if err := yaml.Unmarshal(data, &payload); err != nil {
		return nil, fmt.Errorf("decode section payload: %w", err)
	}
	return payload, nil
}

func overlaySectionName(key string) (string, bool) {
	key = strings.TrimSpace(key)
	switch {
	case strings.HasPrefix(key, sectionOverlayKeyPrefix):
		name := strings.TrimSpace(strings.TrimPrefix(key, sectionOverlayKeyPrefix))
		return name, name != ""
	case strings.HasPrefix(key, sectionOverlayLegacyPrefix):
		name := strings.TrimSpace(strings.TrimPrefix(key, sectionOverlayLegacyPrefix))
		return name, name != ""
	default:
		return "", false
	}
}

func canonicalChannelName(name string) (string, bool) {
	target := strings.TrimSpace(strings.ToLower(name))
	target = strings.ReplaceAll(target, "-", "_")
	if target == "" {
		return "", false
	}
	typ := reflect.TypeOf(ChannelsConfig{})
	for index := 0; index < typ.NumField(); index++ {
		field := typ.Field(index)
		tag := yamlTagNameConfig(field.Tag.Get("yaml"))
		if tag == "" {
			continue
		}
		if strings.ReplaceAll(strings.ToLower(tag), "-", "_") == target {
			return tag, true
		}
	}
	return "", false
}

func mergeKnownChannelConfig(name string, currentRaw, incomingRaw json.RawMessage) (json.RawMessage, error) {
	currentCfg, err := decodeChannelConfig(name, currentRaw)
	if err != nil {
		return nil, err
	}
	nextCfg := currentCfg
	if len(incomingRaw) > 0 {
		if err := decodeChannelIntoConfig(&nextCfg, name, incomingRaw, true); err != nil {
			return nil, err
		}
	}
	preserveSecretPlaceholders(&nextCfg, currentCfg)
	return marshalKnownChannelConfig(nextCfg, name)
}

func normalizeKnownChannelConfigForStore(name string, currentRaw, nextRaw json.RawMessage) (json.RawMessage, []string, error) {
	currentCfg, err := decodeChannelConfig(name, currentRaw)
	if err != nil {
		return nil, nil, err
	}
	nextCfg, err := decodeChannelConfig(name, nextRaw)
	if err != nil {
		return nil, nil, err
	}

	var firstErr error
	normalized := nextCfg
	normalized.walkSecretFields(func(path string, value *string) {
		if firstErr != nil {
			return
		}
		relative, ok := relativeChannelSecretPath(path, name)
		if !ok {
			return
		}
		normalizedValue, err := normalizeManagedSecretValue("channels", name, relative, *value)
		if err != nil {
			firstErr = err
			return
		}
		*value = normalizedValue
	})
	if firstErr != nil {
		return nil, nil, firstErr
	}
	cleanup := managedSecretCleanupRefs(filterChannelSecretValues(currentCfg, name), filterChannelSecretValues(normalized, name))
	payload, err := marshalKnownChannelConfig(normalized, name)
	if err != nil {
		return nil, nil, err
	}
	return payload, cleanup, nil
}

func relativeChannelSecretPath(path, name string) (string, bool) {
	prefix := "channels." + strings.TrimSpace(name) + "."
	if !strings.HasPrefix(path, prefix) {
		return "", false
	}
	return strings.TrimPrefix(path, prefix), true
}

func filterChannelSecretValues(cfg Config, name string) map[string]string {
	values := make(map[string]string, 8)
	for path, value := range cfg.secretFieldValues() {
		relative, ok := relativeChannelSecretPath(path, name)
		if !ok {
			continue
		}
		values[relative] = canonicalSecretStorageValue(value)
	}
	return values
}

func decodeChannelConfig(name string, raw json.RawMessage) (Config, error) {
	var cfg Config
	if err := decodeChannelIntoConfig(&cfg, name, raw, false); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func decodeChannelIntoConfig(cfg *Config, name string, raw json.RawMessage, merge bool) error {
	if cfg == nil {
		return fmt.Errorf("channel config target is nil")
	}
	field, ok := channelFieldByName(name)
	if !ok {
		return fmt.Errorf("unknown channel %q", name)
	}
	holder := reflect.New(field.Type)
	if merge {
		holder.Elem().Set(reflect.ValueOf(&cfg.Channels).Elem().FieldByName(field.Name))
	}
	payload := strings.TrimSpace(string(raw))
	if payload != "" && payload != "null" {
		if err := yaml.Unmarshal(raw, holder.Interface()); err != nil {
			return fmt.Errorf("decode channel %q: %w", name, err)
		}
	}
	reflect.ValueOf(&cfg.Channels).Elem().FieldByName(field.Name).Set(holder.Elem())
	return nil
}

func marshalKnownChannelConfig(cfg Config, name string) (json.RawMessage, error) {
	field, ok := channelFieldByName(name)
	if !ok {
		return nil, fmt.Errorf("unknown channel %q", name)
	}
	value := reflect.ValueOf(cfg.Channels).FieldByName(field.Name)
	payload, err := canonicalSectionPayload(value.Interface())
	if err != nil {
		return nil, fmt.Errorf("canonicalize channel %q: %w", name, err)
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("encode channel %q: %w", name, err)
	}
	return json.RawMessage(data), nil
}

func channelFieldByName(name string) (reflect.StructField, bool) {
	typ := reflect.TypeOf(ChannelsConfig{})
	for index := 0; index < typ.NumField(); index++ {
		field := typ.Field(index)
		if yamlTagNameConfig(field.Tag.Get("yaml")) == name {
			return field, true
		}
	}
	return reflect.StructField{}, false
}

func mergeUnknownChannelConfig(currentRaw, incomingRaw json.RawMessage) (json.RawMessage, error) {
	current, err := decodeJSONObject(currentRaw)
	if err != nil {
		return nil, err
	}
	if len(incomingRaw) == 0 {
		return encodeJSONObject(current)
	}
	incoming, err := decodeJSONObject(incomingRaw)
	if err != nil {
		return nil, err
	}
	merged := mergeJSONObject(current, incoming)
	merged = preserveUnknownSecretPlaceholders(current, merged, nil).(map[string]any)
	return encodeJSONObject(merged)
}

func normalizeUnknownChannelConfigForStore(name string, currentRaw, nextRaw json.RawMessage) (json.RawMessage, []string, error) {
	current, err := decodeJSONObject(currentRaw)
	if err != nil {
		return nil, nil, err
	}
	next, err := decodeJSONObject(nextRaw)
	if err != nil {
		return nil, nil, err
	}
	normalized, err := normalizeUnknownChannelSecrets(strings.TrimSpace(name), next, nil)
	if err != nil {
		return nil, nil, err
	}
	cleanup := managedSecretCleanupRefs(collectUnknownChannelSecrets(current, nil), collectUnknownChannelSecrets(normalized, nil))
	payload, err := encodeJSONObject(normalized.(map[string]any))
	if err != nil {
		return nil, nil, err
	}
	return payload, cleanup, nil
}

func sanitizeUnknownChannelConfigForOperator(raw json.RawMessage) (json.RawMessage, error) {
	current, err := decodeJSONObject(raw)
	if err != nil {
		return nil, err
	}
	sanitized := sanitizeUnknownChannelSecretsForOperator(current, nil).(map[string]any)
	return encodeJSONObject(sanitized)
}

func decodeJSONObject(raw json.RawMessage) (map[string]any, error) {
	payload := strings.TrimSpace(string(raw))
	if payload == "" || payload == "null" {
		return map[string]any{}, nil
	}
	var decoded map[string]any
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return nil, fmt.Errorf("decode channel config: %w", err)
	}
	if decoded == nil {
		return map[string]any{}, nil
	}
	return decoded, nil
}

func encodeJSONObject(payload map[string]any) (json.RawMessage, error) {
	if payload == nil {
		payload = map[string]any{}
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	return json.RawMessage(data), nil
}

func mergeJSONObject(base, overlay map[string]any) map[string]any {
	out := cloneJSONObject(base)
	for key, value := range overlay {
		if existing, ok := out[key].(map[string]any); ok {
			if nextMap, ok := value.(map[string]any); ok {
				out[key] = mergeJSONObject(existing, nextMap)
				continue
			}
		}
		out[key] = cloneJSONValue(value)
	}
	return out
}

func cloneJSONObject(in map[string]any) map[string]any {
	if len(in) == 0 {
		return map[string]any{}
	}
	out := make(map[string]any, len(in))
	for key, value := range in {
		out[key] = cloneJSONValue(value)
	}
	return out
}

func cloneJSONValue(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		return cloneJSONObject(typed)
	case []any:
		out := make([]any, len(typed))
		for index := range typed {
			out[index] = cloneJSONValue(typed[index])
		}
		return out
	case string:
		return strings.TrimSpace(typed)
	default:
		return typed
	}
}

func preserveUnknownSecretPlaceholders(current any, next any, path []string) any {
	switch typed := next.(type) {
	case map[string]any:
		out := make(map[string]any, len(typed))
		currentMap, _ := current.(map[string]any)
		for key, value := range typed {
			out[key] = preserveUnknownSecretPlaceholders(currentMap[key], value, append(path, key))
		}
		return out
	case []any:
		out := make([]any, len(typed))
		currentSlice, _ := current.([]any)
		for index := range typed {
			var currentValue any
			if index < len(currentSlice) {
				currentValue = currentSlice[index]
			}
			out[index] = preserveUnknownSecretPlaceholders(currentValue, typed[index], append(path, strconv.Itoa(index)))
		}
		return out
	case string:
		if IsSecretPlaceholder(typed) && looksLikeUnknownSecretPath(path) {
			if currentString, ok := current.(string); ok {
				return strings.TrimSpace(currentString)
			}
			return ""
		}
		return strings.TrimSpace(typed)
	default:
		return typed
	}
}

func sanitizeUnknownChannelSecretsForOperator(value any, path []string) any {
	switch typed := value.(type) {
	case map[string]any:
		out := make(map[string]any, len(typed))
		for key, item := range typed {
			out[key] = sanitizeUnknownChannelSecretsForOperator(item, append(path, key))
		}
		return out
	case []any:
		out := make([]any, len(typed))
		for index := range typed {
			out[index] = sanitizeUnknownChannelSecretsForOperator(typed[index], append(path, strconv.Itoa(index)))
		}
		return out
	case string:
		if !looksLikeUnknownSecretPath(path) {
			return strings.TrimSpace(typed)
		}
		return sanitizeSecretValueForOperator(typed)
	default:
		return typed
	}
}

func normalizeUnknownChannelSecrets(name string, value any, path []string) (any, error) {
	switch typed := value.(type) {
	case map[string]any:
		out := make(map[string]any, len(typed))
		for key, item := range typed {
			normalized, err := normalizeUnknownChannelSecrets(name, item, append(path, key))
			if err != nil {
				return nil, err
			}
			out[key] = normalized
		}
		return out, nil
	case []any:
		out := make([]any, len(typed))
		for index := range typed {
			normalized, err := normalizeUnknownChannelSecrets(name, typed[index], append(path, strconv.Itoa(index)))
			if err != nil {
				return nil, err
			}
			out[index] = normalized
		}
		return out, nil
	case string:
		if !looksLikeUnknownSecretPath(path) {
			return strings.TrimSpace(typed), nil
		}
		return normalizeManagedSecretValue("channels", name, strings.Join(path, "."), typed)
	default:
		return typed, nil
	}
}

func collectUnknownChannelSecrets(value any, path []string) map[string]string {
	out := make(map[string]string)
	collectUnknownChannelSecretsInto(out, value, path)
	return out
}

func collectUnknownChannelSecretsInto(out map[string]string, value any, path []string) {
	switch typed := value.(type) {
	case map[string]any:
		for key, item := range typed {
			collectUnknownChannelSecretsInto(out, item, append(path, key))
		}
	case []any:
		for index := range typed {
			collectUnknownChannelSecretsInto(out, typed[index], append(path, strconv.Itoa(index)))
		}
	case string:
		if !looksLikeUnknownSecretPath(path) {
			return
		}
		if normalized := canonicalSecretStorageValue(typed); normalized != "" {
			out[strings.Join(path, ".")] = normalized
		}
	}
}

func looksLikeUnknownSecretPath(path []string) bool {
	for index := len(path) - 1; index >= 0; index-- {
		if _, err := strconv.Atoi(path[index]); err == nil {
			continue
		}
		return looksLikeUnknownSecretKey(path[index])
	}
	return false
}

func looksLikeUnknownSecretKey(key string) bool {
	key = strings.ToLower(strings.TrimSpace(key))
	if key == "" {
		return false
	}
	switch key {
	case "id", "url", "model", "region", "host", "base_url":
		return false
	}
	if strings.Contains(key, "token") || strings.Contains(key, "secret") || strings.Contains(key, "password") || strings.Contains(key, "cookie") {
		return true
	}
	for _, suffix := range []string{
		"api_key",
		"private_key",
		"secret_key",
		"access_key",
		"session_token",
		"refresh_token",
		"auth_token",
		"bot_token",
		"app_token",
		"oauth_token",
		"verification_key",
		"encrypt_key",
		"ship_code",
		"imei",
	} {
		if key == suffix || strings.HasSuffix(key, suffix) {
			return true
		}
	}
	return false
}

func yamlTagNameConfig(raw string) string {
	name := strings.TrimSpace(raw)
	if name == "" || name == "-" {
		return ""
	}
	if cut, _, ok := strings.Cut(name, ","); ok {
		return strings.TrimSpace(cut)
	}
	return name
}
