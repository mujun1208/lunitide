package app

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/lunitide/lunitide/internal/bridge"
	"github.com/lunitide/lunitide/internal/domain/provider"
	"github.com/lunitide/lunitide/internal/secretlease"
	"github.com/lunitide/lunitide/internal/talk"
	"github.com/oklog/ulid/v2"
)

var errTalkClosed = errors.New("talk session closed")

func handleTalkStart(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p struct {
		ProviderID string `json:"providerId"`
		ModelID    string `json:"modelId"`
		SessionID  string `json:"sessionId"`
	}
	if decodePayload(r.Payload, &p) != nil || !ulidValid(p.ProviderID) || !ulidValid(p.SessionID) || len(p.ModelID) < 1 || len(p.ModelID) > 128 {
		return r.Fail("BRIDGE_SCHEMA_INVALID", "talk.start 参数无效", false)
	}
	if e.providers == nil {
		return r.Fail(talkModelUnsupportedCode, "没有可通话的 realtime/live 模型，这轮用语模型", false)
	}
	item, err := e.providers.Get(ctx, p.ProviderID)
	if err != nil {
		if err == provider.ErrNotFound {
			return r.Fail(talkModelUnsupportedCode, "供应商不存在，无法开通话核", false)
		}
		return r.Fail(talkModelUnsupportedCode, "无法读取供应商", false)
	}
	if _, ok := resolveTalkModel(item, p.ModelID); !ok {
		return r.Fail(talkModelUnsupportedCode, "模型不是已列出的 realtime/live，这轮用语模型", false)
	}
	emit, ok := ctx.Value(eventEmitterKey{}).(EventEmitter)
	if !ok || emit == nil {
		return r.Fail(talkAdapterUnreadyCode, "通话核适配还没接通，这轮用语模型", true)
	}
	if e.leases == nil {
		return r.Fail(talkAdapterUnreadyCode, "通话核密钥不可用，这轮用语模型", true)
	}
	wsURL, err := talk.RealtimeWebSocketURL(item.BaseURL, p.ModelID)
	if err != nil {
		return r.Fail(talkAdapterUnreadyCode, "通话核地址无效，这轮用语模型", true)
	}

	e.streamsMu.Lock()
	if len(e.streams) >= e.maxStreams {
		e.streamsMu.Unlock()
		return r.Fail("STREAM_LIMIT_REACHED", "并发流数量已达上限", true)
	}
	talkID, streamID := newTalkIDs()
	parent, _ := ctx.Value(streamParentKey{}).(context.Context)
	if parent == nil {
		parent = ctx
	}
	streamCtx, cancel := context.WithCancel(parent)
	state := &streamState{cancel: cancel, talk: true}
	e.streams[streamID] = state
	e.streamsMu.Unlock()

	session := &talkSession{talkID: talkID, streamID: streamID, sessionID: p.SessionID, cancel: cancel}
	var connected bool
	defer func() {
		if connected {
			return
		}
		cancel()
		e.finishTerminal(streamID, state)
	}()

	dialCtx, dialCancel := context.WithTimeout(streamCtx, 6*time.Second)
	defer dialCancel()
	leaseErr := e.withProviderLease(dialCtx, item, secretlease.OperationChat, func(op context.Context, secret []byte) error {
		header := http.Header{}
		key := strings.TrimSpace(string(secret))
		if key != "" {
			header.Set("Authorization", "Bearer "+key)
			header.Set("api-key", key)
		}
		header.Set("OpenAI-Beta", "realtime=v1")
		conn, dialErr := e.talkDial(op, wsURL, header)
		if dialErr != nil {
			return dialErr
		}
		if writeErr := session.writePrep(conn, talk.SessionUpdateMessage(companionPersonaChatInstruction())); writeErr != nil {
			_ = conn.Close()
			return writeErr
		}
		session.conn = conn
		return nil
	})
	if leaseErr != nil {
		return r.Fail(talkAdapterUnreadyCode, "通话核适配还没接通，这轮用语模型", true)
	}
	e.putTalk(session)
	connected = true
	go e.runTalkStream(streamCtx, session, state, emit)
	return r.Ok(map[string]any{"talkId": talkID, "streamId": streamID})
}

func (s *talkSession) writePrep(conn talk.Conn, raw []byte) error {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	s.conn = conn
	return talk.WriteText(conn, raw)
}

