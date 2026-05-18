package durablefact

import (
	"sort"
	"strings"
	"time"
)

type FactClass string

const (
	FactClassPreference   FactClass = "preference"
	FactClassAgreement    FactClass = "agreement"
	FactClassBusinessRule FactClass = "business_rule"
	FactClassSystemConfig FactClass = "system_config"
	FactClassImportedNote FactClass = "imported_note"
)

type ViewType string

const (
	ViewTypeContext        ViewType = "context"
	ViewTypeConfigProvider ViewType = "config_provider"
	ViewTypeConfigChannel  ViewType = "config_channel"
	ViewTypeConfigSetting  ViewType = "config_setting"
)

type ValueType string

const (
	ValueTypeText ValueType = "text"
	ValueTypeJSON ValueType = "json"
)

type Evidence struct {
	Source     string    `json:"source,omitempty"`
	Ref        string    `json:"ref,omitempty"`
	Summary    string    `json:"summary,omitempty"`
	Value      string    `json:"value,omitempty"`
	ObservedAt time.Time `json:"observed_at,omitempty"`
}

type Fact struct {
	Key            string         `json:"key"`
	FactClass      FactClass      `json:"fact_class"`
	ViewType       ViewType       `json:"view_type"`
	Namespace      string         `json:"namespace,omitempty"`
	ScopeKey       string         `json:"scope_key,omitempty"`
	Name           string         `json:"name,omitempty"`
	Label          string         `json:"label,omitempty"`
	Value          string         `json:"value,omitempty"`
	ValueType      ValueType      `json:"value_type,omitempty"`
	Source         string         `json:"source,omitempty"`
	Managed        bool           `json:"managed,omitempty"`
	Confidence     float64        `json:"confidence,omitempty"`
	ReviewRequired bool           `json:"review_required,omitempty"`
	Tags           []string       `json:"tags,omitempty"`
	PreviousValues []string       `json:"previous_values,omitempty"`
	Evidence       []Evidence     `json:"evidence,omitempty"`
	Metadata       map[string]any `json:"metadata,omitempty"`
	CreatedAt      time.Time      `json:"created_at,omitempty"`
	UpdatedAt      time.Time      `json:"updated_at,omitempty"`
}

type Filter struct {
	ViewType       ViewType
	FactClass      FactClass
	Namespace      string
	ScopeKey       string
	Prefix         string
	Query          string
	ReviewRequired *bool
}

func NormalizeFact(f Fact) Fact {
	now := time.Now().UTC()
	out := f
	out.Key = strings.TrimSpace(out.Key)
	out.FactClass = normalizeFactClass(out.FactClass)
	out.ViewType = normalizeViewType(out.ViewType)
	out.Namespace = strings.TrimSpace(out.Namespace)
	out.ScopeKey = strings.TrimSpace(out.ScopeKey)
	out.Name = strings.TrimSpace(out.Name)
	out.Label = strings.TrimSpace(out.Label)
	out.Value = strings.TrimSpace(out.Value)
	out.ValueType = normalizeValueType(out.ValueType)
	out.Source = strings.TrimSpace(out.Source)
	if out.Confidence < 0 {
		out.Confidence = 0
	}
	if out.Confidence > 1 {
		out.Confidence = 1
	}
	out.Tags = uniqueSortedStrings(out.Tags)
	out.PreviousValues = uniqueTrimmedStrings(out.PreviousValues)
	out.Evidence = normalizeEvidence(out.Evidence)
	out.Metadata = cloneMap(out.Metadata)
	if out.CreatedAt.IsZero() {
		out.CreatedAt = now
	}
	if out.UpdatedAt.IsZero() {
		out.UpdatedAt = out.CreatedAt
	}
	if out.Key == "" {
		out.Key = buildFallbackKey(out)
	}
	return out
}

func normalizeFactClass(class FactClass) FactClass {
	switch FactClass(strings.TrimSpace(string(class))) {
	case FactClassPreference:
		return FactClassPreference
	case FactClassAgreement:
		return FactClassAgreement
	case FactClassBusinessRule:
		return FactClassBusinessRule
	case FactClassSystemConfig:
		return FactClassSystemConfig
	case FactClassImportedNote:
		return FactClassImportedNote
	default:
		return FactClassImportedNote
	}
}

func normalizeViewType(viewType ViewType) ViewType {
	switch ViewType(strings.TrimSpace(string(viewType))) {
	case ViewTypeContext:
		return ViewTypeContext
	case ViewTypeConfigProvider:
		return ViewTypeConfigProvider
	case ViewTypeConfigChannel:
		return ViewTypeConfigChannel
	case ViewTypeConfigSetting:
		return ViewTypeConfigSetting
	default:
		return ViewTypeContext
	}
}

func normalizeValueType(valueType ValueType) ValueType {
	switch ValueType(strings.TrimSpace(string(valueType))) {
	case "":
		return ValueTypeText
	case ValueTypeJSON:
		return ValueTypeJSON
	default:
		return ValueTypeText
	}
}

func normalizeEvidence(items []Evidence) []Evidence {
	if len(items) == 0 {
		return nil
	}
	out := make([]Evidence, 0, len(items))
	seen := make(map[string]struct{}, len(items))
	for _, item := range items {
		item.Source = strings.TrimSpace(item.Source)
		item.Ref = strings.TrimSpace(item.Ref)
		item.Summary = strings.TrimSpace(item.Summary)
		item.Value = strings.TrimSpace(item.Value)
		if item.ObservedAt.IsZero() {
			item.ObservedAt = time.Now().UTC()
		}
		key := strings.Join([]string{item.Source, item.Ref, item.Summary, item.Value}, "|")
		if key == "|||" {
			continue
		}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, item)
	}
	return out
}

func buildFallbackKey(f Fact) string {
	parts := []string{
		strings.TrimSpace(string(f.ViewType)),
		strings.TrimSpace(f.Namespace),
		strings.TrimSpace(f.ScopeKey),
		strings.TrimSpace(f.Name),
	}
	filtered := make([]string, 0, len(parts))
	for _, part := range parts {
		if part == "" {
			continue
		}
		filtered = append(filtered, part)
	}
	if len(filtered) == 0 {
		return "durable_fact"
	}
	return strings.Join(filtered, "/")
}

func cloneMap(in map[string]any) map[string]any {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]any, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}

func uniqueTrimmedStrings(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	out := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
}

func uniqueSortedStrings(values []string) []string {
	out := uniqueTrimmedStrings(values)
	if len(out) == 0 {
		return nil
	}
	sort.Strings(out)
	return out
}
