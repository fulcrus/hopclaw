//go:build darwin

package desktopd

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	desktoptypes "github.com/fulcrus/hopclaw/desktopapi/types"
)

type desktopCommandSnapshot struct {
	CommandID            string   `json:"command_id"`
	Title                string   `json:"title"`
	MenuPath             []string `json:"menu_path"`
	MenuPathString       string   `json:"menu_path_string"`
	Enabled              bool     `json:"enabled"`
	HasSubmenu           bool     `json:"has_submenu"`
	Aliases              []string `json:"aliases,omitempty"`
	Accelerator          string   `json:"accelerator,omitempty"`
	CommandChar          string   `json:"command_char,omitempty"`
	CommandGlyph         int      `json:"command_glyph,omitempty"`
	CommandMods          int      `json:"command_modifiers,omitempty"`
	CommandVKey          int      `json:"command_virtual_key,omitempty"`
	Source               string   `json:"source"`
	Scope                string   `json:"scope"`
	Transport            string   `json:"preferred_transport"`
	RiskLevel            string   `json:"risk_level,omitempty"`
	SafetyClass          string   `json:"safety_class,omitempty"`
	RequiresConfirmation bool     `json:"requires_confirmation,omitempty"`
	AvailableByDefault   bool     `json:"available_by_default"`
	DriverID             string   `json:"driver_id,omitempty"`
	SupportTier          string   `json:"support_tier,omitempty"`
}

type rawMenuCommandSnapshot struct {
	Title        string   `json:"title"`
	MenuPath     []string `json:"menu_path"`
	Enabled      bool     `json:"enabled"`
	HasSubmenu   bool     `json:"has_submenu"`
	CommandChar  string   `json:"cmd_char"`
	CommandGlyph int      `json:"cmd_glyph"`
	CommandMods  int      `json:"cmd_modifiers"`
	CommandVKey  int      `json:"cmd_virtual_key"`
}

type menuCommandListResult struct {
	App      desktopAppSnapshot       `json:"app"`
	Commands []rawMenuCommandSnapshot `json:"commands"`
}

func (s *darwinSession) handleListCommands(ctx context.Context, params map[string]any) (*desktoptypes.Response, error) {
	appName := stringParam(params, "app")
	bundleID := stringParam(params, "bundle_id")
	maxDepth := intParam(params, "max_depth")
	maxResults := intParam(params, "max_results")
	includeDisabled := boolParam(params, "include_disabled")
	includeSystem := boolParam(params, "include_system")
	includeUnsafe := boolParam(params, "include_unsafe")

	app, commands, err := listMenuCommands(ctx, appName, bundleID, maxDepth, rawMenuCommandLimit(maxResults), includeDisabled)
	if err != nil {
		return nil, fmt.Errorf("list_commands: %w", err)
	}
	driver := resolveAppDriver(app)
	commands = filterCommandSnapshots(commands, desktopCommandListOptions{
		IncludeSystem: includeSystem,
		IncludeUnsafe: includeUnsafe,
		MaxResults:    maxResults,
	})
	s.setFocusLease(app.Name, app.BundleID, "", 0)
	s.setVisibilityLease(app.Name, app.BundleID, "", 0)

	data := annotateActionResult(map[string]any{
		"app":      toMap(&app, false),
		"commands": commandsToAny(commands),
		"count":    len(commands),
	}, actionStatusVerified, "menu_inventory", "command_graph_scan", 0.97, map[string]any{
		"app":              app.Name,
		"bundle_id":        app.BundleID,
		"driver_id":        driver.ID,
		"support_tier":     strings.ToUpper(driver.SupportTier),
		"include_disabled": includeDisabled,
		"include_system":   includeSystem,
		"include_unsafe":   includeUnsafe,
		"max_depth":        maxDepth,
		"max_results":      maxResults,
	})

	return &desktoptypes.Response{OK: true, Data: data}, nil
}

