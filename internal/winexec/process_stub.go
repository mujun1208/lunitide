//go:build !windows

package winexec

// LookupProcessImages is Windows-only.
func LookupProcessImages([]string) []string { return nil }
