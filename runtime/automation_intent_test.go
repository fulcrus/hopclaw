package runtime

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/fulcrus/hopclaw/agent"
	"github.com/fulcrus/hopclaw/approval"
	"github.com/fulcrus/hopclaw/artifact"
	automationintent "github.com/fulcrus/hopclaw/automation/intent"
	automationsetup "github.com/fulcrus/hopclaw/automation/setup"
	"github.com/fulcrus/hopclaw/contextengine"
	"github.com/fulcrus/hopclaw/eventbus"
	"github.com/fulcrus/hopclaw/internal/meta"
)

type stubAutomationIntentClassifier struct {
	plan automationintent.Plan
	err  error
}

func (s stubAutomationIntentClassifier) Analyze(context.Context, AutomationIntentClassifyRequest) (automationintent.Plan, error) {
	if s.err != nil {
		return automationintent.Plan{}, s.err
	}
	return s.plan, nil
}

type automationIntentStubItem struct {
	ID                       string
	Kind                     string
	Name                     string
	Enabled                  bool
	Schedule                 string
	Message                  string
	PromptPreview            string
	DeliveryChannel          string
	DeliveryAccountID        string
	DeliveryTarget           string
	NotificationTodayCount   int
	NotificationFailureCount int
	NotificationTodayDate    string
}

type automationIntentStubExecutor struct {
	mu     sync.Mutex
	nextID int
	items  []automationIntentStubItem
}

func (e *automationIntentStubExecutor) ExecuteBatch(_ context.Context, _ *agent.Run, _ *agent.Session, calls []agent.ToolCall) ([]contextengine.ToolResult, error) {
	e.mu.Lock()
	defer e.mu.Unlock()

	results := make([]contextengine.ToolResult, 0, len(calls))
	for _, call := range calls {
		payload, err := e.execute(call)
		if err != nil {
			return nil, err
		}
		body, err := json.Marshal(payload)
		if err != nil {
			return nil, err
		}
		results = append(results, contextengine.ToolResult{
			ToolName:   call.Name,
			ToolCallID: call.ID,
			Content:    string(body),
		})
	}
	return results, nil
}

func (e *automationIntentStubExecutor) execute(call agent.ToolCall) (map[string]any, error) {
	switch call.Name {
	case "automation.search":
		return e.search(call.Input), nil
	case "automation.stats":
		return e.stats(call.Input), nil
	case "destination.inventory":
		return e.destinationInventory(call.Input), nil
	case "destination.probe":
		return e.destinationProbe(call.Input), nil
	case "cron.add":
		e.nextID++
		id := fmt.Sprintf("cron-%d", e.nextID)
		item := automationIntentStubItem{
			ID:                id,
			Kind:              "cron",
			Name:              strings.TrimSpace(fmt.Sprint(call.Input["name"])),
			Enabled:           testBoolValue(call.Input["enabled"], true),
			Schedule:          strings.TrimSpace(fmt.Sprint(call.Input["schedule_expression"])),
			PromptPreview:     strings.TrimSpace(fmt.Sprint(call.Input["content"])),
			DeliveryChannel:   strings.TrimSpace(fmt.Sprint(call.Input["channel"])),
			DeliveryAccountID: strings.TrimSpace(fmt.Sprint(call.Input["account_id"])),
			DeliveryTarget:    strings.TrimSpace(fmt.Sprint(call.Input["target"])),
		}
		e.items = append(e.items, item)
		return map[string]any{"id": id, "name": item.Name, "schedule": item.Schedule}, nil
	case "cron.update":
		id := strings.TrimSpace(fmt.Sprint(call.Input["id"]))
		for i := range e.items {
			if e.items[i].ID != id {
				continue
			}
			if call.Input["enabled"] != nil {
				e.items[i].Enabled = testBoolValue(call.Input["enabled"], e.items[i].Enabled)
			}
			if call.Input["name"] != nil {
				e.items[i].Name = strings.TrimSpace(fmt.Sprint(call.Input["name"]))
			}
			return map[string]any{"id": id, "updated": true}, nil
		}
		return nil, fmt.Errorf("cron.update: id %s not found", id)
	case "cron.remove":
		id := strings.TrimSpace(fmt.Sprint(call.Input["id"]))
		for i := range e.items {
			if e.items[i].ID == id {
				e.items = append(e.items[:i], e.items[i+1:]...)
				return map[string]any{"id": id, "removed": true}, nil
			}
		}
		return nil, fmt.Errorf("cron.remove: id %s not found", id)
	default:
		return nil, fmt.Errorf("unsupported tool %q", call.Name)
	}
}

