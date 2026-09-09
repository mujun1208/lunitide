package sqlite

import (
	"context"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	domain "github.com/lunitide/lunitide/internal/domain/officestudio"
	"github.com/oklog/ulid/v2"
)

var _ domain.BlobStore = (*Store)(nil)

func officeBlobDigest(ref string) bool {
	if len(ref) != 64 || strings.ToLower(ref) != ref {
		return false
	}
	_, err := hex.DecodeString(ref)
	return err == nil
}

// Acquire the SQLite write lock before reading eligibility. GC, registration,
// reservations and publication all use this lock, including separate Stores.
func lockOfficeStorage(ctx context.Context, tx *sql.Tx) error {
	_, err := tx.ExecContext(ctx, `UPDATE office_storage_control SET revision=revision+1 WHERE id=1`)
	return err
}

func (s *Store) officeStorageTx(ctx context.Context) (*sql.Tx, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	if err = lockOfficeStorage(ctx, tx); err != nil {
		tx.Rollback()
		return nil, err
	}
	return tx, nil
}

func officeBlobEvent(ctx context.Context, tx *sql.Tx, digest, action string, detail any, now time.Time) error {
	raw, err := json.Marshal(detail)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO office_blob_events(id,digest,action,detail_json,created_at) VALUES(?,?,?,?,?)`, ulid.Make().String(), digest, action, string(raw), officeRFC(now))
	return err
}

func officeBlobReferenced(ctx context.Context, tx *sql.Tx, digest string) (bool, error) {
	// The immutable version is a permanent root, irrespective of session deletion,
	// organization, current head, acceptance, source edges or delivery bundles.
	var n int
	err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM office_blob_references WHERE digest=?) OR EXISTS(SELECT 1 FROM artifact_versions WHERE content_ref=?) OR EXISTS(SELECT 1 FROM office_validation_runs WHERE json_extract(evidence_json,'$.pdfRef')=?)`, digest, digest, digest).Scan(&n)
	return n != 0, err
}

