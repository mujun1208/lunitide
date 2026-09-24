package producthub

import (
	"fmt"
	"html"
	"strings"
	"time"
)

func RenderReport(ed Edition) (markdown, pageHTML string) {
	var md strings.Builder
	var hs strings.Builder
	stamp := ed.GeneratedAt
	if stamp == "" {
		stamp = time.Now().Format(time.RFC3339)
	}
	fmt.Fprintf(&md, "# Lunitide 产品说明书（第 %s 版）\n\n", ed.EditionID)
	fmt.Fprintf(&md, "生成时间：%s  \n功能卡：%d  · 新增 %d · 更新 %d · 退役 %d  · 健康分 %d\n\n", stamp, ed.CardCount, ed.Added, ed.Updated, ed.Removed, ed.HealthScore)
	md.WriteString("本文由产品知识中枢对照**初版种子**与**当前活源**实时生成。新产品动词（放歌、开文件、新 Page、新设置）下次生成会自动出现，不必手改总表。\n\n")
	md.WriteString("## 1. 产品总述\n\n")
	md.WriteString("Lunitide 是本机优先的智能工作台：月伴语音与打字对话、办公与媒体、技能/专家/MCP/插件、会议与机务、电脑控制与浏览器，以及底座治理。每个原子功能一张知识卡，含简介、描述、属性、方法、调用链路（成功/失败/重试/降级）和脚手架底座。\n\n")
	md.WriteString("## 2. 本体与体系\n\n")
	md.WriteString("节点类型：Product / Domain / Module / Feature / Expert / Skill / Plugin / Mcp / McpTool / Chain / Step / Capability / Scenario。\n\n")
	md.WriteString("| 域 | 模块数 | 功能卡 |\n|---|---:|---:|\n")
	for _, d := range domainStats(ed.Features) {
		fmt.Fprintf(&md, "| %s | %d | %d |\n", d.Name, d.Modules, d.Cards)
	}
	md.WriteString("\n## 3. 功能知识库（逐张拆解）\n\n")
	for _, c := range ed.Features {
		fmt.Fprintf(&md, "### %s · `%s`\n\n", c.Name, c.StableKey)
		fmt.Fprintf(&md, "- 英文：%s  · 域/模块：%s / %s  · 来源：%s\n", c.NameEN, c.Domain, c.Module, c.Provenance)
		fmt.Fprintf(&md, "- **A 简介**：%s\n", c.Summary)
		fmt.Fprintf(&md, "- **B 描述**：%s\n", c.Description)
		fmt.Fprintf(&md, "- **C 属性**：操作 %s；工具 %s；MCP %s；技能 %s；能力 %s\n",
			join(c.Attributes.Operations), join(c.Attributes.Tools), join(c.Attributes.MCPs), join(c.Attributes.Skills), join(c.Attributes.Capabilities))
		md.WriteString("- **D 方法**：")
		for _, m := range c.Methods {
			fmt.Fprintf(&md, "%s（%s%s）；", m.Type, m.Entry, cont(m.Continuous))
		}
		md.WriteString("\n- **脚手架**：页面 " + join(c.Scaffold.Pages) + "；Bridge " + join(c.Scaffold.Bridge) + "；设置 " + join(c.Scaffold.Settings) + "；运行时 " + join(c.Scaffold.Runtime) + "\n")
		md.WriteString("- **标签**：" + join(c.Tags) + "\n")
		fmt.Fprintf(&md, "- **原理**：%s\n- **逻辑**：%s\n- **技术**：%s\n- **总结分析**：%s\n", c.Principle, c.Logic, c.Tech, c.Analysis)
		md.WriteString("- **链路步骤**：\n")
		for _, st := range c.Chain.Steps {
			fmt.Fprintf(&md, "  %d. %s — %s。%s\n", st.Index, st.Name, st.Detail, st.Description)
		}
		for _, b := range c.Chain.Branches {
			fmt.Fprintf(&md, "  - %s（自步骤 %d）：%s\n", b.Name, b.FromStep, b.Description)
			if b.Retry != nil {
				fmt.Fprintf(&md, "    - 重试：%s\n", b.Retry.Description)
			}
			if b.Fallback != nil {
				fmt.Fprintf(&md, "    - 降级：%s\n", b.Fallback.Description)
			}
		}
		md.WriteString("\n")
	}
	md.WriteString("## 4. 知识图谱摘要\n\n")
	fmt.Fprintf(&md, "节点 %d，边 %d。关系：contains / uses / calls / depends。图谱页可点 Feature 打开知识卡。\n\n", len(ed.Graph.Nodes), len(ed.Graph.Edges))
	md.WriteString("## 5. 自净化诊断\n\n")
	md.WriteString(diagnosisVerdict(ed))
	if len(ed.Findings) == 0 {
		md.WriteString("本轮无发现。\n\n")
	}
	for _, f := range ed.Findings {
		fmt.Fprintf(&md, "### [%s] %s · %s\n\n", f.Severity, f.ErrorCode, f.Title)
		fmt.Fprintf(&md, "- 对象：`%s`  · 状态：%s\n- 证据：%s\n- 根因：%s\n- 改进方案：%s\n- 验证：%s\n\n", f.StableKey, f.Status, f.Evidence, f.RootCause, f.Fix, f.Verify)
	}
	md.WriteString("诊断可在中枢内点「执行净化」：本地修复标签/入口方法，并把任务书交给内部技能或模型。不自动改写 Go/TS；已处理条目下次生成保持 applied。\n\n")
	md.WriteString("## 6. 竞品与前沿\n\n")
	md.WriteString("图景页两张槽位：竞品对照（必须带来源与日期）与 SMTC/owned runtime 播放核验前沿观察。不计入健康度。\n\n")
	md.WriteString("## 7. 总结\n\n")
	fmt.Fprintf(&md, "本版覆盖 %d 张功能卡。以后加放歌/开文件/新设置，只要活源出现，再点生成即可并入，不必重写初版总表。\n", ed.CardCount)

	hs.WriteString(`<!doctype html><html lang="zh-CN"><head><meta charset="utf-8"><title>Lunitide 产品说明书</title>`)
	hs.WriteString(`<style>body{margin:0;background:#0a0a0a;color:#f4f4f4;font:15px/1.55 system-ui,sans-serif}main{max-width:920px;margin:0 auto;padding:32px 24px}h1,h2,h3{font-weight:600}h1{font-size:28px}a{color:#fff}section{border:1px solid #2a2a2a;background:#141414;padding:16px 18px;margin:14px 0}code{color:#ccc}.meta{color:#9a9a9a}</style></head><body><main>`)
	fmt.Fprintf(&hs, "<h1>Lunitide 产品说明书</h1><p class=\"meta\">%s · %d 张卡 · 健康分 %d</p>", html.EscapeString(stamp), ed.CardCount, ed.HealthScore)
	hs.WriteString("<section><h2>总述</h2><p>本机优先的智能工作台。每个原子功能一张知识卡：简介、描述、属性、方法、链路与脚手架。本文由模块内「生成最新说明书」对照初版种子与活源实时生成。</p></section>")
	for _, c := range ed.Features {
		fmt.Fprintf(&hs, "<section id=\"%s\"><h3>%s <code>%s</code></h3>", html.EscapeString(c.StableKey), html.EscapeString(c.Name), html.EscapeString(c.StableKey))
		fmt.Fprintf(&hs, "<p><b>A</b> %s</p><p><b>B</b> %s</p>", html.EscapeString(c.Summary), html.EscapeString(c.Description))
		fmt.Fprintf(&hs, "<p><b>C</b> 工具 %s</p>", html.EscapeString(join(c.Attributes.Tools)))
		fmt.Fprintf(&hs, "<p><b>原理</b> %s</p><p><b>逻辑</b> %s</p><p><b>技术</b> %s</p><p><b>分析</b> %s</p>", html.EscapeString(c.Principle), html.EscapeString(c.Logic), html.EscapeString(c.Tech), html.EscapeString(c.Analysis))
		hs.WriteString("<ol>")
		for _, st := range c.Chain.Steps {
			fmt.Fprintf(&hs, "<li>%s — %s</li>", html.EscapeString(st.Name), html.EscapeString(st.Description))
		}
		hs.WriteString("</ol></section>")
	}
	hs.WriteString("<section><h2>诊断</h2>")
	for _, f := range ed.Findings {
		fmt.Fprintf(&hs, "<p><b>%s</b> %s：%s<br>方案：%s</p>", html.EscapeString(f.ErrorCode), html.EscapeString(f.Title), html.EscapeString(f.Evidence), html.EscapeString(f.Fix))
	}
	hs.WriteString("</section></main></body></html>")
	return md.String(), hs.String()
}