func (s *darwinSession) handleInvokeCommand(ctx context.Context, params map[string]any) (*desktoptypes.Response, error) {
	appName := stringParam(params, "app")
	bundleID := stringParam(params, "bundle_id")
	allowUnsafe := boolParam(params, "allow_unsafe")
	transport := strings.ToLower(firstNonEmpty(stringParam(params, "transport"), "menu"))
	if transport != "menu" && transport != "hotkey" && transport != "auto" {
		return nil, fmt.Errorf("invoke_command: unsupported transport %q", transport)
	}

	app, commands, err := listMenuCommands(ctx, appName, bundleID, 4, 1500, true)
	if err != nil {
		return nil, fmt.Errorf("invoke_command: %w", err)
	}
	s.setFocusLease(app.Name, app.BundleID, "", 0)
	s.setVisibilityLease(app.Name, app.BundleID, "", 0)
	command, err := resolveCommandTarget(params, commands)
	if err != nil {
		return nil, fmt.Errorf("invoke_command: %w", err)
	}
	if err := commandBlockedByPolicy(command, allowUnsafe); err != nil {
		return nil, fmt.Errorf("invoke_command: %w", err)
	}
	if !command.Enabled {
		return nil, fmt.Errorf("invoke_command: command %q is disabled", command.MenuPathString)
	}

	actualTransport := transport
	if actualTransport == "auto" {
		if strings.TrimSpace(command.Accelerator) != "" {
			actualTransport = "hotkey"
		} else {
			actualTransport = "menu"
		}
	}

	switch actualTransport {
	case "hotkey":
		if strings.TrimSpace(command.Accelerator) == "" {
			return nil, fmt.Errorf("invoke_command: command %q does not expose a usable accelerator", command.MenuPathString)
		}
		if err := pressCommandAccelerator(ctx, command); err != nil {
			return nil, fmt.Errorf("invoke_command: %w", err)
		}
	case "menu":
		if err := invokeMenuPath(ctx, app.Name, app.BundleID, command.MenuPath); err != nil {
			return nil, fmt.Errorf("invoke_command: %w", err)
		}
	default:
		return nil, fmt.Errorf("invoke_command: unsupported transport %q", actualTransport)
	}

	data := annotateActionResult(map[string]any{
		"invoked":   true,
		"transport": actualTransport,
		"command":   commandToMap(command),
	}, actionStatusAttempted, actualTransport, "command_invoke", confidenceForCommandTransport(actualTransport), map[string]any{
		"app":                 app.Name,
		"bundle_id":           app.BundleID,
		"menu_path":           command.MenuPath,
		"accelerator":         command.Accelerator,
		"allow_unsafe":        allowUnsafe,
		"requested_transport": transport,
	})
	return &desktoptypes.Response{OK: true, Data: data}, nil
}

