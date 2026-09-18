// Package xlsxrows reads worksheet grids from an XLSX zip with the standard
// library. excelize v2.11.0 File.GetRows / Rows.Columns panics on a negative
// shared-string index (GO-2026-6452, no patched release). Callers that only
// need string cells must not go through that API.
package xlsxrows

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"fmt"
	"io"
	"path"
	"strconv"
	"strings"
)

const (
	maxSheetXML = 64 << 20
	maxSSTXML   = 32 << 20
	maxCols     = 256
	maxRows     = 100_000
)

// Grid is one worksheet's string cells. Trailing blank rows are omitted;
// blank rows between populated ones are kept.
type Grid struct {
	Name string
	Rows [][]string
}

// Grids returns every worksheet in workbook order.
func Grids(data []byte) ([]Grid, error) {
	if len(data) == 0 || !bytes.HasPrefix(data, []byte("PK")) {
		return nil, fmt.Errorf("xlsx: not a workbook")
	}
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, fmt.Errorf("xlsx: open: %w", err)
	}
	sst, err := readSharedStrings(zr)
	if err != nil {
		return nil, err
	}
	sheets, err := readSheetList(zr)
	if err != nil {
		return nil, err
	}
	out := make([]Grid, 0, len(sheets))
	for _, sheet := range sheets {
		rows, err := readSheet(zr, sheet.path, sst)
		if err != nil {
			return nil, err
		}
		out = append(out, Grid{Name: sheet.name, Rows: rows})
	}
	return out, nil
}

type sheetRef struct {
	name string
	path string
}

func readSheetList(zr *zip.Reader) ([]sheetRef, error) {
	body, err := zipFile(zr, "xl/workbook.xml", maxSSTXML)
	if err != nil {
		return nil, fmt.Errorf("xlsx: workbook: %w", err)
	}
	rels := relTargets(zr)
	dec := xml.NewDecoder(bytes.NewReader(body))
	var out []sheetRef
	for {
		tok, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("xlsx: workbook: %w", err)
		}
		se, ok := tok.(xml.StartElement)
		if !ok || se.Name.Local != "sheet" {
			continue
		}
		name := xmlAttr(se, "name")
		id := xmlAttr(se, "id")
		target := rels[id]
		if target == "" {
			target = fmt.Sprintf("worksheets/sheet%d.xml", len(out)+1)
		}
		out = append(out, sheetRef{name: name, path: xlPath(target)})
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("xlsx: no sheets")
	}
	return out, nil
}

func relTargets(zr *zip.Reader) map[string]string {
	body, err := zipFile(zr, "xl/_rels/workbook.xml.rels", maxSSTXML)
	if err != nil {
		return map[string]string{}
	}
	dec := xml.NewDecoder(bytes.NewReader(body))
	out := map[string]string{}
	for {
		tok, err := dec.Token()
		if err != nil {
			return out
		}
		se, ok := tok.(xml.StartElement)
		if !ok || se.Name.Local != "Relationship" {
			continue
		}
		id := xmlAttr(se, "Id")
		target := xmlAttr(se, "Target")
		if id != "" && target != "" {
			out[id] = target
		}
	}
}

func readSharedStrings(zr *zip.Reader) ([]string, error) {
	body, err := zipFile(zr, "xl/sharedStrings.xml", maxSSTXML)
	if err != nil {
		if isMissing(err) {
			return nil, nil
		}
		return nil, err
	}
	dec := xml.NewDecoder(bytes.NewReader(body))
	var (
		out     []string
		inSI    bool
		inT     bool
		builder strings.Builder
	)
	for {
		tok, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("xlsx: shared strings: %w", err)
		}
		switch el := tok.(type) {
		case xml.StartElement:
			switch el.Name.Local {
			case "si":
				inSI = true
				builder.Reset()
			case "t":
				if inSI {
					inT = true
				}
			}
		case xml.EndElement:
			switch el.Name.Local {
			case "t":
				inT = false
			case "si":
				if inSI {
					out = append(out, builder.String())
				}
				inSI, inT = false, false
			}
		case xml.CharData:
			if inT {
				builder.Write(el)
			}
		}
	}
	return out, nil
}

