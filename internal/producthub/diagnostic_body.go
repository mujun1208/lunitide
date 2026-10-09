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
	// 模型融合行只在注入了供应商/模型配置时出现：未注入环境与旧报告保持零漂移。
	if audit := AuditModelFusion(); len(audit.Slots) > 0 {
		writeNamed(&b, "模型融合", modelFusionLine(audit), modelFusionNames(audit))
	}
	b.WriteString(wiringSection(features))
	writeNamed(&b, "实测", probeLine(ed), nonemptyOrNil(openProbeNames(ed.Findings)))
	writeNamed(&b, "日志与故障", logLine(ed.Findings), nonemptyOrNil(logNames(ed.Findings)))
	writeNamed(&b, "竞品对照", landscapeLine(ed.Findings), nonemptyOrNil(landscapeNames(ed.Findings)))
	writeNamed(&b, "升级对照", upgradeLine(ed.Findings), nonemptyOrNil(upgradeNames(ed.Findings)))
	b.WriteString(scopeSections(ed, features, started))
	return b.String()
}

// gapPlan is one five-part optimization recommendation: problem, location,
// root cause, action, acceptance. Every part comes from this run's cards,
// findings, or call hops; a part with no source stays unsaid rather than
// being invented.
type gapPlan struct {
	Problem string
	Locate  string
	Root    string
	Action  string
	Accept  string
}

func (p gapPlan) String() string {
	var b strings.Builder
	fmt.Fprintf(&b, "- 【问题】%s。", p.Problem)
	if p.Locate != "" {
		fmt.Fprintf(&b, "定位：%s。", p.Locate)
	}
	if p.Root != "" {
		fmt.Fprintf(&b, "根因：%s。", p.Root)
	}
	if p.Action != "" {
		fmt.Fprintf(&b, "修复动作：%s。", p.Action)
	}
	if p.Accept != "" {
		fmt.Fprintf(&b, "验收：%s。", p.Accept)
	}
	b.WriteString("\n")
	return b.String()
}

// envFindingCodes are findings whose fix sits on the machine or the store,
// not in the product code: install a runtime, unlock the hub, or supply the
// missing card.
var envFindingCodes = map[string]string{
	"PH_L01": "本机补齐精识别运行时后重测；这一条不改产品代码",
	"PH_012": "先解锁产品知识中枢，再重跑诊断",
	"PH_018": "按活源补齐或退役这张功能卡，再重跑诊断",
}

