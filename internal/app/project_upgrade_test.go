package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/lunitide/lunitide/internal/attachmentapp"
	"github.com/lunitide/lunitide/internal/domain/deliverable"
	"github.com/lunitide/lunitide/internal/domain/project"
	"github.com/lunitide/lunitide/internal/domain/projectattachment"
	"github.com/lunitide/lunitide/internal/domain/stage"
	"github.com/lunitide/lunitide/internal/projectapp"
	"github.com/lunitide/lunitide/internal/providerapp"
	"github.com/lunitide/lunitide/internal/stageapp"
	storage "github.com/lunitide/lunitide/internal/storage/sqlite"
)

func seedProjectPhaseEvidence(t *testing.T, store *storage.Store, id string, phase int) {
	t.Helper()
	ctx := context.Background()
	files := attachmentapp.NewDirFileStorage(t.TempDir())
	store.SetProjectEvidenceFiles(files, files)
	_, err := stageapp.New(store, store).Create(ctx, fmt.Sprintf("seed-stage-%d", phase), "test", map[string]any{"projectId": id, "phase": phase}, stage.Stage{ProjectID: id, Phase: phase, Title: fmt.Sprintf("Phase %d", phase)})
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range project.RequiredPhaseDocuments(project.TypeImplementation, phase) {
		content := []byte("Evidence for " + key)
		if err = files.WriteFile(ctx, key, content); err != nil {
			t.Fatal(err)
		}
		sum := sha256.Sum256(content)
		att, err := store.CreateProjectAttachment(ctx, projectattachment.Attachment{ProjectID: id, Phase: phase, FileName: key + ".txt", FilePath: key, MimeType: "text/plain", Digest: hex.EncodeToString(sum[:])})
		if err != nil {
			t.Fatal(err)
		}
		if _, err = store.UpsertProjectDeliverable(ctx, deliverable.ProjectDeliverable{ProjectID: id, Phase: phase, DocumentType: key, Title: key, AttachmentID: att.ID, Status: deliverable.StatusApproved}); err != nil {
			t.Fatal(err)
		}
	}
}

func TestProjectUpgradeRejectsAdvanceWithoutPublicationOrEvidence(t *testing.T) {
	e, svc, p := projectUpgradeEngine(t)
	r := validRequest("project.advanceStatus", fmt.Sprintf(`{"id":%q,"version":%d,"phase":8}`, p.ID, p.Version))
	r.IdempotencyKey = "skip-phase"
	out := e.Handle(context.Background(), r)
	if out.OK || out.Error == nil || out.Error.Code != "PROJECT_INVALID_TRANSITION" {
		t.Fatalf("out=%+v", out)
	}
	saved, err := svc.Get(context.Background(), p.ID)
	if err != nil || saved.Version != p.Version || saved.Status != project.StatusCreated {
		t.Fatalf("saved=%+v err=%v", saved, err)
	}
}

func projectUpgradeEngine(t *testing.T) (*Engine, *projectapp.Service, project.Project) {
	t.Helper()
	store, err := storage.OpenTemplated(context.Background(), filepath.Join(t.TempDir(), "project-upgrade.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	svc := projectapp.New(store, store)
	e := NewEngineWithProjects(providerapp.New(store, store), svc, "test", nil)
	p, err := svc.Create(context.Background(), "upgrade-create", "test", map[string]string{"name": "Upgrade"}, project.Project{Name: "Upgrade"})
	if err != nil {
		t.Fatal(err)
	}
	return e, svc, p
}

func TestProjectUpgradeOneHundredUpdatesAndReplays(t *testing.T) {
	e, svc, p := projectUpgradeEngine(t)
	for i := 1; i <= 100; i++ {
		payload, _ := json.Marshal(projectUpdatePayload{ID: p.ID, Version: p.Version, Name: p.Name, Summary: fmt.Sprintf("revision %d", i)})
		r := validRequest("project.update", string(payload))
		r.IdempotencyKey = fmt.Sprintf("revision-%d", i)
		first := e.Handle(context.Background(), r)
		if !first.OK {
			t.Fatalf("update %d: %+v", i, first.Error)
		}
		replay := e.Handle(context.Background(), r)
		if !replay.OK || !reflect.DeepEqual(first.Payload, replay.Payload) {
			t.Fatalf("replay %d differs: first=%#v replay=%#v", i, first, replay)
		}
		var err error
		p, err = svc.Get(context.Background(), p.ID)
		if err != nil || p.Version != int64(i+1) || p.Summary != fmt.Sprintf("revision %d", i) {
			t.Fatalf("update %d persisted=%+v err=%v", i, p, err)
		}
	}
}

func TestProjectUpgradeSameKeyChangedPayloadConflicts(t *testing.T) {
	e, svc, p := projectUpgradeEngine(t)
	payload := projectUpdatePayload{ID: p.ID, Version: p.Version, Name: p.Name, Summary: "first"}
	raw, _ := json.Marshal(payload)
	r := validRequest("project.update", string(raw))
	r.IdempotencyKey = "changed-payload"
	if out := e.Handle(context.Background(), r); !out.OK {
		t.Fatal(out.Error)
	}
	payload.Summary = "different"
	r.Payload, _ = json.Marshal(payload)
	out := e.Handle(context.Background(), r)
	if out.OK || out.Error == nil || out.Error.Code != "IDEMPOTENCY_CONFLICT" {
		t.Fatalf("out=%+v", out)
	}
	saved, err := svc.Get(context.Background(), p.ID)
	if err != nil || saved.Summary != "first" || saved.Version != 2 {
		t.Fatalf("saved=%+v err=%v", saved, err)
	}
}

func TestProjectUpgradeCloseReopenReplayPreservesLifecycleFields(t *testing.T) {
	e, svc, p := projectUpgradeEngine(t)
	var err error
	p, err = svc.Mutate(context.Background(), "seed-active", "test", "project.update", p.ID, p.Version, map[string]string{"status": "req_architecture"}, func(p *project.Project) error { p.Status = project.StatusReqArchitecture; return nil })
	if err != nil {
		t.Fatal(err)
	}
	for i, method := range []string{"project.close", "project.reopen", "project.close", "project.reopen"} {
		raw, _ := json.Marshal(projectMutationMeta{ID: p.ID, Version: p.Version, Reason: fmt.Sprintf("reason %d", i)})
		r := validRequest(method, string(raw))
		r.IdempotencyKey = fmt.Sprintf("cycle-%d", i)
		first := e.Handle(context.Background(), r)
		replay := e.Handle(context.Background(), r)
		if !first.OK || !replay.OK || !reflect.DeepEqual(first.Payload, replay.Payload) {
			t.Fatalf("%s first=%+v replay=%+v", method, first.Error, replay.Error)
		}
		var err error
		p, err = svc.Get(context.Background(), p.ID)
		if err != nil {
			t.Fatal(err)
		}
	}
}
