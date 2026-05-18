package gateway

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"github.com/fulcrus/hopclaw/hooks"
	runtimesvc "github.com/fulcrus/hopclaw/runtime"
)

type hookOperatorDeps struct {
	executor *hooks.Executor
	runtime  *runtimesvc.Service
}

func hookOperatorDepsFromGateway(g *Gateway) hookOperatorDeps {
	if g == nil {
		return hookOperatorDeps{}
	}
	return hookOperatorDeps{
		executor: g.hooks,
		runtime:  g.runtime,
	}
}

type operatorHookSurface struct {
	deps hookOperatorDeps
}

func newOperatorHookSurface(deps hookOperatorDeps) *operatorHookSurface {
	return &operatorHookSurface{deps: deps}
}

func (s *operatorHookSurface) RegisterRoutes(mux *http.ServeMux, mountAuthed func(*http.ServeMux, string, func(http.ResponseWriter, *http.Request))) {
	if mux == nil || mountAuthed == nil {
		return
	}
	mountAuthed(mux, "GET /operator/hooks", s.handleHooksList)
	mountAuthed(mux, "GET /operator/hooks/events", s.handleHooksEvents)
	mountAuthed(mux, "POST /operator/hooks", s.handleHooksCreate)
	mountAuthed(mux, "PATCH /operator/hooks/{id}", s.handleHooksUpdate)
	mountAuthed(mux, "DELETE /operator/hooks/{id}", s.handleHooksDelete)
	mountAuthed(mux, "GET /operator/hooks/{id}/results", s.handleHooksResults)
	mountAuthed(mux, "POST /operator/hooks/{id}/fire", s.handleHooksFire)
	mountAuthed(mux, "POST /operator/hooks/{id}/replay", s.handleHooksReplay)
}

func (s *operatorHookSurface) handleHooksList(w http.ResponseWriter, r *http.Request) {
	if s.deps.executor == nil {
		gwError(w, http.StatusServiceUnavailable, "hooks not available")
		return
	}
	authScope := authScopeFromIdentity(AuthIdentityFromContext(r.Context()))
	all, err := s.deps.executor.Store().List(r.Context())
	if err != nil {
		gwError(w, http.StatusInternalServerError, err.Error())
		return
	}
	items := make([]hooks.Hook, 0, len(all))
	for _, h := range all {
		if !hookMatchesAuthScope(authScope, h) {
			continue
		}
		items = append(items, *h)
	}
	gwJSON(w, http.StatusOK, hookListResponse{Items: items, Count: len(items)})
}

func (s *operatorHookSurface) handleHooksEvents(w http.ResponseWriter, _ *http.Request) {
	items := hooks.EventSpecs()
	gwJSON(w, http.StatusOK, hookEventsResponse{Items: items, Count: len(items)})
}

func (s *operatorHookSurface) handleHooksCreate(w http.ResponseWriter, r *http.Request) {
	if s.deps.executor == nil {
		gwError(w, http.StatusServiceUnavailable, "hooks not available")
		return
	}
	var req hookCreateRequest
	if !decodeOperatorStrictJSONBody(w, r, &req) {
		return
	}
	hook := hookFromCreateRequest(req)
	if err := hooks.ValidateHookDefinition(hook); err != nil {
		gwError(w, http.StatusBadRequest, err.Error())
		return
	}

	created, err := s.deps.executor.Store().Add(r.Context(), hook)
	if err != nil {
		gwError(w, http.StatusInternalServerError, err.Error())
		return
	}
	gwJSON(w, http.StatusCreated, hookResponse{Hook: *created})
}

