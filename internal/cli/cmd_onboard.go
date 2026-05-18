package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/fulcrus/hopclaw/config"
	"github.com/fulcrus/hopclaw/internal/daemon"
	"github.com/fulcrus/hopclaw/internal/execenv"
	"github.com/fulcrus/hopclaw/internal/telemetry"
	"github.com/fulcrus/hopclaw/keychain"

	"github.com/charmbracelet/huh"
	"github.com/spf13/cobra"
)

// ---------------------------------------------------------------------------
// onboard command
// ---------------------------------------------------------------------------

const (
	onboardHealthTimeout                  = 5 * time.Second
	onboardVerifyMaxRetries               = 3
	onboardRetryDelay                     = 2 * time.Second
	onboardTotalSteps                     = 7
	onboardWebFirstSteps                  = 4
	onboardTelemetryTimeout               = 750 * time.Millisecond
	onboardExistingConfigUseValue         = "__use_existing_config__"
	onboardExistingConfigReconfigureValue = "__reconfigure_existing_config__"
	onboardExistingConfigDashboardValue   = "__dashboard_existing_config__"
)

// recommendedSkills lists skills suggested during onboarding.
var recommendedSkills = []string{
	"summarize",
	"translate",
	"weather",
	"github",
}

func newOnboardCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "onboard",
		Short: "Guided install and first-time configuration",
		Long: fmt.Sprintf(`Walk through the complete HopClaw onboarding process:

  1. Auth choice (bearer/JWT/API key/none)
  2. Model provider selection and validation
  3. Channel setup (multi-select)
  4. Gateway configuration
  5. Daemon installation
  6. Start & verify connectivity
  7. Skill installation

For a minimal setup, use 'hopclaw setup' instead.

Total steps: %d`, onboardTotalSteps),
		RunE: runOnboard,
	}
	cmd.Flags().Bool("non-interactive", false, "skip prompts, use env vars and defaults")
	cmd.Flags().Bool("web-first", false, "skip CLI setup details, start the local gateway, and continue in the dashboard")
	return cmd
}

func runOnboard(cmd *cobra.Command, _ []string) error {
	webFirst, _ := cmd.Flags().GetBool("web-first")
	if webFirst {
		return runOnboardWebFirst(cmd)
	}
	nonInteractive, _ := cmd.Flags().GetBool("non-interactive")
	if nonInteractive {
		return runOnboardNonInteractive()
	}
	return runOnboardInteractive(cmd)
}

func runOnboardWebFirst(cmd *cobra.Command) error {
	configPath := resolveConfigPath()
	if configPath == "" {
		configPath = daemon.ConfigFilePath()
	}

	fmt.Println(itext("HopClaw web-first onboarding", "HopClaw 网页优先安装向导"))
	fmt.Println()

	fmt.Printf(itext("Step 1/%d: Gateway configuration\n", "第 1/%d 步：网关配置\n"), onboardWebFirstSteps)
	reusedExistingConfig, err := ensureWebFirstConfig(configPath)
	if err != nil {
		return err
	}
	if reusedExistingConfig {
		fmt.Printf(itext("  Using existing config at %s\n", "  复用已有配置：%s\n"), configPath)
	} else {
		fmt.Printf(itext("  Config written to %s\n", "  已写入配置：%s\n"), configPath)
	}

	fmt.Println()
	fmt.Printf(itext("Step 2/%d: Model setup\n", "第 2/%d 步：模型配置\n"), onboardWebFirstSteps)
	fmt.Println(itext("  Deferred to dashboard settings.", "  这一步先放到控制台里完成。"))

	fmt.Println()
	fmt.Printf(itext("Step 3/%d: Channel setup\n", "第 3/%d 步：渠道配置\n"), onboardWebFirstSteps)
	fmt.Println(itext("  Deferred to dashboard settings.", "  这一步先放到控制台里完成。"))

	fmt.Println()
	fmt.Printf(itext("Step 4/%d: Start local gateway\n", "第 4/%d 步：启动本地网关\n"), onboardWebFirstSteps)

	client, err := NewGatewayClient()
	if err != nil {
		return fmt.Errorf("create gateway client: %w", err)
	}
	client.HTTP.Timeout = onboardHealthTimeout

	body, statusCode, err := fetchOperatorStatus(context.Background(), client)
	gatewayRunning := err == nil && statusCode < 400
	if gatewayRunning {
		fmt.Println(itext("  Gateway is already running.", "  网关已经在运行。"))
	} else {
		logPath, startErr := startGatewayInBackground(configPath)
		if startErr != nil {
			fmt.Printf(itext("  Warning: could not start gateway in background: %v\n", "  警告：后台启动网关失败：%v\n"), startErr)
			fmt.Println(itext("  Start manually with: hopclaw serve", "  你可以手动运行：hopclaw serve"))
			fmt.Printf(itext("  Logs: %s\n", "  日志：%s\n"), logPath)
		} else {
			fmt.Printf(itext("  Starting gateway in background (logs: %s)\n", "  正在后台启动网关（日志：%s）\n"), logPath)
		}
		verifyGatewayConnectivityWithRetriesWithClient(context.Background(), os.Stdout, client, resolveGatewayAddr())
		body, statusCode, err = fetchOperatorStatus(context.Background(), client)
		gatewayRunning = err == nil && statusCode < 400
	}

	fmt.Println()
	if gatewayRunning {
		printOnboardDashboardAccess()
		fmt.Println(itext("  Continue in Settings > Models and Settings > Channels.", "  接下来请在控制台的“模型设置”和“渠道设置”中继续完成配置。"))
	} else {
		fmt.Println(itext("Dashboard is not ready yet.", "控制台暂时还没有准备好。"))
		fmt.Println(itext("  Guided setup: hopclaw onboard", "  可重新进入引导：hopclaw onboard"))
		fmt.Println(itext("  Manual retry: hopclaw serve", "  可手动启动：hopclaw serve"))
		fmt.Println(itext("  Then: hopclaw dashboard --open", "  然后运行：hopclaw dashboard --open"))
		if err == nil && statusCode >= 400 {
			fmt.Printf(itext("  Last gateway response: %s\n", "  最近一次网关响应：%s\n"), gatewayHTTPError(statusCode, body))
		}
	}

	fmt.Println()
	if gatewayRunning {
		fmt.Println(itext("Web-first onboarding complete!", "网页优先安装向导已完成。"))
	} else {
		fmt.Println(itext("Web-first onboarding needs attention.", "网页优先安装向导还需要你处理一下。"))
	}
	fmt.Println()
	fmt.Println(itext("Useful commands:", "常用命令："))
	fmt.Println(itext("  hopclaw serve             # start the gateway manually", "  hopclaw serve             # 手动启动网关"))
	fmt.Println(itext("  hopclaw dashboard --open  # open the local dashboard", "  hopclaw dashboard --open  # 打开本地控制台"))
	fmt.Println(itext("  hopclaw onboard           # guided setup with skip options", "  hopclaw onboard           # 再次进入安装向导"))
	fmt.Println(itext("  hopclaw daemon install    # install the user-level background service later", "  hopclaw daemon install    # 稍后安装后台服务"))
	fmt.Println(itext("  hopclaw health            # health check", "  hopclaw health            # 健康检查"))
	fmt.Println(itext("  hopclaw config show       # view configuration", "  hopclaw config show       # 查看配置"))

	emitOnboardTelemetry(configPath, onboardTelemetryInfo{
		Interactive:          true,
		Provider:             "",
		DaemonInstalled:      false,
		SkillsSelectedCount:  0,
		ReusedExistingConfig: reusedExistingConfig,
	})
	return nil
}

