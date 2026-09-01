package app

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/lunitide/lunitide/internal/bridge"
	"github.com/lunitide/lunitide/internal/domain/provider"
	"github.com/lunitide/lunitide/internal/talk"
)

type talkProviderStub struct {
	providerRepositoryStub
	item provider.Provider
}

func (s talkProviderStub) Get(_ context.Context, id string) (provider.Provider, error) {
	if s.item.ID == id {
		return s.item, nil
	}
	return provider.Provider{}, provider.ErrNotFound
}

func talkFixtureProvider() provider.Provider {
	return provider.Provider{
		ID:       "01ARZ3NDEKTSV4RRFFQ69G5FAV",
		Name:     "Chat",
		Protocol: provider.ProtocolOpenAICompatible,
		BaseURL:  "https://example.com",
		Status:   provider.StatusEnabled,
		Models: []provider.Model{
			{ModelID: "glm-4-air", DisplayName: "Air"},
			{ModelID: "gpt-4o-realtime-preview", DisplayName: "Realtime"},
		},
	}
}

func TestIsTalkRealtimeModelID(t *testing.T) {
	if !isTalkRealtimeModelID("gpt-4o-realtime-preview", "") {
		t.Fatal("realtime-preview")
	}
	if !isTalkRealtimeModelID("doubao-realtime", "豆包实时") {
		t.Fatal("realtime")
	}
	if !isTalkRealtimeModelID("gemini-live", "Gemini Live") {
		t.Fatal("live segment")
	}
	if isTalkRealtimeModelID("glm-4-air", "Air") {
		t.Fatal("air is not realtime")
	}
	if isTalkRealtimeModelID("olive-chat", "Olive") {
		t.Fatal("olive must not match live")
	}
}

func TestResolveTalkModelRequiresListedRealtime(t *testing.T) {
	item := talkFixtureProvider()
	if _, ok := resolveTalkModel(item, "glm-4-air"); ok {
		t.Fatal("air must be unsupported")
	}
	if _, ok := resolveTalkModel(item, "gpt-4o-realtime-preview"); !ok {
		t.Fatal("listed realtime must resolve")
	}
	if _, ok := resolveTalkModel(item, "gpt-4o-realtime-preview-invented"); ok {
		t.Fatal("must not invent an id")
	}
	item.Protocol = provider.ProtocolAnthropic
	if _, ok := resolveTalkModel(item, "gpt-4o-realtime-preview"); ok {
		t.Fatal("anthropic is not a talk protocol")
	}
}

func TestTalkStartRejectsAirModel(t *testing.T) {
	item := talkFixtureProvider()
	e := NewEngine(talkProviderStub{item: item}, "test")
	resp := e.Handle(context.Background(), validRequest("talk.start", `{
		"providerId":"01ARZ3NDEKTSV4RRFFQ69G5FAV",
		"modelId":"glm-4-air",
		"sessionId":"01ARZ3NDEKTSV4RRFFQ69G5FAW"
	}`))
	if resp.OK || resp.Error == nil || resp.Error.Code != talkModelUnsupportedCode {
		t.Fatalf("talk.start air = %+v, want TALK_MODEL_UNSUPPORTED", resp)
	}
}

func TestTalkStartMatchingRealtimeStaysUnready(t *testing.T) {
	item := talkFixtureProvider()
	e := NewEngine(talkProviderStub{item: item}, "test")
	resp := e.Handle(context.Background(), validRequest("talk.start", `{
		"providerId":"01ARZ3NDEKTSV4RRFFQ69G5FAV",
		"modelId":"gpt-4o-realtime-preview",
		"sessionId":"01ARZ3NDEKTSV4RRFFQ69G5FAW"
	}`))
	if resp.OK || resp.Error == nil || resp.Error.Code != talkAdapterUnreadyCode {
		t.Fatalf("talk.start realtime = %+v, want TALK_ADAPTER_UNREADY", resp)
	}
}

func TestTalkStartMissingProvider(t *testing.T) {
	e := NewEngine(providerRepositoryStub{}, "test")
	resp := e.Handle(context.Background(), validRequest("talk.start", `{
		"providerId":"01ARZ3NDEKTSV4RRFFQ69G5FAV",
		"modelId":"gpt-4o-realtime-preview",
		"sessionId":"01ARZ3NDEKTSV4RRFFQ69G5FAW"
	}`))
	if resp.OK || resp.Error == nil || resp.Error.Code != talkModelUnsupportedCode {
		t.Fatalf("talk.start missing = %+v, want TALK_MODEL_UNSUPPORTED", resp)
	}
}

func TestTalkAppendCancelNeedSession(t *testing.T) {
	e := NewEngine(providerRepositoryStub{}, "test")
	appendResp := e.Handle(context.Background(), validRequest("talk.append", `{
		"sessionId":"01ARZ3NDEKTSV4RRFFQ69G5FAV","pcm":"AAAA"
	}`))
	if appendResp.OK || appendResp.Error == nil || appendResp.Error.Code != talkSessionMissingCode {
		t.Fatalf("talk.append = %+v", appendResp)
	}
	cancelResp := e.Handle(context.Background(), validRequest("talk.cancel", `{
		"sessionId":"01ARZ3NDEKTSV4RRFFQ69G5FAV","mode":"output"
	}`))
	if cancelResp.OK || cancelResp.Error == nil || cancelResp.Error.Code != talkSessionMissingCode {
		t.Fatalf("talk.cancel = %+v", cancelResp)
	}
}

