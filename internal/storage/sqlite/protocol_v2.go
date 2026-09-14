package sqlite

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/lunitide/lunitide/internal/modelfit"
	"github.com/lunitide/lunitide/internal/secret"
	"github.com/oklog/ulid/v2"
)

const (
	protocolBlobVersion     = 2
	protocolNonceSize       = 12
	protocolLegacyBatchSize = 100
	protocolLegacyMigration = "protocol_v2_runtime"
)

// ProtocolAAD is the CONTRACTS §4 field list. Do not shorten.
type ProtocolAAD struct {
	FormatVersion uint64
	Owner         string
	Session       string
	Epoch         string
	Sequence      uint64
	MessageID     string
	Role          string
	TargetDigest  string
	KeyID         string
	Complete      bool
	TurnID        string
	CallID        string
	Provenance    string
}

type ProtocolEpochV2 struct {
	ID, OwnerScope, SessionID                            string
	TargetJSON, TargetDigest, ProfileJSON, ProfileDigest string
	Revision, LastSequence                               int64
	State, LastCommitID                                  string
	ObservedModel, ObservedRevision                      *string
	IdentityIntegrity, ObservedAt                        string
	CipherBytes, PreparedBytes                           int64
}

type ProtocolMessageV2 struct {
	ID, EpochID, OwnerScope, SessionID string
	Sequence                           int64
	TurnID, CallID, Role, Provenance   string
	Complete                           bool
	KeyID, TargetDigest                string
}

type LegacyMigrationReport struct {
	State          string
	Migrated       int
	Degraded       int
	Remaining      int
	SkippedDeleted int
}

var errProtocolSessionDeleted = fmt.Errorf("protocol session deleted")

func EncodeProtocolAAD(a ProtocolAAD) []byte {
	var buf []byte
	putU64 := func(v uint64) {
		var b [8]byte
		binary.BigEndian.PutUint64(b[:], v)
		buf = append(buf, b[:]...)
	}
	putStr := func(s string) {
		var b [4]byte
		binary.BigEndian.PutUint32(b[:], uint32(len(s)))
		buf = append(buf, b[:]...)
		buf = append(buf, s...)
	}
	putU64(a.FormatVersion)
	putStr(a.Owner)
	putStr(a.Session)
	putStr(a.Epoch)
	putU64(a.Sequence)
	putStr(a.MessageID)
	putStr(a.Role)
	putStr(a.TargetDigest)
	putStr(a.KeyID)
	if a.Complete {
		buf = append(buf, 1)
	} else {
		buf = append(buf, 0)
	}
	putStr(a.TurnID)
	putStr(a.CallID)
	putStr(a.Provenance)
	return buf
}

func SealProtocolV2(key []byte, aad ProtocolAAD, plain []byte) ([]byte, error) {
	aead, err := protocolGCM(key)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, protocolNonceSize)
	if _, err = rand.Read(nonce); err != nil {
		return nil, err
	}
	sealed := aead.Seal(nil, nonce, plain, EncodeProtocolAAD(aad))
	out := make([]byte, 0, 1+protocolNonceSize+len(sealed))
	out = append(out, protocolBlobVersion)
	out = append(out, nonce...)
	out = append(out, sealed...)
	return out, nil
}

func OpenProtocolV2(key []byte, aad ProtocolAAD, blob []byte) ([]byte, error) {
	aead, err := protocolGCM(key)
	if err != nil {
		return nil, err
	}
	if len(blob) < 1+protocolNonceSize+aead.Overhead() || blob[0] != protocolBlobVersion {
		return nil, fmt.Errorf("invalid protocol v2 blob")
	}
	nonce, ct := blob[1:1+protocolNonceSize], blob[1+protocolNonceSize:]
	return aead.Open(nil, nonce, ct, EncodeProtocolAAD(aad))
}

