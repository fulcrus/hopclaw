package runtime

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/fulcrus/hopclaw/agent"
	automationintent "github.com/fulcrus/hopclaw/automation/intent"
	automationsetup "github.com/fulcrus/hopclaw/automation/setup"
	"github.com/fulcrus/hopclaw/internal/meta"
)

const (
	automationIntentTimeout             = 5 * time.Second
	automationIntentConfidenceThreshold = 0.55
	automationIntentInventoryLimit      = 12
	automationIntentSearchLimit         = 10
)

type automationSearchPayload struct {
	Items []automationInventoryItem `json:"items"`
	Count int                       `json:"count"`
}

type automationInventoryItem struct {
	ID                       string `json:"id"`
	Kind                     string `json:"kind"`
	Name                     string `json:"name"`
	Enabled                  bool   `json:"enabled"`
	Schedule                 string `json:"schedule"`
	Message                  string `json:"message"`
	PromptPreview            string `json:"prompt_preview"`
	SessionKey               string `json:"session_key"`
	Model                    string `json:"model"`
	Channel                  string `json:"channel"`
	DeliveryChannel          string `json:"delivery_channel"`
	DeliveryAccountID        string `json:"delivery_account_id"`
	DeliveryTarget           string `json:"delivery_target"`
	SourceKind               string `json:"source_kind"`
	SourceLabel              string `json:"source_label"`
	LastStatus               string `json:"last_status"`
	NotificationTotalCount   int    `json:"notification_total_count"`
	NotificationFailureCount int    `json:"notification_failure_count"`
	NotificationTodayCount   int    `json:"notification_today_count"`
	NotificationTodayDate    string `json:"notification_today_date"`
}

type automationStatsPayload struct {
	TotalCount               int    `json:"total_count"`
	EnabledCount             int    `json:"enabled_count"`
	DisabledCount            int    `json:"disabled_count"`
	CronCount                int    `json:"cron_count"`
	WakeupCount              int    `json:"wakeup_count"`
	WatchCount               int    `json:"watch_count"`
	NotificationTotalCount   int    `json:"notification_total_count"`
	NotificationFailureCount int    `json:"notification_failure_count"`
	NotificationTodayCount   int    `json:"notification_today_count"`
	NotificationTodayDate    string `json:"notification_today_date"`
}

type destinationInventoryPayload struct {
	Items []automationsetup.InventoryItem `json:"items"`
	Count int                             `json:"count"`
}

type destinationProbePayload struct {
	Probe automationsetup.ProbeResult     `json:"probe"`
	Spec  automationsetup.DestinationSpec `json:"spec"`
}

func (s *Service) tryAutomationIntent(
	ctx context.Context,
	req InteractionRequest,
	content string,
	snap InteractionContextSnapshot,
	sessionKey, model string,
	metadata map[string]any,
) (*InteractionResult, bool) {
	if s == nil || s.automationClassifier == nil || snap.HasActiveRun || snap.WaitingApproval || snap.WaitingInput {
		return nil, false
	}
	content = strings.TrimSpace(content)
	if content == "" {
		return nil, false
	}

	classifyReq := AutomationIntentClassifyRequest{
		SessionKey:   sessionKey,
		Message:      content,
		Model:        model,
		SessionState: snap.SessionState,
	}
	if snap.SessionID != "" {
		classifyReq.RecentMessages = s.loadRecentMessages(ctx, snap.SessionID, interactRecentMessageLimit)
	}
	if inventory, err := s.loadAutomationIntentInventory(ctx, sessionKey, model, req); err == nil && len(inventory) > 0 {
		classifyReq.Inventory = inventory
	}

	classifyCtx, cancel := context.WithTimeout(ctx, automationIntentTimeout)
	defer cancel()

	plan, err := s.automationClassifier.Analyze(classifyCtx, classifyReq)
	if err != nil {
		log.Warn("automation intent classifier failed", "error", err, "session_key", sessionKey)
		return nil, false
	}
	return s.executeAutomationIntentPlan(ctx, req, snap, sessionKey, model, metadata, plan)
}

