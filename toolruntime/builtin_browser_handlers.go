package toolruntime

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/fulcrus/hopclaw/agent"
	browsertypes "github.com/fulcrus/hopclaw/browserapi/types"
	"github.com/fulcrus/hopclaw/contextengine"
)

func handleBrowserOpen(ctx context.Context, b *Builtins, call agent.ToolCall) (contextengine.ToolResult, error) {
	client, err := requireBrowserClient(b)
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("browser.open: %w", err)
	}

	params := make(map[string]any)
	if rawURL, _ := stringFrom(call.Input["url"]); rawURL != "" {
		params["url"] = rawURL
	}

	resp, err := doBrowserAction(ctx, client, browsertypes.ActionCreateSession, "", params)
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("browser.open: %w", err)
	}

	sessionID := ""
	finalURL := ""
	title := ""
	if resp.SessionID != "" {
		sessionID = resp.SessionID
	} else if resp.Data != nil {
		if id, ok := resp.Data["session_id"].(string); ok {
			sessionID = id
		}
	}
	if resp.Data != nil {
		if raw, ok := resp.Data["url"].(string); ok {
			finalURL = raw
		}
		if raw, ok := resp.Data["title"].(string); ok {
			title = raw
		}
	}
	if finalURL == "" {
		if raw, ok := params["url"].(string); ok {
			finalURL = raw
		}
	}

	return b.jsonResult(call, map[string]any{
		"session_id": sessionID,
		"url":        finalURL,
		"title":      title,
	})
}

func handleBrowserClose(ctx context.Context, b *Builtins, call agent.ToolCall) (contextengine.ToolResult, error) {
	client, err := requireBrowserClient(b)
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("browser.close: %w", err)
	}

	sessionID, err := requiredString(call.Input, "session_id")
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("browser.close: %w", err)
	}

	_, err = doBrowserAction(ctx, client, browsertypes.ActionCloseSession, sessionID, nil)
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("browser.close: %w", err)
	}

	return b.jsonResult(call, map[string]any{
		"ok": true,
	})
}

func handleBrowserNavigate(ctx context.Context, b *Builtins, call agent.ToolCall) (contextengine.ToolResult, error) {
	client, err := requireBrowserClient(b)
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("browser.navigate: %w", err)
	}

	sessionID, err := requiredString(call.Input, "session_id")
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("browser.navigate: %w", err)
	}
	rawURL, err := requiredString(call.Input, "url")
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("browser.navigate: %w", err)
	}

	resp, err := doBrowserAction(ctx, client, browsertypes.ActionNavigate, sessionID, map[string]any{
		"url": rawURL,
	})
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("browser.navigate: %w", err)
	}

	finalURL := ""
	if resp.Data != nil {
		if u, ok := resp.Data["url"].(string); ok {
			finalURL = u
		}
	}

	return b.jsonResult(call, map[string]any{
		"ok":  true,
		"url": finalURL,
	})
}

func handleBrowserClick(ctx context.Context, b *Builtins, call agent.ToolCall) (contextengine.ToolResult, error) {
	client, err := requireBrowserClient(b)
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("browser.click: %w", err)
	}

	sessionID, err := requiredString(call.Input, "session_id")
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("browser.click: %w", err)
	}
	selector, err := requiredString(call.Input, "selector")
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("browser.click: %w", err)
	}

	resp, err := doBrowserAction(ctx, client, browsertypes.ActionClick, sessionID, map[string]any{
		"selector": selector,
	})
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("browser.click: %w", err)
	}

	result := map[string]any{
		"ok": true,
	}
	if resp != nil && resp.Data != nil {
		if raw, ok := resp.Data["url"].(string); ok && strings.TrimSpace(raw) != "" {
			result["url"] = raw
		}
		if raw, ok := resp.Data["title"].(string); ok && strings.TrimSpace(raw) != "" {
			result["title"] = raw
		}
	}
	return b.jsonResult(call, result)
}

