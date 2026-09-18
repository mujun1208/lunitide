package ocrapp

import "testing"

func TestOCRScopeValid(t *testing.T) {
	user := OCRScope{OwnerSubjectID: "user-1", ScopeKind: "user", ScopeID: "user-1"}
	if !user.Valid() {
		t.Fatal("user scope")
	}
	if (OCRScope{OwnerSubjectID: "user-1", ScopeKind: "user", ScopeID: "other"}).Valid() {
		t.Fatal("user must not carry foreign scopeId")
	}
	if (OCRScope{OwnerSubjectID: "user-1", ScopeKind: "project"}).Valid() {
		t.Fatal("project requires scopeId")
	}
	if !(OCRScope{OwnerSubjectID: "user-1", ScopeKind: "project", ScopeID: "proj-1"}).Valid() {
		t.Fatal("project scope")
	}
}
