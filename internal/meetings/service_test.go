package meetings_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/lunitide/lunitide/internal/meetings"
	sqlitestore "github.com/lunitide/lunitide/internal/storage/sqlite"
)

func testMeetings(t *testing.T) *meetings.Service {
	t.Helper()
	meetings.SilenceLoopbackForTest(t)
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
	meetings.InstallLoopbackForTest(t, func() []byte { return nil })
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

func TestHeartbeatKeepsRecordingAlive(t *testing.T) {
	svc := testMeetings(t)
	ctx := context.Background()
	started, err := svc.Start(ctx, "长会", "")
	if err != nil {
		t.Fatal(err)
	}
	before := started.UpdatedAt
	time.Sleep(15 * time.Millisecond)
	got, err := svc.Heartbeat(ctx, started.MeetingID)
	if err != nil || got.Status != meetings.StatusRecording {
		t.Fatalf("heartbeat = %#v %v", got, err)
	}
	if got.UpdatedAt == "" || got.UpdatedAt == before {
		t.Fatalf("heartbeat did not touch updatedAt: before=%q after=%q", before, got.UpdatedAt)
	}
	if got.DurationMS <= 0 {
		t.Fatalf("heartbeat duration = %d, list must not stay 0:00", got.DurationMS)
	}
	if _, err := svc.Stop(ctx, started.MeetingID); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Heartbeat(ctx, started.MeetingID); err != meetings.ErrNotRecording {
		t.Fatalf("heartbeat after stop = %v", err)
	}
}

func TestSummarizeLargeTranscriptSucceedsChunked(t *testing.T) {
	svc := testMeetings(t)
	var calls int
	svc.SetCompleter(func(ctx context.Context, title, transcript string) (meetings.Notes, error) {
		calls++
		if utf8.RuneCountInString(transcript) > meetings.SummarizeChunkRunes+800 {
			return meetings.Notes{}, errors.New("context overflow")
		}
		return meetings.Notes{Title: "发布评审", Summary: "背景：长会。\n讨论要点：范围。\n结论：继续。", Actions: "- 导出纪要"}, nil
	})
	ctx := context.Background()
	started, err := svc.Start(ctx, "周会", "")
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 800; i++ {
		line := "这一段把下周发布和安装包范围对齐清楚，并且把验收标准写进纪要。"
		if _, err := svc.Append(ctx, started.MeetingID, line, int64(i*1500)); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := svc.Stop(ctx, started.MeetingID); err != nil {
		t.Fatal(err)
	}
	ready, err := svc.Summarize(ctx, started.MeetingID)
	if err != nil || ready.Status != meetings.StatusReady {
		t.Fatalf("summarize = %#v %v", ready, err)
	}
	if ready.Transcript == "" || !strings.Contains(ready.Summary, "结论") {
		t.Fatalf("notes lost transcript or summary: %#v", ready)
	}
	if calls < 2 {
		t.Fatalf("expected chunked complete, calls=%d", calls)
	}
}

func TestSummarizeIgnoresCallerCancel(t *testing.T) {
	svc := testMeetings(t)
	ctx := context.Background()
	started, err := svc.Start(ctx, "周会", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Append(ctx, started.MeetingID, "先对齐范围", 0); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Stop(ctx, started.MeetingID); err != nil {
		t.Fatal(err)
	}
	ctx2, cancel := context.WithCancel(context.Background())
	svc.SetCompleter(func(ctx context.Context, title, transcript string) (meetings.Notes, error) {
		cancel()
		return meetings.Notes{Title: "评审", Summary: "背景：对齐。\n讨论要点：范围。\n结论：继续。", Actions: "- 写纪要"}, nil
	})
	got, err := svc.Summarize(ctx2, started.MeetingID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != meetings.StatusReady || !strings.Contains(got.Summary, "结论") {
		t.Fatalf("summarize after caller cancel = %#v", got)
	}
}

func TestGetAndListReclaimAbandonedSummarizing(t *testing.T) {
	meetings.SilenceLoopbackForTest(t)
	store, err := sqlitestore.OpenTemplated(context.Background(), filepath.Join(t.TempDir(), "meetings.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	svc := meetings.New(store)
	ctx := context.Background()
	started, err := svc.Start(ctx, "卡住的会", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Append(ctx, started.MeetingID, "只有逐字稿", 0); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Stop(ctx, started.MeetingID); err != nil {
		t.Fatal(err)
	}
	m, err := store.GetMeeting(ctx, started.MeetingID)
	if err != nil {
		t.Fatal(err)
	}
	m.Status = meetings.StatusSummarizing
	m.SummaryError = ""
	if err := store.UpdateMeeting(ctx, m); err != nil {
		t.Fatal(err)
	}
	listed, err := svc.List(ctx)
	if err != nil || len(listed) != 1 || listed[0].Status != meetings.StatusNeedsSummary {
		t.Fatalf("list reclaim = %#v %v", listed, err)
	}
	got, err := svc.Get(ctx, started.MeetingID)
	if err != nil || got.Status != meetings.StatusNeedsSummary {
		t.Fatalf("get reclaim = %#v %v", got, err)
	}
	if !strings.Contains(got.SummaryError, "可重试") {
		t.Fatalf("error = %q", got.SummaryError)
	}
}

func TestSummarizeInFlightIsNotReclaimed(t *testing.T) {
	svc := testMeetings(t)
	entered := make(chan struct{})
	release := make(chan struct{})
	svc.SetCompleter(func(ctx context.Context, title, transcript string) (meetings.Notes, error) {
		close(entered)
		<-release
		return meetings.Notes{Title: title, Summary: "背景：进行中。\n讨论要点：范围。\n结论：完成。", Actions: "- 导出"}, nil
	})
	ctx := context.Background()
	started, err := svc.Start(ctx, "进行中", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Append(ctx, started.MeetingID, "先对齐范围", 0); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Stop(ctx, started.MeetingID); err != nil {
		t.Fatal(err)
	}
	done := make(chan meetings.Meeting, 1)
	go func() {
		got, sumErr := svc.Summarize(ctx, started.MeetingID)
		if sumErr != nil {
			t.Errorf("summarize: %v", sumErr)
		}
		done <- got
	}()
	<-entered
	listed, err := svc.List(ctx)
	if err != nil || len(listed) != 1 || listed[0].Status != meetings.StatusSummarizing {
		t.Fatalf("in-flight list = %#v %v", listed, err)
	}
	close(release)
	ready := <-done
	if ready.Status != meetings.StatusReady {
		t.Fatalf("ready = %#v", ready)
	}
}

func silencePCM(ms int) []byte {
	if ms <= 0 {
		return nil
	}
	return make([]byte, ms*32)
}

func TestSegmentCapAllowsHourScaleUtterances(t *testing.T) {
	if meetings.MaxSegments < 100_000 {
		t.Fatalf("MaxSegments = %d; 1–2h of typical ASR chops needs >= 100000", meetings.MaxSegments)
	}
}

func TestDurableAudioSurvivesAndCatchupFillsGap(t *testing.T) {
	svc := testMeetings(t)
	audioRoot := t.TempDir()
	svc.SetAudioRoot(audioRoot)
	var transcribed []int
	svc.SetAudioTranscriber(func(ctx context.Context, pcm []byte) (string, error) {
		transcribed = append(transcribed, len(pcm))
		if len(pcm) == 0 {
			return "", nil
		}
		return "补转写这一段", nil
	})
	ctx := context.Background()
	started, err := svc.Start(ctx, "长会", "")
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 40; i++ {
		if _, err := svc.Append(ctx, started.MeetingID, "实时字幕一句", int64(i*800)); err != nil {
			t.Fatal(err)
		}
	}
	const liveMS = 40 * 800
	if _, err := svc.AppendAudio(ctx, started.MeetingID, silencePCM(liveMS+25_000)); err != nil {
		t.Fatal(err)
	}
	beat, err := svc.Heartbeat(ctx, started.MeetingID)
	if err != nil {
		t.Fatal(err)
	}
	if beat.DurationMS < 25_000 {
		t.Fatalf("heartbeat should follow audio/file clock, duration=%d", beat.DurationMS)
	}
	dir := filepath.Join(audioRoot, started.MeetingID)
	files, _ := filepath.Glob(filepath.Join(dir, "chunk_*.wav"))
	if len(files) == 0 {
		t.Fatal("expected WAV chunks on disk while recording")
	}
	stopped, err := svc.Stop(ctx, started.MeetingID)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stopped.Transcript, "实时字幕一句") {
		t.Fatalf("live transcript lost: %q", stopped.Transcript)
	}
	afterStop, _ := filepath.Glob(filepath.Join(dir, "chunk_*.wav"))
	if len(afterStop) == 0 {
		t.Fatal("audio files must survive stop")
	}
	caught, err := svc.CatchUp(ctx, started.MeetingID)
	if err != nil {
		t.Fatal(err)
	}
	if len(transcribed) == 0 {
		t.Fatal("expected leftover audio to be transcribed")
	}
	if !strings.Contains(caught.Transcript, "补转写这一段") {
		t.Fatalf("catch-up missing: %q", caught.Transcript)
	}
	if !strings.Contains(caught.Transcript, "实时字幕一句") {
		t.Fatalf("live lines dropped during catch-up: %q", caught.Transcript)
	}
}

func TestCatchupSkippedWhenLiveTranscriptCoversAudio(t *testing.T) {
	svc := testMeetings(t)
	svc.SetAudioRoot(t.TempDir())
	calls := 0
	svc.SetAudioTranscriber(func(ctx context.Context, pcm []byte) (string, error) {
		calls++
		return "不应调用", nil
	})
	ctx := context.Background()
	started, err := svc.Start(ctx, "短会", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Append(ctx, started.MeetingID, strings.Repeat("全程都有字幕。", 20), 1000); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.AppendAudio(ctx, started.MeetingID, silencePCM(8_000)); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Stop(ctx, started.MeetingID); err != nil {
		t.Fatal(err)
	}
	got, err := svc.CatchUp(ctx, started.MeetingID)
	if err != nil {
		t.Fatal(err)
	}
	if calls != 0 {
		t.Fatalf("catch-up should skip complete live transcript, calls=%d", calls)
	}
	if !strings.Contains(got.Transcript, "全程都有字幕") {
		t.Fatalf("transcript = %q", got.Transcript)
	}
}

func TestCatchUpSecondPassWhenFirstPassLeavesGap(t *testing.T) {
	svc := testMeetings(t)
	audioRoot := t.TempDir()
	svc.SetAudioRoot(audioRoot)
	ctx := context.Background()
	started, err := svc.Start(ctx, "长会", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Append(ctx, started.MeetingID, "实时字幕", 0); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.AppendAudio(ctx, started.MeetingID, silencePCM(120_000)); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Stop(ctx, started.MeetingID); err != nil {
		t.Fatal(err)
	}
	calls := 0
	const firstPassSpans = 6
	svc.SetAudioTranscriber(func(context.Context, []byte) (string, error) {
		calls++
		if calls <= firstPassSpans {
			return "", nil
		}
		return "补转写尾段", nil
	})
	caught, err := svc.CatchUp(ctx, started.MeetingID)
	if err != nil {
		t.Fatal(err)
	}
	if calls < 2 {
		t.Fatalf("expected a second catch-up pass, calls=%d", calls)
	}
	if !strings.Contains(caught.Transcript, "实时字幕") {
		t.Fatalf("live lines dropped: %q", caught.Transcript)
	}
	if !strings.Contains(caught.Transcript, "补转写尾段") {
		t.Fatalf("second pass missing: %q", caught.Transcript)
	}
}

func TestManyAppendsAndDeleteRemovesAudio(t *testing.T) {
	svc := testMeetings(t)
	audioRoot := t.TempDir()
	svc.SetAudioRoot(audioRoot)
	ctx := context.Background()
	started, err := svc.Start(ctx, "多段", "")
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 1200; i++ {
		if _, err := svc.Append(ctx, started.MeetingID, "一句对齐范围", int64(i*1500)); err != nil {
			t.Fatalf("append %d: %v", i, err)
		}
	}
	if _, err := svc.AppendAudio(ctx, started.MeetingID, silencePCM(3_000)); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Stop(ctx, started.MeetingID); err != nil {
		t.Fatal(err)
	}
	got, err := svc.Get(ctx, started.MeetingID)
	if err != nil || len(got.Segments) != 1200 {
		t.Fatalf("segments = %d err=%v", len(got.Segments), err)
	}
	dir := filepath.Join(audioRoot, started.MeetingID)
	if files, _ := filepath.Glob(filepath.Join(dir, "chunk_*.wav")); len(files) == 0 {
		t.Fatal("audio missing before delete")
	}
	if err := svc.Delete(ctx, started.MeetingID); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatalf("audio dir should be gone: %v", err)
	}
}

func TestNeedsCatchup(t *testing.T) {
	if meetings.NeedsCatchup(2_000, 0, false, "") {
		t.Fatal("tiny audio should not catch up")
	}
	if !meetings.NeedsCatchup(120_000, 0, false, "") {
		t.Fatal("hour of audio with no transcript needs catch-up")
	}
	if meetings.NeedsCatchup(120_000, 118_000, true, strings.Repeat("逐", 400)) {
		t.Fatal("live captions covering the clock should skip catch-up")
	}
	if !meetings.NeedsCatchup(3_600_000, 600_000, true, strings.Repeat("逐", 400)) {
		t.Fatal("ASR dying at 10 minutes of a 1h meeting needs catch-up")
	}
	if meetings.NeedsCatchup(600_000, 598_000, true, strings.Repeat("词", 400)) {
		t.Fatal("10-minute meeting with substantial live captions should skip catch-up")
	}
	if !meetings.NeedsCatchup(282_000, 280_000, true, "火焰已把火烧到天亮。往前走") {
		t.Fatal("fragmentary live captions on a long meeting should re-transcribe")
	}
}

func TestCatchupReplacesSparseLiveCaptions(t *testing.T) {
	svc := testMeetings(t)
	audioRoot := t.TempDir()
	svc.SetAudioRoot(audioRoot)
	svc.SetAudioTranscriber(func(ctx context.Context, pcm []byte) (string, error) {
		return "离线补转写完整句子。", nil
	})
	ctx := context.Background()
	started, err := svc.Start(ctx, "稀疏字幕", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Append(ctx, started.MeetingID, "火焰已把火烧到天亮", 270_000); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.AppendAudio(ctx, started.MeetingID, silencePCM(282_000)); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Stop(ctx, started.MeetingID); err != nil {
		t.Fatal(err)
	}
	caught, err := svc.CatchUp(ctx, started.MeetingID)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(caught.Transcript, "火焰已把火烧到天亮") {
		t.Fatalf("sparse live captions should be replaced: %q", caught.Transcript)
	}
	if !strings.Contains(caught.Transcript, "离线补转写完整句子") {
		t.Fatalf("catch-up transcript = %q", caught.Transcript)
	}
}

func TestRollingWavChunksDoNotStopTheMeeting(t *testing.T) {
	svc := testMeetings(t)
	audioRoot := t.TempDir()
	svc.SetAudioRoot(audioRoot)
	ctx := context.Background()
	started, err := svc.Start(ctx, "长会", meetings.AudioMicrophoneAndSystem)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.AppendAudio(ctx, started.MeetingID, silencePCM(150_000)); err != nil {
		t.Fatalf("150s append: %v", err)
	}
	live, err := svc.Get(ctx, started.MeetingID)
	if err != nil || live.Status != meetings.StatusRecording {
		t.Fatalf("after 150s status=%#v err=%v", live, err)
	}
	dir := filepath.Join(audioRoot, started.MeetingID)
	files, _ := filepath.Glob(filepath.Join(dir, "chunk_*.wav"))
	if len(files) < 2 {
		t.Fatalf("expected 2+ rolling WAVs after 150s, got %d", len(files))
	}
	if _, err := svc.Heartbeat(ctx, started.MeetingID); err != nil {
		t.Fatalf("heartbeat after rotate: %v", err)
	}
	if _, err := svc.Append(ctx, started.MeetingID, "旋转后仍在录", 151_000); err != nil {
		t.Fatalf("caption after rotate: %v", err)
	}
	if _, err := svc.AppendAudio(ctx, started.MeetingID, silencePCM(180_000)); err != nil {
		t.Fatalf("180s append: %v", err)
	}
	still, err := svc.Get(ctx, started.MeetingID)
	if err != nil || still.Status != meetings.StatusRecording {
		t.Fatalf("after 330s status=%#v err=%v — rotate must not call Stop", still, err)
	}
	files, _ = filepath.Glob(filepath.Join(dir, "chunk_*.wav"))
	if len(files) < 2 {
		t.Fatalf("expected 2+ WAVs after 330s, got %d", len(files))
	}
	stopped, err := svc.Stop(ctx, started.MeetingID)
	if err != nil || stopped.Status != meetings.StatusTranscribed {
		t.Fatalf("explicit stop = %#v %v", stopped, err)
	}
}

func TestCatchupSplitsLongWavUnderSherpaLimit(t *testing.T) {
	svc := testMeetings(t)
	svc.SetAudioRoot(t.TempDir())
	var sizes []int
	svc.SetAudioTranscriber(func(ctx context.Context, pcm []byte) (string, error) {
		sizes = append(sizes, len(pcm))
		return "补转写", nil
	})
	ctx := context.Background()
	started, err := svc.Start(ctx, "长会", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.AppendAudio(ctx, started.MeetingID, silencePCM(150_000)); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Stop(ctx, started.MeetingID); err != nil {
		t.Fatal(err)
	}
	caught, err := svc.CatchUp(ctx, started.MeetingID)
	if err != nil {
		t.Fatal(err)
	}
	maxBytes := meetings.CatchupSpanSeconds * 16000 * 2
	if len(sizes) < 2 {
		t.Fatalf("expected multiple catch-up spans for 150s, got %v", sizes)
	}
	for i, n := range sizes {
		if n > maxBytes {
			t.Fatalf("span %d is %d bytes; sherpa max window is %d", i, n, maxBytes)
		}
	}
	if !strings.Contains(caught.Transcript, "补转写") {
		t.Fatalf("catch-up transcript = %q", caught.Transcript)
	}
}

func TestSummarizeAfterStopWritesNotesOrNeedsSummary(t *testing.T) {
	ctx := context.Background()
	t.Run("notes", func(t *testing.T) {
		svc := testMeetings(t)
		svc.SetCompleter(func(ctx context.Context, title, transcript string) (meetings.Notes, error) {
			return meetings.Notes{Title: "纪要", Summary: "背景：对齐。\n讨论要点：范围。\n结论：继续。", Actions: "- 写BRD"}, nil
		})
		started, err := svc.Start(ctx, "周会", "")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := svc.Append(ctx, started.MeetingID, "先对齐范围", 0); err != nil {
			t.Fatal(err)
		}
		if _, err := svc.Stop(ctx, started.MeetingID); err != nil {
			t.Fatal(err)
		}
		got, err := svc.Summarize(ctx, started.MeetingID)
		if err != nil || got.Status != meetings.StatusReady || got.Summary == "" || got.Actions == "" {
			t.Fatalf("summarize = %#v %v", got, err)
		}
	})
	t.Run("needs_summary", func(t *testing.T) {
		svc := testMeetings(t)
		started, err := svc.Start(ctx, "周会", "")
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
			t.Fatalf("honest hang/missing model = %#v %v", got, err)
		}
		if got.Summary != "" {
			t.Fatalf("must not fake 摘要: %#v", got)
		}
	})
}

func TestSummarizeSurfacesCompleterError(t *testing.T) {
	svc := testMeetings(t)
	svc.SetCompleter(func(ctx context.Context, title, transcript string) (meetings.Notes, error) {
		return meetings.Notes{}, errors.New("upstream 400: stream required")
	})
	ctx := context.Background()
	started, err := svc.Start(ctx, "周会", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Append(ctx, started.MeetingID, "对齐范围后发布", 0); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Stop(ctx, started.MeetingID); err != nil {
		t.Fatal(err)
	}
	got, err := svc.Summarize(ctx, started.MeetingID)
	if err != nil || got.Status != meetings.StatusNeedsSummary {
		t.Fatalf("summarize = %#v %v", got, err)
	}
	if !strings.Contains(got.SummaryError, "stream required") {
		t.Fatalf("summaryError = %q", got.SummaryError)
	}
}

func TestSummarizePersistsAfterSlowCompleter(t *testing.T) {
	restore := meetings.SetPersistTimeoutForTest(40 * time.Millisecond)
	t.Cleanup(restore)
	svc := testMeetings(t)
	svc.SetCompleter(func(ctx context.Context, title, transcript string) (meetings.Notes, error) {
		time.Sleep(80 * time.Millisecond)
		return meetings.Notes{Summary: "背景：对齐。\n结论：继续。", Actions: "- 写纪要"}, nil
	})
	ctx := context.Background()
	started, err := svc.Start(ctx, "周会", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Append(ctx, started.MeetingID, "先对齐范围", 0); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Stop(ctx, started.MeetingID); err != nil {
		t.Fatal(err)
	}
	got, err := svc.Summarize(ctx, started.MeetingID)
	if err != nil || got.Status != meetings.StatusReady || got.Summary == "" {
		t.Fatalf("slow summarize must still persist: %#v %v", got, err)
	}
}
