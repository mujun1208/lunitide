//go:build windows

package toolruntime

import (
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/sys/windows/registry"
)

func expandWindowsPath(raw string) string {
	s := strings.TrimSpace(raw)
	if s == "" {
		return ""
	}
	if strings.HasPrefix(s, "%") {
		return filepath.Clean(os.ExpandEnv(s))
	}
	return filepath.Clean(s)
}

func registryDesktopDir() string {
	k, err := registry.OpenKey(registry.CURRENT_USER, `Software\Microsoft\Windows\CurrentVersion\Explorer\User Shell Folders`, registry.READ)
	if err != nil {
		return ""
	}
	defer k.Close()
	v, _, err := k.GetStringValue("Desktop")
	if err != nil {
		return ""
	}
	return expandWindowsPath(v)
}

func desktopDirCandidates() []string {
	var candidates []string
	if p := registryDesktopDir(); p != "" {
		candidates = append(candidates, p)
	}
	if p := os.Getenv("USERPROFILE"); p != "" {
		candidates = append(candidates,
			filepath.Join(p, "Desktop"),
			filepath.Join(p, "桌面"),
			filepath.Join(p, "OneDrive", "Desktop"),
			filepath.Join(p, "OneDrive", "桌面"),
		)
	}
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		candidates = append(candidates,
			filepath.Join(home, "Desktop"),
			filepath.Join(home, "桌面"),
			filepath.Join(home, "OneDrive", "Desktop"),
			filepath.Join(home, "OneDrive", "桌面"),
		)
	}
	seen := map[string]bool{}
	out := make([]string, 0, len(candidates))
	for _, c := range candidates {
		c = filepath.Clean(c)
		if c == "" || seen[c] {
			continue
		}
		seen[c] = true
		if st, err := os.Stat(c); err == nil && st.IsDir() {
			out = append(out, c)
		}
	}
	return out
}

func startMenuRoots() []string {
	var roots []string
	if p := os.Getenv("APPDATA"); p != "" {
		roots = append(roots, filepath.Join(p, "Microsoft", "Windows", "Start Menu", "Programs"))
	}
	if p := os.Getenv("ProgramData"); p != "" {
		roots = append(roots, filepath.Join(p, "Microsoft", "Windows", "Start Menu", "Programs"))
	}
	return roots
}

func lookupUninstallExecutables(app knownLaunchApp) []string {
	needles := make([]string, 0, 1+len(app.Aliases)+len(app.Processes))
	needles = append(needles, app.Canonical)
	needles = append(needles, app.Aliases...)
	needles = append(needles, app.Processes...)
	roots := []registry.Key{registry.CURRENT_USER, registry.LOCAL_MACHINE}
	paths := []string{
		`Software\Microsoft\Windows\CurrentVersion\Uninstall`,
		`Software\WOW6432Node\Microsoft\Windows\CurrentVersion\Uninstall`,
	}
	var out []string
	seen := map[string]bool{}
	for _, hive := range roots {
		for _, path := range paths {
			k, err := registry.OpenKey(hive, path, registry.ENUMERATE_SUB_KEYS|registry.READ)
			if err != nil {
				continue
			}
			names, _ := k.ReadSubKeyNames(0)
			for _, name := range names {
				sub, err := registry.OpenKey(k, name, registry.READ)
				if err != nil {
					continue
				}
				display, _, _ := sub.GetStringValue("DisplayName")
				icon, _, _ := sub.GetStringValue("DisplayIcon")
				install, _, _ := sub.GetStringValue("InstallLocation")
				sub.Close()
				if !uninstallNameMatches(display, needles) {
					continue
				}
				for _, candidate := range uninstallExeCandidates(icon, install, app.Processes) {
					key := strings.ToLower(candidate)
					if seen[key] {
						continue
					}
					seen[key] = true
					out = append(out, candidate)
				}
			}
			k.Close()
		}
	}
	return out
}

func uninstallNameMatches(display string, needles []string) bool {
	folded := strings.ToLower(strings.TrimSpace(display))
	if folded == "" {
		return false
	}
	for _, needle := range needles {
		n := strings.ToLower(strings.TrimSpace(needle))
		n = strings.TrimSuffix(n, ".exe")
		if n == "" {
			continue
		}
		if strings.Contains(folded, n) || strings.Contains(n, folded) {
			return true
		}
	}
	return false
}

func uninstallExeCandidates(icon, install string, processes []string) []string {
	var out []string
	icon = strings.Trim(strings.TrimSpace(icon), `"`)
	if i := strings.Index(icon, ","); i >= 0 {
		icon = strings.TrimSpace(icon[:i])
	}
	icon = expandWindowsPath(icon)
	if strings.EqualFold(filepath.Ext(icon), ".exe") {
		out = append(out, icon)
	}
	install = expandWindowsPath(install)
	if install != "" {
		for _, proc := range processes {
			base := filepath.Base(proc)
			if !strings.EqualFold(filepath.Ext(base), ".exe") {
				base += ".exe"
			}
			out = append(out, filepath.Join(install, base))
			out = append(out, filepath.Join(install, "CloudMusic", base))
		}
	}
	return out
}

func pickStartMenuShortcut(query string) (string, []string, error) {
	var hits []desktopHit
	for _, root := range startMenuRoots() {
		_ = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
			if err != nil {
				return nil
			}
			if d.IsDir() {
				return nil
			}
			ext := strings.ToLower(filepath.Ext(d.Name()))
			if ext != ".lnk" && ext != ".exe" && ext != ".url" {
				return nil
			}
			score := desktopNameScore(d.Name(), query)
			if score <= 0 {
				return nil
			}
			hits = append(hits, desktopHit{path: path, base: d.Name(), score: score})
			return nil
		})
	}
	return pickBestDesktopHit(hits, query)
}
