package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/fulcrus/hopclaw/contextengine"
	"github.com/fulcrus/hopclaw/internal/cli/imageinput"
	replpkg "github.com/fulcrus/hopclaw/internal/cli/repl"
	"github.com/spf13/cobra"
)

// ---------------------------------------------------------------------------
// message command group
// ---------------------------------------------------------------------------

const (
	messagePollInterval       = 1 * time.Second
	messageDefaultLimit       = 20
	messageDefaultSession     = "cli"
	messageDefaultChannel     = "cli"
	messageSearchDefaultLimit = 20
	messageContentPreviewLen  = 60
	messageContentDisplayLen  = 200
	messageErrorDisplayLen    = 40

	// Run status values (mirror agent.RunStatus for API responses)
	runStatusFailed    = "failed"
	runStatusCancelled = "cancelled"
)

func newMessageCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "message",
		Short: "Scripted message and run operations",
		Long: "Use the default `hopclaw` command for the interactive terminal or one-shot asks. " +
			"Use `hopclaw message ...` when you need explicit session/run messaging operations in scripts or tooling.",
	}

	cmd.AddCommand(
		newMessageSendCmd(),
		newMessageListCmd(),
		newMessageReadCmd(),
		newMessageEditCmd(),
		newMessageDeleteCmd(),
		newMessageSearchCmd(),
		newMessageBroadcastCmd(),
		newMessageReactCmd(),
		newMessageThreadCmd(),
	)

	return cmd
}

// ---------------------------------------------------------------------------
// message send
// ---------------------------------------------------------------------------

func newMessageSendCmd() *cobra.Command {
	var sessionKey string
	var channel string
	var imagePaths []string

	cmd := &cobra.Command{
		Use:   "send [flags] <message>",
		Short: "Send a scripted message to the agent",
		Long: "Submit a message, wait for the run to complete, and print the response. " +
			"For ad-hoc asks, prefer `hopclaw \"...\"`.",
		Example: strings.Join([]string{
			"  hopclaw \"summarize the latest run\"",
			"    Preferred for ad-hoc one-shot asks",
			"",
			"  hopclaw message send --session-key ops \"summarize the latest run\"",
			"    Use when you need an explicit session key in scripts",
		}, "\n"),
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runMessageSend(cmd.Context(), sessionKey, channel, args[0], imagePaths)
		},
	}

	cmd.Flags().StringVar(&sessionKey, "session-key", messageDefaultSession, "session key for the conversation")
	cmd.Flags().StringVar(&channel, "channel", messageDefaultChannel, "channel identifier")
	cmd.Flags().StringSliceVar(&imagePaths, "image", nil, "Image file path(s) to attach")

	return cmd
}

// messageSendRequest is the body for POST /runtime/runs.
type messageSendRequest struct {
	SessionKey string         `json:"session_key"`
	Content    string         `json:"content"`
	Images     []string       `json:"images,omitempty"`
	Metadata   map[string]any `json:"metadata,omitempty"`
}

// messageRunResponse is the shape of a run returned by the API.
type messageRunResponse struct {
	ID        string `json:"id"`
	SessionID string `json:"session_id"`
	Status    string `json:"status"`
	Phase     string `json:"phase"`
	Model     string `json:"model,omitempty"`
	Error     string `json:"error,omitempty"`
	CreatedAt string `json:"created_at,omitempty"`
}

// messageSessionResponse is the shape of a session returned by the API.
type messageSessionResponse struct {
	ID       string                  `json:"id"`
	Key      string                  `json:"key"`
	Messages []contextengine.Message `json:"messages,omitempty"`
}

type messageCompletionResponse struct {
	Result *messageRunResultView `json:"result,omitempty"`
	Bundle *messageResultBundle  `json:"bundle,omitempty"`
}

type messageRunResultView struct {
	Output string               `json:"output,omitempty"`
	Bundle *messageResultBundle `json:"bundle,omitempty"`
}

type messageResultBundle struct {
	FinalText string `json:"final_text,omitempty"`
}

