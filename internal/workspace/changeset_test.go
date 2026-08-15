package workspace_test

import (
	"context"
	"errors"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"testing"

	"github.com/lunitide/lunitide/internal/domain/m5workspace"
	"github.com/lunitide/lunitide/internal/workspace"
)

func changesetHarness(t *testing.T) (*workspace.ChangeSetService, string, string, string) {
	t.Helper()
	ctx := context.Background()
	svc, clock, store := adhocHarness(t)
	_, _ = svc, clock
	runID := newRunID(t, store)
	wsRoot := filepath.Join(t.TempDir(), "ws")
	if err := os.MkdirAll(wsRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	adhoc := workspace.NewAdHocService(store.AgentRuntimeRepository())
	w, err := adhoc.Create(ctx, runID, wsRoot, "CS")
	if err != nil {
		t.Fatal(err)
	}
	cas, err := workspace.NewCASStore(filepath.Join(t.TempDir(), "cas"))
	if err != nil {
		t.Fatal(err)
	}
	cs := workspace.NewChangeSetService(store.AgentRuntimeRepository(), cas)
	return cs, w.ID, wsRoot, runID
}

// TestChangesetRoundtrip replays 200 seeded random change sets. After apply
// the tree must equal the expected state; after revert the tree must equal
// the pre-apply state byte for byte.
func TestChangesetRoundtrip(t *testing.T) {
	csSvc, wsID, wsRoot, runID := changesetHarness(t)
	ctx := context.Background()
	rng := rand.New(rand.NewSource(42))
	files := map[string]string{} // rel path -> content
	for round := 0; round < 200; round++ {
		// Random 1..5 item change set over a pool of paths.
		pool := []string{"a.txt", "src/main.go", "src/util.go", "docs/readme.md", "x/y/z.txt", "tmp.log"}
		var items []workspace.StageItem
		want := map[string]string{}
		for k, v := range files {
			want[k] = v
		}
		chosen := map[string]bool{}
		n := 1 + rng.Intn(5)
		for i := 0; i < n; i++ {
			path := pool[rng.Intn(len(pool))]
			if chosen[path] {
				continue
			}
			chosen[path] = true
			roll := rng.Intn(10)
			switch {
			case roll < 4: // add or modify
				content := fmt.Sprintf("round=%d i=%d rand=%d", round, i, rng.Int63())
				items = append(items, workspace.StageItem{Path: path, Change: m5workspace.ChangeAdd, Content: []byte(content)})
				if _, exists := want[path]; exists {
					items[len(items)-1].Change = m5workspace.ChangeModify
				}
				want[path] = content
			case roll < 7: // delete (only meaningful when present)
				if _, exists := files[path]; exists {
					items = append(items, workspace.StageItem{Path: path, Change: m5workspace.ChangeDelete})
					delete(want, path)
				}
			default: // skip
			}
		}
		if len(items) == 0 {
			continue
		}
		staged, err := csSvc.Stage(ctx, workspace.StageInput{RunID: runID, WorkspaceID: wsID, Source: "agent", Items: items})
		if err != nil {
			t.Fatalf("round %d stage: %v", round, err)
		}
		if _, err := csSvc.Apply(ctx, staged.ID); err != nil {
			t.Fatalf("round %d apply: %v", round, err)
		}
		if err := assertTree(wsRoot, want); err != nil {
			t.Fatalf("round %d post-apply tree: %v", round, err)
		}
		if _, err := csSvc.Revert(ctx, staged.ID); err != nil {
			t.Fatalf("round %d revert: %v", round, err)
		}
		if err := assertTree(wsRoot, files); err != nil {
			t.Fatalf("round %d post-revert tree: %v", round, err)
		}
	}
}

func assertTree(root string, want map[string]string) error {
	got := map[string]string{}
	err := filepath.Walk(root, func(p string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return err
		}
		rel, rerr := filepath.Rel(root, p)
		if rerr != nil {
			return rerr
		}
		b, rerr := os.ReadFile(p)
		if rerr != nil {
			return rerr
		}
		got[filepath.ToSlash(rel)] = string(b)
		return nil
	})
	if err != nil {
		return err
	}
	if len(got) != len(want) {
		return fmt.Errorf("file count got %d want %d (%v vs %v)", len(got), len(want), keys(got), keys(want))
	}
	for k, v := range want {
		if got[k] != v {
			return fmt.Errorf("file %s content mismatch", k)
		}
	}
	return nil
}

