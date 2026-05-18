package repl

import (
	"context"
	"fmt"
	"strings"

	"github.com/fulcrus/hopclaw/acp"
	"github.com/fulcrus/hopclaw/approval"
)

func (r *REPL) handlePermission(ctx context.Context, req acp.PermissionRequest) error {
	r.pendingApproval = true
	r.prompt.SetApproval(true)
	r.approvalReturn = r.phase
	card := approvalCardFields(req)
	r.approvalState = approvalDockState{
		ID:    strings.TrimSpace(card.RequestID),
		Tool:  card.Action,
		Scope: card.Scope,
		Risk:  card.Impact,
	}
	r.transitionPhase(PhaseWaitingApproval, req.ToolName)
	r.renderApprovalSnapshot()
	defer func() {
		r.pendingApproval = false
		r.prompt.SetApproval(false)
		r.approvalState = approvalDockState{}
		if r.approvalReturn == "" {
			r.approvalReturn = PhaseExecutingTools
		}
		r.transitionPhase(r.approvalReturn, req.ToolName)
		r.approvalReturn = ""
	}()
	if !r.renderer.tty {
		r.renderer.RenderSystemEvent(fmt.Sprintf("Approval required for %s. Non-interactive mode: denied.", card.Action))
		return r.service.ResolvePermission(ctx, req, PermissionDecision{
			Approved: false,
			Scope:    "once",
		})
	}

	expanded := false
	var choice rune
	for {
		if r.quitConfirmPending {
			r.renderQuitConfirmation()
		} else {
			r.renderer.RenderApprovalCard(card, expanded)
		}
		value, err := r.prompter.ReadApproval(r.prompt.Approval())
		if err != nil {
			return err
		}
		if value == 12 {
			r.renderer.ClearScreen()
			continue
		}
		if r.quitConfirmPending {
			switch value {
			case 3, 'q':
				return r.quitTerminal(ctx)
			case 27, 'b':
				r.dismissQuitConfirmation()
			}
			continue
		}
		if value == 3 {
			if r.supportsQuitConfirmation() {
				r.renderQuitConfirmation()
				continue
			}
			return r.quitTerminal(ctx)
		}
		if value == 27 {
			if expanded {
				expanded = false
				continue
			}
			r.renderer.RenderSystemEvent("Approval still pending.")
			continue
		}
		if value == 'v' {
			expanded = !expanded
			continue
		}
		if value == 'q' || value == 'b' {
			continue
		}
		if value == 'a' && !card.AllowSession {
			r.renderer.RenderSystemEvent("Conversation approval is unavailable for this request.")
			continue
		}
		choice = value
		break
	}
	decision := PermissionDecision{Approved: choice == 'y' || choice == 'a', Scope: approvalDecisionScope(choice, req)}
	if !decision.Approved {
		decision.Scope = "deny"
	}
	if err := r.service.ResolvePermission(ctx, req, decision); err != nil {
		return err
	}
	if decision.Approved {
		r.renderer.RenderSystemEvent("Approved.")
	} else {
		r.renderer.RenderSystemEvent("Denied.")
	}
	return nil
}

type ApprovalCard struct {
	RequestID    string
	Action       string
	Reason       string
	Impact       string
	Input        string
	Scope        string
	AllowSession bool
}

func approvalCardFields(req acp.PermissionRequest) ApprovalCard {
	return ApprovalCard{
		RequestID:    strings.TrimSpace(req.RequestID),
		Action:       defaultString(req.ToolName, "tool.exec"),
		Reason:       defaultString(firstNonEmpty(req.Description, req.PolicySummary, req.GovernanceSummary), "Approval required by the current task."),
		Impact:       approvalImpact(req),
		Input:        strings.TrimSpace(req.Input),
		Scope:        approvalScopeSummary(req),
		AllowSession: approvalAllowsSessionGrant(req),
	}
}

func approvalImpact(req acp.PermissionRequest) string {
	switch {
	case req.RequiresExternalSideEffect:
		return "destructive"
	case strings.TrimSpace(req.PolicySummary) != "":
		return "review required"
	case strings.TrimSpace(req.GovernanceSummary) != "":
		return "review required"
	case strings.TrimSpace(req.PolicyAction) != "" && !strings.EqualFold(strings.TrimSpace(req.PolicyAction), "allow"):
		return "review required"
	default:
		return "review required"
	}
}

func approvalScopeSummary(req acp.PermissionRequest) string {
	scope := "once"
	if approvalAllowsSessionGrant(req) {
		scope = "once | conversation"
	}
	summaries := make([]string, 0, 2)
	if summary := strings.TrimSpace(req.ScopeSummary); summary != "" {
		summaries = append(summaries, summary)
	}
	if summary := strings.TrimSpace(req.ResourceScopeSummary); summary != "" {
		summaries = append(summaries, summary)
	}
	if len(summaries) == 0 {
		return scope
	}
	return scope + " · " + strings.Join(summaries, " · ")
}

func approvalAllowsSessionGrant(req acp.PermissionRequest) bool {
	switch normalizedApprovalScope(req.MaxGrantScope) {
	case approval.ScopeSession, approval.ScopeAlways:
		return true
	default:
		return false
	}
}

func approvalDecisionScope(choice rune, req acp.PermissionRequest) string {
	if choice != 'y' && choice != 'a' {
		return "deny"
	}
	if choice != 'a' {
		return "once"
	}
	switch normalizedApprovalScope(req.MaxGrantScope) {
	case approval.ScopeAlways:
		return string(approval.ScopeAlways)
	case approval.ScopeSession:
		return string(approval.ScopeSession)
	default:
		return "once"
	}
}

func normalizedApprovalScope(value string) approval.Scope {
	scope, err := approval.NormalizeScope(approval.Scope(strings.TrimSpace(value)))
	if err != nil {
		return ""
	}
	return scope
}
