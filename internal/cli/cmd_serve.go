package cli

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/fulcrus/hopclaw/bootstrap"
	"github.com/fulcrus/hopclaw/config"
	"github.com/fulcrus/hopclaw/internal/daemon"
	"github.com/fulcrus/hopclaw/internal/telemetry"
	"github.com/fulcrus/hopclaw/internal/update"
	"github.com/fulcrus/hopclaw/internal/version"
	"github.com/spf13/cobra"
)

// ---------------------------------------------------------------------------
// HTTP server timeouts
// ---------------------------------------------------------------------------

const (
	readHeaderTimeout = 5 * time.Second
	readTimeout       = 30 * time.Second
	writeTimeout      = 5 * time.Minute // extended for long-running model responses and SSE streams
	idleTimeout       = 2 * time.Minute
	shutdownTimeout   = 10 * time.Second
	closeTimeout      = 5 * time.Second
)

func newServeCmd() *cobra.Command {
	var name string
	cmd := &cobra.Command{
		Use:   "serve",
		Short: "Start the HopClaw gateway server",
		Long:  "Start the HopClaw gateway server. This is the default command when no subcommand is given.",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runServe(cmd, args, name)
		},
	}
	cmd.Flags().StringVar(&name, "name", "", "publish this runtime under a stable local name")
	return cmd
}

func runServe(cmd *cobra.Command, _ []string, serveName string) error {
	if strings.TrimSpace(serveName) != "" {
		if err := validateManagedTargetName(serveName); err != nil {
			return err
		}
	}
	configPath := resolveConfigPath()
	if configPath == "" {
		return handleNoConfig()
	}

	cfg, err := config.Load(configPath)
	if err != nil {
		return fmt.Errorf("load config %s: %w", configPath, err)
	}
	cfg.Server.Version = version.Version

	ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	return serve(ctx, cfg, configPath, serveName)
}

// resolveConfigPath finds the config file to use, checking:
// 1. --config flag
// 2. auto-discovery (env var, ./.hopclaw/, ~/.hopclaw/, /etc/hopclaw/)
func resolveConfigPath() string {
	if flagConfig != "" {
		return flagConfig
	}
	return config.DiscoverConfigPath()
}

// handleNoConfig is invoked when no config file is found. If API key
// environment variables are set, it auto-generates a minimal config and
// starts the server. Otherwise it prints a helpful message.
func handleNoConfig() error {
	if config.HasAPIKey() {
		return autoStartWithEnvKey()
	}
	fmt.Fprintln(os.Stderr, itextKey("cli.serve.no_config_found", "no config file found", "未找到配置文件"))
	fmt.Fprintln(os.Stderr, "")
	fmt.Fprintln(os.Stderr, itextKey("cli.serve.quick_start_options", "Quick start options:", "快速开始方式："))
	fmt.Fprintln(os.Stderr, itextKey("cli.serve.option_set_api_key", "  1. Set an API key environment variable:", "  1. 设置 API Key 环境变量："))
	for _, hint := range config.ProviderEnvExportHints(6) {
		fmt.Fprintf(os.Stderr, "     %s\n", hint)
	}
	fmt.Fprintln(os.Stderr, "")
	fmt.Fprintln(os.Stderr, itextKey("cli.serve.option_run_setup", "  2. Run interactive setup:", "  2. 运行交互式配置："))
	fmt.Fprintln(os.Stderr, "     hopclaw setup")
	fmt.Fprintln(os.Stderr, "")
	fmt.Fprintln(os.Stderr, itextKey("cli.serve.option_specify_config", "  3. Specify a config file:", "  3. 指定配置文件："))
	fmt.Fprintln(os.Stderr, "     hopclaw --config /path/to/config.yaml")
	return fmt.Errorf("%s", itextKey("cli.serve.no_config_help", "no config file found; run 'hopclaw setup' or set a supported provider API key env var", "未找到配置文件；请运行 'hopclaw setup' 或设置受支持模型提供商的 API Key 环境变量"))
}

