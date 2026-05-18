package runtime

import (
	"context"
	"fmt"
	"strings"

	"github.com/fulcrus/hopclaw/agent"
	"github.com/fulcrus/hopclaw/contextengine"
	"github.com/fulcrus/hopclaw/eventbus"
	"github.com/fulcrus/hopclaw/resultmodel"
	verifyrt "github.com/fulcrus/hopclaw/runtime/verify"
)

func (s *Service) GetRunVerification(ctx context.Context, id string) (*verifyrt.RunVerification, error) {
	state, err := s.buildRunCompletionState(ctx, id)
	if err != nil {
		return nil, err
	}
	return state.verification, nil
}

func (s *Service) getRunVerification(ctx context.Context, run *agent.Run, result *RunResult, session *agent.Session) (*verifyrt.RunVerification, error) {
	input, err := s.buildRunVerificationInput(ctx, run, result, session)
	if err != nil {
		return nil, err
	}
	verification := verifyrt.Evaluate(input, verifyrt.WithPolicy(s.verificationPolicy))
	return &verification, nil
}

func (s *Service) buildRunVerificationInput(ctx context.Context, run *agent.Run, result *RunResult, session *agent.Session) (verifyrt.Input, error) {
	input := verifyrt.Input{}
	if run == nil {
		return input, fmt.Errorf("run is required")
	}
	if result == nil {
		return input, fmt.Errorf("run result is required")
	}

	input.RunID = run.ID
	input.SessionID = run.SessionID
	input.Status = string(result.Status)
	input.Error = strings.TrimSpace(result.Error)
	input.Summary = strings.TrimSpace(result.Summary)
	input.Output = strings.TrimSpace(result.Output)
	input.Deliverables = toVerificationDeliverables(result.Deliverables)
	events := s.EventSnapshotContext(ctx)
	input.ToolNames = mergeVerificationToolNames(
		collectToolNamesFromDeliverables(result.Deliverables),
		collectToolNamesFromEvents(events, run.ID),
	)
	input.Contract = toVerificationContract(run.TaskContract)
	if run.Plan != nil {
		input.PlanCoverageWarnings = append([]string(nil), run.Plan.CoverageWarnings...)
	}

	if session == nil {
		input.ToolOutputs = collectToolOutputsFromEvents(events, run.ID)
		return input, nil
	}
	input.SessionKey = strings.TrimSpace(session.Key)
	toolResults := collectToolResultsFromMessagesForRun(session.Messages, run.ID)
	if len(toolResults) == 0 {
		toolResults = collectLegacyToolResultsForRun(session.Messages, run)
	}
	input.ToolNames = mergeVerificationToolNames(
		input.ToolNames,
		collectToolNamesFromResults(toolResults),
	)
	input.ToolOutputs = mergeVerificationToolOutputs(
		collectRunToolOutputs(session.Messages, run),
		collectToolOutputsFromEvents(events, run.ID),
	)
	return input, nil
}

