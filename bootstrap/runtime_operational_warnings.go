package bootstrap

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/fulcrus/hopclaw/controlplane"
	"github.com/fulcrus/hopclaw/eventbus"
)

const (
	defaultDeliveryFailureWarningTTL = 10 * time.Minute
	defaultRuntimeEventWarningTTL    = 15 * time.Minute
)

type combinedOperationalWarningSource struct {
	sources []controlplane.OperationalWarningSource
}

func newCombinedOperationalWarningSource(sources ...controlplane.OperationalWarningSource) controlplane.OperationalWarningSource {
	filtered := make([]controlplane.OperationalWarningSource, 0, len(sources))
	for _, source := range sources {
		if source != nil {
			filtered = append(filtered, source)
		}
	}
	if len(filtered) == 0 {
		return nil
	}
	return &combinedOperationalWarningSource{sources: filtered}
}

func (s *combinedOperationalWarningSource) OperationalWarnings() []controlplane.OperationalWarning {
	if s == nil || len(s.sources) == 0 {
		return nil
	}
	merged := make(map[string]controlplane.OperationalWarning)
	for _, source := range s.sources {
		if source == nil {
			continue
		}
		for _, warning := range source.OperationalWarnings() {
			component := strings.TrimSpace(warning.Component)
			if component == "" {
				component = strings.TrimSpace(warning.Summary)
			}
			if component == "" {
				continue
			}
			warning.Component = component
			merged[component] = warning
		}
	}
	if len(merged) == 0 {
		return nil
	}
	out := make([]controlplane.OperationalWarning, 0, len(merged))
	for _, warning := range merged {
		out = append(out, warning)
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].Component < out[j].Component
	})
	return out
}

type deliveryFailureWarningCollector struct {
	mu      sync.Mutex
	ttl     time.Duration
	now     func() time.Time
	records map[string]deliveryFailureWarningRecord
}

type deliveryFailureWarningRecord struct {
	component      string
	channel        string
	count          int
	lastAt         time.Time
	lastRunID      string
	lastTargetID   string
	lastSessionKey string
	lastStatusKind string
	lastPreview    string
	lastError      string
	lastAttempts   int
}

func newDeliveryFailureWarningCollector(ttl time.Duration) *deliveryFailureWarningCollector {
	if ttl <= 0 {
		ttl = defaultDeliveryFailureWarningTTL
	}
	return &deliveryFailureWarningCollector{
		ttl:     ttl,
		now:     func() time.Time { return time.Now().UTC() },
		records: make(map[string]deliveryFailureWarningRecord),
	}
}

func (c *deliveryFailureWarningCollector) Handle(_ context.Context, event eventbus.Event) error {
	if c == nil || event.Type != eventbus.EventDeliveryFailed {
		return nil
	}
	payload, ok := event.DeliveryFailedPayload()
	if !ok {
		return nil
	}
	channel := deliveryFailureWarningChannel(payload.Channel)
	record := deliveryFailureWarningRecord{
		component:      deliveryFailureWarningComponent(channel),
		channel:        channel,
		lastRunID:      strings.TrimSpace(event.RunID),
		lastTargetID:   strings.TrimSpace(payload.TargetID),
		lastSessionKey: strings.TrimSpace(payload.SessionKey),
		lastStatusKind: strings.TrimSpace(payload.StatusKind),
		lastPreview:    strings.TrimSpace(payload.ContentPreview),
		lastError:      strings.TrimSpace(payload.Error),
		lastAttempts:   payload.Attempts,
	}

	now := c.currentTime()
	c.mu.Lock()
	defer c.mu.Unlock()
	c.pruneLocked(now)
	previous := c.records[channel]
	record.count = previous.count + 1
	record.lastAt = now
	c.records[channel] = record
	return nil
}

func (c *deliveryFailureWarningCollector) OperationalWarnings() []controlplane.OperationalWarning {
	if c == nil {
		return nil
	}
	now := c.currentTime()
	c.mu.Lock()
	c.pruneLocked(now)
	if len(c.records) == 0 {
		c.mu.Unlock()
		return nil
	}
	out := make([]controlplane.OperationalWarning, 0, len(c.records))
	for _, record := range c.records {
		out = append(out, deliveryFailureWarning(record))
	}
	c.mu.Unlock()
	sort.Slice(out, func(i, j int) bool {
		return out[i].Component < out[j].Component
	})
	return out
}

func (c *deliveryFailureWarningCollector) currentTime() time.Time {
	if c == nil || c.now == nil {
		return time.Now().UTC()
	}
	return c.now().UTC()
}

func (c *deliveryFailureWarningCollector) pruneLocked(now time.Time) {
	if c == nil {
		return
	}
	for key, record := range c.records {
		if record.lastAt.IsZero() || now.Sub(record.lastAt) > c.ttl {
			delete(c.records, key)
		}
	}
}

