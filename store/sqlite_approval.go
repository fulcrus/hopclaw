package store

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"sync/atomic"
	"time"

	"github.com/fulcrus/hopclaw/approval"
)

// ---------------------------------------------------------------------------
// SQLiteApprovalStore
// ---------------------------------------------------------------------------

// SQLiteApprovalStore implements approval.Store.
type SQLiteApprovalStore struct {
	db     *sql.DB
	nextID atomic.Uint64
}

type approvalQueryer interface {
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

func NewSQLiteApprovalStore(db *sql.DB) *SQLiteApprovalStore {
	s := &SQLiteApprovalStore{db: db}
	s.nextID.Store(recoverMaxIDCounter(db, "approvals", "appr-"))
	return s
}

func (s *SQLiteApprovalStore) Create(ctx context.Context, ticket approval.Ticket) (*approval.Ticket, error) {
	now := time.Now().UTC()
	ticket.ID = fmt.Sprintf("appr-%06d", s.nextID.Add(1))
	ticket.Status = approval.StatusPending
	ticket.CreatedAt = now
	toolCallsJSON, err := marshalJSONSliceValue(ticket.ToolCalls)
	if err != nil {
		return nil, fmt.Errorf("marshal approval tool calls: %w", err)
	}
	reasonsJSON, err := marshalJSONSliceValue(ticket.Reasons)
	if err != nil {
		return nil, fmt.Errorf("marshal approval reasons: %w", err)
	}
	metadataJSON, err := marshalJSONValue(ticket.Metadata)
	if err != nil {
		return nil, fmt.Errorf("marshal approval metadata: %w", err)
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin create approval tx: %w", err)
	}
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO approvals (id, run_id, session_id, kind, status, tool_calls, reasons, metadata, scope, note, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		ticket.ID, ticket.RunID, ticket.SessionID,
		string(ticket.Kind), string(ticket.Status),
		toolCallsJSON,
		reasonsJSON,
		metadataJSON,
		string(ticket.Scope),
		ticket.Note,
		formatTime(now),
	); err != nil {
		_ = tx.Rollback()
		return nil, fmt.Errorf("create approval: %w", err)
	}
	for _, ref := range ticket.External {
		_, merged, err := approval.UpsertExternalReferences(nil, ref)
		if err != nil {
			_ = tx.Rollback()
			return nil, err
		}
		externalMetadataJSON, err := marshalJSONValue(merged.Metadata)
		if err != nil {
			_ = tx.Rollback()
			return nil, fmt.Errorf("marshal approval external metadata: %w", err)
		}
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO approval_external_refs (approval_id, provider, external_id, url, status, metadata, synced_at)
			 VALUES (?, ?, ?, ?, ?, ?, ?)`,
			ticket.ID,
			merged.Provider,
			merged.ExternalID,
			merged.URL,
			merged.Status,
			externalMetadataJSON,
			formatTime(merged.SyncedAt),
		); err != nil {
			_ = tx.Rollback()
			return nil, fmt.Errorf("create approval external ref: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit create approval: %w", err)
	}
	return &ticket, nil
}

func (s *SQLiteApprovalStore) Get(ctx context.Context, ticketID string) (*approval.Ticket, error) {
	return s.loadApproval(ctx, s.db, fmt.Sprintf("approval %s not found", ticketID), selectApprovalSQL+` WHERE id = ?`, ticketID)
}

func (s *SQLiteApprovalStore) GetByRun(ctx context.Context, runID string) (*approval.Ticket, error) {
	return s.loadApproval(ctx, s.db, fmt.Sprintf("approval for run %s not found", runID), selectApprovalSQL+` WHERE run_id = ? ORDER BY created_at DESC LIMIT 1`, runID)
}

func (s *SQLiteApprovalStore) GetByExternal(ctx context.Context, provider, externalID string) (*approval.Ticket, error) {
	return s.loadApproval(ctx, s.db,
		fmt.Sprintf("approval for external reference %s/%s not found", strings.TrimSpace(provider), strings.TrimSpace(externalID)),
		selectApprovalSQL+
			` INNER JOIN approval_external_refs ON approval_external_refs.approval_id = approvals.id
		   WHERE approval_external_refs.provider = ? AND approval_external_refs.external_id = ?
		   ORDER BY approvals.created_at DESC LIMIT 1`,
		strings.TrimSpace(provider), strings.TrimSpace(externalID),
	)
}

func (s *SQLiteApprovalStore) List(ctx context.Context, filter approval.ListFilter) ([]*approval.Ticket, error) {
	filter = filter.Normalize()
	q := selectApprovalSQL
	var args []any
	if filter.Status != "" {
		q += ` WHERE status = ?`
		args = append(args, string(filter.Status))
	}
	q += ` ORDER BY created_at, id`
	if filter.Limit > 0 {
		q += ` LIMIT ?`
		args = append(args, filter.Limit)
	}
	if filter.Offset > 0 {
		if filter.Limit <= 0 {
			q += ` LIMIT -1`
		}
		q += ` OFFSET ?`
		args = append(args, filter.Offset)
	}

	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []*approval.Ticket
	for rows.Next() {
		t, err := scanApproval(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if err := s.hydrateExternalRefsBatch(ctx, s.db, out); err != nil {
		return nil, err
	}
	return out, nil
}

func (s *SQLiteApprovalStore) Resolve(ctx context.Context, ticketID string, resolution approval.Resolution) (*approval.Ticket, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	committed := false
	defer func() {
		if committed {
			return
		}
		_ = tx.Rollback()
	}()

	ticket, err := s.loadApproval(ctx, tx, fmt.Sprintf("approval %s not found", ticketID), selectApprovalSQL+` WHERE id = ?`, ticketID)
	if err != nil {
		return nil, err
	}
	if ticket.Status != approval.StatusPending {
		return nil, fmt.Errorf("ticket %s: %w", ticketID, approval.ErrAlreadyResolved)
	}
	resolution, err = approval.NormalizeResolution(ticket, resolution)
	if err != nil {
		return nil, err
	}

	now := time.Now().UTC()
	ticket.Status = resolution.Status
	ticket.ResolvedBy = resolution.ResolvedBy
	ticket.Note = resolution.Note
	ticket.Scope = resolution.Scope
	ticket.ResolvedAt = now

	result, err := tx.ExecContext(ctx,
		`UPDATE approvals SET status = ?, resolved_by = ?, note = ?, scope = ?, resolved_at = ?
		 WHERE id = ? AND status = ?`,
		string(ticket.Status), ticket.ResolvedBy, ticket.Note,
		string(ticket.Scope), formatTime(now), ticketID, string(approval.StatusPending),
	)
	if err != nil {
		return nil, fmt.Errorf("resolve approval: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return nil, fmt.Errorf("resolve approval rows affected: %w", err)
	}
	if rows == 0 {
		return nil, fmt.Errorf("ticket %s: %w", ticketID, approval.ErrAlreadyResolved)
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit resolve approval: %w", err)
	}
	committed = true
	return ticket, nil
}

func (s *SQLiteApprovalStore) UpsertExternalRef(ctx context.Context, ticketID string, ref approval.ExternalReference) (*approval.Ticket, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	committed := false
	defer func() {
		if committed {
			return
		}
		_ = tx.Rollback()
	}()

	ticket, err := s.loadApproval(ctx, tx, fmt.Sprintf("approval %s not found", ticketID), selectApprovalSQL+` WHERE id = ?`, ticketID)
	if err != nil {
		return nil, err
	}
	nextRefs, merged, err := approval.UpsertExternalReferences(ticket.External, ref)
	if err != nil {
		return nil, err
	}
	externalMetadataJSON, err := marshalJSONValue(merged.Metadata)
	if err != nil {
		return nil, fmt.Errorf("marshal approval external metadata: %w", err)
	}
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO approval_external_refs (approval_id, provider, external_id, url, status, metadata, synced_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT(approval_id, provider) DO UPDATE SET
		   external_id = excluded.external_id,
		   url = excluded.url,
		   status = excluded.status,
		   metadata = excluded.metadata,
		   synced_at = excluded.synced_at`,
		ticketID,
		merged.Provider,
		merged.ExternalID,
		merged.URL,
		merged.Status,
		externalMetadataJSON,
		formatTime(merged.SyncedAt),
	); err != nil {
		return nil, fmt.Errorf("upsert approval external ref: %w", err)
	}
	ticket.External = nextRefs
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit approval external ref: %w", err)
	}
	committed = true
	return ticket, nil
}

