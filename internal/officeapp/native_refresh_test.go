package officeapp

import (
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/lunitide/lunitide/internal/commandworker"
	domain "github.com/lunitide/lunitide/internal/domain/officestudio"
	content "github.com/lunitide/lunitide/internal/officestudio"
	"github.com/lunitide/lunitide/internal/storage/sqlite"
)

type interruptedNativeCheckStore struct {
	*sqlite.Store
	fail bool
}

func (s *interruptedNativeCheckStore) AddOfficeValidation(ctx context.Context, v domain.Validation) (domain.Validation, error) {
	if s.fail {
		return domain.Validation{}, errors.New("controlled validation interruption")
	}
	return s.Store.AddOfficeValidation(ctx, v)
}

// Uses real isolated SQLite, blobs, cache merge and immutable publication.
// Only the native subprocess is controlled; its real equivalent is covered
// separately by officerender's opt-in LibreOffice integration tests.
func TestNativeRefreshPublicationRetryAndCancellation(t *testing.T) {
	for _, interruptCheck := range []bool{false, true} {
		t.Run(fmt.Sprintf("interrupted_check_%t", interruptCheck), func(t *testing.T) {
			svc, db, task := studioServiceFixture(t)
			ctx := context.Background()
			spec := shortWordSpec()
			spec.Blocks = append([]content.Block{{Type: "toc"}}, spec.Blocks...)
			v, err := svc.Generate(ctx, task.ID, "目录报告.docx", spec, "native-refresh-source")
			if err != nil {
				t.Fatal(err)
			}
			_, source, err := svc.ReadVersion(ctx, task.ID, v.ID)
			if err != nil {
				t.Fatal(err)
			}
			candidate := nativeRefreshWordCandidate(t, source)
			head := headOf(t, db, task.ID)
			checks := &interruptedNativeCheckStore{Store: db, fail: interruptCheck}
			svc.Store = checks
			calls := 0
			var cancelWorker context.CancelFunc
			svc.Renderer.Run = func(_ context.Context, work commandworker.Spec, _ commandworker.StartGuard, chunk func([]byte)) (commandworker.Outcome, error) {
				calls++
				if len(work.Args) == 1 && work.Args[0] == "--version" {
					if chunk != nil {
						chunk([]byte("LibreOffice controlled-cache-fixture"))
					}
					return commandworker.Outcome{}, nil
				}
				if work.Args[len(work.Args)-1] == "--terminate_after_init" {
					user := filepath.Join(work.Dir, "profile", "user")
					if err := os.MkdirAll(user, 0700); err != nil {
						return commandworker.Outcome{}, err
					}
					return commandworker.Outcome{}, os.WriteFile(filepath.Join(user, "registrymodifications.xcu"), []byte(`<oor:items xmlns:oor="http://openoffice.org/2001/registry"></oor:items>`), 0600)
				}
				if cancelWorker != nil {
					cancelWorker()
					return commandworker.Outcome{}, context.Canceled
				}
				receipt := fmt.Sprintf("office-native-v1\nsourceSha256=%s\nkind=docx\nfieldsRefreshed=1\nupdatedIndexes=1\nrecalculated=0\nformulaCells=0\nformulaErrorCount=0\nformulaScanFull=0\ndone=1\n", v.SHA256)
				for name, data := range map[string][]byte{"receipt.txt": []byte(receipt), "source.pdf": []byte("%PDF-1.4\ncontrolled native result\n%%EOF\n"), "updated.docx": candidate} {
					if err := os.WriteFile(filepath.Join(work.Dir, name), data, 0600); err != nil {
						return commandworker.Outcome{}, err
					}
				}
				return commandworker.Outcome{}, nil
			}
			updated, err := svc.RefreshNativeCaches(ctx, task.ID, v.ID, head.Revision, "refresh-key")
			if (err != nil) != interruptCheck || updated.ID == "" || updated.ID == v.ID {
				t.Fatalf("first publication: %+v %v", updated, err)
			}
			checks.fail = false
			beforeRetry := calls
			retry, err := svc.RefreshNativeCaches(ctx, task.ID, v.ID, head.Revision, "refresh-key")
			if err != nil || retry.ID != updated.ID || calls != beforeRetry {
				t.Fatalf("retry republished or reran native worker: %+v calls=%d/%d err=%v", retry, calls, beforeRetry, err)
			}
			if retry.Quality == "unverified" {
				t.Fatal("retry returned a stale quality status after restoring validation")
			}
			qa, err := db.ListOfficeValidations(ctx, retry.ID)
			if err != nil || len(qa) != 1 {
				t.Fatalf("retry did not restore exactly one validation: %d %v", len(qa), err)
			}
			found := false
			for _, check := range qa[0].Checks {
				if check.ID == "fields_update" && check.Status == "passed" {
					found = true
				}
			}
			if !found {
				t.Fatalf("source field update evidence missing: %+v", qa[0].Checks)
			}
			if _, err = svc.RefreshNativeCaches(ctx, task.ID, v.ID, head.Revision+1, "refresh-key"); !errors.Is(err, domain.ErrConflict) {
				t.Fatalf("retry key accepted different revision: %v", err)
			}
			if _, err = svc.RefreshNativeCaches(ctx, task.ID, v.ID, head.Revision, "new-stale-key"); !errors.Is(err, domain.ErrConflict) || calls != beforeRetry {
				t.Fatalf("stale revision reached worker: %v calls=%d", err, calls)
			}
			_, after, err := svc.ReadVersion(ctx, task.ID, v.ID)
			if err != nil || !bytes.Equal(after, source) {
				t.Fatal("original version changed", err)
			}
			latest := headOf(t, db, task.ID)
			if latest.Revision != head.Revision+1 || latest.LatestVersionID != updated.ID {
				t.Fatalf("publication head mismatch: %+v", latest)
			}
			cancelCtx, cancel := context.WithCancel(ctx)
			defer cancel()
			cancelWorker = cancel
			if _, err = svc.RefreshNativeCaches(cancelCtx, task.ID, v.ID, latest.Revision, "cancelled-key"); !errors.Is(err, context.Canceled) {
				t.Fatalf("cancelled update result: %v", err)
			}
			if next := headOf(t, db, task.ID); next != latest {
				t.Fatalf("cancel published another version: %+v", next)
			}
		})
	}
}

func nativeRefreshWordCandidate(t *testing.T, source []byte) []byte {
	t.Helper()
	zr, err := zip.NewReader(bytes.NewReader(source), int64(len(source)))
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	zw := zip.NewWriter(&out)
	for _, f := range zr.File {
		r, err := f.Open()
		if err != nil {
			t.Fatal(err)
		}
		body, err := io.ReadAll(r)
		r.Close()
		if err != nil {
			t.Fatal(err)
		}
		if f.Name == "word/document.xml" {
			body = bytes.Replace(body, []byte("目录待实际排版后更新；当前未计算页码。"), []byte("进度 1"), 1)
		}
		w, err := zw.CreateHeader(&f.FileHeader)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = w.Write(body); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return out.Bytes()
}
