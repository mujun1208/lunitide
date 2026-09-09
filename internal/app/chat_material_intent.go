package app

import "strings"

// Context assembly and vision append evidence after the operator's request.
// Preserve those bytes for the model, but never let quoted material choose a
// deterministic product workflow (for example an uploaded report -> docx.gen).
func chatRoutingText(content string) string {
	for _, marker := range []string{"\n\n[Untrusted Attachment Data", "\n\n[BEGIN UNTRUSTED ", "\n\n[视觉模型识别]"} {
		if i := strings.Index(content, marker); i >= 0 {
			content = content[:i]
		}
	}
	return strings.TrimSpace(content)
}

func officeMaterialReview(goal string) bool {
	t := strings.ToLower(chatRoutingText(goal))
	if officeExplicitCreationRE.MatchString(t) || explicitOfficeOutputTool(t) != "" {
		return false
	}
	for _, verb := range []string{"分析", "解读", "解释", "检查", "复核", "审阅", "看懂", "读取", "阅读", "review", "explain", "analyse", "analyze"} {
		if !strings.Contains(t, verb) {
			continue
		}
		for _, noun := range []string{"附件", "材料", "上传", "文件", "代码", "文档", "报告", "周报", "表格", "ppt", "xlsx", "json", "typescript", "图片", "这份", "这个", "这段"} {
			if strings.Contains(t, noun) {
				return true
			}
		}
	}
	return false
}
