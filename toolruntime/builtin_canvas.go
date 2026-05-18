package toolruntime

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"

	"github.com/fulcrus/hopclaw/agent"
	browsertypes "github.com/fulcrus/hopclaw/browserapi/types"
	"github.com/fulcrus/hopclaw/contextengine"
	"github.com/fulcrus/hopclaw/skill"
)

// ---------------------------------------------------------------------------
// Constants
// ---------------------------------------------------------------------------

// canvasBlankURL is the URL used to hide canvas content.
const canvasBlankURL = "about:blank"

// canvasDefaultWaitTimeoutMS is the default timeout for canvas.wait in milliseconds.
const canvasDefaultWaitTimeoutMS = 5000

// ---------------------------------------------------------------------------
// Tool definitions
// ---------------------------------------------------------------------------

func canvasToolDefs(cfg BuiltinsConfig) []builtinToolDef {
	_ = cfg
	return []builtinToolDef{
		{
			Manifest: skill.ToolManifest{
				Name:            "canvas.present",
				Description:     "Render HTML content in a browser session via a data URL.",
				InputSchema:     canvasPresentInputSchema(),
				OutputSchema:    canvasPresentOutputSchema(),
				SideEffectClass: "local_write",
				ExecutionKey:    "canvas:present:{session_id}",
			},
			Handler: handleCanvasPresent,
		},
		{
			Manifest: skill.ToolManifest{
				Name:            "canvas.eval",
				Description:     "Evaluate a JavaScript expression in the canvas browser session.",
				InputSchema:     canvasEvalInputSchema(),
				OutputSchema:    canvasEvalOutputSchema(),
				SideEffectClass: "local_write",
				ExecutionKey:    "canvas:eval:{session_id}",
			},
			Handler: handleCanvasEval,
		},
		{
			Manifest: skill.ToolManifest{
				Name:            "canvas.snapshot",
				Description:     "Capture a screenshot of the canvas browser session.",
				InputSchema:     canvasSnapshotInputSchema(),
				OutputSchema:    canvasSnapshotOutputSchema(),
				SideEffectClass: "read",
				Idempotent:      true,
				ExecutionKey:    "canvas:snapshot:{session_id}",
			},
			Handler: handleCanvasSnapshot,
		},
		{
			Manifest: skill.ToolManifest{
				Name:            "canvas.hide",
				Description:     "Hide the canvas by navigating to about:blank.",
				InputSchema:     canvasHideInputSchema(),
				OutputSchema:    canvasHideOutputSchema(),
				SideEffectClass: "local_write",
				ExecutionKey:    "canvas:hide:{session_id}",
			},
			Handler: handleCanvasHide,
		},
		{
			Manifest: skill.ToolManifest{
				Name:            "canvas.style",
				Description:     "Apply CSS styles to an element in the canvas browser session.",
				InputSchema:     canvasStyleInputSchema(),
				OutputSchema:    canvasStyleOutputSchema(),
				SideEffectClass: "local_write",
				ExecutionKey:    "canvas:style:{session_id}",
			},
			Handler: handleCanvasStyle,
		},
		{
			Manifest: skill.ToolManifest{
				Name:            "canvas.dom",
				Description:     "Perform DOM manipulation on the canvas (set_html, set_attr, create, remove).",
				InputSchema:     canvasDOMInputSchema(),
				OutputSchema:    canvasDOMOutputSchema(),
				SideEffectClass: "local_write",
				ExecutionKey:    "canvas:dom:{session_id}",
			},
			Handler: handleCanvasDOM,
		},
		{
			Manifest: skill.ToolManifest{
				Name:            "canvas.click",
				Description:     "Simulate a click on an element in the canvas browser session.",
				InputSchema:     canvasClickInputSchema(),
				OutputSchema:    canvasClickOutputSchema(),
				SideEffectClass: "local_write",
				ExecutionKey:    "canvas:click:{session_id}",
			},
			Handler: handleCanvasClick,
		},
		{
			Manifest: skill.ToolManifest{
				Name:            "canvas.type",
				Description:     "Type text into an element in the canvas browser session.",
				InputSchema:     canvasTypeInputSchema(),
				OutputSchema:    canvasTypeOutputSchema(),
				SideEffectClass: "local_write",
				ExecutionKey:    "canvas:type:{session_id}",
			},
			Handler: handleCanvasType,
		},
		{
			Manifest: skill.ToolManifest{
				Name:            "canvas.scroll",
				Description:     "Scroll to an element or coordinates in the canvas browser session.",
				InputSchema:     canvasScrollInputSchema(),
				OutputSchema:    canvasScrollOutputSchema(),
				SideEffectClass: "local_write",
				ExecutionKey:    "canvas:scroll:{session_id}",
			},
			Handler: handleCanvasScroll,
		},
		{
			Manifest: skill.ToolManifest{
				Name:            "canvas.console",
				Description:     "Capture, read, or clear console logs in the canvas browser session.",
				InputSchema:     canvasConsoleInputSchema(),
				OutputSchema:    canvasConsoleOutputSchema(),
				SideEffectClass: "read",
				ExecutionKey:    "canvas:console:{session_id}",
			},
			Handler: handleCanvasConsole,
		},
		{
			Manifest: skill.ToolManifest{
				Name:            "canvas.wait",
				Description:     "Wait for an element or JavaScript condition in the canvas browser session.",
				InputSchema:     canvasWaitInputSchema(),
				OutputSchema:    canvasWaitOutputSchema(),
				SideEffectClass: "read",
				ExecutionKey:    "canvas:wait:{session_id}",
			},
			Handler: handleCanvasWait,
		},
		{
			Manifest: skill.ToolManifest{
				Name:            "canvas.pdf",
				Description:     "Export the canvas browser session as a PDF.",
				InputSchema:     canvasPDFInputSchema(),
				OutputSchema:    canvasPDFOutputSchema(),
				SideEffectClass: "local_write",
				ExecutionKey:    "canvas:pdf:{session_id}",
			},
			Handler: handleCanvasPDF,
		},
	}
}

