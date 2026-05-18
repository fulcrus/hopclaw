package triage

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	automationintent "github.com/fulcrus/hopclaw/automation/intent"
	"github.com/fulcrus/hopclaw/contextengine"
	"github.com/fulcrus/hopclaw/internal/semanticschema"
)

type ChatRequest struct {
	Model        string
	SystemPrompt string
	Payload      string
	Budget       contextengine.Budget
}

type ChatFunc func(ctx context.Context, req ChatRequest) (string, error)

type ModelTriage struct {
	chat         ChatFunc
	timeout      time.Duration
	defaultModel string
}

func NewModelTriage(chat ChatFunc, timeout time.Duration) *ModelTriage {
	if chat == nil {
		return nil
	}
	if timeout <= 0 {
		timeout = 3 * time.Second
	}
	return &ModelTriage{chat: chat, timeout: timeout}
}

func (m *ModelTriage) WithDefaultModel(model string) *ModelTriage {
	if m != nil {
		m.defaultModel = strings.TrimSpace(model)
	}
	return m
}

type RunRequest struct {
	Model            string             `json:"model,omitempty"`
	Message          string             `json:"message"`
	SessionSummary   string             `json:"session_summary,omitempty"`
	PlannerAvailable bool               `json:"planner_available,omitempty"`
	LanguageHint     string             `json:"language_hint,omitempty"`
	MainSemanticPath bool               `json:"main_semantic_path,omitempty"`
	SemanticSignal   *RunSemanticSignal `json:"semantic_signal,omitempty"`
}

type RunSemanticSignal struct {
	LanguageHint        string `json:"language_hint,omitempty"`
	MainSemanticPath    bool   `json:"main_semantic_path,omitempty"`
	RequiresCurrentInfo bool   `json:"requires_current_info,omitempty"`
}

type RunDecision struct {
	ExecutionMode       string   `json:"execution_mode,omitempty"`
	NeedsReference      bool     `json:"needs_reference,omitempty"`
	NeedsConfirmation   bool     `json:"needs_confirmation,omitempty"`
	RequiresCurrentInfo bool     `json:"requires_current_info,omitempty"`
	SuggestedDomains    []string `json:"suggested_domains,omitempty"`
	Reason              string   `json:"reason,omitempty"`
	Confidence          float64  `json:"confidence,omitempty"`
}

type IngressRunState struct {
	ID           string   `json:"id,omitempty"`
	Status       string   `json:"status,omitempty"`
	Phase        string   `json:"phase,omitempty"`
	ToolRounds   int      `json:"tool_rounds,omitempty"`
	RecentTools  []string `json:"recent_tools,omitempty"`
	OriginalTask string   `json:"original_task,omitempty"`
	ActiveTask   string   `json:"active_task,omitempty"`
	Completed    int      `json:"completed_tasks,omitempty"`
	Total        int      `json:"total_tasks,omitempty"`
}

type IngressApprovalState struct {
	ID   string `json:"id,omitempty"`
	Kind string `json:"kind,omitempty"`
}

type IngressMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type IngressRequest struct {
	Model           string                `json:"model,omitempty"`
	Message         string                `json:"message"`
	ActiveRun       *IngressRunState      `json:"active_run,omitempty"`
	PendingApproval *IngressApprovalState `json:"pending_approval,omitempty"`
	RecentMessages  []IngressMessage      `json:"recent_messages,omitempty"`
}

type IngressDecision struct {
	Intent     string  `json:"intent,omitempty"`
	Reason     string  `json:"reason,omitempty"`
	Confidence float64 `json:"confidence,omitempty"`
}

type InteractionRequest struct {
	Model           string                `json:"model,omitempty"`
	Message         string                `json:"message"`
	SessionState    string                `json:"session_state,omitempty"`
	ActiveRun       *IngressRunState      `json:"active_run,omitempty"`
	PendingApproval *IngressApprovalState `json:"pending_approval,omitempty"`
	WaitingInput    bool                  `json:"waiting_input,omitempty"`
	WaitingApproval bool                  `json:"waiting_approval,omitempty"`
	RecentMessages  []IngressMessage      `json:"recent_messages,omitempty"`
}

type InteractionDecision struct {
	SpeechAct   string  `json:"speech_act,omitempty"`
	TargetScope string  `json:"target_scope,omitempty"`
	ReplyAct    string  `json:"reply_act,omitempty"`
	Reason      string  `json:"reason,omitempty"`
	Confidence  float64 `json:"confidence,omitempty"`
}

