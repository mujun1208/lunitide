package app

import (
	"bytes"
	"context"
	"encoding/json"
	"html"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/lunitide/lunitide/internal/bridge"
	"github.com/lunitide/lunitide/internal/ipc"
	"github.com/lunitide/lunitide/internal/meetings"
	storage "github.com/lunitide/lunitide/internal/storage/sqlite"
	"github.com/oklog/ulid/v2"
)

func TestMeetingLongDetailFitsActualIPCFrame(t *testing.T) {
	ctx := context.Background()
	store, err := storage.OpenTemplated(ctx, filepath.Join(t.TempDir(), "meeting-frame.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	now := time.Now().UTC().Format(time.RFC3339Nano)
	m := meetings.Meeting{MeetingID: ulid.Make().String(), Title: "Long meeting", Status: meetings.StatusReady, AudioSource: meetings.AudioMicrophone, StartedAt: now, EndedAt: now, CreatedAt: now, UpdatedAt: now, Transcript: strings.Repeat("中", 1<<20), Summary: "ready"}
	if err = store.InsertMeeting(ctx, m); err != nil {
		t.Fatal(err)
	}
	if err = store.ReplaceDocs(ctx, m.MeetingID, []meetings.Doc{{DocID: ulid.Make().String(), MeetingID: m.MeetingID, Kind: "markdown", Body: m.Transcript, CreatedAt: now}, {DocID: ulid.Make().String(), MeetingID: m.MeetingID, Kind: "html", Body: m.Transcript, CreatedAt: now}}); err != nil {
		t.Fatal(err)
	}
	e := NewEngine(nil, "test")
	e.SetMeetingsService(meetings.New(store))
	r := e.Handle(ctx, validRequest("meetings.get", `{"meetingId":"`+m.MeetingID+`"}`))
	if !r.OK {
		t.Fatal(r.Error)
	}
	raw, err := json.Marshal(r)
	if err != nil {
		t.Fatal(err)
	}
	if err = ipc.WriteFrame(&bytes.Buffer{}, raw); err != nil {
		t.Fatalf("legal long meeting response=%d bytes exceeds actual frame=%d: %v", len(raw), ipc.MaxFrameSize, err)
	}
}

func TestMeetingPagedContentPreservesFullUnicodeAndRejectsStaleEdits(t *testing.T) {
	ctx := context.Background()
	store, err := storage.OpenTemplated(ctx, filepath.Join(t.TempDir(), "paged.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	now := time.Now().UTC().Format(time.RFC3339Nano)
	text := strings.Repeat("<中🙂&", 1<<18)
	m := meetings.Meeting{MeetingID: ulid.Make().String(), Title: "Long Unicode", Status: meetings.StatusReady, AudioSource: meetings.AudioMicrophone, StartedAt: now, EndedAt: now, CreatedAt: now, UpdatedAt: now, Transcript: text, Summary: strings.Repeat("<", 65536), Actions: strings.Repeat("&", 32768)}
	if err = store.InsertMeeting(ctx, m); err != nil {
		t.Fatal(err)
	}
	for seq := 1; seq <= 35; seq++ {
		if err = store.InsertSegment(ctx, meetings.Segment{SegmentID: ulid.Make().String(), MeetingID: m.MeetingID, Seq: seq, Text: strings.Repeat("<", 16384), CreatedAt: now}); err != nil {
			t.Fatal(err)
		}
	}
	svc := meetings.New(store)
	e := NewEngine(nil, "test")
	e.SetMeetingsService(svc)
	call := func(method string, payload any) bridge.Response {
		t.Helper()
		raw, err := json.Marshal(payload)
		if err != nil {
			t.Fatal(err)
		}
		r := e.Handle(ctx, validRequest(method, string(raw)))
		frame, err := json.Marshal(r)
		if err != nil {
			t.Fatal(err)
		}
		if err = ipc.WriteFrame(&bytes.Buffer{}, frame); err != nil {
			t.Fatalf("%s response %d: %v", method, len(frame), err)
		}
		return r
	}
	var detail struct {
		Revision, TranscriptRevision int64
		Transcript                   string
		TranscriptTotalRunes         int
		TranscriptComplete           bool
		Segments                     []meetings.Segment
		Docs                         []meetings.Doc
	}
	m7Decode(t, call("meetings.get", map[string]any{"meetingId": m.MeetingID}), &detail)
	if detail.TranscriptComplete || detail.TranscriptTotalRunes != 1<<20 || len([]rune(detail.Transcript)) != meetings.TranscriptPageRunes || len(detail.Segments) != 10 || len(detail.Docs) != 0 {
		t.Fatalf("detail not bounded: %+v", struct {
			Complete        bool
			Total, Segments int
		}{detail.TranscriptComplete, detail.TranscriptTotalRunes, len(detail.Segments)})
	}
	var rebuilt strings.Builder
	for offset := 0; ; {
		var page meetings.TranscriptPage
		m7Decode(t, call("meetings.transcript.get", map[string]any{"meetingId": m.MeetingID, "transcriptRevision": detail.TranscriptRevision, "offset": offset}), &page)
		if page.Offset != offset || page.TotalRunes != 1<<20 {
			t.Fatal("page position drift")
		}
		rebuilt.WriteString(page.Text)
		if page.NextOffset == 0 {
			break
		}
		offset = page.NextOffset
	}
	if rebuilt.String() != text {
		t.Fatal("paged transcript lost Unicode or content")
	}
	var first meetings.SegmentPage
	m7Decode(t, call("meetings.segments.list", map[string]any{"meetingId": m.MeetingID, "expectedRevision": detail.Revision, "afterSeq": 0}), &first)
	if first.ThroughSeq != 35 || len(first.Items) != 10 || !first.HasMore {
		t.Fatalf("first segment page %+v", first)
	}
	if err = store.InsertSegment(ctx, meetings.Segment{SegmentID: ulid.Make().String(), MeetingID: m.MeetingID, Seq: 36, Text: "later", CreatedAt: now}); err != nil {
		t.Fatal(err)
	}
	seqs := len(first.Items)
	for page := first; page.HasMore; {
		m7Decode(t, call("meetings.segments.list", map[string]any{"meetingId": m.MeetingID, "expectedRevision": detail.Revision, "afterSeq": page.NextSeq, "throughSeq": first.ThroughSeq}), &page)
		seqs += len(page.Items)
	}
	if seqs != 35 {
		t.Fatalf("snapshot segments=%d", seqs)
	}
	for _, payload := range []map[string]any{
		{"meetingId": m.MeetingID, "expectedRevision": detail.Revision, "transcript": detail.Transcript},
		{"meetingId": m.MeetingID, "expectedRevision": detail.Revision, "transcriptEdit": map[string]any{"transcriptRevision": detail.TranscriptRevision + 1, "offset": 0, "deleteRunes": 1, "text": "bad"}},
	} {
		if r := call("meetings.update", payload); r.OK {
			t.Fatal("partial or wrong-source replacement accepted")
		}
	}
	edit := meetings.TranscriptEdit{TranscriptRevision: detail.TranscriptRevision, Offset: 16384, DeleteRunes: 16384, Text: "改🙂"}
	var updated struct{ Revision, TranscriptRevision int64 }
	m7Decode(t, call("meetings.update", map[string]any{"meetingId": m.MeetingID, "expectedRevision": detail.Revision, "transcriptEdit": edit}), &updated)
	want := string([]rune(text)[:edit.Offset]) + edit.Text + string([]rune(text)[edit.Offset+edit.DeleteRunes:])
	stored, err := store.GetMeeting(ctx, m.MeetingID)
	if err != nil || stored.Transcript != want {
		t.Fatalf("range edit lost outside-page text: %v", err)
	}
	for _, tc := range []struct {
		method  string
		payload any
	}{
		{"meetings.transcript.get", map[string]any{"meetingId": m.MeetingID, "transcriptRevision": detail.TranscriptRevision, "offset": 16384}},
		{"meetings.segments.list", map[string]any{"meetingId": m.MeetingID, "expectedRevision": detail.Revision, "afterSeq": 10, "throughSeq": 35}},
		{"meetings.update", map[string]any{"meetingId": m.MeetingID, "expectedRevision": updated.Revision, "transcriptEdit": edit}},
	} {
		if r := call(tc.method, tc.payload); r.OK || r.Error.Code != "MEETING_CHANGED" {
			t.Fatalf("stale %s: %+v", tc.method, r)
		}
	}
	for _, format := range []string{"txt", "markdown", "html"} {
		path := filepath.Join(t.TempDir(), "complete."+format)
		if _, _, err = svc.Export(ctx, m.MeetingID, format, path); err != nil {
			t.Fatal(err)
		}
		body, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		needle := want
		if format == "html" {
			needle = html.EscapeString(want)
		}
		if !strings.Contains(string(body), needle) {
			t.Fatalf("%s export omitted original content", format)
		}
	}
	// Large list rows must not multiply the full body or summary by 200.
	m.MeetingID = ulid.Make().String()
	if err = store.InsertMeeting(ctx, m); err != nil {
		t.Fatal(err)
	}
	var list struct{ Items []meetings.Meeting }
	m7Decode(t, call("meetings.list", map[string]any{}), &list)
	if len(list.Items) != 2 {
		t.Fatal("list omitted meetings")
	}
	for _, item := range list.Items {
		if item.Transcript != "" || item.Summary != "" || item.Actions != "" {
			t.Fatal("list returned large body")
		}
	}
	t.Logf("verified %d Unicode runes, %d segment snapshot rows, full exports, and stale request refusal", 1<<20, seqs)
}
