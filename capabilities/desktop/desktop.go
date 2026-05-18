package desktop

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	capprofile "github.com/fulcrus/hopclaw/capability/profile"
	registry "github.com/fulcrus/hopclaw/capability/registry"
	captypes "github.com/fulcrus/hopclaw/capability/types"
	desktopclient "github.com/fulcrus/hopclaw/desktopapi/client"
	desktoptypes "github.com/fulcrus/hopclaw/desktopapi/types"
	supportmaps "github.com/fulcrus/hopclaw/internal/support/maps"
	"github.com/fulcrus/hopclaw/internal/support/normalize"
)

type Capability struct {
	client   *desktopclient.Client
	profiles *capprofile.Registry
	router   *capprofile.Router
	mu       sync.Mutex
	sessions map[string]*captypes.SessionHandle
	states   map[string]capprofile.SessionState
}

type Config struct {
	BaseURL   string
	AuthToken string
	Client    *desktopclient.Client
	Profiles  *capprofile.Registry
}

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
	return &Capability{
		client: desktopclient.NewWithConfig(desktopclient.Config{
			BaseURL:   cfg.BaseURL,
			AuthToken: cfg.AuthToken,
			Timeout:   5 * time.Second,
		}),
		profiles: profiles,
		router:   capprofile.NewRouter(profiles),
		sessions: make(map[string]*captypes.SessionHandle),
		states:   make(map[string]capprofile.SessionState),
	}
}

