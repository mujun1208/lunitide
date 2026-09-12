package agenthub

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
)

func kimiACPArgv() (string, []string) {
	return "kimi", []string{"acp"}
}

func resolveKimiACP(look LookPath) (string, []string, error) {
	if look == nil {
		look = defaultLookPath
	}
	exe, args := kimiACPArgv()
	path, err := look(exe)
	if err != nil || strings.TrimSpace(path) == "" {
		if err == nil {
			err = fmt.Errorf("kimi 未找到")
		}
		return "", nil, err
	}
	if runtime.GOOS == "windows" && strings.EqualFold(filepath.Ext(path), ".cmd") {
		node, nodeArgs, ok := resolveCursorNodeACP(path)
		if !ok {
			return "", nil, fmt.Errorf("kimi 无法解析为 node")
		}
		return node, nodeArgs, nil
	}
	return path, args, nil
}

var (
	kimiACPMu sync.Mutex
	kimiACPs  = map[*ThreadStore]*KimiACP{}
)

var _ ThreadAdapter = (*KimiACP)(nil)

type KimiACP struct {
	store           *ThreadStore
	look            LookPath
	startPersistent persistentStarter

	mu       sync.Mutex
	sessions map[string]*kimiACPSession
}

type kimiACPSession struct {
	proc      *PersistentProc
	threadID  string
	sessionID string
	access    string
	nextID    int64
	pending   map[int64]chan *acpRPC
	serverReq map[string]json.RawMessage
	reader    *bufio.Reader
	assist    strings.Builder
	rejected  strings.Builder
	ready     chan struct{}
	readyErr  error
	mu        sync.Mutex
}

func NewKimiACP(store *ThreadStore) *KimiACP {
	kimiACPMu.Lock()
	defer kimiACPMu.Unlock()
	if a, ok := kimiACPs[store]; ok {
		return a
	}
	a := &KimiACP{
		store:           store,
		look:            defaultLookPath,
		startPersistent: StartPersistent,
		sessions:        map[string]*kimiACPSession{},
	}
	kimiACPs[store] = a
	return a
}

func (a *KimiACP) Open(thread ThreadRecord) error {
	_, _ = a.ensure(thread)
	return nil
}

func (a *KimiACP) Close(threadID string) error {
	a.mu.Lock()
	sess := a.sessions[threadID]
	a.mu.Unlock()
	if sess == nil {
		return nil
	}
	<-sess.ready
	a.mu.Lock()
	if a.sessions[threadID] == sess {
		delete(a.sessions, threadID)
	}
	a.mu.Unlock()
	if sess.proc == nil {
		return nil
	}
	return sess.proc.Close()
}

func (a *KimiACP) Prompt(threadID, text string) error {
	thread, err := a.store.Get(threadID)
	if err != nil {
		return err
	}
	if thread.Status == "waiting_user" || thread.Status == "running" {
		return ErrThreadBusy
	}
	open, err := countOpenPrompts(a.store, threadID)
	if err != nil {
		return err
	}
	if open > 0 {
		return ErrThreadBusy
	}
	sess, err := a.ensure(thread)
	if err != nil {
		return err
	}
	var blocks []map[string]any
	if msgs, listErr := a.store.ListMessages(threadID); listErr == nil {
		for _, msg := range msgs {
			if msg.Role == "system" {
				blocks = append(blocks, map[string]any{"type": "text", "text": msg.Content})
			}
		}
	}
	if err = insertThreadMessage(a.store, threadID, "user", text); err != nil {
		return err
	}
	if err = setThreadStatus(a.store, threadID, "running"); err != nil {
		return err
	}
	blocks = append(blocks, map[string]any{"type": "text", "text": text})
	if err = sess.send("session/prompt", map[string]any{"sessionId": sess.sessionID, "prompt": blocks}, func(resp *acpRPC, callErr error) {
		if callErr != nil {
			a.fault(threadID, sess.proc, sess, callErr)
			return
		}
		sess.flushAssistant(a)
		if thread, getErr := a.store.Get(threadID); getErr == nil && thread.Status == "waiting_user" {
			return
		}
		_ = setThreadStatus(a.store, threadID, "success")
	}); err != nil {
		a.dropSession(threadID, sess)
		a.fault(threadID, sess.proc, sess, err)
		return err
	}
	return nil
}

