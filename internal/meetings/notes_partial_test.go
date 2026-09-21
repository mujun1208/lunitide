package meetings

import (
	"strings"
	"testing"
)

const partialHead = `{"title":"上线评审","background":"对齐范围","topics":[{"heading":"范围","points":["只做浏览器包"],"reasoning":["其余延后"]},`

// The point of partial parsing: a finished topic must be readable before the
// closing brace arrives, or the reader waits through the whole generation.
func TestParsePartialNotesShowsFinishedTopicsMidStream(t *testing.T) {
	notes, ready, ok := ParsePartialNotes(partialHead+`{"heading":"排期","points":["下周三"`, "兜底标题")
	if !ok {
		t.Fatal("finished topic was not readable mid-stream")
	}
	if ready < 1 {
		t.Fatalf("ready = %d", ready)
	}
	if !strings.Contains(notes.Summary, "只做浏览器包") {
		t.Fatalf("first topic missing:\n%s", notes.Summary)
	}
	if notes.Title != "上线评审" {
		t.Fatalf("title = %q", notes.Title)
	}
}

// Every cut point in a real stream has to be survivable. A crash or a false
// negative here shows up as a document that never appears.
func TestParsePartialNotesSurvivesEveryCutPoint(t *testing.T) {
	full := partialHead + `{"heading":"排期","points":["下周三上线"],"diagram":{"caption":"流程","steps":["评审","上线"]}}],"decisions":["先发浏览器包"],"actions":[{"owner":"张三","task":"补文档"}]}`
	firstReadable := -1
	for i := 1; i <= len(full); i++ {
		notes, ready, ok := ParsePartialNotes(full[:i], "兜底标题")
		if !ok {
			continue
		}
		if ready < 1 || strings.TrimSpace(notes.Summary) == "" {
			t.Fatalf("cut %d reported ok with empty document", i)
		}
		if firstReadable < 0 {
			firstReadable = i
		}
	}
	if firstReadable < 0 {
		t.Fatal("no prefix was ever readable")
	}
	// It must become readable well before the end, otherwise streaming bought
	// nothing over waiting for the final response.
	if firstReadable > len(full)*3/4 {
		t.Fatalf("first readable at %d of %d bytes", firstReadable, len(full))
	}
	notes, _, ok := ParsePartialNotes(full, "兜底标题")
	if !ok || !strings.Contains(notes.Summary, "下周三上线") {
		t.Fatalf("complete stream lost content: ok=%v\n%s", ok, notes.Summary)
	}
	if !strings.Contains(notes.Actions, "补文档") {
		t.Fatalf("actions missing: %q", notes.Actions)
	}
}

// Publishing a title with no topics would swap the spinner for a blank page.
func TestParsePartialNotesWithoutTopicsStaysSilent(t *testing.T) {
	for _, prefix := range []string{`{`, `{"title":"上线评审"`, `{"title":"上线评审","topics":[`, `{"title":"x","topics":[{"points":["无标题"]}]`} {
		if _, _, ok := ParsePartialNotes(prefix, "兜底标题"); ok {
			t.Fatalf("published an empty document for %q", prefix)
		}
	}
}

// A brace inside a quoted string must not be counted as structure.
func TestCloseOpenJSONIgnoresBracesInsideStrings(t *testing.T) {
	notes, _, ok := ParsePartialNotes(`{"title":"t","topics":[{"heading":"格式","points":["模板写成 {\"a\":[1}"`, "兜底")
	if !ok {
		t.Fatal("string-embedded braces broke the repair")
	}
	if !strings.Contains(notes.Summary, "模板写成") {
		t.Fatalf("content lost:\n%s", notes.Summary)
	}
}

func TestCloseOpenJSONHandlesTrailingCommaColonAndEscape(t *testing.T) {
	for _, prefix := range []string{
		`{"title":"t","topics":[{"heading":"h","points":["p"]},`,
		`{"title":"t","topics":[{"heading":"h","points":["p"]}],"decisions":`,
		`{"title":"t","topics":[{"heading":"h","points":["p\`,
	} {
		if _, _, ok := ParsePartialNotes(prefix, "兜底"); !ok {
			t.Fatalf("repair failed for %q -> %q", prefix, closeOpenJSON(jsonBody(prefix)))
		}
	}
}

// The model sometimes opens a fence it has not closed yet.
func TestParsePartialNotesReadsThroughOpenFence(t *testing.T) {
	if _, _, ok := ParsePartialNotes("```json\n"+partialHead, "兜底"); !ok {
		t.Fatal("open fence hid a readable document")
	}
}
