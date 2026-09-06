package sqlite

import (
	"context"
	"testing"
	"time"

	"github.com/lunitide/lunitide/internal/meetings"
	"github.com/oklog/ulid/v2"
)

func TestMeetingSegmentPagesAtHundredThousandRows(t *testing.T) {
	store := openRuntimeStore(t)
	ctx := context.Background()
	now := time.Now().UTC().Format(time.RFC3339Nano)
	m := meetings.Meeting{MeetingID: ulid.Make().String(), Title: "100000 segments", Status: meetings.StatusTranscribed, AudioSource: meetings.AudioMicrophone, StartedAt: now, EndedAt: now, CreatedAt: now, UpdatedAt: now}
	if err := store.InsertMeeting(ctx, m); err != nil {
		t.Fatal(err)
	}
	if _, err := store.db.ExecContext(ctx, `WITH RECURSIVE n(v) AS (SELECT 1 UNION ALL SELECT v+1 FROM n WHERE v<100000) INSERT INTO meeting_segments(segment_id,meeting_id,seq,started_ms,body,created_at) SELECT printf('01ARZ3NDEKTSV4RRFFQ6%06d',v),?,v,v,'text',? FROM n`, m.MeetingID, now); err != nil {
		t.Fatal(err)
	}
	svc := meetings.New(store)
	for _, after := range []int{0, 99990} {
		page, err := svc.Segments(ctx, m.MeetingID, 1, after, 0)
		if err != nil {
			t.Fatal(err)
		}
		if page.ThroughSeq != 100000 || len(page.Items) != 10 || page.Items[0].Seq != after+1 || page.Items[9].Seq != after+10 || page.HasMore != (after == 0) {
			t.Fatalf("boundary after %d: %+v", after, page)
		}
	}
	detail, err := svc.Detail(ctx, m.MeetingID)
	if err != nil || len(detail.Segments) != 10 || len(detail.Docs) != 0 {
		t.Fatalf("detail read whole collection: %d %v", len(detail.Segments), err)
	}
}
