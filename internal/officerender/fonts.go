package officerender

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/xml"
	"errors"
	"io"
	"path"
	"sort"
	"strings"
)

type FontInventory struct {
	Families []string `json:"-"`
	Basis    string   `json:"basis"`
	Complete bool     `json:"complete"`
	Notice   string   `json:"notice,omitempty"`
}

type FontFamilyStatus struct {
	Family            string   `json:"family"`
	Status            string   `json:"status"` // available, missing, unknown
	ReferenceCount    int      `json:"referenceCount"`
	Parts             []string `json:"parts"`
	PartsTruncated    bool     `json:"partsTruncated"`
	SuggestedFallback string   `json:"suggestedFallback,omitempty"`
	Notice            string   `json:"notice,omitempty"`
}

type FontReport struct {
	SourceDigest            string             `json:"sourceDigest"`
	InventoryBasis          string             `json:"inventoryBasis"`
	InventoryComplete       bool               `json:"inventoryComplete"`
	InventoryFamilies       int                `json:"inventoryFamilies"`
	DeclarationScanComplete bool               `json:"declarationScanComplete"`
	Families                []FontFamilyStatus `json:"families"`
	MissingCount            int                `json:"missingCount"`
	UnknownCount            int                `json:"unknownCount"`
	SubstitutionVerified    bool               `json:"substitutionVerified"`
	Notice                  string             `json:"notice"`
}

// FontReport compares literal/style font declarations with the OS font-family
// inventory. Neither GDI enumeration nor PDF conversion proves which face and
// fallback glyphs a renderer used; that boundary is explicit in the report.
func (r *Renderer) FontReport(ctx context.Context, kind string, data []byte) (FontReport, error) {
	sum := sha256.Sum256(data)
	report := FontReport{SourceDigest: hex.EncodeToString(sum[:]), Families: []FontFamilyStatus{}, Notice: "字体家族可用性检查；包括文档样式中的声明，不代表每项都实际用于正文。尚未验证渲染器实际替代、每个字符的字形覆盖、粗体/斜体与字体度量；未改写任何字体。"}
	declared, complete, err := declaredFontFamilies(ctx, kind, data)
	if err != nil {
		return report, err
	}
	report.DeclarationScanComplete = complete
	inventory := r.ListFonts
	if inventory == nil {
		inventory = systemFontInventory
	}
	fonts, inventoryErr := inventory(ctx)
	if ctx.Err() != nil {
		return report, ctx.Err()
	}
	report.InventoryBasis = fonts.Basis
	report.InventoryComplete = fonts.Complete && inventoryErr == nil
	available := map[string]string{}
	for _, font := range fonts.Families {
		if name := fontFamilyName(font); name != "" {
			available[fontFamilyKey(name)] = name
		}
	}
	report.InventoryFamilies = len(available)
	if inventoryErr != nil {
		report.Notice += " 本机字体清单暂时不可读取。"
	}
	if fonts.Notice != "" {
		report.Notice += " " + fonts.Notice
	}
	fallback := ""
	for _, name := range []string{"Noto Sans CJK SC", "Noto Sans SC", "Microsoft YaHei", "微软雅黑", "Arial", "Liberation Sans", "DejaVu Sans"} {
		if match := available[fontFamilyKey(name)]; match != "" {
			fallback = match
			break
		}
	}
	for _, family := range declared {
		if strings.HasPrefix(family.Family, "theme:") {
			family.Status = "unknown"
			family.Notice = "主题字体缺少唯一映射，尚未解析到具体家族。"
			report.UnknownCount++
		} else if _, ok := available[fontFamilyKey(family.Family)]; ok {
			family.Status = "available"
		} else if report.InventoryComplete {
			family.Status = "missing"
			family.SuggestedFallback = fallback
			family.Notice = "本机清单未找到同名家族，可能发生字体替代或布局改变；本地化名称别名及嵌入字体仍需渲染检查。"
			report.MissingCount++
		} else {
			family.Status = "unknown"
			family.Notice = "本机字体清单不完整，不能把未匹配当作确定缺失。"
			report.UnknownCount++
		}
		report.Families = append(report.Families, family)
	}
	if len(declared) == 0 {
		report.Notice += " 未找到可直接核对的字体声明，不能据此认定字体检查全部通过。"
	}
	return report, nil
}

