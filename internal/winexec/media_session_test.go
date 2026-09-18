package winexec

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func TestSMTCTargetAndCapabilities(t *testing.T) {
	raw := []byte(`{"app":"Spotify.exe","status":"Playing","title":"Track","artist":"Artist","verified":true,"shuffle":false,"sessionKey":"Spotify.exe","capabilities":["play","pause","stop","next","previous","seek"],"positionMs":1200,"durationMs":180000}`)
	var result MediaSessionResult
	if err := json.Unmarshal(raw, &result); err != nil {
		t.Fatal(err)
	}
	result.Capabilities = sanitizeSMTCCapabilities(result.Capabilities)
	for _, item := range result.Capabilities {
		if item == "seek" || item == "volume" || item == "set_volume" {
			t.Fatalf("seek/volume must stay disabled: %v", result.Capabilities)
		}
	}
	if result.SessionKey == "" || !SMTCCapabilityAllowed(result.Capabilities, "pause") {
		t.Fatalf("%+v", result)
	}
	if SMTCCapabilityAllowed(result.Capabilities, "seek") {
		t.Fatal("seek capability must not be allowed")
	}
	if err := ValidateMediaSessionAction("seek"); !errors.Is(err, ErrSMTCSeekVolumeDisabled) {
		t.Fatalf("seek: %v", err)
	}
	if err := ValidateMediaSessionAction("set_volume"); !errors.Is(err, ErrSMTCSeekVolumeDisabled) {
		t.Fatalf("volume: %v", err)
	}
	if err := ValidateMediaSessionAction("play"); err != nil {
		t.Fatal(err)
	}
}

func TestSMTCAmbiguousSessionsStayUncertain(t *testing.T) {
	if SMTCCapabilityAllowed(nil, "play") {
		t.Fatal("empty capabilities must not invent play")
	}
	if got := sanitizeSMTCCapabilities([]string{"seek", "play", "play", "volume"}); strings.Join(got, ",") != "play" {
		t.Fatalf("%v", got)
	}
}
