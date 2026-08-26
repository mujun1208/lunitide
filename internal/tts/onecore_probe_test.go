//go:build windows

package tts

import (
	"testing"
)

// TestOneCoreNaturalVoicesProbe reports what the natural-voice path actually
// sees, which is the difference between "this machine has no neural voices"
// and "it has them and we cannot reach them".
//
// Those two look identical from the settings screen — both end as 没有自然语音 —
// and only one of them is ours to fix.
func TestOneCoreNaturalVoicesProbe(t *testing.T) {
	if testing.Short() {
		t.Skip("SAPI probe skipped in -short")
	}
	if err := mirrorOneCoreTokens(); err != nil {
		t.Logf("mirror: %v", err)
	}
	pool := mirroredOneCoreNames()
	t.Logf("OneCore tokens in HKLM: %d", len(pool))
	for name := range pool {
		t.Logf("  pool: %s", name)
	}

	all, err := desktopVoices()
	if err != nil {
		t.Skipf("no SAPI on this machine: %v", err)
	}
	t.Logf("SAPI enumerated: %d", len(all))
	for _, v := range all {
		t.Logf("  sapi: %s  (%s)", v.VoiceID, v.DisplayName)
	}

	natural, err := (sapiEngine{}).NaturalVoices()
	t.Logf("NaturalVoices: %d err=%v", len(natural), err)
	for _, v := range natural {
		t.Logf("  natural: %s  (%s)", v.VoiceID, v.DisplayName)
	}
}
