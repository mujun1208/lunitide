package app

import (
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"
)

const (
	repoGuidanceMaxBytes     = 12288
	agentsMarkdownMaxBytes   = 4096
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
	header := "\n\n[仓库约定] 以下来自工作区文件，叠在月汐身份之上，不替换身份，也不引入外部 Codex/云端线程。近处的 AGENTS.md 覆盖远处的同名约定。\n"
	var b strings.Builder
	b.WriteString(header)
	budget := repoGuidanceMaxBytes - b.Len()
	if chain := readAgentsMarkdownChain(root); chain != "" {
		if len(chain) > budget {
			chain = truncateUTF8Bytes(chain, budget)
		}
		b.WriteString(chain)
		budget = repoGuidanceMaxBytes - b.Len()
	}
	_, skillsRoot := walkRepoGuidanceRoots(root)
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
	if b.Len() <= len(header) {
		return ""
	}
	return b.String()
}

// readAgentsMarkdownChain walks git root → cwd and concatenates every
// AGENTS.md (nearer last). Over budget, distant files drop first.
// No git root means only the start directory — never ~ or the user home.
func readAgentsMarkdownChain(start string) string {
	dirs := agentsChainDirs(start)
	if len(dirs) == 0 {
		return ""
	}
	gitRoot := dirs[0]
	var sections []string
	for _, dir := range dirs {
		body := readBoundedAgentsMarkdown(dir)
		if body == "" {
			continue
		}
		label := agentsChainLabel(gitRoot, dir)
		sections = append(sections, "AGENTS.md（"+label+"）：\n"+body+"\n")
	}
	for len(sections) > 0 {
		joined := strings.Join(sections, "")
		if len(joined) <= repoGuidanceMaxBytes {
			return joined
		}
		if len(sections) == 1 {
			return truncateUTF8Bytes(sections[0], repoGuidanceMaxBytes)
		}
		sections = sections[1:]
	}
	return ""
}

func agentsChainLabel(root, dir string) string {
	rel, err := filepath.Rel(root, dir)
	if err != nil || rel == "." || rel == "" {
		return "仓库根"
	}
	if strings.HasPrefix(rel, "..") {
		return filepath.Base(dir)
	}
	return filepath.ToSlash(rel)
}

func agentsChainDirs(start string) []string {
	start = filepath.Clean(start)
	root := gitRootOrStart(start)
	rel, err := filepath.Rel(root, start)
	if err != nil || strings.HasPrefix(rel, "..") {
		return []string{start}
	}
	dirs := []string{root}
	acc := root
	for _, part := range strings.Split(rel, string(filepath.Separator)) {
		if part == "" || part == "." {
			continue
		}
		acc = filepath.Join(acc, part)
		dirs = append(dirs, acc)
	}
	return dirs
}

func gitRootOrStart(start string) string {
	dir := filepath.Clean(start)
	for i := 0; i < 8; i++ {
		if isGitRoot(dir) {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return start
		}
		dir = parent
	}
	return start
}

// walkRepoGuidanceRoots looks from the workspace directory up to the
// enclosing git root (max 8 parents) so a nested worktree still finds
// .agents/skills. No git root means only the start directory is searched.
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
