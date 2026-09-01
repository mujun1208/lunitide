package talk

import (
	"strings"
	"testing"
)

func TestRealtimeWebSocketURL(t *testing.T) {
	got, err := RealtimeWebSocketURL("https://api.example.com/v1", "gpt-4o-realtime-preview")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(got, "wss://api.example.com/v1/realtime?") || !strings.Contains(got, "model=gpt-4o-realtime-preview") {
		t.Fatalf("url = %s", got)
	}
	got, err = RealtimeWebSocketURL("http://127.0.0.1:9/openai", "gemini-live")
	if err != nil || !strings.HasPrefix(got, "ws://127.0.0.1:9/openai/realtime?") {
		t.Fatalf("http origin = %s err=%v", got, err)
	}
	if _, err := RealtimeWebSocketURL("", "x"); err == nil {
		t.Fatal("empty base must fail")
	}
}

func TestParseServerEvent(t *testing.T) {
	audio := ParseServerEvent([]byte(`{"type":"response.audio.delta","delta":"AAAA"}`))
	if audio.Kind != "audio" || audio.Audio != "AAAA" {
		t.Fatalf("audio = %+v", audio)
	}
	user := ParseServerEvent([]byte(`{"type":"conversation.item.input_audio_transcription.completed","transcript":"今晚月色如何"}`))
	if user.Kind != "transcript" || user.Role != "user" || user.Transcript != "今晚月色如何" {
		t.Fatalf("user = %+v", user)
	}
	asst := ParseServerEvent([]byte(`{"type":"response.audio_transcript.delta","delta":"月色很好。"}`))
	if asst.Kind != "transcript" || asst.Role != "assistant" {
		t.Fatalf("asst = %+v", asst)
	}
	if ParseServerEvent([]byte(`{"type":"input_audio_buffer.speech_started"}`)).Kind != "barge" {
		t.Fatal("barge")
	}
	if ParseServerEvent([]byte(`not-json`)).Kind != "error" {
		t.Fatal("bad json")
	}
}

func TestSessionMessages(t *testing.T) {
	update := string(SessionUpdateMessage("月汐"))
	if !strings.Contains(update, `"session.update"`) {
		t.Fatal("session.update")
	}
	if !strings.Contains(update, `"whisper-1"`) {
		t.Fatal("session.update must request user transcription")
	}
	if !strings.Contains(string(AppendAudioMessage("AAAA")), `"input_audio_buffer.append"`) {
		t.Fatal("append")
	}
	if !strings.Contains(string(CancelOutputMessage()), `"response.cancel"`) {
		t.Fatal("cancel")
	}
}
