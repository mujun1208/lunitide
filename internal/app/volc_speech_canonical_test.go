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

func TestPrepareVolcSpeechFieldsLeavesChatProvidersAlone(t *testing.T) {
	models := []provider.Model{{ModelID: "deepseek-chat", DisplayName: "Chat", IsDefault: true}}
	url, got, err := prepareVolcSpeechFields(provider.ProtocolOpenAICompatible, "https://api.deepseek.com", models)
	if err != nil || url != "https://api.deepseek.com" || got[0].ModelID != "deepseek-chat" {
		t.Fatalf("url=%s models=%#v err=%v", url, got, err)
	}
}