func runMessageSend(ctx context.Context, sessionKey, channel, content string, imagePaths []string) error {
	client, err := newGatewayClient()
	if err != nil {
		return err
	}
	return runMessageSendWithClient(ctx, client, sessionKey, channel, content, imagePaths)
}

func runMessageSendWithClient(ctx context.Context, client *GatewayClient, sessionKey, channel, content string, imagePaths []string) error {
	// Build effective session key.
	effectiveKey := sessionKey
	if channel != "" && !strings.Contains(sessionKey, ":") {
		effectiveKey = channel + ":" + sessionKey
	}

	encodedImages := make([]string, 0, len(imagePaths))
	for _, path := range imagePaths {
		dataURI, err := imageinput.EncodeFileAsDataURI(path)
		if err != nil {
			return err
		}
		encodedImages = append(encodedImages, dataURI)
	}

	// Submit the run.
	reqBody := messageSendRequest{
		SessionKey: effectiveKey,
		Content:    content,
		Images:     encodedImages,
		Metadata:   scriptedMessageSubmitMetadata(channel),
	}
	var run messageRunResponse
	if err := client.Post(ctx, "/runtime/runs", reqBody, &run); err != nil {
		return fmt.Errorf("submit message: %w", err)
	}

	if flagVerbose {
		fmt.Fprintf(os.Stderr, "run %s submitted (session %s)\n", run.ID, run.SessionID)
	}

	// Poll until terminal status.
	for {
		switch run.Status {
		case "completed", "failed", "cancelled":
			return printMessageResult(ctx, client, run)
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(messagePollInterval):
		}

		if err := client.Get(ctx, "/runtime/runs/"+run.ID, &run); err != nil {
			return fmt.Errorf("poll run %s: %w", run.ID, err)
		}

		if flagVerbose {
			fmt.Fprintf(os.Stderr, "run %s: %s (%s)\n", run.ID, run.Status, run.Phase)
		}
	}
}

func scriptedMessageSubmitMetadata(channel string) map[string]any {
	switch strings.ToLower(strings.TrimSpace(channel)) {
	case "", messageDefaultChannel:
		return cliRequestMetadata()
	default:
		return nil
	}
}

func printMessageResult(ctx context.Context, client *GatewayClient, run messageRunResponse) error {
	if run.Status == runStatusFailed {
		if flagJSON {
			return printJSON(run)
		}
		return fmt.Errorf("run %s failed: %s", run.ID, run.Error)
	}
	if run.Status == runStatusCancelled {
		if flagJSON {
			return printJSON(run)
		}
		return fmt.Errorf("run %s was cancelled", run.ID)
	}

	var completion messageCompletionResponse
	completionErr := client.Get(ctx, "/runtime/runs/"+run.ID+"/completion", &completion)
	if completionErr == nil {
		if flagJSON {
			return printJSON(completion)
		}
		if text := messageCompletionText(completion); text != "" {
			fmt.Println(text)
			return nil
		}
	}

	session, sessionErr := fetchMessageSession(ctx, client, run.SessionID, true)
	if sessionErr != nil {
		if completionErr != nil {
			return fmt.Errorf("fetch run completion: %w", completionErr)
		}
		return fmt.Errorf("fetch session: %w", sessionErr)
	}

	if flagJSON {
		return printJSON(session)
	}

	// Find the last assistant message.
	for i := len(session.Messages) - 1; i >= 0; i-- {
		msg := session.Messages[i]
		if msg.Role == contextengine.RoleAssistant && strings.TrimSpace(msg.TextContent()) != "" {
			fmt.Println(msg.TextContent())
			return nil
		}
	}

	fmt.Println("(no response)")
	return nil
}

// ---------------------------------------------------------------------------
// message list
// ---------------------------------------------------------------------------

func newMessageListCmd() *cobra.Command {
	var sessionKey string
	var limit int

	cmd := &cobra.Command{
		Use:   "list",
		Short: "List recent runs",
		Long:  "List recent runs, optionally filtered by session key.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runMessageList(cmd.Context(), sessionKey, limit)
		},
	}

	cmd.Flags().StringVar(&sessionKey, "session-key", "", "filter by session key")
	cmd.Flags().IntVar(&limit, "limit", messageDefaultLimit, "max number of runs to list")

	return cmd
}

