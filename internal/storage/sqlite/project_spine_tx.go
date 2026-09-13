package sqlite

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/lunitide/lunitide/internal/domain/project"
	"github.com/lunitide/lunitide/internal/projectapp"
	"github.com/lunitide/lunitide/internal/projectboard"
	"github.com/lunitide/lunitide/internal/projectrules"
	"github.com/lunitide/lunitide/internal/projectsync"
	"github.com/lunitide/lunitide/internal/projecttask"
	"github.com/lunitide/lunitide/internal/projecttestkit"
	"github.com/lunitide/lunitide/internal/projecttree"
	"github.com/oklog/ulid/v2"
)

func (t *txAdapter) resolveBoundTree(ctx context.Context, p project.Project) (projecttree.Tree, error) {
	if p.RootPath != "" {
		if raw, err := os.ReadFile(filepath.Join(p.RootPath, ".lunitide", "project-tree.json")); err == nil {
			if tree, perr := projecttree.Parse(raw); perr == nil {
				return tree, nil
			}
		}
	}
	var mime, path string
	err := t.q.QueryRowContext(ctx, `SELECT a.mime_type,a.file_path FROM project_deliverables d JOIN project_attachments a ON a.id=d.attachment_id WHERE d.project_id=? AND d.phase=1 AND d.document_type='project_structure'`, p.ID).Scan(&mime, &path)
	if err == nil && strings.Contains(strings.ToLower(mime), "json") && t.s.projectEvidenceFiles.attachments != nil {
		if raw, rerr := t.s.projectEvidenceFiles.attachments.ReadFile(ctx, path); rerr == nil {
			if tree, perr := projecttree.Parse(raw); perr == nil {
				return tree, nil
			}
		}
	}
	return projecttree.Default(p.Type == project.TypeOperations)
}

func (t *txAdapter) loadChecklistTx(ctx context.Context, projectID string, phase int, documentType string) (projecttask.Doc, error) {
	var attID sql.NullString
	err := t.q.QueryRowContext(ctx, `SELECT attachment_id FROM project_deliverables WHERE project_id=? AND phase=? AND document_type=?`, projectID, phase, documentType).Scan(&attID)
	if err == sql.ErrNoRows || !attID.Valid || attID.String == "" {
		return projecttask.Doc{Version: 1}, nil
	}
	if err != nil {
		return projecttask.Doc{}, err
	}
	var path string
	if err = t.q.QueryRowContext(ctx, `SELECT file_path FROM project_attachments WHERE id=?`, attID.String).Scan(&path); err != nil {
		if err == sql.ErrNoRows {
			return projecttask.Doc{Version: 1}, nil
		}
		return projecttask.Doc{}, err
	}
	if t.s.projectEvidenceFiles.attachments == nil {
		return projecttask.Doc{Version: 1}, nil
	}
	raw, err := t.s.projectEvidenceFiles.attachments.ReadFile(ctx, path)
	if err != nil {
		return projecttask.Doc{Version: 1}, nil
	}
	return projecttask.Parse(raw)
}

func (t *txAdapter) enforceDevChecklist(ctx context.Context, p project.Project) error {
	dev, err := t.loadChecklistTx(ctx, p.ID, project.DevPhase(p.Type), "dev_checklist")
	if err != nil {
		return err
	}
	if projectboard.Dirty(dev) {
		return projectapp.ErrBoardDirty
	}
	if err = projecttask.DevComplete(dev); err != nil {
		return err
	}
	if len(dev.Items) == 0 && p.Type != project.TypeOperations {
		feat, ferr := t.loadChecklistTx(ctx, p.ID, project.DesignPhase(p.Type), "feature_dev_list")
		if ferr == nil && len(feat.Items) > 0 {
			return projectapp.ErrDevIncomplete
		}
	}
	return nil
}

func (t *txAdapter) enforceFactoryGates(ctx context.Context, p project.Project, phase int) error {
	if phase == project.DBPhase(p.Type) && p.DBStatus != project.DBReady {
		return projectapp.ErrDBIncomplete
	}
	if phase == project.InterfacePhase(p.Type) {
		iface, err := t.loadChecklistTx(ctx, p.ID, phase, "interface_list")
		if err != nil {
			return err
		}
		if projectboard.Dirty(iface) {
			return projectapp.ErrBoardDirty
		}
		for _, item := range iface.Items {
			if item.ChangeKind == "removed" {
				continue
			}
			if item.Status != "dev_done" {
				return phaseGateError("interface board is incomplete")
			}
		}
	}
	if phase == project.TestPhase(p.Type) {
		test, err := t.loadChecklistTx(ctx, p.ID, phase, "test_checklist")
		if err != nil {
			return err
		}
		if projectboard.Dirty(test) {
			return projectapp.ErrBoardDirty
		}
		for _, item := range test.Items {
			if item.ChangeKind == "removed" {
				continue
			}
			if !projecttestkit.RequiredPassed(item) {
				return projectapp.ErrTestOpen
			}
		}
	}
	if phase == 7 && p.Type != project.TypeOperations {
		integ, err := t.loadChecklistTx(ctx, p.ID, 7, "integration_test_list")
		if err != nil {
			return err
		}
		if projectboard.Dirty(integ) {
			return projectapp.ErrBoardDirty
		}
		test, err := t.loadChecklistTx(ctx, p.ID, project.TestPhase(p.Type), "test_checklist")
		if err != nil {
			return err
		}
		for _, scene := range integ.Items {
			if scene.ChangeKind == "removed" {
				continue
			}
			if scene.Status != "test_pass" {
				return projectapp.ErrTestOpen
			}
		}
		if err = projecttask.TestClosed(test); err != nil {
			return err
		}
	}
	if phase == project.ReleasePhase(p.Type) {
		if _, err := projectsync.LoadReceipt(p.RootPath); err != nil {
			return projectapp.ErrSyncRequired
		}
	}
	return nil
}

