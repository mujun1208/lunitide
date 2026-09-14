package desktopupdate

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/lunitide/lunitide/internal/domain/m7flow"
)

// FeedDocument is the on-disk latest.json written beside a Setup.
type FeedDocument struct {
	Version   string `json:"version"`
	Channel   string `json:"channel"`
	SHA256    string `json:"sha256"`
	Installer string `json:"installer"`
}

// Offer is a verified local installer the running app may apply.
type Offer struct {
	Version string
	Channel string
	Digest  string
	Path    string
}

// Feed looks up a local upgrade package. Channel "" accepts either track.
type Feed interface {
	Latest(channel string) (Offer, bool, error)
	Locate(digest string) (Offer, bool, error)
}

// LocalFeed reads latest.json from one or more drop directories.
type LocalFeed struct {
	Dirs []string
}

// DefaultDirs is %LOCALAPPDATA%\Lunitide\updates plus optional LUNITIDE_UPDATE_DIR.
func DefaultDirs() []string {
	var dirs []string
	if extra := strings.TrimSpace(os.Getenv("LUNITIDE_UPDATE_DIR")); extra != "" {
		dirs = append(dirs, extra)
	}
	if local := strings.TrimSpace(os.Getenv("LOCALAPPDATA")); local != "" {
		dirs = append(dirs, filepath.Join(local, "Lunitide", "updates"))
	}
	return dirs
}

func NewLocalFeed() *LocalFeed { return &LocalFeed{Dirs: DefaultDirs()} }

func (f *LocalFeed) dirs() []string {
	if f != nil && len(f.Dirs) > 0 {
		return f.Dirs
	}
	return DefaultDirs()
}

func (f *LocalFeed) Latest(channel string) (Offer, bool, error) {
	return Discover(f.dirs(), channel)
}

func (f *LocalFeed) Locate(digest string) (Offer, bool, error) {
	offer, ok, err := Discover(f.dirs(), "")
	if err != nil || !ok {
		return Offer{}, false, err
	}
	if !strings.EqualFold(offer.Digest, digest) {
		return Offer{}, false, nil
	}
	return offer, true, nil
}

// ParseFeed reads a latest.json document. Unknown fields fail closed.
func ParseFeed(raw []byte) (FeedDocument, error) {
	dec := json.NewDecoder(strings.NewReader(string(raw)))
	dec.DisallowUnknownFields()
	var doc FeedDocument
	if err := dec.Decode(&doc); err != nil {
		return FeedDocument{}, fmt.Errorf("desktopupdate: feed: %w", err)
	}
	if _, ok := m7flow.ParseVersion(doc.Version); !ok {
		return FeedDocument{}, fmt.Errorf("desktopupdate: invalid version %q", doc.Version)
	}
	if doc.Channel != m7flow.ChannelStable && doc.Channel != m7flow.ChannelBeta {
		return FeedDocument{}, fmt.Errorf("desktopupdate: invalid channel %q", doc.Channel)
	}
	if !validDigest(doc.SHA256) {
		return FeedDocument{}, fmt.Errorf("desktopupdate: invalid sha256")
	}
	want := "Lunitide-Setup-" + doc.Version + "-x64.exe"
	if doc.Installer != want || strings.ContainsAny(doc.Installer, `/\`) {
		return FeedDocument{}, fmt.Errorf("desktopupdate: installer name %q must be %q", doc.Installer, want)
	}
	return doc, nil
}

// ResolveOffer checks that dir/installer exists and matches the feed digest.
func ResolveOffer(dir string, doc FeedDocument) (Offer, error) {
	name := filepath.Base(doc.Installer)
	if name != doc.Installer {
		return Offer{}, fmt.Errorf("desktopupdate: installer must be a bare filename")
	}
	path := filepath.Join(dir, name)
	if err := VerifyInstaller(path, doc.SHA256); err != nil {
		return Offer{}, err
	}
	return Offer{Version: doc.Version, Channel: doc.Channel, Digest: strings.ToLower(doc.SHA256), Path: path}, nil
}

// Discover returns the newest valid offer on channel across dirs.
func Discover(dirs []string, channel string) (Offer, bool, error) {
	var best Offer
	found := false
	for _, dir := range dirs {
		raw, err := os.ReadFile(filepath.Join(dir, "latest.json"))
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return Offer{}, false, err
		}
		doc, err := ParseFeed(raw)
		if err != nil {
			return Offer{}, false, err
		}
		if channel != "" && doc.Channel != channel {
			continue
		}
		offer, err := ResolveOffer(dir, doc)
		if err != nil {
			return Offer{}, false, err
		}
		if !found || m7flow.CompareVersions(offer.Version, best.Version) > 0 {
			best = offer
			found = true
		}
	}
	return best, found, nil
}

// IsNewer reports whether candidate is a numeric upgrade over current.
func IsNewer(candidate, current string) bool {
	return m7flow.CompareVersions(candidate, current) > 0
}

func validDigest(v string) bool {
	if len(v) != 64 {
		return false
	}
	for _, c := range v {
		if c >= '0' && c <= '9' || c >= 'a' && c <= 'f' {
			continue
		}
		return false
	}
	return true
}

// VerifyInstaller requires path to be a regular file whose SHA-256 is want.
func VerifyInstaller(path, want string) error {
	want = strings.ToLower(strings.TrimSpace(want))
	if !validDigest(want) {
		return fmt.Errorf("desktopupdate: invalid digest")
	}
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("desktopupdate: installer: %w", err)
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("desktopupdate: installer is not a file")
	}
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	sum := sha256.New()
	if _, err := io.Copy(sum, f); err != nil {
		return err
	}
	got := hex.EncodeToString(sum.Sum(nil))
	if got != want {
		return fmt.Errorf("desktopupdate: installer digest mismatch")
	}
	return nil
}
