package feishu

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/fulcrus/hopclaw/channels"
	channelpairing "github.com/fulcrus/hopclaw/channels/pairing"
	"github.com/fulcrus/hopclaw/internal/meta"
)

type pairingCommand struct {
	action string
	code   string
}

func parsePairingCommand(text string) (pairingCommand, bool) {
	fields := strings.Fields(strings.TrimSpace(text))
	if len(fields) == 0 {
		return pairingCommand{}, false
	}
	if !strings.EqualFold(fields[0], "/pair") && !strings.EqualFold(fields[0], "/unpair") {
		return pairingCommand{}, false
	}
	if strings.EqualFold(fields[0], "/unpair") {
		return pairingCommand{action: "revoke"}, true
	}
	if len(fields) == 1 {
		return pairingCommand{action: "init"}, true
	}
	if len(fields) == 2 {
		switch strings.ToLower(fields[1]) {
		case "status":
			return pairingCommand{action: "status"}, true
		case "reset":
			return pairingCommand{action: "reset"}, true
		default:
			return pairingCommand{action: "verify", code: fields[1]}, true
		}
	}
	if len(fields) >= 3 && strings.EqualFold(fields[1], "verify") {
		return pairingCommand{action: "verify", code: fields[2]}, true
	}
	return pairingCommand{}, false
}

func pairingChannelName(accountID string) string {
	if strings.TrimSpace(accountID) == "" || strings.TrimSpace(accountID) == "default" {
		return "feishu"
	}
	return "feishu:" + strings.TrimSpace(accountID)
}

func (b *Bridge) handlePairingMessage(ctx context.Context, account ResolvedAccount, envelope inboundEnvelope, content string, target channels.RunNotificationTarget) bool {
	if b == nil || b.pairing == nil || envelope.ChatType != "p2p" {
		return false
	}
	command, isCommand := parsePairingCommand(content)
	channelName := pairingChannelName(account.ID)
	if account.DMPolicy == "pairing" && !b.pairing.IsVerified(channelName, envelope.SenderID) && !isCommand {
		return b.replyPairingRequired(ctx, account, envelope, target, false)
	}
	if !isCommand {
		return false
	}

	switch command.action {
	case "init", "reset":
		return b.replyPairingRequired(ctx, account, envelope, target, true)
	case "status":
		return b.replyPairingStatus(ctx, account, envelope, target)
	case "verify":
		return b.replyPairingVerify(ctx, account, envelope, command.code, target)
	case "revoke":
		return b.replyPairingRevoke(ctx, account, envelope, target)
	default:
		return false
	}
}

func (b *Bridge) replyPairingRequired(ctx context.Context, account ResolvedAccount, envelope inboundEnvelope, target channels.RunNotificationTarget, forceNew bool) bool {
	record, _ := b.pairing.Store().Get(pairingChannelName(account.ID), envelope.SenderID)
	now := time.Now().UTC()
	if forceNew || record == nil || record.Status != channelpairing.StatusPending || now.After(record.CodeExpiresAt) {
		code, err := b.pairing.InitiatePairing(pairingChannelName(account.ID), envelope.SenderID, "")
		if err != nil {
			return false
		}
		record, _ = b.pairing.Store().Get(pairingChannelName(account.ID), envelope.SenderID)
		if record != nil {
			record.Code = code
		}
	}
	if record == nil {
		return false
	}
	content := fmt.Sprintf("Pairing required before this direct message can start tasks.\nVerification code: %s\nAsk an operator to verify it from the HopClaw management UI or with `hopclaw pairing verify %s`.\nThen send `/pair status` here.", record.Code, record.Code)
	return b.sendPairingReply(ctx, target, content, map[string]any{
		meta.KeyStatusKind: meta.StatusKindPairingRequired.String(),
		"pairing_channel":  pairingChannelName(account.ID),
		"pairing_user_id":  envelope.SenderID,
		"pairing_code":     record.Code,
	})
}

