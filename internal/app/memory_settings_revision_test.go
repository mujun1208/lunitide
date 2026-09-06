package app

import (
	"context"
	"encoding/json"
	"github.com/lunitide/lunitide/internal/m8app"
	storage "github.com/lunitide/lunitide/internal/storage/sqlite"
	"path/filepath"
	"strings"
	"testing"
)

func TestMemorySettingsBridgeUsesActualRevision(t *testing.T) {
	ctx := context.Background()
	s, err := storage.OpenTemplated(ctx, filepath.Join(t.TempDir(), "settings.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	e := NewEngine(nil, "test")
	e.memoryOps = m8app.NewMemoryOpsService(s)
	get := e.Handle(ctx, nominationRequest("memory.settings.get", `{"subjectId":"local-user"}`))
	if !get.OK {
		t.Fatalf("get: %+v", get)
	}
	var initial memorySettingsDTO
	if err = json.Unmarshal(mustJSON(get.Payload), &initial); err != nil {
		t.Fatal(err)
	}
	if len(initial.Version) != 64 || initial.UpdatedAt == "" {
		t.Fatalf("implicit defaults invalid: %+v", initial)
	}
	payload := `{"subjectId":"local-user","memoryEnabled":false,"autoNominate":true,"growthDays":30,"expectedVersion":"` + initial.Version + `"}`
	first := e.Handle(ctx, nominationRequest("memory.settings.update", payload))
	if !first.OK {
		t.Fatalf("update: %+v", first)
	}
	var saved memorySettingsDTO
	if err = json.Unmarshal(mustJSON(first.Payload), &saved); err != nil {
		t.Fatal(err)
	}
	if saved.Version == initial.Version || saved.GrowthDays != 30 {
		t.Fatalf("stale response %+v", saved)
	}
	replay := e.Handle(ctx, nominationRequest("memory.settings.update", payload))
	if replay.OK || replay.Error.Code != "MEMORY_SETTINGS_CONFLICT" {
		t.Fatalf("stale update: %+v", replay)
	}
	foreign := e.Handle(ctx, nominationRequest("memory.settings.update", strings.Replace(payload, "local-user", "foreign", 1)))
	if foreign.OK || foreign.Error.Code != "M8-009" {
		t.Fatalf("foreign settings: %+v", foreign)
	}
}