// ---------------------------------------------------------------------------
// Scan helpers
// ---------------------------------------------------------------------------

const selectApprovalSQL = `SELECT approvals.id, approvals.run_id, approvals.session_id, approvals.kind, approvals.status, approvals.tool_calls, approvals.reasons, approvals.metadata, approvals.scope, approvals.note, approvals.resolved_by, approvals.created_at, approvals.resolved_at FROM approvals`

func scanApproval(row interface{ Scan(...any) error }) (*approval.Ticket, error) {
	var (
		id, runID, sessionID        string
		kind, status                string
		toolCallsJSON, reasonsJSON  string
		metadataJSON                string
		scope, note, resolvedBy     string
		createdAtStr, resolvedAtStr string
	)
	if err := row.Scan(
		&id, &runID, &sessionID, &kind, &status,
		&toolCallsJSON, &reasonsJSON, &metadataJSON,
		&scope, &note, &resolvedBy,
		&createdAtStr, &resolvedAtStr,
	); err != nil {
		return nil, err
	}
	createdAt, err := parseTime(createdAtStr, "sqlite approvals", id, "created_at")
	if err != nil {
		return nil, err
	}
	resolvedAt, err := parseTime(resolvedAtStr, "sqlite approvals", id, "resolved_at")
	if err != nil {
		return nil, err
	}

	ticket := &approval.Ticket{
		ID:         id,
		RunID:      runID,
		SessionID:  sessionID,
		Kind:       approval.Kind(kind),
		Status:     approval.Status(status),
		Scope:      approval.Scope(scope),
		Note:       note,
		ResolvedBy: resolvedBy,
		CreatedAt:  createdAt,
		ResolvedAt: resolvedAt,
	}
	metadata, err := decodeJSONMapField(metadataJSON, "sqlite approvals", id, "metadata")
	if err != nil {
		return nil, err
	}
	ticket.Metadata = metadata

	if toolCallsJSON != "" && toolCallsJSON != "[]" {
		var calls []approval.ToolCall
		if err := decodeJSONField(toolCallsJSON, "sqlite approvals", id, "tool_calls", &calls); err != nil {
			return nil, err
		}
		ticket.ToolCalls = calls
	}
	reasons, err := decodeJSONStringSliceField(reasonsJSON, "sqlite approvals", id, "reasons")
	if err != nil {
		return nil, err
	}
	ticket.Reasons = reasons
	return ticket, nil
}

