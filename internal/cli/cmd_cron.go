package cli

import (
	"context"
	"fmt"
	"strings"
	"time"

	autopkg "github.com/fulcrus/hopclaw/automation"
	"github.com/spf13/cobra"
)

// ---------------------------------------------------------------------------
// Constants
// ---------------------------------------------------------------------------

const (
	cronJobsPath   = "/operator/cron/jobs"
	cronStatusPath = "/operator/cron/status"
	automationPath = "/operator/automation/items"
	templatesPath  = "/operator/automation/templates"
)

// ---------------------------------------------------------------------------
// Response types (mirror the gateway JSON shapes)
// ---------------------------------------------------------------------------

type cronCLIJob struct {
	ID         string          `json:"id"`
	Name       string          `json:"name"`
	Enabled    bool            `json:"enabled"`
	Schedule   cronCLISchedule `json:"schedule"`
	Payload    cronCLIPayload  `json:"payload"`
	SessionKey string          `json:"session_key,omitempty"`
	Model      string          `json:"model,omitempty"`
	LastRunAt  time.Time       `json:"last_run_at,omitempty"`
	NextRunAt  time.Time       `json:"next_run_at,omitempty"`
	LastStatus string          `json:"last_status,omitempty"`
	LastError  string          `json:"last_error,omitempty"`
	CreatedAt  time.Time       `json:"created_at"`
	UpdatedAt  time.Time       `json:"updated_at"`
}

type cronCLISchedule struct {
	Kind       string `json:"kind"`
	At         string `json:"at,omitempty"`
	Every      string `json:"every,omitempty"`
	Expression string `json:"expression,omitempty"`
	Timezone   string `json:"timezone,omitempty"`
}

type cronCLIPayload struct {
	Content string `json:"content"`
}

type cronCLIJobResponse struct {
	Job cronCLIJob `json:"job"`
}

type cronCLIOKResponse struct {
	OK bool   `json:"ok"`
	ID string `json:"id,omitempty"`
}

type automationCLIItemsResponse struct {
	Items    []autopkg.Item                   `json:"items"`
	Count    int                              `json:"count"`
	Services map[string]autopkg.ServiceStatus `json:"services"`
}

type automationCLITemplatesResponse struct {
	Items []autopkg.StarterTemplate `json:"items"`
	Count int                       `json:"count"`
}

type automationCLIItemDetailResponse struct {
	Item             autopkg.Item              `json:"item"`
	RecentExecutions []autopkg.ExecutionRecord `json:"recent_executions,omitempty"`
	RunPath          string                    `json:"run_path,omitempty"`
	CanReplay        bool                      `json:"can_replay,omitempty"`
	ErrorSignatures  []automationCLIErrorSig   `json:"error_signatures,omitempty"`
}

type automationCLIErrorSig struct {
	Signature      string    `json:"signature"`
	Count          int       `json:"count"`
	LastOccurredAt time.Time `json:"last_occurred_at,omitempty"`
	LastError      string    `json:"last_error,omitempty"`
}

// ---------------------------------------------------------------------------
// Parent command
// ---------------------------------------------------------------------------

func newAutomationCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "automation",
		Aliases: []string{"cron"},
		Short:   "Inspect and manage automations",
		Long:    "Inspect scheduled jobs, hooks, wakeups, and watches. Create and trigger scheduled jobs from the CLI.",
	}
	cmd.AddCommand(
		newAutomationListCmd(),
		newAutomationInspectCmd(),
		newAutomationRecentCmd(),
		newAutomationTemplatesCmd(),
		newAutomationPauseCmd(),
		newAutomationResumeCmd(),
		newCronCreateCmd(),
		newCronDeleteCmd(),
		newCronTriggerCmd(),
		newAutomationStatusCmd(),
	)
	return cmd
}

// ---------------------------------------------------------------------------
// cron list
// ---------------------------------------------------------------------------

func newAutomationListCmd() *cobra.Command {
	var kind string
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List automations",
		Long:  "List scheduled jobs, watches, wakeups, and hooks from the running gateway.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runAutomationList(cmd.Context(), kind)
		},
	}
	cmd.Flags().StringVar(&kind, "kind", "", "filter by automation kind: cron, wakeup, watch, hook")
	return cmd
}

