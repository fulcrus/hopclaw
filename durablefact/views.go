package durablefact

import "time"

type ContextView struct {
	Key            string    `json:"key"`
	FactClass      FactClass `json:"fact_class"`
	Namespace      string    `json:"namespace,omitempty"`
	ScopeKey       string    `json:"scope_key,omitempty"`
	Field          string    `json:"field,omitempty"`
	Label          string    `json:"label,omitempty"`
	Value          string    `json:"value,omitempty"`
	Source         string    `json:"source,omitempty"`
	Managed        bool      `json:"managed,omitempty"`
	Confidence     float64   `json:"confidence,omitempty"`
	ReviewRequired bool      `json:"review_required,omitempty"`
	Tags           []string  `json:"tags,omitempty"`
	PreviousValues []string  `json:"previous_values,omitempty"`
	EvidenceCount  int       `json:"evidence_count,omitempty"`
	CreatedAt      time.Time `json:"created_at,omitempty"`
	UpdatedAt      time.Time `json:"updated_at,omitempty"`
}

type ConfigViewKind string

const (
	ConfigViewKindProvider ConfigViewKind = "provider"
	ConfigViewKindChannel  ConfigViewKind = "channel"
	ConfigViewKindSetting  ConfigViewKind = "setting"
)

type ConfigView struct {
	Key            string         `json:"key"`
	Kind           ConfigViewKind `json:"kind"`
	Name           string         `json:"name"`
	Payload        string         `json:"payload"`
	Source         string         `json:"source,omitempty"`
	ReviewRequired bool           `json:"review_required,omitempty"`
	CreatedAt      time.Time      `json:"created_at,omitempty"`
	UpdatedAt      time.Time      `json:"updated_at,omitempty"`
	Metadata       map[string]any `json:"metadata,omitempty"`
}

type OperatorView struct {
	Key            string         `json:"key"`
	FactClass      FactClass      `json:"fact_class"`
	ViewType       ViewType       `json:"view_type"`
	Namespace      string         `json:"namespace,omitempty"`
	ScopeKey       string         `json:"scope_key,omitempty"`
	Name           string         `json:"name,omitempty"`
	Label          string         `json:"label,omitempty"`
	Value          string         `json:"value,omitempty"`
	Source         string         `json:"source,omitempty"`
	Managed        bool           `json:"managed,omitempty"`
	Confidence     float64        `json:"confidence,omitempty"`
	ReviewRequired bool           `json:"review_required,omitempty"`
	Tags           []string       `json:"tags,omitempty"`
	CreatedAt      time.Time      `json:"created_at,omitempty"`
	UpdatedAt      time.Time      `json:"updated_at,omitempty"`
	Metadata       map[string]any `json:"metadata,omitempty"`
}

func ToContextView(f Fact) (ContextView, bool) {
	if NormalizeFact(f).ViewType != ViewTypeContext {
		return ContextView{}, false
	}
	f = NormalizeFact(f)
	return ContextView{
		Key:            f.Key,
		FactClass:      f.FactClass,
		Namespace:      f.Namespace,
		ScopeKey:       f.ScopeKey,
		Field:          f.Name,
		Label:          f.Label,
		Value:          f.Value,
		Source:         f.Source,
		Managed:        f.Managed,
		Confidence:     f.Confidence,
		ReviewRequired: f.ReviewRequired,
		Tags:           append([]string(nil), f.Tags...),
		PreviousValues: append([]string(nil), f.PreviousValues...),
		EvidenceCount:  len(f.Evidence),
		CreatedAt:      f.CreatedAt,
		UpdatedAt:      f.UpdatedAt,
	}, true
}

func ToConfigView(f Fact) (ConfigView, bool) {
	f = NormalizeFact(f)
	view := ConfigView{
		Key:            f.Key,
		Name:           f.Name,
		Payload:        f.Value,
		Source:         f.Source,
		ReviewRequired: f.ReviewRequired,
		CreatedAt:      f.CreatedAt,
		UpdatedAt:      f.UpdatedAt,
		Metadata:       cloneMap(f.Metadata),
	}
	switch f.ViewType {
	case ViewTypeConfigProvider:
		view.Kind = ConfigViewKindProvider
	case ViewTypeConfigChannel:
		view.Kind = ConfigViewKindChannel
	case ViewTypeConfigSetting:
		view.Kind = ConfigViewKindSetting
	default:
		return ConfigView{}, false
	}
	return view, true
}

func ToOperatorView(f Fact) OperatorView {
	f = NormalizeFact(f)
	return OperatorView{
		Key:            f.Key,
		FactClass:      f.FactClass,
		ViewType:       f.ViewType,
		Namespace:      f.Namespace,
		ScopeKey:       f.ScopeKey,
		Name:           f.Name,
		Label:          f.Label,
		Value:          f.Value,
		Source:         f.Source,
		Managed:        f.Managed,
		Confidence:     f.Confidence,
		ReviewRequired: f.ReviewRequired,
		Tags:           append([]string(nil), f.Tags...),
		CreatedAt:      f.CreatedAt,
		UpdatedAt:      f.UpdatedAt,
		Metadata:       cloneMap(f.Metadata),
	}
}
