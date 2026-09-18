package app

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lunitide/lunitide/internal/toolruntime"
	"github.com/lunitide/lunitide/internal/tts"
)

type clipTTSEngine struct{}

func (clipTTSEngine) Voices() ([]tts.Voice, error) {
	return []tts.Voice{{VoiceID: "v1", DisplayName: "测试音色", Lang: "zh"}}, nil
}

func (clipTTSEngine) Synthesize(in tts.SynthesizeInput) (tts.SynthesizeResult, bool, error) {
	pcm := []byte{1, 0, 2, 0}
	if strings.Contains(in.Text, "第二段") {
		pcm = []byte{3, 0, 4, 0}
	}
	wav := tts.PCM16MonoWAV(16000, pcm)
	return tts.SynthesizeResult{WavBase64: base64.StdEncoding.EncodeToString(wav), DurationHint: 0.2}, false, nil
}

func TestInvokeAudioGenerateUsesTTSAndSavesWav(t *testing.T) {
	e := NewEngineWithGateway(nil, "test", streamTestLease{})
	e.SetM9TtsService(tts.NewService(clipTTSEngine{}))
	runtime, err := toolruntime.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { runtime.Close() })
	e.tools = runtime

	out, err := e.invokeAudioGenerate(context.Background(), chatAttachmentSessionID, json.RawMessage(`{"lyrics":"春风又绿江南岸","path":"song.wav"}`))
	if err != nil || out.Artifact == nil || out.Artifact.Kind != "audio" {
		t.Fatalf("%+v %v", out, err)
	}
	if !strings.Contains(out.Output, "tts-speech") || !strings.Contains(out.Output, "不是演唱成曲") {
		t.Fatalf("must label TTS honestly: %s", out.Output)
	}
	root, err := runtime.SessionFolder(chatAttachmentSessionID)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(root, out.Artifact.Path))
	if err != nil || !strings.HasPrefix(string(raw), "RIFF") {
		t.Fatalf("saved wav missing: %v", err)
	}
	longLyrics := strings.Repeat("第二段继续唱。", 80)
	payload, _ := json.Marshal(map[string]string{"lyrics": longLyrics, "path": "long.wav"})
	longOut, err := e.invokeAudioGenerate(context.Background(), chatAttachmentSessionID, payload)
	if err != nil || longOut.Artifact == nil {
		t.Fatalf("%+v %v", longOut, err)
	}
	longRaw, err := os.ReadFile(filepath.Join(root, longOut.Artifact.Path))
	if err != nil || len(longRaw) <= len(raw) {
		t.Fatalf("long lyrics must concatenate clips: short=%d long=%d err=%v", len(raw), len(longRaw), err)
	}
}

func TestInvokeAudioGenerateFailsWithoutEngine(t *testing.T) {
	e := NewEngineWithGateway(nil, "test", streamTestLease{})
	runtime, err := toolruntime.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { runtime.Close() })
	e.tools = runtime
	_, err = e.invokeAudioGenerate(context.Background(), chatAttachmentSessionID, json.RawMessage(`{"prompt":"你好"}`))
	if err == nil || !strings.Contains(err.Error(), "本机无可用语音合成引擎") {
		t.Fatalf("want engine error, got %v", err)
	}
}

func TestEngineToolsIncludeAudioGenerate(t *testing.T) {
	found := false
	for _, d := range engineToolDefinitions() {
		if d.Name == "audio.generate" {
			found = true
			if !strings.Contains(d.Description, "朗读") && !strings.Contains(strings.ToLower(d.Description), "speech") {
				t.Fatalf("description must say it is speech, not singing: %s", d.Description)
			}
		}
	}
	if !found {
		t.Fatal("audio.generate missing from engine tools")
	}
}
