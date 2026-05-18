package cli

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/fulcrus/hopclaw/acp"
	"github.com/fulcrus/hopclaw/agent"
	"github.com/fulcrus/hopclaw/approval"
	"github.com/fulcrus/hopclaw/bootstrap"
	"github.com/fulcrus/hopclaw/channels"
	"github.com/fulcrus/hopclaw/contextengine"
	"github.com/fulcrus/hopclaw/controlplane"
	"github.com/fulcrus/hopclaw/eventbus"
	"github.com/fulcrus/hopclaw/internal/meta"
	runtimesvc "github.com/fulcrus/hopclaw/runtime"
)

type interactiveGatewayResponse struct {
	Decision      runtimesvc.InteractionDecision `json:"decision"`
	Run           *agent.Run                     `json:"run,omitempty"`
	SubmitRequest *runtimesvc.SubmitRequest      `json:"submit_request,omitempty"`
	Message       string                         `json:"message,omitempty"`
	Error         string                         `json:"error,omitempty"`
}

func interactionSubmittedRun(resp *interactiveGatewayResponse) bool {
	return resp != nil && resp.Run != nil && resp.SubmitRequest != nil && strings.TrimSpace(resp.Run.ID) != ""
}

type embeddedInteractionResult struct {
	Result *runtimesvc.InteractionResult
	Error  error
}

type externalInteractionResult struct {
	Result interactiveGatewayResponse
	Error  error
}

func interactiveImmediateEvents(message, errText string) <-chan acp.RunEvent {
	events := make(chan acp.RunEvent, 2)
	go func() {
		defer close(events)
		if text := normalizeInteractiveReplyText(message); text != "" {
			events <- acp.RunEvent{Type: "text_delta", Text: text, Runless: true}
			events <- acp.RunEvent{Type: "complete", StopReason: acp.StopEndTurn, Runless: true}
			return
		}
		if errText = strings.TrimSpace(errText); errText != "" {
			events <- acp.RunEvent{Type: "error", Error: errText, Runless: true}
			return
		}
		events <- acp.RunEvent{Type: "complete", StopReason: acp.StopEndTurn, Runless: true}
	}()
	return events
}

func normalizeInteractiveReplyText(text string) string {
	if strings.TrimSpace(text) == "" {
		return ""
	}
	if strings.HasSuffix(text, "\n") {
		return text
	}
	return text + "\n"
}

func newInteractiveTurnID() string {
	return fmt.Sprintf("turn-%d", time.Now().UTC().UnixNano())
}

func cliRequestMetadata() map[string]any {
	metadata := map[string]any{
		meta.KeyChannel:  "cli",
		meta.KeyChatType: meta.ChatTypeDirect.String(),
	}
	channels.ApplyChannelCapabilityMetadata(metadata, channels.ChannelCapabilityDescriptor{
		Interactive: true,
	})
	return metadata
}

func interactiveRequestMetadata(turnID string) map[string]any {
	metadata := cliRequestMetadata()
	if strings.TrimSpace(turnID) != "" {
		metadata[meta.KeyInteractionTurnID] = strings.TrimSpace(turnID)
	}
	return metadata
}

func interactiveEventTurnID(event eventbus.Event) string {
	if event.Attrs == nil {
		return ""
	}
	value := strings.TrimSpace(fmt.Sprint(event.Attrs[meta.KeyInteractionTurnID]))
	if value == "" || value == "<nil>" {
		return ""
	}
	return value
}

func matchesInteractiveTurnEvent(event eventbus.Event, turnID string) bool {
	return strings.TrimSpace(turnID) != "" && interactiveEventTurnID(event) == strings.TrimSpace(turnID)
}

func matchesInteractiveRunlessSessionEvent(event eventbus.Event, sessionID, turnID string) bool {
	if strings.TrimSpace(event.RunID) != "" {
		return false
	}
	if strings.TrimSpace(sessionID) != "" && strings.TrimSpace(event.SessionID) != strings.TrimSpace(sessionID) {
		return false
	}
	eventTurnID := interactiveEventTurnID(event)
	if strings.TrimSpace(turnID) == "" {
		return eventTurnID == ""
	}
	return eventTurnID == strings.TrimSpace(turnID) || eventTurnID == ""
}

func relayInteractiveEvents(out chan<- acp.RunEvent, events <-chan acp.RunEvent) {
	for event := range events {
		out <- event
	}
}

func runtimeStructuredCommand(cmd *acp.StructuredCommand) *runtimesvc.StructuredCommand {
	if cmd == nil {
		return nil
	}
	return &runtimesvc.StructuredCommand{
		Kind:  strings.TrimSpace(cmd.Kind),
		RunID: strings.TrimSpace(cmd.RunID),
	}
}

