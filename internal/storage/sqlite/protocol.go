package sqlite

import (
	"context"
	"encoding/json"
	"time"

	"github.com/lunitide/lunitide/internal/modelfit"
	"github.com/oklog/ulid/v2"
)

func (s *Store) PutProtocolMessageGroup(ctx context.Context, ownerScope, sessionID, turnID string, group modelfit.MessageGroup) error {
	if group.ID == "" {
		group.ID = ulid.Make().String()
	}
	group.Complete = modelfit.MessageGroupComplete(group)
	payload, err := json.Marshal(jsonSafeMessageGroup(group))
	if err != nil {
		return err
	}
	complete := 0
	if group.Complete {
		complete = 1
	}
	_, err = s.db.ExecContext(ctx, `INSERT INTO protocol_message_groups(id,owner_scope,session_id,turn_id,sequence,complete,payload_json,created_at)
		VALUES(?,?,?,?,?,?,?,?)
		ON CONFLICT(owner_scope, session_id, turn_id, sequence) DO UPDATE SET
			id=excluded.id, complete=excluded.complete, payload_json=excluded.payload_json`,
		group.ID, ownerScope, sessionID, turnID, group.Sequence, complete, string(payload), formatTime(time.Now().UTC()))
	return mapWriteError(err)
}

func (s *Store) ListCompleteProtocolMessageGroups(ctx context.Context, ownerScope, sessionID, turnID string) ([]modelfit.MessageGroup, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT payload_json FROM protocol_message_groups WHERE owner_scope=? AND session_id=? AND turn_id=? AND complete=1 ORDER BY sequence`, ownerScope, sessionID, turnID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []modelfit.MessageGroup
	for rows.Next() {
		var raw string
		if err := rows.Scan(&raw); err != nil {
			return nil, err
		}
		var g modelfit.MessageGroup
		if err := json.Unmarshal([]byte(raw), &g); err != nil {
			return nil, err
		}
		if !modelfit.MessageGroupComplete(g) {
			continue
		}
		out = append(out, g)
	}
	return out, rows.Err()
}

func (s *Store) PutProtocolPrivate(ctx context.Context, ownerScope, ref string, cipher []byte, digest, credGen string) error {
	id := ulid.Make().String()
	_, err := s.db.ExecContext(ctx, `INSERT INTO protocol_private(id,owner_scope,ref,cipher_blob,digest,credential_generation,created_at)
		VALUES(?,?,?,?,?,?,?)
		ON CONFLICT(owner_scope, ref) DO UPDATE SET cipher_blob=excluded.cipher_blob, digest=excluded.digest, credential_generation=excluded.credential_generation`,
		id, ownerScope, ref, cipher, digest, credGen, formatTime(time.Now().UTC()))
	return mapWriteError(err)
}

func (s *Store) GetProtocolPrivate(ctx context.Context, ownerScope, ref string) ([]byte, string, error) {
	var blob []byte
	var digest string
	err := s.db.QueryRowContext(ctx, `SELECT cipher_blob, digest FROM protocol_private WHERE owner_scope=? AND ref=?`, ownerScope, ref).Scan(&blob, &digest)
	return blob, digest, err
}

func jsonSafeMessageGroup(g modelfit.MessageGroup) modelfit.MessageGroup {
	calls := append([]modelfit.ProtocolToolCall(nil), g.Assistant.ToolCalls...)
	for i, c := range calls {
		if json.Valid(c.Arguments) {
			continue
		}
		quoted, err := json.Marshal(string(c.Arguments))
		if err != nil {
			quoted = []byte("null")
		}
		calls[i].Arguments = quoted
	}
	g.Assistant.ToolCalls = calls
	return g
}
