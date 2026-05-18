package hooks

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os/exec"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	domainscope "github.com/fulcrus/hopclaw/internal/domain/scope"
)

// ---------------------------------------------------------------------------
// Constants
// ---------------------------------------------------------------------------

const (
	defaultTimeout             = 30 // seconds
	maxRecentResults           = 100
	shutdownTimeout            = 30 * time.Second
	maxPayloadPreviewKeys      = 12
	maxPayloadPreviewStringLen = 180

	resultStatusOK    = "ok"
	resultStatusError = "error"

	// hookContextPayloadKey is the key used to inject HookContext into the payload.
	hookContextPayloadKey = "_hook_context"

	// hmacSignatureHeader is the HTTP header carrying the HMAC-SHA256 signature.
	hmacSignatureHeader = "X-HopClaw-Signature"
	// hmacTimestampHeader is the HTTP header carrying the signing timestamp.
	hmacTimestampHeader = "X-HopClaw-Timestamp"
)

var (
	ErrNoReplayPayload = errors.New("no replay payload available")

	payloadMaskKeyFragments    = []string{"secret", "token", "password", "authorization", "cookie", "api_key", "apikey"}
	payloadURLPattern          = regexp.MustCompile(`https?://\S+`)
	payloadPreviewPriorityKeys = []string{
		"run_id",
		"session_id",
		"session_key",
		"tool_name",
		"tool_call_id",
		"result_status",
		"result_summary",
		"error",
		"scope",
		"status",
		"phase",
	}
)

// ---------------------------------------------------------------------------
// Executor
// ---------------------------------------------------------------------------

// Executor finds enabled hooks that match a trigger and runs them.
type Executor struct {
	store   Store
	client  *http.Client
	mu      sync.Mutex     // guards results
	results []HookResult   // circular buffer, max maxRecentResults
	pos     int            // next write position in the circular buffer
	count   int            // total items written (used to know if buffer wrapped)
	wg      sync.WaitGroup // tracks in-flight async hook goroutines
}

// NewExecutor creates an Executor backed by the given store.
func NewExecutor(store Store) *Executor {
	return &Executor{
		store:   store,
		client:  &http.Client{},
		results: make([]HookResult, maxRecentResults),
	}
}

// Store returns the underlying hook store.
func (e *Executor) Store() Store {
	return e.store
}

// FireHook executes one hook directly for operator-driven testing.
// It records the result into recent history and always runs synchronously.
func (e *Executor) FireHook(ctx context.Context, hookID string, trigger TriggerEvent, phase HookPhase, payload map[string]any) (HookResult, error) {
	hook, err := e.store.Get(ctx, hookID)
	if err != nil {
		return HookResult{}, err
	}
	if trigger == "" {
		trigger = hook.Trigger
	}
	if phase == "" {
		phase = hook.EffectivePhase()
	}
	if err := ValidateHookInvocation(trigger, phase); err != nil {
		return HookResult{}, err
	}
	result := e.fireSingle(ctx, hook, trigger, phase, payload, true)
	return result, nil
}

// ReplayLatestByHook re-executes the most recent replayable payload for a hook.
func (e *Executor) ReplayLatestByHook(ctx context.Context, hookID string) (HookResult, error) {
	hook, err := e.store.Get(ctx, hookID)
	if err != nil {
		return HookResult{}, err
	}
	results := e.RecentResultsByHook(hookID, 0)
	for _, result := range results {
		if len(result.replayPayload) == 0 {
			continue
		}
		trigger := result.Trigger
		if trigger == "" {
			trigger = hook.Trigger
		}
		phase := result.Phase
		if phase == "" {
			phase = hook.EffectivePhase()
		}
		return e.fireSingle(ctx, hook, trigger, phase, result.replayPayload, true), nil
	}
	return HookResult{}, ErrNoReplayPayload
}

// CanReplayByHook reports whether a hook has any replayable recent execution.
func (e *Executor) CanReplayByHook(hookID string) bool {
	results := e.RecentResultsByHook(hookID, 0)
	for _, result := range results {
		if len(result.replayPayload) > 0 {
			return true
		}
	}
	return false
}

// LatestResultByHook returns the newest recorded result for a hook.
func (e *Executor) LatestResultByHook(hookID string) *HookResult {
	results := e.RecentResultsByHook(hookID, 1)
	if len(results) == 0 {
		return nil
	}
	result := results[0]
	return &result
}

