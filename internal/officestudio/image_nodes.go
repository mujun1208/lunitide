package officestudio

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"image"
	"io"
	"sort"
	"strconv"
	"strings"
)

type pictureRelationship struct {
	id, target, typ, mode string
	raw                   []byte
}
type pictureLocation struct {
	node       Node
	start, end int
	shapeID    int64
	name       string
}

func pictureRelationships(body []byte) (map[string]pictureRelationship, error) {
	out := map[string]pictureRelationship{}
	if len(body) == 0 {
		return out, nil
	}
	d := xml.NewDecoder(bytes.NewReader(body))
	var active *pictureRelationship
	start, depth, activeDepth := 0, 0, 0
	for {
		before := int(d.InputOffset())
		tok, err := d.Token()
		after := int(d.InputOffset())
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, ErrFormat
		}
		switch t := tok.(type) {
		case xml.StartElement:
			depth++
			if t.Name.Space == packageRelNS && t.Name.Local == "Relationship" {
				if active != nil {
					return nil, ErrFormat
				}
				start = before
				activeDepth = depth
				active = &pictureRelationship{id: xmlAttr(t, "Id"), target: xmlAttr(t, "Target"), typ: xmlAttr(t, "Type"), mode: xmlAttr(t, "TargetMode")}
			}
		case xml.EndElement:
			if active != nil && depth == activeDepth {
				if active.id == "" {
					return nil, ErrFormat
				}
				if _, exists := out[active.id]; exists {
					return nil, fmt.Errorf("%w: duplicate relationship ID", ErrFormat)
				}
				active.raw = body[start:after]
				out[active.id] = *active
				active = nil
			}
			depth--
		}
	}
	return out, nil
}

type picturePayload struct {
	NV struct {
		Properties struct {
			ID    int64  `xml:"id,attr"`
			Name  string `xml:"name,attr"`
			Alt   string `xml:"descr,attr"`
			Title string `xml:"title,attr"`
		} `xml:"cNvPr"`
	} `xml:"nvPicPr"`
	Fill struct {
		Blip struct {
			Embed string `xml:"embed,attr"`
		} `xml:"blip"`
		Crop struct {
			L int64 `xml:"l,attr"`
			R int64 `xml:"r,attr"`
			T int64 `xml:"t,attr"`
			B int64 `xml:"b,attr"`
		} `xml:"srcRect"`
	} `xml:"blipFill"`
	Shape struct {
		Transform struct {
			Offset struct {
				X int64 `xml:"x,attr"`
				Y int64 `xml:"y,attr"`
			} `xml:"off"`
			Extent struct {
				W int64 `xml:"cx,attr"`
				H int64 `xml:"cy,attr"`
			} `xml:"ext"`
		} `xml:"xfrm"`
	} `xml:"spPr"`
}

