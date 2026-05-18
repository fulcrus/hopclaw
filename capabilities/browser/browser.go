package browser

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	browserclient "github.com/fulcrus/hopclaw/browserapi/client"
	browsertypes "github.com/fulcrus/hopclaw/browserapi/types"
	capprofile "github.com/fulcrus/hopclaw/capability/profile"
	registry "github.com/fulcrus/hopclaw/capability/registry"
	captypes "github.com/fulcrus/hopclaw/capability/types"
	supportmaps "github.com/fulcrus/hopclaw/internal/support/maps"
	"github.com/fulcrus/hopclaw/internal/support/normalize"
)

// Capability exposes a browser host through the shared capability registry.
type Capability struct {
	client   *browserclient.Client
	profiles *capprofile.Registry
	router   *capprofile.Router
	mu       sync.Mutex
	sessions map[string]*captypes.SessionHandle
	states   map[string]capprofile.SessionState
}

type Config struct {
	BaseURL   string
	AuthToken string
	Timeout   time.Duration
	Client    *browserclient.Client
	Profiles  *capprofile.Registry
}

const captureRequestTimeout = 105 * time.Second

var _ registry.SessionCapability = (*Capability)(nil)

func New(cfg Config) *Capability {
	profiles := cfg.Profiles
	if profiles == nil {
		profiles = capprofile.NewDefaultRegistry()
	}
	if cfg.Client != nil {
		return &Capability{
			client:   cfg.Client,
			profiles: profiles,
			router:   capprofile.NewRouter(profiles),
			sessions: make(map[string]*captypes.SessionHandle),
			states:   make(map[string]capprofile.SessionState),
		}
	}
	if strings.TrimSpace(cfg.BaseURL) == "" {
		return &Capability{
			profiles: profiles,
			router:   capprofile.NewRouter(profiles),
			sessions: make(map[string]*captypes.SessionHandle),
			states:   make(map[string]capprofile.SessionState),
		}
	}
	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = 45 * time.Second
	}
	return &Capability{
		client: browserclient.NewWithConfig(browserclient.Config{
			BaseURL:   cfg.BaseURL,
			AuthToken: cfg.AuthToken,
			Timeout:   timeout,
		}),
		profiles: profiles,
		router:   capprofile.NewRouter(profiles),
		sessions: make(map[string]*captypes.SessionHandle),
		states:   make(map[string]capprofile.SessionState),
	}
}

