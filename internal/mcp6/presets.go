package mcp6

import (
	"fmt"
	"os"
	"path/filepath"
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
	ArgDefault     string   `json:"argDefault,omitempty"`
	Category       string   `json:"category"`
}

// presets is the curated catalog. Only currently maintained npx packages
// (official MCP reference servers that still live on
// modelcontextprotocol/servers, plus Playwright and Context7). Archived
// 2025 packages (GitHub / Puppeteer / SQLite / Git) are not listed —
// git goes through command.run; browsers through Playwright; GitHub
// public data through web.search. Order is the display order.
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
		Description:    "读写月汐为本机准备的文件目录（自动安装，无需选择路径）",
		Transport:      "stdio",
		Command:        "npx",
		Args:           []string{"-y", "@modelcontextprotocol/server-filesystem", "{{dir}}"},
		NeedsArgs:      true,
		ArgPlaceholder: "{{dir}}",
		ArgHint:        "月汐会使用本机数据目录，无需手动填写",
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
		ID:          "playwright",
		Name:        "Playwright",
		Description: "微软开源浏览器自动化（免费直连，首次会拉取 Chromium）",
		Transport:   "stdio",
		Command:     "npx",
		Args:        []string{"-y", "@playwright/mcp"},
		Category:    "浏览器",
	},
	{
		ID:          "time",
		Name:        "Time",
		Description: "查询当前时间、时区转换与工作日计算，适合日程和日志场景",
		Transport:   "stdio",
		Command:     "npx",
		Args:        []string{"-y", "@modelcontextprotocol/server-time"},
		Category:    "效率",
	},
	{
		ID:          "context7",
		Name:        "Context7",
		Description: "按库名拉取最新官方文档片段，给模型准确的 API 与示例",
		Transport:   "stdio",
		Command:     "npx",
		Args:        []string{"-y", "@upstash/context7-mcp"},
		Category:    "开发",
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

// PrepareSandbox returns a metacharacter-free absolute path under the
// Lunitide LocalAppData root so one-click MCP install never asks the user
// for a directory. The directory is created if missing.
func PrepareSandbox(id string) string {
	base := os.Getenv("LOCALAPPDATA")
	if base == "" {
		home, err := os.UserHomeDir()
		if err != nil || home == "" {
			home = "C:/Users/Public"
		}
		base = filepath.Join(home, "AppData", "Local")
	}
	dir := filepath.Join(base, "Lunitide", "mcp", id)
	_ = os.MkdirAll(dir, 0o755)
	if id == "sqlite" {
		return filepath.ToSlash(filepath.Join(dir, "lunitide.db"))
	}
	return filepath.ToSlash(dir)
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
