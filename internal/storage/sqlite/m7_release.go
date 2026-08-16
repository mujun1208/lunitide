// M7 slice 3 storage (T-7.3.1/T-7.3.3): cr_revisions, release_packages and
// the content-addressed release_blobs on the agent-runtime single-writer
// transaction. agentRuntimeTx additionally satisfies m7app.ReleaseTx;
// TransactRelease asserts the interface at open time. Immutability is
// enforced by the migration-0056 triggers (M7-REV-002 / M7-PKG-001), so
// these methods only perform legal status transitions and inserts.
package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/lunitide/lunitide/internal/domain/agentrun"
	"github.com/lunitide/lunitide/internal/domain/m7flow"
	"github.com/lunitide/lunitide/internal/m7app"
)

// TransactRelease runs an m7app slice-3 use case on the shared single-writer
// transaction.
func (r *AgentRuntimeRepository) TransactRelease(ctx context.Context, fn func(m7app.ReleaseTx) error) error {
	return r.Transact(ctx, func(tx agentrun.Tx) error {
		rtx, ok := tx.(m7app.ReleaseTx)
		if !ok {
			return errors.New("agent runtime tx does not satisfy m7app.ReleaseTx")
		}
		return fn(rtx)
	})
}

const crRevColumns = `id,cr_id,revision_no,manifest_json,digest,status,created_at`

func scanCRRevision(s interface{ Scan(...any) error }) (m7flow.CRRevision, error) {
	var r m7flow.CRRevision
	var created string
	if err := s.Scan(&r.ID, &r.CRID, &r.RevisionNo, &r.ManifestJSON, &r.Digest, &r.Status, &created); err != nil {
		return r, err
	}
	var err error
	if r.CreatedAt, err = parseRFC(created); err != nil {
		return r, err
	}
	return r, nil
}

func (t *agentRuntimeTx) PutCRRevision(r m7flow.CRRevision) error {
	_, err := t.tx.ExecContext(t.ctx, `INSERT INTO cr_revisions
		(id,cr_id,revision_no,manifest_json,digest,status,created_at) VALUES(?,?,?,?,?,?,?)`,
		r.ID, r.CRID, r.RevisionNo, r.ManifestJSON, r.Digest, r.Status, rfc(r.CreatedAt))
	return t.fail(err)
}

