package sqlite

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/oklog/ulid/v2"

	"github.com/lunitide/lunitide/internal/mroapp"
)

func TestMROAircraftUniqueTailAndManualParts(t *testing.T) {
	ctx := context.Background()
	store, err := OpenTemplated(ctx, filepath.Join(t.TempDir(), "mro.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	now := time.Now().UTC().Format(time.RFC3339Nano)
	first := mroapp.Aircraft{AircraftID: ulid.Make().String(), TailNo: "B-0000", Model: "A320", CreatedAt: now}
	if err := store.UpsertAircraft(ctx, first); err != nil {
		t.Fatal(err)
	}
	dup := mroapp.Aircraft{AircraftID: ulid.Make().String(), TailNo: "B-0000", Model: "A321", CreatedAt: now}
	if err := store.UpsertAircraft(ctx, dup); !errors.Is(err, mroapp.ErrDuplicateTail) {
		t.Fatalf("err = %v", err)
	}
	manual := mroapp.Manual{
		ManualID: ulid.Make().String(), Title: "AMM", DocType: "AMM", Revision: "42",
		Status: "controlled", ATA: "32", CreatedAt: now,
	}
	if err := store.RegisterManual(ctx, manual, []mroapp.ManualDocInput{
		{DocumentID: ulid.Make().String(), PartNo: 1},
		{DocumentID: ulid.Make().String(), PartNo: 2},
	}); err != nil {
		t.Fatal(err)
	}
	items, err := store.ListManuals(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].SectionCount != 2 {
		t.Fatalf("items = %+v", items)
	}
}
