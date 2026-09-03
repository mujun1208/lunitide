package m8core

import (
	"encoding/json"
	"strings"
	"testing"
)

func sampleDoc() KBDocument {
	return KBDocument{
		DocumentID: "01ARZ3NDEKTSV4RRFFQ69G5FAV",
		Version:    1,
		SHA256:     strings.Repeat("ab", 32),
		CreatedAt:  "2026-09-03T00:00:00Z",
	}
}

func TestBuildChunkProjectionKeepsEmptyBodyAndDefaultLocator(t *testing.T) {
	doc := sampleDoc()
	id := "01ARZ3NDEKTSV4RRFFQ69G5FAW"
	proj, err := BuildChunkProjection(doc, []string{id})
	if err != nil {
		t.Fatal(err)
	}
	if len(proj.Chunks) != 1 {
		t.Fatalf("chunks = %d", len(proj.Chunks))
	}
	c := proj.Chunks[0]
	if c.Body != "" {
		t.Fatalf("legacy projection must keep empty body, got %q", c.Body)
	}
	var loc map[string]any
	if err := json.Unmarshal([]byte(c.LocatorJSON), &loc); err != nil {
		t.Fatal(err)
	}
	if loc["documentId"] != doc.DocumentID || loc["ordinal"] != float64(0) {
		t.Fatalf("locator = %s", c.LocatorJSON)
	}
	if _, ok := loc["ata"]; ok {
		t.Fatal("legacy locator must not carry rich fields")
	}
}

func TestBuildChunkProjectionFromChunksKeepsBodyAndRichLocator(t *testing.T) {
	doc := sampleDoc()
	rich := `{"documentId":"01ARZ3NDEKTSV4RRFFQ69G5FAV","version":1,"ordinal":0,"revision":"42","ata":"32-11","tails":["B-1000"]}`
	proj, err := BuildChunkProjectionFromChunks(doc, []KBChunk{{
		ChunkID: "01ARZ3NDEKTSV4RRFFQ69G5FAX",
		Body:    "Gear retraction fault isolation.",
		LocatorJSON: rich,
	}})
	if err != nil {
		t.Fatal(err)
	}
	c := proj.Chunks[0]
	if c.Body != "Gear retraction fault isolation." {
		t.Fatalf("body lost: %q", c.Body)
	}
	if c.LocatorJSON != rich {
		t.Fatalf("locator overwritten: %s", c.LocatorJSON)
	}
	if c.ContentDigest == "" || len(c.ContentDigest) != 64 {
		t.Fatalf("digest = %q", c.ContentDigest)
	}
	legacy, err := BuildChunkProjection(doc, []string{c.ChunkID})
	if err != nil {
		t.Fatal(err)
	}
	if c.ContentDigest == legacy.Chunks[0].ContentDigest {
		t.Fatal("body digest must differ from id-only digest")
	}
}

func TestBuildChunkProjectionFromChunksRejectsEmptyBody(t *testing.T) {
	doc := sampleDoc()
	if _, err := BuildChunkProjectionFromChunks(doc, []KBChunk{{
		ChunkID: "01ARZ3NDEKTSV4RRFFQ69G5FAY",
		Body:    "   ",
	}}); err == nil {
		t.Fatal("empty body must fail")
	}
}

func TestBuildChunkProjectionFromChunksRejectsInvalidLocator(t *testing.T) {
	doc := sampleDoc()
	if _, err := BuildChunkProjectionFromChunks(doc, []KBChunk{{
		ChunkID:     "01ARZ3NDEKTSV4RRFFQ69G5FAZ",
		Body:        "ok",
		LocatorJSON: "{not-json",
	}}); err == nil {
		t.Fatal("invalid locator must fail")
	}
}
