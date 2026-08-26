package app

import (
	"context"
	"encoding/base64"
	"strings"
	"testing"

	"github.com/lunitide/lunitide/internal/voice"
)

func TestVoiceMethodsRefuseCleanlyWhenNotWired(t *testing.T) {
	// The state every test and a fresh host start in. What matters is that
	// each method answers rather than dereferencing a nil service.
	e := NewEngine(providerRepositoryStub{}, "test")

	status := e.Handle(context.Background(), validRequest("voice.status", `{}`))
	if !status.OK {
		t.Fatalf("voice.status = %+v; an unwired engine should still report status", status)
	}
	payload, ok := status.Payload.(map[string]any)
	if !ok || payload["supported"] != false {
		t.Fatalf("voice.status payload = %+v; want supported=false", status.Payload)
	}

	for _, method := range []string{"voice.install", "voice.start"} {
		resp := e.Handle(context.Background(), validRequest(method, `{}`))
		if resp.OK || resp.Error == nil || resp.Error.Code != "VOICE-002" {
			t.Errorf("%s = %+v; want VOICE-002", method, resp)
		}
	}

	// Stop is safe to call at any time, including before anything started —
	// the renderer calls it on unmount and must not have to know.
	stop := e.Handle(context.Background(), validRequest("voice.stop", `{}`))
	if !stop.OK {
		t.Errorf("voice.stop = %+v; want a clean close", stop)
	}
}

func TestVoiceStatusReportsTheDownloadAUserFaces(t *testing.T) {
	e := NewEngine(providerRepositoryStub{}, "test")
	e.SetVoiceService(NewVoiceService(t.TempDir(), ""))

	resp := e.Handle(context.Background(), validRequest("voice.status", `{}`))
	if !resp.OK {
		t.Fatalf("voice.status = %+v", resp)
	}
	payload := resp.Payload.(map[string]any)
	if payload["supported"] != true {
		t.Error("a wired service should report supported")
	}
	if payload["ready"] != false {
		t.Error("an empty root should not report ready")
	}
	// The number a first-run dialog puts in front of the user. Getting it
	// wrong by leaving out the runtime is how "24 MB" becomes a 43 MB wait.
	total, _ := payload["downloadBytes"].(int64)
	if want := voice.Runtime().TotalBytes(); total <= want {
		t.Errorf("downloadBytes = %d; want more than the runtime alone (%d)", total, want)
	}
	if payload["backend"] != "sherpa-onnx" {
		t.Errorf("backend = %v", payload["backend"])
	}
}

func TestVoiceStartWithoutAModelSaysSoRatherThanFailingInside(t *testing.T) {
	e := NewEngine(providerRepositoryStub{}, "test")
	e.SetVoiceService(NewVoiceService(t.TempDir(), ""))

	resp := e.Handle(context.Background(), validRequest("voice.start", `{}`))
	if resp.OK || resp.Error == nil || resp.Error.Code != "VOICE-003" {
		t.Fatalf("voice.start = %+v; want VOICE-003 for a missing model", resp)
	}
	if !resp.Error.Retryable {
		t.Error("a missing model is retryable once it has been downloaded")
	}
}

func TestVoiceStatusOffersTheCaptionModelsToChooseFrom(t *testing.T) {
	e := NewEngine(providerRepositoryStub{}, "test")
	e.SetVoiceService(NewVoiceService(t.TempDir(), ""))

	resp := e.Handle(context.Background(), validRequest("voice.status", `{}`))
	if !resp.OK {
		t.Fatalf("voice.status = %+v", resp)
	}
	models, _ := resp.Payload.(map[string]any)["models"].([]map[string]any)
	if len(models) != len(voice.StreamingModels()) {
		t.Fatalf("models = %+v; want the streaming models", models)
	}
	// Size is the whole reason to prefer one, so a chooser that cannot show
	// it is not offering a choice.
	for _, model := range models {
		if size, _ := model["sizeBytes"].(int64); size <= 0 {
			t.Errorf("model %v has no size", model["id"])
		}
	}
	// The refiner runs after these rather than instead of them; listing it
	// here would present one stage of the pipeline as a rival to another.
	for _, model := range models {
		if model["id"] == voice.DefaultRefiner {
			t.Error("the refiner is not an alternative to the caption model")
		}
	}
}

func TestVoiceSelectSwitchesTheCaptionModel(t *testing.T) {
	e := NewEngine(providerRepositoryStub{}, "test")
	e.SetVoiceService(NewVoiceService(t.TempDir(), voice.ModelZipformerZh14M))

	resp := e.Handle(context.Background(), validRequest("voice.select", `{"modelId":"`+voice.ModelParaformerZhEn+`"}`))
	if !resp.OK {
		t.Fatalf("voice.select = %+v", resp)
	}
	status := e.Handle(context.Background(), validRequest("voice.status", `{}`))
	if got := status.Payload.(map[string]any)["modelId"]; got != voice.ModelParaformerZhEn {
		t.Errorf("modelId after select = %v; want the chosen model", got)
	}
}

