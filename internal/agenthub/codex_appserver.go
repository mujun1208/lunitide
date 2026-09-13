package agenthub

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"
)

var probeCodexAppServer = defaultProbeCodexAppServer

var (
	probeCacheMu     sync.Mutex
	probeCache       = map[string]probeCacheEntry{}
	codexCallTimeout = 20 * time.Second
	probeCacheTTL    = 30 * time.Second
)

type probeCacheEntry struct {
	ok        bool
	expiresAt time.Time
}

func codexAppServerArgv() (string, []string) {
	return "codex", []string{"app-server"}
}

func encodeCodexRequest(id any, method string, params any) ([]byte, error) {
	msg := map[string]any{"method": method}
	if id != nil {
		msg["id"] = id
	}
	if params != nil {
		msg["params"] = params
	}
	raw, err := json.Marshal(msg)
	if err != nil {
		return nil, err
	}
	return EncodeACPFrame(raw), nil
}

func codexInitializeParams() map[string]any {
	return map[string]any{
		"clientInfo":   map[string]any{"name": "lunitide", "title": "月汐", "version": "0.1.0"},
		"capabilities": map[string]any{"experimentalApi": true},
	}
}

func encodeCodexResult(id json.RawMessage, result any) ([]byte, error) {
	raw, err := json.Marshal(struct {
		ID     json.RawMessage `json:"id"`
		Result any             `json:"result"`
	}{ID: id, Result: result})
	if err != nil {
		return nil, err
	}
	return EncodeACPFrame(raw), nil
}

func resolveCodexAppServer(look LookPath) (string, []string, error) {
	if look == nil {
		look = defaultLookPath
	}
	path, err := look("codex")
	if err != nil || strings.TrimSpace(path) == "" {
		if err == nil {
			err = fmt.Errorf("codex 未找到")
		}
		return "", nil, err
	}
	if runtime.GOOS == "windows" && strings.EqualFold(filepath.Ext(path), ".cmd") {
		exe, args, ok := resolveNpmCodex(path)
		if !ok {
			return "", nil, fmt.Errorf("codex 无法解析为 node")
		}
		return exe, args, nil
	}
	return path, []string{"app-server"}, nil
}

func resolveNpmCodex(cmdPath string) (string, []string, bool) {
	dir := filepath.Dir(cmdPath)
	js := filepath.Join(dir, "node_modules", "@openai", "codex", "bin", "codex.js")
	if info, err := os.Stat(js); err != nil || info.IsDir() {
		return "", nil, false
	}
	node := filepath.Join(dir, "node.exe")
	if info, err := os.Stat(node); err != nil || info.IsDir() {
		looked, lookErr := exec.LookPath("node")
		if lookErr != nil {
			return "", nil, false
		}
		node = looked
	}
	if abs, err := filepath.Abs(node); err == nil {
		node = abs
	}
	return node, []string{js, "app-server"}, true
}

func codexAppServerProcSpec(look LookPath, dir string) (ProcSpec, error) {
	exe, args, err := resolveCodexAppServer(look)
	if err != nil {
		return ProcSpec{}, err
	}
	if dir == "" {
		dir = os.TempDir()
	}
	dir, err = filepath.Abs(dir)
	if err != nil {
		return ProcSpec{}, err
	}
	if !filepath.IsAbs(exe) {
		exe, err = filepath.Abs(exe)
		if err != nil {
			return ProcSpec{}, err
		}
	}
	return ProcSpec{Exe: exe, Dir: dir, Args: args}, nil
}

func defaultProbeCodexAppServer(look LookPath) bool {
	if look == nil {
		look = defaultLookPath
	}
	spec, err := codexAppServerProcSpec(look, "")
	if err != nil {
		return false
	}
	key := spec.Exe + "\x00" + strings.Join(spec.Args, "\x00")
	probeCacheMu.Lock()
	if cached, ok := probeCache[key]; ok && time.Now().Before(cached.expiresAt) {
		probeCacheMu.Unlock()
		return cached.ok
	}
	probeCacheMu.Unlock()
	ok := runCodexAppServerProbe(spec, 8*time.Second)
	probeCacheMu.Lock()
	probeCache[key] = probeCacheEntry{ok: ok, expiresAt: time.Now().Add(probeCacheTTL)}
	probeCacheMu.Unlock()
	return ok
}

