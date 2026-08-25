//go:build windows

package canonpath_test

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lunitide/lunitide/internal/canonpath"
	"golang.org/x/sys/windows"
)

// The two spellings Windows hands out for one directory both have to fold to
// the same answer, because every containment check in the tree compares a
// root against a child and they routinely arrive spelled differently.
func TestCanonicalFoldsJunctionAndTargetToOneName(t *testing.T) {
	base := t.TempDir()
	target := filepath.Join(base, "target")
	junction := filepath.Join(base, "junction")
	if err := os.MkdirAll(filepath.Join(target, "nested"), 0o755); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command("cmd.exe", "/d", "/s", "/c", "mklink", "/J", junction, target).CombinedOutput(); err != nil {
		t.Skipf("directory junction unavailable: %v: %s", err, out)
	}

	viaTarget, err := canonpath.Canonical(filepath.Join(target, "nested"))
	if err != nil {
		t.Fatal(err)
	}
	// filepath.EvalSymlinks reports ErrNotExist for this path, which is why
	// it cannot be used for containment on Windows.
	viaJunction, err := canonpath.Canonical(filepath.Join(junction, "nested"))
	if err != nil {
		t.Fatalf("path through a junction must resolve, got %v", err)
	}
	if !strings.EqualFold(viaTarget, viaJunction) {
		t.Fatalf("one directory produced two names: %q and %q", viaTarget, viaJunction)
	}
}

// A short 8.3 component names the same directory as its long form. GitHub's
// Windows runners hand out exactly this shape as %TEMP%, and comparing the
// two spellings raw is what made every workspace path look like an escape.
func TestCanonicalExpandsShortNameComponents(t *testing.T) {
	base := t.TempDir()
	long := filepath.Join(base, "a-directory-name-well-past-eight-characters")
	if err := os.MkdirAll(long, 0o755); err != nil {
		t.Fatal(err)
	}
	canonical, err := canonpath.Canonical(long)
	if err != nil {
		t.Fatal(err)
	}
	short, err := shortPath(long)
	if err != nil {
		t.Skipf("8.3 short names are disabled on this volume: %v", err)
	}
	if strings.EqualFold(short, long) {
		t.Skip("volume produced no distinct short name")
	}
	got, err := canonpath.Canonical(short)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.EqualFold(got, canonical) {
		t.Fatalf("short name resolved to %q, want %q", got, canonical)
	}
}

func TestCanonicalReportsMissingPathAsNotExist(t *testing.T) {
	_, err := canonpath.Canonical(filepath.Join(t.TempDir(), "absent"))
	if !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("missing path must report ErrNotExist, got %v", err)
	}
}

func shortPath(p string) (string, error) {
	ptr, err := windows.UTF16PtrFromString(p)
	if err != nil {
		return "", err
	}
	buf := make([]uint16, windows.MAX_PATH)
	n, err := windows.GetShortPathName(ptr, &buf[0], uint32(len(buf)))
	if err != nil {
		return "", err
	}
	return windows.UTF16ToString(buf[:n]), nil
}