func (c *Capability) Manifest() captypes.Manifest {
	return captypes.Manifest{
		Name:          "browser",
		Kind:          captypes.KindSession,
		SessionScoped: true,
		Operations: []captypes.OperationSpec{
			{
				Name:            "create_session",
				Description:     "Open a browser automation session",
				SideEffectClass: "external_write",
				InputSchema: objectSchema(map[string]any{
					"url":    stringSchema("Optional initial URL to open."),
					"width":  integerSchema("Optional viewport width in pixels."),
					"height": integerSchema("Optional viewport height in pixels."),
				}),
				OutputSchema: objectSchema(map[string]any{
					"session_id": stringSchema("Created browser session id."),
					"capability": stringSchema("Capability name."),
					"created_at": stringSchema("Creation timestamp in RFC3339 format."),
					"metadata":   openObjectSchema("Additional session metadata."),
				}, "session_id"),
			},
			{
				Name:            "close_session",
				Description:     "Close a browser automation session",
				SideEffectClass: "external_write",
				InputSchema: objectSchema(map[string]any{
					"session_id": stringSchema("Browser session id returned by browser.create_session."),
				}, "session_id"),
				OutputSchema: objectSchema(map[string]any{
					"session_id": stringSchema("Closed browser session id."),
					"closed":     booleanSchema("Whether the session was closed."),
				}, "session_id", "closed"),
			},
			{
				Name:            "navigate",
				Description:     "Navigate the active page",
				SideEffectClass: "external_write",
				InputSchema: objectSchema(map[string]any{
					"session_id": stringSchema("Browser session id."),
					"url":        stringSchema("Destination URL."),
				}, "session_id", "url"),
				OutputSchema: objectSchema(map[string]any{
					"url":   stringSchema("Final page URL."),
					"title": stringSchema("Page title."),
				}),
			},
			{
				Name:            "click",
				Description:     "Click an element",
				SideEffectClass: "external_write",
				InputSchema: objectSchema(map[string]any{
					"session_id": stringSchema("Browser session id."),
					"selector":   stringSchema("CSS selector to click."),
				}, "session_id", "selector"),
				OutputSchema: objectSchema(map[string]any{
					"selector": stringSchema("Clicked selector."),
					"clicked":  booleanSchema("Whether the element was clicked."),
				}, "selector", "clicked"),
			},
			{
				Name:            "type",
				Description:     "Type into an input or page",
				SideEffectClass: "external_write",
				InputSchema: objectSchema(map[string]any{
					"session_id": stringSchema("Browser session id."),
					"selector":   stringSchema("CSS selector to target."),
					"text":       stringSchema("Text to enter."),
					"mode":       stringSchema(`Optional input mode: "replace" or "keys".`),
				}, "session_id", "selector", "text"),
				OutputSchema: objectSchema(map[string]any{
					"selector": stringSchema("Target selector."),
					"text":     stringSchema("Text that was entered."),
				}, "selector", "text"),
			},
			{
				Name:            "wait_for",
				Description:     "Wait for an element or event",
				SideEffectClass: "read",
				InputSchema: objectSchema(map[string]any{
					"session_id": stringSchema("Browser session id."),
					"selector":   stringSchema("CSS selector to wait for."),
					"timeout_ms": integerSchema("Optional wait timeout in milliseconds."),
				}, "session_id", "selector"),
				OutputSchema: objectSchema(map[string]any{
					"selector": stringSchema("Selector that became visible."),
					"state":    stringSchema("Observed state."),
				}, "selector", "state"),
			},
			{
				Name:            "snapshot",
				Description:     "Capture a DOM snapshot",
				SideEffectClass: "read",
				Idempotent:      true,
				InputSchema: objectSchema(map[string]any{
					"session_id": stringSchema("Browser session id."),
				}, "session_id"),
				OutputSchema: objectSchema(map[string]any{
					"url":          stringSchema("Current page URL."),
					"title":        stringSchema("Current page title."),
					"content_type": stringSchema("Snapshot content type."),
					"html":         stringSchema("HTML payload when no artifact store is attached."),
				}),
			},
			{
				Name:            "screenshot",
				Description:     "Capture a page screenshot",
				SideEffectClass: "read",
				Idempotent:      true,
				InputSchema: objectSchema(map[string]any{
					"session_id": stringSchema("Browser session id."),
					"full_page":  booleanSchema("Whether to capture the full page."),
					"quality":    integerSchema("Optional JPEG quality from 1 to 100 for full-page captures."),
				}, "session_id"),
				OutputSchema: objectSchema(map[string]any{
					"mime_type": stringSchema("Screenshot MIME type."),
					"full_page": booleanSchema("Whether the full page was captured."),
				}),
			},
			{
				Name:            "screenshot_labeled",
				Description:     "Capture a screenshot with interactive elements highlighted and labeled.",
				SideEffectClass: "read",
				Idempotent:      true,
				InputSchema: objectSchema(map[string]any{
					"session_id": stringSchema("Browser session id."),
				}, "session_id"),
				OutputSchema: objectSchema(map[string]any{
					"ok":            booleanSchema("Whether the labeled screenshot was captured."),
					"artifact_ref":  stringSchema("Path or reference to the saved screenshot image."),
					"elements":      openObjectSchema("Map of element labels (e1, e2, ...) to element metadata."),
					"element_count": integerSchema("Number of labeled interactive elements found."),
				}, "ok"),
			},
			{
				Name:            "emulate_device",
				Description:     "Set device emulation (viewport, scale, mobile mode, user agent)",
				SideEffectClass: "local_write",
				InputSchema: objectSchema(map[string]any{
					"session_id": stringSchema("Browser session id."),
					"device":     stringSchema("Device preset name."),
					"width":      integerSchema("Viewport width."),
					"height":     integerSchema("Viewport height."),
					"scale":      numberSchema("Device scale factor."),
					"mobile":     booleanSchema("Mobile emulation."),
					"user_agent": stringSchema("User-Agent override."),
				}, "session_id"),
				OutputSchema: objectSchema(map[string]any{
					"ok": booleanSchema("Whether emulation was applied."),
				}, "ok"),
			},
			{
				Name:            "emulate_vision",
				Description:     "Simulate vision deficiencies for accessibility testing",
				SideEffectClass: "local_write",
				InputSchema: objectSchema(map[string]any{
					"session_id": stringSchema("Browser session id."),
					"type":       stringSchema("Vision deficiency: none, achromatopsia, deuteranopia, protanopia, tritanopia, blurredVision."),
				}, "session_id", "type"),
				OutputSchema: objectSchema(map[string]any{
					"ok":   booleanSchema("Whether emulation was applied."),
					"type": stringSchema("Applied vision deficiency."),
				}, "ok"),
			},
			{
				Name:            "snapshot_aria",
				Description:     "Get ARIA accessibility tree with ref IDs for interactive elements",
				SideEffectClass: "read",
				Idempotent:      true,
				InputSchema: objectSchema(map[string]any{
					"session_id": stringSchema("Browser session id."),
				}, "session_id"),
				OutputSchema: objectSchema(map[string]any{
					"text":          stringSchema("Text representation of the ARIA tree."),
					"element_count": integerSchema("Number of interactive elements with refs."),
				}),
			},
			{
				Name:            "click_aria",
				Description:     "Click an element by its ARIA ref ID",
				SideEffectClass: "external_write",
				InputSchema: objectSchema(map[string]any{
					"session_id": stringSchema("Browser session id."),
					"ref":        stringSchema("Element ref ID from snapshot_aria (e.g. 'e1')."),
				}, "session_id", "ref"),
				OutputSchema: objectSchema(map[string]any{
					"ok": booleanSchema("Whether the click succeeded."),
				}, "ok"),
			},
			{
				Name:            "type_aria",
				Description:     "Type text into an element by its ARIA ref ID",
				SideEffectClass: "external_write",
				InputSchema: objectSchema(map[string]any{
					"session_id": stringSchema("Browser session id."),
					"ref":        stringSchema("Element ref ID from snapshot_aria (e.g. 'e1')."),
					"text":       stringSchema("Text to type."),
					"clear":      booleanSchema("Clear the field before typing."),
				}, "session_id", "ref", "text"),
				OutputSchema: objectSchema(map[string]any{
					"ok": booleanSchema("Whether the type operation succeeded."),
				}, "ok"),
			},
			{
				Name:            "set_geolocation",
				Description:     "Set or clear geolocation override",
				SideEffectClass: "local_write",
				InputSchema: objectSchema(map[string]any{
					"session_id": stringSchema("Browser session id."),
					"latitude":   numberSchema("Latitude in degrees."),
					"longitude":  numberSchema("Longitude in degrees."),
					"accuracy":   numberSchema("Accuracy in meters."),
					"clear":      booleanSchema("Clear geolocation override."),
				}, "session_id"),
				OutputSchema: objectSchema(map[string]any{
					"ok": booleanSchema("Whether the geolocation was set."),
				}, "ok"),
			},
			{
				Name:            "set_timezone",
				Description:     "Override browser timezone",
				SideEffectClass: "local_write",
				InputSchema: objectSchema(map[string]any{
					"session_id":  stringSchema("Browser session id."),
					"timezone_id": stringSchema("IANA timezone ID."),
				}, "session_id", "timezone_id"),
				OutputSchema: objectSchema(map[string]any{
					"ok": booleanSchema("Whether the timezone was set."),
				}, "ok"),
			},
			{
				Name:            "set_locale",
				Description:     "Override browser locale",
				SideEffectClass: "local_write",
				InputSchema: objectSchema(map[string]any{
					"session_id": stringSchema("Browser session id."),
					"locale":     stringSchema("BCP 47 locale."),
				}, "session_id", "locale"),
				OutputSchema: objectSchema(map[string]any{
					"ok": booleanSchema("Whether the locale was set."),
				}, "ok"),
			},
			{
				Name:            "set_color_scheme",
				Description:     "Set preferred color scheme (dark/light)",
				SideEffectClass: "local_write",
				InputSchema: objectSchema(map[string]any{
					"session_id": stringSchema("Browser session id."),
					"scheme":     stringSchema("Color scheme: dark, light, no-preference."),
				}, "session_id", "scheme"),
				OutputSchema: objectSchema(map[string]any{
					"ok": booleanSchema("Whether the color scheme was set."),
				}, "ok"),
			},
			{
				Name:            "set_offline",
				Description:     "Enable or disable offline network emulation",
				SideEffectClass: "local_write",
				InputSchema: objectSchema(map[string]any{
					"session_id": stringSchema("Browser session id."),
					"offline":    booleanSchema("Whether to go offline."),
				}, "session_id"),
				OutputSchema: objectSchema(map[string]any{
					"ok": booleanSchema("Whether offline mode was set."),
				}, "ok"),
			},
			{
				Name:            "set_headers",
				Description:     "Set extra HTTP headers for all requests",
				SideEffectClass: "local_write",
				InputSchema: objectSchema(map[string]any{
					"session_id": stringSchema("Browser session id."),
					"headers":    openObjectSchema("Map of header names to values."),
				}, "session_id", "headers"),
				OutputSchema: objectSchema(map[string]any{
					"ok":          booleanSchema("Whether the headers were set."),
					"headers_set": integerSchema("Number of headers applied."),
				}, "ok"),
			},
			{
				Name:            "set_credentials",
				Description:     "Set HTTP Basic Auth credentials for automatic authentication",
				SideEffectClass: "local_write",
				InputSchema: objectSchema(map[string]any{
					"session_id": stringSchema("Browser session id."),
					"username":   stringSchema("HTTP Basic Auth username."),
					"password":   stringSchema("HTTP Basic Auth password."),
					"clear":      booleanSchema("Clear credentials and disable auth interception."),
				}, "session_id"),
				OutputSchema: objectSchema(map[string]any{
					"ok": booleanSchema("Whether credentials were set or cleared."),
				}, "ok"),
			},
			{
				Name:            "list_tabs",
				Description:     "List browser tabs",
				SideEffectClass: "read",
				Idempotent:      true,
				InputSchema: objectSchema(map[string]any{
					"session_id": stringSchema("Browser session id."),
				}, "session_id"),
				OutputSchema: objectSchema(map[string]any{
					"tabs": objectArraySchema(map[string]any{
						"id":    stringSchema("Tab id."),
						"url":   stringSchema("Tab URL."),
						"title": stringSchema("Tab title."),
					}, "Open browser tabs."),
				}, "tabs"),
			},
		},
		ArtifactKinds:  []string{"browser.screenshot", "browser.snapshot"},
		Events:         []string{"browser.session.opened", "browser.session.closed", "browser.tab.updated"},
		ApprovalPolicy: "policy",
	}
}