func (s *operatorHookSurface) handleHooksUpdate(w http.ResponseWriter, r *http.Request) {
	if s.deps.executor == nil {
		gwError(w, http.StatusServiceUnavailable, "hooks not available")
		return
	}
	id := r.PathValue("id")
	if id == "" {
		gwError(w, http.StatusBadRequest, "missing hook id")
		return
	}
	var req hookPatchRequest
	if !decodeOperatorStrictJSONBody(w, r, &req) {
		return
	}

	existing, err := s.deps.executor.Store().Get(r.Context(), id)
	if err != nil {
		gwError(w, http.StatusNotFound, err.Error())
		return
	}
	authScope := authScopeFromIdentity(AuthIdentityFromContext(r.Context()))
	if !hookMatchesAuthScope(authScope, existing) {
		gwError(w, http.StatusNotFound, "hook not found")
		return
	}

	if req.Name != nil {
		existing.Name = strings.TrimSpace(*req.Name)
	}
	if req.Enabled != nil {
		existing.Enabled = *req.Enabled
	}
	if req.Priority != nil {
		existing.Priority = *req.Priority
	}
	if req.Phase != nil {
		existing.Phase = *req.Phase
	}
	if req.Filter != nil {
		existing.Filter = strings.TrimSpace(*req.Filter)
	}
	if req.Trigger != nil {
		existing.Trigger = *req.Trigger
	}
	if req.Kind != nil {
		existing.Kind = *req.Kind
	}
	if req.URL != nil {
		existing.URL = strings.TrimSpace(*req.URL)
	}
	if req.Command != nil {
		existing.Command = strings.TrimSpace(*req.Command)
	}
	if req.Headers != nil {
		existing.Headers = normalizeHookHeaders(*req.Headers)
	}
	if req.Timeout != nil {
		existing.Timeout = *req.Timeout
	}
	if req.RetryCount != nil {
		existing.RetryCount = *req.RetryCount
	}
	if req.Async != nil {
		existing.Async = *req.Async
	}
	if req.Secret != nil {
		existing.Secret = strings.TrimSpace(*req.Secret)
	}
	if req.AutomationID != nil {
		existing.AutomationID = strings.TrimSpace(*req.AutomationID)
	}
	normalizeHook(existing)
	if err := hooks.ValidateHookDefinition(*existing); err != nil {
		gwError(w, http.StatusBadRequest, err.Error())
		return
	}

	updated, err := s.deps.executor.Store().Update(r.Context(), *existing)
	if err != nil {
		gwError(w, http.StatusInternalServerError, err.Error())
		return
	}
	gwJSON(w, http.StatusOK, hookResponse{Hook: *updated})
}

func (s *operatorHookSurface) handleHooksDelete(w http.ResponseWriter, r *http.Request) {
	if s.deps.executor == nil {
		gwError(w, http.StatusServiceUnavailable, "hooks not available")
		return
	}
	id := r.PathValue("id")
	if id == "" {
		gwError(w, http.StatusBadRequest, "missing hook id")
		return
	}
	hook, err := getHookScopedFromDeps(s.deps, r)
	if err != nil {
		gwError(w, http.StatusNotFound, err.Error())
		return
	}
	if err := s.deps.executor.Store().Remove(r.Context(), hook.ID); err != nil {
		gwError(w, http.StatusNotFound, err.Error())
		return
	}
	gwJSON(w, http.StatusOK, idOKResponse{OK: true, ID: hook.ID})
}

func (s *operatorHookSurface) handleHooksResults(w http.ResponseWriter, r *http.Request) {
	if s.deps.executor == nil {
		gwError(w, http.StatusServiceUnavailable, "hooks not available")
		return
	}
	hook, err := getHookScopedFromDeps(s.deps, r)
	if err != nil {
		gwError(w, http.StatusNotFound, err.Error())
		return
	}

	results := filterHookResultsForScope(s.deps, r.Context(), authScopeFromIdentity(AuthIdentityFromContext(r.Context())), hook, s.deps.executor.RecentResultsByHook(hook.ID, hookDefaultResultLimit))
	if results == nil {
		results = []hooks.HookResult{}
	}
	gwJSON(w, http.StatusOK, hookResultsResponse{Items: results, Count: len(results)})
}

