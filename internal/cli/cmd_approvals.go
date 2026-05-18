package cli

import (
	"context"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/fulcrus/hopclaw/approval"
	"github.com/fulcrus/hopclaw/controlplane"
	replpkg "github.com/fulcrus/hopclaw/internal/cli/repl"
	"github.com/fulcrus/hopclaw/internal/support/normalize"
	"github.com/spf13/cobra"
)

// ---------------------------------------------------------------------------
// Constants
// ---------------------------------------------------------------------------

const (
	approvalsBasePath = "/runtime/approvals"
)

// ---------------------------------------------------------------------------
// Response types (mirror the API JSON shapes)
// ---------------------------------------------------------------------------

type approvalTicket struct {
	ID                   string                `json:"id"`
	RunID                string                `json:"run_id"`
	SessionID            string                `json:"session_id"`
	Kind                 string                `json:"kind,omitempty"`
	Status               string                `json:"status"`
	ToolCalls            []approvalTool        `json:"tool_calls,omitempty"`
	Reasons              []string              `json:"reasons,omitempty"`
	Note                 string                `json:"note,omitempty"`
	ResolvedBy           string                `json:"resolved_by,omitempty"`
	CreatedAt            time.Time             `json:"created_at"`
	ResolvedAt           time.Time             `json:"resolved_at,omitempty"`
	External             []approvalExternalRef `json:"external,omitempty"`
	Governance           *approvalGovernance   `json:"governance,omitempty"`
	ResourceScopeSummary string                `json:"resource_scope_summary,omitempty"`
}

type approvalTool struct {
	ID            string                 `json:"id"`
	Name          string                 `json:"name"`
	Input         map[string]any         `json:"input,omitempty"`
	ResourceScope approval.ResourceScope `json:"resource_scope,omitempty"`
}

type approvalGovernance struct {
	Scope                     controlplane.ScopeRef            `json:"scope,omitempty"`
	EffectiveConfigSnapshotID string                           `json:"effective_config_snapshot_id,omitempty"`
	Policy                    *controlplane.GovernanceDecision `json:"policy,omitempty"`
	Approval                  *approvalStatusView              `json:"approval,omitempty"`
	ToolNames                 []string                         `json:"tool_names,omitempty"`
	Summary                   string                           `json:"summary,omitempty"`
}

type approvalStatusView struct {
	ID     string `json:"id,omitempty"`
	Kind   string `json:"kind,omitempty"`
	Status string `json:"status,omitempty"`
}

type approvalExternalRef struct {
	Provider   string    `json:"provider,omitempty"`
	ExternalID string    `json:"external_id,omitempty"`
	URL        string    `json:"url,omitempty"`
	Status     string    `json:"status,omitempty"`
	SyncedAt   time.Time `json:"synced_at,omitempty"`
}

type approvalListResponse struct {
	Items []approvalTicket `json:"items"`
}

type approvalResolveRequest struct {
	Status     string `json:"status"`
	ResolvedBy string `json:"resolved_by"`
	Note       string `json:"note,omitempty"`
}

// ---------------------------------------------------------------------------
// Parent command
// ---------------------------------------------------------------------------

func newApprovalsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "approvals",
		Short: "Manage approval requests",
		Long:  "List, inspect, approve, and deny pending approval requests on the running gateway.",
	}

	cmd.AddCommand(
		newApprovalsListCmd(),
		newApprovalsGetCmd(),
		newApprovalsApproveCmd(),
		newApprovalsDenyCmd(),
	)

	return cmd
}

// ---------------------------------------------------------------------------
// approvals list
// ---------------------------------------------------------------------------

func newApprovalsListCmd() *cobra.Command {
	var status string

	cmd := &cobra.Command{
		Use:   "list",
		Short: "List approval requests",
		Long:  "List all approval requests, optionally filtered by status.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runApprovalsList(cmd.Context(), status)
		},
	}

	cmd.Flags().StringVar(&status, "status", "", "filter by status (pending, approved, denied, cancelled)")

	return cmd
}

