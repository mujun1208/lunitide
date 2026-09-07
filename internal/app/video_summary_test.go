package app

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/png"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/lunitide/lunitide/internal/networkpolicy"
	"github.com/lunitide/lunitide/internal/videounderstand"
)

func TestDirectVideoLongTranscriptKeepsBoundariesAfterChatSummaryClip(t *testing.T) {
	for _, failLast := range []bool{false, true} {
		t.Run(map[bool]string{false: "all-four", true: "partial-failure"}[failLast], func(t *testing.T) {
			calls := 0
			out := videounderstand.UnderstandDirect(context.Background(), "https://public.example/long.mp4", videounderstand.DirectOptions{
				Fetch: func(context.Context, string) (networkpolicy.FetchResult, error) {
					return networkpolicy.FetchResult{Status: 200, Body: []byte("fixture")}, nil
				},
				Sample: func(context.Context, []byte) (videounderstand.MediaSample, error) {
					return videounderstand.MediaSample{DurationSeconds: 180, AudioPCM: make([]byte, 120*32000), FrameReason: "frame_decode_failed"}, nil
				},
				Transcribe: func(context.Context, []byte) (string, error) {
					calls++
					if failLast && calls == 4 {
						return "", errors.New("failed")
					}
					return strings.Repeat("长视频内容需要保留清晰的事实边界。", 800), nil
				},
			})
			clipped := clipToolSummary(out.Output)
			if calls != 4 || len(out.Output) > 3840 || !utf8.ValidString(out.Output) || clipped != out.Output {
				t.Fatalf("calls=%d bytes=%d clipped=%v", calls, len(out.Output), clipped != out.Output)
			}
			for _, want := range []string{"coverage: 仅前 120 秒", "audioRecognizedRanges:", "0.0–30.0 秒", "30.0–60.0 秒", "60.0–90.0 秒", "framesUnavailable: frame_decode_failed", "partialResult: true", "transcriptExcerptTruncated: true", "不是完整逐字稿"} {
				if !strings.Contains(clipped, want) {
					t.Fatalf("lost %q in %s", want, clipped)
				}
			}
			if failLast && !strings.Contains(clipped, "audioUnavailable: asr_failed") {
				t.Fatal("lost partial ASR failure")
			}
			if !failLast && !strings.Contains(clipped, "90.0–120.0 秒") {
				t.Fatal("last recognized range lost")
			}
			if len(out.Transcript) < 16000 || !strings.Contains(out.Transcript, "不能作为完整逐字稿") {
				t.Fatal("saved transcript lacks retained text or original truncation marker")
			}
		})
	}
}

func TestDirectVideoFrameTimesRemainAheadOfLongAudioExcerpt(t *testing.T) {
	var pngBytes bytes.Buffer
	if err := png.Encode(&pngBytes, image.NewRGBA(image.Rect(0, 0, 2, 2))); err != nil {
		t.Fatal(err)
	}
	out := videounderstand.UnderstandDirect(context.Background(), "https://public.example/long.mp4", videounderstand.DirectOptions{
		Fetch: func(context.Context, string) (networkpolicy.FetchResult, error) {
			return networkpolicy.FetchResult{Status: 200, Body: []byte("fixture")}, nil
		},
		Sample: func(context.Context, []byte) (videounderstand.MediaSample, error) {
			return videounderstand.MediaSample{DurationSeconds: 120, AudioPCM: make([]byte, 120*32000), Frames: []videounderstand.VideoFrame{{AtSeconds: 0, PNG: pngBytes.Bytes()}, {AtSeconds: 60, PNG: pngBytes.Bytes()}, {AtSeconds: 119.75, PNG: pngBytes.Bytes()}}}, nil
		},
		Transcribe: func(context.Context, []byte) (string, error) { return strings.Repeat("中文识别内容", 600), nil },
	})
	// The runtime may prepend a sidecar path; it must still fit unchanged.
	output := "transcriptFile: video-transcript-01ARZ3NDEKTSV4RRFFQ69G5FAV.txt（已取得的识别文字，保留分段及截断标记）\n" + out.Output
	if len(output) > 3840 || clipToolSummary(output) != output {
		t.Fatalf("summary over budget: %d", len(output))
	}
	for _, want := range []string{"0.00 秒、60.00 秒、119.75 秒", "画面未分析", "transcriptExcerptTruncated: true", "90.0–120.0 秒"} {
		if !strings.Contains(clipToolSummary(output), want) {
			t.Fatalf("lost %q", want)
		}
	}
}
