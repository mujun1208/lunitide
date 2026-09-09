package officestudio

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"io"
)

// Word producers may renumber footer/header ZIP members. Match a story by
// section and first/even/default role, never by a coincidentally equal filename.
func wordSectionStories(p packageData) ([]map[string]string, error) {
	rels, err := pictureRelationships(p.parts["word/_rels/document.xml.rels"])
	if err != nil {
		return nil, err
	}
	d := xml.NewDecoder(bytes.NewReader(p.parts["word/document.xml"]))
	sections := []map[string]string{}
	inherited := map[string]string{}
	var current map[string]string
	depth, sectionDepth := 0, 0
	for {
		token, err := d.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, ErrFormat
		}
		switch t := token.(type) {
		case xml.StartElement:
			depth++
			if t.Name.Space != wordNS {
				continue
			}
			if t.Name.Local == "sectPr" {
				if current != nil {
					return nil, ErrReadOnly
				}
				current = map[string]string{}
				sectionDepth = depth
			}
			if current == nil || depth != sectionDepth+1 || (t.Name.Local != "headerReference" && t.Name.Local != "footerReference") {
				continue
			}
			role, id := "", ""
			for _, a := range t.Attr {
				if a.Name.Space == wordNS && a.Name.Local == "type" {
					role = a.Value
				}
				if a.Name.Space == officeRelNS && a.Name.Local == "id" {
					id = a.Value
				}
			}
			if role != "default" && role != "first" && role != "even" {
				return nil, ErrReadOnly
			}
			kind := "header"
			if t.Name.Local == "footerReference" {
				kind = "footer"
			}
			key := kind + ":" + role
			if _, exists := current[key]; exists {
				return nil, ErrFormat
			}
			r, ok := rels[id]
			if !ok || r.typ != officeRelNS+"/"+kind || r.mode != "" && r.mode != "Internal" {
				return nil, ErrReadOnly
			}
			part, err := relationshipTarget("word/_rels/document.xml.rels", r.target)
			if err != nil || len(p.parts[part]) == 0 {
				return nil, ErrFormat
			}
			current[key] = part
		case xml.EndElement:
			if current != nil && depth == sectionDepth {
				for role, part := range inherited {
					if _, exists := current[role]; !exists {
						current[role] = part
					}
				}
				sections = append(sections, current)
				inherited = current
				current = nil
				if len(sections) > 1000 {
					return nil, ErrLimit
				}
			}
			depth--
		}
	}
	if current != nil || len(sections) == 0 {
		return nil, ErrFormat
	}
	return sections, nil
}

func matchNativeWordStories(p, q packageData) (map[string][]string, error) {
	before, err := wordSectionStories(p)
	if err != nil {
		return nil, err
	}
	after, err := wordSectionStories(q)
	if err != nil {
		return nil, err
	}
	if len(before) != len(after) {
		return nil, fmt.Errorf("%w: 原生候选改变了文档分节，不能匹配页眉页脚", ErrReadOnly)
	}
	result := map[string][]string{"word/document.xml": {"word/document.xml"}}
	for i, section := range before {
		for role, part := range section {
			nativePart := after[i][role]
			if nativePart == "" {
				return nil, fmt.Errorf("%w: 第 %d 节的 %s 未匹配", ErrConflict, i+1, role)
			}
			seen := false
			for _, old := range result[part] {
				if old == nativePart {
					seen = true
				}
			}
			if !seen {
				result[part] = append(result[part], nativePart)
			}
		}
	}
	return result, nil
}

func matchingWordFieldResult(q packageData, parts []string) ([]nativeWordField, string, error) {
	if len(parts) == 0 {
		return nil, "", ErrReadOnly
	}
	var baseline []nativeWordField
	plain := ""
	for i, part := range parts {
		values, text, err := wordFieldValues(q.parts[part])
		if err != nil {
			return nil, "", err
		}
		if i == 0 {
			baseline, plain = values, text
			continue
		}
		if text != plain || len(values) != len(baseline) {
			return nil, "", ErrConflict
		}
		for j, value := range values {
			if value != baseline[j] {
				return nil, "", fmt.Errorf("%w: 同一页眉页脚在不同分节的缓存结果不同，请在目标软件更新", ErrReadOnly)
			}
		}
	}
	return baseline, plain, nil
}

func simpleFieldRunProperties(body []byte) (string, error) {
	// A uniform source field style can be retained for updated display text.
	// Heterogeneous or structured field displays need a format-aware editor.
	wrapped := []byte(`<root xmlns:w="` + wordNS + `">` + string(body) + `</root>`)
	runs, err := spans(wrapped, wordNS, "r")
	if err != nil || len(runs) == 0 {
		return "", ErrReadOnly
	}
	baseline := ""
	for i, run := range runs {
		props, err := spans(wrapped[run.innerStart:run.innerEnd], wordNS, "rPr")
		if err != nil || len(props) > 1 {
			return "", ErrReadOnly
		}
		style := ""
		if len(props) == 1 {
			style = string(wrapped[run.innerStart+props[0].start : run.innerStart+props[0].end])
		}
		if i == 0 {
			baseline = style
		} else if style != baseline {
			return "", ErrReadOnly
		}
	}
	return baseline, nil
}