// ---------------------------------------------------------------------------
// Input schemas
// ---------------------------------------------------------------------------

func canvasPresentInputSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"session_id": stringSchema("Browser session ID to render content in."),
			"html":       stringSchema("HTML content to render (ignored when template is set)."),
			"title":      stringSchema("Optional title for the rendered page."),
			"template":   stringSchema("Name of a canvas template to use instead of raw HTML."),
			"params": map[string]any{
				"type":        "object",
				"description": "Parameters for the selected template.",
			},
		},
		"required":             []string{"session_id"},
		"additionalProperties": false,
	}
}

func canvasEvalInputSchema() map[string]any {
	return objectSchema(map[string]any{
		"session_id": stringSchema("Browser session ID."),
		"expression": stringSchema("JavaScript expression to evaluate."),
	}, "session_id", "expression")
}

func canvasSnapshotInputSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"session_id": stringSchema("Browser session ID."),
			"selector":   stringSchema("Optional CSS selector to capture a specific element."),
		},
		"required":             []string{"session_id"},
		"additionalProperties": false,
	}
}

func canvasHideInputSchema() map[string]any {
	return objectSchema(map[string]any{
		"session_id": stringSchema("Browser session ID to hide."),
	}, "session_id")
}

func canvasStyleInputSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"session_id": stringSchema("Browser session ID."),
			"selector":   stringSchema("CSS selector of the target element."),
			"css": map[string]any{
				"type":                 "object",
				"description":          "Map of CSS property names to values.",
				"additionalProperties": map[string]any{"type": "string"},
			},
		},
		"required":             []string{"session_id", "selector", "css"},
		"additionalProperties": false,
	}
}

func canvasDOMInputSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"session_id":      stringSchema("Browser session ID."),
			"operation":       stringSchema("DOM operation: set_html, set_attr, create, or remove."),
			"selector":        stringSchema("CSS selector of the target element."),
			"value":           stringSchema("Value for set_html or set_attr."),
			"attr_name":       stringSchema("Attribute name for set_attr."),
			"tag":             stringSchema("Tag name for create operation."),
			"parent_selector": stringSchema("Parent CSS selector for create operation."),
		},
		"required":             []string{"session_id", "operation", "selector"},
		"additionalProperties": false,
	}
}

