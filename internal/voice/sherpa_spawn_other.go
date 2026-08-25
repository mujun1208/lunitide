//go:build !windows

package voice

// The runtime bundle in the catalogue is a Windows build, so there is nothing
// to start anywhere else. This exists so `go build ./...` works for a
// developer on a Mac rather than failing in a package they are not touching.
func spawnServer(string, []string, int) (*sherpaServer, error) {
	return nil, ErrUnsupported
}

func (s *sherpaServer) stop() {}

func ensureFirewallRule(string) {}
