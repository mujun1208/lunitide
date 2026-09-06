package meetings_test

import (
	"context"
	"github.com/lunitide/lunitide/internal/meetings"
	"strings"
	"testing"
)

func TestMeetingCapabilityCancellationRejectsLateSummaryAndAllowsRetry(t *testing.T) {
	svc := testMeetings(t)
	ctx := context.Background()
	m, err := svc.Start(ctx, "周会", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = svc.Append(ctx, m.MeetingID, "对齐交付范围", 0); err != nil {
		t.Fatal(err)
	}
	if _, err = svc.Stop(ctx, m.MeetingID); err != nil {
		t.Fatal(err)
	}
	lifetime, cancel := context.WithCancel(ctx)
	defer cancel()
	released := false
	svc.SetExecutionScope(func(context.Context, string) (context.Context, func(), error) {
		return lifetime, func() { released = true }, nil
	})
	svc.SetCompleter(func(context.Context, string, string) (meetings.Notes, error) {
		cancel()
		return meetings.Notes{Summary: "不应写入的迟到摘要", Actions: "不应写入"}, nil
	})
	got, err := svc.Summarize(ctx, m.MeetingID)
	if err != nil || got.Status != meetings.StatusNeedsSummary || strings.Contains(got.Summary, "迟到") || !released {
		t.Fatalf("late summary: %#v %v released=%v", got, err, released)
	}
	svc.SetExecutionScope(nil)
	svc.SetCompleter(func(context.Context, string, string) (meetings.Notes, error) {
		return meetings.Notes{Summary: "恢复后的摘要", Actions: "继续跟进"}, nil
	})
	got, err = svc.Summarize(ctx, m.MeetingID)
	if err != nil || got.Status != meetings.StatusReady || got.Summary != "恢复后的摘要" {
		t.Fatalf("retry: %#v %v", got, err)
	}
}
