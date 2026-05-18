package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/fulcrus/hopclaw/config"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

// ---------------------------------------------------------------------------
// config command group
// ---------------------------------------------------------------------------

func newConfigCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "config",
		Short: "Manage HopClaw configuration",
	}

	cmd.AddCommand(
		newConfigGetCmd(),
		newConfigSetCmd(),
		newConfigUnsetCmd(),
		newConfigValidateCmd(),
		newConfigEditCmd(),
		newConfigPathCmd(),
		newConfigShowCmd(),
	)

	return cmd
}

// ---------------------------------------------------------------------------
// config path
// ---------------------------------------------------------------------------

func newConfigPathCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "path",
		Short: "Print the active config file path",
		RunE: func(_ *cobra.Command, _ []string) error {
			p := resolveConfigPath()
			if p == "" {
				return fmt.Errorf("no config file found")
			}
			fmt.Println(p)
			return nil
		},
	}
}

// ---------------------------------------------------------------------------
// config show
// ---------------------------------------------------------------------------

func newConfigShowCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "show",
		Short: "Display the full resolved configuration",
		RunE:  runConfigShow,
	}
}

func runConfigShow(_ *cobra.Command, _ []string) error {
	p := resolveConfigPath()
	if p == "" {
		return fmt.Errorf("no config file found")
	}

	data, err := os.ReadFile(p)
	if err != nil {
		return fmt.Errorf("read config: %w", err)
	}

	if flagJSON {
		// Parse YAML and re-serialize as JSON.
		var raw any
		if err := yaml.Unmarshal(data, &raw); err != nil {
			return fmt.Errorf("parse config: %w", err)
		}
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(raw)
	}

	os.Stdout.Write(data)
	return nil
}

// ---------------------------------------------------------------------------
// config get
// ---------------------------------------------------------------------------

func newConfigGetCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "get <key>",
		Short: "Get a configuration value by dotted path",
		Long:  "Get a configuration value. Example: hopclaw config get server.address",
		Args:  cobra.ExactArgs(1),
		RunE:  runConfigGet,
	}
}

func runConfigGet(_ *cobra.Command, args []string) error {
	p := resolveConfigPath()
	if p == "" {
		return fmt.Errorf("no config file found")
	}

	data, err := os.ReadFile(p)
	if err != nil {
		return fmt.Errorf("read config: %w", err)
	}

	expanded := os.ExpandEnv(string(data))
	var raw map[string]any
	if err := yaml.Unmarshal([]byte(expanded), &raw); err != nil {
		return fmt.Errorf("parse config: %w", err)
	}

	val, ok := getNestedValue(raw, args[0])
	if !ok {
		return fmt.Errorf("key %q not found", args[0])
	}

	if flagJSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(val)
	}

	fmt.Println(formatValue(val))
	return nil
}

// ---------------------------------------------------------------------------
// config set
// ---------------------------------------------------------------------------

func newConfigSetCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "set <key> <value>",
		Short: "Set a configuration value by dotted path",
		Long:  "Set a configuration value. Example: hopclaw config set server.address 0.0.0.0:8080",
		Args:  cobra.ExactArgs(2),
		RunE:  runConfigSet,
	}
}

func runConfigSet(_ *cobra.Command, args []string) error {
	p := resolveConfigPath()
	if p == "" {
		return fmt.Errorf("no config file found; run 'hopclaw setup' first")
	}

	data, err := os.ReadFile(p)
	if err != nil {
		return fmt.Errorf("read config: %w", err)
	}

	var raw map[string]any
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return fmt.Errorf("parse config: %w", err)
	}

	value, err := parseConfigValue(args[1])
	if err != nil {
		return fmt.Errorf("parse value: %w", err)
	}
	setNestedValue(raw, args[0], value)

	out, err := yaml.Marshal(raw)
	if err != nil {
		return fmt.Errorf("marshal config: %w", err)
	}

	if err := os.WriteFile(p, out, 0o644); err != nil {
		return fmt.Errorf("write config: %w", err)
	}

	fmt.Printf("set %s = %s\n", args[0], args[1])
	return nil
}

// ---------------------------------------------------------------------------
// config unset
// ---------------------------------------------------------------------------

func newConfigUnsetCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "unset <key>",
		Short: "Remove a configuration value by dotted path",
		Long:  "Remove a configuration value. Example: hopclaw config unset channels.slack.bot_token",
		Args:  cobra.ExactArgs(1),
		RunE:  runConfigUnset,
	}
}