func handleBrowserType(ctx context.Context, b *Builtins, call agent.ToolCall) (contextengine.ToolResult, error) {
	client, err := requireBrowserClient(b)
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("browser.type: %w", err)
	}

	sessionID, err := requiredString(call.Input, "session_id")
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("browser.type: %w", err)
	}
	selector, err := requiredString(call.Input, "selector")
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("browser.type: %w", err)
	}
	text, err := requiredString(call.Input, "text")
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("browser.type: %w", err)
	}

	resp, err := doBrowserAction(ctx, client, browsertypes.ActionType, sessionID, map[string]any{
		"selector": selector,
		"text":     text,
	})
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("browser.type: %w", err)
	}

	result := map[string]any{
		"ok": true,
	}
	if resp != nil && resp.Data != nil {
		if raw, ok := resp.Data["selector"].(string); ok && strings.TrimSpace(raw) != "" {
			result["selector"] = raw
		}
		if raw, ok := resp.Data["text"].(string); ok {
			result["text"] = raw
		}
	}
	return b.jsonResult(call, result)
}

func handleBrowserScreenshot(ctx context.Context, b *Builtins, call agent.ToolCall) (contextengine.ToolResult, error) {
	client, err := requireBrowserClient(b)
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("browser.screenshot: %w", err)
	}

	sessionID, err := requiredString(call.Input, "session_id")
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("browser.screenshot: %w", err)
	}

	resp, err := doBrowserActionWithTimeout(ctx, client, browsertypes.ActionScreenshot, sessionID, nil, defaultBrowserCaptureRequestTimeout)
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("browser.screenshot: %w", err)
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

func handleBrowserScreenshotLabeled(ctx context.Context, b *Builtins, call agent.ToolCall) (contextengine.ToolResult, error) {
	client, err := requireBrowserClient(b)
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("browser.screenshot_labeled: %w", err)
	}

	sessionID, err := requiredString(call.Input, "session_id")
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("browser.screenshot_labeled: %w", err)
	}

	resp, err := doBrowserActionWithTimeout(ctx, client, browsertypes.ActionScreenshotLabeled, sessionID, nil, defaultBrowserCaptureRequestTimeout)
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("browser.screenshot_labeled: %w", err)
	}

	artifactRef := resp.ArtifactRef
	if artifactRef == "" && resp.Data != nil {
		if ref, ok := resp.Data["artifact_ref"].(string); ok {
			artifactRef = ref
		}
	}

	result := map[string]any{
		"ok":           true,
		"artifact_ref": artifactRef,
	}
	if resp.Data != nil {
		if elems, ok := resp.Data["elements"]; ok {
			result["elements"] = elems
		}
		if count, ok := resp.Data["element_count"]; ok {
			result["element_count"] = count
		}
	}
	return b.jsonResult(call, result)
}

// ---------------------------------------------------------------------------
// browser.snapshot_aria handler
// ---------------------------------------------------------------------------

func handleBrowserSnapshotAria(ctx context.Context, b *Builtins, call agent.ToolCall) (contextengine.ToolResult, error) {
	client, err := requireBrowserClient(b)
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("browser.snapshot_aria: %w", err)
	}
	sessionID, err := requiredString(call.Input, "session_id")
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("browser.snapshot_aria: %w", err)
	}
	resp, err := doBrowserActionWithTimeout(ctx, client, browsertypes.ActionSnapshotAria, sessionID, nil, defaultBrowserCaptureRequestTimeout)
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("browser.snapshot_aria: %w", err)
	}

	result := map[string]any{
		"ok": true,
	}
	refs := flattenAriaRefs(resp.Data["tree"])
	if resp.Data != nil {
		if text, ok := resp.Data["text"]; ok {
			result["text"] = text
		}
		if count, ok := resp.Data["element_count"]; ok {
			result["element_count"] = count
		}
		if len(refs) > 0 {
			result["refs"] = refs
		}
		if tree, ok := resp.Data["tree"]; ok {
			result["tree"] = tree
		}
	}
	body, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return contextengine.ToolResult{}, err
	}
	transcript := buildAriaSnapshotTranscript(refs, mapStringValue(resp.Data, "text"))
	summary := "ARIA snapshot captured"
	if len(refs) > 0 {
		summary = fmt.Sprintf("ARIA snapshot captured (%d refs)", len(refs))
	}
	return contextengine.ToolResult{
		ToolName:       call.Name,
		ToolCallID:     call.ID,
		TranscriptText: transcript,
		Summary:        summary,
		Content:        string(body),
		Structured:     result,
	}.Normalized(), nil
}

