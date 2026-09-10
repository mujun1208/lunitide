package modelfit

import (
	"encoding/json"
	"testing"
)

func TestClassifyCompletenessNeverDefaultsToNative(t *testing.T) {
	if got := ClassifyCompleteness(ProtocolEvidence{}); got != CompletenessLegacyUnknown {
		t.Fatalf("empty evidence = %q, want %s", got, CompletenessLegacyUnknown)
	}
	if got := ClassifyCompleteness(ProtocolEvidence{HasGoal: true, HasToolReceipts: true}); got != CompletenessStructuredOnly {
		t.Fatalf("structured work = %q, want %s", got, CompletenessStructuredOnly)
	}
	if got := ClassifyCompleteness(ProtocolEvidence{
		HasGoal: true, HasToolReceipts: true, HasMessageGroup: true, HasObtainedPrivate: true,
	}); got != CompletenessStructuredOnly {
		t.Fatalf("obtained private without qualified codec = %q, want structured_only", got)
	}
	if got := ClassifyCompleteness(ProtocolEvidence{
		HasGoal: true, HasMessageGroup: true, HasObtainedPrivate: true, CodecQualified: true,
	}); got != CompletenessNativeComplete {
		t.Fatalf("qualified native = %q, want native_complete", got)
	}
}

func TestCaptureProtocolFieldsDoesNotForgePrivateState(t *testing.T) {
	got := CaptureProtocolFields(ObtainedProtocol{})
	if got.ReasoningContent != "" || got.Complete || got.Source != "" {
		t.Fatalf("empty obtain forged fields: %+v", got)
	}
	got = CaptureProtocolFields(ObtainedProtocol{ReasoningContent: "  chain  ", Source: SourceProvider})
	if got.ReasoningContent != "  chain  " || !got.Complete || got.Source != SourceProvider {
		t.Fatalf("provider reasoning not preserved: %+v", got)
	}
	got = CaptureProtocolFields(ObtainedProtocol{ReasoningContent: "guess", Source: "log"})
	if got.ReasoningContent != "" || got.Complete || got.Source != "" {
		t.Fatalf("non-provider source must not become protocol state: %+v", got)
	}
}

func TestEnvelopeJSONRoundTripKeepsUnknownRefs(t *testing.T) {
	raw := []byte(`{"schemaVersion":"1","completeness":"structured_only","sessionId":"s1","s2Only":{"keep":true}}`)
	env, err := DecodeEnvelope(raw)
	if err != nil {
		t.Fatal(err)
	}
	if env.Completeness != CompletenessStructuredOnly || env.SessionID != "s1" {
		t.Fatalf("decoded %#v", env)
	}
	out, err := EncodeEnvelope(env)
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]json.RawMessage
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatal(err)
	}
	if string(got["s2Only"]) != `{"keep":true}` {
		t.Fatalf("unknown field dropped: %s", out)
	}
}

func TestLegacyCheckpointClassifiesWithoutNativeClaim(t *testing.T) {
	if got := CompletenessFromCheckpoint(nil); got != CompletenessLegacyUnknown {
		t.Fatalf("nil checkpoint = %q", got)
	}
	if got := CompletenessFromCheckpoint(&ContinuationEnvelope{}); got != CompletenessLegacyUnknown {
		t.Fatalf("empty envelope claimed %q", got)
	}
}
