package toolruntime

import (
	"sort"
	"strings"
	"testing"
)

// envMap turns the KEY=VALUE slice returned by commandEnv into a case-insensitive
// lookup so assertions do not depend on ordering or key casing.
func envMap(t *testing.T, kvs []string) map[string]string {
	t.Helper()
	m := map[string]string{}
	for _, kv := range kvs {
		key, val, ok := strings.Cut(kv, "=")
		if !ok {
			t.Fatalf("malformed env entry (no '='): %q", kv)
		}
		m[strings.ToUpper(strings.TrimSpace(key))] = val
	}
	return m
}

// TestCommandEnvDropsSecrets is the core S-02 guarantee: variables that are not
// on the allowlist — provider keys, tokens, arbitrary secrets in the engine
// environment — never reach the child process.
func TestCommandEnvDropsSecrets(t *testing.T) {
	parent := []string{
		"PATH=C:\\Windows\\System32",
		"OPENAI_API_KEY=sk-should-not-leak",
		"ANTHROPIC_API_KEY=should-not-leak",
		"NPM_TOKEN=should-not-leak",
		"AWS_SECRET_ACCESS_KEY=should-not-leak",
		"GITHUB_TOKEN=should-not-leak",
		"LUNITIDE_ENGINE_NONCE=should-not-leak",
		"SOME_RANDOM_SECRET=should-not-leak",
	}
	env := commandEnv(parent)
	got := envMap(t, env)

	if _, ok := got["PATH"]; !ok {
		t.Fatalf("expected allowlisted PATH to be inherited, got %v", env)
	}
	for _, secret := range []string{
		"OPENAI_API_KEY", "ANTHROPIC_API_KEY", "NPM_TOKEN",
		"AWS_SECRET_ACCESS_KEY", "GITHUB_TOKEN",
		"LUNITIDE_ENGINE_NONCE", "SOME_RANDOM_SECRET",
	} {
		if _, ok := got[secret]; ok {
			t.Errorf("secret %s leaked into command environment", secret)
		}
	}
	// Belt-and-suspenders: no VALUE in the output should be the sentinel.
	for _, kv := range env {
		if strings.Contains(kv, "should-not-leak") || strings.Contains(kv, "sk-should-not-leak") {
			t.Errorf("leaked secret value present: %q", kv)
		}
	}
}

// TestCommandEnvKeepsAllowlisted confirms the system/toolchain variables a
// build/test/git command legitimately needs do pass through with their values.
func TestCommandEnvKeepsAllowlisted(t *testing.T) {
	parent := []string{
		"PATH=C:\\Windows\\System32",
		"SYSTEMROOT=C:\\Windows",
		"TEMP=C:\\Temp",
		"GOCACHE=C:\\gocache",
		"GOMODCACHE=C:\\gomodcache",
		"USERPROFILE=C:\\Users\\dev",
	}
	got := envMap(t, commandEnv(parent))
	for k, want := range map[string]string{
		"PATH":        "C:\\Windows\\System32",
		"SYSTEMROOT":  "C:\\Windows",
		"TEMP":        "C:\\Temp",
		"GOCACHE":     "C:\\gocache",
		"GOMODCACHE":  "C:\\gomodcache",
		"USERPROFILE": "C:\\Users\\dev",
	} {
		if got[k] != want {
			t.Errorf("allowlisted %s = %q, want %q", k, got[k], want)
		}
	}
}

// TestCommandEnvOverridesWin verifies an explicit override replaces any
// inherited value for the same key and is always present in the result.
func TestCommandEnvOverridesWin(t *testing.T) {
	parent := []string{
		"PATH=C:\\Windows\\System32",
		"PAGER=less", // would be inherited if PAGER were allowlisted; overridden regardless
	}
	env := commandEnv(parent,
		"GIT_PAGER=cat",
		"PAGER=cat",
		"TERM=dumb",
		"GIT_OPTIONAL_LOCKS=0",
		"PYTHONIOENCODING=utf-8",
		"PYTHONUTF8=1",
	)
	got := envMap(t, env)

	for k, want := range map[string]string{
		"GIT_PAGER":          "cat",
		"PAGER":              "cat",
		"TERM":               "dumb",
		"GIT_OPTIONAL_LOCKS": "0",
		"PYTHONIOENCODING":   "utf-8",
		"PYTHONUTF8":         "1",
	} {
		if got[k] != want {
			t.Errorf("override %s = %q, want %q", k, got[k], want)
		}
	}
	// The inherited PAGER=less must not appear as a duplicate; only the override.
	pagerCount := 0
	for _, kv := range env {
		if key, _, _ := strings.Cut(kv, "="); strings.EqualFold(strings.TrimSpace(key), "PAGER") {
			pagerCount++
		}
	}
	if pagerCount != 1 {
		t.Errorf("expected exactly one PAGER entry, got %d in %v", pagerCount, env)
	}
}

// TestCommandEnvCaseInsensitive confirms Windows-style case-insensitive matching
// against the allowlist and override keys.
func TestCommandEnvCaseInsensitive(t *testing.T) {
	parent := []string{
		"path=C:\\Windows\\System32", // lowercase key still matches PATH allowlist
		"Temp=C:\\Temp",
	}
	got := envMap(t, commandEnv(parent, "gIt_PaGeR=cat"))
	if got["PATH"] != "C:\\Windows\\System32" {
		t.Errorf("lowercase path not matched to PATH allowlist: %v", got)
	}
	if got["TEMP"] != "C:\\Temp" {
		t.Errorf("mixed-case Temp not matched: %v", got)
	}
	if got["GIT_PAGER"] != "cat" {
		t.Errorf("mixed-case override not applied: %v", got)
	}
}

// TestCommandEnvDeterministic confirms the output is sorted for stable behavior.
func TestCommandEnvDeterministic(t *testing.T) {
	parent := []string{
		"TEMP=C:\\Temp",
		"PATH=C:\\Windows\\System32",
		"GOCACHE=C:\\gocache",
	}
	env := commandEnv(parent, "TERM=dumb", "GIT_PAGER=cat")
	if !sort.StringsAreSorted(env) {
		t.Errorf("commandEnv output is not sorted: %v", env)
	}
}

// TestCommandEnvSkipsMalformed confirms parent entries without '=' are ignored
// rather than panicking or being passed through.
func TestCommandEnvSkipsMalformed(t *testing.T) {
	parent := []string{
		"PATH=C:\\Windows\\System32",
		"MALFORMED_NO_EQUALS",
		"",
	}
	env := commandEnv(parent)
	for _, kv := range env {
		if kv == "MALFORMED_NO_EQUALS" || kv == "" {
			t.Errorf("malformed entry leaked: %q", kv)
		}
	}
}
