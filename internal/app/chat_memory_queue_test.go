package app

import (
	"context"
	"testing"
	"time"

	"github.com/oklog/ulid/v2"

	"github.com/lunitide/lunitide/internal/domain/m8core"
)

func TestMemoryQueueRestart(t *testing.T) {
	mem, ops, _ := openAppMemory(t)
	e := NewEngine(nil, "test")
	e.SetM8MemoryServices(mem)
	e.SetMemoryOpsService(ops)
	ctx := context.Background()
	messageID := ulid.Make().String()
	e.persistCaptureJob(ulid.Make().String(), "以后回答默认使用中文", "好的", messageID, false)
	n, err := ops.CountActiveCaptureJobs(ctx)
	if err != nil || n != 1 {
		t.Fatalf("enqueued=%d err=%v", n, err)
	}
	first, ok, err := ops.ClaimCaptureJob(ctx, "owner-a", time.Millisecond)
	if err != nil || !ok {
		t.Fatalf("claim %v %v", ok, err)
	}
	time.Sleep(5 * time.Millisecond)
	if _, err := ops.ReclaimExpiredCaptureJobs(ctx, time.Now()); err != nil {
		t.Fatal(err)
	}
	second, ok, err := ops.ClaimCaptureJob(ctx, "owner-b", time.Second)
	if err != nil || !ok || second.Fence <= first.Fence {
		t.Fatalf("restart claim fence=%d first=%d ok=%v err=%v", second.Fence, first.Fence, ok, err)
	}
	if err := ops.CompleteCaptureJob(ctx, first.JobID, first.Fence, ""); err != nil {
		t.Fatal(err)
	}
	if err := e.runCaptureJob(ctx, second); err != nil {
		t.Fatal(err)
	}
	if err := ops.CompleteCaptureJob(ctx, second.JobID, second.Fence, ""); err != nil {
		t.Fatal(err)
	}
	n, err = ops.CountActiveCaptureJobs(ctx)
	if err != nil || n != 0 {
		t.Fatalf("active after complete=%d err=%v", n, err)
	}
}

func TestMemoryQueueHighWatermarkDrains(t *testing.T) {
	_, ops, _ := openAppMemory(t)
	ctx := context.Background()
	for i := 0; i < 3; i++ {
		ok, err := ops.EnqueueCaptureJob(ctx, m8core.MemoryCaptureJob{
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
			t.Fatalf("expected enqueue %d", i)
		}
		if i == 2 && ok {
			t.Fatal("watermark ignored")
		}
	}
	n, err := ops.CountActiveCaptureJobs(ctx)
	if err != nil || n != 2 {
		t.Fatalf("active=%d err=%v", n, err)
	}
	cur, err := ops.CaptureCursor(ctx, "local-user")
	if err != nil || cur.SourceMessageID == "" {
		t.Fatalf("cursor %+v err=%v", cur, err)
	}
}
