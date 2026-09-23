package contextapp

import (
	"strings"

	"github.com/lunitide/lunitide/internal/domain/token"
)

const attachmentTrimNotice = "（正文过长，这一轮只放入开头。完整文件已保存在本对话附件里。）"

// FitAttachmentExcerpts keeps every referenced file inside the token allowance
// for this turn. Short files stay whole. A file that does not fit keeps its
// name and as much of the leading text as the remaining allowance allows.
// Returned order matches the input order.
func FitAttachmentExcerpts(model string, excerpts []ContextSource, budget int64) ([]ContextSource, bool) {
	if len(excerpts) == 0 {
		return excerpts, false
	}
	if budget < 0 {
		budget = 0
	}
	costs := make([]int64, len(excerpts))
	var sum int64
	for i := range excerpts {
		costs[i] = attachmentExcerptTokens(model, excerpts[i].Content)
		sum += costs[i]
	}
	if sum <= budget {
		return excerpts, false
	}
	out := append([]ContextSource(nil), excerpts...)
	order := make([]int, len(out))
	for i := range order {
		order[i] = i
	}
	for i := 1; i < len(order); i++ {
		for j := i; j > 0 && costs[order[j]] < costs[order[j-1]]; j-- {
			order[j], order[j-1] = order[j-1], order[j]
		}
	}
	remaining := budget
	trimmed := false
	for _, idx := range order {
		if costs[idx] <= remaining {
			remaining -= costs[idx]
			continue
		}
		trimmed = true
		out[idx].Content = shrinkAttachmentContent(model, out[idx].Content, remaining)
		used := attachmentExcerptTokens(model, out[idx].Content)
		remaining -= used
		if remaining < 0 {
			remaining = 0
		}
	}
	return out, trimmed
}

func attachmentExcerptTokens(model, content string) int64 {
	return token.CountTokensForModel(model, renderUntrustedUserContext("Attachment", content))
}

func shrinkAttachmentContent(model, content string, allowance int64) string {
	name, body, hasBody := strings.Cut(content, "\n")
	name = strings.TrimSpace(name)
	if name == "" {
		name = "附件"
	}
	if !hasBody {
		body = ""
	}
	nameOnly := name + "\n" + attachmentTrimNotice
	if allowance <= 0 || attachmentExcerptTokens(model, nameOnly) > allowance {
		return name
	}
	runes := []rune(body)
	best := nameOnly
	lo, hi := 0, len(runes)
	for lo <= hi {
		mid := (lo + hi) / 2
		candidate := name + "\n" + string(runes[:mid])
		if mid > 0 || body != "" {
			candidate += "\n" + attachmentTrimNotice
		}
		if attachmentExcerptTokens(model, candidate) <= allowance {
			best = candidate
			lo = mid + 1
			continue
		}
		hi = mid - 1
	}
	return best
}
