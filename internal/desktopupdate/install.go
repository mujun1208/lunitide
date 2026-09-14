package desktopupdate

import (
	"context"
	"fmt"
	"os/exec"
)

// SilentUpgradeArgs is the NSIS in-place upgrade switch. The installer already
// upgrades the registered location and refuses a path change.
func SilentUpgradeArgs() []string { return []string{"/S"} }

// NsisInstaller applies a verified local Setup. Missing files fail closed so a
// feed-adopted digest cannot no-op.
type NsisInstaller struct {
	Feed  Feed
	Start func(*exec.Cmd) error
}

func NewNsisInstaller(feed Feed) *NsisInstaller {
	return &NsisInstaller{Feed: feed, Start: startDetached}
}

func (n *NsisInstaller) Download(_ context.Context, _, _, digest string) error {
	return n.verify(digest)
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
