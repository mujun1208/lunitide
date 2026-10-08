package remotegateway

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/lunitide/lunitide/internal/bridge"
	"github.com/lunitide/lunitide/internal/ipc"
	"github.com/oklog/ulid/v2"
)

const (
	wsReadLimit        = ipc.MaxFrameSize
	wsWriteTimeout     = 35 * time.Second
	wsIdleTimeout      = 120 * time.Second
	wsPingInterval     = 45 * time.Second
	wsDrainTimeout     = 5 * time.Second
	preResponseLimit   = 64
)

var wsUpgrader = websocket.Upgrader{
	// 指纹锁定发生在配对阶段（二维码带 fp，配对页校验 TLS 指纹）；这里的
	// Origin 校验对 PWA 无意义（手机浏览器 Origin 是 file/https 杂音），
	// 鉴权完全依赖 Bearer 令牌。
	CheckOrigin: func(*http.Request) bool { return true },
}

// serveBridgeWS 处理 /bridge 升级：Bearer 令牌鉴权 → WS 读循环 → 复用
// ipc.ServeSession 的派发语义（slot 闸门、流式事件缓冲、panic 兜底）。
// 与命名管道会话的唯一差别是帧边界：WS 消息本身就是一帧 JSON。
func (s *Service) serveBridgeWS(w http.ResponseWriter, r *http.Request) {
	device, ok := s.authenticateDevice(r)
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	conn, err := wsUpgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	s.serveBridgeSession(r.Context(), conn, device)
}

// authenticateDevice 校验 Authorization: Bearer <token>，命中未吊销、未
// 过期的已配对设备。失败即记审计（来源 IP 进 detail）。
func (s *Service) authenticateDevice(r *http.Request) (*Device, bool) {
	token := bearerToken(r)
	if token == "" {
		return nil, false
	}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	device, err := s.store.DeviceByTokenHash(ctx, hashToken(token))
	if err != nil || device == nil {
		s.auditQuiet("anonymous", "auth-fail", "bad-token", remoteIP(r))
		return nil, false
	}
	if time.Now().After(device.ExpiresAt) {
		s.auditQuiet(device.DeviceID, "auth-fail", "token-expired", remoteIP(r))
		return nil, false
	}
	return device, true
}

func bearerToken(r *http.Request) string {
	value := r.Header.Get("Authorization")
	const prefix = "Bearer "
	if len(value) > len(prefix) && equalFoldASCII(value[:len(prefix)], prefix) {
		return value[len(prefix):]
	}
	// 浏览器 WebSocket API 无法自定义 Authorization 头；PWA 场景回退到
	// ?token= 查询参数（令牌仍走哈希落库校验，泄露面与 header 等价）。
	return r.URL.Query().Get("token")
}

func equalFoldASCII(a, b string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := 0; i < len(a); i++ {
		ca, cb := a[i], b[i]
		if 'A' <= ca && ca <= 'Z' {
			ca += 'a' - 'A'
		}
		if 'A' <= cb && cb <= 'Z' {
			cb += 'a' - 'A'
		}
		if ca != cb {
			return false
		}
	}
	return true
}

func remoteIP(r *http.Request) string {
	host := r.RemoteAddr
	if idx := lastColon(host); idx >= 0 {
		host = host[:idx]
	}
	return host
}

func lastColon(value string) int {
	for i := len(value) - 1; i >= 0; i-- {
		if value[i] == ':' {
			return i
		}
	}
	return -1
}

