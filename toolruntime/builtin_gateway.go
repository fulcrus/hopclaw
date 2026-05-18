package toolruntime

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"

	"github.com/fulcrus/hopclaw/agent"
	"github.com/fulcrus/hopclaw/contextengine"
)

const (
	// defaultGatewayAddr is the fallback gateway address when none is configured.
	defaultGatewayAddr = "http://localhost:16280"

	// gatewayHTTPTimeout is the timeout for HTTP requests to the gateway.
	gatewayHTTPTimeout = 5 * time.Second

	// gatewayAddrEnvVar is the environment variable name for the gateway address.
	gatewayAddrEnvVar = "HOPCLAW_GATEWAY_ADDR"

	// maxGatewayResponseBytes limits the size of gateway response bodies.
	maxGatewayResponseBytes = 1024 * 1024
)

// ---------------------------------------------------------------------------
// Input schemas
// ---------------------------------------------------------------------------

func gatewayStatusInputSchema() map[string]any {
	return map[string]any{
		"type":                 "object",
		"properties":           map[string]any{},
		"additionalProperties": false,
	}
}

func gatewayCapabilitiesInputSchema() map[string]any {
	return map[string]any{
		"type":                 "object",
		"properties":           map[string]any{},
		"additionalProperties": false,
	}
}

func gatewayHealthInputSchema() map[string]any {
	return map[string]any{
		"type":                 "object",
		"properties":           map[string]any{},
		"additionalProperties": false,
	}
}

func gatewayReloadInputSchema() map[string]any {
	return map[string]any{
		"type":                 "object",
		"properties":           map[string]any{},
		"additionalProperties": false,
	}
}

// ---------------------------------------------------------------------------
// Output schemas
// ---------------------------------------------------------------------------

func gatewayStatusOutputSchema() map[string]any {
	return objectSchema(map[string]any{
		"version":          stringSchema("Gateway version."),
		"uptime":           stringSchema("Gateway uptime duration."),
		"status":           stringSchema("Current gateway status."),
		"capability_count": integerSchema("Number of registered capabilities."),
	}, "status")
}

func gatewayCapabilitiesOutputSchema() map[string]any {
	return objectSchema(map[string]any{
		"capabilities": arraySchema(
			objectSchema(map[string]any{
				"name":        stringSchema("Capability name."),
				"description": stringSchema("Capability description."),
			}),
			"Registered capabilities.",
		),
		"count": integerSchema("Total number of capabilities."),
	}, "capabilities", "count")
}

func gatewayHealthOutputSchema() map[string]any {
	return objectSchema(map[string]any{
		"channels": arraySchema(
			objectSchema(map[string]any{
				"name":    stringSchema("Channel name."),
				"healthy": booleanSchema("Whether the channel is healthy."),
				"status":  stringSchema("Channel status details."),
			}),
			"Channel health entries.",
		),
		"healthy_count": integerSchema("Number of healthy channels."),
		"total_count":   integerSchema("Total number of channels."),
	}, "channels", "healthy_count", "total_count")
}

func gatewayReloadOutputSchema() map[string]any {
	return objectSchema(map[string]any{
		"ok":      booleanSchema("Whether the reload was triggered successfully."),
		"message": stringSchema("Human-readable result message."),
	}, "ok", "message")
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// gatewayBaseURL returns the gateway base URL from the environment or the default.
func gatewayBaseURL() string {
	if addr := os.Getenv(gatewayAddrEnvVar); addr != "" {
		return addr
	}
	return defaultGatewayAddr
}

// gatewayHTTPClient returns a short-lived HTTP client for gateway requests.
func gatewayHTTPClient() *http.Client {
	return &http.Client{Timeout: gatewayHTTPTimeout}
}

// gatewayGet performs a GET request to the gateway and decodes the JSON response.
func gatewayGet(ctx context.Context, path string) (map[string]any, error) {
	reqURL := gatewayBaseURL() + path

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := gatewayHTTPClient().Do(req)
	if err != nil {
		return nil, fmt.Errorf("request to %s failed: %w", path, err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxGatewayResponseBytes))
	if err != nil {
		return nil, fmt.Errorf("failed to read response from %s: %w", path, err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("gateway %s returned status %d: %s", path, resp.StatusCode, string(body))
	}

	var result map[string]any
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("failed to decode response from %s: %w", path, err)
	}
	return result, nil
}

// gatewayPost performs a POST request to the gateway and decodes the JSON response.
func gatewayPost(ctx context.Context, path string) (map[string]any, error) {
	reqURL := gatewayBaseURL() + path

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, reqURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := gatewayHTTPClient().Do(req)
	if err != nil {
		return nil, fmt.Errorf("request to %s failed: %w", path, err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxGatewayResponseBytes))
	if err != nil {
		return nil, fmt.Errorf("failed to read response from %s: %w", path, err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("gateway %s returned status %d: %s", path, resp.StatusCode, string(body))
	}

	var result map[string]any
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("failed to decode response from %s: %w", path, err)
	}
	return result, nil
}

// ---------------------------------------------------------------------------
// Handlers
// ---------------------------------------------------------------------------

func gatewayJSONResult(call agent.ToolCall, payload map[string]any) (contextengine.ToolResult, error) {
	body, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return contextengine.ToolResult{}, err
	}
	return contextengine.ToolResult{
		ToolName:   call.Name,
		ToolCallID: call.ID,
		Content:    string(body),
	}, nil
}

func handleGatewayStatus(ctx context.Context, call agent.ToolCall) (contextengine.ToolResult, error) {
	data, err := gatewayGet(ctx, "/operator/status")
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("gateway.status: %w", err)
	}

	version, _ := data["version"].(string)
	uptime, _ := data["uptime"].(string)
	status, _ := data["status"].(string)

	capCount := 0
	if v, ok := data["capability_count"].(float64); ok {
		capCount = int(v)
	}

	return gatewayJSONResult(call, map[string]any{
		"version":          version,
		"uptime":           uptime,
		"status":           status,
		"capability_count": capCount,
	})
}

func handleGatewayCapabilities(ctx context.Context, call agent.ToolCall) (contextengine.ToolResult, error) {
	data, err := gatewayGet(ctx, "/operator/capabilities")
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("gateway.capabilities: %w", err)
	}

	capabilities, _ := data["capabilities"].([]any)
	if capabilities == nil {
		capabilities = []any{}
	}

	return gatewayJSONResult(call, map[string]any{
		"capabilities": capabilities,
		"count":        len(capabilities),
	})
}

func handleGatewayHealth(ctx context.Context, call agent.ToolCall) (contextengine.ToolResult, error) {
	data, err := gatewayGet(ctx, "/operator/channels/health")
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("gateway.health: %w", err)
	}

	channels, _ := data["channels"].([]any)
	if channels == nil {
		channels = []any{}
	}

	healthyCount := 0
	for _, ch := range channels {
		if entry, ok := ch.(map[string]any); ok {
			if healthy, _ := entry["healthy"].(bool); healthy {
				healthyCount++
			}
		}
	}

	return gatewayJSONResult(call, map[string]any{
		"channels":      channels,
		"healthy_count": healthyCount,
		"total_count":   len(channels),
	})
}

func handleGatewayReload(ctx context.Context, call agent.ToolCall) (contextengine.ToolResult, error) {
	data, err := gatewayPost(ctx, "/operator/reload")
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("gateway.reload: %w", err)
	}

	ok, _ := data["ok"].(bool)
	message, _ := data["message"].(string)
	if message == "" && ok {
		message = "configuration reload triggered"
	}

	return gatewayJSONResult(call, map[string]any{
		"ok":      ok,
		"message": message,
	})
}