func handleBrowserClickAria(ctx context.Context, b *Builtins, call agent.ToolCall) (contextengine.ToolResult, error) {
	client, err := requireBrowserClient(b)
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("browser.click_aria: %w", err)
	}
	sessionID, err := requiredString(call.Input, "session_id")
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("browser.click_aria: %w", err)
	}
	ref, err := requiredString(call.Input, "ref")
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("browser.click_aria: %w", err)
	}

	resp, err := doBrowserAction(ctx, client, browsertypes.ActionClickAria, sessionID, map[string]any{
		"ref": ref,
	})
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("browser.click_aria: %w", err)
	}
	result := map[string]any{
		"ok": true,
	}
	if resp != nil && resp.Data != nil {
		if raw, ok := resp.Data["ref"].(string); ok && strings.TrimSpace(raw) != "" {
			result["ref"] = raw
		}
		if raw, ok := resp.Data["clicked"].(bool); ok {
			result["clicked"] = raw
		}
		if raw, ok := resp.Data["url"].(string); ok && strings.TrimSpace(raw) != "" {
			result["url"] = raw
		}
		if raw, ok := resp.Data["title"].(string); ok && strings.TrimSpace(raw) != "" {
			result["title"] = raw
		}
	}
	return b.jsonResult(call, result)
}

// ---------------------------------------------------------------------------
// browser.type_aria handler
// ---------------------------------------------------------------------------

func handleBrowserTypeAria(ctx context.Context, b *Builtins, call agent.ToolCall) (contextengine.ToolResult, error) {
	client, err := requireBrowserClient(b)
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("browser.type_aria: %w", err)
	}
	sessionID, err := requiredString(call.Input, "session_id")
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("browser.type_aria: %w", err)
	}
	ref, err := requiredString(call.Input, "ref")
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("browser.type_aria: %w", err)
	}
	text, err := requiredString(call.Input, "text")
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("browser.type_aria: %w", err)
	}

	params := map[string]any{
		"ref":  ref,
		"text": text,
	}
	if clearVal, ok := call.Input["clear"]; ok {
		params["clear"] = clearVal
	}

	resp, err := doBrowserAction(ctx, client, browsertypes.ActionTypeAria, sessionID, params)
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("browser.type_aria: %w", err)
	}
	result := map[string]any{
		"ok": true,
	}
	if resp != nil && resp.Data != nil {
		if raw, ok := resp.Data["ref"].(string); ok && strings.TrimSpace(raw) != "" {
			result["ref"] = raw
		}
		if raw, ok := resp.Data["text"].(string); ok {
			result["text"] = raw
		}
		if raw, ok := resp.Data["typed"].(bool); ok {
			result["typed"] = raw
		}
	}
	return b.jsonResult(call, result)
}

func handleBrowserSnapshot(ctx context.Context, b *Builtins, call agent.ToolCall) (contextengine.ToolResult, error) {
	client, err := requireBrowserClient(b)
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("browser.snapshot: %w", err)
	}

	sessionID, err := requiredString(call.Input, "session_id")
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("browser.snapshot: %w", err)
	}

	resp, err := doBrowserActionWithTimeout(ctx, client, browsertypes.ActionSnapshot, sessionID, nil, defaultBrowserCaptureRequestTimeout)
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("browser.snapshot: %w", err)
	}

	content := ""
	pageURL := ""
	title := ""
	contentType := ""
	if resp.Data != nil {
		if html, ok := resp.Data["html"].(string); ok {
			content = html
		}
		if raw, ok := resp.Data["url"].(string); ok {
			pageURL = raw
		}
		if raw, ok := resp.Data["title"].(string); ok {
			title = raw
		}
		if raw, ok := resp.Data["content_type"].(string); ok {
			contentType = raw
		}
	}

	return b.jsonResult(call, map[string]any{
		"ok":           true,
		"content":      content,
		"html":         content,
		"url":          pageURL,
		"title":        title,
		"content_type": contentType,
	})
}

