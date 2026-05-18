package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	hookpkg "github.com/fulcrus/hopclaw/hooks"
	"github.com/spf13/cobra"
)

const hooksBasePath = "/operator/hooks"

type hooksListResponse struct {
	Items []hookpkg.Hook `json:"items"`
	Count int            `json:"count"`
}

type hookResultsResponse struct {
	Items []hookpkg.HookResult `json:"items"`
	Count int                  `json:"count"`
}

type hookEventsResponse struct {
	Items []hookpkg.EventSpec `json:"items"`
	Count int                 `json:"count"`
}

type hookFireRequest struct {
	Trigger hookpkg.TriggerEvent `json:"trigger,omitempty"`
	Phase   hookpkg.HookPhase    `json:"phase,omitempty"`
	Payload map[string]any       `json:"payload,omitempty"`
}

type hookFireResponse struct {
	Result hookpkg.HookResult `json:"result"`
}

func newHooksCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "hooks",
		Short: "Inspect and debug runtime hooks",
		Long:  "List hook registrations, inspect recent executions, and manually fire or replay hooks for debugging.",
	}
	cmd.AddCommand(
		newHooksListCmd(),
		newHooksEventsCmd(),
		newHooksInspectCmd(),
		newHooksResultsCmd(),
		newHooksErrorsCmd(),
		newHooksFireCmd(),
		newHooksReplayCmd(),
		newHooksDeleteCmd(),
	)
	return cmd
}

func newHooksEventsCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "events",
		Short: "List supported hook events",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runHooksEvents(cmd.Context())
		},
	}
}

func runHooksEvents(ctx context.Context) error {
	client, err := NewGatewayClient()
	if err != nil {
		return err
	}
	var resp hookEventsResponse
	if err := client.Get(ctx, hooksBasePath+"/events", &resp); err != nil {
		return err
	}
	if flagJSON {
		return printJSON(resp)
	}
	if len(resp.Items) == 0 {
		fmt.Println("no hook events found")
		return nil
	}
	fmt.Printf("%-22s  %-10s  %-12s  %-6s  %s\n", "TRIGGER", "CATEGORY", "PHASES", "BLOCK", "DESCRIPTION")
	fmt.Printf("%-22s  %-10s  %-12s  %-6s  %s\n", "-------", "--------", "------", "-----", "-----------")
	for _, item := range resp.Items {
		phases := make([]string, 0, len(item.AllowedPhases))
		for _, phase := range item.AllowedPhases {
			phases = append(phases, string(phase))
		}
		fmt.Printf("%-22s  %-10s  %-12s  %-6v  %s\n",
			truncate(string(item.Trigger), 22),
			truncate(string(item.Category), 10),
			truncate(strings.Join(phases, ","), 12),
			item.CanBlock,
			truncate(item.Description, 72),
		)
	}
	return nil
}

func newHooksListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List registered hooks",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runHooksList(cmd.Context())
		},
	}
}

func runHooksList(ctx context.Context) error {
	client, err := NewGatewayClient()
	if err != nil {
		return err
	}
	var resp hooksListResponse
	if err := client.Get(ctx, hooksBasePath, &resp); err != nil {
		return err
	}
	if flagJSON {
		return printJSON(resp)
	}
	if len(resp.Items) == 0 {
		fmt.Println("no hooks found")
		return nil
	}
	fmt.Printf("%-20s  %-18s  %-8s  %-8s  %s\n", "ID", "TRIGGER", "PHASE", "KIND", "NAME")
	fmt.Printf("%-20s  %-18s  %-8s  %-8s  %s\n", "--", "-------", "-----", "----", "----")
	for _, item := range resp.Items {
		fmt.Printf("%-20s  %-18s  %-8s  %-8s  %s\n",
			truncate(item.ID, 20),
			truncate(string(item.Trigger), 18),
			truncate(string(item.EffectivePhase()), 8),
			truncate(string(item.Kind), 8),
			truncate(item.Name, 40),
		)
	}
	fmt.Printf("\nTotal: %d hooks\n", resp.Count)
	return nil
}

