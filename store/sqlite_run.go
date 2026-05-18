package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"sync/atomic"
	"time"

	"github.com/fulcrus/hopclaw/agent"
	domaingov "github.com/fulcrus/hopclaw/internal/domain/governance"
	"github.com/fulcrus/hopclaw/planner"
)

// ---------------------------------------------------------------------------
// SQLiteRunStore
// ---------------------------------------------------------------------------

// SQLiteRunStore implements agent.RunStore and agent.RunLister.
type SQLiteRunStore struct {
	db     *sql.DB
	nextID atomic.Uint64
}

func NewSQLiteRunStore(db *sql.DB) *SQLiteRunStore {
	s := &SQLiteRunStore{db: db}
	s.nextID.Store(recoverMaxIDCounter(db, "runs", "run-"))
	return s
}

// ---------------------------------------------------------------------------
// RunStore interface
// ---------------------------------------------------------------------------

func (s *SQLiteRunStore) Seen(ctx context.Context, externalEventID string, within time.Duration) bool {
	if externalEventID == "" {
		return false
	}
	var updatedAtStr string
	err := s.db.QueryRowContext(ctx,
		`SELECT updated_at FROM runs WHERE input_event_id = ? LIMIT 1`,
		externalEventID).Scan(&updatedAtStr)
	if err != nil {
		return false
	}
	if within <= 0 {
		return true
	}
	updatedAt, err := parseTime(updatedAtStr, "sqlite runs", externalEventID, "updated_at")
	if err != nil {
		return false
	}
	return time.Since(updatedAt) <= within
}

func (s *SQLiteRunStore) FindByExternalEvent(ctx context.Context, externalEventID string) (*agent.Run, error) {
	row := s.db.QueryRowContext(ctx, selectRunSQL+` WHERE input_event_id = ? ORDER BY updated_at DESC LIMIT 1`, externalEventID)
	return scanRun(row)
}

func (s *SQLiteRunStore) Get(ctx context.Context, runID string) (*agent.Run, error) {
	return s.GetScoped(ctx, runID, agent.ScopeFilter{})
}

func (s *SQLiteRunStore) GetScoped(ctx context.Context, runID string, scope agent.ScopeFilter) (*agent.Run, error) {
	q := selectRunSQL + ` WHERE id = ?`
	args := []any{runID}
	row := s.db.QueryRowContext(ctx, q, args...)
	run, err := scanRun(row)
	if err != nil {
		return nil, fmt.Errorf("run %s not found", runID)
	}
	return run, nil
}

