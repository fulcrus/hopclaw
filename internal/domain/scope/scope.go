package scope

import "strings"

type Ref struct {
	AutomationID string `json:"automation_id,omitempty"`
}

func (r Ref) Normalize() Ref {
	r.AutomationID = strings.TrimSpace(r.AutomationID)
	return r
}

func (r Ref) IsZero() bool {
	return strings.TrimSpace(r.AutomationID) == ""
}
