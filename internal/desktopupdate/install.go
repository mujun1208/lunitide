package desktopupdate

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
)

// SilentUpgradeArgs is the NSIS in-place upgrade switch. The installer already
// upgrades the registered location and refuses a path change.
func SilentUpgradeArgs() []string { return []string{"/S"} }

// NsisInstaller applies a verified local Setup. Missing files fail closed so a
// feed-adopted digest cannot no-op. Store/Get/GetFile fill a missing local
// file from the remote feed when the user clicks install.
type NsisInstaller struct {
	Feed    Feed
	Store   string
	Start   func(*exec.Cmd) error
	Get     HTTPGet
	GetFile HTTPGetFile
}

func NewNsisInstaller(feed Feed) *NsisInstaller {
	return &NsisInstaller{Feed: feed, Start: startDetached}
}

func (n *NsisInstaller) Download(ctx context.Context, _, _, digest string) error {
	if err := n.verify(digest); err == nil {
		return nil
	}
	get := n.Get
	if get == nil {
		get = DefaultHTTPGet
	}
	doc, ok, err := FetchRemoteLatest(ctx, get, "")
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("desktopupdate: no local installer for digest")
	}
	if !strings.EqualFold(doc.SHA256, digest) {
		return fmt.Errorf("desktopupdate: remote digest mismatch")
	}
	store := n.Store
	if store == "" {
		dirs := DefaultDirs()
		if len(dirs) == 0 {
			return fmt.Errorf("desktopupdate: update store missing")
		}
		store = dirs[0]
	}
	getFile := n.GetFile
	if getFile == nil {
		getFile = DefaultHTTPGetFile
	}
	_, err = EnsureLocalInstaller(ctx, store, doc, getFile)
	return err
}

func (n *NsisInstaller) Install(_ context.Context, _, _, digest string) error {
	offer, err := n.offer(digest)
	if err != nil {
		return err
	}
	cmd := exec.Command(offer.Path, SilentUpgradeArgs()...)
	start := n.Start
	if start == nil {
		start = startDetached
	}
	if err := start(cmd); err != nil {
		return fmt.Errorf("desktopupdate: start setup: %w", err)
	}
	return nil
}

func (n *NsisInstaller) Verify(_ context.Context, _ string, digest string) error {
	return n.verify(digest)
}

func (n *NsisInstaller) Rollback(context.Context, string) error { return nil }

func (n *NsisInstaller) verify(digest string) error {
	offer, err := n.offer(digest)
	if err != nil {
		return err
	}
	return VerifyInstaller(offer.Path, offer.Digest)
}

func (n *NsisInstaller) offer(digest string) (Offer, error) {
	if n == nil || n.Feed == nil {
		return Offer{}, fmt.Errorf("desktopupdate: installer feed unavailable")
	}
	offer, ok, err := n.Feed.Locate(digest)
	if err != nil {
		return Offer{}, err
	}
	if !ok {
		return Offer{}, fmt.Errorf("desktopupdate: no local installer for digest")
	}
	return offer, nil
}
