package app

import (
	"testing"

	"github.com/lunitide/lunitide/internal/domain/provider"
)

func TestPrepareVolcSpeechFieldsCanonicalizesOfficialDocValues(t *testing.T) {
	models := []provider.Model{
		{ModelID: "doubao-seed-asr-2.0", DisplayName: "听", IsDefault: true, Kind: provider.KindASR},
		{ModelID: "seedtts-2.0", DisplayName: "读", Kind: provider.KindTTS},
		{ModelID: "zh_female_xiaohe_uranus_bigtts", DisplayName: "小何", Kind: provider.KindTTS},
	}
	url, got, err := prepareVolcSpeechFields(provider.ProtocolVolcSpeech, "wss://openspeech.bytedance.com/api/v3/plan/sauc/bigmodel_async", models)
	if err != nil {
		t.Fatal(err)
	}
	if url != provider.VolcSpeechOrigin {
		t.Fatalf("url=%s", url)
	}
	if got[0].ModelID != "volc.seedasr.sauc.duration" || got[1].ModelID != "seed-tts-2.0" || got[2].ModelID != "zh_female_xiaohe_uranus_bigtts" {
		t.Fatalf("models=%#v", got)
	}
}

func boolPtr(v bool) *bool { return &v }

func TestApplyProviderPatchKeepsVolcCredentialWhenPastingDocURLs(t *testing.T) {
	item := provider.Provider{
		ID:              "01ARZ3NDEKTSV4RRFFQ69G5FAV",
		Name:            "volc.seedasr.sauc.duration",
		Protocol:        provider.ProtocolVolcSpeech,
		BaseURL:         "wss://openspeech.bytedance.com/api/v3/plan/sauc/bigmodel_async",
		CredentialRef:   "ref-volc",
		CredentialState: provider.CredentialConfigured,
		Status:          provider.StatusEnabled,
		Models: []provider.Model{{
			ModelID: "volc.seedasr.sauc.duration", DisplayName: "seed-asr 2.0",
			IsDefault: true, Kind: provider.KindASR, KindDefault: true,
		}},
	}
	name := item.Name
	wss := "wss://openspeech.bytedance.com/api/v3/plan/tts/bidirection"
	got, err := applyProviderPatch(item, updateProviderPayload{
		Name:    &name,
		BaseURL: &wss,
		Models: &[]modelPayload{
			{ModelID: "doubao-seed-asr-2.0", DisplayName: "豆包流式语音识别 2.0", IsDefault: boolPtr(true), Kind: "asr", KindDefault: true},
			{ModelID: "seedtts-2.0", DisplayName: "豆包语音合成 2.0", IsDefault: boolPtr(false), Kind: "tts", KindDefault: true},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.BaseURL != provider.VolcSpeechOrigin {
		t.Fatalf("url=%s", got.BaseURL)
	}
	if got.CredentialState != provider.CredentialConfigured || got.CredentialRef != "ref-volc" {
		t.Fatalf("credential mutated: %+v", got)
	}
	if len(got.Models) != 2 || got.Models[0].ModelID != "volc.seedasr.sauc.duration" || got.Models[1].ModelID != "seed-tts-2.0" {
		t.Fatalf("models=%#v", got.Models)
	}
}

func TestVolcListenModelIDPrefersASROverDefaultTTS(t *testing.T) {
	p := provider.Provider{Models: []provider.Model{
		{ModelID: "zh_female_xiaohe_uranus_bigtts", DisplayName: "小何", IsDefault: true, Kind: provider.KindTTS},
		{ModelID: "volc.seedasr.sauc.duration", DisplayName: "听", Kind: provider.KindASR, KindDefault: true},
	}}
	if got := volcListenModelID(p); got != "volc.seedasr.sauc.duration" {
		t.Fatalf("got %s", got)
	}
	if volcListenModelID(provider.Provider{Models: []provider.Model{
		{ModelID: "seed-tts-2.0", IsDefault: true, Kind: provider.KindTTS},
	}}) != "" {
		t.Fatal("TTS-only provider must not yield a listen model")
	}
}

func TestPrepareVolcSpeechFieldsLeavesChatProvidersAlone(t *testing.T) {
	models := []provider.Model{{ModelID: "deepseek-chat", DisplayName: "Chat", IsDefault: true}}
	url, got, err := prepareVolcSpeechFields(provider.ProtocolOpenAICompatible, "https://api.deepseek.com", models)
	if err != nil || url != "https://api.deepseek.com" || got[0].ModelID != "deepseek-chat" {
		t.Fatalf("url=%s models=%#v err=%v", url, got, err)
	}
}
