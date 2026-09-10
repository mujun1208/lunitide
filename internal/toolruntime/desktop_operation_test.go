package toolruntime

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/lunitide/lunitide/internal/ccapp"
)

func TestDesktopOperationSerializesAndCancelsQueuedWork(t *testing.T) {
	release, err := acquireDesktopOperation(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	finished := make(chan error, 1)
	go func() {
		next, err := acquireDesktopOperation(ctx)
		if next != nil {
			next()
		}
		finished <- err
	}()
	cancel()
	select {
	case err := <-finished:
		if !errors.Is(err, context.Canceled) {
			t.Errorf("queued action acquired active desktop: %v", err)
		}
	case <-time.After(time.Second):
		t.Error("queued desktop action ignored cancellation")
	}
	release()
	ctx, cancel = context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	next, err := acquireDesktopOperation(ctx)
	if err != nil {
		t.Fatalf("desktop lock leaked: %v", err)
	}
	next()
}

func TestAcquireDesktopOperationBlocksWhenLocked(t *testing.T) {
	desktopInputBlocked = func() bool { return true }
	t.Cleanup(func() { desktopInputBlocked = workstationLocked })
	release, err := acquireDesktopOperation(context.Background())
	if release != nil {
		release()
		t.Fatal("locked session must not take the desktop slot")
	}
	if !errors.Is(err, ErrDesktopLocked) || !strings.Contains(err.Error(), "锁屏") {
		t.Fatalf("locked input must fail closed in Chinese: %v", err)
	}
}

func TestRuntimeDoesNotSendDesktopInputWhenLocked(t *testing.T) {
	desktopInputBlocked = func() bool { return true }
	t.Cleanup(func() { desktopInputBlocked = workstationLocked })
	r, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	r.SetCcExecutor(func(context.Context, string, string, json.RawMessage, bool) (ccapp.Outcome, error) {
		t.Fatal("locked session must not send keys")
		return ccapp.Outcome{}, nil
	})
	_, err = r.Execute(context.Background(), FullAccess, "s01", "desktop.type", json.RawMessage(`{"text":"secret"}`), true)
	if !errors.Is(err, ErrDesktopLocked) {
		t.Fatalf("desktop.type on lock screen: %v", err)
	}
	_, err = r.Execute(context.Background(), FullAccess, "s01", "computer.act", json.RawMessage(`{"action":"type","text":"secret"}`), true)
	if !errors.Is(err, ErrDesktopLocked) {
		t.Fatalf("computer.act on lock screen: %v", err)
	}
}
