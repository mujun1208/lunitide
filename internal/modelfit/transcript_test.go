package modelfit

import (
	"bytes"
	"encoding/json"
	"strconv"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestNativeTargetMismatchRebuild(t *testing.T) {
	glm := mustLoadProfile(t, "glm", "standard")
	deepseek := mustLoadProfile(t, "deepseek", "standard")
	glmCoding := mustLoadProfile(t, "glm", "coding")
	taskID := "task-root-fr04"
	budget := json.RawMessage(`{"maxTotalTokens":262144,"consumed":900}`)
	target := r01Target(t, glm, "https://api.example.com/v1", "glm-5.3", "cred-primary")
	snapshot := NativeSnapshot{
		Epoch: NativeEpoch{
			ID: "epoch-fr04", Scope: NativeScope{OwnerScope: "acct-root", SessionID: "session-fr04"},
			Target: target, Profile: glm, State: "active",
		},
		Messages: []NativeMessage{{ID: "m1", TurnID: "t1", Role: "user", Sequence: 1, Complete: true, WireJSON: []byte(`{"role":"user","content":"keep"}`)}},
	}

	same, err := CheckReplay(snapshot, target, glm)
	if err != nil {
		t.Fatal(err)
	}
	if same.Mode != "native" || same.EpochID != snapshot.Epoch.ID {
		t.Fatalf("compatible target must replay: %+v", same)
	}

	cases := []struct {
		label   string
		target  TargetIdentity
		profile ModelProfile
	}{
		{"endpoint", r01Target(t, glm, "https://coding.example.com/v1", "glm-5.3", "cred-primary"), glm},
		{"family", r01Target(t, deepseek, "https://api.deepseek.com/v1", "deepseek-v4-flash", "cred-ds"), deepseek},
		{"credential", r01Target(t, glm, "https://api.example.com/v1", "glm-5.3", "cred-standby"), glm},
		{"profile", r01Target(t, glmCoding, "https://api.example.com/v1", "glm-5.3", "cred-primary"), glmCoding},
	}
	for _, tc := range cases {
		cont, contErr := ContinueReplay(snapshot, tc.target, tc.profile, taskID, budget)
		if contErr != nil {
			t.Fatal(contErr)
		}
		assertNewEpochKeepsTask(t, tc.label, cont, snapshot, taskID, budget)
	}
}

func TestNativeHistoryExactRoundTrip(t *testing.T) {
	glm := mustLoadProfile(t, "glm", "standard")
	deepseek := mustLoadProfile(t, "deepseek", "standard")
	glmCoding := mustLoadProfile(t, "glm", "coding")

	taskID := "task-root-r01"
	budget := json.RawMessage(`{"maxTotalTokens":262144,"consumed":1200}`)

	target := r01Target(t, glm, "https://api.example.com/v1", "glm-5.3", "cred-primary")
	messages := r01ExactMessages(t)
	epoch := NativeEpoch{
		ID:      "epoch-r01",
		Scope:   NativeScope{OwnerScope: "acct-root", SessionID: "session-r01"},
		Target:  target,
		Profile: glm,
		State:   "active",
	}
	snapshot := NativeSnapshot{Epoch: epoch, Messages: messages}

	decision, err := CheckReplay(snapshot, target, glm)
	if err != nil {
		t.Fatal(err)
	}
	if decision.Mode != "native" || decision.EpochID != epoch.ID || decision.ReasonCode != "" {
		t.Fatalf("compatible target must native-replay: %+v", decision)
	}
	if len(decision.Messages) != len(messages) {
		t.Fatalf("replay dropped messages: got %d want %d", len(decision.Messages), len(messages))
	}
	for i := range messages {
		assertExactNativeMessage(t, i, decision.Messages[i], messages[i])
	}
	assertReasoningPresence(t, decision.Messages)
	assertExactToolArguments(t, decision.Messages)

	sameNameOtherEndpoint := r01Target(t, glm, "https://coding.example.com/v1", "glm-5.3", "cred-primary")
	cont, err := ContinueReplay(snapshot, sameNameOtherEndpoint, glm, taskID, budget)
	if err != nil {
		t.Fatal(err)
	}
	assertNewEpochKeepsTask(t, "endpoint", cont, snapshot, taskID, budget)

	sameNameOtherCred := r01Target(t, glm, "https://api.example.com/v1", "glm-5.3", "cred-standby")
	cont, err = ContinueReplay(snapshot, sameNameOtherCred, glm, taskID, budget)
	if err != nil {
		t.Fatal(err)
	}
	assertNewEpochKeepsTask(t, "credential", cont, snapshot, taskID, budget)

	cont, err = ContinueReplay(snapshot, r01Target(t, glmCoding, "https://api.example.com/v1", "glm-5.3", "cred-primary"), glmCoding, taskID, budget)
	if err != nil {
		t.Fatal(err)
	}
	assertNewEpochKeepsTask(t, "profile", cont, snapshot, taskID, budget)

	dsTarget := r01Target(t, deepseek, "https://api.deepseek.com/v1", "deepseek-v4-flash", "cred-ds")
	cont, err = ContinueReplay(snapshot, dsTarget, deepseek, taskID, budget)
	if err != nil {
		t.Fatal(err)
	}
	assertNewEpochKeepsTask(t, "family", cont, snapshot, taskID, budget)
}

func r01Target(t *testing.T, profile ModelProfile, endpoint, model, cred string) TargetIdentity {
	t.Helper()
	digest, err := ProfileDigest(profile)
	if err != nil {
		t.Fatal(err)
	}
	return TargetIdentity{
		ProviderID:          "provider-r01",
		Protocol:            profile.Protocol,
		EndpointURL:         endpoint,
		EndpointPurpose:     profile.EndpointPurpose,
		ModelRequested:      model,
		Family:              profile.Family,
		ModelContract:       profile.ModelContract,
		CredentialBindingID: cred,
		ProfileDigest:       digest,
		CodecVersion:        profile.CodecVersion,
	}
}

func r01ExactMessages(t *testing.T) []NativeMessage {
	t.Helper()
	unicodeReasoning := "e\u0301"
	if utf8.RuneCountInString(unicodeReasoning) != 2 {
		t.Fatal("fixture unicode must stay decomposed e + combining acute")
	}
	large := strings.Repeat("A", 300001)
	largeWire, err := json.Marshal(map[string]any{
		"role":              "assistant",
		"content":           "final answer",
		"reasoning_content": large,
	})
	if err != nil {
		t.Fatal(err)
	}

	wires := []struct {
		role, callID string
		wire         []byte
	}{
		{"user", "", []byte(`{"role":"user","content":"  keep leading spaces  "}`)},
		{"assistant", "", []byte(`{"role":"assistant","content":"ordinary reply"}`)},
		{"assistant", "c1", []byte(`{"role":"assistant","content":"","reasoning_content":"  前导与结尾  ","tool_calls":[{"id":"c1","type":"function","function":{"name":"workspace.read","arguments":"{\"path\":\"  a.md  \",\"note\":\"keep\\r\\n\"}"}}]}`)},
		{"tool", "c1", []byte(`{"role":"tool","tool_call_id":"c1","content":"\r\nresult body\r\n"}`)},
		{"assistant", "", []byte(`{"role":"assistant","content":"empty reasoning","reasoning_content":""}`)},
		{"assistant", "", []byte(`{"role":"assistant","content":"null reasoning","reasoning_content":null}`)},
		{"assistant", "", []byte(`{"role":"assistant","content":"crlf","reasoning_content":"\r\n第二轮\r\n"}`)},
		{"assistant", "", []byte(`{"role":"assistant","content":"unicode","reasoning_content":"` + unicodeReasoning + `"}`)},
		{"assistant", "", largeWire},
	}

	out := make([]NativeMessage, len(wires))
	for i, w := range wires {
		out[i] = NativeMessage{
			ID:         "msg-" + strconv.Itoa(i+1),
			TurnID:     "turn-r01",
			CallID:     w.callID,
			Role:       w.role,
			Provenance: "adapter_v2",
			Sequence:   int64(i + 1),
			Complete:   true,
			WireJSON:   append(json.RawMessage(nil), w.wire...),
		}
	}
	return out
}

func assertExactNativeMessage(t *testing.T, i int, got, want NativeMessage) {
	t.Helper()
	if got.ID != want.ID || got.TurnID != want.TurnID || got.CallID != want.CallID ||
		got.Role != want.Role || got.Provenance != want.Provenance ||
		got.Sequence != want.Sequence || got.Complete != want.Complete {
		t.Fatalf("message %d index fields changed:\n got %#v\nwant %#v", i, got, want)
	}
	if !bytes.Equal(got.WireJSON, want.WireJSON) {
		t.Fatalf("message %d WireJSON must be exact bytes (no TrimSpace):\n got %q\nwant %q", i, got.WireJSON, want.WireJSON)
	}
}

func assertReasoningPresence(t *testing.T, messages []NativeMessage) {
	t.Helper()
	want := []struct {
		present bool
		raw     string
	}{
		{false, ""},
		{false, ""},
		{true, `"  前导与结尾  "`},
		{false, ""},
		{true, `""`},
		{true, `null`},
		{true, `"\r\n第二轮\r\n"`},
		{true, `"e` + "\u0301" + `"`},
	}
	for i, spec := range want {
		present, raw := reasoningField(t, messages[i].WireJSON)
		if present != spec.present || (spec.present && raw != spec.raw) {
			t.Fatalf("message %d reasoning presence: present=%v raw=%q want present=%v raw=%q", i, present, raw, spec.present, spec.raw)
		}
	}
	if got := decodedReasoningString(t, messages[2].WireJSON); got != "  前导与结尾  " {
		t.Fatalf("leading spaces must survive exact replay, got %q", got)
	}
	if got := decodedReasoningString(t, messages[6].WireJSON); got != "\r\n第二轮\r\n" {
		t.Fatalf("CRLF reasoning must survive exact replay, got %q", got)
	}
	if got := decodedReasoningString(t, messages[7].WireJSON); got != "e\u0301" {
		t.Fatalf("decomposed unicode must not NFC-normalize, got %q", got)
	}
	present, raw := reasoningField(t, messages[len(messages)-1].WireJSON)
	if !present || len(raw) < 300000 {
		t.Fatalf("large reasoning truncated or missing: present=%v len=%d", present, len(raw))
	}
	if !strings.Contains(raw, strings.Repeat("A", 300001)) {
		t.Fatal("large reasoning bytes were normalized or trimmed")
	}
}

func assertExactToolArguments(t *testing.T, messages []NativeMessage) {
	t.Helper()
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(messages[2].WireJSON, &obj); err != nil {
		t.Fatal(err)
	}
	var calls []map[string]json.RawMessage
	if err := json.Unmarshal(obj["tool_calls"], &calls); err != nil || len(calls) != 1 {
		t.Fatalf("tool assistant calls: %v %d", err, len(calls))
	}
	var fn map[string]json.RawMessage
	if err := json.Unmarshal(calls[0]["function"], &fn); err != nil {
		t.Fatal(err)
	}
	want := `"{\"path\":\"  a.md  \",\"note\":\"keep\\r\\n\"}"`
	if string(fn["arguments"]) != want {
		t.Fatalf("tool arguments mutated:\n got %s\nwant %s", fn["arguments"], want)
	}
}

func reasoningField(t *testing.T, wire json.RawMessage) (bool, string) {
	t.Helper()
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(wire, &obj); err != nil {
		t.Fatal(err)
	}
	raw, ok := obj["reasoning_content"]
	if !ok {
		return false, ""
	}
	return true, string(raw)
}

func decodedReasoningString(t *testing.T, wire json.RawMessage) string {
	t.Helper()
	present, raw := reasoningField(t, wire)
	if !present || raw == "null" {
		t.Fatalf("expected string reasoning, present=%v raw=%s", present, raw)
	}
	var s string
	if err := json.Unmarshal([]byte(raw), &s); err != nil {
		t.Fatal(err)
	}
	return s
}

func assertNewEpochKeepsTask(t *testing.T, label string, got ReplayContinuation, snapshot NativeSnapshot, taskID string, budget json.RawMessage) {
	t.Helper()
	if got.Decision.Mode != "structured_rebuild" || got.Decision.ReasonCode != "NATIVE_TARGET_MISMATCH" {
		t.Fatalf("%s: want structured_rebuild/NATIVE_TARGET_MISMATCH, got %+v", label, got.Decision)
	}
	if got.Epoch.ID == "" || got.Epoch.ID == snapshot.Epoch.ID {
		t.Fatalf("%s: incompatible target must open a new epoch, got %q", label, got.Epoch.ID)
	}
	if got.Decision.EpochID != got.Epoch.ID {
		t.Fatalf("%s: decision epoch %q != new epoch %q", label, got.Decision.EpochID, got.Epoch.ID)
	}
	if got.Epoch.Scope != snapshot.Epoch.Scope {
		t.Fatalf("%s: rebuild must keep scope: %+v", label, got.Epoch.Scope)
	}
	if got.TaskID != taskID || !bytes.Equal(got.Budget, budget) {
		t.Fatalf("%s: TaskID/budget reset: task=%q budget=%s", label, got.TaskID, got.Budget)
	}
	if len(got.Decision.Messages) != 0 {
		t.Fatalf("%s: must not claim native bytes after incompatible target", label)
	}
}
