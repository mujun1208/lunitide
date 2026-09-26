package producthub

import (
	"context"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
)

func TestLoginUsesStoredCatalogUntilTheProductVersionChanges(t *testing.T) {
	ctx := context.Background()
	root := filepath.Join(t.TempDir(), "app")
	writeProductFixture(t, root)
	if !UseProductRoot(root) {
		t.Fatal("product files were not read")
	}
	t.Cleanup(resetProductSurface)
	svc := New(&MemoryPersist{})
	svc.SetProductVersion("9.1.0")

	first, err := svc.Graph(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !graphHas(first, "feature.dialog.page.notice") || !graphHas(first, "feature.dialog.page.home") || !graphHas(first, "feature.foundation.settings.canvas") || !graphHas(first, "feature.office.media.caption") || !graphHas(first, "feature.dialog.companion.voice-talk") || !graphHas(first, "feature.assets.plugin.llm") {
		t.Fatal("version query did not keep pages, settings, media actions, and verbs")
	}
	resetProductSurface()
	cached, err := svc.Graph(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !graphHas(cached, "feature.dialog.page.notice") || len(cached.Nodes) != len(first.Nodes) {
		t.Fatal("same version read the live files again instead of storage")
	}

	next := filepath.Join(t.TempDir(), "next")
	mustWrite(t, filepath.Join(next, "web", "src", "app", "appTypes.ts"), "export type Page='home'|'board'\n")
	mustWrite(t, filepath.Join(next, "web", "src", "productHub", "hubCatalog.ts"), ""+
		"    id: 'home', name: '主页', nameEn: 'Home', domain: 'dialog', module: 'home',\n"+
		"    id: 'board', name: '看板', nameEn: 'Board', domain: 'dialog', module: 'board',\n")
	mustWrite(t, filepath.Join(next, "web", "src", "settings", "settingsNav.ts"), ""+
		"export type SettingsCategory =\n  | 'general'\n  | 'canvas'\n"+
		"export const SETTINGS_CATEGORIES = [\n"+
		"  { id: 'general', icon: 'a', label: '常规', labelEn: 'General' },\n"+
		"  { id: 'canvas', icon: 'b', label: '画布', labelEn: 'Canvas' },\n]\n")
	mustWrite(t, filepath.Join(next, "web", "src", "generated", "bridge.ts"), "export type MediaOperationDTO = { \"action\": \"play\" | \"caption\"; }\n")
	if !UseProductRoot(next) {
		t.Fatal("next product files were not read")
	}
	svc.SetProductVersion("9.2.0")
	upgraded, err := svc.Graph(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if graphHas(upgraded, "feature.dialog.page.notice") || !graphHas(upgraded, "feature.dialog.page.board") || !graphHas(upgraded, "feature.dialog.companion.voice-talk") || !graphHas(upgraded, "feature.assets.plugin.llm") {
		t.Fatal("a new version did not reassemble the current product")
	}
	overview, err := svc.Overview(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if overview.CardCount < 4 {
		t.Fatalf("stored overview is missing cards: %d", overview.CardCount)
	}
}

type countingPersist struct {
	*MemoryPersist
	saves atomic.Int32
}

func (c *countingPersist) ProductHubSaveEdition(ctx context.Context, ed Edition) error {
	c.saves.Add(1)
	return c.MemoryPersist.ProductHubSaveEdition(ctx, ed)
}

func TestLoginReadsShareOneAssembly(t *testing.T) {
	ctx := context.Background()
	store := &countingPersist{MemoryPersist: &MemoryPersist{}}
	svc := New(store)
	svc.SetProductVersion("9.1.0")
	var wg sync.WaitGroup
	ids := make([]string, 4)
	errs := make([]error, 4)
	for i := range ids {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			ov, err := svc.Overview(ctx)
			ids[i] = ov.EditionID
			errs[i] = err
		}(i)
	}
	wg.Wait()
	for i, err := range errs {
		if err != nil || ids[i] == "" || ids[i] != ids[0] {
			t.Fatalf("login read %d id=%q err=%v saves=%d", i, ids[i], err, store.saves.Load())
		}
	}
	if store.saves.Load() != 1 {
		t.Fatalf("version change wrote %d assemblies", store.saves.Load())
	}
}

func graphHas(g Graph, key string) bool {
	for _, node := range g.Nodes {
		if node.StableKey == key {
			return true
		}
	}
	return false
}
