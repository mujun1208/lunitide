package app

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lunitide/lunitide/internal/bridge"
	"github.com/lunitide/lunitide/internal/domain/provider"
	"github.com/lunitide/lunitide/internal/llmadapter"
	"github.com/lunitide/lunitide/internal/networkpolicy"
	"github.com/lunitide/lunitide/internal/toolruntime"
	"github.com/lunitide/lunitide/internal/videounderstand"
)

func TestTypedDirectVideoActuallyDecodesAndSuppliesAudioAndVisionToModel(t *testing.T) {
	for _, name := range []string{"ffmpeg", "ffprobe"} {
		if _, err := exec.LookPath(name); err != nil {
			t.Skip("optional installed decoder missing")
		}
	}
	fixture, err := os.ReadFile(filepath.Join("..", "videounderstand", "testdata", "direct-short.mp4"))
	if err != nil {
		t.Fatal(err)
	}
	for _, rejectImages := range []bool{false, true} {
		t.Run(map[bool]string{false: "vision", true: "unsupported-vision"}[rejectImages], func(t *testing.T) {
			e := NewEngineWithGateway(chatAttachmentProvider{}, "test", streamTestLease{})
			r, err := toolruntime.New(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = r.Close() })
			e.SetToolRuntime(r)
			fetches, decodes, calls := 0, 0, 0
			r.SetVideoReader(func(context.Context, string) (networkpolicy.FetchResult, error) {
				fetches++
				return networkpolicy.FetchResult{Status: 200, ContentType: "video/mp4", Body: fixture}, nil
			}, func(_ context.Context, pcm []byte) (string, error) {
				decodes++
				if len(pcm) < 60000 {
					t.Fatal("real extracted PCM not forwarded")
				}
				return "实际音轨交给本地识别器后的隔离文本", nil
			})
			adapter := &routedExecutionAdapter{stream: func(req llmadapter.Request) (llmadapter.Response, error) {
				calls++
				if calls == 1 {
					if !routedRequestHasTool(req, "video.understand") || !strings.Contains(req.Messages[0].Content, "[本轮视频链接]") {
						return llmadapter.Response{}, errors.New("direct video route missing")
					}
					return llmadapter.Response{Message: llmadapter.Message{Role: llmadapter.RoleAssistant, Content: "我先读取这个视频。", ToolCalls: []llmadapter.ToolCall{{ID: "direct-video", Name: "video.understand", Arguments: json.RawMessage(`{"url":"https://public.example/short.mp4"}`)}}}}, nil
				}
				if calls == 2 {
					if len(req.Images) != 1 || req.Images[0].MIME != "image/png" || len(req.Images[0].Data) == 0 {
						return llmadapter.Response{}, errors.New("actual frame sheet not passed to model")
					}
					if rejectImages {
						return llmadapter.Response{}, &llmadapter.Error{HTTPStatus: 400, Message: "This model does not support image"}
					}
				}
				var evidence strings.Builder
				for _, message := range req.Messages {
					evidence.WriteString(message.Content)
				}
				if !strings.Contains(evidence.String(), "实际音轨交给本地识别器后的隔离文本") || !strings.Contains(evidence.String(), "source: direct_media") {
					return llmadapter.Response{}, errors.New("actual transcript lost")
				}
				if rejectImages && (len(req.Images) != 0 || !strings.Contains(evidence.String(), "画面未被读取")) {
					return llmadapter.Response{}, errors.New("unread frames not disclosed after fallback")
				}
				return llmadapter.Response{Message: llmadapter.Message{Role: llmadapter.RoleAssistant, Content: "依据实际音轨识别整理，画面仅有抽样或当前不可读。"}}, nil
			}}
			e.SetAdapterFactoryForTest(func(context.Context, provider.Provider) (llmadapter.Adapter, error) { return adapter, nil })
			events := make(chan bridge.Event, 256)
			payload := `{"providerId":"` + chatAttachmentProviderID + `","modelId":"model","sessionId":"` + chatAttachmentSessionID + `","executionMode":"full-access","messages":[{"role":"user","content":"分析这个视频 https://public.example/short.mp4"}]}`
			response := e.HandleStreaming(context.Background(), validRequest("chat.start", payload), func(event bridge.Event) error { events <- event; return nil })
			if !response.OK {
				t.Fatalf("start failed: %+v", response)
			}
			frames := collectFramedChatEvents(t, response, events)
			if last := frames[len(frames)-1]; last.Type != bridge.EventCompleted {
				t.Fatalf("terminal %s: %+v calls=%d", last.Type, last.Error, calls)
			}
			artifact := false
			for _, event := range frames {
				if event.Type == bridge.EventApprovalRequired {
					t.Fatal("read-only video requested unrelated approval")
				}
				if event.Type == bridge.EventToolCompleted && event.Tool != nil && event.Tool.CallID == "direct-video" && event.Tool.Artifact != nil {
					artifact = event.Tool.Artifact.Kind == "image" && strings.HasSuffix(event.Tool.Artifact.Path, ".png")
				}
			}
			wantCalls := 2
			if rejectImages {
				wantCalls = 3
			}
			if fetches != 1 || decodes != 1 || calls != wantCalls || !artifact {
				t.Fatalf("fetch=%d ASR=%d model=%d artifact=%v", fetches, decodes, calls, artifact)
			}
		})
	}
}

func TestVideoAudioMissingLocalModelDoesNotInstallOrUseProvider(t *testing.T) {
	e := NewEngine(nil, "test")
	e.SetVoiceService(NewVoiceService(t.TempDir(), ""))
	t.Cleanup(e.voice.Close)
	_, err := e.TranscribeVideoAudio(context.Background(), make([]byte, 32000))
	if !errors.Is(err, videounderstand.ErrLocalASRMissing) {
		t.Fatalf("unexpected missing model result: %v", err)
	}
}
