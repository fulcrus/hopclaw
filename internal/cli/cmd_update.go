package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/fulcrus/hopclaw/config"
	"github.com/fulcrus/hopclaw/internal/update"
	"github.com/fulcrus/hopclaw/internal/version"
	"github.com/spf13/cobra"
)

// ---------------------------------------------------------------------------
// update command
// ---------------------------------------------------------------------------

const updateCheckTimeout = 15 * time.Second

var updateCheckWithPolicy = update.CheckWithPolicy

func newUpdateCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "update",
		Short: "Check GitHub releases for updates",
		Long: `Check the latest HopClaw release on GitHub and compare it with the
current version. This command is advisory-only: it does not replace the
binary automatically and only shows release information plus the download URL.`,
		RunE: runUpdate,
	}

	cmd.Flags().Bool("check", false, "only check for updates without showing upgrade guidance")
	cmd.Flags().String("channel", "", "release channel to inspect (stable, beta, nightly)")
	cmd.Flags().String("version", "", "not supported in advisory mode; `hopclaw update` only checks releases")
	cmd.Flags().Bool("no-restart", false, "not supported in advisory mode; `hopclaw update` never restarts services")
	cmd.Flags().BoolP("yes", "y", false, "not supported in advisory mode; `hopclaw update` never installs binaries")

	return cmd
}

func runUpdate(cmd *cobra.Command, _ []string) error {
	checkOnly, _ := cmd.Flags().GetBool("check")
	requestedChannel, _ := cmd.Flags().GetString("channel")
	requestedVersion, _ := cmd.Flags().GetString("version")
	assumeYes, _ := cmd.Flags().GetBool("yes")
	noRestart, _ := cmd.Flags().GetBool("no-restart")

	if err := rejectUnsupportedUpdateFlags(assumeYes, noRestart, requestedVersion); err != nil {
		return err
	}

	policy := loadUpdatePolicy()
	if requestedChannel != "" {
		policy.Channel = requestedChannel
	}
	policy.ManifestURL = ""
	policy.DisableManifest = true

	ctx, cancel := context.WithTimeout(context.Background(), updateCheckTimeout)
	defer cancel()

	result, err := updateCheckWithPolicy(ctx, policy)
	if flagJSON {
		enc := json.NewEncoder(cmd.OutOrStdout())
		enc.SetIndent("", "  ")
		if result == nil {
			result = &update.CheckResult{
				CurrentVersion: version.Version,
				CurrentChannel: policy.Channel,
				Error:          errorString(err),
			}
		}
		return enc.Encode(result)
	}

	out := cmd.OutOrStdout()
	fmt.Fprintf(out, "Current version: %s\n", version.Full())
	fmt.Fprintf(out, "Channel: %s\n", valueOrFallback(resultChannel(result, policy.Channel), "stable"))
	fmt.Fprintln(out, "Source: GitHub releases API")

	if err != nil {
		fmt.Fprintf(out, "Update check failed: %v\n", err)
		fmt.Fprintf(out, "Releases: %s\n", version.DefaultReleasesURL)
		return nil
	}
	if result == nil {
		fmt.Fprintf(out, "Update check returned no release information.\n")
		fmt.Fprintf(out, "Releases: %s\n", version.DefaultReleasesURL)
		return nil
	}

	if result.UpToDate {
		fmt.Fprintln(out, "You are up to date.")
		return nil
	}

	fmt.Fprintf(out, "Latest version: %s\n", valueOrFallback(result.LatestVersion, "unknown"))
	if result.LatestChannel != "" && result.LatestChannel != result.CurrentChannel {
		fmt.Fprintf(out, "Latest channel: %s\n", result.LatestChannel)
	}
	if !result.PublishedAt.IsZero() {
		fmt.Fprintf(out, "Published at: %s\n", result.PublishedAt.Format(time.RFC3339))
	}
	if result.UpdateURL != "" {
		fmt.Fprintf(out, "Download: %s\n", result.UpdateURL)
	}
	if result.Notes != "" {
		fmt.Fprintf(out, "Notes: %s\n", oneLineSummary(result.Notes))
	}
	if checkOnly {
		return nil
	}

	fmt.Fprintln(out, "A newer release is available. Review the notes above and download it from the release URL.")
	return nil
}

func loadUpdatePolicy() update.Policy {
	policy := update.DefaultPolicy()
	configPath := resolveConfigPath()
	if configPath == "" {
		return policy
	}
	cfg, err := config.Load(configPath)
	if err != nil {
		return policy
	}
	if cfg.Update.Enabled != nil {
		policy.Enabled = *cfg.Update.Enabled
	}
	if cfg.Update.CheckOnStart != nil {
		policy.CheckOnStart = *cfg.Update.CheckOnStart
	}
	if cfg.Update.CheckInterval > 0 {
		policy.CheckInterval = cfg.Update.CheckInterval
	}
	if cfg.Update.Channel != "" {
		policy.Channel = cfg.Update.Channel
	}
	if cfg.Update.ManifestURL != "" {
		policy.ManifestURL = cfg.Update.ManifestURL
	}
	if cfg.Update.SkipVersion != "" {
		policy.SkipVersion = cfg.Update.SkipVersion
	}
	return policy
}

func oneLineSummary(text string) string {
	text = strings.TrimSpace(text)
	if text == "" {
		return ""
	}
	text = strings.Join(strings.Fields(text), " ")
	const limit = 120
	if len(text) <= limit {
		return text
	}
	return text[:limit-1] + "…"
}

func resultChannel(result *update.CheckResult, fallback string) string {
	if result == nil {
		return fallback
	}
	if strings.TrimSpace(result.CurrentChannel) != "" {
		return result.CurrentChannel
	}
	return fallback
}

func errorString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

func rejectUnsupportedUpdateFlags(assumeYes, noRestart bool, requestedVersion string) error {
	flags := make([]string, 0, 3)
	if assumeYes {
		flags = append(flags, "--yes")
	}
	if noRestart {
		flags = append(flags, "--no-restart")
	}
	if strings.TrimSpace(requestedVersion) != "" {
		flags = append(flags, "--version")
	}
	if len(flags) == 0 {
		return nil
	}
	return fmt.Errorf("unsupported in advisory-only mode: %s. `hopclaw update` only checks GitHub releases and never installs binaries or restarts services", strings.Join(flags, ", "))
}
