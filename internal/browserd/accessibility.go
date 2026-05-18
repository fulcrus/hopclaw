package browserd

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	browsertypes "github.com/fulcrus/hopclaw/browserapi/types"
	"github.com/fulcrus/hopclaw/logging"
	"github.com/chromedp/cdproto/dom"
	"github.com/chromedp/cdproto/page"
	cdpruntime "github.com/chromedp/cdproto/runtime"
	"github.com/chromedp/chromedp"
)

const (
	ariaSnapshotMaxDepth = 50   // maximum AX tree depth to traverse
	ariaSnapshotMaxNodes = 5000 // maximum AX nodes to return
	ariaRefPrefix        = "e"  // ref label prefix
)

// labelInjectionJS finds all interactive elements, highlights them with an
// orange outline and a label span (e1, e2, …), and returns a JSON string
// mapping each label to element metadata.
const labelInjectionJS = `(function() {
    var ATTR = 'data-hopclaw-label';
    var elems = document.querySelectorAll(
        'button, a, input, select, textarea, [role="button"], [role="link"], [role="menuitem"], [role="tab"], [onclick], [tabindex]:not([tabindex="-1"])'
    );
    var idx = 1;
    var mapping = {};
    var style = document.createElement('style');
    style.id = 'hopclaw-label-style';
    style.textContent = '.hopclaw-label{position:fixed;background:#ff8c00;color:#fff;font:bold 11px/1 monospace;padding:1px 4px;border-radius:3px;z-index:2147483647;pointer-events:none}';
    document.head.appendChild(style);

    elems.forEach(function(el) {
        var r = el.getBoundingClientRect();
        if (r.width < 8 || r.height < 8) return;
        if (r.bottom < 0 || r.top > window.innerHeight) return;
        if (r.right < 0 || r.left > window.innerWidth) return;
        var cs = window.getComputedStyle(el);
        if (cs.display === 'none' || cs.visibility === 'hidden' || cs.opacity === '0') return;

        var label = 'e' + idx++;
        el.setAttribute(ATTR, label);
        el.style.outline = '2px solid #ff8c00';
        el.style.outlineOffset = '1px';

        var tag = document.createElement('span');
        tag.className = 'hopclaw-label';
        tag.textContent = label;
        tag.style.left = Math.max(0, r.left) + 'px';
        tag.style.top = Math.max(0, r.top - 18) + 'px';
        document.body.appendChild(tag);

        mapping[label] = {
            tag: el.tagName.toLowerCase(),
            type: el.type || '',
            text: (el.textContent || '').trim().substring(0, 80),
            placeholder: el.placeholder || '',
            href: el.href || '',
            role: el.getAttribute('role') || '',
            rect: {x: Math.round(r.x), y: Math.round(r.y), w: Math.round(r.width), h: Math.round(r.height)}
        };
    });
    return JSON.stringify(mapping);
})()`

// labelCleanupJS removes all injected labels, outlines, and attributes.
const labelCleanupJS = `(function() {
    document.querySelectorAll('.hopclaw-label').forEach(function(el) { el.remove(); });
    var s = document.getElementById('hopclaw-label-style');
    if (s) s.remove();
    document.querySelectorAll('[data-hopclaw-label]').forEach(function(el) {
        el.style.outline = '';
        el.style.outlineOffset = '';
        el.removeAttribute('data-hopclaw-label');
    });
})()`

type typePreparationResult struct {
	Found     bool   `json:"found"`
	Tag       string `json:"tag"`
	InputType string `json:"inputType"`
	Typable   bool   `json:"typable"`
}

func typePreparationError(selector string, info typePreparationResult) error {
	if !info.Found {
		return fmt.Errorf("selector %q not found", selector)
	}
	if info.Typable {
		return nil
	}
	switch {
	case info.Tag == "select":
		return fmt.Errorf("selector %q targets a <select>; use browser.select", selector)
	case info.Tag == "input" && (info.InputType == "checkbox" || info.InputType == "radio"):
		return fmt.Errorf("selector %q targets input[%s]; use browser.click", selector, info.InputType)
	default:
		return fmt.Errorf("selector %q is not a text-entry target; use browser.click or browser.select", selector)
	}
}

