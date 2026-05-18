package scope

import (
	"encoding/json"
	"strings"
)

func FromValue(value any) Ref {
	switch typed := value.(type) {
	case nil:
		return Ref{}
	case Ref:
		return typed.Normalize()
	case *Ref:
		if typed == nil {
			return Ref{}
		}
		return typed.Normalize()
	}
	data, err := json.Marshal(value)
	if err != nil || len(data) == 0 || string(data) == "null" {
		return Ref{}
	}
	var ref Ref
	if err := json.Unmarshal(data, &ref); err != nil {
		return Ref{}
	}
	return ref.Normalize()
}

func Summary(ref Ref) string {
	ref = ref.Normalize()
	parts := make([]string, 0, 1)
	if ref.AutomationID != "" {
		parts = append(parts, "automation="+ref.AutomationID)
	}
	if len(parts) == 0 {
		return ""
	}
	return strings.Join(parts, " | ")
}
