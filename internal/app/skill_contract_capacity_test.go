package app

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/lunitide/lunitide/internal/skillapp"
)

// The bridge schemas bound how many rows a response may carry, and those bounds
// were written when the catalog was small: skill.catalog.list allowed 128 while
// the catalog had grown to 130, and skill.list allowed 100 for a library that can
// hold every catalog entry plus the user's own skills. A response over its bound
// does not arrive trimmed, it fails validation — the market then shows a library
// of zero and every card offers "+" for a skill that is already installed.
//
// Pin the contracts against the real catalog so adding a skill cannot silently
// break the market again.
func TestBridgeContractsHoldTheWholeCatalog(t *testing.T) {
	entries := len(skillapp.Catalog())
	if entries == 0 {
		t.Fatal("catalog is empty")
	}

	catalogMax := schemaResultMaxItems(t, "skill.catalog.list.schema.json")
	if catalogMax < entries {
		t.Fatalf("skill.catalog.list allows %d items but the catalog ships %d: the market response is invalid", catalogMax, entries)
	}

	// The library holds catalog skills plus anything the user creates or imports,
	// so its bound has to have real headroom over the catalog, not just clear it.
	libraryMax := schemaResultMaxItems(t, "skill.list.schema.json")
	if libraryMax < entries*2 {
		t.Fatalf("skill.list allows %d items for a catalog of %d: no room for user-created skills", libraryMax, entries)
	}
}

func schemaResultMaxItems(t *testing.T, name string) int {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "..", "api", "bridge", "v1", name))
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Result struct {
			Properties struct {
				Items struct {
					MaxItems *int `json:"maxItems"`
				} `json:"items"`
			} `json:"properties"`
		} `json:"x-result"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	if doc.Result.Properties.Items.MaxItems == nil {
		t.Fatalf("%s: x-result.items has no maxItems", name)
	}
	return *doc.Result.Properties.Items.MaxItems
}
