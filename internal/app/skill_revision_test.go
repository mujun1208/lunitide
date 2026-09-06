package app

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/lunitide/lunitide/internal/bridge"
	"github.com/lunitide/lunitide/internal/domain/skill"
	"github.com/lunitide/lunitide/internal/skillapp"
	storage "github.com/lunitide/lunitide/internal/storage/sqlite"
)

func TestSkillClientRevisionGuardsUpdateAndDelete(t *testing.T) {
	ctx := context.Background()
	store, err := storage.OpenTemplated(ctx, filepath.Join(t.TempDir(), "skill.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	e := NewEngine(nil, "test")
	e.skills = skillapp.New(store, store)
	sk, err := e.skills.Create(ctx, skill.Skill{Name: "revision-fixture", DisplayName: "original", Version: "1.0.0", Permissions: []skill.PermissionLevel{skill.PermissionReadOnly}, EntryPoint: "builtin:summarize-input", ManifestJSON: "{}"})
	if err != nil {
		t.Fatal(err)
	}
	read := handleSkillGet(e, ctx, lifecyclePayload(t, map[string]any{"id": sk.ID}))
	if !read.OK {
		t.Fatal(read.Error)
	}
	raw, _ := json.Marshal(read.Payload)
	var dto struct {
		Rev *int64 `json:"rev"`
	}
	if err := json.Unmarshal(raw, &dto); err != nil || dto.Rev == nil || *dto.Rev != 0 {
		t.Fatalf("DTO missing actual initial revision: %s", raw)
	}
	a := handleSkillUpdate(e, ctx, lifecyclePayload(t, map[string]any{"id": sk.ID, "displayName": "writer A", "expectedVersion": 0}))
	if !a.OK {
		t.Fatalf("A: %+v", a.Error)
	}
	for _, method := range []string{"update", "delete"} {
		req := lifecyclePayload(t, map[string]any{"id": sk.ID, "expectedVersion": 0})
		var response bridge.Response
		if method == "update" {
			response = handleSkillUpdate(e, ctx, req)
		} else {
			response = handleSkillDelete(e, ctx, req)
		}
		if response.OK || response.Error.Code != "SKILL_VERSION_CONFLICT" {
			t.Fatalf("stale %s: %+v", method, response)
		}
	}
	current, err := e.skills.Get(ctx, sk.ID)
	if err != nil || current.DisplayName != "writer A" || current.Rev != 1 {
		t.Fatalf("stale operation changed data: %+v %v", current, err)
	}
	if response := handleSkillDelete(e, ctx, lifecyclePayload(t, map[string]any{"id": sk.ID})); response.OK || response.Error.Code != "BRIDGE_SCHEMA_INVALID" {
		t.Fatal("missing version accepted")
	}
	if response := handleSkillDelete(e, ctx, lifecyclePayload(t, map[string]any{"id": sk.ID, "expectedVersion": 1})); !response.OK {
		t.Fatalf("current revision delete: %+v", response.Error)
	}
}
