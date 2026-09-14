package officeapp

import (
	"context"
	"encoding/json"
	"testing"

	domain "github.com/lunitide/lunitide/internal/domain/officestudio"
	"github.com/lunitide/lunitide/internal/officerender"
)

func TestOfficeFontEvidencePersistsWithoutOverclaimingNativeLayout(t *testing.T) {
	svc, store, task := studioServiceFixture(t)
	svc.Renderer.ListFonts = func(context.Context) (officerender.FontInventory, error) {
		return officerender.FontInventory{Families: []string{"Lunitide Synthetic Fixture"}, Complete: true, Basis: "test-inventory"}, nil
	}
	v := generatedWord(t, svc, task, "fonts")
	reports, err := store.ListOfficeValidations(context.Background(), v.ID)
	if err != nil || len(reports) == 0 {
		t.Fatal("font evidence not persisted", err)
	}
	var evidence struct {
		Fonts officerender.FontReport `json:"fonts"`
	}
	if err = json.Unmarshal(reports[0].Evidence, &evidence); err != nil || evidence.Fonts.SourceDigest != v.SHA256 || evidence.Fonts.MissingCount == 0 || evidence.Fonts.SubstitutionVerified {
		t.Fatalf("font report not bound to file: %+v %v", evidence, err)
	}
	for _, check := range reports[0].Checks {
		if check.ID == "font_actual_substitution" && check.Status == "passed" {
			t.Fatal("font inventory fabricated real layout proof")
		}
		if check.ID == "font_actual_substitution" && check.Required {
			t.Fatal("default persist path must stay Basic")
		}
	}
}

func TestFontPolicyDoesNotInheritLegacyRequired(t *testing.T) {
	svc, _, _ := studioServiceFixture(t)
	basic := domain.DeliveryPolicy{Tier: "basic", Revision: "v2-basic"}
	checks, _ := svc.fontChecksForPolicy(context.Background(), "docx", []byte("PK"), basic)
	var actual domain.Check
	for _, c := range checks {
		if c.ID == "font_actual_substitution" {
			actual = c
		}
	}
	if actual.ID == "" {
		t.Fatal("missing font_actual_substitution")
	}
	if actual.Required {
		t.Fatal("basic must not require font_actual_substitution")
	}
	if actual.Status == "passed" {
		t.Fatal("inventory must not mint substitution proof")
	}

	assured := domain.DeliveryPolicy{Tier: "assured", Revision: "v2-assured"}
	achecks, _ := svc.fontChecksForPolicy(context.Background(), "docx", []byte("PK"), assured)
	for _, c := range achecks {
		if c.ID == "font_actual_substitution" && !c.Required {
			t.Fatal("assured must require font_actual_substitution")
		}
		if c.ID == "font_actual_substitution" && c.Status == "passed" {
			t.Fatal("assured unsupported must not become passed")
		}
	}
}
