package toolruntime

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/lunitide/lunitide/internal/winexec"
)

type knownLaunchApp struct {
	Canonical string
	Aliases   []string
	Processes []string
	ExeHints  []string
	WalkRoots []string
}

var knownLaunchApps = []knownLaunchApp{
	{
		Canonical: "网易云音乐",
		Aliases:   []string{"网易云音乐", "网易云", "网易云音", "cloudmusic", "CloudMusic", "netease", "netease cloud music", "NetEase Cloud Music", "163音乐"},
		Processes: []string{"cloudmusic.exe", "cloudmusic"},
		ExeHints: []string{
			`%LOCALAPPDATA%\Netease\CloudMusic\cloudmusic.exe`,
			`%LOCALAPPDATA%\NetEase\CloudMusic\cloudmusic.exe`,
			`%APPDATA%\Netease\CloudMusic\cloudmusic.exe`,
			`%LOCALAPPDATA%\CloudMusic\cloudmusic.exe`,
			`%ProgramFiles%\Netease\CloudMusic\cloudmusic.exe`,
			`%ProgramFiles(x86)%\Netease\CloudMusic\cloudmusic.exe`,
			`%ProgramFiles%\NetEase\CloudMusic\cloudmusic.exe`,
			`%ProgramFiles(x86)%\NetEase\CloudMusic\cloudmusic.exe`,
		},
		WalkRoots: []string{
			`%LOCALAPPDATA%\Netease`,
			`%LOCALAPPDATA%\NetEase`,
			`%APPDATA%\Netease`,
			`%ProgramFiles%\Netease`,
			`%ProgramFiles(x86)%\Netease`,
			`%ProgramFiles%\NetEase`,
			`%ProgramFiles(x86)%\NetEase`,
		},
	},
	{
		Canonical: "汽水音乐",
		Aliases:   []string{"汽水音乐", "汽水", "sodamusic", "soda music", "Soda Music"},
		Processes: []string{"sodamusic.exe", "Soda Music.exe", "SodaMusic.exe", "汽水音乐.exe"},
		ExeHints: []string{
			`%LOCALAPPDATA%\SodaMusic\Soda Music.exe`,
			`%LOCALAPPDATA%\SodaMusic\SodaMusic.exe`,
			`%LOCALAPPDATA%\Soda Music\Soda Music.exe`,
			`%LOCALAPPDATA%\Programs\Soda Music\Soda Music.exe`,
			`%LOCALAPPDATA%\Programs\SodaMusic\Soda Music.exe`,
		},
		WalkRoots: []string{
			`%LOCALAPPDATA%\SodaMusic`,
			`%LOCALAPPDATA%\Soda Music`,
			`%LOCALAPPDATA%\Programs\Soda Music`,
			`%LOCALAPPDATA%\Programs\SodaMusic`,
		},
	},
	{
		Canonical: "QQ音乐",
		Aliases:   []string{"qq音乐", "qqmusic", "qq music"},
		Processes: []string{"qqmusic.exe"},
		ExeHints: []string{
			`%ProgramFiles%\Tencent\QQMusic\QQMusic.exe`,
			`%ProgramFiles(x86)%\Tencent\QQMusic\QQMusic.exe`,
			`%LOCALAPPDATA%\Tencent\QQMusic\QQMusic.exe`,
		},
	},
}

var launchOpenPrefix = regexp.MustCompile(`^(?:你)?(?:请)?(?:可以)?(?:帮我)?(?:给我)?(?:把开了?|打开了?|打开|启动|运行)`)
var desktopLocPrefix = regexp.MustCompile(`^(?:一下)?(?:的)?(?:桌面上的|桌面的|桌面上|桌面里的|桌面里)`)
var desktopDocSuffix = regexp.MustCompile(`(?:文档|文件)$`)

func foldLaunchQuery(raw string) string {
	q := strings.ToLower(strings.TrimSpace(raw))
	q = strings.TrimSuffix(q, ".lnk")
	q = strings.TrimSuffix(q, ".exe")
	q = strings.TrimSpace(q)
	return q
}

func normalizeLaunchQuery(query string) string {
	q := strings.TrimSpace(query)
	q = strings.ReplaceAll(q, "把开了", "打开")
	q = strings.ReplaceAll(q, "把开", "打开")
	q = strings.ReplaceAll(q, "把它桌面", "打开桌面")
	q = strings.ReplaceAll(q, "打开了我", "打开")
	q = strings.ReplaceAll(q, "打开我打开", "打开")
	q = strings.ReplaceAll(q, "写意文档", "协议文档")
	q = strings.ReplaceAll(q, "协意文档", "协议文档")
	return q
}

func launchQueryCore(query string) string {
	q := normalizeLaunchQuery(query)
	q = launchOpenPrefix.ReplaceAllString(q, "")
	q = desktopLocPrefix.ReplaceAllString(q, "")
	return strings.TrimSpace(q)
}