func (c *Capability) Health(ctx context.Context) captypes.Health {
	if c.client == nil {
		return captypes.Health{
			Status:  captypes.StatusUnavailable,
			Message: "browser host is not configured",
		}
	}
	if err := c.client.Health(ctx); err != nil {
		return captypes.Health{
			Status:  captypes.StatusUnavailable,
			Message: err.Error(),
		}
	}
	return captypes.Health{
		Status:  captypes.StatusReady,
		Message: "browser host reachable",
	}
}

func (c *Capability) Invoke(ctx context.Context, req captypes.InvokeRequest) (*captypes.InvokeResult, error) {
	if c.client == nil {
		return nil, fmt.Errorf("browser host is not configured")
	}
	routedReq, plannedTrace := c.prepareRoutedRequest(req)
	browserReq := browsertypes.Request{
		Action:    routedReq.Operation,
		SessionID: routedReq.SessionID,
		Params:    routedReq.Params,
	}
	var (
		resp *browsertypes.Response
		err  error
	)
	switch strings.TrimSpace(routedReq.Operation) {
	case browsertypes.ActionSnapshot, browsertypes.ActionSnapshotAria, browsertypes.ActionScreenshot, browsertypes.ActionScreenshotLabeled:
		resp, err = c.client.DoWithTimeout(ctx, browserReq, captureRequestTimeout)
	default:
		resp, err = c.client.Do(ctx, browserReq)
	}
	if err != nil {
		return nil, err
	}
	result := &captypes.InvokeResult{
		OK:          resp.OK,
		Data:        supportmaps.Clone(resp.Data),
		ArtifactRef: resp.ArtifactRef,
		Error:       resp.Error,
	}
	chosenTransport := inferBrowserTransport(routedReq.Operation, result.Data)
	trace := c.finalizeRouteTrace(plannedTrace, chosenTransport)
	c.decorateInvokeResult(result, trace)
	sessionID := normalize.FirstNonEmpty(routedReq.SessionID, resp.SessionID, normalize.String(result.Data["session_id"]))
	c.rememberSessionState(sessionID, routedReq, result, trace)
	if !resp.OK && strings.TrimSpace(resp.Error) != "" {
		return result, fmt.Errorf("browser host: %s", resp.Error)
	}
	return result, nil
}

