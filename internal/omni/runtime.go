package omni

import (
	"os"
	"path/filepath"
	"strings"
)

const runtimeWalkDepth = 6

// InstallRuntime lays down the product-bundled llama-omni-server under
// root/runtime. payload is a zip or directory; empty discovers the archive
// next to the engine. No-op when the binary is already on disk.
func InstallRuntime(root, payload string) error {
	return EnsureBundledRuntime(root, payload)
}

func findRuntime(root string) string {
	names := []string{"llama-omni-server.exe", "llama-omni-server"}
	var candidates []string
	for _, name := range names {
		candidates = append(candidates,
			filepath.Join(root, "runtime", name),
			filepath.Join(root, name),
		)
	}
	for _, path := range candidates {
		if info, err := os.Stat(path); err == nil && !info.IsDir() {
			return path
		}
	}
	if nested := walkRuntime(filepath.Join(root, "runtime"), runtimeWalkDepth); nested != "" {
		return nested
	}
	if nested := walkRuntime(root, 2); nested != "" {
		return nested
	}
	return ""
}

func walkRuntime(dir string, maxDepth int) string {
	var found string
	var walk func(string, int)
	walk = func(path string, depth int) {
		if found != "" || depth > maxDepth {
			return
		}
		entries, err := os.ReadDir(path)
		if err != nil {
			return
		}
		for _, entry := range entries {
			full := filepath.Join(path, entry.Name())
			if entry.IsDir() {
				walk(full, depth+1)
				continue
			}
			name := entry.Name()
			if strings.EqualFold(name, "llama-omni-server.exe") || name == "llama-omni-server" {
				found = full
				return
			}
		}
	}
	walk(dir, 0)
	return found
}
