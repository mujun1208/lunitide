package deckfill

import (
	"archive/zip"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"unicode/utf8"
)

// TextPut is one shape the model chose to rewrite. ID is the shape id
// from a catalog read, not a phrase the program recognizes.
type TextPut struct {
	ID   string
	Text string
}

// PageUse is one output page cloned from a template slide.
// The same From value may appear many times; each one is a new copy.
type PageUse struct {
	From  int
	Texts []TextPut
}

// ListLibrary lists pptx files under the library. Adding a file does not
// require a code change; the model picks one on the next catalog read.
func ListLibrary(root string) (string, error) {
	var lines []string
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		name := d.Name()
		if strings.HasPrefix(name, "~$") || !strings.EqualFold(filepath.Ext(name), ".pptx") {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return nil
		}
		lines = append(lines, fmt.Sprintf("%s\t%d页\t%dKB", filepath.ToSlash(rel), countSlides(path), info.Size()/1024))
		return nil
	})
	if err != nil {
		return "", err
	}
	sort.Strings(lines)
	if len(lines) == 0 {
		return "", fmt.Errorf("deckfill: no pptx in %s", root)
	}
	return "模板库。下一步对选中的文件再 catalog，读取每页形状 id 和现有文字。\n" + strings.Join(lines, "\n"), nil
}

// DescribeTemplate prints each page's text shapes so the model can decide
// which pages to use and which shape ids to rewrite.
func DescribeTemplate(root, rel string) (string, error) {
	full, err := resolveUnder(root, rel)
	if err != nil {
		return "", err
	}
	raw, err := os.ReadFile(full)
	if err != nil {
		return "", err
	}
	deck, err := openPkg(raw)
	if err != nil {
		return "", err
	}
	refs, err := deck.slides()
	if err != nil {
		return "", err
	}
	var b strings.Builder
	rel = filepath.ToSlash(rel)
	fmt.Fprintf(&b, "template: %s\n", rel)
	b.WriteString("构图由你决定：pages[].from 是下面的页码。同一 from 写多次就是克隆多份，用来把 10 页模板做成更长的稿。texts 按 id 改字。纯图页没有 id，不要往上加字。排版样式用你选中的那一页，不要另画坐标。\n")
	for _, ref := range refs {
		xml := deck.slideXML(ref.num)
		written := 0
		var body strings.Builder
		for _, sp := range shapeSpans(xml) {
			shape := xml[sp[0]:sp[1]]
			text := strings.TrimSpace(shapeText(shape))
			if text == "" {
				continue
			}
			id := shapeID(shape)
			if id == "" {
				id = "?"
			}
			fmt.Fprintf(&body, "  id=%s  %s\n", id, clip(text, 48))
			written++
		}
		if written == 0 {
			fmt.Fprintf(&b, "page %d  纯图（文字在图片里，不能改）\n", ref.num)
			continue
		}
		fmt.Fprintf(&b, "page %d  可改%d处\n%s", ref.num, written, body.String())
	}
	return b.String(), nil
}

// ApplyFile clones the requested template pages in order and rewrites the
// shape ids the model named. No phrase list is consulted.
func ApplyFile(root, rel string, uses []PageUse) ([]byte, []string, error) {
	if len(uses) == 0 || len(uses) > 40 {
		return nil, nil, fmt.Errorf("deckfill: pages must be 1-40")
	}
	full, err := resolveUnder(root, rel)
	if err != nil {
		return nil, nil, err
	}
	raw, err := os.ReadFile(full)
	if err != nil {
		return nil, nil, err
	}
	deck, err := openPkg(raw)
	if err != nil {
		return nil, nil, err
	}
	notes, err := deck.applyUses(uses)
	if err != nil {
		return nil, nil, err
	}
	out, err := deck.bytes()
	return out, notes, err
}

