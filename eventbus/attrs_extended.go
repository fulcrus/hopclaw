package eventbus

import (
	"strings"
	"time"
)

type RunPreflightCheckAttrs struct {
	ID       string `json:"id,omitempty"`
	Title    string `json:"title,omitempty"`
	State    string `json:"state,omitempty"`
	Detail   string `json:"detail,omitempty"`
	Blocking bool   `json:"blocking,omitempty"`
}

func (a RunPreflightCheckAttrs) ToMap() map[string]any {
	m := map[string]any{}
	if id := strings.TrimSpace(a.ID); id != "" {
		m["id"] = id
	}
	if title := strings.TrimSpace(a.Title); title != "" {
		m["title"] = title
	}
	if state := strings.TrimSpace(a.State); state != "" {
		m["state"] = state
	}
	if detail := strings.TrimSpace(a.Detail); detail != "" {
		m["detail"] = detail
	}
	if a.Blocking {
		m["blocking"] = true
	}
	if len(m) == 0 {
		return nil
	}
	return m
}

type RunPreflightClarificationSlotAttrs struct {
	ID          string   `json:"id,omitempty"`
	Label       string   `json:"label,omitempty"`
	Question    string   `json:"question,omitempty"`
	InputMode   string   `json:"input_mode,omitempty"`
	Placeholder string   `json:"placeholder,omitempty"`
	Required    bool     `json:"required,omitempty"`
	Hints       []string `json:"hints,omitempty"`
}

func (a RunPreflightClarificationSlotAttrs) ToMap() map[string]any {
	m := map[string]any{}
	if id := strings.TrimSpace(a.ID); id != "" {
		m["id"] = id
	}
	if label := strings.TrimSpace(a.Label); label != "" {
		m["label"] = label
	}
	if question := strings.TrimSpace(a.Question); question != "" {
		m["question"] = question
	}
	if inputMode := strings.TrimSpace(a.InputMode); inputMode != "" {
		m["input_mode"] = inputMode
	}
	if placeholder := strings.TrimSpace(a.Placeholder); placeholder != "" {
		m["placeholder"] = placeholder
	}
	if a.Required {
		m["required"] = true
	}
	if hints := cloneStrings(a.Hints); len(hints) > 0 {
		m["hints"] = hints
	}
	if len(m) == 0 {
		return nil
	}
	return m
}

type RunPreflightAttrs struct {
	State              string                               `json:"state,omitempty"`
	Summary            string                               `json:"summary,omitempty"`
	Prompt             string                               `json:"prompt,omitempty"`
	Question           string                               `json:"question,omitempty"`
	ReplyTemplate      string                               `json:"reply_template,omitempty"`
	ContinueHint       string                               `json:"continue_hint,omitempty"`
	Blocking           bool                                 `json:"blocking,omitempty"`
	GeneratedAt        time.Time                            `json:"generated_at,omitempty"`
	ReplyHints         []string                             `json:"reply_hints,omitempty"`
	SuggestedDomains   []string                             `json:"suggested_domains,omitempty"`
	DetectedDomains    []string                             `json:"detected_domains,omitempty"`
	ClarificationSlots []RunPreflightClarificationSlotAttrs `json:"clarification_slots,omitempty"`
	Checks             []RunPreflightCheckAttrs             `json:"checks,omitempty"`
}

