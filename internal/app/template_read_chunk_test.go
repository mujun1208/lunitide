package app

import (
	"context"
	"encoding/base64"
	"testing"

	"github.com/lunitide/lunitide/internal/domain/asset"
)

func chunkEngine(tpl asset.AssetTemplate, files map[string][]byte) *Engine {
	return &Engine{assets: &openTemplateStore{tpl: tpl}, templateFiles: &memTemplateFiles{files: files}}
}

func officeTemplate() asset.AssetTemplate {
	return asset.AssetTemplate{
		ID: "01ARZ3NDEKTSV4RRFFQ69G5FAV", FileName: "deck.pptx", FilePath: "stored.pptx",
		TemplateType: asset.TemplateTypePPT, Status: asset.StatusEnabled,
	}
}

func TestTemplateReadChunkSlicesAndEnds(t *testing.T) {
	e := chunkEngine(officeTemplate(), map[string][]byte{"stored.pptx": []byte("0123456789")})
	out := handleTemplateReadChunk(e, context.Background(), validRequest("template.readChunk", `{"id":"01ARZ3NDEKTSV4RRFFQ69G5FAV","offset":0,"limit":4}`))
	if !out.OK {
		t.Fatalf("chunk %+v", out)
	}
	payload, _ := out.Payload.(map[string]any)
	if payload["fileName"] != "deck.pptx" || payload["contentBase64"] != base64.StdEncoding.EncodeToString([]byte("0123")) ||
		payload["nextOffset"] != int64(4) || payload["total"] != int64(10) || payload["eof"] != false {
		t.Fatalf("payload %#v", payload)
	}

	out = handleTemplateReadChunk(e, context.Background(), validRequest("template.readChunk", `{"id":"01ARZ3NDEKTSV4RRFFQ69G5FAV","offset":8,"limit":4}`))
	if !out.OK {
		t.Fatalf("tail %+v", out)
	}
	payload, _ = out.Payload.(map[string]any)
	if payload["contentBase64"] != base64.StdEncoding.EncodeToString([]byte("89")) ||
		payload["nextOffset"] != int64(10) || payload["eof"] != true {
		t.Fatalf("tail payload %#v", payload)
	}

	out = handleTemplateReadChunk(e, context.Background(), validRequest("template.readChunk", `{"id":"01ARZ3NDEKTSV4RRFFQ69G5FAV","offset":10}`))
	if !out.OK {
		t.Fatalf("beyond end %+v", out)
	}
	payload, _ = out.Payload.(map[string]any)
	if payload["contentBase64"] != "" || payload["nextOffset"] != int64(10) || payload["total"] != int64(10) || payload["eof"] != true {
		t.Fatalf("beyond end payload %#v", payload)
	}
}

func TestTemplateReadChunkDefaultsLimitToOneMiB(t *testing.T) {
	content := make([]byte, 1048576+5)
	e := chunkEngine(officeTemplate(), map[string][]byte{"stored.pptx": content})
	out := handleTemplateReadChunk(e, context.Background(), validRequest("template.readChunk", `{"id":"01ARZ3NDEKTSV4RRFFQ69G5FAV","offset":0}`))
	if !out.OK {
		t.Fatalf("default limit %+v", out)
	}
	payload, _ := out.Payload.(map[string]any)
	if payload["nextOffset"] != int64(1048576) || payload["total"] != int64(1048581) || payload["eof"] != false {
		t.Fatalf("default limit payload %#v", payload)
	}
}

func TestTemplateReadChunkRejectsInvalidParams(t *testing.T) {
	e := chunkEngine(officeTemplate(), map[string][]byte{"stored.pptx": []byte("x")})
	for _, body := range []string{
		`{"id":"01ARZ3NDEKTSV4RRFFQ69G5FAV","offset":0,"limit":4194305}`,
		`{"id":"01ARZ3NDEKTSV4RRFFQ69G5FAV","offset":-1}`,
		`{"id":"01ARZ3NDEKTSV4RRFFQ69G5FAV","offset":0,"limit":-4}`,
		`{"id":"not-a-ulid","offset":0}`,
		`{"offset":0}`,
	} {
		out := handleTemplateReadChunk(e, context.Background(), validRequest("template.readChunk", body))
		if out.OK {
			t.Fatalf("accepted invalid body %s: %+v", body, out)
		}
	}
}

func TestTemplateReadChunkEnforcesOfficeRules(t *testing.T) {
	files := map[string][]byte{"stored.pptx": []byte("x"), "note.txt": []byte("x")}
	for _, tpl := range []asset.AssetTemplate{
		{FileName: "deck.pptx", FilePath: "stored.pptx", TemplateType: asset.TemplateTypePPT, Status: asset.StatusDraft},
		{FileName: "note.txt", FilePath: "note.txt", TemplateType: asset.TemplateTypeDocument, Status: asset.StatusEnabled},
		{OrgID: "other", FileName: "deck.pptx", FilePath: "stored.pptx", TemplateType: asset.TemplateTypePPT, Status: asset.StatusEnabled},
		{FileName: "../deck.pptx", FilePath: "stored.pptx", TemplateType: asset.TemplateTypePPT, Status: asset.StatusEnabled},
	} {
		e := chunkEngine(tpl, files)
		out := handleTemplateReadChunk(e, context.Background(), validRequest("template.readChunk", `{"id":"01ARZ3NDEKTSV4RRFFQ69G5FAV","offset":0}`))
		if out.OK {
			t.Fatalf("accepted %#v", tpl)
		}
	}
}

func TestTemplateReadChunkRequiresStoreAndFile(t *testing.T) {
	e := &Engine{assets: &openTemplateStore{tpl: officeTemplate()}}
	out := handleTemplateReadChunk(e, context.Background(), validRequest("template.readChunk", `{"id":"01ARZ3NDEKTSV4RRFFQ69G5FAV","offset":0}`))
	if out.OK {
		t.Fatal("accepted missing template files store")
	}
}
