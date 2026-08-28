package toolruntime

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"unicode/utf8"
)

type knownLaunchApp struct {
	Canonical string
	Aliases   []string
	Processes []string
	ExeHints  []string
}

var knownLaunchApps = []knownLaunchApp{
	{
		Canonical: "网易云音乐",
		Aliases:   []string{"网易云音乐", "网易云", "网易云音", "cloudmusic", "netease", "netease cloud music", "163音乐"},
		Processes: []string{"cloudmusic.exe", "cloudmusic"},
		ExeHints: []string{
			`%LOCALAPPDATA%\Netease\CloudMusic\cloudmusic.exe`,
			`%LOCALAPPDATA%\NetEase\CloudMusic\cloudmusic.exe`,
			`%ProgramFiles%\Netease\CloudMusic\cloudmusic.exe`,
			`%ProgramFiles(x86)%\Netease\CloudMusic\cloudmusic.exe`,
		},
	},
	{
		Canonical: "汽水音乐",
		Aliases:   []string{"汽水音乐", "汽水"},
		Processes: []string{"sodamusic.exe", "汽水音乐.exe"},
		ExeHints: []string{
			`%LOCALAPPDATA%\SodaMusic\Soda Music.exe`,
			`%LOCALAPPDATA%\SodaMusic\SodaMusic.exe`,
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

var launchOpenPrefix = regexp.MustCompile(`^(?:你)?(?:请)?(?:可以)?(?:帮我)?(?:打开|启动|运行)`)

func foldLaunchQuery(raw string) string {
	q := strings.ToLower(strings.TrimSpace(raw))
	q = strings.TrimSuffix(q, ".lnk")
	q = strings.TrimSuffix(q, ".exe")
	q = strings.TrimSpace(q)
	return q
}

func launchQueryCore(query string) string {
	q := strings.TrimSpace(query)
	q = launchOpenPrefix.ReplaceAllString(q, "")
	return strings.TrimSpace(q)
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
	var out []string
	for _, hint := range app.ExeHints {
		p := os.ExpandEnv(hint)
		if p == "" || strings.Contains(p, "%") {
			continue
		}
		p = filepath.Clean(p)
		st, err := os.Stat(p)
		if err != nil || st.IsDir() {
			continue
		}
		out = append(out, p)
	}
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
		if path, _, _ := pickStartMenuShortcut(app.Canonical); path != "" {
			return app.Canonical
		}
	}
	return ""
}
