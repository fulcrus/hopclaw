package cli

import (
	"context"
	"fmt"
	"net/url"

	"github.com/spf13/cobra"
)

func newProjectCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "project",
		Short: "Manage projects",
		Long:  "List, show, rename, and delete registered projects.",
	}
	cmd.AddCommand(
		newProjectListCmd(),
		newProjectShowCmd(),
		newProjectRenameCmd(),
		newProjectDeleteCmd(),
	)
	return cmd
}

func newProjectListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List all known projects",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runProjectList(cmd.Context())
		},
	}
}

func runProjectList(ctx context.Context) error {
	client, err := newGatewayClient()
	if err != nil {
		return err
	}
	var result []map[string]any
	if err := client.Get(ctx, "/runtime/projects", &result); err != nil {
		return err
	}
	if flagJSON {
		return printJSON(result)
	}
	if len(result) == 0 {
		fmt.Println("No projects registered.")
		return nil
	}
	for _, p := range result {
		name, _ := p["name"].(string)
		dir, _ := p["directory"].(string)
		fmt.Printf("  %s | %s\n", name, dir)
	}
	return nil
}

func newProjectShowCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "show <name>",
		Short: "Show project details",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runProjectShow(cmd.Context(), args[0])
		},
	}
}

func runProjectShow(ctx context.Context, name string) error {
	client, err := newGatewayClient()
	if err != nil {
		return err
	}
	var result map[string]any
	if err := client.Get(ctx, "/runtime/projects/"+url.PathEscape(name), &result); err != nil {
		return err
	}
	if flagJSON {
		return printJSON(result)
	}
	fmt.Printf("Project: %s\n", result["name"])
	fmt.Printf("Directory: %s\n", result["directory"])
	if repo, ok := result["git_repo"].(string); ok && repo != "" {
		fmt.Printf("Git Repo: %s\n", repo)
	}
	return nil
}

func newProjectRenameCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "rename <old-name> <new-name>",
		Short: "Rename a project",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runProjectRename(cmd.Context(), args[0], args[1])
		},
	}
}

func runProjectRename(ctx context.Context, oldName, newName string) error {
	client, err := newGatewayClient()
	if err != nil {
		return err
	}
	body := map[string]string{"name": newName}
	if err := client.Patch(ctx, "/runtime/projects/"+url.PathEscape(oldName), body, nil); err != nil {
		return err
	}
	if flagJSON {
		return printJSON(map[string]string{"old_name": oldName, "name": newName})
	}
	fmt.Printf("✓ Project %q renamed to %q.\n", oldName, newName)
	return nil
}

func newProjectDeleteCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "delete <name>",
		Short: "Delete a project and its memories",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			confirmed, _ := cmd.Flags().GetBool("yes")
			if !confirmed {
				ok, err := confirmDestructiveAction(cmd.InOrStdin(), cmd.OutOrStdout(), fmt.Sprintf("Delete project %q and all its memories? [y/N] ", args[0]))
				if err != nil {
					return err
				}
				if !ok {
					return nil
				}
			}
			return runProjectDelete(cmd.Context(), args[0])
		},
	}
	cmd.Flags().BoolP("yes", "y", false, "Skip confirmation")
	return cmd
}

func runProjectDelete(ctx context.Context, name string) error {
	client, err := newGatewayClient()
	if err != nil {
		return err
	}
	if err := client.Delete(ctx, "/runtime/projects/"+url.PathEscape(name), nil); err != nil {
		return err
	}
	if flagJSON {
		return printJSON(map[string]string{"deleted": name})
	}
	fmt.Printf("✓ Project %q deleted.\n", name)
	return nil
}
