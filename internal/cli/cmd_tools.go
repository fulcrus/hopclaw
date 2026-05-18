package cli

import (
	"context"
	"fmt"
	"strings"

	"github.com/spf13/cobra"
)

const (
	toolsDescPreviewLen = 50
	toolsCheckMark      = "[ok]"
	toolsFailMark       = "[--]"
	toolsNoSideEffect   = "-"
)

type toolDefinitionRow struct {
	Name             string         `json:"name"`
	Description      string         `json:"description"`
	InputSchema      map[string]any `json:"input_schema,omitempty"`
	OutputSchema     map[string]any `json:"output_schema,omitempty"`
	SideEffectClass  string         `json:"side_effect_class,omitempty"`
	RequiresApproval bool           `json:"requires_approval,omitempty"`
	Source           string         `json:"source,omitempty"`
	Eligible         bool           `json:"eligible,omitempty"`
}

type toolDefinitionsResponse struct {
	Items []toolDefinitionRow `json:"items"`
}

type toolCheckResponse struct {
	Items []skillCheckResult `json:"items"`
	Total int                `json:"total"`
	Ready int                `json:"ready"`
}

func newToolsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "tools",
		Short: "Inspect runtime tools visible to the gateway",
		Long:  "List, search, and inspect runtime tool definitions exposed by the running gateway.",
	}

	cmd.AddCommand(
		newToolsListCmd(),
		newToolsSearchCmd(),
		newToolsInfoCmd(),
		newToolsCheckCmd(),
	)

	return cmd
}

func newToolsListCmd() *cobra.Command {
	var sessionKey string
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List available tools",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runToolsList(cmd.Context(), sessionKey)
		},
	}
	cmd.Flags().StringVar(&sessionKey, "session-key", "", "session key for context-aware tool listing")
	return cmd
}

func runToolsList(ctx context.Context, sessionKey string) error {
	resp, err := fetchToolDefinitions(ctx, sessionKey)
	if err != nil {
		return err
	}
	if flagJSON {
		return printJSON(resp)
	}
	if len(resp.Items) == 0 {
		fmt.Println("no tools available")
		return nil
	}

	fmt.Printf("%-28s  %-12s  %s\n", "NAME", "SIDE-EFFECTS", "DESCRIPTION")
	fmt.Printf("%-28s  %-12s  %s\n", "----", "------------", "-----------")
	for _, tool := range resp.Items {
		desc := truncate(tool.Description, toolsDescPreviewLen)
		sideEffect := tool.SideEffectClass
		if sideEffect == "" {
			sideEffect = toolsNoSideEffect
		}
		fmt.Printf("%-28s  %-12s  %s\n", truncate(tool.Name, 28), sideEffect, desc)
	}

	fmt.Printf("\nTotal: %d tools\n", len(resp.Items))
	return nil
}

func newToolsSearchCmd() *cobra.Command {
	var sessionKey string
	cmd := &cobra.Command{
		Use:   "search <query>",
		Short: "Search tools by name or description",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runToolsSearch(cmd.Context(), sessionKey, args[0])
		},
	}
	cmd.Flags().StringVar(&sessionKey, "session-key", "", "session key for context-aware tool listing")
	return cmd
}

func runToolsSearch(ctx context.Context, sessionKey, query string) error {
	resp, err := fetchToolDefinitions(ctx, sessionKey)
	if err != nil {
		return err
	}
	queryLower := strings.ToLower(strings.TrimSpace(query))
	filtered := make([]toolDefinitionRow, 0)
	for _, tool := range resp.Items {
		if strings.Contains(strings.ToLower(tool.Name), queryLower) ||
			strings.Contains(strings.ToLower(tool.Description), queryLower) {
			filtered = append(filtered, tool)
		}
	}
	if flagJSON {
		return printJSON(toolDefinitionsResponse{Items: filtered})
	}
	if len(filtered) == 0 {
		fmt.Printf("no tools matching %q\n", query)
		return nil
	}

	fmt.Printf("%-28s  %-12s  %s\n", "NAME", "SIDE-EFFECTS", "DESCRIPTION")
	fmt.Printf("%-28s  %-12s  %s\n", "----", "------------", "-----------")
	for _, tool := range filtered {
		sideEffect := tool.SideEffectClass
		if sideEffect == "" {
			sideEffect = toolsNoSideEffect
		}
		fmt.Printf("%-28s  %-12s  %s\n",
			truncate(tool.Name, 28),
			sideEffect,
			truncate(tool.Description, toolsDescPreviewLen),
		)
	}

	fmt.Printf("\nMatched: %d / %d tools\n", len(filtered), len(resp.Items))
	return nil
}

