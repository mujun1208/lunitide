package mcp6

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/lunitide/lunitide/internal/domain/m7flow"
	"github.com/lunitide/lunitide/internal/mcp"
)

// Preset is one row of the curated free-official-server catalog exposed via
// mcp6.presets.list. Every entry is a real upstream @modelcontextprotocol
// or community server launched over stdio through a whitelisted runner,
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
	URL             string   `json:"url,omitempty"`
	NeedsCredential bool     `json:"needsCredential,omitempty"`
	CredentialEnvs  []string `json:"credentialEnvs,omitempty"`
	SetupURL        string   `json:"setupUrl,omitempty"`
	ID              string   `json:"id"`
	Name            string   `json:"name"`
	Description     string   `json:"description"`
	Transport       string   `json:"transport"`
	Command         string   `json:"command"`
	Args            []string `json:"args"`
	NeedsArgs       bool     `json:"needsArgs"`
	ArgPlaceholder  string   `json:"argPlaceholder,omitempty"`
	ArgHint         string   `json:"argHint,omitempty"`
	ArgDefault      string   `json:"argDefault,omitempty"`
	Category        string   `json:"category"`
}

// extraPresetPackages is the fail-closed community allowlist used by
// tests and ValidatePresetCatalog. Official @modelcontextprotocol/*
// packages are admitted by prefix; archived 2025 IDs (git/github/
// puppeteer/sqlite) stay off the catalog even if the npm name still
// resolves.
var extraPresetPackages = map[string]bool{
	"mcp-server-fetch":                  true,
	"mcp-server-time":                   true,
	"markitdown-mcp":                    true,
	"@playwright/mcp":                   true,
	"@upstash/context7-mcp":             true,
	"chrome-devtools-mcp":               true,
	"@antv/mcp-server-chart":            true,
	"excel-mcp-server":                  true,
	"office-word-mcp-server":            true,
	"office-ppt-mcp-server":             true,
	"pdfnative-mcp":                     true,
	"mcp-server-calculator":             true,
	"duckduckgo-mcp-server":             true,
	"@nickclyde/duckduckgo-mcp-server":  true,
	"youtube-transcript-mcp":            true,
	"@sinco-lab/mcp-youtube-transcript": true,
}

// PresetPackageAllowed reports whether the executable package is curated.
func PresetPackageAllowed(spec string) bool {
	if strings.HasPrefix(spec, "@modelcontextprotocol/") {
		return true
	}
	return extraPresetPackages[spec]
}

// PresetLaunchPackage is the npm/PyPI spec a stdio preset actually launches.
// npx uses args[1] after -y; uvx --from <pkg> uses the following element.
func PresetLaunchPackage(p Preset) string {
	if p.Command == "npx" && len(p.Args) >= 2 && p.Args[0] == "-y" {
		return p.Args[1]
	}
	if p.Command == "uvx" {
		for i, a := range p.Args {
			if a == "--from" && i+1 < len(p.Args) {
				return p.Args[i+1]
			}
		}
		if len(p.Args) > 0 {
			return p.Args[0]
		}
	}
	if len(p.Args) > 0 {
		return p.Args[0]
	}
	return ""
}