func (s *SQLiteRunStore) Create(ctx context.Context, sessionID string, msg agent.IncomingMessage, cfg agent.AgentConfig) (*agent.Run, error) {
	now := time.Now().UTC()
	model := msg.Model
	if model == "" {
		model = cfg.DefaultModel
	}
	run := &agent.Run{
		ID:                  fmt.Sprintf("run-%06d", s.nextID.Add(1)),
		SessionID:           sessionID,
		Scope:               agent.ScopeRefFromIncomingMessage(msg),
		ParentRunID:         strings.TrimSpace(msg.ParentRunID),
		InputEventID:        msg.ExternalEventID,
		Status:              agent.RunQueued,
		QueueMode:           cfg.QueueMode,
		Phase:               agent.PhasePreparing,
		ExecutionMode:       agent.ExecutionModeDirect,
		Model:               model,
		LastSessionRevision: 0,
		UpdatedAt:           now,
	}
	scopeJSON, err := marshalJSONValue(run.Scope)
	if err != nil {
		return nil, fmt.Errorf("marshal run scope: %w", err)
	}

	if _, err := s.db.ExecContext(ctx,
		`INSERT INTO runs (id, session_id, scope, parent_run_id, input_event_id, status, queue_mode, phase, execution_mode, model, last_session_revision, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		run.ID, run.SessionID, scopeJSON, run.ParentRunID, run.InputEventID,
		string(run.Status), string(run.QueueMode), string(run.Phase), string(run.ExecutionMode),
		run.Model, run.LastSessionRevision, formatTime(now),
	); err != nil {
		return nil, fmt.Errorf("create run: %w", err)
	}
	return run, nil
}

func (s *SQLiteRunStore) Update(ctx context.Context, run *agent.Run) error {
	if run == nil {
		return fmt.Errorf("run is required")
	}
	run.UpdatedAt = time.Now().UTC()

	planJSON := ""
	executionGraphJSON := ""
	workflowStateJSON := ""
	if run.Plan != nil {
		data, err := json.Marshal(run.Plan)
		if err != nil {
			return fmt.Errorf("marshal plan: %w", err)
		}
		planJSON = string(data)
	}
	if run.ExecutionGraph != nil {
		data, err := json.Marshal(run.ExecutionGraph)
		if err != nil {
			return fmt.Errorf("marshal execution graph: %w", err)
		}
		executionGraphJSON = string(data)
	}
	if run.WorkflowState != nil {
		data, err := json.Marshal(run.WorkflowState)
		if err != nil {
			return fmt.Errorf("marshal workflow state: %w", err)
		}
		workflowStateJSON = string(data)
	}
	triageJSON := ""
	preflightJSON := ""
	semanticSignalJSON := ""
	profileJSON := ""
	taskContractJSON := ""
	delegationJSON := ""
	governanceJSON := ""
	if run.Triage != nil {
		data, err := json.Marshal(run.Triage)
		if err != nil {
			return fmt.Errorf("marshal triage: %w", err)
		}
		triageJSON = string(data)
	}
	if run.Preflight != nil {
		data, err := json.Marshal(run.Preflight)
		if err != nil {
			return fmt.Errorf("marshal preflight: %w", err)
		}
		preflightJSON = string(data)
	}
	if run.SemanticSignal != nil {
		data, err := json.Marshal(run.SemanticSignal)
		if err != nil {
			return fmt.Errorf("marshal semantic signal: %w", err)
		}
		semanticSignalJSON = string(data)
	}
	if run.EffectiveProfile != nil {
		data, err := json.Marshal(run.EffectiveProfile)
		if err != nil {
			return fmt.Errorf("marshal effective profile: %w", err)
		}
		profileJSON = string(data)
	}
	if run.TaskContract != nil {
		data, err := json.Marshal(run.TaskContract)
		if err != nil {
			return fmt.Errorf("marshal task contract: %w", err)
		}
		taskContractJSON = string(data)
	}
	if run.Delegation != nil {
		data, err := json.Marshal(run.Delegation)
		if err != nil {
			return fmt.Errorf("marshal delegation: %w", err)
		}
		delegationJSON = string(data)
	}
	if run.Governance != nil {
		data, err := json.Marshal(run.Governance)
		if err != nil {
			return fmt.Errorf("marshal governance: %w", err)
		}
		governanceJSON = string(data)
	}
	scopeJSON, err := marshalJSONValue(run.Scope)
	if err != nil {
		return fmt.Errorf("marshal run scope: %w", err)
	}
	pendingToolsJSON, err := marshalJSONSliceValue(run.PendingTools)
	if err != nil {
		return fmt.Errorf("marshal pending tools: %w", err)
	}

	_, err = s.db.ExecContext(ctx,
		`UPDATE runs SET session_id = ?, scope = ?, parent_run_id = ?, input_event_id = ?,
		 status = ?, queue_mode = ?, phase = ?, execution_mode = ?, model = ?, last_session_revision = ?, tool_rounds = ?, tool_recovery_count = ?,
		 approval_id = ?, pending_tools = ?, error = ?, plan = ?, execution_graph = ?, workflow_state = ?, triage = ?, preflight = ?, semantic_signal = ?, effective_agent_profile = ?, task_contract = ?, delegation = ?, governance = ?,
		 started_at = ?, updated_at = ?, finished_at = ?
		 WHERE id = ?`,
		run.SessionID, scopeJSON, run.ParentRunID, run.InputEventID,
		string(run.Status), string(run.QueueMode), string(run.Phase), string(run.ExecutionMode),
		run.Model, run.LastSessionRevision, run.ToolRounds, run.ToolRecoveryCount,
		run.ApprovalID, pendingToolsJSON,
		run.Error, planJSON, executionGraphJSON, workflowStateJSON, triageJSON, preflightJSON, semanticSignalJSON, profileJSON, taskContractJSON, delegationJSON, governanceJSON,
		formatTime(run.StartedAt), formatTime(run.UpdatedAt), formatTime(run.FinishedAt),
		run.ID,
	)
	return err
}

func (s *SQLiteRunStore) ClaimQueuedRun(ctx context.Context, runID string) (*agent.Run, bool, error) {
	if runID == "" {
		return nil, false, fmt.Errorf("run id is required")
	}
	now := time.Now().UTC()
	result, err := s.db.ExecContext(ctx,
		`UPDATE runs
		    SET status = ?,
		        started_at = CASE WHEN started_at = '' THEN ? ELSE started_at END,
		        updated_at = ?
		  WHERE id = ? AND status = ?`,
		string(agent.RunRunning), formatTime(now), formatTime(now), runID, string(agent.RunQueued),
	)
	if err != nil {
		return nil, false, fmt.Errorf("claim queued run: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return nil, false, fmt.Errorf("claim queued run rows affected: %w", err)
	}
	run, err := s.Get(ctx, runID)
	if err != nil {
		return nil, false, err
	}
	return run, rows > 0, nil
}

// ---------------------------------------------------------------------------
// RunLister interface
// ---------------------------------------------------------------------------

func (s *SQLiteRunStore) List(ctx context.Context, filter agent.RunListFilter) ([]*agent.Run, error) {
	q := selectRunSQL + ` WHERE 1=1`
	var args []any
	if filter.SessionID != "" {
		q += ` AND session_id = ?`
		args = append(args, filter.SessionID)
	}
	if filter.Status != "" {
		q += ` AND status = ?`
		args = append(args, string(filter.Status))
	}
	q += ` ORDER BY updated_at DESC`
	if filter.Limit > 0 {
		q += fmt.Sprintf(` LIMIT %d`, filter.Limit)
	}

	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []*agent.Run
	for rows.Next() {
		run, err := scanRunRows(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, run)
	}
	return out, rows.Err()
}

// ---------------------------------------------------------------------------
// Scan helpers
// ---------------------------------------------------------------------------

const selectRunSQL = `SELECT id, session_id, scope, parent_run_id, input_event_id,
	status, queue_mode, phase, execution_mode, model, last_session_revision, tool_rounds, tool_recovery_count,
	approval_id, pending_tools, error, plan, execution_graph, workflow_state, triage, preflight, semantic_signal, effective_agent_profile, task_contract, delegation, governance,
	started_at, updated_at, finished_at
	FROM runs`

func scanRun(row interface{ Scan(...any) error }) (*agent.Run, error) {
	var (
		id, sessionID, scopeJSON, parentRunID, inputEventID string
		status, queueMode, phase, executionMode, model      string
		lastSessionRevision                                 int64
		toolRounds, toolRecoveryCount                       int
		approvalID, pendingToolsJSON, errStr                string
		planJSON, executionGraphJSON                        *string
		workflowStateJSON                                   *string
		triageJSON, preflightJSON                           *string
		semanticSignalJSON                                  *string
		profileJSON                                         *string
		taskContractJSON                                    *string
		delegationJSON, governanceJSON                      *string
		startedAtStr, updatedAtStr, finishedAtStr           string
	)
	if err := row.Scan(
		&id, &sessionID, &scopeJSON, &parentRunID, &inputEventID,
		&status, &queueMode, &phase, &executionMode, &model, &lastSessionRevision, &toolRounds, &toolRecoveryCount,
		&approvalID, &pendingToolsJSON, &errStr, &planJSON, &executionGraphJSON, &workflowStateJSON, &triageJSON, &preflightJSON, &semanticSignalJSON, &profileJSON, &taskContractJSON, &delegationJSON, &governanceJSON,
		&startedAtStr, &updatedAtStr, &finishedAtStr,
	); err != nil {
		return nil, err
	}
	startedAt, err := parseTime(startedAtStr, "sqlite runs", id, "started_at")
	if err != nil {
		return nil, err
	}
	updatedAt, err := parseTime(updatedAtStr, "sqlite runs", id, "updated_at")
	if err != nil {
		return nil, err
	}
	finishedAt, err := parseTime(finishedAtStr, "sqlite runs", id, "finished_at")
	if err != nil {
		return nil, err
	}

	run := &agent.Run{
		ID:                  id,
		SessionID:           sessionID,
		ParentRunID:         parentRunID,
		InputEventID:        inputEventID,
		Status:              agent.RunStatus(status),
		QueueMode:           agent.QueueMode(queueMode),
		Phase:               agent.RunPhase(phase),
		ExecutionMode:       agent.ExecutionMode(executionMode),
		Model:               model,
		LastSessionRevision: lastSessionRevision,
		ToolRounds:          toolRounds,
		ToolRecoveryCount:   toolRecoveryCount,
		ApprovalID:          approvalID,
		Error:               errStr,
		StartedAt:           startedAt,
		UpdatedAt:           updatedAt,
		FinishedAt:          finishedAt,
	}
	if strings.TrimSpace(scopeJSON) != "" && strings.TrimSpace(scopeJSON) != "{}" {
		if err := decodeJSONField(scopeJSON, "sqlite runs", id, "scope", &run.Scope); err != nil {
			return nil, err
		}
	}

	if pendingToolsJSON != "" && pendingToolsJSON != "[]" {
		var tools []agent.ToolCall
		if err := decodeJSONField(pendingToolsJSON, "sqlite runs", id, "pending_tools", &tools); err != nil {
			return nil, err
		}
		run.PendingTools = tools
	}
	if planJSON != nil && *planJSON != "" {
		var p planner.Plan
		if err := decodeJSONField(*planJSON, "sqlite runs", id, "plan", &p); err != nil {
			return nil, err
		}
		run.Plan = &p
	}
	if executionGraphJSON != nil && *executionGraphJSON != "" {
		var graph agent.ExecutionGraph
		if err := decodeJSONField(*executionGraphJSON, "sqlite runs", id, "execution_graph", &graph); err != nil {
			return nil, err
		}
		run.ExecutionGraph = &graph
	}
	if workflowStateJSON != nil && *workflowStateJSON != "" {
		var state agent.WorkflowState
		if err := decodeJSONField(*workflowStateJSON, "sqlite runs", id, "workflow_state", &state); err != nil {
			return nil, err
		}
		run.WorkflowState = &state
	}
	if triageJSON != nil && *triageJSON != "" {
		var trace agent.RunTriageTrace
		if err := decodeJSONField(*triageJSON, "sqlite runs", id, "triage", &trace); err != nil {
			return nil, err
		}
		run.Triage = &trace
	}
	if preflightJSON != nil && *preflightJSON != "" {
		var report agent.RunPreflightReport
		if err := decodeJSONField(*preflightJSON, "sqlite runs", id, "preflight", &report); err != nil {
			return nil, err
		}
		run.Preflight = &report
	}
	if semanticSignalJSON != nil && *semanticSignalJSON != "" {
		var signal agent.SemanticSignal
		if err := decodeJSONField(*semanticSignalJSON, "sqlite runs", id, "semantic_signal", &signal); err != nil {
			return nil, err
		}
		run.SemanticSignal = &signal
	}
	if profileJSON != nil && *profileJSON != "" {
		var profile agent.EffectiveAgentProfile
		if err := decodeJSONField(*profileJSON, "sqlite runs", id, "effective_agent_profile", &profile); err != nil {
			return nil, err
		}
		run.EffectiveProfile = &profile
	}
	if taskContractJSON != nil && *taskContractJSON != "" {
		var contract agent.TaskContract
		if err := decodeJSONField(*taskContractJSON, "sqlite runs", id, "task_contract", &contract); err != nil {
			return nil, err
		}
		run.TaskContract = &contract
	}
	if delegationJSON != nil && *delegationJSON != "" {
		var contract agent.DelegationContract
		if err := decodeJSONField(*delegationJSON, "sqlite runs", id, "delegation", &contract); err != nil {
			return nil, err
		}
		run.Delegation = &contract
	}
	if governanceJSON != nil && *governanceJSON != "" {
		var evaluation domaingov.Evaluation
		if err := decodeJSONField(*governanceJSON, "sqlite runs", id, "governance", &evaluation); err != nil {
			return nil, err
		}
		run.Governance = &evaluation
	}
	return run, nil
}

func scanRunRows(rows *sql.Rows) (*agent.Run, error) {
	return scanRun(rows)
}

func (s *SQLiteRunStore) PruneRuns(ctx context.Context, before time.Time) (int, error) {
	if before.IsZero() {
		return 0, nil
	}
	result, err := s.db.ExecContext(ctx,
		`DELETE FROM runs
		  WHERE status IN (?, ?, ?)
		    AND finished_at != ''
		    AND finished_at < ?`,
		string(agent.RunCompleted), string(agent.RunFailed), string(agent.RunCancelled), formatTime(before),
	)
	if err != nil {
		return 0, fmt.Errorf("prune runs: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("prune runs rows affected: %w", err)
	}
	return int(rows), nil
}
