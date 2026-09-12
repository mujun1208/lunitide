package agenthub

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

var _ ThreadAdapter = (*CodexThread)(nil)

const codexAvailableHint = "当前只能一把跑完，不能中途提问"

type CodexThread struct {
	store *ThreadStore
	look  LookPath
	start StartFunc
}

func NewCodexThread(store *ThreadStore) *CodexThread {
	return &CodexThread{
		store: store,
		look:  defaultLookPath,
		start: StartProcess,
	}
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

func (a *CodexThread) Open(ThreadRecord) error { return nil }

func (a *CodexThread) Close(string) error { return nil }

func (a *CodexThread) Respond(string, string, string) error {
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
	stdin := composeCodexExecPrompt(a.store, threadID, text)
	if err = insertThreadMessage(a.store, threadID, "user", text); err != nil {
		return err
	}
	if err = setThreadStatus(a.store, threadID, "running"); err != nil {
		return err
	}
	exe, args := codexThreadArgv(thread.WorkspaceRoot, "")
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
	var assistant strings.Builder
	exit, timedOut, runErr := start(context.Background(), ProcSpec{
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
	_, err = store.db.Exec(`INSERT INTO agent_hub_thread_events(thread_id, seq, type, title, detail, payload_json, ts)
VALUES(?,?,?,?,?,?,?)`, threadID, last+1, ev.Type, title, ev.Detail, "{}", ts)
	return err
}
