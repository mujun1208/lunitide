package remotegateway

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/lunitide/lunitide/internal/bridge"
	"github.com/oklog/ulid/v2"
)

// TestE2EServeForBrowser 手动端到端壳：起真实网关（真实 web/dist 静态产物
// + fake bridge handler），打印配对 URL 后常驻数分钟，供外部浏览器自动化
// （Playwright 等）访问验证。默认 skip；设 LUNITIDE_E2E=1 启用：
//
//	go test ./internal/remotegateway -run TestE2EServeForBrowser -v -timeout 8m
//
//	浏览器侧验证点：
//	1) /pair 回退到 pair.html（0.16.2 修复）
//	2) 配对页渲染、hash 解析、语言写入
//	3) POST /api/pair 真实配对、凭据落 localStorage
//	4) 成功页安装引导（Android UA → 快捷方式分支文案）
//	5) 进入主界面后 WSS 桥接（system.health 走通）
func TestE2EServeForBrowser(t *testing.T) {
	if os.Getenv("LUNITIDE_E2E") != "1" {
		t.Skip("manual e2e shell; set LUNITIDE_E2E=1")
	}
	dist := os.Getenv("LUNITIDE_E2E_DIST")
	if dist == "" {
		dist = "../../web/dist"
	}
	if _, err := os.Stat(dist + "/pair.html"); err != nil {
		t.Fatalf("dist has no pair.html: %v", err)
	}
	ctx := context.Background()
	svc, err := New(ctx, newTestRoot(t), &fakeHandler{}, "0.0.0-e2e")
	if err != nil {
		t.Fatal(err)
	}
	defer svc.Close()
	svc.rendererDirOverride = dist
	svc.port = 0 // 本机 47651 常被正在运行的桌面实例占用；e2e 用随机端口。
	if err := svc.Enable(ctx); err != nil {
		t.Fatal(err)
	}
	info, err := svc.IssuePairCode(ctx, "zh-CN")
	if err != nil {
		t.Fatal(err)
	}
	// IssuePairCode 的 URL 按固定 DefaultPort 拼接；随机端口下替换为实际端口。
	addr := svc.listenerAddr()
	_, port, _ := net.SplitHostPort(addr)
	url := info.URL
	if port != "" && port != fmt.Sprint(DefaultPort) {
		url = strings.Replace(url, fmt.Sprintf(":%d", DefaultPort), ":"+port, 1)
	}
	fmt.Printf("E2E_PAIR_URL=%s\n", url)
	fmt.Printf("E2E_CODE=%s\n", info.Code)
	fmt.Printf("E2E_ADDR=%s\n", addr)
	fmt.Printf("E2E_DIST=%s\n", dist)
	fmt.Println("E2E_READY=1 (holding 6 minutes for the browser run)")
	time.Sleep(6 * time.Minute)
}

// mobileFakeEngine 模拟可对话的引擎：内存态项目/会话/消息 + chat.start 流式
// 回复，形状对齐 web/src/generated/bridge.ts 的 DTO，供移动端全流程模拟。
type mobileFakeEngine struct {
	mu       sync.Mutex
	projects []map[string]any
	sessions []map[string]any
	messages map[string][]map[string]any // sessionId → 消息（时间序）
}

func newMobileFakeEngine() *mobileFakeEngine {
	return &mobileFakeEngine{
		projects: []map[string]any{},
		sessions: []map[string]any{},
		messages: map[string][]map[string]any{},
	}
}

func (e *mobileFakeEngine) Handle(_ context.Context, request bridge.Request) bridge.Response {
	response, _ := e.handleStream(request)
	return response
}

// HandleStreaming 实现 ipc.StreamingHandler：chat.start 先回 streamId，
// 再按序 emit delta/usage/completed（网关把响应前的事件缓存、响应后按序下发）。
func (e *mobileFakeEngine) HandleStreaming(_ context.Context, request bridge.Request, emit func(bridge.Event) error) bridge.Response {
	response, events := e.handleStream(request)
	for _, event := range events {
		if err := emit(event); err != nil {
			return request.Fail("STREAM_EMIT_FAILED", err.Error(), true)
		}
	}
	return response
}

