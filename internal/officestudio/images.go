package officestudio

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"path"
	"strconv"

	"github.com/oklog/ulid/v2"
)

const (
	MaxImageBytes              = 8 << 20
	MaxImagePixels             = 24_000_000
	MaxImageDimension          = 8192
	MaxPresentationImagePixels = 96_000_000
	MaxImagesPerSlide          = 16
	MaxPresentationImages      = 128
	presentationNS             = "http://schemas.openxmlformats.org/presentationml/2006/main"
	packageRelNS               = "http://schemas.openxmlformats.org/package/2006/relationships"
	officeRelNS                = "http://schemas.openxmlformats.org/officeDocument/2006/relationships"
	contentTypeNS              = "http://schemas.openxmlformats.org/package/2006/content-types"
)

type rasterInfo struct {
	width, height int
	ext, mime     string
}

func validateRaster(data []byte) (rasterInfo, error) {
	if len(data) == 0 || len(data) > MaxImageBytes {
		return rasterInfo{}, ErrLimit
	}
	cfg, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil || (format != "png" && format != "jpeg") {
		return rasterInfo{}, fmt.Errorf("%w: only decoded PNG/JPEG pictures are supported", ErrFormat)
	}
	if cfg.Width < 1 || cfg.Height < 1 || cfg.Width > MaxImageDimension || cfg.Height > MaxImageDimension || int64(cfg.Width)*int64(cfg.Height) > MaxImagePixels {
		return rasterInfo{}, ErrLimit
	}
	// Decode the complete bounded payload to reject a valid header followed by
	// corrupt or truncated pixels. The decoded surface is never persisted.
	decoded, actual, err := image.Decode(bytes.NewReader(data))
	if err != nil || actual != format || decoded.Bounds().Dx() != cfg.Width || decoded.Bounds().Dy() != cfg.Height {
		return rasterInfo{}, fmt.Errorf("%w: corrupt picture pixels", ErrFormat)
	}
	r := rasterInfo{width: cfg.Width, height: cfg.Height, ext: "png", mime: "image/png"}
	if format == "jpeg" {
		r.ext = "jpg"
		r.mime = "image/jpeg"
	}
	return r, nil
}

func validateManagedImage(id, sha, fit, alt string, data []byte) (rasterInfo, error) {
	if err := validateImageReference(id, sha, fit, alt, data); err != nil {
		return rasterInfo{}, err
	}
	return validateRaster(data)
}

func validateImageReference(id, sha, fit, alt string, data []byte) error {
	u, err := ulid.ParseStrict(id)
	if err != nil || u.String() != id || len(sha) != 64 || digest(data) != sha {
		return fmt.Errorf("%w: picture source ID or SHA256 does not match hydrated bytes", ErrConflict)
	}
	if fit != "contain" && fit != "cover" {
		return fmt.Errorf("%w: picture fit must be contain or cover", ErrFormat)
	}
	if !validText(alt) || len(alt) > 2048 {
		return ErrLimit
	}
	return nil
}

func deckSize(parts map[string][]byte) (int64, int64, error) {
	d := xml.NewDecoder(bytes.NewReader(parts["ppt/presentation.xml"]))
	for {
		t, err := d.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return 0, 0, ErrFormat
		}
		if s, ok := t.(xml.StartElement); ok && s.Name.Space == presentationNS && s.Name.Local == "sldSz" {
			w, _ := strconv.ParseInt(xmlAttr(s, "cx"), 10, 64)
			h, _ := strconv.ParseInt(xmlAttr(s, "cy"), 10, 64)
			if w > 0 && h > 0 && w <= 100_000_000 && h <= 100_000_000 {
				return w, h, nil
			}
			return 0, 0, ErrFormat
		}
	}
	return 0, 0, fmt.Errorf("%w: slide dimensions missing", ErrFormat)
}

func validImageBox(x, y, w, h, sw, sh int64) bool {
	return x >= 0 && y >= 0 && w > 0 && h > 0 && x <= sw && y <= sh && w <= sw-x && h <= sh-y
}
func xmlAttr(s xml.StartElement, key string) string {
	for _, a := range s.Attr {
		if a.Name.Local == key {
			return a.Value
		}
	}
	return ""
}
func slideRels(part string) string {
	return path.Join(path.Dir(part), "_rels", path.Base(part)+".rels")
}

