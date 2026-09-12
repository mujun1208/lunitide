package agenthub

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"
)

func resolveCursorACP(look LookPath) (string, []string, error) {
	if look == nil {
		look = defaultLookPath
	}
	path, err := look(exeName("cursor"))
	if err != nil || strings.TrimSpace(path) == "" {
		if err == nil {
			err = fmt.Errorf("cursor-agent 未找到")
		}
		return "", nil, err
	}
	if runtime.GOOS == "windows" && strings.EqualFold(filepath.Ext(path), ".cmd") {
		exe, args, ok := resolveCursorNodeACP(path)
		if !ok {
			return "", nil, fmt.Errorf("cursor-agent 无法解析为 node")
		}
		return exe, args, nil
	}
	return path, []string{"acp"}, nil
}

func resolveCursorNodeACP(cmdPath string) (string, []string, bool) {
	dir := filepath.Dir(cmdPath)
	if node, js, ok := cursorNodePair(dir); ok {
		return node, []string{js, "acp"}, true
	}
	entries, err := os.ReadDir(filepath.Join(dir, "versions"))
	if err != nil {
		return "", nil, false
	}
	best := ""
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		if _, _, ok := cursorNodePair(filepath.Join(dir, "versions", entry.Name())); ok && entry.Name() >= best {
			best = entry.Name()
		}
	}
	if best == "" {
		return "", nil, false
	}
	node, js, _ := cursorNodePair(filepath.Join(dir, "versions", best))
	return node, []string{js, "acp"}, true
}

func cursorNodePair(dir string) (string, string, bool) {
	node := filepath.Join(dir, "node.exe")
	js := filepath.Join(dir, "index.js")
	info, err := os.Stat(node)
	if err != nil || info.IsDir() {
		return "", "", false
	}
	info, err = os.Stat(js)
	if err != nil || info.IsDir() {
		return "", "", false
	}
	return node, js, true
}

var (
	cursorACPMu sync.Mutex
	cursorACPs  = map[*ThreadStore]*CursorACP{}
)

var _ ThreadAdapter = (*CursorACP)(nil)

type persistentStarter func(context.Context, ProcSpec) (*PersistentProc, error)

type CursorACP struct {
	store           *ThreadStore
	look            LookPath
	startPersistent persistentStarter

	mu       sync.Mutex
	sessions map[string]*cursorACPSession
}

type cursorACPSession struct {
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

type acpRPC struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *acpRPCError    `json:"error,omitempty"`
}

type acpRPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func NewCursorACP(store *ThreadStore) *CursorACP {
	cursorACPMu.Lock()
	defer cursorACPMu.Unlock()
	if a, ok := cursorACPs[store]; ok {
		return a
	}
	a := &CursorACP{
		store:           store,
		look:            defaultLookPath,
		startPersistent: StartPersistent,
		sessions:        map[string]*cursorACPSession{},
	}
	cursorACPs[store] = a
	return a
}

func (a *CursorACP) Open(thread ThreadRecord) error {
	_, err := a.ensure(thread)
	return err
}

func (a *CursorACP) Close(threadID string) error {
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

func (a *CursorACP) Prompt(threadID, text string) error {
	thread, err := a.store.Get(threadID)
	if err != nil {
		return err
	}
	if thread.Status == "waiting_user" {
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
		a.fault(threadID, sess.proc, sess, err)
		return err
	}
	return nil
}

func (a *CursorACP) Respond(threadID, callID, option string) error {
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
		return err
	}
	if err = setPromptStatus(a.store, threadID, callID, "answered"); err != nil {
		return err
	}
	return setThreadStatus(a.store, threadID, "running")
}

