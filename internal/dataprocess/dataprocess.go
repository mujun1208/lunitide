package dataprocess

import (
	"bytes"
	"context"
	"encoding/csv"
	"encoding/json"
	"errors"
	"sort"
	"strconv"
	"strings"

	"github.com/xuri/excelize/v2"
)

type Table struct {
	Headers []string
	Rows    [][]string
}

type ColumnStats struct {
	Count int
	Empty int
}

func DetectDuckDB() string {
	return "missing_dependency"
}

func RejectFormulaCell(cell string) error {
	if strings.HasPrefix(strings.TrimSpace(cell), "=") || strings.HasPrefix(strings.TrimSpace(cell), "+") {
		return errors.New("formula injection rejected")
	}
	return nil
}

func ImportCSV(raw []byte) (Table, error) {
	r := csv.NewReader(bytes.NewReader(raw))
	rows, err := r.ReadAll()
	if err != nil || len(rows) == 0 {
		return Table{}, err
	}
	return Table{Headers: rows[0], Rows: rows[1:]}, nil
}

func Filter(t Table, col, equals string) Table {
	idx := colIndex(t.Headers, col)
	if idx < 0 {
		return t
	}
	var out [][]string
	for _, row := range t.Rows {
		if idx < len(row) && row[idx] == equals {
			out = append(out, row)
		}
	}
	return Table{Headers: t.Headers, Rows: out}
}

func Dedup(t Table, keys []string) Table {
	idxs := make([]int, 0, len(keys))
	for _, k := range keys {
		if i := colIndex(t.Headers, k); i >= 0 {
			idxs = append(idxs, i)
		}
	}
	seen := map[string]bool{}
	var out [][]string
	for _, row := range t.Rows {
		var b strings.Builder
		for _, i := range idxs {
			if i < len(row) {
				b.WriteString(row[i])
			}
			b.WriteByte(0)
		}
		key := b.String()
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, row)
	}
	return Table{Headers: t.Headers, Rows: out}
}

func Merge(a, b Table) (Table, error) {
	if strings.Join(a.Headers, ",") != strings.Join(b.Headers, ",") {
		return Table{}, errors.New("header mismatch")
	}
	return Table{Headers: a.Headers, Rows: append(append([][]string{}, a.Rows...), b.Rows...)}, nil
}

func Stats(t Table, col string) ColumnStats {
	idx := colIndex(t.Headers, col)
	st := ColumnStats{Count: len(t.Rows)}
	if idx < 0 {
		return st
	}
	for _, row := range t.Rows {
		if idx >= len(row) || strings.TrimSpace(row[idx]) == "" {
			st.Empty++
		}
	}
	return st
}

func ImportJSON(raw []byte) (Table, error) {
	var rows []map[string]any
	if err := json.Unmarshal(raw, &rows); err != nil || len(rows) == 0 {
		return Table{}, err
	}
	seen := map[string]bool{}
	var headers []string
	for _, row := range rows {
		for k := range row {
			if !seen[k] {
				seen[k] = true
				headers = append(headers, k)
			}
		}
	}
	sort.Strings(headers)
	out := make([][]string, 0, len(rows))
	for _, row := range rows {
		line := make([]string, len(headers))
		for i, h := range headers {
			if v, ok := row[h]; ok && v != nil {
				line[i] = strings.TrimSpace(strings.Trim(strings.TrimSpace(stringifyJSON(v)), `"`))
			}
		}
		out = append(out, line)
	}
	return Table{Headers: headers, Rows: out}, nil
}

func stringifyJSON(v any) string {
	switch t := v.(type) {
	case string:
		return t
	case float64:
		return strconv.FormatFloat(t, 'f', -1, 64)
	default:
		b, _ := json.Marshal(v)
		return string(b)
	}
}

func ImportXLSX(raw []byte) (Table, error) {
	f, err := excelize.OpenReader(bytes.NewReader(raw))
	if err != nil {
		return Table{}, err
	}
	defer func() { _ = f.Close() }()
	name := f.GetSheetName(0)
	rows, err := f.GetRows(name)
	if err != nil || len(rows) == 0 {
		return Table{}, err
	}
	return Table{Headers: rows[0], Rows: rows[1:]}, nil
}

func InferTypes(t Table) map[string]string {
	out := map[string]string{}
	for i, h := range t.Headers {
		kind := "empty"
		for _, row := range t.Rows {
			if i >= len(row) || strings.TrimSpace(row[i]) == "" {
				continue
			}
			if _, err := strconv.ParseFloat(strings.TrimSpace(row[i]), 64); err == nil {
				if kind == "empty" || kind == "number" {
					kind = "number"
					continue
				}
			}
			kind = "text"
			break
		}
		out[h] = kind
	}
	return out
}

func Apply(ctx context.Context, t Table, fn func(Table) Table) (Table, error) {
	if err := ctx.Err(); err != nil {
		return Table{}, err
	}
	if fn == nil {
		return t, nil
	}
	return fn(t), nil
}

func ExportCSV(t Table) []byte {
	var buf bytes.Buffer
	w := csv.NewWriter(&buf)
	_ = w.Write(t.Headers)
	_ = w.WriteAll(t.Rows)
	w.Flush()
	return buf.Bytes()
}

func colIndex(headers []string, col string) int {
	for i, h := range headers {
		if h == col {
			return i
		}
	}
	return -1
}