// appendXMLChild inserts before the namespace-qualified root/end element,
// preserving all pre-existing bytes and namespace prefixes in imported parts.
func appendXMLChild(body []byte, ns, local, child string) ([]byte, error) {
	d := xml.NewDecoder(bytes.NewReader(body))
	depth := 0
	for {
		before := int(d.InputOffset())
		t, err := d.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, ErrFormat
		}
		switch e := t.(type) {
		case xml.StartElement:
			depth++
		case xml.EndElement:
			if e.Name.Space == ns && e.Name.Local == local {
				// A self-closing element has no literal end tag to insert before.
				if before >= len(body) || !bytes.HasPrefix(bytes.TrimSpace(body[before:]), []byte("</")) {
					return nil, ErrReadOnly
				}
				out := make([]byte, 0, len(body)+len(child))
				out = append(out, body[:before]...)
				out = append(out, child...)
				out = append(out, body[before:]...)
				return out, nil
			}
			depth--
		}
	}
	return nil, fmt.Errorf("%w: insertion target %s missing", ErrFormat, local)
}

func nextShapeID(body []byte) (int64, error) {
	d := xml.NewDecoder(bytes.NewReader(body))
	var largest int64
	for {
		t, err := d.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return 0, ErrFormat
		}
		if s, ok := t.(xml.StartElement); ok && s.Name.Space == presentationNS && s.Name.Local == "cNvPr" {
			id, e := strconv.ParseInt(xmlAttr(s, "id"), 10, 64)
			if e != nil || id < 1 || id >= 4_294_967_295 {
				return 0, ErrFormat
			}
			largest = max(largest, id)
		}
	}
	return largest + 1, nil
}

func pictureXML(shapeID int64, name, sourceID, alt, relID string, box ImageInfo, r rasterInfo, fit string) string {
	x, y, w, h := box.X, box.Y, box.Width, box.Height
	var l, right, top, bottom int64
	// Multiplication stays below 100 million EMU * 8192 pixels.
	if fit == "contain" {
		if w*int64(r.height) > h*int64(r.width) {
			nw := max(int64(1), h*int64(r.width)/int64(r.height))
			x += (w - nw) / 2
			w = nw
		} else {
			nh := max(int64(1), w*int64(r.height)/int64(r.width))
			y += (h - nh) / 2
			h = nh
		}
	} else if w*int64(r.height) < h*int64(r.width) {
		l = (100000 - w*int64(r.height)*100000/(h*int64(r.width))) / 2
		right = l
	} else {
		top = (100000 - h*int64(r.width)*100000/(w*int64(r.height))) / 2
		bottom = top
	}
	return fmt.Sprintf(`<p:pic xmlns:p="%s" xmlns:a="%s" xmlns:r="%s"><p:nvPicPr><p:cNvPr id="%d" name="%s" descr="%s" title="sourceId=%s"/><p:cNvPicPr><a:picLocks noChangeAspect="1"/></p:cNvPicPr><p:nvPr/></p:nvPicPr><p:blipFill><a:blip r:embed="%s"/><a:srcRect l="%d" r="%d" t="%d" b="%d"/><a:stretch><a:fillRect/></a:stretch></p:blipFill><p:spPr><a:xfrm><a:off x="%d" y="%d"/><a:ext cx="%d" cy="%d"/></a:xfrm><a:prstGeom prst="rect"><a:avLst/></a:prstGeom></p:spPr></p:pic>`, presentationNS, drawingNS, officeRelNS, shapeID, escapeText(name), escapeText(alt), escapeText(sourceID), escapeText(relID), l, right, top, bottom, x, y, w, h)
}

