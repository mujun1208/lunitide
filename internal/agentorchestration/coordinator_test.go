package agentorchestration

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
)

func harness(t *testing.T, limits Limits) (*Coordinator, *MemoryRepository) {
	t.Helper()
	repo := NewMemoryRepository()
	var n atomic.Int64
	c, err := New(repo, limits, func() string { return fmt.Sprintf("run-%d", n.Add(1)) })
	if err != nil {
		t.Fatal(err)
	}
	return c, repo
}
func todo(id string) Todo {
	return Todo{ID: id, Title: "todo " + id, Metadata: map[string]string{"key": "value"}}
}
func root(t *testing.T, c *Coordinator) AgentRun {
	t.Helper()
	r, err := c.CreateRoot(context.Background(), "plan-1", "node-root", "planner", todo("t-root"))
	if err != nil {
		t.Fatal(err)
	}
	return r
}
func start(t *testing.T, c *Coordinator, id string) {
	t.Helper()
	if _, err := c.Start(context.Background(), id); err != nil {
		t.Fatal(err)
	}
}

func TestSpawnAndJoinAll(t *testing.T) {
	c, _ := harness(t, Limits{MaxDepth: 3, MaxConcurrency: 8})
	p := root(t, c)
	start(t, c, p.ID)
	a, err := c.SpawnChild(context.Background(), p.ID, "node-a", "researcher", todo("a"))
	if err != nil {
		t.Fatal(err)
	}
	b, err := c.SpawnChild(context.Background(), p.ID, "node-b", "writer", todo("b"))
	if err != nil {
		t.Fatal(err)
	}
	if a.ParentRunID != p.ID || a.PlanID != p.PlanID || a.Depth != 1 || b.Role != "writer" {
		t.Fatalf("child linkage incorrect: %#v %#v", a, b)
	}
	start(t, c, a.ID)
	start(t, c, b.ID)
	if _, err = c.Complete(context.Background(), a.ID); err != nil {
		t.Fatal(err)
	}
	got, err := c.JoinChildren(context.Background(), p.ID, JoinAll)
	if err != nil || got.Status != StatusJoining {
		t.Fatalf("join pending=%s %v", got.Status, err)
	}
	if _, err = c.Complete(context.Background(), b.ID); err != nil {
		t.Fatal(err)
	}
	got, err = c.JoinChildren(context.Background(), p.ID, JoinAll)
	if err != nil || got.Status != StatusSucceeded || got.TerminalAt == nil {
		t.Fatalf("join result=%#v %v", got, err)
	}
}

func TestJoinAnyAndChildFailure(t *testing.T) {
	t.Run("any succeeds early", func(t *testing.T) {
		c, _ := harness(t, Limits{2, 5})
		p := root(t, c)
		start(t, c, p.ID)
		a, _ := c.SpawnChild(context.Background(), p.ID, "a", "r", todo("a"))
		_, _ = c.SpawnChild(context.Background(), p.ID, "b", "r", todo("b"))
		start(t, c, a.ID)
		_, _ = c.Complete(context.Background(), a.ID)
		got, err := c.JoinChildren(context.Background(), p.ID, JoinAny)
		if err != nil || got.Status != StatusSucceeded {
			t.Fatalf("%s %v", got.Status, err)
		}
	})
	t.Run("all propagates failure", func(t *testing.T) {
		c, _ := harness(t, Limits{2, 5})
		p := root(t, c)
		start(t, c, p.ID)
		a, _ := c.SpawnChild(context.Background(), p.ID, "a", "r", todo("a"))
		start(t, c, a.ID)
		_, _ = c.Fail(context.Background(), a.ID, "boom")
		got, err := c.JoinChildren(context.Background(), p.ID, JoinAll)
		if err != nil || got.Status != StatusFailed || got.Failure == "" {
			t.Fatalf("%#v %v", got, err)
		}
	})
}

func TestDepthAndConcurrencyLimits(t *testing.T) {
	t.Run("depth", func(t *testing.T) {
		c, _ := harness(t, Limits{MaxDepth: 1, MaxConcurrency: 10})
		p := root(t, c)
		start(t, c, p.ID)
		child, err := c.SpawnChild(context.Background(), p.ID, "child", "r", todo("c"))
		if err != nil {
			t.Fatal(err)
		}
		start(t, c, child.ID)
		if _, err = c.SpawnChild(context.Background(), child.ID, "grand", "r", todo("g")); !errors.Is(err, ErrDepthLimit) {
			t.Fatalf("got %v", err)
		}
	})
	t.Run("concurrency", func(t *testing.T) {
		c, _ := harness(t, Limits{MaxDepth: 2, MaxConcurrency: 2})
		p := root(t, c)
		start(t, c, p.ID)
		a, err := c.SpawnChild(context.Background(), p.ID, "a", "r", todo("a"))
		if err != nil {
			t.Fatal(err)
		}
		if _, err = c.SpawnChild(context.Background(), p.ID, "b", "r", todo("b")); !errors.Is(err, ErrConcurrencyLimit) {
			t.Fatalf("got %v", err)
		}
		if _, err = c.CancelRun(context.Background(), a.ID); err != nil {
			t.Fatal(err)
		}
		if _, err = c.SpawnChild(context.Background(), p.ID, "b", "r", todo("b")); err != nil {
			t.Fatalf("slot not released: %v", err)
		}
	})
}

