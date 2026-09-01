package app

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"strings"
	"sync"
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

const (
	inboundAutoRunTimeout    = 2 * time.Minute
	inboundAutoRunDeadlineMS = 120_000
)

func handleImInboundDeliver(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p struct {
		Kind           string `json:"kind"`
		Sender         string `json:"sender"`
		Text           string `json:"text"`
		MessageID      string `json:"messageId"`
		ConversationID string `json:"conversationId"`
	}
	if decodePayload(r.Payload, &p) != nil {
		return bridge.Failure(r.ID, r.TraceID, "BRIDGE_SCHEMA_INVALID", "im.inbound.deliver 参数无效", false)
	}
	kind, err := imapp.ParseKind(p.Kind)
	if err != nil || !imappInboundKind(kind) {
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
	auto := imapp.InboundShouldAutoRun(ch)
	e.pairInboundSender(ctx, &ch, p.Sender)
	if failure := requireIdempotency(r); failure != nil {
		return *failure
	}
	sessionID, err := e.parkInboundMessage(ctx, r.IdempotencyKey, ch, p.Sender, p.Text, p.ConversationID)
	if err != nil {
		return bridge.Failure(r.ID, r.TraceID, "STORAGE_UNAVAILABLE", "无法写入入站会话", true)
	}
	ran := false
	if auto {
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
		return bridge.Failure(r.ID, r.TraceID, "BRIDGE_SCHEMA_INVALID", "仅飞书、企业微信和钉钉支持入站", false)
	default:
		return bridge.Failure(r.ID, r.TraceID, "BRIDGE_SCHEMA_INVALID", "入站消息无效", false)
	}
}

func imappInboundKind(kind imapp.Kind) bool {
	return kind == imapp.KindFeishu || kind == imapp.KindWeCom || kind == imapp.KindDingTalk
}

type inboundRoute struct {
	Kind           imapp.Kind
	Sender         string
	ConversationID string
}

func (e *Engine) rememberInboundRoute(sessionID string, ch imapp.Channel, sender, conversationID string) {
	if e == nil || strings.TrimSpace(sessionID) == "" {
		return
	}
	e.inboundRoutes.Store(sessionID, inboundRoute{
		Kind:           ch.Kind,
		Sender:         strings.TrimSpace(sender),
		ConversationID: strings.TrimSpace(conversationID),
	})
	e.saveInboundRoutes()
}

func (e *Engine) setWeComWriter(write func([]byte) error) {
	if e == nil {
		return
	}
	e.wecomWriteMu.Lock()
	e.wecomWrite = write
	e.wecomWriteMu.Unlock()
}

func (e *Engine) sendWeComInboundReply(sender, conversationID, text string) error {
	if e == nil {
		return errors.New("engine unavailable")
	}
	e.wecomWriteMu.Lock()
	write := e.wecomWrite
	e.wecomWriteMu.Unlock()
	if write == nil {
		return errors.New("wecom inbound stream is down")
	}
	chatID := strings.TrimSpace(conversationID)
	group := chatID != "" && chatID != strings.TrimSpace(sender)
	if chatID == "" {
		chatID = strings.TrimSpace(sender)
		group = false
	}
	if chatID == "" {
		return errors.New("wecom reply missing chat id")
	}
	return write(imapp.WeComSendMsgPayload(chatID, group, text))
}

func (e *Engine) pushInboundReply(sessionID, text string) {
	if e == nil || strings.TrimSpace(sessionID) == "" || strings.TrimSpace(text) == "" {
		return
	}
	raw, ok := e.inboundRoutes.Load(sessionID)
	if !ok {
		return
	}
	route, ok := raw.(inboundRoute)
	if !ok || route.Sender == "" {
		return
	}
	go func() {
		var err error
		if route.Kind == imapp.KindWeCom {
			err = e.sendWeComInboundReply(route.Sender, route.ConversationID, text)
		} else if e.imChannels == nil {
			return
		} else {
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()
			ch, lookErr := e.imChannels.LookupSecret(ctx, route.Kind)
			if lookErr != nil || ch.Secret() == "" || ch.InboundAppID == "" {
				err = errors.New("im inbound reply missing channel credentials")
			} else {
				err = e.imReply.Send(ctx, route.Kind, ch.InboundAppID, ch.Secret(), route.Sender, route.ConversationID, text)
			}
		}
		if err != nil {
			log.Printf("im inbound reply: %v", err)
			e.noteInboundReplyFailure(sessionID, route.Kind, err)
		}
	}()
}

func inboundReplyFailureNotice(kind imapp.Kind, err error) string {
	msg := "通道暂时不可用"
	if err != nil && strings.TrimSpace(err.Error()) != "" {
		msg = strings.TrimSpace(err.Error())
	}
	body := "【回程失败】没有发回" + imapp.Label(kind) + "：" + msg + "。回答只留在工作台。"
	if n := utf8.RuneCountInString(body); n > message.MaxRunes {
		body = string([]rune(body)[:message.MaxRunes])
	}
	return body
}

func (e *Engine) noteInboundReplyFailure(sessionID string, kind imapp.Kind, err error) {
	if e == nil || !messageServiceAvailable(e.messages) || strings.TrimSpace(sessionID) == "" {
		return
	}
	text := inboundReplyFailureNotice(kind, err)
	norm, nerr := message.NormalizeText(text)
	if nerr != nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	_, _ = e.messages.Append(ctx, ulid.Make().String(), "im-inbound-reply", map[string]string{
		"sessionId": sessionID, "text": norm,
	}, message.Message{SessionID: sessionID, Text: norm})
}

// inboundSessionTitleFor keeps each colleague's inbound thread separate. Keying
// the session only by channel put every sender's messages in one conversation:
// history bled across people and one sender's text could steer a reply meant
// for another. The sender is bounded so the combined title stays inside
// NormalizeTitle's 200-rune ceiling.
func inboundSessionTitleFor(kind imapp.Kind, sender string) string {
	base := imapp.InboundSessionTitle(kind)
	s := strings.TrimSpace(sender)
	if s == "" {
		return base
	}
	if r := []rune(s); len(r) > 80 {
		s = string(r[:80])
	}
	return base + " · " + s
}

func (e *Engine) parkInboundMessage(ctx context.Context, idempotencyKey string, ch imapp.Channel, sender, text, conversationID string) (string, error) {
	if !projectServiceAvailable(e.projects) || !sessionServiceAvailable(e.sessions) || !messageServiceAvailable(e.messages) {
		return "", errors.New("im inbound storage unavailable")
	}
	projectID, err := e.ensurePersonalChatProject(ctx)
	if err != nil {
		return "", err
	}
	title, err := session.NormalizeTitle(inboundSessionTitleFor(ch.Kind, sender))
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
	e.rememberInboundRoute(sessionID, ch, sender, conversationID)
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
	entry, ok := e.resolvePreferredChatModel(items)
	if !ok {
		return false
	}
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
		runCtx, cancel := context.WithTimeout(context.Background(), inboundAutoRunTimeout)
		defer cancel()
		runCtx = context.WithValue(runCtx, streamParentKey{}, runCtx)
		runCtx = context.WithValue(runCtx, eventEmitterKey{}, EventEmitter(func(bridge.Event) error { return nil }))
		// No human is watching this turn: the emitter above is a noop, so a tool
		// that needs approval must be denied in place rather than pausing the
		// loop for a decision that can never arrive (see decideApprovalOutcome).
		runCtx = withUnattended(runCtx)
		req := bridge.Request{
			Version: bridge.Version, Kind: "request",
			ID: ulid.Make().String(), TraceID: ulid.Make().String(),
			Method: "chat.start", SentAt: time.Now().UTC(),
			Payload: payload, DeadlineMS: inboundAutoRunDeadlineMS,
		}
		resp := handleChatStart(e, runCtx, req)
		if !resp.OK {
			log.Printf("im inbound auto-run failed: %+v", resp.Error)
		}
	}()
	return true
}

