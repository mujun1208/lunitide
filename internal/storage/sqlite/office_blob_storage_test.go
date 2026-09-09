package sqlite

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	domain "github.com/lunitide/lunitide/internal/domain/officestudio"
	"github.com/oklog/ulid/v2"
)

func blobReservation(task domain.Task, ref string, size, limit int64, now time.Time) domain.BlobReservation {
	return domain.BlobReservation{TaskID: task.ID, Digest: ref, Size: size, MaxBytes: limit, Now: now, ExpiresAt: now.Add(time.Minute)}
}

func TestOfficeBlobConcurrentReservationsDeduplicateCapacity(t *testing.T) {
	s, task := officeFixture(t)
	ctx := context.Background()
	now := time.Now().UTC()
	ref := strings.Repeat("a", 64)
	const n = 12
	leases := make(chan domain.BlobLease, n)
	errs := make(chan error, n)
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			l, e := s.ReserveOfficeBlob(ctx, blobReservation(task, ref, 120, 120, now))
			leases <- l
			errs <- e
		}()
	}
	wg.Wait()
	close(errs)
	close(leases)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	seen := map[string]bool{}
	for l := range leases {
		if seen[l.ID] || l.Digest != ref {
			t.Fatalf("invalid independent lease: %+v", l)
		}
		seen[l.ID] = true
	}
	u, err := s.OfficeStorageUsage(ctx, now)
	if err != nil || u.TotalBytes != 120 || u.ReservedBytes != 120 || u.ActiveLeaseCount != n {
		t.Fatalf("reservation accounting: %+v %v", u, err)
	}
	if _, err = s.ReserveOfficeBlob(ctx, blobReservation(task, strings.Repeat("b", 64), 1, 120, now)); !errors.Is(err, domain.ErrStorageQuota) {
		t.Fatalf("concurrent reservations bypassed budget: %v", err)
	}
	foreign := domain.WithScope(ctx, ulid.Make().String())
	if _, err = s.ReserveOfficeBlob(foreign, blobReservation(task, ref, 120, 120, now)); err == nil {
		t.Fatal("foreign organization reserved task bytes")
	}
}