// ---------------------------------------------------------------------------
// Non-interactive onboarding
// ---------------------------------------------------------------------------

func runOnboardNonInteractive() error {
	configPath := daemon.ConfigFilePath()
	reusedExistingConfig := false

	fmt.Println(itext("HopClaw non-interactive onboarding", "HopClaw 非交互式安装向导"))
	fmt.Println()

	// Step 1: Auth — skip in non-interactive mode (use defaults).
	fmt.Printf(itext("Step 1/%d: Auth choice ... skipped (using defaults)\n", "第 1/%d 步：认证方式，已跳过（使用默认值）\n"), onboardTotalSteps)

	// Step 2: Model provider from env.
	fmt.Printf(itext("Step 2/%d: Model provider\n", "第 2/%d 步：模型提供商\n"), onboardTotalSteps)

	provider, key := config.DetectAPIKey()
	if provider == "" {
		return fmt.Errorf("%s", config.MissingAPIKeyMessage())
	}
	fmt.Printf(itext("  Detected %s API key\n", "  已检测到 %s API Key\n"), provider)

	// Step 3: Channel — skip in non-interactive.
	fmt.Printf(itext("Step 3/%d: Channel setup ... skipped\n", "第 3/%d 步：渠道配置，已跳过\n"), onboardTotalSteps)

	// Step 4: Gateway — use defaults.
	fmt.Printf(itext("Step 4/%d: Gateway setup ... using defaults\n", "第 4/%d 步：网关配置，使用默认值\n"), onboardTotalSteps)

	// Generate and write config.
	if err := daemon.EnsureStateDir(); err != nil {
		return fmt.Errorf("create state dir: %w", err)
	}

	// Check if config already exists.
	if _, err := os.Stat(configPath); err == nil {
		reusedExistingConfig = true
		fmt.Printf(itext("  Config already exists at %s\n", "  配置已存在：%s\n"), configPath)
	} else {
		cfgContent := config.GenerateDefaultConfig()
		if err := os.WriteFile(configPath, []byte(cfgContent), 0o644); err != nil {
			return fmt.Errorf("write config: %w", err)
		}
		fmt.Printf("  Config written to %s\n", configPath)
	}

	_ = key // key used for config generation via GenerateDefaultConfig

	// Step 5: Daemon install — skip in non-interactive.
	fmt.Printf(itext("Step 5/%d: Daemon install ... skipped\n", "第 5/%d 步：后台服务安装，已跳过\n"), onboardTotalSteps)

	// Step 6: Verify connectivity.
	fmt.Printf(itext("Step 6/%d: Verify connectivity\n", "第 6/%d 步：验证连接\n"), onboardTotalSteps)
	verifyGatewayConnectivity()

	// Step 7: Skill install — skip in non-interactive.
	fmt.Printf(itext("Step 7/%d: Skill install ... skipped\n", "第 7/%d 步：技能安装，已跳过\n"), onboardTotalSteps)

	fmt.Println()
	fmt.Println(itext("Non-interactive onboarding complete!", "非交互式安装向导已完成！"))
	printOnboardSummary()
	emitOnboardTelemetry(configPath, onboardTelemetryInfo{
		Interactive:          false,
		Provider:             provider,
		DaemonInstalled:      false,
		SkillsSelectedCount:  0,
		ReusedExistingConfig: reusedExistingConfig,
	})
	return nil
}

// ---------------------------------------------------------------------------
// Interactive onboarding
// ---------------------------------------------------------------------------

