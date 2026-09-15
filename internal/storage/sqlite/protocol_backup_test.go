package sqlite

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	"github.com/lunitide/lunitide/internal/domain/officestudio"
	"github.com/lunitide/lunitide/internal/domain/project"
	"github.com/lunitide/lunitide/internal/domain/session"
	"github.com/lunitide/lunitide/internal/modelfit"
	"github.com/lunitide/lunitide/internal/projectapp"
	"github.com/lunitide/lunitide/internal/secret"
	"github.com/lunitide/lunitide/internal/sessionapp"
	"github.com/oklog/ulid/v2"
)

func TestProtocolMigrationCannotResurrectDeletedSession(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	livePath := filepath.Join(dir, "live.db")
	store, err := OpenTemplated(ctx, livePath)
	if err != nil {
		t.Fatal(err)
	}

	proj, err := projectapp.New(store, store).Create(ctx, "t19-project", "test", nil, project.Project{Name: "T19 Backup"})
	if err != nil {
		t.Fatal(err)
	}
	sessions := sessionapp.New(store, store)
	deleted, err := sessions.Create(ctx, "t19-deleted", "test", nil, session.Session{ProjectID: proj.ID, Title: "deleted-native"})
	if err != nil {
		t.Fatal(err)
	}
	kept, err := sessions.Create(ctx, "t19-kept", "test", nil, session.Session{ProjectID: proj.ID, Title: "kept-sibling"})
	if err != nil {
		t.Fatal(err)
	}
	requirePersistULID26(t, deleted.ID)
	requirePersistULID26(t, kept.ID)

	dek := bytes.Repeat([]byte{0x5a}, 32)
	keyID := ulid.Make().String()
	requirePersistULID26(t, keyID)
	keys := secret.NewTestProtocolKeys(keyID, dek)
	reasoning := []byte("private reasoning must never appear in a backup manifest")

	deletedEpoch, deletedMsg := putProtocolV2Session(t, store, keys, deleted.ID, keyID, reasoning)
	keptEpoch, keptMsg := putProtocolV2Session(t, store, keys, kept.ID, keyID, []byte("kept sibling transcript"))
	seedV1NativeHistory(t, store, deleted.ID, reasoning)

	task, err := store.CreateOfficeTask(ctx, officestudio.Task{SessionID: kept.ID, Title: "季度报告", Goal: "生成报告并核对金额"}, "kept-office")
	if err != nil {
		t.Fatal(err)
	}
	published := officePublish(t, store, task, ulid.Make().String(), "kept-office-ver")

	if err = store.DeleteSession(ctx, deleted.ID); err != nil {
		t.Fatal(err)
	}

	dataRoot := filepath.Join(dir, "data-root")
	manifest, err := store.BuildProtocolBackupManifest(ctx, dataRoot)
	if err != nil {
		t.Fatal(err)
	}
	if manifest.NativeRestoreState != NativeRestoreNeedsExternalKeys {
		t.Fatalf("DB-only backup state=%q, want %s", manifest.NativeRestoreState, NativeRestoreNeedsExternalKeys)
	}
	if manifest.SourceDataRoot != dataRoot {
		t.Fatalf("SourceDataRoot=%q", manifest.SourceDataRoot)
	}
	if !containsName(manifest.RequiredProtocolKeyIDs, keyID) {
		t.Fatalf("RequiredProtocolKeyIDs=%v missing remaining key %s", manifest.RequiredProtocolKeyIDs, keyID)
	}
	rawManifest, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(rawManifest, reasoning) || bytes.Contains(rawManifest, dek) {
		t.Fatalf("backup manifest leaked raw key or reasoning: %s", rawManifest)
	}

	keySet, err := store.ProtocolBackupKeySet(ctx)
	if err != nil {
		t.Fatal(err)
	}
	wrapped, err := secret.PackTestProtocolKeys(ctx, keys, keySet)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(wrapped, reasoning) {
		t.Fatal("key metadata pack contained reasoning text")
	}

	backup := filepath.Join(dir, "snap.db")
	if err = store.CreateBackup(ctx, backup); err != nil {
		t.Fatal(err)
	}
	if err = store.Close(); err != nil {
		t.Fatal(err)
	}

	cleanPath := filepath.Join(dir, "clean.db")
	clean, err := OpenTemplated(ctx, cleanPath)
	if err != nil {
		t.Fatal(err)
	}
	if err = clean.RestoreBackup(ctx, backup); err != nil {
		t.Fatal(err)
	}
	restored, err := OpenTemplated(ctx, cleanPath)
	if err != nil {
		t.Fatal(err)
	}

	assertSessionAbsent(t, restored, deleted.ID)
	assertProtocolGone(t, restored, deleted.ID, deletedEpoch, deletedMsg)
	assertNoV1Native(t, restored, deleted.ID)

	var keptTitle string
	if err = restored.db.QueryRowContext(ctx, `SELECT title FROM sessions WHERE id=?`, kept.ID).Scan(&keptTitle); err != nil {
		t.Fatalf("kept session missing after restore: %v", err)
	}
	if keptTitle != "kept-sibling" {
		t.Fatalf("kept title=%q", keptTitle)
	}
	var officeCount int
	if err = restored.db.QueryRowContext(ctx, `SELECT count(*) FROM office_version_metadata WHERE version_id=?`, published.ID).Scan(&officeCount); err != nil || officeCount != 1 {
		t.Fatalf("sibling office row missing: count=%d err=%v", officeCount, err)
	}

	restoredKeys, err := secret.UnpackTestProtocolKeys(wrapped)
	if err != nil {
		t.Fatal(err)
	}
	got, err := restored.OpenProtocolMessageV2(ctx, restoredKeys, ProtocolMessageV2{
		ID: keptMsg, EpochID: keptEpoch, OwnerScope: kept.ID, SessionID: kept.ID,
		Sequence: 1, Role: "assistant", Complete: true, Provenance: "adapter_v2",
		KeyID: keyID, TargetDigest: sha256Hex("target-" + kept.ID), TurnID: keptEpoch, CallID: "call-1",
	})
	if err != nil || !bytes.Equal(got, []byte("kept sibling transcript")) {
		t.Fatalf("remaining session V2 restore: %q %v", got, err)
	}

	report, err := restored.MigrateLegacyProtocolBatch(ctx, restoredKeys, deleted.ID, 100)
	if err != nil {
		t.Fatalf("re-open migrate must skip deleted session, not fail: %v", err)
	}
	assertProtocolGone(t, restored, deleted.ID, deletedEpoch, deletedMsg)
	if report.Migrated != 0 {
		t.Fatalf("migrate resurrected deleted session: %+v", report)
	}

	deletedLeftover := "left-del-" + deleted.ID[len(deleted.ID)-8:]
	siblingLeftover := "left-keep-" + kept.ID[len(kept.ID)-8:]
	seedV1NativeHistory(t, restored, deleted.ID, []byte("deleted leftover after delete"), deletedLeftover)
	seedV1NativeHistory(t, restored, kept.ID, []byte("sibling leftover after delete"), siblingLeftover)

	stolen, err := restored.MigrateLegacyProtocolBatch(ctx, restoredKeys, kept.ID, 100)
	if err != nil {
		t.Fatal(err)
	}
	assertProtocolGone(t, restored, deleted.ID, deletedEpoch, deletedMsg)
	assertNoDeletedOwnerEpoch(t, restored, deleted.ID)
	if imported := countLegacyImport(t, restored, kept.ID, kept.ID, siblingLeftover); imported != 1 {
		t.Fatalf("live sibling leftover not imported: %d report=%+v", imported, stolen)
	}
	if imported := countLegacyImport(t, restored, deleted.ID, kept.ID, deletedLeftover); imported != 0 {
		t.Fatalf("deleted owner leftover imported into sibling: %d", imported)
	}

	if _, err = restored.db.ExecContext(ctx, `DELETE FROM protocol_migration_progress WHERE migration_id=?`, protocolLegacyMigration); err != nil {
		t.Fatal(err)
	}
	walkDel := "walk-del-" + deleted.ID[len(deleted.ID)-8:]
	walkKeep := "walk-keep-" + kept.ID[len(kept.ID)-8:]
	seedV1NativeHistory(t, restored, deleted.ID, []byte("deleted leftover walk"), walkDel)
	seedV1NativeHistory(t, restored, kept.ID, []byte("sibling leftover walk"), walkKeep)
	dead, err := restored.MigrateLegacyProtocolBatch(ctx, restoredKeys, deleted.ID, 100)
	if err != nil {
		t.Fatal(err)
	}
	assertProtocolGone(t, restored, deleted.ID, deletedEpoch, deletedMsg)
	assertNoDeletedOwnerEpoch(t, restored, deleted.ID)
	if !leftoverReachable(t, restored, kept.ID, walkKeep) {
		t.Fatalf("migrate(deleted) walked past live sibling leftover: %+v", dead)
	}

	state, err := restored.VerifyNativeRestore(ctx, restoredKeys, dataRoot)
	if err != nil {
		t.Fatal(err)
	}
	if state != NativeRestoreComplete {
		t.Fatalf("native restore after key metadata unpack: %s", state)
	}
	if err = restored.Close(); err != nil {
		t.Fatal(err)
	}

	again, err := OpenTemplated(ctx, cleanPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = again.Close() })
	assertSessionAbsent(t, again, deleted.ID)
	assertProtocolGone(t, again, deleted.ID, deletedEpoch, deletedMsg)
}

