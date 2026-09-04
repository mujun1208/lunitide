package app

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/oklog/ulid/v2"

	"github.com/lunitide/lunitide/internal/mroapp"
	storage "github.com/lunitide/lunitide/internal/storage/sqlite"
)

func TestMRODueListMarksMissingUtilization(t *testing.T) {
	store, err := storage.OpenTemplated(context.Background(), filepath.Join(t.TempDir(), "due.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	svc := mroapp.New(store)
	if err := svc.UpsertDueItem(context.Background(), mroapp.DueItem{
		ID: ulid.Make().String(), ScopeID: "B-1", Kind: "FH", LimitValue: 100, UsedMissing: true,
	}); err != nil {
		t.Fatal(err)
	}
	e := NewEngine(nil, "test")
	e.SetMROService(svc)
	resp := e.Handle(context.Background(), mroMutationRequest("mro.due.list", `{}`, ""))
	if !resp.OK {
		t.Fatalf("due list: %+v", resp)
	}
	var out struct {
		Items []struct {
			Label       string `json:"label"`
			UsedMissing bool   `json:"usedMissing"`
		} `json:"items"`
	}
	raw, _ := json.Marshal(resp.Payload)
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatal(err)
	}
	if len(out.Items) != 1 || !out.Items[0].UsedMissing || out.Items[0].Label != "未录入" {
		t.Fatalf("items = %+v", out.Items)
	}
}

func TestMROToolCheckoutRejectsOverdue(t *testing.T) {
	store, err := storage.OpenTemplated(context.Background(), filepath.Join(t.TempDir(), "tool.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	svc := mroapp.New(store)
	id := ulid.Make().String()
	if err := svc.UpsertTool(context.Background(), mroapp.Tool{ID: id, ToolNo: "TW-1", CalibDue: "2020-01-01"}); err != nil {
		t.Fatal(err)
	}
	e := NewEngine(nil, "test")
	e.SetMROService(svc)
	payload := `{"toolId":"` + id + `","holder":"tech-1"}`
	resp := e.Handle(context.Background(), mroMutationRequest("mro.tool.checkout", payload, "idem-checkout"))
	if resp.OK {
		t.Fatalf("overdue checkout must fail, got %+v", resp.Payload)
	}
}