func runOnboardInteractive(cmd *cobra.Command) error {
	configPath := daemon.ConfigFilePath()

	fmt.Println(itext("Welcome to HopClaw!", "欢迎使用 HopClaw！"))
	fmt.Println()

	// Check if config already exists and offer to reconfigure.
	configExists := false
	if _, err := os.Stat(configPath); err == nil {
		configExists = true
	}

	if configExists {
		fmt.Printf(itext("Config file found at %s\n", "发现已有配置文件：%s\n"), configPath)

		loadedCfg, err := config.Load(configPath)
		if err != nil {
			fmt.Printf(itext("Warning: config has issues: %v\n", "警告：配置有问题：%v\n"), err)

			var reconfigure bool
			form := huh.NewForm(
				huh.NewGroup(
					huh.NewConfirm().
						Title(itext("Reconfigure from scratch?", "要重新从头配置吗？")).
						Value(&reconfigure),
				),
			)
			if err := form.Run(); err != nil {
				return err
			}
			if reconfigure {
				if err := os.Remove(configPath); err != nil {
					return fmt.Errorf("remove old config: %w", err)
				}
				configExists = false
			}
		} else {
			setupCatalog := loadCLISetupCatalogBestEffort(cmd.Context())
			action, err := promptExistingOnboardAction(configPath, loadedCfg, setupCatalog)
			if err != nil {
				return err
			}
			switch action {
			case onboardExistingConfigReconfigureValue:
				if err := os.Remove(configPath); err != nil {
					return fmt.Errorf("remove old config: %w", err)
				}
				configExists = false
			case onboardExistingConfigUseValue:
				return runOnboardExistingConfigHandoff(configPath, loadedCfg, false)
			case onboardExistingConfigDashboardValue:
				return runOnboardExistingConfigHandoff(configPath, loadedCfg, true)
			default:
				return fmt.Errorf("unsupported existing-config action %q", action)
			}
		}
	}

	var (
		opts            config.SetupOptions
		existingCfg     config.Config
		daemonInstalled bool
	)
	setupCatalog := loadCLISetupCatalogBestEffort(cmd.Context())

	// ---------------------------------------------------------------------------
	// Step 1: Auth Choice
	// ---------------------------------------------------------------------------

	fmt.Println()
	fmt.Printf(itext("Step 1/%d: Authentication\n", "第 1/%d 步：认证方式\n"), onboardTotalSteps)

	if !configExists {
		authMode, bearerToken, apiKey, jwtSecret, err := promptOnboardAuthConfigWithCatalog(setupCatalog)
		if err != nil {
			return err
		}
		opts.AuthMode = authMode
		opts.AuthToken = bearerToken
		opts.AuthAPIKey = apiKey
		opts.AuthJWTSecret = jwtSecret
		if strings.TrimSpace(authMode) == "" {
			fmt.Println(itext("  Skipped. Configure operator authentication later in the dashboard.", "  已跳过。你可以稍后在控制台配置访问认证。"))
		} else {
			fmt.Printf(itext("  Auth method: %s\n", "  认证方式：%s\n"), authModeSummaryLabel(authMode))
		}
	} else {
		loadedCfg, err := config.Load(configPath)
		if err == nil {
			existingCfg = loadedCfg
		}
		fmt.Println(itext("  Using existing config auth settings. Re-run 'hopclaw setup' to change them.", "  正在使用已有认证配置。如需修改，可重新运行 'hopclaw setup'。"))
	}

	// ---------------------------------------------------------------------------
	// Step 2: Model Selection
	// ---------------------------------------------------------------------------

	fmt.Println()
	fmt.Printf(itext("Step 2/%d: Model provider\n", "第 2/%d 步：模型提供商\n"), onboardTotalSteps)

	if !configExists {
		providerOpts, err := collectOnboardProviderSetupOptionsWithCatalog(setupCatalog)
		if err != nil {
			return err
		}
		opts.Provider = providerOpts.Provider
		opts.ProviderAPI = providerOpts.ProviderAPI
		if len(providerOpts.ProviderValues) > 0 {
			opts.ProviderValues = make(map[string]string, len(providerOpts.ProviderValues))
			for key, value := range providerOpts.ProviderValues {
				opts.ProviderValues[key] = value
			}
		}
		opts.APIKey = providerOpts.APIKey
		opts.BaseURL = providerOpts.BaseURL
		opts.Model = providerOpts.Model
		if strings.TrimSpace(opts.Provider) == "" {
			fmt.Println(itext("  Skipped. Configure models later with: hopclaw dashboard", "  已跳过。你可以稍后在控制台里配置模型。"))
		} else {
			fmt.Printf(itext("  Provider: %s\n", "  提供商：%s\n"), setupCatalog.ProviderDisplayName(opts.Provider))
		}
	} else {
		fmt.Println(itext("  Using existing model configuration.", "  正在使用已有模型配置。"))
	}

	// ---------------------------------------------------------------------------
	// Step 3: Channel Setup
	// ---------------------------------------------------------------------------

	fmt.Println()
	fmt.Printf(itext("Step 3/%d: Channel setup\n", "第 3/%d 步：渠道配置\n"), onboardTotalSteps)

	if !configExists {
		channelSelections, err := promptMultiSetupChannelsWithCatalog(setupCatalog)
		if err != nil {
			return err
		}
		opts.Channels = channelSelections
		if len(channelSelections) > 0 {
			labels := make([]string, 0, len(channelSelections))
			for _, selection := range channelSelections {
				if profile, ok := setupCatalog.LookupChannelProfile(selection.ID); ok {
					labels = append(labels, profile.DisplayName)
				} else {
					labels = append(labels, selection.ID)
				}
			}
			fmt.Printf(itext("  Configured %d channel(s): %s\n", "  已配置 %d 个渠道：%s\n"), len(channelSelections), strings.Join(labels, ", "))
		} else {
			fmt.Println(itext("  No channels selected. You can add them later in the dashboard.", "  当前没有选择渠道。你可以稍后在控制台继续添加。"))
		}
	} else {
		fmt.Println(itext("  Using existing channel configuration. Add or edit channels later in the dashboard.", "  正在使用已有渠道配置。你也可以稍后在控制台继续添加或编辑。"))
	}

	// ---------------------------------------------------------------------------
	// Step 4: Gateway Setup
	// ---------------------------------------------------------------------------

	fmt.Println()
	fmt.Printf(itext("Step 4/%d: Gateway configuration\n", "第 4/%d 步：网关配置\n"), onboardTotalSteps)

	if !configExists {
		initialAddr := suggestAvailableGatewayAddress(config.DefaultGatewayAddress)
		listenAddr, err := promptInput(
			itext("Gateway listen address", "网关监听地址"),
			gatewayListenPromptDescription(initialAddr),
			initialAddr,
			true,
		)
		if err != nil {
			return err
		}
		fmt.Printf(itext("  Listen address: %s\n", "  监听地址：%s\n"), listenAddr)
		opts.Address = listenAddr

		if err := daemon.EnsureStateDir(); err != nil {
			return fmt.Errorf("create state dir: %w", err)
		}
		cfgContent, err := config.BuildConfig(opts)
		if err != nil {
			return fmt.Errorf("build config: %w", err)
		}
		if err := os.WriteFile(configPath, []byte(cfgContent), 0o644); err != nil {
			return fmt.Errorf("write config: %w", err)
		}
		if loadedCfg, err := config.Load(configPath); err == nil {
			existingCfg = loadedCfg
		} else {
			log.Warn("generated onboarding config has validation issues", "error", err)
		}
		fmt.Printf(itext("  Config written to %s\n", "  已写入配置：%s\n"), configPath)
	} else {
		addr := strings.TrimSpace(existingCfg.Server.Address)
		if addr == "" {
			addr = config.DefaultGatewayAddress
		}
		fmt.Printf(itext("  Using existing listen address: %s\n", "  正在使用已有监听地址：%s\n"), addr)
	}

	// ---------------------------------------------------------------------------
	// Step 5: Daemon Installation
	// ---------------------------------------------------------------------------

	fmt.Println()
	fmt.Printf(itext("Step 5/%d: System service\n", "第 5/%d 步：后台服务\n"), onboardTotalSteps)

	printPlatformDaemonGuidance()

	mgr, err := daemon.NewServiceManager()
	if err != nil {
		fmt.Printf(itext("  Skipping (not supported: %v)\n", "  已跳过（当前平台不支持：%v）\n"), err)
	} else {
		status, err := mgr.Status()
		if err != nil {
			fmt.Printf(itext("  Warning: could not query service status: %v\n", "  警告：无法查询服务状态：%v\n"), err)
		} else if status.Installed {
			daemonInstalled = true
			fmt.Println(itext("  Service already installed.", "  后台服务已经安装。"))
		} else {
			var install bool
			form := huh.NewForm(
				huh.NewGroup(
					huh.NewConfirm().
						Title(itext("Install HopClaw as a system service?", "要把 HopClaw 安装成后台服务吗？")).
						Description(itext("This starts HopClaw automatically when you log in.", "安装后会在你登录系统时自动启动。")).
						Value(&install),
				),
			)
			if err := form.Run(); err != nil {
				return err
			}
			if install {
				if err := runDaemonInstall(nil, nil); err != nil {
					fmt.Printf(itext("  Warning: install failed: %v\n", "  警告：安装后台服务失败：%v\n"), err)
				} else {
					daemonInstalled = true
					fmt.Println(itext("  Service installed.", "  后台服务已安装。"))
				}
			} else {
				fmt.Println(itext("  Skipped. You can install later with: hopclaw daemon install", "  已跳过。你可以稍后运行 hopclaw daemon install 安装后台服务。"))
			}
		}
	}

	// ---------------------------------------------------------------------------
	// Step 6: Start & Verify
	// ---------------------------------------------------------------------------

	fmt.Println()
	fmt.Printf(itext("Step 6/%d: Start & verify\n", "第 6/%d 步：启动并检查\n"), onboardTotalSteps)

	if mgr != nil {
		status, _ := mgr.Status()
		if status != nil && status.Installed {
			if status.Running {
				fmt.Println(itext("  Service is already running.", "  后台服务已经在运行。"))
			} else {
				if err := mgr.Start(); err != nil {
					fmt.Printf(itext("  Warning: could not start: %v\n", "  警告：启动失败：%v\n"), err)
					fmt.Println(itext("  Start manually with: hopclaw daemon start", "  你可以手动运行：hopclaw daemon start"))
				} else {
					fmt.Println(itext("  Service started.", "  后台服务已启动。"))
					time.Sleep(onboardRetryDelay)
				}
			}
		} else {
			fmt.Println(itext("  Service not installed. Start manually with: hopclaw serve", "  后台服务尚未安装。你可以手动运行：hopclaw serve"))
		}
	} else {
		fmt.Println(itext("  Start manually with: hopclaw serve", "  你可以手动运行：hopclaw serve"))
	}

	// Verify with retry logic.
	gatewayHealthy := verifyGatewayConnectivityWithRetries()
	if !gatewayHealthy {
		logPath, startErr := startGatewayInBackground(configPath)
		if startErr != nil {
			fmt.Printf(itext("  Warning: could not start a temporary background gateway: %v\n", "  警告：无法启动临时后台网关：%v\n"), startErr)
			if strings.TrimSpace(logPath) != "" {
				fmt.Printf(itext("  Logs: %s\n", "  日志：%s\n"), logPath)
			}
		} else {
			fmt.Printf(itext("  Started a temporary background gateway for the dashboard (logs: %s)\n", "  已为控制台启动临时后台网关（日志：%s）\n"), logPath)
			gatewayHealthy = verifyGatewayConnectivityWithRetries()
		}
	}

	// ---------------------------------------------------------------------------
	// Step 7: Skill Install
	// ---------------------------------------------------------------------------

	fmt.Println()
	fmt.Printf(itext("Step 7/%d: Recommended skills\n", "第 7/%d 步：推荐技能\n"), onboardTotalSteps)

	var installSkills []string
	switch {
	case configExists:
		fmt.Println(itext("  Skipped. This machine already has a HopClaw config; add skills later only if you need them.", "  已跳过。这台机器已经有 HopClaw 配置，只有在需要时再安装技能即可。"))
	case !gatewayHealthy:
		fmt.Println(itext("  Skipped for now. Finish gateway setup first, then add skills later if you need them.", "  已先跳过。请先完成网关启动，之后如有需要再安装技能。"))
	default:
		var installRecommended bool
		client, clientErr := NewGatewayClient()
		if clientErr != nil {
			fmt.Printf(itext("  Skipped. Skill management API is unavailable: %v\n", "  已跳过。技能管理接口暂不可用：%v\n"), clientErr)
		} else if availableSkills := availableRecommendedSkills(cmd.Context(), client, recommendedSkills); len(availableSkills) == 0 {
			fmt.Println(itext("  Skipped. No recommended skill sources are available locally or from the skill catalog.", "  已跳过。当前本地和技能目录里都没有可安装的推荐技能来源。"))
		} else {
			confirmForm := huh.NewForm(
				huh.NewGroup(
					huh.NewConfirm().
						Title(itext("Install recommended skills now? (optional)", "现在安装推荐技能吗？（可选）")).
						Description(itext("Most users can skip this and continue with the dashboard.", "大多数用户可以先跳过，直接进入控制台继续。")).
						Value(&installRecommended),
				),
			)
			if err := confirmForm.Run(); err != nil {
				return err
			}
			if !installRecommended {
				fmt.Println(itext("  Skipped. You can add skills later if you need them.", "  已跳过。你可以在需要时稍后添加技能。"))
			} else {
				skillOptions := make([]huh.Option[string], 0, len(availableSkills))
				for _, s := range availableSkills {
					skillOptions = append(skillOptions, huh.NewOption(s, s))
				}

				skillForm := huh.NewForm(
					huh.NewGroup(
						huh.NewMultiSelect[string]().
							Title(itext("Select skills to install (space to toggle)", "选择要安装的技能（空格勾选）")).
							Description(itext("Press enter to continue without installing any skills.", "直接回车即可继续，不安装任何技能。")).
							Options(skillOptions...).
							Value(&installSkills),
					),
				)
				if err := skillForm.Run(); err != nil {
					return err
				}

				if len(installSkills) > 0 {
					fmt.Printf(itext("  Selected %d skill(s): %s\n", "  已选择 %d 个技能：%s\n"), len(installSkills), strings.Join(installSkills, ", "))
					for _, selected := range installSkills {
						source, name := resolveSkillInstallTarget(selected)
						req := map[string]any{
							"name":   name,
							"source": valueOrFallback(source, selected),
						}
						var resp skillInstallResponse
						if err := client.Post(cmd.Context(), "/operator/skills/install", req, &resp); err != nil {
							fmt.Printf(itext("  Warning: could not install %s: %v\n", "  警告：无法安装 %s：%v\n"), selected, err)
							continue
						}
						fmt.Printf(itext("  Installed %s", "  已安装 %s"), valueOrFallback(resp.SkillID, selected))
						if resp.Version != "" {
							fmt.Printf(" v%s", resp.Version)
						}
						fmt.Println()
					}
					fmt.Println(itext("  Skills can be managed with: hopclaw skills list", "  可通过 hopclaw skills list 管理技能。"))
				} else {
					fmt.Println(itext("  No skills selected. You can add them later if you need them.", "  当前没有选择技能。你可以在需要时稍后添加。"))
				}
			}
		}
	}

	// ---------------------------------------------------------------------------
	// Done
	// ---------------------------------------------------------------------------

	fmt.Println()
	if gatewayHealthy {
		fmt.Println(itext("Onboarding complete!", "安装向导已完成！"))
		printOnboardDashboardAccess()
	} else {
		fmt.Println(itext("Onboarding needs attention.", "安装向导还需要你处理一下。"))
	}
	printOnboardSummary()
	emitOnboardTelemetry(configPath, onboardTelemetryInfo{
		Interactive:          true,
		Provider:             opts.Provider,
		DaemonInstalled:      daemonInstalled,
		SkillsSelectedCount:  len(installSkills),
		ReusedExistingConfig: configExists,
	})

	return nil
}

