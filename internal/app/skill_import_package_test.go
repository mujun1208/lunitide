package app

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/lunitide/lunitide/internal/domain/skill"
	"github.com/lunitide/lunitide/internal/skillapp"
)

func TestGitHubApprovalPreservesCompletePackageForDraftTrialAndPublish(t *testing.T) {
	f := newSourceImportFixture(t, importFixtureText)
	resources := map[string][]byte{"SKILL.md": []byte(importFixtureText), "scripts/utility.py": []byte("# never automatically execute\nprint('example')\n"), "references/detail.md": []byte("末尾材料约束\n"), "assets/template.bin": {0, 255, 128, 10}}
	var archive bytes.Buffer
	w := zip.NewWriter(&archive)
	for name, raw := range resources {
		entry, err := w.Create("fixture/" + name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = entry.Write(raw); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	f.archive = archive.Bytes()
	c := f.discover(t)
	if c.SkillID != "" {
		t.Fatal("unapproved candidate advertised a runtime skill")
	}
	c = f.step(t, c, "skill.import.inspect")
	c = f.step(t, c, "skill.import.submit")
	c = sourceCandidate(t, f.call(t, "skill.import.approve", map[string]any{"candidateId": c.CandidateID, "expectedVersion": c.Version, "approval": map[string]string{"by": "test"}}))
	if c.SkillID != c.CandidateID {
		t.Fatal("approved result lacks exact runtime skill ID")
	}
	sk, err := f.store.GetSkill(context.Background(), c.CandidateID)
	if err != nil || sk == nil || sk.Status != skill.SkillStatusDraft {
		t.Fatalf("not a draft: %+v %v", sk, err)
	}
	var manifest struct {
		Digest string `json:"localPackageDigest"`
		Scope  string `json:"importScope"`
	}
	if err = json.Unmarshal([]byte(sk.ManifestJSON), &manifest); err != nil || len(manifest.Digest) != 64 || manifest.Scope != "complete_package" {
		t.Fatalf("missing immutable package: %+v %v", manifest, err)
	}
	stored, err := skillapp.LoadLocalPackage(filepath.Join(filepath.Dir(f.dbPath), "skill-package-store"), manifest.Digest)
	if err != nil {
		t.Fatal(err)
	}
	for name, raw := range resources {
		if !bytes.Equal(stored[name], raw) {
			t.Fatalf("resource changed: %s", name)
		}
	}
	f.offline = true
	f.restart()
	if resumed := f.discover(t); resumed.CandidateID != c.CandidateID || resumed.State != "approved" || resumed.SkillID != c.SkillID {
		t.Fatal("offline committed approval lost")
	}
	svc := f.engine.skills.(*skillapp.Service)
	inv, err := svc.InvokeTrial(context.Background(), sk.ID, chatAttachmentSessionID, "试用导入技能", "full-access")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = svc.Execute(context.Background(), inv.ID, chatAttachmentSessionID, true); err != nil {
		t.Fatal(err)
	}
	if err = svc.Publish(context.Background(), sk.ID); err != nil {
		t.Fatal(err)
	}
	files, err := svc.PackageFiles(*sk)
	if err != nil || !bytes.Equal(files["scripts/utility.py"], resources["scripts/utility.py"]) || !bytes.Equal(files["upstream/SKILL.md"], resources["SKILL.md"]) {
		t.Fatalf("runtime package incomplete: %v", err)
	}
}

func TestGitHubPackageWriteFailureDoesNotApproveOrCreateRuntime(t *testing.T) {
	f := newSourceImportFixture(t, importFixtureText)
	c := f.discover(t)
	c = f.step(t, c, "skill.import.inspect")
	c = f.step(t, c, "skill.import.submit")
	blocked := filepath.Join(t.TempDir(), "not-a-directory")
	if err := os.WriteFile(blocked, []byte("unchanged"), 0600); err != nil {
		t.Fatal(err)
	}
	f.engine.m6skills.SetPackageRoot(blocked)
	r := f.call(t, "skill.import.approve", map[string]any{"candidateId": c.CandidateID, "expectedVersion": c.Version, "approval": map[string]string{"by": "test"}})
	if r.OK {
		t.Fatal("package write failure claimed approved")
	}
	if current := f.stored(t, c.CandidateID); current.State != "awaiting_approval" || current.Version != c.Version {
		t.Fatal("failed import advanced approval")
	}
	if sk, err := f.store.GetSkill(context.Background(), c.CandidateID); err != nil || sk != nil {
		t.Fatalf("partial runtime row: %+v %v", sk, err)
	}
}
