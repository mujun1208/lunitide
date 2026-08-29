package meetings_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/lunitide/lunitide/internal/meetings"
)

func TestSplitTranscriptSlidingWindows(t *testing.T) {
	short := "短会"
	if got := meetings.SplitTranscript(short, meetings.SummarizeChunkRunes, meetings.SummarizeOverlapRunes); len(got) != 1 || got[0] != short {
		t.Fatalf("short = %#v", got)
	}
	var b strings.Builder
	for i := 0; i < 80; i++ {
		fmt.Fprintf(&b, "第%d段讨论了发布计划和范围对齐。\n", i)
	}
	raw := b.String()
	chunks := meetings.SplitTranscript(raw, 200, 20)
	if len(chunks) < 2 {
		t.Fatalf("expected several windows, got %d", len(chunks))
	}
	joined := strings.Join(chunks, "")
	if !strings.Contains(joined, "第0段") || !strings.Contains(joined, "第79段") {
		t.Fatalf("lost ends: %d chunks", len(chunks))
	}
	for i, chunk := range chunks {
		if n := utf8.RuneCountInString(chunk); n > 200+40 {
			t.Fatalf("chunk %d too large: %d", i, n)
		}
	}
}

func TestSummarizeLongChunksOversizedTranscript(t *testing.T) {
	var seen []int
	complete := func(ctx context.Context, title, transcript string) (meetings.Notes, error) {
		n := utf8.RuneCountInString(transcript)
		seen = append(seen, n)
		if n > meetings.SummarizeChunkRunes+500 {
			return meetings.Notes{}, errors.New("context overflow")
		}
		return meetings.Notes{Title: "长会", Summary: "背景：测试。\n讨论要点：片段。\n结论：继续。", Actions: "- 跟进"}, nil
	}
	var b strings.Builder
	for i := 0; i < 800; i++ {
		fmt.Fprintf(&b, "第%d段对齐了下周发布和安装包范围，并且把验收标准写进纪要。\n", i)
	}
	notes, err := meetings.SummarizeLong(context.Background(), complete, "周会", b.String())
	if err != nil {
		t.Fatal(err)
	}
	if notes.Title == "" || notes.Summary == "" || !strings.Contains(notes.Actions, "跟进") {
		t.Fatalf("notes = %#v", notes)
	}
	if len(seen) < 2 {
		t.Fatalf("expected map-reduce, calls=%v", seen)
	}
	for i, n := range seen {
		if n > meetings.SummarizeChunkRunes*2 {
			t.Fatalf("call %d still oversized: %d", i, n)
		}
	}
}

func TestSummarizeLongRetriesTruncatedWhenOneShotFails(t *testing.T) {
	calls := 0
	complete := func(ctx context.Context, title, transcript string) (meetings.Notes, error) {
		calls++
		if utf8.RuneCountInString(transcript) > 100 {
			return meetings.Notes{}, errors.New("timeout")
		}
		return meetings.Notes{Title: title, Summary: "背景：短。\n讨论要点：截断。\n结论：仍产出。", Actions: "- 保存逐字稿"}, nil
	}
	notes, err := meetings.SummarizeLong(context.Background(), complete, "评审", strings.Repeat("对齐范围。", 40))
	if err != nil {
		t.Fatal(err)
	}
	if calls < 2 || !strings.Contains(notes.Summary, "结论") {
		t.Fatalf("calls=%d notes=%#v", calls, notes)
	}
}

func TestSummarizeLongStitchesWhenReduceFails(t *testing.T) {
	complete := func(ctx context.Context, title, transcript string) (meetings.Notes, error) {
		if strings.Contains(transcript, "分段纪要") {
			return meetings.Notes{}, errors.New("reduce timeout")
		}
		return meetings.Notes{Title: "片段会", Summary: "背景：一段。\n讨论要点：范围。\n结论：推进。", Actions: "- 写BRD"}, nil
	}
	var b strings.Builder
	for i := 0; i < 800; i++ {
		fmt.Fprintf(&b, "第%d句把范围讲清楚了，并且把验收标准和待办写进纪要。\n", i)
	}
	notes, err := meetings.SummarizeLong(context.Background(), complete, "周会", b.String())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(notes.Summary, "一段") || !strings.Contains(notes.Actions, "BRD") {
		t.Fatalf("stitched = %#v", notes)
	}
}

func TestSummarizeLongTimesOutHungCompleter(t *testing.T) {
	restore := meetings.SetSummarizeDeadlinesForTest(30*time.Millisecond, 80*time.Millisecond)
	t.Cleanup(restore)
	complete := func(ctx context.Context, title, transcript string) (meetings.Notes, error) {
		time.Sleep(400 * time.Millisecond)
		return meetings.Notes{Title: "迟到", Summary: "不该用"}, nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Millisecond)
	defer cancel()
	_, err := meetings.SummarizeLong(ctx, complete, "周会", "对齐范围。")
	if err == nil {
		t.Fatal("hung completer should time out")
	}
}