type onboardTelemetryInfo struct {
	Interactive          bool
	Provider             string
	DaemonInstalled      bool
	SkillsSelectedCount  int
	ReusedExistingConfig bool
}

func ensureWebFirstConfig(configPath string) (bool, error) {
	if err := daemon.EnsureStateDir(); err != nil {
		return false, fmt.Errorf("create state dir: %w", err)
	}
	if _, err := os.Stat(configPath); err == nil {
		return true, nil
	} else if !os.IsNotExist(err) {
		return false, fmt.Errorf("stat config %s: %w", configPath, err)
	}

	cfgContent, err := config.BuildConfig(config.SetupOptions{
		Address: suggestAvailableGatewayAddress(config.DefaultGatewayAddress),
	})
	if err != nil {
		return false, fmt.Errorf("build minimal config: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(configPath), 0o755); err != nil {
		return false, fmt.Errorf("create config dir: %w", err)
	}
	if err := os.WriteFile(configPath, []byte(cfgContent), 0o644); err != nil {
		return false, fmt.Errorf("write config: %w", err)
	}
	return false, nil
}

func startGatewayInBackground(configPath string) (string, error) {
	if err := daemon.EnsureStateDir(); err != nil {
		return "", fmt.Errorf("create state dir: %w", err)
	}

	exe, err := resolveExecutable()
	if err != nil {
		return "", fmt.Errorf("resolve binary path: %w", err)
	}

	logPath := filepath.Join(daemon.LogDir(), "web-first-gateway.log")
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return logPath, fmt.Errorf("open log file: %w", err)
	}
	defer logFile.Close()

	command := exec.Command(exe, "--config", configPath, "serve")
	command.Stdout = logFile
	command.Stderr = logFile
	command.Env = buildBackgroundGatewayEnv()
	configureDetachedProcess(command)

	if err := command.Start(); err != nil {
		return logPath, fmt.Errorf("start gateway: %w", err)
	}
	if err := writeBackgroundGatewayPIDFile(backgroundGatewayPIDInfo{
		PID:        command.Process.Pid,
		BinaryPath: exe,
		ConfigPath: configPath,
	}); err != nil {
		_ = command.Process.Kill()
		return logPath, fmt.Errorf("record background gateway pid: %w", err)
	}
	_ = command.Process.Release()
	return logPath, nil
}

