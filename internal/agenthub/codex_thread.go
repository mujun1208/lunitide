package agenthub

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

var _ ThreadAdapter = (*CodexThread)(nil)

const (
	codexAvailableHint      = "当前只能一把跑完，不能中途提问"
	codexExecFallbackNotice = "app-server 没接上，这轮改为一把跑完，不能中途提问"
)

var (
	codexThreadMu sync.Mutex
	codexThreads  = map[*ThreadStore]*CodexThread{}
)

type CodexThread struct {
	store           *ThreadStore
	look            LookPath
	start           StartFunc
	startPersistent persistentStarter
	mu              sync.Mutex
	stops           map[string]context.CancelFunc
	sessions        map[string]*codexAppSession
}

func NewCodexThread(store *ThreadStore) *CodexThread {
	codexThreadMu.Lock()
	defer codexThreadMu.Unlock()
	if a, ok := codexThreads[store]; ok {
		return a
	}
	a := &CodexThread{
		store:    store,
		look:     defaultLookPath,
		start:    StartProcess,
		stops:    map[string]context.CancelFunc{},
		sessions: map[string]*codexAppSession{},
	}
	codexThreads[store] = a
	return a
}

func codexExecSandbox(access string) string {
	if access == "full-access" {
		return "danger-full-access"
	}
	return "workspace-write"
}

func codexThreadArgv(workspace, sandbox string) (string, []string) {
	if sandbox == "" {
		sandbox = "workspace-write"
	}
	return "codex", []string{
		"exec", "--json", "--skip-git-repo-check",
		"--sandbox", sandbox,
		"--cd", workspace,
		"-o", filepath.Join(workspace, "codex-last-message.md"),
	}
}

func (a *CodexThread) Open(thread ThreadRecord) error {
	return nil
}

func (a *CodexThread) Close(threadID string) error {
	a.mu.Lock()
	cancel := a.stops[threadID]
	delete(a.stops, threadID)
	sess := a.sessions[threadID]
	delete(a.sessions, threadID)
	a.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	if sess == nil {
		return nil
	}
	select {
	case <-sess.ready:
	default:
	}
	if sess.proc != nil {
		return sess.proc.Close()
	}
	return nil
}

func (a *CodexThread) Respond(threadID, callID, option, text string) error {
	if sess := a.liveAppServer(threadID); sess != nil {
		return a.respondAppServer(sess, threadID, callID, option, text)
	}
	return fmt.Errorf("%s", codexAvailableHint)
}

