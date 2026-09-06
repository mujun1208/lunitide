package sqlite

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"path/filepath"
	"testing"

	"github.com/lunitide/lunitide/internal/attachmentapp"
	"github.com/lunitide/lunitide/internal/domain/deliverable"
	"github.com/lunitide/lunitide/internal/domain/project"
	"github.com/lunitide/lunitide/internal/domain/projectattachment"
	"github.com/lunitide/lunitide/internal/domain/stage"
	"github.com/lunitide/lunitide/internal/projectapp"
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
	p, err := svc.Create(ctx, "phase-project", "test", map[string]string{"name": "Phase"}, project.Project{Name: "Phase", Type: typ, Status: project.StatusChartered})
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
		content := []byte("Evidence for " + key)
		files := s.projectEvidenceFiles.attachments.(attachmentapp.FileStorage)
		if err = files.WriteFile(ctx, key, content); err != nil {
			t.Fatal(err)
		}
		sum := sha256.Sum256(content)
		a, err := s.CreateProjectAttachment(ctx, projectattachment.Attachment{ProjectID: p.ID, Phase: phase, FileName: key + ".txt", FilePath: key, MimeType: "text/plain", Digest: hex.EncodeToString(sum[:])})
		if err != nil {
			t.Fatal(err)
		}
		_, err = s.UpsertProjectDeliverable(ctx, deliverable.ProjectDeliverable{ProjectID: p.ID, Phase: phase, DocumentType: key, Title: key, AttachmentID: a.ID, Status: deliverable.StatusApproved})
		if err != nil {
			t.Fatal(err)
		}
	}
}
func completeTestPhase(svc *projectapp.Service, p project.Project, phase int) (project.Project, error) {
	return svc.Mutate(context.Background(), fmt.Sprintf("phase-%d", phase), "test", "project.advanceStatus", p.ID, p.Version, struct {
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
