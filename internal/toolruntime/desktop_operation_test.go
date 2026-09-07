package toolruntime

import (
	"context"
	"errors"
	"testing"
	"time"
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