func listMenuCommands(ctx context.Context, appName, bundleID string, maxDepth, maxResults int, includeDisabled bool) (desktopAppSnapshot, []desktopCommandSnapshot, error) {
	if maxDepth <= 0 {
		maxDepth = 3
	}
	if maxResults <= 0 {
		maxResults = 500
	}

	resolvedApp, err := focusCommandTargetApp(ctx, appName, bundleID)
	if err != nil {
		return desktopAppSnapshot{}, nil, err
	}
	driver := resolveAppDriver(resolvedApp)

	appJSON, _ := json.Marshal(resolvedApp.Name)
	bundleIDJSON, _ := json.Marshal(resolvedApp.BundleID)
	maxDepthJSON, _ := json.Marshal(maxDepth)
	maxResultsJSON, _ := json.Marshal(maxResults)
	includeDisabledJSON, _ := json.Marshal(includeDisabled)
	script := fmt.Sprintf(`(() => {
  const requestedName = %s;
  const requestedBundleID = %s;
  const maxDepth = %s;
  const maxResults = %s;
  const includeDisabled = %s;
  const se = Application("System Events");
  const procs = se.applicationProcesses();

  function safeCall(fn, fallback) {
    try { return fn(); } catch (_) { return fallback; }
  }

  function findProcess() {
    if (requestedName) {
      try {
        const named = se.processes.byName(requestedName);
        if (safeCall(() => named.exists(), true)) return named;
      } catch (_) {}
    }
    if (requestedBundleID) {
      for (let i = 0; i < procs.length; i++) {
        if (safeCall(() => procs[i].bundleIdentifier(), "") === requestedBundleID) {
          return procs[i];
        }
      }
    }
    for (let i = 0; i < procs.length; i++) {
      if (safeCall(() => procs[i].frontmost(), false)) {
        return procs[i];
      }
    }
    throw new Error("application process not found");
  }

  function attr(item, name, fallback) {
    try {
      return item.attributes.byName(name).value();
    } catch (_) {
      return fallback;
    }
  }

  const proc = findProcess();
  const bar = proc.menuBars[0];
  const results = [];

  function walkMenu(menu, path, depth) {
    if (!menu || results.length >= maxResults || depth > maxDepth) return;
    const items = safeCall(() => menu.menuItems(), []);
    for (let i = 0; i < items.length; i++) {
      if (results.length >= maxResults) return;
      const item = items[i];
      const title = safeCall(() => item.name(), "");
      if (!title) continue;
      const enabled = !!safeCall(() => item.enabled(), false);
      const hasSubmenu = safeCall(() => item.menus.length > 0, false);
      const nextPath = path.concat([title]);
      if (includeDisabled || enabled) {
        results.push({
          title: title,
          menu_path: nextPath,
          enabled: enabled,
          has_submenu: hasSubmenu,
          cmd_char: String(attr(item, "AXMenuItemCmdChar", "") || ""),
          cmd_glyph: Number(attr(item, "AXMenuItemCmdGlyph", 0) || 0),
          cmd_modifiers: Number(attr(item, "AXMenuItemCmdModifiers", 0) || 0),
          cmd_virtual_key: Number(attr(item, "AXMenuItemCmdVirtualKey", 0) || 0)
        });
      }
      if (hasSubmenu && depth < maxDepth) {
        walkMenu(item.menus[0], nextPath, depth + 1);
      }
    }
  }

  const topMenus = safeCall(() => bar.menuBarItems(), []);
  for (let i = 0; i < topMenus.length; i++) {
    if (results.length >= maxResults) break;
    const top = topMenus[i];
    const topTitle = safeCall(() => top.name(), "");
    if (!topTitle) continue;
    if (safeCall(() => top.menus.length > 0, false)) {
      walkMenu(top.menus[0], [topTitle], 1);
    }
  }

  return JSON.stringify({
    app: {
      name: safeCall(() => proc.name(), requestedName),
      bundle_id: safeCall(() => proc.bundleIdentifier(), requestedBundleID),
      pid: Number(safeCall(() => proc.unixId(), 0) || 0),
      frontmost: safeCall(() => proc.frontmost(), false),
      window_count: safeCall(() => proc.windows().length, 0)
    },
    commands: results
  });
})()`, string(appJSON), string(bundleIDJSON), string(maxDepthJSON), string(maxResultsJSON), string(includeDisabledJSON))

	var payload menuCommandListResult
	if err := runJXAJSON(ctx, script, &payload); err != nil {
		return desktopAppSnapshot{}, nil, err
	}
	commands := make([]desktopCommandSnapshot, 0, len(payload.Commands))
	for _, raw := range payload.Commands {
		commands = append(commands, normalizeMenuCommand(driver, raw))
	}
	return payload.App, commands, nil
}

func focusCommandTargetApp(ctx context.Context, appName, bundleID string) (desktopAppSnapshot, error) {
	if strings.TrimSpace(appName) != "" || strings.TrimSpace(bundleID) != "" {
		switch {
		case strings.TrimSpace(bundleID) != "":
			if err := runAppleScript(ctx, fmt.Sprintf(`tell application id "%s" to activate`, escapeAppleScript(bundleID))); err != nil {
				return desktopAppSnapshot{}, err
			}
		case strings.TrimSpace(appName) != "":
			if err := runAppleScript(ctx, fmt.Sprintf(`tell application "%s" to activate`, escapeAppleScript(appName))); err != nil {
				return desktopAppSnapshot{}, err
			}
		}
		state, _, err := waitForAppReady(ctx, appName, bundleID, desktopWaitFocused, desktopReadyDefaultWait)
		if err != nil {
			return desktopAppSnapshot{}, err
		}
		return state.App, nil
	}
	return resolveTargetApp(ctx, "", "", false)
}

