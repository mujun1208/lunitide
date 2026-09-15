package agentrunapp_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lunitide/lunitide/internal/doctext"
	"github.com/lunitide/lunitide/internal/officetools"
	"github.com/lunitide/lunitide/internal/toolruntime"
	"github.com/lunitide/lunitide/internal/workspace"
)

const artifactSnapshotSession = "01ARZ3NDEKTSV4RRFFQ69G5FAV"

func sha256Hex(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func TestArtifactSnapshotRawTail(t *testing.T) {
	ctx := context.Background()
	casRoot := filepath.Join(t.TempDir(), "cas")
	cas, err := workspace.NewCASStore(casRoot)
	if err != nil {
		t.Fatal(err)
	}
	rt, err := toolruntime.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = rt.Close() })
	rt.SetWorkspaceCAS(cas)

	folder, err := rt.SessionFolder(artifactSnapshotSession)
	if err != nil {
		t.Fatal(err)
	}

	const size = 1 << 20
	raw := bytes.Repeat([]byte{'A'}, size)
	// Trailing whitespace a paginated/extract path would drop; first 3200
	// bytes stay identical across the tail mutation below.
	copy(raw[size-4:], []byte("  \n "))
	if err := os.WriteFile(filepath.Join(folder, "payload.txt"), raw, 0600); err != nil {
		t.Fatal(err)
	}

	first, err := rt.SnapshotWorkspaceArtifact(ctx, toolruntime.AutoEdit, artifactSnapshotSession, "payload.txt", false)
	if err != nil {
		t.Fatal(err)
	}
	wantFirst := sha256Hex(raw)
	if first.SHA256 != wantFirst {
		t.Fatalf("workspace snapshot SHA %s want raw-file %s", first.SHA256, wantFirst)
	}
	if first.Bytes != int64(len(raw)) {
		t.Fatalf("workspace snapshot bytes %d want %d", first.Bytes, len(raw))
	}
	pageSHA := sha256Hex(raw[:3200])
	if first.SHA256 == pageSHA {
		t.Fatal("workspace snapshot SHA reused paginated first-page bytes as the file")
	}

	raw[900000] = 'Z'
	if !bytes.Equal(raw[:3200], bytes.Repeat([]byte{'A'}, 3200)) {
		t.Fatal("fixture setup changed the first 3200 bytes")
	}
	if err := os.WriteFile(filepath.Join(folder, "payload.txt"), raw, 0600); err != nil {
		t.Fatal(err)
	}
	second, err := rt.SnapshotWorkspaceArtifact(ctx, toolruntime.AutoEdit, artifactSnapshotSession, "payload.txt", false)
	if err != nil {
		t.Fatal(err)
	}
	wantSecond := sha256Hex(raw)
	if second.SHA256 != wantSecond {
		t.Fatalf("mutated tail snapshot SHA %s want raw-file %s", second.SHA256, wantSecond)
	}
	if second.SHA256 == first.SHA256 {
		t.Fatal("byte 900000 changed but snapshot SHA stayed the same (hashed a prefix, not the raw tail)")
	}
	if second.SHA256 == pageSHA {
		t.Fatal("mutated snapshot SHA reused paginated first-page bytes as the file")
	}

	resolved, err := rt.ResolveWorkspaceArtifact(ctx, second.ContentRef)
	if err != nil {
		t.Fatal(err)
	}
	if resolved.SHA256 != wantSecond || resolved.Bytes != int64(len(raw)) {
		t.Fatalf("resolve after snapshot: sha=%s bytes=%d", resolved.SHA256, resolved.Bytes)
	}

	restart, err := toolruntime.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = restart.Close() })
	restart.SetWorkspaceCAS(cas)
	afterRestart, err := restart.ResolveWorkspaceArtifact(ctx, second.ContentRef)
	if err != nil {
		t.Fatal(err)
	}
	if afterRestart.SHA256 != wantSecond {
		t.Fatalf("restart resolve SHA %s want %s", afterRestart.SHA256, wantSecond)
	}

	casPath := filepath.Join(casRoot, second.ContentRef[:2], second.ContentRef)
	if err := os.WriteFile(casPath, []byte("broken-same-name-cas-blob"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := restart.ResolveWorkspaceArtifact(ctx, second.ContentRef); err == nil {
		t.Fatal("resolver accepted a destroyed same-name CAS blob")
	}

	prose := strings.Repeat("办公原文与提取文本必须分开计算摘要。", 80) + "   "
	docx, err := officetools.GenDocx("快照原文", []officetools.DocxBlock{
		{Type: "heading", Text: "标题"},
		{Type: "paragraph", Text: prose},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(folder, "note.docx"), docx, 0600); err != nil {
		t.Fatal(err)
	}
	officeSnap, err := rt.SnapshotWorkspaceArtifact(ctx, toolruntime.AutoEdit, artifactSnapshotSession, "note.docx", false)
	if err != nil {
		t.Fatal(err)
	}
	extracted, err := doctext.Extract("note.docx", docx, "")
	if err != nil {
		t.Fatal(err)
	}
	extractSHA := sha256Hex([]byte(extracted.Text))
	if officeSnap.SHA256 != sha256Hex(docx) {
		t.Fatalf("office workspace snapshot SHA %s want raw blob %s", officeSnap.SHA256, sha256Hex(docx))
	}
	if officeSnap.SHA256 == extractSHA {
		t.Fatal("office workspace snapshot reused extract text SHA as the file digest")
	}
}
