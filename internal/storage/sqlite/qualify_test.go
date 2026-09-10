package sqlite

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/lunitide/lunitide/internal/modelfit"
)

func TestModelFitQualificationDefaultsUntested(t *testing.T) {
	ctx := context.Background()
	db, err := OpenTemplated(ctx, filepath.Join(t.TempDir(), "qualify.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	got, err := db.GetModelFitQualification(ctx, "sess", "deepseek", "deepseek-chat", modelfit.CodecDeepSeekV1)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != modelfit.QualifyUntested || got.Adopted {
		t.Fatalf("missing row must be untested not adopted: %+v", got)
	}
	got.Status = modelfit.QualifyFixturePass
	got.Evidence = "offline codec fixture"
	if err := db.PutModelFitQualification(ctx, "sess", got); err != nil {
		t.Fatal(err)
	}
	saved, err := db.GetModelFitQualification(ctx, "sess", "deepseek", "deepseek-chat", modelfit.CodecDeepSeekV1)
	if err != nil || saved.Status != modelfit.QualifyFixturePass || saved.Adopted {
		t.Fatalf("fixture_pass must not become adopted: %+v %v", saved, err)
	}
}