func deliveryFailureWarning(record deliveryFailureWarningRecord) controlplane.OperationalWarning {
	channelLabel := record.channel
	summary := fmt.Sprintf("Channel %q recently failed to deliver replies", channelLabel)
	if channelLabel == "channel" {
		summary = "A channel recently failed to deliver replies"
	}
	detailParts := []string{fmt.Sprintf("%d recent delivery failure(s)", record.count)}
	if record.lastTargetID != "" {
		detailParts = append(detailParts, "target="+record.lastTargetID)
	}
	if record.lastAttempts > 0 {
		detailParts = append(detailParts, fmt.Sprintf("attempts=%d", record.lastAttempts))
	}
	if record.lastStatusKind != "" {
		detailParts = append(detailParts, "status="+record.lastStatusKind)
	}
	if record.lastRunID != "" {
		detailParts = append(detailParts, "run="+record.lastRunID)
	}
	if record.lastError != "" {
		detailParts = append(detailParts, "error="+record.lastError)
	} else if record.lastPreview != "" {
		detailParts = append(detailParts, "preview="+record.lastPreview)
	}
	return controlplane.OperationalWarning{
		Component: record.component,
		Summary:   summary,
		Detail:    strings.Join(detailParts, "; "),
		Fix:       "inspect channel connectivity, credentials, and permissions, then review recent delivery.failed events",
	}
}

func deliveryFailureWarningChannel(channel string) string {
	channel = strings.TrimSpace(channel)
	if channel == "" {
		return "channel"
	}
	return channel
}

func deliveryFailureWarningComponent(channel string) string {
	channel = deliveryFailureWarningChannel(channel)
	return "channel/" + channel + "/delivery"
}

type runtimeEventWarningCollector struct {
	mu  sync.Mutex
	ttl time.Duration
	now func() time.Time

	approvalTimeout approvalTimeoutRuntimeWarningRecord
	governance      governanceRuntimeWarningRecord
	runFailure      runFailureRuntimeWarningRecord
	modelRetries    map[string]modelRetryRuntimeWarningRecord
}

type approvalTimeoutRuntimeWarningRecord struct {
	count          int
	lastAt         time.Time
	lastApprovalID string
	lastRunID      string
	lastStatus     string
	lastPolicy     string
}

type governanceRuntimeWarningRecord struct {
	count           int
	lastAt          time.Time
	lastDeliveryID  string
	lastAdapter     string
	lastStatus      string
	lastRunID       string
	lastError       string
	lastAttempts    int
	lastMaxAttempts int
	lastNextAttempt time.Time
	lastGovernance  string
	lastSourceEvent string
}

type runFailureRuntimeWarningRecord struct {
	count       int
	lastAt      time.Time
	lastRunID   string
	lastSummary string
	lastError   string
	lastKind    string
}

type modelRetryRuntimeWarningRecord struct {
	model       string
	count       int
	lastAt      time.Time
	lastRunID   string
	lastAttempt int
	lastMax     int
	lastReason  string
	lastError   string
}

func newRuntimeEventWarningCollector(ttl time.Duration) *runtimeEventWarningCollector {
	if ttl <= 0 {
		ttl = defaultRuntimeEventWarningTTL
	}
	return &runtimeEventWarningCollector{
		ttl:          ttl,
		now:          func() time.Time { return time.Now().UTC() },
		modelRetries: make(map[string]modelRetryRuntimeWarningRecord),
	}
}

