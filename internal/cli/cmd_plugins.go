package cli

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	pluginpkg "github.com/fulcrus/hopclaw/plugin"
	sdkplugin "github.com/fulcrus/hopclaw/sdk/plugin"
	"github.com/spf13/cobra"
)

// ---------------------------------------------------------------------------
// Constants
// ---------------------------------------------------------------------------

const (
	pluginsBasePath = "/operator/plugins"
)

// ---------------------------------------------------------------------------
// Response types (mirror the gateway JSON shapes)
// ---------------------------------------------------------------------------

type pluginEntry struct {
	Name            string         `json:"name"`
	Version         string         `json:"version"`
	Description     string         `json:"description,omitempty"`
	Author          string         `json:"author,omitempty"`
	Enabled         bool           `json:"enabled"`
	Source          string         `json:"source,omitempty"`
	ComponentCounts map[string]int `json:"component_counts,omitempty"`
}

type pluginListResponse struct {
	Items []pluginEntry `json:"items"`
	Count int           `json:"count"`
}

type pluginGetResponse struct {
	Plugin          pluginEntry            `json:"plugin"`
	Components      []pluginComponentEntry `json:"components,omitempty"`
	ComponentCounts map[string]int         `json:"component_counts,omitempty"`
	Channels        map[string]any         `json:"channels,omitempty"`
}

type pluginComponentEntry struct {
	Kind        string         `json:"kind"`
	Name        string         `json:"name,omitempty"`
	Description string         `json:"description,omitempty"`
	Path        string         `json:"path,omitempty"`
	Metadata    map[string]any `json:"metadata,omitempty"`
}

type pluginOKResponse struct {
	OK   bool   `json:"ok"`
	Name string `json:"name,omitempty"`
}

type pluginInstallRequest struct {
	Source string `json:"source"`
}

type pluginValidateResponse struct {
	OK     bool     `json:"ok"`
	Name   string   `json:"name,omitempty"`
	Path   string   `json:"path"`
	Errors []string `json:"errors,omitempty"`
}

// ---------------------------------------------------------------------------
// Parent command
// ---------------------------------------------------------------------------

func newPluginsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "plugins",
		Aliases: []string{"plugin"},
		Short:   "Manage plugins",
		Long:    "List, inspect, install, and manage plugins on the running gateway.",
	}
	cmd.AddCommand(
		newPluginsInitCmd(),
		newPluginsListCmd(),
		newPluginsInfoCmd(),
		newPluginsEnableCmd(),
		newPluginsDisableCmd(),
		newPluginsInstallCmd(),
		newPluginsUninstallCmd(),
		newPluginsValidateCmd(),
	)
	return cmd
}

// ---------------------------------------------------------------------------
// plugins list
// ---------------------------------------------------------------------------

func newPluginsListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List all plugins",
		Long:  "List all plugins from the running gateway.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runPluginsList(cmd.Context())
		},
	}
}

func runPluginsList(ctx context.Context) error {
	client, err := NewGatewayClient()
	if err != nil {
		return err
	}

	var resp pluginListResponse
	if err := client.Get(ctx, pluginsBasePath, &resp); err != nil {
		return err
	}

	if flagJSON {
		return printJSON(resp)
	}

	if len(resp.Items) == 0 {
		fmt.Println("no plugins found")
		return nil
	}

	fmt.Printf("%-20s  %-10s  %-8s  %-10s  %s\n",
		"NAME", "VERSION", "STATUS", "COMPONENTS", "DESCRIPTION")
	fmt.Printf("%-20s  %-10s  %-8s  %-10s  %s\n",
		"----", "-------", "------", "----------", "-----------")
	for _, p := range resp.Items {
		status := "disabled"
		if p.Enabled {
			status = "enabled"
		}
		fmt.Printf("%-20s  %-10s  %-8s  %-10s  %s\n",
			truncate(p.Name, 20),
			truncate(p.Version, 10),
			status,
			truncate(renderComponentCounts(p.ComponentCounts), 10),
			truncate(p.Description, 40),
		)
	}
	fmt.Printf("\nTotal: %d plugins\n", resp.Count)
	return nil
}