// Fire triggers all matching hooks for the given event and phase.
// It filters hooks by trigger AND phase, sorts by priority (ascending),
// evaluates filter expressions, and executes each matching hook.
// For HookPhasePre: if any hook returns an error, execution stops early.
func (e *Executor) Fire(ctx context.Context, trigger TriggerEvent, phase HookPhase, payload map[string]any) []HookResult {
	if phase == "" {
		phase = HookPhasePost
	}

	hooks, err := e.store.List(ctx)
	if err != nil {
		return nil
	}

	var matched []*Hook
	for _, h := range hooks {
		if !h.Enabled || h.Trigger != trigger {
			continue
		}
		if h.EffectivePhase() != phase {
			continue
		}
		if !h.MatchesScope(hookPayloadScope(payload)) {
			continue
		}
		if h.Filter != "" && !EvaluateFilter(h.Filter, payload) {
			continue
		}
		matched = append(matched, h)
	}
	if len(matched) == 0 {
		return nil
	}

	// Sort by effective priority ascending (lower = runs first).
	sort.Slice(matched, func(i, j int) bool {
		return matched[i].EffectivePriority() < matched[j].EffectivePriority()
	})

	now := time.Now().UTC()
	out := make([]HookResult, 0, len(matched))
	for _, h := range matched {
		result, completed := e.fireSingleWithTime(ctx, h, trigger, phase, payload, false, now)
		if !completed {
			continue
		}
		out = append(out, result)

		// For pre-phase hooks: abort on first error so the action can be prevented.
		if phase == HookPhasePre && result.Status == resultStatusError {
			return out
		}
	}
	return out
}

func hookPayloadScope(payload map[string]any) domainscope.Ref {
	if len(payload) == 0 {
		return domainscope.Ref{}
	}
	return domainscope.FromValue(payload["scope"])
}

// RecentResults returns up to limit most-recent hook results, newest first.
func (e *Executor) RecentResults(limit int) []HookResult {
	e.mu.Lock()
	defer e.mu.Unlock()

	total := e.count
	if total > maxRecentResults {
		total = maxRecentResults
	}
	if limit <= 0 || limit > total {
		limit = total
	}
	if limit == 0 {
		return nil
	}

	out := make([]HookResult, limit)
	// Read backwards from the most recently written position.
	readPos := (e.pos - 1 + maxRecentResults) % maxRecentResults
	for i := range limit {
		out[i] = e.results[readPos]
		readPos = (readPos - 1 + maxRecentResults) % maxRecentResults
	}
	return out
}

// Shutdown waits for all in-flight async hooks to complete, up to the
// shutdown timeout. It returns nil when all hooks finish, or a context
// deadline error if they do not complete in time.
func (e *Executor) Shutdown() error {
	done := make(chan struct{})
	go func() {
		e.wg.Wait()
		close(done)
	}()
	select {
	case <-done:
		return nil
	case <-time.After(shutdownTimeout):
		return fmt.Errorf("shutdown timed out after %v waiting for async hooks", shutdownTimeout)
	}
}

// ---------------------------------------------------------------------------
// Internal
// ---------------------------------------------------------------------------

func (e *Executor) executeHook(ctx context.Context, h *Hook, trigger TriggerEvent, phase HookPhase, payload map[string]any) HookResult {
	timeout := time.Duration(h.Timeout) * time.Second
	if h.Timeout <= 0 {
		timeout = time.Duration(defaultTimeout) * time.Second
	}

	attempts := h.RetryCount + 1 // first attempt + retries
	if attempts < 1 {
		attempts = 1
	}

	start := time.Now()
	base := buildHookResultBase(h, trigger, phase, payload, start)
	var lastErr error
	for i := range attempts {
		_ = i
		var err error
		switch h.Kind {
		case KindHTTP:
			err = e.fireHTTP(ctx, h, payload, timeout)
		case KindCommand:
			err = e.fireCommand(ctx, h, payload, timeout)
		default:
			err = fmt.Errorf("unsupported hook kind %q", h.Kind)
		}
		if err == nil {
			base.Status = resultStatusOK
			base.Duration = time.Since(start)
			base.AttemptCount = i + 1
			base.Summary = buildHookResultSummary(base)
			return base
		}
		lastErr = err
	}

	base.Status = resultStatusError
	base.Duration = time.Since(start)
	base.Error = lastErr.Error()
	base.AttemptCount = attempts
	base.Summary = buildHookResultSummary(base)
	return base
}

