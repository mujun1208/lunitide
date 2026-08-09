//go:build windows

package sqlite

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestElectronDiscoveryAllowlistAndProductNamePath(t *testing.T) {
	roaming := t.TempDir()
	for _, name := range []string{"Lunitide 月汐", "untrusted-name"} {
		dir := filepath.Join(roaming, name)
		if err := os.Mkdir(dir, 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "providers.json"), []byte(sanitizedElectronFixture), 0600); err != nil {
			t.Fatal(err)
		}
	}
	found, err := discoverElectronProviderMetadataAt(roaming)
	if err != nil {
		t.Fatal(err)
	}
	if len(found) != 1 || len(found[0].providers) != 1 {
		t.Fatalf("product-name discovery = %#v", found)
	}
}

func TestElectronDiscoveryIncludesHistoricalLunitide(t *testing.T) {
	roaming := t.TempDir()
	dir := filepath.Join(roaming, "lunitide")
	if err := os.Mkdir(dir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "providers.json"), []byte(sanitizedElectronFixture), 0600); err != nil {
		t.Fatal(err)
	}
	found, err := discoverElectronProviderMetadataAt(roaming)
	if err != nil || len(found) != 1 {
		t.Fatalf("historical discovery count=%d err=%v", len(found), err)
	}
}

func TestElectronDiscoveryRejectsFileReparsePoint(t *testing.T) {
	roaming := t.TempDir()
	dir := filepath.Join(roaming, "Lunitide 月汐")
	if err := os.Mkdir(dir, 0700); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(roaming, "outside.json")
	if err := os.WriteFile(target, []byte(sanitizedElectronFixture), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, filepath.Join(dir, "providers.json")); err != nil {
		t.Skipf("creating Windows symlink requires developer mode or privilege: %v", err)
	}
	found, err := discoverElectronProviderMetadataAt(roaming)
	if err == nil || len(found) != 0 || !strings.Contains(err.Error(), "reparse point rejected") {
		t.Fatalf("reparse result count=%d err=%v", len(found), err)
	}
}

func TestElectronDiscoveryRejectsDirectoryReparsePoint(t *testing.T) {
	roaming := t.TempDir()
	target := t.TempDir()
	if err := os.WriteFile(filepath.Join(target, "providers.json"), []byte(sanitizedElectronFixture), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, filepath.Join(roaming, "Lunitide 月汐")); err != nil {
		t.Skipf("creating Windows directory symlink requires developer mode or privilege: %v", err)
	}
	found, err := discoverElectronProviderMetadataAt(roaming)
	if err == nil || len(found) != 0 || !strings.Contains(err.Error(), "directory reparse point rejected") {
		t.Fatalf("directory reparse result count=%d err=%v", len(found), err)
	}
}
