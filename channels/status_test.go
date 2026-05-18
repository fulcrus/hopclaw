package channels

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/fulcrus/hopclaw/agent"
	"github.com/fulcrus/hopclaw/eventbus"
)

func TestRunStatusNotifierTrackAndClear(t *testing.T) {
	t.Parallel()

	var mu sync.Mutex
	var sent []OutboundMessage
	send := func(_ context.Context, msg OutboundMessage) error {
		mu.Lock()
		sent = append(sent, msg)
		mu.Unlock()
		return nil
	}

	notifier := NewRunStatusNotifier(time.Hour, send)
	ctx := context.Background()

	target := RunNotificationTarget{
		RunID:     "run-1",
		TargetID:  "user-1",
		ChannelID: "test",
	}
	notifier.Track(ctx, target)

	snap, ok := notifier.SnapshotRun("run-1")
	if !ok {
		t.Fatal("expected SnapshotRun to find tracked run")
	}
	if snap.Target.RunID != "run-1" {
		t.Fatalf("snap.Target.RunID = %q, want %q", snap.Target.RunID, "run-1")
	}

	notifier.Clear("run-1")
	_, ok = notifier.SnapshotRun("run-1")
	if ok {
		t.Fatal("expected SnapshotRun to return false after Clear")
	}
}

func TestRunStatusNotifierToolProgress(t *testing.T) {
	t.Parallel()

	noopSend := func(_ context.Context, _ OutboundMessage) error { return nil }
	notifier := NewRunStatusNotifier(time.Hour, noopSend)
	ctx := context.Background()

	notifier.Track(ctx, RunNotificationTarget{
		RunID:     "run-2",
		TargetID:  "user-2",
		ChannelID: "test",
	})

	ok := notifier.NotifyToolProgress("run-2", 3, []string{"fs.read", "fs.write"})
	if !ok {
		t.Fatal("expected NotifyToolProgress to succeed")
	}

	snap, ok := notifier.SnapshotRun("run-2")
	if !ok {
		t.Fatal("expected snapshot to exist")
	}
	if snap.ToolRounds != 3 {
		t.Fatalf("snap.ToolRounds = %d, want 3", snap.ToolRounds)
	}
	if len(snap.ToolNames) != 2 || snap.ToolNames[0] != "fs.read" {
		t.Fatalf("snap.ToolNames = %v", snap.ToolNames)
	}
}

func TestRunStatusNotifierFindBySessionKey(t *testing.T) {
	t.Parallel()

	noopSend := func(_ context.Context, _ OutboundMessage) error { return nil }
	notifier := NewRunStatusNotifier(time.Hour, noopSend)
	ctx := context.Background()

	notifier.Track(ctx, RunNotificationTarget{
		RunID:      "run-3",
		SessionKey: "telegram:12345",
		TargetID:   "12345",
		ChannelID:  "telegram",
	})

	snap, ok := notifier.FindBySessionKey("telegram:12345")
	if !ok {
		t.Fatal("expected FindBySessionKey to find tracked run")
	}
	if snap.Target.RunID != "run-3" {
		t.Fatalf("snap.Target.RunID = %q", snap.Target.RunID)
	}

	_, ok = notifier.FindBySessionKey("slack:unknown")
	if ok {
		t.Fatal("expected FindBySessionKey to return false for unknown key")
	}
}

func TestRunStatusNotifierNilSafe(t *testing.T) {
	t.Parallel()

	var notifier *RunStatusNotifier
	notifier.Track(context.Background(), RunNotificationTarget{RunID: "run-nil"})
	notifier.Clear("run-nil")
	ok := notifier.NotifyToolProgress("run-nil", 1, nil)
	if ok {
		t.Fatal("expected false from nil notifier")
	}
	_, found := notifier.SnapshotRun("run-nil")
	if found {
		t.Fatal("expected false from nil notifier snapshot")
	}
}

