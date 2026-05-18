//go:build linux

package desktopd

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	desktoptypes "github.com/fulcrus/hopclaw/desktopapi/types"
)

const (
	linuxFindElementMaxDepth   = 5
	linuxFindElementMaxResults = 10
	linuxOCRTimeout            = 30 * time.Second
	linuxFocusSettleDelay      = 200 * time.Millisecond
)

// ---------------------------------------------------------------------------
// find_element — AT-SPI2 via python3+pyatspi (GNOME accessibility)
// ---------------------------------------------------------------------------

func (s *linuxSession) handleFindElement(ctx context.Context, params map[string]any) (*desktoptypes.Response, error) {
	q := parseElementQuery(params, linuxFindElementMaxDepth, linuxFindElementMaxResults)
	matches, err := linuxFindElements(ctx, q)
	if err != nil {
		return nil, fmt.Errorf("find_element: %w", err)
	}
	return &desktoptypes.Response{OK: true, Data: elementMatchesData(matches)}, nil
}

func (s *linuxSession) handleClickElement(ctx context.Context, params map[string]any) (*desktoptypes.Response, error) {
	q := parseElementQuery(params, linuxFindElementMaxDepth, 1)
	match, err := linuxResolveSingleElement(ctx, q)
	if err != nil {
		return nil, fmt.Errorf("click_element: %w", err)
	}
	if q.App != "" {
		linuxEnsureForeground(ctx, q.App)
	}
	clickResp, err := s.handleMouseClick(ctx, map[string]any{"x": match.X, "y": match.Y})
	if err != nil {
		return nil, fmt.Errorf("click_element: click: %w", err)
	}
	return &desktoptypes.Response{
		OK: true,
		Data: map[string]any{
			"clicked":    true,
			"match":      elementToMap(match),
			"click_data": clickResp.Data,
		},
	}, nil
}

func (s *linuxSession) handleGetElementValue(ctx context.Context, params map[string]any) (*desktoptypes.Response, error) {
	q := parseElementQuery(params, linuxFindElementMaxDepth, 1)
	match, err := linuxResolveSingleElement(ctx, q)
	if err != nil {
		return nil, fmt.Errorf("get_element_value: %w", err)
	}
	return &desktoptypes.Response{OK: true, Data: map[string]any{"value": match.Value, "match": elementToMap(match)}}, nil
}

func (s *linuxSession) handleClearElement(ctx context.Context, params map[string]any) (*desktoptypes.Response, error) {
	resp, err := s.handleSetElementValue(ctx, cloneParamsWithValue(params, ""))
	if err != nil {
		return nil, fmt.Errorf("clear_element: %w", err)
	}
	resp.Data["cleared"] = true
	return resp, nil
}

func (s *linuxSession) handleSetElementValue(ctx context.Context, params map[string]any) (*desktoptypes.Response, error) {
	value := stringParam(params, "value")
	q := parseElementQuery(params, linuxFindElementMaxDepth, 1)
	match, err := linuxResolveSingleElement(ctx, q)
	if err != nil {
		return nil, fmt.Errorf("set_element_value: %w", err)
	}
	if q.App != "" {
		linuxEnsureForeground(ctx, q.App)
	}
	if _, err := s.handleMouseClick(ctx, map[string]any{"x": match.X, "y": match.Y}); err != nil {
		return nil, fmt.Errorf("set_element_value: click: %w", err)
	}
	if _, err := s.handleHotkey(ctx, map[string]any{"combo": "ctrl+a"}); err != nil {
		return nil, fmt.Errorf("set_element_value: select all: %w", err)
	}
	if _, err := s.handleHotkey(ctx, map[string]any{"combo": "BackSpace"}); err != nil {
		return nil, fmt.Errorf("set_element_value: clear: %w", err)
	}
	if value != "" {
		if _, err := s.handleTypeText(ctx, map[string]any{"text": value}); err != nil {
			return nil, fmt.Errorf("set_element_value: type: %w", err)
		}
	}
	verify, err := linuxResolveSingleElement(ctx, elementQuery{App: q.App, Path: match.Path, MaxDepth: q.MaxDepth, MaxResults: 1})
	if err != nil {
		return nil, fmt.Errorf("set_element_value: verify: %w", err)
	}
	if verify.Value != value && value != "" {
		return nil, fmt.Errorf("set_element_value: verification failed: expected %q, got %q", value, verify.Value)
	}
	return &desktoptypes.Response{OK: true, Data: map[string]any{"set": true, "verified": verify.Value == value || value == "", "value": verify.Value, "match": elementToMap(verify)}}, nil
}

