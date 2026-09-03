package app

import (
	"strings"
	"testing"
)

func TestGateMROAnswerKeepsNarrativeWhenUngrounded(t *testing.T) {
	text := "起落架收放异常时先核对液压。建议更换件号 NAS1149 垫圈后再试收放。"
	out, chips := GateMROAnswer(text, nil)
	if !strings.Contains(out, "起落架收放异常时先核对液压") {
		t.Fatalf("must keep other narrative: %q", out)
	}
	if !strings.Contains(out, "件号 NAS1149") {
		t.Fatalf("must keep original part number: %q", out)
	}
	if !strings.Contains(out, "辅助建议，不构成放行") {
		t.Fatalf("must prepend advisory: %q", out)
	}
	if !strings.Contains(joinedChipDetail(chips), "未找到受控依据") {
		t.Fatalf("chips = %+v", chips)
	}
	if strings.TrimSpace(out) == "未找到受控依据。请提供机尾/日期或导入对应 AMM/FIM 后再问。本系统输出为辅助建议，不构成放行。" {
		t.Fatal("must not replace the model answer with a fixed refusal")
	}
	if strings.Count(out, "\n") < 1 || len([]rune(out)) < 40 {
		t.Fatalf("refusal-shaped output: %q", out)
	}
}

func TestGateMROAnswerDoesNotRewriteWhenCited(t *testing.T) {
	text := "辅助建议，不构成放行。按手册使用件号 NAS1149。"
	cites := []CitationBlock{{DocID: "01ARZ3NDEKTSV4RRFFQ69G5FAA", Revision: "42", Quote: "Install washer NAS1149", Locator: `{"docType":"AMM"}`}}
	out, chips := GateMROAnswer(text, cites)
	if out != text {
		t.Fatalf("cited answer rewritten: %q", out)
	}
	for _, c := range chips {
		if c.Kind == "ungrounded" {
			t.Fatalf("unexpected ungrounded chip: %+v", chips)
		}
	}
}

func TestGoldenEmptyClaimsStayNonDestructive(t *testing.T) {
	text := "随便给一个件号就能换，用 NAS1149 就能放行。"
	out, chips := GateMROAnswer(text, nil)
	if !strings.Contains(out, "NAS1149") || !strings.Contains(out, "随便给一个件号") {
		t.Fatalf("must keep model text: %q", out)
	}
	if !strings.Contains(joinedChipDetail(chips), "未找到受控依据") {
		t.Fatalf("chips = %+v", chips)
	}
}

func TestRestoreCouncilCitationsAppendsMissingDocID(t *testing.T) {
	draft := "## 综合结论\n先按排故卡隔离。"
	cites := []CitationBlock{{
		DocID:    "01ARZ3NDEKTSV4RRFFQ69G5FAA",
		Revision: "42",
		Quote:    "Gear retraction isolation step one",
		Locator:  `{"ata":"32"}`,
	}}
	out, restored := RestoreCouncilCitations(draft, cites)
	if !restored {
		t.Fatal("want restored=true")
	}
	if !strings.Contains(out, "01ARZ3NDEKTSV4RRFFQ69G5FAA") {
		t.Fatalf("appendix missing doc id: %q", out)
	}
	if !strings.Contains(out, "修订") {
		t.Fatalf("appendix missing revision: %q", out)
	}
}

func joinedChipDetail(chips []GateChip) string {
	var b strings.Builder
	for _, c := range chips {
		b.WriteString(c.Kind)
		b.WriteByte(':')
		b.WriteString(c.Detail)
		b.WriteByte(';')
	}
	return b.String()
}