func buildBackgroundGatewayEnv() []string {
	return execenv.BuildChildEnv(
		execenv.ModuleExecProfile,
		nil,
		map[string]string{"HOPCLAW_NO_BROWSER": "1"},
		nil,
		nil,
	)
}

type backgroundGatewayPIDInfo struct {
	PID        int    `json:"pid"`
	BinaryPath string `json:"binary_path,omitempty"`
	ConfigPath string `json:"config_path,omitempty"`
}

func backgroundGatewayPIDFilePath() string {
	return filepath.Join(daemon.StateDir(), "web-first-gateway.pid")
}

func writeBackgroundGatewayPIDFile(info backgroundGatewayPIDInfo) error {
	data, err := json.Marshal(info)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(backgroundGatewayPIDFilePath()), 0o755); err != nil {
		return err
	}
	return os.WriteFile(backgroundGatewayPIDFilePath(), data, 0o644)
}

func loadBackgroundGatewayPIDFile() (backgroundGatewayPIDInfo, error) {
	data, err := os.ReadFile(backgroundGatewayPIDFilePath())
	if err != nil {
		return backgroundGatewayPIDInfo{}, err
	}
	var info backgroundGatewayPIDInfo
	if err := json.Unmarshal(data, &info); err != nil {
		return backgroundGatewayPIDInfo{}, err
	}
	return info, nil
}

func removeBackgroundGatewayPIDFile() {
	_ = os.Remove(backgroundGatewayPIDFilePath())
}

func emitOnboardTelemetry(configPath string, info onboardTelemetryInfo) {
	cfg, err := config.Load(configPath)
	if err != nil {
		return
	}
	cfg.ResolveSecrets(keychain.ResolveField)

	emit := func(message string, fn func(context.Context) error) {
		ctx, cancel := context.WithTimeout(context.Background(), onboardTelemetryTimeout)
		defer cancel()
		if err := fn(ctx); err != nil {
			telemetry.DebugLog(cfg.Diagnostics, message, "error", err)
		}
	}

	emit("install telemetry failed", func(ctx context.Context) error {
		return telemetry.RecordInstall(ctx, cfg.Diagnostics, "onboard", cfg.Runtime.Profile)
	})
	emit("onboard telemetry failed", func(ctx context.Context) error {
		return telemetry.RecordOnboardCompleted(
			ctx,
			cfg.Diagnostics,
			info.Interactive,
			info.Provider,
			info.DaemonInstalled,
			info.SkillsSelectedCount,
			info.ReusedExistingConfig,
		)
	})
}