func (e *automationIntentStubExecutor) destinationInventory(input map[string]any) map[string]any {
	channel := strings.TrimSpace(fmt.Sprint(input["current_channel"]))
	target := strings.TrimSpace(fmt.Sprint(input["current_target"]))
	accountID := strings.TrimSpace(fmt.Sprint(input["current_account"]))
	items := make([]map[string]any, 0, 2)
	if channel != "" && target != "" {
		items = append(items, map[string]any{
			"id":       "channel:" + channel + ":current",
			"kind":     "channel",
			"provider": channel,
			"label":    "Current conversation",
			"summary":  "Deliver back to the current conversation.",
			"status":   "ready",
			"spec": map[string]any{
				"kind":        "channel",
				"provider":    channel,
				"channel":     channel,
				"account_id":  accountID,
				"target_type": "chat_id",
				"target":      target,
				"label":       "Current conversation",
			},
			"probe": map[string]any{
				"kind":       "channel",
				"provider":   channel,
				"status":     "ready",
				"reachable":  true,
				"code":       "channel_connected",
				"message":    "Channel connected.",
				"checked_at": time.Now().UTC().Format(time.RFC3339),
			},
		})
	} else {
		items = append(items, map[string]any{
			"id":       "channel:slack",
			"kind":     "channel",
			"provider": "slack",
			"label":    "Slack",
			"summary":  "Slack is configured. Provide channel_id to deliver notifications.",
			"status":   "needs_target",
			"spec": map[string]any{
				"kind":        "channel",
				"provider":    "slack",
				"channel":     "slack",
				"target_type": "channel_id",
				"label":       "Slack",
			},
			"probe": map[string]any{
				"kind":       "channel",
				"provider":   "slack",
				"status":     "needs_target",
				"reachable":  false,
				"code":       "missing_target",
				"message":    "Provide channel_id to finish setup.",
				"checked_at": time.Now().UTC().Format(time.RFC3339),
			},
		})
	}
	return map[string]any{"items": items, "count": len(items)}
}

func (e *automationIntentStubExecutor) destinationProbe(input map[string]any) map[string]any {
	kind := strings.TrimSpace(fmt.Sprint(input["kind"]))
	provider := strings.TrimSpace(fmt.Sprint(input["provider"]))
	channel := strings.TrimSpace(fmt.Sprint(input["channel"]))
	accountID := strings.TrimSpace(fmt.Sprint(input["account_id"]))
	targetType := strings.TrimSpace(fmt.Sprint(input["target_type"]))
	target := strings.TrimSpace(fmt.Sprint(input["target"]))
	status := "ready"
	reachable := true
	code := "configured"
	message := "Destination is configured."
	if strings.TrimSpace(target) == "" {
		status = "needs_target"
		reachable = false
		code = "missing_target"
		message = "Destination target is required."
	}
	return map[string]any{
		"spec": map[string]any{
			"kind":        kind,
			"provider":    provider,
			"channel":     channel,
			"account_id":  accountID,
			"target_type": targetType,
			"target":      target,
		},
		"probe": map[string]any{
			"kind":       kind,
			"provider":   provider,
			"status":     status,
			"reachable":  reachable,
			"code":       code,
			"message":    message,
			"checked_at": time.Now().UTC().Format(time.RFC3339),
		},
	}
}

func (e *automationIntentStubExecutor) search(input map[string]any) map[string]any {
	query := strings.ToLower(strings.TrimSpace(fmt.Sprint(input["query"])))
	kind := strings.ToLower(strings.TrimSpace(fmt.Sprint(input["kind"])))

	items := make([]map[string]any, 0, len(e.items))
	for _, item := range e.items {
		if kind != "" && item.Kind != kind {
			continue
		}
		if query != "" {
			haystack := strings.ToLower(strings.Join([]string{
				item.ID,
				item.Name,
				item.Schedule,
				item.Message,
				item.PromptPreview,
				item.DeliveryChannel,
				item.DeliveryTarget,
			}, " "))
			match := strings.Contains(haystack, query)
			if !match {
				match = true
				for _, token := range strings.Fields(query) {
					if !strings.Contains(haystack, token) {
						match = false
						break
					}
				}
			}
			if !match {
				continue
			}
		}
		items = append(items, map[string]any{
			"id":                         item.ID,
			"kind":                       item.Kind,
			"name":                       item.Name,
			"enabled":                    item.Enabled,
			"schedule":                   item.Schedule,
			"message":                    item.Message,
			"prompt_preview":             item.PromptPreview,
			"delivery_channel":           item.DeliveryChannel,
			"delivery_target":            item.DeliveryTarget,
			"notification_today_count":   item.NotificationTodayCount,
			"notification_failure_count": item.NotificationFailureCount,
			"notification_today_date":    item.NotificationTodayDate,
		})
	}
	return map[string]any{"items": items, "count": len(items)}
}

