package app

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/lunitide/lunitide/internal/bridge"
)

const testInstallID = "01ARZ3NDEKTSV4RRFFQ69G5FAV"

// TestPluginConfirmVaultSingleUse proves the W6 guarantee: a freshly issued
// token verifies exactly once for its bound installId and is then burned, so a
// replay of the same token is rejected.
func TestPluginConfirmVaultSingleUse(t *testing.T) {
	var v pluginConfirmVault
	now := time.Unix(1_700_000_000, 0).UTC()
	token, expires, err := v.issue(testInstallID, now)
	if err != nil {
		t.Fatalf("issue: %v", err)
	}
	if len(token) != 64 {
		t.Fatalf("token length = %d, want 64", len(token))
	}
	if !expires.After(now) {
		t.Fatalf("expiry %v not after now %v", expires, now)
	}
	if !v.consume(token, testInstallID, now.Add(time.Second)) {
		t.Fatal("first consume rejected a valid token")
	}
	if v.consume(token, testInstallID, now.Add(time.Second)) {
		t.Fatal("replay of a consumed token was accepted")
	}
}

// TestPluginConfirmVaultRejectsForgedAndMismatched covers the forged token, the
// wrong-installId binding and the expired-token paths — the three ways the old
// client-derived digest could be abused.
func TestPluginConfirmVaultRejectsForgedAndMismatched(t *testing.T) {
	var v pluginConfirmVault
	now := time.Unix(1_700_000_000, 0).UTC()

	// Forged token that was never issued.
	if v.consume("f0f0f0f0f0f0f0f0f0f0f0f0f0f0f0f0f0f0f0f0f0f0f0f0f0f0f0f0f0f0f0f0", testInstallID, now) {
		t.Fatal("forged token was accepted")
	}

	// Real token but presented for a different installId.
	token, _, err := v.issue(testInstallID, now)
	if err != nil {
		t.Fatalf("issue: %v", err)
	}
	if v.consume(token, "01ARZ3NDEKTSV4RRFFQ69G5FZZ", now) {
		t.Fatal("token accepted for the wrong installId")
	}
	// Binding-mismatch still burns the token (defence against probing).
	if v.consume(token, testInstallID, now) {
		t.Fatal("mismatched token was not burned on first presentation")
	}

	// Expired token is rejected.
	token2, _, err := v.issue(testInstallID, now)
	if err != nil {
		t.Fatalf("issue: %v", err)
	}
	if v.consume(token2, testInstallID, now.Add(pluginConfirmTTL+time.Second)) {
		t.Fatal("expired token was accepted")
	}
}

// TestPluginConfirmVaultIssueIsUnique guards against a degenerate RNG or reuse:
// two consecutive issues must produce distinct tokens.
func TestPluginConfirmVaultIssueIsUnique(t *testing.T) {
	var v pluginConfirmVault
	now := time.Unix(1_700_000_000, 0).UTC()
	a, _, err := v.issue(testInstallID, now)
	if err != nil {
		t.Fatalf("issue a: %v", err)
	}
	b, _, err := v.issue(testInstallID, now)
	if err != nil {
		t.Fatalf("issue b: %v", err)
	}
	if a == b {
		t.Fatal("two issues produced the same token")
	}
}

// TestHandlePluginConfirmTokenValidation checks the bridge-handler guards: a
// malformed installId is a schema error and a missing plugin service degrades
// to a retryable storage error (no token minted).
func TestHandlePluginConfirmTokenValidation(t *testing.T) {
	e := &Engine{}
	badPayload, _ := json.Marshal(map[string]any{"installId": "nope"})
	r := bridge.Request{ID: "1", Payload: badPayload}
	resp := handlePluginConfirmToken(e, context.Background(), r)
	if resp.OK || resp.Error == nil || resp.Error.Code != "BRIDGE_SCHEMA_INVALID" {
		t.Fatalf("bad installId: got ok=%v err=%+v", resp.OK, resp.Error)
	}

	okPayload, _ := json.Marshal(map[string]any{"installId": testInstallID})
	r2 := bridge.Request{ID: "2", Payload: okPayload}
	resp2 := handlePluginConfirmToken(e, context.Background(), r2)
	if resp2.OK || resp2.Error == nil || resp2.Error.Code != "STORAGE_UNAVAILABLE" {
		t.Fatalf("nil service: got ok=%v err=%+v", resp2.OK, resp2.Error)
	}
}