func talkWSServer(t *testing.T, frames []map[string]any, inbound *[]map[string]any) *httptest.Server {
	t.Helper()
	upgrader := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
	var mu sync.Mutex
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		for _, frame := range frames {
			if conn.WriteJSON(frame) != nil {
				return
			}
		}
		for {
			_, raw, err := conn.ReadMessage()
			if err != nil {
				return
			}
			var msg map[string]any
			if json.Unmarshal(raw, &msg) != nil {
				continue
			}
			mu.Lock()
			*inbound = append(*inbound, msg)
			mu.Unlock()
		}
	}))
}

func talkDialerFor(serverURL string) talk.Dialer {
	return func(ctx context.Context, _ string, header http.Header) (talk.Conn, error) {
		return talk.DefaultDialer(ctx, "ws"+strings.TrimPrefix(serverURL, "http"), header)
	}
}

func TestTalkStartStreamingEmitsAudioAndHandoff(t *testing.T) {
	item := talkFixtureProvider()
	item.CredentialRef = "cred"
	item.CredentialState = provider.CredentialConfigured
	var inbound []map[string]any
	srv := talkWSServer(t, []map[string]any{
		{"type": "session.created"},
		{"type": "response.audio.delta", "delta": "AAAA"},
		{"type": "conversation.item.input_audio_transcription.completed", "transcript": "帮我打开网易云"},
	}, &inbound)
	defer srv.Close()

	e := NewEngineWithGateway(talkProviderStub{item: item}, "test", streamTestLease{})
	e.SetTalkDialerForTest(talkDialerFor(srv.URL))

	var mu sync.Mutex
	var events []bridge.Event
	resp := e.HandleStreaming(context.Background(), validRequest("talk.start", `{
		"providerId":"01ARZ3NDEKTSV4RRFFQ69G5FAV",
		"modelId":"gpt-4o-realtime-preview",
		"sessionId":"01ARZ3NDEKTSV4RRFFQ69G5FAW"
	}`), func(event bridge.Event) error {
		mu.Lock()
		events = append(events, event)
		mu.Unlock()
		return nil
	})
	if !resp.OK {
		t.Fatalf("talk.start = %+v", resp)
	}
	payload, _ := resp.Payload.(map[string]any)
	if _, ok := payload["streamId"].(string); !ok {
		t.Fatalf("missing streamId %+v", resp.Payload)
	}
	if _, ok := payload["talkId"].(string); !ok {
		t.Fatalf("missing talkId %+v", resp.Payload)
	}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		mu.Lock()
		var sawAudio, sawTool bool
		for _, ev := range events {
			if ev.Type == bridge.EventTalkAudio && ev.Talk != nil && ev.Talk.AudioBase64 == "AAAA" {
				sawAudio = true
			}
			if ev.Type == bridge.EventTalkTool && ev.Talk != nil && ev.Talk.Name == "handoff" && ev.Talk.Text == "帮我打开网易云" {
				sawTool = true
			}
		}
		mu.Unlock()
		if sawAudio && sawTool {
			appendResp := e.Handle(context.Background(), validRequest("talk.append", `{
				"sessionId":"01ARZ3NDEKTSV4RRFFQ69G5FAW","pcm":"BBBB"
			}`))
			if !appendResp.OK {
				t.Fatalf("talk.append live = %+v", appendResp)
			}
			cancelResp := e.Handle(context.Background(), validRequest("talk.cancel", `{
				"sessionId":"01ARZ3NDEKTSV4RRFFQ69G5FAW","mode":"output"
			}`))
			if !cancelResp.OK {
				t.Fatalf("talk.cancel output = %+v", cancelResp)
			}
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	mu.Lock()
	defer mu.Unlock()
	t.Fatalf("events = %+v inbound=%+v", events, inbound)
}

func TestTalkCancelAllDropsSession(t *testing.T) {
	item := talkFixtureProvider()
	item.CredentialRef = "cred"
	item.CredentialState = provider.CredentialConfigured
	var inbound []map[string]any
	srv := talkWSServer(t, []map[string]any{{"type": "session.created"}}, &inbound)
	defer srv.Close()
	e := NewEngineWithGateway(talkProviderStub{item: item}, "test", streamTestLease{})
	e.SetTalkDialerForTest(talkDialerFor(srv.URL))
	resp := e.HandleStreaming(context.Background(), validRequest("talk.start", `{
		"providerId":"01ARZ3NDEKTSV4RRFFQ69G5FAV",
		"modelId":"gpt-4o-realtime-preview",
		"sessionId":"01ARZ3NDEKTSV4RRFFQ69G5FAW"
	}`), func(bridge.Event) error { return nil })
	if !resp.OK {
		t.Fatalf("start = %+v", resp)
	}
	all := e.Handle(context.Background(), validRequest("talk.cancel", `{
		"sessionId":"01ARZ3NDEKTSV4RRFFQ69G5FAW","mode":"all"
	}`))
	if !all.OK {
		t.Fatalf("cancel all = %+v", all)
	}
	again := e.Handle(context.Background(), validRequest("talk.append", `{
		"sessionId":"01ARZ3NDEKTSV4RRFFQ69G5FAW","pcm":"AAAA"
	}`))
	if again.OK || again.Error == nil || again.Error.Code != talkSessionMissingCode {
		t.Fatalf("append after all = %+v", again)
	}
}
