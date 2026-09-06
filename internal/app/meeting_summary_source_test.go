package app

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/lunitide/lunitide/internal/ipc"
	"github.com/lunitide/lunitide/internal/meetings"
)

func TestMeetingSummarySourceEngineRoutePinsActualInputAndRejectsChangedDigest(t *testing.T) {
	e, svc := newMeetingsEngine(t)
	ctx := context.Background()
	m, err := svc.Start(ctx, "真实输入标题", meetings.AudioMicrophone)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = svc.Append(ctx, m.MeetingID, "这份原稿只用于验证真实来源接口。", 0); err != nil {
		t.Fatal(err)
	}
	m, err = svc.Stop(ctx, m.MeetingID, m.Revision)
	if err != nil {
		t.Fatal(err)
	}
	svc.SetCompleter(func(context.Context, string, string) (meetings.Notes, error) {
		return meetings.Notes{Summary: "已提交摘要"}, nil
	})
	m, err = svc.Summarize(ctx, m.MeetingID, m.Revision)
	if err != nil {
		t.Fatal(err)
	}
	payload := fmt.Sprintf(`{"meetingId":%q,"sourceDigest":%q,"offset":0}`, m.MeetingID, m.SummarySourceDigest)
	response := e.Handle(ctx, validRequest("meetings.summary.source.get", payload))
	if !response.OK {
		t.Fatalf("new route failed full Engine validation: %#v", response)
	}
	raw, err := json.Marshal(response)
	if err != nil {
		t.Fatal(err)
	}
	var frame bytes.Buffer
	if err = ipc.WriteFrame(&frame, raw); err != nil {
		t.Fatal(err)
	}
	var got meetings.SummarySourcePage
	payloadJSON, err := json.Marshal(response.Payload)
	if err != nil {
		t.Fatal(err)
	}
	if err = json.Unmarshal(payloadJSON, &got); err != nil {
		t.Fatal(err)
	}
	if got.SourceDigest != m.SummarySourceDigest || got.SourceRevision != m.TranscriptRevision || got.Title != "真实输入标题" || got.Transcript != meetings.CleanTranscript(m.Transcript) {
		t.Fatalf("wrong public source: %#v", got)
	}
	bad := e.Handle(ctx, validRequest("meetings.summary.source.get", fmt.Sprintf(`{"meetingId":%q,"sourceDigest":%q,"offset":0}`, m.MeetingID, "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")))
	if bad.OK || bad.Error == nil || bad.Error.Code != "MEETING_CHANGED" {
		t.Fatalf("changed source falsely acknowledged: %#v", bad)
	}
	if _, exists := publicMeeting(m, true)["summarySourceTranscript"]; exists {
		t.Fatal("detail duplicated the full source body instead of paging")
	}
}
