package bridge

import (
	"context"
	"time"
)

// SlotGate limits in-flight RPCs without fail-fast starving cancel/listen.
//
// Voice companion holds several long Calls at once (voice.append, tts.synthesize).
// A single fail-fast pool turned interrupt and the next spoken turn into
// HOST_BUSY / ENGINE_BUSY. chat.start shares the control lane with cancel.
type SlotGate struct {
	general chan struct{}
	control chan struct{}
	wait    time.Duration
}

const (
	DefaultGeneralSlots = 32
	DefaultControlSlots = 8
	DefaultSlotWait     = 2 * time.Second
)

func NewSlotGate(general, control int, wait time.Duration) *SlotGate {
	if general < 1 {
		general = DefaultGeneralSlots
	}
	if control < 1 {
		control = DefaultControlSlots
	}
	if wait < 0 {
		wait = 0
	}
	return &SlotGate{
		general: make(chan struct{}, general),
		control: make(chan struct{}, control),
		wait:    wait,
	}
}

// ControlRPC is the interrupt/listen/turn lane: cancel, voice session
// open/close, and chat.start must not queue behind a full synthesize/append pool.
func ControlRPC(method string) bool {
	switch Method(method) {
	case MethodStreamCancel, MethodTtsCancel, MethodVoiceStop, MethodVoiceFinish, MethodVoiceStart, MethodChatStart:
		return true
	default:
		return false
	}
}

func (g *SlotGate) pool(method string) chan struct{} {
	if ControlRPC(method) {
		return g.control
	}
	return g.general
}

// Acquire takes a slot for method, waiting up to the gate's budget.
func (g *SlotGate) Acquire(ctx context.Context, method string) bool {
	pool := g.pool(method)
	select {
	case pool <- struct{}{}:
		return true
	default:
	}
	if g.wait == 0 || ctx.Err() != nil {
		return false
	}
	timer := time.NewTimer(g.wait)
	defer timer.Stop()
	select {
	case pool <- struct{}{}:
		return true
	case <-ctx.Done():
		return false
	case <-timer.C:
		return false
	}
}

func (g *SlotGate) Release(method string) {
	<-g.pool(method)
}
