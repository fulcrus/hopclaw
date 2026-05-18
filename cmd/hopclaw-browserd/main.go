// Command hopclaw-browserd runs the browser daemon, a standalone process
// that provides browser automation via the browser.v1 protocol.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"runtime"
	"strings"
	"syscall"
	"time"

	browserclient "github.com/fulcrus/hopclaw/browserapi/client"
	browsertypes "github.com/fulcrus/hopclaw/browserapi/types"
	"github.com/fulcrus/hopclaw/deviceauth"
	"github.com/fulcrus/hopclaw/internal/browserd"
	"github.com/fulcrus/hopclaw/internal/nodedaemon"
	"github.com/fulcrus/hopclaw/logging"
	"github.com/fulcrus/hopclaw/nodeclient"
)

var log = logging.WithSubsystem("browserd")

var version = "dev"

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(args []string) int {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	flags := flag.NewFlagSet("hopclaw-browserd", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)

	listenAddr := flags.String("listen", "127.0.0.1:9223", "listen address")
	authToken := flags.String("auth-token", strings.TrimSpace(os.Getenv("HOPCLAW_BROWSER_TOKEN")), "optional auth token")
	chromePath := flags.String("chrome-path", "", "path to Chrome/Chromium/Edge executable")
	headless := flags.Bool("headless", false, "run browser sessions in headless mode")
	noSandbox := flags.Bool("no-sandbox", false, "disable the Chromium sandbox")
	sessionIdleTimeout := flags.Duration("session-idle-timeout", browserd.DefaultSessionIdleTimeout, "close browser sessions after this much idle time (0 disables cleanup)")
	sessionCleanupInterval := flags.Duration("session-cleanup-interval", browserd.DefaultSessionCleanupInterval, "background cleanup cadence for idle browser sessions (0 disables background cleanup)")
	maxSessions := flags.Int("max-sessions", browserd.DefaultMaxSessions, "soft cap for tracked browser sessions; least-recently-used idle sessions are evicted first (0 disables)")
	gatewayURL := flags.String("gateway-url", strings.TrimSpace(os.Getenv("HOPCLAW_GATEWAY_URL")), "optional HopClaw gateway URL for node registration")
	pairingCode := flags.String("pairing-code", strings.TrimSpace(os.Getenv("HOPCLAW_PAIRING_CODE")), "device pairing code to exchange for a node token")
	deviceID := flags.String("device-id", strings.TrimSpace(os.Getenv("HOPCLAW_DEVICE_ID")), "device identifier used for pairing and node registration")
	deviceName := flags.String("device-name", strings.TrimSpace(os.Getenv("HOPCLAW_DEVICE_NAME")), "friendly device name")
	storeDir := flags.String("store-dir", nodedaemon.DefaultStoreDir("browserd"), "directory for local node credentials")
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}

	engine, err := browserd.NewChromeEngine(browserd.ChromeConfig{
		ExecPath:  *chromePath,
		Headless:  *headless,
		NoSandbox: *noSandbox,
	})
	if err != nil {
		log.Error("browser engine init failed", "error", err)
		return 1
	}

	manager := browserd.NewManager(
		engine,
		browserd.WithSessionIdleTimeout(*sessionIdleTimeout),
		browserd.WithMaxSessions(*maxSessions),
	)
	if *sessionCleanupInterval > 0 {
		go manager.RunCleanupLoop(ctx, *sessionCleanupInterval)
	}
	api := browserd.NewServer(manager, *authToken)
	srv := &http.Server{
		Addr:              *listenAddr,
		Handler:           api.Handler(),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      2 * time.Minute,
		IdleTimeout:       2 * time.Minute,
	}

	if strings.TrimSpace(*gatewayURL) != "" {
		bootstrap, err := nodedaemon.Prepare(ctx, nodedaemon.BootstrapConfig{
			StoreDir:     *storeDir,
			GatewayURL:   *gatewayURL,
			PairingCode:  *pairingCode,
			DeviceID:     *deviceID,
			DeviceName:   *deviceName,
			Platform:     nodePlatform(),
			DeviceFamily: "desktop",
			Role:         deviceauth.RoleNode,
			Scopes:       []string{"browser.proxy"},
		})
		if err != nil {
			log.Error("node bootstrap failed", "error", err)
			return 1
		}
		if bootstrap != nil {
			localClient := browserclient.NewWithConfig(browserclient.Config{
				BaseURL:   localHTTPBaseURL(*listenAddr),
				AuthToken: *authToken,
			})
			node := nodeclient.New(nodeclient.Config{
				GatewayURL:      *gatewayURL,
				WebSocketURL:    bootstrap.WebSocketURL,
				DeviceID:        bootstrap.DeviceID,
				DeviceName:      bootstrap.DeviceName,
				Token:           bootstrap.Token,
				Role:            deviceauth.RoleNode,
				Scopes:          []string{"browser.proxy"},
				ClientID:        "browserd",
				ClientMode:      "browserd",
				NodeID:          bootstrap.DeviceID,
				Platform:        nodePlatform(),
				DeviceFamily:    "desktop",
				Version:         version,
				ModelIdentifier: runtime.GOARCH,
				Capabilities:    []string{"browser", "node"},
				Commands:        []string{"device.info", "device.status", "browser.proxy"},
			})
			node.Register("device.info", func(_ context.Context, _ map[string]any) (map[string]any, error) {
				return map[string]any{
					"device_id":     bootstrap.DeviceID,
					"name":          bootstrap.DeviceName,
					"platform":      nodePlatform(),
					"device_family": "desktop",
					"daemon":        "browserd",
					"version":       version,
					"listen":        *listenAddr,
				}, nil
			})
			node.Register("device.status", func(ctx context.Context, _ map[string]any) (map[string]any, error) {
				if err := localClient.Health(ctx); err != nil {
					return nil, err
				}
				return map[string]any{"ok": true, "daemon": "browserd", "listen": *listenAddr}, nil
			})
			node.Register("browser.proxy", func(ctx context.Context, params map[string]any) (map[string]any, error) {
				req, err := browserProxyRequest(params)
				if err != nil {
					return nil, err
				}
				resp, err := localClient.Do(ctx, req)
				if err != nil {
					return nil, err
				}
				if !resp.OK {
					return nil, fmt.Errorf("%s", strings.TrimSpace(resp.Error))
				}
				result := map[string]any{"ok": true}
				for key, value := range resp.Data {
					result[key] = value
				}
				if resp.SessionID != "" {
					result["session_id"] = resp.SessionID
				}
				if resp.ArtifactRef != "" {
					result["artifact_ref"] = resp.ArtifactRef
				}
				return result, nil
			})
			go func() {
				if err := node.Run(ctx); err != nil && ctx.Err() == nil {
					log.Warn("node client stopped", "error", err)
				}
			}()
		}
	}

	log.Info("hopclaw-browserd listening", "version", version, "address", *listenAddr)

	errCh := make(chan error, 1)
	go func() {
		errCh <- srv.ListenAndServe()
	}()

	select {
	case err := <-errCh:
		if !errors.Is(err, http.ErrServerClosed) {
			log.Error("listen failed", "error", err)
			return 1
		}
	case <-ctx.Done():
		log.Info("shutting down browserd")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := srv.Shutdown(shutdownCtx); err != nil {
			log.Warn("shutdown http failed", "error", err)
		}
		if err := api.Close(shutdownCtx); err != nil {
			log.Warn("shutdown sessions failed", "error", err)
		}
		if err := <-errCh; err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Error("listen failed", "error", err)
			return 1
		}
	}
	return 0
}

