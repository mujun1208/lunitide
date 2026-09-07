package app

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lunitide/lunitide/internal/bridge"
	"github.com/lunitide/lunitide/internal/ipc"
	"github.com/lunitide/lunitide/internal/mroapp"
	storage "github.com/lunitide/lunitide/internal/storage/sqlite"
	"github.com/oklog/ulid/v2"
)

func mroPageFixture(t *testing.T) (*Engine, *storage.Store) {
	t.Helper()
	s, err := storage.OpenTemplated(context.Background(), filepath.Join(t.TempDir(), "mro-page.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	e := NewEngine(nil, "test")
	e.SetMROService(mroapp.New(s))
	return e, s
}

type mroTestPage struct {
	Items           []json.RawMessage `json:"items"`
	Alternates      []json.RawMessage `json:"alternates"`
	NextCursor      string            `json:"nextCursor"`
	ContinuedFields []string          `json:"continuedFields"`
}

func decodeMROTestPage(t *testing.T, response bridge.Response) mroTestPage {
	t.Helper()
	if !response.OK {
		t.Fatalf("page failed: %+v", response.Error)
	}
	raw, err := json.Marshal(response)
	if err != nil || len(raw) > mroPageBytes {
		t.Fatalf("page bytes=%d error=%v", len(raw), err)
	}
	if err := ipc.WriteFrame(io.Discard, raw); err != nil {
		t.Fatalf("real IPC cannot deliver page: %v", err)
	}
	var out mroTestPage
	raw, err = json.Marshal(response.Payload)
	if err != nil || json.Unmarshal(raw, &out) != nil {
		t.Fatal("invalid page DTO")
	}
	if len(out.Items) > 100 || len(out.Alternates) > 100 {
		t.Fatal("page row budget exceeded")
	}
	return out
}

func mroPageRequest(method, cursor string, filter map[string]any) bridge.Request {
	if filter == nil {
		filter = map[string]any{}
	}
	if cursor != "" {
		filter["cursor"] = cursor
	}
	raw, _ := json.Marshal(filter)
	request := mroMutationRequest(method, string(raw), "")
	// Match createMroBridge's actual read deadline. The mutation fixture's
	// 3-second budget makes the 10,000-row completeness test time out under
	// race instrumentation before the product's 8-second deadline is reached.
	request.DeadlineMS = 8_000
	return request
}

func TestMROPublicToolPagesPreserve10000LegalLongRows(t *testing.T) {
	e, s := mroPageFixture(t)
	ctx := context.Background()
	err := s.TransactMRO(ctx, func(txCtx context.Context) error {
		for i := range 10000 {
			if err := s.UpsertTool(txCtx, mroapp.Tool{ID: ulid.Make().String(), ToolNo: fmt.Sprintf("%05d", i) + strings.Repeat("&", 59), SN: strings.Repeat("&", 64), Location: strings.Repeat("&", 64), CalibDue: "2099-01-01", Status: "ready", UpdatedAt: "2026-09-06T00:00:00Z"}); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	cursor, pages := "", 0
	for {
		page := decodeMROTestPage(t, e.Handle(ctx, mroPageRequest("mro.tool.list", cursor, nil)))
		pages++
		for _, raw := range page.Items {
			var row struct{ ID, ToolNo, SN, Location string }
			if err := json.Unmarshal(raw, &row); err != nil {
				t.Fatal(err)
			}
			if seen[row.ID] || row.SN != strings.Repeat("&", 64) || row.Location != strings.Repeat("&", 64) || len(row.ToolNo) != 64 {
				t.Fatal("duplicate or truncated tool")
			}
			seen[row.ID] = true
		}
		if page.NextCursor == "" {
			break
		}
		if page.NextCursor == cursor || pages > 100 {
			t.Fatal("pagination made no progress")
		}
		cursor = page.NextCursor
	}
	if len(seen) != 10000 || pages != 100 {
		t.Fatalf("incomplete tool list: %d rows / %d pages", len(seen), pages)
	}
	t.Logf("10000 complete legal long tool rows; %d real IPC pages", pages)
}

func TestMROPartsPagesBindScopeFiltersAndChangedContent(t *testing.T) {
	e, s := mroPageFixture(t)
	ctx := context.Background()
	if err := s.TransactMRO(ctx, func(txCtx context.Context) error {
		for i := range 223 {
			if err := s.UpsertPartsStock(txCtx, mroapp.PartsStock{PN: fmt.Sprintf("%05d", i) + strings.Repeat("&", 59), Qty: 1, Source: strings.Repeat("&", 32)}); err != nil {
				return err
			}
		}
		for i := range 257 {
			if err := s.UpsertAlternate(txCtx, mroapp.Alternate{PNFrom: fmt.Sprintf("from-%d", i), PNTo: fmt.Sprintf("to-%d", i), Effectivity: "A", CertOK: true}); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	first := decodeMROTestPage(t, e.Handle(ctx, mroPageRequest("mro.parts.stock.list", "", map[string]any{"config": "A"})))
	if first.NextCursor == "" {
		t.Fatal("missing cursor")
	}
	items, alternates := len(first.Items), len(first.Alternates)
	for cursor := first.NextCursor; cursor != ""; {
		page := decodeMROTestPage(t, e.Handle(ctx, mroPageRequest("mro.parts.stock.list", cursor, map[string]any{"config": "A"})))
		items += len(page.Items)
		alternates += len(page.Alternates)
		cursor = page.NextCursor
	}
	if items != 223 || alternates != 257 {
		t.Fatalf("lost unequal collections: %d / %d", items, alternates)
	}
	for _, request := range []bridge.Request{mroPageRequest("mro.parts.stock.list", first.NextCursor, map[string]any{"config": "B"}), mroPageRequest("mro.tool.list", first.NextCursor, nil)} {
		if response := e.Handle(ctx, request); response.OK || response.Error.Code != "MRO_PAGE_CHANGED" {
			t.Fatalf("foreign cursor accepted: %+v", response)
		}
	}
	otherScope := mroapp.WithScope(ctx, ulid.Make().String())
	if response := handleMROPartsStockList(e, otherScope, mroPageRequest("mro.parts.stock.list", first.NextCursor, map[string]any{"config": "A"})); response.OK || response.Error.Code != "MRO_PAGE_CHANGED" {
		t.Fatalf("other organization cursor accepted: %+v", response)
	}
	if err := s.UpsertPartsStock(ctx, mroapp.PartsStock{PN: "new", Qty: 3, Source: "local"}); err != nil {
		t.Fatal(err)
	}
	if response := e.Handle(ctx, mroPageRequest("mro.parts.stock.list", first.NextCursor, map[string]any{"config": "A"})); response.OK || response.Error.Code != "MRO_PAGE_CHANGED" {
		t.Fatalf("stale content cursor accepted: %+v", response)
	}
}

func TestMROComponentPagesPreserveLongParentHistory(t *testing.T) {
	e, s := mroPageFixture(t)
	ctx := context.Background()
	id := ulid.Make().String()
	if err := s.TransactMRO(ctx, func(txCtx context.Context) error {
		if err := s.UpsertComponent(txCtx, mroapp.Component{ID: id, SN: "serial", PN: "part", CreatedAt: "2026-09-06T00:00:00Z"}); err != nil {
			return err
		}
		for i := range 1500 {
			if err := s.InsertLifeEvent(txCtx, mroapp.LifeEvent{ID: ulid.Make().String(), ComponentID: id, Kind: "repair", OccurredAt: "2026-09-06", Note: fmt.Sprintf("%04d", i) + strings.Repeat("&", 508)}); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	cursor, pages := "", 0
	for {
		page := decodeMROTestPage(t, e.Handle(ctx, mroPageRequest("mro.component.list", cursor, nil)))
		if len(page.Items) != 1 {
			t.Fatalf("lost parent: %d", len(page.Items))
		}
		if pages > 0 && (len(page.ContinuedFields) != 1 || page.ContinuedFields[0] != "items") {
			t.Fatal("missing parent continuation marker")
		}
		var row struct {
			ID     string                  `json:"id"`
			Events []struct{ Note string } `json:"events"`
		}
		if err := json.Unmarshal(page.Items[0], &row); err != nil || row.ID != id || len(row.Events) > 100 {
			t.Fatalf("parent fragment invalid: %+v %v", row, err)
		}
		for _, event := range row.Events {
			if len(event.Note) != 512 || seen[event.Note] {
				t.Fatal("event truncated or repeated")
			}
			seen[event.Note] = true
		}
		pages++
		if page.NextCursor == "" {
			break
		}
		cursor = page.NextCursor
	}
	if len(seen) != 1500 || pages != 15 {
		t.Fatalf("history incomplete: %d / %d", len(seen), pages)
	}
}

func TestMRODueMutationReceiptContinuesThroughReadOnlyList(t *testing.T) {
	e, s := mroPageFixture(t)
	ctx := context.Background()
	if err := s.TransactMRO(ctx, func(txCtx context.Context) error {
		for i := range 205 {
			if err := s.UpsertDueItem(txCtx, mroapp.DueItem{ID: ulid.Make().String(), ScopeID: "tail", Kind: "FH", LimitValue: 100, UsedMissing: true, UpdatedAt: fmt.Sprintf("2026-09-06T00:00:%02dZ", i%60)}); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	first := decodeMROTestPage(t, e.Handle(ctx, mroMutationRequest("mro.util.record", `{"scopeId":"tail","hours":3}`, "util-page")))
	count := len(first.Items)
	for cursor := first.NextCursor; cursor != ""; {
		page := decodeMROTestPage(t, e.Handle(ctx, mroPageRequest("mro.due.list", cursor, nil)))
		count += len(page.Items)
		cursor = page.NextCursor
	}
	events, err := s.ListUtilizationEvents(ctx)
	if err != nil || len(events) != 1 || count != 205 {
		t.Fatalf("reading mutation pages re-executed/lost results: rows=%d events=%d err=%v", count, len(events), err)
	}
}

func TestMROPirepByteBudgetPreservesAllLegalNotes(t *testing.T) {
	e, s := mroPageFixture(t)
	ctx := context.Background()
	note := strings.Repeat("&", 4000)
	body, err := json.Marshal(map[string]string{"note": note})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.TransactMRO(ctx, func(txCtx context.Context) error {
		for range 200 {
			if err := s.InsertPirepDraft(txCtx, mroapp.PirepDraft{ID: ulid.Make().String(), TailNo: "B-1", BodyJSON: string(body), State: "draft", CreatedAt: "2026-09-06T00:00:00Z"}); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	cursor := ""
	for {
		page := decodeMROTestPage(t, e.Handle(ctx, mroPageRequest("mro.pirep.list", cursor, nil)))
		if len(page.Items) >= 100 {
			t.Fatal("serialized byte budget did not reduce the page")
		}
		for _, raw := range page.Items {
			var row struct{ ID, Body string }
			if err := json.Unmarshal(raw, &row); err != nil {
				t.Fatal(err)
			}
			var stored map[string]string
			if err := json.Unmarshal([]byte(row.Body), &stored); err != nil || stored["note"] != note || seen[row.ID] {
				t.Fatal("note was truncated or row duplicated")
			}
			seen[row.ID] = true
		}
		if page.NextCursor == "" {
			break
		}
		if page.NextCursor == cursor {
			t.Fatal("byte budget produced a stalled cursor")
		}
		cursor = page.NextCursor
	}
	if len(seen) != 200 {
		t.Fatalf("missing reports: %d", len(seen))
	}
}

func TestMROPartsTodoUsesCompleteKitInsteadOfRendererFragment(t *testing.T) {
	e, s := mroPageFixture(t)
	ctx := context.Background()
	id := ulid.Make().String()
	if err := s.TransactMRO(ctx, func(txCtx context.Context) error {
		if err := s.UpsertKit(txCtx, mroapp.Kit{ID: id, Name: "large kit"}); err != nil {
			return err
		}
		for i := range 205 {
			if err := s.UpsertKitItem(txCtx, mroapp.KitItem{KitID: id, PN: fmt.Sprintf("part-%03d", i), Required: 2, OnHand: 0}); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	request := mroMutationRequest("mro.ops.todo.add", `{"kitId":"`+id+`","detail":"partial-invented-renderer-list"}`, "kit-page")
	for range 2 {
		response := e.Handle(ctx, request)
		if !response.OK {
			t.Fatalf("complete kit todo rejected: %+v", response.Error)
		}
		raw, err := json.Marshal(response)
		if err != nil || ipc.WriteFrame(io.Discard, raw) != nil {
			t.Fatal("todo receipt cannot be delivered")
		}
	}
	items, err := s.ListKitItems(ctx)
	if err != nil {
		t.Fatal(err)
	}
	contents, err := json.Marshal(items)
	if err != nil {
		t.Fatal(err)
	}
	todos, err := s.ListOpsTodos(ctx)
	if err != nil || len(todos) != 1 || !strings.Contains(todos[0].Detail, "205") || !strings.Contains(todos[0].Detail, fmt.Sprintf("%x", sha256.Sum256(contents))) || strings.Contains(todos[0].Detail, "partial-invented") {
		t.Fatalf("todo used a partial or fabricated shortage: %+v %v", todos, err)
	}
	if _, err := e.mro.AddPartsTodo(ctx, ulid.Make().String(), "fake"); err == nil {
		t.Fatal("nonexistent kit accepted")
	}
}
