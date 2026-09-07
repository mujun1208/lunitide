package app

import (
	"strings"
	"testing"
)

func TestMessageReferenceIncludesDurableArtifactPaths(t *testing.T) {
	got := messageReferenceContent("已生成", []SessionArtifact{{Path: "reports/weekly.docx"}, {Path: "reports/weekly.docx"}, {Path: ""}, {Path: "figures/result.webp"}})
	if !strings.Contains(got, "已生成") || strings.Count(got, "reports/weekly.docx") != 1 || !strings.Contains(got, "figures/result.webp") {
		t.Fatalf("reference = %q", got)
	}
	if got := messageReferenceContent("", []SessionArtifact{{Path: "report.md"}}); got == "" {
		t.Fatal("artifact-only message reference lost")
	}
}
