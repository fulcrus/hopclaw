// Command hopclaw-gateway runs the HopClaw gateway server, which composes
// the runtime API with operator endpoints, SSE streaming, channel ingress,
// and web UI hosting.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/fulcrus/hopclaw/bootstrap"
	"github.com/fulcrus/hopclaw/config"
	"github.com/fulcrus/hopclaw/internal/telemetry"
	"github.com/fulcrus/hopclaw/logging"
)

var log = logging.WithSubsystem("gateway")

var version = "dev"

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(args []string) int {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	flags := flag.NewFlagSet("hopclaw-gateway", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	configPath := flags.String("config", "", "path to YAML config")
	addr := flags.String("addr", "", "override listen address (default from config)")
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if *configPath == "" {
		fmt.Fprintln(os.Stderr, "missing -config")
		return 2
	}

	cfg, err := config.Load(*configPath)
	if err != nil {
		log.Error("load config failed", "error", err)
		return 1
	}
	cfg.Server.Version = version
	if *addr != "" {
		cfg.Server.Address = *addr
	}

	app, err := bootstrap.New(ctx, cfg, bootstrap.Dependencies{
		ConfigPath: *configPath,
	})
	if err != nil {
		log.Error("bootstrap failed", "error", err)
		return 1
	}
	defer func() {
		closeCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := app.Close(closeCtx); err != nil {
			log.Warn("close app failed", "error", err)
		}
	}()

	srv := &http.Server{
		Addr:              cfg.Server.Address,
		Handler:           app.Handler,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      60 * time.Second,
		IdleTimeout:       2 * time.Minute,
	}

	log.Info("hopclaw-gateway listening", "version", version, "address", cfg.Server.Address)

	errCh := make(chan error, 1)
	go func() {
		errCh <- srv.ListenAndServe()
	}()
	go func() {
		time.Sleep(500 * time.Millisecond)
		emitCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := telemetry.RecordInstall(emitCtx, cfg.Diagnostics, "gateway", cfg.Runtime.Profile); err != nil {
			telemetry.DebugLog(cfg.Diagnostics, "install telemetry failed", "error", err)
		}
		if err := telemetry.RecordRuntimeActive(emitCtx, cfg.Diagnostics, cfg.Runtime.Profile, "gateway"); err != nil {
			telemetry.DebugLog(cfg.Diagnostics, "runtime telemetry failed", "error", err)
		}
	}()

	select {
	case err := <-errCh:
		if !errors.Is(err, http.ErrServerClosed) {
			log.Error("listen failed", "error", err)
			return 1
		}
	case <-ctx.Done():
		log.Info("shutting down")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := srv.Shutdown(shutdownCtx); err != nil {
			log.Warn("shutdown failed", "error", err)
		}
		if err := <-errCh; err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Error("listen failed", "error", err)
			return 1
		}
	}
	return 0
}