func (b *Bridge) replyPairingStatus(ctx context.Context, account ResolvedAccount, envelope inboundEnvelope, target channels.RunNotificationTarget) bool {
	record, err := b.pairing.Store().Get(pairingChannelName(account.ID), envelope.SenderID)
	if err != nil {
		return b.sendPairingReply(ctx, target, "No pairing record exists. Send `/pair` to create one.", map[string]any{
			meta.KeyStatusKind: meta.StatusKindPairingStatus.String(),
			"pairing_state":    "missing",
		})
	}
	switch record.Status {
	case channelpairing.StatusVerified:
		return b.sendPairingReply(ctx, target, "Pairing is verified. You can send tasks in this direct message.", map[string]any{
			meta.KeyStatusKind: meta.StatusKindPairingStatus.String(),
			"pairing_state":    "verified",
		})
	case channelpairing.StatusPending:
		return b.sendPairingReply(ctx, target, fmt.Sprintf("Pairing is pending.\nVerification code: %s\nExpires at: %s UTC", record.Code, record.CodeExpiresAt.Format(time.RFC3339)), map[string]any{
			meta.KeyStatusKind: meta.StatusKindPairingStatus.String(),
			"pairing_state":    "pending",
			"pairing_code":     record.Code,
		})
	default:
		return b.sendPairingReply(ctx, target, "Pairing is revoked. Send `/pair` to create a new verification code.", map[string]any{
			meta.KeyStatusKind: meta.StatusKindPairingStatus.String(),
			"pairing_state":    string(record.Status),
		})
	}
}

func (b *Bridge) replyPairingVerify(ctx context.Context, account ResolvedAccount, envelope inboundEnvelope, code string, target channels.RunNotificationTarget) bool {
	record, err := b.pairing.VerifyCode(code)
	if err != nil {
		return b.sendPairingReply(ctx, target, "Verification failed. Check the code or ask an operator to verify it from the management interface.", map[string]any{
			meta.KeyStatusKind: meta.StatusKindPairingVerifyFailed.String(),
		})
	}
	if record.Channel != pairingChannelName(account.ID) || record.UserID != envelope.SenderID {
		return b.sendPairingReply(ctx, target, "That verification code belongs to a different conversation.", map[string]any{
			meta.KeyStatusKind: meta.StatusKindPairingVerifyFailed.String(),
		})
	}
	return b.sendPairingReply(ctx, target, "Pairing verified. You can now send tasks in this direct message.", map[string]any{
		meta.KeyStatusKind: meta.StatusKindPairingVerified.String(),
		"pairing_state":    "verified",
	})
}

func (b *Bridge) replyPairingRevoke(ctx context.Context, account ResolvedAccount, envelope inboundEnvelope, target channels.RunNotificationTarget) bool {
	if err := b.pairing.Revoke(pairingChannelName(account.ID), envelope.SenderID); err != nil {
		return b.sendPairingReply(ctx, target, "No verified pairing exists for this direct message.", map[string]any{
			meta.KeyStatusKind: meta.StatusKindPairingRevoke.String(),
			"pairing_state":    "missing",
		})
	}
	return b.sendPairingReply(ctx, target, "Pairing revoked for this direct message.", map[string]any{
		meta.KeyStatusKind: meta.StatusKindPairingRevoke.String(),
		"pairing_state":    "revoked",
	})
}

func (b *Bridge) sendPairingReply(ctx context.Context, target channels.RunNotificationTarget, content string, extra map[string]any) bool {
	if b == nil || b.adapter == nil || strings.TrimSpace(target.TargetID) == "" {
		return false
	}
	metadata := map[string]any{}
	for key, value := range target.Metadata {
		metadata[key] = value
	}
	for key, value := range extra {
		metadata[key] = value
	}
	return b.adapter.Send(ctx, channels.OutboundMessage{
		ChannelID: "feishu",
		TargetID:  target.TargetID,
		ReplyToID: target.ReplyToID,
		Content:   content,
		Format:    "text",
		Metadata:  metadata,
	}) == nil
}