func (c *runtimeEventWarningCollector) Handle(_ context.Context, event eventbus.Event) error {
	if c == nil {
		return nil
	}
	now := c.currentTime()
	c.mu.Lock()
	defer c.mu.Unlock()
	c.pruneLocked(now)

	switch event.Type {
	case eventbus.EventApprovalTimedOut:
		payload, ok := event.ApprovalTimedOutPayload()
		if !ok {
			return nil
		}
		c.approvalTimeout.count++
		c.approvalTimeout.lastAt = now
		c.approvalTimeout.lastApprovalID = strings.TrimSpace(payload.ApprovalID)
		c.approvalTimeout.lastRunID = strings.TrimSpace(event.RunID)
		c.approvalTimeout.lastStatus = strings.TrimSpace(payload.Status)
		c.approvalTimeout.lastPolicy = strings.TrimSpace(payload.PolicySummary)
	case eventbus.EventGovernanceDeliveryRetryScheduled, eventbus.EventGovernanceDeliveryDeadLettered:
		payload, ok := event.GovernanceDeliveryPayload()
		if !ok {
			return nil
		}
		c.governance.count++
		c.governance.lastAt = now
		c.governance.lastDeliveryID = strings.TrimSpace(payload.DeliveryID)
		c.governance.lastAdapter = strings.TrimSpace(payload.AdapterName)
		c.governance.lastStatus = strings.TrimSpace(payload.DeliveryStatus)
		c.governance.lastRunID = strings.TrimSpace(event.RunID)
		c.governance.lastError = strings.TrimSpace(payload.Error)
		c.governance.lastAttempts = payload.DeliveryAttempts
		c.governance.lastMaxAttempts = payload.DeliveryMaxAttempts
		c.governance.lastNextAttempt = payload.NextAttemptAt
		c.governance.lastGovernance = strings.TrimSpace(payload.GovernanceKind)
		c.governance.lastSourceEvent = strings.TrimSpace(payload.SourceEventType)
	case eventbus.EventRunFailed, eventbus.EventWorkflowFailed:
		switch event.Type {
		case eventbus.EventRunFailed:
			payload, _ := event.RunFailedPayload()
			c.runFailure.count++
			c.runFailure.lastAt = now
			c.runFailure.lastRunID = strings.TrimSpace(event.RunID)
			c.runFailure.lastSummary = strings.TrimSpace(payload.Summary)
			c.runFailure.lastError = strings.TrimSpace(payload.Error)
			c.runFailure.lastKind = "run"
		case eventbus.EventWorkflowFailed:
			payload, _ := event.WorkflowFailedPayload()
			c.runFailure.count++
			c.runFailure.lastAt = now
			c.runFailure.lastRunID = strings.TrimSpace(event.RunID)
			c.runFailure.lastSummary = strings.TrimSpace(payload.Summary)
			c.runFailure.lastKind = "workflow"
		}
	case eventbus.EventModelRetry:
		payload, ok := event.ModelRetryPayload()
		if !ok || payload.Attempt < 2 {
			return nil
		}
		model := strings.TrimSpace(payload.Model)
		if model == "" {
			model = "default"
		}
		record := c.modelRetries[model]
		record.model = model
		record.count++
		record.lastAt = now
		record.lastRunID = strings.TrimSpace(event.RunID)
		record.lastAttempt = payload.Attempt
		record.lastMax = payload.MaxAttempts
		record.lastReason = strings.TrimSpace(payload.FailureReason)
		record.lastError = strings.TrimSpace(payload.Error)
		c.modelRetries[model] = record
	}
	return nil
}

func (c *runtimeEventWarningCollector) OperationalWarnings() []controlplane.OperationalWarning {
	if c == nil {
		return nil
	}
	now := c.currentTime()
	c.mu.Lock()
	c.pruneLocked(now)
	var out []controlplane.OperationalWarning
	if c.approvalTimeout.count > 0 {
		out = append(out, approvalTimeoutRuntimeWarning(c.approvalTimeout))
	}
	if c.governance.count > 0 {
		out = append(out, governanceRuntimeWarning(c.governance))
	}
	if c.runFailure.count > 0 {
		out = append(out, runFailureRuntimeWarning(c.runFailure))
	}
	for _, record := range c.modelRetries {
		if record.count <= 0 {
			continue
		}
		out = append(out, modelRetryRuntimeWarning(record))
	}
	c.mu.Unlock()
	if len(out) == 0 {
		return nil
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].Component < out[j].Component
	})
	return out
}

func (c *runtimeEventWarningCollector) currentTime() time.Time {
	if c == nil || c.now == nil {
		return time.Now().UTC()
	}
	return c.now().UTC()
}

func (c *runtimeEventWarningCollector) pruneLocked(now time.Time) {
	if c == nil {
		return
	}
	if c.approvalTimeout.count > 0 && (!c.approvalTimeout.lastAt.IsZero() && now.Sub(c.approvalTimeout.lastAt) > c.ttl) {
		c.approvalTimeout = approvalTimeoutRuntimeWarningRecord{}
	}
	if c.governance.count > 0 && (!c.governance.lastAt.IsZero() && now.Sub(c.governance.lastAt) > c.ttl) {
		c.governance = governanceRuntimeWarningRecord{}
	}
	if c.runFailure.count > 0 && (!c.runFailure.lastAt.IsZero() && now.Sub(c.runFailure.lastAt) > c.ttl) {
		c.runFailure = runFailureRuntimeWarningRecord{}
	}
	for key, record := range c.modelRetries {
		if record.lastAt.IsZero() || now.Sub(record.lastAt) > c.ttl {
			delete(c.modelRetries, key)
		}
	}
}

