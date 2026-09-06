package ccapp

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"
)

// executionFence serializes tools without holding a lock across OS calls.
// Revocation invalidates both the running operation and callers already queued.
type executionFence struct {
	once      sync.Once
	slot      chan struct{}
	mu        sync.Mutex
	epoch     uint64
	stopEpoch uint64
	stopped   bool
	cleaning  bool
	active    *executionScope
}

type executionScope struct {
	ctx        context.Context
	cancel     context.CancelCauseFunc
	settings   Settings
	dispatched bool
}

func (s *Service) beginExecution(ctx context.Context) (*executionScope, error) {
	f := &s.execution
	f.once.Do(func() { f.slot = make(chan struct{}, 1) })
	f.mu.Lock()
	epoch := f.epoch
	f.mu.Unlock()
	select {
	case f.slot <- struct{}{}:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := ctx.Err(); err != nil {
		<-f.slot
		return nil, err
	}
	if f.stopped || f.cleaning || epoch != f.epoch {
		<-f.slot
		if f.stopped || f.cleaning {
			return nil, ErrCcEmergency
		}
		return nil, ErrCcPermissionChanged
	}
	actx, cancel := context.WithCancelCause(ctx)
	op := &executionScope{ctx: actx, cancel: cancel}
	f.active = op
	return op, nil
}

func (s *Service) endExecution(op *executionScope) {
	// Releasing a modifier is cleanup and must remain possible after revocation.
	if context.Cause(op.ctx) != nil {
		s.releaseHeldKeys()
	}
	s.execution.mu.Lock()
	s.execution.active = nil
	s.execution.mu.Unlock()
	op.cancel(nil)
	<-s.execution.slot
}

func (s *Service) revokeExecution(cause error, stop bool) uint64 {
	f := &s.execution
	f.mu.Lock()
	defer f.mu.Unlock()
	f.epoch++
	if stop {
		f.stopped = true
		f.stopEpoch++
	}
	if f.active != nil {
		f.active.cancel(cause)
	}
	return f.epoch
}

func (s *Service) checkExecution() error {
	f := &s.execution
	f.mu.Lock()
	defer f.mu.Unlock()
	return s.checkExecutionLocked()
}

func (s *Service) checkExecutionLocked() error {
	f := &s.execution
	if f.stopped {
		return ErrCcEmergency
	}
	if f.active == nil {
		return nil
	} // Internal host-helper tests.
	if err := context.Cause(f.active.ctx); err != nil {
		return err
	}
	if armExpired(s.clock.Now(), f.active.settings.ArmedUntil) {
		return ErrCcDisabled
	}
	return nil
}

func (s *Service) checkProcess(process string) error {
	s.execution.mu.Lock()
	defer s.execution.mu.Unlock()
	if err := s.checkExecutionLocked(); err != nil {
		return err
	}
	if s.execution.active == nil {
		return nil
	}
	if strings.TrimSpace(process) == "" {
		return fmt.Errorf("%w: target process unavailable", ErrCcProcessBlocked)
	}
	if blocklistHit(s.execution.active.settings.ProcessBlocklist, process) {
		return ErrCcProcessBlocked
	}
	return nil
}

// dispatch marks the point beyond which an OS operation may already have
// happened. Cancellation prevents subsequent calls; it cannot undo this one.
func (s *Service) dispatch(foreground bool, fn func() error) error {
	if err := s.checkExecution(); err != nil {
		return err
	}
	if foreground {
		_, process, err := s.host.ActiveWindow()
		if err != nil {
			return fmt.Errorf("%w: cannot verify foreground: %v", ErrCcProcessBlocked, err)
		}
		if err := s.checkProcess(process); err != nil {
			return err
		}
	}
	s.execution.mu.Lock()
	if err := s.checkExecutionLocked(); err != nil {
		s.execution.mu.Unlock()
		return err
	}
	if s.execution.active != nil {
		s.execution.active.dispatched = true
	}
	s.execution.mu.Unlock()
	return fn()
}

func (s *Service) waitExecution(d time.Duration) error {
	s.execution.mu.Lock()
	ctx := context.Background()
	if s.execution.active != nil {
		ctx = s.execution.active.ctx
	}
	s.execution.mu.Unlock()
	if err := s.checkExecution(); err != nil {
		return err
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return context.Cause(ctx)
	case <-timer.C:
		return s.checkExecution()
	}
}

func executionFenceError(err error) bool {
	return errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) ||
		errors.Is(err, ErrCcEmergency) || errors.Is(err, ErrCcPermissionChanged) ||
		errors.Is(err, ErrCcDisabled) || errors.Is(err, ErrCcProcessBlocked)
}
