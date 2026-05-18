//go:build darwin

package desktopd

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	_ "image/png"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	desktoptypes "github.com/fulcrus/hopclaw/desktopapi/types"
)

const (
	driverActionShortTimeout = 5 * time.Second
	driverActionLongTimeout  = 20 * time.Second
)

type desktopDriverActionDefinition struct {
	ID                  string
	Description         string
	PreferredTransports []string
	Verify              string
	ArgumentsSchema     map[string]any
}

var desktopDriverActionCatalog = map[string][]desktopDriverActionDefinition{
	"qqmusic": {
		{
			ID:                  "app.search.focus",
			Description:         "Focus the in-app search entry point.",
			PreferredTransports: []string{"menu_command", "verified_hotkey", "accessibility_recovery"},
			Verify:              "search_surface_focused",
		},
		{
			ID:                  "search.submit",
			Description:         "Focus search, type a query, and submit it.",
			PreferredTransports: []string{"menu_command", "verified_hotkey", "ocr_text_presence"},
			Verify:              "query_visible",
			ArgumentsSchema: objectSchema(map[string]any{
				"query": stringSchema("Search query text."),
				"text":  stringSchema("Alias of query for compatibility."),
			}),
		},
		{
			ID:                  "media.play_toggle",
			Description:         "Toggle QQMusic playback state.",
			PreferredTransports: []string{"menu_command", "verified_hotkey"},
			Verify:              "menu_state_changed",
		},
	},
	"douyin": {
		{
			ID:                  "app.search.focus",
			Description:         "Focus the Douyin search box using OCR-anchored targeting.",
			PreferredTransports: []string{"ocr_anchored_visual"},
			Verify:              "search_anchor_clicked",
		},
		{
			ID:                  "search.submit",
			Description:         "Focus search, type a query, and submit it.",
			PreferredTransports: []string{"ocr_anchored_visual", "verified_input"},
			Verify:              "query_visible",
			ArgumentsSchema: objectSchema(map[string]any{
				"query": stringSchema("Search query text."),
				"text":  stringSchema("Alias of query for compatibility."),
			}),
		},
		{
			ID:                  "media.next_item",
			Description:         "Advance to the next media item in the feed.",
			PreferredTransports: []string{"verified_scroll"},
			Verify:              "content_signature_changed",
			ArgumentsSchema: objectSchema(map[string]any{
				"dy": integerSchema("Optional vertical scroll delta. Defaults to -900."),
			}),
		},
	},
	"premiere_pro": {
		{
			ID:                  "project.open_recent_first",
			Description:         "Open the first recent project from the Premiere welcome screen.",
			PreferredTransports: []string{"ocr_anchored_visual"},
			Verify:              "welcome_screen_replaced",
		},
		{
			ID:                  "timeline.play_toggle",
			Description:         "Toggle timeline playback in Premiere.",
			PreferredTransports: []string{"verified_hotkey"},
			Verify:              "timecode_motion_toggled",
		},
	},
}

var premiereTimecodePattern = regexp.MustCompile(`\b\d{1,2}[:;：]\d{2}[:;：]\d{2}[:;：]\d{2}\b`)

type driverWindowOCRSnapshot struct {
	App         string
	BundleID    string
	CaptureMode string
	MatchCount  int
	Matches     []map[string]any
	Fingerprint []uint8
	Evidence    map[string]any
}

type premiereRecentProjectMenuItem struct {
	TopMenu string `json:"top_menu"`
	Parent  string `json:"parent"`
	Title   string `json:"title"`
	Enabled bool   `json:"enabled"`
}

func (s *darwinSession) handleListDriverActions(ctx context.Context, params map[string]any) (*desktoptypes.Response, error) {
	app, driver, err := resolveDriverActionTarget(ctx, params, false)
	if err != nil {
		return nil, fmt.Errorf("list_driver_actions: %w", err)
	}
	actions := driverActionsFor(driver)
	s.setFocusLease(app.Name, app.BundleID, "", 0)
	if app.WindowCount > 0 {
		s.setVisibilityLease(app.Name, app.BundleID, "", 0)
	}
	data := annotateActionResult(map[string]any{
		"app":               toMap(&app, false),
		"driver_id":         driver.ID,
		"app_family":        driver.AppFamily,
		"support_tier":      strings.ToUpper(driver.SupportTier),
		"semantic_richness": driver.SemanticRichness,
		"view_model":        driver.ViewModel,
		"actions":           driverActionsToAny(actions),
		"count":             len(actions),
	}, actionStatusVerified, "driver_manifest", "driver_action_inventory", 0.98, map[string]any{
		"app":          app.Name,
		"bundle_id":    app.BundleID,
		"driver_id":    driver.ID,
		"support_tier": strings.ToUpper(driver.SupportTier),
	})
	return &desktoptypes.Response{OK: true, Data: data}, nil
}

func (s *darwinSession) handleInvokeDriverAction(ctx context.Context, params map[string]any) (*desktoptypes.Response, error) {
	semanticAction := firstNonEmpty(stringParam(params, "semantic_action"), stringParam(params, "action_id"))
	if semanticAction == "" {
		return nil, errors.New("invoke_driver_action requires params.semantic_action")
	}
	app, driver, err := resolveDriverActionTarget(ctx, params, true)
	if err != nil {
		return nil, fmt.Errorf("invoke_driver_action: %w", err)
	}
	definition, ok := resolveDriverActionDefinition(driver, semanticAction)
	if !ok {
		return nil, fmt.Errorf("invoke_driver_action: driver %q does not expose semantic action %q", driver.ID, semanticAction)
	}
	args := driverActionArguments(params)

	var data map[string]any
	switch driver.ID {
	case "qqmusic":
		data, err = s.invokeQQMusicDriverAction(ctx, app, semanticAction, args)
	case "douyin":
		data, err = s.invokeDouyinDriverAction(ctx, app, semanticAction, args)
	case "premiere_pro":
		data, err = s.invokePremiereDriverAction(ctx, app, semanticAction, args)
	default:
		err = fmt.Errorf("driver %q does not implement semantic actions yet", driver.ID)
	}
	if err != nil {
		return nil, fmt.Errorf("invoke_driver_action: %w", err)
	}
	if data == nil {
		data = make(map[string]any)
	}
	data["semantic_action"] = semanticAction
	data["driver_id"] = driver.ID
	data["support_tier"] = strings.ToUpper(driver.SupportTier)
	data["app_family"] = driver.AppFamily
	data["semantic_richness"] = driver.SemanticRichness
	data["view_model"] = driver.ViewModel
	data["app"] = firstNonEmpty(normalizeString(data["app"]), app.Name)
	data["bundle_id"] = firstNonEmpty(normalizeString(data["bundle_id"]), app.BundleID)
	if _, ok := data["invoked"]; !ok {
		data["invoked"] = true
	}
	if _, ok := data["verification_mode"]; !ok {
		data["verification_mode"] = definition.Verify
	}
	return &desktoptypes.Response{OK: true, Data: data}, nil
}

func driverActionsFor(driver desktopAppDriver) []desktopDriverActionDefinition {
	definitions := desktopDriverActionCatalog[driver.ID]
	if len(definitions) == 0 {
		return nil
	}
	out := make([]desktopDriverActionDefinition, 0, len(definitions))
	for _, definition := range definitions {
		definition.ArgumentsSchema = cloneMapAny(definition.ArgumentsSchema)
		definition.PreferredTransports = append([]string(nil), definition.PreferredTransports...)
		out = append(out, definition)
	}
	return out
}

