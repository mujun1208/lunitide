package sqlite

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/lunitide/lunitide/internal/domain/handoff"
	"github.com/lunitide/lunitide/internal/handoffapp"
	"github.com/oklog/ulid/v2"
)

func seedHandoff(t *testing.T, s *Store, sameProject bool) (string, string) {
	t.Helper()
	now := time.Now().UTC().Format(time.RFC3339Nano)
	project1, project2 := ulid.Make().String(), ulid.Make().String()
	if sameProject {
		project2 = project1
	}
	source, target := ulid.Make().String(), ulid.Make().String()
	message1, message2, checkpoint, capsule := ulid.Make().String(), ulid.Make().String(), ulid.Make().String(), ulid.Make().String()
	for i, p := range []string{project1, project2} {
		if _, err := s.db.Exec(`INSERT OR IGNORE INTO projects(id,name,project_code,status,created_at,updated_at,version) VALUES(?,?,?, 'active',?,?,1)`, p, "p-"+p, fmt.Sprintf("ITM%05d", i+1), now, now); err != nil {
			t.Fatal(err)
		}
	}
	for _, row := range [][2]string{{source, project1}, {target, project2}} {
		if _, err := s.db.Exec(`INSERT INTO sessions(id,project_id,title,status,created_at,updated_at,version) VALUES(?,?,'session','active',?,?,1)`, row[0], row[1], now, now); err != nil {
			t.Fatal(err)
		}
	}
	for i, id := range []string{message1, message2} {
		if _, err := s.db.Exec(`INSERT INTO messages(id,session_id,role,status,sequence,created_at) VALUES(?,?,'user','completed',?,?)`, id, source, i+1, now); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.db.Exec(`INSERT INTO compaction_checkpoints(id,session_id,version,source_start_id,source_end_id,source_start_seq,source_end_seq,source_digest,summary_schema_version,trigger,trigger_reason,status,provider,model,summary_json,human_summary,created_at,completed_at) VALUES(?,?,1,?,?,1,2,?,'1.0','manual','','succeeded','','','{}','',?,?)`, checkpoint, source, message1, message2, "abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789", now, now); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec(`INSERT INTO handoff_capsules(id,source_session_id,checkpoint_id,active_tasks_json,recent_message_ids,digest,status,created_at) VALUES(?,?,?,'[]','[]',?,'active',?)`, capsule, source, checkpoint, "abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789", now); err != nil {
		t.Fatal(err)
	}
	return capsule, target
}

func TestValidateAndRecordImportRejectsCrossProject(t *testing.T) {
	s, err := Open(context.Background(), filepath.Join(t.TempDir(), "handoff.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	capsule, target := seedHandoff(t, s, false)
	_, _, err = s.ValidateAndRecordImport(context.Background(), capsule, target, time.Now().UTC())
	if !errors.Is(err, handoffapp.ErrCrossProjectImport) {
		t.Fatalf("got %v", err)
	}
}

func TestActivateCapsuleRejectsCrossProjectTransactionally(t *testing.T) {
	s, err := Open(context.Background(), filepath.Join(t.TempDir(), "handoff.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	capsule, target := seedHandoff(t, s, false)
	err = s.ActivateCapsule(context.Background(), capsule, target, time.Now().UTC())
	if !errors.Is(err, handoffapp.ErrCrossProjectImport) {
		t.Fatalf("activate error = %v, want ErrCrossProjectImport", err)
	}
	var status string
	var dest any
	if err := s.db.QueryRow(`SELECT status,dest_session_id FROM handoff_capsules WHERE id=?`, capsule).Scan(&status, &dest); err != nil {
		t.Fatal(err)
	}
	if status != string(handoff.StatusActive) || dest != nil {
		t.Fatalf("rejected activation mutated capsule: status=%s dest=%v", status, dest)
	}
}

func TestActivateCapsuleRejectsExpiredAtBoundary(t *testing.T) {
	s, err := Open(context.Background(), filepath.Join(t.TempDir(), "handoff.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	capsule, target := seedHandoff(t, s, true)
	now := time.Now().UTC()
	if _, err := s.db.Exec(`UPDATE handoff_capsules SET expires_at=? WHERE id=?`, formatTime(now), capsule); err != nil {
		t.Fatal(err)
	}
	err = s.ActivateCapsule(context.Background(), capsule, target, now)
	if !errors.Is(err, handoffapp.ErrCapsuleExpired) {
		t.Fatalf("activate error = %v, want ErrCapsuleExpired", err)
	}
}

func TestConcurrentActivationsOnlyOneWins(t *testing.T) {
	path := filepath.Join(t.TempDir(), "handoff.db")
	a, err := Open(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	b, err := Open(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	capsule, target := seedHandoff(t, a, true)
	start := make(chan struct{})
	errs := make(chan error, 2)
	go func() { <-start; errs <- a.ActivateCapsule(context.Background(), capsule, target, time.Now().UTC()) }()
	go func() { <-start; errs <- b.ActivateCapsule(context.Background(), capsule, target, time.Now().UTC()) }()
	close(start)
	e1, e2 := <-errs, <-errs
	if (e1 == nil) == (e2 == nil) {
		t.Fatalf("want exactly one activation winner: %v, %v", e1, e2)
	}
	loser := e1
	if loser == nil {
		loser = e2
	}
	if !errors.Is(loser, handoffapp.ErrCapsuleNotActive) {
		t.Fatalf("loser error = %v, want ErrCapsuleNotActive", loser)
	}
}

func TestActivateVsRevokeFirstTerminalWins(t *testing.T) {
	s, err := Open(context.Background(), filepath.Join(t.TempDir(), "handoff.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	capsule, target := seedHandoff(t, s, true)
	start := make(chan struct{})
	errs := make(chan error, 2)
	go func() { <-start; errs <- s.ActivateCapsule(context.Background(), capsule, target, time.Now().UTC()) }()
	go func() { <-start; errs <- s.RevokeCapsule(context.Background(), capsule) }()
	close(start)
	e1, e2 := <-errs, <-errs
	if (e1 == nil) == (e2 == nil) {
		t.Fatalf("want one winner: %v, %v", e1, e2)
	}
	loser := e1
	if loser == nil {
		loser = e2
	}
	if !errors.Is(loser, handoffapp.ErrCapsuleNotActive) {
		t.Fatalf("loser = %v", loser)
	}
}

func TestConcurrentRevokeVsImportSerialized(t *testing.T) {
	path := filepath.Join(t.TempDir(), "handoff.db")
	a, err := Open(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	b, err := Open(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	capsule, target := seedHandoff(t, a, true)
	start := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(2)
	var revokeErr, importErr error
	go func() { defer wg.Done(); <-start; revokeErr = a.RevokeCapsule(context.Background(), capsule) }()
	go func() {
		defer wg.Done()
		<-start
		_, _, importErr = b.ValidateAndRecordImport(context.Background(), capsule, target, time.Now().UTC())
	}()
	close(start)
	wg.Wait()
	if revokeErr != nil {
		t.Fatalf("revoke = %v", revokeErr)
	}
	if importErr != nil && !errors.Is(importErr, handoffapp.ErrCapsuleNotActive) {
		t.Fatalf("import = %v", importErr)
	}
	var status string
	if err := a.db.QueryRow(`SELECT status FROM handoff_capsules WHERE id=?`, capsule).Scan(&status); err != nil {
		t.Fatal(err)
	}
	if status != string(handoff.StatusRevoked) {
		t.Fatalf("status=%s", status)
	}
}