func normalizeMenuCommand(driver desktopAppDriver, raw rawMenuCommandSnapshot) desktopCommandSnapshot {
	pathString := strings.Join(raw.MenuPath, " > ")
	accelerator := acceleratorFromAX(raw.CommandChar, raw.CommandMods)
	command := desktopCommandSnapshot{
		CommandID:      normalizeCommandID(raw.MenuPath),
		Title:          raw.Title,
		MenuPath:       append([]string(nil), raw.MenuPath...),
		MenuPathString: pathString,
		Enabled:        raw.Enabled,
		HasSubmenu:     raw.HasSubmenu,
		Accelerator:    accelerator,
		CommandChar:    raw.CommandChar,
		CommandGlyph:   raw.CommandGlyph,
		CommandMods:    raw.CommandMods,
		CommandVKey:    raw.CommandVKey,
		Source:         "menu",
		Scope:          "app",
		Transport:      preferredCommandTransport(accelerator),
	}
	return applyCommandPolicy(driver, command)
}

func normalizeCommandID(menuPath []string) string {
	return "menu:" + strings.Join(menuPath, " > ")
}

func resolveCommandTarget(params map[string]any, commands []desktopCommandSnapshot) (desktopCommandSnapshot, error) {
	commandID := stringParam(params, "command_id")
	menuPath := stringParam(params, "menu_path")
	title := stringParam(params, "title")
	switch {
	case commandID != "":
		for _, command := range commands {
			if command.CommandID == commandID {
				return command, nil
			}
		}
		return desktopCommandSnapshot{}, fmt.Errorf("command_id %q not found", commandID)
	case menuPath != "":
		want := normalizeCommandID(splitMenuPath(menuPath))
		for _, command := range commands {
			if command.CommandID == want {
				return command, nil
			}
		}
		return desktopCommandSnapshot{}, fmt.Errorf("menu_path %q not found", menuPath)
	case title != "":
		for _, command := range commands {
			if command.Title == title {
				return command, nil
			}
		}
		return desktopCommandSnapshot{}, fmt.Errorf("title %q not found", title)
	default:
		return desktopCommandSnapshot{}, errors.New("invoke_command requires command_id, menu_path, or title")
	}
}

func splitMenuPath(menuPath string) []string {
	parts := strings.Split(menuPath, ">")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			out = append(out, part)
		}
	}
	return out
}

func invokeMenuPath(ctx context.Context, appName, bundleID string, menuPath []string) error {
	if len(menuPath) < 2 {
		return errors.New("menu_path must include a top menu and leaf item")
	}
	appJSON, _ := json.Marshal(appName)
	bundleIDJSON, _ := json.Marshal(bundleID)
	menuPathJSON, _ := json.Marshal(menuPath)
	script := fmt.Sprintf(`(() => {
  const requestedName = %s;
  const requestedBundleID = %s;
  const path = %s;
  const se = Application("System Events");
  const procs = se.applicationProcesses();

  function safeCall(fn, fallback) {
    try { return fn(); } catch (_) { return fallback; }
  }

  function findProcess() {
    if (requestedName) {
      try {
        const named = se.processes.byName(requestedName);
        if (safeCall(() => named.exists(), true)) return named;
      } catch (_) {}
    }
    if (requestedBundleID) {
      for (let i = 0; i < procs.length; i++) {
        if (safeCall(() => procs[i].bundleIdentifier(), "") === requestedBundleID) {
          return procs[i];
        }
      }
    }
    throw new Error("application process not found");
  }

  function findByTitle(collection, title) {
    for (let i = 0; i < collection.length; i++) {
      if (safeCall(() => collection[i].name(), "") === title) {
        return collection[i];
      }
    }
    return null;
  }

  const proc = findProcess();
  proc.frontmost = true;
  const barItems = safeCall(() => proc.menuBars[0].menuBarItems(), []);
  const top = findByTitle(barItems, path[0]);
  if (!top) throw new Error("top menu not found");
  let currentMenu = safeCall(() => top.menus[0], null);
  if (!currentMenu) throw new Error("menu not found");

  for (let i = 1; i < path.length; i++) {
    const title = path[i];
    const items = safeCall(() => currentMenu.menuItems(), []);
    const item = findByTitle(items, title);
    if (!item) throw new Error("menu item not found");
    const enabled = !!safeCall(() => item.enabled(), false);
    if (!enabled) throw new Error("menu item is disabled");
    if (i === path.length - 1) {
      item.click();
      return JSON.stringify({ok:true});
    }
    currentMenu = safeCall(() => item.menus[0], null);
    if (!currentMenu) throw new Error("submenu not found");
  }
  throw new Error("menu item not found");
})()`, string(appJSON), string(bundleIDJSON), string(menuPathJSON))

	var payload map[string]any
	if err := runJXAJSON(ctx, script, &payload); err != nil {
		return err
	}
	time.Sleep(inputSettleDelay)
	return nil
}

