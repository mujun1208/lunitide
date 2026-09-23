package toolruntime

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
)

var liveServerURL = regexp.MustCompile(`https?://[^\s"'<>]+`)

type captureBuf struct {
	mu sync.Mutex
	b  strings.Builder
}

func (c *captureBuf) Write(p []byte) (int, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.b.Write(p)
}

func (c *captureBuf) String() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.b.String()
}

func (r *Runtime) trackLiveServer(session string, cmd *exec.Cmd) {
	r.liveMu.Lock()
	defer r.liveMu.Unlock()
	if r.liveServers == nil {
		r.liveServers = map[string]*exec.Cmd{}
	}
	if prev := r.liveServers[session]; prev != nil && prev.Process != nil {
		_ = prev.Process.Kill()
	}
	r.liveServers[session] = cmd
}

// startLiveServer runs a local demo server and leaves it running after the
// tool returns. command.run's job object would otherwise kill the process
// and the model would hand the user a command to paste.
func (r *Runtime) startLiveServer(ctx context.Context, session, root string, argv []string) (string, error) {
	if len(argv) == 0 || argv[0] == "" {
		return "", fmt.Errorf("没有可启动的命令。不要让用户在外面执行")
	}
	exe := argv[0]
	if !filepath.IsAbs(exe) {
		return "", fmt.Errorf("解释器路径无效。不要让用户在外面执行")
	}
	dir := root
	for _, a := range argv[1:] {
		if looksLikeScriptPath(a) && scriptExists(root, a) {
			dir = filepath.Dir(a)
			break
		}
	}
	cmd := exec.Command(exe, argv[1:]...)
	cmd.Dir = dir
	cmd.Env = commandEnv(os.Environ(), "PYTHONIOENCODING=utf-8", "PYTHONUTF8=1", "PYTHONUNBUFFERED=1")
	detachProcess(cmd)
	buf := &captureBuf{}
	cmd.Stdout = buf
	cmd.Stderr = buf
	if err := cmd.Start(); err != nil {
		return "", fmt.Errorf("服务没有启动：%s。不要让用户把命令粘贴到外面执行", err.Error())
	}
	r.trackLiveServer(session, cmd)
	exited := make(chan struct{})
	go func() {
		_ = cmd.Wait()
		close(exited)
	}()
	timer := time.NewTimer(3 * time.Second)
	defer timer.Stop()
	tick := time.NewTicker(100 * time.Millisecond)
	defer tick.Stop()
	var url string
	for url == "" {
		select {
		case <-ctx.Done():
			if cmd.Process != nil {
				_ = cmd.Process.Kill()
			}
			return "", fmt.Errorf("启动被取消。不要让用户在外面执行")
		case <-exited:
			msg := strings.TrimSpace(buf.String())
			if msg == "" {
				msg = "进程已退出且没有输出"
			}
			return "", fmt.Errorf("服务没有留在运行：%s。不要让用户把命令粘贴到外面执行，根据这段输出修好后再 command.run", msg)
		case <-tick.C:
			url = firstLiveURL(buf.String())
		case <-timer.C:
			select {
			case <-exited:
				msg := strings.TrimSpace(buf.String())
				return "", fmt.Errorf("服务没有留在运行：%s。不要让用户把命令粘贴到外面执行", msg)
			default:
				url = firstLiveURL(buf.String())
				if url == "" {
					url = "进程已启动，输出里还没有地址"
				}
			}
		}
	}
	return fmt.Sprintf("服务已在本机运行，无需用户再执行命令。\n地址：%s\n工作目录：%s\n不要输出 PPT，不要把命令交给用户粘贴。", strings.TrimRight(url, ".,)"), dir), nil
}

func firstLiveURL(text string) string {
	return liveServerURL.FindString(text)
}
