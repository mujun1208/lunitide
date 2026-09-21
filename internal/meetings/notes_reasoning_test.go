package meetings

import (
	"strings"
	"testing"
)

// The document has a reading order: what was said, then what we make of it, then
// the table or flow the reasoning called for. A diagram that lands above the
// thinking record reads as decoration instead of a conclusion.
func TestNotesKeepFactsThenReasoningThenTableThenDiagram(t *testing.T) {
	raw := `{"title":"数据分级","topics":[{"heading":"知识库分级",
	 "points":["个人库不做标准化","组织库要走审核"],
	 "reasoning":["个人库直接进生产会带入未审核内容","先判分级再决定入库路径"],
	 "table":{"caption":"三类知识库差异","headers":["类型","审核"],"rows":[["个人","否"],["组织","是"]]},
	 "diagram":{"caption":"入库流程","steps":["接收内容","判定分级","入库"],"branches":[{"from":"判定分级","label":"不合规","to":"驳回并反馈"}]}}],
	 "decisions":["组织库必须审核"]}`
	notes, ok := parseJSONNotes(raw)
	if !ok {
		t.Fatal("notes did not parse")
	}
	s := notes.Summary
	order := []string{"个人库不做标准化", "### 思考", "个人库直接进生产会带入未审核内容", "三类知识库差异", "入库流程", "```mermaid"}
	at := -1
	for _, want := range order {
		i := strings.Index(s, want)
		if i < 0 {
			t.Fatalf("notes lost %q:\n%s", want, s)
		}
		if i < at {
			t.Fatalf("%q came out of order:\n%s", want, s)
		}
		at = i
	}
	// The flow has to carry the branch, or a conditional process silently
	// flattens into a straight line that misstates the meeting.
	if !strings.Contains(s, `|"不合规"|`) {
		t.Fatalf("branch label missing from flow:\n%s", s)
	}
	if !strings.Contains(s, `["驳回并反馈"]`) {
		t.Fatalf("branch target missing from flow:\n%s", s)
	}
}

// Nothing may be invented to fill a slot: a topic without reasoning, table, or
// diagram must emit none of those headings.
func TestNotesOmitReasoningAndDiagramWhenAbsent(t *testing.T) {
	notes, ok := parseJSONNotes(`{"title":"t","topics":[{"heading":"闲聊","points":["确认下周继续"]}],"decisions":[]}`)
	if !ok {
		t.Fatal("notes did not parse")
	}
	for _, unwanted := range []string{"### 思考", "```mermaid", "flowchart"} {
		if strings.Contains(notes.Summary, unwanted) {
			t.Fatalf("empty topic emitted %q:\n%s", unwanted, notes.Summary)
		}
	}
}

// A model will put quotes and brackets in a step label; mermaid treats both as
// syntax and the whole diagram becomes a red error box in the notes.
func TestMermaidFlowchartNeutralizesLabelSyntax(t *testing.T) {
	code := mermaidFlowchart(notesDiagram{Steps: []string{`提交"BRD"[草案]`, "评审\n会"}})
	if strings.Contains(code, `"BRD"`) || strings.Contains(code, "[草案]") {
		t.Fatalf("label syntax survived: %s", code)
	}
	if strings.Contains(code, "\n  n2[\"评审\n") {
		t.Fatalf("label kept a newline: %s", code)
	}
	if !strings.Contains(code, "n1 --> n2") {
		t.Fatalf("chain lost: %s", code)
	}
}

func TestMermaidFlowchartNeedsTwoSteps(t *testing.T) {
	if code := mermaidFlowchart(notesDiagram{Steps: []string{"只有一步"}}); code != "" {
		t.Fatalf("single step produced a diagram: %s", code)
	}
	if code := mermaidFlowchart(notesDiagram{}); code != "" {
		t.Fatalf("empty diagram produced: %s", code)
	}
}

// The exported HTML carries no scripts, so the mermaid fence has to become inert
// markup. Leaking the source into the export reads as a broken document.
func TestNotesHTMLRendersFlowWithoutScript(t *testing.T) {
	var b strings.Builder
	writeNotesHTMLBlocks(&b, "前置说明\n\n```mermaid\nflowchart LR\n  n1[\"接收内容\"]\n  n2[\"判定分级\"]\n  n3[\"驳回\"]\n  n1 --> n2\n  n2 -->|\"不合规\"| n3\n```\n\n收尾说明")
	out := b.String()
	for _, leak := range []string{"flowchart", "mermaid", "-->", "<script"} {
		if strings.Contains(out, leak) {
			t.Fatalf("export leaked %q:\n%s", leak, out)
		}
	}
	for _, want := range []string{"notes-doc-flow", "接收内容", "判定分级", "不合规", "驳回", "前置说明", "收尾说明"} {
		if !strings.Contains(out, want) {
			t.Fatalf("export lost %q:\n%s", want, out)
		}
	}
}

// An unterminated fence must not swallow the rest of the document.
func TestNotesHTMLSurvivesUnclosedFence(t *testing.T) {
	var b strings.Builder
	writeNotesHTMLBlocks(&b, "```mermaid\nflowchart LR\n  n1[\"甲\"]\n  n2[\"乙\"]\n  n1 --> n2")
	if out := b.String(); !strings.Contains(out, "甲") {
		t.Fatalf("unclosed fence dropped the flow: %s", out)
	}
}