func (s *Store) ReconcileOfficeStorage(ctx context.Context, scan func() ([]domain.BlobFile, error)) error {
	if scan == nil {
		return domain.ErrInvalid
	}
	tx, err := s.officeStorageTx(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	files, err := scan() // must not race a registered writer or collector
	if err != nil {
		return err
	}
	now := officeRFC(time.Now().UTC())
	var extra int64
	for _, f := range files {
		if f.Size < 0 || f.Size > 1<<50 {
			return domain.ErrInvalid
		}
		if !officeBlobDigest(f.Digest) {
			var known int
			if err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM office_blob_leases WHERE stage_name=?)`, f.Name).Scan(&known); err != nil {
				return err
			}
			if known == 0 {
				extra += f.Size
			}
			continue
		}
		// A pre-existing unknown file counts against capacity but does not become
		// collectible merely because its basename resembles a digest.
		_, err = tx.ExecContext(ctx, `INSERT INTO office_blobs(digest,size,state,managed,created_at,updated_at) VALUES(?,?,'ready',0,?,?) ON CONFLICT(digest) DO UPDATE SET size=max(office_blobs.size,excluded.size),state=CASE WHEN office_blobs.state='removed' THEN 'ready' ELSE office_blobs.state END,managed=CASE WHEN office_blobs.state='removed' THEN 0 ELSE office_blobs.managed END`, f.Digest, f.Size, now, now)
		if err != nil {
			return err
		}
	}
	if _, err = tx.ExecContext(ctx, `UPDATE office_storage_control SET extra_bytes=? WHERE id=1`, extra); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) ReserveOfficeBlob(ctx context.Context, r domain.BlobReservation) (domain.BlobLease, error) {
	var lease domain.BlobLease
	if !officeBlobDigest(r.Digest) || r.Size < 1 || r.Size > 32<<20 || r.MaxBytes < 1 || r.Now.IsZero() || !r.ExpiresAt.After(r.Now) || r.ExpiresAt.Sub(r.Now) > 24*time.Hour {
		return lease, domain.ErrInvalid
	}
	tx, err := s.officeStorageTx(ctx)
	if err != nil {
		return lease, err
	}
	defer tx.Rollback()
	if err = officeAuthorize(ctx, tx, "office-task", r.TaskID); err != nil {
		return lease, err
	}
	var size int64
	var state string
	err = tx.QueryRowContext(ctx, `SELECT size,state FROM office_blobs WHERE digest=?`, r.Digest).Scan(&size, &state)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return lease, err
	}
	if err == nil && size != 0 && size != r.Size {
		return lease, domain.ErrConflict
	}
	if errors.Is(err, sql.ErrNoRows) || state == "removed" {
		var used int64
		if err = tx.QueryRowContext(ctx, `SELECT COALESCE((SELECT sum(size) FROM office_blobs WHERE state<>'removed'),0)+(SELECT extra_bytes FROM office_storage_control WHERE id=1)`).Scan(&used); err != nil {
			return lease, err
		}
		if used > r.MaxBytes || r.Size > r.MaxBytes-used {
			return lease, domain.ErrStorageQuota
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO office_blobs(digest,size,state,managed,created_at,updated_at) VALUES(?,?,'pending',1,?,?) ON CONFLICT(digest) DO UPDATE SET size=excluded.size,state='pending',managed=1,updated_at=excluded.updated_at`, r.Digest, r.Size, officeRFC(r.Now), officeRFC(r.Now))
		if err != nil {
			return lease, err
		}
	} else if size == 0 {
		// Migrated preview references acquire a measured size before reuse.
		if _, err = tx.ExecContext(ctx, `UPDATE office_blobs SET size=? WHERE digest=?`, r.Size, r.Digest); err != nil {
			return lease, err
		}
	}
	lease = domain.BlobLease{ID: ulid.Make().String(), TaskID: r.TaskID, Digest: r.Digest, Size: r.Size, ExpiresAt: r.ExpiresAt}
	lease.StageName = "stage-" + lease.ID
	_, err = tx.ExecContext(ctx, `INSERT INTO office_blob_leases(id,digest,task_id,owner_org_id,stage_name,expires_at,created_at) VALUES(?,?,?,?,?,?,?)`, lease.ID, lease.Digest, lease.TaskID, domain.Scope(ctx), lease.StageName, officeRFC(lease.ExpiresAt), officeRFC(r.Now))
	if err != nil {
		return lease, err
	}
	if err = officeBlobEvent(ctx, tx, lease.Digest, "reserved", map[string]any{"leaseId": lease.ID, "taskId": lease.TaskID, "bytes": r.Size}, r.Now); err != nil {
		return lease, err
	}
	return lease, tx.Commit()
}

func officeLease(ctx context.Context, tx *sql.Tx, id string, now time.Time) (domain.BlobLease, error) {
	var lease domain.BlobLease
	var expires, owner string
	err := tx.QueryRowContext(ctx, `SELECT l.id,l.task_id,l.digest,b.size,l.stage_name,l.expires_at,l.owner_org_id FROM office_blob_leases l JOIN office_blobs b ON b.digest=l.digest WHERE l.id=? AND b.state<>'removed'`, id).Scan(&lease.ID, &lease.TaskID, &lease.Digest, &lease.Size, &lease.StageName, &expires, &owner)
	if errors.Is(err, sql.ErrNoRows) {
		return lease, domain.ErrBlobLease
	}
	if err != nil {
		return lease, err
	}
	if owner != domain.Scope(ctx) {
		return lease, domain.ErrScope
	}
	if err = officeAuthorize(ctx, tx, "office-task", lease.TaskID); err != nil {
		return lease, err
	}
	lease.ExpiresAt, err = parseRFC(expires)
	if err != nil {
		return lease, err
	}
	if !lease.ExpiresAt.After(now) {
		return lease, domain.ErrBlobLease
	}
	return lease, nil
}

