package toolruntime

import (
	"bytes"
	"context"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestCommandOutputDrainsLongLinesWithBoundedRetention(t *testing.T) {
	for _, streaming := range []bool{false, true} {
		collector := &commandOutput{}
		t.Cleanup(collector.closeProgress)
		var progress atomic.Int32
		var oversized atomic.Bool
		if streaming {
			collector.progress = func(s string) {
				progress.Add(1)
				if len([]rune(s)) > 400 {
					oversized.Store(true)
				}
			}
		}
		payload := bytes.Repeat([]byte("x"), 2<<20)
		n, err := io.Copy(collector, bytes.NewReader(payload))
		if err != nil || n != int64(len(payload)) {
			t.Fatalf("output not drained: %d %v", n, err)
		}
		text := collector.text()
		if len(collector.retained) != commandOutputLimit || len(collector.line) != 0 || !strings.Contains(text, "truncated") {
			t.Fatal("retention budget or explicit truncation violated")
		}
		collector.closeProgress()
		if streaming {
			<-collector.done
		}
		if oversized.Load() || progress.Load() > toolProgressMaxChunks {
			t.Fatalf("unbounded progress: count=%d oversized=%v", progress.Load(), oversized.Load())
		}
	}
}

func TestCommandOutputLongUTF8KeepsChineseAcrossByteLimits(t *testing.T) {
	chunks := make(chan string, toolProgressMaxChunks)
	collector := &commandOutput{progress: func(s string) { chunks <- s }}
	defer collector.closeProgress()
	payload := []byte(strings.Repeat("中", commandOutputLimit))
	if _, err := collector.Write(payload); err != nil {
		t.Fatal(err)
	}
	got := collector.text()
	want := strings.Repeat("中", commandOutputLimit/len("中")) + "\n[output truncated at 64 KiB; process output was drained]"
	if got != want {
		t.Fatal("retained UTF-8 output was decoded as another encoding")
	}
	for range collector.emitted {
		select {
		case chunk := <-chunks:
			if chunk == "" || strings.Trim(chunk, "中") != "" {
				t.Fatalf("split UTF-8 progress: %q", chunk)
			}
		case <-time.After(2 * time.Second):
			t.Fatal("progress was not dispatched")
		}
	}
}

func TestCommandOutputBlockedProgressDoesNotBlockDrainOrWait(t *testing.T) {
	entered, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	unblock := func() { once.Do(func() { close(release) }) }
	defer unblock()
	collector := &commandOutput{progress: func(string) { close(entered); <-release }}
	defer collector.closeProgress()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestCommandOutputProcessHelper$")
	cmd.Env = append(os.Environ(), "LUNITIDE_COMMAND_OUTPUT_TEST_HELPER=1")
	cmd.Stdout, cmd.Stderr = collector, collector
	cmd.WaitDelay = 100 * time.Millisecond
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	waited := make(chan error, 1)
	go func() { waited <- cmd.Wait() }()
	select {
	case <-entered:
	case <-time.After(2 * time.Second):
		t.Fatal("progress callback did not start")
	}
	select {
	case err := <-waited:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		unblock()
		<-waited
		t.Fatal("command Wait depended on blocked progress")
	}
	if !strings.Contains(collector.text(), "truncated") {
		t.Fatal("child output was not retained/drained")
	}
	closed := make(chan struct{})
	go func() { collector.closeProgress(); close(closed) }()
	select {
	case <-closed:
	case <-time.After(time.Second):
		t.Fatal("collector shutdown waited on blocked progress")
	}
	// Even repeated commands cannot accumulate callbacks behind one stuck sink.
	for range 10 {
		var called atomic.Bool
		next := &commandOutput{progress: func(string) { called.Store(true) }}
		if _, err := next.Write([]byte("next\n")); err != nil {
			t.Fatal(err)
		}
		next.closeProgress()
		select {
		case <-next.done:
		case <-time.After(time.Second):
			t.Fatal("another progress worker accumulated behind blocked sink")
		}
		if called.Load() {
			t.Fatal("process-wide callback budget was exceeded")
		}
	}
	unblock()
	<-collector.done
}

func TestCommandOutputProcessHelper(t *testing.T) {
	if os.Getenv("LUNITIDE_COMMAND_OUTPUT_TEST_HELPER") != "1" {
		return
	}
	_, err := os.Stdout.Write(bytes.Repeat([]byte("line\n"), 400000))
	if err != nil {
		os.Exit(1)
	}
	os.Exit(0)
}

func TestCommandOutputProgressPanicDoesNotEscapeWorker(t *testing.T) {
	collector := &commandOutput{progress: func(string) { panic("sink failed") }}
	defer collector.closeProgress()
	if _, err := collector.Write([]byte("line\n")); err != nil {
		t.Fatal(err)
	}
	select {
	case <-collector.done:
	case <-time.After(time.Second):
		t.Fatal("panicking progress worker did not terminate")
	}
	if len(commandProgressSlot) != 0 {
		t.Fatal("panic leaked the process-wide callback slot")
	}
}

func TestCommandOutputCloseFlushesPromptProgress(t *testing.T) {
	delivered := make(chan string, 1)
	collector := &commandOutput{progress: func(s string) { delivered <- s }}
	if _, err := collector.Write([]byte("short command result")); err != nil {
		t.Fatal(err)
	}
	if collector.text() != "short command result" {
		t.Fatal("short output changed")
	}
	collector.closeProgress()
	select {
	case got := <-delivered:
		if got != "short command result" {
			t.Fatalf("wrong progress: %q", got)
		}
	default:
		t.Fatal("prompt progress was discarded at command completion")
	}
}

func TestCommandOutputKeepsSmallOutputAndConcurrentStreams(t *testing.T) {
	collector := &commandOutput{}
	var wg sync.WaitGroup
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range 100 {
				_, _ = collector.Write([]byte("line\n"))
			}
		}()
	}
	wg.Wait()
	if got := strings.Count(collector.text(), "line"); got != 200 {
		t.Fatalf("lost output: %d", got)
	}
}