func buildTypePreparationJS(selector string, clear bool) string {
	return fmt.Sprintf(`(() => {
  const target = document.querySelector(%q);
  %s
})()`, selector, buildTypePreparationBodyJS("target", clear, false))
}

func buildTypePreparationCallFunctionJS(clear bool) string {
	return "function() {\n" + buildTypePreparationBodyJS("this", clear, true) + "\n}"
}

func buildTypePreparationBodyJS(target string, clear bool, allowSpecializedInputs bool) string {
	clearJS := ""
	if clear {
		clearJS = `
if ('value' in el) {
  el.value = '';
} else if (el.isContentEditable) {
  el.textContent = '';
} else {
  el.textContent = '';
}
el.dispatchEvent(new Event('input', { bubbles: true }));
el.dispatchEvent(new Event('change', { bubbles: true }));`
	}
	blockedInputTypes := []string{
		"'checkbox'", "'radio'", "'submit'", "'button'", "'reset'", "'file'", "'color'", "'range'",
	}
	if !allowSpecializedInputs {
		blockedInputTypes = append(blockedInputTypes, "'date'", "'datetime-local'", "'month'", "'week'", "'time'")
	}
	return fmt.Sprintf(`const rawTarget = %[1]s;
  if (!rawTarget) return { found: false };
  const base = rawTarget.nodeType === Node.ELEMENT_NODE ? rawTarget : (rawTarget.parentElement || rawTarget.parentNode);
  const selector = 'input, textarea, [contenteditable], [role="textbox"], [role="searchbox"]';
  const candidates = [];
  const pushCandidate = (node) => {
    if (!node || node.nodeType !== Node.ELEMENT_NODE) return;
    if (!candidates.includes(node)) candidates.push(node);
  };
  pushCandidate(base);
  if (base && typeof base.closest === 'function') {
    pushCandidate(base.closest(selector));
    const label = base.closest('label');
    if (label && label.control) pushCandidate(label.control);
  }
  if (base && base.control) {
    pushCandidate(base.control);
  }
  if (base && typeof base.querySelector === 'function') {
    pushCandidate(base.querySelector(selector));
    const nestedLabel = base.querySelector('label');
    if (nestedLabel && nestedLabel.control) pushCandidate(nestedLabel.control);
  }
  let el = null;
  let tag = '';
  let inputType = '';
  let typable = false;
  const blockedInputTypes = new Set([%[2]s]);
  for (const node of candidates) {
    tag = (node.tagName || '').toLowerCase();
    inputType = ((node.getAttribute && node.getAttribute('type')) || node.type || '').toLowerCase();
    typable = !!node.isContentEditable || tag === 'textarea' || (tag === 'input' && !blockedInputTypes.has(inputType));
    if (typable) {
      el = node;
      break;
    }
  }
  if (!el) {
    return { found: candidates.length > 0, tag, inputType, typable: false };
  }
  if (typeof el.scrollIntoView === 'function') {
    el.scrollIntoView({ block: 'center', inline: 'nearest' });
  }
  if (typeof el.focus === 'function') {
    el.focus();
  }
  %[3]s
  return { found: true, tag, inputType, typable: true };`, target, strings.Join(blockedInputTypes, ", "), clearJS)
}

func buildClickPreparationJS(selector string) string {
	return fmt.Sprintf(`(() => {
  const el = document.querySelector(%q);
  %s
})()`, selector, buildClickPreparationBodyJS("el"))
}

func buildClickPreparationCallFunctionJS() string {
	return "function() {\n" + buildClickPreparationBodyJS("this") + "\n}"
}

