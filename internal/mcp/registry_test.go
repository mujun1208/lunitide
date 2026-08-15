package mcp

import (
	"context"
	"errors"
	"testing"
	"time"
)

// fakeClock freezes time for breaker-window tests.
type fakeClock struct{ now time.Time }

func (f *fakeClock) Now() time.Time { return f.now }

func TestMcpReadonly(t *testing.T) {
	t.Run("non read-only declaration refuses the whole set", func(t *testing.T) {
		_, err := NewRegistry([]ToolDecl{
			{Name: "search", Endpoint: "ep1", ReadOnly: true},
			{Name: "destroy", Endpoint: "ep1", ReadOnly: false},
		})
		if !errors.Is(err, ErrToolNotReadOnly) {
			t.Fatalf("want ErrToolNotReadOnly, got %v", err)
		}
	})

	t.Run("invalid or duplicate names refused", func(t *testing.T) {
		if _, err := NewRegistry([]ToolDecl{{Name: "bad name", Endpoint: "ep1", ReadOnly: true}}); !errors.Is(err, ErrToolNameInvalid) {
			t.Fatalf("want ErrToolNameInvalid, got %v", err)
		}
		if _, err := NewRegistry([]ToolDecl{
			{Name: "dup", Endpoint: "ep1", ReadOnly: true},
			{Name: "dup", Endpoint: "ep2", ReadOnly: true},
		}); !errors.Is(err, ErrToolDuplicate) {
			t.Fatalf("want ErrToolDuplicate, got %v", err)
		}
	})

	t.Run("listed tools are the read-only set", func(t *testing.T) {
		reg, err := NewRegistry([]ToolDecl{
			{Name: "search", Endpoint: "ep1", ReadOnly: true, Description: "search docs"},
			{Name: "fetch", Endpoint: "ep1", ReadOnly: true},
		})
		if err != nil {
			t.Fatalf("NewRegistry: %v", err)
		}
		tools := reg.ListTools()
		if len(tools) != 2 || tools[0].Name != "fetch" || tools[1].Name != "search" {
			t.Fatalf("unexpected catalogue: %+v", tools)
		}
		for _, d := range tools {
			if !d.ReadOnly {
				t.Fatalf("write tool leaked into the catalogue: %+v", d)
			}
		}
	})

	t.Run("unregistered tool answers not-found", func(t *testing.T) {
		reg, err := NewRegistry([]ToolDecl{{Name: "ping", Endpoint: "ep1", ReadOnly: true}})
		if err != nil {
			t.Fatalf("NewRegistry: %v", err)
		}
		if _, err := reg.Invoke(context.Background(), nil, "absent", nil); !errors.Is(err, ErrToolNotFound) {
			t.Fatalf("want ErrToolNotFound, got %v", err)
		}
	})

	t.Run("mid-streak success resets the count", func(t *testing.T) {
		reg, err := NewRegistry([]ToolDecl{{Name: "ping", Endpoint: "ep1", ReadOnly: true}})
		if err != nil {
			t.Fatalf("NewRegistry: %v", err)
		}
		for i := 0; i < 3; i++ {
			reg.RecordFailure("ep1")
		}
		reg.RecordSuccess("ep1")
		for i := 0; i < 4; i++ {
			reg.RecordFailure("ep1")
		}
		// Four consecutive failures (< 5) keep the breaker closed; reaching
		// the nil-client check proves execution passed the breaker.
		if _, err := reg.Invoke(context.Background(), nil, "ping", nil); !errors.Is(err, ErrClientUnavailable) {
			t.Fatalf("breaker must stay closed after a reset, got %v", err)
		}
	})
}

func TestBreaker(t *testing.T) {
	reg, err := NewRegistry([]ToolDecl{{Name: "ping", Endpoint: "ep1", ReadOnly: true}})
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}
	fc := &fakeClock{now: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}
	reg.SetClock(fc)

	for i := 0; i < BreakerThreshold; i++ {
		reg.RecordFailure("ep1")
	}
	if _, err := reg.Invoke(context.Background(), nil, "ping", nil); !errors.Is(err, ErrBreakerOpen) {
		t.Fatalf("want ErrBreakerOpen after %d consecutive failures, got %v", BreakerThreshold, err)
	}

	fc.now = fc.now.Add(BreakerCooldown + time.Second)
	if _, err := reg.Invoke(context.Background(), nil, "ping", nil); !errors.Is(err, ErrClientUnavailable) {
		t.Fatalf("breaker must release after the cooldown, got %v", err)
	}
}
