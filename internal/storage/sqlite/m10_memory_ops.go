// M10 memory-operations storage (migration 0075): settings, fact flags and
// growth-box sidecars over the 0061 memory core, plus the aggregate reads
// (stats/paged facts/traces) and the export/purge units used by the
// memory.* operations Bridge. All writes are single audited transactions.
// Sentinel errors and row projections live in m8core so the m8app service
// can map M10-MO failure codes without an import cycle.
package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/lunitide/lunitide/internal/domain/m8core"
)

// ErrFactNotFound / ErrGrowthNotObserving are matched by the m8app
// memory-ops service via errors.Is to map M10-MO failure codes.
var (
	ErrFactNotFound       = m8core.ErrFactNotFound
	ErrGrowthNotObserving = m8core.ErrGrowthNotObserving
)

// GetMemorySettings returns the subject profile, or the implicit defaults
// when no row exists (settings are lazily materialized on first update).
func (s *Store) GetMemorySettings(ctx context.Context, subjectID string) (m8core.MemorySettings, error) {
	var enabled, auto int
	var growthDays int
	var created, updated string
	err := s.db.QueryRowContext(ctx,
		`SELECT memory_enabled, auto_nominate, growth_days, created_at, updated_at FROM memory_settings WHERE subject_id=?`,
		subjectID).Scan(&enabled, &auto, &growthDays, &created, &updated)
	if errors.Is(err, sql.ErrNoRows) {
		return m8core.DefaultMemorySettings(subjectID), nil
	}
	if err != nil {
		return m8core.MemorySettings{}, err
	}
	return m8core.MemorySettings{SubjectID: subjectID, MemoryEnabled: enabled == 1, AutoNominate: auto == 1, GrowthDays: growthDays, CreatedAt: created, UpdatedAt: updated}, nil
}

// UpsertMemorySettings inserts or refreshes the subject profile.
func (s *Store) UpsertMemorySettings(ctx context.Context, settings m8core.MemorySettings) error {
	now := formatTime(time.Now().UTC())
	created := now
	if existing, err := s.GetMemorySettings(ctx, settings.SubjectID); err == nil && existing.CreatedAt != "" {
		created = existing.CreatedAt
	}
	return s.execWithAudit(ctx, "memory.settings.update", settings.SubjectID, "renderer",
		map[string]any{"memoryEnabled": settings.MemoryEnabled, "autoNominate": settings.AutoNominate, "growthDays": settings.GrowthDays},
		func(tx *sql.Tx) error {
			_, err := tx.ExecContext(ctx,
				`INSERT INTO memory_settings(subject_id, memory_enabled, auto_nominate, growth_days, created_at, updated_at)
				 VALUES(?,?,?,?,?,?)
				 ON CONFLICT(subject_id) DO UPDATE SET memory_enabled=excluded.memory_enabled, auto_nominate=excluded.auto_nominate, growth_days=excluded.growth_days, updated_at=excluded.updated_at`,
				settings.SubjectID, boolInt(settings.MemoryEnabled), boolInt(settings.AutoNominate), settings.GrowthDays, created, now)
			return err
		})
}

func boolInt(v bool) int {
	if v {
		return 1
	}
	return 0
}

// ListFactsPaged returns the newest version of each fact matching the
// optional state/scope filter, newest first, plus the match total.
func (s *Store) ListFactsPaged(ctx context.Context, state, scope string, limit, offset int) ([]m8core.FactRow, int, error) {
	where, args := "WHERE 1=1", []any{}
	if state != "" {
		where += " AND f.state=?"
		args = append(args, state)
	}
	if scope != "" {
		where += " AND f.scope_id=?"
		args = append(args, scope)
	}
	var total int
	if err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM memory_facts f
		 JOIN (SELECT fact_id, MAX(version) v FROM memory_facts GROUP BY fact_id) m ON f.fact_id=m.fact_id AND f.version=m.v `+where,
		args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT f.fact_id, f.scope_id, f.version, f.sensitivity, f.state, f.created_at FROM memory_facts f
		 JOIN (SELECT fact_id, MAX(version) v FROM memory_facts GROUP BY fact_id) m ON f.fact_id=m.fact_id AND f.version=m.v `+where+
			` ORDER BY f.created_at DESC LIMIT ? OFFSET ?`, append(args, limit, offset)...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	var out []m8core.FactRow
	for rows.Next() {
		var r m8core.FactRow
		if err := rows.Scan(&r.FactID, &r.ScopeID, &r.Version, &r.Sensitivity, &r.State, &r.CreatedAt); err != nil {
			return nil, 0, err
		}
		out = append(out, r)
	}
	return out, total, rows.Err()
}