type InteractionIngressRequest struct {
	Model           string                           `json:"model,omitempty"`
	Message         string                           `json:"message"`
	SessionState    string                           `json:"session_state,omitempty"`
	ActiveRun       *IngressRunState                 `json:"active_run,omitempty"`
	PendingApproval *IngressApprovalState            `json:"pending_approval,omitempty"`
	WaitingInput    bool                             `json:"waiting_input,omitempty"`
	WaitingApproval bool                             `json:"waiting_approval,omitempty"`
	RecentMessages  []IngressMessage                 `json:"recent_messages,omitempty"`
	Inventory       []automationintent.InventoryItem `json:"inventory,omitempty"`
}

type InteractionIngressLanguage struct {
	Family     string  `json:"family,omitempty"`
	Script     string  `json:"script,omitempty"`
	Confidence float64 `json:"confidence,omitempty"`
}

type InteractionIngressDecision struct {
	SpeechAct           string                      `json:"speech_act,omitempty"`
	TargetScope         string                      `json:"target_scope,omitempty"`
	ReplyAct            string                      `json:"reply_act,omitempty"`
	RequiresCurrentInfo bool                        `json:"requires_current_info,omitempty"`
	Language            *InteractionIngressLanguage `json:"language,omitempty"`
	Reason              string                      `json:"reason,omitempty"`
	Confidence          float64                     `json:"confidence,omitempty"`
	AutomationPlan      automationintent.Plan       `json:"automation_plan,omitempty"`
}

type AutomationIntentRequest struct {
	Model          string                           `json:"model,omitempty"`
	Message        string                           `json:"message"`
	SessionState   string                           `json:"session_state,omitempty"`
	RecentMessages []IngressMessage                 `json:"recent_messages,omitempty"`
	Inventory      []automationintent.InventoryItem `json:"inventory,omitempty"`
}

func (m *ModelTriage) AnalyzeRun(ctx context.Context, req RunRequest) (RunDecision, error) {
	if m == nil || m.chat == nil {
		return RunDecision{}, context.Canceled
	}
	return callJSON[RunDecision](ctx, m, req.Model, runSystemPrompt, req, contextengine.Budget{
		ContextWindow:  3072,
		MaxInputTokens: 1400,
		ReservedOutput: 240,
	})
}

func (m *ModelTriage) RouteIngress(ctx context.Context, req IngressRequest) (IngressDecision, error) {
	if m == nil || m.chat == nil {
		return IngressDecision{}, context.Canceled
	}
	return callJSON[IngressDecision](ctx, m, req.Model, ingressSystemPrompt, req, contextengine.Budget{
		ContextWindow:  4096,
		MaxInputTokens: 2048,
		ReservedOutput: 220,
	})
}

func (m *ModelTriage) ClassifyInteraction(ctx context.Context, req InteractionRequest) (InteractionDecision, error) {
	if m == nil || m.chat == nil {
		return InteractionDecision{}, context.Canceled
	}
	return callJSON[InteractionDecision](ctx, m, req.Model, interactionSystemPrompt, req, contextengine.Budget{
		ContextWindow:  4096,
		MaxInputTokens: 2200,
		ReservedOutput: 260,
	})
}

func (m *ModelTriage) AnalyzeInteractionIngress(ctx context.Context, req InteractionIngressRequest) (InteractionIngressDecision, error) {
	if m == nil || m.chat == nil {
		return InteractionIngressDecision{}, context.Canceled
	}
	return callJSON[InteractionIngressDecision](ctx, m, req.Model, interactionIngressSystemPrompt, req, contextengine.Budget{
		ContextWindow:  6144,
		MaxInputTokens: 3200,
		ReservedOutput: 720,
	})
}

func (m *ModelTriage) AnalyzeAutomationIntent(ctx context.Context, req AutomationIntentRequest) (automationintent.Plan, error) {
	if m == nil || m.chat == nil {
		return automationintent.Plan{}, context.Canceled
	}
	return callJSON[automationintent.Plan](ctx, m, req.Model, automationIntentSystemPrompt, req, contextengine.Budget{
		ContextWindow:  4096,
		MaxInputTokens: 2200,
		ReservedOutput: 420,
	})
}

func callJSON[T any](ctx context.Context, triage *ModelTriage, model, systemPrompt string, payload any, budget contextengine.Budget) (T, error) {
	var zero T
	ctx, cancel := context.WithTimeout(ctx, triage.timeout)
	defer cancel()

	body, err := json.Marshal(payload)
	if err != nil {
		return zero, err
	}
	if triage.defaultModel != "" {
		model = triage.defaultModel
	}
	raw, err := triage.chat(ctx, ChatRequest{
		Model:        strings.TrimSpace(model),
		SystemPrompt: systemPrompt,
		Payload:      string(body),
		Budget:       budget,
	})
	if err != nil {
		return zero, err
	}
	return parseJSON[T](raw)
}

