package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"slices"
	"strings"

	"github.com/spf13/cobra"
)

type targetView struct {
	Name           string `json:"name"`
	Kind           string `json:"kind"`
	Source         string `json:"source"`
	BaseURL        string `json:"base_url,omitempty"`
	Description    string `json:"description,omitempty"`
	AuthType       string `json:"auth_type,omitempty"`
	AuthConfigured bool   `json:"auth_configured"`
	Insecure       bool   `json:"insecure,omitempty"`
}

func newTargetCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "remote",
		Short: "Manage remote and local connections",
		Long:  "List available connections, save named remotes, and test connection health.",
	}
	cmd.AddCommand(
		newTargetListCmd(),
		newTargetGetCmd(),
		newTargetAddCmd(),
		newTargetLoginCmd(),
		newTargetLogoutCmd(),
		newTargetRemoveCmd(),
		newTargetTestCmd(),
	)
	return cmd
}

func newTargetListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List available connections",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runTargetList(cmd.Context(), cmd.OutOrStdout())
		},
	}
}

func newTargetGetCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "get <name>",
		Short: "Show one connection",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runTargetGet(cmd.Context(), cmd.OutOrStdout(), args[0])
		},
	}
}

func newTargetAddCmd() *cobra.Command {
	var (
		authType string
		token    string
		tokenEnv string
		insecure bool
	)
	cmd := &cobra.Command{
		Use:   "add [name] [url]",
		Short: "Save a named remote",
		Args:  cobra.MaximumNArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runTargetAdd(cmd.Context(), cmd.InOrStdin(), cmd.OutOrStdout(), args, authType, token, tokenEnv, insecure)
		},
	}
	cmd.Flags().StringVar(&authType, "auth", "", "auth mode for the remote (none or bearer)")
	cmd.Flags().StringVar(&token, "token", "", "bearer token to store in the keychain for this remote")
	cmd.Flags().StringVar(&tokenEnv, "token-env", "", "environment variable that provides the bearer token for this remote")
	cmd.Flags().BoolVar(&insecure, "insecure", false, "allow insecure TLS for this remote (https only)")
	return cmd
}

func newTargetRemoveCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "remove <name>",
		Short: "Remove a saved remote",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runTargetRemove(cmd.OutOrStdout(), args[0])
		},
	}
}

func newTargetLoginCmd() *cobra.Command {
	var (
		token    string
		tokenEnv string
	)
	cmd := &cobra.Command{
		Use:   "login <name>",
		Short: "Configure credentials for a saved remote",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runTargetLogin(cmd.InOrStdin(), cmd.OutOrStdout(), args[0], token, tokenEnv)
		},
	}
	cmd.Flags().StringVar(&token, "token", "", "bearer token to store in the keychain for this remote")
	cmd.Flags().StringVar(&tokenEnv, "token-env", "", "environment variable that provides the bearer token for this remote")
	return cmd
}

func newTargetLogoutCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "logout <name>",
		Short: "Clear saved credentials for a remote",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runTargetLogout(cmd.OutOrStdout(), args[0])
		},
	}
}

func newTargetTestCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "test <name-or-url>",
		Short: "Test connection health",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runTargetTest(cmd.Context(), cmd.OutOrStdout(), args[0])
		},
	}
}

func runTargetList(ctx context.Context, out io.Writer) error {
	views, err := collectTargetViews(ctx)
	if err != nil {
		return err
	}
	if flagJSON {
		return writeTargetJSON(out, views)
	}
	if len(views) == 0 {
		fmt.Fprintln(out, "no remotes found")
		return nil
	}
	fmt.Fprintf(out, "%-16s  %-11s  %-10s  %-36s  %s\n", "NAME", "KIND", "SOURCE", "ENDPOINT", "DETAILS")
	fmt.Fprintf(out, "%-16s  %-11s  %-10s  %-36s  %s\n", "----", "----", "------", "--------", "-------")
	for _, item := range views {
		endpoint := item.BaseURL
		if endpoint == "" {
			endpoint = "-"
		}
		details := strings.TrimSpace(item.Description)
		if details == "" {
			details = "-"
		}
		fmt.Fprintf(out, "%-16s  %-11s  %-10s  %-36s  %s\n",
			item.Name,
			item.Kind,
			item.Source,
			truncate(endpoint, 36),
			details,
		)
	}
	return nil
}