func runApprovalsList(ctx context.Context, status string) error {
	client, err := NewGatewayClient()
	if err != nil {
		return err
	}

	items, err := loadApprovals(ctx, client, status, 0)
	if err != nil {
		return err
	}

	if flagJSON {
		return printJSON(struct {
			Items any `json:"items"`
			Count int `json:"count"`
		}{Items: items, Count: len(items)})
	}

	if len(items) == 0 {
		fmt.Println("no approval requests found")
		return nil
	}

	fmt.Printf("%-14s  %-12s  %-12s  %-10s  %-18s  %-22s  %s\n",
		"ID", "TASK", "CONV", "STATUS", "TOOL", "POLICY", "CREATED")
	fmt.Printf("%-14s  %-12s  %-12s  %-10s  %-18s  %-22s  %s\n",
		"---", "----", "----", "------", "----", "------", "-------")
	for _, t := range items {
		toolName := "-"
		if len(t.ToolCalls) > 0 {
			toolName = t.ToolCalls[0].Name
		} else if t.Governance != nil && len(t.Governance.ToolNames) > 0 {
			toolName = t.Governance.ToolNames[0]
		}
		policySummary := "-"
		if t.Governance != nil && t.Governance.Policy != nil && strings.TrimSpace(t.Governance.Policy.Summary) != "" {
			policySummary = t.Governance.Policy.Summary
		}
		fmt.Printf("%-14s  %-12s  %-12s  %-10s  %-18s  %-22s  %s\n",
			t.ID,
			truncate(t.RunID, 12),
			truncate(t.SessionID, 12),
			t.Status,
			truncate(toolName, 18),
			truncate(policySummary, 22),
			formatTime(t.CreatedAt),
		)
	}
	return nil
}

func fetchApprovalSummaries(ctx context.Context, client *GatewayClient, status string, limit int) ([]replpkg.ApprovalSummary, error) {
	if client == nil {
		return nil, fmt.Errorf("gateway client is required")
	}

	values := url.Values{}
	if trimmed := strings.TrimSpace(status); trimmed != "" {
		values.Set("status", trimmed)
	}
	if limit > 0 {
		values.Set("limit", fmt.Sprintf("%d", limit))
	}

	path := approvalsBasePath
	if encoded := values.Encode(); encoded != "" {
		path += "?" + encoded
	}

	var resp approvalListResponse
	if err := client.Get(ctx, path, &resp); err != nil {
		return nil, err
	}

	items := make([]replpkg.ApprovalSummary, 0, len(resp.Items))
	for _, ticket := range resp.Items {
		items = append(items, approvalSummaryFromTicket(ticket))
	}
	return items, nil
}

func resolveApprovalSummary(ctx context.Context, client *GatewayClient, id string, approved bool) (*replpkg.ApprovalSummary, error) {
	if client == nil {
		return nil, fmt.Errorf("gateway client is required")
	}

	status := "denied"
	if approved {
		status = "approved"
	}

	var ticket approvalTicket
	if err := client.Post(ctx, approvalsBasePath+"/"+strings.TrimSpace(id)+"/resolve", approvalResolveRequest{
		Status:     status,
		ResolvedBy: "interactive-repl",
	}, &ticket); err != nil {
		return nil, err
	}

	summary := approvalSummaryFromTicket(ticket)
	return &summary, nil
}

