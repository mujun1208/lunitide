package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	domain "github.com/lunitide/lunitide/internal/domain/officestudio"
	"github.com/oklog/ulid/v2"
)

type officeSweepItemResult struct {
	Candidate bool   `json:"candidate"`
	Freed     int64  `json:"freed"`
	Reserved  int64  `json:"reserved"`
	Error     string `json:"error,omitempty"`
}

func (s *Store) sweepOfficeStorageWithReceipt(ctx context.Context, opts domain.StorageSweepOptions, now time.Time, grace time.Duration, remove func(domain.BlobFile, []string) (int64, error)) (domain.StorageSweepReport, error) {
	empty := domain.StorageSweepReport{Errors: []string{}}
	limit := opts.Limit
	if limit < 1 {
		limit = 100
	}
	if limit > 200 || grace < 0 || remove == nil || !officeKey(opts.RequestKey) {
		return empty, domain.ErrInvalid
	}
	request := officeDigest(struct{ Limit int }{limit})
	tx, err := s.officeStorageTx(ctx)
	if err != nil {
		return empty, err
	}
	var jobID, oldDigest string
	var saved sql.NullString
	err = tx.QueryRowContext(ctx, `SELECT id,request_digest,report_json FROM office_storage_sweeps WHERE owner_org_id=? AND request_key=?`, domain.Scope(ctx), opts.RequestKey).Scan(&jobID, &oldDigest, &saved)
	if err == nil {
		if oldDigest != request {
			tx.Rollback()
			return empty, domain.ErrConflict
		}
		if saved.Valid {
			tx.Rollback()
			var report domain.StorageSweepReport
			err = json.Unmarshal([]byte(saved.String), &report)
			return report, err
		}
	} else if errors.Is(err, sql.ErrNoRows) {
		jobID = ulid.Make().String()
		rows, e := tx.QueryContext(ctx, `SELECT digest FROM office_blobs b WHERE managed=1 AND state<>'removed' AND updated_at<=? AND NOT EXISTS(SELECT 1 FROM office_blob_references r WHERE r.digest=b.digest) AND NOT EXISTS(SELECT 1 FROM office_blob_leases l WHERE l.digest=b.digest AND l.expires_at>?) ORDER BY updated_at,digest LIMIT ?`, officeRFC(now.Add(-grace)), officeRFC(now), limit+1)
		if e != nil {
			tx.Rollback()
			return empty, e
		}
		ids := []string{}
		for rows.Next() {
			var id string
			if e = rows.Scan(&id); e != nil {
				break
			}
			ids = append(ids, id)
		}
		if e == nil {
			e = rows.Err()
		}
		rows.Close()
		if e != nil {
			tx.Rollback()
			return empty, e
		}
		hasMore := len(ids) > limit
		if hasMore {
			ids = ids[:limit]
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO office_storage_sweeps(id,owner_org_id,request_key,request_digest,has_more,created_at) VALUES(?,?,?,?,?,?)`, jobID, domain.Scope(ctx), opts.RequestKey, request, hasMore, officeRFC(now)); err != nil {
			tx.Rollback()
			return empty, err
		}
		for i, id := range ids {
			if _, err = tx.ExecContext(ctx, `INSERT INTO office_storage_sweep_items(sweep_id,digest,ordinal) VALUES(?,?,?)`, jobID, id, i); err != nil {
				tx.Rollback()
				return empty, err
			}
		}
	} else {
		tx.Rollback()
		return empty, err
	}
	if err = tx.Commit(); err != nil {
		return empty, err
	}
	// The durable candidate set is fixed before any physical deletion. Even a
	// cancelled/restarted caller cannot accidentally progress to the next batch.
	rows, err := s.db.QueryContext(ctx, `SELECT digest FROM office_storage_sweep_items WHERE sweep_id=? AND result_json IS NULL ORDER BY ordinal`, jobID)
	if err != nil {
		return empty, err
	}
	ids := []string{}
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
	rows.Close()
	if err != nil {
		return empty, err
	}
	for _, id := range ids {
		if err = s.collectOfficeSweepItem(ctx, jobID, id, now, grace, remove); err != nil {
			return empty, err
		}
	}
	return s.finishOfficeSweep(ctx, jobID)
}

func (s *Store) collectOfficeSweepItem(ctx context.Context, jobID, id string, now time.Time, grace time.Duration, remove func(domain.BlobFile, []string) (int64, error)) error {
	tx, err := s.officeStorageTx(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var saved sql.NullString
	if err = tx.QueryRowContext(ctx, `SELECT i.result_json FROM office_storage_sweep_items i JOIN office_storage_sweeps s ON s.id=i.sweep_id WHERE i.sweep_id=? AND i.digest=? AND s.owner_org_id=?`, jobID, id, domain.Scope(ctx)).Scan(&saved); err != nil {
		return err
	}
	if saved.Valid {
		return nil
	}
	candidate, freed, reserved, err := collectOfficeBlob(ctx, tx, id, false, now, officeRFC(now.Add(-grace)), remove)
	if err != nil {
		tx.Rollback()
		if ctx.Err() != nil {
			return ctx.Err()
		}
		// A filesystem/statement failure records the failed item independently.
		// A simultaneous retry that already completed it wins the write CAS.
		result := officeSweepItemResult{Error: err.Error()}
		if len(result.Error) > 512 {
			result.Error = string([]rune(result.Error)[:min(128, len([]rune(result.Error)))]) + "…"
		}
		raw, _ := json.Marshal(result)
		_, e := s.db.ExecContext(ctx, `UPDATE office_storage_sweep_items SET result_json=? WHERE sweep_id=? AND digest=? AND result_json IS NULL`, string(raw), jobID, id)
		return e
	}
	raw, _ := json.Marshal(officeSweepItemResult{Candidate: candidate, Freed: freed, Reserved: reserved})
	if _, err = tx.ExecContext(ctx, `UPDATE office_storage_sweep_items SET result_json=? WHERE sweep_id=? AND digest=? AND result_json IS NULL`, string(raw), jobID, id); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) finishOfficeSweep(ctx context.Context, id string) (domain.StorageSweepReport, error) {
	r := domain.StorageSweepReport{Errors: []string{}}
	tx, err := s.officeStorageTx(ctx)
	if err != nil {
		return r, err
	}
	defer tx.Rollback()
	var saved sql.NullString
	if err = tx.QueryRowContext(ctx, `SELECT report_json,has_more FROM office_storage_sweeps WHERE id=? AND owner_org_id=?`, id, domain.Scope(ctx)).Scan(&saved, &r.HasMore); err != nil {
		return r, err
	}
	if saved.Valid {
		err = json.Unmarshal([]byte(saved.String), &r)
		return r, err
	}
	rows, err := tx.QueryContext(ctx, `SELECT digest,result_json FROM office_storage_sweep_items WHERE sweep_id=? ORDER BY ordinal`, id)
	if err != nil {
		return r, err
	}
	for rows.Next() {
		var digest string
		var raw sql.NullString
		if err = rows.Scan(&digest, &raw); err != nil {
			break
		}
		if !raw.Valid {
			err = domain.ErrConflict
			break
		}
		var item officeSweepItemResult
		if err = json.Unmarshal([]byte(raw.String), &item); err != nil {
			break
		}
		if item.Error != "" {
			r.Errors = append(r.Errors, digest[:12]+": "+item.Error)
			continue
		}
		if item.Candidate {
			r.Candidates++
			r.Removed++
			r.FreedBytes += item.Freed
			r.ReleasedReservationBytes += item.Reserved
		}
	}
	if err == nil {
		err = rows.Err()
	}
	rows.Close()
	if err != nil {
		return r, err
	}
	raw, err := json.Marshal(r)
	if err != nil {
		return r, err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE office_storage_sweeps SET report_json=? WHERE id=? AND report_json IS NULL`, string(raw), id); err != nil {
		return r, err
	}
	return r, tx.Commit()
}
