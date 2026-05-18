package contextengine

import (
	"fmt"
	"strconv"
	"strings"
)

func budgetPlanDomains(run *Run) []string {
	if run == nil || len(run.DetectedDomains) == 0 {
		return nil
	}
	return dedupeStrings(append([]string(nil), run.DetectedDomains...))
}

func budgetPlanJobType(run *Run) string {
	if run == nil {
		return ""
	}
	return strings.TrimSpace(run.JobType)
}

func (e *SlidingWindowEngine) trimPromptBlockToBudget(content string, maxTokens int) string {
	content = strings.TrimSpace(content)
	if content == "" || maxTokens <= 0 {
		return ""
	}
	if e.config.Estimator == nil || e.config.Estimator.Estimate(content) <= maxTokens {
		return content
	}
	return softTrimContent(content, maxTokens*4, 3)
}

func (e *SlidingWindowEngine) trimPinnedFactsPromptToBudget(content string, maxTokens int) string {
	content = strings.TrimSpace(content)
	if content == "" || maxTokens <= 0 {
		return ""
	}
	if e.config.Estimator == nil || e.config.Estimator.Estimate(content) <= maxTokens {
		return content
	}

	trimmed := softTrimContent(content, maxTokens*4, 3)
	if !strings.Contains(trimmed, "[truncated]") {
		return trimmed
	}

	lines := strings.Split(content, "\n")
	if len(lines) <= 3 {
		return trimmed
	}

	headCount := 2
	if len(lines) < headCount {
		headCount = len(lines)
	}
	tailCount := 2
	if len(lines)-headCount < tailCount {
		tailCount = len(lines) - headCount
	}
	if tailCount <= 0 {
		return trimmed
	}

	omitted := len(lines) - headCount - tailCount
	parts := append([]string(nil), lines[:headCount]...)
	if omitted > 0 {
		parts = append(parts, fmt.Sprintf("... [%d lines omitted] ...", omitted))
	}
	parts = append(parts, lines[len(lines)-tailCount:]...)
	candidate := strings.TrimSpace(strings.Join(parts, "\n"))
	if candidate == "" || len(candidate) >= len(content) {
		return trimmed
	}
	return candidate
}

func (e *SlidingWindowEngine) trimMessagesToBudget(systemPrompt, summary string, messages []Message, maxInput int) []Message {
	if len(messages) == 0 {
		return messages
	}
	for len(messages) > 0 {
		total := e.config.Estimator.Estimate(systemPrompt)
		if summary != "" {
			total += e.config.Estimator.Estimate(summary)
		}
		total += e.config.Estimator.EstimateMessages(messages)
		if total <= maxInput {
			break
		}

		dropIdx := e.config.KeepFirstN
		if dropIdx >= len(messages) {
			dropIdx = 0
		}
		minScore := messageImportance(messages[dropIdx], dropIdx, len(messages))
		for i := dropIdx + 1; i < len(messages); i++ {
			if s := messageImportance(messages[i], i, len(messages)); s < minScore {
				minScore = s
				dropIdx = i
			}
		}
		messages = append(append([]Message(nil), messages[:dropIdx]...), messages[dropIdx+1:]...)
	}
	return messages
}

func softTrimContent(content string, maxChars, keepLines int) string {
	if len(content) <= maxChars {
		return content
	}
	if keepLines > 0 {
		lines := strings.Split(content, "\n")
		if len(lines) > keepLines*2 {
			omitted := len(lines) - keepLines*2
			head := strings.Join(lines[:keepLines], "\n")
			tail := strings.Join(lines[len(lines)-keepLines:], "\n")
			trimmed := fmt.Sprintf("%s\n... [%d lines omitted] ...\n%s", head, omitted, tail)
			if len(trimmed) < len(content) {
				return trimmed
			}
		}
	}
	return strings.TrimSpace(content[:maxChars]) + "\n[truncated]"
}

func messageImportance(msg Message, idx, total int) float64 {
	var score float64
	if total > 1 {
		score += float64(idx) / float64(total-1) * 0.5
	}
	switch msg.Role {
	case RoleUser:
		score += 0.35
	case RoleAssistant:
		score += 0.25
	case RoleTool:
		score += 0.05
	}
	upper := strings.ToUpper(msg.TextContent())
	score += messageImportanceAdjustment(msg)

	if msg.Role == RoleTool {
		for _, kw := range []string{"ERROR", "FAIL", "EXCEPTION", "PANIC", "DENIED", "TIMEOUT", "REFUSED", "NOT FOUND", "FATAL"} {
			if strings.Contains(upper, kw) {
				score += 0.15
				break
			}
		}
	}

	return score
}

func messageImportanceAdjustment(msg Message) float64 {
	if len(msg.Metadata) == 0 {
		return 0
	}
	value, ok := msg.Metadata[MetadataKeyMessageImportance]
	if !ok || value == nil {
		return 0
	}
	switch typed := value.(type) {
	case float64:
		return clampMessageImportanceAdjustment(typed)
	case float32:
		return clampMessageImportanceAdjustment(float64(typed))
	case int:
		return clampMessageImportanceAdjustment(float64(typed))
	case int64:
		return clampMessageImportanceAdjustment(float64(typed))
	case string:
		parsed, err := strconv.ParseFloat(strings.TrimSpace(typed), 64)
		if err != nil {
			return 0
		}
		return clampMessageImportanceAdjustment(parsed)
	default:
		return 0
	}
}

func clampMessageImportanceAdjustment(value float64) float64 {
	if value > 0.25 {
		return 0.25
	}
	if value < -0.25 {
		return -0.25
	}
	return value
}
