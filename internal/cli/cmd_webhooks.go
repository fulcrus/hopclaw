package cli

import (
	"context"
	"fmt"
	"strings"

	"github.com/spf13/cobra"
)

// ---------------------------------------------------------------------------
// Constants
// ---------------------------------------------------------------------------

const (
	webhooksBasePath = "/operator/webhooks"
)

// ---------------------------------------------------------------------------
// Response types (mirror the gateway JSON shapes)
// ---------------------------------------------------------------------------

type webhookEntry struct {
	ID        string   `json:"id"`
	URL       string   `json:"url"`
	Events    []string `json:"events"`
	Secret    string   `json:"secret,omitempty"`
	Enabled   bool     `json:"enabled"`
	CreatedAt string   `json:"created_at"`
}

type webhookListResponse struct {
	Items []webhookEntry `json:"items"`
	Count int            `json:"count"`
}

type webhookGetResponse struct {
	Webhook          webhookEntry      `json:"webhook"`
	RecentDeliveries []webhookDelivery `json:"recent_deliveries,omitempty"`
}

type webhookDelivery struct {
	ID          string `json:"id"`
	Event       string `json:"event"`
	Status      int    `json:"status"`
	DeliveredAt string `json:"delivered_at"`
}

type webhookCreateResponse struct {
	ID string `json:"id"`
}

type webhookOKResponse struct {
	OK     bool   `json:"ok"`
	Status int    `json:"status,omitempty"`
	ID     string `json:"id,omitempty"`
}

// ---------------------------------------------------------------------------
// Parent command
// ---------------------------------------------------------------------------

func newWebhooksCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "webhooks",
		Short: "Manage webhooks",
		Long:  "Create, list, delete, and test webhooks on the running gateway.",
	}
	cmd.AddCommand(
		newWebhooksListCmd(),
		newWebhooksCreateCmd(),
		newWebhooksDeleteCmd(),
		newWebhooksTestCmd(),
		newWebhooksInfoCmd(),
	)
	return cmd
}

// ---------------------------------------------------------------------------
// webhooks list
// ---------------------------------------------------------------------------

func newWebhooksListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List all webhooks",
		Long:  "List all webhooks from the running gateway.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runWebhooksList(cmd.Context())
		},
	}
}

func runWebhooksList(ctx context.Context) error {
	client, err := NewGatewayClient()
	if err != nil {
		return err
	}

	var resp webhookListResponse
	if err := client.Get(ctx, webhooksBasePath, &resp); err != nil {
		return err
	}

	if flagJSON {
		return printJSON(resp)
	}

	if len(resp.Items) == 0 {
		fmt.Println("no webhooks found")
		return nil
	}

	fmt.Printf("%-18s  %-36s  %-24s  %-8s  %s\n",
		"ID", "URL", "EVENTS", "STATUS", "CREATED")
	fmt.Printf("%-18s  %-36s  %-24s  %-8s  %s\n",
		"---", "---", "------", "------", "-------")
	for _, w := range resp.Items {
		events := strings.Join(w.Events, ",")
		status := "disabled"
		if w.Enabled {
			status = "enabled"
		}
		fmt.Printf("%-18s  %-36s  %-24s  %-8s  %s\n",
			w.ID,
			truncate(w.URL, 36),
			truncate(events, 24),
			status,
			w.CreatedAt,
		)
	}
	fmt.Printf("\nTotal: %d webhooks\n", resp.Count)
	return nil
}

// ---------------------------------------------------------------------------
// webhooks create
// ---------------------------------------------------------------------------

func newWebhooksCreateCmd() *cobra.Command {
	var (
		url    string
		events string
		secret string
	)

	cmd := &cobra.Command{
		Use:   "create",
		Short: "Create a new webhook",
		Long: `Create a new webhook on the running gateway.

Examples:
  hopclaw webhooks create --url "https://example.com/hook" --events "run.completed,tool.executed"
  hopclaw webhooks create --url "https://example.com/hook" --events "run.completed" --secret "mysecret"`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			eventList := strings.Split(strings.TrimSpace(events), ",")
			for i := range eventList {
				eventList[i] = strings.TrimSpace(eventList[i])
			}
			return runWebhooksCreate(cmd.Context(), url, eventList, secret)
		},
	}

	cmd.Flags().StringVar(&url, "url", "", "webhook callback URL (required)")
	cmd.Flags().StringVar(&events, "events", "", "comma-separated event types (required)")
	cmd.Flags().StringVar(&secret, "secret", "", "optional signing secret")

	_ = cmd.MarkFlagRequired("url")
	_ = cmd.MarkFlagRequired("events")

	return cmd
}

