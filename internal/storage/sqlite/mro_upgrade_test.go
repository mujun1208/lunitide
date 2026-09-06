package sqlite

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/lunitide/lunitide/internal/m8app"
	"github.com/lunitide/lunitide/internal/mroapp"
	"github.com/oklog/ulid/v2"
)

func mroReadyDocument(t *testing.T, s *Store) string {
	t.Helper()
	ctx := context.Background()
	kb := m8app.NewKBService(s.AgentRuntimeRepository(), "test-user")
	collection, err := kb.EnsureExpertCollection(ctx, ulid.Make().String())
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "manual.md")
	body := []byte("# Controlled manual\n\nTest maintenance source content.\n")
	if err = os.WriteFile(path, body, 0600); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(body)
	id := ulid.Make().String()
	if _, err = kb.UpsertDocument(ctx, m8app.KBUpsertInput{CollectionID: collection.CollectionID, DocumentID: id, MediaType: "text/markdown", ContentRef: path, SHA256: hex.EncodeToString(sum[:]), SourceLocator: "mro://AMM/42?status=controlled", Projector: m8app.ParseBodyIndexer}); err != nil {
		t.Fatal(err)
	}
	return id
}
func mroCount(t *testing.T, s *Store, table string) int {
	t.Helper()
	var n int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM ` + table).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}
func mroPlanningFixture(t *testing.T) (context.Context, *Store, *mroapp.Service, mroapp.WorkPackage, string) {
	t.Helper()
	ctx, s, svc := newOpsService(t)
	doc := mroReadyDocument(t, s)
	manual, err := svc.RegisterManual(ctx, mroapp.ManualInput{Title: "AMM", DocType: "AMM", Revision: "42", Status: "controlled", Documents: []mroapp.ManualDocInput{{DocumentID: doc, PartNo: 1}}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = svc.UpsertAircraft(ctx, mroapp.AircraftInput{TailNo: "B-1", Model: "test"}); err != nil {
		t.Fatal(err)
	}
	if err = svc.UpsertIntervalRule(ctx, mroapp.IntervalRule{TaskKey: "task-1", IntervalValue: 100, Unit: "FH", SourceCite: "manual:" + manual.ManualID}); err != nil {
		t.Fatal(err)
	}
	if err = svc.UpsertScheduleAssignment(ctx, mroapp.ScheduleAssignment{TailNo: "B-1", CheckName: "test", Start: "2099-01-01", End: "2099-01-02", Hours: 2, Skill: "test"}); err != nil {
		t.Fatal(err)
	}
	if err = svc.UpsertCapacitySlot(ctx, "test", 4); err != nil {
		t.Fatal(err)
	}
	pkg, err := svc.BuildWorkPackage(ctx, "Verified work package", []string{"task-1"}, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	return ctx, s, svc, pkg, doc
}
func TestMROUpgradePublicationCurrentEvidenceReplayAndStaleness(t *testing.T) {
	ctx, s, svc, pkg, doc := mroPlanningFixture(t)
	first, err := svc.PublishSchedule(ctx, pkg.ID)
	if err != nil || len(first) != 2 {
		t.Fatalf("publish=%+v %v", first, err)
	}
	second, err := svc.PublishSchedule(ctx, pkg.ID)
	if err != nil || second[0].ID != first[0].ID || mroCount(t, s, "mro_ops_todos") != 2 {
		t.Fatalf("replay duplicated=%+v %v", second, err)
	}
	listed, err := svc.ListWorkPackages(ctx)
	if err != nil || listed[0].EvidenceState != "current" {
		t.Fatalf("current=%+v %v", listed, err)
	}
	if _, err = s.db.Exec(`UPDATE kb_documents SET index_state='failed' WHERE document_id=?`, doc); err != nil {
		t.Fatal(err)
	}
	if _, err = svc.PublishSchedule(ctx, pkg.ID); !errors.Is(err, mroapp.ErrConstraints) {
		t.Fatalf("stale source publish=%v", err)
	}
	listed, err = svc.ListWorkPackages(ctx)
	if err != nil || listed[0].EvidenceState != "blocked" {
		t.Fatalf("stale source not displayed=%+v %v", listed, err)
	}
	if mroCount(t, s, "mro_ops_todos") != 2 {
		t.Fatal("stale replay duplicated todos")
	}
}
func TestMROUpgradeZeroCapacityAndAfter200RiskBlockPublication(t *testing.T) {
	ctx, _, svc, pkg, _ := mroPlanningFixture(t)
	if err := svc.UpsertCapacitySlot(ctx, "test", 0); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.PublishSchedule(ctx, pkg.ID); !errors.Is(err, mroapp.ErrConstraints) {
		t.Fatalf("zero capacity=%v", err)
	}
	if err := svc.UpsertCapacitySlot(ctx, "test", 4); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 205; i++ {
		date := "2099-01-01"
		if i == 204 {
			date = "2000-01-01"
		}
		if err := svc.UpsertDueItem(ctx, mroapp.DueItem{ScopeID: "B-1", Kind: "CAL", DueAt: date}); err != nil {
			t.Fatal(err)
		}
	}
	dues, err := svc.ListDue(ctx)
	if err != nil || len(dues) != 205 {
		t.Fatalf("rows=%d err=%v", len(dues), err)
	}
	if _, err = svc.PublishSchedule(ctx, pkg.ID); !errors.Is(err, mroapp.ErrConstraints) {
		t.Fatalf("risk beyond old 200 cap=%v", err)
	}
}
func TestMROUpgradeToolAndChemicalTransactionsRollBackAndSerialize(t *testing.T) {
	ctx, s, svc := newOpsService(t)
	id := ulid.Make().String()
	if err := svc.UpsertTool(ctx, mroapp.Tool{ID: id, ToolNo: "T-1", CalibDue: "2099-01-01"}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec(`CREATE TRIGGER mro_test_tool_fail BEFORE UPDATE ON mro_tools WHEN NEW.status='out' BEGIN SELECT RAISE(ABORT,'injected tool fault'); END`); err != nil {
		t.Fatal(err)
	}
	if err := svc.CheckoutTool(ctx, id, "tech"); err == nil {
		t.Fatal("tool partial update succeeded")
	}
	if mroCount(t, s, "mro_tool_loans") != 0 {
		t.Fatal("orphan loan")
	}
	if _, err := s.db.Exec(`DROP TRIGGER mro_test_tool_fail`); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	results := make(chan error, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); results <- svc.CheckoutTool(ctx, id, "tech") }()
	}
	wg.Wait()
	close(results)
	ok := 0
	for err := range results {
		if err == nil {
			ok++
		} else if !errors.Is(err, mroapp.ErrCheckoutBlocked) {
			t.Fatal(err)
		}
	}
	if ok != 1 || mroCount(t, s, "mro_tool_loans") != 1 {
		t.Fatalf("concurrent double loan successful=%d", ok)
	}
	lot := ulid.Make().String()
	if err := svc.UpsertChemLot(ctx, mroapp.ChemLot{ID: lot, LotNo: "L-1", Qty: 10, Expires: "2099-01-01"}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec(`CREATE TRIGGER mro_test_child_fail BEFORE INSERT ON mro_chem_lots WHEN NEW.parent_lot_id IS NOT NULL BEGIN SELECT RAISE(ABORT,'injected child fault'); END`); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.IssueChemical(ctx, lot, 3, "B-1", "WO", "tech"); err == nil {
		t.Fatal("partial chemical update succeeded")
	}
	lots, err := s.ListChemLots(ctx)
	if err != nil || len(lots) != 1 || lots[0].Qty != 10 {
		t.Fatalf("stock partially deducted=%+v %v", lots, err)
	}
}
func TestMROUpgradePirepAndWorkPackageFailureLeaveNoPartialState(t *testing.T) {
	ctx, s, svc := newOpsService(t)
	p, err := svc.DraftPirep(ctx, "B-1", `{"symptom":"test"}`)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.db.Exec(`CREATE TRIGGER mro_test_due_fail BEFORE INSERT ON mro_due_items BEGIN SELECT RAISE(ABORT,'injected due fault'); END`); err != nil {
		t.Fatal(err)
	}
	if err = svc.ConfirmPirep(ctx, p.ID, "confirmed"); err == nil {
		t.Fatal("partial confirmation accepted")
	}
	drafts, err := svc.ListPireps(ctx)
	if err != nil || drafts[0].State != "draft" {
		t.Fatalf("confirmed without due=%+v %v", drafts, err)
	}
	if _, err = s.db.Exec(`CREATE TRIGGER mro_test_task_fail BEFORE INSERT ON mro_wp_tasks BEGIN SELECT RAISE(ABORT,'injected task fault'); END`); err != nil {
		t.Fatal(err)
	}
	if _, err = svc.BuildWorkPackage(ctx, "bad", []string{"task"}, nil, nil, nil); err == nil {
		t.Fatal("partial work package accepted")
	}
	if mroCount(t, s, "mro_work_packages") != 0 {
		t.Fatal("orphan package")
	}
}
func TestMROUpgradeRequestReplayPayloadScopeAndAuditAtomicity(t *testing.T) {
	ctx, s, svc := newOpsService(t)
	digest := strings.Repeat("a", 64)
	calls := 0
	fn := func(txCtx context.Context) (json.RawMessage, error) {
		calls++
		err := svc.UpsertPartsStock(txCtx, mroapp.PartsStock{PN: "P-1", Qty: 3})
		return json.RawMessage(`{"ok":true}`), err
	}
	replay := func(context.Context) error { return nil }
	if _, err := svc.ExecuteRequest(ctx, "mro.parts.stock.upsert", "key", digest, fn, replay); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.ExecuteRequest(ctx, "mro.parts.stock.upsert", "key", digest, fn, replay); err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Fatal("receipt replay ran mutation")
	}
	if _, err := svc.ExecuteRequest(ctx, "mro.parts.stock.upsert", "key", strings.Repeat("b", 64), fn, replay); !errors.Is(err, mroapp.ErrConflict) {
		t.Fatalf("changed payload=%v", err)
	}
	if _, err := s.db.Exec(`CREATE TRIGGER mro_test_audit_fail BEFORE INSERT ON mro_operation_audit BEGIN SELECT RAISE(ABORT,'injected audit fault'); END`); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.ExecuteRequest(ctx, "mro.parts.stock.upsert", "bad", digest, func(txCtx context.Context) (json.RawMessage, error) {
		err := svc.UpsertPartsStock(txCtx, mroapp.PartsStock{PN: "P-2", Qty: 9})
		return json.RawMessage(`{"ok":true}`), err
	}, replay); err == nil {
		t.Fatal("audit failure success")
	}
	if mroCount(t, s, "mro_parts_stock") != 1 || mroCount(t, s, "mro_request_receipts") != 1 {
		t.Fatal("request partial commit")
	}
}
func TestMROUpgradePrivateScopeAndParentReferences(t *testing.T) {
	ctx, s, svc := newOpsService(t)
	var contexts []context.Context
	now := time.Now().UTC().Format(time.RFC3339Nano)
	for i := 0; i < 2; i++ {
		id := ulid.Make().String()
		if _, err := s.db.Exec(`INSERT INTO organizations(org_id,name,state,created_at,updated_at) VALUES(?,?,'active',?,?)`, id, fmt.Sprintf("org-%d", i), now, now); err != nil {
			t.Fatal(err)
		}
		contexts = append(contexts, mroapp.WithScope(ctx, id))
	}
	contexts = append(contexts, ctx)
	for i, c := range contexts {
		if _, err := svc.UpsertAircraft(c, mroapp.AircraftInput{TailNo: "B-SAME", Model: fmt.Sprint(i)}); err != nil {
			t.Fatal(err)
		}
		if err := svc.UpsertPartsStock(c, mroapp.PartsStock{PN: "P-SAME", Qty: float64(i + 1)}); err != nil {
			t.Fatal(err)
		}
	}
	for i, c := range contexts {
		a, err := svc.ListAircraft(c)
		if err != nil || len(a) != 1 || a[0].Model != fmt.Sprint(i) {
			t.Fatalf("aircraft scope=%+v %v", a, err)
		}
		stock, _, err := svc.ListParts(c, "")
		if err != nil || len(stock) != 1 || stock[0].Qty != float64(i+1) {
			t.Fatalf("stock scope=%+v %v", stock, err)
		}
	}
	tool := ulid.Make().String()
	if err := svc.UpsertTool(contexts[0], mroapp.Tool{ID: tool, ToolNo: "private", CalibDue: "2099-01-01"}); err != nil {
		t.Fatal(err)
	}
	if err := svc.CheckoutTool(contexts[1], tool, "other"); !errors.Is(err, mroapp.ErrNotFound) {
		t.Fatalf("cross scope tool=%v", err)
	}
	if err := svc.UpsertTool(contexts[1], mroapp.Tool{ID: tool, ToolNo: "stolen"}); !errors.Is(err, mroapp.ErrScope) {
		t.Fatalf("cross scope upsert=%v", err)
	}
	parent := ulid.Make().String()
	if err := svc.UpsertChemLot(contexts[0], mroapp.ChemLot{ID: parent, LotNo: "private"}); err != nil {
		t.Fatal(err)
	}
	if err := svc.InsertChemUse(contexts[1], mroapp.ChemUse{LotID: parent, TailNo: "B-SAME"}); !errors.Is(err, mroapp.ErrScope) {
		t.Fatalf("cross scope parent=%v", err)
	}
}

func TestMROUpgradeStaleSnapshotAndDeadlineRequireRebuild(t *testing.T) {
	ctx, _, svc, pkg, _ := mroPlanningFixture(t)
	if _, err := svc.PublishSchedule(ctx, pkg.ID); err != nil {
		t.Fatal(err)
	}
	if err := svc.UpsertPartsStock(ctx, mroapp.PartsStock{PN: "new-part", Qty: 2}); err != nil {
		t.Fatal(err)
	}
	if err := svc.VerifyPublication(ctx, pkg.ID); !errors.Is(err, mroapp.ErrConflict) {
		t.Fatalf("changed inventory replay=%v", err)
	}
	rows, err := svc.ListWorkPackages(ctx)
	if err != nil || rows[0].EvidenceState != "stale" {
		t.Fatalf("stale=%+v %v", rows, err)
	}
	if err := svc.UpsertDueItem(ctx, mroapp.DueItem{ScopeID: "B-1", Kind: "CAL", DueAt: "2098-12-31"}); err != nil {
		t.Fatal(err)
	}
	if _, err = svc.PublishSchedule(ctx, pkg.ID); !errors.Is(err, mroapp.ErrConstraints) {
		t.Fatalf("future deadline missed=%v", err)
	}
}
func TestMROUpgradeKitSnapshotCycleAndQuarantinedTool(t *testing.T) {
	ctx, s, svc := newOpsService(t)
	kit := mroapp.Kit{ID: ulid.Make().String(), Name: "test-kit"}
	if err := svc.UpsertKit(ctx, kit, []mroapp.KitItem{{PN: "old", Required: 1}, {PN: "remove", Required: 2}}); err != nil {
		t.Fatal(err)
	}
	if err := svc.UpsertKit(ctx, kit, []mroapp.KitItem{{PN: "new", Required: 1}}); err != nil {
		t.Fatal(err)
	}
	items, err := s.ListKitItems(ctx)
	if err != nil || len(items) != 1 || items[0].PN != "new" {
		t.Fatalf("old items survived=%+v %v", items, err)
	}
	if err = svc.UpsertKit(ctx, kit, []mroapp.KitItem{{PN: "bad", Required: 1}, {PN: "bad", Required: 2}}); !errors.Is(err, mroapp.ErrPayloadInvalid) {
		t.Fatalf("duplicate snapshot=%v", err)
	}
	items, err = s.ListKitItems(ctx)
	if err != nil || len(items) != 1 || items[0].PN != "new" {
		t.Fatalf("failed snapshot lost old items=%+v %v", items, err)
	}
	a := mroapp.ChemLot{ID: ulid.Make().String(), LotNo: "a"}
	b := mroapp.ChemLot{ID: ulid.Make().String(), LotNo: "b", ParentLotID: a.ID}
	if err = svc.UpsertChemLot(ctx, a); err != nil {
		t.Fatal(err)
	}
	if err = svc.UpsertChemLot(ctx, b); err != nil {
		t.Fatal(err)
	}
	a.ParentLotID = b.ID
	if err = svc.UpsertChemLot(ctx, a); !errors.Is(err, mroapp.ErrPayloadInvalid) {
		t.Fatalf("cycle=%v", err)
	}
	id := ulid.Make().String()
	if err = svc.UpsertTool(ctx, mroapp.Tool{ID: id, ToolNo: "quarantine", Status: "quarantined", CalibDue: "2099-01-01"}); err != nil {
		t.Fatal(err)
	}
	if err = svc.ReturnTool(ctx, id); !errors.Is(err, mroapp.ErrCheckoutBlocked) {
		t.Fatalf("quarantine removed=%v", err)
	}
	tools, err := s.ListTools(ctx)
	if err != nil || tools[0].Status != "quarantined" {
		t.Fatalf("tool=%+v %v", tools, err)
	}
}
func TestMROUpgradeRecordCapacityFailsWithoutTruncation(t *testing.T) {
	ctx, s, svc := newOpsService(t)
	_, err := s.db.Exec(`WITH RECURSIVE n(v) AS (SELECT 1 UNION ALL SELECT v+1 FROM n WHERE v<10001) INSERT INTO mro_parts_stock(pn,qty,source) SELECT 'pn-'||v,1,'test' FROM n`)
	if err != nil {
		t.Fatal(err)
	}
	rows, _, err := svc.ListParts(ctx, "")
	if !errors.Is(err, mroapp.ErrCapacity) || len(rows) != 0 {
		t.Fatalf("partial capacity success=%d %v", len(rows), err)
	}
}

func TestMROUpgradePublicationChecksActualBytesAndDocumentVersion(t *testing.T) {
	ctx, s, svc, pkg, doc := mroPlanningFixture(t)
	if _, err := svc.PublishSchedule(ctx, pkg.ID); err != nil {
		t.Fatal(err)
	}
	var path string
	if err := s.db.QueryRow(`SELECT content_ref FROM kb_documents WHERE document_id=?`, doc).Scan(&path); err != nil {
		t.Fatal(err)
	}
	// The watcher has not updated index_state or source state yet.
	if err := os.WriteFile(path, []byte("changed manual bytes"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := svc.VerifyPublication(ctx, pkg.ID); !errors.Is(err, mroapp.ErrConstraints) {
		t.Fatalf("unchecked source bytes replay=%v", err)
	}
	rows, err := svc.ListWorkPackages(ctx)
	if err != nil || rows[0].EvidenceState != "blocked" {
		t.Fatalf("byte staleness not displayed=%+v %v", rows, err)
	}
	if mroCount(t, s, "mro_ops_todos") != 2 {
		t.Fatal("stale source created more todos")
	}
	ctx, s, svc, pkg, doc = mroPlanningFixture(t)
	proof, err := s.PrepareMROEvidence(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.db.Exec(`INSERT INTO kb_documents(document_id,collection_id,version,media_type,content_ref,sha256,source_locator,index_state,created_at) SELECT document_id,collection_id,version+1,media_type,content_ref,sha256,source_locator,index_state,created_at FROM kb_documents WHERE document_id=?`, doc); err != nil {
		t.Fatal(err)
	}
	if _, err = svc.PublishSchedule(proof, pkg.ID); !errors.Is(err, mroapp.ErrConstraints) {
		t.Fatalf("captured proof authorized newer document=%v", err)
	}
}
