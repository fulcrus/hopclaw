package store

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	"github.com/fulcrus/hopclaw/channels/pairing"
)

// ---------------------------------------------------------------------------
// SQLitePairingStore
// ---------------------------------------------------------------------------

// SQLitePairingStore implements pairing.Store.
type SQLitePairingStore struct {
	db *sql.DB
}

func NewSQLitePairingStore(db *sql.DB) *SQLitePairingStore {
	return &SQLitePairingStore{db: db}
}

func (s *SQLitePairingStore) Get(channel, userID string) (*pairing.PairingRecord, error) {
	row := s.db.QueryRow(
		`SELECT id, channel, user_id, display_name, status, code, code_expires_at, verified_at, created_at
		 FROM pairings WHERE channel = ? AND user_id = ?`, channel, userID)
	rec, err := scanPairing(row)
	if err != nil {
		return nil, fmt.Errorf("pairing record not found for %s:%s", channel, userID)
	}
	return rec, nil
}

func (s *SQLitePairingStore) GetByCode(code string) (*pairing.PairingRecord, error) {
	row := s.db.QueryRow(
		`SELECT id, channel, user_id, display_name, status, code, code_expires_at, verified_at, created_at
		 FROM pairings WHERE code = ?`, code)
	rec, err := scanPairing(row)
	if err != nil {
		return nil, fmt.Errorf("pairing record not found for code")
	}
	return rec, nil
}

func (s *SQLitePairingStore) Save(rec *pairing.PairingRecord) error {
	if rec == nil {
		return fmt.Errorf("pairing record is nil")
	}
	if rec.ID == "" {
		b := make([]byte, 12)
		if _, err := rand.Read(b); err != nil {
			return err
		}
		rec.ID = hex.EncodeToString(b)
	}
	if rec.CreatedAt.IsZero() {
		rec.CreatedAt = time.Now().UTC()
	}

	_, err := s.db.Exec(
		`INSERT INTO pairings (id, channel, user_id, display_name, status, code, code_expires_at, verified_at, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT(channel, user_id) DO UPDATE SET
		   display_name = excluded.display_name,
		   status = excluded.status,
		   code = excluded.code,
		   code_expires_at = excluded.code_expires_at,
		   verified_at = excluded.verified_at`,
		rec.ID, rec.Channel, rec.UserID, rec.DisplayName,
		string(rec.Status), rec.Code,
		formatTime(rec.CodeExpiresAt), formatTime(rec.VerifiedAt),
		formatTime(rec.CreatedAt),
	)
	return err
}

func (s *SQLitePairingStore) List() ([]pairing.PairingRecord, error) {
	rows, err := s.db.Query(
		`SELECT id, channel, user_id, display_name, status, code, code_expires_at, verified_at, created_at
		 FROM pairings ORDER BY created_at`)
	if err != nil {
		return nil, fmt.Errorf("list pairing records: %w", err)
	}
	defer rows.Close()

	var out []pairing.PairingRecord
	for rows.Next() {
		rec, err := scanPairing(rows)
		if err != nil {
			return nil, fmt.Errorf("scan pairing record: %w", err)
		}
		out = append(out, *rec)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate pairing records: %w", err)
	}
	return out, nil
}

func (s *SQLitePairingStore) Delete(channel, userID string) error {
	res, err := s.db.Exec(`DELETE FROM pairings WHERE channel = ? AND user_id = ?`, channel, userID)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("pairing record not found for %s:%s", channel, userID)
	}
	return nil
}

// ---------------------------------------------------------------------------
// Scan helper
// ---------------------------------------------------------------------------

func scanPairing(row interface{ Scan(...any) error }) (*pairing.PairingRecord, error) {
	var (
		id, channel, userID, displayName string
		status, code                     string
		codeExpiresAtStr, verifiedAtStr  string
		createdAtStr                     string
	)
	if err := row.Scan(&id, &channel, &userID, &displayName, &status, &code, &codeExpiresAtStr, &verifiedAtStr, &createdAtStr); err != nil {
		return nil, err
	}
	codeExpiresAt, err := parsePairingTime("pairings", id, "code_expires_at", codeExpiresAtStr)
	if err != nil {
		return nil, err
	}
	verifiedAt, err := parsePairingTime("pairings", id, "verified_at", verifiedAtStr)
	if err != nil {
		return nil, err
	}
	createdAt, err := parsePairingTime("pairings", id, "created_at", createdAtStr)
	if err != nil {
		return nil, err
	}
	return &pairing.PairingRecord{
		ID:            id,
		Channel:       channel,
		UserID:        userID,
		DisplayName:   displayName,
		Status:        pairing.Status(status),
		Code:          code,
		CodeExpiresAt: codeExpiresAt,
		VerifiedAt:    verifiedAt,
		CreatedAt:     createdAt,
	}, nil
}

func parsePairingTime(storeKind, recordID, fieldName, raw string) (time.Time, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return time.Time{}, nil
	}
	parsed, err := time.Parse(sqliteTimeFmt, raw)
	if err != nil {
		return time.Time{}, fmt.Errorf("%s %s field %s: parse time: %w", storeKind, recordID, fieldName, err)
	}
	return parsed, nil
}
