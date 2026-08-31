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

// extraPresetPackages is the fail-closed community allowlist used by
// tests and ValidatePresetCatalog. Official @modelcontextprotocol/*
// packages are admitted by prefix; archived 2025 IDs (git/github/
// puppeteer/sqlite) stay off the catalog even if the npm name still
// resolves.
var extraPresetPackages = map[string]bool{
	"@playwright/mcp":                         true,
	"@upstash/context7-mcp":                   true,
	"chrome-devtools-mcp":                     true,
	"@agent360/browser-mcp":                   true,
	"@antv/mcp-server-chart":                  true,
	"@amap/amap-maps-mcp-server":              true,
	"tavily-mcp":                              true,
	"firecrawl-mcp":                           true,
	"@notionhq/notion-mcp-server":             true,
	"excel-mcp-server":                        true,
	"@larksuiteoapi/lark-mcp":                 true,
	"@huggingface/mcp-server":                 true,
	"@microsoft/markitdown-mcp":               true,
	"@neondatabase/mcp-server-neon":           true,
	"@supabase/mcp-server-supabase":           true,
	"@qdrant/mcp-server":                      true,
	"@elastic/mcp-server-elasticsearch":       true,
	"mcp-server-calculator":                   true,
	"duckduckgo-mcp-server":                   true,
	"youtube-transcript-mcp":                  true,
	"markdownify-mcp":                         true,
	"linear-mcp-server":                       true,
	"mcp-mongo-server":                        true,
}

// PresetPackageAllowed reports whether args[1] is a curated npx spec.
func PresetPackageAllowed(spec string) bool {
	if strings.HasPrefix(spec, "@modelcontextprotocol/") {
		return true
	}
	return extraPresetPackages[spec]
}

