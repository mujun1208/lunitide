package producthub

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lunitide/lunitide/internal/producthub/generated"
)

func TestOpenReadsPagesFromProductFiles(t *testing.T) {
	root := filepath.Join(t.TempDir(), "app")
	writeProductFixture(t, root)
	if !UseProductRoot(root) {
		t.Fatal("product files were not read")
	}
	t.Cleanup(resetProductSurface)

	ctx := context.Background()
	svc := New(&MemoryPersist{})
	overview, err := svc.Overview(ctx)
	if err != nil {
		t.Fatal(err)
	}
	graph, err := svc.Graph(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var page, setting, media bool
	for _, node := range graph.Nodes {
		switch node.StableKey {
		case "feature.dialog.page.notice":
			page = true
		case "feature.foundation.settings.canvas":
			setting = true
		case "feature.office.media.caption":
			media = true
		case "feature.office.page.mro":
			t.Fatal("a page removed from the product files is still on the graph")
		}
	}
	if !page || !setting || !media {
		t.Fatalf("live query missed page=%v setting=%v media=%v cards=%d nodes=%d", page, setting, media, overview.CardCount, len(graph.Nodes))
	}
}

func TestRepoProductFilesMatchTheCompiledCatalog(t *testing.T) {
	root := FindProductRoot()
	if root == "" {
		t.Fatal("this checkout should contain web/src/app/appTypes.ts")
	}
	surf, ok := readProductSurface(root)
	if !ok {
		t.Fatal("the product files did not parse")
	}
	if len(surf.pages) != len(generated.Pages) || len(surf.settings) != len(generated.Settings) || len(surf.media) != len(generated.MediaActions) {
		t.Fatalf("files pages=%d settings=%d media=%d compiled pages=%d settings=%d media=%d", len(surf.pages), len(surf.settings), len(surf.media), len(generated.Pages), len(generated.Settings), len(generated.MediaActions))
	}
}

func writeProductFixture(t *testing.T, root string) {
	t.Helper()
	mustWrite(t, filepath.Join(root, "web", "src", "app", "appTypes.ts"), "export type Page='home'|'notice'\n")
	mustWrite(t, filepath.Join(root, "web", "src", "productHub", "hubCatalog.ts"), ""+
		"    id: 'home', name: '主页', nameEn: 'Home', domain: 'dialog', module: 'home',\n"+
		"    id: 'notice', name: '公告', nameEn: 'Notice', domain: 'dialog', module: 'notice',\n")
	mustWrite(t, filepath.Join(root, "web", "src", "settings", "settingsNav.ts"), ""+
		"export type SettingsCategory =\n"+
		"  | 'general'\n"+
		"  | 'canvas'\n"+
		"export const SETTINGS_CATEGORIES = [\n"+
		"  { id: 'general', icon: 'a', label: '常规', labelEn: 'General' },\n"+
		"  { id: 'canvas', icon: 'b', label: '画布', labelEn: 'Canvas' },\n"+
		"]\n")
	mustWrite(t, filepath.Join(root, "web", "src", "generated", "bridge.ts"),
		"export type MediaOperationDTO = { \"action\": \"play\" | \"caption\"; }\n")
}

func mustWrite(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0600); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(body, "export") && !strings.Contains(body, "id:") {
		t.Fatal(path)
	}
}