func (a *CodexThread) Prompt(threadID, text string) error {
	thread, err := a.store.Get(threadID)
	if err != nil {
		return err
	}
	if thread.Status == "waiting_user" || thread.Status == "running" {
		return ErrThreadBusy
	}
	if sess := a.liveAppServer(threadID); sess == nil {
		if a.shouldTryAppServer(thread) {
			if openErr := a.tryAppServer(thread); openErr == nil {
				sess = a.liveAppServer(threadID)
			}
			if sess != nil {
				return a.promptAppServer(sess, thread, text)
			}
			_ = insertThreadMessage(a.store, threadID, "notice", codexExecFallbackNotice)
		}
	} else {
		return a.promptAppServer(sess, thread, text)
	}
	stdin := composeCodexExecPrompt(a.store, threadID, text)
	if err = insertThreadMessage(a.store, threadID, "user", text); err != nil {
		return err
	}
	if err = setThreadStatus(a.store, threadID, "running"); err != nil {
		return err
	}
	exe, args := codexThreadArgv(thread.WorkspaceRoot, codexExecSandbox(thread.AccessMode))
	look := a.look
	if look == nil {
		look = defaultLookPath
	}
	looked, lookErr := look(exe)
	if lookErr != nil || strings.TrimSpace(looked) == "" {
		if lookErr == nil {
			lookErr = fmt.Errorf("codex 未找到")
		}
		a.fault(threadID, lookErr)
		return nil
	}
	start := a.start
	if start == nil {
		start = StartProcess
	}
	ctx, cancel := context.WithCancel(context.Background())
	a.mu.Lock()
	if a.stops == nil {
		a.stops = map[string]context.CancelFunc{}
	}
	a.stops[threadID] = cancel
	a.mu.Unlock()
	defer func() {
		a.mu.Lock()
		delete(a.stops, threadID)
		a.mu.Unlock()
		cancel()
	}()
	var assistant strings.Builder
	exit, timedOut, runErr := start(ctx, ProcSpec{
		Exe:   looked,
		Dir:   thread.WorkspaceRoot,
		Args:  args,
		Stdin: []byte(stdin),
	}, func(line string) {
		ev, ok := ParseLine("codex", line)
		if !ok {
			return
		}
		_ = insertThreadEvent(a.store, threadID, ev)
		if ev.Detail != "" && (ev.Type == "message" || ev.Type == "step") {
			if assistant.Len() > 0 {
				assistant.WriteByte('\n')
			}
			assistant.WriteString(ev.Detail)
		}
	})
	if ctx.Err() != nil {
		return nil
	}
	out := strings.TrimSpace(assistant.String())
	if out == "" {
		if body, readErr := os.ReadFile(filepath.Join(thread.WorkspaceRoot, "codex-last-message.md")); readErr == nil {
			out = strings.TrimSpace(string(body))
		}
	}
	if timedOut || runErr != nil || exit != 0 {
		if runErr == nil {
			if timedOut {
				runErr = fmt.Errorf("任务超时")
			} else {
				runErr = fmt.Errorf("退出码 %d", exit)
			}
		}
		if out != "" {
			_ = insertThreadMessage(a.store, threadID, "assistant", out)
		}
		a.fault(threadID, runErr)
		return nil
	}
	if out != "" {
		if err = insertThreadMessage(a.store, threadID, "assistant", out); err != nil {
			return err
		}
	}
	return setThreadStatus(a.store, threadID, "success")
}

func composeCodexExecPrompt(store *ThreadStore, threadID, userText string) string {
	var b strings.Builder
	if msgs, listErr := store.ListMessages(threadID); listErr == nil {
		for _, msg := range msgs {
			if msg.Role != "system" || strings.TrimSpace(msg.Content) == "" {
				continue
			}
			if b.Len() > 0 {
				b.WriteByte('\n')
			}
			b.WriteString(msg.Content)
		}
	}
	if userText != "" {
		if b.Len() > 0 {
			b.WriteByte('\n')
		}
		b.WriteString(userText)
	}
	return b.String()
}

func (a *CodexThread) fault(threadID string, err error) {
	_ = setThreadStatus(a.store, threadID, "faulted")
	text := ""
	if err != nil {
		text = err.Error()
	}
	if text != "" {
		_ = insertThreadMessage(a.store, threadID, "notice", clip(text, 200))
	}
}

func insertThreadEvent(store *ThreadStore, threadID string, ev AgentEvent) error {
	var last int
	err := store.db.QueryRow(`SELECT COALESCE(MAX(seq), 0) FROM agent_hub_thread_events WHERE thread_id=?`, threadID).Scan(&last)
	if err != nil {
		return err
	}
	ts := ev.TS
	if ts == "" {
		ts = time.Now().UTC().Format(time.RFC3339)
	}
	title := ev.Title
	if title == "" {
		title = ev.Type
	}
	payload := "{}"
	if ev.Tokens > 0 {
		payload = fmt.Sprintf(`{"tokens":%d}`, ev.Tokens)
	}
	_, err = store.db.Exec(`INSERT INTO agent_hub_thread_events(thread_id, seq, type, title, detail, payload_json, ts)
VALUES(?,?,?,?,?,?,?)`, threadID, last+1, ev.Type, title, ev.Detail, payload, ts)
	return err
}