func TestVoiceSelectRefusesAModelThatIsNotACaptionModel(t *testing.T) {
	e := NewEngine(providerRepositoryStub{}, "test")
	e.SetVoiceService(NewVoiceService(t.TempDir(), voice.ModelZipformerZh14M))

	for _, id := range []string{"no-such-model", voice.DefaultRefiner, voice.RuntimeSherpa} {
		resp := e.Handle(context.Background(), validRequest("voice.select", `{"modelId":"`+id+`"}`))
		if resp.OK || resp.Error == nil || resp.Error.Code != "VOICE-001" {
			t.Errorf("voice.select %q = %+v; want VOICE-001", id, resp)
		}
	}
}

func TestVoiceInstallRejectsAnUnknownModel(t *testing.T) {
	e := NewEngine(providerRepositoryStub{}, "test")
	e.SetVoiceService(NewVoiceService(t.TempDir(), ""))

	resp := e.Handle(context.Background(), validRequest("voice.install", `{"modelId":"no-such-model"}`))
	if resp.OK || resp.Error == nil || resp.Error.Code != "VOICE-001" {
		t.Fatalf("voice.install = %+v; want VOICE-001", resp)
	}
}

func TestVoiceAppendValidatesBeforeTouchingASession(t *testing.T) {
	e := NewEngine(providerRepositoryStub{}, "test")
	e.SetVoiceService(NewVoiceService(t.TempDir(), ""))

	frame := base64.StdEncoding.EncodeToString(make([]byte, voice.FrameBytes))
	for _, tc := range []struct{ name, payload, code string }{
		{"no session", `{"pcm":"AAAA"}`, "BRIDGE_SCHEMA_INVALID"},
		{"no audio", `{"sessionId":"v1"}`, "BRIDGE_SCHEMA_INVALID"},
		{"audio that is not base64", `{"sessionId":"v1","pcm":"!!!!"}`, "BRIDGE_SCHEMA_INVALID"},
		{"session that does not exist", `{"sessionId":"v1","pcm":"` + frame + `"}`, "VOICE-005"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			resp := e.Handle(context.Background(), validRequest("voice.append", tc.payload))
			if resp.OK || resp.Error == nil || resp.Error.Code != tc.code {
				t.Fatalf("voice.append = %+v; want %s", resp, tc.code)
			}
		})
	}
}

func TestVoiceFinishOnAnUnknownSession(t *testing.T) {
	e := NewEngine(providerRepositoryStub{}, "test")
	e.SetVoiceService(NewVoiceService(t.TempDir(), ""))

	resp := e.Handle(context.Background(), validRequest("voice.finish", `{"sessionId":"gone"}`))
	if resp.OK || resp.Error == nil || resp.Error.Code != "VOICE-005" {
		t.Fatalf("voice.finish = %+v; want VOICE-005", resp)
	}
}

func TestVoiceInstallIsIdempotentWhileRunning(t *testing.T) {
	// Two clicks on "download" must not start two transfers over the same
	// files. Nothing is actually fetched here: the catalogue's real URLs are
	// not reachable from a unit test, so what is checked is that the second
	// call is accepted and reports state rather than racing the first.
	e := NewEngine(providerRepositoryStub{}, "test")
	e.SetVoiceService(NewVoiceService(t.TempDir(), voice.ModelZipformerZh14M))

	first := e.Handle(context.Background(), validRequest("voice.install", `{}`))
	second := e.Handle(context.Background(), validRequest("voice.install", `{}`))
	if !first.OK || !second.OK {
		t.Fatalf("install calls = %+v / %+v; both should be accepted", first, second)
	}
	payload := second.Payload.(map[string]any)
	state, _ := payload["state"].(string)
	if state != "downloading" && state != "failed" && state != "ready" {
		t.Fatalf("state = %q; want a settled or in-flight state", state)
	}
}

func TestVoiceServiceCloseIsSafeToRepeat(t *testing.T) {
	svc := NewVoiceService(t.TempDir(), "")
	svc.Close()
	svc.Close()
}

func TestVoiceErrorMessagesAreBounded(t *testing.T) {
	// Error strings reach a bridge payload with a declared maximum, and a
	// recognizer that fails with a wall of ONNX diagnostics would otherwise
	// blow past it.
	if got := truncate(strings.Repeat("x", 900), 512); len(got) != 512 {
		t.Fatalf("truncate produced %d characters; want 512", len(got))
	}
	if got := truncate("short", 512); got != "short" {
		t.Fatalf("truncate mangled a short string: %q", got)
	}
}
