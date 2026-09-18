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

func TestMemorySettingsR3EmptyGetAndScopeCAS(t *testing.T) {
	ctx := context.Background()
	s, err := storage.OpenTemplated(ctx, filepath.Join(t.TempDir(), "settings-r3.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	e := NewEngine(nil, "test")
	e.memoryOps = m8app.NewMemoryOpsService(s)
	get := e.Handle(ctx, nominationRequest("memory.settings.get", `{}`))
	if !get.OK {
		t.Fatalf("r3 get: %+v", get)
	}
	var initial memorySettingsDTO
	if err = json.Unmarshal(mustJSON(get.Payload), &initial); err != nil {
		t.Fatal(err)
	}
	if initial.CaptureMode != "auto" || !initial.PersonalMemoryEnabled || !initial.ProjectMemoryEnabled || initial.Revision != 1 {
		t.Fatalf("r3 defaults %+v", initial)
	}
	update := e.Handle(ctx, nominationRequest("memory.settings.update", `{"captureMode":"manual","personalMemoryEnabled":true,"projectMemoryEnabled":false,"expectedRevision":1}`))
	if !update.OK {
		t.Fatalf("r3 update: %+v", update)
	}
	var saved memorySettingsDTO
	if err = json.Unmarshal(mustJSON(update.Payload), &saved); err != nil {
		t.Fatal(err)
	}
	if saved.CaptureMode != "manual" || saved.MemoryEnabled != true || saved.ProjectMemoryEnabled || saved.Revision != 2 {
		t.Fatalf("r3 saved %+v", saved)
	}
	stale := e.Handle(ctx, nominationRequest("memory.settings.update", `{"captureMode":"off","personalMemoryEnabled":true,"projectMemoryEnabled":false,"expectedRevision":1}`))
	if stale.OK || stale.Error.Code != "MEMORY_SETTINGS_CONFLICT" {
		t.Fatalf("stale r3: %+v", stale)
	}
	legacy := e.Handle(ctx, nominationRequest("memory.settings.get", `{"subjectId":"local-user"}`))
	if !legacy.OK {
		t.Fatalf("legacy get: %+v", legacy)
	}
	var projected memorySettingsDTO
	if err = json.Unmarshal(mustJSON(legacy.Payload), &projected); err != nil {
		t.Fatal(err)
	}
	if projected.CaptureMode != "manual" || !projected.MemoryEnabled || projected.ProjectMemoryEnabled {
		t.Fatalf("legacy projection %+v", projected)
	}
	growth := projected.GrowthDays
	omit := e.Handle(ctx, nominationRequest("memory.settings.update", `{"subjectId":"local-user","memoryEnabled":true,"autoNominate":false,"growthDays":`+itoa(growth)+`,"expectedVersion":"`+projected.Version+`"}`))
	if !omit.OK {
		t.Fatalf("legacy omit: %+v", omit)
	}
	var afterOmit memorySettingsDTO
	if err = json.Unmarshal(mustJSON(omit.Payload), &afterOmit); err != nil {
		t.Fatal(err)
	}
	if afterOmit.ProjectMemoryEnabled {
		t.Fatalf("legacy update overwrote project scope %+v", afterOmit)
	}
}