func readSheet(zr *zip.Reader, name string, sst []string) ([][]string, error) {
	body, err := zipFile(zr, name, maxSheetXML)
	if err != nil {
		return nil, fmt.Errorf("xlsx: sheet %s: %w", name, err)
	}
	dec := xml.NewDecoder(bytes.NewReader(body))
	results, cur, maxVal := make([][]string, 0, 64), 0, 0
	var (
		inRow, inC, inV, inIS bool
		cellType, cellRef     string
		value                 strings.Builder
		rowCells              []string
	)
	flushRow := func() {
		if cur <= 0 || cur > maxRows {
			return
		}
		row := trimTrailingEmpty(rowCells)
		if len(row) == 0 {
			return
		}
		if emptyRows := cur - maxVal - 1; emptyRows > 0 {
			results = append(results, make([][]string, emptyRows)...)
		}
		results = append(results, row)
		maxVal = cur
	}
	for {
		tok, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("xlsx: sheet %s: %w", name, err)
		}
		switch el := tok.(type) {
		case xml.StartElement:
			switch el.Name.Local {
			case "row":
				inRow = true
				rowCells = rowCells[:0]
				if n := atoi(xmlAttr(el, "r")); n > 0 {
					cur = n
				} else {
					cur++
				}
			case "c":
				if !inRow {
					continue
				}
				inC = true
				cellType = xmlAttr(el, "t")
				cellRef = xmlAttr(el, "r")
				value.Reset()
			case "v":
				if inC {
					inV = true
					value.Reset()
				}
			case "is":
				if inC {
					inIS = true
					value.Reset()
				}
			}
		case xml.EndElement:
			switch el.Name.Local {
			case "row":
				if inRow {
					flushRow()
				}
				inRow = false
			case "c":
				if inC {
					putCell(&rowCells, cellRef, cellValue(cellType, value.String(), sst))
				}
				inC, inV, inIS = false, false, false
			case "v":
				inV = false
			case "is":
				inIS = false
			}
		case xml.CharData:
			if inV || inIS {
				value.Write(el)
			}
		}
	}
	if maxVal == 0 {
		return [][]string{}, nil
	}
	return results[:maxVal], nil
}

func cellValue(cellType, raw string, sst []string) string {
	switch cellType {
	case "s":
		i := atoi(strings.TrimSpace(raw))
		if i < 0 || i >= len(sst) {
			return ""
		}
		return sst[i]
	case "b":
		if raw == "1" || strings.EqualFold(raw, "true") {
			return "TRUE"
		}
		return "FALSE"
	case "inlineStr", "str", "e":
		return raw
	default:
		return raw
	}
}

func putCell(row *[]string, ref, val string) {
	col := colIndex(ref)
	if col <= 0 || col > maxCols {
		return
	}
	for len(*row) < col {
		*row = append(*row, "")
	}
	(*row)[col-1] = val
}

func colIndex(ref string) int {
	n := 0
	for _, r := range ref {
		if r < 'A' || r > 'Z' {
			break
		}
		n = n*26 + int(r-'A'+1)
	}
	return n
}

func trimTrailingEmpty(row []string) []string {
	end := len(row)
	for end > 0 && row[end-1] == "" {
		end--
	}
	if end == 0 {
		return nil
	}
	out := make([]string, end)
	copy(out, row[:end])
	return out
}

func xmlAttr(se xml.StartElement, local string) string {
	for _, a := range se.Attr {
		if a.Name.Local == local {
			return a.Value
		}
	}
	return ""
}

func atoi(s string) int {
	n, err := strconv.Atoi(strings.TrimSpace(s))
	if err != nil {
		return 0
	}
	return n
}

func xlPath(target string) string {
	target = strings.TrimPrefix(strings.ReplaceAll(target, "\\", "/"), "/")
	if strings.HasPrefix(target, "xl/") {
		return path.Clean(target)
	}
	return path.Clean("xl/" + target)
}

func zipFile(zr *zip.Reader, name string, max int) ([]byte, error) {
	name = path.Clean(name)
	for _, f := range zr.File {
		if path.Clean(f.Name) != name {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return nil, err
		}
		data, err := io.ReadAll(io.LimitReader(rc, int64(max)+1))
		_ = rc.Close()
		if err != nil {
			return nil, err
		}
		if len(data) > max {
			return nil, fmt.Errorf("xlsx: %s too large", name)
		}
		return data, nil
	}
	return nil, fmt.Errorf("xlsx: missing %s", name)
}

func isMissing(err error) bool {
	return err != nil && strings.Contains(err.Error(), "missing ")
}
