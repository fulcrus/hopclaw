package channels

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/fulcrus/hopclaw/agent"
	"github.com/fulcrus/hopclaw/approval"
	"github.com/fulcrus/hopclaw/artifact"
	"github.com/fulcrus/hopclaw/contextengine"
	"github.com/fulcrus/hopclaw/eventbus"
	"github.com/fulcrus/hopclaw/internal/meta"
	runtimesvc "github.com/fulcrus/hopclaw/runtime"
	verifyrt "github.com/fulcrus/hopclaw/runtime/verify"
)

type bridgeStubAdapter struct {
	inbound chan InboundMessage
	caps    Capabilities

	mu        sync.Mutex
	sent      []OutboundMessage
	sendCalls int
	sendErrs  []error
}

func (a *bridgeStubAdapter) Connect(context.Context) error    { return nil }
func (a *bridgeStubAdapter) Disconnect(context.Context) error { return nil }
func (a *bridgeStubAdapter) Send(_ context.Context, msg OutboundMessage) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.sendCalls++
	if len(a.sendErrs) > 0 {
		err := a.sendErrs[0]
		a.sendErrs = a.sendErrs[1:]
		if err != nil {
			return err
		}
	}
	a.sent = append(a.sent, msg)
	return nil
}
func (a *bridgeStubAdapter) Capabilities() ChannelCapabilityDescriptor { return a.caps }
func (a *bridgeStubAdapter) Status() Status                            { return StatusConnected }
func (a *bridgeStubAdapter) SubscribeEvents() <-chan InboundMessage    { return a.inbound }
func (a *bridgeStubAdapter) Messages() []OutboundMessage {
	a.mu.Lock()
	defer a.mu.Unlock()
	out := make([]OutboundMessage, len(a.sent))
	copy(out, a.sent)
	return out
}
func (a *bridgeStubAdapter) SendCalls() int {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.sendCalls
}

type bridgeStreamingStubAdapter struct {
	*bridgeStubAdapter

	streamMu      sync.Mutex
	beginCount    int
	updateCount   int
	endCount      int
	streamContent []string
}

func newBridgeStreamingStubAdapter() *bridgeStreamingStubAdapter {
	return &bridgeStreamingStubAdapter{
		bridgeStubAdapter: &bridgeStubAdapter{inbound: make(chan InboundMessage, 1)},
	}
}

func (a *bridgeStreamingStubAdapter) BeginStreaming(_ context.Context, _ OutboundMessage) (string, error) {
	a.streamMu.Lock()
	defer a.streamMu.Unlock()
	a.beginCount++
	return "stream-1", nil
}

func (a *bridgeStreamingStubAdapter) UpdateStreaming(_ context.Context, _ string, content string) error {
	a.streamMu.Lock()
	defer a.streamMu.Unlock()
	a.updateCount++
	a.streamContent = append(a.streamContent, content)
	return nil
}

func (a *bridgeStreamingStubAdapter) EndStreaming(_ context.Context, _ string, final OutboundMessage) error {
	a.streamMu.Lock()
	defer a.streamMu.Unlock()
	a.endCount++
	a.streamContent = append(a.streamContent, final.Content)
	return nil
}

func TestBridgeDeliveredStatePrunesStaleEntries(t *testing.T) {
	t.Parallel()

	bridge := NewBridge(BridgeConfig{ChannelName: "testbridge"}, nil, nil, nil, nil, DefaultStatusReminderDelay)
	now := time.Now().UTC()

	bridge.mu.Lock()
	bridge.delivered["run-stale"] = now.Add(-BridgeDeliveredStateTTL - time.Minute)
	bridge.delivered["run-fresh"] = now
	bridge.mu.Unlock()

	if bridge.isDelivered("run-stale") {
		t.Fatal("isDelivered(stale) = true, want false after pruning")
	}
	if !bridge.isDelivered("run-fresh") {
		t.Fatal("isDelivered(fresh) = false, want true")
	}

	bridge.mu.Lock()
	_, staleExists := bridge.delivered["run-stale"]
	_, freshExists := bridge.delivered["run-fresh"]
	bridge.mu.Unlock()
	if staleExists {
		t.Fatal("stale delivered state should be pruned")
	}
	if !freshExists {
		t.Fatal("fresh delivered state should be retained")
	}
}

func TestBridgeEnsureStreamingStatePrunesStaleEntries(t *testing.T) {
	t.Parallel()

	adapter := newBridgeStreamingStubAdapter()
	bridge := NewBridge(BridgeConfig{ChannelName: "testbridge"}, adapter, nil, nil, nil, DefaultStatusReminderDelay)
	now := time.Now().UTC()

	bridge.mu.Lock()
	bridge.streams["run-stale"] = &StreamingDeliveryState{
		Handle:    "stream-stale",
		UpdatedAt: now.Add(-BridgeStreamingStateTTL - time.Minute),
	}
	bridge.mu.Unlock()

	state := bridge.ensureStreamingState(context.Background(), adapter, "run-fresh", RunNotificationTarget{
		ChannelID:    "testbridge",
		TargetID:     "chat-1",
		ReplyToID:    "msg-1",
		InputContent: "stream please",
		Format:       "text",
		Metadata:     map[string]any{"k": "v"},
	})
	if state == nil {
		t.Fatal("ensureStreamingState() = nil")
	}
	if state.Handle != "stream-1" {
		t.Fatalf("state.Handle = %q, want %q", state.Handle, "stream-1")
	}
	if state.UpdatedAt.IsZero() {
		t.Fatal("state.UpdatedAt should be set")
	}

	bridge.mu.Lock()
	_, staleExists := bridge.streams["run-stale"]
	_, freshExists := bridge.streams["run-fresh"]
	bridge.mu.Unlock()
	if staleExists {
		t.Fatal("stale streaming state should be pruned")
	}
	if !freshExists {
		t.Fatal("fresh streaming state should be stored")
	}
	if adapter.beginCount != 1 {
		t.Fatalf("BeginStreaming() count = %d, want 1", adapter.beginCount)
	}
}

type bridgeStubRuntime struct {
	mu               sync.Mutex // guards all fields
	interactionReq   *runtimesvc.InteractionRequest
	submitted        *runtimesvc.SubmitRequest
	run              *agent.Run
	runResult        *runtimesvc.RunResult
	runVerification  *verifyrt.RunVerification
	submitRun        *agent.Run
	submitErr        error
	submits          int
	cancelled        []string
	pendingBySession map[string]*approval.Ticket
	approvalsByID    map[string]*approval.Ticket
	resolved         []approval.Resolution
	artifacts        map[string]*artifact.Blob
}

type bridgeInteractStubRuntime struct {
	*bridgeStubRuntime
	interactionResp *runtimesvc.InteractionResult
	interactionErr  error
	interactions    int
}

func (r *bridgeStubRuntime) Submit(_ context.Context, req runtimesvc.SubmitRequest) (*agent.Run, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.submits++
	if r.submitErr != nil {
		return nil, r.submitErr
	}
	r.submitted = &req
	if r.submitRun != nil {
		copyRun := *r.submitRun
		return &copyRun, nil
	}
	return r.run, nil
}

func (r *bridgeStubRuntime) Interact(ctx context.Context, req runtimesvc.InteractionRequest) (*runtimesvc.InteractionResult, error) {
	r.mu.Lock()
	copyReq := req
	r.interactionReq = &copyReq
	r.mu.Unlock()

	action := ApprovalReplyAction("")
	ok := false
	if req.StructuredApproval != nil {
		action = ApprovalReplyAction(req.StructuredApproval.Action)
		ok = action != ""
	} else {
		action, _, ok = ParseApprovalReplySignal(nil, req.Content)
	}
	if ok {
		r.mu.Lock()
		var ticket *approval.Ticket
		for _, item := range r.pendingBySession {
			if item != nil {
				copyTicket := *item
				ticket = &copyTicket
				break
			}
		}
		run := r.run
		r.mu.Unlock()
		if ticket == nil {
			return &runtimesvc.InteractionResult{
				Decision: runtimesvc.InteractionDecision{
					SpeechAct: runtimesvc.SpeechActApprovalReply,
					ReplyAct:  runtimesvc.ReplyActChatReply,
					Reason:    "approval_reply_no_pending",
				},
			}, nil
		}
		resolution := approval.Resolution{ResolvedBy: "interaction"}
		switch action {
		case ApprovalReplyApprove, ApprovalReplyAlways:
			resolution.Status = approval.StatusApproved
		case ApprovalReplyDeny:
			resolution.Status = approval.StatusDenied
		}
		if _, err := r.ResolveApproval(ctx, ticket.ID, resolution); err != nil {
			return nil, err
		}
		if run != nil && strings.TrimSpace(run.ID) == "" {
			run = nil
		}
		return &runtimesvc.InteractionResult{
			Decision: runtimesvc.InteractionDecision{
				SpeechAct: runtimesvc.SpeechActApprovalReply,
				ReplyAct:  runtimesvc.ReplyActResumeAck,
				Reason:    "structured_approval_" + string(action),
			},
			Context: runtimesvc.InteractionContextSnapshot{
				SessionID:       ticket.SessionID,
				ActiveRunID:     ticket.RunID,
				WaitingApproval: true,
				PendingTicketID: ticket.ID,
			},
			Run:              run,
			ApprovalResolved: true,
			ApprovalStatus:   resolution.Status,
		}, nil
	}
	if cmd, ok := ParseControlCommand(req.Content); ok {
		switch cmd {
		case ControlCommandStatus:
			r.mu.Lock()
			run := r.run
			r.mu.Unlock()
			return &runtimesvc.InteractionResult{
				Decision: runtimesvc.InteractionDecision{
					SpeechAct: runtimesvc.SpeechActStatusQuery,
					ReplyAct:  runtimesvc.ReplyActStatusReply,
					Reason:    "text_command_status",
				},
				Run: run,
			}, nil
		case ControlCommandCancel:
			r.mu.Lock()
			runID := ""
			if r.run != nil {
				runID = r.run.ID
			}
			r.mu.Unlock()
			cancelledRun, err := r.CancelRun(ctx, runID)
			if err != nil {
				return nil, err
			}
			return &runtimesvc.InteractionResult{
				Decision: runtimesvc.InteractionDecision{
					SpeechAct: runtimesvc.SpeechActCommand,
					ReplyAct:  runtimesvc.ReplyActActionAck,
					Reason:    "text_command_cancel",
				},
				Run:          cancelledRun,
				RunCancelled: true,
			}, nil
		}
	}
	submitReq := runtimesvc.SubmitRequest{
		SessionKey:      req.SessionKey,
		ParentRunID:     req.ParentRunID,
		ExternalEventID: req.ExternalEventID,
		Content:         req.Content,
		AutomationID:    req.AutomationID,
		Metadata:        req.Metadata,
	}
	run, err := r.Submit(ctx, submitReq)
	if err != nil {
		return &runtimesvc.InteractionResult{
			Decision: runtimesvc.InteractionDecision{
				SpeechAct: runtimesvc.SpeechActNewTask,
				ReplyAct:  runtimesvc.ReplyActTaskFailure,
			},
			SubmitRequest: &submitReq,
			Error:         err.Error(),
		}, nil
	}
	return &runtimesvc.InteractionResult{
		Decision: runtimesvc.InteractionDecision{
			SpeechAct: runtimesvc.SpeechActNewTask,
			ReplyAct:  runtimesvc.ReplyActTaskAccept,
		},
		Run:           run,
		SubmitRequest: &submitReq,
	}, nil
}

func (r *bridgeStubRuntime) InteractionRequest() *runtimesvc.InteractionRequest {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.interactionReq == nil {
		return nil
	}
	copyReq := *r.interactionReq
	return &copyReq
}

func TestGenericBridgeApprovesPendingRequestViaStructuredCallback(t *testing.T) {
	t.Parallel()

	store := agent.NewInMemorySessionStore()
	session, err := store.GetOrCreate(context.Background(), "whatsapp:8613800138000", "test-model")
	if err != nil {
		t.Fatalf("GetOrCreate() error = %v", err)
	}

	adapter := &bridgeStubAdapter{inbound: make(chan InboundMessage, 1)}
	runtime := &bridgeStubRuntime{
		run: &agent.Run{ID: "run-approve-structured"},
		pendingBySession: map[string]*approval.Ticket{
			session.ID: {ID: "appr-structured", RunID: "run-approve-structured", SessionID: session.ID, Kind: approval.KindSkillInstall, Status: approval.StatusPending},
		},
		approvalsByID: map[string]*approval.Ticket{
			"appr-structured": {ID: "appr-structured", RunID: "run-approve-structured", SessionID: session.ID, Kind: approval.KindSkillInstall, Status: approval.StatusPending},
		},
	}
	bridge := NewBridge(BridgeConfig{
		ChannelName:  "whatsapp",
		TargetIDKey:  "from",
		MessageIDKey: "message_id",
	}, adapter, runtime, store, nil, DefaultStatusReminderDelay)

	bridge.handleInbound(context.Background(), InboundMessage{
		ChannelID: "whatsapp",
		SenderID:  "8613800138000",
		Content:   "",
		RawEvent: map[string]any{
			"from":            "8613800138000",
			"message_id":      "wamid-approve-structured-1",
			"approval_action": "approval:approve",
		},
	})

	if runtime.Submits() != 0 {
		t.Fatalf("runtime.Submits() = %d, want 0", runtime.Submits())
	}
	if len(runtime.Resolved()) != 1 || runtime.Resolved()[0].Status != approval.StatusApproved {
		t.Fatalf("runtime.Resolved() = %#v", runtime.Resolved())
	}
	sent := adapter.Messages()
	if len(sent) != 1 {
		t.Fatalf("len(sent) = %d", len(sent))
	}
	if got := sent[0].Metadata["status_kind"]; got != "approval_reply_approved" {
		t.Fatalf("status_kind = %#v", got)
	}
}

