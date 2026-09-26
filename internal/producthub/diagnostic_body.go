package producthub

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"
)

func diagnosticInventory(ed Edition) string {
	started := time.Now()
	var b strings.Builder
	features := productFeatures(ed.Features)
	b.WriteString("这一节核对这一版已经组装好的内容。逐张说明在第 3 节。对得上处理函数的链路写成这次从源码读到的调用。对不上的手写步骤不留在报告里，记为还没按真实调用写清。带点的入口对到桥方法和运行工具，插件对到运行名单。听写、播放、下载、识图和上次之后的新日志另计。诊断不改产品代码，也不编对手分数。\n\n")
	b.WriteString("收口：已跑完只表示临时库里的读回，句子写明实际读到的内容，以及没有动到的本机动作。入口已跑到表示处理函数已返回，拒绝原因就是证据，不当成功能已坏。不代跑表示这一项会打开窗口、占用麦克风、安装、联网或执行命令，诊断不代执行。模板步骤不能当成已经走通。没有这三种结论才叫尚未跑完。\n\n")

	writeNamed(&b, "功能", fmt.Sprintf("系统功能就是这 %d 张功能卡。没有入口方法：%s。", len(features), orNone(missingMethodNames(features))), cardNames(features))
	unclear := emptyChainNames(features)
	unwritten := unwrittenChainNames(features)
	broken := brokenLinks(ed.Graph)
	chainLead := fmt.Sprintf("有步骤 %d 张。步骤还不清楚 %d 张：%s。步骤未按真实调用写清 %d 张：%s。链路没有接上：%s。断边才叫链路不顺畅。没有对上处理函数的手写步骤不算走通，也不能当成已经从源码追通，也不能当成任务已经做完。", len(features)-len(unclear), len(unclear), orNone(unclear), len(unwritten), orNone(unwritten), orNone(broken))
	if len(unclear) > 0 {
		chainLead += "没有步骤的卡，修复时按该功能的真实调用补上。诊断不会编造步骤。"
	}
	writeNamed(&b, "链路", chainLead, nil)
	writeNamed(&b, "工具", "卡上写明的工具、能力和 Bridge。", collectTools(features))
	writeNamed(&b, "技能", "图谱节点和卡上写明的技能。", append(nodeNames(ed.Graph, "Skill"), attrNames(features, func(c Card) []string { return c.Attributes.Skills })...))
	writeNamed(&b, "MCP", "图谱节点和卡上写明的 MCP。", append(nodeNames(ed.Graph, "Mcp"), attrNames(features, func(c Card) []string { return c.Attributes.MCPs })...))
	writeNamed(&b, "插件", "图谱节点和带插件键的功能卡。", append(nodeNames(ed.Graph, "Plugin"), pluginFeatureNames(features)...))
	writeNamed(&b, "资产", "域为 assets 的功能卡。", namesWhere(features, func(c Card) bool { return c.Domain == "assets" }))
	writeNamed(&b, "项目", "键、模块或名称里带项目的功能卡。", namesWhere(features, isProjectCard))
	writeNamed(&b, "开发能力", "执行、命令、工作区和 Agent Hub。", namesWhere(features, isDevCard))
	writeNamed(&b, "任务处理", "名称或键里带任务，或属于办公与自动化。", namesWhere(features, isTaskCard))
	writeNamed(&b, "脚手架", fmt.Sprintf("写了页面、Bridge、设置或运行时的有 %d 张。脚手架还是空的：%s。", scaffoldFilled(features), orNone(emptyScaffoldNames(features))), nil)
	writeNamed(&b, "架构", architectureLine(ed, features), nil)
	b.WriteString(wiringSection(features))
	writeNamed(&b, "实测", probeLine(ed), nonemptyOrNil(openProbeNames(ed.Findings)))
	writeNamed(&b, "日志与故障", logLine(ed.Findings), nonemptyOrNil(logNames(ed.Findings)))
	writeNamed(&b, "竞品对照", landscapeLine(ed.Findings), nonemptyOrNil(landscapeNames(ed.Findings)))
	b.WriteString(scopeSections(ed, features, started))
	return b.String()
}

