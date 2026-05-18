package watch

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/fulcrus/hopclaw/agent"
	"github.com/fulcrus/hopclaw/automation"
	browserclient "github.com/fulcrus/hopclaw/browserapi/client"
	"github.com/fulcrus/hopclaw/cron"
	"github.com/fulcrus/hopclaw/internal/support/normalize"
	"github.com/fulcrus/hopclaw/logging"
	"github.com/fulcrus/hopclaw/resultmodel"
	runtimesvc "github.com/fulcrus/hopclaw/runtime"
)

var log = logging.WithSubsystem("watch")

const (
	maxTimerDuration     = 24 * time.Hour
	minTimerDelay        = 1 * time.Millisecond
	httpProbeTimeout     = 15 * time.Second
	httpProbeBodySize    = 256 << 10
	executionTimeout     = 10 * time.Minute
	maxConsecutiveErrors = 10
)

type Service struct {
	store            *Store
	runner           *automation.Runner
	verifier         RuntimeVerifier
	executionTimeout time.Duration
	pollInterval     time.Duration
	email            EmailConfig
	calendar         CalendarConfig
	sessionReader    SessionInboxReader
	browserClient    *browserclient.Client
	channels         ChannelDeliverer

	mu      sync.Mutex
	running bool
	cancel  context.CancelFunc
	rearmCh chan struct{}
}

func NewService(store *Store, runtime RuntimeSubmitter, opts ...Option) *Service {
	var verifier RuntimeVerifier
	if typed, ok := runtime.(RuntimeVerifier); ok {
		verifier = typed
	}
	svc := &Service{
		store:            store,
		verifier:         verifier,
		executionTimeout: executionTimeout,
		pollInterval:     3 * time.Second,
		rearmCh:          make(chan struct{}, 1),
	}
	for _, opt := range opts {
		if opt != nil {
			opt(svc)
		}
	}
	svc.runner = automation.NewRunner(runtime, svc.executionTimeout, svc.pollInterval)
	return svc
}

func (s *Service) Store() *Store {
	return s.store
}

func (s *Service) IsRunning() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.running
}

func (s *Service) Start(ctx context.Context) error {
	s.mu.Lock()
	if s.running {
		s.mu.Unlock()
		return fmt.Errorf("watch service is already running")
	}
	loopCtx, cancel := context.WithCancel(ctx)
	s.cancel = cancel
	s.running = true
	s.mu.Unlock()

	s.seedNextCheckTimes()
	go s.loop(loopCtx)
	return nil
}

func (s *Service) Stop() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.running {
		return nil
	}
	s.cancel()
	s.running = false
	return nil
}

func (s *Service) Rearm() {
	select {
	case s.rearmCh <- struct{}{}:
	default:
	}
}

func (s *Service) Trigger(ctx context.Context, id string) error {
	item, err := s.store.Get(id)
	if err != nil {
		return err
	}
	go s.executeAndUpdate(ctx, item)
	return nil
}

func (s *Service) loop(ctx context.Context) {
	timer := time.NewTimer(s.nextDelay())
	defer timer.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-s.rearmCh:
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
			timer.Reset(s.nextDelay())
		case <-timer.C:
			s.fireDue(ctx)
			timer.Reset(s.nextDelay())
		}
	}
}

func (s *Service) seedNextCheckTimes() {
	now := time.Now().UTC()
	changed := false
	for _, item := range s.store.List() {
		if !item.Enabled || !item.NextCheckAt.IsZero() {
			continue
		}
		changed = true
		_ = s.store.Update(item.ID, func(w *Watch) {
			w.NextCheckAt = now
			w.UpdatedAt = now
		})
	}
	if changed {
		_ = s.store.Save()
	}
}

func (s *Service) fireDue(ctx context.Context) {
	now := time.Now().UTC()
	items := s.store.List()
	for i := range items {
		item := items[i]
		if !item.Enabled || item.NextCheckAt.IsZero() || item.NextCheckAt.After(now) {
			continue
		}
		if !item.BackoffUntil.IsZero() && item.BackoffUntil.After(now) {
			continue
		}
		w := item
		s.executeAndUpdate(ctx, &w)
	}
}