func TestGenericBridgeForwardsInboundImagesToInteract(t *testing.T) {
	t.Parallel()

	store := agent.NewInMemorySessionStore()
	adapter := &bridgeStubAdapter{inbound: make(chan InboundMessage, 1)}
	runtime := &bridgeStubRuntime{run: &agent.Run{ID: "run-images"}}
	bridge := NewBridge(BridgeConfig{
		ChannelName:  "whatsapp",
		TargetIDKey:  "from",
		MessageIDKey: "message_id",
	}, adapter, runtime, store, nil, DefaultStatusReminderDelay)

	bridge.handleInbound(context.Background(), InboundMessage{
		ChannelID: "whatsapp",
		SenderID:  "8613800138000",
		Content:   "describe the image",
		Images:    []string{"data:image/png;base64,ZmFrZS1wbmc="},
		RawEvent: map[string]any{
			"from":       "8613800138000",
			"message_id": "wamid-image-1",
		},
	})

	req := runtime.InteractionRequest()
	if req == nil {
		t.Fatal("expected Interact() to be called")
	}
	if len(req.Images) != 1 || req.Images[0] != "data:image/png;base64,ZmFrZS1wbmc=" {
		t.Fatalf("req.Images = %#v", req.Images)
	}
}

func (r *bridgeInteractStubRuntime) Interact(_ context.Context, _ runtimesvc.InteractionRequest) (*runtimesvc.InteractionResult, error) {
	r.interactions++
	if r.interactionErr != nil {
		return nil, r.interactionErr
	}
	if r.interactionResp == nil {
		return nil, nil
	}
	copyResp := *r.interactionResp
	if r.interactionResp.Run != nil {
		copyRun := *r.interactionResp.Run
		copyResp.Run = &copyRun
	}
	return &copyResp, nil
}

func (r *bridgeStubRuntime) GetRun(_ context.Context, _ string) (*agent.Run, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.run, nil
}

func (r *bridgeStubRuntime) GetRunResult(_ context.Context, _ string) (*runtimesvc.RunResult, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.runResult == nil {
		return nil, nil
	}
	copyResult := *r.runResult
	if len(r.runResult.Deliverables) > 0 {
		copyResult.Deliverables = append([]runtimesvc.DeliverableRef(nil), r.runResult.Deliverables...)
	}
	return &copyResult, nil
}

func (r *bridgeStubRuntime) GetRunVerification(_ context.Context, _ string) (*verifyrt.RunVerification, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.runVerification == nil {
		return nil, nil
	}
	copyVerification := *r.runVerification
	if len(r.runVerification.Checks) > 0 {
		copyVerification.Checks = append([]verifyrt.Check(nil), r.runVerification.Checks...)
	}
	return &copyVerification, nil
}

func (r *bridgeStubRuntime) ListRuns(_ context.Context, filter agent.RunListFilter) ([]*agent.Run, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.run == nil {
		return nil, nil
	}
	if filter.SessionID != "" && r.run.SessionID != filter.SessionID {
		return nil, nil
	}
	copyRun := *r.run
	return []*agent.Run{&copyRun}, nil
}

func (r *bridgeStubRuntime) GetApproval(_ context.Context, id string) (*approval.Ticket, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.approvalsByID == nil {
		return nil, errors.New("approval not found")
	}
	ticket, ok := r.approvalsByID[id]
	if !ok {
		return nil, errors.New("approval not found")
	}
	copyTicket := *ticket
	return &copyTicket, nil
}

func (r *bridgeStubRuntime) FindPendingApproval(_ context.Context, sessionID string) (*approval.Ticket, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.pendingBySession == nil {
		return nil, errors.New("pending approval not found")
	}
	ticket, ok := r.pendingBySession[sessionID]
	if !ok {
		return nil, errors.New("pending approval not found")
	}
	copyTicket := *ticket
	return &copyTicket, nil
}

func (r *bridgeStubRuntime) ResolveApproval(_ context.Context, id string, resolution approval.Resolution) (*approval.Ticket, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.approvalsByID == nil {
		return nil, errors.New("approval not found")
	}
	ticket, ok := r.approvalsByID[id]
	if !ok {
		return nil, errors.New("approval not found")
	}
	copyResolution := resolution
	r.resolved = append(r.resolved, copyResolution)
	ticket.Status = resolution.Status
	ticket.ResolvedBy = resolution.ResolvedBy
	ticket.Note = resolution.Note
	return ticket, nil
}

func (r *bridgeStubRuntime) CancelRun(_ context.Context, runID string) (*agent.Run, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.cancelled = append(r.cancelled, runID)
	if r.run == nil {
		return nil, errors.New("run not found")
	}
	copyRun := *r.run
	copyRun.Status = agent.RunCancelled
	r.run = &copyRun
	return &copyRun, nil
}

func (r *bridgeStubRuntime) GetArtifact(_ context.Context, id string) (*artifact.Blob, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.artifacts == nil {
		return nil, errors.New("artifact not found")
	}
	item, ok := r.artifacts[id]
	if !ok {
		return nil, errors.New("artifact not found")
	}
	copyBlob := *item
	return &copyBlob, nil
}

// Thread-safe accessors for test assertions.
func (r *bridgeStubRuntime) Submits() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.submits
}
func (r *bridgeStubRuntime) Submitted() *runtimesvc.SubmitRequest {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.submitted
}
func (r *bridgeStubRuntime) Cancelled() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]string, len(r.cancelled))
	copy(out, r.cancelled)
	return out
}
func (r *bridgeStubRuntime) Resolved() []approval.Resolution {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]approval.Resolution, len(r.resolved))
	copy(out, r.resolved)
	return out
}

func TestGenericBridgeUsesSenderAsFallbackTarget(t *testing.T) {
	t.Parallel()

	adapter := &bridgeStubAdapter{inbound: make(chan InboundMessage, 1)}
	runtime := &bridgeStubRuntime{run: &agent.Run{ID: "run-1"}}
	bridge := NewBridge(BridgeConfig{
		ChannelName:  "whatsapp",
		TargetIDKey:  "from",
		MessageIDKey: "message_id",
	}, adapter, runtime, agent.NewInMemorySessionStore(), nil, DefaultStatusReminderDelay)

	bridge.handleInbound(context.Background(), InboundMessage{
		ChannelID: "whatsapp",
		SenderID:  "8613800138000",
		Content:   "hello",
		RawEvent: map[string]any{
			"message_id": "wamid-1",
		},
	})

	if runtime.Submitted() == nil {
		t.Fatal("expected runtime submission")
	}
	if runtime.Submitted().SessionKey != "whatsapp:8613800138000" {
		t.Fatalf("SessionKey = %q", runtime.Submitted().SessionKey)
	}
	if got, _ := runtime.Submitted().Metadata["from"].(string); got != "8613800138000" {
		t.Fatalf("metadata[from] = %q", got)
	}
}

func TestGenericBridgePolicyBlocksGroupMessageWithoutMention(t *testing.T) {
	t.Parallel()

	adapter := &bridgeStubAdapter{inbound: make(chan InboundMessage, 1)}
	runtime := &bridgeStubRuntime{run: &agent.Run{ID: "run-mm"}}
	bridge := NewBridge(BridgeConfig{
		ChannelName:  "mattermost",
		TargetIDKey:  "channel_id",
		MessageIDKey: "post_id",
		ThreadIDKey:  "root_id",
	}, adapter, runtime, agent.NewInMemorySessionStore(), nil, DefaultStatusReminderDelay).
		WithPolicy(PolicyConfig{
			GroupPolicy:    "open",
			RequireMention: true,
		})

	bridge.handleInbound(context.Background(), InboundMessage{
		ChannelID: "mattermost",
		SenderID:  "user-1",
		Content:   "hello room",
		RawEvent: map[string]any{
			"channel_id":   "town-square",
			"post_id":      "post-1",
			"channel_type": "O",
		},
	})

	if runtime.Submitted() != nil {
		t.Fatal("expected message to be blocked by policy")
	}
}

func TestGenericBridgeNormalizesChatTypeFromRoomIDWithoutChannelNameGuessing(t *testing.T) {
	t.Parallel()

	adapter := &bridgeStubAdapter{inbound: make(chan InboundMessage, 1)}
	runtime := &bridgeStubRuntime{run: &agent.Run{ID: "run-group-chat"}}
	bridge := NewBridge(BridgeConfig{
		ChannelName:  "custombridge",
		TargetIDKey:  "room_id",
		MessageIDKey: "event_id",
	}, adapter, runtime, agent.NewInMemorySessionStore(), nil, DefaultStatusReminderDelay)

	bridge.handleInbound(context.Background(), InboundMessage{
		ChannelID: "custombridge",
		SenderID:  "@alice:example.org",
		Content:   "hello room",
		RawEvent: map[string]any{
			"room_id":  "!room:example.org",
			"event_id": "$event-1",
		},
	})

	submitted := runtime.Submitted()
	if submitted == nil {
		t.Fatal("expected runtime submission")
	}
	if got := submitted.Metadata["chat_type"]; got != "group" {
		t.Fatalf("chat_type = %#v, want %q", got, "group")
	}
	if got := submitted.Metadata[meta.KeyChannelName]; got != "custombridge" {
		t.Fatalf("channel_name = %#v, want %q", got, "custombridge")
	}
}

func TestGenericBridgeBindCommandUsesThreadBinding(t *testing.T) {
	t.Parallel()

	adapter := &bridgeStubAdapter{inbound: make(chan InboundMessage, 1)}
	runtime := &bridgeStubRuntime{}
	threadBindings := NewThreadBinding()
	bridge := NewBridge(BridgeConfig{
		ChannelName:  "mattermost",
		TargetIDKey:  "channel_id",
		MessageIDKey: "post_id",
		ThreadIDKey:  "root_id",
	}, adapter, runtime, agent.NewInMemorySessionStore(), nil, DefaultStatusReminderDelay).
		WithThreadBindings(threadBindings)

	bridge.handleInbound(context.Background(), InboundMessage{
		ChannelID: "mattermost",
		SenderID:  "user-1",
		Content:   "/bind",
		RawEvent: map[string]any{
			"channel_id":   "town-square",
			"post_id":      "post-2",
			"root_id":      "thread-2",
			"channel_type": "O",
		},
	})

	if sessionKey, ok := threadBindings.Resolve("mattermost", "thread-2"); !ok || sessionKey != "mattermost:town-square" {
		t.Fatalf("thread binding = %q, ok=%v", sessionKey, ok)
	}
	if len(adapter.Messages()) != 1 {
		t.Fatalf("len(adapter.Messages()) = %d", len(adapter.Messages()))
	}
}

func TestGenericBridgeDropsDuplicateInboundMessage(t *testing.T) {
	t.Parallel()

	adapter := &bridgeStubAdapter{inbound: make(chan InboundMessage, 1)}
	runtime := &bridgeStubRuntime{run: &agent.Run{ID: "run-1"}}
	bridge := NewBridge(BridgeConfig{
		ChannelName:  "whatsapp",
		TargetIDKey:  "from",
		MessageIDKey: "message_id",
	}, adapter, runtime, agent.NewInMemorySessionStore(), nil, DefaultStatusReminderDelay).
		WithMessageDeduper(NewMessageDeduper("", time.Hour))

	msg := InboundMessage{
		ChannelID: "whatsapp",
		SenderID:  "8613800138000",
		Content:   "hello",
		RawEvent: map[string]any{
			"from":       "8613800138000",
			"message_id": "wamid-dup-1",
		},
	}
	bridge.handleInbound(context.Background(), msg)
	bridge.handleInbound(context.Background(), msg)

	if got := runtime.Submits(); got != 1 {
		t.Fatalf("runtime submits = %d, want 1", got)
	}
}