func protocolGCM(key []byte) (cipher.AEAD, error) {
	if len(key) != 32 {
		return nil, fmt.Errorf("protocol v2 key must be 32 bytes")
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}

func messageAAD(rec ProtocolMessageV2) ProtocolAAD {
	return ProtocolAAD{
		FormatVersion: protocolBlobVersion,
		Owner:         rec.OwnerScope,
		Session:       rec.SessionID,
		Epoch:         rec.EpochID,
		Sequence:      uint64(rec.Sequence),
		MessageID:     rec.ID,
		Role:          rec.Role,
		TargetDigest:  rec.TargetDigest,
		KeyID:         rec.KeyID,
		Complete:      rec.Complete,
		TurnID:        rec.TurnID,
		CallID:        rec.CallID,
		Provenance:    rec.Provenance,
	}
}

func (s *Store) CreateProtocolEpochV2(ctx context.Context, epoch ProtocolEpochV2) error {
	if epoch.Revision == 0 {
		epoch.Revision = 1
	}
	if epoch.State == "" {
		epoch.State = "active"
	}
	created := formatTime(time.Now().UTC())
	_, err := s.db.ExecContext(ctx, `INSERT INTO protocol_epochs_v2(
		id,owner_scope,session_id,target_json,target_digest,profile_json,profile_digest,
		revision,last_sequence,state,last_commit_id,observed_model,observed_revision,
		identity_integrity,observed_at,cipher_bytes,prepared_bytes,created_at)
		VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		epoch.ID, epoch.OwnerScope, epoch.SessionID, epoch.TargetJSON, epoch.TargetDigest,
		epoch.ProfileJSON, epoch.ProfileDigest, epoch.Revision, epoch.LastSequence, epoch.State,
		epoch.LastCommitID, observedText(epoch.ObservedModel), observedText(epoch.ObservedRevision),
		epoch.IdentityIntegrity, epoch.ObservedAt, epoch.CipherBytes, epoch.PreparedBytes, created)
	return mapWriteError(err)
}

func observedText(v *string) any {
	if v == nil {
		return nil
	}
	return *v
}

func (s *Store) SealAndPutProtocolMessageV2(ctx context.Context, keys secret.ProtocolKeyService, rec ProtocolMessageV2, plain []byte) error {
	if keys == nil {
		return fmt.Errorf("protocol key service is required")
	}
	if rec.KeyID == "" {
		id, err := keys.EnsureProtocolKey(ctx)
		if err != nil {
			return err
		}
		rec.KeyID = id
	}
	var blob []byte
	err := keys.WithProtocolKey(ctx, rec.KeyID, func(dek []byte) error {
		sealed, sealErr := SealProtocolV2(dek, messageAAD(rec), plain)
		if sealErr != nil {
			return sealErr
		}
		blob = sealed
		return nil
	})
	if err != nil {
		return err
	}
	complete := 0
	if rec.Complete {
		complete = 1
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err = tx.ExecContext(ctx, `INSERT INTO protocol_messages_v2(
		id,epoch_id,owner_scope,sequence,turn_id,call_id,role,complete,provenance,key_id,cipher_blob,created_at)
		VALUES(?,?,?,?,?,?,?,?,?,?,?,?)`,
		rec.ID, rec.EpochID, rec.OwnerScope, rec.Sequence, rec.TurnID, rec.CallID, rec.Role,
		complete, rec.Provenance, rec.KeyID, blob, formatTime(time.Now().UTC())); err != nil {
		return mapWriteError(err)
	}
	if _, err = tx.ExecContext(ctx, `UPDATE protocol_epochs_v2 SET last_sequence=?, cipher_bytes=cipher_bytes+? WHERE id=? AND owner_scope=?`,
		rec.Sequence, len(blob), rec.EpochID, rec.OwnerScope); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) OpenProtocolMessageV2(ctx context.Context, keys secret.ProtocolKeyService, rec ProtocolMessageV2) ([]byte, error) {
	if keys == nil {
		return nil, fmt.Errorf("protocol key service is required")
	}
	var blob []byte
	err := s.db.QueryRowContext(ctx, `SELECT cipher_blob FROM protocol_messages_v2 WHERE id=? AND epoch_id=? AND owner_scope=? AND sequence=?`,
		rec.ID, rec.EpochID, rec.OwnerScope, rec.Sequence).Scan(&blob)
	if err != nil {
		return nil, err
	}
	var plain []byte
	err = keys.WithProtocolKey(ctx, rec.KeyID, func(dek []byte) error {
		opened, openErr := OpenProtocolV2(dek, messageAAD(rec), blob)
		if openErr != nil {
			return openErr
		}
		plain = opened
		return nil
	})
	return plain, err
}

func (s *Store) MigrateLegacyProtocolBatch(ctx context.Context, keys secret.ProtocolKeyService, sessionID string, batch int) (LegacyMigrationReport, error) {
	if keys == nil {
		return LegacyMigrationReport{}, fmt.Errorf("protocol key service is required")
	}
	if batch <= 0 {
		batch = protocolLegacyBatchSize
	}
	keyID, err := keys.EnsureProtocolKey(ctx)
	if err != nil {
		return LegacyMigrationReport{}, err
	}
	var report LegacyMigrationReport
	for i := 0; i < batch; i++ {
		row, ok, err := s.nextLegacyPrivate(ctx)
		if err != nil {
			return report, err
		}
		if !ok {
			report.State = "completed"
			report.Remaining = 0
			if err = s.setLegacyProgress(ctx, "", "", "completed"); err != nil {
				return report, err
			}
			return report, nil
		}
		imported, err := s.legacyImportExists(ctx, row.owner, row.ref)
		if err != nil {
			return report, err
		}
		if imported {
			if err = s.setLegacyProgress(ctx, row.owner, row.ref, "running"); err != nil {
				return report, err
			}
			continue
		}
		ownerLive, err := s.protocolSessionMigratable(ctx, row.owner)
		if err != nil {
			return report, err
		}
		ownerSession, err := s.protocolOwnerIsSession(ctx, row.owner)
		if err != nil {
			return report, err
		}
		if ownerSession && !ownerLive {
			if err = s.setLegacyProgress(ctx, row.owner, row.ref, "running"); err != nil {
				return report, err
			}
			report.SkippedDeleted++
			continue
		}
		bindSession := sessionID
		if ownerLive {
			bindSession = row.owner
		}
		callerLive, err := s.protocolSessionMigratable(ctx, sessionID)
		if err != nil {
			return report, err
		}
		if ownerLive && bindSession != sessionID && !callerLive {
			report.State = "running"
			report.Remaining, err = s.countPendingLegacyPrivate(ctx)
			return report, err
		}
		if !ownerLive && !callerLive {
			report.State = "running"
			report.Remaining, err = s.countPendingLegacyPrivate(ctx)
			return report, err
		}
		v1Key := legacyPrivateKey(row.generation)
		plain, openErr := modelfit.OpenProtocolPrivate(row.blob, v1Key)
		if openErr != nil {
			if err = s.commitUnrestorableLegacy(ctx, keys, keyID, bindSession, row); err != nil {
				if errors.Is(err, errProtocolSessionDeleted) {
					report.SkippedDeleted++
					continue
				}
				return report, err
			}
			report.Degraded++
		} else {
			if err = s.commitRestorableLegacy(ctx, keys, keyID, bindSession, row, plain); err != nil {
				if errors.Is(err, errProtocolSessionDeleted) {
					report.SkippedDeleted++
					continue
				}
				return report, err
			}
			report.Migrated++
		}
	}
	remaining, err := s.countPendingLegacyPrivate(ctx)
	if err != nil {
		return report, err
	}
	report.Remaining = remaining
	if remaining == 0 {
		report.State = "completed"
		if err = s.setLegacyProgress(ctx, "", "", "completed"); err != nil {
			return report, err
		}
		return report, nil
	}
	report.State = "running"
	return report, nil
}

type legacyPrivateRow struct {
	id, owner, ref, digest, generation string
	blob                               []byte
}

func (s *Store) nextLegacyPrivate(ctx context.Context) (legacyPrivateRow, bool, error) {
	lastOwner, lastKey, _, err := s.legacyProgress(ctx)
	if err != nil {
		return legacyPrivateRow{}, false, err
	}
	var row legacyPrivateRow
	err = s.db.QueryRowContext(ctx, `SELECT id,owner_scope,ref,cipher_blob,digest,credential_generation
		FROM protocol_private
		WHERE owner_scope > ? OR (owner_scope = ? AND ref > ?)
		ORDER BY owner_scope, ref LIMIT 1`, lastOwner, lastOwner, lastKey).Scan(
		&row.id, &row.owner, &row.ref, &row.blob, &row.digest, &row.generation)
	if err == sql.ErrNoRows {
		return legacyPrivateRow{}, false, nil
	}
	return row, err == nil, err
}

func (s *Store) legacyProgress(ctx context.Context) (owner, key, state string, err error) {
	err = s.db.QueryRowContext(ctx, `SELECT last_owner,last_source_key,state FROM protocol_migration_progress WHERE migration_id=?`, protocolLegacyMigration).
		Scan(&owner, &key, &state)
	if err == sql.ErrNoRows {
		return "", "", "", nil
	}
	return owner, key, state, err
}

func (s *Store) setLegacyProgress(ctx context.Context, owner, key, state string) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO protocol_migration_progress(migration_id,revision,source_kind,last_owner,last_source_key,state,updated_at)
		VALUES(?,1,'protocol_private',?,?,?,?)
		ON CONFLICT(migration_id) DO UPDATE SET last_owner=excluded.last_owner, last_source_key=excluded.last_source_key, state=excluded.state, updated_at=excluded.updated_at`,
		protocolLegacyMigration, owner, key, state, formatTime(time.Now().UTC()))
	return err
}

func (s *Store) legacyImportExists(ctx context.Context, owner, ref string) (bool, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT count(*) FROM protocol_legacy_imports WHERE source_kind='protocol_private' AND owner_scope=? AND source_key=?`, owner, ref).Scan(&n)
	return n > 0, err
}

func (s *Store) countPendingLegacyPrivate(ctx context.Context) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT count(*) FROM protocol_private p
		LEFT JOIN protocol_legacy_imports i ON i.source_kind='protocol_private' AND i.owner_scope=p.owner_scope AND i.source_key=p.ref
		WHERE i.source_key IS NULL`).Scan(&n)
	return n, err
}