func runtimeStructuredApproval(approval *acp.StructuredApproval) *runtimesvc.StructuredApproval {
	if approval == nil {
		return nil
	}
	return &runtimesvc.StructuredApproval{
		Action: strings.TrimSpace(approval.Action),
	}
}

func (g *externalInteractiveGateway) SubmitRun(ctx context.Context, sessionKey, message string, images []string) (string, <-chan acp.RunEvent, error) {
	return g.SubmitRunWithOptions(ctx, sessionKey, message, images, acp.PromptOptions{})
}

func (g *externalInteractiveGateway) SubmitRunWithOptions(ctx context.Context, sessionKey, message string, images []string, options acp.PromptOptions) (string, <-chan acp.RunEvent, error) {
	cursor, _ := lastRuntimeEventID(ctx, g.client)
	turnID := newInteractiveTurnID()
	body := map[string]any{
		"session_key":    sessionKey,
		"content":        message,
		"images":         images,
		"content_blocks": append([]contextengine.ContentBlock(nil), options.ContentBlocks...),
		"metadata":       interactiveRequestMetadata(turnID),
	}
	if strings.TrimSpace(options.Model) != "" {
		body["model"] = strings.TrimSpace(options.Model)
	}
	if options.StructuredCommand != nil {
		body["structured_command"] = map[string]any{
			"kind":   strings.TrimSpace(options.StructuredCommand.Kind),
			"run_id": strings.TrimSpace(options.StructuredCommand.RunID),
		}
	}
	if options.StructuredApproval != nil {
		body["structured_approval"] = map[string]any{
			"action": strings.TrimSpace(options.StructuredApproval.Action),
		}
	}
	events := make(chan acp.RunEvent, 128)
	ready := make(chan string, 1)
	go g.coordinateExternalInteraction(ctx, cursor, turnID, body, events, ready)

	select {
	case runID := <-ready:
		return runID, events, nil
	case <-ctx.Done():
		return "", nil, ctx.Err()
	}
}

func (g *externalInteractiveGateway) streamEventBus(ctx context.Context, sinceID string, out chan<- eventbus.Event, errCh chan<- error) {
	path := "/runtime/events/stream"
	if strings.TrimSpace(sinceID) != "" {
		path += "?since=" + sinceID
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, g.client.url(path), nil)
	if err != nil {
		errCh <- err
		return
	}
	req.Header.Set("Accept", "text/event-stream")
	if g.client.AuthToken != "" {
		req.Header.Set(authHeaderName, g.client.AuthToken)
	}

	httpClient := &http.Client{Timeout: 0}
	if g != nil && g.client != nil && g.client.HTTP != nil {
		cloned := *g.client.HTTP
		cloned.Timeout = 0
		httpClient = &cloned
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		errCh <- err
		return
	}
	defer resp.Body.Close()

	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 0, 64*1024), interactiveSSEBuffer)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		var event eventbus.Event
		if err := json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &event); err != nil {
			continue
		}
		out <- event
	}
	if err := scanner.Err(); err != nil {
		errCh <- err
	}
}

