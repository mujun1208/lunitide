package officeapp

import (
	"context"
	"encoding/json"
	"testing"

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
	}
}
