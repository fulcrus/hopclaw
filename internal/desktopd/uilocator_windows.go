//go:build windows

package desktopd

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	desktoptypes "github.com/fulcrus/hopclaw/desktopapi/types"
)

const (
	winFindElementDefaultMaxDepth   = 5
	winFindElementDefaultMaxResults = 10
	winOCRTimeout                   = 30 * time.Second
	winFocusSettleDelay             = 200 * time.Millisecond
)

// ---------------------------------------------------------------------------
// find_element — Windows UI Automation
// ---------------------------------------------------------------------------

func (s *windowsSession) handleFindElement(ctx context.Context, params map[string]any) (*desktoptypes.Response, error) {
	q := parseElementQuery(params, winFindElementDefaultMaxDepth, winFindElementDefaultMaxResults)
	matches, err := windowsFindElements(ctx, q)
	if err != nil {
		return nil, fmt.Errorf("find_element: %w", err)
	}
	return &desktoptypes.Response{OK: true, Data: elementMatchesData(matches)}, nil
}

func (s *windowsSession) handleClickElement(ctx context.Context, params map[string]any) (*desktoptypes.Response, error) {
	q := parseElementQuery(params, winFindElementDefaultMaxDepth, 1)
	match, err := windowsResolveSingleElement(ctx, q)
	if err != nil {
		return nil, fmt.Errorf("click_element: %w", err)
	}
	if q.App != "" {
		winEnsureForeground(ctx, q.App)
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

func (s *windowsSession) handleGetElementValue(ctx context.Context, params map[string]any) (*desktoptypes.Response, error) {
	q := parseElementQuery(params, winFindElementDefaultMaxDepth, 1)
	match, err := windowsResolveSingleElement(ctx, q)
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

func (s *windowsSession) handleClearElement(ctx context.Context, params map[string]any) (*desktoptypes.Response, error) {
	resp, err := s.handleSetElementValue(ctx, cloneParamsWithValue(params, ""))
	if err != nil {
		return nil, fmt.Errorf("clear_element: %w", err)
	}
	resp.Data["cleared"] = true
	return resp, nil
}

func (s *windowsSession) handleSetElementValue(ctx context.Context, params map[string]any) (*desktoptypes.Response, error) {
	value := stringParam(params, "value")
	q := parseElementQuery(params, winFindElementDefaultMaxDepth, 1)
	match, err := windowsResolveSingleElement(ctx, q)
	if err != nil {
		return nil, fmt.Errorf("set_element_value: %w", err)
	}
	if q.App != "" {
		winEnsureForeground(ctx, q.App)
	}
	if ok, err := windowsSetElementValue(ctx, q.App, match.Path, value); err == nil && ok {
		return s.windowsVerifyElementValue(ctx, q, match.Path, value)
	}
	if _, err := s.handleMouseClick(ctx, map[string]any{"x": match.X, "y": match.Y}); err != nil {
		return nil, fmt.Errorf("set_element_value: click: %w", err)
	}
	if _, err := s.handleHotkey(ctx, map[string]any{"combo": "ctrl+a"}); err != nil {
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
	return s.windowsVerifyElementValue(ctx, q, match.Path, value)
}

func (s *windowsSession) handleAssertElement(ctx context.Context, params map[string]any) (*desktoptypes.Response, error) {
	q := parseElementQuery(params, winFindElementDefaultMaxDepth, 1)
	timeout := q.Timeout
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	deadline := time.Now().Add(timeout)
	var last desktopElement
	var haveMatch bool
	for {
		match, err := windowsResolveSingleElement(ctx, q)
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

func (s *windowsSession) windowsVerifyElementValue(ctx context.Context, q elementQuery, path, value string) (*desktoptypes.Response, error) {
	verify, err := windowsResolveSingleElement(ctx, elementQuery{
		App:        q.App,
		Path:       path,
		MaxDepth:   q.MaxDepth,
		MaxResults: 1,
	})
	if err != nil {
		return nil, fmt.Errorf("verify: %w", err)
	}
	if verify.Value != value {
		return nil, fmt.Errorf("verification failed: expected %q, got %q", value, verify.Value)
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

func windowsResolveSingleElement(ctx context.Context, q elementQuery) (desktopElement, error) {
	matches, err := windowsFindElements(ctx, q)
	if err != nil {
		return desktopElement{}, err
	}
	match, ok := selectElement(matches, q.MatchIndex)
	if !ok {
		return desktopElement{}, errors.New("element not found")
	}
	return match, nil
}

func windowsFindElements(ctx context.Context, q elementQuery) ([]desktopElement, error) {
	if q.App == "" {
		return nil, errors.New("requires params.app")
	}
	ps := fmt.Sprintf(`
Add-Type -AssemblyName UIAutomationClient
Add-Type -AssemblyName UIAutomationTypes

$root = [System.Windows.Automation.AutomationElement]::RootElement
$cond = [System.Windows.Automation.Condition]::TrueCondition
$appFilter = '%s'.ToLowerInvariant()
$pathFilter = '%s'.ToLowerInvariant()
$roleFilter = '%s'.ToLowerInvariant()
$textFilter = '%s'.ToLowerInvariant()
$containsFilter = '%s'.ToLowerInvariant()
$maxDepth = %d
$maxResults = %d
$results = New-Object System.Collections.ArrayList

function Get-ElementValue($elem) {
  try {
    $vp = $elem.GetCurrentPattern([System.Windows.Automation.ValuePattern]::Pattern)
    if ($vp) { return $vp.Current.Value }
  } catch {}
  try {
    $legacy = $elem.GetCurrentPattern([System.Windows.Automation.LegacyIAccessiblePattern]::Pattern)
    if ($legacy) { return $legacy.Current.Value }
  } catch {}
  try { return $elem.Current.Name } catch {}
  return ''
}

function Visit-Element($elem, $depth, $path) {
  if ($depth -gt $maxDepth -or $results.Count -ge $maxResults) { return }
  try {
    $role = '' + $elem.Current.ControlType.ProgrammaticName
    $label = '' + $elem.Current.Name
    $desc = '' + $elem.Current.HelpText
    $value = '' + (Get-ElementValue $elem)
    $rect = $elem.Current.BoundingRectangle

    $pathLower = $path.ToLowerInvariant()
    $roleLower = $role.ToLowerInvariant()
    $labelLower = $label.ToLowerInvariant()
    $descLower = $desc.ToLowerInvariant()
    $valueLower = $value.ToLowerInvariant()
    $match = $true
    if ($pathFilter) {
      $match = $pathLower -eq $pathFilter
    } else {
      if ($roleFilter -and $roleLower -notlike ('*' + $roleFilter + '*')) { $match = $false }
      if ($textFilter -and $labelLower -ne $textFilter -and $descLower -ne $textFilter -and $valueLower -ne $textFilter) { $match = $false }
      if ($containsFilter -and $labelLower -notlike ('*' + $containsFilter + '*') -and $descLower -notlike ('*' + $containsFilter + '*') -and $valueLower -notlike ('*' + $containsFilter + '*')) { $match = $false }
    }
    if ($match) {
      [void]$results.Add(@{
        path = $path
        role = $role
        label = $label
        description = $desc
        value = $value
        position = @([int]$rect.X, [int]$rect.Y)
        size = @([int]$rect.Width, [int]$rect.Height)
        x = [int]($rect.X + ($rect.Width / 2))
        y = [int]($rect.Y + ($rect.Height / 2))
        width = [int]$rect.Width
        height = [int]$rect.Height
      })
    }
  } catch {}
  try {
    $children = $elem.FindAll([System.Windows.Automation.TreeScope]::Children, $cond)
    for ($i = 0; $i -lt $children.Count -and $results.Count -lt $maxResults; $i++) {
      Visit-Element $children.Item($i) ($depth + 1) ($path + '/' + $i)
    }
  } catch {}
}

$windows = $root.FindAll([System.Windows.Automation.TreeScope]::Children, $cond)
for ($w = 0; $w -lt $windows.Count -and $results.Count -lt $maxResults; $w++) {
  $win = $windows.Item($w)
  try {
    $name = '' + $win.Current.Name
    if (-not $name.ToLowerInvariant().Contains($appFilter)) { continue }
    Visit-Element $win 0 ('w' + ($w + 1))
  } catch {}
}

@{ elements = @($results); count = $results.Count } | ConvertTo-Json -Depth 6
`, escapePowerShell(q.App), escapePowerShell(q.Path), escapePowerShell(q.Role), escapePowerShell(q.Text), escapePowerShell(q.Contains), q.MaxDepth, q.MaxResults)

	cmd := exec.CommandContext(ctx, "powershell", "-NoProfile", "-Command", ps)
	out, err := cmd.Output()
	if err != nil {
		return nil, err
	}
	var payload struct {
		Elements []desktopElement `json:"elements"`
	}
	if err := json.Unmarshal(out, &payload); err != nil {
		return nil, fmt.Errorf("decode: %w", err)
	}
	return payload.Elements, nil
}

func windowsSetElementValue(ctx context.Context, app, path, value string) (bool, error) {
	ps := fmt.Sprintf(`
Add-Type -AssemblyName UIAutomationClient
Add-Type -AssemblyName UIAutomationTypes

$root = [System.Windows.Automation.AutomationElement]::RootElement
$cond = [System.Windows.Automation.Condition]::TrueCondition
$appFilter = '%s'.ToLowerInvariant()
$pathFilter = '%s'.ToLowerInvariant()
$target = $null

function Find-ByPath($elem, $depth, $path) {
  if ($target -ne $null) { return }
  if ($path.ToLowerInvariant() -eq $pathFilter) {
    $script:target = $elem
    return
  }
  try {
    $children = $elem.FindAll([System.Windows.Automation.TreeScope]::Children, $cond)
    for ($i = 0; $i -lt $children.Count -and $script:target -eq $null; $i++) {
      Find-ByPath $children.Item($i) ($depth + 1) ($path + '/' + $i)
    }
  } catch {}
}

$windows = $root.FindAll([System.Windows.Automation.TreeScope]::Children, $cond)
for ($w = 0; $w -lt $windows.Count -and $target -eq $null; $w++) {
  $win = $windows.Item($w)
  try {
    $name = '' + $win.Current.Name
    if (-not $name.ToLowerInvariant().Contains($appFilter)) { continue }
    Find-ByPath $win 0 ('w' + ($w + 1))
  } catch {}
}

if ($target -eq $null) {
  Write-Output '{"ok":false,"reason":"not_found"}'
  exit 0
}

try {
  $vp = $target.GetCurrentPattern([System.Windows.Automation.ValuePattern]::Pattern)
  if ($vp) {
    $vp.SetValue('%s')
    Write-Output '{"ok":true,"strategy":"ValuePattern"}'
    exit 0
  }
} catch {}

Write-Output '{"ok":false,"reason":"unsupported"}'
`, escapePowerShell(app), escapePowerShell(path), escapePowerShell(value))

	cmd := exec.CommandContext(ctx, "powershell", "-NoProfile", "-Command", ps)
	out, err := cmd.Output()
	if err != nil {
		return false, err
	}
	var payload struct {
		OK bool `json:"ok"`
	}
	if err := json.Unmarshal(out, &payload); err != nil {
		return false, err
	}
	return payload.OK, nil
}

// ---------------------------------------------------------------------------
// find_text — Windows.Media.Ocr (built-in since Windows 10)
// ---------------------------------------------------------------------------

func (s *windowsSession) handleFindText(ctx context.Context, params map[string]any) (*desktoptypes.Response, error) {
	text := stringParam(params, "text")
	if text == "" {
		return nil, errors.New("find_text requires params.text")
	}
	app := stringParam(params, "app")

	// Focus app atomically.
	if app != "" {
		winEnsureForeground(ctx, app)
	}

	// Screenshot.
	tmpDir, err := os.MkdirTemp("", "hopclaw-ocr-*")
	if err != nil {
		return nil, fmt.Errorf("find_text: %w", err)
	}
	defer os.RemoveAll(tmpDir)
	imgPath := filepath.Join(tmpDir, "screen.png")

	// Use PowerShell to screenshot + OCR in one call for atomicity.
	ps := fmt.Sprintf(`
Add-Type -AssemblyName System.Windows.Forms
Add-Type -AssemblyName System.Drawing

# Screenshot
$screen = [System.Windows.Forms.Screen]::PrimaryScreen
$bitmap = New-Object System.Drawing.Bitmap($screen.Bounds.Width, $screen.Bounds.Height)
$graphics = [System.Drawing.Graphics]::FromImage($bitmap)
$graphics.CopyFromScreen($screen.Bounds.Location, [System.Drawing.Point]::Empty, $screen.Bounds.Size)
$bitmap.Save('%s', [System.Drawing.Imaging.ImageFormat]::Png)
$graphics.Dispose()
$bitmap.Dispose()

# OCR
[Windows.Media.Ocr.OcrEngine, Windows.Foundation, ContentType = WindowsRuntime] | Out-Null
[Windows.Graphics.Imaging.BitmapDecoder, Windows.Foundation, ContentType = WindowsRuntime] | Out-Null

$file = [Windows.Storage.StorageFile]::GetFileFromPathAsync('%s').GetAwaiter().GetResult()
$stream = $file.OpenReadAsync().GetAwaiter().GetResult()
$decoder = [Windows.Graphics.Imaging.BitmapDecoder]::CreateAsync($stream).GetAwaiter().GetResult()
$softwareBitmap = $decoder.GetSoftwareBitmapAsync().GetAwaiter().GetResult()

$engine = [Windows.Media.Ocr.OcrEngine]::TryCreateFromUserProfileLanguages()
$ocrResult = $engine.RecognizeAsync($softwareBitmap).GetAwaiter().GetResult()

$searchText = '%s'
$matches = @()
foreach ($line in $ocrResult.Lines) {
    foreach ($word in $line.Words) {
        if ($word.Text -like "*$searchText*" -or $line.Text -like "*$searchText*") {
            $rect = $word.BoundingRect
            $matches += @{
                text = $line.Text
                x = [int]($rect.X + $rect.Width / 2)
                y = [int]($rect.Y + $rect.Height / 2)
                width = [int]$rect.Width
                height = [int]$rect.Height
                confidence = 1.0
            }
            break
        }
    }
}

@{ matches = $matches; match_count = $matches.Count; scale = 1.0 } | ConvertTo-Json -Depth 5
`, escapePowerShell(imgPath), escapePowerShell(imgPath), escapePowerShell(text))

	ocrCtx, cancel := context.WithTimeout(ctx, winOCRTimeout)
	defer cancel()

	cmd := exec.CommandContext(ocrCtx, "powershell", "-NoProfile", "-Command", ps)
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("find_text: %w", err)
	}

	var result map[string]any
	if err := json.Unmarshal(out, &result); err != nil {
		return nil, fmt.Errorf("find_text: decode: %w", err)
	}

	return &desktoptypes.Response{OK: true, Data: result}, nil
}

// ---------------------------------------------------------------------------
// click_text — Atomic find + click
// ---------------------------------------------------------------------------

func (s *windowsSession) handleClickText(ctx context.Context, params map[string]any) (*desktoptypes.Response, error) {
	text := stringParam(params, "text")
	if text == "" {
		return nil, errors.New("click_text requires params.text")
	}

	resp, err := s.handleFindText(ctx, params)
	if err != nil {
		return nil, fmt.Errorf("click_text: %w", err)
	}

	matchesRaw, _ := resp.Data["matches"].([]any)
	if len(matchesRaw) == 0 {
		return nil, fmt.Errorf("click_text: text %q not found on screen", text)
	}

	match, _ := matchesRaw[0].(map[string]any)
	x, _ := match["x"].(float64)
	y, _ := match["y"].(float64)

	clickResp, err := s.handleMouseClick(ctx, map[string]any{"x": int(x), "y": int(y)})
	if err != nil {
		return nil, fmt.Errorf("click_text: click: %w", err)
	}

	return &desktoptypes.Response{
		OK: true,
		Data: map[string]any{
			"text":       text,
			"match":      match,
			"clicked":    true,
			"clicked_at": []int{int(x), int(y)},
			"click_data": clickResp.Data,
		},
	}, nil
}

func winEnsureForeground(ctx context.Context, app string) {
	ps := fmt.Sprintf(`
$proc = Get-Process -Name '%s' -ErrorAction SilentlyContinue | Select-Object -First 1
if ($proc) {
    $hwnd = $proc.MainWindowHandle
    Add-Type @"
    using System;
    using System.Runtime.InteropServices;
    public class Win32 {
        [DllImport("user32.dll")] public static extern bool SetForegroundWindow(IntPtr hWnd);
    }
"@
    [Win32]::SetForegroundWindow($hwnd)
}
`, escapePowerShell(app))
	exec.CommandContext(ctx, "powershell", "-NoProfile", "-Command", ps).Run()
	time.Sleep(winFocusSettleDelay)
}