func runCodexAppServerProbe(spec ProcSpec, timeout time.Duration) bool {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	proc, err := StartPersistent(ctx, spec)
	if err != nil {
		return false
	}
	defer proc.Close()
	frame, err := encodeCodexRequest(1, "initialize", codexInitializeParams())
	if err != nil {
		return false
	}
	if _, err = proc.stdin.Write(frame); err != nil {
		return false
	}
	done := make(chan bool, 1)
	go func() {
		reader := bufio.NewReader(proc.stdout)
		for {
			body, readErr := DecodeACPFrame(reader)
			if readErr != nil {
				done <- false
				return
			}
			var msg acpRPC
			if json.Unmarshal(body, &msg) != nil {
				continue
			}
			id, ok := rpcID(msg.ID)
			if !ok || id != 1 {
				continue
			}
			if msg.Error != nil {
				done <- false
				return
			}
			if notify, nerr := encodeCodexRequest(nil, "initialized", map[string]any{}); nerr == nil {
				_, _ = proc.stdin.Write(notify)
			}
			done <- true
			return
		}
	}()
	select {
	case ok := <-done:
		return ok
	case <-ctx.Done():
		return false
	}
}

func codexAutoAllow(access, method string) bool {
	switch method {
	case "item/fileChange/requestApproval":
		return access == "auto-edit" || access == "full-access"
	case "item/commandExecution/requestApproval", "item/permissions/requestApproval":
		return access == "full-access"
	default:
		return false
	}
}

type codexServerReq struct {
	id     json.RawMessage
	method string
	params json.RawMessage
}

type codexAppSession struct {
	proc      *PersistentProc
	threadID  string
	nativeID  string
	access    string
	nextID    int64
	pending   map[int64]chan *acpRPC
	serverReq map[string]codexServerReq
	reader    *bufio.Reader
	assist    strings.Builder
	ready     chan struct{}
	readyErr  error
	mu        sync.Mutex
}

func (a *CodexThread) liveAppServer(threadID string) *codexAppSession {
	a.mu.Lock()
	sess := a.sessions[threadID]
	a.mu.Unlock()
	if sess == nil {
		return nil
	}
	select {
	case <-sess.ready:
		if sess.readyErr != nil || sess.proc == nil {
			return nil
		}
		return sess
	default:
		return nil
	}
}

func (a *CodexThread) shouldTryAppServer(thread ThreadRecord) bool {
	if strings.TrimSpace(thread.NativeSessionID) != "" {
		return true
	}
	if a.startPersistent != nil {
		return true
	}
	return probeCodexAppServer(a.look)
}

func (a *CodexThread) tryAppServer(thread ThreadRecord) error {
	_, err := a.ensureAppServer(thread)
	return err
}

func (a *CodexThread) ensureAppServer(thread ThreadRecord) (*codexAppSession, error) {
	a.mu.Lock()
	if a.sessions == nil {
		a.sessions = map[string]*codexAppSession{}
	}
	if sess := a.sessions[thread.ID]; sess != nil {
		a.mu.Unlock()
		<-sess.ready
		if sess.readyErr != nil {
			return nil, sess.readyErr
		}
		return sess, nil
	}
	sess := &codexAppSession{
		threadID:  thread.ID,
		access:    thread.AccessMode,
		pending:   map[int64]chan *acpRPC{},
		serverReq: map[string]codexServerReq{},
		ready:     make(chan struct{}),
	}
	a.sessions[thread.ID] = sess
	a.mu.Unlock()
	err := a.handshakeAppServer(thread, sess)
	sess.readyErr = err
	close(sess.ready)
	if err != nil {
		a.dropAppSession(thread.ID, sess)
		if sess.proc != nil {
			_ = sess.proc.Close()
		}
		return nil, err
	}
	return sess, nil
}

func (a *CodexThread) dropAppSession(threadID string, sess *codexAppSession) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.sessions[threadID] == sess {
		delete(a.sessions, threadID)
	}
}