func (g *externalInteractiveGateway) coordinateExternalInteraction(
	ctx context.Context,
	cursor, turnID string,
	body map[string]any,
	out chan<- acp.RunEvent,
	ready chan<- string,
) {
	defer close(out)

	eventsCh := make(chan eventbus.Event, 128)
	streamErrCh := make(chan error, 1)
	go g.streamEventBus(ctx, cursor, eventsCh, streamErrCh)

	resultCh := make(chan externalInteractionResult, 1)
	go func() {
		var result interactiveGatewayResponse
		err := g.client.Post(ctx, "/runtime/interact", body, &result)
		resultCh <- externalInteractionResult{Result: result, Error: err}
	}()

	var (
		runID            string
		readySent        bool
		streamedRunless  bool
		resultDone       bool
		terminal         bool
		pendingStreamErr error
		bufferedEvents   []eventbus.Event
	)
	markReady := func(value string) {
		if readySent {
			return
		}
		readySent = true
		select {
		case ready <- value:
		default:
		}
	}
	flushBufferedRunEvents := func() bool {
		if runID == "" || len(bufferedEvents) == 0 {
			bufferedEvents = nil
			return false
		}
		terminal := false
		for _, event := range bufferedEvents {
			if strings.TrimSpace(event.RunID) != runID {
				continue
			}
			if mapped, isTerminal, ok := g.mapEvent(ctx, event); ok {
				out <- mapped
				if isTerminal {
					terminal = true
				}
			}
		}
		bufferedEvents = nil
		return terminal
	}
	processEvent := func(event eventbus.Event) bool {
		if runID != "" {
			if strings.TrimSpace(event.RunID) != runID {
				return false
			}
			if mapped, isTerminal, ok := g.mapEvent(ctx, event); ok {
				out <- mapped
				if isTerminal {
					terminal = true
					return true
				}
			}
			return false
		}
		if matchesInteractiveTurnEvent(event, turnID) {
			streamedRunless = true
			markReady("")
			if mapped, isTerminal, ok := g.mapEvent(ctx, event); ok {
				out <- mapped
				if isTerminal {
					terminal = true
					return resultDone
				}
			}
			return false
		}
		if !resultDone && strings.TrimSpace(event.RunID) != "" {
			bufferedEvents = append(bufferedEvents, event)
		}
		return false
	}
	drainPendingEvents := func() bool {
		for {
			select {
			case event, ok := <-eventsCh:
				if !ok {
					return false
				}
				if processEvent(event) {
					return true
				}
			case err := <-streamErrCh:
				if err == nil {
					continue
				}
				pendingStreamErr = err
				if runID != "" {
					markReady(runID)
					out <- acp.RunEvent{Type: "error", Error: err.Error()}
					return true
				}
			default:
				return false
			}
		}
	}

	for {
		select {
		case <-ctx.Done():
			if !readySent {
				markReady("")
			}
			out <- acp.RunEvent{Type: "error", Error: ctx.Err().Error(), Runless: runID == ""}
			return
		case err := <-streamErrCh:
			if err == nil {
				continue
			}
			pendingStreamErr = err
			if runID != "" {
				markReady(runID)
				out <- acp.RunEvent{Type: "error", Error: err.Error()}
				return
			}
		case outcome := <-resultCh:
			resultDone = true
			if outcome.Error != nil {
				markReady("")
				if !streamedRunless || !terminal {
					out <- acp.RunEvent{Type: "error", Error: outcome.Error.Error(), Runless: true}
				}
				return
			}
			if interactionSubmittedRun(&outcome.Result) {
				runID = strings.TrimSpace(outcome.Result.Run.ID)
				markReady(runID)
				if pendingStreamErr != nil {
					out <- acp.RunEvent{Type: "error", Error: pendingStreamErr.Error()}
					return
				}
				if flushBufferedRunEvents() {
					return
				}
				continue
			}
			if drainPendingEvents() {
				return
			}
			if streamedRunless {
				markReady("")
				if terminal {
					return
				}
				if errText := strings.TrimSpace(outcome.Result.Error); errText != "" {
					out <- acp.RunEvent{Type: "error", Error: errText, Runless: true}
					return
				}
				continue
			}
			markReady("")
			relayInteractiveEvents(out, interactiveImmediateEvents(outcome.Result.Message, outcome.Result.Error))
			return
		case event, ok := <-eventsCh:
			if !ok {
				if resultDone && terminal {
					return
				}
				continue
			}
			if processEvent(event) {
				return
			}
		}
	}
}

func (g *externalInteractiveGateway) CancelRun(ctx context.Context, _ string, runID string) error {
	return g.client.Post(ctx, "/runtime/runs/"+runID+"/cancel", map[string]any{}, nil)
}

func (g *externalInteractiveGateway) ResolveApproval(ctx context.Context, requestID string, resolution approval.Resolution) error {
	return g.client.Post(ctx, "/runtime/approvals/"+strings.TrimSpace(requestID)+"/resolve", resolution, nil)
}

func (g *externalInteractiveGateway) ListSessions(ctx context.Context, _, _ int) ([]acp.SessionInfo, error) {
	items, err := fetchSessionSummaries(ctx, g.client)
	if err != nil {
		return nil, err
	}
	out := make([]acp.SessionInfo, 0, len(items))
	for _, item := range items {
		out = append(out, acp.SessionInfo{
			SessionID:  item.ID,
			SessionKey: item.Key,
			Status:     acp.SessionIdle,
		})
	}
	return out, nil
}

func (g *externalInteractiveGateway) ResolveSession(ctx context.Context, key string) (*acp.SessionInfo, error) {
	id, err := externalSessionIDByKey(ctx, g.client, key)
	if err != nil {
		return nil, err
	}
	var session agent.SessionSummary
	if err := g.client.Get(ctx, "/runtime/sessions/"+id, &session); err != nil {
		return nil, err
	}
	return &acp.SessionInfo{
		SessionID:  session.ID,
		SessionKey: session.Key,
		Status:     acp.SessionIdle,
		CreatedAt:  session.CreatedAt,
	}, nil
}

