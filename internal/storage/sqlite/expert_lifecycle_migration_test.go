package sqlite

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/lunitide/lunitide/internal/domain/m8core"
	"github.com/lunitide/lunitide/internal/m8app"
	"github.com/oklog/ulid/v2"
)

func TestExpertLifecycleMigrationPreservesLegacyStates(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "legacy-experts.db")
	raw := legacyBeforeMigration(t, path, "0148_")
	ids := map[string]string{}
	versions := map[string]string{}
	for _, state := range []string{"enabled", "disabled", "archived"} {
		id := ulid.Make().String()
		ids[state] = id
		versions[state] = ulid.Make().String()
		if _, err := raw.Exec(`INSERT INTO expert_catalog(expert_id,subject_id,name,division,source,current_version_id,state,created_at,updated_at,catalog_item_id) VALUES(?,?,?,?,?,?,?,?,?,?)`, id, "owner", state, "design", "local", versions[state], state, "before", "before", "shipped-expert"); err != nil {
			t.Fatal(err)
		}
	}
	if err := raw.Close(); err != nil {
		t.Fatal(err)
	}
	s, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.AgentRuntimeRepository().TransactExpert(ctx, func(tx m8app.ExpertTx) error {
		for state, id := range ids {
			row, err := tx.GetExpert(id)
			if err != nil {
				return err
			}
			if row.State != state || row.CreationOrigin != "legacy" || row.DeletedAt != "" || row.UpdatedAt != "before" || row.CatalogItemID != "shipped-expert" {
				t.Fatalf("legacy changed: %+v", row)
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	svc := m8app.NewExpertService(s.AgentRuntimeRepository(), "owner", &m8app.MemoryPersonaStore{})
	for state, id := range ids {
		in := m8app.ExpertDeleteInput{ExpertID: id, ExpectedVersionID: versions[state], ConfirmToken: m8app.ExpertDeleteToken(id, versions[state])}
		if err := svc.Delete(ctx, in); !errors.Is(err, m8app.ErrExpertManualOnly) {
			t.Fatalf("legacy deletion: %v", err)
		}
	}
	x, err := svc.Create(ctx, m8app.CreateInput{Source: "local", Frontmatter: m8core.Frontmatter{Name: "manual", Division: "design", Description: "test", Semver: "1.0.0"}, SixSection: m8core.SixSection{Identity: "i", Mission: "m", Rules: "r", Workflow: "w", DeliverableTemplate: "d", SuccessMetrics: "s"}, RequestID: "manual"})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	svc = m8app.NewExpertService(s.AgentRuntimeRepository(), "owner", &m8app.MemoryPersonaStore{})
	list, err := svc.List(ctx, m8app.ExpertFilter{CreationOrigin: "manual"})
	if err != nil || len(list.Experts) != 1 || list.Experts[0].ExpertID != x.ExpertID || list.Experts[0].State != "disabled" || !list.Experts[0].IsOwn {
		t.Fatalf("origin not durable: %+v %v", list, err)
	}
}
