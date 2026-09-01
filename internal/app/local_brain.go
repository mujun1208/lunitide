package app

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	BoundBrainPrefix       = "brain:"
	BrainLunitide          = "lunitide"
	BrainCodex             = "codex"
	BrainClaude            = "claude"
	localBrainTimeout      = 90 * time.Second
	localBrainMaxRunes     = 4000
	localBrainPromptCap    = 24000
	localBrainLastFile     = ".last-reply.txt"
	localBrainFallbackLock = "已回落月汐"
)

// BoundBrainFromKeys reads brain:<kind> from expert skill bindings.
// Unknown or empty values fall back to the Lunitide engine.
func BoundBrainFromKeys(keys []string) string {
	for _, raw := range keys {
		rest, ok := strings.CutPrefix(strings.TrimSpace(raw), BoundBrainPrefix)
		if !ok {
			continue
		}
		switch strings.ToLower(strings.TrimSpace(rest)) {
		case BrainCodex:
			return BrainCodex
		case BrainClaude:
			return BrainClaude
		}
	}
	return BrainLunitide
}

func lookPathBrain(kind string) (string, bool) {
	switch kind {
	case BrainCodex:
		path, err := exec.LookPath("codex")
		return path, err == nil && path != ""
	case BrainClaude:
		path, err := exec.LookPath("claude")
		return path, err == nil && path != ""
	default:
		return "", false
	}
}

func localBrainWorkDir(expertID string) string {
	expertID = strings.TrimSpace(expertID)
	if expertID == "" {
		return ""
	}
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return ""
	}
	dir := filepath.Join(home, ".lunitide", "brains", expertID)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return ""
	}
	return dir
}

func localBrainPrefix(kind string) string {
	switch kind {
	case BrainCodex:
		return "【本机 Codex】\n"
	case BrainClaude:
		return "【本机 Claude Code】\n"
	default:
		return ""
	}
}

func localBrainUserError(kind string, err error) string {
	label := "本机大脑"
	if kind == BrainCodex {
		label = "本机 Codex"
	}
	if kind == BrainClaude {
		label = "本机 Claude Code"
	}
	lead := localBrainFallbackLock + "。"
	if err == nil {
		return lead + label + " 失败。已改用月汐引擎。"
	}
	msg := strings.TrimSpace(err.Error())
	if strings.Contains(msg, "not on PATH") || strings.Contains(msg, "executable file not found") {
		return lead + label + " 未在 PATH，没跑起来。已改用月汐引擎。"
	}
	if strings.Contains(strings.ToLower(msg), "timeout") || errors.Is(err, context.DeadlineExceeded) {
		return lead + label + " 超时。已改用月汐引擎。"
	}
	return lead + label + " 失败：" + clipRunes(msg, 200) + "。已改用月汐引擎。"
}

func localBrainFallbackNotice(kind string, err error) string {
	return localBrainUserError(kind, err) + "下面是月汐引擎，不是本机 Codex / Claude Code。\n"
}

func localBrainFallbackLockHint(note string) string {
	note = strings.TrimSpace(note)
	if note == "" {
		return ""
	}
	if !strings.HasSuffix(note, "\n") {
		note += "\n"
	}
	return note + "系统前缀「" + localBrainFallbackLock + "」必须原样留在回复开头，不得改写或删除。\n"
}

func lockLocalBrainFallback(note, body string) string {
	note = strings.TrimSpace(note)
	body = strings.TrimSpace(body)
	if note == "" {
		return body
	}
	if strings.HasPrefix(body, localBrainFallbackLock) {
		return body
	}
	if body == "" {
		return note
	}
	return note + "\n" + body
}

func readLocalBrainResume(workDir string) string {
	if workDir == "" {
		return ""
	}
	raw, err := os.ReadFile(filepath.Join(workDir, localBrainLastFile))
	if err != nil {
		return ""
	}
	text := strings.TrimSpace(string(raw))
	if text == "" {
		return ""
	}
	if strings.HasPrefix(text, "用户：") || strings.Contains(text, "\n助手：") {
		return "\n[上一轮本机大脑]\n" + clipRunes(text, 3500) + "\n"
	}
	return "\n[上一轮本机大脑回复]\n" + clipRunes(text, 2000) + "\n"
}

func writeLocalBrainResume(workDir, userText, assistant string) {
	if workDir == "" || strings.TrimSpace(assistant) == "" {
		return
	}
	var b strings.Builder
	if u := strings.TrimSpace(userText); u != "" {
		b.WriteString("用户：")
		b.WriteString(clipRunes(u, 1500))
		b.WriteByte('\n')
	}
	b.WriteString("助手：")
	b.WriteString(clipRunes(strings.TrimSpace(assistant), 2000))
	_ = os.WriteFile(filepath.Join(workDir, localBrainLastFile), []byte(b.String()), 0o600)
}

func runLocalBrain(ctx context.Context, kind, prompt, workDir, userText string) (string, error) {
	bin, ok := lookPathBrain(kind)
	if !ok {
		return "", errors.New("local brain not on PATH")
	}
	prompt = strings.TrimSpace(prompt)
	if prompt == "" {
		return "", errors.New("empty prompt")
	}
	if resume := readLocalBrainResume(workDir); resume != "" {
		prompt += resume
		prompt += "可接上上一轮，不要重开整份说明书。\n"
	}
	if utf8.RuneCountInString(prompt) > localBrainPromptCap {
		prompt = string([]rune(prompt)[:localBrainPromptCap])
	}
	work, cancel := context.WithTimeout(ctx, localBrainTimeout)
	defer cancel()
	var cmd *exec.Cmd
	switch kind {
	case BrainCodex:
		cmd = exec.CommandContext(work, bin, "exec", "--skip-git-repo-check", "-")
		cmd.Stdin = strings.NewReader(prompt)
	case BrainClaude:
		cmd = exec.CommandContext(work, bin, "-p", prompt, "--output-format", "text")
	default:
		return "", errors.New("unknown local brain")
	}
	if workDir != "" {
		cmd.Dir = workDir
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return "", errors.New(msg)
	}
	out := strings.TrimSpace(stdout.String())
	if out == "" {
		out = strings.TrimSpace(stderr.String())
	}
	if utf8.RuneCountInString(out) > localBrainMaxRunes {
		out = string([]rune(out)[:localBrainMaxRunes])
	}
	if out == "" {
		return "", errors.New("local brain returned empty output")
	}
	writeLocalBrainResume(workDir, userText, out)
	return out, nil
}