func (s *Service) executeAutomationIntentPlan(
	ctx context.Context,
	req InteractionRequest,
	snap InteractionContextSnapshot,
	sessionKey, model string,
	metadata map[string]any,
	plan automationintent.Plan,
) (*InteractionResult, bool) {
	plan = plan.Normalized()
	if !automationPlanNeedsHandling(plan) || plan.Confidence < automationIntentConfidenceThreshold {
		return nil, false
	}

	result := &InteractionResult{
		Context: snap,
		Decision: InteractionDecision{
			SpeechAct:   SpeechActCommand,
			TargetScope: TargetScopeSession,
			ReplyAct:    ReplyActChatReply,
			Confidence:  plan.Confidence,
			Reason:      "automation_intent_" + string(plan.Action),
		},
	}
	if msg := automationMissingInfoMessage(plan); msg != "" {
		result.Decision.ReplyAct = ReplyActClarificationPrompt
		result.ReplyMessage = msg
		return result, true
	}
	if plan.NeedConfirmation {
		result.Decision.ReplyAct = ReplyActClarificationPrompt
		result.ReplyMessage = automationConfirmationMessage(plan)
		return result, true
	}

	switch plan.Action {
	case automationintent.ActionCreate:
		result.Decision.ReplyAct = ReplyActActionAck
		msg, contract, execErr := s.executeAutomationCreate(ctx, req, sessionKey, model, metadata, plan)
		if execErr != nil {
			result.Decision.ReplyAct = ReplyActTaskFailure
			result.Error = execErr.Error()
			result.ReplyMessage = automationFailureMessage(execErr)
		} else {
			if contract != nil {
				result.Decision.ReplyAct = ReplyActClarificationPrompt
				result.SetupContract = contract
			}
			result.ReplyMessage = msg
		}
	case automationintent.ActionDisable, automationintent.ActionDelete, automationintent.ActionUpdate:
		result.Decision.ReplyAct = ReplyActActionAck
		msg, execErr := s.executeAutomationMutation(ctx, req, sessionKey, model, metadata, plan)
		if execErr != nil {
			result.Decision.ReplyAct = ReplyActTaskFailure
			result.Error = execErr.Error()
			result.ReplyMessage = automationFailureMessage(execErr)
		} else {
			result.ReplyMessage = msg
		}
	case automationintent.ActionQuery:
		msg, execErr := s.executeAutomationQuery(ctx, req, sessionKey, model, plan)
		if execErr != nil {
			result.Decision.ReplyAct = ReplyActTaskFailure
			result.Error = execErr.Error()
			result.ReplyMessage = automationFailureMessage(execErr)
		} else {
			result.ReplyMessage = msg
		}
	default:
		return nil, false
	}
	return result, true
}

func (s *Service) executeAutomationQuery(ctx context.Context, req InteractionRequest, sessionKey, model string, plan automationintent.Plan) (string, error) {
	switch plan.Query.Metric {
	case automationintent.QueryMetricList:
		items, err := s.findAutomationCandidates(ctx, req, sessionKey, model, plan, max(plan.Query.Limit, 5))
		if err != nil {
			return "", err
		}
		if len(items) == 0 {
			return "当前没有匹配的自动化。", nil
		}
		lines := make([]string, 0, len(items)+1)
		lines = append(lines, fmt.Sprintf("当前有 %d 个匹配的自动化：", len(items)))
		for _, item := range items {
			status := "disabled"
			if item.Enabled {
				status = "enabled"
			}
			summary := strings.TrimSpace(item.Schedule)
			if summary == "" && strings.TrimSpace(item.SourceLabel) != "" {
				summary = item.SourceLabel
			}
			if summary == "" {
				summary = "-"
			}
			lines = append(lines, fmt.Sprintf("- [%s] %s (%s) · %s", item.Kind, item.Name, status, summary))
		}
		return strings.Join(lines, "\n"), nil
	default:
		var stats automationStatsPayload
		if err := s.executeAutomationTool(ctx, sessionKey, model, "automation.stats", automationScopeInput(req, map[string]any{
			"query": plan.Selector.Query,
			"kind":  firstNonEmpty(plan.Selector.Kind, plan.Kind),
			"limit": automationIntentSearchLimit,
		}), &stats); err != nil {
			return "", err
		}
		return fmt.Sprintf(
			"今天已发送 %d 条通知，失败 %d 次。当前共有 %d 个自动化，其中启用 %d 个。分类：cron %d，wakeup %d，watch %d。",
			stats.NotificationTodayCount,
			stats.NotificationFailureCount,
			stats.TotalCount,
			stats.EnabledCount,
			stats.CronCount,
			stats.WakeupCount,
			stats.WatchCount,
		), nil
	}
}

