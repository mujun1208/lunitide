package m8app_test

import (
	"testing"

	"github.com/lunitide/lunitide/internal/m8app"
)

func TestIsOpsColleagueCoversFiveCardsAndAliases(t *testing.T) {
	yes := [][2]string{
		{"航空机务维修专家", "mro-expert"},
		{"航空机务专家", ""},
		{"", "mro-expert"},
		{"低空适航专家", ""},
		{"", "uas-airworthiness-expert"},
		{"航空工具化工品专家", ""},
		{"工具化工品专家", ""},
		{"", "tooling-chemical-expert"},
		{"航空航材专家", "parts-expert"},
		{"航空维修计划专家", "mx-planning-expert"},
		{"uas-airworthiness-expert", ""},
	}
	for _, pair := range yes {
		if !m8app.IsOpsColleague(pair[0], pair[1]) {
			t.Fatalf("IsOpsColleague(%q,%q) = false", pair[0], pair[1])
		}
	}
	if m8app.IsOpsColleague("PPT专家", "") || m8app.IsOpsColleague("", "ppt-expert") {
		t.Fatal("PPT专家 must not be an ops colleague")
	}
	if m8app.IsOpsColleague("产品经理专家", "pm-expert") {
		t.Fatal("产品经理专家 must not be an ops colleague")
	}
}

func TestOpsColleagueIDsAreFive(t *testing.T) {
	if len(m8app.OpsColleagueIDs) != 5 {
		t.Fatalf("OpsColleagueIDs = %d, want 5", len(m8app.OpsColleagueIDs))
	}
}
