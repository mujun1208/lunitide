package sqlite

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/oklog/ulid/v2"
)

func (s *Store) ChatTurnSessionExists(ctx context.Context, sessionID string) (bool, error) {
	var exists bool
	err := s.db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM sessions WHERE id=?)`, sessionID).Scan(&exists)
	return exists, err
}

// PutChatTurn keeps each turn independently: a new stream can never replace a
// previous stream's recovery draft. The bridge never supplies this JSON.
func (s *Store) PutChatTurn(ctx context.Context, sessionID, turnID string, checkpoint []byte, pending bool) error {
	if _, err := ulid.ParseStrict(sessionID); err != nil {
		return err
	}
	if _, err := ulid.ParseStrict(turnID); err != nil {
		return err
	}
	if !json.Valid(checkpoint) {
		return errors.New("invalid chat turn checkpoint")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	stored := checkpoint
	if len(checkpoint) > checkpointPartBytes {
		stored, err = json.Marshal(checkpointPartsReceipt{Parts: (len(checkpoint) + checkpointPartBytes - 1) / checkpointPartBytes, Bytes: len(checkpoint), SHA256: fmt.Sprintf("%x", sha256.Sum256(checkpoint))})
		if err != nil {
			return err
		}
	}
	result, err := tx.ExecContext(ctx, `INSERT INTO chat_turn_journal(turn_id,session_id,checkpoint_json,pending,updated_at) SELECT ?,?,?,?,? WHERE EXISTS(SELECT 1 FROM sessions WHERE id=?) ON CONFLICT(turn_id) DO UPDATE SET checkpoint_json=excluded.checkpoint_json,pending=excluded.pending,updated_at=excluded.updated_at WHERE session_id=excluded.session_id`, turnID, sessionID, string(stored), pending, formatTime(time.Now().UTC()), sessionID)
	if err != nil {
		return err
	}
	if n, err := result.RowsAffected(); err != nil || n != 1 {
		return errors.New("chat turn scope conflict")
	}
	if _, err = tx.ExecContext(ctx, `DELETE FROM chat_turn_checkpoint_parts WHERE turn_id=?`, turnID); err != nil {
		return err
	}
	if len(checkpoint) > checkpointPartBytes {
		for part, start := 0, 0; start < len(checkpoint); part, start = part+1, start+checkpointPartBytes {
			end := min(start+checkpointPartBytes, len(checkpoint))
			if _, err = tx.ExecContext(ctx, `INSERT INTO chat_turn_checkpoint_parts(turn_id,part,content) VALUES(?,?,?)`, turnID, part, checkpoint[start:end]); err != nil {
				return err
			}
		}
	}
	return tx.Commit()
}

const checkpointPartBytes = 256 << 10

type checkpointPartsReceipt struct {
	Parts  int    `json:"_checkpointParts"`
	Bytes  int    `json:"bytes"`
	SHA256 string `json:"sha256"`
}

// Old checkpoints above the former 1 MiB ceiling remain recoverable. New
// generation is separately budgeted; storage never silently cuts a saved turn.
// Read the receipt and parts in one snapshot to avoid torn replacement reads.
func readCheckpointParts(ctx context.Context, tx *sql.Tx, turnID string, raw []byte) ([]byte, error) {
	var receipt checkpointPartsReceipt
	if err := json.Unmarshal(raw, &receipt); err != nil {
		return nil, err
	}
	if receipt.Parts == 0 {
		return raw, nil
	}
	if receipt.Parts < 1 || receipt.Bytes < 1 || (receipt.Bytes-1)/checkpointPartBytes+1 != receipt.Parts || len(receipt.SHA256) != 64 {
		return nil, errors.New("invalid checkpoint parts receipt")
	}
	rows, err := tx.QueryContext(ctx, `SELECT part,content FROM chat_turn_checkpoint_parts WHERE turn_id=? ORDER BY part`, turnID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []byte
	count := 0
	for rows.Next() {
		var part int
		var content []byte
		if err = rows.Scan(&part, &content); err != nil {
			return nil, err
		}
		if part != count || count >= receipt.Parts || len(content) > receipt.Bytes-len(result) {
			return nil, errors.New("invalid checkpoint part sequence")
		}
		result = append(result, content...)
		count++
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	if count != receipt.Parts || len(result) != receipt.Bytes || fmt.Sprintf("%x", sha256.Sum256(result)) != receipt.SHA256 || !json.Valid(result) {
		return nil, errors.New("incomplete checkpoint parts")
	}
	return result, nil
}

func (s *Store) LatestChatTurn(ctx context.Context, sessionID string) ([]byte, error) {
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	var raw, turnID string
	err = tx.QueryRowContext(ctx, `SELECT turn_id,checkpoint_json FROM chat_turn_journal WHERE session_id=? ORDER BY turn_id DESC LIMIT 1`, sessionID).Scan(&turnID, &raw)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return readCheckpointParts(ctx, tx, turnID, []byte(raw))
}

func (s *Store) PendingChatTurns(ctx context.Context, sessionID string) ([][]byte, error) {
	// Recovery is incremental and ordered. Bound one pass without dropping any
	// rows; a failed write leaves that turn and every later turn pending.
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	rows, err := tx.QueryContext(ctx, `SELECT turn_id,checkpoint_json FROM chat_turn_journal WHERE session_id=? AND pending=1 ORDER BY turn_id LIMIT 100`, sessionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	type record struct{ turnID, raw string }
	var records []record
	for rows.Next() {
		var r record
		if err := rows.Scan(&r.turnID, &r.raw); err != nil {
			return nil, err
		}
		records = append(records, r)
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	if err = rows.Close(); err != nil {
		return nil, err
	}
	var out [][]byte
	for _, r := range records {
		raw, err := readCheckpointParts(ctx, tx, r.turnID, []byte(r.raw))
		if err != nil {
			return nil, err
		}
		out = append(out, raw)
	}
	return out, nil
}
