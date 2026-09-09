package officestudio

import (
	"strings"
	"testing"
)

func TestImportedWordComplexAndSwitchedFieldsRequireNativeUpdate(t *testing.T) {
	base, err := Generate(Spec{SchemaVersion: 1, Kind: DOCX, Title: "合成导入稿", Blocks: []Block{{Type: "paragraph", Text: "正文"}}})
	if err != nil {
		t.Fatal(err)
	}
	parts := zipParts(t, base)
	// A real TOC commonly splits its instruction across runs and contains nested
	// PAGEREF fields in its cached result. PAGE often has a formatting switch.
	fields := `<w:p><w:r><w:fldChar w:fldCharType="begin"/></w:r><w:r><w:instrText>TO</w:instrText></w:r><w:r><w:instrText>C \o "1-3"</w:instrText></w:r><w:r><w:fldChar w:fldCharType="separate"/></w:r><w:r><w:t>原目录缓存</w:t></w:r><w:r><w:fldChar w:fldCharType="begin"/></w:r><w:r><w:instrText>PAGEREF chapter</w:instrText></w:r><w:r><w:fldChar w:fldCharType="separate"/></w:r><w:r><w:t>99</w:t></w:r><w:r><w:fldChar w:fldCharType="end"/></w:r><w:r><w:fldChar w:fldCharType="end"/></w:r></w:p><w:p><w:fldSimple w:instr=" PAGE \* MERGEFORMAT "><w:r><w:t>99</w:t></w:r></w:fldSimple></w:p><w:p><w:r><w:fldChar w:fldCharType="begin"/></w:r><w:r><w:instrText>NUMPAGES \* Arabic</w:instrText></w:r><w:r><w:fldChar w:fldCharType="separate"/></w:r><w:r><w:t>99</w:t></w:r><w:r><w:fldChar w:fldCharType="end"/></w:r></w:p>`
	parts["word/document.xml"] = []byte(strings.Replace(string(parts["word/document.xml"]), "</w:body>", fields+"</w:body>", 1))
	data := editZIP(t, base, map[string][]byte{"word/document.xml": parts["word/document.xml"]})
	i, err := Inspect(DOCX, data)
	if err != nil {
		t.Fatal(err)
	}
	if !i.RenderAllowed || i.Structure == nil || !i.Structure.NeedsFieldUpdate || i.Structure.TOCFields != 1 || i.Structure.PageFields != 2 {
		t.Fatalf("imported fields omitted or counted twice: %+v", i.Structure)
	}
	v, err := Validate(DOCX, data)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, c := range v.Checks {
		if c.ID == "fields_update" && c.Status == "missing" {
			found = true
		}
	}
	if !found {
		t.Fatal("native update trigger missing", v)
	}
}

func TestLocalReferenceAndUnfinishedWordFieldCannotClaimFreshCache(t *testing.T) {
	for _, field := range []string{`<w:fldSimple w:instr="REF chapter"><w:r><w:t>stale</w:t></w:r></w:fldSimple>`, `<w:r><w:fldChar w:fldCharType="begin"/></w:r><w:r><w:instrText>PAGE</w:instrText></w:r>`} {
		xml := `<w:document xmlns:w="` + wordNS + `"><w:body><w:p>` + field + `</w:p></w:body></w:document>`
		s, err := inspectStructure(DOCX, map[string][]byte{"word/document.xml": []byte(xml)})
		if err != nil || !s.NeedsFieldUpdate {
			t.Fatal("field cache silently assumed current", s, err)
		}
	}
}
