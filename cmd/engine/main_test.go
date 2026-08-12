package main

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/lunitide/lunitide/internal/compactionapp"
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

func TestCompactionRecoveryErrorFailsClosed(t *testing.T) {
	internal := fmt.Errorf("database path C:/private/lunitide.db")
	if err := compactionRecoveryError(nil, internal); !errors.Is(err, internal) {
		t.Fatalf("top-level recovery error not propagated: %v", err)
	}
	results := []compactionapp.RecoveryResult{{CheckpointID: "checkpoint-1", Err: internal}}
	if err := compactionRecoveryError(results, nil); !errors.Is(err, internal) {
		t.Fatalf("per-checkpoint recovery error not propagated: %v", err)
	}
	if err := compactionRecoveryError([]compactionapp.RecoveryResult{{CheckpointID: "checkpoint-1"}}, nil); err != nil {
		t.Fatalf("successful recovery rejected: %v", err)
	}
}
