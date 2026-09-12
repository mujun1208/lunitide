package agenthub

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"
)

var defaultLookPath LookPath = lookWithCommonPaths

func lookWithCommonPaths(name string) (string, error) {
	if path, err := exec.LookPath(name); err == nil && strings.TrimSpace(path) != "" {
		return preferRunnableCLI(path), nil
	}
	for _, dir := range append(extraPathDirs(), commonBinDirs()...) {
		if path := firstRunnableInDir(dir, name); path != "" {
			return path, nil
		}
	}
	return "", exec.ErrNotFound
}

func preferRunnableCLI(path string) string {
	if runtime.GOOS != "windows" {
		return path
	}
	switch strings.ToLower(filepath.Ext(path)) {
	case ".cmd", ".exe", ".bat", ".com":
		return path
	}
	for _, ext := range []string{".cmd", ".exe", ".bat"} {
		cand := path + ext
		if info, err := os.Stat(cand); err == nil && !info.IsDir() {
			return cand
		}
	}
	return path
}

func firstRunnableInDir(dir, name string) string {
	cands := []string{name, name + ".exe", name + ".cmd"}
	if runtime.GOOS == "windows" {
		cands = []string{name + ".cmd", name + ".exe", name + ".bat", name}
	}
	for _, cand := range cands {
		path := filepath.Join(dir, cand)
		info, err := os.Stat(path)
		if err == nil && !info.IsDir() {
			return path
		}
	}
	return ""
}

func commonBinDirs() []string {
	home := os.Getenv("USERPROFILE")
	if home == "" {
		home = os.Getenv("HOME")
	}
	local := os.Getenv("LOCALAPPDATA")
	roaming := os.Getenv("APPDATA")
	var dirs []string
	if home != "" {
		dirs = append(dirs, filepath.Join(home, ".local", "bin"), filepath.Join(home, ".cargo", "bin"), filepath.Join(home, "bin"), filepath.Join(home, ".kimi-code", "bin"), filepath.Join(home, ".kimi-code", "node_modules", ".bin"))
	}
	if local != "" {
		dirs = append(dirs, filepath.Join(local, "cursor-agent"), filepath.Join(local, "npm"), filepath.Join(local, "Programs"), filepath.Join(local, "Microsoft", "WinGet", "Links"))
	}
	if roaming != "" {
		dirs = append(dirs, filepath.Join(roaming, "npm"))
	}
	return dirs
}

func DetectAll(look LookPath, version VersionRunner) []AgentStatus {
	if look == nil {
		look = defaultLookPath
	}
	if version == nil {
		version = defaultVersion
	}
	names := agentNames()
	out := make([]AgentStatus, len(names))
	var wg sync.WaitGroup
	for i, name := range names {
		wg.Add(1)
		go func(i int, name string) {
			defer wg.Done()
			out[i] = detectOne(name, look, version)
		}(i, name)
	}
	wg.Wait()
	return out
}

func detectOne(name string, look LookPath, version VersionRunner) AgentStatus {
	cap := CapabilityFor(name)
	st := AgentStatus{Name: name, NonInteractive: cap.NonInteractive, StreamJSON: cap.StreamJSON}
	exe, err := look(exeName(name))
	if err != nil || strings.TrimSpace(exe) == "" {
		st.State = "not_installed"
		st.Hint = installHint(name)
		return st
	}
	text, verErr := version(exe, 2*time.Second)
	if verErr != nil {
		if !cap.NonInteractive {
			st.State = "unknown"
			st.Hint = "探测超时或失败，请确认该 CLI 能在本机执行 --version"
			return st
		}
		st.State = "available"
		st.Hint = "已找到 CLI"
		return st
	}
	st.Version = firstLine(text)
	if looksLoggedOut(text) {
		st.State = "not_logged_in"
		st.Hint = "已安装但未登录。请先在该 CLI 自己的终端完成登录。"
		return st
	}
	if !cap.NonInteractive {
		st.State = "unknown"
		st.Hint = "未检测到非交互 CLI，V1 只探测不执行"
		return st
	}
	st.State = "available"
	st.Hint = "可用"
	return st
}

func defaultVersion(exe string, timeout time.Duration) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, exe, "--version")
	hideVersionCmd(cmd)
	out, err := cmd.CombinedOutput()
	if ctx.Err() != nil {
		return string(out), ctx.Err()
	}
	return string(out), err
}

func firstLine(text string) string {
	text = strings.TrimSpace(text)
	if i := strings.IndexAny(text, "\r\n"); i >= 0 {
		text = text[:i]
	}
	if len(text) > 80 {
		return text[:80]
	}
	return text
}

func looksLoggedOut(text string) bool {
	low := strings.ToLower(text)
	return strings.Contains(text, "未登录") || strings.Contains(low, "not logged") || strings.Contains(low, "please login") || strings.Contains(low, "please sign") || strings.Contains(low, "sign in") || strings.Contains(low, "not authenticated") || strings.Contains(low, "unauthorized") || strings.Contains(low, "no model configured")
}

func installHint(name string) string {
	switch name {
	case "codex":
		return "未安装 Codex CLI。安装后重新打开调度台。"
	case "cursor":
		return "未安装 Cursor CLI（cursor-agent）。安装后重新打开调度台。"
	default:
		return "未安装 Kimi Code CLI（kimi）。安装后重新打开调度台。"
	}
}
