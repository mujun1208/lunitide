//go:build !windows

package toolruntime

func desktopDirCandidates() []string { return nil }

func pickStartMenuShortcut(string) (string, []string, error) {
	return "", nil, nil
}

func lookupUninstallExecutables(knownLaunchApp) []string { return nil }