func (g *externalInteractiveGateway) ResetSession(ctx context.Context, key string) error {
	return resetSessionByKey(ctx, g.client, key)
}

func (g *externalInteractiveGateway) mapEvent(ctx context.Context, event eventbus.Event) (acp.RunEvent, bool, bool) {
	runless := strings.TrimSpace(event.RunID) == ""
	switch event.Type {
	case eventbus.EventModelTextDelta:
		payload, _ := event.ModelTextDeltaPayload()
		return acp.RunEvent{
			Type:    "text_delta",
			Text:    payload.Delta,
			Runless: runless,
		}, false, true
	case eventbus.EventModelStreamComplete:
		payload, _ := event.ModelStreamCompletePayload()
		if runless {
			return acp.RunEvent{
				Type:       "complete",
				StopReason: acp.StopEndTurn,
				Usage:      usageInfoFromModelStreamComplete(payload),
				Runless:    true,
			}, true, true
		}
		usage := usageInfoFromModelStreamComplete(payload)
		if usage == nil {
			return acp.RunEvent{}, false, false
		}
		return acp.RunEvent{
			Type:  "usage",
			Usage: usage,
		}, false, true
	case eventbus.EventModelFailover:
		payload, _ := event.ModelFailoverPayload()
		failover := modelFailoverInfoFromAttrs(payload)
		if failover == nil {
			return acp.RunEvent{}, false, false
		}
		return acp.RunEvent{
			Type:          "model_failover",
			ModelFailover: failover,
		}, false, true
	case eventbus.EventToolExecuted:
		return acp.RunEvent{
			Type:       "tool_end",
			ToolName:   firstToolName(event),
			ToolOutput: firstToolSummary(event),
		}, false, true
	case eventbus.EventApprovalRequested:
		approvalPayload, _ := event.ApprovalRequestedPayload()
		req, err := externalPermissionRequest(ctx, g.client, approvalPayload.ApprovalID)
		if err != nil {
			return acp.RunEvent{Type: "error", Error: err.Error()}, true, true
		}
		return acp.RunEvent{
			Type:       "permission_request",
			Permission: req,
		}, false, true
	case eventbus.EventRunCompleted:
		return acp.RunEvent{Type: "complete", StopReason: acp.StopEndTurn}, true, true
	case eventbus.EventRunCancelled:
		return acp.RunEvent{Type: "complete", StopReason: acp.StopCancelled}, true, true
	case eventbus.EventRunFailed:
		payload, _ := event.RunFailedPayload()
		return acp.RunEvent{Type: "error", Error: firstNonEmpty(payload.Error, "run failed")}, true, true
	default:
		return acp.RunEvent{}, false, false
	}
}

func (g *embeddedInteractiveGateway) SubmitRun(ctx context.Context, sessionKey, message string, images []string) (string, <-chan acp.RunEvent, error) {
	return g.SubmitRunWithOptions(ctx, sessionKey, message, images, acp.PromptOptions{})
}

func (g *embeddedInteractiveGateway) SubmitRunWithOptions(ctx context.Context, sessionKey, message string, images []string, options acp.PromptOptions) (string, <-chan acp.RunEvent, error) {
	turnID := newInteractiveTurnID()
	sessionID := ""
	if g != nil && g.app != nil && g.app.Sessions != nil {
		session, err := g.app.Sessions.GetOrCreate(ctx, sessionKey, strings.TrimSpace(options.Model))
		if err == nil && session != nil {
			sessionID = session.ID
		}
	}
	sub := g.app.Runtime.SubscribeEvents(512)
	events := make(chan acp.RunEvent, 128)
	ready := make(chan string, 1)
	go g.coordinateEmbeddedInteraction(ctx, sub, sessionID, turnID, sessionKey, message, images, options, events, ready)

	select {
	case runID := <-ready:
		return runID, events, nil
	case <-ctx.Done():
		if sub != nil {
			sub.Close()
		}
		return "", nil, ctx.Err()
	}
}

func effectivePromptContent(content string, blocks []contextengine.ContentBlock) string {
	if trimmed := strings.TrimSpace(content); trimmed != "" {
		return trimmed
	}
	parts := make([]string, 0, len(blocks))
	for _, block := range blocks {
		if block.Type != contextengine.ContentBlockText {
			continue
		}
		if text := strings.TrimSpace(block.Text); text != "" {
			parts = append(parts, text)
		}
	}
	return strings.Join(parts, "\n")
}

