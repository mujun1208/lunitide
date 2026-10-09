package remotegateway

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/lunitide/lunitide/internal/bridge"
	"github.com/oklog/ulid/v2"
)

// fakeStreamingHandler 扮演带流式方法的引擎：chat.start 立即返回
// streamId，事件在响应之后由后台 goroutine 持续推送（复刻真实引擎的
// emitter 生命周期——事件晚于 HandleStreaming 返回，这是 HTTP 流式端
// 「响应后保持连接」语义的立足点）；其余方法按非流式一问一答。
type fakeStreamingHandler struct {
	fakeHandler
	streamID string
	events   chan bridge.Event
}

func (h *fakeStreamingHandler) HandleStreaming(ctx context.Context, request bridge.Request, emit func(bridge.Event) error) bridge.Response {
	if string(request.Method) != "chat.start" {
		return h.Handle(ctx, request)
	}
	go func() {
		for {
			select {
			case event := <-h.events:
				if event.ID == "" {
					event.ID = ulid.Make().String()
				}
				_ = emit(event)
				if isTerminalStreamEvent(event.Type) {
					return
				}
			case <-ctx.Done():
				return
			}
		}
	}()
	return request.Ok(map[string]any{"streamId": h.streamID})
}

func newHTTPSessionService(t *testing.T, handler *fakeStreamingHandler) *Service {
	t.Helper()
	ctx := context.Background()
	svc, err := New(ctx, newTestRoot(t), handler, "0.0.0-test")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { svc.Close() })
	device := Device{
		DeviceID:  "01TESTHTTPDEV",
		Name:      "HTTP 测试机",
		Platform:  "android-pwa",
		TokenHash: hashToken("tok123"),
		Scopes:    DefaultScopes,
		GrantedAt: time.Now(),
		ExpiresAt: time.Now().Add(DeviceTokenTTL),
	}
	if err := svc.store.DeviceSave(ctx, device); err != nil {
		t.Fatal(err)
	}
	return svc
}

func postBridgeHTTP(t *testing.T, svc *Service, token string, request bridge.Request) *httptest.ResponseRecorder {
	t.Helper()
	raw, err := json.Marshal(request)
	if err != nil {
		t.Fatal(err)
	}
	recorder := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/bridge/http", strings.NewReader(string(raw)))
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	svc.serveBridgeHTTP(recorder, req)
	return recorder
}

// ndjsonLines 拆 NDJSON 响应体为逐帧 map（顺序即写出顺序）。
func ndjsonLines(t *testing.T, recorder *httptest.ResponseRecorder) []map[string]any {
	t.Helper()
	var frames []map[string]any
	for _, line := range strings.Split(strings.TrimSpace(recorder.Body.String()), "\n") {
		if line == "" {
			continue
		}
		var frame map[string]any
		if err := json.Unmarshal([]byte(line), &frame); err != nil {
			t.Fatalf("ndjson line is not valid json: %q: %v", line, err)
		}
		frames = append(frames, frame)
	}
	return frames
}

func eventFrame(eventType string, sequence uint64, streamID string) bridge.Event {
	return bridge.Event{
		Version:  bridge.Version,
		Kind:     "event",
		ID:       ulid.Make().String(),
		StreamID: streamID,
		Sequence: sequence,
		Type:     bridge.EventType(eventType),
		Delta:    &bridge.DeltaEvent{Text: "增量" + string(rune('0'+sequence))},
	}
}

// TestServeBridgeHTTPAuthAndMethod 钉死鉴权与动词契约：无令牌 401、
// 非 POST 405、坏帧 400。
func TestServeBridgeHTTPAuthAndMethod(t *testing.T) {
	svc := newHTTPSessionService(t, &fakeStreamingHandler{})
	request := newRequest(t, "project.list", map[string]any{})

	recorder := postBridgeHTTP(t, svc, "", request)
	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("no-token code = %d, want 401", recorder.Code)
	}
	recorder = postBridgeHTTP(t, svc, "bad-token", request)
	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("bad-token code = %d, want 401", recorder.Code)
	}

	req := httptest.NewRequest(http.MethodGet, "/bridge/http", nil)
	recorder = httptest.NewRecorder()
	svc.serveBridgeHTTP(recorder, req)
	if recorder.Code != http.StatusMethodNotAllowed {
		t.Fatalf("GET code = %d, want 405", recorder.Code)
	}
	req = httptest.NewRequest(http.MethodPost, "/bridge/http", strings.NewReader("{not-json"))
	req.Header.Set("Authorization", "Bearer tok123")
	recorder = httptest.NewRecorder()
	svc.serveBridgeHTTP(recorder, req)
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("bad-frame code = %d, want 400", recorder.Code)
	}
}

// TestServeBridgeHTTPSimpleRequest 钉死非流式方法：单响应帧、NDJSON 单行、
// Content-Type 契约，响应后连接即关。
func TestServeBridgeHTTPSimpleRequest(t *testing.T) {
	svc := newHTTPSessionService(t, &fakeStreamingHandler{})
	request := newRequest(t, "project.list", map[string]any{})
	recorder := postBridgeHTTP(t, svc, "tok123", request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("code = %d, want 200, body=%s", recorder.Code, recorder.Body.String())
	}
	if got := recorder.Header().Get("Content-Type"); !strings.Contains(got, "ndjson") {
		t.Fatalf("content-type = %q, want ndjson", got)
	}
	frames := ndjsonLines(t, recorder)
	if len(frames) != 1 {
		t.Fatalf("frames = %d, want 1 (response only): %v", len(frames), frames)
	}
	response := frames[0]
	if response["kind"] != "response" || response["ok"] != true || response["requestId"] != request.ID {
		t.Fatalf("response frame mismatch: %v", response)
	}
}

