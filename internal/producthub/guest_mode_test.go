package producthub

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
)

func forceGuestMode(t *testing.T) {
	t.Helper()
	old := sourceRootProbe
	sourceRootProbe = func() string { return "" }
	t.Cleanup(func() { sourceRootProbe = old })
}

func TestGuestApplyIsRefused(t *testing.T) {
	forceGuestMode(t)
	s := New(&MemoryPersist{})
	if _, err := s.Apply(context.Background(), "", ""); !errors.Is(err, ErrNoSource) {
		t.Fatalf("guest apply-all not refused: %v", err)
	}
	if _, err := s.Apply(context.Background(), "wont_fix", "PH_L01|probe.dictate"); !errors.Is(err, ErrNoSource) {
		t.Fatalf("guest wont_fix not refused: %v", err)
	}
}

func TestGuestReportCarriesReadOnlyNote(t *testing.T) {
	forceGuestMode(t)
	md, pageHTML := RenderReport(Edition{EditionID: "guest"})
	if !strings.Contains(md, "只读模式：程序没有定位到产品源码根，不能自净化修复升级") || !strings.Contains(md, "LUNITIDE_SOURCE_ROOT") {
		t.Fatalf("markdown guest note missing:\n%s", md)
	}
	if !strings.Contains(pageHTML, "只读模式") || !strings.Contains(pageHTML, "LUNITIDE_SOURCE_ROOT") {
		t.Fatalf("html guest note missing:\n%s", pageHTML)
	}
}

func TestConfiguredSourceRootWinsOverProbing(t *testing.T) {
	root := filepath.Join(t.TempDir(), "app")
	writeProductFixture(t, root)
	other := filepath.Join(t.TempDir(), "app")
	writeProductFixture(t, other)
	old := configuredSourceRoot
	configuredSourceRoot = other
	t.Cleanup(func() {
		configuredSourceRoot = old
		resetProductSurface()
	})
	if !SetConfiguredSourceRoot(root) {
		t.Fatal("a valid configured root was refused")
	}
	if got := FindProductRoot(); got != root {
		t.Fatalf("configured root %q not used, got %q", root, got)
	}
	if IsGuestMode() {
		t.Fatal("a valid configured root was still counted as guest")
	}
}

func TestConfiguredSourceRootRefusesADirectoryWithoutProductFiles(t *testing.T) {
	nowhere := filepath.Join(t.TempDir(), "nowhere")
	if SetConfiguredSourceRoot(nowhere) {
		t.Fatal("a directory without product files was accepted")
	}
	if got := FindProductRoot(); got == nowhere {
		t.Fatal("a refused root still won the lookup")
	}
}

func TestLockedSourceRootWinsOverProbing(t *testing.T) {
	root := filepath.Join(t.TempDir(), "app")
	writeProductFixture(t, root)
	old := lockedSourceRoot
	lockedSourceRoot = root
	t.Cleanup(func() { lockedSourceRoot = old })
	if got := FindProductRoot(); got != root {
		t.Fatalf("locked root %q not used, got %q", root, got)
	}
	if IsGuestMode() {
		t.Fatal("a valid locked root was still counted as guest")
	}
}

func TestMissingLockedSourceRootFallsBackToProbing(t *testing.T) {
	old := lockedSourceRoot
	lockedSourceRoot = filepath.Join(t.TempDir(), "nowhere")
	t.Cleanup(func() { lockedSourceRoot = old })
	if FindProductRoot() == "" {
		t.Fatal("a missing locked root lost the probed checkout")
	}
}