func promptExistingOnboardAction(configPath string, cfg config.Config, catalog cliSetupCatalog) (string, error) {
	choice := onboardExistingConfigDashboardValue
	summary := existingOnboardConfigSummary(cfg, catalog)
	form := huh.NewForm(
		huh.NewGroup(
			huh.NewSelect[string]().
				Title(itext("Existing HopClaw setup detected", "检测到已有 HopClaw 配置")).
				Description(fmt.Sprintf(
					itext("Config: %s\n%s", "配置文件：%s\n%s"),
					configPath,
					summary,
				)).
				Options(
					huh.NewOption(
						fmt.Sprintf(itext("Use existing config: %s", "使用已有配置：%s"), existingOnboardShortSummary(cfg, catalog)),
						onboardExistingConfigUseValue,
					),
					huh.NewOption(itext("Open dashboard for now", "直接打开控制台"), onboardExistingConfigDashboardValue),
					huh.NewOption(itext("Reconfigure from scratch", "重新从头配置"), onboardExistingConfigReconfigureValue),
				).
				Value(&choice),
		),
	)
	if err := form.Run(); err != nil {
		return "", err
	}
	return choice, nil
}

func existingOnboardConfigSummary(cfg config.Config, catalog cliSetupCatalog) string {
	lines := []string{
		fmt.Sprintf(itext("Auth: %s", "认证：%s"), existingOnboardAuthSummary(cfg)),
		fmt.Sprintf(itext("Models: %s", "模型：%s"), existingOnboardModelSummary(cfg, catalog)),
		fmt.Sprintf(itext("Channels: %s", "渠道：%s"), existingOnboardChannelSummary(cfg, catalog)),
		fmt.Sprintf(itext("Gateway: %s", "网关：%s"), existingOnboardGatewaySummary(cfg)),
	}
	return strings.Join(lines, "\n")
}

func existingOnboardShortSummary(cfg config.Config, catalog cliSetupCatalog) string {
	parts := []string{
		existingOnboardModelSummary(cfg, catalog),
		existingOnboardChannelSummary(cfg, catalog),
		existingOnboardGatewaySummary(cfg),
	}
	return strings.Join(parts, itext(" | ", "｜"))
}

func existingOnboardAuthSummary(cfg config.Config) string {
	switch {
	case strings.TrimSpace(cfg.Server.AuthToken) != "", strings.TrimSpace(cfg.Auth.BearerToken) != "":
		return authModeSummaryLabel("bearer")
	case len(cfg.Auth.APIKeys) > 0:
		return authModeSummaryLabel("apikey")
	case cfg.Auth.JWT != nil && (strings.TrimSpace(cfg.Auth.JWT.Secret) != "" || strings.TrimSpace(cfg.Auth.JWT.PublicKey) != ""):
		return authModeSummaryLabel("jwt")
	default:
		return authModeSummaryLabel("none")
	}
}

func existingOnboardModelSummary(cfg config.Config, catalog cliSetupCatalog) string {
	state, err := buildLocalModelProviderState(cfg)
	if err != nil || len(state.Providers) == 0 {
		return itext("Not configured yet", "暂未配置")
	}
	names := orderedModelProviderNames(state.Providers, catalog)
	labels := make([]string, 0, len(names))
	for _, name := range names {
		switch strings.TrimSpace(name) {
		case "":
			continue
		case "default":
			labels = append(labels, itext("OpenAI Compatible", "OpenAI Compatible"))
		default:
			labels = append(labels, catalog.ProviderDisplayName(name))
		}
	}
	if len(labels) == 0 {
		return itext("Not configured yet", "暂未配置")
	}
	return strings.Join(labels, ", ")
}

func existingOnboardChannelSummary(cfg config.Config, catalog cliSetupCatalog) string {
	rows := buildChannelRows(cfg.Channels)
	if len(rows) == 0 {
		return itext("None yet", "暂未配置")
	}
	labels := make([]string, 0, len(rows))
	for _, row := range rows {
		if profile, ok := catalog.LookupChannelProfile(row.Name); ok {
			labels = append(labels, profile.DisplayName)
			continue
		}
		labels = append(labels, row.Name)
	}
	return strings.Join(labels, ", ")
}

func existingOnboardGatewaySummary(cfg config.Config) string {
	addr := strings.TrimSpace(cfg.Server.Address)
	if addr == "" {
		addr = config.DefaultGatewayAddress
	}
	return addr
}

