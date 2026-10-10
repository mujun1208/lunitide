package toolruntime

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

// capability-self-bootstrap P3: mcp.install is a mutating tool delegated to
// the host installer. The runtime must gate it in approval mode, run it once
// approved, keep it direct in the modes the operator already granted, and
// fail closed with no installer wired.

func TestMcpInstallGatedInApprovalMode(t *testing.T) {
	r := newProductRuntime(t)
	defer r.Close()
	var ran bool
	r.SetMcpInstaller(func(ctx context.Context, session string, raw json.RawMessage) (string, error) {
		ran = true
		return `{"endpointId":"ep","state":"connected"}`, nil
	})
	session := "01ARZ3NDEKTSV4RRFFQ69G5FAV"
	_, err := r.Execute(context.Background(), Approval, session, "mcp.install", []byte(`{"presetId":"fetch"}`), false)
	if !errors.Is(err, ErrApprovalRequired) {
		t.Fatalf("approval mode must gate mcp.install: %v", err)
	}
	if ran {
		t.Fatal("installer must not run before the approval")
	}
	out, err := r.Execute(context.Background(), Approval, session, "mcp.install", []byte(`{"presetId":"fetch"}`), true)
	if err != nil {
		t.Fatal(err)
	}
	if !ran || !strings.Contains(out.Output, `"state":"connected"`) {
		t.Fatalf("approved install must run the installer: %q", out.Output)
	}
}

func TestMcpInstallDirectInGrantedModes(t *testing.T) {
	for _, mode := range []Mode{AutoEdit, FullAccess} {
		r := newProductRuntime(t)
		defer r.Close()
		var ran bool
		r.SetMcpInstaller(func(ctx context.Context, session string, raw json.RawMessage) (string, error) {
			ran = true
			return `{"endpointId":"ep","state":"connected"}`, nil
		})
		if _, err := r.Execute(context.Background(), mode, "01ARZ3NDEKTSV4RRFFQ69G5FAV", "mcp.install", []byte(`{"presetId":"fetch"}`), false); err != nil {
			t.Fatalf("%s must not gate mcp.install: %v", mode, err)
		}
		if !ran {
			t.Fatalf("%s must run the installer without a prompt", mode)
		}
	}
}

func TestMcpInstallFailsClosedWithoutInstaller(t *testing.T) {
	r := newProductRuntime(t)
	_, err := r.Execute(context.Background(), FullAccess, "01ARZ3NDEKTSV4RRFFQ69G5FAV", "mcp.install", []byte(`{"presetId":"fetch"}`), true)
	if err == nil || !strings.Contains(err.Error(), "MCP") {
		t.Fatalf("nil installer must fail closed: %v", err)
	}
}

func TestMcpInstallPrepareDecideRoundTrip(t *testing.T) {
	r := newProductRuntime(t)
	defer r.Close()
	var ran int
	r.SetMcpInstaller(func(ctx context.Context, session string, raw json.RawMessage) (string, error) {
		ran++
		return `{"endpointId":"ep","state":"connected","presetId":"fetch"}`, nil
	})
	ctx := context.Background()
	session := "01ARZ3NDEKTSV4RRFFQ69G5FAV"
	args := []byte(`{"presetId":"fetch"}`)
	p, err := r.Prepare(ctx, "run-1", session, "call-1", "mcp.install", args, Approval, 0)
	if err != nil {
		t.Fatal(err)
	}
	if ran != 0 {
		t.Fatal("Prepare's dry run must not execute the installer")
	}
	out, err := r.Decide(ctx, session, "call-1", p.ArgsDigest, true)
	if err != nil {
		t.Fatal(err)
	}
	if ran != 1 || !strings.Contains(out.Output, `"presetId":"fetch"`) {
		t.Fatalf("decide must execute the install exactly once: ran=%d out=%q", ran, out.Output)
	}
	if _, err := r.Decide(ctx, session, "call-1", p.ArgsDigest, true); !errors.Is(err, ErrPendingConsumed) {
		t.Fatalf("second decide must be consumed: %v", err)
	}
}
