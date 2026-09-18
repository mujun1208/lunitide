package app

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/lunitide/lunitide/internal/mcp"
)

func TestMcpUvInstallStartsDownloadAndReportsProgress(t *testing.T) {
	installer := mcp.NewUvInstaller(t.TempDir())
	started := make(chan struct{})
	var once sync.Once
	installer.SetInstall(func(ctx context.Context, bundle mcp.UvBundle, progress func(mcp.UvProgress)) error {
		once.Do(func() { close(started) })
		progress(mcp.UvProgress{File: "uv.zip", Done: 10, Total: 100})
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(50 * time.Millisecond):
		}
		return nil
	})
	e := NewEngine(providerRepositoryStub{}, "test")
	e.SetMcpUv(installer)

	first := e.Handle(context.Background(), validRequest("mcp.uv.install", `{}`))
	if !first.OK {
		t.Fatalf("mcp.uv.install %#v", first.Error)
	}
	payload := first.Payload.(map[string]any)
	if payload["state"] != "downloading" && payload["state"] != "idle" && payload["state"] != "ready" {
		t.Fatalf("first snapshot %+v", payload)
	}
	<-started
	second := e.Handle(context.Background(), validRequest("mcp.uv.install", `{}`))
	if !second.OK {
		t.Fatalf("progress poll %#v", second.Error)
	}
	got := second.Payload.(map[string]any)
	if got["state"] != "downloading" && got["state"] != "ready" {
		t.Fatalf("poll while installing %+v", got)
	}
}

func TestMcpUvInstallMissingServiceIsChinese(t *testing.T) {
	e := NewEngine(providerRepositoryStub{}, "test")
	resp := e.Handle(context.Background(), validRequest("mcp.uv.install", `{}`))
	if resp.OK || resp.Error == nil || !strings.Contains(resp.Error.Message, "uv") {
		t.Fatalf("unwired install %#v", resp)
	}
	if strings.Contains(resp.Error.Message, "not packaged") || strings.Contains(resp.Error.Message, "nil") {
		t.Fatalf("must not leak English: %#v", resp.Error)
	}
}
