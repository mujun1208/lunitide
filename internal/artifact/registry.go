// Package artifact implements the M5 T-5.2.5 artifact registry:
// registration pins MIME/size/SHA-256/generator and the frozen
// blocked -> allowed -> downloaded lifecycle. Reads re-verify the content
// digest; any tamper, oversize or masked-executable refusal maps onto the
// stable ART-001 wire error family.
package artifact

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"
	"time"

	"github.com/lunitide/lunitide/internal/agentrunapp"
	"github.com/lunitide/lunitide/internal/domain/m5workspace"
	"github.com/lunitide/lunitide/internal/workspace"
	"github.com/oklog/ulid/v2"
)

// MaxArtifactBytes is the M5 artifact ceiling (64 MiB).
const MaxArtifactBytes = 64 << 20

// PreviewableMimes is the prefix allowlist for in-product preview;
// anything else (including every executable shape) is download-only.
var previewable = map[string]bool{
	"text/plain": true, "text/markdown": true, "text/html": true,
	"application/json": true, "image/png": true, "image/jpeg": true,
	"image/gif": true, "image/webp": true, "image/svg+xml": true,
}

type artStore interface {
	PutM5Artifact(m5workspace.Artifact) error
	GetM5Artifact(id string) (m5workspace.Artifact, error)
	ListM5ArtifactsByRun(runID string) ([]m5workspace.Artifact, error)
	TransitionM5ArtifactDownload(id string, from, to m5workspace.DownloadState) (m5workspace.Artifact, error)
}

type Clock interface{ Now() time.Time }

type systemClock struct{}

func (systemClock) Now() time.Time { return time.Now().UTC() }

// Registry owns the m5_artifact aggregate; blobs live in the CAS store so
// content is immutable and addressable by the registered digest.
type Registry struct {
	uow   agentrunapp.UnitOfWork
	cas   *workspace.CASStore
	clock Clock
}

func NewRegistry(uow agentrunapp.UnitOfWork, cas *workspace.CASStore) *Registry {
	return &Registry{uow: uow, cas: cas, clock: systemClock{}}
}

func (r *Registry) SetClock(c Clock) { r.clock = c }

func (r *Registry) store(tx agentrunapp.Tx) (artStore, error) {
	st, ok := tx.(artStore)
	if !ok {
		return nil, errors.New("artifact: unit of work unavailable")
	}
	return st, nil
}

// validMime enforces the token/token shape and the subtype charset.
func validMime(mime string) bool {
	if len(mime) == 0 || len(mime) > 256 || strings.ContainsAny(mime, " \t\r\n") {
		return false
	}
	slash := strings.IndexByte(mime, '/')
	if slash <= 0 || slash == len(mime)-1 || strings.IndexByte(mime[slash+1:], '/') >= 0 {
		return false
	}
	main, sub := mime[:slash], mime[slash+1:]
	if main != strings.ToLower(main) || sub != strings.ToLower(sub) {
		return false
	}
	for _, part := range []string{main, sub} {
		for _, c := range part {
			if !(c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '-' || c == '+' || c == '.') {
				return false
			}
		}
	}
	return true
}

// masksExecutable inspects content magic bytes: a PE/MZ, ELF or shell-script
// prologue under a benign declared MIME is a masked executable (ART-001).
func masksExecutable(mime string, data []byte) bool {
	switch mime {
	case "application/x-msdownload", "application/x-executable", "application/x-dosexec",
		"application/x-sharedlib", "application/x-shellscript", "application/x-bat":
		return true
	}
	if len(data) >= 2 && data[0] == 'M' && data[1] == 'Z' {
		return true // PE executable prologue
	}
	if len(data) >= 4 && data[1] == 'E' && data[2] == 'L' && data[3] == 'F' {
		return true // ELF
	}
	if len(data) >= 2 && data[0] == '#' && data[1] == '!' {
		return true // shebang script
	}
	return false
}

