package cli

import (
	"context"
	"fmt"
	"net/url"
	"strings"

	"github.com/fulcrus/hopclaw/agent"
	"github.com/spf13/cobra"
)

// ---------------------------------------------------------------------------
// memory constants
// ---------------------------------------------------------------------------

const (
	memoryDefaultListLimit = 50
	memoryValuePreviewLen  = 60
)

// ---------------------------------------------------------------------------
// memory command group
// ---------------------------------------------------------------------------

func newMemoryCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "memory",
		Short: "Manage agent memory (key-value store)",
		Long:  "Get, set, delete, and search agent memory entries via the running gateway.",
	}

	cmd.AddCommand(
		newMemoryGetCmd(),
		newMemorySetCmd(),
		newMemoryDeleteCmd(),
		newMemorySearchCmd(),
		newMemoryStatusCmd(),
		newMemoryIndexCmd(),
		newMemoryListCmd(),
	)

	return cmd
}

// ---------------------------------------------------------------------------
// memory get
// ---------------------------------------------------------------------------

func newMemoryGetCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "get <key>",
		Short: "Get a memory value",
		Long:  "Retrieve a memory entry by key from the agent's KV store.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runMemoryGet(cmd.Context(), args[0])
		},
	}
}

// memoryEntry mirrors agent.MemoryEntry for JSON decoding.
type memoryEntry struct {
	Key       string `json:"key"`
	Value     string `json:"value"`
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
}

func runMemoryGet(ctx context.Context, key string) error {
	client, err := newGatewayClient()
	if err != nil {
		return err
	}

	var entry memoryEntry
	if err := client.Get(ctx, "/runtime/memory/"+key, &entry); err != nil {
		return err
	}

	if flagJSON {
		return printJSON(entry)
	}

	fmt.Printf("Key:     %s\n", entry.Key)
	fmt.Printf("Value:   %s\n", entry.Value)
	if entry.UpdatedAt != "" {
		fmt.Printf("Updated: %s\n", entry.UpdatedAt)
	}
	return nil
}

// ---------------------------------------------------------------------------
// memory set
// ---------------------------------------------------------------------------

func newMemorySetCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "set [key] <value>",
		Short: "Set a memory value (highest authority, cannot be overwritten by agent)",
		Long:  "Remember a fact. Single argument auto-generates key. Use --global for cross-project memory.",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			var key, value string
			if len(args) == 1 {
				value = args[0]
			} else {
				key = args[0]
				value = strings.Join(args[1:], " ")
			}
			isGlobal, _ := cmd.Flags().GetBool("global")
			label, _ := cmd.Flags().GetString("label")

			namespace := "project"
			if isGlobal {
				namespace = "global"
			}
			return runMemorySetV2(cmd.Context(), key, value, namespace, label)
		},
	}
	cmd.Flags().Bool("global", false, "Store as global memory (visible across all projects)")
	cmd.Flags().String("label", "", "Human-readable label for display")
	return cmd
}

func runMemorySetV2(ctx context.Context, key, value, namespace, label string) error {
	client, err := newGatewayClient()
	if err != nil {
		return err
	}

	body := map[string]any{
		"value":     value,
		"source":    "user",
		"namespace": namespace,
	}
	if key != "" {
		body["key"] = key
	}
	if label != "" {
		body["label"] = label
	}

	var stored memoryEntry
	if err := client.Post(ctx, "/runtime/memory/records", body, &stored); err != nil {
		return err
	}
	if stored.Key == "" {
		stored.Key = key
	}
	if stored.Value == "" {
		stored.Value = value
	}

	if flagJSON {
		return printJSON(map[string]string{"key": stored.Key, "value": stored.Value, "status": "ok"})
	}

	displayKey := stored.Key
	if displayKey == "" {
		displayKey = "(auto)"
	}
	fmt.Printf("✓ 已记住: %s = %s\n", displayKey, stored.Value)
	return nil
}

// ---------------------------------------------------------------------------
// memory delete
// ---------------------------------------------------------------------------

func newMemoryDeleteCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "delete <key>",
		Short: "Delete a memory value",
		Long:  "Remove a key-value pair from the agent's memory store.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			confirmed, _ := cmd.Flags().GetBool("yes")
			if !confirmed {
				ok, err := confirmDestructiveAction(cmd.InOrStdin(), cmd.OutOrStdout(), fmt.Sprintf("Delete memory %q? [y/N] ", args[0]))
				if err != nil {
					return err
				}
				if !ok {
					return nil
				}
			}
			return runMemoryDelete(cmd.Context(), args[0])
		},
	}
	cmd.Flags().BoolP("yes", "y", false, "Skip confirmation")
	return cmd
}

func runMemoryDelete(ctx context.Context, key string) error {
	client, err := newGatewayClient()
	if err != nil {
		return err
	}

	if err := client.Delete(ctx, "/runtime/memory/"+key, nil); err != nil {
		return err
	}

	if flagJSON {
		return printJSON(map[string]string{"deleted": key})
	}

	fmt.Printf("deleted %s\n", key)
	return nil
}

// ---------------------------------------------------------------------------
// memory search
// ---------------------------------------------------------------------------

func newMemorySearchCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "search <query>",
		Short: "Search memory entries",
		Long:  "Search the agent's memory store for entries matching the query.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runMemorySearch(cmd.Context(), args[0])
		},
	}
}

// memorySearchResponse is the shape of GET /runtime/memory?q=query.
type memorySearchResponse struct {
	Items []memoryEntry `json:"items"`
}

func runMemorySearch(ctx context.Context, query string) error {
	client, err := newGatewayClient()
	if err != nil {
		return err
	}

	var resp memorySearchResponse
	if err := client.Get(ctx, "/runtime/memory?q="+url.QueryEscape(query), &resp); err != nil {
		return err
	}

	if flagJSON {
		return printJSON(resp)
	}

	if len(resp.Items) == 0 {
		fmt.Println("no matching entries found")
		return nil
	}

	fmt.Printf("%-24s  %s\n", "KEY", "VALUE")
	fmt.Printf("%-24s  %s\n", "---", "-----")
	for _, entry := range resp.Items {
		val := entry.Value
		if len(val) > memoryValuePreviewLen {
			val = val[:memoryValuePreviewLen] + "..."
		}
		fmt.Printf("%-24s  %s\n", truncate(entry.Key, 24), val)
	}
	return nil
}

// ---------------------------------------------------------------------------
// memory status
// ---------------------------------------------------------------------------

func newMemoryStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show memory store status",
		Long:  "Display memory store type, entry count, and index status.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runMemoryStatus(cmd.Context())
		},
	}
}

// memoryStatusResponse is the shape of GET /runtime/memory/status.
type memoryStatusResponse struct {
	StoreType  string `json:"store_type"`
	EntryCount int    `json:"entry_count"`
	IndexReady bool   `json:"index_ready"`
}

func runMemoryStatus(ctx context.Context) error {
	client, err := newGatewayClient()
	if err != nil {
		return err
	}

	var status memoryStatusResponse
	if err := client.Get(ctx, "/runtime/memory/status", &status); err != nil {
		return err
	}

	if flagJSON {
		return printJSON(status)
	}

	indexStatus := "not ready"
	if status.IndexReady {
		indexStatus = "ready"
	}

	fmt.Printf("Store type:  %s\n", status.StoreType)
	fmt.Printf("Entries:     %d\n", status.EntryCount)
	fmt.Printf("Index:       %s\n", indexStatus)
	return nil
}

// ---------------------------------------------------------------------------
// memory index
// ---------------------------------------------------------------------------

func newMemoryIndexCmd() *cobra.Command {
	var force bool

	cmd := &cobra.Command{
		Use:   "index",
		Short: "Trigger memory reindexing",
		Long:  "Rebuild the memory search index. Use --force to rebuild from scratch.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runMemoryIndex(cmd.Context(), force)
		},
	}

	cmd.Flags().BoolVar(&force, "force", false, "force full reindex from scratch")

	return cmd
}

// memoryIndexRequest is the body for POST /runtime/memory/index.
type memoryIndexRequest struct {
	Force bool `json:"force"`
}

// memoryIndexResponse is the shape of POST /runtime/memory/index.
type memoryIndexResponse struct {
	Status  string `json:"status"`
	Indexed int    `json:"indexed"`
}