// messageListResponse is the shape of GET /runtime/runs.
type messageListResponse struct {
	Items []messageRunResponse `json:"items"`
	Count int                  `json:"count"`
}

func runMessageList(ctx context.Context, sessionKey string, limit int) error {
	client, err := newGatewayClient()
	if err != nil {
		return err
	}

	path := fmt.Sprintf("/runtime/runs?limit=%d", limit)
	if sessionKey != "" {
		path += "&session_id=" + sessionKey
	}

	var resp messageListResponse
	if err := client.Get(ctx, path, &resp); err != nil {
		return err
	}

	if flagJSON {
		return printJSON(resp)
	}

	if len(resp.Items) == 0 {
		fmt.Println("no runs found")
		return nil
	}

	// Print table header.
	fmt.Printf("%-12s  %-12s  %-18s  %-8s  %s\n", "ID", "SESSION", "STATUS", "PHASE", "ERROR")
	fmt.Printf("%-12s  %-12s  %-18s  %-8s  %s\n", "---", "-------", "------", "-----", "-----")
	for _, run := range resp.Items {
		errStr := run.Error
		if len(errStr) > messageErrorDisplayLen {
			errStr = errStr[:messageErrorDisplayLen] + "..."
		}
		fmt.Printf("%-12s  %-12s  %-18s  %-8s  %s\n", run.ID, run.SessionID, run.Status, run.Phase, errStr)
	}
	return nil
}

func fetchRunSummaries(ctx context.Context, client *GatewayClient, sessionID string, limit int) ([]replpkg.RunSummary, error) {
	if client == nil {
		return nil, fmt.Errorf("gateway client is required")
	}
	if limit <= 0 {
		limit = messageDefaultLimit
	}

	path := fmt.Sprintf("/runtime/runs?limit=%d", limit)
	if trimmed := strings.TrimSpace(sessionID); trimmed != "" {
		path += "&session_id=" + trimmed
	}

	var resp messageListResponse
	if err := client.Get(ctx, path, &resp); err != nil {
		return nil, err
	}

	items := make([]replpkg.RunSummary, 0, len(resp.Items))
	for _, run := range resp.Items {
		items = append(items, replpkg.RunSummary{
			ID:        strings.TrimSpace(run.ID),
			SessionID: strings.TrimSpace(run.SessionID),
			Status:    strings.TrimSpace(run.Status),
			Phase:     strings.TrimSpace(run.Phase),
			Model:     strings.TrimSpace(run.Model),
			Error:     strings.TrimSpace(run.Error),
			CreatedAt: strings.TrimSpace(run.CreatedAt),
		})
	}
	return items, nil
}

func fetchRunDetail(ctx context.Context, client *GatewayClient, runID string) (*replpkg.RunDetail, error) {
	if client == nil {
		return nil, fmt.Errorf("gateway client is required")
	}

	var run messageRunResponse
	if err := client.Get(ctx, "/runtime/runs/"+strings.TrimSpace(runID), &run); err != nil {
		return nil, err
	}

	output, err := loadRunCompletionText(ctx, client, runID)
	if err != nil {
		output = ""
	}

	return &replpkg.RunDetail{
		Run: replpkg.RunSummary{
			ID:        strings.TrimSpace(run.ID),
			SessionID: strings.TrimSpace(run.SessionID),
			Status:    strings.TrimSpace(run.Status),
			Phase:     strings.TrimSpace(run.Phase),
			Model:     strings.TrimSpace(run.Model),
			Error:     strings.TrimSpace(run.Error),
			CreatedAt: strings.TrimSpace(run.CreatedAt),
		},
		Output: strings.TrimSpace(output),
	}, nil
}

// ---------------------------------------------------------------------------
// message read
// ---------------------------------------------------------------------------

func newMessageReadCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "read <session-id>",
		Short: "Read all messages in a session",
		Long:  "Show all messages in a session in chronological order.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runMessageRead(cmd.Context(), args[0])
		},
	}
}