func diagnosticGapPlans(ed Edition) string {
	features := productFeatures(ed.Features)
	var b strings.Builder
	for _, name := range emptyChainNames(features) {
		fmt.Fprintf(&b, "- %s：步骤还不清楚。按该功能的真实调用补上步骤。净化不会编造步骤，也不会改产品代码。\n", name)
	}
	for _, name := range unwrittenChainNames(features) {
		fmt.Fprintf(&b, "- %s：步骤未按真实调用写清。这次没有对上处理函数，手写步骤不作为链路。诊断不会编造步骤。\n", name)
	}
	for _, link := range uniqueSorted(brokenLinks(ed.Graph)) {
		fmt.Fprintf(&b, "- %s：链路没有接上。图谱边的一端不在节点里。修复时补上缺失节点或删掉这条边。净化不会猜一条新链路。\n", link)
	}
	for _, name := range missingMethodNames(features) {
		fmt.Fprintf(&b, "- %s：没有入口方法。目录里已有默认入口的，执行净化可以直接补上。没有默认入口的保持待处理。\n", name)
	}
	if b.Len() == 0 {
		return ""
	}
	b.WriteString("\n")
	return b.String()
}

func productFeatures(cards []Card) []Card {
	out := make([]Card, 0, len(cards))
	for _, c := range cards {
		if strings.HasPrefix(c.StableKey, "landscape.") {
			continue
		}
		out = append(out, c)
	}
	return out
}

func writeNamed(b *strings.Builder, title, lead string, items []string) {
	fmt.Fprintf(b, "### %s\n\n%s\n", title, lead)
	if items == nil {
		b.WriteString("\n")
		return
	}
	items = uniqueSorted(items)
	if len(items) == 0 {
		b.WriteString("本版组装里没有单独列出来。\n\n")
		return
	}
	b.WriteString(strings.Join(items, "、"))
	b.WriteString("。\n\n")
}

func nonemptyOrNil(items []string) []string {
	if len(uniqueSorted(items)) == 0 {
		return nil
	}
	return items
}

func cardNames(cards []Card) []string {
	out := make([]string, 0, len(cards))
	for _, c := range cards {
		out = append(out, cardLabel(c))
	}
	return out
}

func cardLabel(c Card) string {
	if strings.TrimSpace(c.Name) != "" {
		return c.Name
	}
	return c.StableKey
}

func namesWhere(cards []Card, ok func(Card) bool) []string {
	var out []string
	for _, c := range cards {
		if ok(c) {
			out = append(out, cardLabel(c))
		}
	}
	return out
}

func emptyChainNames(cards []Card) []string {
	return namesWhere(cards, func(c Card) bool { return len(c.Chain.Steps) == 0 })
}

func missingMethodNames(cards []Card) []string {
	return namesWhere(cards, func(c Card) bool { return len(c.Methods) == 0 })
}

func emptyScaffoldNames(cards []Card) []string {
	return namesWhere(cards, func(c Card) bool {
		return len(c.Scaffold.Pages) == 0 && len(c.Scaffold.Bridge) == 0 && len(c.Scaffold.Settings) == 0 && len(c.Scaffold.Runtime) == 0
	})
}

func scaffoldFilled(cards []Card) int {
	n := 0
	for _, c := range cards {
		if len(c.Scaffold.Pages)+len(c.Scaffold.Bridge)+len(c.Scaffold.Settings)+len(c.Scaffold.Runtime) > 0 {
			n++
		}
	}
	return n
}

func isProjectCard(c Card) bool {
	return c.Module == "project" || strings.Contains(c.StableKey, "project") || strings.Contains(c.Name, "项目")
}

func isDevCard(c Card) bool {
	if c.Domain == "execution" || c.Module == "command" || c.Module == "workspace" || c.Module == "agenthub" {
		return true
	}
	key := c.StableKey
	return strings.Contains(key, "command") || strings.Contains(key, "agenthub") || strings.Contains(key, "workspace")
}

