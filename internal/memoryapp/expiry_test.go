package memoryapp

import (
	"context"
	"errors"
	"github.com/lunitide/lunitide/internal/domain/memory"
	"testing"
	"time"
)

func TestMemoryExpiryEqualityAndMissingDependencies(t *testing.T) {
	ctx := context.Background()
	now := memNow()
	due := makeMem("due", 0.9)
	due.ExpiresAt = &now
	readOnly := New(&mockMemReader{mem: due}, nil)
	readOnly.SetClock(fixedClock{now})
	if _, err := readOnly.Get(ctx, "due"); !errors.Is(err, ErrMemoryNotFound) {
		t.Fatal(err)
	}
	for _, service := range []*Service{nil, New(nil, nil), New(nil, &mockMemWriter{})} {
		if err := service.UpdateContent(ctx, "x", "content"); err == nil {
			t.Fatal("missing reader accepted update")
		}
		if err := service.Delete(ctx, "x"); err == nil {
			t.Fatal("missing reader accepted delete")
		}
	}
	future := *due
	future.ID = "future"
	expires := now.Add(time.Nanosecond)
	future.ExpiresAt = &expires
	service := newSvc(&mockMemReader{mem: due, memList: []memory.Memory{*due, future}}, &mockMemWriter{})
	rows, err := service.ListByProject(ctx, due.ProjectID, "")
	if err != nil || len(rows) != 1 || rows[0].ID != "future" {
		t.Fatalf("list expiry %+v %v", rows, err)
	}
	if err = service.UpdateContent(ctx, "due", "updated"); !errors.Is(err, ErrMemoryNotFound) {
		t.Fatalf("expired update %v", err)
	}
	service = newSvc(&mockMemReader{memList: []memory.Memory{*due, future}}, &mockMemWriter{})
	rows, err = service.Search(ctx, due.ProjectID, "sky")
	if err != nil || len(rows) != 1 || rows[0].ID != "future" {
		t.Fatalf("search expiry %+v %v", rows, err)
	}
}
func TestMemoryPurgePropagatesWriteFailureAndIncompleteLegacyScan(t *testing.T) {
	now := memNow()
	expired := *makeMem("expired", .9)
	expired.ExpiresAt = &now
	failure := errors.New("fixture delete failed")
	service := newSvc(&mockMemReader{memList: []memory.Memory{expired}}, &mockMemWriter{err: failure})
	n, err := service.PurgeExpired(context.Background(), expired.ProjectID)
	if n != 0 || !errors.Is(err, failure) {
		t.Fatalf("swallowed failure %d %v", n, err)
	}
	service = New(&mockMemReader{}, nil)
	if _, err = service.PurgeExpired(context.Background(), expired.ProjectID); err == nil {
		t.Fatal("missing writer reported success")
	}
	service = newSvc(&mockMemReader{memList: make([]memory.Memory, 100)}, &mockMemWriter{})
	if _, err = service.PurgeExpired(context.Background(), expired.ProjectID); !errors.Is(err, ErrPurgeIncomplete) {
		t.Fatal("legacy partial scan reported complete")
	}
}
