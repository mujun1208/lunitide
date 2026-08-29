package officetools

import (
	"bytes"
	"fmt"
	"regexp"
	"strings"
	"unicode/utf8"
)

var (
	reDocxStyleVal = regexp.MustCompile(`<w:pStyle\s+w:val="([^"]+)"`)
	reDocxStyleID  = regexp.MustCompile(`w:styleId="([^"]+)"`)
)

// ValidateDocx fails closed on empty, unstyled, or trivial Word files.
// A body that is only 11pt Normal with no Heading 1/2 is the DOCX analogue
// of a navy fill-only PPT slide.
func ValidateDocx(data []byte) error {
	if len(data) < 4 || !bytes.HasPrefix(data, []byte("PK")) {
		return fmt.Errorf("officetools: docx is not a zip container")
	}
	names, err := zipEntryNames(data)
	if err != nil {
		return fmt.Errorf("officetools: docx zip: %w", err)
	}
	hasDoc, hasStyles := false, false
	for _, name := range names {
		switch name {
		case "word/document.xml":
			hasDoc = true
		case "word/styles.xml":
			hasStyles = true
		}
	}
	if !hasDoc {
		return fmt.Errorf("officetools: docx has no document part")
	}
	if !hasStyles {
		return fmt.Errorf("officetools: docx missing styles.xml (unstyled body is invalid)")
	}
	styles, err := readZipPart(data, "word/styles.xml", 1<<20)
	if err != nil {
		return fmt.Errorf("officetools: styles.xml: %w", err)
	}
	if err := validateDocxStylesXML(styles); err != nil {
		return err
	}
	body, err := readZipPart(data, "word/document.xml", 4<<20)
	if err != nil {
		return fmt.Errorf("officetools: document.xml: %w", err)
	}
	return validateDocxDocumentXML(body)
}

func validateDocxStylesXML(styles string) error {
	ids := map[string]bool{}
	for _, m := range reDocxStyleID.FindAllStringSubmatch(styles, -1) {
		ids[m[1]] = true
	}
	for _, need := range []string{"Normal", "Heading1", "Heading2", "Title"} {
		if !ids[need] {
			return fmt.Errorf("officetools: styles.xml missing %s", need)
		}
	}
	if !strings.Contains(styles, "SimSun") && !strings.Contains(styles, "SimHei") && !strings.Contains(styles, "Microsoft YaHei") {
		return fmt.Errorf("officetools: styles.xml missing Chinese-friendly fonts")
	}
	if !strings.Contains(styles, `w:line="360"`) {
		return fmt.Errorf("officetools: styles.xml missing readable line spacing")
	}
	return nil
}

func validateDocxDocumentXML(body string) error {
	var texts []string
	total := 0
	for _, m := range reDocxText.FindAllStringSubmatch(body, -1) {
		t := strings.TrimSpace(xmlUnescape(m[1]))
		if t == "" {
			continue
		}
		texts = append(texts, t)
		total += utf8.RuneCountInString(t)
	}
	if len(texts) == 0 || total < minDocxBodyRunes {
		return fmt.Errorf("officetools: document is empty or trivial (no usable text)")
	}
	used := map[string]bool{}
	for _, m := range reDocxStyleVal.FindAllStringSubmatch(body, -1) {
		used[m[1]] = true
	}
	if !used["Heading1"] && !used["Heading2"] {
		return fmt.Errorf("officetools: document has no Heading 1/2 (unstyled single-style body)")
	}
	if !used["Normal"] && !used["Quote"] && !used["ListParagraph"] {
		return fmt.Errorf("officetools: document has headings but no body style")
	}
	return nil
}

// UnstyledDocxFixture is an invalid single-style 11pt body with no styles part.
func UnstyledDocxFixture() []byte {
	doc := xmlDecl + `
<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:body><w:p><w:r><w:t>hello</w:t></w:r></w:p></w:body></w:document>`
	data, err := zipBytes([]zipPart{
		{"[Content_Types].xml", xmlDecl + `
<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types"><Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/><Default Extension="xml" ContentType="application/xml"/><Override PartName="/word/document.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.document.main+xml"/></Types>`},
		{"_rels/.rels", relsDocx},
		{"word/document.xml", doc},
	})
	if err != nil {
		return nil
	}
	return data
}

// EmptyDocxFixture is an invalid zip with a document part and no visible text.
func EmptyDocxFixture() []byte {
	doc := xmlDecl + `
<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:body><w:p/></w:body></w:document>`
	data, err := zipBytes([]zipPart{
		{"[Content_Types].xml", contentTypesDocx()},
		{"_rels/.rels", relsDocx},
		{"word/document.xml", doc},
		{"word/styles.xml", docxStylesXML},
	})
	if err != nil {
		return nil
	}
	return data
}
