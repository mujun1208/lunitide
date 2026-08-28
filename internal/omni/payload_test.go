package omni

import (
	"archive/zip"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func writeStubZip(t *testing.T, path string, files map[string][]byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	writer := zip.NewWriter(file)
	for name, body := range files {
		entry, err := writer.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := entry.Write(body); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestEnsureBundledRuntimeExtractsZipAndSkipsWeights(t *testing.T) {
	root := t.TempDir()
	payload := filepath.Join(t.TempDir(), BundledRuntimeZip)
	writeStubZip(t, payload, map[string][]byte{
		"llama-omni-server.exe": []byte("server"),
		"ggml.dll":              []byte("dll"),
		"weights.gguf":          []byte("model"),
		"Comni.exe":             []byte("gui"),
	})
	if err := EnsureBundledRuntime(root, payload); err != nil {
		t.Fatal(err)
	}
	got := findRuntime(root)
	want := filepath.Join(root, "runtime", "llama-omni-server.exe")
	if got != want {
		t.Fatalf("findRuntime = %q; want %q", got, want)
	}
	if _, err := os.Stat(filepath.Join(root, "runtime", "ggml.dll")); err != nil {
		t.Fatalf("dll not copied: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "runtime", "weights.gguf")); !os.IsNotExist(err) {
		t.Fatal("GGUF weights must stay out of the bundled runtime")
	}
	if _, err := os.Stat(filepath.Join(root, "runtime", "Comni.exe")); !os.IsNotExist(err) {
		t.Fatal("Comni GUI must stay out of the bundled runtime")
	}
	if !runtimeStampMatches(filepath.Join(root, "runtime")) {
		t.Fatal("runtime stamp missing")
	}
	if err := EnsureBundledRuntime(root, payload); err != nil {
		t.Fatal(err)
	}
}

func TestEnsureBundledRuntimeCopiesDirectoryPayload(t *testing.T) {
	root := t.TempDir()
	payload := filepath.Join(t.TempDir(), "runtime")
	if err := os.MkdirAll(payload, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(payload, "llama-omni-server.exe"), []byte("server"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(payload, "cublas64.dll"), []byte("cuda"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := EnsureBundledRuntime(root, payload); err != nil {
		t.Fatal(err)
	}
	if findRuntime(root) == "" {
		t.Fatal("expected copied llama-omni-server")
	}
	if _, err := os.Stat(filepath.Join(root, "runtime", "cublas64.dll")); err != nil {
		t.Fatal(err)
	}
}

func TestEnsureBundledRuntimeRefusesZipTraversal(t *testing.T) {
	root := t.TempDir()
	payload := filepath.Join(t.TempDir(), BundledRuntimeZip)
	writeStubZip(t, payload, map[string][]byte{
		"../llama-omni-server.exe": []byte("nope"),
	})
	if err := EnsureBundledRuntime(root, payload); err == nil {
		t.Fatal("expected traversal to fail")
	}
	if findRuntime(root) != "" {
		t.Fatal("traversal must not install a binary")
	}
}

func TestEnsureBundledRuntimeWithoutPayload(t *testing.T) {
	t.Setenv("LUNITIDE_OMNI_PAYLOAD", "")
	root := t.TempDir()
	err := EnsureBundledRuntime(root, "")
	if err == nil {
		t.Fatal("expected missing payload to fail")
	}
	if !strings.Contains(err.Error(), "llama-omni-server") && !strings.Contains(err.Error(), "重装月汐") {
		t.Fatalf("error = %v", err)
	}
	if runtime.GOOS != "windows" && !strings.Contains(err.Error(), "Windows") {
		t.Fatalf("non-windows stub should say Windows-only, got %v", err)
	}
	if findRuntime(root) != "" {
		t.Fatal("empty root must not report a runtime")
	}
}

func TestHostSnapshotRuntimeReadyModelOptional(t *testing.T) {
	root := t.TempDir()
	payload := filepath.Join(t.TempDir(), BundledRuntimeZip)
	writeStubZip(t, payload, map[string][]byte{
		"llama-omni-server.exe": []byte("server"),
	})
	host := NewHost(root)
	host.Payload = payload
	isolateLoopback(host)
	if err := host.EnsureRuntime(); err != nil {
		t.Fatal(err)
	}
	snap := host.Snapshot()
	if snap["runtimeFound"] != true {
		t.Fatalf("runtimeFound = %v", snap["runtimeFound"])
	}
	if snap["installed"] != false {
		t.Fatalf("installed = %v; model must stay optional", snap["installed"])
	}
	if snap["hostState"] != HostMissingModel {
		t.Fatalf("hostState = %v; missing model is not missing runtime", snap["hostState"])
	}
	state, err := host.Ensure()
	if state != HostMissingModel || err == nil {
		t.Fatalf("Ensure with runtime but no model: state=%s err=%v", state, err)
	}
}

func TestIsRuntimeMemberKeepsServerRejectsModel(t *testing.T) {
	if !isRuntimeMember("bin/llama-omni-server.exe") {
		t.Fatal("server exe")
	}
	if !isRuntimeMember("ggml-cuda.dll") {
		t.Fatal("dll")
	}
	if isRuntimeMember("MiniCPM-o-4_5-Q4_K_M.gguf") {
		t.Fatal("gguf")
	}
	if isRuntimeMember("Comni.exe") {
		t.Fatal("comni gui")
	}
}
