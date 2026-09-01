package m8app_test

import (
	"context"
	"errors"
	"testing"

	"github.com/lunitide/lunitide/internal/m8app"
)

func catalogByUsage(t *testing.T, usage string) m8app.CatalogItem {
	t.Helper()
	for _, item := range m8app.AgencyAgentsCatalog() {
		if item.Usage == usage {
			return item
		}
	}
	t.Fatalf("no catalog item with usage %s", usage)
	return m8app.CatalogItem{}
}

func TestInstallAgencyAgentChatCreatesPersonaExpert(t *testing.T) {
	item := catalogByUsage(t, m8app.CatalogUsageChat)
	svc := openExpertService(t)
	res, err := m8app.InstallAgencyAgent(context.Background(), svc, nil, item.ID)
	if err != nil {
		t.Fatal(err)
	}
	if res.ExpertID == "" || res.SkillID != "" {
		t.Fatalf("persona card must install as expert, not aa-* skill: %+v", res)
	}
	if _, err := m8app.InstallAgencyAgent(context.Background(), svc, nil, item.ID); !errors.Is(err, m8app.ErrCatalogInstalled) {
		t.Fatalf("duplicate: %v", err)
	}
}

func TestInstallAgencyAgentProjectCreatesExpert(t *testing.T) {
	item := catalogByUsage(t, m8app.CatalogUsageProject)
	svc := openExpertService(t)
	res, err := m8app.InstallAgencyAgent(context.Background(), svc, nil, item.ID)
	if err != nil {
		t.Fatal(err)
	}
	if res.ExpertID == "" || res.SkillID != "" {
		t.Fatalf("result = %+v", res)
	}
	listed, err := svc.List(context.Background(), m8app.ExpertFilter{})
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, row := range listed.Experts {
		if row.Name == item.Name {
			found = true
			if row.CatalogItemID != item.ID {
				t.Fatalf("catalog_item_id = %q, want %q", row.CatalogItemID, item.ID)
			}
			if item.ResolvedKind() == m8app.ExpertKindAgent && row.Kind != m8app.ExpertKindAgent {
				t.Fatalf("kind = %q, want agent", row.Kind)
			}
		}
	}
	if !found {
		t.Fatalf("expert %q missing from list", item.Name)
	}
	if _, err := m8app.InstallAgencyAgent(context.Background(), svc, nil, item.ID); !errors.Is(err, m8app.ErrCatalogInstalled) {
		t.Fatalf("duplicate: %v", err)
	}
}

func TestInstallAgencyAgentUnknown(t *testing.T) {
	if _, err := m8app.InstallAgencyAgent(context.Background(), nil, nil, "no-such-agent"); !errors.Is(err, m8app.ErrCatalogUnknown) {
		t.Fatalf("err = %v", err)
	}
}
