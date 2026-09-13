package project_test

import (
	"strings"
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
	p := project.Project{ID: ulid.Make().String(), Name: name, ProjectCode: "ITM00001", Type: project.TypeImplementation, Status: project.StatusChartered, CreatedAt: now, UpdatedAt: now, Version: 1}
	if err := p.Validate(); err != nil {
		t.Fatal(err)
	}
	if _, err := project.NormalizeName(""); err == nil {
		t.Fatal("empty name accepted")
	}
}

func TestValidateCreateBusinessFieldsRequiresRootPath(t *testing.T) {
	p := project.Project{
		Type: project.TypeImplementation, Description: "desc", Client: "客户A",
		PlanStart: "2026-01-01", PlanEnd: "2026-12-31",
	}
	if err := project.ValidateCreateBusinessFields(p); err == nil || err.Error() != "project root path is required" {
		t.Fatalf("missing root: %v", err)
	}
	p.RootPath = "C:\\work\\mall"
	if err := project.ValidateCreateBusinessFields(p); err != nil {
		t.Fatal(err)
	}
}

func TestValidateAllowsEmptyRootAndRejectsBadExecutor(t *testing.T) {
	now := time.Now().UTC()
	p := project.Project{
		ID: ulid.Make().String(), Name: "Alpha", ProjectCode: "ITM00001",
		Type: project.TypeImplementation, Status: project.StatusCreated,
		CreatedAt: now, UpdatedAt: now, Version: 1,
	}
	if err := p.Validate(); err != nil {
		t.Fatalf("historical empty root must validate: %v", err)
	}
	p.DefaultExecutor = "kimi"
	if err := p.Validate(); err == nil {
		t.Fatal("invalid executor accepted")
	}
	p.DefaultExecutor = project.ExecutorLunitide
	p.TreeStatus = "bogus"
	if err := p.Validate(); err == nil {
		t.Fatal("invalid tree status accepted")
	}
	p.TreeStatus = project.TreeNone
	p.RootPath = strings.Repeat("x", 1025)
	if err := p.Validate(); err == nil {
		t.Fatal("oversized root accepted")
	}
}
