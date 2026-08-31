package app

import (
	"github.com/lunitide/lunitide/internal/domain/provider"
	"github.com/lunitide/lunitide/internal/tts"
	"github.com/lunitide/lunitide/internal/voice/volcsauc"
)

func prepareVolcSpeechFields(protocol provider.Protocol, baseURL string, models []provider.Model) (string, []provider.Model, error) {
	models = canonicalizeVolcSpeechModels(protocol, models)
	if protocol != provider.ProtocolVolcSpeech {
		return baseURL, models, nil
	}
	canon, err := provider.CanonicalVolcSpeechURL(baseURL)
	if err != nil {
		return "", models, err
	}
	return canon, models, nil
}

func canonicalizeVolcSpeechModels(protocol provider.Protocol, models []provider.Model) []provider.Model {
	if protocol != provider.ProtocolVolcSpeech || len(models) == 0 {
		return models
	}
	out := make([]provider.Model, len(models))
	copy(out, models)
	for i, m := range out {
		switch m.EffectiveKind() {
		case provider.KindASR:
			out[i].ModelID = volcsauc.ResourceIDFromModel(m.ModelID)
		case provider.KindTTS:
			if tts.IsVolcTTSResourceID(m.ModelID) {
				out[i].ModelID = tts.CanonicalTTSResourceID(m.ModelID)
			}
		}
	}
	return out
}