func newHooksInspectCmd() *cobra.Command {
	return &cobra.Command{
		Use:     "inspect <id>",
		Aliases: []string{"get"},
		Short:   "Inspect one hook",
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runHooksInspect(cmd.Context(), args[0])
		},
	}
}

func runHooksInspect(ctx context.Context, id string) error {
	client, err := NewGatewayClient()
	if err != nil {
		return err
	}
	var list hooksListResponse
	if err := client.Get(ctx, hooksBasePath, &list); err != nil {
		return err
	}
	var item *hookpkg.Hook
	for i := range list.Items {
		if list.Items[i].ID == strings.TrimSpace(id) {
			item = &list.Items[i]
			break
		}
	}
	if item == nil {
		return fmt.Errorf("hook %q not found", id)
	}
	if flagJSON {
		return printJSON(item)
	}
	fmt.Printf("ID:        %s\n", item.ID)
	fmt.Printf("Name:      %s\n", item.Name)
	fmt.Printf("Trigger:   %s\n", item.Trigger)
	fmt.Printf("Phase:     %s\n", item.EffectivePhase())
	fmt.Printf("Kind:      %s\n", item.Kind)
	fmt.Printf("Enabled:   %v\n", item.Enabled)
	fmt.Printf("Priority:  %d\n", item.EffectivePriority())
	if spec, ok := hookpkg.LookupEventSpec(item.Trigger); ok {
		fmt.Printf("Category:  %s\n", spec.Category)
		fmt.Printf("Blocking:  %v\n", spec.CanBlock)
		if spec.Description != "" {
			fmt.Printf("About:     %s\n", spec.Description)
		}
	}
	if item.URL != "" {
		fmt.Printf("URL:       %s\n", item.URL)
	}
	if item.Command != "" {
		fmt.Printf("Command:   %s\n", item.Command)
	}
	if item.Filter != "" {
		fmt.Printf("Filter:    %s\n", item.Filter)
	}
	if item.Timeout > 0 {
		fmt.Printf("Timeout:   %ds\n", item.Timeout)
	}
	if item.RetryCount > 0 {
		fmt.Printf("Retries:   %d\n", item.RetryCount)
	}
	return nil
}

func newHooksResultsCmd() *cobra.Command {
	return &cobra.Command{
		Use:     "recent <id>",
		Aliases: []string{"results"},
		Short:   "Show recent hook executions",
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runHooksResults(cmd.Context(), args[0])
		},
	}
}

func runHooksResults(ctx context.Context, id string) error {
	client, err := NewGatewayClient()
	if err != nil {
		return err
	}
	var resp hookResultsResponse
	if err := client.Get(ctx, hooksBasePath+"/"+strings.TrimSpace(id)+"/results", &resp); err != nil {
		return err
	}
	if flagJSON {
		return printJSON(resp)
	}
	if len(resp.Items) == 0 {
		fmt.Println("no hook executions found")
		return nil
	}
	fmt.Printf("%-24s  %-8s  %-10s  %s\n", "TIME", "STATUS", "TRIGGER", "SUMMARY")
	fmt.Printf("%-24s  %-8s  %-10s  %s\n", "----", "------", "-------", "-------")
	for _, item := range resp.Items {
		summary := item.Summary
		if summary == "" {
			summary = item.Error
		}
		if summary == "" {
			summary = "-"
		}
		fmt.Printf("%-24s  %-8s  %-10s  %s\n",
			formatTime(item.ExecutedAt),
			item.Status,
			truncate(string(item.Trigger), 10),
			truncate(summary, 80),
		)
	}
	return nil
}

func newHooksFireCmd() *cobra.Command {
	var trigger string
	var phase string
	var payload string
	cmd := &cobra.Command{
		Use:     "test-fire <id>",
		Aliases: []string{"fire"},
		Short:   "Fire a hook manually",
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runHooksFire(cmd.Context(), args[0], trigger, phase, payload)
		},
	}
	cmd.Flags().StringVar(&trigger, "trigger", "", "override trigger name")
	cmd.Flags().StringVar(&phase, "phase", "", "override phase: pre, post, error")
	cmd.Flags().StringVar(&payload, "payload", "", "JSON object payload, e.g. '{\"run_id\":\"run_123\"}'")
	return cmd
}

