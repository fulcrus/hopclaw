package toolruntime

import (
	"context"
	"fmt"

	"github.com/fulcrus/hopclaw/agent"
	browsertypes "github.com/fulcrus/hopclaw/browserapi/types"
	"github.com/fulcrus/hopclaw/contextengine"
)

func handleBrowserTraceStartImpl(ctx context.Context, b *Builtins, call agent.ToolCall) (contextengine.ToolResult, error) {
	client, err := requireBrowserClient(b)
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("browser.trace_start: %w", err)
	}

	sessionID, err := requiredString(call.Input, "session_id")
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("browser.trace_start: %w", err)
	}

	params := make(map[string]any)
	if categories, catErr := stringSliceFrom(call.Input["categories"]); catErr == nil && len(categories) > 0 {
		params["categories"] = categories
	}

	resp, err := doBrowserAction(ctx, client, browsertypes.ActionTraceStart, sessionID, params)
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("browser.trace_start: %w", err)
	}

	categories := ""
	if resp.Data != nil {
		if c, ok := resp.Data["categories"].(string); ok {
			categories = c
		}
	}

	return b.jsonResult(call, map[string]any{"ok": true, "categories": categories})
}

func handleBrowserTraceStopImpl(ctx context.Context, b *Builtins, call agent.ToolCall) (contextengine.ToolResult, error) {
	client, err := requireBrowserClient(b)
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("browser.trace_stop: %w", err)
	}

	sessionID, err := requiredString(call.Input, "session_id")
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("browser.trace_stop: %w", err)
	}

	resp, err := doBrowserAction(ctx, client, browsertypes.ActionTraceStop, sessionID, nil)
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("browser.trace_stop: %w", err)
	}

	eventCount := 0
	contentBase64 := ""
	if resp.Data != nil {
		if c, ok := resp.Data["event_count"].(float64); ok {
			eventCount = int(c)
		} else if c, ok := resp.Data["event_count"].(int); ok {
			eventCount = c
		}
		if s, ok := resp.Data["content_base64"].(string); ok {
			contentBase64 = s
		}
	}

	return b.jsonResult(call, map[string]any{
		"ok":             true,
		"event_count":    eventCount,
		"content_base64": contentBase64,
	})
}

func handleBrowserHARStartImpl(ctx context.Context, b *Builtins, call agent.ToolCall) (contextengine.ToolResult, error) {
	client, err := requireBrowserClient(b)
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("browser.har_start: %w", err)
	}

	sessionID, err := requiredString(call.Input, "session_id")
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("browser.har_start: %w", err)
	}

	_, err = doBrowserAction(ctx, client, browsertypes.ActionHARStart, sessionID, nil)
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("browser.har_start: %w", err)
	}

	return b.jsonResult(call, map[string]any{"ok": true})
}

func handleBrowserHARStopImpl(ctx context.Context, b *Builtins, call agent.ToolCall) (contextengine.ToolResult, error) {
	client, err := requireBrowserClient(b)
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("browser.har_stop: %w", err)
	}

	sessionID, err := requiredString(call.Input, "session_id")
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("browser.har_stop: %w", err)
	}

	params := make(map[string]any)
	if format, _ := stringFrom(call.Input["format"]); format != "" {
		params["format"] = format
	}

	resp, err := doBrowserAction(ctx, client, browsertypes.ActionHARStop, sessionID, params)
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("browser.har_stop: %w", err)
	}

	entryCount := 0
	var entries any
	format := "summary"
	if resp.Data != nil {
		if c, ok := resp.Data["entry_count"].(float64); ok {
			entryCount = int(c)
		} else if c, ok := resp.Data["entry_count"].(int); ok {
			entryCount = c
		}
		entries = resp.Data["entries"]
		if f, ok := resp.Data["format"].(string); ok {
			format = f
		}
	}
	if entries == nil {
		entries = []any{}
	}

	return b.jsonResult(call, map[string]any{
		"ok":          true,
		"format":      format,
		"entry_count": entryCount,
		"entries":     entries,
	})
}

func handleBrowserConsoleStartImpl(ctx context.Context, b *Builtins, call agent.ToolCall) (contextengine.ToolResult, error) {
	client, err := requireBrowserClient(b)
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("browser.console_start: %w", err)
	}

	sessionID, err := requiredString(call.Input, "session_id")
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("browser.console_start: %w", err)
	}

	_, err = doBrowserAction(ctx, client, browsertypes.ActionConsoleStart, sessionID, nil)
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("browser.console_start: %w", err)
	}

	return b.jsonResult(call, map[string]any{"ok": true})
}

