package browserd

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	browsertypes "github.com/fulcrus/hopclaw/browserapi/types"
	"github.com/fulcrus/hopclaw/logging"
	cdpbrowser "github.com/chromedp/cdproto/browser"
	cdp "github.com/chromedp/cdproto/cdp"
	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/cdproto/page"
	"github.com/chromedp/cdproto/performance"
	cdpruntime "github.com/chromedp/cdproto/runtime"
	"github.com/chromedp/cdproto/target"
	"github.com/chromedp/cdproto/tracing"
	"github.com/chromedp/chromedp"
	"github.com/chromedp/chromedp/kb"
)

var log = logging.WithSubsystem("browserd")

const (
	defaultActionTimeout   = 30 * time.Second
	defaultWaitTimeout     = 15 * time.Second
	defaultDownloadTimeout = 30 * time.Second
	defaultCaptureTimeout  = 90 * time.Second
	visibilityCheckTimeout = 2 * time.Second
	downloadPollInterval   = 500 * time.Millisecond
	defaultScrollAmount    = 300
	defaultWindowWidth     = 1440
	defaultWindowHeight    = 900
	defaultScreenshotQaul  = 100
	labelCleanupTimeout    = 5 * time.Second
)

type ChromeConfig struct {
	ExecPath   string
	Headless   bool
	NoSandbox  bool
	SSRFPolicy *SSRFPolicy // optional SSRF guard for navigation
}

type ChromeEngine struct {
	cfg ChromeConfig
}

// harEntry represents a single captured HTTP request/response pair.
type harEntry struct {
	URL          string    `json:"url"`
	Method       string    `json:"method"`
	Status       int64     `json:"status"`
	ResponseSize float64   `json:"response_size"`
	StartedAt    time.Time `json:"started_at"`
	Duration     float64   `json:"duration"`
}

// consoleMessage represents a single captured console API call.
type consoleMessage struct {
	Level  string `json:"level"`
	Text   string `json:"text"`
	URL    string `json:"url,omitempty"`
	Line   int    `json:"line,omitempty"`
	Column int    `json:"column,omitempty"`
}

type chromeSession struct {
	id            string
	ctx           context.Context
	cancel        context.CancelFunc
	allocatorStop context.CancelFunc
	profileDir    string
	persistent    bool // true if profileDir should be kept on Close
	actionMu      sync.Mutex
	stateMu       sync.Mutex
	closed        bool

	// Tracing state.
	tracingMu     sync.Mutex // guards tracingActive, traceEvents
	tracingActive bool
	traceEvents   []json.RawMessage

	// HAR recording state.
	harMu        sync.Mutex // guards harRecording, harEntries, harRequests
	harRecording bool
	harEntries   []harEntry
	harRequests  map[network.RequestID]*harRequestInfo

	// Console capture state.
	consoleMu        sync.Mutex // guards consoleCapturing, consoleMessages
	consoleCapturing bool
	consoleMessages  []consoleMessage

	// ARIA ref state.
	ariaMu   sync.Mutex                   // guards ariaRefs
	ariaRefs map[string]cdp.BackendNodeID // ref label → backend DOM node ID

	// SSRF policy — nil means no restrictions.
	ssrfPolicy *SSRFPolicy
}

// harRequestInfo tracks an in-flight request for HAR recording.
type harRequestInfo struct {
	URL       string
	Method    string
	StartedAt time.Time
}

func NewChromeEngine(cfg ChromeConfig) (*ChromeEngine, error) {
	cfg.ExecPath = strings.TrimSpace(cfg.ExecPath)
	if cfg.ExecPath == "" {
		resolved, err := findChromeBinary()
		if err != nil {
			return nil, err
		}
		cfg.ExecPath = resolved
	} else {
		resolved, err := exec.LookPath(cfg.ExecPath)
		if err != nil {
			return nil, fmt.Errorf("resolve chrome binary %q: %w", cfg.ExecPath, err)
		}
		cfg.ExecPath = resolved
	}
	return &ChromeEngine{cfg: cfg}, nil
}

func (e *ChromeEngine) OpenSession(ctx context.Context, spec OpenSessionSpec) (Session, error) {
	// Ensure loopback addresses bypass any configured HTTP proxy so that
	// local CDP connections are not mis-routed.
	restoreProxy := EnsureCDPProxyBypass()
	defer restoreProxy()

	// The allocator and tab contexts must NOT be derived from the HTTP
	// request context (ctx).  The request context is cancelled as soon as
	// the create_session response is written, which would immediately kill
	// the Chrome process.  Instead we use context.Background() so the
	// browser lives until Close() is called.  The request ctx is still
	// checked below for early cancellation during startup.

	// ---------------------------------------------------------------------------
	// Remote CDP mode
	// ---------------------------------------------------------------------------
	if remoteCDPURL := stringParam(spec.Params, "remote_cdp_url"); remoteCDPURL != "" {
		if _, err := url.Parse(remoteCDPURL); err != nil {
			return nil, fmt.Errorf("parse remote cdp url: %w", err)
		}
		allocCtx, allocCancel := chromedp.NewRemoteAllocator(context.Background(), remoteCDPURL)
		tabCtx, cancel := chromedp.NewContext(allocCtx)

		if err := chromedp.Run(tabCtx); err != nil {
			cancel()
			allocCancel()
			return nil, fmt.Errorf("connect remote cdp: %w", err)
		}

		session := &chromeSession{
			id:            spec.ID,
			ctx:           tabCtx,
			cancel:        cancel,
			allocatorStop: allocCancel,
			ssrfPolicy:    e.cfg.SSRFPolicy,
		}

		if rawURL := stringParam(spec.Params, "url"); rawURL != "" {
			resp, err := session.Handle(ctx, browsertypes.Request{
				Action:    browsertypes.ActionNavigate,
				SessionID: spec.ID,
				Params: map[string]any{
					"url": rawURL,
				},
			})
			if err != nil {
				logging.DebugIfErr(session.Close(context.Background()), "close chrome session failed")
				return nil, err
			}
			if resp != nil && !resp.OK {
				logging.DebugIfErr(session.Close(context.Background()), "close chrome session failed")
				return nil, errors.New(resp.Error)
			}
		}

		return session, nil
	}

	// ---------------------------------------------------------------------------
	// Local Chrome mode
	// ---------------------------------------------------------------------------

	// Determine user-data directory: persistent profile or temp.
	var profileDir string
	var persistentProfile bool
	if profileName := stringParam(spec.Params, "profile_name"); profileName != "" {
		homeDir, err := os.UserHomeDir()
		if err != nil {
			return nil, fmt.Errorf("resolve home dir for profile: %w", err)
		}
		profileDir = filepath.Join(homeDir, ".hopclaw", "browser", "profiles", profileName, "user-data")
		if err := os.MkdirAll(profileDir, 0o755); err != nil {
			return nil, fmt.Errorf("create persistent profile dir: %w", err)
		}
		persistentProfile = true
	} else {
		var err error
		profileDir, err = os.MkdirTemp("", "hopclaw-browserd-*")
		if err != nil {
			return nil, fmt.Errorf("create browser profile dir: %w", err)
		}
	}

	width := intParam(spec.Params, "width", defaultWindowWidth)
	height := intParam(spec.Params, "height", defaultWindowHeight)
	opts := append([]chromedp.ExecAllocatorOption{}, chromedp.DefaultExecAllocatorOptions[:]...)
	opts = append(opts,
		chromedp.ExecPath(e.cfg.ExecPath),
		chromedp.UserDataDir(profileDir),
		chromedp.NoFirstRun,
		chromedp.NoDefaultBrowserCheck,
		chromedp.WindowSize(width, height),
		chromedp.Flag("disable-background-networking", true),
		chromedp.Flag("disable-default-apps", true),
		chromedp.Flag("disable-sync", true),
		chromedp.Flag("metrics-recording-only", true),
	)
	if e.cfg.Headless {
		opts = append(opts, chromedp.Headless)
	} else {
		opts = append(opts, chromedp.Flag("headless", false))
	}
	if e.cfg.NoSandbox {
		opts = append(opts, chromedp.NoSandbox)
	}

	allocCtx, allocCancel := chromedp.NewExecAllocator(context.Background(), opts...)
	tabCtx, cancel := chromedp.NewContext(allocCtx)

	if err := chromedp.Run(tabCtx); err != nil {
		cancel()
		allocCancel()
		if !persistentProfile {
			logging.DebugIfErr(os.RemoveAll(profileDir), "remove chrome profile dir failed")
		}
		return nil, fmt.Errorf("start browser session: %w", err)
	}

	session := &chromeSession{
		id:            spec.ID,
		ctx:           tabCtx,
		cancel:        cancel,
		allocatorStop: allocCancel,
		profileDir:    profileDir,
		persistent:    persistentProfile,
		ssrfPolicy:    e.cfg.SSRFPolicy,
	}

	if rawURL := stringParam(spec.Params, "url"); rawURL != "" {
		resp, err := session.Handle(ctx, browsertypes.Request{
			Action:    browsertypes.ActionNavigate,
			SessionID: spec.ID,
			Params: map[string]any{
				"url": rawURL,
			},
		})
		if err != nil {
			logging.DebugIfErr(session.Close(context.Background()), "close chrome session failed")
			return nil, err
		}
		if resp != nil && !resp.OK {
			logging.DebugIfErr(session.Close(context.Background()), "close chrome session failed")
			return nil, errors.New(resp.Error)
		}
	}

	return session, nil
}

