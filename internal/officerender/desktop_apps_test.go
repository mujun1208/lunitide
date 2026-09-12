package officerender

import (
	"strings"
	"testing"
)

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
	if !ready || !strings.Contains(notice, "Microsoft Word") || !strings.Contains(notice, "WPS Writer") || !strings.Contains(notice, "不等于") {
		t.Fatalf("detected applications: %q %v", notice, ready)
	}
}
