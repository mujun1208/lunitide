// archive.pack (M7 slice 7, P2 write-effect): bundles workspace-confined
// sources into one bounded zip/tar archive written back into the workspace.
// Guards mirror unpack: per-source confinement, symlink refusal, entry-count
// and total-size double caps, and runId+idempotencyKey replay.
package m7app

import (
	"archive/tar"
	"archive/zip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/lunitide/lunitide/internal/domain/m7flow"
)

// ArchivePackInput is the archive.pack command.
type ArchivePackInput struct {
	RunID          string
	Sources        []string
	DestPath       string
	WorkspaceRoot  string
	Format         string
	MaxEntries     int64
	MaxBytes       int64
	IdempotencyKey string
}

// PackResult answers the landed archive identity.
type PackResult struct {
	ArchivePath string
	EntryCount  int
	SHA256      string
	Bytes       int64
}

// Pack validates the guards then delegates to packArchive under
// replayOrRun idempotency.
func (s *ToolgapService) Pack(ctx context.Context, in ArchivePackInput) (PackResult, error) {
	switch in.Format {
	case "zip", "tar":
	default:
		return PackResult{}, fmt.Errorf("%w: format %q", ErrToolSchema, in.Format)
	}
	if len(in.Sources) < 1 || len(in.Sources) > 1000 {
		return PackResult{}, fmt.Errorf("%w: sources %d", ErrToolSchema, len(in.Sources))
	}
	if in.MaxEntries < 1 || in.MaxEntries > ToolMaxArchiveEntry {
		return PackResult{}, fmt.Errorf("%w: maxEntries %d", ErrToolSchema, in.MaxEntries)
	}
	if in.MaxBytes < 1 || in.MaxBytes > ToolMaxArchiveBytes {
		return PackResult{}, fmt.Errorf("%w: maxBytes %d", ErrToolSchema, in.MaxBytes)
	}
	dest, err := m7flow.ToolConfinePath(in.WorkspaceRoot, in.DestPath)
	if err != nil {
		return PackResult{}, fmt.Errorf("%w: %v", ErrToolSchema, err)
	}
	roots := make([]string, 0, len(in.Sources))
	for _, src := range in.Sources {
		p, cerr := m7flow.ToolConfinePath(in.WorkspaceRoot, src)
		if cerr != nil {
			return PackResult{}, fmt.Errorf("%w: %v", ErrToolSchema, cerr)
		}
		roots = append(roots, p)
	}
	res, err := s.replayOrRun(ctx, in.RunID, "archive.pack", in.IdempotencyKey, func() (any, error) {
		return packArchive(roots, dest, in.WorkspaceRoot, in.Format, in.MaxEntries, in.MaxBytes)
	})
	if err != nil {
		return PackResult{}, err
	}
	return res.(PackResult), nil
}

// packArchive walks the confined sources and writes the bounded archive
// with a running SHA-256 over the produced bytes.
func packArchive(roots []string, dest, workspaceRoot, format string, maxEntries, maxBytes int64) (PackResult, error) {
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return PackResult{}, err
	}
	f, err := os.Create(dest)
	if err != nil {
		return PackResult{}, err
	}
	defer f.Close()
	h := sha256.New()
	sink := io.MultiWriter(f, h)
	count := 0
	var total int64

	closeAll := func() {
		_ = f.Close()
		_ = os.Remove(dest)
	}
	if format == "zip" {
		zw := zip.NewWriter(sink)
		for _, root := range roots {
			err := filepath.WalkDir(root, func(path string, d os.DirEntry, werr error) error {
				if werr != nil {
					return werr
				}
				if d.Type()&os.ModeSymlink != 0 {
					return fmt.Errorf("%w: symlink source %s", ErrToolSchema, path)
				}
				if d.IsDir() {
					return nil
				}
				info, ierr := d.Info()
				if ierr != nil {
					return ierr
				}
				if int64(count) >= maxEntries {
					return fmt.Errorf("%w: entries over %d", ErrToolSchema, maxEntries)
				}
				rel, rerr := filepath.Rel(workspaceRoot, path)
				if rerr != nil {
					return rerr
				}
				zh, herr := zip.FileInfoHeader(info)
				if herr != nil {
					return herr
				}
				zh.Name = filepath.ToSlash(rel)
				w, cerr := zw.CreateHeader(zh)
				if cerr != nil {
					return cerr
				}
				src, oerr := os.Open(path)
				if oerr != nil {
					return oerr
				}
				n, cperr := io.Copy(w, src)
				src.Close()
				if cperr != nil {
					return cperr
				}
				count++
				total += n
				if total > maxBytes {
					return fmt.Errorf("%w: bytes over %d", ErrToolSchema, maxBytes)
				}
				return nil
			})
			if err != nil {
				zw.Close()
				closeAll()
				return PackResult{}, err
			}
		}
		if err := zw.Close(); err != nil {
			closeAll()
			return PackResult{}, err
		}
	} else {
		tw := tar.NewWriter(sink)
		for _, root := range roots {
			err := filepath.WalkDir(root, func(path string, d os.DirEntry, werr error) error {
				if werr != nil {
					return werr
				}
				if d.Type()&os.ModeSymlink != 0 {
					return fmt.Errorf("%w: symlink source %s", ErrToolSchema, path)
				}
				if d.IsDir() {
					return nil
				}
				info, ierr := d.Info()
				if ierr != nil {
					return ierr
				}
				if int64(count) >= maxEntries {
					return fmt.Errorf("%w: entries over %d", ErrToolSchema, maxEntries)
				}
				rel, rerr := filepath.Rel(workspaceRoot, path)
				if rerr != nil {
					return rerr
				}
				hdr, herr := tar.FileInfoHeader(info, "")
				if herr != nil {
					return herr
				}
				hdr.Name = filepath.ToSlash(rel)
				if herr := tw.WriteHeader(hdr); herr != nil {
					return herr
				}
				src, oerr := os.Open(path)
				if oerr != nil {
					return oerr
				}
				n, cperr := io.Copy(tw, src)
				src.Close()
				if cperr != nil {
					return cperr
				}
				count++
				total += n
				if total > maxBytes {
					return fmt.Errorf("%w: bytes over %d", ErrToolSchema, maxBytes)
				}
				return nil
			})
			if err != nil {
				tw.Close()
				closeAll()
				return PackResult{}, err
			}
		}
		if err := tw.Close(); err != nil {
			closeAll()
			return PackResult{}, err
		}
	}
	st, err := f.Stat()
	if err != nil {
		return PackResult{}, err
	}
	return PackResult{
		ArchivePath: dest, EntryCount: count,
		SHA256: hex.EncodeToString(h.Sum(nil)), Bytes: st.Size(),
	}, nil
}
