package toolruntime

import (
	"context"
	"fmt"

	"github.com/fulcrus/hopclaw/agent"
	browsertypes "github.com/fulcrus/hopclaw/browserapi/types"
	"github.com/fulcrus/hopclaw/contextengine"
)

func handleBrowserReloadImpl(ctx context.Context, b *Builtins, call agent.ToolCall) (contextengine.ToolResult, error) {
	client, err := requireBrowserClient(b)
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("browser.reload: %w", err)
	}

	sessionID, err := requiredString(call.Input, "session_id")
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("browser.reload: %w", err)
	}

	params := make(map[string]any)
	if waitUntil, _ := stringFrom(call.Input["wait_until"]); waitUntil != "" {
		params["wait_until"] = waitUntil
	}

	_, err = doBrowserAction(ctx, client, browsertypes.ActionReload, sessionID, params)
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("browser.reload: %w", err)
	}

	return b.jsonResult(call, map[string]any{"ok": true})
}

func handleBrowserBackImpl(ctx context.Context, b *Builtins, call agent.ToolCall) (contextengine.ToolResult, error) {
	client, err := requireBrowserClient(b)
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("browser.back: %w", err)
	}

	sessionID, err := requiredString(call.Input, "session_id")
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("browser.back: %w", err)
	}

	resp, err := doBrowserAction(ctx, client, browsertypes.ActionBack, sessionID, nil)
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("browser.back: %w", err)
	}

	finalURL := ""
	if resp.Data != nil {
		if u, ok := resp.Data["url"].(string); ok {
			finalURL = u
		}
	}

	return b.jsonResult(call, map[string]any{"ok": true, "url": finalURL})
}

func handleBrowserForwardImpl(ctx context.Context, b *Builtins, call agent.ToolCall) (contextengine.ToolResult, error) {
	client, err := requireBrowserClient(b)
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("browser.forward: %w", err)
	}

	sessionID, err := requiredString(call.Input, "session_id")
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("browser.forward: %w", err)
	}

	resp, err := doBrowserAction(ctx, client, browsertypes.ActionForward, sessionID, nil)
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("browser.forward: %w", err)
	}

	finalURL := ""
	if resp.Data != nil {
		if u, ok := resp.Data["url"].(string); ok {
			finalURL = u
		}
	}

	return b.jsonResult(call, map[string]any{"ok": true, "url": finalURL})
}

func handleBrowserHoverImpl(ctx context.Context, b *Builtins, call agent.ToolCall) (contextengine.ToolResult, error) {
	client, err := requireBrowserClient(b)
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("browser.hover: %w", err)
	}

	sessionID, err := requiredString(call.Input, "session_id")
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("browser.hover: %w", err)
	}
	selector, err := requiredString(call.Input, "selector")
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("browser.hover: %w", err)
	}

	_, err = doBrowserAction(ctx, client, browsertypes.ActionHover, sessionID, map[string]any{
		"selector": selector,
	})
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("browser.hover: %w", err)
	}

	return b.jsonResult(call, map[string]any{"ok": true})
}

func handleBrowserSelectImpl(ctx context.Context, b *Builtins, call agent.ToolCall) (contextengine.ToolResult, error) {
	client, err := requireBrowserClient(b)
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("browser.select: %w", err)
	}

	sessionID, err := requiredString(call.Input, "session_id")
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("browser.select: %w", err)
	}
	selector, err := requiredString(call.Input, "selector")
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("browser.select: %w", err)
	}
	values, err := stringSliceFrom(call.Input["values"])
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("browser.select: %w", err)
	}
	if len(values) == 0 {
		return contextengine.ToolResult{}, fmt.Errorf("browser.select: values is required")
	}

	_, err = doBrowserAction(ctx, client, browsertypes.ActionSelectOption, sessionID, map[string]any{
		"selector": selector,
		"values":   values,
	})
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("browser.select: %w", err)
	}

	return b.jsonResult(call, map[string]any{"ok": true})
}

func handleBrowserFillImpl(ctx context.Context, b *Builtins, call agent.ToolCall) (contextengine.ToolResult, error) {
	client, err := requireBrowserClient(b)
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("browser.fill: %w", err)
	}

	sessionID, err := requiredString(call.Input, "session_id")
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("browser.fill: %w", err)
	}
	selector, err := requiredString(call.Input, "selector")
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("browser.fill: %w", err)
	}
	value, err := requiredString(call.Input, "value")
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("browser.fill: %w", err)
	}

	_, err = doBrowserAction(ctx, client, browsertypes.ActionFill, sessionID, map[string]any{
		"selector": selector,
		"value":    value,
	})
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("browser.fill: %w", err)
	}

	return b.jsonResult(call, map[string]any{"ok": true})
}

