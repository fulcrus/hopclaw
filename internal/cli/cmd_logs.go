package cli

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

// ---------------------------------------------------------------------------
// Constants
// ---------------------------------------------------------------------------

const (
	logsEventsPath      = "/runtime/events"
	logsStreamPath      = "/runtime/events/stream"
	logsDefaultLimit    = 50
	logsSSEDataPrefix   = "data: "
	logsScannerInitBuf  = 64 * 1024
	logsScannerMaxBuf   = 1024 * 1024
	logsStreamNoTimeout = 0 // no HTTP client timeout for SSE streaming
)

// ---------------------------------------------------------------------------
// Response types (mirror the API JSON shapes)
// ---------------------------------------------------------------------------

type logEvent struct {
	ID        string         `json:"id"`
	Type      string         `json:"type"`
	RunID     string         `json:"run_id,omitempty"`
	SessionID string         `json:"session_id,omitempty"`
	Time      time.Time      `json:"time"`
	Attrs     map[string]any `json:"attrs,omitempty"`
}

type logEventsListResponse struct {
	Items []logEvent `json:"items"`
}

// ---------------------------------------------------------------------------
// Parent command
// ---------------------------------------------------------------------------

func newLogsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "logs",
		Short: "View event logs",
		Long:  "List and stream event logs from the running gateway.",
	}

	cmd.AddCommand(
		newLogsListCmd(),
		newLogsStreamCmd(),
	)

	return cmd
}

// ---------------------------------------------------------------------------
// logs list
// ---------------------------------------------------------------------------

func newLogsListCmd() *cobra.Command {
	var (
		limit     int
		eventType string
	)

	cmd := &cobra.Command{
		Use:   "list",
		Short: "List recent events",
		Long:  "List recent events from the gateway event log.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runLogsList(cmd.Context(), limit, eventType)
		},
	}

	cmd.Flags().IntVar(&limit, "limit", logsDefaultLimit, "maximum number of events to return")
	cmd.Flags().StringVar(&eventType, "type", "", "filter by event type (e.g. run.completed)")

	return cmd
}

func runLogsList(ctx context.Context, limit int, eventType string) error {
	client, err := NewGatewayClient()
	if err != nil {
		return err
	}

	path := fmt.Sprintf("%s?limit=%d", logsEventsPath, limit)
	if t := strings.TrimSpace(eventType); t != "" {
		path += "&type=" + t
	}

	var resp logEventsListResponse
	if err := client.Get(ctx, path, &resp); err != nil {
		return err
	}

	if flagJSON {
		return printJSON(resp)
	}

	if len(resp.Items) == 0 {
		fmt.Println("no events found")
		return nil
	}

	fmt.Printf("%-12s  %-28s  %-12s  %-12s  %s\n",
		"ID", "TYPE", "RUN", "SESSION", "TIME")
	fmt.Printf("%-12s  %-28s  %-12s  %-12s  %s\n",
		"---", "----", "---", "-------", "----")
	for _, e := range resp.Items {
		runID := e.RunID
		if runID == "" {
			runID = "-"
		}
		sessionID := e.SessionID
		if sessionID == "" {
			sessionID = "-"
		}
		fmt.Printf("%-12s  %-28s  %-12s  %-12s  %s\n",
			truncate(e.ID, 12),
			truncate(e.Type, 28),
			truncate(runID, 12),
			truncate(sessionID, 12),
			formatTime(e.Time),
		)
	}
	return nil
}

// ---------------------------------------------------------------------------
// logs stream
// ---------------------------------------------------------------------------

func newLogsStreamCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "stream",
		Short: "Stream live events (SSE)",
		Long:  "Connect to the gateway event stream and print events as they arrive.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runLogsStream(cmd.Context())
		},
	}
}

func runLogsStream(ctx context.Context) error {
	gc, err := NewGatewayClient()
	if err != nil {
		return err
	}

	// Build request manually — SSE needs a long-lived connection with no timeout.
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, gc.BaseURL+logsStreamPath, nil)
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Accept", "text/event-stream")
	if gc.AuthToken != "" {
		req.Header.Set(authHeaderName, gc.AuthToken)
	}

	streamClient := &http.Client{Timeout: logsStreamNoTimeout}
	resp, err := streamClient.Do(req)
	if err != nil {
		return fmt.Errorf("connect to event stream: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("event stream returned HTTP %d", resp.StatusCode)
	}

	if flagVerbose {
		fmt.Fprintln(os.Stderr, "connected to event stream")
	}

	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 0, logsScannerInitBuf), logsScannerMaxBuf)

	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, logsSSEDataPrefix) {
			continue
		}
		data := strings.TrimPrefix(line, logsSSEDataPrefix)

		if flagJSON {
			fmt.Println(data)
			continue
		}

		var event logEvent
		if err := json.Unmarshal([]byte(data), &event); err != nil {
			// Print raw data if JSON decode fails.
			fmt.Println(data)
			continue
		}

		runID := event.RunID
		if runID == "" {
			runID = "-"
		}
		fmt.Printf("[%s] %-28s  run=%-12s  session=%-12s\n",
			formatTime(event.Time),
			event.Type,
			truncate(runID, 12),
			truncate(event.SessionID, 12),
		)
	}

	if err := scanner.Err(); err != nil {
		// Context cancellation is expected when the user interrupts.
		if ctx.Err() != nil {
			return nil
		}
		return fmt.Errorf("read event stream: %w", err)
	}
	return nil
}
