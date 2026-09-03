package m8app_test

import (
	"testing"

	"github.com/lunitide/lunitide/internal/m8app"
)

func TestOntologyPacksLoadSoftwareAndMRO(t *testing.T) {
	sw, err := m8app.LoadOntologyPack("software.v1")
	if err != nil || sw.ID != "software.v1" {
		t.Fatalf("software: %+v %v", sw, err)
	}
	if !sw.HasKind("Class") {
		t.Fatal("software.v1 must declare Class")
	}
	mro, err := m8app.LoadOntologyPack("mro.v1")
	if err != nil {
		t.Fatal(err)
	}
	if !mro.HasKind("Fault") || !mro.HasLink("APPLIES_TO") || !mro.HasAction("propose_remediation") {
		t.Fatalf("mro pack incomplete: %+v", mro)
	}
}

func TestOntologyPackValidatePayload(t *testing.T) {
	mro, err := m8app.LoadOntologyPack("mro.v1")
	if err != nil {
		t.Fatal(err)
	}
	if err := mro.ValidateNodePayload(`{"pack":"mro.v1","kind":"Fault","code":"32-00","title":"EGT"}`); err != nil {
		t.Fatal(err)
	}
	if err := mro.ValidateNodePayload(`{"pack":"mro.v1","kind":"Spaceship"}`); err == nil {
		t.Fatal("unknown kind must fail")
	}
}

func TestOntologyPackUnknownID(t *testing.T) {
	if _, err := m8app.LoadOntologyPack("legal.v1"); err == nil {
		t.Fatal("missing pack must fail")
	}
}
