package sqlite

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/lunitide/lunitide/internal/domain/provider"
	"github.com/lunitide/lunitide/internal/providerapp"
)

type fixedClock struct{ at time.Time }

func (c fixedClock) Now() time.Time { return c.at }

func openAppStore(t *testing.T, name string) *Store {
	t.Helper()
	s, err := Open(context.Background(), filepath.Join(t.TempDir(), name+".db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func tableCount(t *testing.T, s *Store, table string) int {
	t.Helper()
	var n int
	if err := s.db.QueryRow(`SELECT count(*) FROM ` + table).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestProviderMutationAuditOutboxIdempotencyFailuresRollbackEverything(t *testing.T) {
	for _, tc := range []struct{ name, trigger string }{
		{"mutation", `CREATE TRIGGER fail_stage BEFORE INSERT ON providers BEGIN SELECT RAISE(ABORT,'mutation failed'); END`},
		{"audit", `CREATE TRIGGER fail_stage BEFORE INSERT ON audit_events BEGIN SELECT RAISE(ABORT,'audit failed'); END`},
		{"outbox", `CREATE TRIGGER fail_stage BEFORE INSERT ON outbox_events BEGIN SELECT RAISE(ABORT,'outbox failed'); END`},
		{"idempotency", `CREATE TRIGGER fail_stage BEFORE INSERT ON idempotency_records BEGIN SELECT RAISE(ABORT,'idempotency failed'); END`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := openAppStore(t, tc.name)
			if _, err := s.db.Exec(tc.trigger); err != nil {
				t.Fatal(err)
			}
			app := providerapp.New(s, s)
			if _, err := app.Create(context.Background(), "rollback-key", "tester", validProvider()); err == nil {
				t.Fatal("injected failure succeeded")
			}
			for _, table := range []string{"providers", "provider_models", "audit_events", "outbox_events", "idempotency_records"} {
				if n := tableCount(t, s, table); n != 0 {
					t.Fatalf("%s retained %d rows", table, n)
				}
			}
		})
	}
}

func TestHundredConcurrentSameKeySameDigestCreatesOnceAndReplaysSameResult(t *testing.T) {
	s := openAppStore(t, "same-key")
	app := providerapp.New(s, s)
	const workers = 100
	start := make(chan struct{})
	results := make(chan provider.Provider, workers)
	errs := make(chan error, workers)
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			p, err := app.Create(context.Background(), "one-key", "tester", validProvider())
			results <- p
			errs <- err
		}()
	}
	close(start)
	wg.Wait()
	close(results)
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("concurrent create: %v", err)
		}
	}
	var id string
	for p := range results {
		if id == "" {
			id = p.ID
		}
		if p.ID != id || p.Version != 1 {
			t.Fatalf("non-identical replay: %#v", p)
		}
	}
	for table, want := range map[string]int{"providers": 1, "provider_models": 1, "audit_events": 1, "outbox_events": 1, "idempotency_records": 1} {
		if got := tableCount(t, s, table); got != want {
			t.Fatalf("%s=%d want %d", table, got, want)
		}
	}
}

func TestSameKeyDifferentDigestConflictsWithoutSecondMutation(t *testing.T) {
	s := openAppStore(t, "digest-conflict")
	app := providerapp.New(s, s)
	if _, err := app.Create(context.Background(), "same", "tester", validProvider()); err != nil {
		t.Fatal(err)
	}
	other := validProvider()
	other.Name = "Different"
	if _, err := app.Create(context.Background(), "same", "tester", other); !errors.Is(err, providerapp.ErrIdempotencyConflict) {
		t.Fatalf("got %v", err)
	}
	if n := tableCount(t, s, "providers"); n != 1 {
		t.Fatalf("providers=%d", n)
	}
}

func TestExpiredIdempotencyKeyIsAtomicallyReusedAtTransactionClock(t *testing.T) {
	s := openAppStore(t, "expiry")
	t0 := time.Date(2030, 1, 2, 3, 4, 5, 0, time.UTC)
	first := providerapp.NewWithClock(s, s, fixedClock{t0})
	if _, err := first.Create(context.Background(), "reusable", "tester", validProvider()); err != nil {
		t.Fatal(err)
	}
	var expires string
	if err := s.db.QueryRow(`SELECT expires_at FROM idempotency_records`).Scan(&expires); err != nil {
		t.Fatal(err)
	}
	if expires != formatTime(t0.Add(24*time.Hour)) {
		t.Fatalf("expires=%s", expires)
	}
	other := validProvider()
	other.Name = "After expiry"
	second := providerapp.NewWithClock(s, s, fixedClock{t0.Add(24 * time.Hour)})
	if _, err := second.Create(context.Background(), "reusable", "tester", other); err != nil {
		t.Fatalf("atomic reuse: %v", err)
	}
	if tableCount(t, s, "providers") != 2 || tableCount(t, s, "idempotency_records") != 1 {
		t.Fatal("expired record was not atomically replaced")
	}
}

