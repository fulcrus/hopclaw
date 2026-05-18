package agent

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/fulcrus/hopclaw/contextengine"
	"github.com/fulcrus/hopclaw/eventbus"
	"github.com/fulcrus/hopclaw/internal/jsonrepair"
	"github.com/fulcrus/hopclaw/internal/meta"
	"github.com/fulcrus/hopclaw/logging"
)

const DefaultWatchIntakeTimeout = 20 * time.Second

type watchIntakeDecision struct {
	Action           string  `json:"action,omitempty"`
	Supported        bool    `json:"supported"`
	NeedConfirmation bool    `json:"need_confirmation,omitempty"`
	Name             string  `json:"name,omitempty"`
	SourceKind       string  `json:"source_kind,omitempty"`
	SourceURL        string  `json:"source_url,omitempty"`
	SourcePath       string  `json:"source_path,omitempty"`
	SourceSessionKey string  `json:"source_session_key,omitempty"`
	CalendarQuery    string  `json:"calendar_query,omitempty"`
	MailboxFolder    string  `json:"mailbox_folder,omitempty"`
	MailboxQuery     string  `json:"mailbox_query,omitempty"`
	WebhookID        string  `json:"webhook_id,omitempty"`
	WebhookSenderID  string  `json:"webhook_sender_id,omitempty"`
	InboxLimit       int     `json:"inbox_limit,omitempty"`
	Interval         string  `json:"interval,omitempty"`
	TargetRef        string  `json:"target_ref,omitempty"`
	RemoveAll        bool    `json:"remove_all,omitempty"`
	Prompt           string  `json:"prompt,omitempty"`
	FireOnStart      bool    `json:"fire_on_start,omitempty"`
	Summary          string  `json:"summary,omitempty"`
	Question         string  `json:"question,omitempty"`
	Reason           string  `json:"reason,omitempty"`
	Confidence       float64 `json:"confidence,omitempty"`
}

const watchIntakeSystemPrompt = `You are HopClaw's internal watch workflow intake.
Decide whether the user wants a persistent monitoring job that the current watch service can represent.
Return JSON only.

Output format:
{"action":"create|cancel","supported":true|false,"need_confirmation":true|false,"name":"...","source_kind":"http|file|feed|mailbox|browser_snapshot|calendar|webhook|structured_app_inbox","source_url":"...","source_path":"...","source_session_key":"...","calendar_query":"...","mailbox_folder":"...","mailbox_query":"...","webhook_id":"...","webhook_sender_id":"...","inbox_limit":20,"interval":"...","target_ref":"...","remove_all":true|false,"prompt":"...","fire_on_start":true|false,"summary":"...","question":"...","reason":"...","confidence":0.0-1.0}

Rules:
- action=create when the user wants a new persistent monitoring job.
- action=cancel when the user wants to stop, remove, disable, or unsubscribe an existing monitoring job.
- Only set supported=true when the request is clearly about recurring or trigger-based monitoring/checking.
- For cancel actions, set supported=true when the request clearly refers to existing monitoring jobs.
- For cancel actions, set target_ref to a concrete watch id, URL, host/domain, file path, session key, or other stable identifier when present.
- For cancel actions, set remove_all=true only when the user clearly wants every matching monitoring job removed. Otherwise leave remove_all=false.
- Only set source_url when there is a concrete HTTP or HTTPS URL to monitor.
- Only set source_path when there is a concrete local file path to monitor.
- For structured app inbox monitoring, set source_kind=structured_app_inbox and provide source_session_key.
- For calendar monitoring, set source_kind=calendar. calendar_query is optional.
- For mailbox monitoring, set source_kind=mailbox and provide mailbox_folder when possible.
- For webhook inbox monitoring, set source_kind=webhook and provide source_session_key when possible. Otherwise provide webhook_id and webhook_sender_id.
- If the user wants monitoring but no concrete target URL, file path, session key, mailbox target, webhook target, or supported calendar target is available, set supported=false and need_confirmation=true.
- summary should be user-facing and in the same language as the user request.
- question should be short and user-facing when need_confirmation=true.
- interval must be a Go duration string like 10m, 1h, or 24h.
- Return JSON only.`

