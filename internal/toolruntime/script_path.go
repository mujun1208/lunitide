package toolruntime

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

func windowsAppsAlias(path string) bool {
	p := strings.ToLower(filepath.Clean(path))
	return strings.Contains(p, `\windowsapps\`) || strings.Contains(p, `/windowsapps/`)
}

func usableExe(path string) bool {
	if path == "" || windowsAppsAlias(path) {
		return false
	}
	st, err := os.Stat(path)
	return err == nil && !st.IsDir() && st.Size() > 0
}

func looksLikeScriptPath(arg string) bool {
	if arg == "" || strings.HasPrefix(arg, "-") {
		return false
	}
	switch strings.ToLower(filepath.Ext(arg)) {
	case ".py", ".js", ".mjs", ".cjs":
		return true
	default:
		return false
	}
}

func scriptExists(root, arg string) bool {
	candidates := []string{arg}
	if !filepath.IsAbs(arg) {
		candidates = append(candidates, filepath.Join(root, filepath.Clean(arg)))
	}
	for _, c := range candidates {
		st, err := os.Stat(c)
		if err == nil && !st.IsDir() {
			return true
		}
	}
	return false
}

// relocateMissingScripts rewrites a script argument onto the copy that
// workspace.write actually created. Models often run an absolute path that
// was never written, while the file sits in the session workspace.
func relocateMissingScripts(root string, argv []string) ([]string, error) {
	if len(argv) == 0 {
		return argv, nil
	}
	out := append([]string(nil), argv...)
	for i := 1; i < len(out); i++ {
		if !looksLikeScriptPath(out[i]) {
			continue
		}
		if scriptExists(root, out[i]) {
			if !filepath.IsAbs(out[i]) {
				out[i] = filepath.Join(root, filepath.Clean(out[i]))
			}
			continue
		}
		found := findScriptInWorkspace(root, out[i])
		if found == "" {
			return nil, fmt.Errorf("脚本不在工作区：%s。先用 workspace.write 写到工作区相对路径，再 command.run。不要让用户在对话外执行命令，也不要改成 PPT", out[i])
		}
		out[i] = found
	}
	return out, nil
}

func findScriptInWorkspace(root, requested string) string {
	base := strings.ToLower(filepath.Base(requested))
	want := strings.ToLower(filepath.ToSlash(requested))
	var unique, suffixHit, bestRel string
	ambiguous := false
	n := 0
	_ = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			if d.Name() == "node_modules" || d.Name() == ".git" {
				return filepath.SkipDir
			}
			return nil
		}
		n++
		if n > 4000 {
			return filepath.SkipAll
		}
		rel, relErr := filepath.Rel(root, path)
		if relErr != nil {
			return nil
		}
		rel = strings.ToLower(filepath.ToSlash(rel))
		if rel != "" && rel != "." && (strings.HasSuffix(want, "/"+rel) || strings.HasSuffix(want, rel)) && len(rel) > len(bestRel) {
			suffixHit = path
			bestRel = rel
		}
		if strings.ToLower(d.Name()) == base {
			if unique == "" && !ambiguous {
				unique = path
			} else {
				unique = ""
				ambiguous = true
			}
		}
		return nil
	})
	if suffixHit != "" {
		return suffixHit
	}
	return unique
}

// resolveInterpreter replaces python/py with a real interpreter. The Windows
// Store alias at WindowsApps\python.exe makes CreateProcess return
// "The system cannot find the file specified."
func resolveInterpreter(argv []string) ([]string, error) {
	if len(argv) == 0 {
		return argv, nil
	}
	switch commandHead(argv[0]) {
	case "python", "python3", "py":
	default:
		return argv, nil
	}
	exe, err := findRealPython()
	if err != nil {
		return nil, err
	}
	rest := append([]string(nil), argv[1:]...)
	if commandHead(argv[0]) == "py" && commandHead(exe) != "py" && len(rest) > 0 {
		flag := rest[0]
		if flag == "-2" || flag == "-3" || strings.HasPrefix(flag, "-3.") {
			rest = rest[1:]
		}
	}
	return append([]string{exe}, rest...), nil
}

func findRealPython() (string, error) {
	for _, name := range []string{"py", "python", "python3"} {
		p, err := exec.LookPath(name)
		if err != nil {
			continue
		}
		if abs, absErr := filepath.Abs(p); absErr == nil {
			p = abs
		}
		if usableExe(p) {
			return p, nil
		}
	}
	if runtime.GOOS == "windows" {
		if found := searchPythonInstalls(); found != "" {
			return found, nil
		}
	}
	return "", fmt.Errorf("找不到可用的 Python。WindowsApps 里的 python.exe 是商店占位，不能运行。不要让用户把命令粘贴到外面执行")
}

func searchPythonInstalls() string {
	var roots []string
	if local := os.Getenv("LOCALAPPDATA"); local != "" {
		roots = append(roots, filepath.Join(local, "Programs", "Python"))
	}
	roots = append(roots, `C:\Python`, `C:\Program Files\Python`)
	var best string
	for _, root := range roots {
		entries, err := os.ReadDir(root)
		if err != nil {
			continue
		}
		for _, e := range entries {
			cand := filepath.Join(root, e.Name(), "python.exe")
			if usableExe(cand) && (best == "" || cand > best) {
				best = cand
			}
		}
	}
	return best
}

func isLocalServerCommand(argv []string) bool {
	if len(argv) < 2 {
		return false
	}
	switch commandHead(argv[0]) {
	case "python", "python3", "py", "node":
	default:
		return false
	}
	blob := strings.ToLower(strings.Join(argv[1:], " "))
	if strings.Contains(blob, "http.server") || strings.Contains(blob, "uvicorn") || strings.Contains(blob, "flask") {
		return true
	}
	for _, a := range argv[1:] {
		if strings.HasPrefix(a, "-") {
			continue
		}
		base := strings.ToLower(filepath.Base(a))
		if base == "server.py" || base == "app.py" || strings.HasSuffix(base, "_server.py") {
			return true
		}
	}
	return false
}