func (c *Capability) Manifest() captypes.Manifest {
	return captypes.Manifest{
		Name:          "desktop",
		Kind:          captypes.KindSession,
		SessionScoped: true,
		Operations: []captypes.OperationSpec{
			{
				Name:            "create_session",
				Description:     "Open a desktop automation session",
				SideEffectClass: "external_write",
				InputSchema: objectSchema(map[string]any{
					"workspace": stringSchema("Optional logical workspace or scenario name."),
				}),
				OutputSchema: objectSchema(map[string]any{
					"session_id": stringSchema("Created desktop session id."),
					"capability": stringSchema("Capability name."),
					"created_at": stringSchema("Creation timestamp in RFC3339 format."),
					"metadata":   openObjectSchema("Additional session metadata."),
				}, "session_id"),
			},
			{
				Name:            "close_session",
				Description:     "Close a desktop automation session",
				SideEffectClass: "external_write",
				InputSchema: objectSchema(map[string]any{
					"session_id": stringSchema("Desktop session id returned by desktop.create_session."),
				}, "session_id"),
				OutputSchema: objectSchema(map[string]any{
					"session_id": stringSchema("Closed desktop session id."),
					"closed":     booleanSchema("Whether the session was closed."),
				}, "session_id", "closed"),
			},
			{
				Name:            "open_app",
				Description:     "Open a desktop application by name or bundle id",
				SideEffectClass: "external_write",
				InputSchema: objectSchema(map[string]any{
					"session_id": stringSchema("Desktop session id."),
					"app":        stringSchema("Application name, such as Safari or WeChat."),
					"bundle_id":  stringSchema("Optional bundle identifier such as com.apple.Safari."),
					"wait_until": stringSchema(`Optional readiness target: "none", "running", "window", "focused", or "interactive". Defaults to "running".`),
					"timeout_ms": integerSchema("Optional maximum time to wait for the requested readiness state."),
				}, "session_id"),
				OutputSchema: objectSchema(map[string]any{
					"app":          stringSchema("Resolved application name."),
					"bundle_id":    stringSchema("Resolved application bundle identifier."),
					"opened":       booleanSchema("Whether the application launch command succeeded."),
					"wait_until":   stringSchema("Requested readiness target."),
					"ready_state":  stringSchema("Observed readiness state after waiting."),
					"ready":        booleanSchema("Whether the requested readiness target was reached."),
					"frontmost":    booleanSchema("Whether the application is frontmost."),
					"interactive":  booleanSchema("Whether the application has a visible window and is frontmost."),
					"window_count": integerSchema("Observed number of visible windows."),
					"waited_ms":    integerSchema("Time spent waiting for readiness."),
				}, "opened", "wait_until", "ready_state", "ready"),
			},
			{
				Name:            "focus_app",
				Description:     "Bring an application to the foreground",
				SideEffectClass: "external_write",
				InputSchema: objectSchema(map[string]any{
					"session_id": stringSchema("Desktop session id."),
					"app":        stringSchema("Application name."),
					"bundle_id":  stringSchema("Optional bundle identifier."),
					"wait_until": stringSchema(`Optional readiness target: "none", "running", "window", "focused", or "interactive". Defaults to "focused".`),
					"timeout_ms": integerSchema("Optional maximum time to wait for the requested readiness state."),
				}, "session_id"),
				OutputSchema: objectSchema(map[string]any{
					"app":          stringSchema("Resolved application name."),
					"bundle_id":    stringSchema("Resolved application bundle identifier."),
					"focused":      booleanSchema("Whether the application is frontmost."),
					"wait_until":   stringSchema("Requested readiness target."),
					"ready_state":  stringSchema("Observed readiness state after waiting."),
					"ready":        booleanSchema("Whether the requested readiness target was reached."),
					"frontmost":    booleanSchema("Whether the application is frontmost."),
					"interactive":  booleanSchema("Whether the application has a visible window and is frontmost."),
					"window_count": integerSchema("Observed number of visible windows."),
					"waited_ms":    integerSchema("Time spent waiting for readiness."),
				}, "focused", "wait_until", "ready_state", "ready"),
			},
			{
				Name:            "focus_window",
				Description:     "Focus a specific application window by title or index",
				SideEffectClass: "external_write",
				InputSchema: objectSchema(map[string]any{
					"session_id":     stringSchema("Desktop session id."),
					"app":            stringSchema("Application name."),
					"bundle_id":      stringSchema("Optional bundle identifier."),
					"title_contains": stringSchema("Optional window title substring."),
					"window_index": map[string]any{
						"type":        "integer",
						"description": "Optional 1-based window index when no title match is provided.",
					},
					"wait_until": stringSchema(`Optional readiness target: "window", "focused", or "interactive". Defaults to "interactive".`),
					"timeout_ms": integerSchema("Optional maximum time to wait for the requested readiness state."),
				}, "session_id"),
				OutputSchema: objectSchema(map[string]any{
					"app":          stringSchema("Focused application name."),
					"bundle_id":    stringSchema("Resolved application bundle identifier."),
					"title":        stringSchema("Focused window title."),
					"window_index": map[string]any{"type": "integer"},
					"focused":      booleanSchema("Whether the window was focused."),
					"wait_until":   stringSchema("Requested readiness target."),
					"ready_state":  stringSchema("Observed readiness state after waiting."),
					"ready":        booleanSchema("Whether the requested readiness target was reached."),
					"frontmost":    booleanSchema("Whether the application is frontmost."),
					"interactive":  booleanSchema("Whether the application has a visible window and is frontmost."),
					"window_count": integerSchema("Observed number of visible windows."),
					"waited_ms":    integerSchema("Time spent waiting for readiness."),
				}, "focused", "wait_until", "ready_state", "ready"),
			},
			{
				Name:            "list_apps",
				Description:     "List currently running desktop applications",
				SideEffectClass: "read",
				Idempotent:      true,
				SessionOptional: true,
				InputSchema: objectSchema(map[string]any{
					"include_windows": booleanSchema("Whether to include per-app window summaries."),
				}),
				OutputSchema: objectSchema(map[string]any{
					"frontmost_app": openObjectSchema("The current frontmost application."),
					"apps": map[string]any{
						"type":        "array",
						"description": "Running application summaries.",
						"items":       openObjectSchema("Application summary."),
					},
				}),
			},
			{
				Name:            "describe_host",
				Description:     "Describe desktop host capabilities, permissions, and environment traits",
				SideEffectClass: "read",
				Idempotent:      true,
				SessionOptional: true,
				InputSchema:     objectSchema(map[string]any{}),
				OutputSchema: objectSchema(map[string]any{
					"profile": openObjectSchema("Desktop host capability profile."),
				}, "profile"),
			},
			{
				Name:            "list_windows",
				Description:     "List windows for the frontmost app or a target application",
				SideEffectClass: "read",
				Idempotent:      true,
				InputSchema: objectSchema(map[string]any{
					"session_id": stringSchema("Desktop session id."),
					"app":        stringSchema("Optional application name."),
					"bundle_id":  stringSchema("Optional bundle identifier."),
				}, "session_id"),
				OutputSchema: objectSchema(map[string]any{
					"app": openObjectSchema("Resolved application summary."),
					"windows": map[string]any{
						"type":        "array",
						"description": "Visible windows for the selected application.",
						"items":       openObjectSchema("Window summary."),
					},
				}),
			},
			{
				Name:            "list_commands",
				Description:     "Enumerate menu-backed command graph entries for a target application",
				SideEffectClass: "read",
				Idempotent:      true,
				InputSchema: objectSchema(map[string]any{
					"session_id":       stringSchema("Desktop session id."),
					"app":              stringSchema("Optional target application name."),
					"bundle_id":        stringSchema("Optional target application bundle identifier."),
					"max_depth":        integerSchema("Maximum submenu traversal depth."),
					"max_results":      integerSchema("Maximum number of commands to return."),
					"include_disabled": booleanSchema("Whether to include disabled commands."),
					"include_system":   booleanSchema("Whether to include system-level menu commands such as Apple menu entries. Defaults to false."),
					"include_unsafe":   booleanSchema("Whether to include unsafe or destructive commands in the result. Defaults to false."),
				}, "session_id"),
				OutputSchema: objectSchema(map[string]any{
					"app": openObjectSchema("Resolved application summary."),
					"commands": map[string]any{
						"type":        "array",
						"description": "Discovered command graph entries.",
						"items":       openObjectSchema("Command entry."),
					},
					"count": integerSchema("Number of commands returned."),
				}, "commands", "count"),
			},
			{
				Name:            "invoke_command",
				Description:     "Invoke a menu-backed application command by command id, menu path, or title",
				SideEffectClass: "external_write",
				InputSchema: objectSchema(map[string]any{
					"session_id":   stringSchema("Desktop session id."),
					"app":          stringSchema("Optional target application name."),
					"bundle_id":    stringSchema("Optional target application bundle identifier."),
					"command_id":   stringSchema("Command id returned by desktop.list_commands."),
					"menu_path":    stringSchema(`Menu path such as "File > Open" when command_id is not provided.`),
					"title":        stringSchema("Exact leaf command title fallback when command_id and menu_path are not provided."),
					"transport":    stringSchema(`Execution transport: "menu", "hotkey", or "auto". Defaults to "menu".`),
					"allow_unsafe": booleanSchema("Explicitly allow unsafe or destructive commands. Defaults to false."),
				}, "session_id"),
				OutputSchema: objectSchema(map[string]any{
					"invoked":       booleanSchema("Whether the command transport was sent."),
					"transport":     stringSchema("Resolved command transport."),
					"command":       openObjectSchema("Resolved command entry."),
					"action_status": stringSchema("Structured action result status."),
				}, "invoked", "transport"),
			},
			{
				Name:            "list_driver_actions",
				Description:     "List semantic app-driver actions available for a target desktop application",
				SideEffectClass: "read",
				Idempotent:      true,
				InputSchema: objectSchema(map[string]any{
					"session_id": stringSchema("Desktop session id."),
					"app":        stringSchema("Optional target application name. Defaults to the frontmost app."),
					"bundle_id":  stringSchema("Optional target application bundle identifier."),
					"driver_id":  stringSchema("Optional driver id when resolving the target from the running app inventory."),
				}, "session_id"),
				OutputSchema: objectSchema(map[string]any{
					"app":               openObjectSchema("Resolved target application summary."),
					"driver_id":         stringSchema("Resolved app driver id."),
					"app_family":        stringSchema("Driver-declared application family."),
					"support_tier":      stringSchema("Driver support tier."),
					"semantic_richness": stringSchema("Driver semantic richness tier."),
					"view_model":        stringSchema("Driver view model classification."),
					"actions": map[string]any{
						"type":        "array",
						"description": "Available semantic actions for the resolved driver.",
						"items":       openObjectSchema("Driver semantic action."),
					},
					"count": integerSchema("Number of semantic actions exposed."),
				}, "driver_id", "actions", "count"),
			},
			{
				Name:            "invoke_driver_action",
				Description:     "Execute a semantic app-driver action using the strongest available transport chain",
				SideEffectClass: "external_write",
				InputSchema: objectSchema(map[string]any{
					"session_id":      stringSchema("Desktop session id."),
					"app":             stringSchema("Optional target application name. Defaults to the frontmost compatible app."),
					"bundle_id":       stringSchema("Optional target application bundle identifier."),
					"driver_id":       stringSchema("Optional driver id guardrail or target selector."),
					"semantic_action": stringSchema("Semantic driver action id such as search.submit or media.play_toggle."),
					"arguments":       openObjectSchema("Optional driver-specific action arguments."),
				}, "session_id", "semantic_action"),
				OutputSchema: objectSchema(map[string]any{
					"semantic_action":   stringSchema("Executed semantic action id."),
					"driver_id":         stringSchema("Resolved driver id."),
					"support_tier":      stringSchema("Resolved driver support tier."),
					"app_family":        stringSchema("Resolved driver application family."),
					"semantic_richness": stringSchema("Resolved driver semantic richness."),
					"view_model":        stringSchema("Resolved driver view model."),
					"app":               stringSchema("Resolved application name."),
					"bundle_id":         stringSchema("Resolved application bundle identifier."),
					"invoked":           booleanSchema("Whether a semantic action transport chain was sent."),
					"verified":          booleanSchema("Whether the semantic action was post-verified."),
					"verification_mode": stringSchema("Postcondition strategy used for verification."),
					"action_status":     stringSchema("Structured action result status."),
					"transport":         stringSchema("Resolved transport used for the action."),
					"strategy":          stringSchema("Resolved semantic execution strategy."),
					"evidence":          openObjectSchema("Action evidence and recovery metadata."),
				}, "semantic_action", "driver_id", "invoked", "action_status"),
			},
			{
				Name:            "type_text",
				Description:     "Type text into the active focused application",
				SideEffectClass: "external_write",
				InputSchema: objectSchema(map[string]any{
					"session_id":     stringSchema("Desktop session id."),
					"text":           stringSchema("Text to type."),
					"mode":           stringSchema(`Optional typing mode: "paste" or "keys". Defaults to "paste".`),
					"app":            stringSchema("Optional application name to re-focus before typing."),
					"bundle_id":      stringSchema("Optional application bundle identifier to re-focus before typing."),
					"title_contains": stringSchema("Optional window title substring to re-focus before typing."),
					"window_index": map[string]any{
						"type":        "integer",
						"description": "Optional 1-based window index to re-focus before typing.",
					},
				}, "session_id", "text"),
				OutputSchema: objectSchema(map[string]any{
					"typed":         booleanSchema("Whether the text transport was sent."),
					"mode":          stringSchema("Resolved typing mode."),
					"action_status": stringSchema("Structured action result status."),
				}, "typed"),
			},
			{
				Name:            "hotkey",
				Description:     "Press a hotkey combination in the active application",
				SideEffectClass: "external_write",
				InputSchema: objectSchema(map[string]any{
					"session_id":     stringSchema("Desktop session id."),
					"combo":          stringSchema(`Hotkey combo such as "command+shift+4" or "command+l".`),
					"app":            stringSchema("Optional application name to re-focus before pressing the hotkey."),
					"bundle_id":      stringSchema("Optional application bundle identifier to re-focus before pressing the hotkey."),
					"title_contains": stringSchema("Optional window title substring to re-focus before pressing the hotkey."),
					"window_index": map[string]any{
						"type":        "integer",
						"description": "Optional 1-based window index to re-focus before pressing the hotkey.",
					},
				}, "session_id", "combo"),
				OutputSchema: objectSchema(map[string]any{
					"pressed":       booleanSchema("Whether the hotkey transport was sent."),
					"action_status": stringSchema("Structured action result status."),
				}, "pressed"),
			},
			{
				Name:            "screenshot",
				Description:     "Capture the current desktop or a target application window as an image",
				SideEffectClass: "read",
				Idempotent:      true,
				InputSchema: objectSchema(map[string]any{
					"session_id":     stringSchema("Desktop session id."),
					"app":            stringSchema("Optional application name to capture."),
					"bundle_id":      stringSchema("Optional application bundle identifier."),
					"title_contains": stringSchema("Optional window title substring for targeted captures."),
					"window_index": map[string]any{
						"type":        "integer",
						"description": "Optional 1-based window index when capturing a specific app window.",
					},
				}, "session_id"),
				OutputSchema: objectSchema(map[string]any{
					"mime_type":    stringSchema("Screenshot MIME type."),
					"scope":        stringSchema("Whether the screenshot captured the full screen or a targeted app window."),
					"capture_mode": stringSchema("Whether the screenshot used full screen, window content, or rect fallback capture."),
					"app":          stringSchema("Resolved application name for targeted captures."),
					"bundle_id":    stringSchema("Resolved application bundle identifier for targeted captures."),
					"title":        stringSchema("Resolved window title for targeted captures."),
					"window_index": integerSchema("Resolved window index for targeted captures."),
					"window_id":    integerSchema("Resolved native window id for targeted captures."),
				}),
			},
			{
				Name:            "screen_record",
				Description:     "Record the screen for a given duration and save to a video file.",
				SideEffectClass: "local_write",
				InputSchema: objectSchema(map[string]any{
					"session_id":   stringSchema("Desktop session ID."),
					"output_path":  stringSchema("File path where the recording will be saved."),
					"duration_sec": integerSchema("Recording duration in seconds. Maximum 300."),
					"audio":        booleanSchema("Whether to capture audio. Defaults to false."),
					"display":      stringSchema("Display identifier. macOS: display index for screencapture -D flag."),
				}, "session_id", "output_path", "duration_sec"),
				OutputSchema: objectSchema(map[string]any{
					"ok":           booleanSchema("Whether the recording succeeded."),
					"path":         stringSchema("Absolute path where the recording was saved."),
					"size_bytes":   integerSchema("File size in bytes."),
					"duration_sec": integerSchema("Recording duration in seconds."),
				}, "ok"),
			},
			{
				Name:            "clipboard_read",
				Description:     "Read the current clipboard text",
				SideEffectClass: "read",
				Idempotent:      true,
				InputSchema: objectSchema(map[string]any{
					"session_id": stringSchema("Desktop session id."),
				}, "session_id"),
				OutputSchema: objectSchema(map[string]any{
					"text": stringSchema("Clipboard contents."),
				}),
			},
			{
				Name:            "capture_tree",
				Description:     "Capture a structured desktop state snapshot for reasoning",
				SideEffectClass: "read",
				Idempotent:      true,
				InputSchema: objectSchema(map[string]any{
					"session_id": stringSchema("Desktop session id."),
					"app":        stringSchema("Optional application name."),
					"bundle_id":  stringSchema("Optional bundle identifier."),
				}, "session_id"),
				OutputSchema: objectSchema(map[string]any{
					"frontmost_app": openObjectSchema("Frontmost app summary."),
					"apps": map[string]any{
						"type":        "array",
						"description": "Desktop app summaries.",
						"items":       openObjectSchema("Application snapshot."),
					},
				}),
			},
			{
				Name:            "clipboard_write",
				Description:     "Write text into the system clipboard",
				SideEffectClass: "external_write",
				InputSchema: objectSchema(map[string]any{
					"session_id": stringSchema("Desktop session id."),
					"text":       stringSchema("Text to put into the clipboard."),
				}, "session_id", "text"),
				OutputSchema: objectSchema(map[string]any{
					"written": booleanSchema("Whether clipboard data was updated."),
				}),
			},
			{
				Name:            "mouse_move",
				Description:     "Move the mouse cursor to screen coordinates",
				SideEffectClass: "external_write",
				InputSchema: objectSchema(map[string]any{
					"session_id": stringSchema("Desktop session id."),
					"x":          integerSchema("Target X coordinate in screen space."),
					"y":          integerSchema("Target Y coordinate in screen space."),
				}, "session_id", "x", "y"),
			},
			{
				Name:            "mouse_click",
				Description:     "Click at screen coordinates",
				SideEffectClass: "external_write",
				InputSchema: objectSchema(map[string]any{
					"session_id":  stringSchema("Desktop session id."),
					"x":           integerSchema("Target X coordinate in screen space."),
					"y":           integerSchema("Target Y coordinate in screen space."),
					"button":      stringSchema("Mouse button: left or right."),
					"click_count": integerSchema("Number of clicks. Defaults to 1."),
				}, "session_id", "x", "y"),
				OutputSchema: objectSchema(map[string]any{
					"clicked":     booleanSchema("Whether the click was sent."),
					"click_count": integerSchema("Number of clicks sent."),
				}, "clicked"),
			},
			{
				Name:            "scroll",
				Description:     "Scroll the current desktop view",
				SideEffectClass: "external_write",
				InputSchema: objectSchema(map[string]any{
					"session_id": stringSchema("Desktop session id."),
					"dx":         integerSchema("Horizontal scroll delta."),
					"dy":         integerSchema("Vertical scroll delta."),
				}, "session_id"),
				OutputSchema: objectSchema(map[string]any{
					"scrolled": booleanSchema("Whether the scroll event was sent."),
					"dx":       integerSchema("Horizontal scroll delta."),
					"dy":       integerSchema("Vertical scroll delta."),
				}, "scrolled"),
			},
			{
				Name:            "find_element",
				Description:     "Find UI elements in the target application using native accessibility APIs",
				SideEffectClass: "read",
				Idempotent:      true,
				InputSchema: objectSchema(map[string]any{
					"session_id":  stringSchema("Desktop session id."),
					"app":         stringSchema("Target application name."),
					"bundle_id":   stringSchema("Target application bundle identifier."),
					"path":        stringSchema("Optional stable element path returned by a previous desktop.find_element call."),
					"role":        stringSchema("Optional accessibility role filter."),
					"text":        stringSchema("Optional exact text/name/value filter."),
					"contains":    stringSchema("Optional substring filter against label, description, or value."),
					"match_index": integerSchema("Optional zero-based match index when multiple elements match."),
					"max_depth":   integerSchema("Maximum traversal depth. Defaults to a platform-specific safe limit."),
					"max_results": integerSchema("Maximum number of matches to return."),
				}, "session_id"),
				OutputSchema: objectSchema(map[string]any{
					"matches": map[string]any{
						"type":        "array",
						"description": "Matched UI elements.",
						"items":       openObjectSchema("UI element summary."),
					},
					"match_count": integerSchema("Number of matched UI elements."),
				}, "matches", "match_count"),
			},
			{
				Name:            "click_element",
				Description:     "Click a UI element located via native accessibility APIs",
				SideEffectClass: "external_write",
				InputSchema: objectSchema(map[string]any{
					"session_id":  stringSchema("Desktop session id."),
					"app":         stringSchema("Target application name."),
					"bundle_id":   stringSchema("Target application bundle identifier."),
					"path":        stringSchema("Stable element path from desktop.find_element."),
					"role":        stringSchema("Optional accessibility role filter."),
					"text":        stringSchema("Optional exact text/name/value filter."),
					"contains":    stringSchema("Optional substring filter against label, description, or value."),
					"match_index": integerSchema("Optional zero-based match index when multiple elements match."),
					"max_depth":   integerSchema("Maximum traversal depth."),
					"max_results": integerSchema("Maximum number of matches to scan."),
				}, "session_id"),
				OutputSchema: objectSchema(map[string]any{
					"clicked": booleanSchema("Whether the element was clicked."),
					"match":   openObjectSchema("The resolved element."),
				}, "clicked"),
			},
			{
				Name:            "set_element_value",
				Description:     "Set the value of an input-like UI element and verify the result",
				SideEffectClass: "external_write",
				InputSchema: objectSchema(map[string]any{
					"session_id":  stringSchema("Desktop session id."),
					"value":       stringSchema("Text to write into the element."),
					"app":         stringSchema("Target application name."),
					"bundle_id":   stringSchema("Target application bundle identifier."),
					"path":        stringSchema("Stable element path from desktop.find_element."),
					"role":        stringSchema("Optional accessibility role filter."),
					"text":        stringSchema("Optional exact text/name/value locator filter."),
					"contains":    stringSchema("Optional substring locator filter."),
					"match_index": integerSchema("Optional zero-based match index."),
					"max_depth":   integerSchema("Maximum traversal depth."),
					"max_results": integerSchema("Maximum number of matches to scan."),
				}, "session_id", "value"),
				OutputSchema: objectSchema(map[string]any{
					"set":      booleanSchema("Whether the value was updated."),
					"verified": booleanSchema("Whether the post-action element value matched."),
					"match":    openObjectSchema("The resolved element after the update."),
				}, "set", "verified"),
			},
			{
				Name:            "clear_element",
				Description:     "Clear the value of an input-like UI element and verify it is empty",
				SideEffectClass: "external_write",
				InputSchema: objectSchema(map[string]any{
					"session_id":  stringSchema("Desktop session id."),
					"app":         stringSchema("Target application name."),
					"bundle_id":   stringSchema("Target application bundle identifier."),
					"path":        stringSchema("Stable element path from desktop.find_element."),
					"role":        stringSchema("Optional accessibility role filter."),
					"text":        stringSchema("Optional exact text/name/value filter."),
					"contains":    stringSchema("Optional substring locator filter."),
					"match_index": integerSchema("Optional zero-based match index."),
					"max_depth":   integerSchema("Maximum traversal depth."),
					"max_results": integerSchema("Maximum number of matches to scan."),
				}, "session_id"),
				OutputSchema: objectSchema(map[string]any{
					"cleared":  booleanSchema("Whether a clear action was sent."),
					"verified": booleanSchema("Whether the post-action value is empty."),
					"match":    openObjectSchema("The resolved element after the clear."),
				}, "cleared", "verified"),
			},
			{
				Name:            "get_element_value",
				Description:     "Read the current value of a UI element located via native accessibility APIs",
				SideEffectClass: "read",
				Idempotent:      true,
				InputSchema: objectSchema(map[string]any{
					"session_id":  stringSchema("Desktop session id."),
					"app":         stringSchema("Target application name."),
					"bundle_id":   stringSchema("Target application bundle identifier."),
					"path":        stringSchema("Stable element path from desktop.find_element."),
					"role":        stringSchema("Optional accessibility role filter."),
					"text":        stringSchema("Optional exact text/name/value filter."),
					"contains":    stringSchema("Optional substring locator filter."),
					"match_index": integerSchema("Optional zero-based match index."),
					"max_depth":   integerSchema("Maximum traversal depth."),
					"max_results": integerSchema("Maximum number of matches to scan."),
				}, "session_id"),
				OutputSchema: objectSchema(map[string]any{
					"value": stringSchema("Current element value."),
					"match": openObjectSchema("The resolved element."),
				}, "value"),
			},
			{
				Name:            "assert_element",
				Description:     "Wait for an element to exist and optionally verify its value",
				SideEffectClass: "read",
				Idempotent:      true,
				InputSchema: objectSchema(map[string]any{
					"session_id":     stringSchema("Desktop session id."),
					"app":            stringSchema("Target application name."),
					"bundle_id":      stringSchema("Target application bundle identifier."),
					"path":           stringSchema("Stable element path from desktop.find_element."),
					"role":           stringSchema("Optional accessibility role filter."),
					"text":           stringSchema("Optional exact text/name/value filter."),
					"contains":       stringSchema("Optional substring locator filter."),
					"value_equals":   stringSchema("Optional exact value assertion."),
					"value_contains": stringSchema("Optional substring value assertion."),
					"match_index":    integerSchema("Optional zero-based match index."),
					"max_depth":      integerSchema("Maximum traversal depth."),
					"max_results":    integerSchema("Maximum number of matches to scan."),
					"timeout_ms":     integerSchema("Maximum time to wait for the assertion to pass."),
				}, "session_id"),
				OutputSchema: objectSchema(map[string]any{
					"passed": booleanSchema("Whether the assertion passed within the timeout."),
					"match":  openObjectSchema("The resolved element."),
				}, "passed"),
			},
			{
				Name:            "find_text",
				Description:     "Locate visible text on screen using OCR",
				SideEffectClass: "read",
				Idempotent:      true,
				InputSchema: objectSchema(map[string]any{
					"session_id":     stringSchema("Desktop session id."),
					"text":           stringSchema("Visible text to find."),
					"app":            stringSchema("Optional application name to focus before OCR."),
					"bundle_id":      stringSchema("Optional application bundle identifier."),
					"title_contains": stringSchema("Optional window title substring for window-scoped OCR."),
					"window_index": map[string]any{
						"type":        "integer",
						"description": "Optional 1-based window index for window-scoped OCR.",
					},
				}, "session_id", "text"),
				OutputSchema: objectSchema(map[string]any{
					"scope":        stringSchema("Whether OCR searched the full screen or a targeted app window."),
					"capture_mode": stringSchema("Whether OCR used screen, window content, or rect fallback capture."),
					"matches": map[string]any{
						"type":        "array",
						"description": "OCR match results with coordinates.",
						"items":       openObjectSchema("OCR match."),
					},
					"match_count": integerSchema("Number of OCR matches."),
				}, "matches", "match_count"),
			},
			{
				Name:            "click_text",
				Description:     "Find visible text and click the best match",
				SideEffectClass: "external_write",
				InputSchema: objectSchema(map[string]any{
					"session_id":     stringSchema("Desktop session id."),
					"text":           stringSchema("Visible text to find and click."),
					"app":            stringSchema("Optional application name to focus before OCR."),
					"bundle_id":      stringSchema("Optional application bundle identifier."),
					"title_contains": stringSchema("Optional window title substring for window-scoped OCR."),
					"window_index": map[string]any{
						"type":        "integer",
						"description": "Optional 1-based window index for window-scoped OCR.",
					},
					"match_index": integerSchema("Optional match index to click when multiple matches exist."),
				}, "session_id", "text"),
				OutputSchema: objectSchema(map[string]any{
					"clicked":       booleanSchema("Whether a matching text target was clicked."),
					"match":         openObjectSchema("Clicked OCR match."),
					"action_status": stringSchema("Structured action result status."),
				}, "clicked"),
			},
		},
		ArtifactKinds:  []string{"desktop.screenshot", "desktop.capture_tree"},
		Events:         []string{"desktop.session.opened", "desktop.session.closed"},
		ApprovalPolicy: "policy",
	}
}

