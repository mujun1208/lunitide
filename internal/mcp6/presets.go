package mcp6

import (
	"fmt"
	"strings"

	"github.com/lunitide/lunitide/internal/domain/m7flow"
)

// Preset is one row of the curated free-official-server catalog exposed via
// mcp6.presets.list. Every entry is a real upstream @modelcontextprotocol
// reference server launched over stdio through the npx whitelisted runner,
// so one click maps directly onto the frozen M6-MCP-004 admission shape.
//
// Args templates may carry at most one placeholder element (ArgPlaceholder,
// e.g. "{{dir}}") when NeedsArgs is set; the client collects the value and
// substitutes it verbatim before calling mcp.add / mcp6.register. The
// whitelist stays the authority: ValidatePresetCatalog proves both the
// shipped template and a substituted sample pass m7flow admission, and any
// metacharacter-laden user input is still refused downstream (fail-closed,
// never relaxed here).
type Preset struct {
	ID             string   `json:"id"`
	Name           string   `json:"name"`
	Description    string   `json:"description"`
	Transport      string   `json:"transport"`
	Command        string   `json:"command"`
	Args           []string `json:"args"`
	NeedsArgs      bool     `json:"needsArgs"`
	ArgPlaceholder string   `json:"argPlaceholder,omitempty"`
	ArgHint        string   `json:"argHint,omitempty"`
	Category       string   `json:"category"`
}

// presets is the frozen catalog (task c3-mcp): free official reference
// servers only. Order is the display order.
var presets = []Preset{
	{
		ID:          "everything",
		Name:        "Everything",
		Description: "官方测试服务器：覆盖 MCP 全部协议特性，适合连通性验证与演示",
		Transport:   "stdio",
		Command:     "npx",
		Args:        []string{"-y", "@modelcontextprotocol/server-everything"},
		Category:    "测试",
	},
	{
		ID:             "filesystem",
		Name:           "Filesystem",
		Description:    "读写指定目录内的文件与目录树（需提供挂载目录）",
		Transport:      "stdio",
		Command:        "npx",
		Args:           []string{"-y", "@modelcontextprotocol/server-filesystem", "{{dir}}"},
		NeedsArgs:      true,
		ArgPlaceholder: "{{dir}}",
		ArgHint:        "要挂载的目录绝对路径，例如 E:/projects/myrepo",
		Category:       "文件",
	},
	{
		ID:          "fetch",
		Name:        "Fetch",
		Description: "抓取网页并转为 Markdown，供模型高效阅读与检索",
		Transport:   "stdio",
		Command:     "npx",
		Args:        []string{"-y", "@modelcontextprotocol/server-fetch"},
		Category:    "网络",
	},
	{
		ID:          "memory",
		Name:        "Memory",
		Description: "基于知识图谱的跨会话记忆：实体、关系与观察的增删查",
		Transport:   "stdio",
		Command:     "npx",
		Args:        []string{"-y", "@modelcontextprotocol/server-memory"},
		Category:    "记忆",
	},
	{
		ID:          "sequentialthinking",
		Name:        "Sequential Thinking",
		Description: "结构化多步推理：拆解问题、修正思路并保留思维轨迹",
		Transport:   "stdio",
		Command:     "npx",
		Args:        []string{"-y", "@modelcontextprotocol/server-sequential-thinking"},
		Category:    "推理",
	},
	{
		ID:             "git",
		Name:           "Git",
		Description:    "查看仓库状态、diff、日志与提交等本地 Git 操作（需提供仓库路径）",
		Transport:      "stdio",
		Command:        "npx",
		Args:           []string{"-y", "@modelcontextprotocol/server-git", "--repository", "{{repo}}"},
		NeedsArgs:      true,
		ArgPlaceholder: "{{repo}}",
		ArgHint:        "本地 Git 仓库路径，例如 E:/projects/lunitide",
		Category:       "版本控制",
	},
	{
		ID:          "github",
		Name:        "GitHub",
		Description: "公共仓库的 issue、PR、文件与搜索；无 Token 也可试用公共数据",
		Transport:   "stdio",
		Command:     "npx",
		Args:        []string{"-y", "@modelcontextprotocol/server-github"},
		Category:    "开发",
	},
	{
		ID:          "puppeteer",
		Name:        "Puppeteer",
		Description: "驱动无头浏览器：导航、截图、点击与表单自动化",
		Transport:   "stdio",
		Command:     "npx",
		Args:        []string{"-y", "@modelcontextprotocol/server-puppeteer"},
		Category:    "浏览器",
	},
	{
		ID:          "playwright",
		Name:        "Playwright",
		Description: "微软开源浏览器自动化（免费直连，首次会拉取 Chromium）",
		Transport:   "stdio",
		Command:     "npx",
		Args:        []string{"-y", "@playwright/mcp"},
		Category:    "浏览器",
	},
	{
		ID:             "sqlite",
		Name:           "SQLite",
		Description:    "查询与建表本地 SQLite 数据库（需提供数据库文件路径）",
		Transport:      "stdio",
		Command:        "npx",
		Args:           []string{"-y", "@modelcontextprotocol/server-sqlite", "--db-path", "{{db}}"},
		NeedsArgs:      true,
		ArgPlaceholder: "{{db}}",
		ArgHint:        "数据库文件路径，例如 E:/data/app.db（不存在会自动创建）",
		Category:       "数据库",
	},
}