func TestGenericBridgeNormalizesConversationMetadata(t *testing.T) {
	t.Parallel()

	adapter := &bridgeStubAdapter{inbound: make(chan InboundMessage, 1)}
	runtime := &bridgeStubRuntime{run: &agent.Run{ID: "run-conv"}}
	bridge := NewBridge(BridgeConfig{
		ChannelName:  "webhook",
		TargetIDKey:  "channel_id",
		MessageIDKey: "event_id",
	}, adapter, runtime, agent.NewInMemorySessionStore(), nil, DefaultStatusReminderDelay)

	bridge.handleInbound(context.Background(), InboundMessage{
		ChannelID:  "webhook",
		SenderID:   "user-42",
		SenderName: "Alice",
		Content:    "hello",
		RawEvent: map[string]any{
			"channel_id":          "room-1",
			"event_id":            "evt-1",
			"reply_to_message_id": "evt-parent",
			"thread_name":         "thread-7",
		},
	})

	submitted := runtime.Submitted()
	if submitted == nil {
		t.Fatal("expected submit request")
	}
	if got := submitted.Metadata[meta.KeyMessageID]; got != "evt-1" {
		t.Fatalf("message_id = %v, want evt-1", got)
	}
	if got := submitted.Metadata[meta.KeyReplyToID]; got != "evt-parent" {
		t.Fatalf("reply_to_id = %v, want evt-parent", got)
	}
	if got := submitted.Metadata[meta.KeyThreadID]; got != "thread-7" {
		t.Fatalf("thread_id = %v, want thread-7", got)
	}
	if got := submitted.Metadata[meta.KeySenderName]; got != "Alice" {
		t.Fatalf("sender_name = %v, want Alice", got)
	}
}

func TestGenericBridgeInjectsChannelCapabilityMetadata(t *testing.T) {
	t.Parallel()

	adapter := &bridgeStubAdapter{
		inbound: make(chan InboundMessage, 1),
		caps: Capabilities{
			Interactive:    true,
			Mobile:         true,
			InlineDelivery: true,
		},
	}
	runtime := &bridgeStubRuntime{run: &agent.Run{ID: "run-caps"}}
	bridge := NewBridge(BridgeConfig{
		ChannelName:  "whatsapp",
		TargetIDKey:  "from",
		MessageIDKey: "message_id",
	}, adapter, runtime, agent.NewInMemorySessionStore(), nil, DefaultStatusReminderDelay)

	bridge.handleInbound(context.Background(), InboundMessage{
		ChannelID: "whatsapp",
		SenderID:  "8613800138000",
		Content:   "hello",
		RawEvent: map[string]any{
			"from":       "8613800138000",
			"message_id": "wamid-cap-1",
		},
	})

	submitted := runtime.Submitted()
	if submitted == nil {
		t.Fatal("expected submit request")
	}
	if got := submitted.Metadata[meta.KeyChannelInteractive]; got != true {
		t.Fatalf("channel_interactive = %#v, want true", got)
	}
	if got := submitted.Metadata[meta.KeyChannelMobile]; got != true {
		t.Fatalf("channel_mobile = %#v, want true", got)
	}
	if got := submitted.Metadata[meta.KeyChannelInlineDelivery]; got != true {
		t.Fatalf("channel_inline_delivery = %#v, want true", got)
	}
	rawCaps, ok := submitted.Metadata[meta.KeyChannelCapabilities].(map[string]any)
	if !ok {
		t.Fatalf("channel_capabilities = %#v, want map", submitted.Metadata[meta.KeyChannelCapabilities])
	}
	if got := rawCaps["interactive"]; got != true {
		t.Fatalf("channel_capabilities.interactive = %#v, want true", got)
	}
	if got := rawCaps["mobile"]; got != true {
		t.Fatalf("channel_capabilities.mobile = %#v, want true", got)
	}
	if got := rawCaps["inline_delivery"]; got != true {
		t.Fatalf("channel_capabilities.inline_delivery = %#v, want true", got)
	}
}

func TestGenericBridgeRepliesUsingStoredTargetMetadata(t *testing.T) {
	t.Parallel()

	store := agent.NewInMemorySessionStore()
	session, err := store.GetOrCreate(context.Background(), "whatsapp:8613800138000", "test-model")
	if err != nil {
		t.Fatalf("GetOrCreate() error = %v", err)
	}
	locked, unlock, err := store.LoadForExecution(context.Background(), session.ID)
	if err != nil {
		t.Fatalf("LoadForExecution() error = %v", err)
	}
	now := time.Now().UTC()
	locked.Messages = append(locked.Messages,
		contextengine.Message{
			Role:      contextengine.RoleUser,
			Content:   "hello",
			CreatedAt: now,
			Metadata: map[string]any{
				meta.KeyChannel: "whatsapp",
				"from":          "8613800138000",
				"message_id":    "wamid-1",
			},
		},
		contextengine.Message{
			Role:      contextengine.RoleAssistant,
			Content:   "done",
			CreatedAt: now.Add(time.Second),
			Metadata:  map[string]any{"run_id": "run-1"},
		},
	)
	if err := store.Save(context.Background(), locked); err != nil {
		unlock()
		t.Fatalf("Save() error = %v", err)
	}
	unlock()

	adapter := &bridgeStubAdapter{inbound: make(chan InboundMessage)}
	runtime := &bridgeStubRuntime{run: &agent.Run{
		ID:           "run-1",
		SessionID:    session.ID,
		InputEventID: "wamid-1",
	}}
	bridge := NewBridge(BridgeConfig{
		ChannelName:  "whatsapp",
		TargetIDKey:  "from",
		MessageIDKey: "message_id",
	}, adapter, runtime, store, nil, DefaultStatusReminderDelay)

	bridge.handleTerminalRun(context.Background(), eventbus.Event{
		Type:      eventbus.EventRunCompleted,
		RunID:     "run-1",
		SessionID: session.ID,
	})

	if len(adapter.sent) != 1 {
		t.Fatalf("len(adapter.sent) = %d", len(adapter.sent))
	}
	if adapter.sent[0].TargetID != "8613800138000" {
		t.Fatalf("TargetID = %q", adapter.sent[0].TargetID)
	}
}

func TestGenericBridgeRepliesUsingStoredChannelMetadataWithoutSessionKeyPrefix(t *testing.T) {
	t.Parallel()

	store := agent.NewInMemorySessionStore()
	session, err := store.GetOrCreate(context.Background(), "opaque-session", "test-model")
	if err != nil {
		t.Fatalf("GetOrCreate() error = %v", err)
	}
	locked, unlock, err := store.LoadForExecution(context.Background(), session.ID)
	if err != nil {
		t.Fatalf("LoadForExecution() error = %v", err)
	}
	now := time.Now().UTC()
	locked.Messages = append(locked.Messages,
		contextengine.Message{
			Role:      contextengine.RoleUser,
			Content:   "hello",
			CreatedAt: now,
			Metadata: map[string]any{
				meta.KeyChannel: "whatsapp",
				"from":          "8613800138000",
				"message_id":    "wamid-opaque-1",
			},
		},
		contextengine.Message{
			Role:      contextengine.RoleAssistant,
			Content:   "done",
			CreatedAt: now.Add(time.Second),
			Metadata:  map[string]any{meta.KeyRunID: "run-opaque-1"},
		},
	)
	if err := store.Save(context.Background(), locked); err != nil {
		unlock()
		t.Fatalf("Save() error = %v", err)
	}
	unlock()

	adapter := &bridgeStubAdapter{inbound: make(chan InboundMessage)}
	runtime := &bridgeStubRuntime{run: &agent.Run{
		ID:           "run-opaque-1",
		SessionID:    session.ID,
		InputEventID: "wamid-opaque-1",
	}}
	bridge := NewBridge(BridgeConfig{
		ChannelName:  "whatsapp",
		TargetIDKey:  "from",
		MessageIDKey: "message_id",
	}, adapter, runtime, store, nil, DefaultStatusReminderDelay)

	bridge.handleTerminalRun(context.Background(), eventbus.Event{
		Type:      eventbus.EventRunCompleted,
		RunID:     "run-opaque-1",
		SessionID: session.ID,
	})

	if len(adapter.sent) != 1 {
		t.Fatalf("len(adapter.sent) = %d", len(adapter.sent))
	}
	if adapter.sent[0].TargetID != "8613800138000" {
		t.Fatalf("TargetID = %q", adapter.sent[0].TargetID)
	}
}

func TestGenericBridgePrefersStructuredRunResultOutput(t *testing.T) {
	t.Parallel()

	store := agent.NewInMemorySessionStore()
	session, err := store.GetOrCreate(context.Background(), "whatsapp:8613800138000", "test-model")
	if err != nil {
		t.Fatalf("GetOrCreate() error = %v", err)
	}
	locked, unlock, err := store.LoadForExecution(context.Background(), session.ID)
	if err != nil {
		t.Fatalf("LoadForExecution() error = %v", err)
	}
	now := time.Now().UTC()
	locked.Messages = append(locked.Messages,
		contextengine.Message{
			Role:      contextengine.RoleUser,
			Content:   "hello",
			CreatedAt: now,
			Metadata: map[string]any{
				meta.KeyChannel: "whatsapp",
				"from":          "8613800138000",
				"message_id":    "wamid-structured-1",
			},
		},
		contextengine.Message{
			Role:      contextengine.RoleAssistant,
			Content:   "stale message",
			CreatedAt: now.Add(time.Second),
			Metadata:  map[string]any{"run_id": "run-structured-1"},
		},
	)
	if err := store.Save(context.Background(), locked); err != nil {
		unlock()
		t.Fatalf("Save() error = %v", err)
	}
	unlock()

	adapter := &bridgeStubAdapter{inbound: make(chan InboundMessage)}
	runtime := &bridgeStubRuntime{
		run: &agent.Run{
			ID:           "run-structured-1",
			SessionID:    session.ID,
			InputEventID: "wamid-structured-1",
			Status:       agent.RunCompleted,
		},
		runResult: &runtimesvc.RunResult{
			RunID:  "run-structured-1",
			Output: "fresh structured output",
		},
	}
	bridge := NewBridge(BridgeConfig{
		ChannelName:  "whatsapp",
		TargetIDKey:  "from",
		MessageIDKey: "message_id",
	}, adapter, runtime, store, nil, DefaultStatusReminderDelay)

	bridge.handleTerminalRun(context.Background(), eventbus.Event{
		Type:      eventbus.EventRunCompleted,
		RunID:     "run-structured-1",
		SessionID: session.ID,
	})

	sent := adapter.Messages()
	if len(sent) != 1 {
		t.Fatalf("len(sent) = %d", len(sent))
	}
	if sent[0].Content != "fresh structured output" {
		t.Fatalf("Content = %q, want %q", sent[0].Content, "fresh structured output")
	}
}

func TestGenericBridgeMarksPartialOnCompletedRun(t *testing.T) {
	t.Parallel()

	store := agent.NewInMemorySessionStore()
	session, err := store.GetOrCreate(context.Background(), "whatsapp:8613800138000", "test-model")
	if err != nil {
		t.Fatalf("GetOrCreate() error = %v", err)
	}
	locked, unlock, err := store.LoadForExecution(context.Background(), session.ID)
	if err != nil {
		t.Fatalf("LoadForExecution() error = %v", err)
	}
	now := time.Now().UTC()
	locked.Messages = append(locked.Messages, contextengine.Message{
		Role:      contextengine.RoleUser,
		Content:   "hello",
		CreatedAt: now,
		Metadata: map[string]any{
			meta.KeyChannel: "whatsapp",
			"from":          "8613800138000",
			"message_id":    "wamid-warning-1",
		},
	})
	if err := store.Save(context.Background(), locked); err != nil {
		unlock()
		t.Fatalf("Save() error = %v", err)
	}
	unlock()

	adapter := &bridgeStubAdapter{inbound: make(chan InboundMessage)}
	runtime := &bridgeStubRuntime{
		run: &agent.Run{
			ID:           "run-warning-1",
			SessionID:    session.ID,
			InputEventID: "wamid-warning-1",
			Status:       agent.RunCompleted,
		},
		runResult: &runtimesvc.RunResult{
			RunID:   "run-warning-1",
			Output:  "fresh structured output",
			Outcome: runtimesvc.RunOutcomePartial,
		},
		runVerification: &verifyrt.RunVerification{
			RunID:   "run-warning-1",
			Status:  verifyrt.StatusWarning,
			Summary: "verification finished with 1 required warning",
		},
	}
	bridge := NewBridge(BridgeConfig{
		ChannelName:  "whatsapp",
		TargetIDKey:  "from",
		MessageIDKey: "message_id",
	}, adapter, runtime, store, nil, DefaultStatusReminderDelay)

	bridge.handleTerminalRun(context.Background(), eventbus.Event{
		Type:      eventbus.EventRunCompleted,
		RunID:     "run-warning-1",
		SessionID: session.ID,
	})

	sent := adapter.Messages()
	if len(sent) != 1 {
		t.Fatalf("len(sent) = %d", len(sent))
	}
	if got := sent[0].Metadata["status_kind"]; got != "partial" {
		t.Fatalf("status_kind = %#v", got)
	}
	if !strings.Contains(sent[0].Content, "part of this delivery") && !strings.Contains(sent[0].Content, "部分没有完全确认") {
		t.Fatalf("content = %q", sent[0].Content)
	}
	if !strings.Contains(sent[0].Content, "fresh structured output") {
		t.Fatalf("content = %q", sent[0].Content)
	}
	if got := sent[0].Metadata["outcome"]; got != "partial" {
		t.Fatalf("outcome = %#v", got)
	}
}

