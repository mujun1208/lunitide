package workspace_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/lunitide/lunitide/internal/domain/project"
	"github.com/lunitide/lunitide/internal/projectapp"
	storage "github.com/lunitide/lunitide/internal/storage/sqlite"
	"github.com/lunitide/lunitide/internal/workspace"
)

var convertProjects int

// newConvertProjectID creates a real projects row so the conversion
// target-project foreign key holds.
func newConvertProjectID(t *testing.T, store *storage.Store) string {
	t.Helper()
	ctx := context.Background()
	convertProjects++
	key := fmt.Sprintf("m5-convert-%d", convertProjects)
	p, err := projectapp.New(store, store).Create(ctx, key, "test", map[string]string{"name": "convert"}, project.Project{Name: "convert"})
	if err != nil {
		t.Fatal(err)
	}
	return p.ID
}

// convertHarness wires a ConvertService against a real store, one
// AdHocWorkspace holding a.txt + b/c.txt on disk and a shared fake clock.
// Reuses adhocHarness/newRunID from the sibling test files.
func convertHarness(t *testing.T) (*workspace.ConvertService, *fakeClock, string, string, string, string) {
	t.Helper()
	ctx := context.Background()
	_, clock, store := adhocHarness(t)
	runID := newRunID(t, store)
	projectID := newConvertProjectID(t, store)
	wsRoot := filepath.Join(t.TempDir(), "ws")
	if err := os.MkdirAll(wsRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	for rel, body := range map[string]string{"a.txt": "alpha", "b/c.txt": "charlie"} {
		full := filepath.Join(wsRoot, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	adhoc := workspace.NewAdHocService(store.AgentRuntimeRepository())
	w, err := adhoc.Create(ctx, runID, wsRoot, "CVT")
	if err != nil {
		t.Fatal(err)
	}
	svc := workspace.NewConvertService(store.AgentRuntimeRepository())
	svc.SetClock(clock)
	return svc, clock, w.ID, wsRoot, runID, projectID
}

var convertScope = workspace.ConvertScope{Paths: []string{"a.txt", "b/c.txt"}}

// dirSHA fingerprints every regular file under root (rel path -> sha256).
// A missing root is an empty tree.
func dirSHA(t *testing.T, root string) map[string]string {
	t.Helper()
	out := map[string]string{}
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			if os.IsNotExist(err) {
				return nil
			}
			return err
		}
		if !d.Type().IsRegular() {
			return nil
		}
		rel, rerr := filepath.Rel(root, p)
		if rerr != nil {
			return rerr
		}
		b, rerr := os.ReadFile(p)
		if rerr != nil {
			return rerr
		}
		sum := sha256.Sum256(b)
		out[filepath.ToSlash(rel)] = hex.EncodeToString(sum[:])
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// assertSourceIntact proves the "preserve the source" invariant: every
// file's sha256 must be identical before and after the operation.
func assertSourceIntact(t *testing.T, before, after map[string]string) {
	t.Helper()
	if len(before) != len(after) {
		t.Fatalf("source file count changed: before %d after %d (%v vs %v)", len(before), len(after), before, after)
	}
	for k, v := range before {
		if after[k] != v {
			t.Fatalf("source file %s changed: the source workspace must stay read-only", k)
		}
	}
}

// TestConvertCrashMatrix injects a crash at every phase and requires every
// conversion to converge to a terminal state with the source untouched.
func TestConvertCrashMatrix(t *testing.T) {
	t.Run("preview crash reconciles to abandoned", func(t *testing.T) {
		svc, clock, wsID, wsRoot, runID, projectID := convertHarness(t)
		ctx := context.Background()
		before := dirSHA(t, wsRoot)
		target := filepath.Join(t.TempDir(), "target")
		c, err := svc.Preview(ctx, runID, wsID, projectID, convertScope)
		if err != nil {
			t.Fatal(err)
		}
		if c.Phase != workspace.PhasePreview {
			t.Fatalf("phase must start at preview, got %s", c.Phase)
		}
		// Crash: never confirmed. Inside the 24h TTL the preview stays.
		clock.now = clock.now.Add(2 * time.Hour)
		early, err := svc.Reconcile(ctx, c.ID)
		if err != nil {
			t.Fatal(err)
		}
		if early.Phase != workspace.PhasePreview {
			t.Fatalf("preview inside TTL must stay in flight, got %s", early.Phase)
		}
		// Past the TTL the orphan converges to abandoned.
		clock.now = clock.now.Add(25 * time.Hour)
		after, err := svc.Reconcile(ctx, c.ID)
		if err != nil {
			t.Fatal(err)
		}
		if after.Phase != workspace.PhaseAbandoned {
			t.Fatalf("expired preview must reconcile to abandoned, got %s", after.Phase)
		}
		assertSourceIntact(t, before, dirSHA(t, wsRoot))
		if got := dirSHA(t, target); len(got) != 0 {
			t.Fatalf("no confirm must mean zero bytes at the target, got %v", got)
		}
	})

	t.Run("copying failure marks failed source intact", func(t *testing.T) {
		svc, _, wsID, wsRoot, runID, projectID := convertHarness(t)
		ctx := context.Background()
		before := dirSHA(t, wsRoot)
		staging := filepath.Join(t.TempDir(), "staging")
		c, err := svc.Preview(ctx, runID, wsID, projectID, convertScope)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := svc.Confirm(ctx, c.ID); err != nil {
			t.Fatal(err)
		}
		// Polluted source root: the copy fails midway and must converge to
		// failed without ever touching the real source.
		badRoot := filepath.Join(t.TempDir(), "no-such-root")
		got, err := svc.StageCopy(ctx, c.ID, badRoot, staging)
		if !errors.Is(err, workspace.ErrConvertPublishFailed) {
			t.Fatalf("stage copy must fail with ErrConvertPublishFailed, got %v", err)
		}
		if got.Phase != workspace.PhaseFailed {
			t.Fatalf("failed stage copy must mark phase failed, got %s", got.Phase)
		}
		assertSourceIntact(t, before, dirSHA(t, wsRoot))
	})

	t.Run("publishing crash reconciles to failed", func(t *testing.T) {
		svc, _, wsID, wsRoot, runID, projectID := convertHarness(t)
		ctx := context.Background()
		before := dirSHA(t, wsRoot)
		staging := filepath.Join(t.TempDir(), "staging")
		target := filepath.Join(t.TempDir(), "target")
		c, err := svc.Preview(ctx, runID, wsID, projectID, convertScope)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := svc.Confirm(ctx, c.ID); err != nil {
			t.Fatal(err)
		}
		if _, err := svc.StageCopy(ctx, c.ID, wsRoot, staging); err != nil {
			t.Fatal(err)
		}
		// Crash mid-publish: half the files already moved to the target
		// while the phase journal still says publishing.
		if err := os.MkdirAll(target, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.Rename(filepath.Join(staging, "a.txt"), filepath.Join(target, "a.txt")); err != nil {
			t.Fatal(err)
		}
		after, err := svc.Reconcile(ctx, c.ID)
		if err != nil {
			t.Fatal(err)
		}
		if after.Phase != workspace.PhaseFailed && after.Phase != workspace.PhaseAbandoned {
			t.Fatalf("publishing orphan must converge to failed/abandoned, got %s", after.Phase)
		}
		assertSourceIntact(t, before, dirSHA(t, wsRoot))
		// Reconcile is idempotent: the terminal state never flips again.
		again, err := svc.Reconcile(ctx, c.ID)
		if err != nil {
			t.Fatal(err)
		}
		if again.Phase != after.Phase {
			t.Fatalf("reconcile must be idempotent: %s then %s", after.Phase, again.Phase)
		}
	})

	t.Run("happy path commits", func(t *testing.T) {
		svc, _, wsID, wsRoot, runID, projectID := convertHarness(t)
		ctx := context.Background()
		before := dirSHA(t, wsRoot)
		staging := filepath.Join(t.TempDir(), "staging")
		target := filepath.Join(t.TempDir(), "target")
		c, err := svc.Preview(ctx, runID, wsID, projectID, convertScope)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := svc.Confirm(ctx, c.ID); err != nil {
			t.Fatal(err)
		}
		if _, err := svc.StageCopy(ctx, c.ID, wsRoot, staging); err != nil {
			t.Fatal(err)
		}
		done, err := svc.Publish(ctx, c.ID, staging, target)
		if err != nil {
			t.Fatal(err)
		}
		if done.Phase != workspace.PhaseCommitted || !done.Committed || done.CommittedAt == nil {
			t.Fatalf("commit mismatch: %+v", done)
		}
		assertSourceIntact(t, before, dirSHA(t, wsRoot))
		body, err := os.ReadFile(filepath.Join(target, "a.txt"))
		if err != nil || string(body) != "alpha" {
			t.Fatalf("target a.txt mismatch: %q %v", body, err)
		}
		body, err = os.ReadFile(filepath.Join(target, "b", "c.txt"))
		if err != nil || string(body) != "charlie" {
			t.Fatalf("target b/c.txt mismatch: %q %v", body, err)
		}
	})

	t.Run("stage without confirm refused", func(t *testing.T) {
		svc, _, wsID, wsRoot, runID, projectID := convertHarness(t)
		ctx := context.Background()
		staging := filepath.Join(t.TempDir(), "staging")
		c, err := svc.Preview(ctx, runID, wsID, projectID, convertScope)
		if err != nil {
			t.Fatal(err)
		}
		_, err = svc.StageCopy(ctx, c.ID, wsRoot, staging)
		if !errors.Is(err, workspace.ErrConvertStateBad) {
			t.Fatalf("unconfirmed stage must answer ErrConvertStateBad, got %v", err)
		}
		if !errors.Is(err, workspace.ErrConvertNoConfirm) {
			t.Fatalf("unconfirmed stage must carry CVT-001, got %v", err)
		}
		if got := dirSHA(t, staging); len(got) != 0 {
			t.Fatalf("no confirm must copy zero bytes, staging holds %v", got)
		}
	})

	t.Run("abandon from every non-terminal phase", func(t *testing.T) {
		for _, phase := range []string{"preview", "copying", "publishing"} {
			svc, _, wsID, wsRoot, runID, projectID := convertHarness(t)
			ctx := context.Background()
			before := dirSHA(t, wsRoot)
			c, err := svc.Preview(ctx, runID, wsID, projectID, convertScope)
			if err != nil {
				t.Fatal(err)
			}
			if phase != "preview" {
				if _, err := svc.Confirm(ctx, c.ID); err != nil {
					t.Fatal(err)
				}
			}
			if phase == "publishing" {
				if _, err := svc.StageCopy(ctx, c.ID, wsRoot, filepath.Join(t.TempDir(), "staging")); err != nil {
					t.Fatal(err)
				}
			}
			got, err := svc.Abandon(ctx, c.ID)
			if err != nil {
				t.Fatalf("abandon from %s: %v", phase, err)
			}
			if got.Phase != workspace.PhaseAbandoned {
				t.Fatalf("abandon from %s got %s", phase, got.Phase)
			}
			assertSourceIntact(t, before, dirSHA(t, wsRoot))
			// Terminal: abandoning again is refused.
			if _, err := svc.Abandon(ctx, c.ID); !errors.Is(err, workspace.ErrConvertStateBad) {
				t.Fatalf("re-abandon must be refused, got %v", err)
			}
		}
	})
}

// TestConvertScopeInvalid: traversal paths, oversized and empty scopes
// never reach storage.
func TestConvertScopeInvalid(t *testing.T) {
	svc, _, wsID, _, runID, projectID := convertHarness(t)
	ctx := context.Background()
	if _, err := svc.Preview(ctx, runID, wsID, projectID, workspace.ConvertScope{Paths: []string{`..\evil`}}); !errors.Is(err, workspace.ErrConvertScopeInvalid) {
		t.Fatalf("traversal path must be rejected, got %v", err)
	}
	paths := make([]string, workspace.MaxConvertScopeEntries+1)
	for i := range paths {
		paths[i] = fmt.Sprintf("f%d.txt", i)
	}
	if _, err := svc.Preview(ctx, runID, wsID, projectID, workspace.ConvertScope{Paths: paths}); !errors.Is(err, workspace.ErrConvertScopeInvalid) {
		t.Fatalf("oversized scope must be rejected, got %v", err)
	}
	if _, err := svc.Preview(ctx, runID, wsID, projectID, workspace.ConvertScope{}); !errors.Is(err, workspace.ErrConvertScopeInvalid) {
		t.Fatalf("empty scope must be rejected, got %v", err)
	}
}