func mergeVerificationToolOutputs(groups ...[]string) []string {
	seen := make(map[string]struct{})
	out := make([]string, 0, 8)
	for _, group := range groups {
		for _, raw := range group {
			value := strings.TrimSpace(raw)
			if value == "" {
				continue
			}
			if _, ok := seen[value]; ok {
				continue
			}
			seen[value] = struct{}{}
			out = append(out, value)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func mergeVerificationToolNames(groups ...[]string) []string {
	seen := make(map[string]struct{})
	out := make([]string, 0, 8)
	for _, group := range groups {
		for _, raw := range group {
			value := strings.TrimSpace(raw)
			if value == "" {
				continue
			}
			if _, ok := seen[value]; ok {
				continue
			}
			seen[value] = struct{}{}
			out = append(out, value)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func collectToolNamesFromResults(results []resultmodel.ToolResult) []string {
	if len(results) == 0 {
		return nil
	}
	out := make([]string, 0, len(results))
	for _, result := range results {
		if name := strings.TrimSpace(result.Normalized().ToolName); name != "" {
			out = append(out, name)
		}
	}
	return mergeVerificationToolNames(out)
}

func collectToolNamesFromDeliverables(items []DeliverableRef) []string {
	if len(items) == 0 {
		return nil
	}
	out := make([]string, 0, len(items))
	for _, item := range items {
		if name := strings.TrimSpace(item.ToolName); name != "" {
			out = append(out, name)
		}
	}
	return mergeVerificationToolNames(out)
}

func toVerificationContract(contract *agent.TaskContract) *verifyrt.Contract {
	if contract == nil {
		return nil
	}
	out := &verifyrt.Contract{
		Goal:                   strings.TrimSpace(contract.Goal),
		JobType:                strings.TrimSpace(contract.JobType),
		TargetSummary:          strings.TrimSpace(contract.TargetSummary),
		RequiresExternalEffect: contract.RequiresExternalEffect,
		RequiresApproval:       contract.RequiresApproval,
	}
	for _, item := range contract.ExpectedDeliverables {
		out.ExpectedDeliverables = append(out.ExpectedDeliverables, verifyrt.ContractDeliverable{
			Kind:     strings.TrimSpace(item.Kind),
			Required: item.Required,
		})
	}
	for _, item := range contract.AcceptanceCriteria {
		out.AcceptanceCriteria = append(out.AcceptanceCriteria, verifyrt.ContractAcceptance{
			ID:               strings.TrimSpace(item.ID),
			Summary:          strings.TrimSpace(item.Summary),
			Required:         item.Required,
			DeliverableKinds: append([]string(nil), item.DeliverableKinds...),
		})
	}
	for _, item := range contract.MissingInfo {
		out.MissingInfo = append(out.MissingInfo, verifyrt.ContractMissingInfo{
			ID:       strings.TrimSpace(item.ID),
			Summary:  strings.TrimSpace(item.Summary),
			Required: item.Required,
		})
	}
	if len(out.ExpectedDeliverables) == 0 {
		out.ExpectedDeliverables = nil
	}
	if len(out.AcceptanceCriteria) == 0 {
		out.AcceptanceCriteria = nil
	}
	if len(out.MissingInfo) == 0 {
		out.MissingInfo = nil
	}
	return out
}

func toVerificationDeliverables(items []DeliverableRef) []verifyrt.Deliverable {
	out := make([]verifyrt.Deliverable, 0, len(items))
	for _, item := range items {
		out = append(out, verifyrt.Deliverable{
			Kind:        strings.TrimSpace(item.Kind),
			URI:         strings.TrimSpace(item.URI),
			ToolName:    strings.TrimSpace(item.ToolName),
			ContentType: strings.TrimSpace(item.ContentType),
		})
	}
	return out
}

func collectToolNamesFromEvents(events []eventbus.Event, runID string) []string {
	if len(events) == 0 || strings.TrimSpace(runID) == "" {
		return nil
	}
	out := make([]string, 0, 8)
	seen := make(map[string]struct{}, 8)
	appendTool := func(name string) {
		name = strings.TrimSpace(name)
		if name == "" {
			return
		}
		if _, ok := seen[name]; ok {
			return
		}
		seen[name] = struct{}{}
		out = append(out, name)
	}
	for _, event := range events {
		if event.Type != eventbus.EventToolExecuted || event.RunID != runID {
			continue
		}
		if payload, ok := event.ToolExecutedPayload(); ok {
			for _, name := range payload.ToolNames {
				appendTool(name)
			}
			for _, result := range payload.Results {
				appendTool(result.ToolName)
			}
			continue
		}
	}
	return out
}

func collectRunToolOutputs(messages []contextengine.Message, run *agent.Run) []string {
	if len(messages) == 0 || run == nil {
		return nil
	}
	runMessages := collectToolMessagesForRun(messages, run.ID)
	if len(runMessages) == 0 {
		runMessages = collectLegacyToolMessagesInRunWindow(messages, run)
	}
	out := make([]string, 0, 8)
	for _, msg := range runMessages {
		content := strings.TrimSpace(msg.TextContent())
		if content == "" {
			continue
		}
		out = append(out, content)
	}
	return out
}