func (a *CodexThread) handshakeAppServer(thread ThreadRecord, sess *codexAppSession) error {
	look := a.look
	if look == nil {
		look = defaultLookPath
	}
	exe, args, err := resolveCodexAppServer(look)
	if err != nil {
		return err
	}
	start := a.startPersistent
	if start == nil {
		if _, statErr := os.Stat(exe); statErr != nil {
			return fmt.Errorf("codex 未找到")
		}
		start = StartPersistent
	}
	proc, err := start(context.Background(), ProcSpec{Exe: exe, Dir: thread.WorkspaceRoot, Args: args})
	if err != nil {
		return err
	}
	sess.proc = proc
	sess.reader = bufio.NewReader(proc.stdout)
	go sess.pump(a)
	if _, err = sess.call("initialize", codexInitializeParams()); err != nil {
		return err
	}
	if err = sess.notify("initialized", map[string]any{}); err != nil {
		return err
	}
	params := map[string]any{
		"cwd":            thread.WorkspaceRoot,
		"approvalPolicy": codexApprovalPolicy(thread.AccessMode),
		"sandbox":        codexSandbox(thread.AccessMode),
	}
	var resp *acpRPC
	usedResume := false
	if thread.NativeSessionID != "" {
		resume := map[string]any{}
		for k, v := range params {
			resume[k] = v
		}
		resume["threadId"] = thread.NativeSessionID
		resp, err = sess.call("thread/resume", resume)
		if err != nil {
			resp, err = sess.call("thread/start", params)
		} else {
			usedResume = true
		}
	} else {
		resp, err = sess.call("thread/start", params)
	}
	if err != nil {
		return err
	}
	var created struct {
		Thread struct {
			ID string `json:"id"`
		} `json:"thread"`
	}
	if resp != nil {
		_ = json.Unmarshal(resp.Result, &created)
	}
	sess.nativeID = created.Thread.ID
	if usedResume {
		sess.nativeID = keepNativeID(sess.nativeID, thread.NativeSessionID)
	}
	if sess.nativeID != "" {
		_, _ = a.store.db.Exec(`UPDATE agent_hub_threads SET native_session_id=? WHERE id=?`, sess.nativeID, thread.ID)
	}
	return nil
}

func codexApprovalPolicy(access string) string {
	if access == "full-access" {
		return "never"
	}
	return "unlessTrusted"
}

func codexSandbox(access string) string {
	if access == "full-access" {
		return "dangerFullAccess"
	}
	return "workspaceWrite"
}

func (a *CodexThread) promptAppServer(sess *codexAppSession, thread ThreadRecord, text string) error {
	var blocks []map[string]any
	if msgs, listErr := a.store.ListMessages(thread.ID); listErr == nil {
		for _, msg := range msgs {
			if msg.Role == "system" && strings.TrimSpace(msg.Content) != "" {
				blocks = append(blocks, map[string]any{"type": "text", "text": msg.Content})
			}
		}
	}
	if err := insertThreadMessage(a.store, thread.ID, "user", text); err != nil {
		return err
	}
	if err := setThreadStatus(a.store, thread.ID, "running"); err != nil {
		return err
	}
	blocks = append(blocks, map[string]any{"type": "text", "text": text})
	params := map[string]any{
		"threadId": sess.nativeID,
		"input":    blocks,
		"cwd":      thread.WorkspaceRoot,
		"sandboxPolicy": map[string]any{
			"type": codexSandbox(thread.AccessMode),
		},
	}
	if err := sess.send("turn/start", params, func(_ *acpRPC, callErr error) {
		if callErr != nil {
			a.faultApp(thread.ID, sess, callErr)
		}
	}); err != nil {
		a.dropAppSession(thread.ID, sess)
		a.faultApp(thread.ID, sess, err)
		return err
	}
	return nil
}

func (a *CodexThread) respondAppServer(sess *codexAppSession, threadID, callID, option, text string) error {
	sess.mu.Lock()
	req := sess.serverReq[callID]
	delete(sess.serverReq, callID)
	sess.mu.Unlock()
	if len(req.id) == 0 {
		req.id, _ = json.Marshal(callID)
	}
	frame, err := encodeCodexResult(req.id, codexRespondResult(req.method, option, text, req.params))
	if err != nil {
		return err
	}
	if err = sess.writeFrame(frame); err != nil {
		a.dropAppSession(threadID, sess)
		return err
	}
	if err = setPromptStatus(a.store, threadID, callID, "answered"); err != nil {
		return err
	}
	return setThreadStatus(a.store, threadID, "running")
}

func (a *CodexThread) faultApp(threadID string, sess *codexAppSession, err error) {
	text := ""
	if sess != nil && sess.proc != nil {
		text = strings.TrimSpace(sess.proc.logText())
	}
	if text == "" && err != nil {
		text = err.Error()
	}
	if text == "" {
		text = "app-server failed"
	}
	_ = setThreadStatus(a.store, threadID, "faulted")
	_ = insertThreadMessage(a.store, threadID, "notice", clip(text, 200))
	if sess != nil {
		a.dropAppSession(threadID, sess)
		if sess.proc != nil {
			_ = sess.proc.Close()
		}
	}
}

func (s *codexAppSession) send(method string, params any, done func(*acpRPC, error)) error {
	s.mu.Lock()
	s.nextID++
	id := s.nextID
	if done != nil {
		ch := make(chan *acpRPC, 1)
		s.pending[id] = ch
		go waitCodexReply(method, ch, done)
	}
	s.mu.Unlock()
	frame, err := encodeCodexRequest(id, method, params)
	if err != nil {
		s.failPending(err)
		return err
	}
	if err = s.writeFrame(frame); err != nil {
		s.failPending(err)
		return err
	}
	return nil
}