func TestRunStatusNotifierNotifyCancelledSendsConfirmation(t *testing.T) {
	t.Parallel()

	var mu sync.Mutex
	var sent []OutboundMessage
	send := func(_ context.Context, msg OutboundMessage) error {
		mu.Lock()
		sent = append(sent, msg)
		mu.Unlock()
		return nil
	}

	notifier := NewRunStatusNotifier(time.Hour, send)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	notifier.Track(ctx, RunNotificationTarget{
		RunID:        "run-cancel",
		TargetID:     "user-cancel",
		ChannelID:    "test",
		InputContent: "帮我处理一下",
	})

	if ok := notifier.NotifyCancelled(ctx, "run-cancel"); !ok {
		t.Fatal("expected NotifyCancelled to succeed")
	}

	mu.Lock()
	defer mu.Unlock()
	if len(sent) != 1 {
		t.Fatalf("len(sent) = %d, want 1", len(sent))
	}
	if sent[0].Content != "这个请求已被取消。" {
		t.Fatalf("sent[0].Content = %q", sent[0].Content)
	}
	if got := sent[0].Metadata["status_kind"]; got != "cancelled" {
		t.Fatalf("status_kind = %#v, want %q", got, "cancelled")
	}
	if _, ok := notifier.SnapshotRun("run-cancel"); ok {
		t.Fatal("expected tracked run to be cleared after cancellation")
	}
}

func TestRunStatusNotifierNotifyApprovalSendsImmediateStatus(t *testing.T) {
	t.Parallel()

	var mu sync.Mutex
	var sent []OutboundMessage
	send := func(_ context.Context, msg OutboundMessage) error {
		mu.Lock()
		sent = append(sent, msg)
		mu.Unlock()
		return nil
	}

	notifier := NewRunStatusNotifier(time.Hour, send)
	ctx := context.Background()
	notifier.Track(ctx, RunNotificationTarget{
		RunID:        "run-approval",
		TargetID:     "user-approval",
		ChannelID:    "test",
		InputContent: "please continue",
	})

	if ok := notifier.NotifyApproval(ctx, "run-approval"); !ok {
		t.Fatal("expected NotifyApproval to succeed")
	}

	mu.Lock()
	defer mu.Unlock()
	if len(sent) != 1 {
		t.Fatalf("len(sent) = %d, want 1", len(sent))
	}
	if got := sent[0].Metadata["status_kind"]; got != "approval_waiting" {
		t.Fatalf("status_kind = %#v, want %q", got, "approval_waiting")
	}
	if got := sent[0].Metadata["phase"]; got != "waiting_approval" {
		t.Fatalf("phase = %#v, want %q", got, "waiting_approval")
	}
	if sent[0].Content != "This request is waiting for approval. Reply with [1] Approve  [2] Deny  [3] Always." {
		t.Fatalf("sent[0].Content = %q", sent[0].Content)
	}

	snap, ok := notifier.SnapshotRun("run-approval")
	if !ok {
		t.Fatal("expected approval run snapshot")
	}
	if snap.Phase != "waiting_approval" {
		t.Fatalf("snap.Phase = %q, want %q", snap.Phase, "waiting_approval")
	}
}

