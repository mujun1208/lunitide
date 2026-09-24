package asset

import "testing"

func TestOfficeTemplateLabelUsesModelName(t *testing.T) {
	name, desc := OfficeTemplateLabel("Q1.pptx", "名称：季度经营汇报\n描述：按季度汇总经营指标的演示稿")
	if name != "季度经营汇报" || desc != "按季度汇总经营指标的演示稿" {
		t.Fatalf("name=%q desc=%q", name, desc)
	}
}

func TestOfficeTemplateLabelFallsBackToFileStem(t *testing.T) {
	name, desc := OfficeTemplateLabel("经营周报.docx", "")
	if name != "经营周报" || desc != OfficeTemplateFallbackDescription {
		t.Fatalf("name=%q desc=%q", name, desc)
	}
	name, desc = OfficeTemplateLabel("ledger.xlsx", "不是规定格式")
	if name != "ledger" || desc != OfficeTemplateFallbackDescription {
		t.Fatalf("unparsed name=%q desc=%q", name, desc)
	}
}
