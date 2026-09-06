package m8app

import (
	"context"
	"fmt"
	"sync"
)

type pluginGrantState struct {
	epoch uint64
	calls map[uint64]context.CancelCauseFunc
}

// AcquireCapability binds an operation to the current committed grant epoch.
// Revocation cancels every operation holding the old epoch before returning.
// Database reads run outside grantMu: registrars may themselves use a gate.
func (s *PluginService) AcquireCapability(ctx context.Context, ids ...string) (context.Context, func(), error) {
	if len(ids) == 0 {
		return ctx, func() {}, nil
	}
	if s == nil || s.uow == nil {
		return ctx, func() {}, ErrServiceUnavailable
	}
	unique := make([]string, 0, len(ids))
	seen := map[string]bool{}
	for _, id := range ids {
		if id != "" && !seen[id] {
			unique = append(unique, id)
			seen[id] = true
		}
	}
	for {
		if err := ctx.Err(); err != nil {
			return ctx, func() {}, err
		}
		s.grantMu.Lock()
		epochs := make(map[string]uint64, len(unique))
		for _, id := range unique {
			epochs[id] = s.grantState(id).epoch
		}
		s.grantMu.Unlock()
		if err := s.RequireEnabled(ctx, unique...); err != nil {
			return ctx, func() {}, err
		}
		s.grantMu.Lock()
		changed := false
		for _, id := range unique {
			if epochs[id] != s.grantState(id).epoch {
				changed = true
				break
			}
		}
		if changed {
			s.grantMu.Unlock()
			continue
		}
		s.grantSequence++
		callID := s.grantSequence
		op, cancel := context.WithCancelCause(ctx)
		for _, id := range unique {
			s.grantState(id).calls[callID] = cancel
		}
		s.grantMu.Unlock()
		var once sync.Once
		release := func() {
			once.Do(func() {
				s.grantMu.Lock()
				for _, id := range unique {
					delete(s.grantState(id).calls, callID)
				}
				s.grantMu.Unlock()
				cancel(nil)
			})
		}
		return op, release, nil
	}
}

// grantState is only accessed while holding grantMu.
func (s *PluginService) grantState(id string) *pluginGrantState {
	if s.grants == nil {
		s.grants = map[string]*pluginGrantState{}
	}
	state := s.grants[id]
	if state == nil {
		state = &pluginGrantState{calls: map[uint64]context.CancelCauseFunc{}}
		s.grants[id] = state
	}
	return state
}

func (s *PluginService) invalidateCapability(id string) {
	if id == "" {
		return
	}
	s.grantMu.Lock()
	state := s.grantState(id)
	state.epoch++
	calls := state.calls
	state.calls = map[uint64]context.CancelCauseFunc{}
	s.grantMu.Unlock()
	cause := fmt.Errorf("%w: %s grant changed", ErrBindingInactive, id)
	for _, cancel := range calls {
		cancel(cause)
	}
}
