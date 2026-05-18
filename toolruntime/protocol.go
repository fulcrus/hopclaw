package toolruntime

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/fulcrus/hopclaw/agent"
	"github.com/fulcrus/hopclaw/bundle"
	"github.com/fulcrus/hopclaw/contextengine"
	"github.com/fulcrus/hopclaw/resultmodel"
)

type legacyExternalToolResponse struct {
	Output string `json:"output"`
	Error  string `json:"error,omitempty"`
}

type legacyLocalToolResponse struct {
	Content     string `json:"content"`
	ArtifactURI string `json:"artifact_uri"`
}

func buildExecuteRequest(call agent.ToolCall, run *agent.Run, session *agent.Session) bundle.ExecuteRequest {
	req := bundle.ExecuteRequest{
		ProtocolVersion: bundle.ProtocolVersionV1,
		ToolName:        call.Name,
		ToolCallID:      call.ID,
		Input:           call.Input,
	}
	if run != nil {
		req.RunID = run.ID
	}
	if session != nil {
		req.SessionID = session.ID
	}
	return req
}

func encodeExecuteRequest(call agent.ToolCall, run *agent.Run, session *agent.Session) ([]byte, error) {
	return json.Marshal(buildExecuteRequest(call, run, session))
}

func decodeToolResultPayload(call agent.ToolCall, body []byte) contextengine.ToolResult {
	if normalized, ok := decodeStructuredToolResult(call, body); ok {
		return normalized
	}
	return contextengine.ToolResult{
		ToolName:       call.Name,
		ToolCallID:     call.ID,
		TranscriptText: strings.TrimSpace(string(body)),
		Content:        strings.TrimSpace(string(body)),
	}
}

func decodeStructuredToolResult(call agent.ToolCall, body []byte) (contextengine.ToolResult, bool) {
	trimmed := bytes.TrimSpace(body)
	if len(trimmed) == 0 {
		return contextengine.ToolResult{
			ToolName:       call.Name,
			ToolCallID:     call.ID,
			TranscriptText: "",
			Content:        "",
		}, true
	}

	var legacyLocal legacyLocalToolResponse
	if err := json.Unmarshal(trimmed, &legacyLocal); err == nil &&
		(legacyLocal.Content != "" || legacyLocal.ArtifactURI != "") {
		return contextengine.ToolResult{
			ToolName:       call.Name,
			ToolCallID:     call.ID,
			TranscriptText: strings.TrimSpace(legacyLocal.Content),
			Content:        strings.TrimSpace(legacyLocal.Content),
			ArtifactURI:    strings.TrimSpace(legacyLocal.ArtifactURI),
		}, true
	}

	var legacyExternal legacyExternalToolResponse
	if err := json.Unmarshal(trimmed, &legacyExternal); err == nil &&
		(legacyExternal.Output != "" || legacyExternal.Error != "") {
		if legacyExternal.Error != "" {
			return contextengine.ToolResult{
				ToolName:       call.Name,
				ToolCallID:     call.ID,
				Status:         resultmodel.ToolResultError,
				TranscriptText: "error: " + strings.TrimSpace(legacyExternal.Error),
				Content:        "error: " + strings.TrimSpace(legacyExternal.Error),
				Error: &resultmodel.ResultError{
					Message: strings.TrimSpace(legacyExternal.Error),
				},
			}, true
		}
		return contextengine.ToolResult{
			ToolName:       call.Name,
			ToolCallID:     call.ID,
			TranscriptText: strings.TrimSpace(legacyExternal.Output),
			Content:        strings.TrimSpace(legacyExternal.Output),
		}, true
	}

	var objectKeys map[string]json.RawMessage
	if err := json.Unmarshal(trimmed, &objectKeys); err == nil && looksLikeExecuteResponse(objectKeys) {
		var structured bundle.ExecuteResponse
		if err := json.Unmarshal(trimmed, &structured); err == nil {
			return renderExecuteResponse(call, structured.Normalized()), true
		}
	}

	return contextengine.ToolResult{}, false
}