// SetFactFlag upserts one (fact_id, flag) marker; the fact must exist.
func (s *Store) SetFactFlag(ctx context.Context, factID, flag, note string) error {
	return s.execWithAudit(ctx, "memory.fact.flag", factID, "renderer",
		map[string]any{"flag": flag, "note": note},
		func(tx *sql.Tx) error {
			var one int
			if err := tx.QueryRowContext(ctx, `SELECT 1 FROM memory_facts WHERE fact_id=? LIMIT 1`, factID).Scan(&one); err != nil {
				if errors.Is(err, sql.ErrNoRows) {
					return ErrFactNotFound
				}
				return err
			}
			now := formatTime(time.Now().UTC())
			_, err := tx.ExecContext(ctx,
				`INSERT INTO memory_fact_flags(fact_id, flag, note, created_at, updated_at) VALUES(?,?,?,?,?)
				 ON CONFLICT(fact_id, flag) DO UPDATE SET note=excluded.note, updated_at=excluded.updated_at`,
				factID, flag, note, now, now)
			return err
		})
}

// ClearFactFlag removes one marker; clearing an absent flag is a no-op.
func (s *Store) ClearFactFlag(ctx context.Context, factID, flag string) error {
	return s.execWithAudit(ctx, "memory.fact.unflag", factID, "renderer",
		map[string]any{"flag": flag},
		func(tx *sql.Tx) error {
			_, err := tx.ExecContext(ctx, `DELETE FROM memory_fact_flags WHERE fact_id=? AND flag=?`, factID, flag)
			return err
		})
}

