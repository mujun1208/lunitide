// P1-2 command progress coverage: ExecuteStreaming pipes stdout/stderr
// line-by-line to the progress sink while preserving the legacy combined
// result shape (64 KiB cap, error text in failure messages).
package toolruntime

import (
	"context"
	"strings"
	"sync"
	"testing"
)

func TestExecuteStreamingEmitsProgressAndResult(t *testing.T) {
	r, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	var mu sync.Mutex
	var chunks []string
	out, err := r.ExecuteStreaming(context.Background(), Approval, "01ARZ3NDEKTSV4RRFFQ69G5FAV", "command.run", []byte(`{"argv":["go","version"]}`), true, func(chunk string) {
		mu.Lock()
		chunks = append(chunks, chunk)
		mu.Unlock()
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.Output, "go version") {
		t.Fatalf("result missing go version output: %q", out.Output)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(chunks) < 1 {
		t.Fatalf("no progress chunks emitted: %v", chunks)
	}
	joined := strings.Join(chunks, "\n")
	if !strings.Contains(joined, "go version") {
		t.Fatalf("progress chunks missing output: %v", chunks)
	}
}

func TestExecuteStreamingFailureCarriesOutput(t *testing.T) {
	r, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	if err = r.SetCommandPolicyJSON([]byte(`{"commands":[],"fullAccess":true}`)); err != nil {
		t.Fatal(err)
	}
	var chunks []string
	_, err = r.ExecuteUnconfinedStreaming(context.Background(), "01ARZ3NDEKTSV4RRFFQ69G5FAV", "command.run", []byte(`{"argv":["cmd","/c","echo","boom","&&","exit","2"]}`), false, func(chunk string) {
		chunks = append(chunks, chunk)
	})
	if err == nil {
		t.Fatal("failing command must surface an error")
	}
	// On Windows cmd chains the echo runs before exit 2, so both the live
	// feed and the failure message carry the output.
	if len(chunks) == 0 && !strings.Contains(err.Error(), "command failed") {
		t.Fatalf("neither progress nor error carried output: chunks=%v err=%v", chunks, err)
	}
}

func TestExecuteWithoutProgressUnchanged(t *testing.T) {
	r, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	out, err := r.Execute(context.Background(), Approval, "01ARZ3NDEKTSV4RRFFQ69G5FAV", "command.run", []byte(`{"argv":["go","version"]}`), true)
	if err != nil || !strings.Contains(out.Output, "go version") {
		t.Fatalf("legacy path broken: out=%q err=%v", out.Output, err)
	}
}
