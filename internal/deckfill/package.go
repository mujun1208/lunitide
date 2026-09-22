package deckfill

import (
	"archive/zip"
	"bytes"
	"fmt"
	"io"
	"path"
	"regexp"
	"sort"
	"strings"
)

const slideRelType = "http://schemas.openxmlformats.org/officeDocument/2006/relationships/slide"

var (
	sldIDTag = regexp.MustCompile(`<p:sldId\b[^>]*/>`)
	relTag   = regexp.MustCompile(`<Relationship\b[^>]*/>`)
	attrRE   = func(name string) *regexp.Regexp {
		return regexp.MustCompile(name + `="([^"]*)"`)
	}
	idAttr   = attrRE("Id")
	ridAttr  = attrRE("r:id")
	sidAttr  = regexp.MustCompile(`\sid="(\d+)"`)
	tgtAttr  = attrRE("Target")
	typeAttr = attrRE("Type")
	partAttr = regexp.MustCompile(`<Override\b[^>]*PartName="([^"]+)"[^>]*/>`)
)

type pkg struct {
	files map[string][]byte
}

func openPkg(data []byte) (*pkg, error) {
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, err
	}
	files := make(map[string][]byte, len(zr.File))
	for _, f := range zr.File {
		if f.FileInfo().IsDir() {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return nil, err
		}
		body, err := io.ReadAll(io.LimitReader(rc, 80<<20))
		rc.Close()
		if err != nil {
			return nil, err
		}
		files[strings.ReplaceAll(f.Name, "\\", "/")] = body
	}
	return &pkg{files: files}, nil
}