func (a RunPreflightAttrs) ToMap() map[string]any {
	m := map[string]any{}
	if state := strings.TrimSpace(a.State); state != "" {
		m["state"] = state
	}
	if summary := strings.TrimSpace(a.Summary); summary != "" {
		m["summary"] = summary
	}
	if prompt := strings.TrimSpace(a.Prompt); prompt != "" {
		m["prompt"] = prompt
	}
	if question := strings.TrimSpace(a.Question); question != "" {
		m["question"] = question
	}
	if replyTemplate := strings.TrimSpace(a.ReplyTemplate); replyTemplate != "" {
		m["reply_template"] = replyTemplate
	}
	if continueHint := strings.TrimSpace(a.ContinueHint); continueHint != "" {
		m["continue_hint"] = continueHint
	}
	if a.Blocking {
		m["blocking"] = true
	}
	if !a.GeneratedAt.IsZero() {
		m["generated"] = a.GeneratedAt.UTC().Format(time.RFC3339Nano)
	}
	if hints := cloneStrings(a.ReplyHints); len(hints) > 0 {
		m["reply_hints"] = hints
	}
	if domains := cloneStrings(a.SuggestedDomains); len(domains) > 0 {
		m["suggested_domains"] = domains
	}
	if domains := cloneStrings(a.DetectedDomains); len(domains) > 0 {
		m["detected_domains"] = domains
	}
	if len(a.ClarificationSlots) > 0 {
		items := make([]map[string]any, 0, len(a.ClarificationSlots))
		for _, slot := range a.ClarificationSlots {
			if item := slot.ToMap(); len(item) > 0 {
				items = append(items, item)
			}
		}
		if len(items) > 0 {
			m["clarification_slots"] = items
		}
	}
	if len(a.Checks) > 0 {
		items := make([]map[string]any, 0, len(a.Checks))
		for _, check := range a.Checks {
			if item := check.ToMap(); len(item) > 0 {
				items = append(items, item)
			}
		}
		if len(items) > 0 {
			m["checks"] = items
		}
	}
	if len(m) == 0 {
		return nil
	}
	return m
}

type RunTimeoutAttrs struct {
	Channel        string `json:"channel,omitempty"`
	Reason         string `json:"reason,omitempty"`
	Error          string `json:"error,omitempty"`
	MaxRunDuration string `json:"max_run_duration,omitempty"`
}

func (a RunTimeoutAttrs) ToMap() map[string]any {
	m := map[string]any{}
	if channel := strings.TrimSpace(a.Channel); channel != "" {
		m["channel"] = channel
	}
	if reason := strings.TrimSpace(a.Reason); reason != "" {
		m["reason"] = reason
	}
	if errText := strings.TrimSpace(a.Error); errText != "" {
		m["error"] = errText
	}
	if maxRunDuration := strings.TrimSpace(a.MaxRunDuration); maxRunDuration != "" {
		m["max_run_duration"] = maxRunDuration
	}
	if len(m) == 0 {
		return nil
	}
	return m
}

type ModelStreamCompleteAttrs struct {
	PromptTokens     int `json:"prompt_tokens,omitempty"`
	CompletionTokens int `json:"completion_tokens,omitempty"`
	TotalTokens      int `json:"total_tokens,omitempty"`
}

func (a ModelStreamCompleteAttrs) ToMap() map[string]any {
	m := map[string]any{}
	if a.PromptTokens > 0 {
		m["prompt_tokens"] = a.PromptTokens
	}
	if a.CompletionTokens > 0 {
		m["completion_tokens"] = a.CompletionTokens
	}
	if a.TotalTokens > 0 {
		m["total_tokens"] = a.TotalTokens
	}
	if len(m) == 0 {
		return nil
	}
	return m
}

type SecurityRiskItemAttrs struct {
	Category string `json:"category,omitempty"`
	Type     string `json:"type,omitempty"`
	Detail   string `json:"detail,omitempty"`
	Severity string `json:"severity,omitempty"`
}

func (a SecurityRiskItemAttrs) ToMap() map[string]any {
	m := map[string]any{}
	if category := strings.TrimSpace(a.Category); category != "" {
		m["category"] = category
	}
	if riskType := strings.TrimSpace(a.Type); riskType != "" {
		m["type"] = riskType
	}
	if detail := strings.TrimSpace(a.Detail); detail != "" {
		m["detail"] = detail
	}
	if severity := strings.TrimSpace(a.Severity); severity != "" {
		m["severity"] = severity
	}
	if len(m) == 0 {
		return nil
	}
	return m
}

type SecurityRiskDetectedAttrs struct {
	ToolName  string                  `json:"tool_name,omitempty"`
	RiskCount int                     `json:"risk_count,omitempty"`
	Severity  string                  `json:"severity,omitempty"`
	Risks     []SecurityRiskItemAttrs `json:"risks,omitempty"`
}