func TestRecursiveCancellation(t *testing.T) {
	c, _ := harness(t, Limits{4, 10})
	p := root(t, c)
	start(t, c, p.ID)
	a, _ := c.SpawnChild(context.Background(), p.ID, "a", "r", todo("a"))
	start(t, c, a.ID)
	b, _ := c.SpawnChild(context.Background(), a.ID, "b", "r", todo("b"))
	got, err := c.CancelRun(context.Background(), p.ID)
	if err != nil || got.Status != StatusCancelRequested {
		t.Fatalf("root %s %v", got.Status, err)
	}
	a, _ = c.Get(context.Background(), a.ID)
	b, _ = c.Get(context.Background(), b.ID)
	if a.Status != StatusCancelRequested || b.Status != StatusCancelled {
		t.Fatalf("descendants: %s %s", a.Status, b.Status)
	}
	if _, err = c.AcknowledgeCancellation(context.Background(), a.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = c.AcknowledgeCancellation(context.Background(), p.ID); err != nil {
		t.Fatal(err)
	}
}

func TestTerminalRaceAndIdempotency(t *testing.T) {
	c, _ := harness(t, Limits{1, 2})
	r := root(t, c)
	start(t, c, r.ID)
	var wg sync.WaitGroup
	wg.Add(2)
	errs := make(chan error, 2)
	go func() { defer wg.Done(); _, e := c.Complete(context.Background(), r.ID); errs <- e }()
	go func() { defer wg.Done(); _, e := c.Fail(context.Background(), r.ID, "race"); errs <- e }()
	wg.Wait()
	close(errs)
	successes := 0
	for e := range errs {
		if e == nil {
			successes++
		} else if !errors.Is(e, ErrInvalidTransition) {
			t.Fatalf("unexpected %v", e)
		}
	}
	if successes != 1 {
		t.Fatalf("terminal successes=%d", successes)
	}
	final, _ := c.Get(context.Background(), r.ID)
	events, _ := c.Events(context.Background(), r.ID)
	before := len(events)
	var err error
	if final.Status == StatusSucceeded {
		_, err = c.Complete(context.Background(), r.ID)
	} else {
		_, err = c.Fail(context.Background(), r.ID, "race")
	}
	if err != nil {
		t.Fatalf("same terminal retry: %v", err)
	}
	events, _ = c.Events(context.Background(), r.ID)
	if len(events) != before {
		t.Fatalf("idempotent retry appended event")
	}
}

func TestEventOrdering(t *testing.T) {
	c, _ := harness(t, Limits{2, 4})
	p := root(t, c)
	start(t, c, p.ID)
	a, _ := c.SpawnChild(context.Background(), p.ID, "a", "r", todo("a"))
	start(t, c, a.ID)
	_, _ = c.Complete(context.Background(), a.ID)
	_, _ = c.JoinChildren(context.Background(), p.ID, JoinAll)
	events, err := c.Events(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	if len(events) < 6 {
		t.Fatalf("events=%d", len(events))
	}
	for i, e := range events {
		if e.Sequence != uint64(i+1) {
			t.Fatalf("event %d sequence=%d", i, e.Sequence)
		}
	}
}

func TestRestartReconciliation(t *testing.T) {
	c, _ := harness(t, Limits{3, 8})
	p := root(t, c)
	start(t, c, p.ID)
	a, _ := c.SpawnChild(context.Background(), p.ID, "a", "r", todo("a"))
	start(t, c, a.ID)
	_, _ = c.CancelRun(context.Background(), a.ID)
	if err := c.ReconcileRestart(context.Background()); err != nil {
		t.Fatal(err)
	}
	p, _ = c.Get(context.Background(), p.ID)
	a, _ = c.Get(context.Background(), a.ID)
	if p.Status != StatusQueued || a.Status != StatusCancelled {
		t.Fatalf("recovery: parent=%s child=%s", p.Status, a.Status)
	}
	e, _ := c.Events(context.Background(), "")
	before := len(e)
	if err := c.ReconcileRestart(context.Background()); err != nil {
		t.Fatal(err)
	}
	e, _ = c.Events(context.Background(), "")
	if len(e) != before {
		t.Fatalf("recovery not idempotent: %d -> %d", before, len(e))
	}
}

func TestMemoryRepositoryRollsBackFailedTransaction(t *testing.T) {
	r := NewMemoryRepository()
	sentinel := errors.New("abort")
	_ = r.Transact(context.Background(), func(tx Transaction) error { tx.Put(AgentRun{ID: "x"}); tx.Append(Event{RunID: "x"}); return sentinel })
	_ = r.Transact(context.Background(), func(tx Transaction) error {
		if _, ok := tx.Get("x"); ok {
			t.Fatal("run committed")
		}
		if len(tx.ListEvents("")) != 0 {
			t.Fatal("event committed")
		}
		return nil
	})
}