func resolveDriverActionDefinition(driver desktopAppDriver, actionID string) (desktopDriverActionDefinition, bool) {
	actionID = strings.TrimSpace(actionID)
	for _, definition := range driverActionsFor(driver) {
		if definition.ID == actionID {
			return definition, true
		}
	}
	return desktopDriverActionDefinition{}, false
}

func driverActionsToAny(definitions []desktopDriverActionDefinition) []any {
	out := make([]any, 0, len(definitions))
	for _, definition := range definitions {
		out = append(out, map[string]any{
			"id":                   definition.ID,
			"description":          definition.Description,
			"preferred_transports": append([]string(nil), definition.PreferredTransports...),
			"verify":               definition.Verify,
			"arguments_schema":     cloneMapAny(definition.ArgumentsSchema),
		})
	}
	return out
}

func resolveDriverActionTarget(ctx context.Context, params map[string]any, includeWindows bool) (desktopAppSnapshot, desktopAppDriver, error) {
	appName := stringParam(params, "app")
	bundleID := stringParam(params, "bundle_id")
	requestedDriverID := strings.ToLower(strings.TrimSpace(stringParam(params, "driver_id")))

	if appName != "" || bundleID != "" {
		app, err := resolveTargetApp(ctx, appName, bundleID, includeWindows)
		if err != nil {
			return desktopAppSnapshot{}, desktopAppDriver{}, err
		}
		driver := resolveAppDriver(app)
		if requestedDriverID != "" && requestedDriverID != driver.ID {
			return desktopAppSnapshot{}, desktopAppDriver{}, fmt.Errorf("target app %q resolved to driver %q, not %q", app.Name, driver.ID, requestedDriverID)
		}
		return app, driver, nil
	}

	snapshot, err := desktopSnapshot(ctx, includeWindows)
	if err != nil {
		return desktopAppSnapshot{}, desktopAppDriver{}, err
	}
	if requestedDriverID != "" {
		if snapshot.FrontmostApp != nil {
			driver := resolveAppDriver(*snapshot.FrontmostApp)
			if driver.ID == requestedDriverID {
				return *snapshot.FrontmostApp, driver, nil
			}
		}
		for _, app := range snapshot.Apps {
			driver := resolveAppDriver(app)
			if driver.ID == requestedDriverID {
				return app, driver, nil
			}
		}
		return desktopAppSnapshot{}, desktopAppDriver{}, fmt.Errorf("no running application matched driver_id %q", requestedDriverID)
	}

	app, err := snapshot.findApp("", "")
	if err != nil {
		return desktopAppSnapshot{}, desktopAppDriver{}, err
	}
	return app, resolveAppDriver(app), nil
}

func driverActionArguments(params map[string]any) map[string]any {
	out := mapParam(params, "arguments")
	for key, value := range params {
		switch key {
		case "app", "bundle_id", "driver_id", "semantic_action", "action_id", "arguments":
			continue
		default:
			if out == nil {
				out = make(map[string]any)
			}
			out[key] = cloneValue(value)
		}
	}
	return out
}

func mapParam(params map[string]any, key string) map[string]any {
	if len(params) == 0 {
		return nil
	}
	value, _ := params[key].(map[string]any)
	return cloneMapAny(value)
}

func (s *darwinSession) invokeQQMusicDriverAction(ctx context.Context, app desktopAppSnapshot, actionID string, args map[string]any) (map[string]any, error) {
	app, err := s.focusDriverApp(ctx, app)
	if err != nil {
		return nil, err
	}
	recovery, err := s.qqmusicDismissUpdatePrompt(ctx, app)
	if err != nil {
		return nil, err
	}
	switch actionID {
	case "app.search.focus":
		return s.qqmusicSearchFocus(ctx, app, recovery)
	case "search.submit":
		query := firstNonEmpty(stringParam(args, "query"), stringParam(args, "text"))
		if query == "" {
			return nil, errors.New("search.submit requires arguments.query")
		}
		return s.qqmusicSearchSubmit(ctx, app, query, recovery)
	case "media.play_toggle":
		return s.qqmusicPlayToggle(ctx, app, recovery)
	default:
		return nil, fmt.Errorf("unsupported QQMusic semantic action %q", actionID)
	}
}

func (s *darwinSession) qqmusicDismissUpdatePrompt(ctx context.Context, app desktopAppSnapshot) (map[string]any, error) {
	candidates := []string{"以后提醒", "忽略此版本更新", "稍后"}
	for _, label := range candidates {
		match, err := darwinResolveSingleElement(ctx, elementQuery{
			App:        app.Name,
			BundleID:   app.BundleID,
			Role:       "AXButton",
			Text:       label,
			MaxDepth:   6,
			MaxResults: 1,
		})
		if err != nil {
			continue
		}
		if _, err := s.handleMouseClick(ctx, map[string]any{"x": match.X, "y": match.Y}); err != nil {
			return nil, fmt.Errorf("dismiss QQMusic update prompt: %w", err)
		}
		time.Sleep(300 * time.Millisecond)
		return map[string]any{
			"update_prompt_dismissed": true,
			"dismiss_button":          label,
		}, nil
	}
	return map[string]any{"update_prompt_dismissed": false}, nil
}

func (s *darwinSession) qqmusicSearchFocus(ctx context.Context, app desktopAppSnapshot, recovery map[string]any) (map[string]any, error) {
	commands, err := listDriverMenuCommands(ctx, app)
	if err != nil {
		return nil, err
	}
	command, ok := findCommandByAlias(commands, "app.search.focus", "search.submit")
	if !ok {
		command, ok = findCommandByTitle(commands, []string{"编辑", "Edit"}, "搜索", "Search")
	}
	if !ok {
		return nil, errors.New("QQMusic search command not found")
	}
	beforeCount := countTextInputElements(ctx, app)
	transport, err := invokeDriverCommand(ctx, app, command, "menu")
	if err != nil {
		return nil, err
	}
	time.Sleep(250 * time.Millisecond)
	afterCount := countTextInputElements(ctx, app)
	verified := afterCount > beforeCount && afterCount > 0
	status := actionStatusAttempted
	if verified {
		status = actionStatusVerified
	}
	return annotateActionResult(map[string]any{
		"app":       app.Name,
		"bundle_id": app.BundleID,
		"focused":   true,
		"verified":  verified,
	}, status, transport, "driver_search_focus", 0.92, mergeEvidenceMaps(recovery, map[string]any{
		"command_id":               command.CommandID,
		"menu_path":                command.MenuPath,
		"text_inputs_before":       beforeCount,
		"text_inputs_after":        afterCount,
		"verification_mode":        "text_input_count_delta",
		"preferred_driver_command": command.MenuPathString,
	})), nil
}