func buildClickPreparationBodyJS(target string) string {
	return fmt.Sprintf(`const rawTarget = %[1]s;
  if (!rawTarget) return false;
  const candidate = rawTarget.nodeType === Node.ELEMENT_NODE ? rawTarget : (rawTarget.parentElement || rawTarget.parentNode);
  const el = candidate && typeof candidate.closest === 'function'
    ? (candidate.closest('button, input, a, label, textarea, select, option, [role="button"], [role="link"], [role="checkbox"], [role="radio"], [role="switch"], [role="tab"]') || candidate)
    : candidate;
  if (!el || el.nodeType !== Node.ELEMENT_NODE) return false;
  if (typeof el.scrollIntoView === 'function') {
    el.scrollIntoView({ block: 'center', inline: 'nearest' });
  }
  if (typeof el.focus === 'function') {
    el.focus();
  }
  const tag = (el.tagName || '').toLowerCase();
  const type = ((typeof el.getAttribute === 'function' ? el.getAttribute('type') : '') || '').toLowerCase();
  const form = el.form || (typeof el.closest === 'function' ? el.closest('form') : null);
  const isSubmitControl = (tag === 'button' || (tag === 'input' && (type === 'submit' || type === 'image'))) && form;
  if (isSubmitControl) {
    if (typeof form.requestSubmit === 'function') {
      form.requestSubmit(el);
      return true;
    }
    if (typeof form.submit === 'function') {
      form.submit();
      return true;
    }
  }
  if (typeof el.click === 'function') {
    el.click();
  } else {
    el.dispatchEvent(new MouseEvent('click', { bubbles: true, cancelable: true, view: window }));
  }
  return true;`, target)
}

func (s *chromeSession) handleScreenshotLabeled(ctx context.Context, params map[string]any) (*browsertypes.Response, error) {
	timeout := durationParamMillis(params, "timeout_ms", defaultCaptureTimeout)
	actionCtx, cancel := timedActionContext(s.ctx, ctx, timeout)
	defer cancel()

	var mappingJSON string
	if err := chromedp.Run(actionCtx, chromedp.Evaluate(labelInjectionJS, &mappingJSON)); err != nil {
		return nil, fmt.Errorf("screenshot_labeled: inject labels: %w", err)
	}
	defer s.cleanupInjectedLabels()

	quality := screenshotQuality(intParam(params, "quality", defaultScreenshotQaul))
	var raw []byte
	mimeType := "image/png"
	if quality < 100 {
		mimeType = "image/jpeg"
	}
	var err error
	err = chromedp.Run(actionCtx, chromedp.ActionFunc(func(cctx context.Context) error {
		capture := page.CaptureScreenshot().WithFromSurface(true)
		if quality < 100 {
			capture = capture.WithFormat(page.CaptureScreenshotFormatJpeg).WithQuality(int64(quality))
		}
		raw, err = capture.Do(cctx)
		return err
	}))
	if err != nil {
		return nil, fmt.Errorf("screenshot_labeled: capture: %w", err)
	}

	var elements map[string]any
	if err := json.Unmarshal([]byte(mappingJSON), &elements); err != nil {
		elements = map[string]any{}
	}

	return &browsertypes.Response{
		OK: true,
		Data: map[string]any{
			"mime_type":      mimeType,
			"encoding":       "base64",
			"content_base64": base64.StdEncoding.EncodeToString(raw),
			"elements":       elements,
			"element_count":  len(elements),
		},
	}, nil
}

func (s *chromeSession) cleanupInjectedLabels() {
	if s == nil {
		return
	}
	cleanupCtx, cancel := timedActionContext(s.ctx, context.Background(), labelCleanupTimeout)
	defer cancel()
	_ = chromedp.Run(cleanupCtx, chromedp.Evaluate(labelCleanupJS, nil))
}

