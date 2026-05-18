package gateway

import (
	"errors"
	"net/http"

	apiresponse "github.com/fulcrus/hopclaw/internal/apiresponse"
	"github.com/fulcrus/hopclaw/sandbox"
)

// ---------------------------------------------------------------------------
// Request / response types
// ---------------------------------------------------------------------------

type sandboxExecRequest struct {
	Image   string            `json:"image"`
	Command []string          `json:"command"`
	Stdin   string            `json:"stdin,omitempty"`
	Env     map[string]string `json:"env,omitempty"`
	Timeout int               `json:"timeout,omitempty"`
}

type sandboxExecResponse struct {
	Stdout     string `json:"stdout"`
	Stderr     string `json:"stderr"`
	ExitCode   int    `json:"exit_code"`
	TimedOut   bool   `json:"timed_out"`
	Truncated  bool   `json:"truncated,omitempty"`
	DurationMS int64  `json:"duration_ms"`
}

type sandboxStatusResponse struct {
	Available bool `json:"available"`
	Docker    bool `json:"docker"`
}

// ---------------------------------------------------------------------------
// Handlers
// ---------------------------------------------------------------------------

// handleSandboxExec runs a command inside a sandboxed container.
//
//	POST /operator/sandbox/exec
func (g *Gateway) handleSandboxExec(w http.ResponseWriter, r *http.Request) {
	if g.sandbox == nil {
		gwError(w, http.StatusServiceUnavailable, "sandbox not available")
		return
	}

	var req sandboxExecRequest
	if err := decodeJSONBody(w, r, &req); err != nil {
		if errors.Is(err, errRequestBodyTooLarge) {
			gwErrorCode(w, http.StatusRequestEntityTooLarge, apiresponse.ErrorCodeRequestBodyTooLarge, err.Error())
			return
		}
		gwErrorCode(w, http.StatusBadRequest, apiresponse.ErrorCodeInvalidJSON, "invalid json")
		return
	}

	result, err := g.sandbox.Exec(r.Context(), sandbox.ExecRequest{
		Image:   req.Image,
		Command: req.Command,
		Stdin:   req.Stdin,
		Env:     req.Env,
		Timeout: req.Timeout,
	})
	if err != nil {
		gwError(w, http.StatusInternalServerError, err.Error())
		return
	}

	gwJSON(w, http.StatusOK, sandboxExecResponse{
		Stdout:     result.Stdout,
		Stderr:     result.Stderr,
		ExitCode:   result.ExitCode,
		TimedOut:   result.TimedOut,
		Truncated:  result.Truncated,
		DurationMS: result.Duration.Milliseconds(),
	})
}

// handleSandboxStatus returns the availability of the sandbox runtime.
//
//	GET /operator/sandbox/status
func (g *Gateway) handleSandboxStatus(w http.ResponseWriter, _ *http.Request) {
	if g.sandbox == nil {
		gwError(w, http.StatusServiceUnavailable, "sandbox not available")
		return
	}

	available := g.sandbox.IsAvailable()
	gwJSON(w, http.StatusOK, sandboxStatusResponse{
		Available: available,
		Docker:    available,
	})
}
