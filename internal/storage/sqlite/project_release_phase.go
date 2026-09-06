package sqlite

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/lunitide/lunitide/internal/domain/m7flow"
	"github.com/lunitide/lunitide/internal/domain/project"
	"github.com/lunitide/lunitide/internal/m7app"
)

func (s *Store) SetProjectPublicationRoot(root string) { s.projectPublicationRoot = root }

func (t *txAdapter) releasePhaseEvidence(ctx context.Context, p project.Project) (map[string]any, error) {
	if t.s.projectPublicationRoot == "" {
		return nil, phaseGateError("release publication verification unavailable")
	}
	// Every preceding phase remains verified at completion time. Later metadata
	// or source-file edits cannot reuse an older completed-stage flag.
	for phase := 1; phase < project.ReleasePhase(p.Type); phase++ {
		for _, key := range project.RequiredPhaseDocuments(p.Type, phase) {
			d, err := scanProjectDeliverable(t.q.QueryRowContext(ctx, deliverableSelect+` WHERE project_id=? AND phase=? AND document_type=?`, p.ID, phase, key))
			if err != nil {
				return nil, phaseGateError("prior phase evidence missing")
			}
			if d.Status != "approved" && d.Status != "immutable" {
				return nil, phaseGateError("prior phase evidence is not approved")
			}
			if _, err = t.phaseEvidence(ctx, d); err != nil {
				return nil, err
			}
		}
	}
	var promotionID, packageID, blob, blobDigest, manifestJSON, receipt string
	err := t.q.QueryRowContext(ctx, `SELECT x.id,k.id,b.content,k.blob_digest,r.manifest_json,d.receipt_json
 FROM promotions x JOIN deployments d ON d.promotion_id=x.id JOIN release_packages k ON k.id=x.package_id
 JOIN cr_revisions r ON r.id=k.cr_revision_id JOIN release_blobs b ON b.digest=k.blob_digest
 WHERE x.state='succeeded' AND x.to_env='stage' AND d.state='succeeded' AND k.state='sealed'
 AND json_extract(r.manifest_json,'$.projectId')=? ORDER BY x.created_at DESC,x.id DESC LIMIT 1`, p.ID).Scan(&promotionID, &packageID, &blob, &blobDigest, &manifestJSON, &receipt)
	if err != nil {
		return nil, phaseGateError("verified stage publication required")
	}
	if m7flow.SHA256Hex([]byte(blob)) != blobDigest {
		return nil, phaseGateError("release package content changed")
	}
	var manifest map[string]any
	if err = json.Unmarshal([]byte(manifestJSON), &manifest); err != nil {
		return nil, err
	}
	content, err := t.s.projectReleaseContent(ctx, t.q, "CR-"+p.ProjectCode, manifest)
	if err != nil {
		return nil, err
	}
	if err = matchReleaseContent(content, manifest); err != nil {
		return nil, phaseGateError("release sources changed since revision")
	}
	var published m7app.LocalPublicationReceipt
	if err = json.Unmarshal([]byte(receipt), &published); err != nil {
		return nil, err
	}
	if published.ProjectID != p.ID || published.BlobDigest != blobDigest || published.PromotionID != promotionID {
		return nil, phaseGateError("release receipt binding mismatch")
	}
	if err = m7app.VerifyLocalPublication(ctx, t.s.projectPublicationRoot, receipt); err != nil {
		return nil, phaseGateError(fmt.Sprintf("local published files failed verification: %v", err))
	}
	return map[string]any{"promotionId": promotionID, "packageId": packageID, "blobDigest": blobDigest, "kind": "local-artifact-publication-v1", "externalDeployment": false}, nil
}
