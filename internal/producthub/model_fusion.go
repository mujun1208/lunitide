package producthub

import (
	"fmt"
	"sort"
	"strings"
	"sync"
)

// ── 注入：当前模型配置快照 ──────────────────────────────────────────────────

// ModelSlot is one configured model row the app layer hands to the hub at
// entry (the same injection pattern as SetBridgeMethods): provider identity,
// model identity, kind slot, capability fields. The hub audits model fusion
// without importing the engine or the provider package.
type ModelSlot struct {
	ProviderID      string `json:"providerId"`
	ProviderName    string `json:"providerName"`
	Protocol        string `json:"protocol"`
	ModelID         string `json:"modelId"`
	DisplayName     string `json:"displayName"`
	Kind            string `json:"kind"`
	IsDefault       bool   `json:"isDefault"`
	KindDefault     bool   `json:"kindDefault"`
	ContextWindow   int64  `json:"contextWindow"`
	SupportsVision  bool   `json:"supportsVision"`
	Status          string `json:"status"`
	CredentialState string `json:"credentialState"`
}

var (
	modelSlotsMu sync.Mutex
	modelSlots   []ModelSlot
)

// SetModelSlots registers the current provider/model configuration. An empty
// slice unregisters so tests can restore package state.
func SetModelSlots(slots []ModelSlot) {
	sorted := make([]ModelSlot, 0, len(slots))
	sorted = append(sorted, slots...)
	sort.Slice(sorted, func(i, j int) bool {
		if sorted[i].ProviderID != sorted[j].ProviderID {
			return sorted[i].ProviderID < sorted[j].ProviderID
		}
		return sorted[i].ModelID < sorted[j].ModelID
	})
	modelSlotsMu.Lock()
	modelSlots = sorted
	modelSlotsMu.Unlock()
}

// RegisteredModelSlots returns a copy of the injected model configuration.
func RegisteredModelSlots() []ModelSlot {
	modelSlotsMu.Lock()
	defer modelSlotsMu.Unlock()
	return append([]ModelSlot(nil), modelSlots...)
}

// slotUsable reports whether the slot's provider can actually serve traffic.
func slotUsable(s ModelSlot) bool {
	return s.Status == "enabled" && s.CredentialState == "configured"
}

// ── 内置模型档案库（声明式数据）──────────────────────────────────────────────

// ModelProfile is one entry of the hub's built-in model registry. The registry
// is product data, not vendor truth: capability numbers stay unset unless the
// product decided to record them, and the upgrade path (Successor) is an
// explicit declaration — the hub never guesses version ordering. Shipping a
// product update that edits this table IS how the product follows model
// upgrades: the next diagnosis compares the user's configured models against
// the refreshed registry and surfaces the upgrade plan.
type ModelProfile struct {
	Family        string
	ModelID       string
	DisplayName   string
	ContextWindow int64    // 0 = 未登记，以厂商文档为准
	Capabilities  []string // 已确认的能力标签；空 = 未登记
	Successor     string   // 显式后继模型 ID；空 = 档案里当前最新
	Notes         string
}

// builtinModelProfiles is the shipped registry. GLM 与 DeepSeek 的演进路径按
// 产品声明维护：GLM-5.3 的下一代为 GLM-5，DeepSeek 4 Pro 的下一代为 5 Pro。
// 厂商发布新一代时随产品发版更新本表，诊断报告即自动给出升级分析。
var builtinModelProfiles = []ModelProfile{
	{Family: "glm", ModelID: "glm-5.3", DisplayName: "GLM-5.3", Successor: "glm-5",
		Notes: "产品档案登记的演进路径：GLM-5.3 的下一代为 GLM-5。能力细节以厂商文档为准。"},
	{Family: "glm", ModelID: "glm-5", DisplayName: "GLM-5",
		Notes: "GLM 在产品档案里登记的最新一代。"},
	{Family: "glm", ModelID: "glm-4.5v", DisplayName: "GLM-4.5V", Capabilities: []string{"vision"},
		Notes: "视觉与 GUI 接地型号。"},
	{Family: "deepseek", ModelID: "deepseek-4-pro", DisplayName: "DeepSeek 4 Pro", Successor: "deepseek-5-pro",
		Notes: "产品档案登记的演进路径：DeepSeek 4 Pro 的下一代为 5 Pro。"},
	{Family: "deepseek", ModelID: "deepseek-5-pro", DisplayName: "DeepSeek 5 Pro",
		Notes: "DeepSeek 在产品档案里登记的最新一代。"},
	{Family: "deepseek", ModelID: "deepseek-chat", DisplayName: "DeepSeek Chat"},
	{Family: "deepseek", ModelID: "deepseek-reasoner", DisplayName: "DeepSeek Reasoner", Capabilities: []string{"reasoning"}},
}