func runMessageRead(ctx context.Context, sessionID string) error {
	client, err := newGatewayClient()
	if err != nil {
		return err
	}

	session, err := fetchMessageSession(ctx, client, sessionID, true)
	if err != nil {
		return fmt.Errorf("fetch session %s: %w", sessionID, err)
	}

	if flagJSON {
		return printJSON(session)
	}

	if len(session.Messages) == 0 {
		fmt.Printf("session %s has no messages\n", sessionID)
		return nil
	}

	fmt.Printf("Session: %s (key: %s)\n\n", session.ID, session.Key)
	for i, msg := range session.Messages {
		ts := ""
		if !msg.CreatedAt.IsZero() {
			ts = " (" + formatTime(msg.CreatedAt) + ")"
		}
		fmt.Printf("[%d] %s%s:\n%s\n\n", i+1, msg.Role, ts, msg.TextContent())
	}
	return nil
}

// ---------------------------------------------------------------------------
// message edit
// ---------------------------------------------------------------------------

func newMessageEditCmd() *cobra.Command {
	var content string

	cmd := &cobra.Command{
		Use:   "edit <run-id>",
		Short: "Edit a message's content",
		Long:  "Update the content of an existing run/message.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if content == "" {
				return fmt.Errorf("--content is required")
			}
			return runMessageEdit(cmd.Context(), args[0], content)
		},
	}

	cmd.Flags().StringVar(&content, "content", "", "new message content")

	return cmd
}

// messageEditRequest is the body for PATCH /runtime/runs/{id}.
type messageEditRequest struct {
	Content string `json:"content"`
}

func runMessageEdit(ctx context.Context, runID, content string) error {
	client, err := newGatewayClient()
	if err != nil {
		return err
	}

	body := messageEditRequest{Content: content}
	var run messageRunResponse
	if err := client.Patch(ctx, "/runtime/runs/"+runID, body, &run); err != nil {
		return fmt.Errorf("edit run %s: %w", runID, err)
	}

	if flagJSON {
		return printJSON(run)
	}

	fmt.Printf("updated run %s\n", runID)
	return nil
}

// ---------------------------------------------------------------------------
// message delete
// ---------------------------------------------------------------------------

func newMessageDeleteCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "delete <run-id>",
		Short: "Delete a message/run",
		Long:  "Delete a run and its associated messages.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runMessageDelete(cmd.Context(), args[0])
		},
	}
}

func runMessageDelete(ctx context.Context, runID string) error {
	client, err := newGatewayClient()
	if err != nil {
		return err
	}

	if err := client.Delete(ctx, "/runtime/runs/"+runID, nil); err != nil {
		return fmt.Errorf("delete run %s: %w", runID, err)
	}

	if flagJSON {
		return printJSON(messageDeleteResponse{Deleted: runID})
	}

	fmt.Printf("deleted run %s\n", runID)
	return nil
}

// messageDeleteResponse is the confirmation shape for delete operations.
type messageDeleteResponse struct {
	Deleted string `json:"deleted"`
}

// ---------------------------------------------------------------------------
// message search
// ---------------------------------------------------------------------------

func newMessageSearchCmd() *cobra.Command {
	var limit int
	var sessionKey string

	cmd := &cobra.Command{
		Use:   "search <query>",
		Short: "Search messages across runs",
		Long:  "Search for runs matching a query, optionally filtered by session.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runMessageSearch(cmd.Context(), args[0], limit, sessionKey)
		},
	}

	cmd.Flags().IntVar(&limit, "limit", messageSearchDefaultLimit, "max number of results")
	cmd.Flags().StringVar(&sessionKey, "session-key", "", "filter by session key")

	return cmd
}

// messageSearchRunResponse extends messageRunResponse with a content field.
type messageSearchRunResponse struct {
	ID        string `json:"id"`
	SessionID string `json:"session_id"`
	Status    string `json:"status"`
	Content   string `json:"content,omitempty"`
}

// messageSearchResponse is the shape of GET /runtime/runs?q=query.
type messageSearchResponse struct {
	Items []messageSearchRunResponse `json:"items"`
	Count int                        `json:"count"`
}

