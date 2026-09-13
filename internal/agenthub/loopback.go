package agenthub

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	"github.com/oklog/ulid/v2"
)

var _ ThreadAdapter = (*LoopbackAdapter)(nil)

type LoopbackAdapter struct {
	store *ThreadStore
}

func NewLoopbackAdapter(store *ThreadStore) *LoopbackAdapter {
	return &LoopbackAdapter{store: store}
}

func (a *LoopbackAdapter) Open(ThreadRecord) error { return nil }

func (a *LoopbackAdapter) Close(string) error { return nil }

func (a *LoopbackAdapter) Prompt(threadID, text string) error {
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
	if err := insertThreadMessage(a.store, threadID, "user", text); err != nil {
		return err
	}
	if !loopbackAsks(text) {
		return nil
	}
	options, err := json.Marshal([]ThreadPromptOption{
		{ID: "是", Label: "是"},
		{ID: "否", Label: "否"},
	})
	if err != nil {
		return err
	}
	if err = insertThreadPrompt(a.store, threadID, ulid.Make().String(), text, string(options)); err != nil {
		return err
	}
	return setThreadStatus(a.store, threadID, "waiting_user")
}

func (a *LoopbackAdapter) Respond(threadID, callID, option, _ string) error {
	thread, err := a.store.Get(threadID)
	if err != nil {
		return err
	}
	dest := filepath.Join(thread.WorkspaceRoot, "loopback.txt")
	if !PathAllowed(thread.WorkspaceRoot, thread.ExportDir, dest) {
		return ErrPathOutside
	}
	if err = os.WriteFile(dest, []byte(option), 0o644); err != nil {
		return err
	}
	if err = insertThreadMessage(a.store, threadID, "assistant", option); err != nil {
		return err
	}
	if err = setPromptStatus(a.store, threadID, callID, "answered"); err != nil {
		return err
	}
	return setThreadStatus(a.store, threadID, "success")
}

func loopbackAsks(text string) bool {
	return strings.Contains(text, "?") || strings.Contains(text, "选择")
}
