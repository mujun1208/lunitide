package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/lunitide/lunitide/internal/domain/officestudio"
	"github.com/oklog/ulid/v2"
)

func (s *Store) FindOfficeDeliveryDecision(ctx context.Context, versionID, sourceSHA, policyRevision, evidenceDigest string) (officestudio.FormalDecision, bool, error) {
	var raw string
	err := s.db.QueryRowContext(ctx, `SELECT decision_json FROM office_delivery_decisions WHERE version_id=? AND source_sha256=? AND policy_revision=? AND evidence_digest=? ORDER BY created_at DESC,id DESC LIMIT 1`, versionID, sourceSHA, policyRevision, evidenceDigest).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return officestudio.FormalDecision{}, false, nil
	}
	if err != nil {
		return officestudio.FormalDecision{}, false, err
	}
	var dec officestudio.FormalDecision
	if err = json.Unmarshal([]byte(raw), &dec); err != nil {
		return officestudio.FormalDecision{}, false, err
	}
	return dec, true, nil
}

func (s *Store) SaveOfficeDeliveryDecision(ctx context.Context, dec officestudio.FormalDecision, taskID, evidenceDigest string) (officestudio.FormalDecision, error) {
	if dec.DecisionID == "" {
		dec.DecisionID = ulid.Make().String()
	}
	if !officestudio.ValidID(dec.DecisionID) || !officestudio.ValidID(taskID) || !officestudio.ValidID(dec.VersionID) || !officestudio.ValidDigest(dec.SourceSHA256) || !officestudio.ValidDigest(evidenceDigest) || dec.PolicyRevision == "" {
		return dec, officestudio.ErrInvalid
	}
	body, err := json.Marshal(dec)
	if err != nil || !officestudio.ValidJSON(body, 1<<20) {
		return dec, officestudio.ErrInvalid
	}
	_, err = s.db.ExecContext(ctx, `INSERT INTO office_delivery_decisions(id,owner_scope,task_id,version_id,source_sha256,policy_revision,evidence_digest,decision_json,created_at) VALUES(?,?,?,?,?,?,?,?,?)`,
		dec.DecisionID, officestudio.Scope(ctx), taskID, dec.VersionID, dec.SourceSHA256, dec.PolicyRevision, evidenceDigest, string(body), officeRFC(time.Now().UTC()))
	if err != nil {
		return dec, err
	}
	return dec, nil
}