func (c *Capability) Health(ctx context.Context) captypes.Health {
	if c.client == nil {
		return captypes.Health{
			Status:  captypes.StatusUnavailable,
			Message: "desktop host is not configured",
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
		Message: "desktop host reachable",
	}
}

func (c *Capability) Invoke(ctx context.Context, req captypes.InvokeRequest) (*captypes.InvokeResult, error) {
	if c.client == nil {
		return nil, fmt.Errorf("desktop host is not configured")
	}
	routedReq, plannedTrace := c.prepareRoutedRequest(req)
	resp, err := c.client.Do(ctx, desktoptypes.Request{
		Action:    routedReq.Operation,
		SessionID: routedReq.SessionID,
		Params:    routedReq.Params,
	})
	if err != nil {
		return nil, err
	}
	result := &captypes.InvokeResult{
		OK:          resp.OK,
		Data:        supportmaps.Clone(resp.Data),
		ArtifactRef: resp.ArtifactRef,
		Error:       resp.Error,
	}
	chosenTransport := inferDesktopTransport(routedReq.Operation, result.Data)
	trace := c.finalizeRouteTrace(plannedTrace, chosenTransport)
	c.decorateInvokeResult(routedReq.Operation, result, trace)
	sessionID := normalize.FirstNonEmpty(routedReq.SessionID, resp.SessionID, normalize.String(result.Data["session_id"]))
	c.rememberSessionState(sessionID, routedReq, result, trace)
	if !resp.OK && strings.TrimSpace(resp.Error) != "" {
		return result, fmt.Errorf("desktop host: %s", resp.Error)
	}
	return result, nil
}

func (c *Capability) OpenSession(ctx context.Context, params map[string]any) (*captypes.SessionHandle, error) {
	result, err := c.Invoke(ctx, captypes.InvokeRequest{
		Operation: desktoptypes.ActionCreateSession,
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
		return nil, fmt.Errorf("desktop host did not return a session id")
	}
	handle := &captypes.SessionHandle{
		ID:         sessionID,
		Capability: "desktop",
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
		Operation: desktoptypes.ActionCloseSession,
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

func integerSchema(description string) map[string]any {
	schema := map[string]any{"type": "integer"}
	if description != "" {
		schema["description"] = description
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