func desktopQueryCandidates(query string) []string {
	core := launchQueryCore(query)
	if core == "" {
		core = strings.TrimSpace(normalizeLaunchQuery(query))
	}
	seen := map[string]bool{}
	var out []string
	add := func(s string) {
		s = strings.TrimSpace(s)
		if s == "" || seen[s] {
			return
		}
		switch s {
		case "桌面", "的", "上的", "一下", "文件", "文档":
			return
		}
		if utf8.RuneCountInString(s) < 2 {
			return
		}
		seen[s] = true
		out = append(out, s)
	}
	add(core)
	add(desktopDocSuffix.ReplaceAllString(core, ""))
	if i := strings.LastIndex(core, "的"); i >= 0 && i+len("的") < len(core) {
		add(core[i+len("的"):])
	}
	return out
}

func matchKnownLaunchApp(query string) (knownLaunchApp, bool) {
	core := launchQueryCore(query)
	folded := foldLaunchQuery(core)
	if folded == "" {
		return knownLaunchApp{}, false
	}
	for _, app := range knownLaunchApps {
		for _, alias := range app.Aliases {
			if foldLaunchQuery(alias) == folded {
				return app, true
			}
		}
		canon := foldLaunchQuery(app.Canonical)
		if utf8.RuneCountInString(core) >= 2 && (strings.HasPrefix(canon, folded) || strings.HasPrefix(folded, canon)) {
			return app, true
		}
	}
	return knownLaunchApp{}, false
}

func musicWindowHints(app string) []string {
	hints := []string{strings.TrimSpace(app)}
	if known, ok := matchKnownLaunchApp(app); ok {
		hints = append(hints, known.Canonical)
		hints = append(hints, known.Aliases...)
		hints = append(hints, known.Processes...)
	}
	seen := map[string]bool{}
	out := make([]string, 0, len(hints))
	for _, h := range hints {
		h = strings.TrimSpace(h)
		if h == "" {
			continue
		}
		key := strings.ToLower(h)
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, h)
	}
	return out
}

var lookupKnownAppExecutables = defaultLookupKnownAppExecutables

func defaultLookupKnownAppExecutables(app knownLaunchApp) []string {
	seen := map[string]bool{}
	var out []string
	add := func(p string) {
		p = strings.TrimSpace(p)
		if p == "" || strings.Contains(p, "%") {
			return
		}
		p = filepath.Clean(p)
		key := strings.ToLower(p)
		if seen[key] {
			return
		}
		st, err := os.Stat(p)
		if err != nil || st.IsDir() {
			return
		}
		seen[key] = true
		out = append(out, p)
	}
	for _, hint := range app.ExeHints {
		add(os.ExpandEnv(hint))
	}
	for _, root := range app.WalkRoots {
		for _, found := range walkForProcess(os.ExpandEnv(root), app.Processes, 4) {
			add(found)
		}
	}
	for _, found := range winexec.LookupProcessImages(app.Processes) {
		add(found)
	}
	for _, found := range lookupUninstallExecutables(app) {
		add(found)
	}
	return out
}

func walkForProcess(root string, processNames []string, maxDepth int) []string {
	root = strings.TrimSpace(root)
	if root == "" || strings.Contains(root, "%") {
		return nil
	}
	st, err := os.Stat(root)
	if err != nil || !st.IsDir() {
		return nil
	}
	want := map[string]bool{}
	for _, name := range processNames {
		base := strings.ToLower(filepath.Base(name))
		if base == "" {
			continue
		}
		want[base] = true
		if !strings.HasSuffix(base, ".exe") {
			want[base+".exe"] = true
		}
	}
	if len(want) == 0 {
		return nil
	}
	var out []string
	root = filepath.Clean(root)
	_ = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		rel, relErr := filepath.Rel(root, path)
		if relErr != nil {
			return nil
		}
		depth := 0
		if rel != "." {
			depth = strings.Count(rel, string(os.PathSeparator)) + 1
		}
		if d.IsDir() {
			if maxDepth > 0 && depth >= maxDepth {
				return filepath.SkipDir
			}
			return nil
		}
		if want[strings.ToLower(d.Name())] {
			out = append(out, path)
		}
		return nil
	})
	return out
}

func CanonicalMusicApp(query string) string {
	if app, ok := matchKnownLaunchApp(query); ok {
		return app.Canonical
	}
	return ""
}

func CanonicalMusicAppFromText(text string) string {
	t := strings.ToLower(strings.TrimSpace(text))
	if t == "" {
		return ""
	}
	for _, app := range knownLaunchApps {
		for _, alias := range app.Aliases {
			if strings.Contains(t, strings.ToLower(alias)) {
				return app.Canonical
			}
		}
	}
	return ""
}

func pickKnownAppExecutable(query string) (string, bool) {
	app, ok := matchKnownLaunchApp(query)
	if !ok {
		return "", false
	}
	found := lookupKnownAppExecutables(app)
	if len(found) == 0 {
		return "", false
	}
	return found[0], true
}

// FirstInstalledMusicApp returns the first known desktop player that is
// actually installed on this PC (exe path or Start Menu shortcut).
// Order is 网易云音乐, 汽水音乐, QQ音乐 — never a website.
func FirstInstalledMusicApp() string {
	for _, app := range knownLaunchApps {
		if len(lookupKnownAppExecutables(app)) > 0 {
			return app.Canonical
		}
		for _, alias := range append([]string{app.Canonical}, app.Aliases...) {
			if path, _, _ := pickStartMenuShortcut(alias); path != "" {
				return app.Canonical
			}
		}
	}
	return ""
}