func handleBrowserEval(ctx context.Context, b *Builtins, call agent.ToolCall) (contextengine.ToolResult, error) {
	client, err := requireBrowserClient(b)
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("browser.eval: %w", err)
	}

	sessionID, err := requiredString(call.Input, "session_id")
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("browser.eval: %w", err)
	}
	expression, err := requiredString(call.Input, "expression")
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("browser.eval: %w", err)
	}

	resp, err := doBrowserAction(ctx, client, browsertypes.ActionEval, sessionID, map[string]any{
		"expression": expression,
	})
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("browser.eval: %w", err)
	}

	var result any
	if resp.Data != nil {
		result = resp.Data["result"]
	}

	return b.jsonResult(call, map[string]any{
		"ok":     true,
		"result": result,
	})
}

func handleBrowserWait(ctx context.Context, b *Builtins, call agent.ToolCall) (contextengine.ToolResult, error) {
	client, err := requireBrowserClient(b)
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("browser.wait: %w", err)
	}

	sessionID, err := requiredString(call.Input, "session_id")
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("browser.wait: %w", err)
	}
	selector, err := requiredString(call.Input, "selector")
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("browser.wait: %w", err)
	}
	timeoutMs, err := intFrom(call.Input["timeout_ms"], defaultBrowserWaitTimeout)
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("browser.wait: %w", err)
	}

	_, err = doBrowserAction(ctx, client, browsertypes.ActionWaitFor, sessionID, map[string]any{
		"selector":   selector,
		"timeout_ms": timeoutMs,
	})
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("browser.wait: %w", err)
	}

	return b.jsonResult(call, map[string]any{
		"ok": true,
	})
}

func handleBrowserTabs(ctx context.Context, b *Builtins, call agent.ToolCall) (contextengine.ToolResult, error) {
	client, err := requireBrowserClient(b)
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("browser.tabs: %w", err)
	}

	sessionID, err := requiredString(call.Input, "session_id")
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("browser.tabs: %w", err)
	}

	resp, err := doBrowserAction(ctx, client, browsertypes.ActionListTabs, sessionID, nil)
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("browser.tabs: %w", err)
	}

	var tabs any
	if resp.Data != nil {
		tabs = resp.Data["tabs"]
	}
	if tabs == nil {
		tabs = []any{}
	}

	return b.jsonResult(call, map[string]any{
		"tabs": tabs,
	})
}

func handleBrowserCookies(ctx context.Context, b *Builtins, call agent.ToolCall) (contextengine.ToolResult, error) {
	client, err := requireBrowserClient(b)
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("browser.cookies: %w", err)
	}

	sessionID, err := requiredString(call.Input, "session_id")
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("browser.cookies: %w", err)
	}

	resp, err := doBrowserAction(ctx, client, browsertypes.ActionGetCookies, sessionID, nil)
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("browser.cookies: %w", err)
	}

	var cookies any
	if resp.Data != nil {
		cookies = resp.Data["cookies"]
	}
	if cookies == nil {
		cookies = []any{}
	}

	return b.jsonResult(call, map[string]any{
		"cookies": cookies,
	})
}

func handleBrowserSetCookie(ctx context.Context, b *Builtins, call agent.ToolCall) (contextengine.ToolResult, error) {
	client, err := requireBrowserClient(b)
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("browser.set_cookie: %w", err)
	}

	sessionID, err := requiredString(call.Input, "session_id")
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("browser.set_cookie: %w", err)
	}
	name, err := requiredString(call.Input, "name")
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("browser.set_cookie: %w", err)
	}
	value, err := requiredString(call.Input, "value")
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("browser.set_cookie: %w", err)
	}
	domain, err := requiredString(call.Input, "domain")
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("browser.set_cookie: %w", err)
	}
	cookiePath, _ := stringFrom(call.Input["path"])
	if cookiePath == "" {
		cookiePath = "/"
	}

	_, err = doBrowserAction(ctx, client, browsertypes.ActionSetCookie, sessionID, map[string]any{
		"name":   name,
		"value":  value,
		"domain": domain,
		"path":   cookiePath,
	})
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("browser.set_cookie: %w", err)
	}

	return b.jsonResult(call, map[string]any{
		"ok": true,
	})
}

