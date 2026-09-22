package deckfill

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ErrNoFillable means the deck has no prompt slots, so the caller should
// fall back to the plain generator.
var ErrNoFillable = fmt.Errorf("deckfill: no prompt slots")

// Point is one block of copy placed into a title slot, a body slot, or both.
type Point struct {
	Title   string
	Body    string
	TitleEn string
}

// Plan is the copy written into one deck.
type Plan struct {
	DeckTitle  string
	Presenter  string
	Department string
	Date       string
	Points     []Point
}

// OutlineSlide is the pptx.gen slide the model already wrote.
type OutlineSlide struct {
	Title    string
	Subtitle string
	Bullets  []string
}

// PlanFromOutline turns a title plus slide specs into fillable points.
// One outline slide becomes one point; bullets stay in that point's body.
func PlanFromOutline(title string, slides []OutlineSlide) Plan {
	plan := Plan{DeckTitle: strings.TrimSpace(title)}
	for _, slide := range slides {
		body := strings.TrimSpace(slide.Subtitle)
		for _, bullet := range slide.Bullets {
			bullet = strings.TrimSpace(bullet)
			if bullet == "" {
				continue
			}
			if body != "" {
				body += "\n"
			}
			body += bullet
		}
		if strings.TrimSpace(slide.Title) == "" && body == "" {
			continue
		}
		plan.Points = append(plan.Points, Point{Title: strings.TrimSpace(slide.Title), Body: body})
	}
	return plan
}

// DefaultRoot is the template library. LUNITIDE_DECK_LIBRARY=off disables it.
// An empty variable uses D:\PPT模板库 when that folder exists.
func DefaultRoot() string {
	switch os.Getenv("LUNITIDE_DECK_LIBRARY") {
	case "off", "0":
		return ""
	case "":
		const fallback = `D:\PPT模板库`
		if info, err := os.Stat(fallback); err == nil && info.IsDir() {
			return fallback
		}
		return ""
	default:
		root := os.Getenv("LUNITIDE_DECK_LIBRARY")
		if info, err := os.Stat(root); err == nil && info.IsDir() {
			return root
		}
		return ""
	}
}

// FindTemplate picks the named 工作总结 deck, then any other pptx in the library.
func FindTemplate(root string) (string, error) {
	preferred := filepath.Join(root, "工作总结", "工作总结.pptx")
	if info, err := os.Stat(preferred); err == nil && !info.IsDir() {
		return preferred, nil
	}
	var found string
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || found != "" {
			return nil
		}
		if strings.EqualFold(filepath.Ext(path), ".pptx") {
			found = path
		}
		return nil
	})
	if err != nil {
		return "", err
	}
	if found == "" {
		return "", fmt.Errorf("deckfill: no pptx in %s", root)
	}
	return found, nil
}

// Build clones prompt pages out of the library template and replaces sample copy.
func Build(root string, plan Plan) ([]byte, []string, error) {
	if root == "" {
		return nil, nil, ErrNoFillable
	}
	path, err := FindTemplate(root)
	if err != nil {
		return nil, nil, err
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, nil, err
	}
	return BuildBytes(raw, plan)
}

