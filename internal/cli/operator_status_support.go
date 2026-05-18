package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/fulcrus/hopclaw/controlplane"
)

type operatorStatusResponse struct {
	OK              bool                    `json:"ok"`
	State           string                  `json:"state,omitempty"`
	Summary         string                  `json:"summary,omitempty"`
	Version         string                  `json:"version,omitempty"`
	Uptime          string                  `json:"uptime,omitempty"`
	CapabilityCount int                     `json:"capability_count,omitempty"`
	ActiveRuns      int                     `json:"active_runs,omitempty"`
	QueuedRuns      int                     `json:"queued_runs,omitempty"`
	Channels        []operatorStatusChannel `json:"connected_channels,omitempty"`
	Warnings        []string                `json:"warnings,omitempty"`
	UserSurface     controlplane.UserSurfaceSummary `json:"user_surface"`
	Update          map[string]any          `json:"update,omitempty"`
}

type operatorStatusChannel struct {
	Name   string `json:"name"`
	Status string `json:"status"`
}

type gatewayHealthResponse struct {
	OK       bool     `json:"ok"`
	State    string   `json:"state,omitempty"`
	Summary  string   `json:"summary,omitempty"`
	Warnings []string `json:"warnings,omitempty"`
}

func fetchOperatorStatus(ctx context.Context, client *GatewayClient) ([]byte, int, error) {
	if client == nil {
		return nil, 0, fmt.Errorf("gateway client is not configured")
	}
	return client.GetRawWithStatus(ctx, operatorStatusPath)
}

func fetchGatewayHealth(ctx context.Context, client *GatewayClient) ([]byte, int, error) {
	if client == nil {
		return nil, 0, fmt.Errorf("gateway client is not configured")
	}
	return client.GetRawWithStatus(ctx, publicHealthPath)
}

func decodeOperatorStatus(body []byte) (operatorStatusResponse, error) {
	var status operatorStatusResponse
	if err := json.Unmarshal(body, &status); err != nil {
		return operatorStatusResponse{}, err
	}
	return status, nil
}

func decodeGatewayHealth(body []byte) (gatewayHealthResponse, error) {
	var status gatewayHealthResponse
	if err := json.Unmarshal(body, &status); err != nil {
		return gatewayHealthResponse{}, err
	}
	return status, nil
}

func operatorStatusLabel(status operatorStatusResponse) string {
	if label := strings.TrimSpace(status.State); label != "" {
		return label
	}
	if status.OK {
		return "ready"
	}
	return "unhealthy"
}

func operatorStatusSummary(status operatorStatusResponse) string {
	if summary := strings.TrimSpace(status.Summary); summary != "" {
		return summary
	}
	for _, item := range status.Warnings {
		if summary := strings.TrimSpace(item); summary != "" {
			return summary
		}
	}
	label := strings.TrimSpace(operatorStatusLabel(status))
	if label == "" {
		return "operator status unavailable"
	}
	return label
}

func gatewayHealthLabel(status gatewayHealthResponse) string {
	if label := strings.TrimSpace(status.State); label != "" {
		return label
	}
	if status.OK {
		return "ready"
	}
	return "unhealthy"
}

func gatewayHealthSummary(status gatewayHealthResponse) string {
	if summary := strings.TrimSpace(status.Summary); summary != "" {
		return summary
	}
	for _, item := range status.Warnings {
		if summary := strings.TrimSpace(item); summary != "" {
			return summary
		}
	}
	label := strings.TrimSpace(gatewayHealthLabel(status))
	if label == "" {
		return "gateway health unavailable"
	}
	return label
}
