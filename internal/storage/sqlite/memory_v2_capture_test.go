package sqlite

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/oklog/ulid/v2"

	"github.com/lunitide/lunitide/internal/domain/m8core"
)

func TestMemoryCaptureJobLeaseRestartAndFence(t *testing.T) {
	ctx := context.Background()
	store, err := Open(ctx, filepath.Join(t.TempDir(), "capture.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	job := m8core.MemoryCaptureJob{
		SubjectID:       "local-user",
		SourceMessageID: ulid.Make().String(),
		SourceRevision:  "1",
		SourceDigest:    m8core.DigestOf("以后回答默认使用中文"),
		CursorJSON:      `{"userText":"以后回答默认使用中文"}`,
	}
	ok, err := store.EnqueueMemoryCaptureJob(ctx, job, 8)
	if err != nil || !ok {
		t.Fatalf("enqueue %v %v", ok, err)
	}
	first, ok, err := store.ClaimMemoryCaptureJob(ctx, "owner-a", time.Millisecond)
	if err != nil || !ok {
		t.Fatalf("claim %v %v", ok, err)
	}
	time.Sleep(5 * time.Millisecond)
	if n, err := store.ReclaimExpiredMemoryCaptureJobs(ctx, time.Now()); err != nil || n != 1 {
		t.Fatalf("reclaim n=%d err=%v", n, err)
	}
	second, ok, err := store.ClaimMemoryCaptureJob(ctx, "owner-b", time.Second)
	if err != nil || !ok || second.Fence <= first.Fence {
		t.Fatalf("reclaim claim fence=%d first=%d ok=%v err=%v", second.Fence, first.Fence, ok, err)
	}
	if err := store.CompleteMemoryCaptureJob(ctx, first.JobID, first.Fence, ""); err != nil {
		t.Fatal(err)
	}
	var state string
	if err := store.db.QueryRow(`SELECT state FROM memory_capture_jobs WHERE job_id=?`, first.JobID).Scan(&state); err != nil {
		t.Fatal(err)
	}
	if state != "running" {
		t.Fatalf("stale fence completed job: %s", state)
	}
	if err := store.CompleteMemoryCaptureJob(ctx, second.JobID, second.Fence, ""); err != nil {
		t.Fatal(err)
	}
	if err := store.db.QueryRow(`SELECT state FROM memory_capture_jobs WHERE job_id=?`, second.JobID).Scan(&state); err != nil {
		t.Fatal(err)
	}
	if state != "succeeded" {
		t.Fatalf("state=%s", state)
	}
}

func TestMemoryQueueHighWatermarkKeepsCursor(t *testing.T) {
	ctx := context.Background()
	store, err := Open(ctx, filepath.Join(t.TempDir(), "capture-hw.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	for i := 0; i < 3; i++ {
		ok, err := store.EnqueueMemoryCaptureJob(ctx, m8core.MemoryCaptureJob{
			SubjectID:       "local-user",
			SourceMessageID: ulid.Make().String(),
			SourceRevision:  "1",
			SourceDigest:    m8core.DigestOf("job"),
			CursorJSON:      "{}",
		}, 2)
		if err != nil {
			t.Fatal(err)
		}
		if i < 2 && !ok {
			t.Fatalf("job %d should enqueue", i)
		}
		if i == 2 && ok {
			t.Fatal("high watermark still materialized")
		}
	}
	n, err := store.CountActiveMemoryCaptureJobs(ctx)
	if err != nil || n != 2 {
		t.Fatalf("active=%d err=%v", n, err)
	}
	cur, err := store.GetMemoryCaptureCursor(ctx, "local-user")
	if err != nil || cur.SourceMessageID == "" {
		t.Fatalf("cursor %+v err=%v", cur, err)
	}
}
