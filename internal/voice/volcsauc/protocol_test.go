package volcsauc

import (
	"bytes"
	"fmt"
	"testing"
)

func TestEncodeDecodeFullClientRoundTrip(t *testing.T) {
	body := []byte(`{"user":{"uid":"lunitide"}}`)
	raw := EncodeFullClient(1, body)
	frame, err := DecodeFrame(raw)
	if err != nil {
		t.Fatal(err)
	}
	if frame.Type != msgFullClient || !frame.HasSeq || frame.Sequence != 1 {
		t.Fatalf("frame = %+v", frame)
	}
	if !bytes.Equal(frame.JSON, body) {
		t.Fatalf("json = %s", frame.JSON)
	}
}

func TestEncodeDecodeAudioLastUsesNegativeSequence(t *testing.T) {
	pcm := bytes.Repeat([]byte{0x01, 0x00}, 160) // 10ms of silence-ish
	raw := EncodeAudio(4, pcm, true)
	frame, err := DecodeFrame(raw)
	if err != nil {
		t.Fatal(err)
	}
	if frame.Type != msgAudioOnly || frame.Flags != flagNegWithSeq || frame.Sequence != -4 {
		t.Fatalf("frame = %+v", frame)
	}
	if !bytes.Equal(frame.Raw, pcm) {
		t.Fatalf("pcm round-trip failed: %d vs %d", len(frame.Raw), len(pcm))
	}
}

func TestTranscriptFromJSONUsesDefinite(t *testing.T) {
	partial := []byte(`{"result":{"text":"打开网","utterances":[{"text":"打开网","definite":false}]}}`)
	text, final, ok := TranscriptFromJSON(partial)
	if !ok || text != "打开网" || final {
		t.Fatalf("partial: %q %v %v", text, final, ok)
	}
	done := []byte(`{"result":{"text":"打开网络。","utterances":[{"text":"打开网络。","definite":true}]}}`)
	text, final, ok = TranscriptFromJSON(done)
	if !ok || text != "打开网络。" || !final {
		t.Fatalf("final: %q %v %v", text, final, ok)
	}
}

func TestDecodeErrorFrame(t *testing.T) {
	// Error packets: header + code + size + gzip json.
	payload := gzipBytes([]byte(`{"message":"forbidden"}`))
	raw := []byte{
		(protocolVersion << 4) | headerSizeUnits,
		(msgErrorServer << 4),
		(serialJSON << 4) | compressGzip,
		0,
	}
	var code [4]byte
	code[3] = 0x01 // 1, just to have a non-zero
	var size [4]byte
	size[3] = byte(len(payload))
	raw = append(raw, code[:]...)
	raw = append(raw, size[:]...)
	raw = append(raw, payload...)
	frame, err := DecodeFrame(raw)
	if err != nil {
		t.Fatal(err)
	}
	if frame.Type != msgErrorServer || frame.Error != 1 {
		t.Fatalf("frame = %+v", frame)
	}
	if !bytes.Contains(frame.JSON, []byte("forbidden")) {
		t.Fatalf("json = %s", frame.JSON)
	}
}

func TestParseCredential(t *testing.T) {
	app, tok := ParseCredential("  only-api-key  ")
	if app != "" || tok != "only-api-key" {
		t.Fatalf("bare: %q %q", app, tok)
	}
	app, tok = ParseCredential("1234567890:access-token")
	if app != "1234567890" || tok != "access-token" {
		t.Fatalf("colon: %q %q", app, tok)
	}
	app, tok = ParseCredential("1234567890\naccess-token")
	if app != "1234567890" || tok != "access-token" {
		t.Fatalf("newline: %q %q", app, tok)
	}
	app, tok = ParseCredential("sk:not-an-app-id")
	if app != "" || tok != "sk:not-an-app-id" {
		t.Fatalf("colon in key: %q %q", app, tok)
	}
}

func TestResourceIDFromModel(t *testing.T) {
	if got := ResourceIDFromModel("seed-asr-2.0"); got != DefaultResourceID {
		t.Fatal(got)
	}
	if got := ResourceIDFromModel("volc.seedasr.sauc.concurrent"); got != "volc.seedasr.sauc.concurrent" {
		t.Fatal(got)
	}
}

func TestStreamURL(t *testing.T) {
	got := StreamURL("https://openspeech.bytedance.com", true)
	if got != "wss://openspeech.bytedance.com/api/v3/plan/sauc/bigmodel_async" {
		t.Fatal(got)
	}
	got = StreamURL("", false)
	if got != "wss://openspeech.bytedance.com/api/v3/plan/sauc/bigmodel" {
		t.Fatal(got)
	}
	got = StreamURL("https://ark.cn-beijing.volces.com/api/plan/v3", true)
	if got != "wss://openspeech.bytedance.com/api/v3/plan/sauc/bigmodel_async" {
		t.Fatal(got)
	}
	got = StreamURL("https://openspeech.bytedance.com/api/v3/sauc/bigmodel_async", true)
	if got != "wss://openspeech.bytedance.com/api/v3/sauc/bigmodel_async" {
		t.Fatal(got)
	}
}

func TestSanitizeProbeError(t *testing.T) {
	msg := SanitizeProbeError(&HandshakeError{Status: 403, Message: "volc.bigasr.sauc.duration"})
	if !bytes.Contains([]byte(msg), []byte("seedasr")) {
		t.Fatal(msg)
	}
	msg = SanitizeProbeError(fmt.Errorf(`acquire failed AuthenticationError`))
	if !bytes.Contains([]byte(msg), []byte("Agent Plan 专属")) {
		t.Fatal(msg)
	}
}

func TestAllowedSpeechHost(t *testing.T) {
	if !AllowedSpeechHost(DefaultHost) || AllowedSpeechHost("evil.example") {
		t.Fatal("host allowlist")
	}
	if !AllowedSpeechHost(hostOf("https://openspeech.bytedance.com:443/api")) {
		t.Fatal("default https port must still be the speech host")
	}
}
