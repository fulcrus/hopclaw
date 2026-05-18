package bootstrap

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	browserclient "github.com/fulcrus/hopclaw/browserapi/client"
	"github.com/fulcrus/hopclaw/config"
	desktopclient "github.com/fulcrus/hopclaw/desktopapi/client"
	"github.com/fulcrus/hopclaw/gateway"
	"github.com/fulcrus/hopclaw/internal/browserd"
	"github.com/fulcrus/hopclaw/internal/desktopd"
)

const (
	defaultManagedBrowserIdleTimeout = 90 * time.Second
	defaultManagedDesktopIdleTimeout = 45 * time.Second
)

type managedHelpers struct {
	Browser *managedHelperSupervisor
	Desktop *managedHelperSupervisor
}

type managedEndpoint struct {
	BaseURL   string
	AuthToken string
}

type managedHelperInstance struct {
	managedEndpoint
	close func(context.Context) error
}

type managedHelperSupervisor struct {
	name        string
	idleTimeout time.Duration
	launch      func(context.Context) (*managedHelperInstance, error)
	client      *http.Client

	mu        sync.Mutex
	instance  *managedHelperInstance
	lastUse   time.Time
	starting  bool
	waitCh    chan struct{}
	closed    bool
	stopCh    chan struct{}
	stoppedCh chan struct{}
}

func initManagedHelpers(cfg config.Config) *managedHelpers {
	root := managedHelperRoot(cfg)
	helpers := &managedHelpers{}
	if browserHostManaged(cfg.Hosts.Browser) {
		helpers.Browser = newManagedHelperSupervisor("browser", managedIdleTimeout(cfg.Hosts.Browser.IdleTimeout, defaultManagedBrowserIdleTimeout), func(ctx context.Context) (*managedHelperInstance, error) {
			return startManagedBrowserHelper(ctx, root, cfg.Hosts.Browser)
		})
	}
	if desktopHostManaged(cfg.Hosts.Desktop) {
		helpers.Desktop = newManagedHelperSupervisor("desktop", managedIdleTimeout(cfg.Hosts.Desktop.IdleTimeout, defaultManagedDesktopIdleTimeout), func(ctx context.Context) (*managedHelperInstance, error) {
			return startManagedDesktopHelper(ctx, cfg.Hosts.Desktop)
		})
	}
	return helpers
}

func newBrowserHostClient(cfg config.BrowserHostConfig, helper *managedHelperSupervisor) *browserclient.Client {
	if helper != nil {
		return browserclient.NewWithConfig(browserclient.Config{
			EndpointResolver: func(ctx context.Context) (browserclient.Endpoint, error) {
				endpoint, err := helper.Endpoint(ctx)
				if err != nil {
					return browserclient.Endpoint{}, err
				}
				return browserclient.Endpoint{BaseURL: endpoint.BaseURL, AuthToken: endpoint.AuthToken}, nil
			},
		})
	}
	if !browserHostConfigured(cfg) {
		return nil
	}
	return browserclient.NewWithConfig(browserclient.Config{
		BaseURL:   cfg.BaseURL,
		AuthToken: cfg.AuthToken,
	})
}

func newDesktopHostClient(cfg config.DesktopHostConfig, helper *managedHelperSupervisor) *desktopclient.Client {
	if helper != nil {
		return desktopclient.NewWithConfig(desktopclient.Config{
			EndpointResolver: func(ctx context.Context) (desktopclient.Endpoint, error) {
				endpoint, err := helper.Endpoint(ctx)
				if err != nil {
					return desktopclient.Endpoint{}, err
				}
				return desktopclient.Endpoint{BaseURL: endpoint.BaseURL, AuthToken: endpoint.AuthToken}, nil
			},
		})
	}
	if !desktopHostConfigured(cfg) {
		return nil
	}
	return desktopclient.NewWithConfig(desktopclient.Config{
		BaseURL:   cfg.BaseURL,
		AuthToken: cfg.AuthToken,
	})
}

