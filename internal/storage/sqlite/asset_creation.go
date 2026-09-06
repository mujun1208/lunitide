package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/lunitide/lunitide/internal/domain/asset"
	"github.com/lunitide/lunitide/internal/providerapp"
	"github.com/oklog/ulid/v2"
)

func (t *txAdapter) templateCreationReplay(ctx context.Context, key, digest string) (asset.AssetTemplate, bool, error) {
	var tpl asset.AssetTemplate
	var savedDigest, response, expiry string
	err := t.q.QueryRowContext(ctx, `SELECT request_digest,response_json,expires_at FROM asset_template_creations WHERE request_key=?`, key).Scan(&savedDigest, &response, &expiry)
	if err == sql.ErrNoRows {
		return tpl, false, nil
	}
	if err != nil {
		return tpl, false, err
	}
	if savedDigest != digest {
		return tpl, true, asset.ErrIdempotencyConflict
	}
	expires, err := time.Parse(time.RFC3339Nano, expiry)
	if err != nil {
		return tpl, true, err
	}
	// Expiration never frees this key for another creation. Keep its tombstone
	// so an old retry cannot silently recreate a deleted template or its file.
	if !time.Now().Before(expires) {
		return tpl, true, asset.ErrCreationExpired
	}
	err = json.Unmarshal([]byte(response), &tpl)
	return tpl, true, err
}
func (s *Store) ReplayAssetTemplateCreation(ctx context.Context, key, digest string) (asset.AssetTemplate, bool, error) {
	var tpl asset.AssetTemplate
	var found bool
	err := s.do(ctx, func(t *txAdapter) error {
		var err error
		tpl, found, err = t.templateCreationReplay(ctx, key, digest)
		return err
	})
	return tpl, found, err
}

func (s *Store) CreateAssetTemplateIdempotent(ctx context.Context, key, digest string, tpl asset.AssetTemplate) (asset.AssetTemplate, error) {
	if !providerapp.ValidIdempotencyKey(key) {
		return asset.AssetTemplate{}, providerapp.ErrIdempotencyKeyRequired
	}
	var out asset.AssetTemplate
	err := s.do(ctx, func(t *txAdapter) error {
		replay, found, err := t.templateCreationReplay(ctx, key, digest)
		if err != nil {
			return err
		}
		if found {
			out = replay
			return nil
		}
		now := time.Now().UTC()
		tpl.ID = ulid.Make().String()
		tpl.CreatedAt = now
		tpl.UpdatedAt = now
		tpl.Status = asset.StatusDraft
		tpl.Version = 1
		var next int
		if err = t.q.QueryRowContext(ctx, `SELECT COALESCE(MAX(CAST(substr(template_code,4) AS INTEGER)),0)+1 FROM asset_templates`).Scan(&next); err != nil {
			return err
		}
		tpl.TemplateCode = fmt.Sprintf("TPL%05d", next)
		_, err = t.q.ExecContext(ctx, `INSERT INTO asset_templates(id,template_code,name,template_type,document_type,description,client,mime_type,file_name,file_path,status,created_at,updated_at,version,org_id) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, tpl.ID, tpl.TemplateCode, tpl.Name, tpl.TemplateType, tpl.DocumentType, tpl.Description, tpl.Client, tpl.MimeType, tpl.FileName, tpl.FilePath, tpl.Status, formatTime(now), formatTime(now), tpl.Version, nullableProjectID(tpl.OrgID))
		if err != nil {
			return err
		}
		response, err := json.Marshal(tpl)
		if err != nil {
			return err
		}
		_, err = t.q.ExecContext(ctx, `INSERT INTO asset_template_creations(request_key,request_digest,response_json,created_at,expires_at) VALUES(?,?,?,?,?)`, key, digest, string(response), formatTime(now), formatTime(now.Add(24*time.Hour)))
		if err != nil {
			return err
		}
		meta, _ := json.Marshal(map[string]any{"templateCode": tpl.TemplateCode, "name": tpl.Name, "requestDigest": digest})
		if err = t.PutAudit(ctx, providerapp.Audit{ID: ulid.Make().String(), Action: "asset_template.created", AggregateID: tpl.ID, Actor: "engine", Metadata: meta, CreatedAt: now}); err != nil {
			return err
		}
		out = tpl
		return nil
	})
	return out, err
}