func (e *Engine) pairInboundSender(ctx context.Context, ch *imapp.Channel, sender string) {
	if e == nil || e.imChannels == nil || ch == nil {
		return
	}
	if len(imapp.ParseAllowlist(ch.InboundAllowlist)) > 0 {
		return
	}
	sender = strings.TrimSpace(sender)
	if sender == "" {
		return
	}
	if _, err := e.imChannels.Set(ctx, ch.Kind, imapp.ChannelPatch{InboundAllowlist: &sender}); err != nil {
		log.Printf("im inbound pair: %v", err)
		return
	}
	ch.InboundAllowlist = sender
}

// StartIMInbound connects outbound to Feishu / WeCom / DingTalk streams when
// inbound is enabled and credentials exist. No listen port. Safe to call once from cmd/engine.
func (e *Engine) StartIMInbound(ctx context.Context) {
	if e == nil || e.imChannels == nil {
		return
	}
	go e.loopFeishuInbound(ctx)
	go e.loopWeComInbound(ctx)
	go e.loopDingTalkInbound(ctx)
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
		auto := imapp.InboundShouldAutoRun(ch)
		e.pairInboundSender(context.Background(), &ch, msg.Sender)
		key := "im-inbound-" + msg.MessageID
		if strings.TrimSpace(msg.MessageID) == "" {
			key = ulid.Make().String()
		}
		sessionID, err := e.parkInboundMessage(context.Background(), key, ch, msg.Sender, msg.Text, msg.ConversationID)
		if err != nil {
			log.Printf("feishu inbound park: %v", err)
			return
		}
		if auto {
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

func (e *Engine) loopWeComInbound(ctx context.Context) {
	for {
		if ctx.Err() != nil {
			return
		}
		e.tickWeComInbound(ctx)
		timer := time.NewTimer(20 * time.Second)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
	}
}

func (e *Engine) tickWeComInbound(ctx context.Context) {
	ch, err := e.imChannels.LookupSecret(ctx, imapp.KindWeCom)
	if err != nil || !ch.InboundEnabled || ch.InboundAppID == "" || ch.Secret() == "" {
		return
	}
	if err := runWeComWebsocket(ctx, imapp.WeComOpenWS, ch.InboundAppID, ch.Secret(), e.setWeComWriter, func(msg imapp.WeComInboundMessage) {
		if err := imapp.AdmitInbound(ch, msg.Sender, msg.Text); err != nil {
			return
		}
		auto := imapp.InboundShouldAutoRun(ch)
		e.pairInboundSender(context.Background(), &ch, msg.Sender)
		key := "im-inbound-" + msg.MessageID
		if strings.TrimSpace(msg.MessageID) == "" {
			key = ulid.Make().String()
		}
		sessionID, err := e.parkInboundMessage(context.Background(), key, ch, msg.Sender, msg.Text, msg.ConversationID)
		if err != nil {
			log.Printf("wecom inbound park: %v", err)
			return
		}
		if auto {
			e.kickInboundChat(sessionID, msg.Text)
		}
	}); err != nil && ctx.Err() == nil {
		log.Printf("wecom inbound websocket: %v", err)
	}
}

func runWeComWebsocket(ctx context.Context, wsURL, botID, secret string, onWriter func(func([]byte) error), onMessage func(imapp.WeComInboundMessage)) error {
	dialer := websocket.Dialer{HandshakeTimeout: 30 * time.Second}
	conn, _, err := dialer.DialContext(ctx, wsURL, nil)
	if err != nil {
		return err
	}
	defer conn.Close()
	wsCtx, stop := context.WithCancel(ctx)
	defer stop()
	var writeMu sync.Mutex
	writeJSON := func(payload []byte) error {
		writeMu.Lock()
		defer writeMu.Unlock()
		return conn.WriteMessage(websocket.TextMessage, payload)
	}
	if onWriter != nil {
		onWriter(writeJSON)
		defer onWriter(nil)
	}
	if err := writeJSON(imapp.WeComSubscribePayload(botID, secret)); err != nil {
		return err
	}
	go func() {
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-wsCtx.Done():
				return
			case <-ticker.C:
				if err := writeJSON(imapp.WeComPingPayload()); err != nil {
					return
				}
			}
		}
	}()
	for {
		if ctx.Err() != nil {
			return nil
		}
		_ = conn.SetReadDeadline(time.Now().Add(90 * time.Second))
		_, data, err := conn.ReadMessage()
		if err != nil {
			return err
		}
		msg, ok := imapp.ParseWeComMessageEvent(data)
		if !ok {
			continue
		}
		onMessage(msg)
	}
}