func diagnosticGapPlans(ed Edition) string {
	features := productFeatures(ed.Features)
	hops, ready := traceCalls(features)
	var plans []gapPlan
	for _, c := range features {
		if len(c.Chain.Steps) == 0 {
			p := gapPlan{
				Problem: "链路步骤还不清楚",
				Locate:  gapLocate(c, hops, ready),
				Root:    "这张卡还没有落成链路步骤",
				Action:  "按该功能的真实调用补上步骤。净化不会编造步骤，也不会改产品代码",
				Accept:  "重写后步骤全部来自这次源码读到的调用，占位步骤不再出现",
			}
			if f, ok := openFindingFor(ed.Findings, c.StableKey); ok {
				p.Root = firstNonEmpty(f.RootCause, f.Evidence, p.Root)
				p.Accept = firstNonEmpty(f.Verify, p.Accept)
			}
			plans = append(plans, p)
			continue
		}
		if c.Chain.Steps[0].Name == unwrittenStepName {
			detail := "无处理函数"
			if methods := claimedBridges(c); len(methods) > 0 {
				detail = methods[0]
			}
			p := gapPlan{
				Problem: "步骤未按真实调用写清",
				Locate:  gapLocate(c, hops, ready) + "，入口 " + detail,
				Root:    "这次没有对上处理函数，手写步骤不作为链路",
				Action:  "对上处理函数之后按源码里的调用重写；诊断不会编造步骤",
				Accept:  "下次重新检查读到源码后，这张卡的步骤不再是未写清占位",
			}
			if f, ok := openFindingFor(ed.Findings, c.StableKey); ok {
				p.Root = firstNonEmpty(f.RootCause, p.Root)
				p.Accept = firstNonEmpty(f.Verify, p.Accept)
			}
			plans = append(plans, p)
		}
	}
	if unread := unreadSourceNames(features); len(unread) > 0 {
		plans = append(plans, gapPlan{
			Problem: fmt.Sprintf("这次运行的程序旁边没有产品源码，%d 张链路没有按源码重写", len(unread)),
			Locate:  fmt.Sprintf("%d 张链路", len(unread)),
			Root:    "安装副本没带源码，链路保持编译时写入的调用。这不是这些功能没对上处理函数",
			Action:  "在带源码的机器上重新生成这一版；发布构建可把源码根目录注入程序",
			Accept:  "源码读到后链路按真实调用重写，源码占位步骤清零",
		})
	}
	for _, link := range uniqueSorted(brokenLinks(ed.Graph)) {
		plans = append(plans, gapPlan{
			Problem: "链路没有接上",
			Locate:  link,
			Root:    "图谱边的一端不在节点里",
			Action:  "补上缺失节点或删掉这条边。净化不会猜一条新链路",
			Accept:  "图谱每条边的两端都在节点里",
		})
	}
	for _, c := range features {
		if len(c.Methods) > 0 {
			continue
		}
		p := gapPlan{
			Problem: "没有入口方法",
			Locate:  gapLocate(c, hops, ready),
			Root:    "目录里这张卡没有写入口",
			Action:  "目录里已有默认入口的，执行净化直接补上；没有默认入口的保持待处理",
			Accept:  "入口方法补上后，PH_021 复核这条入口接通",
		}
		if f, ok := openFindingFor(ed.Findings, c.StableKey); ok {
			p.Root = firstNonEmpty(f.RootCause, p.Root)
			p.Action = firstNonEmpty(f.Fix, p.Action)
			p.Accept = firstNonEmpty(f.Verify, p.Accept)
		}
		plans = append(plans, p)
	}
	plans = append(plans, envGapPlans(ed.Findings)...)
	plans = append(plans, modelGapPlans(ed.Findings)...)
	if len(plans) == 0 {
		return ""
	}
	var b strings.Builder
	for _, p := range plans {
		b.WriteString(p.String())
	}
	b.WriteString("\n")
	return b.String()
}

func envGapPlans(findings []Finding) []gapPlan {
	var plans []gapPlan
	for _, f := range findings {
		advice, env := envFindingCodes[f.ErrorCode]
		if !env || !isOpenFinding(f.Status) {
			continue
		}
		plans = append(plans, gapPlan{
			Problem: "环境未就绪：" + f.Title,
			Locate:  f.StableKey,
			Root:    firstNonEmpty(f.RootCause, f.Evidence, "未记录"),
			Action:  advice,
			Accept:  "按建议处理本机环境后重跑这一项，探测转为 pass 才算收口",
		})
	}
	return plans
}

// modelGapPlans turns open model-fusion findings into five-part upgrade
// plans. PH_M02 is the model-upgrade follow path: the analysis goes through
// 执行净化 (the internal model gets the registry profile and the affected
// feature slots), the switch itself stays a confirmed user action.
func modelGapPlans(findings []Finding) []gapPlan {
	var plans []gapPlan
	for _, f := range findings {
		if !isOpenFinding(f.Status) {
			continue
		}
		switch f.ErrorCode {
		case "PH_M01":
			plans = append(plans, gapPlan{
				Problem: "模型槽位未配齐：" + f.Title,
				Locate:  f.StableKey,
				Root:    firstNonEmpty(f.RootCause, f.Evidence, "未记录"),
				Action:  firstNonEmpty(f.Fix, "在模型供应商页配置该槽位模型"),
				Accept:  "重新检测后该槽位有可用模型，本条消失",
			})
		case "PH_M02":
			plans = append(plans, gapPlan{
				Problem: "模型有新一代：" + f.Title,
				Locate:  f.StableKey,
				Root:    "产品档案库登记了演进路径，供应商配置仍停在上一代",
				Action:  "点「执行净化」：内部模型按档案与受影响槽位深度分析新旧能力差异并产出切换方案；确认方案后在供应商页把默认模型换成新一代，再点「重新检测」",
				Accept:  "换新一代并重新检测后本条消失，供应商连通测试通过",
			})
		case "PH_M03":
			plans = append(plans, gapPlan{
				Problem: "模型上下文窗口未登记",
				Locate:  f.StableKey,
				Root:    "供应商配置里该模型的 contextWindow 为空",
				Action:  firstNonEmpty(f.Fix, "按厂商文档补上上下文窗口"),
				Accept:  "补上窗口值并重新检测后本条消失",
			})
		case "PH_M04":
			plans = append(plans, gapPlan{
				Problem: "模型供应商不可用",
				Locate:  f.StableKey,
				Root:    firstNonEmpty(f.RootCause, f.Evidence, "未记录"),
				Action:  firstNonEmpty(f.Fix, "启用供应商并配置凭据"),
				Accept:  "供应商可用后重新检测，本条消失",
			})
		}
	}
	return plans
}