// ---------------------------------------------------------------------------
// browser.reload handler
// ---------------------------------------------------------------------------

func handleBrowserReload(ctx context.Context, b *Builtins, call agent.ToolCall) (contextengine.ToolResult, error) {
	return handleBrowserReloadImpl(ctx, b, call)
}

func handleBrowserBack(ctx context.Context, b *Builtins, call agent.ToolCall) (contextengine.ToolResult, error) {
	return handleBrowserBackImpl(ctx, b, call)
}

func handleBrowserForward(ctx context.Context, b *Builtins, call agent.ToolCall) (contextengine.ToolResult, error) {
	return handleBrowserForwardImpl(ctx, b, call)
}

func handleBrowserHover(ctx context.Context, b *Builtins, call agent.ToolCall) (contextengine.ToolResult, error) {
	return handleBrowserHoverImpl(ctx, b, call)
}

func handleBrowserSelect(ctx context.Context, b *Builtins, call agent.ToolCall) (contextengine.ToolResult, error) {
	return handleBrowserSelectImpl(ctx, b, call)
}

func handleBrowserFill(ctx context.Context, b *Builtins, call agent.ToolCall) (contextengine.ToolResult, error) {
	return handleBrowserFillImpl(ctx, b, call)
}

func handleBrowserStorageGet(ctx context.Context, b *Builtins, call agent.ToolCall) (contextengine.ToolResult, error) {
	return handleBrowserStorageGetImpl(ctx, b, call)
}

func handleBrowserStorageSet(ctx context.Context, b *Builtins, call agent.ToolCall) (contextengine.ToolResult, error) {
	return handleBrowserStorageSetImpl(ctx, b, call)
}

func handleBrowserDialogHandle(ctx context.Context, b *Builtins, call agent.ToolCall) (contextengine.ToolResult, error) {
	return handleBrowserDialogHandleImpl(ctx, b, call)
}

func handleBrowserPDF(ctx context.Context, b *Builtins, call agent.ToolCall) (contextengine.ToolResult, error) {
	return handleBrowserPDFImpl(ctx, b, call)
}

func handleBrowserNetworkEnable(ctx context.Context, b *Builtins, call agent.ToolCall) (contextengine.ToolResult, error) {
	return handleBrowserNetworkEnableImpl(ctx, b, call)
}

func handleBrowserNetworkRequests(ctx context.Context, b *Builtins, call agent.ToolCall) (contextengine.ToolResult, error) {
	return handleBrowserNetworkRequestsImpl(ctx, b, call)
}

func handleBrowserScroll(ctx context.Context, b *Builtins, call agent.ToolCall) (contextengine.ToolResult, error) {
	return handleBrowserScrollImpl(ctx, b, call)
}

func handleBrowserDrag(ctx context.Context, b *Builtins, call agent.ToolCall) (contextengine.ToolResult, error) {
	return handleBrowserDragImpl(ctx, b, call)
}

func handleBrowserUpload(ctx context.Context, b *Builtins, call agent.ToolCall) (contextengine.ToolResult, error) {
	return handleBrowserUploadImpl(ctx, b, call)
}

func handleBrowserDownload(ctx context.Context, b *Builtins, call agent.ToolCall) (contextengine.ToolResult, error) {
	return handleBrowserDownloadImpl(ctx, b, call)
}

// ---------------------------------------------------------------------------
// browser.tab_new handler
// ---------------------------------------------------------------------------

func handleBrowserTabNew(ctx context.Context, b *Builtins, call agent.ToolCall) (contextengine.ToolResult, error) {
	return handleBrowserTabNewImpl(ctx, b, call)
}

func handleBrowserTabSwitch(ctx context.Context, b *Builtins, call agent.ToolCall) (contextengine.ToolResult, error) {
	return handleBrowserTabSwitchImpl(ctx, b, call)
}

func handleBrowserTabClose(ctx context.Context, b *Builtins, call agent.ToolCall) (contextengine.ToolResult, error) {
	return handleBrowserTabCloseImpl(ctx, b, call)
}

func handleBrowserElementText(ctx context.Context, b *Builtins, call agent.ToolCall) (contextengine.ToolResult, error) {
	return handleBrowserElementTextImpl(ctx, b, call)
}

