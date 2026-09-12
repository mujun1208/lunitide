// Package diagramrender owns a short-lived desktop worker for real Mermaid
// rendering. Its Job Object, not a renderer Promise, enforces the CPU deadline.
package diagramrender

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/lunitide/lunitide/internal/bridge"
	"github.com/lunitide/lunitide/internal/commandworker"
)

// Cold-start of a hidden WebView2 plus a 10-node Chinese flowchart needs more
// than 8s; the Job Object still kills a wedged worker.
const Timeout = 20 * time.Second
const MaxResultBytes = 2 << 20

type Request struct {
	Source string          `json:"source"`
	Config json.RawMessage `json:"config"`
}
type Job struct {
	Request     Request `json:"request"`
	RendererDir string  `json:"rendererDir"`
}
type Result struct {
	SVG   string `json:"svg"`
	Error string `json:"error,omitempty"`
}
type Runner func(context.Context, commandworker.Spec, commandworker.StartGuard, func([]byte)) (commandworker.Outcome, error)
type Handler struct {
	RendererDir string
	Executable  string
	Run         Runner
	slots       chan struct{}
}

func New(rendererDir string) *Handler {
	return &Handler{RendererDir: rendererDir, slots: make(chan struct{}, 2)}
}

func (h *Handler) HandleHost(ctx context.Context, r bridge.Request) bridge.Response {
	var p Request
	if json.Unmarshal(r.Payload, &p) != nil || len(p.Source) == 0 || len(p.Source) > 32768 || len(p.Config) > 32768 || !json.Valid(p.Config) {
		return r.Fail("BRIDGE_SCHEMA_INVALID", "图表参数无效", false)
	}
	ctx, cancel := context.WithTimeout(ctx, Timeout)
	defer cancel()
	select {
	case h.slots <- struct{}{}:
		defer func() { <-h.slots }()
	case <-ctx.Done():
		return r.Fail("DIAGRAM_BUSY", "图表正在处理中，请稍后重试", true)
	}
	result, err := h.render(ctx, p)
	if err != nil {
		return r.Fail("DIAGRAM_RENDER_FAILED", err.Error(), false)
	}
	return r.Ok(result)
}

func (h *Handler) render(ctx context.Context, p Request) (Result, error) {
	task, err := os.MkdirTemp("", "lunitide-diagram-")
	if err != nil {
		return Result{}, err
	}
	defer func() { _ = os.RemoveAll(task) }()
	raw, err := json.Marshal(Job{Request: p, RendererDir: h.RendererDir})
	if err != nil {
		return Result{}, err
	}
	path := filepath.Join(task, "job.json")
	if err = os.WriteFile(path, raw, 0600); err != nil {
		return Result{}, err
	}
	executable := h.Executable
	if executable == "" {
		executable, err = os.Executable()
		if err != nil {
			return Result{}, err
		}
	}
	run := h.Run
	if run == nil {
		run = commandworker.Run
	}
	outcome, err := run(ctx, commandworker.Spec{Exe: executable, Args: []string{"--diagram-worker=" + path}, Dir: task, Env: os.Environ(), Timeout: Timeout, MaxOutputBytes: 4096}, nil, nil)
	if err != nil {
		return Result{}, errors.New("独立图表进程未能启动，源码仍保留")
	}
	if outcome.TimedOut || errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return Result{}, errors.New("图表渲染超时，已回收独立渲染进程；源码仍保留")
	}
	if ctx.Err() != nil {
		return Result{}, errors.New("图表渲染已取消，已回收独立渲染进程；源码仍保留")
	}
	if outcome.ExitCode != 0 {
		return Result{}, errors.New("独立图表渲染失败，源码仍保留")
	}
	resultPath := filepath.Join(task, "result.json")
	file, err := os.Open(resultPath)
	if err != nil {
		return Result{}, errors.New("图表没有返回可核验结果")
	}
	defer func() { _ = file.Close() }()
	body, err := io.ReadAll(io.LimitReader(file, MaxResultBytes+1))
	if err != nil || len(body) > MaxResultBytes {
		return Result{}, errors.New("图表输出超过预算，源码仍保留")
	}
	var result Result
	if json.Unmarshal(body, &result) != nil {
		return Result{}, errors.New("图表结果格式无效")
	}
	if result.Error != "" {
		return Result{}, errors.New(result.Error)
	}
	if !strings.HasPrefix(strings.TrimSpace(result.SVG), "<svg") {
		return Result{}, errors.New("图表没有生成有效 SVG")
	}
	return result, nil
}
