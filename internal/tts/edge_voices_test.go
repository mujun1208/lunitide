package tts

import "testing"

func TestEdgeMandarinPresetCatalog(t *testing.T) {
	if len(edgeMandarinPresets) != 50 {
		t.Fatalf("preset count = %d, want 50", len(edgeMandarinPresets))
	}
	seen := map[string]bool{}
	female, male := 0, 0
	for _, row := range edgeMandarinPresets {
		id := edgeVoiceStyleID(row.BaseVoice, row.Style)
		if seen[id] {
			t.Fatalf("duplicate voice id %s", id)
		}
		seen[id] = true
		switch row.Gender {
		case "female":
			female++
		case "male":
			male++
		default:
			t.Fatalf("bad gender %q for %s", row.Gender, id)
		}
	}
	if female != 25 || male != 25 {
		t.Fatalf("gender split female=%d male=%d, want 25/25", female, male)
	}
}

func TestExpandEdgeMandarinVoicesRequiresAPIBases(t *testing.T) {
	api := []Voice{{VoiceID: "zh-CN-XiaoxiaoNeural", Lang: "zh-CN"}}
	out := expandEdgeMandarinVoices(api)
	if len(out) != 13 {
		t.Fatalf("expanded=%d, want 13 Xiaoxiao presets only", len(out))
	}
	if out[0].VoiceID != edgeVoiceStyleID(edgeDefaultVoice, "chat") {
		t.Fatalf("first=%+v", out[0])
	}
}

func TestEdgeApplyStyleVoice(t *testing.T) {
	in := SynthesizeInput{VoiceID: edgeVoiceStyleID("zh-CN-YunxiNeural", "cheerful")}
	edgeApplyStyleVoice(&in)
	if in.VoiceID != "zh-CN-YunxiNeural" || in.Style != "cheerful" {
		t.Fatalf("got voice=%q style=%q", in.VoiceID, in.Style)
	}
}