func runHooksFire(ctx context.Context, id, trigger, phase, payload string) error {
	client, err := NewGatewayClient()
	if err != nil {
		return err
	}
	req := hookFireRequest{
		Trigger: hookpkg.TriggerEvent(strings.TrimSpace(trigger)),
		Phase:   hookpkg.HookPhase(strings.TrimSpace(phase)),
	}
	if strings.TrimSpace(payload) != "" {
		if err := json.Unmarshal([]byte(payload), &req.Payload); err != nil {
			return fmt.Errorf("parse payload: %w", err)
		}
	}
	var resp hookFireResponse
	if err := client.Post(ctx, hooksBasePath+"/"+strings.TrimSpace(id)+"/fire", req, &resp); err != nil {
		return err
	}
	if flagJSON {
		return printJSON(resp)
	}
	fmt.Printf("Hook fired: %s\n", id)
	fmt.Printf("Status:     %s\n", resp.Result.Status)
	if resp.Result.Summary != "" {
		fmt.Printf("Summary:    %s\n", resp.Result.Summary)
	}
	if resp.Result.Error != "" {
		fmt.Printf("Error:      %s\n", resp.Result.Error)
	}
	return nil
}

func newHooksReplayCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "replay <id>",
		Short: "Replay the latest hook payload",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runHooksReplay(cmd.Context(), args[0])
		},
	}
}

func runHooksReplay(ctx context.Context, id string) error {
	client, err := NewGatewayClient()
	if err != nil {
		return err
	}
	var resp hookFireResponse
	if err := client.Post(ctx, hooksBasePath+"/"+strings.TrimSpace(id)+"/replay", nil, &resp); err != nil {
		return err
	}
	if flagJSON {
		return printJSON(resp)
	}
	fmt.Printf("Hook replayed: %s\n", id)
	fmt.Printf("Status:        %s\n", resp.Result.Status)
	if resp.Result.Summary != "" {
		fmt.Printf("Summary:       %s\n", resp.Result.Summary)
	}
	if resp.Result.Error != "" {
		fmt.Printf("Error:         %s\n", resp.Result.Error)
	}
	return nil
}

func newHooksErrorsCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "errors <id>",
		Short: "Show clustered recent hook errors",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runHooksErrors(cmd.Context(), args[0])
		},
	}
}

func runHooksErrors(ctx context.Context, id string) error {
	client, err := NewGatewayClient()
	if err != nil {
		return err
	}
	var resp automationCLIItemDetailResponse
	if err := client.Get(ctx, automationPath+"/hook/"+strings.TrimSpace(id), &resp); err != nil {
		return err
	}
	if flagJSON {
		return printJSON(resp.ErrorSignatures)
	}
	if len(resp.ErrorSignatures) == 0 {
		fmt.Println("no clustered hook errors found")
		return nil
	}
	fmt.Printf("%-8s  %-24s  %s\n", "COUNT", "LAST SEEN", "SIGNATURE")
	fmt.Printf("%-8s  %-24s  %s\n", "-----", "---------", "---------")
	for _, item := range resp.ErrorSignatures {
		fmt.Printf("%-8d  %-24s  %s\n", item.Count, formatTime(item.LastOccurredAt), truncate(item.Signature, 80))
	}
	return nil
}

func newHooksDeleteCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "delete <id>",
		Short: "Delete a hook",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runHooksDelete(cmd.Context(), args[0])
		},
	}
}

func runHooksDelete(ctx context.Context, id string) error {
	client, err := NewGatewayClient()
	if err != nil {
		return err
	}
	var resp map[string]any
	if err := client.Delete(ctx, hooksBasePath+"/"+strings.TrimSpace(id), &resp); err != nil {
		return err
	}
	if flagJSON {
		return printJSON(resp)
	}
	fmt.Printf("Deleted hook %s\n", id)
	return nil
}
