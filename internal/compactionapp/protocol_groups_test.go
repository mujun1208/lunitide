package compactionapp

import "testing"

func TestExtractProtocolGroupFactsKeepsToolCallIDs(t *testing.T) {
	msgs := []SummaryMessage{
		{Role: "user", Content: "写周报", Sequence: 1},
		{Role: "user", Content: "[tool-result callId=c1]\nweekly.md ok", Sequence: 2},
		{Role: "user", Content: "昨天开了会", Sequence: 3},
	}
	facts := ExtractProtocolGroupFacts(msgs)
	if len(facts) != 1 || facts[0].Kind != "protocol" || facts[0].Value != "c1" {
		t.Fatalf("complete tool pair must stay as protocol fact: %+v", facts)
	}
	plain := []SummaryMessage{{Role: "user", Content: "昨天开了会", Sequence: 1}}
	if got := ExtractProtocolGroupFacts(plain); len(got) != 0 {
		t.Fatalf("history without groups must stay summarizable: %+v", got)
	}
	summary := `{"summary":"continued","keyPoints":["c1"],"actionItems":[]}`
	if err := ValidateProtectedFacts(summary, facts); err != nil {
		t.Fatal(err)
	}
	if err := ValidateProtectedFacts(`{"summary":"ok"}`, facts); err == nil {
		t.Fatal("summary that drops the tool pair must fail")
	}
}

func TestExtractProtocolGroupFactsFromNativeJSON(t *testing.T) {
	msgs := []SummaryMessage{{
		Role: "assistant",
		Content: `{"tool_calls":[{"id":"call_native","name":"workspace.read"}]}` + "\n" + `tool_call_id":"call_native"`,
	}}
	facts := ExtractProtocolGroupFacts(msgs)
	if len(facts) == 0 || facts[0].Value != "call_native" {
		t.Fatalf("native tool pair JSON must be a protocol fact: %+v", facts)
	}
}
