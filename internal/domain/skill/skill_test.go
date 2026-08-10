package skill

import (
	"testing"
	"time"
)

func TestSkillValidate(t *testing.T) {
	valid := Skill{
		ID:          "01ARZ3NDEKTSV4RRFFQ69G5FAV",
		Name:        "test-skill",
		DisplayName: "Test Skill",
		Description: "A test skill",
		Version:     "1.0.0",
		Status:      SkillStatusDraft,
		Permissions: []PermissionLevel{PermissionReadOnly},
		EntryPoint:  "test/entry",
		ManifestJSON: "{}",
		CreatedAt:   time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		UpdatedAt:   time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
	}
	if err := valid.Validate(); err != nil {
		t.Fatalf("valid skill rejected: %v", err)
	}

	invalid := valid
	invalid.ID = "bad"
	if err := invalid.Validate(); err == nil {
		t.Fatal("invalid ID accepted")
	}
	invalid = valid
	invalid.Status = "unknown"
	if err := invalid.Validate(); err == nil {
		t.Fatal("unknown status accepted")
	}
	invalid = valid
	invalid.Permissions = []PermissionLevel{}
	if err := invalid.Validate(); err == nil {
		t.Fatal("empty permissions accepted")
	}
	invalid = valid
	invalid.Permissions = []PermissionLevel{"unknown"}
	if err := invalid.Validate(); err == nil {
		t.Fatal("unknown permission accepted")
	}
	invalid = valid
	invalid.EntryPoint = ""
	if err := invalid.Validate(); err == nil {
		t.Fatal("empty entry_point accepted")
	}
	invalid = valid
	invalid.ManifestJSON = "{"
	if err := invalid.Validate(); err == nil {
		t.Fatal("too short manifest_json accepted")
	}
}

func TestSkillHasPermission(t *testing.T) {
	s := Skill{Permissions: []PermissionLevel{PermissionReadOnly, PermissionNetwork}}
	if !s.HasPermission(PermissionReadOnly) {
		t.Fatal("should have read_only permission")
	}
	if !s.HasPermission(PermissionNetwork) {
		t.Fatal("should have network permission")
	}
	if s.HasPermission(PermissionShell) {
		t.Fatal("should not have shell permission")
	}
}

func TestSkillMaxRiskLevel(t *testing.T) {
	s := Skill{Permissions: []PermissionLevel{PermissionReadOnly}}
	if s.MaxRiskLevel() != "low" {
		t.Fatalf("expected low risk, got %s", s.MaxRiskLevel())
	}
	s.Permissions = []PermissionLevel{PermissionReadOnly, PermissionNetwork}
	if s.MaxRiskLevel() != "medium" {
		t.Fatalf("expected medium risk, got %s", s.MaxRiskLevel())
	}
	s.Permissions = []PermissionLevel{PermissionFileSystem, PermissionReadOnly}
	if s.MaxRiskLevel() != "high" {
		t.Fatalf("expected high risk, got %s", s.MaxRiskLevel())
	}
	s.Permissions = []PermissionLevel{PermissionReadOnly, PermissionShell}
	if s.MaxRiskLevel() != "critical" {
		t.Fatalf("expected critical risk, got %s", s.MaxRiskLevel())
	}
}

func TestPermissionRiskLevel(t *testing.T) {
	if PermissionReadOnly.RiskLevel() != "low" {
		t.Fatal("read_only should be low risk")
	}
	if PermissionReadWrite.RiskLevel() != "medium" {
		t.Fatal("read_write should be medium risk")
	}
	if PermissionNetwork.RiskLevel() != "medium" {
		t.Fatal("network should be medium risk")
	}
	if PermissionFileSystem.RiskLevel() != "high" {
		t.Fatal("file_system should be high risk")
	}
	if PermissionShell.RiskLevel() != "critical" {
		t.Fatal("shell should be critical risk")
	}
	if PermissionAdmin.RiskLevel() != "critical" {
		t.Fatal("admin should be critical risk")
	}
}