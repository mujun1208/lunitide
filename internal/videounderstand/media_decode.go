package videounderstand

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// DecodeMedia uses installed tools only. Remote content never reaches an
// ffmpeg URL parser: pinned HTTP fetch happens before this local-only step.
func DecodeMedia(ctx context.Context, data []byte) (MediaSample, error) {
	ffmpeg, err := exec.LookPath("ffmpeg")
	if err != nil {
		return MediaSample{}, ErrDecoderMissing
	}
	ffprobe, err := exec.LookPath("ffprobe")
	if err != nil {
		return MediaSample{}, ErrDecoderMissing
	}
	if len(data) == 0 || len(data) > MaxMediaBytes {
		return MediaSample{}, errors.New("invalid media size")
	}
	dir, err := os.MkdirTemp("", "lunitide-video-")
	if err != nil {
		return MediaSample{}, err
	}
	defer removeMediaTemp(dir)
	path := filepath.Join(dir, "source.media")
	if err = os.WriteFile(path, data, 0600); err != nil {
		return MediaSample{}, err
	}
	input := []string{"-protocol_whitelist", "file,pipe", "-format_whitelist", "mov,matroska,webm", "-probesize", "8388608", "-analyzeduration", "5000000"}
	probeArgs := append([]string{"-v", "error", "-max_alloc", "67108864"}, input...)
	probeArgs = append(probeArgs, "-show_entries", "format=duration:stream=codec_type,width,height", "-of", "json", path)
	probe, err := runMediaCommand(ctx, 10*time.Second, ffprobe, probeArgs, 64<<10)
	if err != nil {
		return MediaSample{}, err
	}
	var metadata struct {
		Format struct {
			Duration string `json:"duration"`
		} `json:"format"`
		Streams []struct {
			Type   string `json:"codec_type"`
			Width  int    `json:"width"`
			Height int    `json:"height"`
		} `json:"streams"`
	}
	if json.Unmarshal(probe, &metadata) != nil {
		return MediaSample{}, errors.New("invalid media metadata")
	}
	duration, err := strconv.ParseFloat(metadata.Format.Duration, 64)
	if err != nil || duration <= 0 || math.IsNaN(duration) || math.IsInf(duration, 0) {
		return MediaSample{}, errors.New("unknown media duration")
	}
	sample := MediaSample{DurationSeconds: duration}
	var audio, video bool
	for _, stream := range metadata.Streams {
		audio = audio || stream.Type == "audio"
		if stream.Type == "video" {
			if stream.Width <= 0 || stream.Height <= 0 || stream.Width > 8192 || stream.Height > 8192 {
				return MediaSample{}, errors.New("unsupported frame dimensions")
			}
			video = true
		}
	}
	base := append([]string{"-nostdin", "-v", "error", "-max_alloc", "67108864", "-threads", "2"}, input...)
	base = append(base, "-i", path)
	if audio {
		args := append(append([]string{}, base...), "-t", "120", "-map", "0:a:0", "-vn", "-ac", "1", "-ar", "16000", "-c:a", "pcm_s16le", "-f", "s16le", "pipe:1")
		sample.AudioPCM, err = runMediaCommand(ctx, 30*time.Second, ffmpeg, args, int(MaxMediaSeconds)*pcmBytesPerSecond)
		if err != nil {
			sample.AudioPCM = nil
			sample.AudioReason = "audio_decode_failed"
		}
	} else {
		sample.AudioReason = "no_audio_track"
	}
	if video {
		limit := math.Min(duration, MaxMediaSeconds)
		positions := []float64{0, limit / 2, math.Max(0, limit-0.25)}
		for _, position := range positions {
			if ctx.Err() != nil {
				sample.FrameReason = fetchFailureReason(ctx.Err())
				break
			}
			// Fixed demuxer/protocol allowlists forbid network and playlist
			// expansion; bounds cap decoder memory, runtime and output.
			args := append(append([]string{}, base...), "-ss", fmt.Sprintf("%.3f", position), "-map", "0:v:0", "-an", "-frames:v", "1", "-vf", "scale=640:360:force_original_aspect_ratio=decrease,pad=640:360:(ow-iw)/2:(oh-ih)/2", "-threads", "2", "-c:v", "png", "-f", "image2pipe", "pipe:1")
			frame, err := runMediaCommand(ctx, 12*time.Second, ffmpeg, args, 2<<20)
			if err != nil || len(frame) == 0 {
				sample.FrameReason = "frame_decode_failed"
				continue
			}
			sample.Frames = append(sample.Frames, VideoFrame{AtSeconds: position, PNG: frame})
		}
	} else {
		sample.FrameReason = "no_video_track"
	}
	if len(sample.AudioPCM) == 0 && len(sample.Frames) == 0 && ctx.Err() != nil {
		return MediaSample{}, ctx.Err()
	}
	return sample, nil
}

// Removal is limited to the resolved temporary directory created above, with
// a literal path and a validated parent. No user directory is accepted.
func removeMediaTemp(dir string) {
	parent, err := filepath.Abs(os.TempDir())
	if err != nil {
		return
	}
	absolute, err := filepath.Abs(dir)
	if err != nil {
		return
	}
	rel, err := filepath.Rel(parent, absolute)
	if err != nil || filepath.Dir(rel) != "." || !strings.HasPrefix(filepath.Base(rel), "lunitide-video-") {
		return
	}
	_ = os.RemoveAll(absolute)
}

type mediaOutput struct {
	buffer bytes.Buffer
	limit  int
}

func (b *mediaOutput) Write(p []byte) (int, error) {
	if b.buffer.Len()+len(p) > b.limit {
		return 0, errors.New("media output limit exceeded")
	}
	return b.buffer.Write(p)
}

func runMediaCommand(parent context.Context, timeout time.Duration, executable string, args []string, limit int) ([]byte, error) {
	ctx, cancel := context.WithTimeout(parent, timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, executable, args...)
	hideMediaCommand(cmd)
	cmd.WaitDelay = time.Second
	out := &mediaOutput{limit: limit}
	cmd.Stdout = out
	// Decoder diagnostics can contain source metadata. Only bounded, stable
	// failure codes are exposed; no raw media-controlled logs enter prompts.
	cmd.Stderr = io.Discard
	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, errors.New("media decoder failed")
	}
	return out.buffer.Bytes(), nil
}
