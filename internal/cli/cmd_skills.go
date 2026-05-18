package cli

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
)

const skillSummaryPreviewLen = 72

type installedSkillRow struct {
	ID          string `json:"id,omitempty"`
	Name        string `json:"name"`
	Kind        string `json:"kind,omitempty"`
	Status      string `json:"status,omitempty"`
	Trust       string `json:"trust,omitempty"`
	Version     string `json:"version,omitempty"`
	InstallDir  string `json:"install_dir,omitempty"`
	BundleDir   string `json:"bundle_dir,omitempty"`
	Pinned      bool   `json:"pinned,omitempty"`
	InstalledAt string `json:"installed_at,omitempty"`
}

type installedSkillsResponse struct {
	Items []installedSkillRow `json:"items"`
	Count int                 `json:"count"`
}

type catalogSkillRow struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Version   string `json:"version,omitempty"`
	Summary   string `json:"summary,omitempty"`
	Installed bool   `json:"installed"`
}

type catalogSkillsResponse struct {
	Items []catalogSkillRow `json:"items"`
	Count int               `json:"count"`
}

type skillInstallResponse struct {
	OK           bool             `json:"ok"`
	SkillID      string           `json:"skill_id"`
	Version      string           `json:"version,omitempty"`
	InstallDir   string           `json:"install_dir,omitempty"`
	LockFile     string           `json:"lock_file,omitempty"`
	InstallSteps []map[string]any `json:"install_steps,omitempty"`
}

func newSkillsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "skills",
		Short: "Manage discovered and installable skill packages",
		Long:  "List discovered skills, search the skill catalog, and install or remove skill packages through the running gateway.",
	}

	cmd.AddCommand(
		newSkillsListCmd(),
		newSkillsSearchCmd(),
		newSkillsInfoCmd(),
		newSkillsInstallCmd(),
		newSkillsRemoveCmd(),
	)

	return cmd
}

func newSkillsListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List discovered skills",
		Long:  "List discovered skill packages from installed, workspace, and compatibility roots on the running gateway.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runSkillsList(cmd.Context())
		},
	}
}

func runSkillsList(ctx context.Context) error {
	client, err := NewGatewayClient()
	if err != nil {
		return err
	}

	var resp installedSkillsResponse
	if err := client.Get(ctx, "/operator/skills", &resp); err != nil {
		return err
	}
	if flagJSON {
		return printJSON(resp)
	}
	if len(resp.Items) == 0 {
		fmt.Println("no skills discovered")
		return nil
	}

	fmt.Printf("%-24s  %-10s  %-10s  %-10s  %s\n", "NAME", "VERSION", "STATUS", "TRUST", "LOCATION")
	fmt.Printf("%-24s  %-10s  %-10s  %-10s  %s\n", "----", "-------", "------", "-----", "--------")
	for _, item := range resp.Items {
		location := item.InstallDir
		if location == "" {
			location = item.BundleDir
		}
		fmt.Printf("%-24s  %-10s  %-10s  %-10s  %s\n",
			truncate(valueOrFallback(item.Name, item.ID), 24),
			valueOrDash(item.Version),
			valueOrDash(item.Status),
			valueOrDash(item.Trust),
			valueOrDash(location),
		)
	}
	fmt.Printf("\nTotal: %d skills\n", resp.Count)
	return nil
}

func newSkillsSearchCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "search <query>",
		Short: "Search the skill catalog",
		Long:  "Search the configured skill catalog for installable skill packages.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runSkillsSearch(cmd.Context(), args[0])
		},
	}
}

