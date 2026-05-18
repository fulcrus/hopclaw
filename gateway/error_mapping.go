package gateway

import (
	"errors"
	"net/http"

	"github.com/fulcrus/hopclaw/agent"
	"github.com/fulcrus/hopclaw/approval"
	"github.com/fulcrus/hopclaw/cron"
	"github.com/fulcrus/hopclaw/deviceauth"
	"github.com/fulcrus/hopclaw/hooks"
	runtimepkg "github.com/fulcrus/hopclaw/runtime"
	"github.com/fulcrus/hopclaw/internal/usererror"
	"github.com/fulcrus/hopclaw/watch"
)

func gatewayHTTPStatusForError(err error, fallback int) int {
	switch {
	case err == nil:
		return http.StatusOK
	case errors.Is(err, approval.ErrNotFound),
		errors.Is(err, cron.ErrNotFound),
		errors.Is(err, deviceauth.ErrNotFound),
		errors.Is(err, watch.ErrNotFound):
		return http.StatusNotFound
	case errors.Is(err, approval.ErrAlreadyResolved),
		errors.Is(err, hooks.ErrNoReplayPayload),
		errors.Is(err, agent.ErrRunRejected),
		errors.Is(err, agent.ErrRunCancelled):
		return http.StatusConflict
	case errors.Is(err, approval.ErrInvalidScope),
		errors.Is(err, approval.ErrScopePolicy):
		return http.StatusBadRequest
	case errors.Is(err, agent.ErrToolDenied):
		return http.StatusForbidden
	case errors.Is(err, runtimepkg.ErrRateLimited):
		return http.StatusTooManyRequests
	case errors.Is(err, runtimepkg.ErrApprovalSyncerNil),
		errors.Is(err, runtimepkg.ErrGovernanceDeliveryControllerNil):
		return http.StatusServiceUnavailable
	}
	return usererror.HTTPStatus(err, fallback)
}