// Only direct rectangular pictures without groups, effects, hyperlinks or
// extensions are rewritten. Unknown structures remain byte-for-byte in the
// source and are exposed as non-editable nodes, never flattened away.
func simplePicture(raw []byte, namespaces map[string]string) bool {
	allowed := map[string]string{
		"pic": "", "nvPicPr": "", "cNvPr": "id name descr title", "cNvPicPr": "", "nvPr": "", "blipFill": "", "spPr": "",
		"blip": "embed", "picLocks": "noChangeAspect", "srcRect": "l r t b", "stretch": "", "fillRect": "", "xfrm": "", "off": "x y", "ext": "cx cy", "prstGeom": "prst", "avLst": "",
	}
	var wrapped bytes.Buffer
	wrapped.WriteString("<scope")
	for prefix, ns := range namespaces {
		if prefix == "" {
			wrapped.WriteString(` xmlns="` + escapeText(ns) + `"`)
		} else {
			wrapped.WriteString(` xmlns:` + prefix + `="` + escapeText(ns) + `"`)
		}
	}
	wrapped.WriteByte('>')
	wrapped.Write(raw)
	wrapped.WriteString("</scope>")
	d := xml.NewDecoder(bytes.NewReader(wrapped.Bytes()))
	counts := map[string]int{}
	depth := 0
	for {
		tok, err := d.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return false
		}
		if _, ok := tok.(xml.EndElement); ok {
			depth--
		}
		if t, ok := tok.(xml.StartElement); ok {
			depth++
			if depth == 1 {
				continue
			}
			attrs, ok := allowed[t.Name.Local]
			if !ok || (t.Name.Space != drawingNS && t.Name.Space != presentationNS) {
				return false
			}
			presentationElement := strings.Contains(" pic nvPicPr cNvPr cNvPicPr nvPr blipFill spPr ", " "+t.Name.Local+" ")
			if presentationElement && t.Name.Space != presentationNS || !presentationElement && t.Name.Space != drawingNS {
				return false
			}
			counts[t.Name.Local]++
			if counts[t.Name.Local] > 1 {
				return false
			}
			for _, a := range t.Attr {
				if a.Name.Space == "xmlns" || a.Name.Local == "xmlns" {
					continue
				}
				if !strings.Contains(" "+attrs+" ", " "+a.Name.Local+" ") {
					return false
				}
				if a.Name.Local == "embed" && a.Name.Space != officeRelNS {
					return false
				}
				if a.Name.Local != "embed" && a.Name.Space != "" {
					return false
				}
				if a.Name.Local == "prst" && a.Value != "rect" {
					return false
				}
			}
		}
	}
	return counts["pic"] == 1 && counts["cNvPr"] == 1 && counts["blip"] == 1 && counts["xfrm"] == 1 && counts["off"] == 1 && counts["ext"] == 1 && counts["prstGeom"] == 1 && counts["stretch"] == 1 && counts["fillRect"] == 1 && counts["srcRect"] <= 1
}

type rasterCheck struct {
	info rasterInfo
	err  error
}
type pictureScanBudget struct {
	rasters map[string]rasterCheck
	pixels  int64
	images  int
}

func (b *pictureScanBudget) raster(data []byte, sha string) (rasterInfo, error) {
	if b.rasters == nil {
		b.rasters = map[string]rasterCheck{}
	}
	if cached, ok := b.rasters[sha]; ok {
		return cached.info, cached.err
	}
	cfg, _, err := image.DecodeConfig(bytes.NewReader(data))
	var r rasterInfo
	if err == nil {
		pixels := int64(cfg.Width) * int64(cfg.Height)
		if cfg.Width > MaxImageDimension || cfg.Height > MaxImageDimension || pixels > MaxImagePixels || pixels > MaxPresentationImagePixels-b.pixels {
			err = ErrLimit
		} else {
			b.pixels += pixels
			r, err = validateRaster(data)
		}
	}
	b.rasters[sha] = rasterCheck{r, err}
	return r, err
}

