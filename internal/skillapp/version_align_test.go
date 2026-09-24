package skillapp

import (
	"context"
	"errors"
	"testing"

	"github.com/lunitide/lunitide/internal/domain/skill"
)

func TestPublishReplacesRegisteredVersionFromManifest(t *testing.T) {
	store := newMemSkillStore()
	id := "01ARZ3NDEKTSV4RRFFQ69G5FAV"
	store.byID[id] = skill.Skill{
		ID: id, Name: "poc-fast-build", DisplayName: "POC 快速构建",
		Description: "draft", Version: "1.0.0", Status: skill.SkillStatusDraft, Rev: 6,
		EntryPoint:   "SKILL.md",
		ManifestJSON: `{"version":"2.0.0","prompt":"rules"}`,
		Permissions:  []skill.PermissionLevel{skill.PermissionReadOnly},
	}
	svc := New(store, store)
	if err := svc.Publish(context.Background(), id); err != nil {
		t.Fatal(err)
	}
	got, err := svc.Get(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != id || got.Status != skill.SkillStatusPublished || got.Version != "2.0.0" {
		t.Fatalf("publish must keep the same skill and replace 1.0.0 with the manifest version, got %+v", got)
	}
}

func TestAlignRegisteredVersionReplacesPublishedLabel(t *testing.T) {
	store := newMemSkillStore()
	id := "01ARZ3NDEKTSV4RRFFQ69G5FAV"
	store.byID[id] = skill.Skill{
		ID: id, Name: "poc-fast-build", DisplayName: "POC 快速构建",
		Description: "live", Version: "1.0.0", Status: skill.SkillStatusPublished, Rev: 6,
		EntryPoint:   "SKILL.md",
		ManifestJSON: `{"version":"2.0","prompt":"rules"}`,
		Permissions:  []skill.PermissionLevel{skill.PermissionReadOnly},
	}
	svc := New(store, store)
	got, err := svc.AlignRegisteredVersion(context.Background(), id, "")
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != id || got.Status != skill.SkillStatusPublished || got.Version != "2.0" {
		t.Fatalf("publishing again must replace the registered 1.0.0 label on the same skill, got %+v", got)
	}
	listed, err := svc.List(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	if len(listed) != 1 || listed[0].Version != "2.0" {
		t.Fatalf("skill center list must show one row at 2.0, got %+v", listed)
	}
}

func TestAlignRegisteredVersionUsesExplicitVersion(t *testing.T) {
	store := newMemSkillStore()
	id := "01ARZ3NDEKTSV4RRFFQ69G5FAV"
	store.byID[id] = skill.Skill{
		ID: id, Name: "poc-fast-build", DisplayName: "POC 快速构建",
		Description: "live", Version: "1.0.0", Status: skill.SkillStatusPublished, Rev: 3,
		EntryPoint:   "SKILL.md",
		ManifestJSON: `{"version":"1.0.0","prompt":"rules"}`,
		Permissions:  []skill.PermissionLevel{skill.PermissionReadOnly},
	}
	svc := New(store, store)
	got, err := svc.AlignRegisteredVersion(context.Background(), id, "2.0")
	if err != nil {
		t.Fatal(err)
	}
	if got.Version != "2.0" || got.ID != id {
		t.Fatalf("explicit 2.0 must replace the registered label, got %+v", got)
	}
}

func TestPublishKeepsDraftWhenManifestVersionIsTaken(t *testing.T) {
	store := newMemSkillStore()
	draft := "01ARZ3NDEKTSV4RRFFQ69G5FAV"
	taken := "01ARZ3NDEKTSV4RRFFQ69G5FAW"
	store.byID[draft] = skill.Skill{
		ID: draft, Name: "poc-fast-build", DisplayName: "POC", Version: "1.0.0",
		Status: skill.SkillStatusDraft, Rev: 4, EntryPoint: "SKILL.md",
		ManifestJSON: `{"version":"2.0.0","prompt":"rules"}`,
		Permissions:  []skill.PermissionLevel{skill.PermissionReadOnly},
	}
	store.byID[taken] = skill.Skill{
		ID: taken, Name: "poc-fast-build", DisplayName: "POC", Version: "2.0.0",
		Status: skill.SkillStatusPublished, Rev: 1, EntryPoint: "SKILL.md",
		ManifestJSON: `{"version":"2.0.0","prompt":"rules"}`,
		Permissions:  []skill.PermissionLevel{skill.PermissionReadOnly},
	}
	svc := New(store, store)
	err := svc.Publish(context.Background(), draft)
	if !errors.Is(err, ErrSkillAlreadyExists) {
		t.Fatalf("publish must refuse a taken manifest version, got %v", err)
	}
	got, gerr := svc.Get(context.Background(), draft)
	if gerr != nil || got.Status != skill.SkillStatusDraft || got.Version != "1.0.0" {
		t.Fatalf("failed publish must leave the draft at 1.0.0, got %+v %v", got, gerr)
	}
}

func TestAlignRegisteredVersionRejectsTakenLabel(t *testing.T) {
	store := newMemSkillStore()
	older := "01ARZ3NDEKTSV4RRFFQ69G5FAV"
	newer := "01ARZ3NDEKTSV4RRFFQ69G5FAW"
	store.byID[older] = skill.Skill{
		ID: older, Name: "poc-fast-build", DisplayName: "POC", Version: "1.0.0",
		Status: skill.SkillStatusPublished, Rev: 1, EntryPoint: "SKILL.md",
		ManifestJSON: `{"version":"2.0.0","prompt":"rules"}`,
		Permissions:  []skill.PermissionLevel{skill.PermissionReadOnly},
	}
	store.byID[newer] = skill.Skill{
		ID: newer, Name: "poc-fast-build", DisplayName: "POC", Version: "2.0.0",
		Status: skill.SkillStatusPublished, Rev: 1, EntryPoint: "SKILL.md",
		ManifestJSON: `{"version":"2.0.0","prompt":"rules"}`,
		Permissions:  []skill.PermissionLevel{skill.PermissionReadOnly},
	}
	svc := New(store, store)
	_, err := svc.AlignRegisteredVersion(context.Background(), older, "")
	if !errors.Is(err, ErrSkillAlreadyExists) {
		t.Fatalf("a taken name+version must stay put, got %v", err)
	}
	got, gerr := svc.Get(context.Background(), older)
	if gerr != nil || got.Version != "1.0.0" {
		t.Fatalf("rejected upgrade must keep 1.0.0, got %+v %v", got, gerr)
	}
}

func TestAlignRegisteredVersionKeepsNewerLabel(t *testing.T) {
	store := newMemSkillStore()
	id := "01ARZ3NDEKTSV4RRFFQ69G5FAV"
	store.byID[id] = skill.Skill{
		ID: id, Name: "poc-fast-build", DisplayName: "POC 快速构建",
		Description: "live", Version: "2.0.0", Status: skill.SkillStatusPublished, Rev: 7,
		EntryPoint:   "SKILL.md",
		ManifestJSON: `{"version":"1.0.0","prompt":"rules"}`,
		Permissions:  []skill.PermissionLevel{skill.PermissionReadOnly},
	}
	svc := New(store, store)
	got, err := svc.AlignRegisteredVersion(context.Background(), id, "1.5.0")
	if err != nil {
		t.Fatal(err)
	}
	if got.Version != "2.0.0" || got.Rev != 7 {
		t.Fatalf("a lower version must leave the registered label, got %+v", got)
	}
}
