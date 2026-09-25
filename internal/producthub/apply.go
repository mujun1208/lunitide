package producthub

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/oklog/ulid/v2"
)

func (s *Service) SetCollaborator(c Collaborator) {
	if s != nil {
		s.collab = c
	}
}

func (s *Service) Apply(ctx context.Context, errorCode, stableKey string) (ApplyResult, error) {
	ed, err := s.requireEdition(ctx)
	if err != nil {
		return ApplyResult{}, err
	}
	if errorCode == "wont_fix" {
		code, key, ok := strings.Cut(stableKey, "|")
		if !ok || code == "" || key == "" {
			return ApplyResult{}, ErrNotFound
		}
		return s.markWontFix(ctx, &ed, code, key)
	}
	if strings.TrimSpace(errorCode) == "" && strings.TrimSpace(stableKey) == "" {
		return s.applyAll(ctx, ed)
	}
	return s.applyOne(ctx, &ed, errorCode, stableKey)
}

func (s *Service) markWontFix(ctx context.Context, ed *Edition, errorCode, stableKey string) (ApplyResult, error) {
	if _, ok := findFinding(ed.Findings, errorCode, stableKey); !ok {
		return ApplyResult{}, ErrNotFound
	}
	now := time.Now().UTC().Format(time.RFC3339)
	for i := range ed.Findings {
		if ed.Findings[i].ErrorCode == errorCode && ed.Findings[i].StableKey == stableKey {
			ed.Findings[i].Status = "wont_fix"
			ed.Findings[i].AppliedAt = now
			ed.Findings[i].Plan = "人工标记 wont_fix。不调用技能，不改 Go/TS。"
		}
	}
	findings, _, score := refreshFindings(*ed)
	ed.Findings = findings
	ed.HealthScore = score
	ed.ReportMarkdown, ed.ReportHTML = RenderReport(*ed)
	if err := s.persist.ProductHubSaveEdition(ctx, *ed); err != nil {
		return ApplyResult{}, err
	}
	return ApplyResult{
		OK: true, Applied: true, Count: 1, Status: "wont_fix",
		Plan:      "已记为 wont_fix。下次打开仍对照活源，但这条不再当成未处理。",
		ErrorCode: errorCode, StableKey: stableKey,
	}, nil
}

func (s *Service) applyAll(ctx context.Context, ed Edition) (ApplyResult, error) {
	var open []Finding
	for _, f := range ed.Findings {
		if isOpenFinding(f.Status) {
			open = append(open, f)
		}
	}
	if len(open) == 0 {
		return ApplyResult{OK: true, Applied: true, Count: 0, Status: "applied", Plan: "没有待处理诊断。"}, nil
	}
	advice := s.consult(ctx, combinedPrompt(open))
	applied := 0
	last := ApplyResult{OK: true, SkillID: advice.SkillID, SkillName: advice.SkillName, SkillOutput: advice.Output}
	for _, f := range open {
		res, err := s.applyFinding(ctx, &ed, f, advice)
		if err != nil {
			return ApplyResult{}, err
		}
		last = res
		last.Count = 0
		if res.Applied || res.Status == "planned" || res.Status == "applied" {
			applied++
		}
	}
	last.OK = true
	last.Count = applied
	last.SkillID = advice.SkillID
	last.SkillName = advice.SkillName
	last.SkillOutput = advice.Output
	if last.Plan == "" {
		last.Plan = fmt.Sprintf("已处理 %d 条诊断，并交给内部技能 %s。", applied, emptyText(advice.SkillName, "本地方案"))
	}
	return last, nil
}

func (s *Service) applyOne(ctx context.Context, ed *Edition, errorCode, stableKey string) (ApplyResult, error) {
	f, ok := findFinding(ed.Findings, errorCode, stableKey)
	if !ok {
		return ApplyResult{}, ErrNotFound
	}
	advice := s.consult(ctx, f.ApplyPrompt)
	return s.applyFinding(ctx, ed, f, advice)
}