func runOnboardExistingConfigHandoff(configPath string, cfg config.Config, dashboardOnly bool) error {
	fmt.Println()
	if dashboardOnly {
		fmt.Println(itext("Keeping your existing config unchanged and continuing in the dashboard.", "保留现有配置不变，直接进入控制台。"))
	} else {
		fmt.Println(itext("Using your existing config and continuing in the dashboard.", "继续使用已有配置，直接进入控制台。"))
	}

	dashboardReady := false
	mgr, err := daemon.NewServiceManager()
	if err == nil {
		status, statusErr := mgr.Status()
		if statusErr != nil {
			fmt.Printf(itext("  Warning: could not query service status: %v\n", "  警告：无法查询服务状态：%v\n"), statusErr)
		} else if status != nil && status.Installed {
			if status.Running {
				fmt.Println(itext("  Service is already running.", "  后台服务已经在运行。"))
			} else if startErr := mgr.Start(); startErr != nil {
				fmt.Printf(itext("  Warning: could not start the installed service: %v\n", "  警告：无法启动已安装的后台服务：%v\n"), startErr)
			} else {
				fmt.Println(itext("  Service started.", "  后台服务已启动。"))
				time.Sleep(onboardRetryDelay)
			}
			dashboardReady = verifyDashboardConnectivityWithRetries()
		}
	}
	if !dashboardReady {
		logPath, startErr := startGatewayInBackground(configPath)
		if startErr != nil {
			fmt.Printf(itext("  Warning: could not start a temporary background gateway: %v\n", "  警告：无法启动临时后台网关：%v\n"), startErr)
			if strings.TrimSpace(logPath) != "" {
				fmt.Printf(itext("  Logs: %s\n", "  日志：%s\n"), logPath)
			}
		} else {
			fmt.Printf(itext("  Started a temporary background gateway for the dashboard (logs: %s)\n", "  已为控制台启动临时后台网关（日志：%s）\n"), logPath)
			dashboardReady = verifyDashboardConnectivityWithRetries()
		}
	}

	fmt.Println()
	if dashboardReady {
		printOnboardDashboardAccess()
		fmt.Println(itext("Existing setup handoff complete.", "已有配置接管完成。"))
	} else {
		fmt.Println(itext("Dashboard is not ready yet.", "控制台暂时还没有准备好。"))
		fmt.Println(itext("  Open it later with: hopclaw dashboard --open", "  你可以稍后运行：hopclaw dashboard --open"))
	}
	printOnboardSummary()
	emitOnboardTelemetry(configPath, onboardTelemetryInfo{
		Interactive:          true,
		Provider:             strings.TrimSpace(cfg.Models.DefaultProvider),
		DaemonInstalled:      false,
		SkillsSelectedCount:  0,
		ReusedExistingConfig: true,
	})
	return nil
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func printPlatformDaemonGuidance() {
	switch runtime.GOOS {
	case "darwin":
		fmt.Println(itext("  Platform: macOS (uses launchd)", "  平台：macOS（使用 launchd）"))
		fmt.Println(itext("  The service will be installed as a LaunchAgent.", "  后台服务会以 LaunchAgent 的方式安装。"))
	case "linux":
		fmt.Println(itext("  Platform: Linux (uses systemd)", "  平台：Linux（使用 systemd）"))
		fmt.Println(itext("  The service will be installed as a user systemd unit.", "  后台服务会以用户级 systemd 单元的方式安装。"))
	case "windows":
		fmt.Println(itext("  Platform: Windows (uses Task Scheduler)", "  平台：Windows（使用任务计划程序）"))
		fmt.Println(itext("  The service will run as a scheduled task at login.", "  后台服务会在登录时通过计划任务启动。"))
	default:
		fmt.Printf(itext("  Platform: %s (manual setup may be required)\n", "  平台：%s（可能需要手动配置）\n"), runtime.GOOS)
	}
}

func gatewayListenPromptDescription(initialAddr string) string {
	if strings.TrimSpace(initialAddr) != strings.TrimSpace(config.DefaultGatewayAddress) {
		return fmt.Sprintf(
			itext(
				"Default address %s is already in use on this machine. Press enter to use %s.",
				"默认地址 %s 在这台机器上已经被占用。直接回车即可使用 %s。",
			),
			config.DefaultGatewayAddress,
			initialAddr,
		)
	}
	return itext("Press enter to keep the default address.", "直接回车即可使用默认地址。")
}

func suggestAvailableGatewayAddress(addr string) string {
	addr = strings.TrimSpace(addr)
	if addr == "" {
		addr = config.DefaultGatewayAddress
	}
	if canListenOnAddress(addr) {
		return addr
	}

	host, portStr, err := net.SplitHostPort(addr)
	if err != nil {
		return addr
	}
	port, err := strconv.Atoi(strings.TrimSpace(portStr))
	if err != nil || port <= 0 {
		return addr
	}
	for offset := 1; offset <= 10; offset++ {
		candidate := net.JoinHostPort(host, strconv.Itoa(port+offset))
		if canListenOnAddress(candidate) {
			return candidate
		}
	}
	return addr
}

func canListenOnAddress(addr string) bool {
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return false
	}
	_ = listener.Close()
	return true
}

func availableRecommendedSkills(ctx context.Context, client *GatewayClient, candidates []string) []string {
	if len(candidates) == 0 {
		return nil
	}
	available := make([]string, 0, len(candidates))
	for _, candidate := range candidates {
		if recommendedSkillAvailableLocally(candidate) || recommendedSkillAvailableInCatalog(ctx, client, candidate) {
			available = append(available, candidate)
		}
	}
	return available
}

func recommendedSkillAvailableLocally(name string) bool {
	source, _ := resolveSkillInstallTarget(name)
	return strings.TrimSpace(source) != ""
}

func recommendedSkillAvailableInCatalog(ctx context.Context, client *GatewayClient, name string) bool {
	if client == nil || strings.TrimSpace(name) == "" {
		return false
	}
	var resp catalogSkillsResponse
	path := "/operator/skills/catalog?q=" + url.QueryEscape(strings.TrimSpace(name))
	if err := client.Get(ctx, path, &resp); err != nil {
		return false
	}
	target := strings.TrimSpace(strings.ToLower(name))
	for _, item := range resp.Items {
		if strings.TrimSpace(strings.ToLower(item.ID)) == target || strings.TrimSpace(strings.ToLower(item.Name)) == target {
			return true
		}
	}
	return false
}

func verifyGatewayConnectivity() bool {
	addr := resolveGatewayAddr()
	client, err := NewGatewayClient()
	if err != nil {
		fmt.Printf(itext("  Gateway client error: %v\n", "  网关客户端错误：%v\n"), err)
		fmt.Println(itext("  Run 'hopclaw health' to check later.", "  你可以稍后运行 'hopclaw health' 检查状态。"))
		return false
	}
	client.HTTP.Timeout = onboardHealthTimeout
	return verifyGatewayConnectivityWithClient(context.Background(), os.Stdout, client, addr)
}

func verifyDashboardConnectivityWithRetries() bool {
	addr := resolveGatewayAddr()
	client, err := NewGatewayClient()
	if err != nil {
		fmt.Printf(itext("  Gateway client error: %v\n", "  网关客户端错误：%v\n"), err)
		fmt.Println(itext("  Run 'hopclaw dashboard --open' to retry later.", "  你可以稍后运行 'hopclaw dashboard --open' 再试一次。"))
		return false
	}
	client.HTTP.Timeout = onboardHealthTimeout
	return verifyDashboardConnectivityWithRetriesWithClient(context.Background(), os.Stdout, client, addr)
}

func verifyDashboardConnectivityWithRetriesWithClient(parent context.Context, out io.Writer, client *GatewayClient, addr string) bool {
	for attempt := 1; attempt <= onboardVerifyMaxRetries; attempt++ {
		ctx, cancel := context.WithTimeout(parent, onboardHealthTimeout)
		body, statusCode, err := client.GetRawWithStatus(ctx, "/dashboard/api/config")
		cancel()

		if err == nil {
			if statusCode < 400 {
				fmt.Fprintf(out, itext("  Dashboard at %s is ready (HTTP %d).\n", "  控制台 %s 已可用（HTTP %d）。\n"), addr, statusCode)
				return true
			}
			fmt.Fprintf(out, itext("  Dashboard at %s responded with %s.\n", "  控制台 %s 返回了 %s。\n"), addr, gatewayHTTPError(statusCode, body))
			return false
		}

		if attempt < onboardVerifyMaxRetries {
			fmt.Fprintf(out, itext("  Attempt %d/%d: dashboard not reachable yet, retrying...\n", "  第 %d/%d 次尝试：控制台暂不可访问，正在重试...\n"), attempt, onboardVerifyMaxRetries)
			time.Sleep(onboardRetryDelay)
		}
	}

	fmt.Fprintf(out, itext("  Dashboard at %s is not reachable after %d attempts.\n", "  控制台 %s 在连续 %d 次尝试后仍不可访问。\n"), addr, onboardVerifyMaxRetries)
	fmt.Fprintln(out, itext("  Run 'hopclaw dashboard --open' to retry later.", "  你可以稍后运行 'hopclaw dashboard --open' 再试一次。"))
	return false
}

