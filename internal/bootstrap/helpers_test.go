package bootstrap

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/lunitide/lunitide/internal/compactionapp"
	"github.com/lunitide/lunitide/internal/ipc"
)

func TestShutdownAfterSessionCleanEndKeepsEngine(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ShutdownAfterSession(nil, cancel)
	select {
	case <-ctx.Done():
		t.Fatal("clean session end must not cancel the engine")
	default:
	}
}

func TestShutdownAfterSessionACKFailureTriggersShutdown(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	ShutdownAfterSession(fmt.Errorf("write failed: %w", ipc.ErrHandshakeACK), cancel)
	select {
	case <-ctx.Done():
	default:
		t.Fatal("ACK write failure did not trigger Engine shutdown")
	}
}

func TestCompactionRecoveryErrorFailsClosed(t *testing.T) {
	internal := fmt.Errorf("database path C:/private/lunitide.db")
	if err := CompactionRecoveryError(nil, internal); !errors.Is(err, internal) {
		t.Fatalf("top-level recovery error not propagated: %v", err)
	}
	results := []compactionapp.RecoveryResult{{CheckpointID: "checkpoint-1", Err: internal}}
	if err := CompactionRecoveryError(results, nil); !errors.Is(err, internal) {
		t.Fatalf("per-checkpoint recovery error not propagated: %v", err)
	}
	if err := CompactionRecoveryError([]compactionapp.RecoveryResult{{CheckpointID: "checkpoint-1"}}, nil); err != nil {
		t.Fatalf("successful recovery rejected: %v", err)
	}
}