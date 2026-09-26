package producthub

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"

	"github.com/lunitide/lunitide/internal/producthub/generated"
)

// productSurface is the page, setting, and media-action list read from the
// product files at query time. An empty surface means this process is using
// the catalog compiled into the engine.
type productSurface struct {
	pages    []generated.Page
	settings []generated.Setting
	media    []string
}

var (
	surfaceMu     sync.Mutex
	activeSurface productSurface
)

// UseProductRoot points later 功能全景 / 知识图谱 / 解剖视图 queries at the
// product files under root. Those three views share one catalog, so a page,
// setting, or media action added or removed in the files shows up on the
// next open without regenerating the compiled catalog.
func UseProductRoot(root string) bool {
	surf, ok := readProductSurface(root)
	if !ok {
		return false
	}
	surfaceMu.Lock()
	activeSurface = surf
	surfaceMu.Unlock()
	return true
}

func resetProductSurface() {
	surfaceMu.Lock()
	activeSurface = productSurface{}
	surfaceMu.Unlock()
}

// FindProductRoot walks from the working directory and the executable toward
// the filesystem root, looking for the product's page union. An installed
// copy that does not ship those files keeps the compiled catalog.
func FindProductRoot() string {
	var starts []string
	if wd, err := os.Getwd(); err == nil {
		starts = append(starts, wd)
	}
	if exe, err := os.Executable(); err == nil {
		starts = append(starts, filepath.Dir(exe))
	}
	for _, start := range starts {
		dir := start
		for i := 0; i < 8; i++ {
			if fileExists(filepath.Join(dir, "web", "src", "app", "appTypes.ts")) {
				return dir
			}
			parent := filepath.Dir(dir)
			if parent == dir {
				break
			}
			dir = parent
		}
	}
	return ""
}

func catalogPages() []generated.Page {
	surfaceMu.Lock()
	pages := activeSurface.pages
	surfaceMu.Unlock()
	if len(pages) > 0 {
		return pages
	}
	return generated.Pages
}

func catalogSettings() []generated.Setting {
	surfaceMu.Lock()
	items := activeSurface.settings
	surfaceMu.Unlock()
	if len(items) > 0 {
		return items
	}
	return generated.Settings
}

func catalogMedia() []string {
	surfaceMu.Lock()
	actions := activeSurface.media
	surfaceMu.Unlock()
	if len(actions) > 0 {
		return actions
	}
	return generated.MediaActions
}

func readProductSurface(root string) (productSurface, bool) {
	pageIDs := pageIDsFrom(readText(filepath.Join(root, "web", "src", "app", "appTypes.ts")))
	meta := pageMetaFrom(readText(filepath.Join(root, "web", "src", "productHub", "hubCatalog.ts")))
	settings := settingsFrom(readText(filepath.Join(root, "web", "src", "settings", "settingsNav.ts")))
	media := mediaFrom(readText(filepath.Join(root, "web", "src", "generated", "bridge.ts")))
	if len(pageIDs) < 2 || len(settings) < 2 || len(media) < 2 {
		return productSurface{}, false
	}
	pages := make([]generated.Page, 0, len(pageIDs))
	for _, id := range pageIDs {
		item, ok := meta[id]
		if !ok || item.Name == "" || item.Domain == "" || item.Module == "" {
			return productSurface{}, false
		}
		pages = append(pages, item)
	}
	return productSurface{pages: pages, settings: settings, media: media}, true
}

func pageIDsFrom(src string) []string {
	hit := regexp.MustCompile(`export type Page=((?:'[^']+'\|?)+)`).FindStringSubmatch(src)
	if hit == nil {
		return nil
	}
	return quoted(hit[1])
}

func pageMetaFrom(src string) map[string]generated.Page {
	out := map[string]generated.Page{}
	for _, hit := range regexp.MustCompile(`(?m)^\s*id: '([^']+)', name: '([^']+)', nameEn: '([^']+)', domain: '([^']+)', module: '([^']+)',\s*$`).FindAllStringSubmatch(src, -1) {
		out[hit[1]] = generated.Page{ID: hit[1], Name: hit[2], NameEN: hit[3], Domain: hit[4], Module: hit[5]}
	}
	return out
}

func settingsFrom(src string) []generated.Setting {
	var out []generated.Setting
	for _, hit := range regexp.MustCompile(`\{\s*id:\s*'([^']+)',\s*icon:\s*'[^']*',\s*label:\s*'([^']+)',\s*labelEn:\s*'([^']+)'`).FindAllStringSubmatch(src, -1) {
		out = append(out, generated.Setting{ID: hit[1], Name: hit[2], NameEN: hit[3]})
	}
	return out
}

func mediaFrom(src string) []string {
	hit := regexp.MustCompile(`export type MediaOperationDTO = \{.*?"action":\s*([^;]+);`).FindStringSubmatch(src)
	if hit == nil {
		return nil
	}
	var out []string
	for _, name := range regexp.MustCompile(`"([a-z_]+)"`).FindAllStringSubmatch(hit[1], -1) {
		out = append(out, name[1])
	}
	return out
}

func quoted(src string) []string {
	var out []string
	for _, hit := range regexp.MustCompile(`'([^']+)'`).FindAllStringSubmatch(src, -1) {
		out = append(out, hit[1])
	}
	return out
}

func readText(path string) string {
	raw, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return strings.ReplaceAll(string(raw), "\r\n", "\n")
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}
