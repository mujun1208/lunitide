package agenthub

import (
	"database/sql"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestLoopbackTwoStepWithoutCLI(t *testing.T) {
	db := openThreadDB(t)
	store := NewThreadStore(db)
	workspace := t.TempDir()
	thread := sampleThread("01ARZ3NDEKTSV4RRFFQ69G5FAE", "loopback", "Loopback", false)
	thread.WorkspaceRoot = workspace
	if err := store.Insert(thread); err != nil {
		t.Fatal(err)
	}

	adapter := NewLoopbackAdapter(store)
	if err := adapter.Open(thread); err != nil {
		t.Fatal(err)
	}
	if err := adapter.Prompt(thread.ID, "选哪个?"); err != nil {
		t.Fatal(err)
	}

	got, err := store.Get(thread.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != "waiting_user" {
		t.Fatalf("status after prompt = %q, want waiting_user", got.Status)
	}

	role, content := loadLastMessage(t, db, thread.ID)
	if role != "user" || content != "选哪个?" {
		t.Fatalf("user message = %s %q", role, content)
	}

	callID, promptText, optionsJSON, promptStatus := loadOnlyPrompt(t, db, thread.ID)
	if promptStatus != "open" || promptText != "选哪个?" {
		t.Fatalf("prompt = status %q text %q", promptStatus, promptText)
	}
	assertYesNoOptions(t, optionsJSON)

	if err = adapter.Respond(thread.ID, callID, "是"); err != nil {
		t.Fatal(err)
	}

	got, err = store.Get(thread.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != "success" {
		t.Fatalf("status after respond = %q, want success", got.Status)
	}

	_, promptText, optionsJSON, promptStatus = loadOnlyPrompt(t, db, thread.ID)
	if promptStatus != "answered" {
		t.Fatalf("prompt status after respond = %q, want answered", promptStatus)
	}
	assertYesNoOptions(t, optionsJSON)
	if promptText != "选哪个?" {
		t.Fatalf("prompt text changed: %q", promptText)
	}

	role, content = loadLastMessage(t, db, thread.ID)
	if role != "assistant" || content == "" {
		t.Fatalf("assistant message = %s %q", role, content)
	}

	body, err := os.ReadFile(filepath.Join(workspace, "loopback.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(body) == "" {
		t.Fatal("loopback.txt is empty")
	}
}

func TestLoopbackSecondPromptWhileWaitingErrors(t *testing.T) {
	db := openThreadDB(t)
	store := NewThreadStore(db)
	thread := sampleThread("01ARZ3NDEKTSV4RRFFQ69G5FAE", "loopback", "Loopback", false)
	thread.WorkspaceRoot = t.TempDir()
	if err := store.Insert(thread); err != nil {
		t.Fatal(err)
	}
	adapter := NewLoopbackAdapter(store)
	if err := adapter.Prompt(thread.ID, "请选择"); err != nil {
		t.Fatal(err)
	}

	err := adapter.Prompt(thread.ID, "再问一次?")
	if err == nil {
		t.Fatal("second prompt while waiting_user must error")
	}

	got, getErr := store.Get(thread.ID)
	if getErr != nil {
		t.Fatal(getErr)
	}
	if got.Status != "waiting_user" {
		t.Fatalf("status after rejected prompt = %q, want waiting_user", got.Status)
	}
	if _, _, _, status := loadOnlyPrompt(t, db, thread.ID); status != "open" {
		t.Fatalf("must not open a second prompt, status=%q", status)
	}
	var users int
	if scanErr := db.QueryRow(`SELECT COUNT(*) FROM agent_hub_messages WHERE thread_id=? AND role='user'`, thread.ID).Scan(&users); scanErr != nil {
		t.Fatal(scanErr)
	}
	if users != 1 {
		t.Fatalf("user messages = %d, want 1 (do not insert the busy prompt)", users)
	}
}

func TestDetectOmitsLoopbackByDefault(t *testing.T) {
	t.Setenv("LUNITIDE_HARNESS_LOOPBACK", "")
	st := DetectAll(func(string) (string, error) { return "", exec.ErrNotFound }, nil)
	for _, item := range st {
		if item.Name == "loopback" {
			t.Fatalf("detect must omit loopback by default: %+v", st)
		}
	}
	if len(st) != 3 {
		t.Fatalf("default detect count = %d, want 3: %+v", len(st), st)
	}
}

func TestDetectIncludesLoopbackWhenHarnessEnvSet(t *testing.T) {
	t.Setenv("LUNITIDE_HARNESS_LOOPBACK", "1")
	st := DetectAll(func(string) (string, error) { return "", exec.ErrNotFound }, nil)
	var found bool
	for _, item := range st {
		if item.Name == "loopback" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("detect must include name=loopback when LUNITIDE_HARNESS_LOOPBACK=1: %+v", st)
	}
	if len(st) != 4 {
		t.Fatalf("detect count with loopback = %d, want 4: %+v", len(st), st)
	}
	if st[0].Name != "codex" || st[1].Name != "cursor" || st[2].Name != "kimi" {
		t.Fatalf("cursor/codex/kimi detect order changed: %+v", st)
	}
	if st[0].State != "not_installed" || st[1].State != "not_installed" || st[2].State != "not_installed" {
		t.Fatalf("cursor/codex/kimi detect outcomes changed: %+v", st)
	}
}

func loadLastMessage(t *testing.T, db *sql.DB, threadID string) (role, content string) {
	t.Helper()
	err := db.QueryRow(`SELECT role, content FROM agent_hub_messages WHERE thread_id=? ORDER BY seq DESC LIMIT 1`, threadID).Scan(&role, &content)
	if err != nil {
		t.Fatal(err)
	}
	return role, content
}

func loadOnlyPrompt(t *testing.T, db *sql.DB, threadID string) (callID, prompt, optionsJSON, status string) {
	t.Helper()
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM agent_hub_prompts WHERE thread_id=?`, threadID).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("prompt rows = %d, want 1", n)
	}
	err := db.QueryRow(`SELECT call_id, prompt, options_json, status FROM agent_hub_prompts WHERE thread_id=?`, threadID).
		Scan(&callID, &prompt, &optionsJSON, &status)
	if err != nil {
		t.Fatal(err)
	}
	return callID, prompt, optionsJSON, status
}

func assertYesNoOptions(t *testing.T, optionsJSON string) {
	t.Helper()
	var options []ThreadPromptOption
	if err := json.Unmarshal([]byte(optionsJSON), &options); err != nil {
		t.Fatalf("options_json: %v", err)
	}
	if len(options) != 2 || options[0].Label != "是" || options[1].Label != "否" {
		t.Fatalf("options = %#v, want labels 是 then 否", options)
	}
}