func (s *operatorHookSurface) handleHooksFire(w http.ResponseWriter, r *http.Request) {
	if s.deps.executor == nil {
		gwError(w, http.StatusServiceUnavailable, "hooks not available")
		return
	}
	id := r.PathValue("id")
	if id == "" {
		gwError(w, http.StatusBadRequest, "missing hook id")
		return
	}
	hook, err := getHookScopedFromDeps(s.deps, r)
	if err != nil {
		gwError(w, http.StatusNotFound, err.Error())
		return
	}
	var req hookFireRequest
	if _, ok := decodeOptionalGatewayJSONBodyDisallowUnknownFields(w, r, &req); !ok {
		return
	}
	if req.Trigger != "" || req.Phase != "" {
		effectiveTrigger := req.Trigger
		if effectiveTrigger == "" {
			effectiveTrigger = hook.Trigger
		}
		effectivePhase := req.Phase
		if effectivePhase == "" {
			effectivePhase = hook.EffectivePhase()
		}
		if err := hooks.ValidateHookInvocation(effectiveTrigger, effectivePhase); err != nil {
			gwError(w, http.StatusBadRequest, err.Error())
			return
		}
	}
	result, err := s.deps.executor.FireHook(r.Context(), hook.ID, req.Trigger, req.Phase, req.Payload)
	if err != nil {
		gwError(w, gatewayHTTPStatusForError(err, http.StatusInternalServerError), err.Error())
		return
	}
	gwJSON(w, http.StatusOK, hookFireResponse{Result: result})
}

func (s *operatorHookSurface) handleHooksReplay(w http.ResponseWriter, r *http.Request) {
	if s.deps.executor == nil {
		gwError(w, http.StatusServiceUnavailable, "hooks not available")
		return
	}
	id := r.PathValue("id")
	if id == "" {
		gwError(w, http.StatusBadRequest, "missing hook id")
		return
	}
	hook, err := getHookScopedFromDeps(s.deps, r)
	if err != nil {
		gwError(w, http.StatusNotFound, err.Error())
		return
	}
	result, err := s.deps.executor.ReplayLatestByHook(r.Context(), hook.ID)
	if err != nil {
		gwError(w, gatewayHTTPStatusForError(err, http.StatusInternalServerError), err.Error())
		return
	}
	gwJSON(w, http.StatusOK, hookFireResponse{Result: result})
}

func getHookScopedFromDeps(deps hookOperatorDeps, r *http.Request) (*hooks.Hook, error) {
	if deps.executor == nil {
		return nil, fmt.Errorf("hooks not available")
	}
	id := strings.TrimSpace(r.PathValue("id"))
	hook, err := deps.executor.Store().Get(r.Context(), id)
	if err != nil {
		return nil, err
	}
	if !hookMatchesAuthScope(authScopeFromIdentity(AuthIdentityFromContext(r.Context())), hook) {
		return nil, fmt.Errorf("hook %s: not found", id)
	}
	return hook, nil
}

func filterHookResultsForScope(deps hookOperatorDeps, ctx context.Context, scope authScope, hook *hooks.Hook, results []hooks.HookResult) []hooks.HookResult {
	if len(results) == 0 {
		return nil
	}
	if scope.isZero() {
		return results
	}
	out := make([]hooks.HookResult, 0, len(results))
	for _, result := range results {
		if hookResultMatchesScope(deps, ctx, scope, hook, result) {
			out = append(out, result)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func hookResultMatchesScope(deps hookOperatorDeps, ctx context.Context, scope authScope, hook *hooks.Hook, result hooks.HookResult) bool {
	if scope.isZero() {
		return true
	}
	scopeFilter := scope.scopeFilter()
	if deps.runtime != nil {
		if runID := strings.TrimSpace(result.RunID); runID != "" {
			_, err := deps.runtime.GetRunScoped(ctx, runID, scopeFilter)
			return err == nil
		}
		if sessionID := strings.TrimSpace(result.SessionID); sessionID != "" {
			_, err := deps.runtime.GetSessionScoped(ctx, sessionID, scopeFilter)
			return err == nil
		}
	}
	if hook != nil {
		return scopeFilter.Matches(hook.Scope())
	}
	return false
}
