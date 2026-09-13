package sqlite

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/lunitide/lunitide/internal/attachmentapp"
	"github.com/lunitide/lunitide/internal/domain/deliverable"
	"github.com/lunitide/lunitide/internal/domain/project"
	"github.com/lunitide/lunitide/internal/domain/projectattachment"
	"github.com/lunitide/lunitide/internal/domain/stage"
	"github.com/lunitide/lunitide/internal/projectapp"
	"github.com/lunitide/lunitide/internal/projectgen"
	"github.com/lunitide/lunitide/internal/projecttask"
	"github.com/lunitide/lunitide/internal/stageapp"
)

func phaseTestStore(t *testing.T, typ project.Type) (*Store, *projectapp.Service, project.Project) {
	t.Helper()
	ctx := context.Background()
	s, err := OpenTemplated(ctx, filepath.Join(t.TempDir(), "phase.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	files := attachmentapp.NewDirFileStorage(t.TempDir())
	s.SetProjectEvidenceFiles(files, files)
	svc := projectapp.New(s, s)
	p, err := svc.Create(ctx, "phase-project", "test", map[string]string{"name": "Phase"}, project.Project{Name: "Phase", Type: typ, Status: project.StatusChartered, RootPath: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	return s, svc, p
}
func seedPhaseTestEvidence(t *testing.T, s *Store, p project.Project, phase int) {
	t.Helper()
	ctx := context.Background()
	_, err := stageapp.New(s, s).Create(ctx, fmt.Sprintf("stage-%d", phase), "test", map[string]int{"phase": phase}, stage.Stage{ProjectID: p.ID, Phase: phase, Title: fmt.Sprintf("Phase %d", phase)})
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range project.RequiredPhaseDocuments(p.Type, phase) {
		content := []byte("Evidence for " + key + " — phase document body")
		name, mime := key+".txt", "text/plain"
		if projectgen.IsChecklistType(key) {
			content = []byte(`{"version":1,"items":[],"note":"Evidence for ` + key + `"}`)
			name, mime = key+".json", "application/json"
		}
		files := s.projectEvidenceFiles.attachments.(attachmentapp.FileStorage)
		if err = files.WriteFile(ctx, key, content); err != nil {
			t.Fatal(err)
		}
		sum := sha256.Sum256(content)
		a, err := s.CreateProjectAttachment(ctx, projectattachment.Attachment{ProjectID: p.ID, Phase: phase, FileName: name, FilePath: key, MimeType: mime, Digest: hex.EncodeToString(sum[:])})
		if err != nil {
			t.Fatal(err)
		}
		_, err = s.UpsertProjectDeliverable(ctx, deliverable.ProjectDeliverable{ProjectID: p.ID, Phase: phase, DocumentType: key, Title: key, AttachmentID: a.ID, Status: deliverable.StatusApproved})
		if err != nil {
			t.Fatal(err)
		}
	}
	if phase == project.DBPhase(p.Type) {
		if _, err = s.db.Exec(`UPDATE projects SET db_status='ready' WHERE id=?`, p.ID); err != nil {
			t.Fatal(err)
		}
	}
	if phase == project.ReleasePhase(p.Type) && p.RootPath != "" {
		dir := filepath.Join(p.RootPath, ".lunitide")
		if err = os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		receipt := []byte(`{"version":1,"projectId":"` + p.ID + `","sourceRoot":"src","destPath":"dest","at":"2026-09-13T00:00:00Z","files":[]}`)
		if err = os.WriteFile(filepath.Join(dir, "sync-receipt.json"), receipt, 0o644); err != nil {
			t.Fatal(err)
		}
	}
}
func completeTestPhase(svc *projectapp.Service, p project.Project, phase int) (project.Project, error) {
	return completeTestPhaseAs(svc, fmt.Sprintf("phase-%d", phase), p, phase)
}

func completeTestPhaseAs(svc *projectapp.Service, key string, p project.Project, phase int) (project.Project, error) {
	return svc.Mutate(context.Background(), key, "test", "project.advanceStatus", p.ID, p.Version, struct {
		Phase int `json:"phase"`
	}{phase}, func(*project.Project) error { return nil })
}
func TestProjectPhaseCompletesEveryDocumentPhaseAtomically(t *testing.T) {
	for _, typ := range []project.Type{project.TypeImplementation, project.TypeEnhancement, project.TypeOperations} {
		t.Run(string(typ), func(t *testing.T) {
			s, svc, p := phaseTestStore(t, typ)
			max := 7
			if typ == project.TypeOperations {
				max = 5
			}
			for phase := 1; phase <= max; phase++ {
				seedPhaseTestEvidence(t, s, p, phase)
				next, err := completeTestPhase(svc, p, phase)
				if err != nil {
					t.Fatalf("phase %d: %v", phase, err)
				}
				if phase == 1 {
					if next.TreeStatus != project.TreeReady || next.RootPath == "" {
						t.Fatalf("tree after phase 1: %+v", next)
					}
					if _, err := os.Stat(filepath.Join(next.RootPath, "src")); err != nil {
						t.Fatalf("src missing: %v", err)
					}
				}
				replay, err := completeTestPhase(svc, p, phase)
				if err != nil || replay.Version != next.Version {
					t.Fatalf("replay=%+v err=%v", replay, err)
				}
				p = next
				var unlocked int
				if err = s.db.QueryRow(`SELECT count(*) FROM project_deliverables WHERE project_id=? AND phase=? AND (status!='immutable' OR gate_confirmations!=3)`, p.ID, phase).Scan(&unlocked); err != nil || unlocked != 0 {
					t.Fatalf("unlocked=%d err=%v", unlocked, err)
				}
				var status string
				if err = s.db.QueryRow(`SELECT status FROM stages WHERE project_id=? AND phase=?`, p.ID, phase).Scan(&status); err != nil || status != "completed" {
					t.Fatalf("stage=%s err=%v", status, err)
				}
			}
		})
	}
}
func TestProjectPhaseFailureRollsBackFrozenEvidenceAndStage(t *testing.T) {
	for _, point := range []string{"stages", "projects", "audit_events"} {
		t.Run(point, func(t *testing.T) {
			s, svc, p := phaseTestStore(t, project.TypeImplementation)
			seedPhaseTestEvidence(t, s, p, 1)
			op := "UPDATE"
			if point == "audit_events" {
				op = "INSERT"
			}
			_, err := s.db.Exec(fmt.Sprintf("CREATE TRIGGER injected_phase_failure BEFORE %s ON %s BEGIN SELECT RAISE(ABORT,'injected phase failure'); END", op, point))
			if err != nil {
				t.Fatal(err)
			}
			if _, err = completeTestPhase(svc, p, 1); err == nil {
				t.Fatal("injected failure was ignored")
			}
			saved, err := svc.Get(context.Background(), p.ID)
			if err != nil || saved.Version != p.Version || saved.Status != p.Status {
				t.Fatalf("saved=%+v err=%v", saved, err)
			}
			var changed int
			if err = s.db.QueryRow(`SELECT count(*) FROM project_deliverables WHERE project_id=? AND (status!='approved' OR gate_confirmations!=0)`, p.ID).Scan(&changed); err != nil || changed != 0 {
				t.Fatalf("partially frozen=%d err=%v", changed, err)
			}
			var status string
			_ = s.db.QueryRow(`SELECT status FROM stages WHERE project_id=? AND phase=1`, p.ID).Scan(&status)
			if status != "not_started" {
				t.Fatalf("partially advanced stage=%s", status)
			}
		})
	}
}
func TestProjectPhaseRejectsMissingAndCrossPhaseEvidence(t *testing.T) {
	s, svc, p := phaseTestStore(t, project.TypeImplementation)
	seedPhaseTestEvidence(t, s, p, 1)
	if _, err := s.db.Exec(`UPDATE project_attachments SET phase=2 WHERE project_id=?`, p.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := completeTestPhase(svc, p, 1); err == nil {
		t.Fatal("cross-phase evidence accepted")
	}
	if _, err := s.db.Exec(`UPDATE project_deliverables SET attachment_id=NULL WHERE project_id=?`, p.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := completeTestPhase(svc, p, 1); err == nil {
		t.Fatal("missing evidence accepted")
	}
}
func TestProjectDeliverableApprovalPreservesEvidenceReference(t *testing.T) {
	s, _, p := phaseTestStore(t, project.TypeImplementation)
	seedPhaseTestEvidence(t, s, p, 1)
	docs, err := s.ListProjectDeliverables(context.Background(), deliverable.Filter{ProjectID: p.ID, Phase: 1})
	if err != nil {
		t.Fatal(err)
	}
	d := docs[0]
	saved, err := s.UpsertProjectDeliverable(context.Background(), deliverable.ProjectDeliverable{ProjectID: p.ID, Phase: 1, DocumentType: d.DocumentType, Title: d.Title, Status: deliverable.StatusApproved})
	if err != nil || saved.AttachmentID != d.AttachmentID {
		t.Fatalf("evidence lost saved=%+v err=%v", saved, err)
	}
}

func TestProjectPhaseRejectsMissingTamperedOrUnboundFiles(t *testing.T) {
	for _, kind := range []string{"missing", "tampered", "changed_metadata", "reader_unavailable"} {
		t.Run(kind, func(t *testing.T) {
			s, svc, p := phaseTestStore(t, project.TypeImplementation)
			seedPhaseTestEvidence(t, s, p, 1)
			docs, err := s.ListProjectDeliverables(context.Background(), deliverable.Filter{ProjectID: p.ID, Phase: 1})
			if err != nil {
				t.Fatal(err)
			}
			d := docs[0]
			a, err := s.GetProjectAttachment(context.Background(), d.AttachmentID)
			if err != nil {
				t.Fatal(err)
			}
			files := s.projectEvidenceFiles.attachments.(attachmentapp.FileStorage)
			switch kind {
			case "missing":
				err = files.DeleteFile(context.Background(), a.FilePath)
			case "tampered", "changed_metadata":
				content := []byte("changed after approval")
				err = files.WriteFile(context.Background(), a.FilePath, content)
				if err == nil && kind == "changed_metadata" {
					sum := sha256.Sum256(content)
					_, err = s.db.Exec(`UPDATE project_attachments SET digest=?,version=version+1 WHERE id=?`, hex.EncodeToString(sum[:]), a.ID)
				}
			case "reader_unavailable":
				s.SetProjectEvidenceFiles(nil, nil)
			}
			if err != nil {
				t.Fatal(err)
			}
			if _, err = completeTestPhase(svc, p, 1); err == nil {
				t.Fatal("unverified file completed project phase")
			}
			current, err := svc.Get(context.Background(), p.ID)
			if err != nil || current.Version != p.Version {
				t.Fatalf("partial project advance=%+v err=%v", current, err)
			}
			if _, err = s.ConfirmDeliverableGate(context.Background(), p.ID, d.ID, d.Version); err == nil {
				t.Fatal("gate confirmation ignored file integrity")
			}
		})
	}
}

func seedChecklistJSON(t *testing.T, s *Store, p project.Project, phase int, documentType string, body []byte) {
	t.Helper()
	ctx := context.Background()
	_, err := stageapp.New(s, s).Create(ctx, fmt.Sprintf("stage-%d", phase), "test", map[string]int{"phase": phase}, stage.Stage{ProjectID: p.ID, Phase: phase, Title: fmt.Sprintf("Phase %d", phase)})
	if err != nil {
		t.Fatal(err)
	}
	files := s.projectEvidenceFiles.attachments.(attachmentapp.FileStorage)
	if err = files.WriteFile(ctx, documentType, body); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(body)
	a, err := s.CreateProjectAttachment(ctx, projectattachment.Attachment{ProjectID: p.ID, Phase: phase, FileName: documentType + ".json", FilePath: documentType, MimeType: "application/json", Digest: hex.EncodeToString(sum[:])})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.UpsertProjectDeliverable(ctx, deliverable.ProjectDeliverable{ProjectID: p.ID, Phase: phase, DocumentType: documentType, Title: documentType, AttachmentID: a.ID, Status: deliverable.StatusApproved, Digest: hex.EncodeToString(sum[:])}); err != nil {
		t.Fatal(err)
	}
}

func completePhases(t *testing.T, s *Store, svc *projectapp.Service, p project.Project, through int) project.Project {
	t.Helper()
	for phase := 1; phase <= through; phase++ {
		seedPhaseTestEvidence(t, s, p, phase)
		next, err := completeTestPhase(svc, p, phase)
		if err != nil {
			t.Fatalf("phase %d: %v", phase, err)
		}
		p = next
	}
	return p
}

func TestProjectPhaseTreeFailDoesNotAdvance(t *testing.T) {
	s, svc, p := phaseTestStore(t, project.TypeImplementation)
	if err := os.WriteFile(filepath.Join(p.RootPath, "src"), []byte("blocked"), 0o644); err != nil {
		t.Fatal(err)
	}
	seedPhaseTestEvidence(t, s, p, 1)
	if _, err := completeTestPhase(svc, p, 1); !errors.Is(err, projectapp.ErrTreeFailed) {
		t.Fatalf("got %v", err)
	}
	saved, err := svc.Get(context.Background(), p.ID)
	if err != nil || saved.Status != project.StatusChartered || saved.TreeStatus == project.TreeReady {
		t.Fatalf("status leaked: %+v %v", saved, err)
	}
}

func TestProjectPhaseDevIncompleteAndTestOpen(t *testing.T) {
	s, svc, p := phaseTestStore(t, project.TypeImplementation)
	p = completePhases(t, s, svc, p, 4)
	seedChecklistJSON(t, s, p, 5, "dev_checklist", []byte(`{"version":1,"items":[{"id":"F001","title":"登录","status":"pending"}]}`))
	if _, err := completeTestPhase(svc, p, 5); !errors.Is(err, projectapp.ErrDevIncomplete) {
		t.Fatalf("dev gate: %v", err)
	}
	saved, err := svc.Get(context.Background(), p.ID)
	if err != nil || saved.Status != p.Status {
		t.Fatalf("dev advance leaked: %+v %v", saved, err)
	}

	s2, svc2, p2 := phaseTestStore(t, project.TypeImplementation)
	p2 = completePhases(t, s2, svc2, p2, 4)
	seedChecklistJSON(t, s2, p2, 5, "dev_checklist", []byte(`{"version":1,"items":[{"id":"F001","title":"登录","status":"dev_done"}]}`))
	next, err := completeTestPhase(svc2, p2, 5)
	if err != nil {
		t.Fatal(err)
	}
	seedChecklistJSON(t, s2, next, 6, "test_checklist", []byte(`{"version":1,"items":[{"id":"T-F001","title":"测登录","status":"pending","sourceId":"F001"}]}`))
	if _, err = completeTestPhase(svc2, next, 6); !errors.Is(err, projectapp.ErrTestOpen) {
		t.Fatalf("test gate: %v", err)
	}
}

func TestProjectPhase7ReadsSameChecklistFacts(t *testing.T) {
	s, svc, p := phaseTestStore(t, project.TypeImplementation)
	p = completePhases(t, s, svc, p, 6)
	files := s.projectEvidenceFiles.attachments.(attachmentapp.FileStorage)
	if err := files.WriteFile(context.Background(), "test_checklist", []byte(`{"version":1,"items":[{"id":"T-F001","title":"测登录","status":"pending","sourceId":"F001"}]}`)); err != nil {
		t.Fatal(err)
	}
	seedPhaseTestEvidence(t, s, p, 7)
	if _, err := completeTestPhase(svc, p, 7); !errors.Is(err, projectapp.ErrTestOpen) {
		t.Fatalf("integration gate: %v", err)
	}
}

func TestProjectPhaseSeedsInterfaceAndDevTests(t *testing.T) {
	s, svc, p := phaseTestStore(t, project.TypeImplementation)
	p = completePhases(t, s, svc, p, 4)
	files := s.projectEvidenceFiles.attachments.(attachmentapp.FileStorage)
	iface := []byte(`{"version":1,"items":[{"id":"I001","title":"登录接口","status":"dev_done"}]}`)
	if err := files.WriteFile(context.Background(), "interface_list", iface); err != nil {
		t.Fatal(err)
	}
	seedChecklistJSON(t, s, p, 5, "dev_checklist", []byte(`{"version":1,"items":[{"id":"F001","title":"登录","status":"dev_done"}]}`))
	next, err := completeTestPhase(svc, p, 5)
	if err != nil {
		t.Fatal(err)
	}
	docs, err := s.ListProjectDeliverables(context.Background(), deliverable.Filter{ProjectID: next.ID, Phase: 6})
	if err != nil {
		t.Fatal(err)
	}
	var rec deliverable.ProjectDeliverable
	for _, item := range docs {
		if item.DocumentType == "test_checklist" {
			rec = item
			break
		}
	}
	if rec.AttachmentID == "" {
		t.Fatal("test checklist was not seeded")
	}
	att, err := s.GetProjectAttachment(context.Background(), rec.AttachmentID)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := files.ReadFile(context.Background(), att.FilePath)
	if err != nil {
		t.Fatal(err)
	}
	doc, err := projecttask.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, ok := projecttask.Find(doc, "T-I001"); !ok {
		t.Fatalf("missing interface-derived test: %+v", doc.Items)
	}
	if _, _, ok := projecttask.Find(doc, "T-F001"); !ok {
		t.Fatalf("missing dev-derived test: %+v", doc.Items)
	}
}

func TestProjectDeliverableApprovalBindsServerFileDigest(t *testing.T) {
	s, _, p := phaseTestStore(t, project.TypeImplementation)
	seedPhaseTestEvidence(t, s, p, 1)
	docs, err := s.ListProjectDeliverables(context.Background(), deliverable.Filter{ProjectID: p.ID, Phase: 1})
	if err != nil {
		t.Fatal(err)
	}
	d := docs[0]
	actual := d.Digest
	d.Digest = "caller-cannot-choose-approval-digest"
	updated, err := s.UpsertProjectDeliverable(context.Background(), d)
	if err != nil || updated.Digest != actual {
		t.Fatalf("approval=%+v err=%v", updated, err)
	}
	d.Status = deliverable.StatusImmutable
	if _, err = s.UpsertProjectDeliverable(context.Background(), d); err == nil {
		t.Fatal("direct immutable upsert bypassed gate")
	}
}
