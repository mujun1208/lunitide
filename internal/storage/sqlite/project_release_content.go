package sqlite

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/lunitide/lunitide/internal/domain/deliverable"
	"github.com/lunitide/lunitide/internal/domain/m7flow"
	"github.com/lunitide/lunitide/internal/domain/project"
	"github.com/lunitide/lunitide/internal/m7app"
	"github.com/lunitide/lunitide/internal/workspace"
)

type projectReleaseSource struct {
	DeliverableID string `json:"deliverableId"`
	Version       int64  `json:"version"`
	Phase         int    `json:"phase"`
	DocumentType  string `json:"documentType"`
}
type projectReleaseContent struct {
	members   []m7flow.PackageMember
	sources   []projectReleaseSource
	bytes     map[string][]byte
	inventory []byte
}

func (s *Store) projectReleaseContent(ctx context.Context, q phaseEvidenceQuery, crID string, manifest map[string]any) (projectReleaseContent, error) {
	var out projectReleaseContent
	projectID, ok := manifest["projectId"].(string)
	if !ok || projectID == "" {
		return out, fmt.Errorf("%w: projectId required", m7app.ErrPackageInvalid)
	}
	var code string
	var typ project.Type
	if err := q.QueryRowContext(ctx, `SELECT project_code,project_type FROM projects WHERE id=? AND status NOT IN ('archived','closed')`, projectID).Scan(&code, &typ); err != nil {
		return out, fmt.Errorf("%w: project unavailable", m7app.ErrEvidenceMissing)
	}
	if crID != "CR-"+code {
		return out, fmt.Errorf("%w: CR belongs to another project", m7app.ErrPackageInvalid)
	}
	dev, api, db := 5, 4, 3
	if typ == project.TypeOperations {
		dev, api, db = 4, 3, 2
	}
	out.bytes = map[string][]byte{}
	for _, entry := range []struct {
		phase int
		key   string
	}{{db, "db_design"}, {api, "interface_list"}, {dev, "dev_checklist"}} {
		d, err := scanProjectDeliverable(q.QueryRowContext(ctx, deliverableSelect+` WHERE project_id=? AND phase=? AND document_type=?`, projectID, entry.phase, entry.key))
		if err != nil {
			return out, fmt.Errorf("%w: %s", m7app.ErrEvidenceMissing, entry.key)
		}
		if d.Status != deliverable.StatusApproved && d.Status != deliverable.StatusImmutable {
			return out, fmt.Errorf("%w: %s not approved", m7app.ErrEvidenceMissing, entry.key)
		}
		receipt, err := s.verifyProjectEvidence(ctx, q, d, true)
		if err != nil {
			return out, fmt.Errorf("%w: %s source unavailable or changed", m7app.ErrEvidenceMissing, entry.key)
		}
		var path, fileName string
		files := s.projectEvidenceFiles.attachments
		if d.AttachmentID != "" {
			err = q.QueryRowContext(ctx, `SELECT file_path,file_name FROM project_attachments WHERE id=?`, d.AttachmentID).Scan(&path, &fileName)
		} else {
			files = s.projectEvidenceFiles.templates
			err = q.QueryRowContext(ctx, `SELECT file_path,file_name FROM asset_templates WHERE id=?`, d.TemplateID).Scan(&path, &fileName)
		}
		if err != nil {
			return out, err
		}
		data, err := files.ReadFile(ctx, path)
		if err != nil {
			return out, err
		}
		sha := m7flow.SHA256Hex(data)
		if sha != receipt["digest"] {
			return out, m7app.ErrDigestMismatch
		}
		ext := strings.ToLower(filepath.Ext(fileName))
		if ext == "" {
			ext = ".bin"
		}
		name := entry.key + ext
		if workspace.ValidateRelPath(name) != nil {
			return out, m7app.ErrPackageInvalid
		}
		out.members = append(out.members, m7flow.PackageMember{Name: name, Size: int64(len(data)), SHA256: sha, Algorithm: "sha256"})
		out.sources = append(out.sources, projectReleaseSource{d.ID, d.Version, d.Phase, d.DocumentType})
		out.bytes[sha] = data
	}
	out.members = m7flow.SortMembers(out.members)
	out.inventory, _ = json.Marshal(map[string]any{"format": "lunitide-source-inventory-v1", "projectId": projectID, "members": out.members, "sources": out.sources})
	return out, nil
}

func (s *Store) BindReleaseContent(ctx context.Context, tx m7app.ReleaseTx, crID string, manifest map[string]any) error {
	t, ok := tx.(*agentRuntimeTx)
	if !ok {
		return m7app.ErrServiceUnavailable
	}
	content, err := s.projectReleaseContent(ctx, t.tx, crID, manifest)
	if err != nil {
		return err
	}
	manifest["members"], manifest["memberSources"], manifest["contentVerified"] = content.members, content.sources, true
	manifest["sbom"] = m7flow.SBOMRef{Format: "lunitide-source-inventory-v1", Digest: m7flow.SHA256Hex(content.inventory)}
	return nil
}
func (s *Store) CaptureReleaseContent(ctx context.Context, tx m7app.ReleaseTx, crID string, manifest map[string]any) error {
	t, ok := tx.(*agentRuntimeTx)
	if !ok {
		return m7app.ErrServiceUnavailable
	}
	content, err := s.projectReleaseContent(ctx, t.tx, crID, manifest)
	if err != nil {
		return err
	}
	if err := matchReleaseContent(content, manifest); err != nil {
		return err
	}
	now := time.Now().UTC()
	for digest, bytes := range content.bytes {
		if err := tx.PutReleaseBlob(digest, base64.StdEncoding.EncodeToString(bytes), now); err != nil {
			return err
		}
		if _, err := m7app.ReadReleaseMember(tx, digest, int64(len(bytes))); err != nil {
			return err
		}
	}
	digest := m7flow.SHA256Hex(content.inventory)
	if err := tx.PutReleaseBlob(digest, string(content.inventory), now); err != nil {
		return err
	}
	captured, err := tx.GetReleaseBlob(digest)
	if err != nil {
		return err
	}
	if captured != string(content.inventory) {
		return m7app.ErrDigestMismatch
	}
	return nil
}

func matchReleaseContent(content projectReleaseContent, manifest map[string]any) error {
	want := map[string]any{"members": content.members, "memberSources": content.sources, "contentVerified": true, "sbom": m7flow.SBOMRef{Format: "lunitide-source-inventory-v1", Digest: m7flow.SHA256Hex(content.inventory)}}
	for key, value := range want {
		a, _ := json.Marshal(value)
		var normalized any
		if err := json.Unmarshal(a, &normalized); err != nil {
			return err
		}
		a, _ = json.Marshal(normalized)
		b, _ := json.Marshal(manifest[key])
		if string(a) != string(b) {
			return fmt.Errorf("%w: approved content differs from revision %s", m7app.ErrDigestMismatch, key)
		}
	}
	return nil
}
