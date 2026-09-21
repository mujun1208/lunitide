package sqlite

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"time"

	"github.com/lunitide/lunitide/internal/domain/media"
	"github.com/oklog/ulid/v2"
)

var (
	ErrMediaSessionV2Disabled   = media.ErrSessionV2Disabled
	ErrMediaScopeDenied         = media.ErrScopeDenied
	ErrMediaQueueConflict       = media.ErrQueueConflict
	ErrMediaSessionNotFound     = media.ErrSessionNotFound
	ErrMediaAssetNotFound       = media.ErrAssetNotFound
	ErrMediaRevisionConflict    = media.ErrRevisionConflict
	ErrMediaIdempotencyConflict = media.ErrIdempotencyConflict
	ErrMediaAssetChanged        = media.ErrAssetChanged
	ErrMediaPlayerLeaseConflict = media.ErrPlayerLeaseConflict
)

type MediaSettingsRow struct {
	MediaSessionV2   bool
	ActivityCenterV2 bool
	AutoAdvance      bool
	Revision         int64
}

func (s *Store) MediaSettings(ctx context.Context) (MediaSettingsRow, error) {
	var row MediaSettingsRow
	err := s.db.QueryRowContext(ctx, `SELECT media_session_v2,activity_center_v2,auto_advance,revision FROM media_settings WHERE singleton_id='default'`).
		Scan(sqliteBool{&row.MediaSessionV2}, sqliteBool{&row.ActivityCenterV2}, sqliteBool{&row.AutoAdvance}, &row.Revision)
	return row, err
}

func (s *Store) EnableMediaSessionV2(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, `UPDATE media_settings SET media_session_v2=1, activity_center_v2=1, revision=revision+1, updated_at=?
		WHERE singleton_id='default' AND (media_session_v2=0 OR activity_center_v2=0)`,
		time.Now().UTC().Format(time.RFC3339Nano))
	return err
}

func (s *Store) EnableMediaSessionV2ForTest(ctx context.Context) error {
	return s.EnableMediaSessionV2(ctx)
}

func (s *Store) InsertMediaAsset(ctx context.Context, ownerSubjectID, scopeKind, scopeID, sourceKind, sourceRef, mime, kind, title string, size int64) (string, error) {
	now := time.Now().UTC().Format(time.RFC3339Nano)
	id := ulid.Make().String()
	identity := sourceRef
	if info, err := os.Stat(sourceRef); err == nil {
		identity = media.FileIdentity(info.Size(), info.ModTime().UnixNano())
		if size <= 0 {
			size = info.Size()
		}
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO media_assets(
		asset_id,owner_subject_id,scope_kind,scope_id,source_kind,source_ref,file_identity,mime,kind,title,size,state,revision,created_at,updated_at)
		VALUES(?,?,?,?,?,?,?,?,?,?,?,'ready',1,?,?)`,
		id, ownerSubjectID, scopeKind, scopeID, sourceKind, sourceRef, identity, mime, kind, title, size, now, now)
	return id, err
}

func (s *Store) CreateMediaSession(ctx context.Context, ownerSubjectID, scopeKind, scopeID, origin, assetID, operationKey string) (media.Snapshot, media.Operation, error) {
	settings, err := s.MediaSettings(ctx)
	if err != nil {
		return media.Snapshot{}, media.Operation{}, err
	}
	if !settings.MediaSessionV2 {
		return media.Snapshot{}, media.Operation{}, ErrMediaSessionV2Disabled
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return media.Snapshot{}, media.Operation{}, err
	}
	defer tx.Rollback()
	now := time.Now().UTC().Format(time.RFC3339Nano)
	sessionID := ulid.Make().String()
	opID := canonicalOrNewULID(operationKey)
	digest := requestDigestHex(operationKey + ":" + assetID)
	if _, err := tx.ExecContext(ctx, `INSERT INTO media_sessions(
		media_session_id,owner_subject_id,scope_kind,scope_id,origin,phase,verification_status,verification_source,
		asset_id,playback_epoch,auto_advance,position_ms,duration_ms,volume,muted,queue_revision,revision,created_at,updated_at)
		VALUES(?,?,?,?,?,'idle','none','none',?,0,1,0,0,100,0,1,1,?,?)`,
		sessionID, ownerSubjectID, scopeKind, scopeID, origin, nullIfEmpty(assetID), now, now); err != nil {
		return media.Snapshot{}, media.Operation{}, err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO media_operations(
		operation_id,media_session_id,action,request_digest,idempotency_key,phase,verification_status,verification_source,error_code,evidence_json,revision,created_at,updated_at)
		VALUES(?,?,'create',?,?,'succeeded','not_applicable','none','','{}',1,?,?)`,
		opID, sessionID, digest, operationKey, now, now); err != nil {
		return media.Snapshot{}, media.Operation{}, err
	}
	if assetID != "" {
		itemID := ulid.Make().String()
		if _, err := tx.ExecContext(ctx, `INSERT INTO media_queue_items(item_id,media_session_id,asset_id,order_index,state) VALUES(?,?,?,0,'current')`,
			itemID, sessionID, assetID); err != nil {
			return media.Snapshot{}, media.Operation{}, err
		}
	}
	if err := tx.Commit(); err != nil {
		return media.Snapshot{}, media.Operation{}, err
	}
	snap, err := s.GetMediaSession(ctx, ownerSubjectID, scopeKind, scopeID, sessionID)
	if err != nil {
		return media.Snapshot{}, media.Operation{}, err
	}
	return snap, media.Operation{OperationID: opID, MediaSessionID: sessionID, Action: "create", Phase: media.OpSucceeded, VerificationStatus: "not_applicable", VerificationSource: "none", Revision: 1}, nil
}

