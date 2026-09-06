package m8app_test

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/lunitide/lunitide/internal/m8app"
)

func scopeBuiltin(t *testing.T, s *m8app.PluginService, id string) string {
	t.Helper()
	list, err := s.List(context.Background(), "", "")
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range list.Plugins {
		if item.PluginID == id {
			return item.InstallID
		}
	}
	t.Fatal("missing builtin " + id)
	return ""
}

func TestPluginCapabilityEpochCancelsOldOperationsOnly(t *testing.T) {
	s, _ := openPluginService(t)
	ctx := context.Background()
	if err := m8app.EnsureBuiltinPlugins(ctx, s); err != nil {
		t.Fatal(err)
	}
	workspace := scopeBuiltin(t, s, "workspace")
	memory := scopeBuiltin(t, s, "memory")
	op, release, err := s.AcquireCapability(ctx, "workspace", "filesystem")
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	if _, err = s.Toggle(ctx, m8app.ToggleInput{InstallID: memory, Enabled: false}); err != nil {
		t.Fatal(err)
	}
	if op.Err() != nil {
		t.Fatal("unrelated capability cancelled operation")
	}
	if _, err = s.Toggle(ctx, m8app.ToggleInput{InstallID: workspace, Enabled: false}); err != nil {
		t.Fatal(err)
	}
	select {
	case <-op.Done():
	case <-time.After(time.Second):
		t.Fatal("disable did not cancel old epoch")
	}
	if !errors.Is(context.Cause(op), m8app.ErrBindingInactive) {
		t.Fatal(context.Cause(op))
	}
	if _, err = s.Toggle(ctx, m8app.ToggleInput{InstallID: workspace, Enabled: true}); err != nil {
		t.Fatal(err)
	}
	next, releaseNext, err := s.AcquireCapability(ctx, "workspace")
	if err != nil {
		t.Fatal(err)
	}
	defer releaseNext()
	release()
	if next.Err() != nil {
		t.Fatal("old cleanup cancelled re-enabled epoch")
	}
	if _, err = s.Toggle(ctx, m8app.ToggleInput{InstallID: "01ARZ3NDEKTSV4RRFFQ69G5FZZ", Enabled: false}); err == nil {
		t.Fatal("invalid mutation succeeded")
	}
	if next.Err() != nil {
		t.Fatal("failed mutation cancelled a valid grant")
	}
}

type pauseGrantUOW struct {
	m8app.PluginUnitOfWork
	pause  atomic.Bool
	read   chan struct{}
	resume chan struct{}
}

func (u *pauseGrantUOW) TransactPlugin(ctx context.Context, fn func(m8app.PluginTx) error) error {
	paused := u.pause.CompareAndSwap(true, false)
	err := u.PluginUnitOfWork.TransactPlugin(ctx, fn)
	if paused {
		close(u.read)
		select {
		case <-u.resume:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return err
}

func TestPluginCapabilityEpochCatchesRevokeBetweenReadAndSubscribe(t *testing.T) {
	_, st := openPluginService(t)
	u := &pauseGrantUOW{PluginUnitOfWork: st.AgentRuntimeRepository(), read: make(chan struct{}), resume: make(chan struct{})}
	s := m8app.NewPluginService(u, "local-user")
	ctx := context.Background()
	if err := m8app.EnsureBuiltinPlugins(ctx, s); err != nil {
		t.Fatal(err)
	}
	id := scopeBuiltin(t, s, "workspace")
	u.pause.Store(true)
	done := make(chan error, 1)
	go func() { _, release, err := s.AcquireCapability(ctx, "workspace"); release(); done <- err }()
	select {
	case <-u.read:
	case <-time.After(time.Second):
		t.Fatal("grant read not reached")
	}
	if _, err := s.Toggle(ctx, m8app.ToggleInput{InstallID: id, Enabled: false}); err != nil {
		t.Fatal(err)
	}
	close(u.resume)
	select {
	case err := <-done:
		if !errors.Is(err, m8app.ErrBindingInactive) {
			t.Fatalf("stale read admitted: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("acquisition stranded")
	}
}
