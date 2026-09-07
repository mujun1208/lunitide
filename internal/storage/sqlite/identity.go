package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"

	"github.com/lunitide/lunitide/internal/identity"
	"github.com/lunitide/lunitide/internal/secret"
)

// privateKeySentinel is what the local_identity.private_key column holds once
// the real Ed25519 private key lives in the DPAPI credential store instead of
// the database. It is deliberately a valid 128-char lowercase-hex string so it
// passes the column CHECK constraint (unlike a NUL-prefixed marker), yet it is
// not a real key: it is a fixed, recognizable pattern ("5ea1ed01" x16, i.e.
// "sealed01") that cannot be produced by crypto/rand key generation with any
// meaningful probability.
const privateKeySentinel = "5ea1ed01" + "5ea1ed01" + "5ea1ed01" + "5ea1ed01" +
	"5ea1ed01" + "5ea1ed01" + "5ea1ed01" + "5ea1ed01" +
	"5ea1ed01" + "5ea1ed01" + "5ea1ed01" + "5ea1ed01" +
	"5ea1ed01" + "5ea1ed01" + "5ea1ed01" + "5ea1ed01"

// identitySecretRef keys the local identity private key in the DPAPI store.
// The Origin must parse as a base URL because secret.Ref.Validate normalizes
// it, hence the https sentinel host.
func identitySecretRef() secret.Ref {
	return secret.Ref{
		CredentialRef: "local-identity-privkey",
		ProviderID:    "local-identity-privkey",
		Origin:        "https://identity.local.lunitide.local",
		Protocol:      "identity-privkey",
	}
}

// sealPrivateKey moves a plaintext private key hex into DPAPI and returns the
// sentinel to persist in the column. When no secret store is wired it returns
// the plaintext unchanged so isolated/non-Windows tests keep working.
func (s *Store) sealPrivateKey(ctx context.Context, privateKeyHex string) (string, error) {
	if s.identitySecrets == nil {
		return privateKeyHex, nil
	}
	plain := strings.TrimSpace(privateKeyHex)
	if plain == "" || plain == privateKeySentinel {
		return plain, nil
	}
	if err := s.identitySecrets.Put(ctx, identitySecretRef(), []byte(plain)); err != nil {
		return "", errors.New("sqlite: 保存身份私钥失败")
	}
	return privateKeySentinel, nil
}

// resolvePrivateKey fills rec.PrivateKey with the real plaintext hex. Three
// cases mirror the imapp inbound-secret seam:
//   - no secret store wired: leave the column value as-is (tests / legacy).
//   - sentinel present: read the plaintext back out of DPAPI.
//   - a real key still in the column (row written before this change):
//     migrate it into DPAPI now and rewrite the column to the sentinel, so the
//     plaintext copy in the database is gone after the first load.
func (s *Store) resolvePrivateKey(ctx context.Context, rec identity.Record) (identity.Record, error) {
	if s.identitySecrets == nil {
		return rec, nil
	}
	stored := strings.TrimSpace(rec.PrivateKey)
	if stored == "" {
		return rec, nil
	}
	if stored == privateKeySentinel {
		plain, err := s.readPrivateKey(ctx)
		if err != nil {
			return rec, err
		}
		rec.PrivateKey = plain
		return rec, nil
	}
	// Legacy plaintext row: seal it into DPAPI, then overwrite the column with
	// the sentinel so the plaintext no longer survives in the database.
	if err := s.identitySecrets.Put(ctx, identitySecretRef(), []byte(stored)); err != nil {
		return rec, errors.New("sqlite: 迁移身份私钥失败")
	}
	if _, err := s.db.ExecContext(ctx, `UPDATE local_identity SET private_key=? WHERE singleton=1`, privateKeySentinel); err != nil {
		return rec, err
	}
	rec.PrivateKey = stored
	return rec, nil
}