// FindModelProfile looks one model ID up in the shipped registry.
func FindModelProfile(modelID string) (ModelProfile, bool) {
	for _, p := range builtinModelProfiles {
		if p.ModelID == modelID {
			return p, true
		}
	}
	return ModelProfile{}, false
}

// kindFeatures names the product capabilities each model kind slot unlocks.
// The audit quotes this so a missing slot is tied to concrete features, not
// abstract labels.
var kindFeatures = []struct{ kind, note string }{
	{"llm", "月伴对话、专家会诊、办公任务生成、会议摘要"},
	{"vision", "看图理解、截图分析、图片识别辅助"},
	{"gui", "电脑控制接地（截图→点击坐标）"},
	{"image", "文生图"},
	{"video", "文生视频"},
	{"embedding", "语义检索与记忆"},
	{"asr", "语音转写（听写、会议转写）"},
	{"tts", "语音播报（月伴出声）"},
}

// coreModelKinds are the slots on the conversation/voice main chain: a missing
// one is a warn, the rest surface as info.
var coreModelKinds = map[string]bool{"llm": true, "asr": true, "tts": true}

// ── 融合审计 ────────────────────────────────────────────────────────────────

// KindCoverageRow is one model-kind slot's coverage state.
type KindCoverageRow struct {
	Kind    string
	Covered bool
	Count   int      // 可用供应商上该 kind 的模型数
	Owners  []string // 提供该 kind 的供应商名
}

// ModelUpgrade is one detected "your model has a next generation" event.
type ModelUpgrade struct {
	ProviderID    string
	ProviderName  string
	CurrentID     string
	NextID        string
	Kinds         []string // 该模型在该供应商上承担的槽位
	CurrentNote   string
	NextKnown     bool // 后继模型是否已在档案库登记
	NextNote      string
}

// FusionAudit is the full model-fusion picture the diagnosis reports.
type FusionAudit struct {
	Slots         []ModelSlot
	KindCoverage  []KindCoverageRow
	Upgrades      []ModelUpgrade
	UnknownModels []string // 未在产品档案库登记的模型 ID（诚实列出，不编造能力）
	Providers     int
	Usable        int // 可用供应商数（启用 + 凭据已配置）
}