func (a *KimiACP) Respond(threadID, callID, option string) error {
	a.mu.Lock()
	sess := a.sessions[threadID]
	a.mu.Unlock()
	if sess == nil {
		return fmt.Errorf("会话未打开")
	}
	select {
	case <-sess.ready:
		if sess.readyErr != nil || sess.proc == nil {
			return fmt.Errorf("会话未打开")
		}
	default:
		return fmt.Errorf("会话未打开")
	}
	sess.mu.Lock()
	rawID := sess.serverReq[callID]
	delete(sess.serverReq, callID)
	sess.mu.Unlock()
	if len(rawID) == 0 {
		rawID, _ = json.Marshal(callID)
	}
	body, err := json.Marshal(acpRPC{
		JSONRPC: "2.0",
		ID:      rawID,
		Result:  json.RawMessage(fmt.Sprintf(`{"outcome":{"outcome":"selected","optionId":%s}}`, strconv.Quote(option))),
	})
	if err != nil {
		return err
	}
	if _, err = sess.proc.stdin.Write(EncodeACPFrame(body)); err != nil {
		a.dropSession(threadID, sess)
		return err
	}
	if err = setPromptStatus(a.store, threadID, callID, "answered"); err != nil {
		return err
	}
	return setThreadStatus(a.store, threadID, "running")
}

func (a *KimiACP) ensure(thread ThreadRecord) (*kimiACPSession, error) {
	a.mu.Lock()
	if sess := a.sessions[thread.ID]; sess != nil {
		a.mu.Unlock()
		<-sess.ready
		if sess.readyErr != nil {
			return nil, sess.readyErr
		}
		return sess, nil
	}
	sess := &kimiACPSession{
		threadID:  thread.ID,
		access:    thread.AccessMode,
		pending:   map[int64]chan *acpRPC{},
		serverReq: map[string]json.RawMessage{},
		ready:     make(chan struct{}),
	}
	a.sessions[thread.ID] = sess
	a.mu.Unlock()
	err := a.handshake(thread, sess)
	sess.readyErr = err
	close(sess.ready)
	if err != nil {
		a.mu.Lock()
		if a.sessions[thread.ID] == sess {
			delete(a.sessions, thread.ID)
		}
		a.mu.Unlock()
		if sess.proc != nil {
			_ = sess.proc.Close()
		}
		return nil, err
	}
	return sess, nil
}

func (a *KimiACP) dropSession(threadID string, sess *kimiACPSession) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.sessions[threadID] == sess {
		delete(a.sessions, threadID)
	}
}

func (a *KimiACP) handshake(thread ThreadRecord, sess *kimiACPSession) error {
	exe, args, err := resolveKimiACP(a.look)
	if err != nil {
		a.fault(thread.ID, nil, sess, err)
		return err
	}
	start := a.startPersistent
	if start == nil {
		start = StartPersistent
	}
	proc, err := start(context.Background(), ProcSpec{Exe: exe, Dir: thread.WorkspaceRoot, Args: args})
	if err != nil {
		a.fault(thread.ID, nil, sess, err)
		return err
	}
	sess.proc = proc
	sess.reader = bufio.NewReader(proc.stdout)
	go sess.pump(a)
	_, err = sess.call("initialize", map[string]any{
		"protocolVersion": 1,
		"clientCapabilities": map[string]any{
			"fs": map[string]any{"readTextFile": false, "writeTextFile": false},
		},
		"clientInfo": map[string]any{"name": "lunitide", "version": "0.1.0"},
	})
	if err != nil {
		a.fault(thread.ID, proc, sess, err)
		return err
	}
	newResp, err := sess.call("session/new", map[string]any{"cwd": thread.WorkspaceRoot, "mcpServers": []any{}})
	if err != nil {
		a.fault(thread.ID, proc, sess, err)
		return err
	}
	var created struct {
		SessionID string `json:"sessionId"`
	}
	if newResp != nil {
		_ = json.Unmarshal(newResp.Result, &created)
	}
	sess.sessionID = created.SessionID
	_, _ = a.store.db.Exec(`UPDATE agent_hub_threads SET native_session_id=? WHERE id=?`, sess.sessionID, thread.ID)
	return nil
}

func (a *KimiACP) fault(threadID string, proc *PersistentProc, sess *kimiACPSession, err error) {
	var parts []string
	if proc != nil {
		if logs := strings.TrimSpace(proc.logText()); logs != "" {
			parts = append(parts, logs)
		}
	}
	if sess != nil {
		sess.mu.Lock()
		rejected := strings.TrimSpace(sess.rejected.String())
		sess.mu.Unlock()
		if rejected != "" {
			parts = append(parts, rejected)
		}
	}
	text := strings.Join(parts, "\n")
	if text == "" && err != nil {
		text = err.Error()
	}
	if text == "" {
		text = "initialize failed"
	}
	_ = setThreadStatus(a.store, threadID, "faulted")
	_ = insertThreadMessage(a.store, threadID, "notice", clip(text, 200))
}