func (s *Service) executeAutomationCreate(
	ctx context.Context,
	req InteractionRequest,
	sessionKey, model string,
	metadata map[string]any,
	plan automationintent.Plan,
) (string, *automationsetup.Contract, error) {
	spec := plan.Spec.Normalized()
	kind := firstNonEmpty(spec.Kind, plan.Kind)
	switch kind {
	case "watch":
		if spec.SourceURL == "" && spec.SourcePath == "" {
			return "要创建 watch 自动化，还需要一个具体的数据源 URL 或文件路径。", nil, nil
		}
		delivery, contract, err := s.resolveAutomationDelivery(ctx, req, sessionKey, model, metadata, kind, spec, plan)
		if err != nil || contract != nil {
			return automationSetupMessage(contract), contract, err
		}
		name := firstNonEmpty(spec.Name, "Watch automation")
		interval := firstNonEmpty(spec.Interval, "5m")
		prompt := firstNonEmpty(spec.Prompt, strings.TrimSpace(req.Content))
		input := automationScopeInput(req, map[string]any{
			"name":                name,
			"enabled":             true,
			"interval":            interval,
			"source_kind":         firstNonEmpty(spec.SourceKind, sourceKindFromSpec(spec)),
			"source_url":          spec.SourceURL,
			"source_path":         spec.SourcePath,
			"prompt":              prompt,
			"session_key":         firstNonEmpty(spec.SessionKey, automationSessionKey(sessionKey, kind, name)),
			"model":               firstNonEmpty(spec.Model, model),
			"delivery_channel":    delivery.Channel,
			"delivery_account_id": delivery.AccountID,
			"delivery_target":     delivery.Target,
			"fire_on_start":       trueValue(spec.FireOnStart),
			"automation_id":       firstNonEmpty(spec.AutomationID, req.AutomationID),
		})
		var resp struct {
			ID string `json:"id"`
		}
		if err := s.executeAutomationTool(ctx, sessionKey, model, "watch.add", input, &resp); err != nil {
			return "", nil, err
		}
		return fmt.Sprintf("已创建 watch 自动化 `%s`（%s），轮询间隔 %s，通知发送到 %s。", name, resp.ID, interval, automationDeliverySummary(delivery)), nil, nil
	case "wakeup":
		if spec.Schedule == "" {
			return "要创建 wakeup 自动化，还需要明确时间计划。", nil, nil
		}
		delivery := automationDeliveryFromSpecOrMetadata(spec.Delivery, metadata)
		name := firstNonEmpty(spec.Name, "Wakeup automation")
		message := firstNonEmpty(spec.Message, firstNonEmpty(spec.Prompt, strings.TrimSpace(req.Content)))
		input := automationScopeInput(req, map[string]any{
			"name":          name,
			"schedule":      spec.Schedule,
			"channel":       firstNonEmpty(channelFromMetadata(metadata), deliveryChannelOrEmpty(delivery)),
			"session_key":   firstNonEmpty(spec.SessionKey, sessionKey),
			"message":       message,
			"model":         firstNonEmpty(spec.Model, model),
			"timezone":      spec.Timezone,
			"enabled":       true,
			"automation_id": firstNonEmpty(spec.AutomationID, req.AutomationID),
		})
		var resp struct {
			ID string `json:"id"`
		}
		if err := s.executeAutomationTool(ctx, sessionKey, model, "wakeup.add", input, &resp); err != nil {
			return "", nil, err
		}
		return fmt.Sprintf("已创建 wakeup 自动化 `%s`（%s），计划 %s。", name, resp.ID, spec.Schedule), nil, nil
	default:
		if spec.Schedule == "" {
			return "要创建这个自动化，还需要明确计划时间。比如 `0 8 * * *` 或 `every 1h`。", nil, nil
		}
		delivery, contract, err := s.resolveAutomationDelivery(ctx, req, sessionKey, model, metadata, "cron", spec, plan)
		if err != nil || contract != nil {
			return automationSetupMessage(contract), contract, err
		}
		name := firstNonEmpty(spec.Name, "Scheduled automation")
		content := firstNonEmpty(spec.Prompt, firstNonEmpty(spec.Message, strings.TrimSpace(req.Content)))
		scheduleKind, scheduleExpression := compileCronSchedule(spec.Schedule)
		input := automationScopeInput(req, map[string]any{
			"name":                name,
			"schedule_kind":       scheduleKind,
			"schedule_expression": scheduleExpression,
			"content":             content,
			"session_key":         firstNonEmpty(spec.SessionKey, automationSessionKey(sessionKey, "cron", name)),
			"channel":             delivery.Channel,
			"account_id":          delivery.AccountID,
			"target":              delivery.Target,
			"timezone":            spec.Timezone,
			"enabled":             true,
			"model":               firstNonEmpty(spec.Model, model),
			"automation_id":       firstNonEmpty(spec.AutomationID, req.AutomationID),
		})
		var resp struct {
			ID string `json:"id"`
		}
		if err := s.executeAutomationTool(ctx, sessionKey, model, "cron.add", input, &resp); err != nil {
			return "", nil, err
		}
		return fmt.Sprintf("已创建 cron 自动化 `%s`（%s），计划 %s，通知发送到 %s。", name, resp.ID, spec.Schedule, automationDeliverySummary(delivery)), nil, nil
	}
}

func (s *Service) executeAutomationMutation(
	ctx context.Context,
	req InteractionRequest,
	sessionKey, model string,
	metadata map[string]any,
	plan automationintent.Plan,
) (string, error) {
	items, err := s.findAutomationCandidates(ctx, req, sessionKey, model, plan, automationIntentSearchLimit)
	if err != nil {
		return "", err
	}
	if len(items) == 0 {
		return "没有找到匹配的自动化。请补充名字、计划时间、城市或通知目标。", nil
	}
	if len(items) > 1 {
		return automationAmbiguousMessage(plan, items), nil
	}
	item := items[0]
	switch plan.Action {
	case automationintent.ActionDelete:
		toolName := item.Kind + ".remove"
		if err := s.executeAutomationTool(ctx, sessionKey, model, toolName, map[string]any{"id": item.ID}, nil); err != nil {
			return "", err
		}
		return fmt.Sprintf("已删除 `%s`（%s）。", item.Name, item.Kind), nil
	case automationintent.ActionUpdate:
		return s.executeAutomationUpdate(ctx, req, sessionKey, model, metadata, plan, item)
	default:
		toolName := item.Kind + ".update"
		input := map[string]any{"id": item.ID, "enabled": false}
		if err := s.executeAutomationTool(ctx, sessionKey, model, toolName, input, nil); err != nil {
			return "", err
		}
		return fmt.Sprintf("已停用 `%s`（%s）。", item.Name, item.Kind), nil
	}
}