func scanPictures(part string, parts map[string][]byte, budgets ...*pictureScanBudget) ([]pictureLocation, []Issue, error) {
	budget := &pictureScanBudget{}
	if len(budgets) > 0 {
		budget = budgets[0]
	}
	rels, err := pictureRelationships(parts[slideRels(part)])
	if err != nil {
		return nil, nil, err
	}
	body := parts[part]
	d := xml.NewDecoder(bytes.NewReader(body))
	depth, start, activeDepth := 0, -1, 0
	editableContainer := false
	var ancestors []xml.Name
	var namespaceStack []map[string]string
	var picNamespaces map[string]string
	out := []pictureLocation{}
	issues := []Issue{}
	seen := map[int64]bool{}
	for {
		before := int(d.InputOffset())
		tok, err := d.Token()
		after := int(d.InputOffset())
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, nil, ErrFormat
		}
		switch t := tok.(type) {
		case xml.StartElement:
			namespaces := map[string]string{}
			if len(namespaceStack) > 0 {
				for k, v := range namespaceStack[len(namespaceStack)-1] {
					namespaces[k] = v
				}
			}
			for _, a := range t.Attr {
				if a.Name.Space == "xmlns" {
					namespaces[a.Name.Local] = a.Value
				} else if a.Name.Local == "xmlns" {
					namespaces[""] = a.Value
				}
			}
			namespaceStack = append(namespaceStack, namespaces)
			depth++
			if t.Name.Space == presentationNS && t.Name.Local == "pic" {
				if start >= 0 {
					return nil, nil, ErrFormat
				}
				start = before
				activeDepth = depth
				picNamespaces = namespaces
				editableContainer = len(ancestors) > 0 && ancestors[len(ancestors)-1].Space == presentationNS && ancestors[len(ancestors)-1].Local == "spTree"
			}
			ancestors = append(ancestors, t.Name)
		case xml.EndElement:
			if start >= 0 && depth == activeDepth {
				raw := body[start:after]
				var pic picturePayload
				if xml.Unmarshal(raw, &pic) != nil || pic.NV.Properties.ID < 1 || seen[pic.NV.Properties.ID] {
					return nil, nil, fmt.Errorf("%w: invalid or duplicate picture identity", ErrFormat)
				}
				seen[pic.NV.Properties.ID] = true
				id := "node_" + digest([]byte(part + "\x00picture:" + strconv.FormatInt(pic.NV.Properties.ID, 10)))[:24]
				i := &ImageInfo{RelationshipID: pic.Fill.Blip.Embed, X: pic.Shape.Transform.Offset.X, Y: pic.Shape.Transform.Offset.Y, Width: pic.Shape.Transform.Extent.W, Height: pic.Shape.Transform.Extent.H, CropLeft: pic.Fill.Crop.L, CropRight: pic.Fill.Crop.R, CropTop: pic.Fill.Crop.T, CropBottom: pic.Fill.Crop.B}
				if strings.HasPrefix(pic.NV.Properties.Title, "sourceId=") {
					i.SourceID = strings.TrimPrefix(pic.NV.Properties.Title, "sourceId=")
				}
				n := Node{ID: id, Part: part, Kind: "image", Text: pic.NV.Properties.Alt, Ordinal: len(out) + 1, Editable: editableContainer && simplePicture(raw, picNamespaces), Locator: "picture:" + strconv.FormatInt(pic.NV.Properties.ID, 10), Image: i}
				rel, exists := rels[i.RelationshipID]
				if !exists || rel.typ != officeRelNS+"/image" || strings.EqualFold(rel.mode, "External") {
					n.Editable = false
					issues = append(issues, Issue{Code: "OFFICE_PICTURE_RELATIONSHIP", Severity: "blocked", Message: "图片引用缺失、类型不符或指向外部资源；保留原件，禁止自动渲染与替换。", Part: part, NodeID: id})
				} else {
					media, e := relationshipTarget(slideRels(part), rel.target)
					if e != nil {
						return nil, nil, e
					}
					i.MediaPart = media
					i.MediaSHA256 = digest(parts[media])
					r, e := budget.raster(parts[media], i.MediaSHA256)
					if e != nil {
						n.Editable = false
						issues = append(issues, Issue{Code: "OFFICE_PICTURE_UNSUPPORTED", Severity: "blocked", Message: "图片不是已验证的有界 PNG/JPEG，或像素数据损坏；SVG/EMF 等原样保留，不自动渲染与替换。", Part: media, NodeID: id})
					} else {
						i.PixelWidth = r.width
						i.PixelHeight = r.height
					}
				}
				if pic.NV.Properties.ID > 4_294_967_295 || i.Width <= 0 || i.Height <= 0 || i.X < 0 || i.Y < 0 || i.CropLeft < 0 || i.CropTop < 0 || i.CropRight < 0 || i.CropBottom < 0 || i.CropLeft >= 100000 || i.CropRight >= 100000 || i.CropTop >= 100000 || i.CropBottom >= 100000 || i.CropLeft+i.CropRight >= 100000 || i.CropTop+i.CropBottom >= 100000 {
					n.Editable = false
				}
				n.Digest = digest(bytes.Join([][]byte{raw, rel.raw, []byte(i.MediaSHA256)}, []byte{0}))
				out = append(out, pictureLocation{node: n, start: start, end: after, shapeID: pic.NV.Properties.ID, name: pic.NV.Properties.Name})
				start = -1
				budget.images++
				if budget.images > MaxPresentationImages {
					return nil, nil, ErrLimit
				}
			}
			depth--
			ancestors = ancestors[:len(ancestors)-1]
			namespaceStack = namespaceStack[:len(namespaceStack)-1]
		}
	}
	return out, issues, nil
}

