package skillapp

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"testing"

	"github.com/lunitide/lunitide/internal/domain/skill"
)

func TestNewCommunityEntriesRemainUninstalledAtStartup(t *testing.T) {
	legacySeeds := map[string]bool{"skill-creator": true}
	store := newMemSkillStore()
	service := New(store, store)
	if _, err := service.EnsureBundledSkills(context.Background()); err != nil {
		t.Fatal(err)
	}
	for _, tpl := range Catalog() {
		_, community := tpl.Manifest["bundledPackage"]
		native := false
		if source, ok := tpl.Manifest["source"].(map[string]any); ok {
			native = source["kind"] == "lunitide-native"
		}
		if !community && !native {
			continue
		}
		if legacySeeds[tpl.ID] {
			if !tpl.Bundled {
				t.Fatalf("existing product seed removed: %s", tpl.ID)
			}
			continue
		}
		if tpl.Bundled || tpl.Compose {
			t.Fatalf("new market entry opted into automatic install: %s", tpl.ID)
		}
		if _, err := service.GetByNameVersion(context.Background(), tpl.Name, tpl.Version); err == nil {
			t.Fatalf("startup installed market-only entry: %s", tpl.ID)
		}
	}
}

func communitySkillForTest(t *testing.T, id string) skill.Skill {
	t.Helper()
	for _, tpl := range Catalog() {
		if tpl.ID == id {
			return skill.Skill{Name: tpl.Name, Version: tpl.Version, ManifestJSON: manifestFor(tpl)}
		}
	}
	t.Fatalf("missing catalog %s", id)
	return skill.Skill{}
}

func TestCommunitySourcesArePinnedCompleteAndPortable(t *testing.T) {
	packages, err := CommunityPackages()
	if err != nil {
		t.Fatal(err)
	}
	if len(packages) != 37 {
		t.Fatalf("reviewed package inventory changed: %d", len(packages))
	}
	seen := map[string]bool{}
	for _, pkg := range packages {
		t.Run(pkg.ID, func(t *testing.T) {
			if seen[pkg.ID] {
				t.Fatal("duplicate package")
			}
			seen[pkg.ID] = true
			if len(pkg.Commit) != 40 || !strings.HasPrefix(pkg.Repository, "https://github.com/") || pkg.License == "" || pkg.LicenseEvidence == "" {
				t.Fatalf("incomplete provenance: %s", pkg.ID)
			}
			if _, err := hex.DecodeString(pkg.Commit); err != nil {
				t.Fatal(err)
			}
			files, err := PackageFiles(communitySkillForTest(t, pkg.CatalogID))
			if err != nil {
				t.Fatal(err)
			}
			if len(files) != len(pkg.Resources)+2 {
				t.Fatalf("resources lost: %d vs %d", len(files), len(pkg.Resources))
			}
			if len(files[pkg.Entry]) == 0 {
				t.Fatal("missing upstream entry")
			}
			h := sha256.New()
			resources := append([]CommunityResource(nil), pkg.Resources...)
			sort.Slice(resources, func(i, j int) bool { return resources[i].Path < resources[j].Path })
			for _, resource := range resources {
				fmt.Fprintf(h, "%s\x00%s\n", resource.Path, resource.SHA256)
			}
			if hex.EncodeToString(h.Sum(nil)) != pkg.Digest {
				t.Fatal("bundle digest changed without new receipt")
			}
		})
	}
}

func TestCommunityCreatorAndBrowserKeepActualSupportFiles(t *testing.T) {
	files, err := PackageFiles(communitySkillForTest(t, "skill-creator"))
	if err != nil {
		t.Fatal(err)
	}
	for _, relative := range []string{"agents/grader.md", "agents/comparator.md", "agents/analyzer.md", "assets/eval_review.html", "references/schemas.md", "eval-viewer/generate_review.py", "eval-viewer/viewer.html", "scripts/aggregate_benchmark.py", "scripts/run_eval.py", "scripts/run_loop.py", "scripts/improve_description.py", "scripts/package_skill.py", "scripts/quick_validate.py", "scripts/__init__.py", "LICENSE.txt"} {
		if _, ok := files["upstream/skills/skill-creator/"+relative]; !ok {
			t.Fatalf("creator missing %s", relative)
		}
	}
	if !strings.Contains(string(files["SKILL.md"]), "skill.try") || !strings.Contains(string(files["SKILL.md"]), "对照") {
		t.Fatal("Lunitide trial/compare agreement missing")
	}
	browser, err := PackageFiles(communitySkillForTest(t, "agent-browser"))
	if err != nil {
		t.Fatal(err)
	}
	if len(browser["upstream/skill-data/core/SKILL.md"]) <= len(browser["upstream/skills/agent-browser/SKILL.md"]) {
		t.Fatal("browser only ships discovery stub")
	}
}

func TestCommunityLargeSkillRemainsCompleteOutsideManifest(t *testing.T) {
	sk := communitySkillForTest(t, "design-taste-frontend")
	files, err := PackageFiles(sk)
	if err != nil {
		t.Fatal(err)
	}
	if len(sk.ManifestJSON) > 65536 {
		t.Fatal("manifest storage overflow")
	}
	if len(files["upstream/skills/taste-skill/SKILL.md"]) < 80000 {
		t.Fatal("upstream instructions truncated")
	}
	var manifest map[string]any
	if json.Unmarshal([]byte(sk.ManifestJSON), &manifest) != nil || manifest["sourceBodyExternal"] != true {
		t.Fatal("external source not declared")
	}
}

func TestCommunitySourceReceiptCannotBeForgedByName(t *testing.T) {
	sk := communitySkillForTest(t, "skill-creator")
	for _, field := range []string{"name", "version", "commit", "digest", "repository"} {
		t.Run(field, func(t *testing.T) {
			changed := sk
			var m map[string]any
			_ = json.Unmarshal([]byte(sk.ManifestJSON), &m)
			switch field {
			case "name":
				changed.Name = "lookalike"
			case "version":
				changed.Version = "99.0.0"
			case "commit", "digest":
				m["bundledPackage"].(map[string]any)[field] = "forged"
			case "repository":
				m["source"].(map[string]any)[field] = "https://github.com/untrusted/fork"
			}
			b, _ := json.Marshal(m)
			changed.ManifestJSON = string(b)
			if _, err := BundledPackageFiles(changed); err == nil {
				t.Fatal("forged provenance accepted")
			}
		})
	}
	files, err := BundledPackageFiles(skill.Skill{Name: "skill-creator", Version: "2.0.0", ManifestJSON: `{"prompt":"user-created content"}`})
	if err != nil || files != nil {
		t.Fatal("name alone attached official resources")
	}
}

func TestCommunityDependencyNamesAndRestrictedAlternatives(t *testing.T) {
	names := map[string]bool{}
	for _, tpl := range Catalog() {
		names[tpl.Name] = true
	}
	for _, name := range []string{"grilling", "domain-modeling", "codebase-design", "wayfinder"} {
		if !names[name] {
			t.Fatalf("missing transitive dependency %s", name)
		}
	}
	for _, id := range []string{"docx", "xlsx", "pptx", "pdf", "doc-coauthoring", "docker-optimize"} {
		sk := communitySkillForTest(t, id)
		files, err := BundledPackageFiles(sk)
		if err != nil || files != nil {
			t.Fatalf("restricted/unverified upstream copied for %s", id)
		}
		if !strings.Contains(sk.ManifestJSON, "lunitide-native") {
			t.Fatalf("native alternative falsely attributed %s", id)
		}
	}
}