func pressCommandAccelerator(ctx context.Context, command desktopCommandSnapshot) error {
	combo := strings.TrimSpace(command.Accelerator)
	if combo == "" {
		return errors.New("accelerator is unavailable")
	}
	script, err := hotkeyAppleScript(combo)
	if err != nil {
		return err
	}
	if err := runAppleScript(ctx, script); err != nil {
		return err
	}
	time.Sleep(inputSettleDelay)
	return nil
}

func preferredCommandTransport(accelerator string) string {
	if strings.TrimSpace(accelerator) != "" {
		return "hotkey"
	}
	return "menu"
}

func acceleratorFromAX(commandChar string, modifiers int) string {
	commandChar = strings.TrimSpace(commandChar)
	if commandChar == "" {
		return ""
	}
	parts := make([]string, 0, 4)
	// AXMenuItemCmdModifiers omits command unless the "no command" bit is set.
	if modifiers&8 == 0 {
		parts = append(parts, "command")
	}
	if modifiers&1 != 0 {
		parts = append(parts, "shift")
	}
	if modifiers&2 != 0 {
		parts = append(parts, "option")
	}
	if modifiers&4 != 0 {
		parts = append(parts, "control")
	}
	parts = append(parts, strings.ToLower(commandChar))
	return strings.Join(parts, "+")
}

func confidenceForCommandTransport(transport string) float64 {
	switch transport {
	case "menu":
		return 0.98
	case "hotkey":
		return 0.94
	default:
		return 0.9
	}
}

func rawMenuCommandLimit(maxResults int) int {
	switch {
	case maxResults <= 0:
		return 500
	case maxResults < 50:
		return minInt(maxResults*8, 2000)
	default:
		return minInt(maxResults*4, 4000)
	}
}

func commandsToAny(commands []desktopCommandSnapshot) []any {
	out := make([]any, 0, len(commands))
	for _, command := range commands {
		out = append(out, commandToMap(command))
	}
	return out
}

func commandToMap(command desktopCommandSnapshot) map[string]any {
	return map[string]any{
		"command_id":            command.CommandID,
		"title":                 command.Title,
		"menu_path":             append([]string(nil), command.MenuPath...),
		"menu_path_string":      command.MenuPathString,
		"enabled":               command.Enabled,
		"has_submenu":           command.HasSubmenu,
		"aliases":               append([]string(nil), command.Aliases...),
		"accelerator":           command.Accelerator,
		"command_char":          command.CommandChar,
		"command_glyph":         command.CommandGlyph,
		"command_modifiers":     command.CommandMods,
		"command_virtual_key":   command.CommandVKey,
		"source":                command.Source,
		"scope":                 command.Scope,
		"preferred_transport":   command.Transport,
		"risk_level":            command.RiskLevel,
		"safety_class":          command.SafetyClass,
		"requires_confirmation": command.RequiresConfirmation,
		"available_by_default":  command.AvailableByDefault,
		"driver_id":             command.DriverID,
		"support_tier":          command.SupportTier,
	}
}