func (s *darwinSession) qqmusicSearchSubmit(ctx context.Context, app desktopAppSnapshot, query string, recovery map[string]any) (map[string]any, error) {
	focusData, err := s.qqmusicSearchFocus(ctx, app, recovery)
	if err != nil {
		return nil, err
	}
	if err := s.clearFocusedTextField(ctx); err != nil {
		return nil, err
	}
	if _, err := s.handleTypeText(ctx, map[string]any{"text": query}); err != nil {
		return nil, fmt.Errorf("QQMusic search.submit type query: %w", err)
	}
	if _, err := s.handleHotkey(ctx, map[string]any{"combo": "return"}); err != nil {
		return nil, fmt.Errorf("QQMusic search.submit press return: %w", err)
	}
	verifyResp, err := s.waitForVisibleText(ctx, map[string]any{
		"app":       app.Name,
		"bundle_id": app.BundleID,
		"text":      query,
	}, driverActionShortTimeout)
	if err != nil {
		return nil, fmt.Errorf("QQMusic search.submit verify query %q: %w", query, err)
	}
	matches := ocrMatchesFromResponse(verifyResp)
	return annotateActionResult(map[string]any{
		"app":         app.Name,
		"bundle_id":   app.BundleID,
		"query":       query,
		"verified":    true,
		"match_count": len(matches),
		"matches":     matches,
	}, actionStatusVerified, "menu+paste", "driver_search_submit", 0.95, mergeEvidenceMaps(focusData, map[string]any{
		"verification_mode": "ocr_text_presence",
	})), nil
}

func (s *darwinSession) qqmusicPlayToggle(ctx context.Context, app desktopAppSnapshot, recovery map[string]any) (map[string]any, error) {
	beforeCommands, err := listDriverMenuCommands(ctx, app)
	if err != nil {
		return nil, err
	}
	before, ok := findQQMusicPlayToggleCommand(beforeCommands)
	if !ok {
		return nil, errors.New("QQMusic play toggle command not found")
	}
	transport, err := invokeDriverCommand(ctx, app, before, "menu")
	if err != nil {
		return nil, err
	}
	time.Sleep(350 * time.Millisecond)
	afterCommands, err := listDriverMenuCommands(ctx, app)
	if err != nil {
		return nil, err
	}
	after, ok := findQQMusicPlayToggleCommand(afterCommands)
	if !ok {
		return nil, errors.New("QQMusic play toggle verification command not found")
	}
	verified := !strings.EqualFold(strings.TrimSpace(before.Title), strings.TrimSpace(after.Title))
	status := actionStatusAttempted
	confidence := 0.9
	if verified {
		status = actionStatusVerified
		confidence = 0.97
	}
	return annotateActionResult(map[string]any{
		"app":          app.Name,
		"bundle_id":    app.BundleID,
		"verified":     verified,
		"before_title": before.Title,
		"after_title":  after.Title,
	}, status, transport, "driver_media_toggle", confidence, mergeEvidenceMaps(recovery, map[string]any{
		"command_id":          before.CommandID,
		"verification_mode":   "menu_state_changed",
		"command_before":      before.MenuPathString,
		"command_after":       after.MenuPathString,
		"requested_transport": "menu",
	})), nil
}

func (s *darwinSession) invokeDouyinDriverAction(ctx context.Context, app desktopAppSnapshot, actionID string, args map[string]any) (map[string]any, error) {
	app, err := s.focusDriverApp(ctx, app)
	if err != nil {
		return nil, err
	}
	switch actionID {
	case "app.search.focus":
		match, err := s.douyinSearchFocus(ctx, app)
		if err != nil {
			return nil, err
		}
		return annotateActionResult(map[string]any{
			"app":       app.Name,
			"bundle_id": app.BundleID,
			"focused":   true,
			"verified":  false,
			"anchor":    match,
		}, actionStatusAttempted, "mouse", "driver_search_focus", 0.84, map[string]any{
			"verification_mode": "search_anchor_clicked",
		}), nil
	case "search.submit":
		query := firstNonEmpty(stringParam(args, "query"), stringParam(args, "text"))
		if query == "" {
			return nil, errors.New("search.submit requires arguments.query")
		}
		match, err := s.douyinSearchFocus(ctx, app)
		if err != nil {
			return nil, err
		}
		if err := s.clearFocusedTextField(ctx); err != nil {
			return nil, err
		}
		if _, err := s.handleTypeText(ctx, map[string]any{"text": query}); err != nil {
			return nil, fmt.Errorf("Douyin search.submit type query: %w", err)
		}
		if _, err := s.handleHotkey(ctx, map[string]any{"combo": "return"}); err != nil {
			return nil, fmt.Errorf("Douyin search.submit press return: %w", err)
		}
		verifyResp, err := s.waitForVisibleText(ctx, map[string]any{
			"app":       app.Name,
			"bundle_id": app.BundleID,
			"text":      query,
		}, driverActionShortTimeout)
		if err != nil {
			return nil, fmt.Errorf("Douyin search.submit verify query %q: %w", query, err)
		}
		return annotateActionResult(map[string]any{
			"app":         app.Name,
			"bundle_id":   app.BundleID,
			"query":       query,
			"verified":    true,
			"match_count": intValue(verifyResp.Data["match_count"]),
			"matches":     ocrMatchesFromResponse(verifyResp),
			"anchor":      match,
		}, actionStatusVerified, "mouse+paste", "driver_search_submit", 0.9, map[string]any{
			"verification_mode": "ocr_text_presence",
		}), nil
	case "media.next_item":
		dy := intParam(args, "dy")
		if dy == 0 {
			dy = -900
		}
		if err := s.focusDriverWindowContentArea(ctx, app, 0.5, 0.55); err != nil {
			return nil, fmt.Errorf("Douyin media.next_item focus content: %w", err)
		}
		before, err := s.captureDriverWindowOCRSnapshot(ctx, app)
		if err != nil {
			return nil, fmt.Errorf("Douyin media.next_item pre-capture: %w", err)
		}
		if _, err := s.handleScroll(ctx, map[string]any{"dx": 0, "dy": dy}); err != nil {
			return nil, fmt.Errorf("Douyin media.next_item: %w", err)
		}
		time.Sleep(700 * time.Millisecond)
		after, err := s.captureDriverWindowOCRSnapshot(ctx, app)
		if err != nil {
			return nil, fmt.Errorf("Douyin media.next_item post-capture: %w", err)
		}
		verified, signatureEvidence := evaluateDouyinNextItemVerification(before, after)
		status := actionStatusAttempted
		confidence := 0.78
		if verified {
			status = actionStatusVerified
			confidence = 0.88
		}
		return annotateActionResult(map[string]any{
			"app":       app.Name,
			"bundle_id": app.BundleID,
			"verified":  verified,
			"dy":        dy,
		}, status, "scroll", "driver_media_next_item", confidence, mergeEvidenceMaps(signatureEvidence, map[string]any{
			"capture_mode_before": before.CaptureMode,
			"capture_mode_after":  after.CaptureMode,
			"verification_mode":   "content_signature_changed",
		})), nil
	default:
		return nil, fmt.Errorf("unsupported Douyin semantic action %q", actionID)
	}
}

func (s *darwinSession) douyinSearchFocus(ctx context.Context, app desktopAppSnapshot) (map[string]any, error) {
	resp, err := s.handleFindText(ctx, map[string]any{
		"app":       app.Name,
		"bundle_id": app.BundleID,
		"text":      "搜索",
	})
	if err != nil {
		return nil, err
	}
	match, ok := selectDouyinSearchMatch(ocrMatchesFromResponse(resp))
	if !ok {
		return nil, errors.New("Douyin search anchor not found")
	}
	if _, err := s.handleMouseClick(ctx, map[string]any{
		"x": intValue(match["x"]),
		"y": intValue(match["y"]),
	}); err != nil {
		return nil, fmt.Errorf("Douyin search anchor click: %w", err)
	}
	time.Sleep(250 * time.Millisecond)
	return match, nil
}