func (s *Service) applyFinding(ctx context.Context, ed *Edition, f Finding, advice ConsultResult) (ApplyResult, error) {
	card, _ := cardByKey(ed.Features, f.StableKey)
	plan := localPlan(f, card)
	if advice.Output != "" {
		plan = plan + "\n\n内部技能 / 模型：\n" + advice.Output
	}
	// Every branch below sets both, so there is no default to fall back on.
	var status string
	applied := false
	var liveEvidence, liveFix string
	if strings.HasPrefix(f.ErrorCode, "PH_L") {
		var evidence, fix string
		status, applied, evidence, fix = recheckLive(ctx, f)
		plan = fix
		liveEvidence, liveFix = evidence, fix
		advice = ConsultResult{}
	} else {
		switch f.ErrorCode {
		case "PH_014":
			methods := defaultMethods(emptyText(card.ChainClass, "crud-bridge"))
			_ = s.persist.ProductHubSaveEnrichment(ctx, Enrichment{
				StableKey: f.StableKey,
				Summary:   "自净化补入口方法",
				Methods:   methods,
				Tags:      []string{"status:已补入口"},
			})
			_ = s.persist.ProductHubSaveTag(ctx, NodeTag{StableKey: f.StableKey, Vocab: "status", Value: "已补入口", AssignedBy: "manual"})
			for i := range ed.Features {
				if ed.Features[i].StableKey == f.StableKey {
					ed.Features[i].Methods = methods
					ed.Features[i].Tags = appendUnique(ed.Features[i].Tags, "status:已补入口")
				}
			}
			status, applied = "applied", true
		case "PH_016":
			_ = s.persist.ProductHubSaveTag(ctx, NodeTag{StableKey: f.StableKey, Vocab: "status", Value: "弃用", AssignedBy: "manual"})
			status, applied = "applied", true
		case "PH_000":
			status, applied = "applied", true
		default:
			_ = s.persist.ProductHubSaveTag(ctx, NodeTag{StableKey: f.StableKey, Vocab: "status", Value: "待修复", AssignedBy: "manual"})
			status = "planned"
		}
	}
	now := time.Now().UTC().Format(time.RFC3339)
	rec := ApplyLog{
		ID: ulid.Make().String(), ErrorCode: f.ErrorCode, StableKey: f.StableKey,
		Plan: plan, SkillID: advice.SkillID, SkillName: advice.SkillName, SkillOutput: advice.Output,
		Status: status, CreatedAt: now,
	}
	if err := s.persist.ProductHubSaveApply(ctx, rec); err != nil {
		return ApplyResult{}, err
	}
	for i := range ed.Findings {
		if ed.Findings[i].ErrorCode == f.ErrorCode && ed.Findings[i].StableKey == f.StableKey {
			ed.Findings[i].Status = status
			ed.Findings[i].Plan = plan
			if liveEvidence != "" {
				ed.Findings[i].Evidence = liveEvidence
			}
			if liveFix != "" {
				ed.Findings[i].Fix = liveFix
			}
			if status == "fixed" {
				ed.Findings[i].Severity = "info"
			}
			ed.Findings[i].AppliedAt = now
			if !strings.HasPrefix(f.ErrorCode, "PH_L") {
				ed.Findings[i].SkillID = advice.SkillID
				ed.Findings[i].SkillName = advice.SkillName
				ed.Findings[i].SkillOutput = advice.Output
			}
			ed.Findings[i].ApplyPrompt = applyPrompt(ed.Findings[i])
		}
	}
	if probe, ok := probeFromFindings(ed.Findings); ok {
		ed.LiveProbe = probe
	}
	findings, _, score := refreshFindings(*ed)
	ed.Findings = findings
	ed.HealthScore = score
	ed.ReportMarkdown, ed.ReportHTML = RenderReport(*ed)
	if err := s.persist.ProductHubSaveEdition(ctx, *ed); err != nil {
		return ApplyResult{}, err
	}
	return ApplyResult{
		OK: true, Applied: applied, Count: 1, Status: status, Plan: plan,
		SkillID: advice.SkillID, SkillName: advice.SkillName, SkillOutput: advice.Output,
		ErrorCode: f.ErrorCode, StableKey: f.StableKey,
	}, nil
}

func (s *Service) consult(ctx context.Context, prompt string) ConsultResult {
	if s == nil || s.collab == nil || strings.TrimSpace(prompt) == "" {
		return ConsultResult{Output: "未挂载内部技能。已使用中枢本地方案，可在技能中心发布 debugger / code-reviewer / implement 后重试。"}
	}
	out, err := s.collab.Consult(ctx, prompt)
	if err != nil || strings.TrimSpace(out.Output) == "" {
		if strings.TrimSpace(out.Output) == "" {
			out.Output = "内部技能咨询未返回正文。已使用中枢本地方案。"
		}
		return out
	}
	return out
}

