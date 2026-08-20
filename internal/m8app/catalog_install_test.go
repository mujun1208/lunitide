package m8app_test

import (
	"context"
	"errors"
	"testing"

	"github.com/oklog/ulid/v2"

	"github.com/lunitide/lunitide/internal/domain/skill"
	"github.com/lunitide/lunitide/internal/m8app"
)

type memCatalogSkills struct {
	byName map[string]skill.Skill
}

func (m *memCatalogSkills) GetByNameVersion(_ context.Context, name, version string) (*skill.Skill, error) {
	sk, ok := m.byName[name+"@"+version]
	if !ok {
		return nil, errors.New("not found")
	}
	copy := sk
	return &copy, nil
}

func (m *memCatalogSkills) Create(_ context.Context, sk skill.Skill) (skill.Skill, error) {
	if m.byName == nil {
		m.byName = map[string]skill.Skill{}
	}
	sk.ID = ulid.Make().String()
	sk.Status = skill.SkillStatusDraft
	m.byName[sk.Name+"@"+sk.Version] = sk
	return sk, nil
}

func (m *memCatalogSkills) Publish(_ context.Context, id string) error {
	for key, sk := range m.byName {
		if sk.ID == id {
			sk.Status = skill.SkillStatusPublished
			m.byName[key] = sk
			return nil
		}
	}
	return errors.New("not found")
}

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

func TestInstallAgencyAgentChatPublishesSkill(t *testing.T) {
	item := catalogByUsage(t, m8app.CatalogUsageChat)
	skills := &memCatalogSkills{}
	res, err := m8app.InstallAgencyAgent(context.Background(), nil, skills, item.ID)
	if err != nil {
		t.Fatal(err)
	}
	if res.SkillID == "" || res.SkillName != item.SkillName() || res.ExpertID != "" {
		t.Fatalf("result = %+v", res)
	}
	got, err := skills.GetByNameVersion(context.Background(), item.SkillName(), item.Version)
	if err != nil || got.Status != skill.SkillStatusPublished {
		t.Fatalf("published skill: %+v err=%v", got, err)
	}
	if _, err := m8app.InstallAgencyAgent(context.Background(), nil, skills, item.ID); !errors.Is(err, m8app.ErrCatalogInstalled) {
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
