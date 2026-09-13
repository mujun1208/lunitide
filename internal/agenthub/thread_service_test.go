package agenthub

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

const (
	sceneWriteProject = "在你选的文件夹里按你的规则创建子目录并写文件。不要把已有文件挪到别处。"
	sceneFix          = "在此仓库根内检索和修改。已有文件保持原路径。新文件按已有结构和你的规则放置。"
	scenePPT          = "用 Kimi 自己的技能做文稿。pptx 写在工作区；指定了导出目录则完成时复制过去。"
	restartNotice     = "应用重启后未能继续"
	restartTaskNotice = "应用重启后未能继续该任务"
)

func TestCreateThreadStoresExactSceneSystemMessage(t *testing.T) {
	cases := []struct {
		scene string
		want  string
	}{
		{"write_project", sceneWriteProject},
		{"fix", sceneFix},
		{"ppt", scenePPT},
	}
	for _, tc := range cases {
		t.Run(tc.scene, func(t *testing.T) {
			s := testThreadService(t)
			detail, err := s.CreateThread(ThreadCreateRequest{
				HarnessID: "loopback",
				Scene:     tc.scene,
			})
			if err != nil {
				t.Fatal(err)
			}
			if len(detail.Messages) < 1 {
				t.Fatal("expected a stored system message")
			}
			first := detail.Messages[0]
			if first.Role != "system" || first.Content != tc.want {
				t.Fatalf("first message = %s %q, want system %q", first.Role, first.Content, tc.want)
			}
			got, err := s.GetThread(detail.Thread.ID)
			if err != nil {
				t.Fatal(err)
			}
			if len(got.Messages) < 1 || got.Messages[0].Role != "system" || got.Messages[0].Content != tc.want {
				t.Fatalf("GetThread messages = %#v", got.Messages)
			}
		})
	}
}

func TestCreateThreadFreeHasNoSystemMessage(t *testing.T) {
	s := testThreadService(t)
	detail, err := s.CreateThread(ThreadCreateRequest{HarnessID: "loopback", Scene: "free"})
	if err != nil {
		t.Fatal(err)
	}
	for _, msg := range detail.Messages {
		if msg.Role == "system" {
			t.Fatalf("free scene must not store a system row: %#v", detail.Messages)
		}
	}
}