// BuildBytes fills a template already in memory.
func BuildBytes(raw []byte, plan Plan) ([]byte, []string, error) {
	deck, err := openPkg(raw)
	if err != nil {
		return nil, nil, err
	}
	refs, err := deck.slides()
	if err != nil {
		return nil, nil, err
	}
	type scored struct {
		num       int
		pairs     int
		score     int
		meta      int
		sample    bool
		imageOnly bool
	}
	var pages []scored
	for _, ref := range refs {
		xml := deck.slideXML(ref.num)
		if xml == nil {
			continue
		}
		_, title, meta, imageOnly := slotCounts(xml)
		pairs := len(pairContent(contentSlots(xml)))
		sample := false
		for _, sp := range shapeSpans(xml) {
			if Classify(shapeText(xml[sp[0]:sp[1]])) == SlotSampleTitle {
				sample = true
				break
			}
		}
		pages = append(pages, scored{ref.num, pairs, title*10 + pairs, meta, sample, imageOnly})
	}
	content := scored{}
	cover := scored{}
	imageOnly := 0
	for _, page := range pages {
		if page.imageOnly {
			imageOnly++
		}
		if page.pairs > 0 && page.score > content.score {
			content = page
		}
		if (page.sample || page.meta > 0) && !page.imageOnly && cover.num == 0 {
			cover = page
		}
	}
	if content.pairs == 0 {
		return nil, nil, ErrNoFillable
	}
	chunks := chunkPoints(plan.Points, content.pairs)
	order := make([]int, 0, len(chunks)+1)
	if cover.num != 0 && cover.num != content.num {
		order = append(order, cover.num)
	}
	order = append(order, content.num)
	for i := 1; i < len(chunks); i++ {
		n, err := deck.clone(content.num)
		if err != nil {
			return nil, nil, err
		}
		order = append(order, n)
	}
	var notes []string
	if imageOnly > 0 {
		notes = append(notes, "纯图页未插入")
	}
	if cover.num != 0 && cover.num != content.num {
		xml, extra := fillSlide(deck.slideXML(cover.num), Plan{
			DeckTitle: plan.DeckTitle, Presenter: plan.Presenter, Department: plan.Department, Date: plan.Date,
		})
		deck.setSlideXML(cover.num, xml)
		notes = append(notes, extra...)
	}
	contentPages := order
	if cover.num != 0 && cover.num != content.num {
		contentPages = order[1:]
	}
	for i, num := range contentPages {
		pagePlan := Plan{Points: chunks[i]}
		if i == 0 && (cover.num == 0 || cover.num == content.num) {
			pagePlan.DeckTitle = plan.DeckTitle
			pagePlan.Presenter = plan.Presenter
			pagePlan.Department = plan.Department
			pagePlan.Date = plan.Date
		}
		xml, extra := fillSlide(deck.slideXML(num), pagePlan)
		deck.setSlideXML(num, xml)
		notes = append(notes, extra...)
	}
	if err := deck.keep(order); err != nil {
		return nil, nil, err
	}
	if err := deck.renumber(); err != nil {
		return nil, nil, err
	}
	deck.gc()
	notes = append(notes, fontNotes(deck.files)...)
	out, err := deck.bytes()
	return out, uniqueNotes(notes), err
}

// FillKept keeps the given slides, in that order, and fills each with the full plan.
// Used by the proof that checks two real template pages.
func FillKept(raw []byte, slideNums []int, plan Plan) ([]byte, []string, error) {
	deck, err := openPkg(raw)
	if err != nil {
		return nil, nil, err
	}
	var notes []string
	for _, num := range slideNums {
		xml := deck.slideXML(num)
		if xml == nil {
			return nil, nil, fmt.Errorf("deckfill: slide %d missing", num)
		}
		next, extra := fillSlide(xml, plan)
		deck.setSlideXML(num, next)
		notes = append(notes, extra...)
	}
	if err := deck.keep(slideNums); err != nil {
		return nil, nil, err
	}
	deck.gc()
	notes = append(notes, fontNotes(deck.files)...)
	out, err := deck.bytes()
	return out, uniqueNotes(notes), err
}

func chunkPoints(points []Point, size int) [][]Point {
	if size < 1 {
		size = 1
	}
	if len(points) == 0 {
		return [][]Point{nil}
	}
	var chunks [][]Point
	for len(points) > 0 {
		n := size
		if n > len(points) {
			n = len(points)
		}
		chunks = append(chunks, points[:n])
		points = points[n:]
	}
	return chunks
}

func uniqueNotes(notes []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, note := range notes {
		if note == "" || seen[note] {
			continue
		}
		seen[note] = true
		out = append(out, note)
	}
	return out
}
