package toolruntime

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/fulcrus/hopclaw/agent"
	"github.com/fulcrus/hopclaw/contextengine"
	"github.com/fulcrus/hopclaw/skill"
)

// ExternalToolConfig configures an HTTP-based external tool.
type ExternalToolConfig struct {
	Name        string
	Description string
	Endpoint    string // HTTP endpoint that handles tool invocations
	Timeout     time.Duration
	InputSchema map[string]any
}

// ExternalToolExecutor executes tools by calling external HTTP endpoints.
// This allows users to write tool implementations in any language.
//
// Protocol:
//
//	POST {endpoint}
//	Content-Type: application/json
//	{"name":"tool.name","input":{"key":"value"},"session_id":"...","run_id":"..."}
//
//	Response:
//	{"output":"result text","error":"optional error message"}
type ExternalToolExecutor struct {
	tools      map[string]ExternalToolConfig
	httpClient *http.Client
}

const (
	externalToolDefaultSideEffect = "external_write"
)

// NewExternalToolExecutor creates an executor for the given external tools.
func NewExternalToolExecutor(tools []ExternalToolConfig) *ExternalToolExecutor {
	index := make(map[string]ExternalToolConfig, len(tools))
	for _, t := range tools {
		if strings.TrimSpace(t.Name) == "" || strings.TrimSpace(t.Endpoint) == "" {
			continue
		}
		if t.Timeout <= 0 {
			t.Timeout = 30 * time.Second
		}
		index[t.Name] = t
	}
	if len(index) == 0 {
		return nil
	}
	return &ExternalToolExecutor{
		tools:      index,
		httpClient: &http.Client{},
	}
}

func (e *ExternalToolExecutor) ExecuteBatch(ctx context.Context, run *agent.Run, session *agent.Session, calls []agent.ToolCall) ([]contextengine.ToolResult, error) {
	results := make([]contextengine.ToolResult, 0, len(calls))
	for _, call := range calls {
		cfg, ok := e.tools[call.Name]
		if !ok {
			continue
		}
		result := e.executeOne(ctx, cfg, call, run, session)
		results = append(results, result)
	}
	return results, nil
}

func (e *ExternalToolExecutor) executeOne(ctx context.Context, cfg ExternalToolConfig, call agent.ToolCall, run *agent.Run, session *agent.Session) contextengine.ToolResult {
	payload, err := encodeExecuteRequest(call, run, session)
	if err != nil {
		return contextengine.ToolResult{
			ToolCallID: call.ID,
			ToolName:   call.Name,
			Content:    fmt.Sprintf("error marshaling request: %v", err),
		}
	}

	callCtx, cancel := context.WithTimeout(ctx, cfg.Timeout)
	defer cancel()

	httpReq, err := http.NewRequestWithContext(callCtx, http.MethodPost, cfg.Endpoint, bytes.NewReader(payload))
	if err != nil {
		return contextengine.ToolResult{
			ToolCallID: call.ID,
			ToolName:   call.Name,
			Content:    fmt.Sprintf("error creating request: %v", err),
		}
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := e.httpClient.Do(httpReq)
	if err != nil {
		return contextengine.ToolResult{
			ToolCallID: call.ID,
			ToolName:   call.Name,
			Content:    fmt.Sprintf("error calling endpoint: %v", err),
		}
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20)) // 1MB limit

	if resp.StatusCode >= 400 {
		return contextengine.ToolResult{
			ToolCallID: call.ID,
			ToolName:   call.Name,
			Content:    fmt.Sprintf("endpoint returned %d: %s", resp.StatusCode, strings.TrimSpace(string(body))),
		}
	}

	return decodeToolResultPayload(call, body)
}

// ToolDefinitions returns tool definitions for all registered external tools.
func (e *ExternalToolExecutor) ToolDefinitions(_ *agent.Session) []agent.ToolDefinition {
	names := make([]string, 0, len(e.tools))
	for name := range e.tools {
		names = append(names, name)
	}
	sort.Strings(names)
	defs := make([]agent.ToolDefinition, 0, len(names))
	for _, name := range names {
		cfg := e.tools[name]
		defs = append(defs, agent.ToolDefinition{
			Name:             cfg.Name,
			Description:      cfg.Description,
			InputSchema:      cfg.InputSchema,
			SideEffectClass:  externalToolDefaultSideEffect,
			RequiresApproval: true,
			Source:           "external",
			SourceRef:        cfg.Endpoint,
			Trust:            "community",
			Eligible:         true,
			Availability: agent.ToolAvailability{
				Status: agent.AvailabilityReady,
			},
		})
	}
	return defs
}

// ResolveTool returns a canonical resolved tool for external tools.
func (e *ExternalToolExecutor) ResolveTool(_ *agent.Session, name string) (*agent.ResolvedTool, bool) {
	cfg, ok := e.tools[name]
	if !ok {
		return nil, false
	}
	binding := &skill.BoundTool{
		Package: &skill.SkillPackage{
			Trust: skill.TrustCommunity,
			Source: skill.SkillSource{
				Kind: skill.SourcePlugin,
				Dir:  cfg.Endpoint,
				Root: cfg.Endpoint,
			},
		},
		Manifest: skill.ToolManifest{
			Name:             cfg.Name,
			Description:      cfg.Description,
			InputSchema:      cfg.InputSchema,
			SideEffectClass:  externalToolDefaultSideEffect,
			RequiresApproval: true,
		},
		Eligibility: skill.EligibilityResult{Eligible: true},
	}
	return resolvedToolFromBinding(binding, agent.ToolDefinition{
		Name:             cfg.Name,
		Description:      cfg.Description,
		InputSchema:      cloneSchema(cfg.InputSchema),
		SideEffectClass:  externalToolDefaultSideEffect,
		RequiresApproval: true,
		Source:           "external",
		SourceRef:        cfg.Endpoint,
		Trust:            "community",
		Eligible:         true,
		Availability: agent.ToolAvailability{
			Status: agent.AvailabilityReady,
		},
	}, "external:"+cfg.Name), true
}
