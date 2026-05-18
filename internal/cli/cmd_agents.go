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
	agentsBasePath     = "/operator/agents"
	agentsDescMaxLen   = 40
	agentsPromptMaxLen = 200
)

// ---------------------------------------------------------------------------
// Response types (mirror the API JSON shapes)
// ---------------------------------------------------------------------------

type agentCLIProfile struct {
	Name         string   `json:"name"`
	Description  string   `json:"description,omitempty"`
	SystemPrompt string   `json:"system_prompt,omitempty"`
	Model        string   `json:"model,omitempty"`
	Tools        []string `json:"tools,omitempty"`
	Skills       []string `json:"skills,omitempty"`
	MaxTokens    int      `json:"max_tokens,omitempty"`
}

type agentCLIListResponse struct {
	Items []agentCLIProfile `json:"items"`
	Count int               `json:"count"`
}

type agentCLIGetResponse struct {
	Agent agentCLIProfile `json:"agent"`
}

type agentAddRequest struct {
	Name         string   `json:"name"`
	Description  string   `json:"description,omitempty"`
	Model        string   `json:"model,omitempty"`
	SystemPrompt string   `json:"system_prompt,omitempty"`
	Tools        []string `json:"tools,omitempty"`
	Skills       []string `json:"skills,omitempty"`
	MaxTokens    int      `json:"max_tokens,omitempty"`
}

type agentAddResponse struct {
	OK   bool   `json:"ok"`
	Name string `json:"name"`
}

type agentDeleteResponse struct {
	OK   bool   `json:"ok"`
	Name string `json:"name"`
}

type agentBindRequest struct {
	Channel    string `json:"channel"`
	SessionKey string `json:"session_key,omitempty"`
}

type agentBindResponse struct {
	OK      bool   `json:"ok"`
	Agent   string `json:"agent"`
	Channel string `json:"channel"`
}

// ---------------------------------------------------------------------------
// Parent command
// ---------------------------------------------------------------------------

func newAgentsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "agents",
		Short: "Manage agent profiles",
		Long:  "List, inspect, add, delete, and bind agent profiles on the running gateway.",
	}

	cmd.AddCommand(
		newAgentsListCmd(),
		newAgentsGetCmd(),
		newAgentsAddCmd(),
		newAgentsDeleteCmd(),
		newAgentsBindCmd(),
	)

	return cmd
}

// ---------------------------------------------------------------------------
// agents list
// ---------------------------------------------------------------------------

func newAgentsListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List agent profiles",
		Long:  "List all registered agent profiles from the running gateway.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runAgentsList(cmd.Context())
		},
	}
}

func runAgentsList(ctx context.Context) error {
	client, err := NewGatewayClient()
	if err != nil {
		return err
	}

	var resp agentCLIListResponse
	if err := client.Get(ctx, agentsBasePath, &resp); err != nil {
		return err
	}

	if flagJSON {
		return printJSON(resp)
	}

	if len(resp.Items) == 0 {
		fmt.Println("no agent profiles found")
		return nil
	}

	fmt.Printf("%-20s  %-24s  %-6s  %-6s  %s\n",
		"NAME", "MODEL", "TOOLS", "SKILLS", "DESCRIPTION")
	fmt.Printf("%-20s  %-24s  %-6s  %-6s  %s\n",
		"----", "-----", "-----", "------", "-----------")
	for _, p := range resp.Items {
		model := p.Model
		if model == "" {
			model = "-"
		}
		desc := p.Description
		if len(desc) > agentsDescMaxLen {
			desc = desc[:agentsDescMaxLen] + "..."
		}
		fmt.Printf("%-20s  %-24s  %-6d  %-6d  %s\n",
			truncate(p.Name, 20),
			truncate(model, 24),
			len(p.Tools),
			len(p.Skills),
			desc,
		)
	}

	fmt.Printf("\nTotal: %d profiles\n", resp.Count)
	return nil
}

// ---------------------------------------------------------------------------
// agents get
// ---------------------------------------------------------------------------

func newAgentsGetCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "get <name>",
		Short: "Get agent profile details",
		Long:  "Get full details for a single agent profile by name.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runAgentsGet(cmd.Context(), args[0])
		},
	}
}

