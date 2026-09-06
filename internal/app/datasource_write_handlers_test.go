package app

import (
	"context"
	"encoding/json"
	"sync/atomic"
	"testing"

	"github.com/lunitide/lunitide/internal/datasourceapp"
)

func TestDatasourceWriteBridgeRequiresConfirmationAndPreservesReceipt(t *testing.T) {
	e, svc := datasourceEngine(t)
	ctx := context.Background()
	row, err := svc.Create(ctx, datasourceapp.CreateInput{Name: "write", Kind: "postgres", DSN: "postgres://fixture:readonly@127.0.0.1/db?sslmode=disable"})
	if err != nil {
		t.Fatal(err)
	}
	if err = svc.Probe(ctx, row.ID); err != nil {
		t.Fatal(err)
	}
	var writes atomic.Int32
	svc.SetWriteQuerier(func(context.Context, string, string, string, []any, int) ([]string, [][]any, bool, error) {
		writes.Add(1)
		return []string{"rows_affected"}, [][]any{{1}}, false, nil
	})
	request := validRequest("datasource.write.prepare", `{"connectionId":"`+row.ID+`","sql":"DELETE FROM stock WHERE id=1"}`)
	if got := e.Handle(ctx, request); got.OK {
		t.Fatal("missing idempotency accepted")
	}
	request.IdempotencyKey = "write-bridge"
	prepared := e.Handle(ctx, request)
	if !prepared.OK {
		t.Fatalf("prepare: %+v", prepared.Error)
	}
	raw, _ := json.Marshal(prepared.Payload)
	var op datasourceapp.WriteOperation
	if err = json.Unmarshal(raw, &op); err != nil {
		t.Fatal(err)
	}
	if writes.Load() != 0 {
		t.Fatal("prepare performed write")
	}
	commit := validRequest("datasource.write.commit", `{"id":"`+op.ID+`","digest":"`+op.Digest+`"}`)
	commit.IdempotencyKey = "commit"
	for range 2 {
		if got := e.Handle(ctx, commit); !got.OK {
			t.Fatalf("commit: %+v", got.Error)
		}
	}
	if writes.Load() != 1 {
		t.Fatalf("wrote %d times", writes.Load())
	}
	if got := e.Handle(ctx, validRequest("datasource.write.get", `{"id":"`+op.ID+`"}`)); !got.OK {
		t.Fatalf("get: %+v", got.Error)
	}
}
