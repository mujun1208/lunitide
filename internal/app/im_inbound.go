package app

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/gorilla/websocket"
	"github.com/lunitide/lunitide/internal/bridge"
	"github.com/lunitide/lunitide/internal/domain/message"
	"github.com/lunitide/lunitide/internal/domain/project"
	"github.com/lunitide/lunitide/internal/domain/provider"
	"github.com/lunitide/lunitide/internal/domain/session"
	"github.com/lunitide/lunitide/internal/imapp"
	"github.com/oklog/ulid/v2"
)

func handleImInboundDeliver(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p struct {
		Kind      string `json:"kind"`
		Sender    string `json:"sender"`
		Text      string `json:"text"`
		MessageID string `json:"messageId"`
	}
	if decodePayload(r.Payload, &p) != nil {
		return bridge.Failure(r.ID, r.TraceID, "BRIDGE_SCHEMA_INVALID", "im.inbound.deliver 参数无效", false)
	}
	kind, err := imapp.ParseKind(p.Kind)
	if err != nil || (kind != imapp.KindFeishu && kind != imapp.KindWeCom) {
		return bridge.Failure(r.ID, r.TraceID, "BRIDGE_SCHEMA_INVALID", "im.inbound.deliver 参数无效", false)
	}
	if e.imChannels == nil {
		return bridge.Failure(r.ID, r.TraceID, "STORAGE_UNAVAILABLE", "消息通道暂时不可用", true)
	}
	ch, err := e.imChannels.Lookup(ctx, kind)
	if err != nil {
		return bridge.Failure(r.ID, r.TraceID, "STORAGE_UNAVAILABLE", "无法读取消息通道", true)
	}
	if err := imapp.AdmitInbound(ch, p.Sender, p.Text); err != nil {
		return inboundAdmitFailure(r, err)
	}
	if failure := requireIdempotency(r); failure != nil {
		return *failure
	}
	sessionID, err := e.parkInboundMessage(ctx, r.IdempotencyKey, ch, p.Sender, p.Text)
	if err != nil {
		return bridge.Failure(r.ID, r.TraceID, "STORAGE_UNAVAILABLE", "无法写入入站会话", true)
	}
	ran := false
	if ch.InboundAutoRun {
		ran = e.kickInboundChat(sessionID, p.Text)
	}
	return bridge.Success(r.ID, map[string]any{
		"accepted":  true,
		"sessionId": sessionID,
		"autoRun":   ran,
	})
}

func inboundAdmitFailure(r bridge.Request, err error) bridge.Response {
	switch {
	case errors.Is(err, imapp.ErrInboundOff):
		return bridge.Failure(r.ID, r.TraceID, "IM_INBOUND_OFF", "入站未启用", false)
	case errors.Is(err, imapp.ErrInboundDenied):
		return bridge.Failure(r.ID, r.TraceID, "IM_INBOUND_DENIED", "发送者不在白名单", false)
	case errors.Is(err, imapp.ErrInboundKind):
		return bridge.Failure(r.ID, r.TraceID, "BRIDGE_SCHEMA_INVALID", "仅飞书和企业微信支持入站", false)
	default:
		return bridge.Failure(r.ID, r.TraceID, "BRIDGE_SCHEMA_INVALID", "入站消息无效", false)
	}
}

func (e *Engine) parkInboundMessage(ctx context.Context, idempotencyKey string, ch imapp.Channel, sender, text string) (string, error) {
	if !projectServiceAvailable(e.projects) || !sessionServiceAvailable(e.sessions) || !messageServiceAvailable(e.messages) {
		return "", errors.New("im inbound storage unavailable")
	}
	projectID, err := e.ensurePersonalChatProject(ctx)
	if err != nil {
		return "", err
	}
	title, err := session.NormalizeTitle(imapp.InboundSessionTitle(ch.Kind))
	if err != nil {
		return "", err
	}
	sessionID, err := e.ensureInboundSession(ctx, projectID, title)
	if err != nil {
		return "", err
	}
	body := "【" + ch.Label + " · " + strings.TrimSpace(sender) + "】\n" + strings.TrimSpace(text)
	if n := utf8.RuneCountInString(body); n > message.MaxRunes {
		runes := []rune(body)
		body = string(runes[:message.MaxRunes])
	}
	norm, err := message.NormalizeText(body)
	if err != nil {
		return "", err
	}
	_, err = e.messages.Append(ctx, idempotencyKey, "im-inbound", map[string]string{
		"sessionId": sessionID, "text": norm,
	}, message.Message{SessionID: sessionID, Text: norm})
	if err != nil {
		return "", err
	}
	return sessionID, nil
}

func (e *Engine) ensurePersonalChatProject(ctx context.Context) (string, error) {
	items, err := e.projects.List(ctx, project.Filter{})
	if err != nil {
		return "", err
	}
	for _, item := range items {
		if item.Name == imapp.PersonalChatProjectName || strings.HasPrefix(item.Name, "\u2063") {
			return item.ID, nil
		}
	}
	name, err := project.NormalizeName(imapp.PersonalChatProjectName)
	if err != nil {
		return "", err
	}
	created, err := e.projects.Create(ctx, "im-inbound-personal", "im-inbound", map[string]string{"name": name}, project.Project{
		Name: name, Type: project.TypeImplementation, Status: project.StatusCreated,
	})
	if err != nil {
		return "", err
	}
	return created.ID, nil
}

