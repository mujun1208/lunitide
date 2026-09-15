package toolruntime

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/lunitide/lunitide/internal/domain/agentrun"
	"github.com/lunitide/lunitide/internal/workspace"
)

var (
	// ErrArtifactChanged is returned when the source file still mutates after one retry.
	ErrArtifactChanged = errors.New("ARTIFACT_CHANGED")
	// ErrTaskEvidenceInvalid is returned when a CAS blob is missing or its bytes no longer match the ref.
	ErrTaskEvidenceInvalid = errors.New("TASK_EVIDENCE_INVALID")
)

// SetWorkspaceCAS injects the existing workspace CAS used for raw-file snapshots.
func (r *Runtime) SetWorkspaceCAS(cas *workspace.CASStore) {
	if r != nil {
		r.artifactCAS = cas
	}
}

func (r *Runtime) snapshotBlobs() (interface {
	Put([]byte) (string, error)
	Get(string) ([]byte, error)
}, error) {
	if r.artifactCAS != nil {
		return r.artifactCAS, nil
	}
	cas, err := workspace.NewCASStore(filepath.Join(r.root, "artifact-cas"))
	if err != nil {
		return nil, err
	}
	r.artifactCAS = cas
	return cas, nil
}

// SnapshotWorkspaceArtifact reads the authorized raw file (not workspace.read
// pages or document extract), hashes the complete bytes, and stores them in CAS.
func (r *Runtime) SnapshotWorkspaceArtifact(ctx context.Context, mode Mode, sessionID, path string, unconfined bool) (agentrun.ArtifactSnapshot, error) {
	if err := ctx.Err(); err != nil {
		return agentrun.ArtifactSnapshot{}, err
	}
	abs, err := r.path(mode, sessionID, path, false, unconfined)
	if err != nil {
		return agentrun.ArtifactSnapshot{}, err
	}
	raw, ident, err := readRawArtifact(abs)
	if errors.Is(err, ErrArtifactChanged) {
		raw, ident, err = readRawArtifact(abs)
	}
	if err != nil {
		return agentrun.ArtifactSnapshot{}, err
	}
	if err := ctx.Err(); err != nil {
		return agentrun.ArtifactSnapshot{}, err
	}
	cas, err := r.snapshotBlobs()
	if err != nil {
		return agentrun.ArtifactSnapshot{}, err
	}
	ref, err := cas.Put(raw)
	if err != nil {
		return agentrun.ArtifactSnapshot{}, err
	}
	sum := sha256.Sum256(raw)
	sha := hex.EncodeToString(sum[:])
	if ref != sha {
		return agentrun.ArtifactSnapshot{}, fmt.Errorf("%w: cas ref %s != raw sha %s", ErrTaskEvidenceInvalid, ref, sha)
	}
	return agentrun.ArtifactSnapshot{
		ID:             sha,
		Path:           filepath.ToSlash(path),
		SHA256:         sha,
		ContentRef:     sha,
		Bytes:          int64(len(raw)),
		SourceIdentity: ident,
	}, nil
}

// ResolveWorkspaceArtifact reloads a snapshot blob and rejects a missing or
// same-name CAS object whose bytes no longer match the content ref.
func (r *Runtime) ResolveWorkspaceArtifact(ctx context.Context, contentRef string) (agentrun.ArtifactSnapshot, error) {
	if err := ctx.Err(); err != nil {
		return agentrun.ArtifactSnapshot{}, err
	}
	ref := strings.TrimPrefix(strings.TrimSpace(contentRef), "workspace-cas:")
	cas, err := r.snapshotBlobs()
	if err != nil {
		return agentrun.ArtifactSnapshot{}, err
	}
	raw, err := cas.Get(ref)
	if err != nil {
		return agentrun.ArtifactSnapshot{}, fmt.Errorf("%w: %v", ErrTaskEvidenceInvalid, err)
	}
	sum := sha256.Sum256(raw)
	sha := hex.EncodeToString(sum[:])
	if sha != ref {
		return agentrun.ArtifactSnapshot{}, fmt.Errorf("%w: blob sha %s != ref %s", ErrTaskEvidenceInvalid, sha, ref)
	}
	return agentrun.ArtifactSnapshot{
		ID:         sha,
		SHA256:     sha,
		ContentRef: sha,
		Bytes:      int64(len(raw)),
	}, nil
}

func readRawArtifact(abs string) ([]byte, string, error) {
	before, err := os.Lstat(abs)
	if err != nil {
		return nil, "", err
	}
	if !before.Mode().IsRegular() || before.Size() > maxGeneratedBytes {
		return nil, "", errors.New("文件不存在或超过 8 MiB 文档上限")
	}
	f, err := os.Open(abs)
	if err != nil {
		return nil, "", err
	}
	defer f.Close()
	raw, err := io.ReadAll(io.LimitReader(f, maxGeneratedBytes+1))
	if err != nil {
		return nil, "", err
	}
	if int64(len(raw)) > maxGeneratedBytes {
		return nil, "", errors.New("文件超过 8 MiB 文档上限")
	}
	after, err := os.Lstat(abs)
	if err != nil {
		return nil, "", err
	}
	if !sameRawIdentity(before, after) || after.Size() != int64(len(raw)) {
		return nil, "", ErrArtifactChanged
	}
	ident := fmt.Sprintf("%d:%s", after.Size(), after.ModTime().UTC().Format(time.RFC3339Nano))
	return raw, ident, nil
}

func sameRawIdentity(a, b os.FileInfo) bool {
	return a.Size() == b.Size() && a.ModTime().Equal(b.ModTime()) && a.Mode().IsRegular() && b.Mode().IsRegular()
}