func (s *Service) executeAutomationUpdate(
	ctx context.Context,
	req InteractionRequest,
	sessionKey, model string,
	metadata map[string]any,
	plan automationintent.Plan,
	item automationInventoryItem,
) (string, error) {
	spec := plan.Spec.Normalized()
	input := map[string]any{"id": item.ID}
	changed := 0
	if spec.Schedule != "" {
		switch item.Kind {
		case "watch":
			input["interval"] = spec.Schedule
		case "wakeup":
			input["schedule"] = spec.Schedule
		default:
			kind, expr := compileCronSchedule(spec.Schedule)
			input["schedule_kind"] = kind
			input["schedule_expression"] = expr
			input["timezone"] = spec.Timezone
		}
		changed++
	}
	if spec.Name != "" {
		input["name"] = spec.Name
		changed++
	}
	if spec.Prompt != "" {
		switch item.Kind {
		case "watch":
			input["prompt"] = spec.Prompt
		case "wakeup":
			input["message"] = spec.Prompt
		default:
			input["content"] = spec.Prompt
		}
		changed++
	}
	if spec.Message != "" && item.Kind == "wakeup" {
		input["message"] = spec.Message
		changed++
	}
	if spec.Enabled {
		input["enabled"] = true
		changed++
	}
	if delivery := automationDeliveryFromSpecOrMetadata(spec.Delivery, metadata); delivery != nil {
		switch item.Kind {
		case "watch":
			input["delivery_channel"] = delivery.Channel
			input["delivery_account_id"] = delivery.AccountID
			input["delivery_target"] = delivery.Target
			changed++
		case "cron":
			input["channel"] = delivery.Channel
			input["account_id"] = delivery.AccountID
			input["target"] = delivery.Target
			changed++
		}
	}
	if changed == 0 {
		return "这条更新指令还不够具体。请直接说明要改什么，比如时间、通知目标或提示词。", nil
	}
	if err := s.executeAutomationTool(ctx, sessionKey, model, item.Kind+".update", input, nil); err != nil {
		return "", err
	}
	return fmt.Sprintf("已更新 `%s`（%s）。", item.Name, item.Kind), nil
}

func (s *Service) findAutomationCandidates(
	ctx context.Context,
	req InteractionRequest,
	sessionKey, model string,
	plan automationintent.Plan,
	limit int,
) ([]automationInventoryItem, error) {
	searchQuery := buildAutomationSearchQuery(plan)
	searchInput := automationScopeInput(req, map[string]any{
		"query": searchQuery,
		"kind":  firstNonEmpty(plan.Selector.Kind, plan.Kind),
		"limit": max(limit, 1),
	})
	var payload automationSearchPayload
	if err := s.executeAutomationTool(ctx, sessionKey, model, "automation.search", searchInput, &payload); err != nil {
		return nil, err
	}
	return payload.Items, nil
}

func (s *Service) loadAutomationIntentInventory(ctx context.Context, sessionKey, model string, req InteractionRequest) ([]automationintent.InventoryItem, error) {
	var payload automationSearchPayload
	if err := s.executeAutomationTool(ctx, sessionKey, model, "automation.search", automationScopeInput(req, map[string]any{
		"limit": automationIntentInventoryLimit,
	}), &payload); err != nil {
		return nil, err
	}
	if len(payload.Items) == 0 {
		return nil, nil
	}
	out := make([]automationintent.InventoryItem, 0, len(payload.Items))
	for _, item := range payload.Items {
		entry := automationintent.InventoryItem{
			ID:            item.ID,
			Kind:          item.Kind,
			Name:          item.Name,
			Enabled:       item.Enabled,
			Schedule:      item.Schedule,
			Message:       item.Message,
			PromptPreview: item.PromptPreview,
			SourceLabel:   item.SourceLabel,
		}
		if item.DeliveryChannel != "" || item.DeliveryTarget != "" {
			entry.Delivery = &automationintent.DeliveryTarget{
				Kind:      "channel",
				Provider:  strings.ToLower(item.DeliveryChannel),
				Channel:   item.DeliveryChannel,
				AccountID: item.DeliveryAccountID,
				Target:    item.DeliveryTarget,
			}
		}
		out = append(out, entry)
	}
	return out, nil
}