// Register validates and journals an artifact; the bytes land in CAS and the
// row starts blocked. Tamper/oversize/masked-MIME refusals answer ART-001.
func (r *Registry) Register(ctx context.Context, runID, mime, generator string, data []byte) (m5workspace.Artifact, error) {
	if runID == "" || generator == "" {
		return m5workspace.Artifact{}, errors.New("artifact: runId and generator are required")
	}
	if !validMime(mime) {
		return m5workspace.Artifact{}, m5workspace.ErrArtifactMime
	}
	if int64(len(data)) > MaxArtifactBytes {
		return m5workspace.Artifact{}, m5workspace.ErrArtifactTooLarge
	}
	if masksExecutable(mime, data) {
		return m5workspace.Artifact{}, m5workspace.ErrArtifactMime
	}
	sum := sha256.Sum256(data)
	ref, err := r.cas.Put(data)
	if err != nil {
		return m5workspace.Artifact{}, err
	}
	// The CAS ref is the content sha256; assert rather than trust.
	if ref != hex.EncodeToString(sum[:]) {
		return m5workspace.Artifact{}, m5workspace.ErrArtifactTampered
	}
	a := m5workspace.Artifact{
		ID: ulid.Make().String(), RunID: runID, Mime: mime,
		Size: int64(len(data)), SHA256: ref,
		Generator: generator, DownloadState: m5workspace.DownloadBlocked,
		CreatedAt: r.clock.Now().UTC(),
	}
	err = r.uow.TransactAgentRuntime(ctx, func(tx agentrunapp.Tx) error {
		st, err := r.store(tx)
		if err != nil {
			return err
		}
		return st.PutM5Artifact(a)
	})
	if err != nil {
		return m5workspace.Artifact{}, err
	}
	return a, nil
}

// Get returns the artifact row.
func (r *Registry) Get(ctx context.Context, id string) (m5workspace.Artifact, error) {
	var out m5workspace.Artifact
	err := r.uow.TransactAgentRuntime(ctx, func(tx agentrunapp.Tx) error {
		st, err := r.store(tx)
		if err != nil {
			return err
		}
		out, err = st.GetM5Artifact(id)
		return err
	})
	return out, err
}

// ListByRun returns the run's artifacts oldest-first.
func (r *Registry) ListByRun(ctx context.Context, runID string) ([]m5workspace.Artifact, error) {
	var out []m5workspace.Artifact
	err := r.uow.TransactAgentRuntime(ctx, func(tx agentrunapp.Tx) error {
		st, err := r.store(tx)
		if err != nil {
			return err
		}
		out, err = st.ListM5ArtifactsByRun(runID)
		return err
	})
	return out, err
}

// AllowDownload records the explicit user confirmation (blocked -> allowed).
func (r *Registry) AllowDownload(ctx context.Context, id string) (m5workspace.Artifact, error) {
	return r.transition(ctx, id, m5workspace.DownloadBlocked, m5workspace.DownloadAllowed)
}

// MarkDownloaded seals the lifecycle (allowed -> downloaded).
func (r *Registry) MarkDownloaded(ctx context.Context, id string) (m5workspace.Artifact, error) {
	return r.transition(ctx, id, m5workspace.DownloadAllowed, m5workspace.DownloadDownloaded)
}

func (r *Registry) transition(ctx context.Context, id string, from, to m5workspace.DownloadState) (m5workspace.Artifact, error) {
	var out m5workspace.Artifact
	err := r.uow.TransactAgentRuntime(ctx, func(tx agentrunapp.Tx) error {
		st, err := r.store(tx)
		if err != nil {
			return err
		}
		out, err = st.TransitionM5ArtifactDownload(id, from, to)
		return err
	})
	return out, err
}

// Content re-verifies the digest before returning bytes; a mismatch is the
// ART-001 tamper refusal.
func (r *Registry) Content(ctx context.Context, id string) (m5workspace.Artifact, []byte, error) {
	a, err := r.Get(ctx, id)
	if err != nil {
		return a, nil, err
	}
	data, err := r.cas.Get(a.SHA256)
	if err != nil {
		return a, nil, m5workspace.ErrArtifactTampered
	}
	sum := sha256.Sum256(data)
	if hex.EncodeToString(sum[:]) != a.SHA256 || int64(len(data)) != a.Size {
		return a, nil, m5workspace.ErrArtifactTampered
	}
	return a, data, nil
}

// CanPreview reports whether the mime family may render in-product;
// non-previewable types are download-only per M5-FR-07.
func CanPreview(mime string) bool { return previewable[mime] }