func TestServiceUoWConcurrentUpdateUpdateAndUpdateDeleteHaveOneWinner(t *testing.T) {
	for _, mode := range []string{"update-update", "update-delete"} {
		t.Run(mode, func(t *testing.T) {
			s := openAppStore(t, mode)
			app := providerapp.New(s, s)
			created, err := app.Create(context.Background(), "create", "tester", validProvider())
			if err != nil {
				t.Fatal(err)
			}
			left, right := created, created
			left.Name, right.Name = "left", "right"
			start := make(chan struct{})
			result := make(chan error, 2)
			go func() { <-start; _, e := app.Update(context.Background(), "left", "tester", left, 1); result <- e }()
			go func() {
				<-start
				if mode == "update-delete" {
					_, e := app.Delete(context.Background(), "right", "tester", created.ID, 1)
					result <- e
				} else {
					_, e := app.Update(context.Background(), "right", "tester", right, 1)
					result <- e
				}
			}()
			close(start)
			e1, e2 := <-result, <-result
			success := 0
			for _, e := range []error{e1, e2} {
				if e == nil {
					success++
				} else if !errors.Is(e, provider.ErrConflict) && !errors.Is(e, provider.ErrNotFound) {
					t.Fatalf("unexpected loser: %v", e)
				}
			}
			if success != 1 {
				t.Fatalf("want one winner: %v, %v", e1, e2)
			}
		})
	}
}

func insertOutbox(t *testing.T, s *Store, id string, attempts int, at time.Time) {
	t.Helper()
	_, err := s.db.Exec(`INSERT INTO outbox_events(id,topic,aggregate_id,payload_json,status,available_at,attempts,created_at) VALUES(?,?,?,?, 'pending',?,?,?)`, id, "provider.updated", "p", `{}`, formatTime(at), attempts, formatTime(at))
	if err != nil {
		t.Fatal(err)
	}
}

func TestOutboxTwoWorkersDoNotDuplicateAndExpiredLeaseCanBeReclaimed(t *testing.T) {
	s := openAppStore(t, "claims")
	now := time.Date(2031, 1, 1, 0, 0, 0, 0, time.UTC)
	for i := 0; i < 20; i++ {
		insertOutbox(t, s, fmt.Sprintf("e%02d", i), 0, now)
	}
	start := make(chan struct{})
	got := make(chan []providerapp.ClaimedEvent, 2)
	errs := make(chan error, 2)
	for _, owner := range []string{"a", "b"} {
		go func(owner string) {
			<-start
			events, err := s.Claim(context.Background(), owner, now, time.Minute, 20)
			got <- events
			errs <- err
		}(owner)
	}
	close(start)
	a, b := <-got, <-got
	for i := 0; i < 2; i++ {
		if err := <-errs; err != nil {
			t.Fatal(err)
		}
	}
	seen := map[string]bool{}
	for _, list := range [][]providerapp.ClaimedEvent{a, b} {
		for _, e := range list {
			if seen[e.ID] {
				t.Fatalf("duplicate claim %s", e.ID)
			}
			seen[e.ID] = true
		}
	}
	if len(seen) != 20 {
		t.Fatalf("claimed %d", len(seen))
	}
	one := a
	if len(one) == 0 {
		one = b
	}
	id := one[0].ID
	events, err := s.Claim(context.Background(), "c", now.Add(time.Minute), time.Minute, 20)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, e := range events {
		if e.ID == id {
			found = true
		}
	}
	if !found {
		t.Fatalf("expired lease %s not reclaimed", id)
	}
}

func TestOutboxThousandthFailureDeadLettersAndDoesNotBlockFollowingEvent(t *testing.T) {
	s := openAppStore(t, "dead-letter")
	now := time.Date(2032, 1, 1, 0, 0, 0, 0, time.UTC)
	insertOutbox(t, s, "poison", 999, now)
	insertOutbox(t, s, "healthy", 0, now.Add(time.Second))
	claimed, err := s.Claim(context.Background(), "worker", now, time.Minute, 1)
	if err != nil || len(claimed) != 1 || claimed[0].Attempts != 1000 {
		t.Fatalf("claim: %#v %v", claimed, err)
	}
	if err = s.Retry(context.Background(), "poison", "worker", now, "still bad"); err != nil {
		t.Fatal(err)
	}
	var status string
	if err = s.db.QueryRow(`SELECT status FROM outbox_events WHERE id='poison'`).Scan(&status); err != nil || status != "dead_letter" {
		t.Fatalf("status=%s err=%v", status, err)
	}
	next, err := s.Claim(context.Background(), "worker2", now.Add(time.Second), time.Minute, 10)
	if err != nil || len(next) != 1 || next[0].ID != "healthy" {
		t.Fatalf("poison blocked next: %#v %v", next, err)
	}
}