func (s *Service) executeAndUpdate(ctx context.Context, item *Watch) {
	now := time.Now().UTC()
	result := s.check(ctx, item)
	nextCheck := now.Add(parseIntervalOrDefault(item.Interval))
	normalized := result.Normalized()
	consecutiveErrors := 0
	backoffUntil := time.Time{}
	autoDisable := false
	if normalized.Status == resultmodel.AutomationStatusError {
		consecutiveErrors = item.ConsecutiveErrors + 1
		autoDisable = consecutiveErrors >= maxConsecutiveErrors
		if !autoDisable {
			backoffUntil = now.Add(cron.ComputeBackoff(consecutiveErrors))
		}
	}

	if item.Delivery != nil {
		s.deliverResult(ctx, item, normalized)
	}

	_ = s.store.Update(item.ID, func(w *Watch) {
		w.LastCheckedAt = now
		w.LastStatus = string(normalized.Status)
		w.LastSummary = normalized.Summary
		w.LastError = normalized.ErrorMessage()
		w.LastVerificationStatus = verificationStatus(normalized.Verification)
		w.LastVerificationSummary = verificationSummary(normalized.Verification)
		w.LastResult = resultmodel.CloneAutomationResult(&normalized)
		w.UpdatedAt = now
		if normalized.Fingerprint != "" {
			w.LastFingerprint = normalized.Fingerprint
		}
		if normalized.RunID != "" {
			w.LastRunID = normalized.RunID
			w.LastTriggeredAt = now
		}
		if normalized.Status == resultmodel.AutomationStatusError {
			w.ConsecutiveErrors = consecutiveErrors
			w.BackoffUntil = backoffUntil
		} else {
			w.ConsecutiveErrors = 0
			w.BackoffUntil = time.Time{}
		}
		if autoDisable {
			w.Enabled = false
			w.NextCheckAt = time.Time{}
			w.BackoffUntil = time.Time{}
			return
		}
		w.NextCheckAt = nextCheck
	})
	_ = s.store.Save()
}

func (s *Service) deliverResult(ctx context.Context, item *Watch, result resultmodel.AutomationResult) {
	if s == nil || s.channels == nil || item == nil || item.Delivery == nil {
		return
	}
	channel := strings.TrimSpace(item.Delivery.Channel)
	target := strings.TrimSpace(item.Delivery.Target)
	if channel == "" || target == "" {
		return
	}

	content := ""
	if shouldDeliverWatchResult(result) {
		content = watchDeliveryContent(item, result)
	} else if watchVerificationFailed(result.Verification) {
		content = watchVerificationFailureContent(item, result)
	}
	if strings.TrimSpace(content) == "" {
		return
	}
	attemptAt := time.Now().UTC()
	if err := s.channels.DeliverMessage(ctx, *item.Delivery, content); err != nil {
		if recordErr := s.recordNotificationAttempt(item.ID, attemptAt, err); recordErr != nil {
			log.Warn("watch: record notification stats failed",
				"watch_id", item.ID,
				"error", recordErr,
			)
		}
		log.Warn("watch: delivery failed",
			"watch_id", item.ID,
			"channel", channel,
			"target", target,
			"error", err,
		)
		return
	}
	if recordErr := s.recordNotificationAttempt(item.ID, attemptAt, nil); recordErr != nil {
		log.Warn("watch: record notification stats failed",
			"watch_id", item.ID,
			"error", recordErr,
		)
	}
}