func (p *pkg) applyUses(uses []PageUse) ([]string, error) {
	pristine := map[int][]byte{}
	for _, use := range uses {
		if use.From <= 0 {
			return nil, fmt.Errorf("deckfill: bad slide %d", use.From)
		}
		if pristine[use.From] != nil {
			continue
		}
		xml := p.slideXML(use.From)
		if xml == nil {
			return nil, fmt.Errorf("deckfill: slide %d is not in the template", use.From)
		}
		pristine[use.From] = append([]byte(nil), xml...)
	}
	seen := map[int]bool{}
	var order []int
	var notes []string
	for _, use := range uses {
		edited, extra := applyTexts(append([]byte(nil), pristine[use.From]...), use.Texts)
		notes = append(notes, extra...)
		if !seen[use.From] {
			seen[use.From] = true
			p.setSlideXML(use.From, edited)
			order = append(order, use.From)
			continue
		}
		saved := append([]byte(nil), p.slideXML(use.From)...)
		p.setSlideXML(use.From, pristine[use.From])
		n, err := p.clone(use.From)
		if err != nil {
			return nil, err
		}
		p.setSlideXML(use.From, saved)
		p.setSlideXML(n, edited)
		order = append(order, n)
	}
	if err := p.keep(order); err != nil {
		return nil, err
	}
	if err := p.renumber(); err != nil {
		return nil, err
	}
	p.gc()
	notes = append(notes, fontNotes(p.files)...)
	return uniqueNotes(notes), nil
}

func applyTexts(xml []byte, texts []TextPut) ([]byte, []string) {
	want := map[string]string{}
	for _, text := range texts {
		if text.ID == "" {
			continue
		}
		want[text.ID] = text.Text
	}
	if len(want) == 0 {
		return xml, nil
	}
	spans := shapeSpans(xml)
	type edit struct {
		s, e         int
		next, sample string
	}
	var edits []edit
	found := map[string]bool{}
	for _, sp := range spans {
		shape := xml[sp[0]:sp[1]]
		id := shapeID(shape)
		next, ok := want[id]
		if !ok {
			continue
		}
		found[id] = true
		edits = append(edits, edit{sp[0], sp[1], next, shapeText(shape)})
	}
	var missing []string
	for id := range want {
		if !found[id] {
			missing = append(missing, id)
		}
	}
	sort.Strings(missing)
	var notes []string
	if len(missing) > 0 {
		notes = append(notes, "找不到形状 "+strings.Join(missing, ","))
	}
	for i := len(edits) - 1; i >= 0; i-- {
		ed := edits[i]
		shape := shrinkToFit(xml[ed.s:ed.e], ed.sample, ed.next)
		shape = setShapeText(shape, ed.next)
		xml = append(xml[:ed.s], append(shape, xml[ed.e:]...)...)
	}
	return xml, notes
}

func shapeID(shape []byte) string {
	m := cNvPrID.FindSubmatch(shape)
	if m == nil {
		return ""
	}
	return string(m[1])
}

var cNvPrID = regexp.MustCompile(`<p:cNvPr\b[^>]*\bid="(\d+)"`)

func countSlides(path string) int {
	r, err := zip.OpenReader(path)
	if err != nil {
		return 0
	}
	defer r.Close()
	for _, f := range r.File {
		if !strings.HasSuffix(strings.ReplaceAll(f.Name, "\\", "/"), "ppt/_rels/presentation.xml.rels") {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return 0
		}
		body, err := io.ReadAll(io.LimitReader(rc, 1<<20))
		rc.Close()
		if err != nil {
			return 0
		}
		n := 0
		for _, tag := range relTag.FindAll(body, -1) {
			target := attr(tgtAttr, tag)
			if strings.Contains(target, "slides/slide") && !strings.Contains(target, "slideLayout") && !strings.Contains(target, "slideMaster") {
				n++
			}
		}
		return n
	}
	return 0
}

func resolveUnder(root, rel string) (string, error) {
	rel = strings.TrimSpace(strings.ReplaceAll(rel, "\\", "/"))
	if rel == "" || strings.Contains(rel, "..") {
		return "", fmt.Errorf("deckfill: bad template path")
	}
	rootAbs, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	fullAbs, err := filepath.Abs(filepath.Join(rootAbs, filepath.FromSlash(rel)))
	if err != nil {
		return "", err
	}
	out, err := filepath.Rel(rootAbs, fullAbs)
	if err != nil || strings.HasPrefix(out, "..") {
		return "", fmt.Errorf("deckfill: template escapes the library")
	}
	info, err := os.Stat(fullAbs)
	if err != nil || info.IsDir() {
		return "", fmt.Errorf("deckfill: template not found")
	}
	return fullAbs, nil
}

func clip(text string, n int) string {
	text = strings.Join(strings.Fields(text), " ")
	if utf8.RuneCountInString(text) <= n {
		return text
	}
	runes := []rune(text)
	return string(runes[:n]) + "…"
}
