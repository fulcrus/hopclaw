package agent

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/fulcrus/hopclaw/contextengine"
	"github.com/fulcrus/hopclaw/internal/jsonrepair"
	"github.com/fulcrus/hopclaw/internal/semanticschema"
)

const DefaultTaskContractAnalyzerTimeout = 2 * time.Second

type ModelTaskContractAnalyzer struct {
	model        ModelClient
	timeout      time.Duration
	defaultModel string
}

func NewModelTaskContractAnalyzer(model ModelClient, timeout time.Duration) *ModelTaskContractAnalyzer {
	if model == nil {
		return nil
	}
	if timeout <= 0 {
		timeout = DefaultTaskContractAnalyzerTimeout
	}
	return &ModelTaskContractAnalyzer{model: model, timeout: timeout}
}

func (a *ModelTaskContractAnalyzer) WithDefaultModel(model string) *ModelTaskContractAnalyzer {
	if a != nil {
		a.defaultModel = strings.TrimSpace(model)
	}
	return a
}

func (a *ModelTaskContractAnalyzer) Analyze(ctx context.Context, req TaskContractAnalysisRequest) (TaskContractAnalysis, error) {
	if a == nil || a.model == nil {
		return TaskContractAnalysis{}, context.Canceled
	}
	ctx, cancel := context.WithTimeout(ctx, a.timeout)
	defer cancel()

	modelName := strings.TrimSpace(req.Model)
	if a.defaultModel != "" {
		modelName = a.defaultModel
	}
	response, err := a.model.Chat(ctx, ChatRequest{
		Model:        modelName,
		SystemPrompt: taskContractAnalyzerSystemPrompt,
		Messages: []contextengine.Message{{
			Role:    contextengine.RoleUser,
			Content: marshalTaskContractAnalysisRequest(req),
		}},
		Budget: contextengine.Budget{
			ContextWindow:  3072,
			MaxInputTokens: 1400,
			ReservedOutput: 320,
		},
	})
	if err != nil {
		return TaskContractAnalysis{}, err
	}
	if response == nil {
		return TaskContractAnalysis{}, ErrModelClientNil
	}
	return normalizeTaskContractAnalysis(parseTaskContractAnalysis(response.Message.Content)), nil
}

var taskContractAnalyzerSystemPrompt = semanticschema.BuildTaskContractAnalyzerPrompt()

func marshalTaskContractAnalysisRequest(req TaskContractAnalysisRequest) string {
	body, err := json.Marshal(req)
	if err != nil {
		return `{}`
	}
	return string(body)
}

func parseTaskContractAnalysis(raw string) TaskContractAnalysis {
	var analysis TaskContractAnalysis
	if err := jsonrepair.DecodeJSONObjectCandidate(raw, &analysis); err != nil {
		return TaskContractAnalysis{}
	}
	var fields map[string]json.RawMessage
	if err := jsonrepair.DecodeJSONObjectCandidate(raw, &fields); err == nil {
		_, analysis.MissingInfoSpecified = fields["missing_info_ids"]
	}
	return analysis
}

func normalizeTaskContractAnalysis(analysis TaskContractAnalysis) TaskContractAnalysis {
	analysis.JobType = normalizeTaskContractJobType(analysis.JobType)
	analysis.TargetSummary = strings.TrimSpace(analysis.TargetSummary)
	analysis.SuggestedDomains = normalizeSemanticDomains(analysis.SuggestedDomains)
	analysis.CapabilityHints = normalizeCapabilityHints(analysis.CapabilityHints)
	analysis.DeliverableKinds = normalizeTaskContractDeliverableKinds(analysis.DeliverableKinds)
	missingInfoIDs := normalizeTaskContractMissingInfoIDs(analysis.MissingInfoIDs)
	if analysis.MissingInfoSpecified && missingInfoIDs == nil {
		analysis.MissingInfoIDs = []string{}
	} else {
		analysis.MissingInfoIDs = missingInfoIDs
	}
	if analysis.Confidence < 0 {
		analysis.Confidence = 0
	}
	if analysis.Confidence > 1 {
		analysis.Confidence = 1
	}
	return analysis
}