func imagePart(p packageData, replaced map[string][]byte, sha string, data []byte, r rasterInfo) (string, error) {
	name := "ppt/media/studio-" + sha + "." + r.ext
	if old, ok := p.parts[name]; ok {
		if !bytes.Equal(old, data) {
			return "", ErrConflict
		}
	} else {
		replaced[name] = append([]byte(nil), data...)
	}
	ct := p.parts["[Content_Types].xml"]
	if updated, ok := replaced["[Content_Types].xml"]; ok {
		ct = updated
	}
	d := xml.NewDecoder(bytes.NewReader(ct))
	exists := false
	for {
		t, err := d.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return "", ErrFormat
		}
		if s, ok := t.(xml.StartElement); ok && s.Name.Space == contentTypeNS && s.Name.Local == "Override" && xmlAttr(s, "PartName") == "/"+name {
			if xmlAttr(s, "ContentType") != r.mime {
				return "", ErrConflict
			}
			exists = true
		}
	}
	if !exists {
		var err error
		ct, err = appendXMLChild(ct, contentTypeNS, "Types", fmt.Sprintf(`<Override xmlns="%s" PartName="/%s" ContentType="%s"/>`, contentTypeNS, name, r.mime))
		if err != nil {
			return "", err
		}
		replaced["[Content_Types].xml"] = ct
	}
	return name, nil
}

func addImageRelationship(p packageData, replaced map[string][]byte, part, media string) (string, error) {
	rp := slideRels(part)
	body := p.parts[rp]
	if v, ok := replaced[rp]; ok {
		body = v
	}
	if len(body) == 0 {
		body = []byte(`<Relationships xmlns="` + packageRelNS + `"></Relationships>`)
	}
	rels, err := pictureRelationships(body)
	if err != nil {
		return "", err
	}
	id := ""
	for n := 1; n <= MaxParts+MaxPresentationImages; n++ {
		candidate := "rIdStudioImage" + strconv.Itoa(n)
		if _, ok := rels[candidate]; !ok {
			id = candidate
			break
		}
	}
	if id == "" {
		return "", ErrLimit
	}
	child := fmt.Sprintf(`<Relationship xmlns="%s" Id="%s" Type="%s/image" Target="../media/%s"/>`, packageRelNS, id, officeRelNS, path.Base(media))
	updated, err := appendXMLChild(body, packageRelNS, "Relationships", child)
	if err != nil {
		return "", err
	}
	replaced[rp] = updated
	return id, nil
}

func addSlideImages(data []byte, slides []Slide) ([]byte, error) {
	total := 0
	for _, s := range slides {
		if len(s.Images) > MaxImagesPerSlide {
			return nil, ErrLimit
		}
		total += len(s.Images)
	}
	if total == 0 {
		return data, nil
	}
	if total > MaxPresentationImages {
		return nil, ErrLimit
	}
	p, err := readPackage(data)
	if err != nil {
		return nil, err
	}
	sw, sh, err := deckSize(p.parts)
	if err != nil {
		return nil, err
	}
	replaced := map[string][]byte{}
	budget := &pictureScanBudget{}
	for i, s := range slides {
		part := fmt.Sprintf("ppt/slides/slide%d.xml", i+1)
		body := p.parts[part]
		id, err := nextShapeID(body)
		if err != nil {
			return nil, err
		}
		for j, img := range s.Images {
			if err := validateImageReference(img.SourceID, img.SHA256, img.Fit, img.Alt, img.Data); err != nil {
				return nil, err
			}
			r, err := budget.raster(img.Data, img.SHA256)
			if err != nil {
				return nil, err
			}
			if !validImageBox(img.X, img.Y, img.Width, img.Height, sw, sh) {
				return nil, fmt.Errorf("%w: picture box exceeds slide %d", ErrFormat, i+1)
			}
			media, err := imagePart(p, replaced, img.SHA256, img.Data, r)
			if err != nil {
				return nil, err
			}
			rel, err := addImageRelationship(p, replaced, part, media)
			if err != nil {
				return nil, err
			}
			pic := pictureXML(id+int64(j), "Picture "+strconv.Itoa(j+1), img.SourceID, img.Alt, rel, ImageInfo{X: img.X, Y: img.Y, Width: img.Width, Height: img.Height}, r, img.Fit)
			body, err = appendXMLChild(body, presentationNS, "spTree", pic)
			if err != nil {
				return nil, err
			}
		}
		if len(s.Images) > 0 {
			replaced[part] = body
		}
	}
	return rewritePackage(p, replaced)
}