func localPlan(f Finding, card Card) string {
	var b strings.Builder
	fmt.Fprintf(&b, "【本地方案】%s · %s\n问题：%s\n证据：%s\n根因：%s\n方案：%s\n验证：%s\n",
		f.ErrorCode, f.StableKey, f.Title, f.Evidence, f.RootCause, f.Fix, f.Verify)
	if card.StableKey != "" {
		fmt.Fprintf(&b, "脚手架：页面 %s；Bridge %s；运行时 %s。\n",
			join(card.Scaffold.Pages), join(card.Scaffold.Bridge), join(card.Scaffold.Runtime))
	}
	switch f.ErrorCode {
	case "PH_014":
		b.WriteString("中枢动作：按链路族写入默认入口方法，并打 status:已补入口。\n")
	case "PH_016":
		b.WriteString("中枢动作：保留弃用标记，记为已处理，不删除种子讲解。\n")
	case "PH_000":
		b.WriteString("中枢动作：无需改代码。\n")
	case "PH_019", "PH_020":
		b.WriteString("中枢动作：只提示重新检测。不生成新卡，不改 Go/TS，不循环调用生成。\n")
	default:
		b.WriteString("中枢动作：打 status:待修复，把任务书交给内部技能/模型，不自动改 Go/TS。\n")
	}
	return strings.TrimSpace(b.String())
}

func combinedPrompt(findings []Finding) string {
	var b strings.Builder
	b.WriteString("【Lunitide 批量自我净化】\n")
	for _, f := range findings {
		fmt.Fprintf(&b, "- %s %s %s\n", f.ErrorCode, f.StableKey, f.Title)
	}
	b.WriteString("请配合产品内部模型与技能输出可落地补丁计划。中枢会完成本地目录修复（标签/入口方法/状态）。\n")
	return b.String()
}

func findFinding(in []Finding, code, key string) (Finding, bool) {
	for _, f := range in {
		if f.ErrorCode == code && (key == "" || f.StableKey == key) {
			return f, true
		}
	}
	return Finding{}, false
}

func cardByKey(cards []Card, key string) (Card, bool) {
	for _, c := range cards {
		if c.StableKey == key {
			return c, true
		}
	}
	return Card{}, false
}

func isOpenFinding(status string) bool {
	return status == "" || status == "open" || status == "regressed"
}

func isResolvedFinding(status string) bool {
	switch status {
	case "applied", "fixed", "wont_fix":
		return true
	default:
		return false
	}
}

func mergeFindingStatus(prev, next []Finding) []Finding {
	prevBy := map[string]Finding{}
	for _, f := range prev {
		prevBy[f.ErrorCode+"|"+f.StableKey] = f
	}
	for i, f := range next {
		if f.ErrorCode == "PH_000" {
			continue
		}
		// A new live result wins. An older「待修复」tag must not hide a probe that just failed again.
		if strings.HasPrefix(f.ErrorCode, "PH_L") && (f.Status == "open" || f.Status == "pass") {
			continue
		}
		p, ok := prevBy[f.ErrorCode+"|"+f.StableKey]
		if !ok {
			continue
		}
		if isResolvedFinding(p.Status) || p.Status == "planned" {
			next[i].Status = p.Status
			next[i].Plan = p.Plan
			next[i].AppliedAt = p.AppliedAt
			next[i].SkillID = p.SkillID
			next[i].SkillName = p.SkillName
			next[i].SkillOutput = p.SkillOutput
		}
	}
	return next
}

func attachApplies(findings []Finding, logs []ApplyLog) []Finding {
	latest := map[string]ApplyLog{}
	for _, rec := range logs {
		latest[rec.ErrorCode+"|"+rec.StableKey] = rec
	}
	for i, f := range findings {
		rec, ok := latest[f.ErrorCode+"|"+f.StableKey]
		if !ok {
			continue
		}
		if f.Status == "open" || f.Status == "" {
			// A fresh live failure stays open. An older fixed or planned log must not paint over it.
			if strings.HasPrefix(f.ErrorCode, "PH_L") && rec.Status != "open" && rec.Status != "wont_fix" {
				continue
			}
			findings[i].Status = rec.Status
		}
		if findings[i].Plan == "" {
			findings[i].Plan = rec.Plan
		}
		if findings[i].SkillOutput == "" {
			findings[i].SkillID = rec.SkillID
			findings[i].SkillName = rec.SkillName
			findings[i].SkillOutput = rec.SkillOutput
			findings[i].AppliedAt = rec.CreatedAt
		}
	}
	return findings
}

func applyEnrichments(cards []Card, ens []Enrichment) []Card {
	by := map[string]Enrichment{}
	for _, e := range ens {
		if e.StableKey != "" {
			by[e.StableKey] = e
		}
	}
	for i, c := range cards {
		e, ok := by[c.StableKey]
		if !ok {
			continue
		}
		if e.Summary != "" && c.Summary == "" {
			cards[i].Summary = e.Summary
		}
		if len(e.Methods) > 0 {
			cards[i].Methods = e.Methods
		}
		for _, tag := range e.Tags {
			cards[i].Tags = appendUnique(cards[i].Tags, tag)
		}
	}
	return cards
}
