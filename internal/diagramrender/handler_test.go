package diagramrender

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lunitide/lunitide/internal/commandworker"
)

func TestDiagramWorkerOwnsBoundedProcessAndRemovesPrivateArtifacts(t *testing.T) {
	h := New(t.TempDir())
	h.Executable = filepath.Join(t.TempDir(), "desktop.exe")
	var task string
	h.Run = func(ctx context.Context, spec commandworker.Spec, _ commandworker.StartGuard, _ func([]byte)) (commandworker.Outcome, error) {
		task = spec.Dir
		if spec.Exe != h.Executable || spec.Timeout != Timeout || spec.MaxOutputBytes != 4096 || len(spec.Args) != 1 {
			t.Fatalf("unbounded worker: %#v", spec)
		}
		if _, ok := ctx.Deadline(); !ok {
			t.Fatal("missing independent deadline")
		}
		path := strings.TrimPrefix(spec.Args[0], "--diagram-worker=")
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		var job Job
		if json.Unmarshal(raw, &job) != nil || job.RendererDir != h.RendererDir || job.Request.Source != "sequenceDiagram\nA->>B: hi" {
			t.Fatalf("wrong worker input: %s", raw)
		}
		if err = os.WriteFile(filepath.Join(task, "result.json"), []byte(`{"svg":"<svg xmlns=\"http://www.w3.org/2000/svg\"><text>hi</text></svg>"}`), 0600); err != nil {
			t.Fatal(err)
		}
		return commandworker.Outcome{}, nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), Timeout)
	defer cancel()
	result, err := h.render(ctx, Request{Source: "sequenceDiagram\nA->>B: hi", Config: json.RawMessage(`{}`)})
	if err != nil || !strings.Contains(result.SVG, "hi") {
		t.Fatalf("result: %#v %v", result, err)
	}
	if _, err = os.Stat(task); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("private source/profile left behind: %v", err)
	}
}

func TestDiagramTimeoutNeverReturnsPartialSVG(t *testing.T) {
	h := New(t.TempDir())
	h.Executable = filepath.Join(t.TempDir(), "desktop.exe")
	h.Run = func(context.Context, commandworker.Spec, commandworker.StartGuard, func([]byte)) (commandworker.Outcome, error) {
		return commandworker.Outcome{TimedOut: true}, nil
	}
	result, err := h.render(context.Background(), Request{Source: "flowchart LR\nA-->B", Config: json.RawMessage(`{}`)})
	if err == nil || result.SVG != "" || !strings.Contains(err.Error(), "源码仍保留") {
		t.Fatalf("timeout appeared successful: %#v %v", result, err)
	}
}
