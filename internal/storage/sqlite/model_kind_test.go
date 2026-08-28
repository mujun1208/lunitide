package sqlite

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/lunitide/lunitide/internal/domain/provider"
)

func TestModelKindsPersistAndKindDefaultIsGlobal(t *testing.T) {
	ctx := context.Background()
	store, err := Open(ctx, filepath.Join(t.TempDir(), "kinds.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	first := validProvider()
	first.Models = []provider.Model{
		{ModelID: "chat", DisplayName: "Chat", IsDefault: true, Kind: provider.KindLLM, KindDefault: true},
		{ModelID: "ocr", DisplayName: "OCR", Kind: provider.KindVision, KindDefault: true},
		{ModelID: "draw", DisplayName: "Draw", Kind: provider.KindImage, KindDefault: true},
		{ModelID: "clip", DisplayName: "Clip", Kind: provider.KindVideo, KindDefault: true},
	}
	created, err := store.Create(ctx, first)
	if err != nil {
		t.Fatal(err)
	}
	got, err := store.Get(ctx, created.ID)
	if err != nil {
		t.Fatal(err)
	}
	byID := map[string]provider.Model{}
	for _, m := range got.Models {
		byID[m.ModelID] = m
	}
	if byID["chat"].Kind != "" || !byID["chat"].KindDefault {
		t.Fatalf("llm row: %#v", byID["chat"])
	}
	if byID["ocr"].Kind != provider.KindVision || !byID["ocr"].KindDefault {
		t.Fatalf("vision row: %#v", byID["ocr"])
	}
	if byID["draw"].Kind != provider.KindImage || byID["clip"].Kind != provider.KindVideo {
		t.Fatalf("gen rows: %#v %#v", byID["draw"], byID["clip"])
	}

	second := validProvider()
	second.Name = "Backup Vision"
	second.BaseURL = "https://vision.example/v1"
	second.Models = []provider.Model{{ModelID: "ocr-2", DisplayName: "OCR 2", IsDefault: true, Kind: provider.KindVision, KindDefault: true}}
	if _, err = store.Create(ctx, second); err != nil {
		t.Fatal(err)
	}
	got, err = store.Get(ctx, created.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range got.Models {
		if m.ModelID == "ocr" && m.KindDefault {
			t.Fatal("previous vision kind_default must yield to the new catalog default")
		}
	}
	listed, err := store.List(ctx, provider.Filter{})
	if err != nil {
		t.Fatal(err)
	}
	catalog := provider.CatalogForKind(listed, provider.KindVision)
	if len(catalog) < 2 || catalog[0].Model.ModelID != "ocr-2" {
		t.Fatalf("vision catalog = %#v", catalog)
	}
}