func runMessageSearch(ctx context.Context, query string, limit int, sessionKey string) error {
	client, err := newGatewayClient()
	if err != nil {
		return err
	}

	path := fmt.Sprintf("/runtime/runs?q=%s&limit=%d", query, limit)
	if sessionKey != "" {
		path += "&session_id=" + sessionKey
	}

	var resp messageSearchResponse
	if err := client.Get(ctx, path, &resp); err != nil {
		return err
	}

	if flagJSON {
		return printJSON(resp)
	}

	if len(resp.Items) == 0 {
		fmt.Printf("no runs matching %q\n", query)
		return nil
	}

	fmt.Printf("%-12s  %-12s  %-10s  %s\n", "ID", "SESSION", "STATUS", "CONTENT")
	fmt.Printf("%-12s  %-12s  %-10s  %s\n", "---", "-------", "------", "-------")
	for _, run := range resp.Items {
		content := run.Content
		if len(content) > messageContentPreviewLen {
			content = content[:messageContentPreviewLen] + "..."
		}
		fmt.Printf("%-12s  %-12s  %-10s  %s\n", run.ID, run.SessionID, run.Status, content)
	}

	fmt.Printf("\nMatched: %d runs\n", resp.Count)
	return nil
}

// ---------------------------------------------------------------------------
// message broadcast
// ---------------------------------------------------------------------------

func newMessageBroadcastCmd() *cobra.Command {
	var channels string
	var content string

	cmd := &cobra.Command{
		Use:   "broadcast",
		Short: "Broadcast a message to multiple channels",
		Long:  "Send the same message to multiple channels by posting a run for each.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if channels == "" {
				return fmt.Errorf("--channels is required")
			}
			if content == "" {
				return fmt.Errorf("--content is required")
			}
			return runMessageBroadcast(cmd.Context(), channels, content)
		},
	}

	cmd.Flags().StringVar(&channels, "channels", "", "comma-separated channel names")
	cmd.Flags().StringVar(&content, "content", "", "message content to broadcast")

	return cmd
}

// messageBroadcastResult tracks the outcome per channel.
type messageBroadcastResult struct {
	Channel string `json:"channel"`
	RunID   string `json:"run_id,omitempty"`
	Status  string `json:"status"`
	Error   string `json:"error,omitempty"`
}

// messageBroadcastResponse is the aggregated result of a broadcast.
type messageBroadcastResponse struct {
	Results []messageBroadcastResult `json:"results"`
}

func runMessageBroadcast(ctx context.Context, channelsStr, content string) error {
	client, err := newGatewayClient()
	if err != nil {
		return err
	}

	channelList := strings.Split(channelsStr, ",")
	var results []messageBroadcastResult

	for _, ch := range channelList {
		ch = strings.TrimSpace(ch)
		if ch == "" {
			continue
		}

		reqBody := messageSendRequest{
			SessionKey: ch + ":" + messageDefaultSession,
			Content:    content,
		}

		var run messageRunResponse
		if err := client.Post(ctx, "/runtime/runs", reqBody, &run); err != nil {
			results = append(results, messageBroadcastResult{
				Channel: ch,
				Status:  runStatusFailed,
				Error:   err.Error(),
			})
			continue
		}

		results = append(results, messageBroadcastResult{
			Channel: ch,
			RunID:   run.ID,
			Status:  "submitted",
		})
	}

	resp := messageBroadcastResponse{Results: results}

	if flagJSON {
		return printJSON(resp)
	}

	fmt.Printf("%-16s  %-12s  %s\n", "CHANNEL", "RUN", "STATUS")
	fmt.Printf("%-16s  %-12s  %s\n", "-------", "---", "------")
	for _, r := range results {
		errStr := ""
		if r.Error != "" {
			errStr = " (" + r.Error + ")"
		}
		fmt.Printf("%-16s  %-12s  %s%s\n", r.Channel, r.RunID, r.Status, errStr)
	}
	return nil
}

