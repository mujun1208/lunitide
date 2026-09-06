package skillarchive

import (
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/lunitide/lunitide/internal/networkpolicy"
)

const fixtureSHA = "0123456789abcdef0123456789abcdef01234567"
const fixtureSkill = "---\nname: meeting-helper\ndescription: >-\n  Summarize supplied notes\n  into action items.\n---\nRead the supplied notes and produce a concise summary.\n"

func fixtureArchive(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var b bytes.Buffer
	w := zip.NewWriter(&b)
	for name, body := range files {
		f, err := w.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = f.Write([]byte(body)); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

func TestStandardSkillArchiveReadsPinnedSubdirectory(t *testing.T) {
	archive := fixtureArchive(t, map[string]string{"repo-sha/skills/notes/SKILL.md": fixtureSkill, "repo-sha/skills/notes/scripts/unsafe.py": "raise SystemExit('never run')"})
	var fetched string
	loader := Loader{Fetch: func(_ context.Context, raw string, o networkpolicy.FetchOptions) (networkpolicy.FetchResult, error) {
		fetched = raw
		if o.MaxBodyBytes != MaxArchiveBytes || o.OverallTimeout <= 0 {
			t.Fatal("download is unbounded")
		}
		return networkpolicy.FetchResult{Status: 200, FinalURL: raw, Body: archive}, nil
	}}
	p, err := loader.Load(context.Background(), "https://github.com/acme/repo/tree/"+fixtureSHA+"/skills/notes", fixtureSHA)
	if err != nil {
		t.Fatal(err)
	}
	if fetched != "https://codeload.github.com/acme/repo/zip/"+fixtureSHA || p.Name != "meeting-helper" || p.Description != "Summarize supplied notes into action items." || p.License != "unknown" || p.SkippedFiles != 1 || !strings.Contains(p.Prompt, "concise summary") {
		t.Fatalf("wrong source projection: %+v %s", p, fetched)
	}
	if p.ArchiveHash != hash(archive) {
		t.Fatal("hash not computed from archive")
	}
}

func TestSkillArchiveRejectsUnsupportedURLsBeforeFetch(t *testing.T) {
	loader := Loader{Fetch: func(context.Context, string, networkpolicy.FetchOptions) (networkpolicy.FetchResult, error) {
		t.Fatal("unvalidated URL reached fetch")
		return networkpolicy.FetchResult{}, nil
	}}
	for _, raw := range []string{"http://github.com/acme/repo", "https://localhost/acme/repo", "https://github.com@evil.test/acme/repo", "https://github.com/acme/repo?token=no", "https://github.com/acme/repo/tree/main/skills/x", "https://github.com/acme/repo/tree/" + fixtureSHA + "/../x", "https://github.com/acme/repo%2fother"} {
		if _, err := loader.Load(context.Background(), raw, fixtureSHA); !errors.Is(err, ErrInvalid) {
			t.Fatalf("accepted %q: %v", raw, err)
		}
	}
	if _, err := loader.Load(context.Background(), "https://github.com/acme/repo", "main"); !errors.Is(err, ErrInvalid) {
		t.Fatal("mutable ref accepted")
	}
}

func TestSkillArchiveRejectsUnsafeOrInvalidContent(t *testing.T) {
	src, err := ParseSource("https://github.com/acme/repo", fixtureSHA)
	if err != nil {
		t.Fatal(err)
	}
	for name, files := range map[string]map[string]string{
		"traversal":                  {"repo/SKILL.md": fixtureSkill, "repo/../outside": "bad"},
		"backslash":                  {"repo/SKILL.md": fixtureSkill, "repo/dir\\outside": "bad"},
		"duplicate-case":             {"repo/SKILL.md": fixtureSkill, "repo/skill.md": fixtureSkill},
		"missing-frontmatter":        {"repo/SKILL.md": "prompt only"},
		"duplicate-yaml-key":         {"repo/SKILL.md": "---\nname: first\nname: second\ndescription: duplicate\n---\ntext"},
		"oversize-prompt":            {"repo/SKILL.md": fixtureSkill + strings.Repeat("x", MaxPromptBytes)},
		"empty-body":                 {"repo/SKILL.md": "---\nname: empty\ndescription: missing body\n---\n"},
		"encoded-attestation-budget": {"repo/SKILL.md": "---\nname: escaped\ndescription: \"" + strings.Repeat("&", 3000) + "\"\n---\ntext"},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := Read(src, fixtureArchive(t, files)); !errors.Is(err, ErrInvalid) {
				t.Fatalf("accepted: %v", err)
			}
		})
	}
	var b bytes.Buffer
	w := zip.NewWriter(&b)
	h := &zip.FileHeader{Name: "repo/SKILL.md", Method: zip.Store}
	h.SetMode(os.ModeSymlink | 0o777)
	f, err := w.CreateHeader(h)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.Write([]byte("../secret")); err != nil {
		t.Fatal(err)
	}
	if err = w.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := Read(src, b.Bytes()); !errors.Is(err, ErrInvalid) {
		t.Fatal("symlink accepted")
	}
}

func TestSkillArchiveRejectsClippedOrRedirectedDownloads(t *testing.T) {
	for _, result := range []networkpolicy.FetchResult{{Status: 404}, {Status: 200, Truncated: true}, {Status: 200, FinalURL: "https://other.test/archive"}} {
		loader := Loader{Fetch: func(context.Context, string, networkpolicy.FetchOptions) (networkpolicy.FetchResult, error) {
			return result, nil
		}}
		if _, err := loader.Load(context.Background(), "https://github.com/acme/repo", fixtureSHA); !errors.Is(err, ErrFetch) {
			t.Fatalf("accepted %#v", result)
		}
	}
}

func TestSkillArchiveExpandedBudgetAppliesToSkippedFiles(t *testing.T) {
	src, err := ParseSource("https://github.com/acme/repo", fixtureSHA)
	if err != nil {
		t.Fatal(err)
	}
	archive := fixtureArchive(t, map[string]string{"repo/SKILL.md": fixtureSkill, "repo/ignored.bin": strings.Repeat("x", MaxExpandedBytes)})
	if _, err := Read(src, archive); !errors.Is(err, ErrInvalid) {
		t.Fatalf("expansion budget ignored: %v", err)
	}
}
