package app

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lunitide/lunitide/internal/bridge"
	"github.com/lunitide/lunitide/internal/domain/skill"
	"github.com/lunitide/lunitide/internal/skillapp"
	storage "github.com/lunitide/lunitide/internal/storage/sqlite"
)

func skillPackageFixture(t *testing.T) (*Engine, *skillapp.Service) {
	t.Helper()
	ctx := context.Background()
	store, err := storage.OpenTemplated(ctx, filepath.Join(t.TempDir(), "skills.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	e := NewEngine(nil, "test")
	svc := skillapp.New(store, store)
	e.skills = svc
	e.SetPersistDir(t.TempDir())
	return e, svc
}
func packageRequest(t *testing.T, method string, data map[string]any) bridge.Request {
	t.Helper()
	r := lifecyclePayload(t, data)
	r.Method = method
	return r
}
func packagePayload(t *testing.T, response bridge.Response) map[string]any {
	t.Helper()
	if !response.OK {
		t.Fatalf("package response: %+v", response.Error)
	}
	raw, err := json.Marshal(response.Payload)
	if err != nil {
		t.Fatal(err)
	}
	var out map[string]any
	if err = json.Unmarshal(raw, &out); err != nil {
		t.Fatal(err)
	}
	return out
}
func packageZIP(t *testing.T, files map[string][]byte) []byte {
	t.Helper()
	var b bytes.Buffer
	w := zip.NewWriter(&b)
	for name, data := range files {
		f, err := w.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = f.Write(data); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

func TestSkillPackageUploadActualFilesDraftReplayAndRead(t *testing.T) {
	e, svc := skillPackageFixture(t)
	ctx := context.Background()
	prompt := "---\nname: weekly-report\ndescription: Produce a weekly report from actual task records.\n---\n核对真实来源，再生成周报。\n"
	reference := strings.Repeat("依据不能丢失🙂；", 2600)
	zipped := packageZIP(t, map[string][]byte{"weekly-report/SKILL.md": []byte(prompt), "weekly-report/scripts/check.py": []byte("print('review only')\n"), "weekly-report/references/rules.md": []byte(reference), "weekly-report/assets/sample.bin": {0, 255, 128, 12}})
	digest := sha256.Sum256(zipped)
	begin := packagePayload(t, handleSkillPackageUpload(e, ctx, packageRequest(t, "skill.package.upload.begin", map[string]any{"name": "weekly.skill", "size": len(zipped), "sha256": hex.EncodeToString(digest[:])})))
	id := begin["uploadId"].(string)
	if response := handleSkillPackageUpload(e, ctx, packageRequest(t, "skill.package.upload.commit", map[string]any{"uploadId": id})); response.OK {
		t.Fatal("incomplete upload committed")
	}
	for offset := 0; offset < len(zipped); {
		end := min(offset+77, len(zipped))
		request := packageRequest(t, "skill.package.upload.chunk", map[string]any{"uploadId": id, "offset": offset, "dataBase64": base64.StdEncoding.EncodeToString(zipped[offset:end])})
		page := packagePayload(t, handleSkillPackageUpload(e, ctx, request))
		if int(page["received"].(float64)) != end {
			t.Fatal(page)
		}
		packagePayload(t, handleSkillPackageUpload(e, ctx, request))
		offset = end
	}
	commitReq := packageRequest(t, "skill.package.upload.commit", map[string]any{"uploadId": id})
	committed := packagePayload(t, handleSkillPackageUpload(e, ctx, commitReq))
	replay := packagePayload(t, handleSkillPackageUpload(e, ctx, commitReq))
	created := committed["skill"].(map[string]any)
	skillID := created["id"].(string)
	if created["status"] != "draft" || replay["skill"].(map[string]any)["id"] != skillID {
		t.Fatalf("draft/replay: %#v %#v", committed, replay)
	}
	if _, err := svc.Invoke(ctx, skillID, "01ARZ3NDEKTSV4RRFFQ69G5FAV", "test", "full_access"); err == nil {
		t.Fatal("unpublished skill entered normal invocation")
	}
	listing := packagePayload(t, handleSkillPackageList(e, ctx, packageRequest(t, "skill.package.list", map[string]any{"skillId": skillID})))
	root := listing["rootPath"].(string)
	revision := listing["revision"].(string)
	for _, rel := range []string{"SKILL.md", "manifest.json", "scripts/check.py", "references/rules.md", "assets/sample.bin", "upstream/SKILL.md"} {
		if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(rel))); err != nil {
			t.Fatalf("missing actual resource %s: %v", rel, err)
		}
	}
	var read strings.Builder
	offset := 0
	for {
		page := packagePayload(t, handleSkillPackageRead(e, ctx, packageRequest(t, "skill.package.read", map[string]any{"skillId": skillID, "path": "references/rules.md", "offset": offset, "limit": 1025, "expectedRevision": revision})))
		read.WriteString(page["content"].(string))
		next := int(page["nextOffset"].(float64))
		if page["eof"].(bool) {
			break
		}
		if next <= offset {
			t.Fatal("read stuck")
		}
		offset = next
	}
	if read.String() != reference {
		t.Fatal("paged UTF-8 reference changed")
	}
	binary := packagePayload(t, handleSkillPackageRead(e, ctx, packageRequest(t, "skill.package.read", map[string]any{"skillId": skillID, "path": "assets/sample.bin"})))
	if binary["encoding"] != "binary" || binary["content"] != "" {
		t.Fatal(binary)
	}
	args, _ := json.Marshal(map[string]any{"skillId": skillID, "path": "scripts/check.py"})
	view, err := e.invokeSkillViewTool(ctx, args)
	if err != nil || !strings.Contains(view.Output, "review only") {
		t.Fatalf("model cannot read actual script: %s %v", view.Output, err)
	}
	if err = svc.Publish(ctx, skillID); err != nil {
		t.Fatal(err)
	}
	if stale := handleSkillPackageRead(e, ctx, packageRequest(t, "skill.package.read", map[string]any{"skillId": skillID, "path": "SKILL.md", "expectedRevision": revision})); stale.OK {
		t.Fatal("old revision accepted after publish")
	}
}

func TestSkillPackageCreatedInChatRetainsOriginAndFiles(t *testing.T) {
	e, svc := skillPackageFixture(t)
	ctx := context.Background()
	sessionID := "01ARZ3NDEKTSV4RRFFQ69G5FAV"
	manifest, _ := json.Marshal(map[string]any{"prompt": "不要编造周报数据。", "originSessionId": "01ARZ3NDEKTSV4RRFFQ69G5FAX", "files": map[string]string{"evals/evals.json": "{\"evals\":[]}", "references/rules.md": "保留所有约束"}})
	args, _ := json.Marshal(map[string]any{"name": "created-here", "permissions": []string{"read_only"}, "manifestJson": string(manifest)})
	if _, err := e.invokeSkillCreateTool(withSkillCreationSession(ctx, sessionID), args); err != nil {
		t.Fatal(err)
	}
	items, err := svc.List(ctx, skill.SkillStatusDraft)
	if err != nil || len(items) != 1 {
		t.Fatalf("%#v %v", items, err)
	}
	matched := packagePayload(t, handleSkillList(e, ctx, packageRequest(t, "skill.list", map[string]any{"sourceSessionId": sessionID})))
	if len(matched["items"].([]any)) != 1 {
		t.Fatal("origin association lost")
	}
	other := packagePayload(t, handleSkillList(e, ctx, packageRequest(t, "skill.list", map[string]any{"sourceSessionId": "01ARZ3NDEKTSV4RRFFQ69G5FAX"})))
	if len(other["items"].([]any)) != 0 {
		t.Fatal("forged origin retained")
	}
	listing := packagePayload(t, handleSkillPackageList(e, ctx, packageRequest(t, "skill.package.list", map[string]any{"skillId": items[0].ID})))
	if b, err := os.ReadFile(filepath.Join(listing["rootPath"].(string), "evals", "evals.json")); err != nil || string(b) != "{\"evals\":[]}" {
		t.Fatal("draft resources missing", err)
	}
	for _, name := range []string{"../manifest.json", "C:/Windows/x", "references\\rules.md", "manifest.json:other", "NUL", "refs/../SKILL.md"} {
		if res := handleSkillPackageRead(e, ctx, packageRequest(t, "skill.package.read", map[string]any{"skillId": items[0].ID, "path": name})); res.OK {
			t.Fatalf("unsafe path accepted %s", name)
		}
	}
	if err := os.WriteFile(filepath.Join(listing["rootPath"].(string), "SKILL.md"), []byte("tampered"), 0600); err != nil {
		t.Fatal(err)
	}
	if res := handleSkillPackageList(e, ctx, packageRequest(t, "skill.package.list", map[string]any{"skillId": items[0].ID})); res.OK {
		t.Fatal("edited mirror silently claimed authoritative")
	}
}