func runSkillsSearch(ctx context.Context, query string) error {
	client, err := NewGatewayClient()
	if err != nil {
		return err
	}

	var resp catalogSkillsResponse
	path := "/operator/skills/catalog?q=" + url.QueryEscape(strings.TrimSpace(query))
	if err := client.Get(ctx, path, &resp); err != nil {
		return err
	}
	if flagJSON {
		return printJSON(resp)
	}
	if len(resp.Items) == 0 {
		fmt.Printf("no skills matching %q\n", query)
		return nil
	}

	fmt.Printf("%-24s  %-10s  %-10s  %s\n", "NAME", "VERSION", "INSTALLED", "SUMMARY")
	fmt.Printf("%-24s  %-10s  %-10s  %s\n", "----", "-------", "---------", "-------")
	for _, item := range resp.Items {
		fmt.Printf("%-24s  %-10s  %-10v  %s\n",
			truncate(valueOrFallback(item.Name, item.ID), 24),
			valueOrDash(item.Version),
			item.Installed,
			truncate(item.Summary, skillSummaryPreviewLen),
		)
	}
	fmt.Printf("\nMatched: %d skills\n", resp.Count)
	return nil
}

func newSkillsInfoCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "info <name>",
		Short: "Show installed and catalog information for a skill",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runSkillsInfo(cmd.Context(), args[0])
		},
	}
}

func runSkillsInfo(ctx context.Context, name string) error {
	client, err := NewGatewayClient()
	if err != nil {
		return err
	}

	var installed installedSkillsResponse
	if err := client.Get(ctx, "/operator/skills", &installed); err != nil {
		return err
	}
	var catalog catalogSkillsResponse
	if err := client.Get(ctx, "/operator/skills/catalog?q="+url.QueryEscape(strings.TrimSpace(name)), &catalog); err != nil {
		return err
	}

	var installedMatch *installedSkillRow
	for i := range installed.Items {
		if strings.EqualFold(installed.Items[i].ID, name) || strings.EqualFold(installed.Items[i].Name, name) {
			installedMatch = &installed.Items[i]
			break
		}
	}

	var catalogMatch *catalogSkillRow
	for i := range catalog.Items {
		if strings.EqualFold(catalog.Items[i].ID, name) || strings.EqualFold(catalog.Items[i].Name, name) {
			catalogMatch = &catalog.Items[i]
			break
		}
	}
	if catalogMatch == nil && len(catalog.Items) > 0 {
		catalogMatch = &catalog.Items[0]
	}

	if installedMatch == nil && catalogMatch == nil {
		return fmt.Errorf("skill %q not found", name)
	}

	if flagJSON {
		return printJSON(map[string]any{
			"installed": installedMatch,
			"catalog":   catalogMatch,
		})
	}

	if installedMatch != nil {
		fmt.Println("Installed")
		fmt.Printf("Name:        %s\n", valueOrFallback(installedMatch.Name, installedMatch.ID))
		fmt.Printf("ID:          %s\n", valueOrDash(installedMatch.ID))
		fmt.Printf("Version:     %s\n", valueOrDash(installedMatch.Version))
		fmt.Printf("Status:      %s\n", valueOrDash(installedMatch.Status))
		fmt.Printf("Trust:       %s\n", valueOrDash(installedMatch.Trust))
		fmt.Printf("Install dir: %s\n", valueOrDash(installedMatch.InstallDir))
		fmt.Printf("Bundle dir:  %s\n", valueOrDash(installedMatch.BundleDir))
		if installedMatch.InstalledAt != "" {
			fmt.Printf("Installed:   %s\n", installedMatch.InstalledAt)
		}
	}

	if catalogMatch != nil {
		if installedMatch != nil {
			fmt.Println()
		}
		fmt.Println("Catalog")
		fmt.Printf("Name:      %s\n", valueOrFallback(catalogMatch.Name, catalogMatch.ID))
		fmt.Printf("ID:        %s\n", valueOrDash(catalogMatch.ID))
		fmt.Printf("Version:   %s\n", valueOrDash(catalogMatch.Version))
		fmt.Printf("Installed: %v\n", catalogMatch.Installed)
		fmt.Printf("Summary:   %s\n", valueOrDash(catalogMatch.Summary))
	}

	return nil
}

