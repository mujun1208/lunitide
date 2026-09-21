package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"time"

	"github.com/lunitide/lunitide/internal/producthub"
)

func (s *Store) ProductHubLoadAuth(ctx context.Context) (string, error) {
	if s == nil || s.db == nil {
		return "", nil
	}
	var hash string
	err := s.db.QueryRowContext(ctx, `SELECT password_hash FROM product_hub_auth WHERE singleton_id='default'`).Scan(&hash)
	if err == sql.ErrNoRows {
		return "", nil
	}
	return hash, err
}

func (s *Store) ProductHubSaveAuth(ctx context.Context, hash string) error {
	if s == nil || s.db == nil {
		return sql.ErrConnDone
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO product_hub_auth(singleton_id,username,password_hash,updated_at)
VALUES('default','mujun',?,?)
ON CONFLICT(singleton_id) DO UPDATE SET password_hash=excluded.password_hash, updated_at=excluded.updated_at`, hash, time.Now().UTC().Format(time.RFC3339))
	return err
}

func (s *Store) ProductHubLoadLatest(ctx context.Context) (*producthub.Edition, error) {
	if s == nil || s.db == nil {
		return nil, nil
	}
	var snapshotID, digest, features, graph, findings, changes, md, html, generated string
	var cards, health int
	err := s.db.QueryRowContext(ctx, `SELECT snapshot_id,digest,features_json,graph_json,findings_json,changes_json,report_markdown,report_html,card_count,health_score,generated_at
FROM product_snapshots WHERE state='verified' ORDER BY generated_at DESC LIMIT 1`).Scan(
		&snapshotID, &digest, &features, &graph, &findings, &changes, &md, &html, &cards, &health, &generated)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	ed := producthub.Edition{
		EditionID: snapshotID, Digest: digest, ReportMarkdown: md, ReportHTML: html,
		CardCount: cards, HealthScore: health, GeneratedAt: generated,
	}
	_ = json.Unmarshal([]byte(features), &ed.Features)
	_ = json.Unmarshal([]byte(graph), &ed.Graph)
	_ = json.Unmarshal([]byte(findings), &ed.Findings)
	_ = json.Unmarshal([]byte(changes), &ed.Changes)
	return &ed, nil
}

func (s *Store) ProductHubSaveEdition(ctx context.Context, ed producthub.Edition) error {
	if s == nil || s.db == nil {
		return sql.ErrConnDone
	}
	feat, _ := json.Marshal(ed.Features)
	graph, _ := json.Marshal(ed.Graph)
	findings, _ := json.Marshal(ed.Findings)
	changes, _ := json.Marshal(ed.Changes)
	_, _ = s.db.ExecContext(ctx, `UPDATE product_snapshots SET state='retired' WHERE state='verified' AND snapshot_id<>?`, ed.EditionID)
	_, err := s.db.ExecContext(ctx, `INSERT INTO product_snapshots(
snapshot_id,state,trigger,digest,features_json,graph_json,findings_json,changes_json,report_markdown,report_html,card_count,health_score,generated_at)
VALUES(?,'verified','manual',?,?,?,?,?,?,?,?,?,?)
ON CONFLICT(snapshot_id) DO UPDATE SET
state='verified', digest=excluded.digest, features_json=excluded.features_json, graph_json=excluded.graph_json,
findings_json=excluded.findings_json, changes_json=excluded.changes_json, report_markdown=excluded.report_markdown,
report_html=excluded.report_html, card_count=excluded.card_count, health_score=excluded.health_score,
generated_at=excluded.generated_at`,
		ed.EditionID, ed.Digest, string(feat), string(graph), string(findings), string(changes),
		ed.ReportMarkdown, ed.ReportHTML, ed.CardCount, ed.HealthScore, ed.GeneratedAt)
	return err
}

func (s *Store) ProductHubLoadTags(ctx context.Context) ([]producthub.NodeTag, error) {
	if s == nil || s.db == nil {
		return nil, nil
	}
	rows, err := s.db.QueryContext(ctx, `SELECT stable_key,vocab,value,assigned_by FROM product_node_tags`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []producthub.NodeTag
	for rows.Next() {
		var t producthub.NodeTag
		if err := rows.Scan(&t.StableKey, &t.Vocab, &t.Value, &t.AssignedBy); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

func (s *Store) ProductHubSaveTag(ctx context.Context, tag producthub.NodeTag) error {
	if s == nil || s.db == nil {
		return sql.ErrConnDone
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO product_node_tags(stable_key,vocab,value,assigned_by)
VALUES(?,?,?,?)
ON CONFLICT(stable_key,vocab,value) DO UPDATE SET assigned_by=excluded.assigned_by`,
		tag.StableKey, tag.Vocab, tag.Value, tag.AssignedBy)
	return err
}

func (s *Store) ProductHubLoadEnrichments(ctx context.Context) ([]producthub.Enrichment, error) {
	if s == nil || s.db == nil {
		return nil, nil
	}
	rows, err := s.db.QueryContext(ctx, `SELECT stable_key,summary,payload_json FROM product_enrichments`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []producthub.Enrichment
	for rows.Next() {
		var e producthub.Enrichment
		var payload string
		if err := rows.Scan(&e.StableKey, &e.Summary, &payload); err != nil {
			return nil, err
		}
		key, summary := e.StableKey, e.Summary
		if payload != "" && payload != "{}" {
			_ = json.Unmarshal([]byte(payload), &e)
		}
		e.StableKey = key
		if e.Summary == "" {
			e.Summary = summary
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

func (s *Store) ProductHubSaveEnrichment(ctx context.Context, en producthub.Enrichment) error {
	if s == nil || s.db == nil {
		return sql.ErrConnDone
	}
	payload, _ := json.Marshal(en)
	_, err := s.db.ExecContext(ctx, `INSERT INTO product_enrichments(stable_key,summary,description,marked_ai,payload_json)
VALUES(?,?, '', 0, ?)
ON CONFLICT(stable_key) DO UPDATE SET summary=excluded.summary, payload_json=excluded.payload_json`,
		en.StableKey, en.Summary, string(payload))
	return err
}

func (s *Store) ProductHubLoadApplies(ctx context.Context) ([]producthub.ApplyLog, error) {
	if s == nil || s.db == nil {
		return nil, nil
	}
	rows, err := s.db.QueryContext(ctx, `SELECT apply_id,error_code,stable_key,plan,skill_id,skill_name,skill_output,status,created_at
FROM product_apply_log ORDER BY created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []producthub.ApplyLog
	for rows.Next() {
		var rec producthub.ApplyLog
		if err := rows.Scan(&rec.ID, &rec.ErrorCode, &rec.StableKey, &rec.Plan, &rec.SkillID, &rec.SkillName, &rec.SkillOutput, &rec.Status, &rec.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, rec)
	}
	return out, rows.Err()
}

func (s *Store) ProductHubSaveApply(ctx context.Context, rec producthub.ApplyLog) error {
	if s == nil || s.db == nil {
		return sql.ErrConnDone
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO product_apply_log(apply_id,error_code,stable_key,plan,skill_id,skill_name,skill_output,status,created_at)
VALUES(?,?,?,?,?,?,?,?,?)`,
		rec.ID, rec.ErrorCode, rec.StableKey, rec.Plan, rec.SkillID, rec.SkillName, rec.SkillOutput, rec.Status, rec.CreatedAt)
	return err
}
