package toolruntime

import (
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"
)

// App vocabulary powers semantic routing: any installed or known desktop app
// name the user mentions is a strong signal that the turn is a desktop
// control task (R2), regardless of whether the static hint table knows it.

const appVocabularyTTL = 5 * time.Minute

var (
	appVocabMu      sync.Mutex
	appVocabCache   []string
	appVocabExpires time.Time
	appVocabScanner = listStartMenuShortcutNames
)

// KnownLaunchAppNames returns every canonical name and alias from the
// built-in launch table. Stable, cheap, platform independent.
func KnownLaunchAppNames() []string {
	seen := map[string]bool{}
	var out []string
	add := func(name string) {
		name = strings.TrimSpace(name)
		if name == "" {
			return
		}
		key := strings.ToLower(name)
		if seen[key] {
			return
		}
		seen[key] = true
		out = append(out, name)
	}
	for _, app := range knownLaunchApps {
		add(app.Canonical)
		for _, alias := range app.Aliases {
			add(alias)
		}
	}
	// Native Settings pages / shell folders open through desktop.open too,
	// so "打开蓝牙设置" and the shorter spoken forms ("打开蓝牙", "换壁纸")
	// must route as desktop tasks, not how-to answers.
	for _, t := range nativeLaunchTargets {
		add(t.Label)
		for _, alias := range t.Aliases {
			if keepNativeAlias(alias) {
				add(alias)
			}
		}
	}
	return out
}

// keepNativeAlias drops one-character noise and two-letter Latin tokens
// ("ok", "go") that would false-trigger routing. Two CJK characters
// ("蓝牙", "壁纸", "锁屏") are the names people actually say.
func keepNativeAlias(alias string) bool {
	n := utf8.RuneCountInString(strings.TrimSpace(alias))
	if n < 2 {
		return false
	}
	if n >= 3 {
		return true
	}
	for _, r := range alias {
		if r < 0x80 {
			return false
		}
	}
	return true
}

// InstalledAppNames returns Start Menu shortcut names on this PC, cached for
// appVocabularyTTL. Never blocks a chat turn for longer than one scan; an
// empty result simply means "no dynamic vocabulary" and routing falls back
// to the static table.
func InstalledAppNames() []string {
	appVocabMu.Lock()
	defer appVocabMu.Unlock()
	now := time.Now()
	if appVocabCache != nil && now.Before(appVocabExpires) {
		return append([]string(nil), appVocabCache...)
	}
	names := appVocabScanner()
	if names == nil {
		names = []string{}
	}
	appVocabCache = names
	appVocabExpires = now.Add(appVocabularyTTL)
	return append([]string(nil), names...)
}

// ResetInstalledAppNamesForTest clears the cache and optionally swaps the scanner.
func ResetInstalledAppNamesForTest(scanner func() []string) {
	appVocabMu.Lock()
	defer appVocabMu.Unlock()
	appVocabCache = nil
	appVocabExpires = time.Time{}
	if scanner != nil {
		appVocabScanner = scanner
	} else {
		appVocabScanner = listStartMenuShortcutNames
	}
}

// usefulAppVocabularyName drops shortcut names that would create false
// positives in routing: uninstallers, generic words, and anything too short
// to be a meaningful mention.
func usefulAppVocabularyName(name string) bool {
	n := strings.ToLower(strings.TrimSpace(name))
	if n == "" {
		return false
	}
	if utf8.RuneCountInString(n) < 2 {
		return false
	}
	// Two-letter Latin names ("ie", "go") are too ambiguous; two CJK
	// characters ("微信", "飞书") are real app names.
	if utf8.RuneCountInString(n) == 2 {
		for _, r := range n {
			if r < 0x80 {
				return false
			}
		}
	}
	for _, bad := range []string{
		"uninstall", "卸载", "readme", "help", "帮助", "documentation", "文档",
		"license", "release notes", "更新日志", "website", "官网", "主页",
		"command prompt", "run", "windows", "system", "系统", "administrative tools",
		"accessories", "startup", "启动", "maintenance", "更新", "update",
		"tools", "工具", "设置", "settings", "control panel", "控制面板",
	} {
		if strings.Contains(n, bad) {
			return false
		}
	}
	// Names that are entirely punctuation/digits are not apps.
	for _, r := range n {
		if unicode.IsLetter(r) || unicode.Is(unicode.Han, r) {
			return true
		}
	}
	return false
}
