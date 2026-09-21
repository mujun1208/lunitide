package meetings

import (
	"strings"
	"testing"
)

// The prompt asks for restraint, but a model that ignores it turns the notes into
// a wall the reader waits through. Caps have to hold in code, not in wording.
func TestNotesClampReasoningAndDiagramGrowth(t *testing.T) {
	p := notesPayload{Topics: []notesTopic{{
		Heading:   "上线",
		Reasoning: []string{"一", "二", "三", "四", "五"},
		Diagram: &notesDiagram{
			Steps:    []string{"1", "2", "3", "4", "5", "6", "7", "8"},
			Branches: []notesBranch{{From: "2", To: "a"}, {From: "3", To: "b"}, {From: "4", To: "c"}, {From: "5", To: "d"}},
		},
	}}}
	clampNotesPayload(&p)
	topic := p.Topics[0]
	if len(topic.Reasoning) != maxTopicReasoning {
		t.Fatalf("reasoning kept %d items", len(topic.Reasoning))
	}
	if len(topic.Diagram.Steps) != maxDiagramSteps {
		t.Fatalf("diagram kept %d steps", len(topic.Diagram.Steps))
	}
	if len(topic.Diagram.Branches) != maxDiagramBranches {
		t.Fatalf("diagram kept %d branches", len(topic.Diagram.Branches))
	}
}

// A clamped step must still match the branch that points at it, or the flow grows
// a second disconnected copy of the same box.
func TestNotesClampKeepsBranchesAttachedToClampedSteps(t *testing.T) {
	long := strings.Repeat("流", maxDiagramLabel+8)
	p := notesPayload{Topics: []notesTopic{{
		Heading: "排查",
		Diagram: &notesDiagram{
			Steps:    []string{"收到告警", long},
			Branches: []notesBranch{{From: long, Label: "仍失败", To: "升级处理"}},
		},
	}}}
	clampNotesPayload(&p)
	code := mermaidFlowchart(*p.Topics[0].Diagram)
	if strings.Count(code, "[\"") != 3 {
		t.Fatalf("expected 3 boxes, got:\n%s", code)
	}
	if !strings.Contains(code, "…") {
		t.Fatalf("long label was not clamped:\n%s", code)
	}
	if !strings.Contains(code, `|"仍失败"|`) {
		t.Fatalf("branch detached from clamped step:\n%s", code)
	}
}

// Clamping runs on the parse path, so the rendered document is bounded too.
func TestParseJSONNotesClampsBeforeRendering(t *testing.T) {
	notes, ok := parseJSONNotes(`{"title":"t","topics":[{"heading":"h","points":["p"],"reasoning":["a","b","c","d","e"]}]}`)
	if !ok {
		t.Fatal("notes did not parse")
	}
	if got := strings.Count(notes.Summary, "\n- "); got > maxTopicReasoning+1 {
		t.Fatalf("rendered %d bullets, caps did not apply:\n%s", got, notes.Summary)
	}
	if strings.Contains(notes.Summary, "- e") {
		t.Fatalf("overflow reasoning reached the document:\n%s", notes.Summary)
	}
}