// ListFactFlags returns every marker row (joined with nothing: the service
// merges them onto fact rows by id).
func (s *Store) ListFactFlags(ctx context.Context) ([]m8core.FactFlag, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT fact_id, flag, note, created_at, updated_at FROM memory_fact_flags ORDER BY updated_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []m8core.FactFlag
	for rows.Next() {
		var f m8core.FactFlag
		if err := rows.Scan(&f.FactID, &f.Flag, &f.Note, &f.CreatedAt, &f.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	return out, rows.Err()
}

// ListRecallTracesPaged returns recall traces newest first plus the total.
func (s *Store) ListRecallTracesPaged(ctx context.Context, limit, offset int) ([]m8core.TraceRow, int, error) {
	var total int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM memory_recall_traces`).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, query_digest, hits_json, reasons_json, policy_redactions_json, created_at FROM memory_recall_traces ORDER BY created_at DESC LIMIT ? OFFSET ?`,
		limit, offset)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	var out []m8core.TraceRow
	for rows.Next() {
		var t m8core.TraceRow
		if err := rows.Scan(&t.ID, &t.QueryDigest, &t.HitsJSON, &t.ReasonsJSON, &t.PolicyRedactionsJSON, &t.CreatedAt); err != nil {
			return nil, 0, err
		}
		out = append(out, t)
	}
	return out, total, rows.Err()
}

// UpsertGrowthEntry registers a fact in the growth box (or refreshes its
// review window when already observing).
func (s *Store) UpsertGrowthEntry(ctx context.Context, entry m8core.GrowthEntry) error {
	now := formatTime(time.Now().UTC())
	return s.execWithAudit(ctx, "memory.growth.enroll", entry.FactID, "engine",
		map[string]any{"scopeId": entry.ScopeID, "reviewAt": entry.ReviewAt},
		func(tx *sql.Tx) error {
			_, err := tx.ExecContext(ctx,
				`INSERT INTO memory_growth_box(fact_id, scope_id, status, reference_count, last_referenced_at, review_at, decided_at, created_at, updated_at)
				 VALUES(?,?,?,0,NULL,?,NULL,?,?)
				 ON CONFLICT(fact_id) DO UPDATE SET review_at=excluded.review_at, updated_at=excluded.updated_at`,
				entry.FactID, entry.ScopeID, m8core.GrowthObserving, entry.ReviewAt, now, now)
			return err
		})
}

// ListGrowthEntries returns growth-box rows filtered by optional status,
// oldest review first, plus the total.
func (s *Store) ListGrowthEntries(ctx context.Context, status string, limit, offset int) ([]m8core.GrowthEntry, int, error) {
	where, args := "", []any{}
	if status != "" {
		where = "WHERE status=?"
		args = append(args, status)
	}
	var total int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM memory_growth_box `+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT fact_id, scope_id, status, reference_count, COALESCE(last_referenced_at,''), review_at, COALESCE(decided_at,''), created_at, updated_at
		 FROM memory_growth_box `+where+` ORDER BY review_at ASC LIMIT ? OFFSET ?`, append(args, limit, offset)...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	var out []m8core.GrowthEntry
	for rows.Next() {
		var g m8core.GrowthEntry
		if err := rows.Scan(&g.FactID, &g.ScopeID, &g.Status, &g.ReferenceCount, &g.LastReferencedAt, &g.ReviewAt, &g.DecidedAt, &g.CreatedAt, &g.UpdatedAt); err != nil {
			return nil, 0, err
		}
		out = append(out, g)
	}
	return out, total, rows.Err()
}

// DecideGrowthEntry moves an observing entry to promoted/dropped; a missing
// or already-decided entry fails with ErrGrowthNotObserving.
func (s *Store) DecideGrowthEntry(ctx context.Context, factID, decision string) error {
	now := formatTime(time.Now().UTC())
	return s.execWithAudit(ctx, "memory.growth.decide", factID, "renderer",
		map[string]any{"decision": decision},
		func(tx *sql.Tx) error {
			res, err := tx.ExecContext(ctx,
				`UPDATE memory_growth_box SET status=?, decided_at=?, updated_at=? WHERE fact_id=? AND status=?`,
				decision, now, now, factID, m8core.GrowthObserving)
			if err != nil {
				return err
			}
			if n, _ := res.RowsAffected(); n == 0 {
				return ErrGrowthNotObserving
			}
			return nil
		})
}

// CountFactsBy returns the newest-version fact counts grouped by column
// ("state" or "sensitivity").
func (s *Store) CountFactsBy(ctx context.Context, column string) ([]m8core.GroupCount, error) {
	if column != "state" && column != "sensitivity" {
		return nil, errors.New("invalid group column")
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT f.`+column+`, COUNT(*) FROM memory_facts f
		 JOIN (SELECT fact_id, MAX(version) v FROM memory_facts GROUP BY fact_id) m ON f.fact_id=m.fact_id AND f.version=m.v
		 GROUP BY f.`+column)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []m8core.GroupCount
	for rows.Next() {
		var g m8core.GroupCount
		if err := rows.Scan(&g.Label, &g.Count); err != nil {
			return nil, err
		}
		out = append(out, g)
	}
	return out, rows.Err()
}

// CountCandidatesBy groups candidate rows by state.
func (s *Store) CountCandidatesBy(ctx context.Context) ([]m8core.GroupCount, error) {
	return s.groupCount(ctx, `SELECT state, COUNT(*) FROM memory_candidates GROUP BY state`)
}

// CountGrowthBy groups growth-box rows by status.
func (s *Store) CountGrowthBy(ctx context.Context) ([]m8core.GroupCount, error) {
	return s.groupCount(ctx, `SELECT status, COUNT(*) FROM memory_growth_box GROUP BY status`)
}

// CountTracesSince counts recall traces recorded after the boundary.
func (s *Store) CountTracesSince(ctx context.Context, since time.Time) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM memory_recall_traces WHERE created_at>?`, formatTime(since)).Scan(&n)
	return n, err
}

// CountMemories counts the four-layer memory rows (M5 memories table).
func (s *Store) CountMemories(ctx context.Context) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM memories`).Scan(&n)
	return n, err
}

func (s *Store) groupCount(ctx context.Context, query string) ([]m8core.GroupCount, error) {
	rows, err := s.db.QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []m8core.GroupCount
	for rows.Next() {
		var g m8core.GroupCount
		if err := rows.Scan(&g.Label, &g.Count); err != nil {
			return nil, err
		}
		out = append(out, g)
	}
	return out, rows.Err()
}

// PurgeAllMemoryData is the one-shot clear: active facts are tombstoned
// (the version chain stays immutable), candidates/growth/flags/traces and
// the four-layer memories are deleted, settings survive.
func (s *Store) PurgeAllMemoryData(ctx context.Context) (m8core.MemoryOpsCounts, error) {
	var counts m8core.MemoryOpsCounts
	now := formatTime(time.Now().UTC())
	err := s.execWithAudit(ctx, "memory.purge", "memory", "renderer",
		map[string]any{"scope": "all"},
		func(tx *sql.Tx) error {
			count := func(query string, args ...any) (int64, error) {
				res, err := tx.ExecContext(ctx, query, args...)
				if err != nil {
					return 0, err
				}
				return res.RowsAffected()
			}
			var err error
			if counts.FactsTombstoned, err = count(`UPDATE memory_facts SET state='tombstoned', deleted_at=? WHERE state!='tombstoned'`, now); err != nil {
				return err
			}
			if counts.Candidates, err = count(`DELETE FROM memory_candidates`); err != nil {
				return err
			}
			if counts.GrowthRows, err = count(`DELETE FROM memory_growth_box`); err != nil {
				return err
			}
			if counts.Flags, err = count(`DELETE FROM memory_fact_flags`); err != nil {
				return err
			}
			if counts.Traces, err = count(`DELETE FROM memory_recall_traces`); err != nil {
				return err
			}
			counts.Memories, err = count(`DELETE FROM memories`)
			return err
		})
	return counts, err
}

// ExportAllMemoryData reads every memory surface in one snapshot.
func (s *Store) ExportAllMemoryData(ctx context.Context) (m8core.ExportBundle, error) {
	var bundle m8core.ExportBundle
	rows, err := s.db.QueryContext(ctx,
		`SELECT fact_id, scope_id, version, sensitivity, state, COALESCE(superseded_by,''), COALESCE(deleted_at,''), created_at FROM memory_facts ORDER BY fact_id, version`)
	if err != nil {
		return bundle, err
	}
	for rows.Next() {
		var f m8core.ExportFactVersion
		if err := rows.Scan(&f.FactID, &f.ScopeID, &f.Version, &f.Sensitivity, &f.State, &f.SupersededBy, &f.DeletedAt, &f.CreatedAt); err != nil {
			rows.Close()
			return bundle, err
		}
		bundle.Facts = append(bundle.Facts, f)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return bundle, err
	}

	leafRows, err := s.db.QueryContext(ctx,
		`SELECT id, fact_id, fact_version, json_pointer, evidence_ref, digest, created_at FROM memory_source_leaves ORDER BY fact_id, fact_version`)
	if err != nil {
		return bundle, err
	}
	for leafRows.Next() {
		var l m8core.SourceLeaf
		if err := leafRows.Scan(&l.ID, &l.FactID, &l.FactVersion, &l.JSONPointer, &l.EvidenceRef, &l.Digest, &l.CreatedAt); err != nil {
			leafRows.Close()
			return bundle, err
		}
		bundle.Leaves = append(bundle.Leaves, l)
	}
	leafRows.Close()
	if err := leafRows.Err(); err != nil {
		return bundle, err
	}

	candRows, err := s.db.QueryContext(ctx,
		`SELECT candidate_id, subject_id, payload, payload_digest, inferred, trust, state, COALESCE(confirm_token,''), expires_at, created_at, COALESCE(confirmed_at,'') FROM memory_candidates ORDER BY created_at`)
	if err != nil {
		return bundle, err
	}
	for candRows.Next() {
		var inferred int
		var c m8core.MemoryCandidate
		if err := candRows.Scan(&c.CandidateID, &c.SubjectID, &c.Payload, &c.PayloadDigest, &inferred, &c.Trust, &c.State, &c.ConfirmToken, &c.ExpiresAt, &c.CreatedAt, &c.ConfirmedAt); err != nil {
			candRows.Close()
			return bundle, err
		}
		c.Inferred = inferred == 1
		bundle.Candidates = append(bundle.Candidates, c)
	}
	candRows.Close()
	if err := candRows.Err(); err != nil {
		return bundle, err
	}

	if bundle.Traces, _, err = s.ListRecallTracesPaged(ctx, 1000, 0); err != nil {
		return bundle, err
	}
	if bundle.Growth, _, err = s.ListGrowthEntries(ctx, "", 1000, 0); err != nil {
		return bundle, err
	}
	if bundle.Flags, err = s.ListFactFlags(ctx); err != nil {
		return bundle, err
	}
	setRows, err := s.db.QueryContext(ctx, `SELECT subject_id, memory_enabled, auto_nominate, growth_days, created_at, updated_at FROM memory_settings ORDER BY subject_id`)
	if err != nil {
		return bundle, err
	}
	for setRows.Next() {
		var enabled, auto int
		var st m8core.MemorySettings
		if err := setRows.Scan(&st.SubjectID, &enabled, &auto, &st.GrowthDays, &st.CreatedAt, &st.UpdatedAt); err != nil {
			setRows.Close()
			return bundle, err
		}
		st.MemoryEnabled, st.AutoNominate = enabled == 1, auto == 1
		bundle.Settings = append(bundle.Settings, st)
	}
	setRows.Close()
	return bundle, setRows.Err()
}