func (e *automationIntentStubExecutor) stats(input map[string]any) map[string]any {
	search := e.search(input)
	rawItems, _ := search["items"].([]map[string]any)
	total := len(rawItems)
	enabled := 0
	cronCount := 0
	today := 0
	failures := 0
	for _, item := range rawItems {
		if testBoolValue(item["enabled"], false) {
			enabled++
		}
		if strings.TrimSpace(fmt.Sprint(item["kind"])) == "cron" {
			cronCount++
		}
		today += intValue(item["notification_today_count"])
		failures += intValue(item["notification_failure_count"])
	}
	return map[string]any{
		"total_count":                total,
		"enabled_count":              enabled,
		"disabled_count":             total - enabled,
		"cron_count":                 cronCount,
		"wakeup_count":               0,
		"watch_count":                0,
		"notification_today_count":   today,
		"notification_failure_count": failures,
		"notification_today_date":    time.Now().UTC().Format("2006-01-02"),
	}
}

func newAutomationIntentService(t *testing.T, exec *automationIntentStubExecutor) *Service {
	t.Helper()

	sessions := agent.NewInMemorySessionStore()
	runs := agent.NewInMemoryRunStore()
	approvals := approval.NewInMemoryStore()
	artifacts := artifact.NewInMemoryStore()
	bus := eventbus.NewInMemoryBus()
	component := agent.NewComponent(agent.AgentConfig{
		DefaultModel: "test-model",
		QueueMode:    agent.QueueEnqueue,
	}, sessions, runs, agent.NewInMemoryCoordinator(), newContextEngine(), mockModelClient{}, exec, nil)
	return NewService(component, sessions, runs, approvals, bus, artifacts)
}

func TestInteractAutomationIntentQueryStats(t *testing.T) {
	t.Parallel()

	exec := &automationIntentStubExecutor{
		items: []automationIntentStubItem{{
			ID:                       "cron-news",
			Kind:                     "cron",
			Name:                     "Daily news",
			Enabled:                  true,
			Schedule:                 "0 8 * * *",
			NotificationTodayCount:   2,
			NotificationFailureCount: 1,
			NotificationTodayDate:    time.Now().UTC().Format("2006-01-02"),
		}},
	}
	svc := newAutomationIntentService(t, exec)
	svc.WithAutomationClassifier(stubAutomationIntentClassifier{
		plan: automationintent.Plan{
			Action:     automationintent.ActionQuery,
			Confidence: 0.92,
			Query: automationintent.Query{
				Metric: automationintent.QueryMetricNotificationToday,
			},
		},
	})

	result, err := svc.Interact(context.Background(), InteractionRequest{
		SessionKey: "chat-automation",
		Content:    "今天一共给我发了多少通知消息了？",
	})
	if err != nil {
		t.Fatalf("Interact() error = %v", err)
	}
	if result.Decision.ReplyAct != ReplyActChatReply {
		t.Fatalf("ReplyAct = %q, want %q", result.Decision.ReplyAct, ReplyActChatReply)
	}
	if !strings.Contains(result.ReplyMessage, "今天已发送 2 条通知") {
		t.Fatalf("ReplyMessage = %q", result.ReplyMessage)
	}
}

func TestInteractAutomationIntentDisableUniqueMatch(t *testing.T) {
	t.Parallel()

	exec := &automationIntentStubExecutor{
		items: []automationIntentStubItem{{
			ID:       "cron-weather",
			Kind:     "cron",
			Name:     "Shanghai and Beijing weather",
			Enabled:  true,
			Schedule: "0 8 * * *",
		}},
	}
	svc := newAutomationIntentService(t, exec)
	svc.WithAutomationClassifier(stubAutomationIntentClassifier{
		plan: automationintent.Plan{
			Action:     automationintent.ActionDisable,
			Confidence: 0.93,
			Selector: automationintent.Selector{
				Query: "Shanghai and Beijing weather",
				Names: []string{"Shanghai and Beijing weather"},
			},
			Kind: "cron",
		},
	})

	result, err := svc.Interact(context.Background(), InteractionRequest{
		SessionKey: "chat-automation",
		Content:    "取消 每天 上海和北京 天气信息的通知",
	})
	if err != nil {
		t.Fatalf("Interact() error = %v", err)
	}
	if result.Decision.ReplyAct != ReplyActActionAck {
		t.Fatalf("ReplyAct = %q, want %q", result.Decision.ReplyAct, ReplyActActionAck)
	}
	if !strings.Contains(result.ReplyMessage, "已停用") {
		t.Fatalf("ReplyMessage = %q", result.ReplyMessage)
	}
	if exec.items[0].Enabled {
		t.Fatal("cron automation should be disabled")
	}
}

