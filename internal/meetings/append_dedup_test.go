package meetings_test

import (
	"context"
	"testing"
)

func TestAppendIdenticalFinalDoesNotDuplicateSegment(t *testing.T) {
	// A recognizer that emits the same final twice — a repeated flush, or a
	// restart replaying its last result — used to skip the dedup block
	// outright, because the peel only ran when the text differed. The second
	// copy landed as its own row and the reader saw the sentence twice.
	svc := testMeetings(t)
	ctx := context.Background()
	started, err := svc.Start(ctx, "", "")
	if err != nil {
		t.Fatal(err)
	}
	const line = "今天多云，气温二十六度。"
	first, err := svc.Append(ctx, started.MeetingID, line, 0)
	if err != nil {
		t.Fatal(err)
	}
	again, err := svc.Append(ctx, started.MeetingID, line, 900)
	if err != nil {
		t.Fatal(err)
	}
	if again.SegmentID != first.SegmentID {
		t.Fatalf("repeat produced a new segment %q; want the existing %q", again.SegmentID, first.SegmentID)
	}
	m, err := svc.Get(ctx, started.MeetingID)
	if err != nil {
		t.Fatal(err)
	}
	if len(m.Segments) != 1 {
		t.Fatalf("segments = %d; a repeated final must not add a row: %#v", len(m.Segments), m.Segments)
	}
}

func TestAppendStillPeelsAndOrdersAfterDedup(t *testing.T) {
	// The cheap last-segment read replaced a full segment listing, so the
	// prefix peel and the seq counter both have to keep working off it.
	svc := testMeetings(t)
	ctx := context.Background()
	started, err := svc.Start(ctx, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Append(ctx, started.MeetingID, "我们先看发布计划", 0); err != nil {
		t.Fatal(err)
	}
	// Growing final: the recognizer resends what it already committed plus
	// the new tail. Only the tail is new text.
	if _, err := svc.Append(ctx, started.MeetingID, "我们先看发布计划，然后过一遍风险", 1200); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Append(ctx, started.MeetingID, "最后定下发布时间", 2400); err != nil {
		t.Fatal(err)
	}
	m, err := svc.Get(ctx, started.MeetingID)
	if err != nil {
		t.Fatal(err)
	}
	if len(m.Segments) != 3 {
		t.Fatalf("segments = %d; want 3: %#v", len(m.Segments), m.Segments)
	}
	for i, seg := range m.Segments {
		if seg.Seq != i+1 {
			t.Fatalf("segment %d has seq %d; seq must stay dense and ordered: %#v", i, seg.Seq, m.Segments)
		}
	}
	if m.Segments[1].Text != "然后过一遍风险" {
		t.Fatalf("second segment = %q; the committed prefix should have been peeled off", m.Segments[1].Text)
	}
}