func (s *Service) check(ctx context.Context, item *Watch) resultmodel.AutomationResult {
	if item == nil {
		return watchErrorResult("", "", "watch is required")
	}
	if err := Validate(*item); err != nil {
		return watchErrorResult("", "", err.Error())
	}
	observation, err := s.probe(ctx, item.Source)
	if err != nil {
		return watchErrorResult("", "", err.Error())
	}
	fingerprint := observation.Fingerprint
	summary := observation.Summary

	if strings.TrimSpace(item.LastFingerprint) == "" {
		if !item.FireOnStart {
			return resultmodel.AutomationResult{
				Source:      resultmodel.AutomationSourceWatch,
				Status:      resultmodel.AutomationStatusPrimed,
				Summary:     summary,
				Fingerprint: fingerprint,
			}.Normalized()
		}
	} else if item.LastFingerprint == fingerprint {
		return resultmodel.AutomationResult{
			Source:      resultmodel.AutomationSourceWatch,
			Status:      resultmodel.AutomationStatusUnchanged,
			Summary:     summary,
			Fingerprint: fingerprint,
		}.Normalized()
	}

	if s.runner == nil {
		return watchErrorResult(summary, fingerprint, "runtime submitter is not configured")
	}
	runtimeResult := watchRuntimeResult(s.run(ctx, item, observation))
	if runtimeResult.RunID == "" {
		runtimeResult.RunID = strings.TrimSpace(item.LastRunID)
	}
	if runtimeResult.Verification == nil {
		if status, verificationSummary := s.lookupVerification(ctx, runtimeResult.RunID); status != "" || verificationSummary != "" {
			runtimeResult.Verification = &resultmodel.ResultVerification{
				Status:  resultmodel.VerificationStatus(status),
				Summary: verificationSummary,
			}
		}
	}
	watchResult := runtimeResult.Normalized()
	watchResult.Source = resultmodel.AutomationSourceWatch
	watchResult.Fingerprint = fingerprint
	watchResult.Summary = normalize.FirstNonEmpty(verificationSummary(watchResult.Verification), watchResult.Summary, summary)

	switch watchResult.Status {
	case resultmodel.AutomationStatusOK:
		watchResult.Status = resultmodel.AutomationStatusTriggered
		return watchResult.Normalized()
	case resultmodel.AutomationStatusSkipped:
		if watchResult.Error == nil {
			watchResult.Error = &resultmodel.ResultError{Message: "run was cancelled"}
		}
		return watchResult.Normalized()
	case resultmodel.AutomationStatusError:
		if watchResult.Error == nil {
			watchResult.Error = &resultmodel.ResultError{Message: normalize.FirstNonEmpty(strings.TrimSpace(watchResult.RunStatus), "runtime execution failed")}
		}
		return watchResult.Normalized()
	case resultmodel.AutomationStatusPending:
		if watchResult.Error == nil {
			watchResult.Error = &resultmodel.ResultError{Message: "runtime returned non-terminal result"}
		}
		watchResult.Status = resultmodel.AutomationStatusError
		return watchResult.Normalized()
	default:
		if watchResult.Error == nil && watchResult.RunID == "" {
			return watchErrorResult(summary, fingerprint, "runtime returned nil result")
		}
		watchResult.Status = resultmodel.AutomationStatusTriggered
		return watchResult.Normalized()
	}
}

func (s *Service) lookupVerification(ctx context.Context, runID string) (string, string) {
	if s == nil || s.verifier == nil || strings.TrimSpace(runID) == "" {
		return "", ""
	}
	verification, err := s.verifier.GetRunVerification(ctx, runID)
	if err != nil || verification == nil {
		return "", ""
	}
	return strings.TrimSpace(string(verification.Status)), strings.TrimSpace(verification.Summary)
}

func watchRuntimeResult(result *runtimesvc.RunResult) resultmodel.AutomationResult {
	if result == nil {
		return resultmodel.AutomationResult{
			Source: resultmodel.AutomationSourceWatch,
			Status: resultmodel.AutomationStatusError,
			Error:  &resultmodel.ResultError{Message: "runtime returned nil result"},
		}
	}
	if result.Canonical.Populated() {
		return result.Canonical.Normalized()
	}
	out := resultmodel.AutomationResult{
		Source:    resultmodel.AutomationSourceRuntime,
		RunID:     strings.TrimSpace(result.RunID),
		RunStatus: strings.TrimSpace(string(result.Status)),
		Summary:   strings.TrimSpace(result.Summary),
		Output:    strings.TrimSpace(result.Output),
		Actions:   append([]resultmodel.ResultAction(nil), result.NextActions...),
	}
	if errText := strings.TrimSpace(result.Error); errText != "" {
		out.Error = &resultmodel.ResultError{Message: errText}
	}
	if len(result.Deliverables) > 0 {
		out.Artifacts = make([]resultmodel.ResultArtifact, 0, len(result.Deliverables))
		for _, item := range result.Deliverables {
			out.Artifacts = append(out.Artifacts, resultmodel.ResultArtifact{
				Kind:        strings.TrimSpace(item.Kind),
				Name:        strings.TrimSpace(item.Name),
				URI:         strings.TrimSpace(item.URI),
				ContentType: strings.TrimSpace(item.ContentType),
				SizeBytes:   item.SizeBytes,
				PreviewText: strings.TrimSpace(item.PreviewText),
			})
		}
	}
	return out.Normalized()
}