func canvasClickInputSchema() map[string]any {
	return objectSchema(map[string]any{
		"session_id": stringSchema("Browser session ID."),
		"selector":   stringSchema("CSS selector of the element to click."),
	}, "session_id", "selector")
}

func canvasTypeInputSchema() map[string]any {
	return objectSchema(map[string]any{
		"session_id": stringSchema("Browser session ID."),
		"selector":   stringSchema("CSS selector of the input element."),
		"text":       stringSchema("Text to type into the element."),
	}, "session_id", "selector", "text")
}

func canvasScrollInputSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"session_id": stringSchema("Browser session ID."),
			"selector":   stringSchema("Optional CSS selector to scroll to."),
			"x":          numberSchema("Optional horizontal scroll coordinate."),
			"y":          numberSchema("Optional vertical scroll coordinate."),
		},
		"required":             []string{"session_id"},
		"additionalProperties": false,
	}
}

func canvasConsoleInputSchema() map[string]any {
	return objectSchema(map[string]any{
		"session_id": stringSchema("Browser session ID."),
		"operation":  stringSchema("Console operation: start, read, or clear."),
	}, "session_id", "operation")
}

func canvasWaitInputSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"session_id": stringSchema("Browser session ID."),
			"selector":   stringSchema("CSS selector to wait for."),
			"expression": stringSchema("JavaScript expression that must evaluate to truthy."),
			"timeout_ms": integerSchema("Timeout in milliseconds (default 5000)."),
		},
		"required":             []string{"session_id"},
		"additionalProperties": false,
	}
}

func canvasPDFInputSchema() map[string]any {
	return objectSchema(map[string]any{
		"session_id": stringSchema("Browser session ID."),
	}, "session_id")
}

// ---------------------------------------------------------------------------
// Output schemas
// ---------------------------------------------------------------------------

func canvasPresentOutputSchema() map[string]any {
	return objectSchema(map[string]any{
		"ok":    booleanSchema("Whether the content was rendered successfully."),
		"title": stringSchema("Title of the rendered page."),
	}, "ok")
}

func canvasEvalOutputSchema() map[string]any {
	return objectSchema(map[string]any{
		"ok":     booleanSchema("Whether the expression was evaluated successfully."),
		"result": map[string]any{"description": "Result of the JavaScript evaluation."},
	}, "ok")
}

func canvasSnapshotOutputSchema() map[string]any {
	return objectSchema(map[string]any{
		"ok":     booleanSchema("Whether the screenshot was captured successfully."),
		"base64": stringSchema("Base64-encoded screenshot image."),
	}, "ok")
}

func canvasHideOutputSchema() map[string]any {
	return objectSchema(map[string]any{
		"ok": booleanSchema("Whether the canvas was hidden successfully."),
	}, "ok")
}

func canvasStyleOutputSchema() map[string]any {
	return objectSchema(map[string]any{
		"ok":       booleanSchema("Whether the styles were applied successfully."),
		"selector": stringSchema("CSS selector that was targeted."),
	}, "ok")
}

func canvasDOMOutputSchema() map[string]any {
	return objectSchema(map[string]any{
		"ok":        booleanSchema("Whether the DOM operation succeeded."),
		"operation": stringSchema("DOM operation that was performed."),
	}, "ok")
}

func canvasClickOutputSchema() map[string]any {
	return objectSchema(map[string]any{
		"ok":       booleanSchema("Whether the click was simulated successfully."),
		"selector": stringSchema("CSS selector of the clicked element."),
	}, "ok")
}

func canvasTypeOutputSchema() map[string]any {
	return objectSchema(map[string]any{
		"ok":       booleanSchema("Whether the text was typed successfully."),
		"selector": stringSchema("CSS selector of the input element."),
	}, "ok")
}

