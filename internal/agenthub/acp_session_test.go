package agenthub

import (
	"encoding/json"
	"testing"
)

func TestACPOpenNativeSessionKeepsIDWhenLoadOmitsSessionId(t *testing.T) {
	id, err := acpOpenNativeSession(func(method string, _ any) (*acpRPC, error) {
		if method != "session/load" {
			t.Fatalf("method = %s, want session/load", method)
		}
		return &acpRPC{Result: json.RawMessage(`{}`)}, nil
	}, `C:\proj`, "sess_old")
	if err != nil || id != "sess_old" {
		t.Fatalf("id = %q err=%v, want sess_old", id, err)
	}
}

func TestKeepNativeIDPrefersReturned(t *testing.T) {
	if got := keepNativeID("sess_new", "sess_old"); got != "sess_new" {
		t.Fatalf("got %q", got)
	}
	if got := keepNativeID("", "sess_old"); got != "sess_old" {
		t.Fatalf("empty returned should keep stored: %q", got)
	}
}

func TestACPOpenNativeSessionErrorsWhenNewOmitsSessionId(t *testing.T) {
	_, err := acpOpenNativeSession(func(method string, _ any) (*acpRPC, error) {
		if method != "session/new" {
			t.Fatalf("method = %s, want session/new", method)
		}
		return &acpRPC{Result: json.RawMessage(`{}`)}, nil
	}, `C:\proj`, "")
	if err == nil {
		t.Fatal("empty session/new must fail")
	}
}
