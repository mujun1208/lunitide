package sqlite

import (
	"context"
	"database/sql"
	"strings"

	"github.com/lunitide/lunitide/internal/mroapp"
	"github.com/lunitide/lunitide/internal/sessionapp"
)

func (s *Store) UpsertAircraft(ctx context.Context, row mroapp.Aircraft) error {
	_, err := s.mroWrite(ctx, `INSERT INTO mro_aircraft(aircraft_id,tail_no,msn,model,config,created_at,org_id) VALUES(?,?,?,?,?,?,?)`,
		row.AircraftID, row.TailNo, row.MSN, row.Model, row.Config, row.CreatedAt, mroOrg(ctx))
	if isUniqueViolation(err) {
		return mroapp.ErrDuplicateTail
	}
	return err
}

func (s *Store) ListAircraft(ctx context.Context) ([]mroapp.Aircraft, error) {
	rows, err := s.mroDB(ctx).QueryContext(ctx, `SELECT aircraft_id,tail_no,msn,model,config,created_at FROM mro_aircraft WHERE org_id IS ? ORDER BY tail_no, aircraft_id LIMIT 10001`, mroOrg(ctx))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []mroapp.Aircraft{}
	for rows.Next() {
		var v mroapp.Aircraft
		if err = rows.Scan(&v.AircraftID, &v.TailNo, &v.MSN, &v.Model, &v.Config, &v.CreatedAt); err != nil {
			return nil, err
		}
		if len(items) >= 10000 {
			return nil, mroapp.ErrCapacity
		}
		items = append(items, v)
	}
	return items, rows.Err()
}

func (s *Store) RegisterManual(ctx context.Context, row mroapp.Manual, docs []mroapp.ManualDocInput) error {
	ids := make([]string, 0, len(docs))
	for _, doc := range docs {
		ids = append(ids, doc.DocumentID)
	}
	var err error
	ctx, err = s.PrepareMROEvidence(ctx, ids)
	if err != nil {
		return err
	}
	return s.TransactMRO(ctx, func(ctx context.Context) error {
		var err error
		if _, err = s.mroWrite(ctx, `INSERT INTO mro_manuals(manual_id,title,doc_type,revision,status,ata,created_at,org_id) VALUES(?,?,?,?,?,?,?,?)`,
			row.ManualID, row.Title, row.DocType, row.Revision, row.Status, row.ATA, row.CreatedAt, mroOrg(ctx)); err != nil {
			return err
		}
		for _, d := range docs {
			if err = s.mroDocumentReady(ctx, strings.TrimSpace(d.DocumentID)); err != nil {
				return err
			}
			if _, err = s.mroWrite(ctx, `INSERT INTO mro_manual_docs(manual_id,document_id,part_no,org_id) VALUES(?,?,?,?)`,
				row.ManualID, strings.TrimSpace(d.DocumentID), d.PartNo, mroOrg(ctx)); err != nil {
				return err
			}
		}
		return nil
	})
}

func (s *Store) ListManuals(ctx context.Context) ([]mroapp.Manual, error) {
	rows, err := s.mroDB(ctx).QueryContext(ctx, `SELECT m.manual_id,m.title,m.doc_type,m.revision,m.status,m.ata,m.created_at,COUNT(d.document_id)
FROM mro_manuals m LEFT JOIN mro_manual_docs d ON d.manual_id=m.manual_id WHERE m.org_id IS ?
GROUP BY m.manual_id ORDER BY m.created_at, m.manual_id LIMIT 10001`, mroOrg(ctx))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []mroapp.Manual{}
	for rows.Next() {
		var v mroapp.Manual
		if err = rows.Scan(&v.ManualID, &v.Title, &v.DocType, &v.Revision, &v.Status, &v.ATA, &v.CreatedAt, &v.SectionCount); err != nil {
			return nil, err
		}
		if len(items) >= 10000 {
			return nil, mroapp.ErrCapacity
		}
		items = append(items, v)
	}
	return items, rows.Err()
}

func (s *Store) GetSessionMetadata(ctx context.Context, id string) (string, error) {
	var raw string
	err := s.db.QueryRowContext(ctx, `SELECT metadata_json FROM sessions WHERE id=?`, id).Scan(&raw)
	if err == sql.ErrNoRows {
		return "", sessionapp.ErrSessionNotFound
	}
	return raw, err
}

func (s *Store) PutSessionMetadata(ctx context.Context, id, metadataJSON string) error {
	res, err := s.db.ExecContext(ctx, `UPDATE sessions SET metadata_json=? WHERE id=?`, metadataJSON, id)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return sessionapp.ErrSessionNotFound
	}
	return nil
}
