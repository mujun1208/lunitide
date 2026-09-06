package app

import (
	"context"
	"path/filepath"
	"testing"

	storage "github.com/lunitide/lunitide/internal/storage/sqlite"
)

func TestSystemHealthReflectsLiveStorage(t *testing.T) {
	store, err := storage.OpenTemplated(context.Background(), filepath.Join(t.TempDir(), "health.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	e := NewEngine(nil, "test")
	e.SetStorageReadiness(store)
	request := validRequest("system.health", `{}`)
	if r := e.Handle(context.Background(), request); !r.OK {
		t.Fatalf("healthy DB: %#v", r)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	if r := e.Handle(context.Background(), request); r.OK || r.Error.Code != "STORAGE_UNAVAILABLE" {
		t.Fatalf("closed database reported ready: %#v", r)
	}
}
