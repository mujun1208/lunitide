package m8app_test

import (
	"testing"

	"github.com/lunitide/lunitide/internal/m8app"
)

func TestExpertKindForConversationSpecialists(t *testing.T) {
	if got := m8app.ExpertKindForName("PPT专家"); got != m8app.ExpertKindAgent {
		t.Fatalf("PPT专家 kind = %q", got)
	}
	if got := m8app.ExpertKindForName("pm-advisor"); got != m8app.ExpertKindPromptSkill {
		t.Fatalf("pm-advisor kind = %q", got)
	}
	if got := m8app.ExpertKindForExpert("演示顾问", "ppt-expert"); got != m8app.ExpertKindAgent {
		t.Fatalf("renamed specialist must stay agent: %q", got)
	}
	if got := m8app.ExpertKindForExpert("演示顾问", ""); got != m8app.ExpertKindPromptSkill {
		t.Fatalf("unknown name without catalog id must be prompt_skill: %q", got)
	}
	item, ok := m8app.ResolveConversationExpert("演示顾问", "ppt-expert")
	if !ok || item.ID != "ppt-expert" {
		t.Fatalf("resolve renamed specialist: ok=%v id=%q", ok, item.ID)
	}
	excel, ok := m8app.ConversationExpertByID("excel-maker")
	if !ok || excel.ResolvedKind() != m8app.ExpertKindAgent {
		t.Fatalf("excel-maker resolved kind missing: ok=%v kind=%q", ok, excel.ResolvedKind())
	}
}

func TestDivisionRoleSeedsPersonaTitle(t *testing.T) {
	if got := m8app.DivisionRole("design"); got != "设计师" {
		t.Fatalf("design role = %q", got)
	}
	if got := m8app.DivisionRole("engineering"); got != "工程师" {
		t.Fatalf("engineering role = %q", got)
	}
	if got := m8app.DivisionRole("product"); got != "产品" {
		t.Fatalf("product role = %q", got)
	}
	if got := m8app.DivisionRole("data"); got != "研究员" {
		t.Fatalf("data role = %q", got)
	}
}

func TestConversationSpecialistsDoNotMaterializeChatSkills(t *testing.T) {
	for _, item := range m8app.ConversationExperts() {
		if item.NeedsChat() {
			t.Fatalf("%s still materializes aa-* chat skill", item.ID)
		}
		if !item.NeedsProject() {
			t.Fatalf("%s should install as an expert", item.ID)
		}
	}
}