func runAutomationList(ctx context.Context, kind string) error {
	client, err := NewGatewayClient()
	if err != nil {
		return err
	}

	path := automationPath
	if kind = strings.TrimSpace(kind); kind != "" {
		path += "?kinds=" + kind
	}

	var resp automationCLIItemsResponse
	if err := client.Get(ctx, path, &resp); err != nil {
		return err
	}

	if flagJSON {
		return printJSON(resp)
	}

	if len(resp.Items) == 0 {
		fmt.Println("no automations found")
		return nil
	}

	fmt.Printf("%-12s  %-24s  %-8s  %-10s  %-12s  %s\n", "KIND", "NAME", "ENABLED", "SOURCE", "LAST", "NEXT")
	fmt.Printf("%-12s  %-24s  %-8s  %-10s  %-12s  %s\n", "----", "----", "-------", "------", "----", "----")
	for _, item := range resp.Items {
		source := item.SourceKind
		if source == "" {
			source = "-"
		}
		fmt.Printf("%-12s  %-24s  %-8v  %-10s  %-12s  %s\n",
			item.Kind,
			truncate(item.Name, 24),
			item.Enabled,
			truncate(source, 10),
			formatTime(item.LastRunAt),
			formatTime(item.NextRunAt),
		)
	}
	fmt.Printf("\nTotal: %d automations\n", resp.Count)
	return nil
}

func newAutomationInspectCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "inspect <kind> <id>",
		Short: "Inspect one automation",
		Long:  "Show detail, recent executions, and replayability for a specific automation item.",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runAutomationInspect(cmd.Context(), args[0], args[1])
		},
	}
}

func runAutomationInspect(ctx context.Context, kind, id string) error {
	client, err := NewGatewayClient()
	if err != nil {
		return err
	}

	var resp automationCLIItemDetailResponse
	if err := client.Get(ctx, automationPath+"/"+strings.TrimSpace(kind)+"/"+strings.TrimSpace(id), &resp); err != nil {
		return err
	}

	if flagJSON {
		return printJSON(resp)
	}

	fmt.Printf("Name:       %s\n", resp.Item.Name)
	fmt.Printf("Kind:       %s\n", resp.Item.Kind)
	fmt.Printf("ID:         %s\n", resp.Item.ID)
	fmt.Printf("Enabled:    %v\n", resp.Item.Enabled)
	if resp.Item.Schedule != "" {
		fmt.Printf("Schedule:   %s\n", resp.Item.Schedule)
	}
	if resp.Item.Channel != "" {
		fmt.Printf("Channel:    %s\n", resp.Item.Channel)
	}
	if resp.RunPath != "" {
		fmt.Printf("Run Path:   %s\n", resp.RunPath)
	}
	fmt.Printf("Can Replay: %v\n", resp.CanReplay)
	if len(resp.RecentExecutions) == 0 {
		fmt.Println("Recent:     none")
		return nil
	}
	fmt.Println("Recent Executions:")
	for _, exec := range resp.RecentExecutions {
		summary := exec.Summary
		if summary == "" {
			summary = exec.Error
		}
		if summary == "" {
			summary = "-"
		}
		fmt.Printf("  - %s  %-8s  %s\n", formatTime(exec.OccurredAt), exec.Status, truncate(summary, 80))
	}
	return nil
}

func newAutomationRecentCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "recent <kind> <id>",
		Short: "Show recent executions for one automation",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runAutomationRecent(cmd.Context(), args[0], args[1])
		},
	}
}

func runAutomationRecent(ctx context.Context, kind, id string) error {
	client, err := NewGatewayClient()
	if err != nil {
		return err
	}
	var resp automationCLIItemDetailResponse
	if err := client.Get(ctx, automationPath+"/"+strings.TrimSpace(kind)+"/"+strings.TrimSpace(id), &resp); err != nil {
		return err
	}
	if flagJSON {
		return printJSON(resp.RecentExecutions)
	}
	if len(resp.RecentExecutions) == 0 {
		fmt.Println("no recent executions found")
		return nil
	}
	fmt.Printf("%-24s  %-10s  %-18s  %s\n", "TIME", "STATUS", "VERIFICATION", "SUMMARY")
	fmt.Printf("%-24s  %-10s  %-18s  %s\n", "----", "------", "------------", "-------")
	for _, exec := range resp.RecentExecutions {
		summary := exec.Summary
		if summary == "" {
			summary = exec.Error
		}
		if summary == "" {
			summary = "-"
		}
		verification := exec.VerificationStatus
		if verification == "" {
			verification = "-"
		}
		fmt.Printf("%-24s  %-10s  %-18s  %s\n",
			formatTime(exec.OccurredAt),
			truncate(exec.Status, 10),
			truncate(verification, 18),
			truncate(summary, 72),
		)
	}
	return nil
}

func newAutomationTemplatesCmd() *cobra.Command {
	var kind string
	cmd := &cobra.Command{
		Use:   "templates",
		Short: "List starter automation templates",
		Long:  "List starter templates that can be used to seed automations in the console or APIs.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runAutomationTemplates(cmd.Context(), kind)
		},
	}
	cmd.Flags().StringVar(&kind, "kind", "", "filter by template kind")
	return cmd
}

