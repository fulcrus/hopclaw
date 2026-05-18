package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/fulcrus/hopclaw/contextengine"
	"github.com/spf13/cobra"
)

// ---------------------------------------------------------------------------
// sessions command group
// ---------------------------------------------------------------------------

func newSessionsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "sessions",
		Short: "Manage conversations",
		Long:  "List and inspect saved conversations on the running gateway.",
	}

	cmd.AddCommand(
		newSessionsListCmd(),
		newSessionsGetCmd(),
		newSessionsExportCmd(),
		newSessionsPruneCmd(),
	)

	return cmd
}

// ---------------------------------------------------------------------------
// sessions list
// ---------------------------------------------------------------------------

func newSessionsListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List conversations",
		Long:  "List saved conversations from the running gateway.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runSessionsList(cmd.Context())
		},
	}
}

// sessionsListResponse is the shape of GET /runtime/sessions.
type sessionsListResponse struct {
	Items []sessionSummary `json:"items"`
	Count int              `json:"count"`
}

type sessionSummary struct {
	ID           string    `json:"id"`
	Key          string    `json:"key"`
	Model        string    `json:"model"`
	MessageCount int       `json:"message_count"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

func runSessionsList(ctx context.Context) error {
	client, err := NewGatewayClient()
	if err != nil {
		return err
	}

	var resp sessionsListResponse
	if err := client.Get(ctx, "/runtime/sessions", &resp); err != nil {
		return err
	}

	if flagJSON {
		return printJSON(resp)
	}

	if len(resp.Items) == 0 {
		fmt.Println("no conversations found")
		return nil
	}

	fmt.Printf("%-12s  %-24s  %-24s  %-6s  %s\n", "ID", "KEY", "MODEL", "TURNS", "UPDATED")
	fmt.Printf("%-12s  %-24s  %-24s  %-6s  %s\n", "---", "---", "-----", "-----", "-------")
	for _, s := range resp.Items {
		fmt.Printf("%-12s  %-24s  %-24s  %-6d  %s\n",
			s.ID,
			truncate(s.Key, 24),
			truncate(s.Model, 24),
			s.MessageCount,
			formatTime(s.UpdatedAt),
		)
	}
	return nil
}

// ---------------------------------------------------------------------------
// sessions get
// ---------------------------------------------------------------------------

func newSessionsGetCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "get <id>",
		Short: "Get conversation details",
		Long:  "Get full conversation details including messages.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runSessionsGet(cmd.Context(), args[0])
		},
	}
}

// sessionDetail is the shape of GET /runtime/sessions/{id}.
type sessionDetail struct {
	ID        string           `json:"id"`
	Key       string           `json:"key"`
	Model     string           `json:"model"`
	CreatedAt time.Time        `json:"created_at"`
	UpdatedAt time.Time        `json:"updated_at"`
	Messages  []sessionMessage `json:"messages,omitempty"`
}

type sessionMessage struct {
	Role      string    `json:"role"`
	Content   string    `json:"content"`
	CreatedAt time.Time `json:"created_at"`
}

func runSessionsGet(ctx context.Context, id string) error {
	client, err := NewGatewayClient()
	if err != nil {
		return err
	}

	var session sessionDetail
	if err := client.Get(ctx, "/runtime/sessions/"+id, &session); err != nil {
		return err
	}

	if flagJSON {
		return printJSON(session)
	}

	fmt.Printf("Conversation: %s\n", session.ID)
	fmt.Printf("Key:      %s\n", session.Key)
	fmt.Printf("Model:    %s\n", session.Model)
	fmt.Printf("Created:  %s\n", formatTime(session.CreatedAt))
	fmt.Printf("Updated:  %s\n", formatTime(session.UpdatedAt))
	fmt.Printf("Turns:    %d\n", len(session.Messages))

	if len(session.Messages) > 0 {
		fmt.Println()
		for i, msg := range session.Messages {
			content := msg.Content
			if len(content) > 200 {
				content = content[:200] + "..."
			}
			fmt.Printf("  [%d] %s: %s\n", i+1, msg.Role, content)
		}
	}

	return nil
}

// ---------------------------------------------------------------------------
// sessions export
// ---------------------------------------------------------------------------

func newSessionsExportCmd() *cobra.Command {
	var outputPath string
	var format string

	cmd := &cobra.Command{
		Use:   "export <id>",
		Short: "Export conversation messages",
		Long:  "Export the full conversation transcript for a saved conversation.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runSessionsExport(cmd.Context(), args[0], outputPath, format)
		},
	}

	cmd.Flags().StringVar(&outputPath, "output", "", "write export to a file instead of stdout")
	cmd.Flags().StringVar(&format, "format", "text", "export format: text or json")

	return cmd
}

func runSessionsExport(ctx context.Context, sessionID string, outputPath string, format string) error {
	client, err := NewGatewayClient()
	if err != nil {
		return err
	}
	return runSessionsExportWithClient(ctx, client, sessionID, outputPath, format, os.Stdout)
}

func runSessionsExportWithClient(ctx context.Context, client *GatewayClient, sessionID string, outputPath string, format string, stdout io.Writer) error {
	if client == nil {
		return fmt.Errorf("gateway client is not configured")
	}

	raw, err := client.GetRaw(ctx, "/runtime/sessions/"+sessionID+"/messages")
	if err != nil {
		return err
	}

	format = strings.ToLower(strings.TrimSpace(format))
	if format == "" {
		format = "text"
	}

	var data []byte
	switch format {
	case "json":
		data = raw
	case "text":
		data, err = renderSessionMessagesText(raw)
		if err != nil {
			return err
		}
	default:
		return fmt.Errorf("unsupported export format %q", format)
	}

	return writeSessionsExport(outputPath, data, stdout)
}

func renderSessionMessagesText(raw []byte) ([]byte, error) {
	var messages []contextengine.Message
	if err := json.Unmarshal(raw, &messages); err != nil {
		return nil, fmt.Errorf("decode session messages: %w", err)
	}

	var builder strings.Builder
	for _, message := range messages {
		timestamp := "-"
		if !message.CreatedAt.IsZero() {
			timestamp = message.CreatedAt.Format("2006-01-02 15:04:05")
		}
		builder.WriteString("[")
		builder.WriteString(timestamp)
		builder.WriteString("] ")
		builder.WriteString(string(message.Role))
		builder.WriteString(":\n")
		builder.WriteString(strings.TrimSuffix(message.TextContent(), "\n"))
		builder.WriteString("\n\n")
	}
	return []byte(builder.String()), nil
}

func writeSessionsExport(outputPath string, data []byte, stdout io.Writer) error {
	if strings.TrimSpace(outputPath) != "" {
		if err := os.WriteFile(outputPath, data, 0o644); err != nil {
			return fmt.Errorf("write export file: %w", err)
		}
		return nil
	}

	if stdout == nil {
		stdout = os.Stdout
	}
	if _, err := stdout.Write(data); err != nil {
		return fmt.Errorf("write export output: %w", err)
	}
	return nil
}

// ---------------------------------------------------------------------------
// sessions prune
// ---------------------------------------------------------------------------

var dayDurationPattern = regexp.MustCompile(`(?i)(\d+(?:\.\d+)?)d`)

func newSessionsPruneCmd() *cobra.Command {
	var olderThan string
	var dryRun bool

	cmd := &cobra.Command{
		Use:   "prune",
		Short: "Delete old conversations",
		Long:  "Remove conversations older than the specified duration.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runSessionsPrune(cmd.Context(), olderThan, dryRun)
		},
	}
	cmd.Flags().StringVar(&olderThan, "older-than", "30d", "delete conversations older than this duration (e.g. 7d, 24h)")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "show what would be deleted without deleting")
	return cmd
}

func runSessionsPrune(ctx context.Context, olderThan string, dryRun bool) error {
	client, err := NewGatewayClient()
	if err != nil {
		return err
	}
	return runSessionsPruneWithClient(ctx, client, olderThan, dryRun, os.Stdout, time.Now())
}

func runSessionsPruneWithClient(ctx context.Context, client *GatewayClient, olderThan string, dryRun bool, stdout io.Writer, now time.Time) error {
	if client == nil {
		return fmt.Errorf("gateway client is not configured")
	}
	if stdout == nil {
		stdout = os.Stdout
	}
	if now.IsZero() {
		now = time.Now()
	}

	duration, err := parseOlderThanDuration(olderThan)
	if err != nil {
		return err
	}

	var sessions sessionsListResponse
	if err := client.Get(ctx, "/runtime/sessions", &sessions); err != nil {
		return err
	}

	cutoff := now.Add(-duration)
	matches := matchingPrunableSessions(sessions.Items, cutoff)
	if dryRun {
		for _, session := range matches {
			fmt.Fprintf(stdout, "%s\t%s\t%s\n", session.ID, formatTime(session.UpdatedAt), session.Key)
		}
		fmt.Fprintf(stdout, "Would prune %d conversations\n", len(matches))
		return nil
	}

	for _, session := range matches {
		if err := client.Delete(ctx, "/runtime/sessions/"+session.ID, nil); err != nil {
			return fmt.Errorf("delete session %s: %w", session.ID, err)
		}
	}
	fmt.Fprintf(stdout, "Pruned %d conversations\n", len(matches))
	return nil
}

func parseOlderThanDuration(raw string) (time.Duration, error) {
	value := strings.ToLower(strings.TrimSpace(raw))
	if value == "" {
		return 0, fmt.Errorf("older-than duration is required")
	}

	normalized := dayDurationPattern.ReplaceAllStringFunc(value, func(match string) string {
		days, err := strconv.ParseFloat(match[:len(match)-1], 64)
		if err != nil {
			return match
		}
		hours := days * 24
		return strconv.FormatFloat(hours, 'f', -1, 64) + "h"
	})
	if strings.ContainsRune(normalized, 'd') {
		return 0, fmt.Errorf("parse older-than duration %q: unsupported day format", raw)
	}

	duration, err := time.ParseDuration(normalized)
	if err != nil {
		return 0, fmt.Errorf("parse older-than duration %q: %w", raw, err)
	}
	if duration <= 0 {
		return 0, fmt.Errorf("older-than duration must be greater than zero")
	}
	return duration, nil
}

func matchingPrunableSessions(items []sessionSummary, cutoff time.Time) []sessionSummary {
	matches := make([]sessionSummary, 0, len(items))
	for _, session := range items {
		if session.UpdatedAt.Before(cutoff) {
			matches = append(matches, session)
		}
	}
	sort.Slice(matches, func(i, j int) bool {
		if matches[i].UpdatedAt.Equal(matches[j].UpdatedAt) {
			return matches[i].ID < matches[j].ID
		}
		return matches[i].UpdatedAt.Before(matches[j].UpdatedAt)
	})
	return matches
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-3] + "..."
}

func formatTime(t time.Time) string {
	if t.IsZero() {
		return "-"
	}
	return t.Format(time.RFC3339)
}
