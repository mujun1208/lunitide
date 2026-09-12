//go:build livehub

package agenthub

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestLiveCodexDetectStartArtifactPreview(t *testing.T) {
	s := New(NewMemoryStore(), t.TempDir(), func(string, string) error { return nil })
	var codex AgentStatus
	for _, agent := range s.Detect() {
		t.Logf("detect %s state=%s version=%q hint=%s", agent.Name, agent.State, agent.Version, agent.Hint)
		if agent.Name == "codex" {
			codex = agent
		}
		if agent.Name == "kimi" {
			t.Logf("kimi live detect: %+v", agent)
		}
	}
	if codex.State != "available" || !codex.NonInteractive {
		t.Fatalf("codex must be available on this machine: %+v", codex)
	}

	workDir := t.TempDir()
	detail, err := s.StartTask(TaskRequest{
		Agent:          "codex",
		Prompt:         "只在本目录创建一个名为 hello.txt 的文件，内容只写 hello，不要读取或修改其它文件，完成后立刻结束。",
		WorkDir:        workDir,
		Sandbox:        "workspace-write",
		TimeoutMin:     8,
		IdempotencyKey: "live-codex-hello",
	})
	if err != nil {
		t.Fatal(err)
	}

	deadline := time.Now().Add(5 * time.Minute)
	for time.Now().Before(deadline) {
		detail, err = s.GetTask(detail.Task.ID)
		if err != nil {
			t.Fatal(err)
		}
		if detail.Task.Status == "queued" || detail.Task.Status == "running" {
			time.Sleep(400 * time.Millisecond)
			continue
		}
		break
	}
	if detail.Task.Status != "success" {
		t.Fatalf("live task %s: %+v events=%+v", detail.Task.Status, detail.Task, detail.Events)
	}
	entries, _ := os.ReadDir(workDir)
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		names = append(names, entry.Name())
	}
	t.Logf("status=%s tokens=%d files=%v artifacts=%d events=%d", detail.Task.Status, detail.Task.TokensUsed, names, len(detail.Artifacts), len(detail.Events))

	preview := ""
	for _, art := range detail.Artifacts {
		if strings.EqualFold(art.Name, "hello.txt") || strings.EqualFold(art.Name, "codex-last-message.md") {
			preview = art.Path
			if strings.EqualFold(art.Name, "hello.txt") {
				break
			}
		}
	}
	if preview == "" {
		t.Fatalf("no previewable artifact: files=%v artifacts=%+v events=%+v", names, detail.Artifacts, detail.Events)
	}
	abs, err := s.ResolveFile(detail.Task.ID, preview)
	if err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(abs)
	if err != nil {
		t.Fatal(err)
	}
	if len(strings.TrimSpace(string(body))) == 0 {
		t.Fatalf("preview body empty: %q", abs)
	}
	if _, err = s.ResolveFile(detail.Task.ID, filepath.Join("..", "secret.txt")); err == nil {
		t.Fatal("escape must fail")
	}
}