func runAutomationTemplates(ctx context.Context, kind string) error {
	client, err := NewGatewayClient()
	if err != nil {
		return err
	}
	path := templatesPath
	if kind = strings.TrimSpace(kind); kind != "" {
		path += "?kind=" + kind
	}
	var resp automationCLITemplatesResponse
	if err := client.Get(ctx, path, &resp); err != nil {
		return err
	}
	if flagJSON {
		return printJSON(resp)
	}
	if len(resp.Items) == 0 {
		fmt.Println("no templates found")
		return nil
	}
	fmt.Printf("%-12s  %-24s  %s\n", "KIND", "NAME", "DESCRIPTION")
	fmt.Printf("%-12s  %-24s  %s\n", "----", "----", "-----------")
	for _, item := range resp.Items {
		description := item.Headline
		if description == "" {
			description = item.Summary
		}
		fmt.Printf("%-12s  %-24s  %s\n", item.Kind, truncate(item.Name, 24), truncate(description, 60))
	}
	fmt.Printf("\nTotal: %d templates\n", resp.Count)
	return nil
}

func newAutomationPauseCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "pause <kind> <id>",
		Short: "Disable one automation item",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runAutomationEnabled(cmd.Context(), args[0], args[1], false)
		},
	}
}

func newAutomationResumeCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "resume <kind> <id>",
		Short: "Enable one automation item",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runAutomationEnabled(cmd.Context(), args[0], args[1], true)
		},
	}
}

func runAutomationEnabled(ctx context.Context, kind, id string, enabled bool) error {
	client, err := NewGatewayClient()
	if err != nil {
		return err
	}
	path, err := automationUpdatePath(kind, id)
	if err != nil {
		return err
	}
	body := map[string]any{"enabled": enabled}
	var resp map[string]any
	if err := client.Patch(ctx, path, body, &resp); err != nil {
		return err
	}
	if flagJSON {
		return printJSON(resp)
	}
	action := "Paused"
	if enabled {
		action = "Resumed"
	}
	fmt.Printf("%s automation %s (%s)\n", action, id, strings.TrimSpace(kind))
	return nil
}

func automationUpdatePath(kind, id string) (string, error) {
	id = strings.TrimSpace(id)
	switch autopkg.Kind(strings.TrimSpace(kind)) {
	case autopkg.KindCron:
		return cronJobsPath + "/" + id, nil
	case autopkg.KindWatch:
		return "/operator/watch/items/" + id, nil
	case autopkg.KindWakeup:
		return "/operator/wakeup/triggers/" + id, nil
	case autopkg.KindHook:
		return hooksBasePath + "/" + id, nil
	default:
		return "", fmt.Errorf("unsupported automation kind %q (want cron, watch, wakeup, or hook)", kind)
	}
}

// ---------------------------------------------------------------------------
// cron create
// ---------------------------------------------------------------------------

func newCronCreateCmd() *cobra.Command {
	var (
		name         string
		scheduleKind string
		expression   string
		every        string
		at           string
		content      string
		model        string
		sessionKey   string
		timezone     string
		disabled     bool
	)

	cmd := &cobra.Command{
		Use:   "create",
		Short: "Create a new cron job",
		Long: `Create a new scheduled cron job on the running gateway.

Examples:
  hopclaw automation create --name daily-report --schedule-kind cron --expression "0 9 * * *" --content "generate daily report"
  hopclaw automation create --name hourly-check --schedule-kind every --every 1h --content "run health check"
  hopclaw automation create --name one-shot --schedule-kind at --at 2025-01-01T09:00:00Z --content "happy new year"`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			schedule := cronCLISchedule{
				Kind:       strings.TrimSpace(scheduleKind),
				Expression: strings.TrimSpace(expression),
				Every:      strings.TrimSpace(every),
				At:         strings.TrimSpace(at),
				Timezone:   strings.TrimSpace(timezone),
			}
			enabled := !disabled
			return runCronCreate(cmd.Context(), name, schedule, content, model, sessionKey, enabled)
		},
	}

	cmd.Flags().StringVar(&name, "name", "", "job name (required)")
	cmd.Flags().StringVar(&scheduleKind, "schedule-kind", "", "schedule kind: cron, every, or at (required)")
	cmd.Flags().StringVar(&expression, "expression", "", "cron expression (for schedule-kind=cron)")
	cmd.Flags().StringVar(&every, "every", "", "interval duration, e.g. 1h (for schedule-kind=every)")
	cmd.Flags().StringVar(&at, "at", "", "RFC3339 timestamp (for schedule-kind=at)")
	cmd.Flags().StringVar(&content, "content", "", "message content to submit (required)")
	cmd.Flags().StringVar(&model, "model", "", "model override")
	cmd.Flags().StringVar(&sessionKey, "session-key", "", "session key override")
	cmd.Flags().StringVar(&timezone, "timezone", "", "timezone for cron expressions (e.g. America/New_York)")
	cmd.Flags().BoolVar(&disabled, "disabled", false, "create the job in disabled state")

	_ = cmd.MarkFlagRequired("name")
	_ = cmd.MarkFlagRequired("schedule-kind")
	_ = cmd.MarkFlagRequired("content")

	return cmd
}

