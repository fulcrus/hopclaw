package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/fulcrus/hopclaw/contextengine"
	runtimesvc "github.com/fulcrus/hopclaw/runtime"
)

type interactRequestPayload struct {
	runtimesvc.InteractionRequest
	Input string `json:"input,omitempty"`
}

type interactResponse struct {
	*runtimesvc.InteractionResult
	Message string `json:"message,omitempty"`
}

func (s *Server) handleInteract(w http.ResponseWriter, r *http.Request) {
	ct := r.Header.Get("Content-Type")
	if ct != "" && !strings.HasPrefix(ct, "application/json") {
		writeError(w, http.StatusUnsupportedMediaType, fmt.Errorf("content-type must be application/json"))
		return
	}
	var req interactRequestPayload
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if strings.TrimSpace(req.Content) == "" && strings.TrimSpace(req.Input) != "" {
		req.Content = req.Input
	}
	if strings.TrimSpace(req.Content) == "" && len(req.ContentBlocks) == 0 && len(req.Images) == 0 && req.StructuredCommand == nil && req.StructuredApproval == nil {
		writeError(w, http.StatusBadRequest, fmt.Errorf("content, content_blocks, images, or structured action is required"))
		return
	}
	if err := applyInteractionAuthScope(r, &req.InteractionRequest); err != nil {
		writeError(w, http.StatusForbidden, err)
		return
	}
	result, err := s.runtime.Interact(r.Context(), req.InteractionRequest)
	if err != nil {
		writeMappedError(w, err)
		return
	}
	writeJSON(w, http.StatusAccepted, interactResponse{
		InteractionResult: result,
		Message:           runtimesvc.RenderDirectInteractionReply(result, effectiveInteractReplyContent(req.Content, req.ContentBlocks)),
	})
}

func effectiveInteractReplyContent(content string, blocks []contextengine.ContentBlock) string {
	if trimmed := strings.TrimSpace(content); trimmed != "" {
		return trimmed
	}
	parts := make([]string, 0, len(blocks))
	for _, block := range blocks {
		if block.Type != "text" {
			continue
		}
		if text := strings.TrimSpace(block.Text); text != "" {
			parts = append(parts, text)
		}
	}
	return strings.Join(parts, "\n")
}