func (t *agentRuntimeTx) GetCRRevision(id string) (m7flow.CRRevision, error) {
	r, err := scanCRRevision(t.tx.QueryRowContext(t.ctx,
		`SELECT `+crRevColumns+` FROM cr_revisions WHERE id=?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return r, m7flow.ErrNotFound
	}
	return r, t.fail(err)
}

func (t *agentRuntimeTx) MaxCRRevisionNo(crID string) (int64, error) {
	var max sql.NullInt64
	if err := t.tx.QueryRowContext(t.ctx,
		`SELECT max(revision_no) FROM cr_revisions WHERE cr_id=?`, crID).Scan(&max); err != nil {
		return 0, t.fail(err)
	}
	return max.Int64, nil
}

// FindOpenCRRevision returns the newest draft/submitted revision of a CR.
func (t *agentRuntimeTx) FindOpenCRRevision(crID string) (m7flow.CRRevision, error) {
	r, err := scanCRRevision(t.tx.QueryRowContext(t.ctx,
		`SELECT `+crRevColumns+` FROM cr_revisions
		 WHERE cr_id=? AND status IN ('draft','submitted')
		 ORDER BY revision_no DESC LIMIT 1`, crID))
	if errors.Is(err, sql.ErrNoRows) {
		return r, m7flow.ErrNotFound
	}
	return r, t.fail(err)
}

func (t *agentRuntimeTx) ListCRRevisions(crID string) ([]m7flow.CRRevision, error) {
	rows, err := t.tx.QueryContext(t.ctx,
		`SELECT `+crRevColumns+` FROM cr_revisions WHERE cr_id=? ORDER BY revision_no`, crID)
	if err != nil {
		return nil, t.fail(err)
	}
	defer rows.Close()
	var out []m7flow.CRRevision
	for rows.Next() {
		r, err := scanCRRevision(rows)
		if err != nil {
			return nil, t.fail(err)
		}
		out = append(out, r)
	}
	return out, t.fail(rows.Err())
}

// UpdateCRRevisionStatus performs one legal status transition; the frozen
// columns are untouched (the M7-REV-002 trigger guards them at the DB layer).
func (t *agentRuntimeTx) UpdateCRRevisionStatus(id, from, to string) error {
	res, err := t.tx.ExecContext(t.ctx,
		`UPDATE cr_revisions SET status=? WHERE id=? AND status=?`, to, id, from)
	if err != nil {
		return t.fail(err)
	}
	if n, _ := res.RowsAffected(); n != 1 {
		return t.fail(sql.ErrNoRows)
	}
	return nil
}

const pkgColumns = `id,cr_revision_id,manifest_digest,blob_digest,signature,state,created_at,sealed_at`

func scanReleasePackage(s interface{ Scan(...any) error }) (m7flow.ReleasePackage, error) {
	var p m7flow.ReleasePackage
	var created string
	var sealed sql.NullString
	if err := s.Scan(&p.ID, &p.CRRevisionID, &p.ManifestDigest, &p.BlobDigest,
		&p.Signature, &p.State, &created, &sealed); err != nil {
		return p, err
	}
	var err error
	if p.CreatedAt, err = parseRFC(created); err != nil {
		return p, err
	}
	if sealed.Valid {
		t, err := parseRFC(sealed.String)
		if err != nil {
			return p, err
		}
		p.SealedAt = &t
	}
	return p, nil
}

func (t *agentRuntimeTx) PutReleasePackage(p m7flow.ReleasePackage) error {
	var sealed any
	if p.SealedAt != nil {
		sealed = rfc(*p.SealedAt)
	}
	_, err := t.tx.ExecContext(t.ctx, `INSERT INTO release_packages
		(id,cr_revision_id,manifest_digest,blob_digest,signature,state,created_at,sealed_at)
		VALUES(?,?,?,?,?,?,?,?)`,
		p.ID, p.CRRevisionID, p.ManifestDigest, p.BlobDigest, p.Signature,
		p.State, rfc(p.CreatedAt), sealed)
	return t.fail(err)
}

func (t *agentRuntimeTx) GetReleasePackage(id string) (m7flow.ReleasePackage, error) {
	p, err := scanReleasePackage(t.tx.QueryRowContext(t.ctx,
		`SELECT `+pkgColumns+` FROM release_packages WHERE id=?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return p, m7flow.ErrNotFound
	}
	return p, t.fail(err)
}

func (t *agentRuntimeTx) FindPackageByRevision(crRevisionID string) (m7flow.ReleasePackage, error) {
	p, err := scanReleasePackage(t.tx.QueryRowContext(t.ctx,
		`SELECT `+pkgColumns+` FROM release_packages
		 WHERE cr_revision_id=? ORDER BY created_at LIMIT 1`, crRevisionID))
	if errors.Is(err, sql.ErrNoRows) {
		return p, m7flow.ErrNotFound
	}
	return p, t.fail(err)
}

func (t *agentRuntimeTx) PutReleaseBlob(digest, content string, createdAt time.Time) error {
	_, err := t.tx.ExecContext(t.ctx,
		`INSERT OR IGNORE INTO release_blobs(digest,content,created_at) VALUES(?,?,?)`,
		digest, content, rfc(createdAt))
	return t.fail(err)
}

func (t *agentRuntimeTx) GetReleaseBlob(digest string) (string, error) {
	var content string
	err := t.tx.QueryRowContext(t.ctx,
		`SELECT content FROM release_blobs WHERE digest=?`, digest).Scan(&content)
	if errors.Is(err, sql.ErrNoRows) {
		return "", m7flow.ErrNotFound
	}
	return content, t.fail(err)
}

func (t *agentRuntimeTx) ListSubjectReviews(subjectType, subjectID string) ([]m7flow.Review, error) {
	rows, err := t.tx.QueryContext(t.ctx,
		`SELECT id,subject_type,subject_id,subject_version,verdict,reviewer_id,reason,created_at
		 FROM reviews WHERE subject_type=? AND subject_id=? ORDER BY created_at`,
		subjectType, subjectID)
	if err != nil {
		return nil, t.fail(err)
	}
	defer rows.Close()
	var out []m7flow.Review
	for rows.Next() {
		var r m7flow.Review
		var created string
		if err := rows.Scan(&r.ID, &r.SubjectType, &r.SubjectID, &r.SubjectVersion,
			&r.Verdict, &r.ReviewerID, &r.Reason, &created); err != nil {
			return nil, t.fail(err)
		}
		if r.CreatedAt, err = parseRFC(created); err != nil {
			return nil, t.fail(err)
		}
		out = append(out, r)
	}
	return out, t.fail(rows.Err())
}

// EvidenceReportDigest reads the report digest of one evidence row for the
// trace-edge endpoint (test_runs and scan_runs share the column name).
func (t *agentRuntimeTx) EvidenceReportDigest(kind, id string) (string, error) {
	table, ok := nodeTables[kind]
	if !ok || (kind != "test_run" && kind != "scan_run") {
		return "", t.fail(sql.ErrNoRows)
	}
	var digest string
	err := t.tx.QueryRowContext(t.ctx,
		`SELECT report_digest FROM `+table+` WHERE id=?`, id).Scan(&digest)
	if errors.Is(err, sql.ErrNoRows) {
		return "", m7flow.ErrNotFound
	}
	return digest, t.fail(err)
}