func (s *linuxSession) handleAssertElement(ctx context.Context, params map[string]any) (*desktoptypes.Response, error) {
	q := parseElementQuery(params, linuxFindElementMaxDepth, 1)
	timeout := q.Timeout
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	deadline := time.Now().Add(timeout)
	var last desktopElement
	var haveMatch bool
	for {
		match, err := linuxResolveSingleElement(ctx, q)
		if err == nil {
			last = match
			haveMatch = true
			if elementValueVerified(match, q) {
				return &desktoptypes.Response{OK: true, Data: map[string]any{"passed": true, "match": elementToMap(match)}}, nil
			}
		}
		if time.Now().After(deadline) {
			data := map[string]any{"passed": false}
			if haveMatch {
				data["match"] = elementToMap(last)
			}
			return &desktoptypes.Response{OK: true, Data: data}, nil
		}
		time.Sleep(250 * time.Millisecond)
	}
}

func linuxResolveSingleElement(ctx context.Context, q elementQuery) (desktopElement, error) {
	matches, err := linuxFindElements(ctx, q)
	if err != nil {
		return desktopElement{}, err
	}
	match, ok := selectElement(matches, q.MatchIndex)
	if !ok {
		return desktopElement{}, errors.New("element not found")
	}
	return match, nil
}

func linuxFindElements(ctx context.Context, q elementQuery) ([]desktopElement, error) {
	if q.App == "" {
		return nil, errors.New("requires params.app")
	}
	pyScript := fmt.Sprintf(`
import json, pyatspi

app_name = %q.lower()
path_filter = %q.lower()
role_filter = %q.lower()
text_filter = %q.lower()
contains_filter = %q.lower()
max_depth = %d
max_results = %d
results = []

def get_value(child):
    try:
        return child.queryText().getText(0, -1) or ""
    except:
        return child.name or ""

def matches(path, role, label, desc, value):
    if path_filter:
        return path.lower() == path_filter
    role_l = (role or "").lower()
    label_l = (label or "").lower()
    desc_l = (desc or "").lower()
    value_l = (value or "").lower()
    if role_filter and role_filter not in role_l:
        return False
    if text_filter and text_filter not in (label_l, desc_l, value_l):
        return False
    if contains_filter and contains_filter not in label_l and contains_filter not in desc_l and contains_filter not in value_l:
        return False
    if not role_filter and not text_filter and not contains_filter:
        return True
    return True

def traverse(obj, depth, path):
    if depth > max_depth or len(results) >= max_results:
        return
    try:
        role = obj.getRoleName()
        label = obj.name or ""
        desc = obj.description or ""
        value = get_value(obj)
        pos = [0, 0]
        size = [0, 0]
        try:
            comp = obj.queryComponent()
            ext = comp.getExtents(pyatspi.DESKTOP_COORDS)
            pos = [ext.x, ext.y]
            size = [ext.width, ext.height]
        except:
            pass
        if matches(path, role, label, desc, value):
            results.append({
                "path": path,
                "role": role,
                "label": label,
                "description": desc,
                "value": value,
                "position": pos,
                "size": size,
                "x": int(pos[0] + size[0] / 2),
                "y": int(pos[1] + size[1] / 2),
                "width": int(size[0]),
                "height": int(size[1]),
            })
    except:
        pass
    try:
        for i in range(obj.childCount):
            if len(results) >= max_results:
                return
            try:
                traverse(obj.getChildAtIndex(i), depth + 1, path + "/" + str(i))
            except:
                pass
    except:
        pass

desktop = pyatspi.Registry.getDesktop(0)
for i in range(desktop.childCount):
    try:
        app = desktop.getChildAtIndex(i)
        if app_name not in (app.name or "").lower():
            continue
        traverse(app, 0, "a" + str(i))
    except:
        pass

print(json.dumps({"elements": results}))
`, q.App, q.Path, q.Role, q.Text, q.Contains, q.MaxDepth, q.MaxResults)

	cmd := exec.CommandContext(ctx, "python3", "-c", pyScript)
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("requires python3-pyatspi: %w", err)
	}
	var payload struct {
		Elements []desktopElement `json:"elements"`
	}
	if err := json.Unmarshal(out, &payload); err != nil {
		return nil, fmt.Errorf("decode: %w", err)
	}
	return payload.Elements, nil
}

// ---------------------------------------------------------------------------
// find_text — Tesseract OCR (TSV output with bounding boxes)
// ---------------------------------------------------------------------------