// presets is the one-click free catalog. Every row installs without a
// token, API key, or typed path. Filesystem still uses NeedsArgs so the
// sandbox directory can be substituted, but the client never asks for it.
// Paid, OAuth, and connection-string servers stay off the market.
// Order is display order.
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
		Command:     "uvx",
		Args:        []string{"mcp-server-fetch"},
		Category:    "网络",
	},
	{
		ID:          "memory",
		Name:        "Memory",
		Description: "MCP Memory 是该服务器自己的知识图谱，不是月汐记忆中心。产品记忆需用户确认后才写入。",
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
		Description: "查询当前时间与时区转换，适合日程和日志场景",
		Transport:   "stdio",
		Command:     "uvx",
		Args:        []string{"mcp-server-time"},
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
	{ID: "chrome-devtools", Name: "Chrome DevTools", Description: "官方 Chrome DevTools MCP。人装才生效，不是默认电脑控制，月伴不会自动安装。默认网页自动化请用 Playwright。", Transport: "stdio", Command: "npx", Args: []string{"-y", "chrome-devtools-mcp"}, Category: "浏览器"},
	{ID: "antv-chart", Name: "AntV Chart", Description: "按描述生成图表，默认走公开图表服务", Transport: "stdio", Command: "npx", Args: []string{"-y", "@antv/mcp-server-chart"}, Category: "开发"},
	{ID: "excel-mcp", Name: "Excel MCP", Description: "本机表格读写（无需安装 Excel）。新建工作簿仍优先用月汐 excel.gen / office.generate", Transport: "stdio", Command: "uvx", Args: []string{"excel-mcp-server", "stdio"}, Category: "办公", SetupURL: "https://github.com/haris-musa/excel-mcp-server"},
	{ID: "word-mcp", Name: "Word MCP", Description: "深度编辑已有 Word：段落、表格、样式。新建文档仍优先用月汐 docx.gen / office.generate", Transport: "stdio", Command: "uvx", Args: []string{"--from", "office-word-mcp-server", "word_mcp_server"}, Category: "办公", SetupURL: "https://github.com/GongRzhe/Office-Word-MCP-Server"},
	{ID: "ppt-mcp", Name: "PPT MCP", Description: "深度编辑已有演示：增删页与布局。新建演示仍优先用月汐 pptx.gen / office.generate，不走 Office COM", Transport: "stdio", Command: "uvx", Args: []string{"office-ppt-mcp-server"}, Category: "办公", SetupURL: "https://pypi.org/project/office-ppt-mcp-server/"},
	{ID: "pdf-mcp", Name: "PDF MCP", Description: "本机生成/批注/书签等 PDF 操作。简单文本 PDF 仍优先用月汐 pdf.gen / office.generate", Transport: "stdio", Command: "npx", Args: []string{"-y", "pdfnative-mcp"}, Category: "办公", SetupURL: "https://github.com/Nizoka/pdfnative-mcp"},
	{ID: "markitdown", Name: "MarkItDown", Description: "Office/PDF 转 Markdown，本机转换；需要 Python 3.10+，首次启动会准备依赖", Transport: "stdio", Command: "uvx", Args: []string{"markitdown-mcp"}, Category: "办公", SetupURL: "https://github.com/microsoft/markitdown/tree/main/packages/markitdown-mcp"},
	{ID: "calculator", Name: "Calculator", Description: "精确算术，无网络、无密钥", Transport: "stdio", Command: "uvx", Args: []string{"mcp-server-calculator"}, Category: "效率"},
	{ID: "duckduckgo", Name: "DuckDuckGo", Description: "无密钥网页搜索；调用时需要网络可访问 DuckDuckGo。与 Hermes / OpenClaw 引导安装的搜索 MCP 同类，无需密钥。", Transport: "stdio", Command: "uvx", Args: []string{"duckduckgo-mcp-server"}, Category: "网络", SetupURL: "https://pypi.org/project/duckduckgo-mcp-server/"},
	{ID: "youtube-transcript", Name: "YouTube Transcript", Description: "拉取公开字幕，无密钥。与 Hermes / OpenClaw 引导安装的字幕 MCP 同类。", Transport: "stdio", Command: "npx", Args: []string{"-y", "@sinco-lab/mcp-youtube-transcript"}, Category: "内容", SetupURL: "https://www.npmjs.com/package/@sinco-lab/mcp-youtube-transcript"},
}

// Presets returns a copy of the preset catalog in display order.
func Presets() []Preset {
	out := make([]Preset, len(presets))
	for i, p := range presets {
		p.Args = append([]string{}, p.Args...)
		p.CredentialEnvs = append([]string(nil), p.CredentialEnvs...)
		out[i] = p
	}
	return out
}

// PresetByID returns the catalog entry with the given id.
func PresetByID(id string) (Preset, bool) {
	for _, p := range presets {
		if p.ID == id {
			p.Args = append([]string{}, p.Args...)
			p.CredentialEnvs = append([]string(nil), p.CredentialEnvs...)
			return p, true
		}
	}
	return Preset{}, false
}

// ResolveArgs substitutes the placeholder element with the user-supplied
// value. Input shaping stays the caller's concern; admission is re-checked
// by the registry whitelist, so hostile input can never slip through here.
func (p Preset) ResolveArgs(input string) []string {
	out := append([]string{}, p.Args...)
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
	if p.Transport == "https" {
		if p.Command != "" || len(p.Args) != 0 || p.NeedsArgs || len(p.CredentialEnvs) != 0 {
			return fmt.Errorf("mcp6: preset %q mixes remote and local config", p.ID)
		}
		return mcp.ValidateBaseURL(p.URL)
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
