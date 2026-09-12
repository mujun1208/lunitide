package officestudio

import (
	"bytes"
	"encoding/xml"
	"io"
	"path"
	"strings"
)

func sheetLayoutIssues(parts map[string][]byte) []Issue {
	names := workbookSheetNames(parts)
	var issues []Issue
	for _, part := range sortedPartNames(parts) {
		if !strings.HasPrefix(part, "xl/worksheets/sheet") || strings.Contains(part, "/_rels/") {
			continue
		}
		if names[part] != "原始数据" {
			continue
		}
		if !bytes.Contains(parts[part], []byte("mergeCell")) {
			continue
		}
		issues = append(issues, Issue{
			Code: "OFFICE_DETAIL_MERGE", Severity: "warning",
			Message: "明细表使用了合并单元格，可能妨碍筛选。",
			Part:    part,
		})
	}
	return issues
}

func workbookSheetNames(parts map[string][]byte) map[string]string {
	idToName := map[string]string{}
	dec := xml.NewDecoder(bytes.NewReader(parts["xl/workbook.xml"]))
	for {
		tok, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil
		}
		start, ok := tok.(xml.StartElement)
		if !ok || start.Name.Local != "sheet" {
			continue
		}
		name, id := "", ""
		for _, a := range start.Attr {
			switch a.Name.Local {
			case "name":
				name = a.Value
			case "id":
				id = a.Value
			}
		}
		if name != "" && id != "" {
			idToName[id] = name
		}
	}
	idToTarget := map[string]string{}
	relDec := xml.NewDecoder(bytes.NewReader(parts["xl/_rels/workbook.xml.rels"]))
	for {
		tok, err := relDec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil
		}
		start, ok := tok.(xml.StartElement)
		if !ok || start.Name.Local != "Relationship" {
			continue
		}
		id, target := "", ""
		for _, a := range start.Attr {
			switch a.Name.Local {
			case "Id":
				id = a.Value
			case "Target":
				target = a.Value
			}
		}
		if id != "" && target != "" {
			idToTarget[id] = target
		}
	}
	out := map[string]string{}
	for id, name := range idToName {
		target := idToTarget[id]
		if target == "" {
			continue
		}
		part := path.Clean("xl/" + strings.TrimPrefix(target, "/"))
		if strings.HasPrefix(target, "worksheets/") {
			part = path.Clean("xl/" + target)
		}
		out[part] = name
	}
	return out
}
