//go:build !windows

package stdioworker

import "errors"

func makeJunction(link, target string) error {
	return errors.New("junctions are windows-only")
}

func pidsAlive(pids []int) []int { return nil }

func rtAllocProbe(want int64) (committed int64, failed bool, detail string) {
	return 0, false, "non-windows stub"
}
