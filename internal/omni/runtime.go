package omni

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/lunitide/lunitide/internal/voice"
)

const runtimeWalkDepth = 6

// InstallRuntime fetches the pinned Comni NSIS installer (if needed) and
// unpacks llama-omni-server under root/runtime. No-op when the binary is
// already on disk.
func InstallRuntime(ctx context.Context, root string, installer *voice.Installer, progress func(voice.Progress)) error {
	if findRuntime(root) != "" {
		return nil
	}
	bundle := RuntimeBundle()
	if err := installer.Install(ctx, bundle, progress); err != nil {
		return err
	}
	setup := filepath.Join(installer.BundleDir(bundle.ID), RuntimeSetupFile)
	dest := filepath.Join(root, "runtime")
	if err := applyRuntimeSetup(setup, dest); err != nil {
		return err
	}
	if findRuntime(root) == "" {
		return ErrMissingRuntime
	}
	return nil
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
	if local := os.Getenv("LOCALAPPDATA"); local != "" {
		candidates = append(candidates,
			filepath.Join(local, "Programs", "Comni", "llama-omni-server.exe"),
			filepath.Join(local, "Comni", "llama-omni-server.exe"),
		)
	}
	if pf := os.Getenv("ProgramFiles"); pf != "" {
		candidates = append(candidates, filepath.Join(pf, "Comni", "llama-omni-server.exe"))
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
	for _, name := range names {
		if found, err := exec.LookPath(name); err == nil {
			return found
		}
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
