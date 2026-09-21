package producthub

import (
	"fmt"
	"strings"
)

func enrichCard(c Card) Card {
	if c.Name == "播放歌曲" && c.StableKey == "feature.dialog.music.play" {
		c.Name = "放歌"
	}
	c.Tags = appendUnique(c.Tags, "domain:"+c.Domain)
	if c.StableKey == "feature.dialog.music.play" {
		c.Tags = appendUnique(c.Tags, "alias:放歌")
	}
	if c.Principle == "" {
		c.Principle = fmt.Sprintf("链路族 %s。成功以核验为准，不把「已发送」当成功。底座：页面 %s；Bridge %s；运行时 %s。",
			emptyText(c.ChainClass, "crud-bridge"), join(c.Scaffold.Pages), join(c.Scaffold.Bridge), join(c.Scaffold.Runtime))
	}
	if c.Logic == "" {
		var b strings.Builder
		for _, st := range c.Chain.Steps {
			fmt.Fprintf(&b, "%d.%s（%s）→ ", st.Index, st.Name, st.Detail)
		}
		for _, br := range c.Chain.Branches {
			fmt.Fprintf(&b, "[%s：%s] ", br.Type, br.Description)
			if br.Retry != nil {
				fmt.Fprintf(&b, "重试 %s；", emptyText(br.Retry.Description, br.Retry.Name))
			}
			if br.Fallback != nil {
				fmt.Fprintf(&b, "降级 %s；", emptyText(br.Fallback.Description, br.Fallback.Name))
			}
		}
		c.Logic = strings.TrimSpace(b.String())
		if c.Logic == "" {
			c.Logic = c.Summary + " 按默认链路执行。"
		}
	}
	if c.Tech == "" {
		c.Tech = fmt.Sprintf("工具 %s；能力 %s；MCP %s；技能 %s；设置 %s。",
			join(c.Attributes.Tools), join(c.Attributes.Capabilities), join(c.Attributes.MCPs), join(c.Attributes.Skills), join(c.Scaffold.Settings))
	}
	if c.Analysis == "" {
		c.Analysis = fmt.Sprintf("%s（%s）来源 %s。%s 探针 %d/%d。新动词只要活源出现，下次生成按 stable_key 并入，不必重写初版总表。",
			c.Name, c.StableKey, emptyText(c.Provenance, c.Source), c.Summary, c.Probe.Passed, c.Probe.Total)
	}
	return c
}

func emptyText(v, fallback string) string {
	if strings.TrimSpace(v) == "" {
		return fallback
	}
	return v
}

func applyPrompt(f Finding) string {
	return strings.Join([]string{
		"【Lunitide 自我净化任务】",
		fmt.Sprintf("编号 %s  对象 %s  严重度 %s  状态 %s", f.ErrorCode, f.StableKey, f.Severity, f.Status),
		"问题：" + f.Title,
		"证据：" + f.Evidence,
		"根因：" + f.RootCause,
		"改进方案：" + f.Fix,
		"验证：" + f.Verify,
		"请配合产品内部模型与技能执行修复。中枢会完成本地目录修复（标签/入口方法/状态）；不自动改写 Go/TS。输出可落地补丁计划，并写明要调用的技能或模型。",
	}, "\n")
}
