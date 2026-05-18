package store

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"fmt"
	"time"

	"github.com/fulcrus/hopclaw/usage"
)

// ---------------------------------------------------------------------------
// SQLiteUsageStore
// ---------------------------------------------------------------------------

// SQLiteUsageStore implements usage.Store.
type SQLiteUsageStore struct {
	db *sql.DB
}

func NewSQLiteUsageStore(db *sql.DB) *SQLiteUsageStore {
	return &SQLiteUsageStore{db: db}
}

func (s *SQLiteUsageStore) Record(ctx context.Context, rec usage.Record) error {
	if rec.ID == "" {
		b := make([]byte, 12)
		if _, err := rand.Read(b); err != nil {
			return fmt.Errorf("generate usage id: %w", err)
		}
		rec.ID = hex.EncodeToString(b)
	}
	if rec.CreatedAt.IsZero() {
		rec.CreatedAt = time.Now().UTC()
	}

	_, err := s.db.ExecContext(ctx,
		`INSERT INTO usage_records (id, session_id, run_id, workflow_id, parent_run_id, continuation_index, model, provider,
		 prompt_tokens, completion_tokens, total_tokens, cost_estimate,
		 duration_ns, tool_name, tool_call_id, record_type, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		rec.ID, rec.SessionID, rec.RunID, rec.WorkflowID, rec.ParentRunID, rec.ContinuationIndex, rec.Model, rec.Provider,
		rec.PromptTokens, rec.CompletionTokens, rec.TotalTokens, rec.CostEstimate,
		int64(rec.Duration), rec.ToolName, rec.ToolCallID,
		string(rec.RecordType), formatTime(rec.CreatedAt),
	)
	return err
}

func (s *SQLiteUsageStore) Query(ctx context.Context, filter usage.QueryFilter) ([]usage.Record, error) {
	q := `SELECT id, session_id, run_id, workflow_id, parent_run_id, continuation_index, model, provider,
		prompt_tokens, completion_tokens, total_tokens, cost_estimate,
		duration_ns, tool_name, tool_call_id, record_type, created_at
		FROM usage_records WHERE 1=1`
	args := usageFilterArgs(&q, filter)
	q += ` ORDER BY created_at DESC`
	if filter.Limit > 0 {
		q += fmt.Sprintf(` LIMIT %d`, filter.Limit)
	}

	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []usage.Record
	for rows.Next() {
		rec, err := scanUsageRecord(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, rec)
	}
	return out, rows.Err()
}

func (s *SQLiteUsageStore) Summarize(ctx context.Context, filter usage.QueryFilter) (*usage.Summary, error) {
	q := `SELECT model,
		SUM(prompt_tokens), SUM(completion_tokens), SUM(total_tokens),
		SUM(cost_estimate), COUNT(*)
		FROM usage_records WHERE 1=1`
	args := usageFilterArgs(&q, filter)
	q += ` GROUP BY model`

	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	summary := &usage.Summary{ByModel: make(map[string]usage.ModelUsage)}
	for rows.Next() {
		var (
			model                            string
			prompt, completion, total, count int
			cost                             float64
		)
		if err := rows.Scan(&model, &prompt, &completion, &total, &cost, &count); err != nil {
			return nil, err
		}
		summary.ByModel[model] = usage.ModelUsage{
			PromptTokens: prompt, CompletionTokens: completion,
			TotalTokens: total, CostEstimate: cost, CallCount: count,
		}
		summary.TotalPromptTokens += prompt
		summary.TotalCompletionTokens += completion
		summary.TotalTokens += total
		summary.TotalCostEstimate += cost
		summary.RecordCount += count
	}
	return summary, rows.Err()
}

func (s *SQLiteUsageStore) SessionSummary(ctx context.Context, sessionID string) (*usage.SessionCostSummary, error) {
	out := &usage.SessionCostSummary{
		SessionID: sessionID,
		ByModel:   make(map[string]usage.ModelUsage),
		ByTool:    make(map[string]usage.ToolUsage),
	}

	// Model call aggregation.
	rows, err := s.db.QueryContext(ctx,
		`SELECT model, SUM(prompt_tokens), SUM(completion_tokens), SUM(total_tokens),
		 SUM(cost_estimate), SUM(duration_ns), COUNT(*), MIN(created_at), MAX(created_at)
		 FROM usage_records WHERE session_id = ? AND record_type = 'model_call'
		 GROUP BY model`, sessionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var (
			model                            string
			prompt, completion, total, count int
			cost                             float64
			durNs                            int64
			firstStr, lastStr                string
		)
		if err := rows.Scan(&model, &prompt, &completion, &total, &cost, &durNs, &count, &firstStr, &lastStr); err != nil {
			return nil, err
		}
		out.ByModel[model] = usage.ModelUsage{
			PromptTokens: prompt, CompletionTokens: completion,
			TotalTokens: total, CostEstimate: cost, CallCount: count,
		}
		out.TotalCost += cost
		out.TotalTokens += total
		out.TotalDuration += time.Duration(durNs)
		out.ModelCallCount += count
		first, err := parseTime(firstStr, "sqlite usage_records", sessionID, "min(created_at)")
		if err != nil {
			return nil, err
		}
		if !first.IsZero() && (out.FirstCallAt.IsZero() || first.Before(out.FirstCallAt)) {
			out.FirstCallAt = first
		}
		last, err := parseTime(lastStr, "sqlite usage_records", sessionID, "max(created_at)")
		if err != nil {
			return nil, err
		}
		if !last.IsZero() && last.After(out.LastCallAt) {
			out.LastCallAt = last
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	rows.Close()

	// Tool execution aggregation.
	toolRows, err := s.db.QueryContext(ctx,
		`SELECT tool_name, COUNT(*), SUM(duration_ns)
		 FROM usage_records WHERE session_id = ? AND record_type = 'tool_execution'
		 GROUP BY tool_name`, sessionID)
	if err != nil {
		return nil, err
	}
	defer toolRows.Close()
	for toolRows.Next() {
		var (
			toolName string
			count    int
			durNs    int64
		)
		if err := toolRows.Scan(&toolName, &count, &durNs); err != nil {
			return nil, err
		}
		totalDur := time.Duration(durNs)
		var avg time.Duration
		if count > 0 {
			avg = totalDur / time.Duration(count)
		}
		out.ByTool[toolName] = usage.ToolUsage{
			CallCount: count, TotalDuration: totalDur, AvgDuration: avg,
		}
		out.ToolExecutionCount += count
	}
	return out, toolRows.Err()
}

func (s *SQLiteUsageStore) DailySummary(ctx context.Context, filter usage.QueryFilter) ([]usage.DailyUsage, error) {
	q := `SELECT substr(created_at, 1, 10) AS date, model,
		SUM(prompt_tokens), SUM(completion_tokens), SUM(total_tokens),
		SUM(cost_estimate), COUNT(*)
		FROM usage_records WHERE 1=1`
	args := usageFilterArgs(&q, filter)
	q += ` GROUP BY date, model ORDER BY date`

	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	byDate := make(map[string]*usage.DailyUsage)
	var order []string
	for rows.Next() {
		var (
			date, model                      string
			prompt, completion, total, count int
			cost                             float64
		)
		if err := rows.Scan(&date, &model, &prompt, &completion, &total, &cost, &count); err != nil {
			return nil, err
		}
		d, ok := byDate[date]
		if !ok {
			d = &usage.DailyUsage{Date: date, ByModel: make(map[string]usage.ModelUsage)}
			byDate[date] = d
			order = append(order, date)
		}
		d.PromptTokens += prompt
		d.CompletionTokens += completion
		d.TotalTokens += total
		d.CostEstimate += cost
		d.CallCount += count
		d.ByModel[model] = usage.ModelUsage{
			PromptTokens: prompt, CompletionTokens: completion,
			TotalTokens: total, CostEstimate: cost, CallCount: count,
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	out := make([]usage.DailyUsage, 0, len(order))
	for _, date := range order {
		out = append(out, *byDate[date])
	}
	return out, nil
}

func (s *SQLiteUsageStore) ProviderSummary(ctx context.Context, filter usage.QueryFilter) (map[string]*usage.ProviderUsage, error) {
	q := `SELECT provider, model,
		SUM(prompt_tokens), SUM(completion_tokens), SUM(total_tokens),
		SUM(cost_estimate), COUNT(*)
		FROM usage_records WHERE 1=1`
	args := usageFilterArgs(&q, filter)
	q += ` GROUP BY provider, model`

	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make(map[string]*usage.ProviderUsage)
	for rows.Next() {
		var (
			provider, model                  string
			prompt, completion, total, count int
			cost                             float64
		)
		if err := rows.Scan(&provider, &model, &prompt, &completion, &total, &cost, &count); err != nil {
			return nil, err
		}
		pu, ok := out[provider]
		if !ok {
			pu = &usage.ProviderUsage{Provider: provider, ByModel: make(map[string]usage.ModelUsage)}
			out[provider] = pu
		}
		pu.PromptTokens += prompt
		pu.CompletionTokens += completion
		pu.TotalTokens += total
		pu.CostEstimate += cost
		pu.CallCount += count
		pu.ByModel[model] = usage.ModelUsage{
			PromptTokens: prompt, CompletionTokens: completion,
			TotalTokens: total, CostEstimate: cost, CallCount: count,
		}
	}
	return out, rows.Err()
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func usageFilterArgs(q *string, filter usage.QueryFilter) []any {
	var args []any
	if filter.SessionID != "" {
		*q += ` AND session_id = ?`
		args = append(args, filter.SessionID)
	}
	if filter.RunID != "" {
		*q += ` AND run_id = ?`
		args = append(args, filter.RunID)
	}
	if filter.WorkflowID != "" {
		*q += ` AND workflow_id = ?`
		args = append(args, filter.WorkflowID)
	}
	if filter.Model != "" {
		*q += ` AND model = ?`
		args = append(args, filter.Model)
	}
	if filter.RecordType != "" {
		*q += ` AND record_type = ?`
		args = append(args, string(filter.RecordType))
	}
	if !filter.Since.IsZero() {
		*q += ` AND created_at >= ?`
		args = append(args, formatTime(filter.Since))
	}
	if !filter.Until.IsZero() {
		*q += ` AND created_at <= ?`
		args = append(args, formatTime(filter.Until))
	}
	return args
}

func scanUsageRecord(rows *sql.Rows) (usage.Record, error) {
	var (
		id, sessionID, runID, workflowID, parentRunID, model, provider string
		continuationIndex                                              int
		prompt, completion, total                                      int
		cost                                                           float64
		durNs                                                          int64
		toolName, toolCallID, recordType                               string
		createdAtStr                                                   string
	)
	if err := rows.Scan(
		&id, &sessionID, &runID, &workflowID, &parentRunID, &continuationIndex, &model, &provider,
		&prompt, &completion, &total, &cost,
		&durNs, &toolName, &toolCallID, &recordType, &createdAtStr,
	); err != nil {
		return usage.Record{}, err
	}
	createdAt, err := parseTime(createdAtStr, "sqlite usage_records", id, "created_at")
	if err != nil {
		return usage.Record{}, err
	}
	return usage.Record{
		ID: id, SessionID: sessionID, RunID: runID,
		WorkflowID: workflowID, ParentRunID: parentRunID, ContinuationIndex: continuationIndex,
		Model: model, Provider: provider,
		PromptTokens: prompt, CompletionTokens: completion, TotalTokens: total,
		CostEstimate: cost, Duration: time.Duration(durNs),
		ToolName: toolName, ToolCallID: toolCallID,
		RecordType: usage.RecordType(recordType),
		CreatedAt:  createdAt,
	}, nil
}
