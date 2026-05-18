package usage

import "time"

// RecordType distinguishes between model calls and tool executions.
type RecordType string

const (
	RecordTypeModelCall     RecordType = "model_call"
	RecordTypeToolExecution RecordType = "tool_execution"
)

// Record captures token usage for a single model call or tool execution.
type Record struct {
	ID                string        `json:"id"`
	RunID             string        `json:"run_id"`
	WorkflowID        string        `json:"workflow_id,omitempty"`
	ParentRunID       string        `json:"parent_run_id,omitempty"`
	ContinuationIndex int           `json:"continuation_index,omitempty"`
	SessionID         string        `json:"session_id"`
	Model             string        `json:"model"`
	Provider          string        `json:"provider"`
	PromptTokens      int           `json:"prompt_tokens"`
	CompletionTokens  int           `json:"completion_tokens"`
	TotalTokens       int           `json:"total_tokens"`
	CostEstimate      float64       `json:"cost_estimate,omitempty"`
	Duration          time.Duration `json:"duration,omitempty"`
	ToolName          string        `json:"tool_name,omitempty"`
	ToolCallID        string        `json:"tool_call_id,omitempty"`
	RecordType        RecordType    `json:"record_type"`
	CreatedAt         time.Time     `json:"created_at"`
}

// Summary aggregates token usage across multiple records.
type Summary struct {
	TotalPromptTokens     int                   `json:"total_prompt_tokens"`
	TotalCompletionTokens int                   `json:"total_completion_tokens"`
	TotalTokens           int                   `json:"total_tokens"`
	TotalCostEstimate     float64               `json:"total_cost_estimate"`
	RecordCount           int                   `json:"record_count"`
	ByModel               map[string]ModelUsage `json:"by_model"`
}

// ModelUsage aggregates token usage for a single model.
type ModelUsage struct {
	PromptTokens     int     `json:"prompt_tokens"`
	CompletionTokens int     `json:"completion_tokens"`
	TotalTokens      int     `json:"total_tokens"`
	CostEstimate     float64 `json:"cost_estimate"`
	CallCount        int     `json:"call_count"`
}

// SessionCostSummary provides a comprehensive cost breakdown for a session.
type SessionCostSummary struct {
	SessionID          string                `json:"session_id"`
	TotalCost          float64               `json:"total_cost"`
	TotalTokens        int                   `json:"total_tokens"`
	TotalDuration      time.Duration         `json:"total_duration"`
	ModelCallCount     int                   `json:"model_call_count"`
	ToolExecutionCount int                   `json:"tool_execution_count"`
	ByModel            map[string]ModelUsage `json:"by_model"`
	ByTool             map[string]ToolUsage  `json:"by_tool"`
	FirstCallAt        time.Time             `json:"first_call_at"`
	LastCallAt         time.Time             `json:"last_call_at"`
}

// ToolUsage aggregates execution metrics for a single tool.
type ToolUsage struct {
	CallCount     int           `json:"call_count"`
	TotalDuration time.Duration `json:"total_duration"`
	AvgDuration   time.Duration `json:"avg_duration"`
}

// DailyUsage aggregates usage for a single UTC day.
type DailyUsage struct {
	Date             string                `json:"date"` // "2025-01-15"
	PromptTokens     int                   `json:"prompt_tokens"`
	CompletionTokens int                   `json:"completion_tokens"`
	TotalTokens      int                   `json:"total_tokens"`
	CostEstimate     float64               `json:"cost_estimate"`
	CallCount        int                   `json:"call_count"`
	ByModel          map[string]ModelUsage `json:"by_model"`
}

// ProviderUsage aggregates usage for a single model provider.
type ProviderUsage struct {
	Provider         string                `json:"provider"`
	PromptTokens     int                   `json:"prompt_tokens"`
	CompletionTokens int                   `json:"completion_tokens"`
	TotalTokens      int                   `json:"total_tokens"`
	CostEstimate     float64               `json:"cost_estimate"`
	CallCount        int                   `json:"call_count"`
	ByModel          map[string]ModelUsage `json:"by_model"`
}

// QueryFilter specifies criteria for querying usage records.
type QueryFilter struct {
	SessionID  string
	RunID      string
	WorkflowID string
	Model      string
	RecordType RecordType
	Since      time.Time
	Until      time.Time
	Limit      int
}