func TestRunStatusNotifierNotifyApprovalDoesNotResendOnFirstHeartbeatWithoutNewProgress(t *testing.T) {
	t.Parallel()

	var mu sync.Mutex
	var sent []OutboundMessage
	send := func(_ context.Context, msg OutboundMessage) error {
		mu.Lock()
		sent = append(sent, msg)
		mu.Unlock()
		return nil
	}

	notifier := NewRunStatusNotifier(10*time.Millisecond, send)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	notifier.Track(ctx, RunNotificationTarget{
		RunID:        "run-approval-reminder",
		TargetID:     "user-approval-reminder",
		ChannelID:    "test",
		InputContent: "请继续执行",
	})

	if ok := notifier.NotifyApproval(ctx, "run-approval-reminder"); !ok {
		t.Fatal("expected NotifyApproval to succeed")
	}

	time.Sleep(50 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()
	if len(sent) != 1 {
		t.Fatalf("len(sent) = %d, want 1 immediate approval notification", len(sent))
	}
}

func TestRunStatusNotifierRestoreRunningRunSendsImmediateProgress(t *testing.T) {
	t.Parallel()

	var mu sync.Mutex
	var sent []OutboundMessage
	send := func(_ context.Context, msg OutboundMessage) error {
		mu.Lock()
		defer mu.Unlock()
		sent = append(sent, msg)
		return nil
	}

	notifier := NewRunStatusNotifier(time.Hour, send)
	ctx := context.Background()
	run := &agent.Run{
		ID:     "run-restore-running",
		Status: agent.RunRunning,
		Phase:  "executing_tools",
	}
	target := RunNotificationTarget{
		TargetID:     "user-restore-running",
		ChannelID:    "test",
		InputContent: "restore progress",
	}

	if ok := notifier.Restore(ctx, target, run); !ok {
		t.Fatal("expected Restore() to succeed for running run")
	}

	mu.Lock()
	defer mu.Unlock()
	if len(sent) != 1 {
		t.Fatalf("len(sent) = %d, want 1", len(sent))
	}
	if got := sent[0].Metadata["status_kind"]; got != "processing" {
		t.Fatalf("status_kind = %#v, want %q", got, "processing")
	}
	if got := sent[0].Metadata["phase"]; got != "executing_tools" {
		t.Fatalf("phase = %#v, want %q", got, "executing_tools")
	}

	snap, ok := notifier.SnapshotRun("run-restore-running")
	if !ok {
		t.Fatal("expected restored run snapshot")
	}
	if snap.Phase != "executing_tools" {
		t.Fatalf("snap.Phase = %q, want %q", snap.Phase, "executing_tools")
	}
}

func TestRunStatusNotifierRestoreWaitingApprovalSendsImmediateApproval(t *testing.T) {
	t.Parallel()

	var mu sync.Mutex
	var sent []OutboundMessage
	send := func(_ context.Context, msg OutboundMessage) error {
		mu.Lock()
		defer mu.Unlock()
		sent = append(sent, msg)
		return nil
	}

	notifier := NewRunStatusNotifier(time.Hour, send)
	ctx := context.Background()
	run := &agent.Run{
		ID:     "run-restore-approval",
		Status: agent.RunWaitingApproval,
	}
	target := RunNotificationTarget{
		TargetID:     "user-restore-approval",
		ChannelID:    "test",
		InputContent: "restore approval",
	}

	if ok := notifier.Restore(ctx, target, run); !ok {
		t.Fatal("expected Restore() to succeed for waiting approval run")
	}

	mu.Lock()
	defer mu.Unlock()
	if len(sent) != 1 {
		t.Fatalf("len(sent) = %d, want 1", len(sent))
	}
	if got := sent[0].Metadata["status_kind"]; got != "approval_waiting" {
		t.Fatalf("status_kind = %#v, want %q", got, "approval_waiting")
	}
	if got := sent[0].Metadata["phase"]; got != "waiting_approval" {
		t.Fatalf("phase = %#v, want %q", got, "waiting_approval")
	}

	snap, ok := notifier.SnapshotRun("run-restore-approval")
	if !ok {
		t.Fatal("expected restored approval run snapshot")
	}
	if snap.Phase != "waiting_approval" {
		t.Fatalf("snap.Phase = %q, want %q", snap.Phase, "waiting_approval")
	}
}

func TestAuthFailureGateArmAndBlock(t *testing.T) {
	t.Parallel()

	gate := NewAuthFailureGate(time.Hour, time.Hour)

	blocked, notify := gate.Blocked("key-1")
	if blocked || notify {
		t.Fatal("expected key-1 to not be blocked before arming")
	}

	gate.Arm("key-1")

	blocked, notify = gate.Blocked("key-1")
	if !blocked || !notify {
		t.Fatal("expected key-1 to be blocked after arming")
	}

	gate.Clear("key-1")
	blocked, _ = gate.Blocked("key-1")
	if blocked {
		t.Fatal("expected key-1 to not be blocked after clearing")
	}
}

func TestExtractToolProgress(t *testing.T) {
	t.Parallel()

	event := eventbus.Event{
		Attrs: map[string]any{
			"tool_round": 5,
			"tool_names": []string{"fs.read", "exec.run"},
		},
	}

	rounds, names := ExtractToolProgress(event)
	if rounds != 5 {
		t.Fatalf("rounds = %d, want 5", rounds)
	}
	if len(names) != 2 || names[0] != "fs.read" {
		t.Fatalf("names = %v", names)
	}
}

func TestExtractToolProgressFloat64Round(t *testing.T) {
	t.Parallel()

	event := eventbus.Event{
		Attrs: map[string]any{
			"tool_round": float64(3),
			"tool_names": []any{"search.web", "fs.write"},
		},
	}

	rounds, names := ExtractToolProgress(event)
	if rounds != 3 {
		t.Fatalf("rounds = %d, want 3", rounds)
	}
	if len(names) != 2 {
		t.Fatalf("names = %v", names)
	}
}

func TestExtractPlanProgress(t *testing.T) {
	t.Parallel()

	event := eventbus.Event{
		Attrs: map[string]any{
			"task_title":      "compile code",
			"completed_count": 2,
			"total_tasks":     5,
		},
	}

	task, completed, total := ExtractPlanProgress(event)
	if task != "compile code" {
		t.Fatalf("task = %q", task)
	}
	if completed != 2 {
		t.Fatalf("completed = %d", completed)
	}
	if total != 5 {
		t.Fatalf("total = %d", total)
	}
}

func TestBridgeProcessingMessageEnglish(t *testing.T) {
	t.Parallel()

	msg := BridgeProcessingMessage("hello world", 3, []string{"fs.read", "exec.run"}, "", 0, 0)
	if msg == "" {
		t.Fatal("expected non-empty message")
	}
	if !containsSubstring(msg, "verified result") {
		t.Fatalf("msg = %q, expected verification mention", msg)
	}
}

func TestBridgeProcessingMessageChinese(t *testing.T) {
	t.Parallel()

	msg := BridgeProcessingMessage("帮我搜索新闻", 2, []string{"search.web"}, "", 0, 0)
	if msg == "" {
		t.Fatal("expected non-empty message")
	}
	if !containsSubstring(msg, "可验证的结果") {
		t.Fatalf("msg = %q, expected verification mention", msg)
	}
}

func TestBridgeProgressUpdateMessageEnglish(t *testing.T) {
	t.Parallel()

	msg := BridgeProgressUpdateMessage("search the latest news", "executing_tools", 2, []string{"search.news", "web.fetch"}, "Fetch sources", 1, 3)
	if !containsSubstring(msg, "Currently running: search.news, web.fetch.") {
		t.Fatalf("msg = %q, expected tool progress detail", msg)
	}
	if !containsSubstring(msg, "Current step: Fetch sources. Progress: 1/3.") {
		t.Fatalf("msg = %q, expected task progress detail", msg)
	}
}

func TestBridgeProgressStatusMessageWithRun(t *testing.T) {
	t.Parallel()

	run := &agent.Run{
		ID:     "run-status",
		Status: agent.RunRunning,
		Phase:  "tools",
	}
	msg := BridgeProgressStatusMessage("check progress", run, 2, []string{"fs.read"})
	if msg == "" {
		t.Fatal("expected non-empty status message")
	}
	if !containsSubstring(msg, "still working") {
		t.Fatalf("msg = %q, expected friendly running mention", msg)
	}
}

func TestBridgeProgressStatusMessageIncludesPreflight(t *testing.T) {
	t.Parallel()

	run := &agent.Run{
		ID:     "run-status-preflight",
		Status: agent.RunRunning,
		Phase:  "tools",
		Preflight: &agent.RunPreflightReport{
			State:   agent.RunPreflightAutoPreparing,
			Summary: "The system is preparing the required capabilities automatically.",
		},
	}
	msg := BridgeProgressStatusMessage("check progress", run, 1, []string{"browser.screenshot"})
	if !containsSubstring(msg, "Preflight:") {
		t.Fatalf("msg = %q, expected preflight line", msg)
	}
}

func TestBridgeProgressStatusMessageWaitingInput(t *testing.T) {
	t.Parallel()

	run := &agent.Run{
		ID:     "run-status-waiting-input",
		Status: agent.RunWaitingInput,
		Phase:  "ingest",
		Preflight: &agent.RunPreflightReport{
			State:    agent.RunPreflightNeedsConfirmation,
			Summary:  "More information or confirmation is needed before the task is well-grounded.",
			Prompt:   "Please reply with the exact file path, URL, screenshot, repository, or other concrete target so I can continue.",
			Blocking: true,
			Checks: []agent.RunPreflightCheck{{
				ID:       "reference_gap",
				State:    agent.RunPreflightNeedsConfirmation,
				Blocking: true,
			}},
		},
	}
	msg := BridgeProgressStatusMessage("把这个文件改一下", run, 0, nil)
	if !containsSubstring(msg, "等待补充信息") {
		t.Fatalf("msg = %q", msg)
	}
	if !containsSubstring(msg, "你直接补充") {
		t.Fatalf("msg = %q", msg)
	}
	if !containsSubstring(msg, "可直接这样回") {
		t.Fatalf("msg = %q", msg)
	}
}

func TestBridgePreflightMessageChinese(t *testing.T) {
	t.Parallel()

	msg := BridgePreflightMessage("把这个文件改一下", &agent.RunPreflightReport{
		State:        agent.RunPreflightNeedsConfirmation,
		Summary:      "The request refers to an existing file, URL, screenshot, or repo, but no concrete reference was included.",
		Question:     "What exact file, URL, screenshot, repository, or target should I use?",
		ReplyHints:   []string{"/tmp/demo.txt", "https://example.com/page"},
		ContinueHint: "After you reply, I will continue the current task instead of starting over and I will verify the result before sending it.",
		Blocking:     true,
		Checks: []agent.RunPreflightCheck{{
			ID:       "reference_gap",
			State:    agent.RunPreflightNeedsConfirmation,
			Blocking: true,
		}},
	})
	if !containsSubstring(msg, "前置条件") {
		t.Fatalf("msg = %q", msg)
	}
	if !containsSubstring(msg, "需要你确认") {
		t.Fatalf("msg = %q", msg)
	}
	if !containsSubstring(msg, "可直接回复示例") {
		t.Fatalf("msg = %q", msg)
	}
	if !containsSubstring(msg, "可直接按这个格式回复") {
		t.Fatalf("msg = %q", msg)
	}
	if !containsSubstring(msg, "目标对象：<文件路径 / URL / 仓库 / 截图>") {
		t.Fatalf("msg = %q", msg)
	}
}

func TestBridgePreflightMessageTemplateForScheduleAndDelivery(t *testing.T) {
	t.Parallel()

	msg := BridgePreflightMessage("Please schedule and notify me", &agent.RunPreflightReport{
		State:        agent.RunPreflightNeedsConfirmation,
		Summary:      "More information or confirmation is needed before the task is well-grounded.",
		Question:     "When should this run, and where should I send the result?",
		ContinueHint: "After you reply, I will continue the current task instead of starting over and I will verify the result before sending it.",
		Blocking:     true,
		Checks: []agent.RunPreflightCheck{
			{ID: "schedule", State: agent.RunPreflightNeedsConfirmation, Blocking: true},
			{ID: "delivery_target", State: agent.RunPreflightNeedsConfirmation, Blocking: true},
		},
	})
	if !containsSubstring(msg, "You can reply like this:") {
		t.Fatalf("msg = %q", msg)
	}
	if !containsSubstring(msg, "Schedule: <for example every day at 09:00 starting next Monday>") {
		t.Fatalf("msg = %q", msg)
	}
	if !containsSubstring(msg, "Destination: <email / Slack channel / chat thread / current chat>") {
		t.Fatalf("msg = %q", msg)
	}
}

func TestBridgeProgressStatusMessageNilRun(t *testing.T) {
	t.Parallel()

	msg := BridgeProgressStatusMessage("check progress", nil, 0, nil)
	if !containsSubstring(msg, "no active work") {
		t.Fatalf("msg = %q, expected no active work", msg)
	}
}

func containsSubstring(s, substr string) bool {
	return len(s) >= len(substr) && findSubstring(s, substr)
}

func findSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
		// Case-insensitive search for ASCII.
		match := true
		for j := range substr {
			a, b := s[i+j], substr[j]
			if a >= 'A' && a <= 'Z' {
				a += 32
			}
			if b >= 'A' && b <= 'Z' {
				b += 32
			}
			if a != b {
				match = false
				break
			}
		}
		if match {
			return true
		}
	}
	return false
}
