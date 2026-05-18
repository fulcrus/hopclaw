package cli

import (
	"context"
	"fmt"
	"os"

	"github.com/fulcrus/hopclaw/config"
	"github.com/fulcrus/hopclaw/internal/daemon"
	"github.com/charmbracelet/huh"
	"github.com/spf13/cobra"
)

func newSetupCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "setup",
		Short: "Minimal first-time configuration",
		Long: `Run the setup wizard to create a config file and optionally
install HopClaw as a system service.

This creates ~/.hopclaw/config.yaml with your model provider settings.
For the fuller guided path, use 'hopclaw onboard'.`,
		RunE: runSetup,
	}
	cmd.Flags().Bool("non-interactive", false, "skip prompts, use env vars only")
	return cmd
}

func runSetup(cmd *cobra.Command, _ []string) error {
	configPath := daemon.ConfigFilePath()

	// Check if config already exists.
	if _, err := os.Stat(configPath); err == nil {
		fmt.Printf("Config file already exists at %s\n", configPath)
		fmt.Println("Use 'hopclaw config edit' or 'hopclaw config set <key> <value>' to modify settings.")
		fmt.Println("Delete the file and re-run 'hopclaw setup' to start fresh.")
		return nil
	}

	nonInteractive, _ := cmd.Flags().GetBool("non-interactive")
	if nonInteractive {
		return setupNonInteractive(configPath)
	}

	return setupInteractive(cmd.Context(), configPath)
}

// ---------------------------------------------------------------------------
// Non-interactive setup (env var detection only)
// ---------------------------------------------------------------------------

func setupNonInteractive(configPath string) error {
	provider, key := config.DetectAPIKey()
	if provider == "" {
		return fmt.Errorf("%s", config.MissingAPIKeyMessage())
	}

	if err := daemon.EnsureStateDir(); err != nil {
		return fmt.Errorf("create state dir: %w", err)
	}

	cfgContent := config.GenerateDefaultConfig()
	if err := os.WriteFile(configPath, []byte(cfgContent), 0o644); err != nil {
		return fmt.Errorf("write config: %w", err)
	}

	if len(key) >= 8 {
		fmt.Printf("Detected %s API key: %s...%s\n", provider, key[:4], key[len(key)-4:])
	} else {
		fmt.Printf("Detected %s API key: ****\n", provider)
	}
	fmt.Printf("Config written to %s\n", configPath)
	return nil
}

// ---------------------------------------------------------------------------
// Interactive setup (charmbracelet/huh)
// ---------------------------------------------------------------------------

func setupInteractive(ctx context.Context, configPath string) error {
	catalog := loadCLISetupCatalogBestEffort(ctx)

	opts, err := collectProviderSetupOptionsWithCatalog(catalog)
	if err != nil {
		return err
	}
	channelSelections, err := promptSingleSetupChannelWithCatalog(catalog)
	if err != nil {
		return fmt.Errorf("channel selection: %w", err)
	}
	opts.Channels = channelSelections

	// Step 5: Generate and write config.
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

	fmt.Printf("\nConfig written to %s\n", configPath)

	// Validate the generated config.
	if _, err := config.Load(configPath); err != nil {
		log.Warn("generated config has validation issues", "error", err)
	}

	// Step 6: Offer to install as daemon.
	var installDaemon bool
	installForm := huh.NewForm(
		huh.NewGroup(
			huh.NewConfirm().
				Title("Install as system service? (auto-start on login)").
				Value(&installDaemon),
		),
	)
	if err := installForm.Run(); err != nil {
		return fmt.Errorf("daemon prompt: %w", err)
	}

	if installDaemon {
		// Delegate to daemon install logic.
		if err := runDaemonInstall(nil, nil); err != nil {
			fmt.Printf("Warning: failed to install daemon: %v\n", err)
			fmt.Println("You can install later with: hopclaw daemon install")
		} else {
			fmt.Println("System service installed and enabled.")
		}
	}

	fmt.Println()
	fmt.Println("Setup complete! Next steps:")
	fmt.Println("  hopclaw              # start the gateway")
	fmt.Println("  hopclaw status       # check running status")
	fmt.Println("  hopclaw config show  # review configuration")

	return nil
}