// ariaNodeInfo is the compact representation of an AX node for AI consumption.
type ariaNodeInfo struct {
	Ref         string         `json:"ref,omitempty"`
	Role        string         `json:"role"`
	Name        string         `json:"name,omitempty"`
	Value       string         `json:"value,omitempty"`
	Description string         `json:"description,omitempty"`
	Focused     bool           `json:"focused,omitempty"`
	Required    bool           `json:"required,omitempty"`
	Checked     string         `json:"checked,omitempty"`
	Disabled    bool           `json:"disabled,omitempty"`
	Expanded    string         `json:"expanded,omitempty"`
	Level       int            `json:"level,omitempty"`
	Children    []ariaNodeInfo `json:"children,omitempty"`
}

func (s *chromeSession) handleSnapshotAria(ctx context.Context, _ map[string]any) (*browsertypes.Response, error) {
	actionCtx, cancel := timedActionContext(s.ctx, ctx, defaultCaptureTimeout)
	defer cancel()

	axNodes, err := getFullAXTreeRaw(actionCtx, ariaSnapshotMaxDepth)
	if err != nil {
		return nil, fmt.Errorf("snapshot_aria: get accessibility tree: %w", err)
	}

	if len(axNodes) == 0 {
		return &browsertypes.Response{
			OK: true,
			Data: map[string]any{
				"tree":          nil,
				"element_count": 0,
			},
		}, nil
	}

	root, refs := buildCompactAriaTree(axNodes, ariaSnapshotMaxDepth)

	s.ariaMu.Lock()
	s.ariaRefs = refs
	s.ariaMu.Unlock()

	var textBuf strings.Builder
	var renderText func(node *ariaNodeInfo, indent int)
	renderText = func(node *ariaNodeInfo, indent int) {
		if node == nil {
			return
		}
		prefix := strings.Repeat("  ", indent)
		line := prefix
		if node.Ref != "" {
			line += "[" + node.Ref + "] "
		}
		line += node.Role
		if node.Name != "" {
			line += " \"" + node.Name + "\""
		}
		if node.Value != "" {
			line += " value=\"" + node.Value + "\""
		}
		if node.Checked != "" {
			line += " checked=" + node.Checked
		}
		if node.Disabled {
			line += " disabled"
		}
		if node.Focused {
			line += " focused"
		}
		if node.Expanded != "" {
			line += " expanded=" + node.Expanded
		}
		textBuf.WriteString(line + "\n")
		for i := range node.Children {
			renderText(&node.Children[i], indent+1)
		}
	}
	renderText(root, 0)

	return &browsertypes.Response{
		OK: true,
		Data: map[string]any{
			"tree":          root,
			"text":          textBuf.String(),
			"element_count": len(refs),
		},
	}, nil
}

func (s *chromeSession) handleClickAria(ctx context.Context, params map[string]any) (*browsertypes.Response, error) {
	ref := stringParam(params, "ref")
	if ref == "" {
		return nil, errors.New("click_aria requires params.ref")
	}

	s.ariaMu.Lock()
	backendID, ok := s.ariaRefs[ref]
	s.ariaMu.Unlock()
	if !ok {
		return nil, fmt.Errorf("click_aria: ref %q not found (run snapshot_aria first)", ref)
	}

	actionCtx, cancel := timedActionContext(s.ctx, ctx, defaultActionTimeout)
	defer cancel()

	if err := chromedp.Run(actionCtx, chromedp.ActionFunc(func(ctx context.Context) error {
		obj, err := dom.ResolveNode().WithBackendNodeID(backendID).Do(ctx)
		if err != nil {
			return fmt.Errorf("resolve node for ref %q: %w", ref, err)
		}
		if obj == nil || obj.ObjectID == "" {
			return fmt.Errorf("ref %q resolved to nil object", ref)
		}
		defer cdpruntime.ReleaseObject(obj.ObjectID).Do(ctx)

		_, _, err = cdpruntime.CallFunctionOn(buildClickPreparationCallFunctionJS()).WithObjectID(obj.ObjectID).Do(ctx)
		return err
	})); err != nil {
		return nil, fmt.Errorf("click_aria: %w", err)
	}

	finalURL := ""
	title := ""
	logging.DebugIfErr(chromedp.Run(actionCtx,
		chromedp.Location(&finalURL),
		chromedp.Title(&title),
	), "chromedp get location after aria click failed")

	return &browsertypes.Response{
		OK: true,
		Data: map[string]any{
			"ref":     ref,
			"clicked": true,
			"url":     finalURL,
			"title":   title,
		},
	}, nil
}

