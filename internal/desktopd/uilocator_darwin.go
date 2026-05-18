//go:build darwin

package desktopd

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	desktoptypes "github.com/fulcrus/hopclaw/desktopapi/types"
)

// ---------------------------------------------------------------------------
// Constants
// ---------------------------------------------------------------------------

const (
	findElementDefaultMaxDepth   = 5
	findElementDefaultMaxResults = 10
	ocrTimeout                   = 30 * time.Second
	focusSettleDelay             = 150 * time.Millisecond
	inputSettleDelay             = 150 * time.Millisecond
)

// ---------------------------------------------------------------------------
// find_element — Accessibility tree search
// ---------------------------------------------------------------------------

func (s *darwinSession) handleFindElement(ctx context.Context, params map[string]any) (*desktoptypes.Response, error) {
	q := parseElementQuery(params, findElementDefaultMaxDepth, findElementDefaultMaxResults)
	matches, err := darwinFindElements(ctx, q)
	if err != nil {
		return nil, fmt.Errorf("find_element: %w", err)
	}
	return &desktoptypes.Response{OK: true, Data: elementMatchesData(matches)}, nil
}

func (s *darwinSession) handleClickElement(ctx context.Context, params map[string]any) (*desktoptypes.Response, error) {
	q := parseElementQuery(params, findElementDefaultMaxDepth, 1)
	match, err := darwinResolveSingleElement(ctx, q)
	if err != nil {
		return nil, fmt.Errorf("click_element: %w", err)
	}
	if err := ensureForeground(ctx, q.App, q.BundleID); err != nil {
		return nil, fmt.Errorf("click_element: focus: %w", err)
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

func (s *darwinSession) handleGetElementValue(ctx context.Context, params map[string]any) (*desktoptypes.Response, error) {
	q := parseElementQuery(params, findElementDefaultMaxDepth, 1)
	match, err := darwinResolveSingleElement(ctx, q)
	if err != nil {
		return nil, fmt.Errorf("get_element_value: %w", err)
	}
	return &desktoptypes.Response{
		OK: true,
		Data: map[string]any{
			"value": match.Value,
			"match": elementToMap(match),
		},
	}, nil
}

func (s *darwinSession) handleClearElement(ctx context.Context, params map[string]any) (*desktoptypes.Response, error) {
	resp, err := s.handleSetElementValue(ctx, cloneParamsWithValue(params, ""))
	if err != nil {
		return nil, fmt.Errorf("clear_element: %w", err)
	}
	resp.Data["cleared"] = true
	return resp, nil
}

func (s *darwinSession) handleSetElementValue(ctx context.Context, params map[string]any) (*desktoptypes.Response, error) {
	value := stringParam(params, "value")
	q := parseElementQuery(params, findElementDefaultMaxDepth, 1)
	match, err := darwinResolveSingleElement(ctx, q)
	if err != nil {
		return nil, fmt.Errorf("set_element_value: %w", err)
	}
	if err := ensureForeground(ctx, q.App, q.BundleID); err != nil {
		return nil, fmt.Errorf("set_element_value: focus: %w", err)
	}
	if _, err := s.handleMouseClick(ctx, map[string]any{"x": match.X, "y": match.Y}); err != nil {
		return nil, fmt.Errorf("set_element_value: click: %w", err)
	}
	if _, err := s.handleHotkey(ctx, map[string]any{"combo": "command+a"}); err != nil {
		return nil, fmt.Errorf("set_element_value: select all: %w", err)
	}
	if _, err := s.handleHotkey(ctx, map[string]any{"combo": "delete"}); err != nil {
		return nil, fmt.Errorf("set_element_value: clear: %w", err)
	}
	if value != "" {
		if _, err := s.handleTypeText(ctx, map[string]any{"text": value}); err != nil {
			return nil, fmt.Errorf("set_element_value: type: %w", err)
		}
	}

	verify, err := darwinResolveSingleElement(ctx, elementQuery{
		App:        q.App,
		BundleID:   q.BundleID,
		Path:       match.Path,
		MaxDepth:   q.MaxDepth,
		MaxResults: 1,
	})
	if err != nil {
		return nil, fmt.Errorf("set_element_value: verify: %w", err)
	}
	verified := verify.Value == value
	if !verified {
		return nil, fmt.Errorf("set_element_value: verification failed: expected %q, got %q", value, verify.Value)
	}
	return &desktoptypes.Response{
		OK: true,
		Data: map[string]any{
			"set":      true,
			"verified": true,
			"value":    verify.Value,
			"match":    elementToMap(verify),
		},
	}, nil
}

func (s *darwinSession) handleAssertElement(ctx context.Context, params map[string]any) (*desktoptypes.Response, error) {
	q := parseElementQuery(params, findElementDefaultMaxDepth, 1)
	timeout := q.Timeout
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	deadline := time.Now().Add(timeout)
	var last desktopElement
	var haveMatch bool
	for {
		match, err := darwinResolveSingleElement(ctx, q)
		if err == nil {
			last = match
			haveMatch = true
			if elementValueVerified(match, q) {
				return &desktoptypes.Response{
					OK: true,
					Data: map[string]any{
						"passed": true,
						"match":  elementToMap(match),
					},
				}, nil
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

func darwinResolveSingleElement(ctx context.Context, q elementQuery) (desktopElement, error) {
	matches, err := darwinFindElements(ctx, q)
	if err != nil {
		return desktopElement{}, err
	}
	match, ok := selectElement(matches, q.MatchIndex)
	if !ok {
		return desktopElement{}, errors.New("element not found")
	}
	return match, nil
}

func darwinFindElements(ctx context.Context, q elementQuery) ([]desktopElement, error) {
	if q.App == "" && q.BundleID == "" {
		return nil, errors.New("requires params.app or params.bundle_id")
	}
	if q.App == "" && q.BundleID != "" {
		resolved, err := resolveApp(ctx, "", q.BundleID)
		if err != nil {
			return nil, err
		}
		q.App = resolved.Name
	}

	appJSON, _ := json.Marshal(q.App)
	pathJSON, _ := json.Marshal(strings.ToLower(q.Path))
	roleJSON, _ := json.Marshal(strings.ToLower(q.Role))
	textJSON, _ := json.Marshal(strings.ToLower(q.Text))
	containsJSON, _ := json.Marshal(strings.ToLower(q.Contains))
	script := fmt.Sprintf(`(() => {
  const se = Application("System Events");
  const proc = se.processes.byName(%s);
  const pathFilter = %s;
  const roleFilter = %s;
  const textFilter = %s;
  const containsFilter = %s;
  const maxDepth = %d;
  const maxResults = %d;
  const results = [];

  function lower(value) {
    return String(value || "").toLowerCase();
  }

  function pushMatch(elem, currentPath) {
    let role = "";
    let label = "";
    let description = "";
    let value = "";
    let pos = [0, 0];
    let size = [0, 0];
    try { role = elem.role() || ""; } catch (_) {}
    try { label = elem.name() || ""; } catch (_) {}
    try { description = elem.description() || ""; } catch (_) {}
    try { value = elem.value() || ""; } catch (_) {}
    try { pos = elem.position(); } catch (_) {}
    try { size = elem.size(); } catch (_) {}
    results.push({
      path: currentPath,
      role: role,
      label: label,
      description: description,
      value: String(value).substring(0, 200),
      position: Array.isArray(pos) ? pos.map(v => Number(v)) : [0, 0],
      size: Array.isArray(size) ? size.map(v => Number(v)) : [0, 0]
    });
  }

  function matches(elem, currentPath) {
    if (pathFilter) return lower(currentPath) === pathFilter;
    let role = "";
    let label = "";
    let description = "";
    let value = "";
    try { role = elem.role() || ""; } catch (_) {}
    try { label = elem.name() || ""; } catch (_) {}
    try { description = elem.description() || ""; } catch (_) {}
    try { value = elem.value() || ""; } catch (_) {}

    const roleText = lower(role);
    const labelText = lower(label);
    const descriptionText = lower(description);
    const valueText = lower(value);
    const roleOk = !roleFilter || roleText.indexOf(roleFilter) >= 0;
    const textOk = !textFilter || labelText === textFilter || descriptionText === textFilter || valueText === textFilter;
    const containsOk = !containsFilter || labelText.indexOf(containsFilter) >= 0 || descriptionText.indexOf(containsFilter) >= 0 || valueText.indexOf(containsFilter) >= 0;
    if (!roleFilter && !textFilter && !containsFilter) return true;
    return roleOk && textOk && containsOk;
  }

  function traverse(elem, depth, currentPath) {
    if (depth > maxDepth || results.length >= maxResults) return;
    try {
      if (matches(elem, currentPath)) pushMatch(elem, currentPath);
    } catch (_) {}
    try {
      const children = elem.uiElements();
      for (let i = 0; i < children.length && results.length < maxResults; i++) {
        traverse(children[i], depth + 1, currentPath + "/" + i);
      }
    } catch (_) {}
  }

  try {
    const wins = proc.windows();
    for (let w = 0; w < wins.length && results.length < maxResults; w++) {
      traverse(wins[w], 0, "w" + (w + 1));
    }
  } catch (_) {}
  return JSON.stringify({elements: results});
})()`, string(appJSON), string(pathJSON), string(roleJSON), string(textJSON), string(containsJSON), q.MaxDepth, q.MaxResults)

	var payload struct {
		Elements []desktopElement `json:"elements"`
	}
	if err := runJXAJSON(ctx, script, &payload); err != nil {
		return nil, err
	}
	for i := range payload.Elements {
		if len(payload.Elements[i].Size) >= 2 {
			payload.Elements[i].Width = payload.Elements[i].Size[0]
			payload.Elements[i].Height = payload.Elements[i].Size[1]
		}
		if len(payload.Elements[i].Position) >= 2 {
			payload.Elements[i].X = payload.Elements[i].Position[0] + payload.Elements[i].Width/2
			payload.Elements[i].Y = payload.Elements[i].Position[1] + payload.Elements[i].Height/2
		}
	}
	return payload.Elements, nil
}

// ---------------------------------------------------------------------------
// find_text — OCR-based text location (Vision.framework, local, offline)
// ---------------------------------------------------------------------------

// ocrSwiftScript is the inline Swift program that uses Vision.framework to
// perform OCR and return matching text bounding boxes as JSON.  It is
// compiled once and cached in a temp directory for the process lifetime.
const ocrSwiftScript = `
import Vision
import AppKit
import Foundation

guard CommandLine.arguments.count >= 3 else {
    fputs("{\"error\":\"usage: ocr <image> <text>\"}\n", stderr); exit(1)
}

let imagePath = CommandLine.arguments[1]
let searchText = CommandLine.arguments[2].lowercased()

guard let image = NSImage(contentsOfFile: imagePath),
      let cgImage = image.cgImage(forProposedRect: nil, context: nil, hints: nil) else {
    fputs("{\"error\":\"cannot load image\"}\n", stderr); exit(1)
}

let imageWidth = Double(cgImage.width)
let imageHeight = Double(cgImage.height)

let request = VNRecognizeTextRequest()
request.recognitionLevel = .accurate
request.recognitionLanguages = ["zh-Hans", "zh-Hant", "en", "ja", "ko"]
request.usesLanguageCorrection = true

let handler = VNImageRequestHandler(cgImage: cgImage, options: [:])
do { try handler.perform([request]) } catch {
    fputs("{\"error\":\"\(error.localizedDescription)\"}\n", stderr); exit(1)
}

struct Match: Encodable {
    let text: String
    let pixel_x: Int; let pixel_y: Int
    let pixel_w: Int; let pixel_h: Int
    let center_x: Int; let center_y: Int
    let confidence: Double
}

struct Result: Encodable {
    let image_width: Int; let image_height: Int
    let matches: [Match]; let match_count: Int
}

var matches: [Match] = []
if let observations = request.results {
    for obs in observations {
        guard let candidate = obs.topCandidates(1).first else { continue }
        let text = candidate.string
        if text.lowercased().contains(searchText) {
            let box = obs.boundingBox
            let px = Int(box.origin.x * imageWidth)
            let py = Int((1.0 - box.origin.y - box.height) * imageHeight)
            let pw = Int(box.width * imageWidth)
            let ph = Int(box.height * imageHeight)
            matches.append(Match(
                text: text,
                pixel_x: px, pixel_y: py, pixel_w: pw, pixel_h: ph,
                center_x: px + pw / 2, center_y: py + ph / 2,
                confidence: Double(candidate.confidence)
            ))
        }
    }
}

let result = Result(image_width: Int(imageWidth), image_height: Int(imageHeight),
                    matches: matches, match_count: matches.count)
let data = try! JSONEncoder().encode(result)
print(String(data: data, encoding: .utf8)!)
`

var (
	ocrBinaryPath string
	ocrBinaryMu   sync.Mutex
)

// ensureOCRBinary compiles the Swift OCR helper once and caches the binary.
func ensureOCRBinary() (string, error) {
	ocrBinaryMu.Lock()
	defer ocrBinaryMu.Unlock()

	if ocrBinaryPath != "" {
		if _, err := os.Stat(ocrBinaryPath); err == nil {
			return ocrBinaryPath, nil
		}
	}

	cacheRoot, err := os.UserCacheDir()
	if err != nil || strings.TrimSpace(cacheRoot) == "" {
		cacheRoot = os.TempDir()
	}
	sum := fmt.Sprintf("%x", sha256.Sum256([]byte(ocrSwiftScript)))
	dir := filepath.Join(cacheRoot, "hopclaw", "ocr", sum[:16])
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("create ocr cache dir: %w", err)
	}

	srcPath := filepath.Join(dir, "ocr.swift")
	binPath := filepath.Join(dir, "ocr")
	if _, err := os.Stat(binPath); err == nil {
		ocrBinaryPath = binPath
		return binPath, nil
	}

	tmpDir, err := os.MkdirTemp("", "hopclaw-ocr-build-*")
	if err != nil {
		return "", fmt.Errorf("create ocr temp dir: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	if err := os.WriteFile(srcPath, []byte(ocrSwiftScript), 0o644); err != nil {
		return "", fmt.Errorf("write ocr source: %w", err)
	}

	tmpBinPath := filepath.Join(tmpDir, "ocr")
	cmd := exec.Command("swiftc", "-O", srcPath, "-o", tmpBinPath)
	if out, err := cmd.CombinedOutput(); err != nil {
		return "", fmt.Errorf("compile ocr helper: %w: %s", err, strings.TrimSpace(string(out)))
	}
	if err := os.Rename(tmpBinPath, binPath); err != nil {
		if _, statErr := os.Stat(binPath); statErr != nil {
			return "", fmt.Errorf("install ocr helper: %w", err)
		}
	}

	ocrBinaryPath = binPath
	return binPath, nil
}

func runOCRBinary(ctx context.Context, imagePath, searchText string) (ocrResult, error) {
	ocrBin, err := ensureOCRBinary()
	if err != nil {
		return ocrResult{}, err
	}
	ocrCtx, cancel := context.WithTimeout(ctx, ocrTimeout)
	defer cancel()

	ocrCmd := exec.CommandContext(ocrCtx, ocrBin, imagePath, searchText)
	ocrOut, err := ocrCmd.Output()
	if err != nil {
		return ocrResult{}, err
	}

	var result ocrResult
	if err := json.Unmarshal(ocrOut, &result); err != nil {
		return ocrResult{}, fmt.Errorf("decode ocr: %w", err)
	}
	return result, nil
}

// ensureForeground brings the target app to the front and waits for it to
// settle.  This is critical for accurate screenshots — without it, the
// calling terminal may still be visible.
func ensureForeground(ctx context.Context, app, bundleID string) error {
	switch {
	case bundleID != "":
		if err := runAppleScript(ctx, fmt.Sprintf(
			`tell application id "%s" to activate`, escapeAppleScript(bundleID))); err != nil {
			return err
		}
	case app != "":
		if err := runAppleScript(ctx, fmt.Sprintf(
			`tell application "%s" to activate`, escapeAppleScript(app))); err != nil {
			return err
		}
	default:
		return nil // no app specified, screenshot whatever is on screen
	}
	state, _, _, err := waitForAppReadyWithWindowRecovery(ctx, app, bundleID, desktopWaitInteractive, desktopReadyDefaultWait)
	if err != nil {
		if state.ReadyState != desktopReadyFocused {
			return err
		}
	}
	if state.ReadyState == "" {
		state, _, err = waitForAppReady(ctx, app, bundleID, desktopWaitFocused, desktopReadyDefaultWait)
		if err != nil {
			return err
		}
	}
	if state.ReadyState != desktopReadyFocused && state.ReadyState != desktopReadyInteractive {
		return fmt.Errorf("app %q foreground activation settled at unsupported ready_state=%q", firstNonEmpty(bundleID, app), state.ReadyState)
	}
	time.Sleep(focusSettleDelay)
	return nil
}

type ocrResult struct {
	ImageWidth  int        `json:"image_width"`
	ImageHeight int        `json:"image_height"`
	Matches     []ocrMatch `json:"matches"`
	MatchCount  int        `json:"match_count"`
}

type ocrMatch struct {
	Text       string  `json:"text"`
	PixelX     int     `json:"pixel_x"`
	PixelY     int     `json:"pixel_y"`
	PixelW     int     `json:"pixel_w"`
	PixelH     int     `json:"pixel_h"`
	CenterX    int     `json:"center_x"`
	CenterY    int     `json:"center_y"`
	Confidence float64 `json:"confidence"`
}

type ocrCaptureRegion struct {
	X      int
	Y      int
	Width  int
	Height int
}

func regionFromWindow(window desktopWindowSnapshot) (ocrCaptureRegion, bool) {
	if len(window.Position) < 2 || len(window.Size) < 2 {
		return ocrCaptureRegion{}, false
	}
	width := window.Size[0]
	height := window.Size[1]
	if width <= 0 || height <= 0 {
		return ocrCaptureRegion{}, false
	}
	return ocrCaptureRegion{
		X:      window.Position[0],
		Y:      window.Position[1],
		Width:  width,
		Height: height,
	}, true
}

func regionFromApp(app desktopAppSnapshot) (ocrCaptureRegion, bool) {
	for _, window := range app.Windows {
		if region, ok := regionFromWindow(window); ok {
			return region, true
		}
	}
	return ocrCaptureRegion{}, false
}

func matchToScreenMap(match ocrMatch, scale float64, region ocrCaptureRegion, scoped bool) map[string]any {
	if scale <= 0 {
		scale = 1.0
	}
	x := int(float64(match.CenterX) / scale)
	y := int(float64(match.CenterY) / scale)
	if scoped {
		x += region.X
		y += region.Y
	}
	return map[string]any{
		"text":       match.Text,
		"x":          x,
		"y":          y,
		"width":      int(float64(match.PixelW) / scale),
		"height":     int(float64(match.PixelH) / scale),
		"confidence": match.Confidence,
	}
}

func (s *darwinSession) handleFindText(ctx context.Context, params map[string]any) (*desktoptypes.Response, error) {
	text := stringParam(params, "text")
	if text == "" {
		return nil, errors.New("find_text requires params.text")
	}
	app := stringParam(params, "app")
	bundleID := stringParam(params, "bundle_id")

	if err := ensureScreenCapturePermission(ctx); err != nil {
		return nil, err
	}

	// Atomic: focus → wait → screenshot → OCR
	if leaseTargetFromParams(params) != nil {
		if _, err := s.ensureFocusLease(ctx, params); err != nil {
			return nil, fmt.Errorf("find_text: focus: %w", err)
		}
	} else if app != "" || bundleID != "" {
		if err := ensureForeground(ctx, app, bundleID); err != nil {
			return nil, fmt.Errorf("find_text: focus: %w", err)
		}
	}

	region := ocrCaptureRegion{}
	scopedToWindow := false
	captureMode := "screen"
	evidence := map[string]any{"text": text}

	// Take screenshot.
	tmpDir, err := os.MkdirTemp("", "hopclaw-ocr-screenshot-*")
	if err != nil {
		return nil, fmt.Errorf("find_text: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	imgPath := filepath.Join(tmpDir, "screen.png")
	if wantsTargetedScreenshot(params) {
		targetApp, targetWindow, err := resolveScreenshotTarget(ctx, params)
		if err != nil {
			return nil, fmt.Errorf("find_text: screenshot target: %w", err)
		}
		capture, err := captureTargetedPNG(ctx, targetApp, targetWindow, imgPath)
		if err != nil {
			return nil, fmt.Errorf("find_text: screenshot: %w", err)
		}
		captureMode = capture.CaptureMode
		evidence["app"] = targetApp.Name
		evidence["bundle_id"] = targetApp.BundleID
		evidence["title"] = targetWindow.Title
		evidence["window_index"] = targetWindow.Index
		evidence["capture_mode"] = capture.CaptureMode
		for key, value := range capture.Evidence {
			evidence[key] = value
		}
		if len(capture.Bounds) >= 4 {
			region = ocrCaptureRegion{
				X:      capture.Bounds[0],
				Y:      capture.Bounds[1],
				Width:  capture.Bounds[2],
				Height: capture.Bounds[3],
			}
			scopedToWindow = true
		}
		s.setVisibilityLease(targetApp.Name, targetApp.BundleID, firstNonEmpty(targetWindow.Title, stringParam(params, "title_contains")), targetWindow.Index)
	} else {
		if err := captureFullScreenPNG(ctx, imgPath); err != nil {
			return nil, fmt.Errorf("find_text: screenshot: %w", err)
		}
	}

	result, err := runOCRBinary(ctx, imgPath, text)
	if err != nil {
		return nil, fmt.Errorf("find_text: ocr: %w", err)
	}

	// Convert pixel coordinates to logical coordinates (divide by Retina scale).
	logicalWidth := 1800 // fallback
	if scopedToWindow {
		logicalWidth = region.Width
	} else {
		out, _ := runAppleScriptString(ctx,
			`tell application "Finder" to get bounds of window of desktop`)
		if parts := strings.Split(strings.ReplaceAll(out, " ", ""), ","); len(parts) >= 3 {
			if w := parseInt(parts[2]); w > 0 {
				logicalWidth = w
			}
		}
	}

	scale := 1.0
	if result.ImageWidth > 0 && logicalWidth > 0 {
		scale = float64(result.ImageWidth) / float64(logicalWidth)
	}

	matches := make([]map[string]any, 0, len(result.Matches))
	for _, m := range result.Matches {
		matches = append(matches, matchToScreenMap(m, scale, region, scopedToWindow))
	}

	scope := "screen"
	if scopedToWindow {
		scope = "app_window"
	}
	data := map[string]any{
		"matches":      matches,
		"match_count":  len(matches),
		"scale":        scale,
		"scope":        scope,
		"capture_mode": captureMode,
	}
	data = annotateActionResult(data, actionStatusVerified, "", "ocr_lookup", confidenceForCaptureMode(captureMode), evidence)
	return &desktoptypes.Response{OK: true, Data: data}, nil
}

// ---------------------------------------------------------------------------
// click_text — Atomic: focus → screenshot → OCR → click center of first match
// ---------------------------------------------------------------------------

func (s *darwinSession) handleClickText(ctx context.Context, params map[string]any) (*desktoptypes.Response, error) {
	text := stringParam(params, "text")
	if text == "" {
		return nil, errors.New("click_text requires params.text")
	}

	// Use find_text to locate the text.
	resp, err := s.handleFindText(ctx, params)
	if err != nil {
		return nil, fmt.Errorf("click_text: %w", err)
	}

	matchesRaw, _ := resp.Data["matches"].([]map[string]any)
	if len(matchesRaw) == 0 {
		return nil, fmt.Errorf("click_text: text %q not found on screen", text)
	}

	// Pick the best match (highest confidence, or by index).
	idx := intParam(params, "match_index")
	if idx < 0 || idx >= len(matchesRaw) {
		idx = 0
	}
	match := matchesRaw[idx]

	x := intValue(match["x"])
	y := intValue(match["y"])

	// Re-focus the app (find_text already focused, but ensure it's still front).
	app := stringParam(params, "app")
	bundleID := stringParam(params, "bundle_id")
	if leaseTargetFromParams(params) != nil {
		if _, err := s.ensureFocusLease(ctx, params); err != nil {
			return nil, fmt.Errorf("click_text: re-focus: %w", err)
		}
	} else if app != "" || bundleID != "" {
		if err := ensureForeground(ctx, app, bundleID); err != nil {
			return nil, fmt.Errorf("click_text: re-focus: %w", err)
		}
	}

	// Click at the center of the matched text.
	clickResp, err := s.handleMouseClick(ctx, map[string]any{
		"x": x,
		"y": y,
	})
	if err != nil {
		return nil, fmt.Errorf("click_text: click: %w", err)
	}

	return &desktoptypes.Response{
		OK: true,
		Data: annotateActionResult(map[string]any{
			"text":       text,
			"match":      match,
			"clicked":    true,
			"clicked_at": []int{x, y},
			"click_data": clickResp.Data,
		}, actionStatusAttempted, "mouse", "ocr_click", confidenceFromOCRMatch(match), map[string]any{
			"match_index":  idx,
			"capture_mode": resp.Data["capture_mode"],
		}),
	}, nil
}

func confidenceFromOCRMatch(match map[string]any) float64 {
	switch value := match["confidence"].(type) {
	case float64:
		return value
	case float32:
		return float64(value)
	case int:
		return float64(value)
	default:
		return 0.85
	}
}

// parseInt parses a decimal integer, returning 0 on error.
func parseInt(s string) int {
	s = strings.TrimSpace(s)
	n := 0
	for _, c := range s {
		if c < '0' || c > '9' {
			break
		}
		n = n*10 + int(c-'0')
	}
	return n
}
