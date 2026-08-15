package artifact_test

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/lunitide/lunitide/internal/artifact"
	"github.com/lunitide/lunitide/internal/domain/m5workspace"
	"github.com/lunitide/lunitide/internal/workspace"
)

func registryHarness(t *testing.T) (*artifact.Registry, string) {
	t.Helper()
	// Reuse the storage harness shape from the workspace package tests via a
	// minimal local open so this package stays independent.
	store, err := openTestStore(t)
	if err != nil {
		t.Fatal(err)
	}
	runID, err := seedRun(t, store)
	if err != nil {
		t.Fatal(err)
	}
	tmp := t.TempDir()
	cas, err := workspace.NewCASStore(filepath.Join(tmp, "cas"))
	if err != nil {
		t.Fatal(err)
	}
	casRoot = filepath.Join(tmp, "cas")
	return artifact.NewRegistry(store.AgentRuntimeRepository(), cas), runID
}

// TestArtifactRegister: happy-path registration pins the digest, starts
// blocked, and Content returns byte-identical data.
func TestArtifactRegister(t *testing.T) {
	reg, runID := registryHarness(t)
	ctx := context.Background()
	body := []byte("# hello\nartifact body\n")
	a, err := reg.Register(ctx, runID, "text/markdown", "agent", body)
	if err != nil {
		t.Fatal(err)
	}
	if a.DownloadState != m5workspace.DownloadBlocked || a.Size != int64(len(body)) || a.Generator != "agent" {
		t.Fatalf("registration fields wrong: %+v", a)
	}
	if !artifact.CanPreview(a.Mime) {
		t.Fatalf("markdown must be previewable")
	}
	got, data, err := reg.Content(ctx, a.ID)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != string(body) || got.ID != a.ID {
		t.Fatalf("content roundtrip mismatch")
	}
	list, err := reg.ListByRun(ctx, runID)
	if err != nil || len(list) != 1 || list[0].ID != a.ID {
		t.Fatalf("list by run wrong: %v %v", list, err)
	}
}

// TestArtifactLifecycle: blocked -> allowed -> downloaded is the only line;
// skips and reversals are refused.
func TestArtifactLifecycle(t *testing.T) {
	reg, runID := registryHarness(t)
	ctx := context.Background()
	a, err := reg.Register(ctx, runID, "application/zip", "agent", []byte("PK\x03\x04zip"))
	if err != nil {
		t.Fatal(err)
	}
	if artifact.CanPreview(a.Mime) {
		t.Fatalf("zip must be download-only")
	}
	// Skipping the confirmation is refused.
	if _, err := reg.MarkDownloaded(ctx, a.ID); !errors.Is(err, m5workspace.ErrArtifactStateBad) {
		t.Fatalf("blocked -> downloaded must be refused, got %v", err)
	}
	a, err = reg.AllowDownload(ctx, a.ID)
	if err != nil || a.DownloadState != m5workspace.DownloadAllowed {
		t.Fatalf("allow download failed: %v %+v", err, a)
	}
	// Re-allowing is refused (already allowed).
	if _, err := reg.AllowDownload(ctx, a.ID); !errors.Is(err, m5workspace.ErrArtifactStateBad) {
		t.Fatalf("re-allow must be refused, got %v", err)
	}
	a, err = reg.MarkDownloaded(ctx, a.ID)
	if err != nil || a.DownloadState != m5workspace.DownloadDownloaded {
		t.Fatalf("mark downloaded failed: %v %+v", err, a)
	}
	// Terminal: nothing further.
	if _, err := reg.AllowDownload(ctx, a.ID); !errors.Is(err, m5workspace.ErrArtifactStateBad) {
		t.Fatalf("downloaded -> allowed must be refused, got %v", err)
	}
}

// TestArtifactRegisterRefusals: oversize, malformed mime and masked
// executables all answer the ART-001 family without persisting a row.
func TestArtifactRegisterRefusals(t *testing.T) {
	reg, runID := registryHarness(t)
	ctx := context.Background()
	big := make([]byte, artifact.MaxArtifactBytes+1)
	if _, err := reg.Register(ctx, runID, "application/octet-stream", "agent", big); !errors.Is(err, m5workspace.ErrArtifactTooLarge) {
		t.Fatalf("oversize must be refused, got %v", err)
	}
	for _, mime := range []string{"", "text", "text/plain extra", "TEXT/PLAIN", "a/b/c"} {
		if _, err := reg.Register(ctx, runID, mime, "agent", []byte("x")); !errors.Is(err, m5workspace.ErrArtifactMime) {
			t.Fatalf("mime %q must be refused, got %v", mime, err)
		}
	}
	// Masked executable: benign mime, PE prologue.
	if _, err := reg.Register(ctx, runID, "text/plain", "agent", []byte("MZ\x90\x00pe-payload")); !errors.Is(err, m5workspace.ErrArtifactMime) {
		t.Fatalf("masked PE must be refused, got %v", err)
	}
	// Explicit executable mime is refused outright.
	if _, err := reg.Register(ctx, runID, "application/x-msdownload", "agent", []byte("MZxx")); !errors.Is(err, m5workspace.ErrArtifactMime) {
		t.Fatalf("executable mime must be refused, got %v", err)
	}
	list, err := reg.ListByRun(ctx, runID)
	if err != nil || len(list) != 0 {
		t.Fatalf("refusals must not persist rows: %v %v", list, err)
	}
}

// TestArtifactTamperDetected: replacing the CAS blob with different bytes
// makes Content answer the tamper refusal.
func TestArtifactTamperDetected(t *testing.T) {
	reg, runID := registryHarness(t)
	ctx := context.Background()
	a, err := reg.Register(ctx, runID, "text/plain", "agent", []byte("original"))
	if err != nil {
		t.Fatal(err)
	}
	// Overwrite the CAS blob behind the registry's back.
	if err := tamperCAS(t, a.SHA256, []byte("swapped!")); err != nil {
		t.Fatal(err)
	}
	if _, _, err := reg.Content(ctx, a.ID); !errors.Is(err, m5workspace.ErrArtifactTampered) {
		t.Fatalf("tampered content must be refused, got %v", err)
	}
}
