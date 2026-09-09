package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"

	"github.com/lunitide/lunitide/internal/domain/officestudio"
	"github.com/oklog/ulid/v2"
)

// A revision keeps the provenance of unchanged values. Editing a referenced
// value does not silently certify the replacement: retain the evidence and
// mark it stale until an explicit metric application supplies fresh evidence.
func inheritOfficeEvidence(ctx context.Context, tx *sql.Tx, v officestudio.Version, explicit []officestudio.EvidenceEdge) (bool, error) {
	if v.BaseVersionID == "" {
		return false, nil
	}
	var baseIndex, baseSHA, baseQuality string
	if err := tx.QueryRowContext(ctx, `SELECT m.index_json,a.sha256,m.quality FROM office_version_metadata m JOIN artifact_versions a ON a.id=m.version_id WHERE m.version_id=?`, v.BaseVersionID).Scan(&baseIndex, &baseSHA, &baseQuality); err != nil {
		return false, err
	}
	baseNodes, nextNodes := officeNodeDigests([]byte(baseIndex)), officeNodeDigests(v.Index)
	overrides := map[string]bool{}
	for _, e := range explicit {
		overrides[e.TargetNode] = true
	}
	rows, err := tx.QueryContext(ctx, `SELECT e.source_version_id,e.source_node,e.target_node,e.metric_json,h.latest_version_id,m.quality FROM office_evidence_edges e JOIN artifact_versions a ON a.id=e.source_version_id JOIN office_artifact_heads h ON h.artifact_id=a.artifact_id JOIN office_version_metadata m ON m.version_id=a.id WHERE e.target_version_id=?`, v.BaseVersionID)
	if err != nil {
		return false, err
	}
	type source struct{ version, node, target, metric, latest, quality string }
	var sources []source
	for rows.Next() {
		var e source
		if err = rows.Scan(&e.version, &e.node, &e.target, &e.metric, &e.latest, &e.quality); err != nil {
			rows.Close()
			return false, err
		}
		if !overrides[e.target] {
			sources = append(sources, e)
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return false, err
	}
	stale := false
	for _, e := range sources {
		var bound struct {
			NodeDigest string `json:"_officeTargetNodeDigest"`
			SHA256     string `json:"_officeTargetSHA256"`
		}
		_ = json.Unmarshal([]byte(e.metric), &bound)
		if e.version != e.latest || e.quality == "stale" || e.quality == "blocked" {
			stale = true
		}
		if e.target == "" {
			if officestudio.ValidDigest(bound.SHA256) {
				if bound.SHA256 != v.SHA256 {
					stale = true
				}
			} else if baseQuality == "stale" || baseSHA != v.SHA256 {
				stale = true
			}
		} else {
			if officestudio.ValidDigest(bound.NodeDigest) {
				if bound.NodeDigest != nextNodes[e.target] {
					stale = true
				}
			} else if baseQuality == "stale" || baseNodes[e.target] == "" || baseNodes[e.target] != nextNodes[e.target] {
				// Old edges have no persistent target binding. Once stale they
				// stay stale until explicitly replaced by fresh source evidence.
				stale = true
			}
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO office_evidence_edges(id,task_id,source_version_id,target_version_id,source_node,target_node,metric_json,created_at) VALUES(?,?,?,?,?,?,?,?)`, ulid.Make().String(), v.TaskID, e.version, v.ID, e.node, e.target, e.metric, officeRFC(v.CreatedAt)); err != nil {
			return false, err
		}
	}
	if len(sources) > 0 {
		if err = appendOfficeEvent(ctx, tx, v.TaskID, v.ID, "evidence.inherited", map[string]any{"baseVersionId": v.BaseVersionID, "count": len(sources), "stale": stale}, v.CreatedAt); err != nil {
			return false, err
		}
	}
	if stale {
		_, err = tx.ExecContext(ctx, `UPDATE office_version_metadata SET quality='stale' WHERE version_id=?`, v.ID)
	}
	return stale, err
}

func officeNodeDigests(raw []byte) map[string]string {
	var index struct{ Nodes []struct{ ID, Digest string } }
	_ = json.Unmarshal(raw, &index)
	out := map[string]string{}
	for _, n := range index.Nodes {
		out[n.ID] = n.Digest
	}
	return out
}