func watchErrorResult(summary, fingerprint, errText string) resultmodel.AutomationResult {
	return resultmodel.AutomationResult{
		Source:      resultmodel.AutomationSourceWatch,
		Status:      resultmodel.AutomationStatusError,
		Summary:     strings.TrimSpace(summary),
		Fingerprint: strings.TrimSpace(fingerprint),
		Error:       &resultmodel.ResultError{Message: strings.TrimSpace(errText)},
	}.Normalized()
}

func verificationStatus(result *resultmodel.ResultVerification) string {
	if result == nil {
		return ""
	}
	return strings.TrimSpace(string(result.Status))
}

func verificationSummary(result *resultmodel.ResultVerification) string {
	if result == nil {
		return ""
	}
	return strings.TrimSpace(result.Summary)
}

func watchVerificationFailed(result *resultmodel.ResultVerification) bool {
	if result == nil {
		return false
	}
	return strings.EqualFold(strings.TrimSpace(string(result.Status)), "failed")
}

func shouldDeliverWatchResult(result resultmodel.AutomationResult) bool {
	switch result.Status {
	case resultmodel.AutomationStatusTriggered, resultmodel.AutomationStatusOK:
		return true
	default:
		return false
	}
}

func watchDeliveryContent(item *Watch, result resultmodel.AutomationResult) string {
	name := strings.TrimSpace(item.Name)
	if name == "" {
		name = strings.TrimSpace(item.ID)
	}
	parts := []string{
		fmt.Sprintf("[watch:%s] %s", strings.TrimSpace(item.ID), name),
	}
	if summary := strings.TrimSpace(result.Summary); summary != "" {
		parts = append(parts, summary)
	}
	if output := strings.TrimSpace(result.Output); output != "" {
		parts = append(parts, output)
	}
	if len(result.Artifacts) > 0 {
		uris := make([]string, 0, len(result.Artifacts))
		for _, artifact := range result.Artifacts {
			uri := strings.TrimSpace(artifact.URI)
			if uri != "" {
				uris = append(uris, uri)
			}
		}
		if len(uris) > 0 {
			parts = append(parts, "Artifacts: "+strings.Join(uris, ", "))
		}
	}
	return strings.TrimSpace(strings.Join(parts, "\n\n"))
}

func watchVerificationFailureContent(item *Watch, result resultmodel.AutomationResult) string {
	name := strings.TrimSpace(item.Name)
	if name == "" {
		name = strings.TrimSpace(item.ID)
	}
	summary := verificationSummary(result.Verification)
	if summary == "" {
		summary = normalize.FirstNonEmpty(strings.TrimSpace(result.Summary), "verification failed")
	}
	return strings.TrimSpace(fmt.Sprintf("[watch:%s] %s\n\n%s", strings.TrimSpace(item.ID), name, summary))
}

func (s *Service) recordNotificationAttempt(watchID string, attemptAt time.Time, deliverErr error) error {
	if s == nil || s.store == nil || strings.TrimSpace(watchID) == "" {
		return nil
	}
	if err := s.store.Update(watchID, func(w *Watch) {
		w.Notifications = automation.RecordNotification(w.Notifications, attemptAt, deliverErr == nil, errorText(deliverErr))
		w.UpdatedAt = time.Now().UTC()
	}); err != nil {
		return err
	}
	return s.store.Save()
}

func errorText(err error) string {
	if err == nil {
		return ""
	}
	return strings.TrimSpace(err.Error())
}