func (s *darwinSession) invokePremiereDriverAction(ctx context.Context, app desktopAppSnapshot, actionID string, args map[string]any) (map[string]any, error) {
	switch actionID {
	case "project.open_recent_first":
		app, err := s.focusDriverProcess(ctx, app)
		if err != nil {
			return nil, err
		}
		currentProject := premiereCurrentProjectTitle(app)
		menuPath, menuEvidence, menuErr := s.findPremiereRecentProjectMenuPath(ctx, app, currentProject)
		if menuErr == nil {
			if err := invokeMenuPath(ctx, app.Name, app.BundleID, menuPath); err != nil {
				return nil, fmt.Errorf("Premiere invoke recent project menu path: %w", err)
			}
			verified, verificationEvidence, err := s.waitForPremiereProjectOpen(ctx, app, currentProject, menuPath[len(menuPath)-1])
			if err != nil {
				return nil, err
			}
			status := actionStatusAttempted
			confidence := 0.84
			if verified {
				status = actionStatusVerified
				confidence = 0.92
			}
			return annotateActionResult(map[string]any{
				"app":              app.Name,
				"bundle_id":        app.BundleID,
				"verified":         verified,
				"selected_project": menuPath[len(menuPath)-1],
			}, status, "menu", "driver_open_recent_first", confidence, mergeEvidenceMaps(menuEvidence, verificationEvidence)), nil
		}
		resp, err := s.handleFindText(ctx, map[string]any{
			"app":       app.Name,
			"bundle_id": app.BundleID,
			"text":      "Premiere Pro Project",
		})
		if err != nil {
			return nil, fmt.Errorf("Premiere recent project lookup failed: %w", menuErr)
		}
		match, ok := selectTopmostOCRMatch(ocrMatchesFromResponse(resp))
		if !ok {
			if menuEvidence != nil {
				menuEvidence["menu_lookup_error"] = menuErr.Error()
			}
			return nil, errors.New("Premiere recent project entry not found")
		}
		if _, err := s.handleMouseClick(ctx, map[string]any{
			"x": intValue(match["x"]),
			"y": intValue(match["y"]),
		}); err != nil {
			return nil, fmt.Errorf("Premiere click recent project: %w", err)
		}
		verified, verificationEvidence, err := s.waitForPremiereProjectOpen(ctx, app, currentProject, "")
		if err != nil {
			return nil, err
		}
		status := actionStatusAttempted
		confidence := 0.82
		if verified {
			status = actionStatusVerified
			confidence = 0.9
		}
		return annotateActionResult(map[string]any{
			"app":       app.Name,
			"bundle_id": app.BundleID,
			"verified":  verified,
			"match":     match,
		}, status, "mouse", "driver_open_recent_first", confidence, mergeEvidenceMaps(menuEvidence, verificationEvidence, map[string]any{
			"menu_lookup_error": menuErr.Error(),
		})), nil
	case "timeline.play_toggle":
		app, err := s.focusDriverApp(ctx, app)
		if err != nil {
			return nil, err
		}
		if blocked, preconditionEvidence, err := s.detectPremierePlaybackBlocker(ctx, app); err != nil {
			return nil, fmt.Errorf("Premiere timeline.play_toggle precondition: %w", err)
		} else if blocked {
			return annotateActionResult(map[string]any{
				"app":       app.Name,
				"bundle_id": app.BundleID,
				"verified":  false,
				"blocked":   true,
				"invoked":   false,
			}, actionStatusFailed, "precondition", "driver_timeline_play_toggle", 0.96, preconditionEvidence), nil
		}
		if err := s.focusDriverWindowContentArea(ctx, app, 0.5, 0.72); err != nil {
			return nil, fmt.Errorf("Premiere timeline.play_toggle focus timeline: %w", err)
		}
		beforeA, err := s.captureDriverWindowOCRSnapshot(ctx, app)
		if err != nil {
			return nil, fmt.Errorf("Premiere timeline.play_toggle pre-capture A: %w", err)
		}
		if blocked, preconditionEvidence := evaluatePremierePlaybackPrecondition(beforeA); blocked {
			return annotateActionResult(map[string]any{
				"app":       app.Name,
				"bundle_id": app.BundleID,
				"verified":  false,
				"blocked":   true,
				"invoked":   false,
			}, actionStatusFailed, "precondition", "driver_timeline_play_toggle", 0.94, mergeEvidenceMaps(beforeA.Evidence, preconditionEvidence, map[string]any{
				"verification_mode": "playback_precondition",
				"toggle_state":      "blocked",
			})), nil
		}
		time.Sleep(700 * time.Millisecond)
		beforeB, err := s.captureDriverWindowOCRSnapshot(ctx, app)
		if err != nil {
			return nil, fmt.Errorf("Premiere timeline.play_toggle pre-capture B: %w", err)
		}
		if _, err := s.handleHotkey(ctx, map[string]any{"combo": "space"}); err != nil {
			return nil, fmt.Errorf("Premiere timeline.play_toggle: %w", err)
		}
		time.Sleep(250 * time.Millisecond)
		afterA, err := s.captureDriverWindowOCRSnapshot(ctx, app)
		if err != nil {
			return nil, fmt.Errorf("Premiere timeline.play_toggle post-capture A: %w", err)
		}
		time.Sleep(700 * time.Millisecond)
		afterB, err := s.captureDriverWindowOCRSnapshot(ctx, app)
		if err != nil {
			return nil, fmt.Errorf("Premiere timeline.play_toggle post-capture B: %w", err)
		}
		verified, playbackEvidence := evaluatePremierePlaybackToggle(beforeA, beforeB, afterA, afterB)
		status := actionStatusAttempted
		confidence := 0.86
		if verified {
			status = actionStatusVerified
			confidence = 0.93
		}
		return annotateActionResult(map[string]any{
			"app":       app.Name,
			"bundle_id": app.BundleID,
			"verified":  verified,
		}, status, "hotkey", "driver_timeline_play_toggle", confidence, mergeEvidenceMaps(playbackEvidence, map[string]any{
			"combo":             "space",
			"verification_mode": "timecode_motion_toggled",
		})), nil
	default:
		_ = args
		return nil, fmt.Errorf("unsupported Premiere semantic action %q", actionID)
	}
}

func (s *darwinSession) waitForPremiereProjectOpen(ctx context.Context, app desktopAppSnapshot, previousProject, expectedProject string) (bool, map[string]any, error) {
	previousBase := premiereProjectBaseName(previousProject)
	expectedBase := premiereProjectBaseName(expectedProject)
	deadline := time.Now().Add(driverActionLongTimeout)
	for {
		resolved, err := resolveTargetApp(ctx, app.Name, app.BundleID, true)
		if err == nil {
			title, index, ok := premiereCurrentWindowTitleWithIndex(resolved)
			currentBase := premiereProjectBaseName(title)
			if ok && currentBase != "" {
				switch {
				case expectedBase != "" && strings.EqualFold(currentBase, expectedBase):
					return true, map[string]any{
						"verification_mode": "window_title_changed",
						"window_title":      title,
						"window_index":      index,
						"current_project":   currentBase,
						"expected_project":  expectedBase,
						"previous_project":  previousBase,
					}, nil
				case expectedBase == "" && (previousBase == "" || !strings.EqualFold(currentBase, previousBase)):
					return true, map[string]any{
						"verification_mode": "window_title_changed",
						"window_title":      title,
						"window_index":      index,
						"current_project":   currentBase,
						"previous_project":  previousBase,
					}, nil
				}
			}
		}
		resp, err := s.handleFindText(ctx, map[string]any{
			"app":       app.Name,
			"bundle_id": app.BundleID,
			"text":      "Premiere Pro Project",
		})
		if err == nil && intValue(resp.Data["match_count"]) == 0 && previousBase == "" && expectedBase == "" {
			return true, map[string]any{
				"verification_mode":        "welcome_entries_disappeared",
				"remaining_recent_entries": 0,
			}, nil
		}
		if time.Now().After(deadline) {
			matchCount := 0
			if err == nil && resp != nil {
				matchCount = intValue(resp.Data["match_count"])
			}
			return false, map[string]any{
				"verification_mode":        "window_title_changed",
				"remaining_recent_entries": matchCount,
				"expected_project":         expectedBase,
				"previous_project":         previousBase,
			}, nil
		}
		if err := sleepContext(ctx, 1200*time.Millisecond); err != nil {
			return false, nil, err
		}
	}
}