func (a *AgentComponent) executeWatchMode(ctx context.Context, run *Run, session *Session) error {
	if a == nil {
		return ErrModelClientNil
	}
	if run == nil || session == nil {
		return errors.New("watch mode requires run and session")
	}
	if a.watchFlow == nil {
		return a.executeLoop(ctx, run, session, func() {})
	}
	decision, err := a.classifyWatchRequest(ctx, run, session)
	if err != nil {
		return a.handleRunExecutionError(ctx, &run, err)
	}
	if watchDecisionRequestsCancellation(decision) {
		cancelFlow, ok := a.watchFlow.(WatchCancelWorkflow)
		if !ok {
			return a.handleRunExecutionError(ctx, &run, errors.New("watch cancellation workflow is not configured"))
		}
		cancelResult, err := cancelFlow.Cancel(ctx, WatchWorkflowCancelRequest{
			RunID:      run.ID,
			SessionKey: defaultString(session.Key, "watch:"+run.ID),
			Query:      latestPlanningMessage(session),
			TargetRef:  strings.TrimSpace(decision.TargetRef),
			RemoveAll:  decision.RemoveAll,
		})
		if err != nil {
			return a.handleRunExecutionError(ctx, &run, err)
		}
		content := strings.TrimSpace(cancelResult.Summary)
		if content == "" {
			content = "Monitoring has been cancelled."
		}
		appendAssistantRunMessage(session, run.ID, content)
		if err := a.sessions.Save(ctx, session); err != nil {
			return err
		}
		return a.completeRun(ctx, run, session)
	}
	if !decision.Supported || !watchDecisionHasConcreteSource(decision) {
		question := strings.TrimSpace(decision.Question)
		if question == "" {
			question = "Please reply with the exact URL or local file path you want me to monitor."
		}
		summary := strings.TrimSpace(decision.Summary)
		if summary == "" {
			summary = "I need a concrete URL or local file path before I can set up continuous monitoring."
		}
		report := &RunPreflightReport{
			State:        RunPreflightNeedsConfirmation,
			Summary:      summary,
			Question:     question,
			Prompt:       question,
			Blocking:     true,
			ContinueHint: "After you reply, I will continue with the monitoring setup and send back the created watch.",
			GeneratedAt:  time.Now().UTC(),
			Checks: []RunPreflightCheck{{
				ID:       "watch_target_required",
				Title:    "Need A Monitor Target",
				State:    RunPreflightNeedsConfirmation,
				Detail:   summary,
				Blocking: true,
			}},
		}
		transitionRun(run, RunWaitingInput, PhasePreparing, withRunError(""))
		run.Preflight = report
		if err := a.runs.Update(ctx, run); err != nil {
			return err
		}
		logging.LogIfErr(ctx, a.emit(ctx, eventbus.NewRunPreflightUpdatedEvent(
			run.ID,
			run.SessionID,
			preflightEventPayload(report),
			nil,
		)), "emit event failed")
		logging.LogIfErr(ctx, a.emit(ctx, eventbus.NewRunWaitingInputEvent(
			run.ID,
			run.SessionID,
			preflightEventPayload(report),
			nil,
		)), "emit event failed")
		return nil
	}

	result, err := a.watchFlow.Create(ctx, WatchWorkflowRequest{
		RunID:            run.ID,
		SessionKey:       defaultString(session.Key, "watch:"+run.ID),
		Name:             strings.TrimSpace(decision.Name),
		SourceKind:       strings.TrimSpace(decision.SourceKind),
		SourceURL:        strings.TrimSpace(decision.SourceURL),
		SourcePath:       strings.TrimSpace(decision.SourcePath),
		SourceSessionKey: strings.TrimSpace(decision.SourceSessionKey),
		CalendarQuery:    strings.TrimSpace(decision.CalendarQuery),
		MailboxFolder:    strings.TrimSpace(decision.MailboxFolder),
		MailboxQuery:     strings.TrimSpace(decision.MailboxQuery),
		WebhookID:        strings.TrimSpace(decision.WebhookID),
		WebhookSenderID:  strings.TrimSpace(decision.WebhookSenderID),
		InboxLimit:       decision.InboxLimit,
		Interval:         strings.TrimSpace(decision.Interval),
		Prompt:           strings.TrimSpace(decision.Prompt),
		Model:            run.Model,
		FireOnStart:      decision.FireOnStart,
	})
	if err != nil {
		return a.handleRunExecutionError(ctx, &run, err)
	}
	if result == nil {
		return a.handleRunExecutionError(ctx, &run, errors.New("watch workflow returned nil result"))
	}

	content := strings.TrimSpace(result.Summary)
	if content == "" {
		content = "Monitoring has been set up."
	}
	appendAssistantRunMessage(session, run.ID, content)
	if err := a.sessions.Save(ctx, session); err != nil {
		return err
	}
	return a.completeRun(ctx, run, session)
}