// serveBridgeSession 是远程版 ipc.serveSession：读循环语义保持一致，
// 差异仅三点——鉴权已前置（令牌）、帧边界是 WS 消息、写路径带互斥与超时。
func (s *Service) serveBridgeSession(ctx context.Context, conn *websocket.Conn, device *Device) {
	defer conn.Close()
	sessionCtx, cancelSession := context.WithCancel(ctx)
	defer cancelSession()
	done := make(chan struct{})
	defer close(done)
	go func() {
		select {
		case <-ctx.Done():
			_ = conn.Close()
		case <-done:
		}
	}()
	ip := remoteIPFromConn(conn)
	_ = s.store.DeviceTouch(sessionCtx, device.DeviceID, ip, time.Now())
	// 在线注册表（M4）：remote.sessions.list 展示 + Revoke 立即断开。
	unregister := s.registerActiveSession(device, ip, func() { _ = conn.Close() })
	defer unregister()

	var writeMu sync.Mutex
	write := func(value any) error {
		buf := bytes.NewBuffer(make([]byte, 0, 1024))
		enc := json.NewEncoder(buf)
		enc.SetEscapeHTML(false)
		if err := enc.Encode(value); err != nil {
			return err
		}
		raw := buf.Bytes()
		if len(raw) > 0 && raw[len(raw)-1] == '\n' {
			raw = raw[:len(raw)-1]
		}
		if len(raw) == 0 || len(raw) > wsReadLimit {
			return errors.New("invalid frame size")
		}
		writeMu.Lock()
		defer writeMu.Unlock()
		if err := conn.SetWriteDeadline(time.Now().Add(wsWriteTimeout)); err != nil {
			return err
		}
		if err := conn.WriteMessage(websocket.TextMessage, raw); err != nil {
			return err
		}
		return nil
	}
	// 保活：读超时靠 pong 续期，写侧定期 ping。
	conn.SetReadLimit(wsReadLimit)
	_ = conn.SetReadDeadline(time.Now().Add(wsIdleTimeout))
	conn.SetPongHandler(func(string) error {
		return conn.SetReadDeadline(time.Now().Add(wsIdleTimeout))
	})
	stopPinger := make(chan struct{})
	go func() {
		ticker := time.NewTicker(wsPingInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				writeMu.Lock()
				_ = conn.SetWriteDeadline(time.Now().Add(wsWriteTimeout))
				_ = conn.WriteMessage(websocket.PingMessage, nil)
				writeMu.Unlock()
			case <-stopPinger:
				return
			}
		}
	}()
	defer close(stopPinger)

	var requests sync.WaitGroup
	slots := bridge.NewSlotGate(bridge.DefaultGeneralSlots, bridge.DefaultControlSlots, bridge.DefaultSlotWait)
	defer func() {
		cancelSession()
		drained := make(chan struct{})
		go func() {
			requests.Wait()
			close(drained)
		}()
		select {
		case <-drained:
		case <-time.After(wsDrainTimeout):
		}
	}()
	for {
		messageType, frame, err := conn.ReadMessage()
		if err != nil {
			return
		}
		if messageType != websocket.TextMessage {
			return
		}
		var request bridge.Request
		if err := decodeStrictBridge(frame, &request); err != nil {
			return
		}
		if !deviceAllows(device.Scopes, request.Method) {
			if err := write(bridge.Failure(request.ID, request.TraceID, "REMOTE_SCOPE_DENIED", "该设备未被授权此操作", false)); err != nil {
				return
			}
			s.auditQuiet(device.DeviceID, "scope-denied", request.Method, "")
			continue
		}
		if !slots.Acquire(sessionCtx, request.Method) {
			if err := write(bridge.Failure(request.ID, request.TraceID, "ENGINE_BUSY", "核心引擎正忙，请稍后重试", true)); err != nil {
				return
			}
			continue
		}
		requests.Add(1)
		go func(request bridge.Request) {
			defer requests.Done()
			defer slots.Release(request.Method)
			var eventMu sync.Mutex
			responseWritten := false
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
				return write(event)
			}
			var response bridge.Response
			if streaming, ok := s.handler.(ipc.StreamingHandler); ok {
				func() {
					defer func() {
						if r := recover(); r != nil {
							log.Printf("ENGINE PANIC recovered in remote streaming handler: %v (request %s)", r, request.ID)
							response = bridge.Failure(request.ID, request.TraceID, "ENGINE_INTERNAL_ERROR", "引擎内部错误，已自动恢复", false)
						}
					}()
					response = streaming.HandleStreaming(sessionCtx, request, emit)
				}()
			} else {
				func() {
					defer func() {
						if r := recover(); r != nil {
							log.Printf("ENGINE PANIC recovered in remote handler: %v (request %s)", r, request.ID)
							response = bridge.Failure(request.ID, request.TraceID, "ENGINE_INTERNAL_ERROR", "引擎内部错误，已自动恢复", false)
						}
					}()
					response = s.handler.Handle(sessionCtx, request)
				}()
			}
			eventMu.Lock()
			if preResponseError != nil {
				eventMu.Unlock()
				_ = conn.Close()
				return
			}
			if err := write(response); err != nil {
				eventMu.Unlock()
				_ = conn.Close()
				return
			}
			responseWritten = true
			for _, event := range preResponseEvents {
				if err := write(event); err != nil {
					eventMu.Unlock()
					_ = conn.Close()
					return
				}
			}
			preResponseEvents = nil
			eventMu.Unlock()
			s.auditMethod(device.DeviceID, request.Method)
		}(request)
	}
}

func remoteIPFromConn(conn *websocket.Conn) string {
	if conn == nil || conn.RemoteAddr() == nil {
		return ""
	}
	host := conn.RemoteAddr().String()
	if idx := lastColon(host); idx >= 0 {
		return host[:idx]
	}
	return host
}

// decodeStrictBridge 与 ipc.decodeStrict 同语义：未知字段与多 JSON 值
// 都拒绝。复制而非导出，避免为网关放开 ipc 的内部约定。
func decodeStrictBridge(raw []byte, target any) error {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return errors.New("multiple JSON values are not allowed")
	}
	return nil
}

func validateStreamEnvelope(event bridge.Event) error {
	if event.Version != bridge.Version || event.Kind != "event" || event.Sequence < 1 {
		return errors.New("stream event envelope is incomplete")
	}
	if _, err := ulid.ParseStrict(event.ID); err != nil {
		return errors.New("stream event ID is invalid")
	}
	if _, err := ulid.ParseStrict(event.StreamID); err != nil {
		return errors.New("stream event stream ID is invalid")
	}
	return nil
}

