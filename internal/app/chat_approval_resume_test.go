package app

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/lunitide/lunitide/internal/toolruntime"
)

func TestApprovalWaitResumesTheSameCall(t *testing.T) {
	e := NewEngine(nil, "test")
	if !e.sameStreamApproval {
		t.Fatal("a turn must be able to resume after approval")
	}
	done := make(chan approvalResume, 1)
	go func() {
		time.Sleep(30 * time.Millisecond)
		if !e.deliverApprovalWait("sess", "call-1", true, toolruntime.Result{Output: "ok:true"}, nil) {
			t.Error("approval had no waiting stream")
			return
		}
	}()
	resume, err := e.waitApprovalResume(context.Background(), "sess", "call-1")
	if err != nil {
		t.Fatal(err)
	}
	done <- resume
	if !resume.approved || resume.result.Output != "ok:true" {
		t.Fatalf("resume = %+v", resume)
	}
}

func TestApprovalWaitStopsWhenTheTurnIsCancelled(t *testing.T) {
	e := NewEngine(nil, "test")
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(30 * time.Millisecond)
		cancel()
	}()
	_, err := e.waitApprovalResume(ctx, "sess", "call-2")
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v", err)
	}
}