// releaseTaskBook renders the shipping task book that carries a purified
// edition into a released update. Stage names follow the fixed ship
// pipeline; scripts are named by repository-relative path only and no
// machine path is ever written into the report.
func releaseTaskBook(ed Edition) string {
	var fixed []string
	applied, planned := 0, 0
	for _, f := range ed.Findings {
		switch f.Status {
		case "fixed":
			fixed = append(fixed, f.ErrorCode+" "+f.Title)
		case "applied":
			applied++
		case "planned":
			planned++
		}
	}
	var b strings.Builder
	b.WriteString("### 发布任务书\n\n")
	if len(fixed) == 0 {
		b.WriteString("本轮还没有复查通过的修复。先执行净化：复查通过才记 fixed，这里才会列出随版发布的收口清单。\n\n")
	} else {
		fixed = uniqueSorted(fixed)
		fmt.Fprintf(&b, "本轮已复查通过、可随版发布 %d 条：%s。\n\n", len(fixed), strings.Join(fixed, "；"))
	}
	if applied > 0 || planned > 0 {
		fmt.Fprintf(&b, "另有已执行待复查 %d 条、已立方案待修复 %d 条，不进本次发版口径。\n\n", applied, planned)
	}
	b.WriteString("发版按固定流程顺序执行，不通过就停在当阶段修，不跳阶段：\n")
	b.WriteString("0. 现状勘察（只读）：git 状态与 diff、VERSION、远端 release、并发写入判断。\n")
	b.WriteString("1. 复盘复核未提交改动：逐文件读全文 diff、同类扫描、生成物一致性核对。\n")
	b.WriteString("2. 修正：按复盘结论改，改完立刻跑对应窄闸门；生成物只重生成不手改。\n")
	b.WriteString("3. 全量本地闸门：桥契约、编目、typecheck、前端测试与构建、go vet、go build、覆盖率、golangci-lint、govulncheck、npm audit、排除集测试、生成物零漂移，全绿才继续。\n")
	b.WriteString("4. 版本与发布说明：VERSION 是唯一真源，tag = v<VERSION>；发布说明写给用户，写清不改什么。\n")
	b.WriteString("5. 提交 + 打 tag：按明确文件清单暂存，禁止整目录暂存；tag 指向提交里的 VERSION 与 tag 名一致。\n")
	b.WriteString("6. 签名构建：release/Build-Release.ps1 -RequireSignature；构建前设 LUNITIDE_SOURCE_ROOT 指向本机源码根，把自净化源码锁定注入程序；产出安装包、latest.json、SHA256SUMS.txt 三资产。\n")
	b.WriteString("7. 发布为 Latest：用本机签名产物创建 release，三资产齐备并标 Latest。\n")
	b.WriteString("8. 推送：推分支与 tag；直连不可达时走 Git Data API 重放脚本，逐步校验 SHA。\n")
	b.WriteString("9. 验证 Latest：tagName、isLatest、三资产、latest.json 的版本号与安装包哈希全部核对一致。\n")
	b.WriteString("10. 清理：远端只保留最新 3 个 release（删除前列清单确认）；删本地产物与工作树；绝不删 git tag。\n")
	b.WriteString("应用内更新读 latest.json 自动升级。装好新版后重跑诊断，复查通过才记 fixed，闭环到这里完成。\n\n")
	return b.String()
}