type webhookCreateRequest struct {
	URL    string   `json:"url"`
	Events []string `json:"events"`
	Secret string   `json:"secret,omitempty"`
}

func runWebhooksCreate(ctx context.Context, url string, events []string, secret string) error {
	client, err := NewGatewayClient()
	if err != nil {
		return err
	}

	body := webhookCreateRequest{
		URL:    strings.TrimSpace(url),
		Events: events,
		Secret: strings.TrimSpace(secret),
	}

	var resp webhookCreateResponse
	if err := client.Post(ctx, webhooksBasePath, body, &resp); err != nil {
		return err
	}

	if flagJSON {
		return printJSON(resp)
	}

	fmt.Printf("Created webhook %s\n", resp.ID)
	return nil
}

// ---------------------------------------------------------------------------
// webhooks delete
// ---------------------------------------------------------------------------

func newWebhooksDeleteCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "delete <id>",
		Short: "Delete a webhook",
		Long:  "Delete a webhook from the running gateway.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runWebhooksDelete(cmd.Context(), args[0])
		},
	}
}

func runWebhooksDelete(ctx context.Context, id string) error {
	client, err := NewGatewayClient()
	if err != nil {
		return err
	}

	var resp webhookOKResponse
	if err := client.Delete(ctx, webhooksBasePath+"/"+id, &resp); err != nil {
		return err
	}

	if flagJSON {
		return printJSON(resp)
	}

	fmt.Printf("Deleted webhook %s\n", id)
	return nil
}

// ---------------------------------------------------------------------------
// webhooks test
// ---------------------------------------------------------------------------

func newWebhooksTestCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "test <id>",
		Short: "Send a test event to a webhook",
		Long:  "Send a test event to a webhook and display the response status.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runWebhooksTest(cmd.Context(), args[0])
		},
	}
}

func runWebhooksTest(ctx context.Context, id string) error {
	client, err := NewGatewayClient()
	if err != nil {
		return err
	}

	var resp webhookOKResponse
	if err := client.Post(ctx, webhooksBasePath+"/"+id+"/test", nil, &resp); err != nil {
		return err
	}

	if flagJSON {
		return printJSON(resp)
	}

	if resp.OK {
		fmt.Printf("Test event delivered to webhook %s (HTTP %d)\n", id, resp.Status)
	} else {
		fmt.Printf("Test event delivery failed for webhook %s (HTTP %d)\n", id, resp.Status)
	}
	return nil
}

// ---------------------------------------------------------------------------
// webhooks info
// ---------------------------------------------------------------------------

func newWebhooksInfoCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "info <id>",
		Short: "Show webhook details",
		Long:  "Show details for a webhook including URL, events, secret (masked), and recent deliveries.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runWebhooksInfo(cmd.Context(), args[0])
		},
	}
}

func runWebhooksInfo(ctx context.Context, id string) error {
	client, err := NewGatewayClient()
	if err != nil {
		return err
	}

	var resp webhookGetResponse
	if err := client.Get(ctx, webhooksBasePath+"/"+id, &resp); err != nil {
		return err
	}

	if flagJSON {
		return printJSON(resp)
	}

	w := resp.Webhook
	status := "disabled"
	if w.Enabled {
		status = "enabled"
	}

	fmt.Printf("ID:      %s\n", w.ID)
	fmt.Printf("URL:     %s\n", w.URL)
	fmt.Printf("Events:  %s\n", strings.Join(w.Events, ", "))
	fmt.Printf("Status:  %s\n", status)
	fmt.Printf("Created: %s\n", w.CreatedAt)

	if w.Secret != "" {
		fmt.Printf("Secret:  %s\n", maskSecret(w.Secret))
	}

	if len(resp.RecentDeliveries) > 0 {
		fmt.Printf("\nRecent Deliveries:\n")
		fmt.Printf("  %-18s  %-20s  %-6s  %s\n", "ID", "EVENT", "STATUS", "DELIVERED")
		fmt.Printf("  %-18s  %-20s  %-6s  %s\n", "---", "-----", "------", "---------")
		for _, d := range resp.RecentDeliveries {
			fmt.Printf("  %-18s  %-20s  %-6d  %s\n",
				d.ID,
				truncate(d.Event, 20),
				d.Status,
				d.DeliveredAt,
			)
		}
	}

	return nil
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

const (
	secretMaskLen    = 4
	secretMaskPrefix = "****"
)

func maskSecret(s string) string {
	if len(s) <= secretMaskLen {
		return secretMaskPrefix
	}
	return secretMaskPrefix + s[len(s)-secretMaskLen:]
}
