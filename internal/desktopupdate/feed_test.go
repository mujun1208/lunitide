package desktopupdate

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"

	"github.com/lunitide/lunitide/internal/domain/m7flow"
)

func TestParseFeedRejectsUnknownFieldsAndBadDigest(t *testing.T) {
	if _, err := ParseFeed([]byte(`{"version":"0.4.82","channel":"stable","sha256":"zz","installer":"Lunitide-Setup-0.4.82-x64.exe"}`)); err == nil {
		t.Fatal("bad digest must fail")
	}
	if _, err := ParseFeed([]byte(`{"version":"0.4.82","channel":"stable","sha256":"` + hex.EncodeToString(make([]byte, 32)) + `","installer":"evil.exe","extra":1}`)); err == nil {
		t.Fatal("unknown field must fail")
	}
}

func TestResolveOfferRequiresMatchingNameAndHash(t *testing.T) {
	dir := t.TempDir()
	body := []byte("setup-bytes")
	sum := sha256.Sum256(body)
	digest := hex.EncodeToString(sum[:])
	name := "Lunitide-Setup-0.4.82-x64.exe"
	if err := os.WriteFile(filepath.Join(dir, name), body, 0o644); err != nil {
		t.Fatal(err)
	}
	doc := FeedDocument{Version: "0.4.82", Channel: "stable", SHA256: digest, Installer: name}
	offer, err := ResolveOffer(dir, doc)
	if err != nil {
		t.Fatal(err)
	}
	if offer.Path != filepath.Join(dir, name) || offer.Digest != digest {
		t.Fatalf("offer = %+v", offer)
	}
	doc.Installer = "Lunitide-Setup-0.4.81-x64.exe"
	if _, err := ResolveOffer(dir, doc); err == nil {
		t.Fatal("name/version mismatch must fail")
	}
	doc.Installer = name
	doc.SHA256 = hex.EncodeToString(make([]byte, 32))
	if _, err := ResolveOffer(dir, doc); err == nil {
		t.Fatal("hash mismatch must fail")
	}
}

func TestDiscoverPicksNewerStableOffer(t *testing.T) {
	dir := t.TempDir()
	body := []byte("setup-bytes-0.4.83")
	sum := sha256.Sum256(body)
	digest := hex.EncodeToString(sum[:])
	name := "Lunitide-Setup-0.4.83-x64.exe"
	if err := os.WriteFile(filepath.Join(dir, name), body, 0o644); err != nil {
		t.Fatal(err)
	}
	feed := `{"version":"0.4.83","channel":"stable","sha256":"` + digest + `","installer":"` + name + `"}`
	if err := os.WriteFile(filepath.Join(dir, "latest.json"), []byte(feed), 0o644); err != nil {
		t.Fatal(err)
	}
	offer, ok, err := Discover([]string{dir}, "stable")
	if err != nil || !ok {
		t.Fatalf("discover: ok=%v err=%v", ok, err)
	}
	if offer.Version != "0.4.83" || !IsNewer(offer.Version, "0.4.82") {
		t.Fatalf("offer version %q", offer.Version)
	}
	if m7flow.CompareVersions(offer.Version, "0.4.83") != 0 {
		t.Fatal("version parse")
	}
	if IsNewer("0.4.82", "0.4.82") || IsNewer("0.4.81", "0.4.82") {
		t.Fatal("IsNewer must be strict")
	}
}