func runConfigUnset(_ *cobra.Command, args []string) error {
	p := resolveConfigPath()
	if p == "" {
		return fmt.Errorf("no config file found; run 'hopclaw setup' first")
	}

	data, err := os.ReadFile(p)
	if err != nil {
		return fmt.Errorf("read config: %w", err)
	}

	var raw map[string]any
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return fmt.Errorf("parse config: %w", err)
	}

	if !unsetNestedValue(raw, args[0]) {
		return fmt.Errorf("key %q not found", args[0])
	}

	out, err := yaml.Marshal(raw)
	if err != nil {
		return fmt.Errorf("marshal config: %w", err)
	}
	if err := os.WriteFile(p, out, 0o644); err != nil {
		return fmt.Errorf("write config: %w", err)
	}

	fmt.Printf("unset %s\n", args[0])
	return nil
}

// ---------------------------------------------------------------------------
// config validate
// ---------------------------------------------------------------------------

func newConfigValidateCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "validate",
		Short: "Validate the active configuration",
		RunE:  runConfigValidate,
	}
}

func newConfigEditCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "edit",
		Short: "Open the active config in your editor",
		RunE:  runConfigEdit,
	}
}

func runConfigEdit(cmd *cobra.Command, _ []string) error {
	p := resolveConfigPath()
	if p == "" {
		return fmt.Errorf("no config file found; run 'hopclaw setup' first")
	}
	editor := strings.TrimSpace(os.Getenv("EDITOR"))
	if editor == "" {
		editor = "vi"
	}
	command := exec.Command(editor, p)
	command.Stdin = os.Stdin
	command.Stdout = cmd.OutOrStdout()
	command.Stderr = cmd.ErrOrStderr()
	if err := command.Run(); err != nil {
		return fmt.Errorf("open editor: %w", err)
	}
	return nil
}

func runConfigValidate(cmd *cobra.Command, _ []string) error {
	p := resolveConfigPath()
	if p == "" {
		return fmt.Errorf("no config file found; run 'hopclaw setup' first")
	}

	cfg, err := config.Load(p)
	if err != nil {
		return fmt.Errorf("config is invalid: %w", err)
	}

	summary := map[string]any{
		"path":               p,
		"server_address":     cfg.Server.Address,
		"default_model":      cfg.Agent.DefaultModel,
		"default_provider":   cfg.Models.DefaultProvider,
		"provider_count":     len(cfg.Models.Providers),
		"skills_auto_detect": cfg.Skills.AutoDetect,
	}
	if flagJSON {
		return printJSON(summary)
	}

	fmt.Fprintf(cmd.OutOrStdout(), "Config OK: %s\n", p)
	if cfg.Server.Address != "" {
		fmt.Fprintf(cmd.OutOrStdout(), "Gateway:   %s\n", cfg.Server.Address)
	}
	if cfg.Agent.DefaultModel != "" {
		fmt.Fprintf(cmd.OutOrStdout(), "Model:     %s\n", cfg.Agent.DefaultModel)
	}
	if cfg.Models.DefaultProvider != "" {
		fmt.Fprintf(cmd.OutOrStdout(), "Provider:  %s\n", cfg.Models.DefaultProvider)
	}
	return nil
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

func getNestedValue(m map[string]any, key string) (any, bool) {
	parts := strings.Split(key, ".")
	current := any(m)
	for _, part := range parts {
		switch v := current.(type) {
		case map[string]any:
			val, ok := v[part]
			if !ok {
				return nil, false
			}
			current = val
		default:
			return nil, false
		}
	}
	return current, true
}

func setNestedValue(m map[string]any, key string, value any) {
	parts := strings.Split(key, ".")
	current := m
	for i, part := range parts {
		if i == len(parts)-1 {
			current[part] = value
			return
		}
		next, ok := current[part].(map[string]any)
		if !ok {
			next = make(map[string]any)
			current[part] = next
		}
		current = next
	}
}

func unsetNestedValue(m map[string]any, key string) bool {
	parts := strings.Split(key, ".")
	return unsetNestedValueParts(m, parts)
}

func unsetNestedValueParts(current map[string]any, parts []string) bool {
	if len(parts) == 0 {
		return false
	}
	part := parts[0]
	if len(parts) == 1 {
		if _, ok := current[part]; !ok {
			return false
		}
		delete(current, part)
		return true
	}
	next, ok := current[part].(map[string]any)
	if !ok {
		return false
	}
	if !unsetNestedValueParts(next, parts[1:]) {
		return false
	}
	if len(next) == 0 {
		delete(current, part)
	}
	return true
}

func parseConfigValue(raw string) (any, error) {
	var value any
	if err := yaml.Unmarshal([]byte(raw), &value); err != nil {
		return nil, err
	}
	return value, nil
}

func formatValue(v any) string {
	switch val := v.(type) {
	case string:
		return val
	case nil:
		return ""
	default:
		b, _ := json.Marshal(val)
		return string(b)
	}
}