// presets is the curated catalog. Official reference servers plus a
// reviewed community shelf (LobeHub / awesome-mcp). Archived 2025
// packages (GitHub / Puppeteer / SQLite / Git) are not listed.
// Keys stay local via NeedsArgs placeholders. Order is display order.
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
	{ID: "postgres", Name: "Postgres", Description: "只连你填的 Postgres URL，密钥留在本机", Transport: "stdio", Command: "npx", Args: []string{"-y", "@modelcontextprotocol/server-postgres", "{{url}}"}, NeedsArgs: true, ArgPlaceholder: "{{url}}", ArgHint: "本机 Postgres 连接串，不会上传", Category: "数据"},
	{ID: "redis", Name: "Redis", Description: "只连你填的 Redis URL，密钥留在本机", Transport: "stdio", Command: "npx", Args: []string{"-y", "@modelcontextprotocol/server-redis", "{{url}}"}, NeedsArgs: true, ArgPlaceholder: "{{url}}", ArgHint: "本机 Redis 连接串，不会上传", Category: "数据"},
	{ID: "google-maps", Name: "Google Maps", Description: "地理编码与地点查询，API Key 本地填写", Transport: "stdio", Command: "npx", Args: []string{"-y", "@modelcontextprotocol/server-google-maps", "{{key}}"}, NeedsArgs: true, ArgPlaceholder: "{{key}}", ArgHint: "Google Maps API Key，只存在本机", Category: "网络"},
	{ID: "brave-search", Name: "Brave Search", Description: "Brave 搜索，API Key 本地填写", Transport: "stdio", Command: "npx", Args: []string{"-y", "@modelcontextprotocol/server-brave-search", "{{key}}"}, NeedsArgs: true, ArgPlaceholder: "{{key}}", ArgHint: "Brave Search API Key，只存在本机", Category: "网络"},
	{ID: "gitlab", Name: "GitLab", Description: "本机 GitLab 实例，URL 本地填写。不是已归档的 GitHub MCP", Transport: "stdio", Command: "npx", Args: []string{"-y", "@modelcontextprotocol/server-gitlab", "{{url}}"}, NeedsArgs: true, ArgPlaceholder: "{{url}}", ArgHint: "GitLab URL 或 token 参数，只存在本机", Category: "开发"},
	{ID: "sentry", Name: "Sentry", Description: "Sentry 问题查询，密钥本地填写", Transport: "stdio", Command: "npx", Args: []string{"-y", "@modelcontextprotocol/server-sentry", "{{url}}"}, NeedsArgs: true, ArgPlaceholder: "{{url}}", ArgHint: "Sentry 组织/DSN 参数，只存在本机", Category: "开发"},
	{ID: "gdrive", Name: "Google Drive", Description: "本机授权后的云盘访问，凭证目录由月汐沙箱提供", Transport: "stdio", Command: "npx", Args: []string{"-y", "@modelcontextprotocol/server-gdrive", "{{dir}}"}, NeedsArgs: true, ArgPlaceholder: "{{dir}}", ArgHint: "月汐会使用本机数据目录存放凭证", ArgDefault: "", Category: "文件"},
	{ID: "everart", Name: "EverArt", Description: "图像生成 MCP，API Key 本地填写", Transport: "stdio", Command: "npx", Args: []string{"-y", "@modelcontextprotocol/server-everart", "{{key}}"}, NeedsArgs: true, ArgPlaceholder: "{{key}}", ArgHint: "EverArt API Key，只存在本机", Category: "内容"},
	{ID: "aws-kb", Name: "AWS KB", Description: "AWS Knowledge Base 检索，凭证本地填写", Transport: "stdio", Command: "npx", Args: []string{"-y", "@modelcontextprotocol/server-aws-kb-retrieval", "{{url}}"}, NeedsArgs: true, ArgPlaceholder: "{{url}}", ArgHint: "Knowledge Base 标识，只存在本机", Category: "数据"},
	{ID: "chrome-devtools", Name: "Chrome DevTools", Description: "官方 Chrome DevTools MCP。人装才生效，不是默认电脑控制，月伴不会自动安装。默认网页自动化请用 Playwright。", Transport: "stdio", Command: "npx", Args: []string{"-y", "chrome-devtools-mcp"}, Category: "浏览器"},
	{ID: "browsermcp", Name: "Browser MCP", Description: "使用已登录的本机 Chrome（需扩展）。人装才生效，不是默认电脑控制，也不是引擎劫持用户 Chrome。默认网页自动化请用 Playwright。", Transport: "stdio", Command: "npx", Args: []string{"-y", "@agent360/browser-mcp"}, Category: "浏览器"},
	{ID: "antv-chart", Name: "AntV Chart", Description: "按描述生成图表，默认走公开图表服务", Transport: "stdio", Command: "npx", Args: []string{"-y", "@antv/mcp-server-chart"}, Category: "开发"},
	{ID: "amap", Name: "高德地图", Description: "地理编码与路线，Key 本地填写", Transport: "stdio", Command: "npx", Args: []string{"-y", "@amap/amap-maps-mcp-server", "{{key}}"}, NeedsArgs: true, ArgPlaceholder: "{{key}}", ArgHint: "高德 Web 服务 Key，只存在本机", Category: "网络"},
	{ID: "tavily", Name: "Tavily", Description: "检索增强搜索，API Key 本地填写", Transport: "stdio", Command: "npx", Args: []string{"-y", "tavily-mcp", "{{key}}"}, NeedsArgs: true, ArgPlaceholder: "{{key}}", ArgHint: "Tavily API Key，只存在本机", Category: "网络"},
	{ID: "firecrawl", Name: "Firecrawl", Description: "网页抓取转 Markdown，API Key 本地填写", Transport: "stdio", Command: "npx", Args: []string{"-y", "firecrawl-mcp", "{{key}}"}, NeedsArgs: true, ArgPlaceholder: "{{key}}", ArgHint: "Firecrawl API Key，只存在本机", Category: "网络"},
	{ID: "notion", Name: "Notion", Description: "Notion 工作区，Token 本地填写", Transport: "stdio", Command: "npx", Args: []string{"-y", "@notionhq/notion-mcp-server", "{{key}}"}, NeedsArgs: true, ArgPlaceholder: "{{key}}", ArgHint: "Notion integration token，只存在本机", Category: "效率"},
	{ID: "excel-mcp", Name: "Excel MCP", Description: "本机表格读写，不是已归档的 SQLite MCP", Transport: "stdio", Command: "npx", Args: []string{"-y", "excel-mcp-server"}, Category: "数据"},
	{ID: "lark", Name: "飞书", Description: "飞书开放平台，Token 本地填写，不上云", Transport: "stdio", Command: "npx", Args: []string{"-y", "@larksuiteoapi/lark-mcp", "{{token}}"}, NeedsArgs: true, ArgPlaceholder: "{{token}}", ArgHint: "飞书应用 token，只存在本机", Category: "效率"},
	{ID: "huggingface", Name: "Hugging Face", Description: "HF 模型与数据集查询，Token 本地填写", Transport: "stdio", Command: "npx", Args: []string{"-y", "@huggingface/mcp-server", "{{token}}"}, NeedsArgs: true, ArgPlaceholder: "{{token}}", ArgHint: "Hugging Face token，只存在本机", Category: "开发"},
	{ID: "markitdown", Name: "MarkItDown", Description: "Office/PDF 转 Markdown，本机转换", Transport: "stdio", Command: "npx", Args: []string{"-y", "@microsoft/markitdown-mcp"}, Category: "效率"},
	{ID: "neon", Name: "Neon", Description: "Neon Postgres，API Key 本地填写", Transport: "stdio", Command: "npx", Args: []string{"-y", "@neondatabase/mcp-server-neon", "{{key}}"}, NeedsArgs: true, ArgPlaceholder: "{{key}}", ArgHint: "Neon API Key，只存在本机", Category: "数据"},
	{ID: "supabase", Name: "Supabase", Description: "Supabase 项目，URL 本地填写", Transport: "stdio", Command: "npx", Args: []string{"-y", "@supabase/mcp-server-supabase", "{{url}}"}, NeedsArgs: true, ArgPlaceholder: "{{url}}", ArgHint: "Supabase project URL，只存在本机", Category: "数据"},
	{ID: "qdrant", Name: "Qdrant", Description: "向量库查询，URL 本地填写", Transport: "stdio", Command: "npx", Args: []string{"-y", "@qdrant/mcp-server", "{{url}}"}, NeedsArgs: true, ArgPlaceholder: "{{url}}", ArgHint: "Qdrant URL，只存在本机", Category: "数据"},
	{ID: "elasticsearch", Name: "Elasticsearch", Description: "检索集群，URL 本地填写", Transport: "stdio", Command: "npx", Args: []string{"-y", "@elastic/mcp-server-elasticsearch", "{{url}}"}, NeedsArgs: true, ArgPlaceholder: "{{url}}", ArgHint: "Elasticsearch URL，只存在本机", Category: "数据"},
	{ID: "calculator", Name: "Calculator", Description: "精确算术，无网络、无密钥", Transport: "stdio", Command: "npx", Args: []string{"-y", "mcp-server-calculator"}, Category: "效率"},
	{ID: "duckduckgo", Name: "DuckDuckGo", Description: "无密钥网页搜索", Transport: "stdio", Command: "npx", Args: []string{"-y", "duckduckgo-mcp-server"}, Category: "网络"},
	{ID: "youtube-transcript", Name: "YouTube Transcript", Description: "拉取公开字幕，无密钥", Transport: "stdio", Command: "npx", Args: []string{"-y", "youtube-transcript-mcp"}, Category: "内容"},
	{ID: "markdownify", Name: "Markdownify", Description: "网页/文件转 Markdown", Transport: "stdio", Command: "npx", Args: []string{"-y", "markdownify-mcp"}, Category: "效率"},
	{ID: "linear", Name: "Linear", Description: "议题查询，API Key 本地填写", Transport: "stdio", Command: "npx", Args: []string{"-y", "linear-mcp-server", "{{key}}"}, NeedsArgs: true, ArgPlaceholder: "{{key}}", ArgHint: "Linear API Key，只存在本机", Category: "效率"},
	{ID: "mongodb", Name: "MongoDB", Description: "只连你填的 Mongo URL，密钥留在本机", Transport: "stdio", Command: "npx", Args: []string{"-y", "mcp-mongo-server", "{{url}}"}, NeedsArgs: true, ArgPlaceholder: "{{url}}", ArgHint: "Mongo 连接串，不会上传", Category: "数据"},
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
