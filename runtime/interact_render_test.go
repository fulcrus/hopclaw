package runtime

import (
	"testing"

	"github.com/fulcrus/hopclaw/agent"
)

func TestRenderDirectInteractionReplyUsesModelReplyForChat(t *testing.T) {
	t.Parallel()

	got := RenderDirectInteractionReply(&InteractionResult{
		Decision:     InteractionDecision{ReplyAct: ReplyActChatReply},
		ReplyMessage: "I am HopClaw.",
	}, "who are you?")
	if got != "I am HopClaw." {
		t.Fatalf("RenderDirectInteractionReply() = %q, want %q", got, "I am HopClaw.")
	}
}

func TestRenderDirectInteractionReplyTreatsMissingConversationReplyAsFailure(t *testing.T) {
	t.Parallel()

	got := RenderDirectInteractionReply(&InteractionResult{
		Decision: InteractionDecision{ReplyAct: ReplyActChatReply},
	}, "who are you?")
	if got == "" {
		t.Fatal("expected infrastructure failure message")
	}
	if got == "who are you?" {
		t.Fatalf("RenderDirectInteractionReply() = %q, should not echo the user input", got)
	}
}

func TestRenderDirectInteractionReplyTreatsMissingClarificationReplyAsFailure(t *testing.T) {
	t.Parallel()

	got := RenderDirectInteractionReply(&InteractionResult{
		Decision: InteractionDecision{ReplyAct: ReplyActClarificationPrompt},
	}, "帮我改一下")
	if got == "" {
		t.Fatal("expected infrastructure failure message")
	}
	if got == "帮我改一下" {
		t.Fatalf("RenderDirectInteractionReply() = %q, should not echo the user input", got)
	}
}

func TestRenderDirectInteractionReplyTaskAcceptShowsApprovalState(t *testing.T) {
	t.Parallel()

	got := RenderDirectInteractionReply(&InteractionResult{
		Decision: InteractionDecision{ReplyAct: ReplyActTaskAccept},
		Run:      &agent.Run{Status: agent.RunWaitingApproval},
	}, "请部署到 staging")
	if got == "" {
		t.Fatal("expected approval-required message")
	}
}

func TestRenderDirectInteractionReplyTaskAcceptShowsAcceptedState(t *testing.T) {
	t.Parallel()

	got := RenderDirectInteractionReply(&InteractionResult{
		Decision: InteractionDecision{ReplyAct: ReplyActTaskAccept},
		Run:      &agent.Run{Status: agent.RunQueued},
	}, "start it")
	if got == "" {
		t.Fatal("expected accepted message")
	}
}