func localHTTPBaseURL(listenAddr string) string {
	host, port, err := net.SplitHostPort(listenAddr)
	if err != nil {
		return "http://127.0.0.1:9223"
	}
	if host == "" || host == "0.0.0.0" || host == "::" || host == "[::]" {
		host = "127.0.0.1"
	}
	return "http://" + net.JoinHostPort(host, port)
}

func nodePlatform() string {
	switch runtime.GOOS {
	case "darwin":
		return "macOS"
	case "windows":
		return "Windows"
	case "linux":
		return "Linux"
	default:
		return runtime.GOOS
	}
}

func browserProxyRequest(params map[string]any) (browsertypes.Request, error) {
	action, _ := params["action"].(string)
	action = strings.TrimSpace(action)
	if action == "" {
		return browsertypes.Request{}, fmt.Errorf("action is required")
	}
	sessionID, _ := params["session_id"].(string)
	browserType, _ := params["browser_type"].(string)
	request := browsertypes.Request{
		Action:      action,
		SessionID:   strings.TrimSpace(sessionID),
		BrowserType: browsertypes.BrowserType(strings.TrimSpace(browserType)),
	}
	if nested, ok := params["params"].(map[string]any); ok {
		request.Params = nested
	} else {
		request.Params = make(map[string]any)
		for key, value := range params {
			if key == "action" || key == "session_id" || key == "browser_type" {
				continue
			}
			request.Params[key] = value
		}
	}
	return request, nil
}