func TestOfficeBlobPublicationConsumesLeaseAtomicallyAndKeepsPermanentRoots(t *testing.T) {
	s, task := officeFixture(t)
	ctx := context.Background()
	now := time.Now().UTC()
	ref := strings.Repeat("c", 64)
	l, err := s.ReserveOfficeBlob(ctx, blobReservation(task, ref, 120, 1000, now))
	if err != nil {
		t.Fatal(err)
	}
	if err = s.WriteOfficeBlob(ctx, l.ID, now, func(domain.BlobLease) error { return nil }); err != nil {
		t.Fatal(err)
	}
	v := officeVersion(task.ID, ulid.Make().String())
	v.ContentRef = ref
	v.SHA256 = ref
	request := domain.PublishRequest{Version: v, IdempotencyKey: "publish", BlobLeaseID: l.ID}
	if _, err = s.db.Exec(`CREATE TRIGGER fail_blob_reference BEFORE INSERT ON office_blob_references BEGIN SELECT RAISE(ABORT,'forced reference failure'); END;`); err != nil {
		t.Fatal(err)
	}
	if _, err = s.PublishOfficeVersion(ctx, request); err == nil {
		t.Fatal("reference failure published version")
	}
	var versions, leases int
	if err = s.db.QueryRow(`SELECT count(*) FROM office_version_metadata`).Scan(&versions); err != nil {
		t.Fatal(err)
	}
	if err = s.db.QueryRow(`SELECT count(*) FROM office_blob_leases WHERE id=?`, l.ID).Scan(&leases); err != nil {
		t.Fatal(err)
	}
	if versions != 0 || leases != 1 {
		t.Fatalf("publication not atomic versions=%d leases=%d", versions, leases)
	}
	if _, err = s.db.Exec(`DROP TRIGGER fail_blob_reference`); err != nil {
		t.Fatal(err)
	}
	v, err = s.PublishOfficeVersion(ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	retry, err := s.PublishOfficeVersion(ctx, request)
	if err != nil || retry.ID != v.ID {
		t.Fatalf("retry with consumed lease: %+v %v", retry, err)
	}
	if _, err = s.AcceptOfficeVersion(ctx, task.ID, v.ArtifactID, v.ID, 1); err != nil {
		t.Fatal(err)
	}
	// A preview has its own permanent QA reference and shares the same atomic
	// lease binding; the original version's acceptance is independent.
	preview := strings.Repeat("d", 64)
	pl, err := s.ReserveOfficeBlob(ctx, blobReservation(task, preview, 50, 1000, now))
	if err != nil {
		t.Fatal(err)
	}
	if err = s.WriteOfficeBlob(ctx, pl.ID, now, func(domain.BlobLease) error { return nil }); err != nil {
		t.Fatal(err)
	}
	_, err = s.AddOfficeValidation(ctx, domain.Validation{VersionID: v.ID, SHA256: v.SHA256, Validator: "fixture", Evidence: json.RawMessage(`{"pdfRef":"` + preview + `"}`), BlobLeaseID: pl.ID})
	if err != nil {
		t.Fatal(err)
	}
	called := false
	// GC invoked from a different organization still inspects all immutable
	// roots. It must never scope-filter the rows used to decide physical removal.
	report, err := s.SweepOfficeStorage(domain.WithScope(ctx, ulid.Make().String()), domain.StorageSweepOptions{}, now.Add(72*time.Hour), time.Hour, func(domain.BlobFile, []string) (int64, error) { called = true; return 0, nil })
	if err != nil || report.Removed != 0 || called {
		t.Fatalf("permanent file collected: %+v %v", report, err)
	}
	u, err := s.OfficeStorageUsage(ctx, now)
	if err != nil || u.ReferencedBytes != 170 || u.ActiveLeaseCount != 0 {
		t.Fatalf("permanent refs/leases: %+v %v", u, err)
	}
	if _, err = s.db.Exec(`UPDATE office_blob_events SET action='removed'`); err == nil {
		t.Fatal("storage audit rewritable")
	}
}

func TestOfficeBlobSweepRechecksReferenceAfterCandidateAndRenewedLease(t *testing.T) {
	s, task := officeFixture(t)
	ctx := context.Background()
	now := time.Now().UTC()
	ref := strings.Repeat("e", 64)
	l, err := s.ReserveOfficeBlob(ctx, blobReservation(task, ref, 120, 1000, now))
	if err != nil {
		t.Fatal(err)
	}
	if err = s.WriteOfficeBlob(ctx, l.ID, now, func(domain.BlobLease) error { return nil }); err != nil {
		t.Fatal(err)
	}
	if err = s.RenewOfficeBlob(ctx, l.ID, now.Add(30*time.Second), now.Add(4*time.Hour)); err != nil {
		t.Fatal(err)
	}
	remove := func(domain.BlobFile, []string) (int64, error) {
		t.Error("live or referenced blob removed")
		return 0, nil
	}
	r, err := s.SweepOfficeStorage(ctx, domain.StorageSweepOptions{}, now.Add(2*time.Hour), time.Hour, remove)
	if err != nil || r.Candidates != 0 {
		t.Fatalf("renewed lease lost: %+v %v", r, err)
	}
	if err = s.ReleaseOfficeBlob(ctx, l.ID, now, nil); err != nil {
		t.Fatal(err)
	}
	r, err = s.SweepOfficeStorage(ctx, domain.StorageSweepOptions{DryRun: true}, now.Add(2*time.Hour), time.Hour, remove)
	if err != nil || r.Candidates != 1 {
		t.Fatalf("expected isolated candidate: %+v %v", r, err)
	}
	// Deterministically add a root after the candidate was identified, then
	// exercise the same final collector operation used by a concurrent sweep.
	v := officeVersion(task.ID, ulid.Make().String())
	v.ContentRef = ref
	v.SHA256 = ref
	if _, err = s.PublishOfficeVersion(ctx, domain.PublishRequest{Version: v, IdempotencyKey: "late-root"}); err != nil {
		t.Fatal(err)
	}
	candidate, _, _, err := s.sweepOfficeBlob(ctx, ref, false, now.Add(2*time.Hour), officeRFC(now.Add(time.Hour)), remove)
	if err != nil || candidate {
		t.Fatalf("candidate snapshot used as deletion authority: %v %v", candidate, err)
	}
	if err = s.RenewOfficeBlob(ctx, l.ID, now.Add(8*time.Hour), now.Add(9*time.Hour)); !errors.Is(err, domain.ErrBlobLease) {
		t.Fatalf("revived consumed lease: %v", err)
	}
}

func TestOfficeBlobCollectorRollbackRetainsAccountingAndRetry(t *testing.T) {
	s, task := officeFixture(t)
	ctx := context.Background()
	now := time.Now().UTC()
	l, err := s.ReserveOfficeBlob(ctx, blobReservation(task, strings.Repeat("f", 64), 80, 80, now))
	if err != nil {
		t.Fatal(err)
	}
	if err = s.WriteOfficeBlob(ctx, l.ID, now, func(domain.BlobLease) error { return errors.New("write interrupted") }); err == nil {
		t.Fatal("write failure hidden")
	}
	failure := func(domain.BlobFile, []string) (int64, error) { return 0, errors.New("busy file") }
	r, err := s.SweepOfficeStorage(ctx, domain.StorageSweepOptions{}, now.Add(48*time.Hour), 24*time.Hour, failure)
	if err != nil || r.Removed != 0 || len(r.Errors) != 1 {
		t.Fatalf("failed collection: %+v %v", r, err)
	}
	u, err := s.OfficeStorageUsage(ctx, now)
	if err != nil || u.ReservedBytes != 80 {
		t.Fatalf("failed removal freed budget: %+v %v", u, err)
	}
	r, err = s.SweepOfficeStorage(ctx, domain.StorageSweepOptions{}, now.Add(48*time.Hour), 24*time.Hour, func(b domain.BlobFile, stages []string) (int64, error) {
		if b.Digest != l.Digest || len(stages) != 1 || stages[0] != l.StageName {
			t.Fatal("unregistered deletion paths")
		}
		return 0, nil
	})
	if err != nil || r.Removed != 1 || r.ReleasedReservationBytes != 80 {
		t.Fatalf("retry: %+v %v", r, err)
	}
	if _, err = s.ReserveOfficeBlob(ctx, blobReservation(task, strings.Repeat("a", 64), 80, 80, now)); err != nil {
		t.Fatalf("success did not release budget: %v", err)
	}
}

func TestOfficeBlobSweepRetryKeepsFixedBatchAfterCancellation(t *testing.T) {
	s, task := officeFixture(t)
	ctx := context.Background()
	now := time.Now().UTC()
	for _, letter := range []string{"a", "b"} {
		l, err := s.ReserveOfficeBlob(ctx, blobReservation(task, strings.Repeat(letter, 64), 80, 1000, now))
		if err != nil {
			t.Fatal(err)
		}
		if err = s.WriteOfficeBlob(ctx, l.ID, now, func(domain.BlobLease) error { return nil }); err != nil {
			t.Fatal(err)
		}
		if err = s.ReleaseOfficeBlob(ctx, l.ID, now, nil); err != nil {
			t.Fatal(err)
		}
	}
	cancelCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	calls := 0
	opts := domain.StorageSweepOptions{Limit: 2, RequestKey: "lost-response"}
	_, err := s.SweepOfficeStorage(cancelCtx, opts, now.Add(48*time.Hour), time.Hour, func(domain.BlobFile, []string) (int64, error) {
		calls++
		if calls == 2 {
			cancel()
			return 0, cancelCtx.Err()
		}
		return 80, nil
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected interrupted fixed batch: %v", err)
	}
	newRef := strings.Repeat("c", 64)
	l, err := s.ReserveOfficeBlob(ctx, blobReservation(task, newRef, 80, 1000, now))
	if err != nil {
		t.Fatal(err)
	}
	if err = s.WriteOfficeBlob(ctx, l.ID, now, func(domain.BlobLease) error { return nil }); err != nil {
		t.Fatal(err)
	}
	if err = s.ReleaseOfficeBlob(ctx, l.ID, now, nil); err != nil {
		t.Fatal(err)
	}
	r, err := s.SweepOfficeStorage(ctx, opts, now.Add(48*time.Hour), time.Hour, func(b domain.BlobFile, _ []string) (int64, error) {
		if b.Digest == newRef {
			t.Fatal("retry expanded candidate batch")
		}
		return 80, nil
	})
	if err != nil || r.Removed != 2 || r.FreedBytes != 160 {
		t.Fatalf("resumed receipt: %+v %v", r, err)
	}
	retry, err := s.SweepOfficeStorage(ctx, opts, now.Add(49*time.Hour), time.Hour, func(domain.BlobFile, []string) (int64, error) {
		t.Fatal("completed receipt re-executed deletion")
		return 0, nil
	})
	if err != nil || retry.Removed != r.Removed || retry.FreedBytes != r.FreedBytes {
		t.Fatalf("receipt drift: %+v %v", retry, err)
	}
	opts.Limit = 1
	if _, err = s.SweepOfficeStorage(ctx, opts, now.Add(49*time.Hour), time.Hour, func(domain.BlobFile, []string) (int64, error) { return 0, nil }); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("changed request accepted: %v", err)
	}
}
