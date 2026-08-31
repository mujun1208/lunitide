package ipc

import "testing"

func TestGatewayPipeNameIsStableAndSafe(t *testing.T) {
	a := GatewayPipeName("Mu Jun")
	b := GatewayPipeName("mu jun")
	if a != b {
		t.Fatalf("pipe name not stable: %q vs %q", a, b)
	}
	if a != `\\.\pipe\lunitide-gateway-mu-jun` {
		t.Fatalf("pipe = %q", a)
	}
	if got := GatewayPipeName(`evil\..\pipe`); got != `\\.\pipe\lunitide-gateway-evil-pipe` {
		t.Fatalf("unsafe name leaked: %q", got)
	}
}
