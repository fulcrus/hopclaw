package toolruntime

import (
	"context"
	"fmt"

	"github.com/fulcrus/hopclaw/agent"
	browsertypes "github.com/fulcrus/hopclaw/browserapi/types"
	"github.com/fulcrus/hopclaw/contextengine"
)

func handleBrowserTabNewImpl(ctx context.Context, b *Builtins, call agent.ToolCall) (contextengine.ToolResult, error) {
	client, err := requireBrowserClient(b)
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("browser.tab_new: %w", err)
	}

	sessionID, err := requiredString(call.Input, "session_id")
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("browser.tab_new: %w", err)
	}

	params := make(map[string]any)
	if rawURL, _ := stringFrom(call.Input["url"]); rawURL != "" {
		params["url"] = rawURL
	}

	resp, err := doBrowserAction(ctx, client, browsertypes.ActionNewTab, sessionID, params)
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("browser.tab_new: %w", err)
	}

	targetID := ""
	if resp.Data != nil {
		if id, ok := resp.Data["target_id"].(string); ok {
			targetID = id
		}
	}

	return b.jsonResult(call, map[string]any{"ok": true, "target_id": targetID})
}

func handleBrowserTabSwitchImpl(ctx context.Context, b *Builtins, call agent.ToolCall) (contextengine.ToolResult, error) {
	client, err := requireBrowserClient(b)
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("browser.tab_switch: %w", err)
	}

	sessionID, err := requiredString(call.Input, "session_id")
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("browser.tab_switch: %w", err)
	}
	targetID, err := requiredString(call.Input, "target_id")
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("browser.tab_switch: %w", err)
	}

	_, err = doBrowserAction(ctx, client, browsertypes.ActionSwitchTab, sessionID, map[string]any{
		"target_id": targetID,
	})
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("browser.tab_switch: %w", err)
	}

	return b.jsonResult(call, map[string]any{"ok": true})
}

func handleBrowserTabCloseImpl(ctx context.Context, b *Builtins, call agent.ToolCall) (contextengine.ToolResult, error) {
	client, err := requireBrowserClient(b)
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("browser.tab_close: %w", err)
	}

	sessionID, err := requiredString(call.Input, "session_id")
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("browser.tab_close: %w", err)
	}
	targetID, err := requiredString(call.Input, "target_id")
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("browser.tab_close: %w", err)
	}

	_, err = doBrowserAction(ctx, client, browsertypes.ActionCloseTab, sessionID, map[string]any{
		"target_id": targetID,
	})
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("browser.tab_close: %w", err)
	}

	return b.jsonResult(call, map[string]any{"ok": true})
}

func handleBrowserElementTextImpl(ctx context.Context, b *Builtins, call agent.ToolCall) (contextengine.ToolResult, error) {
	client, err := requireBrowserClient(b)
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("browser.element_text: %w", err)
	}

	sessionID, err := requiredString(call.Input, "session_id")
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("browser.element_text: %w", err)
	}
	selector, err := requiredString(call.Input, "selector")
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("browser.element_text: %w", err)
	}

	resp, err := doBrowserAction(ctx, client, browsertypes.ActionElementText, sessionID, map[string]any{
		"selector": selector,
	})
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("browser.element_text: %w", err)
	}

	text := ""
	if resp.Data != nil {
		if t, ok := resp.Data["text"].(string); ok {
			text = t
		}
	}

	return b.jsonResult(call, map[string]any{"ok": true, "text": text})
}

func handleBrowserElementAttrImpl(ctx context.Context, b *Builtins, call agent.ToolCall) (contextengine.ToolResult, error) {
	client, err := requireBrowserClient(b)
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("browser.element_attr: %w", err)
	}

	sessionID, err := requiredString(call.Input, "session_id")
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("browser.element_attr: %w", err)
	}
	selector, err := requiredString(call.Input, "selector")
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("browser.element_attr: %w", err)
	}
	attribute, err := requiredString(call.Input, "attribute")
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("browser.element_attr: %w", err)
	}

	resp, err := doBrowserAction(ctx, client, browsertypes.ActionElementAttr, sessionID, map[string]any{
		"selector":  selector,
		"attribute": attribute,
	})
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("browser.element_attr: %w", err)
	}

	var value any
	found := false
	if resp.Data != nil {
		value = resp.Data["value"]
		if f, ok := resp.Data["found"].(bool); ok {
			found = f
		}
	}

	return b.jsonResult(call, map[string]any{"ok": true, "value": value, "found": found})
}

func handleBrowserElementVisibleImpl(ctx context.Context, b *Builtins, call agent.ToolCall) (contextengine.ToolResult, error) {
	client, err := requireBrowserClient(b)
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("browser.element_visible: %w", err)
	}

	sessionID, err := requiredString(call.Input, "session_id")
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("browser.element_visible: %w", err)
	}
	selector, err := requiredString(call.Input, "selector")
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("browser.element_visible: %w", err)
	}

	resp, err := doBrowserAction(ctx, client, browsertypes.ActionElementVisible, sessionID, map[string]any{
		"selector": selector,
	})
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("browser.element_visible: %w", err)
	}

	visible := false
	if resp.Data != nil {
		if v, ok := resp.Data["visible"].(bool); ok {
			visible = v
		}
	}

	return b.jsonResult(call, map[string]any{"ok": true, "visible": visible})
}

func handleBrowserKeyboardImpl(ctx context.Context, b *Builtins, call agent.ToolCall) (contextengine.ToolResult, error) {
	client, err := requireBrowserClient(b)
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("browser.keyboard: %w", err)
	}

	sessionID, err := requiredString(call.Input, "session_id")
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("browser.keyboard: %w", err)
	}
	keys, err := requiredString(call.Input, "keys")
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("browser.keyboard: %w", err)
	}

	_, err = doBrowserAction(ctx, client, browsertypes.ActionKeyboard, sessionID, map[string]any{
		"keys": keys,
	})
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("browser.keyboard: %w", err)
	}

	return b.jsonResult(call, map[string]any{"ok": true})
}

func handleBrowserIframeImpl(ctx context.Context, b *Builtins, call agent.ToolCall) (contextengine.ToolResult, error) {
	client, err := requireBrowserClient(b)
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("browser.iframe: %w", err)
	}

	sessionID, err := requiredString(call.Input, "session_id")
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("browser.iframe: %w", err)
	}

	params := make(map[string]any)
	if selector, _ := stringFrom(call.Input["selector"]); selector != "" {
		params["selector"] = selector
	}

	resp, err := doBrowserAction(ctx, client, browsertypes.ActionIframe, sessionID, params)
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("browser.iframe: %w", err)
	}

	frame := "main"
	if resp.Data != nil {
		if f, ok := resp.Data["frame"].(string); ok {
			frame = f
		}
	}

	return b.jsonResult(call, map[string]any{"ok": true, "frame": frame})
}