func TestInteractAutomationIntentCreateCronInfersDeliveryTarget(t *testing.T) {
	t.Parallel()

	exec := &automationIntentStubExecutor{}
	svc := newAutomationIntentService(t, exec)
	svc.WithAutomationClassifier(stubAutomationIntentClassifier{
		plan: automationintent.Plan{
			Action:     automationintent.ActionCreate,
			Confidence: 0.95,
			Kind:       "cron",
			Spec: automationintent.Spec{
				Kind:     "cron",
				Name:     "Daily news and weather",
				Schedule: "0 8 * * *",
				Timezone: "Asia/Shanghai",
				Prompt:   "Collect latest news and Beijing weather, then send a concise briefing.",
			},
		},
	})

	result, err := svc.Interact(context.Background(), InteractionRequest{
		SessionKey: "feishu:chat-auto",
		Content:    "每天收集最新新闻，还有每天北京的天气信息，发我飞书",
		Metadata: map[string]any{
			meta.KeyChannel: "feishu",
			meta.KeyChatID:  "chat-123",
			"account_id":    "ops",
		},
	})
	if err != nil {
		t.Fatalf("Interact() error = %v", err)
	}
	if result.Decision.ReplyAct != ReplyActActionAck {
		t.Fatalf("ReplyAct = %q, want %q", result.Decision.ReplyAct, ReplyActActionAck)
	}
	if !strings.Contains(result.ReplyMessage, "已创建 cron 自动化") {
		t.Fatalf("ReplyMessage = %q", result.ReplyMessage)
	}
	if len(exec.items) != 1 {
		t.Fatalf("items len = %d, want 1", len(exec.items))
	}
	if exec.items[0].DeliveryChannel != "feishu" || exec.items[0].DeliveryTarget != "chat-123" || exec.items[0].DeliveryAccountID != "ops" {
		t.Fatalf("delivery = %#v", exec.items[0])
	}
}

func TestInteractAutomationIntentCreateCronReturnsSetupContractWhenDestinationMissing(t *testing.T) {
	t.Parallel()

	exec := &automationIntentStubExecutor{}
	svc := newAutomationIntentService(t, exec)
	svc.WithAutomationClassifier(stubAutomationIntentClassifier{
		plan: automationintent.Plan{
			Action:     automationintent.ActionCreate,
			Confidence: 0.95,
			Kind:       "cron",
			Spec: automationintent.Spec{
				Kind:     "cron",
				Name:     "Daily briefing",
				Schedule: "0 8 * * *",
				Prompt:   "Collect the latest news and send a briefing.",
			},
		},
	})

	result, err := svc.Interact(context.Background(), InteractionRequest{
		SessionKey: "api:automation",
		Content:    "每天早上 8 点给我发简报",
	})
	if err != nil {
		t.Fatalf("Interact() error = %v", err)
	}
	if result.Decision.ReplyAct != ReplyActClarificationPrompt {
		t.Fatalf("ReplyAct = %q, want %q", result.Decision.ReplyAct, ReplyActClarificationPrompt)
	}
	if result.SetupContract == nil {
		t.Fatal("SetupContract should be populated")
	}
	if result.SetupContract.Status != automationsetup.ContractStatusNeedsInput {
		t.Fatalf("SetupContract.Status = %q", result.SetupContract.Status)
	}
	if !strings.Contains(result.ReplyMessage, "通知投递目标") {
		t.Fatalf("ReplyMessage = %q", result.ReplyMessage)
	}
	if len(exec.items) != 0 {
		t.Fatalf("items len = %d, want 0", len(exec.items))
	}
}

func TestAutomationSetupMessageIncludesAllSlots(t *testing.T) {
	t.Parallel()

	message := automationSetupMessage(&automationsetup.Contract{
		Summary: "还需要补充自动化参数。",
		Slots: []automationsetup.SetupSlot{
			{
				ID:     "delivery",
				Prompt: "请选择通知目标。",
				Options: []automationsetup.SetupOption{
					{Label: "当前会话", Description: "把结果发回这里"},
				},
			},
			{
				ID:      "schedule",
				Prompt:  "请选择执行频率。",
				Options: []automationsetup.SetupOption{{Label: "每天 8 点", Description: "工作日早晨执行"}},
				Example: "每天 8 点执行",
			},
		},
	})

	for _, want := range []string{"请选择通知目标。", "当前会话", "请选择执行频率。", "每天 8 点", "例如：每天 8 点执行"} {
		if !strings.Contains(message, want) {
			t.Fatalf("automationSetupMessage() = %q, want substring %q", message, want)
		}
	}
}

func testBoolValue(value any, fallback bool) bool {
	switch typed := value.(type) {
	case nil:
		return fallback
	case bool:
		return typed
	default:
		return fallback
	}
}

func intValue(value any) int {
	switch typed := value.(type) {
	case int:
		return typed
	case float64:
		return int(typed)
	default:
		return 0
	}
}
