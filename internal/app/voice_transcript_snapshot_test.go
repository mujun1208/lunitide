package app

import (
	"context"
	"testing"

	"github.com/lunitide/lunitide/internal/voice"
)

type snapshotVoiceSession struct{ transcript voice.Transcript }

func (s *snapshotVoiceSession) Append(context.Context, []byte) error   { return nil }
func (s *snapshotVoiceSession) Finish(context.Context) (string, error) { return s.transcript.Text, nil }
func (s *snapshotVoiceSession) Close() error                           { return nil }
func (s *snapshotVoiceSession) LatestTranscript() voice.Transcript     { return s.transcript }

func TestVoiceAppendPreservesOptionalUtteranceTimelineThroughCapabilityScope(t *testing.T) {
	e := NewEngine(providerRepositoryStub{}, "test")
	e.voice = &VoiceService{sessions: map[string]voice.Session{}}
	tr := voice.Transcript{Text: "现在能听到我说话吗？", Final: true, Utterances: []voice.Utterance{{Text: "现在能听到我说话吗？", StartMs: 1500, EndMs: 3800, Final: true}}}
	id, err := e.startScopedVoice(context.Background(), func(context.Context) (voice.Session, error) { return &snapshotVoiceSession{transcript: tr}, nil })
	if err != nil {
		t.Fatal(err)
	}
	defer e.dropVoiceSession(id)
	response := e.Handle(context.Background(), validRequest("voice.append", `{"sessionId":"`+id+`","pcm":"AAAA"}`))
	if !response.OK {
		t.Fatalf("append: %+v", response.Error)
	}
	result := response.Payload.(map[string]any)
	utterances, ok := result["utterances"].([]voice.Utterance)
	if !ok || len(utterances) != 1 || utterances[0].StartMs != 1500 || result["text"] != tr.Text {
		t.Fatalf("timeline lost: %+v", result)
	}
}