func handleBrowserStorageGetImpl(ctx context.Context, b *Builtins, call agent.ToolCall) (contextengine.ToolResult, error) {
	client, err := requireBrowserClient(b)
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("browser.storage_get: %w", err)
	}

	sessionID, err := requiredString(call.Input, "session_id")
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("browser.storage_get: %w", err)
	}
	key, err := requiredString(call.Input, "key")
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("browser.storage_get: %w", err)
	}
	storageType, _ := stringFrom(call.Input["storage_type"])
	if storageType == "" {
		storageType = defaultStorageType
	}

	storageObj := "localStorage"
	if storageType == "session" {
		storageObj = "sessionStorage"
	}
	expression := fmt.Sprintf("window.%s.getItem(%q)", storageObj, key)

	resp, err := doBrowserAction(ctx, client, browsertypes.ActionEval, sessionID, map[string]any{
		"expression": expression,
	})
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("browser.storage_get: %w", err)
	}

	var value any
	if resp.Data != nil {
		value = resp.Data["result"]
	}

	return b.jsonResult(call, map[string]any{"ok": true, "value": value})
}

func handleBrowserStorageSetImpl(ctx context.Context, b *Builtins, call agent.ToolCall) (contextengine.ToolResult, error) {
	client, err := requireBrowserClient(b)
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("browser.storage_set: %w", err)
	}

	sessionID, err := requiredString(call.Input, "session_id")
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("browser.storage_set: %w", err)
	}
	key, err := requiredString(call.Input, "key")
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("browser.storage_set: %w", err)
	}
	value, err := requiredString(call.Input, "value")
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("browser.storage_set: %w", err)
	}
	storageType, _ := stringFrom(call.Input["storage_type"])
	if storageType == "" {
		storageType = defaultStorageType
	}

	storageObj := "localStorage"
	if storageType == "session" {
		storageObj = "sessionStorage"
	}
	expression := fmt.Sprintf("window.%s.setItem(%q, %q)", storageObj, key, value)

	_, err = doBrowserAction(ctx, client, browsertypes.ActionEval, sessionID, map[string]any{
		"expression": expression,
	})
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("browser.storage_set: %w", err)
	}

	return b.jsonResult(call, map[string]any{"ok": true})
}

func handleBrowserDialogHandleImpl(ctx context.Context, b *Builtins, call agent.ToolCall) (contextengine.ToolResult, error) {
	client, err := requireBrowserClient(b)
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("browser.dialog_handle: %w", err)
	}

	sessionID, err := requiredString(call.Input, "session_id")
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("browser.dialog_handle: %w", err)
	}
	action, err := requiredString(call.Input, "action")
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("browser.dialog_handle: %w", err)
	}

	params := map[string]any{"action": action}
	if promptText, _ := stringFrom(call.Input["prompt_text"]); promptText != "" {
		params["prompt_text"] = promptText
	}

	_, err = doBrowserAction(ctx, client, browsertypes.ActionHandleDialog, sessionID, params)
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("browser.dialog_handle: %w", err)
	}

	return b.jsonResult(call, map[string]any{"ok": true})
}

func handleBrowserPDFImpl(ctx context.Context, b *Builtins, call agent.ToolCall) (contextengine.ToolResult, error) {
	client, err := requireBrowserClient(b)
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("browser.pdf: %w", err)
	}

	sessionID, err := requiredString(call.Input, "session_id")
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("browser.pdf: %w", err)
	}

	params := make(map[string]any)
	if format, _ := stringFrom(call.Input["format"]); format != "" {
		params["format"] = format
	}
	if call.Input["landscape"] != nil {
		landscape, boolErr := boolFrom(call.Input["landscape"])
		if boolErr != nil {
			return contextengine.ToolResult{}, fmt.Errorf("browser.pdf: %w", boolErr)
		}
		params["landscape"] = landscape
	}

	resp, err := doBrowserAction(ctx, client, browsertypes.ActionPDF, sessionID, params)
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("browser.pdf: %w", err)
	}

	artifactRef := resp.ArtifactRef
	if artifactRef == "" && resp.Data != nil {
		if ref, ok := resp.Data["artifact_ref"].(string); ok {
			artifactRef = ref
		}
	}

	return b.jsonResult(call, map[string]any{
		"ok":           true,
		"artifact_ref": artifactRef,
		"artifact_uri": artifactRef,
	})
}

func handleBrowserNetworkEnableImpl(ctx context.Context, b *Builtins, call agent.ToolCall) (contextengine.ToolResult, error) {
	client, err := requireBrowserClient(b)
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("browser.network_enable: %w", err)
	}

	sessionID, err := requiredString(call.Input, "session_id")
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("browser.network_enable: %w", err)
	}

	_, err = doBrowserAction(ctx, client, browsertypes.ActionNetworkEnable, sessionID, nil)
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("browser.network_enable: %w", err)
	}

	return b.jsonResult(call, map[string]any{"ok": true})
}

