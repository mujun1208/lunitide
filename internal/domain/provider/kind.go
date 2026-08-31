package provider

import (
	"sort"
	"strings"
)

// Kind is the catalog a stored model belongs to. Chat pickers only expose LLM.
type Kind string

const (
	KindLLM    Kind = "llm"
	KindVision Kind = "vision"
	KindImage  Kind = "image"
	KindVideo  Kind = "video"
	KindVoice  Kind = "voice" // leftover listen rows; NormalizeKind maps to asr
	KindASR    Kind = "asr"
	KindTTS    Kind = "tts"
)

// NormalizeKind maps empty/unknown values to llm so pre-0096 rows stay chat models.
// Leftover kind=voice is listen (asr). Speak is a separate tts row.
func NormalizeKind(raw string) Kind {
	switch Kind(strings.ToLower(strings.TrimSpace(raw))) {
	case KindVision:
		return KindVision
	case KindImage:
		return KindImage
	case KindVideo:
		return KindVideo
	case KindVoice, KindASR:
		return KindASR
	case KindTTS:
		return KindTTS
	default:
		return KindLLM
	}
}

// ValidKind reports whether raw is empty (legacy llm) or one of the catalogs.
// voice stays valid so old clients can still write leftover listen rows.
func ValidKind(raw string) bool {
	if strings.TrimSpace(raw) == "" {
		return true
	}
	switch Kind(raw) {
	case KindLLM, KindVision, KindImage, KindVideo, KindVoice, KindASR, KindTTS:
		return true
	default:
		return false
	}
}

// IsSpeechKind is asr, tts, or leftover voice. Volc speech providers may only
// carry these; LLM stays on chat.prefer.
func IsSpeechKind(k Kind) bool {
	switch NormalizeKind(string(k)) {
	case KindASR, KindTTS:
		return true
	default:
		return k == KindVoice
	}
}

// EffectiveKind is llm when Kind is unset. Leftover voice reads as asr.
func (m Model) EffectiveKind() Kind {
	if m.Kind == "" {
		return KindLLM
	}
	return NormalizeKind(string(m.Kind))
}

// kindMatchesCatalog reports whether a stored row belongs in the requested catalog.
// KindVoice is the 语音模型 tab: asr + tts + leftover voice. KindASR is listen only.
func kindMatchesCatalog(have, want Kind) bool {
	if want == KindVoice {
		return IsSpeechKind(have)
	}
	return NormalizeKind(string(have)) == NormalizeKind(string(want))
}

// CatalogEntry is one enabled, credentialed model in a kind catalog.
type CatalogEntry struct {
	Provider Provider
	Model    Model
}

// CatalogForKind returns default-then-backups for a kind across enabled providers.
func CatalogForKind(items []Provider, kind Kind) []CatalogEntry {
	out := make([]CatalogEntry, 0)
	for _, p := range items {
		if p.Status != StatusEnabled || p.CredentialState != CredentialConfigured || p.CredentialRef == "" {
			continue
		}
		for _, m := range p.Models {
			if !kindMatchesCatalog(m.EffectiveKind(), kind) {
				continue
			}
			out = append(out, CatalogEntry{Provider: p, Model: m})
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if a.Model.KindDefault != b.Model.KindDefault {
			return a.Model.KindDefault
		}
		if a.Model.IsDefault != b.Model.IsDefault {
			return a.Model.IsDefault
		}
		if !a.Provider.CreatedAt.Equal(b.Provider.CreatedAt) {
			return a.Provider.CreatedAt.Before(b.Provider.CreatedAt)
		}
		if a.Provider.ID != b.Provider.ID {
			return a.Provider.ID < b.Provider.ID
		}
		return a.Model.ModelID < b.Model.ModelID
	})
	return out
}

// VisionDescribeCatalog is KindVision first, then LLM rows marked SupportsVision
// (except skipModelID, usually the active chat model). Used when the chat LLM
// cannot see images and a dedicated OCR/VLM must describe them.
func VisionDescribeCatalog(items []Provider, skipModelID string) []CatalogEntry {
	skipModelID = strings.TrimSpace(skipModelID)
	seen := map[string]bool{}
	out := make([]CatalogEntry, 0)
	add := func(entry CatalogEntry) {
		if skipModelID != "" && entry.Model.ModelID == skipModelID {
			return
		}
		key := entry.Provider.ID + "\x00" + entry.Model.ModelID
		if seen[key] {
			return
		}
		seen[key] = true
		out = append(out, entry)
	}
	for _, entry := range CatalogForKind(items, KindVision) {
		add(entry)
	}
	for _, p := range items {
		if p.Status != StatusEnabled || p.CredentialState != CredentialConfigured || p.CredentialRef == "" {
			continue
		}
		for _, m := range p.Models {
			if !m.SupportsVision || m.EffectiveKind() != KindLLM {
				continue
			}
			add(CatalogEntry{Provider: p, Model: m})
		}
	}
	return out
}
