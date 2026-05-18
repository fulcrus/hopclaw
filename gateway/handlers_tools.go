package gateway

import (
	"net/http"
	"strings"
	"time"

	"github.com/fulcrus/hopclaw/agent"
	"github.com/fulcrus/hopclaw/resultmodel"
)

// ---------------------------------------------------------------------------
// Request / response types
// ---------------------------------------------------------------------------

type toolTestRequest struct {
	Tool       string         `json:"tool"`
	Input      map[string]any `json:"input"`
	Arguments  map[string]any `json:"arguments"`
	SessionKey string         `json:"session_key"`
}

type toolTestResponse struct {
	OK              bool                         `json:"ok"`
	Tool            string                       `json:"tool"`
	Output          string                       `json:"output"`
	Summary         string                       `json:"summary,omitempty"`
	ArtifactURI     string                       `json:"artifact_uri,omitempty"`
	Artifacts       []resultmodel.ResultArtifact `json:"artifacts,omitempty"`
	Blocks          []resultmodel.ResultBlock    `json:"blocks,omitempty"`
	Actions         []resultmodel.ResultAction   `json:"actions,omitempty"`
	Status          resultmodel.ToolResultStatus `json:"status,omitempty"`
	DurationMS      int64                        `json:"duration_ms"`
	SideEffectClass string                       `json:"side_effect_class,omitempty"`
	InputSchema     map[string]any               `json:"input_schema,omitempty"`
	OutputSchema    map[string]any               `json:"output_schema,omitempty"`
}

// ---------------------------------------------------------------------------
// Handlers
// ---------------------------------------------------------------------------

// handleToolsTest executes a single read-only tool call.
//
//	POST /operator/tools/test
func (g *Gateway) handleToolsTest(w http.ResponseWriter, r *http.Request) {
	if g.runtime == nil || g.runtime.Agent() == nil {
		gwError(w, http.StatusServiceUnavailable, "runtime agent is not available")
		return
	}
	var req toolTestRequest
	if !decodeOperatorJSONBody(w, r, &req) {
		return
	}
	if strings.TrimSpace(req.Tool) == "" {
		gwError(w, http.StatusBadRequest, "tool is required")
		return
	}
	input := req.Input
	if input == nil {
		input = req.Arguments
	}
	if input == nil {
		input = map[string]any{}
	}
	call := agent.ToolCall{
		Name:  strings.TrimSpace(req.Tool),
		Input: input,
	}
	start := time.Now()
	definition, result, err := g.runtime.Agent().TestTool(r.Context(), strings.TrimSpace(req.SessionKey), call)
	if err != nil {
		gwError(w, http.StatusBadRequest, err.Error())
		return
	}
	normalized := result.Normalized()
	gwJSON(w, http.StatusOK, toolTestResponse{
		OK:              true,
		Tool:            definition.Name,
		Output:          normalized.TranscriptText,
		Summary:         normalized.Summary,
		ArtifactURI:     normalized.ArtifactURI,
		Artifacts:       normalized.Artifacts,
		Blocks:          normalized.Blocks,
		Actions:         normalized.Actions,
		Status:          normalized.Status,
		DurationMS:      time.Since(start).Milliseconds(),
		SideEffectClass: definition.SideEffectClass,
		InputSchema:     definition.InputSchema,
		OutputSchema:    definition.OutputSchema,
	})
}