func (e *Executor) fireHTTP(ctx context.Context, h *Hook, payload map[string]any, timeout time.Duration) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal payload: %w", err)
	}

	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, h.URL, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	for k, v := range h.Headers {
		req.Header.Set(k, v)
	}

	// HMAC-SHA256 signing: sign "timestamp.body" to prevent replay attacks.
	if h.Secret != "" {
		timestamp := strconv.FormatInt(time.Now().Unix(), 10)
		signed := computeHMAC(h.Secret, timestamp, body)
		req.Header.Set(hmacSignatureHeader, "sha256="+signed)
		req.Header.Set(hmacTimestampHeader, timestamp)
	}

	resp, err := e.client.Do(req)
	if err != nil {
		return fmt.Errorf("http post: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= http.StatusBadRequest {
		return fmt.Errorf("http status %d", resp.StatusCode)
	}
	return nil
}

// computeHMAC returns the hex-encoded HMAC-SHA256 of "timestamp.body" using
// the provided secret as the key.
func computeHMAC(secret, timestamp string, body []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(timestamp))
	mac.Write([]byte("."))
	mac.Write(body)
	return hex.EncodeToString(mac.Sum(nil))
}

func (e *Executor) fireCommand(ctx context.Context, h *Hook, payload map[string]any, timeout time.Duration) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal payload: %w", err)
	}

	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "sh", "-c", h.Command)
	cmd.Stdin = bytes.NewReader(body)

	output, err := cmd.CombinedOutput()
	if err != nil {
		if len(output) > 0 {
			return fmt.Errorf("command failed: %w: %s", err, string(output))
		}
		return fmt.Errorf("command failed: %w", err)
	}
	return nil
}

func (e *Executor) recordResult(r HookResult) {
	e.mu.Lock()
	defer e.mu.Unlock()

	e.results[e.pos] = r
	e.pos = (e.pos + 1) % maxRecentResults
	e.count++
}

