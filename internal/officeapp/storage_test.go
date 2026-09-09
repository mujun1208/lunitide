package officeapp

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"sync"
	"testing"
	"time"

	domain "github.com/lunitide/lunitide/internal/domain/officestudio"
	content "github.com/lunitide/lunitide/internal/officestudio"
	"github.com/lunitide/lunitide/internal/storage/sqlite"
)

func TestOfficeStorageQuotaPreservesOriginalReadExportAndRetry(t *testing.T) {
	svc, store, task := studioServiceFixture(t)
	ctx := context.Background()
	data, err := content.Generate(shortWordSpec())
	if err != nil {
		t.Fatal(err)
	}
	svc.StoragePolicy.MaxBytes = int64(len(data))
	v, err := svc.Import(ctx, task.ID, "", "report.docx", data, "", 0, "first")
	if err != nil {
		t.Fatal(err)
	}
	svc.StoragePolicy.MaxBytes = 1 // existing usage above new configured ceiling
	retry, err := svc.Import(ctx, task.ID, "", "report.docx", data, "", 0, "first")
	if err != nil || retry.ID != v.ID {
		t.Fatalf("same digest retry needs no extra capacity: %+v %v", retry, err)
	}
	changed := shortWordSpec()
	changed.Title = "new bytes"
	if _, err = svc.Generate(ctx, task.ID, "second.docx", changed, "second"); !errors.Is(err, domain.ErrStorageQuota) {
		t.Fatalf("over quota generated new bytes: %v", err)
	}
	versions, err := store.ListOfficeVersions(ctx, task.ID, "")
	if err != nil || len(versions) != 1 {
		t.Fatalf("quota failure published phantom file: %d %v", len(versions), err)
	}
	_, actual, err := svc.ReadVersion(ctx, task.ID, v.ID)
	if err != nil || !bytes.Equal(actual, data) {
		t.Fatal("over quota damaged original", err)
	}
	path, err := svc.Export(ctx, task.ID, v.ID, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	exported, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(exported, data) {
		t.Fatal("over quota blocked export", err)
	}
	u, err := svc.StorageUsage(ctx)
	if err != nil || !u.OverLimit || u.ReferencedBytes != int64(len(data)) || u.ActiveLeaseCount != 0 {
		t.Fatalf("usage: %+v %v", u, err)
	}
}

func TestOfficeStorageRealPartialWriteRollbackAndSuccessfulRetry(t *testing.T) {
	svc, store, task := studioServiceFixture(t)
	ctx := context.Background()
	data, err := content.Generate(shortWordSpec())
	if err != nil {
		t.Fatal(err)
	}
	svc.StoragePolicy.MaxBytes = int64(len(data))
	svc.storageWrite = func(b []byte, stage string) (string, error) {
		if err := os.WriteFile(filepath.Join(svc.Root, "blobs", stage), b[:len(b)/2], 0600); err != nil {
			return "", err
		}
		return "", errors.New("injected disk full after physical partial write")
	}
	if _, err = svc.Import(ctx, task.ID, "", "report.docx", data, "", 0, "retry"); err == nil {
		t.Fatal("partial write accepted")
	}
	versions, err := store.ListOfficeVersions(ctx, task.ID, "")
	if err != nil || len(versions) != 0 {
		t.Fatal("partial file got a version", err)
	}
	u, err := svc.StorageUsage(ctx)
	if err != nil || u.TotalBytes != 0 || u.ActiveLeaseCount != 0 {
		t.Fatalf("failed write leaked reservation: %+v %v", u, err)
	}
	files, err := os.ReadDir(filepath.Join(svc.Root, "blobs"))
	if err != nil || len(files) != 0 {
		t.Fatalf("partial file survived normal rollback: %d %v", len(files), err)
	}
	svc.storageWrite = nil
	if _, err = svc.Import(ctx, task.ID, "", "report.docx", data, "", 0, "retry"); err != nil {
		t.Fatalf("failed write could not retry: %v", err)
	}
}

func TestOfficeStorageRealOrphanCrashStageAndUnknownFileRetention(t *testing.T) {
	svc, store, task := studioServiceFixture(t)
	ctx := context.Background()
	now := time.Now().UTC()
	svc.storageClock = func() time.Time { return now }
	svc.StoragePolicy.Grace = time.Hour
	permanent := generatedWord(t, svc, task, "keep")
	if _, err := store.AcceptOfficeVersion(ctx, task.ID, permanent.ArtifactID, permanent.ID, 1); err != nil {
		t.Fatal(err)
	}
	bundle, err := svc.CreateBundle(ctx, task.ID, "fixed", []string{permanent.ID}, "bundle")
	if err != nil {
		t.Fatal(err)
	}
	orphanData := []byte("registered abandoned bytes")
	orphan, err := svc.putManaged(ctx, task.ID, orphanData)
	if err != nil {
		t.Fatal(err)
	}
	svc.releaseBlob(ctx, orphan.ID)
	crashData := []byte("incomplete crash data")
	crash, err := store.ReserveOfficeBlob(ctx, domain.BlobReservation{TaskID: task.ID, Digest: digest(crashData), Size: int64(len(crashData)), MaxBytes: 1 << 20, Now: now, ExpiresAt: now.Add(time.Minute)})
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(svc.Root, "blobs", crash.StageName), crashData[:4], 0600); err != nil {
		t.Fatal(err)
	}
	// This hash-looking pre-existing file has no provenance receipt. Count it,
	// but never adopt it into the automatic deletion set.
	unknown := []byte("user-owned unknown content")
	unknownPath := filepath.Join(svc.Root, "blobs", digest(unknown))
	if err = os.WriteFile(unknownPath, unknown, 0600); err != nil {
		t.Fatal(err)
	}
	now = now.Add(3 * time.Hour)
	dry, err := svc.SweepStorage(ctx, domain.StorageSweepOptions{DryRun: true})
	if err != nil || dry.Candidates != 2 || dry.Removed != 0 {
		t.Fatalf("dry run: %+v %v", dry, err)
	}
	if _, err = os.Stat(filepath.Join(svc.Root, "blobs", orphan.Digest)); err != nil {
		t.Fatal("dry run removed file")
	}
	report, err := svc.SweepStorage(ctx, domain.StorageSweepOptions{})
	if err != nil || report.Removed != 2 || len(report.Errors) != 0 || report.FreedBytes != int64(len(orphanData)+4) {
		t.Fatalf("real cleanup: %+v %v", report, err)
	}
	for _, path := range []string{filepath.Join(svc.Root, "blobs", orphan.Digest), filepath.Join(svc.Root, "blobs", crash.StageName)} {
		if _, err = os.Stat(path); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("owned orphan survived: %s %v", path, err)
		}
	}
	if b, e := os.ReadFile(unknownPath); e != nil || !bytes.Equal(b, unknown) {
		t.Fatal("unknown file deleted", e)
	}
	if _, _, err = svc.ReadVersion(ctx, task.ID, permanent.ID); err != nil {
		t.Fatal("formal version deleted", err)
	}
	out, err := svc.ExportBundle(ctx, task.ID, bundle.ID, t.TempDir())
	if err != nil || !out.Complete {
		t.Fatal("accepted bundle broken", err)
	}
	u, err := svc.StorageUsage(ctx)
	if err != nil || u.ReservedBytes != 0 || u.ActiveLeaseCount != 0 || u.UnmanagedBytes != int64(len(unknown)) {
		t.Fatalf("cleanup accounting: %+v %v", u, err)
	}
}