func runTargetGet(ctx context.Context, out io.Writer, name string) error {
	view, err := findTargetView(ctx, name)
	if err != nil {
		return err
	}
	if flagJSON {
		return writeTargetJSON(out, view)
	}
	fmt.Fprintf(out, "Name:           %s\n", view.Name)
	fmt.Fprintf(out, "Kind:           %s\n", view.Kind)
	fmt.Fprintf(out, "Source:         %s\n", view.Source)
	if view.BaseURL != "" {
		fmt.Fprintf(out, "Base URL:       %s\n", view.BaseURL)
	}
	if view.AuthType != "" {
		fmt.Fprintf(out, "Auth Type:      %s\n", view.AuthType)
		fmt.Fprintf(out, "Auth Config:    %t\n", view.AuthConfigured)
	}
	if view.Insecure {
		fmt.Fprintf(out, "TLS Verify:     insecure\n")
	}
	if strings.TrimSpace(view.Description) != "" {
		fmt.Fprintf(out, "Description:    %s\n", view.Description)
	}
	return nil
}

func runTargetAdd(ctx context.Context, in io.Reader, out io.Writer, args []string, authType, token, tokenEnv string, insecure bool) error {
	var (
		name   string
		rawURL string
	)
	if len(args) > 0 {
		name = strings.TrimSpace(args[0])
	}
	if len(args) > 1 {
		rawURL = strings.TrimSpace(args[1])
	}
	if name != "" {
		if err := validateManagedTargetName(name); err != nil {
			return err
		}
	}
	if token != "" && tokenEnv != "" {
		return fmt.Errorf("--token and --token-env cannot be used together")
	}
	prompt := newTargetPrompter(in, out)
	if strings.TrimSpace(name) == "" {
		if !prompt.CanPrompt() {
			return fmt.Errorf("remote name is required in non-interactive mode; pass `hopclaw remote add <name> <url>`")
		}
		value, err := prompt.PromptText("Remote name", "", true)
		if err != nil {
			return err
		}
		name = value
		if err := validateManagedTargetName(name); err != nil {
			return err
		}
	}
	if strings.TrimSpace(rawURL) == "" {
		if !prompt.CanPrompt() {
			return fmt.Errorf("remote URL is required in non-interactive mode; pass `hopclaw remote add <name> <url>`")
		}
		value, err := prompt.PromptText("Remote URL", "", true)
		if err != nil {
			return err
		}
		rawURL = value
	}
	if _, found, err := getSavedTargetProfile(name); err != nil {
		return err
	} else if found {
		return fmt.Errorf("remote %q already exists", name)
	}
	if conflictsWithLiveTarget(ctx, name) {
		return fmt.Errorf("remote name %q conflicts with a live local runtime", name)
	}

	authType = strings.ToLower(strings.TrimSpace(authType))
	if authType == "" {
		switch {
		case strings.TrimSpace(token) != "" || strings.TrimSpace(tokenEnv) != "":
			authType = targetAuthTypeBearer
		case len(args) < 2:
			if !prompt.CanPrompt() {
				return fmt.Errorf("auth mode is required in non-interactive mode; pass --auth {none|bearer}")
			}
			value, err := prompt.PromptChoice("Auth mode", targetAuthTypeNone, []string{targetAuthTypeNone, targetAuthTypeBearer})
			if err != nil {
				return err
			}
			authType = value
		default:
			authType = targetAuthTypeNone
		}
	}
	if authType == targetAuthTypeNone && (strings.TrimSpace(token) != "" || strings.TrimSpace(tokenEnv) != "") {
		return fmt.Errorf("auth mode %q does not accept bearer token flags", authType)
	}
	if authType == targetAuthTypeBearer && strings.TrimSpace(token) == "" && strings.TrimSpace(tokenEnv) == "" {
		if prompt.CanPrompt() {
			value, err := prompt.PromptSecret("Bearer token (leave blank to configure later)", false)
			if err == nil {
				token = value
			} else if err != io.EOF {
				return err
			}
		}
	}

	authRef := ""
	switch authType {
	case targetAuthTypeNone:
	case targetAuthTypeBearer:
		switch {
		case strings.TrimSpace(token) != "":
			ref, err := saveManagedTargetSecret(name, strings.TrimSpace(token))
			if err != nil {
				return fmt.Errorf("save remote token: %w", err)
			}
			authRef = ref
		case strings.TrimSpace(tokenEnv) != "":
			authRef = "env:" + strings.TrimSpace(tokenEnv)
		}
	default:
		return fmt.Errorf("unsupported auth mode %q", authType)
	}

	profile, err := normalizeSavedTargetProfile(savedTargetProfile{
		Name:     name,
		Kind:     targetKindRemote,
		BaseURL:  rawURL,
		AuthType: authType,
		AuthRef:  authRef,
		Insecure: insecure,
	})
	if err != nil {
		if authRef != "" {
			_ = deleteManagedTargetSecret(authRef)
		}
		return err
	}
	if err := addSavedTargetProfile(profile); err != nil {
		if authRef != "" {
			_ = deleteManagedTargetSecret(authRef)
		}
		return err
	}

	if flagJSON {
		return writeTargetJSON(out, profile)
	}
	fmt.Fprintf(out, "Added remote %s -> %s\n", profile.Name, profile.BaseURL)
	if profile.AuthType == targetAuthTypeBearer && strings.TrimSpace(profile.AuthRef) == "" {
		fmt.Fprintf(out, "Credentials not configured yet. Run `hopclaw remote login %s` when ready.\n", profile.Name)
	}
	return nil
}