func handleBrowserConsoleMessagesImpl(ctx context.Context, b *Builtins, call agent.ToolCall) (contextengine.ToolResult, error) {
	client, err := requireBrowserClient(b)
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("browser.console_messages: %w", err)
	}

	sessionID, err := requiredString(call.Input, "session_id")
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("browser.console_messages: %w", err)
	}

	params := make(map[string]any)
	if call.Input["clear"] != nil {
		if clear, ok := call.Input["clear"].(bool); ok {
			params["clear"] = clear
		}
	}

	resp, err := doBrowserAction(ctx, client, browsertypes.ActionConsoleMessages, sessionID, params)
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("browser.console_messages: %w", err)
	}

	messageCount := 0
	var messages any
	if resp.Data != nil {
		if c, ok := resp.Data["message_count"].(float64); ok {
			messageCount = int(c)
		} else if c, ok := resp.Data["message_count"].(int); ok {
			messageCount = c
		}
		messages = resp.Data["messages"]
	}
	if messages == nil {
		messages = []any{}
	}

	return b.jsonResult(call, map[string]any{
		"ok":            true,
		"message_count": messageCount,
		"messages":      messages,
	})
}

func handleBrowserPerformanceImpl(ctx context.Context, b *Builtins, call agent.ToolCall) (contextengine.ToolResult, error) {
	client, err := requireBrowserClient(b)
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("browser.performance: %w", err)
	}

	sessionID, err := requiredString(call.Input, "session_id")
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("browser.performance: %w", err)
	}

	resp, err := doBrowserAction(ctx, client, browsertypes.ActionPerformanceMetrics, sessionID, nil)
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("browser.performance: %w", err)
	}

	var metrics any
	if resp.Data != nil {
		metrics = resp.Data["metrics"]
	}
	if metrics == nil {
		metrics = map[string]any{}
	}

	return b.jsonResult(call, map[string]any{"ok": true, "metrics": metrics})
}

func handleBrowserEmulateDeviceImpl(ctx context.Context, b *Builtins, call agent.ToolCall) (contextengine.ToolResult, error) {
	client, err := requireBrowserClient(b)
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("browser.emulate_device: %w", err)
	}
	sessionID, err := requiredString(call.Input, "session_id")
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("browser.emulate_device: %w", err)
	}

	params := make(map[string]any)
	for _, key := range []string{"device", "width", "height", "scale", "mobile", "user_agent"} {
		if v, ok := call.Input[key]; ok {
			params[key] = v
		}
	}

	resp, err := doBrowserAction(ctx, client, browsertypes.ActionEmulateDevice, sessionID, params)
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("browser.emulate_device: %w", err)
	}

	result := map[string]any{"ok": true}
	if resp.Data != nil {
		for _, key := range []string{"width", "height", "scale", "mobile", "user_agent", "device"} {
			if v, ok := resp.Data[key]; ok {
				result[key] = v
			}
		}
	}
	return b.jsonResult(call, result)
}

func handleBrowserEmulateVisionImpl(ctx context.Context, b *Builtins, call agent.ToolCall) (contextengine.ToolResult, error) {
	client, err := requireBrowserClient(b)
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("browser.emulate_vision: %w", err)
	}
	sessionID, err := requiredString(call.Input, "session_id")
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("browser.emulate_vision: %w", err)
	}
	visionType, err := requiredString(call.Input, "type")
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("browser.emulate_vision: %w", err)
	}

	_, err = doBrowserAction(ctx, client, browsertypes.ActionEmulateVision, sessionID, map[string]any{
		"type": visionType,
	})
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("browser.emulate_vision: %w", err)
	}
	return b.jsonResult(call, map[string]any{"ok": true, "type": visionType})
}

func handleBrowserSetGeolocationImpl(ctx context.Context, b *Builtins, call agent.ToolCall) (contextengine.ToolResult, error) {
	client, err := requireBrowserClient(b)
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("browser.set_geolocation: %w", err)
	}
	sessionID, err := requiredString(call.Input, "session_id")
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("browser.set_geolocation: %w", err)
	}
	params := make(map[string]any)
	for _, key := range []string{"latitude", "longitude", "accuracy", "clear"} {
		if v, ok := call.Input[key]; ok {
			params[key] = v
		}
	}
	resp, err := doBrowserAction(ctx, client, browsertypes.ActionSetGeolocation, sessionID, params)
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("browser.set_geolocation: %w", err)
	}
	result := map[string]any{"ok": true}
	if resp.Data != nil {
		for k, v := range resp.Data {
			result[k] = v
		}
	}
	return b.jsonResult(call, result)
}

