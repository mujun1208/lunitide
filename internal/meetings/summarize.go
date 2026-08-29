package meetings

import (
	"context"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	// SummarizeChunkRunes is one LLM window: short enough for typical
	// provider context, long enough that an hour of speech is a handful of
	// map calls rather than hundreds.
	SummarizeChunkRunes = 8000
	// SummarizeOverlapRunes keeps a little of the previous window so a
	// sentence split across a chunk boundary is not lost.
	SummarizeOverlapRunes = 240
	summarizeMaxChunks    = 32
)

var (
	// summarizeChunkDeadline bounds one Completer call so a hung provider
	// cannot consume the whole meetings.summarize Bridge deadline.
	summarizeChunkDeadline = 2 * time.Minute
	// summarizeJobDeadline is slightly under the 10-minute Bridge cap so
	// the meeting is always flipped off 生成摘要中 before the RPC dies.
	summarizeJobDeadline = 9 * time.Minute
)

// SetSummarizeDeadlinesForTest shortens hang/timeout coverage. Restore via the returned func.
func SetSummarizeDeadlinesForTest(chunk, job time.Duration) func() {
	prevChunk, prevJob := summarizeChunkDeadline, summarizeJobDeadline
	if chunk > 0 {
		summarizeChunkDeadline = chunk
	}
	if job > 0 {
		summarizeJobDeadline = job
	}
	return func() {
		summarizeChunkDeadline = prevChunk
		summarizeJobDeadline = prevJob
	}
}

// SplitTranscript cuts a cleaned 逐字稿 into sliding windows. A short
// transcript is a single chunk so summarize stays one Completer call.
func SplitTranscript(text string, maxRunes, overlapRunes int) []string {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil
	}
	if maxRunes < 32 {
		maxRunes = SummarizeChunkRunes
	}
	runes := []rune(text)
	if len(runes) <= maxRunes {
		return []string{string(runes)}
	}
	if overlapRunes < 0 {
		overlapRunes = 0
	}
	if overlapRunes >= maxRunes {
		overlapRunes = maxRunes / 8
	}
	var chunks []string
	for start := 0; start < len(runes); {
		end := start + maxRunes
		if end >= len(runes) {
			chunks = append(chunks, string(runes[start:]))
			break
		}
		cut := end
		for i := end; i > start+maxRunes/2; i-- {
			switch runes[i-1] {
			case '\n', '。', '！', '？', '!', '?':
				cut = i
				i = start
			}
		}
		chunks = append(chunks, string(runes[start:cut]))
		next := cut - overlapRunes
		if next <= start {
			next = cut
		}
		start = next
	}
	if len(chunks) > summarizeMaxChunks {
		head := chunks[:summarizeMaxChunks-1]
		rest := strings.Join(chunks[summarizeMaxChunks-1:], "\n")
		chunks = append(head, rest)
	}
	return chunks
}

