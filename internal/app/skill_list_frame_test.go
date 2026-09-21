package app

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lunitide/lunitide/internal/domain/skill"
	"github.com/lunitide/lunitide/internal/ipc"
	"github.com/lunitide/lunitide/internal/skillapp"
	storage "github.com/lunitide/lunitide/internal/storage/sqlite"
)

// A user who installs most of the market ends up with a large library, and
// skill.list answers with every row in one frame. Cross MaxFrameSize and the
// response is not truncated, it is dropped: the renderer's library stays empty,
// so every market card computes "not installed" and shows a "+" that appears to
// do nothing when clicked. Measure the real handler against the real frame
// limit so growth in the catalog cannot quietly reintroduce that.
func TestSkillListResponseFitsOneFrameWithFullCatalog(t *testing.T) {
	ctx := context.Background()
	store, err := storage.OpenTemplated(ctx, filepath.Join(t.TempDir(), "skills.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	svc := skillapp.New(store, store)
	e := NewEngine(nil, "frame")
	e.skills = svc
	e.SetPersistDir(t.TempDir())

	installed := 0
	for _, tpl := range skillapp.Catalog() {
		if _, err := svc.InstallFromCatalog(ctx, tpl.ID); err != nil {
			continue
		}
		installed++
	}
	if installed < 100 {
		t.Fatalf("installed only %d templates; the catalog should offer far more", installed)
	}

	resp := handleSkillList(e, ctx, packageRequest(t, "skill.list", map[string]any{}))
	if !resp.OK {
		t.Fatalf("skill.list failed with a full library: %+v", resp.Error)
	}
	raw, err := json.Marshal(resp)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("installed=%d skill.list response=%d bytes (frame limit %d)", installed, len(raw), ipc.MaxFrameSize)
	if len(raw) > ipc.MaxFrameSize {
		t.Fatalf("skill.list response=%d bytes exceeds frame limit %d: the library never reaches the renderer", len(raw), ipc.MaxFrameSize)
	}
}

// When a library really is too heavy for one frame, the response must shed
// manifest text and keep every row. The opposite trade — fewer rows, full
// manifests — is what froze market cards on "+": the renderer decides "installed"
// from the presence of a row, and a row it never receives can never flip.
func TestSkillListShedsManifestNotRowsWhenOversized(t *testing.T) {
	ctx := context.Background()
	store, err := storage.OpenTemplated(ctx, filepath.Join(t.TempDir(), "heavy.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	e := NewEngine(nil, "heavy")
	e.skills = skillapp.New(store, store)
	e.SetPersistDir(t.TempDir())

	// Each manifest is individually capped at 64 KiB, so an oversized library is
	// reached by row count, not by one giant row: ~70 near-cap manifests is past
	// the frame budget.
	bulk := strings.Repeat("a", 60_000)
	const total = 70
	for i := 0; i < total; i++ {
		if _, err := store.CreateSkill(ctx, skill.Skill{
			Name:         fmt.Sprintf("heavy-%02d", i),
			DisplayName:  fmt.Sprintf("Heavy %02d", i),
			Description:  "row",
			Version:      "1.0.0",
			Status:       skill.SkillStatusPublished,
			Permissions:  []skill.PermissionLevel{skill.PermissionReadOnly},
			EntryPoint:   "SKILL.md",
			ManifestJSON: `{"prompt":"` + bulk + `","triggers":["t"]}`,
		}); err != nil {
			t.Fatal(err)
		}
	}

	resp := handleSkillList(e, ctx, packageRequest(t, "skill.list", map[string]any{}))
	if !resp.OK {
		t.Fatalf("skill.list failed on a heavy library: %+v", resp.Error)
	}
	raw, err := json.Marshal(resp)
	if err != nil {
		t.Fatal(err)
	}
	if len(raw) > ipc.MaxFrameSize {
		t.Fatalf("response=%d bytes still exceeds the frame limit %d", len(raw), ipc.MaxFrameSize)
	}
	var out struct {
		Items []struct {
			Name         string `json:"name"`
			ManifestJSON string `json:"manifestJson"`
		} `json:"items"`
		ManifestTrimmed bool `json:"manifestTrimmed"`
	}
	payload, err := json.Marshal(resp.Payload)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(payload, &out); err != nil {
		t.Fatal(err)
	}
	if len(out.Items) != total {
		t.Fatalf("got %d rows, want all %d: the handler dropped skills instead of manifest text", len(out.Items), total)
	}
	if !out.ManifestTrimmed {
		t.Fatal("manifestTrimmed not set: the client cannot tell withheld text from an empty manifest")
	}
	for _, item := range out.Items {
		if item.ManifestJSON != "" {
			t.Fatalf("%s kept its manifest while the payload was over budget", item.Name)
		}
	}
}
