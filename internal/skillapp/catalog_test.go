package skillapp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/lunitide/lunitide/internal/domain/skill"
)

func TestCatalogTemplatesWellFormed(t *testing.T) {
	seen := map[string]bool{}
	for _, tpl := range Catalog() {
		if tpl.ID == "" || tpl.Name == "" || tpl.DisplayName == "" || tpl.Category == "" {
			t.Fatalf("template missing required identity fields: %+v", tpl)
		}
		if !strings.HasPrefix(tpl.Name, "tpl-") {
			t.Fatalf("template name must use tpl- prefix: %q", tpl.Name)
		}
		if seen[tpl.ID] {
			t.Fatalf("duplicate template id: %s", tpl.ID)
		}
		seen[tpl.ID] = true
		if len(tpl.Permissions) == 0 {
			t.Fatalf("template %s needs at least one permission", tpl.ID)
		}
		var m map[string]any
		if err := json.Unmarshal([]byte(manifestFor(tpl)), &m); err != nil {
			t.Fatalf("template %s manifest not valid JSON: %v", tpl.ID, err)
		}
		if _, ok := m["prompt"].(string); !ok {
			t.Fatalf("template %s manifest missing prompt", tpl.ID)
		}
		if _, ok := m["triggers"].([]any); !ok {
			t.Fatalf("template %s manifest missing triggers", tpl.ID)
		}
	}
	if len(Catalog()) < 6 {
		t.Fatalf("catalog suspiciously small: %d", len(Catalog()))
	}
}

func TestInstallFromCatalogMaterializesDraft(t *testing.T) {
	svc := New(&mockSkillReader{}, &mockSkillWriter{})
	created, err := svc.InstallFromCatalog(context.Background(), "meeting-minutes")
	if err != nil {
		t.Fatal(err)
	}
	if created.Name != "tpl-meeting-minutes" || created.Status != skill.SkillStatusDraft {
		t.Fatalf("created = %+v", created)
	}
	if created.EntryPoint != "builtin://meeting-minutes" {
		t.Fatalf("entry point = %q", created.EntryPoint)
	}
	var m map[string]any
	if err := json.Unmarshal([]byte(created.ManifestJSON), &m); err != nil {
		t.Fatal(err)
	}
	if m["prompt"] == nil {
		t.Fatal("manifest lost the prompt")
	}
}

func TestInstallFromCatalogUnknownAndDuplicate(t *testing.T) {
	svc := New(&mockSkillReader{}, &mockSkillWriter{})
	if _, err := svc.InstallFromCatalog(context.Background(), "no-such-template"); !errors.Is(err, ErrTemplateUnknown) {
		t.Fatalf("unknown template: %v", err)
	}
	// Second install with the name+version already persisted must refuse.
	dupe := New(&mockSkillReader{byNameVer: &skill.Skill{Name: "tpl-meeting-minutes"}}, &mockSkillWriter{})
	if _, err := dupe.InstallFromCatalog(context.Background(), "meeting-minutes"); !errors.Is(err, ErrTemplateInstalled) {
		t.Fatalf("duplicate install: %v", err)
	}
}

type memSkillStore struct {
	byID map[string]skill.Skill
	n    int
}

func newMemSkillStore() *memSkillStore {
	return &memSkillStore{byID: map[string]skill.Skill{}}
}

func (m *memSkillStore) GetSkill(_ context.Context, id string) (*skill.Skill, error) {
	sk, ok := m.byID[id]
	if !ok {
		return nil, nil
	}
	cp := sk
	return &cp, nil
}
func (m *memSkillStore) GetSkillByNameVersion(_ context.Context, name, version string) (*skill.Skill, error) {
	for _, sk := range m.byID {
		if sk.Name == name && sk.Version == version {
			cp := sk
			return &cp, nil
		}
	}
	return nil, nil
}
func (m *memSkillStore) ListSkills(_ context.Context, status string, _ int) ([]skill.Skill, error) {
	var out []skill.Skill
	for _, sk := range m.byID {
		if status == "" || string(sk.Status) == status {
			out = append(out, sk)
		}
	}
	return out, nil
}
func (m *memSkillStore) CreateSkill(_ context.Context, sk skill.Skill) (skill.Skill, error) {
	m.n++
	sk.ID = fmt.Sprintf("01ARZ3NDEKTSV4RRFFQ69G5F%02d", m.n)
	m.byID[sk.ID] = sk
	return sk, nil
}
func (m *memSkillStore) UpdateSkill(context.Context, string, string, string) error { return nil }
func (m *memSkillStore) UpdateSkillFields(context.Context, string, string, string, string, string, string, *string) error {
	return nil
}
func (m *memSkillStore) UpdateSkillStatus(_ context.Context, id, status string) error {
	sk, ok := m.byID[id]
	if !ok {
		return ErrSkillNotFound
	}
	sk.Status = skill.SkillStatus(status)
	m.byID[id] = sk
	return nil
}
func (m *memSkillStore) DeleteSkill(_ context.Context, id string) error {
	delete(m.byID, id)
	return nil
}

func TestEnsureBundledSkillsPublishesCatalog(t *testing.T) {
	store := newMemSkillStore()
	svc := New(store, store)
	n, err := svc.EnsureBundledSkills(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if n != len(Catalog()) {
		t.Fatalf("published %d, want %d", n, len(Catalog()))
	}
	again, err := svc.EnsureBundledSkills(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if again != 0 {
		t.Fatalf("second ensure published %d", again)
	}
	listed, err := svc.List(context.Background(), skill.SkillStatusPublished)
	if err != nil {
		t.Fatal(err)
	}
	if len(listed) != len(Catalog()) {
		t.Fatalf("listed %d published", len(listed))
	}
}

func TestCatalogBuiltinEntryPointReturnsWorkingAgreement(t *testing.T) {
	sk := skill.Skill{
		ID: "01ARZ3NDEKTSV4RRFFQ69G5FAV", Name: "tpl-meeting-minutes", DisplayName: "会议纪要助手",
		Description: "整理会议", Version: "1.0.0", Status: skill.SkillStatusPublished,
		Permissions:  []skill.PermissionLevel{skill.PermissionReadWrite},
		EntryPoint:   "builtin://meeting-minutes",
		ManifestJSON: `{"prompt":"你是会议纪要助手。","triggers":["会议纪要"]}`,
		CreatedAt:    skNow(), UpdatedAt: skNow(),
	}
	svc := New(&mockSkillReader{skill: &sk}, &mockSkillWriter{})
	inv, err := svc.Invoke(context.Background(), sk.ID, "sess", "整理今天的会", "full")
	if err != nil {
		t.Fatal(err)
	}
	out, err := svc.Execute(context.Background(), inv.ID, "sess", true)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.Output, "你是会议纪要助手") || !strings.Contains(out.Output, "整理今天的会") {
		t.Fatalf("output = %q", out.Output)
	}
}
