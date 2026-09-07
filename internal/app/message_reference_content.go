package app

import "strings"

// These paths come from the session's durable artifact receipts. Include them
// only as quoted evidence; referring to a result does not authorize running it.
func messageReferenceContent(content string, artifacts []SessionArtifact) string {
	content = strings.TrimSpace(content)
	seen := make(map[string]bool)
	var paths []string
	for _, artifact := range artifacts {
		path := strings.TrimSpace(artifact.Path)
		if path == "" || seen[path] {
			continue
		}
		seen[path] = true
		paths = append(paths, path)
		if len(paths) == 32 {
			break
		}
	}
	if len(paths) > 0 {
		content += "\n本消息已保存的产物路径（查看或修改时使用对应文件工具）：\n" + strings.Join(paths, "\n")
	}
	return strings.TrimSpace(content)
}