func looksLikeExecuteResponse(keys map[string]json.RawMessage) bool {
	if len(keys) == 0 {
		return false
	}
	for _, key := range []string{
		"protocol_version",
		"ok",
		"status",
		"summary",
		"content",
		"data",
		"artifacts",
		"evidence",
		"verification",
		"error",
		"metrics",
	} {
		if _, ok := keys[key]; ok {
			return true
		}
	}
	return false
}

func renderExecuteResponse(call agent.ToolCall, resp bundle.ExecuteResponse) contextengine.ToolResult {
	content := strings.TrimSpace(resp.Summary)
	if content == "" {
		content = strings.TrimSpace(resp.Content)
	}
	if content == "" && resp.Data != nil {
		if data, err := json.MarshalIndent(resp.Data, "", "  "); err == nil {
			content = string(data)
		}
	}
	if content == "" && resp.Error != nil {
		content = renderExecuteError(resp.Error)
	}
	if content == "" {
		content = string(resp.Status)
	}

	if len(resp.Evidence) > 0 {
		lines := make([]string, 0, len(resp.Evidence))
		for _, item := range resp.Evidence {
			label := strings.TrimSpace(item.Kind)
			if strings.TrimSpace(item.Name) != "" {
				label += ":" + strings.TrimSpace(item.Name)
			}
			if strings.TrimSpace(item.Detail) != "" {
				label += "=" + strings.TrimSpace(item.Detail)
			}
			if label != "" {
				lines = append(lines, "[evidence] "+label)
			}
		}
		if len(lines) > 0 {
			if content != "" {
				content += "\n"
			}
			content += strings.Join(lines, "\n")
		}
	}

	firstArtifact := ""
	if len(resp.Artifacts) > 0 {
		firstArtifact = strings.TrimSpace(resp.Artifacts[0].URI)
		if len(resp.Artifacts) > 1 {
			extra := make([]string, 0, len(resp.Artifacts)-1)
			for _, artifact := range resp.Artifacts[1:] {
				if uri := strings.TrimSpace(artifact.URI); uri != "" {
					extra = append(extra, "[artifact] "+uri)
				}
			}
			if len(extra) > 0 {
				if content != "" {
					content += "\n"
				}
				content += strings.Join(extra, "\n")
			}
		}
	}

	return contextengine.ToolResult{
		ToolName:       call.Name,
		ToolCallID:     call.ID,
		TranscriptText: strings.TrimSpace(content),
		Summary:        strings.TrimSpace(resp.Summary),
		Content:        strings.TrimSpace(content),
		ArtifactURI:    firstArtifact,
		Artifacts:      buildResultArtifacts(resp.Artifacts),
		Status:         mapExecuteStatus(resp.Status),
	}.Normalized()
}

func renderExecuteError(err *bundle.ExecuteError) string {
	if err == nil {
		return "error: tool execution failed"
	}
	code := strings.TrimSpace(err.Code)
	msg := strings.TrimSpace(err.Message)
	switch {
	case code != "" && msg != "":
		return fmt.Sprintf("error [%s]: %s", code, msg)
	case msg != "":
		return "error: " + msg
	case code != "":
		return "error [" + code + "]"
	default:
		return "error: tool execution failed"
	}
}

func buildResultArtifacts(items []bundle.Artifact) []resultmodel.ResultArtifact {
	if len(items) == 0 {
		return nil
	}
	out := make([]resultmodel.ResultArtifact, 0, len(items))
	for _, item := range items {
		uri := strings.TrimSpace(item.URI)
		if uri == "" {
			continue
		}
		out = append(out, resultmodel.ResultArtifact{
			Kind:        "artifact",
			Name:        strings.TrimSpace(item.Name),
			URI:         uri,
			ContentType: strings.TrimSpace(item.MediaType),
		})
	}
	return out
}

func mapExecuteStatus(status bundle.ResultStatus) resultmodel.ToolResultStatus {
	switch status {
	case bundle.ResultError, bundle.ResultRetryableError, bundle.ResultBlocked:
		return resultmodel.ToolResultError
	case bundle.ResultPartial:
		return resultmodel.ToolResultPartial
	default:
		return resultmodel.ToolResultOK
	}
}
