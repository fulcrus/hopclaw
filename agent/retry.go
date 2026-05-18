package agent

import (
	"context"
	"fmt"
	"log/slog"
	"math"
	"math/rand/v2"
	"strings"
	"time"

	"github.com/fulcrus/hopclaw/eventbus"
	"github.com/fulcrus/hopclaw/logging"
	"github.com/fulcrus/hopclaw/modelrouter"
)

// RetryConfig configures model call retries.
type RetryConfig struct {
	MaxAttempts int           // Default 3
	MinDelay    time.Duration // Default 500ms
	MaxDelay    time.Duration // Default 30s
	Jitter      float64       // 0-1, default 0.2
}

type retryPolicy struct {
	AllowRetry          bool
	AllowFailover       bool
	AlignToRouterWindow bool
	MinDelay            time.Duration
}

type modelFailoverSelection struct {
	FromModel string
	ToModel   string
	Reason    string
}

const (
	defaultModelRetryAttempts = 4
	defaultModelRetryMinDelay = 500 * time.Millisecond
	defaultModelRetryMaxDelay = 20 * time.Second
	defaultModelRetryJitter   = 0.2
)

// retryModelCall wraps a model call with retry logic.
// Only transient failures are retried. Permanent failures return immediately.
// Reports failure/success to the router and emits EventModelRetry events.
//
// If chatReq is non-nil and uses ThinkingExtended, a thinking-related failure
// (timeout on a thinking-capable model) triggers automatic degradation to
// ThinkingRegular. The chatReq is mutated in-place so the closure sees the
// updated thinking mode on the next iteration.
func (a *AgentComponent) retryModelCall(
	ctx context.Context,
	run *Run,
	session *Session,
	chatModel string,
	chatReq *ChatRequest,
	fn func() (*ModelResponse, error),
) (*ModelResponse, error) {
	cfg := a.config.Retry
	if cfg.MaxAttempts <= 0 {
		cfg.MaxAttempts = defaultModelRetryAttempts
	}
	if cfg.MinDelay <= 0 {
		cfg.MinDelay = defaultModelRetryMinDelay
	}
	if cfg.MaxDelay <= 0 {
		cfg.MaxDelay = defaultModelRetryMaxDelay
	}
	if cfg.Jitter <= 0 || cfg.Jitter > 1 {
		cfg.Jitter = defaultModelRetryJitter
	}

	thinkingDegraded := false

	// retryAttempts counts actual retry attempts (excludes thinking
	// degradation which gets a free re-try). This avoids the fragile
	// attempt-- arithmetic on the loop counter.
	retryAttempts := 0
	var pendingFailovers []modelFailoverSelection

	var lastErr error
	for retryAttempts < cfg.MaxAttempts {
		retryAttempts++

		resp, err := fn()
		if err == nil {
			for _, failover := range pendingFailovers {
				if strings.TrimSpace(safeRunID(run)) == "" || session == nil {
					continue
				}
				logging.LogIfErr(ctx, a.emit(ctx, eventbus.NewModelFailoverEvent(
					safeRunID(run),
					safeSessionID(session),
					eventbus.ModelFailoverAttrs{
						FromModel: failover.FromModel,
						ToModel:   failover.ToModel,
						Reason:    failover.Reason,
					},
					nil,
				)), "emit event failed", slog.String("kind", string(eventbus.EventModelFailover)))
			}
			pendingFailovers = nil
			// Report success to router on successful attempt.
			if a.router != nil && chatModel != "" {
				logging.LogIfErr(ctx, a.router.ReportSuccess(ctx, chatModel),
					"router report success failed", slog.String("model", chatModel))
			}
			return resp, nil
		}

		lastErr = err

		// Classify the error.
		status, message := extractErrorInfo(err)
		reason := modelrouter.ClassifyError(status, message)

		// Check for thinking degradation opportunity before giving up.
		// Degradation gets one free re-try: decrement the counter so the
		// next iteration does not consume a retry budget slot.
		if !thinkingDegraded && chatReq != nil && chatReq.ThinkingMode == ThinkingExtended && isThinkingRelated(reason) {
			thinkingDegraded = true
			chatReq.ThinkingMode = ThinkingRegular

			if strings.TrimSpace(safeRunID(run)) != "" && session != nil {
				logging.LogIfErr(ctx, a.emit(ctx, eventbus.NewThinkingDegradedEvent(
					run.ID,
					session.ID,
					eventbus.ThinkingDegradedAttrs{
						Model:   chatModel,
						From:    string(ThinkingExtended),
						To:      string(ThinkingRegular),
						Reason:  string(reason),
						Error:   err.Error(),
						Attempt: retryAttempts,
					},
					nil,
				)), "emit event failed", slog.String("kind", string(eventbus.EventThinkingDegraded)))
			}

			// Don't count thinking degradation as a retry attempt.
			retryAttempts--
			continue
		}

		policy := retryPolicyForReason(reason)

		// Report failure to router after request-level degradations are exhausted.
		if a.router != nil && chatModel != "" {
			logging.LogIfErr(ctx, a.reportFailureWithReason(ctx, chatModel, reason),
				"router report failure failed", slog.String("model", chatModel))
		}

		// If the current model just entered cooldown/disabled state, try to
		// re-route immediately instead of hammering the same model again.
		if policy.AllowFailover {
			if failover, rerouted := a.failoverRetryModel(ctx, run, session, chatModel, chatReq, reason); rerouted {
				chatModel = failover.ToModel
				pendingFailovers = append(pendingFailovers, failover)
				continue
			}
		}

		// If permanent failure, don't retry the same model.
		if !policy.AllowRetry {
			return nil, err
		}

		// If this was the last attempt, don't sleep.
		if retryAttempts >= cfg.MaxAttempts {
			break
		}

		// Calculate backoff delay with jitter, then grade it by failure type.
		delay := effectiveRetryDelay(retryAttempts, cfg, policy, routerCooldownDelay(a.router, chatModel))

		// Emit retry event.
		if strings.TrimSpace(safeRunID(run)) != "" && session != nil {
			logging.LogIfErr(ctx, a.emit(ctx, eventbus.NewModelRetryEvent(
				run.ID,
				session.ID,
				eventbus.ModelRetryAttrs{
					Model:         chatModel,
					Attempt:       retryAttempts,
					MaxAttempts:   cfg.MaxAttempts,
					FailureReason: string(reason),
					DelayMs:       delay.Milliseconds(),
					Error:         err.Error(),
				},
				nil,
			)), "emit event failed", slog.String("kind", string(eventbus.EventModelRetry)))
		}

		// Wait with context awareness.
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(delay):
		}
	}

	return nil, lastErr
}