func openFindingFor(findings []Finding, key string) (Finding, bool) {
	for _, f := range findings {
		if f.StableKey == key && isOpenFinding(f.Status) {
			return f, true
		}
	}
	return Finding{}, false
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

// gapLocate names the card and, when this run traced real calls, the source
// file or handler behind its first bridge method.
func gapLocate(c Card, hops map[string]callHop, ready bool) string {
	loc := fmt.Sprintf("%s（%s）", cardLabel(c), c.StableKey)
	if !ready {
		return loc
	}
	methods := claimedBridges(c)
	if len(methods) == 0 {
		return loc
	}
	hop := hops[methods[0]]
	if hop.Source != "" {
		return loc + "，源码 " + hop.Source
	}
	if hop.Handler != "" {
		return loc + "，处理函数 " + hop.Handler
	}
	return loc
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
		_, ok := pluginRosterID(c.StableKey)
		return ok
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
	return applyCallChains(cards, hops, ready)
}

const unreadSourceStepName = "未读到源码"

func applyCallChains(cards []Card, hops map[string]callHop, ready bool) []Card {
	out := make([]Card, len(cards))
	copy(out, cards)
	for i := range out {
		if strings.HasPrefix(out[i].StableKey, "landscape.") {
			continue
		}
		if !ready {
			if len(out[i].Chain.Steps) == 0 {
				continue
			}
			out[i].Chain = unreadSourceChain()
			continue
		}
		traced := hopsForCard(out[i], hops, true)
		if len(traced) > 0 {
			out[i].Chain = chainFromHops(traced)
			continue
		}
		if chain, ok := entryChain(out[i]); ok {
			out[i].Chain = chain
			continue
		}
		if len(out[i].Chain.Steps) == 0 {
			continue
		}
		out[i].Chain = unwrittenChain(out[i])
	}
	return out
}

func unreadSourceChain() Chain {
	return closedChain([]Step{{
		Index: 1, Name: unreadSourceStepName, Detail: "源码",
		Description: "这次运行的程序旁边没有产品源码，这条链路没有按源码重写。这不是功能没写。",
	}}, 1,
		"没有读到源码",
		"没有重写链路",
		"下次在源码旁边重新检查",
		"不把没读到源码写成没有处理函数",
	)
}

func unreadSourceNames(cards []Card) []string {
	return namesWhere(cards, func(c Card) bool {
		return len(c.Chain.Steps) > 0 && c.Chain.Steps[0].Name == unreadSourceStepName
	})
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
	var validates, failures, undefined []string
	for _, hop := range hops {
		desc := "这次从源码读到的处理函数。"
		if hop.Dispatch {
			desc = "按工具名分派，不把函数内部分支串成一条链路。"
		} else if hop.Source != "" {
			desc = "这次从源码读到的处理函数，源码位置 " + hop.Source + "。"
		}
		steps = append(steps, Step{
			Index: len(steps) + 1, Name: hop.Handler, Detail: hop.Method,
			Description: desc,
		})
		if hop.Dispatch {
			continue
		}
		for _, name := range hop.Steps {
			d := "这次从源码读到的后续调用。"
			switch callRole(name) {
			case roleValidate:
				d = "校验调用，参数不合法走失败分支。"
				validates = append(validates, name)
			case roleFailure:
				d = "失败路径，判定错误并返回给调用方。"
				failures = append(failures, name)
			case roleResource:
				d = "资源调用，控制这次调用的时限和释放。"
			case roleSuccess:
				d = "成功返回，把结果返回给调用方。"
			}
			steps = append(steps, Step{
				Index: len(steps) + 1, Name: name, Detail: hop.Method,
				Description: d,
			})
		}
		undefined = append(undefined, hop.Missing...)
	}
	success, failure := chainBranchText(validates, failures, undefined)
	return closedChain(steps, len(steps), success, failure,
		"下次重新检查再读源码",
		"对不上处理函数的卡片仍标步骤未写清",
	)
}

// chainBranchText 用这次读到的校验、失败路径和没有定义的调用写成功与失败分支，
// 不写与这条链路无关的样板句子。
func chainBranchText(validates, failures, undefined []string) (success, failure string) {
	success = "校验和业务调用走完，正常返回。"
	if len(validates) > 0 {
		success = "校验（" + strings.Join(dedupNames(validates), "、") + "）通过后走完业务调用，正常返回。"
	}
	failure = "这次读到的源码里没有写单独的失败分支。"
	if len(failures) > 0 {
		failure = "失败路径（" + strings.Join(dedupNames(failures), "、") + "）把错误返回给调用方。"
	}
	if len(undefined) > 0 {
		failure += "没有定义的调用：" + strings.Join(dedupNames(undefined), "、") + "，这条链路走不通。"
	}
	return success, failure
}

func dedupNames(in []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(in))
	for _, s := range in {
		if s == "" || seen[s] {
			continue
		}
		seen[s] = true
		out = append(out, s)
	}
	return out
}