func handleTalkAppend(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p struct {
		SessionID string `json:"sessionId"`
		PCM       string `json:"pcm"`
	}
	if decodePayload(r.Payload, &p) != nil || !ulidValid(p.SessionID) || p.PCM == "" {
		return r.Fail("BRIDGE_SCHEMA_INVALID", "talk.append 参数无效", false)
	}
	session := e.talkBySession(p.SessionID)
	if session == nil {
		return r.Fail(talkSessionMissingCode, "没有通话核会话", false)
	}
	if err := session.write(talk.AppendAudioMessage(p.PCM)); err != nil {
		return r.Fail(talkSessionMissingCode, "通话核会话已结束", true)
	}
	return r.Ok(map[string]any{"accepted": true})
}

func handleTalkCancel(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p struct {
		SessionID string `json:"sessionId"`
		Mode      string `json:"mode"`
	}
	if decodePayload(r.Payload, &p) != nil || !ulidValid(p.SessionID) || (p.Mode != "output" && p.Mode != "all") {
		return r.Fail("BRIDGE_SCHEMA_INVALID", "talk.cancel 参数无效", false)
	}
	session := e.talkBySession(p.SessionID)
	if session == nil {
		return r.Fail(talkSessionMissingCode, "没有通话核会话", false)
	}
	if p.Mode == "output" {
		_ = session.write(talk.CancelOutputMessage())
		return r.Ok(map[string]any{"cancelled": true})
	}
	e.dropTalk(p.SessionID, session.talkID)
	e.cancelStream(session.streamID)
	return r.Ok(map[string]any{"cancelled": true})
}

func (e *Engine) runTalkStream(ctx context.Context, session *talkSession, state *streamState, emit EventEmitter) {
	// The read loop below blocks on conn.ReadMessage() with no deadline.
	// cancelStream / CancelAllStreams / host stream.cancel only cancel the
	// stream context; they do not close the socket, so on page navigation the
	// reader would hang until the provider sends a frame or the TCP connection
	// dies — leaking this goroutine and holding the stream slot. Close the
	// socket on ctx cancel so the read returns promptly. shutdown() is
	// once-guarded, so the normal-exit path (dropTalk) stays safe.
	watchDone := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			session.shutdown()
		case <-watchDone:
		}
	}()
	defer close(watchDone)

	var sendMu sync.Mutex
	var seq uint64
	send := func(event bridge.Event) error {
		sendMu.Lock()
		defer sendMu.Unlock()
		seq++
		event.Version = bridge.Version
		event.Kind = "event"
		event.ID = ulid.Make().String()
		event.StreamID = session.streamID
		event.Sequence = seq
		return emit(event)
	}
	defer func() {
		e.dropTalk(session.sessionID, session.talkID)
		_ = send(bridge.Event{Type: bridge.EventTalkEnded})
		term := e.selectTerminal(session.streamID, state, ctx.Err())
		if term == bridge.EventFailed {
			_ = send(bridge.Event{Type: term, Error: &bridge.StreamError{Code: "TALK_SESSION_FAILED", Message: "通话核结束", Retryable: true}})
		} else {
			_ = send(bridge.Event{Type: term})
		}
		e.finishTerminal(session.streamID, state)
	}()

	var lastHandoff string
	for {
		if ctx.Err() != nil {
			return
		}
		session.writeMu.Lock()
		conn := session.conn
		session.writeMu.Unlock()
		if conn == nil {
			return
		}
		_, raw, err := conn.ReadMessage()
		if err != nil {
			return
		}
		ev := talk.ParseServerEvent(raw)
		switch ev.Kind {
		case "audio":
			if ev.Audio == "" {
				continue
			}
			_ = send(bridge.Event{Type: bridge.EventTalkAudio, Talk: &bridge.TalkEvent{AudioBase64: ev.Audio, Mime: "audio/pcm;rate=24000"}})
		case "transcript":
			if ev.Transcript == "" {
				continue
			}
			role := ev.Role
			if role != "user" && role != "assistant" {
				role = "assistant"
			}
			if role == "user" && companionWantsTools(ev.Transcript) && ev.Transcript != lastHandoff {
				lastHandoff = ev.Transcript
				_ = session.write(talk.CancelOutputMessage())
				_ = send(bridge.Event{Type: bridge.EventTalkTool, Talk: &bridge.TalkEvent{Name: "handoff", Text: ev.Transcript, Role: "user"}})
			}
			_ = send(bridge.Event{Type: bridge.EventTalkTranscript, Talk: &bridge.TalkEvent{Text: ev.Transcript, Role: role}})
		case "barge":
			_ = session.write(talk.CancelOutputMessage())
			_ = send(bridge.Event{Type: bridge.EventTalkError, Talk: &bridge.TalkEvent{Code: "TALK_BARGE", Message: "对着麦打断"}})
		case "error":
			_ = send(bridge.Event{Type: bridge.EventTalkError, Talk: &bridge.TalkEvent{Code: ev.Code, Message: ev.Message}})
			return
		}
	}
}
