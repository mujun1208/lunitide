package remotegateway

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/lunitide/lunitide/internal/bridge"
	"github.com/lunitide/lunitide/internal/ipc"
)

// HTTPS NDJSON 流式桥：壳内 WebView 的 WebSocket 升级在部分安卓网络栈下
// 静默失败（0.17.6 实测：同一 WebView 里 HTTPS 配对 POST 成功，/bridge
// WSS 却零连接，系统全部报 BRIDGE_UNAVAILABLE），而 HTTPS fetch 完全可用。
// 此端点把「一次请求一个响应」放宽为「一次请求一条 NDJSON 流」：响应帧
// 先行写出，流事件随后持续追加，流终结后关闭连接——帧序天然有序，客户端
// 逐行解析即可。帧格式与 WSS 完全一致（一条 WS 文本消息 = 一行 NDJSON）。
const (
	// httpStreamIdleTimeout 是流式请求的空闲回收：终结由事件类型驱动
	// （isTerminalStreamEvent），但长闲流（如终端 10 分钟无输出）不能永远
	// 占着连接。与 WSS「连接生命周期即流生命周期」不同，HTTP handler
	// return 即 cancel r.Context()——引擎后台流随之终止，客户端收到 EOF
	// 并由 FetchTransport 合成 failed 事件，有明确可发现的失败出口。
	httpStreamIdleTimeout = 600 * time.Second
)

// isTerminalStreamEvent 判定事件是否为其所在流的最后一帧：chat 流的
// completed/cancelled/failed、终端流的 terminal_exit、语音对话流的
// talk_ended。写完终结帧即可关流。
func isTerminalStreamEvent(t bridge.EventType) bool {
	switch t {
	case bridge.EventCompleted, bridge.EventCancelled, bridge.EventFailed,
		bridge.EventTerminalExit, bridge.EventTalkEnded:
		return true
	}
	return false
}

// isStreamingMethod 判定方法的响应帧之后是否还有持续事件流（引擎把
// emitter 存进请求 ctx、由后台 goroutine 继续推送）。与 app 包
// eventEmitterKey 的消费点一一对应：chat.go(chat.start) /
// talk_handlers.go(talk.start) / terminal_handlers.go(terminal.start) /
// m9_tts_handlers.go(tts.stream) / media_handlers.go(media.session.watch)。
// 新增流式方法时这里必须同步：漏配的后果是响应后连接立即关闭、事件丢失
// （客户端合成 failed，可发现可修）；多配则连接闲置到空闲回收，无害。
func isStreamingMethod(method string) bool {
	switch method {
	case "chat.start", "talk.start", "terminal.start", "voice.start",
		"tts.stream", "media.session.watch":
		return true
	}
	return false
}