func newToolsInfoCmd() *cobra.Command {
	var sessionKey string
	cmd := &cobra.Command{
		Use:   "info <name>",
		Short: "Show detailed tool information",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runToolsInfo(cmd.Context(), sessionKey, args[0])
		},
	}
	cmd.Flags().StringVar(&sessionKey, "session-key", "", "session key for context-aware tool listing")
	return cmd
}

func runToolsInfo(ctx context.Context, sessionKey, name string) error {
	resp, err := fetchToolDefinitions(ctx, sessionKey)
	if err != nil {
		return err
	}
	for _, tool := range resp.Items {
		if !strings.EqualFold(tool.Name, name) {
			continue
		}
		if flagJSON {
			return printJSON(tool)
		}
		fmt.Printf("Name:              %s\n", tool.Name)
		fmt.Printf("Description:       %s\n", tool.Description)
		fmt.Printf("Source:            %s\n", valueOrDash(tool.Source))
		fmt.Printf("Side effects:      %s\n", valueOrDash(tool.SideEffectClass))
		fmt.Printf("Requires approval: %v\n", tool.RequiresApproval)
		fmt.Printf("Eligible:          %v\n", tool.Eligible)
		if len(tool.InputSchema) > 0 {
			fmt.Println("\nInput schema:")
			if err := printJSON(tool.InputSchema); err != nil {
				return fmt.Errorf("print input schema: %w", err)
			}
		}
		if len(tool.OutputSchema) > 0 {
			fmt.Println("\nOutput schema:")
			if err := printJSON(tool.OutputSchema); err != nil {
				return fmt.Errorf("print output schema: %w", err)
			}
		}
		return nil
	}
	return fmt.Errorf("tool %q not found", name)
}

func newToolsCheckCmd() *cobra.Command {
	var sessionKey string
	cmd := &cobra.Command{
		Use:   "check [name]",
		Short: "Check tool availability",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 1 {
				return runToolsCheckOne(cmd.Context(), sessionKey, args[0])
			}
			return runToolsCheckAll(cmd.Context(), sessionKey)
		},
	}
	cmd.Flags().StringVar(&sessionKey, "session-key", "", "session key for context-aware tool listing")
	return cmd
}

type skillCheckResult struct {
	Name     string `json:"name"`
	Eligible bool   `json:"eligible"`
	Source   string `json:"source,omitempty"`
}

func runToolsCheckOne(ctx context.Context, sessionKey, name string) error {
	resp, err := fetchToolDefinitions(ctx, sessionKey)
	if err != nil {
		return err
	}
	for _, tool := range resp.Items {
		if !strings.EqualFold(tool.Name, name) {
			continue
		}
		result := skillCheckResult{
			Name:     tool.Name,
			Eligible: tool.Eligible,
			Source:   tool.Source,
		}
		if flagJSON {
			return printJSON(result)
		}
		mark := toolsCheckMark
		if !tool.Eligible {
			mark = toolsFailMark
		}
		fmt.Printf("%s %s (source: %s)\n", mark, tool.Name, valueOrDash(tool.Source))
		return nil
	}
	return fmt.Errorf("tool %q not found", name)
}

func runToolsCheckAll(ctx context.Context, sessionKey string) error {
	resp, err := fetchToolDefinitions(ctx, sessionKey)
	if err != nil {
		return err
	}
	results := make([]skillCheckResult, 0, len(resp.Items))
	readyCount := 0
	for _, tool := range resp.Items {
		if tool.Eligible {
			readyCount++
		}
		results = append(results, skillCheckResult{
			Name:     tool.Name,
			Eligible: tool.Eligible,
			Source:   tool.Source,
		})
	}
	checkResp := toolCheckResponse{
		Items: results,
		Total: len(results),
		Ready: readyCount,
	}
	if flagJSON {
		return printJSON(checkResp)
	}
	if len(results) == 0 {
		fmt.Println("no tools registered")
		return nil
	}
	for _, result := range results {
		mark := toolsCheckMark
		if !result.Eligible {
			mark = toolsFailMark
		}
		fmt.Printf("%s %-28s  (source: %s)\n", mark, truncate(result.Name, 28), valueOrDash(result.Source))
	}
	fmt.Printf("\n%d / %d tools eligible\n", readyCount, len(results))
	return nil
}

func fetchToolDefinitions(ctx context.Context, sessionKey string) (toolDefinitionsResponse, error) {
	client, err := NewGatewayClient()
	if err != nil {
		return toolDefinitionsResponse{}, err
	}

	path := "/runtime/tools"
	if sessionKey != "" {
		path += "?session_key=" + sessionKey
	}

	var resp toolDefinitionsResponse
	if err := client.Get(ctx, path, &resp); err != nil {
		return toolDefinitionsResponse{}, err
	}
	return resp, nil
}
