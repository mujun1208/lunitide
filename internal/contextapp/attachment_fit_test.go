package contextapp

import (
	"strings"
	"testing"
)

func TestFitAttachmentExcerptsKeepsShortFilesAndTrimsTheLongOne(t *testing.T) {
	shortA := ContextSource{ID: "a", Content: "FAQ.md\n常见问题只有几十字。"}
	shortB := ContextSource{ID: "b", Content: "访谈.md\n访谈纪要同样很短。"}
	long := ContextSource{ID: "c", Content: "项目日程.md\n" + strings.Repeat("这是一段很长的参考资料。", 8000)}
	excerpts := []ContextSource{long, shortA, shortB}

	full := attachmentExcerptTokens("glm-5.3", shortA.Content) + attachmentExcerptTokens("glm-5.3", shortB.Content)
	allowance := full + 400
	got, trimmed := FitAttachmentExcerpts("glm-5.3", excerpts, allowance)
	if !trimmed {
		t.Fatal("expected the long file to be trimmed")
	}
	if len(got) != 3 || got[0].ID != "c" || got[1].ID != "a" || got[2].ID != "b" {
		t.Fatalf("order changed: %+v", got)
	}
	if got[1].Content != shortA.Content || got[2].Content != shortB.Content {
		t.Fatalf("short files changed: %q %q", got[1].Content, got[2].Content)
	}
	if !strings.HasPrefix(got[0].Content, "项目日程.md\n") {
		t.Fatalf("long file lost its name: %q", got[0].Content)
	}
	if !strings.Contains(got[0].Content, attachmentTrimNotice) {
		t.Fatalf("long file missing trim notice: %q", got[0].Content)
	}
	if len(got[0].Content) >= len(long.Content) {
		t.Fatal("long file was not shortened")
	}
	var sum int64
	for _, item := range got {
		sum += attachmentExcerptTokens("glm-5.3", item.Content)
	}
	if sum > allowance {
		t.Fatalf("fitted excerpts = %d tokens, allowance %d", sum, allowance)
	}
}

func TestFitAttachmentExcerptsLeavesFittingFilesUntouched(t *testing.T) {
	excerpts := []ContextSource{{ID: "a", Content: "说明.md\n短正文"}}
	got, trimmed := FitAttachmentExcerpts("glm-5.3", excerpts, 100000)
	if trimmed || got[0].Content != excerpts[0].Content {
		t.Fatalf("unexpected trim: %v %q", trimmed, got[0].Content)
	}
}

func TestFitAttachmentExcerptsKeepsNamesWhenNothingElseFits(t *testing.T) {
	excerpts := []ContextSource{{ID: "a", Content: "销售助手.md\n" + strings.Repeat("正文", 500)}}
	got, trimmed := FitAttachmentExcerpts("glm-5.3", excerpts, 0)
	if !trimmed {
		t.Fatal("expected a trim")
	}
	if got[0].Content != "销售助手.md" {
		t.Fatalf("content = %q", got[0].Content)
	}
}
