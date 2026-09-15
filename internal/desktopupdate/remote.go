package desktopupdate

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/lunitide/lunitide/internal/networkpolicy"
)

// DefaultFeedURL is the GitHub Releases latest.json for this repository.
const DefaultFeedURL = "https://github.com/mujun1208/lunitide/releases/latest/download/latest.json"

const githubLatestSuffix = "/mujun1208/lunitide/releases/latest/download/latest.json"

// FeedURL is DefaultFeedURL unless LUNITIDE_UPDATE_FEED_URL is set (tests/mirrors).
func FeedURL() string {
	if override := strings.TrimSpace(os.Getenv("LUNITIDE_UPDATE_FEED_URL")); override != "" {
		return override
	}
	return DefaultFeedURL
}

// InstallerURL is the Setup location for version. GitHub latest.json maps to
// releases/download/v{version}/Lunitide-Setup-{version}-x64.exe; a custom feed
// uses the sibling filename next to latest.json.
func InstallerURL(feedURL, version string) string {
	name := "Lunitide-Setup-" + version + "-x64.exe"
	if isGitHubLatestFeed(feedURL) {
		return "https://github.com/mujun1208/lunitide/releases/download/v" + version + "/" + name
	}
	return strings.TrimSuffix(feedURL, "latest.json") + name
}

func isGitHubLatestFeed(feedURL string) bool {
	u, err := url.Parse(feedURL)
	if err != nil || u.Host == "" {
		return false
	}
	host := strings.ToLower(u.Hostname())
	return host == "github.com" && strings.EqualFold(u.Path, githubLatestSuffix)
}

// AllowedUpdateURL reports whether raw may be fetched for an update check or
// installer download. Production hosts are GitHub only. Loopback HTTP is
// allowed only when the feed override is the same loopback host.
func AllowedUpdateURL(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil || !u.IsAbs() || u.User != nil || u.Opaque != "" {
		return false
	}
	host := strings.ToLower(u.Hostname())
	scheme := strings.ToLower(u.Scheme)
	if scheme == "https" && isGitHubUpdateHost(host) {
		return true
	}
	if scheme != "http" && scheme != "https" {
		return false
	}
	override := strings.TrimSpace(os.Getenv("LUNITIDE_UPDATE_FEED_URL"))
	if override == "" {
		return false
	}
	ou, err := url.Parse(override)
	if err != nil || !ou.IsAbs() {
		return false
	}
	return sameLoopbackAuthority(u, ou)
}

func isGitHubUpdateHost(host string) bool {
	return host == "github.com" || host == "objects.githubusercontent.com" ||
		host == "release-assets.githubusercontent.com" ||
		host == "github-releases.githubusercontent.com" ||
		strings.HasSuffix(host, ".githubusercontent.com")
}

func sameLoopbackAuthority(target, feed *url.URL) bool {
	if !isLoopbackHost(target.Hostname()) || !isLoopbackHost(feed.Hostname()) {
		return false
	}
	return strings.EqualFold(target.Hostname(), feed.Hostname()) && target.Port() == feed.Port()
}