func TestLiveCursorDetectStartArtifactPreview(t *testing.T) {
	s := New(NewMemoryStore(), t.TempDir(), func(string, string) error { return nil })
	var cursor AgentStatus
	for _, agent := range s.Detect() {
		t.Logf("detect %s state=%s version=%q hint=%s", agent.Name, agent.State, agent.Version, agent.Hint)
		if agent.Name == "cursor" {
			cursor = agent
		}
	}
	if cursor.State != "available" || !cursor.NonInteractive {
		t.Fatalf("cursor must be available after CLI install: %+v", cursor)
	}
	workDir := t.TempDir()
	detail, err := s.StartTask(TaskRequest{
		Agent:          "cursor",
		Prompt:         "只在本目录创建一个名为 hello.txt 的文件，内容只写 hello，不要读取或修改其它文件，完成后立刻结束。",
		WorkDir:        workDir,
		TimeoutMin:     8,
		IdempotencyKey: "live-cursor-hello",
	})
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Minute)
	for time.Now().Before(deadline) {
		detail, err = s.GetTask(detail.Task.ID)
		if err != nil {
			t.Fatal(err)
		}
		if detail.Task.Status == "queued" || detail.Task.Status == "running" {
			time.Sleep(400 * time.Millisecond)
			continue
		}
		break
	}
	if detail.Task.Status != "success" {
		t.Fatalf("live cursor %s: %+v events=%+v", detail.Task.Status, detail.Task, detail.Events)
	}
	if len(detail.Events) == 0 {
		t.Fatal("cursor produced no timeline events")
	}
	entries, _ := os.ReadDir(workDir)
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		names = append(names, entry.Name())
	}
	t.Logf("status=%s files=%v artifacts=%d events=%d", detail.Task.Status, names, len(detail.Artifacts), len(detail.Events))
	preview := ""
	for _, art := range detail.Artifacts {
		if strings.EqualFold(art.Name, "hello.txt") || strings.HasSuffix(strings.ToLower(art.Name), ".md") || strings.HasSuffix(strings.ToLower(art.Name), ".txt") {
			preview = art.Path
			if strings.EqualFold(art.Name, "hello.txt") {
				break
			}
		}
	}
	if preview == "" && len(detail.Events) > 0 {
		return
	}
	if preview == "" {
		t.Fatalf("cursor left no previewable output: files=%v artifacts=%+v events=%+v", names, detail.Artifacts, detail.Events)
	}
	if _, err = s.ResolveFile(detail.Task.ID, preview); err != nil {
		t.Fatal(err)
	}
}

func TestLiveKimiDetectStartArtifactPreview(t *testing.T) {
	s := New(NewMemoryStore(), t.TempDir(), func(string, string) error { return nil })
	var kimi AgentStatus
	for _, agent := range s.Detect() {
		t.Logf("detect %s state=%s version=%q hint=%s", agent.Name, agent.State, agent.Version, agent.Hint)
		if agent.Name == "kimi" {
			kimi = agent
		}
	}
	if kimi.State != "available" || !kimi.NonInteractive {
		t.Fatalf("kimi must be available after CLI install: %+v", kimi)
	}
	workDir := t.TempDir()
	detail, err := s.StartTask(TaskRequest{
		Agent:          "kimi",
		Prompt:         "只在本目录创建一个名为 hello.txt 的文件，内容只写 hello，不要读取或修改其它文件，完成后立刻结束。",
		WorkDir:        workDir,
		TimeoutMin:     8,
		IdempotencyKey: "live-kimi-hello",
	})
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Minute)
	for time.Now().Before(deadline) {
		detail, err = s.GetTask(detail.Task.ID)
		if err != nil {
			t.Fatal(err)
		}
		if detail.Task.Status == "queued" || detail.Task.Status == "running" {
			time.Sleep(400 * time.Millisecond)
			continue
		}
		break
	}
	if detail.Task.Status != "success" {
		t.Fatalf("live kimi %s: %+v events=%+v", detail.Task.Status, detail.Task, detail.Events)
	}
	if len(detail.Events) == 0 {
		t.Fatal("kimi produced no timeline events")
	}
	entries, _ := os.ReadDir(workDir)
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		names = append(names, entry.Name())
	}
	t.Logf("status=%s files=%v artifacts=%d events=%d", detail.Task.Status, names, len(detail.Artifacts), len(detail.Events))
	preview := ""
	for _, art := range detail.Artifacts {
		if strings.EqualFold(art.Name, "hello.txt") || strings.HasSuffix(strings.ToLower(art.Name), ".md") || strings.HasSuffix(strings.ToLower(art.Name), ".txt") {
			preview = art.Path
			if strings.EqualFold(art.Name, "hello.txt") {
				break
			}
		}
	}
	if preview == "" && len(detail.Events) > 0 {
		return
	}
	if preview == "" {
		t.Fatalf("kimi left no previewable output: files=%v artifacts=%+v events=%+v", names, detail.Artifacts, detail.Events)
	}
	if _, err = s.ResolveFile(detail.Task.ID, preview); err != nil {
		t.Fatal(err)
	}
}