func (s *Service) executeAutomationTool(ctx context.Context, sessionKey, model, toolName string, input map[string]any, out any) error {
	if s == nil || s.agent == nil {
		return fmt.Errorf("runtime agent is not configured")
	}
	executor := s.agent.ToolExecutor()
	if executor == nil {
		return agent.ErrToolExecutorNil
	}
	session, err := s.sessions.GetOrCreate(ctx, firstNonEmpty(sessionKey, "automation:intent"), firstNonEmpty(model, "automation-intent"))
	if err != nil {
		return err
	}
	call := agent.ToolCall{
		ID:    fmt.Sprintf("call-%d", time.Now().UTC().UnixNano()),
		Name:  toolName,
		Input: input,
	}
	run := &agent.Run{
		ID:        fmt.Sprintf("interact-automation-%d", time.Now().UTC().UnixNano()),
		SessionID: session.ID,
		Status:    agent.RunRunning,
	}
	results, err := executor.ExecuteBatch(ctx, run, session, []agent.ToolCall{call})
	if err != nil {
		return err
	}
	if len(results) == 0 {
		return fmt.Errorf("%s returned no result", toolName)
	}
	if out == nil {
		return nil
	}
	return json.Unmarshal([]byte(results[0].Content), out)
}

func automationScopeInput(req InteractionRequest, input map[string]any) map[string]any {
	if input == nil {
		input = make(map[string]any, 4)
	}
	if strings.TrimSpace(req.AutomationID) != "" {
		input["automation_id"] = strings.TrimSpace(req.AutomationID)
	}
	return input
}

func (s *Service) resolveAutomationDelivery(
	ctx context.Context,
	req InteractionRequest,
	sessionKey, model string,
	metadata map[string]any,
	kind string,
	spec automationintent.Spec,
	plan automationintent.Plan,
) (*automationintent.DeliveryTarget, *automationsetup.Contract, error) {
	candidate := automationDeliveryFromSpecOrMetadata(spec.Delivery, metadata)
	if candidate != nil {
		probed, probe, err := s.probeAutomationDelivery(ctx, sessionKey, model, *candidate)
		if err != nil {
			return nil, nil, err
		}
		if probe.Status == automationsetup.ProbeStatusReady {
			return automationDeliveryFromSetupSpec(probed), nil, nil
		}
		inventory, invErr := s.loadAutomationDestinationInventory(ctx, req, sessionKey, model, metadata)
		if invErr != nil {
			return nil, nil, invErr
		}
		contract := buildAutomationDeliveryContract(kind, spec, plan, req.Content, probed, inventory, []automationsetup.ProbeResult{probe})
		return nil, &contract, nil
	}

	inventory, err := s.loadAutomationDestinationInventory(ctx, req, sessionKey, model, metadata)
	if err != nil {
		return nil, nil, err
	}
	if ready := firstReadyAutomationDestination(inventory); ready != nil {
		return automationDeliveryFromSetupSpec(ready.Spec), nil, nil
	}
	contract := buildAutomationDeliveryContract(kind, spec, plan, req.Content, automationsetup.DestinationSpec{}, inventory, nil)
	return nil, &contract, nil
}

func (s *Service) loadAutomationDestinationInventory(
	ctx context.Context,
	req InteractionRequest,
	sessionKey, model string,
	metadata map[string]any,
) ([]automationsetup.InventoryItem, error) {
	input := automationScopeInput(req, map[string]any{
		"kind":            "channel",
		"current_channel": channelFromMetadata(metadata),
		"current_target":  deliveryTargetFromMetadata(channelFromMetadata(metadata), metadata),
		"current_account": automationMetadataString(metadata, "account_id"),
		"include_probe":   true,
	})
	var payload destinationInventoryPayload
	if err := s.executeAutomationTool(ctx, sessionKey, model, "destination.inventory", input, &payload); err != nil {
		return nil, err
	}
	return payload.Items, nil
}

func (s *Service) probeAutomationDelivery(
	ctx context.Context,
	sessionKey, model string,
	delivery automationintent.DeliveryTarget,
) (automationsetup.DestinationSpec, automationsetup.ProbeResult, error) {
	spec := automationsetup.DestinationSpec{
		Kind:       firstNonEmpty(delivery.Kind, "channel"),
		Provider:   firstNonEmpty(delivery.Provider, delivery.Channel),
		Channel:    firstNonEmpty(delivery.Channel, delivery.Provider),
		AccountID:  delivery.AccountID,
		TargetType: delivery.TargetType,
		Target:     delivery.Target,
		Label:      delivery.Label,
	}.Normalized()
	var payload destinationProbePayload
	if err := s.executeAutomationTool(ctx, sessionKey, model, "destination.probe", map[string]any{
		"kind":        spec.Kind,
		"provider":    spec.Provider,
		"channel":     spec.Channel,
		"account_id":  spec.AccountID,
		"target_type": spec.TargetType,
		"target":      spec.Target,
		"label":       spec.Label,
	}, &payload); err != nil {
		return automationsetup.DestinationSpec{}, automationsetup.ProbeResult{}, err
	}
	return payload.Spec.Normalized(), payload.Probe.Normalized(), nil
}