func isTaskCard(c Card) bool {
	return strings.Contains(c.Name, "任务") || strings.Contains(c.StableKey, ".task") || c.Module == "office" || c.Module == "automation"
}

func pluginFeatureNames(cards []Card) []string {
	return namesWhere(cards, func(c Card) bool {
		return strings.Contains(c.StableKey, ".plugin.") || strings.Contains(c.StableKey, "plugin.")
	})
}

func collectTools(cards []Card) []string {
	var out []string
	for _, c := range cards {
		out = append(out, c.Attributes.Tools...)
		out = append(out, c.Attributes.Capabilities...)
		out = append(out, c.Scaffold.Bridge...)
	}
	return out
}

func attrNames(cards []Card, pick func(Card) []string) []string {
	var out []string
	for _, c := range cards {
		out = append(out, pick(c)...)
	}
	return out
}

func nodeNames(g Graph, typ string) []string {
	var out []string
	for _, n := range g.Nodes {
		if n.Type == typ {
			if strings.TrimSpace(n.Name) != "" {
				out = append(out, n.Name)
			} else {
				out = append(out, n.StableKey)
			}
		}
	}
	return out
}

func brokenLinks(g Graph) []string {
	ids := map[string]bool{}
	for _, n := range g.Nodes {
		if n.ID != "" {
			ids[n.ID] = true
		}
		if n.StableKey != "" {
			ids[n.StableKey] = true
		}
	}
	var out []string
	for _, e := range g.Edges {
		if ids[e.From] && ids[e.To] {
			continue
		}
		out = append(out, e.From+" → "+e.To)
	}
	return out
}

func nodeTypeLine(g Graph) string {
	counts := map[string]int{}
	for _, n := range g.Nodes {
		typ := n.Type
		if typ == "" {
			typ = "未标类型"
		}
		counts[typ]++
	}
	keys := make([]string, 0, len(counts))
	for k := range counts {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, fmt.Sprintf("%s %d", k, counts[k]))
	}
	return orNone(parts)
}

