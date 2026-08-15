//go:build !windows

package stdiopoc

import "errors"

// pidAlive / makeJunction are Windows-only helpers; the harness refuses to
// run the POC elsewhere, so these exist only to keep the package building.
func pidAlive(pid int) bool { return false }

func makeJunction(link, target string) error {
	return errors.New("junctions are windows-only")
}