func (a *AgentComponent) failoverRetryModel(
	ctx context.Context,
	run *Run,
	session *Session,
	currentModel string,
	chatReq *ChatRequest,
	reason modelrouter.FailureReason,
) (modelFailoverSelection, bool) {
	if a == nil || a.router == nil || chatReq == nil || strings.TrimSpace(currentModel) == "" {
		return modelFailoverSelection{}, false
	}
	if reason == modelrouter.FailureFormat {
		return modelFailoverSelection{}, false
	}

	decision, err := a.router.Select(ctx, modelrouter.RouteRequest{
		RequestedModel:   currentModel,
		Required:         requiredCapabilities(chatReq.Tools),
		MinContextWindow: chatReq.Budget.ContextWindow,
		MinOutputTokens:  chatReq.Budget.ReservedOutput,
	})
	if err != nil || strings.TrimSpace(decision.Model.ID) == "" || decision.Model.ID == currentModel {
		return modelFailoverSelection{}, false
	}

	chatReq.Model = decision.Model.ID
	if run != nil {
		run.Model = decision.Model.ID
	}
	if session != nil {
		session.Model = decision.Model.ID
	}

	reasonText := decision.Reason
	if strings.TrimSpace(reasonText) == "" {
		reasonText = "retry failover after " + string(reason)
	}

	return modelFailoverSelection{
		FromModel: currentModel,
		ToModel:   decision.Model.ID,
		Reason:    reasonText,
	}, true
}

func safeRunID(run *Run) string {
	if run == nil {
		return ""
	}
	return run.ID
}

func safeSessionID(session *Session) string {
	if session == nil {
		return ""
	}
	return session.ID
}

// isThinkingRelated returns true if the failure is likely caused by extended
// thinking — e.g. timeout, overloaded, or thinking-specific error messages.
func isThinkingRelated(reason modelrouter.FailureReason) bool {
	return reason == modelrouter.FailureTimeout || reason == modelrouter.FailureOverloaded
}

func retryPolicyForReason(reason modelrouter.FailureReason) retryPolicy {
	switch reason {
	case modelrouter.FailureRateLimit:
		return retryPolicy{
			AllowRetry:          true,
			AllowFailover:       true,
			AlignToRouterWindow: true,
			MinDelay:            2 * time.Second,
		}
	case modelrouter.FailureOverloaded:
		return retryPolicy{
			AllowRetry:          true,
			AllowFailover:       true,
			AlignToRouterWindow: true,
			MinDelay:            1200 * time.Millisecond,
		}
	case modelrouter.FailureTimeout:
		return retryPolicy{
			AllowRetry:          true,
			AllowFailover:       true,
			AlignToRouterWindow: false,
			MinDelay:            750 * time.Millisecond,
		}
	case modelrouter.FailureUnknown:
		return retryPolicy{
			AllowRetry:          true,
			AllowFailover:       false,
			AlignToRouterWindow: false,
			MinDelay:            1 * time.Second,
		}
	case modelrouter.FailureAuth, modelrouter.FailureAuthPermanent, modelrouter.FailureBilling, modelrouter.FailureModelNotFound:
		return retryPolicy{
			AllowRetry:          false,
			AllowFailover:       true,
			AlignToRouterWindow: false,
		}
	case modelrouter.FailureFormat:
		return retryPolicy{
			AllowRetry:          false,
			AllowFailover:       false,
			AlignToRouterWindow: false,
		}
	default:
		return retryPolicy{
			AllowRetry:          modelrouter.IsTransient(reason),
			AllowFailover:       modelrouter.IsTransient(reason),
			AlignToRouterWindow: false,
			MinDelay:            defaultModelRetryMinDelay,
		}
	}
}