func TestProtocolBackupKeySet(t *testing.T) {
	ctx := context.Background()
	store, err := OpenTemplated(ctx, filepath.Join(t.TempDir(), "keyset.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })

	proj, err := projectapp.New(store, store).Create(ctx, "t19-keys", "test", nil, project.Project{Name: "T19 Keys"})
	if err != nil {
		t.Fatal(err)
	}
	sess, err := sessionapp.New(store, store).Create(ctx, "t19-keys-sess", "test", nil, session.Session{ProjectID: proj.ID, Title: "keys"})
	if err != nil {
		t.Fatal(err)
	}
	firstID := ulid.Make().String()
	secondID := ulid.Make().String()
	first := secret.NewTestProtocolKeys(firstID, bytes.Repeat([]byte{0x11}, 32))
	second := secret.NewTestProtocolKeys(secondID, bytes.Repeat([]byte{0x22}, 32))
	putProtocolV2Session(t, store, first, sess.ID, firstID, []byte("one"))
	putProtocolV2Session(t, store, second, sess.ID, secondID, []byte("two"))

	ids, err := store.ProtocolBackupKeySet(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(ids) != 2 || !containsName(ids, firstID) || !containsName(ids, secondID) {
		t.Fatalf("key set=%v", ids)
	}
	manifest, err := store.BuildProtocolBackupManifest(ctx, `E:\t19-root`)
	if err != nil {
		t.Fatal(err)
	}
	body, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(body, bytes.Repeat([]byte{0x11}, 32)) || bytes.Contains(body, []byte("one")) || bytes.Contains(body, []byte("two")) {
		t.Fatalf("key-set manifest leaked material: %s", body)
	}
	if manifest.NativeRestoreState != NativeRestoreNeedsExternalKeys {
		t.Fatalf("state=%q", manifest.NativeRestoreState)
	}
}

func putProtocolV2Session(t *testing.T, store *Store, keys secret.ProtocolKeyService, sessionID, keyID string, plain []byte) (epochID, msgID string) {
	t.Helper()
	ctx := context.Background()
	epochID = ulid.Make().String()
	msgID = ulid.Make().String()
	requirePersistULID26(t, epochID)
	requirePersistULID26(t, msgID)
	digest := sha256Hex("target-" + sessionID)
	if err := store.CreateProtocolEpochV2(ctx, ProtocolEpochV2{
		ID: epochID, OwnerScope: sessionID, SessionID: sessionID,
		TargetJSON: `{"id":"t19"}`, TargetDigest: digest,
		ProfileJSON: `{"id":"p"}`, ProfileDigest: sha256Hex("profile-t19"),
		State: "active", IdentityIntegrity: "observed",
	}); err != nil {
		t.Fatal(err)
	}
	if err := store.SealAndPutProtocolMessageV2(ctx, keys, ProtocolMessageV2{
		ID: msgID, EpochID: epochID, OwnerScope: sessionID, SessionID: sessionID,
		Sequence: 1, TurnID: epochID, CallID: "call-1", Role: "assistant",
		Complete: true, Provenance: "adapter_v2", KeyID: keyID, TargetDigest: digest,
	}, plain); err != nil {
		t.Fatal(err)
	}
	return epochID, msgID
}

func seedV1NativeHistory(t *testing.T, store *Store, sessionID string, reasoning []byte, ref ...string) {
	t.Helper()
	ctx := context.Background()
	gen := "g-t19"
	sum := sha256.Sum256([]byte("lunitide-protocol-private|" + gen))
	blob, digest, err := modelfit.SealProtocolPrivate(reasoning, sum[:])
	if err != nil {
		t.Fatal(err)
	}
	privRef := "native-" + sessionID[len(sessionID)-8:]
	if len(ref) > 0 && ref[0] != "" {
		privRef = ref[0]
	}
	if err = store.PutProtocolPrivate(ctx, sessionID, privRef, blob, digest, gen); err != nil {
		t.Fatal(err)
	}
	groupID := ulid.Make().String()
	now := formatTime(time.Now().UTC())
	if _, err = store.db.ExecContext(ctx, `INSERT INTO protocol_message_groups(id,owner_scope,session_id,turn_id,sequence,complete,payload_json,created_at)
		VALUES(?,?,?,?,1,1,?,?)`,
		groupID, sessionID, sessionID, groupID, `{"id":"`+groupID+`","sequence":1,"complete":true,"assistant":{"role":"assistant","content":"native"}}`, now); err != nil {
		t.Fatal(err)
	}
}

func assertSessionAbsent(t *testing.T, store *Store, sessionID string) {
	t.Helper()
	var n int
	if err := store.db.QueryRow(`SELECT count(*) FROM sessions WHERE id=?`, sessionID).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("deleted session %s still has a row", sessionID)
	}
}

func assertProtocolGone(t *testing.T, store *Store, sessionID, epochID, msgID string) {
	t.Helper()
	var epochs, msgs int
	if err := store.db.QueryRow(`SELECT count(*) FROM protocol_epochs_v2 WHERE session_id=? OR id=?`, sessionID, epochID).Scan(&epochs); err != nil {
		t.Fatal(err)
	}
	if err := store.db.QueryRow(`SELECT count(*) FROM protocol_messages_v2 WHERE id=? OR epoch_id=?`, msgID, epochID).Scan(&msgs); err != nil {
		t.Fatal(err)
	}
	if epochs != 0 || msgs != 0 {
		t.Fatalf("deleted session protocol resurrected epochs=%d messages=%d", epochs, msgs)
	}
}

func assertNoV1Native(t *testing.T, store *Store, sessionID string) {
	t.Helper()
	var priv, groups int
	if err := store.db.QueryRow(`SELECT count(*) FROM protocol_private WHERE owner_scope=?`, sessionID).Scan(&priv); err != nil {
		t.Fatal(err)
	}
	if err := store.db.QueryRow(`SELECT count(*) FROM protocol_message_groups WHERE session_id=? OR owner_scope=?`, sessionID, sessionID).Scan(&groups); err != nil {
		t.Fatal(err)
	}
	if priv != 0 || groups != 0 {
		t.Fatalf("deleted session native history survived priv=%d groups=%d", priv, groups)
	}
}

func requirePersistULID26(t *testing.T, id string) {
	t.Helper()
	if len(id) != 26 {
		t.Fatalf("persist id %q length=%d, want ULID 26", id, len(id))
	}
}

func assertNoDeletedOwnerEpoch(t *testing.T, store *Store, owner string) {
	t.Helper()
	var n int
	if err := store.db.QueryRow(`SELECT count(*) FROM protocol_epochs_v2 WHERE owner_scope=?`, owner).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("deleted owner %s has %d protocol epochs", owner, n)
	}
}

func countLegacyImport(t *testing.T, store *Store, owner, sessionID, ref string) int {
	t.Helper()
	var n int
	if err := store.db.QueryRow(`SELECT count(*) FROM protocol_legacy_imports i JOIN protocol_epochs_v2 e ON e.id=i.epoch_id
		WHERE i.source_kind='protocol_private' AND i.owner_scope=? AND i.source_key=? AND e.session_id=?`, owner, ref, sessionID).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func leftoverReachable(t *testing.T, store *Store, owner, ref string) bool {
	t.Helper()
	var lastOwner, lastKey string
	err := store.db.QueryRow(`SELECT last_owner,last_source_key FROM protocol_migration_progress WHERE migration_id=?`, protocolLegacyMigration).
		Scan(&lastOwner, &lastKey)
	if err != nil {
		lastOwner, lastKey = "", ""
	}
	var imported int
	if err = store.db.QueryRow(`SELECT count(*) FROM protocol_legacy_imports WHERE source_kind='protocol_private' AND owner_scope=? AND source_key=?`, owner, ref).Scan(&imported); err != nil {
		t.Fatal(err)
	}
	if imported > 0 {
		return true
	}
	var n int
	if err = store.db.QueryRow(`SELECT count(*) FROM protocol_private WHERE owner_scope=? AND ref=? AND (owner_scope > ? OR (owner_scope = ? AND ref > ?))`,
		owner, ref, lastOwner, lastOwner, lastKey).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n == 1
}
