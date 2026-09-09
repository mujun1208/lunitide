package m8app_test

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/lunitide/lunitide/internal/domain/m8core"
	"github.com/lunitide/lunitide/internal/domain/project"
	"github.com/lunitide/lunitide/internal/domain/session"
	"github.com/lunitide/lunitide/internal/m8app"
	"github.com/lunitide/lunitide/internal/projectapp"
	"github.com/lunitide/lunitide/internal/sessionapp"
)

func createManualExpert(t *testing.T, svc *m8app.ExpertService, name string) m8app.CreateResult {
	t.Helper()
	out, err := svc.Create(context.Background(), m8app.CreateInput{Source: "local", Frontmatter: fm(name), SixSection: sixBody(name), RequestID: "manual-" + name})
	if err != nil {
		t.Fatal(err)
	}
	if out.State != "disabled" || out.CreationOrigin != "manual" || out.Name != name {
		t.Fatalf("creation: %+v", out)
	}
	return out
}

func deleteInput(created m8app.CreateResult) m8app.ExpertDeleteInput {
	return m8app.ExpertDeleteInput{ExpertID: created.ExpertID, ExpectedVersionID: created.VersionID, ConfirmToken: m8app.ExpertDeleteToken(created.ExpertID, created.VersionID)}
}

func TestExpertManualTrialEnableDeleteLifecycle(t *testing.T) {
	ctx := context.Background()
	store := openSliceStore(t)
	repo := store.AgentRuntimeRepository()
	svc := m8app.NewExpertService(repo, "local-user", &m8app.MemoryPersonaStore{})
	created := createManualExpert(t, svc, "short-drama")
	before, err := svc.Detail(ctx, m8app.DetailInput{ExpertID: created.ExpertID})
	if err != nil {
		t.Fatal(err)
	}
	trial, err := svc.PrepareTrial(ctx, created.ExpertID, created.VersionID)
	if err != nil || trial.State != "disabled" || trial.Name != created.Name || trial.SixSection != sixBody(created.Name) {
		t.Fatalf("trial %+v: %v", trial, err)
	}
	after, err := svc.Detail(ctx, m8app.DetailInput{ExpertID: created.ExpertID})
	if err != nil || !reflect.DeepEqual(before, after) {
		t.Fatalf("trial mutated expert: %v", err)
	}
	if _, err := svc.Mount(ctx, m8app.MountInput{ExpertID: created.ExpertID, ProjectID: "01ARZ3NDEKTSV4RRFFQ69G5FAV", PhaseKey: m8core.PhaseArchitecturePlan, Action: "mount"}); !errors.Is(err, m8app.ErrExpertNotMountable) {
		t.Fatalf("disabled mount: %v", err)
	}
	if _, err := svc.Toggle(ctx, m8app.ExpertToggleInput{ExpertID: created.ExpertID, Enabled: true}); err != nil {
		t.Fatal(err)
	}
	listed, err := svc.List(ctx, m8app.ExpertFilter{CreationOrigin: "manual", State: "enabled"})
	if err != nil || len(listed.Experts) != 1 || !listed.Experts[0].IsOwn {
		t.Fatalf("enabled filter: %+v %v", listed, err)
	}
	if err := svc.Delete(ctx, deleteInput(created)); err != nil {
		t.Fatal(err)
	}
	if err := svc.Delete(ctx, deleteInput(created)); err != nil {
		t.Fatalf("delete retry: %v", err)
	}
	listed, err = svc.List(ctx, m8app.ExpertFilter{})
	if err != nil || len(listed.Experts) != 0 {
		t.Fatalf("deleted still listed: %+v %v", listed, err)
	}
	if _, err := svc.Detail(ctx, m8app.DetailInput{ExpertID: created.ExpertID}); !errors.Is(err, m8app.ErrExpertNotFound) {
		t.Fatalf("deleted detail: %v", err)
	}
	if _, err := svc.Toggle(ctx, m8app.ExpertToggleInput{ExpertID: created.ExpertID, Enabled: true}); !errors.Is(err, m8app.ErrExpertNotFound) {
		t.Fatalf("deleted enabled: %v", err)
	}
	if _, err := svc.PrepareTrial(ctx, created.ExpertID, created.VersionID); !errors.Is(err, m8app.ErrExpertNotFound) {
		t.Fatalf("deleted trial: %v", err)
	}
	if err := repo.TransactExpert(ctx, func(tx m8app.ExpertTx) error {
		row, err := tx.GetExpert(created.ExpertID)
		if err != nil {
			return err
		}
		versions, err := tx.ListVersions(created.ExpertID)
		if err != nil {
			return err
		}
		if row.DeletedAt == "" || row.State != "archived" || row.Name != created.Name || row.CreationOrigin != "manual" || len(versions) != 1 {
			t.Fatalf("history lost: %+v %+v", row, versions)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func TestExpertDeleteGuardsOwnershipOriginAndVersion(t *testing.T) {
	ctx := context.Background()
	store := openSliceStore(t)
	repo := store.AgentRuntimeRepository()
	svc := m8app.NewExpertService(repo, "owner", &m8app.MemoryPersonaStore{})
	created := createManualExpert(t, svc, "own")
	foreign := m8app.NewExpertService(repo, "other", &m8app.MemoryPersonaStore{})
	if err := foreign.Delete(ctx, deleteInput(created)); !errors.Is(err, m8app.ErrExpertNotFound) {
		t.Fatalf("foreign delete: %v", err)
	}
	if _, err := foreign.PrepareTrial(ctx, created.ExpertID, created.VersionID); !errors.Is(err, m8app.ErrExpertNotFound) {
		t.Fatalf("foreign trial: %v", err)
	}
	if _, err := foreign.Toggle(ctx, m8app.ExpertToggleInput{ExpertID: created.ExpertID, Enabled: true}); !errors.Is(err, m8app.ErrExpertNotFound) {
		t.Fatalf("foreign enable: %v", err)
	}
	list, err := foreign.List(ctx, m8app.ExpertFilter{CreationOrigin: "manual"})
	if err != nil || len(list.Experts) != 0 {
		t.Fatalf("foreign filter %+v %v", list, err)
	}
	bad := deleteInput(created)
	bad.ConfirmToken = "bad"
	if err := svc.Delete(ctx, bad); !errors.Is(err, m8app.ErrPayloadInvalid) {
		t.Fatalf("confirmation: %v", err)
	}
	_, err = svc.Update(ctx, m8app.UpdateInput{ExpertID: created.ExpertID, ExpectedVersionID: created.VersionID, SixSection: map[string]string{"identity": "i", "mission": "revised", "rules": "r", "workflow": "w", "deliverableTemplate": "d", "successMetrics": "s"}, ChangeNote: "revision"})
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.Delete(ctx, deleteInput(created)); !errors.Is(err, m8app.ErrExpertVersionConflict) {
		t.Fatalf("stale delete: %v", err)
	}
	if _, err := svc.PrepareTrial(ctx, created.ExpertID, created.VersionID); !errors.Is(err, m8app.ErrExpertVersionConflict) {
		t.Fatalf("stale trial: %v", err)
	}
	for _, origin := range []string{"builtin", "catalog"} {
		t.Run(origin, func(t *testing.T) {
			in := m8app.CreateInput{Source: "local", CreationOrigin: origin, Frontmatter: fm(origin), SixSection: sixBody(origin), RequestID: origin}
			x, err := svc.Create(ctx, in)
			if err != nil {
				t.Fatal(err)
			}
			if x.State != "enabled" {
				t.Fatalf("factory disabled: %+v", x)
			}
			if err := svc.Delete(ctx, deleteInput(x)); !errors.Is(err, m8app.ErrExpertManualOnly) {
				t.Fatalf("protected delete: %v", err)
			}
			if _, err := svc.PrepareTrial(ctx, x.ExpertID, x.VersionID); !errors.Is(err, m8app.ErrExpertManualOnly) {
				t.Fatalf("protected trial: %v", err)
			}
		})
	}
}

func TestExpertDeleteBlocksProjectAndSessionReferences(t *testing.T) {
	ctx := context.Background()
	store := openSliceStore(t)
	svc := m8app.NewExpertService(store.AgentRuntimeRepository(), "local-user", &m8app.MemoryPersonaStore{})
	created := createManualExpert(t, svc, "mounted")
	p, err := projectapp.New(store, store).Create(ctx, "project", "test", nil, project.Project{Name: "Expert test"})
	if err != nil {
		t.Fatal(err)
	}
	s, err := sessionapp.New(store, store).Create(ctx, "session", "test", nil, session.Session{ProjectID: p.ID, Title: "Trial"})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.ReplaceSessionExpertIDs(ctx, s.ID, []string{created.ExpertID}); err == nil {
		t.Fatal("disabled manual expert mounted")
	}
	if _, err := svc.Toggle(ctx, m8app.ExpertToggleInput{ExpertID: created.ExpertID, Enabled: true}); err != nil {
		t.Fatal(err)
	}
	mount := m8app.MountInput{ExpertID: created.ExpertID, ProjectID: p.ID, PhaseKey: m8core.PhaseArchitecturePlan, Action: "mount"}
	if _, err := svc.Mount(ctx, mount); err != nil {
		t.Fatal(err)
	}
	if err := svc.Delete(ctx, deleteInput(created)); !errors.Is(err, m8app.ErrExpertInUse) {
		t.Fatalf("project guard: %v", err)
	}
	mount.Action = "unmount"
	if _, err := svc.Mount(ctx, mount); err != nil {
		t.Fatal(err)
	}
	if err := store.ReplaceSessionExpertIDs(ctx, s.ID, []string{created.ExpertID}); err != nil {
		t.Fatal(err)
	}
	if err := svc.Delete(ctx, deleteInput(created)); !errors.Is(err, m8app.ErrExpertInUse) {
		t.Fatalf("session guard: %v", err)
	}
	if err := store.ReplaceSessionExpertIDs(ctx, s.ID, nil); err != nil {
		t.Fatal(err)
	}
	if err := svc.Delete(ctx, deleteInput(created)); err != nil {
		t.Fatal(err)
	}
	if err := store.ReplaceSessionExpertIDs(ctx, s.ID, []string{created.ExpertID}); err == nil {
		t.Fatal("deleted expert remounted")
	}
}

func TestExpertBootstrapPreservesSameNameManualProfile(t *testing.T) {
	ctx := context.Background()
	store := openSliceStore(t)
	svc := m8app.NewExpertService(store.AgentRuntimeRepository(), "local-user", &m8app.MemoryPersonaStore{})
	svc.SetSkillStore(store)
	created := createManualExpert(t, svc, "PPT专家")
	if err := m8app.EnsureBuiltinExperts(ctx, svc); err != nil {
		t.Fatal(err)
	}
	detail, err := svc.Detail(ctx, m8app.DetailInput{ExpertID: created.ExpertID})
	if err != nil {
		t.Fatal(err)
	}
	if detail.Expert["state"] != "disabled" || detail.Expert["creationOrigin"] != "manual" || detail.Expert["catalogItemId"] != nil || len(detail.Versions) != 1 {
		t.Fatalf("manual profile overwritten: %+v", detail)
	}
	list, err := svc.List(ctx, m8app.ExpertFilter{CreationOrigin: "manual"})
	if err != nil || len(list.Experts) != 1 {
		t.Fatalf("manual filter includes bundled local rows: %+v %v", list, err)
	}
}

func TestExpertTrialRejectsForeignVersionPointer(t *testing.T) {
	ctx := context.Background()
	store := openSliceStore(t)
	repo := store.AgentRuntimeRepository()
	svc := m8app.NewExpertService(repo, "owner", &m8app.MemoryPersonaStore{})
	one := createManualExpert(t, svc, "one")
	two := createManualExpert(t, svc, "two")
	if err := repo.TransactExpert(ctx, func(tx m8app.ExpertTx) error {
		row, err := tx.GetExpert(one.ExpertID)
		if err != nil {
			return err
		}
		row.CurrentVersionID = two.VersionID
		return tx.PutExpert(row)
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.PrepareTrial(ctx, one.ExpertID, two.VersionID); !errors.Is(err, m8app.ErrExpertNotFound) {
		t.Fatalf("foreign profile exposed: %v", err)
	}
}