func (s *Store) WriteOfficeBlob(ctx context.Context, id string, now time.Time, write func(domain.BlobLease) error) error {
	if write == nil {
		return domain.ErrInvalid
	}
	tx, err := s.officeStorageTx(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	l, err := officeLease(ctx, tx, id, now)
	if err != nil {
		return err
	}
	if err = write(l); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE office_blobs SET state='ready',updated_at=? WHERE digest=?`, officeRFC(now), l.Digest); err != nil {
		return err
	}
	if err = officeBlobEvent(ctx, tx, l.Digest, "ready", map[string]string{"leaseId": id}, now); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) RenewOfficeBlob(ctx context.Context, id string, now, expires time.Time) error {
	if !expires.After(now) || expires.Sub(now) > 24*time.Hour {
		return domain.ErrInvalid
	}
	tx, err := s.officeStorageTx(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = officeLease(ctx, tx, id, now); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE office_blob_leases SET expires_at=max(expires_at,?) WHERE id=?`, officeRFC(expires), id); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) ReleaseOfficeBlob(ctx context.Context, id string, now time.Time, remove func(domain.BlobFile, []string) (int64, error)) error {
	if id == "" {
		return nil
	}
	tx, err := s.officeStorageTx(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var digest, stage, owner, state string
	var size int64
	err = tx.QueryRowContext(ctx, `SELECT l.digest,l.stage_name,l.owner_org_id,b.state,b.size FROM office_blob_leases l JOIN office_blobs b ON b.digest=l.digest WHERE l.id=?`, id).Scan(&digest, &stage, &owner, &state, &size)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	if owner != domain.Scope(ctx) {
		return domain.ErrScope
	}
	if _, err = tx.ExecContext(ctx, `DELETE FROM office_blob_leases WHERE id=?`, id); err != nil {
		return err
	}
	var others int
	if err = tx.QueryRowContext(ctx, `SELECT count(*) FROM office_blob_leases WHERE digest=?`, digest).Scan(&others); err != nil {
		return err
	}
	referenced, err := officeBlobReferenced(ctx, tx, digest)
	if err != nil {
		return err
	}
	if state == "pending" && others == 0 && !referenced && remove != nil {
		if _, err = remove(domain.BlobFile{Digest: digest, Size: size}, []string{stage}); err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, `UPDATE office_blobs SET state='removed',updated_at=? WHERE digest=?`, officeRFC(now), digest); err != nil {
			return err
		}
	} else if remove != nil {
		// Remove this registered stage before forgetting its lease, including
		// when another writer of the same digest still owns a reservation.
		if _, err = remove(domain.BlobFile{Digest: digest, Size: size, StageOnly: true}, []string{stage}); err != nil {
			return err
		}
	}
	if err = officeBlobEvent(ctx, tx, digest, "released", map[string]string{"leaseId": id}, now); err != nil {
		return err
	}
	return tx.Commit()
}