func (s *SQLiteApprovalStore) loadApproval(ctx context.Context, q approvalQueryer, notFoundMessage, query string, args ...any) (*approval.Ticket, error) {
	row := q.QueryRowContext(ctx, query, args...)
	ticket, err := scanApproval(row)
	if err != nil {
		if strings.TrimSpace(notFoundMessage) == "" {
			return nil, fmt.Errorf("approval not found")
		}
		return nil, fmt.Errorf("%s", notFoundMessage)
	}
	if err := s.hydrateExternalRefs(ctx, q, ticket); err != nil {
		return nil, err
	}
	return ticket, nil
}

func (s *SQLiteApprovalStore) hydrateExternalRefs(ctx context.Context, q approvalQueryer, ticket *approval.Ticket) error {
	return s.hydrateExternalRefsBatch(ctx, q, []*approval.Ticket{ticket})
}

func (s *SQLiteApprovalStore) hydrateExternalRefsBatch(ctx context.Context, q approvalQueryer, tickets []*approval.Ticket) error {
	if len(tickets) == 0 {
		return nil
	}
	byID := make(map[string]*approval.Ticket, len(tickets))
	placeholders := make([]string, 0, len(tickets))
	args := make([]any, 0, len(tickets))
	for _, ticket := range tickets {
		if ticket == nil {
			continue
		}
		ticket.External = nil
		id := strings.TrimSpace(ticket.ID)
		if id == "" {
			continue
		}
		byID[id] = ticket
		placeholders = append(placeholders, "?")
		args = append(args, id)
	}
	if len(args) == 0 {
		return nil
	}

	rows, err := q.QueryContext(ctx,
		`SELECT approval_id, provider, external_id, url, status, metadata, synced_at
		   FROM approval_external_refs
		  WHERE approval_id IN (`+strings.Join(placeholders, ",")+`)
		  ORDER BY approval_id, provider`,
		args...,
	)
	if err != nil {
		return fmt.Errorf("load approval external refs: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var (
			approvalID   string
			provider     string
			externalID   string
			url          string
			status       string
			metadataJSON string
			syncedAtStr  string
		)
		if err := rows.Scan(&approvalID, &provider, &externalID, &url, &status, &metadataJSON, &syncedAtStr); err != nil {
			return err
		}
		ticket := byID[strings.TrimSpace(approvalID)]
		if ticket == nil {
			continue
		}
		metadata, err := decodeJSONMapField(metadataJSON, "sqlite approval_external_refs", ticket.ID, "metadata")
		if err != nil {
			return err
		}
		syncedAt, err := parseTime(syncedAtStr, "sqlite approval_external_refs", ticket.ID, "synced_at")
		if err != nil {
			return err
		}
		ticket.External = append(ticket.External, approval.ExternalReference{
			Provider:   strings.TrimSpace(provider),
			ExternalID: strings.TrimSpace(externalID),
			URL:        strings.TrimSpace(url),
			Status:     strings.TrimSpace(status),
			Metadata:   metadata,
			SyncedAt:   syncedAt,
		})
	}
	return rows.Err()
}