// AuditModelFusion compares the injected configuration against the shipped
// registry. With no injection (older callers, tests) everything stays empty.
func AuditModelFusion() FusionAudit {
	slots := RegisteredModelSlots()
	var a FusionAudit
	a.Slots = slots
	byProvider := map[string][]ModelSlot{}
	providerNames := map[string]string{}
	usableProviders := map[string]bool{}
	for _, s := range slots {
		if _, ok := byProvider[s.ProviderID]; !ok {
			a.Providers++
			providerNames[s.ProviderID] = s.ProviderName
		}
		byProvider[s.ProviderID] = append(byProvider[s.ProviderID], s)
		if slotUsable(s) {
			usableProviders[s.ProviderID] = true
		}
		if _, known := FindModelProfile(s.ModelID); !known && !hasStr(a.UnknownModels, s.ModelID) {
			a.UnknownModels = append(a.UnknownModels, s.ModelID)
		}
	}
	a.Usable = len(usableProviders)
	// Kind coverage: a slot counts only on a usable provider.
	for _, kf := range kindFeatures {
		row := KindCoverageRow{Kind: kf.kind}
		for _, s := range slots {
			if s.Kind != kf.kind || !slotUsable(s) {
				continue
			}
			row.Count++
			if !hasStr(row.Owners, s.ProviderName) {
				row.Owners = append(row.Owners, s.ProviderName)
			}
		}
		row.Covered = row.Count > 0
		a.KindCoverage = append(a.KindCoverage, row)
	}
	// Upgrades are configuration-driven: the ACTIVE model of a kind slot —
	// the kind-default, falling back to the provider default — on a usable
	// provider has a registered successor AND that successor already appears
	// in the provider's configured model list (added by the user or pulled in
	// by model.sync). Without the new generation actually configured there is
	// nothing to compare and no upgrade finding fires. Merely listing the new
	// generation is not adopting it: the finding clears only after the switch.
	for pid, group := range byProvider {
		activeByKind := map[string]string{}
		configured := make(map[string]bool, len(group))
		for _, s := range group {
			configured[s.ModelID] = true
		}
		for _, s := range group {
			if slotUsable(s) && s.KindDefault {
				activeByKind[s.Kind] = s.ModelID
			}
		}
		for _, s := range group {
			if !slotUsable(s) || s.KindDefault || !s.IsDefault {
				continue
			}
			if _, ok := activeByKind[s.Kind]; !ok {
				activeByKind[s.Kind] = s.ModelID
			}
		}
		for _, kf := range kindFeatures {
			currentID, ok := activeByKind[kf.kind]
			if !ok {
				continue
			}
			profile, known := FindModelProfile(currentID)
			if !known || profile.Successor == "" {
				continue
			}
			// 新一代没有进入供应商配置：无从分析新旧差异，不报升级。
			if !configured[profile.Successor] {
				continue
			}
			next, nextKnown := FindModelProfile(profile.Successor)
			a.Upgrades = append(a.Upgrades, ModelUpgrade{
				ProviderID: pid, ProviderName: providerNames[pid],
				CurrentID: currentID, NextID: profile.Successor, Kinds: []string{kf.kind},
				CurrentNote: profile.Notes, NextKnown: nextKnown, NextNote: next.Notes,
			})
		}
	}
	sort.Slice(a.Upgrades, func(i, j int) bool {
		if a.Upgrades[i].ProviderName != a.Upgrades[j].ProviderName {
			return a.Upgrades[i].ProviderName < a.Upgrades[j].ProviderName
		}
		return a.Upgrades[i].CurrentID < a.Upgrades[j].CurrentID
	})
	sort.Strings(a.UnknownModels)
	return a
}

func hasStr(in []string, v string) bool {
	for _, s := range in {
		if s == v {
			return true
		}
	}
	return false
}

// ── 诊断发现 ────────────────────────────────────────────────────────────────

// fusionFindings turns the audit into PH_M findings. They ride the normal
// finding pipeline: the report lists them, gap plans give the five-part
// upgrade plan, and 执行净化 hands the analysis to the internal model.
func fusionFindings(a FusionAudit) []Finding {
	if len(a.Slots) == 0 {
		return nil
	}
	var out []Finding
	// PH_M01: model-kind slot without a usable model.
	for _, row := range a.KindCoverage {
		if row.Covered {
			continue
		}
		sev := "info"
		if coreModelKinds[row.Kind] {
			sev = "warn"
		}
		evidence := "当前供应商配置里没有启用且凭据可用的 " + row.Kind + " 类模型"
		if note := kindFeatureNote(row.Kind); note != "" {
			evidence += "；受影响功能：" + note
		}
		out = append(out, finding(sev, "PH_M01", "model.kind."+row.Kind,
			row.Kind+" 模型槽位没有可用配置",
			evidence,
			"该槽位没有配置默认模型，或所属供应商未启用/凭据未配置",
			"在模型供应商页为 "+row.Kind+" 槽位配置模型；配好后点「重新检测」。",
			"重新检测后该槽位有可用模型，本条消失", "open"))
	}
	// PH_M02: the next generation is configured while the active model is
	// still the previous one — only then is there something to analyze.
	for _, u := range a.Upgrades {
		evidence := fmt.Sprintf("供应商「%s」已配置新一代 %s，当前主力仍是 %s。涉及槽位：%s。",
			u.ProviderName, u.NextID, u.CurrentID, strings.Join(u.Kinds, "、"))
		if u.CurrentNote != "" {
			evidence += u.CurrentNote
		}
		if !u.NextKnown {
			evidence += "后继模型的档案尚未登记，能力细节以厂商文档为准。"
		}
		out = append(out, finding("info", "PH_M02", "model.upgrade."+dashedKey(u.ProviderID)+"."+dashedKey(u.CurrentID),
			"模型有新一代："+u.CurrentID+" → "+u.NextID,
			evidence,
			"新一代模型已进入供应商配置，但默认主力仍是上一代",
			"执行净化生成本次升级的同步方案（交给内部模型深度分析新旧能力差异、受影响功能与切换步骤）；确认后在供应商页把默认模型换成 "+u.NextID+"，再点「重新检测」。",
			"换成 "+u.NextID+" 并重新检测后本条消失；供应商连通测试通过", "open"))
	}
	// PH_M03: chat-grounding model without a recorded context window.
	for _, s := range a.Slots {
		if !slotUsable(s) || s.ContextWindow > 0 {
			continue
		}
		if s.Kind != "llm" && s.Kind != "vision" && s.Kind != "gui" {
			continue
		}
		out = append(out, finding("info", "PH_M03", "model.window."+dashedKey(s.ProviderID)+"."+dashedKey(s.ModelID),
			"模型上下文窗口未登记："+s.ModelID,
			fmt.Sprintf("供应商「%s」的 %s 没有登记上下文窗口，对话的 Token 预算按引擎兜底值计算。", s.ProviderName, s.ModelID),
			"供应商配置里该模型的 contextWindow 为空",
			"在供应商页按厂商文档补上该模型的上下文窗口；或执行净化由内部模型按厂商公开资料给出建议值再人工确认。",
			"补上窗口值并重新检测后本条消失", "open"))
	}
	// PH_M04: provider not usable.
	seenProvider := map[string]bool{}
	for _, s := range a.Slots {
		if slotUsable(s) || seenProvider[s.ProviderID] {
			continue
		}
		seenProvider[s.ProviderID] = true
		out = append(out, finding("warn", "PH_M04", "model.provider."+dashedKey(s.ProviderID),
			"模型供应商不可用："+s.ProviderName,
			fmt.Sprintf("状态 %s，凭据 %s，其下模型当前不可用。", statusWord(s.Status), credWord(s.CredentialState)),
			"供应商未启用或凭据未配置",
			"在模型供应商页启用该供应商并配置凭据。",
			"供应商可用后重新检测，本条消失", "open"))
	}
	return out
}