// serveBridgeHTTP 处理 POST /bridge/http：Bearer 令牌鉴权 → 单请求帧 →
// NDJSON 流式响应。派发语义与 serveBridgeSession 一致（scope 检查、slot
// 闸门、preResponse 缓冲、panic 兜底、审计），差异仅在传输形状：WSS 的
// 「多路复用长连接」换成「每请求独立流式响应」，流事件与响应同流传输。
func (s *Service) serveBridgeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	device, ok := s.authenticateDevice(r)
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, ipc.MaxFrameSize+1))
	if err != nil || len(body) == 0 || len(body) > ipc.MaxFrameSize {
		http.Error(w, "invalid frame size", http.StatusBadRequest)
		return
	}
	var request bridge.Request
	if err := decodeStrictBridge(body, &request); err != nil {
		http.Error(w, "invalid frame", http.StatusBadRequest)
		return
	}
	_ = s.store.DeviceTouch(r.Context(), device.DeviceID, remoteIP(r), time.Now())

	flusher, _ := w.(http.Flusher)
	header := w.Header()
	header.Set("Content-Type", "application/x-ndjson")
	header.Set("Cache-Control", "no-store")
	header.Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	if flusher != nil {
		flusher.Flush()
	}

	var writeMu sync.Mutex
	writeFrame := func(value any) error {
		buf := bytes.NewBuffer(make([]byte, 0, 1024))
		enc := json.NewEncoder(buf)
		enc.SetEscapeHTML(false)
		if err := enc.Encode(value); err != nil {
			return err
		}
		writeMu.Lock()
		defer writeMu.Unlock()
		if _, err := w.Write(buf.Bytes()); err != nil {
			return err
		}
		if flusher != nil {
			flusher.Flush()
		}
		return nil
	}

	if !deviceAllows(device.Scopes, request.Method) {
		_ = writeFrame(bridge.Failure(request.ID, request.TraceID, "REMOTE_SCOPE_DENIED", "该设备未被授权此操作", false))
		s.auditQuiet(device.DeviceID, "scope-denied", request.Method, "")
		return
	}
	// WSS 每连接一个 slot gate；HTTP 无会话概念，全网关共享一个 gate——
	// 个人网关设备数个位数，等价于「网关总并发」上限，闸门语义不弱化。
	if !s.httpSlotGate().Acquire(r.Context(), request.Method) {
		_ = writeFrame(bridge.Failure(request.ID, request.TraceID, "ENGINE_BUSY", "核心引擎正忙，请稍后重试", true))
		return
	}
	defer s.httpSlotGate().Release(request.Method)

	var eventMu sync.Mutex
	responseWritten := false
	terminal := make(chan struct{}, 1)
	preResponseEvents := make([]bridge.Event, 0, 4)
	var preResponseError error
	emit := func(event bridge.Event) error {
		if err := validateStreamEnvelope(event); err != nil {
			log.Printf("rejected malformed remote stream event method=%s type=%s: %v", request.Method, event.Type, err)
			return err
		}
		eventMu.Lock()
		defer eventMu.Unlock()
		if !responseWritten {
			if preResponseError != nil {
				return preResponseError
			}
			if len(preResponseEvents) == preResponseLimit {
				preResponseError = errors.New("stream emitted too many events before its response")
				return preResponseError
			}
			preResponseEvents = append(preResponseEvents, event)
			return nil
		}
		if err := writeFrame(event); err != nil {
			return err
		}
		if isTerminalStreamEvent(event.Type) {
			select {
			case terminal <- struct{}{}:
			default:
			}
		}
		return nil
	}
	var response bridge.Response
	if streaming, ok := s.handler.(ipc.StreamingHandler); ok {
		func() {
			defer func() {
				if rec := recover(); rec != nil {
					log.Printf("ENGINE PANIC recovered in remote http handler: %v (request %s)", rec, request.ID)
					response = bridge.Failure(request.ID, request.TraceID, "ENGINE_INTERNAL_ERROR", "引擎内部错误，已自动恢复", false)
				}
			}()
			response = streaming.HandleStreaming(r.Context(), request, emit)
		}()
	} else {
		func() {
			defer func() {
				if rec := recover(); rec != nil {
					log.Printf("ENGINE PANIC recovered in remote http handler: %v (request %s)", rec, request.ID)
					response = bridge.Failure(request.ID, request.TraceID, "ENGINE_INTERNAL_ERROR", "引擎内部错误，已自动恢复", false)
				}
			}()
			response = s.handler.Handle(r.Context(), request)
		}()
	}
	eventMu.Lock()
	if preResponseError != nil {
		eventMu.Unlock()
		return
	}
	if err := writeFrame(response); err != nil {
		eventMu.Unlock()
		return
	}
	responseWritten = true
	for _, event := range preResponseEvents {
		if err := writeFrame(event); err != nil {
			eventMu.Unlock()
			return
		}
		if isTerminalStreamEvent(event.Type) {
			select {
			case terminal <- struct{}{}:
			default:
			}
		}
	}
	preResponseEvents = nil
	eventMu.Unlock()
	s.auditMethod(device.DeviceID, request.Method)
	if !isStreamingMethod(string(request.Method)) {
		// 非流式方法：响应与前置事件已全部写出，直接关流。
		return
	}
	// 流式方法：HTTP 请求生命周期即流生命周期（对齐 WSS 的连接语义）。
	// 引擎把 emitter 存进 r.Context() 派生的链上，终结事件、客户端断开
	//（abort/离开页面——ctx Done 即杀流，与 WSS 断连一致）或空闲回收，
	// 三者先到即关。终结帧可能出现在 preResponseEvents 里（响应前就被
	// 取消的流），上面已同步投递 terminal 信号。
	idle := time.NewTimer(httpStreamIdleTimeout)
	defer idle.Stop()
	select {
	case <-terminal:
	case <-r.Context().Done():
	case <-idle.C:
	}
}
