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

var (
	defaultLookPath     LookPath = lookWithCommonPaths
	versionProbeTimeout          = 8 * time.Second
	// extraPathLook / vendorInstallLook are the Windows registry PATH and
	// Uninstall-location fallbacks. Tests replace them so a developer
	// machine that already has cursor-agent cannot leak into fixtures.
	extraPathLook     = extraPathDirs
	vendorInstallLook = vendorInstallDirs
	loginProbe        = defaultLoginProbe
)

// SilenceExternalProbesForTest stops detect from launching Codex, Cursor, or Kimi.
func SilenceExternalProbesForTest() func() {
	prevCodex := probeCodexAppServer
	prevLogin := loginProbe
	probeCodexAppServer = func(LookPath) bool { return false }
	loginProbe = func(string, string) string { return "" }
	return func() {
		probeCodexAppServer = prevCodex
		loginProbe = prevLogin
	}
}

func lookWithCommonPaths(name string) (string, error) {
	if path, err := exec.LookPath(name); err == nil && strings.TrimSpace(path) != "" {
		return preferRunnableCLI(path), nil
	}
	dirs := append(extraPathLook(), commonBinDirs()...)
	dirs = append(dirs, vendorInstallLook()...)
	switch name {
	case "cursor-agent":
		dirs = append(dirs, dirsNearLauncher("cursor")...)
	case "kimi", "codex":
		dirs = append(dirs, dirsNearLauncher(name)...)
	}
	for _, dir := range dirs {
		if path := firstRunnableInDir(dir, name); path != "" {
			return path, nil
		}
	}
	return "", exec.ErrNotFound
}

func dirsNearLauncher(launcher string) []string {
	path, err := exec.LookPath(launcher)
	if err != nil || strings.TrimSpace(path) == "" {
		return nil
	}
	dir := filepath.Dir(preferRunnableCLI(path))
	var out []string
	for i := 0; i < 6; i++ {
		if dir == "" {
			break
		}
		out = append(out, dir)
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return out
}

func launcherPresent(name string) bool {
	path, err := exec.LookPath(name)
	return err == nil && strings.TrimSpace(path) != ""
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
		dirs = append(dirs, filepath.Join(home, ".local", "bin"), filepath.Join(home, ".cargo", "bin"), filepath.Join(home, "bin"), filepath.Join(home, ".cursor", "bin"), filepath.Join(home, ".kimi-code", "bin"), filepath.Join(home, ".kimi-code", "node_modules", ".bin"), filepath.Join(home, ".npm-global"), filepath.Join(home, ".npm-global", "bin"))
	}
	if local != "" {
		dirs = append(dirs, filepath.Join(local, "cursor-agent"), filepath.Join(local, "cursor-agent", "bin"), filepath.Join(local, "npm"), filepath.Join(local, "Programs"), filepath.Join(local, "Programs", "cursor"), filepath.Join(local, "Programs", "cursor", "resources", "app", "bin"), filepath.Join(local, "Programs", "Cursor"), filepath.Join(local, "Programs", "Cursor", "resources", "app", "bin"), filepath.Join(local, "Microsoft", "WinGet", "Links"))
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
	if os.Getenv("LUNITIDE_HARNESS_LOOPBACK") == "1" {
		out = append(out, AgentStatus{Name: "loopback", State: "available", Interactive: true, Protocol: "none"})
	}
	return out
}

func detectOne(name string, look LookPath, version VersionRunner) AgentStatus {
	cap := CapabilityFor(name)
	st := AgentStatus{Name: name, NonInteractive: cap.NonInteractive, StreamJSON: cap.StreamJSON, Interactive: cap.Interactive, Protocol: cap.Protocol}
	exe, err := look(exeName(name))
	if err != nil || strings.TrimSpace(exe) == "" {
		st.State = "not_installed"
		st.Hint = installHint(name)
		if name == "cursor" && launcherPresent("cursor") {
			st.Hint = "已找到 Cursor 软件，但没有 cursor-agent CLI。装好并登录后会自动连通。"
		}
		return st
	}
	text, verErr := version(exe, versionProbeTimeout)
	if verErr != nil {
		if !cap.NonInteractive {
			st.State = "unknown"
			st.Hint = "探测超时或失败，请确认该 CLI 能在本机执行 --version"
			return st
		}
		st.State = "available"
		st.Hint = "已找到 CLI"
		if name == "codex" {
			applyCodexDetect(&st, look)
		}
		if name == "cursor" {
			applyCursorDetect(&st, look, exe)
		}
		if name == "kimi" {
			applyKimiDetect(&st, look, exe)
		}
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
	if name == "codex" {
		applyCodexDetect(&st, look)
	}
	if name == "cursor" {
		applyCursorDetect(&st, look, exe)
	}
	if name == "kimi" {
		applyKimiDetect(&st, look, exe)
	}
	if (st.State == "available" || st.State == "unknown") && (name == "cursor" || name == "kimi") {
		if looksLoggedOut(loginProbe(name, exe)) {
			st.State = "not_logged_in"
			st.Hint = "已安装但未登录。桌面软件已登录时，请在 Agent Hub 再点一次“连接”完成本机 CLI 授权。"
		}
	}
	return st
}

func applyCursorDetect(st *AgentStatus, look LookPath, exe string) {
	if _, err := os.Stat(exe); err != nil {
		return
	}
	if _, _, err := resolveCursorACP(look); err != nil {
		st.State = "unknown"
		st.Hint = "已安装 Cursor CLI，但还不能聊天。需要本机 Node.js，或把 cursor-agent 配成可直接运行的程序。"
	}
}

func applyKimiDetect(st *AgentStatus, look LookPath, exe string) {
	if _, err := os.Stat(exe); err != nil {
		return
	}
	if _, _, err := resolveKimiACP(look); err != nil {
		st.State = "unknown"
		st.Hint = "已安装 Kimi CLI，但还不能聊天。需要本机 Node.js，或把 kimi 配成可直接运行的程序。"
	}
}

func applyCodexDetect(st *AgentStatus, look LookPath) {
	if probeCodexAppServer(look) {
		st.Interactive = true
		st.Protocol = "app-server"
		st.Hint = "可用"
		return
	}
	st.Interactive = false
	st.Protocol = "exec"
	st.Hint = codexAvailableHint
}

func defaultLoginProbe(name, exe string) string {
	if name != "cursor" && name != "kimi" {
		return ""
	}
	ctx, cancel := context.WithTimeout(context.Background(), versionProbeTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, exe, "status")
	hideVersionCmd(cmd)
	out, _ := cmd.CombinedOutput()
	return string(out)
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
		return "未安装 Codex CLI。安装后重新打开 Work。"
	case "cursor":
		return "未安装 Cursor CLI（cursor-agent）。安装后重新打开 Work。"
	default:
		return "未安装 Kimi Code CLI（kimi）。安装后重新打开 Work。"
	}
}
