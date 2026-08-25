package artifact_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/lunitide/lunitide/internal/agentrunapp"
	"github.com/lunitide/lunitide/internal/domain/agentrun"
	"github.com/lunitide/lunitide/internal/domain/project"
	"github.com/lunitide/lunitide/internal/domain/session"
	"github.com/lunitide/lunitide/internal/projectapp"
	"github.com/lunitide/lunitide/internal/sessionapp"
	storage "github.com/lunitide/lunitide/internal/storage/sqlite"
)

// openTestStore opens a fresh migrated SQLite store.
func openTestStore(t *testing.T) (*storage.Store, error) {
	t.Helper()
	store, err := storage.OpenTemplated(context.Background(), filepath.Join(t.TempDir(), "m5-artifact.db"))
	if err != nil {
		return nil, err
	}
	t.Cleanup(func() { _ = store.Close() })
	return store, nil
}

var artifactRuns int

// seedRun creates the project -> session -> run chain so the artifact
// foreign key holds.
func seedRun(t *testing.T, store *storage.Store) (string, error) {
	t.Helper()
	ctx := context.Background()
	artifactRuns++
	key := fmt.Sprintf("m5-art-%d", artifactRuns)
	p, err := projectapp.New(store, store).Create(ctx, key+"-p", "test", map[string]string{"name": "artifact"}, project.Project{Name: "artifact"})
	if err != nil {
		return "", err
	}
	sess, err := sessionapp.New(store, store).Create(ctx, key+"-s", "test", map[string]string{"projectId": p.ID}, session.Session{ProjectID: p.ID, Title: "artifact"})
	if err != nil {
		return "", err
	}
	run, err := agentrunapp.New(store.AgentRuntimeRepository()).Start(ctx, key+"-r", "test", map[string]string{"sessionId": sess.ID}, sess.ID, agentrun.Budget{MaxModelTurns: 4, MaxToolCalls: 4, MaxTokens: 100, MaxCostMicros: 100, MaxWallClockSeconds: 30, MaxOutputBytes: 1024, MaxRetries: 1, MaxNoProgress: 1, HardCeiling: true})
	if err != nil {
		return "", err
	}
	return run.ID, nil
}

// casRoot is captured by registryHarness for tamper tests.
var casRoot string

// tamperCAS overwrites the blob file under a registered digest.
func tamperCAS(t *testing.T, ref string, data []byte) error {
	t.Helper()
	return os.WriteFile(filepath.Join(casRoot, ref[:2], ref), data, 0o644)
}