func nullIfEmpty(v string) any {
	if v == "" {
		return nil
	}
	return v
}

func canonicalOrNewULID(v string) string {
	if len(v) == 26 {
		if parsed, err := ulid.ParseStrict(v); err == nil && parsed.String() == v && v[0] <= '7' {
			return v
		}
	}
	return ulid.Make().String()
}

func (s *Store) GetMediaSession(ctx context.Context, ownerSubjectID, scopeKind, scopeID, sessionID string) (media.Snapshot, error) {
	return getMediaSession(ctx, s.db, ownerSubjectID, scopeKind, scopeID, sessionID)
}

type mediaQueryer interface {
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

func getMediaSession(ctx context.Context, q mediaQueryer, ownerSubjectID, scopeKind, scopeID, sessionID string) (media.Snapshot, error) {
	var snap media.Snapshot
	var asset sql.NullString
	var auto, muted int
	err := q.QueryRowContext(ctx, `SELECT media_session_id,owner_subject_id,scope_kind,scope_id,origin,phase,verification_status,verification_source,
		asset_id,playback_epoch,auto_advance,position_ms,duration_ms,volume,muted,queue_revision,revision,updated_at
		FROM media_sessions WHERE media_session_id=?`, sessionID).Scan(
		&snap.MediaSessionID, &snap.OwnerSubjectID, &snap.ScopeKind, &snap.ScopeID, &snap.Origin, &snap.Phase,
		&snap.VerificationStatus, &snap.VerificationSource, &asset, &snap.PlaybackEpoch, &auto,
		&snap.PositionMs, &snap.DurationMs, &snap.Volume, &muted, &snap.QueueRevision, &snap.Revision, &snap.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return media.Snapshot{}, ErrMediaSessionNotFound
	}
	if err != nil {
		return media.Snapshot{}, err
	}
	if snap.OwnerSubjectID != ownerSubjectID || snap.ScopeKind != scopeKind || snap.ScopeID != scopeID {
		return media.Snapshot{}, ErrMediaScopeDenied
	}
	snap.AutoAdvance = auto != 0
	snap.Muted = muted != 0
	if asset.Valid {
		snap.AssetID = asset.String
	}
	return snap, nil
}

func (s *Store) InsertMediaOperation(ctx context.Context, ownerSubjectID, scopeKind, scopeID, sessionID, action, idempotencyKey string) (media.Operation, bool, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return media.Operation{}, false, err
	}
	defer tx.Rollback()
	if _, err := getMediaSession(ctx, tx, ownerSubjectID, scopeKind, scopeID, sessionID); err != nil {
		return media.Operation{}, false, err
	}
	digest := requestDigestHex(sessionID + ":" + action + ":" + idempotencyKey)
	var existing media.Operation
	err = tx.QueryRowContext(ctx, `SELECT operation_id,media_session_id,action,phase,verification_status,verification_source,error_code,revision
		FROM media_operations WHERE media_session_id=? AND idempotency_key=?`, sessionID, idempotencyKey).
		Scan(&existing.OperationID, &existing.MediaSessionID, &existing.Action, &existing.Phase, &existing.VerificationStatus, &existing.VerificationSource, &existing.ErrorCode, &existing.Revision)
	if err == nil {
		var storedDigest string
		if err = tx.QueryRowContext(ctx, `SELECT request_digest FROM media_operations WHERE operation_id=?`, existing.OperationID).Scan(&storedDigest); err != nil {
			return media.Operation{}, false, err
		}
		if storedDigest != digest {
			return media.Operation{}, false, ErrMediaIdempotencyConflict
		}
		if err = tx.Commit(); err != nil {
			return media.Operation{}, false, err
		}
		return existing, false, nil
	}
	if err != sql.ErrNoRows {
		return media.Operation{}, false, err
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	id := canonicalOrNewULID(idempotencyKey)
	var parent sql.NullString
	_ = tx.QueryRowContext(ctx, `SELECT operation_id FROM media_operations
		WHERE media_session_id=? AND phase IN ('failed','uncertain')
		ORDER BY created_at DESC, operation_id DESC LIMIT 1`, sessionID).Scan(&parent)
	if _, err := tx.ExecContext(ctx, `INSERT INTO media_operations(
		operation_id,media_session_id,parent_operation_id,action,request_digest,idempotency_key,phase,verification_status,verification_source,error_code,evidence_json,revision,created_at,updated_at)
		VALUES(?,?,?,?,?,?,'requested','not_started','none','','{}',1,?,?)`,
		id, sessionID, parent, action, digest, idempotencyKey, now, now); err != nil {
		return media.Operation{}, false, err
	}
	if err := tx.Commit(); err != nil {
		return media.Operation{}, false, err
	}
	op := media.Operation{OperationID: id, MediaSessionID: sessionID, Action: action, Phase: media.OpRequested, VerificationStatus: "not_started", VerificationSource: "none", RootOperationID: id, Revision: 1}
	if parent.Valid {
		op.ParentOperationID = parent.String
		op.RootOperationID = parent.String
	}
	return op, true, nil
}

func (s *Store) ReplaceMediaQueue(ctx context.Context, ownerSubjectID, scopeKind, scopeID, sessionID string, expectedQueueRevision int64, assetIDs []string) (int64, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	snap, err := getMediaSession(ctx, tx, ownerSubjectID, scopeKind, scopeID, sessionID)
	if err != nil {
		return 0, err
	}
	if snap.QueueRevision != expectedQueueRevision {
		return snap.QueueRevision, ErrMediaQueueConflict
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM media_queue_items WHERE media_session_id=?`, sessionID); err != nil {
		return 0, err
	}
	for i, assetID := range assetIDs {
		state := "queued"
		if i == 0 {
			state = "current"
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO media_queue_items(item_id,media_session_id,asset_id,order_index,state) VALUES(?,?,?,?,?)`,
			ulid.Make().String(), sessionID, assetID, i, state); err != nil {
			return 0, err
		}
	}
	next := expectedQueueRevision + 1
	currentAsset := ""
	if len(assetIDs) > 0 {
		currentAsset = assetIDs[0]
	}
	res, err := tx.ExecContext(ctx, `UPDATE media_sessions SET queue_revision=?, asset_id=?, playback_epoch=playback_epoch+1, position_ms=0, revision=revision+1, updated_at=? WHERE media_session_id=? AND queue_revision=?`,
		next, nullIfEmpty(currentAsset), time.Now().UTC().Format(time.RFC3339Nano), sessionID, expectedQueueRevision)
	if err != nil {
		return 0, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return 0, err
	}
	if n != 1 {
		return snap.QueueRevision, ErrMediaQueueConflict
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return next, nil
}

func (s *Store) AdvanceMediaQueueOnce(ctx context.Context, ownerSubjectID, scopeKind, scopeID, sessionID, idempotencyKey string) (media.Operation, error) {
	op, _, err := s.InsertMediaOperation(ctx, ownerSubjectID, scopeKind, scopeID, sessionID, "next", idempotencyKey)
	if err != nil {
		return media.Operation{}, err
	}
	return op, nil
}

func (s *Store) ListMediaSessions(ctx context.Context, ownerSubjectID, scopeKind, scopeID string, limit int) ([]media.Snapshot, error) {
	if limit <= 0 {
		limit = 50
	}
	if limit > 100 {
		limit = 100
	}
	rows, err := s.db.QueryContext(ctx, `SELECT media_session_id FROM media_sessions
		WHERE owner_subject_id=? AND scope_kind=? AND scope_id=?
		ORDER BY updated_at DESC, media_session_id DESC LIMIT ?`, ownerSubjectID, scopeKind, scopeID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	out := make([]media.Snapshot, 0, len(ids))
	for _, id := range ids {
		snap, err := s.GetMediaSession(ctx, ownerSubjectID, scopeKind, scopeID, id)
		if err != nil {
			return nil, err
		}
		out = append(out, snap)
	}
	return out, nil
}

func (s *Store) ListMediaAssets(ctx context.Context, ownerSubjectID, scopeKind, scopeID, sourceKind string, limit int) ([]string, error) {
	if limit <= 0 {
		limit = 50
	}
	if limit > 100 {
		limit = 100
	}
	q := `SELECT asset_id FROM media_assets WHERE owner_subject_id=? AND scope_kind=? AND scope_id=?`
	args := []any{ownerSubjectID, scopeKind, scopeID}
	if sourceKind != "" {
		q += ` AND source_kind=?`
		args = append(args, sourceKind)
	}
	q += ` ORDER BY created_at DESC, asset_id DESC LIMIT ?`
	args = append(args, limit)
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

func (s *Store) ListMediaOperations(ctx context.Context, ownerSubjectID, scopeKind, scopeID string, limit int) ([]media.Operation, error) {
	if limit <= 0 {
		limit = 50
	}
	if limit > 100 {
		limit = 100
	}
	rows, err := s.db.QueryContext(ctx, `SELECT o.operation_id,o.media_session_id,o.parent_operation_id,o.action,o.phase,o.verification_status,o.verification_source,o.error_code,o.revision,o.created_at,o.updated_at
		FROM media_operations o
		INNER JOIN media_sessions s ON s.media_session_id=o.media_session_id
		WHERE s.owner_subject_id=? AND s.scope_kind=? AND s.scope_id=?
		ORDER BY o.created_at DESC, o.operation_id DESC LIMIT ?`, ownerSubjectID, scopeKind, scopeID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []media.Operation
	for rows.Next() {
		var row media.Operation
		var parent sql.NullString
		if err := rows.Scan(&row.OperationID, &row.MediaSessionID, &parent, &row.Action, &row.Phase, &row.VerificationStatus, &row.VerificationSource, &row.ErrorCode, &row.Revision, &row.CreatedAt, &row.UpdatedAt); err != nil {
			return nil, err
		}
		row.RootOperationID = row.OperationID
		if parent.Valid {
			row.ParentOperationID = parent.String
			row.RootOperationID = parent.String
		}
		out = append(out, row)
	}
	return out, rows.Err()
}

func (s *Store) GetMediaOperation(ctx context.Context, ownerSubjectID, scopeKind, scopeID, operationID string) (media.Operation, error) {
	var row media.Operation
	var owner, kind, scope string
	err := s.db.QueryRowContext(ctx, `SELECT o.operation_id,o.media_session_id,o.action,o.phase,o.verification_status,o.verification_source,o.error_code,o.revision,
		s.owner_subject_id,s.scope_kind,s.scope_id
		FROM media_operations o INNER JOIN media_sessions s ON s.media_session_id=o.media_session_id
		WHERE o.operation_id=?`, operationID).Scan(&row.OperationID, &row.MediaSessionID, &row.Action, &row.Phase, &row.VerificationStatus, &row.VerificationSource, &row.ErrorCode, &row.Revision, &owner, &kind, &scope)
	if errors.Is(err, sql.ErrNoRows) {
		return media.Operation{}, ErrMediaSessionNotFound
	}
	if err != nil {
		return media.Operation{}, err
	}
	if owner != ownerSubjectID || kind != scopeKind || scope != scopeID {
		return media.Operation{}, ErrMediaScopeDenied
	}
	return row, nil
}

func (s *Store) GetMediaAsset(ctx context.Context, ownerSubjectID, assetID string) (media.Asset, error) {
	var row media.Asset
	var owner, scopeKind, scopeID string
	err := s.db.QueryRowContext(ctx, `SELECT asset_id,owner_subject_id,scope_kind,scope_id,source_kind,kind,title,mime,size,state,revision,source_ref,file_identity
		FROM media_assets WHERE asset_id=?`, assetID).Scan(
		&row.AssetID, &owner, &scopeKind, &scopeID, &row.SourceKind, &row.Kind, &row.Title, &row.MIME, &row.Size, &row.State, &row.Revision, &row.SourceRef, &row.FileIdentity)
	if errors.Is(err, sql.ErrNoRows) {
		return media.Asset{}, ErrMediaAssetNotFound
	}
	if err != nil {
		return media.Asset{}, err
	}
	if owner != ownerSubjectID {
		return media.Asset{}, ErrMediaScopeDenied
	}
	return row, nil
}

func (s *Store) GetMediaSessionForOwner(ctx context.Context, ownerSubjectID, sessionID string) (media.Snapshot, error) {
	var owner, kind, scope string
	err := s.db.QueryRowContext(ctx, `SELECT owner_subject_id,scope_kind,scope_id FROM media_sessions WHERE media_session_id=?`, sessionID).
		Scan(&owner, &kind, &scope)
	if errors.Is(err, sql.ErrNoRows) {
		return media.Snapshot{}, ErrMediaSessionNotFound
	}
	if err != nil {
		return media.Snapshot{}, err
	}
	if owner != ownerSubjectID {
		return media.Snapshot{}, ErrMediaScopeDenied
	}
	return s.GetMediaSession(ctx, ownerSubjectID, kind, scope, sessionID)
}

func (s *Store) ApplyMediaSessionCommand(ctx context.Context, ownerSubjectID, sessionID, action, operationID string, expectedRevision int64, positionMs, volume int) (media.Snapshot, media.Operation, error) {
	settings, err := s.MediaSettings(ctx)
	if err != nil {
		return media.Snapshot{}, media.Operation{}, err
	}
	if !settings.MediaSessionV2 {
		return media.Snapshot{}, media.Operation{}, ErrMediaSessionV2Disabled
	}
	snap, err := s.GetMediaSessionForOwner(ctx, ownerSubjectID, sessionID)
	if err != nil {
		return media.Snapshot{}, media.Operation{}, err
	}
	var existing media.Operation
	err = s.db.QueryRowContext(ctx, `SELECT operation_id,media_session_id,action,phase,verification_status,verification_source,error_code,revision
		FROM media_operations WHERE media_session_id=? AND idempotency_key=?`, sessionID, operationID).
		Scan(&existing.OperationID, &existing.MediaSessionID, &existing.Action, &existing.Phase, &existing.VerificationStatus, &existing.VerificationSource, &existing.ErrorCode, &existing.Revision)
	if err == nil {
		digest := requestDigestHex(sessionID + ":" + action + ":" + operationID)
		var storedDigest string
		if err = s.db.QueryRowContext(ctx, `SELECT request_digest FROM media_operations WHERE operation_id=?`, existing.OperationID).Scan(&storedDigest); err != nil {
			return media.Snapshot{}, media.Operation{}, err
		}
		if storedDigest != digest {
			return media.Snapshot{}, media.Operation{}, ErrMediaIdempotencyConflict
		}
		out, err := s.GetMediaSessionForOwner(ctx, ownerSubjectID, sessionID)
		if err != nil {
			return media.Snapshot{}, media.Operation{}, err
		}
		return out, existing, nil
	}
	if err != sql.ErrNoRows {
		return media.Snapshot{}, media.Operation{}, err
	}
	if snap.Revision != expectedRevision {
		return snap, media.Operation{}, ErrMediaRevisionConflict
	}
	op, created, err := s.InsertMediaOperation(ctx, ownerSubjectID, snap.ScopeKind, snap.ScopeID, sessionID, action, operationID)
	if err != nil {
		return media.Snapshot{}, media.Operation{}, err
	}
	if !created {
		out, err := s.GetMediaSessionForOwner(ctx, ownerSubjectID, sessionID)
		if err != nil {
			return media.Snapshot{}, media.Operation{}, err
		}
		return out, op, nil
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	desired, err := json.Marshal(map[string]any{"action": action, "positionMs": positionMs, "volume": volume})
	if err != nil {
		return media.Snapshot{}, media.Operation{}, err
	}
	set := `revision=revision+1, verification_status='command_dispatched', updated_at=?`
	args := []any{now}
	commandAsset := snap.AssetID
	commandEpoch := snap.PlaybackEpoch
	switch action {
	case "seek":
		set += `, position_ms=?`
		args = append(args, positionMs)
	case "set_volume":
		set += `, volume=?`
		args = append(args, volume)
	case "mute":
		set += `, muted=1`
	case "unmute":
		set += `, muted=0`
	case "next", "previous":
		nextAsset, err := s.rotateMediaQueueLocked(ctx, sessionID, snap.AssetID, action == "previous")
		if err != nil {
			return media.Snapshot{}, media.Operation{}, err
		}
		if nextAsset != "" {
			set += `, asset_id=?, playback_epoch=playback_epoch+1, position_ms=0`
			args = append(args, nextAsset)
			commandAsset = nextAsset
			commandEpoch = snap.PlaybackEpoch + 1
		}
	}
	args = append(args, sessionID, expectedRevision)
	res, err := s.db.ExecContext(ctx, `UPDATE media_sessions SET `+set+` WHERE media_session_id=? AND revision=?`, args...)
	if err != nil {
		return media.Snapshot{}, media.Operation{}, err
	}
	if n, _ := res.RowsAffected(); n != 1 {
		return snap, media.Operation{}, ErrMediaRevisionConflict
	}
	if _, err := s.db.ExecContext(ctx, `INSERT OR IGNORE INTO media_player_commands(operation_id,media_session_id,lease_generation,asset_id,playback_epoch,desired_state_json,state)
		VALUES(?,?,1,?,?,?,'pending')`, op.OperationID, sessionID, nullIfEmpty(commandAsset), commandEpoch, string(desired)); err != nil {
		return media.Snapshot{}, media.Operation{}, err
	}
	if _, err := s.db.ExecContext(ctx, `UPDATE media_operations SET phase='dispatching', verification_status='unconfirmed', verification_source='owned_runtime', dispatched_at=?, revision=revision+1, updated_at=? WHERE operation_id=?`, now, now, op.OperationID); err != nil {
		return media.Snapshot{}, media.Operation{}, err
	}
	out, err := s.GetMediaSessionForOwner(ctx, ownerSubjectID, sessionID)
	if err != nil {
		return media.Snapshot{}, media.Operation{}, err
	}
	op.Phase = media.OpDispatching
	op.VerificationStatus = "unconfirmed"
	op.VerificationSource = "owned_runtime"
	op.Revision++
	return out, op, nil
}

func (s *Store) MarkMediaAssetState(ctx context.Context, ownerSubjectID, assetID, state string) error {
	now := time.Now().UTC().Format(time.RFC3339Nano)
	res, err := s.db.ExecContext(ctx, `UPDATE media_assets SET state=?, revision=revision+1, updated_at=? WHERE asset_id=? AND owner_subject_id=?`,
		state, now, assetID, ownerSubjectID)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n != 1 {
		return ErrMediaAssetNotFound
	}
	return nil
}

func mediaLeaseTokenDigest(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

func (s *Store) VerifyMediaPlayerLease(ctx context.Context, sessionID, windowInstanceID, token string, generation int64) error {
	digest, window, gen, expires, err := s.MediaPlayerLease(ctx, sessionID)
	if err != nil {
		return ErrMediaPlayerLeaseConflict
	}
	if window != windowInstanceID || gen != generation {
		return ErrMediaPlayerLeaseConflict
	}
	if expires != "" {
		when, parseErr := time.Parse(time.RFC3339Nano, expires)
		if parseErr != nil {
			when, parseErr = time.Parse(time.RFC3339, expires)
		}
		if parseErr == nil && time.Now().UTC().After(when) {
			return ErrMediaPlayerLeaseConflict
		}
	}
	want := mediaLeaseTokenDigest(token)
	if subtle.ConstantTimeCompare([]byte(digest), []byte(want)) != 1 {
		return ErrMediaPlayerLeaseConflict
	}
	return nil
}

func (s *Store) ClaimMediaAudioFocus(ctx context.Context, ownerSubjectID, sessionID string, playbackEpoch int64, reason string) error {
	now := time.Now().UTC().Format(time.RFC3339Nano)
	_, err := s.db.ExecContext(ctx, `UPDATE media_audio_focus SET owner_subject_id=?, media_session_id=?, playback_epoch=?, reason=?, revision=revision+1, updated_at=? WHERE singleton_id='default'`,
		ownerSubjectID, nullIfEmpty(sessionID), playbackEpoch, reason, now)
	return err
}

func (s *Store) MediaAudioFocus(ctx context.Context) (sessionID, reason string, epoch int64, err error) {
	var session sql.NullString
	err = s.db.QueryRowContext(ctx, `SELECT media_session_id, reason, playback_epoch FROM media_audio_focus WHERE singleton_id='default'`).
		Scan(&session, &reason, &epoch)
	if session.Valid {
		sessionID = session.String
	}
	return sessionID, reason, epoch, err
}

func (s *Store) AttachMediaPlayerLease(ctx context.Context, sessionID, windowInstanceID string, navigationEpoch int64, tokenDigest string, expiresAt string) (int64, error) {
	var generation sql.NullInt64
	var window string
	err := s.db.QueryRowContext(ctx, `SELECT generation,window_instance_id FROM media_player_leases WHERE media_session_id=?`, sessionID).Scan(&generation, &window)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return 0, err
	}
	next := int64(1)
	if err == nil {
		if window == windowInstanceID {
			next = generation.Int64
			_, err = s.db.ExecContext(ctx, `UPDATE media_player_leases SET lease_token_digest=?,lease_expires_at=?,navigation_epoch=? WHERE media_session_id=?`,
				tokenDigest, expiresAt, navigationEpoch, sessionID)
			return next, err
		}
		next = generation.Int64 + 1
	}
	_, err = s.db.ExecContext(ctx, `INSERT INTO media_player_leases(media_session_id,window_instance_id,navigation_epoch,lease_token_digest,lease_expires_at,generation)
		VALUES(?,?,?,?,?,?)
		ON CONFLICT(media_session_id) DO UPDATE SET window_instance_id=excluded.window_instance_id,navigation_epoch=excluded.navigation_epoch,lease_token_digest=excluded.lease_token_digest,lease_expires_at=excluded.lease_expires_at,generation=excluded.generation`,
		sessionID, windowInstanceID, navigationEpoch, tokenDigest, expiresAt, next)
	return next, err
}

func (s *Store) MediaPlayerLease(ctx context.Context, sessionID string) (digest, window string, generation int64, expires string, err error) {
	err = s.db.QueryRowContext(ctx, `SELECT lease_token_digest,window_instance_id,generation,lease_expires_at FROM media_player_leases WHERE media_session_id=?`, sessionID).
		Scan(&digest, &window, &generation, &expires)
	return
}

func (s *Store) NextMediaPlayerCommand(ctx context.Context, sessionID string, generation int64) (operationID, desired string, err error) {
	err = s.db.QueryRowContext(ctx, `SELECT operation_id,desired_state_json FROM media_player_commands
		WHERE media_session_id=? AND state IN ('pending','claimed') AND lease_generation<=? ORDER BY operation_id LIMIT 1`, sessionID, generation).
		Scan(&operationID, &desired)
	if errors.Is(err, sql.ErrNoRows) {
		return "", "", nil
	}
	if err != nil {
		return "", "", err
	}
	_, err = s.db.ExecContext(ctx, `UPDATE media_player_commands SET state='claimed', claimed_at=? WHERE operation_id=? AND state IN ('pending','claimed')`,
		time.Now().UTC().Format(time.RFC3339Nano), operationID)
	return operationID, desired, err
}

func (s *Store) AckMediaPlayerCommand(ctx context.Context, sessionID, operationID, event string, positionMs, durationMs int64) error {
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if event == "position" {
		_, err := s.db.ExecContext(ctx, `UPDATE media_sessions SET position_ms=?, duration_ms=?, updated_at=? WHERE media_session_id=?`,
			positionMs, durationMs, now, sessionID)
		return err
	}
	phase := "uncertain"
	verify := "unconfirmed"
	source := "owned_runtime"
	switch event {
	case "playing":
		phase = "playing"
		verify = "verified_playing"
	case "pause":
		phase = "paused"
		verify = "verified_paused"
	case "ended":
		phase = "ended"
		verify = "verified_ended"
	case "error":
		phase = "failed"
		verify = "none"
		source = "none"
	case "stalled":
		phase = "stalled"
		verify = "unconfirmed"
	case "stop":
		phase = "stopped"
		verify = "verified_stopped"
	}
	opPhase := "succeeded"
	opVerify := "confirmed"
	if event == "error" {
		opPhase = "failed"
		opVerify = "unconfirmed"
	}
	if event == "stalled" {
		opPhase = "uncertain"
		opVerify = "unconfirmed"
	}
	if _, err := s.db.ExecContext(ctx, `UPDATE media_player_commands SET state='acknowledged', acknowledged_at=? WHERE operation_id=? AND media_session_id=?`, now, operationID, sessionID); err != nil {
		return err
	}
	if _, err := s.db.ExecContext(ctx, `UPDATE media_operations SET phase=?, verification_status=?, completed_at=?, revision=revision+1, updated_at=? WHERE operation_id=?`,
		opPhase, opVerify, now, now, operationID); err != nil {
		return err
	}
	_, err := s.db.ExecContext(ctx, `UPDATE media_sessions SET phase=?, verification_status=?, verification_source=?, position_ms=?, duration_ms=?, updated_at=? WHERE media_session_id=?`,
		phase, verify, source, positionMs, durationMs, now, sessionID)
	return err
}

func (s *Store) AckMediaPlayerCommandForOwner(ctx context.Context, ownerSubjectID, sessionID, operationID, event string, positionMs, durationMs int64) error {
	if _, err := s.GetMediaSessionForOwner(ctx, ownerSubjectID, sessionID); err != nil {
		return err
	}
	if err := s.AckMediaPlayerCommand(ctx, sessionID, operationID, event, positionMs, durationMs); err != nil {
		return err
	}
	reason := "none"
	epoch := int64(0)
	if event == "playing" {
		reason = "owned_playback"
		if snap, err := s.GetMediaSessionForOwner(ctx, ownerSubjectID, sessionID); err == nil {
			epoch = snap.PlaybackEpoch
		}
	}
	return s.ClaimMediaAudioFocus(ctx, ownerSubjectID, sessionID, epoch, reason)
}

func (s *Store) ListOCRPackOperationsForSubject(ctx context.Context, subjectID string, limit int) ([]OCRPackOperationRow, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := s.db.QueryContext(ctx, `SELECT operation_id,pack_id,phase,error_code,revision,created_at,updated_at FROM ocr_pack_operations WHERE subject_id=? ORDER BY created_at DESC, operation_id DESC LIMIT ?`, subjectID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []OCRPackOperationRow
	for rows.Next() {
		var row OCRPackOperationRow
		if err := rows.Scan(&row.OperationID, &row.PackID, &row.Phase, &row.ErrorCode, &row.Revision, &row.CreatedAt, &row.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, row)
	}
	return out, rows.Err()
}

func (s *Store) GetMediaOperationByID(ctx context.Context, ownerSubjectID, operationID string) (media.Operation, error) {
	var row media.Operation
	var owner string
	var parent sql.NullString
	err := s.db.QueryRowContext(ctx, `SELECT o.operation_id,o.media_session_id,o.parent_operation_id,o.action,o.phase,o.verification_status,o.verification_source,o.error_code,o.revision,o.created_at,o.updated_at,s.owner_subject_id
		FROM media_operations o INNER JOIN media_sessions s ON s.media_session_id=o.media_session_id
		WHERE o.operation_id=?`, operationID).Scan(&row.OperationID, &row.MediaSessionID, &parent, &row.Action, &row.Phase, &row.VerificationStatus, &row.VerificationSource, &row.ErrorCode, &row.Revision, &row.CreatedAt, &row.UpdatedAt, &owner)
	if errors.Is(err, sql.ErrNoRows) {
		return media.Operation{}, ErrMediaSessionNotFound
	}
	if err != nil {
		return media.Operation{}, err
	}
	if owner != ownerSubjectID {
		return media.Operation{}, ErrMediaScopeDenied
	}
	if parent.Valid {
		row.ParentOperationID = parent.String
	}
	row.RootOperationID = row.OperationID
	if row.ParentOperationID != "" {
		row.RootOperationID = row.ParentOperationID
	}
	return row, nil
}

func (s *Store) ListMediaAssetRecords(ctx context.Context, ownerSubjectID, scopeKind, scopeID, sourceKind string, limit int) ([]media.Asset, error) {
	ids, err := s.ListMediaAssets(ctx, ownerSubjectID, scopeKind, scopeID, sourceKind, limit)
	if err != nil {
		return nil, err
	}
	out := make([]media.Asset, 0, len(ids))
	for _, id := range ids {
		asset, err := s.GetMediaAsset(ctx, ownerSubjectID, id)
		if err != nil {
			return nil, err
		}
		out = append(out, asset)
	}
	return out, nil
}

func (s *Store) ListMediaQueueAssets(ctx context.Context, ownerSubjectID, sessionID string) ([]media.Asset, error) {
	if _, err := s.GetMediaSessionForOwner(ctx, ownerSubjectID, sessionID); err != nil {
		return nil, err
	}
	items, err := s.ListMediaQueueItems(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	out := make([]media.Asset, 0, len(items))
	for _, item := range items {
		asset, err := s.GetMediaAsset(ctx, ownerSubjectID, item.AssetID)
		if errors.Is(err, ErrMediaAssetNotFound) {
			continue
		}
		if err != nil {
			return nil, err
		}
		out = append(out, asset)
	}
	return out, nil
}

func (s *Store) ListMediaQueueItems(ctx context.Context, sessionID string) ([]media.QueueItem, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT item_id,asset_id,order_index,state FROM media_queue_items WHERE media_session_id=? ORDER BY order_index ASC, item_id ASC`, sessionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []media.QueueItem
	for rows.Next() {
		var item media.QueueItem
		if err := rows.Scan(&item.ItemID, &item.AssetID, &item.OrderIndex, &item.State); err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func (s *Store) ApplyMediaQueueCommand(ctx context.Context, ownerSubjectID, sessionID, action, itemID, operationID string, beforeItemID *string, expectedQueueRevision int64) (media.Snapshot, media.Operation, error) {
	snap, err := s.GetMediaSessionForOwner(ctx, ownerSubjectID, sessionID)
	if err != nil {
		return media.Snapshot{}, media.Operation{}, err
	}
	items, err := s.ListMediaQueueItems(ctx, sessionID)
	if err != nil {
		return media.Snapshot{}, media.Operation{}, err
	}
	var next []string
	switch action {
	case "clear":
		next = []string{}
	case "remove":
		matched, ok := matchQueueItem(items, itemID)
		if !ok {
			return media.Snapshot{}, media.Operation{}, ErrMediaAssetNotFound
		}
		for _, item := range items {
			if item.ItemID != matched.ItemID {
				next = append(next, item.AssetID)
			}
		}
	case "jump":
		matched, ok := matchQueueItem(items, itemID)
		if ok {
			rest := make([]string, 0, len(items))
			for _, item := range items {
				if item.ItemID == matched.ItemID {
					continue
				}
				rest = append(rest, item.AssetID)
			}
			next = append([]string{matched.AssetID}, rest...)
			break
		}
		asset, err := s.GetMediaAsset(ctx, ownerSubjectID, itemID)
		if err != nil {
			return media.Snapshot{}, media.Operation{}, err
		}
		rest := make([]string, 0, len(items))
		for _, item := range items {
			if item.AssetID == asset.AssetID {
				continue
			}
			rest = append(rest, item.AssetID)
		}
		next = append([]string{asset.AssetID}, rest...)
	case "move":
		matched, ok := matchQueueItem(items, itemID)
		if !ok {
			return media.Snapshot{}, media.Operation{}, ErrMediaAssetNotFound
		}
		kept := make([]media.QueueItem, 0, len(items))
		for _, item := range items {
			if item.ItemID == matched.ItemID {
				continue
			}
			kept = append(kept, item)
		}
		inserted := false
		for _, item := range kept {
			if beforeItemID != nil && *beforeItemID != "" && (*beforeItemID == item.ItemID || *beforeItemID == item.AssetID) {
				next = append(next, matched.AssetID)
				inserted = true
			}
			next = append(next, item.AssetID)
		}
		if !inserted {
			next = append(next, matched.AssetID)
		}
	default:
		return media.Snapshot{}, media.Operation{}, ErrMediaQueueConflict
	}
	if _, err := s.ReplaceMediaQueue(ctx, ownerSubjectID, snap.ScopeKind, snap.ScopeID, sessionID, expectedQueueRevision, next); err != nil {
		return media.Snapshot{}, media.Operation{}, err
	}
	op, _, err := s.InsertMediaOperation(ctx, ownerSubjectID, snap.ScopeKind, snap.ScopeID, sessionID, action, operationID)
	if err != nil {
		return media.Snapshot{}, media.Operation{}, err
	}
	out, err := s.GetMediaSessionForOwner(ctx, ownerSubjectID, sessionID)
	if err != nil {
		return media.Snapshot{}, media.Operation{}, err
	}
	return out, op, nil
}

func matchQueueItem(items []media.QueueItem, id string) (media.QueueItem, bool) {
	if id == "" {
		return media.QueueItem{}, false
	}
	for _, item := range items {
		if item.ItemID == id || item.AssetID == id {
			return item, true
		}
	}
	return media.QueueItem{}, false
}

func (s *Store) rotateMediaQueueLocked(ctx context.Context, sessionID, currentAssetID string, previous bool) (string, error) {
	items, err := s.ListMediaQueueItems(ctx, sessionID)
	if err != nil || len(items) == 0 {
		return "", err
	}
	cur := 0
	for i, item := range items {
		if item.State == "current" || (currentAssetID != "" && item.AssetID == currentAssetID) {
			cur = i
			break
		}
	}
	delta := 1
	if previous {
		delta = -1
	}
	nextIdx := (cur + delta + len(items)) % len(items)
	for i, item := range items {
		state := "queued"
		if i == nextIdx {
			state = "current"
		}
		if _, err := s.db.ExecContext(ctx, `UPDATE media_queue_items SET state=? WHERE item_id=?`, state, item.ItemID); err != nil {
			return "", err
		}
	}
	return items[nextIdx].AssetID, nil
}