func firstReadyAutomationDestination(items []automationsetup.InventoryItem) *automationsetup.InventoryItem {
	for i := range items {
		if items[i].Probe != nil && items[i].Probe.Status == automationsetup.ProbeStatusReady {
			return &items[i]
		}
	}
	return nil
}

func buildAutomationDeliveryContract(
	kind string,
	spec automationintent.Spec,
	plan automationintent.Plan,
	content string,
	selected automationsetup.DestinationSpec,
	inventory []automationsetup.InventoryItem,
	probes []automationsetup.ProbeResult,
) automationsetup.Contract {
	workflow := automationsetup.WorkflowSpec{
		Kind:    firstNonEmpty(kind, spec.Kind, plan.Kind),
		Name:    firstNonEmpty(spec.Name, automationDisplayKind(firstNonEmpty(kind, spec.Kind, "automation"))),
		Summary: firstNonEmpty(strings.TrimSpace(spec.Prompt), strings.TrimSpace(spec.Message), strings.TrimSpace(content)),
	}
	selected = selected.Normalized()
	if selected.Kind != "" {
		workflow.Destination = &selected
	}

	options := make([]automationsetup.SetupOption, 0, len(inventory)+2)
	seen := make(map[string]struct{}, len(inventory))
	contractProbes := make([]automationsetup.ProbeResult, 0, len(probes)+len(inventory))
	for _, probe := range probes {
		contractProbes = append(contractProbes, probe.Normalized())
	}
	for _, item := range inventory {
		item = item.Normalized()
		specValue := item.Spec
		key := strings.Join([]string{specValue.Kind, specValue.Provider, specValue.AccountID, specValue.Target}, ":")
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		description := firstNonEmpty(item.Summary, probeMessage(item.Probe))
		options = append(options, automationsetup.SetupOption{
			ID:          item.ID,
			Mode:        "pick_existing",
			Label:       firstNonEmpty(item.Label, specValue.DisplayName()),
			Description: description,
			Value:       &specValue,
		})
		if item.Probe != nil {
			contractProbes = append(contractProbes, item.Probe.Normalized())
		}
	}
	options = append(options, automationsetup.SetupOption{
		ID:          "manual:channel_target",
		Mode:        "manual_input",
		Label:       "Provide channel target",
		Description: "Reply with a concrete channel name and target ID, for example `发到 slack 的 channel C12345`.",
	})
	options = append(options, automationsetup.SetupOption{
		ID:          "setup:channel_config",
		Mode:        "create_new",
		Label:       "Configure a channel",
		Description: "If no channel is ready, add or fix one in YAML and retry.",
	})

	status := automationsetup.ContractStatusNeedsInput
	slotStatus := automationsetup.SlotStatusMissing
	summary := "创建这条自动化前，还需要确认通知投递目标。"
	if len(inventory) == 0 {
		status = automationsetup.ContractStatusBlocked
		slotStatus = automationsetup.SlotStatusBlocked
		summary = "当前没有可用的通知通道。先配置至少一个可发送消息的 channel，再创建自动化。"
	}
	if selected.Kind != "" {
		slotStatus = automationsetup.SlotStatusMissing
	}
	if len(probes) > 0 {
		if msg := probeMessage(&probes[0]); msg != "" {
			summary = msg
		}
		if probes[0].Status == automationsetup.ProbeStatusNotConfigured || probes[0].Status == automationsetup.ProbeStatusNotConnected {
			status = automationsetup.ContractStatusBlocked
			slotStatus = automationsetup.SlotStatusBlocked
		}
	}

	slot := automationsetup.SetupSlot{
		ID:            "delivery",
		Kind:          "destination",
		Label:         "Notification destination",
		Prompt:        "告诉我要把自动化结果发到哪里。你可以直接说当前会话，或者指定 channel + target。",
		Required:      true,
		Status:        slotStatus,
		PreferredMode: "pick_existing",
		Value:         destinationValueOrNil(selected),
		Options:       options,
		Example:       automationDeliveryExample(inventory),
	}
	return automationsetup.Contract{
		Status:   status,
		Workflow: &workflow,
		Summary:  summary,
		Slots:    []automationsetup.SetupSlot{slot},
		Probes:   dedupeAutomationProbes(contractProbes),
	}
}

func automationSetupMessage(contract *automationsetup.Contract) string {
	if contract == nil {
		return ""
	}
	lines := make([]string, 0, 8)
	if summary := strings.TrimSpace(contract.Summary); summary != "" {
		lines = append(lines, summary)
	}
	for _, slot := range contract.Slots {
		if prompt := strings.TrimSpace(slot.Prompt); prompt != "" {
			lines = append(lines, prompt)
		}
		for i, option := range slot.Options {
			if i >= 4 {
				break
			}
			line := "- " + strings.TrimSpace(option.Label)
			if desc := strings.TrimSpace(option.Description); desc != "" {
				line += "： " + desc
			}
			lines = append(lines, line)
		}
		if example := strings.TrimSpace(slot.Example); example != "" {
			lines = append(lines, "例如："+example)
		}
	}
	if len(lines) == 0 {
		return "还需要补充自动化的投递目标。"
	}
	return strings.Join(lines, "\n")
}