func runCronCreate(ctx context.Context, name string, schedule cronCLISchedule, content, model, sessionKey string, enabled bool) error {
	client, err := NewGatewayClient()
	if err != nil {
		return err
	}

	body := map[string]any{
		"name":    strings.TrimSpace(name),
		"enabled": enabled,
		"schedule": map[string]any{
			"kind":       schedule.Kind,
			"at":         schedule.At,
			"every":      schedule.Every,
			"expression": schedule.Expression,
			"timezone":   schedule.Timezone,
		},
		"payload": map[string]any{
			"content": content,
		},
	}
	if m := strings.TrimSpace(model); m != "" {
		body["model"] = m
	}
	if sk := strings.TrimSpace(sessionKey); sk != "" {
		body["session_key"] = sk
	}

	var resp cronCLIJobResponse
	if err := client.Post(ctx, cronJobsPath, body, &resp); err != nil {
		return err
	}

	if flagJSON {
		return printJSON(resp)
	}

	fmt.Printf("Created cron job %s (%s)\n", resp.Job.ID, resp.Job.Name)
	if !resp.Job.NextRunAt.IsZero() {
		fmt.Printf("Next run at: %s\n", resp.Job.NextRunAt.Format(time.RFC3339))
	}
	return nil
}

// ---------------------------------------------------------------------------
// cron delete
// ---------------------------------------------------------------------------

func newCronDeleteCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "delete <id>",
		Short: "Delete a cron job",
		Long:  "Delete a cron job from the running gateway.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runCronDelete(cmd.Context(), args[0])
		},
	}
}

func runCronDelete(ctx context.Context, id string) error {
	client, err := NewGatewayClient()
	if err != nil {
		return err
	}

	var resp cronCLIOKResponse
	if err := client.Delete(ctx, cronJobsPath+"/"+id, &resp); err != nil {
		return err
	}

	if flagJSON {
		return printJSON(resp)
	}

	fmt.Printf("Deleted cron job %s\n", id)
	return nil
}

// ---------------------------------------------------------------------------
// cron trigger
// ---------------------------------------------------------------------------

func newCronTriggerCmd() *cobra.Command {
	return &cobra.Command{
		Use:     "trigger <id>",
		Aliases: []string{"run"},
		Short:   "Manually trigger a cron job",
		Long:    "Manually trigger an immediate run of a cron job on the running gateway.",
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runCronTrigger(cmd.Context(), args[0])
		},
	}
}

func runCronTrigger(ctx context.Context, id string) error {
	client, err := NewGatewayClient()
	if err != nil {
		return err
	}

	var resp cronCLIOKResponse
	if err := client.Post(ctx, cronJobsPath+"/"+id+"/run", nil, &resp); err != nil {
		return err
	}

	if flagJSON {
		return printJSON(resp)
	}

	fmt.Printf("Triggered cron job %s\n", id)
	return nil
}

// ---------------------------------------------------------------------------
// cron status
// ---------------------------------------------------------------------------

func newAutomationStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show automation service status",
		Long:  "Show runtime availability for automation services such as cron, watch, wakeup, and hooks.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runAutomationStatus(cmd.Context())
		},
	}
}

func runAutomationStatus(ctx context.Context) error {
	client, err := NewGatewayClient()
	if err != nil {
		return err
	}

	var resp automationCLIItemsResponse
	if err := client.Get(ctx, automationPath, &resp); err != nil {
		return err
	}

	if flagJSON {
		return printJSON(resp.Services)
	}

	if len(resp.Services) == 0 {
		fmt.Println("no automation services reported")
		return nil
	}
	fmt.Printf("%-12s  %-10s  %-10s  %s\n", "SERVICE", "AVAILABLE", "RUNNING", "COUNT")
	fmt.Printf("%-12s  %-10s  %-10s  %s\n", "-------", "---------", "-------", "-----")
	for _, name := range []string{"cron", "wakeup", "watch", "hook"} {
		status, ok := resp.Services[name]
		if !ok {
			continue
		}
		fmt.Printf("%-12s  %-10v  %-10v  %d\n", name, status.Available, status.Running, status.Count)
	}
	return nil
}