func TestGenericBridgeTurnsCompletedRunIntoVerificationFailureMessage(t *testing.T) {
	t.Parallel()

	store := agent.NewInMemorySessionStore()
	session, err := store.GetOrCreate(context.Background(), "whatsapp:8613800138000", "test-model")
	if err != nil {
		t.Fatalf("GetOrCreate() error = %v", err)
	}
	locked, unlock, err := store.LoadForExecution(context.Background(), session.ID)
	if err != nil {
		t.Fatalf("LoadForExecution() error = %v", err)
	}
	now := time.Now().UTC()
	locked.Messages = append(locked.Messages, contextengine.Message{
		Role:      contextengine.RoleUser,
		Content:   "hello",
		CreatedAt: now,
		Metadata: map[string]any{
			meta.KeyChannel: "whatsapp",
			"from":          "8613800138000",
			"message_id":    "wamid-verify-fail-1",
		},
	})
	if err := store.Save(context.Background(), locked); err != nil {
		unlock()
		t.Fatalf("Save() error = %v", err)
	}
	unlock()

	adapter := &bridgeStubAdapter{inbound: make(chan InboundMessage)}
	runtime := &bridgeStubRuntime{
		run: &agent.Run{
			ID:           "run-verify-fail-1",
			SessionID:    session.ID,
			InputEventID: "wamid-verify-fail-1",
			Status:       agent.RunCompleted,
		},
		runResult: &runtimesvc.RunResult{
			RunID:  "run-verify-fail-1",
			Output: "fresh structured output",
		},
		runVerification: &verifyrt.RunVerification{
			RunID:            "run-verify-fail-1",
			Status:           verifyrt.StatusFailed,
			Summary:          "verification blocked delivery: 1 blocking check did not pass",
			BlockingFailures: 1,
		},
	}
	bridge := NewBridge(BridgeConfig{
		ChannelName:  "whatsapp",
		TargetIDKey:  "from",
		MessageIDKey: "message_id",
	}, adapter, runtime, store, nil, DefaultStatusReminderDelay)

	bridge.handleTerminalRun(context.Background(), eventbus.Event{
		Type:      eventbus.EventRunCompleted,
		RunID:     "run-verify-fail-1",
		SessionID: session.ID,
	})

	sent := adapter.Messages()
	if len(sent) != 1 {
		t.Fatalf("len(sent) = %d", len(sent))
	}
	if got := sent[0].Metadata["status_kind"]; got != "verification_failed" {
		t.Fatalf("status_kind = %#v", got)
	}
	if !strings.Contains(sent[0].Content, "did not pass verification") {
		t.Fatalf("content = %q", sent[0].Content)
	}
	if strings.Contains(sent[0].Content, "fresh structured output") {
		t.Fatalf("content = %q", sent[0].Content)
	}
}

func TestGenericBridgeNotifiesSubmitFailure(t *testing.T) {
	t.Parallel()

	adapter := &bridgeStubAdapter{inbound: make(chan InboundMessage, 1)}
	runtime := &bridgeStubRuntime{submitErr: errors.New("backend unavailable")}
	bridge := NewBridge(BridgeConfig{
		ChannelName:  "whatsapp",
		TargetIDKey:  "from",
		MessageIDKey: "message_id",
	}, adapter, runtime, agent.NewInMemorySessionStore(), nil, DefaultStatusReminderDelay)

	bridge.handleInbound(context.Background(), InboundMessage{
		ChannelID: "whatsapp",
		SenderID:  "8613800138000",
		Content:   "帮我处理一下",
		RawEvent: map[string]any{
			"message_id": "wamid-2",
		},
	})

	sent := adapter.Messages()
	if len(sent) != 1 {
		t.Fatalf("len(sent) = %d", len(sent))
	}
	if sent[0].TargetID != "8613800138000" {
		t.Fatalf("TargetID = %q", sent[0].TargetID)
	}
	if !strings.Contains(sent[0].Content, "没有成功启动") {
		t.Fatalf("unexpected submit failure content: %q", sent[0].Content)
	}
}

func TestGenericBridgeNotifiesPreflightOnSubmit(t *testing.T) {
	t.Parallel()

	adapter := &bridgeStubAdapter{inbound: make(chan InboundMessage, 1)}
	runtime := &bridgeStubRuntime{submitRun: &agent.Run{
		ID: "run-preflight-submit",
		Preflight: &agent.RunPreflightReport{
			State:    agent.RunPreflightNeedsConfirmation,
			Summary:  "The request refers to an existing file, URL, screenshot, or repo, but no concrete reference was included.",
			Blocking: true,
		},
	}}
	bridge := NewBridge(BridgeConfig{
		ChannelName:  "whatsapp",
		TargetIDKey:  "from",
		MessageIDKey: "message_id",
	}, adapter, runtime, agent.NewInMemorySessionStore(), nil, DefaultStatusReminderDelay)

	bridge.handleInbound(context.Background(), InboundMessage{
		ChannelID: "whatsapp",
		SenderID:  "8613800138000",
		Content:   "把这个文件改一下",
		RawEvent: map[string]any{
			"message_id": "wamid-preflight-submit",
		},
	})

	waitForBridgeMessages(t, adapter, 1)
	sent := adapter.Messages()
	if len(sent) != 1 {
		t.Fatalf("len(sent) = %d", len(sent))
	}
	if got := sent[0].Metadata["status_kind"]; got != "preflight_needs_confirmation" {
		t.Fatalf("status_kind = %#v", got)
	}
	if !strings.Contains(sent[0].Content, "前置条件") {
		t.Fatalf("content = %q", sent[0].Content)
	}
}

func TestGenericBridgeVerifiesCompletedLocalServiceBeforeDelivery(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	}))
	defer server.Close()

	store := agent.NewInMemorySessionStore()
	session, err := store.GetOrCreate(context.Background(), "whatsapp:8613800138000", "test-model")
	if err != nil {
		t.Fatalf("GetOrCreate() error = %v", err)
	}
	locked, unlock, err := store.LoadForExecution(context.Background(), session.ID)
	if err != nil {
		t.Fatalf("LoadForExecution() error = %v", err)
	}
	now := time.Now().UTC()
	locked.Messages = append(locked.Messages,
		contextengine.Message{
			Role:      contextengine.RoleUser,
			Content:   "帮我启动网站",
			CreatedAt: now,
			Metadata: map[string]any{
				meta.KeyChannel: "whatsapp",
				"from":          "8613800138000",
				"message_id":    "wamid-service-ok",
			},
		},
		contextengine.Message{
			Role:      contextengine.RoleAssistant,
			Content:   "网站已经启动，可以访问 " + server.URL,
			CreatedAt: now.Add(time.Second),
			Metadata:  map[string]any{"run_id": "run-service-ok"},
		},
	)
	if err := store.Save(context.Background(), locked); err != nil {
		unlock()
		t.Fatalf("Save() error = %v", err)
	}
	unlock()

	adapter := &bridgeStubAdapter{inbound: make(chan InboundMessage)}
	runtime := &bridgeStubRuntime{run: &agent.Run{
		ID:           "run-service-ok",
		SessionID:    session.ID,
		InputEventID: "wamid-service-ok",
		Status:       agent.RunCompleted,
	}}
	bridge := NewBridge(BridgeConfig{
		ChannelName:  "whatsapp",
		TargetIDKey:  "from",
		MessageIDKey: "message_id",
	}, adapter, runtime, store, nil, DefaultStatusReminderDelay)

	bridge.handleTerminalRun(context.Background(), eventbus.Event{
		Type:      eventbus.EventRunCompleted,
		RunID:     "run-service-ok",
		SessionID: session.ID,
	})

	if runtime.Submits() != 0 {
		t.Fatalf("runtime.Submits() = %d, want 0", runtime.Submits())
	}
	sent := adapter.Messages()
	if len(sent) != 1 {
		t.Fatalf("len(sent) = %d", len(sent))
	}
	if sent[0].Content != "网站已经启动，可以访问 "+server.URL {
		t.Fatalf("content = %q", sent[0].Content)
	}
}

func TestGenericBridgeAutoRepairsBrokenCompletedLocalService(t *testing.T) {
	t.Parallel()

	store := agent.NewInMemorySessionStore()
	session, err := store.GetOrCreate(context.Background(), "whatsapp:8613800138000", "test-model")
	if err != nil {
		t.Fatalf("GetOrCreate() error = %v", err)
	}
	locked, unlock, err := store.LoadForExecution(context.Background(), session.ID)
	if err != nil {
		t.Fatalf("LoadForExecution() error = %v", err)
	}
	now := time.Now().UTC()
	locked.Messages = append(locked.Messages,
		contextengine.Message{
			Role:      contextengine.RoleUser,
			Content:   "帮我启动网站",
			CreatedAt: now,
			Metadata: map[string]any{
				meta.KeyChannel: "whatsapp",
				"from":          "8613800138000",
				"message_id":    "wamid-service-bad",
			},
		},
		contextengine.Message{
			Role:      contextengine.RoleAssistant,
			Content:   "网站已经启动，可以访问 http://127.0.0.1:1",
			CreatedAt: now.Add(time.Second),
			Metadata:  map[string]any{"run_id": "run-service-bad"},
		},
	)
	if err := store.Save(context.Background(), locked); err != nil {
		unlock()
		t.Fatalf("Save() error = %v", err)
	}
	unlock()

	runtime := &bridgeStubRuntime{
		run: &agent.Run{
			ID:           "run-service-bad",
			SessionID:    session.ID,
			InputEventID: "wamid-service-bad",
			Status:       agent.RunCompleted,
		},
		submitRun: &agent.Run{
			ID:           "run-repair-1",
			SessionID:    session.ID,
			InputEventID: verificationRepairEventID("run-service-bad"),
			Status:       agent.RunQueued,
		},
	}
	adapter := &bridgeStubAdapter{inbound: make(chan InboundMessage)}
	bridge := NewBridge(BridgeConfig{
		ChannelName:  "whatsapp",
		TargetIDKey:  "from",
		MessageIDKey: "message_id",
	}, adapter, runtime, store, nil, DefaultStatusReminderDelay)

	bridge.handleTerminalRun(context.Background(), eventbus.Event{
		Type:      eventbus.EventRunCompleted,
		RunID:     "run-service-bad",
		SessionID: session.ID,
	})

	if runtime.Submits() != 1 {
		t.Fatalf("runtime.Submits() = %d, want 1", runtime.Submits())
	}
	if runtime.Submitted() == nil || runtime.Submitted().ExternalEventID != verificationRepairEventID("run-service-bad") {
		t.Fatalf("runtime.Submitted() = %#v", runtime.Submitted())
	}
	if !strings.Contains(runtime.Submitted().Content, "Verification failures") {
		t.Fatalf("repair content = %q", runtime.Submitted().Content)
	}
	sent := adapter.Messages()
	if len(sent) != 1 {
		t.Fatalf("len(sent) = %d", len(sent))
	}
	if got := sent[0].Metadata["status_kind"]; got != "verification_repair_started" {
		t.Fatalf("status_kind = %#v", got)
	}
	if !strings.Contains(sent[0].Content, "还不能确认成功") {
		t.Fatalf("content = %q", sent[0].Content)
	}
}

func TestGenericBridgeDoesNotLoopVerificationRepair(t *testing.T) {
	t.Parallel()

	store := agent.NewInMemorySessionStore()
	session, err := store.GetOrCreate(context.Background(), "whatsapp:8613800138000", "test-model")
	if err != nil {
		t.Fatalf("GetOrCreate() error = %v", err)
	}
	locked, unlock, err := store.LoadForExecution(context.Background(), session.ID)
	if err != nil {
		t.Fatalf("LoadForExecution() error = %v", err)
	}
	now := time.Now().UTC()
	locked.Messages = append(locked.Messages,
		contextengine.Message{
			Role:      contextengine.RoleUser,
			Content:   "帮我启动网站",
			CreatedAt: now,
			Metadata: map[string]any{
				meta.KeyChannel: "whatsapp",
				"from":          "8613800138000",
				"message_id":    verificationRepairEventID("run-original"),
			},
		},
		contextengine.Message{
			Role:      contextengine.RoleAssistant,
			Content:   "还是可以访问 http://127.0.0.1:1",
			CreatedAt: now.Add(time.Second),
			Metadata:  map[string]any{"run_id": "run-repair-terminal"},
		},
	)
	if err := store.Save(context.Background(), locked); err != nil {
		unlock()
		t.Fatalf("Save() error = %v", err)
	}
	unlock()

	runtime := &bridgeStubRuntime{run: &agent.Run{
		ID:           "run-repair-terminal",
		SessionID:    session.ID,
		InputEventID: verificationRepairEventID("run-original"),
		Status:       agent.RunCompleted,
	}}
	adapter := &bridgeStubAdapter{inbound: make(chan InboundMessage)}
	bridge := NewBridge(BridgeConfig{
		ChannelName:  "whatsapp",
		TargetIDKey:  "from",
		MessageIDKey: "message_id",
	}, adapter, runtime, store, nil, DefaultStatusReminderDelay)

	bridge.handleTerminalRun(context.Background(), eventbus.Event{
		Type:      eventbus.EventRunCompleted,
		RunID:     "run-repair-terminal",
		SessionID: session.ID,
	})

	if runtime.Submits() != 0 {
		t.Fatalf("runtime.Submits() = %d, want 0", runtime.Submits())
	}
	sent := adapter.Messages()
	if len(sent) != 1 {
		t.Fatalf("len(sent) = %d", len(sent))
	}
	if got := sent[0].Metadata["status_kind"]; got != "verification_failed" {
		t.Fatalf("status_kind = %#v", got)
	}
	if !strings.Contains(sent[0].Content, "没有验证通过") {
		t.Fatalf("content = %q", sent[0].Content)
	}
}