func parseJSON[T any](raw string) (T, error) {
	var out T
	candidate := strings.TrimSpace(raw)
	candidate = strings.TrimPrefix(candidate, "```json")
	candidate = strings.TrimPrefix(candidate, "```JSON")
	candidate = strings.TrimPrefix(candidate, "```")
	candidate = strings.TrimSuffix(candidate, "```")
	candidate = strings.TrimSpace(candidate)
	if err := json.Unmarshal([]byte(candidate), &out); err == nil {
		return out, nil
	}
	start := strings.Index(candidate, "{")
	end := strings.LastIndex(candidate, "}")
	if start < 0 || end <= start {
		return out, json.Unmarshal([]byte(candidate), &out)
	}
	err := json.Unmarshal([]byte(candidate[start:end+1]), &out)
	return out, err
}

var runSystemPrompt = semanticschema.BuildRunTriagePrompt()

var ingressSystemPrompt = semanticschema.BuildIngressRoutingPrompt("ingress triage engine")

const interactionSystemPrompt = `You are HopClaw's interaction classifier. A user sent one message in an existing chat. Decide what the message means in context and return JSON only.

Output format:
{"speech_act":"command|approval_reply|clarification_reply|task_followup|status_query|new_task|negative_feedback|casual_chat|meta_question|unknown","target_scope":"active_run|new_run|session|none","reply_act":"chat_reply|action_ack|status_reply|clarification_prompt|resume_ack|task_accept|task_result|task_failure","reason":"...","confidence":0.0-1.0}

Rules:
- Work from semantics, not language-specific keywords. The message may be in any language.
- Use the provided session_state, waiting flags, active_run, pending_approval, and recent_messages heavily.
- command: only for explicit operational controls such as cancel, stop, bind, unbind, retry, or status commands.
- approval_reply: only when the user is clearly approving or denying a pending approval.
- clarification_reply: only when the session is waiting for missing input and the user is supplying that missing detail.
- task_followup: user is steering, correcting, or extending the currently active run.
- status_query: user asks for progress, whether work is still running, or what happened to the current/recent task.
- new_task: user is requesting fresh work, even if phrased as a question.
- negative_feedback: user complains about the current or recent result without yet giving actionable correction details.
- casual_chat: greeting, thanks, acknowledgement, light social message, or presence check that should not create work.
- meta_question: asks about the assistant, capabilities, supported commands, or why the assistant behaved a certain way.
- unknown: only when genuinely ambiguous.
- If session_state is idle/completed_recently/failed_recently, do not assume there is an active task.
- Prefer reply_act=status_reply for status_query, reply_act=resume_ack for approval_reply/clarification_reply/task_followup, reply_act=task_accept for new_task, reply_act=chat_reply for casual_chat/meta_question, and reply_act=clarification_prompt for unknown when ambiguity is high.
- Return JSON only.`

