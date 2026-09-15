package desktopupdate

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func TestDefaultHTTPGetRejectsForeignHost(t *testing.T) {
	if _, err := DefaultHTTPGet(context.Background(), "https://evil.example/latest.json"); err == nil {
		t.Fatal("foreign host must fail closed")
	}
}

func TestFeedURLHonorsOverride(t *testing.T) {
	t.Setenv("LUNITIDE_UPDATE_FEED_URL", "http://127.0.0.1:9/latest.json")
	if got := FeedURL(); got != "http://127.0.0.1:9/latest.json" {
		t.Fatalf("FeedURL = %q", got)
	}
}

func TestInstallerURLMapsGitHubLatestFeed(t *testing.T) {
	got := InstallerURL(DefaultFeedURL, "0.4.83")
	want := "https://github.com/mujun1208/lunitide/releases/download/v0.4.83/Lunitide-Setup-0.4.83-x64.exe"
	if got != want {
		t.Fatalf("InstallerURL = %q want %q", got, want)
	}
}

func TestInstallerURLUsesSiblingOfCustomFeed(t *testing.T) {
	got := InstallerURL("http://127.0.0.1:9/latest.json", "0.4.83")
	want := "http://127.0.0.1:9/Lunitide-Setup-0.4.83-x64.exe"
	if got != want {
		t.Fatalf("InstallerURL = %q want %q", got, want)
	}
}

func TestAllowedUpdateURLRejectsForeignHost(t *testing.T) {
	if !AllowedUpdateURL("https://github.com/mujun1208/lunitide/releases/latest/download/latest.json") {
		t.Fatal("github.com feed must be allowed")
	}
	if !AllowedUpdateURL("https://objects.githubusercontent.com/github-production-release-asset/1") {
		t.Fatal("GitHub asset host must be allowed")
	}
	if AllowedUpdateURL("https://evil.example/latest.json") {
		t.Fatal("foreign host must be rejected")
	}
	if AllowedUpdateURL("http://127.0.0.1:9/latest.json") {
		t.Fatal("loopback must stay closed unless the feed override is loopback")
	}
	t.Setenv("LUNITIDE_UPDATE_FEED_URL", "http://127.0.0.1:9/latest.json")
	if !AllowedUpdateURL("http://127.0.0.1:9/Lunitide-Setup-0.4.83-x64.exe") {
		t.Fatal("loopback sibling of the override feed must be allowed")
	}
}

func TestFetchRemoteLatestParsesStableFeed(t *testing.T) {
	body := []byte("setup-bytes")
	digest := hex.EncodeToString(sha256Sum(body))
	feed := `{"version":"0.4.83","channel":"stable","sha256":"` + digest + `","installer":"Lunitide-Setup-0.4.83-x64.exe"}`
	var gotURL string
	get := func(_ context.Context, rawURL string) ([]byte, error) {
		gotURL = rawURL
		return []byte(feed), nil
	}
	doc, ok, err := FetchRemoteLatest(context.Background(), get, "stable")
	if err != nil || !ok {
		t.Fatalf("ok=%v err=%v", ok, err)
	}
	if gotURL != DefaultFeedURL {
		t.Fatalf("fetched %q", gotURL)
	}
	if doc.Version != "0.4.83" || doc.SHA256 != digest {
		t.Fatalf("doc = %+v", doc)
	}
	if _, ok, err := FetchRemoteLatest(context.Background(), get, "beta"); err != nil || ok {
		t.Fatalf("wrong channel must be absent, ok=%v err=%v", ok, err)
	}
}

func TestEnsureLocalInstallerWritesVerifiedSetup(t *testing.T) {
	dir := t.TempDir()
	body := []byte("setup-bytes")
	digest := hex.EncodeToString(sha256Sum(body))
	doc := FeedDocument{Version: "0.4.83", Channel: "stable", SHA256: digest, Installer: "Lunitide-Setup-0.4.83-x64.exe"}
	var gotURL string
	getFile := func(_ context.Context, rawURL, dest string) error {
		gotURL = rawURL
		return os.WriteFile(dest, body, 0o644)
	}
	offer, err := EnsureLocalInstaller(context.Background(), dir, doc, getFile)
	if err != nil {
		t.Fatal(err)
	}
	if gotURL != InstallerURL(DefaultFeedURL, "0.4.83") {
		t.Fatalf("downloaded %q", gotURL)
	}
	if offer.Path != filepath.Join(dir, doc.Installer) || offer.Digest != digest {
		t.Fatalf("offer = %+v", offer)
	}
	if _, err := os.Stat(filepath.Join(dir, "latest.json")); err != nil {
		t.Fatal(err)
	}
	if err := VerifyInstaller(offer.Path, digest); err != nil {
		t.Fatal(err)
	}
}

