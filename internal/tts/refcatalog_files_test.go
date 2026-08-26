package tts

import (
	"errors"
	"os"
	"testing"
)

var errNotWave = errors.New("not a readable RIFF/WAVE file")

// TestRefPresetsResolveToRealFiles reports which catalogue voices have no
// reference audio behind them.
//
// Every preset names a WAV by filename under one of two pack directories.
// A preset whose file is missing is not a degraded voice, it is a synthesis
// failure at the moment the user picks it from the dropdown — and the
// dropdown offers it either way, so the fault surfaces as "this voice just
// doesn't work" with nothing saying why.
//
// Skipped rather than failed when the packs are absent: they are not shipped
// with the build, so their absence is the normal state on any machine that
// is not the one they were assembled on.
func TestRefPresetsResolveToRealFiles(t *testing.T) {
	if _, err := os.Stat(DefaultRefPackDir); err != nil {
		t.Skipf("voice pack not present: %v", err)
	}
	var missing, outsideWindow []string
	for _, preset := range refPresets {
		path, ok := RefResolveVoice(RefPresetVoiceIDPrefix+preset.File, "")
		if !ok {
			t.Errorf("preset %q does not resolve at all", preset.File)
			continue
		}
		info, err := os.Stat(path)
		if err != nil {
			missing = append(missing, preset.File)
			t.Logf("MISSING  %s\n         -> %s", preset.File, path)
			continue
		}
		// The same path synthesis uses, so an over-long clip is measured
		// after trimming rather than as it sits on disk.
		seconds, err := wavSeconds(refPromptClip(path))
		if err != nil {
			outsideWindow = append(outsideWindow, preset.File)
			t.Logf("UNREADABLE  %-28s %v (%d bytes)", preset.File, err, info.Size())
			continue
		}
		// GPT-SoVITS clones from a 3-10s prompt. Outside that it either
		// refuses the request or returns something that does not sound like
		// the voice that was picked.
		if seconds < 3 || seconds > 10 {
			outsideWindow = append(outsideWindow, preset.File)
			t.Logf("OUT OF WINDOW  %-28s %.1fs", preset.File, seconds)
		}
	}
	t.Logf("presets: %d, missing: %d, outside the 3-10s clone window: %d",
		len(refPresets), len(missing), len(outsideWindow))
	if len(missing)+len(outsideWindow) > 0 {
		t.Errorf("%d of %d catalogue voices cannot synthesize", len(missing)+len(outsideWindow), len(refPresets))
	}
}

// wavSeconds reads just enough of a RIFF/WAVE header to time it.
func wavSeconds(path string) (float64, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}
	if len(raw) < 12 || string(raw[0:4]) != "RIFF" || string(raw[8:12]) != "WAVE" {
		return 0, errNotWave
	}
	var byteRate uint32
	for at := 12; at+8 <= len(raw); {
		id := string(raw[at : at+4])
		size := int(uint32(raw[at+4]) | uint32(raw[at+5])<<8 | uint32(raw[at+6])<<16 | uint32(raw[at+7])<<24)
		body := at + 8
		if size < 0 || body+size > len(raw) {
			size = len(raw) - body
		}
		switch id {
		case "fmt ":
			if size >= 16 {
				byteRate = uint32(raw[body+8]) | uint32(raw[body+9])<<8 | uint32(raw[body+10])<<16 | uint32(raw[body+11])<<24
			}
		case "data":
			if byteRate == 0 {
				return 0, errNotWave
			}
			return float64(size) / float64(byteRate), nil
		}
		at = body + size
		if size%2 == 1 {
			at++
		}
	}
	return 0, errNotWave
}
