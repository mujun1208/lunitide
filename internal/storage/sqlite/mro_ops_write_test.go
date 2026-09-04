package sqlite

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/oklog/ulid/v2"

	"github.com/lunitide/lunitide/internal/mroapp"
)

func newOpsService(t *testing.T) (context.Context, *Store, *mroapp.Service) {
	t.Helper()
	ctx := context.Background()
	store, err := OpenTemplated(ctx, filepath.Join(t.TempDir(), "ops-write.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return ctx, store, mroapp.New(store)
}

func TestRecordUtilizationRecomputesDue(t *testing.T) {
	ctx, _, svc := newOpsService(t)
	dueID := ulid.Make().String()
	if err := svc.UpsertDueItem(ctx, mroapp.DueItem{ID: dueID, ScopeID: "B-1", Kind: "FH", LimitValue: 100, UsedMissing: true}); err != nil {
		t.Fatal(err)
	}
	views, err := svc.RecordUtilization(ctx, "B-1", 30, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(views) != 1 || views[0].UsedMissing || views[0].UsedValue != 30 || views[0].Status.State != mroapp.DueStateOK {
		t.Fatalf("after 30h = %+v", views)
	}
	views, err = svc.RecordUtilization(ctx, "B-1", 80, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	if views[0].UsedValue != 110 || views[0].Status.State != mroapp.DueStateOverdue {
		t.Fatalf("after +80h = %+v", views)
	}
	// A scope with no events must never be invented to zero.
	other := ulid.Make().String()
	if err := svc.UpsertDueItem(ctx, mroapp.DueItem{ID: other, ScopeID: "B-2", Kind: "FC", LimitValue: 50, UsedMissing: true}); err != nil {
		t.Fatal(err)
	}
	all, err := svc.RecomputeDue(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, v := range all {
		if v.ScopeID == "B-2" && !v.UsedMissing {
			t.Fatalf("B-2 should stay missing: %+v", v)
		}
	}
}

func TestReturnToolClearsHolder(t *testing.T) {
	ctx, _, svc := newOpsService(t)
	id := ulid.Make().String()
	if err := svc.UpsertTool(ctx, mroapp.Tool{ID: id, ToolNo: "TW-9", CalibDue: "2099-01-01"}); err != nil {
		t.Fatal(err)
	}
	if err := svc.CheckoutTool(ctx, id, "tech-7"); err != nil {
		t.Fatal(err)
	}
	if err := svc.ReturnTool(ctx, id); err != nil {
		t.Fatal(err)
	}
	tools, err := svc.ListToolViews(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(tools) != 1 || tools[0].Holder != "" || tools[0].Status != "ready" {
		t.Fatalf("after return = %+v", tools)
	}
}

func TestCapacityAndIntervalConstraints(t *testing.T) {
	ctx, _, svc := newOpsService(t)
	if err := svc.UpsertScheduleAssignment(ctx, mroapp.ScheduleAssignment{TailNo: "B-1", CheckName: "A-CHK", Hours: 10, Skill: "SM"}); err != nil {
		t.Fatal(err)
	}
	if err := svc.UpsertCapacitySlot(ctx, "SM", 5); err != nil {
		t.Fatal(err)
	}
	violations, err := svc.CheckConstraints(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !hasCode(violations, "C2") {
		t.Fatalf("expected C2, got %+v", violations)
	}
	// Interval rule without a source cite trips C7; adding a cite clears it.
	if err := svc.UpsertIntervalRule(ctx, mroapp.IntervalRule{TaskKey: "C-CHK", IntervalValue: 500, Unit: "FH"}); err != nil {
		t.Fatal(err)
	}
	violations, _ = svc.CheckConstraints(ctx)
	if !hasCode(violations, "C7") {
		t.Fatalf("expected C7 without cite, got %+v", violations)
	}
	if err := svc.UpsertIntervalRule(ctx, mroapp.IntervalRule{TaskKey: "C-CHK", IntervalValue: 500, Unit: "FH", SourceCite: "MPD 05-10"}); err != nil {
		t.Fatal(err)
	}
	rules, err := svc.ListIntervalRules(ctx)
	if err != nil || len(rules) != 1 {
		t.Fatalf("interval replace = %+v %v", rules, err)
	}
	violations, _ = svc.CheckConstraints(ctx)
	if hasCode(violations, "C7") {
		t.Fatalf("C7 should clear with cite, got %+v", violations)
	}
}

func TestBuildWorkPackagePersistsTasks(t *testing.T) {
	ctx, store, svc := newOpsService(t)
	pkg, err := svc.BuildWorkPackage(ctx, "C检", []string{"CARD-1", "CARD-2"}, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	keys, err := store.ListWorkPackageTasks(ctx, pkg.ID)
	if err != nil || len(keys) != 2 {
		t.Fatalf("wp tasks = %+v %v", keys, err)
	}
}

func TestLowAltGenealogyAndDrafts(t *testing.T) {
	ctx, _, svc := newOpsService(t)
	comp, err := svc.UpsertComponent(ctx, "SN-1", "PN-1", 120)
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.RecordLifeEvent(ctx, comp.ID, "install", "2026-01-01", "on B-1"); err != nil {
		t.Fatal(err)
	}
	genealogies, err := svc.ListGenealogies(ctx)
	if err != nil || len(genealogies) != 1 || !genealogies[0].Installed {
		t.Fatalf("genealogy installed = %+v %v", genealogies, err)
	}
	if err := svc.RecordLifeEvent(ctx, comp.ID, "remove", "2026-02-01", "shop"); err != nil {
		t.Fatal(err)
	}
	genealogies, _ = svc.ListGenealogies(ctx)
	if genealogies[0].Installed || len(genealogies[0].Events) != 2 {
		t.Fatalf("after remove = %+v", genealogies[0])
	}
	if _, err := svc.DraftPirep(ctx, "B-1", `{"note":"vibration"}`); err != nil {
		t.Fatal(err)
	}
	pireps, err := svc.ListPireps(ctx)
	if err != nil || len(pireps) != 1 || pireps[0].State != "draft" {
		t.Fatalf("pireps = %+v %v", pireps, err)
	}
	aog, err := svc.IntakeAOG(ctx, "tail: B-2\npn: NAS-9\nqty: 3\nleft main gear")
	if err != nil || aog.TailNo != "B-2" || aog.PN != "NAS-9" {
		t.Fatalf("aog = %+v %v", aog, err)
	}
	cases, err := svc.ListAOG(ctx)
	if err != nil || len(cases) != 1 {
		t.Fatalf("aog list = %+v %v", cases, err)
	}
	if _, err := svc.DraftPO(ctx, "NAS-9", "3", "1200"); err != nil {
		t.Fatal(err)
	}
	pos, err := svc.ListPO(ctx)
	if err != nil || len(pos) != 1 || pos[0].State != "draft" {
		t.Fatalf("po = %+v %v", pos, err)
	}
	if genealogies, _ = svc.ListGenealogies(ctx); genealogies[0].TailNo != "" {
		t.Fatalf("removed component should not keep tail: %+v", genealogies[0])
	}
	if err := svc.ConfirmPirep(ctx, pireps[0].ID, "confirmed"); err != nil {
		t.Fatal(err)
	}
	if err := svc.ConfirmAOG(ctx, aog.ID, "rejected"); err != nil {
		t.Fatal(err)
	}
	if err := svc.ConfirmPO(ctx, pos[0].ID, "confirmed"); err != nil {
		t.Fatal(err)
	}
	pireps, _ = svc.ListPireps(ctx)
	cases, _ = svc.ListAOG(ctx)
	pos, _ = svc.ListPO(ctx)
	if pireps[0].State != "confirmed" || cases[0].State != "rejected" || pos[0].State != "confirmed" {
		t.Fatalf("states pirep=%s aog=%s po=%s", pireps[0].State, cases[0].State, pos[0].State)
	}
	dues, err := svc.ListDue(ctx)
	if err != nil {
		t.Fatal(err)
	}
	foundCheck := false
	for _, d := range dues {
		if d.Kind == "CHECK" && d.ScopeID == "B-1" && strings.HasPrefix(d.Source, "pirep:") {
			foundCheck = true
			break
		}
	}
	if !foundCheck {
		t.Fatalf("confirmed pirep should open an advisory CHECK due: %+v", dues)
	}
	if err := svc.ConfirmPirep(ctx, pireps[0].ID, "rejected"); err == nil {
		t.Fatal("second confirm should fail")
	}
}

func TestIssueChemicalAndAddPartsTodo(t *testing.T) {
	ctx, _, svc := newOpsService(t)
	if err := svc.UpsertChemLot(ctx, mroapp.ChemLot{LotNo: "M-1", Qty: 10, Expires: "2027-01-01"}); err != nil {
		t.Fatal(err)
	}
	listed, err := svc.ListLotViews(ctx)
	if err != nil || len(listed) != 1 {
		t.Fatalf("parent = %+v %v", listed, err)
	}
	parentID := listed[0].ID
	child, err := svc.IssueChemical(ctx, parentID, 3, "B-9", "WO-1", "tech-1")
	if err != nil || child.ParentLotID != parentID || child.Qty != 3 {
		t.Fatalf("child = %+v %v", child, err)
	}
	lots, err := svc.ListLotViews(ctx)
	if err != nil || len(lots) != 2 {
		t.Fatalf("lots after issue = %+v %v", lots, err)
	}
	foundParent := false
	for _, lot := range lots {
		if lot.ID == parentID {
			foundParent = true
			if lot.Qty != 7 {
				t.Fatalf("parent qty = %+v", lot)
			}
			if len(lot.Tails) == 0 || lot.Tails[0] != "B-9" {
				t.Fatalf("parent tails = %+v", lot)
			}
		}
	}
	if !foundParent {
		t.Fatalf("parent missing after issue: %+v", lots)
	}
	kitID := ulid.Make().String()
	if err := svc.UpsertKit(ctx, mroapp.Kit{ID: kitID, Name: "C检套件"}, []mroapp.KitItem{{KitID: kitID, PN: "SEAL-1", Required: 2, OnHand: 0}}); err != nil {
		t.Fatal(err)
	}
	todo, err := svc.AddPartsTodo(ctx, kitID, "SEAL-1")
	if err != nil || todo.Kind != "parts_request" || todo.Ref != kitID {
		t.Fatalf("todo = %+v %v", todo, err)
	}
	again, err := svc.AddPartsTodo(ctx, kitID, "SEAL-1")
	if err != nil || again.ID != todo.ID {
		t.Fatalf("idempotent add = %+v %v", again, err)
	}
}

func hasCode(violations []mroapp.ConstraintViolation, code string) bool {
	for _, v := range violations {
		if v.Code == code {
			return true
		}
	}
	return false
}
