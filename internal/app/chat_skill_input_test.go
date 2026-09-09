package app

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func TestSkillToolInputCountsUnicodeForInvokeAndDraftTrial(t *testing.T) {
	for _, trial := range []bool{false, true} {
		e, sk := customSkillFixture(t, "本轮工作约束与尾部说明不得省略")
		ctx := context.Background()
		if trial {
			e, _, sk = draftTrialFixture(t, "本轮工作约束与尾部说明不得省略")
			ctx = withSkillTrials(ctx, chatAttachmentSessionID, []string{sk.ID})
		}
		for _, input := range []string{strings.Repeat("中", 2048), strings.Repeat("🙂", 2048)} {
			args, _ := json.Marshal(map[string]string{"skillId": sk.ID, "input": input})
			var err error
			if trial {
				_, err = e.invokeSkillTrialTool(ctx, executionModeFullAccess, chatAttachmentSessionID, args)
			} else {
				_, err = e.invokeSkillTool(ctx, executionModeFullAccess, chatAttachmentSessionID, args)
			}
			if err != nil {
				t.Fatalf("trial=%t bytes=%d: %v", trial, len(input), err)
			}
		}
	}
	for _, input := range []string{strings.Repeat("中", 2049), "\x00", string([]byte{255})} {
		if validateSkillToolInput(input) == nil {
			t.Fatal("invalid skill input accepted")
		}
	}
}