func (a *CursorACP) ensure(thread ThreadRecord) (*cursorACPSession, error) {
	a.mu.Lock()
	if sess := a.sessions[thread.ID]; sess != nil {
		a.mu.Unlock()
		<-sess.ready
		if sess.readyErr != nil {
			return nil, sess.readyErr
		}
		return sess, nil
	}
	sess := &cursorACPSession{
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

func (a *CursorACP) handshake(thread ThreadRecord, sess *cursorACPSession) error {
	exe, args, err := resolveCursorACP(a.look)
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

func (a *CursorACP) fault(threadID string, proc *PersistentProc, sess *cursorACPSession, err error) {
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

func (s *cursorACPSession) send(method string, params any, done func(*acpRPC, error)) error {
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

func (s *cursorACPSession) call(method string, params any) (*acpRPC, error) {
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

func (s *cursorACPSession) failPending(err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for id, ch := range s.pending {
		delete(s.pending, id)
		if ch != nil {
			ch <- &acpRPC{Error: &acpRPCError{Message: err.Error()}}
		}
	}
}

func waitACPReply(method string, ch <-chan *acpRPC, done func(*acpRPC, error)) {
	finish := func(resp *acpRPC) {
		if resp.Error != nil {
			done(resp, fmt.Errorf("%s", resp.Error.Message))
			return
		}
		done(resp, nil)
	}
	switch method {
	case "initialize", "session/new":
		timer := time.NewTimer(20 * time.Second)
		defer timer.Stop()
		select {
		case resp := <-ch:
			finish(resp)
		case <-timer.C:
			done(nil, fmt.Errorf("acp timeout %s", method))
		}
	default:
		finish(<-ch)
	}
}

func (s *cursorACPSession) pump(a *CursorACP) {
	defer s.failPending(fmt.Errorf("acp closed"))
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
		case "session/request_permission", "cursor/ask_question":
			s.onAsk(a, msg)
		}
	}
}

func (s *cursorACPSession) onUpdate(params json.RawMessage) {
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

func (s *cursorACPSession) flushAssistant(a *CursorACP) {
	s.mu.Lock()
	text := s.assist.String()
	s.assist.Reset()
	s.mu.Unlock()
	if strings.TrimSpace(text) == "" {
		return
	}
	_ = insertThreadMessage(a.store, s.threadID, "assistant", text)
}

func (s *cursorACPSession) onAsk(a *CursorACP, msg acpRPC) {
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

func parseACPAsk(method string, params json.RawMessage) (string, []ThreadPromptOption) {
	if method == "cursor/ask_question" {
		var payload struct {
			Title     string `json:"title"`
			Questions []struct {
				Prompt  string `json:"prompt"`
				Options []struct {
					ID    string `json:"id"`
					Label string `json:"label"`
				} `json:"options"`
			} `json:"questions"`
		}
		_ = json.Unmarshal(params, &payload)
		if len(payload.Questions) > 0 {
			q := payload.Questions[0]
			out := make([]ThreadPromptOption, 0, len(q.Options))
			for _, opt := range q.Options {
				out = append(out, ThreadPromptOption{ID: opt.ID, Label: opt.Label})
			}
			prompt := q.Prompt
			if prompt == "" {
				prompt = payload.Title
			}
			if prompt == "" {
				prompt = "选择"
			}
			return clip(prompt, 4000), out
		}
	}
	var payload struct {
		Options []struct {
			OptionID string `json:"optionId"`
			Name     string `json:"name"`
		} `json:"options"`
		ToolCall struct {
			Title string `json:"title"`
		} `json:"toolCall"`
	}
	_ = json.Unmarshal(params, &payload)
	out := make([]ThreadPromptOption, 0, len(payload.Options))
	for _, opt := range payload.Options {
		label := opt.Name
		if label == "" {
			label = opt.OptionID
		}
		out = append(out, ThreadPromptOption{ID: opt.OptionID, Label: label})
	}
	prompt := payload.ToolCall.Title
	if prompt == "" {
		prompt = "选择"
	}
	return clip(prompt, 4000), out
}

func rpcID(raw json.RawMessage) (int64, bool) {
	if len(raw) == 0 {
		return 0, false
	}
	var n int64
	if json.Unmarshal(raw, &n) == nil {
		return n, true
	}
	return 0, false
}

func rpcIDString(raw json.RawMessage) (string, json.RawMessage) {
	if n, ok := rpcID(raw); ok {
		return strconv.FormatInt(n, 10), raw
	}
	var s string
	if json.Unmarshal(raw, &s) == nil && s != "" {
		return s, raw
	}
	return string(raw), raw
}
