package meetings_test

import (
	"strings"
	"testing"

	"github.com/lunitide/lunitide/internal/meetings"
)

func TestCleanTranscriptStripsFillersAndAcronyms(t *testing.T) {
	raw := "我现在开始做那个会议纪要啊然后然后看看能不能把这个呃呃工作都彻底落实呃第一步应该先写 b r d 然后第二步"
	got := meetings.CleanTranscript(raw)
	if !strings.Contains(got, "BRD") {
		t.Fatalf("acronym = %q", got)
	}
	if strings.Contains(got, "呃") || strings.Contains(got, "啊") || strings.Contains(got, "然后然后") {
		t.Fatalf("fillers survived = %q", got)
	}
	if !strings.Contains(got, "会议纪要") || !strings.Contains(got, "落实") {
		t.Fatalf("meaning lost = %q", got)
	}
	if meetings.CleanTranscript("呃啊嗯") != "" {
		t.Fatalf("filler-only line should drop")
	}
	if !strings.Contains(meetings.CleanTranscript("你好岳西"), "月汐") {
		t.Fatalf("homophone = %q", meetings.CleanTranscript("你好岳西"))
	}
	if got2 := meetings.CleanTranscript(got); got2 != got {
		t.Fatalf("not stable: %q vs %q", got, got2)
	}
}

func TestCleanTranscriptCollapsesRepeatedHistory(t *testing.T) {
	raw := "今天天气不错。\n今天天气不错。\n今天天气不错。我们开会。"
	got := meetings.CleanTranscript(raw)
	if strings.Count(got, "今天天气不错") != 1 {
		t.Fatalf("repeat survived = %q", got)
	}
	if !strings.Contains(got, "我们开会") {
		t.Fatalf("new clause lost = %q", got)
	}
}

func TestCleanTranscriptCollapsesConcatenatedSentence(t *testing.T) {
	clause := "针对一个场景的业务逻辑设计要把项目和专家包对齐"
	got := meetings.CleanTranscript(clause + clause + clause)
	if strings.Count(got, clause) != 1 {
		t.Fatalf("tandem survived = %q", got)
	}
}
