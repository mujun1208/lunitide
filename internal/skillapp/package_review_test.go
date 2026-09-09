package skillapp

import (
	"encoding/json"
	"testing"

	"github.com/lunitide/lunitide/internal/domain/skill"
)

func TestPackageReviewRejectsSpecialResourceOverlay(t *testing.T) {
	for _, entry := range []string{"SKILL.md", "manifest.json"} {
		t.Run(entry, func(t *testing.T) {
			service := New(nil, nil)
			service.SetPackageRoot(t.TempDir())
			digest, err := service.StorePackageFiles(map[string][]byte{entry: []byte("original imported source")})
			if err != nil {
				t.Fatal(err)
			}
			manifest, _ := json.Marshal(map[string]any{"prompt": "adapted working agreement", "localPackageDigest": digest, "files": map[string]string{"upstream/" + entry: "different explicitly stored file"}})
			if _, err = service.PackageFiles(skill.Skill{ManifestJSON: string(manifest)}); err == nil {
				t.Fatal("imported special entry silently replaced an existing different resource")
			}
		})
	}
}

func TestPackageReviewPortablePathRejectsControlCharacters(t *testing.T) {
	for _, p := range []string{"references/a\nb.txt", "scripts/a\rb.py", "assets/a\tb.json", "reference/a\x7fb.md"} {
		if PackageFilePath(p) {
			t.Fatalf("nonportable control character accepted: %q", p)
		}
	}
}

func TestPackageReviewUploadedJSONCannotClaimVerifiedIdentity(t *testing.T) {
	raw := []byte(`{"name":"uploaded","displayName":"Uploaded","version":"1.0.0","status":"published","signature":"unverified-file-claim","publisherId":"01ARZ3NDEKTSV4RRFFQ69G5FAV","manifestJson":"{\"prompt\":\"review this imported draft\"}"}`)
	sk, _, err := decodeUploadedSkill("uploaded.json", raw)
	if err != nil {
		t.Fatal(err)
	}
	if sk.Signature != nil || sk.PublisherID != nil {
		t.Fatal("unverified uploaded identity retained as product signature/publisher")
	}
}