func handleBrowserElementAttr(ctx context.Context, b *Builtins, call agent.ToolCall) (contextengine.ToolResult, error) {
	return handleBrowserElementAttrImpl(ctx, b, call)
}

func handleBrowserElementVisible(ctx context.Context, b *Builtins, call agent.ToolCall) (contextengine.ToolResult, error) {
	return handleBrowserElementVisibleImpl(ctx, b, call)
}

func handleBrowserKeyboard(ctx context.Context, b *Builtins, call agent.ToolCall) (contextengine.ToolResult, error) {
	return handleBrowserKeyboardImpl(ctx, b, call)
}

func handleBrowserIframe(ctx context.Context, b *Builtins, call agent.ToolCall) (contextengine.ToolResult, error) {
	return handleBrowserIframeImpl(ctx, b, call)
}

func handleBrowserTraceStart(ctx context.Context, b *Builtins, call agent.ToolCall) (contextengine.ToolResult, error) {
	return handleBrowserTraceStartImpl(ctx, b, call)
}

func handleBrowserTraceStop(ctx context.Context, b *Builtins, call agent.ToolCall) (contextengine.ToolResult, error) {
	return handleBrowserTraceStopImpl(ctx, b, call)
}

func handleBrowserHARStart(ctx context.Context, b *Builtins, call agent.ToolCall) (contextengine.ToolResult, error) {
	return handleBrowserHARStartImpl(ctx, b, call)
}

func handleBrowserHARStop(ctx context.Context, b *Builtins, call agent.ToolCall) (contextengine.ToolResult, error) {
	return handleBrowserHARStopImpl(ctx, b, call)
}

func handleBrowserConsoleStart(ctx context.Context, b *Builtins, call agent.ToolCall) (contextengine.ToolResult, error) {
	return handleBrowserConsoleStartImpl(ctx, b, call)
}

func handleBrowserConsoleMessages(ctx context.Context, b *Builtins, call agent.ToolCall) (contextengine.ToolResult, error) {
	return handleBrowserConsoleMessagesImpl(ctx, b, call)
}

func handleBrowserPerformance(ctx context.Context, b *Builtins, call agent.ToolCall) (contextengine.ToolResult, error) {
	return handleBrowserPerformanceImpl(ctx, b, call)
}

func handleBrowserEmulateDevice(ctx context.Context, b *Builtins, call agent.ToolCall) (contextengine.ToolResult, error) {
	return handleBrowserEmulateDeviceImpl(ctx, b, call)
}

func handleBrowserEmulateVision(ctx context.Context, b *Builtins, call agent.ToolCall) (contextengine.ToolResult, error) {
	return handleBrowserEmulateVisionImpl(ctx, b, call)
}

func handleBrowserSetGeolocation(ctx context.Context, b *Builtins, call agent.ToolCall) (contextengine.ToolResult, error) {
	return handleBrowserSetGeolocationImpl(ctx, b, call)
}

func handleBrowserSetTimezone(ctx context.Context, b *Builtins, call agent.ToolCall) (contextengine.ToolResult, error) {
	return handleBrowserSetTimezoneImpl(ctx, b, call)
}

func handleBrowserSetLocale(ctx context.Context, b *Builtins, call agent.ToolCall) (contextengine.ToolResult, error) {
	return handleBrowserSetLocaleImpl(ctx, b, call)
}

func handleBrowserSetColorScheme(ctx context.Context, b *Builtins, call agent.ToolCall) (contextengine.ToolResult, error) {
	return handleBrowserSetColorSchemeImpl(ctx, b, call)
}

func handleBrowserSetOffline(ctx context.Context, b *Builtins, call agent.ToolCall) (contextengine.ToolResult, error) {
	return handleBrowserSetOfflineImpl(ctx, b, call)
}

func handleBrowserSetHeaders(ctx context.Context, b *Builtins, call agent.ToolCall) (contextengine.ToolResult, error) {
	return handleBrowserSetHeadersImpl(ctx, b, call)
}

func handleBrowserSetCredentials(ctx context.Context, b *Builtins, call agent.ToolCall) (contextengine.ToolResult, error) {
	return handleBrowserSetCredentialsImpl(ctx, b, call)
}
