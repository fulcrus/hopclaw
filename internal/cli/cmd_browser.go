package cli

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"
)

// ---------------------------------------------------------------------------
// Constants
// ---------------------------------------------------------------------------

const (
	browserSessionsPath     = "/operator/browser/sessions"
	browserCapabilitiesPath = "/operator/capabilities"
	browserCapabilityName   = "browser"
	browserDefaultOutput    = "screenshot.png"
)

// ---------------------------------------------------------------------------
// Response types (mirror the API JSON shapes)
// ---------------------------------------------------------------------------

type browserSession struct {
	ID         string         `json:"id"`
	Capability string         `json:"capability"`
	CreatedAt  time.Time      `json:"created_at"`
	Metadata   map[string]any `json:"metadata,omitempty"`
}

type browserSessionsResponse struct {
	Items []browserSession `json:"items"`
	Count int              `json:"count"`
}

type browserCloseResponse struct {
	OK         bool   `json:"ok"`
	SessionID  string `json:"session_id"`
	Capability string `json:"capability"`
}

type browserStatusResponse struct {
	Available bool   `json:"available"`
	Status    string `json:"status"`
	Message   string `json:"message,omitempty"`
	Sessions  int    `json:"sessions"`
}

type browserOpenRequest struct {
	URL string `json:"url"`
}

type browserOpenResponse struct {
	SessionID string `json:"session_id"`
}

type browserTab struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	URL   string `json:"url"`
}

type browserTabsResponse struct {
	Items []browserTab `json:"items"`
	Count int          `json:"count"`
}

// capabilityReport is the shape of individual items in GET /operator/capabilities.
type capabilityReport struct {
	Manifest struct {
		Name string `json:"name"`
	} `json:"manifest"`
	Health struct {
		Status  string `json:"status"`
		Message string `json:"message,omitempty"`
	} `json:"health"`
}

type capabilitiesListResponse struct {
	Items []capabilityReport `json:"items"`
}

// ---------------------------------------------------------------------------
// Parent command
// ---------------------------------------------------------------------------

func newBrowserCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "browser",
		Short: "Manage browser sessions",
		Long:  "List, open, and manage browser sessions on the running gateway.",
	}

	cmd.AddCommand(
		newBrowserSessionsCmd(),
		newBrowserCloseCmd(),
		newBrowserStatusCmd(),
		newBrowserOpenCmd(),
		newBrowserTabsCmd(),
		newBrowserScreenshotCmd(),
	)

	return cmd
}

// ---------------------------------------------------------------------------
// browser sessions
// ---------------------------------------------------------------------------

func newBrowserSessionsCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "sessions",
		Short: "List browser sessions",
		Long:  "List all open browser sessions from the running gateway.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runBrowserSessions(cmd.Context())
		},
	}
}

func runBrowserSessions(ctx context.Context) error {
	client, err := NewGatewayClient()
	if err != nil {
		return err
	}

	var resp browserSessionsResponse
	if err := client.Get(ctx, browserSessionsPath, &resp); err != nil {
		return err
	}

	if flagJSON {
		return printJSON(resp)
	}

	if len(resp.Items) == 0 {
		fmt.Println("no browser sessions found")
		return nil
	}

	fmt.Printf("%-20s  %-16s  %s\n", "ID", "CAPABILITY", "CREATED")
	fmt.Printf("%-20s  %-16s  %s\n", "---", "----------", "-------")
	for _, s := range resp.Items {
		fmt.Printf("%-20s  %-16s  %s\n",
			truncate(s.ID, 20),
			truncate(s.Capability, 16),
			formatTime(s.CreatedAt),
		)
	}

	fmt.Printf("\nTotal: %d sessions\n", resp.Count)
	return nil
}

// ---------------------------------------------------------------------------
// browser close
// ---------------------------------------------------------------------------

func newBrowserCloseCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "close <id>",
		Short: "Close a browser session",
		Long:  "Close an open browser session by ID.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runBrowserClose(cmd.Context(), args[0])
		},
	}
}

func runBrowserClose(ctx context.Context, id string) error {
	client, err := NewGatewayClient()
	if err != nil {
		return err
	}

	var resp browserCloseResponse
	if err := client.Delete(ctx, browserSessionsPath+"/"+id, &resp); err != nil {
		return err
	}

	if flagJSON {
		return printJSON(resp)
	}

	fmt.Printf("Closed browser session %s\n", id)
	return nil
}

// ---------------------------------------------------------------------------
// browser status
// ---------------------------------------------------------------------------

func newBrowserStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show browser capability status",
		Long:  "Show the browser capability status including health and session count.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runBrowserStatus(cmd.Context())
		},
	}
}

