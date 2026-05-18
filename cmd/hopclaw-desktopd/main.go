// Command hopclaw-desktopd runs the desktop automation daemon, a standalone
// process that provides desktop automation via the desktop.v1 protocol.
package main

import (
	"context"
	"errors"
	"flag"
	"net"
	"net/http"
	"os"
	"os/signal"
	"runtime"
	"strings"
	"syscall"
	"time"

	desktopclient "github.com/fulcrus/hopclaw/desktopapi/client"
	"github.com/fulcrus/hopclaw/deviceauth"
	"github.com/fulcrus/hopclaw/internal/desktopd"
	"github.com/fulcrus/hopclaw/internal/nodedaemon"
	"github.com/fulcrus/hopclaw/logging"
	"github.com/fulcrus/hopclaw/nodeclient"
)

var log = logging.WithSubsystem("desktopd")

var version = "dev"

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(args []string) int {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	flags := flag.NewFlagSet("hopclaw-desktopd", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)

	listenAddr := flags.String("listen", "127.0.0.1:9224", "listen address")
	authToken := flags.String("auth-token", strings.TrimSpace(os.Getenv("HOPCLAW_DESKTOP_TOKEN")), "optional auth token")
	gatewayURL := flags.String("gateway-url", strings.TrimSpace(os.Getenv("HOPCLAW_GATEWAY_URL")), "optional HopClaw gateway URL for node registration")
	pairingCode := flags.String("pairing-code", strings.TrimSpace(os.Getenv("HOPCLAW_PAIRING_CODE")), "device pairing code to exchange for a node token")
	deviceID := flags.String("device-id", strings.TrimSpace(os.Getenv("HOPCLAW_DEVICE_ID")), "device identifier used for pairing and node registration")
	deviceName := flags.String("device-name", strings.TrimSpace(os.Getenv("HOPCLAW_DEVICE_NAME")), "friendly device name")
	storeDir := flags.String("store-dir", nodedaemon.DefaultStoreDir("desktopd"), "directory for local node credentials")
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}

	engine, err := desktopd.NewDefaultEngine()
	if err != nil {
		log.Error("desktop engine init failed", "error", err)
		return 1
	}

	manager := desktopd.NewManager(engine)
	api := desktopd.NewServer(manager, *authToken)
	srv := &http.Server{
		Addr:              *listenAddr,
		Handler:           api.Handler(),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      60 * time.Second,
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
		})
		if err != nil {
			log.Error("node bootstrap failed", "error", err)
			return 1
		}
		if bootstrap != nil {
			localClient := desktopclient.NewWithConfig(desktopclient.Config{
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
				ClientID:        "desktopd",
				ClientMode:      "desktopd",
				NodeID:          bootstrap.DeviceID,
				Platform:        nodePlatform(),
				DeviceFamily:    "desktop",
				Version:         version,
				ModelIdentifier: runtime.GOARCH,
				Capabilities:    []string{"desktop", "node"},
				Commands:        nodedaemon.DesktopNodeCommands(nodePlatform()),
			})
			nodedaemon.RegisterDesktopNodeHandlers(node, localClient, nodedaemon.DesktopNodeConfig{
				DeviceID:     bootstrap.DeviceID,
				DeviceName:   bootstrap.DeviceName,
				Platform:     nodePlatform(),
				DeviceFamily: "desktop",
				Version:      version,
				ListenAddr:   *listenAddr,
			})
			go func() {
				if err := node.Run(ctx); err != nil && ctx.Err() == nil {
					log.Warn("node client stopped", "error", err)
				}
			}()
		}
	}

	log.Info("hopclaw-desktopd listening", "version", version, "address", *listenAddr)

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
		log.Info("shutting down desktopd")
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
		return "http://127.0.0.1:9224"
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