func patchImages(data []byte, req PatchRequest) (PatchResult, error) {
	if req.Kind != PPTX || len(req.Operations) > 0 || len(req.Ranges) > 0 || len(req.Charts) > 0 || len(req.Images) > MaxPresentationImages {
		return PatchResult{}, fmt.Errorf("%w: picture changes require a separate PPTX-only batch", ErrFormat)
	}
	before, err := Inspect(PPTX, data)
	if err != nil {
		return PatchResult{}, err
	}
	if !before.RenderAllowed || before.Editability == "readonly" || before.Editability == "blocked" {
		return PatchResult{}, ErrReadOnly
	}
	p, err := readPackage(data)
	if err != nil {
		return PatchResult{}, err
	}
	sw, sh, err := deckSize(p.parts)
	if err != nil {
		return PatchResult{}, err
	}
	byID := map[string]pictureLocation{}
	budget := &pictureScanBudget{}
	for name := range p.parts {
		if isNodePart(PPTX, name) {
			pics, _, err := scanPictures(name, p.parts, budget)
			if err != nil {
				return PatchResult{}, err
			}
			for _, pic := range pics {
				byID[pic.node.ID] = pic
			}
		}
	}
	type replacement struct {
		loc pictureLocation
		raw string
	}
	changes := map[string][]replacement{}
	seen := map[string]bool{}
	replaced := map[string][]byte{}
	for _, op := range req.Images {
		loc, ok := byID[op.NodeID]
		if !ok || seen[op.NodeID] || op.ExpectedDigest == "" || loc.node.Digest != op.ExpectedDigest {
			return PatchResult{}, ErrConflict
		}
		seen[op.NodeID] = true
		if !loc.node.Editable {
			return PatchResult{}, ErrReadOnly
		}
		alt := loc.node.Text
		if op.Alt != nil {
			alt = *op.Alt
		}
		r, err := validateManagedImage(op.SourceID, op.SHA256, op.Fit, alt, op.Data)
		if err != nil {
			return PatchResult{}, err
		}
		box := *loc.node.Image
		if !validImageBox(box.X, box.Y, box.Width, box.Height, sw, sh) {
			return PatchResult{}, fmt.Errorf("%w: source picture geometry is unsupported", ErrReadOnly)
		}
		media, err := imagePart(p, replaced, op.SHA256, op.Data, r)
		if err != nil {
			return PatchResult{}, err
		}
		rel, err := addImageRelationship(p, replaced, loc.node.Part, media)
		if err != nil {
			return PatchResult{}, err
		}
		changes[loc.node.Part] = append(changes[loc.node.Part], replacement{loc: loc, raw: pictureXML(loc.shapeID, loc.name, op.SourceID, alt, rel, box, r, op.Fit)})
	}
	for part, edits := range changes {
		// Existing node locations are disjoint; preserve every byte between them.
		sort.Slice(edits, func(i, j int) bool { return edits[i].loc.start < edits[j].loc.start })
		var b bytes.Buffer
		last := 0
		for _, e := range edits {
			b.Write(p.parts[part][last:e.loc.start])
			b.WriteString(e.raw)
			last = e.loc.end
		}
		b.Write(p.parts[part][last:])
		replaced[part] = b.Bytes()
	}
	output, err := rewritePackage(p, replaced)
	if err != nil {
		return PatchResult{}, err
	}
	after, err := Inspect(PPTX, output)
	if err != nil {
		return PatchResult{}, err
	}
	if !after.RenderAllowed {
		return PatchResult{}, ErrReadOnly
	}
	oldNodes := map[string]Node{}
	for _, n := range before.Nodes {
		oldNodes[n.ID] = n
	}
	if len(before.Nodes) != len(after.Nodes) {
		return PatchResult{}, ErrConflict
	}
	for _, n := range after.Nodes {
		old, ok := oldNodes[n.ID]
		if !ok || !seen[n.ID] && old.Digest != n.Digest {
			return PatchResult{}, fmt.Errorf("%w: non-target picture/text changed", ErrConflict)
		}
	}
	var changed []string
	for _, part := range after.Parts {
		if _, ok := replaced[part.Name]; ok {
			changed = append(changed, part.Name)
		} else if old, ok := p.parts[part.Name]; !ok || digest(old) != part.SHA256 {
			return PatchResult{}, fmt.Errorf("%w: unrelated package part changed", ErrConflict)
		}
	}
	return PatchResult{Data: output, Inspection: after, ChangedParts: changed}, nil
}