func runBrowserStatus(ctx context.Context) error {
	client, err := NewGatewayClient()
	if err != nil {
		return err
	}

	// Fetch all capabilities and filter for browser.
	var caps capabilitiesListResponse
	if err := client.Get(ctx, browserCapabilitiesPath, &caps); err != nil {
		return err
	}

	// Also fetch session count.
	var sessions browserSessionsResponse
	if err := client.Get(ctx, browserSessionsPath, &sessions); err != nil {
		// Non-fatal: browser sessions endpoint may not exist if browser is unavailable.
		sessions = browserSessionsResponse{}
	}

	// Find browser capability.
	status := browserStatusResponse{
		Available: false,
		Status:    "unavailable",
		Sessions:  sessions.Count,
	}
	for _, cap := range caps.Items {
		if cap.Manifest.Name == browserCapabilityName {
			status.Available = true
			status.Status = cap.Health.Status
			status.Message = cap.Health.Message
			break
		}
	}

	if flagJSON {
		return printJSON(status)
	}

	fmt.Printf("Available: %v\n", status.Available)
	fmt.Printf("Status:    %s\n", status.Status)
	if status.Message != "" {
		fmt.Printf("Message:   %s\n", status.Message)
	}
	fmt.Printf("Sessions:  %d\n", status.Sessions)

	return nil
}

// ---------------------------------------------------------------------------
// browser open
// ---------------------------------------------------------------------------

func newBrowserOpenCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "open <url>",
		Short: "Open a new browser session",
		Long:  "Open a new browser session with the given URL.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runBrowserOpen(cmd.Context(), args[0])
		},
	}
}

func runBrowserOpen(ctx context.Context, url string) error {
	client, err := NewGatewayClient()
	if err != nil {
		return err
	}

	reqBody := browserOpenRequest{URL: url}
	var resp browserOpenResponse
	if err := client.Post(ctx, browserSessionsPath, reqBody, &resp); err != nil {
		return err
	}

	if flagJSON {
		return printJSON(resp)
	}

	fmt.Printf("Opened browser session %s\n", resp.SessionID)
	return nil
}

// ---------------------------------------------------------------------------
// browser tabs
// ---------------------------------------------------------------------------

func newBrowserTabsCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "tabs <session-id>",
		Short: "List tabs in a browser session",
		Long:  "List all tabs in a browser session by session ID.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runBrowserTabs(cmd.Context(), args[0])
		},
	}
}

func runBrowserTabs(ctx context.Context, sessionID string) error {
	client, err := NewGatewayClient()
	if err != nil {
		return err
	}

	var resp browserTabsResponse
	if err := client.Get(ctx, browserSessionsPath+"/"+sessionID+"/tabs", &resp); err != nil {
		return err
	}

	if flagJSON {
		return printJSON(resp)
	}

	if len(resp.Items) == 0 {
		fmt.Println("no tabs found")
		return nil
	}

	fmt.Printf("%-20s  %-30s  %s\n", "ID", "TITLE", "URL")
	fmt.Printf("%-20s  %-30s  %s\n", "---", "-----", "---")
	for _, t := range resp.Items {
		fmt.Printf("%-20s  %-30s  %s\n",
			truncate(t.ID, 20),
			truncate(t.Title, 30),
			truncate(t.URL, 60),
		)
	}

	fmt.Printf("\nTotal: %d tabs\n", resp.Count)
	return nil
}

// ---------------------------------------------------------------------------
// browser screenshot
// ---------------------------------------------------------------------------

func newBrowserScreenshotCmd() *cobra.Command {
	var output string

	cmd := &cobra.Command{
		Use:   "screenshot <session-id>",
		Short: "Take a screenshot of a browser session",
		Long:  "Take a screenshot of a browser session and save it to a file.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runBrowserScreenshot(cmd.Context(), args[0], output)
		},
	}

	cmd.Flags().StringVar(&output, "output", browserDefaultOutput, "output file path for the screenshot")

	return cmd
}

func runBrowserScreenshot(ctx context.Context, sessionID string, output string) error {
	client, err := NewGatewayClient()
	if err != nil {
		return err
	}

	data, err := client.GetRaw(ctx, browserSessionsPath+"/"+sessionID+"/screenshot")
	if err != nil {
		return err
	}

	if err := os.WriteFile(output, data, 0o644); err != nil {
		return fmt.Errorf("write screenshot file: %w", err)
	}

	if flagJSON {
		return printJSON(struct {
			Path string `json:"path"`
			Size int    `json:"size"`
		}{
			Path: output,
			Size: len(data),
		})
	}

	fmt.Printf("Screenshot saved to %s (%d bytes)\n", output, len(data))
	return nil
}
