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
	"@agent360/browser-mcp":             true,
	"@antv/mcp-server-chart":            true,
	"@amap/amap-maps-mcp-server":        true,
	"tavily-mcp":                        true,
	"firecrawl-mcp":                     true,
	"@notionhq/notion-mcp-server":       true,
	"excel-mcp-server":                  true,
	"office-word-mcp-server":            true,
	"office-powerpoint-mcp-server":      true,
	"office-ppt-mcp-server":             true,
	"ppt-mcp":                           true,
	"pdfnative-mcp":                     true,
	"@larksuiteoapi/lark-mcp":           true,
	"@sentry/mcp-server":                true,
	"@microsoft/markitdown-mcp":         true,
	"@neondatabase/mcp-server-neon":     true,
	"@supabase/mcp-server-supabase":     true,
	"mcp-server-qdrant":                 true,
	"@elastic/mcp-server-elasticsearch": true,
	"mcp-server-calculator":             true,
	"duckduckgo-mcp-server":             true,
	"youtube-transcript-mcp":            true,
	"markdownify-mcp":                   true,
	"linear-mcp-server":                 true,
	"mcp-mongo-server":                  true,
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

// presets is the curated catalog. Official reference servers plus a
// reviewed community shelf (LobeHub / awesome-mcp). Archived 2025
// packages (GitHub / Puppeteer / SQLite / Git) are not listed.
// Credentials are configured in the host vault via CredentialEnvs / bearer.
// NeedsArgs is reserved for non-secret launch arguments. Order is display order.
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
	{ID: "postgres", Name: "Postgres", Description: "只连你填的 Postgres URL，密钥留在本机", Transport: "stdio", Command: "npx", Args: []string{"-y", "@modelcontextprotocol/server-postgres", "{{url}}"}, NeedsArgs: true, ArgPlaceholder: "{{url}}", ArgHint: "本机 Postgres 连接串，不会上传", Category: "数据"},
	{ID: "redis", Name: "Redis", Description: "只连你填的 Redis URL，密钥留在本机", Transport: "stdio", Command: "npx", Args: []string{"-y", "@modelcontextprotocol/server-redis", "{{url}}"}, NeedsArgs: true, ArgPlaceholder: "{{url}}", ArgHint: "本机 Redis 连接串，不会上传", Category: "数据"},
	{ID: "google-maps", Name: "Google Maps", Description: "地理编码与地点查询；配置 Google Maps API Key（旧版参考服务器）", Transport: "stdio", Command: "npx", Args: []string{"-y", "@modelcontextprotocol/server-google-maps"}, Category: "网络", NeedsCredential: true, CredentialEnvs: []string{"GOOGLE_MAPS_API_KEY"}},
	{ID: "brave-search", Name: "Brave Search", Description: "Brave 搜索，API Key 本地填写", Transport: "stdio", Command: "npx", Args: []string{"-y", "@modelcontextprotocol/server-brave-search"}, NeedsCredential: true, CredentialEnvs: []string{"BRAVE_API_KEY"}, Category: "网络"},
	{ID: "gitlab", Name: "GitLab", Description: "GitLab 项目与议题；配置访问令牌，自建实例再配置 API URL", Transport: "stdio", Command: "npx", Args: []string{"-y", "@modelcontextprotocol/server-gitlab"}, NeedsCredential: true, CredentialEnvs: []string{"GITLAB_PERSONAL_ACCESS_TOKEN", "GITLAB_API_URL"}, Category: "开发"},
	{ID: "sentry", Name: "Sentry", Description: "官方 Sentry 问题查询；配置 SENTRY_ACCESS_TOKEN，AI 搜索还需模型凭据", Transport: "stdio", Command: "npx", Args: []string{"-y", "@sentry/mcp-server"}, Category: "开发", NeedsCredential: true, CredentialEnvs: []string{"SENTRY_ACCESS_TOKEN"}},
	{ID: "gdrive", Name: "Google Drive", Description: "Google Drive 文件检索；先完成上游 OAuth，再配置凭据文件路径", Transport: "stdio", Command: "npx", Args: []string{"-y", "@modelcontextprotocol/server-gdrive"}, NeedsCredential: true, CredentialEnvs: []string{"GDRIVE_CREDENTIALS_PATH"}, SetupURL: "https://github.com/modelcontextprotocol/servers-archived/tree/main/src/gdrive", Category: "文件"},
	{ID: "everart", Name: "EverArt", Description: "图像生成 MCP，API Key 本地填写", Transport: "stdio", Command: "npx", Args: []string{"-y", "@modelcontextprotocol/server-everart"}, NeedsCredential: true, CredentialEnvs: []string{"EVERART_API_KEY"}, Category: "内容"},
	{ID: "aws-kb", Name: "AWS KB", Description: "AWS Bedrock 知识库检索；配置账号凭据和区域", Transport: "stdio", Command: "npx", Args: []string{"-y", "@modelcontextprotocol/server-aws-kb-retrieval"}, NeedsCredential: true, CredentialEnvs: []string{"AWS_ACCESS_KEY_ID", "AWS_SECRET_ACCESS_KEY", "AWS_REGION", "AWS_SESSION_TOKEN"}, Category: "数据"},
	{ID: "chrome-devtools", Name: "Chrome DevTools", Description: "官方 Chrome DevTools MCP。人装才生效，不是默认电脑控制，月伴不会自动安装。默认网页自动化请用 Playwright。", Transport: "stdio", Command: "npx", Args: []string{"-y", "chrome-devtools-mcp"}, Category: "浏览器"},
	{ID: "browsermcp", Name: "Browser MCP", Description: "使用已登录的本机 Chrome（需扩展）。人装才生效，不是默认电脑控制，也不是引擎劫持用户 Chrome。默认网页自动化请用 Playwright。", Transport: "stdio", Command: "npx", Args: []string{"-y", "@agent360/browser-mcp"}, Category: "浏览器"},
	{ID: "antv-chart", Name: "AntV Chart", Description: "按描述生成图表，默认走公开图表服务", Transport: "stdio", Command: "npx", Args: []string{"-y", "@antv/mcp-server-chart"}, Category: "开发"},
	{ID: "amap", Name: "高德地图", Description: "地理编码与路线，Key 本地填写", Transport: "stdio", Command: "npx", Args: []string{"-y", "@amap/amap-maps-mcp-server"}, NeedsCredential: true, CredentialEnvs: []string{"AMAP_MAPS_API_KEY"}, Category: "网络"},
	{ID: "tavily", Name: "Tavily", Description: "联网检索；通过凭据配置 Tavily API Key", Transport: "stdio", Command: "npx", Args: []string{"-y", "tavily-mcp"}, Category: "网络", NeedsCredential: true, CredentialEnvs: []string{"TAVILY_API_KEY"}},
	{ID: "firecrawl", Name: "Firecrawl", Description: "网页搜索与抓取；通过凭据配置 Firecrawl API Key", Transport: "stdio", Command: "npx", Args: []string{"-y", "firecrawl-mcp"}, Category: "网络", NeedsCredential: true, CredentialEnvs: []string{"FIRECRAWL_API_KEY"}},
	{ID: "notion", Name: "Notion", Description: "Notion 工作区；先配置集成令牌并在 Notion 授权相关页面", Transport: "stdio", Command: "npx", Args: []string{"-y", "@notionhq/notion-mcp-server"}, Category: "效率", NeedsCredential: true, CredentialEnvs: []string{"NOTION_TOKEN"}},
	{ID: "excel-mcp", Name: "Excel MCP", Description: "本机表格读写（无需安装 Excel）。新建工作簿仍优先用月汐 excel.gen / office.generate", Transport: "stdio", Command: "uvx", Args: []string{"excel-mcp-server", "stdio"}, Category: "办公", SetupURL: "https://github.com/haris-musa/excel-mcp-server"},
	{ID: "word-mcp", Name: "Word MCP", Description: "深度编辑已有 Word：段落、表格、样式。新建文档仍优先用月汐 docx.gen / office.generate", Transport: "stdio", Command: "uvx", Args: []string{"--from", "office-word-mcp-server", "word_mcp_server"}, Category: "办公", SetupURL: "https://github.com/GongRzhe/Office-Word-MCP-Server"},
	{ID: "ppt-mcp", Name: "PPT MCP", Description: "深度编辑已有演示：增删页与布局。新建演示仍优先用月汐 pptx.gen / office.generate，不走 Office COM", Transport: "stdio", Command: "uvx", Args: []string{"office-ppt-mcp-server"}, Category: "办公", SetupURL: "https://pypi.org/project/office-ppt-mcp-server/"},
	{ID: "pdf-mcp", Name: "PDF MCP", Description: "本机生成/批注/书签等 PDF 操作。简单文本 PDF 仍优先用月汐 pdf.gen / office.generate", Transport: "stdio", Command: "npx", Args: []string{"-y", "pdfnative-mcp"}, Category: "办公", SetupURL: "https://github.com/Nizoka/pdfnative-mcp"},
	{ID: "lark", Name: "飞书", Description: "飞书开放平台；配置应用 ID、密钥，用户身份接口还需用户授权", Transport: "stdio", Command: "npx", Args: []string{"-y", "@larksuiteoapi/lark-mcp", "mcp", "-l", "zh"}, NeedsCredential: true, CredentialEnvs: []string{"APP_ID", "APP_SECRET", "USER_ACCESS_TOKEN"}, Category: "效率"},
	{ID: "huggingface", Name: "Hugging Face", Description: "官方远程模型、数据集与论文检索；配置 HF Token 后连接", Transport: "https", URL: "https://huggingface.co/mcp", Args: []string{}, Category: "开发", NeedsCredential: true, SetupURL: "https://huggingface.co/docs/hub/en/agents-mcp"},
	{ID: "markitdown", Name: "MarkItDown", Description: "Office/PDF 转 Markdown，本机转换；需要 Python 3.10+，首次启动会准备依赖", Transport: "stdio", Command: "uvx", Args: []string{"markitdown-mcp"}, Category: "办公", SetupURL: "https://github.com/microsoft/markitdown/tree/main/packages/markitdown-mcp"},
	{ID: "neon", Name: "Neon", Description: "官方远程 Postgres 服务；配置 Neon API Key", Transport: "https", URL: "https://mcp.neon.tech/mcp", Args: []string{}, NeedsCredential: true, Category: "数据", SetupURL: "https://neon.tech/docs/ai/neon-mcp-server"},
	{ID: "supabase", Name: "Supabase", Description: "Supabase 项目管理；通过凭据配置 Personal Access Token", Transport: "stdio", Command: "npx", Args: []string{"-y", "@supabase/mcp-server-supabase"}, NeedsCredential: true, CredentialEnvs: []string{"SUPABASE_ACCESS_TOKEN"}, Category: "数据"},
	{ID: "qdrant", Name: "Qdrant", Description: "官方向量库记忆存取；先填 QDRANT_URL，云端实例再填 QDRANT_API_KEY", Transport: "stdio", Command: "uvx", Args: []string{"mcp-server-qdrant"}, Category: "数据", NeedsCredential: true, CredentialEnvs: []string{"QDRANT_URL", "QDRANT_API_KEY"}},
	{ID: "elasticsearch", Name: "Elasticsearch", Description: "Elasticsearch 检索；旧版 npm 接入，配置集群地址与凭据", Transport: "stdio", Command: "npx", Args: []string{"-y", "@elastic/mcp-server-elasticsearch"}, NeedsCredential: true, CredentialEnvs: []string{"ES_URL", "ES_API_KEY", "ES_USERNAME", "ES_PASSWORD"}, SetupURL: "https://github.com/elastic/mcp-server-elasticsearch", Category: "数据"},
	{ID: "calculator", Name: "Calculator", Description: "精确算术，无网络、无密钥", Transport: "stdio", Command: "uvx", Args: []string{"mcp-server-calculator"}, Category: "效率"},
	{ID: "duckduckgo", Name: "DuckDuckGo", Description: "无密钥网页搜索；调用时需要网络可访问 DuckDuckGo", Transport: "stdio", Command: "npx", Args: []string{"-y", "duckduckgo-mcp-server"}, Category: "网络", SetupURL: "https://github.com/zhsama/duckduckgo-mcp-server"},
	{ID: "youtube-transcript", Name: "YouTube Transcript", Description: "拉取公开字幕，无密钥", Transport: "stdio", Command: "npx", Args: []string{"-y", "youtube-transcript-mcp"}, Category: "内容"},
	{ID: "markdownify", Name: "Markdownify", Description: "网页、文件与音频转 Markdown；上游需从源码构建后接入本地入口", Transport: "stdio", Command: "node", Args: []string{"{{entry}}"}, NeedsArgs: true, ArgPlaceholder: "{{entry}}", ArgHint: "已构建的 Markdownify dist/index.js 绝对路径", Category: "效率", SetupURL: "https://github.com/zcaceres/markdownify-mcp"},
	{ID: "linear", Name: "Linear", Description: "官方远程议题与项目管理；配置 Linear API Key", Transport: "https", URL: "https://mcp.linear.app/mcp", Args: []string{}, NeedsCredential: true, Category: "效率", SetupURL: "https://linear.app/docs/mcp"},
	{ID: "mongodb", Name: "MongoDB", Description: "通过凭据配置 MongoDB 连接串，支持查询和文档操作", Transport: "stdio", Command: "npx", Args: []string{"-y", "mcp-mongo-server"}, NeedsCredential: true, CredentialEnvs: []string{"MCP_MONGODB_URI"}, Category: "数据"},
	{ID: "juhe-query", Name: "聚合日常查询", Description: "直连聚合数据 MCP；按账号开通天气、火车、航班等接口，先配置平台 MCP Token", Transport: "https", URL: "https://mcp.juhe.cn/mcp?token={{credential}}", Args: []string{}, Category: "网络", NeedsCredential: true, SetupURL: "https://www.juhe.cn/docs/api/id/817"},
	{ID: "tushare", Name: "Tushare 股票数据", Description: "官方金融数据 MCP，先配置个人 Token；免费基础权限仅非复权日线，实时行情及其他数据按账号权限开通", Transport: "https", URL: "https://api.tushare.pro/mcp/token={{credential}}", Args: []string{}, Category: "数据", NeedsCredential: true, SetupURL: "https://tushare.pro/document/1?doc_id=463"},
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
