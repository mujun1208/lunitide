package app

import (
	"context"
	"encoding/json"
	"errors"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/lunitide/lunitide/internal/domain/skill"
	"github.com/lunitide/lunitide/internal/mcp6"
)

// Capability discovery (capability-self-bootstrap P2). mcp.search only
// covers tools on already-connected endpoints, so a model facing a task it
// cannot do had nowhere to look. capability.discover answers "how could I
// do X" across four sources: built-in tools, connected MCP endpoint tools,
// installed published skills, and the curated one-click MCP preset
// catalog — so the model gets "可以，有这些途径" instead of a bare refusal.

// isGatewayTool reports whether a tool stays mounted on every tool-carrying
// turn regardless of lane, route or profile trimming: the MCP gateway pair,
// the one-click preset arm, and the capability discovery escape hatch. A
// turn that lacks these cannot find or add what a task needs, which is the
// failure mode the bootstrap design exists to fix. Separate agent loops
// (people agent, expert council) keep their own dispatch allowlists.
func isGatewayTool(name string) bool {
	return strings.HasPrefix(name, mcpToolPrefix) ||
		name == "mcp.search" || name == "mcp.call" ||
		name == "capability.discover" || name == "mcp.presets" || name == "mcp.install"
}

// builtinToolKeywords adds the Chinese vocabulary a natural-language
// capability need actually uses. Built-in tool descriptions are mostly
// English, so without this map 「搜索公司信息」 would never match web.search.
var builtinToolKeywords = map[string]string{
	"web.search":       "搜索 检索 查询 查一下 资讯 新闻 百度 谷歌 竞品 舆情 search web",
	"web.fetch":        "抓取 网页内容 打开链接 爬取 fetch url",
	"browser.act":      "浏览器 自动化 点击 网页操作 browser",
	"desktop.browse":   "系统浏览器 打开网页 默认浏览器",
	"desktop.open":     "打开 应用 程序 文件夹 快捷方式",
	"desktop.type":     "输入 填写 打字 键入",
	"desktop.quit":     "退出 关闭程序 彻底退出",
	"media.play":       "播放 音乐 歌曲 视频 电影 切歌 暂停",
	"weather.get":      "天气 气温 预报 weather",
	"location.get":     "定位 位置 经纬度 当前城市",
	"im.send":          "发消息 发送 消息 飞书 企微 钉钉 微信",
	"excel.gen":        "表格 excel xlsx 工作簿",
	"excel.parse":      "读取表格 解析 excel",
	"docx.gen":         "word 文档 docx 报告 论文",
	"pptx.gen":         "ppt 幻灯片 演示 汇报",
	"pdf.gen":          "pdf 生成文档",
	"html.gen":         "网页应用 小游戏 计时器 html",
	"image.generate":   "画图 生图 图片 插图 海报 image",
	"video.generate":   "生成视频 视频 video",
	"audio.generate":   "语音 朗读 配音 tts",
	"video.understand": "视频分析 视频内容 字幕",
	"memory.search":    "记忆 以前说过 长期记忆",
	"workspace.read":   "读文件 读取文件 文档内容",
	"workspace.write":  "写文件 保存文件 生成文件",
	"workspace.search": "搜文件 文件内搜索",
	"workspace.edit":   "改文件 编辑文件 替换",
	"command.run":      "命令 终端 执行命令 powershell",
	"run_terminal_cmd": "终端 命令行 build test git",
	"canvas.present":   "画布 展示 报告页",
	"todo.write":       "清单 待办 任务列表",
	"user.ask":         "问用户 决策 让用户选",
	"system.run":       "系统命令 运行程序 进程",
}

// capabilityStopBigrams are bigrams too generic to signal a capability
// (they appear in almost any need sentence), so they carry no evidence.
var capabilityStopBigrams = map[string]bool{
	"帮我": true, "可以": true, "需要": true, "能够": true, "一个": true,
	"这个": true, "那个": true, "什么": true, "怎么": true, "如何": true,
	"现在": true, "一下": true, "服务": true, "任务": true, "用户": true,
	"能力": true, "本机": true, "电脑": true, "我想": true, "我要": true,
}

// capabilityMatchThreshold is the evidence bar: one meaningful CJK bigram
// (+3) or one shared concept group (+20) clears it; incidental ASCII
// single-character noise does not.
const capabilityMatchThreshold = 3

// capabilityScore matches a natural-language capability need against one
// catalog entry's text: containment is a certain hit; otherwise CJK
// bigrams (minus stopwords) and shared concept groups accumulate.
func capabilityScore(need, text string) int {
	need = strings.ToLower(strings.TrimSpace(need))
	text = strings.ToLower(text)
	if need == "" {
		return 0
	}
	if strings.Contains(text, need) {
		return 100
	}
	score := 0
	for _, concept := range mcpSearchConcepts {
		requested, matches := false, false
		for _, term := range concept {
			requested = requested || strings.Contains(need, term)
			matches = matches || strings.Contains(text, term)
		}
		if requested && matches {
			score += 20
		}
	}
	runes := []rune(need)
	for i := 0; i+1 < len(runes); i++ {
		bg := string(runes[i : i+2])
		if capabilityStopBigrams[bg] {
			continue
		}
		if isCJKBigram(runes[i], runes[i+1]) && strings.Contains(text, bg) {
			score += 3
		}
	}
	for _, term := range strings.Fields(need) {
		if len(term) >= 3 && strings.Contains(text, term) {
			score += 2
		}
	}
	return score
}

