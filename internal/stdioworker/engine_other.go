//go:build !windows

package stdioworker

import (
	"context"
	"errors"
)

// The 5B production engine is Windows-first (Job Object quotas are part of
// the enforced contract). Non-Windows builds compile but refuse to spawn:
// the runtime gate stays closed everywhere else.

var errNoEngine = errors.New("stdioworker: spawn engine requires windows (Job Object envelope)")

type engineProc struct{}

func engineSpawn(cmd string, args []string, dir string, env []string, q Quotas) (*engineProc, error) {
	return nil, errNoEngine
}

func (p *engineProc) kill() error { return nil }

func (p *engineProc) close() {}

func (p *engineProc) wait(ctx context.Context) (int, error) { return -1, errNoEngine }