func (s *Service) run(ctx context.Context, item *Watch, observation Observation) *runtimesvc.RunResult {
	sessionKey := strings.TrimSpace(item.SessionKey)
	if sessionKey == "" {
		sessionKey = "watch:" + item.ID
	}
	automationID := strings.TrimSpace(item.AutomationID)
	if automationID == "" {
		automationID = item.ID
	}
	result, err := s.runner.Run(ctx, automation.SubmitRequest{
		SessionKey:   sessionKey,
		Content:      buildWatchPrompt(item, observation),
		Model:        strings.TrimSpace(item.Model),
		AutomationID: automationID,
		Metadata: map[string]any{
			"automation_kind": "watch",
			"automation_id":   item.ID,
			"automation_name": strings.TrimSpace(item.Name),
			"watch_source":    strings.TrimSpace(item.Source.Kind),
		},
	})
	if err != nil {
		return &runtimesvc.RunResult{
			Status: agent.RunFailed,
			Error:  fmt.Sprintf("execution failed: %v", err),
		}
	}
	return result
}

func (s *Service) nextDelay() time.Duration {
	now := time.Now().UTC()
	var earliest time.Time
	for _, item := range s.store.List() {
		if !item.Enabled || item.NextCheckAt.IsZero() {
			continue
		}
		readyAt := item.NextCheckAt
		if !item.BackoffUntil.IsZero() && item.BackoffUntil.After(readyAt) {
			readyAt = item.BackoffUntil
		}
		if earliest.IsZero() || readyAt.Before(earliest) {
			earliest = readyAt
		}
	}
	if earliest.IsZero() {
		return maxTimerDuration
	}
	delay := earliest.Sub(now)
	if delay <= 0 {
		return minTimerDelay
	}
	if delay > maxTimerDuration {
		return maxTimerDuration
	}
	return delay
}

type Observation struct {
	Summary     string
	Body        string
	Fingerprint string
}

func Validate(item Watch) error {
	if parseIntervalOrDefault(item.Interval) <= 0 {
		return ErrInvalidInterval
	}
	if item.Delivery != nil {
		channel := strings.TrimSpace(item.Delivery.Channel)
		target := strings.TrimSpace(item.Delivery.Target)
		if channel == "" || target == "" {
			return fmt.Errorf("delivery requires both channel and target")
		}
	}
	driver, err := validateDriver(item.Source.Kind)
	if err != nil {
		return err
	}
	return driver.Validate(item.Source)
}

func (s *Service) probe(ctx context.Context, source Source) (Observation, error) {
	driver, err := s.getDriver(source.Kind)
	if err != nil {
		return Observation{}, err
	}
	return driver.Probe(ctx, source)
}

func buildWatchPrompt(item *Watch, observation Observation) string {
	sourceLabel := ""
	if item != nil {
		if driver, err := describeDriver(item.Source.Kind); err == nil {
			sourceLabel = driver.Describe(item.Source)
		}
	}
	body := observation.Body
	if len(body) > 4000 {
		body = strings.TrimSpace(body[:4000]) + "\n[truncated]"
	}
	base := strings.TrimSpace(item.Prompt)
	if base == "" {
		base = "The watched source changed. Summarize what changed and highlight actionable updates."
	}
	parts := []string{base}
	if sourceLabel != "" {
		parts = append(parts, "Source: "+sourceLabel)
	}
	if observation.Summary != "" {
		parts = append(parts, "Observed summary: "+observation.Summary)
	}
	if body != "" {
		parts = append(parts, "Latest observed content:\n"+body)
	}
	return strings.Join(parts, "\n\n")
}

func compactSummary(text string) string {
	text = strings.TrimSpace(text)
	if text == "" {
		return ""
	}
	text = strings.Join(strings.Fields(text), " ")
	const maxLen = 240
	if len(text) <= maxLen {
		return text
	}
	return strings.TrimSpace(text[:maxLen]) + "..."
}

func parseIntervalOrDefault(raw string) time.Duration {
	d, err := time.ParseDuration(strings.TrimSpace(raw))
	if err != nil {
		return 0
	}
	return d
}
