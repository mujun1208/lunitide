//go:build windows

package deckfill

import (
	"strings"

	"golang.org/x/sys/windows/registry"
)

func installedFonts() (map[string]bool, error) {
	names := map[string]bool{}
	var readErr error
	for _, root := range []registry.Key{registry.LOCAL_MACHINE, registry.CURRENT_USER} {
		k, err := registry.OpenKey(root, `SOFTWARE\Microsoft\Windows NT\CurrentVersion\Fonts`, registry.QUERY_VALUE)
		if err != nil {
			readErr = err
			continue
		}
		list, err := k.ReadValueNames(-1)
		k.Close()
		if err != nil {
			readErr = err
			continue
		}
		readErr = nil
		for _, name := range list {
			name = strings.TrimSpace(name)
			if i := strings.Index(name, " ("); i > 0 {
				name = name[:i]
			}
			if i := strings.Index(name, " & "); i > 0 {
				name = name[:i]
			}
			names[strings.ToLower(name)] = true
		}
	}
	if len(names) == 0 && readErr != nil {
		return nil, readErr
	}
	return names, nil
}