const interactionIngressSystemPrompt = `You are HopClaw's unified interaction ingress classifier. Analyze one user message in context and return JSON only.

Output format:
{
  "speech_act":"command|approval_reply|clarification_reply|task_followup|status_query|new_task|negative_feedback|casual_chat|meta_question|unknown",
  "target_scope":"active_run|new_run|session|none",
  "reply_act":"chat_reply|action_ack|status_reply|clarification_prompt|resume_ack|task_accept|task_result|task_failure",
  "requires_current_info":false,
  "language":{"family":"...","script":"...","confidence":0.0},
  "reason":"...",
  "confidence":0.0,
  "automation_plan":{
    "action":"none|create|update|disable|delete|query",
    "kind":"cron|wakeup|watch|",
    "confidence":0.0,
    "reason":"...",
    "need_confirmation":false,
    "missing_info":[{"field":"...","question":"...","example":"...","required":true}],
    "selector":{"query":"...","kind":"cron|wakeup|watch|","ids":["..."],"names":["..."],"cities":["..."],"delivery_channel":"...","delivery_target":"..."},
    "spec":{"kind":"cron|wakeup|watch|","name":"...","schedule":"...","timezone":"...","session_key":"...","model":"...","prompt":"...","message":"...","delivery":{"channel":"...","target":"..."},"source_kind":"http|file|feed|browser_snapshot|mailbox|calendar|webhook|structured_app_inbox","source_url":"...","source_path":"...","interval":"...","fire_on_start":false,"enabled":true,"automation_id":"..."},
    "query":{"metric":"summary|list|notification_today|notification_total|notification_failures","limit":10}
  }
}

Rules:
- Work from semantics, not language-specific keywords. The user may write in any language.
- Deterministic slash commands, structured approvals, and numbered approval replies were already handled before this classifier.
- Always fill speech_act, target_scope, reply_act, reason, and confidence.
- Fill requires_current_info when the request depends on recent, current, or otherwise freshness-sensitive information.
- Fill language with a descriptive profile for the user's message. This is metadata only; do not use it to change the semantic decision.
- Use session_state, waiting flags, active_run, pending_approval, recent_messages, and inventory heavily.
- command: only for explicit operational controls such as cancel, stop, bind, unbind, retry, or status commands phrased in natural language.
- approval_reply: only when the user is clearly approving or denying a pending approval.
- clarification_reply: only when the session is waiting for missing input and the user is supplying that missing detail.
- task_followup: user is steering, correcting, or extending the currently active run.
- status_query: user asks for progress, whether work is still running, or what happened to the current/recent task.
- new_task: user is requesting fresh one-off work, even if phrased as a question.
- negative_feedback: user complains about the current or recent result without yet giving actionable correction details.
- casual_chat: greeting, thanks, acknowledgement, light social message, or presence check that should not create work.
- meta_question: asks about the assistant, capabilities, supported commands, or why the assistant behaved a certain way.
- unknown: only when genuinely ambiguous.
- automation_plan.action=none when the message is not about recurring automation CRUD or automation reporting.
- If the message is about automation CRUD/query, fill automation_plan completely and still set reply_act to the correct execution envelope:
  - query -> chat_reply
  - create/update/disable/delete with enough information -> action_ack
  - missing_info or need_confirmation -> clarification_prompt
- When active_run / waiting_approval / waiting_input is present, prefer current-run semantics unless the user is clearly performing automation management unrelated to the active run.
- If session_state is idle/completed_recently/failed_recently, do not assume there is an active task.
- Prefer reply_act=status_reply for status_query, reply_act=resume_ack for approval_reply/clarification_reply/task_followup, reply_act=task_accept for new_task, reply_act=chat_reply for casual_chat/meta_question, and reply_act=clarification_prompt for unknown when ambiguity is high.
- Never invent automation IDs that are not present in the supplied inventory.
- Return JSON only.`

const automationIntentSystemPrompt = `You are HopClaw's automation intent planner. Analyze one user message and return JSON only.

Output format:
{
  "action":"none|create|update|disable|delete|query",
  "kind":"cron|wakeup|watch|",
  "confidence":0.0,
  "reason":"...",
  "need_confirmation":false,
  "missing_info":[{"field":"...","question":"...","example":"...","required":true}],
  "selector":{"query":"...","kind":"cron|wakeup|watch|","ids":["..."],"names":["..."],"cities":["..."],"delivery_channel":"...","delivery_target":"..."},
  "spec":{"kind":"cron|wakeup|watch|","name":"...","schedule":"...","timezone":"...","session_key":"...","model":"...","prompt":"...","message":"...","delivery":{"channel":"...","target":"..."},"source_kind":"http|file|feed|browser_snapshot|mailbox|calendar|webhook|structured_app_inbox","source_url":"...","source_path":"...","interval":"...","fire_on_start":false,"enabled":true,"automation_id":"..."},
  "query":{"metric":"summary|list|notification_today|notification_total|notification_failures","limit":10}
}

Rules:
- Work from semantics, not keywords. The user may write in any language.
- This planner is for recurring automation and notification CRUD, not for ordinary one-off tasks.
- Use action=none when the message is just casual chat, a normal one-off task, or unrelated to automation management.
- Prefer create for new recurring briefings/reminders/watches.
- Prefer disable for stop/pause/cancel notification intent when the user wants the automation to remain recoverable.
- Prefer delete only when the user clearly wants the automation removed permanently.
- Prefer query for inventory/status/count questions like "what automations exist" or "how many notifications were sent today".
- If the request refers to an existing automation but the target is ambiguous, populate selector.query and selector.names, and use missing_info or need_confirmation instead of inventing an ID.
- Never invent concrete IDs unless they appear in the supplied inventory.
- For create:
  - kind=cron for scheduled recurring agent briefs.
  - kind=wakeup for reminder-style scheduled wake messages.
  - kind=watch only when a concrete watched source is present or clearly implied.
- If the request lacks execution-critical fields such as schedule, delivery target, or watch source, use missing_info with short operator-friendly questions.
- Keep prompt/message concise and execution-ready when the user already gave enough detail.
- Return JSON only.`
