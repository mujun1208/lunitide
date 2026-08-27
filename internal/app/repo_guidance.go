package app

import (
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"
)

const (
	repoGuidanceMaxBytes     = 4096
	agentsMarkdownMaxBytes   = 2048
	repoSkillCatalogMaxItems = 12
)

// workspaceRepoGuidance injects bounded AGENTS.md text and local
// .agents/skills names. It is overlay metadata only: it never replaces
// 月汐 identity.
func (e *Engine) workspaceRepoGuidance() string {
	if e.tools == nil {
		return ""
	}
	root, ok := e.tools.FullAccessRootHint()
	if !ok {
		return ""
	}
	return repoGuidanceInjection(root)
}

func repoGuidanceInjection(root string) string {
	root = filepath.Clean(root)
	if root == "" || root == "." {
		return ""
	}
	agentsRoot, skillsRoot := walkRepoGuidanceRoots(root)
	var b strings.Builder
	b.WriteString("\n\n[仓库约定] 以下来自工作区文件，叠在月汐身份之上，不替换身份，也不引入外部 Codex/云端线程。\n")
	budget := repoGuidanceMaxBytes - b.Len()
	if body := readBoundedAgentsMarkdown(agentsRoot); body != "" {
		line := "AGENTS.md：\n" + body + "\n"
		if len(line) > budget {
			line = truncateUTF8Bytes(line, budget)
		}
		b.WriteString(line)
		budget = repoGuidanceMaxBytes - b.Len()
	}
	if names := listLocalAgentSkills(skillsRoot); len(names) > 0 {
		var skillBlock strings.Builder
		skillBlock.WriteString("本仓库 .agents/skills：")
		skillBlock.WriteString(strings.Join(names, "、"))
		skillBlock.WriteString("。需要时 skill.invoke；不要把仓库约定写成新的系统身份。\n")
		line := skillBlock.String()
		if len(line) > budget {
			line = truncateUTF8Bytes(line, budget)
		}
		if line != "" {
			b.WriteString(line)
		}
	}
	if b.Len() <= len("\n\n[仓库约定] 以下来自工作区文件，叠在月汐身份之上，不替换身份，也不引入外部 Codex/云端线程。\n") {
		return ""
	}
	return b.String()
}

// walkRepoGuidanceRoots looks from the workspace directory up to the
// enclosing git root (max 8 parents) so a nested worktree still finds
// AGENTS.md and .agents/skills. No git root means only the start
// directory is searched — never the user home. It never follows symlinks.
func walkRepoGuidanceRoots(start string) (agentsRoot, skillsRoot string) {
	dir := filepath.Clean(start)
	limit := gitWalkLimit(dir)
	for i := 0; i < limit; i++ {
		info, err := os.Lstat(dir)
		if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			break
		}
		if agentsRoot == "" && readBoundedAgentsMarkdown(dir) != "" {
			agentsRoot = dir
		}
		if skillsRoot == "" && len(listLocalAgentSkills(dir)) > 0 {
			skillsRoot = dir
		}
		if agentsRoot != "" && skillsRoot != "" {
			return agentsRoot, skillsRoot
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return agentsRoot, skillsRoot
}

func gitWalkLimit(start string) int {
	dir := filepath.Clean(start)
	for i := 0; i < 8; i++ {
		if isGitRoot(dir) {
			return i + 1
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return 1
		}
		dir = parent
	}
	return 1
}

func isGitRoot(dir string) bool {
	info, err := os.Lstat(filepath.Join(dir, ".git"))
	if err != nil {
		return false
	}
	return info.IsDir() || info.Mode().IsRegular()
}

func readBoundedAgentsMarkdown(root string) string {
	if root == "" {
		return ""
	}
	path := filepath.Join(root, "AGENTS.md")
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		return ""
	}
	raw, err := os.ReadFile(path)
	if err != nil || len(raw) == 0 {
		return ""
	}
	if len(raw) > agentsMarkdownMaxBytes {
		raw = raw[:agentsMarkdownMaxBytes]
	}
	text := strings.TrimSpace(string(raw))
	if !utf8.ValidString(text) {
		text = strings.ToValidUTF8(text, "")
	}
	return text
}

func listLocalAgentSkills(root string) []string {
	if root == "" {
		return nil
	}
	dir := filepath.Join(root, ".agents", "skills")
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var names []string
	for _, entry := range entries {
		if len(names) >= repoSkillCatalogMaxItems {
			break
		}
		if !entry.IsDir() || strings.HasPrefix(entry.Name(), ".") {
			continue
		}
		skillMD := filepath.Join(dir, entry.Name(), "SKILL.md")
		info, err := os.Lstat(skillMD)
		if err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
			continue
		}
		names = append(names, entry.Name())
	}
	return names
}