func (s *Store) protocolOwnerIsSession(ctx context.Context, owner string) (bool, error) {
	var n int
	if err := s.db.QueryRowContext(ctx, `SELECT count(*) FROM sessions WHERE id=?`, owner).Scan(&n); err != nil {
		return false, err
	}
	if n > 0 {
		return true, nil
	}
	return s.HasTombstone(ctx, "session", owner)
}

func (s *Store) protocolSessionMigratable(ctx context.Context, sessionID string) (bool, error) {
	var n int
	if err := s.db.QueryRowContext(ctx, `SELECT count(*) FROM sessions WHERE id=?`, sessionID).Scan(&n); err != nil {
		return false, err
	}
	if n == 0 {
		return false, nil
	}
	tomb, err := s.HasTombstone(ctx, "session", sessionID)
	if err != nil {
		return false, err
	}
	return !tomb, nil
}

func protocolSessionLiveTx(ctx context.Context, tx *sql.Tx, sessionID string) (bool, error) {
	var n int
	if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM sessions WHERE id=?`, sessionID).Scan(&n); err != nil {
		return false, err
	}
	if n == 0 {
		return false, nil
	}
	var tomb int
	if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM deletion_tombstones WHERE owner_type='session' AND owner_id=?`, sessionID).Scan(&tomb); err != nil {
		return false, err
	}
	return tomb == 0, nil
}