func (e *mobileFakeEngine) handleStream(request bridge.Request) (bridge.Response, []bridge.Event) {
	payload := payloadMap(request)
	e.mu.Lock()
	defer e.mu.Unlock()
	switch request.Method {
	case "system.health":
		return request.Ok(map[string]any{"status": "ok"}), nil
	case "project.list":
		return request.Ok(map[string]any{"items": e.projects}), nil
	case "project.create":
		project := map[string]any{
			"id": dtoID("p"), "name": payload["name"], "projectCode": "ITM001",
			"type": "implementation", "status": "active",
			"createdAt": nowISO(), "updatedAt": nowISO(), "version": 1,
		}
		e.projects = append(e.projects, project)
		return request.Ok(project), nil
	case "session.list":
		items := []map[string]any{}
		for _, session := range e.sessions {
			if session["projectId"] == payload["projectId"] {
				items = append(items, session)
			}
		}
		return request.Ok(map[string]any{"items": items}), nil
	case "session.create":
		session := map[string]any{
			"id": dtoID("s"), "projectId": payload["projectId"], "title": payload["title"],
			"pinned": false, "status": "active",
			"createdAt": nowISO(), "updatedAt": nowISO(), "version": 1,
		}
		e.sessions = append(e.sessions, session)
		return request.Ok(session), nil
	case "session.update":
		for i, session := range e.sessions {
			if session["id"] == payload["id"] {
				if title, ok := payload["title"]; ok {
					session["title"] = title
				}
				session["updatedAt"] = nowISO()
				e.sessions[i] = session
				return request.Ok(session), nil
			}
		}
		return request.Fail("SESSION_NOT_FOUND", "session not found", false), nil
	case "session.delete":
		deletedID := strOf(payload["id"])
		kept := e.sessions[:0]
		for _, session := range e.sessions {
			if session["id"] != payload["id"] {
				kept = append(kept, session)
			}
		}
		e.sessions = kept
		return request.Ok(map[string]any{"deleted": true, "id": deletedID}), nil
	case "expert.list":
		return request.Ok(map[string]any{"experts": []any{}}), nil
	case "provider.list":
		return request.Ok(map[string]any{"items": []any{map[string]any{
			"id": dtoID(""), "name": "模拟供应商", "protocol": "openai_compatible",
			"baseUrl": "https://example.invalid/v1", "status": "enabled", "credentialState": "configured",
			"createdAt": nowISO(), "updatedAt": nowISO(), "version": 1, "credentialBackupCount": 0,
			"models": []any{
				map[string]any{"modelId": "mock-llm", "displayName": "模拟对话模型", "isDefault": true, "kind": "llm", "kindDefault": true, "supportsVision": false},
			},
		}}}), nil
	case "message.list":
		stored := e.messages[strOf(payload["sessionId"])]
		if stored == nil {
			stored = []map[string]any{}
		}
		// 客户端默认 backward（最新在前，序列号降序连续）；forward 反转。
		direction := strOf(payload["direction"])
		if direction == "" {
			direction = "backward"
		}
		items := make([]map[string]any, len(stored))
		copy(items, stored)
		if direction == "backward" {
			for i, j := 0, len(items)-1; i < j; i, j = i+1, j-1 {
				items[i], items[j] = items[j], items[i]
			}
		}
		snapshot := 0
		for _, message := range stored {
			if seq, ok := message["sequence"].(int); ok && seq > snapshot {
				snapshot = seq
			}
		}
		return request.Ok(map[string]any{
			"items": items, "hasMore": false, "nextCursor": nil, "snapshotSequence": snapshot,
		}), nil
	case "message.append":
		sessionID := strOf(payload["sessionId"])
		message := map[string]any{
			"id": dtoID("m"), "sessionId": sessionID, "role": "user", "status": "completed",
			"sequence": len(e.messages[sessionID]) + 1, "text": payload["text"], "createdAt": nowISO(),
		}
		e.messages[sessionID] = append(e.messages[sessionID], message)
		// 客户端校验 append 结果必须是完整 MessageDTO（isMessage）。
		return request.Ok(message), nil
	case "chat.start":
		sessionID := strOf(payload["sessionId"])
		streamID := ulid.Make().String()
		// 非 companion 模式 chat.start 载荷不带 messages：用户文本已由
		// message.append 落库，从会话最近一条 user 消息取；companion
		// 模式回退到 payload["messages"]。
		userText := ""
		if messages, ok := payload["messages"].([]any); ok && len(messages) > 0 {
			if first, ok := messages[0].(map[string]any); ok {
				userText = strOf(first["content"])
			}
		}
		if userText == "" {
			for i := len(e.messages[sessionID]) - 1; i >= 0; i-- {
				if e.messages[sessionID][i]["role"] == "user" {
					userText = strOf(e.messages[sessionID][i]["text"])
					break
				}
			}
		}
		reply := "已收到你的消息「" + userText + "」。这是模拟引擎的流式回复，用来验证移动端对话链路完整可用。"
		sequence := uint64(0)
		next := func() uint64 { sequence++; return sequence }
		events := []bridge.Event{}
		for _, chunk := range []string{"已收到你的消息「", userText, "」。", "这是模拟引擎的流式回复，", "用来验证移动端对话链路完整可用。"} {
			events = append(events, bridge.Event{
				Version: bridge.Version, Kind: "event", ID: ulid.Make().String(),
				StreamID: streamID, Sequence: next(), Type: "delta", Delta: &bridge.DeltaEvent{Text: chunk},
			})
		}
		events = append(events, bridge.Event{
			Version: bridge.Version, Kind: "event", ID: ulid.Make().String(),
			StreamID: streamID, Sequence: next(), Type: "usage",
			Usage: &bridge.UsageEvent{InputTokens: 12, OutputTokens: 34, TotalTokens: 46},
		})
		messageID := dtoID("m")
		events = append(events, bridge.Event{
			Version: bridge.Version, Kind: "event", ID: ulid.Make().String(),
			StreamID: streamID, Sequence: next(), Type: "completed",
			Completed: &bridge.CompletedEvent{MessageID: messageID},
		})
		e.messages[sessionID] = append(e.messages[sessionID], map[string]any{
			"id": messageID, "sessionId": sessionID, "role": "assistant", "status": "completed",
			"sequence": len(e.messages[sessionID]) + 1, "text": reply, "createdAt": nowISO(),
		})
		return request.Ok(map[string]any{"streamId": streamID}), events
	case "run.queueInput":
		// 形状对齐 RunQueueInputResult；turn_boundary 是客户端默认标记。
		return request.Ok(map[string]any{
			"queuedId": dtoID("q"), "seq": 1, "status": "queued", "mark": "turn_boundary",
		}), nil
	case "run.queueList":
		// 空队列即可：inputQueue.refresh 只读 items/delivery，items 必须是数组。
		return request.Ok(map[string]any{"items": []any{}}), nil
	case "run.queueConsume":
		return request.Ok(map[string]any{"count": 0, "items": []any{}}), nil
	case "run.queueWithdraw":
		return request.Ok(map[string]any{
			"queuedId": strOf(payload["queuedId"]), "status": "withdrawn",
		}), nil
	default:
		return request.Ok(map[string]any{"ok": true}), nil
	}
}

