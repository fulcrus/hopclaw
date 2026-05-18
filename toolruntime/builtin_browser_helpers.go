package toolruntime

import (
	"context"
	"fmt"
	"strings"
	"time"

	browserclient "github.com/fulcrus/hopclaw/browserapi/client"
	browsertypes "github.com/fulcrus/hopclaw/browserapi/types"
)

func requireBrowserClient(b *Builtins) (*browserclient.Client, error) {
	if b.browserClient == nil {
		return nil, fmt.Errorf("browser client not available")
	}
	return b.browserClient, nil
}

// doBrowserAction sends a request to the browser host and returns the result.

func doBrowserAction(ctx context.Context, client *browserclient.Client, action, sessionID string, params map[string]any) (*browsertypes.Response, error) {
	return doBrowserActionWithTimeout(ctx, client, action, sessionID, params, 0)
}

func doBrowserActionWithTimeout(ctx context.Context, client *browserclient.Client, action, sessionID string, params map[string]any, timeout time.Duration) (*browsertypes.Response, error) {
	req := browsertypes.Request{
		Action:    action,
		SessionID: sessionID,
		Params:    params,
	}
	var (
		resp *browsertypes.Response
		err  error
	)
	if timeout > 0 {
		resp, err = client.DoWithTimeout(ctx, req, timeout)
	} else {
		resp, err = client.Do(ctx, req)
	}
	if err != nil {
		return nil, err
	}
	if !resp.OK && resp.Error != "" {
		return nil, fmt.Errorf("browser: %s", resp.Error)
	}
	return resp, nil
}

// ---------------------------------------------------------------------------
// Handlers
// ---------------------------------------------------------------------------

type ariaRefSummary struct {
	Ref             string `json:"ref"`
	Role            string `json:"role,omitempty"`
	Name            string `json:"name,omitempty"`
	Value           string `json:"value,omitempty"`
	ActionHint      string `json:"action_hint,omitempty"`
	Specialized     bool   `json:"specialized_control,omitempty"`
	SubmitCandidate bool   `json:"submit_candidate,omitempty"`
}

func flattenAriaRefs(root any) []ariaRefSummary {
	var primaryRefs []ariaRefSummary
	var secondaryRefs []ariaRefSummary
	var walk func(any, bool)
	walk = func(node any, inPrimaryContent bool) {
		m, ok := node.(map[string]any)
		if !ok || len(m) == 0 {
			return
		}
		ref := mapStringValue(m, "ref")
		role := mapStringValue(m, "role")
		name := mapStringValue(m, "name")
		value := mapStringValue(m, "value")
		nextPrimary := inPrimaryContent || isPrimaryAriaContentContainer(role, name)
		if ref != "" {
			summary := ariaRefSummary{
				Ref:             ref,
				Role:            role,
				Name:            name,
				Value:           value,
				ActionHint:      ariaRoleActionHint(role),
				Specialized:     isSpecializedAriaRole(role),
				SubmitCandidate: isLikelySubmitRef(role, name),
			}
			if nextPrimary {
				primaryRefs = append(primaryRefs, summary)
			} else {
				secondaryRefs = append(secondaryRefs, summary)
			}
		}
		for _, child := range anySliceValue(m, "children") {
			walk(child, nextPrimary)
		}
	}
	walk(root, false)
	return append(primaryRefs, secondaryRefs...)
}

func buildAriaSnapshotTranscript(refs []ariaRefSummary, fallbackText string) string {
	if len(refs) == 0 {
		return strings.TrimSpace(fallbackText)
	}
	const maxTranscriptRefs = 24
	visibleRefs := refs
	if len(visibleRefs) > maxTranscriptRefs {
		visibleRefs = visibleRefs[:maxTranscriptRefs]
	}
	lines := make([]string, 0, len(visibleRefs)+3)
	lines = append(lines, fmt.Sprintf("ARIA snapshot: %d interactive refs", len(refs)))
	lines = append(lines, "Primary page-content refs are listed first. Choose refs by role and accessible name, not by numeric order alone.")
	for _, ref := range visibleRefs {
		line := fmt.Sprintf("[%s] %s", ref.Ref, fallbackString(ref.Role, "element"))
		if ref.Name != "" {
			line += fmt.Sprintf(" %q", ref.Name)
		}
		if ref.Value != "" {
			line += fmt.Sprintf(" value=%q", ref.Value)
		}
		if ref.ActionHint != "" {
			line += " action=" + ref.ActionHint
		}
		if ref.Specialized {
			line += " specialized=true"
		}
		if ref.SubmitCandidate {
			line += " submit_candidate=true"
		}
		lines = append(lines, line)
	}
	if len(refs) > len(visibleRefs) {
		lines = append(lines, fmt.Sprintf("... %d additional refs omitted. Use the structured refs list if you need more.", len(refs)-len(visibleRefs)))
	}
	return strings.Join(lines, "\n")
}

func isPrimaryAriaContentContainer(role, name string) bool {
	role = strings.ToLower(strings.TrimSpace(role))
	name = strings.ToLower(strings.TrimSpace(name))
	if role == "main" || role == "article" {
		return true
	}
	for _, needle := range []string{
		"search results", "search result", "results",
		"搜索结果", "检索结果", "结果",
	} {
		if strings.Contains(name, needle) {
			return true
		}
	}
	return false
}

func ariaRoleActionHint(role string) string {
	switch strings.ToLower(strings.TrimSpace(role)) {
	case "textbox", "searchbox", "textfield", "spinbutton":
		return "type"
	case "combobox", "listbox":
		return "select"
	case "button", "link", "radio", "checkbox", "switch", "tab",
		"menuitem", "menuitemcheckbox", "menuitemradio", "option",
		"treeitem", "gridcell":
		return "click"
	default:
		return ""
	}
}

func isLikelySubmitRef(role, name string) bool {
	switch ariaRoleActionHint(role) {
	case "click", "select":
	default:
		return false
	}
	lower := strings.ToLower(strings.TrimSpace(name))
	if lower == "" {
		return false
	}
	for _, needle := range []string{
		"submit", "confirm", "save", "send", "finish", "done",
		"提交", "确认", "保存", "发送", "完成", "确定",
	} {
		if strings.Contains(lower, needle) {
			return true
		}
	}
	return false
}

func isSpecializedAriaRole(role string) bool {
	switch strings.ToLower(strings.TrimSpace(role)) {
	case "spinbutton", "combobox", "listbox", "slider":
		return true
	default:
		return false
	}
}

func fallbackString(primary, fallback string) string {
	if strings.TrimSpace(primary) != "" {
		return strings.TrimSpace(primary)
	}
	return strings.TrimSpace(fallback)
}

// ---------------------------------------------------------------------------
// browser.click_aria handler
// ---------------------------------------------------------------------------