func (s *codexAppSession) writeFrame(frame []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.proc == nil || s.proc.stdin == nil {
		return fmt.Errorf("app-server closed")
	}
	_, err := s.proc.stdin.Write(frame)
	return err
}

func (s *codexAppSession) notify(method string, params any) error {
	frame, err := encodeCodexRequest(nil, method, params)
	if err != nil {
		return err
	}
	return s.writeFrame(frame)
}

func (s *codexAppSession) call(method string, params any) (*acpRPC, error) {
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

func waitCodexReply(method string, ch <-chan *acpRPC, done func(*acpRPC, error)) {
	finish := func(resp *acpRPC) {
		if resp.Error != nil {
			done(resp, fmt.Errorf("%s", resp.Error.Message))
			return
		}
		done(resp, nil)
	}
	switch method {
	case "initialize", "thread/start", "thread/resume", "turn/start":
		timer := time.NewTimer(codexCallTimeout)
		defer timer.Stop()
		select {
		case resp := <-ch:
			finish(resp)
		case <-timer.C:
			done(nil, fmt.Errorf("app-server timeout %s", method))
		}
	default:
		finish(<-ch)
	}
}

func (s *codexAppSession) failPending(err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for id, ch := range s.pending {
		delete(s.pending, id)
		if ch != nil {
			ch <- &acpRPC{Error: &acpRPCError{Message: err.Error()}}
		}
	}
}

func (s *codexAppSession) pump(a *CodexThread) {
	defer func() {
		s.failPending(fmt.Errorf("app-server closed"))
		a.dropAppSession(s.threadID, s)
	}()
	for {
		body, err := DecodeACPFrame(s.reader)
		if err != nil {
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
		if msg.Method != "" && len(msg.ID) > 0 {
			s.onAsk(a, msg)
			continue
		}
		switch msg.Method {
		case "item/agentMessage/delta":
			s.appendDelta(msg.Params)
		case "item/completed":
			s.onItemCompleted(msg.Params)
		case "thread/tokenUsage/updated":
			if n := extractAppServerTokens(msg.Params); n > 0 {
				_ = insertThreadEvent(a.store, s.threadID, AgentEvent{Type: "usage", Tokens: n})
			}
		case "turn/completed", "error":
			s.onTurnDone(a, msg)
		}
	}
}

func (s *codexAppSession) appendDelta(params json.RawMessage) {
	text := codexDeltaText(params)
	if text == "" {
		return
	}
	s.mu.Lock()
	s.assist.WriteString(text)
	s.mu.Unlock()
}

func codexDeltaText(params json.RawMessage) string {
	var payload struct {
		Delta json.RawMessage `json:"delta"`
		Text  string          `json:"text"`
	}
	if json.Unmarshal(params, &payload) != nil {
		return ""
	}
	if payload.Text != "" {
		return payload.Text
	}
	var plain string
	if json.Unmarshal(payload.Delta, &plain) == nil {
		return plain
	}
	var obj struct {
		Text string `json:"text"`
	}
	_ = json.Unmarshal(payload.Delta, &obj)
	return obj.Text
}

func (s *codexAppSession) onItemCompleted(params json.RawMessage) {
	var payload struct {
		Item struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"item"`
	}
	if json.Unmarshal(params, &payload) != nil {
		return
	}
	if payload.Item.Type != "agentMessage" || strings.TrimSpace(payload.Item.Text) == "" {
		return
	}
	s.mu.Lock()
	if s.assist.Len() == 0 {
		s.assist.WriteString(payload.Item.Text)
	}
	s.mu.Unlock()
}

func (s *codexAppSession) flushAssistant(a *CodexThread) {
	s.mu.Lock()
	text := s.assist.String()
	s.assist.Reset()
	s.mu.Unlock()
	if strings.TrimSpace(text) == "" {
		return
	}
	_ = insertThreadMessage(a.store, s.threadID, "assistant", text)
}

func (s *codexAppSession) onTurnDone(a *CodexThread, msg acpRPC) {
	s.flushAssistant(a)
	if thread, err := a.store.Get(s.threadID); err == nil && ignoreTurnSuccess(thread.Status) {
		return
	}
	var payload struct {
		Turn struct {
			Status string `json:"status"`
			Error  struct {
				Message string `json:"message"`
			} `json:"error"`
		} `json:"turn"`
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	_ = json.Unmarshal(msg.Params, &payload)
	status := payload.Turn.Status
	if msg.Method == "error" || status == "failed" || status == "interrupted" {
		errText := payload.Turn.Error.Message
		if errText == "" {
			errText = payload.Error.Message
		}
		if errText == "" {
			errText = status
		}
		a.faultApp(s.threadID, s, fmt.Errorf("%s", errText))
		return
	}
	_ = setThreadStatus(a.store, s.threadID, "success")
}

func (s *codexAppSession) onAsk(a *CodexThread, msg acpRPC) {
	if codexAutoAllow(s.access, msg.Method) {
		frame, err := encodeCodexResult(msg.ID, codexRespondResult(msg.Method, "accept", "", msg.Params))
		if err == nil {
			go func() { _ = s.writeFrame(frame) }()
		}
		return
	}
	callID, rawID := rpcIDString(msg.ID)
	s.mu.Lock()
	s.serverReq[callID] = codexServerReq{id: rawID, method: msg.Method, params: msg.Params}
	s.mu.Unlock()
	prompt, options := parseCodexAsk(msg.Method, msg.Params)
	raw, err := json.Marshal(options)
	if err != nil {
		return
	}
	_ = insertThreadPrompt(a.store, s.threadID, callID, prompt, string(raw))
	_ = setThreadStatus(a.store, s.threadID, "waiting_user")
}

func parseCodexAsk(method string, params json.RawMessage) (string, []ThreadPromptOption) {
	if method == "item/tool/requestUserInput" {
		var payload struct {
			Questions []struct {
				ID       string `json:"id"`
				Header   string `json:"header"`
				Question string `json:"question"`
				Options  []struct {
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
				label := opt.Label
				if label == "" {
					label = opt.ID
				}
				out = append(out, ThreadPromptOption{ID: opt.ID, Label: label})
			}
			prompt := q.Question
			if prompt == "" {
				prompt = q.Header
			}
			if prompt == "" {
				prompt = "选择"
			}
			return clip(prompt, 4000), out
		}
	}
	var payload struct {
		Reason  string `json:"reason"`
		Command string `json:"command"`
	}
	_ = json.Unmarshal(params, &payload)
	prompt := payload.Reason
	if prompt == "" {
		prompt = payload.Command
	}
	if prompt == "" {
		switch {
		case strings.Contains(method, "fileChange"):
			prompt = "允许改文件？"
		case strings.Contains(method, "command"):
			prompt = "允许执行命令？"
		default:
			prompt = "选择"
		}
	}
	return clip(prompt, 4000), []ThreadPromptOption{{ID: "accept", Label: "允许"}, {ID: "decline", Label: "拒绝"}}
}

func codexRespondResult(method, option, text string, params json.RawMessage) any {
	switch method {
	case "item/tool/requestUserInput":
		return codexUserInputResult(option, text, params)
	case "item/permissions/requestApproval":
		if option == "decline" || option == "cancel" {
			return map[string]any{"permissions": map[string]any{}, "scope": "turn"}
		}
		var payload struct {
			Permissions any `json:"permissions"`
		}
		_ = json.Unmarshal(params, &payload)
		if payload.Permissions == nil {
			payload.Permissions = map[string]any{}
		}
		return map[string]any{"permissions": payload.Permissions, "scope": "session"}
	default:
		decision := "accept"
		if option == "decline" || option == "cancel" {
			decision = option
		}
		return map[string]any{"decision": decision}
	}
}

func codexUserInputResult(option, text string, params json.RawMessage) any {
	var payload struct {
		Questions []struct {
			ID string `json:"id"`
		} `json:"questions"`
	}
	_ = json.Unmarshal(params, &payload)
	qid := "q1"
	if len(payload.Questions) > 0 && payload.Questions[0].ID != "" {
		qid = payload.Questions[0].ID
	}
	ans := map[string]any{"questionId": qid}
	if option != "" {
		ans["selectedOptionIds"] = []string{option}
	}
	if strings.TrimSpace(text) != "" {
		ans["text"] = strings.TrimSpace(text)
	}
	return map[string]any{"answers": []any{ans}}
}

func extractAppServerTokens(params json.RawMessage) int64 {
	var payload struct {
		TotalTokens int64 `json:"totalTokens"`
		TokenUsage  struct {
			Total struct {
				TotalTokens int64 `json:"totalTokens"`
			} `json:"total"`
		} `json:"tokenUsage"`
	}
	_ = json.Unmarshal(params, &payload)
	if payload.TotalTokens > 0 {
		return payload.TotalTokens
	}
	return payload.TokenUsage.Total.TotalTokens
}