func (t *txAdapter) materializeRulesTx(ctx context.Context, p project.Project) (projectrules.Manifest, error) {
	dev := t.txDeliverableText(ctx, p.ID, 1, "dev_standard")
	tech := t.txDeliverableText(ctx, p.ID, 1, "tech_standard")
	biz := t.txDeliverableText(ctx, p.ID, 1, "biz_standard")
	return projectrules.Materialize(p.RootPath, projectrules.Input{
		ProjectID: p.ID, DevStandard: dev, TechStandard: tech, BizStandard: biz,
		At: time.Now().UTC().Format(time.RFC3339),
	})
}

func (t *txAdapter) txDeliverableText(ctx context.Context, projectID string, phase int, documentType string) string {
	var attID sql.NullString
	err := t.q.QueryRowContext(ctx, `SELECT attachment_id FROM project_deliverables WHERE project_id=? AND phase=? AND document_type=?`, projectID, phase, documentType).Scan(&attID)
	if err != nil || !attID.Valid || attID.String == "" || t.s.projectEvidenceFiles.attachments == nil {
		return ""
	}
	var path string
	if err = t.q.QueryRowContext(ctx, `SELECT file_path FROM project_attachments WHERE id=?`, attID.String).Scan(&path); err != nil {
		return ""
	}
	raw, err := t.s.projectEvidenceFiles.attachments.ReadFile(ctx, path)
	if err != nil {
		return ""
	}
	return string(raw)
}

func (t *txAdapter) enforceTestChecklist(ctx context.Context, p project.Project) error {
	test, err := t.loadChecklistTx(ctx, p.ID, project.TestPhase(p.Type), "test_checklist")
	if err != nil {
		return err
	}
	return projecttask.TestClosed(test)
}

func (t *txAdapter) seedTestChecklist(ctx context.Context, p project.Project) error {
	test, err := t.loadChecklistTx(ctx, p.ID, project.TestPhase(p.Type), "test_checklist")
	if err != nil || len(test.Items) > 0 {
		return err
	}
	iface, err := t.loadChecklistTx(ctx, p.ID, project.InterfacePhase(p.Type), "interface_list")
	if err != nil {
		return err
	}
	dev, err := t.loadChecklistTx(ctx, p.ID, project.DevPhase(p.Type), "dev_checklist")
	if err != nil {
		return err
	}
	seeded := projectboard.BuildTestBoard(iface, dev)
	if len(seeded.Items) == 0 {
		return nil
	}
	return t.writeChecklistTx(ctx, p, project.TestPhase(p.Type), "test_checklist", "测试检查清单", seeded)
}

func (t *txAdapter) writeChecklistTx(ctx context.Context, p project.Project, phase int, documentType, title string, doc projecttask.Doc) error {
	writer, ok := t.s.projectEvidenceFiles.attachments.(interface {
		WriteFile(context.Context, string, []byte) error
	})
	if !ok {
		return nil
	}
	body, err := json.Marshal(doc)
	if err != nil {
		return err
	}
	id := ulid.Make().String()
	sum := sha256.Sum256(body)
	digest := hex.EncodeToString(sum[:])
	if err = writer.WriteFile(ctx, id, body); err != nil {
		return err
	}
	now := formatTime(time.Now().UTC())
	if _, err = t.q.ExecContext(ctx, `INSERT INTO project_attachments(id,project_id,phase,category,file_name,mime_type,file_path,digest,version,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,1,?,?)`,
		id, p.ID, phase, "checklist", documentType+".json", "application/json", id, digest, now, now); err != nil {
		return err
	}
	_, err = t.q.ExecContext(ctx, `INSERT INTO project_deliverables(id,project_id,phase,document_type,title,template_id,attachment_id,status,gate_confirmations,digest,created_at,updated_at,version)
		VALUES(?,?,?,?,?,NULL,?,?,0,?,?,?,1)
		ON CONFLICT(project_id, phase, document_type) DO UPDATE SET
			title=excluded.title,
			attachment_id=excluded.attachment_id,
			status=CASE WHEN project_deliverables.status='immutable' THEN project_deliverables.status ELSE excluded.status END,
			digest=excluded.digest,
			updated_at=excluded.updated_at,
			version=project_deliverables.version+1
		WHERE project_deliverables.status!='immutable'`,
		id, p.ID, phase, documentType, title, id, "review", digest, now, now)
	if err != nil {
		return err
	}
	if p.RootPath != "" && p.TreeStatus == project.TreeReady {
		if tree, terr := t.resolveBoundTree(ctx, p); terr == nil {
			if rel := tree.PhaseMap[strconv.Itoa(phase)]; rel != "" {
				_, _ = projecttree.ExportCopy(p.RootPath, rel, documentType+".json", body)
			}
		}
	}
	return nil
}