func runMemoryIndex(ctx context.Context, force bool) error {
	client, err := newGatewayClient()
	if err != nil {
		return err
	}

	body := memoryIndexRequest{Force: force}
	var resp memoryIndexResponse
	if err := client.Post(ctx, "/runtime/memory/index", body, &resp); err != nil {
		return fmt.Errorf("trigger reindex: %w", err)
	}

	if flagJSON {
		return printJSON(resp)
	}

	fmt.Printf("reindex %s: %d entries indexed\n", resp.Status, resp.Indexed)
	return nil
}

// ---------------------------------------------------------------------------
// memory list
// ---------------------------------------------------------------------------

func newMemoryListCmd() *cobra.Command {
	var limit int

	cmd := &cobra.Command{
		Use:   "list",
		Short: "List all memory entries",
		Long:  "List all memory keys with value previews.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runMemoryList(cmd.Context(), limit)
		},
	}

	cmd.Flags().IntVar(&limit, "limit", memoryDefaultListLimit, "max number of entries to list")

	return cmd
}

// memoryListResponse is the shape of GET /runtime/memory?limit=N.
type memoryListResponse struct {
	Items []memoryEntry `json:"items"`
	Count int           `json:"count"`
}

func runMemoryList(ctx context.Context, limit int) error {
	client, err := NewGatewayClient()
	if err != nil {
		return err
	}

	path := fmt.Sprintf("/runtime/memory?limit=%d", limit)
	var resp memoryListResponse
	if err := client.Get(ctx, path, &resp); err != nil {
		return err
	}

	if flagJSON {
		return printJSON(resp)
	}

	if len(resp.Items) == 0 {
		fmt.Println("no memory entries found")
		return nil
	}

	fmt.Printf("%-24s  %s\n", "KEY", "VALUE")
	fmt.Printf("%-24s  %s\n", "---", "-----")
	for _, entry := range resp.Items {
		val := entry.Value
		if len(val) > memoryValuePreviewLen {
			val = val[:memoryValuePreviewLen] + "..."
		}
		fmt.Printf("%-24s  %s\n", truncate(entry.Key, 24), val)
	}

	fmt.Printf("\nTotal: %d entries\n", resp.Count)
	return nil
}

func listMemoryEntries(ctx context.Context, client *GatewayClient, query string, limit int) ([]agent.MemoryEntry, error) {
	if client == nil {
		return nil, fmt.Errorf("gateway client is required")
	}
	if limit <= 0 {
		limit = memoryDefaultListLimit
	}
	values := url.Values{}
	values.Set("limit", fmt.Sprintf("%d", limit))
	if trimmed := strings.TrimSpace(query); trimmed != "" {
		values.Set("q", trimmed)
	}
	var resp struct {
		Items []agent.MemoryEntry `json:"items"`
		Count int                 `json:"count"`
	}
	if err := client.Get(ctx, "/runtime/memory?"+values.Encode(), &resp); err != nil {
		return nil, err
	}
	return resp.Items, nil
}

func getMemoryEntry(ctx context.Context, client *GatewayClient, key string) (*agent.MemoryEntry, error) {
	if client == nil {
		return nil, fmt.Errorf("gateway client is required")
	}
	var entry agent.MemoryEntry
	if err := client.Get(ctx, "/runtime/memory/"+url.PathEscape(strings.TrimSpace(key)), &entry); err != nil {
		return nil, err
	}
	return &entry, nil
}

func upsertMemoryEntry(ctx context.Context, client *GatewayClient, key, value, label, sessionKey, projectID string) (*agent.MemoryEntry, error) {
	if client == nil {
		return nil, fmt.Errorf("gateway client is required")
	}
	body := map[string]any{
		"value":       strings.TrimSpace(value),
		"label":       strings.TrimSpace(label),
		"source":      "user",
		"namespace":   "project",
		"session_key": strings.TrimSpace(sessionKey),
		"project_id":  strings.TrimSpace(projectID),
	}
	if trimmedKey := strings.TrimSpace(key); trimmedKey != "" {
		body["key"] = trimmedKey
	}
	var stored agent.MemoryEntry
	if err := client.Post(ctx, "/runtime/memory/records", body, &stored); err != nil {
		return nil, err
	}
	return &stored, nil
}

func deleteMemoryEntry(ctx context.Context, client *GatewayClient, key string) error {
	if client == nil {
		return fmt.Errorf("gateway client is required")
	}
	return client.Delete(ctx, "/runtime/memory/"+url.PathEscape(strings.TrimSpace(key)), nil)
}
