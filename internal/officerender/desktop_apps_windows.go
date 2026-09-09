//go:build windows

package officerender

import (
	"strings"

	"golang.org/x/sys/windows/registry"
)

func ProbeDesktopApplications() []DesktopApplication {
	candidates := []DesktopApplication{
		{ID: "winword", Label: "Microsoft Word"},
		{ID: "powerpnt", Label: "Microsoft PowerPoint"},
		{ID: "excel", Label: "Microsoft Excel"},
		{ID: "wps", Label: "WPS Writer"},
		{ID: "wpp", Label: "WPS Presentation"},
		{ID: "et", Label: "WPS Spreadsheets"},
	}
	result := make([]DesktopApplication, 0, len(candidates))
	for _, candidate := range candidates {
		if registeredDesktopExecutable(candidate.ID + ".exe") {
			result = append(result, candidate)
		}
	}
	return result
}

func registeredDesktopExecutable(name string) bool {
	path := `Software\Microsoft\Windows\CurrentVersion\App Paths\` + name
	for _, root := range []registry.Key{registry.CURRENT_USER, registry.LOCAL_MACHINE} {
		for _, view := range []uint32{0, registry.WOW64_64KEY, registry.WOW64_32KEY} {
			key, err := registry.OpenKey(root, path, registry.QUERY_VALUE|view)
			if err != nil {
				continue
			}
			value, _, valueErr := key.GetStringValue("")
			_ = key.Close()
			if valueErr == nil && strings.TrimSpace(value) != "" {
				return true
			}
		}
	}
	return false
}
