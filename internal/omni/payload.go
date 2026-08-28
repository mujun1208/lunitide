package omni

import (
	"archive/zip"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"strings"
)

const (
	// BundledRuntimeZip is the slim llama-omni-server payload staged next to
	// lunitide-engine.exe (omni/llama-omni-runtime.zip). It is not the 8 GB
	// MiniCPM-o snapshot and it is not the full Comni GUI installer.
	BundledRuntimeZip = "llama-omni-runtime.zip"
	runtimeStampName  = ".lunitide-omni-runtime"
)

// EnsureBundledRuntime copies or extracts the product-bundled llama-omni-server
// into root/runtime. payload is a zip or a directory; empty means discover the
// archive next to the running engine. No-op when the binary is already present
// at RuntimeRevision. Does not download Comni-Setup and does not fetch GGUF.
func EnsureBundledRuntime(root, payload string) error {
	dest := filepath.Join(root, "runtime")
	if findRuntime(root) != "" && runtimeStampMatches(dest) {
		return nil
	}
	if payload == "" {
		payload = discoverBundledPayload()
	}
	if payload == "" {
		if findRuntime(root) != "" {
			_ = writeRuntimeStamp(dest)
			return nil
		}
		if runtime.GOOS != "windows" {
			return fmt.Errorf("%w: llama-omni-server 内置运行时仅随 Windows 安装包提供", ErrMissingRuntime)
		}
		return fmt.Errorf("%w: 产品内置运行时缺失，请重装月汐", ErrMissingRuntime)
	}
	if err := os.MkdirAll(dest, 0o755); err != nil {
		return err
	}
	info, err := os.Stat(payload)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrMissingRuntime, err)
	}
	if info.IsDir() {
		if err := copyRuntimeTree(payload, dest); err != nil {
			return err
		}
	} else {
		if err := extractRuntimeZip(payload, dest); err != nil {
			return err
		}
	}
	if findRuntime(root) == "" {
		return ErrMissingRuntime
	}
	return writeRuntimeStamp(dest)
}

func discoverBundledPayload() string {
	if override := strings.TrimSpace(os.Getenv("LUNITIDE_OMNI_PAYLOAD")); override != "" {
		if _, err := os.Stat(override); err == nil {
			return override
		}
	}
	exe, err := os.Executable()
	if err != nil {
		return ""
	}
	dir := filepath.Dir(exe)
	zipPath := filepath.Join(dir, "omni", BundledRuntimeZip)
	if info, err := os.Stat(zipPath); err == nil && !info.IsDir() {
		return zipPath
	}
	tree := filepath.Join(dir, "omni", "runtime")
	if info, err := os.Stat(tree); err == nil && info.IsDir() {
		if walkRuntime(tree, runtimeWalkDepth) != "" {
			return tree
		}
	}
	return ""
}

func runtimeStampPath(dest string) string {
	return filepath.Join(dest, runtimeStampName)
}

func runtimeStampMatches(dest string) bool {
	raw, err := os.ReadFile(runtimeStampPath(dest))
	if err != nil {
		return false
	}
	return strings.TrimSpace(string(raw)) == RuntimeRevision
}

func writeRuntimeStamp(dest string) error {
	if err := os.MkdirAll(dest, 0o755); err != nil {
		return err
	}
	return os.WriteFile(runtimeStampPath(dest), []byte(RuntimeRevision+"\n"), 0o644)
}

func extractRuntimeZip(zipPath, dest string) error {
	reader, err := zip.OpenReader(zipPath)
	if err != nil {
		return fmt.Errorf("omni: open bundled runtime: %w", err)
	}
	defer reader.Close()
	root, err := filepath.Abs(dest)
	if err != nil {
		return err
	}
	for _, file := range reader.File {
		if file.FileInfo().IsDir() {
			continue
		}
		name := strings.ReplaceAll(file.Name, `\`, "/")
		destination, err := safeRuntimeJoin(root, name)
		if err != nil {
			return err
		}
		if !isRuntimeMember(name) {
			continue
		}
		if err := os.MkdirAll(filepath.Dir(destination), 0o755); err != nil {
			return err
		}
		if err := writeZipMember(destination, file); err != nil {
			return err
		}
	}
	return nil
}

func writeZipMember(destination string, file *zip.File) error {
	in, err := file.Open()
	if err != nil {
		return fmt.Errorf("omni: read %s: %w", file.Name, err)
	}
	defer in.Close()
	out, err := os.OpenFile(destination, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o755)
	if err != nil {
		return err
	}
	defer out.Close()
	if _, err := io.Copy(out, in); err != nil {
		return err
	}
	return out.Close()
}

func copyRuntimeTree(src, dest string) error {
	root, err := filepath.Abs(dest)
	if err != nil {
		return err
	}
	return filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}
		if info.IsDir() {
			return nil
		}
		name := strings.ReplaceAll(rel, `\`, "/")
		destination, err := safeRuntimeJoin(root, name)
		if err != nil {
			return err
		}
		if !isRuntimeMember(name) {
			return nil
		}
		if err := os.MkdirAll(filepath.Dir(destination), 0o755); err != nil {
			return err
		}
		return copyFile(path, destination)
	})
}

func copyFile(src, dest string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dest, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o755)
	if err != nil {
		return err
	}
	defer out.Close()
	if _, err := io.Copy(out, in); err != nil {
		return err
	}
	return out.Close()
}

// isRuntimeMember keeps llama-omni-server and its shared libraries. GGUF
// weights, the Comni GUI, and Node/Python/Electron trees stay out — those
// either download on demand or are not how 月汐 talks to the server.
func isRuntimeMember(rel string) bool {
	slashed := strings.ToLower(strings.ReplaceAll(rel, `\`, "/"))
	base := path.Base(slashed)
	switch {
	case strings.HasSuffix(base, ".gguf"), strings.HasSuffix(base, ".pdb"):
		return false
	case base == "comni.exe", base == "uninstall.exe":
		return false
	case strings.Contains(slashed, "node_modules"), strings.Contains(slashed, "/electron"), strings.Contains(slashed, "resources.pak"):
		return false
	case strings.HasSuffix(base, ".asar"), strings.HasSuffix(base, ".pyc"):
		return false
	case base == "llama-omni-server.exe", base == "llama-omni-server":
		return true
	case strings.HasSuffix(base, ".dll"), strings.HasSuffix(base, ".so"), strings.HasSuffix(base, ".dylib"):
		return true
	default:
		return false
	}
}

func safeRuntimeJoin(root, name string) (string, error) {
	slashed := strings.ReplaceAll(name, `\`, "/")
	if slashed == "" {
		return "", fmt.Errorf("omni: empty runtime member name")
	}
	if strings.HasPrefix(slashed, "/") {
		return "", fmt.Errorf("omni: %s is an absolute path", name)
	}
	if strings.Contains(slashed, ":") {
		return "", fmt.Errorf("omni: %s names a volume or stream", name)
	}
	for _, part := range strings.Split(slashed, "/") {
		if part == ".." {
			return "", fmt.Errorf("omni: %s traverses out of the extraction directory", name)
		}
	}
	joined := filepath.Join(root, filepath.FromSlash(path.Clean(slashed)))
	if joined != root && !strings.HasPrefix(joined, root+string(os.PathSeparator)) {
		return "", fmt.Errorf("omni: %s escapes the extraction directory", name)
	}
	return joined, nil
}
