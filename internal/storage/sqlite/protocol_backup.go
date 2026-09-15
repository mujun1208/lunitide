package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"sort"

	"github.com/lunitide/lunitide/internal/secret"
)

const (
	NativeRestoreComplete          = "complete"
	NativeRestoreNeedsExternalKeys = "needs_external_keys"
	NativeRestoreUnavailable       = "unavailable"
)

// ProtocolBackupManifest is the CONTRACTS §13.2 backup inventory. It lists
// key IDs and restore state only — never raw DEK bytes or reasoning text.
type ProtocolBackupManifest struct {
	RequiredProtocolKeyIDs []string `json:"requiredProtocolKeyIDs"`
	NativeRestoreState     string   `json:"nativeRestoreState"`
	SourceDataRoot         string   `json:"sourceDataRoot"`
}

func (s *Store) ProtocolBackupKeySet(ctx context.Context) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT DISTINCT key_id FROM protocol_messages_v2 ORDER BY key_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	sort.Strings(ids)
	return ids, nil
}

func (s *Store) BuildProtocolBackupManifest(ctx context.Context, sourceDataRoot string) (ProtocolBackupManifest, error) {
	ids, err := s.ProtocolBackupKeySet(ctx)
	if err != nil {
		return ProtocolBackupManifest{}, err
	}
	state := NativeRestoreComplete
	if len(ids) > 0 {
		state = NativeRestoreNeedsExternalKeys
	}
	return ProtocolBackupManifest{
		RequiredProtocolKeyIDs: ids,
		NativeRestoreState:     state,
		SourceDataRoot:         sourceDataRoot,
	}, nil
}

func (s *Store) VerifyNativeRestore(ctx context.Context, keys secret.ProtocolKeyService, sourceDataRoot string) (string, error) {
	_ = sourceDataRoot
	ids, err := s.ProtocolBackupKeySet(ctx)
	if err != nil {
		return "", err
	}
	if len(ids) == 0 {
		return NativeRestoreComplete, nil
	}
	if keys == nil {
		return NativeRestoreNeedsExternalKeys, nil
	}
	for _, keyID := range ids {
		rec, ok, err := s.firstProtocolMessageForKey(ctx, keyID)
		if err != nil {
			return "", err
		}
		if !ok {
			return NativeRestoreUnavailable, fmt.Errorf("no protocol message for key %s", keyID)
		}
		if _, err = s.OpenProtocolMessageV2(ctx, keys, rec); err != nil {
			return NativeRestoreUnavailable, err
		}
	}
	return NativeRestoreComplete, nil
}

func (s *Store) firstProtocolMessageForKey(ctx context.Context, keyID string) (ProtocolMessageV2, bool, error) {
	var rec ProtocolMessageV2
	var complete int
	err := s.db.QueryRowContext(ctx, `SELECT m.id,m.epoch_id,m.owner_scope,e.session_id,m.sequence,m.turn_id,m.call_id,m.role,m.complete,m.provenance,m.key_id,e.target_digest
		FROM protocol_messages_v2 m JOIN protocol_epochs_v2 e ON e.id=m.epoch_id
		WHERE m.key_id=? ORDER BY m.created_at, m.id LIMIT 1`, keyID).Scan(
		&rec.ID, &rec.EpochID, &rec.OwnerScope, &rec.SessionID, &rec.Sequence,
		&rec.TurnID, &rec.CallID, &rec.Role, &complete, &rec.Provenance, &rec.KeyID, &rec.TargetDigest)
	if err == sql.ErrNoRows {
		return ProtocolMessageV2{}, false, nil
	}
	if err != nil {
		return ProtocolMessageV2{}, false, err
	}
	rec.Complete = complete == 1
	return rec, true, nil
}
