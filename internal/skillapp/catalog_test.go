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

func TestMROCatalogSkillsPresent(t *testing.T) {
	want := map[string]string{
		"mro-manual-rag": "机务手册检索",
		"mro-fault-tree": "排故故障树",
		"mro-checklist":  "机务检查单",
	}
	found := map[string]CatalogTemplate{}
	for _, tpl := range Catalog() {
		if _, ok := want[tpl.ID]; ok {
			found[tpl.ID] = tpl
		}
	}
	for id, display := range want {
		tpl, ok := found[id]
		if !ok {
			t.Fatalf("catalog missing %s", id)
		}
		if tpl.DisplayName != display {
			t.Fatalf("%s DisplayName = %q, want %q", id, tpl.DisplayName, display)
		}
		if tpl.Name != "tpl-"+id {
			t.Fatalf("%s Name = %q, want tpl-%s", id, tpl.Name, id)
		}
		if tpl.EntryPoint != "builtin://"+id {
			t.Fatalf("%s EntryPoint = %q", id, tpl.EntryPoint)
		}
		prompt, _ := tpl.Manifest["prompt"].(string)
		if !strings.Contains(prompt, "不构成放行") && id != "mro-fault-tree" {
			t.Fatalf("%s prompt must mention 不构成放行 or isolation rules", id)
		}
		if id == "mro-manual-rag" && !strings.Contains(prompt, "kb.search") {
			t.Fatalf("mro-manual-rag prompt must mention kb.search")
		}
		if id == "mro-fault-tree" && !strings.Contains(prompt, "禁止无引用确定件号") {
			t.Fatalf("mro-fault-tree prompt must forbid uncited part numbers")
		}
	}
}

