package agenthub

import (
	"encoding/json"
	"testing"
)

func TestACPOpenNativeSessionKeepsIDWhenLoadOmitsSessionId(t *testing.T) {
	id, err := acpOpenNativeSession(func(method string, _ any) (*acpRPC, error) {
		if method != "session/load" {
			t.Fatalf("method = %s, want session/load", method)
		}
		return &acpRPC{Result: json.RawMessage(`{}`)}, nil
	}, `C:\proj`, "sess_old")
	if err != nil || id != "sess_old" {
		t.Fatalf("id = %q err=%v, want sess_old", id, err)
	}
}

func TestKeepNativeIDPrefersReturned(t *testing.T) {
	if got := keepNativeID("sess_new", "sess_old"); got != "sess_new" {
		t.Fatalf("got %q", got)
	}
	if got := keepNativeID("", "sess_old"); got != "sess_old" {
		t.Fatalf("empty returned should keep stored: %q", got)
	}
}

func TestAssistantCaptureDropsReplayAndKeepsTheNewSentence(t *testing.T) {
	store := NewThreadStore(openThreadDB(t))
	thread := sampleThread("01ARZ3NDEKTSV4RRFFQ69G5FAE", "kimi", "天气", false)
	if err := store.Insert(thread); err != nil {
		t.Fatal(err)
	}
	weather := "我查了一下上海今天的天气，多云，22℃，东北风小于 3 级，过去 24 小时无降水。"
	if err := insertThreadMessage(store, thread.ID, "user", "今天上海的天气？"); err != nil {
		t.Fatal(err)
	}
	if err := insertThreadMessage(store, thread.ID, "assistant", weather); err != nil {
		t.Fatal(err)
	}
	if err := insertThreadMessage(store, thread.ID, "user", "收到回复OK即可"); err != nil {
		t.Fatal(err)
	}
	sess := &kimiACPSession{threadID: thread.ID}
	sess.onUpdate(agentChunk(weather))
	sess.mu.Lock()
	if sess.assist.Len() != 0 {
		t.Fatal("session/load replay must not become the next reply")
	}
	beginAssistantCapture(&sess.assist, &sess.capture)
	sess.mu.Unlock()
	sess.onUpdate(agentChunk(weather))
	sess.onUpdate(agentChunk(weather + " OK"))
	sess.mu.Lock()
	raw := takeAssistantCapture(&sess.assist, &sess.capture)
	sess.mu.Unlock()
	if raw != weather+" OK" {
		t.Fatalf("snapshot chunk = %q", raw)
	}
	if got := stripCarriedTurn(store, thread.ID, raw); got != "OK" {
		t.Fatalf("visible reply = %q, want OK", got)
	}
	fresh := "上海明天小雨，18℃。"
	if got := stripCarriedTurn(store, thread.ID, fresh); got != fresh {
		t.Fatalf("fresh reply = %q", got)
	}
	if got := stripCarriedPrefix("", "OK", "OK，明天也看看"); got != "OK，明天也看看" {
		t.Fatalf("short reply must stay: %q", got)
	}
}

func agentChunk(text string) json.RawMessage {
	raw, err := json.Marshal(map[string]any{
		"update": map[string]any{
			"sessionUpdate": "agent_message_chunk",
			"content":       map[string]string{"text": text},
		},
	})
	if err != nil {
		panic(err)
	}
	return raw
}

func TestAbsorbAssistantChunk(t *testing.T) {
	if got := absorbAssistantChunk("上海", "今天"); got != "上海今天" {
		t.Fatalf("delta = %q", got)
	}
	if got := absorbAssistantChunk("上海今天", "上海今天多云"); got != "上海今天多云" {
		t.Fatalf("snapshot = %q", got)
	}
	if got := absorbAssistantChunk("上海今天多云", "上海"); got != "上海今天多云" {
		t.Fatalf("stale snapshot = %q", got)
	}
}

func TestACPOpenNativeSessionErrorsWhenNewOmitsSessionId(t *testing.T) {
	_, err := acpOpenNativeSession(func(method string, _ any) (*acpRPC, error) {
		if method != "session/new" {
			t.Fatalf("method = %s, want session/new", method)
		}
		return &acpRPC{Result: json.RawMessage(`{}`)}, nil
	}, `C:\proj`, "")
	if err == nil {
		t.Fatal("empty session/new must fail")
	}
}
