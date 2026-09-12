package officestudio

import "testing"

func TestQualityForIgnoresHonestTargetCompatibilityGap(t *testing.T) {
	got := QualityFor([]Check{
		{ID: "package", Status: "passed", Required: true},
		{ID: "native_render", Status: "passed", Required: true},
		{ID: "target-compatibility", Status: "unsupported", Required: false, Detail: "尚未在目标软件完成打开验证"},
	})
	if got != "passed" {
		t.Fatalf("honest WPS coverage gap must not block quality rollup: %s", got)
	}
}

func TestQualityForIgnoresHonestTargetAppAndPDFAGaps(t *testing.T) {
	got := QualityFor([]Check{
		{ID: "package", Status: "passed", Required: true},
		{ID: "native_render", Status: "passed", Required: true},
		{ID: "target-compatibility", Status: "unsupported", Required: false},
		{ID: "target-powerpoint", Status: "unsupported", Required: false},
		{ID: "target-wps", Status: "unsupported", Required: false},
		{ID: "target-libreoffice", Status: "unsupported", Required: false},
		{ID: "visual-model", Status: "unsupported", Required: false},
		{ID: "pdfa", Status: "unsupported", Required: false},
	})
	if got != "passed" {
		t.Fatalf("honest coverage matrix must not block quality rollup: %s", got)
	}
}

func TestQualityForKeepsMissingRendererAndDesignOffPartial(t *testing.T) {
	if QualityFor([]Check{
		{ID: "native_render", Status: "unsupported", Required: true},
		{ID: "target-compatibility", Status: "unsupported", Required: false},
	}) == "passed" {
		t.Fatal("missing renderer marked passed")
	}
	if QualityFor([]Check{
		{ID: "package", Status: "passed", Required: true},
		{ID: "design_system", Status: "unsupported", Required: false, Detail: "设计系统已关闭，旧质量范围"},
		{ID: "target-compatibility", Status: "unsupported", Required: false},
	}) == "passed" {
		t.Fatal("design off marked new quality bar passed")
	}
}
