package sqlite

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

type r3AllocationFile struct {
	Items []struct {
		LogicalName string `json:"logicalName"`
		Number      int    `json:"number"`
		FileName    string `json:"fileName"`
		Existing    bool   `json:"existing"`
		Previous    string `json:"previous"`
		MaxObserved int    `json:"maxObserved"`
	} `json:"items"`
	MaxObserved int `json:"maxObserved"`
}

func loadR3Allocation(t *testing.T) r3AllocationFile {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("caller")
	}
	root := filepath.Clean(filepath.Join(filepath.Dir(thisFile), "..", "..", ".."))
	raw, err := os.ReadFile(filepath.Join(root, "docs", "design", "jiyishengji", "evidence", "migration-allocation.json"))
	if err != nil {
		t.Fatal(err)
	}
	var alloc r3AllocationFile
	if err := json.Unmarshal(raw, &alloc); err != nil {
		t.Fatal(err)
	}
	return alloc
}

func TestMigrationAllocation(t *testing.T) {
	alloc := loadR3Allocation(t)
	if alloc.MaxObserved < 163 {
		t.Fatalf("maxObserved %d", alloc.MaxObserved)
	}
	got := map[string]struct {
		number   int
		fileName string
		existing bool
	}{}
	for _, item := range alloc.Items {
		got[item.LogicalName] = struct {
			number   int
			fileName string
			existing bool
		}{item.Number, item.FileName, item.Existing}
	}
	want := []struct {
		logical string
		number  int
		file    string
	}{
		{"memory_fabric", 161, "0161_memory_fabric.sql"},
		{"memory_retrieval", 162, "0162_memory_retrieval.sql"},
		{"memory_generations", 163, "0163_memory_generations.sql"},
		{"ocr_model_packs", 164, "0164_ocr_model_packs.sql"},
		{"media_sessions", 165, "0165_media_sessions.sql"},
	}
	for _, row := range want {
		hit, ok := got[row.logical]
		if !ok {
			t.Fatalf("missing logical %s", row.logical)
		}
		if hit.number != row.number || hit.fileName != row.file {
			t.Fatalf("%s got %+v want %d %s", row.logical, hit, row.number, row.file)
		}
	}
	if got["media_sessions"].number <= got["ocr_model_packs"].number {
		t.Fatal("media_sessions must follow ocr_model_packs")
	}
	if got["ocr_model_packs"].fileName == "0164_media_sessions.sql" || got["media_sessions"].fileName == "0164_ocr_model_packs.sql" {
		t.Fatal("logical names must not swap physical files")
	}
}
