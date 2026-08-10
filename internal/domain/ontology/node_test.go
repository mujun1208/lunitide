package ontology

import (
	"testing"
	"time"
)

func TestNodeValidate(t *testing.T) {
	valid := Node{
		ID:           "01ARZ3NDEKTSV4RRFFQ69G5FAV",
		ProjectID:    "01ARZ3NDEKTSV4RRFFQ69G5FAV",
		Type:         NodeTypeClass,
		Name:         "TestClass",
		FullPath:     "pkg.TestClass",
		Description:  "A test class",
		MetadataJSON: "{}",
		Version:      1,
		CreatedAt:    time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		UpdatedAt:    time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
	}
	if err := valid.Validate(); err != nil {
		t.Fatalf("valid node rejected: %v", err)
	}

	invalid := valid
	invalid.ID = "bad"
	if err := invalid.Validate(); err == nil {
		t.Fatal("invalid ID accepted")
	}
	invalid = valid
	invalid.Type = "unknown"
	if err := invalid.Validate(); err == nil {
		t.Fatal("unknown type accepted")
	}
	invalid = valid
	invalid.Name = ""
	if err := invalid.Validate(); err == nil {
		t.Fatal("empty name accepted")
	}
	invalid = valid
	invalid.Version = 0
	if err := invalid.Validate(); err == nil {
		t.Fatal("zero version accepted")
	}
}

func TestEdgeValidate(t *testing.T) {
	valid := Edge{
		ID:             "01ARZ3NDEKTSV4RRFFQ69G5FAV",
		SourceNodeID:   "01ARZ3NDEKTSV4RRFFQ69G5FAV",
		TargetNodeID:   "01ARZ3NDEKTSV4RRFFQ69G5FAB",
		Type:           EdgeTypeDependsOn,
		PropertiesJSON: "{}",
		Weight:         0.5,
		Version:        1,
		CreatedAt:      time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		UpdatedAt:      time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
	}
	if err := valid.Validate(); err != nil {
		t.Fatalf("valid edge rejected: %v", err)
	}

	invalid := valid
	invalid.ID = "bad"
	if err := invalid.Validate(); err == nil {
		t.Fatal("invalid ID accepted")
	}
	invalid = valid
	invalid.SourceNodeID = valid.TargetNodeID
	if err := invalid.Validate(); err == nil {
		t.Fatal("self-referencing edge accepted")
	}
	invalid = valid
	invalid.Type = "unknown"
	if err := invalid.Validate(); err == nil {
		t.Fatal("unknown edge type accepted")
	}
	invalid = valid
	invalid.Weight = -0.1
	if err := invalid.Validate(); err == nil {
		t.Fatal("negative weight accepted")
	}
	invalid = valid
	invalid.Weight = 1.1
	if err := invalid.Validate(); err == nil {
		t.Fatal("weight > 1.0 accepted")
	}
}