func canvasScrollOutputSchema() map[string]any {
	return objectSchema(map[string]any{
		"ok": booleanSchema("Whether the scroll was performed successfully."),
	}, "ok")
}

func canvasConsoleOutputSchema() map[string]any {
	return objectSchema(map[string]any{
		"ok":        booleanSchema("Whether the console operation succeeded."),
		"operation": stringSchema("Console operation that was performed."),
		"messages":  map[string]any{"description": "Console messages (for read operation)."},
	}, "ok")
}

func canvasWaitOutputSchema() map[string]any {
	return objectSchema(map[string]any{
		"ok":      booleanSchema("Whether the condition was met before timeout."),
		"matched": booleanSchema("Whether the selector or expression matched."),
	}, "ok")
}

func canvasPDFOutputSchema() map[string]any {
	return objectSchema(map[string]any{
		"ok":           booleanSchema("Whether the PDF was exported successfully."),
		"artifact_ref": stringSchema("Reference to the exported PDF artifact."),
		"artifact_uri": stringSchema("Normalized artifact URI for the exported PDF."),
	}, "ok")
}

// ---------------------------------------------------------------------------
// Handlers — original tools
// ---------------------------------------------------------------------------

func handleCanvasPresent(ctx context.Context, b *Builtins, call agent.ToolCall) (contextengine.ToolResult, error) {
	client, err := requireBrowserClient(b)
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("canvas.present: %w", err)
	}

	sessionID, err := requiredString(call.Input, "session_id")
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("canvas.present: %w", err)
	}

	// Template mode: render from template + params.
	templateName, _ := stringFrom(call.Input["template"])
	if templateName != "" {
		templateParams, _ := call.Input["params"].(map[string]any)
		if templateParams == nil {
			templateParams = make(map[string]any)
		}
		rendered, renderErr := renderCanvasTemplate(templateName, templateParams)
		if renderErr != nil {
			return contextengine.ToolResult{}, fmt.Errorf("canvas.present: %w", renderErr)
		}
		dataURL := "data:text/html;charset=utf-8," + url.QueryEscape(rendered)
		_, err = doBrowserAction(ctx, client, browsertypes.ActionNavigate, sessionID, map[string]any{
			"url": dataURL,
		})
		if err != nil {
			return contextengine.ToolResult{}, fmt.Errorf("canvas.present: %w", err)
		}
		return b.jsonResult(call, map[string]any{
			"ok":       true,
			"title":    templateName,
			"template": templateName,
		})
	}

	// Raw HTML mode.
	html, err := requiredString(call.Input, "html")
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("canvas.present: %w", err)
	}
	title, _ := stringFrom(call.Input["title"])

	dataURL := "data:text/html;charset=utf-8," + url.QueryEscape(html)

	_, err = doBrowserAction(ctx, client, browsertypes.ActionNavigate, sessionID, map[string]any{
		"url": dataURL,
	})
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("canvas.present: %w", err)
	}

	return b.jsonResult(call, map[string]any{
		"ok":    true,
		"title": title,
	})
}

func handleCanvasEval(ctx context.Context, b *Builtins, call agent.ToolCall) (contextengine.ToolResult, error) {
	client, err := requireBrowserClient(b)
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("canvas.eval: %w", err)
	}

	sessionID, err := requiredString(call.Input, "session_id")
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("canvas.eval: %w", err)
	}
	expression, err := requiredString(call.Input, "expression")
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("canvas.eval: %w", err)
	}

	resp, err := doBrowserAction(ctx, client, browsertypes.ActionEval, sessionID, map[string]any{
		"expression": expression,
	})
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("canvas.eval: %w", err)
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