func (g *embeddedInteractiveGateway) coordinateEmbeddedInteraction(
	ctx context.Context,
	sub *eventbus.Subscription,
	sessionID, turnID, sessionKey, message string,
	images []string,
	options acp.PromptOptions,
	out chan<- acp.RunEvent,
	ready chan<- string,
) {
	defer close(out)
	var eventStream <-chan eventbus.Event
	if sub != nil {
		defer sub.Close()
		eventStream = sub.Events()
	}

	resultCh := make(chan embeddedInteractionResult, 1)
	go func() {
		result, err := g.app.Runtime.Interact(ctx, runtimesvc.InteractionRequest{
			SessionKey:         sessionKey,
			Content:            message,
			ContentBlocks:      append([]contextengine.ContentBlock(nil), options.ContentBlocks...),
			Images:             append([]string(nil), images...),
			Model:              strings.TrimSpace(options.Model),
			StructuredCommand:  runtimeStructuredCommand(options.StructuredCommand),
			StructuredApproval: runtimeStructuredApproval(options.StructuredApproval),
			Metadata:           interactiveRequestMetadata(turnID),
		})
		resultCh <- embeddedInteractionResult{Result: result, Error: err}
	}()

	var (
		runID           string
		readySent       bool
		resultDone      bool
		streamedRunless bool
		terminal        bool
		bufferedEvents  []eventbus.Event
	)
	markReady := func(value string) {
		if readySent {
			return
		}
		readySent = true
		select {
		case ready <- value:
		default:
		}
	}
	flushBufferedRunEvents := func() bool {
		if runID == "" || len(bufferedEvents) == 0 {
			bufferedEvents = nil
			return false
		}
		terminal := false
		for _, event := range bufferedEvents {
			if strings.TrimSpace(event.RunID) != runID {
				continue
			}
			if mapped, isTerminal, ok := g.mapEvent(ctx, event); ok {
				out <- mapped
				if isTerminal {
					terminal = true
				}
			}
		}
		bufferedEvents = nil
		return terminal
	}
	processEvent := func(event eventbus.Event) bool {
		if runID != "" {
			if strings.TrimSpace(event.RunID) != runID {
				return false
			}
			if mapped, isTerminal, ok := g.mapEvent(ctx, event); ok {
				out <- mapped
				if isTerminal {
					return true
				}
			}
			return false
		}
		if matchesInteractiveTurnEvent(event, turnID) || matchesInteractiveRunlessSessionEvent(event, sessionID, turnID) {
			streamedRunless = true
			markReady("")
			if mapped, isTerminal, ok := g.mapEvent(ctx, event); ok {
				out <- mapped
				if isTerminal {
					terminal = true
					return resultDone
				}
			}
			return false
		}
		if !resultDone && strings.TrimSpace(event.RunID) != "" {
			bufferedEvents = append(bufferedEvents, event)
		}
		return false
	}
	drainPendingEvents := func() bool {
		for {
			select {
			case event, ok := <-eventStream:
				if !ok {
					return false
				}
				if processEvent(event) {
					return true
				}
			default:
				return false
			}
		}
	}
	for {
		select {
		case <-ctx.Done():
			if !readySent {
				markReady("")
			}
			out <- acp.RunEvent{Type: "error", Error: ctx.Err().Error(), Runless: runID == ""}
			return
		case outcome := <-resultCh:
			resultDone = true
			if outcome.Error != nil {
				markReady("")
				if !streamedRunless || !terminal {
					out <- acp.RunEvent{Type: "error", Error: outcome.Error.Error(), Runless: true}
				}
				return
			}
			if outcome.Result == nil {
				markReady("")
				relayInteractiveEvents(out, interactiveImmediateEvents("", ""))
				return
			}
			if outcome.Result.Run != nil && outcome.Result.SubmitRequest != nil && strings.TrimSpace(outcome.Result.Run.ID) != "" {
				runID = strings.TrimSpace(outcome.Result.Run.ID)
				markReady(runID)
				if flushBufferedRunEvents() {
					return
				}
				continue
			}
			if drainPendingEvents() {
				return
			}
			if streamedRunless {
				markReady("")
				if terminal {
					return
				}
				if errText := strings.TrimSpace(outcome.Result.Error); errText != "" {
					out <- acp.RunEvent{Type: "error", Error: errText, Runless: true}
					return
				}
				continue
			}
			markReady("")
			relayInteractiveEvents(out, interactiveImmediateEvents(runtimesvc.RenderDirectInteractionReply(outcome.Result, effectivePromptContent(message, options.ContentBlocks)), outcome.Result.Error))
			return
		case event, ok := <-eventStream:
			if !ok {
				continue
			}
			if processEvent(event) {
				return
			}
		}
	}
}

func (g *embeddedInteractiveGateway) CancelRun(ctx context.Context, _ string, runID string) error {
	_, err := g.app.Runtime.CancelRun(ctx, runID)
	return err
}