func (c *Capability) OpenSession(ctx context.Context, params map[string]any) (*captypes.SessionHandle, error) {
	result, err := c.Invoke(ctx, captypes.InvokeRequest{
		Operation: "create_session",
		Params:    params,
	})
	if err != nil {
		return nil, err
	}
	sessionID := normalize.String(result.Data["session_id"])
	if sessionID == "" {
		sessionID = normalize.String(result.Data["id"])
	}
	if sessionID == "" {
		return nil, fmt.Errorf("browser host did not return a session id")
	}
	handle := &captypes.SessionHandle{
		ID:         sessionID,
		Capability: "browser",
		CreatedAt:  time.Now().UTC(),
		Metadata:   supportmaps.Clone(result.Data),
	}
	c.mu.Lock()
	c.sessions[sessionID] = handle
	c.mu.Unlock()
	return handle, nil
}

func (c *Capability) CloseSession(ctx context.Context, sessionID string) error {
	_, err := c.Invoke(ctx, captypes.InvokeRequest{
		Operation: "close_session",
		SessionID: sessionID,
	})
	if err != nil {
		return err
	}
	c.mu.Lock()
	delete(c.sessions, sessionID)
	delete(c.states, sessionID)
	c.mu.Unlock()
	return nil
}

// ListSessions returns a snapshot of all tracked browser sessions.
func (c *Capability) ListSessions() []*captypes.SessionHandle {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]*captypes.SessionHandle, 0, len(c.sessions))
	for _, h := range c.sessions {
		cp := *h
		cp.Metadata = supportmaps.Clone(h.Metadata)
		out = append(out, &cp)
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].CreatedAt.Before(out[j].CreatedAt)
	})
	return out
}

