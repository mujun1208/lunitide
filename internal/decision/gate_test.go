package decision

import (
	"errors"
	"sync"
	"testing"
)

func TestGate(t *testing.T) {
	t.Run("M9-031: fresh gate blocks every production feature and names the first decision", func(t *testing.T) {
		g := NewGate()
		if g.Open() {
			t.Fatal("fresh gate must be closed (阻断期)")
		}
		err := g.RequireOpen("schema-migration")
		if !errors.Is(err, ErrADR011Blocked) || Code(err) != "M9-031" {
			t.Fatalf("want M9-031, got %v", err)
		}
		if g.FirstBlocking() != "ADR-011" {
			t.Fatalf("first blocking decision must be ADR-011, got %q", g.FirstBlocking())
		}
	})

	t.Run("gate opens only after every required decision is accepted", func(t *testing.T) {
		g := NewGate()
		for _, id := range []string{"D-2", "D-3", "D-4", "D-5", "D-6"} {
			if err := g.Accept(id); err != nil {
				t.Fatal(err)
			}
			if err := g.RequireOpen("runner-dispatch"); !errors.Is(err, ErrADR011Blocked) {
				t.Fatalf("gate must stay closed until ADR-011, got %v", err)
			}
		}
		if err := g.Accept("ADR-011"); err != nil {
			t.Fatal(err)
		}
		if err := g.RequireOpen("runner-dispatch"); err != nil {
			t.Fatalf("fully accepted gate must open, got %v", err)
		}
		if !g.Open() {
			t.Fatal("Open must report true")
		}
	})

	t.Run("rejection re-blocks the gate and unknown ids refuse", func(t *testing.T) {
		g := NewGate()
		for _, id := range Required {
			_ = g.Accept(id)
		}
		if err := g.Reject("D-4"); err != nil {
			t.Fatal(err)
		}
		if err := g.RequireOpen("registry"); !errors.Is(err, ErrADR011Blocked) || g.FirstBlocking() != "D-4" {
			t.Fatalf("rejection must re-block naming D-4, got %v / %q", err, g.FirstBlocking())
		}
		if err := g.Accept("D-9"); err == nil {
			t.Fatal("unknown decision id must refuse")
		}
	})

	t.Run("concurrent RequireOpen stays consistent (gate is shared)", func(t *testing.T) {
		g := NewGate()
		_ = g.Accept("ADR-011")
		var wg sync.WaitGroup
		for i := 0; i < 16; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				_ = g.RequireOpen("parallel-feature")
				_ = g.Snapshot()
			}()
		}
		wg.Wait()
	})
}