func verifyGatewayConnectivityWithClient(parent context.Context, out io.Writer, client *GatewayClient, addr string) bool {
	ctx, cancel := context.WithTimeout(parent, onboardHealthTimeout)
	defer cancel()

	body, statusCode, err := fetchOperatorStatus(ctx, client)
	if err != nil {
		fmt.Fprintf(out, itext("  Gateway at %s is not reachable.\n", "  网关 %s 当前不可访问。\n"), addr)
		fmt.Fprintln(out, itext("  This is OK if the service hasn't started yet.", "  如果服务还没启动，这是正常的。"))
		fmt.Fprintln(out, itext("  Run 'hopclaw health' to check later.", "  你可以稍后运行 'hopclaw health' 检查状态。"))
		return false
	}

	if statusCode < 400 {
		fmt.Fprintf(out, itext("  Gateway at %s is healthy (HTTP %d).\n", "  网关 %s 正常（HTTP %d）。\n"), addr, statusCode)
		return true
	}
	fmt.Fprintf(out, itext("  Gateway at %s responded with %s.\n", "  网关 %s 返回了 %s。\n"), addr, gatewayHTTPError(statusCode, body))
	return false
}

func verifyGatewayConnectivityWithRetries() bool {
	addr := resolveGatewayAddr()
	client, err := NewGatewayClient()
	if err != nil {
		fmt.Printf(itext("  Gateway client error: %v\n", "  网关客户端错误：%v\n"), err)
		fmt.Println(itext("  Run 'hopclaw health' to check later.", "  你可以稍后运行 'hopclaw health' 检查状态。"))
		return false
	}
	client.HTTP.Timeout = onboardHealthTimeout
	return verifyGatewayConnectivityWithRetriesWithClient(context.Background(), os.Stdout, client, addr)
}

func verifyGatewayConnectivityWithRetriesWithClient(parent context.Context, out io.Writer, client *GatewayClient, addr string) bool {
	for attempt := 1; attempt <= onboardVerifyMaxRetries; attempt++ {
		ctx, cancel := context.WithTimeout(parent, onboardHealthTimeout)
		body, statusCode, err := fetchOperatorStatus(ctx, client)
		cancel()

		if err == nil {
			if statusCode < 400 {
				fmt.Fprintf(out, itext("  Gateway at %s is healthy (HTTP %d).\n", "  网关 %s 正常（HTTP %d）。\n"), addr, statusCode)
				return true
			}
			fmt.Fprintf(out, itext("  Gateway at %s responded with %s.\n", "  网关 %s 返回了 %s。\n"), addr, gatewayHTTPError(statusCode, body))
			return false
		}

		if attempt < onboardVerifyMaxRetries {
			fmt.Fprintf(out, itext("  Attempt %d/%d: gateway not reachable, retrying...\n", "  第 %d/%d 次尝试：网关暂不可访问，正在重试...\n"), attempt, onboardVerifyMaxRetries)
			time.Sleep(onboardRetryDelay)
		}
	}

	fmt.Fprintf(out, itext("  Gateway at %s is not reachable after %d attempts.\n", "  网关 %s 在连续 %d 次尝试后仍不可访问。\n"), addr, onboardVerifyMaxRetries)
	fmt.Fprintln(out, itext("  This is OK if the service hasn't started yet.", "  如果服务还没启动，这是正常的。"))
	fmt.Fprintln(out, itext("  Run 'hopclaw health' to check later.", "  你可以稍后运行 'hopclaw health' 检查状态。"))
	return false
}

func printOnboardSummary() {
	fmt.Println()
	fmt.Println(itext("Useful commands:", "常用命令："))
	fmt.Println(itext("  hopclaw serve        # start the gateway manually", "  hopclaw serve        # 手动启动网关"))
	fmt.Println(itext("  hopclaw dashboard    # show the local dashboard URL", "  hopclaw dashboard    # 查看本地控制台地址"))
	fmt.Println(itext("  hopclaw dashboard --open  # open the local dashboard", "  hopclaw dashboard --open  # 打开本地控制台"))
	fmt.Println(itext("  hopclaw status       # check gateway status", "  hopclaw status       # 查看网关状态"))
	fmt.Println(itext("  hopclaw health       # health check", "  hopclaw health       # 健康检查"))
	fmt.Println(itext("  hopclaw config show  # view configuration", "  hopclaw config show  # 查看配置"))
	fmt.Println(itext("  hopclaw doctor       # diagnose issues", "  hopclaw doctor       # 诊断问题"))
	fmt.Println(itext("  hopclaw daemon stop  # stop the service", "  hopclaw daemon stop  # 停止后台服务"))
}

func authModeSummaryLabel(mode string) string {
	switch strings.TrimSpace(strings.ToLower(mode)) {
	case "bearer":
		return itext("Bearer token", "Bearer Token")
	case "apikey":
		return itext("API key", "API Key")
	case "jwt":
		return "JWT"
	case "none":
		return itext("None", "无认证")
	default:
		return mode
	}
}

func printOnboardDashboardAccess() {
	displayURL, openURL, err := onboardDashboardURLs()
	if err != nil {
		fmt.Println(itext("  Open the dashboard with: hopclaw dashboard --open", "  你可以运行：hopclaw dashboard --open 打开控制台"))
		return
	}
	fmt.Printf(itext("Dashboard: %s\n", "控制台地址：%s\n"), displayURL)
	if isTrueEnv("HOPCLAW_NO_BROWSER") {
		fmt.Println(itext("  Open it later with: hopclaw dashboard --open", "  你可以稍后运行：hopclaw dashboard --open"))
		return
	}
	if err := openDashboardURL(openURL); err != nil {
		fmt.Println(itext("  Open it manually in your browser if needed.", "  如果没有自动打开，请手动在浏览器里打开。"))
		return
	}
	fmt.Println(itext("  Opened the dashboard in your default browser.", "  已在默认浏览器中打开控制台。"))
}

func onboardDashboardURLs() (string, string, error) {
	access, err := resolveGatewayAccess()
	if err != nil {
		return "", "", err
	}
	displayURL, openURL := dashboardURLs(access)
	return displayURL, openURL, nil
}