func (s *darwinSession) focusDriverApp(ctx context.Context, app desktopAppSnapshot) (desktopAppSnapshot, error) {
	if err := ensureForeground(ctx, app.Name, app.BundleID); err != nil {
		return desktopAppSnapshot{}, err
	}
	resolved, err := resolveTargetApp(ctx, app.Name, app.BundleID, true)
	if err != nil {
		return desktopAppSnapshot{}, err
	}
	s.setFocusLease(resolved.Name, resolved.BundleID, "", 0)
	if resolved.WindowCount > 0 {
		s.setVisibilityLease(resolved.Name, resolved.BundleID, "", 0)
	}
	return resolved, nil
}

func (s *darwinSession) focusDriverProcess(ctx context.Context, app desktopAppSnapshot) (desktopAppSnapshot, error) {
	switch {
	case strings.TrimSpace(app.BundleID) != "":
		if err := runAppleScript(ctx, fmt.Sprintf(`tell application id "%s" to activate`, escapeAppleScript(app.BundleID))); err != nil {
			return desktopAppSnapshot{}, err
		}
	case strings.TrimSpace(app.Name) != "":
		if err := runAppleScript(ctx, fmt.Sprintf(`tell application "%s" to activate`, escapeAppleScript(app.Name))); err != nil {
			return desktopAppSnapshot{}, err
		}
	}
	state, _, recovered, err := waitForAppReadyWithWindowRecovery(ctx, app.Name, app.BundleID, desktopWaitFocused, desktopReadyDefaultWait)
	if err != nil {
		return desktopAppSnapshot{}, err
	}
	resolved := state.App
	if resolved.Name == "" && resolved.BundleID == "" {
		var resolveErr error
		resolved, resolveErr = resolveTargetApp(ctx, app.Name, app.BundleID, true)
		if resolveErr != nil {
			return desktopAppSnapshot{}, resolveErr
		}
	}
	s.setFocusLease(resolved.Name, resolved.BundleID, "", 0)
	if recovered || resolved.WindowCount > 0 {
		s.setVisibilityLease(resolved.Name, resolved.BundleID, "", 0)
	}
	return resolved, nil
}

func (s *darwinSession) focusDriverWindowContentArea(ctx context.Context, app desktopAppSnapshot, xFraction, yFraction float64) error {
	refreshed, err := resolveTargetApp(ctx, app.Name, app.BundleID, true)
	if err == nil {
		app = refreshed
	}
	window, ok := matchTargetWindow(app, "", 0)
	if !ok {
		return errors.New("window not found")
	}
	if len(window.Position) < 2 || len(window.Size) < 2 || window.Size[0] <= 0 || window.Size[1] <= 0 {
		return errors.New("window bounds unavailable")
	}
	if xFraction <= 0 || xFraction >= 1 {
		xFraction = 0.5
	}
	if yFraction <= 0 || yFraction >= 1 {
		yFraction = 0.5
	}
	x := window.Position[0] + int(float64(window.Size[0])*xFraction)
	y := window.Position[1] + int(float64(window.Size[1])*yFraction)
	if _, err := s.handleMouseClick(ctx, map[string]any{"x": x, "y": y}); err != nil {
		return err
	}
	time.Sleep(200 * time.Millisecond)
	return nil
}

func (s *darwinSession) findPremiereRecentProjectMenuPath(ctx context.Context, app desktopAppSnapshot, currentProject string) ([]string, map[string]any, error) {
	appJSON, _ := json.Marshal(app.Name)
	bundleIDJSON, _ := json.Marshal(app.BundleID)
	script := fmt.Sprintf(`(() => {
  const requestedName = %s;
  const requestedBundleID = %s;
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

  function hasRecentName(value) {
    const current = String(value || "").toLowerCase();
    return current.includes("打开最近使用的内容") || current.includes("open recent") || current.includes("recent");
  }

  function matchesFileMenu(value) {
    const current = String(value || "");
    return current === "文件" || current === "File";
  }

  const proc = findProcess();
  proc.frontmost = true;
  const barItems = safeCall(() => proc.menuBars[0].menuBarItems(), []);
  const items = [];
  for (let i = 0; i < barItems.length; i++) {
    const top = barItems[i];
    const topTitle = safeCall(() => top.name(), "");
    if (!matchesFileMenu(topTitle)) continue;
    const menuItems = safeCall(() => top.menus[0].menuItems(), []);
    for (let j = 0; j < menuItems.length; j++) {
      const parent = menuItems[j];
      const parentTitle = safeCall(() => parent.name(), "");
      if (!hasRecentName(parentTitle)) continue;
      const recentItems = safeCall(() => parent.menus[0].menuItems(), []);
      for (let k = 0; k < recentItems.length; k++) {
        items.push({
          top_menu: topTitle,
          parent: parentTitle,
          title: safeCall(() => recentItems[k].name(), ""),
          enabled: !!safeCall(() => recentItems[k].enabled(), false)
        });
      }
    }
  }
  return JSON.stringify({items: items});
})()`, string(appJSON), string(bundleIDJSON))

	var payload struct {
		Items []premiereRecentProjectMenuItem `json:"items"`
	}
	if err := runJXAJSON(ctx, script, &payload); err != nil {
		return nil, nil, err
	}
	selected, selectionEvidence, ok := selectPremiereRecentProjectMenuItem(payload.Items, currentProject)
	if !ok {
		return nil, selectionEvidence, errors.New("Premiere recent project menu entry not found")
	}
	menuPath := []string{selected.TopMenu, selected.Parent, selected.Title}
	return menuPath, mergeEvidenceMaps(selectionEvidence, map[string]any{
		"selected_project":   selected.Title,
		"selected_menu_path": menuPath,
	}), nil
}

func (s *darwinSession) detectPremierePlaybackBlocker(ctx context.Context, app desktopAppSnapshot) (bool, map[string]any, error) {
	for _, phrase := range []string{"在此处放下媒体以创建序列", "Drop media here to create sequence"} {
		resp, err := s.handleFindText(ctx, map[string]any{
			"app":       app.Name,
			"bundle_id": app.BundleID,
			"text":      phrase,
		})
		if err != nil {
			continue
		}
		if intValue(resp.Data["match_count"]) == 0 {
			continue
		}
		match, _ := selectTopmostOCRMatch(ocrMatchesFromResponse(resp))
		return true, map[string]any{
			"reason":            "no_playable_sequence",
			"matched_text":      normalizeString(match["text"]),
			"match":             match,
			"verification_mode": "playback_precondition",
		}, nil
	}
	return false, nil, nil
}

