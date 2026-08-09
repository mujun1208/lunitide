package ipc

import "sync"

type SessionGate struct {
	mu     sync.Mutex
	active int
	limit  int
}

func NewSessionGate(limit int) *SessionGate {
	return &SessionGate{limit: limit}
}

func (gate *SessionGate) TryEnter() (func(), bool) {
	gate.mu.Lock()
	defer gate.mu.Unlock()
	if gate.active >= gate.limit {
		return nil, false
	}
	gate.active++
	var once sync.Once
	return func() {
		once.Do(func() {
			gate.mu.Lock()
			gate.active--
			gate.mu.Unlock()
		})
	}, true
}