func (s *kimiACPSession) send(method string, params any, done func(*acpRPC, error)) error {
	s.mu.Lock()
	s.nextID++
	id := s.nextID
	if done != nil {
		ch := make(chan *acpRPC, 1)
		s.pending[id] = ch
		go func() {
			waitACPReply(method, ch, done)
		}()
	}
	s.mu.Unlock()
	raw, err := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": id, "method": method, "params": params})
	if err != nil {
		s.failPending(err)
		return err
	}
	if _, err = s.proc.stdin.Write(EncodeACPFrame(raw)); err != nil {
		s.failPending(err)
		return err
	}
	return nil
}

func (s *kimiACPSession) call(method string, params any) (*acpRPC, error) {
	ch := make(chan struct {
		resp *acpRPC
		err  error
	}, 1)
	if err := s.send(method, params, func(resp *acpRPC, err error) {
		ch <- struct {
			resp *acpRPC
			err  error
		}{resp, err}
	}); err != nil {
		return nil, err
	}
	got := <-ch
	return got.resp, got.err
}

func (s *kimiACPSession) failPending(err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for id, ch := range s.pending {
		delete(s.pending, id)
		if ch != nil {
			ch <- &acpRPC{Error: &acpRPCError{Message: err.Error()}}
		}
	}
}

func (s *kimiACPSession) pump(a *KimiACP) {
	defer func() {
		s.failPending(fmt.Errorf("acp closed"))
		a.dropSession(s.threadID, s)
	}()
	for {
		body, err := DecodeACPFrame(s.reader)
		if err != nil {
			if frameErr, ok := err.(*acpFrameError); ok && len(frameErr.Line) > 0 && !strings.HasPrefix(strings.TrimSpace(string(frameErr.Line)), "Content-Length:") {
				s.mu.Lock()
				if s.rejected.Len() > 0 {
					s.rejected.WriteByte('\n')
				}
				s.rejected.Write(frameErr.Line)
				s.mu.Unlock()
			}
			return
		}
		var msg acpRPC
		if json.Unmarshal(body, &msg) != nil {
			continue
		}
		if id, ok := rpcID(msg.ID); ok && (len(msg.Result) > 0 || msg.Error != nil) {
			s.mu.Lock()
			ch := s.pending[id]
			delete(s.pending, id)
			s.mu.Unlock()
			if ch != nil {
				ch <- &msg
			}
			continue
		}
		switch msg.Method {
		case "session/update":
			s.onUpdate(msg.Params)
		case "session/request_permission":
			s.onAsk(a, msg)
		}
	}
}

func (s *kimiACPSession) onUpdate(params json.RawMessage) {
	var payload struct {
		Update struct {
			SessionUpdate string `json:"sessionUpdate"`
			Content       struct {
				Text string `json:"text"`
			} `json:"content"`
		} `json:"update"`
	}
	if json.Unmarshal(params, &payload) != nil {
		return
	}
	if payload.Update.SessionUpdate == "agent_message_chunk" && payload.Update.Content.Text != "" {
		s.mu.Lock()
		s.assist.WriteString(payload.Update.Content.Text)
		s.mu.Unlock()
	}
}

func (s *kimiACPSession) flushAssistant(a *KimiACP) {
	s.mu.Lock()
	text := s.assist.String()
	s.assist.Reset()
	s.mu.Unlock()
	if strings.TrimSpace(text) == "" {
		return
	}
	_ = insertThreadMessage(a.store, s.threadID, "assistant", text)
}

func (s *kimiACPSession) onAsk(a *KimiACP, msg acpRPC) {
	if s.access == "full-access" && msg.Method == "session/request_permission" {
		id := msg.ID
		body, _ := json.Marshal(acpRPC{
			JSONRPC: "2.0",
			ID:      id,
			Result:  json.RawMessage(`{"outcome":{"outcome":"selected","optionId":"allow-once"}}`),
		})
		_, _ = s.proc.stdin.Write(EncodeACPFrame(body))
		return
	}
	callID, rawID := rpcIDString(msg.ID)
	s.mu.Lock()
	s.serverReq[callID] = rawID
	s.mu.Unlock()
	prompt, options := parseACPAsk(msg.Method, msg.Params)
	raw, err := json.Marshal(options)
	if err != nil {
		return
	}
	_ = insertThreadPrompt(a.store, s.threadID, callID, prompt, string(raw))
	_ = setThreadStatus(a.store, s.threadID, "waiting_user")
}
