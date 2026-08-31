package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/lunitide/lunitide/internal/identity"
)

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
	return rec, true, nil
}

func (s *Store) InsertIdentity(ctx context.Context, rec identity.Record) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO local_identity(singleton, subject_id, public_key, private_key, nickname, avatar, status, department, title, org_name, bio, password_hash, pairing_code, discovery_enabled, created_at, updated_at) VALUES(1,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		rec.SubjectID, rec.PublicKey, rec.PrivateKey, rec.Nickname, rec.Avatar, string(rec.Status), rec.Department, rec.Title, rec.OrgName, rec.Bio, rec.PasswordHash, rec.PairingCode, boolInt(rec.DiscoveryEnabled), rec.CreatedAt, rec.UpdatedAt)
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
	var fromUpdated string
	err := tx.QueryRowContext(ctx, `SELECT memory_enabled, auto_nominate, growth_days, updated_at FROM memory_settings WHERE subject_id=?`, from).Scan(&fromEnabled, &fromAuto, &fromDays, &fromUpdated)
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
	if fromUpdated >= toUpdated {
		if _, err = tx.ExecContext(ctx, `UPDATE memory_settings SET memory_enabled=?, auto_nominate=?, growth_days=?, updated_at=? WHERE subject_id=?`, fromEnabled, fromAuto, fromDays, now, to); err != nil {
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
