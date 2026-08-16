package budget

import (
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"
)

var baseTime = time.Date(2026, 8, 16, 12, 0, 0, 0, time.UTC)

func TestOrgLedger(t *testing.T) {
	t.Run("reserve→settle→release lifecycle keeps derived balances honest", func(t *testing.T) {
		l := New()
		if err := l.SetLimit("01JDORG", "org", 1000, baseTime); err != nil {
			t.Fatal(err)
		}
		r, err := l.Reserve("res-1", "01JDORG", "org", 200, baseTime.Add(time.Hour), baseTime)
		if err != nil {
			t.Fatal(err)
		}
		a, _ := l.Account("01JDORG", "org")
		if a.Reserved != 200 || a.Available() != 800 {
			t.Fatalf("after reserve: reserved=%d available=%d", a.Reserved, a.Available())
		}
		if _, err := l.Settle(Receipt{ReceiptID: "rc-1", ReservationID: "res-1", ActualAmount: 150, PayloadDigest: "d1"}, baseTime.Add(time.Minute)); err != nil {
			t.Fatal(err)
		}
		a, _ = l.Account("01JDORG", "org")
		if a.Reserved != 0 || a.Settled != 150 || a.Available() != 850 {
			t.Fatalf("after settle: %+v", a)
		}
		if r.State != ReservationSettled || r.Actual != 150 {
			t.Fatalf("reservation view: %+v", r)
		}
		if err := l.VerifyConservation(); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("T-15: concurrent reservations never overshoot the hard limit", func(t *testing.T) {
		l := New()
		if err := l.SetLimit("01JDORG", "org", 1000, baseTime); err != nil {
			t.Fatal(err)
		}
		const workers, amount = 64, 100
		var wg sync.WaitGroup
		var mu sync.Mutex
		okCount, failCount := 0, 0
		for i := 0; i < workers; i++ {
			wg.Add(1)
			go func(i int) {
				defer wg.Done()
				_, err := l.Reserve(fmt.Sprintf("res-%d", i), "01JDORG", "org", amount, baseTime.Add(time.Hour), baseTime)
				mu.Lock()
				defer mu.Unlock()
				if err != nil {
					if Code(err) != "M9-024" {
						t.Errorf("worker %d: want M9-024, got %v", i, err)
					}
					failCount++
				} else {
					okCount++
				}
			}(i)
		}
		wg.Wait()
		if okCount != 10 {
			t.Fatalf("exactly 10 reservations of 100 must fit limit 1000, got %d ok / %d refused", okCount, failCount)
		}
		a, _ := l.Account("01JDORG", "org")
		if a.Reserved != 1000 || a.Available() != 0 {
			t.Fatalf("hard limit must be exactly exhausted: %+v", a)
		}
		if err := l.VerifyConservation(); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("hard-limit shortfall refuses M9-024 and books nothing", func(t *testing.T) {
		l := New()
		_ = l.SetLimit("01JDORG", "org", 100, baseTime)
		if _, err := l.Reserve("res-1", "01JDORG", "org", 150, baseTime.Add(time.Hour), baseTime); !errors.Is(err, ErrHardLimitExceeded) || Code(err) != "M9-024" {
			t.Fatalf("want M9-024, got %v", err)
		}
		a, _ := l.Account("01JDORG", "org")
		if a.Reserved != 0 || len(l.Journal()) != 1 { // only the limit line
			t.Fatalf("refused reservation must book nothing: %+v journal=%d", a, len(l.Journal()))
		}
	})

	t.Run("reservation defects refuse M9-023", func(t *testing.T) {
		l := New()
		_ = l.SetLimit("01JDORG", "org", 100, baseTime)
		if _, err := l.Reserve("res-x", "01JDORG", "missing-account", 10, baseTime.Add(time.Hour), baseTime); Code(err) != "M9-023" {
			t.Fatalf("unknown account: want M9-023, got %v", err)
		}
		if _, err := l.Reserve("res-x", "01JDORG", "org", 0, baseTime.Add(time.Hour), baseTime); Code(err) != "M9-023" {
			t.Fatalf("zero amount: want M9-023, got %v", err)
		}
		if _, err := l.Reserve("res-x", "01JDORG", "org", -5, baseTime.Add(time.Hour), baseTime); Code(err) != "M9-023" {
			t.Fatalf("negative amount: want M9-023, got %v", err)
		}
		if _, err := l.Reserve("res-x", "01JDORG", "org", 10, baseTime, baseTime); Code(err) != "M9-023" {
			t.Fatalf("already-expired: want M9-023, got %v", err)
		}
	})

	t.Run("T-16: duplicate settlement receipts never double-charge (M9-025)", func(t *testing.T) {
		l := New()
		_ = l.SetLimit("01JDORG", "org", 500, baseTime)
		if _, err := l.Reserve("res-1", "01JDORG", "org", 200, baseTime.Add(time.Hour), baseTime); err != nil {
			t.Fatal(err)
		}
		rcpt := Receipt{ReceiptID: "rc-1", ReservationID: "res-1", ActualAmount: 120, PayloadDigest: "d1"}
		for i := 0; i < 5; i++ { // 回执丢失重放
			if _, err := l.Settle(rcpt, baseTime.Add(time.Duration(i)*time.Minute)); err != nil {
				t.Fatalf("replay %d: %v", i, err)
			}
		}
		a, _ := l.Account("01JDORG", "org")
		if a.Settled != 120 {
			t.Fatalf("duplicate receipts charged once, settled=%d", a.Settled)
		}
		// same key, different payload → idempotency conflict
		bad := rcpt
		bad.PayloadDigest = "tampered"
		if _, err := l.Settle(bad, baseTime); !errors.Is(err, ErrIdempotencyConflict) || Code(err) != "M9-025" {
			t.Fatalf("want M9-025, got %v", err)
		}
		// reservation replay with different parameters → M9-025
		if _, err := l.Reserve("res-1", "01JDORG", "org", 999, baseTime.Add(time.Hour), baseTime); Code(err) != "M9-025" {
			t.Fatalf("reservation replay: want M9-025, got %v", err)
		}
		// a second receipt for an already-settled reservation → M9-025
		if _, err := l.Settle(Receipt{ReceiptID: "rc-2", ReservationID: "res-1", ActualAmount: 1, PayloadDigest: "d2"}, baseTime); Code(err) != "M9-025" {
			t.Fatalf("double settle: want M9-025, got %v", err)
		}
	})

	t.Run("over-consumption posts the actual spend and flags OverLimit", func(t *testing.T) {
		l := New()
		_ = l.SetLimit("01JDORG", "org", 200, baseTime)
		if _, err := l.Reserve("res-1", "01JDORG", "org", 100, baseTime.Add(time.Hour), baseTime); err != nil {
			t.Fatal(err)
		}
		// actual spend (250) overshoots the ceiling (200): posted, flagged, never clamped
		if _, err := l.Settle(Receipt{ReceiptID: "rc-1", ReservationID: "res-1", ActualAmount: 250, PayloadDigest: "d1"}, baseTime.Add(time.Minute)); err != nil {
			t.Fatal(err)
		}
		a, _ := l.Account("01JDORG", "org")
		if a.Settled != 250 || !a.OverLimit() || a.Reserved+a.Settled != 250 || a.Available() != -50 {
			t.Fatalf("actual spend posted: %+v overLimit=%v available=%d", a, a.OverLimit(), a.Available())
		}
		if err := l.VerifyConservation(); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("expired headroom releases; outcome_unknown parks and never guesses (S3/S9)", func(t *testing.T) {
		l := New()
		_ = l.SetLimit("01JDORG", "org", 300, baseTime)
		if _, err := l.Reserve("res-live", "01JDORG", "org", 100, baseTime.Add(time.Hour), baseTime); err != nil {
			t.Fatal(err)
		}
		if _, err := l.Reserve("res-stale", "01JDORG", "org", 100, baseTime.Add(time.Minute), baseTime); err != nil {
			t.Fatal(err)
		}
		if _, err := l.Reserve("res-unk", "01JDORG", "org", 100, baseTime.Add(time.Minute), baseTime); err != nil {
			t.Fatal(err)
		}
		// the unknown run parks instead of settling
		if _, err := l.Settle(Receipt{ReceiptID: "rc-unk", ReservationID: "res-unk", OutcomeUnknown: true, PayloadDigest: "d-unk"}, baseTime.Add(time.Second)); err != nil {
			t.Fatal(err)
		}
		after := baseTime.Add(2 * time.Minute)
		released := l.ReleaseExpired(after)
		if len(released) != 1 || released[0] != "res-stale" {
			t.Fatalf("only the stale reservation releases, got %v", released)
		}
		a, _ := l.Account("01JDORG", "org")
		if a.Reserved != 100 || a.Isolated != 100 || a.Available() != 100 {
			t.Fatalf("isolated must not be guess-released: %+v", a)
		}
		if r, _ := l.Reservation("res-unk"); r.State != ReservationOutcomeUnknown {
			t.Fatalf("res-unk parked, got %s", r.State)
		}
		// headroom freed by release is usable again — but isolated stays out
		if _, err := l.Reserve("res-new", "01JDORG", "org", 100, after.Add(time.Hour), after); err != nil {
			t.Fatalf("released headroom must be reusable: %v", err)
		}
		if _, err := l.Reserve("res-over", "01JDORG", "org", 1, after.Add(time.Hour), after); Code(err) != "M9-024" {
			t.Fatalf("isolated amount must count against headroom: got %v", err)
		}
		// a real receipt resolves the parked run (governance reconciliation)
		if _, err := l.Settle(Receipt{ReceiptID: "rc-real", ReservationID: "res-unk", ActualAmount: 90, PayloadDigest: "d-real"}, after); err != nil {
			t.Fatal(err)
		}
		a, _ = l.Account("01JDORG", "org")
		if a.Isolated != 0 || a.Settled != 90 {
			t.Fatalf("parked run resolved by real receipt: %+v", a)
		}
		if err := l.VerifyConservation(); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("conservation holds under concurrent duplicate and late traffic", func(t *testing.T) {
		l := New()
		_ = l.SetLimit("01JDORG", "org", 5000, baseTime)
		_ = l.SetLimit("01JDOTHER", "org", 5000, baseTime)

		var wg sync.WaitGroup
		// phase 1: concurrent duplicate reservations (same ids)
		for i := 0; i < 40; i++ {
			wg.Add(1)
			go func(i int) {
				defer wg.Done()
				for k := 0; k < 3; k++ { // duplicate injection
					_, _ = l.Reserve(fmt.Sprintf("res-%d", i), "01JDORG", "org", 250, baseTime.Add(time.Hour), baseTime)
				}
			}(i)
		}
		// interleaved: another org must be fully independent
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = l.Reserve("res-other", "01JDOTHER", "org", 250, baseTime.Add(time.Hour), baseTime)
		}()
		wg.Wait()

		org, _ := l.Account("01JDORG", "org")
		if org.Reserved != 20*250 { // 5000/250 = 20 fit
			t.Fatalf("org account reserved=%d, want 5000 (exactly 20 x 250)", org.Reserved)
		}
		other, _ := l.Account("01JDOTHER", "org")
		if other.Reserved != 250 {
			t.Fatalf("other org must be unaffected: %+v", other)
		}

		// phase 2: concurrent duplicate receipts + expiry release races
		for i := 0; i < 20; i++ {
			wg.Add(1)
			go func(i int) {
				defer wg.Done()
				id := fmt.Sprintf("res-%d", i)
				rcpt := Receipt{ReceiptID: fmt.Sprintf("rc-%d", i), ReservationID: id, ActualAmount: 100, PayloadDigest: "d"}
				_, _ = l.Settle(rcpt, baseTime.Add(time.Minute))
				_, _ = l.Settle(rcpt, baseTime.Add(2*time.Minute)) // duplicate
			}(i)
		}
		wg.Wait()
		l.ReleaseExpired(baseTime.Add(2 * time.Hour)) // release whatever expired unsettled

		if err := l.VerifyConservation(); err != nil {
			t.Fatalf("conservation broken under concurrent duplicate traffic: %v", err)
		}
		org, _ = l.Account("01JDORG", "org")
		if org.Reserved+org.Settled > org.HardLimit {
			t.Fatalf("hard limit overshot: %+v", org)
		}
	})

	t.Run("settled reservation idempotent re-reserve does not double book", func(t *testing.T) {
		l := New()
		_ = l.SetLimit("01JDORG", "org", 100, baseTime)
		if _, err := l.Reserve("res-1", "01JDORG", "org", 100, baseTime.Add(time.Hour), baseTime); err != nil {
			t.Fatal(err)
		}
		// exact replay is idempotent, not a conflict
		if _, err := l.Reserve("res-1", "01JDORG", "org", 100, baseTime.Add(time.Hour), baseTime); err != nil {
			t.Fatalf("exact replay must be idempotent: %v", err)
		}
		a, _ := l.Account("01JDORG", "org")
		if a.Reserved != 100 {
			t.Fatalf("replay must not double book: %+v", a)
		}
	})
}
