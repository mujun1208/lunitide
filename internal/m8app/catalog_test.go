package m8app_test

import (
	"strings"
	"testing"

	"github.com/lunitide/lunitide/internal/domain/m8core"
	"github.com/lunitide/lunitide/internal/m8app"
)

func TestAgencyAgentsCatalogLoaded(t *testing.T) {
	items := m8app.AgencyAgentsCatalog()
	if len(items) < 200 {
		t.Fatalf("catalog size = %d, want >= 200", len(items))
	}
	seen := map[string]bool{}
	chat, project, both := 0, 0, 0
	for _, item := range items {
		if item.ID == "" || seen[item.ID] {
			t.Fatalf("bad or duplicate id %q", item.ID)
		}
		seen[item.ID] = true
		if !m8core.ValidDivision(item.Division) {
			t.Fatalf("%s division %q", item.ID, item.Division)
		}
		if err := m8core.ValidateSixSection(item.SixSection); err != nil {
			t.Fatalf("%s six-section: %v", item.ID, err)
		}
		fm := m8core.Frontmatter{
			Name: item.Name, Division: item.Division,
			Description: item.Description, Semver: item.Version,
		}
		if len(fm.Description) > 2000 {
			fm.Description = fm.Description[:2000]
		}
		if err := fm.Validate(); err != nil {
			t.Fatalf("%s frontmatter: %v", item.ID, err)
		}
		if item.SkillName() == "" || !strings.HasPrefix(item.SkillName(), "aa-") {
			t.Fatalf("%s skill name %q", item.ID, item.SkillName())
		}
		switch item.Usage {
		case m8app.CatalogUsageChat:
			chat++
		case m8app.CatalogUsageProject:
			project++
		case m8app.CatalogUsageBoth:
			both++
		default:
			t.Fatalf("%s usage %q", item.ID, item.Usage)
		}
	}
	if chat == 0 || project == 0 || both == 0 {
		t.Fatalf("usage mix chat=%d project=%d both=%d", chat, project, both)
	}
}
