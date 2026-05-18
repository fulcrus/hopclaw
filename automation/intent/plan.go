package automationintent

import "strings"

type Action string

const (
	ActionNone    Action = "none"
	ActionCreate  Action = "create"
	ActionUpdate  Action = "update"
	ActionDisable Action = "disable"
	ActionDelete  Action = "delete"
	ActionQuery   Action = "query"
)

type QueryMetric string

const (
	QueryMetricSummary           QueryMetric = "summary"
	QueryMetricList              QueryMetric = "list"
	QueryMetricNotificationToday QueryMetric = "notification_today"
	QueryMetricNotificationTotal QueryMetric = "notification_total"
	QueryMetricNotificationError QueryMetric = "notification_failures"
)

type DeliveryTarget struct {
	Kind       string `json:"kind,omitempty"`
	Provider   string `json:"provider,omitempty"`
	Channel    string `json:"channel,omitempty"`
	AccountID  string `json:"account_id,omitempty"`
	TargetType string `json:"target_type,omitempty"`
	Target     string `json:"target,omitempty"`
	Label      string `json:"label,omitempty"`
}

type MissingInfo struct {
	Field    string `json:"field,omitempty"`
	Question string `json:"question,omitempty"`
	Example  string `json:"example,omitempty"`
	Required bool   `json:"required,omitempty"`
}

type Selector struct {
	Query           string   `json:"query,omitempty"`
	Kind            string   `json:"kind,omitempty"`
	IDs             []string `json:"ids,omitempty"`
	Names           []string `json:"names,omitempty"`
	Cities          []string `json:"cities,omitempty"`
	DeliveryChannel string   `json:"delivery_channel,omitempty"`
	DeliveryTarget  string   `json:"delivery_target,omitempty"`
}

type Spec struct {
	Kind         string          `json:"kind,omitempty"`
	Name         string          `json:"name,omitempty"`
	Schedule     string          `json:"schedule,omitempty"`
	Timezone     string          `json:"timezone,omitempty"`
	SessionKey   string          `json:"session_key,omitempty"`
	Model        string          `json:"model,omitempty"`
	Prompt       string          `json:"prompt,omitempty"`
	Message      string          `json:"message,omitempty"`
	Delivery     *DeliveryTarget `json:"delivery,omitempty"`
	SourceKind   string          `json:"source_kind,omitempty"`
	SourceURL    string          `json:"source_url,omitempty"`
	SourcePath   string          `json:"source_path,omitempty"`
	Interval     string          `json:"interval,omitempty"`
	FireOnStart  bool            `json:"fire_on_start,omitempty"`
	Enabled      bool            `json:"enabled,omitempty"`
	AutomationID string          `json:"automation_id,omitempty"`
}

type Query struct {
	Metric QueryMetric `json:"metric,omitempty"`
	Limit  int         `json:"limit,omitempty"`
}

type InventoryItem struct {
	ID            string          `json:"id,omitempty"`
	Kind          string          `json:"kind,omitempty"`
	Name          string          `json:"name,omitempty"`
	Enabled       bool            `json:"enabled,omitempty"`
	Schedule      string          `json:"schedule,omitempty"`
	Message       string          `json:"message,omitempty"`
	PromptPreview string          `json:"prompt_preview,omitempty"`
	SourceLabel   string          `json:"source_label,omitempty"`
	Delivery      *DeliveryTarget `json:"delivery,omitempty"`
}

type Plan struct {
	Action           Action          `json:"action,omitempty"`
	Kind             string          `json:"kind,omitempty"`
	Confidence       float64         `json:"confidence,omitempty"`
	Reason           string          `json:"reason,omitempty"`
	NeedConfirmation bool            `json:"need_confirmation,omitempty"`
	MissingInfo      []MissingInfo   `json:"missing_info,omitempty"`
	Selector         Selector        `json:"selector,omitempty"`
	Spec             Spec            `json:"spec,omitempty"`
	Query            Query           `json:"query,omitempty"`
	Candidates       []InventoryItem `json:"candidates,omitempty"`
}

func (p Plan) Normalized() Plan {
	p.Kind = strings.TrimSpace(strings.ToLower(p.Kind))
	p.Reason = strings.TrimSpace(p.Reason)
	p.Selector = p.Selector.Normalized()
	p.Spec = p.Spec.Normalized()
	if p.Query.Limit <= 0 {
		p.Query.Limit = 10
	}
	if p.Query.Metric == "" && p.Action == ActionQuery {
		p.Query.Metric = QueryMetricSummary
	}
	if p.Action == "" {
		p.Action = ActionNone
	}
	return p
}

func (p Plan) Actionable() bool {
	return p.Normalized().Action != ActionNone
}

func (s Selector) Normalized() Selector {
	s.Query = strings.TrimSpace(s.Query)
	s.Kind = strings.TrimSpace(strings.ToLower(s.Kind))
	s.DeliveryChannel = strings.TrimSpace(s.DeliveryChannel)
	s.DeliveryTarget = strings.TrimSpace(s.DeliveryTarget)
	s.IDs = trimStringSlice(s.IDs)
	s.Names = trimStringSlice(s.Names)
	s.Cities = trimStringSlice(s.Cities)
	return s
}

func (s Spec) Normalized() Spec {
	s.Kind = strings.TrimSpace(strings.ToLower(s.Kind))
	s.Name = strings.TrimSpace(s.Name)
	s.Schedule = strings.TrimSpace(s.Schedule)
	s.Timezone = strings.TrimSpace(s.Timezone)
	s.SessionKey = strings.TrimSpace(s.SessionKey)
	s.Model = strings.TrimSpace(s.Model)
	s.Prompt = strings.TrimSpace(s.Prompt)
	s.Message = strings.TrimSpace(s.Message)
	s.SourceKind = strings.TrimSpace(strings.ToLower(s.SourceKind))
	s.SourceURL = strings.TrimSpace(s.SourceURL)
	s.SourcePath = strings.TrimSpace(s.SourcePath)
	s.Interval = strings.TrimSpace(s.Interval)
	s.AutomationID = strings.TrimSpace(s.AutomationID)
	if s.Delivery != nil {
		s.Delivery.Kind = strings.TrimSpace(strings.ToLower(s.Delivery.Kind))
		s.Delivery.Provider = strings.TrimSpace(strings.ToLower(s.Delivery.Provider))
		s.Delivery.Channel = strings.TrimSpace(s.Delivery.Channel)
		s.Delivery.AccountID = strings.TrimSpace(s.Delivery.AccountID)
		s.Delivery.TargetType = strings.TrimSpace(strings.ToLower(s.Delivery.TargetType))
		s.Delivery.Target = strings.TrimSpace(s.Delivery.Target)
		s.Delivery.Label = strings.TrimSpace(s.Delivery.Label)
		if s.Delivery.Provider == "" && s.Delivery.Channel != "" {
			s.Delivery.Provider = strings.ToLower(s.Delivery.Channel)
		}
		if s.Delivery.Kind == "" {
			if s.Delivery.Channel != "" || s.Delivery.Provider != "" {
				s.Delivery.Kind = "channel"
			}
		}
		if s.Delivery.Channel == "" && s.Delivery.Kind == "channel" {
			s.Delivery.Channel = s.Delivery.Provider
		}
		if s.Delivery.Channel == "" && s.Delivery.Target == "" && s.Delivery.AccountID == "" {
			s.Delivery = nil
		}
	}
	return s
}

func trimStringSlice(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	out := make([]string, 0, len(values))
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			out = append(out, trimmed)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}
