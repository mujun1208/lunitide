package audit

import (
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"
)

var t0 = time.Date(2026, 8, 16, 12, 0, 0, 0, time.UTC)

func ev(id, actor, action string) Event {
	return Event{ID: id, Actor: actor, Action: action, ResourceType: "doc", ResourceID: "r-" + id}
}

func TestOrgChain(t *testing.T) {
	t.Run("partitions chain independently per org (seq restarts, no cross-org linkage)", func(t *testing.T) {
		l := NewOrgLedger()
		for i := 1; i <= 3; i++ {
			if _, err := l.Append("01JDORG", ev(fmt.Sprintf("a-%d", i), "alice", "doc.update"), t0); err != nil {
				t.Fatal(err)
			}
		}
		if _, err := l.Append("01JDOTHER", ev("b-1", "bob", "doc.update"), t0); err != nil {
			t.Fatal(err)
		}
		if err := l.Verify("01JDORG"); err != nil {
			t.Fatal(err)
		}
		if err := l.Verify("01JDOTHER"); err != nil {
			t.Fatal(err)
		}
		if errs := l.VerifyAll(); len(errs) != 0 {
			t.Fatalf("verify all: %v", errs)
		}
		other := l.Partition("01JDOTHER")
		if len(other) != 1 || other[0].Seq != 1 || other[0].PrevHash != GenesisPrev {
			t.Fatalf("second org must have its own genesis chain: %+v", other[0])
		}
		org := l.Partition("01JDORG")
		if org[2].Seq != 3 || org[2].PrevHash != org[1].EventHash {
			t.Fatalf("first org chains internally: %+v", org[2])
		}
	})

	t.Run("duplicate event id across partitions is refused", func(t *testing.T) {
		l := NewOrgLedger()
		if _, err := l.Append("01JDORG", ev("dup-1", "alice", "doc.update"), t0); err != nil {
			t.Fatal(err)
		}
		if _, err := l.Append("01JDOTHER", ev("dup-1", "bob", "doc.update"), t0); err == nil {
			t.Fatal("event ids must be globally unique across partitions")
		}
	})

	t.Run("T-17: deletion breaks the chain (M9-026)", func(t *testing.T) {
		l := NewOrgLedger()
		for i := 1; i <= 4; i++ {
			_, _ = l.Append("01JDORG", ev(fmt.Sprintf("d-%d", i), "alice", "doc.update"), t0)
		}
		l.mu.Lock()
		removed := append([]Event{}, l.partitions["01JDORG"][0], l.partitions["01JDORG"][2], l.partitions["01JDORG"][3])
		l.partitions["01JDORG"] = removed // attacker deletes seq 2
		l.mu.Unlock()
		err := l.Verify("01JDORG")
		if !errors.Is(err, ErrChainInvalid) || M9Code(err) != "M9-026" {
			t.Fatalf("deletion: want M9-026, got %v", err)
		}
	})

	t.Run("T-17: reordering breaks the chain (M9-026)", func(t *testing.T) {
		l := NewOrgLedger()
		for i := 1; i <= 3; i++ {
			_, _ = l.Append("01JDORG", ev(fmt.Sprintf("o-%d", i), "alice", "doc.update"), t0)
		}
		l.mu.Lock()
		p := l.partitions["01JDORG"]
		p[0], p[1] = p[1], p[0] // swap seq 1 and 2
		l.mu.Unlock()
		if err := l.Verify("01JDORG"); !errors.Is(err, ErrChainInvalid) || M9Code(err) != "M9-026" {
			t.Fatalf("reorder: want M9-026, got %v", err)
		}
	})

	t.Run("T-17: field tampering breaks the chain (M9-026)", func(t *testing.T) {
		l := NewOrgLedger()
		for i := 1; i <= 3; i++ {
			_, _ = l.Append("01JDORG", ev(fmt.Sprintf("t-%d", i), "alice", "doc.update"), t0)
		}
		l.mu.Lock()
		l.partitions["01JDORG"][1].Action = "doc.view" // insider rewrites history
		l.mu.Unlock()
		if err := l.Verify("01JDORG"); !errors.Is(err, ErrChainInvalid) || M9Code(err) != "M9-026" {
			t.Fatalf("tamper: want M9-026, got %v", err)
		}
	})

	t.Run("VerifyAll isolates one tenant's broken chain from the others", func(t *testing.T) {
		l := NewOrgLedger()
		_, _ = l.Append("01JDA", ev("a-1", "alice", "doc.update"), t0)
		_, _ = l.Append("01JDB", ev("b-1", "bob", "doc.update"), t0)
		_, _ = l.Append("01JDB", ev("b-2", "bob", "doc.update"), t0)
		l.mu.Lock()
		l.partitions["01JDA"][0].Actor = "mallory"
		l.mu.Unlock()
		errs := l.VerifyAll()
		if len(errs) != 1 || M9Code(errs[0]) != "M9-026" {
			t.Fatalf("only org A must fail: %v", errs)
		}
		if err := l.Verify("01JDB"); err != nil {
			t.Fatalf("org B must stay verifiable: %v", err)
		}
	})

	t.Run("M9-027: export refuses missing, expired, mismatched and reused grants", func(t *testing.T) {
		l := NewOrgLedger()
		_, _ = l.Append("01JDORG", ev("e-1", "auditor", "doc.update"), t0)

		if _, err := l.Export("", "01JDORG", "auditor", t0); !errors.Is(err, ErrExportDenied) || M9Code(err) != "M9-027" {
			t.Fatalf("no grant: want M9-027, got %v", err)
		}
		if _, err := l.Export("ghost", "01JDORG", "auditor", t0); M9Code(err) != "M9-027" {
			t.Fatalf("unknown grant: want M9-027, got %v", err)
		}

		if err := l.IssueGrant(ExportGrant{GrantID: "g-old", OrgID: "01JDORG", Actor: "auditor", ExpiresAt: t0.Add(-time.Hour)}, t0); M9Code(err) != "M9-027" {
			t.Fatalf("expired-at-issue grant: want M9-027, got %v", err)
		}

		_ = l.IssueGrant(ExportGrant{GrantID: "g-1", OrgID: "01JDORG", Actor: "auditor", Reason: "quarterly-review", ExpiresAt: t0.Add(time.Hour)}, t0)
		if _, err := l.Export("g-1", "01JDORG", "auditor", t0.Add(2*time.Hour)); M9Code(err) != "M9-027" {
			t.Fatalf("expired grant: want M9-027, got %v", err)
		}
		if _, err := l.Export("g-1", "01JDOTHER", "auditor", t0); M9Code(err) != "M9-027" {
			t.Fatalf("org mismatch: want M9-027, got %v", err)
		}
		if _, err := l.Export("g-1", "01JDORG", "intruder", t0); M9Code(err) != "M9-027" {
			t.Fatalf("actor mismatch: want M9-027, got %v", err)
		}

		// first legitimate export consumes the grant
		if _, err := l.Export("g-1", "01JDORG", "auditor", t0); err != nil {
			t.Fatal(err)
		}
		if _, err := l.Export("g-1", "01JDORG", "auditor", t0); M9Code(err) != "M9-027" {
			t.Fatalf("grant reuse must demand re-authorization: want M9-027, got %v", err)
		}
	})

	t.Run("export is independently recomputable and leaves a trail (T-17/M9-027)", func(t *testing.T) {
		l := NewOrgLedger()
		for i := 1; i <= 3; i++ {
			_, _ = l.Append("01JDORG", ev(fmt.Sprintf("x-%d", i), "alice", "doc.update"), t0)
		}
		_ = l.IssueGrant(ExportGrant{GrantID: "g-ok", OrgID: "01JDORG", Actor: "auditor", Reason: "litigation-42", ExpiresAt: t0.Add(time.Hour)}, t0)
		exp, err := l.Export("g-ok", "01JDORG", "auditor", t0.Add(time.Minute))
		if err != nil {
			t.Fatal(err)
		}

		// 独立复算: a third party verifies the bundle with the public kernel
		if err := VerifyChain(exp.Events); err != nil {
			t.Fatalf("exported bundle must independently verify: %v", err)
		}
		last := exp.Events[len(exp.Events)-1]
		if exp.HeadHash != last.EventHash {
			t.Fatalf("head hash must match the shipped tail: %s != %s", exp.HeadHash, last.EventHash)
		}

		// 导出留痕: the trail event is part of the exported chain itself
		if last.Action != "audit.export" || last.Actor != "auditor" || last.CorrelationID != "g-ok" {
			t.Fatalf("export trail missing: %+v", last)
		}
		// tampering the exported bundle breaks independent recomputation
		tampered := append([]Event{}, exp.Events...)
		tampered[0].Action = "doc.view"
		if err := VerifyChain(tampered); err == nil {
			t.Fatal("tampered export must fail independent verification")
		}
	})

	t.Run("broken chain blocks export (M9-026 refuses before M9-027 shipping)", func(t *testing.T) {
		l := NewOrgLedger()
		_, _ = l.Append("01JDORG", ev("y-1", "alice", "doc.update"), t0)
		_, _ = l.Append("01JDORG", ev("y-2", "alice", "doc.update"), t0)
		l.mu.Lock()
		l.partitions["01JDORG"][0].AfterDigest = "forged"
		l.mu.Unlock()
		_ = l.IssueGrant(ExportGrant{GrantID: "g-broken", OrgID: "01JDORG", Actor: "auditor", ExpiresAt: t0.Add(time.Hour)}, t0)
		_, err := l.Export("g-broken", "01JDORG", "auditor", t0)
		if !errors.Is(err, ErrChainInvalid) || M9Code(err) != "M9-026" {
			t.Fatalf("export of broken chain: want M9-026, got %v", err)
		}
	})

	t.Run("signed checkpoints verify and detect forgery", func(t *testing.T) {
		l := NewOrgLedger()
		_, _ = l.Append("01JDORG", ev("c-1", "alice", "doc.update"), t0)
		_, _ = l.Append("01JDORG", ev("c-2", "alice", "doc.update"), t0)
		pub, priv, err := ed25519.GenerateKey(rand.Reader)
		if err != nil {
			t.Fatal(err)
		}
		cp, err := l.Checkpoint("01JDORG", "k-2026-08", priv, t0.Add(time.Minute))
		if err != nil {
			t.Fatal(err)
		}
		if err := VerifyCheckpoint(*cp, pub); err != nil {
			t.Fatal(err)
		}
		// checkpoint must bind the true head
		head := l.Partition("01JDORG")[1]
		if cp.Seq != head.Seq || cp.HeadHash != head.EventHash {
			t.Fatalf("checkpoint binds the partition head: %+v vs %+v", cp, head)
		}
		// forged head hash fails signature verification with M9-026
		bad := *cp
		bad.HeadHash = GenesisPrev
		if err := VerifyCheckpoint(bad, pub); !errors.Is(err, ErrChainInvalid) || M9Code(err) != "M9-026" {
			t.Fatalf("forged checkpoint: want M9-026, got %v", err)
		}
		// wrong key fails too
		otherPub, _, _ := ed25519.GenerateKey(rand.Reader)
		if err := VerifyCheckpoint(*cp, otherPub); M9Code(err) != "M9-026" {
			t.Fatalf("wrong key: want M9-026, got %v", err)
		}
	})

	t.Run("concurrent appends keep every partition gap-free", func(t *testing.T) {
		l := NewOrgLedger()
		const workers, per = 16, 20
		var wg sync.WaitGroup
		for w := 0; w < workers; w++ {
			org := fmt.Sprintf("01JDW%02d", w%2) // two orgs, hot partitions
			wg.Add(1)
			go func(w int, org string) {
				defer wg.Done()
				for i := 0; i < per; i++ {
					if _, err := l.Append(org, ev(fmt.Sprintf("r-%d-%d", w, i), "worker", "doc.update"), t0); err != nil {
						t.Errorf("append: %v", err)
						return
					}
				}
			}(w, org)
		}
		wg.Wait()
		if errs := l.VerifyAll(); len(errs) != 0 {
			t.Fatalf("concurrent chains must verify: %v", errs)
		}
		for _, org := range l.Orgs() {
			if got := len(l.Partition(org)); got != workers/2*per {
				t.Fatalf("org %s: %d events, want %d", org, got, workers/2*per)
			}
		}
	})
}