func TestGenericBridgeStandardizesFileDeliverablesOnCompletedRun(t *testing.T) {
	t.Parallel()

	workspace := t.TempDir()
	if err := os.WriteFile(filepath.Join(workspace, "travel-website-screenshot.png"), []byte("png"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	store := agent.NewInMemorySessionStore()
	session, err := store.GetOrCreate(context.Background(), "whatsapp:8613800138000", "test-model")
	if err != nil {
		t.Fatalf("GetOrCreate() error = %v", err)
	}
	locked, unlock, err := store.LoadForExecution(context.Background(), session.ID)
	if err != nil {
		t.Fatalf("LoadForExecution() error = %v", err)
	}
	now := time.Now().UTC()
	locked.Messages = append(locked.Messages,
		contextengine.Message{
			Role:      contextengine.RoleUser,
			Content:   "帮我生成截图",
			CreatedAt: now,
			Metadata: map[string]any{
				meta.KeyChannel: "whatsapp",
				"from":          "8613800138000",
				"message_id":    "wamid-file-ok",
			},
		},
		contextengine.Message{
			Role:    contextengine.RoleAssistant,
			Content: "",
			ToolCalls: []contextengine.ToolCallRef{
				{ID: "call-file-ok", Name: "fs.write", Arguments: `{"path":"travel-website-screenshot.png"}`},
			},
			CreatedAt: now.Add(time.Second),
			Metadata:  map[string]any{"run_id": "run-file-ok"},
		},
		contextengine.Message{
			Role:       contextengine.RoleTool,
			Name:       "fs.write",
			ToolCallID: "call-file-ok",
			Content:    `{"path":"travel-website-screenshot.png","workspace":"` + filepath.ToSlash(workspace) + `","bytes_written":3,"append":false,"message":"wrote file"}`,
			CreatedAt:  now.Add(2 * time.Second),
		},
		contextengine.Message{
			Role:      contextengine.RoleAssistant,
			Content:   "截图已经保存。",
			CreatedAt: now.Add(3 * time.Second),
			Metadata:  map[string]any{"run_id": "run-file-ok"},
		},
	)
	if err := store.Save(context.Background(), locked); err != nil {
		unlock()
		t.Fatalf("Save() error = %v", err)
	}
	unlock()

	runtime := &bridgeStubRuntime{run: &agent.Run{
		ID:           "run-file-ok",
		SessionID:    session.ID,
		InputEventID: "wamid-file-ok",
		Status:       agent.RunCompleted,
	}}
	adapter := &bridgeStubAdapter{inbound: make(chan InboundMessage)}
	bridge := NewBridge(BridgeConfig{
		ChannelName:  "whatsapp",
		TargetIDKey:  "from",
		MessageIDKey: "message_id",
	}, adapter, runtime, store, nil, DefaultStatusReminderDelay)

	bridge.handleTerminalRun(context.Background(), eventbus.Event{
		Type:      eventbus.EventRunCompleted,
		RunID:     "run-file-ok",
		SessionID: session.ID,
	})

	if runtime.Submits() != 0 {
		t.Fatalf("runtime.Submits() = %d, want 0", runtime.Submits())
	}
	sent := adapter.Messages()
	if len(sent) != 1 {
		t.Fatalf("len(sent) = %d", len(sent))
	}
	if !strings.Contains(sent[0].Content, "交付物：") {
		t.Fatalf("content = %q", sent[0].Content)
	}
	if !strings.Contains(sent[0].Content, "travel-website-screenshot.png") {
		t.Fatalf("content = %q", sent[0].Content)
	}
}

func TestGenericBridgeAutoRepairsMissingFileDeliverable(t *testing.T) {
	t.Parallel()

	workspace := t.TempDir()

	store := agent.NewInMemorySessionStore()
	session, err := store.GetOrCreate(context.Background(), "whatsapp:8613800138000", "test-model")
	if err != nil {
		t.Fatalf("GetOrCreate() error = %v", err)
	}
	locked, unlock, err := store.LoadForExecution(context.Background(), session.ID)
	if err != nil {
		t.Fatalf("LoadForExecution() error = %v", err)
	}
	now := time.Now().UTC()
	locked.Messages = append(locked.Messages,
		contextengine.Message{
			Role:      contextengine.RoleUser,
			Content:   "帮我导出 html",
			CreatedAt: now,
			Metadata: map[string]any{
				meta.KeyChannel: "whatsapp",
				"from":          "8613800138000",
				"message_id":    "wamid-file-missing",
			},
		},
		contextengine.Message{
			Role:    contextengine.RoleAssistant,
			Content: "",
			ToolCalls: []contextengine.ToolCallRef{
				{ID: "call-file-missing", Name: "fs.write", Arguments: `{"path":"dist/index.html"}`},
			},
			CreatedAt: now.Add(time.Second),
			Metadata:  map[string]any{"run_id": "run-file-missing"},
		},
		contextengine.Message{
			Role:       contextengine.RoleTool,
			Name:       "fs.write",
			ToolCallID: "call-file-missing",
			Content:    `{"path":"dist/index.html","workspace":"` + filepath.ToSlash(workspace) + `","bytes_written":10,"append":false,"message":"wrote file"}`,
			CreatedAt:  now.Add(2 * time.Second),
		},
		contextengine.Message{
			Role:      contextengine.RoleAssistant,
			Content:   "HTML 已经导出。",
			CreatedAt: now.Add(3 * time.Second),
			Metadata:  map[string]any{"run_id": "run-file-missing"},
		},
	)
	if err := store.Save(context.Background(), locked); err != nil {
		unlock()
		t.Fatalf("Save() error = %v", err)
	}
	unlock()

	runtime := &bridgeStubRuntime{
		run: &agent.Run{
			ID:           "run-file-missing",
			SessionID:    session.ID,
			InputEventID: "wamid-file-missing",
			Status:       agent.RunCompleted,
		},
		submitRun: &agent.Run{
			ID:           "run-file-repair",
			SessionID:    session.ID,
			InputEventID: verificationRepairEventID("run-file-missing"),
			Status:       agent.RunQueued,
		},
	}
	adapter := &bridgeStubAdapter{inbound: make(chan InboundMessage)}
	bridge := NewBridge(BridgeConfig{
		ChannelName:  "whatsapp",
		TargetIDKey:  "from",
		MessageIDKey: "message_id",
	}, adapter, runtime, store, nil, DefaultStatusReminderDelay)

	bridge.handleTerminalRun(context.Background(), eventbus.Event{
		Type:      eventbus.EventRunCompleted,
		RunID:     "run-file-missing",
		SessionID: session.ID,
	})

	if runtime.Submits() != 1 {
		t.Fatalf("runtime.Submits() = %d, want 1", runtime.Submits())
	}
	if runtime.Submitted() == nil || runtime.Submitted().ExternalEventID != verificationRepairEventID("run-file-missing") {
		t.Fatalf("runtime.Submitted() = %#v", runtime.Submitted())
	}
	sent := adapter.Messages()
	if len(sent) != 1 {
		t.Fatalf("len(sent) = %d", len(sent))
	}
	if got := sent[0].Metadata["status_kind"]; got != "verification_repair_started" {
		t.Fatalf("status_kind = %#v", got)
	}
	if !strings.Contains(sent[0].Content, "还不能确认成功") {
		t.Fatalf("content = %q", sent[0].Content)
	}
}

// TestGenericBridgeApprovalStatusDelivered verifies that approval waits are
// surfaced to the channel user while internal status tracking still updates.
func TestGenericBridgeApprovalStatusDelivered(t *testing.T) {
	t.Parallel()

	adapter := &bridgeStubAdapter{inbound: make(chan InboundMessage, 1)}
	runtime := &bridgeStubRuntime{run: &agent.Run{ID: "run-2"}}
	bus := eventbus.NewInMemoryBus()
	bridge := NewBridge(BridgeConfig{
		ChannelName:  "whatsapp",
		TargetIDKey:  "from",
		MessageIDKey: "message_id",
	}, adapter, runtime, agent.NewInMemorySessionStore(), bus, DefaultStatusReminderDelay)
	bridge.status = NewRunStatusNotifier(10*time.Millisecond, adapter.Send)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	sub := bus.SubscribeChannel(128)
	go bridge.outboundLoop(ctx, sub)

	bridge.handleInbound(ctx, InboundMessage{
		ChannelID: "whatsapp",
		SenderID:  "8613800138000",
		Content:   "hello",
		RawEvent: map[string]any{
			"message_id": "wamid-3",
		},
	})

	if err := bus.Publish(ctx, eventbus.Event{Type: eventbus.EventRunWaitingApproval, RunID: "run-2"}); err != nil {
		t.Fatalf("Publish() error = %v", err)
	}
	time.Sleep(50 * time.Millisecond)

	// Approval waits should be surfaced to the user.
	sent := adapter.Messages()
	foundApproval := false
	for _, msg := range sent {
		kind, _ := msg.Metadata["status_kind"].(string)
		if kind == "approval_waiting" {
			foundApproval = true
			if msg.Content != "This request is waiting for approval. Reply with [1] Approve  [2] Deny  [3] Always." {
				t.Fatalf("unexpected approval message content: %q", msg.Content)
			}
		}
	}
	if !foundApproval {
		t.Fatalf("expected approval_waiting message, got %#v", sent)
	}

	// Internal state should still be tracked.
	snap, ok := bridge.status.SnapshotRun("run-2")
	if !ok {
		t.Fatal("expected run to be tracked in status notifier")
	}
	if snap.Target.RunID != "run-2" {
		t.Fatalf("snapshot RunID = %q", snap.Target.RunID)
	}
}

// TestGenericBridgeProgressTrackedInternally verifies that progress events
// update the internal state and, after throttling, send a friendly status
// message to the user.
func TestGenericBridgeProgressTrackedInternally(t *testing.T) {
	t.Parallel()

	adapter := &bridgeStubAdapter{inbound: make(chan InboundMessage, 1)}
	runtime := &bridgeStubRuntime{run: &agent.Run{ID: "run-progress"}}
	bus := eventbus.NewInMemoryBus()
	bridge := NewBridge(BridgeConfig{
		ChannelName:  "whatsapp",
		TargetIDKey:  "from",
		MessageIDKey: "message_id",
	}, adapter, runtime, agent.NewInMemorySessionStore(), bus, DefaultStatusReminderDelay)
	bridge.status = NewRunStatusNotifier(10*time.Millisecond, adapter.Send)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	sub := bus.SubscribeChannel(128)
	go bridge.outboundLoop(ctx, sub)

	bridge.handleInbound(ctx, InboundMessage{
		ChannelID: "whatsapp",
		SenderID:  "8613800138000",
		Content:   "search today's hot news and keep me posted",
		RawEvent: map[string]any{
			"message_id": "wamid-progress",
		},
	})
	if err := bus.Publish(ctx, eventbus.Event{
		Type:  eventbus.EventRunPhaseChanged,
		RunID: "run-progress",
		Attrs: map[string]any{
			"phase":      "executing_tools",
			"tool_names": []string{"search.news", "web.fetch"},
		},
	}); err != nil {
		t.Fatalf("Publish() error = %v", err)
	}
	if err := bus.Publish(ctx, eventbus.Event{
		Type:  eventbus.EventToolExecuted,
		RunID: "run-progress",
		Attrs: map[string]any{
			"tool_round": 3,
			"tool_names": []string{"search.news", "web.fetch"},
		},
	}); err != nil {
		t.Fatalf("Publish() error = %v", err)
	}

	// Wait for event processing and heartbeat to fire.
	time.Sleep(50 * time.Millisecond)

	sent := adapter.Messages()
	foundProcessing := false
	for _, msg := range sent {
		if kind, _ := msg.Metadata["status_kind"].(string); kind == "processing" {
			foundProcessing = true
			if !strings.Contains(msg.Content, "search.news, web.fetch") {
				t.Fatalf("processing message missing tool detail: %q", msg.Content)
			}
		}
	}
	if !foundProcessing {
		t.Fatalf("expected a processing message, got %#v", sent)
	}

	// Tool progress should also be tracked internally.
	snap, ok := bridge.status.SnapshotRun("run-progress")
	if !ok {
		t.Fatal("expected run to be tracked")
	}
	if snap.ToolRounds != 3 {
		t.Fatalf("snap.ToolRounds = %d, want 3", snap.ToolRounds)
	}
	if len(snap.ToolNames) < 1 || snap.ToolNames[0] != "search.news" {
		t.Fatalf("snap.ToolNames = %v", snap.ToolNames)
	}
	if snap.Phase != "executing_tools" {
		t.Fatalf("snap.Phase = %q, want %q", snap.Phase, "executing_tools")
	}
}

func TestGenericBridgeStatusCommandReturnsActiveRunStatusWithoutSubmittingNewRun(t *testing.T) {
	t.Parallel()

	adapter := &bridgeStubAdapter{inbound: make(chan InboundMessage, 1)}
	runtime := &bridgeStubRuntime{run: &agent.Run{
		ID:     "run-query",
		Status: agent.RunRunning,
		Phase:  "tools",
	}}
	bridge := NewBridge(BridgeConfig{
		ChannelName:  "whatsapp",
		TargetIDKey:  "from",
		MessageIDKey: "message_id",
	}, adapter, runtime, agent.NewInMemorySessionStore(), nil, time.Hour)

	bridge.handleInbound(context.Background(), InboundMessage{
		ChannelID: "whatsapp",
		SenderID:  "8613800138000",
		Content:   "帮我搜今天的新闻",
		RawEvent: map[string]any{
			"message_id": "wamid-task",
		},
	})
	bridge.status.NotifyToolProgress("run-query", 3, []string{"search.news", "web.fetch"})
	bridge.handleInbound(context.Background(), InboundMessage{
		ChannelID: "whatsapp",
		SenderID:  "8613800138000",
		Content:   "/status",
		RawEvent: map[string]any{
			"message_id": "wamid-progress-query",
		},
	})

	if runtime.Submits() != 1 {
		t.Fatalf("runtime.Submits() = %d, want 1", runtime.Submits())
	}
	sent := adapter.Messages()
	if len(sent) != 1 {
		t.Fatalf("len(sent) = %d", len(sent))
	}
	if got := sent[0].Metadata["status_kind"]; got != "control_status" {
		t.Fatalf("status_kind = %#v", got)
	}
	if got := sent[0].Metadata["control_command"]; got != "status" {
		t.Fatalf("control_command = %#v", got)
	}
	if !strings.Contains(sent[0].Content, "running") {
		t.Fatalf("progress reply content = %q", sent[0].Content)
	}
	if !strings.Contains(sent[0].Content, "already done some checks") {
		t.Fatalf("friendly progress summary missing from content = %q", sent[0].Content)
	}
}

func TestGenericBridgeStatusCommandReportsIdleWhenNoActiveRun(t *testing.T) {
	t.Parallel()

	adapter := &bridgeStubAdapter{inbound: make(chan InboundMessage, 1)}
	runtime := &bridgeStubRuntime{}
	bridge := NewBridge(BridgeConfig{
		ChannelName:  "whatsapp",
		TargetIDKey:  "from",
		MessageIDKey: "message_id",
	}, adapter, runtime, agent.NewInMemorySessionStore(), nil, time.Hour)

	bridge.handleInbound(context.Background(), InboundMessage{
		ChannelID: "whatsapp",
		SenderID:  "8613800138000",
		Content:   "/status",
		RawEvent: map[string]any{
			"message_id": "wamid-idle-progress",
		},
	})

	if runtime.Submits() != 0 {
		t.Fatalf("runtime.Submits() = %d, want 0", runtime.Submits())
	}
	sent := adapter.Messages()
	if len(sent) != 1 {
		t.Fatalf("len(sent) = %d", len(sent))
	}
	if got := sent[0].Metadata["run_status"]; got != "idle" {
		t.Fatalf("run_status = %#v", got)
	}
	if !strings.Contains(sent[0].Content, "There is no active work") {
		t.Fatalf("idle reply content = %q", sent[0].Content)
	}
}

func TestGenericBridgeRendersInteractStatusReplyWithoutSubmittingNewRun(t *testing.T) {
	t.Parallel()

	adapter := &bridgeStubAdapter{inbound: make(chan InboundMessage, 1)}
	runtime := &bridgeInteractStubRuntime{
		bridgeStubRuntime: &bridgeStubRuntime{},
		interactionResp: &runtimesvc.InteractionResult{
			Decision: runtimesvc.InteractionDecision{
				SpeechAct: runtimesvc.SpeechActStatusQuery,
				ReplyAct:  runtimesvc.ReplyActStatusReply,
				Reason:    "semantic_status_query",
			},
			Run: &agent.Run{
				ID:         "run-routed-status",
				Status:     agent.RunRunning,
				Phase:      agent.PhaseExecutingTools,
				Model:      "deepseek-chat",
				ToolRounds: 2,
			},
		},
	}
	bridge := NewBridge(BridgeConfig{
		ChannelName:  "whatsapp",
		TargetIDKey:  "from",
		MessageIDKey: "message_id",
	}, adapter, runtime, agent.NewInMemorySessionStore(), nil, time.Hour)

	bridge.handleInbound(context.Background(), InboundMessage{
		ChannelID: "whatsapp",
		SenderID:  "8613800138000",
		Content:   "现在进度怎么样了？",
		RawEvent: map[string]any{
			"message_id": "wamid-route-status",
		},
	})

	if runtime.Submits() != 0 {
		t.Fatalf("runtime.Submits() = %d, want 0", runtime.Submits())
	}
	sent := adapter.Messages()
	if len(sent) != 1 {
		t.Fatalf("len(sent) = %d", len(sent))
	}
	if got := sent[0].Metadata["status_kind"]; got != "control_status" {
		t.Fatalf("status_kind = %#v", got)
	}
	if !strings.Contains(sent[0].Content, "我已经做了一些检查") {
		t.Fatalf("progress reply content = %q", sent[0].Content)
	}
}

func TestGenericBridgeTracksInteractTaskAcceptAndPreflight(t *testing.T) {
	t.Parallel()

	adapter := &bridgeStubAdapter{inbound: make(chan InboundMessage, 1)}
	runtime := &bridgeInteractStubRuntime{
		bridgeStubRuntime: &bridgeStubRuntime{},
		interactionResp: &runtimesvc.InteractionResult{
			Decision: runtimesvc.InteractionDecision{
				SpeechAct: runtimesvc.SpeechActNewTask,
				ReplyAct:  runtimesvc.ReplyActTaskAccept,
			},
			Run: &agent.Run{
				ID: "run-routed-follow-up",
				Preflight: &agent.RunPreflightReport{
					State:   agent.RunPreflightAutoPreparing,
					Summary: "The system is preparing the required capabilities automatically.",
				},
			},
		},
	}
	bridge := NewBridge(BridgeConfig{
		ChannelName:  "whatsapp",
		TargetIDKey:  "from",
		MessageIDKey: "message_id",
	}, adapter, runtime, agent.NewInMemorySessionStore(), nil, time.Hour)

	bridge.handleInbound(context.Background(), InboundMessage{
		ChannelID: "whatsapp",
		SenderID:  "8613800138000",
		Content:   "做完后再导出一个 csv",
		RawEvent: map[string]any{
			"message_id": "wamid-route-enqueue",
		},
	})

	waitForBridgeStatusKind(t, adapter, "preflight_auto_preparing")
	if runtime.interactions != 1 {
		t.Fatalf("runtime.interactions = %d, want 1", runtime.interactions)
	}
}

func TestGenericBridgeRendersInteractSteerAcceptedWithoutSubmittingNewRun(t *testing.T) {
	t.Parallel()

	adapter := &bridgeStubAdapter{inbound: make(chan InboundMessage, 1)}
	runtime := &bridgeInteractStubRuntime{
		bridgeStubRuntime: &bridgeStubRuntime{},
		interactionResp: &runtimesvc.InteractionResult{
			Decision: runtimesvc.InteractionDecision{
				SpeechAct: runtimesvc.SpeechActTaskFollowup,
				ReplyAct:  runtimesvc.ReplyActResumeAck,
			},
			Run:           &agent.Run{ID: "run-routed-steer"},
			SteerEnqueued: true,
		},
	}
	bridge := NewBridge(BridgeConfig{
		ChannelName:  "whatsapp",
		TargetIDKey:  "from",
		MessageIDKey: "message_id",
	}, adapter, runtime, agent.NewInMemorySessionStore(), nil, time.Hour)

	bridge.handleInbound(context.Background(), InboundMessage{
		ChannelID: "whatsapp",
		SenderID:  "8613800138000",
		Content:   "你给我的网址 404 了，先排查一下",
		RawEvent: map[string]any{
			"message_id": "wamid-route-issue",
		},
	})

	if runtime.Submits() != 0 {
		t.Fatalf("runtime.Submits() = %d, want 0", runtime.Submits())
	}
	sent := adapter.Messages()
	if len(sent) != 1 {
		t.Fatalf("len(sent) = %d", len(sent))
	}
	if got := sent[0].Metadata["status_kind"]; got != "steer_accepted" {
		t.Fatalf("status_kind = %#v", got)
	}
	if !strings.Contains(sent[0].Content, "刚补充的要求") {
		t.Fatalf("steer content = %q", sent[0].Content)
	}
}

func TestGenericBridgeRendersInteractCancelAckWithoutSubmittingNewRun(t *testing.T) {
	t.Parallel()

	adapter := &bridgeStubAdapter{inbound: make(chan InboundMessage, 1)}
	runtime := &bridgeInteractStubRuntime{
		bridgeStubRuntime: &bridgeStubRuntime{},
		interactionResp: &runtimesvc.InteractionResult{
			Decision: runtimesvc.InteractionDecision{
				SpeechAct: runtimesvc.SpeechActCommand,
				ReplyAct:  runtimesvc.ReplyActActionAck,
				Reason:    "semantic_cancel",
			},
			RunCancelled: true,
			Run:          &agent.Run{ID: "run-routed-cancel", Status: agent.RunCancelled},
		},
	}
	bridge := NewBridge(BridgeConfig{
		ChannelName:  "whatsapp",
		TargetIDKey:  "from",
		MessageIDKey: "message_id",
	}, adapter, runtime, agent.NewInMemorySessionStore(), nil, time.Hour)

	bridge.handleInbound(context.Background(), InboundMessage{
		ChannelID: "whatsapp",
		SenderID:  "8613800138000",
		Content:   "先别做了，取消吧",
		RawEvent: map[string]any{
			"message_id": "wamid-route-cancel",
		},
	})

	if runtime.Submits() != 0 {
		t.Fatalf("runtime.Submits() = %d, want 0", runtime.Submits())
	}
	sent := adapter.Messages()
	if len(sent) != 1 {
		t.Fatalf("len(sent) = %d", len(sent))
	}
	if got := sent[0].Metadata["status_kind"]; got != "control_cancel" {
		t.Fatalf("status_kind = %#v", got)
	}
	if !strings.Contains(sent[0].Content, "已取消当前请求") {
		t.Fatalf("cancel content = %q", sent[0].Content)
	}
}

func TestGenericBridgeRendersInteractChatReplyWithoutSubmittingNewRun(t *testing.T) {
	t.Parallel()

	adapter := &bridgeStubAdapter{inbound: make(chan InboundMessage, 1)}
	runtime := &bridgeInteractStubRuntime{
		bridgeStubRuntime: &bridgeStubRuntime{},
		interactionResp: &runtimesvc.InteractionResult{
			Decision: runtimesvc.InteractionDecision{
				SpeechAct: runtimesvc.SpeechActCasualChat,
				ReplyAct:  runtimesvc.ReplyActChatReply,
			},
			Context: runtimesvc.InteractionContextSnapshot{
				HasActiveRun: true,
			},
			ReplyMessage: "我还在处理中，有进展会继续同步。",
		},
	}
	bridge := NewBridge(BridgeConfig{
		ChannelName:  "whatsapp",
		TargetIDKey:  "from",
		MessageIDKey: "message_id",
	}, adapter, runtime, agent.NewInMemorySessionStore(), nil, time.Hour)

	bridge.handleInbound(context.Background(), InboundMessage{
		ChannelID: "whatsapp",
		SenderID:  "8613800138000",
		Content:   "辛苦了，继续",
		RawEvent: map[string]any{
			"message_id": "wamid-route-smalltalk",
		},
	})

	if runtime.Submits() != 0 {
		t.Fatalf("runtime.Submits() = %d, want 0", runtime.Submits())
	}
	sent := adapter.Messages()
	if len(sent) != 1 {
		t.Fatalf("len(sent) = %d", len(sent))
	}
	if got := sent[0].Metadata["status_kind"]; got != "smalltalk_during_task" {
		t.Fatalf("status_kind = %#v", got)
	}
	if sent[0].Content != "我还在处理中，有进展会继续同步。" {
		t.Fatalf("chat reply content = %q", sent[0].Content)
	}
}

func TestGenericBridgeTreatsMissingInteractChatReplyAsFailure(t *testing.T) {
	t.Parallel()

	adapter := &bridgeStubAdapter{inbound: make(chan InboundMessage, 1)}
	runtime := &bridgeInteractStubRuntime{
		bridgeStubRuntime: &bridgeStubRuntime{},
		interactionResp: &runtimesvc.InteractionResult{
			Decision: runtimesvc.InteractionDecision{
				SpeechAct: runtimesvc.SpeechActCasualChat,
				ReplyAct:  runtimesvc.ReplyActChatReply,
			},
			Context: runtimesvc.InteractionContextSnapshot{
				HasActiveRun: true,
			},
		},
	}
	bridge := NewBridge(BridgeConfig{
		ChannelName:  "whatsapp",
		TargetIDKey:  "from",
		MessageIDKey: "message_id",
	}, adapter, runtime, agent.NewInMemorySessionStore(), nil, time.Hour)

	bridge.handleInbound(context.Background(), InboundMessage{
		ChannelID: "whatsapp",
		SenderID:  "8613800138000",
		Content:   "辛苦了，继续",
		RawEvent: map[string]any{
			"message_id": "wamid-route-smalltalk-missing",
		},
	})

	if runtime.Submits() != 0 {
		t.Fatalf("runtime.Submits() = %d, want 0", runtime.Submits())
	}
	sent := adapter.Messages()
	if len(sent) != 1 {
		t.Fatalf("len(sent) = %d", len(sent))
	}
	if got := sent[0].Metadata["status_kind"]; got != "task_failure" {
		t.Fatalf("status_kind = %#v, want task_failure", got)
	}
	if strings.TrimSpace(sent[0].Content) == "" {
		t.Fatalf("chat reply content = %q, want infrastructure failure text", sent[0].Content)
	}
}

func TestGenericBridgeShortCircuitsDuringAuthOutage(t *testing.T) {
	t.Parallel()

	adapter := &bridgeStubAdapter{inbound: make(chan InboundMessage, 1)}
	runtime := &bridgeStubRuntime{run: &agent.Run{ID: "run-3"}}
	bridge := NewBridge(BridgeConfig{
		ChannelName:  "whatsapp",
		TargetIDKey:  "from",
		MessageIDKey: "message_id",
	}, adapter, runtime, agent.NewInMemorySessionStore(), nil, DefaultStatusReminderDelay)
	bridge.authGate = NewAuthFailureGate(time.Hour, time.Hour)
	bridge.authGate.Arm("whatsapp:8613800138000")

	bridge.handleInbound(context.Background(), InboundMessage{
		ChannelID: "whatsapp",
		SenderID:  "8613800138000",
		Content:   "再试一次",
		RawEvent: map[string]any{
			"message_id": "wamid-4",
		},
	})
	bridge.handleInbound(context.Background(), InboundMessage{
		ChannelID: "whatsapp",
		SenderID:  "8613800138000",
		Content:   "再试一次",
		RawEvent: map[string]any{
			"message_id": "wamid-5",
		},
	})

	if runtime.Submits() != 0 {
		t.Fatalf("runtime.Submits() = %d, want 0", runtime.Submits())
	}
	sent := adapter.Messages()
	if len(sent) != 1 {
		t.Fatalf("len(sent) = %d", len(sent))
	}
	if got := sent[0].Metadata["status_kind"]; got != "backend_auth_unavailable" {
		t.Fatalf("status_kind = %#v", got)
	}
	if !strings.Contains(sent[0].Content, "鉴权") {
		t.Fatalf("unexpected content = %q", sent[0].Content)
	}
}

func TestGenericBridgeApprovesPendingRequestViaReply(t *testing.T) {
	t.Parallel()

	store := agent.NewInMemorySessionStore()
	session, err := store.GetOrCreate(context.Background(), "whatsapp:8613800138000", "test-model")
	if err != nil {
		t.Fatalf("GetOrCreate() error = %v", err)
	}

	adapter := &bridgeStubAdapter{inbound: make(chan InboundMessage, 1)}
	runtime := &bridgeStubRuntime{
		run: &agent.Run{ID: "run-approve"},
		pendingBySession: map[string]*approval.Ticket{
			session.ID: {ID: "appr-1", RunID: "run-approve", SessionID: session.ID, Kind: approval.KindSkillInstall, Status: approval.StatusPending},
		},
		approvalsByID: map[string]*approval.Ticket{
			"appr-1": {ID: "appr-1", RunID: "run-approve", SessionID: session.ID, Kind: approval.KindSkillInstall, Status: approval.StatusPending},
		},
	}
	bridge := NewBridge(BridgeConfig{
		ChannelName:  "whatsapp",
		TargetIDKey:  "from",
		MessageIDKey: "message_id",
	}, adapter, runtime, store, nil, DefaultStatusReminderDelay)

	bridge.handleInbound(context.Background(), InboundMessage{
		ChannelID: "whatsapp",
		SenderID:  "8613800138000",
		Content:   "Y",
		RawEvent: map[string]any{
			"message_id": "wamid-approve-1",
		},
	})

	if runtime.Submits() != 0 {
		t.Fatalf("runtime.Submits() = %d, want 0", runtime.Submits())
	}
	if len(runtime.Resolved()) != 1 || runtime.Resolved()[0].Status != approval.StatusApproved {
		t.Fatalf("runtime.Resolved() = %#v", runtime.Resolved())
	}
	sent := adapter.Messages()
	if len(sent) != 1 {
		t.Fatalf("len(sent) = %d", len(sent))
	}
	if got := sent[0].Metadata["status_kind"]; got != "approval_reply_approved" {
		t.Fatalf("status_kind = %#v", got)
	}
}

func TestGenericBridgeInteractReplyRendersApprovalDenied(t *testing.T) {
	t.Parallel()

	adapter := &bridgeStubAdapter{inbound: make(chan InboundMessage, 1)}
	runtime := &bridgeInteractStubRuntime{
		bridgeStubRuntime: &bridgeStubRuntime{},
		interactionResp: &runtimesvc.InteractionResult{
			Decision: runtimesvc.InteractionDecision{
				SpeechAct: runtimesvc.SpeechActApprovalReply,
				ReplyAct:  runtimesvc.ReplyActResumeAck,
				Reason:    "text_approval_deny",
			},
			ApprovalResolved: true,
			ApprovalStatus:   approval.StatusDenied,
		},
	}
	bridge := NewBridge(BridgeConfig{
		ChannelName:  "whatsapp",
		TargetIDKey:  "from",
		MessageIDKey: "message_id",
	}, adapter, runtime, agent.NewInMemorySessionStore(), nil, DefaultStatusReminderDelay)

	bridge.handleInbound(context.Background(), InboundMessage{
		ChannelID: "whatsapp",
		SenderID:  "8613800138000",
		Content:   "N",
		RawEvent: map[string]any{
			"from":       "8613800138000",
			"message_id": "wamid-deny-1",
		},
	})

	sent := adapter.Messages()
	if len(sent) != 1 {
		t.Fatalf("len(sent) = %d", len(sent))
	}
	if got := sent[0].Metadata["status_kind"]; got != "approval_reply_denied" {
		t.Fatalf("status_kind = %#v", got)
	}
	if !strings.Contains(strings.ToLower(sent[0].Content), "denied") {
		t.Fatalf("content = %q", sent[0].Content)
	}
}

func TestGenericBridgeAlwaysReplyPersistsSessionAutoApprove(t *testing.T) {
	t.Parallel()

	store := agent.NewInMemorySessionStore()
	session, err := store.GetOrCreate(context.Background(), "whatsapp:8613800138000", "test-model")
	if err != nil {
		t.Fatalf("GetOrCreate() error = %v", err)
	}

	adapter := &bridgeStubAdapter{inbound: make(chan InboundMessage, 1)}
	runtime := &bridgeStubRuntime{
		run: &agent.Run{ID: "run-auto", SessionID: session.ID, ApprovalID: "appr-2"},
		pendingBySession: map[string]*approval.Ticket{
			session.ID: {ID: "appr-2", RunID: "run-auto", SessionID: session.ID, Kind: approval.KindSkillInstall, Status: approval.StatusPending},
		},
		approvalsByID: map[string]*approval.Ticket{
			"appr-2": {ID: "appr-2", RunID: "run-auto", SessionID: session.ID, Kind: approval.KindSkillInstall, Status: approval.StatusPending},
			"appr-3": {ID: "appr-3", RunID: "run-next", SessionID: session.ID, Kind: approval.KindSkillInstall, Status: approval.StatusPending},
		},
	}
	bridge := NewBridge(BridgeConfig{
		ChannelName:  "whatsapp",
		TargetIDKey:  "from",
		MessageIDKey: "message_id",
	}, adapter, runtime, store, nil, DefaultStatusReminderDelay)

	bridge.handleInbound(context.Background(), InboundMessage{
		ChannelID: "whatsapp",
		SenderID:  "8613800138000",
		Content:   "a",
		RawEvent: map[string]any{
			"message_id": "wamid-auto-1",
		},
	})

	saved, err := store.Get(context.Background(), session.ID)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if !SessionAutoApproveSession(saved) {
		t.Fatal("expected session auto-approve to be persisted")
	}

	bridge.status = NewRunStatusNotifier(time.Hour, adapter.Send)
	if !bridge.tryAutoApproveSkillInstall(context.Background(), eventbus.Event{
		Type:      eventbus.EventRunWaitingApproval,
		RunID:     "run-next",
		SessionID: session.ID,
		Attrs:     map[string]any{"approval_id": "appr-3"},
	}) {
		t.Fatal("expected auto-approve to handle waiting approval")
	}
	if len(runtime.Resolved()) < 2 || runtime.Resolved()[len(runtime.Resolved())-1].Status != approval.StatusApproved {
		t.Fatalf("runtime.Resolved() = %#v", runtime.Resolved())
	}
}

func TestDeliverInteractionResultRetriesBeforeSuccess(t *testing.T) {
	t.Parallel()

	adapter := &bridgeStubAdapter{
		sendErrs: []error{
			errors.New("temporary send failure 1"),
			errors.New("temporary send failure 2"),
			nil,
		},
	}
	bus := eventbus.NewInMemoryBus()
	ctx := withInteractionDeliveryRetryBackoffs(context.Background(), []time.Duration{0, 0, 0})
	result := &runtimesvc.InteractionResult{
		Decision:     runtimesvc.InteractionDecision{ReplyAct: runtimesvc.ReplyActChatReply},
		ReplyMessage: "hello after retry",
		Run:          &agent.Run{ID: "run-retry", SessionID: "sess-retry"},
	}
	target := RunNotificationTarget{
		SessionKey: "chat:retry",
		ChannelID:  "test",
		TargetID:   "target-1",
		ReplyToID:  "msg-1",
		Format:     "text",
	}

	DeliverInteractionResult(ctx, adapter.Send, bus, nil, "test", result, target)

	if got := adapter.SendCalls(); got != 3 {
		t.Fatalf("SendCalls() = %d, want 3", got)
	}
	if sent := adapter.Messages(); len(sent) != 1 || sent[0].Content != "hello after retry" {
		t.Fatalf("Messages() = %#v", sent)
	}
	for _, event := range bus.Snapshot() {
		if event.Type == eventbus.EventDeliveryFailed {
			t.Fatalf("unexpected delivery failure event: %#v", event)
		}
	}
}

func TestDeliverInteractionResultPublishesFailureEventAfterRetries(t *testing.T) {
	t.Parallel()

	adapter := &bridgeStubAdapter{
		sendErrs: []error{
			errors.New("temporary send failure 1"),
			errors.New("temporary send failure 2"),
			errors.New("temporary send failure 3"),
			errors.New("temporary send failure 4"),
			nil,
		},
	}
	bus := eventbus.NewInMemoryBus()
	ctx := withInteractionDeliveryRetryBackoffs(context.Background(), []time.Duration{0, 0, 0})
	result := &runtimesvc.InteractionResult{
		Decision:     runtimesvc.InteractionDecision{ReplyAct: runtimesvc.ReplyActChatReply},
		ReplyMessage: "hello never delivered",
		Run:          &agent.Run{ID: "run-failed-delivery", SessionID: "sess-failed-delivery"},
	}
	target := RunNotificationTarget{
		SessionKey: "chat:failed-delivery",
		ChannelID:  "test",
		TargetID:   "target-1",
		ReplyToID:  "msg-1",
		Format:     "text",
	}

	DeliverInteractionResult(ctx, adapter.Send, bus, nil, "test", result, target)

	if got := adapter.SendCalls(); got != 5 {
		t.Fatalf("SendCalls() = %d, want 5", got)
	}
	if sent := adapter.Messages(); len(sent) != 1 {
		t.Fatalf("Messages() = %#v, want one delivery-failure notice", sent)
	} else {
		if sent[0].Content != BridgeDeliveryFailureNoticeMessage("") {
			t.Fatalf("failure notice = %q, want %q", sent[0].Content, BridgeDeliveryFailureNoticeMessage(""))
		}
		if sent[0].ReplyToID != "" {
			t.Fatalf("failure notice ReplyToID = %q, want empty", sent[0].ReplyToID)
		}
		if got := sent[0].Metadata["delivery_failure_notice"]; got != true {
			t.Fatalf("failure notice delivery_failure_notice = %#v, want true", got)
		}
	}
	events := bus.Snapshot()
	if len(events) != 1 {
		t.Fatalf("len(bus.Snapshot()) = %d, want 1", len(events))
	}
	if events[0].Type != eventbus.EventDeliveryFailed {
		t.Fatalf("event.Type = %q, want %q", events[0].Type, eventbus.EventDeliveryFailed)
	}
	if events[0].RunID != "run-failed-delivery" {
		t.Fatalf("event.RunID = %q, want %q", events[0].RunID, "run-failed-delivery")
	}
	if got := events[0].Attrs["attempts"]; got != 4 {
		t.Fatalf("event.Attrs[attempts] = %#v, want 4", got)
	}
	if got := events[0].Attrs["status_kind"]; got != "chat_reply" {
		t.Fatalf("event.Attrs[status_kind] = %#v, want %q", got, "chat_reply")
	}
}

func TestDeliverInteractionResultDoesNotRetryPermanentSendError(t *testing.T) {
	t.Parallel()

	adapter := &bridgeStubAdapter{
		sendErrs: []error{
			MarkSendError(errors.New("bad request"), false, 400),
			nil,
		},
	}
	bus := eventbus.NewInMemoryBus()
	ctx := withInteractionDeliveryRetryBackoffs(context.Background(), []time.Duration{0, 0, 0})
	result := &runtimesvc.InteractionResult{
		Decision:     runtimesvc.InteractionDecision{ReplyAct: runtimesvc.ReplyActChatReply},
		ReplyMessage: "hello never delivered",
		Run:          &agent.Run{ID: "run-permanent-delivery", SessionID: "sess-permanent-delivery"},
	}
	target := RunNotificationTarget{
		SessionKey: "chat:permanent-delivery",
		ChannelID:  "test",
		TargetID:   "target-1",
		ReplyToID:  "msg-1",
		Format:     "text",
	}

	DeliverInteractionResult(ctx, adapter.Send, bus, nil, "test", result, target)

	if got := adapter.SendCalls(); got != 2 {
		t.Fatalf("SendCalls() = %d, want 2", got)
	}
	if sent := adapter.Messages(); len(sent) != 1 || sent[0].Content != BridgeDeliveryFailureNoticeMessage("") {
		t.Fatalf("Messages() = %#v, want one delivery-failure notice", sent)
	}
	events := bus.Snapshot()
	if len(events) != 1 {
		t.Fatalf("len(bus.Snapshot()) = %d, want 1", len(events))
	}
	if got := events[0].Attrs["attempts"]; got != 1 {
		t.Fatalf("event.Attrs[attempts] = %#v, want 1", got)
	}
}

func TestHandleTerminalRunPublishesFailureEventAndNotice(t *testing.T) {
	t.Parallel()

	store := agent.NewInMemorySessionStore()
	session, err := store.GetOrCreate(context.Background(), "chat:terminal-failure", "test-model")
	if err != nil {
		t.Fatalf("GetOrCreate() error = %v", err)
	}
	locked, unlock, err := store.LoadForExecution(context.Background(), session.ID)
	if err != nil {
		t.Fatalf("LoadForExecution() error = %v", err)
	}
	now := time.Now().UTC()
	locked.Messages = append(locked.Messages,
		contextengine.Message{
			Role:      contextengine.RoleUser,
			Content:   "hello",
			CreatedAt: now,
			Metadata: map[string]any{
				meta.KeyChannel: "testbridge",
				"chat_id":       "target-1",
				"message_id":    "msg-1",
			},
		},
		contextengine.Message{
			Role:      contextengine.RoleAssistant,
			Content:   "final result",
			CreatedAt: now.Add(time.Second),
			Metadata:  map[string]any{meta.KeyRunID: "run-terminal-failure"},
		},
	)
	if err := store.Save(context.Background(), locked); err != nil {
		unlock()
		t.Fatalf("Save() error = %v", err)
	}
	unlock()

	adapter := &bridgeStubAdapter{
		inbound:  make(chan InboundMessage),
		sendErrs: []error{errors.New("terminal send failed"), nil},
	}
	bus := eventbus.NewInMemoryBus()
	runtime := &bridgeStubRuntime{run: &agent.Run{
		ID:           "run-terminal-failure",
		SessionID:    session.ID,
		InputEventID: "msg-1",
	}}
	bridge := NewBridge(BridgeConfig{ChannelName: "testbridge"}, adapter, runtime, store, bus, DefaultStatusReminderDelay)

	bridge.handleTerminalRun(context.Background(), eventbus.Event{
		Type:      eventbus.EventRunCompleted,
		RunID:     "run-terminal-failure",
		SessionID: session.ID,
	})

	sent := adapter.Messages()
	if len(sent) != 1 {
		t.Fatalf("len(Messages()) = %d, want 1", len(sent))
	}
	if sent[0].Content != BridgeDeliveryFailureNoticeMessage("hello") {
		t.Fatalf("failure notice = %q, want %q", sent[0].Content, BridgeDeliveryFailureNoticeMessage("hello"))
	}
	if sent[0].ReplyToID != "" {
		t.Fatalf("failure notice ReplyToID = %q, want empty", sent[0].ReplyToID)
	}
	events := bus.Snapshot()
	if len(events) != 1 || events[0].Type != eventbus.EventDeliveryFailed {
		t.Fatalf("delivery failure events = %#v", events)
	}
	if got := events[0].RunID; got != "run-terminal-failure" {
		t.Fatalf("event.RunID = %q, want %q", got, "run-terminal-failure")
	}
}

func TestDeliverInteractionResultSendsClarificationPrompt(t *testing.T) {
	t.Parallel()

	adapter := &bridgeStubAdapter{}
	result := &runtimesvc.InteractionResult{
		Decision:     runtimesvc.InteractionDecision{ReplyAct: runtimesvc.ReplyActClarificationPrompt},
		ReplyMessage: "Please confirm the target file and expected output.",
	}
	target := RunNotificationTarget{
		SessionKey: "chat:clarify",
		ChannelID:  "test",
		TargetID:   "target-1",
		ReplyToID:  "msg-1",
		Format:     "text",
	}

	DeliverInteractionResult(context.Background(), adapter.Send, nil, nil, "test", result, target)

	sent := adapter.Messages()
	if len(sent) != 1 {
		t.Fatalf("len(Messages()) = %d, want 1", len(sent))
	}
	if sent[0].Content != "Please confirm the target file and expected output." {
		t.Fatalf("Content = %q", sent[0].Content)
	}
	if got := sent[0].Metadata["status_kind"]; got != "clarification_prompt" {
		t.Fatalf("status_kind = %#v, want %q", got, "clarification_prompt")
	}
}

func TestDeliverInteractionResultTreatsMissingClarificationPromptAsFailure(t *testing.T) {
	t.Parallel()

	adapter := &bridgeStubAdapter{}
	result := &runtimesvc.InteractionResult{
		Decision: runtimesvc.InteractionDecision{ReplyAct: runtimesvc.ReplyActClarificationPrompt},
	}
	target := RunNotificationTarget{
		SessionKey: "chat:clarify-missing",
		ChannelID:  "test",
		TargetID:   "target-1",
		ReplyToID:  "msg-1",
		Format:     "text",
	}

	DeliverInteractionResult(context.Background(), adapter.Send, nil, nil, "test", result, target)

	sent := adapter.Messages()
	if len(sent) != 1 {
		t.Fatalf("len(Messages()) = %d, want 1", len(sent))
	}
	if got := sent[0].Metadata["status_kind"]; got != "task_failure" {
		t.Fatalf("status_kind = %#v, want %q", got, "task_failure")
	}
	if sent[0].Content == "" {
		t.Fatal("expected infrastructure failure content")
	}
}

func TestBridgeStreamingRendererLifecycle(t *testing.T) {
	t.Parallel()

	adapter := newBridgeStreamingStubAdapter()
	runtime := &bridgeStubRuntime{
		run: &agent.Run{ID: "run-stream", InputEventID: "msg-1"},
		runResult: &runtimesvc.RunResult{
			Output: "hello world",
		},
	}
	store := agent.NewInMemorySessionStore()
	session, err := store.GetOrCreate(context.Background(), "testbridge:chat-1", "test-model", "sess-stream")
	if err != nil {
		t.Fatalf("GetOrCreate() error = %v", err)
	}
	runtime.run.SessionID = session.ID

	locked, unlock, err := store.LoadForExecution(context.Background(), session.ID)
	if err != nil {
		t.Fatalf("LoadForExecution() error = %v", err)
	}
	locked.Messages = append(locked.Messages, contextengine.Message{
		Role:    contextengine.RoleUser,
		Content: "stream please",
		Metadata: map[string]any{
			meta.KeyChannel: "testbridge",
			"chat_id":       "chat-1",
			"message_id":    "msg-1",
		},
	})
	locked.MessageCount = len(locked.Messages)
	if err := store.Save(context.Background(), locked); err != nil {
		unlock()
		t.Fatalf("Save() error = %v", err)
	}
	unlock()

	bus := eventbus.NewInMemoryBus()
	bridge := NewBridge(BridgeConfig{
		ChannelName:  "testbridge",
		TargetIDKey:  "chat_id",
		MessageIDKey: "message_id",
	}, adapter, runtime, store, bus, DefaultStatusReminderDelay).WithDirectSessionUsesChatID(true)
	bridge.status.Track(context.Background(), RunNotificationTarget{
		RunID:        "run-stream",
		SessionKey:   "testbridge:chat-1",
		ChannelID:    "testbridge",
		TargetID:     "chat-1",
		ReplyToID:    "msg-1",
		InputContent: "stream please",
		Format:       "text",
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	bridge.Start(ctx)
	defer bridge.Stop()

	for _, event := range []eventbus.Event{
		{Type: eventbus.EventModelTextDelta, RunID: "run-stream", SessionID: session.ID, Attrs: map[string]any{"delta": "hello"}},
		{Type: eventbus.EventModelTextDelta, RunID: "run-stream", SessionID: session.ID, Attrs: map[string]any{"delta": " world"}},
		{Type: eventbus.EventRunCompleted, RunID: "run-stream", SessionID: session.ID},
	} {
		if err := bus.Publish(context.Background(), event); err != nil {
			t.Fatalf("Publish(%s) error = %v", event.Type, err)
		}
	}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		adapter.streamMu.Lock()
		beginCount := adapter.beginCount
		updateCount := adapter.updateCount
		endCount := adapter.endCount
		var last string
		if len(adapter.streamContent) > 0 {
			last = adapter.streamContent[len(adapter.streamContent)-1]
		}
		adapter.streamMu.Unlock()
		if beginCount == 1 && updateCount >= 1 && endCount == 1 && last == "hello world" {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("stream counts = begin:%d update:%d end:%d contents:%#v", adapter.beginCount, adapter.updateCount, adapter.endCount, adapter.streamContent)
}

func TestGenericBridgeStartStopConcurrent(t *testing.T) {
	t.Parallel()

	for i := 0; i < 200; i++ {
		bridge := NewBridge(BridgeConfig{
			ChannelName:  "testbridge",
			TargetIDKey:  "chat_id",
			MessageIDKey: "message_id",
		}, &bridgeStubAdapter{inbound: make(chan InboundMessage)}, nil, nil, nil, DefaultStatusReminderDelay)

		ctx, cancel := context.WithCancel(context.Background())
		var wg sync.WaitGroup
		wg.Add(2)
		go func() {
			defer wg.Done()
			bridge.Start(ctx)
		}()
		go func() {
			defer wg.Done()
			bridge.Stop()
		}()
		wg.Wait()
		cancel()
		bridge.Stop()
	}
}

func waitForBridgeMessages(t *testing.T, adapter *bridgeStubAdapter, want int) {
	t.Helper()
	deadline := time.Now().Add(500 * time.Millisecond)
	for time.Now().Before(deadline) {
		if len(adapter.Messages()) >= want {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %d messages; got %d", want, len(adapter.Messages()))
}

func waitForBridgeStatusKind(t *testing.T, adapter *bridgeStubAdapter, want string) {
	t.Helper()
	deadline := time.Now().Add(500 * time.Millisecond)
	for time.Now().Before(deadline) {
		for _, msg := range adapter.Messages() {
			if got, _ := msg.Metadata["status_kind"].(string); got == want {
				return
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for status_kind=%q; messages=%#v", want, adapter.Messages())
}