func isLoopbackHost(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// HTTPGet fetches a small URL body (latest.json). Tests inject this seam.
type HTTPGet func(ctx context.Context, rawURL string) ([]byte, error)

// HTTPGetFile streams a URL onto dest. Tests inject this seam.
type HTTPGetFile func(ctx context.Context, rawURL, dest string) error

const (
	maxFeedBytes      = 8 << 10
	maxInstallerBytes = 512 << 20
)

// FetchRemoteLatest GETs FeedURL and parses latest.json. Wrong channel is absent, not an error.
func FetchRemoteLatest(ctx context.Context, get HTTPGet, channel string) (FeedDocument, bool, error) {
	if get == nil {
		return FeedDocument{}, false, fmt.Errorf("desktopupdate: remote get unavailable")
	}
	rawURL := FeedURL()
	if !AllowedUpdateURL(rawURL) {
		return FeedDocument{}, false, fmt.Errorf("desktopupdate: feed host not allowed")
	}
	raw, err := get(ctx, rawURL)
	if err != nil {
		return FeedDocument{}, false, err
	}
	doc, err := ParseFeed(raw)
	if err != nil {
		return FeedDocument{}, false, err
	}
	if channel != "" && doc.Channel != channel {
		return FeedDocument{}, false, nil
	}
	return doc, true, nil
}

// EnsureLocalInstaller downloads the Setup for doc into destDir, verifies the
// digest, then writes latest.json. A bad download leaves neither file.
func EnsureLocalInstaller(ctx context.Context, destDir string, doc FeedDocument, getFile HTTPGetFile) (Offer, error) {
	if getFile == nil {
		return Offer{}, fmt.Errorf("desktopupdate: remote download unavailable")
	}
	if destDir == "" {
		return Offer{}, fmt.Errorf("desktopupdate: update store missing")
	}
	if _, err := ParseFeed(mustFeedJSON(doc)); err != nil {
		return Offer{}, err
	}
	name := filepath.Base(doc.Installer)
	if name != doc.Installer {
		return Offer{}, fmt.Errorf("desktopupdate: installer must be a bare filename")
	}
	rawURL := InstallerURL(FeedURL(), doc.Version)
	if !AllowedUpdateURL(rawURL) {
		return Offer{}, fmt.Errorf("desktopupdate: installer host not allowed")
	}
	if err := os.MkdirAll(destDir, 0o755); err != nil {
		return Offer{}, err
	}
	dest := filepath.Join(destDir, name)
	part := dest + ".part"
	_ = os.Remove(part)
	if err := getFile(ctx, rawURL, part); err != nil {
		_ = os.Remove(part)
		return Offer{}, err
	}
	if err := VerifyInstaller(part, doc.SHA256); err != nil {
		_ = os.Remove(part)
		return Offer{}, err
	}
	_ = os.Remove(dest)
	if err := os.Rename(part, dest); err != nil {
		_ = os.Remove(part)
		return Offer{}, err
	}
	if err := os.WriteFile(filepath.Join(destDir, "latest.json"), mustFeedJSON(doc), 0o644); err != nil {
		_ = os.Remove(dest)
		return Offer{}, err
	}
	return ResolveOffer(destDir, doc)
}

// CompositeLookup returns the newer of a local Setup and remote latest.json.
// A remote error without a local package is "no update", not a hard failure.
func CompositeLookup(local Feed, get HTTPGet, channel string) (string, string, bool, error) {
	var localOffer Offer
	var hasLocal bool
	if local != nil {
		offer, ok, err := local.Latest(channel)
		if err != nil {
			return "", "", false, err
		}
		localOffer, hasLocal = offer, ok
	}
	if get == nil {
		if hasLocal {
			return localOffer.Version, localOffer.Digest, true, nil
		}
		return "", "", false, nil
	}
	doc, hasRemote, err := FetchRemoteLatest(context.Background(), get, channel)
	if err != nil {
		if hasLocal {
			return localOffer.Version, localOffer.Digest, true, nil
		}
		return "", "", false, nil
	}
	if hasRemote && (!hasLocal || IsNewer(doc.Version, localOffer.Version)) {
		return doc.Version, doc.SHA256, true, nil
	}
	if hasLocal {
		return localOffer.Version, localOffer.Digest, true, nil
	}
	return "", "", false, nil
}

func mustFeedJSON(doc FeedDocument) []byte {
	raw, err := json.Marshal(FeedDocument{
		Version:   doc.Version,
		Channel:   doc.Channel,
		SHA256:    strings.ToLower(doc.SHA256),
		Installer: doc.Installer,
	})
	if err != nil {
		return []byte(`{}`)
	}
	return raw
}

func updateFetchPolicy() networkpolicy.Policy {
	override := strings.TrimSpace(os.Getenv("LUNITIDE_UPDATE_FEED_URL"))
	if override == "" {
		return networkpolicy.Policy{}
	}
	u, err := url.Parse(override)
	if err != nil || !u.IsAbs() || !isLoopbackHost(u.Hostname()) {
		return networkpolicy.Policy{}
	}
	return networkpolicy.Policy{AllowHTTP: true, AllowLocalhost: true}
}

// DefaultHTTPGet fetches latest.json through the SSRF-safe client.
func DefaultHTTPGet(ctx context.Context, rawURL string) ([]byte, error) {
	if !AllowedUpdateURL(rawURL) {
		return nil, fmt.Errorf("desktopupdate: feed host not allowed")
	}
	res, err := networkpolicy.Fetch(ctx, rawURL, networkpolicy.FetchOptions{
		Policy:         updateFetchPolicy(),
		MaxBodyBytes:   maxFeedBytes,
		OverallTimeout: 20 * time.Second,
		UserAgent:      "Lunitide/0.4 (app update feed)",
	})
	if err != nil {
		return nil, err
	}
	if res.Status != http.StatusOK || res.Truncated || len(res.Body) == 0 {
		return nil, fmt.Errorf("desktopupdate: feed status %d", res.Status)
	}
	return res.Body, nil
}

// DefaultHTTPGetFile streams an installer through the SSRF-safe client.
func DefaultHTTPGetFile(ctx context.Context, rawURL, dest string) error {
	if !AllowedUpdateURL(rawURL) {
		return fmt.Errorf("desktopupdate: installer host not allowed")
	}
	f, err := os.Create(dest)
	if err != nil {
		return err
	}
	defer f.Close()
	_, res, err := networkpolicy.Copy(ctx, rawURL, f, networkpolicy.FetchOptions{
		Policy:         updateFetchPolicy(),
		MaxBodyBytes:   maxInstallerBytes,
		OverallTimeout: 15 * time.Minute,
		UserAgent:      "Lunitide/0.4 (app update installer)",
	})
	if err != nil {
		return err
	}
	if res.Status != http.StatusOK || res.Truncated {
		return fmt.Errorf("desktopupdate: installer status %d", res.Status)
	}
	return nil
}
