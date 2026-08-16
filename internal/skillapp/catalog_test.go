package skillapp

import (
	"context"
	"encoding/json"
	"errors"
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