func statusWord(status string) string {
	if status == "enabled" {
		return "已启用"
	}
	if status == "disabled" {
		return "已停用"
	}
	if status == "" {
		return "未知"
	}
	return status
}

func credWord(state string) string {
	switch state {
	case "configured":
		return "已配置"
	case "missing":
		return "未配置"
	case "unavailable":
		return "不可用"
	case "requires_reentry":
		return "需重新录入"
	}
	if state == "" {
		return "未知"
	}
	return state
}

// dashedKey turns a dotted ID into a stable-key-safe dashed fragment.
func dashedKey(v string) string {
	return strings.ReplaceAll(strings.ReplaceAll(v, ".", "-"), " ", "-")
}

// ── 报告行 ──────────────────────────────────────────────────────────────────

// modelFusionLine is the diagnosis-report line for model fusion. Empty when no
// model configuration is injected, so older reports stay unchanged.
func modelFusionLine(a FusionAudit) string {
	if len(a.Slots) == 0 {
		return ""
	}
	var covered, missing []string
	for _, row := range a.KindCoverage {
		if row.Covered {
			covered = append(covered, row.Kind)
		} else {
			missing = append(missing, row.Kind)
		}
	}
	line := fmt.Sprintf("已接入供应商 %d 家（可用 %d）、模型 %d 个。槽位配齐：%s；未配：%s。",
		a.Providers, a.Usable, len(a.Slots), orNone(covered), orNone(missing))
	if n := len(a.UnknownModels); n > 0 {
		line += fmt.Sprintf("产品档案未登记 %d 个模型（能力以厂商文档为准，不编造）。", n)
	}
	if len(a.Upgrades) > 0 {
		var ups []string
		for _, u := range a.Upgrades {
			ups = append(ups, fmt.Sprintf("%s：%s → %s", u.ProviderName, u.CurrentID, u.NextID))
		}
		line += "可升级：" + strings.Join(ups, "；") + "。"
	} else {
		line += "配置中没有出现新一代模型，暂无可分析的升级。"
	}
	return line
}

// modelFusionNames lists the fusion rows worth naming under the line.
func modelFusionNames(a FusionAudit) []string {
	if len(a.Slots) == 0 {
		return nil
	}
	var names []string
	for _, u := range a.Upgrades {
		names = append(names, fmt.Sprintf("%s → %s（%s）", u.CurrentID, u.NextID, u.ProviderName))
	}
	for _, row := range a.KindCoverage {
		if !row.Covered {
			names = append(names, row.Kind+" 槽位未配")
		}
	}
	return names
}