func (e *Engine) loopDingTalkInbound(ctx context.Context) {
	for {
		if ctx.Err() != nil {
			return
		}
		e.tickDingTalkInbound(ctx)
		timer := time.NewTimer(20 * time.Second)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
	}
}

func (e *Engine) tickDingTalkInbound(ctx context.Context) {
	ch, err := e.imChannels.LookupSecret(ctx, imapp.KindDingTalk)
	if err != nil || !ch.InboundEnabled || ch.InboundAppID == "" || ch.Secret() == "" {
		return
	}
	ep, err := imapp.FetchDingTalkEndpoint(ctx, http.DefaultClient, imapp.DingTalkOpenAPI, ch.InboundAppID, ch.Secret())
	if err != nil {
		log.Printf("dingtalk inbound endpoint: %v", err)
		return
	}
	if err := runDingTalkWebsocket(ctx, imapp.DingTalkStreamURL(ep), func(msg imapp.DingTalkInboundMessage) {
		if err := imapp.AdmitInbound(ch, msg.Sender, msg.Text); err != nil {
			return
		}
		auto := imapp.InboundShouldAutoRun(ch)
		e.pairInboundSender(context.Background(), &ch, msg.Sender)
		key := "im-inbound-" + msg.MessageID
		if strings.TrimSpace(msg.MessageID) == "" {
			key = ulid.Make().String()
		}
		sessionID, err := e.parkInboundMessage(context.Background(), key, ch, msg.Sender, msg.Text, msg.ConversationID)
		if err != nil {
			log.Printf("dingtalk inbound park: %v", err)
			return
		}
		if auto {
			e.kickInboundChat(sessionID, msg.Text)
		}
	}); err != nil && ctx.Err() == nil {
		log.Printf("dingtalk inbound websocket: %v", err)
	}
}

func runDingTalkWebsocket(ctx context.Context, wsURL string, onMessage func(imapp.DingTalkInboundMessage)) error {
	if strings.TrimSpace(wsURL) == "" {
		return errors.New("dingtalk stream url empty")
	}
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
		msg, ok := imapp.ParseDingTalkMessageEvent(data)
		if !ok {
			continue
		}
		if ack := imapp.DingTalkStreamAck(msg.MessageID); len(ack) > 0 {
			_ = conn.WriteMessage(websocket.TextMessage, ack)
		}
		onMessage(msg)
	}
}