func TestCreateThreadClipsTitleToFirstLine200(t *testing.T) {
	s := testThreadService(t)
	long := strings.Repeat("字", 220)
	detail, err := s.CreateThread(ThreadCreateRequest{
		HarnessID: "loopback",
		Scene:     "free",
		Title:     "第一行\n" + long,
	})
	if err != nil {
		t.Fatal(err)
	}
	if detail.Thread.Title != "第一行" {
		t.Fatalf("title = %q, want first line", detail.Thread.Title)
	}
	clipped, err := s.CreateThread(ThreadCreateRequest{
		HarnessID: "loopback",
		Scene:     "free",
		Title:     long,
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := []rune(clipped.Thread.Title); len(got) != 200 || string(got) != strings.Repeat("字", 200) {
		t.Fatalf("clipped title len = %d %q", len(got), clipped.Thread.Title)
	}
}

func TestGetThreadReportsUsageTokens(t *testing.T) {
	s := testThreadService(t)
	created, err := s.CreateThread(ThreadCreateRequest{HarnessID: "loopback", Scene: "free"})
	if err != nil {
		t.Fatal(err)
	}
	if err = insertThreadEvent(s.Threads, created.Thread.ID, AgentEvent{Type: "usage", Tokens: 7}); err != nil {
		t.Fatal(err)
	}
	got, err := s.GetThread(created.Thread.ID)
	if err != nil || got.TokensUsed != 7 {
		t.Fatalf("tokensUsed = %#v %v", got.TokensUsed, err)
	}
}

func TestRespondThreadKeepsOptionalText(t *testing.T) {
	s := testThreadService(t)
	created, err := s.CreateThread(ThreadCreateRequest{HarnessID: "loopback", Scene: "free", WorkspaceRoot: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.PromptThread(created.Thread.ID, "选哪个?"); err != nil {
		t.Fatal(err)
	}
	waiting, err := s.GetThread(created.Thread.ID)
	if err != nil || waiting.Prompt == nil {
		t.Fatalf("prompt = %#v %v", waiting.Prompt, err)
	}
	got, err := s.RespondThread(created.Thread.ID, waiting.Prompt.CallID, "是", "补充一句")
	if err != nil {
		t.Fatal(err)
	}
	var sawText bool
	for _, msg := range got.Messages {
		if msg.Role == "user" && msg.Content == "补充一句" {
			sawText = true
		}
	}
	if !sawText {
		t.Fatalf("respond text missing from messages: %#v", got.Messages)
	}
}

func TestCreateThreadFaultsWhenHarnessOpenFails(t *testing.T) {
	s := testThreadService(t)
	s.Look = func(string) (string, error) { return "", fmt.Errorf("未安装 Cursor CLI") }
	detail, err := s.CreateThread(ThreadCreateRequest{HarnessID: "cursor", Scene: "free"})
	if err != nil {
		t.Fatal(err)
	}
	if detail.Thread.Status != "faulted" {
		t.Fatalf("status = %q, want faulted", detail.Thread.Status)
	}
	var found bool
	for _, msg := range detail.Messages {
		if msg.Role == "notice" && strings.Contains(msg.Content, "未安装 Cursor CLI") {
			found = true
		}
	}
	if !found {
		t.Fatalf("missing open-fail notice: %#v", detail.Messages)
	}
}

func TestDeleteThreadRemovesRow(t *testing.T) {
	s := testThreadService(t)
	created, err := s.CreateThread(ThreadCreateRequest{HarnessID: "loopback", Scene: "free", Title: "删我"})
	if err != nil {
		t.Fatal(err)
	}
	if err = s.DeleteThread(created.Thread.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = s.GetThread(created.Thread.ID); err == nil {
		t.Fatal("expected deleted thread to be gone")
	}
}

func TestUpdateThreadClipsTitleToFirstLine200(t *testing.T) {
	s := testThreadService(t)
	created, err := s.CreateThread(ThreadCreateRequest{HarnessID: "loopback", Scene: "free", Title: "旧标题"})
	if err != nil {
		t.Fatal(err)
	}
	updated, err := s.UpdateThread(created.Thread.ID, "新标题\n第二行"+strings.Repeat("x", 10), nil)
	if err != nil {
		t.Fatal(err)
	}
	if updated.Thread.Title != "新标题" {
		t.Fatalf("title = %q, want first line", updated.Thread.Title)
	}
}

func TestRecoverFaultsLiveThreadsKeepsIdleAndTaskSentence(t *testing.T) {
	s := testService(t)
	s.Threads = NewThreadStore(openThreadDB(t))
	running := sampleThread("01ARZ3NDEKTSV4RRFFQ69G5FB0", "loopback", "Live", false)
	running.Status = "running"
	waiting := sampleThread("01ARZ3NDEKTSV4RRFFQ69G5FB1", "loopback", "Ask", false)
	waiting.Status = "waiting_user"
	idle := sampleThread("01ARZ3NDEKTSV4RRFFQ69G5FB2", "loopback", "Idle", false)
	done := sampleThread("01ARZ3NDEKTSV4RRFFQ69G5FB3", "loopback", "Done", false)
	done.Status = "success"
	for _, thread := range []ThreadRecord{running, waiting, idle, done} {
		if err := s.Threads.Insert(thread); err != nil {
			t.Fatal(err)
		}
	}
	if err := insertThreadPrompt(s.Threads, waiting.ID, "call-wait", "继续?", `[{"id":"是","label":"是"}]`); err != nil {
		t.Fatal(err)
	}
	if err := s.Store.InsertTask(TaskRecord{
		ID: "01ARZ3NDEKTSV4RRFFQ69G5FB4", Agent: "codex", Prompt: "stale",
		WorkDir: t.TempDir(), Status: "running", CreatedAt: "2026-01-01T00:00:00Z",
		IdempotencyKey: "thread-recover-task",
	}); err != nil {
		t.Fatal(err)
	}

	s.Recover()

	assertThreadFaultedWithRestart(t, s, running.ID)
	assertThreadFaultedWithRestart(t, s, waiting.ID)
	open, err := s.Threads.OpenPrompt(waiting.ID)
	if err != nil || open != nil {
		t.Fatalf("open prompt after recover = %#v %v", open, err)
	}
	idleGot, err := s.Threads.Get(idle.ID)
	if err != nil || idleGot.Status != "idle" {
		t.Fatalf("idle = %#v %v", idleGot, err)
	}
	doneGot, err := s.Threads.Get(done.ID)
	if err != nil || doneGot.Status != "success" {
		t.Fatalf("success = %#v %v", doneGot, err)
	}
	task, err := s.Store.GetTask("01ARZ3NDEKTSV4RRFFQ69G5FB4")
	if err != nil || task.Status != "failed" || task.ErrorMsg != restartTaskNotice {
		t.Fatalf("V1 task recover = %#v %v", task, err)
	}
}

func TestPromptAndStatusAndUpdateBumpUpdatedAtAndTitle(t *testing.T) {
	s := testThreadService(t)
	created, err := s.CreateThread(ThreadCreateRequest{HarnessID: "loopback", Scene: "free", Title: "新会话"})
	if err != nil {
		t.Fatal(err)
	}
	if err = stampThreadTime(s.Threads, created.Thread.ID, "2020-01-01T00:00:00Z"); err != nil {
		t.Fatal(err)
	}
	if _, err = s.PromptThread(created.Thread.ID, "  第一行标题\n第二行"); err != nil {
		t.Fatal(err)
	}
	afterPrompt, err := s.GetThread(created.Thread.ID)
	if err != nil {
		t.Fatal(err)
	}
	if afterPrompt.Thread.UpdatedAt == "2020-01-01T00:00:00Z" {
		t.Fatal("prompt must bump updated_at")
	}
	if afterPrompt.Thread.Title != "第一行标题" {
		t.Fatalf("title = %q, want first user line", afterPrompt.Thread.Title)
	}

	if err = stampThreadTime(s.Threads, created.Thread.ID, "2020-01-02T00:00:00Z"); err != nil {
		t.Fatal(err)
	}
	if err = setThreadStatus(s.Threads, created.Thread.ID, "success"); err != nil {
		t.Fatal(err)
	}
	afterStatus, err := s.Threads.Get(created.Thread.ID)
	if err != nil {
		t.Fatal(err)
	}
	if afterStatus.UpdatedAt == "2020-01-02T00:00:00Z" {
		t.Fatal("status change must bump updated_at")
	}

	if err = stampThreadTime(s.Threads, created.Thread.ID, "2020-01-03T00:00:00Z"); err != nil {
		t.Fatal(err)
	}
	if _, err = s.UpdateThread(created.Thread.ID, "Pinned", boolPtr(true)); err != nil {
		t.Fatal(err)
	}
	afterUpdate, err := s.GetThread(created.Thread.ID)
	if err != nil {
		t.Fatal(err)
	}
	if afterUpdate.Thread.UpdatedAt == "2020-01-03T00:00:00Z" {
		t.Fatal("Update must bump updated_at")
	}
}

func TestListThreadsOrdersLaterActiveAboveOlderIdle(t *testing.T) {
	s := testThreadService(t)
	older, err := s.CreateThread(ThreadCreateRequest{HarnessID: "loopback", Scene: "free", Title: "older"})
	if err != nil {
		t.Fatal(err)
	}
	newer, err := s.CreateThread(ThreadCreateRequest{HarnessID: "loopback", Scene: "free", Title: "newer-idle"})
	if err != nil {
		t.Fatal(err)
	}
	if err = stampThreadTime(s.Threads, older.Thread.ID, "2020-01-01T00:00:00Z"); err != nil {
		t.Fatal(err)
	}
	if err = stampThreadTime(s.Threads, newer.Thread.ID, "2020-06-01T00:00:00Z"); err != nil {
		t.Fatal(err)
	}
	if _, err = s.PromptThread(older.Thread.ID, "later activity"); err != nil {
		t.Fatal(err)
	}
	listed, err := s.ListThreads("")
	if err != nil || len(listed) != 2 {
		t.Fatalf("list = %#v %v", listed, err)
	}
	if listed[0].ID != older.Thread.ID || listed[1].ID != newer.Thread.ID {
		t.Fatalf("order = %q then %q, want later-active first", listed[0].ID, listed[1].ID)
	}
}

func TestTitleFromPromptTruncatesTo200(t *testing.T) {
	s := testThreadService(t)
	created, err := s.CreateThread(ThreadCreateRequest{HarnessID: "loopback", Scene: "free"})
	if err != nil {
		t.Fatal(err)
	}
	long := strings.Repeat("标题", 120)
	if _, err = s.PromptThread(created.Thread.ID, long); err != nil {
		t.Fatal(err)
	}
	got, err := s.GetThread(created.Thread.ID)
	if err != nil {
		t.Fatal(err)
	}
	runes := []rune(got.Thread.Title)
	if len(runes) != 200 {
		t.Fatalf("title runes = %d, want 200", len(runes))
	}
}

func testThreadService(t *testing.T) *Service {
	t.Helper()
	return &Service{
		Store:   NewMemoryStore(),
		Threads: NewThreadStore(openThreadDB(t)),
		Root:    t.TempDir(),
		Now:     time.Now,
	}
}

func assertThreadFaultedWithRestart(t *testing.T, s *Service, id string) {
	t.Helper()
	got, err := s.GetThread(id)
	if err != nil {
		t.Fatal(err)
	}
	if got.Thread.Status != "faulted" {
		t.Fatalf("status = %q, want faulted", got.Thread.Status)
	}
	var found bool
	for _, msg := range got.Messages {
		if strings.Contains(msg.Content, restartTaskNotice) {
			t.Fatalf("thread recover reused the task-only sentence: %q", msg.Content)
		}
		if strings.Contains(msg.Content, restartNotice) {
			found = true
		}
	}
	for _, ev := range got.Events {
		if strings.Contains(ev.Title, restartNotice) || strings.Contains(ev.Detail, restartNotice) {
			found = true
		}
	}
	if !found {
		t.Fatalf("missing %q in messages/events: %#v %#v", restartNotice, got.Messages, got.Events)
	}
}

func boolPtr(v bool) *bool { return &v }

func stampThreadTime(store *ThreadStore, id, ts string) error {
	_, err := store.db.Exec(`UPDATE agent_hub_threads SET updated_at=? WHERE id=?`, ts, id)
	return err
}
