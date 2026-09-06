package app

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/lunitide/lunitide/internal/attachmentapp"
	"github.com/lunitide/lunitide/internal/domain/deliverable"
	"github.com/lunitide/lunitide/internal/domain/m7flow"
	"github.com/lunitide/lunitide/internal/domain/projectattachment"
	"github.com/lunitide/lunitide/internal/m7app"
	storage "github.com/lunitide/lunitide/internal/storage/sqlite"
)

func projectReleaseFixture(t *testing.T) (*Engine, *storage.Store, attachmentapp.FileStorage, string, string) {
	t.Helper()
	ctx := context.Background()
	e, sessionID, store := agentRunEngine(t)
	session, err := store.GetSession(ctx, sessionID)
	if err != nil {
		t.Fatal(err)
	}
	p, err := store.GetProject(ctx, session.ProjectID)
	if err != nil {
		t.Fatal(err)
	}
	files := attachmentapp.NewDirFileStorage(t.TempDir())
	store.SetProjectEvidenceFiles(files, files)
	for _, entry := range []struct {
		phase int
		key   string
	}{{3, "db_design"}, {4, "interface_list"}, {5, "dev_checklist"}} {
		data := []byte("Actual approved bytes for " + entry.key)
		if err = files.WriteFile(ctx, entry.key, data); err != nil {
			t.Fatal(err)
		}
		att, err := store.CreateProjectAttachment(ctx, projectattachment.Attachment{ProjectID: p.ID, Phase: entry.phase, FilePath: entry.key, FileName: entry.key + ".json", MimeType: "application/json", Digest: m7flow.SHA256Hex(data)})
		if err != nil {
			t.Fatal(err)
		}
		if _, err = store.UpsertProjectDeliverable(ctx, deliverable.ProjectDeliverable{ProjectID: p.ID, Phase: entry.phase, DocumentType: entry.key, Title: entry.key, AttachmentID: att.ID, Status: deliverable.StatusApproved}); err != nil {
			t.Fatal(err)
		}
	}
	release := m7app.NewReleaseService(store.AgentRuntimeRepository())
	release.SetProjectContent(store)
	e.SetM7ReleaseServices(release)
	return e, store, files, p.ID, "CR-" + p.ProjectCode
}
func TestProjectReleaseUsesRealBytesAndImmutableCapturedContent(t *testing.T) {
	ctx := context.Background()
	e, _, files, projectID, crID := projectReleaseFixture(t)
	manifest := map[string]any{"authorId": "workbench", "summary": "Release", "projectId": projectID, "members": []any{map[string]any{"name": "fake.stub", "size": 1024, "sha256": "fake"}}}
	revision, err := e.m7release.CreateRevision(ctx, crID, manifest)
	if err != nil {
		t.Fatal(err)
	}
	var bound map[string]any
	if err = json.Unmarshal([]byte(revision.ManifestJSON), &bound); err != nil {
		t.Fatal(err)
	}
	members := bound["members"].([]any)
	if len(members) != 3 {
		t.Fatal("missing sources", members)
	}
	for _, entry := range members {
		m := entry.(map[string]any)
		if m["name"] == "fake.stub" || m["size"] == float64(1024) || m["sha256"] == "fake" {
			t.Fatal("caller member trusted")
		}
	}
	pkg, err := e.m7release.BuildPackage(ctx, revision.ID, revision.Digest)
	if err != nil {
		t.Fatal(err)
	}
	if err = files.DeleteFile(ctx, "db_design"); err != nil {
		t.Fatal(err)
	}
	view, err := e.m7release.GetPackage(ctx, pkg.ID)
	if err != nil || !view.Verified {
		t.Fatalf("captured bytes not durable: %+v %v", view, err)
	}
	if _, err = e.m7release.BuildPackage(ctx, revision.ID, fmt.Sprintf("%064d", 0)); err == nil {
		t.Fatal("wrong digest replay accepted")
	}
}
func TestProjectReleaseChangedMissingUnapprovedOrWrongProjectCannotSeal(t *testing.T) {
	for _, kind := range []string{"changed", "missing", "wrong project"} {
		t.Run(kind, func(t *testing.T) {
			ctx := context.Background()
			e, _, files, projectID, crID := projectReleaseFixture(t)
			manifest := map[string]any{"authorId": "workbench", "summary": "Release", "projectId": projectID}
			revision, err := e.m7release.CreateRevision(ctx, crID, manifest)
			if err != nil {
				t.Fatal(err)
			}
			switch kind {
			case "changed":
				err = files.WriteFile(ctx, "db_design", []byte("changed"))
			case "missing":
				err = files.DeleteFile(ctx, "db_design")
			case "wrong project":
				_, err = e.m7release.CreateRevision(ctx, "CR-another", manifest)
				if err == nil {
					t.Fatal("foreign CR accepted")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if _, err = e.m7release.BuildPackage(ctx, revision.ID, revision.Digest); err == nil {
				t.Fatal("invalid source sealed")
			}
		})
	}
}

func TestProjectLocalPublicationWritesVerifiesRevokesAndRejectsExternalProd(t *testing.T) {
	ctx := context.Background()
	e, store, _, projectID, crID := projectReleaseFixture(t)
	revision, err := e.m7release.CreateRevision(ctx, crID, map[string]any{"projectId": projectID, "authorId": "workbench", "summary": "Actual local publication"})
	if err != nil {
		t.Fatal(err)
	}
	pkg, err := e.m7release.BuildPackage(ctx, revision.ID, revision.Digest)
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	publisher := m7app.NewPromotionService(store.AgentRuntimeRepository())
	publisher.SetLocalPublication(root)
	in := m7app.PromoteInput{PackageID: pkg.ID, TargetEnv: "dev", RequestID: "local-dev", PolicyContext: map[string]any{"requestedBy": "tester"}}
	prm, err := publisher.Promote(ctx, in)
	if err != nil {
		t.Fatal(err)
	}
	view, err := publisher.GetPromotion(ctx, prm.ID)
	if err != nil || len(view.Deployments) != 1 || view.Promotion.State != m7flow.PrmSucceeded {
		t.Fatalf("publication %+v %v", view, err)
	}
	receipt := view.Deployments[0].ReceiptJSON
	if err = m7app.VerifyLocalPublication(ctx, root, receipt); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(root, projectID, "dev", pkg.BlobDigest, "members", "db_design.json")
	data, err := os.ReadFile(file)
	if err != nil || string(data) != "Actual approved bytes for db_design" {
		t.Fatalf("not actual content: %q %v", data, err)
	}
	if err = os.WriteFile(file, []byte("tampered"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err = publisher.GetPromotion(ctx, prm.ID); err == nil {
		t.Fatal("tampered publication reported verified")
	}
	if _, err = publisher.Promote(ctx, in); err == nil {
		t.Fatal("tampered publication replay returned success")
	}
	if err = os.WriteFile(file, data, 0600); err != nil {
		t.Fatal(err)
	}
	rolled, _, err := publisher.Rollback(ctx, m7app.RollbackInput{PromotionID: prm.ID, Reason: "withdraw", RequestID: "withdraw", OperatorID: "tester"})
	if err != nil || rolled.State != m7flow.PrmRolledBack {
		t.Fatalf("rollback %+v %v", rolled, err)
	}
	if err = m7app.VerifyLocalPublication(ctx, root, receipt); err == nil {
		t.Fatal("revoked receipt accepted")
	}
	in.TargetEnv = "prod"
	in.RequestID = "no-external-prod"
	if _, err = publisher.Promote(ctx, in); err == nil {
		t.Fatal("unconfigured production deployment accepted")
	}
}
