package skillapp

import (
	"context"
	"strings"
	"testing"

	"github.com/lunitide/lunitide/internal/domain/skill"
)

func TestPublishChecksActualRunnableContractBeforeChangingState(t *testing.T) {
	for _, tc := range []struct{ name, entry, manifest string }{
		{"empty prompt", "SKILL.md", `{}`},
		{"blank prompt", "SKILL.md", `{"prompt":"  "}`},
		{"wrong prompt type", "SKILL.md", `{"prompt":123}`},
		{"missing imported prompt", "builtin://import/local", `{}`},
		{"unknown executable", "scripts/run.py", `{"prompt":"do work"}`},
		{"non-object", "SKILL.md", `[]`},
		{"invalid json", "SKILL.md", `{x}`},
		{"escaping reference", "SKILL.md", `{"prompt":"do work","references":["../other.md"]}`},
		{"escaping resource", "SKILL.md", `{"prompt":"do work","files":{"../other.md":"x"}}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			sk := makeSkill("01ARZ3NDEKTSV4RRFFQ69G5FAV", skill.SkillStatusDraft, nil)
			sk.EntryPoint, sk.ManifestJSON = tc.entry, tc.manifest
			writer := &mockSkillWriter{}
			svc := New(&mockSkillReader{skill: sk}, writer)
			if err := svc.Publish(context.Background(), sk.ID); err == nil {
				t.Fatal("invalid draft published")
			}
			if writer.updatedStatus != "" {
				t.Fatal("validation changed lifecycle state")
			}
		})
	}
}

func TestPackageRelativeMarkdownDraftTrialLoadsStoredPrompt(t *testing.T) {
	sk := makeSkill("01ARZ3NDEKTSV4RRFFQ69G5FAV", skill.SkillStatusDraft, nil)
	sk.EntryPoint, sk.ManifestJSON = "weekly-report/SKILL.md", `{"prompt":"Use only the supplied weekly report facts."}`
	writer := &mockSkillWriter{}
	svc := New(&mockSkillReader{skill: sk}, writer)
	ctx := context.Background()
	inv, err := svc.InvokeTrial(ctx, sk.ID, "01ARZ3NDEKTSV4RRFFQ69G5FAW", "Create a weekly report", "full-access")
	if err != nil {
		t.Fatal(err)
	}
	out, err := svc.Execute(ctx, inv.ID, inv.SessionID, true)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.Output, "Use only the supplied weekly report facts.") {
		t.Fatalf("missing stored agreement: %s", out.Output)
	}
	if writer.updatedStatus != "" {
		t.Fatal("trial published the draft")
	}
}

func TestMarkdownDraftAliasCannotSelectExecutableOrEscapePackage(t *testing.T) {
	for _, entry := range []string{"../SKILL.md", "/SKILL.md", "C:/SKILL.md", "pkg/SKILL.md:run", "pkg/run.py", `pkg\SKILL.md`, "pkg/../SKILL.md"} {
		if allowlistedSkillEntryPoint(entry) {
			t.Fatalf("unsafe entry accepted: %q", entry)
		}
	}
	sk := makeSkill("01ARZ3NDEKTSV4RRFFQ69G5FAV", skill.SkillStatusDraft, nil)
	sk.EntryPoint, sk.ManifestJSON = "weekly-report/SKILL.md", `{}`
	if err := New(&mockSkillReader{skill: sk}, &mockSkillWriter{}).validateRunnableSkill(*sk); err == nil {
		t.Fatal("alias bypassed prompt validation")
	}
}