func diagnosisVerdict(ed Edition) string {
	var openErr, openWarn int
	var lines []string
	for _, f := range ed.Findings {
		if f.ErrorCode == "PH_000" || isResolvedFinding(f.Status) {
			continue
		}
		switch f.Severity {
		case "error":
			openErr++
		case "warn":
			openWarn++
		}
		if f.Severity == "error" || f.Severity == "warn" {
			lines = append(lines, fmt.Sprintf("- %s %s：%s 验证：%s", f.ErrorCode, f.Title, f.Fix, f.Verify))
		}
	}
	cover := ""
	for _, f := range ed.Findings {
		if f.ErrorCode == "PH_000" {
			cover = f.Evidence
		}
	}
	var b strings.Builder
	fmt.Fprintf(&b, "本轮核对说明书与活源。%s 健康分 %d 是入口覆盖，不是语音听写、媒体播放、文件落盘或任务完成的实测分。\n\n", cover, ed.HealthScore)
	if openErr+openWarn == 0 {
		b.WriteString("没有可执行的目录修复项。入口对齐之后，功能是否真能做完，要在对应页面实测。\n\n")
		return b.String()
	}
	fmt.Fprintf(&b, "可执行项：错误 %d，警告 %d。\n\n", openErr, openWarn)
	for _, line := range lines {
		b.WriteString(line + "\n")
	}
	b.WriteString("\n")
	return b.String()
}

func join(in []string) string {
	if len(in) == 0 {
		return "—"
	}
	return strings.Join(in, ", ")
}

func cont(s string) string {
	if s == "" {
		return ""
	}
	return "；" + s
}
