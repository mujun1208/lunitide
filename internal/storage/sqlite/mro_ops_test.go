package sqlite

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/oklog/ulid/v2"

	"github.com/lunitide/lunitide/internal/mroapp"
)

func TestMROOpsDueMissingAndCheckoutAndPublish(t *testing.T) {
	ctx := context.Background()
	store, err := OpenTemplated(ctx, filepath.Join(t.TempDir(), "ops.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	svc := mroapp.New(store)

	dueID := ulid.Make().String()
	if err := svc.UpsertDueItem(ctx, mroapp.DueItem{
		ID: dueID, ScopeID: "B-0001", Kind: "FH", LimitValue: 100, UsedMissing: true,
	}); err != nil {
		t.Fatal(err)
	}
	dues, err := svc.ListDue(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(dues) != 1 || dues[0].Status.Label != mroapp.DueLabelMissing || dues[0].Status.State != mroapp.DueStateMissing {
		t.Fatalf("due missing = %+v", dues)
	}

	overdueID := ulid.Make().String()
	if err := svc.UpsertTool(ctx, mroapp.Tool{ID: overdueID, ToolNo: "TW-1", CalibDue: "2020-01-01"}); err != nil {
		t.Fatal(err)
	}
	if err := svc.CheckoutTool(ctx, overdueID, "tech-1"); !errors.Is(err, mroapp.ErrCheckoutBlocked) {
		t.Fatalf("overdue checkout = %v", err)
	}
	okID := ulid.Make().String()
	if err := svc.UpsertTool(ctx, mroapp.Tool{ID: okID, ToolNo: "TW-2", CalibDue: "2099-01-01"}); err != nil {
		t.Fatal(err)
	}
	if err := svc.CheckoutTool(ctx, okID, "tech-1"); err != nil {
		t.Fatal(err)
	}

	parent := ulid.Make().String()
	child := ulid.Make().String()
	if err := svc.UpsertChemLot(ctx, mroapp.ChemLot{ID: parent, LotNo: "M-1"}); err != nil {
		t.Fatal(err)
	}
	if err := svc.UpsertChemLot(ctx, mroapp.ChemLot{ID: child, LotNo: "M-1-A", ParentLotID: parent}); err != nil {
		t.Fatal(err)
	}
	if err := svc.InsertChemUse(ctx, mroapp.ChemUse{LotID: child, TailNo: "B-0001"}); err != nil {
		t.Fatal(err)
	}
	trace, err := svc.TraceLotID(ctx, parent)
	if err != nil || len(trace.Tails) != 1 || trace.Tails[0] != "B-0001" {
		t.Fatalf("trace = %+v %v", trace, err)
	}
	chain, err := svc.BulletinChain(ctx, parent)
	if err != nil || len(chain.Tails) != 1 {
		t.Fatalf("bulletin = %+v %v", chain, err)
	}

	if err := svc.UpsertKit(ctx, mroapp.Kit{ID: ulid.Make().String(), Name: "C检套件"}, []mroapp.KitItem{{PN: "SEAL-1", Required: 2, OnHand: 0}}); err != nil {
		t.Fatal(err)
	}
	kits, err := svc.ListKitViews(ctx)
	if err != nil || len(kits) != 1 || len(kits[0].Missing) != 1 {
		t.Fatalf("kits = %+v %v", kits, err)
	}

	if err := svc.UpsertPartsStock(ctx, mroapp.PartsStock{PN: "NAS1", Qty: 0, Source: "local"}); err != nil {
		t.Fatal(err)
	}
	if err := svc.UpsertAlternate(ctx, mroapp.Alternate{PNFrom: "NAS1", PNTo: "NAS2", CertOK: true, Effectivity: "*"}); err != nil {
		t.Fatal(err)
	}
	stock, alts, err := svc.ListParts(ctx, "A320")
	if err != nil || len(stock) != 1 || len(alts) != 1 || alts[0].Accepted {
		t.Fatalf("parts = %+v %+v %v", stock, alts, err)
	}

	pkg, err := svc.BuildWorkPackage(ctx, "C检草稿", []string{"card"}, []string{"ad"}, []string{"mel"}, []string{"open"})
	if err != nil || len(pkg.Sources) != 4 {
		t.Fatalf("wp = %+v %v", pkg, err)
	}
	todos, err := svc.PublishSchedule(ctx, pkg.ID)
	if err != nil || len(todos) != 2 || todos[0].Kind != "kit_staging" || todos[1].Kind != "parts_request" {
		t.Fatalf("publish = %+v %v", todos, err)
	}
	listed, err := svc.ListOpsTodos(ctx)
	if err != nil || len(listed) != 2 {
		t.Fatalf("todos = %+v %v", listed, err)
	}

	if err := svc.ProposeIntervalChangeDraft(ctx, "C-CHK", "", "fleet"); !errors.Is(err, mroapp.ErrPayloadInvalid) {
		t.Fatalf("interval without mpd = %v", err)
	}
	if err := svc.ProposeIntervalChangeDraft(ctx, "C-CHK", "MPD 05-20", "本队 FH"); err != nil {
		t.Fatal(err)
	}
}

func TestMROOpsCheckoutReason(t *testing.T) {
	ctx := context.Background()
	store, err := OpenTemplated(ctx, filepath.Join(t.TempDir(), "ops-reason.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	svc := mroapp.New(store)
	id := ulid.Make().String()
	if err := svc.UpsertTool(ctx, mroapp.Tool{ID: id, ToolNo: "TW-X", CalibDue: "2019-12-31"}); err != nil {
		t.Fatal(err)
	}
	err = svc.CheckoutTool(ctx, id, "tech")
	var blocked *mroapp.CheckoutBlockedError
	if !errors.As(err, &blocked) || blocked.Reason != "校准过期" {
		t.Fatalf("reason = %v", err)
	}
}