func approvalTimeoutRuntimeWarning(record approvalTimeoutRuntimeWarningRecord) controlplane.OperationalWarning {
	detailParts := []string{fmt.Sprintf("%d recent timeout event(s)", record.count)}
	if record.lastApprovalID != "" {
		detailParts = append(detailParts, "approval="+record.lastApprovalID)
	}
	if record.lastRunID != "" {
		detailParts = append(detailParts, "run="+record.lastRunID)
	}
	if record.lastStatus != "" {
		detailParts = append(detailParts, "status="+record.lastStatus)
	}
	if record.lastPolicy != "" {
		detailParts = append(detailParts, "policy="+record.lastPolicy)
	}
	return controlplane.OperationalWarning{
		Component: "approval_timeout/recent",
		Summary:   "Approvals recently timed out while runs were waiting",
		Detail:    strings.Join(detailParts, "; "),
		Fix:       "review pending approvals, timeout policy, and any runs that may need manual follow-up",
	}
}

func governanceRuntimeWarning(record governanceRuntimeWarningRecord) controlplane.OperationalWarning {
	summary := "Governance delivery recently retried failed events"
	if strings.Contains(strings.ToLower(record.lastStatus), "dead") {
		summary = "Governance delivery recently dead-lettered events"
	}
	detailParts := []string{fmt.Sprintf("%d recent governance delivery issue(s)", record.count)}
	if record.lastAdapter != "" {
		detailParts = append(detailParts, "adapter="+record.lastAdapter)
	}
	if record.lastDeliveryID != "" {
		detailParts = append(detailParts, "delivery="+record.lastDeliveryID)
	}
	if record.lastStatus != "" {
		detailParts = append(detailParts, "status="+record.lastStatus)
	}
	if record.lastAttempts > 0 {
		attempts := fmt.Sprintf("attempts=%d", record.lastAttempts)
		if record.lastMaxAttempts > 0 {
			attempts = fmt.Sprintf("attempts=%d/%d", record.lastAttempts, record.lastMaxAttempts)
		}
		detailParts = append(detailParts, attempts)
	}
	if !record.lastNextAttempt.IsZero() {
		detailParts = append(detailParts, "next_attempt="+record.lastNextAttempt.UTC().Format(time.RFC3339))
	}
	if record.lastRunID != "" {
		detailParts = append(detailParts, "run="+record.lastRunID)
	}
	if record.lastGovernance != "" {
		detailParts = append(detailParts, "kind="+record.lastGovernance)
	}
	if record.lastSourceEvent != "" {
		detailParts = append(detailParts, "source="+record.lastSourceEvent)
	}
	if record.lastError != "" {
		detailParts = append(detailParts, "error="+record.lastError)
	}
	return controlplane.OperationalWarning{
		Component: "governance/delivery",
		Summary:   summary,
		Detail:    strings.Join(detailParts, "; "),
		Fix:       "inspect governance adapter health and redrive stuck deliveries after fixing the destination",
	}
}

func runFailureRuntimeWarning(record runFailureRuntimeWarningRecord) controlplane.OperationalWarning {
	summary := "Runs recently failed during execution"
	if record.lastKind == "workflow" {
		summary = "Workflows recently failed during execution"
	}
	detailParts := []string{fmt.Sprintf("%d recent %s failure(s)", record.count, normalizeWarningKind(record.lastKind))}
	if record.lastRunID != "" {
		detailParts = append(detailParts, "run="+record.lastRunID)
	}
	if record.lastSummary != "" {
		detailParts = append(detailParts, "summary="+record.lastSummary)
	}
	if record.lastError != "" {
		detailParts = append(detailParts, "error="+record.lastError)
	}
	return controlplane.OperationalWarning{
		Component: "runtime/run_failures",
		Summary:   summary,
		Detail:    strings.Join(detailParts, "; "),
		Fix:       "inspect recent run.failed or workflow.failed events and recover any affected session before retrying",
	}
}

func modelRetryRuntimeWarning(record modelRetryRuntimeWarningRecord) controlplane.OperationalWarning {
	detailParts := []string{fmt.Sprintf("%d recent retry event(s)", record.count)}
	if record.lastRunID != "" {
		detailParts = append(detailParts, "run="+record.lastRunID)
	}
	if record.lastAttempt > 0 {
		attempts := fmt.Sprintf("attempt=%d", record.lastAttempt)
		if record.lastMax > 0 {
			attempts = fmt.Sprintf("attempt=%d/%d", record.lastAttempt, record.lastMax)
		}
		detailParts = append(detailParts, attempts)
	}
	if record.lastReason != "" {
		detailParts = append(detailParts, "reason="+record.lastReason)
	}
	if record.lastError != "" {
		detailParts = append(detailParts, "error="+record.lastError)
	}
	return controlplane.OperationalWarning{
		Component: "model/" + record.model + "/retry",
		Summary:   fmt.Sprintf("Model %q recently hit repeated call failures", record.model),
		Detail:    strings.Join(detailParts, "; "),
		Fix:       "inspect provider health, quotas, and router failover coverage for the affected model",
	}
}

func normalizeWarningKind(kind string) string {
	kind = strings.TrimSpace(kind)
	if kind == "" {
		return "run"
	}
	return kind
}
