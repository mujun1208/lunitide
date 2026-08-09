package main

import (
	"context"
	"fmt"
	"testing"

	"github.com/lunitide/lunitide/internal/ipc"
)

func TestACKWriteFailureTriggersShutdown(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	shutdownAfterSession(fmt.Errorf("write failed: %w", ipc.ErrHandshakeACK), cancel)
	select {
	case <-ctx.Done():
	default:
		t.Fatal("ACK write failure did not trigger Engine shutdown")
	}
}
