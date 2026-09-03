package mroapp

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/oklog/ulid/v2"
)

type fakeStore struct {
	mu       sync.Mutex
	aircraft []Aircraft
	manuals  []Manual
	docs     map[string][]ManualDocInput
}

func (f *fakeStore) UpsertAircraft(_ context.Context, row Aircraft) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, item := range f.aircraft {
		if item.TailNo == row.TailNo {
			return ErrDuplicateTail
		}
	}
	f.aircraft = append(f.aircraft, row)
	return nil
}

func (f *fakeStore) ListAircraft(context.Context) ([]Aircraft, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]Aircraft, len(f.aircraft))
	copy(out, f.aircraft)
	return out, nil
}

func (f *fakeStore) RegisterManual(_ context.Context, row Manual, docs []ManualDocInput) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.docs == nil {
		f.docs = map[string][]ManualDocInput{}
	}
	f.manuals = append(f.manuals, row)
	f.docs[row.ManualID] = append([]ManualDocInput{}, docs...)
	return nil
}

func (f *fakeStore) ListManuals(context.Context) ([]Manual, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]Manual, len(f.manuals))
	copy(out, f.manuals)
	for i, item := range out {
		out[i].SectionCount = len(f.docs[item.ManualID])
	}
	return out, nil
}

func TestUpsertAircraftRejectsDuplicateTail(t *testing.T) {
	svc := New(&fakeStore{})
	ctx := context.Background()
	first, err := svc.UpsertAircraft(ctx, AircraftInput{TailNo: "B-0000", Model: "A320"})
	if err != nil {
		t.Fatal(err)
	}
	if first.TailNo != "B-0000" {
		t.Fatalf("tail = %q", first.TailNo)
	}
	_, err = svc.UpsertAircraft(ctx, AircraftInput{TailNo: "B-0000", Model: "A321"})
	if !errors.Is(err, ErrDuplicateTail) {
		t.Fatalf("err = %v", err)
	}
}

func TestRegisterManualRejectsIllegalDocType(t *testing.T) {
	svc := New(&fakeStore{})
	_, err := svc.RegisterManual(context.Background(), ManualInput{
		Title: "Bad", DocType: "XYZ", Revision: "1", Status: "controlled",
		Documents: []ManualDocInput{{DocumentID: ulid.Make().String(), PartNo: 1}},
	})
	if !errors.Is(err, ErrPayloadInvalid) {
		t.Fatalf("err = %v", err)
	}
}

func TestListManualsAggregatesSections(t *testing.T) {
	svc := New(&fakeStore{})
	ctx := context.Background()
	docA, docB := ulid.Make().String(), ulid.Make().String()
	manual, err := svc.RegisterManual(ctx, ManualInput{
		Title: "AMM 32", DocType: "AMM", Revision: "42", Status: "controlled", ATA: "32",
		Documents: []ManualDocInput{
			{DocumentID: docA, PartNo: 1},
			{DocumentID: docB, PartNo: 2},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	items, err := svc.ListManuals(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].ManualID != manual.ManualID || items[0].SectionCount != 2 {
		t.Fatalf("items = %+v", items)
	}
}
