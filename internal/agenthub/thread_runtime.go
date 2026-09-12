package agenthub

import (
	"errors"

	"github.com/oklog/ulid/v2"
)

var ErrThreadBusy = errors.New("thread is waiting for user")

type ThreadAdapter interface {
	Open(thread ThreadRecord) error
	Prompt(threadID, text string) error
	Respond(threadID, callID, option string) error
	Close(threadID string) error
}

type ThreadPromptOption struct {
	ID    string `json:"id"`
	Label string `json:"label"`
}

func insertThreadMessage(store *ThreadStore, threadID, role, content string) error {
	var last int
	err := store.db.QueryRow(`SELECT COALESCE(MAX(seq), 0) FROM agent_hub_messages WHERE thread_id=?`, threadID).Scan(&last)
	if err != nil {
		return err
	}
	_, err = store.db.Exec(`INSERT INTO agent_hub_messages(id, thread_id, seq, role, content, created_at)
VALUES(?,?,?,?,?,datetime('now'))`, ulid.Make().String(), threadID, last+1, role, content)
	return err
}

func setThreadStatus(store *ThreadStore, threadID, status string) error {
	_, err := store.db.Exec(`UPDATE agent_hub_threads SET status=? WHERE id=?`, status, threadID)
	if err != nil || status != "success" {
		return err
	}
	thread, loadErr := store.Get(threadID)
	if loadErr != nil {
		return nil
	}
	_ = CopyThreadExport(thread.WorkspaceRoot, thread.ExportDir)
	return nil
}

func insertThreadPrompt(store *ThreadStore, threadID, callID, prompt, optionsJSON string) error {
	_, err := store.db.Exec(`INSERT INTO agent_hub_prompts(thread_id, call_id, prompt, options_json, status)
VALUES(?,?,?,?, 'open')`, threadID, callID, prompt, optionsJSON)
	return err
}

func setPromptStatus(store *ThreadStore, threadID, callID, status string) error {
	_, err := store.db.Exec(`UPDATE agent_hub_prompts SET status=? WHERE thread_id=? AND call_id=?`, status, threadID, callID)
	return err
}

func countOpenPrompts(store *ThreadStore, threadID string) (int, error) {
	var n int
	err := store.db.QueryRow(`SELECT COUNT(*) FROM agent_hub_prompts WHERE thread_id=? AND status='open'`, threadID).Scan(&n)
	return n, err
}
