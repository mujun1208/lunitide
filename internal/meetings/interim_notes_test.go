package meetings_test

import (
	"context"
	"strings"
	"testing"

	"github.com/lunitide/lunitide/internal/meetings"
)

func stoppedMeeting(t *testing.T, svc *meetings.Service, line string) string {
	t.Helper()
	ctx := context.Background()
	started, err := svc.Start(ctx, "上线评审", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Append(ctx, started.MeetingID, line, 0); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Stop(ctx, started.MeetingID); err != nil {
		t.Fatal(err)
	}
	return started.MeetingID
}

// The reason streaming exists: a finished topic has to be readable while the
// model is still writing. And the interim write must not break the final save,
// which swaps on updatedAt — that was the whole risk of publishing early.
func TestSummarizePublishesPartialNotesAndStillSavesFinal(t *testing.T) {
	svc := testMeetings(t)
	var midStatus meetings.Status
	midSummary := ""
	svc.SetCompleter(func(ctx context.Context, title, transcript string) (meetings.Notes, error) {
		meetings.InterimPublisherFrom(ctx)(meetings.Notes{Title: title, Summary: "背景：先对齐范围。\n\n## 范围\n\n- 只做浏览器包\n"})
		mid, err := svc.Metadata(ctx, meetings.MeetingIDFrom(ctx))
		if err != nil {
			return meetings.Notes{}, err
		}
		midStatus, midSummary = mid.Status, mid.Summary
		return meetings.Notes{Title: "上线评审", Summary: "背景：先对齐范围。\n\n## 范围\n\n- 只做浏览器包\n\n## 排期\n\n- 下周三上线\n", Actions: "- 张三 补文档"}, nil
	})
	id := stoppedMeeting(t, svc, "第一步先对齐范围")
	got, err := svc.Summarize(context.Background(), id)
	if err != nil {
		t.Fatalf("summarize failed after an interim write: %v", err)
	}
	if !strings.Contains(midSummary, "只做浏览器包") {
		t.Fatalf("partial notes never reached the store: %q", midSummary)
	}
	if midStatus != meetings.StatusSummarizing {
		// A poll that sees 'ready' stops polling and freezes on the partial doc.
		t.Fatalf("interim write flipped status to %q", midStatus)
	}
	if got.Status != meetings.StatusReady {
		t.Fatalf("final status = %q (%v)", got.Status, got.SummaryError)
	}
	if !strings.Contains(got.Summary, "下周三上线") || !strings.Contains(got.Actions, "补文档") {
		t.Fatalf("final notes lost content:\n%s\n%s", got.Summary, got.Actions)
	}
}

// A long meeting is summarized segment by segment. A segment only sees its own
// slice, so publishing from inside one would replace the segments already on
// screen with less text — the reader watches the document shrink. Segments must
// publish the accumulated stitch instead.
func TestSummarizeLongPublishesGrowingStitchNotSegments(t *testing.T) {
	long := strings.Repeat("第一步先对齐范围，然后排期。", meetings.SummarizeChunkRunes/8)
	var published []string
	calls := 0
	complete := func(ctx context.Context, title, transcript string) (meetings.Notes, error) {
		calls++
		// A segment that publishes is a bug; record whatever reaches the sink so
		// the assertion below can tell shrink from growth either way.
		meetings.InterimPublisherFrom(ctx)(meetings.Notes{Summary: "段内流式片段"})
		return meetings.Notes{Title: "上线评审", Summary: "## 段" + strings.Repeat("x", calls) + "\n\n- 要点\n"}, nil
	}
	ctx := meetings.WithInterimPublisher(context.Background(), func(n meetings.Notes) {
		published = append(published, n.Summary)
	})
	if _, err := meetings.SummarizeLong(ctx, complete, "上线评审", long); err != nil {
		t.Fatal(err)
	}
	if len(published) < 2 {
		t.Fatalf("segmented summarize published %d times, expected one per segment: %#v", len(published), published)
	}
	for i, summary := range published {
		if summary == "段内流式片段" {
			t.Fatalf("publish %d came from inside a segment: %q", i, summary)
		}
		if i > 0 && len(summary) < len(published[i-1]) {
			t.Fatalf("publish %d shrank the document:\n%s\n->\n%s", i, published[i-1], summary)
		}
	}
}

// Parsing runs every few hundred milliseconds; without a floor every delta would
// become a database write while the meeting is still being transcribed.
func TestInterimNotesThrottleRapidPublishes(t *testing.T) {
	svc := testMeetings(t)
	var firstRevision, secondRevision int64
	svc.SetCompleter(func(ctx context.Context, title, transcript string) (meetings.Notes, error) {
		publish := meetings.InterimPublisherFrom(ctx)
		id := meetings.MeetingIDFrom(ctx)
		publish(meetings.Notes{Summary: "背景：一。\n\n## 甲\n\n- 一\n"})
		first, err := svc.Metadata(ctx, id)
		if err != nil {
			return meetings.Notes{}, err
		}
		firstRevision = first.Revision
		publish(meetings.Notes{Summary: "背景：一。\n\n## 甲\n\n- 一\n\n## 乙\n\n- 二\n"})
		second, err := svc.Metadata(ctx, id)
		if err != nil {
			return meetings.Notes{}, err
		}
		secondRevision = second.Revision
		return meetings.Notes{Title: "会", Summary: "背景：一。\n\n## 甲\n\n- 一\n\n## 乙\n\n- 二\n"}, nil
	})
	id := stoppedMeeting(t, svc, "一句话")
	if _, err := svc.Summarize(context.Background(), id); err != nil {
		t.Fatal(err)
	}
	if firstRevision == 0 {
		t.Fatal("first interim publish did not land")
	}
	if secondRevision != firstRevision {
		t.Fatalf("second publish wrote again within the throttle window: %d -> %d", firstRevision, secondRevision)
	}
}