// TestServeBridgeHTTPStreamingRequest 钉死流式方法的生命周期：响应帧先行，
// 事件随后按序推送，终结事件后连接关闭——顺序由 NDJSON 天然保证。
func TestServeBridgeHTTPStreamingRequest(t *testing.T) {
	streamID := ulid.Make().String()
	handler := &fakeStreamingHandler{streamID: streamID, events: make(chan bridge.Event, 4)}
	svc := newHTTPSessionService(t, handler)
	request := newRequest(t, "chat.start", map[string]any{"sessionId": "01S"})

	handler.events <- eventFrame("delta", 1, streamID)
	handler.events <- eventFrame("delta", 2, streamID)
	// completed 带 body-less envelope：终结帧。
	handler.events <- bridge.Event{Version: bridge.Version, Kind: "event", ID: ulid.Make().String(), StreamID: streamID, Sequence: 3, Type: bridge.EventCompleted}

	recorder := postBridgeHTTP(t, svc, "tok123", request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("code = %d, want 200", recorder.Code)
	}
	frames := ndjsonLines(t, recorder)
	if len(frames) != 4 {
		t.Fatalf("frames = %d, want 4 (response + 3 events): %v", len(frames), frames)
	}
	first := frames[0]
	if first["kind"] != "response" {
		t.Fatalf("first frame kind = %v, want response", first["kind"])
	}
	payload, _ := first["payload"].(map[string]any)
	if payload["streamId"] != streamID {
		t.Fatalf("response streamId = %v, want %s", payload["streamId"], streamID)
	}
	for i, wantType := range []string{"delta", "delta", "completed"} {
		frame := frames[i+1]
		if frame["kind"] != "event" || frame["type"] != wantType || frame["streamId"] != streamID {
			t.Fatalf("event frame %d mismatch: %v (want type=%s)", i, frame, wantType)
		}
		if seq, _ := frame["sequence"].(float64); int(seq) != i+1 {
			t.Fatalf("event %d sequence = %v, want %d", i, frame["sequence"], i+1)
		}
	}
}

// TestServeBridgeHTTPClientAbort 钉死客户端断开语义：流式请求中途断开
//（ctx Done）即杀流——与 WSS 断连一致，等待循环必须退出而非挂死。
func TestServeBridgeHTTPClientAbort(t *testing.T) {
	streamID := ulid.Make().String()
	handler := &fakeStreamingHandler{streamID: streamID, events: make(chan bridge.Event, 1)}
	svc := newHTTPSessionService(t, handler)
	request := newRequest(t, "chat.start", map[string]any{})

	raw, _ := json.Marshal(request)
	ctx, cancel := context.WithCancel(context.Background())
	req := httptest.NewRequest(http.MethodPost, "/bridge/http", strings.NewReader(string(raw))).WithContext(ctx)
	req.Header.Set("Authorization", "Bearer tok123")
	recorder := httptest.NewRecorder()
	done := make(chan struct{})
	go func() { svc.serveBridgeHTTP(recorder, req); close(done) }()
	// 给 handler 一点时间进入流等待，然后模拟客户端断开。
	time.Sleep(50 * time.Millisecond)
	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("serveBridgeHTTP did not return after client disconnect")
	}
}

// TestServeBridgeHTTPScopeDenied 钉死未授权方法的拒绝帧（NDJSON 错误帧，
// 而非 HTTP 层错误——鉴权已过，scope 是应用层语义）。
func TestServeBridgeHTTPScopeDenied(t *testing.T) {
	handler := &fakeStreamingHandler{}
	svc := newHTTPSessionService(t, handler)
	// scopes.go 把 web.* 归类为受限控制组：DefaultScopes 若不含该方法即拒。
	request := newRequest(t, "provider.credential.reveal", map[string]any{"providerId": "01P"})
	recorder := postBridgeHTTP(t, svc, "tok123", request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("code = %d, want 200 (in-band denial)", recorder.Code)
	}
	frames := ndjsonLines(t, recorder)
	if len(frames) != 1 || frames[0]["ok"] != false {
		t.Fatalf("denial frame mismatch: %v", frames)
	}
	errObj, _ := frames[0]["error"].(map[string]any)
	if errObj == nil || errObj["code"] != "REMOTE_SCOPE_DENIED" {
		t.Fatalf("error code = %v, want REMOTE_SCOPE_DENIED", errObj)
	}
}

// TestIsStreamingMethod 钉死流式方法白名单与引擎 emitter 消费点的对应
//（漏配 = 响应后连接被关、事件丢失）。
func TestIsStreamingMethod(t *testing.T) {
	for _, method := range []string{"chat.start", "talk.start", "terminal.start", "voice.start", "tts.stream", "media.session.watch"} {
		if !isStreamingMethod(method) {
			t.Fatalf("isStreamingMethod(%q) = false, want true", method)
		}
	}
	for _, method := range []string{"project.list", "chat.prefer", "stream.cancel", "omni.start", "voice.append"} {
		if isStreamingMethod(method) {
			t.Fatalf("isStreamingMethod(%q) = true, want false", method)
		}
	}
}

// 静态保证 io 引用不被未来裁剪（serveBridgeHTTP 的 body 读取）。
var _ = io.ReadAll