func keys(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

// TestChangesetBaseConflict: external drift between stage and apply flips
// the set to conflict without writing a single byte.
func TestChangesetBaseConflict(t *testing.T) {
	csSvc, wsID, wsRoot, runID := changesetHarness(t)
	ctx := context.Background()
	if err := os.WriteFile(filepath.Join(wsRoot, "base.txt"), []byte("base"), 0o644); err != nil {
		t.Fatal(err)
	}
	staged, err := csSvc.Stage(ctx, workspace.StageInput{RunID: runID, WorkspaceID: wsID, Items: []workspace.StageItem{{Path: "base.txt", Change: m5workspace.ChangeModify, Content: []byte("drift-me")}}})
	if err != nil {
		t.Fatal(err)
	}
	// External drift after staging.
	if err := os.WriteFile(filepath.Join(wsRoot, "base.txt"), []byte("externally changed"), 0o644); err != nil {
		t.Fatal(err)
	}
	cs, err := csSvc.Apply(ctx, staged.ID)
	if !errors.Is(err, m5workspace.ErrChangeSetConflict) {
		t.Fatalf("apply must answer CHANGESET_BASE_CONFLICT, got %v", err)
	}
	if cs.State != m5workspace.ChangeSetConflict {
		t.Fatalf("changeset state must be conflict, got %s", cs.State)
	}
	body, _ := os.ReadFile(filepath.Join(wsRoot, "base.txt"))
	if string(body) != "externally changed" {
		t.Fatalf("conflict must not write bytes, got %q", body)
	}
	// A conflicted set can neither apply nor revert.
	if _, err := csSvc.Apply(ctx, staged.ID); !errors.Is(err, workspace.ErrChangeSetStateBad) {
		t.Fatalf("conflicted apply must be refused, got %v", err)
	}
	if _, err := csSvc.Revert(ctx, staged.ID); !errors.Is(err, workspace.ErrChangeSetStateBad) {
		t.Fatalf("conflicted revert must be refused, got %v", err)
	}
}

// TestChangesetStateGuards covers the staged -> applied -> reverted machine
// and its refusal of illegal transitions.
func TestChangesetStateGuards(t *testing.T) {
	csSvc, wsID, _, runID := changesetHarness(t)
	ctx := context.Background()
	staged, err := csSvc.Stage(ctx, workspace.StageInput{RunID: runID, WorkspaceID: wsID, Items: []workspace.StageItem{{Path: "new.txt", Change: m5workspace.ChangeAdd, Content: []byte("hi")}}})
	if err != nil {
		t.Fatal(err)
	}
	// Revert of a staged set is refused.
	if _, err := csSvc.Revert(ctx, staged.ID); !errors.Is(err, workspace.ErrChangeSetStateBad) {
		t.Fatalf("staged revert must be refused, got %v", err)
	}
	if _, err := csSvc.Apply(ctx, staged.ID); err != nil {
		t.Fatal(err)
	}
	// Second apply is refused (already applied).
	if _, err := csSvc.Apply(ctx, staged.ID); !errors.Is(err, workspace.ErrChangeSetStateBad) {
		t.Fatalf("applied re-apply must be refused, got %v", err)
	}
	if _, err := csSvc.Revert(ctx, staged.ID); err != nil {
		t.Fatal(err)
	}
	// Revert of a reverted set is refused.
	if _, err := csSvc.Revert(ctx, staged.ID); !errors.Is(err, workspace.ErrChangeSetStateBad) {
		t.Fatalf("reverted re-revert must be refused, got %v", err)
	}
	// Bad change kinds and escaping paths are refused at stage time.
	if _, err := csSvc.Stage(ctx, workspace.StageInput{RunID: runID, WorkspaceID: wsID, Items: []workspace.StageItem{{Path: "x.txt", Change: "chmod", Content: nil}}}); !errors.Is(err, workspace.ErrChangeKindInvalid) {
		t.Fatalf("bad kind must be refused, got %v", err)
	}
	if _, err := csSvc.Stage(ctx, workspace.StageInput{RunID: runID, WorkspaceID: wsID, Items: []workspace.StageItem{{Path: "../escape", Change: m5workspace.ChangeAdd, Content: []byte("x")}}}); !errors.Is(err, workspace.ErrPathEscape) {
		t.Fatalf("escaping path must be refused WS-002, got %v", err)
	}
	if _, err := csSvc.Stage(ctx, workspace.StageInput{RunID: runID, WorkspaceID: wsID}); !errors.Is(err, workspace.ErrChangeSetEmpty) {
		t.Fatalf("empty set must be refused, got %v", err)
	}
}

// TestChangesetPreviewListsItems checks the review surface.
func TestChangesetPreviewListsItems(t *testing.T) {
	csSvc, wsID, _, runID := changesetHarness(t)
	ctx := context.Background()
	staged, err := csSvc.Stage(ctx, workspace.StageInput{RunID: runID, WorkspaceID: wsID, Source: "test", Items: []workspace.StageItem{
		{Path: "add.txt", Change: m5workspace.ChangeAdd, Content: []byte("A")},
		{Path: "del.txt", Change: m5workspace.ChangeDelete},
	}})
	if err != nil {
		t.Fatal(err)
	}
	cs, items, err := csSvc.Preview(ctx, staged.ID)
	if err != nil {
		t.Fatal(err)
	}
	if cs.State != m5workspace.ChangeSetStaged || cs.Source != "test" || len(items) != 2 {
		t.Fatalf("preview mismatch: %+v %+v", cs, items)
	}
	if items[0].Path != "add.txt" || items[0].Change != m5workspace.ChangeAdd || items[0].Size != 1 {
		t.Fatalf("item0 mismatch: %+v", items[0])
	}
	if items[1].Change != m5workspace.ChangeDelete {
		t.Fatalf("item1 mismatch: %+v", items[1])
	}
}

// TestChangesetApplyCompensation [SEV1 regression]: when a later item's
// write fails, already-applied modify items must be restored from their
// captured rollback blobs (in-memory RollbackRef sync in Apply).
func TestChangesetApplyCompensation(t *testing.T) {
	csSvc, wsID, wsRoot, runID := changesetHarness(t)
	ctx := context.Background()
	// seed a.txt with known bytes; "blocker" is a regular file so writing
	// blocker/new.txt fails (its parent path is not a directory).
	if err := os.WriteFile(filepath.Join(wsRoot, "a.txt"), []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(wsRoot, "blocker"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	staged, err := csSvc.Stage(ctx, workspace.StageInput{RunID: runID, WorkspaceID: wsID, Source: "agent", Items: []workspace.StageItem{
		{Path: "a.txt", Change: m5workspace.ChangeModify, Content: []byte("new")},
		{Path: "blocker/new.txt", Change: m5workspace.ChangeAdd, Content: []byte("n/a")},
	}})
	if err != nil {
		t.Fatal(err)
	}
	// apply order is path-sorted: a.txt lands first, blocker/new.txt fails.
	if _, err := csSvc.Apply(ctx, staged.ID); !errors.Is(err, workspace.ErrChangeApplyFailed) {
		t.Fatalf("apply want ErrChangeApplyFailed, got %v", err)
	}
	got, rerr := os.ReadFile(filepath.Join(wsRoot, "a.txt"))
	if rerr != nil {
		t.Fatal(rerr)
	}
	if string(got) != "old" {
		t.Fatalf("compensated a.txt want %q, got %q", "old", string(got))
	}
}
