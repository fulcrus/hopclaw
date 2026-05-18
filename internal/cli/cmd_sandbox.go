package cli

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
)

// ---------------------------------------------------------------------------
// Constants
// ---------------------------------------------------------------------------

const (
	sandboxExecPath   = "/operator/sandbox/exec"
	sandboxImagesPath = "/operator/sandbox/images"
	sandboxStatusPath = "/operator/sandbox/status"

	defaultSandboxTimeout = 30
)

// ---------------------------------------------------------------------------
// Response types (mirror the gateway JSON shapes)
// ---------------------------------------------------------------------------

type sandboxExecRequest struct {
	Image   string            `json:"image,omitempty"`
	Command []string          `json:"command"`
	Stdin   string            `json:"stdin,omitempty"`
	Env     map[string]string `json:"env,omitempty"`
	Timeout int               `json:"timeout,omitempty"`
}

type sandboxExecResponse struct {
	Stdout   string `json:"stdout"`
	Stderr   string `json:"stderr"`
	ExitCode int    `json:"exit_code"`
	TimedOut bool   `json:"timed_out"`
	Duration string `json:"duration"`
}

type sandboxImagesResponse struct {
	Images []string `json:"images"`
}

type sandboxStatusResponse struct {
	Available     bool     `json:"available"`
	AllowedImages []string `json:"allowed_images,omitempty"`
	Runtime       string   `json:"runtime,omitempty"`
}

// ---------------------------------------------------------------------------
// Parent command
// ---------------------------------------------------------------------------

func newSandboxCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "sandbox",
		Short: "Manage sandbox execution",
		Long:  "Run commands in sandboxed containers, list allowed images, and check sandbox status.",
	}
	cmd.AddCommand(
		newSandboxRunCmd(),
		newSandboxImagesCmd(),
		newSandboxStatusCmd(),
	)
	return cmd
}

// ---------------------------------------------------------------------------
// sandbox run
// ---------------------------------------------------------------------------

func newSandboxRunCmd() *cobra.Command {
	var (
		image   string
		stdin   string
		env     []string
		timeout int
	)

	cmd := &cobra.Command{
		Use:   "run [flags] -- <command> [args...]",
		Short: "Run a command in a sandbox container",
		Long: `Run a command inside a sandboxed container on the running gateway.

Examples:
  hopclaw sandbox run -- echo hello
  hopclaw sandbox run --image python:3.12-slim -- python -c "print('hello')"
  hopclaw sandbox run --env KEY=value --timeout 60 -- ./my-script.sh`,
		Args:               cobra.MinimumNArgs(1),
		DisableFlagParsing: false,
		RunE: func(cmd *cobra.Command, args []string) error {
			envMap := parseSandboxEnv(env)
			return runSandboxRun(cmd.Context(), image, args, stdin, envMap, timeout)
		},
	}

	cmd.Flags().StringVar(&image, "image", "", "container image to use")
	cmd.Flags().StringVar(&stdin, "stdin", "", "string to pass as stdin")
	cmd.Flags().StringSliceVar(&env, "env", nil, "environment variables (KEY=VALUE)")
	cmd.Flags().IntVar(&timeout, "timeout", defaultSandboxTimeout, "execution timeout in seconds")

	return cmd
}

func runSandboxRun(ctx context.Context, image string, command []string, stdin string, env map[string]string, timeout int) error {
	client, err := NewGatewayClient()
	if err != nil {
		return err
	}

	reqBody := sandboxExecRequest{
		Image:   image,
		Command: command,
		Stdin:   stdin,
		Env:     env,
		Timeout: timeout,
	}

	var resp sandboxExecResponse
	if err := client.Post(ctx, sandboxExecPath, reqBody, &resp); err != nil {
		return err
	}

	if flagJSON {
		return printJSON(resp)
	}

	if resp.Stdout != "" {
		fmt.Print(resp.Stdout)
		if !strings.HasSuffix(resp.Stdout, "\n") {
			fmt.Println()
		}
	}
	if resp.Stderr != "" {
		fmt.Fprint(os.Stderr, resp.Stderr)
		if !strings.HasSuffix(resp.Stderr, "\n") {
			fmt.Fprintln(os.Stderr)
		}
	}

	if resp.TimedOut {
		fmt.Fprintf(os.Stderr, "execution timed out after %s\n", resp.Duration)
	}

	if resp.ExitCode != 0 {
		fmt.Fprintf(os.Stderr, "exit code: %d\n", resp.ExitCode)
		os.Exit(resp.ExitCode)
	}

	return nil
}

// ---------------------------------------------------------------------------
// sandbox images
// ---------------------------------------------------------------------------

func newSandboxImagesCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "images",
		Short: "List allowed sandbox images",
		Long:  "List all container images allowed for sandbox execution.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runSandboxImages(cmd.Context())
		},
	}
}

func runSandboxImages(ctx context.Context) error {
	client, err := newGatewayClient()
	if err != nil {
		return err
	}

	var resp sandboxImagesResponse
	if err := client.Get(ctx, sandboxImagesPath, &resp); err != nil {
		return err
	}

	if flagJSON {
		return printJSON(resp)
	}

	if len(resp.Images) == 0 {
		fmt.Println("no allowed images configured")
		return nil
	}

	fmt.Println("Allowed images:")
	for _, img := range resp.Images {
		fmt.Printf("  %s\n", img)
	}
	return nil
}

// ---------------------------------------------------------------------------
// sandbox status
// ---------------------------------------------------------------------------

func newSandboxStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show sandbox status",
		Long:  "Show whether the sandbox runtime (Docker) is available and current configuration.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runSandboxStatus(cmd.Context())
		},
	}
}

func runSandboxStatus(ctx context.Context) error {
	client, err := NewGatewayClient()
	if err != nil {
		return err
	}

	var resp sandboxStatusResponse
	if err := client.Get(ctx, sandboxStatusPath, &resp); err != nil {
		return err
	}

	if flagJSON {
		return printJSON(resp)
	}

	availableStr := "unavailable"
	if resp.Available {
		availableStr = "available"
	}
	fmt.Printf("Sandbox runtime: %s\n", availableStr)
	if resp.Runtime != "" {
		fmt.Printf("Runtime:         %s\n", resp.Runtime)
	}

	if len(resp.AllowedImages) > 0 {
		fmt.Printf("Allowed images:  %d\n", len(resp.AllowedImages))
		for _, img := range resp.AllowedImages {
			fmt.Printf("  %s\n", img)
		}
	}
	return nil
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// parseSandboxEnv converts a slice of "KEY=VALUE" strings into a map.
func parseSandboxEnv(envSlice []string) map[string]string {
	if len(envSlice) == 0 {
		return nil
	}
	result := make(map[string]string, len(envSlice))
	for _, entry := range envSlice {
		parts := strings.SplitN(entry, "=", 2)
		if len(parts) == 2 {
			result[parts[0]] = parts[1]
		}
	}
	return result
}