func handleCanvasSnapshot(ctx context.Context, b *Builtins, call agent.ToolCall) (contextengine.ToolResult, error) {
	client, err := requireBrowserClient(b)
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("canvas.snapshot: %w", err)
	}

	sessionID, err := requiredString(call.Input, "session_id")
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("canvas.snapshot: %w", err)
	}
	selector, _ := stringFrom(call.Input["selector"])

	params := make(map[string]any)
	if selector != "" {
		params["selector"] = selector
	}

	resp, err := doBrowserAction(ctx, client, browsertypes.ActionScreenshot, sessionID, params)
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("canvas.snapshot: %w", err)
	}

	base64Data := ""
	if resp.Data != nil {
		if b64, ok := resp.Data["base64"].(string); ok {
			base64Data = b64
		}
	}
	if base64Data == "" && resp.ArtifactRef != "" {
		base64Data = resp.ArtifactRef
	}

	return b.jsonResult(call, map[string]any{
		"ok":     true,
		"base64": base64Data,
	})
}

func handleCanvasHide(ctx context.Context, b *Builtins, call agent.ToolCall) (contextengine.ToolResult, error) {
	client, err := requireBrowserClient(b)
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("canvas.hide: %w", err)
	}

	sessionID, err := requiredString(call.Input, "session_id")
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("canvas.hide: %w", err)
	}

	_, err = doBrowserAction(ctx, client, browsertypes.ActionNavigate, sessionID, map[string]any{
		"url": canvasBlankURL,
	})
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("canvas.hide: %w", err)
	}

	return b.jsonResult(call, map[string]any{
		"ok": true,
	})
}

// ---------------------------------------------------------------------------
// Handlers — new tools
// ---------------------------------------------------------------------------

func handleCanvasStyle(ctx context.Context, b *Builtins, call agent.ToolCall) (contextengine.ToolResult, error) {
	client, err := requireBrowserClient(b)
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("canvas.style: %w", err)
	}

	sessionID, err := requiredString(call.Input, "session_id")
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("canvas.style: %w", err)
	}
	selector, err := requiredString(call.Input, "selector")
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("canvas.style: %w", err)
	}
	cssMap, ok := call.Input["css"].(map[string]any)
	if !ok || len(cssMap) == 0 {
		return contextengine.ToolResult{}, fmt.Errorf("canvas.style: css is required and must be an object")
	}

	// Build JS that applies each CSS property.
	js := canvasStyleJS(selector, cssMap)

	_, err = doBrowserAction(ctx, client, browsertypes.ActionEval, sessionID, map[string]any{
		"expression": js,
	})
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("canvas.style: %w", err)
	}

	return b.jsonResult(call, map[string]any{
		"ok":       true,
		"selector": selector,
	})
}

// canvasStyleJS builds a JavaScript snippet that applies CSS properties to elements
// matching the given selector.
func canvasStyleJS(selector string, cssMap map[string]any) string {
	var assignments strings.Builder
	for prop, val := range cssMap {
		valStr, _ := val.(string)
		assignments.WriteString(fmt.Sprintf("el.style[%s]=%s;",
			jsStringLiteral(prop), jsStringLiteral(valStr)))
	}
	return fmt.Sprintf(
		`document.querySelectorAll(%s).forEach(function(el){%s})`,
		jsStringLiteral(selector), assignments.String())
}

func handleCanvasDOM(ctx context.Context, b *Builtins, call agent.ToolCall) (contextengine.ToolResult, error) {
	client, err := requireBrowserClient(b)
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("canvas.dom: %w", err)
	}

	sessionID, err := requiredString(call.Input, "session_id")
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("canvas.dom: %w", err)
	}
	operation, err := requiredString(call.Input, "operation")
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("canvas.dom: %w", err)
	}
	selector, err := requiredString(call.Input, "selector")
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("canvas.dom: %w", err)
	}

	js, buildErr := canvasDOMJS(operation, selector, call.Input)
	if buildErr != nil {
		return contextengine.ToolResult{}, fmt.Errorf("canvas.dom: %w", buildErr)
	}

	_, err = doBrowserAction(ctx, client, browsertypes.ActionEval, sessionID, map[string]any{
		"expression": js,
	})
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("canvas.dom: %w", err)
	}

	return b.jsonResult(call, map[string]any{
		"ok":        true,
		"operation": operation,
	})
}