func (s *Store) readPrivateKey(ctx context.Context) (string, error) {
	var out string
	err := s.identitySecrets.WithSecret(ctx, identitySecretRef(), func(plain []byte) error {
		out = string(plain)
		return nil
	})
	if err != nil {
		return "", errors.New("sqlite: 读取身份私钥失败")
	}
	return out, nil
}

func (s *Store) LoadIdentity(ctx context.Context) (identity.Record, bool, error) {
	var rec identity.Record
	var discovery int
	err := s.db.QueryRowContext(ctx, `SELECT subject_id, public_key, private_key, nickname, avatar, status, department, title, org_name, bio, password_hash, pairing_code, discovery_enabled, created_at, updated_at FROM local_identity WHERE singleton=1`).Scan(
		&rec.SubjectID, &rec.PublicKey, &rec.PrivateKey, &rec.Nickname, &rec.Avatar, &rec.Status, &rec.Department, &rec.Title, &rec.OrgName, &rec.Bio, &rec.PasswordHash, &rec.PairingCode, &discovery, &rec.CreatedAt, &rec.UpdatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return identity.Record{}, false, nil
	}
	if err != nil {
		return identity.Record{}, false, err
	}
	rec.DiscoveryEnabled = discovery == 1
	rec, err = s.resolvePrivateKey(ctx, rec)
	if err != nil {
		return identity.Record{}, false, err
	}
	return rec, true, nil
}

