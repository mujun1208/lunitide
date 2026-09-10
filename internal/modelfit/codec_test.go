package modelfit

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestCodecQualifyRequiresFamilyGroupsAndPrivate(t *testing.T) {
	g := MessageGroup{
		Assistant: ProtocolMessage{Role: "assistant", ToolCalls: []ProtocolToolCall{
			{ID: "c1", Name: "workspace.read", Arguments: json.RawMessage(`{}`)},
		}},
		Tools:    []ProtocolMessage{{Role: "tool", ToolCallID: "c1", Content: "ok"}},
		Complete: true,
	}
	priv := ProtocolCapture{ReasoningContent: "think", Source: SourceProvider, Complete: true}
	if !LookupCodec(CodecDeepSeekV1).Qualify(QualifyInput{Family: FamilyDeepSeek, Groups: []MessageGroup{g}, Private: priv}) {
		t.Fatal("deepseek fixture must qualify")
	}
	if LookupCodec(CodecDeepSeekV1).Qualify(QualifyInput{Family: FamilyGLM, Groups: []MessageGroup{g}, Private: priv}) {
		t.Fatal("cross-family must not qualify")
	}
	if LookupCodec(CodecGLMV1).Qualify(QualifyInput{Family: FamilyDeepSeek, Groups: []MessageGroup{g}, Private: priv}) {
		t.Fatal("glm codec must not qualify deepseek")
	}
	if LookupCodec(CodecDeepSeekV1).Qualify(QualifyInput{Family: FamilyDeepSeek, Groups: []MessageGroup{{Assistant: ProtocolMessage{Role: "assistant"}}}, Private: priv}) {
		t.Fatal("incomplete group must not qualify")
	}
	if LookupCodec(CodecDeepSeekV1).Qualify(QualifyInput{Family: FamilyDeepSeek, Groups: []MessageGroup{g}}) {
		t.Fatal("missing provider private must not qualify")
	}
}

func TestDeepSeekAndGLMOfflineRoundTrip(t *testing.T) {
	g := MessageGroup{
		Assistant: ProtocolMessage{
			Role:             "assistant",
			Content:          "先读",
			ReasoningContent: "need file",
			ToolCalls:        []ProtocolToolCall{{ID: "c1", Name: "workspace.read", Arguments: json.RawMessage(`{"path":"a.md"}`)}},
		},
		Tools:    []ProtocolMessage{{Role: "tool", ToolCallID: "c1", Content: "ok"}},
		Complete: true,
	}
	priv := ProtocolCapture{ReasoningContent: "need file", Source: SourceProvider, Complete: true}
	for _, ver := range []string{CodecDeepSeekV1, CodecGLMV1} {
		c := LookupCodec(ver)
		msgs, err := c.EncodeReplay([]MessageGroup{g}, priv)
		if err != nil {
			t.Fatal(err)
		}
		if len(msgs) != 2 || msgs[0].Role != "assistant" || msgs[0].ReasoningContent != "need file" || msgs[0].ToolCalls[0].ID != "c1" || msgs[1].ToolCallID != "c1" {
			t.Fatalf("%s replay lost pairing: %#v", ver, msgs)
		}
		if err := c.RejectCrossFamily(FamilyAnthropic); err == nil {
			t.Fatalf("%s must reject anthropic private fields", ver)
		}
	}
}

func TestUnknownCodecDoesNotQualify(t *testing.T) {
	if LookupCodec("") != nil || LookupCodec("invented") != nil {
		t.Fatal("unknown codec must be nil")
	}
	if ClassifyCompleteness(ProtocolEvidence{HasMessageGroup: true, HasObtainedPrivate: true}) != CompletenessStructuredOnly {
		t.Fatal("no codec still structured_only")
	}
}

func TestEncodeReplayDoesNotInventPrivate(t *testing.T) {
	g := MessageGroup{
		Assistant: ProtocolMessage{Role: "assistant", Content: "hello", ToolCalls: []ProtocolToolCall{
			{ID: "c1", Name: "workspace.read", Arguments: json.RawMessage(`{}`)},
		}},
		Tools:    []ProtocolMessage{{Role: "tool", ToolCallID: "c1", Content: "ok"}},
		Complete: true,
	}
	msgs, err := LookupCodec(CodecDeepSeekV1).EncodeReplay([]MessageGroup{g}, ProtocolCapture{ReasoningContent: "guess", Source: "log"})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(msgs[0].ReasoningContent, "guess") {
		t.Fatalf("non-provider source forged thinking: %#v", msgs[0])
	}
}