func listDriverMenuCommands(ctx context.Context, app desktopAppSnapshot) ([]desktopCommandSnapshot, error) {
	_, commands, err := listMenuCommands(ctx, app.Name, app.BundleID, 4, 1000, true)
	if err != nil {
		return nil, err
	}
	return commands, nil
}

func invokeDriverCommand(ctx context.Context, app desktopAppSnapshot, command desktopCommandSnapshot, preferredTransport string) (string, error) {
	transport := strings.TrimSpace(strings.ToLower(preferredTransport))
	if transport == "" || transport == "auto" {
		if strings.TrimSpace(command.Accelerator) != "" {
			transport = "hotkey"
		} else {
			transport = "menu"
		}
	}
	switch transport {
	case "hotkey":
		if err := pressCommandAccelerator(ctx, command); err != nil {
			if preferredTransport == "" || strings.EqualFold(preferredTransport, "auto") {
				if menuErr := invokeMenuPath(ctx, app.Name, app.BundleID, command.MenuPath); menuErr == nil {
					return "menu", nil
				}
			}
			return "", err
		}
		return "hotkey", nil
	case "menu":
		if err := invokeMenuPath(ctx, app.Name, app.BundleID, command.MenuPath); err != nil {
			return "", err
		}
		return "menu", nil
	default:
		return "", fmt.Errorf("unsupported driver transport %q", preferredTransport)
	}
}

func findCommandByAlias(commands []desktopCommandSnapshot, aliases ...string) (desktopCommandSnapshot, bool) {
	for _, alias := range aliases {
		for _, command := range commands {
			if commandHasAlias(command, alias) && command.Enabled {
				return command, true
			}
		}
	}
	return desktopCommandSnapshot{}, false
}

func findCommandByTitle(commands []desktopCommandSnapshot, topMenus []string, titles ...string) (desktopCommandSnapshot, bool) {
	for _, command := range commands {
		if len(command.MenuPath) == 0 || !command.Enabled {
			continue
		}
		if len(topMenus) > 0 && !stringInFold(command.MenuPath[0], topMenus...) {
			continue
		}
		for _, title := range titles {
			if strings.EqualFold(strings.TrimSpace(command.Title), strings.TrimSpace(title)) {
				return command, true
			}
		}
	}
	return desktopCommandSnapshot{}, false
}

func findQQMusicPlayToggleCommand(commands []desktopCommandSnapshot) (desktopCommandSnapshot, bool) {
	for _, title := range []string{"播放", "暂停", "Play", "Pause"} {
		command, ok := findCommandByTitle(commands, []string{"播放控制", "Playback"}, title)
		if ok {
			return command, true
		}
	}
	return desktopCommandSnapshot{}, false
}

func commandHasAlias(command desktopCommandSnapshot, alias string) bool {
	alias = strings.TrimSpace(alias)
	for _, current := range command.Aliases {
		if current == alias {
			return true
		}
	}
	return false
}

func countTextInputElements(ctx context.Context, app desktopAppSnapshot) int {
	count := 0
	for _, role := range []string{"AXTextField", "AXSearchField"} {
		matches, err := darwinFindElements(ctx, elementQuery{
			App:        app.Name,
			BundleID:   app.BundleID,
			Role:       role,
			MaxDepth:   6,
			MaxResults: 20,
		})
		if err != nil {
			continue
		}
		count += len(matches)
	}
	return count
}

func (s *darwinSession) clearFocusedTextField(ctx context.Context) error {
	if _, err := s.handleHotkey(ctx, map[string]any{"combo": "command+a"}); err != nil {
		return fmt.Errorf("select all: %w", err)
	}
	if _, err := s.handleHotkey(ctx, map[string]any{"combo": "delete"}); err != nil {
		return fmt.Errorf("clear selection: %w", err)
	}
	return nil
}