func (e *Engine) ensureInboundSession(ctx context.Context, projectID, title string) (string, error) {
	listed, err := e.sessions.List(ctx, session.Filter{ProjectID: projectID})
	if err != nil {
		return "", err
	}
	for _, item := range listed {
		if item.Title == title {
			return item.ID, nil
		}
	}
	created, err := e.sessions.Create(ctx, ulid.Make().String(), "im-inbound", map[string]string{
		"projectId": projectID, "title": title,
	}, session.Session{ProjectID: projectID, Title: title})
	if err != nil {
		return "", err
	}
	if _, dirErr := e.sessionOutputDir(created.ID); dirErr != nil {
		_ = dirErr
	}
	return created.ID, nil
}

func (e *Engine) kickInboundChat(sessionID, text string) bool {
	if e.providers == nil || strings.TrimSpace(sessionID) == "" {
		return false
	}
	ctx := context.Background()
	items, err := e.providers.List(ctx, provider.Filter{})
	if err != nil {
		return false
	}
	catalog := provider.CatalogForKind(items, provider.KindLLM)
	if len(catalog) == 0 {
		return false
	}
	entry := catalog[0]
	payload, err := json.Marshal(map[string]any{
		"providerId":    entry.Provider.ID,
		"modelId":       entry.Model.ModelID,
		"sessionId":     sessionID,
		"executionMode": "auto-edit",
		"messages":      []map[string]string{{"role": "user", "content": text}},
	})
	if err != nil {
		return false
	}
	go func() {
		runCtx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
		defer cancel()
		runCtx = context.WithValue(runCtx, streamParentKey{}, runCtx)
		runCtx = context.WithValue(runCtx, eventEmitterKey{}, EventEmitter(func(bridge.Event) error { return nil }))
		req := bridge.Request{
			Version: bridge.Version, Kind: "request",
			ID: ulid.Make().String(), TraceID: ulid.Make().String(),
			Method: "chat.start", SentAt: time.Now().UTC(),
			Payload: payload, DeadlineMS: 30000,
		}
		resp := handleChatStart(e, runCtx, req)
		if !resp.OK {
			log.Printf("im inbound auto-run failed: %+v", resp.Error)
		}
	}()
	return true
}

// StartIMInbound connects outbound to Feishu long-connection when inbound is
// enabled and credentials exist. No listen port. Safe to call once from cmd/engine.
func (e *Engine) StartIMInbound(ctx context.Context) {
	if e == nil || e.imChannels == nil {
		return
	}
	go e.loopFeishuInbound(ctx)
}

func (e *Engine) loopFeishuInbound(ctx context.Context) {
	for {
		if ctx.Err() != nil {
			return
		}
		e.tickFeishuInbound(ctx)
		timer := time.NewTimer(20 * time.Second)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
	}
}

func (e *Engine) tickFeishuInbound(ctx context.Context) {
	ch, err := e.imChannels.LookupSecret(ctx, imapp.KindFeishu)
	if err != nil || !ch.InboundEnabled || ch.InboundAppID == "" || ch.Secret() == "" {
		return
	}
	ep, err := imapp.FetchFeishuEndpoint(ctx, http.DefaultClient, imapp.FeishuOpenDomain, ch.InboundAppID, ch.Secret())
	if err != nil {
		log.Printf("feishu inbound endpoint: %v", err)
		return
	}
	if err := runFeishuWebsocket(ctx, ep.URL, func(msg imapp.FeishuInboundMessage) {
		if err := imapp.AdmitInbound(ch, msg.Sender, msg.Text); err != nil {
			return
		}
		key := "im-inbound-" + msg.MessageID
		if strings.TrimSpace(msg.MessageID) == "" {
			key = ulid.Make().String()
		}
		sessionID, err := e.parkInboundMessage(context.Background(), key, ch, msg.Sender, msg.Text)
		if err != nil {
			log.Printf("feishu inbound park: %v", err)
			return
		}
		if ch.InboundAutoRun {
			e.kickInboundChat(sessionID, msg.Text)
		}
	}); err != nil && ctx.Err() == nil {
		log.Printf("feishu inbound websocket: %v", err)
	}
}

func runFeishuWebsocket(ctx context.Context, wsURL string, onMessage func(imapp.FeishuInboundMessage)) error {
	dialer := websocket.Dialer{HandshakeTimeout: 30 * time.Second}
	conn, _, err := dialer.DialContext(ctx, wsURL, nil)
	if err != nil {
		return err
	}
	defer conn.Close()
	for {
		if ctx.Err() != nil {
			return nil
		}
		_ = conn.SetReadDeadline(time.Now().Add(4 * time.Minute))
		_, data, err := conn.ReadMessage()
		if err != nil {
			return err
		}
		msg, ok := imapp.ParseFeishuMessageEvent(data)
		if !ok {
			continue
		}
		onMessage(msg)
	}
}