func handleBrowserSetTimezoneImpl(ctx context.Context, b *Builtins, call agent.ToolCall) (contextengine.ToolResult, error) {
	client, err := requireBrowserClient(b)
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("browser.set_timezone: %w", err)
	}
	sessionID, err := requiredString(call.Input, "session_id")
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("browser.set_timezone: %w", err)
	}
	timezoneID, err := requiredString(call.Input, "timezone_id")
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("browser.set_timezone: %w", err)
	}
	_, err = doBrowserAction(ctx, client, browsertypes.ActionSetTimezone, sessionID, map[string]any{
		"timezone_id": timezoneID,
	})
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("browser.set_timezone: %w", err)
	}
	return b.jsonResult(call, map[string]any{"ok": true, "timezone_id": timezoneID})
}

func handleBrowserSetLocaleImpl(ctx context.Context, b *Builtins, call agent.ToolCall) (contextengine.ToolResult, error) {
	client, err := requireBrowserClient(b)
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("browser.set_locale: %w", err)
	}
	sessionID, err := requiredString(call.Input, "session_id")
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("browser.set_locale: %w", err)
	}
	locale, err := requiredString(call.Input, "locale")
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("browser.set_locale: %w", err)
	}
	_, err = doBrowserAction(ctx, client, browsertypes.ActionSetLocale, sessionID, map[string]any{
		"locale": locale,
	})
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("browser.set_locale: %w", err)
	}
	return b.jsonResult(call, map[string]any{"ok": true, "locale": locale})
}

func handleBrowserSetColorSchemeImpl(ctx context.Context, b *Builtins, call agent.ToolCall) (contextengine.ToolResult, error) {
	client, err := requireBrowserClient(b)
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("browser.set_color_scheme: %w", err)
	}
	sessionID, err := requiredString(call.Input, "session_id")
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("browser.set_color_scheme: %w", err)
	}
	scheme, err := requiredString(call.Input, "scheme")
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("browser.set_color_scheme: %w", err)
	}
	_, err = doBrowserAction(ctx, client, browsertypes.ActionSetColorScheme, sessionID, map[string]any{
		"scheme": scheme,
	})
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("browser.set_color_scheme: %w", err)
	}
	return b.jsonResult(call, map[string]any{"ok": true, "color_scheme": scheme})
}

func handleBrowserSetOfflineImpl(ctx context.Context, b *Builtins, call agent.ToolCall) (contextengine.ToolResult, error) {
	client, err := requireBrowserClient(b)
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("browser.set_offline: %w", err)
	}
	sessionID, err := requiredString(call.Input, "session_id")
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("browser.set_offline: %w", err)
	}
	params := make(map[string]any)
	if v, ok := call.Input["offline"]; ok {
		params["offline"] = v
	}
	_, err = doBrowserAction(ctx, client, browsertypes.ActionSetOffline, sessionID, params)
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("browser.set_offline: %w", err)
	}
	offline := true
	if v, ok := call.Input["offline"].(bool); ok {
		offline = v
	}
	return b.jsonResult(call, map[string]any{"ok": true, "offline": offline})
}

func handleBrowserSetHeadersImpl(ctx context.Context, b *Builtins, call agent.ToolCall) (contextengine.ToolResult, error) {
	client, err := requireBrowserClient(b)
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("browser.set_headers: %w", err)
	}
	sessionID, err := requiredString(call.Input, "session_id")
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("browser.set_headers: %w", err)
	}
	headers, ok := call.Input["headers"].(map[string]any)
	if !ok || len(headers) == 0 {
		return contextengine.ToolResult{}, fmt.Errorf("browser.set_headers: headers must be a non-empty object")
	}
	_, err = doBrowserAction(ctx, client, browsertypes.ActionSetHeaders, sessionID, map[string]any{
		"headers": headers,
	})
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("browser.set_headers: %w", err)
	}
	return b.jsonResult(call, map[string]any{"ok": true, "headers_set": len(headers)})
}

func handleBrowserSetCredentialsImpl(ctx context.Context, b *Builtins, call agent.ToolCall) (contextengine.ToolResult, error) {
	client, err := requireBrowserClient(b)
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("browser.set_credentials: %w", err)
	}
	sessionID, err := requiredString(call.Input, "session_id")
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("browser.set_credentials: %w", err)
	}
	params := make(map[string]any)
	for _, key := range []string{"username", "password", "clear"} {
		if v, ok := call.Input[key]; ok {
			params[key] = v
		}
	}
	_, err = doBrowserAction(ctx, client, browsertypes.ActionSetCredentials, sessionID, params)
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("browser.set_credentials: %w", err)
	}
	return b.jsonResult(call, map[string]any{"ok": true})
}
