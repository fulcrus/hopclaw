package server

import (
	"errors"
	"net/http"

	"github.com/fulcrus/hopclaw/agent"
	"github.com/fulcrus/hopclaw/approval"
	runtimesvc "github.com/fulcrus/hopclaw/runtime"
	"github.com/fulcrus/hopclaw/internal/usererror"
)

func serverHTTPStatusForError(err error, fallback int) int {
	switch {
	case err == nil:
		return http.StatusOK
	case errors.Is(err, agent.ErrRunRejected),
		errors.Is(err, agent.ErrRunCancelled):
		return http.StatusConflict
	case errors.Is(err, agent.ErrToolDenied):
		return http.StatusForbidden
	case errors.Is(err, approval.ErrInvalidScope),
		errors.Is(err, approval.ErrScopePolicy):
		return http.StatusBadRequest
	case errors.Is(err, runtimesvc.ErrRateLimited):
		return http.StatusTooManyRequests
	case errors.Is(err, runtimesvc.ErrApprovalSyncerNil),
		errors.Is(err, runtimesvc.ErrGovernanceDeliveryControllerNil):
		return http.StatusServiceUnavailable
	case errors.Is(err, runtimesvc.ErrArtifactPruneNoSelector):
		return http.StatusBadRequest
	}
	return usererror.HTTPStatus(err, fallback)
}
