//go:build !windows

package brapp

import "os"

func makeBrowserTestLink(target, link string) error { return os.Symlink(target, link) }
