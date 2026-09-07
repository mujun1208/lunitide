package toolruntime

import (
	"context"
	"os"

	"github.com/lunitide/lunitide/internal/videounderstand"
	"github.com/oklog/ulid/v2"
)

// SetVideoReader reuses the authorized pinned fetch transport and existing
// local ASR. It is wired once at startup and never downloads a model.
func (r *Runtime) SetVideoReader(fetch videounderstand.FetchFunc, transcribe func(context.Context, []byte) (string, error)) {
	r.videoFetch, r.videoTranscribe = fetch, transcribe
}

func (r *Runtime) understandDirectVideo(ctx context.Context, mode Mode, session, url string, unconfined bool) (Result, error) {
	direct := videounderstand.UnderstandDirect(ctx, url, videounderstand.DirectOptions{Fetch: r.videoFetch, Sample: videounderstand.DecodeMedia, Transcribe: r.videoTranscribe})
	if ctx.Err() != nil {
		return Result{}, ctx.Err()
	}
	out := result(direct.Output)
	if direct.Transcript != "" {
		name := "video-transcript-" + ulid.Make().String() + ".txt"
		path, err := r.path(mode, session, name, true, unconfined)
		if err == nil {
			err = os.WriteFile(path, []byte(direct.Transcript), 0600)
		}
		if err != nil {
			out = result("transcriptFileUnavailable: 识别文字文件未保存，以下仅为摘录。\n" + direct.Output)
		} else {
			out = result("transcriptFile: " + name + "（已取得的识别文字，保留分段及截断标记）\n" + direct.Output)
		}
	}
	if len(direct.VisionPNG) > 0 {
		name := "video-frames-" + ulid.Make().String() + ".png"
		path, err := r.path(mode, session, name, true, unconfined)
		if err != nil {
			return Result{}, err
		}
		if err = os.WriteFile(path, direct.VisionPNG, 0600); err != nil {
			return Result{}, err
		}
		out.Artifact = &Artifact{Kind: "image", Path: name}
		out.VisionMIME = "image/png"
		out.VisionData = direct.VisionPNG
	}
	return out, nil
}