func (s *Store) InsertIdentity(ctx context.Context, rec identity.Record) error {
	storedKey, err := s.sealPrivateKey(ctx, rec.PrivateKey)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, `INSERT INTO local_identity(singleton, subject_id, public_key, private_key, nickname, avatar, status, department, title, org_name, bio, password_hash, pairing_code, discovery_enabled, created_at, updated_at) VALUES(1,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		rec.SubjectID, rec.PublicKey, storedKey, rec.Nickname, rec.Avatar, string(rec.Status), rec.Department, rec.Title, rec.OrgName, rec.Bio, rec.PasswordHash, rec.PairingCode, boolInt(rec.DiscoveryEnabled), rec.CreatedAt, rec.UpdatedAt)
	return err
}

func (s *Store) UpdateIdentity(ctx context.Context, rec identity.Record) error {
	_, err := s.db.ExecContext(ctx, `UPDATE local_identity SET nickname=?, avatar=?, status=?, department=?, title=?, org_name=?, bio=?, password_hash=?, pairing_code=?, discovery_enabled=?, updated_at=? WHERE singleton=1`,
		rec.Nickname, rec.Avatar, string(rec.Status), rec.Department, rec.Title, rec.OrgName, rec.Bio, rec.PasswordHash, rec.PairingCode, boolInt(rec.DiscoveryEnabled), rec.UpdatedAt)
	return err
}

func (s *Store) RebindLegacySubject(ctx context.Context, from, to string) error {
	if from == "" || to == "" || from == to {
		return nil
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	now := formatTime(time.Now().UTC())
	if err := rebindMemorySettings(ctx, tx, from, to, now); err != nil {
		return err
	}
	statements := []string{
		`UPDATE memory_candidates SET subject_id=? WHERE subject_id=?`,
		`UPDATE kb_collections SET subject_id=? WHERE subject_id=?`,
		`UPDATE expert_catalog SET subject_id=? WHERE subject_id=? AND NOT EXISTS (SELECT 1 FROM expert_catalog e2 WHERE e2.subject_id=? AND e2.name=expert_catalog.name)`,
		`UPDATE plugin_installs SET subject_id=? WHERE subject_id=? AND NOT EXISTS (SELECT 1 FROM plugin_installs p2 WHERE p2.subject_id=? AND p2.plugin_id=plugin_installs.plugin_id)`,
		`UPDATE device_replicas SET subject_id=? WHERE subject_id=?`,
		`UPDATE feedback_events SET subject_id=? WHERE subject_id=?`,
		`UPDATE eligibility_snapshots SET subject_id=? WHERE subject_id=?`,
		`UPDATE skill_candidates SET subject_id=? WHERE subject_id=?`,
		`UPDATE workflow_candidates SET subject_id=? WHERE subject_id=?`,
		`UPDATE collab_gate_evaluations SET subject_id=? WHERE subject_id=?`,
		`UPDATE collab_gate_decisions SET subject_id=? WHERE subject_id=?`,
		`UPDATE ontology_snapshots SET subject_id=? WHERE subject_id=?`,
		`UPDATE handoffs SET sender=? WHERE sender=?`,
		`UPDATE handoffs SET receiver=? WHERE receiver=?`,
	}
	for _, stmt := range statements {
		if containsTriplePlaceholder(stmt) {
			if _, err := tx.ExecContext(ctx, stmt, to, from, to); err != nil {
				return err
			}
			continue
		}
		if _, err := tx.ExecContext(ctx, stmt, to, from); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func rebindMemorySettings(ctx context.Context, tx *sql.Tx, from, to, now string) error {
	var fromEnabled, fromAuto, fromDays int
	var fromUpdated, fromCaptureMode string
	err := tx.QueryRowContext(ctx, `SELECT memory_enabled, auto_nominate, growth_days, updated_at, capture_mode FROM memory_settings WHERE subject_id=?`, from).Scan(&fromEnabled, &fromAuto, &fromDays, &fromUpdated, &fromCaptureMode)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	var toUpdated string
	err = tx.QueryRowContext(ctx, `SELECT updated_at FROM memory_settings WHERE subject_id=?`, to).Scan(&toUpdated)
	if errors.Is(err, sql.ErrNoRows) {
		_, err = tx.ExecContext(ctx, `UPDATE memory_settings SET subject_id=? WHERE subject_id=?`, to, from)
		return err
	}
	if err != nil {
		return err
	}
	fromTime, err := time.Parse(time.RFC3339Nano, fromUpdated)
	if err != nil {
		return err
	}
	toTime, err := time.Parse(time.RFC3339Nano, toUpdated)
	if err != nil {
		return err
	}
	if !fromTime.Before(toTime) {
		stamp, err := time.Parse(time.RFC3339Nano, now)
		if err != nil {
			return err
		}
		if !stamp.After(toTime) {
			stamp = toTime.Add(time.Nanosecond)
		}
		if _, err = tx.ExecContext(ctx, `UPDATE memory_settings SET memory_enabled=?, auto_nominate=?, growth_days=?, updated_at=?, capture_mode=? WHERE subject_id=?`, fromEnabled, fromAuto, fromDays, formatTime(stamp), fromCaptureMode, to); err != nil {
			return err
		}
	}
	_, err = tx.ExecContext(ctx, `DELETE FROM memory_settings WHERE subject_id=?`, from)
	return err
}

func containsTriplePlaceholder(stmt string) bool {
	count := 0
	for _, c := range stmt {
		if c == '?' {
			count++
		}
	}
	return count == 3
}

func (s *Store) UpsertSelfContact(ctx context.Context, rec identity.Record) error {
	now := rec.UpdatedAt
	if now == "" {
		now = formatTime(time.Now().UTC())
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO people_contacts(subject_id, nickname, avatar, status, department, title, org_name, bio, public_key, pairing_hash, trust_state, host_addr, last_seen_at, created_at, updated_at)
		VALUES(?,?,?,?,?,?,?,?,?,?, 'self', '', ?, ?, ?)
		ON CONFLICT(subject_id) DO UPDATE SET nickname=excluded.nickname, avatar=excluded.avatar, status=excluded.status, department=excluded.department, title=excluded.title, org_name=excluded.org_name, bio=excluded.bio, public_key=excluded.public_key, pairing_hash=excluded.pairing_hash, trust_state='self', last_seen_at=excluded.last_seen_at, updated_at=excluded.updated_at`,
		rec.SubjectID, rec.Nickname, rec.Avatar, string(rec.Status), rec.Department, rec.Title, rec.OrgName, rec.Bio, rec.PublicKey, identity.PairingHash(rec.PairingCode, rec.SubjectID), now, rec.CreatedAt, now)
	return err
}
