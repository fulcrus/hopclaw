package setup

import (
	"fmt"
	"strings"
	"time"
)

type ContractStatus string

const (
	ContractStatusReady      ContractStatus = "ready"
	ContractStatusNeedsInput ContractStatus = "needs_input"
	ContractStatusBlocked    ContractStatus = "blocked"
)

type SlotStatus string

const (
	SlotStatusReady   SlotStatus = "ready"
	SlotStatusMissing SlotStatus = "missing"
	SlotStatusBlocked SlotStatus = "blocked"
)

type ProbeStatus string

const (
	ProbeStatusReady         ProbeStatus = "ready"
	ProbeStatusNeedsTarget   ProbeStatus = "needs_target"
	ProbeStatusNotConnected  ProbeStatus = "not_connected"
	ProbeStatusNotConfigured ProbeStatus = "not_configured"
	ProbeStatusUnsupported   ProbeStatus = "unsupported"
	ProbeStatusInvalid       ProbeStatus = "invalid"
)

type WorkflowSpec struct {
	Kind        string           `json:"kind,omitempty"`
	Name        string           `json:"name,omitempty"`
	Summary     string           `json:"summary,omitempty"`
	Destination *DestinationSpec `json:"destination,omitempty"`
}

type DestinationSpec struct {
	Kind       string         `json:"kind,omitempty"`
	Provider   string         `json:"provider,omitempty"`
	Channel    string         `json:"channel,omitempty"`
	AccountID  string         `json:"account_id,omitempty"`
	TargetType string         `json:"target_type,omitempty"`
	Target     string         `json:"target,omitempty"`
	Label      string         `json:"label,omitempty"`
	Metadata   map[string]any `json:"metadata,omitempty"`
}

func (s DestinationSpec) Normalized() DestinationSpec {
	s.Kind = strings.TrimSpace(strings.ToLower(s.Kind))
	s.Provider = strings.TrimSpace(strings.ToLower(s.Provider))
	s.Channel = strings.TrimSpace(strings.ToLower(s.Channel))
	s.AccountID = strings.TrimSpace(s.AccountID)
	s.TargetType = strings.TrimSpace(strings.ToLower(s.TargetType))
	s.Target = strings.TrimSpace(s.Target)
	s.Label = strings.TrimSpace(s.Label)
	if s.Kind == "" {
		switch {
		case s.Channel != "":
			s.Kind = "channel"
		case s.Provider == "email" || s.Provider == "smtp":
			s.Kind = "email"
		}
	}
	if s.Channel == "" && s.Kind == "channel" {
		s.Channel = s.Provider
	}
	if s.Provider == "" {
		switch s.Kind {
		case "channel":
			s.Provider = s.Channel
		case "email":
			s.Provider = "smtp"
		}
	}
	if len(s.Metadata) == 0 {
		s.Metadata = nil
	}
	return s
}

func (s DestinationSpec) Ready() bool {
	s = s.Normalized()
	return s.Kind != "" && s.Provider != "" && s.Target != ""
}

func (s DestinationSpec) DisplayName() string {
	s = s.Normalized()
	if s.Label != "" {
		return s.Label
	}
	base := s.Provider
	if base == "" {
		base = s.Kind
	}
	switch {
	case s.AccountID != "" && s.Target != "":
		return fmt.Sprintf("%s/%s -> %s", base, s.AccountID, s.Target)
	case s.Target != "":
		return fmt.Sprintf("%s -> %s", base, s.Target)
	case s.AccountID != "":
		return fmt.Sprintf("%s/%s", base, s.AccountID)
	default:
		return base
	}
}

type InventoryItem struct {
	ID           string          `json:"id,omitempty"`
	Kind         string          `json:"kind,omitempty"`
	Provider     string          `json:"provider,omitempty"`
	Label        string          `json:"label,omitempty"`
	Summary      string          `json:"summary,omitempty"`
	Status       string          `json:"status,omitempty"`
	Capabilities []string        `json:"capabilities,omitempty"`
	Spec         DestinationSpec `json:"spec"`
	Probe        *ProbeResult    `json:"probe,omitempty"`
	Metadata     map[string]any  `json:"metadata,omitempty"`
}

func (i InventoryItem) Normalized() InventoryItem {
	i.Kind = strings.TrimSpace(strings.ToLower(i.Kind))
	i.Provider = strings.TrimSpace(strings.ToLower(i.Provider))
	i.Label = strings.TrimSpace(i.Label)
	i.Summary = strings.TrimSpace(i.Summary)
	i.Status = strings.TrimSpace(strings.ToLower(i.Status))
	i.Spec = i.Spec.Normalized()
	if len(i.Capabilities) == 0 {
		i.Capabilities = nil
	}
	if len(i.Metadata) == 0 {
		i.Metadata = nil
	}
	if i.Probe != nil {
		probe := i.Probe.Normalized()
		i.Probe = &probe
	}
	return i
}

type SetupOption struct {
	ID          string           `json:"id,omitempty"`
	Mode        string           `json:"mode,omitempty"`
	Label       string           `json:"label,omitempty"`
	Description string           `json:"description,omitempty"`
	Value       *DestinationSpec `json:"value,omitempty"`
}

type SetupSlot struct {
	ID            string           `json:"id,omitempty"`
	Kind          string           `json:"kind,omitempty"`
	Label         string           `json:"label,omitempty"`
	Prompt        string           `json:"prompt,omitempty"`
	Required      bool             `json:"required,omitempty"`
	Status        SlotStatus       `json:"status,omitempty"`
	PreferredMode string           `json:"preferred_mode,omitempty"`
	Value         *DestinationSpec `json:"value,omitempty"`
	Options       []SetupOption    `json:"options,omitempty"`
	Example       string           `json:"example,omitempty"`
}

type ProbeResult struct {
	SlotID    string         `json:"slot_id,omitempty"`
	Kind      string         `json:"kind,omitempty"`
	Provider  string         `json:"provider,omitempty"`
	Status    ProbeStatus    `json:"status,omitempty"`
	Reachable bool           `json:"reachable,omitempty"`
	Code      string         `json:"code,omitempty"`
	Message   string         `json:"message,omitempty"`
	CheckedAt time.Time      `json:"checked_at,omitempty"`
	Metadata  map[string]any `json:"metadata,omitempty"`
}

func (p ProbeResult) Normalized() ProbeResult {
	p.SlotID = strings.TrimSpace(p.SlotID)
	p.Kind = strings.TrimSpace(strings.ToLower(p.Kind))
	p.Provider = strings.TrimSpace(strings.ToLower(p.Provider))
	p.Status = ProbeStatus(strings.TrimSpace(strings.ToLower(string(p.Status))))
	p.Code = strings.TrimSpace(strings.ToLower(p.Code))
	p.Message = strings.TrimSpace(p.Message)
	p.CheckedAt = p.CheckedAt.UTC()
	if len(p.Metadata) == 0 {
		p.Metadata = nil
	}
	return p
}

type Contract struct {
	Status   ContractStatus `json:"status,omitempty"`
	Workflow *WorkflowSpec  `json:"workflow,omitempty"`
	Summary  string         `json:"summary,omitempty"`
	Slots    []SetupSlot    `json:"slots,omitempty"`
	Probes   []ProbeResult  `json:"probes,omitempty"`
}

func (c Contract) Ready() bool {
	return c.Status == ContractStatusReady
}
