package app

import "testing"

func TestContextAssemblyPath(t *testing.T) {
	if got := contextAssemblyPath(true, false, false, false); got != "durable" {
		t.Fatalf("durable assembly = %q", got)
	}
	if got := contextAssemblyPath(true, false, true, false); got != "explicit" {
		t.Fatalf("explicit reader = %q", got)
	}
	if got := contextAssemblyPath(true, false, false, true); got != "checkpoint" {
		t.Fatalf("checkpoint = %q", got)
	}
	if got := contextAssemblyPath(false, true, false, false); got != "fallback" {
		t.Fatalf("fallback = %q", got)
	}
	if got := contextAssemblyPath(false, true, false, true); got != "fallback" {
		t.Fatalf("fallback beats checkpoint: %q", got)
	}
}