func (s *chromeSession) ID() string {
	return s.id
}

func (s *chromeSession) Handle(ctx context.Context, req browsertypes.Request) (*browsertypes.Response, error) {
	s.stateMu.Lock()
	if s.closed {
		s.stateMu.Unlock()
		return nil, errors.New("browser session is closed")
	}
	s.stateMu.Unlock()

	s.actionMu.Lock()
	defer s.actionMu.Unlock()

	s.stateMu.Lock()
	if s.closed {
		s.stateMu.Unlock()
		return nil, errors.New("browser session is closed")
	}
	s.stateMu.Unlock()

	switch strings.TrimSpace(req.Action) {
	case browsertypes.ActionNavigate:
		return s.handleNavigate(ctx, req.Params)
	case browsertypes.ActionClick:
		return s.handleClick(ctx, req.Params)
	case browsertypes.ActionType:
		return s.handleType(ctx, req.Params)
	case browsertypes.ActionWaitFor:
		return s.handleWaitFor(ctx, req.Params)
	case browsertypes.ActionSnapshot:
		return s.handleSnapshot(ctx)
	case browsertypes.ActionScreenshot:
		return s.handleScreenshot(ctx, req.Params)
	case browsertypes.ActionScreenshotLabeled:
		return s.handleScreenshotLabeled(ctx, req.Params)
	case browsertypes.ActionSnapshotAria:
		return s.handleSnapshotAria(ctx, req.Params)
	case browsertypes.ActionClickAria:
		return s.handleClickAria(ctx, req.Params)
	case browsertypes.ActionTypeAria:
		return s.handleTypeAria(ctx, req.Params)
	case browsertypes.ActionListTabs:
		return s.handleListTabs(ctx)
	case browsertypes.ActionEval:
		return s.handleEval(ctx, req.Params)
	case browsertypes.ActionGetCookies:
		return s.handleGetCookies(ctx)
	case browsertypes.ActionSetCookie:
		return s.handleSetCookie(ctx, req.Params)
	case browsertypes.ActionReload:
		return s.handleReload(ctx, req.Params)
	case browsertypes.ActionBack:
		return s.handleBack(ctx)
	case browsertypes.ActionForward:
		return s.handleForward(ctx)
	case browsertypes.ActionHover:
		return s.handleHover(ctx, req.Params)
	case browsertypes.ActionSelectOption:
		return s.handleSelectOption(ctx, req.Params)
	case browsertypes.ActionFill:
		return s.handleFill(ctx, req.Params)
	case browsertypes.ActionHandleDialog:
		return s.handleDialog(ctx, req.Params)
	case browsertypes.ActionPDF:
		return s.handlePDF(ctx, req.Params)
	case browsertypes.ActionNetworkEnable:
		return s.handleNetworkEnable(ctx)
	case browsertypes.ActionNetworkReqs:
		return s.handleNetworkRequests(ctx, req.Params)
	case browsertypes.ActionDownload:
		return s.handleDownload(ctx, req.Params)
	case browsertypes.ActionScroll:
		return s.handleScroll(ctx, req.Params)
	case browsertypes.ActionDrag:
		return s.handleDrag(ctx, req.Params)
	case browsertypes.ActionUpload:
		return s.handleUpload(ctx, req.Params)
	case browsertypes.ActionNewTab:
		return s.handleNewTab(ctx, req.Params)
	case browsertypes.ActionSwitchTab:
		return s.handleSwitchTab(ctx, req.Params)
	case browsertypes.ActionCloseTab:
		return s.handleCloseTab(ctx, req.Params)
	case browsertypes.ActionElementText:
		return s.handleElementText(ctx, req.Params)
	case browsertypes.ActionElementAttr:
		return s.handleElementAttr(ctx, req.Params)
	case browsertypes.ActionElementVisible:
		return s.handleElementVisible(ctx, req.Params)
	case browsertypes.ActionKeyboard:
		return s.handleKeyboard(ctx, req.Params)
	case browsertypes.ActionIframe:
		return s.handleIframe(ctx, req.Params)
	case browsertypes.ActionTraceStart:
		return s.handleTraceStart(ctx, req.Params)
	case browsertypes.ActionTraceStop:
		return s.handleTraceStop(ctx)
	case browsertypes.ActionHARStart:
		return s.handleHARStart(ctx)
	case browsertypes.ActionHARStop:
		return s.handleHARStop(ctx, req.Params)
	case browsertypes.ActionConsoleStart:
		return s.handleConsoleStart(ctx)
	case browsertypes.ActionConsoleMessages:
		return s.handleConsoleMessages(ctx, req.Params)
	case browsertypes.ActionPerformanceMetrics:
		return s.handlePerformanceMetrics(ctx)
	case browsertypes.ActionEmulateDevice:
		return s.handleEmulateDevice(ctx, req.Params)
	case browsertypes.ActionEmulateVision:
		return s.handleEmulateVision(ctx, req.Params)
	case browsertypes.ActionSetGeolocation:
		return s.handleSetGeolocation(ctx, req.Params)
	case browsertypes.ActionSetTimezone:
		return s.handleSetTimezone(ctx, req.Params)
	case browsertypes.ActionSetLocale:
		return s.handleSetLocale(ctx, req.Params)
	case browsertypes.ActionSetColorScheme:
		return s.handleSetColorScheme(ctx, req.Params)
	case browsertypes.ActionSetOffline:
		return s.handleSetOffline(ctx, req.Params)
	case browsertypes.ActionSetHeaders:
		return s.handleSetHeaders(ctx, req.Params)
	case browsertypes.ActionSetCredentials:
		return s.handleSetCredentials(ctx, req.Params)
	default:
		return nil, fmt.Errorf("unsupported browser action %q", req.Action)
	}
}

