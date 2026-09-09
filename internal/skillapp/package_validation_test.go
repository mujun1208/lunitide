package skillapp

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/lunitide/lunitide/internal/domain/skill"
)

func uploadZIPFixture(t *testing.T, names []string) []byte {
	t.Helper()
	var b bytes.Buffer
	w := zip.NewWriter(&b)
	for _, name := range names {
		f, err := w.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if strings.HasSuffix(name, "SKILL.md") {
			_, err = f.Write([]byte("---\nname: trial\ndescription: Check actual data.\n---\nPreserve evidence.\n"))
		} else {
			_, err = f.Write([]byte("support"))
		}
		if err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

func TestUploadedSkillRejectsIncompleteOrAmbiguousPackages(t *testing.T) {
	for _, names := range [][]string{{"SKILL.md", "../outside"}, {"SKILL.md", "NUL"}, {"SKILL.md", "assets/X", "assets/x"}, {"SKILL.md", "scripts", "scripts/run.py"}, {"one/SKILL.md", "two/SKILL.md"}, {"one/SKILL.md", "outside.txt"}, {"readme.md"}, {"SKILL.md", "C:/outside"}, {"SKILL.md", "a.txt:ads"}, {"SKILL.md", "a. "}} {
		t.Run(strings.Join(names, "_"), func(t *testing.T) {
			if _, _, err := decodeUploadedSkill("test.skill", uploadZIPFixture(t, names)); err == nil {
				t.Fatalf("accepted %v", names)
			}
		})
	}
	for _, raw := range []string{`null`, `{"prompt":""}`, `{"name":"test","prompt":"body","files":{"../x":"escape"}}`, `{"name":"test","prompt":"body","files":{"NUL":"device"}}`} {
		if _, _, err := decodeUploadedSkill("test.json", []byte(raw)); err == nil {
			t.Fatal("invalid JSON accepted", raw)
		}
	}
}

func TestSkillPackageImmutableStorageConcurrentAndTamper(t *testing.T) {
	root := t.TempDir()
	files := map[string][]byte{"SKILL.md": []byte("full agreement"), "scripts/run.py": []byte("# no execution\n"), "assets/data.bin": {0, 1, 255}}
	var wg sync.WaitGroup
	ids := make(chan string, 12)
	errs := make(chan error, 12)
	for range 12 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			id, err := StoreLocalPackage(root, files)
			if err != nil {
				errs <- err
				return
			}
			ids <- id
		}()
	}
	wg.Wait()
	close(ids)
	close(errs)
	for err := range errs {
		t.Fatal(err)
	}
	identity := ""
	for id := range ids {
		if identity != "" && identity != id {
			t.Fatal("non deterministic digest")
		}
		identity = id
	}
	reloaded, err := LoadLocalPackage(root, identity)
	if err != nil || !bytes.Equal(reloaded["assets/data.bin"], files["assets/data.bin"]) {
		t.Fatal("binary changed", err)
	}
	if err = os.WriteFile(filepath.Join(root, identity+".json"), []byte("{}"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err = LoadLocalPackage(root, identity); err == nil {
		t.Fatal("corrupt resource accepted")
	}
	if _, err = StoreLocalPackage(root, files); err == nil {
		t.Fatal("corrupt stored package overwritten silently")
	}
}

func TestSkillPackageConcurrentMaterializationAndUploadLimits(t *testing.T) {
	sk := makeSkill("01ARZ3NDEKTSV4RRFFQ69G5FAV", skill.SkillStatusDraft, nil)
	sk.EntryPoint = "SKILL.md"
	sk.ManifestJSON = `{"prompt":"evidence"}`
	files, err := PackageFiles(*sk)
	if err != nil {
		t.Fatal(err)
	}
	files["references/long.md"] = []byte(strings.Repeat("重要数据🙂", 8000))
	root := t.TempDir()
	var wg sync.WaitGroup
	errs := make(chan error, 12)
	for range 12 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _, err := MaterializePackage(root, *sk, files)
			if err != nil {
				errs <- err
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatal(err)
	}
	w := &mockSkillWriter{}
	s := New(&mockSkillReader{}, w)
	s.SetPackageRoot(t.TempDir())
	raw := []byte(`{"name":"trial","prompt":"Use the real data."}`)
	sum := sha256.Sum256(raw)
	sha := hex.EncodeToString(sum[:])
	id, err := s.BeginPackageUpload("test.json", len(raw), sha)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.AppendPackageUpload(id, 1, raw); err == nil {
		t.Fatal("offset gap accepted")
	}
	if _, err = s.CommitPackageUpload(context.Background(), id); err == nil {
		t.Fatal("incomplete package committed")
	}
	if _, err = s.AppendPackageUpload(id, 0, raw); err != nil {
		t.Fatal(err)
	}
	other := append([]byte(nil), raw...)
	other[0] = '['
	if _, err = s.AppendPackageUpload(id, 0, other); err == nil {
		t.Fatal("conflicting replay accepted")
	}
	created, err := s.CommitPackageUpload(context.Background(), id)
	if err != nil || created.Status != skill.SkillStatusDraft {
		t.Fatal("not a draft", created, err)
	}
	replay, err := s.CommitPackageUpload(context.Background(), id)
	if err != nil || replay.ID != created.ID {
		t.Fatal("commit is not idempotent", err)
	}
	s.AbortPackageUpload(id)
	if _, err = s.CommitPackageUpload(context.Background(), id); err == nil {
		t.Fatal("aborted upload still present")
	}
}