func TestEnsureLocalInstallerReplacesExistingSetup(t *testing.T) {
	dir := t.TempDir()
	name := "Lunitide-Setup-0.4.83-x64.exe"
	if err := os.WriteFile(filepath.Join(dir, name), []byte("old-setup"), 0o644); err != nil {
		t.Fatal(err)
	}
	body := []byte("setup-bytes")
	digest := hex.EncodeToString(sha256Sum(body))
	doc := FeedDocument{Version: "0.4.83", Channel: "stable", SHA256: digest, Installer: name}
	getFile := func(_ context.Context, _, dest string) error {
		return os.WriteFile(dest, body, 0o644)
	}
	if _, err := EnsureLocalInstaller(context.Background(), dir, doc, getFile); err != nil {
		t.Fatal(err)
	}
	if err := VerifyInstaller(filepath.Join(dir, name), digest); err != nil {
		t.Fatal(err)
	}
}

func TestEnsureLocalInstallerRejectsDigestMismatch(t *testing.T) {
	dir := t.TempDir()
	digest := hex.EncodeToString(sha256Sum([]byte("setup-bytes")))
	doc := FeedDocument{Version: "0.4.83", Channel: "stable", SHA256: digest, Installer: "Lunitide-Setup-0.4.83-x64.exe"}
	getFile := func(_ context.Context, _, dest string) error {
		return os.WriteFile(dest, []byte("wrong-bytes"), 0o644)
	}
	if _, err := EnsureLocalInstaller(context.Background(), dir, doc, getFile); err == nil {
		t.Fatal("digest mismatch must fail")
	}
	if _, err := os.Stat(filepath.Join(dir, doc.Installer)); !os.IsNotExist(err) {
		t.Fatalf("bad installer must not remain: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "latest.json")); !os.IsNotExist(err) {
		t.Fatal("latest.json must not be written after a bad download")
	}
}

func TestCompositeLookupPrefersNewerRemote(t *testing.T) {
	dir := t.TempDir()
	localBody := []byte("local-setup")
	localDigest := hex.EncodeToString(sha256Sum(localBody))
	name := "Lunitide-Setup-0.4.82-x64.exe"
	if err := os.WriteFile(filepath.Join(dir, name), localBody, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "latest.json"), []byte(`{"version":"0.4.82","channel":"stable","sha256":"`+localDigest+`","installer":"`+name+`"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	remoteDigest := hex.EncodeToString(sha256Sum([]byte("remote-setup")))
	get := func(context.Context, string) ([]byte, error) {
		return []byte(`{"version":"0.4.83","channel":"stable","sha256":"` + remoteDigest + `","installer":"Lunitide-Setup-0.4.83-x64.exe"}`), nil
	}
	version, digest, ok, err := CompositeLookup(&LocalFeed{Dirs: []string{dir}}, get, "stable")
	if err != nil || !ok || version != "0.4.83" || digest != remoteDigest {
		t.Fatalf("lookup = %s %s ok=%v err=%v", version, digest, ok, err)
	}
}

func TestCompositeLookupIgnoresRemoteFailureWhenLocalExists(t *testing.T) {
	dir := t.TempDir()
	body := []byte("local-setup")
	digest := hex.EncodeToString(sha256Sum(body))
	name := "Lunitide-Setup-0.4.82-x64.exe"
	if err := os.WriteFile(filepath.Join(dir, name), body, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "latest.json"), []byte(`{"version":"0.4.82","channel":"stable","sha256":"`+digest+`","installer":"`+name+`"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	get := func(context.Context, string) ([]byte, error) {
		return nil, fmt.Errorf("network down")
	}
	version, got, ok, err := CompositeLookup(&LocalFeed{Dirs: []string{dir}}, get, "stable")
	if err != nil || !ok || version != "0.4.82" || got != digest {
		t.Fatalf("lookup = %s %s ok=%v err=%v", version, got, ok, err)
	}
}

func TestCompositeLookupSilentWhenRemoteFailsAndLocalMissing(t *testing.T) {
	get := func(context.Context, string) ([]byte, error) {
		return nil, fmt.Errorf("network down")
	}
	version, digest, ok, err := CompositeLookup(&LocalFeed{Dirs: []string{t.TempDir()}}, get, "stable")
	if err != nil || ok || version != "" || digest != "" {
		t.Fatalf("lookup = %s %s ok=%v err=%v", version, digest, ok, err)
	}
}

func sha256Sum(body []byte) []byte {
	sum := sha256.Sum256(body)
	return sum[:]
}