func handleBrowserNetworkRequestsImpl(ctx context.Context, b *Builtins, call agent.ToolCall) (contextengine.ToolResult, error) {
	client, err := requireBrowserClient(b)
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("browser.network_requests: %w", err)
	}

	sessionID, err := requiredString(call.Input, "session_id")
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("browser.network_requests: %w", err)
	}

	params := make(map[string]any)
	if urlPattern, _ := stringFrom(call.Input["url_pattern"]); urlPattern != "" {
		params["url_pattern"] = urlPattern
	}

	resp, err := doBrowserAction(ctx, client, browsertypes.ActionNetworkReqs, sessionID, params)
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("browser.network_requests: %w", err)
	}

	var requests any
	if resp.Data != nil {
		requests = resp.Data["requests"]
	}
	if requests == nil {
		requests = []any{}
	}

	return b.jsonResult(call, map[string]any{"ok": true, "requests": requests})
}

func handleBrowserScrollImpl(ctx context.Context, b *Builtins, call agent.ToolCall) (contextengine.ToolResult, error) {
	client, err := requireBrowserClient(b)
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("browser.scroll: %w", err)
	}

	sessionID, err := requiredString(call.Input, "session_id")
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("browser.scroll: %w", err)
	}

	params := make(map[string]any)
	if direction, _ := stringFrom(call.Input["direction"]); direction != "" {
		params["direction"] = direction
	}
	if call.Input["amount"] != nil {
		amount, amountErr := intFrom(call.Input["amount"], defaultBrowserScrollAmount)
		if amountErr != nil {
			return contextengine.ToolResult{}, fmt.Errorf("browser.scroll: %w", amountErr)
		}
		params["amount"] = amount
	}
	if selector, _ := stringFrom(call.Input["selector"]); selector != "" {
		params["selector"] = selector
	}

	_, err = doBrowserAction(ctx, client, browsertypes.ActionScroll, sessionID, params)
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("browser.scroll: %w", err)
	}

	return b.jsonResult(call, map[string]any{"ok": true})
}

func handleBrowserDragImpl(ctx context.Context, b *Builtins, call agent.ToolCall) (contextengine.ToolResult, error) {
	client, err := requireBrowserClient(b)
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("browser.drag: %w", err)
	}

	sessionID, err := requiredString(call.Input, "session_id")
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("browser.drag: %w", err)
	}
	sourceSelector, err := requiredString(call.Input, "source_selector")
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("browser.drag: %w", err)
	}
	targetSelector, err := requiredString(call.Input, "target_selector")
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("browser.drag: %w", err)
	}

	_, err = doBrowserAction(ctx, client, browsertypes.ActionDrag, sessionID, map[string]any{
		"source_selector": sourceSelector,
		"target_selector": targetSelector,
	})
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("browser.drag: %w", err)
	}

	return b.jsonResult(call, map[string]any{"ok": true})
}

func handleBrowserUploadImpl(ctx context.Context, b *Builtins, call agent.ToolCall) (contextengine.ToolResult, error) {
	client, err := requireBrowserClient(b)
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("browser.upload: %w", err)
	}

	sessionID, err := requiredString(call.Input, "session_id")
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("browser.upload: %w", err)
	}
	selector, err := requiredString(call.Input, "selector")
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("browser.upload: %w", err)
	}
	filePath, err := requiredString(call.Input, "file_path")
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("browser.upload: %w", err)
	}

	_, err = doBrowserAction(ctx, client, browsertypes.ActionUpload, sessionID, map[string]any{
		"selector":  selector,
		"file_path": filePath,
	})
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("browser.upload: %w", err)
	}

	return b.jsonResult(call, map[string]any{"ok": true})
}

func handleBrowserDownloadImpl(ctx context.Context, b *Builtins, call agent.ToolCall) (contextengine.ToolResult, error) {
	client, err := requireBrowserClient(b)
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("browser.download: %w", err)
	}

	sessionID, err := requiredString(call.Input, "session_id")
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("browser.download: %w", err)
	}

	params := make(map[string]any)
	if rawURL, _ := stringFrom(call.Input["url"]); rawURL != "" {
		params["url"] = rawURL
	}
	if call.Input["timeout_ms"] != nil {
		timeoutMs, timeoutErr := intFrom(call.Input["timeout_ms"], defaultBrowserDownloadTimeout)
		if timeoutErr != nil {
			return contextengine.ToolResult{}, fmt.Errorf("browser.download: %w", timeoutErr)
		}
		params["timeout_ms"] = timeoutMs
	}

	resp, err := doBrowserAction(ctx, client, browsertypes.ActionDownload, sessionID, params)
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("browser.download: %w", err)
	}

	filePath := ""
	if resp.Data != nil {
		if fp, ok := resp.Data["file_path"].(string); ok {
			filePath = fp
		}
	}

	return b.jsonResult(call, map[string]any{"ok": true, "file_path": filePath})
}