// ---------------------------------------------------------------------------
// message react
// ---------------------------------------------------------------------------

func newMessageReactCmd() *cobra.Command {
	var emoji string

	cmd := &cobra.Command{
		Use:   "react <run-id>",
		Short: "React to a message with an emoji",
		Long:  "Add an emoji reaction to a run/message.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if emoji == "" {
				return fmt.Errorf("--emoji is required")
			}
			return runMessageReact(cmd.Context(), args[0], emoji)
		},
	}

	cmd.Flags().StringVar(&emoji, "emoji", "", "emoji name for the reaction")

	return cmd
}

// messageReactRequest is the body for POST /runtime/runs/{id}/react.
type messageReactRequest struct {
	Emoji string `json:"emoji"`
}

func runMessageReact(ctx context.Context, runID, emoji string) error {
	client, err := newGatewayClient()
	if err != nil {
		return err
	}

	body := messageReactRequest{Emoji: emoji}
	if err := client.Post(ctx, "/runtime/runs/"+runID+"/react", body, nil); err != nil {
		return fmt.Errorf("react to run %s: %w", runID, err)
	}

	if flagJSON {
		return printJSON(messageReactResponse{RunID: runID, Emoji: emoji})
	}

	fmt.Printf("reacted to run %s with :%s:\n", runID, emoji)
	return nil
}

// messageReactResponse is the confirmation shape for react operations.
type messageReactResponse struct {
	RunID string `json:"run_id"`
	Emoji string `json:"emoji"`
}

// ---------------------------------------------------------------------------
// message thread
// ---------------------------------------------------------------------------

func newMessageThreadCmd() *cobra.Command {
	var content string

	cmd := &cobra.Command{
		Use:   "thread <session-id>",
		Short: "Reply in a session thread",
		Long:  "Send a threaded reply within an existing session.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if content == "" {
				return fmt.Errorf("--content is required")
			}
			return runMessageThread(cmd.Context(), args[0], content)
		},
	}

	cmd.Flags().StringVar(&content, "content", "", "reply content")

	return cmd
}

// messageThreadRequest is the body for POST /runtime/runs (threaded).
type messageThreadRequest struct {
	SessionKey string `json:"session_key"`
	Content    string `json:"content"`
	ThreadID   string `json:"thread_id"`
}

func runMessageThread(ctx context.Context, sessionID, content string) error {
	client, err := newGatewayClient()
	if err != nil {
		return err
	}

	// Fetch the session to resolve its key.
	session, err := fetchMessageSession(ctx, client, sessionID, false)
	if err != nil {
		return fmt.Errorf("fetch session %s: %w", sessionID, err)
	}

	body := messageThreadRequest{
		SessionKey: session.Key,
		Content:    content,
		ThreadID:   sessionID,
	}

	var run messageRunResponse
	if err := client.Post(ctx, "/runtime/runs", body, &run); err != nil {
		return fmt.Errorf("submit thread reply: %w", err)
	}

	if flagJSON {
		return printJSON(run)
	}

	fmt.Printf("thread reply submitted as run %s in session %s\n", run.ID, run.SessionID)
	return nil
}

func fetchMessageSession(ctx context.Context, client *GatewayClient, sessionID string, includeMessages bool) (messageSessionResponse, error) {
	path := "/runtime/sessions/" + sessionID
	if includeMessages {
		path += "?include=messages"
	}
	var session messageSessionResponse
	if err := client.Get(ctx, path, &session); err != nil {
		return messageSessionResponse{}, err
	}
	return session, nil
}

func messageCompletionText(completion messageCompletionResponse) string {
	if completion.Bundle != nil && strings.TrimSpace(completion.Bundle.FinalText) != "" {
		return strings.TrimSpace(completion.Bundle.FinalText)
	}
	if completion.Result == nil {
		return ""
	}
	if completion.Result.Bundle != nil && strings.TrimSpace(completion.Result.Bundle.FinalText) != "" {
		return strings.TrimSpace(completion.Result.Bundle.FinalText)
	}
	return strings.TrimSpace(completion.Result.Output)
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

func printJSON(v any) error {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}
