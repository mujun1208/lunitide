package app

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/lunitide/lunitide/internal/skillapp"
	storage "github.com/lunitide/lunitide/internal/storage/sqlite"
)

type marketItem struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Version   string `json:"version"`
	Installed bool   `json:"installed"`
}

// The market card decides between "+" (install) and "卸载" (uninstall) by
// matching the catalog entry against the library the renderer holds. When a
// skill IS installed and its card still shows "+", clicking it is a no-op the
// user reads as a dead button. That match has to hold for every catalog entry
// after every entry has been installed — no exceptions, or some card in the
// grid is permanently stuck on "+".
func TestEveryInstalledSkillResolvesToItsMarketCard(t *testing.T) {
	ctx := context.Background()
	store, err := storage.OpenTemplated(ctx, filepath.Join(t.TempDir(), "market.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	svc := skillapp.New(store, store)
	e := NewEngine(nil, "market")
	e.skills = svc
	e.SetPersistDir(t.TempDir())

	for _, tpl := range skillapp.Catalog() {
		if _, err := svc.ReplaceFromCatalog(ctx, tpl.ID); err != nil {
			t.Fatalf("install %s: %v", tpl.ID, err)
		}
	}

	library, err := svc.List(ctx, "")
	if err != nil {
		t.Fatal(err)
	}
	installedByKey := map[string]string{}
	for _, s := range library {
		installedByKey[skillapp.SkillNameKey(s.Name)] = s.Version
	}

	resp := handleSkillCatalogList(e, ctx, packageRequest(t, "skill.catalog.list", map[string]any{}))
	if !resp.OK {
		t.Fatalf("skill.catalog.list failed: %+v", resp.Error)
	}
	raw, err := json.Marshal(resp.Payload)
	if err != nil {
		t.Fatal(err)
	}
	var out struct{ Items []marketItem }
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatal(err)
	}
	if len(out.Items) == 0 {
		t.Fatal("catalog returned no entries")
	}

	var stuck []string
	for _, entry := range out.Items {
		key := skillapp.SkillNameKey(entry.Name)
		have, ok := installedByKey[key]
		if !ok {
			// The renderer matches on name key; a catalog name that no installed
			// row can ever match is a card frozen on "+".
			stuck = append(stuck, entry.Name+"@"+entry.Version+" (no library row)")
			continue
		}
		if have != entry.Version {
			stuck = append(stuck, entry.Name+" catalog v"+entry.Version+" vs installed v"+have)
		}
		if !entry.Installed {
			stuck = append(stuck, entry.Name+" installed flag false")
		}
	}
	if len(stuck) > 0 {
		limit := len(stuck)
		if limit > 20 {
			limit = 20
		}
		t.Fatalf("%d catalog cards cannot reach installed state; first %d: %v", len(stuck), limit, stuck[:limit])
	}
}
