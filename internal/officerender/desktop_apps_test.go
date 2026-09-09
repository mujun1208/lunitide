package officerender

import "testing"

func TestDesktopApplicationsNotice(t *testing.T) {
	notice, ready := DesktopApplicationsNotice(nil)
	if ready || notice == "" {
		t.Fatalf("missing applications: %q %v", notice, ready)
	}
	notice, ready = DesktopApplicationsNotice([]DesktopApplication{
		{ID: "winword", Label: "Microsoft Word"},
		{ID: "winword-copy", Label: "Microsoft Word"},
		{ID: "wps", Label: "WPS Writer"},
	})
	if !ready || notice != "已检测到 Microsoft Word、WPS Writer；可用于本机打开核对，不参与隔离自动排版检查" {
		t.Fatalf("detected applications: %q %v", notice, ready)
	}
}
