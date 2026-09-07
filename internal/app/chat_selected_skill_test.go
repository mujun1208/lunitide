package app

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/lunitide/lunitide/internal/domain/skill"
)

func TestExplicitSkillChipSurvivesCatalogTruncation(t *testing.T) {
	var available []skill.Skill
	for i := 0; i < 20; i++ {
		available = append(available, catalogTestSkill(fmt.Sprintf("other-%02d", i), "Other", `{}`))
	}
	selected := catalogTestSkill("EnglishChosenSkill", "Selected", `{}`)
	selected.ID = "01ARZ3NDEKTSV4RRFFQ69G5FAV"
	available = append(available, selected)
	engine := NewEngine(providerRepositoryStub{}, "test")
	engine.skills = &skillCatalogStub{items: available}
	query := "[引用技能 EnglishChosenSkill|" + selected.ID + "]\nUse this"
	for _, companion := range []bool{false, true} {
		catalog := engine.skillCatalogInjection(context.Background(), query, companion, nil)
		if !strings.Contains(catalog, "skillId="+selected.ID) {
			t.Fatalf("selected skill omitted, companion=%t: %s", companion, catalog)
		}
		if len(catalog) > skillInjectMaxBytes {
			t.Fatal("catalog exceeded budget")
		}
	}
}