func entryChain(c Card) (Chain, bool) {
	if len(claimedBridges(c)) > 0 {
		return Chain{}, false
	}
	if c.ChainClass == "page-enter" || strings.Contains(c.StableKey, ".page.") {
		return closedChain([]Step{{
			Index: 1, Name: "进入页面", Detail: "导航",
			Description: "这一卡只说明从导航进入该页，没有带点调用。不记成没有处理函数。",
		}}, 1, "页面能打开", "入口被隐藏或首屏失败", "再从导航进入", "停在当前页"), true
	}
	if c.ChainClass == "settings-toggle" || c.Module == "settings" {
		return closedChain([]Step{{
			Index: 1, Name: "设置项", Detail: "设置",
			Description: "这一卡只改这项设置，没有带点调用。不记成没有处理函数。",
		}}, 1, "设置已保存", "设置没有写上", "再打开这项设置", "保持原来的值"), true
	}
	if id, ok := pluginRosterID(c.StableKey); ok {
		return closedChain([]Step{{
			Index: 1, Name: "运行名单", Detail: id,
			Description: "这个插件在运行名单里，没有单独的带点桥方法。不记成没有处理函数。",
		}}, 1, "插件已在运行名单", "运行名单里没有这个插件", "再对运行名单", "不把插件卡写成没有处理函数"), true
	}
	if strings.HasSuffix(c.StableKey, ".barge-in") {
		return closedChain([]Step{{
			Index: 1, Name: "语音会话", Detail: "插话",
			Description: "插话发生在已经开始的语音会话里，没有单独的带点桥方法。不记成没有处理函数。",
		}}, 1, "插话打断当前播报", "没有正在进行的语音会话", "先开始语音会话", "不把插话写成没有处理函数"), true
	}
	return Chain{}, false
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
		b.WriteString("这次运行的程序旁边没有产品源码，没有重写链路。这不是功能没对上处理函数。这一轮只核对了桥方法名单。\n\n")
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
	if compiledTrace(hops) {
		b.WriteString("\n后续调用写在上面。这版程序编译时从源码写入的调用。\n\n")
	} else {
		b.WriteString("\n后续调用写在上面，定义以这次读到的源码为准。\n\n")
	}
	return b.String()
}

func compiledTrace(hops map[string]callHop) bool {
	for _, hop := range hops {
		if hop.FromBuild {
			return true
		}
	}
	return false
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
	text := hop.Method + " → " + hop.Handler
	if hop.Source != "" {
		text += "（" + hop.Source + "）"
	}
	text += "，" + branch
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
		if d >= spec.budget && (strings.Contains(hit.Evidence, "deadline exceeded") || strings.Contains(hit.Evidence, "没有在时限内")) {
			lines = append(lines, fmt.Sprintf("%s耗时 %s，用满探测预算 %s，没有在时限内完成", spec.title, d, spec.budget))
			continue
		}
		lines = append(lines, fmt.Sprintf("%s耗时 %s，在探测预算 %s 内", spec.title, d, spec.budget))
	}
	return strings.Join(lines, "。") + "。"
}

var probeDuration = regexp.MustCompile(`耗时 (\d+(?:\.\d+)?(?:ns|us|µs|ms|s|m|h))`)