func (s *chromeSession) Close(_ context.Context) error {
	s.stateMu.Lock()
	if s.closed {
		s.stateMu.Unlock()
		return nil
	}
	s.closed = true
	cancel := s.cancel
	allocatorStop := s.allocatorStop
	profileDir := s.profileDir
	s.cancel = nil
	s.allocatorStop = nil
	s.profileDir = ""
	s.stateMu.Unlock()

	if cancel != nil {
		cancel()
	}
	if allocatorStop != nil {
		allocatorStop()
	}

	if profileDir != "" && !s.persistent {
		if err := os.RemoveAll(profileDir); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	return nil
}

func (s *chromeSession) handleNavigate(ctx context.Context, params map[string]any) (*browsertypes.Response, error) {
	rawURL := stringParam(params, "url")
	if rawURL == "" {
		return nil, errors.New("navigate requires params.url")
	}
	if _, err := url.ParseRequestURI(rawURL); err != nil {
		return nil, fmt.Errorf("invalid url %q: %w", rawURL, err)
	}

	// SSRF pre-navigation check.
	if s.ssrfPolicy != nil {
		if err := checkNavigationSSRF(rawURL, *s.ssrfPolicy); err != nil {
			return nil, err
		}
	}

	var finalURL string
	var title string
	actionCtx, cancel := timedActionContext(s.ctx, ctx, defaultActionTimeout)
	defer cancel()
	err := chromedp.Run(actionCtx,
		chromedp.Navigate(rawURL),
		chromedp.WaitReady("body", chromedp.ByQuery),
		chromedp.Location(&finalURL),
		chromedp.Title(&title),
	)
	if err != nil {
		return nil, fmt.Errorf("navigate: %w", err)
	}

	// SSRF post-navigation check — catch JS redirects to private networks.
	if s.ssrfPolicy != nil && finalURL != "" && finalURL != rawURL {
		if err := checkNavigationSSRF(finalURL, *s.ssrfPolicy); err != nil {
			// Redirect to private network detected — navigate to blank page.
			_ = chromedp.Run(actionCtx, chromedp.Navigate("about:blank"))
			return nil, err
		}
	}

	return &browsertypes.Response{
		OK: true,
		Data: map[string]any{
			"url":   finalURL,
			"title": title,
		},
	}, nil
}

func (s *chromeSession) handleClick(ctx context.Context, params map[string]any) (*browsertypes.Response, error) {
	selector := stringParam(params, "selector")
	if selector == "" {
		return nil, errors.New("click requires params.selector")
	}
	resp, err := s.handleEval(ctx, map[string]any{
		"expression": buildClickPreparationJS(selector),
	})
	if err != nil {
		return nil, fmt.Errorf("click %q: %w", selector, err)
	}
	ok, _ := resp.Data["result"].(bool)
	if !ok {
		return nil, fmt.Errorf("selector %q not found", selector)
	}
	actionCtx, cancel := timedActionContext(s.ctx, ctx, defaultActionTimeout)
	defer cancel()

	finalURL := ""
	title := ""
	logging.DebugIfErr(chromedp.Run(actionCtx,
		chromedp.Location(&finalURL),
		chromedp.Title(&title),
	), "chromedp get location after click failed")
	return &browsertypes.Response{
		OK: true,
		Data: map[string]any{
			"selector": selector,
			"clicked":  true,
			"url":      finalURL,
			"title":    title,
		},
	}, nil
}

func (s *chromeSession) handleType(ctx context.Context, params map[string]any) (*browsertypes.Response, error) {
	selector := stringParam(params, "selector")
	text := stringParam(params, "text")
	if selector == "" {
		return nil, errors.New("type requires params.selector")
	}
	actionCtx, cancel := timedActionContext(s.ctx, ctx, defaultActionTimeout)
	defer cancel()

	mode := strings.ToLower(stringParam(params, "mode"))
	clear := mode != "keys"
	actions := []chromedp.Action{
		chromedp.WaitVisible(selector, chromedp.ByQuery),
		chromedp.ActionFunc(func(ctx context.Context) error {
			return focusAndPrepareTypeTarget(ctx, selector, clear)
		}),
	}
	if text != "" {
		actions = append(actions, chromedp.KeyEvent(text))
	}
	if err := chromedp.Run(actionCtx, actions...); err != nil {
		return nil, fmt.Errorf("type %q: %w", selector, err)
	}
	return &browsertypes.Response{
		OK: true,
		Data: map[string]any{
			"selector": selector,
			"text":     text,
		},
	}, nil
}

func focusAndPrepareTypeTarget(ctx context.Context, selector string, clear bool) error {
	var info typePreparationResult
	js := buildTypePreparationJS(selector, clear)
	if err := chromedp.Evaluate(js, &info).Do(ctx); err != nil {
		return err
	}
	return typePreparationError(selector, info)
}

func (s *chromeSession) handleWaitFor(ctx context.Context, params map[string]any) (*browsertypes.Response, error) {
	selector := stringParam(params, "selector")
	if selector == "" {
		return nil, errors.New("wait_for requires params.selector")
	}
	timeout := durationParamMillis(params, "timeout_ms", defaultWaitTimeout)
	actionCtx, cancel := timedActionContext(s.ctx, ctx, timeout)
	defer cancel()
	if err := chromedp.Run(actionCtx, chromedp.WaitVisible(selector, chromedp.ByQuery)); err != nil {
		return nil, fmt.Errorf("wait_for %q: %w", selector, err)
	}
	return &browsertypes.Response{
		OK: true,
		Data: map[string]any{
			"selector": selector,
			"state":    "visible",
		},
	}, nil
}

func (s *chromeSession) handleSnapshot(ctx context.Context) (*browsertypes.Response, error) {
	var html string
	var pageURL string
	var title string
	actionCtx, cancel := timedActionContext(s.ctx, ctx, defaultCaptureTimeout)
	defer cancel()
	if err := chromedp.Run(actionCtx,
		chromedp.OuterHTML("html", &html, chromedp.ByQuery),
		chromedp.Location(&pageURL),
		chromedp.Title(&title),
	); err != nil {
		return nil, fmt.Errorf("snapshot: %w", err)
	}
	return &browsertypes.Response{
		OK: true,
		Data: map[string]any{
			"html":         html,
			"url":          pageURL,
			"title":        title,
			"content_type": "text/html",
		},
	}, nil
}

func (s *chromeSession) handleScreenshot(ctx context.Context, params map[string]any) (*browsertypes.Response, error) {
	fullPage := boolParam(params, "full_page", true)
	quality := screenshotQuality(intParam(params, "quality", defaultScreenshotQaul))
	timeout := durationParamMillis(params, "timeout_ms", defaultCaptureTimeout)
	actionCtx, cancel := timedActionContext(s.ctx, ctx, timeout)
	defer cancel()

	var raw []byte
	var err error
	mimeType := "image/png"
	if fullPage {
		if quality < 100 {
			mimeType = "image/jpeg"
		}
		err = chromedp.Run(actionCtx, chromedp.FullScreenshot(&raw, quality))
	} else {
		raw, err = page.CaptureScreenshot().WithFromSurface(true).Do(actionCtx)
	}
	if err != nil {
		return nil, fmt.Errorf("screenshot: %w", err)
	}
	return &browsertypes.Response{
		OK: true,
		Data: map[string]any{
			"mime_type":      mimeType,
			"encoding":       "base64",
			"content_base64": base64.StdEncoding.EncodeToString(raw),
			"full_page":      fullPage,
		},
	}, nil
}

func (s *chromeSession) handleListTabs(ctx context.Context) (*browsertypes.Response, error) {
	actionCtx, cancel := timedActionContext(s.ctx, ctx, defaultActionTimeout)
	defer cancel()
	targets, err := chromedp.Targets(actionCtx)
	if err != nil {
		return nil, fmt.Errorf("list_tabs: %w", err)
	}
	tabs := make([]browsertypes.Tab, 0, len(targets))
	for _, info := range targets {
		if info == nil || info.Type != "page" {
			continue
		}
		tabs = append(tabs, browsertypes.Tab{
			ID:    string(info.TargetID),
			URL:   info.URL,
			Title: info.Title,
		})
	}
	return &browsertypes.Response{
		OK: true,
		Data: map[string]any{
			"tabs": tabs,
		},
	}, nil
}

func (s *chromeSession) handleEval(ctx context.Context, params map[string]any) (*browsertypes.Response, error) {
	expression := stringParam(params, "expression")
	if expression == "" {
		return nil, errors.New("eval requires params.expression")
	}
	actionCtx, cancel := timedActionContext(s.ctx, ctx, defaultActionTimeout)
	defer cancel()
	var result any
	if err := chromedp.Run(actionCtx, chromedp.Evaluate(expression, &result)); err != nil {
		return nil, fmt.Errorf("eval: %w", err)
	}
	return &browsertypes.Response{
		OK: true,
		Data: map[string]any{
			"result": result,
		},
	}, nil
}

func (s *chromeSession) handleGetCookies(ctx context.Context) (*browsertypes.Response, error) {
	actionCtx, cancel := timedActionContext(s.ctx, ctx, defaultActionTimeout)
	defer cancel()

	var cookies []*network.Cookie
	err := chromedp.Run(actionCtx, chromedp.ActionFunc(func(ctx context.Context) error {
		var err error
		cookies, err = network.GetCookies().Do(ctx)
		return err
	}))
	if err != nil {
		return nil, fmt.Errorf("get_cookies: %w", err)
	}

	result := make([]map[string]any, 0, len(cookies))
	for _, c := range cookies {
		result = append(result, map[string]any{
			"name":   c.Name,
			"value":  c.Value,
			"domain": c.Domain,
			"path":   c.Path,
		})
	}
	return &browsertypes.Response{
		OK: true,
		Data: map[string]any{
			"cookies": result,
		},
	}, nil
}

func (s *chromeSession) handleSetCookie(ctx context.Context, params map[string]any) (*browsertypes.Response, error) {
	name := stringParam(params, "name")
	if name == "" {
		return nil, errors.New("set_cookie requires params.name")
	}
	value := stringParam(params, "value")
	domain := stringParam(params, "domain")
	if domain == "" {
		return nil, errors.New("set_cookie requires params.domain")
	}
	cookiePath := stringParam(params, "path")
	if cookiePath == "" {
		cookiePath = "/"
	}

	actionCtx, cancel := timedActionContext(s.ctx, ctx, defaultActionTimeout)
	defer cancel()

	err := chromedp.Run(actionCtx, chromedp.ActionFunc(func(ctx context.Context) error {
		return network.SetCookie(name, value).
			WithDomain(domain).
			WithPath(cookiePath).
			Do(ctx)
	}))
	if err != nil {
		return nil, fmt.Errorf("set_cookie: %w", err)
	}
	return &browsertypes.Response{
		OK: true,
		Data: map[string]any{
			"name":   name,
			"domain": domain,
			"path":   cookiePath,
		},
	}, nil
}

func (s *chromeSession) handleReload(ctx context.Context, params map[string]any) (*browsertypes.Response, error) {
	actionCtx, cancel := timedActionContext(s.ctx, ctx, defaultActionTimeout)
	defer cancel()

	waitUntil := stringParam(params, "wait_until")
	if err := chromedp.Run(actionCtx, chromedp.Reload()); err != nil {
		return nil, fmt.Errorf("reload: %w", err)
	}

	// Wait for the page to settle according to the requested lifecycle event.
	switch strings.ToLower(waitUntil) {
	case "domcontentloaded":
		logging.DebugIfErr(chromedp.Run(actionCtx, chromedp.WaitReady("body", chromedp.ByQuery)), "chromedp action failed")
	default:
		logging.DebugIfErr(chromedp.Run(actionCtx, chromedp.WaitReady("body", chromedp.ByQuery)), "chromedp action failed")
	}

	return &browsertypes.Response{
		OK:   true,
		Data: map[string]any{"reloaded": true},
	}, nil
}

func (s *chromeSession) handleBack(ctx context.Context) (*browsertypes.Response, error) {
	actionCtx, cancel := timedActionContext(s.ctx, ctx, defaultActionTimeout)
	defer cancel()

	if err := chromedp.Run(actionCtx, chromedp.NavigateBack()); err != nil {
		return nil, fmt.Errorf("back: %w", err)
	}
	logging.DebugIfErr(chromedp.Run(actionCtx, chromedp.WaitReady("body", chromedp.ByQuery)), "chromedp wait ready after back failed")

	var finalURL string
	logging.DebugIfErr(chromedp.Run(actionCtx, chromedp.Location(&finalURL)), "chromedp get location after back failed")

	return &browsertypes.Response{
		OK:   true,
		Data: map[string]any{"url": finalURL},
	}, nil
}

func (s *chromeSession) handleForward(ctx context.Context) (*browsertypes.Response, error) {
	actionCtx, cancel := timedActionContext(s.ctx, ctx, defaultActionTimeout)
	defer cancel()

	if err := chromedp.Run(actionCtx, chromedp.NavigateForward()); err != nil {
		return nil, fmt.Errorf("forward: %w", err)
	}
	logging.DebugIfErr(chromedp.Run(actionCtx, chromedp.WaitReady("body", chromedp.ByQuery)), "chromedp wait ready after forward failed")

	var finalURL string
	logging.DebugIfErr(chromedp.Run(actionCtx, chromedp.Location(&finalURL)), "chromedp get location after forward failed")

	return &browsertypes.Response{
		OK:   true,
		Data: map[string]any{"url": finalURL},
	}, nil
}

func (s *chromeSession) handleHover(ctx context.Context, params map[string]any) (*browsertypes.Response, error) {
	selector := stringParam(params, "selector")
	if selector == "" {
		return nil, errors.New("hover requires params.selector")
	}
	actionCtx, cancel := timedActionContext(s.ctx, ctx, defaultActionTimeout)
	defer cancel()

	// chromedp does not have a dedicated Hover action, so we use
	// MouseClickXY-style approach: find the element, compute its midpoint,
	// then dispatch a mousemove event via chromedp.MouseEvent isn't directly
	// available either, so we use a JS-based hover.
	err := chromedp.Run(actionCtx,
		chromedp.WaitVisible(selector, chromedp.ByQuery),
		chromedp.ActionFunc(func(ctx context.Context) error {
			var res any
			js := fmt.Sprintf(`(function(){
				var el = document.querySelector(%q);
				if (!el) return 'not_found';
				var evt = new MouseEvent('mouseover', {bubbles:true, cancelable:true});
				el.dispatchEvent(evt);
				evt = new MouseEvent('mouseenter', {bubbles:false, cancelable:false});
				el.dispatchEvent(evt);
				return 'ok';
			})()`, selector)
			return chromedp.Evaluate(js, &res).Do(ctx)
		}),
	)
	if err != nil {
		return nil, fmt.Errorf("hover %q: %w", selector, err)
	}
	return &browsertypes.Response{
		OK:   true,
		Data: map[string]any{"selector": selector, "hovered": true},
	}, nil
}

func (s *chromeSession) handleSelectOption(ctx context.Context, params map[string]any) (*browsertypes.Response, error) {
	selector := stringParam(params, "selector")
	if selector == "" {
		return nil, errors.New("select_option requires params.selector")
	}
	rawValues, ok := params["values"]
	if !ok || rawValues == nil {
		return nil, errors.New("select_option requires params.values")
	}

	// Convert values to []string.
	var values []string
	switch v := rawValues.(type) {
	case []any:
		for _, item := range v {
			values = append(values, fmt.Sprint(item))
		}
	case []string:
		values = v
	default:
		return nil, fmt.Errorf("select_option: values must be an array, got %T", rawValues)
	}

	actionCtx, cancel := timedActionContext(s.ctx, ctx, defaultActionTimeout)
	defer cancel()

	err := chromedp.Run(actionCtx,
		chromedp.WaitVisible(selector, chromedp.ByQuery),
		chromedp.SetValue(selector, values[0], chromedp.ByQuery),
	)
	if err != nil {
		return nil, fmt.Errorf("select_option %q: %w", selector, err)
	}

	// If multiple values, use JS to set them on a multi-select.
	if len(values) > 1 {
		err = chromedp.Run(actionCtx, chromedp.ActionFunc(func(ctx context.Context) error {
			var res any
			js := fmt.Sprintf(`(function(){
				var sel = document.querySelector(%q);
				if (!sel) return 'not_found';
				var vals = %q.split(',');
				Array.from(sel.options).forEach(function(opt) {
					opt.selected = vals.indexOf(opt.value) !== -1;
				});
				sel.dispatchEvent(new Event('change', {bubbles:true}));
				return 'ok';
			})()`, selector, strings.Join(values, ","))
			return chromedp.Evaluate(js, &res).Do(ctx)
		}))
		if err != nil {
			return nil, fmt.Errorf("select_option multi %q: %w", selector, err)
		}
	}

	return &browsertypes.Response{
		OK:   true,
		Data: map[string]any{"selector": selector, "values": values},
	}, nil
}

func (s *chromeSession) handleFill(ctx context.Context, params map[string]any) (*browsertypes.Response, error) {
	selector := stringParam(params, "selector")
	if selector == "" {
		return nil, errors.New("fill requires params.selector")
	}
	value := stringParam(params, "value")

	actionCtx, cancel := timedActionContext(s.ctx, ctx, defaultActionTimeout)
	defer cancel()

	err := chromedp.Run(actionCtx,
		chromedp.WaitVisible(selector, chromedp.ByQuery),
		chromedp.ActionFunc(func(ctx context.Context) error {
			return focusAndPrepareTypeTarget(ctx, selector, false)
		}),
		chromedp.Focus(selector, chromedp.ByQuery),
		chromedp.Clear(selector, chromedp.ByQuery),
		chromedp.SendKeys(selector, value, chromedp.ByQuery),
	)
	if err != nil {
		return nil, fmt.Errorf("fill %q: %w", selector, err)
	}
	return &browsertypes.Response{
		OK:   true,
		Data: map[string]any{"selector": selector, "value": value},
	}, nil
}

func (s *chromeSession) handleDialog(ctx context.Context, params map[string]any) (*browsertypes.Response, error) {
	action := stringParam(params, "action")
	if action == "" {
		return nil, errors.New("handle_dialog requires params.action")
	}
	promptText := stringParam(params, "prompt_text")
	accept := strings.ToLower(action) == "accept"

	actionCtx, cancel := timedActionContext(s.ctx, ctx, defaultActionTimeout)
	defer cancel()

	// Install a JS-level beforeunload / dialog override using CDP.
	err := chromedp.Run(actionCtx, chromedp.ActionFunc(func(ctx context.Context) error {
		err := page.Enable().Do(ctx)
		if err != nil {
			return err
		}
		return page.HandleJavaScriptDialog(accept).WithPromptText(promptText).Do(ctx)
	}))
	if err != nil {
		// If no dialog is currently open, that's acceptable for "set handler" semantics.
		// We only fail on unexpected errors.
		if !strings.Contains(err.Error(), "no dialog") && !strings.Contains(err.Error(), "No dialog") {
			return nil, fmt.Errorf("handle_dialog: %w", err)
		}
	}

	return &browsertypes.Response{
		OK:   true,
		Data: map[string]any{"action": action, "accept": accept},
	}, nil
}

func (s *chromeSession) handlePDF(ctx context.Context, params map[string]any) (*browsertypes.Response, error) {
	format := stringParam(params, "format")
	if format == "" {
		format = "A4"
	}
	landscape := boolParam(params, "landscape", false)

	actionCtx, cancel := timedActionContext(s.ctx, ctx, defaultActionTimeout)
	defer cancel()

	var paperWidth, paperHeight float64
	switch strings.ToLower(format) {
	case "letter":
		paperWidth = 8.5
		paperHeight = 11.0
	default: // A4
		paperWidth = 8.27
		paperHeight = 11.69
	}

	var raw []byte
	err := chromedp.Run(actionCtx, chromedp.ActionFunc(func(ctx context.Context) error {
		var err error
		raw, _, err = page.PrintToPDF().
			WithPaperWidth(paperWidth).
			WithPaperHeight(paperHeight).
			WithLandscape(landscape).
			WithPrintBackground(true).
			Do(ctx)
		return err
	}))
	if err != nil {
		return nil, fmt.Errorf("pdf: %w", err)
	}

	return &browsertypes.Response{
		OK: true,
		Data: map[string]any{
			"mime_type":      "application/pdf",
			"encoding":       "base64",
			"content_base64": base64.StdEncoding.EncodeToString(raw),
			"format":         format,
			"landscape":      landscape,
		},
	}, nil
}

func (s *chromeSession) handleNetworkEnable(ctx context.Context) (*browsertypes.Response, error) {
	actionCtx, cancel := timedActionContext(s.ctx, ctx, defaultActionTimeout)
	defer cancel()

	err := chromedp.Run(actionCtx, chromedp.ActionFunc(func(ctx context.Context) error {
		return network.Enable().Do(ctx)
	}))
	if err != nil {
		return nil, fmt.Errorf("network_enable: %w", err)
	}
	return &browsertypes.Response{
		OK:   true,
		Data: map[string]any{"enabled": true},
	}, nil
}

func (s *chromeSession) handleNetworkRequests(ctx context.Context, params map[string]any) (*browsertypes.Response, error) {
	urlPattern := stringParam(params, "url_pattern")

	actionCtx, cancel := timedActionContext(s.ctx, ctx, defaultActionTimeout)
	defer cancel()

	// Retrieve the browser log entries via performance log which requires network
	// to have been enabled. We use CDP's Network.getResponseBody indirectly.
	// Since chromedp doesn't store requests by default, we evaluate JS
	// performance entries as a practical approach.
	var result any
	js := `(function(){
		var entries = performance.getEntriesByType('resource');
		return entries.map(function(e){
			return {
				url: e.name,
				type: e.initiatorType,
				duration_ms: Math.round(e.duration),
				start_time_ms: Math.round(e.startTime)
			};
		});
	})()`
	if err := chromedp.Run(actionCtx, chromedp.Evaluate(js, &result)); err != nil {
		return nil, fmt.Errorf("network_requests: %w", err)
	}

	// Filter by URL pattern if provided.
	if urlPattern != "" {
		if entries, ok := result.([]any); ok {
			var filtered []any
			for _, entry := range entries {
				if m, ok := entry.(map[string]any); ok {
					if u, ok := m["url"].(string); ok && strings.Contains(u, urlPattern) {
						filtered = append(filtered, entry)
					}
				}
			}
			result = filtered
		}
	}

	return &browsertypes.Response{
		OK:   true,
		Data: map[string]any{"requests": result},
	}, nil
}

func (s *chromeSession) handleScroll(ctx context.Context, params map[string]any) (*browsertypes.Response, error) {
	direction := stringParam(params, "direction")
	if direction == "" {
		direction = "down"
	}
	amount := intParam(params, "amount", defaultScrollAmount)
	selector := stringParam(params, "selector")

	var dx, dy int
	switch direction {
	case "up":
		dy = -amount
	case "down":
		dy = amount
	case "left":
		dx = -amount
	case "right":
		dx = amount
	default:
		return nil, fmt.Errorf("scroll: invalid direction %q", direction)
	}

	actionCtx, cancel := timedActionContext(s.ctx, ctx, defaultActionTimeout)
	defer cancel()

	var js string
	if selector != "" {
		js = fmt.Sprintf(`(function(){
			var el = document.querySelector(%q);
			if (!el) return 'not_found';
			el.scrollBy(%d, %d);
			return 'ok';
		})()`, selector, dx, dy)
	} else {
		js = fmt.Sprintf(`(function(){
			window.scrollBy(%d, %d);
			return 'ok';
		})()`, dx, dy)
	}

	var result any
	if err := chromedp.Run(actionCtx, chromedp.Evaluate(js, &result)); err != nil {
		return nil, fmt.Errorf("scroll: %w", err)
	}
	if result == "not_found" {
		return nil, fmt.Errorf("scroll: element %q not found", selector)
	}

	return &browsertypes.Response{
		OK:   true,
		Data: map[string]any{"direction": direction, "amount": amount},
	}, nil
}

func (s *chromeSession) handleDrag(ctx context.Context, params map[string]any) (*browsertypes.Response, error) {
	sourceSelector := stringParam(params, "source_selector")
	if sourceSelector == "" {
		return nil, errors.New("drag requires params.source_selector")
	}
	targetSelector := stringParam(params, "target_selector")
	if targetSelector == "" {
		return nil, errors.New("drag requires params.target_selector")
	}

	actionCtx, cancel := timedActionContext(s.ctx, ctx, defaultActionTimeout)
	defer cancel()

	// Use JS drag-and-drop simulation via DataTransfer events.
	js := fmt.Sprintf(`(function(){
		var src = document.querySelector(%q);
		var tgt = document.querySelector(%q);
		if (!src) return 'source_not_found';
		if (!tgt) return 'target_not_found';
		var dt = new DataTransfer();
		var dragStart = new DragEvent('dragstart', {bubbles:true, cancelable:true, dataTransfer:dt});
		src.dispatchEvent(dragStart);
		var dragOver = new DragEvent('dragover', {bubbles:true, cancelable:true, dataTransfer:dt});
		tgt.dispatchEvent(dragOver);
		var drop = new DragEvent('drop', {bubbles:true, cancelable:true, dataTransfer:dt});
		tgt.dispatchEvent(drop);
		var dragEnd = new DragEvent('dragend', {bubbles:true, cancelable:true, dataTransfer:dt});
		src.dispatchEvent(dragEnd);
		return 'ok';
	})()`, sourceSelector, targetSelector)

	var result any
	if err := chromedp.Run(actionCtx, chromedp.Evaluate(js, &result)); err != nil {
		return nil, fmt.Errorf("drag: %w", err)
	}
	switch result {
	case "source_not_found":
		return nil, fmt.Errorf("drag: source element %q not found", sourceSelector)
	case "target_not_found":
		return nil, fmt.Errorf("drag: target element %q not found", targetSelector)
	}

	return &browsertypes.Response{
		OK:   true,
		Data: map[string]any{"source_selector": sourceSelector, "target_selector": targetSelector},
	}, nil
}

func (s *chromeSession) handleUpload(ctx context.Context, params map[string]any) (*browsertypes.Response, error) {
	selector := stringParam(params, "selector")
	if selector == "" {
		return nil, errors.New("upload requires params.selector")
	}
	filePath := stringParam(params, "file_path")
	if filePath == "" {
		return nil, errors.New("upload requires params.file_path")
	}

	actionCtx, cancel := timedActionContext(s.ctx, ctx, defaultActionTimeout)
	defer cancel()

	err := chromedp.Run(actionCtx,
		chromedp.SetUploadFiles(selector, []string{filePath}, chromedp.ByQuery),
	)
	if err != nil {
		return nil, fmt.Errorf("upload %q: %w", selector, err)
	}

	return &browsertypes.Response{
		OK:   true,
		Data: map[string]any{"selector": selector, "file_path": filePath},
	}, nil
}

func (s *chromeSession) handleDownload(ctx context.Context, params map[string]any) (*browsertypes.Response, error) {
	rawURL := stringParam(params, "url")
	timeout := durationParamMillis(params, "timeout_ms", defaultDownloadTimeout)

	// Create a temporary directory for downloads.
	downloadDir, err := os.MkdirTemp("", "hopclaw-download-*")
	if err != nil {
		return nil, fmt.Errorf("download: create temp dir: %w", err)
	}

	actionCtx, cancel := timedActionContext(s.ctx, ctx, timeout)
	defer cancel()

	// Enable download behavior via CDP browser domain.
	err = chromedp.Run(actionCtx, chromedp.ActionFunc(func(ctx context.Context) error {
		return cdpbrowser.SetDownloadBehavior(cdpbrowser.SetDownloadBehaviorBehaviorAllowAndName).
			WithDownloadPath(downloadDir).
			WithEventsEnabled(true).
			Do(ctx)
	}))
	if err != nil {
		logging.DebugIfErr(os.RemoveAll(downloadDir), "remove chrome download dir failed")
		return nil, fmt.Errorf("download: set download behavior: %w", err)
	}

	// If a URL was provided, navigate to it to trigger the download.
	if rawURL != "" {
		err = chromedp.Run(actionCtx, chromedp.Navigate(rawURL))
		if err != nil {
			logging.DebugIfErr(os.RemoveAll(downloadDir), "remove chrome download dir failed")
			return nil, fmt.Errorf("download: navigate to %q: %w", rawURL, err)
		}
	}

	// Poll for a downloaded file in the directory.
	var downloadedFile string
	pollDeadline := time.Now().Add(timeout)
	for time.Now().Before(pollDeadline) {
		entries, dirErr := os.ReadDir(downloadDir)
		if dirErr != nil {
			break
		}
		for _, entry := range entries {
			if entry.IsDir() {
				continue
			}
			name := entry.Name()
			// Skip partial Chrome download files.
			if strings.HasSuffix(name, ".crdownload") || strings.HasSuffix(name, ".tmp") {
				continue
			}
			downloadedFile = filepath.Join(downloadDir, name)
			break
		}
		if downloadedFile != "" {
			break
		}
		time.Sleep(downloadPollInterval)
	}

	if downloadedFile == "" {
		logging.DebugIfErr(os.RemoveAll(downloadDir), "remove chrome download dir failed")
		return nil, errors.New("download: timed out waiting for download to complete")
	}

	return &browsertypes.Response{
		OK: true,
		Data: map[string]any{
			"file_path": downloadedFile,
		},
	}, nil
}

func (s *chromeSession) handleNewTab(ctx context.Context, params map[string]any) (*browsertypes.Response, error) {
	rawURL := stringParam(params, "url")
	if rawURL == "" {
		rawURL = "about:blank"
	}

	actionCtx, cancel := timedActionContext(s.ctx, ctx, defaultActionTimeout)
	defer cancel()

	var targetID string
	err := chromedp.Run(actionCtx, chromedp.ActionFunc(func(ctx context.Context) error {
		id, createErr := target.CreateTarget(rawURL).Do(ctx)
		if createErr != nil {
			return createErr
		}
		targetID = string(id)
		return nil
	}))
	if err != nil {
		return nil, fmt.Errorf("new_tab: %w", err)
	}

	return &browsertypes.Response{
		OK: true,
		Data: map[string]any{
			"target_id": targetID,
		},
	}, nil
}

func (s *chromeSession) handleSwitchTab(ctx context.Context, params map[string]any) (*browsertypes.Response, error) {
	targetID := stringParam(params, "target_id")
	if targetID == "" {
		return nil, errors.New("switch_tab requires params.target_id")
	}

	actionCtx, cancel := timedActionContext(s.ctx, ctx, defaultActionTimeout)
	defer cancel()

	err := chromedp.Run(actionCtx, chromedp.ActionFunc(func(ctx context.Context) error {
		return target.ActivateTarget(target.ID(targetID)).Do(ctx)
	}))
	if err != nil {
		return nil, fmt.Errorf("switch_tab %q: %w", targetID, err)
	}

	return &browsertypes.Response{
		OK:   true,
		Data: map[string]any{"target_id": targetID},
	}, nil
}

func (s *chromeSession) handleCloseTab(ctx context.Context, params map[string]any) (*browsertypes.Response, error) {
	targetID := stringParam(params, "target_id")
	if targetID == "" {
		return nil, errors.New("close_tab requires params.target_id")
	}

	actionCtx, cancel := timedActionContext(s.ctx, ctx, defaultActionTimeout)
	defer cancel()

	// chromedp's Target.Execute blocks "Target.closeTarget" and tells
	// callers to cancel the Go context instead.  For tabs created via
	// target.CreateTarget (new_tab) there is no dedicated Go context to
	// cancel.  Bypass the restriction by sending the CDP command through
	// the Browser-level executor, which does not block it.
	err := chromedp.Run(actionCtx, chromedp.ActionFunc(func(ctx context.Context) error {
		c := chromedp.FromContext(ctx)
		if c == nil || c.Browser == nil {
			return errors.New("cannot obtain browser executor")
		}
		return c.Browser.Execute(ctx, target.CommandCloseTarget,
			target.CloseTarget(target.ID(targetID)), nil)
	}))
	if err != nil {
		return nil, fmt.Errorf("close_tab %q: %w", targetID, err)
	}

	return &browsertypes.Response{
		OK:   true,
		Data: map[string]any{"target_id": targetID, "closed": true},
	}, nil
}

func (s *chromeSession) handleElementText(ctx context.Context, params map[string]any) (*browsertypes.Response, error) {
	selector := stringParam(params, "selector")
	if selector == "" {
		return nil, errors.New("element_text requires params.selector")
	}

	actionCtx, cancel := timedActionContext(s.ctx, ctx, defaultActionTimeout)
	defer cancel()

	var text string
	err := chromedp.Run(actionCtx,
		chromedp.Text(selector, &text, chromedp.ByQuery),
	)
	if err != nil {
		return nil, fmt.Errorf("element_text %q: %w", selector, err)
	}

	return &browsertypes.Response{
		OK: true,
		Data: map[string]any{
			"selector": selector,
			"text":     text,
		},
	}, nil
}

func (s *chromeSession) handleElementAttr(ctx context.Context, params map[string]any) (*browsertypes.Response, error) {
	selector := stringParam(params, "selector")
	if selector == "" {
		return nil, errors.New("element_attr requires params.selector")
	}
	attribute := stringParam(params, "attribute")
	if attribute == "" {
		return nil, errors.New("element_attr requires params.attribute")
	}

	actionCtx, cancel := timedActionContext(s.ctx, ctx, defaultActionTimeout)
	defer cancel()

	var value string
	var found bool
	err := chromedp.Run(actionCtx,
		chromedp.AttributeValue(selector, attribute, &value, &found, chromedp.ByQuery),
	)
	if err != nil {
		return nil, fmt.Errorf("element_attr %q.%s: %w", selector, attribute, err)
	}

	return &browsertypes.Response{
		OK: true,
		Data: map[string]any{
			"selector":  selector,
			"attribute": attribute,
			"value":     value,
			"found":     found,
		},
	}, nil
}

func (s *chromeSession) handleElementVisible(ctx context.Context, params map[string]any) (*browsertypes.Response, error) {
	selector := stringParam(params, "selector")
	if selector == "" {
		return nil, errors.New("element_visible requires params.selector")
	}

	// Use a short timeout to probe visibility without blocking.
	actionCtx, cancel := timedActionContext(s.ctx, ctx, visibilityCheckTimeout)
	defer cancel()

	err := chromedp.Run(actionCtx, chromedp.WaitVisible(selector, chromedp.ByQuery))
	visible := err == nil

	return &browsertypes.Response{
		OK: true,
		Data: map[string]any{
			"selector": selector,
			"visible":  visible,
		},
	}, nil
}

// namedKeys maps human-readable key names to the chromedp kb constant
// values.  chromedp.KeyEvent interprets each rune through its key table,
// so passing the literal string "Enter" types five characters E-n-t-e-r.
// This map ensures that common key names are translated to the correct
// single-rune constants from the chromedp/kb package.
var namedKeys = map[string]string{
	"enter":      kb.Enter,
	"return":     kb.Enter,
	"tab":        kb.Tab,
	"escape":     kb.Escape,
	"esc":        kb.Escape,
	"backspace":  kb.Backspace,
	"delete":     kb.Delete,
	"arrowup":    kb.ArrowUp,
	"arrowdown":  kb.ArrowDown,
	"arrowleft":  kb.ArrowLeft,
	"arrowright": kb.ArrowRight,
	"home":       kb.Home,
	"end":        kb.End,
	"pageup":     kb.PageUp,
	"pagedown":   kb.PageDown,
	"f1":         kb.F1,
	"f2":         kb.F2,
	"f3":         kb.F3,
	"f4":         kb.F4,
	"f5":         kb.F5,
	"f6":         kb.F6,
	"f7":         kb.F7,
	"f8":         kb.F8,
	"f9":         kb.F9,
	"f10":        kb.F10,
	"f11":        kb.F11,
	"f12":        kb.F12,
	"insert":     kb.Insert,
	"space":      " ",
	"control":    kb.Control,
	"alt":        kb.Alt,
	"shift":      kb.Shift,
	"meta":       kb.Meta,
}

// resolveKeys translates a human-readable key spec into the string that
// chromedp.KeyEvent expects.  It handles:
//   - Single named keys:    "Enter" → kb.Enter ("\r")
//   - Modifier combos:      "Control+a" → kb.Control + "a"
//   - Plain text passthrough: "hello" → "hello"
func resolveKeys(raw string) string {
	// Quick check: single named key (case-insensitive).
	if v, ok := namedKeys[strings.ToLower(raw)]; ok {
		return v
	}

	// Modifier combo: "Control+a", "Shift+Enter", etc.
	if strings.Contains(raw, "+") {
		parts := strings.Split(raw, "+")
		var resolved strings.Builder
		for _, p := range parts {
			p = strings.TrimSpace(p)
			if v, ok := namedKeys[strings.ToLower(p)]; ok {
				resolved.WriteString(v)
			} else {
				resolved.WriteString(p)
			}
		}
		return resolved.String()
	}

	return raw
}

func (s *chromeSession) handleKeyboard(ctx context.Context, params map[string]any) (*browsertypes.Response, error) {
	keys := stringParam(params, "keys")
	if keys == "" {
		return nil, errors.New("keyboard requires params.keys")
	}

	actionCtx, cancel := timedActionContext(s.ctx, ctx, defaultActionTimeout)
	defer cancel()

	resolved := resolveKeys(keys)
	err := chromedp.Run(actionCtx, chromedp.KeyEvent(resolved))
	if err != nil {
		return nil, fmt.Errorf("keyboard %q: %w", keys, err)
	}

	return &browsertypes.Response{
		OK:   true,
		Data: map[string]any{"keys": keys},
	}, nil
}

func (s *chromeSession) handleIframe(ctx context.Context, params map[string]any) (*browsertypes.Response, error) {
	selector := stringParam(params, "selector")

	actionCtx, cancel := timedActionContext(s.ctx, ctx, defaultActionTimeout)
	defer cancel()

	if selector == "" {
		// Switch back to the main frame by evaluating in the top-level context.
		var result any
		js := `(function(){ return 'main'; })()`
		if err := chromedp.Run(actionCtx, chromedp.Evaluate(js, &result)); err != nil {
			return nil, fmt.Errorf("iframe: switch to main frame: %w", err)
		}
		return &browsertypes.Response{
			OK:   true,
			Data: map[string]any{"frame": "main"},
		}, nil
	}

	// Find the iframe element and get its contentDocument via JS evaluation.
	// We use chromedp to locate the iframe and switch to its context.
	var frameID any
	js := fmt.Sprintf(`(function(){
		var iframe = document.querySelector(%q);
		if (!iframe) return 'not_found';
		if (iframe.tagName.toLowerCase() !== 'iframe') return 'not_iframe';
		return 'ok';
	})()`, selector)
	if err := chromedp.Run(actionCtx, chromedp.Evaluate(js, &frameID)); err != nil {
		return nil, fmt.Errorf("iframe %q: %w", selector, err)
	}
	switch frameID {
	case "not_found":
		return nil, fmt.Errorf("iframe: element %q not found", selector)
	case "not_iframe":
		return nil, fmt.Errorf("iframe: element %q is not an iframe", selector)
	}

	return &browsertypes.Response{
		OK:   true,
		Data: map[string]any{"frame": selector},
	}, nil
}

// ---------------------------------------------------------------------------
// Tracing handlers
// ---------------------------------------------------------------------------

const (
	defaultTraceCategories = "devtools.timeline,blink.user_timing"
)

func (s *chromeSession) handleTraceStart(ctx context.Context, params map[string]any) (*browsertypes.Response, error) {
	s.tracingMu.Lock()
	if s.tracingActive {
		s.tracingMu.Unlock()
		return nil, errors.New("trace_start: tracing is already active")
	}
	s.tracingActive = true
	s.traceEvents = nil
	s.tracingMu.Unlock()

	// Parse categories.
	categories := defaultTraceCategories
	if raw, ok := params["categories"]; ok {
		switch v := raw.(type) {
		case []any:
			parts := make([]string, 0, len(v))
			for _, item := range v {
				parts = append(parts, fmt.Sprint(item))
			}
			if len(parts) > 0 {
				categories = strings.Join(parts, ",")
			}
		case []string:
			if len(v) > 0 {
				categories = strings.Join(v, ",")
			}
		case string:
			if v != "" {
				categories = v
			}
		}
	}

	// Install data-collected listener before starting the trace.
	chromedp.ListenTarget(s.ctx, func(ev any) {
		switch ev := ev.(type) {
		case *tracing.EventDataCollected:
			s.tracingMu.Lock()
			defer s.tracingMu.Unlock()
			if !s.tracingActive {
				return
			}
			for _, v := range ev.Value {
				raw := json.RawMessage(v)
				s.traceEvents = append(s.traceEvents, raw)
			}
		}
	})

	actionCtx, cancel := timedActionContext(s.ctx, ctx, defaultActionTimeout)
	defer cancel()

	err := chromedp.Run(actionCtx, chromedp.ActionFunc(func(ctx context.Context) error {
		return tracing.Start().
			WithTraceConfig(&tracing.TraceConfig{
				IncludedCategories: strings.Split(categories, ","),
			}).
			Do(ctx)
	}))
	if err != nil {
		s.tracingMu.Lock()
		s.tracingActive = false
		s.tracingMu.Unlock()
		return nil, fmt.Errorf("trace_start: %w", err)
	}

	return &browsertypes.Response{
		OK:   true,
		Data: map[string]any{"tracing": true, "categories": categories},
	}, nil
}

func (s *chromeSession) handleTraceStop(ctx context.Context) (*browsertypes.Response, error) {
	s.tracingMu.Lock()
	if !s.tracingActive {
		s.tracingMu.Unlock()
		return nil, errors.New("trace_stop: tracing is not active")
	}
	s.tracingMu.Unlock()

	actionCtx, cancel := timedActionContext(s.ctx, ctx, defaultActionTimeout)
	defer cancel()

	err := chromedp.Run(actionCtx, chromedp.ActionFunc(func(ctx context.Context) error {
		return tracing.End().Do(ctx)
	}))
	if err != nil {
		return nil, fmt.Errorf("trace_stop: %w", err)
	}

	// Allow a brief period for dataCollected events to arrive.
	time.Sleep(500 * time.Millisecond)

	s.tracingMu.Lock()
	s.tracingActive = false
	events := s.traceEvents
	s.traceEvents = nil
	s.tracingMu.Unlock()

	// Encode trace data as base64 JSON array.
	traceData, marshalErr := json.Marshal(events)
	if marshalErr != nil {
		return nil, fmt.Errorf("trace_stop: marshal trace events: %w", marshalErr)
	}
	encoded := base64.StdEncoding.EncodeToString(traceData)

	return &browsertypes.Response{
		OK: true,
		Data: map[string]any{
			"event_count":    len(events),
			"encoding":       "base64",
			"content_base64": encoded,
		},
	}, nil
}

// ---------------------------------------------------------------------------
// HAR recording handlers
// ---------------------------------------------------------------------------

func (s *chromeSession) handleHARStart(ctx context.Context) (*browsertypes.Response, error) {
	s.harMu.Lock()
	if s.harRecording {
		s.harMu.Unlock()
		return nil, errors.New("har_start: HAR recording is already active")
	}
	s.harRecording = true
	s.harEntries = nil
	s.harRequests = make(map[network.RequestID]*harRequestInfo)
	s.harMu.Unlock()

	actionCtx, cancel := timedActionContext(s.ctx, ctx, defaultActionTimeout)
	defer cancel()

	// Enable network domain to receive events.
	err := chromedp.Run(actionCtx, chromedp.ActionFunc(func(ctx context.Context) error {
		return network.Enable().Do(ctx)
	}))
	if err != nil {
		s.harMu.Lock()
		s.harRecording = false
		s.harMu.Unlock()
		return nil, fmt.Errorf("har_start: %w", err)
	}

	// Listen for network events.
	chromedp.ListenTarget(s.ctx, func(ev any) {
		s.harMu.Lock()
		defer s.harMu.Unlock()
		if !s.harRecording {
			return
		}
		switch ev := ev.(type) {
		case *network.EventRequestWillBeSent:
			var startedAt time.Time
			if ev.Timestamp != nil {
				startedAt = ev.Timestamp.Time()
			}
			s.harRequests[ev.RequestID] = &harRequestInfo{
				URL:       ev.Request.URL,
				Method:    ev.Request.Method,
				StartedAt: startedAt,
			}
		case *network.EventResponseReceived:
			reqInfo, ok := s.harRequests[ev.RequestID]
			if !ok {
				return
			}
			var duration float64
			if ev.Timestamp != nil && !reqInfo.StartedAt.IsZero() {
				duration = ev.Timestamp.Time().Sub(reqInfo.StartedAt).Seconds()
			}
			entry := harEntry{
				URL:          reqInfo.URL,
				Method:       reqInfo.Method,
				Status:       ev.Response.Status,
				ResponseSize: ev.Response.EncodedDataLength,
				StartedAt:    reqInfo.StartedAt,
				Duration:     duration,
			}
			s.harEntries = append(s.harEntries, entry)
			delete(s.harRequests, ev.RequestID)
		}
	})

	return &browsertypes.Response{
		OK:   true,
		Data: map[string]any{"har_recording": true},
	}, nil
}

func (s *chromeSession) handleHARStop(ctx context.Context, params map[string]any) (*browsertypes.Response, error) {
	s.harMu.Lock()
	if !s.harRecording {
		s.harMu.Unlock()
		return nil, errors.New("har_stop: HAR recording is not active")
	}
	s.harRecording = false
	entries := s.harEntries
	s.harEntries = nil
	s.harRequests = nil
	s.harMu.Unlock()

	if entries == nil {
		entries = []harEntry{}
	}

	format := stringParam(params, "format")
	if format == "" {
		format = "summary"
	}

	if format == "summary" {
		// Return summary: list of entries with key fields.
		summaries := make([]map[string]any, 0, len(entries))
		for _, e := range entries {
			summaries = append(summaries, map[string]any{
				"url":           e.URL,
				"method":        e.Method,
				"status":        int(e.Status),
				"response_size": int64(e.ResponseSize),
				"duration":      e.Duration,
			})
		}
		return &browsertypes.Response{
			OK: true,
			Data: map[string]any{
				"format":      "summary",
				"entry_count": len(entries),
				"entries":     summaries,
			},
		}, nil
	}

	// Full format: return all fields.
	return &browsertypes.Response{
		OK: true,
		Data: map[string]any{
			"format":      "full",
			"entry_count": len(entries),
			"entries":     entries,
		},
	}, nil
}

// ---------------------------------------------------------------------------
// Console capture handlers
// ---------------------------------------------------------------------------

func (s *chromeSession) handleConsoleStart(ctx context.Context) (*browsertypes.Response, error) {
	s.consoleMu.Lock()
	if s.consoleCapturing {
		s.consoleMu.Unlock()
		return nil, errors.New("console_start: console capture is already active")
	}
	s.consoleCapturing = true
	s.consoleMessages = nil
	s.consoleMu.Unlock()

	actionCtx, cancel := timedActionContext(s.ctx, ctx, defaultActionTimeout)
	defer cancel()

	// Ensure Runtime domain is enabled so we receive console events.
	err := chromedp.Run(actionCtx, chromedp.ActionFunc(func(ctx context.Context) error {
		return cdpruntime.Enable().Do(ctx)
	}))
	if err != nil {
		s.consoleMu.Lock()
		s.consoleCapturing = false
		s.consoleMu.Unlock()
		return nil, fmt.Errorf("console_start: %w", err)
	}

	chromedp.ListenTarget(s.ctx, func(ev any) {
		switch ev := ev.(type) {
		case *cdpruntime.EventConsoleAPICalled:
			s.consoleMu.Lock()
			defer s.consoleMu.Unlock()
			if !s.consoleCapturing {
				return
			}

			// Build text from args.
			var parts []string
			for _, arg := range ev.Args {
				if arg.Description != "" {
					parts = append(parts, arg.Description)
				} else if len(arg.Value) > 0 {
					parts = append(parts, string(arg.Value))
				} else {
					parts = append(parts, fmt.Sprintf("[%s]", arg.Type))
				}
			}

			msg := consoleMessage{
				Level: string(ev.Type),
				Text:  strings.Join(parts, " "),
			}

			// Extract source location if available.
			if ev.StackTrace != nil && len(ev.StackTrace.CallFrames) > 0 {
				frame := ev.StackTrace.CallFrames[0]
				msg.URL = frame.URL
				msg.Line = int(frame.LineNumber)
				msg.Column = int(frame.ColumnNumber)
			}

			s.consoleMessages = append(s.consoleMessages, msg)
		}
	})

	return &browsertypes.Response{
		OK:   true,
		Data: map[string]any{"console_capturing": true},
	}, nil
}

func (s *chromeSession) handleConsoleMessages(_ context.Context, params map[string]any) (*browsertypes.Response, error) {
	clear := boolParam(params, "clear", false)

	s.consoleMu.Lock()
	messages := s.consoleMessages
	if clear {
		s.consoleMessages = nil
	}
	s.consoleMu.Unlock()

	if messages == nil {
		messages = []consoleMessage{}
	}

	result := make([]map[string]any, 0, len(messages))
	for _, m := range messages {
		entry := map[string]any{
			"level": m.Level,
			"text":  m.Text,
		}
		if m.URL != "" {
			entry["url"] = m.URL
		}
		if m.Line > 0 {
			entry["line"] = m.Line
		}
		if m.Column > 0 {
			entry["column"] = m.Column
		}
		result = append(result, entry)
	}

	return &browsertypes.Response{
		OK: true,
		Data: map[string]any{
			"message_count": len(result),
			"messages":      result,
		},
	}, nil
}

// ---------------------------------------------------------------------------
// Performance metrics handler
// ---------------------------------------------------------------------------

func (s *chromeSession) handlePerformanceMetrics(ctx context.Context) (*browsertypes.Response, error) {
	actionCtx, cancel := timedActionContext(s.ctx, ctx, defaultActionTimeout)
	defer cancel()

	// Enable the Performance domain, then retrieve metrics.
	var metrics []*performance.Metric
	err := chromedp.Run(actionCtx, chromedp.ActionFunc(func(ctx context.Context) error {
		if enableErr := performance.Enable().Do(ctx); enableErr != nil {
			return enableErr
		}
		var getErr error
		metrics, getErr = performance.GetMetrics().Do(ctx)
		return getErr
	}))
	if err != nil {
		return nil, fmt.Errorf("performance_metrics: %w", err)
	}

	metricsMap := make(map[string]any, len(metrics))
	for _, m := range metrics {
		metricsMap[m.Name] = m.Value
	}

	return &browsertypes.Response{
		OK: true,
		Data: map[string]any{
			"metrics": metricsMap,
		},
	}, nil
}

func timedActionContext(sessionCtx, requestCtx context.Context, timeout time.Duration) (context.Context, context.CancelFunc) {
	baseCtx, baseCancel := context.WithCancel(sessionCtx)
	go func() {
		select {
		case <-requestCtx.Done():
			baseCancel()
		case <-baseCtx.Done():
		}
	}()

	var (
		actionCtx context.Context
		cancel    context.CancelFunc
	)
	if deadline, ok := requestCtx.Deadline(); ok {
		actionCtx, cancel = context.WithDeadline(baseCtx, deadline)
	} else {
		actionCtx, cancel = context.WithTimeout(baseCtx, timeout)
	}
	return actionCtx, func() {
		cancel()
		baseCancel()
	}
}

func findChromeBinary() (string, error) {
	candidates := []string{
		"google-chrome",
		"google-chrome-stable",
		"chromium",
		"chromium-browser",
		"chrome",
		"msedge",
		"microsoft-edge",
	}

	switch runtime.GOOS {
	case "darwin":
		candidates = append(candidates,
			"/Applications/Google Chrome.app/Contents/MacOS/Google Chrome",
			"/Applications/Chromium.app/Contents/MacOS/Chromium",
			"/Applications/Microsoft Edge.app/Contents/MacOS/Microsoft Edge",
		)
	case "windows":
		candidates = append(candidates,
			`C:\Program Files\Google\Chrome\Application\chrome.exe`,
			`C:\Program Files (x86)\Google\Chrome\Application\chrome.exe`,
			`C:\Program Files\Microsoft\Edge\Application\msedge.exe`,
			`C:\Program Files (x86)\Microsoft\Edge\Application\msedge.exe`,
		)
	}

	for _, candidate := range candidates {
		if filepath.IsAbs(candidate) {
			if _, err := os.Stat(candidate); err == nil {
				return candidate, nil
			}
			continue
		}
		resolved, err := exec.LookPath(candidate)
		if err == nil {
			return resolved, nil
		}
	}
	return "", errors.New("could not find a Chrome/Chromium/Edge browser binary; set -chrome-path explicitly")
}

func stringParam(params map[string]any, key string) string {
	if len(params) == 0 {
		return ""
	}
	value, ok := params[key]
	if !ok || value == nil {
		return ""
	}
	switch v := value.(type) {
	case string:
		return strings.TrimSpace(v)
	case fmt.Stringer:
		return strings.TrimSpace(v.String())
	default:
		return strings.TrimSpace(fmt.Sprint(v))
	}
}

func intParam(params map[string]any, key string, fallback int) int {
	if len(params) == 0 {
		return fallback
	}
	value, ok := params[key]
	if !ok || value == nil {
		return fallback
	}
	switch v := value.(type) {
	case int:
		if v > 0 {
			return v
		}
	case int64:
		if v > 0 {
			return int(v)
		}
	case int32:
		if v > 0 {
			return int(v)
		}
	case float64:
		if v > 0 {
			return int(v)
		}
	case float32:
		if v > 0 {
			return int(v)
		}
	}
	return fallback
}

func boolParam(params map[string]any, key string, fallback bool) bool {
	if len(params) == 0 {
		return fallback
	}
	value, ok := params[key]
	if !ok || value == nil {
		return fallback
	}
	switch v := value.(type) {
	case bool:
		return v
	case string:
		switch strings.ToLower(strings.TrimSpace(v)) {
		case "true", "1", "yes", "y":
			return true
		case "false", "0", "no", "n":
			return false
		}
	}
	return fallback
}

func durationParamMillis(params map[string]any, key string, fallback time.Duration) time.Duration {
	millis := intParam(params, key, 0)
	if millis <= 0 {
		return fallback
	}
	return time.Duration(millis) * time.Millisecond
}

func screenshotQuality(v int) int {
	if v <= 0 {
		return defaultScreenshotQaul
	}
	if v > 100 {
		return 100
	}
	return v
}

func floatParam(params map[string]any, key string, fallback float64) float64 {
	if len(params) == 0 {
		return fallback
	}
	value, ok := params[key]
	if !ok || value == nil {
		return fallback
	}
	switch v := value.(type) {
	case float64:
		return v
	case float32:
		return float64(v)
	case int:
		return float64(v)
	case int64:
		return float64(v)
	}
	return fallback
}
