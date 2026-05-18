package channels

import (
	"context"
	"strings"

	"github.com/fulcrus/hopclaw/agent"
	"github.com/fulcrus/hopclaw/eventbus"
	runtimesvc "github.com/fulcrus/hopclaw/runtime"
	verifyrt "github.com/fulcrus/hopclaw/runtime/verify"
)

type runResultProvider interface {
	GetRunResult(ctx context.Context, id string) (*runtimesvc.RunResult, error)
}

type runVerificationProvider interface {
	GetRunVerification(ctx context.Context, id string) (*verifyrt.RunVerification, error)
}

func GetRunResultIfSupported(ctx context.Context, runtime any, runID string) (*runtimesvc.RunResult, error) {
	if strings.TrimSpace(runID) == "" || runtime == nil {
		return nil, nil
	}
	provider, ok := runtime.(runResultProvider)
	if !ok {
		return nil, nil
	}
	return provider.GetRunResult(ctx, runID)
}

func GetRunVerificationIfSupported(ctx context.Context, runtime any, runID string) (*verifyrt.RunVerification, error) {
	if strings.TrimSpace(runID) == "" || runtime == nil {
		return nil, nil
	}
	provider, ok := runtime.(runVerificationProvider)
	if !ok {
		return nil, nil
	}
	return provider.GetRunVerification(ctx, runID)
}

func BridgeCompletedResultContent(session *agent.Session, runID string, inputEventID string, result *runtimesvc.RunResult, verification *verifyrt.RunVerification, event eventbus.Event) string {
	inputContent := BridgeInputContent(session, inputEventID)
	notices := bridgeExecutionDegradedNotices(inputContent, result)
	if verification != nil && verification.ShouldBlockDelivery() {
		if result != nil && result.Delivery != nil {
			if body := bridgeCompletedBody(session, runID, result, event); strings.TrimSpace(body) != "" {
				if len(notices) == 0 {
					return body
				}
				return strings.Join(append(notices, body), "\n\n")
			}
		}
		if result != nil && result.Delivery != nil && strings.TrimSpace(result.Delivery.Summary) != "" {
			summary := strings.TrimSpace(result.Delivery.Summary)
			if len(notices) == 0 {
				return summary
			}
			return strings.Join(append(notices, summary), "\n\n")
		}
		failure := BridgeVerificationFailureMessage(inputContent, strings.TrimSpace(verification.Summary))
		if len(notices) == 0 {
			return failure
		}
		return strings.Join(append(notices, failure), "\n\n")
	}
	verificationNotice := ""
	if verification != nil && verification.Status == verifyrt.StatusWarning {
		if result != nil && result.Outcome == runtimesvc.RunOutcomePartial {
			verificationNotice = BridgePartialResultPrefix(inputContent, strings.TrimSpace(verification.Summary))
		} else {
			verificationNotice = BridgeVerificationWarningPrefix(inputContent, strings.TrimSpace(verification.Summary))
		}
	}
	if strings.TrimSpace(verificationNotice) != "" {
		notices = append(notices, verificationNotice)
	}
	body := bridgeCompletedBody(session, runID, result, event)
	if len(notices) == 0 {
		return body
	}
	prefix := strings.Join(notices, "\n")
	if strings.TrimSpace(body) == "" {
		return prefix
	}
	return prefix + "\n\n" + body
}

func bridgeExecutionDegradedNotices(inputContent string, result *runtimesvc.RunResult) []string {
	if result == nil || result.EventLedger == nil || len(result.EventLedger.Events) == 0 {
		return nil
	}
	notices := make([]string, 0, 2)
	if notice := bridgeModelFailoverNotice(inputContent, result.EventLedger); notice != "" {
		notices = append(notices, notice)
	}
	if notice := bridgeThinkingDegradedNotice(inputContent, result.EventLedger); notice != "" {
		notices = append(notices, notice)
	}
	if len(notices) == 0 {
		return nil
	}
	return notices
}

func bridgeModelFailoverNotice(inputContent string, ledger *runtimesvc.EventLedger) string {
	if ledger == nil {
		return ""
	}
	for i := len(ledger.Events) - 1; i >= 0; i-- {
		item := ledger.Events[i]
		if item.Type != eventbus.EventModelFailover {
			continue
		}
		payload, ok := ledgerEventToEvent(item).ModelFailoverPayload()
		if !ok {
			continue
		}
		fromModel := strings.TrimSpace(payload.FromModel)
		toModel := strings.TrimSpace(payload.ToModel)
		reason := strings.ToLower(strings.TrimSpace(payload.Reason))
		if fromModel == "" || toModel == "" {
			continue
		}
		if bridgeLooksChinese(inputContent) {
			switch {
			case strings.Contains(reason, "timeout"):
				return "注意：主模型 " + fromModel + " 请求超时，已自动切换到备用模型 " + toModel + " 继续完成。"
			case strings.Contains(reason, "unavailable"), strings.Contains(reason, "overload"):
				return "注意：主模型 " + fromModel + " 当前不可用，已自动切换到备用模型 " + toModel + " 继续完成。"
			case strings.Contains(reason, "incompatible"):
				return "注意：请求的模型 " + fromModel + " 当前不可用或不兼容，已自动切换到 " + toModel + "。"
			default:
				return "注意：本次请求已从模型 " + fromModel + " 自动切换到 " + toModel + " 继续完成。"
			}
		}
		switch {
		case strings.Contains(reason, "timeout"):
			return "Note: the primary model " + fromModel + " timed out, so I switched to the fallback model " + toModel + " to finish this request."
		case strings.Contains(reason, "unavailable"), strings.Contains(reason, "overload"):
			return "Note: the primary model " + fromModel + " was unavailable, so I switched to the fallback model " + toModel + " to finish this request."
		case strings.Contains(reason, "incompatible"):
			return "Note: the requested model " + fromModel + " was unavailable or incompatible, so I switched to " + toModel + "."
		default:
			return "Note: this request automatically switched from " + fromModel + " to " + toModel + " before finishing."
		}
	}
	return ""
}