func isCJKBigram(a, b rune) bool {
	return a >= 0x4E00 && a <= 0x9FFF && b >= 0x4E00 && b <= 0x9FFF
}

// discoveredCapability is one match returned to the model.
type discoveredCapability struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	How         string `json:"how"`
}

type capabilityHit struct {
	discoveredCapability
	score int
}

func finalizeCapabilityHits(hits []capabilityHit, max int) []discoveredCapability {
	sort.SliceStable(hits, func(i, j int) bool { return hits[i].score > hits[j].score })
	out := make([]discoveredCapability, 0, len(hits))
	for _, h := range hits {
		if len(out) >= max {
			break
		}
		out = append(out, h.discoveredCapability)
	}
	return out
}

// discoverCapabilities implements the capability.discover tool: one
// natural-language need, matched against every capability source the
// product has, with concrete how-to-use / how-to-install guidance.
func (e *Engine) discoverCapabilities(ctx context.Context, raw json.RawMessage) (string, error) {
	var a struct {
		Need string `json:"need"`
	}
	if json.Unmarshal(raw, &a) != nil || strings.TrimSpace(a.Need) == "" || utf8.RuneCountInString(a.Need) > 400 {
		return "", errors.New("capability.discover needs need (1-400 characters)")
	}
	need := strings.TrimSpace(a.Need)

	var connectedHits []capabilityHit
	if e.mcp6Registry != nil {
		for _, t := range e.mcp6Registry.ReadyToolSnapshot() {
			name, ok := mcpToolName(t.EndpointID, t.Tool)
			if !ok {
				continue
			}
			score := capabilityScore(need, t.Tool+" "+t.Description+" "+name)
			if score < capabilityMatchThreshold {
				continue
			}
			connectedHits = append(connectedHits, capabilityHit{discoveredCapability{
				Name: name, Description: truncateUTF8Bytes(t.Description, 200),
				How: "已连接 MCP 工具，直接调用（或经 mcp.call）",
			}, score})
		}
	}

	var builtinHits []capabilityHit
	for _, d := range engineToolDefinitions() {
		score := capabilityScore(need, d.Name+" "+d.Description+" "+builtinToolKeywords[d.Name])
		if score < capabilityMatchThreshold {
			continue
		}
		builtinHits = append(builtinHits, capabilityHit{discoveredCapability{
			Name: d.Name, Description: truncateUTF8Bytes(d.Description, 200),
			How: "内置工具；本轮未挂载时换更明确的任务表述或切换执行模式",
		}, score})
	}

	var skillHits []capabilityHit
	if skillServiceAvailable(e.skills) {
		if list, err := e.skills.List(ctx, skill.SkillStatusPublished); err == nil {
			for _, sk := range list {
				score := capabilityScore(need, sk.Name+" "+sk.DisplayName+" "+sk.Description)
				if score < capabilityMatchThreshold {
					continue
				}
				skillHits = append(skillHits, capabilityHit{discoveredCapability{
					Name: sk.Name, Description: truncateUTF8Bytes(sk.Description, 200),
					How: "已安装技能，用 skill.invoke 调用",
				}, score})
			}
		}
	}

	var presetHits []capabilityHit
	for _, p := range mcp6.Presets() {
		score := capabilityScore(need, p.ID+" "+p.Name+" "+p.Description+" "+p.Category)
		if score < capabilityMatchThreshold {
			continue
		}
		how := "征得用户同意后 mcp.install 一键安装（presetId=" + p.ID + "；手动审批模式会弹审批卡），或让用户在设置里安装"
		if e.presetInstalledBefore(p.ID) {
			// capability-self-bootstrap P4: the user approved this preset
			// before — the honest next step is reconnecting the surviving
			// endpoint, not paying for a second install.
			how = "此前已安装过此预置；若其工具未挂载，请在设置中重新连接对应端点，无需重复安装"
		}
		presetHits = append(presetHits, capabilityHit{discoveredCapability{
			Name: p.ID, Description: p.Name + "：" + truncateUTF8Bytes(p.Description, 200),
			How:  how,
		}, score})
	}

	connected := finalizeCapabilityHits(connectedHits, 6)
	builtin := finalizeCapabilityHits(builtinHits, 6)
	skills := finalizeCapabilityHits(skillHits, 6)
	installable := finalizeCapabilityHits(presetHits, 8)

	guidance := "没有找到匹配能力：向用户说明缺什么能力，建议在设置中连接对应 MCP 端点、安装技能或反馈给产品；不要虚构工具，也不要未尝试就宣布任务无法完成。"
	if len(connected)+len(builtin)+len(skills)+len(installable) > 0 {
		guidance = "已连接/已安装的能力可直接使用；可安装预置先征得用户同意再用 mcp.install（手动审批模式下会弹出审批卡，用户批准后即安装），安装后下一轮生效，本轮如实报告安装结果。"
	}
	b, err := json.Marshal(map[string]any{
		"need":               need,
		"connectedMcp":       connected,
		"builtin":            builtin,
		"skills":             skills,
		"installablePresets": installable,
		"guidance":           guidance,
	})
	if err != nil {
		return "", err
	}
	return string(b), nil
}