func (a *AgentComponent) classifyWatchRequest(ctx context.Context, run *Run, session *Session) (watchIntakeDecision, error) {
	if a == nil || a.model == nil || run == nil || session == nil {
		return watchIntakeDecision{}, ErrModelClientNil
	}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), DefaultWatchIntakeTimeout)
	defer cancel()
	payload, err := json.Marshal(map[string]any{
		"message":         latestPlanningMessage(session),
		"session_summary": session.Summary,
	})
	if err != nil {
		return watchIntakeDecision{}, err
	}
	response, err := a.model.Chat(ctx, ChatRequest{
		Model:        run.Model,
		SystemPrompt: watchIntakeSystemPrompt,
		Messages: []contextengine.Message{{
			Role:    contextengine.RoleUser,
			Content: string(payload),
		}},
		Budget: contextengine.Budget{
			ContextWindow:  2048,
			MaxInputTokens: 1024,
			ReservedOutput: 240,
		},
	})
	if err != nil {
		return watchIntakeDecision{}, err
	}
	if response == nil {
		return watchIntakeDecision{}, ErrModelClientNil
	}
	return parseWatchIntakeDecision(response.Message.Content), nil
}

func parseWatchIntakeDecision(raw string) watchIntakeDecision {
	var decision watchIntakeDecision
	if err := jsonrepair.DecodeJSONObjectCandidate(raw, &decision); err != nil {
		return watchIntakeDecision{}
	}
	return decision
}

func watchDecisionHasConcreteSource(decision watchIntakeDecision) bool {
	if strings.TrimSpace(decision.SourceKind) == "calendar" {
		return true
	}
	if strings.TrimSpace(decision.SourceKind) == "structured_app_inbox" {
		return strings.TrimSpace(decision.SourceSessionKey) != ""
	}
	if strings.TrimSpace(decision.SourceKind) == "webhook" {
		return strings.TrimSpace(decision.SourceSessionKey) != "" || (strings.TrimSpace(decision.WebhookID) != "" && strings.TrimSpace(decision.WebhookSenderID) != "")
	}
	return strings.TrimSpace(decision.SourceURL) != "" || strings.TrimSpace(decision.SourcePath) != "" || strings.TrimSpace(decision.MailboxFolder) != ""
}

func watchDecisionRequestsCancellation(decision watchIntakeDecision) bool {
	return strings.EqualFold(strings.TrimSpace(decision.Action), "cancel")
}

func appendAssistantRunMessage(session *Session, runID, content string) {
	if session == nil || strings.TrimSpace(content) == "" {
		return
	}
	now := time.Now().UTC()
	session.Messages = append(session.Messages, contextengine.Message{
		Role:      contextengine.RoleAssistant,
		Content:   strings.TrimSpace(content),
		CreatedAt: now,
		Metadata: map[string]any{
			meta.KeyRunID: runID,
		},
	})
	session.UpdatedAt = now
}
