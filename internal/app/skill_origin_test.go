package app

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/lunitide/lunitide/internal/skillapp"
)

func TestSkillDraftEditKeepsOriginalCreationConversation(t *testing.T) {
	e, _, sk := draftTrialFixture(t, "Original contract")
	var original map[string]any
	_ = json.Unmarshal([]byte(sk.ManifestJSON), &original)
	original["originSessionId"] = chatAttachmentSessionID
	data, _ := json.Marshal(original)
	sk.ManifestJSON = string(data)
	// Set origin through the same creation path, rather than mutable update.
	svc := e.skills.(*skillapp.Service)
	sk.ID = ""
	sk.Name = "origin-edit-fixture"
	created, err := svc.Create(context.Background(), sk)
	if err != nil {
		t.Fatal(err)
	}
	for _, raw := range []string{`{"prompt":"Improved from trial"}`, `{"prompt":"Second improvement","originSessionId":"01ARZ3NDEKTSV4RRFFQ69G5FAY","localPackageDigest":"explicit-replacement"}`} {
		updated, err := svc.UpdateFields(context.Background(), created.ID, nil, nil, nil, &raw, nil, nil, created.Rev)
		if err != nil {
			t.Fatal(err)
		}
		var got map[string]any
		_ = json.Unmarshal([]byte(updated.ManifestJSON), &got)
		if got["originSessionId"] != chatAttachmentSessionID {
			t.Fatal("draft edit moved or lost creation conversation")
		}
		if got["prompt"] == "Second improvement" && got["localPackageDigest"] != "explicit-replacement" {
			t.Fatal("unrelated source metadata silently changed")
		}
		created = *updated
	}
}
