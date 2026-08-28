package meetings_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lunitide/lunitide/internal/meetings"
	sqlitestore "github.com/lunitide/lunitide/internal/storage/sqlite"
)

func testMeetings(t *testing.T) *meetings.Service {
	t.Helper()
	store, err := sqlitestore.OpenTemplated(context.Background(), filepath.Join(t.TempDir(), "meetings.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return meetings.New(store)
}

func TestStartAppendStopSummarizeAndExport(t *testing.T) {
	svc := testMeetings(t)
	svc.SetCompleter(func(ctx context.Context, title, transcript string) (meetings.Notes, error) {
		if !strings.Contains(transcript, "下周发布") {
			t.Fatalf("transcript = %q", transcript)
		}
		return meetings.Notes{Title: "发布评审", Summary: "讨论了下周发布。", Actions: "- 完成安装包"}, nil
	})
	ctx := context.Background()
	started, err := svc.Start(ctx, "", "")
	if err != nil || started.Status != meetings.StatusRecording || started.AudioSource != "microphone" {
		t.Fatalf("start = %#v %v", started, err)
	}
	if _, err := svc.Start(ctx, "第二场", ""); err != meetings.ErrBusy {
		t.Fatalf("second start = %v", err)
	}
	if _, err := svc.Append(ctx, started.MeetingID, "大家好", 0); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Append(ctx, started.MeetingID, "下周发布", 1200); err != nil {
		t.Fatal(err)
	}
	stopped, err := svc.Stop(ctx, started.MeetingID)
	if err != nil || stopped.Status != meetings.StatusTranscribed {
		t.Fatalf("stop = %#v %v", stopped, err)
	}
	if !strings.Contains(stopped.Transcript, "大家好") || !strings.Contains(stopped.Transcript, "下周发布") {
		t.Fatalf("transcript = %q", stopped.Transcript)
	}
	ready, err := svc.Summarize(ctx, started.MeetingID)
	if err != nil || ready.Status != meetings.StatusReady || ready.Title != "发布评审" {
		t.Fatalf("summarize = %#v %v", ready, err)
	}
	if ready.Summary != "讨论了下周发布。" || !strings.Contains(ready.Actions, "安装包") {
		t.Fatalf("notes = %#v", ready)
	}
	if len(ready.Docs) != 2 {
		t.Fatalf("docs = %#v", ready.Docs)
	}
	dest := filepath.Join(t.TempDir(), "notes.md")
	path, format, err := svc.Export(ctx, started.MeetingID, "markdown", dest)
	if err != nil || format != "markdown" || path != dest {
		t.Fatalf("export = %q %q %v", path, format, err)
	}
	body, err := os.ReadFile(dest)
	if err != nil || !strings.Contains(string(body), "## 会议摘要") || !strings.Contains(string(body), "本机麦克风") {
		t.Fatalf("markdown = %s %v", body, err)
	}
	listed, err := svc.List(ctx)
	if err != nil || len(listed) != 1 || listed[0].MeetingID != started.MeetingID {
		t.Fatalf("list = %#v %v", listed, err)
	}
}

func TestSummarizeWithoutCompleterStaysHonest(t *testing.T) {
	svc := testMeetings(t)
	ctx := context.Background()
	started, err := svc.Start(ctx, "空模型", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Append(ctx, started.MeetingID, "只有逐字稿", 0); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Stop(ctx, started.MeetingID); err != nil {
		t.Fatal(err)
	}
	got, err := svc.Summarize(ctx, started.MeetingID)
	if err != nil || got.Status != meetings.StatusNeedsSummary {
		t.Fatalf("summarize = %#v %v", got, err)
	}
	if !strings.Contains(got.SummaryError, "尚未配置可用模型") {
		t.Fatalf("error = %q", got.SummaryError)
	}
}

func TestParseNotesJSONAndMarkdown(t *testing.T) {
	got := meetings.ParseNotes(`{"title":"评审","summary":"结论","actions":["跟进安装包"]}`, "fallback")
	if got.Title != "评审" || got.Summary != "结论" || !strings.Contains(got.Actions, "跟进安装包") {
		t.Fatalf("json notes = %#v", got)
	}
	md := meetings.ParseNotes("# 标题\n\n## 会议摘要\n\n已对齐范围。\n\n## 决议/待办\n\n- 写纪要\n\n## 全文逐字稿\n\n原文", "fallback")
	if !strings.Contains(md.Summary, "已对齐范围") || !strings.Contains(md.Actions, "写纪要") {
		t.Fatalf("md notes = %#v", md)
	}
}

func TestStartSystemAudioAndExportLabel(t *testing.T) {
	svc := testMeetings(t)
	ctx := context.Background()
	if _, err := svc.Start(ctx, "坏", "remote"); err != meetings.ErrInvalid {
		t.Fatalf("invalid source = %v", err)
	}
	started, err := svc.Start(ctx, "周会", meetings.AudioMicrophoneAndSystem)
	if err != nil || started.AudioSource != meetings.AudioMicrophoneAndSystem {
		t.Fatalf("start mix = %#v %v", started, err)
	}
	if _, err := svc.Append(ctx, started.MeetingID, "远程同事说了范围", 0); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Stop(ctx, started.MeetingID); err != nil {
		t.Fatal(err)
	}
	got, err := svc.Get(ctx, started.MeetingID)
	if err != nil {
		t.Fatal(err)
	}
	body := meetings.RenderMarkdown(got)
	if !strings.Contains(body, "本机系统声音") || strings.Contains(body, "未混录系统扬声器") {
		t.Fatalf("markdown = %s", body)
	}
}

func TestSummarizePassesCleanedTranscriptToCompleter(t *testing.T) {
	svc := testMeetings(t)
	svc.SetCompleter(func(ctx context.Context, title, transcript string) (meetings.Notes, error) {
		if strings.Contains(transcript, "呃") {
			t.Fatalf("uncleaned transcript = %q", transcript)
		}
		if !strings.Contains(transcript, "BRD") {
			t.Fatalf("acronym missing = %q", transcript)
		}
		return meetings.Notes{Title: "落实会", Summary: "背景：落实工作。\n讨论要点：先写BRD。\n结论：按步骤推进。", Actions: "- 先写BRD"}, nil
	})
	ctx := context.Background()
	started, err := svc.Start(ctx, "工作落实会议", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Append(ctx, started.MeetingID, "呃第一步应该先写 b r d", 0); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Stop(ctx, started.MeetingID); err != nil {
		t.Fatal(err)
	}
	got, err := svc.Summarize(ctx, started.MeetingID)
	if err != nil || got.Status != meetings.StatusReady {
		t.Fatalf("summarize = %#v %v", got, err)
	}
	if !strings.Contains(got.Transcript, "b r d") {
		t.Fatalf("stored transcript should keep the original line: %q", got.Transcript)
	}
	if !strings.Contains(got.Summary, "结论") || !strings.Contains(got.Actions, "BRD") {
		t.Fatalf("notes = %#v", got)
	}
}

func TestUpdateAndDeleteMeeting(t *testing.T) {
	svc := testMeetings(t)
	ctx := context.Background()
	started, err := svc.Start(ctx, "评审", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.Delete(ctx, started.MeetingID); err != meetings.ErrBusy {
		t.Fatalf("delete recording = %v", err)
	}
	if _, err := svc.Append(ctx, started.MeetingID, "大家好", 0); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Stop(ctx, started.MeetingID); err != nil {
		t.Fatal(err)
	}
	summary := "改过的摘要"
	actions := "- 新待办"
	transcript := "改过的稿"
	updated, err := svc.Update(ctx, started.MeetingID, meetings.MeetingPatch{
		Summary: &summary, Actions: &actions, Transcript: &transcript,
	})
	if err != nil || updated.Summary != summary || updated.Actions != actions || updated.Transcript != transcript {
		t.Fatalf("update = %#v %v", updated, err)
	}
	dest := filepath.Join(t.TempDir(), "edited.md")
	if _, _, err := svc.Export(ctx, started.MeetingID, "markdown", dest); err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(dest)
	if err != nil || !strings.Contains(string(body), "改过的摘要") || !strings.Contains(string(body), "新待办") {
		t.Fatalf("export = %s %v", body, err)
	}
	if err := svc.Delete(ctx, started.MeetingID); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Get(ctx, started.MeetingID); err != meetings.ErrNotFound {
		t.Fatalf("get after delete = %v", err)
	}
}