// bindOfficeBlob is part of the version/QA transaction. It never changes an
// immutable version and is excluded from the request's idempotency digest.
func bindOfficeBlob(ctx context.Context, tx *sql.Tx, taskID, ref string, size int64, leaseID, kind, id string, now time.Time) error {
	if !officeBlobDigest(ref) {
		if leaseID != "" {
			return domain.ErrInvalid
		}
		return nil
	}
	if leaseID != "" {
		lease, err := officeLease(ctx, tx, leaseID, now)
		if err != nil {
			return err
		}
		if lease.Digest != ref || lease.TaskID != taskID || (size > 0 && lease.Size != size) {
			return domain.ErrScope
		}
		var state string
		if err = tx.QueryRowContext(ctx, `SELECT state FROM office_blobs WHERE digest=?`, ref).Scan(&state); err != nil {
			return err
		}
		if state != "ready" {
			return domain.ErrBlobLease
		}
	} else {
		// Legacy/direct metadata writers remain compatible. The file service is
		// responsible for actual bytes; an unmanaged root is never TTL-collected.
		_, err := tx.ExecContext(ctx, `INSERT INTO office_blobs(digest,size,state,managed,created_at,updated_at) VALUES(?,?,'ready',0,?,?) ON CONFLICT(digest) DO UPDATE SET size=max(size,excluded.size),state='ready'`, ref, size, officeRFC(now), officeRFC(now))
		if err != nil {
			return err
		}
	}
	if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO office_blob_references(digest,kind,reference_id,created_at) VALUES(?,?,?,?)`, ref, kind, id, officeRFC(now)); err != nil {
		return err
	}
	if leaseID != "" {
		if _, err := tx.ExecContext(ctx, `DELETE FROM office_blob_leases WHERE id=?`, leaseID); err != nil {
			return err
		}
	}
	return officeBlobEvent(ctx, tx, ref, "referenced", map[string]string{"kind": kind, "referenceId": id}, now)
}

func (s *Store) OfficeStorageUsage(ctx context.Context, now time.Time) (domain.StorageUsage, error) {
	var u domain.StorageUsage
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return u, err
	}
	defer tx.Rollback()
	err = tx.QueryRowContext(ctx, `SELECT COALESCE(sum(CASE WHEN state='ready' THEN size ELSE 0 END),0),COALESCE(sum(CASE WHEN state='pending' THEN size ELSE 0 END),0),COALESCE(sum(CASE WHEN managed=0 AND state<>'removed' THEN size ELSE 0 END),0),COALESCE(sum(CASE WHEN state<>'removed' THEN 1 ELSE 0 END),0) FROM office_blobs`).Scan(&u.UsedBytes, &u.ReservedBytes, &u.UnmanagedBytes, &u.BlobCount)
	if err != nil {
		return u, err
	}
	var extra int64
	if err = tx.QueryRowContext(ctx, `SELECT extra_bytes FROM office_storage_control WHERE id=1`).Scan(&extra); err != nil {
		return u, err
	}
	u.UsedBytes += extra
	u.UnmanagedBytes += extra
	u.TotalBytes = u.UsedBytes + u.ReservedBytes
	if err = tx.QueryRowContext(ctx, `SELECT COALESCE(sum(size),0) FROM office_blobs b WHERE state<>'removed' AND EXISTS(SELECT 1 FROM office_blob_references r WHERE r.digest=b.digest)`).Scan(&u.ReferencedBytes); err != nil {
		return u, err
	}
	if err = tx.QueryRowContext(ctx, `SELECT count(*) FROM office_blob_leases WHERE expires_at>?`, officeRFC(now)).Scan(&u.ActiveLeaseCount); err != nil {
		return u, err
	}
	return u, nil
}

func (s *Store) SweepOfficeStorage(ctx context.Context, opts domain.StorageSweepOptions, now time.Time, grace time.Duration, remove func(domain.BlobFile, []string) (int64, error)) (domain.StorageSweepReport, error) {
	if !opts.DryRun && opts.RequestKey != "" {
		return s.sweepOfficeStorageWithReceipt(ctx, opts, now, grace, remove)
	}
	report := domain.StorageSweepReport{DryRun: opts.DryRun, Errors: []string{}}
	if grace < 0 || remove == nil {
		return report, domain.ErrInvalid
	}
	limit := opts.Limit
	if limit < 1 {
		limit = 100
	}
	if limit > 200 {
		return report, domain.ErrInvalid
	}
	cutoff := officeRFC(now.Add(-grace))
	rows, err := s.db.QueryContext(ctx, `SELECT digest FROM office_blobs b WHERE managed=1 AND state<>'removed' AND updated_at<=? AND NOT EXISTS(SELECT 1 FROM office_blob_references r WHERE r.digest=b.digest) AND NOT EXISTS(SELECT 1 FROM office_blob_leases l WHERE l.digest=b.digest AND l.expires_at>?) ORDER BY updated_at,digest LIMIT ?`, cutoff, officeRFC(now), limit+1)
	if err != nil {
		return report, err
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
		return report, err
	}
	if len(ids) > limit {
		report.HasMore = true
		ids = ids[:limit]
	}
	for _, id := range ids {
		if err = ctx.Err(); err != nil {
			return report, err
		}
		candidate, freed, reserved, e := s.sweepOfficeBlob(ctx, id, opts.DryRun, now, cutoff, remove)
		if e != nil {
			report.Errors = append(report.Errors, fmt.Sprintf("%s: %v", id[:12], e))
			continue
		}
		if candidate {
			report.Candidates++
			if !opts.DryRun {
				report.Removed++
				report.FreedBytes += freed
				report.ReleasedReservationBytes += reserved
			}
		}
	}
	return report, nil
}

func (s *Store) sweepOfficeBlob(ctx context.Context, id string, dry bool, now time.Time, cutoff string, remove func(domain.BlobFile, []string) (int64, error)) (bool, int64, int64, error) {
	tx, err := s.officeStorageTx(ctx)
	if err != nil {
		return false, 0, 0, err
	}
	defer tx.Rollback()
	candidate, freed, reserved, err := collectOfficeBlob(ctx, tx, id, dry, now, cutoff, remove)
	if err != nil {
		return false, 0, 0, err
	}
	if dry {
		return candidate, freed, reserved, nil
	}
	return candidate, freed, reserved, tx.Commit()
}

func collectOfficeBlob(ctx context.Context, tx *sql.Tx, id string, dry bool, now time.Time, cutoff string, remove func(domain.BlobFile, []string) (int64, error)) (bool, int64, int64, error) {
	var f domain.BlobFile
	f.Digest = id
	var state string
	err := tx.QueryRowContext(ctx, `SELECT size,state FROM office_blobs b WHERE digest=? AND managed=1 AND state<>'removed' AND updated_at<=? AND NOT EXISTS(SELECT 1 FROM office_blob_leases l WHERE l.digest=b.digest AND l.expires_at>?)`, id, cutoff, officeRFC(now)).Scan(&f.Size, &state)
	if errors.Is(err, sql.ErrNoRows) {
		return false, 0, 0, nil
	}
	if err != nil {
		return false, 0, 0, err
	}
	referenced, err := officeBlobReferenced(ctx, tx, id)
	if err != nil || referenced {
		return false, 0, 0, err
	}
	if dry {
		return true, 0, 0, nil
	}
	rows, err := tx.QueryContext(ctx, `SELECT stage_name FROM office_blob_leases WHERE digest=?`, id)
	if err != nil {
		return false, 0, 0, err
	}
	stages := []string{}
	for rows.Next() {
		var stage string
		if err = rows.Scan(&stage); err != nil {
			break
		}
		stages = append(stages, stage)
	}
	if err == nil {
		err = rows.Err()
	}
	rows.Close()
	if err != nil {
		return false, 0, 0, err
	}
	// The final reference/lease check and filesystem removal share the same
	// write lock. Another publisher cannot pass its eligibility check mid-delete.
	freed, err := remove(f, stages)
	if err != nil {
		return false, 0, 0, err
	}
	if _, err = tx.ExecContext(ctx, `DELETE FROM office_blob_leases WHERE digest=?`, id); err != nil {
		return false, 0, 0, err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE office_blobs SET state='removed',updated_at=? WHERE digest=?`, officeRFC(now), id); err != nil {
		return false, 0, 0, err
	}
	if err = officeBlobEvent(ctx, tx, id, "removed", map[string]any{"freedBytes": freed, "stageCount": len(stages)}, now); err != nil {
		return false, 0, 0, err
	}
	var reserved int64
	if state == "pending" {
		reserved = f.Size
	}
	return true, freed, reserved, nil
}