func automationDeliveryExample(items []automationsetup.InventoryItem) string {
	for _, item := range items {
		if item.Probe != nil && item.Probe.Status == automationsetup.ProbeStatusReady {
			switch strings.TrimSpace(item.Spec.Provider) {
			case "feishu", "telegram":
				return "发到当前会话"
			case "slack", "discord":
				return "发到当前频道"
			}
		}
	}
	for _, item := range items {
		targetType := firstNonEmpty(item.Spec.TargetType, "target_id")
		switch strings.TrimSpace(item.Spec.Provider) {
		case "feishu":
			return fmt.Sprintf("发到 feishu 的 %s oc_xxx", targetType)
		case "slack":
			return fmt.Sprintf("发到 slack 的 %s C12345", targetType)
		}
	}
	return "发到 slack 的 channel_id C12345"
}

func dedupeAutomationProbes(items []automationsetup.ProbeResult) []automationsetup.ProbeResult {
	if len(items) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(items))
	out := make([]automationsetup.ProbeResult, 0, len(items))
	for _, item := range items {
		item = item.Normalized()
		key := strings.Join([]string{item.Kind, item.Provider, string(item.Status), item.Code, item.Message}, ":")
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, item)
	}
	return out
}

func probeMessage(probe *automationsetup.ProbeResult) string {
	if probe == nil {
		return ""
	}
	return strings.TrimSpace(probe.Message)
}

func destinationValueOrNil(spec automationsetup.DestinationSpec) *automationsetup.DestinationSpec {
	spec = spec.Normalized()
	if spec.Kind == "" && spec.Provider == "" && spec.Channel == "" && spec.Target == "" && spec.AccountID == "" {
		return nil
	}
	return &spec
}

func automationDeliveryFromSetupSpec(spec automationsetup.DestinationSpec) *automationintent.DeliveryTarget {
	spec = spec.Normalized()
	if spec.Kind == "" && spec.Provider == "" && spec.Channel == "" && spec.Target == "" && spec.AccountID == "" {
		return nil
	}
	return &automationintent.DeliveryTarget{
		Kind:       spec.Kind,
		Provider:   spec.Provider,
		Channel:    spec.Channel,
		AccountID:  spec.AccountID,
		TargetType: spec.TargetType,
		Target:     spec.Target,
		Label:      spec.Label,
	}
}

func automationDeliverySummary(target *automationintent.DeliveryTarget) string {
	if target == nil {
		return "-"
	}
	if strings.TrimSpace(target.AccountID) != "" {
		return fmt.Sprintf("%s/%s:%s", target.Channel, target.AccountID, target.Target)
	}
	return fmt.Sprintf("%s:%s", target.Channel, target.Target)
}

func automationDisplayKind(kind string) string {
	kind = strings.TrimSpace(kind)
	if kind == "" {
		return ""
	}
	return strings.ToUpper(kind[:1]) + kind[1:]
}

func buildAutomationSearchQuery(plan automationintent.Plan) string {
	parts := make([]string, 0, 4)
	if query := strings.TrimSpace(plan.Selector.Query); query != "" {
		parts = append(parts, query)
	}
	if len(plan.Selector.Names) > 0 {
		parts = append(parts, strings.Join(plan.Selector.Names, " "))
	}
	if len(plan.Selector.Cities) > 0 {
		parts = append(parts, strings.Join(plan.Selector.Cities, " "))
	}
	if kind := firstNonEmpty(plan.Kind, plan.Spec.Kind); kind != "" {
		parts = append(parts, kind)
	}
	return strings.TrimSpace(strings.Join(parts, " "))
}

func automationMissingInfoMessage(plan automationintent.Plan) string {
	if len(plan.MissingInfo) == 0 {
		return ""
	}
	lines := []string{"还缺这些关键信息，补充后我才能直接创建或修改自动化："}
	for _, item := range plan.MissingInfo {
		question := strings.TrimSpace(item.Question)
		if question == "" {
			question = strings.TrimSpace(item.Field)
		}
		if question == "" {
			continue
		}
		line := "- " + question
		if example := strings.TrimSpace(item.Example); example != "" {
			line += "，例如：" + example
		}
		lines = append(lines, line)
	}
	if len(lines) == 1 {
		return ""
	}
	return strings.Join(lines, "\n")
}

func automationConfirmationMessage(plan automationintent.Plan) string {
	target := buildAutomationSearchQuery(plan)
	if target == "" {
		target = "这条自动化"
	}
	switch plan.Action {
	case automationintent.ActionDelete:
		return "这看起来是永久删除操作。为避免误删，请直接明确说出要删除的自动化名字或 ID。"
	default:
		return fmt.Sprintf("我理解你是在操作自动化 `%s`，但这条指令还有歧义。请直接说明是停用、删除，还是修改。", target)
	}
}