func strOf(value any) string {
	text, _ := value.(string)
	return text
}

func payloadMap(request bridge.Request) map[string]any {
	var payload map[string]any
	_ = json.Unmarshal(request.Payload, &payload)
	return payload
}

func dtoID(string) string { return ulid.Make().String() }

func nowISO() string { return time.Now().UTC().Format(time.RFC3339Nano) }

// TestE2EMobileServe 移动端全流程模拟壳：真实 dist + 可对话的模拟引擎。
// LUNITIDE_E2E_MOBILE=1 启用；Playwright 以 Android UA 完成
// 配对 → 主界面 → 发消息 → 流式回复 → 会话恢复全链路。
func TestE2EMobileServe(t *testing.T) {
	if os.Getenv("LUNITIDE_E2E_MOBILE") != "1" {
		t.Skip("manual mobile e2e shell; set LUNITIDE_E2E_MOBILE=1")
	}
	dist := os.Getenv("LUNITIDE_E2E_DIST")
	if dist == "" {
		dist = "../../web/dist"
	}
	if _, err := os.Stat(dist + "/pair.html"); err != nil {
		t.Fatalf("dist has no pair.html: %v", err)
	}
	ctx := context.Background()
	svc, err := New(ctx, newTestRoot(t), newMobileFakeEngine(), "0.0.0-e2e")
	if err != nil {
		t.Fatal(err)
	}
	defer svc.Close()
	svc.rendererDirOverride = dist
	svc.port = 0
	if err := svc.Enable(ctx); err != nil {
		t.Fatal(err)
	}
	info, err := svc.IssuePairCode(ctx, "zh-CN")
	if err != nil {
		t.Fatal(err)
	}
	addr := svc.listenerAddr()
	_, port, _ := net.SplitHostPort(addr)
	url := info.URL
	if port != "" && port != fmt.Sprint(DefaultPort) {
		url = strings.Replace(url, fmt.Sprintf(":%d", DefaultPort), ":"+port, 1)
	}
	fmt.Printf("E2E_PAIR_URL=%s\n", url)
	fmt.Printf("E2E_CODE=%s\n", info.Code)
	fmt.Printf("E2E_ADDR=%s\n", addr)
	fmt.Println("E2E_READY=1 (holding 6 minutes for the mobile browser run)")
	time.Sleep(6 * time.Minute)
}