func (a SecurityRiskDetectedAttrs) ToMap() map[string]any {
	m := map[string]any{}
	if toolName := strings.TrimSpace(a.ToolName); toolName != "" {
		m["tool_name"] = toolName
	}
	riskCount := a.RiskCount
	if riskCount <= 0 && len(a.Risks) > 0 {
		riskCount = len(a.Risks)
	}
	if riskCount > 0 {
		m["risk_count"] = riskCount
	}
	if severity := strings.TrimSpace(a.Severity); severity != "" {
		m["severity"] = severity
	}
	if len(a.Risks) > 0 {
		items := make([]map[string]any, 0, len(a.Risks))
		for _, risk := range a.Risks {
			if item := risk.ToMap(); len(item) > 0 {
				items = append(items, item)
			}
		}
		if len(items) > 0 {
			m["risks"] = items
		}
	}
	if len(m) == 0 {
		return nil
	}
	return m
}

type SecurityFindingAttrs struct {
	ToolName string `json:"tool_name,omitempty"`
	Type     string `json:"type,omitempty"`
	Detail   string `json:"detail,omitempty"`
	Severity string `json:"severity,omitempty"`
}

func (a SecurityFindingAttrs) ToMap() map[string]any {
	m := map[string]any{}
	if toolName := strings.TrimSpace(a.ToolName); toolName != "" {
		m["tool_name"] = toolName
	}
	if findingType := strings.TrimSpace(a.Type); findingType != "" {
		m["type"] = findingType
	}
	if detail := strings.TrimSpace(a.Detail); detail != "" {
		m["detail"] = detail
	}
	if severity := strings.TrimSpace(a.Severity); severity != "" {
		m["severity"] = severity
	}
	if len(m) == 0 {
		return nil
	}
	return m
}

type GovernanceDeliveryAttrs struct {
	DeliveryID          string    `json:"delivery_id,omitempty"`
	AdapterName         string    `json:"adapter_name,omitempty"`
	IdempotencyKey      string    `json:"idempotency_key,omitempty"`
	DeliveryStatus      string    `json:"delivery_status,omitempty"`
	DeliveryAttempts    int       `json:"delivery_attempts,omitempty"`
	DeliveryMaxAttempts int       `json:"delivery_max_attempts,omitempty"`
	GovernanceKind      string    `json:"governance_kind,omitempty"`
	SourceEventID       string    `json:"source_event_id,omitempty"`
	SourceEventType     string    `json:"source_event_type,omitempty"`
	NextAttemptAt       time.Time `json:"next_attempt_at,omitempty"`
	DeliveredAt         time.Time `json:"delivered_at,omitempty"`
	Error               string    `json:"error,omitempty"`
}

func (a GovernanceDeliveryAttrs) ToMap() map[string]any {
	m := map[string]any{}
	if deliveryID := strings.TrimSpace(a.DeliveryID); deliveryID != "" {
		m["delivery_id"] = deliveryID
	}
	if adapterName := strings.TrimSpace(a.AdapterName); adapterName != "" {
		m["adapter_name"] = adapterName
	}
	if idempotencyKey := strings.TrimSpace(a.IdempotencyKey); idempotencyKey != "" {
		m["idempotency_key"] = idempotencyKey
	}
	if deliveryStatus := strings.TrimSpace(a.DeliveryStatus); deliveryStatus != "" {
		m["delivery_status"] = deliveryStatus
	}
	if a.DeliveryAttempts > 0 {
		m["delivery_attempts"] = a.DeliveryAttempts
	}
	if a.DeliveryMaxAttempts > 0 {
		m["delivery_max_attempts"] = a.DeliveryMaxAttempts
	}
	if governanceKind := strings.TrimSpace(a.GovernanceKind); governanceKind != "" {
		m["governance_kind"] = governanceKind
	}
	if sourceEventID := strings.TrimSpace(a.SourceEventID); sourceEventID != "" {
		m["source_event_id"] = sourceEventID
	}
	if sourceEventType := strings.TrimSpace(a.SourceEventType); sourceEventType != "" {
		m["source_event_type"] = sourceEventType
	}
	if !a.NextAttemptAt.IsZero() {
		m["next_attempt_at"] = a.NextAttemptAt.UTC().Format(time.RFC3339)
	}
	if !a.DeliveredAt.IsZero() {
		m["delivered_at"] = a.DeliveredAt.UTC().Format(time.RFC3339)
	}
	if errText := strings.TrimSpace(a.Error); errText != "" {
		m["error"] = errText
	}
	if len(m) == 0 {
		return nil
	}
	return m
}