func bridgeThinkingDegradedNotice(inputContent string, ledger *runtimesvc.EventLedger) string {
	if ledger == nil {
		return ""
	}
	for i := len(ledger.Events) - 1; i >= 0; i-- {
		item := ledger.Events[i]
		if item.Type != eventbus.EventThinkingDegraded {
			continue
		}
		payload, ok := ledgerEventToEvent(item).ThinkingDegradedPayload()
		if !ok {
			continue
		}
		reason := strings.ToLower(strings.TrimSpace(payload.Reason))
		if bridgeLooksChinese(inputContent) {
			switch {
			case strings.Contains(reason, "timeout"):
				return "注意：本次请求的深度思考模式超时，已自动降级为常规模式继续完成。"
			case strings.Contains(reason, "unavailable"), strings.Contains(reason, "overload"):
				return "注意：本次请求的深度思考模式当前不可用，已自动降级为常规模式继续完成。"
			default:
				return "注意：本次请求的深度思考模式已自动降级为常规模式继续完成。"
			}
		}
		switch {
		case strings.Contains(reason, "timeout"):
			return "Note: the extended thinking mode timed out, so I degraded to regular mode to finish this request."
		case strings.Contains(reason, "unavailable"), strings.Contains(reason, "overload"):
			return "Note: the extended thinking mode was unavailable, so I degraded to regular mode to finish this request."
		default:
			return "Note: the extended thinking mode was degraded to regular mode so the request could finish."
		}
	}
	return ""
}

func ledgerEventToEvent(item runtimesvc.LedgerEvent) eventbus.Event {
	return eventbus.Event{
		ID:        item.ID,
		Type:      item.Type,
		RunID:     item.RunID,
		SessionID: item.SessionID,
		Time:      item.Time,
		Attrs:     item.Attrs,
	}
}

func bridgeCompletedBody(session *agent.Session, runID string, result *runtimesvc.RunResult, event eventbus.Event) string {
	if result != nil {
		if content := formatTerminalResultContent(result); content != "" {
			return content
		}
	}
	if content := strings.TrimSpace(BridgeCompletedContent(session, runID)); content != "" {
		return content
	}
	if result != nil && strings.TrimSpace(result.Summary) != "" {
		return strings.TrimSpace(result.Summary)
	}
	if payload, ok := event.RunStatusPayload(); ok && strings.TrimSpace(payload.Summary) != "" {
		return strings.TrimSpace(payload.Summary)
	}
	return ""
}

func formatTerminalResultContent(result *runtimesvc.RunResult) string {
	if result == nil {
		return ""
	}
	if content := formatTerminalDelivery(result.Delivery); content != "" {
		return content
	}
	content := strings.TrimSpace(result.Output)
	summary := strings.TrimSpace(result.Summary)
	uris := resultArtifactURIs(result)

	if content != "" {
		if len(uris) == 0 || containsAllResultArtifacts(content, uris) {
			return content
		}
		return strings.TrimSpace(content) + "\n\nArtifacts:\n- " + strings.Join(uris, "\n- ")
	}
	if summary != "" {
		if len(uris) == 0 || containsAllResultArtifacts(summary, uris) {
			return summary
		}
		return strings.TrimSpace(summary) + "\n\nArtifacts:\n- " + strings.Join(uris, "\n- ")
	}
	if len(uris) > 0 {
		return "Artifacts:\n- " + strings.Join(uris, "\n- ")
	}
	return ""
}

func formatTerminalDelivery(delivery *runtimesvc.TerminalDelivery) string {
	if delivery == nil {
		return ""
	}
	parts := make([]string, 0, len(delivery.Blocks)+2)
	for _, block := range delivery.Blocks {
		content := strings.TrimSpace(block.Content)
		if content == "" {
			continue
		}
		title := strings.TrimSpace(block.Title)
		if title != "" && !strings.EqualFold(title, "Result") {
			parts = append(parts, title+":\n"+content)
		} else {
			parts = append(parts, content)
		}
	}
	if len(delivery.Attachments) > 0 {
		lines := make([]string, 0, len(delivery.Attachments))
		for _, item := range delivery.Attachments {
			uri := strings.TrimSpace(item.URI)
			if uri == "" {
				continue
			}
			label := strings.TrimSpace(item.Label)
			if label != "" && label != uri {
				lines = append(lines, label+" — "+uri)
			} else {
				lines = append(lines, uri)
			}
		}
		if len(lines) > 0 {
			parts = append(parts, "Attachments:\n- "+strings.Join(lines, "\n- "))
		}
	}
	if len(parts) == 0 {
		return strings.TrimSpace(delivery.Summary)
	}
	return strings.TrimSpace(strings.Join(parts, "\n\n"))
}

func resultArtifactURIs(result *runtimesvc.RunResult) []string {
	if result == nil || len(result.Deliverables) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(result.Deliverables))
	out := make([]string, 0, len(result.Deliverables))
	for _, item := range result.Deliverables {
		uri := strings.TrimSpace(item.URI)
		if uri == "" {
			continue
		}
		if _, ok := seen[uri]; ok {
			continue
		}
		seen[uri] = struct{}{}
		out = append(out, uri)
	}
	return out
}

func containsAllResultArtifacts(content string, uris []string) bool {
	for _, uri := range uris {
		if !strings.Contains(content, uri) {
			return false
		}
	}
	return true
}
