package skillarchive

import (
	"bytes"
	"encoding/json"
	"testing"
)

func TestSkillPackagePreservesBinaryResourcesScriptsAndInheritedLicense(t *testing.T) {
	asset := []byte{0, 255, 13, 10, 128, 42}
	files := map[string]string{
		"repo/skills/demo/SKILL.md":             fixtureSkill,
		"repo/skills/demo/scripts/run.py":       "# inert; do not execute\r\nprint('sample')\r\n",
		"repo/skills/demo/assets/sample.bin":    string(asset),
		"repo/skills/demo/references/detail.md": "材料末尾约束\n",
		"repo/other/secret.txt":                 "outside-selected-directory",
		"repo/LICENSE":                          "Example redistributable license text\n",
	}
	src, err := ParseSource("https://github.com/acme/repo/tree/"+fixtureSHA+"/skills/demo", fixtureSHA)
	if err != nil {
		t.Fatal(err)
	}
	p, err := Read(src, fixtureArchive(t, files))
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Files) != 5 || p.SkippedFiles != 0 || !bytes.Equal(p.Files["assets/sample.bin"], asset) || string(p.Files["scripts/run.py"]) != files["repo/skills/demo/scripts/run.py"] || string(p.Files["upstream-license/LICENSE"]) != files["repo/LICENSE"] {
		t.Fatalf("incomplete resource projection: %#v", p.Files)
	}
	if _, found := p.Files["other/secret.txt"]; found {
		t.Fatal("import escaped selected directory")
	}
	if !p.MatchesAttestation(p.Attestation()) {
		t.Fatal("new attestation mismatch")
	}
	var legacy map[string]any
	_ = json.Unmarshal([]byte(p.Attestation()), &legacy)
	legacy["scope"] = "SKILL.md instructions only; repository scripts are not installed or executed"
	legacy["skippedFiles"] = 3
	delete(legacy, "fileCount")
	raw, _ := json.Marshal(legacy)
	if !p.MatchesAttestation(string(raw)) {
		t.Fatal("same immutable source could not resume legacy approval")
	}
	legacy["archiveHash"] = "changed"
	raw, _ = json.Marshal(legacy)
	if p.MatchesAttestation(string(raw)) {
		t.Fatal("different archive matched legacy attestation")
	}
}