func runTargetRemove(out io.Writer, name string) error {
	profile, found, err := removeSavedTargetProfile(name)
	if err != nil {
		return err
	}
	if !found {
		return fmt.Errorf("remote %q not found", strings.TrimSpace(name))
	}
	if err := deleteManagedTargetSecret(profile.AuthRef); err != nil {
		return fmt.Errorf("delete remote secret: %w", err)
	}
	if flagJSON {
		return writeTargetJSON(out, map[string]any{"deleted": profile.Name})
	}
	fmt.Fprintf(out, "Removed remote %s\n", profile.Name)
	return nil
}

func runTargetLogin(in io.Reader, out io.Writer, name, token, tokenEnv string) error {
	if strings.TrimSpace(token) == "" && strings.TrimSpace(tokenEnv) == "" {
		prompt := newTargetPrompter(in, out)
		if !prompt.CanPrompt() {
			return fmt.Errorf("credentials are required in non-interactive mode; pass --token or --token-env")
		}
		value, err := prompt.PromptSecret("Bearer token", true)
		if err != nil {
			return err
		}
		token = value
	}
	updated, err := updateSavedTargetCredentials(name, targetAuthUpdate{
		Token:    token,
		TokenEnv: tokenEnv,
	})
	if err != nil {
		return err
	}
	if flagJSON {
		return writeTargetJSON(out, updated)
	}
	fmt.Fprintf(out, "Updated credentials for remote %s\n", updated.Name)
	return nil
}

func runTargetLogout(out io.Writer, name string) error {
	updated, err := clearSavedTargetCredentials(name)
	if err != nil {
		return err
	}
	if flagJSON {
		return writeTargetJSON(out, updated)
	}
	fmt.Fprintf(out, "Cleared credentials for remote %s\n", updated.Name)
	return nil
}

func runTargetTest(ctx context.Context, out io.Writer, name string) error {
	target, err := resolveNamedInteractiveTarget(ctx, name, interactiveTarget{})
	if err != nil {
		return err
	}
	if isPrivateLocalInteractiveTarget(target) {
		if flagJSON {
			return writeTargetJSON(out, map[string]any{"runtime": target.label(), "ok": true, "kind": string(interactiveTargetLocal)})
		}
		fmt.Fprintf(out, "Local connection %s is available.\n", target.label())
		return nil
	}
	status, err := checkTargetHealth(ctx, target)
	if err != nil {
		return err
	}
	if flagJSON {
		return writeTargetJSON(out, map[string]any{"runtime": target.label(), "ok": true, "kind": string(target.Kind), "status": status})
	}
	switch target.Kind {
	case interactiveTargetLocal:
		fmt.Fprintf(out, "Local connection %s is healthy (HTTP %d)\n", target.label(), status)
	default:
		fmt.Fprintf(out, "Remote %s is healthy (HTTP %d)\n", target.label(), status)
	}
	return nil
}

