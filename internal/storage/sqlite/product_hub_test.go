package sqlite

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/lunitide/lunitide/internal/producthub"
)

func TestProductHubPersistApplyAndReload(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "product-hub.db")
	store, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	svc := producthub.New(store)
	svc.SetProductVersion("9.1.0")
	ed, err := svc.Generate(ctx, "boot")
	if err != nil {
		t.Fatal(err)
	}
	if ed.CardCount < 40 {
		t.Fatalf("cards=%d", ed.CardCount)
	}
	if len(ed.Findings) == 0 {
		t.Fatal("expected findings")
	}
	res, err := svc.Apply(ctx, ed.Findings[0].ErrorCode, ed.Findings[0].StableKey)
	if err != nil || !res.OK || res.Plan == "" {
		t.Fatalf("apply %#v err=%v", res, err)
	}
	if err := svc.TagSet(ctx, "feature.dialog.music.play", "alias", "放歌"); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}

	reopened, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = reopened.Close() })
	svc2 := producthub.New(reopened)
	svc2.SetProductVersion("9.1.0")
	card, ok, err := svc2.FeatureCard(ctx, "feature.dialog.music.play")
	if err != nil || !ok {
		t.Fatalf("reload card ok=%v err=%v", ok, err)
	}
	found := false
	for _, tag := range card.Tags {
		if tag == "alias:放歌" {
			found = true
		}
	}
	if !found {
		t.Fatalf("manual tag missing after reopen: %v", card.Tags)
	}
	logs, err := reopened.ProductHubLoadApplies(ctx)
	if err != nil || len(logs) == 0 {
		t.Fatalf("apply log missing err=%v n=%d", err, len(logs))
	}
	ov, err := svc2.Overview(ctx)
	if err != nil || ov.CardCount < 40 || ov.EditionID != ed.EditionID {
		t.Fatalf("overview %#v err=%v", ov, err)
	}
	latest, err := svc2.Latest(ctx)
	if err != nil || latest == nil || latest.ProductVersion != "9.1.0" || latest.EditionID != ed.EditionID || len(latest.Graph.Nodes) == 0 || latest.CatalogProbe.Total == 0 {
		t.Fatalf("stored version missing: %+v %v", latest, err)
	}
}
