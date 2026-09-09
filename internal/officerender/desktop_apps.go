package officerender

import "strings"

type DesktopApplication struct {
	ID    string
	Label string
}

func DesktopApplicationsNotice(apps []DesktopApplication) (string, bool) {
	if len(apps) == 0 {
		return "未检测到 Microsoft Office 或 WPS；仍可导出文件后自行打开", false
	}
	labels := make([]string, 0, len(apps))
	seen := map[string]bool{}
	for _, app := range apps {
		label := strings.TrimSpace(app.Label)
		if label == "" || seen[label] {
			continue
		}
		seen[label] = true
		labels = append(labels, label)
	}
	if len(labels) == 0 {
		return "未检测到 Microsoft Office 或 WPS；仍可导出文件后自行打开", false
	}
	return "已检测到 " + strings.Join(labels, "、") + "；可用于本机打开核对，不参与隔离自动排版检查", true
}