func protocolOwnerDeletedTx(ctx context.Context, tx *sql.Tx, owner string) (bool, error) {
	var tomb int
	if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM deletion_tombstones WHERE owner_type='session' AND owner_id=?`, owner).Scan(&tomb); err != nil {
		return false, err
	}
	return tomb > 0, nil
}

func refuseDeletedProtocolOwnerTx(ctx context.Context, tx *sql.Tx, s *Store, sessionID string, row legacyPrivateRow) error {
	deleted, err := protocolOwnerDeletedTx(ctx, tx, row.owner)
	if err != nil {
		return err
	}
	live, err := protocolSessionLiveTx(ctx, tx, sessionID)
	if err != nil {
		return err
	}
	if !deleted && live {
		return nil
	}
	if err = s.setLegacyProgressTx(ctx, tx, row.owner, row.ref, "running"); err != nil {
		return err
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	return errProtocolSessionDeleted
}

func (s *Store) commitRestorableLegacy(ctx context.Context, keys secret.ProtocolKeyService, keyID, sessionID string, row legacyPrivateRow, plain []byte) error {
	epochID := ulid.Make().String()
	msgID := ulid.Make().String()
	targetDigest := sha256HexBytes([]byte("legacy|protocol_private|" + row.owner + "|" + row.ref))
	profileJSON := `{"legacy":true}`
	profileDigest := sha256HexBytes([]byte(profileJSON))
	rec := ProtocolMessageV2{
		ID: msgID, EpochID: epochID, OwnerScope: row.owner, SessionID: sessionID,
		Sequence: 1, Role: "assistant", Complete: true, Provenance: "legacy_import",
		KeyID: keyID, TargetDigest: targetDigest,
	}
	var blob []byte
	err := keys.WithProtocolKey(ctx, keyID, func(dek []byte) error {
		sealed, sealErr := SealProtocolV2(dek, messageAAD(rec), plain)
		if sealErr != nil {
			return sealErr
		}
		blob = sealed
		return nil
	})
	if err != nil {
		return err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if err = refuseDeletedProtocolOwnerTx(ctx, tx, s, sessionID, row); err != nil {
		return err
	}
	var liveDigest string
	if err = tx.QueryRowContext(ctx, `SELECT digest FROM protocol_private WHERE owner_scope=? AND ref=?`, row.owner, row.ref).Scan(&liveDigest); err != nil {
		return err
	}
	if liveDigest != row.digest {
		return fmt.Errorf("legacy source digest changed")
	}
	var exists int
	if err = tx.QueryRowContext(ctx, `SELECT count(*) FROM protocol_legacy_imports WHERE source_kind='protocol_private' AND owner_scope=? AND source_key=?`, row.owner, row.ref).Scan(&exists); err != nil {
		return err
	}
	if exists > 0 {
		if err = s.setLegacyProgressTx(ctx, tx, row.owner, row.ref, "running"); err != nil {
			return err
		}
		return tx.Commit()
	}
	now := formatTime(time.Now().UTC())
	if _, err = tx.ExecContext(ctx, `INSERT INTO protocol_epochs_v2(
		id,owner_scope,session_id,target_json,target_digest,profile_json,profile_digest,
		revision,last_sequence,state,last_commit_id,identity_integrity,observed_at,cipher_bytes,prepared_bytes,created_at)
		VALUES(?,?,?,?,?,?,?,1,1,'legacy','','unknown','',?,?,?)`,
		epochID, row.owner, sessionID, `{"sourceKind":"protocol_private"}`, targetDigest,
		profileJSON, profileDigest, len(blob), 0, now); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO protocol_messages_v2(
		id,epoch_id,owner_scope,sequence,turn_id,call_id,role,complete,provenance,key_id,cipher_blob,created_at)
		VALUES(?,?,?,1,'','','assistant',1,'legacy_import',?,?,?)`,
		msgID, epochID, row.owner, keyID, blob, now); err != nil {
		return err
	}
	refs, _ := json.Marshal([]string{msgID})
	if _, err = tx.ExecContext(ctx, `INSERT INTO protocol_legacy_imports(source_kind,owner_scope,source_key,source_digest,epoch_id,message_refs_json,state)
		VALUES('protocol_private',?,?,?,?,?,'imported')`, row.owner, row.ref, row.digest, epochID, string(refs)); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE protocol_private SET cipher_blob=? WHERE owner_scope=? AND ref=?`, []byte{}, row.owner, row.ref); err != nil {
		return err
	}
	if err = s.setLegacyProgressTx(ctx, tx, row.owner, row.ref, "running"); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) commitUnrestorableLegacy(ctx context.Context, keys secret.ProtocolKeyService, keyID, sessionID string, row legacyPrivateRow) error {
	_ = keys
	_ = keyID
	epochID := ulid.Make().String()
	targetDigest := sha256HexBytes([]byte("legacy|protocol_private|" + row.owner + "|" + row.ref))
	profileJSON := `{"legacy":true}`
	profileDigest := sha256HexBytes([]byte(profileJSON))
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if err = refuseDeletedProtocolOwnerTx(ctx, tx, s, sessionID, row); err != nil {
		return err
	}
	var exists int
	if err = tx.QueryRowContext(ctx, `SELECT count(*) FROM protocol_legacy_imports WHERE source_kind='protocol_private' AND owner_scope=? AND source_key=?`, row.owner, row.ref).Scan(&exists); err != nil {
		return err
	}
	if exists > 0 {
		if err = s.setLegacyProgressTx(ctx, tx, row.owner, row.ref, "running"); err != nil {
			return err
		}
		return tx.Commit()
	}
	now := formatTime(time.Now().UTC())
	if _, err = tx.ExecContext(ctx, `INSERT INTO protocol_epochs_v2(
		id,owner_scope,session_id,target_json,target_digest,profile_json,profile_digest,
		revision,last_sequence,state,last_commit_id,identity_integrity,observed_at,cipher_bytes,prepared_bytes,created_at)
		VALUES(?,?,?,?,?,?,?,1,0,'degraded','','unknown','',0,0,?)`,
		epochID, row.owner, sessionID, `{"sourceKind":"protocol_private"}`, targetDigest,
		profileJSON, profileDigest, now); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO protocol_legacy_imports(source_kind,owner_scope,source_key,source_digest,epoch_id,message_refs_json,state)
		VALUES('protocol_private',?,?,?,?,?,'degraded')`, row.owner, row.ref, row.digest, epochID, `[]`); err != nil {
		return err
	}
	if err = s.setLegacyProgressTx(ctx, tx, row.owner, row.ref, "running"); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) setLegacyProgressTx(ctx context.Context, tx *sql.Tx, owner, key, state string) error {
	_, err := tx.ExecContext(ctx, `INSERT INTO protocol_migration_progress(migration_id,revision,source_kind,last_owner,last_source_key,state,updated_at)
		VALUES(?,1,'protocol_private',?,?,?,?)
		ON CONFLICT(migration_id) DO UPDATE SET last_owner=excluded.last_owner, last_source_key=excluded.last_source_key, state=excluded.state, updated_at=excluded.updated_at`,
		protocolLegacyMigration, owner, key, state, formatTime(time.Now().UTC()))
	return err
}

func legacyPrivateKey(generation string) []byte {
	sum := sha256.Sum256([]byte("lunitide-protocol-private|" + generation))
	return sum[:]
}

func sha256HexBytes(in []byte) string {
	sum := sha256.Sum256(in)
	return hex.EncodeToString(sum[:])
}