func (p *pkg) bytes() ([]byte, error) {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	names := make([]string, 0, len(p.files))
	for name := range p.files {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		w, err := zw.Create(name)
		if err != nil {
			return nil, err
		}
		if _, err = w.Write(p.files[name]); err != nil {
			return nil, err
		}
	}
	if err := zw.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

type slideRef struct {
	num int
	id  string
	rid string
	tag string
}

func (p *pkg) slides() ([]slideRef, error) {
	pres := p.files["ppt/presentation.xml"]
	rels := p.files["ppt/_rels/presentation.xml.rels"]
	if pres == nil || rels == nil {
		return nil, fmt.Errorf("deckfill: presentation parts missing")
	}
	ridToNum := map[string]int{}
	for _, tag := range relTag.FindAll(rels, -1) {
		if !strings.Contains(string(tag), "slide") || strings.Contains(string(tag), "slideMaster") || strings.Contains(string(tag), "slideLayout") {
			continue
		}
		if attr(typeAttr, tag) != slideRelType && !strings.Contains(attr(tgtAttr, tag), "slides/slide") {
			continue
		}
		num := slideNum(attr(tgtAttr, tag))
		if num > 0 {
			ridToNum[attr(idAttr, tag)] = num
		}
	}
	var out []slideRef
	for _, tag := range sldIDTag.FindAll(pres, -1) {
		rid := attr(ridAttr, tag)
		num := ridToNum[rid]
		if num == 0 {
			continue
		}
		out = append(out, slideRef{num: num, id: attr(sidAttr, tag), rid: rid, tag: string(tag)})
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("deckfill: no slides")
	}
	return out, nil
}

func (p *pkg) slideXML(num int) []byte {
	return p.files[fmt.Sprintf("ppt/slides/slide%d.xml", num)]
}

func (p *pkg) setSlideXML(num int, xml []byte) {
	p.files[fmt.Sprintf("ppt/slides/slide%d.xml", num)] = xml
}

func (p *pkg) keep(nums []int) error {
	refs, err := p.slides()
	if err != nil {
		return err
	}
	byNum := map[int]slideRef{}
	for _, ref := range refs {
		byNum[ref.num] = ref
	}
	var tags []string
	keepNum := map[int]bool{}
	for _, n := range nums {
		ref, ok := byNum[n]
		if !ok {
			return fmt.Errorf("deckfill: slide %d is not in the template", n)
		}
		tags = append(tags, ref.tag)
		keepNum[n] = true
	}
	pres := p.files["ppt/presentation.xml"]
	list := regexp.MustCompile(`(?s)<p:sldIdLst\b[^>]*>.*?</p:sldIdLst>`)
	pres = list.ReplaceAll(pres, []byte("<p:sldIdLst>"+strings.Join(tags, "")+"</p:sldIdLst>"))
	p.files["ppt/presentation.xml"] = pres

	rels := p.files["ppt/_rels/presentation.xml.rels"]
	for _, tag := range relTag.FindAll(rels, -1) {
		num := slideNum(attr(tgtAttr, tag))
		if num == 0 || keepNum[num] {
			continue
		}
		rels = bytes.Replace(rels, tag, nil, 1)
	}
	p.files["ppt/_rels/presentation.xml.rels"] = rels
	for _, ref := range refs {
		if keepNum[ref.num] {
			continue
		}
		delete(p.files, fmt.Sprintf("ppt/slides/slide%d.xml", ref.num))
		delete(p.files, fmt.Sprintf("ppt/slides/_rels/slide%d.xml.rels", ref.num))
	}
	return nil
}

func (p *pkg) clone(num int) (int, error) {
	refs, err := p.slides()
	if err != nil {
		return 0, err
	}
	src := p.slideXML(num)
	if src == nil {
		return 0, fmt.Errorf("deckfill: slide %d missing", num)
	}
	next, maxID, maxRID := 0, 256, 0
	for _, ref := range refs {
		if ref.num > next {
			next = ref.num
		}
		if id := atoi(ref.id); id > maxID {
			maxID = id
		}
		if id := atoi(strings.TrimPrefix(ref.rid, "rId")); id > maxRID {
			maxRID = id
		}
	}
	for name := range p.files {
		if n := slideNum(name); n > next {
			next = n
		}
	}
	next++
	maxID++
	maxRID++
	p.files[fmt.Sprintf("ppt/slides/slide%d.xml", next)] = append([]byte(nil), src...)
	if rels := p.files[fmt.Sprintf("ppt/slides/_rels/slide%d.xml.rels", num)]; rels != nil {
		p.files[fmt.Sprintf("ppt/slides/_rels/slide%d.xml.rels", next)] = stripNotes(rels)
	}
	rid := fmt.Sprintf("rId%d", maxRID)
	p.files["ppt/_rels/presentation.xml.rels"] = bytes.Replace(
		p.files["ppt/_rels/presentation.xml.rels"],
		[]byte("</Relationships>"),
		[]byte(fmt.Sprintf(`<Relationship Id="%s" Type="%s" Target="slides/slide%d.xml"/></Relationships>`, rid, slideRelType, next)),
		1,
	)
	p.files["ppt/presentation.xml"] = bytes.Replace(
		p.files["ppt/presentation.xml"],
		[]byte("</p:sldIdLst>"),
		[]byte(fmt.Sprintf(`<p:sldId id="%d" r:id="%s"/></p:sldIdLst>`, maxID, rid)),
		1,
	)
	if ct := p.files["[Content_Types].xml"]; ct != nil {
		line := fmt.Sprintf(`<Override PartName="/ppt/slides/slide%d.xml" ContentType="application/vnd.openxmlformats-officedocument.presentationml.slide+xml"/>`, next)
		p.files["[Content_Types].xml"] = bytes.Replace(ct, []byte("</Types>"), []byte(line+"</Types>"), 1)
	}
	return next, nil
}

// renumber makes the kept slides slide1, slide2, ... so callers can rely on slide1.xml.
func (p *pkg) renumber() error {
	refs, err := p.slides()
	if err != nil {
		return err
	}
	type move struct{ from, to int }
	var moves []move
	for i, ref := range refs {
		if ref.num != i+1 {
			moves = append(moves, move{ref.num, i + 1})
		}
	}
	if len(moves) == 0 {
		return nil
	}
	for i, mv := range moves {
		tmp := 1000 + i
		p.renameSlide(mv.from, tmp)
		moves[i].from = tmp
	}
	for _, mv := range moves {
		p.renameSlide(mv.from, mv.to)
	}
	return nil
}

func (p *pkg) renameSlide(from, to int) {
	fromName := fmt.Sprintf("ppt/slides/slide%d.xml", from)
	toName := fmt.Sprintf("ppt/slides/slide%d.xml", to)
	if body, ok := p.files[fromName]; ok {
		p.files[toName] = body
		delete(p.files, fromName)
	}
	fromRels := fmt.Sprintf("ppt/slides/_rels/slide%d.xml.rels", from)
	toRels := fmt.Sprintf("ppt/slides/_rels/slide%d.xml.rels", to)
	if body, ok := p.files[fromRels]; ok {
		p.files[toRels] = body
		delete(p.files, fromRels)
	}
	fromTarget := fmt.Sprintf("slides/slide%d.xml", from)
	toTarget := fmt.Sprintf("slides/slide%d.xml", to)
	if rels := p.files["ppt/_rels/presentation.xml.rels"]; rels != nil {
		p.files["ppt/_rels/presentation.xml.rels"] = bytes.ReplaceAll(rels, []byte(fromTarget), []byte(toTarget))
	}
	fromPart := fmt.Sprintf("/ppt/slides/slide%d.xml", from)
	toPart := fmt.Sprintf("/ppt/slides/slide%d.xml", to)
	if ct := p.files["[Content_Types].xml"]; ct != nil {
		p.files["[Content_Types].xml"] = bytes.ReplaceAll(ct, []byte(fromPart), []byte(toPart))
	}
}

func stripNotes(rels []byte) []byte {
	for _, tag := range relTag.FindAll(rels, -1) {
		if strings.Contains(string(tag), "notesSlide") {
			rels = bytes.Replace(rels, tag, nil, 1)
		}
	}
	return rels
}

// gc drops parts no kept slide can reach, including unused pictures.
func (p *pkg) gc() {
	keep := map[string]bool{"[Content_Types].xml": true}
	var walk func(part string)
	walk = func(part string) {
		part = strings.TrimPrefix(part, "/")
		if part == "" || keep[part] || strings.Contains(part, "://") {
			return
		}
		if _, ok := p.files[part]; !ok {
			return
		}
		keep[part] = true
		relsName := relsFor(part)
		rels := p.files[relsName]
		if rels == nil {
			return
		}
		keep[relsName] = true
		for _, tag := range relTag.FindAll(rels, -1) {
			target := attr(tgtAttr, tag)
			if target == "" || strings.Contains(target, "://") {
				continue
			}
			walk(path.Clean(path.Join(path.Dir(part), target)))
		}
	}
	if rels := p.files["_rels/.rels"]; rels != nil {
		keep["_rels/.rels"] = true
		for _, tag := range relTag.FindAll(rels, -1) {
			target := attr(tgtAttr, tag)
			if target != "" && !strings.Contains(target, "://") {
				walk(path.Clean(target))
			}
		}
	}
	for name := range p.files {
		if !keep[name] {
			delete(p.files, name)
		}
	}
	if ct := p.files["[Content_Types].xml"]; ct != nil {
		for _, tag := range partAttr.FindAllSubmatch(ct, -1) {
			name := strings.TrimPrefix(string(tag[1]), "/")
			if _, ok := p.files[name]; !ok {
				ct = bytes.Replace(ct, tag[0], nil, 1)
			}
		}
		p.files["[Content_Types].xml"] = ct
	}
}

func relsFor(part string) string {
	dir, base := path.Split(part)
	return path.Join(dir, "_rels", base+".rels")
}

func attr(re *regexp.Regexp, tag []byte) string {
	m := re.FindSubmatch(tag)
	if m == nil {
		return ""
	}
	return string(m[1])
}

func slideNum(target string) int {
	base := path.Base(target)
	base = strings.TrimSuffix(base, ".xml")
	base = strings.TrimSuffix(base, ".rels")
	if !strings.HasPrefix(base, "slide") {
		return 0
	}
	return atoi(strings.TrimPrefix(base, "slide"))
}
