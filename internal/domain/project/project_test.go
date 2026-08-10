package project_test

import (
	"testing"
	"time"

	"github.com/lunitide/lunitide/internal/domain/project"
	"github.com/oklog/ulid/v2"
)

func TestNormalizeAndValidate(t *testing.T) {
	name, err := project.NormalizeName("  Alpha\n  Beta  ")
	if err != nil || name != "Alpha Beta" {
		t.Fatalf("NormalizeName = %q, %v", name, err)
	}
	now := time.Now().UTC()
	p := project.Project{ID: ulid.Make().String(), Name: name, Status: project.StatusActive, CreatedAt: now, UpdatedAt: now, Version: 1}
	if err := p.Validate(); err != nil {
		t.Fatal(err)
	}
	if _, err := project.NormalizeName(""); err == nil {
		t.Fatal("empty name accepted")
	}
}