func collectTargetViews(ctx context.Context) ([]targetView, error) {
	locals, err := selectableLocalTargets(ctx)
	if err != nil {
		return nil, err
	}
	profiles, err := loadSavedTargetProfiles()
	if err != nil {
		return nil, err
	}
	views := make([]targetView, 0, len(locals)+len(profiles)+1)
	for _, item := range locals {
		views = append(views, targetView{
			Name:        item.Name,
			Kind:        string(item.Kind),
			Source:      "live",
			BaseURL:     item.BaseURL,
			Description: item.Description,
		})
	}
	for _, profile := range profiles {
		details := profile.AuthType
		if strings.EqualFold(profile.AuthType, targetAuthTypeBearer) && strings.TrimSpace(profile.AuthRef) == "" {
			if details == "" {
				details = targetAuthTypeBearer
			}
			details += ", login required"
		}
		if profile.Insecure {
			if details != "" && details != targetAuthTypeNone {
				details += ", "
			}
			details += "insecure"
		}
		if strings.TrimSpace(details) == "" || details == targetAuthTypeNone {
			details = "-"
		}
		views = append(views, targetView{
			Name:           profile.Name,
			Kind:           targetKindRemote,
			Source:         "saved",
			BaseURL:        profile.BaseURL,
			Description:    details,
			AuthType:       profile.AuthType,
			AuthConfigured: strings.TrimSpace(profile.AuthRef) != "",
			Insecure:       profile.Insecure,
		})
	}
	views = append(views, targetView{
		Name:        localTargetName,
		Kind:        string(interactiveTargetLocal),
		Source:      "builtin",
		Description: "private local connection",
	})
	sortTargetViews(views)
	return views, nil
}

func findTargetView(ctx context.Context, name string) (targetView, error) {
	name = strings.TrimSpace(name)
	if isBuiltinLocalTargetName(name) {
		name = localTargetName
	}
	views, err := collectTargetViews(ctx)
	if err != nil {
		return targetView{}, err
	}
	var matches []targetView
	for _, item := range views {
		if strings.EqualFold(item.Name, name) {
			matches = append(matches, item)
		}
	}
	switch len(matches) {
	case 0:
		return targetView{}, fmt.Errorf("connection %q not found", name)
	case 1:
		return matches[0], nil
	default:
		return targetView{}, fmt.Errorf("connection %q is ambiguous across multiple sources", name)
	}
}

func conflictsWithLiveTarget(ctx context.Context, name string) bool {
	targets, err := selectableLocalTargets(ctx)
	if err != nil {
		return false
	}
	for _, item := range targets {
		if strings.EqualFold(item.Name, name) {
			return true
		}
	}
	return false
}

func sortTargetViews(views []targetView) {
	slices.SortFunc(views, func(a, b targetView) int {
		priority := func(item targetView) int {
			switch item.Kind {
			case string(interactiveTargetLocal):
				return 0
			case targetKindRemote:
				return 1
			default:
				return 2
			}
		}
		if pa, pb := priority(a), priority(b); pa != pb {
			return pa - pb
		}
		return strings.Compare(a.Name, b.Name)
	})
}

func checkTargetHealth(ctx context.Context, target interactiveTarget) (int, error) {
	authToken, err := resolveInteractiveTargetAuthToken(target)
	if err != nil {
		return 0, err
	}
	client, _, err := newGatewayClientWithOptions(target.BaseURL, authToken, target.Insecure)
	if err != nil {
		return 0, err
	}
	checkCtx, cancel := context.WithTimeout(ctx, interactiveHealthTimeout)
	defer cancel()
	oldTimeout := client.HTTP.Timeout
	client.HTTP.Timeout = interactiveHealthTimeout
	defer func() { client.HTTP.Timeout = oldTimeout }()

	_, status, err := client.GetRawWithStatus(checkCtx, "/healthz")
	if err != nil {
		return 0, fmt.Errorf("connection %q is not reachable: %w", target.label(), err)
	}
	if status >= 400 {
		return 0, fmt.Errorf("connection %q health check failed (HTTP %d)", target.label(), status)
	}
	return status, nil
}

func writeTargetJSON(out io.Writer, value any) error {
	enc := json.NewEncoder(out)
	enc.SetIndent("", "  ")
	return enc.Encode(value)
}