func TestOutboxExpiredThousandthClaimIsSafelyReclaimedAfterCrash(t *testing.T) {
	s := openAppStore(t, "final-claim-crash")
	now := time.Date(2033, 1, 1, 0, 0, 0, 0, time.UTC)
	insertOutbox(t, s, "cleanup", 999, now)
	first, err := s.Claim(context.Background(), "crashed", now, time.Minute, 1)
	if err != nil || len(first) != 1 || first[0].Attempts != 1000 {
		t.Fatalf("final claim: %#v %v", first, err)
	}
	reclaimed, err := s.Claim(context.Background(), "recovery", now.Add(time.Minute), time.Minute, 1)
	if err != nil || len(reclaimed) != 1 || reclaimed[0].ID != "cleanup" || reclaimed[0].Attempts != 1000 {
		t.Fatalf("expired final claim not reclaimed: %#v %v", reclaimed, err)
	}
	if err = s.Complete(context.Background(), "cleanup", "recovery", now.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
}

func TestCredentialOriginBindingAndFingerprintTamperFailClosed(t *testing.T) {
	s := openAppStore(t, "binding")
	ctx := context.Background()
	created, err := s.Create(ctx, validProvider())
	if err != nil {
		t.Fatal(err)
	}
	pathOnly := created
	pathOnly.BaseURL = "https://example.com/v2"
	updated, err := s.Update(ctx, pathOnly, 1)
	if err != nil {
		t.Fatalf("path-only rejected: %v", err)
	}
	for name, mutate := range map[string]func(*provider.Provider){
		"origin":   func(p *provider.Provider) { p.BaseURL = "https://other.example/v2" },
		"protocol": func(p *provider.Provider) { p.Protocol = provider.ProtocolAnthropic },
	} {
		t.Run(name, func(t *testing.T) {
			p := updated
			mutate(&p)
			if _, e := s.Update(ctx, p, 2); !errors.Is(e, provider.ErrCredentialReentryRequired) {
				t.Fatalf("got %v", e)
			}
		})
	}
	if _, err = s.db.Exec(`UPDATE providers SET origin_fingerprint=? WHERE id=?`, strings.Repeat("f", 64), created.ID); err != nil {
		t.Fatal(err)
	}
	tampered := updated
	tampered.Name = "tampered"
	if _, err = s.Update(ctx, tampered, 2); err == nil || !strings.Contains(err.Error(), "fingerprint mismatch") {
		t.Fatalf("tamper did not fail closed: %v", err)
	}
}

func TestOutboxPayloadExplicitlyExcludesLegacyAndCredentialFields(t *testing.T) {
	s := openAppStore(t, "payload")
	app := providerapp.New(s, s)
	p := validProvider()
	p.LegacyID = "legacy-secret"
	created, err := app.Create(context.Background(), "payload-key", "tester", p)
	if err != nil {
		t.Fatal(err)
	}
	var raw string
	if err = s.db.QueryRow(`SELECT payload_json FROM outbox_events`).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	var payload map[string]any
	if err = json.Unmarshal([]byte(raw), &payload); err != nil {
		t.Fatal(err)
	}
	if len(payload) != 2 || payload["providerId"] != created.ID || payload["version"] != float64(1) {
		t.Fatalf("unexpected payload fields: %s", raw)
	}
	lower := strings.ToLower(raw)
	for _, forbidden := range []string{"legacy", "credential", "vault-item-1"} {
		if strings.Contains(lower, forbidden) {
			t.Fatalf("payload leaked %q: %s", forbidden, raw)
		}
	}
}

func TestIdempotencyResponseExcludesLegacyAndCredentialFields(t *testing.T) {
	s := openAppStore(t, "idempotency-public-response")
	app := providerapp.New(s, s)
	p := validProvider()
	p.LegacyID = "legacy/private-id"
	created, err := app.Create(context.Background(), "safe-response", "tester", p)
	if err != nil {
		t.Fatal(err)
	}
	var response string
	if err = s.db.QueryRow(`SELECT response_json FROM idempotency_records WHERE operation='provider.create' AND idempotency_key='safe-response'`).Scan(&response); err != nil {
		t.Fatal(err)
	}
	lower := strings.ToLower(response)
	if strings.Contains(lower, "legacyid") || strings.Contains(lower, "credentialref") || strings.Contains(lower, created.CredentialRef) {
		t.Fatalf("internal field entered idempotency response: %s", response)
	}
}

func TestMapWriteErrorDoesNotClassifyArbitraryBusyText(t *testing.T) {
	err := errors.New("business rule says resource is busy")
	if mapped := mapWriteError(err); !errors.Is(mapped, err) || errors.Is(mapped, providerapp.ErrStorageBusy) {
		t.Fatalf("ordinary error was misclassified: %v", mapped)
	}
}