func newManagedHelperSupervisor(name string, idleTimeout time.Duration, launch func(context.Context) (*managedHelperInstance, error)) *managedHelperSupervisor {
	h := &managedHelperSupervisor{
		name:        name,
		idleTimeout: idleTimeout,
		launch:      launch,
		client:      &http.Client{Timeout: 2 * time.Second},
		stopCh:      make(chan struct{}),
		stoppedCh:   make(chan struct{}),
	}
	go h.idleLoop()
	return h
}

func (h *managedHelperSupervisor) Endpoint(ctx context.Context) (managedEndpoint, error) {
	for {
		h.mu.Lock()
		if h.closed {
			h.mu.Unlock()
			return managedEndpoint{}, fmt.Errorf("%s helper is stopped", h.name)
		}
		h.lastUse = time.Now().UTC()
		if h.starting {
			waitCh := h.waitCh
			h.mu.Unlock()
			select {
			case <-ctx.Done():
				return managedEndpoint{}, ctx.Err()
			case <-waitCh:
				continue
			}
		}
		if inst := h.instance; inst != nil {
			endpoint := inst.managedEndpoint
			h.mu.Unlock()
			if _, err := h.sessionCount(ctx, endpoint); err == nil {
				return endpoint, nil
			}
			h.stopInstance(inst, "unhealthy")
			continue
		}
		h.starting = true
		h.waitCh = make(chan struct{})
		waitCh := h.waitCh
		h.mu.Unlock()

		inst, err := h.launch(ctx)
		if err == nil {
			err = h.waitHealthy(ctx, inst.managedEndpoint)
		}
		if err != nil && inst != nil {
			shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			_ = inst.close(shutdownCtx)
			cancel()
		}

		h.mu.Lock()
		shouldClose := err == nil && h.closed
		if err == nil && !h.closed {
			h.instance = inst
			h.lastUse = time.Now().UTC()
		}
		h.starting = false
		close(waitCh)
		h.waitCh = nil
		h.mu.Unlock()
		if shouldClose && inst != nil {
			shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			_ = inst.close(shutdownCtx)
			cancel()
		}

		if err != nil {
			return managedEndpoint{}, fmt.Errorf("start %s helper: %w", h.name, err)
		}
	}
}

func (h *managedHelperSupervisor) Stop(ctx context.Context) error {
	h.mu.Lock()
	if h.closed {
		h.mu.Unlock()
		return nil
	}
	h.closed = true
	inst := h.instance
	h.instance = nil
	close(h.stopCh)
	h.mu.Unlock()
	if inst != nil {
		if err := inst.close(ctx); err != nil {
			return err
		}
	}
	<-h.stoppedCh
	return nil
}

func (h *managedHelperSupervisor) idleLoop() {
	defer close(h.stoppedCh)
	interval := 15 * time.Second
	if h.idleTimeout > 0 && h.idleTimeout < interval {
		interval = h.idleTimeout
	}
	if interval <= 0 {
		interval = 15 * time.Second
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-h.stopCh:
			return
		case <-ticker.C:
			h.maybeStopIdle()
		}
	}
}

