package app

import (
	"context"
	"fmt"
	"sync"

	"github.com/lunitide/lunitide/internal/voice"
)

type scopedVoiceSession struct {
	session voice.Session
	scope   context.Context
	cancel  context.CancelFunc
	release func()
	mu      sync.Mutex
	once    sync.Once
}

func scopeVoiceCall(parent, scope context.Context) (context.Context, func()) {
	ctx, cancel := context.WithCancel(parent)
	stop := context.AfterFunc(scope, cancel)
	if scope.Err() != nil {
		cancel()
	}
	return ctx, func() { stop(); cancel() }
}
func (s *scopedVoiceSession) Append(ctx context.Context, pcm []byte) error {
	ctx, done := scopeVoiceCall(ctx, s.scope)
	defer done()
	s.mu.Lock()
	defer s.mu.Unlock()
	if ctx.Err() != nil {
		return ctx.Err()
	}
	err := s.session.Append(ctx, pcm)
	if ctx.Err() != nil {
		return ctx.Err()
	}
	return err
}
func (s *scopedVoiceSession) Finish(ctx context.Context) (string, error) {
	ctx, done := scopeVoiceCall(ctx, s.scope)
	defer done()
	s.mu.Lock()
	defer s.mu.Unlock()
	if ctx.Err() != nil {
		return "", ctx.Err()
	}
	text, err := s.session.Finish(ctx)
	if ctx.Err() != nil {
		return "", ctx.Err()
	}
	return text, err
}
func (s *scopedVoiceSession) Latest() (string, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.scope.Err() != nil {
		return "", false
	}
	if reader, ok := s.session.(interface{ Latest() (string, bool) }); ok {
		return reader.Latest()
	}
	return "", false
}
func (s *scopedVoiceSession) Close() error {
	s.cancel()
	var err error
	s.once.Do(func() { s.mu.Lock(); defer s.mu.Unlock(); err = s.session.Close(); s.release() })
	return err
}

func (e *Engine) startScopedVoice(ctx context.Context, start func(context.Context) (voice.Session, error)) (string, error) {
	parent, cancel := context.WithCancel(context.WithoutCancel(ctx))
	scoped, release, err := e.AcquireCapability(parent, "stt")
	if err != nil {
		cancel()
		return "", err
	}
	transferred := false
	defer func() {
		if !transferred {
			cancel()
			release()
		}
	}()
	op, finish := scopeVoiceCall(ctx, scoped)
	opened, err := start(op)
	finish()
	if err != nil {
		return "", err
	}
	if err = scoped.Err(); err == nil {
		err = ctx.Err()
	}
	if err != nil {
		_ = opened.Close()
		return "", err
	}
	session := &scopedVoiceSession{session: opened, scope: scoped, cancel: cancel, release: release}
	id := fmt.Sprintf("v%d", e.voice.counter.Add(1))
	e.voice.mu.Lock()
	e.voice.sessions[id] = session
	e.voice.mu.Unlock()
	transferred = true
	context.AfterFunc(scoped, func() { e.dropVoiceSession(id) })
	if err = scoped.Err(); err != nil {
		e.dropVoiceSession(id)
		return "", err
	}
	return id, nil
}