// SummarizeLong maps chunks then reduces. If the full transcript already
// fits one window it is a single Completer call; on timeout or overflow it
// retries a truncated copy. Map failures skip that window; if at least one
// window succeeded, notes are still produced (reduce, or a local stitch).
func SummarizeLong(ctx context.Context, complete Completer, title, transcript string) (Notes, error) {
	if complete == nil {
		return Notes{}, fmt.Errorf("completer missing")
	}
	transcript = strings.TrimSpace(transcript)
	if transcript == "" {
		return Notes{}, fmt.Errorf("empty transcript")
	}
	chunks := SplitTranscript(transcript, SummarizeChunkRunes, SummarizeOverlapRunes)
	if len(chunks) <= 1 {
		return completeOnce(ctx, complete, title, transcript)
	}
	var parts []Notes
	var lastErr error
	total := len(chunks)
	for i, chunk := range chunks {
		if err := ctx.Err(); err != nil {
			lastErr = err
			break
		}
		labeled := fmt.Sprintf("（长会分段 %d/%d）\n%s", i+1, total, chunk)
		notes, err := runComplete(ctx, complete, title, labeled)
		if err != nil {
			notes, err = runComplete(ctx, complete, title, clipRunes(chunk, SummarizeChunkRunes/2))
		}
		if err != nil {
			lastErr = err
			continue
		}
		parts = append(parts, notes)
	}
	if len(parts) == 0 {
		if lastErr == nil {
			lastErr = fmt.Errorf("分段摘要失败")
		}
		truncated, err := completeOnce(ctx, complete, title, clipRunes(transcript, SummarizeChunkRunes))
		if err != nil {
			return Notes{}, lastErr
		}
		return truncated, nil
	}
	if len(parts) == 1 {
		return parts[0], nil
	}
	merged := formatPartialNotes(title, parts)
	if utf8.RuneCountInString(merged) > SummarizeChunkRunes {
		return stitchNotes(title, parts), nil
	}
	reduced, err := runComplete(ctx, complete, title, "以下是分段纪要，请合并为一份完整纪要 JSON。不要重复逐字稿。\n\n"+merged)
	if err != nil {
		return stitchNotes(title, parts), nil
	}
	return reduced, nil
}

func completeOnce(ctx context.Context, complete Completer, title, transcript string) (Notes, error) {
	notes, err := runComplete(ctx, complete, title, transcript)
	if err == nil {
		return notes, nil
	}
	runes := utf8.RuneCountInString(transcript)
	for _, limit := range []int{SummarizeChunkRunes, runes / 2, runes / 4} {
		if limit < 32 {
			continue
		}
		truncated := clipRunes(transcript, limit)
		if truncated == transcript {
			continue
		}
		notes, err2 := runComplete(ctx, complete, title, truncated)
		if err2 == nil {
			return notes, nil
		}
	}
	return Notes{}, err
}

func runComplete(ctx context.Context, complete Completer, title, transcript string) (Notes, error) {
	if err := ctx.Err(); err != nil {
		return Notes{}, err
	}
	chunkCtx, cancel := context.WithTimeout(ctx, summarizeChunkDeadline)
	defer cancel()
	type out struct {
		notes Notes
		err   error
	}
	ch := make(chan out, 1)
	go func() {
		notes, err := complete(chunkCtx, title, transcript)
		ch <- out{notes, err}
	}()
	select {
	case <-chunkCtx.Done():
		return Notes{}, chunkCtx.Err()
	case got := <-ch:
		return got.notes, got.err
	}
}

func formatPartialNotes(fallbackTitle string, parts []Notes) string {
	var b strings.Builder
	for i, part := range parts {
		title := strings.TrimSpace(part.Title)
		if title == "" {
			title = fallbackTitle
		}
		fmt.Fprintf(&b, "## 片段 %d\n标题：%s\n摘要：\n%s\n待办：\n%s\n\n", i+1, title, strings.TrimSpace(part.Summary), strings.TrimSpace(part.Actions))
	}
	return strings.TrimSpace(b.String())
}

func stitchNotes(fallbackTitle string, parts []Notes) Notes {
	notes := Notes{Title: fallbackTitle}
	var summaries []string
	var actions []string
	seenAction := map[string]bool{}
	for _, part := range parts {
		if notes.Title == fallbackTitle {
			if title := strings.TrimSpace(part.Title); title != "" {
				notes.Title = title
			}
		}
		if summary := strings.TrimSpace(part.Summary); summary != "" {
			summaries = append(summaries, summary)
		}
		for _, line := range strings.Split(part.Actions, "\n") {
			line = strings.TrimSpace(line)
			if line == "" || seenAction[line] {
				continue
			}
			seenAction[line] = true
			actions = append(actions, line)
		}
	}
	notes.Summary = strings.Join(summaries, "\n\n")
	notes.Actions = strings.Join(actions, "\n")
	return notes
}
