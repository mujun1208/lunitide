package deckfill

import (
	"regexp"
	"sort"
	"strings"
)

var typefaceAttr = regexp.MustCompile(`typeface="([^"]+)"`)

func fontNotes(files map[string][]byte) []string {
	seen := map[string]bool{}
	var faces []string
	for name, body := range files {
		if !strings.Contains(name, "ppt/slides/slide") || strings.Contains(name, "_rels") {
			continue
		}
		for _, match := range typefaceAttr.FindAllSubmatch(body, -1) {
			face := string(match[1])
			if face == "" || strings.HasPrefix(face, "+") || seen[face] {
				continue
			}
			seen[face] = true
			faces = append(faces, face)
		}
	}
	installed, err := installedFonts()
	if err != nil || installed == nil {
		if len(faces) > 0 {
			return []string{"未能读取本机字体"}
		}
		return nil
	}
	var missing []string
	for _, face := range faces {
		if !fontInstalled(installed, face) {
			missing = append(missing, face)
		}
	}
	if len(missing) == 0 {
		return nil
	}
	sort.Strings(missing)
	return []string{"缺少字体：" + strings.Join(missing, "、")}
}

func fontInstalled(installed map[string]bool, face string) bool {
	face = strings.ToLower(strings.TrimSpace(face))
	if installed[face] {
		return true
	}
	for name := range installed {
		if strings.Contains(name, face) || strings.Contains(face, name) {
			return true
		}
	}
	return false
}