func (h *managedHelperSupervisor) maybeStopIdle() {
	if h.idleTimeout <= 0 {
		return
	}
	h.mu.Lock()
	inst := h.instance
	lastUse := h.lastUse
	closed := h.closed
	h.mu.Unlock()
	if closed || inst == nil {
		return
	}
	if time.Since(lastUse) < h.idleTimeout {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	sessionCount, err := h.sessionCount(ctx, inst.managedEndpoint)
	if err != nil {
		h.stopInstance(inst, "health check failed")
		return
	}
	if sessionCount == 0 {
		h.stopInstance(inst, "idle")
	}
}

func (h *managedHelperSupervisor) waitHealthy(ctx context.Context, endpoint managedEndpoint) error {
	deadline := time.Now().Add(10 * time.Second)
	for {
		if _, err := h.sessionCount(ctx, endpoint); err == nil {
			return nil
		}
		if time.Now().After(deadline) {
			break
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(150 * time.Millisecond):
		}
	}
	return fmt.Errorf("%s helper did not become ready", h.name)
}

func (h *managedHelperSupervisor) sessionCount(ctx context.Context, endpoint managedEndpoint) (int, error) {
	healthCtx := ctx
	if _, ok := ctx.Deadline(); !ok {
		var cancel context.CancelFunc
		healthCtx, cancel = context.WithTimeout(ctx, 2*time.Second)
		defer cancel()
	}
	req, err := http.NewRequestWithContext(healthCtx, http.MethodGet, endpoint.BaseURL+"/healthz", nil)
	if err != nil {
		return 0, fmt.Errorf("create health request: %w", err)
	}
	if endpoint.AuthToken != "" {
		req.Header.Set("Authorization", "Bearer "+endpoint.AuthToken)
	}
	resp, err := h.client.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("health status %s", resp.Status)
	}
	var payload struct {
		SessionCount int `json:"session_count"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return 0, fmt.Errorf("decode health response: %w", err)
	}
	return payload.SessionCount, nil
}

func (h *managedHelperSupervisor) stopInstance(inst *managedHelperInstance, reason string) {
	h.mu.Lock()
	if h.instance != inst {
		h.mu.Unlock()
		return
	}
	h.instance = nil
	h.mu.Unlock()
	log.Info("managed helper stopped", "helper", h.name, "reason", reason)
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := inst.close(shutdownCtx); err != nil {
		log.Warn("managed helper stop failed", "helper", h.name, "error", err)
	}
}

func startManagedBrowserHelper(_ context.Context, root string, cfg config.BrowserHostConfig) (*managedHelperInstance, error) {
	token := strings.TrimSpace(cfg.AuthToken)
	if token == "" {
		var err error
		token, err = randomHelperToken()
		if err != nil {
			return nil, err
		}
	}
	engine, err := browserd.NewChromeEngine(browserd.ChromeConfig{
		ExecPath:  strings.TrimSpace(cfg.ChromePath),
		Headless:  boolValue(cfg.Headless, false),
		NoSandbox: boolValue(cfg.NoSandbox, false),
	})
	if err != nil {
		return nil, err
	}
	manager := browserd.NewManager(engine)
	api := browserd.NewServer(manager, token)
	if err := api.WithProfilesDir(root); err != nil {
		return nil, err
	}
	return startManagedHTTPServer("browser", token, api.Handler(), api.Close)
}

func startManagedDesktopHelper(_ context.Context, cfg config.DesktopHostConfig) (*managedHelperInstance, error) {
	token := strings.TrimSpace(cfg.AuthToken)
	if token == "" {
		var err error
		token, err = randomHelperToken()
		if err != nil {
			return nil, err
		}
	}
	engine, err := desktopd.NewDefaultEngine()
	if err != nil {
		return nil, err
	}
	manager := desktopd.NewManager(engine)
	api := desktopd.NewServer(manager, token)
	return startManagedHTTPServer("desktop", token, api.Handler(), api.Close)
}

func startManagedHTTPServer(name, token string, handler http.Handler, closer func(context.Context) error) (*managedHelperInstance, error) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, fmt.Errorf("listen %s helper: %w", name, err)
	}
	srv := &http.Server{
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      60 * time.Second,
		IdleTimeout:       2 * time.Minute,
	}
	go func() {
		if err := srv.Serve(listener); err != nil && err != http.ErrServerClosed {
			log.Warn("managed helper serve failed", "helper", name, "error", err)
		}
	}()
	endpoint := managedEndpoint{BaseURL: "http://" + listener.Addr().String(), AuthToken: token}
	log.Info("managed helper started", "helper", name, "base_url", endpoint.BaseURL)
	return &managedHelperInstance{
		managedEndpoint: endpoint,
		close: func(ctx context.Context) error {
			shutdownErr := srv.Shutdown(ctx)
			closeErr := closer(ctx)
			if shutdownErr != nil && shutdownErr != http.ErrServerClosed {
				return shutdownErr
			}
			return closeErr
		},
	}, nil
}

func browserHostManaged(cfg config.BrowserHostConfig) bool {
	return cfg.Enabled != nil && *cfg.Enabled && strings.TrimSpace(cfg.BaseURL) == ""
}

func desktopHostManaged(cfg config.DesktopHostConfig) bool {
	return cfg.Enabled != nil && *cfg.Enabled && strings.TrimSpace(cfg.BaseURL) == ""
}

func managedIdleTimeout(value, fallback time.Duration) time.Duration {
	if value > 0 {
		return value
	}
	return fallback
}

// ManagedHelpersController exposes status and reclaim for the operator UI.
// It implements gateway.ManagedHelpersController and is set on the gateway.
type ManagedHelpersController struct {
	Helpers *managedHelpers
}

// Status returns the current state of both browser and desktop helpers.
// Implements gateway.ManagedHelpersController.
func (c *ManagedHelpersController) Status(ctx context.Context) (browserState, desktopState gateway.HelperState, err error) {
	empty := gateway.HelperState{Status: "stopped"}
	if c == nil || c.Helpers == nil {
		return empty, empty, nil
	}
	if c.Helpers.Browser != nil {
		browserState = c.Helpers.Browser.status(ctx)
	} else {
		browserState = empty
	}
	if c.Helpers.Desktop != nil {
		desktopState = c.Helpers.Desktop.status(ctx)
	} else {
		desktopState = empty
	}
	return browserState, desktopState, nil
}

// Reclaim stops the named helper ("browser" or "desktop") so it can be restarted on next use.
func (c *ManagedHelpersController) Reclaim(ctx context.Context, name string) error {
	if c == nil || c.Helpers == nil {
		return nil
	}
	switch strings.TrimSpace(strings.ToLower(name)) {
	case "browser":
		if c.Helpers.Browser != nil {
			return c.Helpers.Browser.Stop(ctx)
		}
	case "desktop":
		if c.Helpers.Desktop != nil {
			return c.Helpers.Desktop.Stop(ctx)
		}
	default:
		return fmt.Errorf("unknown helper name %q", name)
	}
	return nil
}

func (h *managedHelperSupervisor) status(ctx context.Context) gateway.HelperState {
	h.mu.Lock()
	inst := h.instance
	lastUse := h.lastUse
	closed := h.closed
	idleSec := int(h.idleTimeout.Seconds())
	h.mu.Unlock()

	out := gateway.HelperState{
		Status:         "stopped",
		IdleTimeoutSec: idleSec,
	}
	if !lastUse.IsZero() {
		out.LastUseAt = lastUse.Format(time.RFC3339)
	}
	if closed || inst == nil {
		return out
	}
	out.Status = "running"
	if ctx != nil {
		if n, err := h.sessionCount(ctx, inst.managedEndpoint); err == nil {
			out.SessionCount = n
		}
	}
	return out
}

func managedHelperRoot(cfg config.Config) string {
	if base := strings.TrimSpace(cfg.Store.Path); base != "" {
		return filepath.Join(base, "managed-hosts")
	}
	if home, err := os.UserHomeDir(); err == nil && strings.TrimSpace(home) != "" {
		return filepath.Join(home, ".hopclaw")
	}
	return filepath.Join(os.TempDir(), "hopclaw")
}

func boolValue(v *bool, fallback bool) bool {
	if v != nil {
		return *v
	}
	return fallback
}

func randomHelperToken() (string, error) {
	var buf [24]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return "", fmt.Errorf("generate helper token: %w", err)
	}
	return hex.EncodeToString(buf[:]), nil
}