func (g *embeddedInteractiveGateway) ResolveApproval(ctx context.Context, requestID string, resolution approval.Resolution) error {
	_, err := g.app.Runtime.ResolveApprovalView(ctx, strings.TrimSpace(requestID), resolution)
	return err
}

func (g *embeddedInteractiveGateway) ListSessions(ctx context.Context, _, _ int) ([]acp.SessionInfo, error) {
	items, err := g.app.Runtime.ListSessions(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]acp.SessionInfo, 0, len(items))
	for _, item := range items {
		out = append(out, acp.SessionInfo{
			SessionID:  item.ID,
			SessionKey: item.Key,
			Status:     acp.SessionIdle,
			CreatedAt:  item.CreatedAt,
		})
	}
	return out, nil
}

func (g *embeddedInteractiveGateway) ResolveSession(ctx context.Context, key string) (*acp.SessionInfo, error) {
	session, err := agent.LoadSessionMetadataByKey(ctx, g.app.Sessions, key, agent.ScopeFilter{})
	if err != nil {
		return nil, err
	}
	return &acp.SessionInfo{
		SessionID:  session.ID,
		SessionKey: session.Key,
		Status:     acp.SessionIdle,
		CreatedAt:  session.CreatedAt,
	}, nil
}

func (g *embeddedInteractiveGateway) ResetSession(ctx context.Context, key string) error {
	session, err := agent.LoadSessionMetadataByKey(ctx, g.app.Sessions, key, agent.ScopeFilter{})
	if err != nil {
		return err
	}
	return g.app.Runtime.DeleteSession(ctx, session.ID)
}

func (g *embeddedInteractiveGateway) mapEvent(ctx context.Context, event eventbus.Event) (acp.RunEvent, bool, bool) {
	runless := strings.TrimSpace(event.RunID) == ""
	switch event.Type {
	case eventbus.EventModelTextDelta:
		payload, _ := event.ModelTextDeltaPayload()
		return acp.RunEvent{Type: "text_delta", Text: payload.Delta, Runless: runless}, false, true
	case eventbus.EventModelStreamComplete:
		payload, _ := event.ModelStreamCompletePayload()
		if runless {
			return acp.RunEvent{
				Type:       "complete",
				StopReason: acp.StopEndTurn,
				Usage:      usageInfoFromModelStreamComplete(payload),
				Runless:    true,
			}, true, true
		}
		usage := usageInfoFromModelStreamComplete(payload)
		if usage == nil {
			return acp.RunEvent{}, false, false
		}
		return acp.RunEvent{Type: "usage", Usage: usage}, false, true
	case eventbus.EventModelFailover:
		payload, _ := event.ModelFailoverPayload()
		failover := modelFailoverInfoFromAttrs(payload)
		if failover == nil {
			return acp.RunEvent{}, false, false
		}
		return acp.RunEvent{Type: "model_failover", ModelFailover: failover}, false, true
	case eventbus.EventToolExecuted:
		return acp.RunEvent{
			Type:       "tool_end",
			ToolName:   firstToolName(event),
			ToolOutput: firstToolSummary(event),
		}, false, true
	case eventbus.EventApprovalRequested:
		approvalPayload, _ := event.ApprovalRequestedPayload()
		req, err := embeddedPermissionRequest(ctx, g.app, approvalPayload.ApprovalID)
		if err != nil {
			return acp.RunEvent{Type: "error", Error: err.Error()}, true, true
		}
		return acp.RunEvent{Type: "permission_request", Permission: req}, false, true
	case eventbus.EventRunCompleted:
		return acp.RunEvent{Type: "complete", StopReason: acp.StopEndTurn}, true, true
	case eventbus.EventRunCancelled:
		return acp.RunEvent{Type: "complete", StopReason: acp.StopCancelled}, true, true
	case eventbus.EventRunFailed:
		payload, _ := event.RunFailedPayload()
		return acp.RunEvent{Type: "error", Error: firstNonEmpty(payload.Error, "run failed")}, true, true
	default:
		return acp.RunEvent{}, false, false
	}
}

func usageInfoFromModelStreamComplete(payload eventbus.ModelStreamCompleteAttrs) *acp.UsageInfo {
	if payload.TotalTokens <= 0 {
		return nil
	}
	return &acp.UsageInfo{
		PromptTokens:     payload.PromptTokens,
		CompletionTokens: payload.CompletionTokens,
		TotalTokens:      payload.TotalTokens,
	}
}

