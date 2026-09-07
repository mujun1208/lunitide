package videounderstand

import (
	"bytes"
	"context"
	"errors"
	"image/png"
	"net"
	"net/netip"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/lunitide/lunitide/internal/networkpolicy"
)

func fixtureVideo(t *testing.T) []byte {
	t.Helper()
	data, err := os.ReadFile("testdata/direct-short.mp4")
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func requireDecoder(t *testing.T) {
	t.Helper()
	for _, name := range []string{"ffmpeg", "ffprobe"} {
		if _, err := exec.LookPath(name); err != nil {
			t.Skip("optional local decoder not installed")
		}
	}
}

func TestDirectVideoDecodesActualAudioAndFrames(t *testing.T) {
	requireDecoder(t)
	sample, err := DecodeMedia(context.Background(), fixtureVideo(t))
	if err != nil {
		t.Fatal(err)
	}
	if sample.DurationSeconds < 1.9 || sample.DurationSeconds > 2.1 || len(sample.AudioPCM) < 60000 || len(sample.AudioPCM) > 70000 || len(sample.Frames) != 3 {
		t.Fatalf("actual decode duration=%v pcm=%d frames=%d reasons=%s/%s", sample.DurationSeconds, len(sample.AudioPCM), len(sample.Frames), sample.AudioReason, sample.FrameReason)
	}
	if bytes.Equal(sample.AudioPCM, make([]byte, len(sample.AudioPCM))) {
		t.Fatal("sine audio became silence")
	}
	for _, frame := range sample.Frames {
		img, err := png.Decode(bytes.NewReader(frame.PNG))
		if err != nil {
			t.Fatal(err)
		}
		r, g, b, _ := img.At(320, 180).RGBA()
		if r < 40000 || g > 5000 || b > 5000 {
			t.Fatalf("frame is not actual red video: %d/%d/%d", r, g, b)
		}
	}
	called := 0
	out := UnderstandDirect(context.Background(), "https://public.example/clip.mp4", DirectOptions{
		Fetch: func(context.Context, string) (networkpolicy.FetchResult, error) {
			return networkpolicy.FetchResult{Status: 200, ContentType: "video/mp4", Body: fixtureVideo(t)}, nil
		},
		Sample: DecodeMedia,
		Transcribe: func(_ context.Context, pcm []byte) (string, error) {
			called++
			if len(pcm) < 60000 {
				t.Fatal("audio not forwarded")
			}
			return "隔离识别器的测试文本", nil
		},
	})
	if called != 1 || len(out.VisionPNG) == 0 || !strings.Contains(out.Output, "隔离识别器的测试文本") || !strings.Contains(out.Output, "不是逐帧观看") {
		t.Fatalf("lost actual evidence: %s", out.Output)
	}
	cfg, err := png.DecodeConfig(bytes.NewReader(out.VisionPNG))
	if err != nil || cfg.Width != 1920 || cfg.Height != 360 {
		t.Fatalf("contact sheet: %+v %v", cfg, err)
	}
}

func TestDirectVideoPreservesFramesWhenLocalASRMissing(t *testing.T) {
	requireDecoder(t)
	out := UnderstandDirect(context.Background(), "https://public.example/clip.mp4", DirectOptions{
		Fetch: func(context.Context, string) (networkpolicy.FetchResult, error) {
			return networkpolicy.FetchResult{Status: 200, Body: fixtureVideo(t)}, nil
		}, Sample: DecodeMedia,
		Transcribe: func(context.Context, []byte) (string, error) { return "", ErrLocalASRMissing },
	})
	if len(out.VisionPNG) == 0 || !strings.Contains(out.Output, "local_asr_missing") || !strings.Contains(out.Output, "ok: true") {
		t.Fatalf("frames discarded: %s", out.Output)
	}
}

func TestDirectVideoBoundsAndFailuresDoNotPretendContent(t *testing.T) {
	for _, tc := range []struct {
		name, reason string
		page         networkpolicy.FetchResult
		err          error
	}{
		{"oversize", "media_too_large", networkpolicy.FetchResult{Status: 200, Body: []byte("short"), Truncated: true}, nil},
		{"login-page", "unsupported_media_type", networkpolicy.FetchResult{Status: 200, ContentType: "text/html", Body: []byte("login")}, nil},
		{"forbidden", "http_403", networkpolicy.FetchResult{Status: 403}, nil},
		{"ssrf", "ssrf_blocked", networkpolicy.FetchResult{}, &networkpolicy.Error{Code: networkpolicy.CodeSSRFBlocked}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			out := UnderstandDirect(context.Background(), "https://public.example/clip.mp4", DirectOptions{Fetch: func(context.Context, string) (networkpolicy.FetchResult, error) { return tc.page, tc.err }, Sample: func(context.Context, []byte) (MediaSample, error) {
				t.Fatal("decoder reached invalid body")
				return MediaSample{}, nil
			}})
			if !strings.Contains(out.Output, "reason: "+tc.reason) || !strings.Contains(out.Output, "ok: false") {
				t.Fatal(out.Output)
			}
		})
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	out := UnderstandDirect(ctx, "https://public.example/clip.mp4", DirectOptions{Fetch: func(context.Context, string) (networkpolicy.FetchResult, error) {
		t.Fatal("cancelled download started")
		return networkpolicy.FetchResult{}, nil
	}, Sample: DecodeMedia})
	if !strings.Contains(out.Output, "cancelled") {
		t.Fatal(out.Output)
	}
	t.Setenv("PATH", t.TempDir())
	_, err := DecodeMedia(context.Background(), fixtureVideo(t))
	if !errors.Is(err, ErrDecoderMissing) {
		t.Fatalf("missing deps: %v", err)
	}
}

func TestDirectVideoLocalDecoderCancellationAndFormatAllowlist(t *testing.T) {
	requireDecoder(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := DecodeMedia(ctx, fixtureVideo(t))
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel: %v", err)
	}
	// A disguised playlist must not make the decoder read local/network targets.
	_, err = DecodeMedia(context.Background(), []byte("#EXTM3U\nhttp://127.0.0.1/private.mp4\n"))
	if err == nil {
		t.Fatal("playlist accepted")
	}
}

func TestDirectVideoCapsAudioCoverageAndPreservesPartialASR(t *testing.T) {
	count := 0
	out := UnderstandDirect(context.Background(), "https://public.example/long.webm", DirectOptions{
		Fetch: func(context.Context, string) (networkpolicy.FetchResult, error) {
			return networkpolicy.FetchResult{Status: 200, Body: []byte("media")}, nil
		},
		Sample: func(context.Context, []byte) (MediaSample, error) {
			return MediaSample{DurationSeconds: 300, AudioPCM: make([]byte, 150*pcmBytesPerSecond), FrameReason: "no_video_track"}, nil
		},
		Transcribe: func(_ context.Context, pcm []byte) (string, error) {
			count++
			if len(pcm) > 30*pcmBytesPerSecond {
				t.Fatal("unbounded ASR chunk")
			}
			if count == 3 {
				return "", errors.New("recognizer stopped")
			}
			return "已识别的片段", nil
		},
	})
	if count != 3 || strings.Count(out.Output, "已识别的片段") != 2 || !strings.Contains(out.Output, "仅前 120 秒") || !strings.Contains(out.Output, "asr_failed") {
		t.Fatal(out.Output)
	}
}

func TestDirectVideoCapsActualOversizedBodyAndRejectsInvalidFrames(t *testing.T) {
	out := UnderstandDirect(context.Background(), "https://public.example/clip.mp4", DirectOptions{
		Fetch: func(context.Context, string) (networkpolicy.FetchResult, error) {
			return networkpolicy.FetchResult{Status: 200, Body: make([]byte, MaxMediaBytes+1)}, nil
		},
		Sample: func(context.Context, []byte) (MediaSample, error) {
			t.Fatal("oversized body decoded")
			return MediaSample{}, nil
		},
	})
	if !strings.Contains(out.Output, "media_too_large") {
		t.Fatal(out.Output)
	}
	if _, err := composeVideoFrames([]VideoFrame{{PNG: []byte("not a frame")}}); err == nil {
		t.Fatal("invalid frame accepted")
	}
	if _, err := composeVideoFrames([]VideoFrame{{PNG: make([]byte, (2<<20)+1)}}); err == nil {
		t.Fatal("oversized frame accepted")
	}
}

func TestDirectVideoShortClipWithoutAudioStillHasRealFrames(t *testing.T) {
	requireDecoder(t)
	sample, err := DecodeMedia(context.Background(), fixtureVideo(t))
	if err != nil {
		t.Fatal(err)
	}
	sample.AudioPCM = nil
	sample.AudioReason = "no_audio_track"
	out := UnderstandDirect(context.Background(), "https://public.example/clip.mp4", DirectOptions{
		Fetch: func(context.Context, string) (networkpolicy.FetchResult, error) {
			return networkpolicy.FetchResult{Status: 200, Body: []byte("video")}, nil
		},
		Sample: func(context.Context, []byte) (MediaSample, error) { return sample, nil },
	})
	if len(out.VisionPNG) == 0 || !strings.Contains(out.Output, "no_audio_track") {
		t.Fatal(out.Output)
	}
}

func TestDetectPublicVideoFilesKeepsExplicitFileTypes(t *testing.T) {
	for _, raw := range []string{"https://public.example/clip.mp4?download=1", "https://public.example/clip.MOV", "https://public.example/clip.webm", "https://public.example/clip.mkv"} {
		if _, platform, ok := DetectShareURL("分析 " + raw); !ok || platform != PlatformDirect {
			t.Fatal(raw)
		}
	}
	for _, raw := range []string{"file:///private.mp4", "https://user:pass@public.example/clip.mp4", "https://public.example/stream.m3u8", "https://public.example/page?video=clip.mp4"} {
		if _, ok := ClassifyDirectURL(raw); ok {
			t.Fatalf("unsupported source accepted: %s", raw)
		}
	}
}

type privateVideoResolver struct{}

func (privateVideoResolver) LookupNetIP(context.Context, string, string) ([]netip.Addr, error) {
	return []netip.Addr{netip.MustParseAddr("127.0.0.1")}, nil
}

func TestDirectVideoPublicTransportRejectsPrivateAddressBeforeDial(t *testing.T) {
	out := UnderstandDirect(context.Background(), "https://public.example/clip.mp4", DirectOptions{
		Fetch: func(ctx context.Context, raw string) (networkpolicy.FetchResult, error) {
			return networkpolicy.Fetch(ctx, raw, networkpolicy.FetchOptions{Resolver: privateVideoResolver{}, MaxBodyBytes: MaxMediaBytes, DialContext: func(context.Context, string, string) (net.Conn, error) {
				t.Fatal("private IP dialed")
				return nil, errors.New("forbidden")
			}})
		},
		Sample: func(context.Context, []byte) (MediaSample, error) {
			t.Fatal("private bytes decoded")
			return MediaSample{}, nil
		},
	})
	if !strings.Contains(out.Output, "ssrf_blocked") {
		t.Fatal(out.Output)
	}
}

func TestDirectVideoProcessOutputLimit(t *testing.T) {
	requireDecoder(t)
	ffmpeg, _ := exec.LookPath("ffmpeg")
	_, err := runMediaCommand(context.Background(), time.Second, ffmpeg, []string{"-version"}, 1)
	if err == nil {
		t.Fatal("decoder output cap ignored")
	}
}