// canvasDOMJS builds a JavaScript snippet for the requested DOM operation.
func canvasDOMJS(operation, selector string, input map[string]any) (string, error) {
	value, _ := stringFrom(input["value"])

	switch operation {
	case "set_html":
		return fmt.Sprintf(
			`document.querySelector(%s).innerHTML=%s`,
			jsStringLiteral(selector), jsStringLiteral(value)), nil

	case "set_attr":
		attrName, err := requiredString(input, "attr_name")
		if err != nil {
			return "", fmt.Errorf("set_attr requires attr_name")
		}
		return fmt.Sprintf(
			`document.querySelector(%s).setAttribute(%s,%s)`,
			jsStringLiteral(selector), jsStringLiteral(attrName), jsStringLiteral(value)), nil

	case "create":
		tag, err := requiredString(input, "tag")
		if err != nil {
			return "", fmt.Errorf("create requires tag")
		}
		parentSelector, _ := stringFrom(input["parent_selector"])
		if parentSelector == "" {
			parentSelector = "body"
		}
		js := fmt.Sprintf(
			`(function(){var el=document.createElement(%s);el.id=%s;document.querySelector(%s).appendChild(el)})()`,
			jsStringLiteral(tag), jsStringLiteral(selector), jsStringLiteral(parentSelector))
		return js, nil

	case "remove":
		return fmt.Sprintf(
			`(function(){var el=document.querySelector(%s);if(el)el.remove()})()`,
			jsStringLiteral(selector)), nil

	default:
		return "", fmt.Errorf("unknown operation %q", operation)
	}
}

func handleCanvasClick(ctx context.Context, b *Builtins, call agent.ToolCall) (contextengine.ToolResult, error) {
	client, err := requireBrowserClient(b)
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("canvas.click: %w", err)
	}

	sessionID, err := requiredString(call.Input, "session_id")
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("canvas.click: %w", err)
	}
	selector, err := requiredString(call.Input, "selector")
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("canvas.click: %w", err)
	}

	_, err = doBrowserAction(ctx, client, browsertypes.ActionClick, sessionID, map[string]any{
		"selector": selector,
	})
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("canvas.click: %w", err)
	}

	return b.jsonResult(call, map[string]any{
		"ok":       true,
		"selector": selector,
	})
}

func handleCanvasType(ctx context.Context, b *Builtins, call agent.ToolCall) (contextengine.ToolResult, error) {
	client, err := requireBrowserClient(b)
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("canvas.type: %w", err)
	}

	sessionID, err := requiredString(call.Input, "session_id")
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("canvas.type: %w", err)
	}
	selector, err := requiredString(call.Input, "selector")
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("canvas.type: %w", err)
	}
	text, err := requiredString(call.Input, "text")
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("canvas.type: %w", err)
	}

	_, err = doBrowserAction(ctx, client, browsertypes.ActionType, sessionID, map[string]any{
		"selector": selector,
		"text":     text,
	})
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("canvas.type: %w", err)
	}

	return b.jsonResult(call, map[string]any{
		"ok":       true,
		"selector": selector,
	})
}

func handleCanvasScroll(ctx context.Context, b *Builtins, call agent.ToolCall) (contextengine.ToolResult, error) {
	client, err := requireBrowserClient(b)
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("canvas.scroll: %w", err)
	}

	sessionID, err := requiredString(call.Input, "session_id")
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("canvas.scroll: %w", err)
	}

	params := make(map[string]any)
	if selector, _ := stringFrom(call.Input["selector"]); selector != "" {
		params["selector"] = selector
	}
	if x, ok := call.Input["x"]; ok {
		params["x"] = x
	}
	if y, ok := call.Input["y"]; ok {
		params["y"] = y
	}

	_, err = doBrowserAction(ctx, client, browsertypes.ActionScroll, sessionID, params)
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("canvas.scroll: %w", err)
	}

	return b.jsonResult(call, map[string]any{
		"ok": true,
	})
}