func automationAmbiguousMessage(plan automationintent.Plan, items []automationInventoryItem) string {
	lines := []string{"找到多个匹配的自动化，请直接说更具体一点："}
	for i, item := range items {
		if i >= 5 {
			break
		}
		summary := firstNonEmpty(item.Schedule, item.SourceLabel, item.DeliveryTarget, "-")
		lines = append(lines, fmt.Sprintf("- %s (%s) · %s", item.Name, item.Kind, summary))
	}
	if plan.Action == automationintent.ActionDelete {
		lines = append(lines, "如果你要永久删除，请直接说：删除 <名字>。")
	} else {
		lines = append(lines, "如果你只是想停掉通知，请直接说：停用 <名字>。")
	}
	return strings.Join(lines, "\n")
}

func automationFailureMessage(err error) string {
	if err == nil {
		return ""
	}
	return "自动化操作失败：" + strings.TrimSpace(err.Error())
}

func automationSessionKey(base, kind, name string) string {
	base = strings.TrimSpace(base)
	if base == "" {
		base = "automation"
	}
	slug := strings.ToLower(strings.TrimSpace(name))
	slug = strings.NewReplacer(" ", "-", "/", "-", ":", "-", ".", "-").Replace(slug)
	slug = strings.Trim(slug, "-")
	if slug == "" {
		slug = kind
	}
	return base + ":automation:" + slug
}

func compileCronSchedule(raw string) (string, string) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "cron", raw
	}
	if _, err := time.Parse(time.RFC3339, raw); err == nil {
		return "at", raw
	}
	if strings.HasPrefix(strings.ToLower(raw), "every ") {
		return "every", strings.TrimSpace(raw[len("every "):])
	}
	return "cron", raw
}

func sourceKindFromSpec(spec automationintent.Spec) string {
	switch {
	case spec.SourceKind != "":
		return spec.SourceKind
	case spec.SourcePath != "":
		return "file"
	default:
		return "http"
	}
}

func automationDeliveryFromSpecOrMetadata(spec *automationintent.DeliveryTarget, metadata map[string]any) *automationintent.DeliveryTarget {
	if spec != nil && (strings.TrimSpace(spec.Channel) != "" || strings.TrimSpace(spec.Target) != "" || strings.TrimSpace(spec.AccountID) != "") {
		provider := firstNonEmpty(spec.Provider, spec.Channel)
		return &automationintent.DeliveryTarget{
			Kind:       firstNonEmpty(spec.Kind, "channel"),
			Provider:   strings.ToLower(provider),
			Channel:    strings.TrimSpace(spec.Channel),
			AccountID:  strings.TrimSpace(spec.AccountID),
			TargetType: firstNonEmpty(spec.TargetType, metadataTargetType(strings.TrimSpace(spec.Channel))),
			Target:     strings.TrimSpace(spec.Target),
			Label:      strings.TrimSpace(spec.Label),
		}
	}
	channel := channelFromMetadata(metadata)
	target := deliveryTargetFromMetadata(channel, metadata)
	if channel == "" || target == "" {
		return nil
	}
	return &automationintent.DeliveryTarget{
		Kind:       "channel",
		Provider:   strings.ToLower(channel),
		Channel:    channel,
		AccountID:  automationMetadataString(metadata, "account_id"),
		TargetType: metadataTargetType(channel),
		Target:     target,
		Label:      firstNonEmpty(channel, "channel") + ":" + target,
	}
}

func channelFromMetadata(metadata map[string]any) string {
	if len(metadata) == 0 {
		return ""
	}
	return strings.TrimSpace(fmt.Sprint(metadata[meta.KeyChannel]))
}

func deliveryTargetFromMetadata(channel string, metadata map[string]any) string {
	switch strings.TrimSpace(channel) {
	case "feishu", "telegram":
		return automationMetadataString(metadata, meta.KeyChatID, "chat_id")
	case "slack", "discord":
		return automationMetadataString(metadata, meta.KeyChannelID, "channel_id")
	default:
		return automationMetadataString(metadata, meta.KeyChatID, meta.KeyChannelID, "target_id", "chat_id", "channel_id")
	}
}

func deliveryChannelOrEmpty(target *automationintent.DeliveryTarget) string {
	if target == nil {
		return ""
	}
	return strings.TrimSpace(target.Channel)
}

func metadataTargetType(channel string) string {
	switch strings.TrimSpace(strings.ToLower(channel)) {
	case "feishu", "telegram":
		return "chat_id"
	case "slack", "discord":
		return "channel_id"
	default:
		return "target_id"
	}
}

func automationMetadataString(metadata map[string]any, keys ...string) string {
	if len(metadata) == 0 {
		return ""
	}
	for _, key := range keys {
		if value, ok := metadata[key]; ok {
			if trimmed := strings.TrimSpace(fmt.Sprint(value)); trimmed != "" && trimmed != "<nil>" {
				return trimmed
			}
		}
	}
	return ""
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func trueValue(value bool) bool {
	return value
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