func architectureLine(ed Edition, features []Card) string {
	domains := map[string]int{}
	for _, c := range features {
		domains[c.Domain]++
	}
	keys := make([]string, 0, len(domains))
	for k := range domains {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var parts []string
	for _, k := range keys {
		label := k
		if label == "" {
			label = "未分域"
		}
		parts = append(parts, fmt.Sprintf("%s %d", label, domains[k]))
	}
	return fmt.Sprintf("域节点 %d，模块节点 %d，边 %d。功能卡按域：%s。节点类型：%s。域节点：%s。模块节点：%s。",
		len(nodeNames(ed.Graph, "Domain")), len(nodeNames(ed.Graph, "Module")), len(ed.Graph.Edges),
		orNone(parts), nodeTypeLine(ed.Graph), orNone(nodeNames(ed.Graph, "Domain")), orNone(nodeNames(ed.Graph, "Module")))
}

func wiringSection(cards []Card) string {
	bridges, missingBridges, plugins, missingPlugins, findings := collectWiring(cards)
	var bare []string
	for _, f := range findings {
		if strings.Contains(f.Evidence, "没有桥方法") {
			bare = append(bare, f.Evidence)
		}
	}
	var skills, mcps int
	for _, c := range cards {
		if strings.Contains(c.StableKey, ".skill.") || strings.HasSuffix(c.StableKey, ".skill") {
			skills++
		}
		if strings.Contains(c.StableKey, ".mcp.") || strings.HasSuffix(c.StableKey, ".mcp") {
			mcps++
		}
	}
	var b strings.Builder
	b.WriteString("### 逐条核对\n\n")
	fmt.Fprintf(&b, "桥方法核对 %d 处，未接通 %d 处：%s。\n已接通的桥方法：%s。\n", len(uniqueSorted(bridges)), len(uniqueSorted(missingBridges)), orNone(missingBridges), orNone(without(bridges, missingBridges)))
	fmt.Fprintf(&b, "插件核对 %d 个，不在运行名单 %d 个：%s。\n已在运行名单：%s。\n", len(uniqueSorted(plugins)), len(uniqueSorted(missingPlugins)), orNone(missingPlugins), orNone(without(plugins, missingPlugins)))
	fmt.Fprintf(&b, "技能卡 %d，MCP 卡 %d。没有写上桥方法：%s。\n", skills, mcps, orNone(bare))
	b.WriteString("对得上处理函数的链路改写成这次从源码读到的调用。对不上的手写步骤不作为链路。带点入口对到桥方法名单，插件对到运行名单。\n\n")
	return b.String()
}

const unwrittenStepName = "未按真实调用写清"

func clarifyTemplateChains(cards []Card) []Card {
	hops, ready := traceCalls(cards)
	out := make([]Card, len(cards))
	copy(out, cards)
	for i := range out {
		traced := hopsForCard(out[i], hops, ready)
		if len(traced) > 0 {
			out[i].Chain = chainFromHops(traced)
			continue
		}
		if len(out[i].Chain.Steps) == 0 {
			continue
		}
		out[i].Chain = unwrittenChain(out[i])
	}
	return out
}

func hopsForCard(c Card, hops map[string]callHop, ready bool) []callHop {
	if !ready {
		return nil
	}
	var traced []callHop
	seen := map[string]bool{}
	for _, method := range claimedBridges(c) {
		hop := hops[method]
		if hop.Handler == "" || seen[method] {
			continue
		}
		seen[method] = true
		traced = append(traced, hop)
	}
	return traced
}

func chainFromHops(hops []callHop) Chain {
	var steps []Step
	for _, hop := range hops {
		desc := "这次从源码读到的处理函数。"
		if hop.Dispatch {
			desc = "按工具名分派，不把函数内部分支串成一条链路。"
		}
		steps = append(steps, Step{
			Index: len(steps) + 1, Name: hop.Handler, Detail: hop.Method,
			Description: desc,
		})
		if hop.Dispatch {
			continue
		}
		for _, name := range hop.Steps {
			steps = append(steps, Step{
				Index: len(steps) + 1, Name: name, Detail: hop.Method,
				Description: "这次从源码读到的后续调用。",
			})
		}
	}
	return closedChain(steps, len(steps),
		"步骤只写这次从源码读到的调用",
		"没读到的调用不写进步骤",
		"下次重新检查再读源码",
		"对不上处理函数的卡片仍标步骤未写清",
	)
}

func unwrittenChain(c Card) Chain {
	detail := "无处理函数"
	if methods := claimedBridges(c); len(methods) > 0 {
		detail = methods[0]
	}
	return closedChain([]Step{{
		Index: 1, Name: unwrittenStepName, Detail: detail,
		Description: "这次没有对上处理函数，手写步骤不作为链路。",
	}}, 1,
		"对上处理函数之后再写成源码里的调用",
		"没有读到处理函数",
		"下次重新检查再读源码",
		"保持未写清，不把手写步骤当成已经追通",
	)
}

func unwrittenChainNames(cards []Card) []string {
	return namesWhere(cards, func(c Card) bool {
		return len(c.Chain.Steps) > 0 && c.Chain.Steps[0].Name == unwrittenStepName
	})
}

func scopeSections(ed Edition, features []Card, started time.Time) string {
	bridges, missingBridges, plugins, missingPlugins, wiring := collectWiring(features)
	wired := len(uniqueSorted(without(bridges, missingBridges)))
	bridgeN := len(uniqueSorted(bridges))
	pluginOK := len(uniqueSorted(without(plugins, missingPlugins)))
	pluginN := len(uniqueSorted(plugins))
	unwritten := unwrittenChainNames(features)
	featureNodes := len(nodeNames(ed.Graph, "Feature"))
	pluginNodes := len(nodeNames(ed.Graph, "Plugin"))
	pluginCards := len(pluginFeatureNames(features))
	hops, ready := traceCalls(features)
	var b strings.Builder
	b.WriteString(callSection(features, hops, ready))
	fmt.Fprintf(&b, "### 速度与效率\n\n%s组装这份诊断用了 %s。这不是产品速度。实测 %d/%d。入口接通 %d/%d。插件在运行名单 %d/%d。后续调用按这次读到的源码核对。\n\n",
		probeBudgetLines(ed.Findings), time.Since(started).Round(time.Millisecond), ed.LiveProbe.Passed, ed.LiveProbe.Total, wired, bridgeN, pluginOK, pluginN)
	fmt.Fprintf(&b, "### 准确\n\n这一轮已经跑过的是 %d/%d。写着尚未跑完的任务不记成通过。功能卡 %d，图谱 Feature 节点 %d。%s插件功能卡 %d，图谱 Plugin 节点 %d。%s\n\n",
		ed.LiveProbe.Passed, ed.LiveProbe.Total, len(features), featureNodes, countMatch(len(features), featureNodes), pluginCards, pluginNodes, countMatch(pluginCards, pluginNodes))
	fmt.Fprintf(&b, "### 能力\n\n对不上的入口就是走不通，也是这一轮看到的能力不足。未接通：%s。不在运行名单的插件：%s。没有写上桥方法：%s。\n\n",
		orNone(missingBridges), orNone(missingPlugins), orNone(bareEvidence(wiring)))
	fmt.Fprintf(&b, "### 合理性\n\n未按真实调用写清 %d 张，不能当成已经走通。断边 %d 条。未接通入口 %d 处。入口覆盖分不是实测分。\n\n",
		len(unwritten), len(brokenLinks(ed.Graph)), len(uniqueSorted(missingBridges)))
	fmt.Fprintf(&b, "### 数据\n\n方法 %d，步骤 %d，缺原理 %d，缺逻辑 %d，缺分析 %d，空脚手架 %d。域和节点类型在架构一节。\n\n",
		countMethods(features), countSteps(features), countEmpty(features, func(c Card) bool { return strings.TrimSpace(c.Principle) == "" }), countEmpty(features, func(c Card) bool { return strings.TrimSpace(c.Logic) == "" }), countEmpty(features, func(c Card) bool { return strings.TrimSpace(c.Analysis) == "" }), len(emptyScaffoldNames(features)))
	b.WriteString(upgradeText(ed, features, hops, ready, wired, bridgeN, len(unwritten)))
	fmt.Fprintf(&b, "### 任务完成\n\n%s\n\n", taskLines(features, wiring, ed.Findings, hops, ready))
	fmt.Fprintf(&b, "### 宕机\n\n%s\n\n", faultLine(ed.Findings, "PH_L11", "本轮日志里没有新的宕机。"))
	fmt.Fprintf(&b, "### 卡壳\n\n%s\n\n", faultLine(ed.Findings, "PH_L13", "本轮日志里没有新的卡壳。"))
	fmt.Fprintf(&b, "### 完整\n\n这一节列出本版组装里的功能、链路、工具、技能、MCP、插件、资产、项目、开发能力、任务处理、脚手架、架构、读回、日志和竞品。没有对上处理函数的步骤，包括手写步骤，没有按真实调用写清。临时库读回不是本机键鼠、麦克风、真实供应商或真实进程的实测。空着的类别在各自小节写成「本版组装里没有单独列出来」。\n\n")
	fmt.Fprintf(&b, "### 问题、错误、BUG\n\n%s\n\n", problemLine(ed.Findings))
	return b.String()
}

func callSection(cards []Card, hops map[string]callHop, ready bool) string {
	var b strings.Builder
	b.WriteString("### 真实调用\n\n")
	if !ready {
		b.WriteString("本机没有读到处理函数注册表。这一轮只核对了桥方法名单。\n\n")
		return b.String()
	}
	var lines []string
	for _, c := range cards {
		for _, method := range claimedBridges(c) {
			lines = append(lines, c.Name+"："+hopText(hops[method]))
		}
	}
	if len(lines) == 0 {
		b.WriteString("这一版功能卡没有带点入口。\n\n")
		return b.String()
	}
	b.WriteString(strings.Join(lines, "\n"))
	b.WriteString("\n后续调用写在上面，定义以这次读到的源码为准。\n\n")
	return b.String()
}

func hopText(hop callHop) string {
	if hop.Handler == "" {
		return hop.Method + " 没有处理函数"
	}
	if hop.Dispatch {
		return hop.Method + " → " + hop.Handler + "，按工具名分派，不把函数内部分支串成一条链路"
	}
	branch := "注册表对上了这个处理函数"
	if hop.Branch {
		branch = "方法分支在"
	}
	text := hop.Method + " → " + hop.Handler + "，" + branch
	if len(hop.Steps) > 0 {
		text += "；后续调用：" + strings.Join(hop.Steps, "、")
	}
	if len(hop.Missing) > 0 {
		text += "；没有定义：" + strings.Join(hop.Missing, "、") + "，任务走不通，任务完成不了"
	}
	return text
}

func probeBudgetLines(findings []Finding) string {
	specs := []struct {
		code   string
		title  string
		budget time.Duration
	}{
		{"PH_L01", "听写", 25 * time.Second},
		{"PH_L02", "播放", 8 * time.Second},
		{"PH_L03", "下载", 20 * time.Second},
		{"PH_L04", "图片识别", 18 * time.Second},
	}
	var lines []string
	for _, spec := range specs {
		var hit *Finding
		for i := range findings {
			if findings[i].ErrorCode == spec.code {
				hit = &findings[i]
				break
			}
		}
		if hit == nil {
			lines = append(lines, spec.title+"这一轮没有记下耗时")
			continue
		}
		m := probeDuration.FindStringSubmatch(hit.Evidence)
		if m == nil {
			lines = append(lines, spec.title+"这一轮没有记下耗时")
			continue
		}
		d, err := time.ParseDuration(m[1])
		if err != nil {
			lines = append(lines, spec.title+"这一轮没有记下耗时")
			continue
		}
		if d > spec.budget {
			lines = append(lines, fmt.Sprintf("%s耗时 %s，超过探测预算 %s", spec.title, d, spec.budget))
			continue
		}
		lines = append(lines, fmt.Sprintf("%s耗时 %s，在探测预算 %s 内", spec.title, d, spec.budget))
	}
	return strings.Join(lines, "。") + "。"
}

var probeDuration = regexp.MustCompile(`耗时 (\d+(?:\.\d+)?(?:ns|us|µs|ms|s|m|h))`)

func upgradeText(ed Edition, features []Card, hops map[string]callHop, ready bool, wired, bridgeN, templates int) string {
	var gaps []string
	for _, f := range ed.Findings {
		if f.ErrorCode == "PH_L02" && isOpenFinding(f.Status) {
			gaps = append(gaps, "先修这次的播放")
		}
	}
	if ready {
		seen := map[string]bool{}
		for _, c := range features {
			for _, method := range claimedBridges(c) {
				if seen[method] || hops[method].Handler != "" {
					continue
				}
				seen[method] = true
				gaps = append(gaps, method+" 没有处理函数，接通或从卡片去掉后再检查")
			}
		}
	}
	var b strings.Builder
	fmt.Fprintf(&b, "### 升级对照\n\n本产品这一轮：读回 %d/%d，入口接通 %d/%d，未按真实调用写清 %d 张。对照产品：%s",
		ed.LiveProbe.Passed, ed.LiveProbe.Total, wired, bridgeN, templates, landscapeOrNone(ed.Findings))
	if len(gaps) == 0 {
		b.WriteString("这一轮没有测出要先修的缺口。已保存的对照照录，不据此编升级。不编对手分数。\n\n")
		return b.String()
	}
	b.WriteString("升级：" + strings.Join(gaps, "；") + "。已保存的对照只作背景，不编对手分数。\n\n")
	return b.String()
}

func countMatch(a, b int) string {
	if a == b {
		return "数量一致。"
	}
	return "数量不一致，数据还不准。"
}

func bareEvidence(findings []Finding) []string {
	var out []string
	for _, f := range findings {
		if strings.Contains(f.Evidence, "没有桥方法") {
			out = append(out, f.Evidence)
		}
	}
	return out
}

func landscapeOrNone(findings []Finding) string {
	names := landscapeNames(findings)
	if len(names) == 0 {
		return "还没有选定对照产品。"
	}
	return strings.Join(names, "；") + "。"
}

func taskLines(cards []Card, wiring, findings []Finding, hops map[string]callHop, ready bool) string {
	blocked := map[string]bool{}
	for _, f := range wiring {
		if f.ErrorCode == "PH_021" || f.ErrorCode == "PH_022" {
			blocked[f.StableKey] = true
		}
	}
	var lines []string
	for _, c := range cards {
		if !isTaskCard(c) {
			continue
		}
		if len(claimedBridges(c)) == 0 && len(c.Methods) == 0 {
			lines = append(lines, c.Name+"：没有入口，任务走不通，任务完成不了")
			continue
		}
		var parts []string
		if ready {
			for _, method := range claimedBridges(c) {
				hop := hops[method]
				if run, ok := runFinding(c, method, findings); ok {
					ev := strings.TrimSpace(run.Evidence)
					switch {
					case strings.HasPrefix(ev, "不代跑"):
						parts = append(parts, ev)
					case strings.HasPrefix(ev, "入口已跑到"):
						parts = append(parts, ev)
					case run.Status == "pass" && strings.HasPrefix(ev, "已跑完"):
						parts = append(parts, ev)
					case run.Status == "pass":
						parts = append(parts, "已跑完："+ev)
					default:
						parts = append(parts, "任务完成不了："+ev)
					}
					continue
				}
				if hop.Handler == "" {
					parts = append(parts, method+" 没有处理函数，任务走不通，任务完成不了")
					continue
				}
				if len(hop.Missing) > 0 {
					parts = append(parts, hopText(hop)+"，任务完成不了")
					continue
				}
				parts = append(parts, hopText(hop)+"；尚未跑完")
			}
		}
		if len(parts) == 0 && blocked[c.StableKey] {
			parts = append(parts, "入口未接通，任务走不通，任务完成不了")
		}
		if len(parts) == 0 && len(c.Methods) > 0 {
			parts = append(parts, "菜单入口已写上，没有带点调用")
		}
		if len(parts) == 0 {
			parts = append(parts, "没有入口，任务走不通，任务完成不了")
		}
		lines = append(lines, c.Name+"："+strings.Join(parts, "；"))
	}
	for _, f := range findings {
		if !isOpenFinding(f.Status) {
			continue
		}
		switch f.ErrorCode {
		case "PH_L01", "PH_L02", "PH_L03", "PH_L04":
			lines = append(lines, f.Title+"：实测没有完成，任务完成不了")
		}
	}
	if len(lines) == 0 {
		return "本版没有单独的任务卡。"
	}
	return strings.Join(lines, "。") + "。"
}

func runFinding(c Card, method string, findings []Finding) (Finding, bool) {
	for _, f := range findings {
		if f.Status == "note" || f.ErrorCode == "PH_L99" || f.ErrorCode == "PH_021" || f.ErrorCode == "PH_022" {
			continue
		}
		if f.StableKey == "probe."+method || (f.StableKey == c.StableKey && strings.HasPrefix(f.ErrorCode, "PH_L")) {
			return f, true
		}
	}
	return Finding{}, false
}

func problemLine(findings []Finding) string {
	var lines []string
	for _, f := range findings {
		if f.ErrorCode == "PH_L99" || f.ErrorCode == "PH_000" || !isOpenFinding(f.Status) {
			continue
		}
		kind := "问题"
		if f.Severity == "error" {
			kind = "错误"
		}
		lines = append(lines, kind+" "+f.ErrorCode+" "+f.Title+"："+f.Evidence)
	}
	if len(lines) == 0 {
		return "这一轮快照里没有未关闭的问题条目。这不是说产品没有别的问题。"
	}
	return strings.Join(lines, "；") + "。"
}

func faultLine(findings []Finding, code, empty string) string {
	var lines []string
	for _, f := range findings {
		if f.ErrorCode == code && isOpenFinding(f.Status) {
			lines = append(lines, f.Evidence)
		}
	}
	if len(lines) == 0 {
		return empty
	}
	return strings.Join(lines, "；") + "。"
}

func countMethods(cards []Card) int {
	n := 0
	for _, c := range cards {
		n += len(c.Methods)
	}
	return n
}

func countSteps(cards []Card) int {
	n := 0
	for _, c := range cards {
		n += len(c.Chain.Steps)
	}
	return n
}

func countEmpty(cards []Card, empty func(Card) bool) int {
	n := 0
	for _, c := range cards {
		if empty(c) {
			n++
		}
	}
	return n
}

func without(all, drop []string) []string {
	skip := map[string]bool{}
	for _, name := range drop {
		skip[name] = true
	}
	var out []string
	for _, name := range all {
		if !skip[name] {
			out = append(out, name)
		}
	}
	return out
}

func probeLine(ed Edition) string {
	if ed.LiveProbe.Total == 0 {
		return "本轮没有重跑听写、播放、下载和图片识别。当前分数是入口覆盖，不是这四项的实测。点重新检测才会留下实测。"
	}
	return fmt.Sprintf("本轮读回 %d/%d。这个数字含临时库读回，不是本机键鼠、麦克风、真实供应商或真实进程的实测。听写、播放、下载、图片识别以这次重跑为准。未通过的列在下面。", ed.LiveProbe.Passed, ed.LiveProbe.Total)
}

func openProbeNames(findings []Finding) []string {
	var out []string
	for _, f := range findings {
		if !isOpenFinding(f.Status) {
			continue
		}
		switch f.ErrorCode {
		case "PH_L01", "PH_L02", "PH_L03", "PH_L04":
			out = append(out, f.Title)
		}
	}
	return out
}

func logLine(findings []Finding) string {
	if len(logNames(findings)) == 0 {
		return "本版快照里没有从引擎日志对上的宕机、卡壳、反复调用或流程不通。点重新检测会再读今天的引擎日志。"
	}
	return "下面这些来自今天的引擎日志。执行净化会再读日志：这句不在了才改为已修复。"
}

func logNames(findings []Finding) []string {
	var out []string
	for _, f := range findings {
		if isLogFinding(f) {
			out = append(out, f.Title)
		}
	}
	return out
}

func isLogFinding(f Finding) bool {
	switch f.ErrorCode {
	case "PH_L10", "PH_L11", "PH_L12", "PH_L13", "PH_L14", "PH_L15", "PH_L16", "PH_L17", "PH_L18":
		return true
	default:
		return strings.HasPrefix(f.StableKey, "log.")
	}
}

func landscapeLine(findings []Finding) string {
	if len(landscapeNames(findings)) == 0 {
		return "尚未在图景页选择产品。对照只引用图景页已经保存的结论，诊断不另排名次。"
	}
	return "下面只引用图景页已经保存的结论，不另排名次，也不计入健康度。"
}

func landscapeNames(findings []Finding) []string {
	var out []string
	for _, f := range findings {
		if f.ErrorCode == "PH_L90" || f.ErrorCode == "PH_L91" {
			out = append(out, f.Title+"："+f.Evidence)
		}
	}
	return out
}

func orNone(items []string) string {
	items = uniqueSorted(items)
	if len(items) == 0 {
		return "无"
	}
	return strings.Join(items, "、")
}

func uniqueSorted(in []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(in))
	for _, s := range in {
		s = strings.TrimSpace(s)
		if s == "" || seen[s] {
			continue
		}
		seen[s] = true
		out = append(out, s)
	}
	sort.Strings(out)
	return out
}