func fontFamilyName(name string) string {
	name = strings.TrimSpace(name)
	if len(name) == 0 || len(name) > 256 || strings.ContainsAny(name, "\x00\r\n\t") {
		return ""
	}
	return name
}
func fontFamilyKey(name string) string {
	return strings.ToLower(strings.TrimPrefix(strings.TrimSpace(name), "@"))
}

type fontDeclaration struct{ family, part string }

func declaredFontFamilies(ctx context.Context, kind string, data []byte) ([]FontFamilyStatus, bool, error) {
	prefix := map[string]string{"docx": "word/", "pptx": "ppt/", "xlsx": "xl/"}[kind]
	if prefix == "" || len(data) == 0 || len(data) > 32<<20 {
		return nil, false, errors.New("字体检查输入格式或长度不支持")
	}
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, false, err
	}
	if len(zr.File) > 4096 {
		return nil, false, errors.New("字体检查包部件数超过限制")
	}
	declarations := []fontDeclaration{}
	complete := true
	themes := map[string]map[string]bool{}
	var total uint64
	seen := map[string]bool{}
	for _, f := range zr.File {
		if !strings.HasPrefix(f.Name, prefix) || !strings.HasSuffix(f.Name, ".xml") {
			continue
		}
		if path.Clean(f.Name) != f.Name || strings.ContainsAny(f.Name, "\\:\x00") || seen[strings.ToLower(f.Name)] {
			return nil, false, errors.New("字体检查包路径不合法或重复")
		}
		seen[strings.ToLower(f.Name)] = true
		if f.UncompressedSize64 > 16<<20 || total+f.UncompressedSize64 > 128<<20 {
			return nil, false, errors.New("字体声明内容超出检查上限")
		}
		total += f.UncompressedSize64
		if err = ctx.Err(); err != nil {
			return nil, false, err
		}
		reader, e := f.Open()
		if e != nil {
			return nil, false, e
		}
		b, e := io.ReadAll(io.LimitReader(reader, 16<<20+1))
		closeErr := reader.Close()
		if e != nil {
			return nil, false, e
		}
		if closeErr != nil {
			return nil, false, closeErr
		}
		if len(b) > 16<<20 {
			return nil, false, errors.New("字体部件过大")
		}
		themePart := strings.Contains(f.Name, "/theme/")
		dec := xml.NewDecoder(bytes.NewReader(b))
		parents := []string{}
		count := 0
		for {
			token, e := dec.Token()
			if errors.Is(e, io.EOF) {
				break
			}
			if e != nil {
				return nil, false, e
			}
			count++
			if count%256 == 0 {
				if e = ctx.Err(); e != nil {
					return nil, false, e
				}
			}
			if count > 2_000_000 {
				return nil, false, errors.New("字体XML超过解析上限")
			}
			switch t := token.(type) {
			case xml.Directive:
				return nil, false, errors.New("字体XML不支持外部声明")
			case xml.StartElement:
				if len(parents) > 128 {
					return nil, false, errors.New("字体XML嵌套过深")
				}
				parents = append(parents, t.Name.Local)
				attr := func(name string) string {
					for _, a := range t.Attr {
						if a.Name.Local == name {
							return a.Value
						}
					}
					return ""
				}
				add := func(value string) {
					raw := value
					value = fontFamilyName(value)
					if value != "" {
						declarations = append(declarations, fontDeclaration{value, f.Name})
					} else if strings.TrimSpace(raw) != "" {
						complete = false
					}
				}
				const drawingNS = "http://schemas.openxmlformats.org/drawingml/2006/main"
				const wordNS = "http://schemas.openxmlformats.org/wordprocessingml/2006/main"
				const sheetNS = "http://schemas.openxmlformats.org/spreadsheetml/2006/main"
				if themePart {
					if t.Name.Space == drawingNS && (t.Name.Local == "latin" || t.Name.Local == "ea" || t.Name.Local == "cs") {
						family := fontFamilyName(attr("typeface"))
						base := ""
						for _, parent := range parents {
							if parent == "majorFont" {
								base = "+mj-"
							}
							if parent == "minorFont" {
								base = "+mn-"
							}
						}
						suffix := map[string]string{"latin": "lt", "ea": "ea", "cs": "cs"}[t.Name.Local]
						if base != "" && family != "" {
							if themes[base+suffix] == nil {
								themes[base+suffix] = map[string]bool{}
							}
							themes[base+suffix][family] = true
						}
					}
					continue
				}
				if t.Name.Space == drawingNS && (t.Name.Local == "latin" || t.Name.Local == "ea" || t.Name.Local == "cs" || t.Name.Local == "sym") {
					add(attr("typeface"))
				}
				if t.Name.Space == wordNS && t.Name.Local == "rFonts" {
					for _, name := range []string{"ascii", "hAnsi", "eastAsia", "cs"} {
						add(attr(name))
					}
					for _, name := range []string{"asciiTheme", "hAnsiTheme", "eastAsiaTheme", "cstheme", "csTheme"} {
						value := attr(name)
						if value != "" {
							add(wordThemeFont(value))
						}
					}
				}
				if t.Name.Space == sheetNS && (t.Name.Local == "rFont" || t.Name.Local == "name" && strings.Contains(strings.Join(parents, "/"), "font")) {
					add(attr("val"))
				}
				if len(declarations) > 50000 {
					return nil, false, errors.New("字体引用超过检查上限")
				}
			case xml.EndElement:
				if len(parents) > 0 {
					parents = parents[:len(parents)-1]
				}
			}
		}
	}
	families := map[string]*FontFamilyStatus{}
	for _, d := range declarations {
		if strings.HasPrefix(d.family, "+") {
			options := themes[d.family]
			if len(options) == 1 {
				for name := range options {
					d.family = name
				}
			} else {
				d.family = "theme:" + d.family
			}
		}
		key := fontFamilyKey(d.family)
		family := families[key]
		if family == nil {
			if len(families) >= 128 {
				complete = false
				continue
			}
			family = &FontFamilyStatus{Family: d.family, Parts: []string{}}
			families[key] = family
		}
		family.ReferenceCount++
		if len(d.part) > 240 {
			d.part = string([]rune(d.part)[:min(60, len([]rune(d.part)))]) + "…"
			family.PartsTruncated = true
		}
		found := false
		for _, part := range family.Parts {
			if part == d.part {
				found = true
				break
			}
		}
		if !found {
			if len(family.Parts) < 4 {
				family.Parts = append(family.Parts, d.part)
			} else {
				family.PartsTruncated = true
			}
		}
	}
	out := make([]FontFamilyStatus, 0, len(families))
	for _, f := range families {
		sort.Strings(f.Parts)
		out = append(out, *f)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Family < out[j].Family })
	return out, complete, nil
}

func wordThemeFont(value string) string {
	base := ""
	switch {
	case strings.HasPrefix(value, "major"):
		base = "+mj-"
	case strings.HasPrefix(value, "minor"):
		base = "+mn-"
	default:
		return "theme:" + value
	}
	switch {
	case strings.HasSuffix(value, "Ascii"), strings.HasSuffix(value, "HAnsi"):
		return base + "lt"
	case strings.HasSuffix(value, "EastAsia"):
		return base + "ea"
	case strings.HasSuffix(value, "Bidi"):
		return base + "cs"
	}
	return "theme:" + value
}
