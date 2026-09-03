package mroapp

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

type goldenItem struct {
	Q               string `json:"q"`
	ExpectDocType   string `json:"expectDocType"`
	ExpectContains  string `json:"expectContains"`
	ExpectEmpty     bool   `json:"expectEmpty"`
	ExpectNotAdopted string `json:"expectNotAdopted"`
	ForbidBarePN    bool   `json:"forbidBarePN"`
}

func loadGoldenP0(t *testing.T) []goldenItem {
	t.Helper()
	path := filepath.Join("..", "..", "testdata", "mro", "golden_p0.json")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var items []goldenItem
	if err := json.Unmarshal(raw, &items); err != nil {
		t.Fatal(err)
	}
	return items
}

func TestGoldenFixtureHasFiveRealItems(t *testing.T) {
	items := loadGoldenP0(t)
	if len(items) != 5 {
		t.Fatalf("golden items = %d, want 5 real fixtures (do not pad with synthetic stories)", len(items))
	}
	empty := 0
	for _, item := range items {
		if item.Q == "" {
			t.Fatal("golden item missing q")
		}
		if item.ExpectEmpty {
			empty++
		}
	}
	if empty < 2 {
		t.Fatalf("want at least two expectEmpty items, got %d", empty)
	}
}

// controlledDocFamilies are the manual families a grounded MRO answer may cite.
var controlledDocFamilies = map[string]bool{
	"AMM": true, "MEL": true, "FIM": true, "TSM": true, "IPC": true,
	"CMM": true, "SB": true, "AD": true, "SRM": true, "WDM": true,
}

// TestGoldenNonEmptyItemsDeclareACoherentExpectation replaces the old skipped
// placeholder: every non-empty golden item must declare at least one concrete
// expectation, and any docType must be a known controlled family. The live
// corpus-seeded grounding for these same items is asserted in
// internal/m8app (TestGoldenNonEmptyCorpusGroundsWithMroLocators).
func TestGoldenNonEmptyItemsDeclareACoherentExpectation(t *testing.T) {
	nonEmpty := 0
	for _, item := range loadGoldenP0(t) {
		if item.ExpectEmpty {
			continue
		}
		nonEmpty++
		t.Run(item.Q, func(t *testing.T) {
			if item.ExpectDocType == "" && item.ExpectContains == "" && item.ExpectNotAdopted == "" {
				t.Fatalf("non-empty golden item %q must declare docType, contains, or notAdopted", item.Q)
			}
			if item.ExpectDocType != "" && !controlledDocFamilies[item.ExpectDocType] {
				t.Fatalf("docType %q is not a known controlled family", item.ExpectDocType)
			}
		})
	}
	if nonEmpty < 3 {
		t.Fatalf("expected at least three non-empty golden items, got %d", nonEmpty)
	}
}