func (s *linuxSession) handleFindText(ctx context.Context, params map[string]any) (*desktoptypes.Response, error) {
	text := stringParam(params, "text")
	if text == "" {
		return nil, errors.New("find_text requires params.text")
	}
	app := stringParam(params, "app")

	// Check tesseract is available.
	if _, err := exec.LookPath("tesseract"); err != nil {
		return nil, fmt.Errorf("find_text: tesseract not found; install with: sudo apt install tesseract-ocr tesseract-ocr-chi-sim")
	}

	// Focus app.
	if app != "" {
		linuxEnsureForeground(ctx, app)
	}

	// Screenshot.
	tmpDir, err := os.MkdirTemp("", "hopclaw-ocr-*")
	if err != nil {
		return nil, fmt.Errorf("find_text: %w", err)
	}
	defer os.RemoveAll(tmpDir)
	imgPath := filepath.Join(tmpDir, "screen.png")

	// Try gnome-screenshot, scrot, or import (ImageMagick).
	screenshotted := false
	for _, args := range [][]string{
		{"gnome-screenshot", "-f", imgPath},
		{"scrot", imgPath},
		{"import", "-window", "root", imgPath},
	} {
		if _, lookErr := exec.LookPath(args[0]); lookErr == nil {
			if err := exec.CommandContext(ctx, args[0], args[1:]...).Run(); err == nil {
				screenshotted = true
				break
			}
		}
	}
	if !screenshotted {
		return nil, fmt.Errorf("find_text: no screenshot tool found (tried gnome-screenshot, scrot, import)")
	}

	// Run tesseract with TSV output.
	ocrCtx, cancel := context.WithTimeout(ctx, linuxOCRTimeout)
	defer cancel()

	tsvCmd := exec.CommandContext(ocrCtx, "tesseract", imgPath, "stdout", "--oem", "3", "--psm", "3", "tsv")
	tsvOut, err := tsvCmd.Output()
	if err != nil {
		return nil, fmt.Errorf("find_text: tesseract: %w", err)
	}

	// Parse TSV output and find matching text.
	searchLower := strings.ToLower(text)
	var matches []map[string]any
	lines := strings.Split(string(tsvOut), "\n")
	for _, line := range lines[1:] { // skip header
		fields := strings.Split(line, "\t")
		if len(fields) < 12 {
			continue
		}
		word := strings.TrimSpace(fields[11])
		if word == "" {
			continue
		}
		if strings.Contains(strings.ToLower(word), searchLower) {
			x, _ := strconv.Atoi(fields[6])
			y, _ := strconv.Atoi(fields[7])
			w, _ := strconv.Atoi(fields[8])
			h, _ := strconv.Atoi(fields[9])
			conf, _ := strconv.ParseFloat(fields[10], 64)
			matches = append(matches, map[string]any{
				"text":       word,
				"x":          x + w/2,
				"y":          y + h/2,
				"width":      w,
				"height":     h,
				"confidence": conf / 100.0,
			})
		}
	}

	return &desktoptypes.Response{
		OK: true,
		Data: map[string]any{
			"matches":     matches,
			"match_count": len(matches),
			"scale":       1.0, // Linux typically 1x scale
		},
	}, nil
}

// ---------------------------------------------------------------------------
// click_text — Atomic find + click
// ---------------------------------------------------------------------------

func (s *linuxSession) handleClickText(ctx context.Context, params map[string]any) (*desktoptypes.Response, error) {
	text := stringParam(params, "text")
	if text == "" {
		return nil, errors.New("click_text requires params.text")
	}

	resp, err := s.handleFindText(ctx, params)
	if err != nil {
		return nil, fmt.Errorf("click_text: %w", err)
	}

	matchesRaw, _ := resp.Data["matches"].([]map[string]any)
	if len(matchesRaw) == 0 {
		return nil, fmt.Errorf("click_text: text %q not found on screen", text)
	}

	match := matchesRaw[0]
	x, _ := match["x"].(int)
	y, _ := match["y"].(int)

	clickResp, err := s.handleMouseClick(ctx, map[string]any{"x": x, "y": y})
	if err != nil {
		return nil, fmt.Errorf("click_text: click: %w", err)
	}

	return &desktoptypes.Response{
		OK: true,
		Data: map[string]any{
			"text":       text,
			"match":      match,
			"clicked":    true,
			"clicked_at": []int{x, y},
			"click_data": clickResp.Data,
		},
	}, nil
}

func linuxEnsureForeground(ctx context.Context, app string) {
	// Try wmctrl first (X11), then xdotool.
	if _, err := exec.LookPath("wmctrl"); err == nil {
		exec.CommandContext(ctx, "wmctrl", "-a", app).Run()
	} else if _, err := exec.LookPath("xdotool"); err == nil {
		exec.CommandContext(ctx, "xdotool", "search", "--name", app, "windowactivate").Run()
	}
	time.Sleep(linuxFocusSettleDelay)
}
