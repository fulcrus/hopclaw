package authscope

import "strings"

const (
	HeaderSubject      = "X-HopClaw-Auth-Subject"
	HeaderAutomationID = "X-HopClaw-Auth-Automation-ID"
)

func NormalizeAutomationIDs(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, raw := range values {
		for _, value := range strings.Split(raw, ",") {
			trimmed := strings.TrimSpace(value)
			if trimmed == "" {
				continue
			}
			if _, ok := seen[trimmed]; ok {
				continue
			}
			seen[trimmed] = struct{}{}
			out = append(out, trimmed)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func AutomationIDsFromClaims(claims []string) []string {
	out := make([]string, 0, len(claims))
	for _, raw := range claims {
		claim := strings.TrimSpace(raw)
		if claim == "" {
			continue
		}
		lower := strings.ToLower(claim)
		switch {
		case strings.HasPrefix(lower, "automation:"):
			out = append(out, strings.TrimSpace(claim[len("automation:"):]))
		case strings.HasPrefix(lower, "automation_id:"):
			out = append(out, strings.TrimSpace(claim[len("automation_id:"):]))
		case strings.HasPrefix(lower, "automation="):
			out = append(out, strings.TrimSpace(claim[len("automation="):]))
		case strings.HasPrefix(lower, "automation_id="):
			out = append(out, strings.TrimSpace(claim[len("automation_id="):]))
		}
	}
	return NormalizeAutomationIDs(out)
}

func AutomationIDsFromMetadata(metadata map[string]string) []string {
	if len(metadata) == 0 {
		return nil
	}
	out := make([]string, 0, len(metadata))
	for key, value := range metadata {
		switch {
		case strings.EqualFold(strings.TrimSpace(key), "automation"):
			out = append(out, value)
		case strings.EqualFold(strings.TrimSpace(key), "automation_id"):
			out = append(out, value)
		case strings.EqualFold(strings.TrimSpace(key), "automation_ids"):
			out = append(out, value)
		}
	}
	return NormalizeAutomationIDs(out)
}

func AutomationIDsFromHeaderValues(values []string) []string {
	return NormalizeAutomationIDs(values)
}

func ContainsAutomationID(values []string, id string) bool {
	id = strings.TrimSpace(id)
	if id == "" {
		return false
	}
	for _, candidate := range NormalizeAutomationIDs(values) {
		if candidate == id {
			return true
		}
	}
	return false
}

func IntersectAutomationIDs(left, right []string) []string {
	left = NormalizeAutomationIDs(left)
	right = NormalizeAutomationIDs(right)
	if len(left) == 0 || len(right) == 0 {
		return nil
	}
	allowed := make(map[string]struct{}, len(left))
	for _, id := range left {
		allowed[id] = struct{}{}
	}
	out := make([]string, 0, len(right))
	for _, id := range right {
		if _, ok := allowed[id]; ok {
			out = append(out, id)
		}
	}
	return NormalizeAutomationIDs(out)
}