// RecentResultsByHook returns up to limit most-recent results for a specific hook.
func (e *Executor) RecentResultsByHook(hookID string, limit int) []HookResult {
	all := e.RecentResults(0)
	var filtered []HookResult
	for _, r := range all {
		if r.HookID == hookID {
			filtered = append(filtered, r)
			if limit > 0 && len(filtered) >= limit {
				break
			}
		}
	}
	return filtered
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// payloadString extracts a string value from a payload map, returning "" if
// the key is absent or not a string.
func payloadString(payload map[string]any, key string) string {
	if payload == nil {
		return ""
	}
	v, ok := payload[key]
	if !ok {
		return ""
	}
	s, ok := v.(string)
	if !ok {
		return ""
	}
	return s
}

// clonePayload returns a shallow copy of a payload map.
func clonePayload(payload map[string]any) map[string]any {
	if payload == nil {
		return map[string]any{}
	}
	out := make(map[string]any, len(payload)+1)
	for k, v := range payload {
		out[k] = v
	}
	return out
}

func buildHookResultBase(h *Hook, trigger TriggerEvent, phase HookPhase, payload map[string]any, executedAt time.Time) HookResult {
	replayPayload := cloneReplayPayload(payload)
	return HookResult{
		HookID:         h.ID,
		HookName:       h.Name,
		HookKind:       h.Kind,
		TargetLabel:    hookTargetLabel(h),
		Trigger:        trigger,
		Phase:          phase,
		Async:          h.Async && phase != HookPhasePre,
		SessionID:      payloadString(payload, "session_id"),
		RunID:          payloadString(payload, "run_id"),
		ToolName:       payloadString(payload, "tool_name"),
		PayloadPreview: sanitizePayloadPreview(replayPayload),
		ExecutedAt:     executedAt,
		replayPayload:  replayPayload,
	}
}

func buildHookResultSummary(result HookResult) string {
	parts := make([]string, 0, 6)
	if result.Trigger != "" {
		parts = append(parts, string(result.Trigger))
	}
	if result.Phase != "" {
		parts = append(parts, string(result.Phase))
	}
	if result.ToolName != "" {
		parts = append(parts, "tool "+result.ToolName)
	}
	if result.RunID != "" {
		parts = append(parts, "run "+result.RunID)
	}
	if result.SessionID != "" {
		parts = append(parts, "session "+result.SessionID)
	}
	if result.Duration > 0 {
		parts = append(parts, result.Duration.String())
	}
	return strings.Join(parts, " · ")
}

func hookTargetLabel(h *Hook) string {
	if h == nil {
		return ""
	}
	switch h.Kind {
	case KindHTTP:
		return h.URL
	case KindCommand:
		return h.Command
	default:
		return ""
	}
}

func (e *Executor) fireSingle(ctx context.Context, h *Hook, trigger TriggerEvent, phase HookPhase, payload map[string]any, forceSync bool) HookResult {
	result, _ := e.fireSingleWithTime(ctx, h, trigger, phase, payload, forceSync, time.Now().UTC())
	return result
}

func (e *Executor) fireSingleWithTime(ctx context.Context, h *Hook, trigger TriggerEvent, phase HookPhase, payload map[string]any, forceSync bool, triggerTime time.Time) (HookResult, bool) {
	enriched := enrichHookPayload(payload, phase, triggerTime)
	isAsync := h.Async && phase != HookPhasePre && !forceSync
	if isAsync {
		hook := h
		e.wg.Add(1)
		go func() {
			defer e.wg.Done()
			result := e.executeHook(ctx, hook, trigger, phase, enriched)
			e.recordResult(result)
		}()
		return HookResult{}, false
	}
	result := e.executeHook(ctx, h, trigger, phase, enriched)
	e.recordResult(result)
	return result, true
}

func enrichHookPayload(payload map[string]any, phase HookPhase, triggerTime time.Time) map[string]any {
	hctx := HookContext{
		SessionID:   payloadString(payload, "session_id"),
		RunID:       payloadString(payload, "run_id"),
		ToolName:    payloadString(payload, "tool_name"),
		Phase:       phase,
		TriggerTime: triggerTime,
		Payload:     payload,
	}
	enriched := clonePayload(payload)
	enriched[hookContextPayloadKey] = hctx
	return enriched
}

func cloneReplayPayload(payload map[string]any) map[string]any {
	if payload == nil {
		return map[string]any{}
	}
	out := make(map[string]any, len(payload))
	for k, v := range payload {
		if k == hookContextPayloadKey {
			continue
		}
		out[k] = v
	}
	return out
}

func sanitizePayloadPreview(payload map[string]any) map[string]any {
	if len(payload) == 0 {
		return nil
	}
	out := make(map[string]any)
	count := 0
	for _, key := range payloadPreviewPriorityKeys {
		value, ok := payload[key]
		if !ok {
			continue
		}
		if count >= maxPayloadPreviewKeys {
			break
		}
		out[key] = sanitizePayloadValue(key, value)
		count++
	}
	if count < maxPayloadPreviewKeys {
		keys := make([]string, 0, len(payload))
		for key := range payload {
			if _, ok := out[key]; ok {
				continue
			}
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			if count >= maxPayloadPreviewKeys {
				break
			}
			out[key] = sanitizePayloadValue(key, payload[key])
			count++
		}
	}
	if len(payload) > count {
		out["_truncated"] = fmt.Sprintf("%d more field(s)", len(payload)-count)
	}
	return out
}

func sanitizePayloadValue(key string, value any) any {
	lowerKey := strings.ToLower(strings.TrimSpace(key))
	for _, fragment := range payloadMaskKeyFragments {
		if strings.Contains(lowerKey, fragment) {
			return "[redacted]"
		}
	}
	switch typed := value.(type) {
	case string:
		return sanitizePayloadString(typed)
	case fmt.Stringer:
		return sanitizePayloadString(typed.String())
	case []byte:
		return sanitizePayloadString(string(typed))
	case map[string]any:
		return map[string]any{"_object": fmt.Sprintf("%d field(s)", len(typed))}
	case []any:
		return map[string]any{"_array": fmt.Sprintf("%d item(s)", len(typed))}
	default:
		return value
	}
}

func sanitizePayloadString(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	value = payloadURLPattern.ReplaceAllString(value, "<url>")
	if len(value) > maxPayloadPreviewStringLen {
		return value[:maxPayloadPreviewStringLen] + "…"
	}
	return value
}