func modelFailoverInfoFromAttrs(payload eventbus.ModelFailoverAttrs) *acp.ModelFailoverInfo {
	original := strings.TrimSpace(payload.FromModel)
	fallback := strings.TrimSpace(payload.ToModel)
	if original == "" && fallback == "" {
		return nil
	}
	return &acp.ModelFailoverInfo{
		OriginalModel: original,
		FallbackModel: fallback,
		Reason:        strings.TrimSpace(payload.Reason),
	}
}

func externalPermissionRequest(ctx context.Context, client *GatewayClient, approvalID string) (*acp.PermissionRequest, error) {
	var view runtimesvc.ApprovalView
	if err := client.Get(ctx, "/runtime/approvals/"+approvalID, &view); err != nil {
		return nil, err
	}
	return approvalViewToPermissionRequest(&view), nil
}

func embeddedPermissionRequest(ctx context.Context, app *bootstrap.App, approvalID string) (*acp.PermissionRequest, error) {
	view, err := app.Runtime.GetApprovalView(ctx, approvalID)
	if err != nil {
		return nil, err
	}
	return approvalViewToPermissionRequest(view), nil
}

func approvalViewToPermissionRequest(view *runtimesvc.ApprovalView) *acp.PermissionRequest {
	if view == nil {
		return nil
	}
	toolName := ""
	if len(view.ToolCalls) > 0 {
		toolName = view.ToolCalls[0].Name
	}
	input := ""
	if len(view.ToolCalls) > 0 && len(view.ToolCalls[0].Input) > 0 {
		if data, err := json.Marshal(view.ToolCalls[0].Input); err == nil {
			input = string(data)
		}
	}
	description := strings.Join(view.Reasons, "; ")
	if description == "" {
		description = "Tool execution requires approval"
	}
	req := &acp.PermissionRequest{
		RequestID:   view.ID,
		SessionID:   view.SessionID,
		ToolName:    toolName,
		Description: description,
		Input:       input,
	}
	req.ResourceScopeSummary = strings.TrimSpace(view.ResourceScopeSummary)
	if req.ResourceScopeSummary == "" && len(view.ToolCalls) > 0 {
		req.ResourceScopeSummary = strings.TrimSpace(view.ToolCalls[0].ResourceScope.Normalized().Summary)
	}
	if governance := view.Governance; governance != nil {
		req.GovernanceSummary = strings.TrimSpace(governance.Summary)
		req.ScopeSummary = controlplane.ScopeSummary(governance.Scope)
		if governance.Policy != nil {
			req.PolicySummary = strings.TrimSpace(governance.Policy.Summary)
			req.PolicyAction = strings.TrimSpace(string(governance.Policy.Action))
			if governance.Policy.ApprovalPolicy != nil {
				req.DefaultGrantScope = strings.TrimSpace(string(governance.Policy.ApprovalPolicy.DefaultScope))
				req.MaxGrantScope = strings.TrimSpace(string(governance.Policy.ApprovalPolicy.MaxScope))
			}
		}
	}
	if metadataBool(view.Metadata["harness_requires_external_side_effect"]) {
		req.RequiresExternalSideEffect = true
	}
	return req
}

func metadataBool(value any) bool {
	switch strings.ToLower(strings.TrimSpace(fmt.Sprint(value))) {
	case "1", "t", "true", "y", "yes", "on":
		return true
	default:
		return false
	}
}

func lastRuntimeEventID(ctx context.Context, client *GatewayClient) (string, error) {
	var response struct {
		Items []struct {
			ID string `json:"id"`
		} `json:"items"`
	}
	if err := client.Get(ctx, "/runtime/events?limit=1", &response); err != nil {
		return "", err
	}
	if len(response.Items) == 0 {
		return "", nil
	}
	return response.Items[0].ID, nil
}

func firstToolName(event eventbus.Event) string {
	if payload, ok := event.ToolExecutedPayload(); ok {
		if len(payload.Results) > 0 && strings.TrimSpace(payload.Results[0].ToolName) != "" {
			return payload.Results[0].ToolName
		}
		if len(payload.ToolNames) > 0 && strings.TrimSpace(payload.ToolNames[0]) != "" {
			return strings.TrimSpace(payload.ToolNames[0])
		}
		if strings.TrimSpace(payload.TaskTitle) != "" {
			return payload.TaskTitle
		}
	}
	return "tool"
}

func firstToolSummary(event eventbus.Event) string {
	if payload, ok := event.ToolExecutedPayload(); ok {
		for _, item := range payload.Results {
			if errText := compactInteractiveToolSummary(item.Error); errText != "" {
				return errText
			}
			if suppressInteractiveToolStatus(item) {
				continue
			}
			if summary := compactInteractiveToolSummary(item.Summary); summary != "" {
				return summary
			}
			if hint := interactiveToolHint(item); hint != "" {
				return hint
			}
		}
	}
	return ""
}

