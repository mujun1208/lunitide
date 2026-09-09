package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"

	"github.com/lunitide/lunitide/internal/domain/officestudio"
	"github.com/oklog/ulid/v2"
)

// The target is a new immutable version, so it cannot already have outgoing
// edges and cannot close a dependency cycle. Source heads are checked in the
// same transaction that publishes the target and its evidence.
func publishOfficeEvidence(ctx context.Context, tx *sql.Tx, v officestudio.Version, edges []officestudio.EvidenceEdge) error {
	if len(edges) > 128 {
		return officestudio.ErrInvalid
	}
	for _, e := range edges {
		if !officestudio.ValidID(e.SourceVersionID) || e.SourceVersionID == v.ID || len(e.SourceNode) > 512 || len(e.TargetNode) > 512 || !officestudio.ValidJSON(e.Metric, 65536) {
			return officestudio.ErrInvalid
		}
		var taskID, headID, quality string
		if err := tx.QueryRowContext(ctx, `SELECT b.task_id,h.latest_version_id,m.quality FROM artifact_versions a JOIN office_task_artifacts b ON b.artifact_id=a.artifact_id JOIN office_artifact_heads h ON h.artifact_id=a.artifact_id JOIN office_version_metadata m ON m.version_id=a.id WHERE a.id=?`, e.SourceVersionID).Scan(&taskID, &headID, &quality); err != nil {
			return officeError(err)
		}
		if taskID != v.TaskID {
			return officestudio.ErrScope
		}
		if headID != e.SourceVersionID || quality == "stale" || quality == "blocked" {
			return officestudio.ErrConflict
		}
		metric, err := bindOfficeEvidenceTarget(v, e)
		if err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO office_evidence_edges(id,task_id,source_version_id,target_version_id,source_node,target_node,metric_json,created_at) VALUES(?,?,?,?,?,?,?,?)`, ulid.Make().String(), v.TaskID, e.SourceVersionID, v.ID, e.SourceNode, e.TargetNode, string(metric), officeRFC(v.CreatedAt))
		if err != nil {
			return err
		}
	}
	return nil
}

// This server-computed binding remains unchanged when evidence is inherited.
// Comparing only adjacent revisions would let an incorrect value become
// "fresh" after the following unrelated edit leaves that value unchanged.
func bindOfficeEvidenceTarget(v officestudio.Version, e officestudio.EvidenceEdge) (json.RawMessage, error) {
	fields := map[string]json.RawMessage{}
	if json.Unmarshal(e.Metric, &fields) != nil || fields == nil {
		fields = map[string]json.RawMessage{"metric": e.Metric}
	}
	delete(fields, "_officeTargetNodeDigest")
	delete(fields, "_officeTargetSHA256")
	if e.TargetNode == "" {
		fields["_officeTargetSHA256"], _ = json.Marshal(v.SHA256)
	} else {
		digest := officeNodeDigests(v.Index)[e.TargetNode]
		if !officestudio.ValidDigest(digest) {
			return nil, officestudio.ErrInvalid
		}
		fields["_officeTargetNodeDigest"], _ = json.Marshal(digest)
	}
	raw, err := json.Marshal(fields)
	if err != nil {
		return nil, err
	}
	if !officestudio.ValidJSON(raw, 65536) {
		return nil, officestudio.ErrInvalid
	}
	return raw, nil
}
