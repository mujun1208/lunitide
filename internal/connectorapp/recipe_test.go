package connectorapp

import (
	"path/filepath"
	"testing"
)

func TestMissingCredentialAndPendingExternalNeverLookReady(t *testing.T) {
	s := NewFileStore(filepath.Join(t.TempDir(), "recipes.json"))
	ifind, err := s.Put(Recipe{ID: "ifind", Scope: "quotes", RateLimit: "1/s"})
	if err != nil || ifind.Status != "missing_credential" {
		t.Fatalf("no account must be missing_credential: %+v %v", ifind, err)
	}
	sec, err := s.Put(Recipe{ID: "sec-edgar", Scope: "legal", PendingExternal: true})
	if err != nil || sec.Status != "pending_external" {
		t.Fatalf("external-only must stay pending_external: %+v %v", sec, err)
	}
	if ImpersonateViaWebSearch("ifind") || ImpersonateViaWebSearch("tianyancha") || ImpersonateViaWebSearch("sec") {
		t.Fatal("must not allow web search to impersonate commercial feeds")
	}
	if !ForbiddenLookup("ifind") || !ForbiddenLookup("tianyancha") || !ForbiddenLookup("sec") {
		t.Fatal("commercial feeds stay fail-closed")
	}
	ready, err := s.Put(Recipe{ID: "im-text", Scope: "im", CredentialRef: "cred-1"})
	if err != nil || ready.Status != "ready" {
		t.Fatalf("existing IM with credential can be ready: %+v %v", ready, err)
	}
	paused, err := s.RevokeCredential("im-text")
	if err != nil || paused.Status != "missing_credential" || !paused.Paused {
		t.Fatalf("revoke must pause: %+v %v", paused, err)
	}
	if s.BackgroundAllowed("im-text") {
		t.Fatal("revoked credential must pause background work")
	}
	if AttachmentReceipt(sec) != "pending_external" {
		t.Fatalf("external connector attachment must stay pending_external: %s", AttachmentReceipt(sec))
	}
}

func TestListOrCatalogKeepsCommercialClosedAfterReadyRecipe(t *testing.T) {
	s := NewFileStore(filepath.Join(t.TempDir(), "recipes.json"))
	if _, err := s.Put(Recipe{ID: "im-text", Scope: "im", CredentialRef: "cred-1"}); err != nil {
		t.Fatal(err)
	}
	fake, err := s.Put(Recipe{ID: "ifind", Scope: "quotes", CredentialRef: "pasted-key"})
	if err != nil || fake.Status == "ready" {
		t.Fatalf("catalog commercial feed must not become ready: %+v %v", fake, err)
	}
	if s.BackgroundAllowed("ifind") {
		t.Fatal("ifind must not start background work")
	}
	seen := map[string]string{}
	for _, item := range s.ListOrCatalog() {
		seen[item.ID] = item.Status
	}
	if seen["im-text"] != "ready" {
		t.Fatalf("existing IM recipe vanished or lost ready: %+v", seen)
	}
	if seen["ifind"] != "missing_credential" || seen["tianyancha"] != "missing_credential" || seen["sec"] != "pending_external" {
		t.Fatalf("catalog must stay visible and closed: %+v", seen)
	}
}