func suppressInteractiveToolStatus(item eventbus.ToolExecutionResultPayload) bool {
	status := strings.ToLower(strings.TrimSpace(item.Status))
	if status == "error" || strings.TrimSpace(item.Error) != "" {
		return false
	}
	switch strings.ToLower(strings.TrimSpace(item.ToolName)) {
	case "exec.run", "exec.shell", "exec.script", "exec.which",
		"fs.read", "file.read", "fs.stat", "file.stat",
		"fs.list", "file.list", "fs.tree", "file.tree",
		"fs.find", "file.find":
		return true
	default:
		return false
	}
}

func compactInteractiveToolSummary(text string) string {
	text = strings.Join(strings.Fields(strings.TrimSpace(text)), " ")
	if text == "" {
		return ""
	}
	lower := strings.ToLower(text)
	switch {
	case strings.HasPrefix(text, "{"),
		strings.HasPrefix(text, "["),
		strings.HasPrefix(text, "<"),
		strings.HasPrefix(lower, "```"),
		strings.HasPrefix(lower, "error:"),
		strings.HasPrefix(lower, "curl "),
		strings.HasPrefix(lower, "cd "),
		strings.HasPrefix(lower, "$ "),
		strings.HasPrefix(lower, "<!doctype"),
		strings.HasPrefix(lower, "<html"):
		return ""
	}
	for _, marker := range []string{
		`"command":`,
		`"stdout":`,
		`"stderr":`,
		`"exit_code":`,
		`"tool_name":`,
		`"tool_call_id":`,
		`"artifact_uri":`,
		`"tool_execution_error":`,
		`"content_type":`,
		`"url":`,
		`"html":`,
		`"selector":`,
		"&&",
	} {
		if strings.Contains(lower, marker) {
			return ""
		}
	}
	if len(text) <= 120 {
		return text
	}
	return strings.TrimSpace(text[:120]) + "..."
}

func interactiveToolHint(item eventbus.ToolExecutionResultPayload) string {
	payload := interactiveToolPayload(item.ToolResult)
	if len(payload) == 0 {
		return ""
	}
	toolName := strings.ToLower(strings.TrimSpace(item.ToolName))
	switch toolName {
	case "exec.shell", "exec.run", "exec.script":
		if command := stringValue(payload["command"]); command != "" {
			return compactInteractiveToolSummary(command)
		}
	case "fs.list", "file.list", "fs.tree", "file.tree":
		count, ok := intValueFromAny(payload["count"])
		if !ok {
			return ""
		}
		path := stringValue(payload["path"])
		if path == "" {
			return fmt.Sprintf("%d entries", count)
		}
		return fmt.Sprintf("%d entries in %s", count, path)
	case "fs.find", "file.find":
		count, ok := intValueFromAny(payload["count"])
		if !ok {
			return ""
		}
		pattern := stringValue(payload["pattern"])
		path := stringValue(payload["path"])
		switch {
		case pattern != "" && path != "":
			return fmt.Sprintf("%d matches for %q in %s", count, pattern, path)
		case path != "":
			return fmt.Sprintf("%d matches in %s", count, path)
		default:
			return fmt.Sprintf("%d matches", count)
		}
	case "fs.read", "file.read", "fs.stat", "file.stat":
		if path := stringValue(payload["path"]); path != "" {
			if toolName == "fs.stat" || toolName == "file.stat" {
				return "stat " + path
			}
			return "read " + path
		}
	}
	return ""
}

func interactiveToolPayload(raw map[string]any) map[string]any {
	if len(raw) == 0 {
		return nil
	}
	if structured, ok := raw["structured"].(map[string]any); ok && len(structured) > 0 {
		return structured
	}
	for _, key := range []string{"content", "transcript_text", "summary"} {
		text := stringValue(raw[key])
		if text == "" || (!strings.HasPrefix(text, "{") && !strings.HasPrefix(text, "[")) {
			continue
		}
		var decoded map[string]any
		if json.Unmarshal([]byte(text), &decoded) == nil && len(decoded) > 0 {
			return decoded
		}
	}
	return nil
}

func intValueFromAny(value any) (int, bool) {
	switch typed := value.(type) {
	case int:
		return typed, true
	case int32:
		return int(typed), true
	case int64:
		return int(typed), true
	case float64:
		return int(typed), true
	case float32:
		return int(typed), true
	default:
		return 0, false
	}
}

func stringValue(value any) string {
	if value == nil {
		return ""
	}
	return strings.TrimSpace(fmt.Sprint(value))
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