func newSkillsInstallCmd() *cobra.Command {
	var version string
	cmd := &cobra.Command{
		Use:   "install <name-or-path>",
		Short: "Install a skill from the catalog or a local skill directory",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runSkillsInstall(cmd.Context(), args[0], version)
		},
	}
	cmd.Flags().StringVar(&version, "version", "", "specific version to install")
	return cmd
}

func runSkillsInstall(ctx context.Context, raw string, version string) error {
	client, err := NewGatewayClient()
	if err != nil {
		return err
	}

	source, name := resolveSkillInstallTarget(strings.TrimSpace(raw))
	req := map[string]any{
		"name":   name,
		"source": valueOrFallback(source, raw),
	}
	if version != "" {
		req["version"] = version
	}

	var resp skillInstallResponse
	if err := client.Post(ctx, "/operator/skills/install", req, &resp); err != nil {
		return err
	}
	if flagJSON {
		return printJSON(resp)
	}

	fmt.Printf("Installed skill %s", valueOrFallback(resp.SkillID, name))
	if resp.Version != "" {
		fmt.Printf(" v%s", resp.Version)
	}
	fmt.Println()
	if resp.InstallDir != "" {
		fmt.Printf("Install dir: %s\n", resp.InstallDir)
	}
	if len(resp.InstallSteps) > 0 {
		fmt.Printf("Installer steps: %d\n", len(resp.InstallSteps))
	}
	return nil
}

func newSkillsRemoveCmd() *cobra.Command {
	return &cobra.Command{
		Use:     "remove <name>",
		Aliases: []string{"delete", "uninstall"},
		Short:   "Remove an installed skill",
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runSkillsRemove(cmd.Context(), args[0])
		},
	}
}

func runSkillsRemove(ctx context.Context, name string) error {
	client, err := NewGatewayClient()
	if err != nil {
		return err
	}
	var resp map[string]any
	if err := client.Delete(ctx, "/operator/skills/"+url.PathEscape(strings.TrimSpace(name)), &resp); err != nil {
		return err
	}
	if flagJSON {
		return printJSON(resp)
	}
	fmt.Printf("Removed skill %s\n", name)
	return nil
}

func resolveSkillInstallTarget(raw string) (source string, name string) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", ""
	}
	if resolved := firstExistingSkillPath(raw); resolved != "" {
		return resolved, filepath.Base(resolved)
	}
	for _, candidate := range localSkillCandidates(raw) {
		if resolved := firstExistingSkillPath(candidate); resolved != "" {
			return resolved, raw
		}
	}
	return "", raw
}

func localSkillCandidates(name string) []string {
	candidates := []string{
		filepath.Join(".", "skills", name),
		filepath.Join(".", ".hopclaw", "skills", name),
		filepath.Join(".", ".openclaw", "workspace", "skills", name),
		filepath.Join(".", ".openclaw", "skills", name),
	}
	if home, err := os.UserHomeDir(); err == nil && strings.TrimSpace(home) != "" {
		candidates = append(candidates,
			filepath.Join(home, ".hopclaw", "skills", name),
			filepath.Join(home, ".openclaw", "workspace", "skills", name),
			filepath.Join(home, ".openclaw", "skills", name),
		)
	}
	return candidates
}

func firstExistingSkillPath(path string) string {
	if strings.TrimSpace(path) == "" {
		return ""
	}
	info, err := os.Stat(path)
	if err != nil || !info.IsDir() {
		return ""
	}
	if _, err := os.Stat(filepath.Join(path, "SKILL.md")); err != nil {
		return ""
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return path
	}
	return abs
}

func valueOrFallback(primary, fallback string) string {
	if strings.TrimSpace(primary) != "" {
		return primary
	}
	return fallback
}

func valueOrDash(s string) string {
	if strings.TrimSpace(s) == "" {
		return "-"
	}
	return s
}