// ---------------------------------------------------------------------------
// plugins info
// ---------------------------------------------------------------------------

func newPluginsInfoCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "info <name>",
		Short: "Show plugin details",
		Long:  "Show details for a plugin including its packaged extension components.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runPluginsInfo(cmd.Context(), args[0])
		},
	}
}

func runPluginsInfo(ctx context.Context, name string) error {
	client, err := NewGatewayClient()
	if err != nil {
		return err
	}

	var resp pluginGetResponse
	if err := client.Get(ctx, pluginsBasePath+"/"+name, &resp); err != nil {
		return err
	}

	if flagJSON {
		return printJSON(resp)
	}

	p := resp.Plugin
	status := "disabled"
	if p.Enabled {
		status = "enabled"
	}

	fmt.Printf("Name:        %s\n", p.Name)
	fmt.Printf("Version:     %s\n", p.Version)
	fmt.Printf("Status:      %s\n", status)
	if p.Author != "" {
		fmt.Printf("Author:      %s\n", p.Author)
	}
	if p.Description != "" {
		fmt.Printf("Description: %s\n", p.Description)
	}
	if p.Source != "" {
		fmt.Printf("Source:      %s\n", p.Source)
	}
	if len(resp.ComponentCounts) > 0 {
		fmt.Printf("Components:  %s\n", renderComponentCounts(resp.ComponentCounts))
	}
	if len(resp.Components) > 0 {
		fmt.Printf("\nComponents:\n")
		for _, item := range resp.Components {
			line := fmt.Sprintf("  - %s", item.Kind)
			if item.Name != "" {
				line += " / " + item.Name
			}
			if item.Description != "" {
				line += " — " + item.Description
			}
			fmt.Println(line)
			if item.Path != "" {
				fmt.Printf("      path: %s\n", item.Path)
			}
			if len(item.Metadata) > 0 {
				for _, key := range sortedKeys(item.Metadata) {
					fmt.Printf("      %s: %v\n", key, item.Metadata[key])
				}
			}
		}
	}

	return nil
}

func renderComponentCounts(counts map[string]int) string {
	if len(counts) == 0 {
		return "-"
	}
	parts := make([]string, 0, len(counts))
	for _, key := range sortedKeysInt(counts) {
		parts = append(parts, fmt.Sprintf("%s=%d", key, counts[key]))
	}
	return strings.Join(parts, ",")
}

func sortedKeysInt(values map[string]int) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	return sortedStrings(keys)
}

func sortedKeys(values map[string]any) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	return sortedStrings(keys)
}

func sortedStrings(values []string) []string {
	out := append([]string(nil), values...)
	sort.Strings(out)
	return out
}

// ---------------------------------------------------------------------------
// plugins enable
// ---------------------------------------------------------------------------

func newPluginsEnableCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "enable <name>",
		Short: "Enable a plugin",
		Long:  "Enable a plugin on the running gateway.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runPluginsEnable(cmd.Context(), args[0])
		},
	}
}

func runPluginsEnable(ctx context.Context, name string) error {
	client, err := NewGatewayClient()
	if err != nil {
		return err
	}

	var resp pluginOKResponse
	if err := client.Post(ctx, pluginsBasePath+"/"+name+"/enable", nil, &resp); err != nil {
		return err
	}

	if flagJSON {
		return printJSON(resp)
	}

	fmt.Printf("Enabled plugin %s\n", name)
	return nil
}

// ---------------------------------------------------------------------------
// plugins disable
// ---------------------------------------------------------------------------

func newPluginsDisableCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "disable <name>",
		Short: "Disable a plugin",
		Long:  "Disable a plugin on the running gateway.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runPluginsDisable(cmd.Context(), args[0])
		},
	}
}

func runPluginsDisable(ctx context.Context, name string) error {
	client, err := NewGatewayClient()
	if err != nil {
		return err
	}

	var resp pluginOKResponse
	if err := client.Post(ctx, pluginsBasePath+"/"+name+"/disable", nil, &resp); err != nil {
		return err
	}

	if flagJSON {
		return printJSON(resp)
	}

	fmt.Printf("Disabled plugin %s\n", name)
	return nil
}

