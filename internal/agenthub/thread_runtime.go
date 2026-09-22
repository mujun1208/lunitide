package agenthub

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/oklog/ulid/v2"
)

var ErrThreadBusy = errors.New("thread is waiting for user")

func ignoreTurnSuccess(status string) bool {
	switch status {
	case "waiting_user", "cancelled", "faulted":
		return true
	default:
		return false
	}
}

type ThreadAdapter interface {
	Open(thread ThreadRecord) error
	Prompt(threadID, text string) error
	Respond(threadID, callID, option, text string) error
	Close(threadID string) error
}

type ThreadPromptOption struct {
	ID    string `json:"id"`
	Label string `json:"label"`
}

var threadWriteMu sync.Mutex

func insertThreadMessage(store *ThreadStore, threadID, role, content string) error {
	threadWriteMu.Lock()
	defer threadWriteMu.Unlock()
	return insertThreadMessageLocked(store, threadID, role, content)
}

// claimUserTurn stores the user line and marks the thread running as one
// critical section. Two sends can no longer read the same seq, and the second
// send is refused while the first is still running.
func claimUserTurn(store *ThreadStore, threadID, text string) error {
	threadWriteMu.Lock()
	defer threadWriteMu.Unlock()
	thread, err := store.Get(threadID)
	if err != nil {
		return err
	}
	if thread.Status == "waiting_user" || thread.Status == "running" {
		return ErrThreadBusy
	}
	if err = insertThreadMessageLocked(store, threadID, "user", text); err != nil {
		return err
	}
	return setThreadStatus(store, threadID, "running")
}

func insertThreadMessageLocked(store *ThreadStore, threadID, role, content string) error {
	res, err := store.db.Exec(`INSERT INTO agent_hub_messages(id, thread_id, seq, role, content, created_at)
VALUES(?, ?, (SELECT COALESCE(MAX(seq), 0) + 1 FROM agent_hub_messages WHERE thread_id = ?), ?, ?, datetime('now'))`,
		ulid.Make().String(), threadID, threadID, role, content)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return errors.New("message was not stored")
	}
	return nil
}

func MarkThreadSuccess(store *ThreadStore, threadID string) error {
	return setThreadStatus(store, threadID, "success")
}

func setThreadStatus(store *ThreadStore, threadID, status string) error {
	_, err := store.db.Exec(`UPDATE agent_hub_threads SET status=?, updated_at=? WHERE id=?`, status, threadNow(), threadID)
	if err != nil || status != "success" {
		return err
	}
	thread, loadErr := store.Get(threadID)
	if loadErr != nil {
		return nil
	}
	if copyErr := CopyThreadExport(thread.WorkspaceRoot, thread.ExportDir); copyErr != nil {
		_ = insertThreadEvent(store, threadID, AgentEvent{Type: "export_error", Title: "export_failed", Detail: copyErr.Error()})
	}
	verifyThreadExports(store, thread)
	persistThreadFiles(store, thread)
	return nil
}

func persistThreadFiles(store *ThreadStore, thread ThreadRecord) {
	_ = filepath.WalkDir(thread.WorkspaceRoot, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d == nil {
			return nil
		}
		if d.IsDir() {
			if path != thread.WorkspaceRoot && (skipScanDir(d.Name()) || d.Type()&os.ModeSymlink != 0) {
				return fs.SkipDir
			}
			return nil
		}
		if d.Type()&os.ModeSymlink != 0 {
			return nil
		}
		info, infoErr := d.Info()
		if infoErr != nil || !info.Mode().IsRegular() {
			return nil
		}
		if !PathAllowed(thread.WorkspaceRoot, thread.ExportDir, path) {
			return nil
		}
		_ = store.UpsertFile(thread.ID, slashRel(thread.WorkspaceRoot, path), path, info.Size(), "scan")
		return nil
	})
	if thread.ExportDir == "" {
		return
	}
	entries, err := os.ReadDir(thread.ExportDir)
	if err != nil {
		return
	}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		abs := filepath.Join(thread.ExportDir, entry.Name())
		if !PathAllowed(thread.WorkspaceRoot, thread.ExportDir, abs) {
			continue
		}
		info, infoErr := entry.Info()
		if infoErr != nil {
			continue
		}
		_ = store.UpsertFile(thread.ID, filepath.ToSlash(entry.Name()), abs, info.Size(), "export")
	}
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

func touchThread(store *ThreadStore, threadID string) error {
	_, err := store.db.Exec(`UPDATE agent_hub_threads SET updated_at=? WHERE id=?`, threadNow(), threadID)
	return err
}

func applyFirstUserTitle(store *ThreadStore, threadID, text string) error {
	thread, err := store.Get(threadID)
	if err != nil {
		return err
	}
	if thread.Title != "" && thread.Title != "新会话" {
		return nil
	}
	title := titleFromPrompt(text)
	if title == "" {
		return nil
	}
	return store.Update(threadID, title, thread.Pinned)
}

func titleFromPrompt(text string) string {
	text = strings.TrimSpace(text)
	if i := strings.IndexAny(text, "\r\n"); i >= 0 {
		text = strings.TrimSpace(text[:i])
	}
	if text == "" {
		return ""
	}
	if utf8.RuneCountInString(text) <= 200 {
		return text
	}
	return string([]rune(text)[:200])
}

func threadNow() string {
	return time.Now().UTC().Format(time.RFC3339)
}
