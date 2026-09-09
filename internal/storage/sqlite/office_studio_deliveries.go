package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/lunitide/lunitide/internal/domain/officestudio"
	"github.com/oklog/ulid/v2"
)

var _ officestudio.DeliveryStore = (*Store)(nil)

func (s *Store) CreateOfficeMetric(ctx context.Context, m officestudio.Metric, key string) (officestudio.Metric, error) {
	if !officeKey(key) || m.Name == "" || len(m.Name) > 256 || len(m.RawValue) > 16000 || len(m.DisplayValue) > 16000 || !officestudio.ValidID(m.TaskID) || !officestudio.ValidID(m.SourceVersionID) || !officestudio.ValidDigest(m.SourceSHA256) || !officestudio.ValidDigest(m.SourceNodeDigest) || m.SourceNodeID == "" || len(m.SourceNodeID) > 512 {
		return m, officestudio.ErrInvalid
	}
	if m.ID == "" {
		m.ID = ulid.Make().String()
	}
	if !officestudio.ValidID(m.ID) {
		return m, officestudio.ErrInvalid
	}
	if m.CreatedAt.IsZero() {
		m.CreatedAt = time.Now().UTC()
	}
	identity := m
	identity.ID = ""
	identity.CreatedAt = time.Time{}
	digest := officeDigest(identity)
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return m, err
	}
	defer tx.Rollback()
	if err = officeAuthorize(ctx, tx, "office-task", m.TaskID); err != nil {
		return m, err
	}
	var old, oldDigest string
	err = tx.QueryRowContext(ctx, `SELECT metric_json,request_digest FROM office_metrics WHERE task_id=? AND idempotency_key=?`, m.TaskID, key).Scan(&old, &oldDigest)
	if err == nil {
		if oldDigest != digest {
			return m, officestudio.ErrConflict
		}
		err = json.Unmarshal([]byte(old), &m)
		return m, err
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return m, err
	}
	v, err := scanOfficeVersion(tx.QueryRowContext(ctx, `SELECT `+officeVersionColumns+officeVersionFrom+` WHERE v.id=?`, m.SourceVersionID))
	if err != nil {
		return m, err
	}
	if v.TaskID != m.TaskID {
		return m, officestudio.ErrScope
	}
	if v.SHA256 != m.SourceSHA256 || v.Quality == "stale" || v.Quality == "blocked" {
		return m, officestudio.ErrConflict
	}
	var index struct {
		Nodes []struct{ ID, Text, Digest, Kind string }
	}
	if err = json.Unmarshal(v.Index, &index); err != nil {
		return m, err
	}
	matched := false
	for _, n := range index.Nodes {
		if n.ID == m.SourceNodeID {
			kind := strings.TrimPrefix(n.Kind, "cell:")
			matched = n.Digest == m.SourceNodeDigest && n.Text == m.RawValue && kind == m.ValueType && kind != "formula" && kind != "error" && kind != "invalid"
			break
		}
	}
	if !matched {
		return m, officestudio.ErrConflict
	}
	var count int
	if err = tx.QueryRowContext(ctx, `SELECT count(*) FROM office_metrics WHERE task_id=?`, m.TaskID).Scan(&count); err != nil {
		return m, err
	}
	if count >= 1000 {
		return m, officestudio.ErrInvalid
	}
	b, err := json.Marshal(m)
	if err != nil {
		return m, err
	}
	if len(b) > 65536 {
		return m, officestudio.ErrInvalid
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO office_metrics(id,task_id,source_version_id,metric_json,idempotency_key,request_digest,created_at) VALUES(?,?,?,?,?,?,?)`, m.ID, m.TaskID, m.SourceVersionID, string(b), key, digest, officeRFC(m.CreatedAt))
	if err != nil {
		return m, err
	}
	if err = appendOfficeEvent(ctx, tx, m.TaskID, m.SourceVersionID, "metric.captured", map[string]string{"metricId": m.ID, "sourceNodeId": m.SourceNodeID}, m.CreatedAt); err != nil {
		return m, err
	}
	return m, tx.Commit()
}

func (s *Store) GetOfficeMetric(ctx context.Context, id string) (officestudio.Metric, error) {
	var m officestudio.Metric
	var raw string
	if err := s.db.QueryRowContext(ctx, `SELECT metric_json FROM office_metrics WHERE id=?`, id).Scan(&raw); err != nil {
		return m, officeError(err)
	}
	if err := json.Unmarshal([]byte(raw), &m); err != nil {
		return m, err
	}
	if err := officeAuthorize(ctx, s.db, "office-task", m.TaskID); err != nil {
		return officestudio.Metric{}, err
	}
	return m, nil
}

func (s *Store) ListOfficeMetrics(ctx context.Context, taskID string) ([]officestudio.Metric, error) {
	if err := officeAuthorize(ctx, s.db, "office-task", taskID); err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx, `SELECT metric_json FROM office_metrics WHERE task_id=? ORDER BY created_at DESC,id LIMIT 1000`, taskID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []officestudio.Metric{}
	for rows.Next() {
		var raw string
		var m officestudio.Metric
		if err = rows.Scan(&raw); err != nil {
			return nil, err
		}
		if err = json.Unmarshal([]byte(raw), &m); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

func (s *Store) CreateOfficeBundle(ctx context.Context, b officestudio.Bundle, key string) (officestudio.Bundle, error) {
	if !officeKey(key) || !officestudio.ValidID(b.TaskID) || strings.TrimSpace(b.Title) == "" || len(b.Title) > 256 || len(b.Files) == 0 || len(b.Files) > 32 {
		return b, officestudio.ErrInvalid
	}
	if b.ID == "" {
		b.ID = ulid.Make().String()
	}
	if !officestudio.ValidID(b.ID) {
		return b, officestudio.ErrInvalid
	}
	if b.CreatedAt.IsZero() {
		b.CreatedAt = time.Now().UTC()
	}
	b.SchemaVersion = 1
	identity := b
	identity.ID = ""
	identity.CreatedAt = time.Time{}
	identity.Files = append([]officestudio.BundleFile{}, b.Files...)
	for i := range identity.Files {
		identity.Files[i].Quality = ""
		identity.Files[i].Accepted = false
	}
	digest := officeDigest(identity)
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return b, err
	}
	defer tx.Rollback()
	if err = officeAuthorize(ctx, tx, "office-task", b.TaskID); err != nil {
		return b, err
	}
	var old, oldDigest string
	err = tx.QueryRowContext(ctx, `SELECT manifest_json,request_digest FROM office_bundles WHERE task_id=? AND idempotency_key=?`, b.TaskID, key).Scan(&old, &oldDigest)
	if err == nil {
		if oldDigest != digest {
			return b, officestudio.ErrConflict
		}
		err = json.Unmarshal([]byte(old), &b)
		return b, err
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return b, err
	}
	names, artifacts := map[string]bool{}, map[string]bool{}
	for i, f := range b.Files {
		if f.Name == "" || f.Name == "." || f.Name == ".." || len(f.Name) > 512 || strings.ContainsAny(f.Name, "/\\\x00\r\n") || names[strings.ToLower(f.Name)] || artifacts[f.ArtifactID] {
			return b, officestudio.ErrInvalid
		}
		names[strings.ToLower(f.Name)] = true
		artifacts[f.ArtifactID] = true
		v, e := scanOfficeVersion(tx.QueryRowContext(ctx, `SELECT `+officeVersionColumns+officeVersionFrom+` WHERE v.id=?`, f.VersionID))
		if e != nil {
			return b, e
		}
		if v.TaskID != b.TaskID {
			return b, officestudio.ErrScope
		}
		if v.ArtifactID != f.ArtifactID || v.SHA256 != f.SHA256 || v.Size != f.Size || v.Kind != f.Kind {
			return b, officestudio.ErrConflict
		}
		b.Files[i].Quality = v.Quality
		var accepted sql.NullString
		if e = tx.QueryRowContext(ctx, `SELECT accepted_version_id FROM office_artifact_heads WHERE artifact_id=?`, v.ArtifactID).Scan(&accepted); e != nil {
			return b, e
		}
		b.Files[i].Accepted = accepted.String == v.ID
	}
	var count int
	if err = tx.QueryRowContext(ctx, `SELECT count(*) FROM office_bundles WHERE task_id=?`, b.TaskID).Scan(&count); err != nil {
		return b, err
	}
	if count >= 200 {
		return b, officestudio.ErrInvalid
	}
	raw, err := json.Marshal(b)
	if err != nil {
		return b, err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO office_bundles(id,task_id,manifest_json,idempotency_key,request_digest,created_at) VALUES(?,?,?,?,?,?)`, b.ID, b.TaskID, string(raw), key, digest, officeRFC(b.CreatedAt))
	if err != nil {
		return b, err
	}
	if err = appendOfficeEvent(ctx, tx, b.TaskID, "", "bundle.created", map[string]any{"bundleId": b.ID, "files": len(b.Files)}, b.CreatedAt); err != nil {
		return b, err
	}
	return b, tx.Commit()
}

func (s *Store) GetOfficeBundle(ctx context.Context, id string) (officestudio.Bundle, error) {
	var b officestudio.Bundle
	var raw string
	if err := s.db.QueryRowContext(ctx, `SELECT manifest_json FROM office_bundles WHERE id=?`, id).Scan(&raw); err != nil {
		return b, officeError(err)
	}
	if err := json.Unmarshal([]byte(raw), &b); err != nil {
		return b, err
	}
	if err := officeAuthorize(ctx, s.db, "office-task", b.TaskID); err != nil {
		return officestudio.Bundle{}, err
	}
	return b, nil
}

func (s *Store) ListOfficeBundles(ctx context.Context, taskID string) ([]officestudio.Bundle, error) {
	if err := officeAuthorize(ctx, s.db, "office-task", taskID); err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx, `SELECT manifest_json FROM office_bundles WHERE task_id=? ORDER BY created_at DESC,id LIMIT 200`, taskID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []officestudio.Bundle{}
	for rows.Next() {
		var raw string
		var b officestudio.Bundle
		if err = rows.Scan(&raw); err != nil {
			return nil, err
		}
		if err = json.Unmarshal([]byte(raw), &b); err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	return out, rows.Err()
}