func handleCanvasConsole(ctx context.Context, b *Builtins, call agent.ToolCall) (contextengine.ToolResult, error) {
	client, err := requireBrowserClient(b)
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("canvas.console: %w", err)
	}

	sessionID, err := requiredString(call.Input, "session_id")
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("canvas.console: %w", err)
	}
	operation, err := requiredString(call.Input, "operation")
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("canvas.console: %w", err)
	}

	switch operation {
	case "start":
		_, err = doBrowserAction(ctx, client, browsertypes.ActionConsoleStart, sessionID, nil)
		if err != nil {
			return contextengine.ToolResult{}, fmt.Errorf("canvas.console: %w", err)
		}
		return b.jsonResult(call, map[string]any{
			"ok":        true,
			"operation": operation,
		})

	case "read":
		resp, readErr := doBrowserAction(ctx, client, browsertypes.ActionConsoleMessages, sessionID, nil)
		if readErr != nil {
			return contextengine.ToolResult{}, fmt.Errorf("canvas.console: %w", readErr)
		}
		var messages any
		if resp.Data != nil {
			messages = resp.Data["messages"]
		}
		return b.jsonResult(call, map[string]any{
			"ok":        true,
			"operation": operation,
			"messages":  messages,
		})

	case "clear":
		_, err = doBrowserAction(ctx, client, browsertypes.ActionEval, sessionID, map[string]any{
			"expression": "console.clear()",
		})
		if err != nil {
			return contextengine.ToolResult{}, fmt.Errorf("canvas.console: %w", err)
		}
		return b.jsonResult(call, map[string]any{
			"ok":        true,
			"operation": operation,
		})

	default:
		return contextengine.ToolResult{}, fmt.Errorf("canvas.console: unknown operation %q", operation)
	}
}

func handleCanvasWait(ctx context.Context, b *Builtins, call agent.ToolCall) (contextengine.ToolResult, error) {
	client, err := requireBrowserClient(b)
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("canvas.wait: %w", err)
	}

	sessionID, err := requiredString(call.Input, "session_id")
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("canvas.wait: %w", err)
	}

	selector, _ := stringFrom(call.Input["selector"])
	expression, _ := stringFrom(call.Input["expression"])
	if selector == "" && expression == "" {
		return contextengine.ToolResult{}, fmt.Errorf("canvas.wait: either selector or expression is required")
	}

	timeoutMS := canvasDefaultWaitTimeoutMS
	if raw, ok := call.Input["timeout_ms"]; ok {
		switch v := raw.(type) {
		case float64:
			timeoutMS = int(v)
		case int:
			timeoutMS = v
		case int64:
			timeoutMS = int(v)
		}
	}

	params := map[string]any{
		"timeout": timeoutMS,
	}
	if selector != "" {
		params["selector"] = selector
	}
	if expression != "" {
		params["expression"] = expression
	}

	resp, err := doBrowserAction(ctx, client, browsertypes.ActionWaitFor, sessionID, params)
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("canvas.wait: %w", err)
	}

	matched := true
	if resp.Data != nil {
		if m, ok := resp.Data["matched"].(bool); ok {
			matched = m
		}
	}

	return b.jsonResult(call, map[string]any{
		"ok":      true,
		"matched": matched,
	})
}

func handleCanvasPDF(ctx context.Context, b *Builtins, call agent.ToolCall) (contextengine.ToolResult, error) {
	client, err := requireBrowserClient(b)
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("canvas.pdf: %w", err)
	}

	sessionID, err := requiredString(call.Input, "session_id")
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("canvas.pdf: %w", err)
	}

	resp, err := doBrowserAction(ctx, client, browsertypes.ActionPDF, sessionID, nil)
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("canvas.pdf: %w", err)
	}

	artifactRef := ""
	if resp.ArtifactRef != "" {
		artifactRef = resp.ArtifactRef
	}

	return b.jsonResult(call, map[string]any{
		"ok":           true,
		"artifact_ref": artifactRef,
		"artifact_uri": artifactRef,
	})
}

// ---------------------------------------------------------------------------
// JS helper
// ---------------------------------------------------------------------------

// jsStringLiteral returns a safely quoted JavaScript string literal.
func jsStringLiteral(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}
