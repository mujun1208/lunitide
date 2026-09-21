package webviewhost

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The nesting mistake is invisible: index.html still resolves, the window still
// opens, and the app silently serves whatever bundle shipped last time. Without
// this tripwire the only symptom is "your fix isn't in the build".
func TestCheckNestedRendererDeployRejectsStaleBundle(t *testing.T) {
	folder := t.TempDir()
	write(t, filepath.Join(folder, "index.html"), "<html>old release</html>")
	if err := checkNestedRendererDeploy(folder); err != nil {
		t.Fatalf("clean deployment rejected: %v", err)
	}
	nested := filepath.Join(folder, "dist")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(nested, "index.html"), "<html>new release</html>")
	err := checkNestedRendererDeploy(folder)
	if err == nil {
		t.Fatal("nested web/dist/dist accepted; a stale bundle would be served")
	}
	// The message has to name the fix, or the next person repeats the copy.
	if !strings.Contains(err.Error(), "contents of web/dist") {
		t.Fatalf("error does not say how to recover: %v", err)
	}
}

// A directory named dist/index.html is not the nesting signature; refusing to
// start on it would break trees that keep a source folder beside the bundle.
func TestCheckNestedRendererDeployIgnoresDirectory(t *testing.T) {
	folder := t.TempDir()
	write(t, filepath.Join(folder, "index.html"), "<html></html>")
	if err := os.MkdirAll(filepath.Join(folder, "dist", "index.html"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := checkNestedRendererDeploy(folder); err != nil {
		t.Fatalf("directory mistaken for a nested bundle: %v", err)
	}
}

func write(t *testing.T, path, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}
