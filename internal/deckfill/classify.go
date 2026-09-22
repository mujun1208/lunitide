package deckfill

import (
	"regexp"
	"strings"
	"unicode"
)

// SlotKind is what a text shape on a designer slide is for.
// Classification uses the sample copy, not the OOXML placeholder tag:
// the decks people actually pick often have no placeholders at all.
type SlotKind int

const (
	SlotSkip SlotKind = iota
	SlotTitle
	SlotBody
	SlotMetaPresenter
	SlotMetaDept
	SlotMetaDate
	SlotSampleTitle
	SlotEnglish
	SlotConfirm
)

var (
	metricValue = regexp.MustCompile(`^(?:XX%|\d[\d.,]*%?\+?)$`)
	taskLabel   = regexp.MustCompile(`^任务[一二三四五六七八九十0-9]+$`)
	sampleTitle = map[string]bool{
		"工作总结": true, "励志工作总结": true, "月度工作总结": true,
		"财务工作总结": true, "年中工作总结": true, "岗位竞聘": true,
	}
)

// Classify decides whether sample copy may be replaced.
// SlotConfirm is real wording already on the page; callers must not overwrite it.
func Classify(sample string) SlotKind {
	text := strings.TrimSpace(sample)
	if text == "" || text == "LOGO" || text == "Logo" {
		return SlotSkip
	}
	switch {
	case strings.Contains(text, "汇报人") || strings.Contains(text, "主讲人"):
		return SlotMetaPresenter
	case strings.Contains(text, "部门"):
		return SlotMetaDept
	case strings.Contains(text, "时间") || strings.Contains(text, "日期"):
		return SlotMetaDate
	case isTitlePrompt(text):
		return SlotTitle
	case isBodyPrompt(text):
		return SlotBody
	case sampleTitle[text] || text == "完成情况":
		return SlotSampleTitle
	case metricValue.MatchString(strings.ReplaceAll(text, " ", "")):
		return SlotSkip
	case isLatinLine(text):
		return SlotEnglish
	default:
		return SlotConfirm
	}
}

func isTitlePrompt(text string) bool {
	return strings.Contains(text, "添加标题") || text == "关键词标题" || taskLabel.MatchString(text)
}

func isBodyPrompt(text string) bool {
	for _, cue := range []string{"此处添加", "在这里输入", "关键词", "单击此处", "请在此", "简述", "梳理本阶段", "总结本阶段"} {
		if strings.Contains(text, cue) {
			return true
		}
	}
	return false
}

func isLatinLine(text string) bool {
	letters := 0
	for _, r := range text {
		if unicode.Is(unicode.Han, r) {
			return false
		}
		if unicode.IsLetter(r) {
			letters++
		}
	}
	return letters >= 2
}