func approvalSummaryFromTicket(ticket approvalTicket) replpkg.ApprovalSummary {
	toolName := "-"
	if len(ticket.ToolCalls) > 0 && strings.TrimSpace(ticket.ToolCalls[0].Name) != "" {
		toolName = strings.TrimSpace(ticket.ToolCalls[0].Name)
	} else if ticket.Governance != nil && len(ticket.Governance.ToolNames) > 0 && strings.TrimSpace(ticket.Governance.ToolNames[0]) != "" {
		toolName = strings.TrimSpace(ticket.Governance.ToolNames[0])
	}

	policySummary := "-"
	if ticket.Governance != nil && ticket.Governance.Policy != nil && strings.TrimSpace(ticket.Governance.Policy.Summary) != "" {
		policySummary = strings.TrimSpace(ticket.Governance.Policy.Summary)
	}

	return replpkg.ApprovalSummary{
		ID:            strings.TrimSpace(ticket.ID),
		RunID:         strings.TrimSpace(ticket.RunID),
		SessionID:     strings.TrimSpace(ticket.SessionID),
		Status:        strings.TrimSpace(ticket.Status),
		ToolName:      toolName,
		PolicySummary: policySummary,
		CreatedAt:     formatTime(ticket.CreatedAt),
	}
}

// ---------------------------------------------------------------------------
// approvals get
// ---------------------------------------------------------------------------

func newApprovalsGetCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "get <id>",
		Short: "Get approval request details",
		Long:  "Get full details for a single approval request.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runApprovalsGet(cmd.Context(), args[0])
		},
	}
}

func runApprovalsGet(ctx context.Context, id string) error {
	client, err := NewGatewayClient()
	if err != nil {
		return err
	}

	ticket, err := loadApprovalView(ctx, client, id)
	if err != nil {
		return err
	}

	if flagJSON {
		return printJSON(ticket)
	}

	fmt.Printf("ID:         %s\n", ticket.ID)
	fmt.Printf("Run:        %s\n", ticket.RunID)
	fmt.Printf("Session:    %s\n", ticket.SessionID)
	fmt.Printf("Kind:       %s\n", ticket.Kind)
	fmt.Printf("Status:     %s\n", ticket.Status)
	fmt.Printf("Created:    %s\n", formatTime(ticket.CreatedAt))

	if !ticket.ResolvedAt.IsZero() {
		fmt.Printf("Resolved:   %s\n", formatTime(ticket.ResolvedAt))
	}
	if ticket.ResolvedBy != "" {
		fmt.Printf("ResolvedBy: %s\n", ticket.ResolvedBy)
	}
	if ticket.Note != "" {
		fmt.Printf("Note:       %s\n", ticket.Note)
	}
	if len(ticket.External) > 0 {
		for _, ref := range ticket.External {
			fmt.Printf("External:   %s id=%s status=%s\n", normalize.FirstNonEmpty(ref.Provider, "-"), normalize.FirstNonEmpty(ref.ExternalID, "-"), normalize.FirstNonEmpty(ref.Status, "-"))
			if strings.TrimSpace(ref.URL) != "" {
				fmt.Printf("ExtURL:     %s\n", ref.URL)
			}
			if !ref.SyncedAt.IsZero() {
				fmt.Printf("ExtSync:    %s\n", formatTime(ref.SyncedAt))
			}
		}
	}

	if len(ticket.Reasons) > 0 {
		fmt.Printf("Reasons:    %s\n", strings.Join(ticket.Reasons, ", "))
	}
	if ticket.Governance != nil {
		if ticket.Governance.Policy != nil {
			fmt.Printf("Policy:     %s\n", normalize.FirstNonEmpty(strings.TrimSpace(ticket.Governance.Policy.Summary), string(ticket.Governance.Policy.Action)))
			if strings.TrimSpace(ticket.Governance.Policy.PolicySource) != "" {
				fmt.Printf("PolicySrc:  %s\n", ticket.Governance.Policy.PolicySource)
			}
			if len(ticket.Governance.Policy.ReasonCodes) > 0 {
				fmt.Printf("PolicyCode: %s\n", strings.Join(ticket.Governance.Policy.ReasonCodes, ", "))
			}
			if len(ticket.Governance.Policy.AuditLabels) > 0 {
				fmt.Printf("AuditTags:  %s\n", strings.Join(ticket.Governance.Policy.AuditLabels, ", "))
			}
			if ticket.Governance.Policy.ApprovalPolicy != nil {
				defaultScope := strings.TrimSpace(string(ticket.Governance.Policy.ApprovalPolicy.DefaultScope))
				maxScope := strings.TrimSpace(string(ticket.Governance.Policy.ApprovalPolicy.MaxScope))
				if defaultScope != "" || maxScope != "" {
					fmt.Printf("GrantScope: default=%s max=%s\n", normalize.FirstNonEmpty(defaultScope, "-"), normalize.FirstNonEmpty(maxScope, "-"))
				}
			}
		}
		if strings.TrimSpace(ticket.Governance.EffectiveConfigSnapshotID) != "" {
			fmt.Printf("ConfigSnap: %s\n", ticket.Governance.EffectiveConfigSnapshotID)
		}
		if scope := formatApprovalRunScope(ticket.Governance.Scope); scope != "" {
			fmt.Printf("Scope:      %s\n", scope)
		}
		if ticket.Governance.Approval != nil && strings.TrimSpace(string(ticket.Governance.Approval.Status)) != "" {
			fmt.Printf("Approval:   %s\n", ticket.Governance.Approval.Status)
		}
		if len(ticket.Governance.ToolNames) > 0 {
			fmt.Printf("Tools:      %s\n", strings.Join(ticket.Governance.ToolNames, ", "))
		}
	}
	if strings.TrimSpace(ticket.ResourceScopeSummary) != "" {
		fmt.Printf("Resource:   %s\n", ticket.ResourceScopeSummary)
	}

	if len(ticket.ToolCalls) > 0 {
		fmt.Println("\nTool Calls:")
		for i, tc := range ticket.ToolCalls {
			fmt.Printf("  [%d] %s (id: %s)\n", i+1, tc.Name, tc.ID)
			if summary := strings.TrimSpace(tc.ResourceScope.Summary); summary != "" {
				fmt.Printf("      scope: %s\n", summary)
			}
		}
	}

	return nil
}