// Presets returns a copy of the preset catalog in display order.
func Presets() []Preset {
	out := make([]Preset, len(presets))
	for i, p := range presets {
		p.Args = append([]string(nil), p.Args...)
		out[i] = p
	}
	return out
}

// PresetByID returns the catalog entry with the given id.
func PresetByID(id string) (Preset, bool) {
	for _, p := range presets {
		if p.ID == id {
			p.Args = append([]string(nil), p.Args...)
			return p, true
		}
	}
	return Preset{}, false
}

// ResolveArgs substitutes the placeholder element with the user-supplied
// value. Input shaping stays the caller's concern; admission is re-checked
// by the registry whitelist, so hostile input can never slip through here.
func (p Preset) ResolveArgs(input string) []string {
	out := append([]string(nil), p.Args...)
	for i, a := range out {
		if p.ArgPlaceholder != "" && a == p.ArgPlaceholder {
			out[i] = input
		}
	}
	return out
}

// validatePreset checks one catalog row against the frozen stdio admission
// rules (m7flow whitelist, fail-closed): stdio transport, whitelisted
// launcher, 1..16 metacharacter-free args, and the placeholder contract
// (exactly one placeholder element iff NeedsArgs).
func validatePreset(p Preset) error {
	if p.ID == "" || p.Name == "" || p.Description == "" || p.Category == "" {
		return fmt.Errorf("mcp6: preset %q missing id/name/description/category", p.ID)
	}
	if p.Transport != "stdio" {
		return fmt.Errorf("mcp6: preset %q transport must be stdio, got %q", p.ID, p.Transport)
	}
	if !m7flow.McpStdioCommandAllowed(p.Command) {
		return fmt.Errorf("mcp6: preset %q command %q not whitelisted", p.ID, p.Command)
	}
	if len(p.Args) == 0 || len(p.Args) > 16 {
		return fmt.Errorf("mcp6: preset %q needs 1-16 args, got %d", p.ID, len(p.Args))
	}
	if !m7flow.McpArgsSafe(p.Args) {
		return fmt.Errorf("mcp6: preset %q args contain metacharacters", p.ID)
	}
	placeholders := 0
	for _, a := range p.Args {
		if p.ArgPlaceholder != "" && a == p.ArgPlaceholder {
			placeholders++
		}
	}
	if p.NeedsArgs && (p.ArgPlaceholder == "" || placeholders != 1 || p.ArgHint == "") {
		return fmt.Errorf("mcp6: preset %q needsArgs requires exactly one placeholder element and a hint", p.ID)
	}
	if !p.NeedsArgs && (p.ArgPlaceholder != "" || placeholders != 0 || strings.Contains(strings.Join(p.Args, " "), "{{")) {
		return fmt.Errorf("mcp6: preset %q must not carry a placeholder", p.ID)
	}
	return nil
}

// ValidatePresetCatalog validates every shipped row plus a benign
// substitution, proving the whole catalog survives the unchanged M6-MCP-004
// admission gate as-shipped and after placeholder resolution.
func ValidatePresetCatalog() error {
	seen := make(map[string]bool, len(presets))
	for _, p := range presets {
		if seen[p.ID] {
			return fmt.Errorf("mcp6: duplicate preset id %q", p.ID)
		}
		seen[p.ID] = true
		if err := validatePreset(p); err != nil {
			return err
		}
		if p.NeedsArgs {
			resolved := p.ResolveArgs("C:/Users/demo/projects/sample")
			if !m7flow.McpArgsSafe(resolved) || !m7flow.McpStdioCommandAllowed(p.Command) {
				return fmt.Errorf("mcp6: preset %q resolved args fail admission", p.ID)
			}
		}
	}
	return nil
}
