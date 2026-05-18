package store

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/fulcrus/hopclaw/artifact"
)

// ---------------------------------------------------------------------------
// SQLiteArtifactStore
// ---------------------------------------------------------------------------

// SQLiteArtifactStore implements artifact.Store with metadata in SQLite
// and binary bodies on the filesystem.
type SQLiteArtifactStore struct {
	db   *sql.DB
	root string // filesystem root for binary bodies
	rng  io.Reader
}

func NewSQLiteArtifactStore(db *sql.DB, root string) (*SQLiteArtifactStore, error) {
	if strings.TrimSpace(root) == "" {
		return nil, fmt.Errorf("artifact root is required")
	}
	if err := os.MkdirAll(root, 0o755); err != nil {
		return nil, err
	}
	store := &SQLiteArtifactStore{db: db, root: root}
	if err := store.reconcileBodies(); err != nil {
		return nil, err
	}
	return store, nil
}

func (s *SQLiteArtifactStore) Put(ctx context.Context, req artifact.PutRequest) (*artifact.Blob, error) {
	metadataJSON, err := artifact.MarshalMetadataJSON(req.Metadata)
	if err != nil {
		return nil, err
	}
	rng := s.rng
	if rng == nil {
		rng = rand.Reader
	}
	b := make([]byte, 12)
	if _, err := io.ReadFull(rng, b); err != nil {
		return nil, err
	}
	id := hex.EncodeToString(b)
	now := time.Now().UTC()

	kind := strings.TrimSpace(req.Kind)
	if kind == "" {
		kind = "tool_output"
	}
	ct := strings.TrimSpace(req.ContentType)
	if ct == "" {
		ct = "text/plain; charset=utf-8"
	}

	tempBodyPath := s.tempBodyPath(id)
	if err := os.WriteFile(tempBodyPath, req.Body, 0o644); err != nil {
		return nil, err
	}
	bodyPath := filepath.Join(s.root, id+".bin")

	// Extract first-class columns from metadata.
	runID, _ := req.Metadata["run_id"].(string)
	sessionID, _ := req.Metadata["session_id"].(string)
	toolName, _ := req.Metadata["tool_name"].(string)
	toolCallID, _ := req.Metadata["tool_call_id"].(string)

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		_ = os.Remove(tempBodyPath)
		return nil, err
	}
	finalized := false
	defer func() {
		if finalized {
			return
		}
		_ = tx.Rollback()
		_ = os.Remove(tempBodyPath)
		_ = os.Remove(bodyPath)
	}()

	if _, err := tx.ExecContext(ctx,
		`INSERT INTO artifacts (id, uri, kind, content_type, size, run_id, session_id, tool_name, tool_call_id, metadata, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		id, "artifact://local/"+id, kind, ct, int64(len(req.Body)),
		runID, sessionID, toolName, toolCallID,
		metadataJSON, formatTime(now),
	); err != nil {
		return nil, err
	}
	if err := os.Rename(tempBodyPath, bodyPath); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	finalized = true

	blob := &artifact.Blob{
		ID:          id,
		URI:         "artifact://local/" + id,
		Kind:        kind,
		ContentType: ct,
		Size:        int64(len(req.Body)),
		CreatedAt:   now,
		Metadata:    req.Metadata,
	}
	return blob, nil
}

func (s *SQLiteArtifactStore) Get(ctx context.Context, id string) (*artifact.Blob, error) {
	id = artifact.ParseID(id)
	row := s.db.QueryRowContext(ctx,
		`SELECT id, uri, kind, content_type, size, metadata, created_at
		 FROM artifacts WHERE id = ?`, id)
	return scanArtifact(row)
}

func (s *SQLiteArtifactStore) Read(ctx context.Context, id string) ([]byte, string, error) {
	id = artifact.ParseID(id)
	blob, err := s.Get(ctx, id)
	if err != nil {
		return nil, "", err
	}
	bodyPath := filepath.Join(s.root, id+".bin")
	body, err := os.ReadFile(bodyPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, "", fmt.Errorf("artifact %q not found", id)
		}
		return nil, "", err
	}
	return body, blob.ContentType, nil
}

func (s *SQLiteArtifactStore) List(ctx context.Context, filter artifact.ListFilter) ([]*artifact.Blob, error) {
	q := `SELECT id, uri, kind, content_type, size, metadata, created_at FROM artifacts WHERE 1=1`
	var args []any
	if filter.Kind != "" {
		q += ` AND kind = ?`
		args = append(args, filter.Kind)
	}
	if filter.RunID != "" {
		q += ` AND run_id = ?`
		args = append(args, filter.RunID)
	}
	if filter.SessionID != "" {
		q += ` AND session_id = ?`
		args = append(args, filter.SessionID)
	}
	if filter.ToolName != "" {
		q += ` AND tool_name = ?`
		args = append(args, filter.ToolName)
	}
	if filter.ToolCallID != "" {
		q += ` AND tool_call_id = ?`
		args = append(args, filter.ToolCallID)
	}
	if !filter.Before.IsZero() {
		q += ` AND created_at < ?`
		args = append(args, formatTime(filter.Before))
	}
	q += ` ORDER BY created_at DESC`
	if filter.Limit > 0 {
		q += fmt.Sprintf(` LIMIT %d`, filter.Limit)
	}

	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []*artifact.Blob
	for rows.Next() {
		b, err := scanArtifact(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	return out, rows.Err()
}

func (s *SQLiteArtifactStore) Delete(ctx context.Context, id string) error {
	id = artifact.ParseID(id)
	res, err := s.db.ExecContext(ctx, `DELETE FROM artifacts WHERE id = ?`, id)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("artifact %q not found", id)
	}
	bodyPath := filepath.Join(s.root, id+".bin")
	os.Remove(bodyPath) // best effort
	return nil
}

func (s *SQLiteArtifactStore) tempBodyPath(id string) string {
	return filepath.Join(s.root, id+".bin.tmp")
}

func (s *SQLiteArtifactStore) reconcileBodies() error {
	rows, err := s.db.Query(`SELECT id FROM artifacts`)
	if err != nil {
		return err
	}
	defer rows.Close()

	known := make(map[string]struct{})
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return err
		}
		known[id] = struct{}{}
	}
	if err := rows.Err(); err != nil {
		return err
	}

	entries, err := os.ReadDir(s.root)
	if err != nil {
		return err
	}
	finalBodies := make(map[string]struct{})
	tempBodies := make(map[string]struct{})
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		switch {
		case strings.HasSuffix(name, ".bin.tmp"):
			tempBodies[strings.TrimSuffix(name, ".bin.tmp")] = struct{}{}
		case strings.HasSuffix(name, ".bin"):
			finalBodies[strings.TrimSuffix(name, ".bin")] = struct{}{}
		}
	}
	for id := range tempBodies {
		tempBodyPath := s.tempBodyPath(id)
		bodyPath := filepath.Join(s.root, id+".bin")
		if _, ok := known[id]; ok {
			if _, finalExists := finalBodies[id]; !finalExists {
				if err := os.Rename(tempBodyPath, bodyPath); err == nil {
					finalBodies[id] = struct{}{}
					continue
				}
			}
		}
		if err := os.Remove(tempBodyPath); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	for id := range finalBodies {
		if _, ok := known[id]; ok {
			continue
		}
		bodyPath := filepath.Join(s.root, id+".bin")
		if err := os.Remove(bodyPath); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	for id := range known {
		if _, ok := finalBodies[id]; ok {
			continue
		}
		if _, ok := tempBodies[id]; ok {
			continue
		}
		if _, err := s.db.Exec(`DELETE FROM artifacts WHERE id = ?`, id); err != nil {
			return err
		}
	}
	return nil
}

// ---------------------------------------------------------------------------
// Scan helper
// ---------------------------------------------------------------------------

func scanArtifact(row interface{ Scan(...any) error }) (*artifact.Blob, error) {
	var (
		id, uri, kind, ct string
		size              int64
		metadataJSON      string
		createdAtStr      string
	)
	if err := row.Scan(&id, &uri, &kind, &ct, &size, &metadataJSON, &createdAtStr); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("artifact not found")
		}
		return nil, err
	}
	metadata, err := decodeJSONMapField(metadataJSON, "sqlite artifacts", id, "metadata")
	if err != nil {
		return nil, err
	}
	createdAt, err := parseTime(createdAtStr, "sqlite artifacts", id, "created_at")
	if err != nil {
		return nil, err
	}
	return &artifact.Blob{
		ID:          id,
		URI:         uri,
		Kind:        kind,
		ContentType: ct,
		Size:        size,
		CreatedAt:   createdAt,
		Metadata:    metadata,
	}, nil
}