// reportFailureWithReason reports a failure to the router using FailureReason
// when the router supports it, falling back to FailureClass otherwise.
func (a *AgentComponent) reportFailureWithReason(ctx context.Context, modelID string, reason modelrouter.FailureReason) error {
	if a.router == nil {
		return nil
	}
	// Try the extended interface first.
	type reasonReporter interface {
		ReportFailureWithReason(ctx context.Context, modelID string, reason modelrouter.FailureReason) error
	}
	if rr, ok := a.router.(reasonReporter); ok {
		return rr.ReportFailureWithReason(ctx, modelID, reason)
	}
	// Fall back to legacy FailureClass.
	return a.router.ReportFailure(ctx, modelID, reasonToFailureClass(reason))
}

// reasonToFailureClass converts FailureReason back to FailureClass for backward compatibility.
func reasonToFailureClass(reason modelrouter.FailureReason) modelrouter.FailureClass {
	switch reason {
	case modelrouter.FailureRateLimit:
		return modelrouter.FailureRateLimited
	case modelrouter.FailureBilling:
		return modelrouter.FailureQuota
	case modelrouter.FailureOverloaded:
		return modelrouter.FailureUnavailable
	case modelrouter.FailureTimeout:
		return modelrouter.FailureServer
	case modelrouter.FailureFormat, modelrouter.FailureAuth, modelrouter.FailureAuthPermanent, modelrouter.FailureModelNotFound:
		return modelrouter.FailureClient
	default:
		return modelrouter.FailureUnavailable
	}
}

// retryDelay calculates the backoff delay for a given attempt with jitter.
func retryDelay(attempt int, cfg RetryConfig) time.Duration {
	// Exponential backoff: MinDelay * 2^(attempt-1)
	base := float64(cfg.MinDelay) * math.Pow(2, float64(attempt-1))
	// Apply jitter: multiply by (1 - jitter/2 + random*jitter)
	jitterFactor := 1.0 - cfg.Jitter/2 + rand.Float64()*cfg.Jitter
	delay := time.Duration(base * jitterFactor)
	if delay > cfg.MaxDelay {
		delay = cfg.MaxDelay
	}
	if delay < cfg.MinDelay {
		delay = cfg.MinDelay
	}
	return delay
}

func effectiveRetryDelay(attempt int, cfg RetryConfig, policy retryPolicy, cooldown time.Duration) time.Duration {
	delay := retryDelay(attempt, cfg)
	if policy.MinDelay > delay {
		delay = policy.MinDelay
	}
	if policy.AlignToRouterWindow && cooldown > delay {
		delay = cooldown
	}
	if cfg.MaxDelay > 0 && delay > cfg.MaxDelay {
		delay = cfg.MaxDelay
	}
	if delay < cfg.MinDelay {
		delay = cfg.MinDelay
	}
	return delay
}

func routerCooldownDelay(router ModelRouter, modelID string) time.Duration {
	if router == nil || strings.TrimSpace(modelID) == "" {
		return 0
	}
	type failureStatsReader interface {
		GetFailureStats(modelID string) *modelrouter.ProfileFailureStats
	}
	reader, ok := router.(failureStatsReader)
	if !ok {
		return 0
	}
	stats := reader.GetFailureStats(modelID)
	if stats == nil {
		return 0
	}
	now := time.Now().UTC()
	if !stats.CooldownUntil.IsZero() && stats.CooldownUntil.After(now) {
		return stats.CooldownUntil.Sub(now)
	}
	if !stats.DisabledUntil.IsZero() && stats.DisabledUntil.After(now) {
		return stats.DisabledUntil.Sub(now)
	}
	return 0
}

func actualProviderForModel(modelID, fallback string) string {
	modelID = strings.TrimSpace(modelID)
	fallback = strings.TrimSpace(fallback)
	if fallback != "" && strings.HasPrefix(modelID, fallback+"/") {
		return fallback
	}
	if idx := strings.Index(modelID, "/"); idx > 0 {
		return strings.TrimSpace(modelID[:idx])
	}
	if inferred := inferProvider(modelID); inferred != "" {
		return inferred
	}
	return fallback
}

// StatusError is an error type that carries an HTTP status code.
type StatusError struct {
	Status  int
	Message string
}

func (e *StatusError) Error() string {
	return fmt.Sprintf("status %d: %s", e.Status, e.Message)
}

// extractErrorInfo attempts to extract HTTP status and message from an error.
func extractErrorInfo(err error) (int, string) {
	if err == nil {
		return 0, ""
	}
	// Check for StatusError.
	if se, ok := err.(*StatusError); ok {
		return se.Status, se.Message
	}
	// Check for an error that implements a StatusCode() method.
	type statusCoder interface {
		StatusCode() int
	}
	if sc, ok := err.(statusCoder); ok {
		return sc.StatusCode(), err.Error()
	}
	// Fall back to message-only classification.
	return 0, err.Error()
}
