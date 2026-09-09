package officeapp

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	domain "github.com/lunitide/lunitide/internal/domain/officestudio"
	content "github.com/lunitide/lunitide/internal/officestudio"
	"github.com/oklog/ulid/v2"
)

func (s *Service) storageNow() time.Time {
	if s.storageClock != nil {
		return s.storageClock().UTC()
	}
	return time.Now().UTC()
}

func (s *Service) storagePolicy() (domain.StoragePolicy, error) {
	p := s.StoragePolicy
	defaults := domain.DefaultStoragePolicy()
	if p.MaxBytes == 0 {
		p.MaxBytes = defaults.MaxBytes
	}
	if p.Grace == 0 {
		p.Grace = defaults.Grace
	}
	if p.LeaseTTL == 0 {
		p.LeaseTTL = defaults.LeaseTTL
	}
	if p.MaxBytes < 1 || p.Grace < 0 || p.LeaseTTL < time.Second || p.LeaseTTL > 24*time.Hour {
		return p, domain.ErrInvalid
	}
	return p, nil
}

func (s *Service) blobStore() (domain.BlobStore, error) {
	store, ok := s.Store.(domain.BlobStore)
	if !ok {
		return nil, errors.New("办公文件存储管理尚未初始化")
	}
	return store, nil
}

func (s *Service) managedRoot() (*os.Root, error) {
	p := filepath.Join(s.Root, "blobs")
	info, err := os.Lstat(p)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return nil, errors.New("办公受管目录已改变，停止文件写入与清理")
	}
	return os.OpenRoot(p)
}

func (s *Service) storageInventory() ([]domain.BlobFile, error) {
	r, err := s.managedRoot()
	if err != nil {
		return nil, err
	}
	defer r.Close()
	f, err := r.Open(".")
	if err != nil {
		return nil, err
	}
	defer f.Close()
	files := []domain.BlobFile{}
	for {
		entries, e := f.ReadDir(256)
		for _, entry := range entries {
			if len(files) >= 100000 {
				return nil, errors.New("办公受管目录文件数超过扫描上限，未改动已有内容")
			}
			info, e := entry.Info()
			if e != nil {
				return nil, e
			}
			// Do not descend into directories or follow filesystem links.
			if !info.Mode().IsRegular() {
				continue
			}
			item := domain.BlobFile{Name: entry.Name(), Size: info.Size()}
			if validBlobRef(entry.Name()) {
				item.Digest = entry.Name()
			}
			files = append(files, item)
		}
		if errors.Is(e, io.EOF) {
			break
		}
		if e != nil {
			return nil, e
		}
	}
	return files, nil
}

func (s *Service) prepareStorage(ctx context.Context, refresh bool) (domain.BlobStore, error) {
	store, err := s.blobStore()
	if err != nil {
		return nil, err
	}
	s.storageInitMu.Lock()
	defer s.storageInitMu.Unlock()
	if !s.storageReady || refresh {
		if err = store.ReconcileOfficeStorage(ctx, s.storageInventory); err != nil {
			return nil, err
		}
		s.storageReady = true
	}
	return store, nil
}

func validBlobRef(ref string) bool {
	if len(ref) != 64 || strings.ToLower(ref) != ref {
		return false
	}
	_, err := hex.DecodeString(ref)
	return err == nil
}

func (s *Service) putManaged(ctx context.Context, taskID string, data []byte) (domain.BlobLease, error) {
	var lease domain.BlobLease
	if err := ctx.Err(); err != nil {
		return lease, err
	}
	if len(data) == 0 || len(data) > content.MaxInputBytes {
		return lease, domain.ErrInvalid
	}
	p, err := s.storagePolicy()
	if err != nil {
		return lease, err
	}
	store, err := s.prepareStorage(ctx, false)
	if err != nil {
		return lease, err
	}
	now := s.storageNow()
	// Bounded opportunistic maintenance needs no background process and never
	// shortens retention. Explicit sweep still reports any cleanup failures.
	s.storageInitMu.Lock()
	due := s.storageSweepAt.IsZero() || now.Sub(s.storageSweepAt) >= time.Hour
	if due {
		s.storageSweepAt = now
	}
	s.storageInitMu.Unlock()
	if due {
		_, _ = store.SweepOfficeStorage(ctx, domain.StorageSweepOptions{Limit: 25}, now, p.Grace, s.removeManagedBlob)
	}
	lease, err = store.ReserveOfficeBlob(ctx, domain.BlobReservation{TaskID: taskID, Digest: digest(data), Size: int64(len(data)), MaxBytes: p.MaxBytes, Now: now, ExpiresAt: now.Add(p.LeaseTTL)})
	if err != nil {
		return lease, err
	}
	err = store.WriteOfficeBlob(ctx, lease.ID, s.storageNow(), func(l domain.BlobLease) error {
		if l.Digest != digest(data) || l.Size != int64(len(data)) {
			return domain.ErrConflict
		}
		write := s.writeBlob
		if s.storageWrite != nil {
			write = s.storageWrite
		}
		ref, e := write(data, l.StageName)
		if e == nil && ref != l.Digest {
			return domain.ErrConflict
		}
		return e
	})
	if err != nil {
		s.releaseBlob(ctx, lease.ID)
		return lease, err
	}
	return lease, nil
}