func stringSchema(description string) map[string]any {
	schema := map[string]any{"type": "string"}
	if description != "" {
		schema["description"] = description
	}
	return schema
}

func integerSchema(description string) map[string]any {
	schema := map[string]any{"type": "integer"}
	if description != "" {
		schema["description"] = description
	}
	return schema
}

func booleanSchema(description string) map[string]any {
	schema := map[string]any{"type": "boolean"}
	if description != "" {
		schema["description"] = description
	}
	return schema
}

func objectSchema(properties map[string]any, required ...string) map[string]any {
	schema := map[string]any{
		"type":                 "object",
		"properties":           properties,
		"additionalProperties": false,
	}
	if len(required) > 0 {
		schema["required"] = required
	}
	return schema
}

func openObjectSchema(description string) map[string]any {
	schema := map[string]any{
		"type":                 "object",
		"additionalProperties": true,
	}
	if description != "" {
		schema["description"] = description
	}
	return schema
}

func numberSchema(description string) map[string]any {
	schema := map[string]any{"type": "number"}
	if description != "" {
		schema["description"] = description
	}
	return schema
}

func objectArraySchema(properties map[string]any, description string) map[string]any {
	schema := map[string]any{
		"type": "array",
		"items": map[string]any{
			"type":                 "object",
			"properties":           properties,
			"additionalProperties": false,
		},
	}
	if description != "" {
		schema["description"] = description
	}
	return schema
}
