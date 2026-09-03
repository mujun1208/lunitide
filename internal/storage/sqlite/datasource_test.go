package sqlite

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/oklog/ulid/v2"

	"github.com/lunitide/lunitide/internal/datasourceapp"
)

func TestDBConnectionListDisableAndBindingUniqueness(t *testing.T) {
	ctx := context.Background()
	store, err := OpenTemplated(ctx, filepath.Join(t.TempDir(), "ds.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	now := time.Now().UTC().Format(time.RFC3339Nano)
	id := ulid.Make().String()
	dsn := "postgres://ro:s3cret@10.0.0.8:5432/ops"
	row := datasourceapp.Connection{
		ID: id, Name: "航司只读副本", Kind: "postgres", DSNSecretRef: "dbconn:" + id,
		State: "active", CreatedAt: now, CreatedBy: "local-user",
	}
	if err := store.PutConnection(ctx, row); err != nil {
		t.Fatal(err)
	}
	items, err := store.ListConnections(ctx)
	if err != nil || len(items) != 1 {
		t.Fatalf("list = %+v err=%v", items, err)
	}
	if items[0].DSNSecretRef == "" || items[0].DSNSecretRef == dsn {
		t.Fatalf("secret ref = %q", items[0].DSNSecretRef)
	}
	if strings.Contains(items[0].Name+items[0].DSNSecretRef, "s3cret") {
		t.Fatal("list stored plaintext dsn")
	}
	if err := store.SetConnectionVerified(ctx, id, now); err != nil {
		t.Fatal(err)
	}
	if err := store.DisableConnection(ctx, id); err != nil {
		t.Fatal(err)
	}
	got, err := store.GetConnection(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if got.State != "disabled" || got.ReadOnlyVerifiedAt != nil {
		t.Fatalf("disabled = %+v", got)
	}
	dup := row
	dup.ID = ulid.Make().String()
	dup.DSNSecretRef = "dbconn:" + dup.ID
	if err := store.PutConnection(ctx, dup); !errors.Is(err, datasourceapp.ErrDuplicateName) {
		t.Fatalf("dup name = %v", err)
	}
	active := datasourceapp.Connection{
		ID: ulid.Make().String(), Name: "实验库", Kind: "mysql", DSNSecretRef: "dbconn:x",
		State: "active", CreatedAt: now, CreatedBy: "local-user",
	}
	active.DSNSecretRef = "dbconn:" + active.ID
	if err := store.PutConnection(ctx, active); err != nil {
		t.Fatal(err)
	}
	if err := store.SetConnectionVerified(ctx, active.ID, now); err != nil {
		t.Fatal(err)
	}
	bind := datasourceapp.Binding{
		BindingID: ulid.Make().String(), OwnerType: "mro", OwnerID: datasourceapp.WorkbenchOwnerID,
		ConnectionID: active.ID, Purpose: "stock", TableMapJSON: `{"schema":"inv","table":"stock"}`, CreatedAt: now,
	}
	if err := store.PutBinding(ctx, bind); err != nil {
		t.Fatal(err)
	}
	again := bind
	again.BindingID = ulid.Make().String()
	if err := store.PutBinding(ctx, again); !errors.Is(err, datasourceapp.ErrDuplicateBinding) {
		t.Fatalf("dup bind = %v", err)
	}
}
