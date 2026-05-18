package gateway

import (
	"context"
	"net/http"
	"strings"

	"github.com/fulcrus/hopclaw/hooks"
)

// ---------------------------------------------------------------------------
// Constants
// ---------------------------------------------------------------------------

const (
	hookDefaultResultLimit = 50
)

// ---------------------------------------------------------------------------
// Request / response types
// ---------------------------------------------------------------------------

type hookCreateRequest struct {
	Name         string             `json:"name"`
	Enabled      *bool              `json:"enabled,omitempty"`
	Trigger      hooks.TriggerEvent `json:"trigger"`
	Kind         hooks.HookKind     `json:"kind"`
	Priority     int                `json:"priority,omitempty"`
	Phase        hooks.HookPhase    `json:"phase,omitempty"`
	Filter       string             `json:"filter,omitempty"`
	URL          string             `json:"url,omitempty"`
	Command      string             `json:"command,omitempty"`
	Headers      map[string]string  `json:"headers,omitempty"`
	Timeout      int                `json:"timeout,omitempty"`
	RetryCount   int                `json:"retry_count,omitempty"`
	Async        *bool              `json:"async,omitempty"`
	Secret       *string            `json:"secret,omitempty"`
	AutomationID string             `json:"automation_id,omitempty"`
}

type hookPatchRequest struct {
	Name         *string             `json:"name,omitempty"`
	Enabled      *bool               `json:"enabled,omitempty"`
	Priority     *int                `json:"priority,omitempty"`
	Phase        *hooks.HookPhase    `json:"phase,omitempty"`
	Filter       *string             `json:"filter,omitempty"`
	Trigger      *hooks.TriggerEvent `json:"trigger,omitempty"`
	Kind         *hooks.HookKind     `json:"kind,omitempty"`
	URL          *string             `json:"url,omitempty"`
	Command      *string             `json:"command,omitempty"`
	Headers      *map[string]string  `json:"headers,omitempty"`
	Timeout      *int                `json:"timeout,omitempty"`
	RetryCount   *int                `json:"retry_count,omitempty"`
	Async        *bool               `json:"async,omitempty"`
	Secret       *string             `json:"secret,omitempty"`
	AutomationID *string             `json:"automation_id,omitempty"`
}

type hookResponse struct {
	Hook hooks.Hook `json:"hook"`
}

type hookListResponse struct {
	Items []hooks.Hook `json:"items"`
	Count int          `json:"count"`
}

type hookEventsResponse struct {
	Items []hooks.EventSpec `json:"items"`
	Count int               `json:"count"`
}

type hookResultsResponse struct {
	Items []hooks.HookResult `json:"items"`
	Count int                `json:"count"`
}

type hookFireRequest struct {
	Trigger hooks.TriggerEvent `json:"trigger,omitempty"`
	Phase   hooks.HookPhase    `json:"phase,omitempty"`
	Payload map[string]any     `json:"payload,omitempty"`
}

type hookFireResponse struct {
	Result hooks.HookResult `json:"result"`
}

func hookFromCreateRequest(req hookCreateRequest) hooks.Hook {
	enabled := true
	if req.Enabled != nil {
		enabled = *req.Enabled
	}
	async := false
	if req.Async != nil {
		async = *req.Async
	}
	secret := ""
	if req.Secret != nil {
		secret = strings.TrimSpace(*req.Secret)
	}

	hook := hooks.Hook{
		Name:         strings.TrimSpace(req.Name),
		Enabled:      enabled,
		Trigger:      req.Trigger,
		Kind:         req.Kind,
		Priority:     req.Priority,
		Phase:        req.Phase,
		Filter:       strings.TrimSpace(req.Filter),
		URL:          strings.TrimSpace(req.URL),
		Command:      strings.TrimSpace(req.Command),
		Headers:      req.Headers,
		Timeout:      req.Timeout,
		RetryCount:   req.RetryCount,
		Async:        async,
		Secret:       secret,
		AutomationID: strings.TrimSpace(req.AutomationID),
	}
	normalizeHook(&hook)
	return hook
}

func normalizeHook(hook *hooks.Hook) {
	if hook == nil {
		return
	}
	hook.Name = strings.TrimSpace(hook.Name)
	hook.Filter = strings.TrimSpace(hook.Filter)
	hook.URL = strings.TrimSpace(hook.URL)
	hook.Command = strings.TrimSpace(hook.Command)
	hook.Secret = strings.TrimSpace(hook.Secret)
	hook.AutomationID = strings.TrimSpace(hook.AutomationID)
	hook.Headers = normalizeHookHeaders(hook.Headers)
	if hook.Phase == "" {
		hook.Phase = hooks.HookPhasePost
	}
	switch hook.Kind {
	case hooks.KindHTTP:
		hook.Command = ""
	case hooks.KindCommand:
		hook.URL = ""
	}
}

func normalizeHookHeaders(headers map[string]string) map[string]string {
	if len(headers) == 0 {
		return nil
	}
	out := make(map[string]string, len(headers))
	for key, value := range headers {
		name := http.CanonicalHeaderKey(strings.TrimSpace(key))
		if name == "" {
			continue
		}
		out[name] = strings.TrimSpace(value)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func hookMatchesAuthScope(scope authScope, hook *hooks.Hook) bool {
	if hook == nil {
		return false
	}
	if scope.isZero() {
		return true
	}
	return scope.scopeFilter().Matches(hook.Scope())
}

func (g *Gateway) filterHookResultsByAuthScope(ctx context.Context, scope authScope, hook *hooks.Hook, results []hooks.HookResult) []hooks.HookResult {
	return filterHookResultsForScope(hookOperatorDepsFromGateway(g), ctx, scope, hook, results)
}