func TestOfficeStorageParallelServicesSameBlobDoNotDoubleCount(t *testing.T) {
	svc, _, task := studioServiceFixture(t)
	ctx := context.Background()
	// Distinct database handles simulate two engine instances; a process-local
	// mutex or the original Store's single-connection pool cannot pass this test.
	secondStore, err := sqlite.Open(ctx, filepath.Join(filepath.Dir(svc.Root), "studio-test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer secondStore.Close()
	other, err := New(secondStore, svc.Root)
	if err != nil {
		t.Fatal(err)
	}
	data := []byte("both writers must share one immutable blob")
	svc.StoragePolicy.MaxBytes = int64(len(data))
	other.StoragePolicy.MaxBytes = int64(len(data))
	var wg sync.WaitGroup
	errs := make(chan error, 2)
	leases := make(chan domain.BlobLease, 2)
	for _, service := range []*Service{svc, other} {
		wg.Add(1)
		go func(s *Service) { defer wg.Done(); l, e := s.putManaged(ctx, task.ID, data); leases <- l; errs <- e }(service)
	}
	wg.Wait()
	close(errs)
	close(leases)
	for e := range errs {
		if e != nil {
			t.Fatal(e)
		}
	}
	for l := range leases {
		svc.releaseBlob(ctx, l.ID)
	}
	u, err := svc.StorageUsage(ctx)
	if err != nil || u.TotalBytes != int64(len(data)) || u.BlobCount != 1 {
		t.Fatalf("shared physical bytes counted twice: %+v %v", u, err)
	}
	b, err := os.ReadFile(filepath.Join(svc.Root, "blobs", digest(data)))
	if err != nil || !bytes.Equal(b, data) {
		t.Fatal("parallel publication damaged bytes", err)
	}
}

func TestOfficeStorageCollectorRefusesReplacedBlob(t *testing.T) {
	svc, _, task := studioServiceFixture(t)
	ctx := context.Background()
	now := time.Now().UTC()
	svc.storageClock = func() time.Time { return now }
	svc.StoragePolicy.Grace = time.Hour
	l, err := svc.putManaged(ctx, task.ID, []byte("original owned blob"))
	if err != nil {
		t.Fatal(err)
	}
	svc.releaseBlob(ctx, l.ID)
	path := filepath.Join(svc.Root, "blobs", l.Digest)
	if err = os.WriteFile(path, []byte("external replacement"), 0600); err != nil {
		t.Fatal(err)
	}
	now = now.Add(2 * time.Hour)
	r, err := svc.SweepStorage(ctx, domain.StorageSweepOptions{})
	if err != nil || r.Removed != 0 || len(r.Errors) != 1 {
		t.Fatalf("replaced file was deleted: %+v %v", r, err)
	}
	if b, e := os.ReadFile(path); e != nil || string(b) != "external replacement" {
		t.Fatal("external bytes not preserved", e)
	}
}

func TestOfficeStorageRealSweepLostResponseDoesNotDeleteNextBatch(t *testing.T) {
	svc, _, task := studioServiceFixture(t)
	ctx := context.Background()
	now := time.Now().UTC()
	svc.storageClock = func() time.Time { return now }
	svc.StoragePolicy.Grace = time.Hour
	for _, data := range [][]byte{[]byte("orphan first"), []byte("orphan second"), []byte("orphan third")} {
		l, err := svc.putManaged(ctx, task.ID, data)
		if err != nil {
			t.Fatal(err)
		}
		svc.releaseBlob(ctx, l.ID)
	}
	now = now.Add(2 * time.Hour)
	opts := domain.StorageSweepOptions{Limit: 1, RequestKey: "one-user-click"}
	first, err := svc.SweepStorage(ctx, opts)
	if err != nil || first.Removed != 1 || !first.HasMore {
		t.Fatalf("first batch: %+v %v", first, err)
	}
	retry, err := svc.SweepStorage(ctx, opts)
	if err != nil || !reflect.DeepEqual(first, retry) {
		t.Fatalf("lost response retry changed receipt: %+v %v", retry, err)
	}
	files, err := os.ReadDir(filepath.Join(svc.Root, "blobs"))
	if err != nil || len(files) != 2 {
		t.Fatalf("same click deleted next batch: %d %v", len(files), err)
	}
	opts.RequestKey = "next-user-click"
	next, err := svc.SweepStorage(ctx, opts)
	if err != nil || next.Removed != 1 {
		t.Fatalf("new click failed: %+v %v", next, err)
	}
}
