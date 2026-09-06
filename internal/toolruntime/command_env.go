package toolruntime

import (
	"sort"
	"strings"
)

// commandEnvAllowlist is the curated set of environment variable names that a
// command.run / run_terminal_cmd child process is allowed to inherit from the
// engine. S-02: the engine process may hold provider credentials, tokens and
// other secrets in its own environment; passing os.Environ() through verbatim
// leaks all of them to every spawned command. Instead we pass ONLY the system
// and toolchain variables a build/test/git command legitimately needs, plus
// the explicit overrides. Matching is case-insensitive (Windows env keys are
// case-insensitive). Anything not on this list — API keys, tokens, secrets —
// never reaches the child.
var commandEnvAllowlist = map[string]bool{
	// Windows system essentials (path resolution, temp, user profile).
	"PATH":                  true,
	"PATHEXT":               true,
	"SYSTEMROOT":            true,
	"WINDIR":                true,
	"SYSTEMDRIVE":           true,
	"COMSPEC":               true,
	"TEMP":                  true,
	"TMP":                   true,
	"USERPROFILE":           true,
	"HOMEDRIVE":             true,
	"HOMEPATH":              true,
	"HOME":                  true,
	"USERNAME":              true,
	"LOGNAME":               true,
	"COMPUTERNAME":          true,
	"NUMBER_OF_PROCESSORS":  true,
	"PROCESSOR_ARCHITECTURE": true,
	"PROCESSOR_IDENTIFIER":  true,
	"OS":                    true,
	"PROGRAMDATA":           true,
	"PROGRAMFILES":          true,
	"PROGRAMFILES(X86)":     true,
	"PROGRAMW6432":          true,
	"COMMONPROGRAMFILES":    true,
	"COMMONPROGRAMFILES(X86)": true,
	"COMMONPROGRAMW6432":    true,
	"LOCALAPPDATA":          true,
	"APPDATA":               true,
	"ALLUSERSPROFILE":       true,
	"PUBLIC":                true,
	"SESSIONNAME":           true,
	// Locale.
	"LANG":   true,
	"LC_ALL": true,
	// Go toolchain (build/test caches, module resolution). GOCACHE/GOMODCACHE
	// are required or `go test` re-downloads and recompiles the world.
	"GOPATH":      true,
	"GOROOT":      true,
	"GOBIN":       true,
	"GOCACHE":     true,
	"GOMODCACHE":  true,
	"GOTMPDIR":    true,
	"GOFLAGS":     true,
	"GOPROXY":     true,
	"GOSUMDB":     true,
	"GONOSUMDB":   true,
	"GONOSUMCHECK": true,
	"GOPRIVATE":   true,
	"GONOSUMDBPATH": true,
	"GO111MODULE": true,
	"GOOS":        true,
	"GOARCH":      true,
	"GOHOSTOS":    true,
	"GOHOSTARCH":  true,
	"GOTOOLCHAIN": true,
	"CGO_ENABLED": true,
	"CC":          true,
	"CXX":         true,
	// Python (interpreter/venv resolution — not secrets).
	"PYTHONPATH":  true,
	"PYTHONHOME":  true,
	"VIRTUAL_ENV": true,
	"CONDA_PREFIX": true,
	// Node (module resolution — NPM_TOKEN is deliberately NOT here).
	"NODE_PATH": true,
}

// commandEnv builds the explicit environment for a command.run child from the
// engine's environment: exactly the allowlisted keys that are present, plus the
// given overrides (which always win). The result is sorted for deterministic
// behavior. This is the S-02 boundary — secrets in the engine environment do
// not cross it.
func commandEnv(parent []string, overrides ...string) []string {
	env := make([]string, 0, len(commandEnvAllowlist)+len(overrides))
	overrideKeys := map[string]bool{}
	for _, kv := range overrides {
		if key, _, ok := strings.Cut(kv, "="); ok {
			overrideKeys[strings.ToUpper(strings.TrimSpace(key))] = true
		}
	}
	for _, kv := range parent {
		key, _, ok := strings.Cut(kv, "=")
		if !ok {
			continue
		}
		up := strings.ToUpper(strings.TrimSpace(key))
		if overrideKeys[up] {
			continue // an override replaces the inherited value
		}
		if commandEnvAllowlist[up] {
			env = append(env, kv)
		}
	}
	env = append(env, overrides...)
	sort.Strings(env)
	return env
}