func (s *chromeSession) handleTypeAria(ctx context.Context, params map[string]any) (*browsertypes.Response, error) {
	ref := stringParam(params, "ref")
	if ref == "" {
		return nil, errors.New("type_aria requires params.ref")
	}
	text := stringParam(params, "text")
	if text == "" {
		return nil, errors.New("type_aria requires params.text")
	}

	s.ariaMu.Lock()
	backendID, ok := s.ariaRefs[ref]
	s.ariaMu.Unlock()
	if !ok {
		return nil, fmt.Errorf("type_aria: ref %q not found (run snapshot_aria first)", ref)
	}

	clear := boolParam(params, "clear", false)

	actionCtx, cancel := timedActionContext(s.ctx, ctx, defaultActionTimeout)
	defer cancel()

	if err := chromedp.Run(actionCtx, chromedp.ActionFunc(func(ctx context.Context) error {
		obj, err := dom.ResolveNode().WithBackendNodeID(backendID).Do(ctx)
		if err != nil {
			return fmt.Errorf("resolve node for ref %q: %w", ref, err)
		}
		if obj == nil || obj.ObjectID == "" {
			return fmt.Errorf("ref %q resolved to nil object", ref)
		}
		defer cdpruntime.ReleaseObject(obj.ObjectID).Do(ctx)

		prep, _, err := cdpruntime.CallFunctionOn(buildTypePreparationCallFunctionJS(clear)).
			WithObjectID(obj.ObjectID).
			WithReturnByValue(true).
			Do(ctx)
		if err != nil {
			return err
		}
		if prep == nil {
			return fmt.Errorf("ref %q preparation returned nil result", ref)
		}
		var info typePreparationResult
		if len(prep.Value) > 0 {
			if err := json.Unmarshal(prep.Value, &info); err != nil {
				return fmt.Errorf("decode preparation result for ref %q: %w", ref, err)
			}
		}
		if info.Typable {
			return nil
		}
		switch {
		case !info.Found:
			return fmt.Errorf("ref %q is not a text-entry target", ref)
		case info.Tag == "select":
			return fmt.Errorf("ref %q targets a <select>; use browser.select or browser.click_aria", ref)
		case info.Tag == "input" && (info.InputType == "checkbox" || info.InputType == "radio"):
			return fmt.Errorf("ref %q targets input[%s]; use browser.click_aria", ref, info.InputType)
		default:
			return fmt.Errorf("ref %q is not a text-entry target; use browser.click_aria", ref)
		}
	})); err != nil {
		return nil, fmt.Errorf("type_aria: %w", err)
	}

	if err := chromedp.Run(actionCtx, chromedp.KeyEvent(text)); err != nil {
		return nil, fmt.Errorf("type_aria: key event: %w", err)
	}

	return &browsertypes.Response{
		OK: true,
		Data: map[string]any{
			"ref":   ref,
			"text":  text,
			"typed": true,
		},
	}, nil
}

// isInteractiveRole returns true for ARIA roles that represent interactive elements.
func isInteractiveRole(role string) bool {
	switch role {
	case "button", "link", "textbox", "searchbox", "combobox", "listbox",
		"menuitem", "menuitemcheckbox", "menuitemradio", "option",
		"radio", "checkbox", "switch", "slider", "spinbutton",
		"tab", "treeitem", "gridcell",
		"TextField", "Button", "Link", "MenuItem", "ListBoxOption",
		"Checkbox", "RadioButton", "ComboBox", "SearchBox", "Slider",
		"SpinButton", "Switch", "Tab", "TreeItem":
		return true
	}
	return false
}