// ---------------------------------------------------------------------------
// plugins install
// ---------------------------------------------------------------------------

func newPluginsInstallCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "install <name-or-url>",
		Short: "Install a plugin",
		Long: `Install a plugin from a name or URL.

Examples:
  hopclaw plugins install my-plugin
  hopclaw plugins install https://github.com/user/hopclaw-plugin-example`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runPluginsInstall(cmd.Context(), args[0])
		},
	}
}

func runPluginsInstall(ctx context.Context, source string) error {
	client, err := NewGatewayClient()
	if err != nil {
		return err
	}

	body := pluginInstallRequest{
		Source: strings.TrimSpace(source),
	}

	var resp pluginOKResponse
	if err := client.Post(ctx, pluginsBasePath, body, &resp); err != nil {
		return err
	}

	if flagJSON {
		return printJSON(resp)
	}

	name := resp.Name
	if name == "" {
		name = source
	}
	fmt.Printf("Installed plugin %s\n", name)
	return nil
}

// ---------------------------------------------------------------------------
// plugins uninstall
// ---------------------------------------------------------------------------

func newPluginsUninstallCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "uninstall <name>",
		Short: "Uninstall a plugin",
		Long:  "Uninstall a plugin from the running gateway.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runPluginsUninstall(cmd.Context(), args[0])
		},
	}
}

func runPluginsUninstall(ctx context.Context, name string) error {
	client, err := NewGatewayClient()
	if err != nil {
		return err
	}

	var resp pluginOKResponse
	if err := client.Delete(ctx, pluginsBasePath+"/"+name, &resp); err != nil {
		return err
	}

	if flagJSON {
		return printJSON(resp)
	}

	fmt.Printf("Uninstalled plugin %s\n", name)
	return nil
}

// ---------------------------------------------------------------------------
// plugins validate
// ---------------------------------------------------------------------------

func newPluginsValidateCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "validate <path>",
		Short: "Validate a plugin manifest from disk",
		Long:  "Load a plugin manifest from a local directory or manifest file path and validate its public contract.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runPluginsValidate(cmd, args[0])
		},
	}
}

func runPluginsValidate(cmd *cobra.Command, rawPath string) error {
	dir, err := pluginManifestDir(rawPath)
	if err != nil {
		return err
	}

	loaded, err := pluginpkg.Load(dir)
	if err != nil {
		return err
	}

	validationErrs := sdkplugin.ValidateManifest(loaded.Manifest)
	resp := pluginValidateResponse{
		OK:   len(validationErrs) == 0,
		Name: loaded.Manifest.Name,
		Path: dir,
	}
	if len(validationErrs) > 0 {
		resp.Errors = make([]string, 0, len(validationErrs))
		for _, validationErr := range validationErrs {
			resp.Errors = append(resp.Errors, validationErr.Error())
		}
	}

	if flagJSON {
		return printJSON(resp)
	}

	out := cmd.OutOrStdout()
	if resp.OK {
		fmt.Fprintf(out, "Plugin %s is valid (%s)\n", valueOrFallback(resp.Name, "plugin"), resp.Path)
		return nil
	}

	fmt.Fprintf(out, "Plugin %s has %d validation error(s):\n", valueOrFallback(resp.Name, "plugin"), len(resp.Errors))
	for _, item := range resp.Errors {
		fmt.Fprintf(out, "  - %s\n", item)
	}
	return nil
}

func pluginManifestDir(rawPath string) (string, error) {
	path := strings.TrimSpace(rawPath)
	if path == "" {
		return "", fmt.Errorf("plugin path is required")
	}

	info, err := os.Stat(path)
	if err != nil {
		return "", err
	}
	if info.IsDir() {
		return path, nil
	}

	base := filepath.Base(path)
	switch base {
	case "hopclaw.plugin.yaml", "openclaw.plugin.json":
		return filepath.Dir(path), nil
	default:
		return "", fmt.Errorf("plugin path must be a directory or manifest file")
	}
}
