package bridge

import (
	"context"
	"testing"
	"time"
)

func TestControlRPCClassifiesCancelAndVoiceLifecycle(t *testing.T) {
	t.Parallel()
	if !ControlRPC(string(MethodStreamCancel)) || !ControlRPC(string(MethodTtsCancel)) {
		t.Fatal("cancel methods must use the control lane")
	}
	if !ControlRPC(string(MethodVoiceStart)) || !ControlRPC(string(MethodVoiceStop)) {
		t.Fatal("voice start/stop must use the control lane")
	}
	if !ControlRPC(string(MethodChatStart)) {
		t.Fatal("chat.start must use the control lane so a spoken turn is not HOST_BUSY behind TTS")
	}
	if ControlRPC(string(MethodTtsSynthesize)) || ControlRPC(string(MethodVoiceAppend)) {
		t.Fatal("tts/append stay on the general lane")
	}
}

func TestSlotGateControlSucceedsWhenGeneralIsFull(t *testing.T) {
	t.Parallel()
	gate := NewSlotGate(1, 1, 20*time.Millisecond)
	if !gate.Acquire(context.Background(), string(MethodVoiceAppend)) {
		t.Fatal("first general acquire")
	}
	if gate.Acquire(context.Background(), string(MethodTtsSynthesize)) {
		t.Fatal("second general acquire should fail while full")
	}
	if !gate.Acquire(context.Background(), string(MethodStreamCancel)) {
		t.Fatal("control acquire must not wait behind a full general pool")
	}
	gate.Release(string(MethodStreamCancel))
	gate.Release(string(MethodVoiceAppend))
}

func TestSlotGateWaitsThenAcquires(t *testing.T) {
	t.Parallel()
	gate := NewSlotGate(1, 1, 200*time.Millisecond)
	if !gate.Acquire(context.Background(), string(MethodTtsSynthesize)) {
		t.Fatal("first acquire")
	}
	released := make(chan struct{})
	go func() {
		time.Sleep(40 * time.Millisecond)
		gate.Release(string(MethodTtsSynthesize))
		close(released)
	}()
	if !gate.Acquire(context.Background(), string(MethodVoiceAppend)) {
		t.Fatal("waited acquire should succeed once the slot is released")
	}
	<-released
	gate.Release(string(MethodVoiceAppend))
}