func TestCatalogTemplatesWellFormed(t *testing.T) {
	seen := map[string]bool{}
	for _, tpl := range Catalog() {
		if tpl.ID == "" || tpl.Name == "" || tpl.DisplayName == "" || tpl.Category == "" {
			t.Fatalf("template missing required identity fields: %+v", tpl)
		}
		if !strings.HasPrefix(tpl.Name, "tpl-") && tpl.ID != "skill-creator" && tpl.ID != "expert-manager" && tpl.ID != "plugin-creator" &&
			tpl.ID != "find-skills" && tpl.ID != "brainstorming" && tpl.ID != "pm-skill" && tpl.ID != "super-coders" &&
			tpl.ID != "frontend-design" && tpl.ID != "ui-components" && tpl.ID != "design-system" && tpl.ID != "computer-control" && tpl.ID != "browser-automation" {
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
func (m *memSkillStore) UpdateSkillFields(_ context.Context, id, display, desc, entry, manifest, _ string, minEV *string) error {
	sk, ok := m.byID[id]
	if !ok {
		return ErrSkillNotFound
	}
	sk.DisplayName = display
	sk.Description = desc
	sk.EntryPoint = entry
	sk.ManifestJSON = manifest
	if minEV != nil {
		sk.MinEngineVersion = minEV
	}
	m.byID[id] = sk
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

func TestExpertManagerCatalogManifest(t *testing.T) {
	for _, tpl := range Catalog() {
		if tpl.ID != "expert-manager" {
			continue
		}
		if tpl.Name != "expert-manager" || tpl.DisplayName != "expert-manager" {
			t.Fatalf("expert-manager identity = %#v", tpl)
		}
		raw := manifestFor(tpl)
		if len(raw) < 500 {
			t.Fatalf("manifest too short: %d", len(raw))
		}
		if len(raw) > 65536 {
			t.Fatalf("manifest too long: %d", len(raw))
		}
		return
	}
	t.Fatal("expert-manager template missing from catalog")
}

func TestSkillCreatorCatalogManifest(t *testing.T) {
	for _, tpl := range Catalog() {
		if tpl.ID != "skill-creator" {
			continue
		}
		if tpl.Name != "skill-creator" || tpl.DisplayName != "skill-creator" {
			t.Fatalf("skill-creator identity = %#v", tpl)
		}
		raw := manifestFor(tpl)
		if len(raw) < 1000 {
			t.Fatalf("manifest too short: %d", len(raw))
		}
		if len(raw) > 65536 {
			t.Fatalf("manifest too long: %d", len(raw))
		}
		if !strings.Contains(tpl.Description, "skill.create") {
			t.Fatalf("skill-creator Description must mention skill.create: %q", tpl.Description)
		}
		for _, banned := range []string{"eval-viewer", "generate_review", "measure skill performance"} {
			if strings.Contains(strings.ToLower(tpl.Description), banned) || strings.Contains(strings.ToLower(raw), banned) {
				t.Fatalf("skill-creator still mentions %q", banned)
			}
		}
		return
	}
	t.Fatal("skill-creator template missing from catalog")
}

func TestEnsureBundledSkillsPublishesCatalog(t *testing.T) {
	store := newMemSkillStore()
	svc := New(store, store)
	want := 0
	for _, tpl := range Catalog() {
		if tpl.Bundled {
			want++
		}
	}
	if want < 2 {
		t.Fatalf("expected bundled templates, got %d", want)
	}
	n, err := svc.EnsureBundledSkills(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if n != want {
		t.Fatalf("published %d, want %d", n, want)
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
	if len(listed) != want {
		t.Fatalf("listed %d published, want %d", len(listed), want)
	}
	if _, err := svc.GetByNameVersion(context.Background(), "tpl-grill-me", "1.0.0"); err == nil {
		t.Fatal("market-only template should not auto-install")
	}
}

func TestEnsureComposeSkillsPublishesPreferred(t *testing.T) {
	store := newMemSkillStore()
	svc := New(store, store)
	if _, err := svc.EnsureBundledSkills(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.GetByNameVersion(context.Background(), "tpl-slide-builder", "1.0.0"); err == nil {
		t.Fatal("slide-builder must stay market-only until compose ensure")
	}
	n, err := svc.EnsureComposeSkills(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if n < 5 {
		t.Fatalf("compose published %d, want several preferred skills", n)
	}
	for _, name := range []string{
		"tpl-slide-builder", "tpl-web-researcher", "tpl-mermaid-diagrams",
		"tpl-docx-writer", "tpl-anti-ai-prose", "tpl-e2e-browser", "tpl-fiction-continuity",
		"tpl-mro-manual-rag", "tpl-mro-fault-tree", "tpl-mro-checklist",
	} {
		if _, err := svc.GetByNameVersion(context.Background(), name, "1.0.0"); err != nil {
			t.Fatalf("compose skill %s missing: %v", name, err)
		}
	}
	again, err := svc.EnsureComposeSkills(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if again != 0 {
		t.Fatalf("second compose ensure published %d", again)
	}
}

func TestEnsureComposeSkillsRefreshesStaleManifest(t *testing.T) {
	store := newMemSkillStore()
	svc := New(store, store)
	if _, err := svc.EnsureComposeSkills(context.Background()); err != nil {
		t.Fatal(err)
	}
	sk, err := svc.GetByNameVersion(context.Background(), "tpl-find-bug", "1.0.0")
	if err != nil {
		t.Fatal(err)
	}
	stale := `{"prompt":"旧正文","triggers":["找 bug"]}`
	if _, err := svc.UpdateFields(context.Background(), sk.ID, nil, nil, nil, &stale, nil, nil, 0); err != nil {
		t.Fatal(err)
	}
	n, err := svc.EnsureComposeSkills(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if n < 1 {
		t.Fatal("stale compose skill must be refreshed")
	}
	got, err := svc.GetByNameVersion(context.Background(), "tpl-find-bug", "1.0.0")
	if err != nil {
		t.Fatal(err)
	}
	if got.Version != "1.0.0" {
		t.Fatalf("refresh must keep semver, got %q", got.Version)
	}
	if !strings.Contains(got.ManifestJSON, "七类探针") {
		t.Fatalf("stale find-bug not refreshed: %s", got.ManifestJSON)
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

func TestComposeSkillPromptsCarryRecipes(t *testing.T) {
	want := map[string][]string{
		"slide-builder":       {"九步", "演讲备注", "文类"},
		"find-bug":            {"七类探针", "已验证正确项", "状态机"},
		"anti-ai-prose":       {"质量体检", "赋能"},
		"fiction-continuity":  {"账本", "晋升", "kind=novel"},
		"hardware-bom":        {"Mandatory", "KV", "ERP"},
	}
	for _, tpl := range Catalog() {
		needles, ok := want[tpl.ID]
		if !ok {
			continue
		}
		prompt, _ := tpl.Manifest["prompt"].(string)
		for _, needle := range needles {
			if !strings.Contains(prompt, needle) {
				t.Fatalf("%s prompt missing %q", tpl.ID, needle)
			}
		}
		delete(want, tpl.ID)
	}
	if len(want) > 0 {
		t.Fatalf("missing catalog templates: %v", want)
	}
}
