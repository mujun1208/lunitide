package sqlite

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/lunitide/lunitide/internal/modelfit"
	"github.com/lunitide/lunitide/internal/secret"
	"github.com/oklog/ulid/v2"
)

var protocolSessionSeq atomic.Int64

func TestProtocolCipherAADAndLegacyMigration(t *testing.T) {
	ctx := context.Background()
	store, err := OpenTemplated(ctx, filepath.Join(t.TempDir(), "protocol-v2.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })

	for _, table := range []string{"protocol_epochs_v2", "protocol_messages_v2", "protocol_migration_progress", "protocol_legacy_imports"} {
		var name string
		if err = store.db.QueryRow(`SELECT name FROM sqlite_schema WHERE type='table' AND name=?`, table).Scan(&name); err != nil {
			t.Fatalf("0157 must create %s: %v", table, err)
		}
	}

	sessionA := ulid.Make().String()
	sessionB := ulid.Make().String()
	seedProtocolSession(t, store, sessionA)
	seedProtocolSession(t, store, sessionB)

	dek := bytes.Repeat([]byte{0x42}, 32)
	wrong := bytes.Repeat([]byte{0x24}, 32)
	keyID := "protocol-test-key"
	keys := secret.NewTestProtocolKeys(keyID, dek)

	epochA := ulid.Make().String()
	epochB := ulid.Make().String()
	msgID := ulid.Make().String()
	turnID := ulid.Make().String()
	callID := "call-1"
	digestA := sha256Hex("target-a")
	digestB := sha256Hex("target-b")
	plain := []byte("keep  spaces\nand CRLF\r\nplus unicode 月")

	aad := ProtocolAAD{
		FormatVersion: 2,
		Owner:         "acct\nroot",
		Session:       sessionA,
		Epoch:         epochA,
		Sequence:      1,
		MessageID:     msgID,
		Role:          "assistant",
		TargetDigest:  digestA,
		KeyID:         keyID,
		Complete:      true,
		TurnID:        turnID,
		CallID:        callID,
		Provenance:    "adapter_v2\nkeep",
	}
	if got := encodeProtocolAADForTest(aad); !bytes.Equal(got, EncodeProtocolAAD(aad)) {
		t.Fatalf("AAD encoding drifted from CONTRACTS §4 length-prefix list\n got %x\nwant %x", EncodeProtocolAAD(aad), got)
	}

	blob, err := SealProtocolV2(dek, aad, plain)
	if err != nil {
		t.Fatal(err)
	}
	if len(blob) < 1+12+16 || blob[0] != 2 {
		t.Fatalf("V2 blob must be version|nonce|gcm, got len=%d head=%v", len(blob), blob)
	}
	if bytes.Contains(blob, plain) {
		t.Fatal("ciphertext leaked plaintext")
	}
	opened, err := OpenProtocolV2(dek, aad, blob)
	if err != nil || !bytes.Equal(opened, plain) {
		t.Fatalf("round-trip: %q %v", opened, err)
	}

	mutations := []ProtocolAAD{
		withAAD(aad, func(a *ProtocolAAD) { a.FormatVersion = 1 }),
		withAAD(aad, func(a *ProtocolAAD) { a.Owner = "acct" }),
		withAAD(aad, func(a *ProtocolAAD) { a.Session = sessionB }),
		withAAD(aad, func(a *ProtocolAAD) { a.Epoch = epochB }),
		withAAD(aad, func(a *ProtocolAAD) { a.Sequence = 2 }),
		withAAD(aad, func(a *ProtocolAAD) { a.MessageID = ulid.Make().String() }),
		withAAD(aad, func(a *ProtocolAAD) { a.Role = "user" }),
		withAAD(aad, func(a *ProtocolAAD) { a.TargetDigest = digestB }),
		withAAD(aad, func(a *ProtocolAAD) { a.KeyID = "other-key" }),
		withAAD(aad, func(a *ProtocolAAD) { a.Complete = false }),
		withAAD(aad, func(a *ProtocolAAD) { a.TurnID = ulid.Make().String() }),
		withAAD(aad, func(a *ProtocolAAD) { a.CallID = "call-2" }),
		withAAD(aad, func(a *ProtocolAAD) { a.Provenance = "adapter_v2" }),
	}
	for i, bad := range mutations {
		if _, err = OpenProtocolV2(dek, bad, blob); err == nil {
			t.Fatalf("mutation %d must fail AAD bind: %+v", i, bad)
		}
	}
	if _, err = OpenProtocolV2(wrong, aad, blob); err == nil {
		t.Fatal("wrong key must not decrypt")
	}
	flipped := append([]byte(nil), blob...)
	flipped[len(flipped)-1] ^= 0x01
	if _, err = OpenProtocolV2(dek, aad, flipped); err == nil {
		t.Fatal("flipped GCM integrity must fail")
	}

	if err = store.CreateProtocolEpochV2(ctx, ProtocolEpochV2{
		ID: epochA, OwnerScope: aad.Owner, SessionID: sessionA,
		TargetJSON: `{"id":"a"}`, TargetDigest: digestA,
		ProfileJSON: `{"id":"p"}`, ProfileDigest: sha256Hex("profile-a"),
		State: "active", IdentityIntegrity: "observed",
	}); err != nil {
		t.Fatal(err)
	}
	if err = store.CreateProtocolEpochV2(ctx, ProtocolEpochV2{
		ID: epochB, OwnerScope: "other-owner", SessionID: sessionB,
		TargetJSON: `{"id":"b"}`, TargetDigest: digestB,
		ProfileJSON: `{"id":"p"}`, ProfileDigest: sha256Hex("profile-b"),
		State: "active", IdentityIntegrity: "observed",
	}); err != nil {
		t.Fatal(err)
	}
	rec := ProtocolMessageV2{
		ID: msgID, EpochID: epochA, OwnerScope: aad.Owner, Sequence: 1,
		TurnID: turnID, CallID: callID, Role: aad.Role, Complete: true,
		Provenance: aad.Provenance, KeyID: keyID, TargetDigest: digestA, SessionID: sessionA,
	}
	if err = store.SealAndPutProtocolMessageV2(ctx, keys, rec, plain); err != nil {
		t.Fatal(err)
	}
	var stored []byte
	if err = store.db.QueryRowContext(ctx, `SELECT cipher_blob FROM protocol_messages_v2 WHERE id=?`, msgID).Scan(&stored); err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(stored, plain) || len(stored) == 0 || stored[0] != 2 {
		t.Fatalf("protocol_messages_v2 must store V2 ciphertext, got %q", stored)
	}
	got, err := store.OpenProtocolMessageV2(ctx, keys, rec)
	if err != nil || !bytes.Equal(got, plain) {
		t.Fatalf("store open: %q %v", got, err)
	}

	copiedID := ulid.Make().String()
	if _, err = store.db.ExecContext(ctx, `INSERT INTO protocol_messages_v2(id,epoch_id,owner_scope,sequence,turn_id,call_id,role,complete,provenance,key_id,cipher_blob,created_at)
		VALUES(?,?,?,?,?,?,?,?,?,?,?,?)`,
		copiedID, epochB, "other-owner", 1, turnID, callID, rec.Role, 1, rec.Provenance, keyID, stored, formatTime(time.Now().UTC())); err != nil {
		t.Fatal(err)
	}
	if _, err = store.OpenProtocolMessageV2(ctx, keys, ProtocolMessageV2{
		ID: copiedID, EpochID: epochB, OwnerScope: "other-owner", Sequence: 1,
		TurnID: turnID, CallID: callID, Role: rec.Role, Complete: true,
		Provenance: rec.Provenance, KeyID: keyID, TargetDigest: digestB, SessionID: sessionB,
	}); err == nil {
		t.Fatal("copied ciphertext on another owner/session/target must not decrypt")
	}
	if _, err = store.db.ExecContext(ctx, `UPDATE protocol_messages_v2 SET complete=0 WHERE id=?`, msgID); err != nil {
		t.Fatal(err)
	}
	incomplete := rec
	incomplete.Complete = false
	if _, err = store.OpenProtocolMessageV2(ctx, keys, incomplete); err == nil {
		t.Fatal("flipped complete integrity flag must fail decrypt")
	}
	if _, err = store.db.ExecContext(ctx, `UPDATE protocol_messages_v2 SET complete=1 WHERE id=?`, msgID); err != nil {
		t.Fatal(err)
	}

	wrongKeys := secret.NewTestProtocolKeys(keyID, wrong)
	if _, err = store.OpenProtocolMessageV2(ctx, wrongKeys, rec); err == nil {
		t.Fatal("wrong injected key must fail decrypt")
	}

	runLegacyMigrationLock(t, store, keys, dek)
}

func runLegacyMigrationLock(t *testing.T, store *Store, keys secret.ProtocolKeyService, dek []byte) {
	t.Helper()
	ctx := context.Background()
	sessionID := ulid.Make().String()
	seedProtocolSession(t, store, sessionID)
	owner := "legacy-owner"
	gen := "g-legacy"
	v1Key := sha256.Sum256([]byte("lunitide-protocol-private|" + gen))

	okRef := "priv-ok"
	okPlain := []byte("old reasoning\nexact")
	okBlob, okDigest, err := modelfit.SealProtocolPrivate(okPlain, v1Key[:])
	if err != nil {
		t.Fatal(err)
	}
	secondRef := "priv-ok-2"
	secondPlain := []byte("second row")
	secondBlob, secondDigest, err := modelfit.SealProtocolPrivate(secondPlain, v1Key[:])
	if err != nil {
		t.Fatal(err)
	}
	badRef := "priv-truncated"
	badBlob := []byte{0x00, 0x01}
	badDigest := sha256Hex("not-recoverable")
	now := formatTime(time.Now().UTC())
	for _, row := range [][]any{
		{ulid.Make().String(), owner, okRef, okBlob, okDigest, gen, now},
		{ulid.Make().String(), owner, secondRef, secondBlob, secondDigest, gen, now},
		{ulid.Make().String(), owner, badRef, badBlob, badDigest, gen, now},
	} {
		if _, err = store.db.ExecContext(ctx, `INSERT INTO protocol_private(id,owner_scope,ref,cipher_blob,digest,credential_generation,created_at) VALUES(?,?,?,?,?,?,?)`, row...); err != nil {
			t.Fatal(err)
		}
	}

	first, err := store.MigrateLegacyProtocolBatch(ctx, keys, sessionID, 1)
	if err != nil {
		t.Fatal(err)
	}
	if first.State != "running" || first.Migrated != 1 || first.Remaining < 1 {
		t.Fatalf("interrupted batch: %+v", first)
	}

	var cleared int
	if err = store.db.QueryRowContext(ctx, `SELECT count(*) FROM protocol_private WHERE owner_scope=? AND ref=? AND length(cipher_blob)=0`, owner, okRef).Scan(&cleared); err != nil || cleared != 1 {
		t.Fatalf("first restorable old field must be cleared once: %d %v", cleared, err)
	}

	resume, err := store.MigrateLegacyProtocolBatch(ctx, keys, sessionID, 100)
	if err != nil {
		t.Fatal(err)
	}
	if resume.Degraded < 1 {
		t.Fatalf("unrestorable legacy must be marked degraded: %+v", resume)
	}

	var unrestorable []byte
	if err = store.db.QueryRowContext(ctx, `SELECT cipher_blob FROM protocol_private WHERE owner_scope=? AND ref=?`, owner, badRef).Scan(&unrestorable); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(unrestorable, badBlob) {
		t.Fatalf("resume must not double-clear unrestorable legacy: %q", unrestorable)
	}

	again, err := store.MigrateLegacyProtocolBatch(ctx, keys, sessionID, 100)
	if err != nil {
		t.Fatal(err)
	}
	var still []byte
	if err = store.db.QueryRowContext(ctx, `SELECT cipher_blob FROM protocol_private WHERE owner_scope=? AND ref=?`, owner, badRef).Scan(&still); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(still, badBlob) {
		t.Fatal("second resume cleared unrestorable history")
	}

	var invented int
	if err = store.db.QueryRowContext(ctx, `SELECT count(*) FROM protocol_messages_v2 m JOIN protocol_legacy_imports i ON i.epoch_id=m.epoch_id WHERE i.source_key=?`, badRef).Scan(&invented); err != nil {
		t.Fatal(err)
	}
	if invented != 0 {
		t.Fatal("unrestorable legacy must not invent a complete message")
	}

	var degradedState string
	if err = store.db.QueryRowContext(ctx, `SELECT e.state FROM protocol_epochs_v2 e JOIN protocol_legacy_imports i ON i.epoch_id=e.id WHERE i.source_key=?`, badRef).Scan(&degradedState); err != nil {
		t.Fatal(err)
	}
	if degradedState != "degraded" {
		t.Fatalf("unrestorable epoch state=%q, want degraded", degradedState)
	}

	var migratedPlain []byte
	if err = store.db.QueryRowContext(ctx, `SELECT m.cipher_blob FROM protocol_messages_v2 m JOIN protocol_legacy_imports i ON i.epoch_id=m.epoch_id WHERE i.source_key=?`, okRef).Scan(&migratedPlain); err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(migratedPlain, okPlain) {
		t.Fatal("migrated V2 row stored plaintext")
	}
	var epochID, msgID, provenance, targetDigest, turnID, callID, role, storedKey string
	var complete, sequence int
	if err = store.db.QueryRowContext(ctx, `SELECT m.epoch_id,m.id,m.sequence,m.turn_id,m.call_id,m.role,m.complete,m.provenance,m.key_id,e.target_digest FROM protocol_messages_v2 m JOIN protocol_legacy_imports i ON i.epoch_id=m.epoch_id JOIN protocol_epochs_v2 e ON e.id=m.epoch_id WHERE i.source_key=?`, okRef).Scan(&epochID, &msgID, &sequence, &turnID, &callID, &role, &complete, &provenance, &storedKey, &targetDigest); err != nil {
		t.Fatal(err)
	}
	if provenance != "legacy_import" {
		t.Fatalf("imported provenance=%q", provenance)
	}
	got, err := store.OpenProtocolMessageV2(ctx, keys, ProtocolMessageV2{
		ID: msgID, EpochID: epochID, OwnerScope: owner, Sequence: int64(sequence),
		TurnID: turnID, CallID: callID, Role: role, Complete: complete == 1,
		Provenance: provenance, KeyID: storedKey, TargetDigest: targetDigest, SessionID: sessionID,
	})
	if err != nil || !bytes.Equal(got, okPlain) {
		t.Fatalf("migrated decrypt: %q %v", got, err)
	}
	_ = again
	_ = dek
}

func seedProtocolSession(t *testing.T, store *Store, sessionID string) {
	t.Helper()
	projectID := ulid.Make().String()
	stamp := time.Date(2026, 9, 13, 0, 0, 0, 0, time.UTC).Format(time.RFC3339Nano)
	code := fmt.Sprintf("ITM%08d", protocolSessionSeq.Add(1))
	if _, err := store.db.Exec(`INSERT INTO projects(id,name,project_code,created_at,updated_at) VALUES(?,?,?,?,?)`, projectID, "p", code, stamp, stamp); err != nil {
		t.Fatal(err)
	}
	if _, err := store.db.Exec(`INSERT INTO sessions(id,project_id,title,created_at,updated_at) VALUES(?,?,?,?,?)`, sessionID, projectID, "s", stamp, stamp); err != nil {
		t.Fatal(err)
	}
}

func sha256Hex(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

func withAAD(in ProtocolAAD, mut func(*ProtocolAAD)) ProtocolAAD {
	mut(&in)
	return in
}

// encodeProtocolAADForTest is a hand-derived CONTRACTS §4 encoder. A shorter
// production AAD (or newline-split fields) must not match these bytes.
func encodeProtocolAADForTest(a ProtocolAAD) []byte {
	var buf bytes.Buffer
	putU64 := func(v uint64) {
		var b [8]byte
		binary.BigEndian.PutUint64(b[:], v)
		buf.Write(b[:])
	}
	putStr := func(s string) {
		var b [4]byte
		binary.BigEndian.PutUint32(b[:], uint32(len(s)))
		buf.Write(b[:])
		buf.WriteString(s)
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
		buf.WriteByte(1)
	} else {
		buf.WriteByte(0)
	}
	putStr(a.TurnID)
	putStr(a.CallID)
	putStr(a.Provenance)
	return buf.Bytes()
}