// ── 净化任务书 ──────────────────────────────────────────────────────────────

// kindFeatureNote names the product capabilities a model-kind slot unlocks, so
// a missing slot or an upgrade is tied to concrete features, not an abstract
// label.
func kindFeatureNote(kind string) string {
	for _, kf := range kindFeatures {
		if kf.kind == kind {
			return kf.note
		}
	}
	return ""
}

// modelFusionPrompt builds the consultation brief for PH_M findings. PH_M02 is
// the upgrade deep-analysis task: both registry profiles, the affected slots,
// and the features each slot unlocked go in, and the deliverables are named up
// front so 执行净化 returns a usable switch plan.
func modelFusionPrompt(f Finding) string {
	a := AuditModelFusion()
	var b strings.Builder
	b.WriteString("【Lunitide 模型融合任务】\n")
	fmt.Fprintf(&b, "编号 %s  对象 %s  严重度 %s  状态 %s\n", f.ErrorCode, f.StableKey, f.Severity, f.Status)
	b.WriteString("问题：" + f.Title + "\n")
	b.WriteString("证据：" + f.Evidence + "\n")
	if strings.HasPrefix(f.StableKey, "model.upgrade.") {
		for _, u := range a.Upgrades {
			if "model.upgrade."+dashedKey(u.ProviderID)+"."+dashedKey(u.CurrentID) != f.StableKey {
				continue
			}
			writeUpgradeBrief(&b, u)
			return b.String()
		}
	}
	b.WriteString("请结合产品链路给出补齐该模型配置的具体建议与验证方法。不编造厂商未公开的能力；档案未登记的字段注明以厂商文档为准。\n")
	return b.String()
}

// writeUpgradeBrief assembles the PH_M02 brief from the shipped registry: what
// the current model is, what the successor is registered as, which slots and
// features the switch touches, and the five deliverables.
func writeUpgradeBrief(b *strings.Builder, u ModelUpgrade) {
	cur, _ := FindModelProfile(u.CurrentID)
	next, nextKnown := FindModelProfile(u.NextID)
	fmt.Fprintf(b, "\n供应商「%s」受影响槽位：%s。\n", u.ProviderName, strings.Join(u.Kinds, "、"))
	for _, kind := range u.Kinds {
		if note := kindFeatureNote(kind); note != "" {
			fmt.Fprintf(b, "- %s 槽位承载功能：%s\n", kind, note)
		}
	}
	fmt.Fprintf(b, "\n当前模型档案：%s（%s）｜上下文窗口：%s｜能力：%s｜%s\n",
		cur.ModelID, cur.DisplayName, windowWord(cur.ContextWindow), orNone(cur.Capabilities), cur.Notes)
	if nextKnown {
		fmt.Fprintf(b, "新一代模型档案：%s（%s）｜上下文窗口：%s｜能力：%s｜%s\n",
			next.ModelID, next.DisplayName, windowWord(next.ContextWindow), orNone(next.Capabilities), next.Notes)
	} else {
		fmt.Fprintf(b, "新一代模型 %s 尚未在产品档案库登记：请按厂商公开文档分析，未公开的能力注明待确认。\n", u.NextID)
	}
	b.WriteString("\n请深度分析本次模型升级并输出：\n")
	b.WriteString("1. 新旧模型能力差异（对照产品链路：对话、专家会诊、GUI 接地、多模态）\n")
	b.WriteString("2. 受影响的产品功能清单（按槽位逐一列出，含对话 Token 预算的变化）\n")
	b.WriteString("3. 同步升级方案：产品侧要跟进的配置与档案更新（供应商页换默认模型、contextWindow、能力标记）\n")
	b.WriteString("4. 验证方法：供应商连通测试通过；重新检测后 PH_M02 消失\n")
	b.WriteString("5. 回退方案：新一代异常时如何退回当前模型\n")
	b.WriteString("不编造厂商未公开的能力；档案未登记的字段以厂商文档为准。\n")
}

// windowWord keeps unrecorded registry numbers honest in prompts and plans.
func windowWord(w int64) string {
	if w <= 0 {
		return "未登记"
	}
	return fmt.Sprintf("%d", w)
}