func autoStartWithEnvKey() error {
	provider, _ := config.DetectAPIKey()
	log.Info("auto-configuring with API key from environment", "provider", provider)

	// Generate config and write to ~/.hopclaw/config.yaml.
	if err := daemon.EnsureStateDir(); err != nil {
		return fmt.Errorf("create state dir: %w", err)
	}

	configContent := config.GenerateDefaultConfig()
	configPath := daemon.ConfigFilePath()

	// Only write if the file doesn't already exist.
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		dir := filepath.Dir(configPath)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("create config dir: %w", err)
		}
		if err := os.WriteFile(configPath, []byte(configContent), 0o644); err != nil {
			return fmt.Errorf("write config: %w", err)
		}
		log.Info("generated config", "path", configPath)
	}

	cfg, err := config.Load(configPath)
	if err != nil {
		// If the saved config is stale, try parsing the generated one directly.
		cfg, err = config.Parse([]byte(configContent))
		if err != nil {
			return fmt.Errorf("parse auto-generated config: %w", err)
		}
	}
	cfg.Server.Version = version.Version

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	return serve(ctx, cfg, configPath, "")
}

func serve(ctx context.Context, cfg config.Config, configPath, serveName string) error {
	app, err := bootstrap.New(ctx, cfg, bootstrap.Dependencies{
		ConfigPath: configPath,
	})
	if err != nil {
		return fmt.Errorf("bootstrap app: %w", err)
	}
	defer func() {
		closeCtx, cancel := context.WithTimeout(context.Background(), closeTimeout)
		defer cancel()
		if err := app.Close(closeCtx); err != nil {
			log.Warn("close app failed", "error", err)
		}
	}()

	srv := &http.Server{
		Addr:              cfg.Server.Address,
		Handler:           app.Handler,
		ReadHeaderTimeout: readHeaderTimeout,
		ReadTimeout:       readTimeout,
		WriteTimeout:      writeTimeout,
		IdleTimeout:       idleTimeout,
	}

	log.Info("hopclaw listening", "version", version.Version, "address", cfg.Server.Address)

	if lease, err := registerServeInstance(ctx, cfg, configPath, serveName); err != nil {
		log.Warn("register local serve instance failed", "error", err)
	} else if lease != nil {
		defer lease.Close()
	}

	// Start background update check (async, non-blocking).
	update.BackgroundCheck(loadUpdatePolicy())

	errCh := make(chan error, 1)
	go func() {
		errCh <- srv.ListenAndServe()
	}()
	go emitServeTelemetry(cfg)

	select {
	case err := <-errCh:
		if !errors.Is(err, http.ErrServerClosed) {
			return fmt.Errorf("listen and serve: %w", err)
		}
	case <-ctx.Done():
		log.Info("shutting down")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
		defer cancel()
		if err := srv.Shutdown(shutdownCtx); err != nil {
			return fmt.Errorf("shutdown server: %w", err)
		}
		if err := <-errCh; err != nil && !errors.Is(err, http.ErrServerClosed) {
			return fmt.Errorf("listen and serve: %w", err)
		}
	}
	return nil
}

func emitServeTelemetry(cfg config.Config) {
	time.Sleep(500 * time.Millisecond)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := telemetry.RecordInstall(ctx, cfg.Diagnostics, "serve", cfg.Runtime.Profile); err != nil {
		telemetry.DebugLog(cfg.Diagnostics, "install telemetry failed", "error", err)
	}
	if err := telemetry.RecordRuntimeActive(ctx, cfg.Diagnostics, cfg.Runtime.Profile, "serve"); err != nil {
		telemetry.DebugLog(cfg.Diagnostics, "runtime telemetry failed", "error", err)
	}
}

// openBrowser attempts to open the given URL in the user's default browser.
// It is a best-effort operation — failures are silently ignored.
func isTrueEnv(name string) bool {
	switch strings.TrimSpace(strings.ToLower(os.Getenv(name))) {
	case "1", "true", "yes", "y", "on":
		return true
	default:
		return false
	}
}