// ---------------------------------------------------------------------------
// approvals approve
// ---------------------------------------------------------------------------

func newApprovalsApproveCmd() *cobra.Command {
	var note string

	cmd := &cobra.Command{
		Use:   "approve <id>",
		Short: "Approve a pending request",
		Long:  "Resolve a pending approval request as approved.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runApprovalsResolve(cmd.Context(), args[0], "approved", note)
		},
	}

	cmd.Flags().StringVar(&note, "note", "", "optional note for the resolution")

	return cmd
}

// ---------------------------------------------------------------------------
// approvals deny
// ---------------------------------------------------------------------------

func newApprovalsDenyCmd() *cobra.Command {
	var note string

	cmd := &cobra.Command{
		Use:   "deny <id>",
		Short: "Deny a pending request",
		Long:  "Resolve a pending approval request as denied.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runApprovalsResolve(cmd.Context(), args[0], "denied", note)
		},
	}

	cmd.Flags().StringVar(&note, "note", "", "optional note for the resolution")

	return cmd
}

// ---------------------------------------------------------------------------
// shared resolve helper
// ---------------------------------------------------------------------------

func runApprovalsResolve(ctx context.Context, id, status, note string) error {
	client, err := NewGatewayClient()
	if err != nil {
		return err
	}

	approvalStatus := approval.StatusDenied
	if strings.EqualFold(strings.TrimSpace(status), "approved") {
		approvalStatus = approval.StatusApproved
	}
	ticket, err := resolveApprovalView(ctx, client, id, approvalStatus, note)
	if err != nil {
		return err
	}

	if flagJSON {
		return printJSON(ticket)
	}

	fmt.Printf("Approval %s %s\n", ticket.ID, ticket.Status)
	return nil
}

func formatApprovalRunScope(scope controlplane.ScopeRef) string {
	return strings.ReplaceAll(controlplane.ScopeSummary(scope), " | ", " ")
}
