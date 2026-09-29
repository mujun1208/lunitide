package producthub

import (
	"context"
	"fmt"
	"strings"
)

// landscapeAxisOrder fixes the report order of the four comparison axes.
var landscapeAxisOrder = []string{"local", "media", "assets", "hub"}

// UpgradeFindings builds the 升级对照 notes: confirmed competitor quotes
// (each with its source URL and date) set against this product's own gap list
// from the same edition. The merge never invents the competitor side — only
// human-confirmed drafts are quoted — and without any confirmed quote the
// section degrades to pointing at the local optimization plans.
func UpgradeFindings(drafts []LandscapeDraft, ed Edition) []Finding {
	var confirmed []LandscapeDraft
	for _, d := range drafts {
		if d.Status == "confirmed" {
			confirmed = append(confirmed, d)
		}
	}
	if len(confirmed) == 0 {
		return []Finding{finding("info", "PH_L92", "landscape.upgrade.none", "升级对照",
			"还没有人工确认的竞品摘录。升级方向只来自本产品诊断的优化方案，不拿未确认的采集内容当依据。",
			"采集草稿必须先在图景页人工确认；确认过的摘录才会进入升级对照。",
			"先在图景页点「采集」并确认摘录，再重新生成诊断报告。",
			"升级对照出现带来源与日期的竞品摘录后，本条消失。", "note")}
	}
	gaps := gapSummary(ed)
	byAxis := map[string][]LandscapeDraft{}
	for _, d := range confirmed {
		if _, ok := landscapeAxes[d.Axis]; ok {
			byAxis[d.Axis] = append(byAxis[d.Axis], d)
		}
	}
	out := make([]Finding, 0, len(landscapeAxisOrder))
	for _, axis := range landscapeAxisOrder {
		group := byAxis[axis]
		if len(group) == 0 {
			continue
		}
		var quotes []string
		for _, d := range group {
			quotes = append(quotes, fmt.Sprintf("%s「%s」（%s %s）", d.Name, d.Quote, d.URL, d.Date))
		}
		out = append(out, finding("info", "PH_L92", "landscape.upgrade."+axis, "升级对照 · "+landscapeAxes[axis],
			fmt.Sprintf("竞品确认摘录：%s × 本产品缺口：%s。", strings.Join(quotes, "；"), gaps),
			"竞品侧只引用人工确认过的原文摘录（带来源与日期）；本产品侧来自这一版诊断的缺口清单，两侧都不另编。",
			"把差距写进下一轮优化方案：按摘录指向的公开行为核对本产品在这一维的实现。",
			"本产品在这一维补齐后，重新采集并人工确认最新摘录，再重新生成对照。", "note"))
	}
	return out
}

// gapSummary compresses this edition's own gap list into one line every axis
// can be set against. Every count comes from the edition being rendered.
func gapSummary(ed Edition) string {
	features := productFeatures(ed.Features)
	unclear := len(emptyChainNames(features))
	unwritten := len(unwrittenChainNames(features))
	nomethod := len(missingMethodNames(features))
	broken := len(brokenLinks(ed.Graph))
	open := len(openProbeNames(ed.Findings))
	if unclear+unwritten+nomethod+broken+open == 0 {
		return "本版没有待修缺口（链路与实测全部通过），升级对照只作前瞻"
	}
	return fmt.Sprintf("步骤还不清楚 %d 张、未按真实调用写清 %d 张、没有入口方法 %d 张、链路没有接上 %d 条、实测未通过 %d 项",
		unclear, unwritten, nomethod, broken, open)
}

// confirmedLandscape loads the human-confirmed drafts for the upgrade merge.
// A load failure yields nothing rather than blocking the report.
func (s *Service) confirmedLandscape(ctx context.Context) []LandscapeDraft {
	if s == nil {
		return nil
	}
	all, err := s.persist.ProductHubLoadLandscapeDrafts(ctx)
	if err != nil {
		return nil
	}
	return all
}