func upgradeText(ed Edition, features []Card, hops map[string]callHop, ready bool, wired, bridgeN, templates int) string {
	var gaps []string
	for _, f := range liveRepairGaps(ed.Findings) {
		gap := fmt.Sprintf("先修这次的%s（%s）", f.Title, f.ErrorCode)
		if fix := strings.TrimSpace(f.Fix); fix != "" {
			gap += "：" + fix
		}
		gaps = append(gaps, gap)
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

// liveRepairGaps lists this run's failed live probes as the ordered
// fix-first list for the upgrade section: errors before warnings, then by
// error code. Landscape notes and the watermark never qualify.
func liveRepairGaps(findings []Finding) []Finding {
	var out []Finding
	for _, f := range findings {
		if !isOpenFinding(f.Status) || f.ErrorCode == "PH_L99" || !strings.HasPrefix(f.ErrorCode, "PH_L") {
			continue
		}
		if f.Severity != "error" && f.Severity != "warn" {
			continue
		}
		out = append(out, f)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Severity != out[j].Severity {
			return out[i].Severity == "error"
		}
		return out[i].ErrorCode < out[j].ErrorCode
	})
	return out
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
			if _, roster := pluginRosterID(c.StableKey); roster {
				parts = append(parts, "这个插件在运行名单里，没有带点调用")
			} else {
				parts = append(parts, "菜单入口已写上，没有带点调用")
			}
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
			if f.ErrorCode == "PH_L01" && dictationRuntimeMissing(f.Evidence) {
				lines = append(lines, f.Title+"：精识别运行时没装上，不是流式听写已经坏了")
				continue
			}
			if f.ErrorCode == "PH_L03" && localModelLengthTimeout(f.Evidence) {
				lines = append(lines, f.Title+"：本机模型目录已核对，这次没在时限内拿到服务器文件长度，不是下载已经坏了")
				continue
			}
			lines = append(lines, f.Title+"：实测没有完成，任务完成不了")
		}
	}
	if len(lines) == 0 {
		return "本版没有单独的任务卡。"
	}
	return strings.Join(lines, "。") + "。"
}

func dictationRuntimeMissing(evidence string) bool {
	return strings.Contains(evidence, "not installed") || strings.Contains(evidence, "没装")
}

func localModelLengthTimeout(evidence string) bool {
	return strings.Contains(evidence, "本机模型目录已核对") && strings.Contains(evidence, "没有在时限内拿到服务器文件长度")
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
		return "本轮没有重跑听写、播放、下载和图片识别。当前健康分只计真实问题；活源覆盖单列，不是这四项的实测。点重新检测才会留下实测。"
	}
	extra := ""
	if reached, skipped := probeStateCounts(ed.Findings); reached > 0 || skipped > 0 {
		extra = fmt.Sprintf("另有入口已跑到 %d 项（处理函数返回了，入口活着，但功能没有完整跑完，不计入读回，也不扣分）、不代跑 %d 项（会开窗、占麦克风、安装、联网或执行命令，诊断不代执行，要到对应页面人工核验，不扣分）。", reached, skipped)
	}
	return fmt.Sprintf("本轮读回 %d/%d，这是覆盖率，单列不计分；健康分只扣探测失败和日志故障。%s这个数字含临时库读回，不是本机键鼠、麦克风、真实供应商或真实进程的实测。听写、播放、下载、图片识别以这次重跑为准。未通过的列在下面。", ed.LiveProbe.Passed, ed.LiveProbe.Total, extra)
}

// probeStateCounts tallies the two in-between probe rows: reached (the handler
// returned but the function did not complete) and skipped (the probe would open
// a window, take the microphone, install, go online, or run a command). They
// occupy the measured total but never count as a read-back pass.
func probeStateCounts(findings []Finding) (reached, skipped int) {
	for _, f := range findings {
		if !strings.HasPrefix(f.StableKey, "probe.") {
			continue
		}
		switch f.Status {
		case "reached":
			reached++
		case "skipped":
			skipped++
		}
	}
	return reached, skipped
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

func upgradeLine(findings []Finding) string {
	if len(upgradeNames(findings)) == 0 {
		return "这一版没有生成升级对照。生成诊断报告时才合并竞品确认摘录与本产品缺口。"
	}
	return "竞品确认摘录 × 本产品缺口。摘录必须人工确认过才引用；两侧都不另编。"
}

func upgradeNames(findings []Finding) []string {
	var out []string
	for _, f := range findings {
		if f.ErrorCode == "PH_L92" {
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
