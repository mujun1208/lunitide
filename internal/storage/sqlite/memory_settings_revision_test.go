package sqlite

import (
	"context"
	"errors"
	"github.com/lunitide/lunitide/internal/domain/m8core"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func TestMemorySettingsCASConcurrentAndMonotonicABA(t *testing.T) {
	ctx := context.Background()
	s, err := OpenTemplated(ctx, filepath.Join(t.TempDir(), "settings.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	initial, err := s.GetMemorySettings(ctx, "local-user")
	if err != nil {
		t.Fatal(err)
	}
	version := m8core.SettingsVersion(initial)
	var wg sync.WaitGroup
	errs := make(chan error, 2)
	for _, days := range []int{7, 30} {
		wg.Add(1)
		go func(days int) {
			defer wg.Done()
			next := initial
			next.GrowthDays = days
			_, err := s.CompareAndSwapMemorySettings(ctx, next, version)
			errs <- err
		}(days)
	}
	wg.Wait()
	close(errs)
	successes, conflicts := 0, 0
	for err := range errs {
		if err == nil {
			successes++
		} else if errors.Is(err, m8core.ErrSettingsConflict) {
			conflicts++
		} else {
			t.Fatal(err)
		}
	}
	if successes != 1 || conflicts != 1 {
		t.Fatalf("success=%d conflict=%d", successes, conflicts)
	}
	current, err := s.GetMemorySettings(ctx, "local-user")
	if err != nil {
		t.Fatal(err)
	}
	back := current
	back.GrowthDays = initial.GrowthDays
	saved, err := s.CompareAndSwapMemorySettings(ctx, back, m8core.SettingsVersion(current))
	if err != nil {
		t.Fatal(err)
	}
	if m8core.SettingsVersion(saved) == version {
		t.Fatal("ABA reused version")
	}
	if _, err = s.CompareAndSwapMemorySettings(ctx, initial, version); !errors.Is(err, m8core.ErrSettingsConflict) {
		t.Fatalf("stale ABA: %v", err)
	}
	future := time.Now().UTC().Add(time.Hour)
	if _, err = s.db.ExecContext(ctx, `UPDATE memory_settings SET updated_at=? WHERE subject_id=?`, formatTime(future), initial.SubjectID); err != nil {
		t.Fatal(err)
	}
	current, err = s.GetMemorySettings(ctx, initial.SubjectID)
	if err != nil {
		t.Fatal(err)
	}
	saved, err = s.CompareAndSwapMemorySettings(ctx, current, m8core.SettingsVersion(current))
	if err != nil {
		t.Fatal(err)
	}
	stamp, err := time.Parse(time.RFC3339Nano, saved.UpdatedAt)
	if err != nil || !stamp.Equal(future.Add(time.Nanosecond)) {
		t.Fatalf("non-monotonic: %s %v", saved.UpdatedAt, err)
	}
	if _, err = s.db.ExecContext(ctx, `CREATE TRIGGER fail_memory_settings_audit BEFORE INSERT ON audit_events WHEN NEW.action='memory.settings.update' BEGIN SELECT RAISE(ABORT,'fixture audit failure'); END`); err != nil {
		t.Fatal(err)
	}
	changed := saved
	changed.GrowthDays = 42
	if _, err = s.CompareAndSwapMemorySettings(ctx, changed, m8core.SettingsVersion(saved)); err == nil {
		t.Fatal("audit failure accepted")
	}
	actual, err := s.GetMemorySettings(ctx, initial.SubjectID)
	if err != nil {
		t.Fatal(err)
	}
	if m8core.SettingsVersion(actual) != m8core.SettingsVersion(saved) {
		t.Fatal("failed audit committed settings")
	}
}