// A committed binding consumes its lease in SQLite. Remaining failed attempts
// release it here; cleanup is still bounded even if the caller was cancelled.
func (s *Service) releaseBlob(ctx context.Context, id string) {
	if id == "" {
		return
	}
	store, err := s.blobStore()
	if err != nil {
		return
	}
	cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	_ = store.ReleaseOfficeBlob(cleanup, id, s.storageNow(), s.removeManagedBlob)
}

func (s *Service) StorageUsage(ctx context.Context) (domain.StorageUsage, error) {
	var usage domain.StorageUsage
	p, err := s.storagePolicy()
	if err != nil {
		return usage, err
	}
	store, err := s.prepareStorage(ctx, true)
	if err != nil {
		return usage, err
	}
	usage, err = store.OfficeStorageUsage(ctx, s.storageNow())
	usage.LimitBytes = p.MaxBytes
	usage.OverLimit = usage.TotalBytes >= p.MaxBytes
	return usage, err
}

func (s *Service) SweepStorage(ctx context.Context, opts domain.StorageSweepOptions) (domain.StorageSweepReport, error) {
	p, err := s.storagePolicy()
	if err != nil {
		return domain.StorageSweepReport{}, err
	}
	store, err := s.prepareStorage(ctx, true)
	if err != nil {
		return domain.StorageSweepReport{}, err
	}
	return store.SweepOfficeStorage(ctx, opts, s.storageNow(), p.Grace, s.removeManagedBlob)
}

func (s *Service) removeManagedBlob(blob domain.BlobFile, stages []string) (int64, error) {
	if !validBlobRef(blob.Digest) {
		return 0, domain.ErrInvalid
	}
	r, err := s.managedRoot()
	if err != nil {
		return 0, err
	}
	defer r.Close()
	// Validate all targets before removing any. Corrupt/replaced objects remain
	// available for investigation; no guessed path or unregistered name is used.
	names := append([]string{}, stages...)
	if !blob.StageOnly {
		names = append(names, blob.Digest)
	}
	var freed int64
	for _, name := range names {
		if name != blob.Digest {
			if !strings.HasPrefix(name, "stage-") || len(name) != 32 {
				return 0, domain.ErrInvalid
			}
			if _, err = ulid.ParseStrict(strings.TrimPrefix(name, "stage-")); err != nil {
				return 0, domain.ErrInvalid
			}
		}
		st, e := r.Lstat(name)
		if errors.Is(e, os.ErrNotExist) {
			continue
		}
		if e != nil {
			return 0, e
		}
		if !st.Mode().IsRegular() {
			return 0, errors.New("受管文件已被替换为链接或目录，未清理")
		}
		if name == blob.Digest {
			f, e := r.Open(name)
			if e != nil {
				return 0, e
			}
			b, e := io.ReadAll(io.LimitReader(f, content.MaxInputBytes+1))
			closeErr := f.Close()
			if e != nil {
				return 0, e
			}
			if closeErr != nil {
				return 0, closeErr
			}
			if digest(b) != blob.Digest || int64(len(b)) != blob.Size {
				return 0, errors.New("受管文件摘要或长度不匹配，未清理")
			}
		}
	}
	for _, name := range names {
		st, e := r.Lstat(name)
		if errors.Is(e, os.ErrNotExist) {
			continue
		}
		if e != nil {
			return freed, e
		}
		if !st.Mode().IsRegular() {
			return freed, errors.New("清理期间文件类型改变")
		}
		if e = r.Remove(name); e != nil && !errors.Is(e, os.ErrNotExist) {
			return freed, e
		}
		if e == nil {
			freed += st.Size()
		}
	}
	return freed, nil
}

// writeBlob is called only while the persisted lease and SQLite writer lock
// are held. New bytes reach an immutable digest through an atomic hard link;
// a concurrently appearing destination is verified and never overwritten.
func (s *Service) writeBlob(data []byte, stage string) (string, error) {
	key := digest(data)
	if !strings.HasPrefix(stage, "stage-") || len(stage) != 32 {
		return "", domain.ErrInvalid
	}
	r, err := s.managedRoot()
	if err != nil {
		return "", err
	}
	defer r.Close()
	verify := func() error {
		st, e := r.Lstat(key)
		if e != nil {
			return e
		}
		if !st.Mode().IsRegular() {
			return errors.New("存档路径不是普通文件")
		}
		f, e := r.Open(key)
		if e != nil {
			return e
		}
		defer f.Close()
		old, e := io.ReadAll(io.LimitReader(f, int64(len(data))+1))
		if e != nil {
			return e
		}
		if digest(old) != key {
			return errors.New("存档摘要不一致，已停止发布")
		}
		return nil
	}
	if err = verify(); err == nil {
		return key, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", err
	}
	f, err := r.OpenFile(stage, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return "", err
	}
	defer func() { _ = r.Remove(stage) }()
	_, err = f.Write(data)
	if err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		return "", err
	}
	if err = r.Link(stage, key); err != nil {
		if !errors.Is(err, os.ErrExist) {
			return "", fmt.Errorf("无法原子发布办公文件：%w", err)
		}
		if err = verify(); err != nil {
			return "", err
		}
	}
	return key, nil
}
