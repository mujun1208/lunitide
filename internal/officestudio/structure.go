package officestudio

import (
	"bytes"
	"encoding/xml"
	"io"
	"strconv"
	"strings"
)

func inspectStructure(kind Kind, parts map[string][]byte) (*Structure, error) {
	s := &Structure{HeadingCounts: map[string]int{}}
	if kind != DOCX && kind != PPTX {
		return nil, nil
	}
	for part, body := range parts {
		if !isNodePart(kind, part) {
			continue
		}
		dec := xml.NewDecoder(bytes.NewReader(body))
		type wordField struct {
			instruction strings.Builder
			collecting  bool
		}
		var fields []*wordField
		inInstruction := false
		for {
			tok, err := dec.Token()
			if err == io.EOF {
				break
			}
			if err != nil {
				return nil, ErrFormat
			}
			switch node := tok.(type) {
			case xml.CharData:
				if inInstruction && len(fields) > 0 && fields[len(fields)-1].collecting {
					fields[len(fields)-1].instruction.Write(node)
				}
			case xml.EndElement:
				if node.Name.Space == wordNS && node.Name.Local == "instrText" {
					inInstruction = false
				}
			}
			start, ok := tok.(xml.StartElement)
			if !ok {
				continue
			}
			if start.Name.Space == wordNS {
				switch start.Name.Local {
				case "instrText":
					inInstruction = true
				case "fldChar":
					switch attr(start, "fldCharType") {
					case "begin":
						// Native Word usually stores TOC/PAGE fields across runs.
						// Even an unknown/local field needs a real refresh before
						// its cached display can be considered current.
						s.NeedsFieldUpdate = true
						fields = append(fields, &wordField{collecting: true})
					case "separate":
						if len(fields) > 0 {
							fields[len(fields)-1].collecting = false
						}
					case "end":
						if len(fields) > 0 {
							inspectWordField(s, fields[len(fields)-1].instruction.String())
							fields = fields[:len(fields)-1]
						}
					}
				case "sectPr":
					s.Sections++
				case "tbl":
					s.Tables++
				case "pStyle":
					for _, a := range start.Attr {
						if a.Name.Local == "val" {
							for level := 1; level <= 3; level++ {
								if strings.EqualFold(a.Value, "Heading"+strconv.Itoa(level)) {
									s.HeadingCounts[strconv.Itoa(level)]++
								}
							}
						}
					}
				case "fldSimple":
					s.NeedsFieldUpdate = true
					for _, a := range start.Attr {
						if a.Name.Local == "instr" {
							inspectWordField(s, a.Value)
						}
					}
				}
			}
			if start.Name.Space == drawingNS && start.Name.Local == "tbl" {
				s.Tables++
			}
		}
	}
	return s, nil
}

func inspectWordField(s *Structure, instruction string) {
	words := strings.Fields(strings.ToUpper(instruction))
	if len(words) == 0 {
		return
	}
	switch words[0] {
	case "PAGE", "NUMPAGES", "SECTIONPAGES":
		s.PageFields++
	case "TOC":
		s.TOCFields++
	}
}
