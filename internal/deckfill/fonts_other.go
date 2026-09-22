//go:build !windows

package deckfill

func installedFonts() (map[string]bool, error) {
	return nil, nil
}
