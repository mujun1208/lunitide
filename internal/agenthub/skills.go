package agenthub

import (
	"os"
	"path/filepath"
	"strings"
)

func filterKimiSkillRoots(candidates []string) []string {
	var out []string
	for _, root := range candidates {
		root = strings.TrimSpace(root)
		if root == "" {
			continue
		}
		if _, err := os.Stat(filepath.Join(root, "kimi-slides", "SKILL.md")); err == nil {
			out = append(out, root)
		}
	}
	return out
}

func kimiSkillDirs() []string {
	return filterKimiSkillRoots([]string{
		filepath.Join(os.Getenv("APPDATA"), "kimi-desktop", "daimon-share", "daimon", "skills"),
		filepath.Join(os.Getenv("USERPROFILE"), ".kimi-code", "skills"),
		os.Getenv("KIMI_SKILLS_DIR"),
	})
}
