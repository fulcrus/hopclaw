package gateway

import (
	"errors"
	"fmt"
	"net/http"
	"time"
)

// ---------------------------------------------------------------------------
// POST /v1/chat/completions
// ---------------------------------------------------------------------------

// handleChatCompletions implements an OpenAI Chat Completions compatible
// endpoint. It extracts the last user message from the request, delegates to
// the runtime's one-shot executor, and returns an OpenAI-format response.
func (g *Gateway) handleChatCompletions(w http.ResponseWriter, r *http.Request) {
	if g.runtime == nil {
		gwJSON(w, http.StatusServiceUnavailable, oaiErrorResponse("runtime not available"))
		return
	}

	var req oaiChatRequest
	if err := decodeJSONBody(w, r, &req); err != nil {
		if errors.Is(err, errRequestBodyTooLarge) {
			gwJSON(w, http.StatusRequestEntityTooLarge, oaiErrorResponse(err.Error()))
			return
		}
		gwJSON(w, http.StatusBadRequest, oaiErrorResponse(fmt.Sprintf("invalid request body: %v", err)))
		return
	}

	message := extractLastUserMessage(req.Messages)
	if message == "" {
		gwJSON(w, http.StatusBadRequest, oaiErrorResponse("no user message found"))
		return
	}

	model := req.Model
	if model == "" {
		model = "default"
	}

	result, err := g.runtime.ExecuteOneShot(r.Context(), message, model)
	if err != nil {
		gwJSON(w, http.StatusInternalServerError, oaiErrorResponse(fmt.Sprintf("execution failed: %v", err)))
		return
	}

	resp := oaiChatResponse{
		ID:      fmt.Sprintf("chatcmpl-%d", time.Now().UnixNano()),
		Object:  "chat.completion",
		Created: time.Now().Unix(),
		Model:   model,
		Choices: []oaiChatChoice{
			{
				Index: 0,
				Message: oaiChatMessage{
					Role:    "assistant",
					Content: result,
				},
				FinishReason: "stop",
			},
		},
		Usage: oaiUsage{},
	}
	gwJSON(w, http.StatusOK, resp)
}

// extractLastUserMessage returns the content of the last message with role
// "user" from the messages list.
func extractLastUserMessage(messages []oaiChatMessage) string {
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Role == "user" {
			return messages[i].Content
		}
	}
	return ""
}

// oaiErrorResponse builds a minimal OpenAI-compatible error response body.
func oaiErrorResponse(message string) map[string]any {
	return map[string]any{
		"error": map[string]any{
			"message": message,
			"type":    "invalid_request_error",
		},
	}
}
