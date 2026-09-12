package app

import (
	"encoding/json"
	"path/filepath"
	"strings"

	content "github.com/lunitide/lunitide/internal/officestudio"
)

// adaptOfficeGenerateArgs rewrites a docx.gen / pptx.gen / excel.gen payload
// into office.generate {name,spec} without loosening global decodePayload.
func adaptOfficeGenerateArgs(raw json.RawMessage) json.RawMessage {
	var m map[string]json.RawMessage
	if json.Unmarshal(raw, &m) != nil || m == nil {
		return raw
	}
	if spec, ok := m["spec"]; ok {
		out := map[string]json.RawMessage{"spec": spec}
		if v, ok := m["taskId"]; ok {
			out["taskId"] = v
		}
		if v, ok := m["name"]; ok {
			out["name"] = v
		} else if v, ok := m["path"]; ok {
			out["name"] = v
		}
		changed := false
		if _, hasName := m["name"]; !hasName {
			if _, hasPath := m["path"]; hasPath {
				changed = true
			}
		}
		for k := range m {
			if k != "taskId" && k != "name" && k != "spec" {
				changed = true
				break
			}
		}
		if !changed {
			return raw
		}
		b, err := json.Marshal(out)
		if err != nil {
			return raw
		}
		return b
	}
	name := jsonRawString(m["name"])
	if name == "" {
		name = jsonRawString(m["path"])
	}
	title := jsonRawString(m["title"])
	if title == "" && name != "" {
		title = strings.TrimSuffix(filepath.Base(name), filepath.Ext(name))
	}
	kind := officeKindFromName(name, jsonRawString(m["kind"]))
	spec := map[string]any{"schemaVersion": 1}
	if title != "" {
		spec["title"] = title
	}
	hasContent := false
	if b, ok := m["blocks"]; ok {
		spec["blocks"] = json.RawMessage(b)
		hasContent = true
		if kind == "" {
			kind = "docx"
		}
	}
	if s, ok := m["slides"]; ok {
		slides, ok := adaptPptxGenSlides(s)
		if !ok {
			return raw
		}
		spec["slides"] = slides
		hasContent = true
		if kind == "" {
			kind = "pptx"
		}
	}
	if s, ok := m["sheets"]; ok {
		sheets, ok := adaptExcelGenSheets(s)
		if !ok {
			return raw
		}
		spec["sheets"] = sheets
		hasContent = true
		if kind == "" {
			kind = "xlsx"
		}
	}
	if b, ok := m["body"]; ok {
		var body string
		if json.Unmarshal(b, &body) != nil || strings.TrimSpace(body) == "" {
			return raw
		}
		spec["body"] = body
		hasContent = true
		if kind == "" {
			kind = "pdf"
		}
	}
	if !hasContent || name == "" {
		return raw
	}
	if kind != "" {
		spec["kind"] = kind
	}
	out := map[string]any{"name": name, "spec": spec}
	if tid, ok := m["taskId"]; ok {
		out["taskId"] = json.RawMessage(tid)
	}
	b, err := json.Marshal(out)
	if err != nil {
		return raw
	}
	return b
}

func officeKindFromName(name, kind string) string {
	switch strings.ToLower(strings.TrimSpace(kind)) {
	case "docx", "pptx", "xlsx", "pdf":
		return strings.ToLower(strings.TrimSpace(kind))
	case "report", "novel", "document":
		return "docx"
	}
	switch strings.ToLower(filepath.Ext(name)) {
	case ".docx":
		return "docx"
	case ".pptx":
		return "pptx"
	case ".xlsx":
		return "xlsx"
	case ".pdf":
		return "pdf"
	}
	return ""
}

func jsonRawString(raw json.RawMessage) string {
	var s string
	if json.Unmarshal(raw, &s) != nil {
		return ""
	}
	return strings.TrimSpace(s)
}

func adaptPptxGenSlides(raw json.RawMessage) ([]content.Slide, bool) {
	var office []content.Slide
	if json.Unmarshal(raw, &office) == nil && len(office) > 0 && strings.TrimSpace(office[0].Title) != "" {
		return office, true
	}
	var gen []struct {
		Title   string   `json:"title"`
		Bullets []string `json:"bullets"`
		Notes   string   `json:"notes"`
		Layout  string   `json:"layout"`
	}
	if json.Unmarshal(raw, &gen) != nil || len(gen) == 0 {
		return nil, false
	}
	out := make([]content.Slide, 0, len(gen))
	for _, s := range gen {
		if strings.TrimSpace(s.Title) == "" {
			continue
		}
		out = append(out, content.Slide{Title: s.Title, Bullets: s.Bullets, Notes: s.Notes, Layout: s.Layout})
	}
	return out, len(out) > 0
}

func adaptExcelGenSheets(raw json.RawMessage) ([]content.Sheet, bool) {
	var office []content.Sheet
	if json.Unmarshal(raw, &office) == nil && len(office) > 0 && officeSheetTyped(office[0]) {
		return office, true
	}
	var gen []struct {
		Name    string              `json:"name"`
		Headers []string            `json:"headers"`
		Rows    [][]json.RawMessage `json:"rows"`
	}
	if json.Unmarshal(raw, &gen) != nil || len(gen) == 0 {
		return nil, false
	}
	out := make([]content.Sheet, 0, len(gen))
	for _, s := range gen {
		name := strings.TrimSpace(s.Name)
		if name == "" {
			name = "Sheet1"
		}
		var rows [][]content.Cell
		if len(s.Headers) > 0 {
			header := make([]content.Cell, len(s.Headers))
			for i, h := range s.Headers {
				header[i] = content.Cell{Type: "text", Value: h}
			}
			rows = append(rows, header)
		}
		for _, row := range s.Rows {
			cells := make([]content.Cell, len(row))
			for i, cell := range row {
				cells[i] = excelGenCell(cell)
			}
			rows = append(rows, cells)
		}
		if len(rows) == 0 {
			continue
		}
		out = append(out, content.Sheet{Name: name, Rows: rows})
	}
	return out, len(out) > 0
}

func officeSheetTyped(sheet content.Sheet) bool {
	return len(sheet.Rows) > 0 && len(sheet.Rows[0]) > 0 && sheet.Rows[0][0].Type != ""
}

func excelGenCell(raw json.RawMessage) content.Cell {
	var typed content.Cell
	if json.Unmarshal(raw, &typed) == nil && typed.Type != "" {
		return typed
	}
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return content.Cell{Type: "text", Value: s}
	}
	return content.Cell{Type: "text", Value: strings.TrimSpace(string(raw))}
}
