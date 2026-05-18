package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/fulcrus/hopclaw/backup"
	"github.com/fulcrus/hopclaw/internal/daemon"
	"github.com/spf13/cobra"
)

// ---------------------------------------------------------------------------
// Parent command
// ---------------------------------------------------------------------------

func newBackupCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "backup",
		Short: "Manage state backups",
		Long:  "Create, list, and restore snapshots of HopClaw state data.",
	}
	cmd.AddCommand(
		newBackupCreateCmd(),
		newBackupListCmd(),
		newBackupRestoreCmd(),
	)
	return cmd
}

// ---------------------------------------------------------------------------
// backup create
// ---------------------------------------------------------------------------

func newBackupCreateCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "create",
		Short: "Create a new backup",
		Long:  "Snapshot the HopClaw state directory into a timestamped tar.gz archive.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runBackupCreate(cmd.Context())
		},
	}
}

func runBackupCreate(ctx context.Context) error {
	svc := backup.NewService(daemon.StateDir())

	fmt.Println("Creating backup...")
	result, err := svc.Create(ctx)
	if err != nil {
		return fmt.Errorf("backup create: %w", err)
	}

	if flagJSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(result)
	}

	fmt.Printf("Backup created successfully\n")
	fmt.Printf("  Path:  %s\n", result.Path)
	fmt.Printf("  Size:  %s\n", formatBytes(result.Size))
	fmt.Printf("  Files: %d\n", result.FileCount)
	fmt.Printf("  Time:  %s\n", result.CreatedAt.Format(time.RFC3339))
	return nil
}

// ---------------------------------------------------------------------------
// backup list
// ---------------------------------------------------------------------------

func newBackupListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List available backups",
		Long:  "List all backup archives found in the HopClaw state directory.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runBackupList(cmd.Context())
		},
	}
}

func runBackupList(_ context.Context) error {
	svc := backup.NewService(daemon.StateDir())

	results, err := svc.List()
	if err != nil {
		return fmt.Errorf("backup list: %w", err)
	}

	if flagJSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(results)
	}

	if len(results) == 0 {
		fmt.Println("no backups found")
		return nil
	}

	fmt.Printf("%-22s  %-10s  %-6s  %s\n", "CREATED", "SIZE", "FILES", "PATH")
	fmt.Printf("%-22s  %-10s  %-6s  %s\n", "-------", "----", "-----", "----")
	for _, r := range results {
		created := "-"
		if !r.CreatedAt.IsZero() {
			created = r.CreatedAt.Format(time.RFC3339)
		}
		files := "-"
		if r.FileCount >= 0 {
			files = fmt.Sprintf("%d", r.FileCount)
		}
		fmt.Printf("%-22s  %-10s  %-6s  %s\n",
			created,
			formatBytes(r.Size),
			files,
			r.Path,
		)
	}
	fmt.Printf("\nTotal: %d backups\n", len(results))
	return nil
}

// ---------------------------------------------------------------------------
// backup restore
// ---------------------------------------------------------------------------

func newBackupRestoreCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "restore <path>",
		Short: "Restore from a backup",
		Long:  "Restore HopClaw state from a backup archive. Existing files are saved with a .bak suffix.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runBackupRestore(cmd.Context(), args[0])
		},
	}
}

func runBackupRestore(ctx context.Context, backupPath string) error {
	svc := backup.NewService(daemon.StateDir())

	fmt.Printf("Restoring from %s...\n", backupPath)
	result, err := svc.Restore(ctx, backupPath)
	if err != nil {
		return fmt.Errorf("backup restore: %w", err)
	}

	if flagJSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(result)
	}

	fmt.Printf("Restore completed successfully\n")
	fmt.Printf("  Files restored: %d\n", result.FilesRestored)
	fmt.Printf("  Source:         %s\n", result.BackupPath)
	return nil
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

const (
	bytesPerKB = 1024
	bytesPerMB = 1024 * 1024
	bytesPerGB = 1024 * 1024 * 1024
)

// formatBytes returns a human-readable string for a byte count.
func formatBytes(b int64) string {
	switch {
	case b >= bytesPerGB:
		return fmt.Sprintf("%.1f GB", float64(b)/float64(bytesPerGB))
	case b >= bytesPerMB:
		return fmt.Sprintf("%.1f MB", float64(b)/float64(bytesPerMB))
	case b >= bytesPerKB:
		return fmt.Sprintf("%.1f KB", float64(b)/float64(bytesPerKB))
	default:
		return fmt.Sprintf("%d B", b)
	}
}
