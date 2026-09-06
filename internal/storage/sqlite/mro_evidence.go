package sqlite

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"sort"
	"strings"

	"github.com/lunitide/lunitide/internal/mroapp"
)

func (s *Store) MROManualSourcesCurrent(ctx context.Context, rules []mroapp.IntervalRule) (bool, error) {
	if len(rules) == 0 {
		return false, nil
	}
	for _, rule := range rules {
		id := strings.TrimPrefix(strings.TrimSpace(rule.SourceCite), "manual:")
		var count int
		err := s.mroDB(ctx).QueryRowContext(ctx, `SELECT COUNT(*) FROM mro_manuals WHERE manual_id=? AND org_id IS ? AND status='controlled'`, id, mroOrg(ctx)).Scan(&count)
		if err != nil {
			return false, err
		}
		if count != 1 {
			return false, nil
		}
		rows, err := s.mroDB(ctx).QueryContext(ctx, `SELECT document_id FROM mro_manual_docs WHERE manual_id=? AND org_id IS ? ORDER BY part_no`, id, mroOrg(ctx))
		if err != nil {
			return false, err
		}
		var ids []string
		for rows.Next() {
			var id string
			if err = rows.Scan(&id); err != nil {
				break
			}
			ids = append(ids, id)
		}
		if err == nil {
			err = rows.Err()
		}
		_ = rows.Close()
		if err != nil {
			return false, err
		}
		if len(ids) == 0 {
			return false, nil
		}
		for _, id := range ids {
			if err = s.mroDocumentReady(ctx, id); errors.Is(err, mroapp.ErrConstraints) {
				return false, nil
			} else if err != nil {
				return false, err
			}
		}
	}
	return true, nil
}

func (s *Store) mroDocumentReady(ctx context.Context, id string) error {
	proof, ok := s.mroProvedDocument(ctx, id)
	if !ok {
		return mroapp.ErrConstraints
	}
	var count int
	err := s.mroDB(ctx).QueryRowContext(ctx, `SELECT COUNT(*) FROM kb_documents d WHERE d.document_id=? AND d.version=? AND d.sha256=? AND d.version=(SELECT MAX(x.version) FROM kb_documents x WHERE x.document_id=d.document_id) AND d.index_state='ready' AND `+KBSourceCurrentPredicate, id, proof.version, proof.digest).Scan(&count)
	if err != nil {
		return err
	}
	if count != 1 {
		return mroapp.ErrConstraints
	}
	return nil
}

// MROEvidenceDigest captures the full current private planning inventory and
// current KB versions referenced by its manuals within the same transaction.
// Historical request receipts/todos are excluded so replay cannot invalidate
// itself. Sorting canonical row JSON is independent of SQLite rowid/VACUUM.
func (s *Store) MROEvidenceDigest(ctx context.Context) (string, error) {
	h := sha256.New()
	for _, table := range []string{"mro_aircraft", "mro_manuals", "mro_manual_docs", "mro_due_items", "mro_utilization_events", "mro_tools", "mro_chem_lots", "mro_kits", "mro_kit_items", "mro_parts_stock", "mro_alternates", "mro_aog_cases", "mro_interval_rules", "mro_work_packages", "mro_wp_tasks", "mro_schedule_assignments", "mro_capacity_slots"} {
		rows, err := s.mroDB(ctx).QueryContext(ctx, `SELECT * FROM `+table+` WHERE org_id IS ? LIMIT 10001`, mroOrg(ctx))
		if err != nil {
			return "", err
		}
		values, err := mroCanonicalRows(rows)
		if err != nil {
			return "", err
		}
		_, _ = h.Write([]byte(table + "\n"))
		for _, value := range values {
			_, _ = h.Write([]byte(value + "\n"))
		}
	}
	rows, err := s.mroDB(ctx).QueryContext(ctx, `SELECT d.document_id,d.version,d.sha256,d.index_state,d.source_locator,`+KBSourceCurrentPredicate+` FROM kb_documents d WHERE d.version=(SELECT MAX(x.version) FROM kb_documents x WHERE x.document_id=d.document_id) AND EXISTS(SELECT 1 FROM mro_manual_docs md WHERE md.document_id=d.document_id AND md.org_id IS ?) LIMIT 10001`, mroOrg(ctx))
	if err != nil {
		return "", err
	}
	values, err := mroCanonicalRows(rows)
	if err != nil {
		return "", err
	}
	for _, value := range values {
		_, _ = h.Write([]byte(value + "\n"))
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
func mroCanonicalRows(rows *sql.Rows) ([]string, error) {
	defer rows.Close()
	columns, err := rows.Columns()
	if err != nil {
		return nil, err
	}
	var out []string
	for rows.Next() {
		if len(out) >= 10000 {
			return nil, mroapp.ErrCapacity
		}
		values := make([]any, len(columns))
		targets := make([]any, len(values))
		for i := range values {
			targets[i] = &values[i]
		}
		if err = rows.Scan(targets...); err != nil {
			return nil, err
		}
		raw, err := json.Marshal(values)
		if err != nil {
			return nil, err
		}
		out = append(out, string(raw))
	}
	sort.Strings(out)
	return out, rows.Err()
}
func (s *Store) GetMROPublication(ctx context.Context, packageID string) (mroapp.Publication, error) {
	var p mroapp.Publication
	var raw string
	err := s.mroDB(ctx).QueryRowContext(ctx, `SELECT evidence_digest,todos_json,created_at FROM mro_publications WHERE package_id=? AND org_id IS ?`, packageID, mroOrg(ctx)).Scan(&p.EvidenceDigest, &raw, &p.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return p, mroapp.ErrNotFound
	}
	if err != nil {
		return p, err
	}
	err = json.Unmarshal([]byte(raw), &p.Todos)
	return p, err
}
func (s *Store) PutMROPublication(ctx context.Context, packageID string, p mroapp.Publication) error {
	raw, err := json.Marshal(p.Todos)
	if err != nil {
		return err
	}
	_, err = s.mroDB(ctx).ExecContext(ctx, `INSERT INTO mro_publications(package_id,org_id,evidence_digest,todos_json,created_at) VALUES(?,?,?,?,?)`, packageID, mroOrg(ctx), p.EvidenceDigest, string(raw), p.CreatedAt)
	return err
}