func (s *darwinSession) captureDriverWindowOCRSnapshot(ctx context.Context, app desktopAppSnapshot) (driverWindowOCRSnapshot, error) {
	refreshed, err := resolveTargetApp(ctx, app.Name, app.BundleID, true)
	if err == nil {
		app = refreshed
	}
	window, ok := matchTargetWindow(app, "", 0)
	if !ok {
		return driverWindowOCRSnapshot{}, errors.New("window not found")
	}

	tmpDir, err := os.MkdirTemp("", "hopclaw-driver-ocr-*")
	if err != nil {
		return driverWindowOCRSnapshot{}, fmt.Errorf("create temp dir: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	imgPath := filepath.Join(tmpDir, "window.png")
	capture, err := captureTargetedPNG(ctx, app, window, imgPath)
	if err != nil {
		return driverWindowOCRSnapshot{}, fmt.Errorf("capture targeted screenshot: %w", err)
	}
	result, err := runOCRBinary(ctx, imgPath, "")
	if err != nil {
		return driverWindowOCRSnapshot{}, fmt.Errorf("run OCR: %w", err)
	}
	fingerprint, err := computeImageFingerprint(imgPath)
	if err != nil {
		return driverWindowOCRSnapshot{}, fmt.Errorf("compute image fingerprint: %w", err)
	}

	region := ocrCaptureRegion{}
	if len(capture.Bounds) >= 4 {
		region = ocrCaptureRegion{
			X:      capture.Bounds[0],
			Y:      capture.Bounds[1],
			Width:  capture.Bounds[2],
			Height: capture.Bounds[3],
		}
	} else if fallback, ok := regionFromWindow(window); ok {
		region = fallback
	}
	scale := 1.0
	if result.ImageWidth > 0 && region.Width > 0 {
		scale = float64(result.ImageWidth) / float64(region.Width)
	}
	matches := make([]map[string]any, 0, len(result.Matches))
	for _, match := range result.Matches {
		matches = append(matches, matchToScreenMap(match, scale, region, true))
	}
	return driverWindowOCRSnapshot{
		App:         app.Name,
		BundleID:    app.BundleID,
		CaptureMode: capture.CaptureMode,
		MatchCount:  len(matches),
		Matches:     matches,
		Fingerprint: fingerprint,
		Evidence: mergeEvidenceMaps(capture.Evidence, map[string]any{
			"scale":              scale,
			"fingerprint_digest": digestFingerprint(fingerprint),
		}),
	}, nil
}

func (s *darwinSession) waitForVisibleText(ctx context.Context, params map[string]any, timeout time.Duration) (*desktoptypes.Response, error) {
	deadline := time.Now().Add(timeout)
	var lastResp *desktoptypes.Response
	var lastErr error
	for {
		resp, err := s.handleFindText(ctx, params)
		if err == nil {
			lastResp = resp
			if intValue(resp.Data["match_count"]) > 0 {
				return resp, nil
			}
		} else {
			lastErr = err
		}
		if time.Now().After(deadline) {
			if lastErr != nil {
				return lastResp, lastErr
			}
			return lastResp, fmt.Errorf("text %q not visible before timeout", stringParam(params, "text"))
		}
		if err := sleepContext(ctx, 450*time.Millisecond); err != nil {
			return lastResp, err
		}
	}
}

func premiereCurrentProjectTitle(app desktopAppSnapshot) string {
	title, _, ok := premiereCurrentWindowTitleWithIndex(app)
	if !ok {
		return ""
	}
	return title
}

func premiereCurrentWindowTitleWithIndex(app desktopAppSnapshot) (string, int, bool) {
	for _, window := range app.Windows {
		title := strings.TrimSpace(window.Title)
		if title == "" {
			continue
		}
		return title, window.Index, true
	}
	return "", 0, false
}

func selectPremiereRecentProjectMenuItem(items []premiereRecentProjectMenuItem, currentProject string) (premiereRecentProjectMenuItem, map[string]any, bool) {
	currentBase := premiereProjectBaseName(currentProject)
	available := make([]string, 0, len(items))
	skippedCurrent := 0
	skippedUntitled := 0
	for _, item := range items {
		title := strings.TrimSpace(item.Title)
		if title == "" || !item.Enabled {
			continue
		}
		available = append(available, title)
		base := premiereProjectBaseName(title)
		if base == "" {
			continue
		}
		if isPremiereUntitledProject(base) {
			skippedUntitled++
			continue
		}
		if currentBase != "" && strings.EqualFold(base, currentBase) {
			skippedCurrent++
			continue
		}
		return item, map[string]any{
			"current_project":           currentBase,
			"available_projects":        truncateStrings(available, 6),
			"skipped_current":           skippedCurrent,
			"skipped_untitled":          skippedUntitled,
			"selected_project_basename": base,
		}, true
	}
	return premiereRecentProjectMenuItem{}, map[string]any{
		"current_project":    currentBase,
		"available_projects": truncateStrings(available, 6),
		"skipped_current":    skippedCurrent,
		"skipped_untitled":   skippedUntitled,
	}, false
}

func premiereProjectBaseName(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	value = strings.TrimRight(strings.ReplaceAll(value, "\\", "/"), "/")
	base := filepath.Base(value)
	if base == "." || base == "/" {
		base = value
	}
	return strings.ToLower(strings.TrimSpace(base))
}

func isPremiereUntitledProject(base string) bool {
	return equalsAnyNormalized(base, "未命名.prproj", "untitled.prproj")
}

func evaluatePremierePlaybackPrecondition(snapshot driverWindowOCRSnapshot) (bool, map[string]any) {
	for _, match := range snapshot.Matches {
		text := normalizeString(match["text"])
		if !containsAnyNormalized(text, "在此处放下媒体以创建序列", "drop media here to create sequence") {
			continue
		}
		return true, map[string]any{
			"reason":       "no_playable_sequence",
			"matched_text": text,
			"match":        cloneMapAny(match),
		}
	}
	return false, nil
}

func ocrMatchesFromResponse(resp *desktoptypes.Response) []map[string]any {
	if resp == nil || len(resp.Data) == 0 {
		return nil
	}
	switch matches := resp.Data["matches"].(type) {
	case []map[string]any:
		out := make([]map[string]any, 0, len(matches))
		for _, match := range matches {
			out = append(out, cloneMapAny(match))
		}
		return out
	case []any:
		out := make([]map[string]any, 0, len(matches))
		for _, raw := range matches {
			typed, _ := raw.(map[string]any)
			if typed != nil {
				out = append(out, cloneMapAny(typed))
			}
		}
		return out
	default:
		return nil
	}
}

func evaluateDouyinNextItemVerification(before, after driverWindowOCRSnapshot) (bool, map[string]any) {
	beforeSig := douyinOCRSignature(before.Matches)
	afterSig := douyinOCRSignature(after.Matches)
	added, removed, overlapRatio := diffOCRSignatures(beforeSig, afterSig)
	imageDistance := fingerprintDistance(before.Fingerprint, after.Fingerprint)
	ocrVerified := (len(added) >= 2 || len(removed) >= 2) && overlapRatio < 0.75
	imageVerified := imageDistance >= 0.09
	verified := ocrVerified || imageVerified
	return verified, map[string]any{
		"before_signature": beforeSig,
		"after_signature":  afterSig,
		"added_text":       truncateStrings(added, 8),
		"removed_text":     truncateStrings(removed, 8),
		"overlap_ratio":    overlapRatio,
		"image_distance":   imageDistance,
		"ocr_verified":     ocrVerified,
		"image_verified":   imageVerified,
	}
}

func evaluatePremierePlaybackToggle(beforeA, beforeB, afterA, afterB driverWindowOCRSnapshot) (bool, map[string]any) {
	beforeCodesA := extractPremiereTimecodes(beforeA.Matches)
	beforeCodesB := extractPremiereTimecodes(beforeB.Matches)
	afterCodesA := extractPremiereTimecodes(afterA.Matches)
	afterCodesB := extractPremiereTimecodes(afterB.Matches)

	beforeKnown, beforeMoving := timecodeMotionState(beforeCodesA, beforeCodesB)
	afterKnown, afterMoving := timecodeMotionState(afterCodesA, afterCodesB)
	beforeVisualDistance := fingerprintDistance(beforeA.Fingerprint, beforeB.Fingerprint)
	afterVisualDistance := fingerprintDistance(afterA.Fingerprint, afterB.Fingerprint)
	visualKnown, visualBeforeMoving, visualAfterMoving := visualMotionState(beforeVisualDistance, afterVisualDistance)

	verified := false
	state := "unknown"
	switch {
	case beforeKnown && afterKnown && beforeMoving != afterMoving:
		verified = true
		if !beforeMoving && afterMoving {
			state = "started_playback"
		} else if beforeMoving && !afterMoving {
			state = "paused_playback"
		}
	case visualKnown && visualBeforeMoving != visualAfterMoving:
		verified = true
		if !visualBeforeMoving && visualAfterMoving {
			state = "started_playback"
		} else if visualBeforeMoving && !visualAfterMoving {
			state = "paused_playback"
		}
	}

	return verified, map[string]any{
		"before_timecodes_a":     beforeCodesA,
		"before_timecodes_b":     beforeCodesB,
		"after_timecodes_a":      afterCodesA,
		"after_timecodes_b":      afterCodesB,
		"before_motion":          beforeMoving,
		"before_known":           beforeKnown,
		"after_motion":           afterMoving,
		"after_known":            afterKnown,
		"before_visual_distance": beforeVisualDistance,
		"after_visual_distance":  afterVisualDistance,
		"visual_known":           visualKnown,
		"before_visual_motion":   visualBeforeMoving,
		"after_visual_motion":    visualAfterMoving,
		"toggle_state":           state,
	}
}

func computeImageFingerprint(imagePath string) ([]uint8, error) {
	file, err := os.Open(imagePath)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	img, _, err := image.Decode(file)
	if err != nil {
		return nil, err
	}
	bounds := img.Bounds()
	if bounds.Empty() {
		return nil, errors.New("empty image bounds")
	}
	const cols = 12
	const rows = 12
	out := make([]uint8, 0, cols*rows)
	width := bounds.Dx()
	height := bounds.Dy()
	for row := 0; row < rows; row++ {
		y0 := bounds.Min.Y + row*height/rows
		y1 := bounds.Min.Y + (row+1)*height/rows
		if y1 <= y0 {
			y1 = y0 + 1
		}
		for col := 0; col < cols; col++ {
			x0 := bounds.Min.X + col*width/cols
			x1 := bounds.Min.X + (col+1)*width/cols
			if x1 <= x0 {
				x1 = x0 + 1
			}
			var sum uint64
			var samples uint64
			for _, sample := range fingerprintSamplePoints(x0, x1) {
				for _, sampleY := range fingerprintSamplePoints(y0, y1) {
					r, g, b, _ := img.At(sample, sampleY).RGBA()
					gray := (299*r + 587*g + 114*b) / 1000
					sum += uint64(gray >> 8)
					samples++
				}
			}
			if samples == 0 {
				out = append(out, 0)
				continue
			}
			out = append(out, uint8(sum/samples))
		}
	}
	return out, nil
}

func fingerprintSamplePoints(start, end int) []int {
	if end <= start {
		return []int{start}
	}
	span := end - start
	points := []int{
		start + span/4,
		start + span/2,
		start + (span*3)/4,
	}
	for i := range points {
		if points[i] >= end {
			points[i] = end - 1
		}
		if points[i] < start {
			points[i] = start
		}
	}
	return points
}

func fingerprintDistance(first, second []uint8) float64 {
	if len(first) == 0 || len(first) != len(second) {
		return 0
	}
	var total float64
	for i := range first {
		diff := int(first[i]) - int(second[i])
		if diff < 0 {
			diff = -diff
		}
		total += float64(diff) / 255.0
	}
	return total / float64(len(first))
}

func digestFingerprint(values []uint8) string {
	if len(values) == 0 {
		return ""
	}
	limit := 12
	if len(values) < limit {
		limit = len(values)
	}
	parts := make([]string, 0, limit)
	for _, value := range values[:limit] {
		parts = append(parts, fmt.Sprintf("%02x", value))
	}
	return strings.Join(parts, "")
}

func douyinOCRSignature(matches []map[string]any) []string {
	seen := make(map[string]struct{})
	out := make([]string, 0, len(matches))
	for _, match := range matches {
		if ocrMatchConfidence(match) < 0.45 {
			continue
		}
		y := intValue(match["y"])
		if y < 160 || y > 860 {
			continue
		}
		text := normalizeOCRSignatureText(normalizeString(match["text"]))
		if text == "" || isDouyinChromeText(text) {
			continue
		}
		if _, ok := seen[text]; ok {
			continue
		}
		seen[text] = struct{}{}
		out = append(out, text)
	}
	sort.Strings(out)
	return out
}

func normalizeOCRSignatureText(text string) string {
	text = strings.TrimSpace(text)
	if text == "" {
		return ""
	}
	replacer := strings.NewReplacer(
		" ", "",
		"\n", "",
		"\t", "",
		"“", "",
		"”", "",
		"\"", "",
		"'", "",
		"‘", "",
		"’", "",
	)
	text = replacer.Replace(text)
	if len([]rune(text)) < 3 {
		return ""
	}
	return text
}

func isDouyinChromeText(text string) bool {
	if text == "" {
		return true
	}
	return containsAnyNormalized(text,
		"搜索", "搜索你感兴趣的内容",
		"抖音", "抖音精选",
		"首页", "精选", "朋友", "消息", "我",
		"综合", "视频", "图文", "用户", "音乐", "直播", "商品",
	)
}

func diffOCRSignatures(before, after []string) ([]string, []string, float64) {
	beforeSet := make(map[string]struct{}, len(before))
	afterSet := make(map[string]struct{}, len(after))
	for _, item := range before {
		beforeSet[item] = struct{}{}
	}
	for _, item := range after {
		afterSet[item] = struct{}{}
	}
	added := make([]string, 0)
	removed := make([]string, 0)
	common := 0
	for _, item := range after {
		if _, ok := beforeSet[item]; !ok {
			added = append(added, item)
		}
	}
	for _, item := range before {
		if _, ok := afterSet[item]; !ok {
			removed = append(removed, item)
		} else {
			common++
		}
	}
	denominator := len(before)
	if len(after) > denominator {
		denominator = len(after)
	}
	overlap := 1.0
	if denominator > 0 {
		overlap = float64(common) / float64(denominator)
	}
	return added, removed, overlap
}

func extractPremiereTimecodes(matches []map[string]any) []string {
	seen := make(map[string]struct{})
	out := make([]string, 0, 4)
	for _, match := range matches {
		if ocrMatchConfidence(match) < 0.4 {
			continue
		}
		text := normalizeString(match["text"])
		if text == "" {
			continue
		}
		for _, current := range premiereTimecodePattern.FindAllString(text, -1) {
			current = strings.NewReplacer(";", ":", "：", ":").Replace(current)
			if _, ok := seen[current]; ok {
				continue
			}
			seen[current] = struct{}{}
			out = append(out, current)
		}
	}
	sort.Strings(out)
	return out
}

func timecodeMotionState(first, second []string) (bool, bool) {
	if len(first) == 0 || len(second) == 0 {
		return false, false
	}
	if len(first) != len(second) {
		return true, true
	}
	for index := range first {
		if first[index] != second[index] {
			return true, true
		}
	}
	return true, false
}

func visualMotionState(beforeDistance, afterDistance float64) (bool, bool, bool) {
	const motionThreshold = 0.018
	return beforeDistance > 0 || afterDistance > 0, beforeDistance >= motionThreshold, afterDistance >= motionThreshold
}

func truncateStrings(values []string, limit int) []string {
	if limit <= 0 || len(values) <= limit {
		return append([]string(nil), values...)
	}
	return append([]string(nil), values[:limit]...)
}

func selectDouyinSearchMatch(matches []map[string]any) (map[string]any, bool) {
	bestIndex := -1
	bestScore := -1.0
	for idx, match := range matches {
		score := ocrMatchConfidence(match)
		text := normalizeCommandText(normalizeString(match["text"]))
		x := intValue(match["x"])
		y := intValue(match["y"])
		width := intValue(match["width"])
		if containsAnyNormalized(text, "搜索你感兴趣的内容") {
			score += 5
		}
		if containsAnyNormalized(text, "搜索", "q搜索", "ai搜索") {
			score += 1
		}
		if y > 0 && y < 200 {
			score += 2
		}
		if x >= 400 && x <= 1400 {
			score += 1.5
		}
		if x < 300 {
			score -= 1
		}
		score += float64(width) / 120.0
		if score > bestScore {
			bestIndex = idx
			bestScore = score
		}
	}
	if bestIndex < 0 {
		return nil, false
	}
	return cloneMapAny(matches[bestIndex]), true
}

func selectTopmostOCRMatch(matches []map[string]any) (map[string]any, bool) {
	if len(matches) == 0 {
		return nil, false
	}
	sorted := make([]map[string]any, 0, len(matches))
	for _, match := range matches {
		sorted = append(sorted, cloneMapAny(match))
	}
	sort.SliceStable(sorted, func(i, j int) bool {
		yi := intValue(sorted[i]["y"])
		yj := intValue(sorted[j]["y"])
		if yi == yj {
			return intValue(sorted[i]["x"]) < intValue(sorted[j]["x"])
		}
		return yi < yj
	})
	return sorted[0], true
}

func ocrMatchConfidence(match map[string]any) float64 {
	switch value := match["confidence"].(type) {
	case float64:
		return value
	case float32:
		return float64(value)
	case int:
		return float64(value)
	default:
		return 0
	}
}

func stringInFold(value string, candidates ...string) bool {
	value = strings.TrimSpace(value)
	for _, candidate := range candidates {
		if strings.EqualFold(value, strings.TrimSpace(candidate)) {
			return true
		}
	}
	return false
}

func mergeEvidenceMaps(values ...map[string]any) map[string]any {
	out := make(map[string]any)
	for _, value := range values {
		for key, current := range value {
			out[key] = cloneValue(current)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func normalizeString(value any) string {
	typed, _ := value.(string)
	return strings.TrimSpace(typed)
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