func runAgentsGet(ctx context.Context, name string) error {
	client, err := NewGatewayClient()
	if err != nil {
		return err
	}

	var resp agentCLIGetResponse
	if err := client.Get(ctx, agentsBasePath+"/"+name, &resp); err != nil {
		return err
	}

	if flagJSON {
		return printJSON(resp)
	}

	p := resp.Agent
	fmt.Printf("Name:        %s\n", p.Name)
	if p.Description != "" {
		fmt.Printf("Description: %s\n", p.Description)
	}
	if p.Model != "" {
		fmt.Printf("Model:       %s\n", p.Model)
	}
	if p.MaxTokens > 0 {
		fmt.Printf("Max Tokens:  %d\n", p.MaxTokens)
	}

	if len(p.Tools) > 0 {
		fmt.Printf("Tools:       %s\n", strings.Join(p.Tools, ", "))
	}
	if len(p.Skills) > 0 {
		fmt.Printf("Skills:      %s\n", strings.Join(p.Skills, ", "))
	}

	if p.SystemPrompt != "" {
		prompt := p.SystemPrompt
		if len(prompt) > agentsPromptMaxLen {
			prompt = prompt[:agentsPromptMaxLen] + "..."
		}
		fmt.Printf("\nSystem Prompt:\n  %s\n", prompt)
	}

	return nil
}

// ---------------------------------------------------------------------------
// agents add
// ---------------------------------------------------------------------------

func newAgentsAddCmd() *cobra.Command {
	var (
		model        string
		systemPrompt string
		description  string
		tools        string
		skills       string
		maxTokens    int
	)

	cmd := &cobra.Command{
		Use:   "add <name>",
		Short: "Add an agent profile",
		Long:  "Add a new agent profile to the running gateway.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			var toolsList []string
			if t := strings.TrimSpace(tools); t != "" {
				toolsList = strings.Split(t, ",")
			}
			var skillsList []string
			if s := strings.TrimSpace(skills); s != "" {
				skillsList = strings.Split(s, ",")
			}

			req := agentAddRequest{
				Name:         args[0],
				Description:  description,
				Model:        model,
				SystemPrompt: systemPrompt,
				Tools:        toolsList,
				Skills:       skillsList,
				MaxTokens:    maxTokens,
			}
			return runAgentsAdd(cmd.Context(), req)
		},
	}

	cmd.Flags().StringVar(&model, "model", "", "model to use for this agent")
	cmd.Flags().StringVar(&systemPrompt, "system-prompt", "", "system prompt for the agent")
	cmd.Flags().StringVar(&description, "description", "", "description of the agent")
	cmd.Flags().StringVar(&tools, "tools", "", "comma-separated list of tool names")
	cmd.Flags().StringVar(&skills, "skills", "", "comma-separated list of skill names")
	cmd.Flags().IntVar(&maxTokens, "max-tokens", 0, "maximum tokens for model responses")

	return cmd
}

func runAgentsAdd(ctx context.Context, req agentAddRequest) error {
	client, err := NewGatewayClient()
	if err != nil {
		return err
	}

	var resp agentAddResponse
	if err := client.Post(ctx, agentsBasePath, req, &resp); err != nil {
		return err
	}

	if flagJSON {
		return printJSON(resp)
	}

	fmt.Printf("Added agent profile %q\n", req.Name)
	return nil
}

// ---------------------------------------------------------------------------
// agents delete
// ---------------------------------------------------------------------------

func newAgentsDeleteCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "delete <name>",
		Short: "Delete an agent profile",
		Long:  "Delete an agent profile by name from the running gateway.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runAgentsDelete(cmd.Context(), args[0])
		},
	}
}

func runAgentsDelete(ctx context.Context, name string) error {
	client, err := NewGatewayClient()
	if err != nil {
		return err
	}

	var resp agentDeleteResponse
	if err := client.Delete(ctx, agentsBasePath+"/"+name, &resp); err != nil {
		return err
	}

	if flagJSON {
		return printJSON(resp)
	}

	fmt.Printf("Deleted agent profile %q\n", name)
	return nil
}

// ---------------------------------------------------------------------------
// agents bind
// ---------------------------------------------------------------------------

func newAgentsBindCmd() *cobra.Command {
	var (
		channel    string
		sessionKey string
	)

	cmd := &cobra.Command{
		Use:   "bind <name>",
		Short: "Bind an agent to a channel",
		Long:  "Bind an agent profile to a specific channel on the running gateway.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			req := agentBindRequest{
				Channel:    channel,
				SessionKey: sessionKey,
			}
			return runAgentsBind(cmd.Context(), args[0], req)
		},
	}

	cmd.Flags().StringVar(&channel, "channel", "", "channel to bind the agent to (required)")
	cmd.Flags().StringVar(&sessionKey, "session-key", "", "session key for the binding")
	_ = cmd.MarkFlagRequired("channel")

	return cmd
}

func runAgentsBind(ctx context.Context, name string, req agentBindRequest) error {
	client, err := NewGatewayClient()
	if err != nil {
		return err
	}

	var resp agentBindResponse
	if err := client.Post(ctx, agentsBasePath+"/"+name+"/bind", req, &resp); err != nil {
		return err
	}

	if flagJSON {
		return printJSON(resp)
	}

	fmt.Printf("Bound agent %q to channel %q\n", name, req.Channel)
	return nil
}
