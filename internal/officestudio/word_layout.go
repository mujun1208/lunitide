package officestudio

import (
	"bytes"
	"encoding/xml"
	"io"
	"strings"
)

type wordFlowBlock struct {
	kind        string
	level       int
	text        string
	header      bool
	fixedHeight bool
}

func wordLayoutIssues(parts map[string][]byte, nodes []Node) []Issue {
	body := parts["word/document.xml"]
	if len(body) == 0 {
		return nil
	}
	blocks, err := scanWordFlow(body)
	if err != nil {
		return nil
	}
	var issues []Issue
	for i, block := range blocks {
		switch block.kind {
		case "heading":
			if wordHeadingIsWidow(blocks, i) {
				issues = append(issues, Issue{
					Code: "OFFICE_WIDOW_HEADING", Severity: "warning",
					Message: "标题后没有正文，可能成为孤行。",
					Part:    "word/document.xml", NodeID: wordNodeID(nodes, block.text),
				})
			}
		case "caption":
			if strings.TrimSpace(block.text) == "" {
				issues = append(issues, Issue{
					Code: "OFFICE_EMPTY_CAPTION", Severity: "warning",
					Message: "题注样式没有说明文字。",
					Part:    "word/document.xml",
				})
			}
		case "table":
			if !block.header {
				issues = append(issues, Issue{
					Code: "OFFICE_TABLE_HEADER", Severity: "warning",
					Message: "表格第一行未标记为重复表头。",
					Part:    "word/document.xml",
				})
			}
			if block.fixedHeight {
				issues = append(issues, Issue{
					Code: "OFFICE_FIXED_ROW_CLIP", Severity: "warning",
					Message: "表格使用固定行高，长单元格可能被裁切。",
					Part:    "word/document.xml",
				})
			}
		}
	}
	return issues
}

func wordHeadingIsWidow(blocks []wordFlowBlock, index int) bool {
	level := blocks[index].level
	for _, next := range blocks[index+1:] {
		switch next.kind {
		case "heading":
			if next.level <= level {
				return true
			}
		case "body", "table":
			return false
		case "caption":
			if strings.TrimSpace(next.text) != "" {
				return false
			}
		}
	}
	return true
}

func wordNodeID(nodes []Node, text string) string {
	text = strings.TrimSpace(text)
	if text == "" {
		return ""
	}
	for _, n := range nodes {
		if n.Text == text {
			return n.ID
		}
	}
	return ""
}

func scanWordFlow(body []byte) ([]wordFlowBlock, error) {
	dec := xml.NewDecoder(bytes.NewReader(body))
	depth := 0
	bodyDepth := 0
	inBody := false
	var blocks []wordFlowBlock
	var cur *wordFlowBlock
	var text strings.Builder
	firstRow := false
	firstRowSeen := false
	for {
		tok, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		switch t := tok.(type) {
		case xml.StartElement:
			depth++
			if t.Name.Space != wordNS {
				continue
			}
			if t.Name.Local == "body" {
				inBody = true
				bodyDepth = depth
				continue
			}
			if !inBody {
				continue
			}
			if depth == bodyDepth+1 && t.Name.Local == "p" {
				cur = &wordFlowBlock{kind: "body"}
				text.Reset()
			}
			if depth == bodyDepth+1 && t.Name.Local == "tbl" {
				cur = &wordFlowBlock{kind: "table"}
				firstRow, firstRowSeen = false, false
			}
			if cur == nil {
				continue
			}
			switch t.Name.Local {
			case "pStyle":
				style := attr(t, "val")
				if strings.EqualFold(style, "Caption") {
					cur.kind = "caption"
				}
				for level := 1; level <= 3; level++ {
					if strings.EqualFold(style, "Heading"+string(rune('0'+level))) {
						cur.kind = "heading"
						cur.level = level
					}
				}
			case "tr":
				if cur.kind == "table" && !firstRowSeen {
					firstRow = true
					firstRowSeen = true
				}
			case "tblHeader":
				if firstRow {
					cur.header = true
				}
			case "trHeight":
				if cur.kind == "table" {
					if val := attr(t, "val"); val != "" && val != "0" {
						cur.fixedHeight = true
					}
				}
			}
		case xml.CharData:
			if cur != nil && cur.kind != "table" {
				text.Write(t)
			}
		case xml.EndElement:
			if t.Name.Space == wordNS && inBody && depth == bodyDepth+1 && cur != nil && (t.Name.Local == "p" || t.Name.Local == "tbl") {
				cur.text = text.String()
				if cur.kind != "body" || strings.TrimSpace(cur.text) != "" {
					blocks = append(blocks, *cur)
				}
				cur = nil
			}
			if t.Name.Space == wordNS && t.Name.Local == "tr" {
				firstRow = false
			}
			if t.Name.Space == wordNS && t.Name.Local == "body" {
				inBody = false
			}
			depth--
		}
	}
	return blocks, nil
}
